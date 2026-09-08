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
	"github.com/maxnussbaum/BoostedBudget/internal/testsupport/mockassert"
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

func (m *MockWebhookRepository) MarkFailed(ctx context.Context, id string, errorMsg string) error {
	args := m.Called(ctx, id, errorMsg)
	return args.Error(0)
}

// MockWebhookTransactionSyncService mocks TransactionSyncService for webhook tests
type MockWebhookTransactionSyncService struct {
	mock.Mock
}

func (m *MockWebhookTransactionSyncService) ProcessWebhook(ctx context.Context, webhook *domain.WebhookEvent) error {
	args := m.Called(ctx, webhook)
	return args.Error(0)
}

// MockWebhookInvestmentSyncService mocks InvestmentSyncService for webhook tests.
// Issue #659: Plaid Investments
type MockWebhookInvestmentSyncService struct {
	mock.Mock
}

func (m *MockWebhookInvestmentSyncService) ProcessWebhook(ctx context.Context, webhook *domain.WebhookEvent) error {
	args := m.Called(ctx, webhook)
	return args.Error(0)
}

// MockWebhookLiabilitySyncService mocks LiabilitySyncService for webhook tests.
// Issue #910: Plaid Liabilities
type MockWebhookLiabilitySyncService struct {
	mock.Mock
}

func (m *MockWebhookLiabilitySyncService) ProcessWebhook(ctx context.Context, webhook *domain.WebhookEvent) error {
	args := m.Called(ctx, webhook)
	return args.Error(0)
}

// MockItemErrorService mocks ItemErrorService for webhook tests
type MockItemErrorService struct {
	mock.Mock
}

