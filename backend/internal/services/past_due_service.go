// Package services provides business logic and external service integrations.
// This file implements PastDueService for dunning grace-expiry and Plaid-disconnect logic.
// Issue #898: past_due lifecycle management.
package services

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	phclient "github.com/maxnussbaum/BoostedBudget/internal/integration/posthog"
	"github.com/maxnussbaum/BoostedBudget/internal/repository"
)

// ErrPaddleAlreadyCancelled is returned by PastDuePaddleClient.CancelSubscription when
// the subscription is already in a cancelled state in Paddle. This error is tolerated —
// the flow continues because the local state still needs to be updated.
var ErrPaddleAlreadyCancelled = errors.New("paddle subscription already cancelled")

// pastDueClaimLease is the lease duration requested for both ActionPlaidDisconnectPastDue and
// ActionPastDueGraceCancel work claims — matches ExecuteDeletion's accountDeletionClaimLease
// (Issue #1413, deletion_service.go). Both paths make Plaid/Paddle calls; generous beats tight,
// and the fencing token (repository.WorkClaimsRepository) makes an over-run detectable rather
// than silently re-granted to a second worker.
const pastDueClaimLease = 15 * time.Minute

// PastDueProfileRepository defines the repository methods needed by PastDueService.
type PastDueProfileRepository interface {
	// GetUsersInPastDue returns users with subscription_status='past_due' and non-nil past_due_started_at.
	GetUsersInPastDue(ctx context.Context) ([]domain.User, error)

	// GetUserByID retrieves a user by their internal UUID (used for race-condition re-read).
	GetUserByID(ctx context.Context, userID string) (*domain.User, error)

	// CancelIfPastDueOccurrenceWithRetention atomically flips subscription_status to 'cancelled'
	// AND starts the 6-month retention clock in the SAME UPDATE, only if the user is still
	// 'past_due', carries no currently-active manual tier override, AND past_due_started_at still
	// equals expectedPastDueStartedAt — the SPECIFIC dunning cycle this flow claimed. Returns
	// (true, nil) if the flip happened, (false, nil) for ANY predicate miss: the user already
	// recovered, an admin granted/extended a comp in that window (Issue #1413 High finding H6,
	// Codex review round 3), or (Issue #1413 round-4 finding H1) the row has already moved on to a
	// DIFFERENT past_due cycle than the one this flow verified — cycle B is ALSO 'past_due' with
	// no override, so without the occurrence check a stale worker holding cycle A's claim would
	// flip cycle B before cycle B's own grace period had expired.
	//
	// Issue #1413 round-3 finding H5 residual: this REPLACES the earlier separate
	// CancelIfPastDue + SetRetentionDates pair. That pair left a window where the CAS could
	// succeed but the retention-clock write could still be lost (crash, dead-lettered job) — and
	// because GetUsersInPastDue only selects subscription_status = 'past_due', the daily dunning
	// job can never re-select a user this already flipped to 'cancelled', so that user would be
	// cancelled forever with no retention clock: never warned, never deleted. One statement means
	// either both land or neither does.
	CancelIfPastDueOccurrenceWithRetention(ctx context.Context, userID string, expectedPastDueStartedAt, retentionStarted, retentionExpires time.Time) (bool, error)

	// ClearPastDueStartedAtIfMatches clears the past_due_started_at timestamp after auto-cancel,
	// but ONLY if the column still holds expected — the SAME timestamp this flow captured at the
	// top of processGraceExpiry (Issue #1413 round-4 finding H3). The plain, unconditional
	// ClearPastDueStartedAt (used by subscription_webhook.go's recovery-path callers, which have
	// no "expected occurrence" to compare against) is WRONG here: between this flow's CAS flip
	// and this clear step, the user could have recovered and re-entered a NEWER past_due cycle
	// (a new past_due_started_at already stamped by the webhook) — an unconditional clear would
	// erase that newer cycle's timestamp, and since GetUsersInPastDue requires
	// past_due_started_at IS NOT NULL, that user would never be selected for dunning again.
	//
	// Returns (true, nil) when cleared, (false, nil) when the timestamp had already moved on to a
	// newer cycle (a legitimate skip — see the call site), and (false, ErrUserNotFound) if the
	// user genuinely no longer exists — a DIFFERENT condition the caller must be able to tell
	// apart from the "moved on" skip (round-4 review requirement).
	ClearPastDueStartedAtIfMatches(ctx context.Context, userID string, expected time.Time) (bool, error)
}

// PastDuePlaidCleanup defines the Plaid cleanup operation needed by PastDueService.
type PastDuePlaidCleanup interface {
	// DisconnectAllUserItems calls Plaid /item/remove for all items
	// then marks them as 'disconnected' (preserves transaction history).
	DisconnectAllUserItems(ctx context.Context, userID string) error
}

