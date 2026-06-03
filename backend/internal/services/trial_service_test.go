package services

import (
	"context"
	"testing"
	"time"

	"github.com/maxnussbaum/BoostedBudget/internal/config"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// mockTrialRepository is a mock implementation of TrialRepository for testing
type mockTrialRepository struct {
	mock.Mock
}

func (m *mockTrialRepository) GetUserByID(ctx context.Context, id string) (*domain.User, error) {
	args := m.Called(ctx, id)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*domain.User), args.Error(1)
}

func (m *mockTrialRepository) UpdateTrialStatus(ctx context.Context, userID string, status string, trialStartedAt, trialEndsAt *time.Time, hadTrial bool) error {
	args := m.Called(ctx, userID, status, trialStartedAt, trialEndsAt, hadTrial)
	return args.Error(0)
}

func (m *mockTrialRepository) UpdateSubscriptionStatus(ctx context.Context, userID string, status string) error {
	args := m.Called(ctx, userID, status)
	return args.Error(0)
}

func (m *mockTrialRepository) GetUsersWithExpiredTrials(ctx context.Context) ([]*domain.User, error) {
	args := m.Called(ctx)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]*domain.User), args.Error(1)
}

func (m *mockTrialRepository) GetUsersWithExpiringTrials(ctx context.Context, withinDays int) ([]*domain.User, error) {
	args := m.Called(ctx, withinDays)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]*domain.User), args.Error(1)
}

// #930: SetRetentionDates stub — allows mock to satisfy updated TrialRepository interface
func (m *mockTrialRepository) SetRetentionDates(ctx context.Context, userID string, startedAt, expiresAt time.Time) error {
	return m.Called(ctx, userID, startedAt, expiresAt).Error(0)
}

// mockPlaidCleanupService is a mock for Plaid cleanup
type mockPlaidCleanupService struct {
	mock.Mock
}

func (m *mockPlaidCleanupService) DisconnectAllUserItems(ctx context.Context, userID string) error {
	args := m.Called(ctx, userID)
	return args.Error(0)
}

func setupTestTrialService(t *testing.T) (*TrialService, *mockTrialRepository, *mockPlaidCleanupService) {
	t.Helper()

	// Load tier config for trial duration
	tierConfig, err := config.NewTierConfig("")
	require.NoError(t, err)

	repo := new(mockTrialRepository)
	plaidCleanup := new(mockPlaidCleanupService)

	service := NewTrialService(tierConfig, repo, plaidCleanup)

	return service, repo, plaidCleanup
}

func TestTrialService_GrantTrial_Success(t *testing.T) {
	service, repo, _ := setupTestTrialService(t)
	ctx := context.Background()
	userID := "user-123"

	// User who hasn't had a trial
	user := &domain.User{
		ID:                 userID,
		SubscriptionTier:   "premium",
		SubscriptionStatus: "active",
		HadTrial:           false,
	}

	repo.On("GetUserByID", ctx, userID).Return(user, nil)
	repo.On("UpdateTrialStatus", ctx, userID, "trial", mock.AnythingOfType("*time.Time"), mock.AnythingOfType("*time.Time"), true).Return(nil)

	err := service.GrantTrial(ctx, userID)

	require.NoError(t, err)
	repo.AssertExpectations(t)
}

func TestTrialService_GrantTrial_AlreadyHadTrial(t *testing.T) {
	service, repo, _ := setupTestTrialService(t)
	ctx := context.Background()
	userID := "user-123"

	// User who already had a trial
	user := &domain.User{
		ID:                 userID,
		SubscriptionTier:   "premium",
		SubscriptionStatus: "inactive",
		HadTrial:           true,
	}

	repo.On("GetUserByID", ctx, userID).Return(user, nil)

	err := service.GrantTrial(ctx, userID)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "already had a trial")
	repo.AssertExpectations(t)
}

func TestTrialService_IsTrialActive_Active(t *testing.T) {
	service, _, _ := setupTestTrialService(t)

	// User with active trial (ends in the future)
	trialEndsAt := time.Now().Add(30 * 24 * time.Hour)
	user := &domain.User{
		ID:                 "user-123",
		SubscriptionStatus: "trial",
		TrialEndsAt:        &trialEndsAt,
	}

	isActive := service.IsTrialActive(user)

	assert.True(t, isActive)
}

func TestTrialService_IsTrialActive_Expired(t *testing.T) {
	service, _, _ := setupTestTrialService(t)

	// User with expired trial (ended in the past)
	trialEndsAt := time.Now().Add(-1 * time.Hour)
	user := &domain.User{
		ID:                 "user-123",
		SubscriptionStatus: "trial",
		TrialEndsAt:        &trialEndsAt,
	}

	isActive := service.IsTrialActive(user)

	assert.False(t, isActive)
}

func TestTrialService_IsTrialActive_NotTrial(t *testing.T) {
	service, _, _ := setupTestTrialService(t)

	// User with active subscription (not trial)
	user := &domain.User{
		ID:                 "user-123",
		SubscriptionStatus: "active",
	}

	isActive := service.IsTrialActive(user)

	assert.False(t, isActive)
}

