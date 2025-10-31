package workers

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/events"
	"github.com/plaid/plaid-go/v23/plaid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

// TestPlaidSyncProvider_SyncForLinkedItem_WithAddedTransactions tests processAdded path
func TestPlaidSyncProvider_SyncForLinkedItem_WithAddedTransactions(t *testing.T) {
	linkedItemRepo := new(MockLinkedItemRepository)
	txnRepo := new(MockTransactionRepository)
	plaidClient := new(MockPlaidClient)
	emitter := new(MockEventEmitter)

	provider := NewPlaidSyncProvider(linkedItemRepo, txnRepo, plaidClient, emitter)
	ctx := context.Background()

	linkedItemID := "test-linked-item"
	cursor := "test-cursor"
	linkedItem := &domain.LinkedItem{
		ID:                   linkedItemID,
		UserID:               "user-123",
		AccessTokenEncrypted: "encrypted-token",
		Cursor:               &cursor,
	}

	// Mock Plaid API response with added transactions
	amount := 25.50
	txnID := "plaid-txn-1"
	accountID := "acc-123"
	date := "2024-01-15"
	nextCursor := "new-cursor"

	plaidTxn := plaid.Transaction{
		Amount:        amount,
		TransactionId: txnID,
		AccountId:     accountID,
		Date:          date,
		Name:          "Test Store",
		Pending:       false,
	}

	syncResp := &plaid.TransactionsSyncResponse{
		Added:      []plaid.Transaction{plaidTxn},
		Modified:   []plaid.Transaction{},
		Removed:    []plaid.RemovedTransaction{},
		NextCursor: nextCursor,
		HasMore:    false,
	}

	// Setup expectations
	linkedItemRepo.On("GetByID", ctx, linkedItemID).Return(linkedItem, nil)
	plaidClient.On("TransactionsSync", ctx, "encrypted-token", &cursor).Return(syncResp, nil)
	txnRepo.On("CreateBatch", ctx, mock.MatchedBy(func(txns []domain.Transaction) bool {
		return len(txns) == 1 &&
			txns[0].UserID == "user-123" &&
			txns[0].Source == domain.SourcePlaid &&
			*txns[0].ExternalTransactionID == txnID
	})).Return(nil)
	emitter.On("Emit", ctx, mock.MatchedBy(func(e events.TransactionEvent) bool {
		return e.Type == events.EventTypeAdded && e.UserID == "user-123"
	})).Return(nil)
	linkedItemRepo.On("UpdateCursor", ctx, linkedItemID, nextCursor).Return(nil)

	// Execute
	count, err := provider.SyncForLinkedItem(ctx, linkedItemID)

	// Assert
	assert.NoError(t, err)
	assert.Equal(t, 1, count)

	linkedItemRepo.AssertExpectations(t)
	txnRepo.AssertExpectations(t)
	plaidClient.AssertExpectations(t)
	emitter.AssertExpectations(t)
}

