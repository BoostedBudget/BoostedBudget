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

	// HouseholdScope, when non-nil, aggregates across the household's shared accounts
	// instead of the requesting user's own. nil = personal scope (today's behavior,
	// byte-identical output). Issue #1369 — Phase 5.
	HouseholdScope *HouseholdScope
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
//
// ⚠ start_date and end_date are the range this response's NUMBERS cover, which is the range the
// caller requested, verbatim (final whole-branch review, finding F2). This endpoint fabricates
// nothing — it is one aggregate over rows that exist — and transactions.date is a zoneless,
// effectively institution-local calendar date, so a user ahead of UTC legitimately has rows dated
// later than the UTC day. Clamping the QUERY to today would delete those rows from their own
// total, and clamping only the LABEL is worse: it advertises a window the aggregate did not
// measure. Compare CashFlowHistoryResponse, which DOES clamp, discloses it in
// requested_end_date, and has a fabrication problem to solve that this endpoint does not have.
type CashFlowSummaryResponse struct {
	// StartDate/EndDate stay time.Time — NOT serialized — because they are the SQL bind params the
	// service threads into GetCashFlowAggregation; StartDateDisplay/EndDateDisplay are what
	// actually reach clients (#1631, same defect HoldingSnapshotView.AsOfDate and
	// InvestmentTransaction.DateDisplay already fix for this endpoint's siblings).
	StartDate        time.Time                 `json:"-"`
	StartDateDisplay string                    `json:"start_date" format:"date" doc:"UTC calendar day (YYYY-MM-DD) the aggregate window starts on"`
	EndDate          time.Time                 `json:"-"`
	EndDateDisplay   string                    `json:"end_date" format:"date" doc:"UTC calendar day (YYYY-MM-DD) the aggregate window ends on"`
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
//
// ⚠ THIS ENDPOINT CLAMPS ITS QUERY, AND SAYS SO. Unlike the summary it GAP-FILLS, so an
// unclamped future end fabricates a zero-valued bucket for every period that has not happened
// (#1626 Phase 3, Defect 1 — the user-visible symptom was "Week of Sep 13 / Sep 20 / Sep 27"
// rendered on Sep 9, flat at exactly 0). end_date is therefore the horizon actually queried and
// answered for, and requested_end_date is what the caller asked for. When they differ, the
// difference is the tail this response deliberately does not cover — stated as a field rather
// than left for the caller to infer, so an API or LLM-tool consumer comparing this endpoint with
// cash-flow-summary can see WHY the two windows differ instead of concluding they disagree
// (final whole-branch review, finding F2).
//
// end_date can also exceed the clamp: a period the repository genuinely returned beyond today
// (institution-local dates run ahead of UTC) widens it rather than being contradicted — see
// responseEndDate. So end_date > requested_end_date is impossible, but end_date > today is not.
type CashFlowHistoryResponse struct {
	// StartDate/EndDate stay time.Time — NOT serialized — for the same reason as
	// CashFlowSummaryResponse above; StartDateDisplay/EndDateDisplay are what reach clients
	// (#1631).
	StartDate        time.Time `json:"-"`
	StartDateDisplay string    `json:"start_date" format:"date" doc:"UTC calendar day (YYYY-MM-DD) this history response starts on"`
	EndDate          time.Time `json:"-"`
	EndDateDisplay   string    `json:"end_date" format:"date" doc:"UTC calendar day (YYYY-MM-DD) this history response ends on"`
	// RequestedEndDate is the caller's own end date, echoed unchanged. Equal to EndDate for every
	// request that does not reach past today, which is nearly all of them.
	//
	// Same treatment as the two above. ⚠ This field was MISSED by the mechanical inventory of
	// this defect class, and the reason generalises: that inventory matched struct fields against
	// the DATE columns named in the migrations, and this one echoes the caller's request rather
	// than reading a column, so it had no column name to match. A date-shaped value does not need
	// a DATE column behind it to be serialized wrong. Search by SHAPE, not only by column.
	RequestedEndDate        time.Time            `json:"-"`
	RequestedEndDateDisplay string               `json:"requested_end_date" format:"date" doc:"UTC calendar day (YYYY-MM-DD) the caller asked this history response to end on"`
	Grouping                string               `json:"grouping"` // "daily", "weekly", "monthly"
	Periods                 []CashFlowPeriodData `json:"periods"`
}

// CashFlowPeriodData represents a single period in the cash flow history timeline
type CashFlowPeriodData struct {
	// PeriodStart/PeriodEnd stay time.Time — NOT serialized — for the same reason as
	// CashFlowSummaryResponse above; PeriodStartDisplay/PeriodEndDisplay are what reach clients
	// (#1631).
	PeriodStart        time.Time `json:"-"`
	PeriodStartDisplay string    `json:"period_start" format:"date" doc:"UTC calendar day (YYYY-MM-DD) this period starts on"`
	PeriodEnd          time.Time `json:"-"`
	PeriodEndDisplay   string    `json:"period_end" format:"date" doc:"UTC calendar day (YYYY-MM-DD) this period ends on"`
	Income             float64   `json:"income"`
	Expenses           float64   `json:"expenses"`
	NetCashFlow        float64   `json:"net_cash_flow"`
	TransactionCount   int       `json:"transaction_count"`
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

	// HouseholdScope, when non-nil, aggregates across the household's shared accounts
	// instead of the requesting user's own. nil = personal scope (today's behavior,
	// byte-identical output). CustomCategoryIDs always resolve against the requesting
	// user's own categories, never a household member's — namespace-safe by design.
	// Issue #1369 — Phase 5.
	HouseholdScope *HouseholdScope
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
	StartDate  string               `json:"start_date" format:"date"`
	EndDate    string               `json:"end_date" format:"date"`
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
	StartDate string               `json:"start_date" format:"date"`
	EndDate   string               `json:"end_date" format:"date"`
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
	PeriodStart string             `json:"period_start" format:"date"`
	PeriodEnd   string             `json:"period_end" format:"date"`
	Categories  []CategorySpending `json:"categories"`
	Total       float64            `json:"total"`
}

// SpendingTrendsResponse is the response for GET /analytics/spending-trends
type SpendingTrendsResponse struct {
	StartDate     string                `json:"start_date" format:"date"`
	EndDate       string                `json:"end_date" format:"date"`
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
	// CategoryDisplay carries effective_category_name so a custom category resolves to its own
	// name rather than a title-cased UUID (#1626). Internal; the wire field stays PFCPrimaryDisplay.
	CategoryDisplay string
}

// CategoryTotalResponse is the response for GET /analytics/category-total (AI-tool-callable)
type CategoryTotalResponse struct {
	Category          string  `json:"category"`
	PFCPrimaryDisplay string  `json:"pfc_primary_display"`
	Total             float64 `json:"total"`
	TransactionCount  int     `json:"transaction_count"`
	StartDate         string  `json:"start_date" format:"date"`
	EndDate           string  `json:"end_date" format:"date"`
}

// TopExpenseRow is a raw database row for GetTopExpense
type TopExpenseRow struct {
	TransactionID string
	MerchantName  string
	Amount        float64
	Category      string
	Date          time.Time
	// CategoryDisplay carries effective_category_name so a custom category resolves to its own
	// name rather than a title-cased UUID (#1626). Internal; the wire field stays PFCPrimaryDisplay.
	CategoryDisplay string
}

// TopExpenseResponse is the response for GET /analytics/top-expense (AI-tool-callable)
// Returns the single largest expense transaction in a date range (nil if no expenses).
type TopExpenseResponse struct {
	TransactionID     string  `json:"transaction_id"`
	MerchantName      string  `json:"merchant_name"`
	Amount            float64 `json:"amount"`
	Category          string  `json:"category"`
	PFCPrimaryDisplay string  `json:"pfc_primary_display"`
	Date              string  `json:"date" format:"date"`
	StartDate         string  `json:"start_date" format:"date"`
	EndDate           string  `json:"end_date" format:"date"`
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
	StartDate           string  `json:"start_date" format:"date"`
	EndDate             string  `json:"end_date" format:"date"`
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
	LastChargeDate   string  `json:"last_charge_date" format:"date"`
	NextExpectedDate string  `json:"next_expected_date" format:"date"`
}

// RecurringSubscriptionsResponse is the response for GET /analytics/recurring-subscriptions.
// AI-tool-callable: heuristic detection of recurring charges (monthly cadence). Issue #1215.
type RecurringSubscriptionsResponse struct {
	Subscriptions []RecurringSubscriptionItem `json:"subscriptions"`
	AsOfDate      string                      `json:"as_of_date" format:"date"`
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
	StartDate   string   `json:"start_date" format:"date"`
	EndDate     string   `json:"end_date" format:"date"`
}

// MonthOverMonthBucketRow is a raw DB row for GetMonthOverMonthSpending.
type MonthOverMonthBucketRow struct {
	Month time.Time
	Total float64
}

// MonthOverMonthBucket is a single month in the month-over-month response.
type MonthOverMonthBucket struct {
	Month     string   `json:"month"` // "2024-01-01" (first day of month)
	Total     float64  `json:"total"`
	ChangePct *float64 `json:"change_pct" doc:"Percentage change vs the previous period (12.5 means +12.5%)."` // nil for first bucket
	ChangeAbs *float64 `json:"change_abs"`                                                                     // nil for first bucket
}

// MonthOverMonthResponse is the response for GET /analytics/month-over-month (AI-tool-callable).
type MonthOverMonthResponse struct {
	Buckets    []MonthOverMonthBucket `json:"buckets"`
	Category   string                 `json:"category"` // "" = all spending
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
	// CategoryDisplay carries effective_category_name so a custom category resolves to its own
	// name rather than a title-cased UUID (#1626). Internal; the wire field stays PFCPrimaryDisplay.
	CategoryDisplay string
}

// LargestTransactionItem is a single transaction in the largest-transactions response.
type LargestTransactionItem struct {
	TransactionID     string  `json:"transaction_id"`
	MerchantName      string  `json:"merchant_name"`
	Amount            float64 `json:"amount"`
	Category          string  `json:"category"`
	PFCPrimaryDisplay string  `json:"pfc_primary_display"`
	Date              string  `json:"date" format:"date"`
	AccountID         string  `json:"account_id"`
}

// LargestTransactionsResponse is the response for GET /analytics/largest-transactions.
type LargestTransactionsResponse struct {
	Transactions []LargestTransactionItem `json:"transactions"`
	Limit        int                      `json:"limit"`
	StartDate    string                   `json:"start_date" format:"date"`
	EndDate      string                   `json:"end_date" format:"date"`
}

// CategoryBreakdownForMerchantRow is a raw DB row for GetCategoryBreakdownForMerchant.
type CategoryBreakdownForMerchantRow struct {
	Category string
	Total    float64
	Count    int
	// See CategoryTotalRow.CategoryDisplay (#1626).
	CategoryDisplay string
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
	StartDate    string                  `json:"start_date" format:"date"`
	EndDate      string                  `json:"end_date" format:"date"`
}

// IncomeSourceRow is a raw DB row for GetIncomeSourceBreakdown.
//
// CategoryDisplay carries t.effective_category_name — the custom category's own name for a
// custom-kind row, the raw PFC enum for a PFC-kind row. It is INTERNAL: the wire contract keeps
// exposing the resolved label through IncomeSourceItem.PFCPrimaryDisplay, so adding this field
// changes no OpenAPI schema and no generated client (#1626).
//
// The column is nullable and app-maintained asynchronously, so it is scanned through a nullable
// type and normalised to "" — never assume it is populated.
type IncomeSourceRow struct {
	Category        string
	CategoryDisplay string
	Total           float64
	Count           int
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
	StartDate   string             `json:"start_date" format:"date"`
	EndDate     string             `json:"end_date" format:"date"`
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
	// See CategoryTotalRow.CategoryDisplay (#1626).
	CategoryDisplay string
}

// UnusualSpendItem is a single unusual spend transaction in the response.
type UnusualSpendItem struct {
	TransactionID     string  `json:"transaction_id"`
	MerchantName      string  `json:"merchant_name"`
	Amount            float64 `json:"amount"`
	Category          string  `json:"category"`
	PFCPrimaryDisplay string  `json:"pfc_primary_display"`
	Date              string  `json:"date" format:"date"`
	BaselineAvg       float64 `json:"baseline_avg"`
	SigmaAbove        float64 `json:"sigma_above"`
}

// UnusualSpendResponse is the response for GET /analytics/unusual-spend (AI-tool-callable).
type UnusualSpendResponse struct {
	Items          []UnusualSpendItem `json:"items"`
	StartDate      string             `json:"start_date" format:"date"`
	EndDate        string             `json:"end_date" format:"date"`
	SigmaThreshold float64            `json:"sigma_threshold"`
}

// NetWorthBucket represents one time period in the net worth trend.
type NetWorthBucket struct {
	Date      string   `json:"date" format:"date"` // first day of bucket period (YYYY-MM-DD)
	NetWorth  float64  `json:"net_worth"`
	ChangeAbs *float64 `json:"change_abs"`                                                                     // nil for first bucket
	ChangePct *float64 `json:"change_pct" doc:"Percentage change vs the previous period (12.5 means +12.5%)."` // nil for first bucket; nil if prev=0
}

// NetWorthTrendResponse is the response for GET /analytics/net-worth-trend (AI-tool-callable).
type NetWorthTrendResponse struct {
	Buckets   []NetWorthBucket `json:"buckets"`
	Grouping  string           `json:"grouping"`
	StartDate string           `json:"start_date" format:"date"`
	EndDate   string           `json:"end_date" format:"date"`
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

// ─── Issue #1215 PR4: composite financial summary ──────────────────────────

// FinancialSummaryResponse is the response for GET /analytics/financial-summary.
// It is a parallel fan-out of all key analytics metrics for a given date range.
// Sub-metrics are best-effort: individual failures are logged and the field is
// omitted (nil) rather than propagating a 500. Designed as an AI-tool-callable
// single round-trip for "how am I doing financially?" questions.
type FinancialSummaryResponse struct {
	SavingsRate    *SavingsRateResponse         `json:"savings_rate,omitempty"`
	AverageSpend   *AverageMonthlySpendResponse `json:"average_monthly_spend,omitempty"`
	TopCategories  *SpendingByCategoryResponse  `json:"top_categories,omitempty"`
	MomChange      *MonthOverMonthResponse      `json:"mom_spending_change,omitempty"`
	BudgetVsActual *BudgetVsActualResponse      `json:"budget_vs_actual,omitempty"`
	NetWorthTrend  *NetWorthTrendResponse       `json:"net_worth_trend,omitempty"`
	GeneratedAt    string                       `json:"generated_at"`
}

// ─── Category hierarchy (#1626) ──────────────────────────────────────────────────────────────────

// CategoryDirection selects which side of the ledger a hierarchy read aggregates. The sign
// convention is the repository's: income is amount < 0, expense is amount > 0.
type CategoryDirection string

const (
	CategoryDirectionExpense CategoryDirection = "expense"
	CategoryDirectionIncome  CategoryDirection = "income"
)

// IsValid reports whether d names a direction the repository can serve.
func (d CategoryDirection) IsValid() bool {
	return d == CategoryDirectionExpense || d == CategoryDirectionIncome
}

// CategoryHierarchyRow is a raw DB row for GetCategoryHierarchy: one grouped leaf, carrying the
// whole ancestor path that leads to it. L2/L3 are empty where the branch is shallower — the tree is
// ragged, and a branch simply ends where its data ends.
//
// Display fields carry CUSTOM category names only. A PFC key has no name in the database — the
// denormalized column holds the raw enum — so the service resolves those from the pfc registry.
type CategoryHierarchyRow struct {
	L1Key, L1Display string
	L2Key, L2Display string
	L3Key, L3Display string
	Total            float64
	TransactionCount int
}

// CategoryHierarchyNode is one node of the tree the API returns.
//
// Total is the ROLL-UP: this node plus everything beneath it. DirectTotal is the part belonging to
// this node's own transactions, i.e. Total minus the sum of the children's Totals. A node can have
// both — a PFC primary with rows carrying no detailed value, or a custom parent used directly — and
// without DirectTotal a renderer cannot make the flows into and out of that node balance.
type CategoryHierarchyNode struct {
	Category         string                  `json:"category"`
	Display          string                  `json:"display"`
	Total            float64                 `json:"total"`
	DirectTotal      float64                 `json:"direct_total"`
	TransactionCount int                     `json:"transaction_count"`
	Percentage       float64                 `json:"percentage"`
	Children         []CategoryHierarchyNode `json:"children,omitempty"`
}

// CategoryHierarchyResponse is the response for GET /analytics/category-hierarchy.
type CategoryHierarchyResponse struct {
	Levels    []CategoryHierarchyNode `json:"levels"`
	Total     float64                 `json:"total"`
	Direction string                  `json:"direction"`
	StartDate string                  `json:"start_date" format:"date"`
	EndDate   string                  `json:"end_date" format:"date"`
}
