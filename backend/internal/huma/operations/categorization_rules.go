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

// CategorizationRuleHandler handles categorization rule-related HTTP requests.
type CategorizationRuleHandler struct {
	service services.CategorizationRuleService
}

// NewCategorizationRuleHandler creates a new CategorizationRuleHandler instance.
func NewCategorizationRuleHandler(service services.CategorizationRuleService) *CategorizationRuleHandler {
	return &CategorizationRuleHandler{service: service}
}

// --- Input/Output Types ---

// GetUserRulesInput represents the input for getting user rules.
type GetUserRulesInput struct {
	inputs.AuthenticatedInput
}

// GetUserRulesOutput represents the response for getting user rules.
type GetUserRulesOutput struct {
	Body []*domain.CategorizationRule
}

// GetRuleByIDInput represents the input for getting a rule by ID.
type GetRuleByIDInput struct {
	inputs.AuthenticatedInput
	ID string `path:"id" doc:"Rule UUID"`
}

// GetRuleByIDOutput represents the response for getting a rule by ID.
type GetRuleByIDOutput struct {
	Body *domain.CategorizationRule
}

// CreateRuleInput represents the input for creating a rule.
type CreateRuleInput struct {
	inputs.AuthenticatedInput
	Body domain.CreateCategorizationRuleRequest
}

// CreateRuleOutput represents the response for creating a rule.
type CreateRuleOutput struct {
	Body *domain.CategorizationRule
}

// UpdateRuleInput represents the input for updating a rule.
type UpdateRuleInput struct {
	inputs.AuthenticatedInput
	ID   string `path:"id" doc:"Rule UUID"`
	Body domain.UpdateCategorizationRuleRequest
}

// UpdateRuleOutput represents the response for updating a rule.
type UpdateRuleOutput struct {
	Body *domain.CategorizationRule
}

// DeleteRuleInput represents the input for deleting a rule.
type DeleteRuleInput struct {
	inputs.AuthenticatedInput
	ID string `path:"id" doc:"Rule UUID"`
}

// ToggleRuleInput represents the input for toggling a rule.
type ToggleRuleInput struct {
	inputs.AuthenticatedInput
	ID   string `path:"id" doc:"Rule UUID"`
	Body domain.ToggleCategorizationRuleRequest
}

// ToggleRuleOutput represents the response for toggling a rule.
type ToggleRuleOutput struct {
	Body *domain.CategorizationRule
}

// --- Handler Methods ---

// GetUserRules handles GET /api/v1/auto-categorization requests.
// Retrieves all categorization rules for the authenticated user.
func (h *CategorizationRuleHandler) GetUserRules(ctx context.Context, input *GetUserRulesInput) (*GetUserRulesOutput, error) {
	rules, err := h.service.GetUserRules(ctx, input.UserID)
	if err != nil {
		return nil, humaerrors.LoggingMapError(ctx, err)
	}

	// Return empty array instead of null if no rules
	if rules == nil {
		rules = []*domain.CategorizationRule{}
	}

	return &GetUserRulesOutput{Body: rules}, nil
}

// GetRuleByID handles GET /api/v1/auto-categorization/:id requests.
// Retrieves a specific rule by ID.
func (h *CategorizationRuleHandler) GetRuleByID(ctx context.Context, input *GetRuleByIDInput) (*GetRuleByIDOutput, error) {
	rule, err := h.service.GetRuleByID(ctx, input.UserID, input.ID)
	if err != nil {
		return nil, humaerrors.LoggingMapError(ctx, err)
	}

	return &GetRuleByIDOutput{Body: rule}, nil
}

// CreateRule handles POST /api/v1/auto-categorization requests.
// Creates a new categorization rule for the authenticated user.
func (h *CategorizationRuleHandler) CreateRule(ctx context.Context, input *CreateRuleInput) (*CreateRuleOutput, error) {
	rule, err := h.service.CreateRule(ctx, input.UserID, &input.Body)
	if err != nil {
		// Check for validation errors - return as 400
		errMsg := err.Error()
		if errMsg == "name is required" ||
			errMsg == "at least one condition is required" ||
			errMsg == "rule must have at least one action (category or tags)" {
			return nil, huma.Error400BadRequest(errMsg)
		}
		return nil, humaerrors.LoggingMapError(ctx, err)
	}

	return &CreateRuleOutput{Body: rule}, nil
}

