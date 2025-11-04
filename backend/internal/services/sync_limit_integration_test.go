package services

import (
	"context"
	"testing"
	"time"

	"github.com/maxnussbaum/BoostedBudget/internal/config"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

// MockSyncAuditRepository is a mock implementation of repository.SyncAuditRepository
type MockSyncAuditRepository struct {
	mock.Mock
}

func (m *MockSyncAuditRepository) CountSyncsThisMonth(ctx context.Context, userID, month string) (int, error) {
	args := m.Called(ctx, userID, month)
	return args.Int(0), args.Error(1)
}

func (m *MockSyncAuditRepository) HasSyncedThisMonth(ctx context.Context, userID, plaidAccountID, month string) (bool, error) {
	args := m.Called(ctx, userID, plaidAccountID, month)
	return args.Bool(0), args.Error(1)
}

func (m *MockSyncAuditRepository) RecordSync(ctx context.Context, audit *domain.SyncAudit) error {
	args := m.Called(ctx, audit)
	return args.Error(0)
}

func (m *MockSyncAuditRepository) GetSyncStats(ctx context.Context, userID, month string) (*domain.SyncStats, error) {
	args := m.Called(ctx, userID, month)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*domain.SyncStats), args.Error(1)
}

// TestSyncLimitService_Integration tests the complete sync limit flow
func TestSyncLimitService_Integration(t *testing.T) {
	ctx := context.Background()

	// Load real tier config
	tierConfig, err := config.LoadTierConfig()
	assert.NoError(t, err)

	subscriptionService := NewSubscriptionService(tierConfig, nil)

	testCases := []struct {
		name               string
		userTier           domain.TierName
		accountsSynced     int
		expectedAllowed    bool
		expectedLimit      int
		expectedHistorical int
	}{
		{
			name:               "free_tier_under_limit",
			userTier:           domain.TierFree,
			accountsSynced:     2,
			expectedAllowed:    true,
			expectedLimit:      3,
			expectedHistorical: 30,
		},
		{
			name:               "free_tier_at_limit",
			userTier:           domain.TierFree,
			accountsSynced:     3,
			expectedAllowed:    false,
			expectedLimit:      3,
			expectedHistorical: 30,
		},
		{
			name:               "premium_lite_under_limit",
			userTier:           domain.TierPremiumLite,
			accountsSynced:     10,
			expectedAllowed:    true,
			expectedLimit:      15,
			expectedHistorical: 365,
		},
		{
			name:               "premium_unlimited",
			userTier:           domain.TierPremium,
			accountsSynced:     25,
			expectedAllowed:    true,
			expectedLimit:      30,
			expectedHistorical: 730,
		},
		{
			name:               "premium_plus_unlimited",
			userTier:           domain.TierPremiumPlus,
			accountsSynced:     100,
			expectedAllowed:    true,
			expectedLimit:      -1, // Unlimited
			expectedHistorical: 730,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			userID := "user-" + string(tc.userTier)
			plaidAccountID := "plaid-acc-123"
			currentMonth := time.Now().Format("2006-01")

			// Setup mocks
			mockSyncAuditRepo := new(MockSyncAuditRepository)
			mockProfileRepo := new(MockUserRepository)

			// User hasn't synced this specific account yet
			mockSyncAuditRepo.On("HasSyncedThisMonth", ctx, userID, plaidAccountID, currentMonth).Return(false, nil)

			// User has synced N accounts this month
			mockSyncAuditRepo.On("CountSyncsThisMonth", ctx, userID, currentMonth).Return(tc.accountsSynced, nil)

			// Mock user with specified tier
			user := &domain.User{
				ID:               userID,
				SubscriptionTier: string(tc.userTier),
			}
			mockProfileRepo.On("GetUserByID", ctx, userID).Return(user, nil)

			// Create service
			service := NewSyncLimitService(mockSyncAuditRepo, mockProfileRepo, subscriptionService)

			// Test CheckSyncLimit
			allowed, reason, err := service.CheckSyncLimit(ctx, userID, plaidAccountID)
			assert.NoError(t, err)
			assert.Equal(t, tc.expectedAllowed, allowed)

			if !allowed {
				assert.Contains(t, reason, "monthly sync limit reached")
			}

			// Test GetHistoricalDepthLimit
			daysHistorical, err := service.GetHistoricalDepthLimit(ctx, userID)
			assert.NoError(t, err)
			assert.Equal(t, tc.expectedHistorical, daysHistorical)

			mockSyncAuditRepo.AssertExpectations(t)
			mockProfileRepo.AssertExpectations(t)
		})
	}
}

