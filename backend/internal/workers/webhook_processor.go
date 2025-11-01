package workers

import (
	"context"
	"encoding/json"
	"log"
	"time"

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
	syncService      services.TransactionSyncService
	itemErrorService services.ItemErrorService
	webhookRepo      repository.WebhookRepository
	interval         time.Duration
	batchSize        int
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
) *WebhookProcessor {
	return &WebhookProcessor{
		syncService:      syncService,
		itemErrorService: itemErrorService,
		webhookRepo:      webhookRepo,
		interval:         interval,
		batchSize:        batchSize,
	}
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
		log.Printf("Error processing TRANSACTIONS webhooks: %v", err)
	}

	// Process ITEM webhooks
	if err := w.processItemWebhooks(ctx); err != nil {
		// Log but don't return - graceful degradation
		log.Printf("Error processing ITEM webhooks: %v", err)
	}

	return nil
}

// processTransactionWebhooks processes unprocessed TRANSACTIONS webhooks
func (w *WebhookProcessor) processTransactionWebhooks(ctx context.Context) error {
	// Get unprocessed TRANSACTIONS webhooks (respects retry schedule via next_retry_at)
	webhooks, err := w.webhookRepo.GetUnprocessedByType(ctx, "TRANSACTIONS", w.batchSize)
	if err != nil {
		log.Printf("Error fetching unprocessed TRANSACTIONS webhooks: %v", err)
		return err
	}

	if len(webhooks) == 0 {
		// No webhooks to process - not an error
		return nil
	}

	log.Printf("Processing %d unprocessed TRANSACTIONS webhooks", len(webhooks))

	// Process each webhook
	for i := range webhooks {
		webhook := &webhooks[i]

		// Check context cancellation
		select {
		case <-ctx.Done():
			log.Printf("Context cancelled, stopping TRANSACTIONS webhook processing")
			return nil
		default:
			// Continue processing
		}

		// Process this webhook via TransactionSyncService
		if err := w.syncService.ProcessWebhook(ctx, webhook); err != nil {
			// Log error but continue processing other webhooks
			// The webhook will be retried later based on retry_count and next_retry_at
			log.Printf("Error processing TRANSACTIONS webhook %s (item_id: %v): %v",
				webhook.ID, webhook.ItemID, err)
			// Don't return error - we want to continue processing
			continue
		}

		log.Printf("Successfully processed TRANSACTIONS webhook %s (item_id: %v)",
			webhook.ID, webhook.ItemID)
	}

	return nil
}

// processItemWebhooks processes unprocessed ITEM webhooks
func (w *WebhookProcessor) processItemWebhooks(ctx context.Context) error {
	// Get unprocessed ITEM webhooks (respects retry schedule via next_retry_at)
	webhooks, err := w.webhookRepo.GetUnprocessedByType(ctx, "ITEM", w.batchSize)
	if err != nil {
		log.Printf("Error fetching unprocessed ITEM webhooks: %v", err)
		return err
	}

	if len(webhooks) == 0 {
		// No webhooks to process - not an error
		return nil
	}

	log.Printf("Processing %d unprocessed ITEM webhooks", len(webhooks))

	// Process each webhook
	for i := range webhooks {
		webhook := &webhooks[i]

		// Check context cancellation
		select {
		case <-ctx.Done():
			log.Printf("Context cancelled, stopping ITEM webhook processing")
			return nil
		default:
			// Continue processing
		}

		// Extract itemID (required for processing)
		if webhook.ItemID == nil {
			log.Printf("Warning: ITEM webhook %s has no item_id, skipping", webhook.ID)
			continue
		}
		itemID := *webhook.ItemID

		// Parse error details from payload
		var payload ItemWebhookPayload
		if err := json.Unmarshal(webhook.Payload, &payload); err != nil {
			log.Printf("Error parsing ITEM webhook payload for %s: %v", webhook.ID, err)
			// Continue to next webhook (invalid payload can't be processed)
			continue
		}

		// Validate required error fields
		if payload.Error.ErrorType == "" || payload.Error.ErrorCode == "" {
			log.Printf("Warning: ITEM webhook %s missing error details in payload, skipping", webhook.ID)
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
			log.Printf("Error processing ITEM webhook %s (item_id: %s): %v",
				webhook.ID, itemID, err)
			// Don't return error - we want to continue processing
			continue
		}

		log.Printf("Successfully processed ITEM webhook %s (item_id: %s, error_type: %s)",
			webhook.ID, itemID, payload.Error.ErrorType)
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
	log.Printf("Webhook processor starting with interval: %s, batch size: %d", w.interval, w.batchSize)

	ticker := time.NewTicker(w.interval)
	defer ticker.Stop()

	// Check if context is already cancelled before first run
	select {
	case <-ctx.Done():
		log.Printf("Webhook processor shutting down gracefully (context cancelled before start)")
		return
	default:
		// Run once immediately on startup
		if err := w.ProcessPendingWebhooks(ctx); err != nil {
			log.Printf("Error processing pending webhooks on startup: %v", err)
		}
	}

	// Then run periodically
	for {
		select {
		case <-ctx.Done():
			log.Printf("Webhook processor shutting down gracefully")
			return
		case <-ticker.C:
			if err := w.ProcessPendingWebhooks(ctx); err != nil {
				log.Printf("Error processing pending webhooks: %v", err)
			}
		}
	}
}