// PastDuePaddleClient defines the Paddle cancel operation needed by PastDueService.
// The implementation must tolerate paddle.ErrSubscriptionIsCanceledActionInvalid by
// mapping it to ErrPaddleAlreadyCancelled.
type PastDuePaddleClient interface {
	// CancelSubscription cancels the given Paddle subscription ID immediately.
	// Returns ErrPaddleAlreadyCancelled if already cancelled (caller should tolerate).
	CancelSubscription(ctx context.Context, subscriptionID string) error
}

// PastDueClerkClient defines the Clerk metadata update needed by PastDueService.
type PastDueClerkClient interface {
	// UpdateUserMetadata sets public metadata on a Clerk user.
	UpdateUserMetadata(ctx context.Context, clerkUserID string, metadata map[string]interface{}) error
}

// PastDueNotificationRepository defines the notification creation needed by PastDueService.
type PastDueNotificationRepository interface {
	// Create persists a new in-app notification.
	Create(ctx context.Context, n *domain.Notification) error
}

// householdCoverageCheckerForPastDue is the narrow capability PastDueService needs to detect an
// active household-sourced entitlement, so the daily past_due dunning job doesn't cancel a
// covered member's subscription or disconnect their Plaid items despite being actively covered
// (#1413 — mirrors householdCoverageCheckerForTrial in trial_service.go). Satisfied structurally
// by *HouseholdEntitlementService.
type householdCoverageCheckerForPastDue interface {
	IsCoveredByHousehold(ctx context.Context, userID string) (bool, error)
}

// PastDueService manages the dunning lifecycle for users with past_due subscriptions.
// It is called daily by the dunning cron job.
//
// Ordering contract (non-negotiable for retryability):
//   1. Disconnect Plaid — if this fails, stop; user stays past_due and retries tomorrow.
//   2. Cancel Paddle — tolerate already-cancelled.
//   3. Flip subscription_status → cancelled (CAS: only if still past_due).
//   4. Start retention clock.
//   5. Clear past_due_started_at.
//   6. Sync Clerk metadata + create notification.
type PastDueService struct {
	repo                PastDueProfileRepository
	plaidCleanup        PastDuePlaidCleanup
	paddleClient        PastDuePaddleClient
	clerkClient         PastDueClerkClient
	notificationRepo    PastDueNotificationRepository
	analytics           phclient.Analytics
	householdChecker    householdCoverageCheckerForPastDue // #1413: nil skips the exemption
	claimsRepo          repository.WorkClaimsRepository     // #1413 H6a/H6b: required, see SetWorkClaimsRepo
	graceDays           int
	plaidDisconnectDays int
	retentionMonths     int
	logger              *slog.Logger
}

// NewPastDueService creates a new PastDueService.
//
//   - graceDays: days past_due before auto-cancel (typically 21).
//   - plaidDisconnectDays: days past_due before Plaid is pre-emptively disconnected (typically 14).
func NewPastDueService(
	repo PastDueProfileRepository,
	plaidCleanup PastDuePlaidCleanup,
	paddleClient PastDuePaddleClient,
	clerkClient PastDueClerkClient,
	notificationRepo PastDueNotificationRepository,
	graceDays, plaidDisconnectDays int,
) *PastDueService {
	return &PastDueService{
		repo:                repo,
		plaidCleanup:        plaidCleanup,
		paddleClient:        paddleClient,
		clerkClient:         clerkClient,
		notificationRepo:    notificationRepo,
		graceDays:           graceDays,
		plaidDisconnectDays: plaidDisconnectDays,
		retentionMonths:     6,
		logger:              slog.Default(),
	}
}

// SetPlaidCleanup wires in the Plaid cleanup service (called after Plaid is configured).
func (s *PastDueService) SetPlaidCleanup(svc PastDuePlaidCleanup) {
	s.plaidCleanup = svc
}

// SetAnalytics sets the PostHog analytics client (nil-safe no-op when not configured).
func (s *PastDueService) SetAnalytics(a phclient.Analytics) {
	s.analytics = a
}

// SetHouseholdChecker wires the household-coverage exemption for past_due dunning.
// #1413 — mirrors TrialService.SetHouseholdChecker. Nil-safe: an unset checker preserves
// pre-#1413 behavior.
func (s *PastDueService) SetHouseholdChecker(c householdCoverageCheckerForPastDue) {
	s.householdChecker = c
}

// SetWorkClaimsRepo wires the durable work-claims mechanism processGraceExpiry (H6b) and
// processPlaidDisconnectWindow (H6a) require for EXCLUSIVITY before any irreversible Plaid/Paddle
// call. Issue #1413, Codex review round 4 — mirrors DeletionService.SetWorkClaimsRepo. Like that
// method, this is NOT an optional capability: the self-assigning-update "claims" both paths used
// before (ClaimPlaidDisconnectIfPastDue, and the old ordering in processGraceExpiry) reserved
// nothing, so two workers could both disconnect the same user's Plaid items or both cancel their
// Paddle subscription. A nil claims repo makes both destructive paths fail loudly rather than
// silently falling back to that guard-only behavior.
func (s *PastDueService) SetWorkClaimsRepo(repo repository.WorkClaimsRepository) {
	s.claimsRepo = repo
}

