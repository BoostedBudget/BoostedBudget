// Package jobs contains River job definitions and workers for async processing.
package jobs

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/observability"
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

// errInvalidWebhookSignature is the wrapped error passed to river.JobCancel when the
// signature-validity guard refuses to dispatch a webhook. Issue #1514.
var errInvalidWebhookSignature = errors.New("refusing to process webhook with invalid signature")

// WebhookRepository defines the methods needed to interact with webhook storage.
type WebhookRepository interface {
	// GetByID fetches a webhook event by ID
	GetByID(ctx context.Context, id string) (*domain.WebhookEvent, error)

	// MarkFailed marks a webhook event as failed with an error message (Issue #1514).
	// Used by the signature-validity guard to record why a row was refused.
	MarkFailed(ctx context.Context, id string, errorMsg string) error
}

// TransactionSyncService defines the methods needed to process TRANSACTIONS webhooks.
type TransactionSyncService interface {
	// ProcessWebhook processes a TRANSACTIONS webhook and syncs transactions
	ProcessWebhook(ctx context.Context, webhook *domain.WebhookEvent) error
}

// ItemErrorService defines the methods needed to process ITEM webhooks.
type ItemErrorService interface {
	// ProcessWebhook processes an ITEM webhook for errors.
	//
	// Issue #1623: receivedAt is webhook_events.created_at — Postgres-stamped on insert
	// (webhook_repository.go's INSERT lets the column DEFAULT and reads it back via RETURNING),
	// never a Go clock. The LOGIN_REPAIRED path compares it against
	// linked_items.last_failure_asserted_at, which is also written by now(), so the ordering is
	// one database's clock against itself.
	ProcessWebhook(ctx context.Context, itemID, errorType, errorCode, errorMessage string, receivedAt time.Time) error
}

// InvestmentSyncService defines the methods needed to process HOLDINGS and
// INVESTMENTS_TRANSACTIONS webhooks. Issue #659: Plaid Investments.
type InvestmentSyncService interface {
	// ProcessWebhook routes HOLDINGS and INVESTMENTS_TRANSACTIONS webhooks to the
	// appropriate sync function.
	ProcessWebhook(ctx context.Context, webhook *domain.WebhookEvent) error
}

// LiabilitySyncService defines the methods needed to process LIABILITIES webhooks.
// Issue #910: Plaid Liabilities — Credit, Mortgage, Student Loans.
type LiabilitySyncService interface {
	// ProcessWebhook routes LIABILITIES webhooks to SyncLiabilities.
	ProcessWebhook(ctx context.Context, webhook *domain.WebhookEvent) error
}

// WebhookProcessingWorker processes webhook events from the queue.
type WebhookProcessingWorker struct {
	river.WorkerDefaults[WebhookProcessingArgs]

	webhookRepo        WebhookRepository
	transactionSyncSvc TransactionSyncService
	investmentSyncSvc  InvestmentSyncService
	liabilitySyncSvc   LiabilitySyncService
	itemErrorSvc       ItemErrorService
	logger             *slog.Logger
}

