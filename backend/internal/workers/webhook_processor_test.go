package workers

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/maxnussbaum/BoostedBudget/internal/cache"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/integration/posthog"
	"github.com/maxnussbaum/BoostedBudget/internal/repository"
	"github.com/maxnussbaum/BoostedBudget/internal/services"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

// ============================================================================
// Mock Services and Repositories for WebhookProcessor Tests
// ============================================================================

// MockTransactionSyncServiceForWorker for testing
type MockTransactionSyncServiceForWorker struct {
	mock.Mock
}

func (m *MockTransactionSyncServiceForWorker) ProcessWebhook(ctx context.Context, webhook *domain.WebhookEvent) error {
	args := m.Called(ctx, webhook)
	return args.Error(0)
}

func (m *MockTransactionSyncServiceForWorker) SyncTransactions(ctx context.Context, linkedItem *domain.LinkedItem) (*domain.TransactionSyncResult, error) {
	args := m.Called(ctx, linkedItem)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*domain.TransactionSyncResult), args.Error(1)
}

// SetTransferCorrelationService satisfies the TransactionSyncService interface (Issue #972: B4).
func (m *MockTransactionSyncServiceForWorker) SetTransferCorrelationService(_ services.TransferCorrelationService) {
}

// SetSavingsProgressServices satisfies the TransactionSyncService interface (Issue #1300: G2).
func (m *MockTransactionSyncServiceForWorker) SetSavingsProgressServices(_ services.SyncContributionCleaner, _ services.SyncGoalProgressUpdater, _ services.SyncManagedCategoryBackfiller) {
}

// SetSavingsDetectionServices satisfies the TransactionSyncService interface (Issue #1300 M18 RG1).
func (m *MockTransactionSyncServiceForWorker) SetSavingsDetectionServices(_ services.SyncContributionsBasisGoalReader, _ services.SyncProposedContributionUpserter, _ repository.TransactionRepository) {
}

// SetCacheInvalidation satisfies the TransactionSyncService interface (Issue #1531 Task 10b-2).
// This test double never calls SyncTransactions/ProcessWebhook for real, so there is nothing to
// wire — the no-op mirrors SetSavingsDetectionServices above.
func (m *MockTransactionSyncServiceForWorker) SetCacheInvalidation(_ *cache.Generations, _ services.SyncCacheHouseholdLookup) {
}

// SetAuditLogger satisfies the TransactionSyncService interface (Issue #1569 Task 5).
func (m *MockTransactionSyncServiceForWorker) SetAuditLogger(_ services.AuditLogger) {}

// MockItemErrorServiceForWorker for testing
type MockItemErrorServiceForWorker struct {
	mock.Mock
}

func (m *MockItemErrorServiceForWorker) ProcessWebhook(ctx context.Context, itemID string, errorType string, errorCode string, errorMessage string) error {
	args := m.Called(ctx, itemID, errorType, errorCode, errorMessage)
	return args.Error(0)
}

func (m *MockItemErrorServiceForWorker) HandleLoginRequired(ctx context.Context, linkedItem *domain.LinkedItem, errorCode string, errorMessage string) error {
	args := m.Called(ctx, linkedItem, errorCode, errorMessage)
	return args.Error(0)
}

func (m *MockItemErrorServiceForWorker) HandleError(ctx context.Context, linkedItem *domain.LinkedItem, errorCode string, errorMessage string) error {
	args := m.Called(ctx, linkedItem, errorCode, errorMessage)
	return args.Error(0)
}

func (m *MockItemErrorServiceForWorker) HandlePendingExpiration(ctx context.Context, linkedItem *domain.LinkedItem, errorCode string, errorMessage string) error {
	args := m.Called(ctx, linkedItem, errorCode, errorMessage)
	return args.Error(0)
}

func (m *MockItemErrorServiceForWorker) HandlePermissionRevoked(ctx context.Context, linkedItem *domain.LinkedItem, errorCode string, errorMessage string) error {
	args := m.Called(ctx, linkedItem, errorCode, errorMessage)
	return args.Error(0)
}

// MockWebhookRepoForWorker for testing
type MockWebhookRepoForWorker struct {
	mock.Mock
}

func (m *MockWebhookRepoForWorker) Create(ctx context.Context, event *domain.WebhookEvent) error {
	args := m.Called(ctx, event)
	return args.Error(0)
}

func (m *MockWebhookRepoForWorker) MarkProcessed(ctx context.Context, id string) error {
	args := m.Called(ctx, id)
	return args.Error(0)
}

func (m *MockWebhookRepoForWorker) MarkFailed(ctx context.Context, id string, errorMsg string) error {
	args := m.Called(ctx, id, errorMsg)
	return args.Error(0)
}

func (m *MockWebhookRepoForWorker) GetUnprocessed(ctx context.Context) ([]domain.WebhookEvent, error) {
	args := m.Called(ctx)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]domain.WebhookEvent), args.Error(1)
}

func (m *MockWebhookRepoForWorker) GetByItemID(ctx context.Context, itemID string, limit int) ([]domain.WebhookEvent, error) {
	args := m.Called(ctx, itemID, limit)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]domain.WebhookEvent), args.Error(1)
}

func (m *MockWebhookRepoForWorker) Update(ctx context.Context, event *domain.WebhookEvent) error {
	args := m.Called(ctx, event)
	return args.Error(0)
}

func (m *MockWebhookRepoForWorker) GetUnprocessedByType(ctx context.Context, webhookType string, limit int) ([]domain.WebhookEvent, error) {
	args := m.Called(ctx, webhookType, limit)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]domain.WebhookEvent), args.Error(1)
}

func (m *MockWebhookRepoForWorker) GetByID(ctx context.Context, id string) (*domain.WebhookEvent, error) {
	args := m.Called(ctx, id)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*domain.WebhookEvent), args.Error(1)
}

func (m *MockWebhookRepoForWorker) IncrementRetryCount(ctx context.Context, id string) error {
	args := m.Called(ctx, id)
	return args.Error(0)
}

// ============================================================================
// PHASE 5 RED: WebhookProcessor Tests
// Issue #256: Transaction Sync Service
// ============================================================================

