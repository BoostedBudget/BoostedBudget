package jobs

import (
	"context"
	"errors"
	"log/slog"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/maxnussbaum/BoostedBudget/internal/cache"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/queue"
	"github.com/maxnussbaum/BoostedBudget/internal/repository"
	"github.com/maxnussbaum/BoostedBudget/internal/services"
	"github.com/plaid/plaid-go/v23/plaid"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// MockTransactionSyncService mocks the TransactionSyncService interface
type MockTransactionSyncService struct {
	mock.Mock
}

func (m *MockTransactionSyncService) ProcessWebhook(ctx context.Context, webhook *domain.WebhookEvent) error {
	args := m.Called(ctx, webhook)
	return args.Error(0)
}

func (m *MockTransactionSyncService) SyncTransactions(ctx context.Context, linkedItem *domain.LinkedItem) (*domain.TransactionSyncResult, error) {
	args := m.Called(ctx, linkedItem)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*domain.TransactionSyncResult), args.Error(1)
}

// SetTransferCorrelationService implements the TransactionSyncService interface.
// Issue #972: Added to fix mock after SetTransferCorrelationService was added to the interface.
func (m *MockTransactionSyncService) SetTransferCorrelationService(svc services.TransferCorrelationService) {
	m.Called(svc)
}

// SetSavingsProgressServices implements the TransactionSyncService interface.
// Issue #1300 G2: Added to fix mock after SetSavingsProgressServices was added to the interface.
func (m *MockTransactionSyncService) SetSavingsProgressServices(
	contribCleaner services.SyncContributionCleaner,
	progressUpdater services.SyncGoalProgressUpdater,
	categoryBackfiller services.SyncManagedCategoryBackfiller,
) {
	m.Called(contribCleaner, progressUpdater, categoryBackfiller)
}

// SetSavingsDetectionServices satisfies the TransactionSyncService interface (Issue #1300 M18 RG1).
func (m *MockTransactionSyncService) SetSavingsDetectionServices(_ services.SyncContributionsBasisGoalReader, _ services.SyncProposedContributionUpserter, _ repository.TransactionRepository) {
}

// SetCacheInvalidation satisfies the TransactionSyncService interface (Issue #1531 Task 10b-2).
// This test double never calls the real SyncTransactions, so there is nothing to wire — see
// plaid_sync_cache_invalidation_test.go for the end-to-end test that DOES wire this against a
// real transactionSyncService instance and a real (fake-Redis-backed) cache.Generations.
func (m *MockTransactionSyncService) SetCacheInvalidation(_ *cache.Generations, _ services.SyncCacheHouseholdLookup) {
}

// SetAuditLogger satisfies the TransactionSyncService interface (Issue #1569 Task 5).
func (m *MockTransactionSyncService) SetAuditLogger(_ services.AuditLogger) {}

// MockLinkedItemRepository mocks the LinkedItemRepository interface
type MockLinkedItemRepository struct {
	mock.Mock
}

func (m *MockLinkedItemRepository) Create(ctx context.Context, item *domain.LinkedItem) error {
	args := m.Called(ctx, item)
	return args.Error(0)
}

func (m *MockLinkedItemRepository) GetByID(ctx context.Context, id string) (*domain.LinkedItem, error) {
	args := m.Called(ctx, id)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*domain.LinkedItem), args.Error(1)
}

func (m *MockLinkedItemRepository) GetByUserID(ctx context.Context, userID string) ([]domain.LinkedItem, error) {
	args := m.Called(ctx, userID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]domain.LinkedItem), args.Error(1)
}

func (m *MockLinkedItemRepository) GetByItemID(ctx context.Context, itemID string) (*domain.LinkedItem, error) {
	args := m.Called(ctx, itemID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*domain.LinkedItem), args.Error(1)
}

func (m *MockLinkedItemRepository) GetByIDAndUser(ctx context.Context, id, userID string) (*domain.LinkedItem, error) {
	args := m.Called(ctx, id, userID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*domain.LinkedItem), args.Error(1)
}

