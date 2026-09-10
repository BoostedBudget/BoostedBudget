package operations

import (
	"context"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/maxnussbaum/BoostedBudget/internal/dateutil"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	humaerrors "github.com/maxnussbaum/BoostedBudget/internal/huma/errors"
	"github.com/maxnussbaum/BoostedBudget/internal/huma/inputs"
	"github.com/maxnussbaum/BoostedBudget/internal/services"
	"golang.org/x/sync/errgroup"
)

// AnalyticsHandler handles analytics-related HTTP requests.
// Issue #354: Cash Flow Analysis & Income/Expense Visualization
type AnalyticsHandler struct {
	service      services.AnalyticsService
	householdSvc services.HouseholdService
}

// NewAnalyticsHandler creates a new AnalyticsHandler instance. householdSvc resolves
// `scope=household` requests into a validated domain.HouseholdScope (Issue #1369 —
// Phase 5); it must never be nil in production wiring since every scoped endpoint below
// depends on it.
func NewAnalyticsHandler(service services.AnalyticsService, householdSvc services.HouseholdService) *AnalyticsHandler {
	return &AnalyticsHandler{service: service, householdSvc: householdSvc}
}

// --- Input/Output Types ---

// GetCashFlowSummaryInput represents the input for getting cash flow summary.
type GetCashFlowSummaryInput struct {
	inputs.AuthenticatedInput
	StartDate        string `query:"start_date" required:"true" doc:"Start of date range (YYYY-MM-DD)"`
	EndDate          string `query:"end_date" required:"true" doc:"End of date range (YYYY-MM-DD)"`
	AccountTypes     string `query:"account_types" doc:"Comma-separated account types to filter"`
	IncludeTransfers string `query:"include_transfers" doc:"Include transfer transactions (true/false, default false)"`
	Scope            string `query:"scope" enum:"personal,household" default:"personal" doc:"Aggregation scope: personal (default) or household (requires active household membership)"`
}

// GetCashFlowSummaryOutput represents the response for getting cash flow summary.
type GetCashFlowSummaryOutput struct {
	Body *domain.CashFlowSummaryResponse
}

// GetCashFlowHistoryInput represents the input for getting cash flow history.
type GetCashFlowHistoryInput struct {
	inputs.AuthenticatedInput
	StartDate        string `query:"start_date" required:"true" doc:"Start of date range (YYYY-MM-DD)"`
	EndDate          string `query:"end_date" required:"true" doc:"End of date range (YYYY-MM-DD)"`
	Grouping         string `query:"grouping" doc:"Time grouping: daily, weekly, or monthly (auto-selected if not provided)"`
	AccountTypes     string `query:"account_types" doc:"Comma-separated account types to filter"`
	IncludeTransfers string `query:"include_transfers" doc:"Include transfer transactions (true/false, default false)"`
	Scope            string `query:"scope" enum:"personal,household" default:"personal" doc:"Aggregation scope: personal (default) or household (requires active household membership)"`
}

// GetCashFlowHistoryOutput represents the response for getting cash flow history.
type GetCashFlowHistoryOutput struct {
	Body *domain.CashFlowHistoryResponse
}

// --- Helper Functions ---

// parseCashFlowFilters parses query parameters into CashFlowFilters.
func parseCashFlowFilters(startDateStr, endDateStr, accountTypes, includeTransfers, grouping string) (*domain.CashFlowFilters, error) {
	filters := &domain.CashFlowFilters{}

	startDate, endDate, err := dateutil.ParseDateRange(startDateStr, endDateStr)
	if err != nil {
		return nil, err
	}
	filters.StartDate = startDate
	filters.EndDate = endDate
	filters.AccountTypes = parseAccountTypes(accountTypes)

	includeTransfersVal, err := parseBoolString(includeTransfers)
	if err != nil {
		return nil, err
	}
	filters.IncludeTransfers = includeTransfersVal

	// Parse grouping (optional)
	if grouping != "" {
		validGroupings := map[string]bool{"daily": true, "weekly": true, "monthly": true}
		if !validGroupings[grouping] {
			return nil, huma.Error400BadRequest("Invalid grouping. Must be one of: daily, weekly, monthly")
		}
		filters.Grouping = grouping
	}

	return filters, nil
}

// GetSpendingByCategoryInput represents the input for the spending-by-category endpoint.
type GetSpendingByCategoryInput struct {
	inputs.AuthenticatedInput
	StartDate         string `query:"start_date" required:"true" doc:"Start of date range (YYYY-MM-DD)"`
	EndDate           string `query:"end_date" required:"true" doc:"End of date range (YYYY-MM-DD)"`
	AccountTypes      string `query:"account_types" doc:"Comma-separated account types to filter"`
	IncludeTransfers  string `query:"include_transfers" doc:"Include transfer transactions (true/false, default false)"`
	Category          string `query:"category" doc:"Primary category to drill into subcategories"`
	CustomCategoryIDs string `query:"custom_category_ids" doc:"Comma-separated custom category UUIDs to scope the report (Issue #1203)"`
	RollUp            string `query:"roll_up" doc:"When true, a parent custom category also includes its children (default true)"`
	Scope             string `query:"scope" enum:"personal,household" default:"personal" doc:"Aggregation scope: personal (default) or household (requires active household membership). Custom category scoping always resolves to the requester's own categories, never a household member's."`
}

// GetSpendingByCategoryOutput represents the response for spending-by-category.
type GetSpendingByCategoryOutput struct {
	Body *domain.SpendingByCategoryResponse
}

// GetCategoryHierarchyInput represents the input for the category-hierarchy endpoint (#1626).
//
// Deliberately does NOT carry `category`: that param means "drill into exactly one primary", which
// is meaningless for a read that returns the whole tree.
type GetCategoryHierarchyInput struct {
	inputs.AuthenticatedInput
	StartDate         string `query:"start_date" required:"true" doc:"Start of date range (YYYY-MM-DD)"`
	EndDate           string `query:"end_date" required:"true" doc:"End of date range (YYYY-MM-DD)"`
	Direction         string `query:"direction" enum:"income,expense" default:"expense" doc:"Which side of the ledger to aggregate: income is amount < 0, expense is amount > 0. Both directions return the SAME full tree — this parameter selects which transactions are aggregated, not how deep the result goes. How many levels to SHOW is a presentation choice: the reports Sankey renders income at leaf depth, because income's useful distinction (wages, dividends, rental) lives one level down, and expenses at primary depth, because they read better consolidated."`
	AccountTypes      string `query:"account_types" doc:"Comma-separated account types to filter"`
	IncludeTransfers  string `query:"include_transfers" doc:"Include transfer transactions (true/false, default false)"`
	CustomCategoryIDs string `query:"custom_category_ids" doc:"Comma-separated custom category UUIDs to scope the report. NOTE: supplying any value narrows the WHOLE tree to custom-categorised transactions, so no PFC branches are returned — that is what scoping a report to specific custom categories means, and it matches the reports page's existing custom-category filter."`
	RollUp            string `query:"roll_up" doc:"When true, a parent custom category also includes its children (default true)"`
	Scope             string `query:"scope" enum:"personal,household" default:"personal" doc:"Aggregation scope: personal (default) or household (requires active household membership). Custom category scoping always resolves to the requester's own categories, never a household member's."`
}