// TestWebhookProcessor_ProcessPendingWebhooks_Success tests successful processing of webhooks
func TestWebhookProcessor_ProcessPendingWebhooks_Success(t *testing.T) {
	ctx := context.Background()
	mockSyncService := new(MockTransactionSyncServiceForWorker)
	mockItemErrorService := new(MockItemErrorServiceForWorker)
	mockWebhookRepo := new(MockWebhookRepoForWorker)

	processor := NewWebhookProcessor(mockSyncService, mockItemErrorService, mockWebhookRepo, 30*time.Second, 10, nil)

	// Create test webhooks
	itemID1 := "item-1"
	itemID2 := "item-2"
	webhooks := []domain.WebhookEvent{
		{
			ID:             "webhook-1",
			WebhookType:    "TRANSACTIONS",
			ItemID:         &itemID1,
			Payload:        json.RawMessage(`{"webhook_type":"TRANSACTIONS"}`),
			SignatureValid: true,
			Processed:      false,
			CreatedAt:      time.Now(),
			UpdatedAt:      time.Now(),
		},
		{
			ID:             "webhook-2",
			WebhookType:    "TRANSACTIONS",
			ItemID:         &itemID2,
			Payload:        json.RawMessage(`{"webhook_type":"TRANSACTIONS"}`),
			SignatureValid: true,
			Processed:      false,
			CreatedAt:      time.Now(),
			UpdatedAt:      time.Now(),
		},
	}

	// Mock: GetUnprocessedByType returns 2 webhooks
	mockWebhookRepo.On("GetUnprocessedByType", ctx, "TRANSACTIONS", 10).Return(webhooks, nil)
	// Mock: No ITEM webhooks
	mockWebhookRepo.On("GetUnprocessedByType", ctx, "ITEM", 10).Return([]domain.WebhookEvent{}, nil)

	// Mock: ProcessWebhook succeeds for both
	mockSyncService.On("ProcessWebhook", ctx, &webhooks[0]).Return(nil)
	mockSyncService.On("ProcessWebhook", ctx, &webhooks[1]).Return(nil)

	// Execute
	err := processor.ProcessPendingWebhooks(ctx)

	// Assert
	assert.NoError(t, err)
	mockWebhookRepo.AssertExpectations(t)
	mockSyncService.AssertExpectations(t)
}

// TestWebhookProcessor_ProcessPendingWebhooks_NoWebhooks tests graceful handling when no webhooks exist
func TestWebhookProcessor_ProcessPendingWebhooks_NoWebhooks(t *testing.T) {
	ctx := context.Background()
	mockSyncService := new(MockTransactionSyncServiceForWorker)
	mockItemErrorService := new(MockItemErrorServiceForWorker)
	mockWebhookRepo := new(MockWebhookRepoForWorker)

	processor := NewWebhookProcessor(mockSyncService, mockItemErrorService, mockWebhookRepo, 30*time.Second, 10, nil)

	// Mock: GetUnprocessedByType returns empty slice for both types
	mockWebhookRepo.On("GetUnprocessedByType", ctx, "TRANSACTIONS", 10).Return([]domain.WebhookEvent{}, nil)
	mockWebhookRepo.On("GetUnprocessedByType", ctx, "ITEM", 10).Return([]domain.WebhookEvent{}, nil)

	// Execute
	err := processor.ProcessPendingWebhooks(ctx)

	// Assert - should not error when no webhooks to process
	assert.NoError(t, err)
	mockWebhookRepo.AssertExpectations(t)
	// ProcessWebhook should NOT be called when there are no webhooks
	mockSyncService.AssertNotCalled(t, "ProcessWebhook")
}

// TestWebhookProcessor_ProcessPendingWebhooks_PartialFailure tests that processing continues after errors
func TestWebhookProcessor_ProcessPendingWebhooks_PartialFailure(t *testing.T) {
	ctx := context.Background()
	mockSyncService := new(MockTransactionSyncServiceForWorker)
	mockItemErrorService := new(MockItemErrorServiceForWorker)
	mockWebhookRepo := new(MockWebhookRepoForWorker)

	processor := NewWebhookProcessor(mockSyncService, mockItemErrorService, mockWebhookRepo, 30*time.Second, 10, nil)

	// Create 3 test webhooks
	itemID1 := "item-1"
	itemID2 := "item-2"
	itemID3 := "item-3"
	webhooks := []domain.WebhookEvent{
		{
			ID:             "webhook-1",
			WebhookType:    "TRANSACTIONS",
			ItemID:         &itemID1,
			Payload:        json.RawMessage(`{}`),
			SignatureValid: true,
			Processed:      false,
			CreatedAt:      time.Now(),
			UpdatedAt:      time.Now(),
		},
		{
			ID:             "webhook-2",
			WebhookType:    "TRANSACTIONS",
			ItemID:         &itemID2,
			Payload:        json.RawMessage(`{}`),
			SignatureValid: true,
			Processed:      false,
			CreatedAt:      time.Now(),
			UpdatedAt:      time.Now(),
		},
		{
			ID:             "webhook-3",
			WebhookType:    "TRANSACTIONS",
			ItemID:         &itemID3,
			Payload:        json.RawMessage(`{}`),
			SignatureValid: true,
			Processed:      false,
			CreatedAt:      time.Now(),
			UpdatedAt:      time.Now(),
		},
	}

	// Mock: GetUnprocessedByType returns 3 webhooks
	mockWebhookRepo.On("GetUnprocessedByType", ctx, "TRANSACTIONS", 10).Return(webhooks, nil)
	// Mock: No ITEM webhooks
	mockWebhookRepo.On("GetUnprocessedByType", ctx, "ITEM", 10).Return([]domain.WebhookEvent{}, nil)

	// Mock: ProcessWebhook - webhook1 succeeds, webhook2 FAILS, webhook3 succeeds
	mockSyncService.On("ProcessWebhook", ctx, &webhooks[0]).Return(nil)
	mockSyncService.On("ProcessWebhook", ctx, &webhooks[1]).Return(errors.New("sync failed for webhook-2"))
	mockSyncService.On("ProcessWebhook", ctx, &webhooks[2]).Return(nil)

	// Execute
	err := processor.ProcessPendingWebhooks(ctx)

	// Assert - should NOT error (errors are logged but processing continues)
	assert.NoError(t, err)
	mockWebhookRepo.AssertExpectations(t)
	// Verify ProcessWebhook was called 3 times (didn't stop after webhook2 error)
	mockSyncService.AssertNumberOfCalls(t, "ProcessWebhook", 3)
}

// TestWebhookProcessor_Start_GracefulShutdown tests context cancellation
func TestWebhookProcessor_Start_GracefulShutdown(t *testing.T) {
	mockSyncService := new(MockTransactionSyncServiceForWorker)
	mockItemErrorService := new(MockItemErrorServiceForWorker)
	mockWebhookRepo := new(MockWebhookRepoForWorker)

	processor := NewWebhookProcessor(mockSyncService, mockItemErrorService, mockWebhookRepo, 30*time.Second, 10, nil)

	// Create a context that's already cancelled
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	// Mock: Even though we cancel immediately, Start() might call ProcessPendingWebhooks once
	// We need to handle this case gracefully
	mockWebhookRepo.On("GetUnprocessedByType", mock.Anything, "TRANSACTIONS", 10).
		Return([]domain.WebhookEvent{}, nil).Maybe()
	mockWebhookRepo.On("GetUnprocessedByType", mock.Anything, "ITEM", 10).
		Return([]domain.WebhookEvent{}, nil).Maybe()

	// Execute in goroutine since Start() blocks until context is cancelled
	done := make(chan bool)
	go func() {
		processor.Start(ctx)
		done <- true
	}()

	// Wait for graceful shutdown with timeout
	select {
	case <-done:
		// Success - processor shut down gracefully
	case <-time.After(1 * time.Second):
		t.Fatal("Processor did not shut down gracefully within timeout")
	}
}

