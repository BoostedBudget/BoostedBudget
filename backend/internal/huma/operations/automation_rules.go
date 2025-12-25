// Package operations provides HTTP handlers for the BoostedBudget API.
// Issue #37: Rule Scheduler - Automation rule operations including manual triggering.
package operations

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	humaerrors "github.com/maxnussbaum/BoostedBudget/internal/huma/errors"
	"github.com/maxnussbaum/BoostedBudget/internal/huma/inputs"
	"github.com/maxnussbaum/BoostedBudget/internal/services"
	"github.com/maxnussbaum/BoostedBudget/internal/workers"
)

// AutomationRuleHandler handles automation rule-related HTTP requests.
type AutomationRuleHandler struct {
	service          services.AutomationRuleService
	executionService services.RuleExecutionService
	schedulerManager *workers.RuleSchedulerManager
}

// NewAutomationRuleHandler creates a new AutomationRuleHandler instance.
func NewAutomationRuleHandler(
	service services.AutomationRuleService,
	schedulerManager *workers.RuleSchedulerManager,
) *AutomationRuleHandler {
	return &AutomationRuleHandler{
		service:          service,
		schedulerManager: schedulerManager,
	}
}

// SetExecutionService sets the rule execution service for test and execution history endpoints.
// Issue #38: Rules API Endpoints - Test and execution history.
func (h *AutomationRuleHandler) SetExecutionService(executionService services.RuleExecutionService) {
	h.executionService = executionService
}

// --- Input/Output Types ---

// GetAutomationRulesInput represents the input for getting user automation rules.
type GetAutomationRulesInput struct {
	inputs.AuthenticatedInput
}

// GetAutomationRulesOutput represents the response for getting user automation rules.
type GetAutomationRulesOutput struct {
	Body []*domain.AutomationRule
}

// GetAutomationRuleByIDInput represents the input for getting a rule by ID.
type GetAutomationRuleByIDInput struct {
	inputs.AuthenticatedInput
	ID string `path:"id" doc:"Automation Rule UUID"`
}

// GetAutomationRuleByIDOutput represents the response for getting a rule by ID.
type GetAutomationRuleByIDOutput struct {
	Body *domain.AutomationRule
}

// CreateAutomationRuleInput represents the input for creating an automation rule.
type CreateAutomationRuleInput struct {
	inputs.AuthenticatedInput
	Body domain.CreateAutomationRuleRequest
}

// CreateAutomationRuleOutput represents the response for creating an automation rule.
type CreateAutomationRuleOutput struct {
	Body *domain.AutomationRule
}

// UpdateAutomationRuleInput represents the input for updating an automation rule.
type UpdateAutomationRuleInput struct {
	inputs.AuthenticatedInput
	ID   string `path:"id" doc:"Automation Rule UUID"`
	Body domain.UpdateAutomationRuleRequest
}

// UpdateAutomationRuleOutput represents the response for updating an automation rule.
type UpdateAutomationRuleOutput struct {
	Body *domain.AutomationRule
}

// DeleteAutomationRuleInput represents the input for deleting an automation rule.
type DeleteAutomationRuleInput struct {
	inputs.AuthenticatedInput
	ID string `path:"id" doc:"Automation Rule UUID"`
}

// ToggleAutomationRuleInput represents the input for toggling an automation rule.
type ToggleAutomationRuleInput struct {
	inputs.AuthenticatedInput
	ID   string `path:"id" doc:"Automation Rule UUID"`
	Body struct {
		Enabled bool `json:"enabled" doc:"Whether the rule should be enabled"`
	}
}

// ToggleAutomationRuleOutput represents the response for toggling an automation rule.
type ToggleAutomationRuleOutput struct {
	Body *domain.AutomationRule
}

// TriggerAutomationRuleInput represents the input for manually triggering an automation rule.
type TriggerAutomationRuleInput struct {
	inputs.AuthenticatedInput
	ID   string `path:"id" doc:"Automation Rule UUID"`
	Body struct {
		TriggerData map[string]interface{} `json:"trigger_data,omitempty" doc:"Optional data to pass to the rule execution"`
	}
}

