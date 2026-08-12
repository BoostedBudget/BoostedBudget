package operations

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/queue"
	"github.com/maxnussbaum/BoostedBudget/internal/queue/jobs"
	"github.com/maxnussbaum/BoostedBudget/internal/repository"
	"github.com/maxnussbaum/BoostedBudget/internal/services"
)

// PlaidWebhookHandler handles Plaid webhook HTTP requests.
type PlaidWebhookHandler struct {
	webhookService     services.WebhookService
	webhookRepo        repository.WebhookRepository
	invalidAttemptRepo repository.InvalidWebhookAttemptRepository
	jobQueue           queue.JobQueue
	logger             *slog.Logger
}

// PlaidWebhookHandlerDeps contains dependencies for PlaidWebhookHandler.
type PlaidWebhookHandlerDeps struct {
	WebhookService     services.WebhookService
	WebhookRepo        repository.WebhookRepository
	InvalidAttemptRepo repository.InvalidWebhookAttemptRepository
	JobQueue           queue.JobQueue
	Logger             *slog.Logger
}

// NewPlaidWebhookHandler creates a new PlaidWebhookHandler instance.
func NewPlaidWebhookHandler(deps PlaidWebhookHandlerDeps) *PlaidWebhookHandler {
	logger := deps.Logger
	if logger == nil {
		logger = slog.Default()
	}
	return &PlaidWebhookHandler{
		webhookService:     deps.WebhookService,
		webhookRepo:        deps.WebhookRepo,
		invalidAttemptRepo: deps.InvalidAttemptRepo,
		jobQueue:           deps.JobQueue,
		logger:             logger,
	}
}

// --- Input/Output Types ---

// PlaidWebhookPayload represents the structure of Plaid webhook notifications.
type PlaidWebhookPayload struct {
	WebhookType         string                 `json:"webhook_type" doc:"Type of webhook (TRANSACTIONS, ITEM)"`
	WebhookCode         string                 `json:"webhook_code" doc:"Specific webhook code"`
	ItemID              string                 `json:"item_id,omitempty" doc:"Plaid item ID"`
	Error               map[string]interface{} `json:"error,omitempty" doc:"Error details if applicable"`
	NewTransactions     int                    `json:"new_transactions,omitempty" doc:"Count of new transactions"`
	RemovedTransactions []string               `json:"removed_transactions,omitempty" doc:"IDs of removed transactions"`
}

// PlaidWebhookInput represents the input for the Plaid webhook.
type PlaidWebhookInput struct {
	PlaidVerification string `header:"Plaid-Verification" doc:"JWT signature from Plaid for verification"`
	RawBody           []byte
}

// PlaidWebhookOutput represents the response for webhook handling.
type PlaidWebhookOutput struct {
	Body PlaidWebhookResponseBody
}

// PlaidWebhookResponseBody is the response body for webhook handling.
type PlaidWebhookResponseBody struct {
	Status  string `json:"status" doc:"Processing status (ok or error)"`
	Message string `json:"message" doc:"Status message"`
}

// --- Handler Methods ---

