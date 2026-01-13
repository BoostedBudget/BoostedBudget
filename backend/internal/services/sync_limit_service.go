package services

import (
	"context"
	"fmt"
	"time"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/repository"
)

// SyncLimitService defines the interface for checking and enforcing monthly transaction sync limits.
// Issue #242: Prevent account/transaction abuse via monthly sync limits
type SyncLimitService interface {
	// CheckSyncLimit checks if a user is allowed to sync a specific account this month.
	// Returns (allowed, reason, error).
	//
	// Key behaviors:
	//   - If account already synced this month: returns (true, "", nil) - re-sync allowed
	//   - If under monthly limit: returns (true, "", nil)
	//   - If at/over limit: returns (false, "reason message", nil)
	//   - On error: returns (false, "", error)
	CheckSyncLimit(ctx context.Context, userID, plaidAccountID string) (bool, string, error)

	// RecordSync records a successful sync operation for audit trail and limit enforcement.
	// Uses UPSERT pattern: first sync creates record, re-syncs update transaction count.
	//
	// Should be called AFTER successful transaction sync completes.
	RecordSync(ctx context.Context, userID, accountID, plaidAccountID string, tier domain.TierName, daysHistorical, transactionCount int) error

	// GetSyncStats returns sync usage statistics for a user in the current month.
	// Used to display: "You've synced 2 of 3 accounts this month"
	GetSyncStats(ctx context.Context, userID string) (*domain.SyncStats, error)

	// GetHistoricalDepthLimit returns the number of days of historical transactions
	// the user is allowed to sync based on their subscription tier.
	// Free: 30 days, Premium Lite: 365 days, Premium/Premium+: 730 days
	GetHistoricalDepthLimit(ctx context.Context, userID string) (int, error)
}

// syncLimitService implements SyncLimitService
type syncLimitService struct {
	syncAuditRepo       repository.SyncAuditRepository
	profileRepo         repository.ProfileRepository
	subscriptionService *SubscriptionService
}

// NewSyncLimitService creates a new SyncLimitService instance
func NewSyncLimitService(
	syncAuditRepo repository.SyncAuditRepository,
	profileRepo repository.ProfileRepository,
	subscriptionService *SubscriptionService,
) SyncLimitService {
	return &syncLimitService{
		syncAuditRepo:       syncAuditRepo,
		profileRepo:         profileRepo,
		subscriptionService: subscriptionService,
	}
}

// monthFormat is the standard format for sync_month (YYYY-MM)
const monthFormat = "2006-01"

// getUserSubscription is a helper that fetches user and their effective subscription.
// This pattern is used by multiple methods to avoid duplication.
func (s *syncLimitService) getUserSubscription(ctx context.Context, userID string) (*domain.User, *domain.EffectiveSubscription, error) {
	user, err := s.profileRepo.GetUserByID(ctx, userID)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to get user: %w", err)
	}

	subscription, err := s.subscriptionService.GetEffectiveSubscription(ctx, user)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to get subscription: %w", err)
	}

	return user, subscription, nil
}

// CheckSyncLimit checks if a user is allowed to sync a specific account this month.
// Simplified: Always returns true (no monthly sync limits for any tier).
func (s *syncLimitService) CheckSyncLimit(ctx context.Context, userID, plaidAccountID string) (bool, string, error) {
	// No monthly sync limits - all users can sync unlimited accounts
	return true, "", nil
}

// RecordSync records a successful sync operation
func (s *syncLimitService) RecordSync(ctx context.Context, userID, accountID, plaidAccountID string, tier domain.TierName, daysHistorical, transactionCount int) error {
	currentMonth := time.Now().Format(monthFormat)
	now := time.Now()

	audit := &domain.SyncAudit{
		UserID:           userID,
		AccountID:        &accountID,
		PlaidAccountID:   plaidAccountID,
		SyncMonth:        currentMonth,
		FirstSyncAt:      now,
		LastSyncAt:       now,
		TierAtSync:       string(tier),
		DaysHistorical:   daysHistorical,
		TransactionCount: transactionCount,
	}

	// Validate audit record
	if err := audit.Validate(); err != nil {
		return fmt.Errorf("invalid sync audit record: %w", err)
	}

	// Record sync (UPSERT pattern)
	if err := s.syncAuditRepo.RecordSync(ctx, audit); err != nil {
		return fmt.Errorf("failed to record sync: %w", err)
	}

	return nil
}

// GetSyncStats returns sync usage statistics for a user
func (s *syncLimitService) GetSyncStats(ctx context.Context, userID string) (*domain.SyncStats, error) {
	currentMonth := time.Now().Format(monthFormat)

	// Step 1: Get sync statistics from repository
	stats, err := s.syncAuditRepo.GetSyncStats(ctx, userID, currentMonth)
	if err != nil {
		return nil, fmt.Errorf("failed to get sync stats: %w", err)
	}

	// Step 2: Look up user and subscription (using helper to reduce duplication)
	_, subscription, err := s.getUserSubscription(ctx, userID)
	if err != nil {
		return nil, err // Error already wrapped by helper
	}

	// Step 3: Populate limit and calculate remaining capacity
	// No monthly sync limits - use account limit and always allow more syncs
	stats.AccountsLimit = subscription.Limits.Accounts
	stats.CanSyncMore = true // No monthly sync limits

	// Step 4: Calculate reset date (first day of next month)
	stats.ResetDate = stats.CalculateResetDate()

	return stats, nil
}

// GetHistoricalDepthLimit returns the tier-based historical depth limit
func (s *syncLimitService) GetHistoricalDepthLimit(ctx context.Context, userID string) (int, error) {
	// Look up user and subscription (using helper to reduce duplication)
	_, subscription, err := s.getUserSubscription(ctx, userID)
	if err != nil {
		return 0, err // Error already wrapped by helper
	}

	return subscription.Limits.TransactionHistoryDays, nil
}