// TestPlaidSyncProvider_SyncForLinkedItem_WithRemovedTransactions tests processRemoved path
func TestPlaidSyncProvider_SyncForLinkedItem_WithRemovedTransactions(t *testing.T) {
	linkedItemRepo := new(MockLinkedItemRepository)
	txnRepo := new(MockTransactionRepository)
	plaidClient := new(MockPlaidClient)
	emitter := new(MockEventEmitter)

	provider := NewPlaidSyncProvider(linkedItemRepo, txnRepo, plaidClient, emitter)
	ctx := context.Background()

	linkedItemID := "test-linked-item"
	linkedItem := &domain.LinkedItem{
		ID:                   linkedItemID,
		UserID:               "user-123",
		AccessTokenEncrypted: "encrypted-token",
		Cursor:               nil, // Initial sync
	}

	// Mock Plaid API response with removed transactions
	txnID := "plaid-txn-removed"
	nextCursor := "new-cursor"

	removedTxn := plaid.RemovedTransaction{
		TransactionId: &txnID,
	}

	syncResp := &plaid.TransactionsSyncResponse{
		Added:      []plaid.Transaction{},
		Modified:   []plaid.Transaction{},
		Removed:    []plaid.RemovedTransaction{removedTxn},
		NextCursor: nextCursor,
		HasMore:    false,
	}

	// Setup expectations
	linkedItemRepo.On("GetByID", ctx, linkedItemID).Return(linkedItem, nil)
	plaidClient.On("TransactionsSync", ctx, "encrypted-token", (*string)(nil)).Return(syncResp, nil)
	txnRepo.On("DeleteByExternalID", ctx, domain.SourcePlaid, txnID, "user-123").Return(nil)
	emitter.On("Emit", ctx, mock.MatchedBy(func(e events.TransactionEvent) bool {
		return e.Type == events.EventTypeRemoved &&
			e.UserID == "user-123" &&
			e.Transaction.Source == domain.SourcePlaid
	})).Return(nil)
	linkedItemRepo.On("UpdateCursor", ctx, linkedItemID, nextCursor).Return(nil)

	// Execute
	count, err := provider.SyncForLinkedItem(ctx, linkedItemID)

	// Assert
	assert.NoError(t, err)
	assert.Equal(t, 1, count)

	linkedItemRepo.AssertExpectations(t)
	txnRepo.AssertExpectations(t)
	plaidClient.AssertExpectations(t)
	emitter.AssertExpectations(t)
}

// TestPlaidSyncProvider_SyncForLinkedItem_WithPagination tests multi-page sync
func TestPlaidSyncProvider_SyncForLinkedItem_WithPagination(t *testing.T) {
	linkedItemRepo := new(MockLinkedItemRepository)
	txnRepo := new(MockTransactionRepository)
	plaidClient := new(MockPlaidClient)
	emitter := new(MockEventEmitter)

	provider := NewPlaidSyncProvider(linkedItemRepo, txnRepo, plaidClient, emitter)
	ctx := context.Background()

	linkedItemID := "test-linked-item"
	linkedItem := &domain.LinkedItem{
		ID:                   linkedItemID,
		UserID:               "user-123",
		AccessTokenEncrypted: "encrypted-token",
		Cursor:               nil,
	}

	// First page response
	amount1 := 10.0
	txnID1 := "txn-1"
	accountID := "acc-123"
	date := "2024-01-15"
	cursor1 := "cursor-page-1"

	plaidTxn1 := plaid.Transaction{
		Amount:        amount1,
		TransactionId: txnID1,
		AccountId:     accountID,
		Date:          date,
		Name:          "Store 1",
		Pending:       false,
	}

	syncResp1 := &plaid.TransactionsSyncResponse{
		Added:      []plaid.Transaction{plaidTxn1},
		Modified:   []plaid.Transaction{},
		Removed:    []plaid.RemovedTransaction{},
		NextCursor: cursor1,
		HasMore:    true, // Has more pages
	}

	// Second page response
	amount2 := 20.0
	txnID2 := "txn-2"
	finalCursor := "cursor-final"

	plaidTxn2 := plaid.Transaction{
		Amount:        amount2,
		TransactionId: txnID2,
		AccountId:     accountID,
		Date:          date,
		Name:          "Store 2",
		Pending:       false,
	}

	syncResp2 := &plaid.TransactionsSyncResponse{
		Added:      []plaid.Transaction{plaidTxn2},
		Modified:   []plaid.Transaction{},
		Removed:    []plaid.RemovedTransaction{},
		NextCursor: finalCursor,
		HasMore:    false, // Last page
	}

	// Setup expectations
	linkedItemRepo.On("GetByID", ctx, linkedItemID).Return(linkedItem, nil)

	// First page call
	plaidClient.On("TransactionsSync", ctx, "encrypted-token", (*string)(nil)).Return(syncResp1, nil).Once()
	txnRepo.On("CreateBatch", ctx, mock.Anything).Return(nil).Once()
	emitter.On("Emit", ctx, mock.Anything).Return(nil).Once()

	// Second page call
	plaidClient.On("TransactionsSync", ctx, "encrypted-token", &cursor1).Return(syncResp2, nil).Once()
	txnRepo.On("CreateBatch", ctx, mock.Anything).Return(nil).Once()
	emitter.On("Emit", ctx, mock.Anything).Return(nil).Once()

	linkedItemRepo.On("UpdateCursor", ctx, linkedItemID, finalCursor).Return(nil)

	// Execute
	count, err := provider.SyncForLinkedItem(ctx, linkedItemID)

	// Assert
	assert.NoError(t, err)
	assert.Equal(t, 2, count) // 1 from each page

	linkedItemRepo.AssertExpectations(t)
	txnRepo.AssertExpectations(t)
	plaidClient.AssertExpectations(t)
}

