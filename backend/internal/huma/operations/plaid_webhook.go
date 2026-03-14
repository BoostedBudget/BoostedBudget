package operations

import (
	"context"
	"encoding/json"
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
	webhookService services.WebhookService
	webhookRepo    repository.WebhookRepository
	jobQueue       queue.JobQueue
	logger         *slog.Logger
}

// PlaidWebhookHandlerDeps contains dependencies for PlaidWebhookHandler.
type PlaidWebhookHandlerDeps struct {
	WebhookService services.WebhookService
	WebhookRepo    repository.WebhookRepository
	JobQueue       queue.JobQueue
	Logger         *slog.Logger
}

// NewPlaidWebhookHandler creates a new PlaidWebhookHandler instance.
func NewPlaidWebhookHandler(deps PlaidWebhookHandlerDeps) *PlaidWebhookHandler {
	logger := deps.Logger
	if logger == nil {
		logger = slog.Default()
	}
	return &PlaidWebhookHandler{
		webhookService: deps.WebhookService,
		webhookRepo:    deps.WebhookRepo,
		jobQueue:       deps.JobQueue,
		logger:         logger,
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
func (h *PlaidWebhookHandler) HandlePlaidWebhook(ctx context.Context, input *PlaidWebhookInput) (*PlaidWebhookOutput, error) {
	// Verify signature if service is available
	signatureValid := false
	if h.webhookService != nil && input.PlaidVerification != "" {
		valid, err := h.webhookService.VerifyWebhook(ctx, input.RawBody, input.PlaidVerification)
		if err != nil {
			// Log verification failure but DON'T fail the webhook
			// This ensures audit trail even for invalid signatures
		} else if valid {
			signatureValid = true
		}
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
			WebhookCode:     stringPtr(payload.WebhookCode),
			ItemID:          stringPtr(payload.ItemID),
			NewTransactions: intPtr(payload.NewTransactions),
			Payload:         json.RawMessage(input.RawBody),
			Signature:       stringPtr(input.PlaidVerification),
			SignatureValid:  signatureValid,
			Processed:       false,
		}

		// Extract error code if present
		if payload.Error != nil {
			if errorCode, ok := payload.Error["error_code"].(string); ok {
				webhookEvent.ErrorCode = &errorCode
			}
		}

		// Store webhook for audit trail (always, even for unverified signatures)
		if err := h.webhookRepo.Create(ctx, webhookEvent); err != nil {
			h.logger.Error("failed to store webhook",
				"webhook_type", payload.WebhookType,
				"error", err,
			)
			// Don't fail - return OK to prevent Plaid retry storms
		} else if signatureValid && h.jobQueue != nil {
			// Only enqueue processing for verified webhooks (matches Paddle webhook pattern)
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
		} else if !signatureValid {
			h.logger.Warn("webhook stored but not enqueued — invalid or missing signature",
				"webhook_id", webhookEvent.ID,
				"webhook_type", payload.WebhookType,
			)
		}
	}

	// Handle different webhook types
	var message string
	switch payload.WebhookType {
	case "TRANSACTIONS":
		message = h.handleTransactionsWebhook(&payload)
	case "ITEM":
		message = h.handleItemWebhook(&payload)
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

// Helper functions for pointer conversion (return nil for zero values)
func stringPtr(s string) *string {
	if s != "" {
		return &s
	}
	return nil
}

func intPtr(i int) *int {
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
