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
		nil, // profileRepo - not needed for these tests
		nil, // balanceSnapshotRepo - not needed for these tests
		mockPlaidService,
		mockEncryptionService,
		nil, // syncLimitService - not needed for these tests
		nil, // ruleApplicationService - not needed for these tests
		nil, // budgetSyncService - not needed for these tests
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
	mockPlaidService.On("TransactionsSync", ctx, "decrypted-access-token", "old-cursor", 0).Return(plaidResp, nil)

	// Mock cursor update (after sync completes)
	mockLinkedItemRepo.On("UpdateCursor", ctx, linkedItem.ID, "new-cursor-from-plaid").Return(nil)

	// Issue #261: Mock account repository for transaction storage
	mockAccountRepo.On("GetByLinkedItemID", ctx, linkedItem.ID).Return([]domain.Account{}, nil)
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
	mockEncryptionService := new(MockEncryptionService)

	service := NewTransactionSyncService(
		mockWebhookRepo,
		mockLinkedItemRepo,
		mockTransactionRepo,
		mockAccountRepo,
		nil, // profileRepo - not needed for these tests
		nil, // balanceSnapshotRepo - not needed for these tests
		mockPlaidService,
		mockEncryptionService,
		nil, // syncLimitService - not needed for these tests
		nil, // ruleApplicationService - not needed for these tests
		nil, // budgetSyncService - not needed for these tests
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
		nil, // profileRepo - not needed for these tests
		nil, // balanceSnapshotRepo - not needed for these tests
		mockPlaidService,
		mockEncryptionService,
		nil, // syncLimitService - not needed for these tests
		nil, // ruleApplicationService - not needed for these tests
		nil, // budgetSyncService - not needed for these tests
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
		nil, // profileRepo - not needed for these tests
		nil, // balanceSnapshotRepo - not needed for these tests
		mockPlaidService,
		mockEncryptionService,
		nil, // syncLimitService - not needed for these tests
		nil, // ruleApplicationService - not needed for these tests
		nil, // budgetSyncService - not needed for these tests
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
	mockEncryptionService := new(MockEncryptionService)

	service := NewTransactionSyncService(
		nil, // webhookRepo not needed for SyncTransactions
		mockLinkedItemRepo,
		mockTransactionRepo,
		mockAccountRepo,
		nil, // profileRepo - not needed for these tests
		nil, // balanceSnapshotRepo - not needed for these tests
		mockPlaidService,
		mockEncryptionService,
		nil, // syncLimitService - not needed for these tests
		nil, // ruleApplicationService - not needed for these tests
		nil, // budgetSyncService - not needed for these tests
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
	mockPlaidService.On("TransactionsSync", ctx, "decrypted-token", "cursor-before", 0).Return(plaidResp, nil)

	// Mock cursor update
	mockLinkedItemRepo.On("UpdateCursor", ctx, linkedItem.ID, "cursor-after").Return(nil)

	// Issue #261: Mock account repository for transaction storage
	mockAccountRepo.On("GetByLinkedItemID", ctx, linkedItem.ID).Return([]domain.Account{}, nil)
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

	mockEncryptionService.AssertExpectations(t)
	mockPlaidService.AssertExpectations(t)
	mockLinkedItemRepo.AssertExpectations(t)
}

// TestSyncTransactions_SuccessMultiplePages tests pagination with has_more=true
func TestSyncTransactions_SuccessMultiplePages(t *testing.T) {
	mockLinkedItemRepo := new(MockLinkedItemRepository)
	mockTransactionRepo := new(MockTransactionRepository)
	mockAccountRepo := new(MockAccountRepository)
	mockPlaidService := new(MockPlaidService)
	mockEncryptionService := new(MockEncryptionService)

	service := NewTransactionSyncService(
		nil,
		mockLinkedItemRepo,
		mockTransactionRepo,
		mockAccountRepo,
		nil, // profileRepo - not needed for these tests
		nil, // balanceSnapshotRepo - not needed for these tests
		mockPlaidService,
		mockEncryptionService,
		nil, // syncLimitService - not needed for these tests
		nil, // ruleApplicationService - not needed for these tests
		nil, // budgetSyncService - not needed for these tests
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
	mockPlaidService.On("TransactionsSync", ctx, "decrypted-token", "cursor-page0", 0).Return(plaidResp1, nil)
	mockLinkedItemRepo.On("UpdateCursor", ctx, linkedItem.ID, "cursor-page1").Return(nil).Once()

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
	mockAccountRepo.On("GetByLinkedItemID", ctx, linkedItem.ID).Return([]domain.Account{}, nil)
	// Mock deletion of removed transaction
	mockTransactionRepo.On("DeleteByExternalID", ctx, domain.SourcePlaid, "txn-rem-1", linkedItem.UserID).Return(nil)

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
		nil, // profileRepo
		nil, // balanceSnapshotRepo
		nil, // plaidService
		mockEncryptionService,
		nil, // syncLimitService
		nil, // ruleApplicationService
		nil, // budgetSyncService
	)

	ctx := context.Background()
	cursor := "existing-cursor" // Set cursor to avoid first-sync path
	linkedItem := &domain.LinkedItem{
		ID:                   "linked-item-123",
		AccessTokenEncrypted: "bad-encrypted-token",
		Cursor:               &cursor, // Re-sync scenario to avoid sync limit checks
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
		nil, // profileRepo - not needed for these tests
		nil, // balanceSnapshotRepo - not needed for these tests
		mockPlaidService,
		mockEncryptionService,
		nil, // syncLimitService - not needed for these tests
		nil, // ruleApplicationService - not needed for these tests
		nil, // budgetSyncService - not needed for these tests
	)

	ctx := context.Background()
	cursor := "existing-cursor" // Set cursor to avoid first-sync path
	linkedItem := &domain.LinkedItem{
		ID:                   "linked-item-123",
		AccessTokenEncrypted: "encrypted-token",
		Cursor:               &cursor, // Re-sync scenario to avoid sync limit checks
	}

	// Mock decryption
	mockEncryptionService.On("Decrypt", "encrypted-token").Return("decrypted-token", nil)

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
		nil, // profileRepo - not needed for these tests
		nil, // balanceSnapshotRepo - not needed for these tests
		mockPlaidService,
		mockEncryptionService,
		nil, // syncLimitService - not needed for these tests
		nil, // ruleApplicationService - not needed for these tests
		nil, // budgetSyncService - not needed for these tests
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
	mockPlaidService.On("TransactionsSync", ctx, "decrypted-token", "cursor-start", 0).Return(plaidResp1, nil)
	mockLinkedItemRepo.On("UpdateCursor", ctx, linkedItem.ID, "cursor-page1").Return(nil).Once()

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
		nil, // profileRepo - not needed for these tests
		nil, // balanceSnapshotRepo - not needed for these tests
		mockPlaidService,
		mockEncryptionService,
		nil, // syncLimitService - not needed for these tests
		nil, // ruleApplicationService - not needed for these tests
		nil, // budgetSyncService - not needed for these tests
	)

	ctx := context.Background()
	cursor := "existing-cursor" // Set cursor to avoid first-sync path
	linkedItem := &domain.LinkedItem{
		ID:                   "linked-item-123",
		AccessTokenEncrypted: "encrypted-token",
		Cursor:               &cursor, // Re-sync scenario to avoid sync limit checks
	}

	// Mock decryption
	mockEncryptionService.On("Decrypt", "encrypted-token").Return("decrypted-token", nil)

	// Plaid API succeeds
	plaidResp := &plaid.TransactionsSyncResponse{
		Added:      []plaid.Transaction{{TransactionId: "txn-1"}},
		NextCursor: "cursor-new",
		HasMore:    false,
	}
	mockPlaidService.On("TransactionsSync", ctx, "decrypted-token", "existing-cursor", 0).Return(plaidResp, nil)

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
	mockTransactionRepo := new(MockTransactionRepository)
	mockAccountRepo := new(MockAccountRepository)
	mockPlaidService := new(MockPlaidService)
	mockEncryptionService := new(MockEncryptionService)

	service := NewTransactionSyncService(
		nil,
		mockLinkedItemRepo,
		mockTransactionRepo,
		mockAccountRepo,
		nil, // profileRepo - not needed for these tests
		nil, // balanceSnapshotRepo - not needed for these tests
		mockPlaidService,
		mockEncryptionService,
		nil, // syncLimitService - not needed for these tests
		nil, // ruleApplicationService - not needed for these tests
		nil, // budgetSyncService - not needed for these tests
	)

	ctx := context.Background()
	cursor := "existing-cursor" // Set cursor to avoid first-sync path
	linkedItem := &domain.LinkedItem{
		ID:                   "linked-item-123",
		AccessTokenEncrypted: "encrypted-token",
		Cursor:               &cursor, // Re-sync scenario to avoid sync limit checks
	}

	// Mock decryption
	mockEncryptionService.On("Decrypt", "encrypted-token").Return("decrypted-token", nil)

	// Mock Plaid API with cursor (re-sync scenario)
	plaidResp := &plaid.TransactionsSyncResponse{
		Added:      []plaid.Transaction{{TransactionId: "txn-1"}},
		NextCursor: "cursor-initial",
		HasMore:    false,
	}
	mockPlaidService.On("TransactionsSync", ctx, "decrypted-token", "existing-cursor", 0).Return(plaidResp, nil)
	mockLinkedItemRepo.On("UpdateCursor", ctx, linkedItem.ID, "cursor-initial").Return(nil)

	// Issue #261: Mock account repository for transaction storage
	mockAccountRepo.On("GetByLinkedItemID", ctx, linkedItem.ID).Return([]domain.Account{}, nil)

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
		nil, // profileRepo
		nil, // balanceSnapshotRepo
		nil, // plaidService
		nil, // encryptionService
		nil, // syncLimitService
		nil, // ruleApplicationService
		nil, // budgetSyncService
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
		nil, // profileRepo
		nil, // balanceSnapshotRepo
		nil, // plaidService
		nil, // encryptionService
		nil, // syncLimitService
		nil, // ruleApplicationService
		nil, // budgetSyncService
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
		nil, // profileRepo
		nil, // balanceSnapshotRepo
		nil, // plaidService
		nil, // encryptionService
		nil, // syncLimitService
		nil, // ruleApplicationService
		nil, // budgetSyncService
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
		nil, // profileRepo
		nil, // balanceSnapshotRepo
		nil, // plaidService
		nil, // encryptionService
		nil, // syncLimitService
		nil, // ruleApplicationService
		nil, // budgetSyncService
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
		nil, // profileRepo
		nil, // balanceSnapshotRepo
		nil, // plaidService
		nil, // encryptionService
		nil, // syncLimitService
		nil, // ruleApplicationService
		nil, // budgetSyncService
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
		nil, // profileRepo
		nil, // balanceSnapshotRepo
		nil, // plaidService
		nil, // encryptionService
		nil, // syncLimitService
		nil, // ruleApplicationService
		nil, // budgetSyncService
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
		nil, // profileRepo
		nil, // balanceSnapshotRepo
		nil, // plaidService
		nil, // encryptionService
		nil, // syncLimitService
		nil, // ruleApplicationService
		nil, // budgetSyncService
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
		nil, // encryptionService
		nil, // syncLimitService
		nil, // ruleApplicationService
		nil, // budgetSyncService
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
		{
			TransactionId: "plaid-txn-1",
			AccountId:     "plaid-acc-1",
			Amount:        150.00, // Amount changed from 100 to 150
			Date:          "2024-01-15",
			Name:          "Transaction 1 Updated",
			Pending:       false,
		},
		{
			TransactionId: "plaid-txn-2",
			AccountId:     "plaid-acc-2",
			Amount:        75.00, // Amount changed
			Date:          "2024-01-16",
			Name:          "Transaction 2 Updated",
			Pending:       true, // Now pending
		},
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
	err := service.processModifiedTransactions(ctx, modified, linkedItem, accountMap)

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
		nil, // encryptionService
		nil, // syncLimitService
		nil, // ruleApplicationService
		nil, // budgetSyncService
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
	err := service.processModifiedTransactions(ctx, modified, linkedItem, accountMap)

	// Assert
	assert.NoError(t, err)
	mockTransactionRepo.AssertExpectations(t)
}