// GetCategoryHierarchyOutput represents the response for the category-hierarchy endpoint.
type GetCategoryHierarchyOutput struct {
	Body *domain.CategoryHierarchyResponse
}

// GetSpendingByMerchantInput represents the input for the spending-by-merchant endpoint.
type GetSpendingByMerchantInput struct {
	inputs.AuthenticatedInput
	StartDate        string `query:"start_date" required:"true" doc:"Start of date range (YYYY-MM-DD)"`
	EndDate          string `query:"end_date" required:"true" doc:"End of date range (YYYY-MM-DD)"`
	AccountTypes     string `query:"account_types" doc:"Comma-separated account types to filter"`
	IncludeTransfers string `query:"include_transfers" doc:"Include transfer transactions (true/false, default false)"`
	Limit            int    `query:"limit" doc:"Maximum number of merchants to return (default 10)" default:"10"`
	Scope            string `query:"scope" enum:"personal,household" default:"personal" doc:"Aggregation scope: personal (default) or household (requires active household membership)"`
}

// GetSpendingByMerchantOutput represents the response for spending-by-merchant.
type GetSpendingByMerchantOutput struct {
	Body *domain.SpendingByMerchantResponse
}

// GetSpendingTrendsInput represents the input for the spending-trends endpoint.
type GetSpendingTrendsInput struct {
	inputs.AuthenticatedInput
	StartDate        string `query:"start_date" required:"true" doc:"Start of date range (YYYY-MM-DD)"`
	EndDate          string `query:"end_date" required:"true" doc:"End of date range (YYYY-MM-DD)"`
	AccountTypes     string `query:"account_types" doc:"Comma-separated account types to filter"`
	IncludeTransfers string `query:"include_transfers" doc:"Include transfer transactions (true/false, default false)"`
	Scope            string `query:"scope" enum:"personal,household" default:"personal" doc:"Aggregation scope: personal (default) or household (requires active household membership)"`
}

// GetSpendingTrendsOutput represents the response for spending-trends.
type GetSpendingTrendsOutput struct {
	Body *domain.SpendingTrendsResponse
}

// parseBoolString parses string "true"/"false" to bool, returning false if empty
func parseBoolString(s string) (bool, error) {
	if s == "" {
		return false, nil
	}
	if s == "true" {
		return true, nil
	}
	if s == "false" {
		return false, nil
	}
	return false, huma.Error400BadRequest("Invalid boolean value. Use true or false")
}

// parseAccountTypes parses comma-separated account types into a slice
func parseAccountTypes(s string) []string {
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	result := make([]string, 0, len(parts))
	for _, p := range parts {
		if trimmed := strings.TrimSpace(p); trimmed != "" {
			result = append(result, trimmed)
		}
	}
	return result
}

// parseSpendingFilters parses query parameters into SpendingFilters.
func parseSpendingFilters(startDateStr, endDateStr, accountTypes, includeTransfers string) (*domain.SpendingFilters, error) {
	filters := &domain.SpendingFilters{}

	startDate, endDate, err := dateutil.ParseDateRange(startDateStr, endDateStr)
	if err != nil {
		return nil, err
	}
	filters.StartDate = startDate
	filters.EndDate = endDate
	filters.AccountTypes = parseAccountTypes(accountTypes)

	includeTransfersVal, err := parseBoolString(includeTransfers)
	if err != nil {
		return nil, err
	}
	filters.IncludeTransfers = includeTransfersVal

	return filters, nil
}

// --- Helper Functions ---

// GetCashFlowSummary handles GET /api/v1/analytics/cash-flow-summary requests.
// Returns aggregated cash flow data (income, expenses, net) for a date range.
func (h *AnalyticsHandler) GetCashFlowSummary(ctx context.Context, input *GetCashFlowSummaryInput) (*GetCashFlowSummaryOutput, error) {
	filters, err := parseCashFlowFilters(input.StartDate, input.EndDate, input.AccountTypes, input.IncludeTransfers, "")
	if err != nil {
		return nil, err
	}
	if filters.HouseholdScope, err = resolveHouseholdScopeIfRequested(ctx, h.householdSvc, input.UserID, input.Scope); err != nil {
		return nil, err
	}

	response, err := h.service.GetCashFlowSummary(ctx, input.UserID, *filters)
	if err != nil {
		return nil, humaerrors.LoggingMapError(ctx, err)
	}

	return &GetCashFlowSummaryOutput{Body: response}, nil
}

// GetCashFlowHistory handles GET /api/v1/analytics/cash-flow-history requests.
// Returns time series cash flow data grouped by period for charts.
func (h *AnalyticsHandler) GetCashFlowHistory(ctx context.Context, input *GetCashFlowHistoryInput) (*GetCashFlowHistoryOutput, error) {
	filters, err := parseCashFlowFilters(input.StartDate, input.EndDate, input.AccountTypes, input.IncludeTransfers, input.Grouping)
	if err != nil {
		return nil, err
	}
	if filters.HouseholdScope, err = resolveHouseholdScopeIfRequested(ctx, h.householdSvc, input.UserID, input.Scope); err != nil {
		return nil, err
	}

	response, err := h.service.GetCashFlowHistory(ctx, input.UserID, *filters)
	if err != nil {
		return nil, humaerrors.LoggingMapError(ctx, err)
	}

	return &GetCashFlowHistoryOutput{Body: response}, nil
}