// TriggerAutomationRuleOutput represents the response for triggering an automation rule.
type TriggerAutomationRuleOutput struct {
	Body *domain.RuleExecution
}

// TestAutomationRuleInput represents the input for testing a rule execution (dry-run).
// Issue #38: Rules API Endpoints.
type TestAutomationRuleInput struct {
	inputs.AuthenticatedInput
	ID   string `path:"id" doc:"Automation Rule UUID"`
	Body struct {
		TriggerData map[string]interface{} `json:"trigger_data,omitempty" doc:"Optional test data to evaluate conditions against"`
	}
}

// TestAutomationRuleOutput represents the response for testing a rule execution.
type TestAutomationRuleOutput struct {
	Body *domain.RuleTestResult
}

// EnableAutomationRuleInput represents the input for enabling an automation rule.
type EnableAutomationRuleInput struct {
	inputs.AuthenticatedInput
	ID string `path:"id" doc:"Automation Rule UUID"`
}

// DisableAutomationRuleInput represents the input for disabling an automation rule.
type DisableAutomationRuleInput struct {
	inputs.AuthenticatedInput
	ID string `path:"id" doc:"Automation Rule UUID"`
}

// GetRuleExecutionsInput represents the input for getting rule execution history.
type GetRuleExecutionsInput struct {
	inputs.AuthenticatedInput
	ID     string `path:"id" doc:"Automation Rule UUID"`
	Status string `query:"status" doc:"Filter by status: pending, running, completed, failed"`
	Limit  int    `query:"limit" default:"20" minimum:"1" maximum:"100" doc:"Maximum number of results"`
	Offset int    `query:"offset" default:"0" minimum:"0" doc:"Number of results to skip"`
}

// GetRuleExecutionsOutput represents the response for getting rule execution history.
type GetRuleExecutionsOutput struct {
	Body *domain.RuleExecutionsResponse
}

// --- Handler Methods ---

// GetAutomationRules handles GET /api/v1/rules requests.
// Retrieves all automation rules for the authenticated user.
func (h *AutomationRuleHandler) GetAutomationRules(ctx context.Context, input *GetAutomationRulesInput) (*GetAutomationRulesOutput, error) {
	rules, err := h.service.GetUserRules(ctx, input.UserID)
	if err != nil {
		return nil, humaerrors.MapError(err)
	}

	// Return empty array instead of null if no rules
	if rules == nil {
		rules = []*domain.AutomationRule{}
	}

	return &GetAutomationRulesOutput{Body: rules}, nil
}

// GetAutomationRuleByID handles GET /api/v1/rules/:id requests.
// Retrieves a specific automation rule by ID.
func (h *AutomationRuleHandler) GetAutomationRuleByID(ctx context.Context, input *GetAutomationRuleByIDInput) (*GetAutomationRuleByIDOutput, error) {
	rule, err := h.service.GetRuleByID(ctx, input.UserID, input.ID)
	if err != nil {
		return nil, humaerrors.MapError(err)
	}

	return &GetAutomationRuleByIDOutput{Body: rule}, nil
}

// CreateAutomationRule handles POST /api/v1/rules requests.
// Creates a new automation rule for the authenticated user.
func (h *AutomationRuleHandler) CreateAutomationRule(ctx context.Context, input *CreateAutomationRuleInput) (*CreateAutomationRuleOutput, error) {
	rule, err := h.service.CreateRule(ctx, input.UserID, &input.Body)
	if err != nil {
		return nil, humaerrors.MapError(err)
	}

	return &CreateAutomationRuleOutput{Body: rule}, nil
}

// UpdateAutomationRule handles PUT /api/v1/rules/:id requests.
// Updates an automation rule's configuration.
func (h *AutomationRuleHandler) UpdateAutomationRule(ctx context.Context, input *UpdateAutomationRuleInput) (*UpdateAutomationRuleOutput, error) {
	rule, err := h.service.UpdateRule(ctx, input.UserID, input.ID, &input.Body)
	if err != nil {
		return nil, humaerrors.MapError(err)
	}

	return &UpdateAutomationRuleOutput{Body: rule}, nil
}

