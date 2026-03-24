// Package jobs contains River job definitions and workers for async processing.
// Issue #762: Paddle reconciliation job tests
package jobs

import (
	"context"
	"errors"
	"log/slog"
	"testing"
	"time"

	paddle "github.com/PaddleHQ/paddle-go-sdk/v4"
	"github.com/maxnussbaum/BoostedBudget/internal/config"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/queue"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// MockReconciliationProfileRepository mocks ReconciliationProfileRepository.
type MockReconciliationProfileRepository struct {
	mock.Mock
}

func (m *MockReconciliationProfileRepository) GetUsersWithActivePaddleSubscriptions(ctx context.Context) ([]*domain.User, error) {
	args := m.Called(ctx)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]*domain.User), args.Error(1)
}

func (m *MockReconciliationProfileRepository) UpdateSubscriptionFromPaddle(ctx context.Context, userID, tier, status string, startedAt, endsAt *time.Time, subscriptionID *string) error {
	args := m.Called(ctx, userID, tier, status, startedAt, endsAt, subscriptionID)
	return args.Error(0)
}

// MockReconciliationPaddleService mocks ReconciliationPaddleService.
type MockReconciliationPaddleService struct {
	mock.Mock
}

func (m *MockReconciliationPaddleService) GetSubscription(ctx context.Context, subscriptionID string) (*paddle.Subscription, error) {
	args := m.Called(ctx, subscriptionID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*paddle.Subscription), args.Error(1)
}

// Test helpers

func newTestPaddleReconciliationJob(args PaddleReconciliationArgs) *river.Job[PaddleReconciliationArgs] {
	return &river.Job[PaddleReconciliationArgs]{
		JobRow: &rivertype.JobRow{
			ID:      1,
			Attempt: 1,
		},
		Args: args,
	}
}

func newTestWorker(deps PaddleReconciliationWorkerDeps) *PaddleReconciliationWorker {
	if deps.Logger == nil {
		deps.Logger = slog.Default()
	}
	return NewPaddleReconciliationWorker(deps)
}

// newTestTierConfig builds a minimal TierConfig for reconciliation tests.
// Maps "pri_test_monthly" → "premium" and "pri_household2_monthly" → "household_2".
// Built directly (without YAML loading) so no file I/O is needed in unit tests.
func newTestTierConfig() *config.TierConfig {
	premiumMonthly := "pri_test_monthly"
	household2Monthly := "pri_household2_monthly"
	return &config.TierConfig{
		Tiers: map[domain.TierName]domain.SubscriptionTier{
			domain.TierPremium: {
				Name:        domain.TierPremium,
				DisplayName: "Premium",
				Pricing: domain.SubscriptionPricing{
					PaddlePriceIDMonthly: &premiumMonthly,
				},
			},
			domain.TierHousehold2: {
				Name:        domain.TierHousehold2,
				DisplayName: "Household 2",
				Pricing: domain.SubscriptionPricing{
					PaddlePriceIDMonthly: &household2Monthly,
				},
			},
		},
		DefaultTier: domain.TierPremium,
	}
}

// --- Tests ---

func TestPaddleReconciliationArgs_Kind(t *testing.T) {
	args := PaddleReconciliationArgs{}
	assert.Equal(t, "paddle_reconciliation", args.Kind())
}

func TestPaddleReconciliationWorker_NoUsers(t *testing.T) {
	ctx := context.Background()
	mockRepo := new(MockReconciliationProfileRepository)
	mockRepo.On("GetUsersWithActivePaddleSubscriptions", ctx).Return([]*domain.User{}, nil)

	worker := newTestWorker(PaddleReconciliationWorkerDeps{ProfileRepo: mockRepo})
	job := newTestPaddleReconciliationJob(PaddleReconciliationArgs{TriggerSource: "periodic"})

	err := worker.Work(ctx, job)

	require.NoError(t, err)
	mockRepo.AssertExpectations(t)
}

