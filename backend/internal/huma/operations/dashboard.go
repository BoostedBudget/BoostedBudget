package operations

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/maxnussbaum/BoostedBudget/internal/config"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	humaerrors "github.com/maxnussbaum/BoostedBudget/internal/huma/errors"
	"github.com/maxnussbaum/BoostedBudget/internal/huma/inputs"
	"github.com/maxnussbaum/BoostedBudget/internal/services"
)

// DashboardHandler handles dashboard-related HTTP requests.
// Issue #353: Balance tracking dashboard
type DashboardHandler struct {
	service services.DashboardService
}

// NewDashboardHandler creates a new DashboardHandler instance.
func NewDashboardHandler(service services.DashboardService) *DashboardHandler {
	return &DashboardHandler{service: service}
}

// --- Input/Output Types ---

// GetBalanceSummaryInput represents the input for getting balance summary.
type GetBalanceSummaryInput struct {
	inputs.AuthenticatedInput
	AccountTypes string `query:"account_types" doc:"Comma-separated account types to filter (investment, checking, savings, credit, loan)"`
	AsOfDate     string `query:"as_of_date" doc:"Get balance as of specific date (YYYY-MM-DD)"`
}

// GetBalanceSummaryOutput represents the response for getting balance summary.
type GetBalanceSummaryOutput struct {
	Body *domain.BalanceSummaryResponse
}

// GetBalanceHistoryInput represents the input for getting balance history.
type GetBalanceHistoryInput struct {
	inputs.AuthenticatedInput
	AccountTypes string `query:"account_types" doc:"Comma-separated account types to filter"`
	StartDate    string `query:"start_date" doc:"Start of date range (YYYY-MM-DD)"`
	EndDate      string `query:"end_date" doc:"End of date range (YYYY-MM-DD)"`
	Granularity  string `query:"granularity" doc:"Time granularity: daily, weekly, or monthly"`
}

// GetBalanceHistoryOutput represents the response for getting balance history.
type GetBalanceHistoryOutput struct {
	Body *domain.BalanceHistoryResponse
}

// GetAccountTypesInput represents the input for getting account types.
type GetAccountTypesInput struct {
	inputs.AuthenticatedInput
}

// GetAccountTypesOutput represents the response for getting account types.
type GetAccountTypesOutput struct {
	Body *domain.AccountTypesResponse
}

// GetDefaultLayoutInput represents the input for getting default layout.
// Note: This is a PUBLIC endpoint, no authentication required.
type GetDefaultLayoutInput struct{}

// GetDefaultLayoutOutput represents the response for getting default layout.
type GetDefaultLayoutOutput struct {
	Body *domain.DashboardLayout
}

// Issue #127: Net Worth Tracking & Visualization

// GetNetWorthInput represents the input for getting net worth.
type GetNetWorthInput struct {
	inputs.AuthenticatedInput
	AccountTypes string `query:"account_types" doc:"Comma-separated account types to filter (depository, investment, credit, loan)"`
	AsOfDate     string `query:"as_of_date" doc:"Get net worth as of specific date (YYYY-MM-DD)"`
}

// GetNetWorthOutput represents the response for getting net worth.
type GetNetWorthOutput struct {
	Body *domain.NetWorthResponse
}

// GetNetWorthHistoryInput represents the input for getting net worth history.
type GetNetWorthHistoryInput struct {
	inputs.AuthenticatedInput
	AccountTypes string `query:"account_types" doc:"Comma-separated account types to filter"`
	StartDate    string `query:"start_date" doc:"Start of date range (YYYY-MM-DD)"`
	EndDate      string `query:"end_date" doc:"End of date range (YYYY-MM-DD)"`
	Granularity  string `query:"granularity" doc:"Time granularity: daily, weekly, or monthly"`
}

// GetNetWorthHistoryOutput represents the response for getting net worth history.
type GetNetWorthHistoryOutput struct {
	Body *domain.NetWorthHistoryResponse
}

// --- Helper Functions ---

