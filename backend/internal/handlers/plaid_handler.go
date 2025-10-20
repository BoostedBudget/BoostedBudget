package handlers

import (
	"net/http"

	"github.com/labstack/echo/v4"
	"github.com/maxnussbaum/BoostedBudget/internal/middleware"
	"github.com/maxnussbaum/BoostedBudget/internal/services"
)

// PlaidHandler handles Plaid-related HTTP requests
type PlaidHandler struct {
	plaidService services.PlaidService
}

// NewPlaidHandler creates a new PlaidHandler instance
func NewPlaidHandler(service services.PlaidService) *PlaidHandler {
	return &PlaidHandler{plaidService: service}
}

// CreateLinkToken handles POST /plaid/link-token
// Generates a Plaid Link token for initiating bank account connection flow
func (h *PlaidHandler) CreateLinkToken(c echo.Context) error {
	// Extract user_id from context (set by Auth0 middleware)
	userID, err := middleware.GetUserID(c)
	if err != nil {
		return echo.NewHTTPError(http.StatusUnauthorized, "User not authenticated")
	}

	// Create link token via service
	response, err := h.plaidService.CreateLinkToken(c.Request().Context(), userID)
	if err != nil {
		// Log error details server-side (don't expose Plaid internals to client)
		c.Logger().Errorf("Failed to create Plaid link token for user %s: %v", userID, err)
		return echo.NewHTTPError(http.StatusInternalServerError, "Failed to create link token")
	}

	return c.JSON(http.StatusOK, response)
}
