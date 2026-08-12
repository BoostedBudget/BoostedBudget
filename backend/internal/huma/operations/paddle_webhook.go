package operations

import (
	"context"
	"encoding/json"
	"errors"
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
	webhookService     services.PaddleWebhookService
	webhookRepo        repository.PaddleWebhookRepository
	invalidAttemptRepo repository.InvalidWebhookAttemptRepository
	jobQueue           queue.JobQueue
	logger             *slog.Logger
}

// PaddleWebhookHandlerDeps contains dependencies for PaddleWebhookHandler.
type PaddleWebhookHandlerDeps struct {
	WebhookService     services.PaddleWebhookService
	WebhookRepo        repository.PaddleWebhookRepository
	InvalidAttemptRepo repository.InvalidWebhookAttemptRepository
	JobQueue           queue.JobQueue
	Logger             *slog.Logger
}

// NewPaddleWebhookHandler creates a new PaddleWebhookHandler instance.
func NewPaddleWebhookHandler(deps PaddleWebhookHandlerDeps) *PaddleWebhookHandler {
	logger := deps.Logger
	if logger == nil {
		logger = slog.Default()
	}
	return &PaddleWebhookHandler{
		webhookService:     deps.WebhookService,
		webhookRepo:        deps.WebhookRepo,
		invalidAttemptRepo: deps.InvalidAttemptRepo,
		jobQueue:           deps.JobQueue,
		logger:             logger,
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
//
// Issue #1514: fails closed BEFORE persistence. A missing or cryptographically invalid signature
// is rejected with a non-2xx response and is NEVER written to paddle_webhook_events — only the
// event's provider, body length, and a SHA-256 digest are recorded via recordInvalidWebhookAttempt
// (bounded metadata, no raw payload, no attacker-chosen event_id in a UNIQUE column). This closes
// the idempotency-poisoning vector: since an invalid attempt never reaches the table, an attacker
// can no longer plant a row under a future legitimate event_id to suppress that real webhook.
func (h *PaddleWebhookHandler) HandlePaddleWebhook(ctx context.Context, input *PaddleWebhookInput) (*PaddleWebhookOutput, error) {
	signatureValid, verifyErr := h.verifySignature(input)
	if !signatureValid {
		h.logger.Warn("Rejecting Paddle webhook: missing or invalid signature", "error", verifyErr)
		recordInvalidWebhookAttempt(ctx, h.invalidAttemptRepo, "paddle", input.RawBody, h.logger)
		return nil, huma.Error401Unauthorized("invalid or missing webhook signature")
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

	// Issue #1514 item 3: this guard is defensive/structural, not a reachable production state.
	// In the wired deployment (backend/cmd/server/main.go) HandlePaddleWebhook is only ever
	// registered with a non-nil WebhookRepo: the branch that supplies a non-nil WebhookService
	// (main.go:2048-2064) is nested inside the same `db != nil && cfg.ClerkSecretKey != ""` block
	// that unconditionally assigns paddleWebhookRepo (main.go:365,441), so WebhookService != nil
	// implies WebhookRepo != nil there; and the fallback, unverified branch (main.go:2065-2079)
	// checks `paddleWebhookRepo != nil` explicitly before registering. There is no route
	// registration path in main.go that wires a verified/unverified handler with a nil repo. The
	// guard exists purely so unit tests (see setupTestHandler) and any future caller that
	// constructs the handler directly stay nil-safe rather than panicking — see
	// TestHandlePaddleWebhook_NilRepo_AcksWithoutPersisting_DocumentedUnreachable, which pins the
	// current (silent-ack) behaviour for that unreachable-in-prod configuration so a future
	// change to it is a deliberate choice, not a surprise.
	if h.webhookRepo != nil {
		webhookEvent := &domain.PaddleWebhookEvent{
			EventID:            payload.EventID,
			EventType:          payload.EventType,
			Payload:            json.RawMessage(input.RawBody),
			Signature:          ptrIfNotEmpty(input.PaddleSignature),
			SignatureValid:     true,
			Processed:          false,
			ProcessingAttempts: 0,
		}

		existing, err := h.webhookRepo.GetByEventID(ctx, payload.EventID)
		if err == nil && existing != nil {
			if !existing.SignatureValid {
				// Issue #1514 item 1 (defence-in-depth): the stored row for this event_id was
				// itself recorded with signature_valid=false. Going forward that should be rare
				// — the fail-closed check above never persists an invalid webhook, and migration
				// 000147 purged legacy invalid rows — but ingestion is the one layer in this
				// system that didn't check SignatureValid on the duplicate path (the admin replay
				// guard in admin_paddle_webhooks.go and the Paddle worker in
				// queue/jobs/subscription_webhook.go both already do). Never treat a poisoned row
				// as a legitimate duplicate of THIS now-verified webhook: doing so would silently
				// drop a genuine billing event and return 200, after which Paddle never retries.
				// Return non-2xx instead so Paddle retries delivery until the poisoned row is
				// resolved (deleted or repaired) out-of-band.
				h.logger.Error("Refusing to treat signature_valid=false stored row as a duplicate of a verified webhook — returning error so Paddle retries",
					"event_id", payload.EventID,
					"event_type", payload.EventType,
				)
				return nil, huma.Error500InternalServerError("stored webhook record failed prior signature verification; refusing to suppress a verified event with the same event_id")
			}

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
			if errors.Is(err, repository.ErrPaddleWebhookDuplicate) {
				// Issue #1514 item 2: check-then-insert is racy — two concurrent deliveries of
				// the same valid event can both pass GetByEventID above before either commits,
				// so the loser's Create() hits the unique constraint on event_id. Nothing was
				// lost: the winner already persisted the row (and already enqueued processing).
				// Treat this exactly like the "already received" branch above rather than
				// reporting a fabricated persistence outage that makes Paddle retry a webhook
				// that was, in fact, already handled.
				h.logger.Info("Concurrent duplicate Paddle webhook detected on insert — treating as idempotent success",
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
			// Issue #1514: this is a VALID, verified webhook we failed to persist — returning
			// "ok" here would silently lose a real billing event forever, since Paddle never
			// retries a 2xx response. Return non-2xx so Paddle retries delivery.
			h.logger.Error("Failed to store valid Paddle webhook — returning error so Paddle retries",
				"event_id", payload.EventID,
				"event_type", payload.EventType,
				"error", err,
			)
			return nil, huma.Error500InternalServerError("failed to persist webhook")
		}

		h.logger.Info("Paddle webhook stored",
			"event_id", payload.EventID,
			"event_type", payload.EventType,
		)

		if h.jobQueue != nil {
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
		}
	}

	return &PaddleWebhookOutput{
		Body: PaddleWebhookResponseBody{
			Status:  "ok",
			Message: "Webhook received",
		},
	}, nil
}

// verifySignature reports whether input carries a cryptographically valid Paddle-Signature.
// Every non-valid outcome (missing header, unconfigured service, malformed signature, HMAC
// mismatch) returns a descriptive error for logging — Paddle's HMAC scheme has no notion of a
// "verifier unavailable" state (no external call is involved), so every failure here means
// reject, unlike the Plaid path's verifier-unavailable/invalid distinction.
func (h *PaddleWebhookHandler) verifySignature(input *PaddleWebhookInput) (bool, error) {
	if input.PaddleSignature == "" {
		return false, errors.New("missing Paddle-Signature header")
	}
	if h.webhookService == nil {
		return false, errors.New("paddle webhook service not configured")
	}

	valid, err := h.webhookService.VerifyWebhook(input.PaddleSignature, string(input.RawBody))
	if err != nil {
		return false, err
	}
	if !valid {
		return false, errors.New("signature verification failed")
	}
	return true, nil
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
