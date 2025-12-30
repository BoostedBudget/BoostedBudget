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

// SavingsGoalHandler handles savings goal-related HTTP requests.
// Issue #28: Savings goals
type SavingsGoalHandler struct {
	service services.SavingsGoalService
}

// NewSavingsGoalHandler creates a new SavingsGoalHandler instance.
func NewSavingsGoalHandler(service services.SavingsGoalService) *SavingsGoalHandler {
	return &SavingsGoalHandler{service: service}
}

// --- Input/Output Types ---

// CreateSavingsGoalInput represents the input for creating a savings goal.
type CreateSavingsGoalInput struct {
	inputs.AuthenticatedInput
	Body domain.CreateSavingsGoalRequest
}

// CreateSavingsGoalOutput represents the response for creating a savings goal.
type CreateSavingsGoalOutput struct {
	Body *domain.SavingsGoal
}

// GetSavingsGoalsInput represents the input for getting all savings goals.
type GetSavingsGoalsInput struct {
	inputs.AuthenticatedInput
}

// GetSavingsGoalsOutput represents the response for getting all savings goals.
type GetSavingsGoalsOutput struct {
	Body []*domain.SavingsGoal
}

// GetActiveSavingsGoalsInput represents the input for getting active savings goals.
type GetActiveSavingsGoalsInput struct {
	inputs.AuthenticatedInput
}

// GetActiveSavingsGoalsOutput represents the response for getting active savings goals.
type GetActiveSavingsGoalsOutput struct {
	Body []*domain.SavingsGoal
}

// GetSavingsGoalInput represents the input for getting a specific savings goal.
type GetSavingsGoalInput struct {
	inputs.AuthenticatedInput
	ID string `path:"id" doc:"Savings goal ID"`
}

// GetSavingsGoalOutput represents the response for getting a savings goal.
type GetSavingsGoalOutput struct {
	Body *domain.SavingsGoal
}

// UpdateSavingsGoalInput represents the input for updating a savings goal.
type UpdateSavingsGoalInput struct {
	inputs.AuthenticatedInput
	ID   string `path:"id" doc:"Savings goal ID"`
	Body domain.UpdateSavingsGoalRequest
}

// UpdateSavingsGoalOutput represents the response for updating a savings goal.
type UpdateSavingsGoalOutput struct {
	Body *domain.SavingsGoal
}

// DeleteSavingsGoalInput represents the input for deleting a savings goal.
type DeleteSavingsGoalInput struct {
	inputs.AuthenticatedInput
	ID string `path:"id" doc:"Savings goal ID"`
}

// --- Handler Methods ---

// CreateGoal handles POST /api/v1/savings-goals requests.
// Creates a new savings goal for the authenticated user.
func (h *SavingsGoalHandler) CreateGoal(ctx context.Context, input *CreateSavingsGoalInput) (*CreateSavingsGoalOutput, error) {
	goal, err := h.service.CreateGoal(ctx, input.UserID, &input.Body)
	if err != nil {
		return nil, humaerrors.MapError(err)
	}

	return &CreateSavingsGoalOutput{Body: goal}, nil
}

// GetGoals handles GET /api/v1/savings-goals requests.
// Retrieves all savings goals for the authenticated user.
func (h *SavingsGoalHandler) GetGoals(ctx context.Context, input *GetSavingsGoalsInput) (*GetSavingsGoalsOutput, error) {
	goals, err := h.service.GetGoals(ctx, input.UserID)
	if err != nil {
		return nil, humaerrors.MapError(err)
	}

	// Return empty array instead of null
	if goals == nil {
		goals = []*domain.SavingsGoal{}
	}

	return &GetSavingsGoalsOutput{Body: goals}, nil
}

// GetActiveGoals handles GET /api/v1/savings-goals/active requests.
// Retrieves all active (not completed) savings goals for the authenticated user.
func (h *SavingsGoalHandler) GetActiveGoals(ctx context.Context, input *GetActiveSavingsGoalsInput) (*GetActiveSavingsGoalsOutput, error) {
	goals, err := h.service.GetActiveGoals(ctx, input.UserID)
	if err != nil {
		return nil, humaerrors.MapError(err)
	}

	// Return empty array instead of null
	if goals == nil {
		goals = []*domain.SavingsGoal{}
	}

	return &GetActiveSavingsGoalsOutput{Body: goals}, nil
}

