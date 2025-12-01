package services

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/plaid/plaid-go/v23/plaid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ============================================================================
// Issue #261: mapPlaidTransaction Tests
// ============================================================================

// TestMapPlaidTransaction_AllFields tests complete field mapping
func TestMapPlaidTransaction_AllFields(t *testing.T) {
	// Setup
	service := NewTransactionSyncService(nil, nil, nil, nil, nil, nil, nil, nil, nil, nil).(*transactionSyncService)
	ctx := context.Background()

	linkedItemID := "linked-item-123"
	userID := "user-456"

	// Account map: Plaid account_id -> Internal UUID
	accountMap := map[string]string{
		"plaid-acc-1": "account-uuid-1",
	}

	// Create comprehensive Plaid transaction
	authorizedDate := "2024-01-14"
	website := "https://amazon.com"
	merchantName := "Amazon"

	plaidTxn := plaid.Transaction{
		TransactionId: "plaid-txn-123",
		AccountId:     "plaid-acc-1",
		Amount:        100.50,
		Date:          "2024-01-15",
		AuthorizedDate: *plaid.NewNullableString(&authorizedDate),
		Name:          "AMAZON.COM",
		MerchantName:  *plaid.NewNullableString(&merchantName),
		Category:      []string{"Shops", "Supermarkets and Groceries"},
		PaymentChannel: "online",
		Pending:       false,
		Website:       *plaid.NewNullableString(&website),
	}

	// Execute
	result, err := service.mapPlaidTransaction(ctx, plaidTxn, linkedItemID, userID, accountMap)

	// Assert
	require.NoError(t, err)
	assert.Equal(t, userID, result.UserID)
	assert.Equal(t, "account-uuid-1", result.AccountID) // Mapped correctly!
	assert.Equal(t, linkedItemID, result.LinkedItemID)
	assert.Equal(t, domain.SourcePlaid, result.Source)
	assert.Equal(t, "plaid-txn-123", *result.ExternalTransactionID)
	assert.Equal(t, 100.50, result.Amount)
	assert.Equal(t, "AMAZON.COM", result.Name)
	assert.Equal(t, "Amazon", *result.MerchantName)
	assert.Equal(t, "online", *result.PaymentChannel)
	assert.False(t, result.Pending)
	assert.Nil(t, result.UserNotes) // Always nil for new transactions

	// Check date parsing
	expectedDate, _ := time.Parse("2006-01-02", "2024-01-15")
	assert.Equal(t, expectedDate, result.Date)

	// Check authorized date
	expectedAuthDate, _ := time.Parse("2006-01-02", "2024-01-14")
	assert.Equal(t, expectedAuthDate, *result.AuthorizedDate)

	// Check category JSON serialization
	var category []string
	json.Unmarshal([]byte(*result.Category), &category)
	assert.Equal(t, []string{"Shops", "Supermarkets and Groceries"}, category)

	// Check external metadata includes website
	var metadata map[string]interface{}
	json.Unmarshal([]byte(*result.ExternalMetadata), &metadata)
	assert.Equal(t, "https://amazon.com", metadata["website"])
}

// TestMapPlaidTransaction_MissingAccount tests error when account not in map
func TestMapPlaidTransaction_MissingAccount(t *testing.T) {
	service := NewTransactionSyncService(nil, nil, nil, nil, nil, nil, nil, nil, nil, nil).(*transactionSyncService)
	ctx := context.Background()

	// Empty account map
	accountMap := map[string]string{}

	plaidTxn := plaid.Transaction{
		TransactionId: "plaid-txn-123",
		AccountId:     "plaid-acc-UNKNOWN",
		Amount:        50.00,
		Date:          "2024-01-15",
		Name:          "Test Transaction",
	}

	// Execute
	result, err := service.mapPlaidTransaction(ctx, plaidTxn, "item-1", "user-1", accountMap)

	// Assert: Should return error
	assert.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "account not found")
	assert.Contains(t, err.Error(), "plaid-acc-UNKNOWN")
}

// TestMapPlaidTransaction_MinimalFields tests with only required fields
func TestMapPlaidTransaction_MinimalFields(t *testing.T) {
	service := NewTransactionSyncService(nil, nil, nil, nil, nil, nil, nil, nil, nil, nil).(*transactionSyncService)
	ctx := context.Background()

	accountMap := map[string]string{
		"plaid-acc-1": "account-uuid-1",
	}

	// Minimal Plaid transaction
	plaidTxn := plaid.Transaction{
		TransactionId: "plaid-txn-minimal",
		AccountId:     "plaid-acc-1",
		Amount:        25.00,
		Date:          "2024-01-15",
		Name:          "Simple Transaction",
		Pending:       true,
	}

	// Execute
	result, err := service.mapPlaidTransaction(ctx, plaidTxn, "item-1", "user-1", accountMap)

	// Assert
	require.NoError(t, err)
	assert.Equal(t, "Simple Transaction", result.Name)
	assert.Equal(t, 25.00, result.Amount)
	assert.True(t, result.Pending)
	assert.Nil(t, result.MerchantName)
	assert.Nil(t, result.Category)
	assert.Nil(t, result.PaymentChannel)
	assert.Nil(t, result.ExternalMetadata)
}

// TestMapPlaidTransaction_InvalidDate tests date parsing error
func TestMapPlaidTransaction_InvalidDate(t *testing.T) {
	service := NewTransactionSyncService(nil, nil, nil, nil, nil, nil, nil, nil, nil, nil).(*transactionSyncService)
	ctx := context.Background()

	accountMap := map[string]string{
		"plaid-acc-1": "account-uuid-1",
	}

	plaidTxn := plaid.Transaction{
		TransactionId: "plaid-txn-123",
		AccountId:     "plaid-acc-1",
		Amount:        50.00,
		Date:          "invalid-date",
		Name:          "Test",
	}

	// Execute
	result, err := service.mapPlaidTransaction(ctx, plaidTxn, "item-1", "user-1", accountMap)

	// Assert: Should return error
	assert.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "failed to parse transaction date")
}