// DeleteAutomationRule handles DELETE /api/v1/rules/:id requests.
// Removes an automation rule.
func (h *AutomationRuleHandler) DeleteAutomationRule(ctx context.Context, input *DeleteAutomationRuleInput) (*struct{}, error) {
	err := h.service.DeleteRule(ctx, input.UserID, input.ID)
	if err != nil {
		return nil, humaerrors.MapError(err)
	}

	return nil, nil
}

// ToggleAutomationRule handles POST /api/v1/rules/:id/toggle requests.
// Enables or disables an automation rule.
func (h *AutomationRuleHandler) ToggleAutomationRule(ctx context.Context, input *ToggleAutomationRuleInput) (*ToggleAutomationRuleOutput, error) {
	rule, err := h.service.ToggleRule(ctx, input.UserID, input.ID, input.Body.Enabled)
	if err != nil {
		return nil, humaerrors.MapError(err)
	}

	return &ToggleAutomationRuleOutput{Body: rule}, nil
}

// TriggerAutomationRule handles POST /api/v1/rules/:id/trigger requests.
// Manually triggers an automation rule for immediate execution.
// Issue #37: Rule Scheduler - Manual trigger support.
func (h *AutomationRuleHandler) TriggerAutomationRule(ctx context.Context, input *TriggerAutomationRuleInput) (*TriggerAutomationRuleOutput, error) {
	// Verify user owns the rule first
	_, err := h.service.GetRuleByID(ctx, input.UserID, input.ID)
	if err != nil {
		return nil, humaerrors.MapError(err)
	}

	// Check if scheduler manager is available
	if h.schedulerManager == nil {
		return nil, huma.Error500InternalServerError("Rule scheduler not available")
	}

	// Check if manager is started
	if !h.schedulerManager.IsStarted() {
		return nil, huma.Error503ServiceUnavailable("Rule scheduler is not running")
	}

	// Trigger the rule via the scheduler manager
	execution, err := h.schedulerManager.TriggerRule(ctx, input.ID, input.Body.TriggerData)
	if err != nil {
		// Map known errors to appropriate HTTP statuses
		switch err {
		case workers.ErrNoManualTriggerProvider:
			return nil, huma.Error501NotImplemented("Manual triggering is not available")
		case workers.ErrManagerNotStarted:
			return nil, huma.Error503ServiceUnavailable("Rule scheduler is not running")
		case workers.ErrRuleDisabled:
			return nil, huma.Error400BadRequest("Rule is disabled")
		case workers.ErrExecutionLimitExceeded:
			return nil, huma.Error429TooManyRequests("Daily execution limit exceeded")
		default:
			return nil, humaerrors.MapError(err)
		}
	}

	return &TriggerAutomationRuleOutput{Body: execution}, nil
}

// TestAutomationRule handles POST /api/v1/rules/:id/test requests.
// Performs a dry-run of the rule to preview results without persisting changes.
// Issue #38: Rules API Endpoints.
func (h *AutomationRuleHandler) TestAutomationRule(ctx context.Context, input *TestAutomationRuleInput) (*TestAutomationRuleOutput, error) {
	// Verify user owns the rule
	rule, err := h.service.GetRuleByID(ctx, input.UserID, input.ID)
	if err != nil {
		return nil, humaerrors.MapError(err)
	}

	// Check if execution service is available
	if h.executionService == nil {
		return nil, huma.Error500InternalServerError("Rule execution service not available")
	}

	// Perform dry-run test
	result, err := h.executionService.TestRule(ctx, rule, input.Body.TriggerData)
	if err != nil {
		return nil, humaerrors.MapError(err)
	}

	return &TestAutomationRuleOutput{Body: result}, nil
}

