package handlers

import (
	"net/http"

	"github.com/labstack/echo/v4"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/middleware"
	"github.com/maxnussbaum/BoostedBudget/internal/repository"
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

// GetAccountsResponse represents the response when retrieving user's linked accounts
type GetAccountsResponse struct {
	Accounts   []domain.Account `json:"accounts"`
	TotalCount int              `json:"total_count"`
}

// GetAccounts handles GET /accounts
// Retrieves all linked bank accounts for the authenticated user with institution information
// Query params:
//   - include_hidden (optional): If "true", includes hidden accounts. Default: false
func (h *PlaidHandler) GetAccounts(c echo.Context) error {
	// Extract user_id from context (set by Auth0 middleware)
	userID, err := middleware.GetUserID(c)
	if err != nil {
		return echo.NewHTTPError(http.StatusUnauthorized, "User not authenticated")
	}

	// Check for include_hidden query param (Issue #307)
	includeHidden := c.QueryParam("include_hidden") == "true"

	// Get accounts via service with hidden filter
	accounts, err := h.accountService.GetAccountsFiltered(c.Request().Context(), userID, includeHidden)
	if err != nil {
		// Log error details server-side (don't expose internals to client)
		c.Logger().Errorf("Failed to get accounts for user %s: %v", userID, err)
		return echo.NewHTTPError(http.StatusInternalServerError, "Failed to retrieve accounts")
	}

	// Build response
	response := GetAccountsResponse{
		Accounts:   accounts,
		TotalCount: len(accounts),
	}

	return c.JSON(http.StatusOK, response)
}

// DeleteAccount handles DELETE /accounts/:id
// Deletes a single account with ownership validation
func (h *PlaidHandler) DeleteAccount(c echo.Context) error {
	// Extract user_id from context (set by Auth0 middleware)
	userID, err := middleware.GetUserID(c)
	if err != nil {
		return echo.NewHTTPError(http.StatusUnauthorized, "User not authenticated")
	}

	// Get account ID from URL parameter
	accountID := c.Param("id")

	// Delete account via service (validates ownership)
	err = h.accountService.DeleteAccount(c.Request().Context(), accountID, userID)
	if err != nil {
		// Handle specific error types
		if err == repository.ErrAccountNotFound {
			return echo.NewHTTPError(http.StatusNotFound, "Account not found")
		}
		// Log error details server-side (don't expose internals to client)
		c.Logger().Errorf("Failed to delete account %s for user %s: %v", accountID, userID, err)
		return echo.NewHTTPError(http.StatusInternalServerError, "Failed to delete account")
	}

	// Return 204 No Content on success
	return c.NoContent(http.StatusNoContent)
}

// DisconnectInstitution handles DELETE /linked-items/:id
// Disconnects an institution (linked item) and all associated accounts
// Also calls Plaid /item/remove to stop monthly billing
func (h *PlaidHandler) DisconnectInstitution(c echo.Context) error {
	// Extract user_id from context (set by Auth0 middleware)
	userID, err := middleware.GetUserID(c)
	if err != nil {
		return echo.NewHTTPError(http.StatusUnauthorized, "User not authenticated")
	}

	// Get linked item ID from URL parameter
	linkedItemID := c.Param("id")

	// Disconnect institution via service (validates ownership, calls Plaid, deletes from DB)
	err = h.accountService.DisconnectInstitution(c.Request().Context(), linkedItemID, userID)
	if err != nil {
		// Handle specific error types
		if err == repository.ErrLinkedItemNotFound {
			return echo.NewHTTPError(http.StatusNotFound, "Institution not found")
		}
		// Log error details server-side (don't expose internals to client)
		c.Logger().Errorf("Failed to disconnect institution %s for user %s: %v", linkedItemID, userID, err)
		return echo.NewHTTPError(http.StatusInternalServerError, "Failed to disconnect institution")
	}

	// Return 204 No Content on success
	return c.NoContent(http.StatusNoContent)
}

// UpdateAccountResponse represents the response when updating an account
type UpdateAccountResponse struct {
	Account domain.Account `json:"account"`
}

// UpdateAccount handles PATCH /accounts/:id
// Updates account customization fields (Issue #307)
// Supports: nickname, custom_color, custom_icon, custom_emoji, display_order, is_hidden
func (h *PlaidHandler) UpdateAccount(c echo.Context) error {
	// Extract user_id from context (set by Auth0 middleware)
	userID, err := middleware.GetUserID(c)
	if err != nil {
		return echo.NewHTTPError(http.StatusUnauthorized, "User not authenticated")
	}

	// Get account ID from URL parameter
	accountID := c.Param("id")

	// Parse request body
	var req domain.UpdateAccountRequest
	if err := c.Bind(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "Invalid request body")
	}

	// Update account via service (validates ownership and fields)
	account, err := h.accountService.UpdateAccount(c.Request().Context(), accountID, userID, &req)
	if err != nil {
		// Handle specific error types
		if err == repository.ErrAccountNotFound {
			return echo.NewHTTPError(http.StatusNotFound, "Account not found")
		}
		// Check for validation errors (returned from domain.UpdateAccountRequest.Validate())
		if err.Error() == "custom_color must be a valid hex color (format: #RRGGBB)" ||
			err.Error() == "custom_icon must be one of the allowed icons" ||
			err.Error() == "nickname must not exceed 255 characters" ||
			err.Error() == "custom_emoji must not exceed 10 characters" {
			return echo.NewHTTPError(http.StatusBadRequest, err.Error())
		}
		// Log error details server-side (don't expose internals to client)
		c.Logger().Errorf("Failed to update account %s for user %s: %v", accountID, userID, err)
		return echo.NewHTTPError(http.StatusInternalServerError, "Failed to update account")
	}

	// Build response
	response := UpdateAccountResponse{
		Account: *account,
	}

	return c.JSON(http.StatusOK, response)
}

// ReorderAccountsResponse represents the response when reordering accounts
type ReorderAccountsResponse struct {
	Success bool `json:"success"`
}

// ReorderAccounts handles PATCH /accounts/reorder
// Bulk updates display_order for multiple accounts (Issue #307)
// Used for drag-and-drop reordering
func (h *PlaidHandler) ReorderAccounts(c echo.Context) error {
	// Extract user_id from context (set by Auth0 middleware)
	userID, err := middleware.GetUserID(c)
	if err != nil {
		return echo.NewHTTPError(http.StatusUnauthorized, "User not authenticated")
	}

	// Parse request body
	var req domain.ReorderAccountsRequest
	if err := c.Bind(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "Invalid request body")
	}

	// Reorder accounts via service
	err = h.accountService.ReorderAccounts(c.Request().Context(), userID, &req)
	if err != nil {
		// Handle specific error types
		if err == repository.ErrAccountNotFound {
			return echo.NewHTTPError(http.StatusNotFound, "One or more accounts not found")
		}
		// Check for empty list error
		if err.Error() == "account orders cannot be empty" {
			return echo.NewHTTPError(http.StatusBadRequest, err.Error())
		}
		// Log error details server-side (don't expose internals to client)
		c.Logger().Errorf("Failed to reorder accounts for user %s: %v", userID, err)
		return echo.NewHTTPError(http.StatusInternalServerError, "Failed to reorder accounts")
	}

	// Build response
	response := ReorderAccountsResponse{
		Success: true,
	}

	return c.JSON(http.StatusOK, response)
}
