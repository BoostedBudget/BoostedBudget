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
	service := NewTransactionSyncService(nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil).(*transactionSyncService)
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
	result, err := service.mapPlaidTransaction(ctx, plaidTxn, linkedItemID, userID, accountMap, nil)

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
	service := NewTransactionSyncService(nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil).(*transactionSyncService)
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
	result, err := service.mapPlaidTransaction(ctx, plaidTxn, "item-1", "user-1", accountMap, nil)

	// Assert: Should return error
	assert.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "account not found")
	assert.Contains(t, err.Error(), "plaid-acc-UNKNOWN")
}

// TestMapPlaidTransaction_MinimalFields tests with only required fields
func TestMapPlaidTransaction_MinimalFields(t *testing.T) {
	service := NewTransactionSyncService(nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil).(*transactionSyncService)
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
	result, err := service.mapPlaidTransaction(ctx, plaidTxn, "item-1", "user-1", accountMap, nil)

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
	service := NewTransactionSyncService(nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil).(*transactionSyncService)
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
	result, err := service.mapPlaidTransaction(ctx, plaidTxn, "item-1", "user-1", accountMap, nil)

	// Assert: Should return error
	assert.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "failed to parse transaction date")
}

// ============================================================================
// Issue #1058: PFC Fallback Tests
// ============================================================================

// TestMapPlaidTransaction_PFCFallback_NoLegacyCategory verifies that when Plaid returns no legacy
// category but does return personal_finance_category, the domain transaction's Category is
// synthesized from PFC primary + detailed.
func TestMapPlaidTransaction_PFCFallback_NoLegacyCategory(t *testing.T) {
	service := NewTransactionSyncService(nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil).(*transactionSyncService)
	ctx := context.Background()

	accountMap := map[string]string{"plaid-acc-1": "account-uuid-1"}

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
		Category:                nil, // no legacy category
		PersonalFinanceCategory: *plaid.NewNullablePersonalFinanceCategory(&pfc),
	}

	result, err := service.mapPlaidTransaction(ctx, plaidTxn, "item-1", "user-1", accountMap, nil)

	require.NoError(t, err)
	require.NotNil(t, result.Category, "Category should be synthesized from PFC when legacy is absent")

	var categories []string
	require.NoError(t, json.Unmarshal([]byte(*result.Category), &categories))
	assert.Equal(t, "Food And Drink", categories[0], "Primary display name should be curated")
	assert.Equal(t, "Fast Food", categories[1], "Detailed display name should strip primary prefix")

	// OriginalCategory must NOT be set from synthesized data — faithful to Plaid's actual output
	assert.Nil(t, result.OriginalCategory, "OriginalCategory must remain nil when Plaid sent no legacy category")
}

// TestMapPlaidTransaction_LegacyCategoryTakesPriority verifies legacy category wins over PFC
// when Plaid sends both.
func TestMapPlaidTransaction_LegacyCategoryTakesPriority(t *testing.T) {
	service := NewTransactionSyncService(nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil).(*transactionSyncService)
	ctx := context.Background()

	accountMap := map[string]string{"plaid-acc-1": "account-uuid-1"}

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
		Category:                []string{"Food and Drink", "Restaurants"}, // legacy present
		PersonalFinanceCategory: *plaid.NewNullablePersonalFinanceCategory(&pfc),
	}

	result, err := service.mapPlaidTransaction(ctx, plaidTxn, "item-1", "user-1", accountMap, nil)

	require.NoError(t, err)
	require.NotNil(t, result.Category)

	var categories []string
	require.NoError(t, json.Unmarshal([]byte(*result.Category), &categories))
	assert.Equal(t, "Food and Drink", categories[0], "Legacy category should take priority over synthesized PFC")

	// OriginalCategory should also mirror legacy (existing behavior)
	require.NotNil(t, result.OriginalCategory)
	var origCategories []string
	require.NoError(t, json.Unmarshal([]byte(*result.OriginalCategory), &origCategories))
	assert.Equal(t, "Food and Drink", origCategories[0])
}

// TestMapPlaidTransaction_PartialPFC_EmptyPrimary verifies that PFC with an empty Primary
// is treated as absent and Category stays nil.
func TestMapPlaidTransaction_PartialPFC_EmptyPrimary(t *testing.T) {
	service := NewTransactionSyncService(nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil).(*transactionSyncService)
	ctx := context.Background()

	accountMap := map[string]string{"plaid-acc-1": "account-uuid-1"}

	pfc := plaid.PersonalFinanceCategory{
		Primary:  "", // empty primary — treat as missing
		Detailed: "FOOD_AND_DRINK_FAST_FOOD",
	}

	plaidTxn := plaid.Transaction{
		TransactionId:           "plaid-txn-partial-pfc",
		AccountId:               "plaid-acc-1",
		Amount:                  12.45,
		Date:                    "2024-01-15",
		Name:                    "Unknown Merchant",
		Category:                nil,
		PersonalFinanceCategory: *plaid.NewNullablePersonalFinanceCategory(&pfc),
	}

	result, err := service.mapPlaidTransaction(ctx, plaidTxn, "item-1", "user-1", accountMap, nil)

	require.NoError(t, err)
	assert.Nil(t, result.Category, "Category must remain nil when PFC Primary is empty")
	assert.Nil(t, result.OriginalCategory)
}

// TestMapPlaidTransaction_PFCOnly_NoDetailed verifies PFC with only primary (no detailed)
// synthesizes a single-element category array.
func TestMapPlaidTransaction_PFCOnly_NoDetailed(t *testing.T) {
	service := NewTransactionSyncService(nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil).(*transactionSyncService)
	ctx := context.Background()

	accountMap := map[string]string{"plaid-acc-1": "account-uuid-1"}

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
		Category:                nil,
		PersonalFinanceCategory: *plaid.NewNullablePersonalFinanceCategory(&pfc),
	}

	result, err := service.mapPlaidTransaction(ctx, plaidTxn, "item-1", "user-1", accountMap, nil)

	require.NoError(t, err)
	require.NotNil(t, result.Category)

	var categories []string
	require.NoError(t, json.Unmarshal([]byte(*result.Category), &categories))
	assert.Equal(t, []string{"Transportation"}, categories)
}
