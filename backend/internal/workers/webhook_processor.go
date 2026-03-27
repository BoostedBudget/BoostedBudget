package workers

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	"github.com/maxnussbaum/BoostedBudget/internal/integration/posthog"
	"github.com/maxnussbaum/BoostedBudget/internal/repository"
	"github.com/maxnussbaum/BoostedBudget/internal/services"
)

// WebhookProcessor handles periodic processing of unprocessed webhooks
// Issue #256: Transaction Sync Service
// Issue #257: Item Error Handler - Process ITEM Webhooks
//
// Architecture:
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

// NewWebhookProcessor creates a new WebhookProcessor instance
//
// Parameters:
//   - syncService: Handles TRANSACTIONS webhook processing (Plaid transaction sync)
//   - itemErrorService: Handles ITEM webhook processing (Plaid connection errors)
//   - webhookRepo: Fetches unprocessed webhooks from database
//   - interval: How often to poll for webhooks (e.g., 30 seconds)
//   - batchSize: Maximum webhooks to process per iteration per type (e.g., 10)
//
// Example usage:
//
//	processor := NewWebhookProcessor(syncService, itemErrorService, webhookRepo, 30*time.Second, 10)
//	go processor.Start(ctx) // Run in background goroutine
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

// Start begins the processor's periodic webhook processing
// Runs until context is cancelled
//
// Behavior:
// - Runs ProcessPendingWebhooks() immediately on startup
// - Then runs periodically based on interval (e.g., every 30 seconds)
// - Gracefully shuts down when context is cancelled
//
// Usage:
//
//	processor := NewWebhookProcessor(...)
//	go processor.Start(ctx) // Run in background
//	// ... later ...
//	cancel() // Triggers graceful shutdown
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
