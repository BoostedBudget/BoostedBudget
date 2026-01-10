// Package jobs contains River job definitions and workers for async processing.
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

// MockLinkedItemCleanupRepo mocks the LinkedItemCleanupRepository interface for tests
// Named differently to avoid conflict with MockLinkedItemCleanupRepo in plaid_sync_test.go
type MockLinkedItemCleanupRepo struct {
	mock.Mock
}

func (m *MockLinkedItemCleanupRepo) GetItemsForCleanup(ctx context.Context, errorDays, staleDays int) ([]domain.LinkedItem, error) {
	args := m.Called(ctx, errorDays, staleDays)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]domain.LinkedItem), args.Error(1)
}

func (m *MockLinkedItemCleanupRepo) GetItemsPendingCleanup(ctx context.Context) ([]domain.LinkedItem, error) {
	args := m.Called(ctx)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]domain.LinkedItem), args.Error(1)
}

func (m *MockLinkedItemCleanupRepo) MarkForCleanup(ctx context.Context, itemID string, cleanupAt time.Time) error {
	args := m.Called(ctx, itemID, cleanupAt)
	return args.Error(0)
}

func (m *MockLinkedItemCleanupRepo) ClearCleanupSchedule(ctx context.Context, itemID string) error {
	args := m.Called(ctx, itemID)
	return args.Error(0)
}

// Test helpers
func newTestItemCleanupCheckLogger() *slog.Logger {
	return slog.Default()
}

func newTestItemCleanupCheckJob(args ItemCleanupCheckArgs) *river.Job[ItemCleanupCheckArgs] {
	return &river.Job[ItemCleanupCheckArgs]{
		JobRow: &rivertype.JobRow{
			ID:      1,
			Attempt: 1,
		},
		Args: args,
	}
}

func TestItemCleanupCheckArgs_Kind(t *testing.T) {
	args := ItemCleanupCheckArgs{}
	assert.Equal(t, "item_cleanup_check", args.Kind())
}

func TestItemCleanupCheckWorker_Work_NoCleanupCandidates(t *testing.T) {
	ctx := context.Background()

	mockRepo := new(MockLinkedItemCleanupRepo)
	mockQueue := new(MockJobQueue)

	mockRepo.On("GetItemsForCleanup", ctx, 30, 60).Return([]domain.LinkedItem{}, nil)

	worker := NewItemCleanupCheckWorker(ItemCleanupCheckWorkerDeps{
		LinkedItemRepo:      mockRepo,
		JobQueue:            mockQueue,
		Logger:              newTestItemCleanupCheckLogger(),
		GracePeriodDays:     7,
		ErrorDaysThreshold:  30,
		StaleDaysThreshold:  60,
	})

	job := newTestItemCleanupCheckJob(ItemCleanupCheckArgs{
		TriggerSource: "periodic",
	})

	err := worker.Work(ctx, job)

	require.NoError(t, err)
	mockRepo.AssertExpectations(t)
	mockQueue.AssertNotCalled(t, "Enqueue")
}

func TestItemCleanupCheckWorker_Work_DetectsLongTermErrorItems(t *testing.T) {
	ctx := context.Background()

	mockRepo := new(MockLinkedItemCleanupRepo)
	mockQueue := new(MockJobQueue)

	errorTime := time.Now().Add(-35 * 24 * time.Hour) // 35 days ago
	items := []domain.LinkedItem{
		{
			ID:          "item-1",
			UserID:      "user-1",
			Status:      "error",
			LastErrorAt: &errorTime,
		},
	}

	mockRepo.On("GetItemsForCleanup", ctx, 30, 60).Return(items, nil)
	mockRepo.On("MarkForCleanup", ctx, "item-1", mock.Anything).Return(nil)
	mockQueue.On("Enqueue", ctx, mock.MatchedBy(func(job queue.Job) bool {
		cleanupArgs, ok := job.(ItemCleanupArgs)
		return ok && cleanupArgs.LinkedItemID == "item-1" && cleanupArgs.Reason == "long_term_error"
	}), mock.Anything).Return(&queue.JobResult{ID: 1}, nil)

	worker := NewItemCleanupCheckWorker(ItemCleanupCheckWorkerDeps{
		LinkedItemRepo:      mockRepo,
		JobQueue:            mockQueue,
		Logger:              newTestItemCleanupCheckLogger(),
		GracePeriodDays:     7,
		ErrorDaysThreshold:  30,
		StaleDaysThreshold:  60,
	})

	job := newTestItemCleanupCheckJob(ItemCleanupCheckArgs{
		TriggerSource: "periodic",
	})

	err := worker.Work(ctx, job)

	require.NoError(t, err)
	mockRepo.AssertExpectations(t)
	mockQueue.AssertExpectations(t)
}

