package jobs

import (
	"context"
	"errors"
	"log/slog"
	"testing"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/queue"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// MockProfileRepository mocks the ProfileRepository interface for tests
type MockProfileRepository struct {
	mock.Mock
}

func (m *MockProfileRepository) GetUsersScheduledForDeletion(ctx context.Context) ([]*domain.User, error) {
	args := m.Called(ctx)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]*domain.User), args.Error(1)
}

// Note: MockJobQueue is defined in plaid_sync_test.go and reused here

// Test helpers
func newTestDeletionCheckLogger() *slog.Logger {
	return slog.Default()
}

func newTestScheduledDeletionCheckJob(args ScheduledDeletionCheckArgs) *river.Job[ScheduledDeletionCheckArgs] {
	return &river.Job[ScheduledDeletionCheckArgs]{
		JobRow: &rivertype.JobRow{
			ID:      1,
			Attempt: 1,
		},
		Args: args,
	}
}

func TestScheduledDeletionCheckArgs_Kind(t *testing.T) {
	args := ScheduledDeletionCheckArgs{}
	assert.Equal(t, "scheduled_deletion_check", args.Kind())
}

func TestScheduledDeletionCheckWorker_Work_NoUsersScheduled(t *testing.T) {
	ctx := context.Background()

	mockRepo := new(MockProfileRepository)
	mockQueue := new(MockJobQueue)

	mockRepo.On("GetUsersScheduledForDeletion", ctx).Return([]*domain.User{}, nil)

	worker := NewScheduledDeletionCheckWorker(ScheduledDeletionCheckWorkerDeps{
		ProfileRepo: mockRepo,
		JobQueue:    mockQueue,
		Logger:      newTestDeletionCheckLogger(),
	})

	job := newTestScheduledDeletionCheckJob(ScheduledDeletionCheckArgs{
		TriggerSource: "periodic",
	})

	err := worker.Work(ctx, job)

	require.NoError(t, err)
	mockRepo.AssertExpectations(t)
	mockQueue.AssertNotCalled(t, "Enqueue")
}

func TestScheduledDeletionCheckWorker_Work_EnqueuesForEachUser(t *testing.T) {
	ctx := context.Background()

	mockRepo := new(MockProfileRepository)
	mockQueue := new(MockJobQueue)

	users := []*domain.User{
		{ID: "user-1"},
		{ID: "user-2"},
		{ID: "user-3"},
	}

	mockRepo.On("GetUsersScheduledForDeletion", ctx).Return(users, nil)
	mockQueue.On("Enqueue", ctx, mock.MatchedBy(func(job queue.Job) bool {
		deletionArgs, ok := job.(AccountDeletionArgs)
		return ok && (deletionArgs.UserID == "user-1" || deletionArgs.UserID == "user-2" || deletionArgs.UserID == "user-3")
	}), mock.Anything).Return(&queue.JobResult{ID: 1}, nil)

	worker := NewScheduledDeletionCheckWorker(ScheduledDeletionCheckWorkerDeps{
		ProfileRepo: mockRepo,
		JobQueue:    mockQueue,
		Logger:      newTestDeletionCheckLogger(),
	})

	job := newTestScheduledDeletionCheckJob(ScheduledDeletionCheckArgs{
		TriggerSource: "periodic",
	})

	err := worker.Work(ctx, job)

	require.NoError(t, err)
	mockRepo.AssertExpectations(t)
	mockQueue.AssertNumberOfCalls(t, "Enqueue", 3)
}

func TestScheduledDeletionCheckWorker_Work_RepositoryError(t *testing.T) {
	ctx := context.Background()

	mockRepo := new(MockProfileRepository)
	mockQueue := new(MockJobQueue)

	dbErr := errors.New("database connection error")
	mockRepo.On("GetUsersScheduledForDeletion", ctx).Return(nil, dbErr)

	worker := NewScheduledDeletionCheckWorker(ScheduledDeletionCheckWorkerDeps{
		ProfileRepo: mockRepo,
		JobQueue:    mockQueue,
		Logger:      newTestDeletionCheckLogger(),
	})

	job := newTestScheduledDeletionCheckJob(ScheduledDeletionCheckArgs{
		TriggerSource: "periodic",
	})

	err := worker.Work(ctx, job)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to fetch users scheduled for deletion")
	mockRepo.AssertExpectations(t)
	mockQueue.AssertNotCalled(t, "Enqueue")
}

