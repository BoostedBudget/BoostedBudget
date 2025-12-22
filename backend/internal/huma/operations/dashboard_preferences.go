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

// DashboardPreferencesHandler handles dashboard preferences HTTP requests.
// Issue #354: Customizable Dashboard Widget System
type DashboardPreferencesHandler struct {
	service services.DashboardPreferencesService
}

// NewDashboardPreferencesHandler creates a new DashboardPreferencesHandler instance.
func NewDashboardPreferencesHandler(service services.DashboardPreferencesService) *DashboardPreferencesHandler {
	return &DashboardPreferencesHandler{service: service}
}

// --- Input/Output Types ---

// GetDashboardPreferencesInput represents the input for getting dashboard preferences.
type GetDashboardPreferencesInput struct {
	inputs.AuthenticatedInput
}

// GetDashboardPreferencesOutput represents the response for getting dashboard preferences.
type GetDashboardPreferencesOutput struct {
	Body *domain.DashboardPreferencesResponse
}

// SaveDashboardPreferencesInput represents the input for saving dashboard preferences.
type SaveDashboardPreferencesInput struct {
	inputs.AuthenticatedInput
	Body struct {
		Layout domain.DashboardLayout `json:"layout" doc:"Dashboard layout configuration"`
	}
}

// SaveDashboardPreferencesOutput represents the response for saving dashboard preferences.
type SaveDashboardPreferencesOutput struct {
	Body *domain.DashboardPreferencesResponse
}

// ResetDashboardPreferencesInput represents the input for resetting dashboard preferences.
type ResetDashboardPreferencesInput struct {
	inputs.AuthenticatedInput
}

// ResetDashboardPreferencesOutput represents the response for resetting dashboard preferences.
type ResetDashboardPreferencesOutput struct {
	Body *domain.DashboardPreferencesResponse
}

// --- Handler Methods ---

// GetPreferences handles GET /api/v1/dashboard/preferences requests.
// Returns the user's dashboard layout preferences (or default if none exist).
func (h *DashboardPreferencesHandler) GetPreferences(ctx context.Context, input *GetDashboardPreferencesInput) (*GetDashboardPreferencesOutput, error) {
	response, err := h.service.GetPreferences(ctx, input.UserID)
	if err != nil {
		return nil, humaerrors.MapError(err)
	}

	return &GetDashboardPreferencesOutput{Body: response}, nil
}

// SavePreferences handles PUT /api/v1/dashboard/preferences requests.
// Saves the user's dashboard layout preferences.
func (h *DashboardPreferencesHandler) SavePreferences(ctx context.Context, input *SaveDashboardPreferencesInput) (*SaveDashboardPreferencesOutput, error) {
	response, err := h.service.SavePreferences(ctx, input.UserID, input.Body.Layout)
	if err != nil {
		return nil, humaerrors.MapError(err)
	}

	return &SaveDashboardPreferencesOutput{Body: response}, nil
}

// ResetPreferences handles POST /api/v1/dashboard/preferences/reset requests.
// Resets the user's dashboard layout to the default configuration.
func (h *DashboardPreferencesHandler) ResetPreferences(ctx context.Context, input *ResetDashboardPreferencesInput) (*ResetDashboardPreferencesOutput, error) {
	response, err := h.service.ResetToDefault(ctx, input.UserID)
	if err != nil {
		return nil, humaerrors.MapError(err)
	}

	return &ResetDashboardPreferencesOutput{Body: response}, nil
}

// RegisterDashboardPreferencesRoutes registers dashboard preferences routes with the Huma API.
// Note: These routes require the Auth0 middleware to be applied.
func RegisterDashboardPreferencesRoutes(api huma.API, handler *DashboardPreferencesHandler) {
	// GET /api/v1/dashboard/preferences - Get dashboard preferences
	huma.Register(api, huma.Operation{
		OperationID: "getDashboardPreferences",
		Method:      http.MethodGet,
		Path:        "/api/v1/dashboard/preferences",
		Summary:     "Get dashboard preferences",
		Description: "Returns the user's dashboard layout preferences (or default if none exist).",
		Tags:        []string{"Dashboard"},
		Security:    []map[string][]string{{"auth0": {}}},
	}, handler.GetPreferences)

	// PUT /api/v1/dashboard/preferences - Save dashboard preferences
	huma.Register(api, huma.Operation{
		OperationID: "saveDashboardPreferences",
		Method:      http.MethodPut,
		Path:        "/api/v1/dashboard/preferences",
		Summary:     "Save dashboard preferences",
		Description: "Saves the user's dashboard layout preferences.",
		Tags:        []string{"Dashboard"},
		Security:    []map[string][]string{{"auth0": {}}},
	}, handler.SavePreferences)

	// POST /api/v1/dashboard/preferences/reset - Reset dashboard preferences
	huma.Register(api, huma.Operation{
		OperationID: "resetDashboardPreferences",
		Method:      http.MethodPost,
		Path:        "/api/v1/dashboard/preferences/reset",
		Summary:     "Reset dashboard preferences",
		Description: "Resets the user's dashboard layout to the default configuration.",
		Tags:        []string{"Dashboard"},
		Security:    []map[string][]string{{"auth0": {}}},
	}, handler.ResetPreferences)
}
