// Package jobs contains River job definitions and workers for async processing.
// Issue #762: Paddle reconciliation job tests
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

func newReconciliationWorker(repo ReconciliationProfileRepository) *PaddleReconciliationWorker {
	return NewPaddleReconciliationWorker(PaddleReconciliationWorkerDeps{
		ProfileRepo: repo,
		Logger:      slog.Default(),
	})
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

	worker := newReconciliationWorker(mockRepo)
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
	worker := newReconciliationWorker(mockRepo)
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

	worker := newReconciliationWorker(mockRepo)
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
