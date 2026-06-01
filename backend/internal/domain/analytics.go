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
	StartDate        time.Time                 `json:"start_date"`
	EndDate          time.Time                 `json:"end_date"`
	Income           float64                   `json:"income"`
	Expenses         float64                   `json:"expenses"`
	NetCashFlow      float64                   `json:"net_cash_flow"`
	IncomeCount      int                       `json:"income_count"`
	ExpenseCount     int                       `json:"expense_count"`
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
	StartDate time.Time            `json:"start_date"`
	EndDate   time.Time            `json:"end_date"`
	Grouping  string               `json:"grouping"` // "daily", "weekly", "monthly"
	Periods   []CashFlowPeriodData `json:"periods"`
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

	// CustomCategoryIDs scopes the report to specific custom categories (Issue #1203).
	// When RollUpChildren is true, a parent custom id also matches its children.
	CustomCategoryIDs []string
	RollUpChildren    bool
}

// SpendingByCategory represents spending totals for a single category
type SpendingByCategory struct {
	Category          string  `json:"category"`
	PFCPrimaryDisplay string  `json:"pfc_primary_display"`
	Total             float64 `json:"total"`
	Percentage        float64 `json:"percentage"`
	TransactionCount  int     `json:"transaction_count"`
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
	MerchantID       *string `json:"merchant_id,omitempty"`
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
	Category          string  `json:"category"`
	PFCPrimaryDisplay string  `json:"pfc_primary_display"`
	Total             float64 `json:"total"`
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
	PeriodStart     time.Time
	Category        string
	CategoryDisplay string // resolved display name for custom categories; empty for PFC rows (service computes display)
	Total           float64
}

// ─── MCP analytics helpers (Issue #1087) ──────────────────────────────────────

// CategoryTotalRow is a raw database row for GetCategoryTotalForPeriod
type CategoryTotalRow struct {
	Total            float64
	TransactionCount int
}

// CategoryTotalResponse is the response for GET /analytics/category-total (AI-tool-callable)
type CategoryTotalResponse struct {
	Category         string  `json:"category"`
	PFCPrimaryDisplay string `json:"pfc_primary_display"`
	Total            float64 `json:"total"`
	TransactionCount int     `json:"transaction_count"`
	StartDate        string  `json:"start_date"`
	EndDate          string  `json:"end_date"`
}

// TopExpenseRow is a raw database row for GetTopExpense
type TopExpenseRow struct {
	TransactionID string
	MerchantName  string
	Amount        float64
	Category      string
	Date          time.Time
}

// TopExpenseResponse is the response for GET /analytics/top-expense (AI-tool-callable)
// Returns the single largest expense transaction in a date range (nil if no expenses).
type TopExpenseResponse struct {
	TransactionID     string  `json:"transaction_id"`
	MerchantName      string  `json:"merchant_name"`
	Amount            float64 `json:"amount"`
	Category          string  `json:"category"`
	PFCPrimaryDisplay string  `json:"pfc_primary_display"`
	Date              string  `json:"date"`
	StartDate         string  `json:"start_date"`
	EndDate           string  `json:"end_date"`
}

// ─── Issue #1215 analytics helpers ──────────────────────────────────────────

// AverageMonthlySpendRow is the raw DB row for GetAverageMonthlySpend.
type AverageMonthlySpendRow struct {
	Total float64
}

// AverageMonthlySpendResponse is the response for GET /analytics/average-monthly-spend.
// AI-tool-callable: single DB round-trip, token-efficient summary.
type AverageMonthlySpendResponse struct {
	AverageMonthlySpend float64 `json:"average_monthly_spend"`
	TotalSpend          float64 `json:"total_spend"`
	MonthCount          int     `json:"month_count"`
	StartDate           string  `json:"start_date"`
	EndDate             string  `json:"end_date"`
}

// RecurringSubscriptionRow is the raw DB row for GetRecurringSubscriptions.
type RecurringSubscriptionRow struct {
	MerchantName     string
	Amount           float64
	OccurrenceCount  int
	AvgGapDays       float64
	StddevGapDays    float64
	LastChargeDate   time.Time
	NextExpectedDate time.Time
}