// EnableAutomationRule handles POST /api/v1/rules/:id/enable requests.
// Convenience wrapper for enabling a rule.
// Issue #38: Rules API Endpoints.
func (h *AutomationRuleHandler) EnableAutomationRule(ctx context.Context, input *EnableAutomationRuleInput) (*ToggleAutomationRuleOutput, error) {
	rule, err := h.service.ToggleRule(ctx, input.UserID, input.ID, true)
	if err != nil {
		return nil, humaerrors.MapError(err)
	}

	return &ToggleAutomationRuleOutput{Body: rule}, nil
}

// DisableAutomationRule handles POST /api/v1/rules/:id/disable requests.
// Convenience wrapper for disabling a rule.
// Issue #38: Rules API Endpoints.
func (h *AutomationRuleHandler) DisableAutomationRule(ctx context.Context, input *DisableAutomationRuleInput) (*ToggleAutomationRuleOutput, error) {
	rule, err := h.service.ToggleRule(ctx, input.UserID, input.ID, false)
	if err != nil {
		return nil, humaerrors.MapError(err)
	}

	return &ToggleAutomationRuleOutput{Body: rule}, nil
}

// GetRuleExecutions handles GET /api/v1/rules/:id/executions requests.
// Retrieves execution history for a specific rule with pagination.
// Issue #38: Rules API Endpoints.
func (h *AutomationRuleHandler) GetRuleExecutions(ctx context.Context, input *GetRuleExecutionsInput) (*GetRuleExecutionsOutput, error) {
	// Verify user owns the rule
	_, err := h.service.GetRuleByID(ctx, input.UserID, input.ID)
	if err != nil {
		return nil, humaerrors.MapError(err)
	}

	// Check if execution service is available
	if h.executionService == nil {
		return nil, huma.Error500InternalServerError("Rule execution service not available")
	}

	// Parse status filter if provided
	var statusFilter *domain.ExecutionStatus
	if input.Status != "" {
		status := domain.ExecutionStatus(input.Status)
		statusFilter = &status
	}

	// Get execution history with filter
	executions, total, err := h.executionService.GetExecutionHistoryWithFilter(ctx, input.ID, statusFilter, input.Limit, input.Offset)
	if err != nil {
		return nil, humaerrors.MapError(err)
	}

	return &GetRuleExecutionsOutput{
		Body: &domain.RuleExecutionsResponse{
			Executions: executions,
			Total:      total,
			Limit:      input.Limit,
			Offset:     input.Offset,
		},
	}, nil
}

