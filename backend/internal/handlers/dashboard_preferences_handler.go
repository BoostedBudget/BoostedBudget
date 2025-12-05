package handlers

import (
	"context"
	"net/http"

	"github.com/labstack/echo/v4"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/middleware"
)

// DashboardPreferencesService defines the interface for dashboard preferences business logic
type DashboardPreferencesService interface {
	GetPreferences(ctx context.Context, userID string) (*domain.DashboardPreferencesResponse, error)
	SavePreferences(ctx context.Context, userID string, layout domain.DashboardLayout) (*domain.DashboardPreferencesResponse, error)
	ResetToDefault(ctx context.Context, userID string) (*domain.DashboardPreferencesResponse, error)
}

// DashboardPreferencesHandler handles dashboard preferences HTTP requests
// Issue #354: Customizable Dashboard Widget System
type DashboardPreferencesHandler struct {
	service DashboardPreferencesService
}

// NewDashboardPreferencesHandler creates a new DashboardPreferencesHandler instance
func NewDashboardPreferencesHandler(service DashboardPreferencesService) *DashboardPreferencesHandler {
	return &DashboardPreferencesHandler{
		service: service,
	}
}

// GetPreferences handles GET /api/v1/dashboard/preferences
// Returns the user's dashboard layout preferences (or default if none exist)
func (h *DashboardPreferencesHandler) GetPreferences(c echo.Context) error {
	// Extract user ID from context (set by auth middleware)
	userID, err := middleware.GetUserID(c)
	if err != nil {
		return echo.NewHTTPError(http.StatusUnauthorized, "User not authenticated")
	}

	// Call service
	response, err := h.service.GetPreferences(c.Request().Context(), userID)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "Failed to get dashboard preferences: "+err.Error())
	}

	return c.JSON(http.StatusOK, response)
}

// SavePreferencesRequest represents the request body for saving preferences
type SavePreferencesRequest struct {
	Layout domain.DashboardLayout `json:"layout"`
}

// SavePreferences handles PUT /api/v1/dashboard/preferences
// Saves the user's dashboard layout preferences
func (h *DashboardPreferencesHandler) SavePreferences(c echo.Context) error {
	// Extract user ID from context
	userID, err := middleware.GetUserID(c)
	if err != nil {
		return echo.NewHTTPError(http.StatusUnauthorized, "User not authenticated")
	}

	// Parse request body
	var req SavePreferencesRequest
	if err := c.Bind(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "Invalid request body: "+err.Error())
	}

	// Call service
	response, err := h.service.SavePreferences(c.Request().Context(), userID, req.Layout)
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "Failed to save dashboard preferences: "+err.Error())
	}

	return c.JSON(http.StatusOK, response)
}

// ResetPreferences handles POST /api/v1/dashboard/preferences/reset
// Resets the user's dashboard layout to the default configuration
func (h *DashboardPreferencesHandler) ResetPreferences(c echo.Context) error {
	// Extract user ID from context
	userID, err := middleware.GetUserID(c)
	if err != nil {
		return echo.NewHTTPError(http.StatusUnauthorized, "User not authenticated")
	}

	// Call service
	response, err := h.service.ResetToDefault(c.Request().Context(), userID)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "Failed to reset dashboard preferences: "+err.Error())
	}

	return c.JSON(http.StatusOK, response)
}