// WebhookProcessingWorkerDeps contains the dependencies for WebhookProcessingWorker.
type WebhookProcessingWorkerDeps struct {
	WebhookRepo        WebhookRepository
	TransactionSyncSvc TransactionSyncService
	InvestmentSyncSvc  InvestmentSyncService
	LiabilitySyncSvc   LiabilitySyncService
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
		investmentSyncSvc:  deps.InvestmentSyncSvc,
		liabilitySyncSvc:   deps.LiabilitySyncSvc,
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
func (w *WebhookProcessingWorker) Work(ctx context.Context, job *river.Job[WebhookProcessingArgs]) (err error) {
	args := job.Args

	w.logger.Info("starting webhook processing job",
		"job_id", job.ID,
		"webhook_id", args.WebhookID,
		"webhook_type", args.WebhookType,
		"item_id", args.ItemID,
		"attempt", job.Attempt,
	)

	startTime := time.Now()
	defer func() {
		status := "success"
		if err != nil {
			status = "failed"
		}
		observability.JobsCompleted.WithLabelValues(job.Kind, job.Queue, status).Inc()
		observability.JobDuration.WithLabelValues(job.Kind).Observe(time.Since(startTime).Seconds())
	}()

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

	// Issue #1514: defense-in-depth, mirroring SubscriptionWebhookWorker's guard
	// (subscription_webhook.go). HandlePlaidWebhook validates the JWT signature before
	// persisting the row, so this worker should never actually see signature_valid=false —
	// it refuses to dispatch anyway, in case something upstream (a bug, a future write path,
	// a pre-fix row) got a bad row into the queue.
	//
	// This will NEVER succeed on retry — the signature can't become valid — so it cancels
	// the job via river.JobCancel rather than returning nil. In River v0.43.0, returning nil
	// from Work is *successful completion*, not cancellation (see river.JobCancel's doc
	// comment: it "cancel[s] the job at the end of execution"); returning nil here would
	// record a refused, forged webhook as a completed job and emit
	// jobs_completed_total{status="success"} for it. river.JobCancel keeps the same
	// no-further-retry behavior while reporting the outcome truthfully.
	if !webhook.SignatureValid {
		observability.InvalidWebhookAttemptsTotal.WithLabelValues("plaid_worker_guard").Inc()
		w.logger.Error("refusing to process Plaid webhook with invalid signature; cancelling job",
			"job_id", job.ID,
			"webhook_id", args.WebhookID,
			"webhook_type", webhook.WebhookType,
		)
		if failErr := w.webhookRepo.MarkFailed(ctx, args.WebhookID, "rejected: signature_valid=false"); failErr != nil {
			// Don't swallow this: MarkFailed is what removes the row from
			// GetUnprocessed/GetUnprocessedByType eligibility (processed = true) and records
			// why it was refused. If it fails (e.g. a transient DB error), surface a plain
			// retryable error so River tries again — the security property still holds on
			// every attempt, since retrying re-fetches the webhook, finds SignatureValid still
			// false, and hits this same guard again rather than ever reaching dispatch.
			w.logger.Error("failed to mark webhook as failed after signature guard; will retry",
				"job_id", job.ID,
				"webhook_id", args.WebhookID,
				"error", failErr,
			)
			return fmt.Errorf("failed to record invalid-signature refusal: %w", failErr)
		}
		return river.JobCancel(errInvalidWebhookSignature)
	}

	// Route to appropriate processor based on webhook type
	switch webhook.WebhookType {
	case "TRANSACTIONS":
		err = w.processTransactionsWebhook(ctx, webhook)
	case "ITEM":
		err = w.processItemWebhook(ctx, webhook)
	case "LIABILITIES":
		// Issue #910: Plaid Liabilities — full sync via LiabilitySyncService.
		err = w.processLiabilitiesWebhook(ctx, webhook)
	case "HOLDINGS", "INVESTMENTS_TRANSACTIONS":
		// Issue #659: Plaid Investments — routed to InvestmentSyncService.
		err = w.processInvestmentWebhook(ctx, webhook)
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

// processInvestmentWebhook handles HOLDINGS and INVESTMENTS_TRANSACTIONS webhook processing.
// Issue #659: Plaid Investments
func (w *WebhookProcessingWorker) processInvestmentWebhook(ctx context.Context, webhook *domain.WebhookEvent) error {
	return w.investmentSyncSvc.ProcessWebhook(ctx, webhook)
}

// processLiabilitiesWebhook handles LIABILITIES webhook processing.
// Issue #910: Plaid Liabilities
func (w *WebhookProcessingWorker) processLiabilitiesWebhook(ctx context.Context, webhook *domain.WebhookEvent) error {
	return w.liabilitySyncSvc.ProcessWebhook(ctx, webhook)
}

// processItemWebhook handles ITEM webhook processing (errors, status changes)
func (w *WebhookProcessingWorker) processItemWebhook(ctx context.Context, webhook *domain.WebhookEvent) error {
	// Extract error details from webhook
	if webhook.ItemID == nil || *webhook.ItemID == "" {
		return errors.New("ITEM webhook missing item_id")
	}

	errorType := ""
	if webhook.WebhookCode != nil {
		errorType = *webhook.WebhookCode
	}

	errorCode := ""
	if webhook.ErrorCode != nil {
		errorCode = *webhook.ErrorCode
	}

	// webhook.CreatedAt is "when this webhook event was received", DEFAULTed by Postgres on
	// insert — see the interface doc comment for why nothing here may substitute time.Now().
	return w.itemErrorSvc.ProcessWebhook(ctx, *webhook.ItemID, errorType, errorCode, itemWebhookErrorMessage(webhook.Payload), webhook.CreatedAt)
}

// itemWebhookErrorMessage recovers Plaid's human-readable error_message from a stored ITEM
// webhook's raw payload. Issue #1609 P3-T3: domain.WebhookEvent/webhook_events only ever stored
// error_code (migration 000011), never error_message, so there is no domain field to forward. But
// Plaid's ITEM webhook schema always nests it the same way regardless of webhook_code
// (LOGIN_REQUIRED, ERROR, PENDING_EXPIRATION, PERMISSION_REVOKED all share one "error" object
// shape — see plaid-go's vendored openapi.yaml and https://plaid.com/docs/api/webhooks/), and this
// helper is only ever reached from the ITEM branch of processWebhookByType, so there is no
// cross-webhook-type shape variance to worry about here.
//
// Best-effort: an empty/malformed/absent payload (or one missing the field) yields "", which
// item_error_service.go's notification template now tolerates instead of rendering a dangling
// "with Chase: ." sentence. This must never fail webhook processing itself.
func itemWebhookErrorMessage(payload json.RawMessage) string {
	if len(payload) == 0 {
		return ""
	}

	var parsed struct {
		Error struct {
			ErrorMessage string `json:"error_message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(payload, &parsed); err != nil {
		return ""
	}

	return parsed.Error.ErrorMessage
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
