package repository

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
)

// RecipeRepository defines the interface for MCP recipe data access (Issue #1087).
type RecipeRepository interface {
	Create(ctx context.Context, recipe *domain.MCPRecipe) error
	List(ctx context.Context, userID string) ([]domain.MCPRecipe, error)
	Get(ctx context.Context, id string, userID string) (*domain.MCPRecipe, error)
	Update(ctx context.Context, recipe *domain.MCPRecipe) error
	Delete(ctx context.Context, id string, userID string) error
}

// RecipeRepositoryImpl is the concrete implementation (exported for service compile-time checks).
type RecipeRepositoryImpl = recipeRepository

type recipeRepository struct {
	db DB
}

// NewRecipeRepository creates a new RecipeRepository instance.
func NewRecipeRepository(db DB) RecipeRepository {
	return &recipeRepository{db: db}
}

func (r *recipeRepository) Create(ctx context.Context, recipe *domain.MCPRecipe) error {
	specJSON, err := json.Marshal(recipe.Spec)
	if err != nil {
		return fmt.Errorf("failed to marshal recipe spec: %w", err)
	}

	query := `
		INSERT INTO mcp_recipes (id, user_id, name, description, spec, schedule, enabled)
		VALUES (gen_random_uuid(), $1, $2, $3, $4::jsonb, $5, $6)
		RETURNING id, created_at, updated_at
	`
	err = r.db.QueryRow(ctx, query,
		recipe.UserID, recipe.Name, recipe.Description,
		specJSON, recipe.Schedule, recipe.Enabled,
	).Scan(&recipe.ID, &recipe.CreatedAt, &recipe.UpdatedAt)
	if err != nil {
		if isUniqueViolation(err, "mcp_recipes_user_id_name_unique") {
			return fmt.Errorf("recipe name already exists: %w", domain.ErrDuplicateRecipeName)
		}
		return err
	}
	return nil
}

func (r *recipeRepository) List(ctx context.Context, userID string) ([]domain.MCPRecipe, error) {
	// Issue #1444: description is nullable (migration 000085_create_mcp_recipes.up.sql:10 — no
	// NOT NULL constraint) but domain.MCPRecipe.Description is a bare string, not *string.
	// COALESCE guarantees a non-NULL scan target — "" is the natural zero for this omitempty field.
	query := `
		SELECT id, user_id, name, COALESCE(description, '') AS description, spec, schedule, enabled, created_at, updated_at
		FROM mcp_recipes
		WHERE user_id = $1
		ORDER BY created_at DESC
	`
	rows, err := r.db.Query(ctx, query, userID)
	if err != nil {
		return nil, fmt.Errorf("failed to list recipes: %w", err)
	}
	defer rows.Close()

	var results []domain.MCPRecipe
	for rows.Next() {
		var recipe domain.MCPRecipe
		var specJSON []byte
		if err := rows.Scan(&recipe.ID, &recipe.UserID, &recipe.Name, &recipe.Description,
			&specJSON, &recipe.Schedule, &recipe.Enabled, &recipe.CreatedAt, &recipe.UpdatedAt); err != nil {
			return nil, fmt.Errorf("failed to scan recipe: %w", err)
		}
		if err := json.Unmarshal(specJSON, &recipe.Spec); err != nil {
			return nil, fmt.Errorf("failed to unmarshal recipe spec: %w", err)
		}
		results = append(results, recipe)
	}
	return results, rows.Err()
}

func (r *recipeRepository) Get(ctx context.Context, id string, userID string) (*domain.MCPRecipe, error) {
	// Issue #1444: same NULL-safety as List above — description is nullable.
	query := `
		SELECT id, user_id, name, COALESCE(description, '') AS description, spec, schedule, enabled, created_at, updated_at
		FROM mcp_recipes
		WHERE id = $1 AND user_id = $2
	`
	var recipe domain.MCPRecipe
	var specJSON []byte
	err := r.db.QueryRow(ctx, query, id, userID).Scan(
		&recipe.ID, &recipe.UserID, &recipe.Name, &recipe.Description,
		&specJSON, &recipe.Schedule, &recipe.Enabled, &recipe.CreatedAt, &recipe.UpdatedAt,
	)
	if err != nil {
		if isNotFoundError(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to get recipe: %w", err)
	}
	if err := json.Unmarshal(specJSON, &recipe.Spec); err != nil {
		return nil, fmt.Errorf("failed to unmarshal recipe spec: %w", err)
	}
	return &recipe, nil
}

func (r *recipeRepository) Update(ctx context.Context, recipe *domain.MCPRecipe) error {
	specJSON, err := json.Marshal(recipe.Spec)
	if err != nil {
		return fmt.Errorf("failed to marshal recipe spec: %w", err)
	}

	query := `
		UPDATE mcp_recipes
		SET name=$2, description=$3, spec=$4::jsonb, schedule=$5, enabled=$6, updated_at=NOW()
		WHERE id=$1 AND user_id=$7
		RETURNING updated_at
	`
	err = r.db.QueryRow(ctx, query,
		recipe.ID, recipe.Name, recipe.Description, specJSON,
		recipe.Schedule, recipe.Enabled, recipe.UserID,
	).Scan(&recipe.UpdatedAt)
	if err != nil {
		if isUniqueViolation(err, "mcp_recipes_user_id_name_unique") {
			return fmt.Errorf("recipe name already exists: %w", domain.ErrDuplicateRecipeName)
		}
		return err
	}
	return nil
}

func (r *recipeRepository) Delete(ctx context.Context, id string, userID string) error {
	_, err := r.db.Exec(ctx, `DELETE FROM mcp_recipes WHERE id=$1 AND user_id=$2`, id, userID)
	return err
}