// HandlePlaidWebhook handles POST /webhooks/plaid requests.
// Receives and processes webhook notifications from Plaid.
//
// Issue #202: Enhanced with JWT signature verification and database storage
//
// Webhook Types:
//   - TRANSACTIONS: New transactions available, historical updates
//   - ITEM: Connection status changes (LOGIN_REQUIRED, ERROR, etc.)
//
// Security: Verifies JWT signatures using ES256 (ECDSA P-256)
//
// Issue #1514: fails closed BEFORE persistence. A missing or cryptographically invalid signature
// is rejected with a non-2xx response and is NEVER written to webhook_events — only the event's
// provider, body length, and a SHA-256 digest are recorded via recordInvalidWebhookAttempt
// (bounded metadata, no raw payload). A verifier-unavailable outcome (e.g. the Plaid JWK fetch
// itself failed) is distinguished from a cryptographically invalid signature: it is treated as
// retryable — not as evidence of a forged request — and returns 503 without recording an invalid
// attempt, so Plaid retries once the verifier is healthy again.
func (h *PlaidWebhookHandler) HandlePlaidWebhook(ctx context.Context, input *PlaidWebhookInput) (*PlaidWebhookOutput, error) {
	signatureValid, verifyErr := h.verifySignature(ctx, input)
	if errors.Is(verifyErr, services.ErrWebhookVerifierUnavailable) {
		h.logger.Warn("Plaid webhook verifier temporarily unavailable; asking Plaid to retry", "error", verifyErr)
		return nil, huma.Error503ServiceUnavailable("webhook verifier temporarily unavailable, please retry")
	}
	if !signatureValid {
		h.logger.Warn("Rejecting Plaid webhook: missing or invalid signature", "error", verifyErr)
		recordInvalidWebhookAttempt(ctx, h.invalidAttemptRepo, "plaid", input.RawBody, h.logger)
		return nil, huma.Error401Unauthorized("invalid or missing webhook signature")
	}

	// Parse webhook payload from raw body
	var payload PlaidWebhookPayload
	if err := json.Unmarshal(input.RawBody, &payload); err != nil {
		// Still return 200 OK to prevent retry storms
		return &PlaidWebhookOutput{
			Body: PlaidWebhookResponseBody{
				Status:  "error",
				Message: "Invalid JSON payload",
			},
		}, nil
	}

	// Validate required fields
	if payload.WebhookType == "" || payload.WebhookCode == "" {
		return nil, huma.Error400BadRequest("Missing required fields")
	}

	// Store webhook in database for audit trail
	if h.webhookRepo != nil {
		webhookEvent := &domain.WebhookEvent{
			WebhookType:     payload.WebhookType,
			WebhookCode:     ptrIfNotEmpty(payload.WebhookCode),
			ItemID:          ptrIfNotEmpty(payload.ItemID),
			NewTransactions: ptrIfNotZero(payload.NewTransactions),
			Payload:         json.RawMessage(input.RawBody),
			Signature:       ptrIfNotEmpty(input.PlaidVerification),
			SignatureValid:  true,
			Processed:       false,
		}

		// Extract error code if present
		if payload.Error != nil {
			if errorCode, ok := payload.Error["error_code"].(string); ok {
				webhookEvent.ErrorCode = &errorCode
			}
		}

		if err := h.webhookRepo.Create(ctx, webhookEvent); err != nil {
			// Issue #1514: this is a VALID, verified webhook we failed to persist — returning
			// "ok" here would silently lose it forever, since Plaid never retries a 2xx
			// response. Return non-2xx so Plaid retries delivery.
			h.logger.Error("Failed to store valid Plaid webhook — returning error so Plaid retries",
				"webhook_type", payload.WebhookType,
				"error", err,
			)
			return nil, huma.Error500InternalServerError("failed to persist webhook")
		}

		if h.jobQueue != nil {
			jobArgs := jobs.WebhookProcessingArgs{
				WebhookID:   webhookEvent.ID,
				WebhookType: payload.WebhookType,
				ItemID:      payload.ItemID,
			}
			uniqueOpts := jobs.WebhookProcessingUniqueOpts()
			_, err := h.jobQueue.Enqueue(ctx, jobArgs, queue.WithUnique(*uniqueOpts))
			if err != nil {
				h.logger.Error("failed to enqueue webhook processing job",
					"webhook_id", webhookEvent.ID,
					"webhook_type", payload.WebhookType,
					"error", err,
				)
				// Don't fail - webhook is stored, can be processed later
			} else {
				h.logger.Info("webhook processing job enqueued",
					"webhook_id", webhookEvent.ID,
					"webhook_type", payload.WebhookType,
					"item_id", payload.ItemID,
				)
			}
		}
	}

	// Handle different webhook types
	var message string
	switch payload.WebhookType {
	case "TRANSACTIONS":
		message = h.handleTransactionsWebhook(&payload)
	case "ITEM":
		message = h.handleItemWebhook(&payload)
	case "LIABILITIES":
		// Phase 2 (#241): LIABILITIES product added. Full sync deferred to follow-up issue.
		message = "Liabilities webhook received"
	case "HOLDINGS":
		// Issue #659: Plaid Investments — processed via InvestmentSyncService.
		message = h.handleHoldingsWebhook(&payload)
	case "INVESTMENTS_TRANSACTIONS":
		// Issue #659: Plaid Investments — processed via InvestmentSyncService.
		message = h.handleInvestmentsTransactionsWebhook(&payload)
	default:
		message = "Webhook received"
	}

	// Always return 200 OK to acknowledge receipt
	return &PlaidWebhookOutput{
		Body: PlaidWebhookResponseBody{
			Status:  "ok",
			Message: message,
		},
	}, nil
}

