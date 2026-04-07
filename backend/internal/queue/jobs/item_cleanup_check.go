// Package jobs contains River job definitions and workers for async processing.
package jobs

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/observability"
	"github.com/maxnussbaum/BoostedBudget/internal/queue"
	"github.com/riverqueue/river"
)

// ItemCleanupCheckArgs contains the arguments for the item cleanup check job.
// This job runs periodically to find Plaid items that need cleanup (orphaned,
// in error state for too long, or stale) and enqueues individual ItemCleanup
// jobs for each.
//
// Issue #668: Plaid Item Management for Cost Optimization
type ItemCleanupCheckArgs struct {
	// TriggerSource indicates what triggered this check (e.g., "periodic", "manual")
	TriggerSource string `json:"trigger_source,omitempty"`
}

// Kind returns the unique identifier for this job type.
func (ItemCleanupCheckArgs) Kind() string { return "item_cleanup_check" }

// LinkedItemCleanupRepository defines the methods needed for item cleanup scheduling.
type LinkedItemCleanupRepository interface {
	// GetItemsForCleanup returns items in error state > errorDays, OR revoked, OR not synced > staleDays, OR zero accounts
	GetItemsForCleanup(ctx context.Context, errorDays, staleDays int) ([]domain.LinkedItem, error)
	// GetItemsPendingCleanup returns items where cleanup_scheduled_at < NOW()
	GetItemsPendingCleanup(ctx context.Context) ([]domain.LinkedItem, error)
	// MarkForCleanup marks an item for cleanup with the scheduled cleanup time
	MarkForCleanup(ctx context.Context, itemID string, cleanupAt time.Time) error
	// ClearCleanupSchedule clears the cleanup schedule (e.g., if user reconnected)
	ClearCleanupSchedule(ctx context.Context, itemID string) error
	// Delete hard-deletes the linked_items row after Plaid billing is stopped.
	// Safe no-op: skips deletion if accounts (any status) still reference the item,
	// preventing cascade-deletion of historical 'replaced' accounts and their balance_snapshots.
	Delete(ctx context.Context, id string) error
}

// AccountCountRepository defines methods needed to count accounts for cleanup decisions.
// Issue #668: Zero-account item detection
type AccountCountRepository interface {
	// CountActiveByLinkedItemID counts active accounts for a linked item.
	CountActiveByLinkedItemID(ctx context.Context, linkedItemID string) (int, error)
}

// ItemCleanupCheckWorker processes item cleanup check jobs.
// This worker is the coordinator that finds items ready for cleanup and
// enqueues individual cleanup jobs for each.
//
// Cleanup criteria:
// - Status 'error' or 'login_required' for > ErrorDaysThreshold days
// - Status 'revoked' (any duration)
// - Last sync > StaleDaysThreshold days ago
// - Items with zero active accounts (immediate cleanup, no grace period)
//
// Items are given a grace period before actual cleanup to allow users
// to reconnect if needed, EXCEPT for zero-account items which are cleaned up immediately.
type ItemCleanupCheckWorker struct {
	river.WorkerDefaults[ItemCleanupCheckArgs]

	linkedItemRepo     LinkedItemCleanupRepository
	accountRepo        AccountCountRepository
	jobQueue           queue.JobQueue
	logger             *slog.Logger
	gracePeriodDays    int
	errorDaysThreshold int
	staleDaysThreshold int
}

// ItemCleanupCheckWorkerDeps contains the dependencies for ItemCleanupCheckWorker.
type ItemCleanupCheckWorkerDeps struct {
	LinkedItemRepo     LinkedItemCleanupRepository
	AccountRepo        AccountCountRepository // Optional: for zero-account detection
	JobQueue           queue.JobQueue
	Logger             *slog.Logger
	GracePeriodDays    int // Days to wait before cleanup (default 7)
	ErrorDaysThreshold int // Days in error state before cleanup candidate (default 30)
	StaleDaysThreshold int // Days since last sync before cleanup candidate (default 60)
}

// NewItemCleanupCheckWorker creates a new item cleanup check worker.
// Note: JobQueue can be nil initially and set later via SetJobQueue() to handle
// circular dependency when configuring periodic jobs.
func NewItemCleanupCheckWorker(deps ItemCleanupCheckWorkerDeps) *ItemCleanupCheckWorker {
	logger := deps.Logger
	if logger == nil {
		logger = slog.Default()
	}

	// Set defaults
	gracePeriodDays := deps.GracePeriodDays
	if gracePeriodDays <= 0 {
		gracePeriodDays = 7
	}
	errorDaysThreshold := deps.ErrorDaysThreshold
	if errorDaysThreshold <= 0 {
		errorDaysThreshold = 30
	}
	staleDaysThreshold := deps.StaleDaysThreshold
	if staleDaysThreshold <= 0 {
		staleDaysThreshold = 60
	}

	return &ItemCleanupCheckWorker{
		linkedItemRepo:     deps.LinkedItemRepo,
		accountRepo:        deps.AccountRepo,
		jobQueue:           deps.JobQueue,
		logger:             logger,
		gracePeriodDays:    gracePeriodDays,
		errorDaysThreshold: errorDaysThreshold,
		staleDaysThreshold: staleDaysThreshold,
	}
}

// SetJobQueue sets the job queue after worker creation.
// This is needed because of circular dependency: worker must be registered
// before client creation, but needs client to enqueue jobs.
func (w *ItemCleanupCheckWorker) SetJobQueue(jq queue.JobQueue) {
	w.jobQueue = jq
}