// TestWebhookProcessor_Start_ProcessesOnStartup tests immediate processing on startup
func TestWebhookProcessor_Start_ProcessesOnStartup(t *testing.T) {
	mockSyncService := new(MockTransactionSyncServiceForWorker)
	mockItemErrorService := new(MockItemErrorServiceForWorker)
	mockWebhookRepo := new(MockWebhookRepoForWorker)

	processor := NewWebhookProcessor(mockSyncService, mockItemErrorService, mockWebhookRepo, 1*time.Hour, 10, nil)

	// Create context with timeout (1s to allow processing to complete)
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	itemID := "item-1"
	webhook := domain.WebhookEvent{
		ID:             "webhook-startup",
		WebhookType:    "TRANSACTIONS",
		ItemID:         &itemID,
		Payload:        json.RawMessage(`{}`),
		SignatureValid: true,
		Processed:      false,
		CreatedAt:      time.Now(),
		UpdatedAt:      time.Now(),
	}

	// Mock: First call (on startup) returns 1 webhook
	mockWebhookRepo.On("GetUnprocessedByType", mock.Anything, "TRANSACTIONS", 10).
		Return([]domain.WebhookEvent{webhook}, nil).Once()
	// Mock: No ITEM webhooks
	mockWebhookRepo.On("GetUnprocessedByType", mock.Anything, "ITEM", 10).
		Return([]domain.WebhookEvent{}, nil).Maybe()

	// Mock: Subsequent calls return empty (to avoid infinite processing)
	mockWebhookRepo.On("GetUnprocessedByType", mock.Anything, "TRANSACTIONS", 10).
		Return([]domain.WebhookEvent{}, nil).Maybe()

	mockSyncService.On("ProcessWebhook", mock.Anything, &webhook).Return(nil).Once()

	// Execute
	processor.Start(ctx)

	// Assert - ProcessWebhook should have been called at least once on startup
	mockSyncService.AssertCalled(t, "ProcessWebhook", mock.Anything, &webhook)
}

// ============================================================================
// PHASE 6 RED: ITEM Webhook Processing Tests
// Issue #257: Item Error Handler - Process ITEM Webhooks
// ============================================================================

// TestWebhookProcessor_ProcessPendingWebhooks_ItemWebhook_Success tests successful ITEM webhook processing
func TestWebhookProcessor_ProcessPendingWebhooks_ItemWebhook_Success(t *testing.T) {
	ctx := context.Background()
	mockSyncService := new(MockTransactionSyncServiceForWorker)
	mockItemErrorService := new(MockItemErrorServiceForWorker)
	mockWebhookRepo := new(MockWebhookRepoForWorker)

	processor := NewWebhookProcessor(mockSyncService, mockItemErrorService, mockWebhookRepo, 30*time.Second, 10, nil)

	// Create ITEM webhook with error details in payload
	itemID1 := "item-1"
	itemWebhook := domain.WebhookEvent{
		ID:             "webhook-item-1",
		WebhookType:    "ITEM",
		ItemID:         &itemID1,
		Payload:        json.RawMessage(`{"error":{"error_type":"ITEM_LOGIN_REQUIRED","error_code":"ITEM_LOGIN_REQUIRED","error_message":"User credentials changed"}}`),
		SignatureValid: true,
		Processed:      false,
		CreatedAt:      time.Now(),
		UpdatedAt:      time.Now(),
	}

	// Mock: No TRANSACTIONS webhooks
	mockWebhookRepo.On("GetUnprocessedByType", ctx, "TRANSACTIONS", 10).Return([]domain.WebhookEvent{}, nil)
	// Mock: GetUnprocessedByType for ITEM webhooks returns 1 webhook
	mockWebhookRepo.On("GetUnprocessedByType", ctx, "ITEM", 10).Return([]domain.WebhookEvent{itemWebhook}, nil)

	// Mock: ItemErrorService.ProcessWebhook succeeds
	mockItemErrorService.On("ProcessWebhook", ctx, "item-1", "ITEM_LOGIN_REQUIRED", "ITEM_LOGIN_REQUIRED", "User credentials changed").Return(nil)

	// Execute
	err := processor.ProcessPendingWebhooks(ctx)

	// Assert
	assert.NoError(t, err)
	mockWebhookRepo.AssertExpectations(t)
	mockItemErrorService.AssertExpectations(t)
}

// TestWebhookProcessor_ProcessPendingWebhooks_ItemWebhook_PartialFailure tests ITEM webhook with error handling
func TestWebhookProcessor_ProcessPendingWebhooks_ItemWebhook_PartialFailure(t *testing.T) {
	ctx := context.Background()
	mockSyncService := new(MockTransactionSyncServiceForWorker)
	mockItemErrorService := new(MockItemErrorServiceForWorker)
	mockWebhookRepo := new(MockWebhookRepoForWorker)

	processor := NewWebhookProcessor(mockSyncService, mockItemErrorService, mockWebhookRepo, 30*time.Second, 10, nil)

	// Create 2 ITEM webhooks - first fails, second succeeds
	itemID1 := "item-1"
	itemID2 := "item-2"
	webhooks := []domain.WebhookEvent{
		{
			ID:             "webhook-item-1",
			WebhookType:    "ITEM",
			ItemID:         &itemID1,
			Payload:        json.RawMessage(`{"error":{"error_type":"ITEM_LOGIN_REQUIRED","error_code":"ITEM_LOGIN_REQUIRED","error_message":"User credentials changed"}}`),
			SignatureValid: true,
			Processed:      false,
			CreatedAt:      time.Now(),
			UpdatedAt:      time.Now(),
		},
		{
			ID:             "webhook-item-2",
			WebhookType:    "ITEM",
			ItemID:         &itemID2,
			Payload:        json.RawMessage(`{"error":{"error_type":"ERROR","error_code":"INSTITUTION_DOWN","error_message":"Institution temporarily unavailable"}}`),
			SignatureValid: true,
			Processed:      false,
			CreatedAt:      time.Now(),
			UpdatedAt:      time.Now(),
		},
	}

	// Mock: No TRANSACTIONS webhooks
	mockWebhookRepo.On("GetUnprocessedByType", ctx, "TRANSACTIONS", 10).Return([]domain.WebhookEvent{}, nil)
	// Mock: GetUnprocessedByType returns 2 ITEM webhooks
	mockWebhookRepo.On("GetUnprocessedByType", ctx, "ITEM", 10).Return(webhooks, nil)

	// Mock: First webhook fails, second succeeds
	mockItemErrorService.On("ProcessWebhook", ctx, "item-1", "ITEM_LOGIN_REQUIRED", "ITEM_LOGIN_REQUIRED", "User credentials changed").
		Return(errors.New("failed to process ITEM webhook"))
	mockItemErrorService.On("ProcessWebhook", ctx, "item-2", "ERROR", "INSTITUTION_DOWN", "Institution temporarily unavailable").
		Return(nil)

	// Execute
	err := processor.ProcessPendingWebhooks(ctx)

	// Assert - should NOT error (errors are logged but processing continues)
	assert.NoError(t, err)
	mockWebhookRepo.AssertExpectations(t)
	// Verify both webhooks were attempted (didn't stop after first failure)
	mockItemErrorService.AssertNumberOfCalls(t, "ProcessWebhook", 2)
}

