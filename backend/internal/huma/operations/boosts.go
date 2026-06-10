// Package operations provides HTTP handlers for the BoostedBudget API.
// Issue #37: Boost Scheduler - Automation boost operations including manual triggering.
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
// Issue #38: Boosts API Endpoints - Test and execution history.
func (h *BoostHandler) SetExecutionService(executionService services.BoostExecutionService) {
	h.executionService = executionService
}

// --- Input/Output Types ---

// GetBoostsInput represents the input for getting user automation boosts.
type GetBoostsInput struct {
	inputs.AuthenticatedInput
	// BoostType is an optional list of boost types to filter by.
	// Valid values: transfer_suggestion, budget_alert, category_rollover, savings_goal.
	// When omitted, all boost types are returned.
	BoostType []string `query:"boost_type" doc:"Filter by boost type (repeatable). Valid values: transfer_suggestion, budget_alert, category_rollover, savings_goal." required:"false"`
}

// GetBoostsOutput represents the response for getting user automation boosts.
type GetBoostsOutput struct {
	Body []*domain.Boost
}

// GetBoostByIDInput represents the input for getting a boost by ID.
type GetBoostByIDInput struct {
	inputs.AuthenticatedInput
	ID string `path:"id" doc:"Automation Boost UUID"`
}

// GetBoostByIDOutput represents the response for getting a boost by ID.
type GetBoostByIDOutput struct {
	Body *domain.Boost
}

// CreateBoostInput represents the input for creating an automation boost.
type CreateBoostInput struct {
	inputs.AuthenticatedInput
	Body domain.CreateBoostRequest
}

// CreateBoostOutput represents the response for creating an automation boost.
type CreateBoostOutput struct {
	Body *domain.Boost
}

// UpdateBoostInput represents the input for updating an automation boost.
type UpdateBoostInput struct {
	inputs.AuthenticatedInput
	ID   string `path:"id" doc:"Automation Boost UUID"`
	Body domain.UpdateBoostRequest
}

// UpdateBoostOutput represents the response for updating an automation boost.
type UpdateBoostOutput struct {
	Body *domain.Boost
}

// DeleteBoostInput represents the input for deleting an automation boost.
type DeleteBoostInput struct {
	inputs.AuthenticatedInput
	ID string `path:"id" doc:"Automation Boost UUID"`
}

// ToggleBoostInput represents the input for toggling an automation boost.
type ToggleBoostInput struct {
	inputs.AuthenticatedInput
	ID   string `path:"id" doc:"Automation Boost UUID"`
	Body struct {
		Enabled bool `json:"enabled" doc:"Whether the boost should be enabled"`
	}
}

// ToggleBoostOutput represents the response for toggling an automation boost.
type ToggleBoostOutput struct {
	Body *domain.Boost
}

// TriggerBoostInput represents the input for manually triggering an automation boost.
type TriggerBoostInput struct {
	inputs.AuthenticatedInput
	ID   string `path:"id" doc:"Automation Boost UUID"`
	Body struct {
		TriggerData map[string]interface{} `json:"trigger_data,omitempty" doc:"Optional data to pass to the boost execution"`
	}
}

// TriggerBoostOutput represents the response for triggering an automation boost.
type TriggerBoostOutput struct {
	Body *domain.BoostExecution
}

// TestBoostInput represents the input for testing a boost execution (dry-run).
type TestBoostInput struct {
	inputs.AuthenticatedInput
	ID   string `path:"id" doc:"Automation Boost UUID"`
	Body struct {
		TriggerData map[string]interface{} `json:"trigger_data,omitempty" doc:"Optional test data to evaluate conditions against"`
	}
}

// TestBoostOutput represents the response for testing a rule execution.
type TestBoostOutput struct {
	Body *domain.BoostTestResult
}

// EnableBoostInput represents the input for enabling an automation boost.
type EnableBoostInput struct {
	inputs.AuthenticatedInput
	ID string `path:"id" doc:"Automation Boost UUID"`
}

// DisableBoostInput represents the input for disabling an automation boost.
type DisableBoostInput struct {
	inputs.AuthenticatedInput
	ID string `path:"id" doc:"Automation Boost UUID"`
}

// GetBoostExecutionsInput represents the input for getting boost execution history.
type GetBoostExecutionsInput struct {
	inputs.AuthenticatedInput
	ID     string `path:"id" doc:"Automation Boost UUID"`
	Status string `query:"status" doc:"Filter by status: pending, running, completed, failed"`
	Limit  int    `query:"limit" default:"20" minimum:"1" maximum:"100" doc:"Maximum number of results"`
	Offset int    `query:"offset" default:"0" minimum:"0" doc:"Number of results to skip"`
}