// RecurringSubscriptionItem is a single detected recurring subscription.
type RecurringSubscriptionItem struct {
	MerchantName     string  `json:"merchant_name"`
	Amount           float64 `json:"amount"`
	OccurrenceCount  int     `json:"occurrence_count"`
	AvgGapDays       float64 `json:"avg_gap_days"`
	LastChargeDate   string  `json:"last_charge_date"`
	NextExpectedDate string  `json:"next_expected_date"`
}

// RecurringSubscriptionsResponse is the response for GET /analytics/recurring-subscriptions.
// AI-tool-callable: heuristic detection of recurring charges (monthly cadence). Issue #1215.
type RecurringSubscriptionsResponse struct {
	Subscriptions []RecurringSubscriptionItem `json:"subscriptions"`
	AsOfDate      string                      `json:"as_of_date"`
}

// ─── Issue #1215 PR2 analytics helpers ──────────────────────────────────────

// SavingsRateRow is the raw DB row for GetSavingsRateForPeriod.
type SavingsRateRow struct {
	Income   float64
	Expenses float64
}

// SavingsRateResponse is the response for GET /analytics/savings-rate (AI-tool-callable).
type SavingsRateResponse struct {
	SavingsRate *float64 `json:"savings_rate"` // nil when income=0
	Income      float64  `json:"income"`
	Expenses    float64  `json:"expenses"`
	NetSavings  float64  `json:"net_savings"`
	StartDate   string   `json:"start_date"`
	EndDate     string   `json:"end_date"`
}

// MonthOverMonthBucketRow is a raw DB row for GetMonthOverMonthSpending.
type MonthOverMonthBucketRow struct {
	Month time.Time
	Total float64
}

// MonthOverMonthBucket is a single month in the month-over-month response.
type MonthOverMonthBucket struct {
	Month     string   `json:"month"`      // "2024-01-01" (first day of month)
	Total     float64  `json:"total"`
	ChangePct *float64 `json:"change_pct"` // nil for first bucket
	ChangeAbs *float64 `json:"change_abs"` // nil for first bucket
}

// MonthOverMonthResponse is the response for GET /analytics/month-over-month (AI-tool-callable).
type MonthOverMonthResponse struct {
	Buckets    []MonthOverMonthBucket `json:"buckets"`
	Category   string                 `json:"category"`    // "" = all spending
	MonthCount int                    `json:"month_count"`
}

// LargestTransactionRow is a raw DB row for GetLargestTransactionsForPeriod.
type LargestTransactionRow struct {
	TransactionID string
	MerchantName  string
	Amount        float64
	Category      string
	Date          time.Time
	AccountID     string
}

// LargestTransactionItem is a single transaction in the largest-transactions response.
type LargestTransactionItem struct {
	TransactionID     string  `json:"transaction_id"`
	MerchantName      string  `json:"merchant_name"`
	Amount            float64 `json:"amount"`
	Category          string  `json:"category"`
	PFCPrimaryDisplay string  `json:"pfc_primary_display"`
	Date              string  `json:"date"`
	AccountID         string  `json:"account_id"`
}

// LargestTransactionsResponse is the response for GET /analytics/largest-transactions.
type LargestTransactionsResponse struct {
	Transactions []LargestTransactionItem `json:"transactions"`
	Limit        int                      `json:"limit"`
	StartDate    string                   `json:"start_date"`
	EndDate      string                   `json:"end_date"`
}

// CategoryBreakdownForMerchantRow is a raw DB row for GetCategoryBreakdownForMerchant.
type CategoryBreakdownForMerchantRow struct {
	Category string
	Total    float64
	Count    int
}

// CategoryBreakdownItem is a single category in the merchant-category-breakdown response.
type CategoryBreakdownItem struct {
	Category          string  `json:"category"`
	PFCPrimaryDisplay string  `json:"pfc_primary_display"`
	Total             float64 `json:"total"`
	Percentage        float64 `json:"percentage"`
	TransactionCount  int     `json:"transaction_count"`
}

// CategoryBreakdownForMerchantResponse is the response for GET /analytics/merchant-category-breakdown.
type CategoryBreakdownForMerchantResponse struct {
	MerchantName string                  `json:"merchant_name"`
	Categories   []CategoryBreakdownItem `json:"categories"`
	TotalSpend   float64                 `json:"total_spend"`
	StartDate    string                  `json:"start_date"`
	EndDate      string                  `json:"end_date"`
}