func TestPaddleReconciliationWorker_NilPaddleService_SkipsRemoteCheck(t *testing.T) {
	ctx := context.Background()
	mockRepo := new(MockReconciliationProfileRepository)

	subID := "sub_123"
	users := []*domain.User{
		{ID: "user-1", PaddleSubscriptionID: &subID, SubscriptionStatus: "active"},
	}
	mockRepo.On("GetUsersWithActivePaddleSubscriptions", ctx).Return(users, nil)

	// paddleSvc is nil (not set in deps)
	worker := newTestWorker(PaddleReconciliationWorkerDeps{ProfileRepo: mockRepo})
	job := newTestPaddleReconciliationJob(PaddleReconciliationArgs{TriggerSource: "manual"})

	// Should succeed even without a Paddle service
	err := worker.Work(ctx, job)

	require.NoError(t, err)
	mockRepo.AssertExpectations(t)
}

func TestPaddleReconciliationWorker_ProfileRepoError_FailsJob(t *testing.T) {
	ctx := context.Background()
	mockRepo := new(MockReconciliationProfileRepository)

	dbErr := errors.New("database connection error")
	mockRepo.On("GetUsersWithActivePaddleSubscriptions", ctx).Return(nil, dbErr)

	worker := newTestWorker(PaddleReconciliationWorkerDeps{ProfileRepo: mockRepo})
	job := newTestPaddleReconciliationJob(PaddleReconciliationArgs{TriggerSource: "periodic"})

	err := worker.Work(ctx, job)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to fetch users with active subscriptions")
	mockRepo.AssertExpectations(t)
}

func TestPaddleReconciliationUniqueOpts_Period(t *testing.T) {
	opts := PaddleReconciliationUniqueOpts()

	assert.False(t, opts.ByArgs)
	assert.Equal(t, 12*time.Hour, opts.ByPeriod)
	assert.Contains(t, opts.ByState, queue.JobStateAvailable)
	assert.Contains(t, opts.ByState, queue.JobStateScheduled)
	assert.Contains(t, opts.ByState, queue.JobStateRunning)
	assert.Contains(t, opts.ByState, queue.JobStateRetryable)
	assert.Contains(t, opts.ByState, queue.JobStatePending)
}

func TestNewPaddleReconciliationWorker_DefaultLogger(t *testing.T) {
	mockRepo := new(MockReconciliationProfileRepository)

	worker := NewPaddleReconciliationWorker(PaddleReconciliationWorkerDeps{
		ProfileRepo: mockRepo,
		// Logger is nil, should use default
	})

	assert.NotNil(t, worker.logger)
}

// --- Reconciliation tests with Paddle service (Tests 7-16) ---

// TestReconciliation_AllMatched verifies that when all local subscriptions match
// Paddle's remote state, no updates are applied and no error is returned.
func TestReconciliation_AllMatched(t *testing.T) {
	ctx := context.Background()
	mockRepo := new(MockReconciliationProfileRepository)
	mockPaddle := new(MockReconciliationPaddleService)
	tierConfig := newTestTierConfig()

	sub1 := "sub_111"
	sub2 := "sub_222"
	users := []*domain.User{
		{ID: "user-1", PaddleSubscriptionID: &sub1, SubscriptionStatus: "active", SubscriptionTier: "premium"},
		{ID: "user-2", PaddleSubscriptionID: &sub2, SubscriptionStatus: "active", SubscriptionTier: "premium"},
	}
	mockRepo.On("GetUsersWithActivePaddleSubscriptions", ctx).Return(users, nil)

	matchingSub := &paddle.Subscription{
		Status: paddle.SubscriptionStatusActive,
		Items: []paddle.SubscriptionItem{
			{Price: paddle.Price{ID: "pri_test_monthly"}},
		},
	}
	mockPaddle.On("GetSubscription", ctx, "sub_111").Return(matchingSub, nil)
	mockPaddle.On("GetSubscription", ctx, "sub_222").Return(matchingSub, nil)

	worker := newTestWorker(PaddleReconciliationWorkerDeps{
		ProfileRepo: mockRepo,
		PaddleSvc:   mockPaddle,
		TierConfig:  tierConfig,
		AutoFix:     false,
	})
	job := newTestPaddleReconciliationJob(PaddleReconciliationArgs{TriggerSource: "periodic"})

	err := worker.Work(ctx, job)

	require.NoError(t, err)
	mockRepo.AssertNotCalled(t, "UpdateSubscriptionFromPaddle")
	mockRepo.AssertExpectations(t)
	mockPaddle.AssertExpectations(t)
}

