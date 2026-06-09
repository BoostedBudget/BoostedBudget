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

// BoostHandler handles boost-related HTTP requests.
type BoostHandler struct {
	service          services.BoostService
	executionService services.BoostExecutionService
	schedulerManager *workers.BoostSchedulerManager
}

// NewBoostHandler creates a new BoostHandler instance.
func NewBoostHandler(
	service services.BoostService,
	schedulerManager *workers.BoostSchedulerManager,
) *BoostHandler {
	return &BoostHandler{
		service:          service,
		schedulerManager: schedulerManager,
	}
}

// SetExecutionService sets the boost execution service for test and execution history endpoints.
// Issue #38: Rules API Endpoints - Test and execution history.
func (h *BoostHandler) SetExecutionService(executionService services.BoostExecutionService) {
	h.executionService = executionService
}

// --- Input/Output Types ---

// GetBoostsInput represents the input for getting user automation rules.
type GetBoostsInput struct {
	inputs.AuthenticatedInput
	// RuleType is an optional list of rule types to filter by.
	// Valid values: transfer_suggestion, budget_alert, category_rollover, savings_goal.
	// When omitted, all rule types are returned.
	RuleType []string `query:"rule_type" doc:"Filter by rule type (repeatable). Valid values: transfer_suggestion, budget_alert, category_rollover, savings_goal." required:"false"`
}

// GetBoostsOutput represents the response for getting user automation rules.
type GetBoostsOutput struct {
	Body []*domain.Boost
}

// GetBoostByIDInput represents the input for getting a rule by ID.
type GetBoostByIDInput struct {
	inputs.AuthenticatedInput
	ID string `path:"id" doc:"Automation Rule UUID"`
}

// GetBoostByIDOutput represents the response for getting a rule by ID.
type GetBoostByIDOutput struct {
	Body *domain.Boost
}

// CreateBoostInput represents the input for creating an automation rule.
type CreateBoostInput struct {
	inputs.AuthenticatedInput
	Body domain.CreateBoostRequest
}

// CreateBoostOutput represents the response for creating an automation rule.
type CreateBoostOutput struct {
	Body *domain.Boost
}

// UpdateBoostInput represents the input for updating an automation rule.
type UpdateBoostInput struct {
	inputs.AuthenticatedInput
	ID   string `path:"id" doc:"Automation Rule UUID"`
	Body domain.UpdateBoostRequest
}

// UpdateBoostOutput represents the response for updating an automation rule.
type UpdateBoostOutput struct {
	Body *domain.Boost
}

// DeleteBoostInput represents the input for deleting an automation rule.
type DeleteBoostInput struct {
	inputs.AuthenticatedInput
	ID string `path:"id" doc:"Automation Rule UUID"`
}

// ToggleBoostInput represents the input for toggling an automation rule.
type ToggleBoostInput struct {
	inputs.AuthenticatedInput
	ID   string `path:"id" doc:"Automation Rule UUID"`
	Body struct {
		Enabled bool `json:"enabled" doc:"Whether the rule should be enabled"`
	}
}

// ToggleBoostOutput represents the response for toggling an automation rule.
type ToggleBoostOutput struct {
	Body *domain.Boost
}

// TriggerBoostInput represents the input for manually triggering an automation rule.
type TriggerBoostInput struct {
	inputs.AuthenticatedInput
	ID   string `path:"id" doc:"Automation Rule UUID"`
	Body struct {
		TriggerData map[string]interface{} `json:"trigger_data,omitempty" doc:"Optional data to pass to the rule execution"`
	}
}

// TriggerBoostOutput represents the response for triggering an automation rule.
type TriggerBoostOutput struct {
	Body *domain.BoostExecution
}

// TestBoostInput represents the input for testing a rule execution (dry-run).
// Issue #38: Rules API Endpoints.
type TestBoostInput struct {
	inputs.AuthenticatedInput
	ID   string `path:"id" doc:"Automation Rule UUID"`
	Body struct {
		TriggerData map[string]interface{} `json:"trigger_data,omitempty" doc:"Optional test data to evaluate conditions against"`
	}
}

// TestBoostOutput represents the response for testing a rule execution.
type TestBoostOutput struct {
	Body *domain.BoostTestResult
}

// EnableBoostInput represents the input for enabling an automation rule.
type EnableBoostInput struct {
	inputs.AuthenticatedInput
	ID string `path:"id" doc:"Automation Rule UUID"`
}

// DisableBoostInput represents the input for disabling an automation rule.
type DisableBoostInput struct {
	inputs.AuthenticatedInput
	ID string `path:"id" doc:"Automation Rule UUID"`
}