// GetSpendingByCategory handles GET /api/v1/analytics/spending-by-category requests.
// Returns spending totals grouped by category with percentages and display names.
// When category param is provided, returns subcategory breakdown for that category.
func (h *AnalyticsHandler) GetSpendingByCategory(ctx context.Context, input *GetSpendingByCategoryInput) (*GetSpendingByCategoryOutput, error) {
	filters, err := parseSpendingFilters(input.StartDate, input.EndDate, input.AccountTypes, input.IncludeTransfers)
	if err != nil {
		return nil, err
	}
	if filters.HouseholdScope, err = resolveHouseholdScopeIfRequested(ctx, h.householdSvc, input.UserID, input.Scope); err != nil {
		return nil, err
	}
	filters.Category = input.Category

	// Issue #1203: optional custom-category scoping. Roll-up defaults to true so a
	// parent custom category includes its children unless roll_up=false.
	if input.CustomCategoryIDs != "" {
		for _, p := range strings.Split(input.CustomCategoryIDs, ",") {
			if trimmed := strings.TrimSpace(p); trimmed != "" {
				filters.CustomCategoryIDs = append(filters.CustomCategoryIDs, trimmed)
			}
		}
	}
	filters.RollUpChildren = input.RollUp != "false"

	response, err := h.service.GetSpendingByCategory(ctx, input.UserID, *filters)
	if err != nil {
		return nil, humaerrors.LoggingMapError(ctx, err)
	}

	return &GetSpendingByCategoryOutput{Body: response}, nil
}

// validateLimit ensures limit is between 1 and maxLimit, defaulting to defaultLimit if invalid
func validateLimit(limit, defaultLimit, maxLimit int) int {
	if limit <= 0 {
		return defaultLimit
	}
	if limit > maxLimit {
		return maxLimit
	}
	return limit
}

// GetCategoryHierarchy handles GET /api/v1/analytics/category-hierarchy requests.
// Returns the category tree (up to three levels) for one flow direction, with roll-up totals and
// each node's own direct spend.
func (h *AnalyticsHandler) GetCategoryHierarchy(ctx context.Context, input *GetCategoryHierarchyInput) (*GetCategoryHierarchyOutput, error) {
	filters, err := parseSpendingFilters(input.StartDate, input.EndDate, input.AccountTypes, input.IncludeTransfers)
	if err != nil {
		return nil, err
	}
	if filters.HouseholdScope, err = resolveHouseholdScopeIfRequested(ctx, h.householdSvc, input.UserID, input.Scope); err != nil {
		return nil, err
	}

	if input.CustomCategoryIDs != "" {
		for _, p := range strings.Split(input.CustomCategoryIDs, ",") {
			if trimmed := strings.TrimSpace(p); trimmed != "" {
				filters.CustomCategoryIDs = append(filters.CustomCategoryIDs, trimmed)
			}
		}
	}
	filters.RollUpChildren = input.RollUp != "false"

	direction := domain.CategoryDirection(input.Direction)
	if !direction.IsValid() {
		direction = domain.CategoryDirectionExpense
	}

	response, err := h.service.GetCategoryHierarchy(ctx, input.UserID, *filters, direction)
	if err != nil {
		return nil, humaerrors.LoggingMapError(ctx, err)
	}
	return &GetCategoryHierarchyOutput{Body: response}, nil
}

// GetSpendingByMerchant handles GET /api/v1/analytics/spending-by-merchant requests.
// Returns top merchants by spending with percentages.
func (h *AnalyticsHandler) GetSpendingByMerchant(ctx context.Context, input *GetSpendingByMerchantInput) (*GetSpendingByMerchantOutput, error) {
	filters, err := parseSpendingFilters(input.StartDate, input.EndDate, input.AccountTypes, input.IncludeTransfers)
	if err != nil {
		return nil, err
	}
	if filters.HouseholdScope, err = resolveHouseholdScopeIfRequested(ctx, h.householdSvc, input.UserID, input.Scope); err != nil {
		return nil, err
	}

	limit := validateLimit(input.Limit, 10, 1000)

	response, err := h.service.GetSpendingByMerchant(ctx, input.UserID, *filters, limit)
	if err != nil {
		return nil, humaerrors.LoggingMapError(ctx, err)
	}

	return &GetSpendingByMerchantOutput{Body: response}, nil
}

// GetSpendingTrends handles GET /api/v1/analytics/spending-trends requests.
// Returns monthly category spending trends for stacked bar charts.
func (h *AnalyticsHandler) GetSpendingTrends(ctx context.Context, input *GetSpendingTrendsInput) (*GetSpendingTrendsOutput, error) {
	filters, err := parseSpendingFilters(input.StartDate, input.EndDate, input.AccountTypes, input.IncludeTransfers)
	if err != nil {
		return nil, err
	}
	if filters.HouseholdScope, err = resolveHouseholdScopeIfRequested(ctx, h.householdSvc, input.UserID, input.Scope); err != nil {
		return nil, err
	}

	response, err := h.service.GetSpendingTrends(ctx, input.UserID, *filters)
	if err != nil {
		return nil, humaerrors.LoggingMapError(ctx, err)
	}

	return &GetSpendingTrendsOutput{Body: response}, nil
}

// GetAverageMonthlySpendInput represents input for GET /analytics/average-monthly-spend.
type GetAverageMonthlySpendInput struct {
	inputs.AuthenticatedInput
	StartDate        string `query:"start_date" required:"true" doc:"Start of date range (YYYY-MM-DD)"`
	EndDate          string `query:"end_date" required:"true" doc:"End of date range (YYYY-MM-DD)"`
	AccountTypes     string `query:"account_types" doc:"Comma-separated account types to filter (e.g. depository,credit)"`
	IncludeTransfers string `query:"include_transfers" doc:"Include transfer transactions (true/false, default false)"`
}

// GetAverageMonthlySpendOutput represents the response for GET /analytics/average-monthly-spend.
type GetAverageMonthlySpendOutput struct {
	Body *domain.AverageMonthlySpendResponse
}

// GetAverageMonthlySpend handles GET /api/v1/analytics/average-monthly-spend requests.
func (h *AnalyticsHandler) GetAverageMonthlySpend(ctx context.Context, input *GetAverageMonthlySpendInput) (*GetAverageMonthlySpendOutput, error) {
	filters, err := parseSpendingFilters(input.StartDate, input.EndDate, input.AccountTypes, input.IncludeTransfers)
	if err != nil {
		return nil, err
	}

	response, err := h.service.GetAverageMonthlySpend(ctx, input.UserID, *filters)
	if err != nil {
		return nil, humaerrors.LoggingMapError(ctx, err)
	}

	return &GetAverageMonthlySpendOutput{Body: response}, nil
}

// GetRecurringSubscriptionsInput represents input for GET /analytics/recurring-subscriptions.
type GetRecurringSubscriptionsInput struct {
	inputs.AuthenticatedInput
}