// parseFilters parses query parameters into DashboardFilters.
func parseFilters(accountTypes, asOfDate, startDate, endDate, granularity string) (*domain.DashboardFilters, error) {
	filters := &domain.DashboardFilters{}

	// Parse account_types (comma-separated list)
	if accountTypes != "" {
		parts := strings.Split(accountTypes, ",")
		for _, p := range parts {
			trimmed := strings.TrimSpace(p)
			if trimmed != "" {
				filters.AccountTypes = append(filters.AccountTypes, trimmed)
			}
		}
	}

	// Parse as_of_date (YYYY-MM-DD format)
	if asOfDate != "" {
		parsed, err := time.Parse("2006-01-02", asOfDate)
		if err != nil {
			return nil, huma.Error400BadRequest("Invalid as_of_date format, expected YYYY-MM-DD")
		}
		filters.AsOfDate = &parsed
	}

	// Parse start_date (YYYY-MM-DD format)
	if startDate != "" {
		parsed, err := time.Parse("2006-01-02", startDate)
		if err != nil {
			return nil, huma.Error400BadRequest("Invalid start_date format, expected YYYY-MM-DD")
		}
		filters.StartDate = &parsed
	}

	// Parse end_date (YYYY-MM-DD format)
	if endDate != "" {
		parsed, err := time.Parse("2006-01-02", endDate)
		if err != nil {
			return nil, huma.Error400BadRequest("Invalid end_date format, expected YYYY-MM-DD")
		}
		filters.EndDate = &parsed
	}

	// Parse granularity (daily, weekly, monthly)
	if granularity != "" {
		filters.Granularity = granularity
	}

	return filters, nil
}

// --- Handler Methods ---

// GetBalanceSummary handles GET /api/v1/dashboard/balance-summary requests.
// Returns current balance overview with breakdown by account type and period comparison.
func (h *DashboardHandler) GetBalanceSummary(ctx context.Context, input *GetBalanceSummaryInput) (*GetBalanceSummaryOutput, error) {
	filters, err := parseFilters(input.AccountTypes, input.AsOfDate, "", "", "")
	if err != nil {
		return nil, err
	}

	response, err := h.service.GetBalanceSummary(ctx, input.UserID, filters)
	if err != nil {
		return nil, humaerrors.MapError(err)
	}

	return &GetBalanceSummaryOutput{Body: response}, nil
}

// GetBalanceHistory handles GET /api/v1/dashboard/balance-history requests.
// Returns time series balance data for charts with optional account type breakdown.
func (h *DashboardHandler) GetBalanceHistory(ctx context.Context, input *GetBalanceHistoryInput) (*GetBalanceHistoryOutput, error) {
	filters, err := parseFilters(input.AccountTypes, "", input.StartDate, input.EndDate, input.Granularity)
	if err != nil {
		return nil, err
	}

	response, err := h.service.GetBalanceHistory(ctx, input.UserID, filters)
	if err != nil {
		return nil, humaerrors.MapError(err)
	}

	return &GetBalanceHistoryOutput{Body: response}, nil
}

// GetAccountTypes handles GET /api/v1/dashboard/account-types requests.
// Returns available account types for the user with counts and totals.
func (h *DashboardHandler) GetAccountTypes(ctx context.Context, input *GetAccountTypesInput) (*GetAccountTypesOutput, error) {
	response, err := h.service.GetAccountTypes(ctx, input.UserID)
	if err != nil {
		return nil, humaerrors.MapError(err)
	}

	return &GetAccountTypesOutput{Body: response}, nil
}

// GetDefaultLayout handles GET /api/v1/dashboard/default-layout requests.
// Returns the default dashboard layout configuration from YAML config.
// This endpoint is PUBLIC (no auth required) - used for initial dashboard setup.
func GetDefaultLayout(ctx context.Context, input *GetDefaultLayoutInput) (*GetDefaultLayoutOutput, error) {
	// Load the dashboard layout configuration
	layoutConfig, err := config.LoadDashboardLayoutConfig()
	if err != nil {
		return nil, huma.Error500InternalServerError("Failed to load default layout configuration")
	}

	// Get the default layout as domain object
	layout := layoutConfig.GetDefaultLayout()

	return &GetDefaultLayoutOutput{Body: layout}, nil
}

