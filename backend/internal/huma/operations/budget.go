package operations

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	humaerrors "github.com/maxnussbaum/BoostedBudget/internal/huma/errors"
	"github.com/maxnussbaum/BoostedBudget/internal/huma/inputs"
	"github.com/maxnussbaum/BoostedBudget/internal/services"
)

// BudgetHandler handles budget-related HTTP requests.
// Issue #28: Budget management
type BudgetHandler struct {
	service            services.BudgetService
	syncService        services.BudgetSyncService
	transactionService services.TransactionService
}

// NewBudgetHandler creates a new BudgetHandler instance.
func NewBudgetHandler(service services.BudgetService, syncService services.BudgetSyncService, transactionService services.TransactionService) *BudgetHandler {
	return &BudgetHandler{
		service:            service,
		syncService:        syncService,
		transactionService: transactionService,
	}
}

// --- Input/Output Types ---

// CreateBudgetInput represents the input for creating a budget.
type CreateBudgetInput struct {
	inputs.AuthenticatedInput
	Body domain.CreateBudgetRequest
}

// CreateBudgetOutput represents the response for creating a budget.
type CreateBudgetOutput struct {
	Body *domain.Budget
}

// GetBudgetsInput represents the input for getting all budgets.
type GetBudgetsInput struct {
	inputs.AuthenticatedInput
}

// GetBudgetsOutput represents the response for getting all budgets.
type GetBudgetsOutput struct {
	Body []*domain.Budget
}

// GetBudgetInput represents the input for getting a specific budget.
type GetBudgetInput struct {
	inputs.AuthenticatedInput
	ID string `path:"id" doc:"Budget ID"`
}

// GetBudgetOutput represents the response for getting a budget.
type GetBudgetOutput struct {
	Body *domain.Budget
}

// GetCurrentBudgetInput represents the input for getting the current budget.
type GetCurrentBudgetInput struct {
	inputs.AuthenticatedInput
}

// GetCurrentBudgetOutput represents the response for getting the current budget.
type GetCurrentBudgetOutput struct {
	Body *domain.Budget
}

// UpdateBudgetInput represents the input for updating a budget.
type UpdateBudgetInput struct {
	inputs.AuthenticatedInput
	ID   string `path:"id" doc:"Budget ID"`
	Body domain.UpdateBudgetRequest
}

// UpdateBudgetOutput represents the response for updating a budget.
type UpdateBudgetOutput struct {
	Body *domain.Budget
}

// DeleteBudgetInput represents the input for deleting a budget.
type DeleteBudgetInput struct {
	inputs.AuthenticatedInput
	ID string `path:"id" doc:"Budget ID"`
}

// AddCategoryInput represents the input for adding a category to a budget.
type AddCategoryInput struct {
	inputs.AuthenticatedInput
	ID   string `path:"id" doc:"Budget ID"`
	Body domain.CreateBudgetCategoryRequest
}

// AddCategoryOutput represents the response for adding a category.
type AddCategoryOutput struct {
	Body *domain.BudgetCategory
}

// UpdateBudgetCategoryInput represents the input for updating a budget category.
type UpdateBudgetCategoryInput struct {
	inputs.AuthenticatedInput
	ID         string `path:"id" doc:"Budget ID"`
	CategoryID string `path:"categoryId" doc:"Category ID"`
	Body       domain.UpdateBudgetCategoryRequest
}

// UpdateBudgetCategoryOutput represents the response for updating a category.
type UpdateBudgetCategoryOutput struct {
	Body *domain.BudgetCategory
}

// DeleteBudgetCategoryInput represents the input for deleting a budget category.
type DeleteBudgetCategoryInput struct {
	inputs.AuthenticatedInput
	ID         string `path:"id" doc:"Budget ID"`
	CategoryID string `path:"categoryId" doc:"Category ID"`
}

