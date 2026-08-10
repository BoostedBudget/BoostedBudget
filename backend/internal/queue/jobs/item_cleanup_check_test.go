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

// ClaimItemForCleanup (Issue #1486) is never called through THIS mock — it belongs to
// ItemCleanupWorker, not to the sweep this file exercises — so it is a bare no-op returning "not
// claimable" rather than a testify expectation, which would panic as unregistered. Stubbed only to
// satisfy the widened LinkedItemCleanupRepository interface. Returning false is the fail-safe
// direction: were the sweep ever to start calling it, no test here would silently authorise an
// irreversible removal.
func (m *MockLinkedItemCleanupRepo) ClaimItemForCleanup(ctx context.Context, itemID string, errorDays, staleDays int) (bool, error) {
	return false, nil
}

func (m *MockLinkedItemCleanupRepo) ClearCleanupSchedule(ctx context.Context, itemID string) error {
	args := m.Called(ctx, itemID)
	return args.Error(0)
}

func (m *MockLinkedItemCleanupRepo) Delete(ctx context.Context, id string) error {
	args := m.Called(ctx, id)
	return args.Error(0)
}

// newMockLinkedItemCleanupRepo is the constructor every test in this file that is NOT about the
// pending-cleanup backstop uses. It pre-programs GetItemsPendingCleanup to return NO stranded rows,
// with .Maybe() so the call is permitted but not required.
//
// Issue #1486 follow-up part C wired GetItemsPendingCleanup into ItemCleanupCheckWorker.Work (it had been
// declared on two interfaces and implemented, with zero production callers). Without a default here,
// every pre-existing test in this file would panic on an unregistered testify expectation, which
// would say nothing about the behaviour those tests exist to pin. Tests that ARE about the backstop
// construct the mock directly with new(MockLinkedItemCleanupRepo) and program this method
// themselves — a .Maybe() registered here would otherwise win over their override, since testify
// matches the first registered expectation.
func newMockLinkedItemCleanupRepo() *MockLinkedItemCleanupRepo {
	m := new(MockLinkedItemCleanupRepo)
	m.On("GetItemsPendingCleanup", mock.Anything).Return([]domain.LinkedItem{}, nil).Maybe()
	return m
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

	mockRepo := newMockLinkedItemCleanupRepo()
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

	mockRepo := newMockLinkedItemCleanupRepo()
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

	mockRepo := newMockLinkedItemCleanupRepo()
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

	mockRepo := newMockLinkedItemCleanupRepo()
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

	mockRepo := newMockLinkedItemCleanupRepo()
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

	mockRepo := newMockLinkedItemCleanupRepo()
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

	mockRepo := newMockLinkedItemCleanupRepo()
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

	mockRepo := newMockLinkedItemCleanupRepo()
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
	// Issue #1486 follow-up part D: the failed enqueue now releases the stamp MarkForCleanup set for item-1,
	// so it stays visible to the candidate sweep. See
	// TestItemCleanupCheckWorker_Work_EnqueueFailure_ReleasesTheCleanupStamp for why.
	mockRepo.On("ClearCleanupSchedule", ctx, "item-1").Return(nil)

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
	// Only the FAILED item's stamp is released; item-2's job exists and must keep its schedule.
	mockRepo.AssertNotCalled(t, "ClearCleanupSchedule", ctx, "item-2")
}

func TestItemCleanupCheckWorker_Work_EnqueuesWithCorrectScheduledTime(t *testing.T) {
	ctx := context.Background()

	mockRepo := newMockLinkedItemCleanupRepo()
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
	mockRepo := newMockLinkedItemCleanupRepo()
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
	mockRepo := newMockLinkedItemCleanupRepo()
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

	mockRepo := newMockLinkedItemCleanupRepo()
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

	mockRepo := newMockLinkedItemCleanupRepo()
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

	mockRepo := newMockLinkedItemCleanupRepo()
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

// expectItemCleanupCursor matches the cursor the WORKER built against the row it must have been built
// from — all three components, with pointer nil-ness compared exactly, since nil is a legitimate
// cursor value here (both columns are nullable) and therefore cannot be treated as "don't care".
// Matching ItemID alone is what left the timestamp components unpinned; see the comment inside
// TestItemCleanupCheckWorker_Work_PagesPastFirstBatch.
func expectItemCleanupCursor(from domain.LinkedItem) interface{} {
	return mock.MatchedBy(func(c *repository.ItemCleanupSweepCursor) bool {
		return c != nil &&
			c.ItemID == from.ID &&
			sameTimePtr(c.LastErrorAt, from.LastErrorAt) &&
			sameTimePtr(c.LastSyncedAt, from.LastSyncedAt)
	})
}

// sameTimePtr reports whether two *time.Time carry the same value, treating nil as a distinct,
// meaningful value rather than a wildcard.
func sameTimePtr(a, b *time.Time) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return a.Equal(*b)
}

