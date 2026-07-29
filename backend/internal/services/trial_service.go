// Package services provides business logic and external service integrations.
// This file implements TrialService for managing user trials.
// Issue #679: Implement trial service and auto-grant logic
package services

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/maxnussbaum/BoostedBudget/internal/config"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/repository"
)

// TrialRepository defines the repository methods needed by TrialService
type TrialRepository interface {
	// GetUserByID retrieves a user by their internal UUID
	GetUserByID(ctx context.Context, id string) (*domain.User, error)

	// UpdateTrialStatus updates the trial-related fields for a user
	UpdateTrialStatus(ctx context.Context, userID string, status string, trialStartedAt, trialEndsAt *time.Time, hadTrial bool) error

	// ExpireTrialIfNoOverride atomically flips subscription_status to 'inactive' only if the
	// user is still 'trial' AND carries no currently-active manual tier override. Returns
	// (true, nil) if the flip happened, (false, nil) if an admin granted/extended a comp between
	// ExpireTrial's earlier HasActiveManualOverride() guard and this call — a legitimate skip,
	// not an error (Issue #1413 High finding H5, Codex review round 3).
	ExpireTrialIfNoOverride(ctx context.Context, userID string) (bool, error)

	// GetUsersWithExpiredTrials returns users with trial status but past trial_ends_at
	GetUsersWithExpiredTrials(ctx context.Context) ([]*domain.User, error)

	// GetUsersWithExpiringTrials returns users whose trial ends within N days
	GetUsersWithExpiringTrials(ctx context.Context, withinDays int) ([]*domain.User, error)

	// SetRetentionDates starts the retention clock (#930)
	SetRetentionDates(ctx context.Context, userID string, startedAt, expiresAt time.Time) error
}

// PlaidCleanupForTrial defines the Plaid cleanup methods needed by TrialService
type PlaidCleanupForTrial interface {
	// DisconnectAllUserItems calls Plaid /item/remove for all items
	// then marks them as 'disconnected' (preserves transaction history)
	DisconnectAllUserItems(ctx context.Context, userID string) error
}

// trialExpirationClaimLease is the lease duration requested for ActionTrialExpiration — matches
// ExecuteDeletion's accountDeletionClaimLease and PastDueService's pastDueClaimLease (Issue
// #1413 round-4 finding H2). Generous beats tight, and the fencing token
// (repository.WorkClaimsRepository) makes a lease over-run detectable rather than silently
// re-granted to a second worker.
const trialExpirationClaimLease = 15 * time.Minute

// householdCoverageCheckerForTrial is the narrow capability TrialService needs to
// detect an active household-sourced entitlement, so the daily trial-expiry job
// doesn't flip a covered member to inactive and disconnect their Plaid items despite
// being actively covered (#1367 — Phase 3). Satisfied structurally by
// *HouseholdEntitlementService.
type householdCoverageCheckerForTrial interface {
	IsCoveredByHousehold(ctx context.Context, userID string) (bool, error)
}

// TrialService handles trial management for users
// Issue #679: Implement trial service and auto-grant logic
type TrialService struct {
	tierConfig       *config.TierConfig
	repo             TrialRepository
	plaidCleanup     PlaidCleanupForTrial
	householdChecker householdCoverageCheckerForTrial // #1367 — Phase 3: nil skips the exemption
	claimsRepo       repository.WorkClaimsRepository  // #1413 round-4 H2: required, see SetWorkClaimsRepo
	retentionMonths  int                              // #930: Months before auto-deletion after trial expiry
	logger           *slog.Logger
}

// NewTrialService creates a new TrialService
func NewTrialService(
	tierConfig *config.TierConfig,
	repo TrialRepository,
	plaidCleanup PlaidCleanupForTrial,
) *TrialService {
	return &TrialService{
		tierConfig:      tierConfig,
		repo:            repo,
		plaidCleanup:    plaidCleanup,
		retentionMonths: 6,
		logger:          slog.Default(),
	}
}

// SetHouseholdChecker wires the household-coverage exemption for trial expiry.
// #1367 — Phase 3. Nil-safe: an unset checker preserves pre-#1367 behavior.
func (s *TrialService) SetHouseholdChecker(c householdCoverageCheckerForTrial) {
	s.householdChecker = c
}

// SetRetentionMonths configures the retention period (default: 6).
// #930: Called from main.go to propagate RetentionPeriodMonths config.
func (s *TrialService) SetRetentionMonths(months int) {
	if months > 0 {
		s.retentionMonths = months
	}
}