// RegisterAutomationRuleRoutes registers automation rule routes with the Huma API.
// Note: These routes require the Auth0 middleware to be applied.
func RegisterAutomationRuleRoutes(api huma.API, handler *AutomationRuleHandler) {
	// GET /api/v1/rules - Get all user automation rules
	huma.Register(api, huma.Operation{
		OperationID: "getAutomationRules",
		Method:      http.MethodGet,
		Path:        "/api/v1/rules",
		Summary:     "Get user automation rules",
		Description: "Retrieves all automation rules for the authenticated user, ordered by priority.",
		Tags:        []string{"Automation Rules"},
		Security:    []map[string][]string{{"auth0": {}}},
	}, handler.GetAutomationRules)

	// GET /api/v1/rules/{id} - Get automation rule by ID
	huma.Register(api, huma.Operation{
		OperationID: "getAutomationRuleByID",
		Method:      http.MethodGet,
		Path:        "/api/v1/rules/{id}",
		Summary:     "Get automation rule by ID",
		Description: "Retrieves a specific automation rule by its UUID.",
		Tags:        []string{"Automation Rules"},
		Security:    []map[string][]string{{"auth0": {}}},
	}, handler.GetAutomationRuleByID)

	// POST /api/v1/rules - Create a new automation rule
	huma.Register(api, huma.Operation{
		OperationID:   "createAutomationRule",
		Method:        http.MethodPost,
		Path:          "/api/v1/rules",
		Summary:       "Create a new automation rule",
		Description:   "Creates a new automation rule for the authenticated user.",
		Tags:          []string{"Automation Rules"},
		Security:      []map[string][]string{{"auth0": {}}},
		DefaultStatus: http.StatusCreated,
	}, handler.CreateAutomationRule)

	// PUT /api/v1/rules/{id} - Update an automation rule
	huma.Register(api, huma.Operation{
		OperationID: "updateAutomationRule",
		Method:      http.MethodPut,
		Path:        "/api/v1/rules/{id}",
		Summary:     "Update an automation rule",
		Description: "Updates an automation rule's configuration.",
		Tags:        []string{"Automation Rules"},
		Security:    []map[string][]string{{"auth0": {}}},
	}, handler.UpdateAutomationRule)

	// DELETE /api/v1/rules/{id} - Delete an automation rule
	huma.Register(api, huma.Operation{
		OperationID:   "deleteAutomationRule",
		Method:        http.MethodDelete,
		Path:          "/api/v1/rules/{id}",
		Summary:       "Delete an automation rule",
		Description:   "Removes an automation rule.",
		Tags:          []string{"Automation Rules"},
		Security:      []map[string][]string{{"auth0": {}}},
		DefaultStatus: http.StatusNoContent,
	}, handler.DeleteAutomationRule)

	// POST /api/v1/rules/{id}/toggle - Toggle an automation rule
	huma.Register(api, huma.Operation{
		OperationID: "toggleAutomationRule",
		Method:      http.MethodPost,
		Path:        "/api/v1/rules/{id}/toggle",
		Summary:     "Toggle an automation rule",
		Description: "Enables or disables an automation rule.",
		Tags:        []string{"Automation Rules"},
		Security:    []map[string][]string{{"auth0": {}}},
	}, handler.ToggleAutomationRule)

	// POST /api/v1/rules/{id}/trigger - Manually trigger an automation rule
	huma.Register(api, huma.Operation{
		OperationID: "triggerAutomationRule",
		Method:      http.MethodPost,
		Path:        "/api/v1/rules/{id}/trigger",
		Summary:     "Manually trigger an automation rule",
		Description: "Triggers an automation rule for immediate execution with optional custom data.",
		Tags:        []string{"Automation Rules"},
		Security:    []map[string][]string{{"auth0": {}}},
	}, handler.TriggerAutomationRule)

	// Issue #38: Rules API Endpoints - New routes

	// POST /api/v1/rules/{id}/test - Test automation rule (dry-run)
	huma.Register(api, huma.Operation{
		OperationID: "testAutomationRule",
		Method:      http.MethodPost,
		Path:        "/api/v1/rules/{id}/test",
		Summary:     "Test automation rule",
		Description: "Executes a dry-run of the rule to preview results without persisting changes.",
		Tags:        []string{"Automation Rules"},
		Security:    []map[string][]string{{"auth0": {}}},
	}, handler.TestAutomationRule)

	// POST /api/v1/rules/{id}/enable - Enable an automation rule
	huma.Register(api, huma.Operation{
		OperationID: "enableAutomationRule",
		Method:      http.MethodPost,
		Path:        "/api/v1/rules/{id}/enable",
		Summary:     "Enable automation rule",
		Description: "Enables an automation rule for execution.",
		Tags:        []string{"Automation Rules"},
		Security:    []map[string][]string{{"auth0": {}}},
	}, handler.EnableAutomationRule)

	// POST /api/v1/rules/{id}/disable - Disable an automation rule
	huma.Register(api, huma.Operation{
		OperationID: "disableAutomationRule",
		Method:      http.MethodPost,
		Path:        "/api/v1/rules/{id}/disable",
		Summary:     "Disable automation rule",
		Description: "Disables an automation rule from execution.",
		Tags:        []string{"Automation Rules"},
		Security:    []map[string][]string{{"auth0": {}}},
	}, handler.DisableAutomationRule)

	// GET /api/v1/rules/{id}/executions - Get rule execution history
	huma.Register(api, huma.Operation{
		OperationID: "getRuleExecutions",
		Method:      http.MethodGet,
		Path:        "/api/v1/rules/{id}/executions",
		Summary:     "Get rule execution history",
		Description: "Retrieves execution history for a specific automation rule with optional status filter.",
		Tags:        []string{"Automation Rules"},
		Security:    []map[string][]string{{"auth0": {}}},
	}, handler.GetRuleExecutions)
}