func (m *MockLinkedItemRepository) UpdateCursor(ctx context.Context, id string, cursor string) error {
	args := m.Called(ctx, id, cursor)
	return args.Error(0)
}

func (m *MockLinkedItemRepository) Update(ctx context.Context, item *domain.LinkedItem) error {
	args := m.Called(ctx, item)
	return args.Error(0)
}

func (m *MockLinkedItemRepository) Delete(ctx context.Context, id string) error {
	args := m.Called(ctx, id)
	return args.Error(0)
}

func (m *MockLinkedItemRepository) PurgeLinkedItem(ctx context.Context, linkedItemID, userID string, auditLog repository.AuditLogRepository) error {
	args := m.Called(ctx, linkedItemID, userID, auditLog)
	return args.Error(0)
}

// RepairStatus atomically transitions a linked item to 'active' (Issue #1609 round 2).
func (m *MockLinkedItemRepository) RepairStatus(ctx context.Context, id string, userID string, allowedStatuses []domain.LinkedItemStatus, notNewerThan *time.Time) (bool, error) {
	args := m.Called(ctx, id, userID, allowedStatuses)
	return args.Bool(0), args.Error(1)
}

// TransitionErrorStatus atomically transitions a linked item to an error status (Issue #1609 P3-T2).
// SetInvestmentsSkippedAtLink records whether the investment jobs were skipped at link time
// (Issue #1613 P4-T3 item E).
func (m *MockLinkedItemRepository) SetInvestmentsSkippedAtLink(ctx context.Context, id string, skipped bool) error {
	args := m.Called(ctx, id, skipped)
	return args.Error(0)
}

// MarkFailureAsserted stamps last_failure_asserted_at unconditionally (Issue #1623).
func (m *MockLinkedItemRepository) MarkFailureAsserted(ctx context.Context, id string, terminalStatuses []domain.LinkedItemStatus) error {
	args := m.Called(ctx, id, terminalStatuses)
	return args.Error(0)
}

func (m *MockLinkedItemRepository) TransitionErrorStatus(ctx context.Context, id string, newStatus domain.LinkedItemStatus, errorCode, errorMessage string, terminalStatuses []domain.LinkedItemStatus) (bool, error) {
	args := m.Called(ctx, id, newStatus, errorCode, errorMessage, terminalStatuses)
	return args.Bool(0), args.Error(1)
}

func (m *MockLinkedItemRepository) GetByIDForUpdate(ctx context.Context, tx pgx.Tx, id string) (*domain.LinkedItem, error) {
	args := m.Called(ctx, tx, id)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*domain.LinkedItem), args.Error(1)
}

// GetByUserAndInstitution retrieves a linked item by user ID, institution ID, and status.
// Issue #171 - Plaid Re-Link Handling
func (m *MockLinkedItemRepository) GetByUserAndInstitution(ctx context.Context, userID, institutionID, status string) (*domain.LinkedItem, error) {
	args := m.Called(ctx, userID, institutionID, status)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*domain.LinkedItem), args.Error(1)
}

// GetItemsPendingCleanup - Issue #668
func (m *MockLinkedItemRepository) GetItemsPendingCleanup(ctx context.Context) ([]domain.LinkedItem, error) {
	args := m.Called(ctx)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]domain.LinkedItem), args.Error(1)
}

// MarkForCleanup - Issue #668
func (m *MockLinkedItemRepository) MarkForCleanup(ctx context.Context, itemID string, cleanupAt time.Time) error {
	args := m.Called(ctx, itemID, cleanupAt)
	return args.Error(0)
}

// ClearCleanupSchedule - Issue #668
func (m *MockLinkedItemRepository) ClearCleanupSchedule(ctx context.Context, itemID string) error {
	args := m.Called(ctx, itemID)
	return args.Error(0)
}

