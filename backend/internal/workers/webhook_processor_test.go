package workers

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
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

// ============================================================================
// PHASE 5 RED: WebhookProcessor Tests
// Issue #256: Transaction Sync Service
// ============================================================================

// TestWebhookProcessor_ProcessPendingWebhooks_Success tests successful processing of webhooks
func TestWebhookProcessor_ProcessPendingWebhooks_Success(t *testing.T) {
	ctx := context.Background()
	mockSyncService := new(MockTransactionSyncServiceForWorker)
	mockWebhookRepo := new(MockWebhookRepoForWorker)

	processor := NewWebhookProcessor(mockSyncService, mockWebhookRepo, 30*time.Second, 10)

	// Create test webhooks
	itemID1 := "item-1"
	itemID2 := "item-2"
	webhooks := []domain.WebhookEvent{
		{
			ID:          "webhook-1",
			WebhookType: "TRANSACTIONS",
			ItemID:      &itemID1,
			Payload:     json.RawMessage(`{"webhook_type":"TRANSACTIONS"}`),
			Processed:   false,
			CreatedAt:   time.Now(),
			UpdatedAt:   time.Now(),
		},
		{
			ID:          "webhook-2",
			WebhookType: "TRANSACTIONS",
			ItemID:      &itemID2,
			Payload:     json.RawMessage(`{"webhook_type":"TRANSACTIONS"}`),
			Processed:   false,
			CreatedAt:   time.Now(),
			UpdatedAt:   time.Now(),
		},
	}

	// Mock: GetUnprocessedByType returns 2 webhooks
	mockWebhookRepo.On("GetUnprocessedByType", ctx, "TRANSACTIONS", 10).Return(webhooks, nil)

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
	mockWebhookRepo := new(MockWebhookRepoForWorker)

	processor := NewWebhookProcessor(mockSyncService, mockWebhookRepo, 30*time.Second, 10)

	// Mock: GetUnprocessedByType returns empty slice
	mockWebhookRepo.On("GetUnprocessedByType", ctx, "TRANSACTIONS", 10).Return([]domain.WebhookEvent{}, nil)

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
	mockWebhookRepo := new(MockWebhookRepoForWorker)

	processor := NewWebhookProcessor(mockSyncService, mockWebhookRepo, 30*time.Second, 10)

	// Create 3 test webhooks
	itemID1 := "item-1"
	itemID2 := "item-2"
	itemID3 := "item-3"
	webhooks := []domain.WebhookEvent{
		{
			ID:          "webhook-1",
			WebhookType: "TRANSACTIONS",
			ItemID:      &itemID1,
			Payload:     json.RawMessage(`{}`),
			Processed:   false,
			CreatedAt:   time.Now(),
			UpdatedAt:   time.Now(),
		},
		{
			ID:          "webhook-2",
			WebhookType: "TRANSACTIONS",
			ItemID:      &itemID2,
			Payload:     json.RawMessage(`{}`),
			Processed:   false,
			CreatedAt:   time.Now(),
			UpdatedAt:   time.Now(),
		},
		{
			ID:          "webhook-3",
			WebhookType: "TRANSACTIONS",
			ItemID:      &itemID3,
			Payload:     json.RawMessage(`{}`),
			Processed:   false,
			CreatedAt:   time.Now(),
			UpdatedAt:   time.Now(),
		},
	}

	// Mock: GetUnprocessedByType returns 3 webhooks
	mockWebhookRepo.On("GetUnprocessedByType", ctx, "TRANSACTIONS", 10).Return(webhooks, nil)

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
	mockWebhookRepo := new(MockWebhookRepoForWorker)

	processor := NewWebhookProcessor(mockSyncService, mockWebhookRepo, 30*time.Second, 10)

	// Create a context that's already cancelled
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	// Mock: Even though we cancel immediately, Start() might call ProcessPendingWebhooks once
	// We need to handle this case gracefully
	mockWebhookRepo.On("GetUnprocessedByType", mock.Anything, "TRANSACTIONS", 10).
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
	mockWebhookRepo := new(MockWebhookRepoForWorker)

	processor := NewWebhookProcessor(mockSyncService, mockWebhookRepo, 1*time.Hour, 10)

	// Create context with timeout (1s to allow processing to complete)
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	itemID := "item-1"
	webhook := domain.WebhookEvent{
		ID:          "webhook-startup",
		WebhookType: "TRANSACTIONS",
		ItemID:      &itemID,
		Payload:     json.RawMessage(`{}`),
		Processed:   false,
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	}

	// Mock: First call (on startup) returns 1 webhook
	mockWebhookRepo.On("GetUnprocessedByType", mock.Anything, "TRANSACTIONS", 10).
		Return([]domain.WebhookEvent{webhook}, nil).Once()

	// Mock: Subsequent calls return empty (to avoid infinite processing)
	mockWebhookRepo.On("GetUnprocessedByType", mock.Anything, "TRANSACTIONS", 10).
		Return([]domain.WebhookEvent{}, nil).Maybe()

	mockSyncService.On("ProcessWebhook", mock.Anything, &webhook).Return(nil).Once()

	// Execute
	processor.Start(ctx)

	// Assert - ProcessWebhook should have been called at least once on startup
	mockSyncService.AssertCalled(t, "ProcessWebhook", mock.Anything, &webhook)
}
