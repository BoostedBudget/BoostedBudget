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
	service := NewTransactionSyncService(nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil).(*transactionSyncService)
	ctx := context.Background()

	linkedItemID := "linked-item-123"
	userID := "user-456"

	accountMap := map[string]string{
		"plaid-acc-1": "account-uuid-1",
	}

	authorizedDate := "2024-01-14"
	website := "https://amazon.com"
	merchantName := "Amazon"

	pfc := plaid.PersonalFinanceCategory{
		Primary:  "GENERAL_MERCHANDISE",
		Detailed: "GENERAL_MERCHANDISE_ONLINE_MARKETPLACE",
	}
	pfc.SetConfidenceLevel("HIGH")

	plaidTxn := plaid.Transaction{
		TransactionId:           "plaid-txn-123",
		AccountId:               "plaid-acc-1",
		Amount:                  100.50,
		Date:                    "2024-01-15",
		AuthorizedDate:          *plaid.NewNullableString(&authorizedDate),
		Name:                    "AMAZON.COM",
		MerchantName:            *plaid.NewNullableString(&merchantName),
		PaymentChannel:          "online",
		Pending:                 false,
		Website:                 *plaid.NewNullableString(&website),
		PersonalFinanceCategory: *plaid.NewNullablePersonalFinanceCategory(&pfc),
	}

	result, err := service.mapPlaidTransaction(ctx, plaidTxn, linkedItemID, userID, accountMap, nil)

	require.NoError(t, err)
	assert.Equal(t, userID, result.UserID)
	assert.Equal(t, "account-uuid-1", result.AccountID)
	assert.Equal(t, linkedItemID, result.LinkedItemID)
	assert.Equal(t, domain.SourcePlaid, result.Source)
	assert.Equal(t, "plaid-txn-123", *result.ExternalTransactionID)
	assert.Equal(t, 100.50, result.Amount)
	assert.Equal(t, "AMAZON.COM", result.Name)
	assert.Equal(t, "Amazon", *result.MerchantName)
	assert.Equal(t, "online", *result.PaymentChannel)
	assert.False(t, result.Pending)
	assert.Nil(t, result.UserNotes)

	// Check date parsing
	expectedDate, _ := time.Parse("2006-01-02", "2024-01-15")
	assert.Equal(t, expectedDate, result.Date)

	// Check authorized date
	expectedAuthDate, _ := time.Parse("2006-01-02", "2024-01-14")
	assert.Equal(t, expectedAuthDate, *result.AuthorizedDate)

	// Check PFC v2 fields
	require.NotNil(t, result.PFCPrimary)
	assert.Equal(t, "GENERAL_MERCHANDISE", *result.PFCPrimary)
	require.NotNil(t, result.PFCDetailed)
	assert.Equal(t, "GENERAL_MERCHANDISE_ONLINE_MARKETPLACE", *result.PFCDetailed)
	require.NotNil(t, result.PFCConfidenceLevel)
	assert.Equal(t, "HIGH", *result.PFCConfidenceLevel)

	// Check external metadata includes website
	require.NotNil(t, result.ExternalMetadata)
	var metadata map[string]interface{}
	require.NoError(t, json.Unmarshal([]byte(*result.ExternalMetadata), &metadata))
	assert.Equal(t, "https://amazon.com", metadata["website"])
}

// TestMapPlaidTransaction_MissingAccount tests error when account not in map
func TestMapPlaidTransaction_MissingAccount(t *testing.T) {
	service := NewTransactionSyncService(nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil).(*transactionSyncService)
	ctx := context.Background()

	accountMap := map[string]string{}

	plaidTxn := plaid.Transaction{
		TransactionId: "plaid-txn-123",
		AccountId:     "plaid-acc-UNKNOWN",
		Amount:        50.00,
		Date:          "2024-01-15",
		Name:          "Test Transaction",
	}

	result, err := service.mapPlaidTransaction(ctx, plaidTxn, "item-1", "user-1", accountMap, nil)

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

	plaidTxn := plaid.Transaction{
		TransactionId: "plaid-txn-minimal",
		AccountId:     "plaid-acc-1",
		Amount:        25.00,
		Date:          "2024-01-15",
		Name:          "Simple Transaction",
		Pending:       true,
	}

	result, err := service.mapPlaidTransaction(ctx, plaidTxn, "item-1", "user-1", accountMap, nil)

	require.NoError(t, err)
	assert.Equal(t, "Simple Transaction", result.Name)
	assert.Equal(t, 25.00, result.Amount)
	assert.True(t, result.Pending)
	assert.Nil(t, result.MerchantName)
	assert.Nil(t, result.PFCPrimary)
	assert.Nil(t, result.PFCDetailed)
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

	result, err := service.mapPlaidTransaction(ctx, plaidTxn, "item-1", "user-1", accountMap, nil)

	assert.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "failed to map plaid transaction")
}

// ============================================================================
// Issue #1058: PFC Fallback Tests
// ============================================================================

// TestMapPlaidTransaction_PFCFallback_NoLegacyCategory verifies that when Plaid returns no legacy
// category but does return personal_finance_category, the domain transaction stores PFCPrimary/PFCDetailed.
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
		PersonalFinanceCategory: *plaid.NewNullablePersonalFinanceCategory(&pfc),
	}

	result, err := service.mapPlaidTransaction(ctx, plaidTxn, "item-1", "user-1", accountMap, nil)

	require.NoError(t, err)
	require.NotNil(t, result.PFCPrimary, "PFCPrimary should be populated from personal_finance_category")
	assert.Equal(t, "FOOD_AND_DRINK", *result.PFCPrimary)
	require.NotNil(t, result.PFCDetailed)
	assert.Equal(t, "FOOD_AND_DRINK_FAST_FOOD", *result.PFCDetailed)
	require.NotNil(t, result.PFCConfidenceLevel)
	assert.Equal(t, "VERY_HIGH", *result.PFCConfidenceLevel)
}