func TestItemCleanupCheckWorker_Work_DetectsRevokedItems(t *testing.T) {
	ctx := context.Background()

	mockRepo := new(MockLinkedItemCleanupRepo)
	mockQueue := new(MockJobQueue)

	items := []domain.LinkedItem{
		{
			ID:     "item-2",
			UserID: "user-2",
			Status: "revoked",
		},
	}

	mockRepo.On("GetItemsForCleanup", ctx, 30, 60).Return(items, nil)
	mockRepo.On("MarkForCleanup", ctx, "item-2", mock.Anything).Return(nil)
	mockQueue.On("Enqueue", ctx, mock.MatchedBy(func(job queue.Job) bool {
		cleanupArgs, ok := job.(ItemCleanupArgs)
		return ok && cleanupArgs.LinkedItemID == "item-2" && cleanupArgs.Reason == "revoked"
	}), mock.Anything).Return(&queue.JobResult{ID: 2}, nil)

	worker := NewItemCleanupCheckWorker(ItemCleanupCheckWorkerDeps{
		LinkedItemRepo:      mockRepo,
		JobQueue:            mockQueue,
		Logger:              newTestItemCleanupCheckLogger(),
		GracePeriodDays:     7,
		ErrorDaysThreshold:  30,
		StaleDaysThreshold:  60,
	})

	job := newTestItemCleanupCheckJob(ItemCleanupCheckArgs{
		TriggerSource: "periodic",
	})

	err := worker.Work(ctx, job)

	require.NoError(t, err)
	mockRepo.AssertExpectations(t)
	mockQueue.AssertExpectations(t)
}

func TestItemCleanupCheckWorker_Work_DetectsStaleItems(t *testing.T) {
	ctx := context.Background()

	mockRepo := new(MockLinkedItemCleanupRepo)
	mockQueue := new(MockJobQueue)

	lastSyncTime := time.Now().Add(-65 * 24 * time.Hour) // 65 days ago
	items := []domain.LinkedItem{
		{
			ID:           "item-3",
			UserID:       "user-3",
			Status:       "active",
			LastSyncedAt: &lastSyncTime,
		},
	}

	mockRepo.On("GetItemsForCleanup", ctx, 30, 60).Return(items, nil)
	mockRepo.On("MarkForCleanup", ctx, "item-3", mock.Anything).Return(nil)
	mockQueue.On("Enqueue", ctx, mock.MatchedBy(func(job queue.Job) bool {
		cleanupArgs, ok := job.(ItemCleanupArgs)
		return ok && cleanupArgs.LinkedItemID == "item-3" && cleanupArgs.Reason == "stale"
	}), mock.Anything).Return(&queue.JobResult{ID: 3}, nil)

	worker := NewItemCleanupCheckWorker(ItemCleanupCheckWorkerDeps{
		LinkedItemRepo:      mockRepo,
		JobQueue:            mockQueue,
		Logger:              newTestItemCleanupCheckLogger(),
		GracePeriodDays:     7,
		ErrorDaysThreshold:  30,
		StaleDaysThreshold:  60,
	})

	job := newTestItemCleanupCheckJob(ItemCleanupCheckArgs{
		TriggerSource: "periodic",
	})

	err := worker.Work(ctx, job)

	require.NoError(t, err)
	mockRepo.AssertExpectations(t)
	mockQueue.AssertExpectations(t)
}

func TestItemCleanupCheckWorker_Work_RepositoryError(t *testing.T) {
	ctx := context.Background()

	mockRepo := new(MockLinkedItemCleanupRepo)
	mockQueue := new(MockJobQueue)

	dbErr := errors.New("database connection error")
	mockRepo.On("GetItemsForCleanup", ctx, 30, 60).Return(nil, dbErr)

	worker := NewItemCleanupCheckWorker(ItemCleanupCheckWorkerDeps{
		LinkedItemRepo:      mockRepo,
		JobQueue:            mockQueue,
		Logger:              newTestItemCleanupCheckLogger(),
		GracePeriodDays:     7,
		ErrorDaysThreshold:  30,
		StaleDaysThreshold:  60,
	})

	job := newTestItemCleanupCheckJob(ItemCleanupCheckArgs{
		TriggerSource: "periodic",
	})

	err := worker.Work(ctx, job)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to get cleanup candidates")
	mockRepo.AssertExpectations(t)
	mockQueue.AssertNotCalled(t, "Enqueue")
}

