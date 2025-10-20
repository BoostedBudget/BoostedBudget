package handlers

import (
	"net/http"
	"strings"

	"github.com/labstack/echo/v4"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/middleware"
	"github.com/maxnussbaum/BoostedBudget/internal/services"
)

// PrivacyHandler handles privacy settings-related HTTP requests
type PrivacyHandler struct {
	privacyService services.PrivacyService
	profileService services.ProfileService
}

// NewPrivacyHandler creates a new PrivacyHandler instance
// Requires both PrivacyService and ProfileService (to resolve user UUID from auth0 ID)
func NewPrivacyHandler(privacyService services.PrivacyService, profileService services.ProfileService) *PrivacyHandler {
	return &PrivacyHandler{
		privacyService: privacyService,
		profileService: profileService,
	}
}

// GetPrivacySettings handles GET /user/privacy
// Retrieves the authenticated user's privacy settings, creating defaults if none exist
func (h *PrivacyHandler) GetPrivacySettings(c echo.Context) error {
	// Extract userID from context
	// First check if we have user_uuid set (for testing or if middleware sets it)
	userUUID, ok := c.Get("user_uuid").(string)
	if !ok || userUUID == "" {
		// Fall back to getting it from profile using auth0 ID
		auth0UserID, err := middleware.GetUserID(c)
		if err != nil {
			return echo.NewHTTPError(http.StatusUnauthorized, "User not authenticated")
		}

		email, err := middleware.GetEmail(c)
		if err != nil {
			return echo.NewHTTPError(http.StatusUnauthorized, "Email not found in token")
		}

		// Get user profile to obtain internal UUID
		user, err := h.profileService.GetProfile(c.Request().Context(), auth0UserID, email)
		if err != nil {
			return echo.NewHTTPError(http.StatusInternalServerError, "Failed to retrieve user profile")
		}
		userUUID = user.ID
	}

	// Get privacy settings (auto-creates defaults if not found)
	settings, err := h.privacyService.GetPrivacySettings(c.Request().Context(), userUUID)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "Failed to retrieve privacy settings")
	}

	return c.JSON(http.StatusOK, settings)
}

// UpdatePrivacySettings handles PUT /user/privacy
// Updates the authenticated user's privacy settings with GDPR validation
func (h *PrivacyHandler) UpdatePrivacySettings(c echo.Context) error {
	// Extract userID from context
	userUUID, ok := c.Get("user_uuid").(string)
	if !ok || userUUID == "" {
		// Fall back to getting it from profile using auth0 ID
		auth0UserID, err := middleware.GetUserID(c)
		if err != nil {
			return echo.NewHTTPError(http.StatusUnauthorized, "User not authenticated")
		}

		email, err := middleware.GetEmail(c)
		if err != nil {
			return echo.NewHTTPError(http.StatusUnauthorized, "Email not found in token")
		}

		// Get user profile to obtain internal UUID
		user, err := h.profileService.GetProfile(c.Request().Context(), auth0UserID, email)
		if err != nil {
			return echo.NewHTTPError(http.StatusInternalServerError, "Failed to retrieve user profile")
		}
		userUUID = user.ID
	}

	// Parse request body
	var req domain.UpdatePrivacySettingsRequest
	if err := c.Bind(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "Invalid request body")
	}

	// Update privacy settings (validates GDPR compliance)
	settings, err := h.privacyService.UpdatePrivacySettings(c.Request().Context(), userUUID, &req)
	if err != nil {
		// Handle specific GDPR validation errors
		if strings.Contains(err.Error(), "necessary_consent") {
			return echo.NewHTTPError(http.StatusBadRequest, err.Error())
		}
		// Generic server error
		return echo.NewHTTPError(http.StatusInternalServerError, "Failed to update privacy settings")
	}

	return c.JSON(http.StatusOK, settings)
}
