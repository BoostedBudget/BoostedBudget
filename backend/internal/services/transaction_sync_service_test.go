package services

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/maxnussbaum/BoostedBudget/internal/config"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/repository"
	"github.com/maxnussbaum/BoostedBudget/internal/testsupport/mockassert"
	"github.com/plaid/plaid-go/v23/plaid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// ============================================================================
// Mock Repositories
// ============================================================================
// Note: MockPlaidService, MockLinkedItemRepository, MockTransactionRepository
// are defined in other test files in this package and reused here.

// MockWebhookRepository mocks WebhookRepository for testing
// This is unique to transaction sync service tests
type MockWebhookRepository struct {
	mock.Mock
}

func (m *MockWebhookRepository) Create(ctx context.Context, event *domain.WebhookEvent) error {
	args := m.Called(ctx, event)
	return args.Error(0)
}

func (m *MockWebhookRepository) MarkProcessed(ctx context.Context, id string) error {
	args := m.Called(ctx, id)
	return args.Error(0)
}

func (m *MockWebhookRepository) MarkFailed(ctx context.Context, id string, errorMsg string) error {
	args := m.Called(ctx, id, errorMsg)
	return args.Error(0)
}

func (m *MockWebhookRepository) GetUnprocessed(ctx context.Context) ([]domain.WebhookEvent, error) {
	args := m.Called(ctx)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]domain.WebhookEvent), args.Error(1)
}

func (m *MockWebhookRepository) GetByItemID(ctx context.Context, itemID string, limit int) ([]domain.WebhookEvent, error) {
	args := m.Called(ctx, itemID, limit)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]domain.WebhookEvent), args.Error(1)
}

func (m *MockWebhookRepository) Update(ctx context.Context, event *domain.WebhookEvent) error {
	args := m.Called(ctx, event)
	return args.Error(0)
}

func (m *MockWebhookRepository) GetUnprocessedByType(ctx context.Context, webhookType string, limit int) ([]domain.WebhookEvent, error) {
	args := m.Called(ctx, webhookType, limit)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]domain.WebhookEvent), args.Error(1)
}

func (m *MockWebhookRepository) GetByID(ctx context.Context, id string) (*domain.WebhookEvent, error) {
	args := m.Called(ctx, id)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*domain.WebhookEvent), args.Error(1)
}

func (m *MockWebhookRepository) IncrementRetryCount(ctx context.Context, id string) error {
	args := m.Called(ctx, id)
	return args.Error(0)
}

// Note: MockEncryptionService is defined in account_service_test.go and reused here

// ============================================================================
// PHASE 4 RED: Failing Tests for TransactionSyncService
// Issue #256: Transaction Sync Service
// ============================================================================

// TestProcessWebhook_Success tests successful webhook processing
// TDD RED phase: This test will fail because ProcessWebhook doesn't exist yet
func TestProcessWebhook_Success(t *testing.T) {
	// Setup mocks
	mockWebhookRepo := new(MockWebhookRepository)
	mockLinkedItemRepo := new(MockLinkedItemRepository)
	mockTransactionRepo := new(MockTransactionRepository)
	mockAccountRepo := new(MockAccountRepository)
	mockPlaidService := new(MockPlaidService)
	service := NewTransactionSyncService(
		mockWebhookRepo,
		mockLinkedItemRepo,
		mockTransactionRepo,
		mockAccountRepo,
		nil, // profileRepo - not needed for these tests
		nil, // balanceSnapshotRepo - not needed for these tests
		mockPlaidService,
		nil, // subscriptionService - not needed for these tests
		nil, // ruleApplicationService - not needed for these tests
		nil, // budgetSyncService - not needed for these tests
		nil, // transactionMonitorService - not needed for these tests
		nil, // merchantRepo - not needed for these tests
		nil, // logger
	)

	ctx := context.Background()
	webhookID := "webhook-123"

	// Mock webhook event with item_id
	itemID := "item-abc"
	webhookEvent := &domain.WebhookEvent{
		ID:          webhookID,
		WebhookType: "TRANSACTIONS",
		WebhookCode: stringPtr("DEFAULT_UPDATE"),
		ItemID:      &itemID,
		Payload:     json.RawMessage(`{"webhook_type":"TRANSACTIONS","item_id":"item-abc"}`),
		Processed:   false,
		RetryCount:  0,
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	}

	// Mock linked item with encrypted access token
	linkedItem := &domain.LinkedItem{
		ID:          "linked-item-123",
		UserID:      "user-123",
		ItemID:      itemID,
		AccessToken: "decrypted-access-token",
		Cursor:      stringPtr("old-cursor"),
		Provider:    "plaid",
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	}

	// Mock webhook update (mark as processing started)
	mockWebhookRepo.On("Update", ctx, mock.MatchedBy(func(e *domain.WebhookEvent) bool {
		return e.ID == webhookID && e.ProcessingStartedAt != nil
	})).Return(nil).Once()

	// Mock fetching webhook by ID (not implemented in repository yet, will need to add GetByID)
	// For now, we'll assume the webhook is passed directly or fetched earlier
	// This is a simplification for the RED phase

	// Mock getting linked item
	mockLinkedItemRepo.On("GetByItemID", ctx, itemID).Return(linkedItem, nil)

	// Mock Plaid API call for TransactionsSync
	plaidResp := &plaid.TransactionsSyncResponse{
		Added:      []plaid.Transaction{{TransactionId: "txn-1"}},
		Modified:   []plaid.Transaction{},
		Removed:    []plaid.RemovedTransaction{},
		NextCursor: "new-cursor-from-plaid",
		HasMore:    false,
	}
	mockPlaidService.On("TransactionsSync", ctx, "decrypted-access-token", "old-cursor", 0).Return(plaidResp, nil)

	// Mock cursor update (after sync completes)
	mockLinkedItemRepo.On("UpdateCursor", ctx, linkedItem.ID, "new-cursor-from-plaid").Return(nil)

	// Issue #261: Mock account repository for transaction storage
	mockAccountRepo.On("GetByLinkedItemIDIncludingRetained", ctx, linkedItem.ID).Return([]domain.Account{}, nil)
	// Mock transaction batch creation (empty because transaction has unmapped account, so will be skipped)
	// No accounts exist, so all transactions will be skipped in processAddedTransactions

	// Mock marking webhook as processed
	mockWebhookRepo.On("Update", ctx, mock.MatchedBy(func(e *domain.WebhookEvent) bool {
		return e.ID == webhookID && e.Processed && e.ProcessingCompletedAt != nil
	})).Return(nil).Once()

	// Execute
	err := service.ProcessWebhook(ctx, webhookEvent)

	// Assert
	assert.NoError(t, err)
	mockWebhookRepo.AssertExpectations(t)
	mockLinkedItemRepo.AssertExpectations(t)
}

// TestProcessWebhook_WebhookNotFound tests error handling when webhook doesn't exist
func TestProcessWebhook_WebhookNotFound(t *testing.T) {
	mockWebhookRepo := new(MockWebhookRepository)
	mockLinkedItemRepo := new(MockLinkedItemRepository)
	mockTransactionRepo := new(MockTransactionRepository)
	mockAccountRepo := new(MockAccountRepository)
	mockPlaidService := new(MockPlaidService)
	service := NewTransactionSyncService(
		mockWebhookRepo,
		mockLinkedItemRepo,
		mockTransactionRepo,
		mockAccountRepo,
		nil, // profileRepo - not needed for these tests
		nil, // balanceSnapshotRepo - not needed for these tests
		mockPlaidService,
		nil, // subscriptionService - not needed for these tests
		nil, // ruleApplicationService - not needed for these tests
		nil, // budgetSyncService - not needed for these tests
		nil, // transactionMonitorService - not needed for these tests
		nil, // merchantRepo - not needed for these tests
		nil, // logger
	)

	ctx := context.Background()

	// Execute with nil webhook (simulating not found)
	err := service.ProcessWebhook(ctx, nil)

	// Assert
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "webhook event is nil")
}

// TestProcessWebhook_ItemIDMissing tests error handling when webhook has no item_id
func TestProcessWebhook_ItemIDMissing(t *testing.T) {
	mockWebhookRepo := new(MockWebhookRepository)
	mockLinkedItemRepo := new(MockLinkedItemRepository)
	mockTransactionRepo := new(MockTransactionRepository)
	mockAccountRepo := new(MockAccountRepository)
	mockPlaidService := new(MockPlaidService)
	service := NewTransactionSyncService(
		mockWebhookRepo,
		mockLinkedItemRepo,
		mockTransactionRepo,
		mockAccountRepo,
		nil, // profileRepo - not needed for these tests
		nil, // balanceSnapshotRepo - not needed for these tests
		mockPlaidService,
		nil, // subscriptionService - not needed for these tests
		nil, // ruleApplicationService - not needed for these tests
		nil, // budgetSyncService - not needed for these tests
		nil, // transactionMonitorService - not needed for these tests
		nil, // merchantRepo - not needed for these tests
		nil, // logger
	)

	ctx := context.Background()

	// Webhook without item_id (some webhook types don't have item_id)
	webhookEvent := &domain.WebhookEvent{
		ID:          "webhook-123",
		WebhookType: "TRANSACTIONS",
		ItemID:      nil, // Missing item_id
		Processed:   false,
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	}

	// Mock update to mark error
	mockWebhookRepo.On("Update", ctx, mock.MatchedBy(func(e *domain.WebhookEvent) bool {
		return e.ID == webhookEvent.ID && e.ProcessingError != nil
	})).Return(nil)

	// Execute
	err := service.ProcessWebhook(ctx, webhookEvent)

	// Assert
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "item_id")
	mockWebhookRepo.AssertExpectations(t)
}

// TestProcessWebhook_LinkedItemNotFound tests error handling when linked item doesn't exist
func TestProcessWebhook_LinkedItemNotFound(t *testing.T) {
	mockWebhookRepo := new(MockWebhookRepository)
	mockLinkedItemRepo := new(MockLinkedItemRepository)
	mockTransactionRepo := new(MockTransactionRepository)
	mockAccountRepo := new(MockAccountRepository)
	mockPlaidService := new(MockPlaidService)
	service := NewTransactionSyncService(
		mockWebhookRepo,
		mockLinkedItemRepo,
		mockTransactionRepo,
		mockAccountRepo,
		nil, // profileRepo - not needed for these tests
		nil, // balanceSnapshotRepo - not needed for these tests
		mockPlaidService,
		nil, // subscriptionService - not needed for these tests
		nil, // ruleApplicationService - not needed for these tests
		nil, // budgetSyncService - not needed for these tests
		nil, // transactionMonitorService - not needed for these tests
		nil, // merchantRepo - not needed for these tests
		nil, // logger
	)

	ctx := context.Background()
	webhookID := "webhook-123"
	itemID := "item-not-found"

	webhookEvent := &domain.WebhookEvent{
		ID:          webhookID,
		WebhookType: "TRANSACTIONS",
		ItemID:      &itemID,
		Processed:   false,
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	}

	// Mock processing started
	mockWebhookRepo.On("Update", ctx, mock.MatchedBy(func(e *domain.WebhookEvent) bool {
		return e.ID == webhookID && e.ProcessingStartedAt != nil
	})).Return(nil).Once()

	// Mock linked item not found
	mockLinkedItemRepo.On("GetByItemID", ctx, itemID).Return(nil, errors.New("linked item not found"))

	// Mock update with error
	mockWebhookRepo.On("Update", ctx, mock.MatchedBy(func(e *domain.WebhookEvent) bool {
		return e.ID == webhookID && e.ProcessingError != nil && !e.Processed
	})).Return(nil).Once()

	// Execute
	err := service.ProcessWebhook(ctx, webhookEvent)

	// Assert
	assert.Error(t, err)
	mockWebhookRepo.AssertExpectations(t)
	mockLinkedItemRepo.AssertExpectations(t)
}

// ============================================================================
// PHASE 7: Unit Tests for SyncTransactions
// Issue #260: Plaid TransactionsSync API Integration
// ============================================================================

// TestSyncTransactions_SuccessSinglePage tests successful sync with single page (has_more=false)
func TestSyncTransactions_SuccessSinglePage(t *testing.T) {
	// Setup mocks
	mockLinkedItemRepo := new(MockLinkedItemRepository)
	mockTransactionRepo := new(MockTransactionRepository)
	mockAccountRepo := new(MockAccountRepository)
	mockPlaidService := new(MockPlaidService)
	service := NewTransactionSyncService(
		nil, // webhookRepo not needed for SyncTransactions
		mockLinkedItemRepo,
		mockTransactionRepo,
		mockAccountRepo,
		nil, // profileRepo - not needed for these tests
		nil, // balanceSnapshotRepo - not needed for these tests
		mockPlaidService,
		nil, // subscriptionService - not needed for these tests
		nil, // ruleApplicationService - not needed for these tests
		nil, // budgetSyncService - not needed for these tests
		nil, // transactionMonitorService - not needed for these tests
		nil, // merchantRepo - not needed for these tests
		nil, // logger
	)

	ctx := context.Background()
	linkedItem := &domain.LinkedItem{
		ID:          "linked-item-123",
		UserID:      "user-123",
		ItemID:      "item-abc",
		AccessToken: "decrypted-token",
		Cursor:      stringPtr("cursor-before"),
	}

	// Mock Plaid API response (single page, has_more=false)
	plaidResp := &plaid.TransactionsSyncResponse{
		Added:      []plaid.Transaction{{TransactionId: "txn-1", Amount: 10.0}, {TransactionId: "txn-2", Amount: 20.0}},
		Modified:   []plaid.Transaction{},
		Removed:    []plaid.RemovedTransaction{},
		NextCursor: "cursor-after",
		HasMore:    false,
	}
	mockPlaidService.On("TransactionsSync", ctx, "decrypted-token", "cursor-before", 0).Return(plaidResp, nil)

	// Mock cursor update
	mockLinkedItemRepo.On("UpdateCursor", ctx, linkedItem.ID, "cursor-after").Return(nil)

	// Issue #261: Mock account repository for transaction storage
	mockAccountRepo.On("GetByLinkedItemIDIncludingRetained", ctx, linkedItem.ID).Return([]domain.Account{}, nil)
	// All transactions will be skipped since no accounts exist

	// Execute
	result, err := service.SyncTransactions(ctx, linkedItem)

	// Assert
	assert.NoError(t, err)
	assert.NotNil(t, result)
	assert.Len(t, result.Added, 2)
	assert.Len(t, result.Modified, 0)
	assert.Len(t, result.Removed, 0)
	assert.Equal(t, "cursor-after", result.NewCursor)

	mockPlaidService.AssertExpectations(t)
	mockLinkedItemRepo.AssertExpectations(t)
}

// TestSyncTransactions_SuccessMultiplePages tests pagination with has_more=true
func TestSyncTransactions_SuccessMultiplePages(t *testing.T) {
	mockLinkedItemRepo := new(MockLinkedItemRepository)
	mockTransactionRepo := new(MockTransactionRepository)
	mockAccountRepo := new(MockAccountRepository)
	mockPlaidService := new(MockPlaidService)
	service := NewTransactionSyncService(
		nil,
		mockLinkedItemRepo,
		mockTransactionRepo,
		mockAccountRepo,
		nil, // profileRepo - not needed for these tests
		nil, // balanceSnapshotRepo - not needed for these tests
		mockPlaidService,
		nil, // subscriptionService - not needed for these tests
		nil, // ruleApplicationService - not needed for these tests
		nil, // budgetSyncService - not needed for these tests
		nil, // transactionMonitorService - not needed for these tests
		nil, // merchantRepo - not needed for these tests
		nil, // logger
	)

	ctx := context.Background()
	linkedItem := &domain.LinkedItem{
		ID:          "linked-item-123",
		AccessToken: "decrypted-token",
		Cursor:      stringPtr("cursor-page0"),
	}

	// Page 1: has_more=true
	plaidResp1 := &plaid.TransactionsSyncResponse{
		Added:      []plaid.Transaction{{TransactionId: "txn-1"}},
		Modified:   []plaid.Transaction{{TransactionId: "txn-mod-1"}},
		Removed:    []plaid.RemovedTransaction{{TransactionId: stringPtr("txn-rem-1")}},
		NextCursor: "cursor-page1",
		HasMore:    true,
	}
	mockPlaidService.On("TransactionsSync", ctx, "decrypted-token", "cursor-page0", 0).Return(plaidResp1, nil)
	// #1570: page 1's cursor is NOT persisted. The cursor advances once, after storage.

	// Page 2: has_more=false
	plaidResp2 := &plaid.TransactionsSyncResponse{
		Added:      []plaid.Transaction{{TransactionId: "txn-2"}},
		Modified:   []plaid.Transaction{},
		Removed:    []plaid.RemovedTransaction{},
		NextCursor: "cursor-page2",
		HasMore:    false,
	}
	mockPlaidService.On("TransactionsSync", ctx, "decrypted-token", "cursor-page1", 0).Return(plaidResp2, nil)
	mockLinkedItemRepo.On("UpdateCursor", ctx, linkedItem.ID, "cursor-page2").Return(nil).Once()

	// Issue #261: Mock account repository for transaction storage
	mockAccountRepo.On("GetByLinkedItemIDIncludingRetained", ctx, linkedItem.ID).Return([]domain.Account{}, nil)
	// Mock deletion of removed transaction
	mockTransactionRepo.On("TombstoneByExternalID", ctx, domain.SourcePlaid, "txn-rem-1", linkedItem.UserID, domain.TxnDeletedByPlaidSync).Return(domain.TxnDeletedProviderRemovedPosted, nil)
	// #1569 round 1: processModifiedTransactions now looks the row up BEFORE mapping, so this
	// lookup happens even for a transaction whose account cannot be mapped. Not found + not
	// mappable is the one remaining skip, which is the same outcome this test always asserted.
	mockTransactionRepo.On("GetByExternalID", ctx, domain.SourcePlaid, "txn-mod-1", linkedItem.UserID).
		Return(nil, repository.ErrTransactionNotFound)

	// Execute
	result, err := service.SyncTransactions(ctx, linkedItem)

	// Assert - aggregated results from both pages
	assert.NoError(t, err)
	assert.Len(t, result.Added, 2)    // txn-1 + txn-2
	assert.Len(t, result.Modified, 1) // txn-mod-1
	assert.Len(t, result.Removed, 1)  // txn-rem-1
	assert.Equal(t, "cursor-page2", result.NewCursor)

	// #1570: exactly ONE cursor write, carrying the final page's cursor. Persisting page 1's cursor
	// mid-loop is what used to strand page 1's rows behind an advanced cursor when storage failed.
	assert.Equal(t, []string{"cursor-page2"}, persistedCursors(mockLinkedItemRepo))
	mockLinkedItemRepo.AssertNotCalled(t, "UpdateCursor", ctx, linkedItem.ID, "cursor-page1")

	mockPlaidService.AssertExpectations(t)
	mockLinkedItemRepo.AssertExpectations(t)
}

// TestSyncTransactions_PlaidErrorFirstPage tests Plaid API error on first page
func TestSyncTransactions_PlaidErrorFirstPage(t *testing.T) {
	mockLinkedItemRepo := new(MockLinkedItemRepository)
	mockPlaidService := new(MockPlaidService)
	service := NewTransactionSyncService(
		nil,
		mockLinkedItemRepo,
		nil,
		nil,
		nil, // profileRepo - not needed for these tests
		nil, // balanceSnapshotRepo - not needed for these tests
		mockPlaidService,
		nil, // subscriptionService - not needed for these tests
		nil, // ruleApplicationService - not needed for these tests
		nil, // budgetSyncService - not needed for these tests
		nil, // transactionMonitorService - not needed for these tests
		nil, // merchantRepo - not needed for these tests
		nil, // logger
	)

	ctx := context.Background()
	cursor := "existing-cursor" // Set cursor to avoid first-sync path
	linkedItem := &domain.LinkedItem{
		ID:          "linked-item-123",
		AccessToken: "decrypted-token",
		Cursor:      &cursor, // Re-sync scenario to avoid sync limit checks
	}

	// Mock Plaid API error
	mockPlaidService.On("TransactionsSync", ctx, "decrypted-token", "existing-cursor", 0).
		Return(nil, errors.New("INVALID_ACCESS_TOKEN"))

	// Execute
	result, err := service.SyncTransactions(ctx, linkedItem)

	// Assert - returns empty result with error
	assert.Error(t, err)
	assert.NotNil(t, result) // Partial results returned
	assert.Len(t, result.Added, 0)
	assert.Contains(t, err.Error(), "sync failed on page 1")

	mockPlaidService.AssertExpectations(t)
}

// TestSyncTransactions_PlaidErrorSubsequentPage tests Plaid API error on page 2 (resumability)
func TestSyncTransactions_PlaidErrorSubsequentPage(t *testing.T) {
	mockLinkedItemRepo := new(MockLinkedItemRepository)
	mockPlaidService := new(MockPlaidService)
	service := NewTransactionSyncService(
		nil,
		mockLinkedItemRepo,
		nil,
		nil,
		nil, // profileRepo - not needed for these tests
		nil, // balanceSnapshotRepo - not needed for these tests
		mockPlaidService,
		nil, // subscriptionService - not needed for these tests
		nil, // ruleApplicationService - not needed for these tests
		nil, // budgetSyncService - not needed for these tests
		nil, // transactionMonitorService - not needed for these tests
		nil, // merchantRepo - not needed for these tests
		nil, // logger
	)

	ctx := context.Background()
	linkedItem := &domain.LinkedItem{
		ID:          "linked-item-123",
		AccessToken: "decrypted-token",
		Cursor:      stringPtr("cursor-start"),
	}

	// Page 1 succeeds
	plaidResp1 := &plaid.TransactionsSyncResponse{
		Added:      []plaid.Transaction{{TransactionId: "txn-1"}},
		NextCursor: "cursor-page1",
		HasMore:    true,
	}
	mockPlaidService.On("TransactionsSync", ctx, "decrypted-token", "cursor-start", 0).Return(plaidResp1, nil)
	mockLinkedItemRepo.On("UpdateCursor", mock.Anything, mock.Anything, mock.Anything).Return(nil).Maybe()

	// Page 2 fails
	mockPlaidService.On("TransactionsSync", ctx, "decrypted-token", "cursor-page1", 0).
		Return(nil, errors.New("RATE_LIMIT_EXCEEDED"))

	// Execute
	result, err := service.SyncTransactions(ctx, linkedItem)

	// Assert - returns partial results from page 1
	assert.Error(t, err)
	assert.NotNil(t, result)
	assert.Len(t, result.Added, 1) // Got page 1 data
	assert.Contains(t, err.Error(), "sync failed on page 2")

	// #1570: the cursor must NOT have moved. Page 1's rows were never stored (the worker discards
	// `result` on error), so persisting page 1's cursor would lose them permanently — Plaid does not
	// re-deliver past a stored cursor. Leaving it put means the retry re-fetches page 1 and page 2.
	assert.Empty(t, persistedCursors(mockLinkedItemRepo))
	mockLinkedItemRepo.AssertExpectations(t)
}