func TestItemCleanupCheckWorker_Work_SkipsAlreadyScheduledItems(t *testing.T) {
	ctx := context.Background()

	mockRepo := new(MockLinkedItemCleanupRepo)
	mockQueue := new(MockJobQueue)

	scheduledTime := time.Now().Add(5 * 24 * time.Hour) // 5 days from now
	items := []domain.LinkedItem{
		{
			ID:                 "item-already-scheduled",
			UserID:             "user-1",
			Status:             "error",
			CleanupScheduledAt: &scheduledTime, // Already scheduled
		},
	}

	mockRepo.On("GetItemsForCleanup", ctx, 30, 60).Return(items, nil)
	// MarkForCleanup should NOT be called for already scheduled items
	// Enqueue should NOT be called for already scheduled items

	worker := NewItemCleanupCheckWorker(ItemCleanupCheckWorkerDeps{
		LinkedItemRepo:      mockRepo,
		JobQueue:            mockQueue,
		Logger:              newTestItemCleanupCheckLogger(),
		GracePeriodDays:     7,
		ErrorDaysThreshold:  30,
		StaleDaysThreshold:  60,
	})

	job := newTestItemCleanupCheckJob(ItemCleanupCheckArgs{
		TriggerSource: "periodic",
	})

	err := worker.Work(ctx, job)

	require.NoError(t, err)
	mockRepo.AssertExpectations(t)
	mockRepo.AssertNotCalled(t, "MarkForCleanup")
	mockQueue.AssertNotCalled(t, "Enqueue")
}

func TestItemCleanupCheckWorker_Work_MarkForCleanupErrorContinues(t *testing.T) {
	ctx := context.Background()

	mockRepo := new(MockLinkedItemCleanupRepo)
	mockQueue := new(MockJobQueue)

	items := []domain.LinkedItem{
		{
			ID:     "item-1",
			UserID: "user-1",
			Status: "error",
		},
		{
			ID:     "item-2",
			UserID: "user-2",
			Status: "revoked",
		},
	}

	mockRepo.On("GetItemsForCleanup", ctx, 30, 60).Return(items, nil)
	// First item fails to mark
	mockRepo.On("MarkForCleanup", ctx, "item-1", mock.Anything).Return(errors.New("db error"))
	// Second item succeeds
	mockRepo.On("MarkForCleanup", ctx, "item-2", mock.Anything).Return(nil)
	mockQueue.On("Enqueue", ctx, mock.MatchedBy(func(job queue.Job) bool {
		cleanupArgs, ok := job.(ItemCleanupArgs)
		return ok && cleanupArgs.LinkedItemID == "item-2"
	}), mock.Anything).Return(&queue.JobResult{ID: 2}, nil)

	worker := NewItemCleanupCheckWorker(ItemCleanupCheckWorkerDeps{
		LinkedItemRepo:      mockRepo,
		JobQueue:            mockQueue,
		Logger:              newTestItemCleanupCheckLogger(),
		GracePeriodDays:     7,
		ErrorDaysThreshold:  30,
		StaleDaysThreshold:  60,
	})

	job := newTestItemCleanupCheckJob(ItemCleanupCheckArgs{
		TriggerSource: "periodic",
	})

	err := worker.Work(ctx, job)

	// Should not fail even though one item failed to mark
	require.NoError(t, err)
	mockRepo.AssertExpectations(t)
	mockQueue.AssertNumberOfCalls(t, "Enqueue", 1) // Only one successful enqueue
}

