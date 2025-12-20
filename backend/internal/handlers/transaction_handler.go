package handlers

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/middleware"
	"github.com/maxnussbaum/BoostedBudget/internal/repository"
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
//   - account_id (optional): Filter by specific account ID (deprecated: use account_ids)
//   - account_ids (optional): Filter by multiple account IDs (comma-separated)
//   - start_date (optional): Filter transactions on or after this date (format: YYYY-MM-DD)
//   - end_date (optional): Filter transactions on or before this date (format: YYYY-MM-DD)
//   - category (optional): Filter by category string (case-insensitive partial match, deprecated: use categories)
//   - categories (optional): Filter by multiple categories (comma-separated)
//   - search (optional): Search in merchant name or transaction name (case-insensitive)
//   - pending (optional): Filter by pending status (true/false)
//   - min_amount (optional): Filter by minimum absolute transaction amount
//   - max_amount (optional): Filter by maximum absolute transaction amount
//   - amount_type (optional): Filter by transaction type (income/expense/all)
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

	// Account ID filter (single, deprecated)
	if accountID := c.QueryParam("account_id"); accountID != "" {
		filters.AccountID = &accountID
	}

	// Account IDs filter (multi-value, Issue #529)
	if accountIDsStr := c.QueryParam("account_ids"); accountIDsStr != "" {
		accountIDs := strings.Split(accountIDsStr, ",")
		// Filter out empty strings
		validIDs := make([]string, 0, len(accountIDs))
		for _, id := range accountIDs {
			trimmed := strings.TrimSpace(id)
			if trimmed != "" {
				validIDs = append(validIDs, trimmed)
			}
		}
		if len(validIDs) > 0 {
			filters.AccountIDs = validIDs
		}
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

	// Category filter (single, deprecated)
	if category := c.QueryParam("category"); category != "" {
		filters.Category = &category
	}

	// Categories filter (multi-value, Issue #529)
	if categoriesStr := c.QueryParam("categories"); categoriesStr != "" {
		categories := strings.Split(categoriesStr, ",")
		// Filter out empty strings
		validCategories := make([]string, 0, len(categories))
		for _, cat := range categories {
			trimmed := strings.TrimSpace(cat)
			if trimmed != "" {
				validCategories = append(validCategories, trimmed)
			}
		}
		if len(validCategories) > 0 {
			filters.Categories = validCategories
		}
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

	// Min amount filter (Issue #529)
	if minAmountStr := c.QueryParam("min_amount"); minAmountStr != "" {
		minAmount, err := strconv.ParseFloat(minAmountStr, 64)
		if err != nil {
			return echo.NewHTTPError(http.StatusBadRequest, "Invalid min_amount parameter (expected number)")
		}
		filters.MinAmount = &minAmount
	}

	// Max amount filter (Issue #529)
	if maxAmountStr := c.QueryParam("max_amount"); maxAmountStr != "" {
		maxAmount, err := strconv.ParseFloat(maxAmountStr, 64)
		if err != nil {
			return echo.NewHTTPError(http.StatusBadRequest, "Invalid max_amount parameter (expected number)")
		}
		filters.MaxAmount = &maxAmount
	}

	// Amount type filter (Issue #529)
	if amountType := c.QueryParam("amount_type"); amountType != "" {
		validTypes := map[string]bool{"income": true, "expense": true, "all": true}
		if !validTypes[amountType] {
			return echo.NewHTTPError(http.StatusBadRequest, "Invalid amount_type parameter (expected income, expense, or all)")
		}
		// Don't set filter for "all" - it's the default behavior
		if amountType != "all" {
			filters.AmountType = &amountType
		}
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
		case errors.Is(err, services.ErrInvalidAmountRange):
			return echo.NewHTTPError(http.StatusBadRequest, "min_amount must be less than or equal to max_amount")
		default:
			c.Logger().Errorf("Failed to fetch transactions for user %s: %v", userID, err)
			return echo.NewHTTPError(http.StatusInternalServerError, "Failed to fetch transactions")
		}
	}

	return c.JSON(http.StatusOK, result)
}

// UpdateTransactionTagsRequest represents the request body for updating transaction tags
type UpdateTransactionTagsRequest struct {
	Tags []string `json:"tags"`
}

