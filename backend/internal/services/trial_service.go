// Package services provides business logic and external service integrations.
// This file implements TrialService for managing user trials.
// Issue #679: Implement trial service and auto-grant logic
package services

import (
	"context"
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

	// #1413 High finding H5 (Codex review round 3): the HasActiveManualOverride() guard above is
	// a plain read and is NOT atomic with this write — an admin can grant/extend a comp in the
	// window between that check and here. Replace the unconditional status write with a
	// conditional transition that re-verifies "still trial, still no live override" against
	// committed state at the moment of the write itself. A lost claim is a legitimate skip (the
	// comp already granted access), not a failure — retention and Plaid must NOT be touched.
	flipped, err := s.repo.ExpireTrialIfNoOverride(ctx, userID)
	if err != nil {
		s.releaseClaimOnFailure(ctx, userID, token, err.Error())
		return fmt.Errorf("failed to expire trial status: %w", err)
	}
	if !flipped {
		s.releaseClaimOnFailure(ctx, userID, token, "precondition lost — comp granted concurrently")
		s.logger.Info("trial expiry claim lost — user comped concurrently, skipping",
			"user_id", userID)
		return nil
	}

	// #930: Start 6-month retention clock — data preserved read-only until auto-deletion
	now := time.Now()
	if retErr := s.repo.SetRetentionDates(ctx, userID, now, now.AddDate(0, s.retentionMonths, 0)); retErr != nil {
		s.logger.Warn("failed to set retention dates on trial expiry", "user_id", userID, "error", retErr)
	}

	// Disconnect all Plaid items (preserves transaction history).
	//
	// Issue #1413 round-4 finding H2: re-check the comp predicate ONE more time, as late as the
	// code can make it — immediately before this external call — narrowing (not eliminating) the
	// window a comp granted after the CAS above could slip through. No database mechanism can
	// make this window zero: there is always some interval between the last possible check and
	// an external API call, and a worker already inside a Plaid call cannot be recalled — so this
	// is a best-effort narrowing, not a closure, and the residual left after it is DELIBERATELY
	// accepted rather than built further against (see the caution in this issue's brief against
	// overbuilding H2). Unlike C1's irreversible-deletion residual, the consequence here is
	// acceptable: a Plaid disconnect is recoverable (the user relinks their accounts), and the
	// retention clock this run just started is cleared the moment the comp is applied (see
	// ClearRetentionDates, called wherever a comp/resubscription is granted) — inconvenience, not
	// data loss.
	if s.plaidCleanup != nil {
		shouldDisconnect := true
		if latest, err := s.repo.GetUserByID(ctx, userID); err != nil {
			s.logger.Warn("failed to re-read user before trial-expiry Plaid disconnect; proceeding with disconnect",
				"user_id", userID, "error", err)
		} else if latest.HasActiveManualOverride() {
			shouldDisconnect = false
			s.logger.Info("skipping trial-expiry Plaid disconnect — comp granted since the status flip",
				"user_id", userID)
		}
		if shouldDisconnect {
			if err := s.plaidCleanup.DisconnectAllUserItems(ctx, userID); err != nil {
				// Log but don't fail - the subscription status is already updated. Matches this
				// flow's pre-existing graceful-degradation contract (unlike PastDueService/
				// DeletionService, which abort-and-release on a Plaid failure) — not something
				// this task's H2 fix is asked to change (see the brief's "do not overbuild"
				// caution). The claim is still completed below regardless, since nothing in this
				// codebase would ever retry a failed disconnect here either way: once flipped out
				// of 'trial', GetUsersWithExpiredTrials never reselects this user again.
				s.logger.Warn("failed to disconnect Plaid items during trial expiration",
					"user_id", userID,
					"error", err,
				)
			}
		}
	}

	s.logger.Info("trial expired for user",
		"user_id", userID,
		"new_status", domain.SubscriptionStatusInactive,
	)

	completed, err := s.claimsRepo.Complete(ctx, repository.ResourceTypeUser, userID, repository.ActionTrialExpiration, token)
	if err != nil {
		s.logger.Error("failed to mark trial expiration claim complete", "user_id", userID, "action", repository.ActionTrialExpiration, "error", err)
	} else if !completed {
		s.logger.Error("trial expiration claim lease lost mid-flight — Plaid disconnect may have run twice",
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