// GetUsersInPastDue returns users whose subscription_status is 'past_due'
// and whose past_due_started_at is non-nil.
func (s *PastDueService) GetUsersInPastDue(ctx context.Context) ([]domain.User, error) {
	return s.repo.GetUsersInPastDue(ctx)
}

// ProcessPastDueUser evaluates one user's dunning state and takes action if thresholds are met.
//
// now is the current wall-clock time (injected so callers can test with arbitrary clocks).
func (s *PastDueService) ProcessPastDueUser(ctx context.Context, user domain.User, now time.Time) error {
	if user.PastDueStartedAt == nil {
		// Guard: nothing to do for users without a stamp.
		return nil
	}

	// #1413: an active manual override ("comp") grants access and must not be undermined by
	// the daily dunning job — a comped user can still carry a past_due status and a live
	// PaddleSubscriptionID (the admin comp never clears it), so every destructive branch below
	// is reachable unless gated here. The domain.User passed in is populated via
	// userSelectQuery, which already includes the override columns, so this check is free —
	// no DB round-trip. Checked before the household-coverage lookup below for the same
	// pure-and-cheap-first reason as TrialService.ExpireTrial.
	if user.HasActiveManualOverride() {
		s.logger.Info("skipping past_due processing for actively comped user", "user_id", user.ID)
		return nil
	}

	// #1413: closes an asymmetry with TrialService — exempt actively covered household
	// members. Fail closed on an uncertain check (skip processing this run) but PROPAGATE the
	// error rather than swallowing it — see trial_service.go:197-204 for why swallowing would
	// hide an operational failure. The job retries, so this self-heals.
	if s.householdChecker != nil {
		covered, err := s.householdChecker.IsCoveredByHousehold(ctx, user.ID)
		if err != nil {
			s.logger.Warn("failed to check household coverage during past_due processing; skipping this run",
				"user_id", user.ID, "error", err)
			return fmt.Errorf("failed to check household coverage: %w", err)
		}
		if covered {
			s.logger.Info("skipping past_due processing for actively covered household member", "user_id", user.ID)
			return nil
		}
	}

	age := now.Sub(*user.PastDueStartedAt)

	switch {
	case age >= time.Duration(s.graceDays)*24*time.Hour:
		return s.processGraceExpiry(ctx, user, now)
	case age >= time.Duration(s.plaidDisconnectDays)*24*time.Hour:
		return s.processPlaidDisconnectWindow(ctx, user)
	}
	return nil
}

