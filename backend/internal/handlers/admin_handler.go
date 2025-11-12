package handlers

import (
	"net/http"

	"github.com/labstack/echo/v4"
	"github.com/maxnussbaum/BoostedBudget/internal/repository"
	"github.com/maxnussbaum/BoostedBudget/internal/services"
)

// AdminHandler handles admin-only endpoints for testing and troubleshooting.
//
// Security: These endpoints are protected by:
//   1. Route registration only in staging environment (main.go)
//   2. AdminMiddleware returns 404 in production (security through obscurity)
//   3. Service-level validations (e.g., PlaidService checks sandbox environment)
//
// Future: Issue #309 will add role-based access control requiring "admin" role in JWT.
type AdminHandler struct {
	plaidService      services.PlaidService
	linkedItemRepo    repository.LinkedItemRepository
	encryptionService services.EncryptionService
}

// NewAdminHandler creates a new AdminHandler with required dependencies.
func NewAdminHandler(
	plaidService services.PlaidService,
	linkedItemRepo repository.LinkedItemRepository,
	encryptionService services.EncryptionService,
) *AdminHandler {
	return &AdminHandler{
		plaidService:      plaidService,
		linkedItemRepo:    linkedItemRepo,
		encryptionService: encryptionService,
	}
}

// FireWebhookRequest defines the request body for firing a Plaid webhook.
type FireWebhookRequest struct {
	ItemID      string `json:"item_id" validate:"required,uuid"`
	WebhookCode string `json:"webhook_code" validate:"required,oneof=INITIAL_UPDATE DEFAULT_UPDATE HISTORICAL_UPDATE TRANSACTIONS_REMOVED"`
}

// FireWebhookResponse defines the response body for webhook firing.
type FireWebhookResponse struct {
	WebhookFired bool   `json:"webhook_fired"`
	RequestID    string `json:"request_id"`
	ItemID       string `json:"item_id"`
	WebhookCode  string `json:"webhook_code"`
}

// FireWebhook fires a Plaid webhook for testing in staging environment.
//
// POST /api/v1/admin/plaid/fire-webhook
//
// Request Body:
//
//	{
//	  "item_id": "uuid",
//	  "webhook_code": "DEFAULT_UPDATE"
//	}
//
// Response:
//
//	{
//	  "webhook_fired": true,
//	  "request_id": "wh_123",
//	  "item_id": "uuid",
//	  "webhook_code": "DEFAULT_UPDATE"
//	}
//
// Security:
//   - Staging-only (AdminMiddleware returns 404 in production)
//   - Requires Auth0 JWT authentication (auth0Middleware)
//   - Future (Issue #309): Will require "admin" role in JWT
//
// Error Responses:
//   - 400: Invalid request body or validation failure
//   - 404: Linked item not found
//   - 500: Decryption failure or Plaid API error
//
// Issue #308: Admin API for Manual Webhook Firing in Staging
func (h *AdminHandler) FireWebhook(c echo.Context) error {
	// 1. Parse request body
	var req FireWebhookRequest
	if err := c.Bind(&req); err != nil {
		c.Logger().Errorf("Failed to parse request body: %v", err)
		return echo.NewHTTPError(http.StatusBadRequest, "Invalid request body")
	}

	// 2. Validate request
	if err := c.Validate(&req); err != nil {
		c.Logger().Errorf("Request validation failed: %v", err)
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	}

	// 3. Fetch linked item by Plaid item ID (not database primary key)
	// Issue #331: item_id is the Plaid identifier, not the database primary key
	linkedItem, err := h.linkedItemRepo.GetByItemID(c.Request().Context(), req.ItemID)
	if err != nil {
		if err == repository.ErrLinkedItemNotFound {
			return echo.NewHTTPError(http.StatusNotFound, "Item not found")
		}
		c.Logger().Errorf("Failed to fetch linked item: %v", err)
		return echo.NewHTTPError(http.StatusInternalServerError, "Failed to fetch linked item")
	}

	// 4. Decrypt access token
	accessToken, err := h.encryptionService.Decrypt(linkedItem.AccessTokenEncrypted)
	if err != nil {
		c.Logger().Errorf("Failed to decrypt access token for item %s: %v", req.ItemID, err)
		return echo.NewHTTPError(http.StatusInternalServerError, "Failed to decrypt access token")
	}

	// 5. Fire webhook via Plaid service
	response, err := h.plaidService.FireWebhook(c.Request().Context(), accessToken, req.WebhookCode)
	if err != nil {
		c.Logger().Errorf("Failed to fire webhook for item %s: %v", req.ItemID, err)
		return echo.NewHTTPError(http.StatusInternalServerError, "Failed to fire webhook")
	}

	// 6. Build and return success response
	return c.JSON(http.StatusOK, FireWebhookResponse{
		WebhookFired: response.WebhookFired,
		RequestID:    response.RequestId,
		ItemID:       req.ItemID,
		WebhookCode:  req.WebhookCode,
	})
}
