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

	// CancelIfPastDue atomically flips subscription_status to 'cancelled' only if the user is
	// still 'past_due' AND carries no currently-active manual tier override. Returns (true, nil)
	// if the flip happened, (false, nil) if the user has already recovered (payment arrived
	// between Plaid disconnect and this point) or an admin granted/extended a comp in that same
	// window (Issue #1413 High finding H6, Codex review round 3: predicate extended to also
	// close the comp race).
	CancelIfPastDue(ctx context.Context, userID string) (bool, error)

	// SetRetentionDates starts the 6-month retention clock.
	SetRetentionDates(ctx context.Context, userID string, started, expires time.Time) error

	// ClearPastDueStartedAt clears the past_due_started_at timestamp after auto-cancel.
	ClearPastDueStartedAt(ctx context.Context, userID string) error
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
	// Cheap early check (mirrors ExecuteDeletion's belt-and-braces short-circuit): avoids
	// claiming at all in the common already-recovered/already-comped case. NOT the source of
	// correctness — the re-verify immediately after the claim below is.
	if stillPastDue, err := s.reReadStillPastDue(ctx, user.ID); err != nil {
		return fmt.Errorf("failed to re-read user %s before grace expiry: %w", user.ID, err)
	} else if !stillPastDue {
		return nil
	}

	if s.claimsRepo == nil {
		return fmt.Errorf("processGraceExpiry: work claims repository not configured")
	}
	token, claimed, err := s.claimsRepo.Claim(ctx, repository.ResourceTypeUser, user.ID, repository.ActionPastDueGraceCancel, pastDueClaimLease)
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
	// claim and before any irreversible external work.
	stillPastDue, err := s.reReadStillPastDue(ctx, user.ID)
	if err != nil {
		s.releaseClaimOnFailure(ctx, user.ID, repository.ActionPastDueGraceCancel, token, err.Error())
		return fmt.Errorf("failed to re-verify user %s before grace expiry: %w", user.ID, err)
	}
	if !stillPastDue {
		s.releaseClaimOnFailure(ctx, user.ID, repository.ActionPastDueGraceCancel, token, "precondition lost since claim")
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
			s.releaseClaimOnFailure(ctx, user.ID, repository.ActionPastDueGraceCancel, token, err.Error())
			return fmt.Errorf("failed to disconnect Plaid items for past_due user %s: %w", user.ID, err)
		}
	}

	// Step 2: Cancel Paddle subscription.
	// Nil-guard: paddleClient is nil when PADDLE_API_KEY is unset (mirrors plaidCleanup convention).
	// When Paddle is unconfigured, log a warning and skip — the subscription will eventually be
	// reconciled via Paddle's own dunning or a future webhook, but ops should be aware.
	// Tolerate already-cancelled — the subscription may have been cancelled by Paddle's dunning
	// before our job ran.
	if s.paddleClient == nil && user.PaddleSubscriptionID != nil {
		s.logger.Warn("skipping Paddle cancel in grace expiry: paddleClient not configured",
			"user_id", user.ID,
			"subscription_id", *user.PaddleSubscriptionID,
		)
	}
	if s.paddleClient != nil && user.PaddleSubscriptionID != nil {
		if err := s.paddleClient.CancelSubscription(ctx, *user.PaddleSubscriptionID); err != nil {
			if !errors.Is(err, ErrPaddleAlreadyCancelled) {
				s.releaseClaimOnFailure(ctx, user.ID, repository.ActionPastDueGraceCancel, token, err.Error())
				return fmt.Errorf("failed to cancel Paddle subscription for past_due user %s: %w", user.ID, err)
			}
			s.logger.Info("Paddle subscription already cancelled, continuing",
				"user_id", user.ID,
				"subscription_id", *user.PaddleSubscriptionID,
			)
		}
	}

	// Step 3: CAS flip subscription_status → cancelled.
	// Only flips if the user is still 'past_due' — guards against a concurrent
	// subscription.activated webhook that may have restored the user to 'active', or a comp
	// granted, between the Plaid/Paddle calls above and this point. This is the final,
	// authoritative correctness check (kept exactly as before) — the claim above provides
	// exclusivity, this predicate still provides correctness.
	flipped, err := s.repo.CancelIfPastDue(ctx, user.ID)
	if err != nil {
		s.releaseClaimOnFailure(ctx, user.ID, repository.ActionPastDueGraceCancel, token, err.Error())
		return fmt.Errorf("CancelIfPastDue: %w", err)
	}
	if !flipped {
		// User recovered, or was comped, during the external calls above — a narrow residual
		// window no guard can close (it cannot be atomic with an external Plaid/Paddle call).
		// Release (never Complete) so a later run can retry once eligible again: Completing here
		// would permanently block reclaiming per the work_claims contract ("a completed claim is
		// never re-granted"), stranding a user who must still be cancelled once a mid-flight comp
		// lapses.
		s.releaseClaimOnFailure(ctx, user.ID, repository.ActionPastDueGraceCancel, token, "CancelIfPastDue predicate lost during external work")
		s.logger.Info("past_due user recovered before cancel deadline, skipping cancellation",
			slog.String("user_id", user.ID))
		return nil
	}

	// Step 4: Start 6-month retention clock.
	retentionExpires := now.AddDate(0, s.retentionMonths, 0)
	// #1413 round-3 finding H5: this write is REQUIRED, not best-effort. Both the retention-
	// notice jobs and hard deletion require retention_expires_at IS NOT NULL — swallowing this
	// error left the user cancelled (Step 3's CAS already committed) with no clock: never warned,
	// never deleted, indefinitely.
	//
	// Known residual (documented per the #1413 design doc rather than silently accepted): unlike
	// the cancellation webhook — where a retry naturally re-reaches this same write because
	// nothing upstream is gated on subscription_status — Release + return-error here does NOT
	// get retried by the next scheduled run. GetUsersInPastDue filters on
	// subscription_status = 'past_due', and Step 3 already flipped this user to 'cancelled', so
	// tomorrow's run will never select them again. Making the CAS and this write a single atomic
	// transaction would close that gap, but doing so needs a new PastDueProfileRepository method
	// (e.g. CancelIfPastDueWithRetention) implemented in profile_repository.go, which is outside
	// this fix's scope — as would a periodic repair query for cancelled users with a NULL
	// retention_expires_at. Returning the error at least makes this failure LOUD (job failure,
	// dead-letter, alerting) instead of the prior silent swallow; closing the gap for good is a
	// follow-up.
	if retErr := s.repo.SetRetentionDates(ctx, user.ID, now, retentionExpires); retErr != nil {
		s.releaseClaimOnFailure(ctx, user.ID, repository.ActionPastDueGraceCancel, token, retErr.Error())
		return fmt.Errorf("failed to set retention dates for past_due user %s: %w", user.ID, retErr)
	}

	// Step 5: Clear the past_due_started_at stamp.
	if clearErr := s.repo.ClearPastDueStartedAt(ctx, user.ID); clearErr != nil {
		s.logger.Warn("failed to clear past_due_started_at on auto-cancel",
			"user_id", user.ID, "error", clearErr)
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
	completed, err := s.claimsRepo.Complete(ctx, repository.ResourceTypeUser, user.ID, repository.ActionPastDueGraceCancel, token)
	if err != nil {
		s.logger.Error("failed to mark grace-expiry claim complete", "user_id", user.ID, "action", repository.ActionPastDueGraceCancel, "error", err)
	} else if !completed {
		s.logger.Error("grace-expiry claim lease lost mid-flight — Plaid/Paddle cleanup and cancellation may have run twice",
			"user_id", user.ID, "action", repository.ActionPastDueGraceCancel)
	}

	return nil
}

