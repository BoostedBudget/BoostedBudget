package handlers

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
)

// DashboardService defines the interface for dashboard business logic
type DashboardService interface {
	GetBalanceSummary(ctx context.Context, userID string, filters *domain.DashboardFilters) (*domain.BalanceSummaryResponse, error)
	GetBalanceHistory(ctx context.Context, userID string, filters *domain.DashboardFilters) (*domain.BalanceHistoryResponse, error)
	GetAccountTypes(ctx context.Context, userID string) (*domain.AccountTypesResponse, error)
}

// DashboardHandler handles dashboard-related HTTP requests
type DashboardHandler struct {
	service DashboardService
}

// NewDashboardHandler creates a new DashboardHandler instance
func NewDashboardHandler(service DashboardService) *DashboardHandler {
	return &DashboardHandler{
		service: service,
	}
}

// GetBalanceSummary handles GET /api/v1/dashboard/balance-summary
// Returns current balance overview with breakdown by account type and period comparison
func (h *DashboardHandler) GetBalanceSummary(c echo.Context) error {
	// Extract user ID from context (set by auth middleware)
	userID, err := middleware.GetUserID(c)
	if err != nil {
		return echo.NewHTTPError(http.StatusUnauthorized, "User not authenticated")
	}

	// Parse query parameters
	filters, err := h.parseFilters(c)
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	}

	// Call service
	response, err := h.service.GetBalanceSummary(c.Request().Context(), userID, filters)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "Failed to get balance summary")
	}

	return c.JSON(http.StatusOK, response)
}

// GetBalanceHistory handles GET /api/v1/dashboard/balance-history
// Returns time series balance data for charts with optional account type breakdown
func (h *DashboardHandler) GetBalanceHistory(c echo.Context) error {
	// Extract user ID from context
	userID, err := middleware.GetUserID(c)
	if err != nil {
		return echo.NewHTTPError(http.StatusUnauthorized, "User not authenticated")
	}

	// Parse query parameters
	filters, err := h.parseFilters(c)
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	}

	// Call service
	response, err := h.service.GetBalanceHistory(c.Request().Context(), userID, filters)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "Failed to get balance history")
	}

	return c.JSON(http.StatusOK, response)
}

// GetAccountTypes handles GET /api/v1/dashboard/account-types
// Returns available account types for the user with counts and totals
func (h *DashboardHandler) GetAccountTypes(c echo.Context) error {
	// Extract user ID from context
	userID, err := middleware.GetUserID(c)
	if err != nil {
		return echo.NewHTTPError(http.StatusUnauthorized, "User not authenticated")
	}

	// Call service
	response, err := h.service.GetAccountTypes(c.Request().Context(), userID)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "Failed to get account types")
	}

	return c.JSON(http.StatusOK, response)
}

// parseFilters parses query parameters into DashboardFilters
func (h *DashboardHandler) parseFilters(c echo.Context) (*domain.DashboardFilters, error) {
	filters := &domain.DashboardFilters{}

	// Parse account_types (comma-separated list)
	accountTypesParam := c.QueryParam("account_types")
	if accountTypesParam != "" {
		filters.AccountTypes = strings.Split(accountTypesParam, ",")
		// Trim whitespace from each type
		for i := range filters.AccountTypes {
			filters.AccountTypes[i] = strings.TrimSpace(filters.AccountTypes[i])
		}
	}

	// Parse as_of_date (YYYY-MM-DD format)
	asOfDateParam := c.QueryParam("as_of_date")
	if asOfDateParam != "" {
		asOfDate, err := time.Parse("2006-01-02", asOfDateParam)
		if err != nil {
			return nil, err
		}
		filters.AsOfDate = &asOfDate
	}

	// Parse start_date (YYYY-MM-DD format)
	startDateParam := c.QueryParam("start_date")
	if startDateParam != "" {
		startDate, err := time.Parse("2006-01-02", startDateParam)
		if err != nil {
			return nil, err
		}
		filters.StartDate = &startDate
	}

	// Parse end_date (YYYY-MM-DD format)
	endDateParam := c.QueryParam("end_date")
	if endDateParam != "" {
		endDate, err := time.Parse("2006-01-02", endDateParam)
		if err != nil {
			return nil, err
		}
		filters.EndDate = &endDate
	}

	// Parse granularity (daily, weekly, monthly)
	granularityParam := c.QueryParam("granularity")
	if granularityParam != "" {
		filters.Granularity = granularityParam
	}

	return filters, nil
}