// GetAvailableCategoriesInput represents the input for getting available categories.
type GetAvailableCategoriesInput struct {
	inputs.AuthenticatedInput
}

// GetAvailableCategoriesOutput represents the response for getting available categories.
type GetAvailableCategoriesOutput struct {
	Body []string
}

// SyncBudgetInput represents the input for syncing a budget.
type SyncBudgetInput struct {
	inputs.AuthenticatedInput
	ID string `path:"id" doc:"Budget ID"`
}

// SyncBudgetOutput represents the response for syncing a budget.
type SyncBudgetOutput struct {
	Body *domain.Budget
}

// GetCategoryTransactionsInput represents the input for getting category transactions.
type GetCategoryTransactionsInput struct {
	inputs.AuthenticatedInput
	ID         string `path:"id" doc:"Budget ID"`
	CategoryID string `path:"categoryId" doc:"Category ID"`
	Limit      int    `query:"limit" minimum:"1" maximum:"100" default:"20" doc:"Number of transactions to return"`
	Offset     int    `query:"offset" minimum:"0" default:"0" doc:"Offset for pagination"`
}

// GetCategoryTransactionsOutput represents the response for getting category transactions.
type GetCategoryTransactionsOutput struct {
	Body *domain.CategoryTransactionsResponse
}

// --- Handler Methods ---

// CreateBudget handles POST /api/v1/budgets requests.
// Creates a new budget for the authenticated user.
func (h *BudgetHandler) CreateBudget(ctx context.Context, input *CreateBudgetInput) (*CreateBudgetOutput, error) {
	budget, err := h.service.CreateBudget(ctx, input.UserID, &input.Body)
	if err != nil {
		return nil, humaerrors.LoggingMapError(ctx, err)
	}

	return &CreateBudgetOutput{Body: budget}, nil
}

// GetBudgets handles GET /api/v1/budgets requests.
// Retrieves all budgets for the authenticated user.
func (h *BudgetHandler) GetBudgets(ctx context.Context, input *GetBudgetsInput) (*GetBudgetsOutput, error) {
	budgets, err := h.service.GetBudgets(ctx, input.UserID)
	if err != nil {
		return nil, humaerrors.LoggingMapError(ctx, err)
	}

	// Return empty array instead of null
	if budgets == nil {
		budgets = []*domain.Budget{}
	}

	return &GetBudgetsOutput{Body: budgets}, nil
}

// GetBudget handles GET /api/v1/budgets/:id requests.
// Retrieves a specific budget by ID.
func (h *BudgetHandler) GetBudget(ctx context.Context, input *GetBudgetInput) (*GetBudgetOutput, error) {
	budget, err := h.service.GetBudget(ctx, input.UserID, input.ID)
	if err != nil {
		return nil, humaerrors.LoggingMapError(ctx, err)
	}

	return &GetBudgetOutput{Body: budget}, nil
}

// GetCurrentBudget handles GET /api/v1/budgets/current requests.
// Retrieves the current active budget.
func (h *BudgetHandler) GetCurrentBudget(ctx context.Context, input *GetCurrentBudgetInput) (*GetCurrentBudgetOutput, error) {
	budget, err := h.service.GetCurrentBudget(ctx, input.UserID)
	if err != nil {
		return nil, humaerrors.LoggingMapError(ctx, err)
	}

	return &GetCurrentBudgetOutput{Body: budget}, nil
}

// UpdateBudget handles PUT /api/v1/budgets/:id requests.
// Updates a budget's properties.
func (h *BudgetHandler) UpdateBudget(ctx context.Context, input *UpdateBudgetInput) (*UpdateBudgetOutput, error) {
	budget, err := h.service.UpdateBudget(ctx, input.UserID, input.ID, &input.Body)
	if err != nil {
		return nil, humaerrors.LoggingMapError(ctx, err)
	}

	return &UpdateBudgetOutput{Body: budget}, nil
}

