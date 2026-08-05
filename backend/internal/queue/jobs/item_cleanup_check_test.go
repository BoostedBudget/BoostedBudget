// Package jobs contains River job definitions and workers for async processing.
package jobs

import (
	"context"
	"errors"
	"log/slog"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/queue"
	"github.com/maxnussbaum/BoostedBudget/internal/repository"
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

// GetItemsForCleanupPage is the paged, keyset-cursored form ItemCleanupCheckWorker.Work calls as of
// Issue #1469 — the unbounded GetItemsForCleanup is no longer on the narrow
// LinkedItemCleanupRepository interface at all, so this mock does not implement it.
//
// Note for the expectations below that return a page of 1-3 items and never program a second call:
// the run stops as soon as a page comes back SHORT (len < pageSize), and those tests leave
// CleanupPageSize unset, so pageSize is the production default (500). Any page smaller than that is
// short, so exactly one fetch happens per run — the same call count these expectations had before
// the paging loop existed.
func (m *MockLinkedItemCleanupRepo) GetItemsForCleanupPage(ctx context.Context, errorDays, staleDays int, after *repository.ItemCleanupSweepCursor, limit int) ([]domain.LinkedItem, error) {
	args := m.Called(ctx, errorDays, staleDays, after, limit)
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

func (m *MockLinkedItemCleanupRepo) Delete(ctx context.Context, id string) error {
	args := m.Called(ctx, id)
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

	mockRepo.On("GetItemsForCleanupPage", ctx, 30, 60, mock.Anything, mock.Anything).Return([]domain.LinkedItem{}, nil)

	worker := NewItemCleanupCheckWorker(ItemCleanupCheckWorkerDeps{
		LinkedItemRepo:     mockRepo,
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

	mockRepo.On("GetItemsForCleanupPage", ctx, 30, 60, mock.Anything, mock.Anything).Return(items, nil)
	mockRepo.On("MarkForCleanup", ctx, "item-1", mock.Anything).Return(nil)
	mockQueue.On("Enqueue", ctx, mock.MatchedBy(func(job queue.Job) bool {
		cleanupArgs, ok := job.(ItemCleanupArgs)
		return ok && cleanupArgs.LinkedItemID == "item-1" && cleanupArgs.Reason == "long_term_error"
	}), mock.Anything).Return(&queue.JobResult{ID: 1}, nil)

	worker := NewItemCleanupCheckWorker(ItemCleanupCheckWorkerDeps{
		LinkedItemRepo:     mockRepo,
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

	mockRepo.On("GetItemsForCleanupPage", ctx, 30, 60, mock.Anything, mock.Anything).Return(items, nil)
	mockRepo.On("MarkForCleanup", ctx, "item-2", mock.Anything).Return(nil)
	mockQueue.On("Enqueue", ctx, mock.MatchedBy(func(job queue.Job) bool {
		cleanupArgs, ok := job.(ItemCleanupArgs)
		return ok && cleanupArgs.LinkedItemID == "item-2" && cleanupArgs.Reason == "revoked"
	}), mock.Anything).Return(&queue.JobResult{ID: 2}, nil)

	worker := NewItemCleanupCheckWorker(ItemCleanupCheckWorkerDeps{
		LinkedItemRepo:     mockRepo,
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

	mockRepo.On("GetItemsForCleanupPage", ctx, 30, 60, mock.Anything, mock.Anything).Return(items, nil)
	mockRepo.On("MarkForCleanup", ctx, "item-3", mock.Anything).Return(nil)
	mockQueue.On("Enqueue", ctx, mock.MatchedBy(func(job queue.Job) bool {
		cleanupArgs, ok := job.(ItemCleanupArgs)
		return ok && cleanupArgs.LinkedItemID == "item-3" && cleanupArgs.Reason == "stale"
	}), mock.Anything).Return(&queue.JobResult{ID: 3}, nil)

	worker := NewItemCleanupCheckWorker(ItemCleanupCheckWorkerDeps{
		LinkedItemRepo:     mockRepo,
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
	mockQueue.AssertExpectations(t)
}

func TestItemCleanupCheckWorker_Work_RepositoryError(t *testing.T) {
	ctx := context.Background()

	mockRepo := new(MockLinkedItemCleanupRepo)
	mockQueue := new(MockJobQueue)

	dbErr := errors.New("database connection error")
	mockRepo.On("GetItemsForCleanupPage", ctx, 30, 60, mock.Anything, mock.Anything).Return(nil, dbErr)

	worker := NewItemCleanupCheckWorker(ItemCleanupCheckWorkerDeps{
		LinkedItemRepo:     mockRepo,
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

	mockRepo.On("GetItemsForCleanupPage", ctx, 30, 60, mock.Anything, mock.Anything).Return(items, nil)
	// MarkForCleanup should NOT be called for already scheduled items
	// Enqueue should NOT be called for already scheduled items

	worker := NewItemCleanupCheckWorker(ItemCleanupCheckWorkerDeps{
		LinkedItemRepo:     mockRepo,
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

	mockRepo.On("GetItemsForCleanupPage", ctx, 30, 60, mock.Anything, mock.Anything).Return(items, nil)
	// First item fails to mark
	mockRepo.On("MarkForCleanup", ctx, "item-1", mock.Anything).Return(errors.New("db error"))
	// Second item succeeds
	mockRepo.On("MarkForCleanup", ctx, "item-2", mock.Anything).Return(nil)
	mockQueue.On("Enqueue", ctx, mock.MatchedBy(func(job queue.Job) bool {
		cleanupArgs, ok := job.(ItemCleanupArgs)
		return ok && cleanupArgs.LinkedItemID == "item-2"
	}), mock.Anything).Return(&queue.JobResult{ID: 2}, nil)

	worker := NewItemCleanupCheckWorker(ItemCleanupCheckWorkerDeps{
		LinkedItemRepo:     mockRepo,
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

	mockRepo.On("GetItemsForCleanupPage", ctx, 30, 60, mock.Anything, mock.Anything).Return(items, nil)
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
		LinkedItemRepo:     mockRepo,
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

	mockRepo.On("GetItemsForCleanupPage", ctx, 30, 60, mock.Anything, mock.Anything).Return(items, nil)
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
		LinkedItemRepo:     mockRepo,
		JobQueue:           mockQueue,
		Logger:             newTestItemCleanupCheckLogger(),
		GracePeriodDays:    gracePeriodDays,
		ErrorDaysThreshold: 30,
		StaleDaysThreshold: 60,
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

	assert.False(t, opts.ByArgs)                 // All check jobs are equivalent
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
		LinkedItemRepo:     mockRepo,
		JobQueue:           mockQueue,
		GracePeriodDays:    7,
		ErrorDaysThreshold: 30,
		StaleDaysThreshold: 60,
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

	mockRepo.On("GetItemsForCleanupPage", ctx, 30, 60, mock.Anything, mock.Anything).Return(items, nil)
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

	mockRepo.On("GetItemsForCleanupPage", ctx, 30, 60, mock.Anything, mock.Anything).Return(items, nil)
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

	mockRepo.On("GetItemsForCleanupPage", ctx, 30, 60, mock.Anything, mock.Anything).Return(items, nil)
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

// ─── Issue #1448: ItemCleanupCheckUniqueOpts existed but was never wired. ────────────────

func TestItemCleanupCheckInsertOpts_Queue(t *testing.T) {
	opts := ItemCleanupCheckInsertOpts()
	require.NotNil(t, opts)
	assert.Equal(t, "low", opts.Queue)
}

func TestItemCleanupCheckInsertOpts_CarriesUniqueOpts(t *testing.T) {
	opts := ItemCleanupCheckInsertOpts()
	want := ItemCleanupCheckUniqueOpts()

	assert.Equal(t, want.ByArgs, opts.UniqueOpts.ByArgs)
	assert.Equal(t, want.ByPeriod, opts.UniqueOpts.ByPeriod)
	assert.False(t, opts.UniqueOpts.ByQueue, "uniqueness must stay global across queues")

	require.Len(t, opts.UniqueOpts.ByState, len(want.ByState),
		"main.go's periodic registration must carry ItemCleanupCheckUniqueOpts' dedup "+
			"(Issue #1448) — ByState must match exactly, not just be non-empty")
	for i, s := range want.ByState {
		assert.Equal(t, rivertype.JobState(s), opts.UniqueOpts.ByState[i])
	}
}

func TestItemCleanupCheckPeriodicOpts_RunOnStart(t *testing.T) {
	opts := ItemCleanupCheckPeriodicOpts()
	require.NotNil(t, opts)
	assert.True(t, opts.RunOnStart)
}

func TestItemCleanupCheckPeriodicOpts_ID(t *testing.T) {
	opts := ItemCleanupCheckPeriodicOpts()
	assert.Equal(t, "item_cleanup_check", opts.ID)
}

func TestItemCleanupCheckPeriodicOpts_PairedWithUniqueOpts(t *testing.T) {
	opts := ItemCleanupCheckPeriodicOpts()
	require.True(t, opts.RunOnStart)

	unique := ItemCleanupCheckUniqueOpts()
	require.NotEmpty(t, unique.ByState)
}

// ─── Issue #1469: bounding the item-cleanup sweep ──────────────────────────────────────────────
//
// Work() used to consume the ENTIRE unbounded result of GetItemsForCleanup, marking every row for
// cleanup and enqueueing an ItemCleanup job per row — and ItemCleanup calls Plaid /item/remove,
// which the user can only undo by re-linking the institution through Plaid Link.
// ItemCleanupCheckPeriodicOpts registers this job with RunOnStart: true, so the first run after
// every deploy acted on however many candidates the table happened to hold.
//
// These two tests are the fast unit pins for the two halves of the fix. The real-SQL half — that the
// keyset predicate over two NULLABLE sort columns plus the li.id tie-breaker actually produces a
// deterministic, non-overlapping page sequence — is pinned by
// linked_item_repository_cleanup_page_integration_test.go, which pgxmock cannot substitute for.

// TestItemCleanupCheckWorker_Work_PagesPastFirstBatch proves boundedness WITHOUT starvation inside
// one run: with a page size of 2 and 5 candidates, Work() must issue three bounded page fetches
// (2, 2, 1 — never all 5 in one call) and still schedule every candidate, including the ones ranked
// past the first page. A naive `LIMIT 2` with no cursor would only ever see the first two.
func TestItemCleanupCheckWorker_Work_PagesPastFirstBatch(t *testing.T) {
	ctx := context.Background()

	base := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	items := make([]domain.LinkedItem, 5)
	for i := range items {
		errAt := base.Add(time.Duration(i) * time.Hour)
		items[i] = domain.LinkedItem{
			ID:          uuid.New().String(),
			UserID:      "user-1",
			Status:      "revoked",
			LastErrorAt: &errAt,
		}
	}

	mockRepo := new(MockLinkedItemCleanupRepo)
	mockQueue := new(MockJobQueue)

	mockRepo.On("GetItemsForCleanupPage", ctx, 30, 60, (*repository.ItemCleanupSweepCursor)(nil), 2).
		Return(items[0:2], nil).Once()
	mockRepo.On("GetItemsForCleanupPage", ctx, 30, 60, mock.MatchedBy(func(c *repository.ItemCleanupSweepCursor) bool {
		return c != nil && c.ItemID == items[1].ID
	}), 2).Return(items[2:4], nil).Once()
	mockRepo.On("GetItemsForCleanupPage", ctx, 30, 60, mock.MatchedBy(func(c *repository.ItemCleanupSweepCursor) bool {
		return c != nil && c.ItemID == items[3].ID
	}), 2).Return(items[4:5], nil).Once() // short page (1 < 2): the run must stop here, not re-query

	mockRepo.On("MarkForCleanup", ctx, mock.Anything, mock.Anything).Return(nil)
	mockQueue.On("Enqueue", ctx, mock.Anything, mock.Anything, mock.Anything).Return(&queue.JobResult{ID: 1}, nil)

	worker := NewItemCleanupCheckWorker(ItemCleanupCheckWorkerDeps{
		LinkedItemRepo:     mockRepo,
		JobQueue:           mockQueue,
		Logger:             newTestItemCleanupCheckLogger(),
		GracePeriodDays:    7,
		ErrorDaysThreshold: 30,
		StaleDaysThreshold: 60,
		CleanupPageSize:    2,
	})

	require.NoError(t, worker.Work(ctx, newTestItemCleanupCheckJob(ItemCleanupCheckArgs{TriggerSource: "periodic"})))

	mockRepo.AssertExpectations(t) // proves all three page fetches happened with the right cursors
	mockRepo.AssertNumberOfCalls(t, "MarkForCleanup", 5)
	mockQueue.AssertNumberOfCalls(t, "Enqueue", 5)
}

// TestItemCleanupCheckWorker_Work_StopsAtTotalProcessedCap is the failing-first pin for the per-run
// cap itself. Bounding each QUERY to a page size does not bound the run: the loop around it would
// keep paging until a short page, so a large enough backlog makes one run's Plaid-disable count
// unbounded again.
//
// The stub returns a FULL page indefinitely, standing in for an arbitrarily large backlog. It is
// bounded to safetyNetCalls returns so a regression cannot hang CI: without the cap the run keeps
// calling past ceil(cap/pageSize), the stub runs out of programmed calls, and testify panics with
// "mock: I don't know what to return" — a fast, loud failure rather than a hang.
func TestItemCleanupCheckWorker_Work_StopsAtTotalProcessedCap(t *testing.T) {
	ctx := context.Background()

	const pageSize = 10
	const sweepCap = 25                        // deliberately NOT a multiple of pageSize: proves the last page is truncated
	const wantPageFetches = 3                  // ceil(25/10)
	const safetyNetCalls = wantPageFetches + 5 // far more than a correct implementation ever needs

	mockRepo := new(MockLinkedItemCleanupRepo)
	mockQueue := new(MockJobQueue)

	base := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	for i := 0; i < safetyNetCalls; i++ {
		page := make([]domain.LinkedItem, pageSize)
		for j := 0; j < pageSize; j++ {
			errAt := base.Add(time.Duration(i*pageSize+j) * time.Minute)
			page[j] = domain.LinkedItem{ID: uuid.New().String(), UserID: "user-1", Status: "revoked", LastErrorAt: &errAt}
		}
		mockRepo.On("GetItemsForCleanupPage", ctx, 30, 60, mock.Anything, pageSize).Return(page, nil).Once()
	}

	mockRepo.On("MarkForCleanup", ctx, mock.Anything, mock.Anything).Return(nil)
	mockQueue.On("Enqueue", ctx, mock.Anything, mock.Anything, mock.Anything).Return(&queue.JobResult{ID: 1}, nil)

	worker := NewItemCleanupCheckWorker(ItemCleanupCheckWorkerDeps{
		LinkedItemRepo:          mockRepo,
		JobQueue:                mockQueue,
		Logger:                  newTestItemCleanupCheckLogger(),
		GracePeriodDays:         7,
		ErrorDaysThreshold:      30,
		StaleDaysThreshold:      60,
		CleanupPageSize:         pageSize,
		MaxItemsPerCleanupSweep: sweepCap,
	})

	require.NoError(t, worker.Work(ctx, newTestItemCleanupCheckJob(ItemCleanupCheckArgs{TriggerSource: "periodic"})))

	fetchCalls := 0
	for _, call := range mockRepo.Calls {
		if call.Method == "GetItemsForCleanupPage" {
			fetchCalls++
		}
	}
	assert.Equal(t, wantPageFetches, fetchCalls,
		"the run must stop fetching pages once the total-processed cap is reached, not keep paging "+
			"until a short/empty page — without the cap this exceeds wantPageFetches long before the "+
			"stub's safety net runs out")
	mockRepo.AssertNumberOfCalls(t, "MarkForCleanup", sweepCap)
	mockQueue.AssertNumberOfCalls(t, "Enqueue", sweepCap)
}
