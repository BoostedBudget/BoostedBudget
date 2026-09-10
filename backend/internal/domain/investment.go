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
	ID string `json:"id" db:"id"`
	// PlaidSecurityID is Plaid's own dedup key, and the ONLY column our unique index
	// (`unique_plaid_security_id`, migration 000051) and ON CONFLICT target key on. Plaid's docs
	// say it "does not typically change, but may change if inherent details of the security change
	// due to a corporate action, for example, in the event of a ticker symbol change or CUSIP
	// change", and is only "typically" (not guaranteed) the same across institutions. So a
	// corporate action can silently fork one real-world security into two `securities` rows, each
	// with its own purchase/sale/dividend history hanging off `investment_transactions.security_id`
	// — the exact shape observed in production for AGG/ISHARES.CORE.U.S.AGGREGATE and
	// IEFA/IEMG/VCIT. Not fixed here: #1631 stores InstitutionSecurityID below so a future merge
	// has a key to join forked identities on, but does NOT rewrite `security_id` on existing
	// transactions — that is out of scope (retention ADR forbids merging history as a side effect
	// of a display fix).
	PlaidSecurityID string  `json:"plaid_security_id" db:"plaid_security_id"`
	TickerSymbol    *string `json:"ticker_symbol,omitempty" db:"ticker_symbol"`
	Name            string  `json:"name" db:"name"`
	Type            *string `json:"type,omitempty" db:"type"`
	// ISIN and CUSIP: Plaid nulls both by default for new customers, and for ALL customers after
	// 2024-03-12, unless a CUSIP licence has been separately verified with Plaid. That is why every
	// one of the 34 securities in production has NULL cusip/isin — it is not a sync defect, and no
	// dedupe/consolidation design may key on either column without first completing that
	// commercial licensing step.
	ISIN             *string `json:"isin,omitempty" db:"isin"`
	CUSIP            *string `json:"cusip,omitempty" db:"cusip"`
	IsCashEquivalent *bool   `json:"is_cash_equivalent,omitempty" db:"is_cash_equivalent"`
	// InstitutionSecurityID is "the identifier given to the security by the institution" (Plaid
	// docs) — a second, institution-scoped identity for the same security, independent of
	// PlaidSecurityID's corporate-action fragility above. Stored so a future consolidation of
	// forked security identities has something to join on; #1631 stores and exposes it only —
	// merging is explicitly out of scope.
	InstitutionSecurityID *string `json:"institution_security_id,omitempty" db:"institution_security_id"`
	// ProxySecurityID is "the ID of another security whose performance resembles this security,
	// typically when the original security has low volume, or when a private security can be
	// modeled with a publicly traded security" (Plaid docs). Stored for future use; nothing reads
	// it yet.
	ProxySecurityID *string `json:"proxy_security_id,omitempty" db:"proxy_security_id"`
	// CurrencyCode is the currency of this row's PRICE columns — close_price above all — from
	// Plaid's iso_currency_code, falling back to unofficial_currency_code (#1631 M8a). NULL means
	// unknown: Plaid sent neither, or the row predates the mapper that reads them.
	//
	// ⚠ This is NOT interchangeable with Holding.CurrencyCode, and the difference is the whole
	// reason both exist. Plaid documents close_price as being "priced in USD" when the security
	// is a foreign currency, while a holding's institution_price is "the last price given by the
	// institution" — so for such a security the two are quoted in different units, and
	// subtracting them (which is what a day change does) is a unit error rather than a small
	// inaccuracy. See domain/investment_currency.go for how that is handled.
	CurrencyCode *string `json:"currency_code,omitempty" db:"currency_code"`

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
	// CurrencyCode is the currency of CostBasis, InstitutionValue and InstitutionPrice on this
	// row, from Plaid's iso_currency_code falling back to unofficial_currency_code (#1631 M8a).
	// NULL means unknown, which is the state of every row written before that change.
	//
	// Not summable across differing values: there is no FX rate source in this system, so an
	// aggregate over a mixed set is declared as such rather than converted. See
	// domain/investment_currency.go.
	CurrencyCode *string `json:"currency_code,omitempty" db:"currency_code"`

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
	// #1631 M8a: refuse the subtraction outright when the two operands are quoted in different
	// currencies. Plaid prices a foreign-currency security's close_price in USD while the
	// institution reports the position in its own currency, so this is a unit error and its
	// result was being rendered as money. There is no FX rate source here to convert with, and
	// an invented rate would produce a number nobody could tell from a correct one.
	if h.HasCurrencyMismatch() {
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
	ActivityType                 *string  `json:"activity_type,omitempty" db:"-"`
	PlaidInvestmentTransactionID string   `json:"plaid_investment_transaction_id" db:"plaid_investment_transaction_id"`
	Type                         string   `json:"type" db:"type"` // buy, sell, dividend, cash, fee, transfer, etc.
	Subtype                      *string  `json:"subtype,omitempty" db:"subtype"`
	Name                         string   `json:"name" db:"name"`
	Quantity                     *float64 `json:"quantity,omitempty" db:"quantity"`
	Amount                       float64  `json:"amount" db:"amount"`
	Price                        *float64 `json:"price,omitempty" db:"price"`
	Fees                         *float64 `json:"fees,omitempty" db:"fees"`
	// CurrencyCode is the currency of Amount, Price and Fees on this row, from Plaid's
	// iso_currency_code falling back to unofficial_currency_code (#1631 M8a). NULL means unknown
	// — the state of every row written before that change.
	CurrencyCode *string `json:"currency_code,omitempty" db:"currency_code"`
	// Date is the UTC calendar day of the trade/dividend/fee, over a DATE column
	// (investment_transactions.date). Kept as time.Time — NOT serialized — because it is also the
	// SQL bind param on insert and the source of InvestmentTransactionCursor.SVT for keyset
	// pagination, both of which need a real time.Time rather than a formatted string.
	Date time.Time `json:"-" db:"date"`
	// DateDisplay is Date formatted as a UTC calendar day (YYYY-MM-DD), populated at the DB edge
	// by the repository — the same transient-field pattern as TickerSymbol/SecurityName/
	// ActivityType above. This is what actually reaches clients as "date".
	//
	// Added by #1631: Date used to carry `json:"date"` directly, so a DATE column serialized as
	// a full timestamp (`2026-01-02T00:00:00Z`), which renders a day early for any client west of
	// UTC that parses it in local time — the same defect class HoldingSnapshotView's AsOfDate
	// documents and fixes for holding_snapshots.
	DateDisplay string `json:"date" db:"-" doc:"UTC calendar day (YYYY-MM-DD) of the transaction"`

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
//
// ⚠ Every counter below that is not TransactionsInserted means something DIFFERENT, and #1631
// exists partly because they were previously conflated. Read the distinction before adding a
// fifth: "we chose not to store it", "it was already there", and "we lost it" have opposite
// operational responses, and a single number cannot tell an on-call engineer which happened.
type InvestmentTransactionSyncResult struct {
	SecuritiesUpserted   int
	TransactionsInserted int

	// TransactionsSkipped means ALREADY PRESENT and nothing else — it is computed as
	// len(domainTxns) - inserted, i.e. rows the upsert found on a unique conflict. It is a
	// healthy number on every incremental sync (the 1-day overlap window guarantees some), so it
	// must never be overloaded with a loss or an exclusion, both of which would then be invisible
	// inside a figure nobody investigates.
	TransactionsSkipped int

	// TransactionsExcluded counts rows DELIBERATELY not stored because the owner set
	// user_accounts.exclude_investment_sync on the chain that owns the account (#1631 M4). This is
	// the user's own instruction being carried out, not a defect and not a loss, which is why it
	// does not pin the watermark the way the Dropped* counters below do.
	TransactionsExcluded int

	// BackfilledChains counts the chains whose outstanding backfill obligation
	// (user_accounts.investment_backfill_requested_at) this run honoured by widening its Plaid
	// start date to the full lookback. Non-zero means the run cost a full-history re-fetch, which
	// is worth being able to see in the log rather than inferring from a slow sync.
	BackfilledChains int

	// The Dropped* counters below are LOSSES: a row Plaid delivered that this sync could not
	// store correctly. Any of them being non-zero pins investments_last_synced_date, because
	// advancing the cursor past a row we did not store is what makes the loss permanent — the
	// next run begins at lastSynced - 1 day and never sees it again.
	//
	// Each reason is separate on purpose. They have different causes and different fixes: an
	// unmapped account means our account table is behind Plaid's, an unparsable date means Plaid
	// sent something outside its own documented format, and an unresolved security means the
	// response referenced a security it did not include.
	TransactionsDroppedUnmappedAccount    int
	TransactionsDroppedUnparsableDate     int
	TransactionsDroppedUnresolvedSecurity int

	PagesProcessed int
}

// AnyTransactionDropped reports whether this run failed to store a row Plaid delivered, which is
// the condition under which investments_last_synced_date must NOT advance.
//
// ⚠ TransactionsExcluded is deliberately absent. An exclusion is the owner's instruction, so the
// window was fully processed and the watermark may move; including it here would pin the cursor of
// every item carrying an excluded brokerage cash account forever, re-fetching two years of the
// SIBLING accounts' history on every single webhook.
func (r *InvestmentTransactionSyncResult) AnyTransactionDropped() bool {
	return r.TransactionsDroppedUnmappedAccount > 0 ||
		r.TransactionsDroppedUnparsableDate > 0 ||
		r.TransactionsDroppedUnresolvedSecurity > 0
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
