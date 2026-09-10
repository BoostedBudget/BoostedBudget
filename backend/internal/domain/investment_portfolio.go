package domain

import "time"

// PortfolioSnapshot is a daily aggregate snapshot of a user's full investment portfolio.
// One row per (user_id, snapshot_date) UTC calendar day, stored in portfolio_snapshots.
// Written by holdings-sync (recomputed from live holdings after each sync) and by the
// daily continuity cron. The write is skipped when any account was skipped in the sync
// to avoid a partial total becoming a day-change baseline.
type PortfolioSnapshot struct {
	ID               string    `json:"id" db:"id"`
	UserID           string    `json:"user_id" db:"user_id"`
	SnapshotDate     time.Time `json:"snapshot_date" db:"snapshot_date"`
	TotalValue       float64   `json:"total_value" db:"total_value"`
	TotalCostBasis   float64   `json:"total_cost_basis" db:"total_cost_basis"`
	TotalGainLoss    float64   `json:"total_gain_loss" db:"total_gain_loss"`
	TotalGainLossPct *float64  `json:"total_gain_loss_pct,omitempty" db:"total_gain_loss_pct"` // nil when cost_basis=0

	// TotalValueInvestmentOnly is TotalValue restricted to accounts of type 'investment'
	// (#1613 P4-T3). It exists because TotalValue is deliberately UNGATED by account type — see
	// portfolio_snapshot_repository.go Upsert for why gating the historical series mid-stream is
	// forbidden — while every LIVE holdings read is type-gated. GetPortfolioSummary subtracts a
	// prior snapshot from a live total, so without this column the two sides of that subtraction
	// came from different account universes and DayChange was wrong by the whole value of any
	// depository account carrying holdings.
	//
	// ⚠ NIL means UNAVAILABLE, not zero. Every row written before migration 000189 has it NULL.
	// Readers must render day-change as unavailable in that case — never fall back to TotalValue,
	// which is precisely the wrong number this field exists to stop being shown.
	//
	// ⚠ CORRECTED: this comment used to add "and nothing can reconstruct it (holdings carry no
	// as-of account type)". The parenthetical is true and the conclusion drawn from it was wrong,
	// which is why RepairUserFromHistory originally left this column alone and could leave a
	// STALE value beside a corrected total_value (found by Codex adversarial review). No as-of
	// type is NEEDED: the figure this must stay comparable with is the LIVE holdings read, which
	// is itself gated on the account's CURRENT type — so reconstructing from current type is the
	// same predicate the other operand uses, not an approximation. The repair now writes this
	// column whenever it rewrites a row.
	TotalValueInvestmentOnly *float64 `json:"total_value_investment_only,omitempty" db:"total_value_investment_only"`

	CreatedAt time.Time `json:"created_at" db:"created_at"`
	UpdatedAt time.Time `json:"updated_at" db:"updated_at"`
}

// PortfolioSummary is the live portfolio summary returned by GET /portfolio/summary.
// total_value and gain_loss figures are computed live from current holdings.
// day_change is derived from the most-recent-prior portfolio snapshot; null when no prior snapshot exists.
type PortfolioSummary struct {
	TotalValue       float64  `json:"total_value"`
	TotalCostBasis   float64  `json:"total_cost_basis"`
	TotalGainLoss    float64  `json:"total_gain_loss"`
	TotalGainLossPct *float64 `json:"total_gain_loss_pct,omitempty"` // nil when cost_basis=0
	DayChange        *float64 `json:"day_change"`                    // nil when no prior snapshot
	DayChangePct     *float64 `json:"day_change_pct"`                // nil when no prior snapshot or value=0
	HoldingsCount    int      `json:"holdings_count"`
}

// PortfolioHistoryPoint is one data point in a portfolio value chart.
//
// Date is a UTC calendar day (YYYY-MM-DD), formatted at the DB edge from
// portfolio_snapshots.snapshot_date (a DATE column) by investmentService.GetPortfolioHistory.
// Deliberately a plain string rather than time.Time, following HoldingSnapshotView's precedent
// (#1631): unlike InvestmentTransaction, this type is never bound as a SQL param or compared as a
// time.Time after construction, so there is no reason to carry the timestamp shift a DATE column
// serialized through time.Time inflicts on any client west of UTC.
type PortfolioHistoryPoint struct {
	Date       string  `json:"date" doc:"UTC calendar day (YYYY-MM-DD) this snapshot was captured"`
	TotalValue float64 `json:"total_value"`
}