// GetBoostExecutionsInput represents the input for getting rule execution history.
type GetBoostExecutionsInput struct {
	inputs.AuthenticatedInput
	ID     string `path:"id" doc:"Automation Rule UUID"`
	Status string `query:"status" doc:"Filter by status: pending, running, completed, failed"`
	Limit  int    `query:"limit" default:"20" minimum:"1" maximum:"100" doc:"Maximum number of results"`
	Offset int    `query:"offset" default:"0" minimum:"0" doc:"Number of results to skip"`
}

// GetBoostExecutionsOutput represents the response for getting rule execution history.
type GetBoostExecutionsOutput struct {
	Body *domain.BoostExecutionsResponse
}

// --- Handler Methods ---

// validBoostTypeValues lists the allowed rule_type query values for the list endpoint.
var validBoostTypeValues = map[string]domain.BoostType{
	string(domain.BoostTypeTransferSuggestion): domain.BoostTypeTransferSuggestion,
	string(domain.BoostTypeBudgetAlert):        domain.BoostTypeBudgetAlert,
	string(domain.BoostTypeCategoryRollover):   domain.BoostTypeCategoryRollover,
	string(domain.BoostTypeSavingsGoal):        domain.BoostTypeSavingsGoal,
}

// GetBoosts handles GET /api/v1/rules requests.
// Retrieves automation rules for the authenticated user, optionally filtered by rule type.
func (h *BoostHandler) GetBoosts(ctx context.Context, input *GetBoostsInput) (*GetBoostsOutput, error) {
	var boostTypes []domain.BoostType
	for _, rt := range input.RuleType {
		canonical, ok := validBoostTypeValues[rt]
		if !ok {
			return nil, huma.Error422UnprocessableEntity("invalid rule_type: " + rt + "; valid values: transfer_suggestion, budget_alert, category_rollover, savings_goal")
		}
		boostTypes = append(boostTypes, canonical)
	}

	rules, err := h.service.GetUserRulesFiltered(ctx, input.UserID, boostTypes)
	if err != nil {
		return nil, humaerrors.LoggingMapError(ctx, err)
	}

	// Return empty array instead of null if no rules
	if rules == nil {
		rules = []*domain.Boost{}
	}

	return &GetBoostsOutput{Body: rules}, nil
}

// GetBoostByID handles GET /api/v1/rules/:id requests.
// Retrieves a specific automation rule by ID.
func (h *BoostHandler) GetBoostByID(ctx context.Context, input *GetBoostByIDInput) (*GetBoostByIDOutput, error) {
	rule, err := h.service.GetRuleByID(ctx, input.UserID, input.ID)
	if err != nil {
		return nil, humaerrors.LoggingMapError(ctx, err)
	}

	return &GetBoostByIDOutput{Body: rule}, nil
}

// CreateBoost handles POST /api/v1/rules requests.
// Creates a new automation rule for the authenticated user.
func (h *BoostHandler) CreateBoost(ctx context.Context, input *CreateBoostInput) (*CreateBoostOutput, error) {
	rule, err := h.service.CreateRule(ctx, input.UserID, &input.Body)
	if err != nil {
		return nil, humaerrors.LoggingMapError(ctx, err)
	}

	return &CreateBoostOutput{Body: rule}, nil
}

// UpdateBoost handles PUT /api/v1/rules/:id requests.
// Updates an automation rule's configuration.
func (h *BoostHandler) UpdateBoost(ctx context.Context, input *UpdateBoostInput) (*UpdateBoostOutput, error) {
	rule, err := h.service.UpdateRule(ctx, input.UserID, input.ID, &input.Body)
	if err != nil {
		return nil, humaerrors.LoggingMapError(ctx, err)
	}

	return &UpdateBoostOutput{Body: rule}, nil
}

// DeleteBoost handles DELETE /api/v1/rules/:id requests.
// Removes an automation rule.
func (h *BoostHandler) DeleteBoost(ctx context.Context, input *DeleteBoostInput) (*struct{}, error) {
	err := h.service.DeleteRule(ctx, input.UserID, input.ID)
	if err != nil {
		return nil, humaerrors.LoggingMapError(ctx, err)
	}

	return nil, nil
}

// ToggleBoost handles POST /api/v1/rules/:id/toggle requests.
// Enables or disables an automation rule.
func (h *BoostHandler) ToggleBoost(ctx context.Context, input *ToggleBoostInput) (*ToggleBoostOutput, error) {
	rule, err := h.service.ToggleRule(ctx, input.UserID, input.ID, input.Body.Enabled)
	if err != nil {
		return nil, humaerrors.LoggingMapError(ctx, err)
	}

	return &ToggleBoostOutput{Body: rule}, nil
}

