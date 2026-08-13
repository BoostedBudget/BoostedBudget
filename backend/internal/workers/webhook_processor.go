package workers

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/integration/posthog"
	"github.com/maxnussbaum/BoostedBudget/internal/observability"
	"github.com/maxnussbaum/BoostedBudget/internal/repository"
	"github.com/maxnussbaum/BoostedBudget/internal/services"
)

// WebhookProcessor is a LEGACY polling consumer of the webhook_events table.
//
// ⚠ NOT WIRED INTO PRODUCTION. Production dispatches Plaid webhooks via the River job
// jobs.NewWebhookProcessingWorker (wired in cmd/server/main.go, see the
// NewWebhookProcessingWorker call there) instead of this poller. This type is retained for
// reference/history (Issue #256: Transaction Sync Service; Issue #257: Item Error Handler)
// and is exercised only by its own test suite — grep for NewWebhookProcessor turning up
// nothing but comments and tests is expected and desired. Do not wire this into main.go;
// if a poller is ever needed again, prefer extending the River worker instead of reviving
// this one.
//
// Issue #1514: as dead code this was still a landmine — it dispatched webhooks to
// downstream services without ever checking SignatureValid, and the ITEM path can change a
// user's bank-connection status and send notifications/email based on an unverified payload.
// It now refuses to dispatch any webhook with SignatureValid == false (see
// refuseIfInvalidSignature below), mirroring the guard already present in the River worker
// (webhook_processing.go). GetUnprocessedByType does not filter on signature_valid itself, so
// this guard is this type's own responsibility, not the repository's.
//
// Architecture (as originally built, if ever revived):
// - Polls webhook_events table every 30 seconds (configurable)
// - Fetches up to batchSize unprocessed webhooks for each type:
//   * TRANSACTIONS webhooks → TransactionSyncService.ProcessWebhook()
//   * ITEM webhooks → ItemErrorService.ProcessWebhook()
// - Continues processing even if individual webhooks fail (errors logged)
// - Supports graceful shutdown via context cancellation
type WebhookProcessor struct {
	logger           *slog.Logger
	syncService      services.TransactionSyncService
	itemErrorService services.ItemErrorService
	webhookRepo      repository.WebhookRepository
	interval         time.Duration
	batchSize        int
	analytics        posthog.Analytics
}

// ItemWebhookPayload represents the structure of ITEM webhook payload from Plaid
type ItemWebhookPayload struct {
	Error struct {
		ErrorType    string `json:"error_type"`
		ErrorCode    string `json:"error_code"`
		ErrorMessage string `json:"error_message"`
	} `json:"error"`
}

// NewWebhookProcessor creates a new WebhookProcessor instance.
//
// ⚠ Not used by production — see the WebhookProcessor doc comment above. This constructor
// exists for the legacy poller's own test suite; do not call it from cmd/server/main.go or
// any other production wiring.
//
// Parameters:
//   - syncService: Handles TRANSACTIONS webhook processing (Plaid transaction sync)
//   - itemErrorService: Handles ITEM webhook processing (Plaid connection errors)
//   - webhookRepo: Fetches unprocessed webhooks from database
//   - interval: How often to poll for webhooks (e.g., 30 seconds)
//   - batchSize: Maximum webhooks to process per iteration per type (e.g., 10)
func NewWebhookProcessor(
	syncService services.TransactionSyncService,
	itemErrorService services.ItemErrorService,
	webhookRepo repository.WebhookRepository,
	interval time.Duration,
	batchSize int,
	logger *slog.Logger,
) *WebhookProcessor {
	if logger == nil {
		logger = slog.Default()
	}
	return &WebhookProcessor{
		logger:           logger,
		syncService:      syncService,
		itemErrorService: itemErrorService,
		webhookRepo:      webhookRepo,
		interval:         interval,
		batchSize:        batchSize,
	}
}

// SetAnalytics sets the analytics client for event capture
func (w *WebhookProcessor) SetAnalytics(analytics posthog.Analytics) {
	w.analytics = analytics
}

