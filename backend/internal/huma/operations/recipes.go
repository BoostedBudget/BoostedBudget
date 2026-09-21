package operations

// MCP recipe endpoints (Issue #1087)
// Recipes are saved, named sequences of read-only API calls.
// v1: read-only compositions only; write-recipes deferred to follow-up.

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	humaerrors "github.com/maxnussbaum/BoostedBudget/internal/huma/errors"
	"github.com/maxnussbaum/BoostedBudget/internal/huma/inputs"
	"github.com/maxnussbaum/BoostedBudget/internal/repository"
	"github.com/maxnussbaum/BoostedBudget/internal/services"
)

// RecipeHandler handles MCP recipe HTTP requests.
type RecipeHandler struct {
	service         services.RecipeService
	idempotencyRepo repository.IdempotencyRepository // optional; nil disables dedup
}

// NewRecipeHandler creates a new RecipeHandler.
// idempotencyRepo is optional (pass nil for spec-gen nil-stubs); when non-nil,
// createMCPRecipe requests with an Idempotency-Key header are deduplicated.
func NewRecipeHandler(service services.RecipeService, idempotencyRepo ...repository.IdempotencyRepository) *RecipeHandler {
	h := &RecipeHandler{service: service}
	if len(idempotencyRepo) > 0 {
		h.idempotencyRepo = idempotencyRepo[0]
	}
	return h
}

// --- Input / Output types ---

type CreateRecipeInput struct {
	inputs.AuthenticatedInput
	inputs.IdempotencyInput // reads Idempotency-Key header; empty = no deduplication
	Body domain.CreateMCPRecipeRequest
}
type CreateRecipeOutput struct{ Body *domain.MCPRecipe }

type ListRecipesInput struct {
	inputs.AuthenticatedInput
}
type ListRecipesOutput struct{ Body []domain.MCPRecipe }

type GetRecipeInput struct {
	inputs.AuthenticatedInput
	ID string `path:"id" doc:"Recipe ID"`
}
type GetRecipeOutput struct{ Body *domain.MCPRecipe }

type UpdateRecipeInput struct {
	inputs.AuthenticatedInput
	ID   string `path:"id" doc:"Recipe ID"`
	Body domain.UpdateMCPRecipeRequest
}
type UpdateRecipeOutput struct{ Body *domain.MCPRecipe }

type DeleteRecipeInput struct {
	inputs.AuthenticatedInput
	ID string `path:"id" doc:"Recipe ID"`
}
type DeleteRecipeOutput struct{}

type RunRecipeInput struct {
	inputs.AuthenticatedInput
	ID string `path:"id" doc:"Recipe ID"`
}
type RunRecipeOutput struct{ Body *domain.MCPRecipeRunResult }

// --- Handlers ---

func (h *RecipeHandler) CreateRecipe(ctx context.Context, input *CreateRecipeInput) (*CreateRecipeOutput, error) {
	// Idempotency: if the Worker sent an Idempotency-Key header, deduplicate the create.
	if h.idempotencyRepo != nil && input.IdempotencyKey != "" {
		var recipe *domain.MCPRecipe
		_, err := h.idempotencyRepo.GetOrCreate(ctx, input.IdempotencyKey, input.UserID, "createMCPRecipe",
			func() (interface{}, error) {
				return h.service.Create(ctx, input.UserID, input.Body)
			}, &recipe)
		if err != nil {
			return nil, humaerrors.LoggingMapError(ctx, err)
		}
		return &CreateRecipeOutput{Body: recipe}, nil
	}

	recipe, err := h.service.Create(ctx, input.UserID, input.Body)
	if err != nil {
		return nil, humaerrors.LoggingMapError(ctx, err)
	}
	return &CreateRecipeOutput{Body: recipe}, nil
}

func (h *RecipeHandler) ListRecipes(ctx context.Context, input *ListRecipesInput) (*ListRecipesOutput, error) {
	recipes, err := h.service.List(ctx, input.UserID)
	if err != nil {
		return nil, humaerrors.LoggingMapError(ctx, err)
	}
	if recipes == nil {
		recipes = []domain.MCPRecipe{}
	}
	return &ListRecipesOutput{Body: recipes}, nil
}

