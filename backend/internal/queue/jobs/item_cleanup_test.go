package jobs

import (
	"context"
	"errors"
	"log/slog"
	"testing"
	"time"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/queue"
	"github.com/maxnussbaum/BoostedBudget/internal/repository"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// MockPlaidCleanupService mocks the services.PlaidItemCleanupService interface
type MockPlaidCleanupService struct {
	mock.Mock
}

func (m *MockPlaidCleanupService) RemoveAllUserItems(ctx context.Context, userID string) error {
	args := m.Called(ctx, userID)
	return args.Error(0)
}

func (m *MockPlaidCleanupService) RemoveItem(ctx context.Context, linkedItemID string) error {
	args := m.Called(ctx, linkedItemID)
	return args.Error(0)
}

// Issue #679, #680: DisconnectAllUserItems for trial expiration
func (m *MockPlaidCleanupService) DisconnectAllUserItems(ctx context.Context, userID string) error {
	args := m.Called(ctx, userID)
	return args.Error(0)
}

// MockLinkedItemRepoForCleanup mocks the LinkedItemCleanupRepository interface
// Named to avoid conflict with MockLinkedItemCleanupRepo in item_cleanup_check_test.go
type MockLinkedItemRepoForCleanup struct {
	mock.Mock
}

func (m *MockLinkedItemRepoForCleanup) ClearCleanupSchedule(ctx context.Context, itemID string) error {
	args := m.Called(ctx, itemID)
	return args.Error(0)
}

// Implement other interface methods (not used in item_cleanup.go but required)
func (m *MockLinkedItemRepoForCleanup) GetItemsForCleanup(ctx context.Context, errorDays, staleDays int) ([]domain.LinkedItem, error) {
	args := m.Called(ctx, errorDays, staleDays)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]domain.LinkedItem), args.Error(1)
}

func (m *MockLinkedItemRepoForCleanup) GetItemsPendingCleanup(ctx context.Context) ([]domain.LinkedItem, error) {
	args := m.Called(ctx)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]domain.LinkedItem), args.Error(1)
}

func (m *MockLinkedItemRepoForCleanup) MarkForCleanup(ctx context.Context, itemID string, cleanupAt time.Time) error {
	args := m.Called(ctx, itemID, cleanupAt)
	return args.Error(0)
}

func (m *MockLinkedItemRepoForCleanup) Delete(ctx context.Context, id string) error {
	args := m.Called(ctx, id)
	return args.Error(0)
}

func newTestItemCleanupJob(args ItemCleanupArgs) *river.Job[ItemCleanupArgs] {
	return &river.Job[ItemCleanupArgs]{
		JobRow: &rivertype.JobRow{
			ID:      1,
			Attempt: 1,
		},
		Args: args,
	}
}

func TestItemCleanupArgs_Kind(t *testing.T) {
	args := ItemCleanupArgs{}
	assert.Equal(t, "item_cleanup", args.Kind())
}

func TestItemCleanupWorker_Work_Success(t *testing.T) {
	ctx := context.Background()
	linkedItemID := "item-123"
	userID := "user-456"

	mockCleanupSvc := new(MockPlaidCleanupService)
	mockRepo := new(MockLinkedItemRepoForCleanup)

	mockCleanupSvc.On("RemoveItem", ctx, linkedItemID).Return(nil)
	mockRepo.On("ClearCleanupSchedule", ctx, linkedItemID).Return(nil)
	mockRepo.On("Delete", ctx, linkedItemID).Return(nil)

	worker := NewItemCleanupWorker(ItemCleanupWorkerDeps{
		PlaidCleanupSvc: mockCleanupSvc,
		LinkedItemRepo:  mockRepo,
		Logger:          slog.Default(),
	})

	job := newTestItemCleanupJob(ItemCleanupArgs{
		LinkedItemID: linkedItemID,
		UserID:       userID,
		Reason:       "long_term_error",
	})

	err := worker.Work(ctx, job)

	require.NoError(t, err)
	mockCleanupSvc.AssertExpectations(t)
	mockRepo.AssertExpectations(t)
}