// TestSyncTransactions_CursorUpdateError tests cursor update failure.
//
// #1570: the cursor write now happens AFTER storage, so this exercises the one remaining failure
// mode of the boundary — rows are already durable and the cursor could not be advanced. The sync
// must report the error; the next run re-delivers those rows and CreateBatch upserts them.
func TestSyncTransactions_CursorUpdateError(t *testing.T) {
	mockLinkedItemRepo := new(MockLinkedItemRepository)
	mockPlaidService := new(MockPlaidService)
	mockAccountRepo := new(MockAccountRepository)
	mockTransactionRepo := new(MockTransactionRepository)
	service := NewTransactionSyncService(
		nil,
		mockLinkedItemRepo,
		mockTransactionRepo,
		mockAccountRepo,
		nil, // profileRepo - not needed for these tests
		nil, // balanceSnapshotRepo - not needed for these tests
		mockPlaidService,
		nil, // subscriptionService - not needed for these tests
		nil, // ruleApplicationService - not needed for these tests
		nil, // budgetSyncService - not needed for these tests
		nil, // transactionMonitorService - not needed for these tests
		nil, // merchantRepo - not needed for these tests
		nil, // logger
	)

	ctx := context.Background()
	existingCursor := "existing-cursor"
	linkedItem := &domain.LinkedItem{
		ID:          "linked-item-123",
		AccessToken: "decrypted-token",
		Cursor:      &existingCursor,
	}

	// Mock Plaid API success
	plaidResp := &plaid.TransactionsSyncResponse{
		Added:      []plaid.Transaction{{TransactionId: "txn-1", AccountId: "plaid-acc-1", Amount: 5.00, Date: "2024-03-01", Name: "Stored"}},
		NextCursor: "cursor-new",
		HasMore:    false,
	}
	mockPlaidService.On("TransactionsSync", ctx, "decrypted-token", "existing-cursor", 0).Return(plaidResp, nil)

	// Storage runs first and genuinely COMMITS the row — that is the situation this test is about:
	// durable rows plus a cursor that could not be advanced. An empty account list would skip the
	// row and make the test vacuous.
	mockAccountRepo.On("GetByLinkedItemIDIncludingRetained", ctx, linkedItem.ID).
		Return([]domain.Account{{ID: "acc-1", PlaidAccountID: "plaid-acc-1", UserID: linkedItem.UserID}}, nil)
	mockTransactionRepo.On("CreateBatch", ctx, mock.Anything).Return(map[string]bool{"txn-1": true}, nil)

	// Cursor update fails
	mockLinkedItemRepo.On("UpdateCursor", ctx, linkedItem.ID, "cursor-new").
		Return(errors.New("database connection error"))

	// Execute
	result, err := service.SyncTransactions(ctx, linkedItem)

	// Assert - returns partial results with error
	assert.Error(t, err)
	assert.NotNil(t, result)
	assert.Len(t, result.Added, 1) // Got transaction data
	assert.Contains(t, err.Error(), "failed to update cursor after storage")

	// The row really was written before the cursor write failed — otherwise this test would be
	// exercising nothing. The next run re-delivers it and CreateBatch upserts it.
	assert.Equal(t, 1, storedExternalIDs(mockTransactionRepo)["txn-1"])

	mockLinkedItemRepo.AssertExpectations(t)
}

// TestSyncTransactions_EmptyCursor tests first sync with no cursor
func TestSyncTransactions_EmptyCursor(t *testing.T) {
	mockLinkedItemRepo := new(MockLinkedItemRepository)
	mockTransactionRepo := new(MockTransactionRepository)
	mockAccountRepo := new(MockAccountRepository)
	mockPlaidService := new(MockPlaidService)
	service := NewTransactionSyncService(
		nil,
		mockLinkedItemRepo,
		mockTransactionRepo,
		mockAccountRepo,
		nil, // profileRepo - not needed for these tests
		nil, // balanceSnapshotRepo - not needed for these tests
		mockPlaidService,
		nil, // subscriptionService - not needed for these tests
		nil, // ruleApplicationService - not needed for these tests
		nil, // budgetSyncService - not needed for these tests
		nil, // transactionMonitorService - not needed for these tests
		nil, // merchantRepo - not needed for these tests
		nil, // logger
	)

	ctx := context.Background()
	existingCursor := "existing-cursor"
	linkedItem := &domain.LinkedItem{
		ID:          "linked-item-123",
		AccessToken: "decrypted-token",
		Cursor:      &existingCursor,
	}

	plaidResp := &plaid.TransactionsSyncResponse{
		Added:      []plaid.Transaction{{TransactionId: "txn-1"}},
		NextCursor: "cursor-initial",
		HasMore:    false,
	}
	mockPlaidService.On("TransactionsSync", ctx, "decrypted-token", "existing-cursor", 0).Return(plaidResp, nil)
	mockLinkedItemRepo.On("UpdateCursor", ctx, linkedItem.ID, "cursor-initial").Return(nil)

	// Issue #261: Mock account repository for transaction storage
	mockAccountRepo.On("GetByLinkedItemIDIncludingRetained", ctx, linkedItem.ID).Return([]domain.Account{}, nil)

	// Execute
	result, err := service.SyncTransactions(ctx, linkedItem)

	// Assert
	assert.NoError(t, err)
	assert.NotNil(t, result)
	assert.Len(t, result.Added, 1)
	assert.Equal(t, "cursor-initial", result.NewCursor)

	mockPlaidService.AssertExpectations(t)
}

// ============================================================================
// Issue #261: Transaction Storage Logic Tests
// ============================================================================

// TestBuildAccountMap_Success tests building account map with multiple accounts
func TestBuildAccountMap_Success(t *testing.T) {
	// Setup
	mockAccountRepo := new(MockAccountRepository)
	mockLinkedItemRepo := new(MockLinkedItemRepository)

	service := NewTransactionSyncService(
		nil,
		mockLinkedItemRepo,
		nil,
		mockAccountRepo,
		nil, // profileRepo
		nil, // balanceSnapshotRepo
		nil, // plaidService
		nil, // subscriptionService
		nil, // ruleApplicationService
		nil, // budgetSyncService
		nil, // transactionMonitorService
		nil, // merchantRepo
		nil, // logger
	).(*transactionSyncService) // Type assert to access private method

	ctx := context.Background()
	linkedItemID := "linked-item-123"

	// Mock: GetByLinkedItemID returns 3 accounts
	accounts := []domain.Account{
		{
			ID:             "account-uuid-1",
			PlaidAccountID: "plaid-acc-1",
			Name:           "Checking",
		},
		{
			ID:             "account-uuid-2",
			PlaidAccountID: "plaid-acc-2",
			Name:           "Savings",
		},
		{
			ID:             "account-uuid-3",
			PlaidAccountID: "plaid-acc-3",
			Name:           "Credit Card",
		},
	}
	mockAccountRepo.On("GetByLinkedItemIDIncludingRetained", ctx, linkedItemID).Return(accounts, nil)

	// Execute
	accountMap, _, err := service.buildAccountMaps(ctx, linkedItemID, "")

	// Assert
	assert.NoError(t, err)
	assert.NotNil(t, accountMap)
	assert.Len(t, accountMap, 3)
	assert.Equal(t, "account-uuid-1", accountMap["plaid-acc-1"])
	assert.Equal(t, "account-uuid-2", accountMap["plaid-acc-2"])
	assert.Equal(t, "account-uuid-3", accountMap["plaid-acc-3"])

	mockAccountRepo.AssertExpectations(t)
}

// TestBuildAccountMap_EmptyAccounts tests handling when no accounts exist
func TestBuildAccountMap_EmptyAccounts(t *testing.T) {
	// Setup
	mockAccountRepo := new(MockAccountRepository)

	service := NewTransactionSyncService(
		nil,
		nil,
		nil,
		mockAccountRepo,
		nil, // profileRepo
		nil, // balanceSnapshotRepo
		nil, // plaidService
		nil, // subscriptionService
		nil, // ruleApplicationService
		nil, // budgetSyncService
		nil, // transactionMonitorService
		nil, // merchantRepo
		nil, // logger
	).(*transactionSyncService)

	ctx := context.Background()
	linkedItemID := "linked-item-123"

	// Mock: GetByLinkedItemID returns empty slice
	mockAccountRepo.On("GetByLinkedItemIDIncludingRetained", ctx, linkedItemID).Return([]domain.Account{}, nil)

	// Execute
	accountMap, _, err := service.buildAccountMaps(ctx, linkedItemID, "")

	// Assert
	assert.NoError(t, err)
	assert.NotNil(t, accountMap)
	assert.Len(t, accountMap, 0) // Empty map, not nil

	mockAccountRepo.AssertExpectations(t)
}

// TestBuildAccountMap_RepositoryError tests error handling
func TestBuildAccountMap_RepositoryError(t *testing.T) {
	// Setup
	mockAccountRepo := new(MockAccountRepository)

	service := NewTransactionSyncService(
		nil,
		nil,
		nil,
		mockAccountRepo,
		nil, // profileRepo
		nil, // balanceSnapshotRepo
		nil, // plaidService
		nil, // subscriptionService
		nil, // ruleApplicationService
		nil, // budgetSyncService
		nil, // transactionMonitorService
		nil, // merchantRepo
		nil, // logger
	).(*transactionSyncService)

	ctx := context.Background()
	linkedItemID := "linked-item-123"

	// Mock: GetByLinkedItemID returns error
	mockAccountRepo.On("GetByLinkedItemIDIncludingRetained", ctx, linkedItemID).Return(nil, errors.New("database error"))

	// Execute
	accountMap, _, err := service.buildAccountMaps(ctx, linkedItemID, "")

	// Assert
	assert.Error(t, err)
	assert.Nil(t, accountMap)
	assert.Contains(t, err.Error(), "failed to load accounts")

	mockAccountRepo.AssertExpectations(t)
}

// ============================================================================
// Issue #261: processAddedTransactions Tests (TDD RED Phase)
// ============================================================================

// TestProcessAddedTransactions_Success tests processing added transactions with all accounts mapped
func TestProcessAddedTransactions_Success(t *testing.T) {
	// Setup
	mockTransactionRepo := new(MockTransactionRepository)
	mockAccountRepo := new(MockAccountRepository)

	service := NewTransactionSyncService(
		nil,
		nil,
		mockTransactionRepo,
		mockAccountRepo,
		nil, // profileRepo
		nil, // balanceSnapshotRepo
		nil, // plaidService
		nil, // subscriptionService
		nil, // ruleApplicationService
		nil, // budgetSyncService
		nil, // transactionMonitorService
		nil, // merchantRepo
		nil, // logger
	).(*transactionSyncService)

	ctx := context.Background()
	linkedItemID := "linked-item-123"
	userID := "user-456"

	// Account map: Plaid account_id -> Internal UUID
	accountMap := map[string]string{
		"plaid-acc-1": "account-uuid-1",
		"plaid-acc-2": "account-uuid-2",
		"plaid-acc-3": "account-uuid-3",
	}

	// Mock linked item
	linkedItem := &domain.LinkedItem{ID: linkedItemID, UserID: userID}

	// 3 Plaid transactions to add
	added := []plaid.Transaction{
		{TransactionId: "plaid-txn-1", AccountId: "plaid-acc-1", Amount: 100.00, Date: "2024-01-15", Name: "Transaction 1", Pending: false},
		{TransactionId: "plaid-txn-2", AccountId: "plaid-acc-2", Amount: 50.00, Date: "2024-01-16", Name: "Transaction 2", Pending: false},
		{TransactionId: "plaid-txn-3", AccountId: "plaid-acc-3", Amount: 25.00, Date: "2024-01-17", Name: "Transaction 3", Pending: true},
	}

	// Mock: CreateBatch should be called with 3 transactions
	mockTransactionRepo.On("CreateBatch", ctx, mock.MatchedBy(func(txns []domain.Transaction) bool {
		// Verify all 3 transactions are mapped correctly
		if len(txns) != 3 {
			return false
		}
		// Check first transaction
		if txns[0].ExternalTransactionID == nil || *txns[0].ExternalTransactionID != "plaid-txn-1" {
			return false
		}
		if txns[0].AccountID != "account-uuid-1" {
			return false
		}
		if txns[0].Amount != 100.00 {
			return false
		}
		// Check second transaction
		if txns[1].AccountID != "account-uuid-2" {
			return false
		}
		// Check third transaction is pending
		if !txns[2].Pending {
			return false
		}
		return true
	})).Return(nil, nil)

	// Execute
	err := service.processAddedTransactions(ctx, added, linkedItem, accountMap, nil)

	// Assert
	assert.NoError(t, err)
	mockTransactionRepo.AssertExpectations(t)
}

// TestProcessAddedTransactions_Deduplication tests that duplicate transactions are handled by DB constraint
func TestProcessAddedTransactions_Deduplication(t *testing.T) {
	// Setup
	mockTransactionRepo := new(MockTransactionRepository)

	service := NewTransactionSyncService(
		nil,
		nil,
		mockTransactionRepo,
		nil,
		nil, // profileRepo
		nil, // balanceSnapshotRepo
		nil, // plaidService
		nil, // subscriptionService
		nil, // ruleApplicationService
		nil, // budgetSyncService
		nil, // transactionMonitorService
		nil, // merchantRepo
		nil, // logger
	).(*transactionSyncService)

	ctx := context.Background()
	linkedItemID := "linked-item-123"
	userID := "user-456"

	accountMap := map[string]string{
		"plaid-acc-1": "account-uuid-1",
	}

	linkedItem := &domain.LinkedItem{
		ID:     linkedItemID,
		UserID: userID,
	}

	// Same transaction_id twice (Plaid API shouldn't do this, but if it does...)
	added := []plaid.Transaction{
		{
			TransactionId: "plaid-txn-duplicate",
			AccountId:     "plaid-acc-1",
			Amount:        100.00,
			Date:          "2024-01-15",
			Name:          "Transaction 1",
		},
		{
			TransactionId: "plaid-txn-duplicate", // DUPLICATE
			AccountId:     "plaid-acc-1",
			Amount:        100.00,
			Date:          "2024-01-15",
			Name:          "Transaction 1",
		},
	}

	// Mock: CreateBatch with ON CONFLICT DO NOTHING handles deduplication
	// Both transactions will be passed, database constraint handles it
	mockTransactionRepo.On("CreateBatch", ctx, mock.MatchedBy(func(txns []domain.Transaction) bool {
		return len(txns) == 2 // Both passed to DB, constraint handles dedup
	})).Return(nil, nil)

	// Execute
	err := service.processAddedTransactions(ctx, added, linkedItem, accountMap, nil)

	// Assert
	assert.NoError(t, err)
	mockTransactionRepo.AssertExpectations(t)
}

// TestProcessAddedTransactions_PartialFailure tests graceful error handling when some accounts missing
func TestProcessAddedTransactions_PartialFailure(t *testing.T) {
	// Setup
	mockTransactionRepo := new(MockTransactionRepository)

	service := NewTransactionSyncService(
		nil,
		nil,
		mockTransactionRepo,
		nil,
		nil, // profileRepo
		nil, // balanceSnapshotRepo
		nil, // plaidService
		nil, // subscriptionService
		nil, // ruleApplicationService
		nil, // budgetSyncService
		nil, // transactionMonitorService
		nil, // merchantRepo
		nil, // logger
	).(*transactionSyncService)

	ctx := context.Background()
	linkedItemID := "linked-item-123"
	userID := "user-456"

	// Account map missing plaid-acc-3
	accountMap := map[string]string{
		"plaid-acc-1": "account-uuid-1",
		"plaid-acc-2": "account-uuid-2",
		// plaid-acc-3 NOT in map
	}

	linkedItem := &domain.LinkedItem{
		ID:     linkedItemID,
		UserID: userID,
	}

	// 3 transactions, but 3rd has unmapped account
	added := []plaid.Transaction{
		{
			TransactionId: "plaid-txn-1",
			AccountId:     "plaid-acc-1",
			Amount:        100.00,
			Date:          "2024-01-15",
			Name:          "Transaction 1",
		},
		{
			TransactionId: "plaid-txn-2",
			AccountId:     "plaid-acc-2",
			Amount:        50.00,
			Date:          "2024-01-16",
			Name:          "Transaction 2",
		},
		{
			TransactionId: "plaid-txn-3",
			AccountId:     "plaid-acc-3", // MISSING FROM MAP
			Amount:        25.00,
			Date:          "2024-01-17",
			Name:          "Transaction 3",
		},
	}

	// Mock: CreateBatch should only receive 2 transactions (3rd skipped due to missing account)
	mockTransactionRepo.On("CreateBatch", ctx, mock.MatchedBy(func(txns []domain.Transaction) bool {
		return len(txns) == 2 // Only first 2 transactions
	})).Return(nil, nil)

	// Execute
	err := service.processAddedTransactions(ctx, added, linkedItem, accountMap, nil)

	// Assert - should succeed but log warning for skipped transaction
	assert.NoError(t, err) // Continue processing despite skipping one
	mockTransactionRepo.AssertExpectations(t)
}

// TestProcessAddedTransactions_EmptySlice tests handling of empty input
func TestProcessAddedTransactions_EmptySlice(t *testing.T) {
	// Setup
	mockTransactionRepo := new(MockTransactionRepository)

	service := NewTransactionSyncService(
		nil,
		nil,
		mockTransactionRepo,
		nil,
		nil, // profileRepo
		nil, // balanceSnapshotRepo
		nil, // plaidService
		nil, // subscriptionService
		nil, // ruleApplicationService
		nil, // budgetSyncService
		nil, // transactionMonitorService
		nil, // merchantRepo
		nil, // logger
	).(*transactionSyncService)

	ctx := context.Background()
	linkedItem := &domain.LinkedItem{
		ID:     "linked-item-123",
		UserID: "user-456",
	}
	accountMap := map[string]string{}

	// Empty slice
	added := []plaid.Transaction{}

	// Mock: CreateBatch should NOT be called for empty slice
	// (We'll optimize to skip DB call if no transactions)

	// Execute
	err := service.processAddedTransactions(ctx, added, linkedItem, accountMap, nil)

	// Assert
	assert.NoError(t, err)
	// CreateBatch should NOT have been called
	mockTransactionRepo.AssertNotCalled(t, "CreateBatch")
}

// ============================================================================
// Issue #261: processModifiedTransactions Tests (TDD RED Phase)
// ============================================================================

// TestProcessModifiedTransactions_Success tests updating existing transactions while preserving user_notes
func TestProcessModifiedTransactions_Success(t *testing.T) {
	// Setup
	mockTransactionRepo := new(MockTransactionRepository)

	service := NewTransactionSyncService(
		nil,
		nil,
		mockTransactionRepo,
		nil,
		nil, // profileRepo
		nil, // balanceSnapshotRepo
		nil, // plaidService
		nil, // subscriptionService
		nil, // ruleApplicationService
		nil, // budgetSyncService
		nil, // transactionMonitorService
		nil, // merchantRepo
		nil, // logger
	).(*transactionSyncService)

	ctx := context.Background()
	linkedItemID := "linked-item-123"
	userID := "user-456"

	accountMap := map[string]string{
		"plaid-acc-1": "account-uuid-1",
		"plaid-acc-2": "account-uuid-2",
	}

	linkedItem := &domain.LinkedItem{
		ID:     linkedItemID,
		UserID: userID,
	}

	// 2 modified transactions from Plaid
	modified := []plaid.Transaction{
		{TransactionId: "plaid-txn-1", AccountId: "plaid-acc-1", Amount: 150.00, Date: "2024-01-15", Name: "Transaction 1 Updated", Pending: false},
		{TransactionId: "plaid-txn-2", AccountId: "plaid-acc-2", Amount: 75.00, Date: "2024-01-16", Name: "Transaction 2 Updated", Pending: true},
	}

	// Mock: GetByExternalID returns existing transactions WITH user_notes
	existingUserNotes1 := "My custom note 1"
	existingUserNotes2 := "My custom note 2"
	externalID1 := "plaid-txn-1"
	externalID2 := "plaid-txn-2"

	existingTxn1 := &domain.Transaction{
		ID:                    "txn-uuid-1",
		UserID:                userID,
		AccountID:             "account-uuid-1",
		ExternalTransactionID: &externalID1,
		Amount:                100.00, // Old amount
		Name:                  "Transaction 1",
		UserNotes:             &existingUserNotes1, // USER'S CUSTOM NOTE
	}
	existingTxn2 := &domain.Transaction{
		ID:                    "txn-uuid-2",
		UserID:                userID,
		AccountID:             "account-uuid-2",
		ExternalTransactionID: &externalID2,
		Amount:                50.00, // Old amount
		Name:                  "Transaction 2",
		UserNotes:             &existingUserNotes2, // USER'S CUSTOM NOTE
	}

	mockTransactionRepo.On("GetByExternalID", ctx, domain.SourcePlaid, "plaid-txn-1", userID).Return(existingTxn1, nil)
	mockTransactionRepo.On("GetByExternalID", ctx, domain.SourcePlaid, "plaid-txn-2", userID).Return(existingTxn2, nil)

	// Mock: Update should be called with new data BUT old user_notes preserved
	mockTransactionRepo.On("Update", ctx, mock.MatchedBy(func(txn *domain.Transaction) bool {
		// Transaction 1: Amount updated, user_notes preserved
		if txn.ID == "txn-uuid-1" {
			return txn.Amount == 150.00 &&
				txn.Name == "Transaction 1 Updated" &&
				txn.UserNotes != nil &&
				*txn.UserNotes == "My custom note 1" // PRESERVED!
		}
		return false
	})).Return(nil).Once()

	mockTransactionRepo.On("Update", ctx, mock.MatchedBy(func(txn *domain.Transaction) bool {
		// Transaction 2: Amount updated, user_notes preserved
		if txn.ID == "txn-uuid-2" {
			return txn.Amount == 75.00 &&
				txn.Pending == true &&
				txn.UserNotes != nil &&
				*txn.UserNotes == "My custom note 2" // PRESERVED!
		}
		return false
	})).Return(nil).Once()

	// Execute
	err := service.processModifiedTransactions(ctx, modified, linkedItem, accountMap, nil)

	// Assert
	assert.NoError(t, err)
	mockTransactionRepo.AssertExpectations(t)
}