// GetRecurringSubscriptionsOutput represents the response for GET /analytics/recurring-subscriptions.
type GetRecurringSubscriptionsOutput struct {
	Body *domain.RecurringSubscriptionsResponse
}

// GetRecurringSubscriptions handles GET /api/v1/analytics/recurring-subscriptions requests.
func (h *AnalyticsHandler) GetRecurringSubscriptions(ctx context.Context, input *GetRecurringSubscriptionsInput) (*GetRecurringSubscriptionsOutput, error) {
	response, err := h.service.GetRecurringSubscriptions(ctx, input.UserID)
	if err != nil {
		return nil, humaerrors.LoggingMapError(ctx, err)
	}
	return &GetRecurringSubscriptionsOutput{Body: response}, nil
}

// GetCategoryTotalForPeriodInput represents input for GET /analytics/category-total.
type GetCategoryTotalForPeriodInput struct {
	inputs.AuthenticatedInput
	StartDate string `query:"start_date" required:"true" doc:"Start of date range (YYYY-MM-DD)"`
	EndDate   string `query:"end_date" required:"true" doc:"End of date range (YYYY-MM-DD)"`
	Category  string `query:"category" required:"true" doc:"Category identifier (PFC primary or custom:<uuid>)"`
}

// GetCategoryTotalForPeriodOutput represents the response for GET /analytics/category-total.
type GetCategoryTotalForPeriodOutput struct {
	Body *domain.CategoryTotalResponse
}

// GetTopExpenseInput represents input for GET /analytics/top-expense.
type GetTopExpenseInput struct {
	inputs.AuthenticatedInput
	StartDate string `query:"start_date" required:"true" doc:"Start of date range (YYYY-MM-DD)"`
	EndDate   string `query:"end_date" required:"true" doc:"End of date range (YYYY-MM-DD)"`
	Scope     string `query:"scope" enum:"personal,household" doc:"Must be omitted or 'personal' — this endpoint exposes a raw transaction ID and does not support household scope (Issue #1369 decision #5)"`
}

// GetTopExpenseOutput represents the response for GET /analytics/top-expense.
type GetTopExpenseOutput struct {
	Body *domain.TopExpenseResponse
}

// GetCategoryTotalForPeriod handles GET /api/v1/analytics/category-total requests.
func (h *AnalyticsHandler) GetCategoryTotalForPeriod(ctx context.Context, input *GetCategoryTotalForPeriodInput) (*GetCategoryTotalForPeriodOutput, error) {
	startDate, endDate, err := dateutil.ParseDateRange(input.StartDate, input.EndDate)
	if err != nil {
		return nil, err
	}

	response, err := h.service.GetCategoryTotalForPeriod(ctx, input.UserID, input.Category, startDate, endDate)
	if err != nil {
		return nil, humaerrors.LoggingMapError(ctx, err)
	}

	return &GetCategoryTotalForPeriodOutput{Body: response}, nil
}

// GetTopExpense handles GET /api/v1/analytics/top-expense requests.
func (h *AnalyticsHandler) GetTopExpense(ctx context.Context, input *GetTopExpenseInput) (*GetTopExpenseOutput, error) {
	if err := rejectHouseholdScope(input.Scope); err != nil {
		return nil, err
	}
	startDate, endDate, err := dateutil.ParseDateRange(input.StartDate, input.EndDate)
	if err != nil {
		return nil, err
	}

	response, err := h.service.GetTopExpense(ctx, input.UserID, startDate, endDate)
	if err != nil {
		return nil, humaerrors.LoggingMapError(ctx, err)
	}

	return &GetTopExpenseOutput{Body: response}, nil
}

// GetSavingsRateForPeriodInput represents input for GET /analytics/savings-rate.
type GetSavingsRateForPeriodInput struct {
	inputs.AuthenticatedInput
	StartDate string `query:"start_date" required:"true" doc:"Start of date range (YYYY-MM-DD)"`
	EndDate   string `query:"end_date" required:"true" doc:"End of date range (YYYY-MM-DD)"`
}

// GetSavingsRateForPeriodOutput represents the response for GET /analytics/savings-rate.
type GetSavingsRateForPeriodOutput struct {
	Body *domain.SavingsRateResponse
}

// GetSavingsRateForPeriod handles GET /api/v1/analytics/savings-rate requests.
func (h *AnalyticsHandler) GetSavingsRateForPeriod(ctx context.Context, input *GetSavingsRateForPeriodInput) (*GetSavingsRateForPeriodOutput, error) {
	startDate, endDate, err := dateutil.ParseDateRange(input.StartDate, input.EndDate)
	if err != nil {
		return nil, err
	}

	// Standalone endpoint — not in the household-scoped set (Issue #1369 decision #5);
	// always personal. Household scope is threaded only through GetFinancialSummary's
	// internal fan-out call to this same service method.
	response, err := h.service.GetSavingsRateForPeriod(ctx, input.UserID, startDate, endDate, nil)
	if err != nil {
		return nil, humaerrors.LoggingMapError(ctx, err)
	}

	return &GetSavingsRateForPeriodOutput{Body: response}, nil
}

// GetMonthOverMonthSpendingChangeInput represents input for GET /analytics/month-over-month.
type GetMonthOverMonthSpendingChangeInput struct {
	inputs.AuthenticatedInput
	Category         string `query:"category" doc:"Optional PFC primary or custom:<uuid> to scope spending"`
	Months           int    `query:"months" doc:"Months to look back (2-24, default 6)" default:"6"`
	AccountTypes     string `query:"account_types" doc:"Comma-separated account types to filter (e.g. depository,credit)"`
	IncludeTransfers string `query:"include_transfers" doc:"Include transfer transactions (true/false, default false)"`
}

// GetMonthOverMonthSpendingChangeOutput represents the response for GET /analytics/month-over-month.
type GetMonthOverMonthSpendingChangeOutput struct {
	Body *domain.MonthOverMonthResponse
}

// GetMonthOverMonthSpendingChange handles GET /api/v1/analytics/month-over-month requests.
func (h *AnalyticsHandler) GetMonthOverMonthSpendingChange(ctx context.Context, input *GetMonthOverMonthSpendingChangeInput) (*GetMonthOverMonthSpendingChangeOutput, error) {
	months := input.Months
	// Huma pre-fills default:"6" for omitted params; no zero-guard needed.
	// Explicit ?months=0 falls through to the range check and returns 400.
	if months < 2 || months > 24 {
		return nil, huma.Error400BadRequest("months must be between 2 and 24")
	}

	accountTypes := parseAccountTypes(input.AccountTypes)
	includeTransfers, err := parseBoolString(input.IncludeTransfers)
	if err != nil {
		return nil, err
	}

	// Standalone endpoint — not in the household-scoped set (Issue #1369 decision #5);
	// always personal. Household scope is threaded only through GetFinancialSummary's
	// internal fan-out call to this same service method.
	response, err := h.service.GetMonthOverMonthSpendingChange(ctx, input.UserID, input.Category, months, accountTypes, includeTransfers, nil)
	if err != nil {
		return nil, humaerrors.LoggingMapError(ctx, err)
	}

	return &GetMonthOverMonthSpendingChangeOutput{Body: response}, nil
}