// DeleteBudget handles DELETE /api/v1/budgets/:id requests.
// Removes a budget from the user's collection.
func (h *BudgetHandler) DeleteBudget(ctx context.Context, input *DeleteBudgetInput) (*struct{}, error) {
	err := h.service.DeleteBudget(ctx, input.UserID, input.ID)
	if err != nil {
		return nil, humaerrors.LoggingMapError(ctx, err)
	}

	return nil, nil
}

// AddCategory handles POST /api/v1/budgets/:id/categories requests.
// Adds a category to a budget.
func (h *BudgetHandler) AddCategory(ctx context.Context, input *AddCategoryInput) (*AddCategoryOutput, error) {
	category, err := h.service.AddCategory(ctx, input.UserID, input.ID, &input.Body)
	if err != nil {
		return nil, humaerrors.LoggingMapError(ctx, err)
	}

	return &AddCategoryOutput{Body: category}, nil
}

// UpdateCategory handles PUT /api/v1/budgets/:id/categories/:categoryId requests.
// Updates a budget category.
func (h *BudgetHandler) UpdateCategory(ctx context.Context, input *UpdateBudgetCategoryInput) (*UpdateBudgetCategoryOutput, error) {
	category, err := h.service.UpdateCategory(ctx, input.UserID, input.ID, input.CategoryID, &input.Body)
	if err != nil {
		return nil, humaerrors.LoggingMapError(ctx, err)
	}

	return &UpdateBudgetCategoryOutput{Body: category}, nil
}

// DeleteCategory handles DELETE /api/v1/budgets/:id/categories/:categoryId requests.
// Removes a category from a budget.
func (h *BudgetHandler) DeleteCategory(ctx context.Context, input *DeleteBudgetCategoryInput) (*struct{}, error) {
	err := h.service.DeleteCategory(ctx, input.UserID, input.ID, input.CategoryID)
	if err != nil {
		return nil, humaerrors.LoggingMapError(ctx, err)
	}

	return nil, nil
}

// GetAvailableCategories handles GET /api/v1/budgets/categories/available requests.
// Returns the merged list of available categories (custom + Plaid).
func (h *BudgetHandler) GetAvailableCategories(ctx context.Context, input *GetAvailableCategoriesInput) (*GetAvailableCategoriesOutput, error) {
	categories, err := h.service.GetAvailableCategories(ctx, input.UserID)
	if err != nil {
		return nil, humaerrors.LoggingMapError(ctx, err)
	}

	// Return empty array instead of null
	if categories == nil {
		categories = []string{}
	}

	return &GetAvailableCategoriesOutput{Body: categories}, nil
}

// SyncBudget handles POST /api/v1/budgets/:id/sync requests.
// Manually triggers recalculation of budget income and spent amounts.
func (h *BudgetHandler) SyncBudget(ctx context.Context, input *SyncBudgetInput) (*SyncBudgetOutput, error) {
	// Get the budget to verify ownership and pass to sync service
	budget, err := h.service.GetBudget(ctx, input.UserID, input.ID)
	if err != nil {
		return nil, humaerrors.LoggingMapError(ctx, err)
	}

	// Recalculate budget amounts
	if err := h.syncService.RecalculateBudget(ctx, budget); err != nil {
		return nil, huma.Error500InternalServerError("Failed to sync budget")
	}

	// Re-fetch to get updated amounts
	budget, err = h.service.GetBudget(ctx, input.UserID, input.ID)
	if err != nil {
		return nil, humaerrors.LoggingMapError(ctx, err)
	}

	return &SyncBudgetOutput{Body: budget}, nil
}

