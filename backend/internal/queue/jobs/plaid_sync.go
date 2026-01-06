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
	"github.com/maxnussbaum/BoostedBudget/internal/services"
	"github.com/riverqueue/river"
)

// PlaidSyncArgs contains the arguments for a Plaid transaction sync job.
// This job fetches transactions from Plaid using the /transactions/sync API,
// handling cursor-based pagination, storing results, and triggering follow-up
// processing (categorization, budget evaluation).
type PlaidSyncArgs struct {
	// UserID is the ID of the user who owns the linked item
	UserID string `json:"user_id"`

	// ItemID is the internal UUID of the LinkedItem to sync
	// (not the Plaid item_id)
	ItemID string `json:"item_id"`

	// WebhookID optionally tracks which webhook triggered this sync
	// Used for tracking and debugging webhook processing
	WebhookID string `json:"webhook_id,omitempty"`
}

// Kind returns the unique identifier for this job type.
// Used by River to route jobs to the appropriate worker.
func (PlaidSyncArgs) Kind() string { return "plaid_sync" }

// PlaidSyncWorker processes Plaid sync jobs by fetching transactions
// from Plaid and storing them in the database.
type PlaidSyncWorker struct {
	river.WorkerDefaults[PlaidSyncArgs]

	syncService    services.TransactionSyncService
	linkedItemRepo repository.LinkedItemRepository
	jobQueue       queue.JobQueue
	logger         *slog.Logger
}

// PlaidSyncWorkerDeps contains the dependencies for PlaidSyncWorker.
// Using a struct makes it easier to construct and test.
type PlaidSyncWorkerDeps struct {
	SyncService    services.TransactionSyncService
	LinkedItemRepo repository.LinkedItemRepository
	JobQueue       queue.JobQueue
	Logger         *slog.Logger
}

// NewPlaidSyncWorker creates a new Plaid sync worker with the given dependencies.
func NewPlaidSyncWorker(deps PlaidSyncWorkerDeps) *PlaidSyncWorker {
	logger := deps.Logger
	if logger == nil {
		logger = slog.Default()
	}
	return &PlaidSyncWorker{
		syncService:    deps.SyncService,
		linkedItemRepo: deps.LinkedItemRepo,
		jobQueue:       deps.JobQueue,
		logger:         logger,
	}
}

// Work processes a Plaid sync job.
//
// The worker:
//  1. Retrieves the LinkedItem to get the access token and cursor
//  2. Calls SyncTransactions which handles all pagination and storage
//  3. Enqueues follow-up jobs for budget evaluation if transactions changed
//
// Errors are returned to River for retry with exponential backoff.
// Jobs are automatically retried up to MaxAttempts (default 5).
func (w *PlaidSyncWorker) Work(ctx context.Context, job *river.Job[PlaidSyncArgs]) error {
	args := job.Args

	w.logger.Info("starting plaid sync job",
		"job_id", job.ID,
		"user_id", args.UserID,
		"item_id", args.ItemID,
		"webhook_id", args.WebhookID,
		"attempt", job.Attempt,
	)

	startTime := time.Now()

	// Get the linked item with ownership validation
	linkedItem, err := w.linkedItemRepo.GetByIDAndUser(ctx, args.ItemID, args.UserID)
	if err != nil {
		if errors.Is(err, repository.ErrLinkedItemNotFound) {
			w.logger.Warn("linked item not found, skipping job",
				"item_id", args.ItemID,
				"user_id", args.UserID,
			)
			// Don't retry if item doesn't exist - it was likely deleted
			return nil
		}
		return fmt.Errorf("failed to get linked item: %w", err)
	}

	// Perform the sync - this is fully self-contained and handles:
	// - Cursor-based pagination until has_more=false
	// - Creating/updating/deleting transactions
	// - Applying categorization rules
	// - Syncing budget amounts
	// - Creating balance snapshots
	result, err := w.syncService.SyncTransactions(ctx, linkedItem)
	if err != nil {
		// Classify the error to determine retry behavior
		// First check if it's already a PlaidSyncError (from the service)
		var plaidErr *domain.PlaidSyncError
		if errors.As(err, &plaidErr) {
			// Already classified by the service
		} else {
			// Classify the raw error
			plaidErr = domain.ClassifyPlaidError(err)
		}

		w.logger.Error("sync failed",
			"job_id", job.ID,
			"item_id", args.ItemID,
			"error", err,
			"error_type", plaidErr.ErrorType,
			"error_code", plaidErr.ErrorCode,
			"duration_ms", time.Since(startTime).Milliseconds(),
		)

		// For permanent errors, don't retry
		if plaidErr.ErrorType == domain.PlaidErrorPermanent {
			w.logger.Warn("permanent plaid error, not retrying",
				"job_id", job.ID,
				"error_code", plaidErr.ErrorCode,
			)
			// Return nil to mark job as complete (not retryable)
			// TODO: Create user notification about the failure
			return nil
		}

		return fmt.Errorf("sync failed: %w", err)
	}

	totalChanges := len(result.Added) + len(result.Modified) + len(result.Removed)
	w.logger.Info("sync completed successfully",
		"job_id", job.ID,
		"item_id", args.ItemID,
		"added", len(result.Added),
		"modified", len(result.Modified),
		"removed", len(result.Removed),
		"total_changes", totalChanges,
		"duration_ms", time.Since(startTime).Milliseconds(),
	)

	// Enqueue follow-up job if there were transaction changes
	// This allows us to decouple sync from budget notifications
	if totalChanges > 0 && w.jobQueue != nil {
		if err := w.enqueueBudgetEvaluation(ctx, args.UserID); err != nil {
			// Log but don't fail the job - sync was successful
			w.logger.Error("failed to enqueue budget evaluation",
				"user_id", args.UserID,
				"error", err,
			)
		}
	}

	return nil
}

// enqueueBudgetEvaluation queues a budget evaluation job for the user.
// This checks if any budgets crossed thresholds and triggers notifications.
func (w *PlaidSyncWorker) enqueueBudgetEvaluation(ctx context.Context, userID string) error {
	// TODO: Implement once BudgetEvaluationArgs is defined
	// For now, this is a placeholder
	w.logger.Debug("would enqueue budget evaluation",
		"user_id", userID,
	)
	return nil
}

// PlaidSyncUniqueOpts returns the recommended uniqueness options for PlaidSyncArgs.
// This prevents duplicate syncs for the same item within a 5-minute window.
func PlaidSyncUniqueOpts() *queue.UniqueOpts {
	return &queue.UniqueOpts{
		ByArgs:   true,
		ByPeriod: 5 * time.Minute,
		ByState: []queue.JobState{
			queue.JobStateAvailable,
			queue.JobStateScheduled,
			queue.JobStateRunning,
			queue.JobStateRetryable,
			queue.JobStatePending,
		},
	}
}