// TriggerBoost handles POST /api/v1/rules/:id/trigger requests.
// Manually triggers an automation rule for immediate execution.
// Issue #37: Rule Scheduler - Manual trigger support.
func (h *BoostHandler) TriggerBoost(ctx context.Context, input *TriggerBoostInput) (*TriggerBoostOutput, error) {
	// Verify user owns the rule first
	_, err := h.service.GetRuleByID(ctx, input.UserID, input.ID)
	if err != nil {
		return nil, humaerrors.LoggingMapError(ctx, err)
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
		default:
			return nil, humaerrors.LoggingMapError(ctx, err)
		}
	}

	return &TriggerBoostOutput{Body: execution}, nil
}

// TestBoost handles POST /api/v1/rules/:id/test requests.
// Performs a dry-run of the rule to preview results without persisting changes.
// Issue #38: Rules API Endpoints.
func (h *BoostHandler) TestBoost(ctx context.Context, input *TestBoostInput) (*TestBoostOutput, error) {
	// Verify user owns the rule
	rule, err := h.service.GetRuleByID(ctx, input.UserID, input.ID)
	if err != nil {
		return nil, humaerrors.LoggingMapError(ctx, err)
	}

	// Check if execution service is available
	if h.executionService == nil {
		return nil, huma.Error500InternalServerError("Rule execution service not available")
	}

	// Perform dry-run test
	result, err := h.executionService.TestBoost(ctx, rule, input.Body.TriggerData)
	if err != nil {
		return nil, humaerrors.LoggingMapError(ctx, err)
	}

	return &TestBoostOutput{Body: result}, nil
}

// EnableBoost handles POST /api/v1/rules/:id/enable requests.
// Convenience wrapper for enabling a rule.
// Issue #38: Rules API Endpoints.
func (h *BoostHandler) EnableBoost(ctx context.Context, input *EnableBoostInput) (*ToggleBoostOutput, error) {
	rule, err := h.service.ToggleRule(ctx, input.UserID, input.ID, true)
	if err != nil {
		return nil, humaerrors.LoggingMapError(ctx, err)
	}

	return &ToggleBoostOutput{Body: rule}, nil
}

// DisableBoost handles POST /api/v1/rules/:id/disable requests.
// Convenience wrapper for disabling a rule.
// Issue #38: Rules API Endpoints.
func (h *BoostHandler) DisableBoost(ctx context.Context, input *DisableBoostInput) (*ToggleBoostOutput, error) {
	rule, err := h.service.ToggleRule(ctx, input.UserID, input.ID, false)
	if err != nil {
		return nil, humaerrors.LoggingMapError(ctx, err)
	}

	return &ToggleBoostOutput{Body: rule}, nil
}

// GetBoostExecutions handles GET /api/v1/rules/:id/executions requests.
// Retrieves execution history for a specific rule with pagination.
// Issue #38: Rules API Endpoints.
func (h *BoostHandler) GetBoostExecutions(ctx context.Context, input *GetBoostExecutionsInput) (*GetBoostExecutionsOutput, error) {
	// Verify user owns the rule
	_, err := h.service.GetRuleByID(ctx, input.UserID, input.ID)
	if err != nil {
		return nil, humaerrors.LoggingMapError(ctx, err)
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
		return nil, humaerrors.LoggingMapError(ctx, err)
	}

	return &GetBoostExecutionsOutput{
		Body: &domain.BoostExecutionsResponse{
			Executions: executions,
			Total:      total,
			Limit:      input.Limit,
			Offset:     input.Offset,
		},
	}, nil
}