// UpdateTransactionTags handles PUT /api/v1/transactions/:id/tags
// Updates the tags on a specific transaction
func (h *TransactionHandler) UpdateTransactionTags(c echo.Context) error {
	userID, err := middleware.GetUserID(c)
	if err != nil {
		return echo.NewHTTPError(http.StatusUnauthorized, "User not authenticated")
	}

	transactionID := c.Param("id")
	if transactionID == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "Transaction ID is required")
	}

	var req UpdateTransactionTagsRequest
	if err := c.Bind(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "Invalid request body")
	}

	// Validate tags (optional: add max tag limit or validation)
	if req.Tags == nil {
		req.Tags = []string{}
	}

	tx, err := h.transactionService.UpdateTransactionTags(c.Request().Context(), userID, transactionID, req.Tags)
	if err != nil {
		if errors.Is(err, repository.ErrTransactionNotFound) {
			return echo.NewHTTPError(http.StatusNotFound, "Transaction not found")
		}
		c.Logger().Errorf("Failed to update transaction tags for user %s: %v", userID, err)
		return echo.NewHTTPError(http.StatusInternalServerError, "Failed to update transaction tags")
	}

	return c.JSON(http.StatusOK, tx)
}

// GetTransactionByID handles GET /api/v1/transactions/:id
// Retrieves a specific transaction by ID
func (h *TransactionHandler) GetTransactionByID(c echo.Context) error {
	userID, err := middleware.GetUserID(c)
	if err != nil {
		return echo.NewHTTPError(http.StatusUnauthorized, "User not authenticated")
	}

	transactionID := c.Param("id")
	if transactionID == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "Transaction ID is required")
	}

	tx, err := h.transactionService.GetTransactionByID(c.Request().Context(), userID, transactionID)
	if err != nil {
		if errors.Is(err, repository.ErrTransactionNotFound) {
			return echo.NewHTTPError(http.StatusNotFound, "Transaction not found")
		}
		c.Logger().Errorf("Failed to get transaction for user %s: %v", userID, err)
		return echo.NewHTTPError(http.StatusInternalServerError, "Failed to get transaction")
	}

	return c.JSON(http.StatusOK, tx)
}

// UpdateTransactionCategoryRequest represents the request body for updating transaction category
type UpdateTransactionCategoryRequest struct {
	Category []string `json:"category"`
}

// UpdateTransactionCategory handles PUT /api/v1/transactions/:id/category
// Updates the category override on a specific transaction
func (h *TransactionHandler) UpdateTransactionCategory(c echo.Context) error {
	userID, err := middleware.GetUserID(c)
	if err != nil {
		return echo.NewHTTPError(http.StatusUnauthorized, "User not authenticated")
	}

	transactionID := c.Param("id")
	if transactionID == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "Transaction ID is required")
	}

	var req UpdateTransactionCategoryRequest
	if err := c.Bind(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "Invalid request body")
	}

	// Validate category (optional: ensure non-nil)
	if req.Category == nil {
		req.Category = []string{}
	}

	tx, err := h.transactionService.UpdateTransactionCategory(c.Request().Context(), userID, transactionID, req.Category)
	if err != nil {
		if errors.Is(err, repository.ErrTransactionNotFound) {
			return echo.NewHTTPError(http.StatusNotFound, "Transaction not found")
		}
		c.Logger().Errorf("Failed to update transaction category for user %s: %v", userID, err)
		return echo.NewHTTPError(http.StatusInternalServerError, "Failed to update transaction category")
	}

	return c.JSON(http.StatusOK, tx)
}

// GetPlaidCategories handles GET /api/v1/categories/plaid
// Returns distinct Plaid category names from the user's transactions
// Uses primary category level (e.g., "Food and Drink") for consistency across the application
// Response: JSON array of category strings, sorted alphabetically
func (h *TransactionHandler) GetPlaidCategories(c echo.Context) error {
	userID, err := middleware.GetUserID(c)
	if err != nil {
		return echo.NewHTTPError(http.StatusUnauthorized, "User not authenticated")
	}

	categories, err := h.transactionService.GetDistinctPlaidCategories(c.Request().Context(), userID)
	if err != nil {
		c.Logger().Errorf("Failed to get plaid categories for user %s: %v", userID, err)
		return echo.NewHTTPError(http.StatusInternalServerError, "Failed to get Plaid categories")
	}

	return c.JSON(http.StatusOK, categories)
}