// TestMapPlaidTransaction_LegacyCategoryIgnored verifies that legacy category from Plaid
// is not stored — only PFC v2 fields are persisted.
func TestMapPlaidTransaction_LegacyCategoryIgnored(t *testing.T) {
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
		Category:                []string{"Food and Drink", "Restaurants"}, // legacy — ignored in PFCv2
		PersonalFinanceCategory: *plaid.NewNullablePersonalFinanceCategory(&pfc),
	}

	result, err := service.mapPlaidTransaction(ctx, plaidTxn, "item-1", "user-1", accountMap, nil)

	require.NoError(t, err)
	// PFC v2 fields should be stored
	require.NotNil(t, result.PFCPrimary, "PFCPrimary should be populated from PFC data")
	assert.Equal(t, "FOOD_AND_DRINK", *result.PFCPrimary)
	assert.Equal(t, "FOOD_AND_DRINK_FAST_FOOD", *result.PFCDetailed)
}

// TestMapPlaidTransaction_PartialPFC_EmptyPrimary verifies that PFC with an empty Primary
// leaves PFCPrimary nil.
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
		PersonalFinanceCategory: *plaid.NewNullablePersonalFinanceCategory(&pfc),
	}

	result, err := service.mapPlaidTransaction(ctx, plaidTxn, "item-1", "user-1", accountMap, nil)

	require.NoError(t, err)
	assert.Nil(t, result.PFCPrimary, "PFCPrimary must remain nil when PFC Primary is empty")
}

// TestMapPlaidTransaction_PFCOnly_NoDetailed verifies PFC with only primary (no detailed)
// stores PFCPrimary but leaves PFCDetailed nil.
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
		PersonalFinanceCategory: *plaid.NewNullablePersonalFinanceCategory(&pfc),
	}

	result, err := service.mapPlaidTransaction(ctx, plaidTxn, "item-1", "user-1", accountMap, nil)

	require.NoError(t, err)
	require.NotNil(t, result.PFCPrimary)
	assert.Equal(t, "TRANSPORTATION", *result.PFCPrimary)
	assert.Nil(t, result.PFCDetailed, "PFCDetailed should be nil when Plaid sends empty detailed")
}

// ============================================================================
// Issue #1058: Mapper parity tests (T12)
// Guards against PFC field divergence between the two ingest paths
// (service mapPlaidTransaction vs worker convertPlaidTransaction).
// ============================================================================

// TestMapPlaidTransaction_PFCFields_AnchorExpectations asserts the exact PFC field values
// for the three PFC scenarios.
func TestMapPlaidTransaction_PFCFields_AnchorExpectations(t *testing.T) {
	service := NewTransactionSyncService(nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil).(*transactionSyncService)
	ctx := context.Background()
	accountMap := map[string]string{"acc": "uuid"}

	t.Run("PFC only: PFCPrimary and PFCDetailed populated", func(t *testing.T) {
		pfc := plaid.PersonalFinanceCategory{Primary: "FOOD_AND_DRINK", Detailed: "FOOD_AND_DRINK_FAST_FOOD"}
		txn := plaid.Transaction{
			TransactionId: "t1", AccountId: "acc", Amount: 10, Date: "2024-01-01", Name: "KFC",
			PersonalFinanceCategory: *plaid.NewNullablePersonalFinanceCategory(&pfc),
		}
		got, err := service.mapPlaidTransaction(ctx, txn, "item", "user", accountMap, nil)
		require.NoError(t, err)

		require.NotNil(t, got.PFCPrimary)
		assert.Equal(t, "FOOD_AND_DRINK", *got.PFCPrimary)
		require.NotNil(t, got.PFCDetailed)
		assert.Equal(t, "FOOD_AND_DRINK_FAST_FOOD", *got.PFCDetailed)
	})

	t.Run("Legacy category present with PFC: PFC fields stored", func(t *testing.T) {
		pfc := plaid.PersonalFinanceCategory{Primary: "FOOD_AND_DRINK", Detailed: "FOOD_AND_DRINK_FAST_FOOD"}
		txn := plaid.Transaction{
			TransactionId: "t2", AccountId: "acc", Amount: 10, Date: "2024-01-01", Name: "KFC",
			Category:                []string{"Food and Drink", "Restaurants"},
			PersonalFinanceCategory: *plaid.NewNullablePersonalFinanceCategory(&pfc),
		}
		got, err := service.mapPlaidTransaction(ctx, txn, "item", "user", accountMap, nil)
		require.NoError(t, err)

		require.NotNil(t, got.PFCPrimary)
		assert.Equal(t, "FOOD_AND_DRINK", *got.PFCPrimary)
	})

	t.Run("Empty PFC Primary: PFCPrimary nil", func(t *testing.T) {
		pfc := plaid.PersonalFinanceCategory{Primary: "", Detailed: "FOOD_AND_DRINK_FAST_FOOD"}
		txn := plaid.Transaction{
			TransactionId: "t3", AccountId: "acc", Amount: 10, Date: "2024-01-01", Name: "Unknown",
			PersonalFinanceCategory: *plaid.NewNullablePersonalFinanceCategory(&pfc),
		}
		got, err := service.mapPlaidTransaction(ctx, txn, "item", "user", accountMap, nil)
		require.NoError(t, err)
		assert.Nil(t, got.PFCPrimary, "PFCPrimary must be nil when PFC Primary is empty")
		// PFCDetailed is still stored even without a primary (implementation stores what Plaid sends)
	})
}