// GetNetWorth handles GET /api/v1/dashboard/net-worth requests.
// Returns current net worth (assets - liabilities) with category breakdown.
func (h *DashboardHandler) GetNetWorth(ctx context.Context, input *GetNetWorthInput) (*GetNetWorthOutput, error) {
	filters, err := parseFilters(input.AccountTypes, input.AsOfDate, "", "", "")
	if err != nil {
		return nil, err
	}

	response, err := h.service.GetNetWorth(ctx, input.UserID, filters)
	if err != nil {
		return nil, humaerrors.MapError(err)
	}

	return &GetNetWorthOutput{Body: response}, nil
}

// GetNetWorthHistory handles GET /api/v1/dashboard/net-worth-history requests.
// Returns time series net worth data for charts.
func (h *DashboardHandler) GetNetWorthHistory(ctx context.Context, input *GetNetWorthHistoryInput) (*GetNetWorthHistoryOutput, error) {
	filters, err := parseFilters(input.AccountTypes, "", input.StartDate, input.EndDate, input.Granularity)
	if err != nil {
		return nil, err
	}

	response, err := h.service.GetNetWorthHistory(ctx, input.UserID, filters)
	if err != nil {
		return nil, humaerrors.MapError(err)
	}

	return &GetNetWorthHistoryOutput{Body: response}, nil
}

// RegisterDashboardRoutes registers dashboard routes with the Huma API.
// Note: Most routes require the Clerk middleware to be applied.
func RegisterDashboardRoutes(api huma.API, handler *DashboardHandler) {
	// GET /api/v1/dashboard/balance-summary - Get balance summary
	huma.Register(api, huma.Operation{
		OperationID: "getBalanceSummary",
		Method:      http.MethodGet,
		Path:        "/api/v1/dashboard/balance-summary",
		Summary:     "Get balance summary",
		Description: "Returns current balance overview with breakdown by account type and period comparison.",
		Tags:        []string{"Dashboard"},
		Security:    []map[string][]string{{"clerk": {}}},
	}, handler.GetBalanceSummary)

	// GET /api/v1/dashboard/balance-history - Get balance history
	huma.Register(api, huma.Operation{
		OperationID: "getBalanceHistory",
		Method:      http.MethodGet,
		Path:        "/api/v1/dashboard/balance-history",
		Summary:     "Get balance history",
		Description: "Returns time series balance data for charts with optional account type breakdown.",
		Tags:        []string{"Dashboard"},
		Security:    []map[string][]string{{"clerk": {}}},
	}, handler.GetBalanceHistory)

	// GET /api/v1/dashboard/account-types - Get account types
	huma.Register(api, huma.Operation{
		OperationID: "getAccountTypes",
		Method:      http.MethodGet,
		Path:        "/api/v1/dashboard/account-types",
		Summary:     "Get account types",
		Description: "Returns available account types for the user with counts and totals.",
		Tags:        []string{"Dashboard"},
		Security:    []map[string][]string{{"clerk": {}}},
	}, handler.GetAccountTypes)

	// GET /api/v1/dashboard/default-layout - Get default layout (PUBLIC)
	huma.Register(api, huma.Operation{
		OperationID: "getDefaultLayout",
		Method:      http.MethodGet,
		Path:        "/api/v1/dashboard/default-layout",
		Summary:     "Get default dashboard layout",
		Description: "Returns the default dashboard layout configuration. Public endpoint.",
		Tags:        []string{"Dashboard"},
		// Note: No Security field - this is a public endpoint
	}, GetDefaultLayout)

	// GET /api/v1/dashboard/net-worth - Get net worth
	huma.Register(api, huma.Operation{
		OperationID: "getNetWorth",
		Method:      http.MethodGet,
		Path:        "/api/v1/dashboard/net-worth",
		Summary:     "Get current net worth",
		Description: "Returns current net worth (assets - liabilities) with breakdown by category and period comparison.",
		Tags:        []string{"Dashboard"},
		Security:    []map[string][]string{{"clerk": {}}},
	}, handler.GetNetWorth)

	// GET /api/v1/dashboard/net-worth-history - Get net worth history
	huma.Register(api, huma.Operation{
		OperationID: "getNetWorthHistory",
		Method:      http.MethodGet,
		Path:        "/api/v1/dashboard/net-worth-history",
		Summary:     "Get net worth history",
		Description: "Returns time series net worth data for charts with assets and liabilities breakdown.",
		Tags:        []string{"Dashboard"},
		Security:    []map[string][]string{{"clerk": {}}},
	}, handler.GetNetWorthHistory)
}