// TestProcessModifiedTransactions_NewTransactionNoUserNotes tests transaction with nil user_notes
func TestProcessModifiedTransactions_NewTransactionNoUserNotes(t *testing.T) {
	// Setup
	mockTransactionRepo := new(MockTransactionRepository)

	service := NewTransactionSyncService(
		nil,
		nil,
		mockTransactionRepo,
		nil,
		nil, // profileRepo
		nil, // balanceSnapshotRepo
		nil, // plaidService
		nil, // subscriptionService
		nil, // ruleApplicationService
		nil, // budgetSyncService
		nil, // transactionMonitorService
		nil, // merchantRepo
		nil, // logger
	).(*transactionSyncService)

	ctx := context.Background()
	linkedItemID := "linked-item-123"
	userID := "user-456"

	accountMap := map[string]string{
		"plaid-acc-1": "account-uuid-1",
	}

	linkedItem := &domain.LinkedItem{
		ID:     linkedItemID,
		UserID: userID,
	}

	modified := []plaid.Transaction{
		{
			TransactionId: "plaid-txn-1",
			AccountId:     "plaid-acc-1",
			Amount:        150.00,
			Date:          "2024-01-15",
			Name:          "Transaction 1",
		},
	}

	// Existing transaction has NO user_notes
	externalID := "plaid-txn-1"
	existingTxn := &domain.Transaction{
		ID:                    "txn-uuid-1",
		UserID:                userID,
		AccountID:             "account-uuid-1",
		ExternalTransactionID: &externalID,
		Amount:                100.00,
		Name:                  "Transaction 1",
		UserNotes:             nil, // NO user notes
	}

	mockTransactionRepo.On("GetByExternalID", ctx, domain.SourcePlaid, "plaid-txn-1", userID).Return(existingTxn, nil)

	// Mock: Update with nil user_notes (unchanged)
	mockTransactionRepo.On("Update", ctx, mock.MatchedBy(func(txn *domain.Transaction) bool {
		return txn.ID == "txn-uuid-1" && txn.UserNotes == nil
	})).Return(nil)

	// Execute
	err := service.processModifiedTransactions(ctx, modified, linkedItem, accountMap, nil)

	// Assert
	assert.NoError(t, err)
	mockTransactionRepo.AssertExpectations(t)
}

// TestProcessModifiedTransactions_TransactionNotFound pins #1569 residual (3): a modification whose
// row is not in the database is UPSERTED, not dropped.
//
// It used to be Warn'd and skipped, and the cursor then advanced past it, so the edit was lost for
// good — Plaid does not re-deliver a modification the stored cursor is past. A missing row is
// evidence an earlier sync failed to store it (before #1570 moved the cursor write after storage, a
// partial page failure advanced the cursor over rows that were never written), not evidence the
// transaction is not ours.
func TestProcessModifiedTransactions_TransactionNotFound(t *testing.T) {
	// Setup
	mockTransactionRepo := new(MockTransactionRepository)

	service := NewTransactionSyncService(
		nil,
		nil,
		mockTransactionRepo,
		nil,
		nil, // profileRepo
		nil, // balanceSnapshotRepo
		nil, // plaidService
		nil, // subscriptionService
		nil, // ruleApplicationService
		nil, // budgetSyncService
		nil, // transactionMonitorService
		nil, // merchantRepo
		nil, // logger
	).(*transactionSyncService)

	ctx := context.Background()
	linkedItemID := "linked-item-123"
	userID := "user-456"

	accountMap := map[string]string{
		"plaid-acc-1": "account-uuid-1",
	}

	linkedItem := &domain.LinkedItem{
		ID:     linkedItemID,
		UserID: userID,
	}

	modified := []plaid.Transaction{
		{
			TransactionId: "plaid-txn-new",
			AccountId:     "plaid-acc-1",
			Amount:        150.00,
			Date:          "2024-01-15",
			Name:          "New Transaction",
		},
	}

	mockTransactionRepo.On("GetByExternalID", ctx, domain.SourcePlaid, "plaid-txn-new", userID).
		Return(nil, repository.ErrTransactionNotFound)

	var upserted []domain.Transaction
	mockTransactionRepo.On("CreateBatch", ctx, mock.Anything).
		Run(func(args mock.Arguments) {
			upserted = args.Get(1).([]domain.Transaction)
		}).
		Return(map[string]bool{}, nil).Once()

	err := service.processModifiedTransactions(ctx, modified, linkedItem, accountMap, nil)

	assert.NoError(t, err)
	assertNoCallTo(t, &mockTransactionRepo.Mock, "Update")
	require.Len(t, upserted, 1, "the modification must reach the database as an upsert, not be dropped")
	assert.Equal(t, "plaid-txn-new", *upserted[0].ExternalTransactionID)
	assert.NotEmpty(t, upserted[0].ID,
		"CreateBatch keys BatchRuleApplication records off pre-assigned IDs, so the upsert must carry one")
	mockTransactionRepo.AssertExpectations(t)
}

// TestProcessModifiedTransactions_UnmappableAccountButStored_StillApplies pins the round-1 fix to
// the OTHER log-and-continue path in this function.
//
// Before it, mapPlaidTransaction ran first, and an account_id missing from accountMap discarded the
// modification before anything checked whether the transaction was already stored — Warn, skip,
// return nil, cursor advances, edit gone. That is residual (3)'s exact failure mode reached one
// branch earlier, and it was missed because the contract comment's enumeration named only
// processAddedTransactions.
//
// The stored row already records account_id / linked_item_id / user_account_id, so the payload is
// remapped and that identity re-attached. The user's notes still survive, as on every other path.
func TestProcessModifiedTransactions_UnmappableAccountButStored_StillApplies(t *testing.T) {
	mockTransactionRepo := new(MockTransactionRepository)

	service := NewTransactionSyncService(
		nil, nil, mockTransactionRepo, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil,
	).(*transactionSyncService)

	ctx := context.Background()
	linkedItem := &domain.LinkedItem{ID: "linked-item-123", UserID: "user-456"}

	// accountMap deliberately does NOT contain the transaction's plaid account.
	accountMap := map[string]string{"plaid-acc-other": "account-uuid-other"}

	modified := []plaid.Transaction{{
		TransactionId: "plaid-txn-unmappable",
		AccountId:     "plaid-acc-vanished",
		Amount:        321.00,
		Date:          "2024-02-01",
		Name:          "Corrected Name",
	}}

	notes := "do not lose this"
	storedUserAccountID := "user-account-uuid-1"
	stored := &domain.Transaction{
		ID:            "stored-uuid",
		UserID:        "user-456",
		AccountID:     "account-uuid-stored",
		LinkedItemID:  "linked-item-stored",
		UserAccountID: &storedUserAccountID,
		UserNotes:     &notes,
	}
	mockTransactionRepo.On("GetByExternalID", ctx, domain.SourcePlaid, "plaid-txn-unmappable", "user-456").
		Return(stored, nil)

	var updated *domain.Transaction
	mockTransactionRepo.On("Update", ctx, mock.Anything).
		Run(func(args mock.Arguments) { updated = args.Get(1).(*domain.Transaction) }).
		Return(nil).Once()

	err := service.processModifiedTransactions(ctx, modified, linkedItem, accountMap, nil)

	require.NoError(t, err)
	assertNoCallTo(t, &mockTransactionRepo.Mock, "CreateBatch")
	require.NotNil(t, updated, "the edit must be applied, not dropped, when we already hold the row")
	assert.Equal(t, "stored-uuid", updated.ID)
	assert.Equal(t, "Corrected Name", updated.Name, "the provider's new data must actually land")
	assert.Equal(t, "account-uuid-stored", updated.AccountID,
		"identity comes from the stored row when accountMap cannot supply it")
	assert.Equal(t, "linked-item-stored", updated.LinkedItemID)
	require.NotNil(t, updated.UserAccountID)
	assert.Equal(t, storedUserAccountID, *updated.UserAccountID)
	require.NotNil(t, updated.UserNotes)
	assert.Equal(t, notes, *updated.UserNotes, "user work survives this path like every other")
	mockTransactionRepo.AssertExpectations(t)
}

// TestProcessModifiedTransactions_UnmappableAndNotStored_IsTheOnlySkip is the boundary of the fix
// above. With nothing stored there is no account_id to insert under and no row to update, so this
// stays a log-and-continue — narrowed exactly as processAddedTransactions' equivalent is by task
// 1b's GetByLinkedItemIDIncludingRetained, and recorded as residual (3) in the
// #1570 STORAGE-BEFORE-CURSOR CONTRACT comment.
func TestProcessModifiedTransactions_UnmappableAndNotStored_IsTheOnlySkip(t *testing.T) {
	mockTransactionRepo := new(MockTransactionRepository)

	service := NewTransactionSyncService(
		nil, nil, mockTransactionRepo, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil,
	).(*transactionSyncService)

	ctx := context.Background()
	linkedItem := &domain.LinkedItem{ID: "linked-item-123", UserID: "user-456"}

	modified := []plaid.Transaction{{
		TransactionId: "plaid-txn-ghost",
		AccountId:     "plaid-acc-vanished",
		Amount:        1.00,
		Date:          "2024-02-01",
		Name:          "Ghost",
	}}

	mockTransactionRepo.On("GetByExternalID", ctx, domain.SourcePlaid, "plaid-txn-ghost", "user-456").
		Return(nil, repository.ErrTransactionNotFound)

	err := service.processModifiedTransactions(ctx, modified, linkedItem, map[string]string{}, nil)

	require.NoError(t, err, "this must not wedge the sync: re-delivery cannot conjure an account row")
	assertNoCallTo(t, &mockTransactionRepo.Mock, "Update")
	assertNoCallTo(t, &mockTransactionRepo.Mock, "CreateBatch")
}

// TestProcessModifiedTransactions_LookupFailureIsNotSwallowed separates residual (3)'s idempotent
// case from a real one. "Row not found" is a decision to upsert; a connection failure is not, and
// treating the two alike would let the cursor advance past a modification nobody applied — the same
// silent loss residual (3) exists to end, arriving through the error branch instead.
func TestProcessModifiedTransactions_LookupFailureIsNotSwallowed(t *testing.T) {
	mockTransactionRepo := new(MockTransactionRepository)

	service := NewTransactionSyncService(
		nil, nil, mockTransactionRepo, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil,
	).(*transactionSyncService)

	ctx := context.Background()
	linkedItem := &domain.LinkedItem{ID: "linked-item-123", UserID: "user-456"}
	accountMap := map[string]string{"plaid-acc-1": "account-uuid-1"}
	modified := []plaid.Transaction{{
		TransactionId: "plaid-txn-boom",
		AccountId:     "plaid-acc-1",
		Amount:        1.00,
		Date:          "2024-01-15",
		Name:          "Boom",
	}}

	mockTransactionRepo.On("GetByExternalID", ctx, domain.SourcePlaid, "plaid-txn-boom", "user-456").
		Return(nil, errors.New("connection reset by peer"))

	err := service.processModifiedTransactions(ctx, modified, linkedItem, accountMap, nil)

	require.Error(t, err, "a lookup failure is not the missing-row case and must abort the sync")
	assert.Contains(t, err.Error(), "connection reset by peer")
	assertNoCallTo(t, &mockTransactionRepo.Mock, "CreateBatch")
	assertNoCallTo(t, &mockTransactionRepo.Mock, "Update")
}

// TestProcessModifiedTransactions_EmptySlice tests handling of empty input
func TestProcessModifiedTransactions_EmptySlice(t *testing.T) {
	// Setup
	mockTransactionRepo := new(MockTransactionRepository)

	service := NewTransactionSyncService(
		nil,
		nil,
		mockTransactionRepo,
		nil,
		nil, // profileRepo
		nil, // balanceSnapshotRepo
		nil, // plaidService
		nil, // subscriptionService
		nil, // ruleApplicationService
		nil, // budgetSyncService
		nil, // transactionMonitorService
		nil, // merchantRepo
		nil, // logger
	).(*transactionSyncService)

	ctx := context.Background()
	linkedItem := &domain.LinkedItem{
		ID:     "linked-item-123",
		UserID: "user-456",
	}
	accountMap := map[string]string{}

	// Empty slice
	modified := []plaid.Transaction{}

	// Execute
	err := service.processModifiedTransactions(ctx, modified, linkedItem, accountMap, nil)

	// Assert
	assert.NoError(t, err)
	// No repository methods should be called
	mockTransactionRepo.AssertNotCalled(t, "GetByExternalID")
	mockTransactionRepo.AssertNotCalled(t, "Update")
}

// ============================================================================
// Issue #261: processRemovedTransactions Tests (TDD RED Phase)
// ============================================================================

// TestProcessRemovedTransactions_Success tests deleting removed transactions
func TestProcessRemovedTransactions_Success(t *testing.T) {
	// Setup
	mockTransactionRepo := new(MockTransactionRepository)

	service := NewTransactionSyncService(
		nil,
		nil,
		mockTransactionRepo,
		nil,
		nil, // profileRepo
		nil, // balanceSnapshotRepo
		nil, // plaidService
		nil, // subscriptionService
		nil, // ruleApplicationService
		nil, // budgetSyncService
		nil, // transactionMonitorService
		nil, // merchantRepo
		nil, // logger
	).(*transactionSyncService)

	ctx := context.Background()
	userID := "user-456"

	linkedItem := &domain.LinkedItem{
		ID:     "linked-item-123",
		UserID: userID,
	}

	// 3 removed transactions from Plaid
	id1, id2, id3 := "plaid-txn-removed-1", "plaid-txn-removed-2", "plaid-txn-removed-3"
	removed := []plaid.RemovedTransaction{
		{TransactionId: &id1},
		{TransactionId: &id2},
		{TransactionId: &id3},
	}

	// Mock: DeleteByExternalID called for each transaction
	mockTransactionRepo.On("TombstoneByExternalID", ctx, domain.SourcePlaid, "plaid-txn-removed-1", userID, domain.TxnDeletedByPlaidSync).Return(domain.TxnDeletedProviderRemovedPosted, nil)
	mockTransactionRepo.On("TombstoneByExternalID", ctx, domain.SourcePlaid, "plaid-txn-removed-2", userID, domain.TxnDeletedByPlaidSync).Return(domain.TxnDeletedProviderRemovedPosted, nil)
	mockTransactionRepo.On("TombstoneByExternalID", ctx, domain.SourcePlaid, "plaid-txn-removed-3", userID, domain.TxnDeletedByPlaidSync).Return(domain.TxnDeletedProviderRemovedPosted, nil)

	// Execute
	err := service.processRemovedTransactions(ctx, removed, linkedItem)

	// Assert
	assert.NoError(t, err)
	mockTransactionRepo.AssertExpectations(t)
}

// TestProcessRemovedTransactions_IdempotentNotFound tests idempotent deletion (transaction already deleted)
func TestProcessRemovedTransactions_IdempotentNotFound(t *testing.T) {
	// Setup
	mockTransactionRepo := new(MockTransactionRepository)

	service := NewTransactionSyncService(
		nil,
		nil,
		mockTransactionRepo,
		nil,
		nil, // profileRepo
		nil, // balanceSnapshotRepo
		nil, // plaidService
		nil, // subscriptionService
		nil, // ruleApplicationService
		nil, // budgetSyncService
		nil, // transactionMonitorService
		nil, // merchantRepo
		nil, // logger
	).(*transactionSyncService)

	ctx := context.Background()
	userID := "user-456"

	linkedItem := &domain.LinkedItem{
		ID:     "linked-item-123",
		UserID: userID,
	}

	txnID := "plaid-txn-not-found"
	removed := []plaid.RemovedTransaction{
		{TransactionId: &txnID},
	}

	// Mock: DeleteByExternalID returns "not found" error
	// This is OK - transaction already deleted (idempotent)
	mockTransactionRepo.On("TombstoneByExternalID", ctx, domain.SourcePlaid, "plaid-txn-not-found", userID, domain.TxnDeletedByPlaidSync).
		Return(domain.TransactionDeletedReason(""), repository.ErrTransactionNotFound)

	// Execute
	err := service.processRemovedTransactions(ctx, removed, linkedItem)

	// Assert - should succeed despite not-found (idempotent)
	assert.NoError(t, err)
	mockTransactionRepo.AssertExpectations(t)
}

// TestProcessRemovedTransactions_EmptySlice tests handling of empty input
func TestProcessRemovedTransactions_EmptySlice(t *testing.T) {
	// Setup
	mockTransactionRepo := new(MockTransactionRepository)

	service := NewTransactionSyncService(
		nil,
		nil,
		mockTransactionRepo,
		nil,
		nil, // profileRepo
		nil, // balanceSnapshotRepo
		nil, // plaidService
		nil, // subscriptionService
		nil, // ruleApplicationService
		nil, // budgetSyncService
		nil, // transactionMonitorService
		nil, // merchantRepo
		nil, // logger
	).(*transactionSyncService)

	ctx := context.Background()
	linkedItem := &domain.LinkedItem{
		ID:     "linked-item-123",
		UserID: "user-456",
	}

	// Empty slice
	removed := []plaid.RemovedTransaction{}

	// Execute
	err := service.processRemovedTransactions(ctx, removed, linkedItem)

	// Assert
	assert.NoError(t, err)
	// DeleteByExternalID should NOT be called
	assertNoCallTo(t, &mockTransactionRepo.Mock, "TombstoneByExternalID")
}

// TestProcessRemovedTransactions_NilTransactionID tests handling of nil transaction_id (should skip)
func TestProcessRemovedTransactions_NilTransactionID(t *testing.T) {
	// Setup
	mockTransactionRepo := new(MockTransactionRepository)

	service := NewTransactionSyncService(
		nil,
		nil,
		mockTransactionRepo,
		nil,
		nil, // profileRepo
		nil, // balanceSnapshotRepo
		nil, // plaidService
		nil, // subscriptionService
		nil, // ruleApplicationService
		nil, // budgetSyncService
		nil, // transactionMonitorService
		nil, // merchantRepo
		nil, // logger
	).(*transactionSyncService)

	ctx := context.Background()
	linkedItem := &domain.LinkedItem{
		ID:     "linked-item-123",
		UserID: "user-456",
	}

	// Transaction with nil ID (should skip)
	removed := []plaid.RemovedTransaction{{TransactionId: nil}}

	// Execute
	err := service.processRemovedTransactions(ctx, removed, linkedItem)

	// Assert - should succeed but skip nil transaction
	assert.NoError(t, err)
	// DeleteByExternalID should NOT be called for nil ID
	assertNoCallTo(t, &mockTransactionRepo.Mock, "TombstoneByExternalID")
}

// ============================================================================
// Issue #331: E2E Migration - Pending to Posted Transition Tests
// ============================================================================
// Tests migrated from YAML scenario #6: pending-to-posted.yaml
// Verifies that transactions correctly transition from pending to posted status
// while preserving user data and handling amount/date changes.

// TestProcessModifiedTransactions_PendingToPosted tests the complete pending→posted transition
// This test validates:
// 1. Single transaction (no duplication on status change)
// 2. Pending status change (true → false)
// 3. Amount change (tip added to restaurant charge)
// 4. Date change (hotel checkout date vs reservation date)
// 5. User notes preserved during transition
//
// Scenario: User has pending transactions that finalize (post/settle) with updates
func TestProcessModifiedTransactions_PendingToPosted(t *testing.T) {
	// Setup
	mockTransactionRepo := new(MockTransactionRepository)

	service := NewTransactionSyncService(
		nil,
		nil,
		mockTransactionRepo,
		nil,
		nil, // profileRepo
		nil, // balanceSnapshotRepo
		nil, // plaidService
		nil, // subscriptionService
		nil, // ruleApplicationService
		nil, // budgetSyncService
		nil, // transactionMonitorService
		nil, // merchantRepo
		nil, // logger
	).(*transactionSyncService)

	ctx := context.Background()
	linkedItemID := "linked-item-123"
	userID := "user-456"

	accountMap := map[string]string{
		"plaid-acc-restaurant": "account-uuid-restaurant",
		"plaid-acc-hotel":      "account-uuid-hotel",
		"plaid-acc-gas":        "account-uuid-gas",
	}

	linkedItem := &domain.LinkedItem{
		ID:     linkedItemID,
		UserID: userID,
	}

	// Existing pending transactions in database
	notesRestaurant, notesHotel := "Team lunch - add tip later", "Business trip hotel"
	existingPendingRestaurant := &domain.Transaction{
		ID: "txn-pending-restaurant", UserID: userID, AccountID: "account-uuid-restaurant",
		LinkedItemID: linkedItemID, ExternalTransactionID: strPtrHelper("plaid-txn-restaurant"),
		Amount: 45.00, Pending: true, UserNotes: &notesRestaurant,
	}
	existingPendingHotel := &domain.Transaction{
		ID: "txn-pending-hotel", UserID: userID, AccountID: "account-uuid-hotel",
		LinkedItemID: linkedItemID, ExternalTransactionID: strPtrHelper("plaid-txn-hotel"),
		Amount: 200.00, Pending: true, UserNotes: &notesHotel,
	}
	existingPendingGas := &domain.Transaction{
		ID: "txn-pending-gas", UserID: userID, AccountID: "account-uuid-gas",
		LinkedItemID: linkedItemID, ExternalTransactionID: strPtrHelper("plaid-txn-gas"),
		Amount: 55.00, Pending: true, UserNotes: nil,
	}

	// Modified transactions from Plaid (posted versions)
	merchantRestaurant := "Olive Garden"
	merchantHotel := "Marriott"
	merchantGas := "Shell"
	modified := []plaid.Transaction{
		// Restaurant: Amount changed (tip added), pending→false, notes MUST be preserved
		{
			TransactionId: "plaid-txn-restaurant",
			AccountId:     "plaid-acc-restaurant",
			Amount:        54.50, // Tip added: $45 + $9.50 tip
			Date:          "2024-01-14",
			Name:          "Restaurant",
			MerchantName:  *plaid.NewNullableString(&merchantRestaurant),
			Pending:       false, // Now posted
		},
		// Hotel: Date changed (checkout date vs reservation date), pending→false, notes preserved
		{
			TransactionId: "plaid-txn-hotel",
			AccountId:     "plaid-acc-hotel",
			Amount:        200.00,       // Amount unchanged
			Date:          "2024-01-16", // Posted on checkout (was 2024-01-13 reservation)
			Name:          "Hotel Reservation",
			MerchantName:  *plaid.NewNullableString(&merchantHotel),
			Pending:       false, // Now posted
		},
		// Gas: Amount unchanged, pending→false, no notes (test nil notes preservation)
		{
			TransactionId: "plaid-txn-gas",
			AccountId:     "plaid-acc-gas",
			Amount:        55.00, // Same as authorization
			Date:          "2024-01-15",
			Name:          "Shell Gas Station",
			MerchantName:  *plaid.NewNullableString(&merchantGas),
			Pending:       false, // Now posted
		},
	}

	// Mock GetByExternalID to return existing pending transactions
	mockTransactionRepo.On("GetByExternalID", ctx, domain.SourcePlaid, "plaid-txn-restaurant", userID).
		Return(existingPendingRestaurant, nil)
	mockTransactionRepo.On("GetByExternalID", ctx, domain.SourcePlaid, "plaid-txn-hotel", userID).
		Return(existingPendingHotel, nil)
	mockTransactionRepo.On("GetByExternalID", ctx, domain.SourcePlaid, "plaid-txn-gas", userID).
		Return(existingPendingGas, nil)

	// Mock Update calls - verify each transaction updates correctly
	// Restaurant: Amount changed, pending→false, user_notes PRESERVED
	mockTransactionRepo.On("Update", ctx, mock.MatchedBy(func(txn *domain.Transaction) bool {
		return txn.ExternalTransactionID != nil &&
			*txn.ExternalTransactionID == "plaid-txn-restaurant" &&
			txn.Amount == 54.50 && // Tip added (Plaid returns positive for debits)
			!txn.Pending && // No longer pending
			txn.UserNotes != nil &&
			*txn.UserNotes == "Team lunch - add tip later" // Notes PRESERVED
	})).Return(nil).Once()

	// Hotel: Date changed, pending→false, user_notes PRESERVED
	mockTransactionRepo.On("Update", ctx, mock.MatchedBy(func(txn *domain.Transaction) bool {
		return txn.ExternalTransactionID != nil &&
			*txn.ExternalTransactionID == "plaid-txn-hotel" &&
			txn.Amount == 200.00 && // Plaid returns positive for debits
			!txn.Pending &&
			txn.UserNotes != nil &&
			*txn.UserNotes == "Business trip hotel" // Notes PRESERVED
	})).Return(nil).Once()

	// Gas: Amount unchanged, pending→false, nil notes preserved as nil
	mockTransactionRepo.On("Update", ctx, mock.MatchedBy(func(txn *domain.Transaction) bool {
		return txn.ExternalTransactionID != nil &&
			*txn.ExternalTransactionID == "plaid-txn-gas" &&
			txn.Amount == 55.00 && // Plaid returns positive for debits
			!txn.Pending &&
			txn.UserNotes == nil // No notes, preserved as nil
	})).Return(nil).Once()

	// Execute
	err := service.processModifiedTransactions(ctx, modified, linkedItem, accountMap, nil)

	// Assert
	assert.NoError(t, err)

	// Verify all expected Update calls occurred exactly once
	mockTransactionRepo.AssertExpectations(t)

	// Verify no Create calls (no duplication - same transaction updated, not created new)
	mockTransactionRepo.AssertNotCalled(t, "Create")

	// Key assertions verified by mock matchers:
	// ✓ Single transaction per external_transaction_id (no duplication)
	// ✓ Pending status changed from true to false
	// ✓ Amount updated with tip (restaurant)
	// ✓ Date can change (hotel - not verified in mock, but supported)
	// ✓ User notes preserved during transition (restaurant, hotel)
	// ✓ Nil user notes preserved as nil (gas)
}

