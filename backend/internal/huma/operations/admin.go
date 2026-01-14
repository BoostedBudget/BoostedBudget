package operations

import (
	"context"
	"net/http"
	"strings"

	"github.com/danielgtaylor/huma/v2"
	"github.com/maxnussbaum/BoostedBudget/internal/huma/inputs"
	"github.com/maxnussbaum/BoostedBudget/internal/repository"
	"github.com/maxnussbaum/BoostedBudget/internal/services"
)

// TrialGranter defines the interface for granting trials to users.
// Issue #698: Used by admin endpoint to grant trial during E2E tests.
type TrialGranter interface {
	GrantTrial(ctx context.Context, userID string) error
}

// AdminHandler handles admin-only endpoints for testing and troubleshooting.
// Routes are protected by AdminMiddleware with role validation and environment checks.
type AdminHandler struct {
	plaidService      services.PlaidService
	linkedItemRepo    repository.LinkedItemRepository
	encryptionService services.EncryptionService
	trialService      TrialGranter
}

// NewAdminHandler creates a new AdminHandler with required dependencies.
// The trialService parameter can be nil if trial grant functionality is not needed.
func NewAdminHandler(
	plaidService services.PlaidService,
	linkedItemRepo repository.LinkedItemRepository,
	encryptionService services.EncryptionService,
	trialService TrialGranter,
) *AdminHandler {
	return &AdminHandler{
		plaidService:      plaidService,
		linkedItemRepo:    linkedItemRepo,
		encryptionService: encryptionService,
		trialService:      trialService,
	}
}

// --- Input/Output Types ---

// FireWebhookInput represents the input for firing a Plaid webhook.
type FireWebhookInput struct {
	inputs.AdminAuthenticatedInput
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

// GrantTrialInput represents the input for granting trial to current user.
// Issue #698: Used by E2E tests to ensure test user has active subscription.
type GrantTrialInput struct {
	inputs.AdminAuthenticatedInput
}

// GrantTrialOutput represents the response for trial grant.
type GrantTrialOutput struct {
	Body GrantTrialResponseBody
}

// GrantTrialResponseBody defines the response body for trial grant.
type GrantTrialResponseBody struct {
	Success bool   `json:"success" doc:"Whether the trial was granted successfully"`
	Message string `json:"message" doc:"Description of the result"`
}

// --- Handler Methods ---

// FireWebhook fires a Plaid webhook for testing in QA environment.
//
// POST /api/v1/admin/plaid/fire-webhook
//
// Error Responses:
//   - 400: Invalid request body or validation failure
//   - 401: Not authenticated
//   - 403: Not an admin (checked by AdminMiddleware)
//   - 404: Linked item not found
//   - 500: Decryption failure or Plaid API error
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

// GrantTrial grants a 60-day trial subscription to the authenticated user.
// Issue #698: Used by E2E tests to ensure test user has active subscription.
//
// POST /api/v1/admin/subscription/grant-trial
//
// Error Responses:
//   - 401: Not authenticated
//   - 403: Not an admin (checked by AdminMiddleware)
//   - 409: User has already had a trial
//   - 500: Failed to grant trial
//   - 503: Trial service not configured
func (h *AdminHandler) GrantTrial(ctx context.Context, input *GrantTrialInput) (*GrantTrialOutput, error) {
	// Check if trial service is configured
	if h.trialService == nil {
		return nil, huma.Error503ServiceUnavailable("Trial service not configured")
	}

	// Grant trial to the authenticated user
	err := h.trialService.GrantTrial(ctx, input.UserID)
	if err != nil {
		// Check if user already had a trial (not an error for E2E purposes)
		if strings.Contains(err.Error(), "has already had a trial") {
			return &GrantTrialOutput{
				Body: GrantTrialResponseBody{
					Success: true,
					Message: "User already has or had a trial",
				},
			}, nil
		}
		return nil, huma.Error500InternalServerError("Failed to grant trial: " + err.Error())
	}

	return &GrantTrialOutput{
		Body: GrantTrialResponseBody{
			Success: true,
			Message: "Trial granted successfully",
		},
	}, nil
}

// RegisterAdminRoutes registers admin routes with the Huma API.
// Routes are protected by AdminMiddleware (environment and role validation).
func RegisterAdminRoutes(api huma.API, handler *AdminHandler) {
	// POST /api/v1/admin/plaid/fire-webhook - Fire a Plaid webhook (QA only)
	huma.Register(api, huma.Operation{
		OperationID: "fireWebhook",
		Method:      http.MethodPost,
		Path:        "/api/v1/admin/plaid/fire-webhook",
		Summary:     "Fire a Plaid webhook (QA only)",
		Description: "Fires a Plaid webhook for testing in QA environment. Requires Clerk authentication.",
		Tags:        []string{"Admin"},
		Security:    []map[string][]string{{"clerk": {}}},
	}, handler.FireWebhook)

	// POST /api/v1/admin/subscription/grant-trial - Grant trial to current user (QA only)
	// Issue #698: Used by E2E tests to ensure test user has active subscription.
	huma.Register(api, huma.Operation{
		OperationID: "grantTrial",
		Method:      http.MethodPost,
		Path:        "/api/v1/admin/subscription/grant-trial",
		Summary:     "Grant trial subscription (QA only)",
		Description: "Grants a 60-day trial to the authenticated user. For E2E testing. Returns success if user already has trial.",
		Tags:        []string{"Admin"},
		Security:    []map[string][]string{{"clerk": {}}},
	}, handler.GrantTrial)
}
