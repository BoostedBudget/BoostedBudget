// Package jobs contains River job definitions and workers for async processing.
package jobs

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/observability"
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

// TxBeginner is an interface for starting database transactions.
// This allows the worker to acquire row-level locks for concurrent sync prevention.
type TxBeginner interface {
	Begin(ctx context.Context) (pgx.Tx, error)
}

// PlaidSyncWorker processes Plaid sync jobs by fetching transactions
// from Plaid and storing them in the database.
type PlaidSyncWorker struct {
	river.WorkerDefaults[PlaidSyncArgs]

	syncService    services.TransactionSyncService
	linkedItemRepo repository.LinkedItemRepository
	jobQueue       queue.JobQueue
	logger         *slog.Logger
	db             TxBeginner
}

// PlaidSyncWorkerDeps contains the dependencies for PlaidSyncWorker.
// Using a struct makes it easier to construct and test.
type PlaidSyncWorkerDeps struct {
	SyncService    services.TransactionSyncService
	LinkedItemRepo repository.LinkedItemRepository
	JobQueue       queue.JobQueue
	Logger         *slog.Logger
	DB             TxBeginner
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
		db:             deps.DB,
	}
}

// SetJobQueue sets the job queue for enqueueing follow-up jobs.
// This resolves the circular dependency: worker registration before client creation.
func (w *PlaidSyncWorker) SetJobQueue(jq queue.JobQueue) {
	w.jobQueue = jq
}

// Work processes a Plaid sync job.
//
// The worker:
//  1. Acquires an exclusive row-level lock on the LinkedItem to prevent concurrent syncs
//  2. Retrieves the LinkedItem to get the access token and cursor
//  3. Calls SyncTransactions which handles all pagination and storage
//  4. Releases the lock and enqueues follow-up jobs if transactions changed
//
// The row-level lock (FOR UPDATE) ensures only one worker can sync a given item at a time.
// If another job is already syncing, this job will block until the lock is released.
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
	defer func() {
		observability.PlaidSyncDuration.Observe(time.Since(startTime).Seconds())
	}()

	// Acquire row-level lock via transaction
	// This prevents concurrent sync operations on the same linked item
	linkedItem, tx, err := w.acquireLockAndGetItem(ctx, args.ItemID, args.UserID)
	if err != nil {
		if errors.Is(err, repository.ErrLinkedItemNotFound) {
			w.logger.Warn("linked item not found, skipping job",
				"item_id", args.ItemID,
				"user_id", args.UserID,
			)
			// Don't retry if item doesn't exist - it was likely deleted
			return nil
		}
		if errors.Is(err, errOwnershipMismatch) {
			w.logger.Warn("linked item ownership mismatch, skipping job",
				"item_id", args.ItemID,
				"user_id", args.UserID,
			)
			// Don't retry if user doesn't own the item
			return nil
		}
		return fmt.Errorf("failed to acquire lock on linked item: %w", err)
	}
	// Ensure transaction is always cleaned up (commit on success, rollback on error)
	defer func() {
		if tx != nil {
			_ = tx.Rollback(ctx) // No-op if already committed
		}
	}()

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
	observability.PlaidSyncTxns.WithLabelValues("added").Add(float64(len(result.Added)))
	observability.PlaidSyncTxns.WithLabelValues("modified").Add(float64(len(result.Modified)))
	observability.PlaidSyncTxns.WithLabelValues("removed").Add(float64(len(result.Removed)))
	w.logger.Info("sync completed successfully",
		"job_id", job.ID,
		"item_id", args.ItemID,
		"added", len(result.Added),
		"modified", len(result.Modified),
		"removed", len(result.Removed),
		"total_changes", totalChanges,
		"duration_ms", time.Since(startTime).Milliseconds(),
	)

	// Commit the transaction to release the lock (if we're using locking)
	// This must happen before enqueueing follow-up jobs to avoid holding the lock
	if tx != nil {
		if err := tx.Commit(ctx); err != nil {
			return fmt.Errorf("failed to commit transaction: %w", err)
		}
	}

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

// errOwnershipMismatch is returned when the linked item's user ID doesn't match the job's user ID.
var errOwnershipMismatch = errors.New("linked item ownership mismatch")

// acquireLockAndGetItem starts a transaction, acquires an exclusive row-level lock
// on the linked item, and validates ownership.
//
// The returned transaction MUST be committed or rolled back by the caller.
// The lock is held until the transaction ends.
//
// Returns:
//   - The linked item if successful
//   - The transaction (caller must commit/rollback)
//   - repository.ErrLinkedItemNotFound if the item doesn't exist
//   - errOwnershipMismatch if the item belongs to a different user
//   - Other errors for database failures
func (w *PlaidSyncWorker) acquireLockAndGetItem(ctx context.Context, itemID, userID string) (*domain.LinkedItem, pgx.Tx, error) {
	// If DB is not configured, fall back to non-locking behavior
	// This allows graceful degradation in tests or when locking is not needed
	if w.db == nil {
		item, err := w.linkedItemRepo.GetByIDAndUser(ctx, itemID, userID)
		return item, nil, err
	}

	// Start transaction for row-level locking
	tx, err := w.db.Begin(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to begin transaction: %w", err)
	}

	// Acquire exclusive lock and get the item
	item, err := w.linkedItemRepo.GetByIDForUpdate(ctx, tx, itemID)
	if err != nil {
		_ = tx.Rollback(ctx)
		return nil, nil, err
	}

	// Validate ownership - the item must belong to the specified user
	if item.UserID != userID {
		_ = tx.Rollback(ctx)
		return nil, nil, errOwnershipMismatch
	}

	w.logger.Debug("acquired lock on linked item",
		"item_id", itemID,
		"user_id", userID,
	)

	return item, tx, nil
}
