// Package jobs contains River job definitions and workers for async processing.
package jobs

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/queue"
	"github.com/maxnussbaum/BoostedBudget/internal/repository"
	"github.com/riverqueue/river"
)

// WebhookProcessingArgs contains the arguments for a webhook processing job.
// This job processes Plaid webhooks that have been stored in the database.
type WebhookProcessingArgs struct {
	// WebhookID is the ID of the stored webhook event to process
	WebhookID string `json:"webhook_id"`

	// WebhookType helps with routing to the correct processor
	WebhookType string `json:"webhook_type"`

	// ItemID is the Plaid item ID associated with the webhook (if available)
	ItemID string `json:"item_id,omitempty"`
}

// Kind returns the unique identifier for this job type.
func (WebhookProcessingArgs) Kind() string { return "webhook_processing" }

// WebhookRepository defines the methods needed to interact with webhook storage.
type WebhookRepository interface {
	// GetByID fetches a webhook event by ID
	GetByID(ctx context.Context, id string) (*domain.WebhookEvent, error)
}

// TransactionSyncService defines the methods needed to process TRANSACTIONS webhooks.
type TransactionSyncService interface {
	// ProcessWebhook processes a TRANSACTIONS webhook and syncs transactions
	ProcessWebhook(ctx context.Context, webhook *domain.WebhookEvent) error
}

// ItemErrorService defines the methods needed to process ITEM webhooks.
type ItemErrorService interface {
	// ProcessWebhook processes an ITEM webhook for errors
	ProcessWebhook(ctx context.Context, itemID, errorType, errorCode, errorMessage string) error
}

// WebhookProcessingWorker processes webhook events from the queue.
type WebhookProcessingWorker struct {
	river.WorkerDefaults[WebhookProcessingArgs]

	webhookRepo        WebhookRepository
	transactionSyncSvc TransactionSyncService
	itemErrorSvc       ItemErrorService
	logger             *slog.Logger
}

// WebhookProcessingWorkerDeps contains the dependencies for WebhookProcessingWorker.
type WebhookProcessingWorkerDeps struct {
	WebhookRepo        WebhookRepository
	TransactionSyncSvc TransactionSyncService
	ItemErrorSvc       ItemErrorService
	Logger             *slog.Logger
}

// NewWebhookProcessingWorker creates a new webhook processing worker.
func NewWebhookProcessingWorker(deps WebhookProcessingWorkerDeps) *WebhookProcessingWorker {
	logger := deps.Logger
	if logger == nil {
		logger = slog.Default()
	}
	return &WebhookProcessingWorker{
		webhookRepo:        deps.WebhookRepo,
		transactionSyncSvc: deps.TransactionSyncSvc,
		itemErrorSvc:       deps.ItemErrorSvc,
		logger:             logger,
	}
}

// Work processes a webhook processing job.
//
// The worker:
//  1. Fetches the webhook from the database
//  2. Routes to the appropriate processor based on webhook type
//  3. Returns errors for retry (temporary) or cancellation (permanent)
func (w *WebhookProcessingWorker) Work(ctx context.Context, job *river.Job[WebhookProcessingArgs]) error {
	args := job.Args

	w.logger.Info("starting webhook processing job",
		"job_id", job.ID,
		"webhook_id", args.WebhookID,
		"webhook_type", args.WebhookType,
		"item_id", args.ItemID,
		"attempt", job.Attempt,
	)

	startTime := time.Now()

	// Fetch webhook from database
	webhook, err := w.webhookRepo.GetByID(ctx, args.WebhookID)
	if err != nil {
		w.logger.Error("failed to fetch webhook",
			"job_id", job.ID,
			"webhook_id", args.WebhookID,
			"error", err,
		)
		// If webhook not found, don't retry (permanent failure)
		if errors.Is(err, repository.ErrWebhookNotFound) {
			return nil // Cancel job - webhook doesn't exist
		}
		return fmt.Errorf("failed to fetch webhook: %w", err)
	}

	// Skip if already processed
	if webhook.Processed {
		w.logger.Info("webhook already processed, skipping",
			"job_id", job.ID,
			"webhook_id", args.WebhookID,
		)
		return nil
	}

	// Route to appropriate processor based on webhook type
	switch webhook.WebhookType {
	case "TRANSACTIONS":
		err = w.processTransactionsWebhook(ctx, webhook)
	case "ITEM":
		err = w.processItemWebhook(ctx, webhook)
	default:
		w.logger.Warn("unknown webhook type, skipping",
			"job_id", job.ID,
			"webhook_type", webhook.WebhookType,
		)
		return nil // Don't retry unknown types
	}

	if err != nil {
		w.logger.Error("webhook processing failed",
			"job_id", job.ID,
			"webhook_id", args.WebhookID,
			"webhook_type", webhook.WebhookType,
			"error", err,
			"duration_ms", time.Since(startTime).Milliseconds(),
		)
		return fmt.Errorf("webhook processing failed: %w", err)
	}

	w.logger.Info("webhook processed successfully",
		"job_id", job.ID,
		"webhook_id", args.WebhookID,
		"webhook_type", webhook.WebhookType,
		"duration_ms", time.Since(startTime).Milliseconds(),
	)

	return nil
}

// processTransactionsWebhook handles TRANSACTIONS webhook processing
func (w *WebhookProcessingWorker) processTransactionsWebhook(ctx context.Context, webhook *domain.WebhookEvent) error {
	return w.transactionSyncSvc.ProcessWebhook(ctx, webhook)
}

// processItemWebhook handles ITEM webhook processing (errors, status changes)
func (w *WebhookProcessingWorker) processItemWebhook(ctx context.Context, webhook *domain.WebhookEvent) error {
	// Extract error details from webhook
	if webhook.ItemID == nil || *webhook.ItemID == "" {
		return errors.New("ITEM webhook missing item_id")
	}

	// Get error details from webhook code
	errorType := ""
	if webhook.WebhookCode != nil {
		errorType = *webhook.WebhookCode
	}

	errorCode := ""
	if webhook.ErrorCode != nil {
		errorCode = *webhook.ErrorCode
	}

	// Error message can be extracted from payload if needed
	errorMessage := ""

	return w.itemErrorSvc.ProcessWebhook(ctx, *webhook.ItemID, errorType, errorCode, errorMessage)
}

// WebhookProcessingUniqueOpts returns the recommended uniqueness options.
// This prevents duplicate processing of the same webhook.
func WebhookProcessingUniqueOpts() *queue.UniqueOpts {
	return &queue.UniqueOpts{
		ByArgs: true,
		// 10 minute period - webhooks should be processed quickly
		ByPeriod: 10 * time.Minute,
		ByState: []queue.JobState{
			queue.JobStateAvailable,
			queue.JobStateScheduled,
			queue.JobStateRunning,
			queue.JobStateRetryable,
			queue.JobStatePending,
		},
	}
}
