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
	"github.com/maxnussbaum/BoostedBudget/internal/observability"
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

// plaidCleanupMarkerClearMaxAttempts / plaidCleanupMarkerClearRetryDelay mirror
// RetentionCleanupCheckWorker.repairPlaidDisconnects's bounded-retry pattern for the identical
// marker-clear write (queue/jobs/retention_cleanup_check.go's plaidCleanupMarkerClearMaxAttempts/
// plaidCleanupMarkerClearRetryDelay) — Issue #1413 #1453 (Codex review, PR #1443 follow-up): a
// single, swallowed clear attempt here used to leave plaid_cleanup_pending_at stuck non-NULL
// forever on any transient DB error, since this flow's synchronous disconnect already removed the
// live linked item GetUsersNeedingPlaidDisconnectRepair's own predicate requires to ever reselect
// this user again.
//
// Issue #1413 Finding 3 (Codex review, PR #1443 follow-up): through that fix, this comment said
// these were "duplicated here (not imported) rather than shared" because "retention_cleanup_check.go's
// constants are unexported in a different package (queue/jobs vs services)" — true, and exactly the
// drift risk Finding 3 flagged: two independently hand-copied definitions of the same two numbers,
// the identical failure mode that produced this branch's main.go wiring bug elsewhere. `services`
// still cannot import `queue/jobs` (that package already imports `services`; importing back would
// invert the dependency), but both packages already import `repository` — the package that defines
// ClearPlaidCleanupPendingIfMatches itself, the call these retries wrap — so these two names are now
// plain aliases to repository.PlaidCleanupMarkerClearMaxAttempts /
// repository.PlaidCleanupMarkerClearRetryDelay (see that pair's own doc comment, profile_repository.go)
// rather than a second independent literal. Every existing reference in this file (and its tests)
// keeps working unchanged; only the underlying LITERAL moved, to the one place both packages can
// reach without inverting anything.
const (
	plaidCleanupMarkerClearMaxAttempts = repository.PlaidCleanupMarkerClearMaxAttempts
	plaidCleanupMarkerClearRetryDelay  = repository.PlaidCleanupMarkerClearRetryDelay
)

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
	//
	// retentionMonths replaces a Go-computed retentionExpires time.Time (Task NA2, Issue #1413
	// follow-up on PR #1443): retention_expires_at is now computed entirely in Postgres via
	// clock_timestamp() + make_interval(months => retentionMonths), matching SetRetentionDates and
	// AdminUserRepository.RemoveUserTierOverride — Go's time.Time.AddDate normalizes month overflow
	// FORWARD while Postgres's make_interval clamps to the target month's last day, and the Go
	// semantics over-retained personal data relative to a GDPR deletion deadline.
	CancelIfPastDueOccurrenceWithRetention(ctx context.Context, userID string, expectedPastDueStartedAt, retentionStarted time.Time, retentionMonths int) (bool, error)

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

	// ClearPlaidCleanupPendingIfMatches clears plaid_cleanup_pending_at ONLY if it still equals
	// expected — Issue #1413 round-12 Fix 1. processGraceExpiry's ordering (Plaid disconnect FIRST,
	// then the CAS flip that WRITES this marker) means that by the time the flip succeeds, a
	// successful synchronous disconnect has ALREADY completed cleanup for this episode — the marker
	// the flip just wrote would otherwise start life already stale. The CAS guard (not the
	// unconditional ClearPlaidCleanupPending) matters for the same reason it matters everywhere else
	// this pattern appears: a brand-new episode's marker must survive untouched if one somehow starts
	// between the flip and this call.
	ClearPlaidCleanupPendingIfMatches(ctx context.Context, userID string, expected time.Time) (bool, error)
}