// TestProcessAddedTransactions_SetsPFCFields tests that PFC v2 typed fields are
// populated from Plaid's PersonalFinanceCategory when transactions are synced.
func TestProcessAddedTransactions_SetsPFCFields(t *testing.T) {
	mockTransactionRepo := new(MockTransactionRepository)

	service := NewTransactionSyncService(
		nil,
		nil,
		mockTransactionRepo,
		nil,
		nil, // profileRepo
		nil, // balanceSnapshotRepo
		nil, // plaidService
		nil, // subscriptionService
		nil, // ruleApplicationService
		nil, // budgetSyncService
		nil, // transactionMonitorService
		nil, // merchantRepo
		nil, // logger
	).(*transactionSyncService)

	ctx := context.Background()
	linkedItem := &domain.LinkedItem{
		ID:     "linked-item-123",
		UserID: "user-456",
	}
	accountMap := map[string]string{"plaid-acc-1": "account-uuid-1"}

	// Build Plaid transaction with PFC v2 data
	pfcPrimary := "FOOD_AND_DRINK"
	pfcDetailed := "FOOD_AND_DRINK_GROCERIES"
	confidenceLevel := "HIGH"
	pfc := plaid.NewPersonalFinanceCategory(pfcPrimary, pfcDetailed)
	pfc.ConfidenceLevel = *plaid.NewNullableString(&confidenceLevel)
	plaidTxn := plaid.Transaction{
		TransactionId: "plaid-txn-pfc",
		AccountId:     "plaid-acc-1",
		Amount:        50.00,
		Date:          "2024-01-15",
		Name:          "Whole Foods",
		Pending:       false,
	}
	plaidTxn.PersonalFinanceCategory.Set(pfc)

	added := []plaid.Transaction{plaidTxn}

	mockTransactionRepo.On("CreateBatch", ctx, mock.MatchedBy(func(txns []domain.Transaction) bool {
		if len(txns) != 1 {
			return false
		}
		if txns[0].PFCPrimary == nil || *txns[0].PFCPrimary != pfcPrimary {
			t.Errorf("PFCPrimary should be %q, got %v", pfcPrimary, txns[0].PFCPrimary)
			return false
		}
		if txns[0].PFCDetailed == nil || *txns[0].PFCDetailed != pfcDetailed {
			t.Errorf("PFCDetailed should be %q, got %v", pfcDetailed, txns[0].PFCDetailed)
			return false
		}
		return true
	})).Return(nil, nil)

	err := service.processAddedTransactions(ctx, added, linkedItem, accountMap, nil)

	assert.NoError(t, err)
	mockTransactionRepo.AssertExpectations(t)
}

// strPtrHelper creates a pointer to a string value
func strPtrHelper(s string) *string {
	return &s
}

// stringPtr is an alias for strPtrHelper for backwards compatibility
var stringPtr = strPtrHelper

// mockProfileRepoForCursorTests is a minimal mock for ProfileRepository used in cursor tests
type mockProfileRepoForCursorTests struct {
	mock.Mock
}

// CountRetentionDeletionsWithheld is Issue #1569 T1's withholding-count read. Never exercised by
// this suite: it exists so mockProfileRepoForCursorTests keeps satisfying
// repository.ProfileRepository.
func (m *mockProfileRepoForCursorTests) CountRetentionDeletionsWithheld(ctx context.Context) (repository.RetentionWithholdingCounts, error) {
	return repository.RetentionWithholdingCounts{}, nil
}

func (m *mockProfileRepoForCursorTests) GetUserByClerkID(ctx context.Context, clerkUserID string) (*domain.User, error) {
	args := m.Called(ctx, clerkUserID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*domain.User), args.Error(1)
}

func (m *mockProfileRepoForCursorTests) GetUserByID(ctx context.Context, id string) (*domain.User, error) {
	args := m.Called(ctx, id)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*domain.User), args.Error(1)
}

func (m *mockProfileRepoForCursorTests) GetUserByPaddleSubscriptionID(ctx context.Context, subscriptionID string) (*domain.User, error) {
	args := m.Called(ctx, subscriptionID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*domain.User), args.Error(1)
}

func (m *mockProfileRepoForCursorTests) CreateUser(ctx context.Context, user *domain.User) error {
	args := m.Called(ctx, user)
	return args.Error(0)
}

func (m *mockProfileRepoForCursorTests) CreateUserTx(ctx context.Context, tx pgx.Tx, user *domain.User) error {
	args := m.Called(ctx, tx, user)
	return args.Error(0)
}

func (m *mockProfileRepoForCursorTests) UpdateUser(ctx context.Context, user *domain.User) error {
	args := m.Called(ctx, user)
	return args.Error(0)
}

func (m *mockProfileRepoForCursorTests) UpdateLastLogin(ctx context.Context, clerkUserID string) error {
	args := m.Called(ctx, clerkUserID)
	return args.Error(0)
}

func (m *mockProfileRepoForCursorTests) MarkForDeletion(ctx context.Context, userID string, scheduledAt time.Time) error {
	args := m.Called(ctx, userID, scheduledAt)
	return args.Error(0)
}

func (m *mockProfileRepoForCursorTests) CancelDeletion(ctx context.Context, userID string) error {
	args := m.Called(ctx, userID)
	return args.Error(0)
}

// RecordDeletionResidue (Issue #1504) records that a deletion performed an irreversible external
// effect and then did not complete. See repository.ProfileRepository for the contract.
func (m *mockProfileRepoForCursorTests) RecordDeletionResidue(ctx context.Context, userID string, effect repository.DeletionResidueEffect) error {
	args := m.Called(ctx, userID, effect)
	return args.Error(0)
}

func (m *mockProfileRepoForCursorTests) DeleteUserCascade(ctx context.Context, userID string, expectedDeletionRequestedAt *time.Time) (bool, []string, error) {
	args := m.Called(ctx, userID, expectedDeletionRequestedAt)
	return args.Bool(0), nil, args.Error(1)
}

// CountUncleanedLinkedItems — Issue #1494. Only DeletionService's Plaid-less branch calls this; these
// tests never reach it, so this is a plain stub rather than a testify expectation (which would panic
// unmatched).
func (m *mockProfileRepoForCursorTests) CountUncleanedLinkedItems(ctx context.Context, userID string) (int, error) {
	return 0, nil
}

// HouseholdOwnerBlocksDeletion — round 15, finding D2. Only DeletionService's pre-cleanup
// precondition calls this; these tests never reach it, so it is a plain stub for the same reason as
// the one above. It returns FALSE (nothing blocks) so a test that ever DID reach it would exercise
// the proceed path rather than an unexplained abort.
func (m *mockProfileRepoForCursorTests) HouseholdOwnerBlocksDeletion(ctx context.Context, userID string) (bool, error) {
	return false, nil
}

// Issue #679, #680: Trial expiration - methods for trial management
func (m *mockProfileRepoForCursorTests) GetUsersWithExpiredTrials(ctx context.Context) ([]*domain.User, error) {
	args := m.Called(ctx)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]*domain.User), args.Error(1)
}

func (m *mockProfileRepoForCursorTests) GetUsersWithExpiringTrials(ctx context.Context, withinDays int) ([]*domain.User, error) {
	args := m.Called(ctx, withinDays)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]*domain.User), args.Error(1)
}

// Issue #44, #45, #46: Paddle integration support
func (m *mockProfileRepoForCursorTests) UpdatePaddleCustomerID(ctx context.Context, userID, paddleCustomerID string) error {
	args := m.Called(ctx, userID, paddleCustomerID)
	return args.Error(0)
}

func (m *mockProfileRepoForCursorTests) UpdateSubscriptionFromPaddle(ctx context.Context, userID, tier, status string, startedAt, endsAt *time.Time, subscriptionID *string) error {
	args := m.Called(ctx, userID, tier, status, startedAt, endsAt, subscriptionID)
	return args.Error(0)
}

func (m *mockProfileRepoForCursorTests) GetUsersWithActivePaddleSubscriptions(ctx context.Context) ([]*domain.User, error) {
	args := m.Called(ctx)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]*domain.User), args.Error(1)
}

// Retention tracking stubs (#930)
func (m *mockProfileRepoForCursorTests) GetEffectiveSubscriptionStatusByUserID(ctx context.Context, userID string) (string, error) {
	return "", nil
}
func (m *mockProfileRepoForCursorTests) SetRetentionDates(ctx context.Context, userID string, guard repository.RetentionClockGuard, retentionMonths int) error {
	return nil
}
func (m *mockProfileRepoForCursorTests) ClearRetentionDates(ctx context.Context, userID string) error {
	return nil
}
func (m *mockProfileRepoForCursorTests) SetRetentionNotice30dSent(ctx context.Context, userID string) error {
	return nil
}
func (m *mockProfileRepoForCursorTests) SetRetentionNotice7dSent(ctx context.Context, userID string) error {
	return nil
}
func (m *mockProfileRepoForCursorTests) GetUsersNeedingRetention30dNotice(ctx context.Context) ([]*domain.User, error) {
	return nil, nil
}
func (m *mockProfileRepoForCursorTests) GetUsersNeedingRetention7dNotice(ctx context.Context) ([]*domain.User, error) {
	return nil, nil
}
func (m *mockProfileRepoForCursorTests) SetPastDueStartedAt(ctx context.Context, userID string, t time.Time) error {
	return nil
}
func (m *mockProfileRepoForCursorTests) ClearPastDueStartedAt(ctx context.Context, userID string) error {
	return nil
}
func (m *mockProfileRepoForCursorTests) GetUsersInPastDue(ctx context.Context) ([]domain.User, error) {
	return []domain.User{}, nil
}

// GetUsersNeedingRetentionClockRepair, RepairRetentionClock (Issue #1413 H5 residual) are not
// exercised by these cursor tests; stubbed to satisfy the widened ProfileRepository interface.
func (m *mockProfileRepoForCursorTests) GetUsersNeedingRetentionClockRepair(ctx context.Context) ([]*domain.User, error) {
	return []*domain.User{}, nil
}

func (m *mockProfileRepoForCursorTests) RepairRetentionClock(ctx context.Context, userID string, startedAt time.Time, retentionMonths int) (bool, error) {
	return false, nil
}

// GetUsersNeedingPlaidDisconnectRepair (Issue #1413 round-7 grace-window repair path) is not
// exercised by these cursor tests; stubbed to satisfy the widened ProfileRepository interface.
func (m *mockProfileRepoForCursorTests) GetUsersNeedingPlaidDisconnectRepair(ctx context.Context, gracePeriod time.Duration) ([]*domain.User, error) {
	return []*domain.User{}, nil
}
func (m *mockProfileRepoForCursorTests) ClaimPlaidDisconnectIfPastDue(ctx context.Context, userID string) (bool, error) {
	return false, nil
}

// ClaimForDeletion (Issue #1413 Codex review round 3) is not exercised by these cursor tests;
// stubbed to satisfy the widened ProfileRepository interface.
func (m *mockProfileRepoForCursorTests) ClaimForDeletion(ctx context.Context, userID string, kind repository.DeletionClaimKind) (bool, error) {
	return false, nil
}

// DeletionStillAuthorised — unreachable from these cursor tests; stubbed to satisfy the widened
// ProfileRepository interface.
func (m *mockProfileRepoForCursorTests) DeletionStillAuthorised(ctx context.Context, userID string, kind repository.DeletionClaimKind, expectedDeletionRequestedAt *time.Time) (bool, error) {
	return true, nil
}
func (m *mockProfileRepoForCursorTests) GetUserByEmail(ctx context.Context, email string) (*domain.User, error) {
	return nil, nil
}
func (m *mockProfileRepoForCursorTests) UpdateUserEmailTx(ctx context.Context, tx pgx.Tx, userID, plainEmail string) error {
	return nil
}

// ============================================================================
// CURSOR PERSISTENCE TESTS (Critical)
// Plaid Staging Validation - Cursor handling edge cases
// ============================================================================

// TestSyncTransactions_CursorHeldBackWhenTxnStorageFails verifies that the cursor is NOT persisted
// when transaction storage fails.
//
// #1570 INVERTED THIS TEST. It used to assert the opposite ("cursor persists even when storage
// fails ... ensures resumability"), which was precisely the data-loss bug: resuming from an
// advanced cursor means Plaid never re-delivers the rows that failed to store, so they are gone
// permanently. Real resumability is holding the cursor back until storage has committed.
func TestSyncTransactions_CursorHeldBackWhenTxnStorageFails(t *testing.T) {
	ctx := context.Background()

	// Setup mocks
	mockPlaidService := new(MockPlaidService)
	mockLinkedItemRepo := new(MockLinkedItemRepository)
	mockTransactionRepo := new(MockTransactionRepository)
	mockAccountRepo := new(MockAccountRepository)
	service := NewTransactionSyncService(
		nil, // webhookRepo
		mockLinkedItemRepo,
		mockTransactionRepo,
		mockAccountRepo,
		nil, // profileRepo - not needed for these tests
		nil, // balanceSnapshotRepo - not needed for these tests
		mockPlaidService,
		nil, // subscriptionService - not needed for these tests
		nil, // ruleApplicationService - not needed for these tests
		nil, // budgetSyncService - not needed for these tests
		nil, // transactionMonitorService - not needed for these tests
		nil, // merchantRepo - not needed for these tests
		nil, // logger
	)

	// LinkedItem with existing cursor (simulating mid-sync resume)
	existingCursor := "cursor-page-1"
	linkedItem := &domain.LinkedItem{
		ID:          "linked-item-123",
		UserID:      "user-123",
		ItemID:      "plaid-item-123",
		AccessToken: "access-token",
		Cursor:      &existingCursor,
	}

	// Mock: Get accounts for mapping
	accounts := []domain.Account{
		{ID: "acc-uuid-1", PlaidAccountID: "plaid-acc-1", UserID: "user-123"},
	}
	mockAccountRepo.On("GetByLinkedItemIDIncludingRetained", ctx, "linked-item-123").Return(accounts, nil)

	// Mock: Plaid TransactionsSync returns transactions
	newCursor := "cursor-page-2"
	plaidResponse := &plaid.TransactionsSyncResponse{
		Added: []plaid.Transaction{
			{TransactionId: "txn-1", AccountId: "plaid-acc-1", Amount: 50.00, Date: "2024-01-15", Name: "Test", Pending: false},
		},
		Modified: []plaid.Transaction{},
		Removed:  []plaid.RemovedTransaction{},
	}
	plaidResponse.SetNextCursor(newCursor)
	plaidResponse.SetHasMore(false)
	mockPlaidService.On("TransactionsSync", ctx, "access-token", existingCursor, 0).Return(plaidResponse, nil)

	// Mock: cursor update would succeed if called — the point is that it must NOT be called.
	mockLinkedItemRepo.On("UpdateCursor", mock.Anything, mock.Anything, mock.Anything).Return(nil).Maybe()

	// Mock: Transaction storage FAILS
	mockTransactionRepo.On("CreateBatch", ctx, mock.Anything).Return(nil, errors.New("database connection lost"))

	// Execute
	result, err := service.SyncTransactions(ctx, linkedItem)

	// Assert: Error is returned due to transaction storage failure
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "database connection lost")

	// Assert: the cursor was NOT advanced — the rows never reached the database, so advancing past
	// them would destroy them (Plaid does not re-deliver behind a stored cursor).
	assert.Empty(t, persistedCursors(mockLinkedItemRepo))
	mockLinkedItemRepo.AssertNotCalled(t, "UpdateCursor", ctx, "linked-item-123", newCursor)

	// Assert: Partial results were returned (transactions that failed to store). NewCursor is the
	// in-memory cursor the run reached; it is deliberately NOT what was persisted.
	assert.NotNil(t, result)
	assert.Equal(t, newCursor, result.NewCursor)
}

// TestSyncTransactions_CursorResumeFromLastSuccessfulPage verifies that after a
// partial sync failure, the next sync attempt resumes from the last successful cursor.
func TestSyncTransactions_CursorResumeFromLastSuccessfulPage(t *testing.T) {
	ctx := context.Background()

	// Setup mocks
	mockPlaidService := new(MockPlaidService)
	mockLinkedItemRepo := new(MockLinkedItemRepository)
	mockTransactionRepo := new(MockTransactionRepository)
	mockAccountRepo := new(MockAccountRepository)
	service := NewTransactionSyncService(
		nil, // webhookRepo
		mockLinkedItemRepo,
		mockTransactionRepo,
		mockAccountRepo,
		nil, // profileRepo - not needed for these tests
		nil, // balanceSnapshotRepo - not needed for these tests
		mockPlaidService,
		nil, // subscriptionService - not needed for these tests
		nil, // ruleApplicationService - not needed for these tests
		nil, // budgetSyncService - not needed for these tests
		nil, // transactionMonitorService - not needed for these tests
		nil, // merchantRepo - not needed for these tests
		nil, // logger
	)

	// LinkedItem with cursor from previous partial sync
	resumeCursor := "cursor-after-page-2"
	linkedItem := &domain.LinkedItem{
		ID:          "linked-item-123",
		UserID:      "user-123",
		ItemID:      "plaid-item-123",
		AccessToken: "access-token",
		Cursor:      &resumeCursor,
	}

	// Mock: Get accounts for mapping
	accounts := []domain.Account{
		{ID: "acc-uuid-1", PlaidAccountID: "plaid-acc-1", UserID: "user-123"},
	}
	mockAccountRepo.On("GetByLinkedItemIDIncludingRetained", ctx, "linked-item-123").Return(accounts, nil)

	// Mock: Plaid API is called with the RESUME cursor (not empty cursor)
	finalCursor := "cursor-final"
	plaidResponse := &plaid.TransactionsSyncResponse{
		Added: []plaid.Transaction{
			{TransactionId: "txn-resume-1", AccountId: "plaid-acc-1", Amount: 25.00, Date: "2024-01-16", Name: "Resume Txn", Pending: false},
		},
		Modified: []plaid.Transaction{},
		Removed:  []plaid.RemovedTransaction{},
	}
	plaidResponse.SetNextCursor(finalCursor)
	plaidResponse.SetHasMore(false)

	// CRITICAL: The cursor passed to TransactionsSync should be the resume cursor
	mockPlaidService.On("TransactionsSync", ctx, "access-token", resumeCursor, 0).Return(plaidResponse, nil)

	// Mock: Cursor update succeeds
	mockLinkedItemRepo.On("UpdateCursor", ctx, "linked-item-123", finalCursor).Return(nil)

	// Mock: Transaction storage succeeds
	mockTransactionRepo.On("CreateBatch", ctx, mock.Anything).Return(nil, nil)

	// Execute
	result, err := service.SyncTransactions(ctx, linkedItem)

	// Assert: Sync completes successfully
	assert.NoError(t, err)
	assert.NotNil(t, result)
	assert.Equal(t, finalCursor, result.NewCursor)

	// Assert: TransactionsSync was called with the resume cursor (not empty)
	mockPlaidService.AssertCalled(t, "TransactionsSync", ctx, "access-token", resumeCursor, 0)
}

