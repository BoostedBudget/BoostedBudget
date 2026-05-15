package domain

import (
	"errors"
	"time"
)

// Security represents a financial security (stock, ETF, mutual fund, etc.)
// Securities are shared across users — not user-scoped.
type Security struct {
	ID              string  `json:"id" db:"id"`
	PlaidSecurityID string  `json:"plaid_security_id" db:"plaid_security_id"`
	TickerSymbol    *string `json:"ticker_symbol,omitempty" db:"ticker_symbol"`
	Name            string  `json:"name" db:"name"`
	Type            *string `json:"type,omitempty" db:"type"`
	ISIN            *string `json:"isin,omitempty" db:"isin"`
	CUSIP           *string `json:"cusip,omitempty" db:"cusip"`
	IsCashEquivalent *bool  `json:"is_cash_equivalent,omitempty" db:"is_cash_equivalent"`

	// Institutional pricing (overnight updates, not real-time)
	InstitutionPrice     *float64   `json:"institution_price,omitempty" db:"institution_price"`
	InstitutionPriceAsOf *time.Time `json:"institution_price_as_of,omitempty" db:"institution_price_as_of"`
	ClosePrice           *float64   `json:"close_price,omitempty" db:"close_price"`
	ClosePriceAsOf       *string    `json:"close_price_as_of,omitempty" db:"close_price_as_of"`

	CreatedAt time.Time `json:"created_at" db:"created_at"`
	UpdatedAt time.Time `json:"updated_at" db:"updated_at"`
}

// Validate checks that the Security has all required fields.
func (s *Security) Validate() error {
	if s.PlaidSecurityID == "" {
		return errors.New("plaid_security_id is required")
	}
	if s.Name == "" {
		return errors.New("name is required")
	}
	return nil
}

// Holding represents a user's current position in a security within an account.
// Holdings are replaced in full on each sync (no delta/cursor mechanism).
type Holding struct {
	ID               string   `json:"id" db:"id"`
	UserID           string   `json:"user_id" db:"user_id"`
	AccountID        string   `json:"account_id" db:"account_id"`
	UserAccountID    *string  `json:"user_account_id,omitempty" db:"user_account_id"` // Stable reference across Plaid re-links (Issue #1109)
	SecurityID       string   `json:"security_id" db:"security_id"`
	PlaidSecurityID  string   `json:"plaid_security_id,omitempty" db:"-"` // transient: used during sync to resolve SecurityID
	Quantity         float64  `json:"quantity" db:"quantity"`
	CostBasis        *float64 `json:"cost_basis,omitempty" db:"cost_basis"`
	InstitutionValue *float64 `json:"institution_value,omitempty" db:"institution_value"`
	InstitutionPrice *float64 `json:"institution_price,omitempty" db:"institution_price"`

	CreatedAt time.Time `json:"created_at" db:"created_at"`
	UpdatedAt time.Time `json:"updated_at" db:"updated_at"`

	// Populated on demand for API responses
	Security *Security `json:"security,omitempty" db:"-"`
}

// Validate checks that the Holding has all required fields.
func (h *Holding) Validate() error {
	if h.UserID == "" {
		return errors.New("user_id is required")
	}
	if h.AccountID == "" {
		return errors.New("account_id is required")
	}
	if h.SecurityID == "" {
		return errors.New("security_id is required")
	}
	return nil
}

// InvestmentTransaction represents a trade, dividend, fee, or other investment activity.
// Investment transactions are immutable once posted by Plaid.
type InvestmentTransaction struct {
	ID                           string   `json:"id" db:"id"`
	UserID                       string   `json:"user_id" db:"user_id"`
	AccountID                    string   `json:"account_id" db:"account_id"`
	UserAccountID                *string  `json:"user_account_id,omitempty" db:"user_account_id"` // Stable reference across Plaid re-links (Issue #1109)
	SecurityID                   *string  `json:"security_id,omitempty" db:"security_id"` // NULL for cash transactions
	PlaidSecurityID              string   `json:"plaid_security_id,omitempty" db:"-"`     // transient: used during sync
	PlaidInvestmentTransactionID string   `json:"plaid_investment_transaction_id" db:"plaid_investment_transaction_id"`
	Type                         string   `json:"type" db:"type"` // buy, sell, dividend, cash, fee, transfer, etc.
	Subtype                      *string  `json:"subtype,omitempty" db:"subtype"`
	Name                         string   `json:"name" db:"name"`
	Quantity                     *float64 `json:"quantity,omitempty" db:"quantity"`
	Amount                       float64  `json:"amount" db:"amount"`
	Price                        *float64 `json:"price,omitempty" db:"price"`
	Fees                         *float64 `json:"fees,omitempty" db:"fees"`
	Date                         time.Time `json:"date" db:"date"`

	CreatedAt time.Time `json:"created_at" db:"created_at"`
}

// Validate checks that the InvestmentTransaction has all required fields.
func (t *InvestmentTransaction) Validate() error {
	if t.UserID == "" {
		return errors.New("user_id is required")
	}
	if t.AccountID == "" {
		return errors.New("account_id is required")
	}
	if t.PlaidInvestmentTransactionID == "" {
		return errors.New("plaid_investment_transaction_id is required")
	}
	if t.Type == "" {
		return errors.New("type is required")
	}
	if t.Name == "" {
		return errors.New("name is required")
	}
	if t.Date.IsZero() {
		return errors.New("date is required")
	}
	return nil
}

// HoldingSyncResult contains the results of a holdings sync operation.
type HoldingSyncResult struct {
	SecuritiesUpserted int
	HoldingsReplaced   int
	AccountsProcessed  int
	Errors             []string // accounts skipped due to unmapped securities
}

// InvestmentTransactionSyncResult contains the results of an investment transaction sync.
type InvestmentTransactionSyncResult struct {
	SecuritiesUpserted   int
	TransactionsInserted int
	TransactionsSkipped  int // duplicates already in DB
	PagesProcessed       int
}

// InvestmentTransactionFilters holds optional filters for querying investment transactions.
type InvestmentTransactionFilters struct {
	AccountID  string
	SecurityID string
	Type       string
	StartDate  *time.Time
	EndDate    *time.Time
}