// GetBoostExecutionsOutput represents the response for getting boost execution history.
type GetBoostExecutionsOutput struct {
	Body *domain.BoostExecutionsResponse
}

// --- Handler Methods ---

// validBoostTypeValues lists the allowed boost_type query values for the list endpoint.
var validBoostTypeValues = map[string]domain.BoostType{
	string(domain.BoostTypeTransferSuggestion): domain.BoostTypeTransferSuggestion,
	string(domain.BoostTypeBudgetAlert):        domain.BoostTypeBudgetAlert,
	string(domain.BoostTypeCategoryRollover):   domain.BoostTypeCategoryRollover,
	string(domain.BoostTypeSavingsGoal):        domain.BoostTypeSavingsGoal,
}

// GetBoosts handles GET /api/v1/boosts requests.
// Retrieves automation boosts for the authenticated user, optionally filtered by boost type.
func (h *BoostHandler) GetBoosts(ctx context.Context, input *GetBoostsInput) (*GetBoostsOutput, error) {
	var boostTypes []domain.BoostType
	for _, rt := range input.BoostType {
		canonical, ok := validBoostTypeValues[rt]
		if !ok {
			return nil, huma.Error422UnprocessableEntity("invalid boost_type: " + rt + "; valid values: transfer_suggestion, budget_alert, category_rollover, savings_goal")
		}
		boostTypes = append(boostTypes, canonical)
	}

	boosts, err := h.service.GetUserBoostsFiltered(ctx, input.UserID, boostTypes)
	if err != nil {
		return nil, humaerrors.LoggingMapError(ctx, err)
	}

	// Return empty array instead of null if no boosts
	if boosts == nil {
		boosts = []*domain.Boost{}
	}

	return &GetBoostsOutput{Body: boosts}, nil
}

// GetBoostByID handles GET /api/v1/boosts/:id requests.
// Retrieves a specific automation boost by ID.
func (h *BoostHandler) GetBoostByID(ctx context.Context, input *GetBoostByIDInput) (*GetBoostByIDOutput, error) {
	boost, err := h.service.GetBoostByID(ctx, input.UserID, input.ID)
	if err != nil {
		return nil, humaerrors.LoggingMapError(ctx, err)
	}

	return &GetBoostByIDOutput{Body: boost}, nil
}

// CreateBoost handles POST /api/v1/boosts requests.
// Creates a new automation boost for the authenticated user.
func (h *BoostHandler) CreateBoost(ctx context.Context, input *CreateBoostInput) (*CreateBoostOutput, error) {
	boost, err := h.service.CreateBoost(ctx, input.UserID, &input.Body)
	if err != nil {
		return nil, humaerrors.LoggingMapError(ctx, err)
	}

	return &CreateBoostOutput{Body: boost}, nil
}

// UpdateBoost handles PUT /api/v1/boosts/:id requests.
// Updates an automation boost's configuration.
func (h *BoostHandler) UpdateBoost(ctx context.Context, input *UpdateBoostInput) (*UpdateBoostOutput, error) {
	boost, err := h.service.UpdateBoost(ctx, input.UserID, input.ID, &input.Body)
	if err != nil {
		return nil, humaerrors.LoggingMapError(ctx, err)
	}

	return &UpdateBoostOutput{Body: boost}, nil
}

// DeleteBoost handles DELETE /api/v1/boosts/:id requests.
// Removes an automation boost.
func (h *BoostHandler) DeleteBoost(ctx context.Context, input *DeleteBoostInput) (*struct{}, error) {
	err := h.service.DeleteBoost(ctx, input.UserID, input.ID)
	if err != nil {
		return nil, humaerrors.LoggingMapError(ctx, err)
	}

	return nil, nil
}

// ToggleBoost handles POST /api/v1/boosts/:id/toggle requests.
// Enables or disables an automation boost.
func (h *BoostHandler) ToggleBoost(ctx context.Context, input *ToggleBoostInput) (*ToggleBoostOutput, error) {
	boost, err := h.service.ToggleBoost(ctx, input.UserID, input.ID, input.Body.Enabled)
	if err != nil {
		return nil, humaerrors.LoggingMapError(ctx, err)
	}

	return &ToggleBoostOutput{Body: boost}, nil
}