// processGraceExpiry handles auto-cancellation when grace period has elapsed.
// Ordering: re-read → claim → re-verify → Plaid → Paddle → CAS flip → retention → clear stamp →
// notify → Complete. Issue #1413 High finding H6b: a real work claim on ActionPastDueGraceCancel
// runs BEFORE external work for EXCLUSIVITY; predicate is re-verified immediately after claim wins.
func (s *PastDueService) processGraceExpiry(ctx context.Context, user domain.User, now time.Time) error {
	// Issue #1413 round-4 finding H1: occurrence computed ONCE and threaded into every check and
	// write below — the claim key (P5, already fixed), BOTH reReadStillPastDue re-verifications
	// (this fix), and the final CAS predicate (this fix) — so a worker that has been sitting on a
	// STALE snapshot of an earlier dunning cycle can never act on a newer one. user.PastDueStartedAt
	// is guaranteed non-nil here: ProcessPastDueUser returns early otherwise.
	occurrence := repository.FormatOccurrenceTimestamp(user.PastDueStartedAt)

	// Cheap early check (mirrors ExecuteDeletion's belt-and-braces short-circuit): avoids
	// claiming at all in the common already-recovered/already-comped/already-newer-cycle case. NOT
	// the source of correctness — the re-verify immediately after the claim below is.
	if stillPastDue, err := s.reReadStillPastDue(ctx, user.ID, occurrence); err != nil {
		return fmt.Errorf("failed to re-read user %s before grace expiry: %w", user.ID, err)
	} else if !stillPastDue {
		return nil
	}

	if s.claimsRepo == nil {
		return fmt.Errorf("processGraceExpiry: work claims repository not configured")
	}
	// Issue #1413 P5: the claim key must identify not just WHO (user.ID) and WHAT
	// (ActionPastDueGraceCancel) but WHICH OCCURRENCE — otherwise the moment this flow completes
	// once, the SAME user can never have grace-cancel run again for the lifetime of the
	// work_claims row, even after a full recover→re-enter-past-due cycle. past_due_started_at is
	// the dunning-cycle discriminator: it is set once when the user enters past_due and cleared
	// (ClearPastDueStartedAtIfMatches, below) only after this flow completes, so it is stable
	// across retries of the SAME cycle and changes on the NEXT one. See
	// internal/repository/work_claims_occurrence.go for the full rationale.
	resourceID, err := repository.OccurrenceResourceID(user.ID, occurrence)
	if err != nil {
		return fmt.Errorf("processGraceExpiry: %w", err)
	}
	token, claimed, err := s.claimsRepo.Claim(ctx, repository.ResourceTypeUser, resourceID, repository.ActionPastDueGraceCancel, pastDueClaimLease)
	if err != nil {
		return fmt.Errorf("failed to claim grace-expiry cancellation for user %s: %w", user.ID, err)
	}
	if !claimed {
		// #1413 round-3 finding H3: this bare bool conflates "another worker holds this lease
		// right now" with "already completed." That distinction matters for a webhook (see
		// SubscriptionWebhookWorker.processSubscriptionCanceled, which uses ClaimOutcome and
		// returns an error on HeldByOther instead) because a webhook delivery is a one-shot
		// event — nothing else re-triggers it if the current holder crashes.
		//
		// This is a DELIBERATE, per-caller choice to keep the plain skip here, not an oversight:
		// GetUsersInPastDue re-runs on its own daily schedule regardless of this call's outcome,
		// so both "in-flight elsewhere" and "already done" self-heal on tomorrow's run without
		// this service needing to distinguish them or propagate an error today.
		s.logger.Info("past_due grace-expiry claim lost — another worker holds it, or it was already completed",
			"user_id", user.ID)
		return nil
	}

	// Re-verify the predicate against the freshest possible read, immediately after winning the
	// claim and before any irreversible external work. This is already as late as the code can
	// make this check — it sits directly above Step 1's Plaid disconnect below, with nothing but
	// a log statement in between.
	//
	// Issue #1413 round-4 finding H2: a comp granted in the window between this re-check and the
	// Plaid disconnect call is a DOCUMENTED, ACCEPTED residual, not something further tightening
	// here can close — no database mechanism can make that window zero (there is always some
	// interval between the last possible check and an external API call), and a worker already
	// inside a Plaid call cannot be recalled. Unlike C1's residual (irreversible account
	// deletion), this one is acceptable: a Plaid disconnect is recoverable (the user relinks),
	// and the retention clock this run is about to start is cleared the moment the comp is
	// applied — inconvenience, not data loss.
	stillPastDue, err := s.reReadStillPastDue(ctx, user.ID, occurrence)
	if err != nil {
		s.releaseClaimOnFailure(ctx, user.ID, resourceID, repository.ActionPastDueGraceCancel, token, err.Error())
		return fmt.Errorf("failed to re-verify user %s before grace expiry: %w", user.ID, err)
	}
	if !stillPastDue {
		s.releaseClaimOnFailure(ctx, user.ID, resourceID, repository.ActionPastDueGraceCancel, token, "precondition lost since claim")
		return nil
	}

	s.logger.Info("past_due grace period expired, auto-cancelling",
		"user_id", user.ID,
		"past_due_started_at", user.PastDueStartedAt,
	)

	// Step 1: Disconnect Plaid FIRST.
	// If this fails, Release (so the next run can retry immediately) and return the error so the
	// caller (cron job) retries. The user stays past_due — no partial cancellation.
	if s.plaidCleanup != nil {
		if err := s.plaidCleanup.DisconnectAllUserItems(ctx, user.ID); err != nil {
			// Issue #1413 H1: DisconnectAllUserItems can return ErrSideEffectClaimBusy when
			// another flow (the subscription-cancellation webhook, the Plaid-disconnect window
			// pass, or another grace-cancel attempt) currently holds ActionPlaidDisconnectUser for
			// this user. Log it distinctly at Info — this is expected contention, not a Plaid
			// failure — but it must still release this flow claim and propagate exactly like any
			// other Step 1 failure so tomorrow's run retries.
			if errors.Is(err, ErrSideEffectClaimBusy) {
				s.logger.Info("grace-cancel Plaid disconnect deferred — side-effect claim held by another flow",
					"user_id", user.ID)
			}
			s.releaseClaimOnFailure(ctx, user.ID, resourceID, repository.ActionPastDueGraceCancel, token, err.Error())
			return fmt.Errorf("failed to disconnect Plaid items for past_due user %s: %w", user.ID, err)
		}
	}

	// Step 2: Cancel Paddle subscription.
	// Nil-guard: paddleClient is nil when PADDLE_API_KEY is unset (mirrors plaidCleanup convention).
	// When Paddle is unconfigured, log a warning and skip — the subscription will eventually be
	// reconciled via Paddle's own dunning or a future webhook, but ops should be aware.
	// Tolerate already-cancelled — the subscription may have been cancelled by Paddle's dunning
	// before our job ran.
	if user.PaddleSubscriptionID != nil {
		if s.paddleClient == nil {
			s.logger.Warn("skipping Paddle cancel in grace expiry: paddleClient not configured",
				"user_id", user.ID,
				"subscription_id", *user.PaddleSubscriptionID,
			)
		} else {
			// Issue #1413 H1: claim the OPERATION-keyed ActionPaddleCancelSubscription side-effect
			// claim before calling Paddle, nested inside this flow's own ActionPastDueGraceCancel
			// claim. Without this, a concurrent account deletion (the only other flow that cancels a
			// Paddle subscription) could cancel the SAME subscription at the same instant.
			subErr := claimSideEffectAndRun(ctx, s.claimsRepo, repository.ActionPaddleCancelSubscription, user.ID, paddleCancelSideEffectLease, func() error {
				return s.paddleClient.CancelSubscription(ctx, *user.PaddleSubscriptionID)
			})
			if subErr != nil {
				if errors.Is(subErr, ErrSideEffectClaimBusy) {
					s.logger.Info("grace-cancel Paddle cancel deferred — side-effect claim held by another flow",
						"user_id", user.ID)
					s.releaseClaimOnFailure(ctx, user.ID, resourceID, repository.ActionPastDueGraceCancel, token, subErr.Error())
					return fmt.Errorf("failed to cancel Paddle subscription for past_due user %s: %w", user.ID, subErr)
				}
				if !errors.Is(subErr, ErrPaddleAlreadyCancelled) {
					s.releaseClaimOnFailure(ctx, user.ID, resourceID, repository.ActionPastDueGraceCancel, token, subErr.Error())
					return fmt.Errorf("failed to cancel Paddle subscription for past_due user %s: %w", user.ID, subErr)
				}
				s.logger.Info("Paddle subscription already cancelled, continuing",
					"user_id", user.ID,
					"subscription_id", *user.PaddleSubscriptionID,
				)
			}
		}
	}

	// Steps 3+4 (combined): CAS flip subscription_status → cancelled AND start the 6-month
	// retention clock, in ONE atomic UPDATE.
	//
	// The CAS still only flips if the user is still 'past_due', still uncomped, AND still on the
	// EXACT dunning cycle this flow claimed (past_due_started_at = the timestamp captured at the
	// top of this function) — guarding against a concurrent subscription.activated webhook that
	// restored the user to 'active', a comp granted, or (Issue #1413 round-4 finding H1) a full
	// recover→re-enter-past-due cycle completing between the Plaid/Paddle calls above and this
	// point. This is the final, authoritative correctness check — the claim above provides
	// exclusivity, this predicate still provides correctness.
	//
	// Issue #1413 round-3 finding H5 residual, closed for real: the two writes used to be
	// separate (CancelIfPastDue then SetRetentionDates), so a lost/failed retention write after a
	// successful CAS left the user cancelled with NO clock — and since GetUsersInPastDue only
	// selects subscription_status = 'past_due', tomorrow's run could never re-reach them to
	// retry (they are no longer past_due). CancelIfPastDueOccurrenceWithRetention makes both
	// writes land together or not at all, closing that window on this path entirely. (A
	// belt-and-braces repair query, RepairRetentionClock, also exists in profile_repository.go for
	// any user already stuck in this state from before this fix, or from any other path — see
	// retention_cleanup_check.go.)
	retentionExpires := now.AddDate(0, s.retentionMonths, 0)
	flipped, err := s.repo.CancelIfPastDueOccurrenceWithRetention(ctx, user.ID, *user.PastDueStartedAt, now, retentionExpires)
	if err != nil {
		s.releaseClaimOnFailure(ctx, user.ID, resourceID, repository.ActionPastDueGraceCancel, token, err.Error())
		return fmt.Errorf("CancelIfPastDueOccurrenceWithRetention: %w", err)
	}
	if !flipped {
		// User recovered, was comped, or (H1) a newer dunning cycle already replaced this one
		// during the external calls above — a narrow residual window no guard can close (it
		// cannot be atomic with an external Plaid/Paddle call). Release (never Complete) so a
		// later run can retry once eligible again: Completing here would permanently block
		// reclaiming per the work_claims contract ("a completed claim is never re-granted"),
		// stranding a user who must still be cancelled once a mid-flight comp lapses, or whose
		// newer cycle must still get its OWN eventual grace-cancel evaluation.
		s.releaseClaimOnFailure(ctx, user.ID, resourceID, repository.ActionPastDueGraceCancel, token, "CancelIfPastDueOccurrenceWithRetention predicate lost during external work")
		s.logger.Info("past_due user recovered before cancel deadline, skipping cancellation",
			slog.String("user_id", user.ID))
		return nil
	}

	// Step 5: Clear the past_due_started_at stamp — but ONLY if it still holds the SAME
	// timestamp this flow captured at the top (Issue #1413 round-4 finding H3). By this point the
	// CAS above has already flipped subscription_status to 'cancelled' for THIS cycle, so clearing
	// is best-effort tidying, not correctness-critical for THIS cycle — but an unconditional clear
	// could erase a NEWER cycle's timestamp if the user recovered and re-entered past_due again
	// during the external Plaid/Paddle calls above, which would silently stop that newer cycle's
	// dunning forever (GetUsersInPastDue requires the column non-NULL). A zero-rows/false result
	// here is therefore a normal, expected skip — logged, not escalated — because leaving a newer
	// cycle's stamp untouched is exactly the correct outcome, not a failure.
	cleared, clearErr := s.repo.ClearPastDueStartedAtIfMatches(ctx, user.ID, *user.PastDueStartedAt)
	if clearErr != nil {
		s.logger.Warn("failed to clear past_due_started_at on auto-cancel",
			"user_id", user.ID, "error", clearErr)
	} else if !cleared {
		s.logger.Warn("past_due_started_at had already moved on to a newer cycle when clearing — leaving it untouched so a future run can still process that cycle",
			"user_id", user.ID)
	}

	// Step 6a: Sync Clerk metadata.
	s.syncClerkMetadata(ctx, user, "cancelled")

	// Step 6b: Create in-app notification.
	s.createCancelledNotification(ctx, user)

	if s.analytics != nil {
		s.analytics.Capture(user.ID, phclient.EventPastDueAutoCancelled, nil)
	}

	s.logger.Info("past_due auto-cancel complete",
		"user_id", user.ID,
		"tier", user.SubscriptionTier,
	)

	// Mark the claim durably complete. A false ok (no error) means our lease was reclaimed by
	// another worker while we were doing the work above — that worker may have ALSO run the
	// Plaid/Paddle cleanup and CAS flip concurrently, which is precisely what this mechanism
	// exists to make visible instead of silent.
	completed, err := s.claimsRepo.Complete(ctx, repository.ResourceTypeUser, resourceID, repository.ActionPastDueGraceCancel, token)
	if err != nil {
		s.logger.Error("failed to mark grace-expiry claim complete", "user_id", user.ID, "action", repository.ActionPastDueGraceCancel, "error", err)
	} else if !completed {
		s.logger.Error("grace-expiry claim lease lost mid-flight — Plaid/Paddle cleanup and cancellation may have run twice",
			"user_id", user.ID, "action", repository.ActionPastDueGraceCancel)
	}

	return nil
}