// TestPlaidSyncProvider_SyncForLinkedItem_PlaidAPIError tests Plaid API failure
func TestPlaidSyncProvider_SyncForLinkedItem_PlaidAPIError(t *testing.T) {
	linkedItemRepo := new(MockLinkedItemRepository)
	txnRepo := new(MockTransactionRepository)
	plaidClient := new(MockPlaidClient)
	emitter := new(MockEventEmitter)

	provider := NewPlaidSyncProvider(linkedItemRepo, txnRepo, plaidClient, emitter)
	ctx := context.Background()

	linkedItemID := "test-linked-item"
	linkedItem := &domain.LinkedItem{
		ID:                   linkedItemID,
		UserID:               "user-123",
		AccessTokenEncrypted: "encrypted-token",
		Cursor:               nil,
	}

	// Setup expectations
	linkedItemRepo.On("GetByID", ctx, linkedItemID).Return(linkedItem, nil)
	plaidClient.On("TransactionsSync", ctx, "encrypted-token", (*string)(nil)).
		Return(nil, errors.New("plaid API error"))

	// Execute
	count, err := provider.SyncForLinkedItem(ctx, linkedItemID)

	// Assert
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "plaid transactions sync failed")
	assert.Equal(t, 0, count)

	linkedItemRepo.AssertExpectations(t)
	plaidClient.AssertExpectations(t)
}

// TestPlaidSyncProvider_SyncForLinkedItem_CreateBatchError tests batch insert failure
func TestPlaidSyncProvider_SyncForLinkedItem_CreateBatchError(t *testing.T) {
	linkedItemRepo := new(MockLinkedItemRepository)
	txnRepo := new(MockTransactionRepository)
	plaidClient := new(MockPlaidClient)
	emitter := new(MockEventEmitter)

	provider := NewPlaidSyncProvider(linkedItemRepo, txnRepo, plaidClient, emitter)
	ctx := context.Background()

	linkedItemID := "test-linked-item"
	linkedItem := &domain.LinkedItem{
		ID:                   linkedItemID,
		UserID:               "user-123",
		AccessTokenEncrypted: "encrypted-token",
		Cursor:               nil,
	}

	// Mock Plaid API response
	amount := 25.50
	txnID := "plaid-txn-1"
	accountID := "acc-123"
	date := "2024-01-15"
	nextCursor := "new-cursor"

	plaidTxn := plaid.Transaction{
		Amount:        amount,
		TransactionId: txnID,
		AccountId:     accountID,
		Date:          date,
		Name:          "Test Store",
		Pending:       false,
	}

	syncResp := &plaid.TransactionsSyncResponse{
		Added:      []plaid.Transaction{plaidTxn},
		Modified:   []plaid.Transaction{},
		Removed:    []plaid.RemovedTransaction{},
		NextCursor: nextCursor,
		HasMore:    false,
	}

	// Setup expectations
	linkedItemRepo.On("GetByID", ctx, linkedItemID).Return(linkedItem, nil)
	plaidClient.On("TransactionsSync", ctx, "encrypted-token", (*string)(nil)).Return(syncResp, nil)
	txnRepo.On("CreateBatch", ctx, mock.Anything).Return(errors.New("database error"))

	// Execute
	count, err := provider.SyncForLinkedItem(ctx, linkedItemID)

	// Assert
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to process added transactions")
	assert.Equal(t, 0, count)

	linkedItemRepo.AssertExpectations(t)
	txnRepo.AssertExpectations(t)
	plaidClient.AssertExpectations(t)
}