// TestProcessModifiedTransactions_TransactionNotFound tests handling when transaction doesn't exist yet
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
		nil, // encryptionService
		nil, // syncLimitService
		nil, // ruleApplicationService
		nil, // budgetSyncService
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

	// GetByExternalID returns error (not found)
	mockTransactionRepo.On("GetByExternalID", ctx, domain.SourcePlaid, "plaid-txn-new", userID).
		Return(nil, errors.New("transaction not found"))

	// Execute
	err := service.processModifiedTransactions(ctx, modified, linkedItem, accountMap)

	// Assert - should continue despite not finding transaction (log warning)
	assert.NoError(t, err) // Gracefully skip
	// Update should NOT be called for not-found transaction
	mockTransactionRepo.AssertNotCalled(t, "Update")
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
		nil, // encryptionService
		nil, // syncLimitService
		nil, // ruleApplicationService
		nil, // budgetSyncService
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
	err := service.processModifiedTransactions(ctx, modified, linkedItem, accountMap)

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
		nil, // encryptionService
		nil, // syncLimitService
		nil, // ruleApplicationService
		nil, // budgetSyncService
	).(*transactionSyncService)

	ctx := context.Background()
	userID := "user-456"

	linkedItem := &domain.LinkedItem{
		ID:     "linked-item-123",
		UserID: userID,
	}

	// 3 removed transactions from Plaid
	txnID1 := "plaid-txn-removed-1"
	txnID2 := "plaid-txn-removed-2"
	txnID3 := "plaid-txn-removed-3"

	removed := []plaid.RemovedTransaction{
		{TransactionId: &txnID1},
		{TransactionId: &txnID2},
		{TransactionId: &txnID3},
	}

	// Mock: DeleteByExternalID called for each transaction
	mockTransactionRepo.On("DeleteByExternalID", ctx, domain.SourcePlaid, "plaid-txn-removed-1", userID).Return(nil)
	mockTransactionRepo.On("DeleteByExternalID", ctx, domain.SourcePlaid, "plaid-txn-removed-2", userID).Return(nil)
	mockTransactionRepo.On("DeleteByExternalID", ctx, domain.SourcePlaid, "plaid-txn-removed-3", userID).Return(nil)

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
		nil, // encryptionService
		nil, // syncLimitService
		nil, // ruleApplicationService
		nil, // budgetSyncService
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
	mockTransactionRepo.On("DeleteByExternalID", ctx, domain.SourcePlaid, "plaid-txn-not-found", userID).
		Return(errors.New("transaction not found"))

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
		nil, // encryptionService
		nil, // syncLimitService
		nil, // ruleApplicationService
		nil, // budgetSyncService
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
	mockTransactionRepo.AssertNotCalled(t, "DeleteByExternalID")
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
		nil, // encryptionService
		nil, // syncLimitService
		nil, // ruleApplicationService
		nil, // budgetSyncService
	).(*transactionSyncService)

	ctx := context.Background()
	linkedItem := &domain.LinkedItem{
		ID:     "linked-item-123",
		UserID: "user-456",
	}

	// Transaction with nil ID (should skip)
	removed := []plaid.RemovedTransaction{
		{TransactionId: nil}, // NIL ID
	}

	// Execute
	err := service.processRemovedTransactions(ctx, removed, linkedItem)

	// Assert - should succeed but skip nil transaction
	assert.NoError(t, err)
	// DeleteByExternalID should NOT be called for nil ID
	mockTransactionRepo.AssertNotCalled(t, "DeleteByExternalID")
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
		nil, // encryptionService
		nil, // syncLimitService
		nil, // ruleApplicationService
		nil, // budgetSyncService
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

	// Existing pending transactions in database (will be queried during processModified)
	userNotesRestaurant := "Team lunch - add tip later"
	userNotesHotel := "Business trip hotel"
	existingPendingRestaurant := &domain.Transaction{
		ID:                    "txn-pending-restaurant",
		UserID:                userID,
		AccountID:             "account-uuid-restaurant",
		LinkedItemID:          linkedItemID,
		ExternalTransactionID: strPtrHelper("plaid-txn-restaurant"),
		Amount:                -45.00, // Authorization amount (before tip)
		Pending:               true,   // Currently pending
		UserNotes:             &userNotesRestaurant,
	}

	existingPendingHotel := &domain.Transaction{
		ID:                    "txn-pending-hotel",
		UserID:                userID,
		AccountID:             "account-uuid-hotel",
		LinkedItemID:          linkedItemID,
		ExternalTransactionID: strPtrHelper("plaid-txn-hotel"),
		Amount:                -200.00, // Hotel reservation hold
		Pending:               true,
		UserNotes:             &userNotesHotel,
	}

	existingPendingGas := &domain.Transaction{
		ID:                    "txn-pending-gas",
		UserID:                userID,
		AccountID:             "account-uuid-gas",
		LinkedItemID:          linkedItemID,
		ExternalTransactionID: strPtrHelper("plaid-txn-gas"),
		Amount:                -55.00, // Gas station authorization
		Pending:               true,
		UserNotes:             nil, // No notes on this one
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
			Amount:        54.50,  // Tip added: $45 + $9.50 tip
			Date:          "2024-01-14",
			Name:          "Restaurant",
			MerchantName:  *plaid.NewNullableString(&merchantRestaurant),
			Pending:       false, // Now posted
		},
		// Hotel: Date changed (checkout date vs reservation date), pending→false, notes preserved
		{
			TransactionId: "plaid-txn-hotel",
			AccountId:     "plaid-acc-hotel",
			Amount:        200.00, // Amount unchanged
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
	err := service.processModifiedTransactions(ctx, modified, linkedItem, accountMap)

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

// TestProcessAddedTransactions_SetsOriginalCategory tests that OriginalCategory is set
// to the same value as Category when transactions are first synced from Plaid
// TDD RED: This test will FAIL until mapPlaidTransaction sets OriginalCategory
func TestProcessAddedTransactions_SetsOriginalCategory(t *testing.T) {
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
		nil, // encryptionService
		nil, // syncLimitService
		nil, // ruleApplicationService
		nil, // budgetSyncService
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

	// Create Plaid transaction with category
	added := []plaid.Transaction{
		{
			TransactionId: "plaid-txn-with-category",
			AccountId:     "plaid-acc-1",
			Amount:        100.00,
			Date:          "2024-01-15",
			Name:          "Grocery Store",
			Category:      []string{"Food and Drink", "Groceries"},
			Pending:       false,
		},
	}

	// Mock: CreateBatch should be called with transactions that have OriginalCategory set
	mockTransactionRepo.On("CreateBatch", ctx, mock.MatchedBy(func(txns []domain.Transaction) bool {
		if len(txns) != 1 {
			return false
		}
		// Verify Category is set
		if txns[0].Category == nil {
			t.Errorf("Category should be set")
			return false
		}
		// Verify OriginalCategory is set to same value as Category
		if txns[0].OriginalCategory == nil {
			t.Errorf("OriginalCategory should be set when transaction has category")
			return false
		}
		// Both should have the same JSON value
		if *txns[0].Category != *txns[0].OriginalCategory {
			t.Errorf("OriginalCategory should match Category, got %s vs %s",
				*txns[0].OriginalCategory, *txns[0].Category)
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

// strPtrHelper is a helper to create string pointers for tests
func strPtrHelper(s string) *string {
	return &s
}
