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

// TestPlaidSyncProvider_SyncForLinkedItem_TenPagePagination tests 10+ page sync
// This is a stress test to verify cursor handling and pagination over many pages.
func TestPlaidSyncProvider_SyncForLinkedItem_TenPagePagination(t *testing.T) {
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

	// Generate 10 pages of transactions
	const numPages = 10
	const transactionsPerPage = 50 // Simulate realistic page sizes
	accountID := "acc-123"
	date := "2024-01-15"

	// Build expected cursors and responses
	cursors := make([]string, numPages)
	for i := 0; i < numPages; i++ {
		cursors[i] = "cursor-page-" + string(rune('A'+i))
	}
	finalCursor := "cursor-final"

	// Setup expectations
	linkedItemRepo.On("GetByID", ctx, linkedItemID).Return(linkedItem, nil)

	// Build responses for each page
	var previousCursor *string
	for pageNum := 0; pageNum < numPages; pageNum++ {
		// Generate transactions for this page
		transactions := make([]plaid.Transaction, transactionsPerPage)
		for i := 0; i < transactionsPerPage; i++ {
			txnID := "txn-page-" + string(rune('A'+pageNum)) + "-" + string(rune('0'+i%10)) + string(rune('0'+(i/10)%10))
			amount := float64(10 + pageNum*100 + i)
			transactions[i] = plaid.Transaction{
				Amount:        amount,
				TransactionId: txnID,
				AccountId:     accountID,
				Date:          date,
				Name:          "Store " + txnID,
				Pending:       false,
			}
		}

		isLastPage := pageNum == numPages-1
		var nextCursor string
		if isLastPage {
			nextCursor = finalCursor
		} else {
			nextCursor = cursors[pageNum]
		}

		syncResp := &plaid.TransactionsSyncResponse{
			Added:      transactions,
			Modified:   []plaid.Transaction{},
			Removed:    []plaid.RemovedTransaction{},
			NextCursor: nextCursor,
			HasMore:    !isLastPage,
		}

		// Mock the Plaid API call
		if previousCursor == nil {
			plaidClient.On("TransactionsSync", ctx, "encrypted-token", (*string)(nil)).Return(syncResp, nil).Once()
		} else {
			cursorCopy := *previousCursor
			plaidClient.On("TransactionsSync", ctx, "encrypted-token", &cursorCopy).Return(syncResp, nil).Once()
		}

		// Mock CreateBatch for each page
		txnRepo.On("CreateBatch", ctx, mock.MatchedBy(func(txns []domain.Transaction) bool {
			return len(txns) == transactionsPerPage
		})).Return(nil).Once()

		// Mock Emit for each transaction in this page (one event per transaction)
		for i := 0; i < transactionsPerPage; i++ {
			emitter.On("Emit", ctx, mock.MatchedBy(func(e events.TransactionEvent) bool {
				return e.Type == events.EventTypeAdded && e.UserID == "user-123"
			})).Return(nil).Once()
		}

		// Update previous cursor for next iteration
		if !isLastPage {
			previousCursor = &cursors[pageNum]
		}
	}

	// Final cursor update
	linkedItemRepo.On("UpdateCursor", ctx, linkedItemID, finalCursor).Return(nil)

	// Execute
	count, err := provider.SyncForLinkedItem(ctx, linkedItemID)

	// Assert
	assert.NoError(t, err)
	expectedCount := numPages * transactionsPerPage
	assert.Equal(t, expectedCount, count, "Should have synced %d transactions across %d pages", expectedCount, numPages)

	// Verify all expectations were met
	linkedItemRepo.AssertExpectations(t)
	txnRepo.AssertExpectations(t)
	plaidClient.AssertExpectations(t)
	emitter.AssertExpectations(t)

	// Verify specific call counts (AssertNumberOfCalls returns true if count matches)
	assert.True(t, plaidClient.AssertNumberOfCalls(t, "TransactionsSync", numPages), "Should have made exactly %d Plaid API calls", numPages)
	assert.True(t, txnRepo.AssertNumberOfCalls(t, "CreateBatch", numPages), "Should have created %d batches", numPages)
	assert.True(t, emitter.AssertNumberOfCalls(t, "Emit", expectedCount), "Should have emitted %d events (one per transaction)", expectedCount)
}

