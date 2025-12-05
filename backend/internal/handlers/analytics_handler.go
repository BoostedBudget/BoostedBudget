package handlers

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/middleware"
)

// AnalyticsService defines the interface for analytics business logic
type AnalyticsService interface {
	GetCashFlowSummary(ctx context.Context, userID string, filters domain.CashFlowFilters) (*domain.CashFlowSummaryResponse, error)
	GetCashFlowHistory(ctx context.Context, userID string, filters domain.CashFlowFilters) (*domain.CashFlowHistoryResponse, error)
}

// AnalyticsHandler handles analytics-related HTTP requests
// Issue #354: Cash Flow Analysis & Income/Expense Visualization
type AnalyticsHandler struct {
	service AnalyticsService
}

// NewAnalyticsHandler creates a new AnalyticsHandler instance
func NewAnalyticsHandler(service AnalyticsService) *AnalyticsHandler {
	return &AnalyticsHandler{
		service: service,
	}
}

// GetCashFlowSummary handles GET /api/v1/analytics/cash-flow-summary
// Returns aggregated cash flow data (income, expenses, net) for a date range
// Query params:
//   - start_date (required): YYYY-MM-DD
//   - end_date (required): YYYY-MM-DD
//   - account_types (optional): comma-separated list
//   - include_transfers (optional): boolean, default false
func (h *AnalyticsHandler) GetCashFlowSummary(c echo.Context) error {
	// Extract user ID from context (set by auth middleware)
	userID, err := middleware.GetUserID(c)
	if err != nil {
		return echo.NewHTTPError(http.StatusUnauthorized, "User not authenticated")
	}

	// Parse and validate filters
	filters, err := h.parseCashFlowFilters(c)
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	}

	// Call service
	response, err := h.service.GetCashFlowSummary(c.Request().Context(), userID, *filters)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "Failed to get cash flow summary: "+err.Error())
	}

	return c.JSON(http.StatusOK, response)
}

// GetCashFlowHistory handles GET /api/v1/analytics/cash-flow-history
// Returns time series cash flow data grouped by period for charts
// Query params:
//   - start_date (required): YYYY-MM-DD
//   - end_date (required): YYYY-MM-DD
//   - grouping (optional): daily|weekly|monthly (auto-selected if not provided)
//   - account_types (optional): comma-separated list
//   - include_transfers (optional): boolean, default false
func (h *AnalyticsHandler) GetCashFlowHistory(c echo.Context) error {
	// Extract user ID from context
	userID, err := middleware.GetUserID(c)
	if err != nil {
		return echo.NewHTTPError(http.StatusUnauthorized, "User not authenticated")
	}

	// Parse and validate filters
	filters, err := h.parseCashFlowFilters(c)
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	}

	// Parse grouping for history endpoint
	groupingParam := c.QueryParam("grouping")
	if groupingParam != "" {
		validGroupings := map[string]bool{"daily": true, "weekly": true, "monthly": true}
		if !validGroupings[groupingParam] {
			return echo.NewHTTPError(http.StatusBadRequest, "Invalid grouping. Must be one of: daily, weekly, monthly")
		}
		filters.Grouping = groupingParam
	}

	// Call service
	response, err := h.service.GetCashFlowHistory(c.Request().Context(), userID, *filters)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "Failed to get cash flow history: "+err.Error())
	}

	return c.JSON(http.StatusOK, response)
}

// parseCashFlowFilters parses query parameters into CashFlowFilters
func (h *AnalyticsHandler) parseCashFlowFilters(c echo.Context) (*domain.CashFlowFilters, error) {
	filters := &domain.CashFlowFilters{}

	// Parse start_date (required)
	startDateParam := c.QueryParam("start_date")
	if startDateParam == "" {
		return nil, echo.NewHTTPError(http.StatusBadRequest, "start_date is required")
	}
	startDate, err := time.Parse("2006-01-02", startDateParam)
	if err != nil {
		return nil, echo.NewHTTPError(http.StatusBadRequest, "Invalid start_date format. Use YYYY-MM-DD")
	}
	filters.StartDate = startDate

	// Parse end_date (required)
	endDateParam := c.QueryParam("end_date")
	if endDateParam == "" {
		return nil, echo.NewHTTPError(http.StatusBadRequest, "end_date is required")
	}
	endDate, err := time.Parse("2006-01-02", endDateParam)
	if err != nil {
		return nil, echo.NewHTTPError(http.StatusBadRequest, "Invalid end_date format. Use YYYY-MM-DD")
	}
	filters.EndDate = endDate

	// Validate date range
	if endDate.Before(startDate) {
		return nil, echo.NewHTTPError(http.StatusBadRequest, "end_date cannot be before start_date")
	}

	// Parse account_types (optional, comma-separated)
	accountTypesParam := c.QueryParam("account_types")
	if accountTypesParam != "" {
		accountTypes := strings.Split(accountTypesParam, ",")
		for i := range accountTypes {
			accountTypes[i] = strings.TrimSpace(accountTypes[i])
		}
		filters.AccountTypes = accountTypes
	}

	// Parse include_transfers (optional, default false)
	includeTransfersParam := c.QueryParam("include_transfers")
	if includeTransfersParam != "" {
		includeTransfers, err := strconv.ParseBool(includeTransfersParam)
		if err != nil {
			return nil, echo.NewHTTPError(http.StatusBadRequest, "Invalid include_transfers value. Use true or false")
		}
		filters.IncludeTransfers = includeTransfers
	}

	return filters, nil
}