// TestReconciliation_StatusMismatch_NoAutoFix verifies that a status mismatch
// is detected but no update is applied when autoFix is false.
func TestReconciliation_StatusMismatch_NoAutoFix(t *testing.T) {
	ctx := context.Background()
	mockRepo := new(MockReconciliationProfileRepository)
	mockPaddle := new(MockReconciliationPaddleService)
	tierConfig := newTestTierConfig()

	sub1 := "sub_111"
	users := []*domain.User{
		{ID: "user-1", PaddleSubscriptionID: &sub1, SubscriptionStatus: "active", SubscriptionTier: "premium"},
	}
	mockRepo.On("GetUsersWithActivePaddleSubscriptions", ctx).Return(users, nil)

	canceledSub := &paddle.Subscription{
		Status: paddle.SubscriptionStatusCanceled,
		Items: []paddle.SubscriptionItem{
			{Price: paddle.Price{ID: "pri_test_monthly"}},
		},
	}
	mockPaddle.On("GetSubscription", ctx, "sub_111").Return(canceledSub, nil)

	worker := newTestWorker(PaddleReconciliationWorkerDeps{
		ProfileRepo: mockRepo,
		PaddleSvc:   mockPaddle,
		TierConfig:  tierConfig,
		AutoFix:     false,
	})
	job := newTestPaddleReconciliationJob(PaddleReconciliationArgs{TriggerSource: "periodic"})

	err := worker.Work(ctx, job)

	require.NoError(t, err)
	mockRepo.AssertNotCalled(t, "UpdateSubscriptionFromPaddle")
	mockRepo.AssertExpectations(t)
	mockPaddle.AssertExpectations(t)
}

// TestReconciliation_TierMismatch_NoAutoFix verifies that a tier mismatch
// is detected but no update is applied when autoFix is false.
func TestReconciliation_TierMismatch_NoAutoFix(t *testing.T) {
	ctx := context.Background()
	mockRepo := new(MockReconciliationProfileRepository)
	mockPaddle := new(MockReconciliationPaddleService)
	tierConfig := newTestTierConfig()

	sub1 := "sub_111"
	users := []*domain.User{
		{ID: "user-1", PaddleSubscriptionID: &sub1, SubscriptionStatus: "active", SubscriptionTier: "premium"},
	}
	mockRepo.On("GetUsersWithActivePaddleSubscriptions", ctx).Return(users, nil)

	household2Sub := &paddle.Subscription{
		Status: paddle.SubscriptionStatusActive,
		Items: []paddle.SubscriptionItem{
			{Price: paddle.Price{ID: "pri_household2_monthly"}},
		},
	}
	mockPaddle.On("GetSubscription", ctx, "sub_111").Return(household2Sub, nil)

	worker := newTestWorker(PaddleReconciliationWorkerDeps{
		ProfileRepo: mockRepo,
		PaddleSvc:   mockPaddle,
		TierConfig:  tierConfig,
		AutoFix:     false,
	})
	job := newTestPaddleReconciliationJob(PaddleReconciliationArgs{TriggerSource: "periodic"})

	err := worker.Work(ctx, job)

	require.NoError(t, err)
	mockRepo.AssertNotCalled(t, "UpdateSubscriptionFromPaddle")
	mockRepo.AssertExpectations(t)
	mockPaddle.AssertExpectations(t)
}