// ProcessPendingWebhooks processes batches of unprocessed webhooks for both types
// Errors are logged but don't stop processing of other webhooks
//
// Flow:
// 1. Fetch and process TRANSACTIONS webhooks:
//    a. Fetch up to batchSize unprocessed TRANSACTIONS webhooks
//    b. For each: Call syncService.ProcessWebhook()
// 2. Fetch and process ITEM webhooks:
//    a. Fetch up to batchSize unprocessed ITEM webhooks
//    b. For each: Parse error details from payload
//    c. Call itemErrorService.ProcessWebhook() with error details
// 3. On error: Log but continue (webhook retried later based on retry_count)
func (w *WebhookProcessor) ProcessPendingWebhooks(ctx context.Context) error {
	// Process TRANSACTIONS webhooks
	if err := w.processTransactionWebhooks(ctx); err != nil {
		// Log but don't return - continue to process ITEM webhooks
		w.logger.Error("error processing TRANSACTIONS webhooks", "error", err)
	}

	// Process ITEM webhooks
	if err := w.processItemWebhooks(ctx); err != nil {
		// Log but don't return - graceful degradation
		w.logger.Error("error processing ITEM webhooks", "error", err)
	}

	return nil
}

// refuseIfInvalidSignature reports whether webhook has an unverified signature and, if so,
// marks it as failed so it is never re-selected by GetUnprocessedByType (Issue #1514).
//
// GetUnprocessedByType (webhook_repository.go) deliberately does not filter on
// signature_valid, so every caller of it — this poller included — must guard before
// dispatching to a downstream service. Neither TransactionSyncService.ProcessWebhook nor
// ItemErrorService.ProcessWebhook checks SignatureValid themselves.
//
// Does not log the signature/HMAC value itself (only webhook id + type) - see finding F2 of
// issue #1514.
func (w *WebhookProcessor) refuseIfInvalidSignature(ctx context.Context, webhook *domain.WebhookEvent) bool {
	if webhook.SignatureValid {
		return false
	}

	observability.InvalidWebhookAttemptsTotal.WithLabelValues("plaid_legacy_poller_guard").Inc()
	w.logger.Error("refusing to dispatch webhook with invalid signature",
		"webhook_id", webhook.ID,
		"webhook_type", webhook.WebhookType,
	)

	if err := w.webhookRepo.MarkFailed(ctx, webhook.ID, "rejected: signature_valid=false"); err != nil {
		w.logger.Error("failed to mark webhook as failed after signature guard",
			"webhook_id", webhook.ID,
			"webhook_type", webhook.WebhookType,
			"error", err,
		)
	}

	return true
}

// processTransactionWebhooks processes unprocessed TRANSACTIONS webhooks
func (w *WebhookProcessor) processTransactionWebhooks(ctx context.Context) error {
	// Get unprocessed TRANSACTIONS webhooks (respects retry schedule via next_retry_at)
	webhooks, err := w.webhookRepo.GetUnprocessedByType(ctx, "TRANSACTIONS", w.batchSize)
	if err != nil {
		w.logger.Error("error fetching unprocessed TRANSACTIONS webhooks", "error", err)
		return err
	}

	if len(webhooks) == 0 {
		// No webhooks to process - not an error
		return nil
	}

	w.logger.Info("processing unprocessed TRANSACTIONS webhooks", "count", len(webhooks))

	// Process each webhook
	for i := range webhooks {
		webhook := &webhooks[i]

		// Check context cancellation
		select {
		case <-ctx.Done():
			w.logger.Info("context cancelled, stopping TRANSACTIONS webhook processing")
			return nil
		default:
			// Continue processing
		}

		// Capture webhook_received event
		distinctID := "system"
		if webhook.ItemID != nil {
			distinctID = *webhook.ItemID
		}
		p := map[string]interface{}{
			"webhook_type": webhook.WebhookType,
			"webhook_code": "",
		}
		if webhook.WebhookCode != nil {
			p["webhook_code"] = *webhook.WebhookCode
		}
		if w.analytics != nil {
			w.analytics.Capture(distinctID, posthog.EventWebhookReceived, p)
		}

		// Issue #1514: refuse to dispatch webhooks whose signature was never verified.
		// GetUnprocessedByType does not filter on signature_valid (see
		// webhook_repository.go GetUnprocessedByType doc comment), so this poller must guard
		// itself before handing a row to TransactionSyncService.
		if w.refuseIfInvalidSignature(ctx, webhook) {
			continue
		}

		// Process this webhook via TransactionSyncService
		if err := w.syncService.ProcessWebhook(ctx, webhook); err != nil {
			// Log error but continue processing other webhooks
			// The webhook will be retried later based on retry_count and next_retry_at
			w.logger.Error("error processing TRANSACTIONS webhook",
				"webhook_id", webhook.ID,
				"item_id", webhook.ItemID,
				"error", err,
			)
			// Don't return error - we want to continue processing
			continue
		}

		w.logger.Info("successfully processed TRANSACTIONS webhook",
			"webhook_id", webhook.ID,
			"item_id", webhook.ItemID,
		)
	}

	return nil
}