func TestScheduledDeletionCheckWorker_Work_EnqueueErrorContinues(t *testing.T) {
	ctx := context.Background()

	mockRepo := new(MockProfileRepository)
	mockQueue := new(MockJobQueue)

	users := []*domain.User{
		{ID: "user-1"},
		{ID: "user-2"},
	}

	mockRepo.On("GetUsersScheduledForDeletion", ctx).Return(users, nil)
	// First user fails to enqueue
	mockQueue.On("Enqueue", ctx, mock.MatchedBy(func(job queue.Job) bool {
		deletionArgs, ok := job.(AccountDeletionArgs)
		return ok && deletionArgs.UserID == "user-1"
	}), mock.Anything).Return(nil, errors.New("queue full"))
	// Second user succeeds
	mockQueue.On("Enqueue", ctx, mock.MatchedBy(func(job queue.Job) bool {
		deletionArgs, ok := job.(AccountDeletionArgs)
		return ok && deletionArgs.UserID == "user-2"
	}), mock.Anything).Return(&queue.JobResult{ID: 2}, nil)

	worker := NewScheduledDeletionCheckWorker(ScheduledDeletionCheckWorkerDeps{
		ProfileRepo: mockRepo,
		JobQueue:    mockQueue,
		Logger:      newTestDeletionCheckLogger(),
	})

	job := newTestScheduledDeletionCheckJob(ScheduledDeletionCheckArgs{
		TriggerSource: "manual",
	})

	err := worker.Work(ctx, job)

	// Should not fail even though one enqueue failed
	require.NoError(t, err)
	mockRepo.AssertExpectations(t)
	mockQueue.AssertNumberOfCalls(t, "Enqueue", 2)
}

func TestScheduledDeletionCheckWorker_Work_UniqueSkippedCounting(t *testing.T) {
	ctx := context.Background()

	mockRepo := new(MockProfileRepository)
	mockQueue := new(MockJobQueue)

	users := []*domain.User{
		{ID: "user-1"},
		{ID: "user-2"},
	}

	mockRepo.On("GetUsersScheduledForDeletion", ctx).Return(users, nil)
	// First user - new job
	mockQueue.On("Enqueue", ctx, mock.MatchedBy(func(job queue.Job) bool {
		deletionArgs, ok := job.(AccountDeletionArgs)
		return ok && deletionArgs.UserID == "user-1"
	}), mock.Anything).Return(&queue.JobResult{ID: 1, UniqueSkipped: false}, nil)
	// Second user - already queued (unique constraint)
	mockQueue.On("Enqueue", ctx, mock.MatchedBy(func(job queue.Job) bool {
		deletionArgs, ok := job.(AccountDeletionArgs)
		return ok && deletionArgs.UserID == "user-2"
	}), mock.Anything).Return(&queue.JobResult{ID: 0, UniqueSkipped: true}, nil)

	worker := NewScheduledDeletionCheckWorker(ScheduledDeletionCheckWorkerDeps{
		ProfileRepo: mockRepo,
		JobQueue:    mockQueue,
		Logger:      newTestDeletionCheckLogger(),
	})

	job := newTestScheduledDeletionCheckJob(ScheduledDeletionCheckArgs{
		TriggerSource: "periodic",
	})

	err := worker.Work(ctx, job)

	require.NoError(t, err)
	mockRepo.AssertExpectations(t)
	mockQueue.AssertExpectations(t)
}

func TestScheduledDeletionCheckUniqueOpts(t *testing.T) {
	opts := ScheduledDeletionCheckUniqueOpts()

	assert.False(t, opts.ByArgs) // All check jobs are equivalent
	assert.Equal(t, 1*60*60*1000000000, int(opts.ByPeriod.Nanoseconds())) // 1 hour
	assert.Contains(t, opts.ByState, queue.JobStateAvailable)
	assert.Contains(t, opts.ByState, queue.JobStateScheduled)
	assert.Contains(t, opts.ByState, queue.JobStateRunning)
	assert.Contains(t, opts.ByState, queue.JobStateRetryable)
	assert.Contains(t, opts.ByState, queue.JobStatePending)
}

func TestNewScheduledDeletionCheckWorker_DefaultLogger(t *testing.T) {
	mockRepo := new(MockProfileRepository)
	mockQueue := new(MockJobQueue)

	worker := NewScheduledDeletionCheckWorker(ScheduledDeletionCheckWorkerDeps{
		ProfileRepo: mockRepo,
		JobQueue:    mockQueue,
		// Logger is nil, should use default
	})

	assert.NotNil(t, worker.logger)
}
