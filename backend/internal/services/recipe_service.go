package services

// RecipeService manages MCP saved read-only workflow recipes (Issue #1087).
// v1: recipes may only contain read-class operations.
// Write-recipes are deferred to a follow-up (require standing-auth model).

import (
	"context"
	"fmt"
	"time"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/repository"
)

// RecipeRepository is a subset of repository.RecipeRepository used by the service.
type RecipeRepository interface {
	Create(ctx context.Context, recipe *domain.MCPRecipe) error
	List(ctx context.Context, userID string) ([]domain.MCPRecipe, error)
	Get(ctx context.Context, id string, userID string) (*domain.MCPRecipe, error)
	Update(ctx context.Context, recipe *domain.MCPRecipe) error
	Delete(ctx context.Context, id string, userID string) error
}

// RecipeService defines the interface for recipe management.
type RecipeService interface {
	Create(ctx context.Context, userID string, req domain.CreateMCPRecipeRequest) (*domain.MCPRecipe, error)
	List(ctx context.Context, userID string) ([]domain.MCPRecipe, error)
	Get(ctx context.Context, id string, userID string) (*domain.MCPRecipe, error)
	Update(ctx context.Context, id string, userID string, req domain.UpdateMCPRecipeRequest) (*domain.MCPRecipe, error)
	Delete(ctx context.Context, id string, userID string) error
	// Run resolves the recipe's spec for execution by the Worker.
	// Returns the ordered list of API calls to chain through readExecute()/writeExecute().
	Run(ctx context.Context, id string, userID string) (*domain.MCPRecipeRunResult, error)
}

type recipeService struct {
	repo RecipeRepository
}

// NewRecipeService creates a new RecipeService.
func NewRecipeService(repo RecipeRepository) RecipeService {
	return &recipeService{repo: repo}
}

// Ensure recipeRepository satisfies both interfaces (compile-time check)
var _ RecipeRepository = (*repository.RecipeRepositoryImpl)(nil)

// validateSpec checks that all calls in a recipe spec are read-class and allowed.
// v1 hard constraint: no write-class operations.
func validateSpec(spec []domain.MCPRecipeCall) error {
	if len(spec) == 0 {
		return fmt.Errorf("recipe spec must contain at least one call: %w", domain.ErrInvalidRecipeSpec)
	}
	for _, call := range spec {
		if call.Detail != nil && *call.Detail != "compact" && *call.Detail != "full" {
			return fmt.Errorf("operation %q detail must be compact or full: %w", call.OperationID, domain.ErrInvalidRecipeSpec)
		}
		if !domain.MCPOperationAllowed(call.OperationID) {
			return fmt.Errorf("operation %q is not available via MCP: %w", call.OperationID, domain.ErrInvalidRecipeSpec)
		}
		if !domain.MCPOperationIsRead(call.OperationID) {
			return fmt.Errorf("operation %q is a write-class operation; v1 recipes may only contain read operations: %w", call.OperationID, domain.ErrInvalidRecipeSpec)
		}
	}
	return nil
}

func (s *recipeService) Create(ctx context.Context, userID string, req domain.CreateMCPRecipeRequest) (*domain.MCPRecipe, error) {
	if err := validateSpec(req.Spec); err != nil {
		return nil, fmt.Errorf("invalid recipe spec: %w", err)
	}

	// R4-4: reject schedule field (v1 constraint; column preserved for future use)
	if req.Schedule != nil && *req.Schedule != "" {
		return nil, fmt.Errorf("scheduled recipes are not available in v1; omit the schedule field: %w", domain.ErrInvalidRecipeSpec)
	}

	recipe := &domain.MCPRecipe{
		UserID:      userID,
		Name:        req.Name,
		Description: req.Description,
		Spec:        req.Spec,
		Schedule:    req.Schedule,
		Enabled:     true,
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	}

	if err := s.repo.Create(ctx, recipe); err != nil {
		return nil, fmt.Errorf("failed to create recipe: %w", err)
	}
	return recipe, nil
}

func (s *recipeService) List(ctx context.Context, userID string) ([]domain.MCPRecipe, error) {
	recipes, err := s.repo.List(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("failed to list recipes: %w", err)
	}
	return recipes, nil
}

func (s *recipeService) Get(ctx context.Context, id string, userID string) (*domain.MCPRecipe, error) {
	recipe, err := s.repo.Get(ctx, id, userID)
	if err != nil {
		return nil, fmt.Errorf("failed to get recipe: %w", err)
	}
	return recipe, nil
}

func (s *recipeService) Update(ctx context.Context, id string, userID string, req domain.UpdateMCPRecipeRequest) (*domain.MCPRecipe, error) {
	recipe, err := s.repo.Get(ctx, id, userID)
	if err != nil {
		return nil, fmt.Errorf("failed to get recipe: %w", err)
	}
	if recipe == nil {
		return nil, fmt.Errorf("recipe %s not found: %w", id, domain.ErrRecipeNotFound)
	}

	if req.Name != nil {
		recipe.Name = *req.Name
	}
	if req.Description != nil {
		recipe.Description = *req.Description
	}
	if req.Spec != nil {
		recipe.Spec = req.Spec
	}
	if err := validateSpec(recipe.Spec); err != nil {
		return nil, fmt.Errorf("invalid recipe spec: %w", err)
	}
	if req.Schedule != nil && *req.Schedule != "" {
		// R4-4: reject schedule updates (v1 constraint)
		return nil, fmt.Errorf("scheduled recipes are not available in v1; omit the schedule field: %w", domain.ErrInvalidRecipeSpec)
	}
	if req.Enabled != nil {
		recipe.Enabled = *req.Enabled
	}

	if err := s.repo.Update(ctx, recipe); err != nil {
		return nil, fmt.Errorf("failed to update recipe: %w", err)
	}
	return recipe, nil
}

func (s *recipeService) Delete(ctx context.Context, id string, userID string) error {
	return s.repo.Delete(ctx, id, userID)
}

func (s *recipeService) Run(ctx context.Context, id string, userID string) (*domain.MCPRecipeRunResult, error) {
	recipe, err := s.repo.Get(ctx, id, userID)
	if err != nil {
		return nil, fmt.Errorf("failed to get recipe: %w", err)
	}
	if recipe == nil {
		return nil, fmt.Errorf("recipe %s not found: %w", id, domain.ErrRecipeNotFound)
	}
	// Re-validate spec at run time (defense-in-depth: GAP-R3-10).
	// A recipe saved before a manifest change must not be allowed to run write-class ops.
	// The Worker's readExecute()/writeExecute() tools also gate on the manifest, but we enforce here too
	// so the backend never silently resolves a spec that violates the v1 read-only constraint.
	if err := validateSpec(recipe.Spec); err != nil {
		return nil, fmt.Errorf("recipe spec no longer valid: %w", err)
	}
	return &domain.MCPRecipeRunResult{
		RecipeID: recipe.ID,
		Name:     recipe.Name,
		Calls:    recipe.Spec,
	}, nil
}