// processItemWebhooks processes unprocessed ITEM webhooks
func (w *WebhookProcessor) processItemWebhooks(ctx context.Context) error {
	// Get unprocessed ITEM webhooks (respects retry schedule via next_retry_at)
	webhooks, err := w.webhookRepo.GetUnprocessedByType(ctx, "ITEM", w.batchSize)
	if err != nil {
		w.logger.Error("error fetching unprocessed ITEM webhooks", "error", err)
		return err
	}

	if len(webhooks) == 0 {
		// No webhooks to process - not an error
		return nil
	}

	w.logger.Info("processing unprocessed ITEM webhooks", "count", len(webhooks))

	// Process each webhook
	for i := range webhooks {
		webhook := &webhooks[i]

		// Check context cancellation
		select {
		case <-ctx.Done():
			w.logger.Info("context cancelled, stopping ITEM webhook processing")
			return nil
		default:
			// Continue processing
		}

		// Extract itemID (required for processing)
		if webhook.ItemID == nil {
			w.logger.Warn("ITEM webhook has no item_id, skipping", "webhook_id", webhook.ID)
			continue
		}
		itemID := *webhook.ItemID

		// Capture webhook_received event
		p := map[string]interface{}{
			"webhook_type": webhook.WebhookType,
			"webhook_code": "",
		}
		if webhook.WebhookCode != nil {
			p["webhook_code"] = *webhook.WebhookCode
		}
		if w.analytics != nil {
			w.analytics.Capture(itemID, posthog.EventWebhookReceived, p)
		}

		// Parse error details from payload
		var payload ItemWebhookPayload
		if err := json.Unmarshal(webhook.Payload, &payload); err != nil {
			w.logger.Error("error parsing ITEM webhook payload", "webhook_id", webhook.ID, "error", err)
			// Continue to next webhook (invalid payload can't be processed)
			continue
		}

		// Validate required error fields
		if payload.Error.ErrorType == "" || payload.Error.ErrorCode == "" {
			w.logger.Warn("ITEM webhook missing error details in payload, skipping", "webhook_id", webhook.ID)
			continue
		}

		// Issue #1514: refuse to dispatch webhooks whose signature was never verified.
		// This path is especially dangerous to leave unguarded: ItemErrorService.ProcessWebhook
		// can change a user's bank-connection status and send notifications/email based on an
		// unverified payload (see item_error_service.go).
		if w.refuseIfInvalidSignature(ctx, webhook) {
			continue
		}

		// Process via ItemErrorService
		if err := w.itemErrorService.ProcessWebhook(
			ctx,
			itemID,
			payload.Error.ErrorType,
			payload.Error.ErrorCode,
			payload.Error.ErrorMessage,
		); err != nil {
			// Log error but continue processing other webhooks
			w.logger.Error("error processing ITEM webhook",
				"webhook_id", webhook.ID,
				"item_id", itemID,
				"error", err,
			)
			// Don't return error - we want to continue processing
			continue
		}

		w.logger.Info("successfully processed ITEM webhook",
			"webhook_id", webhook.ID,
			"item_id", itemID,
			"error_type", payload.Error.ErrorType,
		)
	}

	return nil
}

// Start begins the processor's periodic webhook processing.
// Runs until context is cancelled.
//
// ⚠ Not called from production — this is exercised only by this package's tests. See the
// WebhookProcessor doc comment for what actually runs (jobs.NewWebhookProcessingWorker).
//
// Behavior (if ever revived):
// - Runs ProcessPendingWebhooks() immediately on startup
// - Then runs periodically based on interval (e.g., every 30 seconds)
// - Gracefully shuts down when context is cancelled
func (w *WebhookProcessor) Start(ctx context.Context) {
	w.logger.Info("webhook processor starting", "interval", w.interval, "batch_size", w.batchSize)

	ticker := time.NewTicker(w.interval)
	defer ticker.Stop()

	// Check if context is already cancelled before first run
	select {
	case <-ctx.Done():
		w.logger.Info("webhook processor shutting down gracefully (context cancelled before start)")
		return
	default:
		// Run once immediately on startup
		if err := w.ProcessPendingWebhooks(ctx); err != nil {
			w.logger.Error("error processing pending webhooks on startup", "error", err)
		}
	}

	// Then run periodically
	for {
		select {
		case <-ctx.Done():
			w.logger.Info("webhook processor shutting down gracefully")
			return
		case <-ticker.C:
			if err := w.ProcessPendingWebhooks(ctx); err != nil {
				w.logger.Error("error processing pending webhooks", "error", err)
			}
		}
	}
}