// GetActiveItemsByUserID - Issue #679, #680: Trial expiration
func (m *MockLinkedItemRepository) GetActiveItemsByUserID(ctx context.Context, userID string) ([]domain.LinkedItem, error) {
	args := m.Called(ctx, userID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]domain.LinkedItem), args.Error(1)
}

// MarkAllUserItemsDisconnected - Issue #679, #680: Trial expiration
func (m *MockLinkedItemRepository) MarkAllUserItemsDisconnected(ctx context.Context, userID string) error {
	args := m.Called(ctx, userID)
	return args.Error(0)
}

// MarkItemsDisconnected (Issue #1413 H2) is not exercised by these sync-job tests; stubbed to
// satisfy the widened repository.LinkedItemRepository interface. Note: this is the one
// unavoidable, minimal, mechanical touch to internal/queue/jobs required by this task (the
// package would otherwise fail to compile) — see the #1413 P3 report for detail.
func (m *MockLinkedItemRepository) MarkItemsDisconnected(ctx context.Context, ids []string) error {
	args := m.Called(ctx, ids)
	return args.Error(0)
}

// MarkPlaidRemoved (Issue #1495) is not exercised by these sync-job tests either; stubbed for the
// same compile-only reason as MarkItemsDisconnected above.
func (m *MockLinkedItemRepository) MarkPlaidRemoved(ctx context.Context, ids []string) error {
	args := m.Called(ctx, ids)
	return args.Error(0)
}

// MarkPlaidRemovalFailed records an unrecoverable Plaid removal verdict — Issue #1494.
func (m *MockLinkedItemRepository) MarkPlaidRemovalFailed(ctx context.Context, ids []string, errorCode string) error {
	args := m.Called(ctx, ids, errorCode)
	return args.Error(0)
}

// UpdateInvestmentsLastSyncedDate - Issue #659: Plaid Investments
func (m *MockLinkedItemRepository) UpdateInvestmentsLastSyncedDate(ctx context.Context, id string, date time.Time) error {
	args := m.Called(ctx, id, date)
	return args.Error(0)
}

// UpdateLiabilitiesLastSyncedAt - Issue #910: Plaid Liabilities
func (m *MockLinkedItemRepository) UpdateLiabilitiesLastSyncedAt(ctx context.Context, id string, t time.Time) error {
	args := m.Called(ctx, id, t)
	return args.Error(0)
}

// MockJobQueue mocks the JobQueue interface
type MockJobQueue struct {
	mock.Mock
}

func (m *MockJobQueue) Enqueue(ctx context.Context, job queue.Job, opts ...queue.EnqueueOption) (*queue.JobResult, error) {
	args := m.Called(ctx, job, opts)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*queue.JobResult), args.Error(1)
}

func (m *MockJobQueue) EnqueueTx(ctx context.Context, tx pgx.Tx, job queue.Job, opts ...queue.EnqueueOption) (*queue.JobResult, error) {
	args := m.Called(ctx, tx, job, opts)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*queue.JobResult), args.Error(1)
}

func (m *MockJobQueue) EnqueueMany(ctx context.Context, jobs []queue.Job, opts ...queue.EnqueueOption) ([]*queue.JobResult, error) {
	args := m.Called(ctx, jobs, opts)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]*queue.JobResult), args.Error(1)
}

func (m *MockJobQueue) EnqueueManyTx(ctx context.Context, tx pgx.Tx, jobs []queue.Job, opts ...queue.EnqueueOption) ([]*queue.JobResult, error) {
	args := m.Called(ctx, tx, jobs, opts)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]*queue.JobResult), args.Error(1)
}

// Test helpers
func newTestLogger() *slog.Logger {
	return slog.Default()
}

func newTestRiverJob(args PlaidSyncArgs) *river.Job[PlaidSyncArgs] {
	return &river.Job[PlaidSyncArgs]{
		JobRow: &rivertype.JobRow{
			ID:      1,
			Attempt: 1,
		},
		Args: args,
	}
}

