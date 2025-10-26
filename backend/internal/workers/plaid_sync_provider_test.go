package workers

import (
	"context"
	"testing"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/repository"
	"github.com/plaid/plaid-go/v23/plaid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

// TestPlaidSyncProvider_ProcessModified_PendingToPosted tests the pending→posted transition
// This is the RED phase test - should fail initially until GetByExternalID is used
func TestPlaidSyncProvider_ProcessModified_PendingToPosted(t *testing.T) {
	linkedItemRepo := new(MockLinkedItemRepository)
	txnRepo := new(MockTransactionRepository)
	plaidClient := new(MockPlaidClient)
	emitter := new(MockEventEmitter)

	provider := NewPlaidSyncProvider(linkedItemRepo, txnRepo, plaidClient, emitter)
	ctx := context.Background()

	// Setup: Mock linked item
	linkedItem := &domain.LinkedItem{
		ID:                   "item-123",
		UserID:               "user-123",
		ItemID:               "plaid_item_456",
		AccessTokenEncrypted: "encrypted_access_token_789",
		Provider:             "plaid",
	}

	// Setup: Mock Plaid transaction (now posted, was previously pending)
	plaidTxnID := "plaid_tx_abc"
	accountID := "plaid_account_123"
	pending := false
	date := "2025-10-25"
	name := "Starbucks"

	plaidTxn := plaid.Transaction{
		TransactionId: plaidTxnID,
		AccountId:     accountID,
		Amount:        25.50,
		Date:          date,
		Name:          name,
		Pending:       pending, // Now posted (was pending before)
	}

	// Setup: Mock existing transaction in DB (with ID set)
	existingTxnID := "tx-uuid-123"
	externalTxnID := plaidTxnID
	existingTxn := &domain.Transaction{
		ID:                    existingTxnID, // This ID must be set
		UserID:                linkedItem.UserID,
		AccountID:             "account-456",
		LinkedItemID:          linkedItem.ID,
		Source:                domain.SourcePlaid,
		ExternalTransactionID: &externalTxnID,
		Amount:                -25.50,
		Pending:               true, // Was pending
	}

	// Mock: GetByExternalID should return existing transaction
	txnRepo.On("GetByExternalID", ctx, domain.SourcePlaid, plaidTxnID, linkedItem.UserID).
		Return(existingTxn, nil)

	// Mock: Update should be called with transaction that has ID set
	txnRepo.On("Update", ctx, mock.MatchedBy(func(txn *domain.Transaction) bool {
		// CRITICAL: Verify that the transaction passed to Update has the ID set
		return txn.ID == existingTxnID &&
		       txn.Pending == false && // Should be updated to posted
		       *txn.ExternalTransactionID == plaidTxnID
	})).Return(nil)

	// Mock: Event emitter
	emitter.On("Emit", ctx, mock.AnythingOfType("events.TransactionEvent")).Return(nil)

	// Execute: Process the modified transaction
	count, err := provider.(*plaidSyncProvider).processModified(ctx, linkedItem, []plaid.Transaction{plaidTxn})

	// Assert: Should succeed and process 1 transaction
	assert.NoError(t, err)
	assert.Equal(t, 1, count)

	// Assert: All mocks were called as expected
	txnRepo.AssertExpectations(t)
	emitter.AssertExpectations(t)
}

// TestPlaidSyncProvider_ProcessModified_TransactionNotFound tests error handling when transaction doesn't exist
func TestPlaidSyncProvider_ProcessModified_TransactionNotFound(t *testing.T) {
	linkedItemRepo := new(MockLinkedItemRepository)
	txnRepo := new(MockTransactionRepository)
	plaidClient := new(MockPlaidClient)
	emitter := new(MockEventEmitter)

	provider := NewPlaidSyncProvider(linkedItemRepo, txnRepo, plaidClient, emitter)
	ctx := context.Background()

	linkedItem := &domain.LinkedItem{
		ID:                   "item-123",
		UserID:               "user-123",
		ItemID:               "plaid_item_456",
		AccessTokenEncrypted: "encrypted_access_token_789",
		Provider:             "plaid",
	}

	plaidTxnID := "plaid_tx_nonexistent"
	accountID := "plaid_account_123"
	pending := false
	date := "2025-10-25"
	name := "Transaction"

	plaidTxn := plaid.Transaction{
		TransactionId: plaidTxnID,
		AccountId:     accountID,
		Amount:        25.50,
		Date:          date,
		Name:          name,
		Pending:       pending,
	}

	// Mock: GetByExternalID returns not found error
	txnRepo.On("GetByExternalID", ctx, domain.SourcePlaid, plaidTxnID, linkedItem.UserID).
		Return(nil, repository.ErrTransactionNotFound)

	// Execute
	count, err := provider.(*plaidSyncProvider).processModified(ctx, linkedItem, []plaid.Transaction{plaidTxn})

	// Assert: Should fail with not found error
	assert.Error(t, err)
	assert.Equal(t, 0, count)
	assert.Contains(t, err.Error(), "failed to find existing transaction")

	// Assert: Update should NOT have been called
	txnRepo.AssertNotCalled(t, "Update")
}