// TestSyncTransactions_NilVsEmptyCursor verifies that both nil cursor and empty string
// cursor trigger a first-sync (no cursor parameter to Plaid API).
func TestSyncTransactions_NilVsEmptyCursor(t *testing.T) {
	testCases := []struct {
		name   string
		cursor *string
	}{
		{"nil cursor", nil},
		{"empty string", strPtrHelper("")},
	}

	// Load real subscription service for historical depth (all tiers return 730 days)
	tierConfig, err := config.LoadTierConfig()
	require.NoError(t, err)
	subscriptionService := NewSubscriptionService(tierConfig, nil)

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()

			// Setup mocks
			mockPlaidService := new(MockPlaidService)
			mockLinkedItemRepo := new(MockLinkedItemRepository)
			mockTransactionRepo := new(MockTransactionRepository)
			mockAccountRepo := new(MockAccountRepository)
			mockProfileRepo := new(mockProfileRepoForCursorTests)
			service := NewTransactionSyncService(
				nil, mockLinkedItemRepo, mockTransactionRepo, mockAccountRepo, mockProfileRepo,
				nil, mockPlaidService, subscriptionService,
				nil, nil, nil, nil, nil,
			)

			linkedItem := &domain.LinkedItem{
				ID:          "linked-item-first-sync",
				UserID:      "user-123",
				ItemID:      "plaid-item-123",
				AccessToken: "access-token",
				Cursor:      tc.cursor,
			}

			// Mock: Get user for historical depth lookup (first sync)
			testUser := &domain.User{
				ID:               "user-123",
				SubscriptionTier: string(domain.TierPremium),
			}
			mockProfileRepo.On("GetUserByID", ctx, "user-123").Return(testUser, nil)

			// Mock: Get accounts
			accounts := []domain.Account{
				{ID: "acc-uuid-1", PlaidAccountID: "plaid-acc-1", UserID: "user-123"},
			}
			mockAccountRepo.On("GetByLinkedItemIDIncludingRetained", ctx, "linked-item-first-sync").Return(accounts, nil)

			// Mock: Plaid API - expect empty string cursor for first sync; premium tier = 730 days
			firstCursor := "cursor-first"
			plaidResponse := &plaid.TransactionsSyncResponse{
				Added:    []plaid.Transaction{},
				Modified: []plaid.Transaction{},
				Removed:  []plaid.RemovedTransaction{},
			}
			plaidResponse.SetNextCursor(firstCursor)
			plaidResponse.SetHasMore(false)

			// CRITICAL: Both nil and "" should result in empty string being passed; 730 days for premium
			mockPlaidService.On("TransactionsSync", ctx, "access-token", "", 730).Return(plaidResponse, nil)

			// Mock: Cursor update
			mockLinkedItemRepo.On("UpdateCursor", ctx, "linked-item-first-sync", firstCursor).Return(nil)

			// Execute
			result, err := service.SyncTransactions(ctx, linkedItem)

			// Assert
			assert.NoError(t, err)
			assert.NotNil(t, result)
			assert.Equal(t, firstCursor, result.NewCursor)

			// Assert: TransactionsSync was called with empty cursor and 730 days historical
			mockPlaidService.AssertCalled(t, "TransactionsSync", ctx, "access-token", "", 730)
		})
	}
}

// TestSyncTransactions_FinalPageCursorUpdate verifies that cursor is updated
// even on the final page (has_more=false).
func TestSyncTransactions_FinalPageCursorUpdate(t *testing.T) {
	ctx := context.Background()

	// Setup mocks
	mockPlaidService := new(MockPlaidService)
	mockLinkedItemRepo := new(MockLinkedItemRepository)
	mockTransactionRepo := new(MockTransactionRepository)
	mockAccountRepo := new(MockAccountRepository)
	service := NewTransactionSyncService(
		nil, // webhookRepo
		mockLinkedItemRepo,
		mockTransactionRepo,
		mockAccountRepo,
		nil, // profileRepo - not needed for these tests
		nil, // balanceSnapshotRepo - not needed for these tests
		mockPlaidService,
		nil, // subscriptionService - not needed for these tests
		nil, // ruleApplicationService - not needed for these tests
		nil, // budgetSyncService - not needed for these tests
		nil, // transactionMonitorService - not needed for these tests
		nil, // merchantRepo - not needed for these tests
		nil, // logger
	)

	// LinkedItem with existing cursor
	existingCursor := "cursor-page-3"
	linkedItem := &domain.LinkedItem{
		ID:          "linked-item-123",
		UserID:      "user-123",
		ItemID:      "plaid-item-123",
		AccessToken: "access-token",
		Cursor:      &existingCursor,
	}

	// Mock: Get accounts
	accounts := []domain.Account{
		{ID: "acc-uuid-1", PlaidAccountID: "plaid-acc-1", UserID: "user-123"},
	}
	mockAccountRepo.On("GetByLinkedItemIDIncludingRetained", ctx, "linked-item-123").Return(accounts, nil)

	// Mock: Plaid API returns final page (has_more=false)
	finalCursor := "cursor-final-page"
	plaidResponse := &plaid.TransactionsSyncResponse{
		Added: []plaid.Transaction{
			{TransactionId: "txn-final", AccountId: "plaid-acc-1", Amount: 10.00, Date: "2024-01-17", Name: "Final", Pending: false},
		},
		Modified: []plaid.Transaction{},
		Removed:  []plaid.RemovedTransaction{},
	}
	plaidResponse.SetNextCursor(finalCursor)
	plaidResponse.SetHasMore(false) // Final page

	mockPlaidService.On("TransactionsSync", ctx, "access-token", existingCursor, 0).Return(plaidResponse, nil)

	// CRITICAL: Even on final page, cursor should be updated
	mockLinkedItemRepo.On("UpdateCursor", ctx, "linked-item-123", finalCursor).Return(nil)

	// Mock: Transaction storage succeeds
	mockTransactionRepo.On("CreateBatch", ctx, mock.Anything).Return(nil, nil)

	// Execute
	result, err := service.SyncTransactions(ctx, linkedItem)

	// Assert
	assert.NoError(t, err)
	assert.NotNil(t, result)
	assert.Equal(t, finalCursor, result.NewCursor)

	// Assert: UpdateCursor was called with final cursor
	mockLinkedItemRepo.AssertCalled(t, "UpdateCursor", ctx, "linked-item-123", finalCursor)
}

// TestSyncTransactions_InvalidCursorAutoReset verifies that when Plaid returns an
// INVALID_CURSOR error, the cursor is automatically reset to empty string to trigger
// a full re-sync on retry.
// Issue #171: Auto-recovery from cursor corruption
func TestSyncTransactions_InvalidCursorAutoReset(t *testing.T) {
	ctx := context.Background()

	// Setup mocks
	mockPlaidService := new(MockPlaidService)
	mockLinkedItemRepo := new(MockLinkedItemRepository)
	service := NewTransactionSyncService(
		nil, // webhookRepo
		mockLinkedItemRepo,
		nil, // transactionRepo
		nil, // accountRepo
		nil, // profileRepo
		nil, // balanceSnapshotRepo
		mockPlaidService,
		nil, // subscriptionService
		nil, // ruleApplicationService
		nil, // budgetSyncService
		nil, // transactionMonitorService
		nil, // merchantRepo
		nil, // logger
	)

	// LinkedItem with corrupted cursor
	corruptedCursor := "corrupted-cursor-xyz"
	linkedItem := &domain.LinkedItem{
		ID:          "linked-item-123",
		UserID:      "user-123",
		ItemID:      "plaid-item-123",
		AccessToken: "access-token",
		Cursor:      &corruptedCursor,
	}

	// Mock: Plaid API returns INVALID_CURSOR error
	// This simulates a corrupted or expired cursor scenario
	invalidCursorErr := createPlaidInvalidCursorError()
	mockPlaidService.On("TransactionsSync", ctx, "access-token", corruptedCursor, 0).Return(nil, invalidCursorErr)

	// CRITICAL: Cursor should be reset to empty string for full re-sync
	mockLinkedItemRepo.On("UpdateCursor", ctx, "linked-item-123", "").Return(nil)

	// Execute
	result, err := service.SyncTransactions(ctx, linkedItem)

	// Assert: Error is returned (to trigger retry with reset cursor)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "cursor reset")

	// Assert: Cursor was reset to empty string
	mockLinkedItemRepo.AssertCalled(t, "UpdateCursor", ctx, "linked-item-123", "")

	// Assert: Partial results returned
	assert.NotNil(t, result)
	assert.Empty(t, result.Added) // No transactions fetched due to error
}

// TestSyncTransactions_InvalidCursorResetFailure verifies error handling when
// cursor reset fails during INVALID_CURSOR recovery.
func TestSyncTransactions_InvalidCursorResetFailure(t *testing.T) {
	ctx := context.Background()

	// Setup mocks
	mockPlaidService := new(MockPlaidService)
	mockLinkedItemRepo := new(MockLinkedItemRepository)
	service := NewTransactionSyncService(
		nil, mockLinkedItemRepo, nil, nil, nil, nil,
		mockPlaidService, nil, nil, nil, nil, nil, nil,
	)

	// LinkedItem with corrupted cursor
	corruptedCursor := "corrupted-cursor"
	linkedItem := &domain.LinkedItem{
		ID:          "linked-item-123",
		UserID:      "user-123",
		ItemID:      "plaid-item-123",
		AccessToken: "access-token",
		Cursor:      &corruptedCursor,
	}

	// Mock: Plaid API returns INVALID_CURSOR error
	invalidCursorErr := createPlaidInvalidCursorError()
	mockPlaidService.On("TransactionsSync", ctx, "access-token", corruptedCursor, 0).Return(nil, invalidCursorErr)

	// Mock: Cursor reset FAILS (database error)
	mockLinkedItemRepo.On("UpdateCursor", ctx, "linked-item-123", "").Return(errors.New("database connection lost"))

	// Execute
	result, err := service.SyncTransactions(ctx, linkedItem)

	// Assert: Error indicates cursor reset failure
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to reset cursor")

	// Assert: Partial results returned
	assert.NotNil(t, result)
}

// createPlaidInvalidCursorError creates a mock Plaid error that classifies as INVALID_CURSOR.
// In production, this would be a plaid.PlaidError with ErrorCode="INVALID_CURSOR".
func createPlaidInvalidCursorError() error {
	// Create a mock error that ClassifyPlaidError will recognize as INVALID_CURSOR.
	// Since we can't easily create a real plaid.PlaidError, we use a workaround:
	// We need to mock at the domain.ClassifyPlaidError level or use a real Plaid error structure.
	//
	// For this test, we'll create an error that will be classified as Temporary by default
	// (since it's not a real Plaid error), and we need to ensure the test exercises the
	// cursor reset code path.
	//
	// To properly test this, we need to create an actual Plaid-formatted error response.
	// Let's use the plaid SDK's error handling.
	return &domain.PlaidSyncError{
		OriginalError: errors.New("cursor is invalid or expired"),
		ErrorType:     domain.PlaidErrorInvalidCursor,
		ErrorCode:     "INVALID_CURSOR",
	}
}

// ============================================================================
// Issue #171 - Plaid Re-Link Handling: UserAccountID Propagation Tests
// ============================================================================
// These tests verify that transactions synced from Plaid have their
// UserAccountID field set correctly for stable reference across re-links.

// TestTransactionSyncService_SyncTransactions_SetsUserAccountID verifies that
// transactions synced from Plaid have UserAccountID set from the account.
// TDD RED: This test will fail until mapPlaidTransaction is updated.
func TestTransactionSyncService_SyncTransactions_SetsUserAccountID(t *testing.T) {
	// Setup mocks
	mockWebhookRepo := new(MockWebhookRepository)
	mockLinkedItemRepo := new(MockLinkedItemRepository)
	mockTransactionRepo := new(MockTransactionRepository)
	mockAccountRepo := new(MockAccountRepository)
	mockPlaidService := new(MockPlaidService)
	service := NewTransactionSyncService(
		mockWebhookRepo,
		mockLinkedItemRepo,
		mockTransactionRepo,
		mockAccountRepo,
		nil, // profileRepo
		nil, // balanceSnapshotRepo
		mockPlaidService,
		nil, // subscriptionService
		nil, // ruleApplicationService
		nil, // budgetSyncService
		nil, // transactionMonitorService
		nil, // merchantRepo
		nil, // logger
	)

	ctx := context.Background()
	userAccountID := "user-account-uuid-123"

	// LinkedItem with active status - use existing cursor to skip sync limit check
	linkedItem := &domain.LinkedItem{
		ID:          "linked-item-123",
		UserID:      "user-123",
		ItemID:      "plaid-item-123",
		AccessToken: "access-token",
		Cursor:      stringPtr("existing-cursor"), // Re-sync to skip limit check
		Provider:    "plaid",
		Status:      domain.LinkedItemStatusActive,
	}

	// Account with UserAccountID set (from re-link or initial link)
	accounts := []domain.Account{
		{
			ID:             "internal-account-uuid",
			UserID:         "user-123",
			PlaidAccountID: "plaid-account-id-1",
			LinkedItemID:   "linked-item-123",
			UserAccountID:  &userAccountID, // Issue #171: Stable reference
			Status:         domain.AccountStatusActive,
		},
	}

	// Mock Plaid TransactionsSync response with one new transaction
	plaidResp := &plaid.TransactionsSyncResponse{
		Added: []plaid.Transaction{
			{
				TransactionId: "plaid-txn-1",
				AccountId:     "plaid-account-id-1",
				Amount:        99.99,
				Date:          "2024-01-15",
				Name:          "Test Transaction",
				Pending:       false,
			},
		},
		Modified:   []plaid.Transaction{},
		Removed:    []plaid.RemovedTransaction{},
		NextCursor: "cursor-123",
		HasMore:    false,
	}
	mockPlaidService.On("TransactionsSync", ctx, "access-token", "existing-cursor", 0).Return(plaidResp, nil)

	// Mock GetByLinkedItemID returns accounts with UserAccountID set
	mockAccountRepo.On("GetByLinkedItemIDIncludingRetained", ctx, "linked-item-123").Return(accounts, nil)

	// Mock CreateBatch - verify transactions have UserAccountID set
	mockTransactionRepo.On("CreateBatch", ctx, mock.MatchedBy(func(txns []domain.Transaction) bool {
		if len(txns) != 1 {
			t.Logf("Expected 1 transaction, got %d", len(txns))
			return false
		}
		// Issue #171: UserAccountID MUST be set from the account
		if txns[0].UserAccountID == nil || *txns[0].UserAccountID != userAccountID {
			t.Logf("Expected UserAccountID=%s, got %v", userAccountID, txns[0].UserAccountID)
			return false
		}
		return true
	})).Return(nil, nil)

	// Mock cursor update
	mockLinkedItemRepo.On("UpdateCursor", ctx, "linked-item-123", "cursor-123").Return(nil)

	// Execute
	result, err := service.SyncTransactions(ctx, linkedItem)

	// Assert
	assert.NoError(t, err)
	assert.NotNil(t, result)
	assert.Len(t, result.Added, 1)

	mockTransactionRepo.AssertExpectations(t)
	t.Log("✓ UserAccountID correctly propagated from account to transaction")
}

// TestTransactionSyncService_SyncTransactions_LooksUpUserAccountFromAccount verifies
// that the sync service correctly looks up UserAccountID from each account.
func TestTransactionSyncService_SyncTransactions_LooksUpUserAccountFromAccount(t *testing.T) {
	mockWebhookRepo := new(MockWebhookRepository)
	mockLinkedItemRepo := new(MockLinkedItemRepository)
	mockTransactionRepo := new(MockTransactionRepository)
	mockAccountRepo := new(MockAccountRepository)
	mockPlaidService := new(MockPlaidService)
	service := NewTransactionSyncService(
		mockWebhookRepo,
		mockLinkedItemRepo,
		mockTransactionRepo,
		mockAccountRepo,
		nil, nil,
		mockPlaidService,
		nil, // subscriptionService
		nil, nil, nil, nil, nil,
	)

	ctx := context.Background()
	userAccountID1 := "user-account-1"
	userAccountID2 := "user-account-2"

	linkedItem := &domain.LinkedItem{
		ID:          "linked-item-123",
		UserID:      "user-123",
		ItemID:      "plaid-item-123",
		AccessToken: "access-token",
		Cursor:      stringPtr("existing-cursor"), // Re-sync to skip limit check
		Provider:    "plaid",
		Status:      domain.LinkedItemStatusActive,
	}

	// Two accounts with different UserAccountIDs
	accounts := []domain.Account{
		{
			ID:             "account-uuid-1",
			UserID:         "user-123",
			PlaidAccountID: "plaid-acc-1",
			LinkedItemID:   "linked-item-123",
			UserAccountID:  &userAccountID1,
			Status:         domain.AccountStatusActive,
		},
		{
			ID:             "account-uuid-2",
			UserID:         "user-123",
			PlaidAccountID: "plaid-acc-2",
			LinkedItemID:   "linked-item-123",
			UserAccountID:  &userAccountID2,
			Status:         domain.AccountStatusActive,
		},
	}

	// Plaid returns transactions from both accounts
	plaidResp := &plaid.TransactionsSyncResponse{
		Added: []plaid.Transaction{
			{TransactionId: "txn-1", AccountId: "plaid-acc-1", Amount: 10.00, Date: "2024-01-15", Name: "Txn 1"},
			{TransactionId: "txn-2", AccountId: "plaid-acc-2", Amount: 20.00, Date: "2024-01-16", Name: "Txn 2"},
		},
		Modified:   []plaid.Transaction{},
		Removed:    []plaid.RemovedTransaction{},
		NextCursor: "cursor-456",
		HasMore:    false,
	}
	mockPlaidService.On("TransactionsSync", ctx, "access-token", "existing-cursor", 0).Return(plaidResp, nil)

	mockAccountRepo.On("GetByLinkedItemIDIncludingRetained", ctx, "linked-item-123").Return(accounts, nil)

	// Verify each transaction gets correct UserAccountID from its account
	mockTransactionRepo.On("CreateBatch", ctx, mock.MatchedBy(func(txns []domain.Transaction) bool {
		if len(txns) != 2 {
			return false
		}
		// First txn from plaid-acc-1 should have user-account-1
		if txns[0].UserAccountID == nil || *txns[0].UserAccountID != userAccountID1 {
			t.Logf("txn[0] expected UserAccountID=%s, got %v", userAccountID1, txns[0].UserAccountID)
			return false
		}
		// Second txn from plaid-acc-2 should have user-account-2
		if txns[1].UserAccountID == nil || *txns[1].UserAccountID != userAccountID2 {
			t.Logf("txn[1] expected UserAccountID=%s, got %v", userAccountID2, txns[1].UserAccountID)
			return false
		}
		return true
	})).Return(nil, nil)

	mockLinkedItemRepo.On("UpdateCursor", ctx, "linked-item-123", "cursor-456").Return(nil)

	result, err := service.SyncTransactions(ctx, linkedItem)

	assert.NoError(t, err)
	assert.NotNil(t, result)
	assert.Len(t, result.Added, 2)

	mockTransactionRepo.AssertExpectations(t)
	t.Log("✓ Each transaction correctly maps to its account's UserAccountID")
}

// TestTransactionSyncService_ProcessAddedTransactions_SetsUserAccountID tests the
// internal processAddedTransactions method with userAccountMap.
func TestTransactionSyncService_ProcessAddedTransactions_SetsUserAccountID(t *testing.T) {
	mockTransactionRepo := new(MockTransactionRepository)

	service := NewTransactionSyncService(
		nil, nil,
		mockTransactionRepo,
		nil, nil, nil, nil, nil, nil, nil, nil, nil, nil,
	).(*transactionSyncService)

	ctx := context.Background()
	userAccountID := "user-account-stable-uuid"

	linkedItem := &domain.LinkedItem{
		ID:     "linked-item-123",
		UserID: "user-456",
	}

	// Account map: Plaid account_id -> Internal account UUID
	accountMap := map[string]string{
		"plaid-acc-1": "account-uuid-1",
	}

	// Issue #171: User account map: Plaid account_id -> UserAccount UUID
	userAccountMap := map[string]string{
		"plaid-acc-1": userAccountID,
	}

	added := []plaid.Transaction{
		{
			TransactionId: "plaid-txn-1",
			AccountId:     "plaid-acc-1",
			Amount:        50.00,
			Date:          "2024-01-20",
			Name:          "Test Purchase",
			Pending:       false,
		},
	}

	// Verify transaction has UserAccountID set
	mockTransactionRepo.On("CreateBatch", ctx, mock.MatchedBy(func(txns []domain.Transaction) bool {
		if len(txns) != 1 {
			return false
		}
		// Issue #171: Must have UserAccountID from userAccountMap
		if txns[0].UserAccountID == nil || *txns[0].UserAccountID != userAccountID {
			t.Logf("Expected UserAccountID=%s, got %v", userAccountID, txns[0].UserAccountID)
			return false
		}
		// Also verify AccountID is set correctly
		if txns[0].AccountID != "account-uuid-1" {
			t.Logf("Expected AccountID=account-uuid-1, got %s", txns[0].AccountID)
			return false
		}
		return true
	})).Return(nil, nil)

	// Execute - note: processAddedTransactions signature will need to be updated
	// to accept userAccountMap parameter for Issue #171
	err := service.processAddedTransactions(ctx, added, linkedItem, accountMap, userAccountMap)

	assert.NoError(t, err)
	mockTransactionRepo.AssertExpectations(t)
	t.Log("✓ processAddedTransactions correctly sets UserAccountID from userAccountMap")
}

// TestTransactionSyncService_ProcessAddedTransactions_NullUserAccountID_Handled tests
// that transactions are still created when UserAccountID is nil (legacy data).
func TestTransactionSyncService_ProcessAddedTransactions_NullUserAccountID_Handled(t *testing.T) {
	mockTransactionRepo := new(MockTransactionRepository)

	service := NewTransactionSyncService(
		nil, nil,
		mockTransactionRepo,
		nil, nil, nil, nil, nil, nil, nil, nil, nil, nil,
	).(*transactionSyncService)

	ctx := context.Background()

	linkedItem := &domain.LinkedItem{
		ID:     "linked-item-123",
		UserID: "user-456",
	}

	accountMap := map[string]string{
		"plaid-acc-1": "account-uuid-1",
	}

	// Empty userAccountMap (legacy account without user_account_id)
	userAccountMap := map[string]string{}

	added := []plaid.Transaction{
		{
			TransactionId: "plaid-txn-legacy",
			AccountId:     "plaid-acc-1",
			Amount:        75.00,
			Date:          "2024-01-25",
			Name:          "Legacy Transaction",
			Pending:       false,
		},
	}

	// Transaction should still be created, but with nil UserAccountID
	mockTransactionRepo.On("CreateBatch", ctx, mock.MatchedBy(func(txns []domain.Transaction) bool {
		if len(txns) != 1 {
			return false
		}
		// UserAccountID should be nil (not in map)
		if txns[0].UserAccountID != nil {
			t.Logf("Expected nil UserAccountID, got %v", *txns[0].UserAccountID)
			return false
		}
		// AccountID should still be set correctly
		if txns[0].AccountID != "account-uuid-1" {
			return false
		}
		return true
	})).Return(nil, nil)

	err := service.processAddedTransactions(ctx, added, linkedItem, accountMap, userAccountMap)

	assert.NoError(t, err)
	mockTransactionRepo.AssertExpectations(t)
	t.Log("✓ Transactions created with nil UserAccountID for legacy accounts")
}