// TriggerBoost handles POST /api/v1/boosts/:id/trigger requests.
// Manually triggers an automation boost for immediate execution.
// Issue #37: Rule Scheduler - Manual trigger support.
func (h *BoostHandler) TriggerBoost(ctx context.Context, input *TriggerBoostInput) (*TriggerBoostOutput, error) {
	// Verify user owns the boost first
	_, err := h.service.GetBoostByID(ctx, input.UserID, input.ID)
	if err != nil {
		return nil, humaerrors.LoggingMapError(ctx, err)
	}

	// Check if scheduler manager is available
	if h.schedulerManager == nil {
		return nil, huma.Error500InternalServerError("Boost scheduler not available")
	}

	// Check if manager is started
	if !h.schedulerManager.IsStarted() {
		return nil, huma.Error503ServiceUnavailable("Boost scheduler is not running")
	}

	// Trigger the boost via the scheduler manager
	execution, err := h.schedulerManager.TriggerBoost(ctx, input.ID, input.Body.TriggerData)
	if err != nil {
		// Map known errors to appropriate HTTP statuses
		switch err {
		case workers.ErrNoManualTriggerProvider:
			return nil, huma.Error501NotImplemented("Manual triggering is not available")
		case workers.ErrManagerNotStarted:
			return nil, huma.Error503ServiceUnavailable("Boost scheduler is not running")
		case workers.ErrBoostDisabled:
			return nil, huma.Error400BadRequest("Boost is disabled")
		default:
			return nil, humaerrors.LoggingMapError(ctx, err)
		}
	}

	return &TriggerBoostOutput{Body: execution}, nil
}

// TestBoost handles POST /api/v1/boosts/:id/test requests.
// Performs a dry-run of the boost to preview results without persisting changes.
// Issue #38: Boosts API Endpoints.
func (h *BoostHandler) TestBoost(ctx context.Context, input *TestBoostInput) (*TestBoostOutput, error) {
	// Verify user owns the boost
	boost, err := h.service.GetBoostByID(ctx, input.UserID, input.ID)
	if err != nil {
		return nil, humaerrors.LoggingMapError(ctx, err)
	}

	// Check if execution service is available
	if h.executionService == nil {
		return nil, huma.Error500InternalServerError("Boost execution service not available")
	}

	// Perform dry-run test
	result, err := h.executionService.TestBoost(ctx, boost, input.Body.TriggerData)
	if err != nil {
		return nil, humaerrors.LoggingMapError(ctx, err)
	}

	return &TestBoostOutput{Body: result}, nil
}

// EnableBoost handles POST /api/v1/boosts/:id/enable requests.
// Convenience wrapper for enabling a boost.
// Issue #38: Boosts API Endpoints.
func (h *BoostHandler) EnableBoost(ctx context.Context, input *EnableBoostInput) (*ToggleBoostOutput, error) {
	boost, err := h.service.ToggleBoost(ctx, input.UserID, input.ID, true)
	if err != nil {
		return nil, humaerrors.LoggingMapError(ctx, err)
	}

	return &ToggleBoostOutput{Body: boost}, nil
}

// DisableBoost handles POST /api/v1/boosts/:id/disable requests.
// Convenience wrapper for disabling a boost.
// Issue #38: Boosts API Endpoints.
func (h *BoostHandler) DisableBoost(ctx context.Context, input *DisableBoostInput) (*ToggleBoostOutput, error) {
	boost, err := h.service.ToggleBoost(ctx, input.UserID, input.ID, false)
	if err != nil {
		return nil, humaerrors.LoggingMapError(ctx, err)
	}

	return &ToggleBoostOutput{Body: boost}, nil
}