// TestWebhookProcessor_ProcessPendingWebhooks_MixedWebhooks tests processing both TRANSACTIONS and ITEM webhooks
func TestWebhookProcessor_ProcessPendingWebhooks_MixedWebhooks(t *testing.T) {
	ctx := context.Background()
	mockSyncService := new(MockTransactionSyncServiceForWorker)
	mockItemErrorService := new(MockItemErrorServiceForWorker)
	mockWebhookRepo := new(MockWebhookRepoForWorker)

	processor := NewWebhookProcessor(mockSyncService, mockItemErrorService, mockWebhookRepo, 30*time.Second, 10, nil)

	// Create TRANSACTIONS webhook
	itemID1 := "item-1"
	transactionWebhook := domain.WebhookEvent{
		ID:             "webhook-txn-1",
		WebhookType:    "TRANSACTIONS",
		ItemID:         &itemID1,
		Payload:        json.RawMessage(`{"webhook_type":"TRANSACTIONS"}`),
		SignatureValid: true,
		Processed:      false,
		CreatedAt:      time.Now(),
		UpdatedAt:      time.Now(),
	}

	// Create ITEM webhook
	itemID2 := "item-2"
	itemWebhook := domain.WebhookEvent{
		ID:             "webhook-item-1",
		WebhookType:    "ITEM",
		ItemID:         &itemID2,
		Payload:        json.RawMessage(`{"error":{"error_type":"PENDING_EXPIRATION","error_code":"PENDING_EXPIRATION","error_message":"Item expiring soon"}}`),
		SignatureValid: true,
		Processed:      false,
		CreatedAt:      time.Now(),
		UpdatedAt:      time.Now(),
	}

	// Mock: GetUnprocessedByType returns TRANSACTIONS webhooks
	mockWebhookRepo.On("GetUnprocessedByType", ctx, "TRANSACTIONS", 10).Return([]domain.WebhookEvent{transactionWebhook}, nil)
	// Mock: GetUnprocessedByType returns ITEM webhooks
	mockWebhookRepo.On("GetUnprocessedByType", ctx, "ITEM", 10).Return([]domain.WebhookEvent{itemWebhook}, nil)

	// Mock: TransactionSyncService processes TRANSACTIONS webhook
	mockSyncService.On("ProcessWebhook", ctx, &transactionWebhook).Return(nil)

	// Mock: ItemErrorService processes ITEM webhook
	mockItemErrorService.On("ProcessWebhook", ctx, "item-2", "PENDING_EXPIRATION", "PENDING_EXPIRATION", "Item expiring soon").Return(nil)

	// Execute
	err := processor.ProcessPendingWebhooks(ctx)

	// Assert
	assert.NoError(t, err)
	mockWebhookRepo.AssertExpectations(t)
	mockSyncService.AssertExpectations(t)
	mockItemErrorService.AssertExpectations(t)
}

// TestWebhookProcessor_ProcessPendingWebhooks_ItemWebhook_InvalidPayload tests ITEM webhook with malformed payload
func TestWebhookProcessor_ProcessPendingWebhooks_ItemWebhook_InvalidPayload(t *testing.T) {
	ctx := context.Background()
	mockSyncService := new(MockTransactionSyncServiceForWorker)
	mockItemErrorService := new(MockItemErrorServiceForWorker)
	mockWebhookRepo := new(MockWebhookRepoForWorker)

	processor := NewWebhookProcessor(mockSyncService, mockItemErrorService, mockWebhookRepo, 30*time.Second, 10, nil)

	// Create ITEM webhook with invalid JSON payload
	itemID := "item-1"
	webhook := domain.WebhookEvent{
		ID:             "webhook-item-invalid",
		WebhookType:    "ITEM",
		ItemID:         &itemID,
		Payload:        json.RawMessage(`{invalid json`),
		SignatureValid: true, // this test targets payload-parsing failure, not the signature guard
		Processed:      false,
		CreatedAt:      time.Now(),
		UpdatedAt:      time.Now(),
	}

	// Mock: No TRANSACTIONS webhooks
	mockWebhookRepo.On("GetUnprocessedByType", ctx, "TRANSACTIONS", 10).Return([]domain.WebhookEvent{}, nil)
	// Mock: GetUnprocessedByType returns webhook with invalid payload
	mockWebhookRepo.On("GetUnprocessedByType", ctx, "ITEM", 10).Return([]domain.WebhookEvent{webhook}, nil)

	// Execute - should log error but not crash
	err := processor.ProcessPendingWebhooks(ctx)

	// Assert - should not return error (logged and continues)
	assert.NoError(t, err)
	mockWebhookRepo.AssertExpectations(t)
	// ItemErrorService.ProcessWebhook should NOT be called due to invalid payload
	mockItemErrorService.AssertNotCalled(t, "ProcessWebhook")
}

// ============================================================================
// Coverage Gap Tests - Issue #372
// ============================================================================

// TestWebhookProcessor_TransactionWebhooks_RepoError tests error handling when repository fails
func TestWebhookProcessor_TransactionWebhooks_RepoError(t *testing.T) {
	ctx := context.Background()
	mockSyncService := new(MockTransactionSyncServiceForWorker)
	mockItemErrorService := new(MockItemErrorServiceForWorker)
	mockWebhookRepo := new(MockWebhookRepoForWorker)

	processor := NewWebhookProcessor(mockSyncService, mockItemErrorService, mockWebhookRepo, 30*time.Second, 10, nil)

	// Mock: TRANSACTIONS repo returns error
	mockWebhookRepo.On("GetUnprocessedByType", ctx, "TRANSACTIONS", 10).
		Return(nil, errors.New("database connection failed"))
	// Mock: ITEM webhooks still processed
	mockWebhookRepo.On("GetUnprocessedByType", ctx, "ITEM", 10).
		Return([]domain.WebhookEvent{}, nil)

	// Execute
	err := processor.ProcessPendingWebhooks(ctx)

	// Assert - should not error (logged but continues to ITEM webhooks)
	assert.NoError(t, err)
	mockWebhookRepo.AssertExpectations(t)
	// Sync service should NOT be called since repo failed
	mockSyncService.AssertNotCalled(t, "ProcessWebhook")
}

