package operations

import (
	"context"
	"encoding/json"
	"log/slog"

	"github.com/danielgtaylor/huma/v2"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/queue"
	"github.com/maxnussbaum/BoostedBudget/internal/queue/jobs"
	"github.com/maxnussbaum/BoostedBudget/internal/repository"
	"github.com/maxnussbaum/BoostedBudget/internal/services"
)

// PaddleWebhookHandler handles Paddle webhook HTTP requests.
type PaddleWebhookHandler struct {
	webhookService services.PaddleWebhookService
	webhookRepo    repository.PaddleWebhookRepository
	jobQueue       queue.JobQueue
	logger         *slog.Logger
}

// PaddleWebhookHandlerDeps contains dependencies for PaddleWebhookHandler.
type PaddleWebhookHandlerDeps struct {
	WebhookService services.PaddleWebhookService
	WebhookRepo    repository.PaddleWebhookRepository
	JobQueue       queue.JobQueue
	Logger         *slog.Logger
}

// NewPaddleWebhookHandler creates a new PaddleWebhookHandler instance.
func NewPaddleWebhookHandler(deps PaddleWebhookHandlerDeps) *PaddleWebhookHandler {
	logger := deps.Logger
	if logger == nil {
		logger = slog.Default()
	}
	return &PaddleWebhookHandler{
		webhookService: deps.WebhookService,
		webhookRepo:    deps.WebhookRepo,
		jobQueue:       deps.JobQueue,
		logger:         logger,
	}
}

// PaddleWebhookPayload represents the structure of Paddle webhook notifications.
// Paddle webhooks have a standard structure with event_id, event_type, and data.
type PaddleWebhookPayload struct {
	EventID   string                 `json:"event_id" doc:"Unique event identifier from Paddle"`
	EventType string                 `json:"event_type" doc:"Type of event (subscription.created, transaction.completed, etc.)"`
	Data      map[string]interface{} `json:"data" doc:"Event-specific data payload"`
}

// PaddleWebhookInput represents the input for the Paddle webhook.
type PaddleWebhookInput struct {
	PaddleSignature string `header:"Paddle-Signature" doc:"HMAC-SHA256 signature from Paddle for verification"`
	RawBody         []byte
}

// PaddleWebhookOutput represents the response for webhook handling.
type PaddleWebhookOutput struct {
	Body PaddleWebhookResponseBody
}

// PaddleWebhookResponseBody is the response body for webhook handling.
type PaddleWebhookResponseBody struct {
	Status  string `json:"status" doc:"Processing status (ok or error)"`
	Message string `json:"message" doc:"Status message"`
}

// HandlePaddleWebhook handles POST /webhooks/paddle requests.
// Receives and processes webhook notifications from Paddle.
// Verifies HMAC-SHA256 signature, stores in database, and enqueues async processing.
func (h *PaddleWebhookHandler) HandlePaddleWebhook(ctx context.Context, input *PaddleWebhookInput) (*PaddleWebhookOutput, error) {
	signatureValid := false
	if h.webhookService != nil && input.PaddleSignature != "" {
		valid, err := h.webhookService.VerifyWebhook(input.PaddleSignature, string(input.RawBody))
		if err != nil {
			h.logger.Warn("Paddle webhook signature verification error", "error", err)
		} else if valid {
			signatureValid = true
		}
	}

	var payload PaddleWebhookPayload
	if err := json.Unmarshal(input.RawBody, &payload); err != nil {
		h.logger.Error("Invalid Paddle webhook JSON payload", "error", err)
		return &PaddleWebhookOutput{
			Body: PaddleWebhookResponseBody{
				Status:  "error",
				Message: "Invalid JSON payload",
			},
		}, nil
	}

	if payload.EventID == "" || payload.EventType == "" {
		h.logger.Error("Missing required fields in Paddle webhook",
			"event_id", payload.EventID,
			"event_type", payload.EventType,
		)
		return nil, huma.Error400BadRequest("Missing required fields: event_id or event_type")
	}

	if h.webhookRepo != nil {
		webhookEvent := &domain.PaddleWebhookEvent{
			EventID:            payload.EventID,
			EventType:          payload.EventType,
			Payload:            json.RawMessage(input.RawBody),
			Signature:          ptrIfNotEmpty(input.PaddleSignature),
			SignatureValid:     signatureValid,
			Processed:          false,
			ProcessingAttempts: 0,
		}

		existing, err := h.webhookRepo.GetByEventID(ctx, payload.EventID)
		if err == nil && existing != nil {
			h.logger.Info("Duplicate Paddle webhook received",
				"event_id", payload.EventID,
				"event_type", payload.EventType,
			)
			return &PaddleWebhookOutput{
				Body: PaddleWebhookResponseBody{
					Status:  "ok",
					Message: "Webhook already received",
				},
			}, nil
		}

		if err := h.webhookRepo.Create(ctx, webhookEvent); err != nil {
			h.logger.Error("Failed to store Paddle webhook",
				"event_id", payload.EventID,
				"event_type", payload.EventType,
				"error", err,
			)
		} else {
			h.logger.Info("Paddle webhook stored",
				"event_id", payload.EventID,
				"event_type", payload.EventType,
				"signature_valid", signatureValid,
			)

			if signatureValid && h.jobQueue != nil {
				jobArgs := jobs.SubscriptionWebhookArgs{
					WebhookID: webhookEvent.ID,
					EventID:   payload.EventID,
					EventType: payload.EventType,
				}

				_, err := h.jobQueue.Enqueue(ctx, jobArgs)
				if err != nil {
					h.logger.Error("Failed to enqueue webhook processing job",
						"webhook_id", webhookEvent.ID,
						"event_id", payload.EventID,
						"error", err,
					)
				} else {
					h.logger.Info("Webhook processing job enqueued",
						"webhook_id", webhookEvent.ID,
						"event_id", payload.EventID,
					)
				}
			} else if !signatureValid {
				h.logger.Warn("Webhook NOT enqueued due to invalid signature",
					"event_id", payload.EventID,
				)
			}
		}
	}

	return &PaddleWebhookOutput{
		Body: PaddleWebhookResponseBody{
			Status:  "ok",
			Message: "Webhook received",
		},
	}, nil
}

// RegisterPaddleWebhookRoutes registers Paddle webhook routes with the Huma API.
func RegisterPaddleWebhookRoutes(api huma.API, handler *PaddleWebhookHandler) {
	huma.Register(api, huma.Operation{
		OperationID: "paddle-webhook",
		Method:      "POST",
		Path:        "/webhooks/paddle",
		Summary:     "Receive Paddle webhook notifications",
		Description: "Public endpoint for Paddle to send subscription and payment event notifications. Verifies HMAC-SHA256 signature, stores in database, and enqueues async processing job.",
		Tags:        []string{"Webhooks", "Paddle"},
		// No Security - this is a public endpoint validated by Paddle HMAC signature
	}, handler.HandlePaddleWebhook)
}
