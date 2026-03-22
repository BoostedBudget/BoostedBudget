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

// MockWebhookRepository mocks the WebhookRepository interface for tests
type MockWebhookRepository struct {
	mock.Mock
}

func (m *MockWebhookRepository) GetByID(ctx context.Context, id string) (*domain.WebhookEvent, error) {
	args := m.Called(ctx, id)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*domain.WebhookEvent), args.Error(1)
}

// MockWebhookTransactionSyncService mocks TransactionSyncService for webhook tests
type MockWebhookTransactionSyncService struct {
	mock.Mock
}

func (m *MockWebhookTransactionSyncService) ProcessWebhook(ctx context.Context, webhook *domain.WebhookEvent) error {
	args := m.Called(ctx, webhook)
	return args.Error(0)
}

// MockItemErrorService mocks ItemErrorService for webhook tests
type MockItemErrorService struct {
	mock.Mock
}

func (m *MockItemErrorService) ProcessWebhook(ctx context.Context, itemID, errorType, errorCode, errorMessage string) error {
	args := m.Called(ctx, itemID, errorType, errorCode, errorMessage)
	return args.Error(0)
}

// Test helpers
func newTestWebhookLogger() *slog.Logger {
	return slog.Default()
}

func newTestWebhookProcessingJob(args WebhookProcessingArgs) *river.Job[WebhookProcessingArgs] {
	return &river.Job[WebhookProcessingArgs]{
		JobRow: &rivertype.JobRow{
			ID:      1,
			Attempt: 1,
		},
		Args: args,
	}
}

func stringPtr(s string) *string {
	return &s
}

func TestWebhookProcessingArgs_Kind(t *testing.T) {
	args := WebhookProcessingArgs{}
	assert.Equal(t, "webhook_processing", args.Kind())
}

func TestWebhookProcessingWorker_Work_TransactionsWebhook_Success(t *testing.T) {
	ctx := context.Background()
	webhookID := uuid.New().String()
	itemID := "plaid-item-123"

	webhook := &domain.WebhookEvent{
		ID:          webhookID,
		WebhookType: "TRANSACTIONS",
		WebhookCode: stringPtr("DEFAULT_UPDATE"),
		ItemID:      &itemID,
		Processed:   false,
	}

	mockRepo := new(MockWebhookRepository)
	mockSyncSvc := new(MockWebhookTransactionSyncService)
	mockItemErrorSvc := new(MockItemErrorService)

	mockRepo.On("GetByID", ctx, webhookID).Return(webhook, nil)
	mockSyncSvc.On("ProcessWebhook", ctx, webhook).Return(nil)

	worker := NewWebhookProcessingWorker(WebhookProcessingWorkerDeps{
		WebhookRepo:        mockRepo,
		TransactionSyncSvc: mockSyncSvc,
		ItemErrorSvc:       mockItemErrorSvc,
		Logger:             newTestWebhookLogger(),
	})

	job := newTestWebhookProcessingJob(WebhookProcessingArgs{
		WebhookID:   webhookID,
		WebhookType: "TRANSACTIONS",
		ItemID:      itemID,
	})

	err := worker.Work(ctx, job)

	require.NoError(t, err)
	mockRepo.AssertExpectations(t)
	mockSyncSvc.AssertExpectations(t)
	mockItemErrorSvc.AssertNotCalled(t, "ProcessWebhook")
}

func TestWebhookProcessingWorker_Work_ItemWebhook_Success(t *testing.T) {
	ctx := context.Background()
	webhookID := uuid.New().String()
	itemID := "plaid-item-123"

	webhook := &domain.WebhookEvent{
		ID:          webhookID,
		WebhookType: "ITEM",
		WebhookCode: stringPtr("ERROR"),
		ItemID:      &itemID,
		ErrorCode:   stringPtr("ITEM_LOGIN_REQUIRED"),
		Processed:   false,
	}

	mockRepo := new(MockWebhookRepository)
	mockSyncSvc := new(MockWebhookTransactionSyncService)
	mockItemErrorSvc := new(MockItemErrorService)

	mockRepo.On("GetByID", ctx, webhookID).Return(webhook, nil)
	mockItemErrorSvc.On("ProcessWebhook", ctx, itemID, "ERROR", "ITEM_LOGIN_REQUIRED", "").Return(nil)

	worker := NewWebhookProcessingWorker(WebhookProcessingWorkerDeps{
		WebhookRepo:        mockRepo,
		TransactionSyncSvc: mockSyncSvc,
		ItemErrorSvc:       mockItemErrorSvc,
		Logger:             newTestWebhookLogger(),
	})

	job := newTestWebhookProcessingJob(WebhookProcessingArgs{
		WebhookID:   webhookID,
		WebhookType: "ITEM",
		ItemID:      itemID,
	})

	err := worker.Work(ctx, job)

	require.NoError(t, err)
	mockRepo.AssertExpectations(t)
	mockItemErrorSvc.AssertExpectations(t)
	mockSyncSvc.AssertNotCalled(t, "ProcessWebhook")
}