// reReadStillPastDue re-reads the user's current status and returns true only if the user is
// still past_due. Returns false (+ logs) if the user recovered. Used by both grace-expiry and
// Plaid-disconnect-window paths to guard against concurrent recovery webhooks.
func (s *PastDueService) reReadStillPastDue(ctx context.Context, userID string) (bool, error) {
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
	return true, nil
}

// processPlaidDisconnectWindow handles the pre-emptive Plaid disconnect before grace expiry.
// Issue #1413 High finding H6a: a real work claim on ActionPlaidDisconnectPastDue provides
// EXCLUSIVITY (replacing the deleted ClaimPlaidDisconnectIfPastDue guard); reReadStillPastDue
// immediately after claim wins re-verifies CORRECTNESS against the freshest read.
func (s *PastDueService) processPlaidDisconnectWindow(ctx context.Context, user domain.User) error {
	stillPastDue, err := s.reReadStillPastDue(ctx, user.ID)
	if err != nil {
		return fmt.Errorf("failed to re-read user %s for Plaid disconnect check: %w", user.ID, err)
	}
	if !stillPastDue {
		return nil
	}

	if s.claimsRepo == nil {
		return fmt.Errorf("processPlaidDisconnectWindow: work claims repository not configured")
	}
	token, claimed, err := s.claimsRepo.Claim(ctx, repository.ResourceTypeUser, user.ID, repository.ActionPlaidDisconnectPastDue, pastDueClaimLease)
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
	// claim and before the external Plaid call.
	stillPastDue, err = s.reReadStillPastDue(ctx, user.ID)
	if err != nil {
		s.releaseClaimOnFailure(ctx, user.ID, repository.ActionPlaidDisconnectPastDue, token, err.Error())
		return fmt.Errorf("failed to re-verify user %s before Plaid disconnect: %w", user.ID, err)
	}
	if !stillPastDue {
		s.releaseClaimOnFailure(ctx, user.ID, repository.ActionPlaidDisconnectPastDue, token, "precondition lost since claim")
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
			s.releaseClaimOnFailure(ctx, user.ID, repository.ActionPlaidDisconnectPastDue, token, err.Error())
			return fmt.Errorf("failed to disconnect Plaid items in past_due window for user %s: %w", user.ID, err)
		}
		if s.analytics != nil {
			s.analytics.Capture(user.ID, phclient.EventPastDuePlaidDisconnected, nil)
		}
		s.createPlaidDisconnectedNotification(ctx, user)
	}

	completed, err := s.claimsRepo.Complete(ctx, repository.ResourceTypeUser, user.ID, repository.ActionPlaidDisconnectPastDue, token)
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
func (s *PastDueService) releaseClaimOnFailure(ctx context.Context, userID, action, token, reason string) {
	if _, releaseErr := s.claimsRepo.Release(ctx, repository.ResourceTypeUser, userID, action, token, reason); releaseErr != nil {
		s.logger.Warn("failed to release work claim", "user_id", userID, "action", action, "reason", reason, "error", releaseErr)
	}
}

// ptr returns a pointer to v (helper for inline pointer literals).
func ptr[T any](v T) *T {
	return &v
}
