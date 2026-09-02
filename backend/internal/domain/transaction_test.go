package domain

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestTransaction_Validate_Success_Plaid tests validation for Plaid transactions
func TestTransaction_Validate_Success_Plaid(t *testing.T) {
	externalID := "plaid_tx_123"
	transaction := &Transaction{
		ID:                    "tx-uuid-1",
		UserID:                "user-uuid-1",
		AccountID:             "account-uuid-1",
		LinkedItemID:          "item-uuid-1",
		Source:                SourcePlaid,
		ExternalTransactionID: &externalID,
		Amount:                -50.25,
		Date:                  time.Now(),
		Name:                  "Starbucks",
	}

	err := transaction.Validate()
	assert.NoError(t, err, "Valid Plaid transaction should pass validation")
}

// TestTransaction_Validate_Success_Manual tests validation for manual transactions
func TestTransaction_Validate_Success_Manual(t *testing.T) {
	transaction := &Transaction{
		ID:                    "tx-uuid-2",
		UserID:                "user-uuid-1",
		AccountID:             "account-uuid-1",
		LinkedItemID:          "item-uuid-1",
		Source:                SourceManual,
		ExternalTransactionID: nil, // Manual transactions don't have external ID
		Amount:                -25.00,
		Date:                  time.Now(),
		Name:                  "Cash payment",
	}

	err := transaction.Validate()
	assert.NoError(t, err, "Valid manual transaction should pass validation")
}

// TestTransaction_Validate_MissingUserID tests that validation fails without user ID
func TestTransaction_Validate_MissingUserID(t *testing.T) {
	externalID := "plaid_tx_123"
	transaction := &Transaction{
		UserID:                "", // Missing
		AccountID:             "account-uuid-1",
		LinkedItemID:          "item-uuid-1",
		Source:                SourcePlaid,
		ExternalTransactionID: &externalID,
		Amount:                -50.25,
		Date:                  time.Now(),
		Name:                  "Test",
	}

	err := transaction.Validate()
	assert.Error(t, err, "Transaction without user_id should fail validation")
	assert.Contains(t, err.Error(), "user_id is required")
}

// TestTransaction_Validate_MissingSource tests that validation fails without source
func TestTransaction_Validate_MissingSource(t *testing.T) {
	externalID := "plaid_tx_123"
	transaction := &Transaction{
		UserID:                "user-uuid-1",
		AccountID:             "account-uuid-1",
		LinkedItemID:          "item-uuid-1",
		Source:                "", // Missing
		ExternalTransactionID: &externalID,
		Amount:                -50.25,
		Date:                  time.Now(),
		Name:                  "Test",
	}

	err := transaction.Validate()
	assert.Error(t, err, "Transaction without source should fail validation")
	assert.Contains(t, err.Error(), "source is required")
}

// TestTransaction_Validate_InvalidSource tests that validation fails with invalid source
func TestTransaction_Validate_InvalidSource(t *testing.T) {
	externalID := "invalid_tx_123"
	transaction := &Transaction{
		UserID:                "user-uuid-1",
		AccountID:             "account-uuid-1",
		LinkedItemID:          "item-uuid-1",
		Source:                "invalid_provider", // Invalid
		ExternalTransactionID: &externalID,
		Amount:                -50.25,
		Date:                  time.Now(),
		Name:                  "Test",
	}

	err := transaction.Validate()
	assert.Error(t, err, "Transaction with invalid source should fail validation")
	assert.Contains(t, err.Error(), "invalid transaction source")
}

// TestTransaction_Validate_ExternalIDRequiredForNonManual tests that non-manual
// transactions must have an external ID
func TestTransaction_Validate_ExternalIDRequiredForNonManual(t *testing.T) {
	transaction := &Transaction{
		UserID:                "user-uuid-1",
		AccountID:             "account-uuid-1",
		LinkedItemID:          "item-uuid-1",
		Source:                SourcePlaid,
		ExternalTransactionID: nil, // Missing for Plaid transaction
		Amount:                -50.25,
		Date:                  time.Now(),
		Name:                  "Test",
	}

	err := transaction.Validate()
	assert.Error(t, err, "Plaid transaction without external_transaction_id should fail validation")
	assert.Contains(t, err.Error(), "external_transaction_id required")
}

// TestTransaction_Validate_MissingName tests that validation fails without name
func TestTransaction_Validate_MissingName(t *testing.T) {
	externalID := "plaid_tx_123"
	transaction := &Transaction{
		UserID:                "user-uuid-1",
		AccountID:             "account-uuid-1",
		LinkedItemID:          "item-uuid-1",
		Source:                SourcePlaid,
		ExternalTransactionID: &externalID,
		Amount:                -50.25,
		Date:                  time.Now(),
		Name:                  "", // Missing
	}

	err := transaction.Validate()
	assert.Error(t, err, "Transaction without name should fail validation")
	assert.Contains(t, err.Error(), "name is required")
}

