package domain

import "time"

// BalanceSummaryResponse represents the response for GET /api/v1/dashboard/balance-summary
// Provides current balance overview with breakdown by account type and period comparison.
type BalanceSummaryResponse struct {
	// AsOfDate stays time.Time — NOT serialized; AsOfDateDisplay is what reaches clients (#1631,
	// same defect class HoldingSnapshotView.AsOfDate and InvestmentTransaction.DateDisplay fix).
	AsOfDate         time.Time                      `json:"-"`
	AsOfDateDisplay  string                         `json:"as_of_date" format:"date" doc:"UTC calendar day (YYYY-MM-DD) this balance summary is as of"`
	TotalBalance     float64                        `json:"total_balance" doc:"Signed sum of snapshot balances: raw credit and loan balances are negated (negative raw debt is an overpayment); other account types retain their raw sign"`
	ByType           map[string]*AccountTypeBalance `json:"by_type" doc:"Account type totals and individual balances use the same signed contributions as total_balance"`
	PeriodComparison *PeriodComparisonData          `json:"period_comparison,omitempty" doc:"Comparison with the signed balance 30 days earlier; change_amount is current minus previous, and change_percent divides by the absolute previous balance (zero when previous is zero)"`
}

// AccountTypeBalance represents balance information for a specific account type
type AccountTypeBalance struct {
	Balance      float64                `json:"balance"`
	AccountCount int                    `json:"account_count"`
	Accounts     []AccountBalanceDetail `json:"accounts"`
}

// AccountBalanceDetail represents individual account balance information
type AccountBalanceDetail struct {
	ID      string  `json:"id"`
	Name    string  `json:"name"`
	Balance float64 `json:"balance"`
}

// PeriodComparisonData represents balance change compared to a previous period
type PeriodComparisonData struct {
	PreviousBalance float64 `json:"previous_balance"`
	ChangeAmount    float64 `json:"change_amount"`
	ChangePercent   float64 `json:"change_percent"`
}

// BalanceHistoryResponse represents the response for GET /api/v1/dashboard/balance-history
// Provides time series balance data for charts with optional account type breakdown.
type BalanceHistoryResponse struct {
	// StartDate/EndDate stay time.Time — NOT serialized; StartDateDisplay/EndDateDisplay are what
	// reach clients (#1631).
	StartDate        time.Time                `json:"-"`
	StartDateDisplay string                   `json:"start_date" format:"date" doc:"UTC calendar day (YYYY-MM-DD) this history response starts on"`
	EndDate          time.Time                `json:"-"`
	EndDateDisplay   string                   `json:"end_date" format:"date" doc:"UTC calendar day (YYYY-MM-DD) this history response ends on"`
	Granularity      string                   `json:"granularity"` // "daily", "weekly", "monthly"
	Snapshots        []BalanceHistorySnapshot `json:"snapshots"`
}

// BalanceHistorySnapshot represents a single point in the balance history timeline
type BalanceHistorySnapshot struct {
	// Date stays time.Time — NOT serialized; DateDisplay is what reaches clients (#1631).
	Date        time.Time          `json:"-"`
	DateDisplay string             `json:"date" format:"date" doc:"UTC calendar day (YYYY-MM-DD) of this snapshot"`
	Balance     float64            `json:"balance"`
	ByType      map[string]float64 `json:"by_type,omitempty"` // Optional breakdown by type
}

// AccountTypesResponse represents the response for GET /api/v1/dashboard/account-types
// Returns available account types for the user with counts and totals.
type AccountTypesResponse struct {
	Types []AccountTypeInfo `json:"types"`
}

// AccountTypeInfo represents information about an account type
type AccountTypeInfo struct {
	Key          string  `json:"key"`           // "investment", "checking", "savings", "credit", "loan"
	Label        string  `json:"label"`         // "Investments", "Checking", etc.
	Count        int     `json:"count"`         // Number of accounts
	TotalBalance float64 `json:"total_balance"` // Sum of balances for this type
}

