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
	CreatedAt        time.Time `json:"created_at" db:"created_at"`
	UpdatedAt        time.Time `json:"updated_at" db:"updated_at"`
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
type PortfolioHistoryPoint struct {
	Date       time.Time `json:"date" db:"snapshot_date"`
	TotalValue float64   `json:"total_value" db:"total_value"`
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
	SecurityType *string `json:"security_type,omitempty"` // stock/etf/mutual fund/crypto/etc.; nil → "unknown"

	// Computed values (nil when inputs are missing)
	GainLoss     *float64 `json:"gain_loss,omitempty"`
	GainLossPct  *float64 `json:"gain_loss_pct,omitempty"`
	DayChange    *float64 `json:"day_change,omitempty"`
	DayChangePct *float64 `json:"day_change_pct,omitempty"`
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
	}
	return v
}

// AllocationItem represents one bucket in the portfolio allocation breakdown.
type AllocationItem struct {
	SecurityType string  `json:"security_type"` // "stock", "etf", "bond", "crypto", "cash", "other", "unknown"
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
	HoldingID     string   `json:"holding_id"`
	AccountID     string   `json:"account_id"`
	TickerSymbol  *string  `json:"ticker_symbol,omitempty"`
	SecurityName  *string  `json:"security_name,omitempty"`
	DayChange     *float64 `json:"day_change,omitempty"`     // nil when close_price unavailable
	DayChangePct  *float64 `json:"day_change_pct,omitempty"` // nil when close_price unavailable
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
	AccountID               string   `json:"account_id"`
	UnrealizedGainLoss      float64  `json:"unrealized_gain_loss"`
	UnrealizedGainLossPct   *float64 `json:"unrealized_gain_loss_pct,omitempty"`
	CostBasis               float64  `json:"cost_basis"`
	Value                   float64  `json:"value"`
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
	IncomeType  string  `json:"income_type"`  // "dividend", "interest", "dividend_reinvestment", "interest_reinvestment", "other_income"
	TotalAmount float64 `json:"total_amount"`
	Count       int     `json:"count"`
}