// SetPlaidCleanup injects the Plaid cleanup dependency after construction.
// Issue #1015: Allows TrialService to be created before PlaidCleanupService is available,
// enabling River initialization to run independently of Plaid configuration.
// ExpireTrial already nil-guards plaidCleanup before calling it.
func (s *TrialService) SetPlaidCleanup(svc PlaidCleanupForTrial) {
	s.plaidCleanup = svc
}

// SetWorkClaimsRepo wires the durable work-claims mechanism ExpireTrial requires for
// EXCLUSIVITY on ActionTrialExpiration before its Plaid disconnect. Issue #1413 round-4 finding
// H2: trial_service was the one destructive flow in this branch with no work_claims flow claim
// at all — every sibling flow (DeletionService, PastDueService, SubscriptionWebhookWorker,
// PlaidItemCleanupService) already requires one. Mirrors those services' SetWorkClaimsRepo: not
// an optional capability — a nil claims repo makes ExpireTrial fail loudly rather than silently
// falling back to CAS-only behavior.
func (s *TrialService) SetWorkClaimsRepo(repo repository.WorkClaimsRepository) {
	s.claimsRepo = repo
}

// GrantTrial grants a trial to a user
// Sets subscription_status='trial', subscription_tier='premium',
// trial_started_at=now, trial_ends_at=now+60days, had_trial=true
//
// Returns error if user has already had a trial
func (s *TrialService) GrantTrial(ctx context.Context, userID string) error {
	// Get the user to check if they've already had a trial
	user, err := s.repo.GetUserByID(ctx, userID)
	if err != nil {
		return fmt.Errorf("failed to get user: %w", err)
	}

	// Check if user can receive a trial
	if !s.CanGrantTrial(user) {
		return fmt.Errorf("user %s has already had a trial", userID)
	}

	// Get trial duration from config
	trialConfig := s.tierConfig.GetTrialConfig()
	trialDurationDays := trialConfig.DurationDays
	if trialDurationDays <= 0 {
		trialDurationDays = 60 // Default to 60 days
	}

	// Calculate trial dates
	now := time.Now()
	trialEndsAt := now.Add(time.Duration(trialDurationDays) * 24 * time.Hour)

	// Update user with trial status
	err = s.repo.UpdateTrialStatus(ctx, userID, string(domain.SubscriptionStatusTrial), &now, &trialEndsAt, true)
	if err != nil {
		return fmt.Errorf("failed to update trial status: %w", err)
	}

	s.logger.Info("granted trial to user",
		"user_id", userID,
		"trial_ends_at", trialEndsAt,
		"duration_days", trialDurationDays,
	)

	return nil
}

// IsTrialActive returns true if the user's trial is currently active
// (subscription_status='trial' AND trial_ends_at > now)
func (s *TrialService) IsTrialActive(user *domain.User) bool {
	if user == nil {
		return false
	}

	// Check subscription status is trial
	if user.SubscriptionStatus != string(domain.SubscriptionStatusTrial) {
		return false
	}

	// Check trial hasn't expired
	if user.TrialEndsAt == nil {
		return false
	}

	return user.TrialEndsAt.After(time.Now())
}

// GetTrialDaysRemaining returns the number of days remaining in the user's trial
// Returns 0 if trial has expired or user is not on trial
func (s *TrialService) GetTrialDaysRemaining(user *domain.User) int {
	if user == nil || user.TrialEndsAt == nil {
		return 0
	}

	remaining := time.Until(*user.TrialEndsAt)
	if remaining <= 0 {
		return 0
	}

	// Convert to days (round down)
	return int(remaining.Hours() / 24)
}

