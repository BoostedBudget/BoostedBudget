package operations

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"
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

	// Parse start_date (required)
	if startDateStr == "" {
		return nil, huma.Error400BadRequest("start_date is required")
	}
	startDate, err := time.Parse("2006-01-02", startDateStr)
	if err != nil {
		return nil, huma.Error400BadRequest("Invalid start_date format. Use YYYY-MM-DD")
	}
	filters.StartDate = startDate

	// Parse end_date (required)
	if endDateStr == "" {
		return nil, huma.Error400BadRequest("end_date is required")
	}
	endDate, err := time.Parse("2006-01-02", endDateStr)
	if err != nil {
		return nil, huma.Error400BadRequest("Invalid end_date format. Use YYYY-MM-DD")
	}
	filters.EndDate = endDate

	// Validate date range
	if endDate.Before(startDate) {
		return nil, huma.Error400BadRequest("end_date cannot be before start_date")
	}

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

// --- Handler Methods ---

// GetCashFlowSummary handles GET /api/v1/analytics/cash-flow-summary requests.
// Returns aggregated cash flow data (income, expenses, net) for a date range.
func (h *AnalyticsHandler) GetCashFlowSummary(ctx context.Context, input *GetCashFlowSummaryInput) (*GetCashFlowSummaryOutput, error) {
	filters, err := parseCashFlowFilters(input.StartDate, input.EndDate, input.AccountTypes, input.IncludeTransfers, "")
	if err != nil {
		return nil, err
	}

	response, err := h.service.GetCashFlowSummary(ctx, input.UserID, *filters)
	if err != nil {
		return nil, humaerrors.MapError(err)
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
		return nil, humaerrors.MapError(err)
	}

	return &GetCashFlowHistoryOutput{Body: response}, nil
}

// RegisterAnalyticsRoutes registers analytics routes with the Huma API.
// Note: These routes require the Auth0 middleware to be applied.
func RegisterAnalyticsRoutes(api huma.API, handler *AnalyticsHandler) {
	// GET /api/v1/analytics/cash-flow-summary - Get cash flow summary
	huma.Register(api, huma.Operation{
		OperationID: "getCashFlowSummary",
		Method:      http.MethodGet,
		Path:        "/api/v1/analytics/cash-flow-summary",
		Summary:     "Get cash flow summary",
		Description: "Returns aggregated cash flow data (income, expenses, net) for a date range.",
		Tags:        []string{"Analytics"},
		Security:    []map[string][]string{{"auth0": {}}},
	}, handler.GetCashFlowSummary)

	// GET /api/v1/analytics/cash-flow-history - Get cash flow history
	huma.Register(api, huma.Operation{
		OperationID: "getCashFlowHistory",
		Method:      http.MethodGet,
		Path:        "/api/v1/analytics/cash-flow-history",
		Summary:     "Get cash flow history",
		Description: "Returns time series cash flow data grouped by period for charts.",
		Tags:        []string{"Analytics"},
		Security:    []map[string][]string{{"auth0": {}}},
	}, handler.GetCashFlowHistory)
}