// RegisterBoostRoutes registers boost routes with the Huma API.
// Note: These routes require the Clerk middleware to be applied.
func RegisterBoostRoutes(api huma.API, handler *BoostHandler) {
	// GET /api/v1/rules - Get automation rules, optionally filtered by rule type
	huma.Register(api, huma.Operation{
		OperationID: "getAutomationRules",
		Method:      http.MethodGet,
		Path:        "/api/v1/rules",
		Summary:     "List automation rules (filter by rule_type)",
		Description: "Retrieves automation rules for the authenticated user, ordered by priority. " +
			"Use the repeatable `rule_type` query parameter to filter by type. " +
			"Valid values: transfer_suggestion, budget_alert, category_rollover, savings_goal. " +
			"Transfer-generating types are transfer_suggestion and savings_goal. " +
			"When omitted, all rule types are returned.",
		Tags:     []string{"Automation Rules"},
		Security: []map[string][]string{{"clerk": {}}},
	}, handler.GetBoosts)

	// GET /api/v1/rules/{id} - Get automation rule by ID
	huma.Register(api, huma.Operation{
		OperationID: "getAutomationRuleByID",
		Method:      http.MethodGet,
		Path:        "/api/v1/rules/{id}",
		Summary:     "Get automation rule by ID",
		Description: "Retrieves a specific automation rule by its UUID.",
		Tags:        []string{"Automation Rules"},
		Security:    []map[string][]string{{"clerk": {}}},
	}, handler.GetBoostByID)

	// POST /api/v1/rules - Create a new automation rule
	huma.Register(api, huma.Operation{
		OperationID:   "createAutomationRule",
		Method:        http.MethodPost,
		Path:          "/api/v1/rules",
		Summary:       "Create a new automation rule",
		Description:   "Creates a new automation rule for the authenticated user.",
		Tags:          []string{"Automation Rules"},
		Security:      []map[string][]string{{"clerk": {}}},
		DefaultStatus: http.StatusCreated,
	}, handler.CreateBoost)

	// PUT /api/v1/rules/{id} - Update an automation rule
	huma.Register(api, huma.Operation{
		OperationID: "updateAutomationRule",
		Method:      http.MethodPut,
		Path:        "/api/v1/rules/{id}",
		Summary:     "Update an automation rule",
		Description: "Updates an automation rule's configuration.",
		Tags:        []string{"Automation Rules"},
		Security:    []map[string][]string{{"clerk": {}}},
	}, handler.UpdateBoost)

	// DELETE /api/v1/rules/{id} - Delete an automation rule
	huma.Register(api, huma.Operation{
		OperationID:   "deleteAutomationRule",
		Method:        http.MethodDelete,
		Path:          "/api/v1/rules/{id}",
		Summary:       "Delete an automation rule",
		Description:   "Removes an automation rule.",
		Tags:          []string{"Automation Rules"},
		Security:      []map[string][]string{{"clerk": {}}},
		DefaultStatus: http.StatusNoContent,
	}, handler.DeleteBoost)

	// POST /api/v1/rules/{id}/toggle - Toggle an automation rule
	huma.Register(api, huma.Operation{
		OperationID: "toggleAutomationRule",
		Method:      http.MethodPost,
		Path:        "/api/v1/rules/{id}/toggle",
		Summary:     "Toggle an automation rule",
		Description: "Enables or disables an automation rule.",
		Tags:        []string{"Automation Rules"},
		Security:    []map[string][]string{{"clerk": {}}},
	}, handler.ToggleBoost)

	// POST /api/v1/rules/{id}/trigger - Manually trigger an automation rule
	huma.Register(api, huma.Operation{
		OperationID: "triggerAutomationRule",
		Method:      http.MethodPost,
		Path:        "/api/v1/rules/{id}/trigger",
		Summary:     "Manually trigger an automation rule",
		Description: "Triggers an automation rule for immediate execution with optional custom data.",
		Tags:        []string{"Automation Rules"},
		Security:    []map[string][]string{{"clerk": {}}},
	}, handler.TriggerBoost)

	// Issue #38: Rules API Endpoints - New routes

	// POST /api/v1/rules/{id}/test - Test automation rule (dry-run)
	huma.Register(api, huma.Operation{
		OperationID: "testAutomationRule",
		Method:      http.MethodPost,
		Path:        "/api/v1/rules/{id}/test",
		Summary:     "Test automation rule",
		Description: "Executes a dry-run of the rule to preview results without persisting changes.",
		Tags:        []string{"Automation Rules"},
		Security:    []map[string][]string{{"clerk": {}}},
	}, handler.TestBoost)

	// POST /api/v1/rules/{id}/enable - Enable an automation rule
	huma.Register(api, huma.Operation{
		OperationID: "enableAutomationRule",
		Method:      http.MethodPost,
		Path:        "/api/v1/rules/{id}/enable",
		Summary:     "Enable automation rule",
		Description: "Enables an automation rule for execution.",
		Tags:        []string{"Automation Rules"},
		Security:    []map[string][]string{{"clerk": {}}},
	}, handler.EnableBoost)

	// POST /api/v1/rules/{id}/disable - Disable an automation rule
	huma.Register(api, huma.Operation{
		OperationID: "disableAutomationRule",
		Method:      http.MethodPost,
		Path:        "/api/v1/rules/{id}/disable",
		Summary:     "Disable automation rule",
		Description: "Disables an automation rule from execution.",
		Tags:        []string{"Automation Rules"},
		Security:    []map[string][]string{{"clerk": {}}},
	}, handler.DisableBoost)

	// GET /api/v1/rules/{id}/executions - Get rule execution history
	huma.Register(api, huma.Operation{
		OperationID: "getRuleExecutions",
		Method:      http.MethodGet,
		Path:        "/api/v1/rules/{id}/executions",
		Summary:     "Get rule execution history",
		Description: "Retrieves execution history for a specific automation rule with optional status filter.",
		Tags:        []string{"Automation Rules"},
		Security:    []map[string][]string{{"clerk": {}}},
	}, handler.GetBoostExecutions)
}
