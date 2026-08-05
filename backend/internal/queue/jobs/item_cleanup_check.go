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
	"github.com/maxnussbaum/BoostedBudget/internal/repository"
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

// itemCleanupCheckPageSize bounds each GetItemsForCleanupPage query (Issue #1469). Work() loops
// pages of this size within a single run, so this bounds only how many rows one query call
// materialises — NOT how many items one run schedules for Plaid disconnection. That total is bounded
// separately by maxItemsPerCleanupSweep below. 500 matches scheduledDeletionCheckPageSize and
// expiredRetentionPageSize; a page's cost is a row fetch either way.
const itemCleanupCheckPageSize = 500

// maxItemsPerCleanupSweep bounds the TOTAL number of items Work() marks for cleanup and enqueues
// across every page fetched within ONE run.
//
// ⚠ FIRST, the correction that matters most, because this comment's first version got it wrong and
// the wrong version was the whole basis for choosing 10,000: **this sweep's per-row action is NOT
// uniformly deferred, and NOT reversible.** Three verified facts, each of which contradicts a claim
// the earlier draft made:
//
//   - determineCleanupReasonAndTiming returns ("zero_accounts", true) for the zero-active-accounts
//     leg, and isImmediate sets cleanupAt = now (see the enqueue loop in Work). **That leg gets no
//     grace period whatsoever** — its ItemCleanup job is scheduled for immediate execution.
//   - ItemCleanupWorker.Work (item_cleanup.go) calls plaidCleanupSvc.RemoveItem straight away. It
//     performs **no** recheck that cleanup_scheduled_at still stands and **no** reconnection check,
//     despite its own doc comment claiming it "Validates the item still needs cleanup (grace period
//     expired, not reconnected)" and that cleanup "is skipped" if the user reconnected. That
//     validation does not exist in the body.
//   - The only production ClearCleanupSchedule call is in item_cleanup.go, AFTER a successful
//     RemoveItem. It is post-hoc state cleanup, **not** a cancellation hook, and nothing calls it on
//     reconnection.
//
// So for the zero-accounts leg there is no window, no cancellation path, and no recheck: a
// misclassification (account replacement in flight, a concurrent account create, an accounts row
// briefly non-'active') schedules an immediate, irreversible Plaid /item/remove. The
// error/login_required/stale legs DO carry gracePeriodDays (7 by default) before their job becomes
// eligible, but even they get no recheck when it fires. Tracked as its own issue — the fix belongs in
// ItemCleanupWorker, not in this cap.
//
// GIVEN that, 10,000 is justified as a deliberately CONSERVATIVE blast-radius bound, not as a
// "reversible work is cheap" allowance:
//
//   - It is not defended on reversibility at all. Treat every one of the 10,000 as potentially
//     irreversible-on-the-same-tick, because for the zero-accounts leg it is.
//   - It is 1/5 of maxUsersPerDeletionSweep (50,000) on cost grounds, which is the one comparison
//     that does hold: that sweep's loop body is a single River enqueue, while this one performs up to
//     three round-trips per row (CountActiveByLinkedItemID via
//     determineCleanupReasonAndTiming, MarkForCleanup, and the enqueue), so the same time budget
//     covers proportionally fewer rows.
//   - It is above maxUsersPerRetentionSweep (1,000) only because the per-row loss is narrower, NOT
//     because it is undoable: losing a Plaid item costs the user a re-link through Plaid Link and a
//     re-sync; losing a retention candidate destroys the account. Both are irreversible; they are not
//     equally bad.
//   - Population sanity: linked_items holds roughly one row per user per linked institution, so
//     10,000 candidate items is far above any plausible steady-state volume and the cap does not
//     throttle correct operation. At itemCleanupCheckPageSize=500 that is 20 page round-trips.
//
// **This number should be treated as open, not settled.** It was originally chosen on a
// reversibility argument that turned out to be false for one leg, and the brief that requested it
// suggested the cap "can be more generous" on the same false premise. If the ItemCleanupWorker
// recheck gap is closed (making the deferred legs genuinely cancellable), raising it is defensible;
// until then, lowering it is the safer direction. Do not raise it citing the grace period.
//
// Hitting the cap does NOT reschedule or retry anything: it stops THIS run early and logs a WARN
// naming how many items were processed. The periodic registration re-runs the job on its own
// cadence, and that run restarts pagination from a nil cursor (there is no persisted cross-run
// cursor — #1477). See GetItemsForCleanupPage's doc comment (repository/linked_item_repository.go)
// for what that does and does NOT guarantee: below this cap, an oldest row that cannot be marked
// cannot prevent later rows from being reached; at or above it, every later run re-spends its whole
// budget on the same unmarkable rows and never reaches anything past the cap. Do not restate that as
// "no starvation".
const maxItemsPerCleanupSweep = 10000