// UpdateRule handles PUT /api/v1/auto-categorization/:id requests.
// Updates a rule's configuration.
func (h *CategorizationRuleHandler) UpdateRule(ctx context.Context, input *UpdateRuleInput) (*UpdateRuleOutput, error) {
	rule, err := h.service.UpdateRule(ctx, input.UserID, input.ID, &input.Body)
	if err != nil {
		return nil, humaerrors.LoggingMapError(ctx, err)
	}

	return &UpdateRuleOutput{Body: rule}, nil
}

// DeleteRule handles DELETE /api/v1/auto-categorization/:id requests.
// Removes a rule.
func (h *CategorizationRuleHandler) DeleteRule(ctx context.Context, input *DeleteRuleInput) (*struct{}, error) {
	err := h.service.DeleteRule(ctx, input.UserID, input.ID)
	if err != nil {
		return nil, humaerrors.LoggingMapError(ctx, err)
	}

	return nil, nil
}

// ToggleRule handles POST /api/v1/auto-categorization/:id/toggle requests.
// Enables or disables a rule.
func (h *CategorizationRuleHandler) ToggleRule(ctx context.Context, input *ToggleRuleInput) (*ToggleRuleOutput, error) {
	rule, err := h.service.ToggleRule(ctx, input.UserID, input.ID, input.Body.Enabled)
	if err != nil {
		return nil, humaerrors.LoggingMapError(ctx, err)
	}

	return &ToggleRuleOutput{Body: rule}, nil
}

// RegisterCategorizationRuleRoutes registers categorization rule routes with the Huma API.
// Note: These routes require the Clerk middleware to be applied.
func RegisterCategorizationRuleRoutes(api huma.API, handler *CategorizationRuleHandler) {
	// GET /api/v1/auto-categorization - Get all user rules
	huma.Register(api, huma.Operation{
		OperationID: "getUserRules",
		Method:      http.MethodGet,
		Path:        "/api/v1/auto-categorization",
		Summary:     "Get user categorization rules",
		Description: "Retrieves all categorization rules for the authenticated user, ordered by priority.",
		Tags:        []string{"Auto-Categorization"},
		Security:    []map[string][]string{{"clerk": {}}},
	}, handler.GetUserRules)

	// GET /api/v1/auto-categorization/{id} - Get rule by ID
	huma.Register(api, huma.Operation{
		OperationID: "getRuleByID",
		Method:      http.MethodGet,
		Path:        "/api/v1/auto-categorization/{id}",
		Summary:     "Get rule by ID",
		Description: "Retrieves a specific categorization rule by its UUID.",
		Tags:        []string{"Auto-Categorization"},
		Security:    []map[string][]string{{"clerk": {}}},
	}, handler.GetRuleByID)

	// POST /api/v1/auto-categorization - Create a new rule
	huma.Register(api, huma.Operation{
		OperationID:   "createRule",
		Method:        http.MethodPost,
		Path:          "/api/v1/auto-categorization",
		Summary:       "Create a new categorization rule",
		Description:   "Creates a new categorization rule for the authenticated user.",
		Tags:          []string{"Auto-Categorization"},
		Security:      []map[string][]string{{"clerk": {}}},
		DefaultStatus: http.StatusCreated,
	}, handler.CreateRule)

	// PUT /api/v1/auto-categorization/{id} - Update a rule
	huma.Register(api, huma.Operation{
		OperationID: "updateRule",
		Method:      http.MethodPut,
		Path:        "/api/v1/auto-categorization/{id}",
		Summary:     "Update a rule",
		Description: "Updates a categorization rule's configuration.",
		Tags:        []string{"Auto-Categorization"},
		Security:    []map[string][]string{{"clerk": {}}},
	}, handler.UpdateRule)

	// DELETE /api/v1/auto-categorization/{id} - Delete a rule
	huma.Register(api, huma.Operation{
		OperationID:   "deleteRule",
		Method:        http.MethodDelete,
		Path:          "/api/v1/auto-categorization/{id}",
		Summary:       "Delete a rule",
		Description:   "Removes a categorization rule.",
		Tags:          []string{"Auto-Categorization"},
		Security:      []map[string][]string{{"clerk": {}}},
		DefaultStatus: http.StatusNoContent,
	}, handler.DeleteRule)

	// POST /api/v1/auto-categorization/{id}/toggle - Toggle a rule
	huma.Register(api, huma.Operation{
		OperationID: "toggleRule",
		Method:      http.MethodPost,
		Path:        "/api/v1/auto-categorization/{id}/toggle",
		Summary:     "Toggle a rule",
		Description: "Enables or disables a categorization rule.",
		Tags:        []string{"Auto-Categorization"},
		Security:    []map[string][]string{{"clerk": {}}},
	}, handler.ToggleRule)
}