// TestWebhookProcessor_ItemWebhooks_RepoError tests error handling when ITEM repo fails
func TestWebhookProcessor_ItemWebhooks_RepoError(t *testing.T) {
	ctx := context.Background()
	mockSyncService := new(MockTransactionSyncServiceForWorker)
	mockItemErrorService := new(MockItemErrorServiceForWorker)
	mockWebhookRepo := new(MockWebhookRepoForWorker)

	processor := NewWebhookProcessor(mockSyncService, mockItemErrorService, mockWebhookRepo, 30*time.Second, 10, nil)

	// Mock: TRANSACTIONS succeeds with empty
	mockWebhookRepo.On("GetUnprocessedByType", ctx, "TRANSACTIONS", 10).
		Return([]domain.WebhookEvent{}, nil)
	// Mock: ITEM repo returns error
	mockWebhookRepo.On("GetUnprocessedByType", ctx, "ITEM", 10).
		Return(nil, errors.New("database connection failed"))

	// Execute
	err := processor.ProcessPendingWebhooks(ctx)

	// Assert - should not error (logged but completes)
	assert.NoError(t, err)
	mockWebhookRepo.AssertExpectations(t)
}

// TestWebhookProcessor_ItemWebhook_NilItemID tests that webhooks with nil ItemID are skipped
func TestWebhookProcessor_ItemWebhook_NilItemID(t *testing.T) {
	ctx := context.Background()
	mockSyncService := new(MockTransactionSyncServiceForWorker)
	mockItemErrorService := new(MockItemErrorServiceForWorker)
	mockWebhookRepo := new(MockWebhookRepoForWorker)

	processor := NewWebhookProcessor(mockSyncService, mockItemErrorService, mockWebhookRepo, 30*time.Second, 10, nil)

	// Create ITEM webhook with nil ItemID
	webhook := domain.WebhookEvent{
		ID:             "webhook-nil-item",
		WebhookType:    "ITEM",
		ItemID:         nil, // This should be skipped
		Payload:        json.RawMessage(`{"error":{"error_type":"ERROR","error_code":"CODE","error_message":"msg"}}`),
		SignatureValid: true, // this test targets the nil-ItemID skip, not the signature guard
		Processed:      false,
		CreatedAt:      time.Now(),
		UpdatedAt:      time.Now(),
	}

	// Mock: No TRANSACTIONS webhooks
	mockWebhookRepo.On("GetUnprocessedByType", ctx, "TRANSACTIONS", 10).Return([]domain.WebhookEvent{}, nil)
	// Mock: Return webhook with nil ItemID
	mockWebhookRepo.On("GetUnprocessedByType", ctx, "ITEM", 10).Return([]domain.WebhookEvent{webhook}, nil)

	// Execute
	err := processor.ProcessPendingWebhooks(ctx)

	// Assert - should not error
	assert.NoError(t, err)
	mockWebhookRepo.AssertExpectations(t)
	// ItemErrorService should NOT be called since ItemID is nil
	mockItemErrorService.AssertNotCalled(t, "ProcessWebhook")
}

// TestWebhookProcessor_ItemWebhook_MissingErrorType tests handling of webhook with empty error fields
func TestWebhookProcessor_ItemWebhook_MissingErrorType(t *testing.T) {
	ctx := context.Background()
	mockSyncService := new(MockTransactionSyncServiceForWorker)
	mockItemErrorService := new(MockItemErrorServiceForWorker)
	mockWebhookRepo := new(MockWebhookRepoForWorker)

	processor := NewWebhookProcessor(mockSyncService, mockItemErrorService, mockWebhookRepo, 30*time.Second, 10, nil)

	// Create ITEM webhook with empty error fields
	itemID := "item-1"
	webhook := domain.WebhookEvent{
		ID:             "webhook-empty-error",
		WebhookType:    "ITEM",
		ItemID:         &itemID,
		Payload:        json.RawMessage(`{"error":{"error_type":"","error_code":"","error_message":""}}`),
		SignatureValid: true, // this test targets the missing-error-fields skip, not the signature guard
		Processed:      false,
		CreatedAt:      time.Now(),
		UpdatedAt:      time.Now(),
	}

	// Mock: No TRANSACTIONS webhooks
	mockWebhookRepo.On("GetUnprocessedByType", ctx, "TRANSACTIONS", 10).Return([]domain.WebhookEvent{}, nil)
	// Mock: Return webhook with empty error fields
	mockWebhookRepo.On("GetUnprocessedByType", ctx, "ITEM", 10).Return([]domain.WebhookEvent{webhook}, nil)

	// Execute
	err := processor.ProcessPendingWebhooks(ctx)

	// Assert - should not error
	assert.NoError(t, err)
	mockWebhookRepo.AssertExpectations(t)
	// ItemErrorService should NOT be called since error fields are empty (validation fails)
	mockItemErrorService.AssertNotCalled(t, "ProcessWebhook")
}

// TestWebhookProcessor_BothReposFail tests that function completes even if both repos fail
func TestWebhookProcessor_BothReposFail(t *testing.T) {
	ctx := context.Background()
	mockSyncService := new(MockTransactionSyncServiceForWorker)
	mockItemErrorService := new(MockItemErrorServiceForWorker)
	mockWebhookRepo := new(MockWebhookRepoForWorker)

	processor := NewWebhookProcessor(mockSyncService, mockItemErrorService, mockWebhookRepo, 30*time.Second, 10, nil)

	// Mock: Both repos fail
	mockWebhookRepo.On("GetUnprocessedByType", ctx, "TRANSACTIONS", 10).
		Return(nil, errors.New("TRANSACTIONS repo failed"))
	mockWebhookRepo.On("GetUnprocessedByType", ctx, "ITEM", 10).
		Return(nil, errors.New("ITEM repo failed"))

	// Execute
	err := processor.ProcessPendingWebhooks(ctx)

	// Assert - should not return error (errors are logged)
	assert.NoError(t, err)
	mockWebhookRepo.AssertExpectations(t)
}

// ============================================================================
// PHASE 7 ANALYTICS: WebhookProcessor Analytics Tests
// Issue #924: PostHog Analytics Instrumentation
// ============================================================================