// GetCategoryTransactions handles GET /api/v1/budgets/:id/categories/:categoryId/transactions requests.
// Returns paginated transactions matching a budget category.
func (h *BudgetHandler) GetCategoryTransactions(ctx context.Context, input *GetCategoryTransactionsInput) (*GetCategoryTransactionsOutput, error) {
	// Get the budget to verify ownership and get the date range
	budget, err := h.service.GetBudget(ctx, input.UserID, input.ID)
	if err != nil {
		return nil, humaerrors.LoggingMapError(ctx, err)
	}

	// Find the category
	var category *domain.BudgetCategory
	for i := range budget.Categories {
		if budget.Categories[i].ID == input.CategoryID {
			category = &budget.Categories[i]
			break
		}
	}
	if category == nil {
		return nil, huma.Error404NotFound("Category not found")
	}

	// Determine end date (use EndDate if set, otherwise use current time)
	endDate := budget.StartDate // fallback
	if budget.EndDate != nil {
		endDate = *budget.EndDate
	}

	// Get transactions matching the category
	transactions, totalCount, totalAmount, err := h.transactionService.ListByBudgetCategory(
		ctx,
		input.UserID,
		category,
		budget.StartDate,
		endDate,
		input.Limit,
		input.Offset,
	)
	if err != nil {
		return nil, huma.Error500InternalServerError("failed to get category transactions")
	}

	// Convert to pointer slice (enrichment done by service)
	transactionPtrs := make([]*domain.Transaction, len(transactions))
	for i := range transactions {
		transactionPtrs[i] = &transactions[i]
	}

	response := &domain.CategoryTransactionsResponse{
		Transactions: transactionPtrs,
		TotalCount:   totalCount,
		TotalAmount:  totalAmount,
		HasMore:      input.Offset+len(transactions) < totalCount,
	}

	return &GetCategoryTransactionsOutput{Body: response}, nil
}