// GetBoostExecutions handles GET /api/v1/boosts/:id/executions requests.
// Retrieves execution history for a specific boost with pagination.
// Issue #38: Rules API Endpoints.
func (h *BoostHandler) GetBoostExecutions(ctx context.Context, input *GetBoostExecutionsInput) (*GetBoostExecutionsOutput, error) {
	// Verify user owns the boost
	_, err := h.service.GetBoostByID(ctx, input.UserID, input.ID)
	if err != nil {
		return nil, humaerrors.LoggingMapError(ctx, err)
	}

	// Check if execution service is available
	if h.executionService == nil {
		return nil, huma.Error500InternalServerError("Boost execution service not available")
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
	// GET /api/v1/boosts - Get automation boosts, optionally filtered by boost_type
	huma.Register(api, huma.Operation{
		OperationID: "getBoosts",
		Method:      http.MethodGet,
		Path:        "/api/v1/boosts",
		Summary:     "List boosts (filter by boost_type)",
		Description: "Retrieves boosts for the authenticated user, ordered by priority. " +
			"Use the repeatable `boost_type` query parameter to filter by type. " +
			"Valid values: transfer_suggestion, budget_alert, category_rollover, savings_goal. " +
			"Transfer-generating types are transfer_suggestion and savings_goal. " +
			"When omitted, all boost types are returned.",
		Tags:     []string{"Boosts"},
		Security: []map[string][]string{{"clerk": {}}},
	}, handler.GetBoosts)

	// GET /api/v1/boosts/{id} - Get boost by ID
	huma.Register(api, huma.Operation{
		OperationID: "getBoostByID",
		Method:      http.MethodGet,
		Path:        "/api/v1/boosts/{id}",
		Summary:     "Get boost by ID",
		Description: "Retrieves a specific boost by its UUID.",
		Tags:        []string{"Boosts"},
		Security:    []map[string][]string{{"clerk": {}}},
	}, handler.GetBoostByID)

	// POST /api/v1/boosts - Create a new boost
	huma.Register(api, huma.Operation{
		OperationID:   "createBoost",
		Method:        http.MethodPost,
		Path:          "/api/v1/boosts",
		Summary:       "Create a new boost",
		Description:   "Creates a new boost for the authenticated user.",
		Tags:          []string{"Boosts"},
		Security:      []map[string][]string{{"clerk": {}}},
		DefaultStatus: http.StatusCreated,
	}, handler.CreateBoost)

	// PUT /api/v1/boosts/{id} - Update a boost
	huma.Register(api, huma.Operation{
		OperationID: "updateBoost",
		Method:      http.MethodPut,
		Path:        "/api/v1/boosts/{id}",
		Summary:     "Update a boost",
		Description: "Updates a boost's configuration.",
		Tags:        []string{"Boosts"},
		Security:    []map[string][]string{{"clerk": {}}},
	}, handler.UpdateBoost)

	// DELETE /api/v1/boosts/{id} - Delete a boost
	huma.Register(api, huma.Operation{
		OperationID:   "deleteBoost",
		Method:        http.MethodDelete,
		Path:          "/api/v1/boosts/{id}",
		Summary:       "Delete a boost",
		Description:   "Removes a boost.",
		Tags:          []string{"Boosts"},
		Security:      []map[string][]string{{"clerk": {}}},
		DefaultStatus: http.StatusNoContent,
	}, handler.DeleteBoost)

	// POST /api/v1/boosts/{id}/toggle - Toggle a boost
	huma.Register(api, huma.Operation{
		OperationID: "toggleBoost",
		Method:      http.MethodPost,
		Path:        "/api/v1/boosts/{id}/toggle",
		Summary:     "Toggle a boost",
		Description: "Enables or disables a boost.",
		Tags:        []string{"Boosts"},
		Security:    []map[string][]string{{"clerk": {}}},
	}, handler.ToggleBoost)

	// POST /api/v1/boosts/{id}/trigger - Manually trigger a boost
	huma.Register(api, huma.Operation{
		OperationID: "triggerBoost",
		Method:      http.MethodPost,
		Path:        "/api/v1/boosts/{id}/trigger",
		Summary:     "Manually trigger a boost",
		Description: "Triggers a boost for immediate execution with optional custom data.",
		Tags:        []string{"Boosts"},
		Security:    []map[string][]string{{"clerk": {}}},
	}, handler.TriggerBoost)

	// Issue #38: Boosts API Endpoints - New routes

	// POST /api/v1/boosts/{id}/test - Test boost (dry-run)
	huma.Register(api, huma.Operation{
		OperationID: "testBoost",
		Method:      http.MethodPost,
		Path:        "/api/v1/boosts/{id}/test",
		Summary:     "Test boost",
		Description: "Executes a dry-run of the boost to preview results without persisting changes.",
		Tags:        []string{"Boosts"},
		Security:    []map[string][]string{{"clerk": {}}},
	}, handler.TestBoost)

	// POST /api/v1/boosts/{id}/enable - Enable a boost
	huma.Register(api, huma.Operation{
		OperationID: "enableBoost",
		Method:      http.MethodPost,
		Path:        "/api/v1/boosts/{id}/enable",
		Summary:     "Enable boost",
		Description: "Enables a boost for execution.",
		Tags:        []string{"Boosts"},
		Security:    []map[string][]string{{"clerk": {}}},
	}, handler.EnableBoost)

	// POST /api/v1/boosts/{id}/disable - Disable a boost
	huma.Register(api, huma.Operation{
		OperationID: "disableBoost",
		Method:      http.MethodPost,
		Path:        "/api/v1/boosts/{id}/disable",
		Summary:     "Disable boost",
		Description: "Disables a boost from execution.",
		Tags:        []string{"Boosts"},
		Security:    []map[string][]string{{"clerk": {}}},
	}, handler.DisableBoost)

	// GET /api/v1/boosts/{id}/executions - Get boost execution history
	huma.Register(api, huma.Operation{
		OperationID: "getBoostExecutions",
		Method:      http.MethodGet,
		Path:        "/api/v1/boosts/{id}/executions",
		Summary:     "Get boost execution history",
		Description: "Retrieves execution history for a specific boost with optional status filter.",
		Tags:        []string{"Boosts"},
		Security:    []map[string][]string{{"clerk": {}}},
	}, handler.GetBoostExecutions)
}
