package domain

import (
	"errors"
	"time"
)

// CashFlowFilters represents query parameters for filtering cash flow data
type CashFlowFilters struct {
	StartDate        time.Time // Required: Start of date range
	EndDate          time.Time // Required: End of date range
	AccountTypes     []string  // Optional: Filter by account types
	IncludeTransfers bool      // Optional: Include transfer transactions (default false)
	Grouping         string    // For history: "daily", "weekly", "monthly"
}

// Validate checks that the CashFlowFilters have all required fields
func (f *CashFlowFilters) Validate() error {
	if f.StartDate.IsZero() {
		return errors.New("start_date is required")
	}
	if f.EndDate.IsZero() {
		return errors.New("end_date is required")
	}
	if f.EndDate.Before(f.StartDate) {
		return errors.New("end_date cannot be before start_date")
	}
	// Validate grouping if provided
	if f.Grouping != "" {
		validGroupings := map[string]bool{
			"daily":   true,
			"weekly":  true,
			"monthly": true,
		}
		if !validGroupings[f.Grouping] {
			return errors.New("grouping must be one of: daily, weekly, monthly")
		}
	}
	return nil
}

// CashFlowSummaryResponse represents the response for GET /api/v1/analytics/cash-flow-summary
// Provides aggregated income, expenses, and net cash flow for a date range.
type CashFlowSummaryResponse struct {
	StartDate        time.Time                `json:"start_date"`
	EndDate          time.Time                `json:"end_date"`
	Income           float64                  `json:"income"`
	Expenses         float64                  `json:"expenses"`
	NetCashFlow      float64                  `json:"net_cash_flow"`
	IncomeCount      int                      `json:"income_count"`
	ExpenseCount     int                      `json:"expense_count"`
	PeriodComparison *CashFlowPeriodComparison `json:"period_comparison,omitempty"`
}

// CashFlowPeriodComparison represents cash flow change compared to a previous period
type CashFlowPeriodComparison struct {
	PreviousIncome       float64 `json:"previous_income"`
	PreviousExpenses     float64 `json:"previous_expenses"`
	PreviousNetCashFlow  float64 `json:"previous_net_cash_flow"`
	IncomeChangeAmount   float64 `json:"income_change_amount"`
	IncomeChangePercent  float64 `json:"income_change_percent"`
	ExpenseChangeAmount  float64 `json:"expense_change_amount"`
	ExpenseChangePercent float64 `json:"expense_change_percent"`
	NetChangeAmount      float64 `json:"net_change_amount"`
	NetChangePercent     float64 `json:"net_change_percent"`
}

// CashFlowHistoryResponse represents the response for GET /api/v1/analytics/cash-flow-history
// Provides time series cash flow data for charts with period grouping.
type CashFlowHistoryResponse struct {
	StartDate time.Time             `json:"start_date"`
	EndDate   time.Time             `json:"end_date"`
	Grouping  string                `json:"grouping"` // "daily", "weekly", "monthly"
	Periods   []CashFlowPeriodData  `json:"periods"`
}

// CashFlowPeriodData represents a single period in the cash flow history timeline
type CashFlowPeriodData struct {
	PeriodStart      time.Time `json:"period_start"`
	PeriodEnd        time.Time `json:"period_end"`
	Income           float64   `json:"income"`
	Expenses         float64   `json:"expenses"`
	NetCashFlow      float64   `json:"net_cash_flow"`
	TransactionCount int       `json:"transaction_count"`
}

// CashFlowAggregation represents raw aggregated cash flow data from the database
type CashFlowAggregation struct {
	Income       float64
	Expenses     float64
	IncomeCount  int
	ExpenseCount int
}

// NetCashFlow returns the net cash flow (income - expenses)
func (a *CashFlowAggregation) NetCashFlow() float64 {
	return a.Income - a.Expenses
}

// CashFlowGroupedData represents cash flow data grouped by period from the database
type CashFlowGroupedData struct {
	PeriodStart      time.Time
	Income           float64
	Expenses         float64
	TransactionCount int
}

// SpendingFilters represents query parameters for spending report endpoints
type SpendingFilters struct {
	StartDate        time.Time
	EndDate          time.Time
	AccountTypes     []string
	IncludeTransfers bool
	Category         string // Optional: filter by primary category for subcategory drill-down
}

// SpendingByCategory represents spending totals for a single category
type SpendingByCategory struct {
	Category         string  `json:"category"`
	PFCPrimaryDisplay string  `json:"pfc_primary_display"`
	Total            float64 `json:"total"`
	Percentage       float64 `json:"percentage"`
	TransactionCount int     `json:"transaction_count"`
}

// SpendingByCategoryResponse is the response for GET /analytics/spending-by-category
type SpendingByCategoryResponse struct {
	StartDate  string               `json:"start_date"`
	EndDate    string               `json:"end_date"`
	TotalSpent float64              `json:"total_spent"`
	Categories []SpendingByCategory `json:"categories"`
}

// SpendingByMerchant represents spending totals for a single merchant
type SpendingByMerchant struct {
	MerchantName     string  `json:"merchant_name"`
	Total            float64 `json:"total"`
	TransactionCount int     `json:"transaction_count"`
	Percentage       float64 `json:"percentage"`
}

// SpendingByMerchantResponse is the response for GET /analytics/spending-by-merchant
type SpendingByMerchantResponse struct {
	StartDate string               `json:"start_date"`
	EndDate   string               `json:"end_date"`
	TopN      int                  `json:"top_n"`
	Merchants []SpendingByMerchant `json:"merchants"`
}

// CategorySpending represents spending for a single category within a trend period
type CategorySpending struct {
	Category         string  `json:"category"`
	PFCPrimaryDisplay string  `json:"pfc_primary_display"`
	Total            float64 `json:"total"`
}

// SpendingTrendPeriod represents one month's spending breakdown in a trends response
type SpendingTrendPeriod struct {
	PeriodStart string             `json:"period_start"`
	PeriodEnd   string             `json:"period_end"`
	Categories  []CategorySpending `json:"categories"`
	Total       float64            `json:"total"`
}

// SpendingTrendsResponse is the response for GET /analytics/spending-trends
type SpendingTrendsResponse struct {
	StartDate     string                `json:"start_date"`
	EndDate       string                `json:"end_date"`
	Periods       []SpendingTrendPeriod `json:"periods"`
	TopCategories []string              `json:"top_categories"` // Top 5 categories by total spend
}

// SpendingTrendRow is a raw database row from the spending trends query (month + category)
type SpendingTrendRow struct {
	PeriodStart time.Time
	Category    string
	Total       float64
}
