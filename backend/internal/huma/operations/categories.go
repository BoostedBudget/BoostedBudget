package operations

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	humaerrors "github.com/maxnussbaum/BoostedBudget/internal/huma/errors"
	"github.com/maxnussbaum/BoostedBudget/internal/huma/inputs"
	"github.com/maxnussbaum/BoostedBudget/internal/pfc"
	"github.com/maxnussbaum/BoostedBudget/internal/services"
)

// CategoryHandler handles custom category-related HTTP requests.
type CategoryHandler struct {
	service services.CustomCategoryService
}

// NewCategoryHandler creates a new CategoryHandler instance.
func NewCategoryHandler(service services.CustomCategoryService) *CategoryHandler {
	return &CategoryHandler{service: service}
}

// --- Input/Output Types ---

// GetUserCategoriesInput represents the input for getting user categories.
type GetUserCategoriesInput struct {
	inputs.AuthenticatedInput
}

// GetUserCategoriesOutput represents the response for getting user categories.
type GetUserCategoriesOutput struct {
	Body []*domain.CustomCategory
}

// GetCategoryByIDInput represents the input for getting a category by ID.
type GetCategoryByIDInput struct {
	inputs.AuthenticatedInput
	ID string `path:"id" doc:"Category UUID"`
}

// GetCategoryByIDOutput represents the response for getting a category by ID.
type GetCategoryByIDOutput struct {
	Body *domain.CustomCategory
}

// CreateCategoryInput represents the input for creating a category.
type CreateCategoryInput struct {
	inputs.AuthenticatedInput
	Body domain.CreateCustomCategoryRequest
}

// CreateCategoryOutput represents the response for creating a category.
type CreateCategoryOutput struct {
	Body *domain.CustomCategory
}

// UpdateCategoryInput represents the input for updating a category.
type UpdateCategoryInput struct {
	inputs.AuthenticatedInput
	ID   string `path:"id" doc:"Category UUID"`
	Body domain.UpdateCustomCategoryRequest
}

// UpdateCategoryOutput represents the response for updating a category.
type UpdateCategoryOutput struct {
	Body *domain.CustomCategory
}

// DeleteCategoryInput represents the input for deleting a category.
type DeleteCategoryInput struct {
	inputs.AuthenticatedInput
	ID string `path:"id" doc:"Category UUID"`
}

// GetPlaidPrimaryCategoriesInput represents the input for listing curated PFC primaries.
type GetPlaidPrimaryCategoriesInput struct {
	inputs.AuthenticatedInput
}

// GetPlaidPrimaryCategoriesOutput represents the response for listing curated PFC primaries.
type GetPlaidPrimaryCategoriesOutput struct {
	Body []pfc.Primary
}

// GetPlaidDetailedCategoriesInput represents the input for listing curated PFC detailed categories.
type GetPlaidDetailedCategoriesInput struct {
	inputs.AuthenticatedInput
}

// GetPlaidDetailedCategoriesOutput represents the response for listing curated PFC detailed categories.
type GetPlaidDetailedCategoriesOutput struct {
	Body []pfc.Detailed
}

// --- Handler Methods ---

// GetUserCategories handles GET /api/v1/categories requests.
// Retrieves all custom categories for the authenticated user.
func (h *CategoryHandler) GetUserCategories(ctx context.Context, input *GetUserCategoriesInput) (*GetUserCategoriesOutput, error) {
	categories, err := h.service.GetUserCategories(ctx, input.UserID)
	if err != nil {
		return nil, humaerrors.LoggingMapError(ctx, err)
	}

	// Return empty array instead of null if no categories
	if categories == nil {
		categories = []*domain.CustomCategory{}
	}

	return &GetUserCategoriesOutput{Body: categories}, nil
}

// GetCategoryByID handles GET /api/v1/categories/:id requests.
// Retrieves a specific category by ID.
func (h *CategoryHandler) GetCategoryByID(ctx context.Context, input *GetCategoryByIDInput) (*GetCategoryByIDOutput, error) {
	category, err := h.service.GetCategoryByID(ctx, input.ID)
	if err != nil {
		return nil, humaerrors.LoggingMapError(ctx, err)
	}

	// Verify ownership
	if category.UserID != input.UserID {
		return nil, huma.Error404NotFound("Category not found")
	}

	return &GetCategoryByIDOutput{Body: category}, nil
}

// CreateCategory handles POST /api/v1/categories requests.
// Creates a new custom category for the authenticated user.
func (h *CategoryHandler) CreateCategory(ctx context.Context, input *CreateCategoryInput) (*CreateCategoryOutput, error) {
	category, err := h.service.CreateCategory(ctx, input.UserID, &input.Body)
	if err != nil {
		return nil, humaerrors.LoggingMapError(ctx, err)
	}

	return &CreateCategoryOutput{Body: category}, nil
}

// UpdateCategory handles PUT /api/v1/categories/:id requests.
// Updates a category's name, icon, or color.
func (h *CategoryHandler) UpdateCategory(ctx context.Context, input *UpdateCategoryInput) (*UpdateCategoryOutput, error) {
	category, err := h.service.UpdateCategory(ctx, input.UserID, input.ID, &input.Body)
	if err != nil {
		return nil, humaerrors.LoggingMapError(ctx, err)
	}

	return &UpdateCategoryOutput{Body: category}, nil
}