// IncomeSourceRow is a raw DB row for GetIncomeSourceBreakdown.
type IncomeSourceRow struct {
	Category string
	Total    float64
	Count    int
}

// IncomeSourceItem is a single income source in the breakdown response.
type IncomeSourceItem struct {
	Category          string  `json:"category"`
	PFCPrimaryDisplay string  `json:"pfc_primary_display"`
	Total             float64 `json:"total"`
	Percentage        float64 `json:"percentage"`
	TransactionCount  int     `json:"transaction_count"`
}

// IncomeSourceBreakdownResponse is the response for GET /analytics/income-sources.
type IncomeSourceBreakdownResponse struct {
	Sources     []IncomeSourceItem `json:"sources"`
	TotalIncome float64            `json:"total_income"`
	StartDate   string             `json:"start_date"`
	EndDate     string             `json:"end_date"`
}

// ─── Issue #1215 PR3 analytics helpers ──────────────────────────────────────

// UnusualSpendRow is the raw DB row for GetUnusualSpend.
type UnusualSpendRow struct {
	TransactionID string
	MerchantName  string
	Amount        float64
	Category      string
	Date          time.Time
	BaselineAvg   float64
	SigmaAbove    float64
}

// UnusualSpendItem is a single unusual spend transaction in the response.
type UnusualSpendItem struct {
	TransactionID     string  `json:"transaction_id"`
	MerchantName      string  `json:"merchant_name"`
	Amount            float64 `json:"amount"`
	Category          string  `json:"category"`
	PFCPrimaryDisplay string  `json:"pfc_primary_display"`
	Date              string  `json:"date"`
	BaselineAvg       float64 `json:"baseline_avg"`
	SigmaAbove        float64 `json:"sigma_above"`
}

// UnusualSpendResponse is the response for GET /analytics/unusual-spend (AI-tool-callable).
type UnusualSpendResponse struct {
	Items          []UnusualSpendItem `json:"items"`
	StartDate      string             `json:"start_date"`
	EndDate        string             `json:"end_date"`
	SigmaThreshold float64            `json:"sigma_threshold"`
}

// NetWorthBucket represents one time period in the net worth trend.
type NetWorthBucket struct {
	Date      string   `json:"date"`        // first day of bucket period (YYYY-MM-DD)
	NetWorth  float64  `json:"net_worth"`
	ChangeAbs *float64 `json:"change_abs"`  // nil for first bucket
	ChangePct *float64 `json:"change_pct"`  // nil for first bucket; nil if prev=0
}

// NetWorthTrendResponse is the response for GET /analytics/net-worth-trend (AI-tool-callable).
type NetWorthTrendResponse struct {
	Buckets   []NetWorthBucket `json:"buckets"`
	Grouping  string           `json:"grouping"`
	StartDate string           `json:"start_date"`
	EndDate   string           `json:"end_date"`
}

// BudgetVsActualRow is the raw DB row for GetBudgetVsActual.
type BudgetVsActualRow struct {
	BudgetID        string
	BudgetName      string
	Period          string
	AllocatedAmount float64
	SpentAmount     float64
}

// BudgetVsActualItem is a single budget in the budget-vs-actual response.
type BudgetVsActualItem struct {
	BudgetID        string  `json:"budget_id"`
	BudgetName      string  `json:"budget_name"`
	Period          string  `json:"period"`
	AllocatedAmount float64 `json:"allocated_amount"`
	SpentAmount     float64 `json:"spent_amount"`
	Remaining       float64 `json:"remaining"`
	OverBudget      bool    `json:"over_budget"`
	PctUsed         float64 `json:"pct_used"`
}

// BudgetVsActualResponse is the response for GET /analytics/budget-vs-actual (AI-tool-callable).
type BudgetVsActualResponse struct {
	Budgets        []BudgetVsActualItem `json:"budgets"`
	TotalAllocated float64              `json:"total_allocated"`
	TotalSpent     float64              `json:"total_spent"`
	TotalRemaining float64              `json:"total_remaining"`
	Period         string               `json:"period"`
}
