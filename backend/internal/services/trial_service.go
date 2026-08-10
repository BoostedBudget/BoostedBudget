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

	// SetRetentionDates starts the retention clock (#930) — both dates are computed in Postgres
	// (clock_timestamp() + make_interval); see ProfileRepository.SetRetentionDates's doc comment
	// (repository/profile_repository.go) for why a Go-computed date is never passed in, and
	// RetentionClockGuard's doc comment for why the authorising condition is passed in by this
	// caller rather than hardcoded in the repository.
	SetRetentionDates(ctx context.Context, userID string, guard repository.RetentionClockGuard, retentionMonths int) error
}

// trialExpirationClaimLease is the lease duration requested for ActionTrialExpiration — matches
// ExecuteDeletion's accountDeletionClaimLease and PastDueService's pastDueClaimLease (Issue
// #1413 round-4 finding H2). Generous beats tight, and the fencing token
// (repository.WorkClaimsRepository) makes a lease over-run detectable rather than silently
// re-granted to a second worker.
const trialExpirationClaimLease = 15 * time.Minute

// householdCoverageCheckerForTrial is the narrow capability TrialService needs to detect an
// active household-sourced entitlement, so the daily trial-expiry job doesn't flip a covered
// member to inactive (and, eventually, have their Plaid items disconnected by the grace-window
// repair pass) despite being actively covered (#1367 — Phase 3). Satisfied structurally by
// *HouseholdEntitlementService.
type householdCoverageCheckerForTrial interface {
	IsCoveredByHousehold(ctx context.Context, userID string) (bool, error)
}

// TrialService handles trial management for users
// Issue #679: Implement trial service and auto-grant logic
type TrialService struct {
	tierConfig       *config.TierConfig
	repo             TrialRepository
	householdChecker householdCoverageCheckerForTrial // #1367 — Phase 3: nil skips the exemption
	claimsRepo       repository.WorkClaimsRepository  // #1413 round-4 H2: required, see SetWorkClaimsRepo
	retentionMonths  int                              // #930: Months before auto-deletion after trial expiry
	logger           *slog.Logger
}