// LinkedItemCleanupRepository defines the methods needed for item cleanup scheduling.
type LinkedItemCleanupRepository interface {
	// GetItemsForCleanupPage returns up to `limit` items in error state > errorDays, OR revoked, OR
	// not synced > staleDays, OR with zero active accounts — ordered deterministically for keyset
	// pagination (Issue #1469). after == nil starts from the beginning; pass the cursor derived from
	// the last row of the previous page to continue.
	//
	// This replaces the unbounded GetItemsForCleanup(ctx, errorDays, staleDays) on this interface.
	// It is declared HERE and on the concrete repository only — deliberately NOT on the wide
	// repository.LinkedItemRepository interface, which several test files' mocks implement. See
	// repository.linkedItemRepository.GetItemsForCleanupPage's doc comment for why a bare LIMIT is
	// not sufficient (both pre-existing sort columns are nullable, so the li.id tie-breaker is what
	// makes the order total) and for the precise, cap-bounded statement of what the keyset cursor
	// does and does not guarantee.
	GetItemsForCleanupPage(ctx context.Context, errorDays, staleDays int, after *repository.ItemCleanupSweepCursor, limit int) ([]domain.LinkedItem, error)
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
	// cleanupPageSize / maxItemsPerSweep bound Work()'s candidate sweep (Issue #1469). See the
	// itemCleanupCheckPageSize / maxItemsPerCleanupSweep constants for the rationale and derivation.
	cleanupPageSize  int
	maxItemsPerSweep int
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
	// CleanupPageSize overrides itemCleanupCheckPageSize (Issue #1469). Zero/unset uses the
	// production default. Exists so tests can exercise the multi-page keyset-cursor loop without
	// seeding hundreds of fixture items — production should never set this.
	CleanupPageSize int
	// MaxItemsPerCleanupSweep overrides maxItemsPerCleanupSweep (Issue #1469). Zero/unset uses the
	// production default. Exists so tests can exercise the total-processed safety cap without
	// seeding thousands of fixture items — production should never set this.
	MaxItemsPerCleanupSweep int
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

	cleanupPageSize := deps.CleanupPageSize
	if cleanupPageSize <= 0 {
		cleanupPageSize = itemCleanupCheckPageSize
	}
	maxItemsPerSweep := deps.MaxItemsPerCleanupSweep
	if maxItemsPerSweep <= 0 {
		maxItemsPerSweep = maxItemsPerCleanupSweep
	}

	return &ItemCleanupCheckWorker{
		linkedItemRepo:     deps.LinkedItemRepo,
		accountRepo:        deps.AccountRepo,
		jobQueue:           deps.JobQueue,
		logger:             logger,
		gracePeriodDays:    gracePeriodDays,
		errorDaysThreshold: errorDaysThreshold,
		staleDaysThreshold: staleDaysThreshold,
		cleanupPageSize:    cleanupPageSize,
		maxItemsPerSweep:   maxItemsPerSweep,
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
		} else {
			observability.RecordJobSuccess(job.Kind) // Issue #1413 P4: last-success heartbeat
		}
		observability.JobsCompleted.WithLabelValues(job.Kind, job.Queue, status).Inc()
		observability.JobDuration.WithLabelValues(job.Kind).Observe(time.Since(start).Seconds())
	}()

	startTime := time.Now()

	now := time.Now()
	defaultCleanupAt := now.Add(time.Duration(w.gracePeriodDays) * 24 * time.Hour)

	var cursor *repository.ItemCleanupSweepCursor
	var totalItems, scheduledCount, skippedCount, errorCount int

	for {
		items, pageErr := w.linkedItemRepo.GetItemsForCleanupPage(ctx, w.errorDaysThreshold, w.staleDaysThreshold, cursor, w.cleanupPageSize)
		if pageErr != nil {
			w.logger.Error("failed to get cleanup candidates",
				"job_id", job.ID,
				"error", pageErr,
			)
			return fmt.Errorf("failed to get cleanup candidates: %w", pageErr)
		}

		if len(items) == 0 {
			break
		}

		// fetchedCount is the page length AS RETURNED by the repository, before the cap truncation
		// below — the only reliable signal for whether the repository itself is exhausted
		// (fetchedCount < pageSize means it had nothing left to give, regardless of the cap).
		fetchedCount := len(items)

		// maxItemsPerCleanupSweep: truncate the page now if it would push totalItems to or past the
		// cap, so the loop below never schedules more than the cap allows — "at most
		// maxItemsPerCleanupSweep scheduled", not "cap-or-more".
		if remaining := w.maxItemsPerSweep - totalItems; fetchedCount >= remaining {
			// remaining <= 0 is unreachable given the break at the bottom of this loop, but
			// items[:remaining] would panic on a negative length, so it is guarded explicitly.
			if remaining <= 0 {
				break
			}
			items = items[:remaining]
		}

		totalItems += len(items)
		w.logger.Info("found cleanup candidates",
			"job_id", job.ID,
			"item_count", len(items),
		)

		lastItem := items[len(items)-1]

		// Process each item in this page
		for _, item := range items {
			// Skip items already scheduled for cleanup. GetItemsForCleanupPage's predicate requires
			// cleanup_scheduled_at IS NULL, so this is defensive against a test double or a future
			// predicate change, not an expected production path.
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

		// The cursor advances past every row this page yielded AFTER the cap truncation above — i.e.
		// past the last row actually processed, not necessarily the last row the repository returned.
		// Those are the same row except on a page the cap truncated, and on that page the loop always
		// breaks below, so the difference is never observable. What matters is that it advances past
		// rows the loop SKIPPED or FAILED to mark: that is the point of a keyset cursor here — a row
		// that is a candidate by the predicate but cannot actually be marked must not occupy the
		// oldest slot of every subsequent page within this run.
		cursor = &repository.ItemCleanupSweepCursor{
			LastErrorAt:  lastItem.LastErrorAt,
			LastSyncedAt: lastItem.LastSyncedAt,
			ItemID:       lastItem.ID,
		}

		// exhausted is computed from fetchedCount (pre-truncation) so it reflects what the REPOSITORY
		// said, even on a page that also happens to trip the cap.
		exhausted := fetchedCount < w.cleanupPageSize

		// Cap hit: stop THIS run — but only WARN when the repository had not already told us it was
		// exhausted. A genuinely short final page that happens to land exactly on the cap has no
		// remainder to defer, and warning about one would be a false alarm.
		if totalItems >= w.maxItemsPerSweep && !exhausted {
			w.logger.Warn("item cleanup sweep hit its total-processed safety cap; deferring remainder to the next periodic run",
				"job_id", job.ID, "processed", totalItems, "cap", w.maxItemsPerSweep)
			break
		}

		if exhausted {
			break
		}
	}

	if totalItems == 0 {
		w.logger.Info("no items need cleanup",
			"job_id", job.ID,
			"duration_ms", time.Since(startTime).Milliseconds(),
		)
		return nil
	}

	w.logger.Info("item cleanup check completed",
		"job_id", job.ID,
		"total_items", totalItems,
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
			// Completed is REQUIRED because this job is registered with RunOnStart: true
			// (Issue #1448). Without it, a run that COMPLETED minutes ago does not
			// deduplicate the start-of-process catch-up insert, so every deploy or leader
			// election enqueues a redundant duplicate. Matches the two jobs that already
			// had RunOnStart: true before #1448 (RetentionCleanupCheckUniqueOpts,
			// ManualTierOverrideExpiryCheckUniqueOpts). Found by the final branch-wide
			// Codex review.
			queue.JobStateCompleted,
		},
	}
}

