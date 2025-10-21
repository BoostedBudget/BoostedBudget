package handlers

import (
	"net/http"

	"github.com/labstack/echo/v4"
	"github.com/maxnussbaum/BoostedBudget/internal/middleware"
	"github.com/maxnussbaum/BoostedBudget/internal/services"
)

// PlaidHandler handles Plaid-related HTTP requests
type PlaidHandler struct {
	plaidService   services.PlaidService
	accountService services.AccountService
}

// NewPlaidHandler creates a new PlaidHandler instance
func NewPlaidHandler(plaidService services.PlaidService, accountService services.AccountService) *PlaidHandler {
	return &PlaidHandler{
		plaidService:   plaidService,
		accountService: accountService,
	}
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

// ExchangePublicTokenRequest represents the request body for token exchange
type ExchangePublicTokenRequest struct {
	PublicToken     string `json:"public_token" validate:"required"`
	InstitutionID   string `json:"institution_id" validate:"required"`
	InstitutionName string `json:"institution_name" validate:"required"`
}

// ExchangePublicTokenResponse represents the response after exchanging token
type ExchangePublicTokenResponse struct {
	Success  bool   `json:"success"`
	ItemID   string `json:"item_id"`
	Accounts int    `json:"accounts_linked"`
}

// ExchangePublicToken handles POST /plaid/exchange-token
// Exchanges a Plaid public token for an access token and links bank accounts
func (h *PlaidHandler) ExchangePublicToken(c echo.Context) error {
	// Extract user_id from context (set by Auth0 middleware)
	userID, err := middleware.GetUserID(c)
	if err != nil {
		return echo.NewHTTPError(http.StatusUnauthorized, "User not authenticated")
	}

	// Parse and validate request body
	var req ExchangePublicTokenRequest
	if err := c.Bind(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "Invalid request body")
	}

	if err := c.Validate(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	}

	// Exchange token and link accounts via service
	result, err := h.accountService.LinkAccounts(
		c.Request().Context(),
		userID,
		req.PublicToken,
		req.InstitutionID,
		req.InstitutionName,
	)

	if err != nil {
		// Handle specific error types
		switch {
		case err == services.ErrAccountLimitExceeded:
			return echo.NewHTTPError(http.StatusForbidden, "Account limit exceeded for your subscription tier")
		case err == services.ErrInvalidPublicToken:
			return echo.NewHTTPError(http.StatusBadRequest, "Invalid or expired public token")
		case err == services.ErrPlaidAPIFailure:
			c.Logger().Errorf("Plaid API error for user %s: %v", userID, err)
			return echo.NewHTTPError(http.StatusServiceUnavailable, "Bank service temporarily unavailable")
		default:
			c.Logger().Errorf("Failed to link accounts for user %s: %v", userID, err)
			return echo.NewHTTPError(http.StatusInternalServerError, "Failed to link bank accounts")
		}
	}

	// Build success response
	response := ExchangePublicTokenResponse{
		Success:  true,
		ItemID:   result.LinkedItem.ItemID,
		Accounts: len(result.Accounts),
	}

	return c.JSON(http.StatusOK, response)
}
