// Package jobs contains River job definitions and workers for async processing.
// Issue #679, #680: Trial expiration check job tests
package jobs

import (
	"context"
	"errors"
	"log/slog"
	"testing"
	"time"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/queue"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// MockTrialProfileRepository mocks the TrialProfileRepository interface for tests
type MockTrialProfileRepository struct {
	mock.Mock
}

func (m *MockTrialProfileRepository) GetUsersWithExpiredTrials(ctx context.Context) ([]*domain.User, error) {
	args := m.Called(ctx)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]*domain.User), args.Error(1)
}

// MockTrialService mocks the TrialService interface for tests
type MockTrialService struct {
	mock.Mock
}

func (m *MockTrialService) GrantTrial(ctx context.Context, userID string) error {
	args := m.Called(ctx, userID)
	return args.Error(0)
}

func (m *MockTrialService) IsTrialActive(user *domain.User) bool {
	args := m.Called(user)
	return args.Bool(0)
}

func (m *MockTrialService) GetTrialDaysRemaining(user *domain.User) int {
	args := m.Called(user)
	return args.Int(0)
}

func (m *MockTrialService) ExpireTrial(ctx context.Context, userID string) error {
	args := m.Called(ctx, userID)
	return args.Error(0)
}

func (m *MockTrialService) CanGrantTrial(user *domain.User) bool {
	args := m.Called(user)
	return args.Bool(0)
}

// Test helpers
func newTestTrialExpirationCheckLogger() *slog.Logger {
	return slog.Default()
}

func newTestTrialExpirationCheckJob(args TrialExpirationCheckArgs) *river.Job[TrialExpirationCheckArgs] {
	return &river.Job[TrialExpirationCheckArgs]{
		JobRow: &rivertype.JobRow{
			ID:      1,
			Attempt: 1,
		},
		Args: args,
	}
}

func TestTrialExpirationCheckArgs_Kind(t *testing.T) {
	args := TrialExpirationCheckArgs{}
	assert.Equal(t, "trial_expiration_check", args.Kind())
}

func TestTrialExpirationCheckWorker_Work_NoExpiredTrials(t *testing.T) {
	ctx := context.Background()

	mockRepo := new(MockTrialProfileRepository)
	mockTrialSvc := new(MockTrialService)

	mockRepo.On("GetUsersWithExpiredTrials", ctx).Return([]*domain.User{}, nil)

	worker := NewTrialExpirationCheckWorker(TrialExpirationCheckWorkerDeps{
		ProfileRepo:  mockRepo,
		TrialService: mockTrialSvc,
		Logger:       newTestTrialExpirationCheckLogger(),
	})

	job := newTestTrialExpirationCheckJob(TrialExpirationCheckArgs{
		TriggerSource: "periodic",
	})

	err := worker.Work(ctx, job)

	require.NoError(t, err)
	mockRepo.AssertExpectations(t)
	mockTrialSvc.AssertNotCalled(t, "ExpireTrial")
}

func TestTrialExpirationCheckWorker_Work_ExpiresEachUser(t *testing.T) {
	ctx := context.Background()

	mockRepo := new(MockTrialProfileRepository)
	mockTrialSvc := new(MockTrialService)

	trialEndsAt := time.Now().Add(-24 * time.Hour)
	users := []*domain.User{
		{ID: "user-1", SubscriptionStatus: "trial", TrialEndsAt: &trialEndsAt},
		{ID: "user-2", SubscriptionStatus: "trial", TrialEndsAt: &trialEndsAt},
		{ID: "user-3", SubscriptionStatus: "trial", TrialEndsAt: &trialEndsAt},
	}

	mockRepo.On("GetUsersWithExpiredTrials", ctx).Return(users, nil)
	mockTrialSvc.On("ExpireTrial", ctx, "user-1").Return(nil)
	mockTrialSvc.On("ExpireTrial", ctx, "user-2").Return(nil)
	mockTrialSvc.On("ExpireTrial", ctx, "user-3").Return(nil)

	worker := NewTrialExpirationCheckWorker(TrialExpirationCheckWorkerDeps{
		ProfileRepo:  mockRepo,
		TrialService: mockTrialSvc,
		Logger:       newTestTrialExpirationCheckLogger(),
	})

	job := newTestTrialExpirationCheckJob(TrialExpirationCheckArgs{
		TriggerSource: "periodic",
	})

	err := worker.Work(ctx, job)

	require.NoError(t, err)
	mockRepo.AssertExpectations(t)
	mockTrialSvc.AssertNumberOfCalls(t, "ExpireTrial", 3)
}

