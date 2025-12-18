package handlers

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/labstack/echo/v4"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/middleware"
	"github.com/maxnussbaum/BoostedBudget/internal/repository"
	"github.com/maxnussbaum/BoostedBudget/internal/services"
)

// BudgetHandler handles budget-related HTTP requests
type BudgetHandler struct {
	service         services.BudgetService
	syncService     services.BudgetSyncService        // Issue #494: Budget sync service
	transactionRepo repository.TransactionRepository // Issue #499: Transaction-Budget Linking UI
}

// NewBudgetHandler creates a new BudgetHandler instance
func NewBudgetHandler(service services.BudgetService, syncService services.BudgetSyncService, transactionRepo repository.TransactionRepository) *BudgetHandler {
	return &BudgetHandler{
		service:         service,
		syncService:     syncService,
		transactionRepo: transactionRepo,
	}
}

// CreateBudget handles POST /budgets
// Creates a new budget for the authenticated user
func (h *BudgetHandler) CreateBudget(c echo.Context) error {
	userID, err := middleware.GetUserID(c)
	if err != nil {
		return echo.NewHTTPError(http.StatusUnauthorized, "User not authenticated")
	}

	var req domain.CreateBudgetRequest
	if err := c.Bind(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "Invalid request body")
	}

	budget, err := h.service.CreateBudget(c.Request().Context(), userID, &req)
	if err != nil {
		if errors.Is(err, domain.ErrBudgetAlreadyExists) {
			return echo.NewHTTPError(http.StatusConflict, "A budget with this name already exists")
		}
		// Check for validation errors
		if err.Error() != "" && (errors.Is(err, domain.ErrBudgetAlreadyExists) == false) {
			return echo.NewHTTPError(http.StatusBadRequest, err.Error())
		}
		return echo.NewHTTPError(http.StatusInternalServerError, "Failed to create budget")
	}

	return c.JSON(http.StatusCreated, budget)
}

// GetBudgets handles GET /budgets
// Retrieves all budgets for the authenticated user
func (h *BudgetHandler) GetBudgets(c echo.Context) error {
	userID, err := middleware.GetUserID(c)
	if err != nil {
		return echo.NewHTTPError(http.StatusUnauthorized, "User not authenticated")
	}

	budgets, err := h.service.GetBudgets(c.Request().Context(), userID)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "Failed to retrieve budgets")
	}

	// Return empty array instead of null if no budgets
	if budgets == nil {
		budgets = []*domain.Budget{}
	}

	return c.JSON(http.StatusOK, budgets)
}

// GetBudget handles GET /budgets/:id
// Retrieves a specific budget by ID
func (h *BudgetHandler) GetBudget(c echo.Context) error {
	userID, err := middleware.GetUserID(c)
	if err != nil {
		return echo.NewHTTPError(http.StatusUnauthorized, "User not authenticated")
	}

	budgetID := c.Param("id")
	if budgetID == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "Budget ID is required")
	}

	budget, err := h.service.GetBudget(c.Request().Context(), userID, budgetID)
	if err != nil {
		if errors.Is(err, domain.ErrBudgetNotFound) {
			return echo.NewHTTPError(http.StatusNotFound, "Budget not found")
		}
		return echo.NewHTTPError(http.StatusInternalServerError, "Failed to retrieve budget")
	}

	return c.JSON(http.StatusOK, budget)
}

// GetCurrentBudget handles GET /budgets/current
// Retrieves the current active budget (where today is within the period)
func (h *BudgetHandler) GetCurrentBudget(c echo.Context) error {
	userID, err := middleware.GetUserID(c)
	if err != nil {
		return echo.NewHTTPError(http.StatusUnauthorized, "User not authenticated")
	}

	budget, err := h.service.GetCurrentBudget(c.Request().Context(), userID)
	if err != nil {
		if errors.Is(err, domain.ErrBudgetNotFound) {
			return echo.NewHTTPError(http.StatusNotFound, "No current budget found")
		}
		return echo.NewHTTPError(http.StatusInternalServerError, "Failed to retrieve current budget")
	}

	return c.JSON(http.StatusOK, budget)
}

// UpdateBudget handles PUT /budgets/:id
// Updates a budget's properties
func (h *BudgetHandler) UpdateBudget(c echo.Context) error {
	userID, err := middleware.GetUserID(c)
	if err != nil {
		return echo.NewHTTPError(http.StatusUnauthorized, "User not authenticated")
	}

	budgetID := c.Param("id")
	if budgetID == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "Budget ID is required")
	}

	var req domain.UpdateBudgetRequest
	if err := c.Bind(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "Invalid request body")
	}

	budget, err := h.service.UpdateBudget(c.Request().Context(), userID, budgetID, &req)
	if err != nil {
		if errors.Is(err, domain.ErrBudgetNotFound) {
			return echo.NewHTTPError(http.StatusNotFound, "Budget not found")
		}
		if errors.Is(err, domain.ErrBudgetAlreadyExists) {
			return echo.NewHTTPError(http.StatusConflict, "A budget with this name already exists")
		}
		return echo.NewHTTPError(http.StatusInternalServerError, "Failed to update budget")
	}

	return c.JSON(http.StatusOK, budget)
}