// TestPlaidSyncProvider_Analytics_CapturesOnSuccess verifies analytics events are emitted on successful sync.
func TestPlaidSyncProvider_Analytics_CapturesOnSuccess(t *testing.T) {
	linkedItemRepo := new(MockLinkedItemRepository)
	txnRepo := new(MockTransactionRepository)
	plaidClient := new(MockPlaidClient)
	emitter := new(MockEventEmitter)
	analytics := new(MockAnalytics)

	provider := NewPlaidSyncProvider(linkedItemRepo, txnRepo, plaidClient, emitter)
	provider.(*plaidSyncProvider).SetAnalytics(analytics)
	ctx := context.Background()

	linkedItemID := "test-linked-item"
	cursor := "test-cursor"
	linkedItem := &domain.LinkedItem{
		ID:                   linkedItemID,
		UserID:               "user-123",
		AccessTokenEncrypted: "encrypted-token",
		Cursor:               &cursor,
	}

	txnID := "plaid-txn-1"
	nextCursor := "new-cursor"
	syncResp := &plaid.TransactionsSyncResponse{
		Added: []plaid.Transaction{{
			Amount:        10.0,
			TransactionId: txnID,
			AccountId:     "acc-123",
			Date:          "2024-01-15",
			Name:          "Test",
			Pending:       false,
		}},
		Modified:   []plaid.Transaction{},
		Removed:    []plaid.RemovedTransaction{},
		NextCursor: nextCursor,
		HasMore:    false,
	}

	linkedItemRepo.On("GetByID", ctx, linkedItemID).Return(linkedItem, nil)
	plaidClient.On("TransactionsSync", ctx, "encrypted-token", &cursor).Return(syncResp, nil)
	txnRepo.On("CreateBatch", ctx, mock.Anything).Return(nil)
	emitter.On("Emit", ctx, mock.Anything).Return(nil)
	linkedItemRepo.On("UpdateCursor", ctx, linkedItemID, nextCursor).Return(nil)

	// Expect sync_completed and account_synced analytics events
	analytics.On("Capture", "user-123", "sync_completed", mock.MatchedBy(func(props map[string]interface{}) bool {
		return props["linked_item_id"] == linkedItemID && props["transaction_count"] == 1
	})).Once()
	analytics.On("Capture", "user-123", "account_synced", mock.MatchedBy(func(props map[string]interface{}) bool {
		return props["account_id"] == linkedItemID
	})).Once()

	count, err := provider.SyncForLinkedItem(ctx, linkedItemID)

	assert.NoError(t, err)
	assert.Equal(t, 1, count)
	analytics.AssertExpectations(t)
}

// TestPlaidSyncProvider_Analytics_CapturesOnFailure verifies sync_failed event is emitted on error.
func TestPlaidSyncProvider_Analytics_CapturesOnFailure(t *testing.T) {
	linkedItemRepo := new(MockLinkedItemRepository)
	txnRepo := new(MockTransactionRepository)
	plaidClient := new(MockPlaidClient)
	emitter := new(MockEventEmitter)
	analytics := new(MockAnalytics)

	provider := NewPlaidSyncProvider(linkedItemRepo, txnRepo, plaidClient, emitter)
	provider.(*plaidSyncProvider).SetAnalytics(analytics)
	ctx := context.Background()

	linkedItemID := "test-linked-item"
	linkedItem := &domain.LinkedItem{
		ID:                   linkedItemID,
		UserID:               "user-123",
		AccessTokenEncrypted: "encrypted-token",
		Cursor:               nil,
	}

	linkedItemRepo.On("GetByID", ctx, linkedItemID).Return(linkedItem, nil)
	plaidClient.On("TransactionsSync", ctx, "encrypted-token", (*string)(nil)).
		Return(nil, errors.New("plaid API error"))

	// Expect sync_failed analytics event
	analytics.On("Capture", "user-123", "sync_failed", (map[string]interface{})(nil)).Once()

	count, err := provider.SyncForLinkedItem(ctx, linkedItemID)

	assert.Error(t, err)
	assert.Equal(t, 0, count)
	analytics.AssertExpectations(t)
}

// TestPlaidSyncProvider_Analytics_NoCapture_WhenNil verifies no panic when analytics is nil.
func TestPlaidSyncProvider_Analytics_NoCapture_WhenNil(t *testing.T) {
	linkedItemRepo := new(MockLinkedItemRepository)
	txnRepo := new(MockTransactionRepository)
	plaidClient := new(MockPlaidClient)
	emitter := new(MockEventEmitter)

	// No analytics set
	provider := NewPlaidSyncProvider(linkedItemRepo, txnRepo, plaidClient, emitter)
	ctx := context.Background()

	linkedItemID := "test-linked-item"
	linkedItem := &domain.LinkedItem{
		ID:                   linkedItemID,
		UserID:               "user-123",
		AccessTokenEncrypted: "encrypted-token",
		Cursor:               nil,
	}

	nextCursor := "new-cursor"
	syncResp := &plaid.TransactionsSyncResponse{
		Added:      []plaid.Transaction{},
		Modified:   []plaid.Transaction{},
		Removed:    []plaid.RemovedTransaction{},
		NextCursor: nextCursor,
		HasMore:    false,
	}

	linkedItemRepo.On("GetByID", ctx, linkedItemID).Return(linkedItem, nil)
	plaidClient.On("TransactionsSync", ctx, "encrypted-token", (*string)(nil)).Return(syncResp, nil)
	linkedItemRepo.On("UpdateCursor", ctx, linkedItemID, nextCursor).Return(nil)

	// Should not panic
	count, err := provider.SyncForLinkedItem(ctx, linkedItemID)

	assert.NoError(t, err)
	assert.Equal(t, 0, count)
}
