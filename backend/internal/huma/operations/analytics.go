package operations

import (
	"context"
	"net/http"
	"strings"

	"github.com/danielgtaylor/huma/v2"
	"github.com/maxnussbaum/BoostedBudget/internal/dateutil"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	humaerrors "github.com/maxnussbaum/BoostedBudget/internal/huma/errors"
	"github.com/maxnussbaum/BoostedBudget/internal/huma/inputs"
	"github.com/maxnussbaum/BoostedBudget/internal/services"
)

// AnalyticsHandler handles analytics-related HTTP requests.
// Issue #354: Cash Flow Analysis & Income/Expense Visualization
type AnalyticsHandler struct {
	service services.AnalyticsService
}

// NewAnalyticsHandler creates a new AnalyticsHandler instance.
func NewAnalyticsHandler(service services.AnalyticsService) *AnalyticsHandler {
	return &AnalyticsHandler{service: service}
}

// --- Input/Output Types ---

// GetCashFlowSummaryInput represents the input for getting cash flow summary.
type GetCashFlowSummaryInput struct {
	inputs.AuthenticatedInput
	StartDate        string `query:"start_date" required:"true" doc:"Start of date range (YYYY-MM-DD)"`
	EndDate          string `query:"end_date" required:"true" doc:"End of date range (YYYY-MM-DD)"`
	AccountTypes     string `query:"account_types" doc:"Comma-separated account types to filter"`
	IncludeTransfers string `query:"include_transfers" doc:"Include transfer transactions (true/false, default false)"`
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

	// Parse account_types (optional, comma-separated)
	if accountTypes != "" {
		parts := strings.Split(accountTypes, ",")
		for _, p := range parts {
			trimmed := strings.TrimSpace(p)
			if trimmed != "" {
				filters.AccountTypes = append(filters.AccountTypes, trimmed)
			}
		}
	}

	// Parse include_transfers (optional, default false)
	if includeTransfers != "" {
		if includeTransfers == "true" {
			filters.IncludeTransfers = true
		} else if includeTransfers == "false" {
			filters.IncludeTransfers = false
		} else {
			return nil, huma.Error400BadRequest("Invalid include_transfers value. Use true or false")
		}
	}

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
}

// GetSpendingByCategoryOutput represents the response for spending-by-category.
type GetSpendingByCategoryOutput struct {
	Body *domain.SpendingByCategoryResponse
}

// GetSpendingByMerchantInput represents the input for the spending-by-merchant endpoint.
type GetSpendingByMerchantInput struct {
	inputs.AuthenticatedInput
	StartDate        string `query:"start_date" required:"true" doc:"Start of date range (YYYY-MM-DD)"`
	EndDate          string `query:"end_date" required:"true" doc:"End of date range (YYYY-MM-DD)"`
	AccountTypes     string `query:"account_types" doc:"Comma-separated account types to filter"`
	IncludeTransfers string `query:"include_transfers" doc:"Include transfer transactions (true/false, default false)"`
	Limit            int    `query:"limit" doc:"Maximum number of merchants to return (default 10)" default:"10"`
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
}

// GetSpendingTrendsOutput represents the response for spending-trends.
type GetSpendingTrendsOutput struct {
	Body *domain.SpendingTrendsResponse
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

	if accountTypes != "" {
		parts := strings.Split(accountTypes, ",")
		for _, p := range parts {
			trimmed := strings.TrimSpace(p)
			if trimmed != "" {
				filters.AccountTypes = append(filters.AccountTypes, trimmed)
			}
		}
	}

	if includeTransfers != "" {
		if includeTransfers == "true" {
			filters.IncludeTransfers = true
		} else if includeTransfers == "false" {
			filters.IncludeTransfers = false
		} else {
			return nil, huma.Error400BadRequest("Invalid include_transfers value. Use true or false")
		}
	}

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

// GetSpendingByMerchant handles GET /api/v1/analytics/spending-by-merchant requests.
// Returns top merchants by spending with percentages.
func (h *AnalyticsHandler) GetSpendingByMerchant(ctx context.Context, input *GetSpendingByMerchantInput) (*GetSpendingByMerchantOutput, error) {
	filters, err := parseSpendingFilters(input.StartDate, input.EndDate, input.AccountTypes, input.IncludeTransfers)
	if err != nil {
		return nil, err
	}

	limit := input.Limit
	if limit <= 0 {
		limit = 10
	}

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

	response, err := h.service.GetSpendingTrends(ctx, input.UserID, *filters)
	if err != nil {
		return nil, humaerrors.LoggingMapError(ctx, err)
	}

	return &GetSpendingTrendsOutput{Body: response}, nil
}

// GetAverageMonthlySpendInput represents input for GET /analytics/average-monthly-spend.
type GetAverageMonthlySpendInput struct {
	inputs.AuthenticatedInput
	StartDate    string   `query:"start_date" required:"true" doc:"Start of date range (YYYY-MM-DD)"`
	EndDate      string   `query:"end_date" required:"true" doc:"End of date range (YYYY-MM-DD)"`
	AccountTypes []string `query:"account_types" doc:"Optional: filter by account types (e.g. depository, credit)"`
}

// GetAverageMonthlySpendOutput represents the response for GET /analytics/average-monthly-spend.
type GetAverageMonthlySpendOutput struct {
	Body *domain.AverageMonthlySpendResponse
}

// GetAverageMonthlySpend handles GET /api/v1/analytics/average-monthly-spend requests.
func (h *AnalyticsHandler) GetAverageMonthlySpend(ctx context.Context, input *GetAverageMonthlySpendInput) (*GetAverageMonthlySpendOutput, error) {
	startDate, endDate, err := dateutil.ParseDateRange(input.StartDate, input.EndDate)
	if err != nil {
		return nil, err
	}

	filters := domain.SpendingFilters{
		StartDate:    startDate,
		EndDate:      endDate,
		AccountTypes: input.AccountTypes,
	}

	response, err := h.service.GetAverageMonthlySpend(ctx, input.UserID, filters)
	if err != nil {
		return nil, humaerrors.LoggingMapError(ctx, err)
	}

	return &GetAverageMonthlySpendOutput{Body: response}, nil
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
}
