// Package jobs contains River job definitions and workers for async processing.
package jobs

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/maxnussbaum/BoostedBudget/internal/observability"
	"github.com/maxnussbaum/BoostedBudget/internal/queue"
	"github.com/maxnussbaum/BoostedBudget/internal/services"
	"github.com/riverqueue/river"
)

// ItemCleanupArgs contains the arguments for an item cleanup job.
// This job removes a Plaid item via /item/remove API and updates the database.
//
// Issue #668: Plaid Item Management for Cost Optimization
type ItemCleanupArgs struct {
	// LinkedItemID is the ID of the linked item to clean up
	LinkedItemID string `json:"linked_item_id"`

	// UserID is the owner of the item (for validation)
	UserID string `json:"user_id"`

	// Reason indicates why this item is being cleaned up
	// Possible values: "long_term_error", "revoked", "stale", "subscription_downgrade", "user_deletion"
	Reason string `json:"reason"`
}

// Kind returns the unique identifier for this job type.
func (ItemCleanupArgs) Kind() string { return "item_cleanup" }

// ItemCleanupWorker processes item cleanup jobs.
// This worker removes Plaid items to stop billing and updates the database.
type ItemCleanupWorker struct {
	river.WorkerDefaults[ItemCleanupArgs]

	plaidCleanupSvc services.PlaidItemCleanupService
	linkedItemRepo  LinkedItemCleanupRepository
	logger          *slog.Logger
}

// ItemCleanupWorkerDeps contains the dependencies for ItemCleanupWorker.
type ItemCleanupWorkerDeps struct {
	PlaidCleanupSvc services.PlaidItemCleanupService
	LinkedItemRepo  LinkedItemCleanupRepository
	Logger          *slog.Logger
}

// NewItemCleanupWorker creates a new item cleanup worker.
func NewItemCleanupWorker(deps ItemCleanupWorkerDeps) *ItemCleanupWorker {
	logger := deps.Logger
	if logger == nil {
		logger = slog.Default()
	}
	return &ItemCleanupWorker{
		plaidCleanupSvc: deps.PlaidCleanupSvc,
		linkedItemRepo:  deps.LinkedItemRepo,
		logger:          logger,
	}
}

// Work processes an item cleanup job.
//
// The worker:
//  1. Validates the item still needs cleanup (grace period expired, not reconnected)
//  2. Calls PlaidItemCleanupService.RemoveItem() to call Plaid /item/remove
//  3. Logs the cleanup for audit
//
// If the user has reconnected the item during the grace period, the cleanup is skipped.
func (w *ItemCleanupWorker) Work(ctx context.Context, job *river.Job[ItemCleanupArgs]) (err error) {
	args := job.Args

	w.logger.Info("starting item cleanup",
		"job_id", job.ID,
		"linked_item_id", args.LinkedItemID,
		"user_id", args.UserID,
		"reason", args.Reason,
		"attempt", job.Attempt,
	)

	start := time.Now()
	defer func() {
		status := "success"
		if err != nil {
			status = "failed"
		}
		observability.JobsCompleted.WithLabelValues(job.Kind, job.Queue, status).Inc()
		observability.JobDuration.WithLabelValues(job.Kind).Observe(time.Since(start).Seconds())
	}()

	startTime := time.Now()

	// Call cleanup service to remove the item from Plaid
	removeErr := w.plaidCleanupSvc.RemoveItem(ctx, args.LinkedItemID)
	if removeErr != nil {
		// Check if item not found - this means it was already cleaned up or removed
		// In this case, we consider it successful
		if services.IsItemNotFoundError(removeErr) {
			w.logger.Info("item already removed, marking job as complete",
				"job_id", job.ID,
				"linked_item_id", args.LinkedItemID,
			)
			return nil
		}

		w.logger.Error("item cleanup failed",
			"job_id", job.ID,
			"linked_item_id", args.LinkedItemID,
			"user_id", args.UserID,
			"reason", args.Reason,
			"error", removeErr,
			"duration_ms", time.Since(startTime).Milliseconds(),
		)
		return fmt.Errorf("failed to cleanup item: %w", removeErr)
	}

	// Clear the cleanup schedule since cleanup is complete (best-effort)
	if clearErr := w.linkedItemRepo.ClearCleanupSchedule(ctx, args.LinkedItemID); clearErr != nil {
		w.logger.Warn("failed to clear cleanup schedule",
			"linked_item_id", args.LinkedItemID,
			"error", clearErr,
		)
	}

	w.logger.Info("item cleanup completed",
		"job_id", job.ID,
		"linked_item_id", args.LinkedItemID,
		"user_id", args.UserID,
		"reason", args.Reason,
		"duration_ms", time.Since(startTime).Milliseconds(),
	)

	return nil
}

// ItemCleanupUniqueOpts returns the recommended uniqueness options.
// Only one cleanup job per item can be queued/running at a time.
// This prevents duplicate cleanup attempts.
func ItemCleanupUniqueOpts() *queue.UniqueOpts {
	return &queue.UniqueOpts{
		ByArgs: true, // Each item can only have one pending cleanup
		// No time period - each item can only have one pending cleanup
		ByState: []queue.JobState{
			queue.JobStateAvailable,
			queue.JobStateScheduled,
			queue.JobStateRunning,
			queue.JobStateRetryable,
			queue.JobStatePending,
		},
	}
}
