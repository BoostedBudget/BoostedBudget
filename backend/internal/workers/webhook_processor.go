package workers

import (
	"context"
	"log"
	"time"

	"github.com/maxnussbaum/BoostedBudget/internal/repository"
	"github.com/maxnussbaum/BoostedBudget/internal/services"
)

// WebhookProcessor handles periodic processing of unprocessed TRANSACTIONS webhooks
// Issue #256: Transaction Sync Service
//
// Architecture:
// - Polls webhook_events table every 30 seconds (configurable)
// - Fetches up to batchSize unprocessed TRANSACTIONS webhooks
// - Processes each via TransactionSyncService.ProcessWebhook()
// - Continues processing even if individual webhooks fail (errors logged)
// - Supports graceful shutdown via context cancellation
type WebhookProcessor struct {
	syncService services.TransactionSyncService
	webhookRepo repository.WebhookRepository
	interval    time.Duration
	batchSize   int
}

// NewWebhookProcessor creates a new WebhookProcessor instance
//
// Parameters:
//   - syncService: Handles the actual webhook processing logic (Plaid API calls)
//   - webhookRepo: Fetches unprocessed webhooks from database
//   - interval: How often to poll for webhooks (e.g., 30 seconds)
//   - batchSize: Maximum webhooks to process per iteration (e.g., 10)
//
// Example usage:
//
//	processor := NewWebhookProcessor(syncService, webhookRepo, 30*time.Second, 10)
//	go processor.Start(ctx) // Run in background goroutine
func NewWebhookProcessor(
	syncService services.TransactionSyncService,
	webhookRepo repository.WebhookRepository,
	interval time.Duration,
	batchSize int,
) *WebhookProcessor {
	return &WebhookProcessor{
		syncService: syncService,
		webhookRepo: webhookRepo,
		interval:    interval,
		batchSize:   batchSize,
	}
}

// ProcessPendingWebhooks processes a batch of unprocessed TRANSACTIONS webhooks
// Errors are logged but don't stop processing of other webhooks
//
// Flow:
// 1. Fetch up to batchSize unprocessed TRANSACTIONS webhooks (oldest first)
// 2. For each webhook:
//    a. Call syncService.ProcessWebhook() to sync transactions from Plaid
//    b. On error: Log error, continue to next webhook (webhook will be retried later)
//    c. On success: Webhook marked as processed by syncService
// 3. Return nil (errors don't propagate - they're logged)
func (w *WebhookProcessor) ProcessPendingWebhooks(ctx context.Context) error {
	// Get unprocessed TRANSACTIONS webhooks (respects retry schedule via next_retry_at)
	webhooks, err := w.webhookRepo.GetUnprocessedByType(ctx, "TRANSACTIONS", w.batchSize)
	if err != nil {
		log.Printf("Error fetching unprocessed webhooks: %v", err)
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
			log.Printf("Context cancelled, stopping webhook processing")
			return nil
		default:
			// Continue processing
		}

		// Process this webhook via TransactionSyncService
		if err := w.syncService.ProcessWebhook(ctx, webhook); err != nil {
			// Log error but continue processing other webhooks
			// The webhook will be retried later based on retry_count and next_retry_at
			log.Printf("Error processing webhook %s (item_id: %v): %v",
				webhook.ID, webhook.ItemID, err)
			// Don't return error - we want to continue processing
			continue
		}

		log.Printf("Successfully processed webhook %s (item_id: %v)",
			webhook.ID, webhook.ItemID)
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