func TestPlaidSyncArgs_Kind(t *testing.T) {
	args := PlaidSyncArgs{}
	assert.Equal(t, "plaid_sync", args.Kind())
}

func TestPlaidSyncWorker_Work_Success(t *testing.T) {
	ctx := context.Background()
	userID := uuid.New().String()
	itemID := uuid.New().String()

	linkedItem := &domain.LinkedItem{
		ID:                   itemID,
		UserID:               userID,
		ItemID:               "plaid-item-123",
		AccessTokenEncrypted: "encrypted-token",
		InstitutionID:        "ins_123",
		InstitutionName:      "Test Bank",
	}

	syncResult := &domain.TransactionSyncResult{
		Added: []plaid.Transaction{
			{TransactionId: "tx1"},
			{TransactionId: "tx2"},
		},
		Modified:  []plaid.Transaction{},
		Removed:   []plaid.RemovedTransaction{},
		NewCursor: "cursor-123",
	}

	mockRepo := new(MockLinkedItemRepository)
	mockSyncService := new(MockTransactionSyncService)
	mockQueue := new(MockJobQueue)

	mockRepo.On("GetByIDAndUser", ctx, itemID, userID).Return(linkedItem, nil)
	mockSyncService.On("SyncTransactions", ctx, linkedItem).Return(syncResult, nil)

	worker := NewPlaidSyncWorker(PlaidSyncWorkerDeps{
		SyncService:    mockSyncService,
		LinkedItemRepo: mockRepo,
		JobQueue:       mockQueue,
		Logger:         newTestLogger(),
	})

	job := newTestRiverJob(PlaidSyncArgs{
		UserID: userID,
		ItemID: itemID,
	})

	err := worker.Work(ctx, job)

	require.NoError(t, err)
	mockRepo.AssertExpectations(t)
	mockSyncService.AssertExpectations(t)
}

func TestPlaidSyncWorker_Work_NoTransactions(t *testing.T) {
	ctx := context.Background()
	userID := uuid.New().String()
	itemID := uuid.New().String()

	linkedItem := &domain.LinkedItem{
		ID:     itemID,
		UserID: userID,
	}

	syncResult := &domain.TransactionSyncResult{
		Added:     []plaid.Transaction{},
		Modified:  []plaid.Transaction{},
		Removed:   []plaid.RemovedTransaction{},
		NewCursor: "cursor-123",
	}

	mockRepo := new(MockLinkedItemRepository)
	mockSyncService := new(MockTransactionSyncService)

	mockRepo.On("GetByIDAndUser", ctx, itemID, userID).Return(linkedItem, nil)
	mockSyncService.On("SyncTransactions", ctx, linkedItem).Return(syncResult, nil)

	worker := NewPlaidSyncWorker(PlaidSyncWorkerDeps{
		SyncService:    mockSyncService,
		LinkedItemRepo: mockRepo,
		Logger:         newTestLogger(),
	})

	job := newTestRiverJob(PlaidSyncArgs{
		UserID: userID,
		ItemID: itemID,
	})

	err := worker.Work(ctx, job)

	require.NoError(t, err)
	mockRepo.AssertExpectations(t)
	mockSyncService.AssertExpectations(t)
}

func TestPlaidSyncWorker_Work_LinkedItemNotFound(t *testing.T) {
	ctx := context.Background()
	userID := uuid.New().String()
	itemID := uuid.New().String()

	mockRepo := new(MockLinkedItemRepository)
	mockSyncService := new(MockTransactionSyncService)

	mockRepo.On("GetByIDAndUser", ctx, itemID, userID).Return(nil, repository.ErrLinkedItemNotFound)

	worker := NewPlaidSyncWorker(PlaidSyncWorkerDeps{
		SyncService:    mockSyncService,
		LinkedItemRepo: mockRepo,
		Logger:         newTestLogger(),
	})

	job := newTestRiverJob(PlaidSyncArgs{
		UserID: userID,
		ItemID: itemID,
	})

	err := worker.Work(ctx, job)

	// Should NOT return error (don't retry for missing items)
	require.NoError(t, err)
	mockRepo.AssertExpectations(t)
	// SyncTransactions should NOT be called
	mockSyncService.AssertNotCalled(t, "SyncTransactions")
}