// HoldingView is the API view of a holding, joining security metadata and
// including pre-computed gain/loss and day_change values.
type HoldingView struct {
	// Core holding fields
	ID               string   `json:"id"`
	UserID           string   `json:"user_id"`
	AccountID        string   `json:"account_id"`
	UserAccountID    *string  `json:"user_account_id,omitempty"`
	SecurityID       string   `json:"security_id"`
	Quantity         float64  `json:"quantity"`
	CostBasis        *float64 `json:"cost_basis,omitempty"`
	InstitutionValue *float64 `json:"institution_value,omitempty"`
	InstitutionPrice *float64 `json:"institution_price,omitempty"`

	// Security metadata (from joined securities row)
	TickerSymbol *string `json:"ticker_symbol,omitempty"`
	SecurityName *string `json:"security_name,omitempty"`
	// stock/etf/mutual fund/crypto/etc., or nil when securities.type is NULL. This field carries
	// the raw provider token — nil is NOT rewritten to the literal string "unknown" here. The
	// frontend Holdings table renders nil (and a blank/"unknown" value) as "Unknown" via the
	// shared securityTypeLabel helper (#1631); the allocation aggregates instead COALESCE NULL to
	// 'unknown' in SQL so untyped holdings are not dropped from the allocation breakdown.
	SecurityType *string `json:"security_type,omitempty"`

	// Computed values (nil when inputs are missing)
	GainLoss     *float64 `json:"gain_loss,omitempty"`
	GainLossPct  *float64 `json:"gain_loss_pct,omitempty"`
	DayChange    *float64 `json:"day_change,omitempty"`
	DayChangePct *float64 `json:"day_change_pct,omitempty"`

	// DayChangeAsOf is the prior-session date the day change is measured FROM
	// (securities.close_price_as_of), and DayChangeIsStale is the server's verdict on whether that
	// date is too old for the figure to describe a day.
	//
	// #1631: close_price_as_of was write-only, so nothing could tell how old the anchor was — and
	// the price-retention logic deliberately keeps the last known close alive indefinitely when a
	// payload omits one, which makes unbounded staleness the normal case rather than an edge case.
	// A week-old close still produced a number labelled "day change".
	//
	// ⚠ A date-only STRING, not a timestamp, per the rule stated on HoldingSnapshotView: a
	// date-only value rendered through a timestamp shifts back a day for every user west of UTC.
	// The staleness verdict is served rather than left to the client because the threshold is
	// policy, not presentation.
	DayChangeAsOf    *string `json:"day_change_as_of,omitempty"`
	DayChangeIsStale *bool   `json:"day_change_is_stale,omitempty"`
}

// NewHoldingView constructs a HoldingView from a Holding (which must have Security populated).
func NewHoldingView(h *Holding) *HoldingView {
	v := &HoldingView{
		ID:               h.ID,
		UserID:           h.UserID,
		AccountID:        h.AccountID,
		UserAccountID:    h.UserAccountID,
		SecurityID:       h.SecurityID,
		Quantity:         h.Quantity,
		CostBasis:        h.CostBasis,
		InstitutionValue: h.InstitutionValue,
		InstitutionPrice: h.InstitutionPrice,
		GainLoss:         h.GainLoss(),
		GainLossPct:      h.GainLossPct(),
		DayChange:        h.DayChange(),
		DayChangePct:     h.DayChangePct(),
	}
	if h.Security != nil {
		v.TickerSymbol = h.Security.TickerSymbol
		v.SecurityName = &h.Security.Name
		v.SecurityType = h.Security.Type
		if h.Security.ClosePriceAsOf != nil {
			asOf := h.Security.ClosePriceAsOf.Format("2006-01-02")
			v.DayChangeAsOf = &asOf
		}
	}
	// Only meaningful alongside a day change; omitted otherwise so a client cannot read "not
	// stale" as "the figure is trustworthy" when there is no figure.
	if v.DayChange != nil {
		stale := h.ClosePriceIsStale(time.Now().UTC())
		v.DayChangeIsStale = &stale
	}
	return v
}

// AllocationGroupBy constants for the group_by query parameter.
const (
	AllocationGroupBySecurityType = "security_type" // default — groups by securities.type
	AllocationGroupByAssetClass   = "asset_class"   // collapses into Investments / Cash / Liabilities
	AllocationGroupByAccount      = "account"       // one bucket per account
)

// AllocationBasis constants for the basis query parameter.
const (
	AllocationBasisAssets = "assets" // default — assets-only allocation
	AllocationBasisNet    = "net"    // net-of-liabilities allocation
)

// AllocationOptions holds the optional query parameters that control how the
// GetPortfolioAllocationBreakdown endpoint aggregates data.
// Zero value is the backward-compatible default: investment holdings only,
// grouped by security type, assets basis.
type AllocationOptions struct {
	// Include is a list of additional asset classes to union into the result.
	// Valid values: "cash" (depository accounts), "liabilities" (credit/loan accounts).
	// Default (empty): investment holdings only — backward-compatible with #138 behavior.
	Include []string

	// GroupBy controls the bucketing dimension. Default: AllocationGroupBySecurityType.
	GroupBy string

	// Basis controls whether liabilities reduce the total. Default: AllocationBasisAssets.
	Basis string
}

// EffectiveGroupBy returns the active group_by value, defaulting to AllocationGroupBySecurityType.
func (o AllocationOptions) EffectiveGroupBy() string {
	if o.GroupBy == "" {
		return AllocationGroupBySecurityType
	}
	return o.GroupBy
}

// EffectiveBasis returns the active basis value, defaulting to AllocationBasisAssets.
func (o AllocationOptions) EffectiveBasis() string {
	if o.Basis == "" {
		return AllocationBasisAssets
	}
	return o.Basis
}