// reReadStillPastDue re-reads the user's current status and returns true only if the user is
// still past_due, uncomped, AND still on the exact dunning cycle the caller claimed. Returns
// false (+ logs) otherwise. Used by both grace-expiry and Plaid-disconnect-window paths to guard
// against concurrent recovery webhooks.
//
// expectedOccurrence is repository.FormatOccurrenceTimestamp(user.PastDueStartedAt) for the
// SAME past_due_started_at snapshot the caller used to build its work_claims resourceID — Issue
// #1413 round-4 finding H1. Status and HasActiveManualOverride() alone are not enough: a batch
// snapshot can capture cycle A, the user can recover and re-enter past_due as cycle B before this
// re-read runs, and cycle B is ALSO 'past_due' with no override — so a stale worker holding cycle
// A's claim would pass both of the older checks and act on cycle B, before cycle B's own grace
// period had actually expired. A mismatch here is a normal, expected skip (not an error): the
// CURRENT cycle is left alone for a future run to evaluate on its own merits.
func (s *PastDueService) reReadStillPastDue(ctx context.Context, userID, expectedOccurrence string) (bool, error) {
	current, err := s.repo.GetUserByID(ctx, userID)
	if err != nil {
		return false, err
	}
	if current.SubscriptionStatus != string(domain.SubscriptionStatusPastDue) {
		s.logger.Info("past_due user recovered before dunning action; skipping",
			"user_id", userID,
			"current_status", current.SubscriptionStatus,
		)
		return false, nil
	}
	// #1413: comp applied mid-run — closes the race between GetUsersInPastDue's batch read and
	// this per-user re-read. Both the grace-expiry and Plaid-disconnect paths call this helper,
	// so this one check closes the race for both.
	if current.HasActiveManualOverride() {
		s.logger.Info("past_due user comped mid-run; skipping destructive action",
			"user_id", userID,
		)
		return false, nil
	}
	// Issue #1413 round-4 finding H1: the row must still be on the EXACT cycle this worker
	// claimed, not merely "some" past_due cycle.
	if currentOccurrence := repository.FormatOccurrenceTimestamp(current.PastDueStartedAt); currentOccurrence != expectedOccurrence {
		s.logger.Info("past_due occurrence changed since this worker's snapshot was taken; skipping stale action",
			"user_id", userID,
		)
		return false, nil
	}
	return true, nil
}