func TestPlaidSyncWorker_Work_RepositoryError(t *testing.T) {
	ctx := context.Background()
	userID := uuid.New().String()
	itemID := uuid.New().String()

	mockRepo := new(MockLinkedItemRepository)
	mockSyncService := new(MockTransactionSyncService)

	repoErr := errors.New("database connection error")
	mockRepo.On("GetByIDAndUser", ctx, itemID, userID).Return(nil, repoErr)

	worker := NewPlaidSyncWorker(PlaidSyncWorkerDeps{
		SyncService:    mockSyncService,
		LinkedItemRepo: mockRepo,
		Logger:         newTestLogger(),
	})

	job := newTestRiverJob(PlaidSyncArgs{
		UserID: userID,
		ItemID: itemID,
	})

	err := worker.Work(ctx, job)

	// Should return error for retry
	// Note: With the new locking logic, this now returns "failed to acquire lock on linked item"
	// when DB is nil (fallback to GetByIDAndUser)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to acquire lock on linked item")
	mockRepo.AssertExpectations(t)
}

func TestPlaidSyncWorker_Work_SyncTemporaryError(t *testing.T) {
	ctx := context.Background()
	userID := uuid.New().String()
	itemID := uuid.New().String()

	linkedItem := &domain.LinkedItem{
		ID:     itemID,
		UserID: userID,
	}

	mockRepo := new(MockLinkedItemRepository)
	mockSyncService := new(MockTransactionSyncService)

	// Network error is temporary - should retry
	syncErr := errors.New("network timeout")
	mockRepo.On("GetByIDAndUser", ctx, itemID, userID).Return(linkedItem, nil)
	mockSyncService.On("SyncTransactions", ctx, linkedItem).Return(nil, syncErr)

	worker := NewPlaidSyncWorker(PlaidSyncWorkerDeps{
		SyncService:    mockSyncService,
		LinkedItemRepo: mockRepo,
		Logger:         newTestLogger(),
	})

	job := newTestRiverJob(PlaidSyncArgs{
		UserID: userID,
		ItemID: itemID,
	})

	err := worker.Work(ctx, job)

	// Should return error for retry
	require.Error(t, err)
	assert.Contains(t, err.Error(), "sync failed")
	mockRepo.AssertExpectations(t)
	mockSyncService.AssertExpectations(t)
}

func TestPlaidSyncWorker_Work_PermanentPlaidError(t *testing.T) {
	ctx := context.Background()
	userID := uuid.New().String()
	itemID := uuid.New().String()

	linkedItem := &domain.LinkedItem{
		ID:     itemID,
		UserID: userID,
	}

	mockRepo := new(MockLinkedItemRepository)
	mockSyncService := new(MockTransactionSyncService)

	// INVALID_ACCESS_TOKEN is a permanent error - should NOT retry
	plaidErr := &domain.PlaidSyncError{
		OriginalError: errors.New("access token is invalid"),
		ErrorType:     domain.PlaidErrorPermanent,
		ErrorCode:     "INVALID_ACCESS_TOKEN",
	}
	mockRepo.On("GetByIDAndUser", ctx, itemID, userID).Return(linkedItem, nil)
	mockSyncService.On("SyncTransactions", ctx, linkedItem).Return(nil, plaidErr)

	worker := NewPlaidSyncWorker(PlaidSyncWorkerDeps{
		SyncService:    mockSyncService,
		LinkedItemRepo: mockRepo,
		Logger:         newTestLogger(),
	})

	job := newTestRiverJob(PlaidSyncArgs{
		UserID: userID,
		ItemID: itemID,
	})

	err := worker.Work(ctx, job)

	// Should NOT return error (don't retry permanent errors)
	require.NoError(t, err)
	mockRepo.AssertExpectations(t)
	mockSyncService.AssertExpectations(t)
}