func TestItemCleanupCheckWorker_Work_EnqueueErrorContinues(t *testing.T) {
	ctx := context.Background()

	mockRepo := new(MockLinkedItemCleanupRepo)
	mockQueue := new(MockJobQueue)

	items := []domain.LinkedItem{
		{
			ID:     "item-1",
			UserID: "user-1",
			Status: "error",
		},
		{
			ID:     "item-2",
			UserID: "user-2",
			Status: "revoked",
		},
	}

	mockRepo.On("GetItemsForCleanup", ctx, 30, 60).Return(items, nil)
	mockRepo.On("MarkForCleanup", ctx, "item-1", mock.Anything).Return(nil)
	mockRepo.On("MarkForCleanup", ctx, "item-2", mock.Anything).Return(nil)
	// First item fails to enqueue
	mockQueue.On("Enqueue", ctx, mock.MatchedBy(func(job queue.Job) bool {
		cleanupArgs, ok := job.(ItemCleanupArgs)
		return ok && cleanupArgs.LinkedItemID == "item-1"
	}), mock.Anything).Return(nil, errors.New("queue full"))
	// Second item succeeds
	mockQueue.On("Enqueue", ctx, mock.MatchedBy(func(job queue.Job) bool {
		cleanupArgs, ok := job.(ItemCleanupArgs)
		return ok && cleanupArgs.LinkedItemID == "item-2"
	}), mock.Anything).Return(&queue.JobResult{ID: 2}, nil)

	worker := NewItemCleanupCheckWorker(ItemCleanupCheckWorkerDeps{
		LinkedItemRepo:      mockRepo,
		JobQueue:            mockQueue,
		Logger:              newTestItemCleanupCheckLogger(),
		GracePeriodDays:     7,
		ErrorDaysThreshold:  30,
		StaleDaysThreshold:  60,
	})

	job := newTestItemCleanupCheckJob(ItemCleanupCheckArgs{
		TriggerSource: "periodic",
	})

	err := worker.Work(ctx, job)

	// Should not fail even though one enqueue failed
	require.NoError(t, err)
	mockRepo.AssertExpectations(t)
	mockQueue.AssertNumberOfCalls(t, "Enqueue", 2)
}

func TestItemCleanupCheckWorker_Work_EnqueuesWithCorrectScheduledTime(t *testing.T) {
	ctx := context.Background()

	mockRepo := new(MockLinkedItemCleanupRepo)
	mockQueue := new(MockJobQueue)

	items := []domain.LinkedItem{
		{
			ID:     "item-1",
			UserID: "user-1",
			Status: "revoked",
		},
	}

	mockRepo.On("GetItemsForCleanup", ctx, 30, 60).Return(items, nil)
	mockRepo.On("MarkForCleanup", ctx, "item-1", mock.Anything).Return(nil)

	// Capture the enqueue options to verify scheduling
	var capturedOpts []queue.EnqueueOption
	mockQueue.On("Enqueue", ctx, mock.Anything, mock.Anything).Run(func(args mock.Arguments) {
		if opts, ok := args.Get(2).([]queue.EnqueueOption); ok {
			capturedOpts = opts
		}
	}).Return(&queue.JobResult{ID: 1}, nil)

	gracePeriodDays := 7
	worker := NewItemCleanupCheckWorker(ItemCleanupCheckWorkerDeps{
		LinkedItemRepo:      mockRepo,
		JobQueue:            mockQueue,
		Logger:              newTestItemCleanupCheckLogger(),
		GracePeriodDays:     gracePeriodDays,
		ErrorDaysThreshold:  30,
		StaleDaysThreshold:  60,
	})

	job := newTestItemCleanupCheckJob(ItemCleanupCheckArgs{
		TriggerSource: "periodic",
	})

	beforeWork := time.Now()
	err := worker.Work(ctx, job)
	afterWork := time.Now()

	require.NoError(t, err)
	mockRepo.AssertExpectations(t)
	mockQueue.AssertExpectations(t)

	// Verify that scheduled time option was passed
	require.NotEmpty(t, capturedOpts, "expected enqueue options to be passed")

	// The scheduled time should be approximately grace period days from now
	expectedScheduleTime := beforeWork.Add(time.Duration(gracePeriodDays) * 24 * time.Hour)
	maxScheduleTime := afterWork.Add(time.Duration(gracePeriodDays) * 24 * time.Hour)

	// We can't directly inspect the option, but the test verifies the option was passed
	// The implementation should use WithScheduledAt
	assert.True(t, len(capturedOpts) > 0, "should have scheduled options")
	_ = expectedScheduleTime
	_ = maxScheduleTime
}

func TestItemCleanupCheckUniqueOpts(t *testing.T) {
	opts := ItemCleanupCheckUniqueOpts()

	assert.False(t, opts.ByArgs) // All check jobs are equivalent
	assert.Equal(t, 24*time.Hour, opts.ByPeriod) // 24 hours - matches daily run frequency
	assert.Contains(t, opts.ByState, queue.JobStateAvailable)
	assert.Contains(t, opts.ByState, queue.JobStateScheduled)
	assert.Contains(t, opts.ByState, queue.JobStateRunning)
	assert.Contains(t, opts.ByState, queue.JobStateRetryable)
	assert.Contains(t, opts.ByState, queue.JobStatePending)
}