// DashboardFilters represents query parameters for filtering dashboard data
type DashboardFilters struct {
	AccountTypes []string   // Filter by account types (investment, checking, savings, credit, loan)
	AsOfDate     *time.Time // Get balance as of specific date (for balance summary)
	StartDate    *time.Time // Start of date range (for balance history)
	EndDate      *time.Time // End of date range (for balance history)
	Granularity  string     // "daily", "weekly", "monthly" (for balance history)

	// HouseholdScope, when non-nil, aggregates across the household's shared accounts
	// instead of the requesting user's own. nil = personal scope (today's behavior,
	// byte-identical output). Issue #1369 — Phase 5.
	HouseholdScope *HouseholdScope
}

// NetWorthResponse represents the response for GET /api/v1/dashboard/net-worth
// Provides current net worth (assets - liabilities) with breakdown by category.
type NetWorthResponse struct {
	// AsOfDate stays time.Time — NOT serialized; AsOfDateDisplay is what reaches clients (#1631).
	AsOfDate         time.Time                           `json:"-"`
	AsOfDateDisplay  string                              `json:"as_of_date" format:"date" doc:"UTC calendar day (YYYY-MM-DD) this net worth figure is as of"`
	NetWorth         float64                             `json:"net_worth"`
	TotalAssets      float64                             `json:"total_assets"`
	TotalLiabilities float64                             `json:"total_liabilities"`
	ByCategory       map[string]*NetWorthCategoryBalance `json:"by_category"`
	PeriodComparison *NetWorthPeriodComparison           `json:"period_comparison,omitempty"`
}

// NetWorthCategoryBalance represents balance information for assets or liabilities
type NetWorthCategoryBalance struct {
	Category     string                         `json:"category"` // "assets" or "liabilities"
	Total        float64                        `json:"total"`
	AccountCount int                            `json:"account_count"`
	ByType       map[string]*AccountTypeBalance `json:"by_type"`
}

// NetWorthPeriodComparison represents net worth change compared to a previous period
type NetWorthPeriodComparison struct {
	PreviousNetWorth      float64 `json:"previous_net_worth"`
	PreviousAssets        float64 `json:"previous_assets"`
	PreviousLiabilities   float64 `json:"previous_liabilities"`
	NetWorthChange        float64 `json:"net_worth_change"`
	NetWorthChangePercent float64 `json:"net_worth_change_percent"`
}

// NetWorthHistoryResponse represents the response for GET /api/v1/dashboard/net-worth-history
// Provides time series net worth data for charts.
type NetWorthHistoryResponse struct {
	// StartDate/EndDate stay time.Time — NOT serialized; StartDateDisplay/EndDateDisplay are what
	// reach clients (#1631).
	StartDate        time.Time                 `json:"-"`
	StartDateDisplay string                    `json:"start_date" format:"date" doc:"UTC calendar day (YYYY-MM-DD) this history response starts on"`
	EndDate          time.Time                 `json:"-"`
	EndDateDisplay   string                    `json:"end_date" format:"date" doc:"UTC calendar day (YYYY-MM-DD) this history response ends on"`
	Granularity      string                    `json:"granularity"` // "daily", "weekly", "monthly"
	Snapshots        []NetWorthHistorySnapshot `json:"snapshots"`
}

// NetWorthHistorySnapshot represents a single point in the net worth history timeline
type NetWorthHistorySnapshot struct {
	// Date stays time.Time — NOT serialized; DateDisplay is what reaches clients (#1631).
	Date             time.Time `json:"-"`
	DateDisplay      string    `json:"date" format:"date" doc:"UTC calendar day (YYYY-MM-DD) of this snapshot"`
	NetWorth         float64   `json:"net_worth"`
	TotalAssets      float64   `json:"total_assets"`
	TotalLiabilities float64   `json:"total_liabilities"`
}

// AccountTypeCategory returns "assets" or "liabilities" for an account type.
// Used to classify accounts for net worth calculation.
// - Assets: depository (checking, savings), investment
// - Liabilities: credit (credit cards), loan (mortgages, student loans)
// - Other: defaults to assets (can be configured in future)
func AccountTypeCategory(accountType string) string {
	switch accountType {
	case "credit", "loan":
		return "liabilities"
	default: // depository, investment, other
		return "assets"
	}
}