// verifySignature reports whether input carries a cryptographically valid Plaid-Verification JWT.
//
// Distinguishes two failure classes (Issue #1514):
//   - services.ErrWebhookVerifierUnavailable: the verifier itself failed (e.g. the JWK fetch from
//     Plaid's API errored, or returned an unparsable key) — retryable, not evidence of forgery.
//   - any other error, or a missing header, or an unconfigured service: the signature is either
//     absent or cryptographically invalid — reject.
func (h *PlaidWebhookHandler) verifySignature(ctx context.Context, input *PlaidWebhookInput) (bool, error) {
	if input.PlaidVerification == "" {
		return false, errors.New("missing Plaid-Verification header")
	}
	if h.webhookService == nil {
		return false, errors.New("plaid webhook service not configured")
	}

	valid, err := h.webhookService.VerifyWebhook(ctx, input.RawBody, input.PlaidVerification)
	if err != nil {
		return false, err
	}
	if !valid {
		return false, errors.New("signature verification failed")
	}
	return true, nil
}

// handleTransactionsWebhook processes TRANSACTIONS webhook events.
func (h *PlaidWebhookHandler) handleTransactionsWebhook(payload *PlaidWebhookPayload) string {
	switch payload.WebhookCode {
	case "DEFAULT_UPDATE":
		return "Transaction update processed"
	case "INITIAL_UPDATE":
		return "Initial transactions processed"
	case "HISTORICAL_UPDATE":
		return "Historical update processed"
	case "TRANSACTIONS_REMOVED":
		return "Transactions removed processed"
	default:
		return "Transaction webhook processed"
	}
}

// handleHoldingsWebhook processes HOLDINGS webhook events.
func (h *PlaidWebhookHandler) handleHoldingsWebhook(payload *PlaidWebhookPayload) string {
	switch payload.WebhookCode {
	case "DEFAULT_UPDATE":
		return "Holdings update queued for sync"
	default:
		return "Holdings webhook queued for sync"
	}
}

// handleInvestmentsTransactionsWebhook processes INVESTMENTS_TRANSACTIONS webhook events.
func (h *PlaidWebhookHandler) handleInvestmentsTransactionsWebhook(payload *PlaidWebhookPayload) string {
	switch payload.WebhookCode {
	case "DEFAULT_UPDATE":
		return "Investment transactions update queued for sync"
	case "INITIAL_UPDATE":
		return "Initial investment transactions queued for sync"
	case "HISTORICAL_UPDATE":
		return "Historical investment transactions queued for sync"
	default:
		return "Investment transactions webhook queued for sync"
	}
}

// handleItemWebhook processes ITEM webhook events.
func (h *PlaidWebhookHandler) handleItemWebhook(payload *PlaidWebhookPayload) string {
	switch payload.WebhookCode {
	case "ERROR":
		return "Item error processed"
	case "LOGIN_REQUIRED":
		return "Login required processed"
	case "PENDING_EXPIRATION":
		return "Pending expiration processed"
	case "USER_PERMISSION_REVOKED":
		return "Permission revoked processed"
	case "WEBHOOK_UPDATE_ACKNOWLEDGED":
		return "Webhook update acknowledged"
	default:
		return "Item webhook processed"
	}
}

// ptrIfNotEmpty returns a pointer to the string if it's not empty, otherwise nil.
func ptrIfNotEmpty(s string) *string {
	if s != "" {
		return &s
	}
	return nil
}

// ptrIfNotZero returns a pointer to the int if it's not zero, otherwise nil.
func ptrIfNotZero(i int) *int {
	if i != 0 {
		return &i
	}
	return nil
}

// RegisterPlaidWebhookRoutes registers Plaid webhook routes with the Huma API.
// Note: This is a public endpoint, no authentication required (validated by Plaid JWT).
func RegisterPlaidWebhookRoutes(api huma.API, handler *PlaidWebhookHandler) {
	// POST /webhooks/plaid - Plaid webhook receiver
	huma.Register(api, huma.Operation{
		OperationID: "handlePlaidWebhook",
		Method:      http.MethodPost,
		Path:        "/webhooks/plaid",
		Summary:     "Handle Plaid webhook",
		Description: "Receives and processes webhook notifications from Plaid. Validates JWT signature and stores for async processing.",
		Tags:        []string{"Webhooks"},
		// No Security - this is a public endpoint validated by Plaid JWT signature
	}, handler.HandlePlaidWebhook)
}