func TestItemCleanupWorker_Work_ItemNotFoundSkipsCleanup(t *testing.T) {
	ctx := context.Background()
	linkedItemID := "item-already-removed"

	mockCleanupSvc := new(MockPlaidCleanupService)
	mockRepo := new(MockLinkedItemRepoForCleanup)

	// Return item not found error - this should result in success
	mockCleanupSvc.On("RemoveItem", ctx, linkedItemID).
		Return(repository.ErrLinkedItemNotFound)
	// Delete IS called even when item-not-found — orphan DB row must be cleaned up
	mockRepo.On("Delete", ctx, linkedItemID).Return(nil)

	worker := NewItemCleanupWorker(ItemCleanupWorkerDeps{
		PlaidCleanupSvc: mockCleanupSvc,
		LinkedItemRepo:  mockRepo,
		Logger:          slog.Default(),
	})

	job := newTestItemCleanupJob(ItemCleanupArgs{
		LinkedItemID: linkedItemID,
		UserID:       "user-456",
		Reason:       "revoked",
	})

	err := worker.Work(ctx, job)

	// Should succeed - item was already removed
	require.NoError(t, err)
	mockCleanupSvc.AssertExpectations(t)
	mockRepo.AssertExpectations(t)
	// ClearCleanupSchedule should NOT be called when item not found
	mockRepo.AssertNotCalled(t, "ClearCleanupSchedule")
}

func TestItemCleanupWorker_Work_CleanupError(t *testing.T) {
	ctx := context.Background()
	linkedItemID := "item-123"

	mockCleanupSvc := new(MockPlaidCleanupService)
	mockRepo := new(MockLinkedItemRepoForCleanup)

	cleanupErr := errors.New("plaid API error")
	mockCleanupSvc.On("RemoveItem", ctx, linkedItemID).Return(cleanupErr)

	worker := NewItemCleanupWorker(ItemCleanupWorkerDeps{
		PlaidCleanupSvc: mockCleanupSvc,
		LinkedItemRepo:  mockRepo,
		Logger:          slog.Default(),
	})

	job := newTestItemCleanupJob(ItemCleanupArgs{
		LinkedItemID: linkedItemID,
		UserID:       "user-456",
		Reason:       "stale",
	})

	err := worker.Work(ctx, job)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to cleanup item")
	mockCleanupSvc.AssertExpectations(t)
	// ClearCleanupSchedule should NOT be called when cleanup fails
	mockRepo.AssertNotCalled(t, "ClearCleanupSchedule")
}

func TestItemCleanupWorker_Work_ClearScheduleErrorDoesNotFail(t *testing.T) {
	ctx := context.Background()
	linkedItemID := "item-123"

	mockCleanupSvc := new(MockPlaidCleanupService)
	mockRepo := new(MockLinkedItemRepoForCleanup)

	mockCleanupSvc.On("RemoveItem", ctx, linkedItemID).Return(nil)
	// ClearCleanupSchedule fails - but job should still succeed
	mockRepo.On("ClearCleanupSchedule", ctx, linkedItemID).
		Return(errors.New("database error"))
	mockRepo.On("Delete", ctx, linkedItemID).Return(nil)

	worker := NewItemCleanupWorker(ItemCleanupWorkerDeps{
		PlaidCleanupSvc: mockCleanupSvc,
		LinkedItemRepo:  mockRepo,
		Logger:          slog.Default(),
	})

	job := newTestItemCleanupJob(ItemCleanupArgs{
		LinkedItemID: linkedItemID,
		UserID:       "user-456",
		Reason:       "subscription_downgrade",
	})

	err := worker.Work(ctx, job)

	// Job should succeed even though ClearCleanupSchedule failed
	require.NoError(t, err)
	mockCleanupSvc.AssertExpectations(t)
	mockRepo.AssertExpectations(t)
}

func TestItemCleanupWorker_Work_ZeroAccountsReason(t *testing.T) {
	ctx := context.Background()
	linkedItemID := "item-with-no-accounts"
	userID := "user-456"

	mockCleanupSvc := new(MockPlaidCleanupService)
	mockRepo := new(MockLinkedItemRepoForCleanup)

	mockCleanupSvc.On("RemoveItem", ctx, linkedItemID).Return(nil)
	mockRepo.On("ClearCleanupSchedule", ctx, linkedItemID).Return(nil)
	mockRepo.On("Delete", ctx, linkedItemID).Return(nil)

	worker := NewItemCleanupWorker(ItemCleanupWorkerDeps{
		PlaidCleanupSvc: mockCleanupSvc,
		LinkedItemRepo:  mockRepo,
		Logger:          slog.Default(),
	})

	// Test with zero_accounts reason (immediate cleanup, no grace period)
	job := newTestItemCleanupJob(ItemCleanupArgs{
		LinkedItemID: linkedItemID,
		UserID:       userID,
		Reason:       "zero_accounts",
	})

	err := worker.Work(ctx, job)

	require.NoError(t, err)
	mockCleanupSvc.AssertExpectations(t)
	mockRepo.AssertExpectations(t)
}