// ItemCleanupCheckInsertOpts returns the *river.InsertOpts this job's periodic registration
// in main.go must use — Queue: "low" plus the uniqueness options from
// ItemCleanupCheckUniqueOpts(), converted to river's UniqueOpts type.
//
// Issue #1448: ItemCleanupCheckUniqueOpts() existed but was never called by main.go's
// periodic registration.
func ItemCleanupCheckInsertOpts() *river.InsertOpts {
	return riverInsertOptsFromUnique(queue.QueueLow, ItemCleanupCheckUniqueOpts())
}

// ItemCleanupCheckPeriodicOpts returns the *river.PeriodicJobOpts this job's periodic
// registration in main.go must use. RunOnStart: true is only safe because
// ItemCleanupCheckInsertOpts() above always carries ItemCleanupCheckUniqueOpts()'s dedup —
// previously this job used RunOnStart: false, meaning it could be enqueued zero times ever
// on a deploy cadence faster than its interval (Issue #1448). ID is set so River restores
// nextRunAt across restarts/leader elections instead of recomputing now+interval every time.
func ItemCleanupCheckPeriodicOpts() *river.PeriodicJobOpts {
	return &river.PeriodicJobOpts{RunOnStart: true, ID: ItemCleanupCheckArgs{}.Kind()}
}
