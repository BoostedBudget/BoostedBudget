package workers

import (
	"context"
	"testing"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/repository"
	"github.com/plaid/plaid-go/v23/plaid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
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
		Amount:                25.50,
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

// ============================================================================
// Issue #1058: Worker-path PFC fallback tests (T3)
// Mirrors transaction_mapper_test.go coverage for the service path.
// ============================================================================

func makeTestLinkedItem() *domain.LinkedItem {
	return &domain.LinkedItem{
		ID:     "item-123",
		UserID: "user-456",
	}
}

func makeTestProvider() *plaidSyncProvider {
	return NewPlaidSyncProvider(
		new(MockLinkedItemRepository),
		new(MockTransactionRepository),
		new(MockPlaidClient),
		new(MockEventEmitter),
	).(*plaidSyncProvider)
}

// TestConvertPlaidTransaction_PFCFields_Populated verifies that when Plaid returns
// personal_finance_category, the worker stores PFCPrimary, PFCDetailed, and PFCConfidenceLevel.
func TestConvertPlaidTransaction_PFCFields_Populated(t *testing.T) {
	provider := makeTestProvider()
	linkedItem := makeTestLinkedItem()

	pfc := plaid.PersonalFinanceCategory{
		Primary:  "FOOD_AND_DRINK",
		Detailed: "FOOD_AND_DRINK_FAST_FOOD",
	}
	pfc.SetConfidenceLevel("VERY_HIGH")

	plaidTxn := plaid.Transaction{
		TransactionId:           "plaid-txn-pfc",
		AccountId:               "plaid-acc-1",
		Amount:                  12.45,
		Date:                    "2024-01-15",
		Name:                    "KFC",
		PersonalFinanceCategory: *plaid.NewNullablePersonalFinanceCategory(&pfc),
	}

	result, err := provider.convertPlaidTransaction(linkedItem, plaidTxn)

	require.NoError(t, err)
	require.NotNil(t, result.PFCPrimary, "PFCPrimary should be populated from personal_finance_category")
	assert.Equal(t, "FOOD_AND_DRINK", *result.PFCPrimary)
	require.NotNil(t, result.PFCDetailed)
	assert.Equal(t, "FOOD_AND_DRINK_FAST_FOOD", *result.PFCDetailed)
	require.NotNil(t, result.PFCConfidenceLevel)
	assert.Equal(t, "VERY_HIGH", *result.PFCConfidenceLevel)
}

// TestConvertPlaidTransaction_PFCAndLegacy_BothPresent verifies that when Plaid sends both
// legacy category and PFC, the PFCv2 fields are still stored correctly.
func TestConvertPlaidTransaction_PFCAndLegacy_BothPresent(t *testing.T) {
	provider := makeTestProvider()
	linkedItem := makeTestLinkedItem()

	pfc := plaid.PersonalFinanceCategory{
		Primary:  "FOOD_AND_DRINK",
		Detailed: "FOOD_AND_DRINK_FAST_FOOD",
	}

	plaidTxn := plaid.Transaction{
		TransactionId:           "plaid-txn-both",
		AccountId:               "plaid-acc-1",
		Amount:                  12.45,
		Date:                    "2024-01-15",
		Name:                    "KFC",
		Category:                []string{"Food and Drink", "Restaurants"},
		PersonalFinanceCategory: *plaid.NewNullablePersonalFinanceCategory(&pfc),
	}

	result, err := provider.convertPlaidTransaction(linkedItem, plaidTxn)

	require.NoError(t, err)
	require.NotNil(t, result.PFCPrimary, "PFCPrimary should be populated from PFC data")
	assert.Equal(t, "FOOD_AND_DRINK", *result.PFCPrimary)
}

// TestConvertPlaidTransaction_PartialPFC_EmptyPrimary verifies that PFC with empty Primary
// leaves PFCPrimary nil.
func TestConvertPlaidTransaction_PartialPFC_EmptyPrimary(t *testing.T) {
	provider := makeTestProvider()
	linkedItem := makeTestLinkedItem()

	pfc := plaid.PersonalFinanceCategory{
		Primary:  "",
		Detailed: "FOOD_AND_DRINK_FAST_FOOD",
	}

	plaidTxn := plaid.Transaction{
		TransactionId:           "plaid-txn-partial-pfc",
		AccountId:               "plaid-acc-1",
		Amount:                  12.45,
		Date:                    "2024-01-15",
		Name:                    "Unknown",
		PersonalFinanceCategory: *plaid.NewNullablePersonalFinanceCategory(&pfc),
	}

	result, err := provider.convertPlaidTransaction(linkedItem, plaidTxn)

	require.NoError(t, err)
	assert.Nil(t, result.PFCPrimary, "PFCPrimary must remain nil when PFC Primary is empty")
}

// TestConvertPlaidTransaction_PFCOnly_NoDetailed verifies PFC with only primary (no detailed)
// stores PFCPrimary but leaves PFCDetailed nil.
func TestConvertPlaidTransaction_PFCOnly_NoDetailed(t *testing.T) {
	provider := makeTestProvider()
	linkedItem := makeTestLinkedItem()

	pfc := plaid.PersonalFinanceCategory{
		Primary:  "TRANSPORTATION",
		Detailed: "",
	}

	plaidTxn := plaid.Transaction{
		TransactionId:           "plaid-txn-primary-only",
		AccountId:               "plaid-acc-1",
		Amount:                  25.00,
		Date:                    "2024-01-15",
		Name:                    "Transit",
		PersonalFinanceCategory: *plaid.NewNullablePersonalFinanceCategory(&pfc),
	}

	result, err := provider.convertPlaidTransaction(linkedItem, plaidTxn)

	require.NoError(t, err)
	require.NotNil(t, result.PFCPrimary)
	assert.Equal(t, "TRANSPORTATION", *result.PFCPrimary)
	assert.Nil(t, result.PFCDetailed, "PFCDetailed should be nil when Plaid sends empty detailed")
}