func TestItemCleanupWorker_Work_UserDeletionReason(t *testing.T) {
	ctx := context.Background()
	linkedItemID := "item-123"
	userID := "user-being-deleted"

	mockCleanupSvc := new(MockPlaidCleanupService)
	mockRepo := new(MockLinkedItemRepoForCleanup)

	mockCleanupSvc.On("RemoveItem", ctx, linkedItemID).Return(nil)
	mockRepo.On("ClearCleanupSchedule", ctx, linkedItemID).Return(nil)
	mockRepo.On("Delete", ctx, linkedItemID).Return(nil)

	worker := NewItemCleanupWorker(ItemCleanupWorkerDeps{
		PlaidCleanupSvc: mockCleanupSvc,
		LinkedItemRepo:  mockRepo,
		Logger:          slog.Default(),
	})

	job := newTestItemCleanupJob(ItemCleanupArgs{
		LinkedItemID: linkedItemID,
		UserID:       userID,
		Reason:       "user_deletion",
	})

	err := worker.Work(ctx, job)

	require.NoError(t, err)
	mockCleanupSvc.AssertExpectations(t)
	mockRepo.AssertExpectations(t)
}

func TestItemCleanupUniqueOpts(t *testing.T) {
	opts := ItemCleanupUniqueOpts()

	assert.True(t, opts.ByArgs)
	assert.Contains(t, opts.ByState, queue.JobStateAvailable)
	assert.Contains(t, opts.ByState, queue.JobStateScheduled)
	assert.Contains(t, opts.ByState, queue.JobStateRunning)
	assert.Contains(t, opts.ByState, queue.JobStateRetryable)
	assert.Contains(t, opts.ByState, queue.JobStatePending)
}

func TestNewItemCleanupWorker_DefaultLogger(t *testing.T) {
	mockCleanupSvc := new(MockPlaidCleanupService)
	mockRepo := new(MockLinkedItemRepoForCleanup)

	worker := NewItemCleanupWorker(ItemCleanupWorkerDeps{
		PlaidCleanupSvc: mockCleanupSvc,
		LinkedItemRepo:  mockRepo,
		// Logger is nil - should use default
	})

	assert.NotNil(t, worker.logger)
}

func TestItemCleanupWorker_Work_AllReasons(t *testing.T) {
	// Test all cleanup reasons are handled
	reasons := []string{
		"long_term_error",
		"revoked",
		"stale",
		"zero_accounts",
		"subscription_downgrade",
		"user_deletion",
	}

	for _, reason := range reasons {
		t.Run(reason, func(t *testing.T) {
			ctx := context.Background()
			linkedItemID := "item-" + reason

			mockCleanupSvc := new(MockPlaidCleanupService)
			mockRepo := new(MockLinkedItemRepoForCleanup)

			mockCleanupSvc.On("RemoveItem", ctx, linkedItemID).Return(nil)
			mockRepo.On("ClearCleanupSchedule", ctx, linkedItemID).Return(nil)
			mockRepo.On("Delete", ctx, linkedItemID).Return(nil)

			worker := NewItemCleanupWorker(ItemCleanupWorkerDeps{
				PlaidCleanupSvc: mockCleanupSvc,
				LinkedItemRepo:  mockRepo,
				Logger:          slog.Default(),
			})

			job := newTestItemCleanupJob(ItemCleanupArgs{
				LinkedItemID: linkedItemID,
				UserID:       "user-123",
				Reason:       reason,
			})

			err := worker.Work(ctx, job)

			require.NoError(t, err)
			mockCleanupSvc.AssertExpectations(t)
			mockRepo.AssertExpectations(t)
		})
	}
}

func TestItemCleanupWorker_Work_DeleteErrorDoesNotFail(t *testing.T) {
	ctx := context.Background()
	linkedItemID := "item-delete-fails"
	userID := "user-456"

	mockCleanupSvc := new(MockPlaidCleanupService)
	mockRepo := new(MockLinkedItemRepoForCleanup)

	mockCleanupSvc.On("RemoveItem", ctx, linkedItemID).Return(nil)
	mockRepo.On("ClearCleanupSchedule", ctx, linkedItemID).Return(nil)
	// Delete fails - job should still succeed (best-effort)
	mockRepo.On("Delete", ctx, linkedItemID).Return(errors.New("database error"))

	worker := NewItemCleanupWorker(ItemCleanupWorkerDeps{
		PlaidCleanupSvc: mockCleanupSvc,
		LinkedItemRepo:  mockRepo,
		Logger:          slog.Default(),
	})

	job := newTestItemCleanupJob(ItemCleanupArgs{
		LinkedItemID: linkedItemID,
		UserID:       userID,
		Reason:       "user_deletion",
	})

	err := worker.Work(ctx, job)

	// Job should succeed even though Delete failed
	require.NoError(t, err)
	mockCleanupSvc.AssertExpectations(t)
	mockRepo.AssertExpectations(t)
}
