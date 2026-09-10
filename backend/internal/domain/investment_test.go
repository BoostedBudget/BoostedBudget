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

// --- Holding compute methods ---

func TestHolding_GainLoss(t *testing.T) {
	tests := []struct {
		name    string
		holding Holding
		wantNil bool
		wantVal float64
	}{
		{
			name: "returns gain when both values present",
			holding: Holding{
				InstitutionValue: float64Ptr(1750.00),
				CostBasis:        float64Ptr(1500.00),
			},
			wantNil: false,
			wantVal: 250.00,
		},
		{
			name: "returns loss when institution_value < cost_basis",
			holding: Holding{
				InstitutionValue: float64Ptr(900.00),
				CostBasis:        float64Ptr(1000.00),
			},
			wantNil: false,
			wantVal: -100.00,
		},
		{
			name: "returns nil when cost_basis is nil",
			holding: Holding{
				InstitutionValue: float64Ptr(1750.00),
				CostBasis:        nil,
			},
			wantNil: true,
		},
		{
			name: "returns nil when institution_value is nil",
			holding: Holding{
				InstitutionValue: nil,
				CostBasis:        float64Ptr(1500.00),
			},
			wantNil: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.holding.GainLoss()
			if tt.wantNil {
				assert.Nil(t, got)
			} else {
				assert.NotNil(t, got)
				assert.InDelta(t, tt.wantVal, *got, 0.001)
			}
		})
	}
}

func TestHolding_GainLossPct(t *testing.T) {
	tests := []struct {
		name    string
		holding Holding
		wantNil bool
		wantVal float64
	}{
		{
			name: "returns percentage when both values present",
			holding: Holding{
				InstitutionValue: float64Ptr(1750.00),
				CostBasis:        float64Ptr(1400.00),
			},
			wantNil: false,
			wantVal: 25.0, // (350/1400)*100
		},
		{
			name: "returns nil when cost_basis is nil",
			holding: Holding{
				InstitutionValue: float64Ptr(1750.00),
				CostBasis:        nil,
			},
			wantNil: true,
		},
		{
			name: "returns nil when cost_basis is zero (avoid div-by-zero)",
			holding: Holding{
				InstitutionValue: float64Ptr(1750.00),
				CostBasis:        float64Ptr(0.00),
			},
			wantNil: true,
		},
		{
			name: "returns nil when institution_value is nil",
			holding: Holding{
				InstitutionValue: nil,
				CostBasis:        float64Ptr(1500.00),
			},
			wantNil: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.holding.GainLossPct()
			if tt.wantNil {
				assert.Nil(t, got)
			} else {
				assert.NotNil(t, got)
				assert.InDelta(t, tt.wantVal, *got, 0.001)
			}
		})
	}
}

func TestHolding_DayChange(t *testing.T) {
	tests := []struct {
		name     string
		holding  Holding
		security *Security
		wantNil  bool
		wantVal  float64
	}{
		{
			name: "returns day change when all prices present",
			holding: Holding{
				Quantity:         10.0,
				InstitutionPrice: float64Ptr(175.50), // current price (from holdings)
				Security: &Security{
					ClosePrice: float64Ptr(173.00), // prior close (from securities)
				},
			},
			wantNil: false,
			wantVal: 25.00, // 10 * (175.50 - 173.00)
		},
		{
			name: "returns nil when institution_price is nil",
			holding: Holding{
				Quantity:         10.0,
				InstitutionPrice: nil,
				Security: &Security{
					ClosePrice: float64Ptr(173.00),
				},
			},
			wantNil: true,
		},
		{
			name: "returns nil when security close_price is nil (private/illiquid)",
			holding: Holding{
				Quantity:         10.0,
				InstitutionPrice: float64Ptr(175.50),
				Security: &Security{
					ClosePrice: nil,
				},
			},
			wantNil: true,
		},
		{
			name:    "returns nil when Security is nil",
			holding: Holding{Quantity: 10.0, InstitutionPrice: float64Ptr(175.50)},
			wantNil: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.security != nil {
				tt.holding.Security = tt.security
			}
			got := tt.holding.DayChange()
			if tt.wantNil {
				assert.Nil(t, got)
			} else {
				assert.NotNil(t, got)
				assert.InDelta(t, tt.wantVal, *got, 0.001)
			}
		})
	}
}

func TestHolding_DayChangePct(t *testing.T) {
	tests := []struct {
		name    string
		holding Holding
		wantNil bool
		wantVal float64
	}{
		{
			name: "returns day change pct",
			holding: Holding{
				Quantity:         10.0,
				InstitutionPrice: float64Ptr(175.50),
				InstitutionValue: float64Ptr(1755.00),
				Security: &Security{
					ClosePrice: float64Ptr(170.00),
				},
			},
			wantNil: false,
			// #1631: the denominator is the PRIOR close value (quantity x close_price), not the
			// current institution_value. The two differ by the day change itself, which is what
			// made every percentage in the app wrong.
			wantVal: (10 * (175.50 - 170.00)) / (10 * 170.00) * 100,
		},
		{
			// #1631 changed this from wantNil: institution_value is no longer the denominator, so
			// its absence no longer prevents the calculation. DayChange only needs the price and
			// the prior close, both present here.
			name: "computes without institution_value, which is no longer the denominator",
			holding: Holding{
				Quantity:         10.0,
				InstitutionPrice: float64Ptr(175.50),
				InstitutionValue: nil,
				Security:         &Security{ClosePrice: float64Ptr(170.00)},
			},
			wantNil: false,
			wantVal: (10 * (175.50 - 170.00)) / (10 * 170.00) * 100,
		},
		{
			// Likewise: a zero CURRENT value against a non-zero prior close is a real -100% day,
			// not an undefined one. Only a zero PRIOR value is undefined, and that case is covered
			// in investment_daychange_test.go.
			name: "a zero institution_value no longer suppresses the percentage",
			holding: Holding{
				Quantity:         10.0,
				InstitutionPrice: float64Ptr(175.50),
				InstitutionValue: float64Ptr(0.0),
				Security:         &Security{ClosePrice: float64Ptr(170.00)},
			},
			wantNil: false,
			wantVal: (10 * (175.50 - 170.00)) / (10 * 170.00) * 100,
		},
		{
			name: "returns nil when DayChange is nil",
			holding: Holding{
				Quantity:         10.0,
				InstitutionPrice: nil,
				InstitutionValue: float64Ptr(1755.00),
				Security:         &Security{ClosePrice: float64Ptr(170.00)},
			},
			wantNil: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.holding.DayChangePct()
			if tt.wantNil {
				assert.Nil(t, got)
			} else {
				assert.NotNil(t, got)
				assert.InDelta(t, tt.wantVal, *got, 0.001)
			}
		})
	}
}