// ExpireTrial expires a user's trial
// Sets subscription_status='inactive' and disconnects all Plaid items
//
// This preserves transaction history while stopping Plaid billing
func (s *TrialService) ExpireTrial(ctx context.Context, userID string) error {
	// Get the user
	user, err := s.repo.GetUserByID(ctx, userID)
	if err != nil {
		return fmt.Errorf("failed to get user: %w", err)
	}

	// #1413: an active manual override ("comp") grants access and must not be undermined by
	// the daily trial-expiry job — a comped user can still carry subscription_status='trial'
	// with a past TrialEndsAt, but ComputeEffectiveStatus forces their effective status to
	// active, so expiring them here would silently revoke what the comp granted. Checked
	// before the household-coverage lookup below: HasActiveManualOverride is pure and cannot
	// fail, so there's no reason to pay for a household DB round-trip first. Accepted cost
	// (already ratified): a permanently comped user is re-selected and re-skipped by this job
	// every run — volume is a handful of rows.
	if user.HasActiveManualOverride() {
		s.logger.Info("skipping trial expiry for actively comped user", "user_id", userID)
		return nil
	}

	// #1367 — Phase 3: exempt actively covered household members. Fail closed on an
	// uncertain check (skip expiry this run) rather than risk wrongly locking out an
	// actually-covered member — the daily job retries, so this self-heals.
	if s.householdChecker != nil {
		covered, err := s.householdChecker.IsCoveredByHousehold(ctx, userID)
		if err != nil {
			// Skip the destructive mutation (fail closed on uncertain coverage) but
			// PROPAGATE the error — swallowing it would make the daily job log a false
			// success for a user whose trial was never actually processed, hiding an
			// operational failure and, if the check stays broken, indefinitely
			// un-expiring trials with no visible signal (Codex review, Medium finding 6).
			s.logger.Warn("failed to check household coverage during trial expiry; skipping this run",
				"user_id", userID, "error", err)
			return fmt.Errorf("failed to check household coverage: %w", err)
		}
		if covered {
			s.logger.Info("skipping trial expiry for actively covered household member", "user_id", userID)
			return nil
		}
	}

	s.logger.Info("expiring trial for user",
		"user_id", userID,
		"previous_status", user.SubscriptionStatus,
	)

	// Issue #1413 round-4 finding H2: gain EXCLUSIVITY via a durable work claim BEFORE any
	// destructive work — mirrors PastDueService.processGraceExpiry's shape (Claim, then
	// re-verify via a CAS, then external work, then Complete/Release). ExpireTrialIfNoOverride's
	// own conditional UPDATE already gives this write its correctness against a comp granted in
	// the window since the plain read above; this claim adds the same EXCLUSIVITY guarantee
	// every other destructive flow in this branch already has, and — the practical benefit here
	// — turns the Plaid-disconnect collaborator call below into something a lost claim can
	// legitimately skip, rather than one that always runs regardless.
	if s.claimsRepo == nil {
		return fmt.Errorf("ExpireTrial: work claims repository not configured")
	}
	token, claimed, err := s.claimsRepo.Claim(ctx, repository.ResourceTypeUser, userID, repository.ActionTrialExpiration, trialExpirationClaimLease)
	if err != nil {
		return fmt.Errorf("failed to claim trial expiration for user %s: %w", userID, err)
	}
	if !claimed {
		// Another worker already holds this lease, or trial expiration was already completed
		// for this user. Legitimate, expected outcome — not an error.
		s.logger.Info("trial expiry claim lost — another worker holds it, or it was already completed",
			"user_id", userID)
		return nil
	}

	// Issue #1413 round-6 (independent adversarial review, twice over): round-5's reordering
	// (disconnect Plaid BEFORE flipping subscription_status) was ITSELF the regression, not the
	// fix. Its mid-run re-check below only ever verified HasActiveManualOverride() — it never
	// re-checked SubscriptionStatus — and it ran BEFORE the CAS, so 'trial' (a WRITABLE status,
	// unlike past_due) stayed writable for the entire duration of the disconnect call. Two
	// independent harms followed from that one writable window: (1) a user who completes checkout
	// mid-run (the webhook flips them to 'active') had every Plaid item removed AFTER already
	// paying — an irreversible /item/remove call the CAS then correctly refused to acknowledge,
	// silently logging success with a log line blaming a comp that was never granted; (2) a user
	// who links a NEW account mid-run could have that item orphaned, since cleanupItems only
	// operates on the item list it read when it began.
	//
	// The fix restores CAS-FIRST ordering (claim -> comp re-check -> CAS flip -> disconnect ->
	// retention -> complete): flipping subscription_status BEFORE the disconnect makes the user
	// read-only for the disconnect's entire duration, so neither harm above has a window to occur
	// in AT ALL — this eliminates the race rather than adding more checks inside a window that
	// should not exist. It is also why ExpireTrialIfNoOverride's own WHERE clause (subscription_
	// status = 'trial') now doubles as the subscribe-race guard for free: if the checkout webhook
	// already flipped status to 'active', the CAS simply reports flipped=false and the disconnect
	// below is never reached.
	//
	// This is the SAME shape PastDueService.processGraceExpiry already uses (claim -> external work
	// -> CAS -> complete) being applied correctly here for the first time: that flow's ordering
	// (Plaid disconnect BEFORE its own CAS) is safe specifically BECAUSE past_due is a READ-ONLY
	// status — a past_due user cannot link or write regardless of ordering, so there was never a
	// writable window there to close. 'trial' has no such protection, which is exactly what made
	// round-5's copy-the-pattern reorder the defect: it copied an ordering whose safety depended on
	// a precondition trial does not satisfy.
	//
	// Restoring CAS-first reopens the ORIGINAL problem round-5 existed to solve: once the flip
	// commits, GetUsersWithExpiredTrials (which selects on raw status = 'trial') can never
	// re-select this user, so a disconnect that fails — or never runs at all, e.g. a process killed
	// between the flip and the call below — would be unretryable via that path. The fix is a
	// dedicated repair path instead of reopening the writable window: GetUsersNeedingPlaidDisconnectRepair
	// (profile_repository.go) selects exactly this stuck state (read-only-status, live linked
	// items), and RetentionCleanupCheckWorker's periodic pass retries it — mirroring
	// GetUsersNeedingRetentionClockRepair's existing belt-and-braces shape for the retention clock.
	// This is strictly better than round-5's reorder: it is durable, monitorable, and it ALSO
	// catches a disconnect failure the reorder never could (a process killed between the flip and
	// the disconnect call, with no error to release a claim on at all).
	//
	// Issue #1413 round-4 finding H2's mid-run comp re-check survives this reordering, but now runs
	// immediately before the CAS (not the disconnect) — as late as the code can make it, narrowing
	// (not eliminating; the CAS's own WHERE clause is what eliminates it) the window a comp granted
	// after the top-of-function guard could slip through. Unlike the pre-round-6 code, a failure to
	// re-read here now fails closed (releases the claim and propagates the error) rather than
	// logging a Warn and proceeding with the destructive CAS anyway — matching
	// PastDueService.processGraceExpiry's model for the same situation.
	latest, err := s.repo.GetUserByID(ctx, userID)
	if err != nil {
		s.releaseClaimOnFailure(ctx, userID, token, err.Error())
		return fmt.Errorf("failed to re-read user %s before trial-expiry status flip: %w", userID, err)
	}
	if latest.HasActiveManualOverride() {
		s.releaseClaimOnFailure(ctx, userID, token, "comp granted since claim")
		s.logger.Info("skipping trial-expiry status flip and Plaid disconnect — comp granted since the claim",
			"user_id", userID)
		return nil
	}

	// #1413 High finding H5 (Codex review round 3): the HasActiveManualOverride() guard above is
	// a plain read and is NOT atomic with this write — an admin can grant/extend a comp in the
	// window between that check and here, or the checkout webhook can flip status to 'active' in
	// that same window. Replace the unconditional status write with a conditional transition that
	// re-verifies "still trial, still no live override" against committed state at the moment of
	// the write itself. A lost claim is a legitimate skip (the comp was granted, or the user is no
	// longer on a trial at all), not a failure — and per round-6's reordering, NOTHING external has
	// happened yet when this loses, so there is nothing to undo.
	flipped, err := s.repo.ExpireTrialIfNoOverride(ctx, userID)
	if err != nil {
		s.releaseClaimOnFailure(ctx, userID, token, err.Error())
		return fmt.Errorf("failed to expire trial status: %w", err)
	}
	if !flipped {
		s.releaseClaimOnFailure(ctx, userID, token, "precondition lost — comp granted or subscription status moved on concurrently")
		s.logger.Info("trial expiry claim lost — user comped or subscription status moved on concurrently, skipping",
			"user_id", userID)
		return nil
	}

	// Round-6: disconnect Plaid SECOND, now that the CAS above has already made this user
	// read-only — the writable window round-5 opened cannot recur here. A failure no longer rolls
	// back or blocks completion: the state transition this claim protects has already committed,
	// and GetUsersNeedingPlaidDisconnectRepair (not this claim, not GetUsersWithExpiredTrials) is
	// what makes a failure here retryable — see the reasoning above.
	var disconnectErr error
	if s.plaidCleanup != nil {
		disconnectErr = s.plaidCleanup.DisconnectAllUserItems(ctx, userID)
		if disconnectErr != nil {
			// ErrSideEffectClaimBusy is benign, expected contention (another flow is already
			// mid-disconnect for this user) — logged at Info, not Error, mirroring
			// past_due_service.go's processGraceExpiry Step 1 precedent. Any other failure is
			// logged at Error: unlike a retention-clock write failure (Warn), this means live
			// third-party bank access continues on an account that is now read-only and
			// non-paying — a real, if now-bounded-by-the-repair-pass, concern.
			if errors.Is(disconnectErr, ErrSideEffectClaimBusy) {
				s.logger.Info("trial-expiry Plaid disconnect deferred — side-effect claim held by another flow; will be retried by the Plaid-disconnect repair pass",
					"user_id", userID)
			} else {
				s.logger.Error("failed to disconnect Plaid items after trial-expiry status flip — will be retried by the Plaid-disconnect repair pass",
					"user_id", userID, "error", disconnectErr)
			}
		}
	}

	// #930: Start 6-month retention clock — data preserved read-only until auto-deletion. Gated on
	// `flipped` (never started for a user the CAS just refused to touch), NOT on the Plaid
	// disconnect outcome above — the user is already read-only regardless of whether the disconnect
	// itself succeeded, and retention's own belt-and-braces repair (GetUsersNeedingRetentionClockRepair)
	// exists independently of this disconnect concern.
	now := time.Now()
	if retErr := s.repo.SetRetentionDates(ctx, userID, now, now.AddDate(0, s.retentionMonths, 0)); retErr != nil {
		s.logger.Warn("failed to set retention dates on trial expiry", "user_id", userID, "error", retErr)
	}

	s.logger.Info("trial expired for user",
		"user_id", userID,
		"new_status", domain.SubscriptionStatusInactive,
	)

	// Complete unconditionally: the flow's defining state transition (the CAS flip above) already
	// committed, regardless of the Plaid disconnect outcome. Releasing instead would suggest a
	// retry of THIS claim/flow could still accomplish something — it cannot, since
	// ExpireTrialIfNoOverride's WHERE clause requires subscription_status = 'trial', which this
	// user has already left for good.
	completed, err := s.claimsRepo.Complete(ctx, repository.ResourceTypeUser, userID, repository.ActionTrialExpiration, token)
	if err != nil {
		s.logger.Error("failed to mark trial expiration claim complete", "user_id", userID, "action", repository.ActionTrialExpiration, "error", err)
	} else if !completed {
		s.logger.Error("trial expiration claim lease lost mid-flight — Plaid disconnect may have run twice",
			"user_id", userID, "action", repository.ActionTrialExpiration)
	}

	if disconnectErr != nil {
		// Surfaced for the caller's error/retry accounting visibility (TrialExpirationCheckWorker
		// logs and counts this), even though the state transition itself already succeeded and
		// retrying THIS call would accomplish nothing further — the Plaid-disconnect repair pass is
		// what actually retries the disconnect.
		return fmt.Errorf("trial expired for user %s but failed to disconnect Plaid items: %w", userID, disconnectErr)
	}

	return nil
}