func TestTrialService_GetTrialDaysRemaining(t *testing.T) {
	service, _, _ := setupTestTrialService(t)

	// User with 30 days remaining
	trialEndsAt := time.Now().Add(30 * 24 * time.Hour)
	user := &domain.User{
		ID:                 "user-123",
		SubscriptionStatus: "trial",
		TrialEndsAt:        &trialEndsAt,
	}

	days := service.GetTrialDaysRemaining(user)

	// Should be approximately 30 days (allow for test execution time)
	assert.GreaterOrEqual(t, days, 29)
	assert.LessOrEqual(t, days, 30)
}

func TestTrialService_GetTrialDaysRemaining_Expired(t *testing.T) {
	service, _, _ := setupTestTrialService(t)

	// User with expired trial
	trialEndsAt := time.Now().Add(-5 * 24 * time.Hour)
	user := &domain.User{
		ID:                 "user-123",
		SubscriptionStatus: "trial",
		TrialEndsAt:        &trialEndsAt,
	}

	days := service.GetTrialDaysRemaining(user)

	assert.Equal(t, 0, days)
}

func TestTrialService_ExpireTrial_Success(t *testing.T) {
	service, repo, plaidCleanup := setupTestTrialService(t)
	ctx := context.Background()
	userID := "user-123"

	// User with expired trial
	trialEndsAt := time.Now().Add(-1 * time.Hour)
	user := &domain.User{
		ID:                 userID,
		SubscriptionStatus: "trial",
		TrialEndsAt:        &trialEndsAt,
	}

	repo.On("GetUserByID", ctx, userID).Return(user, nil)
	repo.On("UpdateSubscriptionStatus", ctx, userID, "inactive").Return(nil)
	repo.On("SetRetentionDates", ctx, userID, mock.Anything, mock.Anything).Return(nil)
	plaidCleanup.On("DisconnectAllUserItems", ctx, userID).Return(nil)

	err := service.ExpireTrial(ctx, userID)

	require.NoError(t, err)
	repo.AssertExpectations(t)
	plaidCleanup.AssertExpectations(t)
}

func TestTrialService_ExpireTrial_DisconnectsPlaidItems(t *testing.T) {
	service, repo, plaidCleanup := setupTestTrialService(t)
	ctx := context.Background()
	userID := "user-123"

	trialEndsAt := time.Now().Add(-1 * time.Hour)
	user := &domain.User{
		ID:                 userID,
		SubscriptionStatus: "trial",
		TrialEndsAt:        &trialEndsAt,
	}

	repo.On("GetUserByID", ctx, userID).Return(user, nil)
	repo.On("UpdateSubscriptionStatus", ctx, userID, "inactive").Return(nil)
	repo.On("SetRetentionDates", ctx, userID, mock.Anything, mock.Anything).Return(nil)
	plaidCleanup.On("DisconnectAllUserItems", ctx, userID).Return(nil)

	err := service.ExpireTrial(ctx, userID)

	require.NoError(t, err)
	// Verify Plaid cleanup was called
	plaidCleanup.AssertCalled(t, "DisconnectAllUserItems", ctx, userID)
}

func TestTrialService_CanGrantTrial_True(t *testing.T) {
	service, _, _ := setupTestTrialService(t)

	user := &domain.User{
		ID:       "user-123",
		HadTrial: false,
	}

	canGrant := service.CanGrantTrial(user)

	assert.True(t, canGrant)
}

func TestTrialService_CanGrantTrial_False(t *testing.T) {
	service, _, _ := setupTestTrialService(t)

	user := &domain.User{
		ID:       "user-123",
		HadTrial: true,
	}

	canGrant := service.CanGrantTrial(user)

	assert.False(t, canGrant)
}

// Issue #1015: TrialService must be constructable with nil plaidCleanup and wirable via setter.
// This enables River initialization to be decoupled from Plaid configuration.

func TestTrialService_SetPlaidCleanup(t *testing.T) {
	tierConfig, err := config.NewTierConfig("")
	require.NoError(t, err)
	repo := new(mockTrialRepository)

	// Create with nil plaidCleanup — valid when Plaid not configured
	svc := NewTrialService(tierConfig, repo, nil)

	// Wire cleanup after Plaid initializes via setter
	cleanup := new(mockPlaidCleanupService)
	svc.SetPlaidCleanup(cleanup)
	assert.Equal(t, cleanup, svc.plaidCleanup, "plaidCleanup should be set after SetPlaidCleanup")
}

func TestTrialService_ExpireTrial_NilPlaidCleanup(t *testing.T) {
	tierConfig, err := config.NewTierConfig("")
	require.NoError(t, err)
	repo := new(mockTrialRepository)

	// Create with nil plaidCleanup (no Plaid configured)
	svc := NewTrialService(tierConfig, repo, nil)

	ctx := context.Background()
	userID := "user-456"
	trialEndsAt := time.Now().Add(-1 * time.Hour)
	user := &domain.User{
		ID:                 userID,
		SubscriptionStatus: "trial",
		TrialEndsAt:        &trialEndsAt,
	}

	repo.On("GetUserByID", ctx, userID).Return(user, nil)
	repo.On("UpdateSubscriptionStatus", ctx, userID, "inactive").Return(nil)
	repo.On("SetRetentionDates", ctx, userID, mock.Anything, mock.Anything).Return(nil)

	// Should not panic and should succeed — plaidCleanup nil-guard in ExpireTrial
	err = svc.ExpireTrial(ctx, userID)

	require.NoError(t, err)
	repo.AssertExpectations(t)
	// No DisconnectAllUserItems call expected since plaidCleanup is nil
}
