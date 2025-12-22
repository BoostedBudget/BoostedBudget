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

// ProfileHandler handles user profile-related HTTP requests.
type ProfileHandler struct {
	service services.ProfileService
}

// NewProfileHandler creates a new ProfileHandler instance.
func NewProfileHandler(service services.ProfileService) *ProfileHandler {
	return &ProfileHandler{service: service}
}

// --- Input/Output Types ---

// GetProfileInput represents the input for getting user profile.
type GetProfileInput struct {
	inputs.AuthenticatedWithEmailInput
}

// GetProfileOutput represents the response for getting user profile.
type GetProfileOutput struct {
	Body *domain.User
}

// UpdateProfileInput represents the input for updating user profile.
type UpdateProfileInput struct {
	inputs.AuthenticatedInput
	Body domain.UpdateProfileRequest
}

// UpdateProfileOutput represents the response for updating user profile.
type UpdateProfileOutput struct {
	Body *domain.User
}

// --- Handler Methods ---

// GetProfile handles GET /api/v1/user/profile requests.
// Retrieves the authenticated user's profile, creating one if it's their first login.
func (h *ProfileHandler) GetProfile(ctx context.Context, input *GetProfileInput) (*GetProfileOutput, error) {
	user, err := h.service.GetProfile(ctx, input.UserID, input.Email)
	if err != nil {
		return nil, huma.Error500InternalServerError("Failed to retrieve profile")
	}

	return &GetProfileOutput{Body: user}, nil
}

// UpdateProfile handles PUT /api/v1/user/profile requests.
// Updates the authenticated user's profile with validated data.
func (h *ProfileHandler) UpdateProfile(ctx context.Context, input *UpdateProfileInput) (*UpdateProfileOutput, error) {
	user, err := h.service.UpdateProfile(ctx, input.UserID, &input.Body)
	if err != nil {
		switch err {
		case services.ErrInvalidEmail:
			return nil, huma.Error400BadRequest("Invalid email address")
		case services.ErrInvalidRegion:
			return nil, huma.Error400BadRequest("Invalid primary region")
		default:
			return nil, humaerrors.MapError(err)
		}
	}

	return &UpdateProfileOutput{Body: user}, nil
}

// RegisterProfileRoutes registers profile routes with the Huma API.
// Note: These routes require the Auth0 middleware to be applied.
func RegisterProfileRoutes(api huma.API, handler *ProfileHandler) {
	// GET /api/v1/user/profile - Get user profile
	huma.Register(api, huma.Operation{
		OperationID: "getUserProfile",
		Method:      http.MethodGet,
		Path:        "/api/v1/user/profile",
		Summary:     "Get user profile",
		Description: "Retrieves the authenticated user's profile, creating one if it's their first login.",
		Tags:        []string{"Profile"},
		Security:    []map[string][]string{{"auth0": {}}},
	}, handler.GetProfile)

	// PUT /api/v1/user/profile - Update user profile
	huma.Register(api, huma.Operation{
		OperationID: "updateUserProfile",
		Method:      http.MethodPut,
		Path:        "/api/v1/user/profile",
		Summary:     "Update user profile",
		Description: "Updates the authenticated user's profile with validated data.",
		Tags:        []string{"Profile"},
		Security:    []map[string][]string{{"auth0": {}}},
	}, handler.UpdateProfile)
}