// DeleteBudget handles DELETE /budgets/:id
// Removes a budget from the user's collection
func (h *BudgetHandler) DeleteBudget(c echo.Context) error {
	userID, err := middleware.GetUserID(c)
	if err != nil {
		return echo.NewHTTPError(http.StatusUnauthorized, "User not authenticated")
	}

	budgetID := c.Param("id")
	if budgetID == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "Budget ID is required")
	}

	err = h.service.DeleteBudget(c.Request().Context(), userID, budgetID)
	if err != nil {
		if errors.Is(err, domain.ErrBudgetNotFound) {
			return echo.NewHTTPError(http.StatusNotFound, "Budget not found")
		}
		return echo.NewHTTPError(http.StatusInternalServerError, "Failed to delete budget")
	}

	return c.NoContent(http.StatusNoContent)
}

// AddCategory handles POST /budgets/:id/categories
// Adds a category to a budget
func (h *BudgetHandler) AddCategory(c echo.Context) error {
	userID, err := middleware.GetUserID(c)
	if err != nil {
		return echo.NewHTTPError(http.StatusUnauthorized, "User not authenticated")
	}

	budgetID := c.Param("id")
	if budgetID == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "Budget ID is required")
	}

	var req domain.CreateBudgetCategoryRequest
	if err := c.Bind(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "Invalid request body")
	}

	category, err := h.service.AddCategory(c.Request().Context(), userID, budgetID, &req)
	if err != nil {
		if errors.Is(err, domain.ErrBudgetNotFound) {
			return echo.NewHTTPError(http.StatusNotFound, "Budget not found")
		}
		return echo.NewHTTPError(http.StatusInternalServerError, "Failed to add category")
	}

	return c.JSON(http.StatusCreated, category)
}

// UpdateCategory handles PUT /budgets/:id/categories/:categoryId
// Updates a budget category
func (h *BudgetHandler) UpdateCategory(c echo.Context) error {
	userID, err := middleware.GetUserID(c)
	if err != nil {
		return echo.NewHTTPError(http.StatusUnauthorized, "User not authenticated")
	}

	budgetID := c.Param("id")
	if budgetID == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "Budget ID is required")
	}

	categoryID := c.Param("categoryId")
	if categoryID == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "Category ID is required")
	}

	var req domain.UpdateBudgetCategoryRequest
	if err := c.Bind(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "Invalid request body")
	}

	category, err := h.service.UpdateCategory(c.Request().Context(), userID, budgetID, categoryID, &req)
	if err != nil {
		if errors.Is(err, domain.ErrBudgetNotFound) {
			return echo.NewHTTPError(http.StatusNotFound, "Budget not found")
		}
		if errors.Is(err, domain.ErrBudgetCategoryNotFound) {
			return echo.NewHTTPError(http.StatusNotFound, "Category not found")
		}
		return echo.NewHTTPError(http.StatusInternalServerError, "Failed to update category")
	}

	return c.JSON(http.StatusOK, category)
}

// DeleteCategory handles DELETE /budgets/:id/categories/:categoryId
// Removes a category from a budget
func (h *BudgetHandler) DeleteCategory(c echo.Context) error {
	userID, err := middleware.GetUserID(c)
	if err != nil {
		return echo.NewHTTPError(http.StatusUnauthorized, "User not authenticated")
	}

	budgetID := c.Param("id")
	if budgetID == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "Budget ID is required")
	}

	categoryID := c.Param("categoryId")
	if categoryID == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "Category ID is required")
	}

	err = h.service.DeleteCategory(c.Request().Context(), userID, budgetID, categoryID)
	if err != nil {
		if errors.Is(err, domain.ErrBudgetNotFound) {
			return echo.NewHTTPError(http.StatusNotFound, "Budget not found")
		}
		if errors.Is(err, domain.ErrBudgetCategoryNotFound) {
			return echo.NewHTTPError(http.StatusNotFound, "Category not found")
		}
		return echo.NewHTTPError(http.StatusInternalServerError, "Failed to delete category")
	}

	return c.NoContent(http.StatusNoContent)
}