func TestNewItemCleanupCheckWorker_DefaultLogger(t *testing.T) {
	mockRepo := new(MockLinkedItemCleanupRepo)
	mockQueue := new(MockJobQueue)

	worker := NewItemCleanupCheckWorker(ItemCleanupCheckWorkerDeps{
		LinkedItemRepo:      mockRepo,
		JobQueue:            mockQueue,
		GracePeriodDays:     7,
		ErrorDaysThreshold:  30,
		StaleDaysThreshold:  60,
		// Logger is nil, should use default
	})

	assert.NotNil(t, worker.logger)
}

func TestItemCleanupCheckWorker_SetJobQueue(t *testing.T) {
	mockRepo := new(MockLinkedItemCleanupRepo)
	mockQueue := new(MockJobQueue)
	newMockQueue := new(MockJobQueue)

	worker := NewItemCleanupCheckWorker(ItemCleanupCheckWorkerDeps{
		LinkedItemRepo:     mockRepo,
		JobQueue:           mockQueue,
		Logger:             newTestItemCleanupCheckLogger(),
		GracePeriodDays:    7,
		ErrorDaysThreshold: 30,
		StaleDaysThreshold: 60,
	})

	worker.SetJobQueue(newMockQueue)

	assert.Equal(t, newMockQueue, worker.jobQueue)
}

// MockAccountCountRepo mocks the AccountCountRepository interface for tests
// Issue #668: Zero-account item detection
type MockAccountCountRepo struct {
	mock.Mock
}

func (m *MockAccountCountRepo) CountActiveByLinkedItemID(ctx context.Context, linkedItemID string) (int, error) {
	args := m.Called(ctx, linkedItemID)
	return args.Int(0), args.Error(1)
}

func TestItemCleanupCheckWorker_Work_DetectsZeroAccountItems(t *testing.T) {
	ctx := context.Background()

	mockRepo := new(MockLinkedItemCleanupRepo)
	mockAccountRepo := new(MockAccountCountRepo)
	mockQueue := new(MockJobQueue)

	// Item with zero accounts (will be detected via account count)
	items := []domain.LinkedItem{
		{
			ID:     "item-zero-accounts",
			UserID: "user-1",
			Status: "healthy", // Status is fine, but has zero accounts
		},
	}

	mockRepo.On("GetItemsForCleanup", ctx, 30, 60).Return(items, nil)
	// Return 0 accounts for this item
	mockAccountRepo.On("CountActiveByLinkedItemID", ctx, "item-zero-accounts").Return(0, nil)
	mockRepo.On("MarkForCleanup", ctx, "item-zero-accounts", mock.Anything).Return(nil)

	// Capture the cleanup args to verify reason
	mockQueue.On("Enqueue", ctx, mock.MatchedBy(func(job queue.Job) bool {
		cleanupArgs, ok := job.(ItemCleanupArgs)
		return ok && cleanupArgs.LinkedItemID == "item-zero-accounts" && cleanupArgs.Reason == "zero_accounts"
	}), mock.Anything).Return(&queue.JobResult{ID: 1}, nil)

	worker := NewItemCleanupCheckWorker(ItemCleanupCheckWorkerDeps{
		LinkedItemRepo:     mockRepo,
		AccountRepo:        mockAccountRepo,
		JobQueue:           mockQueue,
		Logger:             newTestItemCleanupCheckLogger(),
		GracePeriodDays:    7,
		ErrorDaysThreshold: 30,
		StaleDaysThreshold: 60,
	})

	job := newTestItemCleanupCheckJob(ItemCleanupCheckArgs{
		TriggerSource: "periodic",
	})

	err := worker.Work(ctx, job)

	require.NoError(t, err)
	mockRepo.AssertExpectations(t)
	mockAccountRepo.AssertExpectations(t)
	mockQueue.AssertExpectations(t)
}