func TestPlaidSyncWorker_Work_WithWebhookID(t *testing.T) {
	ctx := context.Background()
	userID := uuid.New().String()
	itemID := uuid.New().String()
	webhookID := uuid.New().String()

	linkedItem := &domain.LinkedItem{
		ID:     itemID,
		UserID: userID,
	}

	syncResult := &domain.TransactionSyncResult{
		Added:     []plaid.Transaction{{TransactionId: "tx1"}},
		NewCursor: "cursor-123",
	}

	mockRepo := new(MockLinkedItemRepository)
	mockSyncService := new(MockTransactionSyncService)

	mockRepo.On("GetByIDAndUser", ctx, itemID, userID).Return(linkedItem, nil)
	mockSyncService.On("SyncTransactions", ctx, linkedItem).Return(syncResult, nil)

	worker := NewPlaidSyncWorker(PlaidSyncWorkerDeps{
		SyncService:    mockSyncService,
		LinkedItemRepo: mockRepo,
		Logger:         newTestLogger(),
	})

	job := newTestRiverJob(PlaidSyncArgs{
		UserID:    userID,
		ItemID:    itemID,
		WebhookID: webhookID,
	})

	err := worker.Work(ctx, job)

	require.NoError(t, err)
	mockRepo.AssertExpectations(t)
	mockSyncService.AssertExpectations(t)
}

func TestPlaidSyncUniqueOpts(t *testing.T) {
	opts := PlaidSyncUniqueOpts()

	assert.True(t, opts.ByArgs)
	assert.Equal(t, 5*time.Minute, opts.ByPeriod)
	assert.Contains(t, opts.ByState, queue.JobStateAvailable)
	assert.Contains(t, opts.ByState, queue.JobStateRunning)
	assert.Contains(t, opts.ByState, queue.JobStateRetryable)
}

func TestNewPlaidSyncWorker_DefaultLogger(t *testing.T) {
	mockRepo := new(MockLinkedItemRepository)
	mockSyncService := new(MockTransactionSyncService)

	worker := NewPlaidSyncWorker(PlaidSyncWorkerDeps{
		SyncService:    mockSyncService,
		LinkedItemRepo: mockRepo,
		// Logger is nil, should use default
	})

	assert.NotNil(t, worker.logger)
}

// TestPlaidSyncWorker_SetJobQueue tests the SetJobQueue method (Issue #666)
func TestPlaidSyncWorker_SetJobQueue(t *testing.T) {
	mockRepo := new(MockLinkedItemRepository)
	mockSyncService := new(MockTransactionSyncService)
	mockQueue := new(MockJobQueue)

	// Create worker without job queue
	worker := NewPlaidSyncWorker(PlaidSyncWorkerDeps{
		SyncService:    mockSyncService,
		LinkedItemRepo: mockRepo,
		JobQueue:       nil, // Initially nil
		Logger:         newTestLogger(),
	})

	// Job queue should be nil
	assert.Nil(t, worker.jobQueue)

	// Set job queue
	worker.SetJobQueue(mockQueue)

	// Job queue should now be set
	assert.Equal(t, mockQueue, worker.jobQueue)
}

// #1626 Phase 3, Defect 3: the periodic balance-capture dispatcher's global item enumeration.
// Nothing in this test file's subject reaches it; the method exists so the mock still satisfies
// repository.LinkedItemRepository.
func (m *MockLinkedItemRepository) GetItemsForBalanceCapturePage(ctx context.Context, after *repository.BalanceCaptureSweepCursor, limit int) ([]domain.BalanceCaptureTarget, error) {
	args := m.Called(ctx, after, limit)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]domain.BalanceCaptureTarget), args.Error(1)
}
