package handlers

import (
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/middleware"
	"github.com/maxnussbaum/BoostedBudget/internal/services"
)

// TransactionHandler handles transaction-related HTTP requests
type TransactionHandler struct {
	transactionService services.TransactionService
}

// NewTransactionHandler creates a new TransactionHandler instance
func NewTransactionHandler(transactionService services.TransactionService) *TransactionHandler {
	return &TransactionHandler{
		transactionService: transactionService,
	}
}

// GetTransactions handles GET /api/v1/transactions
// Retrieves transactions for the authenticated user with optional filters and pagination
//
// Query Parameters:
//   - limit (optional): Number of transactions to return (1-100, default 50)
//   - offset (optional): Number of transactions to skip for pagination (default 0)
//   - account_id (optional): Filter by specific account ID
//   - start_date (optional): Filter transactions on or after this date (format: YYYY-MM-DD)
//   - end_date (optional): Filter transactions on or before this date (format: YYYY-MM-DD)
//   - category (optional): Filter by category string (case-insensitive partial match)
//   - search (optional): Search in merchant name or transaction name (case-insensitive)
//   - pending (optional): Filter by pending status (true/false)
//
// Response: JSON with transactions array and pagination metadata
func (h *TransactionHandler) GetTransactions(c echo.Context) error {
	// Extract user_id from context (set by Auth0 middleware)
	userID, err := middleware.GetUserID(c)
	if err != nil {
		return echo.NewHTTPError(http.StatusUnauthorized, "User not authenticated")
	}

	// Parse pagination parameters
	limit := 50 // default
	if limitStr := c.QueryParam("limit"); limitStr != "" {
		parsedLimit, err := strconv.Atoi(limitStr)
		if err != nil {
			return echo.NewHTTPError(http.StatusBadRequest, "Invalid limit parameter")
		}
		limit = parsedLimit
	}

	offset := 0 // default
	if offsetStr := c.QueryParam("offset"); offsetStr != "" {
		parsedOffset, err := strconv.Atoi(offsetStr)
		if err != nil {
			return echo.NewHTTPError(http.StatusBadRequest, "Invalid offset parameter")
		}
		offset = parsedOffset
	}

	// Parse filter parameters
	filters := domain.TransactionFilters{}

	// Account ID filter
	if accountID := c.QueryParam("account_id"); accountID != "" {
		filters.AccountID = &accountID
	}

	// Date range filters
	if startDateStr := c.QueryParam("start_date"); startDateStr != "" {
		startDate, err := time.Parse("2006-01-02", startDateStr)
		if err != nil {
			return echo.NewHTTPError(http.StatusBadRequest, "Invalid start_date format (expected YYYY-MM-DD)")
		}
		filters.StartDate = &startDate
	}

	if endDateStr := c.QueryParam("end_date"); endDateStr != "" {
		endDate, err := time.Parse("2006-01-02", endDateStr)
		if err != nil {
			return echo.NewHTTPError(http.StatusBadRequest, "Invalid end_date format (expected YYYY-MM-DD)")
		}
		filters.EndDate = &endDate
	}

	// Category filter
	if category := c.QueryParam("category"); category != "" {
		filters.Category = &category
	}

	// Search filter
	if search := c.QueryParam("search"); search != "" {
		filters.Search = &search
	}

	// Pending status filter
	if pendingStr := c.QueryParam("pending"); pendingStr != "" {
		pending, err := strconv.ParseBool(pendingStr)
		if err != nil {
			return echo.NewHTTPError(http.StatusBadRequest, "Invalid pending parameter (expected true or false)")
		}
		filters.Pending = &pending
	}

	// Call service layer to fetch transactions
	result, err := h.transactionService.ListTransactions(
		c.Request().Context(),
		userID,
		filters,
		limit,
		offset,
	)

	if err != nil {
		// Handle specific service errors
		switch {
		case errors.Is(err, services.ErrInvalidLimit):
			return echo.NewHTTPError(http.StatusBadRequest, "Limit must be between 1 and 100")
		case errors.Is(err, services.ErrInvalidOffset):
			return echo.NewHTTPError(http.StatusBadRequest, "Offset must be non-negative")
		case errors.Is(err, services.ErrInvalidDateRange):
			return echo.NewHTTPError(http.StatusBadRequest, "Start date must be before or equal to end date")
		default:
			c.Logger().Errorf("Failed to fetch transactions for user %s: %v", userID, err)
			return echo.NewHTTPError(http.StatusInternalServerError, "Failed to fetch transactions")
		}
	}

	return c.JSON(http.StatusOK, result)
}