// IncludesCash reports whether the depository (cash) accounts should be unioned in.
func (o AllocationOptions) IncludesCash() bool {
	for _, v := range o.Include {
		if v == "cash" {
			return true
		}
	}
	return false
}

// IncludesLiabilities reports whether the credit/loan accounts should be unioned in.
func (o AllocationOptions) IncludesLiabilities() bool {
	for _, v := range o.Include {
		if v == "liabilities" {
			return true
		}
	}
	return false
}

// AllocationItem represents one bucket in the portfolio allocation breakdown.
type AllocationItem struct {
	// SecurityType is securities.type verbatim from Plaid, or "unknown" when absent. ⚠ #1631: the
	// values are PLAID's enum — cash, cryptocurrency, derivative, equity, etf, fixed income, loan,
	// mutual fund, other — plus "unknown" and the synthetic "liabilities". This comment previously
	// advertised "stock", "bond" and "crypto", none of which Plaid ever sends.
	SecurityType string  `json:"security_type"`
	AssetClass   string  `json:"asset_class"` // "investments", "cash", "liabilities" — populated when group_by=asset_class
	TotalValue   float64 `json:"total_value"`
	Percentage   float64 `json:"percentage"`
	HoldingCount int     `json:"holding_count"`
}

// AllocationBreakdown is the response for GET /portfolio/allocation.
type AllocationBreakdown struct {
	Buckets    []AllocationItem `json:"buckets"`
	TotalValue float64          `json:"total_value"`
}

// TopMoverItem represents one holding in the top movers list.
type TopMoverItem struct {
	HoldingID    string  `json:"holding_id"`
	AccountID    string  `json:"account_id"`
	TickerSymbol *string `json:"ticker_symbol,omitempty"`
	SecurityName *string `json:"security_name,omitempty"`
	// ⚠ #1631 corrected these comments rather than the code. They claimed "nil when close_price
	// unavailable", but such a holding is ABSENT from this surface, not present-with-nil: the query
	// inner-JOINs securities and requires close_price IS NOT NULL. That exclusion is right for a
	// top-MOVERS list — a holding with no prior close cannot be ranked by how far it moved — and it
	// is already stated in the endpoint's own description. The comment was the wrong half.
	//
	// DayChange is therefore always present here. DayChangePct is nil only when the prior close
	// VALUE is zero (a zero quantity, or a zero close price), where the percentage is undefined.
	DayChange     *float64 `json:"day_change,omitempty"`
	DayChangePct  *float64 `json:"day_change_pct,omitempty"` // nil only when the prior value is zero
	TotalGainLoss *float64 `json:"total_gain_loss,omitempty"`
}

// TopMovers is the response for GET /portfolio/top-movers.
type TopMovers struct {
	Gainers []TopMoverItem `json:"gainers"`
	Losers  []TopMoverItem `json:"losers"`
}

// GainLossSummary is the response for GET /portfolio/gain-loss.
// Realized gain/loss is explicitly unsupported — Plaid does not provide per-lot
// cost basis and the 2-year lookback truncates buy history a FIFO replay would need.
type GainLossSummary struct {
	TotalUnrealizedGainLoss    float64  `json:"total_unrealized_gain_loss"`
	TotalUnrealizedGainLossPct *float64 `json:"total_unrealized_gain_loss_pct,omitempty"` // nil when no cost_basis data
	TotalCostBasis             float64  `json:"total_cost_basis"`
	TotalValue                 float64  `json:"total_value"`
	RealizedGainLossSupported  bool     `json:"realized_gain_loss_supported"` // always false — documented in response
	// Per-account breakdown
	ByAccount []AccountGainLoss `json:"by_account"`
}

// AccountGainLoss is the gain/loss breakdown for one account.
type AccountGainLoss struct {
	AccountID             string   `json:"account_id"`
	UnrealizedGainLoss    float64  `json:"unrealized_gain_loss"`
	UnrealizedGainLossPct *float64 `json:"unrealized_gain_loss_pct,omitempty"`
	CostBasis             float64  `json:"cost_basis"`
	Value                 float64  `json:"value"`
}

// IncomeSummary is the response for GET /portfolio/income.
// Income includes dividends, interest, AND reinvested dividends/interest —
// reinvested income is economically income even though the display feed may
// show it in a separate "reinvest" bucket.
type IncomeSummary struct {
	TotalIncome float64        `json:"total_income"`
	ByType      []IncomeByType `json:"by_type"`
	StartDate   time.Time      `json:"start_date"`
	EndDate     time.Time      `json:"end_date"`
}

// IncomeByType is the income breakdown by normalized income subtype.
type IncomeByType struct {
	// IncomeType is one of "dividend", "interest", "capital_gain", "dividend_reinvestment",
	// "interest_reinvestment", "other_income". #1631 added "capital_gain" (capital-gain
	// distributions previously matched no set at all and fell through to the generic bucket) and
	// began counting a structural dividend — a cash credit against a non-cash-equivalent security,
	// which is how some brokerages report an ETF distribution — under "dividend".
	IncomeType  string  `json:"income_type"`
	TotalAmount float64 `json:"total_amount"`
	Count       int     `json:"count"`
}