// TestTransactionSyncService_Relink_PreservesEnrichments verifies that when a
// transaction is re-synced after a re-link (same transaction_id, different account_id),
// the DO UPDATE preserves user enrichments (notes, tags, category_override).
// Note: This is tested via the transaction repository DO UPDATE tests, but we include
// a service-level test to verify the full flow.
func TestTransactionSyncService_Relink_PreservesEnrichments(t *testing.T) {
	mockTransactionRepo := new(MockTransactionRepository)

	service := NewTransactionSyncService(
		nil, nil,
		mockTransactionRepo,
		nil, nil, nil, nil, nil, nil, nil, nil, nil, nil,
	).(*transactionSyncService)

	ctx := context.Background()
	newUserAccountID := "new-user-account-after-relink"

	// This is the NEW linked_item after re-link
	linkedItem := &domain.LinkedItem{
		ID:     "new-linked-item-456",
		UserID: "user-123",
	}

	// New account mapping after re-link (new Plaid account_id -> new internal UUID)
	accountMap := map[string]string{
		"new-plaid-acc-id": "new-account-uuid",
	}

	userAccountMap := map[string]string{
		"new-plaid-acc-id": newUserAccountID,
	}

	// This transaction has the SAME transaction_id as before re-link
	// but comes from a NEW Plaid account_id
	added := []plaid.Transaction{
		{
			TransactionId: "stable-txn-id-preserved", // Same as before re-link
			AccountId:     "new-plaid-acc-id",        // Different from before
			Amount:        150.00,
			Date:          "2024-01-10",
			Name:          "Re-linked Transaction",
			Pending:       false,
		},
	}

	// The DO UPDATE in CreateBatch should:
	// 1. Update account_id, linked_item_id, user_account_id (to new values)
	// 2. PRESERVE user_notes, category_override, tags, tags_source
	mockTransactionRepo.On("CreateBatch", ctx, mock.MatchedBy(func(txns []domain.Transaction) bool {
		if len(txns) != 1 {
			return false
		}
		// Verify new values are set
		if txns[0].AccountID != "new-account-uuid" {
			return false
		}
		if txns[0].LinkedItemID != "new-linked-item-456" {
			return false
		}
		if txns[0].UserAccountID == nil || *txns[0].UserAccountID != newUserAccountID {
			return false
		}
		return true
	})).Return(nil, nil)

	err := service.processAddedTransactions(ctx, added, linkedItem, accountMap, userAccountMap)

	assert.NoError(t, err)
	mockTransactionRepo.AssertExpectations(t)
	t.Log("✓ Re-link transaction sync updates FKs while preserving enrichments (via DO UPDATE)")
}

// TestTransactionSyncService_ModifiedTransactions_SetsUserAccountID verifies that
// modified transactions also get UserAccountID propagated.
func TestTransactionSyncService_ModifiedTransactions_SetsUserAccountID(t *testing.T) {
	mockTransactionRepo := new(MockTransactionRepository)

	service := NewTransactionSyncService(
		nil, nil,
		mockTransactionRepo,
		nil, nil, nil, nil, nil, nil, nil, nil, nil, nil,
	).(*transactionSyncService)

	ctx := context.Background()
	userAccountID := "user-account-uuid-modified"

	linkedItem := &domain.LinkedItem{
		ID:     "linked-item-123",
		UserID: "user-456",
	}

	accountMap := map[string]string{
		"plaid-acc-1": "account-uuid-1",
	}

	userAccountMap := map[string]string{
		"plaid-acc-1": userAccountID,
	}

	// Modified transactions (e.g., pending -> posted)
	modified := []plaid.Transaction{
		{
			TransactionId: "plaid-txn-modified",
			AccountId:     "plaid-acc-1",
			Amount:        100.00,
			Date:          "2024-01-28",
			Name:          "Modified Transaction",
			Pending:       false, // Was true, now false (posted)
		},
	}

	// Mock GetByExternalID - processModifiedTransactions looks up existing transaction first
	existingTxn := &domain.Transaction{
		ID:            "existing-txn-uuid",
		UserID:        "user-456",
		AccountID:     "account-uuid-1",
		LinkedItemID:  "linked-item-123",
		UserAccountID: nil, // Old transaction didn't have UserAccountID
		Amount:        100.00,
	}
	mockTransactionRepo.On("GetByExternalID", ctx, domain.SourcePlaid, "plaid-txn-modified", "user-456").Return(existingTxn, nil)

	// Update should be called with UserAccountID set
	mockTransactionRepo.On("Update", ctx, mock.MatchedBy(func(txn *domain.Transaction) bool {
		if txn.UserAccountID == nil || *txn.UserAccountID != userAccountID {
			t.Logf("Modified txn: expected UserAccountID=%s, got %v", userAccountID, txn.UserAccountID)
			return false
		}
		return true
	})).Return(nil)

	err := service.processModifiedTransactions(ctx, modified, linkedItem, accountMap, userAccountMap)

	assert.NoError(t, err)
	mockTransactionRepo.AssertExpectations(t)
	t.Log("✓ Modified transactions also have UserAccountID set")
}

// TestTransactionSyncService_MapPlaidTransaction_SetsIconURL verifies that
// personal_finance_category_icon_url from Plaid is mapped to PFCCategoryIconURL.
func TestTransactionSyncService_MapPlaidTransaction_SetsIconURL(t *testing.T) {
	svc := &transactionSyncService{}
	ctx := context.Background()

	iconURL := "https://plaid-category-icons.plaid.com/PFC_FOOD_AND_DRINK.png"
	plaidTxn := plaid.Transaction{
		TransactionId:                  "txn-icon-test",
		AccountId:                      "acct-plaid-1",
		Amount:                         12.50,
		Date:                           "2024-03-01",
		Name:                           "Coffee Shop",
		Pending:                        false,
		PersonalFinanceCategoryIconUrl: &iconURL,
	}
	accountMap := map[string]string{"acct-plaid-1": "acct-internal-1"}
	userAccountMap := map[string]string{"acct-plaid-1": "user-acct-1"}

	tx, err := svc.mapPlaidTransaction(ctx, plaidTxn, "linked-item-1", "user-1", accountMap, userAccountMap)
	require.NoError(t, err)
	require.NotNil(t, tx.PFCCategoryIconURL)
	assert.Equal(t, iconURL, *tx.PFCCategoryIconURL)
}

// TestTransactionSyncService_MapPlaidTransaction_NilIconURL verifies that
// a nil icon URL from Plaid results in a nil PFCCategoryIconURL on the domain object.
func TestTransactionSyncService_MapPlaidTransaction_NilIconURL(t *testing.T) {
	svc := &transactionSyncService{}
	ctx := context.Background()

	plaidTxn := plaid.Transaction{
		TransactionId: "txn-no-icon",
		AccountId:     "acct-plaid-1",
		Amount:        5.00,
		Date:          "2024-03-01",
		Name:          "Test Store",
		Pending:       false,
	}
	accountMap := map[string]string{"acct-plaid-1": "acct-internal-1"}
	userAccountMap := map[string]string{"acct-plaid-1": "user-acct-1"}

	tx, err := svc.mapPlaidTransaction(ctx, plaidTxn, "linked-item-1", "user-1", accountMap, userAccountMap)
	require.NoError(t, err)
	assert.Nil(t, tx.PFCCategoryIconURL)
}

// ============================================================================
// Issue #1163: Merchant ID stamping tests
// ============================================================================

// MockMerchantRepo mocks the repository.MerchantRepo interface.
type MockMerchantRepo struct {
	mock.Mock
}

func (m *MockMerchantRepo) GetOrCreateMerchantsWithNormalize(ctx context.Context, names []string, normalize func(string) string) (map[string]string, error) {
	args := m.Called(ctx, names, normalize)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(map[string]string), args.Error(1)
}

func (m *MockMerchantRepo) ListMerchants(ctx context.Context, userID string) ([]domain.MerchantSummary, error) {
	args := m.Called(ctx, userID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]domain.MerchantSummary), args.Error(1)
}

func (m *MockMerchantRepo) GetDuplicateSuggestions(ctx context.Context, userID string, threshold float64) ([][]domain.MerchantSummary, error) {
	args := m.Called(ctx, userID, threshold)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([][]domain.MerchantSummary), args.Error(1)
}

// TestProcessAddedTransactions_StampsMerchantID verifies that when transactions have
// a MerchantName, GetOrCreateMerchantsWithNormalize is called once and MerchantID is stamped on
// each transaction before CreateBatch is invoked.
func TestProcessAddedTransactions_StampsMerchantID(t *testing.T) {
	mockTransactionRepo := new(MockTransactionRepository)
	mockMerchantRepo := new(MockMerchantRepo)

	service := NewTransactionSyncService(
		nil, nil,
		mockTransactionRepo,
		nil, nil, nil, nil, nil, nil, nil, nil,
		mockMerchantRepo,
		nil, // logger
	).(*transactionSyncService)

	ctx := context.Background()
	linkedItem := &domain.LinkedItem{ID: "linked-item-123", UserID: "user-456"}

	merchantName := "Starbucks"
	starbucksID := "merchant-uuid-starbucks"

	accountMap := map[string]string{"plaid-acc-1": "account-uuid-1"}

	added := []plaid.Transaction{
		{
			TransactionId: "txn-1",
			AccountId:     "plaid-acc-1",
			Amount:        5.50,
			Date:          "2024-03-01",
			Name:          "STARBUCKS",
			MerchantName:  *plaid.NewNullableString(&merchantName),
			Pending:       false,
		},
	}

	// GetOrCreateMerchantsWithNormalize should be called exactly once with the deduplicated name set
	mockMerchantRepo.On("GetOrCreateMerchantsWithNormalize", ctx, mock.MatchedBy(func(names []string) bool {
		return len(names) == 1 && names[0] == "Starbucks"
	}), mock.AnythingOfType("func(string) string")).Return(map[string]string{"Starbucks": starbucksID}, nil)

	// CreateBatch must receive the transaction with MerchantID stamped
	mockTransactionRepo.On("CreateBatch", ctx, mock.MatchedBy(func(txns []domain.Transaction) bool {
		if len(txns) != 1 {
			return false
		}
		if txns[0].MerchantID == nil {
			t.Log("MerchantID was not stamped")
			return false
		}
		return *txns[0].MerchantID == starbucksID
	})).Return(nil, nil)

	err := service.processAddedTransactions(ctx, added, linkedItem, accountMap, nil)

	require.NoError(t, err)
	mockMerchantRepo.AssertExpectations(t)
	mockTransactionRepo.AssertExpectations(t)
}

// TestProcessAddedTransactions_MerchantUpsertError_ContinuesSync verifies that when
// GetOrCreateMerchantsWithNormalize fails, the sync continues without MerchantID (best-effort).
func TestProcessAddedTransactions_MerchantUpsertError_ContinuesSync(t *testing.T) {
	mockTransactionRepo := new(MockTransactionRepository)
	mockMerchantRepo := new(MockMerchantRepo)

	service := NewTransactionSyncService(
		nil, nil,
		mockTransactionRepo,
		nil, nil, nil, nil, nil, nil, nil, nil,
		mockMerchantRepo,
		nil, // logger
	).(*transactionSyncService)

	ctx := context.Background()
	linkedItem := &domain.LinkedItem{ID: "linked-item-123", UserID: "user-456"}

	merchantName := "Amazon"
	accountMap := map[string]string{"plaid-acc-1": "account-uuid-1"}

	added := []plaid.Transaction{
		{
			TransactionId: "txn-2",
			AccountId:     "plaid-acc-1",
			Amount:        19.99,
			Date:          "2024-03-02",
			Name:          "AMAZON",
			MerchantName:  *plaid.NewNullableString(&merchantName),
			Pending:       false,
		},
	}

	// Simulate merchant upsert failure
	mockMerchantRepo.On("GetOrCreateMerchantsWithNormalize", ctx, mock.Anything, mock.AnythingOfType("func(string) string")).Return(nil, errors.New("db error"))

	// CreateBatch must still be called — MerchantID will be nil (best-effort)
	mockTransactionRepo.On("CreateBatch", ctx, mock.MatchedBy(func(txns []domain.Transaction) bool {
		return len(txns) == 1 && txns[0].MerchantID == nil
	})).Return(nil, nil)

	err := service.processAddedTransactions(ctx, added, linkedItem, accountMap, nil)

	require.NoError(t, err, "sync must not fail when merchant upsert errors")
	mockMerchantRepo.AssertExpectations(t)
	mockTransactionRepo.AssertExpectations(t)
}

// TestProcessAddedTransactions_NoMerchantName_SkipsMerchantUpsert verifies that when
// no transactions have a MerchantName, GetOrCreateMerchantsWithNormalize is never called.
func TestProcessAddedTransactions_NoMerchantName_SkipsMerchantUpsert(t *testing.T) {
	mockTransactionRepo := new(MockTransactionRepository)
	mockMerchantRepo := new(MockMerchantRepo)

	service := NewTransactionSyncService(
		nil, nil,
		mockTransactionRepo,
		nil, nil, nil, nil, nil, nil, nil, nil,
		mockMerchantRepo,
		nil, // logger
	).(*transactionSyncService)

	ctx := context.Background()
	linkedItem := &domain.LinkedItem{ID: "linked-item-123", UserID: "user-456"}
	accountMap := map[string]string{"plaid-acc-1": "account-uuid-1"}

	// Transaction with no MerchantName
	added := []plaid.Transaction{
		{
			TransactionId: "txn-3",
			AccountId:     "plaid-acc-1",
			Amount:        10.00,
			Date:          "2024-03-03",
			Name:          "Unknown Charge",
			Pending:       false,
		},
	}

	mockTransactionRepo.On("CreateBatch", ctx, mock.Anything).Return(nil, nil)

	err := service.processAddedTransactions(ctx, added, linkedItem, accountMap, nil)

	require.NoError(t, err)
	// GetOrCreateMerchantsWithNormalize must NOT have been called
	mockMerchantRepo.AssertNotCalled(t, "GetOrCreateMerchantsWithNormalize")
	mockTransactionRepo.AssertExpectations(t)
}

// TestProcessAddedTransactions_WhitespaceOnlyMerchantName_SkipsMerchantUpsert verifies that
// a transaction whose MerchantName is whitespace-only is treated the same as empty —
// GetOrCreateMerchantsWithNormalize must NOT be called (G33, issue #957 Round 10).
// Covers spaces, tabs, and newlines since strings.TrimSpace handles all three.
func TestProcessAddedTransactions_WhitespaceOnlyMerchantName_SkipsMerchantUpsert(t *testing.T) {
	for _, wsName := range []string{"   ", "\t", "\n"} {
		wsName := wsName
		t.Run(wsName, func(t *testing.T) {
			mockTransactionRepo := new(MockTransactionRepository)
			mockMerchantRepo := new(MockMerchantRepo)

			service := NewTransactionSyncService(
				nil, nil,
				mockTransactionRepo,
				nil, nil, nil, nil, nil, nil, nil, nil,
				mockMerchantRepo,
				nil,
			).(*transactionSyncService)

			ctx := context.Background()
			linkedItem := &domain.LinkedItem{ID: "linked-item-g33", UserID: "user-g33"}
			accountMap := map[string]string{"plaid-acc-g33": "account-uuid-g33"}

			added := []plaid.Transaction{
				{
					TransactionId: "txn-g33",
					AccountId:     "plaid-acc-g33",
					Amount:        9.99,
					Date:          "2024-06-28",
					Name:          "Whitespace Merchant",
					MerchantName:  *plaid.NewNullableString(&wsName),
					Pending:       false,
				},
			}

			mockTransactionRepo.On("CreateBatch", ctx, mock.Anything).Return(nil, nil)

			err := service.processAddedTransactions(ctx, added, linkedItem, accountMap, nil)

			require.NoError(t, err)
			mockMerchantRepo.AssertNotCalled(t, "GetOrCreateMerchantsWithNormalize")
			mockTransactionRepo.AssertExpectations(t)
		})
	}
}

// TestProcessModifiedTransactions_StampsMerchantID verifies that when modified
// transactions have a MerchantName, GetOrCreateMerchantsWithNormalize is called once (per batch)
// and MerchantID is stamped on each transaction before Update is invoked.
// This is the GREEN test for Gap 1 of the #1163 gap analysis.
func TestProcessModifiedTransactions_StampsMerchantID(t *testing.T) {
	mockTransactionRepo := new(MockTransactionRepository)
	mockMerchantRepo := new(MockMerchantRepo)

	service := NewTransactionSyncService(
		nil, nil,
		mockTransactionRepo,
		nil, nil, nil, nil, nil, nil, nil, nil,
		mockMerchantRepo,
		nil, // logger
	).(*transactionSyncService)

	ctx := context.Background()
	userID := "user-456"
	linkedItem := &domain.LinkedItem{ID: "linked-item-123", UserID: userID}
	accountMap := map[string]string{"plaid-acc-1": "account-uuid-1"}

	merchantName := "Starbucks"
	starbucksID := "merchant-uuid-starbucks"
	externalID := "plaid-txn-mod-1"

	modified := []plaid.Transaction{
		{
			TransactionId: externalID,
			AccountId:     "plaid-acc-1",
			Amount:        5.75,
			Date:          "2024-03-01",
			Name:          "STARBUCKS",
			MerchantName:  *plaid.NewNullableString(&merchantName),
			Pending:       false,
		},
	}

	existingUserNotes := "my coffee note"
	existingTxn := &domain.Transaction{
		ID:                    "txn-uuid-existing",
		UserID:                userID,
		AccountID:             "account-uuid-1",
		ExternalTransactionID: &externalID,
		Amount:                5.50,
		Name:                  "STARBUCKS",
		UserNotes:             &existingUserNotes,
	}

	// GetOrCreateMerchantsWithNormalize must be called once with the merchant name from the batch
	mockMerchantRepo.On("GetOrCreateMerchantsWithNormalize", ctx, mock.MatchedBy(func(names []string) bool {
		return len(names) == 1 && names[0] == "Starbucks"
	}), mock.AnythingOfType("func(string) string")).Return(map[string]string{"Starbucks": starbucksID}, nil)

	mockTransactionRepo.On("GetByExternalID", ctx, domain.SourcePlaid, externalID, userID).Return(existingTxn, nil)

	// Update must be called with MerchantID stamped AND user_notes preserved
	mockTransactionRepo.On("Update", ctx, mock.MatchedBy(func(txn *domain.Transaction) bool {
		if txn.MerchantID == nil {
			t.Log("MerchantID was not stamped on modified transaction")
			return false
		}
		if *txn.MerchantID != starbucksID {
			t.Logf("MerchantID = %s, want %s", *txn.MerchantID, starbucksID)
			return false
		}
		if txn.UserNotes == nil || *txn.UserNotes != existingUserNotes {
			t.Log("user_notes not preserved on modified transaction")
			return false
		}
		return true
	})).Return(nil)

	err := service.processModifiedTransactions(ctx, modified, linkedItem, accountMap, nil)

	require.NoError(t, err)
	mockMerchantRepo.AssertExpectations(t)
	mockTransactionRepo.AssertExpectations(t)
}

// TestProcessModifiedTransactions_MerchantUpsertError_ContinuesUpdate verifies that when
// GetOrCreateMerchantsWithNormalize fails during modification, the Update still proceeds without MerchantID.
func TestProcessModifiedTransactions_MerchantUpsertError_ContinuesUpdate(t *testing.T) {
	mockTransactionRepo := new(MockTransactionRepository)
	mockMerchantRepo := new(MockMerchantRepo)

	service := NewTransactionSyncService(
		nil, nil,
		mockTransactionRepo,
		nil, nil, nil, nil, nil, nil, nil, nil,
		mockMerchantRepo,
		nil, // logger
	).(*transactionSyncService)

	ctx := context.Background()
	userID := "user-456"
	linkedItem := &domain.LinkedItem{ID: "linked-item-123", UserID: userID}
	accountMap := map[string]string{"plaid-acc-1": "account-uuid-1"}

	merchantName := "Amazon"
	externalID := "plaid-txn-mod-2"

	modified := []plaid.Transaction{
		{
			TransactionId: externalID,
			AccountId:     "plaid-acc-1",
			Amount:        19.99,
			Date:          "2024-03-02",
			Name:          "AMAZON",
			MerchantName:  *plaid.NewNullableString(&merchantName),
			Pending:       false,
		},
	}

	existingTxn := &domain.Transaction{
		ID:     "txn-uuid-existing-2",
		UserID: userID,
	}

	// Simulate merchant upsert failure
	mockMerchantRepo.On("GetOrCreateMerchantsWithNormalize", ctx, mock.Anything, mock.AnythingOfType("func(string) string")).Return(nil, errors.New("db error"))

	mockTransactionRepo.On("GetByExternalID", ctx, domain.SourcePlaid, externalID, userID).Return(existingTxn, nil)

	// Update must still be called — MerchantID will be nil (best-effort)
	mockTransactionRepo.On("Update", ctx, mock.MatchedBy(func(txn *domain.Transaction) bool {
		return txn.MerchantID == nil
	})).Return(nil)

	err := service.processModifiedTransactions(ctx, modified, linkedItem, accountMap, nil)

	require.NoError(t, err, "update must not fail when merchant upsert errors")
	mockMerchantRepo.AssertExpectations(t)
	mockTransactionRepo.AssertExpectations(t)
}

// ---------------------------------------------------------------------------
// Issue #1213 — RED tests for processAddedTransactions wiring.
// These test that analytics side-effects are gated on insertedIDs from
// CreateBatch, and that MonitorForMatches still receives all transactions.
// ---------------------------------------------------------------------------

// mockRuleApplicationServiceFor1213 mocks RuleApplicationService with the
// new interface (ApplyRulesToBatch returns []BatchRuleApplication, EmitBatchSideEffects).
type mockRuleApplicationServiceFor1213 struct {
	mock.Mock
}

func (m *mockRuleApplicationServiceFor1213) ApplyRulesToTransaction(ctx context.Context, tx *domain.Transaction) error {
	args := m.Called(ctx, tx)
	return args.Error(0)
}

func (m *mockRuleApplicationServiceFor1213) ApplyRulesToBatch(ctx context.Context, txns []domain.Transaction, userID string) ([]BatchRuleApplication, error) {
	args := m.Called(ctx, txns, userID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]BatchRuleApplication), args.Error(1)
}

func (m *mockRuleApplicationServiceFor1213) EmitBatchSideEffects(ctx context.Context, userID string, applications []BatchRuleApplication, insertedIDs map[string]bool) {
	m.Called(ctx, userID, applications, insertedIDs)
}

// mockTransactionMonitorServiceFor1213 captures MonitorForMatches calls.
type mockTransactionMonitorServiceFor1213 struct {
	mock.Mock
}

func (m *mockTransactionMonitorServiceFor1213) MonitorForMatches(ctx context.Context, userID string, newTransactions []*domain.Transaction) error {
	args := m.Called(ctx, userID, newTransactions)
	return args.Error(0)
}