// TestWebhookProcessor_ProcessPendingWebhooks_TransactionWebhook_Analytics tests analytics capture for TRANSACTIONS webhooks
func TestWebhookProcessor_ProcessPendingWebhooks_TransactionWebhook_Analytics(t *testing.T) {
	ctx := context.Background()
	mockSyncService := new(MockTransactionSyncServiceForWorker)
	mockItemErrorService := new(MockItemErrorServiceForWorker)
	mockWebhookRepo := new(MockWebhookRepoForWorker)
	mockAnalytics := new(MockAnalytics)

	processor := NewWebhookProcessor(mockSyncService, mockItemErrorService, mockWebhookRepo, 30*time.Second, 10, nil)
	processor.SetAnalytics(mockAnalytics)

	// Create test webhook
	itemID := "item-1"
	webhookCode := "SYNC_UPDATES_AVAILABLE"
	webhook := domain.WebhookEvent{
		ID:             "webhook-1",
		WebhookType:    "TRANSACTIONS",
		ItemID:         &itemID,
		WebhookCode:    &webhookCode,
		Payload:        json.RawMessage(`{}`),
		SignatureValid: true,
		Processed:      false,
		CreatedAt:      time.Now(),
		UpdatedAt:      time.Now(),
	}

	// Mock: GetUnprocessedByType returns webhook
	mockWebhookRepo.On("GetUnprocessedByType", ctx, "TRANSACTIONS", 10).Return([]domain.WebhookEvent{webhook}, nil)
	// Mock: No ITEM webhooks
	mockWebhookRepo.On("GetUnprocessedByType", ctx, "ITEM", 10).Return([]domain.WebhookEvent{}, nil)

	// Mock: ProcessWebhook succeeds
	mockSyncService.On("ProcessWebhook", ctx, &webhook).Return(nil)

	// Mock: Analytics.Capture called with correct parameters
	mockAnalytics.On("Capture", "item-1", posthog.EventWebhookReceived, mock.MatchedBy(func(props map[string]interface{}) bool {
		return props["webhook_type"] == "TRANSACTIONS" &&
			props["webhook_code"] == "SYNC_UPDATES_AVAILABLE"
	})).Return()

	// Execute
	err := processor.ProcessPendingWebhooks(ctx)

	// Assert
	assert.NoError(t, err)
	mockWebhookRepo.AssertExpectations(t)
	mockSyncService.AssertExpectations(t)
	mockAnalytics.AssertExpectations(t)
}

// TestWebhookProcessor_ProcessPendingWebhooks_ItemWebhook_Analytics tests analytics capture for ITEM webhooks
func TestWebhookProcessor_ProcessPendingWebhooks_ItemWebhook_Analytics(t *testing.T) {
	ctx := context.Background()
	mockSyncService := new(MockTransactionSyncServiceForWorker)
	mockItemErrorService := new(MockItemErrorServiceForWorker)
	mockWebhookRepo := new(MockWebhookRepoForWorker)
	mockAnalytics := new(MockAnalytics)

	processor := NewWebhookProcessor(mockSyncService, mockItemErrorService, mockWebhookRepo, 30*time.Second, 10, nil)
	processor.SetAnalytics(mockAnalytics)

	// Create test ITEM webhook
	itemID := "item-1"
	webhook := domain.WebhookEvent{
		ID:             "webhook-item-1",
		WebhookType:    "ITEM",
		ItemID:         &itemID,
		WebhookCode:    nil, // No webhook code for ITEM webhooks
		Payload:        json.RawMessage(`{"error":{"error_type":"ITEM_LOGIN_REQUIRED","error_code":"ITEM_LOGIN_REQUIRED","error_message":"User credentials changed"}}`),
		SignatureValid: true,
		Processed:      false,
		CreatedAt:      time.Now(),
		UpdatedAt:      time.Now(),
	}

	// Mock: No TRANSACTIONS webhooks
	mockWebhookRepo.On("GetUnprocessedByType", ctx, "TRANSACTIONS", 10).Return([]domain.WebhookEvent{}, nil)
	// Mock: GetUnprocessedByType for ITEM webhooks
	mockWebhookRepo.On("GetUnprocessedByType", ctx, "ITEM", 10).Return([]domain.WebhookEvent{webhook}, nil)

	// Mock: ItemErrorService.ProcessWebhook succeeds
	mockItemErrorService.On("ProcessWebhook", ctx, "item-1", "ITEM_LOGIN_REQUIRED", "ITEM_LOGIN_REQUIRED", "User credentials changed").Return(nil)

	// Mock: Analytics.Capture called with correct parameters for ITEM webhook
	mockAnalytics.On("Capture", "item-1", posthog.EventWebhookReceived, mock.MatchedBy(func(props map[string]interface{}) bool {
		return props["webhook_type"] == "ITEM" &&
			props["webhook_code"] == "" // Empty string since no WebhookCode
	})).Return()

	// Execute
	err := processor.ProcessPendingWebhooks(ctx)

	// Assert
	assert.NoError(t, err)
	mockWebhookRepo.AssertExpectations(t)
	mockItemErrorService.AssertExpectations(t)
	mockAnalytics.AssertExpectations(t)
}

// TestWebhookProcessor_ProcessPendingWebhooks_AnalyticsWithNilItemID tests analytics fallback to "system" when ItemID is nil (TRANSACTIONS only)
func TestWebhookProcessor_ProcessPendingWebhooks_AnalyticsWithNilItemID(t *testing.T) {
	ctx := context.Background()
	mockSyncService := new(MockTransactionSyncServiceForWorker)
	mockItemErrorService := new(MockItemErrorServiceForWorker)
	mockWebhookRepo := new(MockWebhookRepoForWorker)
	mockAnalytics := new(MockAnalytics)

	processor := NewWebhookProcessor(mockSyncService, mockItemErrorService, mockWebhookRepo, 30*time.Second, 10, nil)
	processor.SetAnalytics(mockAnalytics)

	// Create test webhook with nil ItemID
	webhook := domain.WebhookEvent{
		ID:             "webhook-1",
		WebhookType:    "TRANSACTIONS",
		ItemID:         nil, // No ItemID - should fallback to "system"
		WebhookCode:    nil,
		Payload:        json.RawMessage(`{}`),
		SignatureValid: true,
		Processed:      false,
		CreatedAt:      time.Now(),
		UpdatedAt:      time.Now(),
	}

	// Mock: GetUnprocessedByType returns webhook
	mockWebhookRepo.On("GetUnprocessedByType", ctx, "TRANSACTIONS", 10).Return([]domain.WebhookEvent{webhook}, nil)
	// Mock: No ITEM webhooks
	mockWebhookRepo.On("GetUnprocessedByType", ctx, "ITEM", 10).Return([]domain.WebhookEvent{}, nil)

	// Mock: ProcessWebhook succeeds
	mockSyncService.On("ProcessWebhook", ctx, &webhook).Return(nil)

	// Mock: Analytics.Capture called with "system" as distinctID
	mockAnalytics.On("Capture", "system", posthog.EventWebhookReceived, mock.MatchedBy(func(props map[string]interface{}) bool {
		return props["webhook_type"] == "TRANSACTIONS" &&
			props["webhook_code"] == ""
	})).Return()

	// Execute
	err := processor.ProcessPendingWebhooks(ctx)

	// Assert
	assert.NoError(t, err)
	mockWebhookRepo.AssertExpectations(t)
	mockSyncService.AssertExpectations(t)
	mockAnalytics.AssertExpectations(t)
}

