package services

import (
	"testing"

	"github.com/plaid/plaid-go/v23/plaid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExtractPlaidPFCFields_AllFields(t *testing.T) {
	confidenceStr := "HIGH"
	pfcData := plaid.NewPersonalFinanceCategory("FOOD_AND_DRINK", "FOOD_AND_DRINK_COFFEE")
	pfcData.ConfidenceLevel = *plaid.NewNullableString(&confidenceStr)

	iconURL := "https://plaid-category-icons.plaid.com/PFC_FOOD_AND_DRINK.png"
	plaidTxn := plaid.Transaction{
		PersonalFinanceCategoryIconUrl: &iconURL,
	}
	plaidTxn.PersonalFinanceCategory = *plaid.NewNullablePersonalFinanceCategory(pfcData)

	fields := ExtractPlaidPFCFields(plaidTxn)

	require.NotNil(t, fields.Primary)
	assert.Equal(t, "FOOD_AND_DRINK", *fields.Primary)
	require.NotNil(t, fields.Detailed)
	assert.Equal(t, "FOOD_AND_DRINK_COFFEE", *fields.Detailed)
	require.NotNil(t, fields.ConfidenceLevel)
	assert.Equal(t, "HIGH", *fields.ConfidenceLevel)
	require.NotNil(t, fields.CategoryIconURL)
	assert.Equal(t, iconURL, *fields.CategoryIconURL)
}

func TestExtractPlaidPFCFields_NoPFC(t *testing.T) {
	plaidTxn := plaid.Transaction{}

	fields := ExtractPlaidPFCFields(plaidTxn)

	assert.Nil(t, fields.Primary)
	assert.Nil(t, fields.Detailed)
	assert.Nil(t, fields.ConfidenceLevel)
	assert.Nil(t, fields.CategoryIconURL)
}

func TestExtractPlaidPFCFields_EmptyIconURL(t *testing.T) {
	emptyURL := ""
	plaidTxn := plaid.Transaction{
		PersonalFinanceCategoryIconUrl: &emptyURL,
	}

	fields := ExtractPlaidPFCFields(plaidTxn)

	assert.Nil(t, fields.CategoryIconURL, "empty icon URL should be treated as nil")
}

func TestBuildPlaidExternalMetadata_WithData(t *testing.T) {
	website := "https://example.com"
	plaidTxn := plaid.Transaction{
		Website: *plaid.NewNullableString(&website),
	}

	meta, err := BuildPlaidExternalMetadata(plaidTxn)

	require.NoError(t, err)
	require.NotNil(t, meta)
	assert.Contains(t, *meta, "example.com")
}

func TestBuildPlaidExternalMetadata_Empty(t *testing.T) {
	plaidTxn := plaid.Transaction{}

	meta, err := BuildPlaidExternalMetadata(plaidTxn)

	require.NoError(t, err)
	assert.Nil(t, meta, "should return nil when no metadata fields are set")
}

func TestMapPlaidTransactionBase_SetsCommonFields(t *testing.T) {
	plaidTxn := plaid.Transaction{
		TransactionId: "txn-base-test",
		AccountId:     "acct-1",
		Amount:        42.50,
		Date:          "2024-06-15",
		Name:          "Test Store",
		Pending:       false,
	}

	tx, err := MapPlaidTransactionBase(plaidTxn)
	require.NoError(t, err)

	assert.Equal(t, 42.50, tx.Amount)
	assert.Equal(t, "Test Store", tx.Name)
	assert.Equal(t, 2024, tx.Date.Year())
	assert.Equal(t, 6, int(tx.Date.Month()))
	assert.Equal(t, 15, tx.Date.Day())
}

func TestMapPlaidTransactionBase_InvalidDate(t *testing.T) {
	plaidTxn := plaid.Transaction{
		TransactionId: "txn-bad-date",
		AccountId:     "acct-1",
		Amount:        10.00,
		Date:          "not-a-date",
		Name:          "Bad Date Transaction",
	}

	_, err := MapPlaidTransactionBase(plaidTxn)
	assert.Error(t, err)
}

func TestMapPlaidTransactionBase_SetsIconURL(t *testing.T) {
	iconURL := "https://plaid-category-icons.plaid.com/PFC_INCOME.png"
	plaidTxn := plaid.Transaction{
		TransactionId:                  "txn-icon",
		AccountId:                      "acct-1",
		Amount:                         1000.00,
		Date:                           "2024-01-01",
		Name:                           "Payroll",
		PersonalFinanceCategoryIconUrl: &iconURL,
	}

	tx, err := MapPlaidTransactionBase(plaidTxn)
	require.NoError(t, err)
	require.NotNil(t, tx.PFCCategoryIconURL)
	assert.Equal(t, iconURL, *tx.PFCCategoryIconURL)
}