// TestTransaction_IsOutflow tests the IsOutflow helper (PFCv2: positive = outflow/spending).
func TestTransaction_IsOutflow(t *testing.T) {
	tests := []struct {
		name     string
		amount   float64
		want     bool
	}{
		{"positive amount is outflow", 25.50, true},
		{"zero is not outflow", 0, false},
		{"negative amount is not outflow", -25.50, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tx := &Transaction{Amount: tt.amount}
			assert.Equal(t, tt.want, tx.IsOutflow())
		})
	}
}

// TestTransaction_IsInflow tests the IsInflow helper (PFCv2: negative = inflow/refund).
func TestTransaction_IsInflow(t *testing.T) {
	tests := []struct {
		name   string
		amount float64
		want   bool
	}{
		{"negative amount is inflow", -25.50, true},
		{"zero is not inflow", 0, false},
		{"positive amount is not inflow", 25.50, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tx := &Transaction{Amount: tt.amount}
			assert.Equal(t, tt.want, tx.IsInflow())
		})
	}
}

// TestTransaction_ExpenseAmount tests the ExpenseAmount helper.
func TestTransaction_ExpenseAmount(t *testing.T) {
	tests := []struct {
		name   string
		amount float64
		want   float64
	}{
		{"outflow returns positive amount", 25.50, 25.50},
		{"zero returns zero", 0, 0},
		{"inflow returns zero", -25.50, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tx := &Transaction{Amount: tt.amount}
			assert.Equal(t, tt.want, tx.ExpenseAmount())
		})
	}
}

// TestTransaction_IncomeAmount tests the IncomeAmount helper.
func TestTransaction_IncomeAmount(t *testing.T) {
	tests := []struct {
		name   string
		amount float64
		want   float64
	}{
		{"inflow returns absolute amount", -25.50, 25.50},
		{"zero returns zero", 0, 0},
		{"outflow returns zero", 25.50, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tx := &Transaction{Amount: tt.amount}
			assert.Equal(t, tt.want, tx.IncomeAmount())
		})
	}
}

// TestTransaction_Validate_AllSources tests validation for all supported sources
func TestTransaction_Validate_AllSources(t *testing.T) {
	sources := []TransactionSource{
		SourcePlaid,
		SourceTeller,
		SourceFinicity,
		SourceYodlee,
	}

	for _, source := range sources {
		t.Run(string(source), func(t *testing.T) {
			externalID := string(source) + "_tx_123"
			transaction := &Transaction{
				UserID:                "user-uuid-1",
				AccountID:             "account-uuid-1",
				LinkedItemID:          "item-uuid-1",
				Source:                source,
				ExternalTransactionID: &externalID,
				Amount:                -50.25,
				Date:                  time.Now(),
				Name:                  "Test Transaction",
			}

			err := transaction.Validate()
			assert.NoError(t, err, "Valid %s transaction should pass validation", source)
		})
	}
}

// Issue #1569: Reversed() is the single predicate the API and every renderer branch on, so the
// asymmetry between the two tombstone reasons is pinned here rather than left to each call site.
func TestTransaction_Reversed(t *testing.T) {
	now := time.Now()
	posted := TxnDeletedProviderRemovedPosted
	pending := TxnDeletedProviderRemovedPending

	t.Run("live row is not reversed", func(t *testing.T) {
		tx := &Transaction{}
		assert.False(t, tx.Reversed())
	})

	t.Run("posted removal is reversed", func(t *testing.T) {
		tx := &Transaction{DeletedAt: &now, DeletedReason: &posted}
		assert.True(t, tx.Reversed())
	})

	t.Run("settled pending is NOT reversed", func(t *testing.T) {
		tx := &Transaction{DeletedAt: &now, DeletedReason: &pending}
		assert.False(t, tx.Reversed(),
			"a row removed while pending has a posted twin already in the table; calling it a "+
				"reversal would double-count the same money")
	})

	t.Run("reason without timestamp is not reversed", func(t *testing.T) {
		// The database CHECK (transactions_tombstone_consistency) makes this state unreachable in
		// storage, but Reversed() is also called on structs assembled in memory, and defaulting to
		// "reversed" for a half-populated struct would surface rows nothing decided to surface.
		tx := &Transaction{DeletedReason: &posted}
		assert.False(t, tx.Reversed())
	})
}

// TestNewBankingActivityItem_CarriesReversal: the transactions list endpoint returns ActivityItem,
// not Transaction, so a reversal that is correct in the domain but dropped in the wrapper is
// invisible to every client.
func TestNewBankingActivityItem_CarriesReversal(t *testing.T) {
	now := time.Now()
	posted := TxnDeletedProviderRemovedPosted

	item := NewBankingActivityItem(&Transaction{
		ID: "t1", Date: now, DeletedAt: &now, DeletedReason: &posted,
	})
	require.NotNil(t, item.Reversed)
	assert.True(t, *item.Reversed)
	require.NotNil(t, item.ReversedAt)
	assert.Equal(t, now, *item.ReversedAt)

	live := NewBankingActivityItem(&Transaction{ID: "t2", Date: now})
	require.NotNil(t, live.Reversed)
	assert.False(t, *live.Reversed, "reversed must be present-and-false on live rows, not absent")
	assert.Nil(t, live.ReversedAt)
}