// processPlaidDisconnectWindow handles the pre-emptive Plaid disconnect before grace expiry.
// Issue #1413 High finding H6a: a real work claim on ActionPlaidDisconnectPastDue provides
// EXCLUSIVITY (replacing the deleted ClaimPlaidDisconnectIfPastDue guard); reReadStillPastDue
// immediately after claim wins re-verifies CORRECTNESS against the freshest read.
func (s *PastDueService) processPlaidDisconnectWindow(ctx context.Context, user domain.User) error {
	// Issue #1413 round-4 finding H1: occurrence computed ONCE and threaded into both
	// reReadStillPastDue calls and the claim key below — see processGraceExpiry's matching
	// comment. user.PastDueStartedAt is guaranteed non-nil here: ProcessPastDueUser returns early
	// otherwise.
	occurrence := repository.FormatOccurrenceTimestamp(user.PastDueStartedAt)

	stillPastDue, err := s.reReadStillPastDue(ctx, user.ID, occurrence)
	if err != nil {
		return fmt.Errorf("failed to re-read user %s for Plaid disconnect check: %w", user.ID, err)
	}
	if !stillPastDue {
		return nil
	}

	if s.claimsRepo == nil {
		return fmt.Errorf("processPlaidDisconnectWindow: work claims repository not configured")
	}
	// Issue #1413 P5: same reasoning as processGraceExpiry — the claim key must include WHICH
	// dunning-cycle OCCURRENCE this is, via past_due_started_at, or a completed disconnect in one
	// cycle would permanently block disconnecting again in a later cycle.
	resourceID, err := repository.OccurrenceResourceID(user.ID, occurrence)
	if err != nil {
		return fmt.Errorf("processPlaidDisconnectWindow: %w", err)
	}
	token, claimed, err := s.claimsRepo.Claim(ctx, repository.ResourceTypeUser, resourceID, repository.ActionPlaidDisconnectPastDue, pastDueClaimLease)
	if err != nil {
		return fmt.Errorf("failed to claim Plaid disconnect for user %s: %w", user.ID, err)
	}
	if !claimed {
		// #1413 round-3 finding H3: same deliberate per-caller choice as processGraceExpiry's
		// Claim call above — this collapses "held by another worker right now" and "already
		// completed" into one skip on purpose. GetUsersInPastDue re-runs daily regardless, so
		// both cases self-heal on tomorrow's run without needing ClaimOutcome's finer-grained
		// HeldByOther/AlreadyCompleted split (which the cancellation webhook DOES need, because a
		// webhook delivery does not get a "tomorrow").
		s.logger.Info("past_due Plaid disconnect claim lost — another worker holds it, or it was already completed",
			"user_id", user.ID)
		return nil
	}

	// Re-verify the predicate against the freshest possible read, immediately after winning the
	// claim and before the external Plaid call — already as late as this can be made. Issue
	// #1413 round-4 finding H2: the residual window between this check and the Plaid call itself
	// is documented and accepted, not further closeable here; see processGraceExpiry's matching
	// comment above for the full rationale (recoverable disconnect, re-armed retention clock —
	// inconvenience, not the irreversible-deletion class of harm C1 guards against).
	stillPastDue, err = s.reReadStillPastDue(ctx, user.ID, occurrence)
	if err != nil {
		s.releaseClaimOnFailure(ctx, user.ID, resourceID, repository.ActionPlaidDisconnectPastDue, token, err.Error())
		return fmt.Errorf("failed to re-verify user %s before Plaid disconnect: %w", user.ID, err)
	}
	if !stillPastDue {
		s.releaseClaimOnFailure(ctx, user.ID, resourceID, repository.ActionPlaidDisconnectPastDue, token, "precondition lost since claim")
		s.logger.Info("past_due Plaid disconnect claim lost — user recovered or comped concurrently, skipping",
			"user_id", user.ID)
		return nil
	}

	s.logger.Info("past_due Plaid disconnect window reached, disconnecting",
		"user_id", user.ID,
	)

	if s.plaidCleanup != nil {
		if err := s.plaidCleanup.DisconnectAllUserItems(ctx, user.ID); err != nil {
			// #1413 Codex review round 4: no longer tolerated with a bare log-and-continue — the
			// work claim exists precisely so a failure here can be safely retried (Release makes
			// it immediately reclaimable) instead of silently swallowed with no automatic retry.
			//
			// Issue #1413 H1: ErrSideEffectClaimBusy (another flow holds
			// ActionPlaidDisconnectUser right now) gets the same treatment — logged distinctly at
			// Info since it's expected contention, not a Plaid failure, but still released and
			// propagated so tomorrow's run retries.
			if errors.Is(err, ErrSideEffectClaimBusy) {
				s.logger.Info("past_due Plaid disconnect deferred — side-effect claim held by another flow",
					"user_id", user.ID)
			}
			s.releaseClaimOnFailure(ctx, user.ID, resourceID, repository.ActionPlaidDisconnectPastDue, token, err.Error())
			return fmt.Errorf("failed to disconnect Plaid items in past_due window for user %s: %w", user.ID, err)
		}
		if s.analytics != nil {
			s.analytics.Capture(user.ID, phclient.EventPastDuePlaidDisconnected, nil)
		}
		s.createPlaidDisconnectedNotification(ctx, user)
	}

	completed, err := s.claimsRepo.Complete(ctx, repository.ResourceTypeUser, resourceID, repository.ActionPlaidDisconnectPastDue, token)
	if err != nil {
		s.logger.Error("failed to mark Plaid disconnect claim complete", "user_id", user.ID, "action", repository.ActionPlaidDisconnectPastDue, "error", err)
	} else if !completed {
		s.logger.Error("Plaid disconnect claim lease lost mid-flight — disconnect may have run twice",
			"user_id", user.ID, "action", repository.ActionPlaidDisconnectPastDue)
	}

	return nil
}