// GetLargestTransactionsForPeriodInput represents input for GET /analytics/largest-transactions.
type GetLargestTransactionsForPeriodInput struct {
	inputs.AuthenticatedInput
	StartDate        string `query:"start_date" required:"true" doc:"Start of date range (YYYY-MM-DD)"`
	EndDate          string `query:"end_date" required:"true" doc:"End of date range (YYYY-MM-DD)"`
	AccountTypes     string `query:"account_types" doc:"Comma-separated account types to filter"`
	IncludeTransfers string `query:"include_transfers" doc:"Include transfer transactions (true/false, default false)"`
	Limit            int    `query:"limit" doc:"Maximum number of transactions to return (1-100, default 10)" default:"10"`
	Scope            string `query:"scope" enum:"personal,household" doc:"Must be omitted or 'personal' — this endpoint exposes raw transaction/account IDs and does not support household scope (Issue #1369 decision #5)"`
}

// GetLargestTransactionsForPeriodOutput represents the response for GET /analytics/largest-transactions.
type GetLargestTransactionsForPeriodOutput struct {
	Body *domain.LargestTransactionsResponse
}

// GetLargestTransactionsForPeriod handles GET /api/v1/analytics/largest-transactions requests.
func (h *AnalyticsHandler) GetLargestTransactionsForPeriod(ctx context.Context, input *GetLargestTransactionsForPeriodInput) (*GetLargestTransactionsForPeriodOutput, error) {
	if err := rejectHouseholdScope(input.Scope); err != nil {
		return nil, err
	}
	filters, err := parseSpendingFilters(input.StartDate, input.EndDate, input.AccountTypes, input.IncludeTransfers)
	if err != nil {
		return nil, err
	}

	limit := validateLimit(input.Limit, 10, 100)

	response, err := h.service.GetLargestTransactionsForPeriod(ctx, input.UserID, *filters, limit)
	if err != nil {
		return nil, humaerrors.LoggingMapError(ctx, err)
	}

	return &GetLargestTransactionsForPeriodOutput{Body: response}, nil
}

// GetCategoryBreakdownForMerchantInput represents input for GET /analytics/merchant-category-breakdown.
type GetCategoryBreakdownForMerchantInput struct {
	inputs.AuthenticatedInput
	StartDate        string `query:"start_date" required:"true" doc:"Start of date range (YYYY-MM-DD)"`
	EndDate          string `query:"end_date" required:"true" doc:"End of date range (YYYY-MM-DD)"`
	MerchantID       string `query:"merchant_id" required:"true" doc:"Merchant UUID to filter by" format:"uuid"`
	AccountTypes     string `query:"account_types" doc:"Comma-separated account types to filter"`
	IncludeTransfers string `query:"include_transfers" doc:"Include transfer transactions (true/false, default false)"`
}

// GetCategoryBreakdownForMerchantOutput represents the response for GET /analytics/merchant-category-breakdown.
type GetCategoryBreakdownForMerchantOutput struct {
	Body *domain.CategoryBreakdownForMerchantResponse
}

// GetCategoryBreakdownForMerchant handles GET /api/v1/analytics/merchant-category-breakdown requests.
func (h *AnalyticsHandler) GetCategoryBreakdownForMerchant(ctx context.Context, input *GetCategoryBreakdownForMerchantInput) (*GetCategoryBreakdownForMerchantOutput, error) {
	filters, err := parseSpendingFilters(input.StartDate, input.EndDate, input.AccountTypes, input.IncludeTransfers)
	if err != nil {
		return nil, err
	}

	response, err := h.service.GetCategoryBreakdownForMerchant(ctx, input.UserID, input.MerchantID, *filters)
	if err != nil {
		return nil, humaerrors.LoggingMapError(ctx, err)
	}

	return &GetCategoryBreakdownForMerchantOutput{Body: response}, nil
}

// GetIncomeSourceBreakdownInput represents input for GET /analytics/income-sources.
type GetIncomeSourceBreakdownInput struct {
	inputs.AuthenticatedInput
	StartDate        string `query:"start_date" required:"true" doc:"Start of date range (YYYY-MM-DD)"`
	EndDate          string `query:"end_date" required:"true" doc:"End of date range (YYYY-MM-DD)"`
	AccountTypes     string `query:"account_types" doc:"Comma-separated account types to filter"`
	IncludeTransfers string `query:"include_transfers" doc:"Include transfer transactions (true/false, default false)"`
	Scope            string `query:"scope" enum:"personal,household" default:"personal" doc:"Aggregation scope: personal (default) or household (requires active household membership)"`
}

// GetIncomeSourceBreakdownOutput represents the response for GET /analytics/income-sources.
type GetIncomeSourceBreakdownOutput struct {
	Body *domain.IncomeSourceBreakdownResponse
}

// GetIncomeSourceBreakdown handles GET /api/v1/analytics/income-sources requests.
func (h *AnalyticsHandler) GetIncomeSourceBreakdown(ctx context.Context, input *GetIncomeSourceBreakdownInput) (*GetIncomeSourceBreakdownOutput, error) {
	filters, err := parseSpendingFilters(input.StartDate, input.EndDate, input.AccountTypes, input.IncludeTransfers)
	if err != nil {
		return nil, err
	}
	if filters.HouseholdScope, err = resolveHouseholdScopeIfRequested(ctx, h.householdSvc, input.UserID, input.Scope); err != nil {
		return nil, err
	}

	response, err := h.service.GetIncomeSourceBreakdown(ctx, input.UserID, *filters)
	if err != nil {
		return nil, humaerrors.LoggingMapError(ctx, err)
	}

	return &GetIncomeSourceBreakdownOutput{Body: response}, nil
}

// GetUnusualSpendInput represents input for GET /analytics/unusual-spend.
type GetUnusualSpendInput struct {
	inputs.AuthenticatedInput
	StartDate string `query:"start_date" required:"true" doc:"Start of query window (YYYY-MM-DD)"`
	EndDate   string `query:"end_date" required:"true" doc:"End of query window (YYYY-MM-DD)"`
}