// TestReconciliation_StatusMismatch_AutoFix verifies that a status mismatch
// triggers an update when autoFix is true. Paddle's "canceled" is normalized
// to the domain spelling "cancelled".
func TestReconciliation_StatusMismatch_AutoFix(t *testing.T) {
	ctx := context.Background()
	mockRepo := new(MockReconciliationProfileRepository)
	mockPaddle := new(MockReconciliationPaddleService)
	tierConfig := newTestTierConfig()

	sub1 := "sub_111"
	users := []*domain.User{
		{ID: "user-1", PaddleSubscriptionID: &sub1, SubscriptionStatus: "active", SubscriptionTier: "premium"},
	}
	mockRepo.On("GetUsersWithActivePaddleSubscriptions", ctx).Return(users, nil)

	canceledSub := &paddle.Subscription{
		Status: paddle.SubscriptionStatusCanceled,
		Items: []paddle.SubscriptionItem{
			{Price: paddle.Price{ID: "pri_test_monthly"}},
		},
	}
	mockPaddle.On("GetSubscription", ctx, "sub_111").Return(canceledSub, nil)

	subID := "sub_111"
	mockRepo.On("UpdateSubscriptionFromPaddle", ctx, "user-1", "premium", "cancelled", (*time.Time)(nil), (*time.Time)(nil), &subID).Return(nil)

	worker := newTestWorker(PaddleReconciliationWorkerDeps{
		ProfileRepo: mockRepo,
		PaddleSvc:   mockPaddle,
		TierConfig:  tierConfig,
		AutoFix:     true,
	})
	job := newTestPaddleReconciliationJob(PaddleReconciliationArgs{TriggerSource: "periodic"})

	err := worker.Work(ctx, job)

	require.NoError(t, err)
	mockRepo.AssertExpectations(t)
	mockPaddle.AssertExpectations(t)
}

// TestReconciliation_TierMismatch_AutoFix verifies that a tier mismatch
// triggers an update when autoFix is true.
func TestReconciliation_TierMismatch_AutoFix(t *testing.T) {
	ctx := context.Background()
	mockRepo := new(MockReconciliationProfileRepository)
	mockPaddle := new(MockReconciliationPaddleService)
	tierConfig := newTestTierConfig()

	sub1 := "sub_111"
	users := []*domain.User{
		{ID: "user-1", PaddleSubscriptionID: &sub1, SubscriptionStatus: "active", SubscriptionTier: "premium"},
	}
	mockRepo.On("GetUsersWithActivePaddleSubscriptions", ctx).Return(users, nil)

	household2Sub := &paddle.Subscription{
		Status: paddle.SubscriptionStatusActive,
		Items: []paddle.SubscriptionItem{
			{Price: paddle.Price{ID: "pri_household2_monthly"}},
		},
	}
	mockPaddle.On("GetSubscription", ctx, "sub_111").Return(household2Sub, nil)

	subID := "sub_111"
	mockRepo.On("UpdateSubscriptionFromPaddle", ctx, "user-1", "household_2", "active", (*time.Time)(nil), (*time.Time)(nil), &subID).Return(nil)

	worker := newTestWorker(PaddleReconciliationWorkerDeps{
		ProfileRepo: mockRepo,
		PaddleSvc:   mockPaddle,
		TierConfig:  tierConfig,
		AutoFix:     true,
	})
	job := newTestPaddleReconciliationJob(PaddleReconciliationArgs{TriggerSource: "periodic"})

	err := worker.Work(ctx, job)

	require.NoError(t, err)
	mockRepo.AssertExpectations(t)
	mockPaddle.AssertExpectations(t)
}

// TestReconciliation_BothMismatch_AutoFix verifies that when both tier and status
// mismatch, a single update call is made with the corrected tier and status.
func TestReconciliation_BothMismatch_AutoFix(t *testing.T) {
	ctx := context.Background()
	mockRepo := new(MockReconciliationProfileRepository)
	mockPaddle := new(MockReconciliationPaddleService)
	tierConfig := newTestTierConfig()

	sub1 := "sub_111"
	users := []*domain.User{
		{ID: "user-1", PaddleSubscriptionID: &sub1, SubscriptionStatus: "active", SubscriptionTier: "premium"},
	}
	mockRepo.On("GetUsersWithActivePaddleSubscriptions", ctx).Return(users, nil)

	pausedHousehold2Sub := &paddle.Subscription{
		Status: paddle.SubscriptionStatusPaused,
		Items: []paddle.SubscriptionItem{
			{Price: paddle.Price{ID: "pri_household2_monthly"}},
		},
	}
	mockPaddle.On("GetSubscription", ctx, "sub_111").Return(pausedHousehold2Sub, nil)

	subID := "sub_111"
	mockRepo.On("UpdateSubscriptionFromPaddle", ctx, "user-1", "household_2", "paused", (*time.Time)(nil), (*time.Time)(nil), &subID).Return(nil)

	worker := newTestWorker(PaddleReconciliationWorkerDeps{
		ProfileRepo: mockRepo,
		PaddleSvc:   mockPaddle,
		TierConfig:  tierConfig,
		AutoFix:     true,
	})
	job := newTestPaddleReconciliationJob(PaddleReconciliationArgs{TriggerSource: "periodic"})

	err := worker.Work(ctx, job)

	require.NoError(t, err)
	mockRepo.AssertNumberOfCalls(t, "UpdateSubscriptionFromPaddle", 1)
	mockRepo.AssertExpectations(t)
	mockPaddle.AssertExpectations(t)
}