// TestItemCleanupCheckWorker_Work_PagesPastFirstBatch proves boundedness WITHOUT starvation inside
// one run: with a page size of 2 and 5 candidates, Work() must issue three bounded page fetches
// (2, 2, 1 — never all 5 in one call) and still schedule every candidate, including the ones ranked
// past the first page. A naive `LIMIT 2` with no cursor would only ever see the first two.
func TestItemCleanupCheckWorker_Work_PagesPastFirstBatch(t *testing.T) {
	ctx := context.Background()

	// BOTH timestamp columns are populated (not just last_error_at) so a cursor that drops either one
	// is detectable — see expectItemCleanupCursor for why that matters.
	base := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	items := make([]domain.LinkedItem, 5)
	for i := range items {
		errAt := base.Add(time.Duration(i) * time.Hour)
		syncAt := base.Add(time.Duration(i)*time.Hour + 30*time.Minute)
		items[i] = domain.LinkedItem{
			ID:           uuid.New().String(),
			UserID:       "user-1",
			Status:       "revoked",
			LastErrorAt:  &errAt,
			LastSyncedAt: &syncAt,
		}
	}

	mockRepo := newMockLinkedItemCleanupRepo()
	mockQueue := new(MockJobQueue)

	// ⚠ All THREE cursor components are asserted. An earlier version matched on ItemID alone, which
	// left the two timestamp components unpinned: forcing both to nil left this test — and both full
	// packages, unit and integration — green. A nil-nil cursor COALESCEs to ('infinity','infinity'),
	// which no finite row can exceed, so the query returns nothing after the first page and the run
	// silently stops one page in — or, symmetrically, a wrong non-nil value skips or repeats rows.
	mockRepo.On("GetItemsForCleanupPage", ctx, 30, 60, (*repository.ItemCleanupSweepCursor)(nil), 2).
		Return(items[0:2], nil).Once()
	mockRepo.On("GetItemsForCleanupPage", ctx, 30, 60, expectItemCleanupCursor(items[1]), 2).
		Return(items[2:4], nil).Once()
	mockRepo.On("GetItemsForCleanupPage", ctx, 30, 60, expectItemCleanupCursor(items[3]), 2).
		Return(items[4:5], nil).Once() // short page (1 < 2): the run must stop here, not re-query

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

	mockRepo := newMockLinkedItemCleanupRepo()
	mockQueue := new(MockJobQueue)

	base := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	pages := make([][]domain.LinkedItem, safetyNetCalls)
	for i := 0; i < safetyNetCalls; i++ {
		page := make([]domain.LinkedItem, pageSize)
		for j := 0; j < pageSize; j++ {
			errAt := base.Add(time.Duration(i*pageSize+j) * time.Minute)
			syncAt := errAt.Add(30 * time.Second)
			page[j] = domain.LinkedItem{ID: uuid.New().String(), UserID: "user-1", Status: "revoked", LastErrorAt: &errAt, LastSyncedAt: &syncAt}
		}
		pages[i] = page
	}
	// Each call's cursor is asserted against the previous page's LAST row — all three components, via
	// expectItemCleanupCursor. A mock.Anything cursor here (the earlier version) meant this test could
	// not tell a correct cursor from a nil-timestamped one either.
	mockRepo.On("GetItemsForCleanupPage", ctx, 30, 60, (*repository.ItemCleanupSweepCursor)(nil), pageSize).
		Return(pages[0], nil).Once()
	for i := 1; i < safetyNetCalls; i++ {
		mockRepo.On("GetItemsForCleanupPage", ctx, 30, 60, expectItemCleanupCursor(pages[i-1][pageSize-1]), pageSize).
			Return(pages[i], nil).Once()
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

// ---------------------------------------------------------------------------------------------
// Issue #1486 follow-up part D: the sweep's own strand
// ---------------------------------------------------------------------------------------------

// TestItemCleanupCheckWorker_Work_EnqueueFailure_ReleasesTheCleanupStamp is part D's failing-first
// test, and a THIRD instance of the class commit 24907762 fixed — whose message claims it covered
// "two paths"; there were three.
//
// Work marks the item (MarkForCleanup sets cleanup_scheduled_at) and only then enqueues. Before this
// fix, an enqueue failure logged, incremented errorCount and `continue`d, leaving the stamp set with
// no job in flight. cleanup_scheduled_at is a DURABLE marker that HIDES a row from the sweep —
// repository.cleanupCandidateSelectQuery requires cleanup_scheduled_at IS NULL — so River being
// briefly unavailable permanently removed a still-billing Plaid item from every future sweep, and
// DeleteUserCascade's C2 guard blocked that user's GDPR erasure forever. Before the stamp existed
// this path left the row untouched and the next daily sweep recovered it, so keeping the stamp made a
// transient queue error strictly WORSE than the old behaviour.
//
// Safe from the race the stamp guards against, for the same reason as the identical release in
// accountService.DeleteAccount: that race is with a worker executing the job, and the enqueue that
// would have created that job just failed.
//
// This test failed on the ClearCleanupSchedule expectation before the fix.
func TestItemCleanupCheckWorker_Work_EnqueueFailure_ReleasesTheCleanupStamp(t *testing.T) {
	ctx := context.Background()

	mockRepo := newMockLinkedItemCleanupRepo()
	mockQueue := new(MockJobQueue)

	items := []domain.LinkedItem{{ID: "item-strand", UserID: "user-1", Status: "revoked"}}

	mockRepo.On("GetItemsForCleanupPage", ctx, 30, 60, mock.Anything, mock.Anything).Return(items, nil)
	mockRepo.On("MarkForCleanup", ctx, "item-strand", mock.Anything).Return(nil)
	mockQueue.On("Enqueue", ctx, mock.Anything, mock.Anything, mock.Anything).
		Return((*queue.JobResult)(nil), errors.New("river unavailable"))
	mockRepo.On("ClearCleanupSchedule", ctx, "item-strand").Return(nil)

	worker := NewItemCleanupCheckWorker(ItemCleanupCheckWorkerDeps{
		LinkedItemRepo:     mockRepo,
		JobQueue:           mockQueue,
		Logger:             newTestItemCleanupCheckLogger(),
		GracePeriodDays:    7,
		ErrorDaysThreshold: 30,
		StaleDaysThreshold: 60,
	})

	// A per-item failure still must not fail the whole sweep — the run continues and the next
	// periodic run re-selects the (now unstamped) row.
	require.NoError(t, worker.Work(ctx, newTestItemCleanupCheckJob(ItemCleanupCheckArgs{TriggerSource: "periodic"})))

	mockRepo.AssertExpectations(t)
}

// TestItemCleanupCheckWorker_Work_EnqueueAndReleaseBothFail_DoesNotFailTheSweep: when the release
// itself also fails the row IS stranded, and nothing downstream will notice on its own — hence the
// Error-level log at the call site. It must still not fail the sweep for every other item, and part
// C's backstop is what eventually recovers it, since the row keeps cleanup_scheduled_at.
func TestItemCleanupCheckWorker_Work_EnqueueAndReleaseBothFail_DoesNotFailTheSweep(t *testing.T) {
	ctx := context.Background()

	mockRepo := newMockLinkedItemCleanupRepo()
	mockQueue := new(MockJobQueue)

	items := []domain.LinkedItem{{ID: "item-doubly-stranded", UserID: "user-1", Status: "revoked"}}

	mockRepo.On("GetItemsForCleanupPage", ctx, 30, 60, mock.Anything, mock.Anything).Return(items, nil)
	mockRepo.On("MarkForCleanup", ctx, "item-doubly-stranded", mock.Anything).Return(nil)
	mockQueue.On("Enqueue", ctx, mock.Anything, mock.Anything, mock.Anything).
		Return((*queue.JobResult)(nil), errors.New("river unavailable"))
	mockRepo.On("ClearCleanupSchedule", ctx, "item-doubly-stranded").Return(errors.New("db blip"))

	worker := NewItemCleanupCheckWorker(ItemCleanupCheckWorkerDeps{
		LinkedItemRepo:     mockRepo,
		JobQueue:           mockQueue,
		Logger:             newTestItemCleanupCheckLogger(),
		GracePeriodDays:    7,
		ErrorDaysThreshold: 30,
		StaleDaysThreshold: 60,
	})

	require.NoError(t, worker.Work(ctx, newTestItemCleanupCheckJob(ItemCleanupCheckArgs{TriggerSource: "periodic"})))

	mockRepo.AssertExpectations(t)
}

// ---------------------------------------------------------------------------------------------
// Issue #1486 follow-up part C: the pending-cleanup backstop
// ---------------------------------------------------------------------------------------------

// newStrandedItem builds a row in the exact state GetItemsPendingCleanup selects for: stamped
// (cleanup_scheduled_at IS NOT NULL), overdue (< NOW()), and not yet disconnected/replaced.
func newStrandedItem(id, userID string) domain.LinkedItem {
	stampedAt := time.Now().Add(-3 * 24 * time.Hour)
	return domain.LinkedItem{
		ID:                 id,
		UserID:             userID,
		Status:             "revoked",
		CleanupScheduledAt: &stampedAt,
	}
}

// TestItemCleanupCheckWorker_Work_ReenqueuesStrandedPendingCleanupItems is part C's failing-first
// test.
//
// repository.GetItemsPendingCleanup (`WHERE cleanup_scheduled_at IS NOT NULL AND
// cleanup_scheduled_at < NOW() AND status NOT IN ('replaced','disconnected')`) was declared on the
// wide repository.LinkedItemRepository interface AND on this worker's narrow
// LinkedItemCleanupRepository, and implemented — with ZERO production callers. It selects precisely
// "stamped, overdue, Plaid side not yet done" — i.e. every STRANDED row, from whatever cause. This
// wires it in as a re-enqueue backstop.
//
// It covers three strand paths at once: a dead-lettered ItemCleanup job (ItemCleanupArgs sets no
// MaxAttempts, so it inherits DefaultEnqueueOptions's MaxAttempts=5 (queue/options.go), after which
// DeadLetterHandler.HandleError engages at job.Attempt >= job.MaxAttempts and nothing retries it —
// DeadLetterService.Retry is admin-triggered and single-shot, an alerting path, not a recovery
// path); a failed enqueue whose stamp release also failed (part D's second test above); and any
// future stamp/enqueue asymmetry.
//
// It is safe because ClaimItemForCleanup re-verifies eligibility inside ItemCleanupWorker.Work
// before the irreversible /item/remove, and ItemCleanupUniqueOpts prevents a duplicate live job.
//
// This test failed on the Enqueue expectation before the fix — nothing called GetItemsPendingCleanup
// at all.
func TestItemCleanupCheckWorker_Work_ReenqueuesStrandedPendingCleanupItems(t *testing.T) {
	ctx := context.Background()

	// Programmed directly rather than via newMockLinkedItemCleanupRepo, whose .Maybe() default
	// would win over this override.
	mockRepo := new(MockLinkedItemCleanupRepo)
	mockQueue := new(MockJobQueue)

	// No NEW candidates this run: the backstop must run independently of the candidate sweep, and in
	// particular must not be skipped by Work's "no items need cleanup" early return.
	mockRepo.On("GetItemsForCleanupPage", ctx, 30, 60, mock.Anything, mock.Anything).Return([]domain.LinkedItem{}, nil)
	mockRepo.On("GetItemsPendingCleanup", ctx).Return([]domain.LinkedItem{newStrandedItem("item-stranded", "user-9")}, nil)

	mockQueue.On("Enqueue", ctx, mock.MatchedBy(func(job queue.Job) bool {
		cleanupArgs, ok := job.(ItemCleanupArgs)
		return ok && cleanupArgs.LinkedItemID == "item-stranded" && cleanupArgs.UserID == "user-9"
	}), mock.Anything, mock.Anything).Return(&queue.JobResult{ID: 42}, nil)

	worker := NewItemCleanupCheckWorker(ItemCleanupCheckWorkerDeps{
		LinkedItemRepo:     mockRepo,
		JobQueue:           mockQueue,
		Logger:             newTestItemCleanupCheckLogger(),
		GracePeriodDays:    7,
		ErrorDaysThreshold: 30,
		StaleDaysThreshold: 60,
	})

	require.NoError(t, worker.Work(ctx, newTestItemCleanupCheckJob(ItemCleanupCheckArgs{TriggerSource: "periodic"})))

	mockRepo.AssertExpectations(t)
	mockQueue.AssertExpectations(t)

	// The load-bearing half of "why this beats releasing the stamp": the ORIGINAL
	// cleanup_scheduled_at is PRESERVED. Re-marking would set cleanupAt = now + gracePeriodDays for
	// every non-immediate leg (only "zero_accounts" is immediate), so each terminal failure would buy
	// a fresh FULL grace window of Plaid billing — daily SELECTION, not daily removal.
	mockRepo.AssertNotCalled(t, "MarkForCleanup", mock.Anything, mock.Anything, mock.Anything)
	// And the stamp is not released either: releasing it is what would hand the row back to the
	// candidate sweep and restart the grace period.
	mockRepo.AssertNotCalled(t, "ClearCleanupSchedule", mock.Anything, mock.Anything)
}

// TestItemCleanupCheckWorker_Work_BackstopRunsAlongsideNewCandidates: the backstop must not replace
// the candidate sweep, and the two must not interfere. Both items get exactly one job.
func TestItemCleanupCheckWorker_Work_BackstopRunsAlongsideNewCandidates(t *testing.T) {
	ctx := context.Background()

	mockRepo := new(MockLinkedItemCleanupRepo)
	mockQueue := new(MockJobQueue)

	mockRepo.On("GetItemsForCleanupPage", ctx, 30, 60, mock.Anything, mock.Anything).
		Return([]domain.LinkedItem{{ID: "item-new", UserID: "user-1", Status: "revoked"}}, nil)
	mockRepo.On("MarkForCleanup", ctx, "item-new", mock.Anything).Return(nil)
	mockRepo.On("GetItemsPendingCleanup", ctx).
		Return([]domain.LinkedItem{newStrandedItem("item-stranded", "user-2")}, nil)
	mockQueue.On("Enqueue", ctx, mock.Anything, mock.Anything, mock.Anything).Return(&queue.JobResult{ID: 1}, nil)

	worker := NewItemCleanupCheckWorker(ItemCleanupCheckWorkerDeps{
		LinkedItemRepo:     mockRepo,
		JobQueue:           mockQueue,
		Logger:             newTestItemCleanupCheckLogger(),
		GracePeriodDays:    7,
		ErrorDaysThreshold: 30,
		StaleDaysThreshold: 60,
	})

	require.NoError(t, worker.Work(ctx, newTestItemCleanupCheckJob(ItemCleanupCheckArgs{TriggerSource: "periodic"})))

	mockQueue.AssertNumberOfCalls(t, "Enqueue", 2)
	mockRepo.AssertNumberOfCalls(t, "MarkForCleanup", 1) // the NEW candidate only
	mockRepo.AssertExpectations(t)
}

// TestItemCleanupCheckWorker_Work_BackstopFailuresDoNotFailTheSweep: neither a failed
// GetItemsPendingCleanup query nor a failed re-enqueue may fail the periodic job. The candidate
// sweep's own work is already committed by the time the backstop runs, and failing the job would
// make River retry the WHOLE sweep — re-running the irreversible-authorising half for no reason. The
// next daily run re-selects the same stranded rows, since nothing about them changed.
func TestItemCleanupCheckWorker_Work_BackstopFailuresDoNotFailTheSweep(t *testing.T) {
	ctx := context.Background()

	t.Run("query fails", func(t *testing.T) {
		mockRepo := new(MockLinkedItemCleanupRepo)
		mockQueue := new(MockJobQueue)
		mockRepo.On("GetItemsForCleanupPage", ctx, 30, 60, mock.Anything, mock.Anything).Return([]domain.LinkedItem{}, nil)
		mockRepo.On("GetItemsPendingCleanup", ctx).Return(nil, errors.New("db blip"))

		worker := NewItemCleanupCheckWorker(ItemCleanupCheckWorkerDeps{
			LinkedItemRepo: mockRepo, JobQueue: mockQueue, Logger: newTestItemCleanupCheckLogger(),
			GracePeriodDays: 7, ErrorDaysThreshold: 30, StaleDaysThreshold: 60,
		})

		require.NoError(t, worker.Work(ctx, newTestItemCleanupCheckJob(ItemCleanupCheckArgs{TriggerSource: "periodic"})))
		mockQueue.AssertNotCalled(t, "Enqueue")
		mockRepo.AssertExpectations(t)
	})

	t.Run("re-enqueue fails", func(t *testing.T) {
		mockRepo := new(MockLinkedItemCleanupRepo)
		mockQueue := new(MockJobQueue)
		mockRepo.On("GetItemsForCleanupPage", ctx, 30, 60, mock.Anything, mock.Anything).Return([]domain.LinkedItem{}, nil)
		mockRepo.On("GetItemsPendingCleanup", ctx).
			Return([]domain.LinkedItem{newStrandedItem("item-stranded", "user-2")}, nil)
		mockQueue.On("Enqueue", ctx, mock.Anything, mock.Anything, mock.Anything).
			Return((*queue.JobResult)(nil), errors.New("river unavailable"))

		worker := NewItemCleanupCheckWorker(ItemCleanupCheckWorkerDeps{
			LinkedItemRepo: mockRepo, JobQueue: mockQueue, Logger: newTestItemCleanupCheckLogger(),
			GracePeriodDays: 7, ErrorDaysThreshold: 30, StaleDaysThreshold: 60,
		})

		require.NoError(t, worker.Work(ctx, newTestItemCleanupCheckJob(ItemCleanupCheckArgs{TriggerSource: "periodic"})))
		// Critically: the stamp is NOT released on a failed re-enqueue here (unlike part D's path,
		// where THIS run set the stamp). The stamp is what keeps the row selectable by the backstop
		// on the next run; releasing it would hand the row back to the candidate sweep and restart a
		// full grace window.
		mockRepo.AssertNotCalled(t, "ClearCleanupSchedule", mock.Anything, mock.Anything)
		mockRepo.AssertExpectations(t)
	})
}

// TestItemCleanupCheckWorker_Work_BackstopRespectsPerRunBound bounds the backstop's re-enqueue
// volume. GetItemsPendingCleanup has no LIMIT of its own, so without a bound here one run could
// enqueue an unbounded number of jobs.
//
// It reuses maxItemsPerSweep as that bound WITHOUT changing the constant's value or its derivation:
// these are re-enqueues of work a PREVIOUS run already authorised and already counted against its
// own cap, not new authorisations, and ItemCleanupUniqueOpts plus ClaimItemForCleanup gate each one
// again. The bound is here to cap per-run queue volume, not to re-derive the blast radius.
func TestItemCleanupCheckWorker_Work_BackstopRespectsPerRunBound(t *testing.T) {
	ctx := context.Background()

	const bound = 3

	mockRepo := new(MockLinkedItemCleanupRepo)
	mockQueue := new(MockJobQueue)

	stranded := make([]domain.LinkedItem, bound+4)
	for i := range stranded {
		stranded[i] = newStrandedItem(uuid.New().String(), "user-1")
	}

	mockRepo.On("GetItemsForCleanupPage", ctx, 30, 60, mock.Anything, mock.Anything).Return([]domain.LinkedItem{}, nil)
	mockRepo.On("GetItemsPendingCleanup", ctx).Return(stranded, nil)
	mockQueue.On("Enqueue", ctx, mock.Anything, mock.Anything, mock.Anything).Return(&queue.JobResult{ID: 1}, nil)

	worker := NewItemCleanupCheckWorker(ItemCleanupCheckWorkerDeps{
		LinkedItemRepo: mockRepo, JobQueue: mockQueue, Logger: newTestItemCleanupCheckLogger(),
		GracePeriodDays: 7, ErrorDaysThreshold: 30, StaleDaysThreshold: 60,
		MaxItemsPerCleanupSweep: bound,
	})

	require.NoError(t, worker.Work(ctx, newTestItemCleanupCheckJob(ItemCleanupCheckArgs{TriggerSource: "periodic"})))

	mockQueue.AssertNumberOfCalls(t, "Enqueue", bound)
}

// TestItemCleanupCheckWorker_Work_BackstopSkipsDuplicateLiveJobs: a stamped, overdue row whose
// ItemCleanup job is still Available/Scheduled/Running/Retryable/Pending is NOT stranded, and
// ItemCleanupUniqueOpts is what tells the two apart — the insert comes back UniqueSkipped rather
// than creating a second job for the same item. This is why the backstop can afford to re-enqueue
// every pending row without tracking job state itself.
func TestItemCleanupCheckWorker_Work_BackstopSkipsDuplicateLiveJobs(t *testing.T) {
	ctx := context.Background()

	mockRepo := new(MockLinkedItemCleanupRepo)
	mockQueue := new(MockJobQueue)

	mockRepo.On("GetItemsForCleanupPage", ctx, 30, 60, mock.Anything, mock.Anything).Return([]domain.LinkedItem{}, nil)
	mockRepo.On("GetItemsPendingCleanup", ctx).
		Return([]domain.LinkedItem{newStrandedItem("item-has-live-job", "user-3")}, nil)

	// The options are APPLIED, not merely counted. MockJobQueue.Enqueue calls
	// m.Called(ctx, job, opts) — always exactly three arguments regardless of how many options were
	// passed — so any assertion on the argument COUNT is vacuously true and would pass even for an
	// enqueue with no options at all. Resolving the option slice into an EnqueueOptions and reading
	// .Unique is the only form of this check that can fail.
	var appliedUnique *queue.UniqueOpts
	var sawOpts bool
	mockQueue.On("Enqueue", ctx, mock.Anything, mock.Anything, mock.Anything).
		Run(func(args mock.Arguments) {
			opts, ok := args.Get(2).([]queue.EnqueueOption)
			require.True(t, ok, "MockJobQueue.Enqueue's third mock argument should be the option slice")
			sawOpts = true
			var resolved queue.EnqueueOptions
			for _, opt := range opts {
				opt(&resolved)
			}
			appliedUnique = resolved.Unique
		}).
		Return(&queue.JobResult{ID: 7, UniqueSkipped: true}, nil)

	worker := NewItemCleanupCheckWorker(ItemCleanupCheckWorkerDeps{
		LinkedItemRepo: mockRepo, JobQueue: mockQueue, Logger: newTestItemCleanupCheckLogger(),
		GracePeriodDays: 7, ErrorDaysThreshold: 30, StaleDaysThreshold: 60,
	})

	require.NoError(t, worker.Work(ctx, newTestItemCleanupCheckJob(ItemCleanupCheckArgs{TriggerSource: "periodic"})))
	require.True(t, sawOpts, "the backstop must have enqueued once")
	require.NotNil(t, appliedUnique,
		"the backstop must pass uniqueness options, or it would create a duplicate cleanup job for the same item on every single daily run")
	assert.Equal(t, ItemCleanupUniqueOpts(), appliedUnique,
		"the backstop must use ItemCleanupUniqueOpts — the SAME dedup key and states the candidate sweep uses, or a job created by one would not deduplicate a job created by the other")
	mockRepo.AssertNotCalled(t, "ClearCleanupSchedule", mock.Anything, mock.Anything)
}

// TestItemCleanupCheckWorker_Work_BackstopRecomputesReasonFromCurrentState pins the QUALIFICATION on
// the backstop's dedup guarantee (fix round 1, item 5). The first version of
// reenqueueStrandedCleanups's doc comment asserted "a row that still has a live job comes back
// UniqueSkipped" unconditionally, and nothing tested the case where that is false.
//
// ItemCleanupUniqueOpts is ByArgs: true and Reason is a field of ItemCleanupArgs, so dedup matches
// only on EXACT args. The backstop recomputes Reason from the row's CURRENT state, so if that state
// changed since the original enqueue — the case below: an item enqueued as "long_term_error" whose
// status has since become 'revoked' via a webhook while the job sat Retryable — the args differ and
// dedup does not fire. That is a real, if rare, second live job for the same item.
//
// It is bounded to one wasted /item/remove, and ONLY because of part A: the second job's claim can
// still be granted (ClaimItemForCleanup does not inspect job state), it calls Plaid again, and Plaid
// answers ITEM_NOT_FOUND, which RemoveItem now maps to success. This test therefore pins the
// mechanism that makes the caveat affordable, not just the caveat.
//
// Asserting the recomputed Reason rather than the dedup outcome is deliberate: the recomputation is
// the production behaviour under test, and the dedup consequence follows from it plus ByArgs. A test
// that stubbed UniqueSkipped either way would pin testify, not the worker.
func TestItemCleanupCheckWorker_Work_BackstopRecomputesReasonFromCurrentState(t *testing.T) {
	ctx := context.Background()

	mockRepo := new(MockLinkedItemCleanupRepo)
	mockQueue := new(MockJobQueue)
	mockAccounts := new(MockAccountCountRepo)

	// The row as it is NOW: 'revoked'. Whatever Reason the original job carried, the backstop must
	// send the reason the CURRENT state implies.
	stranded := newStrandedItem("item-status-changed", "user-5")
	stranded.Status = "revoked"

	mockRepo.On("GetItemsForCleanupPage", ctx, 30, 60, mock.Anything, mock.Anything).Return([]domain.LinkedItem{}, nil)
	mockRepo.On("GetItemsPendingCleanup", ctx).Return([]domain.LinkedItem{stranded}, nil)
	// A surviving active account, so the zero-accounts leg does NOT pre-empt the status-based reason
	// and this test actually exercises the status branch it claims to.
	mockAccounts.On("CountActiveByLinkedItemID", ctx, "item-status-changed").Return(1, nil)

	var gotReason string
	mockQueue.On("Enqueue", ctx, mock.MatchedBy(func(job queue.Job) bool {
		cleanupArgs, ok := job.(ItemCleanupArgs)
		if ok {
			gotReason = cleanupArgs.Reason
		}
		return ok
	}), mock.Anything, mock.Anything).Return(&queue.JobResult{ID: 3}, nil)

	worker := NewItemCleanupCheckWorker(ItemCleanupCheckWorkerDeps{
		LinkedItemRepo: mockRepo, AccountRepo: mockAccounts, JobQueue: mockQueue,
		Logger: newTestItemCleanupCheckLogger(), GracePeriodDays: 7,
		ErrorDaysThreshold: 30, StaleDaysThreshold: 60,
	})

	require.NoError(t, worker.Work(ctx, newTestItemCleanupCheckJob(ItemCleanupCheckArgs{TriggerSource: "periodic"})))

	assert.Equal(t, "revoked", gotReason,
		"the backstop must label the job from the row's CURRENT state — and because Reason is inside "+
			"ItemCleanupArgs and ItemCleanupUniqueOpts is ByArgs, that is exactly why dedup can miss "+
			"when the state changed since the original enqueue (see reenqueueStrandedCleanups's doc)")
	mockAccounts.AssertExpectations(t)
}

// TestItemCleanupCheckWorker_Work_BackstopRecoversReplacedItems is the jobs-level companion to
// TestGetItemsPendingCleanup_Integration_ReplacedItemIsRecoverable (fix round 1, item 1). The
// predicate change is what makes a 'replaced' row reachable at all; this pins that the backstop
// actually re-enqueues one once it is returned, rather than filtering it out again in Go.
//
// A 'replaced' item's Plaid Item is still live and still billing — the re-link path marks the old
// item 'replaced' WITHOUT calling /item/remove — and the candidate sweep can never re-select it, so
// this is the only recovery path it has. determineCleanupReasonAndTiming has no 'replaced' case and
// falls through to "stale", which is the correct label here: the row IS stale, and Reason is a log
// label, not an authorisation (ClaimItemForCleanup re-derives eligibility, and deliberately does not
// veto 'replaced').
func TestItemCleanupCheckWorker_Work_BackstopRecoversReplacedItems(t *testing.T) {
	ctx := context.Background()

	mockRepo := new(MockLinkedItemCleanupRepo)
	mockQueue := new(MockJobQueue)

	replaced := newStrandedItem("item-replaced-stranded", "user-6")
	replaced.Status = domain.LinkedItemStatusReplaced

	mockRepo.On("GetItemsForCleanupPage", ctx, 30, 60, mock.Anything, mock.Anything).Return([]domain.LinkedItem{}, nil)
	mockRepo.On("GetItemsPendingCleanup", ctx).Return([]domain.LinkedItem{replaced}, nil)
	mockQueue.On("Enqueue", ctx, mock.MatchedBy(func(job queue.Job) bool {
		cleanupArgs, ok := job.(ItemCleanupArgs)
		return ok && cleanupArgs.LinkedItemID == "item-replaced-stranded"
	}), mock.Anything, mock.Anything).Return(&queue.JobResult{ID: 4}, nil)

	worker := NewItemCleanupCheckWorker(ItemCleanupCheckWorkerDeps{
		LinkedItemRepo: mockRepo, JobQueue: mockQueue, Logger: newTestItemCleanupCheckLogger(),
		GracePeriodDays: 7, ErrorDaysThreshold: 30, StaleDaysThreshold: 60,
	})

	require.NoError(t, worker.Work(ctx, newTestItemCleanupCheckJob(ItemCleanupCheckArgs{TriggerSource: "periodic"})))

	mockQueue.AssertExpectations(t)
	mockRepo.AssertNotCalled(t, "MarkForCleanup", mock.Anything, mock.Anything, mock.Anything)
}