// TestPlaidSyncProvider_SyncForLinkedItem_DeleteError tests deletion failure
func TestPlaidSyncProvider_SyncForLinkedItem_DeleteError(t *testing.T) {
	linkedItemRepo := new(MockLinkedItemRepository)
	txnRepo := new(MockTransactionRepository)
	plaidClient := new(MockPlaidClient)
	emitter := new(MockEventEmitter)

	provider := NewPlaidSyncProvider(linkedItemRepo, txnRepo, plaidClient, emitter)
	ctx := context.Background()

	linkedItemID := "test-linked-item"
	linkedItem := &domain.LinkedItem{
		ID:                   linkedItemID,
		UserID:               "user-123",
		AccessTokenEncrypted: "encrypted-token",
		Cursor:               nil,
	}

	// Mock Plaid API response with removed transaction
	txnID := "plaid-txn-removed"
	nextCursor := "new-cursor"

	removedTxn := plaid.RemovedTransaction{
		TransactionId: &txnID,
	}

	syncResp := &plaid.TransactionsSyncResponse{
		Added:      []plaid.Transaction{},
		Modified:   []plaid.Transaction{},
		Removed:    []plaid.RemovedTransaction{removedTxn},
		NextCursor: nextCursor,
		HasMore:    false,
	}

	// Setup expectations
	linkedItemRepo.On("GetByID", ctx, linkedItemID).Return(linkedItem, nil)
	plaidClient.On("TransactionsSync", ctx, "encrypted-token", (*string)(nil)).Return(syncResp, nil)
	txnRepo.On("DeleteByExternalID", ctx, domain.SourcePlaid, txnID, "user-123").
		Return(errors.New("database error"))

	// Execute
	count, err := provider.SyncForLinkedItem(ctx, linkedItemID)

	// Assert
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to process removed transactions")
	assert.Equal(t, 0, count)

	linkedItemRepo.AssertExpectations(t)
	txnRepo.AssertExpectations(t)
	plaidClient.AssertExpectations(t)
}

// TestPlaidSyncProvider_SyncForLinkedItem_UpdateCursorError tests cursor update failure
func TestPlaidSyncProvider_SyncForLinkedItem_UpdateCursorError(t *testing.T) {
	linkedItemRepo := new(MockLinkedItemRepository)
	txnRepo := new(MockTransactionRepository)
	plaidClient := new(MockPlaidClient)
	emitter := new(MockEventEmitter)

	provider := NewPlaidSyncProvider(linkedItemRepo, txnRepo, plaidClient, emitter)
	ctx := context.Background()

	linkedItemID := "test-linked-item"
	linkedItem := &domain.LinkedItem{
		ID:                   linkedItemID,
		UserID:               "user-123",
		AccessTokenEncrypted: "encrypted-token",
		Cursor:               nil,
	}

	// Mock Plaid API response with no transactions
	nextCursor := "new-cursor"

	syncResp := &plaid.TransactionsSyncResponse{
		Added:      []plaid.Transaction{},
		Modified:   []plaid.Transaction{},
		Removed:    []plaid.RemovedTransaction{},
		NextCursor: nextCursor,
		HasMore:    false,
	}

	// Setup expectations
	linkedItemRepo.On("GetByID", ctx, linkedItemID).Return(linkedItem, nil)
	plaidClient.On("TransactionsSync", ctx, "encrypted-token", (*string)(nil)).Return(syncResp, nil)
	linkedItemRepo.On("UpdateCursor", ctx, linkedItemID, nextCursor).Return(errors.New("database error"))

	// Execute
	count, err := provider.SyncForLinkedItem(ctx, linkedItemID)

	// Assert
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to update cursor")
	assert.Equal(t, 0, count)

	linkedItemRepo.AssertExpectations(t)
	plaidClient.AssertExpectations(t)
}

