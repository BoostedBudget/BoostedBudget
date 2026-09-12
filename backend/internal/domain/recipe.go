package domain

import (
	"time"

	"github.com/danielgtaylor/huma/v2"
)

// MCPRecipeCall represents a single API call in a recipe's declarative spec.
// Only read-class operations are permitted in v1 (validated at service layer).
type MCPRecipeCall struct {
	OperationID string                 `json:"operation_id"`
	Params      map[string]interface{} `json:"params,omitempty"`
	Body        map[string]interface{} `json:"body,omitempty"`
	PathParams  map[string]string      `json:"path_params,omitempty"`
	Detail      *string                `json:"detail,omitempty" enum:"compact,full" nullable:"true" doc:"Response detail; omission preserves the default compact presentation. JSON null is treated as omission, not the string null."`
}

// TransformSchema keeps the nullable enum consistent with JSON decoding: null means absent.
// Huma's nullable tag adds the null type but does not add null to an enum.
func (MCPRecipeCall) TransformSchema(_ huma.Registry, schema *huma.Schema) *huma.Schema {
	schema.Properties["detail"].Enum = append(schema.Properties["detail"].Enum, nil)
	return schema
}

// MCPRecipe is a saved, named sequence of read-only API calls (Issue #1087).
// Recipes are user-scoped and can be scheduled via a cron expression.
// v1: read-only compositions only; write-recipes deferred to follow-up.
type MCPRecipe struct {
	ID          string          `json:"id"`
	UserID      string          `json:"user_id"`
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Spec        []MCPRecipeCall `json:"spec"`
	Schedule    *string         `json:"schedule,omitempty"` // cron expression or nil
	Enabled     bool            `json:"enabled"`
	CreatedAt   time.Time       `json:"created_at"`
	UpdatedAt   time.Time       `json:"updated_at"`
}

// CreateMCPRecipeRequest is the input for creating a new recipe.
type CreateMCPRecipeRequest struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Spec        []MCPRecipeCall `json:"spec"`
	Schedule    *string         `json:"schedule,omitempty"`
}

// UpdateMCPRecipeRequest is the input for updating an existing recipe.
type UpdateMCPRecipeRequest struct {
	Name        *string         `json:"name,omitempty"`
	Description *string         `json:"description,omitempty"`
	Spec        []MCPRecipeCall `json:"spec,omitempty"`
	Schedule    *string         `json:"schedule,omitempty"`
	Enabled     *bool           `json:"enabled,omitempty"`
}

// MCPRecipeRunResult is returned by RecipeService.Run — the resolved calls to execute.
type MCPRecipeRunResult struct {
	RecipeID string          `json:"recipe_id"`
	Name     string          `json:"name"`
	Calls    []MCPRecipeCall `json:"calls"`
}