// GetUnusualSpendOutput represents the response for GET /analytics/unusual-spend.
type GetUnusualSpendOutput struct {
	Body *domain.UnusualSpendResponse
}

// GetUnusualSpend handles GET /api/v1/analytics/unusual-spend requests.
func (h *AnalyticsHandler) GetUnusualSpend(ctx context.Context, input *GetUnusualSpendInput) (*GetUnusualSpendOutput, error) {
	startDate, endDate, err := dateutil.ParseDateRange(input.StartDate, input.EndDate)
	if err != nil {
		return nil, err
	}

	response, err := h.service.GetUnusualSpend(ctx, input.UserID, startDate, endDate)
	if err != nil {
		return nil, humaerrors.LoggingMapError(ctx, err)
	}

	return &GetUnusualSpendOutput{Body: response}, nil
}

// GetNetWorthTrendInput represents input for GET /analytics/net-worth-trend.
type GetNetWorthTrendInput struct {
	inputs.AuthenticatedInput
	Grouping string `query:"grouping" doc:"Bucket size: daily, weekly, or monthly (default monthly)" default:"monthly"`
	Scope    string `query:"scope" enum:"personal,household" default:"personal" doc:"Aggregation scope: personal (default) or household (requires active household membership)"`
}

// GetNetWorthTrendOutput represents the response for GET /analytics/net-worth-trend.
type GetNetWorthTrendOutput struct {
	Body *domain.NetWorthTrendResponse
}

// GetNetWorthTrend handles GET /api/v1/analytics/net-worth-trend requests.
func (h *AnalyticsHandler) GetNetWorthTrend(ctx context.Context, input *GetNetWorthTrendInput) (*GetNetWorthTrendOutput, error) {
	grouping := input.Grouping
	if grouping == "" {
		grouping = "monthly"
	}

	scope, err := resolveHouseholdScopeIfRequested(ctx, h.householdSvc, input.UserID, input.Scope)
	if err != nil {
		return nil, err
	}

	response, err := h.service.GetNetWorthTrend(ctx, input.UserID, grouping, scope)
	if err != nil {
		return nil, humaerrors.LoggingMapError(ctx, err)
	}

	return &GetNetWorthTrendOutput{Body: response}, nil
}

// GetBudgetVsActualInput represents input for GET /analytics/budget-vs-actual.
type GetBudgetVsActualInput struct {
	inputs.AuthenticatedInput
	Period string `query:"period" required:"true" doc:"Budget period to query: weekly, monthly, quarterly, yearly, or custom"`
}

// GetBudgetVsActualOutput represents the response for GET /analytics/budget-vs-actual.
type GetBudgetVsActualOutput struct {
	Body *domain.BudgetVsActualResponse
}

// GetBudgetVsActual handles GET /api/v1/analytics/budget-vs-actual requests.
func (h *AnalyticsHandler) GetBudgetVsActual(ctx context.Context, input *GetBudgetVsActualInput) (*GetBudgetVsActualOutput, error) {
	response, err := h.service.GetBudgetVsActual(ctx, input.UserID, input.Period)
	if err != nil {
		return nil, humaerrors.LoggingMapError(ctx, err)
	}

	return &GetBudgetVsActualOutput{Body: response}, nil
}

// GetFinancialSummaryInput represents input for GET /analytics/financial-summary.
type GetFinancialSummaryInput struct {
	inputs.AuthenticatedInput
	StartDate        string `query:"start_date" required:"true" doc:"Start of date range (YYYY-MM-DD)"`
	EndDate          string `query:"end_date" required:"true" doc:"End of date range (YYYY-MM-DD)"`
	AccountTypes     string `query:"account_types" doc:"Comma-separated account types (optional)"`
	IncludeTransfers string `query:"include_transfers" doc:"Include transfers (true/false)"`
	Scope            string `query:"scope" enum:"personal,household" default:"personal" doc:"Aggregation scope for the savings-rate, average-spend, top-categories, month-over-month, and net-worth-trend sub-metrics (personal default, or household). Budget-vs-actual always stays personal."`
}

// GetFinancialSummaryOutput represents the response for GET /analytics/financial-summary.
type GetFinancialSummaryOutput struct {
	Body *domain.FinancialSummaryResponse
}

// GetFinancialSummary handles GET /api/v1/analytics/financial-summary requests.
// It performs a parallel fan-out across all key analytics metrics. Sub-metrics are
// best-effort: individual failures are logged and the section is omitted from the
// response rather than returning a 500. Designed as an AI-tool-callable single
// round-trip for "how am I doing financially?" questions.
func (h *AnalyticsHandler) GetFinancialSummary(ctx context.Context, input *GetFinancialSummaryInput) (*GetFinancialSummaryOutput, error) {
	filters, err := parseSpendingFilters(input.StartDate, input.EndDate, input.AccountTypes, input.IncludeTransfers)
	if err != nil {
		return nil, err
	}
	if filters.HouseholdScope, err = resolveHouseholdScopeIfRequested(ctx, h.householdSvc, input.UserID, input.Scope); err != nil {
		return nil, err
	}

	resp := &domain.FinancialSummaryResponse{
		GeneratedAt: time.Now().UTC().Format("2006-01-02T15:04:05Z"),
	}

	g, gCtx := errgroup.WithContext(ctx)

	g.Go(func() error {
		r, e := h.service.GetSavingsRateForPeriod(gCtx, input.UserID, filters.StartDate, filters.EndDate, filters.HouseholdScope)
		if e != nil {
			slog.WarnContext(ctx, "financial_summary: savings_rate sub-metric failed", "error", e)
			return nil
		}
		resp.SavingsRate = r
		return nil
	})

	g.Go(func() error {
		r, e := h.service.GetAverageMonthlySpend(gCtx, input.UserID, *filters)
		if e != nil {
			slog.WarnContext(ctx, "financial_summary: average_spend sub-metric failed", "error", e)
			return nil
		}
		resp.AverageSpend = r
		return nil
	})

	g.Go(func() error {
		r, e := h.service.GetSpendingByCategory(gCtx, input.UserID, *filters)
		if e != nil {
			slog.WarnContext(ctx, "financial_summary: top_categories sub-metric failed", "error", e)
			return nil
		}
		resp.TopCategories = r
		return nil
	})

	g.Go(func() error {
		r, e := h.service.GetMonthOverMonthSpendingChange(gCtx, input.UserID, "", 6, nil, false, filters.HouseholdScope)
		if e != nil {
			slog.WarnContext(ctx, "financial_summary: mom_change sub-metric failed", "error", e)
			return nil
		}
		resp.MomChange = r
		return nil
	})

	g.Go(func() error {
		r, e := h.service.GetBudgetVsActual(gCtx, input.UserID, "monthly")
		if e != nil {
			slog.WarnContext(ctx, "financial_summary: budget_vs_actual sub-metric failed", "error", e)
			return nil
		}
		resp.BudgetVsActual = r
		return nil
	})

	g.Go(func() error {
		r, e := h.service.GetNetWorthTrend(gCtx, input.UserID, "monthly", filters.HouseholdScope)
		if e != nil {
			slog.WarnContext(ctx, "financial_summary: net_worth_trend sub-metric failed", "error", e)
			return nil
		}
		resp.NetWorthTrend = r
		return nil
	})

	// All goroutines above return nil on error (degradable design — failures are logged
	// and the field is omitted). g.Wait() will always return nil here; this guard is
	// defensive in case future goroutines ever propagate non-degradable errors.
	if err := g.Wait(); err != nil {
		return nil, humaerrors.LoggingMapError(ctx, err)
	}

	return &GetFinancialSummaryOutput{Body: resp}, nil
}