// TestReconciliation_AutoFix_UpdateError verifies that an error from the repo
// during auto-fix does not fail the job — the error is logged and processing continues.
func TestReconciliation_AutoFix_UpdateError(t *testing.T) {
	ctx := context.Background()
	mockRepo := new(MockReconciliationProfileRepository)
	mockPaddle := new(MockReconciliationPaddleService)
	tierConfig := newTestTierConfig()

	sub1 := "sub_111"
	users := []*domain.User{
		{ID: "user-1", PaddleSubscriptionID: &sub1, SubscriptionStatus: "active", SubscriptionTier: "premium"},
	}
	mockRepo.On("GetUsersWithActivePaddleSubscriptions", ctx).Return(users, nil)

	canceledSub := &paddle.Subscription{
		Status: paddle.SubscriptionStatusCanceled,
		Items: []paddle.SubscriptionItem{
			{Price: paddle.Price{ID: "pri_test_monthly"}},
		},
	}
	mockPaddle.On("GetSubscription", ctx, "sub_111").Return(canceledSub, nil)

	subID := "sub_111"
	dbErr := errors.New("update failed")
	mockRepo.On("UpdateSubscriptionFromPaddle", ctx, "user-1", "premium", "cancelled", (*time.Time)(nil), (*time.Time)(nil), &subID).Return(dbErr)

	worker := newTestWorker(PaddleReconciliationWorkerDeps{
		ProfileRepo: mockRepo,
		PaddleSvc:   mockPaddle,
		TierConfig:  tierConfig,
		AutoFix:     true,
	})
	job := newTestPaddleReconciliationJob(PaddleReconciliationArgs{TriggerSource: "periodic"})

	err := worker.Work(ctx, job)

	require.NoError(t, err)
	mockRepo.AssertCalled(t, "UpdateSubscriptionFromPaddle", ctx, "user-1", "premium", "cancelled", (*time.Time)(nil), (*time.Time)(nil), &subID)
	mockRepo.AssertExpectations(t)
	mockPaddle.AssertExpectations(t)
}

// TestReconciliation_PaddleAPIError_Continues verifies that a Paddle API error
// for one user does not stop processing of remaining users.
func TestReconciliation_PaddleAPIError_Continues(t *testing.T) {
	ctx := context.Background()
	mockRepo := new(MockReconciliationProfileRepository)
	mockPaddle := new(MockReconciliationPaddleService)
	tierConfig := newTestTierConfig()

	sub1 := "sub_111"
	sub2 := "sub_222"
	users := []*domain.User{
		{ID: "user-1", PaddleSubscriptionID: &sub1, SubscriptionStatus: "active", SubscriptionTier: "premium"},
		{ID: "user-2", PaddleSubscriptionID: &sub2, SubscriptionStatus: "active", SubscriptionTier: "premium"},
	}
	mockRepo.On("GetUsersWithActivePaddleSubscriptions", ctx).Return(users, nil)

	apiErr := errors.New("paddle api unavailable")
	mockPaddle.On("GetSubscription", ctx, "sub_111").Return((*paddle.Subscription)(nil), apiErr)

	matchingSub := &paddle.Subscription{
		Status: paddle.SubscriptionStatusActive,
		Items: []paddle.SubscriptionItem{
			{Price: paddle.Price{ID: "pri_test_monthly"}},
		},
	}
	mockPaddle.On("GetSubscription", ctx, "sub_222").Return(matchingSub, nil)

	worker := newTestWorker(PaddleReconciliationWorkerDeps{
		ProfileRepo: mockRepo,
		PaddleSvc:   mockPaddle,
		TierConfig:  tierConfig,
		AutoFix:     false,
	})
	job := newTestPaddleReconciliationJob(PaddleReconciliationArgs{TriggerSource: "periodic"})

	err := worker.Work(ctx, job)

	require.NoError(t, err)
	mockRepo.AssertNotCalled(t, "UpdateSubscriptionFromPaddle")
	mockRepo.AssertExpectations(t)
	mockPaddle.AssertExpectations(t)
}

