package domain

import (
	"errors"
	"time"
)

// Security represents a financial security (stock, ETF, mutual fund, etc.)
// Securities are shared across users — not user-scoped.
//
// ⚠ ClosePriceAsOf is a *time.Time because securities.close_price_as_of is a DATE column
// (migration 000051). It was a *string until #1631, which made every read of it fail with
// "cannot scan date (OID 1082) in binary format into **string" — a hard 500 on
// /investments/portfolio/summary and /investments/holdings for any user holding a security Plaid
// had priced. pgx RESCUES the NULL case, so the bug was invisible to every fixture that left the
// column empty; only a non-null value reaches the failing scan plan. Pinned by
// holding_close_price_as_of_integration_test.go, whose two tests exist to document exactly that
// asymmetry. Do not "simplify" this back to a string, and do not paper over it with a ::text cast:
// the value is what tells DayChange how stale its close price is.
type Security struct {
	ID               string  `json:"id" db:"id"`
	PlaidSecurityID  string  `json:"plaid_security_id" db:"plaid_security_id"`
	TickerSymbol     *string `json:"ticker_symbol,omitempty" db:"ticker_symbol"`
	Name             string  `json:"name" db:"name"`
	Type             *string `json:"type,omitempty" db:"type"`
	ISIN             *string `json:"isin,omitempty" db:"isin"`
	CUSIP            *string `json:"cusip,omitempty" db:"cusip"`
	IsCashEquivalent *bool   `json:"is_cash_equivalent,omitempty" db:"is_cash_equivalent"`

	// Institutional pricing (overnight updates, not real-time)
	InstitutionPrice     *float64   `json:"institution_price,omitempty" db:"institution_price"`
	InstitutionPriceAsOf *time.Time `json:"institution_price_as_of,omitempty" db:"institution_price_as_of"`
	ClosePrice           *float64   `json:"close_price,omitempty" db:"close_price"`
	ClosePriceAsOf       *time.Time `json:"close_price_as_of,omitempty" db:"close_price_as_of"`

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

// GainLoss returns the unrealized gain/loss (institution_value - cost_basis),
// or nil when either value is absent. Realized gain/loss is not supported —
// Plaid does not provide per-lot cost basis and the 2-year lookback truncates
// buy history a FIFO replay would need.
func (h *Holding) GainLoss() *float64 {
	if h.InstitutionValue == nil || h.CostBasis == nil {
		return nil
	}
	v := *h.InstitutionValue - *h.CostBasis
	return &v
}

// GainLossPct returns the unrealized gain/loss as a percentage of cost basis,
// or nil when cost basis is absent or zero.
func (h *Holding) GainLossPct() *float64 {
	if h.InstitutionValue == nil || h.CostBasis == nil || *h.CostBasis == 0 {
		return nil
	}
	v := (*h.InstitutionValue - *h.CostBasis) / *h.CostBasis * 100
	return &v
}

// DayChange returns quantity × (holdings.institution_price − securities.close_price),
// or nil when either price is absent. IMPORTANT: always use holdings.institution_price
// (current intraday price, always synced) vs securities.close_price (prior close).
// securities.institution_price is a dead column — always NULL from Plaid — never use it.
func (h *Holding) DayChange() *float64 {
	if h.InstitutionPrice == nil {
		return nil
	}
	if h.Security == nil || h.Security.ClosePrice == nil {
		return nil
	}
	v := h.Quantity * (*h.InstitutionPrice - *h.Security.ClosePrice)
	return &v
}

// DayChangePct returns the day change as a percentage of the PRIOR CLOSE — i.e.
// (current - prior) / prior * 100 — or nil when the prior value cannot anchor it.
//
// ⚠ #1631: this divided by institution_value (the CURRENT value) until now, which answers a
// different question and made every day-change percentage in the app wrong: it understated gains
// and overstated losses by exactly the day change. A true +5% day (prior 100 → current 105)
// reported 5/105 = 4.76%, and a doubling reported 50% instead of 100%. The error grows with the
// size of the move, which is the opposite of what you want from a volatility figure.
//
// The prior value is quantity × close_price, deliberately recomputed here rather than taken from
// institution_value: the two differ by the day change, and that difference IS the bug.
func (h *Holding) DayChangePct() *float64 {
	dc := h.DayChange()
	if dc == nil {
		return nil
	}
	// DayChange() has already established both are non-nil.
	priorValue := h.Quantity * *h.Security.ClosePrice
	if priorValue == 0 {
		return nil
	}
	v := *dc / priorValue * 100
	return &v
}

// InvestmentTransaction represents a trade, dividend, fee, or other investment activity.
// Investment transactions are immutable once posted by Plaid.
type InvestmentTransaction struct {
	ID              string  `json:"id" db:"id"`
	UserID          string  `json:"user_id" db:"user_id"`
	AccountID       string  `json:"account_id" db:"account_id"`
	UserAccountID   *string `json:"user_account_id,omitempty" db:"user_account_id"` // Stable reference across Plaid re-links (Issue #1109)
	SecurityID      *string `json:"security_id,omitempty" db:"security_id"`         // NULL for cash transactions
	PlaidSecurityID string  `json:"plaid_security_id,omitempty" db:"-"`             // transient: used during sync only
	TickerSymbol    *string `json:"ticker_symbol,omitempty" db:"-"`                 // from LEFT JOIN securities in GetByUserIDKeyset; nil for cash
	SecurityName    *string `json:"security_name,omitempty" db:"-"`                 // from LEFT JOIN securities in GetByUserIDKeyset; nil for cash
	// ActivityType is the NORMALIZED display bucket (buy/sell/dividend/reinvest/interest/fee/
	// transfer/cash/other), derived at read time by domain.NormalizeTradeType. Transient like the
	// two fields above. Added by #1631: the investments page was passing the RAW Plaid `type` to
	// the same chip the transactions feed feeds normalized, so the two screens disagreed about the
	// same row and the investments page could never show Dividend or Reinvest at all.
	ActivityType                 *string   `json:"activity_type,omitempty" db:"-"`
	PlaidInvestmentTransactionID string    `json:"plaid_investment_transaction_id" db:"plaid_investment_transaction_id"`
	Type                         string    `json:"type" db:"type"` // buy, sell, dividend, cash, fee, transfer, etc.
	Subtype                      *string   `json:"subtype,omitempty" db:"subtype"`
	Name                         string    `json:"name" db:"name"`
	Quantity                     *float64  `json:"quantity,omitempty" db:"quantity"`
	Amount                       float64   `json:"amount" db:"amount"`
	Price                        *float64  `json:"price,omitempty" db:"price"`
	Fees                         *float64  `json:"fees,omitempty" db:"fees"`
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
	// Errors records accounts skipped due to unmapped securities — a real per-sync correctness
	// problem, not a stable state. This field gates the portfolio snapshot upsert
	// (investment_sync_service.go SyncHoldings): a non-empty Errors means the sync could not
	// safely produce a full-user aggregate this run, so the snapshot must wait for a clean sync.
	Errors []string
	// SkippedIncomplete records accounts whose holdings clear was skipped because the account was
	// absent from Plaid's response (Issue #1569 Task 4 — partial/incomplete response, not a
	// confirmed liquidation). This is DELIBERATELY a separate field from Errors: an account that
	// Plaid has permanently stopped returning (e.g. closed — Plaid does not re-report closed
	// accounts) would otherwise sit in Errors forever, and Errors gates the portfolio snapshot.
	// That would suppress the user's snapshot indefinitely for a stable, expected state, even
	// though the preserved holdings are exactly the rows the snapshot should be summing.
	// SkippedIncomplete still drives ProcessWebhook's retry decision (so a transient partial
	// response gets retried) without touching the snapshot gate.
	SkippedIncomplete []string
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
	// AccountID filters to a specific account (chain-expanded via user_account_id).
	AccountID  *string
	SecurityID string
	// ActivityTypes filters by normalized trade type
	// (buy/sell/dividend/reinvest/interest/fee/transfer/cash/other).
	// Applied as a SQL WHERE via domain.TradeTypeToSQLPredicate; never post-fetch.
	ActivityTypes []string
	StartDate     *time.Time
	EndDate       *time.Time
}

// maxClosePriceStalenessDays bounds how old securities.close_price_as_of may be before the figure
// derived from it stops describing a "day".
//
// Measured in CALENDAR DAYS, not as a duration, because close_price_as_of is a DATE column: a
// duration comparison makes the answer depend on the time of day the question is asked, so the
// same price would be fresh at 09:00 and stale at 14:00.
//
// Four days, not one: Plaid's close price is an overnight update, so on a Monday the newest
// available close is Friday's (3 days), and a Monday public holiday pushes it to Thursday's (4).
// This is a floor on usefulness rather than a trading-calendar rule — the repo has no market
// calendar, and inventing one here would be worse than a deliberately generous bound.
const maxClosePriceStalenessDays = 4

// ClosePriceIsStale reports whether the prior close backing DayChange is too old to describe a day.
//
// ⚠ #1631: securities.close_price_as_of was WRITE-ONLY — nothing read it, so nothing could tell.
// That matters more than it sounds, because the price-retention logic in
// security_repository.UpsertBatch deliberately keeps the last known close_price alive indefinitely
// when a payload omits one (#1569 T20). Unbounded staleness is therefore the NORMAL case rather
// than an edge case, and a week-old close still produced a number labelled "day change".
//
// Returns false when the timestamp is absent: an undated price is a separate problem from a stale
// one, and refusing to answer would suppress the figure for every security Plaid dates loosely.
// Callers that need certainty should check ClosePriceAsOf themselves.
func (h *Holding) ClosePriceIsStale(now time.Time) bool {
	if h.Security == nil || h.Security.ClosePriceAsOf == nil {
		return false
	}
	asOf := *h.Security.ClosePriceAsOf
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	closeDay := time.Date(asOf.Year(), asOf.Month(), asOf.Day(), 0, 0, 0, 0, time.UTC)
	days := int(today.Sub(closeDay).Hours() / 24)
	return days > maxClosePriceStalenessDays
}