// DeleteCategory handles DELETE /api/v1/categories/:id requests.
// Removes a category from the user's collection.
func (h *CategoryHandler) DeleteCategory(ctx context.Context, input *DeleteCategoryInput) (*struct{}, error) {
	err := h.service.DeleteCategory(ctx, input.UserID, input.ID)
	if err != nil {
		return nil, humaerrors.LoggingMapError(ctx, err)
	}

	return nil, nil
}

// GetPlaidPrimaryCategories handles GET /api/v1/categories/plaid-primaries requests.
// Returns all 18 PFCv2 curated primary categories, sorted alphabetically by display name.
// The handler reads the static pfc.AllPrimaries() registry directly — no DB, no service call.
func (h *CategoryHandler) GetPlaidPrimaryCategories(_ context.Context, _ *GetPlaidPrimaryCategoriesInput) (*GetPlaidPrimaryCategoriesOutput, error) {
	return &GetPlaidPrimaryCategoriesOutput{Body: pfc.AllPrimaries()}, nil
}

// GetPlaidDetailedCategories handles GET /api/v1/categories/plaid-detailed requests.
// Returns the full PFCv2 detailed taxonomy (~120 entries), sorted by primary display then display.
// The handler reads the static pfc.AllDetailed() registry directly — no DB, no service call.
func (h *CategoryHandler) GetPlaidDetailedCategories(_ context.Context, _ *GetPlaidDetailedCategoriesInput) (*GetPlaidDetailedCategoriesOutput, error) {
	return &GetPlaidDetailedCategoriesOutput{Body: pfc.AllDetailed()}, nil
}

// RegisterCategoryRoutes registers category routes with the Huma API.
// Note: These routes require the Clerk middleware to be applied.
func RegisterCategoryRoutes(api huma.API, handler *CategoryHandler) {
	// GET /api/v1/categories - Get all user categories
	huma.Register(api, huma.Operation{
		OperationID: "getUserCategories",
		Method:      http.MethodGet,
		Path:        "/api/v1/categories",
		Summary:     "Get user categories",
		Description: "Retrieves all custom categories for the authenticated user.",
		Tags:        []string{"Categories"},
		Security:    []map[string][]string{{"clerk": {}}},
	}, handler.GetUserCategories)

	// GET /api/v1/categories/{id} - Get category by ID
	huma.Register(api, huma.Operation{
		OperationID: "getCategoryByID",
		Method:      http.MethodGet,
		Path:        "/api/v1/categories/{id}",
		Summary:     "Get category by ID",
		Description: "Retrieves a specific category by its UUID.",
		Tags:        []string{"Categories"},
		Security:    []map[string][]string{{"clerk": {}}},
	}, handler.GetCategoryByID)

	// POST /api/v1/categories - Create a new category
	huma.Register(api, huma.Operation{
		OperationID:   "createCategory",
		Method:        http.MethodPost,
		Path:          "/api/v1/categories",
		Summary:       "Create a new category",
		Description:   "Creates a new custom category for the authenticated user.",
		Tags:          []string{"Categories"},
		Security:      []map[string][]string{{"clerk": {}}},
		DefaultStatus: http.StatusCreated,
	}, handler.CreateCategory)

	// PUT /api/v1/categories/{id} - Update a category
	huma.Register(api, huma.Operation{
		OperationID: "updateCategory",
		Method:      http.MethodPut,
		Path:        "/api/v1/categories/{id}",
		Summary:     "Update a category",
		Description: "Updates a category's name, icon, or color.",
		Tags:        []string{"Categories"},
		Security:    []map[string][]string{{"clerk": {}}},
	}, handler.UpdateCategory)

	// DELETE /api/v1/categories/{id} - Delete a category
	huma.Register(api, huma.Operation{
		OperationID:   "deleteCategory",
		Method:        http.MethodDelete,
		Path:          "/api/v1/categories/{id}",
		Summary:       "Delete a category",
		Description:   "Removes a category from the user's collection.",
		Tags:          []string{"Categories"},
		Security:      []map[string][]string{{"clerk": {}}},
		DefaultStatus: http.StatusNoContent,
	}, handler.DeleteCategory)

	// GET /api/v1/categories/plaid-primaries - List curated PFCv2 primary categories
	huma.Register(api, huma.Operation{
		OperationID: "getPlaidPrimaryCategories",
		Method:      http.MethodGet,
		Path:        "/api/v1/categories/plaid-primaries",
		Summary:     "List curated Plaid PFC primary categories",
		Description: "Returns all 18 PFCv2 curated primary categories sorted alphabetically. Used by every PFC selection surface.",
		Tags:        []string{"Categories"},
		Security:    []map[string][]string{{"clerk": {}}},
	}, handler.GetPlaidPrimaryCategories)

	// GET /api/v1/categories/plaid-detailed - List curated PFCv2 detailed categories
	huma.Register(api, huma.Operation{
		OperationID: "getPlaidDetailedCategories",
		Method:      http.MethodGet,
		Path:        "/api/v1/categories/plaid-detailed",
		Summary:     "List curated Plaid PFC detailed categories",
		Description: "Returns the full PFCv2 detailed taxonomy (~120 entries) sorted by primary then detailed display name. Used only by the rule editor's existing_pfc_detailed condition.",
		Tags:        []string{"Categories"},
		Security:    []map[string][]string{{"clerk": {}}},
	}, handler.GetPlaidDetailedCategories)
}