// TestProcessAddedTransactions_EmitBatchSideEffectsCalledWithInsertedIDs verifies
// that after CreateBatch, EmitBatchSideEffects is called with the exact insertedIDs
// map returned by CreateBatch.  Analytics/match-count filtering is then the
// responsibility of EmitBatchSideEffects (tested in rule_application_service_test.go).
func TestProcessAddedTransactions_EmitBatchSideEffectsCalledWithInsertedIDs(t *testing.T) {
	mockTransactionRepo := new(MockTransactionRepository)
	mockRuleService := new(mockRuleApplicationServiceFor1213)
	mockMonitor := new(mockTransactionMonitorServiceFor1213)

	service := NewTransactionSyncService(
		nil, nil,
		mockTransactionRepo,
		nil, nil, nil, nil, nil,
		mockRuleService,
		nil,
		mockMonitor,
		nil,
		nil, // logger
	).(*transactionSyncService)

	ctx := context.Background()
	linkedItem := &domain.LinkedItem{ID: "li-1", UserID: "user-1213"}
	accountMap := map[string]string{"plaid-acc-1": "account-uuid-1"}

	added := []plaid.Transaction{
		{TransactionId: "tx-a", AccountId: "plaid-acc-1", Amount: 10.00, Date: "2024-01-01", Name: "Inserted"},
		{TransactionId: "tx-b", AccountId: "plaid-acc-1", Amount: 20.00, Date: "2024-01-01", Name: "Conflict"},
	}

	// Simulate: rule matched both transactions
	fakeApplications := []BatchRuleApplication{
		{TransactionID: "tx-a", MatchedRuleIDs: []string{"r1"}, CategoryApplied: true, CategoryRuleID: "r1"},
		{TransactionID: "tx-b", MatchedRuleIDs: []string{"r1"}, CategoryApplied: true, CategoryRuleID: "r1"},
	}
	mockRuleService.On("ApplyRulesToBatch", ctx, mock.Anything, "user-1213").
		Return(fakeApplications, nil)

	// CreateBatch returns: tx-a inserted, tx-b conflicted.
	// CreateBatch now returns (map[string]bool, error).
	insertedIDs := map[string]bool{"tx-a": true}
	mockTransactionRepo.On("CreateBatch", ctx, mock.Anything).
		Return(insertedIDs, nil)

	// EmitBatchSideEffects must be called with exactly the returned insertedIDs.
	mockRuleService.On("EmitBatchSideEffects", ctx, "user-1213",
		mock.MatchedBy(func(apps []BatchRuleApplication) bool { return len(apps) == 2 }),
		mock.MatchedBy(func(ids map[string]bool) bool { return ids["tx-a"] && !ids["tx-b"] }),
	).Return()

	// MonitorForMatches must still receive ALL transactions (both inserted and conflict-updated).
	mockMonitor.On("MonitorForMatches", ctx, "user-1213",
		mock.MatchedBy(func(txns []*domain.Transaction) bool { return len(txns) == 2 }),
	).Return(nil)

	err := service.processAddedTransactions(ctx, added, linkedItem, accountMap, nil)

	require.NoError(t, err)
	mockRuleService.AssertExpectations(t)
	mockTransactionRepo.AssertExpectations(t)
	mockMonitor.AssertExpectations(t)
}

// ============================================================================
// Issue #972: B4 — TransferCorrelationService wiring tests
// ============================================================================

// MockTransferCorrelationService is a testify mock for TransferCorrelationService.
type MockTransferCorrelationService struct {
	mock.Mock
}

func (m *MockTransferCorrelationService) CorrelateForUser(ctx context.Context, userID string) error {
	args := m.Called(ctx, userID)
	return args.Error(0)
}

// TestSyncTransactions_CallsCorrelateForUser verifies that CorrelateForUser is called
// with the correct userID after a successful sync that adds transactions.
func TestSyncTransactions_CallsCorrelateForUser(t *testing.T) {
	mockLinkedItemRepo := new(MockLinkedItemRepository)
	mockTransactionRepo := new(MockTransactionRepository)
	mockAccountRepo := new(MockAccountRepository)
	mockPlaidService := new(MockPlaidService)
	mockCorrelation := new(MockTransferCorrelationService)

	svc := NewTransactionSyncService(
		nil,
		mockLinkedItemRepo,
		mockTransactionRepo,
		mockAccountRepo,
		nil, // profileRepo
		nil, // balanceSnapshotRepo
		mockPlaidService,
		nil, // subscriptionService
		nil, // ruleApplicationService
		nil, // budgetSyncService
		nil, // transactionMonitorService
		nil, // merchantRepo
		nil, // logger
	)
	svc.SetTransferCorrelationService(mockCorrelation)

	ctx := context.Background()
	linkedItem := &domain.LinkedItem{
		ID:          "li-corr-1",
		UserID:      "user-corr-1",
		AccessToken: "token",
		Cursor:      stringPtr("c1"),
	}

	plaidResp := &plaid.TransactionsSyncResponse{
		Added:      []plaid.Transaction{{TransactionId: "txn-corr-1", Amount: 50.0}},
		Modified:   []plaid.Transaction{},
		Removed:    []plaid.RemovedTransaction{},
		NextCursor: "c2",
		HasMore:    false,
	}
	mockPlaidService.On("TransactionsSync", ctx, "token", "c1", 0).Return(plaidResp, nil)
	mockLinkedItemRepo.On("UpdateCursor", ctx, linkedItem.ID, "c2").Return(nil)
	mockAccountRepo.On("GetByLinkedItemIDIncludingRetained", ctx, linkedItem.ID).Return([]domain.Account{}, nil)
	// CorrelateForUser must be called with the correct userID
	mockCorrelation.On("CorrelateForUser", ctx, "user-corr-1").Return(nil)

	result, err := svc.SyncTransactions(ctx, linkedItem)

	require.NoError(t, err)
	assert.NotNil(t, result)
	mockCorrelation.AssertExpectations(t)
}

// TestSyncTransactions_CorrelationErrorDoesNotFailSync verifies that if CorrelateForUser
// returns an error, SyncTransactions still returns nil (error is logged but swallowed).
func TestSyncTransactions_CorrelationErrorDoesNotFailSync(t *testing.T) {
	mockLinkedItemRepo := new(MockLinkedItemRepository)
	mockTransactionRepo := new(MockTransactionRepository)
	mockAccountRepo := new(MockAccountRepository)
	mockPlaidService := new(MockPlaidService)
	mockCorrelation := new(MockTransferCorrelationService)

	svc := NewTransactionSyncService(
		nil,
		mockLinkedItemRepo,
		mockTransactionRepo,
		mockAccountRepo,
		nil, // profileRepo
		nil, // balanceSnapshotRepo
		mockPlaidService,
		nil, // subscriptionService
		nil, // ruleApplicationService
		nil, // budgetSyncService
		nil, // transactionMonitorService
		nil, // merchantRepo
		nil, // logger
	)
	svc.SetTransferCorrelationService(mockCorrelation)

	ctx := context.Background()
	linkedItem := &domain.LinkedItem{
		ID:          "li-corr-2",
		UserID:      "user-corr-2",
		AccessToken: "token2",
		Cursor:      stringPtr("c3"),
	}

	plaidResp := &plaid.TransactionsSyncResponse{
		Added:      []plaid.Transaction{{TransactionId: "txn-corr-2", Amount: 25.0}},
		Modified:   []plaid.Transaction{},
		Removed:    []plaid.RemovedTransaction{},
		NextCursor: "c4",
		HasMore:    false,
	}
	mockPlaidService.On("TransactionsSync", ctx, "token2", "c3", 0).Return(plaidResp, nil)
	mockLinkedItemRepo.On("UpdateCursor", ctx, linkedItem.ID, "c4").Return(nil)
	mockAccountRepo.On("GetByLinkedItemIDIncludingRetained", ctx, linkedItem.ID).Return([]domain.Account{}, nil)
	// CorrelateForUser returns an error — sync must NOT fail
	mockCorrelation.On("CorrelateForUser", ctx, "user-corr-2").Return(errors.New("correlation db error"))

	result, err := svc.SyncTransactions(ctx, linkedItem)

	// Sync must succeed even though correlation errored
	require.NoError(t, err)
	assert.NotNil(t, result)
	mockCorrelation.AssertExpectations(t)
}

// TestProcessAddedTransactions_MonitorForMatchesReceivesAllTransactions ensures
// that MonitorForMatches receives all persisted transactions, not just inserted
// ones, preserving the existing re-link matching semantics.
func TestProcessAddedTransactions_MonitorForMatchesReceivesAllTransactions(t *testing.T) {
	mockTransactionRepo := new(MockTransactionRepository)
	mockMonitor := new(mockTransactionMonitorServiceFor1213)

	service := NewTransactionSyncService(
		nil, nil,
		mockTransactionRepo,
		nil, nil, nil, nil, nil,
		nil, // ruleApplicationService
		nil,
		mockMonitor,
		nil,
		nil, // logger
	).(*transactionSyncService)

	ctx := context.Background()
	linkedItem := &domain.LinkedItem{ID: "li-2", UserID: "user-monitor"}
	accountMap := map[string]string{"plaid-acc-1": "account-uuid-1"}

	added := []plaid.Transaction{
		{TransactionId: "tx-relink", AccountId: "plaid-acc-1", Amount: 50.00, Date: "2024-01-01", Name: "Re-linked"},
	}

	// Only the re-linked row is in the batch; createBatch returns it as NOT inserted
	// (it was conflict-updated for FK refresh on re-link).
	notInserted := map[string]bool{"tx-relink": false}
	mockTransactionRepo.On("CreateBatch", ctx, mock.Anything).Return(notInserted, nil)

	// MonitorForMatches must STILL receive the re-linked transaction even though
	// it was not a fresh insert (the monitor needs it to refresh transfer matching).
	mockMonitor.On("MonitorForMatches", ctx, "user-monitor",
		mock.MatchedBy(func(txns []*domain.Transaction) bool { return len(txns) == 1 }),
	).Return(nil)

	err := service.processAddedTransactions(ctx, added, linkedItem, accountMap, nil)

	require.NoError(t, err)
	mockTransactionRepo.AssertExpectations(t)
	mockMonitor.AssertExpectations(t)
}

// =============================================================================
// Issue #1300 Gap G2 — M12: Wire sync → contribution reconciliation → RecomputeUser
// =============================================================================

// MockSyncContributionCleaner is a testify mock for SyncContributionCleaner.
type MockSyncContributionCleaner struct {
	mock.Mock
}

func (m *MockSyncContributionCleaner) DeleteManagedForTransaction(ctx context.Context, transactionID string) error {
	args := m.Called(ctx, transactionID)
	return args.Error(0)
}

// MockSyncGoalProgressUpdater is a testify mock for SyncGoalProgressUpdater.
type MockSyncGoalProgressUpdater struct {
	mock.Mock
}

func (m *MockSyncGoalProgressUpdater) RecomputeUser(ctx context.Context, userID string) error {
	args := m.Called(ctx, userID)
	return args.Error(0)
}

// MockSyncManagedCategoryBackfiller is a testify mock for SyncManagedCategoryBackfiller.
type MockSyncManagedCategoryBackfiller struct {
	mock.Mock
}

func (m *MockSyncManagedCategoryBackfiller) BackfillManagedCategories(ctx context.Context, userID string) error {
	args := m.Called(ctx, userID)
	return args.Error(0)
}

// TestSyncTransactions_SavingsRecomputeUserCalledAfterSync verifies that
// RecomputeUser is called on the user after a successful sync (Issue #1300 G2).
func TestSyncTransactions_SavingsRecomputeUserCalledAfterSync(t *testing.T) {
	mockLinkedItemRepo := new(MockLinkedItemRepository)
	mockTransactionRepo := new(MockTransactionRepository)
	mockAccountRepo := new(MockAccountRepository)
	mockPlaidService := new(MockPlaidService)
	mockProgressUpdater := new(MockSyncGoalProgressUpdater)

	svc := NewTransactionSyncService(
		nil,
		mockLinkedItemRepo,
		mockTransactionRepo,
		mockAccountRepo,
		nil, nil,
		mockPlaidService,
		nil, nil, nil, nil, nil, nil,
	)
	svc.SetSavingsProgressServices(nil, mockProgressUpdater, nil)

	ctx := context.Background()
	cursor1 := "c1"
	linkedItem := &domain.LinkedItem{
		ID:     "li-recompute-1",
		UserID: "user-recompute-1",
		Cursor: &cursor1,
	}

	plaidResp := &plaid.TransactionsSyncResponse{
		Added:      []plaid.Transaction{},
		Modified:   []plaid.Transaction{},
		Removed:    []plaid.RemovedTransaction{},
		NextCursor: "c2",
		HasMore:    false,
	}
	mockPlaidService.On("TransactionsSync", ctx, "", "c1", 0).Return(plaidResp, nil)
	mockLinkedItemRepo.On("UpdateCursor", ctx, linkedItem.ID, "c2").Return(nil)
	mockAccountRepo.On("GetByLinkedItemIDIncludingRetained", ctx, linkedItem.ID).Return([]domain.Account{}, nil)
	mockProgressUpdater.On("RecomputeUser", ctx, "user-recompute-1").Return(nil)

	result, err := svc.SyncTransactions(ctx, linkedItem)

	require.NoError(t, err)
	assert.NotNil(t, result)
	mockProgressUpdater.AssertExpectations(t)
}

// TestSyncTransactions_SavingsBackfillCalledAfterRecompute verifies that
// BackfillManagedCategories is called after a successful sync (Issue #1300 G3).
func TestSyncTransactions_SavingsBackfillCalledAfterRecompute(t *testing.T) {
	mockLinkedItemRepo := new(MockLinkedItemRepository)
	mockTransactionRepo := new(MockTransactionRepository)
	mockAccountRepo := new(MockAccountRepository)
	mockPlaidService := new(MockPlaidService)
	mockBackfill := new(MockSyncManagedCategoryBackfiller)

	svc := NewTransactionSyncService(
		nil,
		mockLinkedItemRepo,
		mockTransactionRepo,
		mockAccountRepo,
		nil, nil,
		mockPlaidService,
		nil, nil, nil, nil, nil, nil,
	)
	svc.SetSavingsProgressServices(nil, nil, mockBackfill)

	ctx := context.Background()
	cursor1b := "c1"
	linkedItem := &domain.LinkedItem{
		ID:     "li-backfill-1",
		UserID: "user-backfill-1",
		Cursor: &cursor1b,
	}

	plaidResp := &plaid.TransactionsSyncResponse{
		Added: []plaid.Transaction{}, Modified: []plaid.Transaction{},
		Removed: []plaid.RemovedTransaction{}, NextCursor: "c2", HasMore: false,
	}
	mockPlaidService.On("TransactionsSync", ctx, "", "c1", 0).Return(plaidResp, nil)
	mockLinkedItemRepo.On("UpdateCursor", ctx, linkedItem.ID, "c2").Return(nil)
	mockAccountRepo.On("GetByLinkedItemIDIncludingRetained", ctx, linkedItem.ID).Return([]domain.Account{}, nil)
	mockBackfill.On("BackfillManagedCategories", ctx, "user-backfill-1").Return(nil)

	result, err := svc.SyncTransactions(ctx, linkedItem)

	require.NoError(t, err)
	assert.NotNil(t, result)
	mockBackfill.AssertExpectations(t)
}

// TestSyncTransactions_SavingsRecomputeErrorDoesNotFailSync verifies that
// a RecomputeUser error is non-fatal: sync succeeds (Issue #1300 G2).
func TestSyncTransactions_SavingsRecomputeErrorDoesNotFailSync(t *testing.T) {
	mockLinkedItemRepo := new(MockLinkedItemRepository)
	mockTransactionRepo := new(MockTransactionRepository)
	mockAccountRepo := new(MockAccountRepository)
	mockPlaidService := new(MockPlaidService)
	mockProgressUpdater := new(MockSyncGoalProgressUpdater)

	svc := NewTransactionSyncService(
		nil,
		mockLinkedItemRepo,
		mockTransactionRepo,
		mockAccountRepo,
		nil, nil,
		mockPlaidService,
		nil, nil, nil, nil, nil, nil,
	)
	svc.SetSavingsProgressServices(nil, mockProgressUpdater, nil)

	ctx := context.Background()
	cursorNF := "c1"
	linkedItem := &domain.LinkedItem{ID: "li-nonfatal", UserID: "user-nonfatal", Cursor: &cursorNF}

	plaidResp := &plaid.TransactionsSyncResponse{
		Added: []plaid.Transaction{}, Modified: []plaid.Transaction{},
		Removed: []plaid.RemovedTransaction{}, NextCursor: "c2", HasMore: false,
	}
	mockPlaidService.On("TransactionsSync", ctx, "", "c1", 0).Return(plaidResp, nil)
	mockLinkedItemRepo.On("UpdateCursor", ctx, linkedItem.ID, "c2").Return(nil)
	mockAccountRepo.On("GetByLinkedItemIDIncludingRetained", ctx, linkedItem.ID).Return([]domain.Account{}, nil)
	// RecomputeUser errors — sync must still succeed
	mockProgressUpdater.On("RecomputeUser", ctx, "user-nonfatal").Return(errors.New("db connection lost"))

	result, err := svc.SyncTransactions(ctx, linkedItem)

	require.NoError(t, err) // non-fatal
	assert.NotNil(t, result)
	mockProgressUpdater.AssertExpectations(t)
}

// TestSyncTransactions_SavingsNilSafeWhenNotWired verifies that sync works
// normally when savings services are not wired (Issue #1300 G2 nil-safe).
func TestSyncTransactions_SavingsNilSafeWhenNotWired(t *testing.T) {
	mockLinkedItemRepo := new(MockLinkedItemRepository)
	mockTransactionRepo := new(MockTransactionRepository)
	mockAccountRepo := new(MockAccountRepository)
	mockPlaidService := new(MockPlaidService)

	svc := NewTransactionSyncService(
		nil,
		mockLinkedItemRepo,
		mockTransactionRepo,
		mockAccountRepo,
		nil, nil,
		mockPlaidService,
		nil, nil, nil, nil, nil, nil,
	)
	// No SetSavingsProgressServices call — all nil

	ctx := context.Background()
	cursorNS := "c1"
	linkedItem := &domain.LinkedItem{ID: "li-nilsafe", UserID: "user-nilsafe", Cursor: &cursorNS}

	plaidResp := &plaid.TransactionsSyncResponse{
		Added: []plaid.Transaction{}, Modified: []plaid.Transaction{},
		Removed: []plaid.RemovedTransaction{}, NextCursor: "c2", HasMore: false,
	}
	mockPlaidService.On("TransactionsSync", ctx, "", "c1", 0).Return(plaidResp, nil)
	mockLinkedItemRepo.On("UpdateCursor", ctx, linkedItem.ID, "c2").Return(nil)
	mockAccountRepo.On("GetByLinkedItemIDIncludingRetained", ctx, linkedItem.ID).Return([]domain.Account{}, nil)

	result, err := svc.SyncTransactions(ctx, linkedItem)

	require.NoError(t, err)
	assert.NotNil(t, result)
}

// TestSyncTransactions_DeleteManagedContribsBeforeTransactionRemoval verifies that
// DeleteManagedForTransaction is called (with the internal txn ID) before removing
// a transaction, preventing phantom progress (Codex C4, Issue #1300 G2).
func TestSyncTransactions_DeleteManagedContribsBeforeTransactionRemoval(t *testing.T) {
	mockLinkedItemRepo := new(MockLinkedItemRepository)
	mockTransactionRepo := new(MockTransactionRepository)
	mockAccountRepo := new(MockAccountRepository)
	mockPlaidService := new(MockPlaidService)
	mockCleaner := new(MockSyncContributionCleaner)

	svc := NewTransactionSyncService(
		nil,
		mockLinkedItemRepo,
		mockTransactionRepo,
		mockAccountRepo,
		nil, nil,
		mockPlaidService,
		nil, nil, nil, nil, nil, nil,
	)
	svc.SetSavingsProgressServices(mockCleaner, nil, nil)

	ctx := context.Background()
	cursorR1 := "c1"
	linkedItem := &domain.LinkedItem{ID: "li-remove-1", UserID: "user-remove-1", Cursor: &cursorR1}

	plaidExtID := "plaid-txn-ext-1"
	internalTxnID := "internal-uuid-for-txn"

	plaidResp := &plaid.TransactionsSyncResponse{
		Added: []plaid.Transaction{}, Modified: []plaid.Transaction{},
		Removed: []plaid.RemovedTransaction{
			{TransactionId: &plaidExtID},
		},
		NextCursor: "c2", HasMore: false,
	}
	mockPlaidService.On("TransactionsSync", ctx, "", "c1", 0).Return(plaidResp, nil)
	mockLinkedItemRepo.On("UpdateCursor", ctx, linkedItem.ID, "c2").Return(nil)
	mockAccountRepo.On("GetByLinkedItemIDIncludingRetained", ctx, linkedItem.ID).Return([]domain.Account{}, nil)

	// GetByExternalID returns the internal transaction (for contribution cleanup)
	internalTxn := &domain.Transaction{ID: internalTxnID}
	mockTransactionRepo.On("GetByExternalID", ctx, domain.SourcePlaid, plaidExtID, "user-remove-1").
		Return(internalTxn, nil)

	// DeleteManagedForTransaction is called with the internal UUID
	mockCleaner.On("DeleteManagedForTransaction", ctx, internalTxnID).Return(nil)

	// DeleteByExternalID is called after the contribution cleanup
	mockTransactionRepo.On("TombstoneByExternalID", ctx, domain.SourcePlaid, plaidExtID, "user-remove-1", domain.TxnDeletedByPlaidSync).
		Return(domain.TxnDeletedProviderRemovedPosted, nil)

	result, err := svc.SyncTransactions(ctx, linkedItem)

	require.NoError(t, err)
	assert.NotNil(t, result)
	mockCleaner.AssertExpectations(t)
	mockTransactionRepo.AssertExpectations(t)
}

// TestSyncTransactions_DeleteManagedContribNonFatalOnError verifies that
// DeleteManagedForTransaction errors don't abort the removal (Codex C4, non-fatal).
func TestSyncTransactions_DeleteManagedContribNonFatalOnError(t *testing.T) {
	mockLinkedItemRepo := new(MockLinkedItemRepository)
	mockTransactionRepo := new(MockTransactionRepository)
	mockAccountRepo := new(MockAccountRepository)
	mockPlaidService := new(MockPlaidService)
	mockCleaner := new(MockSyncContributionCleaner)

	svc := NewTransactionSyncService(
		nil,
		mockLinkedItemRepo,
		mockTransactionRepo,
		mockAccountRepo,
		nil, nil,
		mockPlaidService,
		nil, nil, nil, nil, nil, nil,
	)
	svc.SetSavingsProgressServices(mockCleaner, nil, nil)

	ctx := context.Background()
	cursorRNF := "c1"
	linkedItem := &domain.LinkedItem{ID: "li-remove-nf", UserID: "user-remove-nf", Cursor: &cursorRNF}

	plaidExtID := "plaid-txn-ext-nf"
	internalTxnID := "internal-uuid-nf"

	plaidResp := &plaid.TransactionsSyncResponse{
		Added: []plaid.Transaction{}, Modified: []plaid.Transaction{},
		Removed:    []plaid.RemovedTransaction{{TransactionId: &plaidExtID}},
		NextCursor: "c2", HasMore: false,
	}
	mockPlaidService.On("TransactionsSync", ctx, "", "c1", 0).Return(plaidResp, nil)
	mockLinkedItemRepo.On("UpdateCursor", ctx, linkedItem.ID, "c2").Return(nil)
	mockAccountRepo.On("GetByLinkedItemIDIncludingRetained", ctx, linkedItem.ID).Return([]domain.Account{}, nil)

	internalTxn := &domain.Transaction{ID: internalTxnID}
	mockTransactionRepo.On("GetByExternalID", ctx, domain.SourcePlaid, plaidExtID, "user-remove-nf").
		Return(internalTxn, nil)
	// DeleteManagedForTransaction errors — removal must still proceed
	mockCleaner.On("DeleteManagedForTransaction", ctx, internalTxnID).Return(errors.New("db error"))
	// Transaction delete still happens despite contribution cleanup error
	mockTransactionRepo.On("TombstoneByExternalID", ctx, domain.SourcePlaid, plaidExtID, "user-remove-nf", domain.TxnDeletedByPlaidSync).
		Return(domain.TransactionDeletedReason(""), repository.ErrTransactionNotFound)

	result, err := svc.SyncTransactions(ctx, linkedItem)

	require.NoError(t, err)
	assert.NotNil(t, result)
	mockCleaner.AssertExpectations(t)
	mockTransactionRepo.AssertExpectations(t)
}

