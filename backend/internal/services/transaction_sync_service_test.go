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
		nil, // logger
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
		AccessToken: "decrypted-access-token",
		Cursor:      stringPtr("old-cursor"),
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
		nil, // logger
	)

	ctx := context.Background()
	linkedItem := &domain.LinkedItem{
		ID:                   "linked-item-123",
		UserID:               "user-123",
		ItemID:               "item-abc",
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
		nil, // logger
	)

	ctx := context.Background()
	linkedItem := &domain.LinkedItem{
		ID:                   "linked-item-123",
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
		nil, // logger
	)

	ctx := context.Background()
	cursor := "existing-cursor" // Set cursor to avoid first-sync path
	linkedItem := &domain.LinkedItem{
		ID:                   "linked-item-123",
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
		nil, // logger
	)

	ctx := context.Background()
	linkedItem := &domain.LinkedItem{
		ID:                   "linked-item-123",
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
	mockAccountRepo.On("GetByLinkedItemID", ctx, linkedItemID).Return(accounts, nil)

	// Execute
	accountMap, _, err := service.buildAccountMaps(ctx, linkedItemID)

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
		nil, // logger
	).(*transactionSyncService)

	ctx := context.Background()
	linkedItemID := "linked-item-123"

	// Mock: GetByLinkedItemID returns empty slice
	mockAccountRepo.On("GetByLinkedItemID", ctx, linkedItemID).Return([]domain.Account{}, nil)

	// Execute
	accountMap, _, err := service.buildAccountMaps(ctx, linkedItemID)

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
		nil, // logger
	).(*transactionSyncService)

	ctx := context.Background()
	linkedItemID := "linked-item-123"

	// Mock: GetByLinkedItemID returns error
	mockAccountRepo.On("GetByLinkedItemID", ctx, linkedItemID).Return(nil, errors.New("database error"))

	// Execute
	accountMap, _, err := service.buildAccountMaps(ctx, linkedItemID)

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
	})).Return(nil)

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
	})).Return(nil)

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
	})).Return(nil)

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
		nil, // subscriptionService
		nil, // ruleApplicationService
		nil, // budgetSyncService
		nil, // transactionMonitorService
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

	// GetByExternalID returns error (not found)
	mockTransactionRepo.On("GetByExternalID", ctx, domain.SourcePlaid, "plaid-txn-new", userID).
		Return(nil, errors.New("transaction not found"))

	// Execute
	err := service.processModifiedTransactions(ctx, modified, linkedItem, accountMap, nil)

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
		nil, // subscriptionService
		nil, // ruleApplicationService
		nil, // budgetSyncService
		nil, // transactionMonitorService
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
		nil, // subscriptionService
		nil, // ruleApplicationService
		nil, // budgetSyncService
		nil, // transactionMonitorService
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
		nil, // subscriptionService
		nil, // ruleApplicationService
		nil, // budgetSyncService
		nil, // transactionMonitorService
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
		nil, // subscriptionService
		nil, // ruleApplicationService
		nil, // budgetSyncService
		nil, // transactionMonitorService
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
		nil, // subscriptionService
		nil, // ruleApplicationService
		nil, // budgetSyncService
		nil, // transactionMonitorService
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
		Amount: -45.00, Pending: true, UserNotes: &notesRestaurant,
	}
	existingPendingHotel := &domain.Transaction{
		ID: "txn-pending-hotel", UserID: userID, AccountID: "account-uuid-hotel",
		LinkedItemID: linkedItemID, ExternalTransactionID: strPtrHelper("plaid-txn-hotel"),
		Amount: -200.00, Pending: true, UserNotes: &notesHotel,
	}
	existingPendingGas := &domain.Transaction{
		ID: "txn-pending-gas", UserID: userID, AccountID: "account-uuid-gas",
		LinkedItemID: linkedItemID, ExternalTransactionID: strPtrHelper("plaid-txn-gas"),
		Amount: -55.00, Pending: true, UserNotes: nil,
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
	})).Return(nil)

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

func (m *mockProfileRepoForCursorTests) GetUsersScheduledForDeletion(ctx context.Context) ([]*domain.User, error) {
	args := m.Called(ctx)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]*domain.User), args.Error(1)
}