// Work processes an item cleanup check job.
//
// The worker:
//  1. Fetches items that are cleanup candidates (error, revoked, or stale)
//  2. Skips items already scheduled for cleanup
//  3. Marks each item for cleanup (sets cleanup_scheduled_at)
//  4. Enqueues an ItemCleanup job for each item, scheduled for after the grace period
//
// Errors in individual item processing don't fail the entire job.
func (w *ItemCleanupCheckWorker) Work(ctx context.Context, job *river.Job[ItemCleanupCheckArgs]) (err error) {
	args := job.Args

	w.logger.Info("starting item cleanup check",
		"job_id", job.ID,
		"trigger_source", args.TriggerSource,
		"attempt", job.Attempt,
		"error_days_threshold", w.errorDaysThreshold,
		"stale_days_threshold", w.staleDaysThreshold,
		"grace_period_days", w.gracePeriodDays,
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

	// Get items that need cleanup
	items, err := w.linkedItemRepo.GetItemsForCleanup(ctx, w.errorDaysThreshold, w.staleDaysThreshold)
	if err != nil {
		w.logger.Error("failed to get cleanup candidates",
			"job_id", job.ID,
			"error", err,
		)
		return fmt.Errorf("failed to get cleanup candidates: %w", err)
	}

	if len(items) == 0 {
		w.logger.Info("no items need cleanup",
			"job_id", job.ID,
			"duration_ms", time.Since(startTime).Milliseconds(),
		)
		return nil
	}

	w.logger.Info("found cleanup candidates",
		"job_id", job.ID,
		"item_count", len(items),
	)

	now := time.Now()
	defaultCleanupAt := now.Add(time.Duration(w.gracePeriodDays) * 24 * time.Hour)

	// Process each item
	var scheduledCount, skippedCount, errorCount int
	for _, item := range items {
		// Skip items already scheduled for cleanup
		if item.CleanupScheduledAt != nil {
			w.logger.Debug("item already scheduled for cleanup, skipping",
				"item_id", item.ID,
				"cleanup_scheduled_at", item.CleanupScheduledAt,
			)
			skippedCount++
			continue
		}

		// Determine cleanup reason and whether to use immediate cleanup
		reason, isImmediate := w.determineCleanupReasonAndTiming(ctx, item)

		// Use immediate or grace period based on reason
		cleanupAt := defaultCleanupAt
		if isImmediate {
			cleanupAt = now
		}

		// Mark item for cleanup
		if markErr := w.linkedItemRepo.MarkForCleanup(ctx, item.ID, cleanupAt); markErr != nil {
			w.logger.Error("failed to mark item for cleanup",
				"job_id", job.ID,
				"item_id", item.ID,
				"error", markErr,
			)
			errorCount++
			continue
		}

		// Enqueue cleanup job scheduled for cleanup time
		cleanupArgs := ItemCleanupArgs{
			LinkedItemID: item.ID,
			UserID:       item.UserID,
			Reason:       reason,
		}

		uniqueOpts := ItemCleanupUniqueOpts()
		result, enqueueErr := w.jobQueue.Enqueue(ctx, cleanupArgs, queue.WithUnique(*uniqueOpts), queue.WithScheduledAt(cleanupAt))
		if enqueueErr != nil {
			w.logger.Error("failed to enqueue item cleanup job",
				"job_id", job.ID,
				"item_id", item.ID,
				"error", enqueueErr,
			)
			errorCount++
			continue
		}

		if result.UniqueSkipped {
			w.logger.Debug("item cleanup job already queued, skipping",
				"item_id", item.ID,
			)
			skippedCount++
		} else {
			w.logger.Info("scheduled item cleanup",
				"item_id", item.ID,
				"user_id", item.UserID,
				"reason", reason,
				"cleanup_at", cleanupAt,
				"cleanup_job_id", result.ID,
				"immediate", isImmediate,
			)
			scheduledCount++
		}
	}

	w.logger.Info("item cleanup check completed",
		"job_id", job.ID,
		"total_items", len(items),
		"scheduled", scheduledCount,
		"skipped", skippedCount,
		"errors", errorCount,
		"duration_ms", time.Since(startTime).Milliseconds(),
	)

	// Don't fail the job if some items failed - they'll be retried next check
	return nil
}

// determineCleanupReasonAndTiming determines the reason for cleaning up an item
// and whether it should be cleaned up immediately (no grace period).
// Returns (reason, isImmediate).
// Issue #668: Zero-account items are cleaned up immediately.
func (w *ItemCleanupCheckWorker) determineCleanupReasonAndTiming(ctx context.Context, item domain.LinkedItem) (string, bool) {
	// Check for zero accounts first (highest priority - immediate cleanup)
	if w.accountRepo != nil {
		count, err := w.accountRepo.CountActiveByLinkedItemID(ctx, item.ID)
		if err != nil {
			w.logger.Warn("failed to count accounts for item",
				"item_id", item.ID,
				"error", err,
			)
			// Fall through to status-based reason on error
		} else if count == 0 {
			// Zero accounts - immediate cleanup
			return "zero_accounts", true
		}
	}

	// Status-based reasons (with grace period)
	switch item.Status {
	case "revoked":
		return "revoked", false
	case "error", "login_required":
		return "long_term_error", false
	default:
		return "stale", false
	}
}

// ItemCleanupCheckUniqueOpts returns the recommended uniqueness options.
// Only one check job should run at a time to prevent duplicate cleanup processing.
func ItemCleanupCheckUniqueOpts() *queue.UniqueOpts {
	return &queue.UniqueOpts{
		ByArgs: false, // All check jobs are equivalent
		// 24 hour period - matches daily run frequency
		ByPeriod: 24 * time.Hour,
		ByState: []queue.JobState{
			queue.JobStateAvailable,
			queue.JobStateScheduled,
			queue.JobStateRunning,
			queue.JobStateRetryable,
			queue.JobStatePending,
		},
	}
}