func (m *MockItemErrorService) ProcessWebhook(ctx context.Context, itemID, errorType, errorCode, errorMessage string, receivedAt time.Time) error {
	args := m.Called(ctx, itemID, errorType, errorCode, errorMessage, receivedAt)
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

func TestWebhookProcessingArgs_Kind(t *testing.T) {
	args := WebhookProcessingArgs{}
	assert.Equal(t, "webhook_processing", args.Kind())
}

func TestWebhookProcessingWorker_Work_TransactionsWebhook_Success(t *testing.T) {
	ctx := context.Background()
	webhookID := uuid.New().String()
	itemID := "plaid-item-123"

	webhook := &domain.WebhookEvent{
		ID:             webhookID,
		WebhookType:    "TRANSACTIONS",
		WebhookCode:    stringPtr("DEFAULT_UPDATE"),
		ItemID:         &itemID,
		SignatureValid: true,
		Processed:      false,
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

	// Issue #1623: a DISTINCTIVE, non-zero receipt instant. webhook_events.created_at is
	// Postgres-DEFAULTed on insert, and it is the only ordering signal a LOGIN_REPAIRED webhook
	// has (its payload carries none), so this worker must forward it verbatim. A zero value here
	// would let a worker that substituted time.Now() — or dropped the argument — pass.
	receivedAt := time.Date(2026, 9, 7, 11, 30, 0, 0, time.UTC)
	webhook := &domain.WebhookEvent{
		ID:             webhookID,
		WebhookType:    "ITEM",
		WebhookCode:    stringPtr("ERROR"),
		ItemID:         &itemID,
		ErrorCode:      stringPtr("ITEM_LOGIN_REQUIRED"),
		SignatureValid: true,
		Processed:      false,
		CreatedAt:      receivedAt,
	}

	mockRepo := new(MockWebhookRepository)
	mockSyncSvc := new(MockWebhookTransactionSyncService)
	mockItemErrorSvc := new(MockItemErrorService)

	mockRepo.On("GetByID", ctx, webhookID).Return(webhook, nil)
	mockItemErrorSvc.On("ProcessWebhook", ctx, itemID, "ERROR", "ITEM_LOGIN_REQUIRED", "", receivedAt).Return(nil)

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

// TestWebhookProcessingWorker_Work_ItemWebhook_RecoversErrorMessageFromPayload is a regression
// test for Issue #1609 P3-T3: domain.WebhookEvent has ErrorCode but no ErrorMessage column (the
// webhook_events table never stored Plaid's error_message), so processItemWebhook used to always
// pass "" — producing a dangling "with Chase: ." notification sentence. Plaid's ITEM error webhook
// payload always nests error_message inside the "error" object
// (https://plaid.com/docs/api/webhooks/ - ITEM/ERROR), so processItemWebhook recovers it by
// parsing the already-stored, already-verified raw Payload for this ITEM-only code path.
func TestWebhookProcessingWorker_Work_ItemWebhook_RecoversErrorMessageFromPayload(t *testing.T) {
	ctx := context.Background()
	webhookID := uuid.New().String()
	itemID := "plaid-item-123"

	webhook := &domain.WebhookEvent{
		ID:             webhookID,
		WebhookType:    "ITEM",
		WebhookCode:    stringPtr("ERROR"),
		ItemID:         &itemID,
		ErrorCode:      stringPtr("ITEM_LOGIN_REQUIRED"),
		Payload:        []byte(`{"webhook_type":"ITEM","webhook_code":"ERROR","item_id":"plaid-item-123","error":{"error_type":"ITEM_ERROR","error_code":"ITEM_LOGIN_REQUIRED","error_message":"the login details of this item have changed","display_message":null}}`),
		SignatureValid: true,
		Processed:      false,
	}

	mockRepo := new(MockWebhookRepository)
	mockSyncSvc := new(MockWebhookTransactionSyncService)
	mockItemErrorSvc := new(MockItemErrorService)

	mockRepo.On("GetByID", ctx, webhookID).Return(webhook, nil)
	mockItemErrorSvc.On("ProcessWebhook", ctx, itemID, "ERROR", "ITEM_LOGIN_REQUIRED",
		"the login details of this item have changed", webhook.CreatedAt).Return(nil)

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
	mockassert.NoCallTo(t, &mockSyncSvc.Mock, "ProcessWebhook")
}

// TestWebhookProcessingWorker_Work_ItemWebhook_MalformedPayload_FallsBackToEmptyMessage asserts
// that an unparseable (or absent) Payload degrades to errorMessage="" rather than failing webhook
// processing outright — recovering the message is a best-effort enrichment, not a requirement.
func TestWebhookProcessingWorker_Work_ItemWebhook_MalformedPayload_FallsBackToEmptyMessage(t *testing.T) {
	ctx := context.Background()
	webhookID := uuid.New().String()
	itemID := "plaid-item-123"

	webhook := &domain.WebhookEvent{
		ID:             webhookID,
		WebhookType:    "ITEM",
		WebhookCode:    stringPtr("ERROR"),
		ItemID:         &itemID,
		ErrorCode:      stringPtr("ITEM_LOGIN_REQUIRED"),
		Payload:        []byte(`not-json`),
		SignatureValid: true,
		Processed:      false,
	}

	mockRepo := new(MockWebhookRepository)
	mockSyncSvc := new(MockWebhookTransactionSyncService)
	mockItemErrorSvc := new(MockItemErrorService)

	mockRepo.On("GetByID", ctx, webhookID).Return(webhook, nil)
	mockItemErrorSvc.On("ProcessWebhook", ctx, itemID, "ERROR", "ITEM_LOGIN_REQUIRED", "", webhook.CreatedAt).Return(nil)

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
	mockassert.NoCallTo(t, &mockSyncSvc.Mock, "ProcessWebhook")
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

// TestWebhookProcessingWorker_Work_InvalidSignature_RefusesProcessing verifies Issue #1514's
// defense-in-depth guard: a webhook row with signature_valid=false must never be dispatched to
// a downstream sync service, must be cancelled via river.JobCancel (a truthful "cancelled",
// not a nil "successfully completed") never retried, and must be recorded via MarkFailed.
func TestWebhookProcessingWorker_Work_InvalidSignature_RefusesProcessing(t *testing.T) {
	ctx := context.Background()
	webhookID := uuid.New().String()
	itemID := "plaid-item-123"

	webhook := &domain.WebhookEvent{
		ID:             webhookID,
		WebhookType:    "TRANSACTIONS",
		WebhookCode:    stringPtr("DEFAULT_UPDATE"),
		ItemID:         &itemID,
		SignatureValid: false,
		Processed:      false,
	}

	mockRepo := new(MockWebhookRepository)
	mockSyncSvc := new(MockWebhookTransactionSyncService)
	mockItemErrorSvc := new(MockItemErrorService)

	mockRepo.On("GetByID", ctx, webhookID).Return(webhook, nil)
	mockRepo.On("MarkFailed", ctx, webhookID, mock.AnythingOfType("string")).Return(nil)

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

	// Must actually cancel the River job (river.JobCancel), not merely return nil.
	// Returning nil is *successful completion* in River v0.43.0 (see JobCancel's doc
	// comment in error.go: "wraps err and can be returned ... to cancel the job at
	// the end of execution"), which would misreport a refused forged webhook as a
	// completed job and increment jobs_completed_total{status="success"}.
	require.Error(t, err, "invalid-signature guard must cancel the job via river.JobCancel, not complete it with nil")
	var cancelErr *river.JobCancelError
	require.ErrorAs(t, err, &cancelErr, "error must be a *river.JobCancelError so River records the job as cancelled")
	mockRepo.AssertExpectations(t)
	mockSyncSvc.AssertNotCalled(t, "ProcessWebhook")
	mockItemErrorSvc.AssertNotCalled(t, "ProcessWebhook")
}

// TestWebhookProcessingWorker_Work_InvalidSignature_MarkFailedError verifies that a transient
// failure to persist the MarkFailed record (e.g. a DB blip) surfaces as a retryable error rather
// than being swallowed into a silent, successful-looking nil return. The security property (the
// forged row is never dispatched) must still hold - re-fetching on retry still sees
// SignatureValid=false and hits this same guard again.
func TestWebhookProcessingWorker_Work_InvalidSignature_MarkFailedError(t *testing.T) {
	ctx := context.Background()
	webhookID := uuid.New().String()
	itemID := "plaid-item-123"

	webhook := &domain.WebhookEvent{
		ID:             webhookID,
		WebhookType:    "TRANSACTIONS",
		WebhookCode:    stringPtr("DEFAULT_UPDATE"),
		ItemID:         &itemID,
		SignatureValid: false,
		Processed:      false,
	}

	mockRepo := new(MockWebhookRepository)
	mockSyncSvc := new(MockWebhookTransactionSyncService)
	mockItemErrorSvc := new(MockItemErrorService)

	mockRepo.On("GetByID", ctx, webhookID).Return(webhook, nil)
	mockRepo.On("MarkFailed", ctx, webhookID, mock.AnythingOfType("string")).Return(errors.New("db unavailable"))

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

	// Must surface as a plain retryable error (NOT nil, and NOT a JobCancelError) so a
	// transient MarkFailed failure gets another attempt at recording the refusal, instead
	// of the job silently completing "successfully" with the refusal never recorded anywhere.
	require.Error(t, err, "a MarkFailed failure must not be swallowed into a nil return")
	var cancelErr *river.JobCancelError
	require.NotErrorAs(t, err, &cancelErr, "a MarkFailed failure should be retryable, not a cancellation")
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
		ID:             webhookID,
		WebhookType:    "TRANSACTIONS",
		ItemID:         &itemID,
		SignatureValid: true,
		Processed:      false,
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
		ID:             webhookID,
		WebhookType:    "ITEM",
		WebhookCode:    stringPtr("ERROR"),
		ItemID:         nil, // Missing item_id
		SignatureValid: true,
		Processed:      false,
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
		ID:             webhookID,
		WebhookType:    "UNKNOWN_TYPE",
		ItemID:         &itemID,
		SignatureValid: true,
		Processed:      false,
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

// TestWebhookProcessingWorker_Work_LiabilitiesWebhook_RoutedToLiabilitySyncService verifies
// LIABILITIES webhook is routed to LiabilitySyncService.ProcessWebhook (Issue #910).
func TestWebhookProcessingWorker_Work_LiabilitiesWebhook_RoutedToLiabilitySyncService(t *testing.T) {
	ctx := context.Background()
	webhookID := uuid.New().String()
	itemID := "plaid-item-liabilities-123"

	webhook := &domain.WebhookEvent{
		ID:             webhookID,
		WebhookType:    "LIABILITIES",
		WebhookCode:    stringPtr("DEFAULT_UPDATE"),
		ItemID:         &itemID,
		SignatureValid: true,
		Processed:      false,
	}

	mockRepo := new(MockWebhookRepository)
	mockSyncSvc := new(MockWebhookTransactionSyncService)
	mockLiabilitySvc := new(MockWebhookLiabilitySyncService)
	mockItemErrorSvc := new(MockItemErrorService)

	mockRepo.On("GetByID", ctx, webhookID).Return(webhook, nil)
	mockLiabilitySvc.On("ProcessWebhook", ctx, webhook).Return(nil)

	worker := NewWebhookProcessingWorker(WebhookProcessingWorkerDeps{
		WebhookRepo:        mockRepo,
		TransactionSyncSvc: mockSyncSvc,
		LiabilitySyncSvc:   mockLiabilitySvc,
		ItemErrorSvc:       mockItemErrorSvc,
		Logger:             newTestWebhookLogger(),
	})

	job := newTestWebhookProcessingJob(WebhookProcessingArgs{
		WebhookID:   webhookID,
		WebhookType: "LIABILITIES",
		ItemID:      itemID,
	})

	err := worker.Work(ctx, job)

	require.NoError(t, err)
	mockRepo.AssertExpectations(t)
	mockLiabilitySvc.AssertExpectations(t)
	mockSyncSvc.AssertNotCalled(t, "ProcessWebhook")
	mockItemErrorSvc.AssertNotCalled(t, "ProcessWebhook")
}

// TestWebhookProcessingWorker_Work_HoldingsWebhook_RoutedToInvestmentSyncService verifies
// HOLDINGS webhook is routed to InvestmentSyncService.ProcessWebhook (Issue #659).
func TestWebhookProcessingWorker_Work_HoldingsWebhook_RoutedToInvestmentSyncService(t *testing.T) {
	ctx := context.Background()
	webhookID := uuid.New().String()
	itemID := "plaid-item-holdings-456"

	webhook := &domain.WebhookEvent{
		ID:             webhookID,
		WebhookType:    "HOLDINGS",
		WebhookCode:    stringPtr("DEFAULT_UPDATE"),
		ItemID:         &itemID,
		SignatureValid: true,
		Processed:      false,
	}

	mockRepo := new(MockWebhookRepository)
	mockSyncSvc := new(MockWebhookTransactionSyncService)
	mockInvestmentSvc := new(MockWebhookInvestmentSyncService)
	mockItemErrorSvc := new(MockItemErrorService)

	mockRepo.On("GetByID", ctx, webhookID).Return(webhook, nil)
	mockInvestmentSvc.On("ProcessWebhook", ctx, webhook).Return(nil)

	worker := NewWebhookProcessingWorker(WebhookProcessingWorkerDeps{
		WebhookRepo:        mockRepo,
		TransactionSyncSvc: mockSyncSvc,
		InvestmentSyncSvc:  mockInvestmentSvc,
		ItemErrorSvc:       mockItemErrorSvc,
		Logger:             newTestWebhookLogger(),
	})

	job := newTestWebhookProcessingJob(WebhookProcessingArgs{
		WebhookID:   webhookID,
		WebhookType: "HOLDINGS",
		ItemID:      itemID,
	})

	err := worker.Work(ctx, job)

	require.NoError(t, err)
	mockRepo.AssertExpectations(t)
	mockInvestmentSvc.AssertExpectations(t)
	mockSyncSvc.AssertNotCalled(t, "ProcessWebhook")
	mockItemErrorSvc.AssertNotCalled(t, "ProcessWebhook")
}

// TestWebhookProcessingWorker_Work_InvestmentTransactionsWebhook_Success verifies
// INVESTMENTS_TRANSACTIONS webhook is routed to InvestmentSyncService.ProcessWebhook (Issue #659).
func TestWebhookProcessingWorker_Work_InvestmentTransactionsWebhook_Success(t *testing.T) {
	ctx := context.Background()
	webhookID := uuid.New().String()
	itemID := "plaid-item-inv-txn-789"

	webhook := &domain.WebhookEvent{
		ID:             webhookID,
		WebhookType:    "INVESTMENTS_TRANSACTIONS",
		WebhookCode:    stringPtr("DEFAULT_UPDATE"),
		ItemID:         &itemID,
		SignatureValid: true,
		Processed:      false,
	}

	mockRepo := new(MockWebhookRepository)
	mockSyncSvc := new(MockWebhookTransactionSyncService)
	mockInvestmentSvc := new(MockWebhookInvestmentSyncService)
	mockItemErrorSvc := new(MockItemErrorService)

	mockRepo.On("GetByID", ctx, webhookID).Return(webhook, nil)
	mockInvestmentSvc.On("ProcessWebhook", ctx, webhook).Return(nil)

	worker := NewWebhookProcessingWorker(WebhookProcessingWorkerDeps{
		WebhookRepo:        mockRepo,
		TransactionSyncSvc: mockSyncSvc,
		InvestmentSyncSvc:  mockInvestmentSvc,
		ItemErrorSvc:       mockItemErrorSvc,
		Logger:             newTestWebhookLogger(),
	})

	job := newTestWebhookProcessingJob(WebhookProcessingArgs{
		WebhookID:   webhookID,
		WebhookType: "INVESTMENTS_TRANSACTIONS",
		ItemID:      itemID,
	})

	err := worker.Work(ctx, job)

	require.NoError(t, err)
	mockRepo.AssertExpectations(t)
	mockInvestmentSvc.AssertExpectations(t)
	mockSyncSvc.AssertNotCalled(t, "ProcessWebhook")
	mockItemErrorSvc.AssertNotCalled(t, "ProcessWebhook")
}

// TestWebhookProcessingWorker_Work_InvestmentTransactionsWebhook_Error verifies
// processing errors from InvestmentSyncService are propagated for retry.
func TestWebhookProcessingWorker_Work_InvestmentTransactionsWebhook_Error(t *testing.T) {
	ctx := context.Background()
	webhookID := uuid.New().String()
	itemID := "plaid-item-inv-txn-789"

	webhook := &domain.WebhookEvent{
		ID:             webhookID,
		WebhookType:    "INVESTMENTS_TRANSACTIONS",
		WebhookCode:    stringPtr("DEFAULT_UPDATE"),
		ItemID:         &itemID,
		SignatureValid: true,
		Processed:      false,
	}

	mockRepo := new(MockWebhookRepository)
	mockSyncSvc := new(MockWebhookTransactionSyncService)
	mockInvestmentSvc := new(MockWebhookInvestmentSyncService)
	mockItemErrorSvc := new(MockItemErrorService)

	mockRepo.On("GetByID", ctx, webhookID).Return(webhook, nil)
	mockInvestmentSvc.On("ProcessWebhook", ctx, webhook).Return(errors.New("investment sync failed"))

	worker := NewWebhookProcessingWorker(WebhookProcessingWorkerDeps{
		WebhookRepo:        mockRepo,
		TransactionSyncSvc: mockSyncSvc,
		InvestmentSyncSvc:  mockInvestmentSvc,
		ItemErrorSvc:       mockItemErrorSvc,
		Logger:             newTestWebhookLogger(),
	})

	job := newTestWebhookProcessingJob(WebhookProcessingArgs{
		WebhookID:   webhookID,
		WebhookType: "INVESTMENTS_TRANSACTIONS",
		ItemID:      itemID,
	})

	err := worker.Work(ctx, job)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "webhook processing failed")
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