// GetAvailableCategories handles GET /budgets/categories/available
// Returns the merged list of available categories (custom + Plaid)
func (h *BudgetHandler) GetAvailableCategories(c echo.Context) error {
	userID, err := middleware.GetUserID(c)
	if err != nil {
		return echo.NewHTTPError(http.StatusUnauthorized, "User not authenticated")
	}

	categories, err := h.service.GetAvailableCategories(c.Request().Context(), userID)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "Failed to retrieve available categories")
	}

	// Return empty array instead of null
	if categories == nil {
		categories = []string{}
	}

	return c.JSON(http.StatusOK, categories)
}

// SyncBudget handles POST /budgets/:id/sync
// Manually triggers recalculation of budget income and spent amounts
// Issue #494: Budget Sync Service
func (h *BudgetHandler) SyncBudget(c echo.Context) error {
	userID, err := middleware.GetUserID(c)
	if err != nil {
		return echo.NewHTTPError(http.StatusUnauthorized, "User not authenticated")
	}

	budgetID := c.Param("id")
	if budgetID == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "Budget ID is required")
	}

	// Get the budget to verify ownership and pass to sync service
	budget, err := h.service.GetBudget(c.Request().Context(), userID, budgetID)
	if err != nil {
		if errors.Is(err, domain.ErrBudgetNotFound) {
			return echo.NewHTTPError(http.StatusNotFound, "Budget not found")
		}
		return echo.NewHTTPError(http.StatusInternalServerError, "Failed to get budget")
	}

	// Recalculate budget amounts
	if err := h.syncService.RecalculateBudget(c.Request().Context(), budget); err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "Failed to sync budget")
	}

	// Re-fetch to get updated amounts
	budget, err = h.service.GetBudget(c.Request().Context(), userID, budgetID)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "Failed to get updated budget")
	}

	return c.JSON(http.StatusOK, budget)
}

// GetCategoryTransactions handles GET /budgets/:id/categories/:categoryId/transactions
// Returns paginated transactions matching a budget category
// Issue #499: Transaction-Budget Linking UI
func (h *BudgetHandler) GetCategoryTransactions(c echo.Context) error {
	userID, err := middleware.GetUserID(c)
	if err != nil {
		return echo.NewHTTPError(http.StatusUnauthorized, "User not authenticated")
	}

	budgetID := c.Param("id")
	if budgetID == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "Budget ID is required")
	}

	categoryID := c.Param("categoryId")
	if categoryID == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "Category ID is required")
	}

	// Parse query params
	limit := 20 // default
	if l := c.QueryParam("limit"); l != "" {
		if parsed, err := strconv.Atoi(l); err == nil {
			limit = parsed
			if limit < 1 {
				limit = 1
			}
			if limit > 100 {
				limit = 100
			}
		}
	}

	offset := 0 // default
	if o := c.QueryParam("offset"); o != "" {
		if parsed, err := strconv.Atoi(o); err == nil {
			offset = parsed
			if offset < 0 {
				offset = 0
			}
		}
	}

	// Get the budget to verify ownership and get the date range
	budget, err := h.service.GetBudget(c.Request().Context(), userID, budgetID)
	if err != nil {
		if errors.Is(err, domain.ErrBudgetNotFound) {
			return echo.NewHTTPError(http.StatusNotFound, "Budget not found")
		}
		return echo.NewHTTPError(http.StatusInternalServerError, "Failed to get budget")
	}

	// Find the category
	var category *domain.BudgetCategory
	for i := range budget.Categories {
		if budget.Categories[i].ID == categoryID {
			category = &budget.Categories[i]
			break
		}
	}
	if category == nil {
		return echo.NewHTTPError(http.StatusNotFound, "Category not found")
	}

	// Determine end date (use EndDate if set, otherwise use current time)
	endDate := budget.StartDate // fallback
	if budget.EndDate != nil {
		endDate = *budget.EndDate
	}

	// Get transactions matching the category
	transactions, totalCount, totalAmount, err := h.transactionRepo.GetTransactionsForBudgetCategory(
		c.Request().Context(),
		userID,
		category.Category,
		category.Tags,
		category.MatchLogic,
		budget.StartDate,
		endDate,
		limit,
		offset,
	)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "Failed to get transactions")
	}

	// Convert to pointer slice for response
	transactionPtrs := make([]*domain.Transaction, len(transactions))
	for i := range transactions {
		transactionPtrs[i] = &transactions[i]
	}

	response := domain.CategoryTransactionsResponse{
		Transactions: transactionPtrs,
		TotalCount:   totalCount,
		TotalAmount:  totalAmount,
		HasMore:      offset+len(transactions) < totalCount,
	}

	return c.JSON(http.StatusOK, response)
}