// syncClerkMetadata updates the Clerk public metadata for the user's new subscription state.
// Logs and swallows errors so Clerk failures don't abort the dunning flow.
//
// Builds the payload via ClerkPublicMetadata() rather than a hand-rolled map, aligning with
// subscription_webhook.go's syncClerkMetadata (Issue #1413 Defect 3) — this also retroactively
// clears the orphaned "tier" key. status is passed explicitly (not read from user.
// SubscriptionStatus) because this call always races ahead of the DB write that will record
// it. This path is unreachable for comped users after Task 3's comp guard (reReadStillPastDue
// checks HasActiveManualOverride before any destructive action reaches here), so this is
// consistency work, not a live bug fix.
func (s *PastDueService) syncClerkMetadata(ctx context.Context, user domain.User, status string) {
	scratch := domain.User{
		SubscriptionTier:   user.SubscriptionTier,
		SubscriptionStatus: status,
		ManualTierOverride: user.ManualTierOverride,
		ManualTierEndsAt:   user.ManualTierEndsAt,
	}
	if err := s.clerkClient.UpdateUserMetadata(ctx, user.ClerkUserID, scratch.ClerkPublicMetadata()); err != nil {
		s.logger.Error("failed to sync Clerk metadata on past_due auto-cancel",
			"user_id", user.ID, "error", err)
	}
}