// RegisterAnalyticsRoutes registers analytics routes with the Huma API.
// Note: These routes require the Clerk middleware to be applied.
func RegisterAnalyticsRoutes(api huma.API, handler *AnalyticsHandler) {
	// GET /api/v1/analytics/cash-flow-summary - Get cash flow summary
	huma.Register(api, huma.Operation{
		OperationID: "getCashFlowSummary",
		Method:      http.MethodGet,
		Path:        "/api/v1/analytics/cash-flow-summary",
		Summary:     "Get cash flow summary",
		Description: "Returns aggregated cash flow data (income, expenses, net) for a date range.",
		Tags:        []string{"Analytics"},
		Security:    []map[string][]string{{"clerk": {}}},
	}, handler.GetCashFlowSummary)

	// GET /api/v1/analytics/cash-flow-history - Get cash flow history
	huma.Register(api, huma.Operation{
		OperationID: "getCashFlowHistory",
		Method:      http.MethodGet,
		Path:        "/api/v1/analytics/cash-flow-history",
		Summary:     "Get cash flow history",
		Description: "Returns time series cash flow data grouped by period for charts.",
		Tags:        []string{"Analytics"},
		Security:    []map[string][]string{{"clerk": {}}},
	}, handler.GetCashFlowHistory)

	// GET /api/v1/analytics/spending-by-category - Get spending by category
	huma.Register(api, huma.Operation{
		OperationID: "getSpendingByCategory",
		Method:      http.MethodGet,
		Path:        "/api/v1/analytics/spending-by-category",
		Summary:     "Get spending by category",
		Description: "Returns spending totals grouped by category with percentages. Pass category param for subcategory drill-down.",
		Tags:        []string{"Analytics"},
		Security:    []map[string][]string{{"clerk": {}}},
	}, handler.GetSpendingByCategory)

	// GET /api/v1/analytics/category-hierarchy - Category tree for one flow direction
	huma.Register(api, huma.Operation{
		OperationID: "getCategoryHierarchy",
		Method:      http.MethodGet,
		Path:        "/api/v1/analytics/category-hierarchy",
		Summary:     "Get the category hierarchy",
		Description: "Returns the category tree for one flow direction (income or expense), up to three levels deep: PFC primary, then its detailed categories or custom categories nested under it, then custom categories nested under those. Each node carries a roll-up total, its own direct total, and a transaction count. Levels are ragged — a branch ends where its data ends.",
		Tags:        []string{"Analytics"},
		Security:    []map[string][]string{{"clerk": {}}},
	}, handler.GetCategoryHierarchy)

	// GET /api/v1/analytics/spending-by-merchant - Get top merchants by spending
	huma.Register(api, huma.Operation{
		OperationID: "getSpendingByMerchant",
		Method:      http.MethodGet,
		Path:        "/api/v1/analytics/spending-by-merchant",
		Summary:     "Get spending by merchant",
		Description: "Returns top merchants by spending with percentages.",
		Tags:        []string{"Analytics"},
		Security:    []map[string][]string{{"clerk": {}}},
	}, handler.GetSpendingByMerchant)

	// GET /api/v1/analytics/spending-trends - Get monthly spending trends
	huma.Register(api, huma.Operation{
		OperationID: "getSpendingTrends",
		Method:      http.MethodGet,
		Path:        "/api/v1/analytics/spending-trends",
		Summary:     "Get spending trends",
		Description: "Returns monthly category spending trends for stacked bar charts. Top 5 categories plus Other.",
		Tags:        []string{"Analytics"},
		Security:    []map[string][]string{{"clerk": {}}},
	}, handler.GetSpendingTrends)

	// GET /api/v1/analytics/average-monthly-spend - MCP helper (Issue #1215)
	huma.Register(api, huma.Operation{
		OperationID: "getAverageMonthlySpend",
		Method:      http.MethodGet,
		Path:        "/api/v1/analytics/average-monthly-spend",
		Summary:     "Get average monthly spend",
		Description: "Returns the average monthly expense total over a date range. Single DB round-trip; designed as an AI-tool-callable function.",
		Tags:        []string{"Analytics"},
		Security:    []map[string][]string{{"clerk": {}}},
	}, handler.GetAverageMonthlySpend)

	// GET /api/v1/analytics/recurring-subscriptions - MCP helper: recurring charge detection (Issue #1215)
	huma.Register(api, huma.Operation{
		OperationID: "getRecurringSubscriptions",
		Method:      http.MethodGet,
		Path:        "/api/v1/analytics/recurring-subscriptions",
		Summary:     "Get recurring subscriptions",
		Description: "Detects recurring subscription charges using heuristic analysis of the trailing 12-month transaction history. Returns merchants with monthly-cadence charges (≥3 occurrences, ~30-day gap). Designed as an AI-tool-callable function.",
		Tags:        []string{"Analytics"},
		Security:    []map[string][]string{{"clerk": {}}},
	}, handler.GetRecurringSubscriptions)

	// GET /api/v1/analytics/category-total - MCP helper: total spend for a category (Issue #1087)
	huma.Register(api, huma.Operation{
		OperationID: "getCategoryTotalForPeriod",
		Method:      http.MethodGet,
		Path:        "/api/v1/analytics/category-total",
		Summary:     "Get total spend for a category",
		Description: "Returns the total expense amount and transaction count for a specific category in a date range. Single DB round-trip; designed as an AI-tool-callable function.",
		Tags:        []string{"Analytics"},
		Security:    []map[string][]string{{"clerk": {}}},
	}, handler.GetCategoryTotalForPeriod)

	// GET /api/v1/analytics/top-expense - MCP helper: top single expense (Issue #1087)
	huma.Register(api, huma.Operation{
		OperationID: "getTopExpense",
		Method:      http.MethodGet,
		Path:        "/api/v1/analytics/top-expense",
		Summary:     "Get top expense",
		Description: "Returns the single largest expense transaction in a date range. Returns null when no expenses exist. Designed as an AI-tool-callable function.",
		Tags:        []string{"Analytics"},
		Security:    []map[string][]string{{"clerk": {}}},
	}, handler.GetTopExpense)

	// GET /api/v1/analytics/savings-rate - MCP helper: savings rate (Issue #1215 PR2)
	huma.Register(api, huma.Operation{
		OperationID: "getSavingsRateForPeriod",
		Method:      http.MethodGet,
		Path:        "/api/v1/analytics/savings-rate",
		Summary:     "Get savings rate for period",
		Description: "Returns savings rate (income - expenses) / income for a date range. Null savings_rate when income is zero. AI-tool-callable.",
		Tags:        []string{"Analytics"},
		Security:    []map[string][]string{{"clerk": {}}},
	}, handler.GetSavingsRateForPeriod)

	// GET /api/v1/analytics/month-over-month - MCP helper: month-over-month change (Issue #1215 PR2)
	huma.Register(api, huma.Operation{
		OperationID: "getMonthOverMonthSpendingChange",
		Method:      http.MethodGet,
		Path:        "/api/v1/analytics/month-over-month",
		Summary:     "Get month-over-month spending change",
		Description: "Returns monthly spend buckets with period-over-period percentage and absolute changes. months param controls lookback (2-24). AI-tool-callable.",
		Tags:        []string{"Analytics"},
		Security:    []map[string][]string{{"clerk": {}}},
	}, handler.GetMonthOverMonthSpendingChange)

	// GET /api/v1/analytics/largest-transactions - MCP helper: largest transactions (Issue #1215 PR2)
	huma.Register(api, huma.Operation{
		OperationID: "getLargestTransactionsForPeriod",
		Method:      http.MethodGet,
		Path:        "/api/v1/analytics/largest-transactions",
		Summary:     "Get largest transactions for period",
		Description: "Returns the N largest expense transactions in a date range, ordered by amount descending. AI-tool-callable.",
		Tags:        []string{"Analytics"},
		Security:    []map[string][]string{{"clerk": {}}},
	}, handler.GetLargestTransactionsForPeriod)

	// GET /api/v1/analytics/merchant-category-breakdown - MCP helper: merchant category breakdown (Issue #1215 PR2)
	huma.Register(api, huma.Operation{
		OperationID: "getCategoryBreakdownForMerchant",
		Method:      http.MethodGet,
		Path:        "/api/v1/analytics/merchant-category-breakdown",
		Summary:     "Get category breakdown for merchant",
		Description: "Returns spending totals grouped by category for a specific merchant, with percentages. AI-tool-callable.",
		Tags:        []string{"Analytics"},
		Security:    []map[string][]string{{"clerk": {}}},
	}, handler.GetCategoryBreakdownForMerchant)

	// GET /api/v1/analytics/income-sources - MCP helper: income source breakdown (Issue #1215 PR2)
	huma.Register(api, huma.Operation{
		OperationID: "getIncomeSourceBreakdown",
		Method:      http.MethodGet,
		Path:        "/api/v1/analytics/income-sources",
		Summary:     "Get income source breakdown",
		Description: "Returns income (inflow) totals grouped by category with percentages. AI-tool-callable.",
		Tags:        []string{"Analytics"},
		Security:    []map[string][]string{{"clerk": {}}},
	}, handler.GetIncomeSourceBreakdown)

	// GET /api/v1/analytics/unusual-spend - MCP helper: unusual spend detection (Issue #1215 PR3)
	huma.Register(api, huma.Operation{
		OperationID: "getUnusualSpend",
		Method:      http.MethodGet,
		Path:        "/api/v1/analytics/unusual-spend",
		Summary:     "Get unusual spend",
		Description: "Returns transactions in the query window that exceed 2 standard deviations above the per-category baseline mean (3-month window before startDate). AI-tool-callable.",
		Tags:        []string{"Analytics"},
		Security:    []map[string][]string{{"clerk": {}}},
	}, handler.GetUnusualSpend)

	// GET /api/v1/analytics/net-worth-trend - MCP helper: net worth trend (Issue #1215 PR3)
	huma.Register(api, huma.Operation{
		OperationID: "getNetWorthTrend",
		Method:      http.MethodGet,
		Path:        "/api/v1/analytics/net-worth-trend",
		Summary:     "Get net worth trend",
		Description: "Returns net worth bucketed by grouping (daily/weekly/monthly) over the trailing 12 months. Uses balance snapshot history. AI-tool-callable.",
		Tags:        []string{"Analytics"},
		Security:    []map[string][]string{{"clerk": {}}},
	}, handler.GetNetWorthTrend)

	// GET /api/v1/analytics/budget-vs-actual - MCP helper: budget vs actual (Issue #1215 PR3)
	huma.Register(api, huma.Operation{
		OperationID: "getBudgetVsActual",
		Method:      http.MethodGet,
		Path:        "/api/v1/analytics/budget-vs-actual",
		Summary:     "Get budget vs actual",
		Description: "Returns active budgets for the given period with allocated vs spent amounts, remaining balance, over-budget flag, and utilisation percentage. period selects by configured budget period (weekly/monthly/quarterly/yearly/custom). AI-tool-callable.",
		Tags:        []string{"Analytics"},
		Security:    []map[string][]string{{"clerk": {}}},
	}, handler.GetBudgetVsActual)

	// GET /api/v1/analytics/financial-summary - composite fan-out (Issue #1215 PR4)
	huma.Register(api, huma.Operation{
		OperationID: "getFinancialSummary",
		Method:      http.MethodGet,
		Path:        "/api/v1/analytics/financial-summary",
		Summary:     "Get financial summary",
		Description: "Returns a bundled financial summary with parallel fan-out across key analytics metrics. Sub-metrics are best-effort: individual failures are logged and the section is omitted from the response (200 with partial data). Designed as an AI-tool-callable single round-trip for common 'how am I doing financially?' questions.",
		Tags:        []string{"Analytics"},
		Security:    []map[string][]string{{"clerk": {}}},
	}, handler.GetFinancialSummary)
}
