package services

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/plaid/plaid-go/v23/plaid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
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
	mockEncryptionService := new(MockEncryptionService)

	service := NewTransactionSyncService(
		mockWebhookRepo,
		mockLinkedItemRepo,
		mockTransactionRepo,
		mockAccountRepo,
		mockPlaidService,
		mockEncryptionService,
	)

	ctx := context.Background()
	webhookID := "webhook-123"

	// Mock webhook event with item_id
	itemID := "item-abc"
	webhookEvent := &domain.WebhookEvent{
		ID:           webhookID,
		WebhookType:  "TRANSACTIONS",
		WebhookCode:  stringPtr("DEFAULT_UPDATE"),
		ItemID:       &itemID,
		Payload:      json.RawMessage(`{"webhook_type":"TRANSACTIONS","item_id":"item-abc"}`),
		Processed:    false,
		RetryCount:   0,
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
	}

	// Mock linked item with encrypted access token
	linkedItem := &domain.LinkedItem{
		ID:                   "linked-item-123",
		UserID:               "user-123",
		ItemID:               itemID,
		AccessTokenEncrypted: "encrypted-access-token",
		Cursor:               stringPtr("old-cursor"),
		Provider:             "plaid",
		CreatedAt:            time.Now(),
		UpdatedAt:            time.Now(),
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

	// Mock decryption of access token
	mockEncryptionService.On("Decrypt", "encrypted-access-token").Return("decrypted-access-token", nil)

	// Mock Plaid API call for TransactionsSync
	plaidResp := &plaid.TransactionsSyncResponse{
		Added:      []plaid.Transaction{{TransactionId: "txn-1"}},
		Modified:   []plaid.Transaction{},
		Removed:    []plaid.RemovedTransaction{},
		NextCursor: "new-cursor-from-plaid",
		HasMore:    false,
	}
	mockPlaidService.On("TransactionsSync", ctx, "decrypted-access-token", "old-cursor").Return(plaidResp, nil)

	// Mock cursor update (after sync completes)
	mockLinkedItemRepo.On("UpdateCursor", ctx, linkedItem.ID, "new-cursor-from-plaid").Return(nil)

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
	mockEncryptionService := new(MockEncryptionService)

	service := NewTransactionSyncService(
		mockWebhookRepo,
		mockLinkedItemRepo,
		mockTransactionRepo,
		mockAccountRepo,
		mockPlaidService,
		mockEncryptionService,
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
	mockEncryptionService := new(MockEncryptionService)

	service := NewTransactionSyncService(
		mockWebhookRepo,
		mockLinkedItemRepo,
		mockTransactionRepo,
		mockAccountRepo,
		mockPlaidService,
		mockEncryptionService,
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
	mockEncryptionService := new(MockEncryptionService)

	service := NewTransactionSyncService(
		mockWebhookRepo,
		mockLinkedItemRepo,
		mockTransactionRepo,
		mockAccountRepo,
		mockPlaidService,
		mockEncryptionService,
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
	mockPlaidService := new(MockPlaidService)
	mockEncryptionService := new(MockEncryptionService)

	service := NewTransactionSyncService(
		nil, // webhookRepo not needed for SyncTransactions
		mockLinkedItemRepo,
		nil, // transactionRepo not needed yet (issue #261)
		nil, // accountRepo not needed yet (issue #261)
		mockPlaidService,
		mockEncryptionService,
	)

	ctx := context.Background()
	linkedItem := &domain.LinkedItem{
		ID:                   "linked-item-123",
		UserID:               "user-123",
		ItemID:               "item-abc",
		AccessTokenEncrypted: "encrypted-token",
		Cursor:               stringPtr("cursor-before"),
	}

	// Mock decryption
	mockEncryptionService.On("Decrypt", "encrypted-token").Return("decrypted-token", nil)

	// Mock Plaid API response (single page, has_more=false)
	plaidResp := &plaid.TransactionsSyncResponse{
		Added: []plaid.Transaction{
			{TransactionId: "txn-1", Amount: 10.0},
			{TransactionId: "txn-2", Amount: 20.0},
		},
		Modified:   []plaid.Transaction{},
		Removed:    []plaid.RemovedTransaction{},
		NextCursor: "cursor-after",
		HasMore:    false,
	}
	mockPlaidService.On("TransactionsSync", ctx, "decrypted-token", "cursor-before").Return(plaidResp, nil)

	// Mock cursor update
	mockLinkedItemRepo.On("UpdateCursor", ctx, linkedItem.ID, "cursor-after").Return(nil)

	// Execute
	result, err := service.SyncTransactions(ctx, linkedItem)

	// Assert
	assert.NoError(t, err)
	assert.NotNil(t, result)
	assert.Len(t, result.Added, 2)
	assert.Len(t, result.Modified, 0)
	assert.Len(t, result.Removed, 0)
	assert.Equal(t, "cursor-after", result.NewCursor)

	mockEncryptionService.AssertExpectations(t)
	mockPlaidService.AssertExpectations(t)
	mockLinkedItemRepo.AssertExpectations(t)
}

// TestSyncTransactions_SuccessMultiplePages tests pagination with has_more=true
func TestSyncTransactions_SuccessMultiplePages(t *testing.T) {
	mockLinkedItemRepo := new(MockLinkedItemRepository)
	mockPlaidService := new(MockPlaidService)
	mockEncryptionService := new(MockEncryptionService)

	service := NewTransactionSyncService(
		nil,
		mockLinkedItemRepo,
		nil,
		nil,
		mockPlaidService,
		mockEncryptionService,
	)

	ctx := context.Background()
	linkedItem := &domain.LinkedItem{
		ID:                   "linked-item-123",
		AccessTokenEncrypted: "encrypted-token",
		Cursor:               stringPtr("cursor-page0"),
	}

	// Mock decryption
	mockEncryptionService.On("Decrypt", "encrypted-token").Return("decrypted-token", nil)

	// Page 1: has_more=true
	plaidResp1 := &plaid.TransactionsSyncResponse{
		Added:      []plaid.Transaction{{TransactionId: "txn-1"}},
		Modified:   []plaid.Transaction{{TransactionId: "txn-mod-1"}},
		Removed:    []plaid.RemovedTransaction{{TransactionId: stringPtr("txn-rem-1")}},
		NextCursor: "cursor-page1",
		HasMore:    true,
	}
	mockPlaidService.On("TransactionsSync", ctx, "decrypted-token", "cursor-page0").Return(plaidResp1, nil)
	mockLinkedItemRepo.On("UpdateCursor", ctx, linkedItem.ID, "cursor-page1").Return(nil).Once()

	// Page 2: has_more=false
	plaidResp2 := &plaid.TransactionsSyncResponse{
		Added:      []plaid.Transaction{{TransactionId: "txn-2"}},
		Modified:   []plaid.Transaction{},
		Removed:    []plaid.RemovedTransaction{},
		NextCursor: "cursor-page2",
		HasMore:    false,
	}
	mockPlaidService.On("TransactionsSync", ctx, "decrypted-token", "cursor-page1").Return(plaidResp2, nil)
	mockLinkedItemRepo.On("UpdateCursor", ctx, linkedItem.ID, "cursor-page2").Return(nil).Once()

	// Execute
	result, err := service.SyncTransactions(ctx, linkedItem)

	// Assert - aggregated results from both pages
	assert.NoError(t, err)
	assert.Len(t, result.Added, 2) // txn-1 + txn-2
	assert.Len(t, result.Modified, 1) // txn-mod-1
	assert.Len(t, result.Removed, 1) // txn-rem-1
	assert.Equal(t, "cursor-page2", result.NewCursor)

	mockEncryptionService.AssertExpectations(t)
	mockPlaidService.AssertExpectations(t)
	mockLinkedItemRepo.AssertExpectations(t)
}

// TestSyncTransactions_DecryptionError tests handling of access token decryption failure
func TestSyncTransactions_DecryptionError(t *testing.T) {
	mockEncryptionService := new(MockEncryptionService)

	service := NewTransactionSyncService(
		nil,
		nil,
		nil,
		nil,
		nil,
		mockEncryptionService,
	)

	ctx := context.Background()
	linkedItem := &domain.LinkedItem{
		ID:                   "linked-item-123",
		AccessTokenEncrypted: "bad-encrypted-token",
	}

	// Mock decryption failure
	mockEncryptionService.On("Decrypt", "bad-encrypted-token").Return("", errors.New("decryption failed"))

	// Execute
	result, err := service.SyncTransactions(ctx, linkedItem)

	// Assert
	assert.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "failed to decrypt access token")

	mockEncryptionService.AssertExpectations(t)
}

// TestSyncTransactions_PlaidErrorFirstPage tests Plaid API error on first page
func TestSyncTransactions_PlaidErrorFirstPage(t *testing.T) {
	mockLinkedItemRepo := new(MockLinkedItemRepository)
	mockPlaidService := new(MockPlaidService)
	mockEncryptionService := new(MockEncryptionService)

	service := NewTransactionSyncService(
		nil,
		mockLinkedItemRepo,
		nil,
		nil,
		mockPlaidService,
		mockEncryptionService,
	)

	ctx := context.Background()
	linkedItem := &domain.LinkedItem{
		ID:                   "linked-item-123",
		AccessTokenEncrypted: "encrypted-token",
		Cursor:               nil, // First sync
	}

	// Mock decryption
	mockEncryptionService.On("Decrypt", "encrypted-token").Return("decrypted-token", nil)

	// Mock Plaid API error
	mockPlaidService.On("TransactionsSync", ctx, "decrypted-token", "").
		Return(nil, errors.New("INVALID_ACCESS_TOKEN"))

	// Execute
	result, err := service.SyncTransactions(ctx, linkedItem)

	// Assert - returns empty result with error
	assert.Error(t, err)
	assert.NotNil(t, result) // Partial results returned
	assert.Len(t, result.Added, 0)
	assert.Contains(t, err.Error(), "sync failed on page 1")

	mockEncryptionService.AssertExpectations(t)
	mockPlaidService.AssertExpectations(t)
}

// TestSyncTransactions_PlaidErrorSubsequentPage tests Plaid API error on page 2 (resumability)
func TestSyncTransactions_PlaidErrorSubsequentPage(t *testing.T) {
	mockLinkedItemRepo := new(MockLinkedItemRepository)
	mockPlaidService := new(MockPlaidService)
	mockEncryptionService := new(MockEncryptionService)

	service := NewTransactionSyncService(
		nil,
		mockLinkedItemRepo,
		nil,
		nil,
		mockPlaidService,
		mockEncryptionService,
	)

	ctx := context.Background()
	linkedItem := &domain.LinkedItem{
		ID:                   "linked-item-123",
		AccessTokenEncrypted: "encrypted-token",
		Cursor:               stringPtr("cursor-start"),
	}

	// Mock decryption
	mockEncryptionService.On("Decrypt", "encrypted-token").Return("decrypted-token", nil)

	// Page 1 succeeds
	plaidResp1 := &plaid.TransactionsSyncResponse{
		Added:      []plaid.Transaction{{TransactionId: "txn-1"}},
		NextCursor: "cursor-page1",
		HasMore:    true,
	}
	mockPlaidService.On("TransactionsSync", ctx, "decrypted-token", "cursor-start").Return(plaidResp1, nil)
	mockLinkedItemRepo.On("UpdateCursor", ctx, linkedItem.ID, "cursor-page1").Return(nil).Once()

	// Page 2 fails
	mockPlaidService.On("TransactionsSync", ctx, "decrypted-token", "cursor-page1").
		Return(nil, errors.New("RATE_LIMIT_EXCEEDED"))

	// Execute
	result, err := service.SyncTransactions(ctx, linkedItem)

	// Assert - returns partial results from page 1
	assert.Error(t, err)
	assert.NotNil(t, result)
	assert.Len(t, result.Added, 1) // Got page 1 data
	assert.Contains(t, err.Error(), "sync failed on page 2")

	// Cursor was updated after page 1, so retry can resume from page 2
	mockLinkedItemRepo.AssertExpectations(t)
}

// TestSyncTransactions_CursorUpdateError tests cursor update failure
func TestSyncTransactions_CursorUpdateError(t *testing.T) {
	mockLinkedItemRepo := new(MockLinkedItemRepository)
	mockPlaidService := new(MockPlaidService)
	mockEncryptionService := new(MockEncryptionService)

	service := NewTransactionSyncService(
		nil,
		mockLinkedItemRepo,
		nil,
		nil,
		mockPlaidService,
		mockEncryptionService,
	)

	ctx := context.Background()
	linkedItem := &domain.LinkedItem{
		ID:                   "linked-item-123",
		AccessTokenEncrypted: "encrypted-token",
		Cursor:               nil,
	}

	// Mock decryption
	mockEncryptionService.On("Decrypt", "encrypted-token").Return("decrypted-token", nil)

	// Plaid API succeeds
	plaidResp := &plaid.TransactionsSyncResponse{
		Added:      []plaid.Transaction{{TransactionId: "txn-1"}},
		NextCursor: "cursor-new",
		HasMore:    false,
	}
	mockPlaidService.On("TransactionsSync", ctx, "decrypted-token", "").Return(plaidResp, nil)

	// Cursor update fails
	mockLinkedItemRepo.On("UpdateCursor", ctx, linkedItem.ID, "cursor-new").
		Return(errors.New("database connection error"))

	// Execute
	result, err := service.SyncTransactions(ctx, linkedItem)

	// Assert - returns partial results with error
	assert.Error(t, err)
	assert.NotNil(t, result)
	assert.Len(t, result.Added, 1) // Got transaction data
	assert.Contains(t, err.Error(), "failed to update cursor on page 1")

	mockLinkedItemRepo.AssertExpectations(t)
}

// TestSyncTransactions_EmptyCursor tests first sync with no cursor
func TestSyncTransactions_EmptyCursor(t *testing.T) {
	mockLinkedItemRepo := new(MockLinkedItemRepository)
	mockPlaidService := new(MockPlaidService)
	mockEncryptionService := new(MockEncryptionService)

	service := NewTransactionSyncService(
		nil,
		mockLinkedItemRepo,
		nil,
		nil,
		mockPlaidService,
		mockEncryptionService,
	)

	ctx := context.Background()
	linkedItem := &domain.LinkedItem{
		ID:                   "linked-item-123",
		AccessTokenEncrypted: "encrypted-token",
		Cursor:               nil, // First sync - no cursor
	}

	// Mock decryption
	mockEncryptionService.On("Decrypt", "encrypted-token").Return("decrypted-token", nil)

	// Mock Plaid API with empty cursor (empty string passed)
	plaidResp := &plaid.TransactionsSyncResponse{
		Added:      []plaid.Transaction{{TransactionId: "txn-1"}},
		NextCursor: "cursor-initial",
		HasMore:    false,
	}
	mockPlaidService.On("TransactionsSync", ctx, "decrypted-token", "").Return(plaidResp, nil)
	mockLinkedItemRepo.On("UpdateCursor", ctx, linkedItem.ID, "cursor-initial").Return(nil)

	// Execute
	result, err := service.SyncTransactions(ctx, linkedItem)

	// Assert
	assert.NoError(t, err)
	assert.NotNil(t, result)
	assert.Len(t, result.Added, 1)
	assert.Equal(t, "cursor-initial", result.NewCursor)

	mockPlaidService.AssertExpectations(t)
}

// Helper function to create string pointers
func stringPtr(s string) *string {
	return &s
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
		nil,
		nil,
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
	mockAccountRepo.On("GetByLinkedItemID", ctx, linkedItemID).Return(accounts, nil)

	// Execute
	accountMap, err := service.buildAccountMap(ctx, linkedItemID)

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
		nil,
		nil,
	).(*transactionSyncService)

	ctx := context.Background()
	linkedItemID := "linked-item-123"

	// Mock: GetByLinkedItemID returns empty slice
	mockAccountRepo.On("GetByLinkedItemID", ctx, linkedItemID).Return([]domain.Account{}, nil)

	// Execute
	accountMap, err := service.buildAccountMap(ctx, linkedItemID)

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
		nil,
		nil,
	).(*transactionSyncService)

	ctx := context.Background()
	linkedItemID := "linked-item-123"

	// Mock: GetByLinkedItemID returns error
	mockAccountRepo.On("GetByLinkedItemID", ctx, linkedItemID).Return(nil, errors.New("database error"))

	// Execute
	accountMap, err := service.buildAccountMap(ctx, linkedItemID)

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
		nil,
		nil,
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
	linkedItem := &domain.LinkedItem{
		ID:     linkedItemID,
		UserID: userID,
	}

	// 3 Plaid transactions to add
	added := []plaid.Transaction{
		{
			TransactionId: "plaid-txn-1",
			AccountId:     "plaid-acc-1",
			Amount:        100.00,
			Date:          "2024-01-15",
			Name:          "Transaction 1",
			Pending:       false,
		},
		{
			TransactionId: "plaid-txn-2",
			AccountId:     "plaid-acc-2",
			Amount:        50.00,
			Date:          "2024-01-16",
			Name:          "Transaction 2",
			Pending:       false,
		},
		{
			TransactionId: "plaid-txn-3",
			AccountId:     "plaid-acc-3",
			Amount:        25.00,
			Date:          "2024-01-17",
			Name:          "Transaction 3",
			Pending:       true,
		},
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
	})).Return(nil)

	// Execute
	err := service.processAddedTransactions(ctx, added, linkedItem, accountMap)

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
		nil,
		nil,
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
	})).Return(nil)

	// Execute
	err := service.processAddedTransactions(ctx, added, linkedItem, accountMap)

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
		nil,
		nil,
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
	})).Return(nil)

	// Execute
	err := service.processAddedTransactions(ctx, added, linkedItem, accountMap)

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
		nil,
		nil,
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
	err := service.processAddedTransactions(ctx, added, linkedItem, accountMap)

	// Assert
	assert.NoError(t, err)
	// CreateBatch should NOT have been called
	mockTransactionRepo.AssertNotCalled(t, "CreateBatch")
}