// PastDuePlaidCleanup defines the Plaid cleanup operations needed by PastDueService.
type PastDuePlaidCleanup interface {
	// DisconnectAllUserItemsIfEligible calls Plaid /item/remove for all of the user's items then
	// marks them as 'disconnected' (preserving transaction history), invoking recheck immediately
	// before EVERY remaining item. The first (false, nil) stops the loop without attempting any
	// further item and reports ErrCleanupAborted. See
	// PlaidItemCleanupService.DisconnectAllUserItemsIfEligible for the full contract.
	//
	// THERE IS DELIBERATELY NO nil-RECHECK COUNTERPART ON THIS INTERFACE (Issue #1500). A
	// DisconnectAllUserItems used to sit here, and processPlaidDisconnectWindow was its last caller
	// in this service; the pair is gone rather than merely unused, because the failure mode being
	// closed is SILENCE — while a recheck-free entry point exists, a future caller gets no per-item
	// recheck by default and nothing complains. A disconnect is one Plaid call PER LINKED ITEM, so
	// a single read taken before the loop leaves every item after the first exposed to the user
	// paying their invoice mid-loop. Both callers in this service now re-ask reReadStillPastDue.
	DisconnectAllUserItemsIfEligible(ctx context.Context, userID string, recheck func(ctx context.Context) (bool, error)) error
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
// #1413 — mirrors the deleted TrialService's SetHouseholdChecker convention. Nil-safe: an unset checker preserves
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
	// pure-and-cheap-first reason as the legacy trial-expiry flow (deleted in #1652).
	if user.HasActiveManualOverride() {
		s.logger.Info("skipping past_due processing for actively comped user", "user_id", user.ID)
		return nil
	}

	// #1413: closes an asymmetry with the legacy trial flow (deleted in #1652) — exempt actively covered household
	// members. Fail closed on an uncertain check (skip processing this run) but PROPAGATE the
	// error rather than swallowing it — see the legacy trial service (deleted in #1652) for why swallowing would
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
	//
	// stillEligible is the PER-ITEM recheck, and passing it is the point (Codex pass-5 Medium 1).
	// This call site passed DisconnectAllUserItems — whose recheck parameter is nil — until now, so
	// the re-verification directly above covered the whole loop rather than each item in it: a
	// disconnect is one Plaid /item/remove per linked item, so a user who paid their invoice after
	// the first item was removed still had every remaining bank connection removed. This is the
	// matched sibling of the same fix already made for RemoveAllUserItems (deletion_service.go) —
	// the same cleanupItems parameter, one caller over.
	//
	// It wraps reReadStillPastDue rather than introducing a fourth notion of "still past due", so
	// none of this flow's re-verifications can disagree about what the predicate means. The
	// residual TOCTOU between the last recheck and the Plaid call it precedes is unchanged and
	// still accepted — see the H2 note above.
	if s.plaidCleanup != nil {
		stillEligible := func(rctx context.Context) (bool, error) {
			return s.reReadStillPastDue(rctx, user.ID, occurrence)
		}
		if err := s.plaidCleanup.DisconnectAllUserItemsIfEligible(ctx, user.ID, stillEligible); err != nil {
			// A recovery-triggered abort is not a Plaid failure and must not be reported as one.
			// ErrCleanupAborted has TWO causes (see its doc comment) and they demand opposite
			// handling here, so the predicate is asked once more to say which: the user is no longer
			// past due (a legitimate stop — release and return nil, WITHOUT cancelling their
			// subscription, exactly as the Step-2 re-verify below would have) versus a new linked
			// item having appeared mid-pass (genuinely incomplete work — falls through to the error
			// path, so tomorrow's run retries). An error from the recheck itself falls through too,
			// which is the fail-closed direction.
			if errors.Is(err, ErrCleanupAborted) {
				if stillPastDue, checkErr := stillEligible(ctx); checkErr == nil && !stillPastDue {
					s.releaseClaimOnFailure(ctx, user.ID, resourceID, repository.ActionPastDueGraceCancel, token, "precondition lost mid Plaid disconnect — remaining items left connected")
					s.logger.Info("past_due user recovered or was comped partway through the Plaid disconnect; the remaining items stay connected and their subscription is NOT being cancelled",
						"user_id", user.ID)
					return nil
				}
			}
			// Issue #1413 H1: the disconnect can return ErrSideEffectClaimBusy when
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

	// RE-VERIFY AGAIN, BECAUSE STEP 1 WAS A NETWORK ROUND-TRIP. The re-check above is immediately
	// before the Plaid call and is correct for that call, but Step 2 is a SECOND irreversible external
	// call performed after Step 1 returns — and nothing used to re-verify in between. A user who paid
	// their invoice while we were disconnecting their banks still had their subscription cancelled at
	// Paddle, after which Step 3's CAS correctly refused to flip their local status: a paid-up customer
	// with no subscription at Paddle and a local row saying nothing happened.
	//
	// This is the same defect shape as ExecuteDeletion's (a precondition checked at selection, work
	// performed later, nothing re-checking in between) and it is fixed the same way — by asking again
	// at the moment of use. It reuses reReadStillPastDue rather than introducing a second predicate, so
	// the three re-verifications in this flow cannot disagree about what "still past due" means.
	//
	// Aborting HERE leaves the Plaid items disconnected. That is accepted for the reason the H2
	// residual note above already gives: a disconnect is recoverable (the user relinks), the retention
	// clock is not started (Step 3 never runs), and the alternative — cancelling a recovered customer's
	// subscription — is worse. Ordering already puts the recoverable call first; see
	// deletion_service.go's ordering note for the same argument made explicitly.
	stillPastDue, err = s.reReadStillPastDue(ctx, user.ID, occurrence)
	if err != nil {
		// Fail closed: not knowing whether the user is still past due is not permission to cancel
		// their subscription.
		s.releaseClaimOnFailure(ctx, user.ID, resourceID, repository.ActionPastDueGraceCancel, token, err.Error())
		return fmt.Errorf("failed to re-verify user %s before cancelling their Paddle subscription: %w", user.ID, err)
	}
	if !stillPastDue {
		s.releaseClaimOnFailure(ctx, user.ID, resourceID, repository.ActionPastDueGraceCancel, token, "precondition lost during Plaid disconnect — not cancelling the subscription")
		s.logger.Info("past_due user recovered or was comped during the Plaid disconnect; their subscription is NOT being cancelled and their status is left alone",
			"user_id", user.ID)
		return nil
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
	//
	// Task NA2 (Issue #1413 follow-up on PR #1443): retention_expires_at used to be computed here
	// via now.AddDate(0, s.retentionMonths, 0) — Go's month arithmetic normalizes overflow FORWARD,
	// disagreeing with the Postgres make_interval semantics every other retention_expires_at writer
	// now uses. s.retentionMonths is passed straight through so
	// CancelIfPastDueOccurrenceWithRetention can compute the deadline entirely in Postgres instead.
	flipped, err := s.repo.CancelIfPastDueOccurrenceWithRetention(ctx, user.ID, *user.PastDueStartedAt, now, s.retentionMonths)
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

	// Issue #1413 round-12 Fix 1 (Blocker): the marker CancelIfPastDueOccurrenceWithRetention just
	// wrote (via its own COALESCE) describes cleanup as outstanding — but Step 1 above already
	// disconnected Plaid FIRST, before this flip ever ran. If that disconnect actually happened and
	// succeeded (s.plaidCleanup != nil; a failed disconnect already returned above, before reaching
	// this point), cleanup for THIS episode is complete the instant the marker is born, and per the
	// invariant (domain.User.PlaidCleanupPendingAt's doc comment) it must go back to NULL rather than
	// starting life already stale. Unlike the cancellation-webhook path (Plaid disconnect AFTER the
	// status/marker write), this flow's ordering means "clear immediately after the flip" is
	// sufficient — there is no window where the marker is ever legitimately observable as "pending"
	// for a disconnect that already finished; changing Step 1/3-4's relative order would be a larger,
	// riskier change for no additional correctness benefit here.
	//
	// Re-reads the row via GetUserByID (not a return value from the CAS call, which reports only
	// success/failure) to obtain the exact marker value THIS flip just wrote, then clears it via the
	// CAS-guarded ClearPlaidCleanupPendingIfMatches — never the unconditional ClearPlaidCleanupPending
	// — so a brand-new episode's marker (e.g. a comp granted then removed in the narrow window since
	// the flip) survives untouched. Losing this write is not correctness-critical for THIS cycle in
	// isolation — GetUsersNeedingPlaidDisconnectRepair requires a LIVE linked item, and none remain
	// right now — but leaving it set FOREVER is real phantom residue (#1453), and Issue #1413 Finding 1
	// (Codex review, PR #1443 follow-up, headline finding) is the correction that matters here: this
	// comment used to call that residue "inert". It is not. At the time this was found, a comp
	// granted and later removed over that stale marker resurrected it via
	// removeUserTierOverrideUpdate's COALESCE (admin_user_repository.go) instead of starting a fresh
	// episode, so a bank the user relinks in the meantime inherited an already-expired grace window
	// and got disconnected with zero notice the next repairPlaidDisconnects pass. ROUND 18
	// (admin_user_repository.go) independently removed that COALESCE — a comp granted and removed
	// over this exact stuck marker no longer resurrects it either way — but a stuck marker on a user
	// no comp is EVER granted over gets no help from that fix. RetentionCleanupCheckWorker's
	// self-healing backstop (ClearCompletedPlaidCleanupMarkers, profile_repository.go) is what
	// actually closes THAT remaining population within one backstop interval — not the residue being
	// harmless on its own — since the disconnect that just succeeded removed the very live item that
	// predicate requires, GetUsersNeedingPlaidDisconnectRepair
	// can never reselect this user again to retry the clear either. Issue #1413 #1453 (Codex review, PR #1443 follow-up): a single,
	// swallowed clear attempt used to leave the marker stuck non-NULL on any transient DB error at
	// exactly this moment — bring this into line with the identical fix already applied to
	// RetentionCleanupCheckWorker.repairPlaidDisconnects's own marker clear (retention_cleanup_check.go):
	// retry up to plaidCleanupMarkerClearMaxAttempts times, then Error-log (not Warn) plus the shared
	// failure counter if every attempt still fails. Still not escalated to fail THIS cycle's overall
	// result — this flow's own cancellation already succeeded regardless of whether the marker clears.
	if s.plaidCleanup != nil {
		if freshUser, reErr := s.repo.GetUserByID(ctx, user.ID); reErr != nil {
			s.logger.Warn("failed to re-read user before clearing plaid_cleanup_pending_at after grace-expiry disconnect",
				"user_id", user.ID, "error", reErr)
		} else if freshUser.PlaidCleanupPendingAt != nil {
			expectedMarker := *freshUser.PlaidCleanupPendingAt
			var cleared bool
			var clearErr error
			for attempt := 1; attempt <= plaidCleanupMarkerClearMaxAttempts; attempt++ {
				cleared, clearErr = s.repo.ClearPlaidCleanupPendingIfMatches(ctx, user.ID, expectedMarker)
				if clearErr == nil {
					break
				}
				if attempt < plaidCleanupMarkerClearMaxAttempts {
					s.logger.Warn("failed to clear plaid_cleanup_pending_at after grace-expiry disconnect — retrying",
						"user_id", user.ID, "attempt", attempt, "error", clearErr)
					time.Sleep(plaidCleanupMarkerClearRetryDelay)
				}
			}
			if clearErr != nil {
				s.logger.Error("Plaid disconnect succeeded but failed to clear plaid_cleanup_pending_at after all retries — this user's Plaid items are already disconnected, so GetUsersNeedingPlaidDisconnectRepair's live-item requirement will never match them again; the marker may now be stuck non-NULL permanently unless cleared through another path",
					"user_id", user.ID, "attempts", plaidCleanupMarkerClearMaxAttempts, "error", clearErr)
				observability.PlaidDisconnectRepairUserFailuresTotal.Inc()
			} else if !cleared {
				s.logger.Info("plaid_cleanup_pending_at had already moved on to a new episode when clearing after grace-expiry disconnect — leaving the new episode's marker untouched",
					"user_id", user.ID)
			}
		}
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
		// stillEligible is the PER-ITEM recheck (Issue #1500). This call site passed the nil-recheck
		// entry point until now — a KNOWN outstanding instance of the same pattern the grace-cancel
		// path above fixed first — so the two re-verifications above covered the whole loop rather
		// than each item in it: a disconnect is one Plaid /item/remove per linked item, so a user who
		// paid their invoice after the first item was removed still lost every remaining bank
		// connection. Nothing irreversible follows in THIS flow (no Paddle cancellation, no retention
		// clock), which is why the finding was rated Medium and did not block a merge; it is closed
		// anyway because the nil-recheck entry point it depended on no longer exists at all.
		//
		// It wraps reReadStillPastDue — the same predicate this flow's other two checks use, and the
		// same one processGraceExpiry re-asks — rather than introducing another notion of "still past
		// due". The residual TOCTOU between the last recheck and the Plaid call it precedes is
		// unchanged and still accepted; see the H2 note above.
		stillEligible := func(rctx context.Context) (bool, error) {
			return s.reReadStillPastDue(rctx, user.ID, occurrence)
		}
		if err := s.plaidCleanup.DisconnectAllUserItemsIfEligible(ctx, user.ID, stillEligible); err != nil {
			// A recovery-triggered abort is not a Plaid failure and must not be reported as one.
			// ErrCleanupAborted has TWO causes (see its doc comment) and they demand opposite
			// handling, so the predicate is asked once more to say which: the user is no longer past
			// due (a legitimate stop — release the claim and return nil, WITHOUT the downstream
			// analytics event and "we disconnected your banks" notification, which would be false for
			// a recovered user) versus a new linked item having appeared mid-pass (genuinely
			// incomplete work — falls through to the error path, so tomorrow's run retries). An error
			// from the recheck itself falls through too, which is the fail-closed direction. This is
			// the same shape processGraceExpiry and ExecuteDeletion already use, not a new one.
			if errors.Is(err, ErrCleanupAborted) {
				if stillPastDue, checkErr := stillEligible(ctx); checkErr == nil && !stillPastDue {
					s.releaseClaimOnFailure(ctx, user.ID, resourceID, repository.ActionPlaidDisconnectPastDue, token, "precondition lost mid Plaid disconnect — remaining items left connected")
					s.logger.Info("past_due user recovered or was comped partway through the pre-emptive Plaid disconnect; the remaining items stay connected",
						"user_id", user.ID)
					return nil
				}
			}
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
