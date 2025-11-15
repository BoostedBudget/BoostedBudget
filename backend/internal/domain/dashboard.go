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