// TestSyncLimitService_ReSyncAllowed tests that re-syncing doesn't count against limit
func TestSyncLimitService_ReSyncAllowed(t *testing.T) {
	ctx := context.Background()
	userID := "user-123"
	plaidAccountID := "plaid-acc-already-synced"
	currentMonth := time.Now().Format("2006-01")

	mockSyncAuditRepo := new(MockSyncAuditRepository)
	mockProfileRepo := new(MockUserRepository)

	tierConfig, err := config.LoadTierConfig()
	assert.NoError(t, err)

	subscriptionService := NewSubscriptionService(tierConfig, nil)

	// User HAS already synced this account this month
	mockSyncAuditRepo.On("HasSyncedThisMonth", ctx, userID, plaidAccountID, currentMonth).Return(true, nil)

	// Note: CountSyncsThisMonth should NOT be called for re-syncs
	// Note: GetUserByID should NOT be called for re-syncs

	service := NewSyncLimitService(mockSyncAuditRepo, mockProfileRepo, subscriptionService)

	allowed, reason, err := service.CheckSyncLimit(ctx, userID, plaidAccountID)

	assert.NoError(t, err)
	assert.True(t, allowed)
	assert.Empty(t, reason)

	mockSyncAuditRepo.AssertExpectations(t)
	// ProfileRepo should have NO expectations (not called for re-syncs)
}

// TestSyncLimitService_RecordSync tests recording a successful sync
func TestSyncLimitService_RecordSync(t *testing.T) {
	ctx := context.Background()
	userID := "user-123"
	accountID := "acc-456"
	plaidAccountID := "plaid-acc-789"
	tier := domain.TierFree
	daysHistorical := 30
	transactionCount := 150

	mockSyncAuditRepo := new(MockSyncAuditRepository)
	mockProfileRepo := new(MockUserRepository)

	tierConfig, err := config.LoadTierConfig()
	assert.NoError(t, err)

	subscriptionService := NewSubscriptionService(tierConfig, nil)

	// Expect RecordSync to be called with audit record
	mockSyncAuditRepo.On("RecordSync", ctx, mock.MatchedBy(func(audit *domain.SyncAudit) bool {
		return audit.UserID == userID &&
			*audit.AccountID == accountID &&
			audit.PlaidAccountID == plaidAccountID &&
			audit.TierAtSync == string(tier) &&
			audit.DaysHistorical == daysHistorical &&
			audit.TransactionCount == transactionCount
	})).Return(nil)

	service := NewSyncLimitService(mockSyncAuditRepo, mockProfileRepo, subscriptionService)

	err = service.RecordSync(ctx, userID, accountID, plaidAccountID, tier, daysHistorical, transactionCount)

	assert.NoError(t, err)
	mockSyncAuditRepo.AssertExpectations(t)
}

// TestSyncLimitService_GetSyncStats tests retrieving sync statistics
func TestSyncLimitService_GetSyncStats(t *testing.T) {
	ctx := context.Background()
	userID := "user-123"
	currentMonth := time.Now().Format("2006-01")

	mockSyncAuditRepo := new(MockSyncAuditRepository)
	mockProfileRepo := new(MockUserRepository)

	tierConfig, err := config.LoadTierConfig()
	assert.NoError(t, err)

	subscriptionService := NewSubscriptionService(tierConfig, nil)

	// Mock repository returning sync stats
	mockSyncAuditRepo.On("GetSyncStats", ctx, userID, currentMonth).Return(&domain.SyncStats{
		UserID:            userID,
		Month:             currentMonth,
		AccountsSynced:    2,
		TotalTransactions: 450,
	}, nil)

	// Mock user lookup
	user := &domain.User{
		ID:               userID,
		SubscriptionTier: string(domain.TierFree),
	}
	mockProfileRepo.On("GetUserByID", ctx, userID).Return(user, nil)

	service := NewSyncLimitService(mockSyncAuditRepo, mockProfileRepo, subscriptionService)

	stats, err := service.GetSyncStats(ctx, userID)

	assert.NoError(t, err)
	assert.NotNil(t, stats)
	assert.Equal(t, 2, stats.AccountsSynced)
	assert.Equal(t, 3, stats.AccountsLimit) // Free tier limit
	assert.Equal(t, 450, stats.TotalTransactions)
	assert.True(t, stats.CanSyncMore) // 2 < 3

	mockSyncAuditRepo.AssertExpectations(t)
	mockProfileRepo.AssertExpectations(t)
}