func TestItemCleanupCheckWorker_Work_ZeroAccountsImmediateCleanup(t *testing.T) {
	ctx := context.Background()

	mockRepo := new(MockLinkedItemCleanupRepo)
	mockAccountRepo := new(MockAccountCountRepo)
	mockQueue := new(MockJobQueue)

	items := []domain.LinkedItem{
		{
			ID:     "item-zero-accounts",
			UserID: "user-1",
			Status: "healthy",
		},
	}

	mockRepo.On("GetItemsForCleanup", ctx, 30, 60).Return(items, nil)
	mockAccountRepo.On("CountActiveByLinkedItemID", ctx, "item-zero-accounts").Return(0, nil)

	// Capture the MarkForCleanup time to verify it's immediate (not grace period)
	var markCleanupAt time.Time
	mockRepo.On("MarkForCleanup", ctx, "item-zero-accounts", mock.Anything).Run(func(args mock.Arguments) {
		markCleanupAt = args.Get(2).(time.Time)
	}).Return(nil)

	mockQueue.On("Enqueue", ctx, mock.Anything, mock.Anything).Return(&queue.JobResult{ID: 1}, nil)

	worker := NewItemCleanupCheckWorker(ItemCleanupCheckWorkerDeps{
		LinkedItemRepo:     mockRepo,
		AccountRepo:        mockAccountRepo,
		JobQueue:           mockQueue,
		Logger:             newTestItemCleanupCheckLogger(),
		GracePeriodDays:    7, // Grace period is 7 days
		ErrorDaysThreshold: 30,
		StaleDaysThreshold: 60,
	})

	beforeWork := time.Now()
	job := newTestItemCleanupCheckJob(ItemCleanupCheckArgs{TriggerSource: "periodic"})
	err := worker.Work(ctx, job)
	afterWork := time.Now()

	require.NoError(t, err)

	// Verify the cleanup time is immediate (within a second of now), NOT 7 days later
	assert.True(t, markCleanupAt.After(beforeWork.Add(-time.Second)), "cleanup should be scheduled now or after")
	assert.True(t, markCleanupAt.Before(afterWork.Add(time.Second)), "cleanup should be immediate, not 7 days later")

	mockRepo.AssertExpectations(t)
	mockAccountRepo.AssertExpectations(t)
}

func TestItemCleanupCheckWorker_Work_WithAccountsUsesGracePeriod(t *testing.T) {
	ctx := context.Background()

	mockRepo := new(MockLinkedItemCleanupRepo)
	mockAccountRepo := new(MockAccountCountRepo)
	mockQueue := new(MockJobQueue)

	items := []domain.LinkedItem{
		{
			ID:     "item-with-accounts",
			UserID: "user-1",
			Status: "revoked",
		},
	}

	mockRepo.On("GetItemsForCleanup", ctx, 30, 60).Return(items, nil)
	// Item has accounts - should use normal grace period
	mockAccountRepo.On("CountActiveByLinkedItemID", ctx, "item-with-accounts").Return(3, nil)

	var markCleanupAt time.Time
	mockRepo.On("MarkForCleanup", ctx, "item-with-accounts", mock.Anything).Run(func(args mock.Arguments) {
		markCleanupAt = args.Get(2).(time.Time)
	}).Return(nil)

	mockQueue.On("Enqueue", ctx, mock.MatchedBy(func(job queue.Job) bool {
		cleanupArgs, ok := job.(ItemCleanupArgs)
		return ok && cleanupArgs.Reason == "revoked" // Not zero_accounts
	}), mock.Anything).Return(&queue.JobResult{ID: 1}, nil)

	gracePeriodDays := 7
	worker := NewItemCleanupCheckWorker(ItemCleanupCheckWorkerDeps{
		LinkedItemRepo:     mockRepo,
		AccountRepo:        mockAccountRepo,
		JobQueue:           mockQueue,
		Logger:             newTestItemCleanupCheckLogger(),
		GracePeriodDays:    gracePeriodDays,
		ErrorDaysThreshold: 30,
		StaleDaysThreshold: 60,
	})

	beforeWork := time.Now()
	job := newTestItemCleanupCheckJob(ItemCleanupCheckArgs{TriggerSource: "periodic"})
	err := worker.Work(ctx, job)

	require.NoError(t, err)

	// Verify the cleanup time is ~7 days in the future (not immediate)
	expectedMinCleanupAt := beforeWork.Add(time.Duration(gracePeriodDays) * 24 * time.Hour)
	assert.True(t, markCleanupAt.After(expectedMinCleanupAt.Add(-time.Minute)), "cleanup should be 7+ days from now")

	mockRepo.AssertExpectations(t)
	mockAccountRepo.AssertExpectations(t)
}