// TestWebhookProcessor_ProcessPendingWebhooks_AnalyticsNilClient tests graceful handling when analytics client is nil
func TestWebhookProcessor_ProcessPendingWebhooks_AnalyticsNilClient(t *testing.T) {
	ctx := context.Background()
	mockSyncService := new(MockTransactionSyncServiceForWorker)
	mockItemErrorService := new(MockItemErrorServiceForWorker)
	mockWebhookRepo := new(MockWebhookRepoForWorker)

	processor := NewWebhookProcessor(mockSyncService, mockItemErrorService, mockWebhookRepo, 30*time.Second, 10, nil)
	// Don't set analytics - it should be nil

	// Create test webhook
	itemID := "item-1"
	webhook := domain.WebhookEvent{
		ID:             "webhook-1",
		WebhookType:    "TRANSACTIONS",
		ItemID:         &itemID,
		WebhookCode:    nil,
		Payload:        json.RawMessage(`{}`),
		SignatureValid: true,
		Processed:      false,
		CreatedAt:      time.Now(),
		UpdatedAt:      time.Now(),
	}

	// Mock: GetUnprocessedByType returns webhook
	mockWebhookRepo.On("GetUnprocessedByType", ctx, "TRANSACTIONS", 10).Return([]domain.WebhookEvent{webhook}, nil)
	// Mock: No ITEM webhooks
	mockWebhookRepo.On("GetUnprocessedByType", ctx, "ITEM", 10).Return([]domain.WebhookEvent{}, nil)

	// Mock: ProcessWebhook succeeds
	mockSyncService.On("ProcessWebhook", ctx, &webhook).Return(nil)

	// Execute - should not crash even with nil analytics
	err := processor.ProcessPendingWebhooks(ctx)

	// Assert
	assert.NoError(t, err)
	mockWebhookRepo.AssertExpectations(t)
	mockSyncService.AssertExpectations(t)
}

// ============================================================================
// Issue #1514: Signature Validity Guard Tests
//
// This legacy poller is unwired dead code (production runs
// jobs.NewWebhookProcessingWorker instead - see main.go), but it must not be a
// landmine if ever revived: it must refuse to dispatch webhooks whose
// signature was never verified (SignatureValid: false).
// ============================================================================

// TestWebhookProcessor_TransactionWebhook_InvalidSignature_NotDispatched tests that a
// TRANSACTIONS webhook with SignatureValid: false is NOT dispatched to TransactionSyncService.
func TestWebhookProcessor_TransactionWebhook_InvalidSignature_NotDispatched(t *testing.T) {
	ctx := context.Background()
	mockSyncService := new(MockTransactionSyncServiceForWorker)
	mockItemErrorService := new(MockItemErrorServiceForWorker)
	mockWebhookRepo := new(MockWebhookRepoForWorker)

	processor := NewWebhookProcessor(mockSyncService, mockItemErrorService, mockWebhookRepo, 30*time.Second, 10, nil)

	itemID := "item-1"
	webhook := domain.WebhookEvent{
		ID:             "webhook-invalid-sig-txn",
		WebhookType:    "TRANSACTIONS",
		ItemID:         &itemID,
		Payload:        json.RawMessage(`{}`),
		SignatureValid: false,
		Processed:      false,
		CreatedAt:      time.Now(),
		UpdatedAt:      time.Now(),
	}

	mockWebhookRepo.On("GetUnprocessedByType", ctx, "TRANSACTIONS", 10).Return([]domain.WebhookEvent{webhook}, nil)
	mockWebhookRepo.On("GetUnprocessedByType", ctx, "ITEM", 10).Return([]domain.WebhookEvent{}, nil)
	mockWebhookRepo.On("MarkFailed", ctx, "webhook-invalid-sig-txn", mock.AnythingOfType("string")).Return(nil)

	// Execute
	err := processor.ProcessPendingWebhooks(ctx)

	// Assert - refused row must never reach the sync service
	assert.NoError(t, err)
	mockSyncService.AssertNotCalled(t, "ProcessWebhook")
	mockWebhookRepo.AssertExpectations(t)
}

// TestWebhookProcessor_ItemWebhook_InvalidSignature_NotDispatched tests that an ITEM webhook
// with SignatureValid: false is NOT dispatched to ItemErrorService (which can change a user's
// bank-connection status and send notifications/email).
func TestWebhookProcessor_ItemWebhook_InvalidSignature_NotDispatched(t *testing.T) {
	ctx := context.Background()
	mockSyncService := new(MockTransactionSyncServiceForWorker)
	mockItemErrorService := new(MockItemErrorServiceForWorker)
	mockWebhookRepo := new(MockWebhookRepoForWorker)

	processor := NewWebhookProcessor(mockSyncService, mockItemErrorService, mockWebhookRepo, 30*time.Second, 10, nil)

	itemID := "item-1"
	webhook := domain.WebhookEvent{
		ID:             "webhook-invalid-sig-item",
		WebhookType:    "ITEM",
		ItemID:         &itemID,
		Payload:        json.RawMessage(`{"error":{"error_type":"ITEM_LOGIN_REQUIRED","error_code":"ITEM_LOGIN_REQUIRED","error_message":"User credentials changed"}}`),
		SignatureValid: false,
		Processed:      false,
		CreatedAt:      time.Now(),
		UpdatedAt:      time.Now(),
	}

	mockWebhookRepo.On("GetUnprocessedByType", ctx, "TRANSACTIONS", 10).Return([]domain.WebhookEvent{}, nil)
	mockWebhookRepo.On("GetUnprocessedByType", ctx, "ITEM", 10).Return([]domain.WebhookEvent{webhook}, nil)
	mockWebhookRepo.On("MarkFailed", ctx, "webhook-invalid-sig-item", mock.AnythingOfType("string")).Return(nil)

	// Execute
	err := processor.ProcessPendingWebhooks(ctx)

	// Assert - refused row must never reach the item error service
	assert.NoError(t, err)
	mockItemErrorService.AssertNotCalled(t, "ProcessWebhook")
	mockWebhookRepo.AssertExpectations(t)
}