// NewTrialService creates a new TrialService
//
// Issue #1413 round-7: TrialService no longer has a Plaid dependency at all — ExpireTrial never
// calls Plaid (see that method's doc comment for why no ordering of a status flip and an
// irreversible Plaid call can be safe). The disconnect now lives entirely in
// RetentionCleanupCheckWorker's grace-window repair pass.
func NewTrialService(
	tierConfig *config.TierConfig,
	repo TrialRepository,
) *TrialService {
	return &TrialService{
		tierConfig:      tierConfig,
		repo:            repo,
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

// SetWorkClaimsRepo wires the durable work-claims mechanism ExpireTrial requires for
// EXCLUSIVITY on ActionTrialExpiration before its status-flip CAS. Issue #1413 round-4 finding
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

// ExpireTrial expires a user's trial.
//
// Issue #1413 round-7 (Codex review): ExpireTrial flips subscription_status to 'inactive' and
// starts the retention clock — that is ALL it does. It does NOT call Plaid. This function has
// been reordered twice before this fix, and both attempts were wrong for opposite reasons:
//
//   - Round-5 (disconnect Plaid BEFORE the flip): 'trial' is a WRITABLE status, so a user could
//     complete checkout or link a new account WHILE the disconnect ran, racing it.
//   - Round-6 (disconnect Plaid AFTER the flip, i.e. CAS-first): the premise was that flipping the
//     user to read-only makes them unable to act. That premise is FALSE — /subscription/checkout is
//     deliberately whitelisted for read-only users (internal/huma/middleware/read_only.go), because a
//     lapsed user must be able to pay to restore access. So even after the CAS commits, the user can
//     start checkout, the Paddle webhook can flip them to 'active'
//     (subscription_webhook.go), and only THEN does the disconnect call (which can take real
//     wall-clock time — one Plaid API call per linked item) actually run — deleting a paying
//     customer's banks after they already paid.
//
// CONCLUSION: no ordering of a status flip and an irreversible external call is safe here, because
// becoming active again is precisely what the system always permits for a read-only user. The fix
// is to stop trying to order it and DEFER instead: the disconnect moves entirely to the repair
// pass (RetentionCleanupCheckWorker.repairPlaidDisconnects), which only runs once a grace window
// has elapsed since the plaid_cleanup_pending_at marker was set — stamped atomically, below, by
// this function's own trial->inactive CAS (ExpireTrialIfNoOverride), NOT by the retention_started_at
// write that follows it. (retention_started_at was the grace anchor only through round-9; round-10
// replaced it with this marker, and round-11 explicitly forbids reintroducing retention_started_at
// as the anchor — see profile_repository.go's GetUsersNeedingPlaidDisconnectRepair doc comment.)
// The repair pass re-verifies eligibility immediately before removing each item, so a user who
// resubscribes within the grace window is never disconnected at all — the race becomes a
// non-event rather than a narrowed window. See profile_repository.go's
// GetUsersNeedingPlaidDisconnectRepair and retention_cleanup_check.go's repairPlaidDisconnects for
// the other half of this design.
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
	// re-verify via a CAS, then Complete/Release). ExpireTrialIfNoOverride's own conditional
	// UPDATE already gives this write its correctness against a comp granted in the window since
	// the plain read above; this claim adds the same EXCLUSIVITY guarantee every other destructive
	// flow in this branch already has.
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

	// Issue #1413 round-4 finding H2's mid-run comp re-check: the top-of-function
	// HasActiveManualOverride() guard above is a plain read, not atomic with the CAS below — an
	// admin can grant/extend a comp in the window between that check and here. Re-reading
	// immediately before the CAS narrows (not eliminates; the CAS's own WHERE clause is what
	// eliminates it) that window. A failure to re-read here fails closed (releases the claim and
	// propagates the error) rather than proceeding with the destructive CAS anyway.
	latest, err := s.repo.GetUserByID(ctx, userID)
	if err != nil {
		s.releaseClaimOnFailure(ctx, userID, token, err.Error())
		return fmt.Errorf("failed to re-read user %s before trial-expiry status flip: %w", userID, err)
	}
	if latest.HasActiveManualOverride() {
		s.releaseClaimOnFailure(ctx, userID, token, "comp granted since claim")
		s.logger.Info("skipping trial-expiry status flip — comp granted since the claim",
			"user_id", userID)
		return nil
	}

	// #1413 High finding H5 (Codex review round 3): replace an unconditional status write with a
	// conditional transition that re-verifies "still trial, still no live override" against
	// committed state at the moment of the write itself. A lost CAS is a legitimate skip (the comp
	// was granted, or the user is no longer on a trial at all — e.g. the checkout webhook already
	// flipped them to 'active'), not a failure.
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

	// #930: Start 6-month retention clock — data preserved read-only until auto-deletion. Gated on
	// `flipped` (never started for a user the CAS just refused to touch). This timestamp
	// (retention_started_at) governs ONLY the deletion clock — it plays NO role in the Plaid
	// disconnect grace window (round-10/round-11; do not reintroduce it as that anchor). The
	// disconnect's own anchor, plaid_cleanup_pending_at, was already stamped atomically by the CAS
	// flip above (ExpireTrialIfNoOverride) — see GetUsersNeedingPlaidDisconnectRepair's doc comment
	// in profile_repository.go for why the two grace windows are governed by separate columns.
	// retention's own belt-and-braces repair (GetUsersNeedingRetentionClockRepair) exists
	// independently as a backstop if this write itself fails.
	//
	// Issue #1500 residual (Codex review, PR #1443 follow-up): this call carries an authorising
	// condition too, and it is NOT the cancellation webhook's. What authorises it is the CAS
	// immediately above: ExpireTrialIfNoOverride flips 'trial' -> 'inactive' and returns true only
	// for the row it actually moved, so the state this write is entitled to act on is
	// "still 'inactive'". `flipped` proves that was true a moment ago, not that it is true now —
	// the user can resubscribe in the window between the CAS and this line (the checkout webhook
	// writes 'active' and calls ClearRetentionDates), and this write would then re-arm a six-month
	// deletion clock on a paying customer, exactly the defect the cancellation path had. The guard
	// is therefore genuinely required here; it is simply a DIFFERENT condition, which is why the
	// repository takes it from the caller instead of hardcoding 'cancelled'.
	//
	// A lost precondition is a legitimate skip, not a failure: the user is no longer inactive, so
	// nobody wants this clock armed. Logged at Info, distinct from a real write failure (Warn), for
	// which GetUsersNeedingRetentionClockRepair remains the backstop.
	if retErr := s.repo.SetRetentionDates(ctx, userID, repository.RetentionClockRequiresStatus(domain.SubscriptionStatusInactive), s.retentionMonths); retErr != nil {
		if errors.Is(retErr, repository.ErrRetentionClockPreconditionLost) {
			s.logger.Info("retention clock not started on trial expiry — the user is no longer inactive (resubscribed or comped concurrently)",
				"user_id", userID)
		} else {
			s.logger.Warn("failed to set retention dates on trial expiry", "user_id", userID, "error", retErr)
		}
	}

	s.logger.Info("trial expired for user",
		"user_id", userID,
		"new_status", domain.SubscriptionStatusInactive,
	)

	// Complete unconditionally: the flow's defining state transition (the CAS flip above) already
	// committed. Releasing instead would suggest a retry of THIS claim/flow could still accomplish
	// something — it cannot, since ExpireTrialIfNoOverride's WHERE clause requires
	// subscription_status = 'trial', which this user has already left for good. There is no Plaid
	// disconnect outcome to factor in here at all (round-7) — that entirely lives in the
	// grace-window repair pass now.
	completed, err := s.claimsRepo.Complete(ctx, repository.ResourceTypeUser, userID, repository.ActionTrialExpiration, token)
	if err != nil {
		s.logger.Error("failed to mark trial expiration claim complete", "user_id", userID, "action", repository.ActionTrialExpiration, "error", err)
	} else if !completed {
		s.logger.Error("trial expiration claim lease lost mid-flight",
			"user_id", userID, "action", repository.ActionTrialExpiration)
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
