package jobs

import (
	"context"
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"log/slog"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/queue"
	"github.com/maxnussbaum/BoostedBudget/internal/repository"
	"github.com/maxnussbaum/BoostedBudget/internal/services"
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

// RemoveAllUserItems now requires an eligibility recheck (deletion-authorisation use-time check).
// The recheck is not part of what these job tests assert, so it is deliberately excluded from the
// expectation arguments — the existing `.On("RemoveAllUserItems", ctx, userID)` call sites stay valid.
func (m *MockPlaidCleanupService) RemoveAllUserItems(ctx context.Context, userID string, _ func(ctx context.Context) (bool, error)) error {
	args := m.Called(ctx, userID)
	return args.Error(0)
}

// RemoveItem consults the use-time eligibility recheck exactly as the real service does (Codex
// pass-5 Medium 2), because that is the only way a WORKER test can observe what Work does when the
// item stops being a cleanup candidate between the claim and the removal. The recheck is
// deliberately not part of the expectation arguments, so every pre-existing
// `.On("RemoveItem", ctx, itemID)` call site stays valid; when the recheck declines, no Plaid work
// happens and m.Called is never reached, which is the point.
func (m *MockPlaidCleanupService) RemoveItem(ctx context.Context, linkedItemID string, recheck func(ctx context.Context) (bool, error)) error {
	if recheck == nil {
		return fmt.Errorf("RemoveItem: an eligibility recheck is required")
	}
	eligible, err := recheck(ctx)
	if err != nil {
		return fmt.Errorf("eligibility recheck failed for item %s: %w", linkedItemID, err)
	}
	if !eligible {
		return fmt.Errorf("%w: item %s", services.ErrItemNoLongerEligible, linkedItemID)
	}
	args := m.Called(ctx, linkedItemID)
	return args.Error(0)
}

// DisconnectAllUserItemsIfEligible is never called through this mock — item-cleanup job tests
// exercise Work() paths that only use RemoveItem/RemoveAllUserItems. Bare no-op stubbed only to
// satisfy the services.PlaidItemCleanupService interface. (Its nil-recheck twin,
// DisconnectAllUserItems, was deleted outright by Issue #1500 — see that interface's doc comment.)
func (m *MockPlaidCleanupService) DisconnectAllUserItemsIfEligible(ctx context.Context, userID string, recheck func(ctx context.Context) (bool, error)) error {
	return nil
}

// SetWorkClaimsRepo (Issue #1413 H1) is never called through this mock — item-cleanup job tests
// exercise Work() on an already-constructed worker, not service wiring — so this is a bare no-op
// (no m.Called) rather than a testify expectation, avoiding an unregistered-expectation panic.
// Stubbed only to satisfy the widened services.PlaidItemCleanupService interface. Note: this is
// one of the unavoidable, minimal, mechanical touches to internal/queue/jobs required by this
// task (the package would otherwise fail to compile) — see the #1413 P3 report for detail.
func (m *MockPlaidCleanupService) SetWorkClaimsRepo(repo repository.WorkClaimsRepository) {}

// MockLinkedItemRepoForCleanup mocks the LinkedItemCleanupRepository interface
// Named to avoid conflict with MockLinkedItemCleanupRepo in item_cleanup_check_test.go
type MockLinkedItemRepoForCleanup struct {
	mock.Mock
}

func (m *MockLinkedItemRepoForCleanup) ClearCleanupSchedule(ctx context.Context, itemID string) error {
	args := m.Called(ctx, itemID)
	return args.Error(0)
}

// ClaimItemForCleanup (Issue #1486) is the eligibility recheck ItemCleanupWorker.Work must perform
// before the irreversible Plaid /item/remove. A real testify expectation, not a no-op stub: whether
// it is called at all, and with which thresholds, is exactly what these tests assert.
func (m *MockLinkedItemRepoForCleanup) ClaimItemForCleanup(ctx context.Context, itemID string, errorDays, staleDays int) (bool, error) {
	args := m.Called(ctx, itemID, errorDays, staleDays)
	return args.Bool(0), args.Error(1)
}

// expectCleanupClaimGranted programs the recheck to grant the claim, which is the precondition for
// every pre-existing test in this file: they were all written before the recheck existed and all
// assert the removal path.
func expectCleanupClaimGranted(mockRepo *MockLinkedItemRepoForCleanup, ctx context.Context, itemID string) {
	mockRepo.On("ClaimItemForCleanup", ctx, itemID, defaultCleanupErrorDays, defaultCleanupStaleDays).Return(true, nil)
}

// Implement other interface methods (not used in item_cleanup.go but required)
// GetItemsForCleanupPage satisfies LinkedItemCleanupRepository (Issue #1469). ItemCleanupWorker —
// the worker these tests exercise — never calls it; it shares the interface with
// ItemCleanupCheckWorker, which is the sweep that does. Kept alongside the unbounded
// GetItemsForCleanup below rather than replacing it, because this mock is also used where the wide
// repository.LinkedItemRepository shape is expected.
func (m *MockLinkedItemRepoForCleanup) GetItemsForCleanupPage(ctx context.Context, errorDays, staleDays int, after *repository.ItemCleanupSweepCursor, limit int) ([]domain.LinkedItem, error) {
	args := m.Called(ctx, errorDays, staleDays, after, limit)
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

// TestItemCleanupWorker_Work_NoLongerEligible_SkipsIrreversibleRemoval is Issue #1486's failing-
// first test. The job was enqueued (and, for the deferred legs, sat behind a 7-day grace period)
// while the item was a cleanup candidate; by the time it runs, the user has reconnected. Nothing
// clears cleanup_scheduled_at on reconnect — Issue #1486 follow-up part E: there are FOUR
// production callers of ClearCleanupSchedule, not two, and only two are in item_cleanup.go — one in
// ItemCleanupWorker.Work itself and one in the releaseDeclinedCleanupSchedule helper that both of
// that worker's decline points share (see Work's #1486
// comment for the enumeration, and TestClearCleanupScheduleProductionCallers_MatchTheDocumentedSet
// below); none of them is a reconnection hook, which is the part that matters here — so the ONLY
// thing that can stop the irreversible Plaid /item/remove is an eligibility recheck at execution
// time.
//
// Before the fix, Work called RemoveItem unconditionally and this test failed on
// AssertNotCalled(RemoveItem).
func TestItemCleanupWorker_Work_NoLongerEligible_SkipsIrreversibleRemoval(t *testing.T) {
	ctx := context.Background()
	linkedItemID := "item-reconnected"

	mockCleanupSvc := new(MockPlaidCleanupService)
	mockRepo := new(MockLinkedItemRepoForCleanup)

	mockRepo.On("ClaimItemForCleanup", ctx, linkedItemID, defaultCleanupErrorDays, defaultCleanupStaleDays).
		Return(false, nil)
	// Declining releases the stale schedule so the sweep (which requires cleanup_scheduled_at IS
	// NULL) can consider the item again if it degrades a second time.
	mockRepo.On("ClearCleanupSchedule", ctx, linkedItemID).Return(nil)

	worker := NewItemCleanupWorker(ItemCleanupWorkerDeps{
		PlaidCleanupSvc: mockCleanupSvc,
		LinkedItemRepo:  mockRepo,
		Logger:          slog.Default(),
	})

	err := worker.Work(ctx, newTestItemCleanupJob(ItemCleanupArgs{
		LinkedItemID: linkedItemID,
		UserID:       "user-456",
		Reason:       "long_term_error",
	}))

	// A no-longer-eligible item is a SUCCESS with nothing to do, not a failure: returning an error
	// would retry the job and re-attempt the same removal.
	require.NoError(t, err)
	mockCleanupSvc.AssertNotCalled(t, "RemoveItem")
	mockRepo.AssertNotCalled(t, "Delete")
	mockRepo.AssertExpectations(t)
}

// TestItemCleanupWorker_Work_NoLongerEligible_ClearFailureIsRetried is the failing-first test for
// the recovery defect in the decline path: the clear was best-effort, so a transient DB failure was
// logged, Work returned nil, River recorded success and never retried — leaving cleanup_scheduled_at
// set forever. Because the sweep's candidate predicate requires cleanup_scheduled_at IS NULL, that
// permanently hides a still-billing Plaid item, which is the exact hazard the decline path's own
// comment describes. Only the CLEAR failing is retryable; the decline itself is still a success.
//
// Before the fix this failed on require.Error: Work swallowed clearErr and returned nil.
func TestItemCleanupWorker_Work_NoLongerEligible_ClearFailureIsRetried(t *testing.T) {
	ctx := context.Background()
	linkedItemID := "item-clear-fails"

	mockCleanupSvc := new(MockPlaidCleanupService)
	mockRepo := new(MockLinkedItemRepoForCleanup)

	mockRepo.On("ClaimItemForCleanup", ctx, linkedItemID, defaultCleanupErrorDays, defaultCleanupStaleDays).
		Return(false, nil)
	mockRepo.On("ClearCleanupSchedule", ctx, linkedItemID).Return(errors.New("connection reset"))

	worker := NewItemCleanupWorker(ItemCleanupWorkerDeps{
		PlaidCleanupSvc: mockCleanupSvc,
		LinkedItemRepo:  mockRepo,
		Logger:          slog.Default(),
	})

	err := worker.Work(ctx, newTestItemCleanupJob(ItemCleanupArgs{
		LinkedItemID: linkedItemID,
		UserID:       "user-456",
		Reason:       "long_term_error",
	}))

	require.Error(t, err, "a failed schedule release must surface so River retries it")
	assert.Contains(t, err.Error(), "connection reset")
	// The retry must not become a licence to remove: the decline still stands.
	mockCleanupSvc.AssertNotCalled(t, "RemoveItem")
	mockRepo.AssertNotCalled(t, "Delete")
	mockRepo.AssertExpectations(t)
}

// TestItemCleanupWorker_Work_StillEligible_ProceedsWithRemoval is the mandatory sibling of the test
// above. Without it, that test could pass because the claim always denies, or because the worker
// skips everything unconditionally — the both-false trap. Together they pin that the recheck
// DISCRIMINATES.
func TestItemCleanupWorker_Work_StillEligible_ProceedsWithRemoval(t *testing.T) {
	ctx := context.Background()
	linkedItemID := "item-still-broken"

	mockCleanupSvc := new(MockPlaidCleanupService)
	mockRepo := new(MockLinkedItemRepoForCleanup)

	expectCleanupClaimGranted(mockRepo, ctx, linkedItemID)
	mockCleanupSvc.On("RemoveItem", ctx, linkedItemID).Return(nil)
	mockRepo.On("ClearCleanupSchedule", ctx, linkedItemID).Return(nil)
	mockRepo.On("Delete", ctx, linkedItemID).Return(nil)

	worker := NewItemCleanupWorker(ItemCleanupWorkerDeps{
		PlaidCleanupSvc: mockCleanupSvc,
		LinkedItemRepo:  mockRepo,
		Logger:          slog.Default(),
	})

	err := worker.Work(ctx, newTestItemCleanupJob(ItemCleanupArgs{
		LinkedItemID: linkedItemID,
		UserID:       "user-456",
		Reason:       "long_term_error",
	}))

	require.NoError(t, err)
	mockCleanupSvc.AssertExpectations(t)
	mockRepo.AssertExpectations(t)
}

// TestItemCleanupWorker_Work_ClaimError_FailsClosed pins that "we could not tell" is never treated
// as "still eligible". A recheck that errored must NOT be followed by an irreversible removal; the
// job returns an error so River retries it.
func TestItemCleanupWorker_Work_ClaimError_FailsClosed(t *testing.T) {
	ctx := context.Background()
	linkedItemID := "item-claim-errors"

	mockCleanupSvc := new(MockPlaidCleanupService)
	mockRepo := new(MockLinkedItemRepoForCleanup)

	mockRepo.On("ClaimItemForCleanup", ctx, linkedItemID, defaultCleanupErrorDays, defaultCleanupStaleDays).
		Return(false, errors.New("database is down"))

	worker := NewItemCleanupWorker(ItemCleanupWorkerDeps{
		PlaidCleanupSvc: mockCleanupSvc,
		LinkedItemRepo:  mockRepo,
		Logger:          slog.Default(),
	})

	err := worker.Work(ctx, newTestItemCleanupJob(ItemCleanupArgs{
		LinkedItemID: linkedItemID,
		UserID:       "user-456",
		Reason:       "stale",
	}))

	require.Error(t, err)
	assert.Contains(t, err.Error(), "database is down")
	mockCleanupSvc.AssertNotCalled(t, "RemoveItem")
	// The schedule must survive a claim error: clearing it would hide the item from the sweep too.
	mockRepo.AssertNotCalled(t, "ClearCleanupSchedule")
	mockRepo.AssertNotCalled(t, "Delete")
	mockRepo.AssertExpectations(t)
}

// TestItemCleanupWorker_Work_UsesConfiguredThresholds pins that the recheck is run with the
// worker's CONFIGURED thresholds, not the package defaults — the pairing with the sweep is what
// keeps the recheck from denying claims the sweep legitimately created.
func TestItemCleanupWorker_Work_UsesConfiguredThresholds(t *testing.T) {
	ctx := context.Background()
	linkedItemID := "item-custom-thresholds"

	mockCleanupSvc := new(MockPlaidCleanupService)
	mockRepo := new(MockLinkedItemRepoForCleanup)

	// Deliberately NOT expectCleanupClaimGranted: this test's whole point is that the CONFIGURED
	// 5/11 reach the recheck, so the default-threshold expectation must be absent.
	mockRepo.On("ClaimItemForCleanup", ctx, linkedItemID, 5, 11).Return(true, nil)
	mockCleanupSvc.On("RemoveItem", ctx, linkedItemID).Return(nil)
	mockRepo.On("ClearCleanupSchedule", ctx, linkedItemID).Return(nil)
	mockRepo.On("Delete", ctx, linkedItemID).Return(nil)

	worker := NewItemCleanupWorker(ItemCleanupWorkerDeps{
		PlaidCleanupSvc:    mockCleanupSvc,
		LinkedItemRepo:     mockRepo,
		Logger:             slog.Default(),
		ErrorDaysThreshold: 5,
		StaleDaysThreshold: 11,
	})

	require.NoError(t, worker.Work(ctx, newTestItemCleanupJob(ItemCleanupArgs{
		LinkedItemID: linkedItemID,
		UserID:       "user-456",
		Reason:       "revoked",
	})))

	mockRepo.AssertExpectations(t)
}

// TestItemCleanupWorkers_ShareEligibilityThresholdDefaults pins the invariant defaultCleanupErrorDays
// / defaultCleanupStaleDays exist for: the sweep that SCHEDULES a cleanup and the worker that
// RE-VERIFIES it must default to the same thresholds, or the recheck silently denies claims the
// sweep created and the item keeps billing forever (its cleanup_scheduled_at stays set, so the
// sweep never re-selects it).
//
// Constructing both workers from zero-valued deps is the production path as of #1486:
// cmd/server/main.go passes no thresholds to either constructor, precisely so that neither can be
// edited one-sidedly. This test therefore pins the wiring production actually uses, not a
// default-only path it bypasses.
func TestItemCleanupWorkers_ShareEligibilityThresholdDefaults(t *testing.T) {
	sweep := NewItemCleanupCheckWorker(ItemCleanupCheckWorkerDeps{})
	remover := NewItemCleanupWorker(ItemCleanupWorkerDeps{})

	assert.Equal(t, sweep.errorDaysThreshold, remover.errorDaysThreshold,
		"the sweep and the eligibility recheck must default to the same errorDays threshold")
	assert.Equal(t, sweep.staleDaysThreshold, remover.staleDaysThreshold,
		"the sweep and the eligibility recheck must default to the same staleDays threshold")
	assert.Equal(t, defaultCleanupErrorDays, remover.errorDaysThreshold)
	assert.Equal(t, defaultCleanupStaleDays, remover.staleDaysThreshold)
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

	expectCleanupClaimGranted(mockRepo, ctx, linkedItemID)
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
	expectCleanupClaimGranted(mockRepo, ctx, linkedItemID)
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
	expectCleanupClaimGranted(mockRepo, ctx, linkedItemID)
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

	expectCleanupClaimGranted(mockRepo, ctx, linkedItemID)
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

	expectCleanupClaimGranted(mockRepo, ctx, linkedItemID)
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

	expectCleanupClaimGranted(mockRepo, ctx, linkedItemID)
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

			expectCleanupClaimGranted(mockRepo, ctx, linkedItemID)
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

	expectCleanupClaimGranted(mockRepo, ctx, linkedItemID)
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

// TestClearCleanupScheduleProductionCallers_MatchTheDocumentedSet makes the ClearCleanupSchedule
// caller enumeration FALSIFIABLE rather than merely asserted (Issue #1486 follow-up part E).
//
// Four comments in this codebase claimed there were exactly two production callers, both inside
// ItemCleanupWorker.Work. There were three — and commit 24907762, whose own message says it corrects
// this very claim and that "there are two as of this branch", ADDED the third (in
// accountService.DeleteAccount) in the same diff. Part D adds a fourth, in
// ItemCleanupCheckWorker.Work. A prose claim that has already drifted twice needs a check, not a
// third careful re-reading: this test fails if a caller is added or removed without the comments
// being updated, and its failure message names the four comment sites to fix.
//
// AST-BASED, not a substring match — fix round 1, and the guard's own first version failed exactly
// the standard it was written to enforce. It matched the literal `".ClearCleanupSchedule(ctx,"`, on
// the theory that requiring `(ctx,` kept the comments QUOTING the grep from counting as callers. A
// reviewer added a real fifth production caller written `ClearCleanupSchedule(c, linkedItemID)`; it
// compiled, and this test PASSED with exit 0. So the claim "fails if a caller is added" was itself
// false — any non-`ctx` context identifier, or a call split across lines, walked straight past it.
//
// Parsing instead of pattern-matching removes the whole class: a *ast.CallExpr whose Fun is a
// *ast.SelectorExpr named ClearCleanupSchedule is a call however it is spelled, whitespace and
// receiver and argument names included, and comments are not part of the AST at all so the
// grep-quoting problem disappears rather than being worked around. Verified by re-applying that same
// fifth caller and watching this test go red.
//
// Counts CALLS only, so an interface/method declaration (an *ast.Field name) and a method VALUE
// passed without being invoked are both excluded. A method value would be a genuine miss, but it is
// not a shape this codebase uses for repository methods; if one ever appears, widen this to bare
// *ast.SelectorExpr rather than trusting the count.
func TestClearCleanupScheduleProductionCallers_MatchTheDocumentedSet(t *testing.T) {
	// Every production call site, keyed by path relative to backend/internal, with its call count.
	// Each entry must correspond to a bullet in ItemCleanupWorker.Work's #1486 enumeration.
	want := map[string]int{
		"queue/jobs/item_cleanup.go":       2, // after a successful RemoveItem; releasing a declined claim's stale stamp
		"queue/jobs/item_cleanup_check.go": 1, // #1486 follow-up part D: releasing the stamp after a failed sweep enqueue
		"services/account_service.go":      1, // releasing the stamp after a failed zero-account enqueue
	}

	internalRoot := filepath.Join("..", "..")
	got := map[string]int{}
	err := filepath.WalkDir(internalRoot, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() || !strings.HasSuffix(d.Name(), ".go") || strings.HasSuffix(d.Name(), "_test.go") {
			return nil
		}
		// ParseComments is deliberately NOT set: comments must not influence the count, and this is
		// the structural reason the substring version needed its `(ctx,` hack in the first place.
		file, parseErr := parser.ParseFile(token.NewFileSet(), path, nil, 0)
		if parseErr != nil {
			return fmt.Errorf("parsing %s: %w", path, parseErr)
		}
		n := 0
		ast.Inspect(file, func(node ast.Node) bool {
			call, ok := node.(*ast.CallExpr)
			if !ok {
				return true
			}
			if sel, ok := call.Fun.(*ast.SelectorExpr); ok && sel.Sel != nil && sel.Sel.Name == "ClearCleanupSchedule" {
				n++
			}
			return true
		})
		if n == 0 {
			return nil
		}
		rel, relErr := filepath.Rel(internalRoot, path)
		if relErr != nil {
			return relErr
		}
		got[filepath.ToSlash(rel)] = n
		return nil
	})
	require.NoError(t, err)

	assert.Equal(t, want, got,
		"the set of production ClearCleanupSchedule callers changed. Four comments enumerate it and "+
			"must be updated together, or they become false the way they were before #1486 follow-up part E: "+
			"queue/jobs/item_cleanup.go (ItemCleanupWorker.Work's #1486 comment), "+
			"queue/jobs/item_cleanup_check.go (maxItemsPerCleanupSweep's third bullet), "+
			"repository/linked_item_repository.go (cleanupCandidateReasonPredicate's first bullet), "+
			"and queue/jobs/item_cleanup_test.go "+
			"(TestItemCleanupWorker_Work_NoLongerEligible_SkipsIrreversibleRemoval's comment). "+
			"The load-bearing half — NOTHING clears cleanup_scheduled_at on reconnection — must be "+
			"re-verified, not assumed, if a new caller appears.")
}