// createPlaidDisconnectedNotification notifies the user that bank sync has been paused
// because of an outstanding payment issue, prompting them to update their payment method.
func (s *PastDueService) createPlaidDisconnectedNotification(ctx context.Context, user domain.User) {
	if err := s.notificationRepo.Create(ctx, &domain.Notification{
		ID:                 uuid.New().String(),
		UserID:             user.ID,
		Type:               "subscription_past_due",
		Title:              "Bank Sync Paused",
		Message:            "Your bank accounts have been disconnected pending payment resolution. Update your payment method to restore sync.",
		ActionURL:          ptr("/billing"),
		ActionLabel:        ptr("Update payment method"),
		InteractionPattern: "action_required",
	}); err != nil {
		s.logger.Error("failed to create plaid_disconnected notification for past_due user",
			"user_id", user.ID, "error", err)
	}
}

// createCancelledNotification creates an in-app notification for an auto-cancelled subscription.
// Logs and swallows errors so notification failures don't abort the dunning flow.
func (s *PastDueService) createCancelledNotification(ctx context.Context, user domain.User) {
	if err := s.notificationRepo.Create(ctx, &domain.Notification{
		ID:                 uuid.New().String(),
		UserID:             user.ID,
		Type:               "subscription_cancelled",
		Title:              "Subscription Cancelled",
		Message:            "Your subscription has been cancelled due to non-payment. Resubscribe to restore access.",
		ActionURL:          ptr("/billing"),
		ActionLabel:        ptr("Resubscribe"),
		InteractionPattern: "action_required",
	}); err != nil {
		s.logger.Error("failed to create subscription_cancelled notification",
			"user_id", user.ID, "error", err)
	}
}

// releaseClaimOnFailure releases a work claim and logs any release failure.
// Used to consolidate the repeated Release + Warn pattern in grace-expiry/Plaid-disconnect paths.
//
// userID and resourceID are both taken (rather than deriving one from the other) because, since
// Issue #1413 P5, resourceID is a composite "{userID}:{occurrence discriminator}" built by
// repository.OccurrenceResourceID — userID stays available for clean, greppable log fields while
// resourceID is what actually goes into the Release call.
func (s *PastDueService) releaseClaimOnFailure(ctx context.Context, userID, resourceID, action, token, reason string) {
	if _, releaseErr := s.claimsRepo.Release(ctx, repository.ResourceTypeUser, resourceID, action, token, reason); releaseErr != nil {
		s.logger.Warn("failed to release work claim", "user_id", userID, "resource_id", resourceID, "action", action, "reason", reason, "error", releaseErr)
	}
}

// ptr returns a pointer to v (helper for inline pointer literals).
func ptr[T any](v T) *T {
	return &v
}