// TestReconciliation_CanceledSpellingNormalized verifies that Paddle's "canceled"
// (single-l) is treated as equivalent to the domain's "cancelled" (double-l),
// so a locally-cancelled user with a Paddle-canceled subscription is considered matched.
func TestReconciliation_CanceledSpellingNormalized(t *testing.T) {
	ctx := context.Background()
	mockRepo := new(MockReconciliationProfileRepository)
	mockPaddle := new(MockReconciliationPaddleService)
	tierConfig := newTestTierConfig()

	sub1 := "sub_111"
	users := []*domain.User{
		// Local uses domain spelling "cancelled" (double-l)
		{ID: "user-1", PaddleSubscriptionID: &sub1, SubscriptionStatus: "cancelled", SubscriptionTier: "premium"},
	}
	mockRepo.On("GetUsersWithActivePaddleSubscriptions", ctx).Return(users, nil)

	// Paddle uses "canceled" (single-l)
	canceledSub := &paddle.Subscription{
		Status: paddle.SubscriptionStatusCanceled,
		Items: []paddle.SubscriptionItem{
			{Price: paddle.Price{ID: "pri_test_monthly"}},
		},
	}
	mockPaddle.On("GetSubscription", ctx, "sub_111").Return(canceledSub, nil)

	worker := newTestWorker(PaddleReconciliationWorkerDeps{
		ProfileRepo: mockRepo,
		PaddleSvc:   mockPaddle,
		TierConfig:  tierConfig,
		AutoFix:     false,
	})
	job := newTestPaddleReconciliationJob(PaddleReconciliationArgs{TriggerSource: "periodic"})

	err := worker.Work(ctx, job)

	require.NoError(t, err)
	mockRepo.AssertNotCalled(t, "UpdateSubscriptionFromPaddle")
	mockRepo.AssertExpectations(t)
	mockPaddle.AssertExpectations(t)
}

// TestReconciliation_EmptyItems_SkipsTierCheck verifies that when Paddle returns
// a subscription with no items (tier unresolvable), the tier check is skipped and
// only the status is compared. A matching status means no update is needed.
func TestReconciliation_EmptyItems_SkipsTierCheck(t *testing.T) {
	ctx := context.Background()
	mockRepo := new(MockReconciliationProfileRepository)
	mockPaddle := new(MockReconciliationPaddleService)
	tierConfig := newTestTierConfig()

	sub1 := "sub_111"
	users := []*domain.User{
		{ID: "user-1", PaddleSubscriptionID: &sub1, SubscriptionStatus: "active", SubscriptionTier: "premium"},
	}
	mockRepo.On("GetUsersWithActivePaddleSubscriptions", ctx).Return(users, nil)

	// Subscription with no items — tier is unresolvable
	activeSub := &paddle.Subscription{
		Status: paddle.SubscriptionStatusActive,
		Items:  []paddle.SubscriptionItem{},
	}
	mockPaddle.On("GetSubscription", ctx, "sub_111").Return(activeSub, nil)

	worker := newTestWorker(PaddleReconciliationWorkerDeps{
		ProfileRepo: mockRepo,
		PaddleSvc:   mockPaddle,
		TierConfig:  tierConfig,
		AutoFix:     false,
	})
	job := newTestPaddleReconciliationJob(PaddleReconciliationArgs{TriggerSource: "periodic"})

	err := worker.Work(ctx, job)

	require.NoError(t, err)
	mockRepo.AssertNotCalled(t, "UpdateSubscriptionFromPaddle")
	mockRepo.AssertExpectations(t)
	mockPaddle.AssertExpectations(t)
}