// GetGoal handles GET /api/v1/savings-goals/:id requests.
// Retrieves a specific savings goal by ID.
func (h *SavingsGoalHandler) GetGoal(ctx context.Context, input *GetSavingsGoalInput) (*GetSavingsGoalOutput, error) {
	goal, err := h.service.GetGoal(ctx, input.UserID, input.ID)
	if err != nil {
		return nil, humaerrors.MapError(err)
	}

	return &GetSavingsGoalOutput{Body: goal}, nil
}

// UpdateGoal handles PUT /api/v1/savings-goals/:id requests.
// Updates a savings goal's properties.
func (h *SavingsGoalHandler) UpdateGoal(ctx context.Context, input *UpdateSavingsGoalInput) (*UpdateSavingsGoalOutput, error) {
	goal, err := h.service.UpdateGoal(ctx, input.UserID, input.ID, &input.Body)
	if err != nil {
		return nil, humaerrors.MapError(err)
	}

	return &UpdateSavingsGoalOutput{Body: goal}, nil
}

// DeleteGoal handles DELETE /api/v1/savings-goals/:id requests.
// Removes a savings goal from the user's collection.
func (h *SavingsGoalHandler) DeleteGoal(ctx context.Context, input *DeleteSavingsGoalInput) (*struct{}, error) {
	err := h.service.DeleteGoal(ctx, input.UserID, input.ID)
	if err != nil {
		return nil, humaerrors.MapError(err)
	}

	return nil, nil
}

// RegisterSavingsGoalRoutes registers savings goal routes with the Huma API.
// Note: These routes require the Clerk middleware to be applied.
func RegisterSavingsGoalRoutes(api huma.API, handler *SavingsGoalHandler) {
	// POST /api/v1/savings-goals - Create savings goal
	huma.Register(api, huma.Operation{
		OperationID:   "createSavingsGoal",
		Method:        http.MethodPost,
		Path:          "/api/v1/savings-goals",
		Summary:       "Create savings goal",
		Description:   "Creates a new savings goal for the authenticated user.",
		Tags:          []string{"Savings Goals"},
		Security:      []map[string][]string{{"clerk": {}}},
		DefaultStatus: http.StatusCreated,
	}, handler.CreateGoal)

	// GET /api/v1/savings-goals - Get all savings goals
	huma.Register(api, huma.Operation{
		OperationID: "getSavingsGoals",
		Method:      http.MethodGet,
		Path:        "/api/v1/savings-goals",
		Summary:     "Get savings goals",
		Description: "Retrieves all savings goals for the authenticated user.",
		Tags:        []string{"Savings Goals"},
		Security:    []map[string][]string{{"clerk": {}}},
	}, handler.GetGoals)

	// GET /api/v1/savings-goals/active - Get active savings goals (must be before :id route)
	huma.Register(api, huma.Operation{
		OperationID: "getActiveSavingsGoals",
		Method:      http.MethodGet,
		Path:        "/api/v1/savings-goals/active",
		Summary:     "Get active savings goals",
		Description: "Retrieves all active (not completed) savings goals for the authenticated user.",
		Tags:        []string{"Savings Goals"},
		Security:    []map[string][]string{{"clerk": {}}},
	}, handler.GetActiveGoals)

	// GET /api/v1/savings-goals/:id - Get specific savings goal
	huma.Register(api, huma.Operation{
		OperationID: "getSavingsGoal",
		Method:      http.MethodGet,
		Path:        "/api/v1/savings-goals/{id}",
		Summary:     "Get savings goal",
		Description: "Retrieves a specific savings goal by ID.",
		Tags:        []string{"Savings Goals"},
		Security:    []map[string][]string{{"clerk": {}}},
	}, handler.GetGoal)

	// PUT /api/v1/savings-goals/:id - Update savings goal
	huma.Register(api, huma.Operation{
		OperationID: "updateSavingsGoal",
		Method:      http.MethodPut,
		Path:        "/api/v1/savings-goals/{id}",
		Summary:     "Update savings goal",
		Description: "Updates a savings goal's properties.",
		Tags:        []string{"Savings Goals"},
		Security:    []map[string][]string{{"clerk": {}}},
	}, handler.UpdateGoal)

	// DELETE /api/v1/savings-goals/:id - Delete savings goal
	huma.Register(api, huma.Operation{
		OperationID:   "deleteSavingsGoal",
		Method:        http.MethodDelete,
		Path:          "/api/v1/savings-goals/{id}",
		Summary:       "Delete savings goal",
		Description:   "Removes a savings goal from the user's collection.",
		Tags:          []string{"Savings Goals"},
		Security:      []map[string][]string{{"clerk": {}}},
		DefaultStatus: http.StatusNoContent,
	}, handler.DeleteGoal)
}
