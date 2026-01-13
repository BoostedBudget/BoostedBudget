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

// TestSyncLimitService_CheckSyncLimit_AlwaysAllowed tests that sync limits are removed
// All users can sync unlimited accounts
func TestSyncLimitService_CheckSyncLimit_AlwaysAllowed(t *testing.T) {
	ctx := context.Background()

	// Load real tier config
	tierConfig, err := config.LoadTierConfig()
	assert.NoError(t, err)

	subscriptionService := NewSubscriptionService(tierConfig, nil)

	mockSyncAuditRepo := new(MockSyncAuditRepository)
	mockProfileRepo := new(MockUserRepository)

	// Create service
	service := NewSyncLimitService(mockSyncAuditRepo, mockProfileRepo, subscriptionService)

	// Test CheckSyncLimit - should always return true (no limits)
	allowed, reason, err := service.CheckSyncLimit(ctx, "any-user-id", "any-plaid-account-id")

	assert.NoError(t, err)
	assert.True(t, allowed, "Sync should always be allowed - no monthly sync limits")
	assert.Empty(t, reason, "No reason should be given when allowed")

	// No mock expectations - mocks should NOT be called since limits are removed
}

// TestSyncLimitService_GetHistoricalDepthLimit tests getting historical depth by tier
func TestSyncLimitService_GetHistoricalDepthLimit(t *testing.T) {
	ctx := context.Background()

	// Load real tier config
	tierConfig, err := config.LoadTierConfig()
	assert.NoError(t, err)

	subscriptionService := NewSubscriptionService(tierConfig, nil)

	testCases := []struct {
		name               string
		userTier           domain.TierName
		expectedHistorical int
	}{
		{
			name:               "premium_tier",
			userTier:           domain.TierPremium,
			expectedHistorical: 730,
		},
		{
			name:               "household_2_tier",
			userTier:           domain.TierHousehold2,
			expectedHistorical: 730,
		},
		{
			name:               "household_5_tier",
			userTier:           domain.TierHousehold5,
			expectedHistorical: 730,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			userID := "user-" + string(tc.userTier)

			mockSyncAuditRepo := new(MockSyncAuditRepository)
			mockProfileRepo := new(MockUserRepository)

			// Mock user with specified tier
			user := &domain.User{
				ID:               userID,
				SubscriptionTier: string(tc.userTier),
			}
			mockProfileRepo.On("GetUserByID", ctx, userID).Return(user, nil)

			// Create service
			service := NewSyncLimitService(mockSyncAuditRepo, mockProfileRepo, subscriptionService)

			// Test GetHistoricalDepthLimit
			daysHistorical, err := service.GetHistoricalDepthLimit(ctx, userID)
			assert.NoError(t, err)
			assert.Equal(t, tc.expectedHistorical, daysHistorical)

			mockProfileRepo.AssertExpectations(t)
		})
	}
}

// TestSyncLimitService_RecordSync tests recording a successful sync
func TestSyncLimitService_RecordSync(t *testing.T) {
	ctx := context.Background()
	userID := "user-123"
	accountID := "acc-456"
	plaidAccountID := "plaid-acc-789"
	tier := domain.TierPremium
	daysHistorical := 730
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
		AccountsSynced:    10,
		TotalTransactions: 450,
	}, nil)

	// Mock user lookup
	user := &domain.User{
		ID:               userID,
		SubscriptionTier: string(domain.TierPremium),
	}
	mockProfileRepo.On("GetUserByID", ctx, userID).Return(user, nil)

	service := NewSyncLimitService(mockSyncAuditRepo, mockProfileRepo, subscriptionService)

	stats, err := service.GetSyncStats(ctx, userID)

	assert.NoError(t, err)
	assert.NotNil(t, stats)
	assert.Equal(t, 10, stats.AccountsSynced)
	assert.Equal(t, 30, stats.AccountsLimit) // Premium tier limit
	assert.Equal(t, 450, stats.TotalTransactions)
	assert.True(t, stats.CanSyncMore) // 10 < 30

	mockSyncAuditRepo.AssertExpectations(t)
	mockProfileRepo.AssertExpectations(t)
}
