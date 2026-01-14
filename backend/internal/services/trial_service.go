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
)

// TrialRepository defines the repository methods needed by TrialService
type TrialRepository interface {
	// GetUserByID retrieves a user by their internal UUID
	GetUserByID(ctx context.Context, id string) (*domain.User, error)

	// UpdateTrialStatus updates the trial-related fields for a user
	UpdateTrialStatus(ctx context.Context, userID string, status string, trialStartedAt, trialEndsAt *time.Time, hadTrial bool) error

	// UpdateSubscriptionStatus updates just the subscription status
	UpdateSubscriptionStatus(ctx context.Context, userID string, status string) error

	// GetUsersWithExpiredTrials returns users with trial status but past trial_ends_at
	GetUsersWithExpiredTrials(ctx context.Context) ([]*domain.User, error)

	// GetUsersWithExpiringTrials returns users whose trial ends within N days
	GetUsersWithExpiringTrials(ctx context.Context, withinDays int) ([]*domain.User, error)
}

// PlaidCleanupForTrial defines the Plaid cleanup methods needed by TrialService
type PlaidCleanupForTrial interface {
	// DisconnectAllUserItems calls Plaid /item/remove for all items
	// then marks them as 'disconnected' (preserves transaction history)
	DisconnectAllUserItems(ctx context.Context, userID string) error
}

// TrialService handles trial management for users
// Issue #679: Implement trial service and auto-grant logic
type TrialService struct {
	tierConfig   *config.TierConfig
	repo         TrialRepository
	plaidCleanup PlaidCleanupForTrial
	logger       *slog.Logger
}

// NewTrialService creates a new TrialService
func NewTrialService(
	tierConfig *config.TierConfig,
	repo TrialRepository,
	plaidCleanup PlaidCleanupForTrial,
) *TrialService {
	return &TrialService{
		tierConfig:   tierConfig,
		repo:         repo,
		plaidCleanup: plaidCleanup,
		logger:       slog.Default(),
	}
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

	s.logger.Info("expiring trial for user",
		"user_id", userID,
		"previous_status", user.SubscriptionStatus,
	)

	// Update subscription status to inactive
	err = s.repo.UpdateSubscriptionStatus(ctx, userID, string(domain.SubscriptionStatusInactive))
	if err != nil {
		return fmt.Errorf("failed to update subscription status: %w", err)
	}

	// Disconnect all Plaid items (preserves transaction history)
	if s.plaidCleanup != nil {
		err = s.plaidCleanup.DisconnectAllUserItems(ctx, userID)
		if err != nil {
			// Log but don't fail - the subscription status is already updated
			s.logger.Warn("failed to disconnect Plaid items during trial expiration",
				"user_id", userID,
				"error", err,
			)
		}
	}

	s.logger.Info("trial expired for user",
		"user_id", userID,
		"new_status", domain.SubscriptionStatusInactive,
	)

	return nil
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