func TestTrialExpirationCheckWorker_Work_RepositoryError(t *testing.T) {
	ctx := context.Background()

	mockRepo := new(MockTrialProfileRepository)
	mockTrialSvc := new(MockTrialService)

	dbErr := errors.New("database connection error")
	mockRepo.On("GetUsersWithExpiredTrials", ctx).Return(nil, dbErr)

	worker := NewTrialExpirationCheckWorker(TrialExpirationCheckWorkerDeps{
		ProfileRepo:  mockRepo,
		TrialService: mockTrialSvc,
		Logger:       newTestTrialExpirationCheckLogger(),
	})

	job := newTestTrialExpirationCheckJob(TrialExpirationCheckArgs{
		TriggerSource: "periodic",
	})

	err := worker.Work(ctx, job)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to fetch users with expired trials")
	mockRepo.AssertExpectations(t)
	mockTrialSvc.AssertNotCalled(t, "ExpireTrial")
}

func TestTrialExpirationCheckWorker_Work_ExpireErrorContinues(t *testing.T) {
	ctx := context.Background()

	mockRepo := new(MockTrialProfileRepository)
	mockTrialSvc := new(MockTrialService)

	trialEndsAt := time.Now().Add(-24 * time.Hour)
	users := []*domain.User{
		{ID: "user-1", SubscriptionStatus: "trial", TrialEndsAt: &trialEndsAt},
		{ID: "user-2", SubscriptionStatus: "trial", TrialEndsAt: &trialEndsAt},
	}

	mockRepo.On("GetUsersWithExpiredTrials", ctx).Return(users, nil)
	// First user fails to expire
	mockTrialSvc.On("ExpireTrial", ctx, "user-1").Return(errors.New("plaid API error"))
	// Second user succeeds
	mockTrialSvc.On("ExpireTrial", ctx, "user-2").Return(nil)

	worker := NewTrialExpirationCheckWorker(TrialExpirationCheckWorkerDeps{
		ProfileRepo:  mockRepo,
		TrialService: mockTrialSvc,
		Logger:       newTestTrialExpirationCheckLogger(),
	})

	job := newTestTrialExpirationCheckJob(TrialExpirationCheckArgs{
		TriggerSource: "manual",
	})

	err := worker.Work(ctx, job)

	// Should not fail even though one expiration failed
	require.NoError(t, err)
	mockRepo.AssertExpectations(t)
	mockTrialSvc.AssertNumberOfCalls(t, "ExpireTrial", 2)
}

func TestTrialExpirationCheckUniqueOpts(t *testing.T) {
	opts := TrialExpirationCheckUniqueOpts()

	assert.False(t, opts.ByArgs) // All check jobs are equivalent
	assert.Equal(t, 1*60*60*1000000000, int(opts.ByPeriod.Nanoseconds())) // 1 hour
	assert.Contains(t, opts.ByState, queue.JobStateAvailable)
	assert.Contains(t, opts.ByState, queue.JobStateScheduled)
	assert.Contains(t, opts.ByState, queue.JobStateRunning)
	assert.Contains(t, opts.ByState, queue.JobStateRetryable)
	assert.Contains(t, opts.ByState, queue.JobStatePending)
}

func TestNewTrialExpirationCheckWorker_DefaultLogger(t *testing.T) {
	mockRepo := new(MockTrialProfileRepository)
	mockTrialSvc := new(MockTrialService)

	worker := NewTrialExpirationCheckWorker(TrialExpirationCheckWorkerDeps{
		ProfileRepo:  mockRepo,
		TrialService: mockTrialSvc,
		// Logger is nil, should use default
	})

	assert.NotNil(t, worker.logger)
}