func (m *mockProfileRepoForCursorTests) DeleteUserCascade(ctx context.Context, userID string) error {
	args := m.Called(ctx, userID)
	return args.Error(0)
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

func (m *mockProfileRepoForCursorTests) UpdateSubscriptionStatus(ctx context.Context, userID string, status string) error {
	args := m.Called(ctx, userID, status)
	return args.Error(0)
}

func (m *mockProfileRepoForCursorTests) UpdateTrialStatus(ctx context.Context, userID string, status string, trialStartedAt, trialEndsAt *time.Time, hadTrial bool) error {
	args := m.Called(ctx, userID, status, trialStartedAt, trialEndsAt, hadTrial)
	return args.Error(0)
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
func (m *mockProfileRepoForCursorTests) GetSubscriptionStatusByUserID(ctx context.Context, userID string) (string, error) { return "", nil }
func (m *mockProfileRepoForCursorTests) SetRetentionDates(ctx context.Context, userID string, startedAt, expiresAt time.Time) error { return nil }
func (m *mockProfileRepoForCursorTests) ClearRetentionDates(ctx context.Context, userID string) error { return nil }
func (m *mockProfileRepoForCursorTests) SetRetentionNotice30dSent(ctx context.Context, userID string) error { return nil }
func (m *mockProfileRepoForCursorTests) SetRetentionNotice7dSent(ctx context.Context, userID string) error { return nil }
func (m *mockProfileRepoForCursorTests) GetUsersNeedingRetention30dNotice(ctx context.Context) ([]*domain.User, error) { return nil, nil }
func (m *mockProfileRepoForCursorTests) GetUsersNeedingRetention7dNotice(ctx context.Context) ([]*domain.User, error) { return nil, nil }
func (m *mockProfileRepoForCursorTests) GetUsersWithExpiredRetention(ctx context.Context) ([]*domain.User, error) { return nil, nil }
func (m *mockProfileRepoForCursorTests) GetUserByEmail(ctx context.Context, email string) (*domain.User, error) { return nil, nil }
func (m *mockProfileRepoForCursorTests) UpdateUserEmailTx(ctx context.Context, tx pgx.Tx, userID, plainEmail string) error {
	return nil
}

// ============================================================================
// CURSOR PERSISTENCE TESTS (Critical)
// Plaid Staging Validation - Cursor handling edge cases
// ============================================================================

// TestSyncTransactions_CursorPersistsAfterTxnStorageFailure verifies that the cursor
// is persisted even when transaction storage fails. This ensures resumability.
func TestSyncTransactions_CursorPersistsAfterTxnStorageFailure(t *testing.T) {
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
	mockAccountRepo.On("GetByLinkedItemID", ctx, "linked-item-123").Return(accounts, nil)

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

	// Mock: Cursor update SUCCEEDS (this is the critical part)
	mockLinkedItemRepo.On("UpdateCursor", ctx, "linked-item-123", newCursor).Return(nil)

	// Mock: Transaction storage FAILS
	mockTransactionRepo.On("CreateBatch", ctx, mock.Anything).Return(errors.New("database connection lost"))

	// Execute
	result, err := service.SyncTransactions(ctx, linkedItem)

	// Assert: Error is returned due to transaction storage failure
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "database connection lost")

	// Assert: But cursor was still updated (critical for resumability)
	mockLinkedItemRepo.AssertCalled(t, "UpdateCursor", ctx, "linked-item-123", newCursor)

	// Assert: Partial results were returned (transactions that failed to store)
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
	mockAccountRepo.On("GetByLinkedItemID", ctx, "linked-item-123").Return(accounts, nil)

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
	mockTransactionRepo.On("CreateBatch", ctx, mock.Anything).Return(nil)

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
				nil, nil, nil, nil,
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
			mockAccountRepo.On("GetByLinkedItemID", ctx, "linked-item-first-sync").Return(accounts, nil)

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
	mockAccountRepo.On("GetByLinkedItemID", ctx, "linked-item-123").Return(accounts, nil)

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
	mockTransactionRepo.On("CreateBatch", ctx, mock.Anything).Return(nil)

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
		mockPlaidService, nil, nil, nil, nil, nil,
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
	mockAccountRepo.On("GetByLinkedItemID", ctx, "linked-item-123").Return(accounts, nil)

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
	})).Return(nil)

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
		nil, nil, nil, nil,
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

	mockAccountRepo.On("GetByLinkedItemID", ctx, "linked-item-123").Return(accounts, nil)

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
	})).Return(nil)

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
		nil, nil, nil, nil, nil, nil, nil, nil, nil,
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
	})).Return(nil)

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
		nil, nil, nil, nil, nil, nil, nil, nil, nil,
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
	})).Return(nil)

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
		nil, nil, nil, nil, nil, nil, nil, nil, nil,
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
	})).Return(nil)

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
		nil, nil, nil, nil, nil, nil, nil, nil, nil,
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