func TestWebhookProcessingWorker_Work_WebhookNotFound(t *testing.T) {
	ctx := context.Background()
	webhookID := uuid.New().String()

	mockRepo := new(MockWebhookRepository)
	mockSyncSvc := new(MockWebhookTransactionSyncService)
	mockItemErrorSvc := new(MockItemErrorService)

	mockRepo.On("GetByID", ctx, webhookID).Return(nil, repository.ErrWebhookNotFound)

	worker := NewWebhookProcessingWorker(WebhookProcessingWorkerDeps{
		WebhookRepo:        mockRepo,
		TransactionSyncSvc: mockSyncSvc,
		ItemErrorSvc:       mockItemErrorSvc,
		Logger:             newTestWebhookLogger(),
	})

	job := newTestWebhookProcessingJob(WebhookProcessingArgs{
		WebhookID:   webhookID,
		WebhookType: "TRANSACTIONS",
	})

	err := worker.Work(ctx, job)

	// Should NOT return error (don't retry for missing webhooks)
	require.NoError(t, err)
	mockRepo.AssertExpectations(t)
	mockSyncSvc.AssertNotCalled(t, "ProcessWebhook")
	mockItemErrorSvc.AssertNotCalled(t, "ProcessWebhook")
}

func TestWebhookProcessingWorker_Work_AlreadyProcessed(t *testing.T) {
	ctx := context.Background()
	webhookID := uuid.New().String()
	itemID := "plaid-item-123"

	webhook := &domain.WebhookEvent{
		ID:          webhookID,
		WebhookType: "TRANSACTIONS",
		ItemID:      &itemID,
		Processed:   true, // Already processed
	}

	mockRepo := new(MockWebhookRepository)
	mockSyncSvc := new(MockWebhookTransactionSyncService)
	mockItemErrorSvc := new(MockItemErrorService)

	mockRepo.On("GetByID", ctx, webhookID).Return(webhook, nil)

	worker := NewWebhookProcessingWorker(WebhookProcessingWorkerDeps{
		WebhookRepo:        mockRepo,
		TransactionSyncSvc: mockSyncSvc,
		ItemErrorSvc:       mockItemErrorSvc,
		Logger:             newTestWebhookLogger(),
	})

	job := newTestWebhookProcessingJob(WebhookProcessingArgs{
		WebhookID:   webhookID,
		WebhookType: "TRANSACTIONS",
	})

	err := worker.Work(ctx, job)

	// Should succeed without calling processor (already done)
	require.NoError(t, err)
	mockRepo.AssertExpectations(t)
	mockSyncSvc.AssertNotCalled(t, "ProcessWebhook")
	mockItemErrorSvc.AssertNotCalled(t, "ProcessWebhook")
}

func TestWebhookProcessingWorker_Work_RepositoryError(t *testing.T) {
	ctx := context.Background()
	webhookID := uuid.New().String()

	mockRepo := new(MockWebhookRepository)
	mockSyncSvc := new(MockWebhookTransactionSyncService)
	mockItemErrorSvc := new(MockItemErrorService)

	dbErr := errors.New("database connection error")
	mockRepo.On("GetByID", ctx, webhookID).Return(nil, dbErr)

	worker := NewWebhookProcessingWorker(WebhookProcessingWorkerDeps{
		WebhookRepo:        mockRepo,
		TransactionSyncSvc: mockSyncSvc,
		ItemErrorSvc:       mockItemErrorSvc,
		Logger:             newTestWebhookLogger(),
	})

	job := newTestWebhookProcessingJob(WebhookProcessingArgs{
		WebhookID:   webhookID,
		WebhookType: "TRANSACTIONS",
	})

	err := worker.Work(ctx, job)

	// Should return error for retry
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to fetch webhook")
	mockRepo.AssertExpectations(t)
}

