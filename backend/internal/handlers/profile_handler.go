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
	auth0UserID, err := middleware.GetUserID(c)
	if err != nil {
		return echo.NewHTTPError(http.StatusUnauthorized, "User not authenticated")
	}

	// Extract email from JWT claims
	email, err := getEmailFromClaims(c)
	if err != nil {
		return echo.NewHTTPError(http.StatusUnauthorized, "Email not found in token")
	}

	// Get or create user profile
	user, err := h.service.GetProfile(c.Request().Context(), auth0UserID, email)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "Failed to retrieve profile")
	}

	return c.JSON(http.StatusOK, user)
}

// UpdateProfile handles PUT /user/profile
// Updates the authenticated user's profile with validated data
func (h *ProfileHandler) UpdateProfile(c echo.Context) error {
	// Extract user_id from context (set by auth middleware)
	auth0UserID, err := middleware.GetUserID(c)
	if err != nil {
		return echo.NewHTTPError(http.StatusUnauthorized, "User not authenticated")
	}

	// Parse request body
	var req domain.UpdateProfileRequest
	if err := c.Bind(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "Invalid request body")
	}

	// Update profile
	user, err := h.service.UpdateProfile(c.Request().Context(), auth0UserID, &req)
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

// getEmailFromClaims extracts the email address from JWT claims
func getEmailFromClaims(c echo.Context) (string, error) {
	claims, err := middleware.GetClaims(c)
	if err != nil {
		return "", err
	}

	email, ok := claims["email"].(string)
	if !ok {
		return "", errors.New("email not found in claims")
	}

	return email, nil
}