// RegisterBudgetRoutes registers budget routes with the Huma API.
// Note: These routes require the Clerk middleware to be applied.
func RegisterBudgetRoutes(api huma.API, handler *BudgetHandler) {
	// POST /api/v1/budgets - Create budget
	huma.Register(api, huma.Operation{
		OperationID:   "createBudget",
		Method:        http.MethodPost,
		Path:          "/api/v1/budgets",
		Summary:       "Create budget",
		Description:   "Creates a new budget for the authenticated user.",
		Tags:          []string{"Budgets"},
		Security:      []map[string][]string{{"clerk": {}}},
		DefaultStatus: http.StatusCreated,
	}, handler.CreateBudget)

	// GET /api/v1/budgets - Get all budgets
	huma.Register(api, huma.Operation{
		OperationID: "getBudgets",
		Method:      http.MethodGet,
		Path:        "/api/v1/budgets",
		Summary:     "Get budgets",
		Description: "Retrieves all budgets for the authenticated user.",
		Tags:        []string{"Budgets"},
		Security:    []map[string][]string{{"clerk": {}}},
	}, handler.GetBudgets)

	// GET /api/v1/budgets/current - Get current budget (must be before :id route)
	huma.Register(api, huma.Operation{
		OperationID: "getCurrentBudget",
		Method:      http.MethodGet,
		Path:        "/api/v1/budgets/current",
		Summary:     "Get current budget",
		Description: "Retrieves the current active budget (where today is within the period).",
		Tags:        []string{"Budgets"},
		Security:    []map[string][]string{{"clerk": {}}},
	}, handler.GetCurrentBudget)

	// GET /api/v1/budgets/categories/available - Get available categories (must be before :id route)
	huma.Register(api, huma.Operation{
		OperationID: "getAvailableCategories",
		Method:      http.MethodGet,
		Path:        "/api/v1/budgets/categories/available",
		Summary:     "Get available categories",
		Description: "Returns the merged list of available categories (custom + Plaid).",
		Tags:        []string{"Budgets"},
		Security:    []map[string][]string{{"clerk": {}}},
	}, handler.GetAvailableCategories)

	// GET /api/v1/budgets/:id - Get specific budget
	huma.Register(api, huma.Operation{
		OperationID: "getBudget",
		Method:      http.MethodGet,
		Path:        "/api/v1/budgets/{id}",
		Summary:     "Get budget",
		Description: "Retrieves a specific budget by ID.",
		Tags:        []string{"Budgets"},
		Security:    []map[string][]string{{"clerk": {}}},
	}, handler.GetBudget)

	// PUT /api/v1/budgets/:id - Update budget
	huma.Register(api, huma.Operation{
		OperationID: "updateBudget",
		Method:      http.MethodPut,
		Path:        "/api/v1/budgets/{id}",
		Summary:     "Update budget",
		Description: "Updates a budget's properties.",
		Tags:        []string{"Budgets"},
		Security:    []map[string][]string{{"clerk": {}}},
	}, handler.UpdateBudget)

	// DELETE /api/v1/budgets/:id - Delete budget
	huma.Register(api, huma.Operation{
		OperationID:   "deleteBudget",
		Method:        http.MethodDelete,
		Path:          "/api/v1/budgets/{id}",
		Summary:       "Delete budget",
		Description:   "Removes a budget from the user's collection.",
		Tags:          []string{"Budgets"},
		Security:      []map[string][]string{{"clerk": {}}},
		DefaultStatus: http.StatusNoContent,
	}, handler.DeleteBudget)

	// POST /api/v1/budgets/:id/categories - Add category
	huma.Register(api, huma.Operation{
		OperationID:   "addBudgetCategory",
		Method:        http.MethodPost,
		Path:          "/api/v1/budgets/{id}/categories",
		Summary:       "Add budget category",
		Description:   "Adds a category to a budget.",
		Tags:          []string{"Budgets"},
		Security:      []map[string][]string{{"clerk": {}}},
		DefaultStatus: http.StatusCreated,
	}, handler.AddCategory)

	// PUT /api/v1/budgets/:id/categories/:categoryId - Update category
	huma.Register(api, huma.Operation{
		OperationID: "updateBudgetCategory",
		Method:      http.MethodPut,
		Path:        "/api/v1/budgets/{id}/categories/{categoryId}",
		Summary:     "Update budget category",
		Description: "Updates a budget category.",
		Tags:        []string{"Budgets"},
		Security:    []map[string][]string{{"clerk": {}}},
	}, handler.UpdateCategory)

	// DELETE /api/v1/budgets/:id/categories/:categoryId - Delete category
	huma.Register(api, huma.Operation{
		OperationID:   "deleteBudgetCategory",
		Method:        http.MethodDelete,
		Path:          "/api/v1/budgets/{id}/categories/{categoryId}",
		Summary:       "Delete budget category",
		Description:   "Removes a category from a budget.",
		Tags:          []string{"Budgets"},
		Security:      []map[string][]string{{"clerk": {}}},
		DefaultStatus: http.StatusNoContent,
	}, handler.DeleteCategory)

	// POST /api/v1/budgets/:id/sync - Sync budget
	huma.Register(api, huma.Operation{
		OperationID: "syncBudget",
		Method:      http.MethodPost,
		Path:        "/api/v1/budgets/{id}/sync",
		Summary:     "Sync budget",
		Description: "Manually triggers recalculation of budget income and spent amounts.",
		Tags:        []string{"Budgets"},
		Security:    []map[string][]string{{"clerk": {}}},
	}, handler.SyncBudget)

	// GET /api/v1/budgets/:id/categories/:categoryId/transactions - Get category transactions
	huma.Register(api, huma.Operation{
		OperationID: "getCategoryTransactions",
		Method:      http.MethodGet,
		Path:        "/api/v1/budgets/{id}/categories/{categoryId}/transactions",
		Summary:     "Get category transactions",
		Description: "Returns paginated transactions matching a budget category.",
		Tags:        []string{"Budgets"},
		Security:    []map[string][]string{{"clerk": {}}},
	}, handler.GetCategoryTransactions)
}
