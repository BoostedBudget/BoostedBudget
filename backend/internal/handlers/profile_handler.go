package handlers

import (
	"errors"
	"net/http"

	"github.com/labstack/echo/v4"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/middleware"
	"github.com/maxnussbaum/BoostedBudget/internal/services"
)

// ProfileHandler handles user profile-related HTTP requests
type ProfileHandler struct {
	service services.ProfileService
}

// NewProfileHandler creates a new ProfileHandler instance
func NewProfileHandler(service services.ProfileService) *ProfileHandler {
	return &ProfileHandler{service: service}
}

// GetProfile handles GET /user/profile
// Retrieves the authenticated user's profile, creating one if it's their first login
func (h *ProfileHandler) GetProfile(c echo.Context) error {
	// Extract user_id from context (set by auth middleware)
	// NOTE: After PR #197, GetUserID() returns the internal UUID, not Auth0 ID
	userID, err := middleware.GetUserID(c)
	if err != nil {
		return echo.NewHTTPError(http.StatusUnauthorized, "User not authenticated")
	}

	// Extract email from JWT claims
	email, err := middleware.GetEmail(c)
	if err != nil {
		return echo.NewHTTPError(http.StatusUnauthorized, "Email not found in token")
	}

	// Get or create user profile using internal user UUID
	user, err := h.service.GetProfile(c.Request().Context(), userID, email)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "Failed to retrieve profile")
	}

	return c.JSON(http.StatusOK, user)
}

// UpdateProfile handles PUT /user/profile
// Updates the authenticated user's profile with validated data
func (h *ProfileHandler) UpdateProfile(c echo.Context) error {
	// Extract user_id from context (set by auth middleware)
	// NOTE: After PR #197, GetUserID() returns the internal UUID, not Auth0 ID
	userID, err := middleware.GetUserID(c)
	if err != nil {
		return echo.NewHTTPError(http.StatusUnauthorized, "User not authenticated")
	}

	// Parse request body
	var req domain.UpdateProfileRequest
	if err := c.Bind(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "Invalid request body")
	}

	// Update profile using internal user UUID
	user, err := h.service.UpdateProfile(c.Request().Context(), userID, &req)
	if err != nil {
		// Handle specific validation errors
		if errors.Is(err, services.ErrInvalidEmail) {
			return echo.NewHTTPError(http.StatusBadRequest, "Invalid email address")
		}
		if errors.Is(err, services.ErrInvalidRegion) {
			return echo.NewHTTPError(http.StatusBadRequest, "Invalid primary region")
		}
		// Generic server error
		return echo.NewHTTPError(http.StatusInternalServerError, "Failed to update profile")
	}

	return c.JSON(http.StatusOK, user)
}