// TestWebhookProcessor_TransactionWebhook_InvalidSignature_SkipsAnalytics tests that the
// signature guard runs BEFORE the PostHog analytics capture on the TRANSACTIONS path, not
// after it. An unverified row's ItemID is attacker-supplied and must never be used as an
// analytics distinct_id.
func TestWebhookProcessor_TransactionWebhook_InvalidSignature_SkipsAnalytics(t *testing.T) {
	ctx := context.Background()
	mockSyncService := new(MockTransactionSyncServiceForWorker)
	mockItemErrorService := new(MockItemErrorServiceForWorker)
	mockWebhookRepo := new(MockWebhookRepoForWorker)
	mockAnalytics := new(MockAnalytics)

	processor := NewWebhookProcessor(mockSyncService, mockItemErrorService, mockWebhookRepo, 30*time.Second, 10, nil)
	processor.SetAnalytics(mockAnalytics)

	itemID := "item-1"
	webhook := domain.WebhookEvent{
		ID:             "webhook-invalid-sig-txn-analytics",
		WebhookType:    "TRANSACTIONS",
		ItemID:         &itemID,
		Payload:        json.RawMessage(`{}`),
		SignatureValid: false,
		Processed:      false,
		CreatedAt:      time.Now(),
		UpdatedAt:      time.Now(),
	}

	mockWebhookRepo.On("GetUnprocessedByType", ctx, "TRANSACTIONS", 10).Return([]domain.WebhookEvent{webhook}, nil)
	mockWebhookRepo.On("GetUnprocessedByType", ctx, "ITEM", 10).Return([]domain.WebhookEvent{}, nil)
	mockWebhookRepo.On("MarkFailed", ctx, "webhook-invalid-sig-txn-analytics", mock.AnythingOfType("string")).Return(nil)

	// Execute
	err := processor.ProcessPendingWebhooks(ctx)

	// Assert - the refused row must never reach PostHog
	assert.NoError(t, err)
	mockAnalytics.AssertNotCalled(t, "Capture")
	mockWebhookRepo.AssertExpectations(t)
}

// TestWebhookProcessor_ItemWebhook_InvalidSignature_SkipsValidationAndAnalytics tests that the
// signature guard on the ITEM path runs BEFORE the nil-ItemID check, before analytics capture,
// and before payload parsing/validation. A webhook with a nil ItemID AND an invalid signature
// must still be MarkFailed by the signature guard - if the nil-ItemID `continue` ran first, the
// row would silently skip the guard and remain processed=false forever (Defect 1).
func TestWebhookProcessor_ItemWebhook_InvalidSignature_SkipsValidationAndAnalytics(t *testing.T) {
	ctx := context.Background()
	mockSyncService := new(MockTransactionSyncServiceForWorker)
	mockItemErrorService := new(MockItemErrorServiceForWorker)
	mockWebhookRepo := new(MockWebhookRepoForWorker)
	mockAnalytics := new(MockAnalytics)

	processor := NewWebhookProcessor(mockSyncService, mockItemErrorService, mockWebhookRepo, 30*time.Second, 10, nil)
	processor.SetAnalytics(mockAnalytics)

	// Nil ItemID AND invalid signature AND malformed payload - if the guard ran anywhere
	// other than first, one of the pre-guard checks would `continue` before MarkFailed is
	// ever called.
	webhook := domain.WebhookEvent{
		ID:             "webhook-invalid-sig-item-nil-item-id",
		WebhookType:    "ITEM",
		ItemID:         nil,
		Payload:        json.RawMessage(`{invalid json`),
		SignatureValid: false,
		Processed:      false,
		CreatedAt:      time.Now(),
		UpdatedAt:      time.Now(),
	}

	mockWebhookRepo.On("GetUnprocessedByType", ctx, "TRANSACTIONS", 10).Return([]domain.WebhookEvent{}, nil)
	mockWebhookRepo.On("GetUnprocessedByType", ctx, "ITEM", 10).Return([]domain.WebhookEvent{webhook}, nil)
	mockWebhookRepo.On("MarkFailed", ctx, "webhook-invalid-sig-item-nil-item-id", mock.AnythingOfType("string")).Return(nil)

	// Execute
	err := processor.ProcessPendingWebhooks(ctx)

	// Assert - the guard must have run (MarkFailed called, asserted via AssertExpectations
	// below) despite the nil ItemID and malformed payload that would otherwise `continue` first.
	assert.NoError(t, err)
	mockAnalytics.AssertNotCalled(t, "Capture")
	mockItemErrorService.AssertNotCalled(t, "ProcessWebhook")
	mockWebhookRepo.AssertExpectations(t)
}

// TestWebhookProcessor_TransactionWebhook_InvalidSignature_MarkFailedError_Propagates tests
// that a failure to persist the invalid-signature refusal (MarkFailed) is surfaced as a
// non-nil error from ProcessPendingWebhooks, rather than silently treated as handled (Defect
// 2). Dispatch must still be refused unconditionally even though persistence failed.
func TestWebhookProcessor_TransactionWebhook_InvalidSignature_MarkFailedError_Propagates(t *testing.T) {
	ctx := context.Background()
	mockSyncService := new(MockTransactionSyncServiceForWorker)
	mockItemErrorService := new(MockItemErrorServiceForWorker)
	mockWebhookRepo := new(MockWebhookRepoForWorker)

	processor := NewWebhookProcessor(mockSyncService, mockItemErrorService, mockWebhookRepo, 30*time.Second, 10, nil)

	itemID := "item-1"
	webhook := domain.WebhookEvent{
		ID:             "webhook-invalid-sig-markfailed-err",
		WebhookType:    "TRANSACTIONS",
		ItemID:         &itemID,
		Payload:        json.RawMessage(`{}`),
		SignatureValid: false,
		Processed:      false,
		CreatedAt:      time.Now(),
		UpdatedAt:      time.Now(),
	}

	mockWebhookRepo.On("GetUnprocessedByType", ctx, "TRANSACTIONS", 10).Return([]domain.WebhookEvent{webhook}, nil)
	mockWebhookRepo.On("GetUnprocessedByType", ctx, "ITEM", 10).Return([]domain.WebhookEvent{}, nil)
	mockWebhookRepo.On("MarkFailed", ctx, "webhook-invalid-sig-markfailed-err", mock.AnythingOfType("string")).
		Return(errors.New("db unavailable"))

	// Execute
	err := processor.ProcessPendingWebhooks(ctx)

	// Assert - dispatch is still refused, but the persistence failure is surfaced, not swallowed
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "webhook-invalid-sig-markfailed-err")
	mockSyncService.AssertNotCalled(t, "ProcessWebhook")
	mockWebhookRepo.AssertExpectations(t)
}
