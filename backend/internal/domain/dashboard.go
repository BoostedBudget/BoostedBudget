package domain

import "time"

// BalanceSummaryResponse represents the response for GET /api/v1/dashboard/balance-summary
// Provides current balance overview with breakdown by account type and period comparison.
type BalanceSummaryResponse struct {
	AsOfDate        time.Time                         `json:"as_of_date"`
	TotalBalance    float64                           `json:"total_balance"`
	ByType          map[string]*AccountTypeBalance    `json:"by_type"`
	PeriodComparison *PeriodComparisonData            `json:"period_comparison,omitempty"`
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
	StartDate   time.Time               `json:"start_date"`
	EndDate     time.Time               `json:"end_date"`
	Granularity string                  `json:"granularity"` // "daily", "weekly", "monthly"
	Snapshots   []BalanceHistorySnapshot `json:"snapshots"`
}

// BalanceHistorySnapshot represents a single point in the balance history timeline
type BalanceHistorySnapshot struct {
	Date         time.Time          `json:"date"`
	Balance      float64            `json:"balance"`
	ByType       map[string]float64 `json:"by_type,omitempty"` // Optional breakdown by type
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
}

// NetWorthResponse represents the response for GET /api/v1/dashboard/net-worth
// Provides current net worth (assets - liabilities) with breakdown by category.
type NetWorthResponse struct {
	AsOfDate         time.Time                          `json:"as_of_date"`
	NetWorth         float64                            `json:"net_worth"`
	TotalAssets      float64                            `json:"total_assets"`
	TotalLiabilities float64                            `json:"total_liabilities"`
	ByCategory       map[string]*NetWorthCategoryBalance `json:"by_category"`
	PeriodComparison *NetWorthPeriodComparison          `json:"period_comparison,omitempty"`
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
	StartDate   time.Time                 `json:"start_date"`
	EndDate     time.Time                 `json:"end_date"`
	Granularity string                    `json:"granularity"` // "daily", "weekly", "monthly"
	Snapshots   []NetWorthHistorySnapshot `json:"snapshots"`
}

// NetWorthHistorySnapshot represents a single point in the net worth history timeline
type NetWorthHistorySnapshot struct {
	Date             time.Time `json:"date"`
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