func TestWebhookProcessingWorker_Work_TransactionProcessingError(t *testing.T) {
	ctx := context.Background()
	webhookID := uuid.New().String()
	itemID := "plaid-item-123"

	webhook := &domain.WebhookEvent{
		ID:          webhookID,
		WebhookType: "TRANSACTIONS",
		ItemID:      &itemID,
		Processed:   false,
	}

	mockRepo := new(MockWebhookRepository)
	mockSyncSvc := new(MockWebhookTransactionSyncService)
	mockItemErrorSvc := new(MockItemErrorService)

	processingErr := errors.New("plaid sync failed")
	mockRepo.On("GetByID", ctx, webhookID).Return(webhook, nil)
	mockSyncSvc.On("ProcessWebhook", ctx, webhook).Return(processingErr)

	worker := NewWebhookProcessingWorker(WebhookProcessingWorkerDeps{
		WebhookRepo:        mockRepo,
		TransactionSyncSvc: mockSyncSvc,
		ItemErrorSvc:       mockItemErrorSvc,
		Logger:             newTestWebhookLogger(),
	})

	job := newTestWebhookProcessingJob(WebhookProcessingArgs{
		WebhookID:   webhookID,
		WebhookType: "TRANSACTIONS",
		ItemID:      itemID,
	})

	err := worker.Work(ctx, job)

	// Should return error for retry
	require.Error(t, err)
	assert.Contains(t, err.Error(), "webhook processing failed")
	mockRepo.AssertExpectations(t)
	mockSyncSvc.AssertExpectations(t)
}

func TestWebhookProcessingWorker_Work_ItemWebhook_MissingItemID(t *testing.T) {
	ctx := context.Background()
	webhookID := uuid.New().String()

	webhook := &domain.WebhookEvent{
		ID:          webhookID,
		WebhookType: "ITEM",
		WebhookCode: stringPtr("ERROR"),
		ItemID:      nil, // Missing item_id
		Processed:   false,
	}

	mockRepo := new(MockWebhookRepository)
	mockSyncSvc := new(MockWebhookTransactionSyncService)
	mockItemErrorSvc := new(MockItemErrorService)

	mockRepo.On("GetByID", ctx, webhookID).Return(webhook, nil)

	worker := NewWebhookProcessingWorker(WebhookProcessingWorkerDeps{
		WebhookRepo:        mockRepo,
		TransactionSyncSvc: mockSyncSvc,
		ItemErrorSvc:       mockItemErrorSvc,
		Logger:             newTestWebhookLogger(),
	})

	job := newTestWebhookProcessingJob(WebhookProcessingArgs{
		WebhookID:   webhookID,
		WebhookType: "ITEM",
	})

	err := worker.Work(ctx, job)

	// Should return error (invalid webhook)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "missing item_id")
	mockRepo.AssertExpectations(t)
	mockItemErrorSvc.AssertNotCalled(t, "ProcessWebhook")
}

func TestWebhookProcessingWorker_Work_UnknownWebhookType(t *testing.T) {
	ctx := context.Background()
	webhookID := uuid.New().String()
	itemID := "plaid-item-123"

	webhook := &domain.WebhookEvent{
		ID:          webhookID,
		WebhookType: "UNKNOWN_TYPE",
		ItemID:      &itemID,
		Processed:   false,
	}

	mockRepo := new(MockWebhookRepository)
	mockSyncSvc := new(MockWebhookTransactionSyncService)
	mockItemErrorSvc := new(MockItemErrorService)

	mockRepo.On("GetByID", ctx, webhookID).Return(webhook, nil)

	worker := NewWebhookProcessingWorker(WebhookProcessingWorkerDeps{
		WebhookRepo:        mockRepo,
		TransactionSyncSvc: mockSyncSvc,
		ItemErrorSvc:       mockItemErrorSvc,
		Logger:             newTestWebhookLogger(),
	})

	job := newTestWebhookProcessingJob(WebhookProcessingArgs{
		WebhookID:   webhookID,
		WebhookType: "UNKNOWN_TYPE",
	})

	err := worker.Work(ctx, job)

	// Should succeed without error (skip unknown types)
	require.NoError(t, err)
	mockRepo.AssertExpectations(t)
	mockSyncSvc.AssertNotCalled(t, "ProcessWebhook")
	mockItemErrorSvc.AssertNotCalled(t, "ProcessWebhook")
}

