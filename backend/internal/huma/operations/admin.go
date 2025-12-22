package operations

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/maxnussbaum/BoostedBudget/internal/huma/inputs"
	"github.com/maxnussbaum/BoostedBudget/internal/repository"
	"github.com/maxnussbaum/BoostedBudget/internal/services"
)

// AdminHandler handles admin-only endpoints for testing and troubleshooting.
//
// Security: These endpoints are protected by:
//  1. Route registration only in staging environment (main.go)
//  2. AdminMiddleware returns 404 in production (security through obscurity)
//  3. Service-level validations (e.g., PlaidService checks sandbox environment)
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

// --- Input/Output Types ---

// FireWebhookInput represents the input for firing a Plaid webhook.
type FireWebhookInput struct {
	inputs.AuthenticatedInput
	Body FireWebhookRequestBody
}

// FireWebhookRequestBody defines the request body for firing a Plaid webhook.
type FireWebhookRequestBody struct {
	ItemID      string `json:"item_id" validate:"required,uuid" doc:"The Plaid item ID (UUID)"`
	WebhookCode string `json:"webhook_code" validate:"required,oneof=INITIAL_UPDATE DEFAULT_UPDATE HISTORICAL_UPDATE TRANSACTIONS_REMOVED" doc:"The webhook code to fire"`
}

// FireWebhookOutput represents the response for webhook firing.
type FireWebhookOutput struct {
	Body FireWebhookResponseBody
}

// FireWebhookResponseBody defines the response body for webhook firing.
type FireWebhookResponseBody struct {
	WebhookFired bool   `json:"webhook_fired" doc:"Whether the webhook was fired successfully"`
	RequestID    string `json:"request_id" doc:"The request ID from Plaid"`
	ItemID       string `json:"item_id" doc:"The Plaid item ID"`
	WebhookCode  string `json:"webhook_code" doc:"The webhook code that was fired"`
}

// --- Handler Methods ---

// FireWebhook fires a Plaid webhook for testing in staging environment.
//
// POST /api/v1/admin/plaid/fire-webhook
//
// Security:
//   - Staging-only (AdminMiddleware returns 404 in production)
//   - Requires Auth0 JWT authentication
//   - Future (Issue #309): Will require "admin" role in JWT
//
// Error Responses:
//   - 400: Invalid request body or validation failure
//   - 404: Linked item not found
//   - 500: Decryption failure or Plaid API error
//
// Issue #308: Admin API for Manual Webhook Firing in Staging
func (h *AdminHandler) FireWebhook(ctx context.Context, input *FireWebhookInput) (*FireWebhookOutput, error) {
	// 1. Fetch linked item by Plaid item ID (not database primary key)
	// Issue #331: item_id is the Plaid identifier, not the database primary key
	linkedItem, err := h.linkedItemRepo.GetByItemID(ctx, input.Body.ItemID)
	if err != nil {
		if err == repository.ErrLinkedItemNotFound {
			return nil, huma.Error404NotFound("Item not found")
		}
		return nil, huma.Error500InternalServerError("Failed to fetch linked item")
	}

	// 2. Decrypt access token
	accessToken, err := h.encryptionService.Decrypt(linkedItem.AccessTokenEncrypted)
	if err != nil {
		return nil, huma.Error500InternalServerError("Failed to decrypt access token")
	}

	// 3. Fire webhook via Plaid service
	response, err := h.plaidService.FireWebhook(ctx, accessToken, input.Body.WebhookCode)
	if err != nil {
		return nil, huma.Error500InternalServerError("Failed to fire webhook")
	}

	// 4. Build and return success response
	return &FireWebhookOutput{
		Body: FireWebhookResponseBody{
			WebhookFired: response.WebhookFired,
			RequestID:    response.RequestId,
			ItemID:       input.Body.ItemID,
			WebhookCode:  input.Body.WebhookCode,
		},
	}, nil
}

// RegisterAdminRoutes registers admin routes with the Huma API.
// Note: These routes should ONLY be registered in staging environment.
func RegisterAdminRoutes(api huma.API, handler *AdminHandler) {
	// POST /api/v1/admin/plaid/fire-webhook - Fire a Plaid webhook (staging only)
	huma.Register(api, huma.Operation{
		OperationID: "fireWebhook",
		Method:      http.MethodPost,
		Path:        "/api/v1/admin/plaid/fire-webhook",
		Summary:     "Fire a Plaid webhook (staging only)",
		Description: "Fires a Plaid webhook for testing in staging environment. Requires Auth0 authentication.",
		Tags:        []string{"Admin"},
		Security:    []map[string][]string{{"auth0": {}}},
	}, handler.FireWebhook)
}