// ============================================================================
// Issue #1300 M18 — D6 source-b: Sync detects transfer-ins → proposed contributions
// ============================================================================

// MockSyncContributionsBasisGoalReader is a testify mock for SyncContributionsBasisGoalReader.
type MockSyncContributionsBasisGoalReader struct {
	mock.Mock
}

func (m *MockSyncContributionsBasisGoalReader) GetActiveByUserID(ctx context.Context, userID string) ([]*domain.SavingsGoal, error) {
	args := m.Called(ctx, userID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]*domain.SavingsGoal), args.Error(1)
}

// MockSyncProposedContributionUpserter is a testify mock for SyncProposedContributionUpserter.
type MockSyncProposedContributionUpserter struct {
	mock.Mock
}

func (m *MockSyncProposedContributionUpserter) UpsertProposedFromTransaction(ctx context.Context, userID, goalID, transactionID string, magnitude float64) error {
	args := m.Called(ctx, userID, goalID, transactionID, magnitude)
	return args.Error(0)
}

// TestSyncTransactions_ProposedContributionsCreatedForTransferIn verifies that after a sync,
// for each active contributions-basis goal with a linked account that has a posted inflow,
// a proposed contribution row is upserted (D6 source-b, Issue #1300 M18 / RG1).
// Requirements:
//   - Called BEFORE RecomputeUser
//   - Uses magnitude = ABS(txn.amount) (always positive, from GetInflowTransactionsForAccount)
//   - Non-fatal: sync still returns success even when upserter errors
//   - Does NOT create rows for manual/balance-basis goals
func TestSyncTransactions_ProposedContributionsCreatedForTransferIn(t *testing.T) {
	mockLinkedItemRepo := new(MockLinkedItemRepository)
	mockTransactionRepo := new(MockTransactionRepository)
	mockAccountRepo := new(MockAccountRepository)
	mockPlaidService := new(MockPlaidService)
	mockProgressUpdater := new(MockSyncGoalProgressUpdater)
	mockGoalReader := new(MockSyncContributionsBasisGoalReader)
	mockProposedUpserter := new(MockSyncProposedContributionUpserter)

	svc := NewTransactionSyncService(
		nil,
		mockLinkedItemRepo,
		mockTransactionRepo,
		mockAccountRepo,
		nil, nil,
		mockPlaidService,
		nil, nil, nil, nil, nil, nil,
	)
	svc.SetSavingsProgressServices(nil, mockProgressUpdater, nil)
	svc.SetSavingsDetectionServices(mockGoalReader, mockProposedUpserter, mockTransactionRepo)

	ctx := context.Background()
	cursor1 := "c1"
	userID := "user-detect-1"
	accountID := "acct-savings-1"
	goalID := "goal-contrib-1"

	linkedItem := &domain.LinkedItem{
		ID:     "li-detect-1",
		UserID: userID,
		Cursor: &cursor1,
	}

	plaidResp := &plaid.TransactionsSyncResponse{
		Added:      []plaid.Transaction{},
		Modified:   []plaid.Transaction{},
		Removed:    []plaid.RemovedTransaction{},
		NextCursor: "c2",
		HasMore:    false,
	}
	mockPlaidService.On("TransactionsSync", ctx, "", "c1", 0).Return(plaidResp, nil)
	mockLinkedItemRepo.On("UpdateCursor", ctx, linkedItem.ID, "c2").Return(nil)
	mockAccountRepo.On("GetByLinkedItemIDIncludingRetained", ctx, linkedItem.ID).Return([]domain.Account{}, nil)

	// Goal reader returns one contributions-basis goal linked to accountID
	contributionsGoal := &domain.SavingsGoal{
		ID:            goalID,
		UserID:        userID,
		AccountID:     accountID,
		ProgressBasis: domain.ProgressBasisContributions,
	}
	mockGoalReader.On("GetActiveByUserID", ctx, userID).Return([]*domain.SavingsGoal{contributionsGoal}, nil)

	// Inflow repo returns one transfer-in with magnitude 250.00
	txnID := "txn-inflow-1"
	inflows := []*domain.SavingsInflowTransaction{
		{ID: txnID, Magnitude: 250.00, Date: "2026-06-14"},
	}
	mockTransactionRepo.On("GetInflowTransactionsForAccount", ctx, userID, accountID, mock.AnythingOfType("time.Time"), mock.AnythingOfType("time.Time")).
		Return(inflows, nil)

	// UpsertProposedFromTransaction is called with the exact inflow magnitude
	mockProposedUpserter.On("UpsertProposedFromTransaction", ctx, userID, goalID, txnID, 250.00).Return(nil)

	// RecomputeUser is called after detection (savings progress still runs)
	mockProgressUpdater.On("RecomputeUser", ctx, userID).Return(nil)

	result, err := svc.SyncTransactions(ctx, linkedItem)

	require.NoError(t, err)
	assert.NotNil(t, result)
	mockGoalReader.AssertExpectations(t)
	mockProposedUpserter.AssertExpectations(t)
	mockTransactionRepo.AssertExpectations(t)
	mockProgressUpdater.AssertExpectations(t)
}

// TestSyncTransactions_ProposedContributionsSkipsNonContributionsBasisGoals verifies that
// goals with progress_basis != 'contributions' are skipped during inflow detection (M18).
func TestSyncTransactions_ProposedContributionsSkipsNonContributionsBasisGoals(t *testing.T) {
	mockLinkedItemRepo := new(MockLinkedItemRepository)
	mockTransactionRepo := new(MockTransactionRepository)
	mockAccountRepo := new(MockAccountRepository)
	mockPlaidService := new(MockPlaidService)
	mockGoalReader := new(MockSyncContributionsBasisGoalReader)
	mockProposedUpserter := new(MockSyncProposedContributionUpserter)

	svc := NewTransactionSyncService(
		nil,
		mockLinkedItemRepo,
		mockTransactionRepo,
		mockAccountRepo,
		nil, nil,
		mockPlaidService,
		nil, nil, nil, nil, nil, nil,
	)
	svc.SetSavingsDetectionServices(mockGoalReader, mockProposedUpserter, mockTransactionRepo)

	ctx := context.Background()
	cursorSkip := "c1"
	userID := "user-skip-basis"

	linkedItem := &domain.LinkedItem{
		ID:     "li-skip-basis",
		UserID: userID,
		Cursor: &cursorSkip,
	}

	plaidResp := &plaid.TransactionsSyncResponse{
		Added: []plaid.Transaction{}, Modified: []plaid.Transaction{},
		Removed: []plaid.RemovedTransaction{}, NextCursor: "c2", HasMore: false,
	}
	mockPlaidService.On("TransactionsSync", ctx, "", "c1", 0).Return(plaidResp, nil)
	mockLinkedItemRepo.On("UpdateCursor", ctx, linkedItem.ID, "c2").Return(nil)
	mockAccountRepo.On("GetByLinkedItemIDIncludingRetained", ctx, linkedItem.ID).Return([]domain.Account{}, nil)

	// Return a manual-basis and a balance-basis goal — neither should trigger inflow detection
	goals := []*domain.SavingsGoal{
		{ID: "goal-manual", UserID: userID, AccountID: "acct-1", ProgressBasis: domain.ProgressBasisManual},
		{ID: "goal-balance", UserID: userID, AccountID: "acct-2", ProgressBasis: domain.ProgressBasisBalance},
	}
	mockGoalReader.On("GetActiveByUserID", ctx, userID).Return(goals, nil)

	// GetInflowTransactionsForAccount must NOT be called for non-contributions goals
	// UpsertProposedFromTransaction must NOT be called

	result, err := svc.SyncTransactions(ctx, linkedItem)

	require.NoError(t, err)
	assert.NotNil(t, result)
	mockGoalReader.AssertExpectations(t)
	// These must not be called: assert zero calls via NotCalled
	mockProposedUpserter.AssertNotCalled(t, "UpsertProposedFromTransaction")
	mockTransactionRepo.AssertNotCalled(t, "GetInflowTransactionsForAccount")
}

// TestSyncTransactions_ProposedContributionsSkipsGoalWithNoAccountID verifies that
// contributions-basis goals without a linked account_id are silently skipped (M18).
func TestSyncTransactions_ProposedContributionsSkipsGoalWithNoAccountID(t *testing.T) {
	mockLinkedItemRepo := new(MockLinkedItemRepository)
	mockTransactionRepo := new(MockTransactionRepository)
	mockAccountRepo := new(MockAccountRepository)
	mockPlaidService := new(MockPlaidService)
	mockGoalReader := new(MockSyncContributionsBasisGoalReader)
	mockProposedUpserter := new(MockSyncProposedContributionUpserter)

	svc := NewTransactionSyncService(
		nil,
		mockLinkedItemRepo,
		mockTransactionRepo,
		mockAccountRepo,
		nil, nil,
		mockPlaidService,
		nil, nil, nil, nil, nil, nil,
	)
	svc.SetSavingsDetectionServices(mockGoalReader, mockProposedUpserter, mockTransactionRepo)

	ctx := context.Background()
	cursorNoAcct := "c1"
	userID := "user-no-acct"

	linkedItem := &domain.LinkedItem{
		ID:     "li-no-acct",
		UserID: userID,
		Cursor: &cursorNoAcct,
	}

	plaidResp := &plaid.TransactionsSyncResponse{
		Added: []plaid.Transaction{}, Modified: []plaid.Transaction{},
		Removed: []plaid.RemovedTransaction{}, NextCursor: "c2", HasMore: false,
	}
	mockPlaidService.On("TransactionsSync", ctx, "", "c1", 0).Return(plaidResp, nil)
	mockLinkedItemRepo.On("UpdateCursor", ctx, linkedItem.ID, "c2").Return(nil)
	mockAccountRepo.On("GetByLinkedItemIDIncludingRetained", ctx, linkedItem.ID).Return([]domain.Account{}, nil)

	// Contributions-basis goal but no AccountID — detection must be skipped
	goals := []*domain.SavingsGoal{
		{ID: "goal-no-acct", UserID: userID, AccountID: "", ProgressBasis: domain.ProgressBasisContributions},
	}
	mockGoalReader.On("GetActiveByUserID", ctx, userID).Return(goals, nil)

	result, err := svc.SyncTransactions(ctx, linkedItem)

	require.NoError(t, err)
	assert.NotNil(t, result)
	mockGoalReader.AssertExpectations(t)
	mockProposedUpserter.AssertNotCalled(t, "UpsertProposedFromTransaction")
	mockTransactionRepo.AssertNotCalled(t, "GetInflowTransactionsForAccount")
}

// TestSyncTransactions_ProposedContributionUpsertErrorIsNonFatal verifies that a failure
// in UpsertProposedFromTransaction for one txn does not abort the sync (M18 non-fatal per-txn).
func TestSyncTransactions_ProposedContributionUpsertErrorIsNonFatal(t *testing.T) {
	mockLinkedItemRepo := new(MockLinkedItemRepository)
	mockTransactionRepo := new(MockTransactionRepository)
	mockAccountRepo := new(MockAccountRepository)
	mockPlaidService := new(MockPlaidService)
	mockProgressUpdater := new(MockSyncGoalProgressUpdater)
	mockGoalReader := new(MockSyncContributionsBasisGoalReader)
	mockProposedUpserter := new(MockSyncProposedContributionUpserter)

	svc := NewTransactionSyncService(
		nil,
		mockLinkedItemRepo,
		mockTransactionRepo,
		mockAccountRepo,
		nil, nil,
		mockPlaidService,
		nil, nil, nil, nil, nil, nil,
	)
	svc.SetSavingsProgressServices(nil, mockProgressUpdater, nil)
	svc.SetSavingsDetectionServices(mockGoalReader, mockProposedUpserter, mockTransactionRepo)

	ctx := context.Background()
	cursorUE := "c1"
	userID := "user-upsert-err"
	accountID := "acct-upsert-err"
	goalID := "goal-upsert-err"

	linkedItem := &domain.LinkedItem{
		ID:     "li-upsert-err",
		UserID: userID,
		Cursor: &cursorUE,
	}

	plaidResp := &plaid.TransactionsSyncResponse{
		Added: []plaid.Transaction{}, Modified: []plaid.Transaction{},
		Removed: []plaid.RemovedTransaction{}, NextCursor: "c2", HasMore: false,
	}
	mockPlaidService.On("TransactionsSync", ctx, "", "c1", 0).Return(plaidResp, nil)
	mockLinkedItemRepo.On("UpdateCursor", ctx, linkedItem.ID, "c2").Return(nil)
	mockAccountRepo.On("GetByLinkedItemIDIncludingRetained", ctx, linkedItem.ID).Return([]domain.Account{}, nil)

	goals := []*domain.SavingsGoal{
		{ID: goalID, UserID: userID, AccountID: accountID, ProgressBasis: domain.ProgressBasisContributions},
	}
	mockGoalReader.On("GetActiveByUserID", ctx, userID).Return(goals, nil)

	inflows := []*domain.SavingsInflowTransaction{
		{ID: "txn-fail-1", Magnitude: 100.00, Date: "2026-06-14"},
		{ID: "txn-ok-1", Magnitude: 200.00, Date: "2026-06-14"},
	}
	mockTransactionRepo.On("GetInflowTransactionsForAccount", ctx, userID, accountID, mock.AnythingOfType("time.Time"), mock.AnythingOfType("time.Time")).
		Return(inflows, nil)

	// First txn fails to upsert — second should still be attempted
	mockProposedUpserter.On("UpsertProposedFromTransaction", ctx, userID, goalID, "txn-fail-1", 100.00).
		Return(errors.New("db error"))
	mockProposedUpserter.On("UpsertProposedFromTransaction", ctx, userID, goalID, "txn-ok-1", 200.00).
		Return(nil)

	// RecomputeUser still runs after the non-fatal upsert errors
	mockProgressUpdater.On("RecomputeUser", ctx, userID).Return(nil)

	result, err := svc.SyncTransactions(ctx, linkedItem)

	require.NoError(t, err) // non-fatal
	assert.NotNil(t, result)
	mockGoalReader.AssertExpectations(t)
	mockProposedUpserter.AssertExpectations(t)
	mockProgressUpdater.AssertExpectations(t)
}

// TestSyncTransactions_ProposedContributionsNilSafeWhenNotWired verifies that
// sync proceeds normally when SetSavingsDetectionServices has not been called (M18 nil-safe).
func TestSyncTransactions_ProposedContributionsNilSafeWhenNotWired(t *testing.T) {
	mockLinkedItemRepo := new(MockLinkedItemRepository)
	mockTransactionRepo := new(MockTransactionRepository)
	mockAccountRepo := new(MockAccountRepository)
	mockPlaidService := new(MockPlaidService)

	svc := NewTransactionSyncService(
		nil,
		mockLinkedItemRepo,
		mockTransactionRepo,
		mockAccountRepo,
		nil, nil,
		mockPlaidService,
		nil, nil, nil, nil, nil, nil,
	)
	// No SetSavingsDetectionServices call — all three fields are nil

	ctx := context.Background()
	cursorNilSafe := "c1"
	linkedItem := &domain.LinkedItem{
		ID:     "li-detect-nil",
		UserID: "user-detect-nil",
		Cursor: &cursorNilSafe,
	}

	plaidResp := &plaid.TransactionsSyncResponse{
		Added: []plaid.Transaction{}, Modified: []plaid.Transaction{},
		Removed: []plaid.RemovedTransaction{}, NextCursor: "c2", HasMore: false,
	}
	mockPlaidService.On("TransactionsSync", ctx, "", "c1", 0).Return(plaidResp, nil)
	mockLinkedItemRepo.On("UpdateCursor", ctx, linkedItem.ID, "c2").Return(nil)
	mockAccountRepo.On("GetByLinkedItemIDIncludingRetained", ctx, linkedItem.ID).Return([]domain.Account{}, nil)

	result, err := svc.SyncTransactions(ctx, linkedItem)

	require.NoError(t, err)
	assert.NotNil(t, result)
}

// ============================================================================
// Issue #1586: the stranded-settlement pull-forward is WIRED into the storage path
// ============================================================================

// TestProcessAddedTransactions_PullsForwardStrandedSettlementAnnotations pins the wiring the
// repository change is useless without. The SQL is proved by the integration suite
// (repository/transaction_late_settlement_integration_test.go); what a mock can prove, and what
// nothing else does, is that the storage path calls it at all and hands it the right ids.
//
// It also pins the FILTER, which is the part that is easy to get subtly wrong: only rows carrying
// Plaid's pending_transaction_id are candidates. Sending the whole batch would work but would ask
// the database to scan ids that cannot possibly match, on every sync.
func TestProcessAddedTransactions_PullsForwardStrandedSettlementAnnotations(t *testing.T) {
	mockTransactionRepo := new(MockTransactionRepository)

	service := NewTransactionSyncService(
		nil, nil, mockTransactionRepo, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil,
	).(*transactionSyncService)

	ctx := context.Background()
	linkedItem := &domain.LinkedItem{ID: "linked-item-1586", UserID: "user-1586"}
	accountMap := map[string]string{"plaid-acc-1": "account-uuid-1"}

	settled := plaid.NewNullableString(strPtrSvc("plaid-pending-999"))
	added := []plaid.Transaction{
		{TransactionId: "plaid-posted-1", AccountId: "plaid-acc-1", Amount: 10, Date: "2024-01-15",
			Name: "Posted twin", PendingTransactionId: *settled},
		{TransactionId: "plaid-ordinary-2", AccountId: "plaid-acc-1", Amount: 20, Date: "2024-01-16",
			Name: "Ordinary"},
	}

	var storedIDs []string
	mockTransactionRepo.On("CreateBatch", ctx, mock.MatchedBy(func(txns []domain.Transaction) bool {
		storedIDs = nil
		for i := range txns {
			storedIDs = append(storedIDs, txns[i].ID)
		}
		return len(txns) == 2
	})).Return(nil, nil)

	var pulledFor []string
	mockTransactionRepo.On("PullForwardSettledPendingAnnotations", ctx, "user-1586",
		mock.MatchedBy(func(ids []string) bool { pulledFor = ids; return true })).
		Return(int64(1), nil)

	require.NoError(t, service.processAddedTransactions(ctx, added, linkedItem, accountMap, nil))

	mockTransactionRepo.AssertExpectations(t)
	require.Len(t, pulledFor, 1,
		"only the row carrying Plaid's pending_transaction_id is a settlement candidate")
	assert.Equal(t, storedIDs[0], pulledFor[0],
		"the id handed to the repository is the id the batch was stored under, not the provider's")
}

// TestProcessAddedTransactions_NoPointerMeansNoPullForward: the overwhelming majority of every sync
// batch is not a settlement, and a database round-trip per batch that can match nothing is a cost
// paid on every sync of every user.
func TestProcessAddedTransactions_NoPointerMeansNoPullForward(t *testing.T) {
	mockTransactionRepo := new(MockTransactionRepository)

	service := NewTransactionSyncService(
		nil, nil, mockTransactionRepo, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil,
	).(*transactionSyncService)

	ctx := context.Background()
	linkedItem := &domain.LinkedItem{ID: "linked-item-1586b", UserID: "user-1586b"}
	accountMap := map[string]string{"plaid-acc-1": "account-uuid-1"}

	added := []plaid.Transaction{
		{TransactionId: "plaid-ordinary-1", AccountId: "plaid-acc-1", Amount: 10, Date: "2024-01-15", Name: "Ordinary"},
	}
	mockTransactionRepo.On("CreateBatch", ctx, mock.Anything).Return(nil, nil)

	require.NoError(t, service.processAddedTransactions(ctx, added, linkedItem, accountMap, nil))

	// mockassert.NoCallTo, not AssertNotCalled with a bare method name: the latter is a NO-OP for
	// any method taking arguments, and this repository has a guard test that says so.
	mockassert.NoCallTo(t, &mockTransactionRepo.Mock, "PullForwardSettledPendingAnnotations")
}

// TestProcessAddedTransactions_PullForwardFailureReturns.
//
// ⚠ THIS TEST USED TO ASSERT THE OPPOSITE, and its reason was the false comment that made the
// defect look acceptable: "failing the sync for it would stall the cursor and re-deliver the whole
// page, and the repository call is idempotent so the next sync retries for free". The re-delivery
// it dismissed as a cost is the only thing that CAN retry the repair — swallowing the error let the
// caller advance Plaid's cursor, and /transactions/sync does not re-deliver a transaction the
// stored cursor is past, so the promised free retry could never happen and the user's annotations
// stranded permanently. Returning is what makes the retry real; see
// transaction_sync_settlement_repair_cursor_test.go for the cursor half of the contract.
func TestProcessAddedTransactions_PullForwardFailureReturns(t *testing.T) {
	mockTransactionRepo := new(MockTransactionRepository)

	service := NewTransactionSyncService(
		nil, nil, mockTransactionRepo, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil,
	).(*transactionSyncService)

	ctx := context.Background()
	linkedItem := &domain.LinkedItem{ID: "linked-item-1586c", UserID: "user-1586c"}
	accountMap := map[string]string{"plaid-acc-1": "account-uuid-1"}

	settled := plaid.NewNullableString(strPtrSvc("plaid-pending-777"))
	added := []plaid.Transaction{
		{TransactionId: "plaid-posted-1", AccountId: "plaid-acc-1", Amount: 10, Date: "2024-01-15",
			Name: "Posted twin", PendingTransactionId: *settled},
	}
	mockTransactionRepo.On("CreateBatch", ctx, mock.Anything).Return(nil, nil)
	mockTransactionRepo.On("PullForwardSettledPendingAnnotations", ctx, "user-1586c", mock.Anything).
		Return(int64(0), errors.New("deadlock detected"))

	assert.Error(t, service.processAddedTransactions(ctx, added, linkedItem, accountMap, nil),
		"a failed repair must return, so the caller holds the cursor and Plaid re-delivers the page")
	mockTransactionRepo.AssertExpectations(t)
}

func strPtrSvc(s string) *string { return &s }