// TestWebhookProcessingWorker_Work_LiabilitiesWebhook_LoggedAndAcknowledged verifies
// LIABILITIES webhook is logged and acknowledged (Phase 2 #241, processing deferred)
func TestWebhookProcessingWorker_Work_LiabilitiesWebhook_LoggedAndAcknowledged(t *testing.T) {
	ctx := context.Background()
	webhookID := uuid.New().String()
	itemID := "plaid-item-liabilities-123"

	webhook := &domain.WebhookEvent{
		ID:          webhookID,
		WebhookType: "LIABILITIES",
		WebhookCode: stringPtr("DEFAULT_UPDATE"),
		ItemID:      &itemID,
		Processed:   false,
	}

	mockRepo := new(MockWebhookRepository)
	mockSyncSvc := new(MockWebhookTransactionSyncService)
	mockItemErrorSvc := new(MockItemErrorService)

	mockRepo.On("GetByID", ctx, webhookID).Return(webhook, nil)

	worker := NewWebhookProcessingWorker(WebhookProcessingWorkerDeps{
		WebhookRepo:        mockRepo,
		TransactionSyncSvc: mockSyncSvc,
		ItemErrorSvc:       mockItemErrorSvc,
		Logger:             newTestWebhookLogger(),
	})

	job := newTestWebhookProcessingJob(WebhookProcessingArgs{
		WebhookID:   webhookID,
		WebhookType: "LIABILITIES",
		ItemID:      itemID,
	})

	err := worker.Work(ctx, job)

	// Should succeed without error (logged, processing deferred)
	require.NoError(t, err)
	mockRepo.AssertExpectations(t)
	mockSyncSvc.AssertNotCalled(t, "ProcessWebhook")
	mockItemErrorSvc.AssertNotCalled(t, "ProcessWebhook")
}

// TestWebhookProcessingWorker_Work_HoldingsWebhook_LoggedAndAcknowledged verifies
// HOLDINGS webhook is logged and acknowledged (Phase 2 #241, deferred to #659)
func TestWebhookProcessingWorker_Work_HoldingsWebhook_LoggedAndAcknowledged(t *testing.T) {
	ctx := context.Background()
	webhookID := uuid.New().String()
	itemID := "plaid-item-holdings-456"

	webhook := &domain.WebhookEvent{
		ID:          webhookID,
		WebhookType: "HOLDINGS",
		WebhookCode: stringPtr("DEFAULT_UPDATE"),
		ItemID:      &itemID,
		Processed:   false,
	}

	mockRepo := new(MockWebhookRepository)
	mockSyncSvc := new(MockWebhookTransactionSyncService)
	mockItemErrorSvc := new(MockItemErrorService)

	mockRepo.On("GetByID", ctx, webhookID).Return(webhook, nil)

	worker := NewWebhookProcessingWorker(WebhookProcessingWorkerDeps{
		WebhookRepo:        mockRepo,
		TransactionSyncSvc: mockSyncSvc,
		ItemErrorSvc:       mockItemErrorSvc,
		Logger:             newTestWebhookLogger(),
	})

	job := newTestWebhookProcessingJob(WebhookProcessingArgs{
		WebhookID:   webhookID,
		WebhookType: "HOLDINGS",
		ItemID:      itemID,
	})

	err := worker.Work(ctx, job)

	// Should succeed without error (logged, deferred to #659)
	require.NoError(t, err)
	mockRepo.AssertExpectations(t)
	mockSyncSvc.AssertNotCalled(t, "ProcessWebhook")
	mockItemErrorSvc.AssertNotCalled(t, "ProcessWebhook")
}

func TestWebhookProcessingUniqueOpts(t *testing.T) {
	opts := WebhookProcessingUniqueOpts()

	assert.True(t, opts.ByArgs)
	assert.Equal(t, 10*time.Minute, opts.ByPeriod)
	assert.Contains(t, opts.ByState, queue.JobStateAvailable)
	assert.Contains(t, opts.ByState, queue.JobStateScheduled)
	assert.Contains(t, opts.ByState, queue.JobStateRunning)
	assert.Contains(t, opts.ByState, queue.JobStateRetryable)
	assert.Contains(t, opts.ByState, queue.JobStatePending)
}

func TestNewWebhookProcessingWorker_DefaultLogger(t *testing.T) {
	mockRepo := new(MockWebhookRepository)
	mockSyncSvc := new(MockWebhookTransactionSyncService)
	mockItemErrorSvc := new(MockItemErrorService)

	worker := NewWebhookProcessingWorker(WebhookProcessingWorkerDeps{
		WebhookRepo:        mockRepo,
		TransactionSyncSvc: mockSyncSvc,
		ItemErrorSvc:       mockItemErrorSvc,
		// Logger is nil, should use default
	})

	assert.NotNil(t, worker.logger)
}