func (h *RecipeHandler) GetRecipe(ctx context.Context, input *GetRecipeInput) (*GetRecipeOutput, error) {
	recipe, err := h.service.Get(ctx, input.ID, input.UserID)
	if err != nil {
		return nil, humaerrors.LoggingMapError(ctx, err)
	}
	if recipe == nil {
		// The repo reports "not found" as (nil, nil). Returning (nil, nil) makes Huma panic,
		// so surface the sentinel explicitly (#1699 MCP-3).
		return nil, humaerrors.LoggingMapError(ctx, domain.ErrRecipeNotFound)
	}
	return &GetRecipeOutput{Body: recipe}, nil
}

func (h *RecipeHandler) UpdateRecipe(ctx context.Context, input *UpdateRecipeInput) (*UpdateRecipeOutput, error) {
	recipe, err := h.service.Update(ctx, input.ID, input.UserID, input.Body)
	if err != nil {
		return nil, humaerrors.LoggingMapError(ctx, err)
	}
	return &UpdateRecipeOutput{Body: recipe}, nil
}

func (h *RecipeHandler) DeleteRecipe(ctx context.Context, input *DeleteRecipeInput) (*DeleteRecipeOutput, error) {
	if err := h.service.Delete(ctx, input.ID, input.UserID); err != nil {
		return nil, humaerrors.LoggingMapError(ctx, err)
	}
	return &DeleteRecipeOutput{}, nil
}

func (h *RecipeHandler) RunRecipe(ctx context.Context, input *RunRecipeInput) (*RunRecipeOutput, error) {
	result, err := h.service.Run(ctx, input.ID, input.UserID)
	if err != nil {
		return nil, humaerrors.LoggingMapError(ctx, err)
	}
	return &RunRecipeOutput{Body: result}, nil
}

// RegisterRecipeRoutes registers MCP recipe routes with the Huma API.
func RegisterRecipeRoutes(api huma.API, handler *RecipeHandler) {
	huma.Register(api, huma.Operation{
		OperationID: "createMCPRecipe",
		Method:      http.MethodPost,
		Path:        "/api/v1/mcp/recipes",
		Summary:     "Create a recipe",
		Description: "Create a saved MCP workflow recipe (read-only compositions only in v1).",
		Tags:        []string{"MCP Recipes"},
		Security:    []map[string][]string{{"clerk": {}}},
	}, handler.CreateRecipe)

	huma.Register(api, huma.Operation{
		OperationID: "listMCPRecipes",
		Method:      http.MethodGet,
		Path:        "/api/v1/mcp/recipes",
		Summary:     "List recipes",
		Description: "List all saved MCP workflow recipes for the authenticated user.",
		Tags:        []string{"MCP Recipes"},
		Security:    []map[string][]string{{"clerk": {}}},
	}, handler.ListRecipes)

	huma.Register(api, huma.Operation{
		OperationID: "getMCPRecipe",
		Method:      http.MethodGet,
		Path:        "/api/v1/mcp/recipes/{id}",
		Summary:     "Get a recipe",
		Tags:        []string{"MCP Recipes"},
		Security:    []map[string][]string{{"clerk": {}}},
	}, handler.GetRecipe)

	huma.Register(api, huma.Operation{
		OperationID: "updateMCPRecipe",
		Method:      http.MethodPut,
		Path:        "/api/v1/mcp/recipes/{id}",
		Summary:     "Update a recipe",
		Tags:        []string{"MCP Recipes"},
		Security:    []map[string][]string{{"clerk": {}}},
	}, handler.UpdateRecipe)

	huma.Register(api, huma.Operation{
		OperationID: "deleteMCPRecipe",
		Method:      http.MethodDelete,
		Path:        "/api/v1/mcp/recipes/{id}",
		Summary:     "Delete a recipe",
		Tags:        []string{"MCP Recipes"},
		Security:    []map[string][]string{{"clerk": {}}},
	}, handler.DeleteRecipe)

	huma.Register(api, huma.Operation{
		OperationID: "runMCPRecipe",
		Method:      http.MethodPost,
		Path:        "/api/v1/mcp/recipes/{id}/run",
		Summary:     "Run a recipe",
		Description: "Resolve a recipe's spec for execution. Returns the ordered list of API calls to chain through readExecute() or writeExecute().",
		Tags:        []string{"MCP Recipes"},
		Security:    []map[string][]string{{"clerk": {}}},
	}, handler.RunRecipe)
}