// TestPlaidSyncProvider_ConvertPlaidTransaction_WithAuthorizedDate tests date parsing
func TestPlaidSyncProvider_ConvertPlaidTransaction_WithAuthorizedDate(t *testing.T) {
	linkedItemRepo := new(MockLinkedItemRepository)
	txnRepo := new(MockTransactionRepository)
	plaidClient := new(MockPlaidClient)
	emitter := new(MockEventEmitter)

	provider := NewPlaidSyncProvider(linkedItemRepo, txnRepo, plaidClient, emitter).(*plaidSyncProvider)

	linkedItem := &domain.LinkedItem{
		ID:                   "test-item",
		UserID:               "user-123",
		AccessTokenEncrypted: "token",
	}

	// Create Plaid transaction with authorized date
	authorizedDate := "2024-01-14"
	authDateNullable := plaid.NewNullableString(&authorizedDate)

	plaidTxn := plaid.Transaction{
		Amount:         100.50,
		TransactionId:  "txn-123",
		AccountId:      "acc-123",
		Date:           "2024-01-15",
		Name:           "Test Merchant",
		Pending:        true,
		AuthorizedDate: *authDateNullable,
	}

	// Convert
	domainTxn, err := provider.convertPlaidTransaction(linkedItem, plaidTxn)

	// Assert
	assert.NoError(t, err)
	assert.NotNil(t, domainTxn)
	assert.Equal(t, "user-123", domainTxn.UserID)
	assert.Equal(t, domain.SourcePlaid, domainTxn.Source)
	assert.Equal(t, "txn-123", *domainTxn.ExternalTransactionID)
	assert.True(t, domainTxn.Pending)
	assert.NotNil(t, domainTxn.AuthorizedDate)

	// Check authorized date was parsed correctly
	expectedAuthDate, _ := time.Parse("2006-01-02", "2024-01-14")
	assert.Equal(t, expectedAuthDate.Year(), domainTxn.AuthorizedDate.Year())
	assert.Equal(t, expectedAuthDate.Month(), domainTxn.AuthorizedDate.Month())
	assert.Equal(t, expectedAuthDate.Day(), domainTxn.AuthorizedDate.Day())
}

// TestPlaidSyncProvider_ConvertPlaidTransaction_InvalidDate tests error handling
func TestPlaidSyncProvider_ConvertPlaidTransaction_InvalidDate(t *testing.T) {
	linkedItemRepo := new(MockLinkedItemRepository)
	txnRepo := new(MockTransactionRepository)
	plaidClient := new(MockPlaidClient)
	emitter := new(MockEventEmitter)

	provider := NewPlaidSyncProvider(linkedItemRepo, txnRepo, plaidClient, emitter).(*plaidSyncProvider)

	linkedItem := &domain.LinkedItem{
		ID:                   "test-item",
		UserID:               "user-123",
		AccessTokenEncrypted: "token",
	}

	// Create Plaid transaction with invalid date
	plaidTxn := plaid.Transaction{
		Amount:        100.50,
		TransactionId: "txn-123",
		AccountId:     "acc-123",
		Date:          "invalid-date",
		Name:          "Test Merchant",
		Pending:       false,
	}

	// Convert
	domainTxn, err := provider.convertPlaidTransaction(linkedItem, plaidTxn)

	// Assert
	assert.Error(t, err)
	assert.Nil(t, domainTxn)
	assert.Contains(t, err.Error(), "failed to parse transaction date")
}