// releaseClaimOnFailure releases the ActionTrialExpiration work claim and logs any release
// failure. Mirrors PastDueService.releaseClaimOnFailure's consolidation of the repeated
// Release + Warn pattern.
func (s *TrialService) releaseClaimOnFailure(ctx context.Context, userID, token, reason string) {
	if _, releaseErr := s.claimsRepo.Release(ctx, repository.ResourceTypeUser, userID, repository.ActionTrialExpiration, token, reason); releaseErr != nil {
		s.logger.Warn("failed to release trial expiration work claim", "user_id", userID, "reason", reason, "error", releaseErr)
	}
}

// CanGrantTrial returns true if the user can receive a trial
// (user has not already had a trial)
func (s *TrialService) CanGrantTrial(user *domain.User) bool {
	if user == nil {
		return false
	}
	return !user.HadTrial
}

// GetUsersWithExpiredTrials returns users whose trial has expired but
// whose status has not yet been updated to inactive
func (s *TrialService) GetUsersWithExpiredTrials(ctx context.Context) ([]*domain.User, error) {
	return s.repo.GetUsersWithExpiredTrials(ctx)
}

// GetUsersWithExpiringTrials returns users whose trial ends within the
// specified number of days (for sending reminder notifications)
func (s *TrialService) GetUsersWithExpiringTrials(ctx context.Context, withinDays int) ([]*domain.User, error) {
	return s.repo.GetUsersWithExpiringTrials(ctx, withinDays)
}
