package jobs

import (
	"context"
	"errors"
	"log/slog"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/queue"
	"github.com/maxnussbaum/BoostedBudget/internal/repository"
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

func (m *MockLinkedItemRepository) DeleteByUser(ctx context.Context, id, userID string) error {
	args := m.Called(ctx, id, userID)
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
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to get linked item")
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
