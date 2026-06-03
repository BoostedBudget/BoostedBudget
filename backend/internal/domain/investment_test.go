package domain

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// TestSecurity_Validate tests Security validation
func TestSecurity_Validate(t *testing.T) {
	tests := []struct {
		name    string
		sec     Security
		wantErr bool
		errMsg  string
	}{
		{
			name: "valid security with all fields",
			sec: Security{
				PlaidSecurityID: "sec-123",
				Name:            "Apple Inc.",
				TickerSymbol:    strPtr("AAPL"),
				Type:            strPtr("equity"),
			},
			wantErr: false,
		},
		{
			name: "valid security minimal fields",
			sec: Security{
				PlaidSecurityID: "sec-456",
				Name:            "US Dollar",
			},
			wantErr: false,
		},
		{
			name: "missing plaid_security_id",
			sec: Security{
				Name: "Apple Inc.",
			},
			wantErr: true,
			errMsg:  "plaid_security_id is required",
		},
		{
			name: "missing name",
			sec: Security{
				PlaidSecurityID: "sec-123",
			},
			wantErr: true,
			errMsg:  "name is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.sec.Validate()
			if tt.wantErr {
				assert.Error(t, err)
				assert.EqualError(t, err, tt.errMsg)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

// TestHolding_Validate tests Holding validation
func TestHolding_Validate(t *testing.T) {
	tests := []struct {
		name    string
		holding Holding
		wantErr bool
		errMsg  string
	}{
		{
			name: "valid holding",
			holding: Holding{
				UserID:     "user-123",
				AccountID:  "account-456",
				SecurityID: "security-789",
				Quantity:   10.5,
			},
			wantErr: false,
		},
		{
			name: "valid holding with all optional fields",
			holding: Holding{
				UserID:           "user-123",
				AccountID:        "account-456",
				SecurityID:       "security-789",
				Quantity:         10.5,
				CostBasis:        float64Ptr(1500.00),
				InstitutionValue: float64Ptr(1750.00),
				InstitutionPrice: float64Ptr(166.67),
			},
			wantErr: false,
		},
		{
			name: "missing user_id",
			holding: Holding{
				AccountID:  "account-456",
				SecurityID: "security-789",
				Quantity:   10.5,
			},
			wantErr: true,
			errMsg:  "user_id is required",
		},
		{
			name: "missing account_id",
			holding: Holding{
				UserID:     "user-123",
				SecurityID: "security-789",
				Quantity:   10.5,
			},
			wantErr: true,
			errMsg:  "account_id is required",
		},
		{
			name: "missing security_id",
			holding: Holding{
				UserID:    "user-123",
				AccountID: "account-456",
				Quantity:  10.5,
			},
			wantErr: true,
			errMsg:  "security_id is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.holding.Validate()
			if tt.wantErr {
				assert.Error(t, err)
				assert.EqualError(t, err, tt.errMsg)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

// TestInvestmentTransaction_Validate tests InvestmentTransaction validation
func TestInvestmentTransaction_Validate(t *testing.T) {
	validDate := time.Date(2024, 1, 15, 0, 0, 0, 0, time.UTC)
	tests := []struct {
		name    string
		txn     InvestmentTransaction
		wantErr bool
		errMsg  string
	}{
		{
			name: "valid buy transaction",
			txn: InvestmentTransaction{
				UserID:                       "user-123",
				AccountID:                    "account-456",
				SecurityID:                   strPtr("security-789"),
				PlaidInvestmentTransactionID: "plaid-txn-001",
				Type:                         "buy",
				Name:                         "Apple Inc.",
				Amount:                       1500.00,
				Date:                         validDate,
			},
			wantErr: false,
		},
		{
			name: "valid cash transaction with nil security_id",
			txn: InvestmentTransaction{
				UserID:                       "user-123",
				AccountID:                    "account-456",
				SecurityID:                   nil, // cash transaction has no security
				PlaidInvestmentTransactionID: "plaid-txn-002",
				Type:                         "cash",
				Name:                         "Cash Deposit",
				Amount:                       500.00,
				Date:                         validDate,
			},
			wantErr: false,
		},
		{
			name: "missing user_id",
			txn: InvestmentTransaction{
				AccountID:                    "account-456",
				PlaidInvestmentTransactionID: "plaid-txn-001",
				Type:                         "buy",
				Name:                         "Apple Inc.",
				Amount:                       1500.00,
				Date:                         validDate,
			},
			wantErr: true,
			errMsg:  "user_id is required",
		},
		{
			name: "missing account_id",
			txn: InvestmentTransaction{
				UserID:                       "user-123",
				PlaidInvestmentTransactionID: "plaid-txn-001",
				Type:                         "buy",
				Name:                         "Apple Inc.",
				Amount:                       1500.00,
				Date:                         validDate,
			},
			wantErr: true,
			errMsg:  "account_id is required",
		},
		{
			name: "missing plaid_investment_transaction_id",
			txn: InvestmentTransaction{
				UserID:    "user-123",
				AccountID: "account-456",
				Type:      "buy",
				Name:      "Apple Inc.",
				Amount:    1500.00,
				Date:      validDate,
			},
			wantErr: true,
			errMsg:  "plaid_investment_transaction_id is required",
		},
		{
			name: "missing type",
			txn: InvestmentTransaction{
				UserID:                       "user-123",
				AccountID:                    "account-456",
				PlaidInvestmentTransactionID: "plaid-txn-001",
				Name:                         "Apple Inc.",
				Amount:                       1500.00,
				Date:                         validDate,
			},
			wantErr: true,
			errMsg:  "type is required",
		},
		{
			name: "missing name",
			txn: InvestmentTransaction{
				UserID:                       "user-123",
				AccountID:                    "account-456",
				PlaidInvestmentTransactionID: "plaid-txn-001",
				Type:                         "buy",
				Amount:                       1500.00,
				Date:                         validDate,
			},
			wantErr: true,
			errMsg:  "name is required",
		},
		{
			name: "zero date",
			txn: InvestmentTransaction{
				UserID:                       "user-123",
				AccountID:                    "account-456",
				PlaidInvestmentTransactionID: "plaid-txn-001",
				Type:                         "buy",
				Name:                         "Apple Inc.",
				Amount:                       1500.00,
				Date:                         time.Time{},
			},
			wantErr: true,
			errMsg:  "date is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.txn.Validate()
			if tt.wantErr {
				assert.Error(t, err)
				assert.EqualError(t, err, tt.errMsg)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

// TestHoldingSyncResult_Counts verifies result struct is correctly populated
func TestHoldingSyncResult_Counts(t *testing.T) {
	result := HoldingSyncResult{
		SecuritiesUpserted: 5,
		HoldingsReplaced:   12,
		AccountsProcessed:  2,
	}
	assert.Equal(t, 5, result.SecuritiesUpserted)
	assert.Equal(t, 12, result.HoldingsReplaced)
	assert.Equal(t, 2, result.AccountsProcessed)
}

// TestInvestmentTransactionSyncResult_Counts verifies result struct
func TestInvestmentTransactionSyncResult_Counts(t *testing.T) {
	result := InvestmentTransactionSyncResult{
		SecuritiesUpserted:   3,
		TransactionsInserted: 25,
		TransactionsSkipped:  5,
		PagesProcessed:       2,
	}
	assert.Equal(t, 3, result.SecuritiesUpserted)
	assert.Equal(t, 25, result.TransactionsInserted)
	assert.Equal(t, 5, result.TransactionsSkipped)
	assert.Equal(t, 2, result.PagesProcessed)
}

