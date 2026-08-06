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

// defaultCleanupErrorDays / defaultCleanupStaleDays are the item-cleanup eligibility thresholds
// shared by BOTH halves of the flow: the sweep (ItemCleanupCheckWorker) that decides an item is a
// candidate and schedules it, and the per-item eligibility recheck (ItemCleanupWorker →
// ClaimItemForCleanup) that re-verifies it just before the irreversible Plaid /item/remove.
//
// Issue #1486: they are constants shared by both constructors, rather than two independently
// defaulted fields, because a mismatch is silent and one-directional. A recheck running a STRICTER
// threshold than the sweep would deny claims the sweep legitimately created — every affected item
// would keep its cleanup_scheduled_at (so the sweep, which requires IS NULL, never re-selects it)
// and keep billing, with nothing in the logs but a routine "no longer a candidate" skip.
// TestItemCleanupWorkers_ShareEligibilityThresholdDefaults pins the pairing.
const (
	defaultCleanupErrorDays = 30
	defaultCleanupStaleDays = 60
)

// maxItemsPerCleanupSweep bounds the TOTAL number of items Work() marks for cleanup and enqueues
// across every page fetched within ONE run.
//
// ⚠ FIRST, the correction that matters most, because this comment's first version got it wrong and
// the wrong version was the whole basis for the 10,000 this cap used to be: **its per-row action is NOT
// uniformly deferred, and NOT reversible.** Three verified facts, each of which contradicts a claim
// the earlier draft made:
//
//   - determineCleanupReasonAndTiming returns ("zero_accounts", true) for the zero-active-accounts
//     leg, and isImmediate sets cleanupAt = now (see the enqueue loop in Work). **That leg gets no
//     grace period whatsoever** — its ItemCleanup job is scheduled for immediate execution.
//   - ItemCleanupWorker.Work (item_cleanup.go) used to call plaidCleanupSvc.RemoveItem straight
//     away, with **no** recheck of any kind, despite its own doc comment claiming it "Validates the
//     item still needs cleanup (grace period expired, not reconnected)". **Issue #1486 fixed that**:
//     Work now calls ClaimItemForCleanup first and skips the Plaid call when the item is no longer a
//     candidate. What that does NOT change is this cap's derivation — see the ⚠ note below the
//     bullet list.
//   - There are FOUR production ClearCleanupSchedule calls, not two (Issue #1486 follow-up part E corrected
//     this count; see the enumeration in ItemCleanupWorker.Work's #1486 comment, and
//     TestClearCleanupScheduleProductionCallers_MatchTheDocumentedSet, which fails if it drifts
//     again). What matters for THIS cap is unchanged and still true: **none of the four is a
//     cancellation hook** — each releases a stamp on a path that has just decided not to act, and in
//     particular **nothing clears cleanup_scheduled_at on reconnection**, which is precisely why
//     #1486's recheck had to re-evaluate ELIGIBILITY rather than re-read the schedule.
//
// So for the zero-accounts leg there is still no grace window and no external cancellation path — a
// misclassification (account replacement in flight, a concurrent account create, an accounts row
// briefly non-'active') still schedules an IMMEDIATE Plaid /item/remove. What #1486 added is a
// recheck at execution time, which closes the transient-misclassification case for every leg whose
// cause has since cleared, but cannot help a row that is genuinely, persistently a candidate.
//
// GIVEN that, the cap is derived from the ASYMMETRY between its two error directions, which is the
// only ground left once reversibility is off the table. The two directions are not comparable:
//
//   - Processing too MANY items in one run is IRREVERSIBLE. Every row over the true figure is a Plaid
//     /item/remove the user did not need, undoable only by re-linking the institution through Plaid
//     Link and re-syncing. For the zero-accounts leg there is not even a window in which to notice.
//   - Processing too FEW is REVERSIBLE and costs only money. A candidate that misses this run is
//     picked up by the next one; in the meantime Plaid keeps billing for an item nobody is using.
//     There is no user-facing harm at all in the interim, because every candidate is by definition
//     already in a state that yields no data: error, login_required, revoked, stale past
//     staleDaysThreshold, or holding zero active accounts.
//
// An irreversible cost on one side and a metered, recoverable cost on the other means the cap should
// be set LOW and allowed to defer, not set high to "keep up". Hence 1,000 — the same value as
// maxUsersPerRetentionSweep, deliberately, so there is one blast-radius number to reason about across
// both irreversible sweeps rather than two that invite comparison. Concretely:
//
//   - Drain arithmetic bounds the downside precisely. At 1,000 per run on this job's ~daily cadence
//     (ItemCleanupCheckUniqueOpts, 24h ByPeriod), a backlog of B items clears in about B/1000 runs.
//     Even an implausible 50,000-item backlog drains in ~50 runs while costing nothing but Plaid item
//     fees, and the cap-hit WARN below names the overflow on every single run, so the backlog is
//     visible for its whole life rather than silent.
//   - It is NOT set to 1,000 because the per-row loss equals retention's — it does not. Destroying an
//     account is worse than dropping a bank connection. It is set there because both are irreversible,
//     and when the recoverable direction is this cheap there is no reason to buy throughput with
//     irreversible risk on either sweep.
//   - The previous value was 10,000, justified on a reversibility argument (a 7-day grace window and
//     ClearCleanupSchedule as a cancellation hook) that the facts above disprove for the immediate
//     leg. Do not restore 10,000, and do not raise this citing a grace period or a cancellation
//     path: the zero-accounts leg still gets neither.
//   - At itemCleanupCheckPageSize=500 the cap is 2 page round-trips, so it is reached on a page
//     boundary rather than by mid-page truncation in the common case.
//
// ⚠ WHAT ISSUE #1486 DID AND DID NOT CHANGE ABOUT THIS CAP. The earlier version of this comment
// named the missing execution-time recheck as the PRECONDITION for raising the cap. That
// precondition is now satisfied (ItemCleanupWorker.Work → ClaimItemForCleanup), so the sentence has
// been removed rather than left standing as a prohibition whose stated reason no longer holds. The
// cap is nonetheless LEFT AT 1,000, on narrower and now-accurate grounds:
//
//   - The recheck removes the STALE-decision risk (an item that stopped being a candidate between
//     being scheduled and its job running), which is the risk a large batch multiplies. It does not
//     make the action reversible: every item that IS still a candidate is still irreversibly
//     removed, so a run's blast radius is still "up to N irreversible Plaid removals", just
//     N accurate ones.
//   - The remaining reasons not to raise it are therefore (a) the accuracy of the PREDICATE itself,
//     which the recheck re-evaluates but does not validate — a predicate that is wrong is wrong
//     twice, at schedule time and at claim time — and (b) throughput/round-trip cost, which nothing
//     currently pressures: there are no production users, so no backlog exists to drain.
//   - Raising it is now a legitimate change to propose on throughput evidence (a real backlog, the
//     cap-hit WARN firing run after run) plus the drain arithmetic above. It is no longer blocked on
//     a missing safety property. It must NOT be raised on the argument that the recheck makes
//     cleanup reversible — it does not.
//
// Hitting the cap does NOT reschedule or retry anything: it stops THIS run early and logs a WARN
// naming how many items were processed. The periodic registration re-runs the job on its own
// cadence, and that run restarts pagination from a nil cursor (there is no persisted cross-run
// cursor — #1477). See GetItemsForCleanupPage's doc comment (repository/linked_item_repository.go)
// for what that does and does NOT guarantee: below this cap, an oldest row that cannot be marked
// cannot prevent later rows from being reached; at or above it, every later run re-spends its whole
// budget on the same unmarkable rows and never reaches anything past the cap. Do not restate that as
// "no starvation".
const maxItemsPerCleanupSweep = 1000

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
	// GetItemsPendingCleanup returns stamped, overdue items — cleanup_scheduled_at IS NOT NULL AND <
	// NOW() — excluding 'disconnected' ONLY. This is reenqueueStrandedCleanups's selection query. It
	// deliberately still returns 'replaced' items, unlike GetItemsForCleanupPage above: a 'replaced'
	// item's Plaid Item is still live and still billing (the re-link path never calls /item/remove),
	// and the sweep can never re-select it, so this is its only recovery path. See
	// repository.linkedItemRepository.GetItemsPendingCleanup's doc comment.
	GetItemsPendingCleanup(ctx context.Context) ([]domain.LinkedItem, error)
	// MarkForCleanup marks an item for cleanup with the scheduled cleanup time
	MarkForCleanup(ctx context.Context, itemID string, cleanupAt time.Time) error
	// ClaimItemForCleanup re-verifies, in one statement, that a single item is STILL a cleanup
	// candidate and reports whether the caller may proceed with the irreversible Plaid
	// /item/remove. Issue #1486: ItemCleanupWorker.Work used to call RemoveItem with no check of
	// any kind, however long the job had been queued behind its grace period.
	//
	// (false, nil) is a LEGITIMATE outcome, not an error — the user reconnected, accounts came
	// back, or the row is gone. A non-nil error must be treated as "cannot tell", which is NOT
	// "still eligible": fail closed and let the job retry.
	//
	// Declared HERE and on the concrete repository only — deliberately NOT on the wide
	// repository.LinkedItemRepository, whose mocks live in several test files. Same placement, and
	// for the same reason, as GetItemsForCleanupPage above. See
	// repository.linkedItemRepository.ClaimItemForCleanup and the
	// cleanupCandidateReasonPredicate constant it splices for why the recheck re-evaluates
	// ELIGIBILITY rather than merely re-reading cleanup_scheduled_at (nothing clears that on
	// reconnect), and for the two sweep-only conditions it deliberately does not reuse.
	ClaimItemForCleanup(ctx context.Context, itemID string, errorDays, staleDays int) (bool, error)
	// ClearCleanupSchedule clears the cleanup schedule. NOT called on reconnection — see the note on
	// ClaimItemForCleanup above.
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

// intOrDefault returns value if positive, otherwise defaultValue.
// Used to apply sensible defaults to optional dependency injection fields.
func intOrDefault(value, defaultValue int) int {
	if value <= 0 {
		return defaultValue
	}
	return value
}

// NewItemCleanupCheckWorker creates a new item cleanup check worker.
// Note: JobQueue can be nil initially and set later via SetJobQueue() to handle
// circular dependency when configuring periodic jobs.
func NewItemCleanupCheckWorker(deps ItemCleanupCheckWorkerDeps) *ItemCleanupCheckWorker {
	logger := deps.Logger
	if logger == nil {
		logger = slog.Default()
	}

	return &ItemCleanupCheckWorker{
		linkedItemRepo:     deps.LinkedItemRepo,
		accountRepo:        deps.AccountRepo,
		jobQueue:           deps.JobQueue,
		logger:             logger,
		gracePeriodDays:    intOrDefault(deps.GracePeriodDays, 7),
		errorDaysThreshold: intOrDefault(deps.ErrorDaysThreshold, defaultCleanupErrorDays),
		staleDaysThreshold: intOrDefault(deps.StaleDaysThreshold, defaultCleanupStaleDays),
		cleanupPageSize:    intOrDefault(deps.CleanupPageSize, itemCleanupCheckPageSize),
		maxItemsPerSweep:   intOrDefault(deps.MaxItemsPerCleanupSweep, maxItemsPerCleanupSweep),
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
				w.logger.Error("failed to enqueue item cleanup job, releasing the cleanup stamp",
					"job_id", job.ID,
					"item_id", item.ID,
					"error", enqueueErr,
				)
				errorCount++

				// Issue #1486 follow-up part D: release the stamp MarkForCleanup just set. This is a THIRD
				// instance of the class commit 24907762 fixed — whose message claims it covered "two
				// paths"; there were three, and this was the one it missed.
				//
				// cleanup_scheduled_at is a DURABLE marker that HIDES a row from the sweep
				// (cleanupCandidateSelectQuery requires cleanup_scheduled_at IS NULL), so a stamp left
				// set with no job in flight is not a harmless leftover: River being briefly unavailable
				// would remove a still-billing Plaid item from every future sweep permanently, and
				// DeleteUserCascade's C2 guard would block that user's GDPR erasure forever. Before the
				// stamp existed this path left the row untouched and the next daily sweep recovered it,
				// so keeping it here made a transient queue error strictly WORSE than the old
				// behaviour.
				//
				// Safe from the race the stamp guards against, for exactly the reason the identical
				// release in accountService.DeleteAccount gives: that race is with a worker EXECUTING
				// the job, and the enqueue that would have created that job just failed, so no worker
				// can be mid-flight.
				if clearErr := w.linkedItemRepo.ClearCleanupSchedule(ctx, item.ID); clearErr != nil {
					// Both writes failed, so the row now keeps cleanup_scheduled_at with no job:
					// invisible to the candidate sweep. Error-level because nothing downstream notices
					// on its own — though as of part C the pending-cleanup backstop below DOES recover
					// exactly this state on a later run, which is belt-and-braces rather than a reason
					// to stop logging it.
					w.logger.Error("failed to release the cleanup stamp after a failed enqueue; item is hidden from the candidate sweep until the pending-cleanup backstop or an operator recovers it",
						"job_id", job.ID,
						"item_id", item.ID,
						"error", clearErr,
					)
				}
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

	// Issue #1486 follow-up part C: recover STRANDED rows. Runs unconditionally — in particular it must not sit
	// behind the "no items need cleanup" early return below, because a run with zero NEW candidates is
	// exactly when a backlog of stranded rows is most likely to be all there is to do.
	requeuedCount, requeueErrors := w.reenqueueStrandedCleanups(ctx, job.ID)

	if totalItems == 0 && requeuedCount == 0 && requeueErrors == 0 {
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
		"requeued_stranded", requeuedCount,
		"requeue_errors", requeueErrors,
		"duration_ms", time.Since(startTime).Milliseconds(),
	)

	// Don't fail the job if some items failed - they'll be retried next check
	return nil
}

// reenqueueStrandedCleanups is the pending-cleanup BACKSTOP (Issue #1486 follow-up part C). It
// re-enqueues an ItemCleanup job for every row that is stamped for cleanup, overdue, and whose Plaid
// side is not yet done. Returns (requeued, errors) for logging.
//
// It wires up repository.GetItemsPendingCleanup (`cleanup_scheduled_at IS NOT NULL AND
// cleanup_scheduled_at < NOW() AND (status IS NULL OR status NOT IN ('disconnected'))`), which was
// declared on the wide repository.LinkedItemRepository interface AND on this worker's narrow
// LinkedItemCleanupRepository, and implemented, with ZERO production callers before this.
//
// SCOPE, stated exactly, because the first version of this comment overclaimed it as "every STRANDED
// row, whatever stranded it" — which was false while that predicate still excluded 'replaced' (fixed
// in the same follow-up round; see the query's own doc comment). What it covers now is every stamped,
// overdue row whose Plaid side is NOT already recorded as done, which is the whole stranded set as
// this codebase can currently define it. Two limits are worth naming rather than implying closure:
//
//   - An UNSTAMPED strand is out of scope by construction — the predicate is the stamp. That is not a
//     gap in practice, because an unstamped row is exactly what the candidate sweep CAN re-select
//     (that sweep requires cleanup_scheduled_at IS NULL), except for a 'replaced'/'disconnected' row,
//     which the sweep excludes — see accountService.DeleteAccount's non-atomic-window disclosure for
//     the one known way to reach that state.
//   - A row whose /item/remove can NEVER succeed is re-enqueued here EVERY run, forever, and this
//     backstop is not a fix for it — see ItemCleanupWorker.Work's note on permanently-failing
//     removals for why that is fail-closed-but-permanent and what the escape hatch would have to be.
//
// THREE strand paths, one mechanism:
//
//  1. A dead-lettered ItemCleanup job. The enqueue call here passes no WithMaxAttempts option, so it
//     inherits DefaultEnqueueOptions's MaxAttempts=5 (queue/options.go), copied into
//     river.InsertOpts.MaxAttempts by toRiverInsertOpts (queue/river/client.go) — river.Config itself
//     sets no RetryPolicy, so River's own default plays no part here. DeadLetterHandler.HandleError
//     engages at job.Attempt >= job.MaxAttempts (queue/river/dead_letter.go) and nothing retries it
//     afterwards. Keep the dead-letter record for VISIBILITY but never as recovery:
//     DeadLetterService.Retry is admin-triggered and SINGLE-SHOT (job.RetriedAt != nil →
//     ErrDeadLetterJobAlreadyRetried), and it can consume that one replay slot even when the enqueue
//     fails. It is an alerting path, not a recovery path.
//  2. A failed enqueue whose stamp release ALSO failed (the branch above).
//  3. Any future stamp/enqueue asymmetry — the predicate is defined by the row's state, not by the
//     cause.
//
// WHY THIS RATHER THAN RELEASING THE STAMP on terminal failure, which was the first design proposed
// for this and was wrong: releasing the stamp hands the row back to the candidate sweep, which
// re-marks it with cleanupAt = now + gracePeriodDays for every non-immediate leg (only
// "zero_accounts" is immediate). Every terminal failure would therefore buy a fresh FULL grace window
// of Plaid billing — that is daily SELECTION, not daily removal. Re-enqueueing PRESERVES the original
// cleanup_scheduled_at, so this function deliberately calls neither MarkForCleanup nor
// ClearCleanupSchedule. It also needs no `job.Attempt >= job.MaxAttempts` plumbing in
// ItemCleanupWorker.Work.
//
// SAFE because the irreversible decision is not made here: ItemCleanupWorker.Work re-verifies
// eligibility via ClaimItemForCleanup immediately before /item/remove. That, not dedup, is what makes
// this safe — which matters because the dedup half needs QUALIFYING, and the first version of this
// comment stated it unconditionally:
//
// ItemCleanupUniqueOpts is `ByArgs: true`, and Reason is a field of ItemCleanupArgs, so dedup matches
// only when the args match EXACTLY. This function RECOMPUTES Reason from the row's current state
// (strandedCleanupReason, below), so if that state changed since the original enqueue — an
// 'error'→'revoked' webhook while the job sat Retryable, or the account count dropping to zero and
// flipping the leg to "zero_accounts" — the args differ, UniqueSkipped does NOT fire, and a SECOND
// live job exists for the same item. The usual case (state unchanged) does come back UniqueSkipped, as
// TestItemCleanupCheckWorker_Work_BackstopSkipsDuplicateLiveJobs pins; the changed-Reason case is
// pinned by TestItemCleanupCheckWorker_Work_BackstopRecomputesReasonFromCurrentState.
//
// The cost of that duplicate is bounded to ONE wasted /item/remove, and only because of part A: the
// second job's claim can still be granted (ClaimItemForCleanup does not check job state), it calls
// Plaid again, and Plaid answers ITEM_NOT_FOUND, which RemoveItem now maps to SUCCESS. Before part A
// that second call would have produced a spurious failure. Do not "fix" this by dropping Reason from
// the dedup key or by persisting the original Reason: the label is what the logs and the dead-letter
// record are read by, and the accurate current reason is worth more than a rarely-exercised dedup hit.
//
// NEVER fails the sweep. The candidate half's writes are already committed by the time this runs, and
// returning an error would make River retry the WHOLE sweep, re-running the half that AUTHORISES
// irreversible removals in order to retry the half that only recovers. Nothing about a stranded row
// changes when this fails, so the next daily run selects it again.
func (w *ItemCleanupCheckWorker) reenqueueStrandedCleanups(ctx context.Context, jobID int64) (requeued, errorCount int) {
	pending, err := w.linkedItemRepo.GetItemsPendingCleanup(ctx)
	if err != nil {
		w.logger.Error("failed to query items pending cleanup; stranded rows are not recovered this run",
			"job_id", jobID,
			"error", err,
		)
		return 0, 1
	}
	if len(pending) == 0 {
		return 0, 0
	}

	// GetItemsPendingCleanup has no LIMIT of its own, so bound the per-run re-enqueue volume.
	// maxItemsPerSweep is reused as that bound WITHOUT changing its value or its derivation (see
	// maxItemsPerCleanupSweep's doc comment): these are re-enqueues of work a PREVIOUS run already
	// authorised and already counted against its own cap, not new authorisations, and each one is
	// gated again by ItemCleanupUniqueOpts and ClaimItemForCleanup. The bound exists to cap per-run
	// queue volume, not to re-derive the blast radius. A remainder is deferred to the next run, which
	// re-selects it unchanged.
	if len(pending) > w.maxItemsPerSweep {
		w.logger.Warn("more stranded cleanup items than one run re-enqueues; deferring the remainder to the next periodic run",
			"job_id", jobID, "pending", len(pending), "bound", w.maxItemsPerSweep)
		pending = pending[:w.maxItemsPerSweep]
	}

	uniqueOpts := ItemCleanupUniqueOpts()
	for _, item := range pending {
		// The grace period is already spent — GetItemsPendingCleanup only returns overdue rows — so
		// this is enqueued for immediate execution. No WithScheduledAt: deferring it again would add a
		// second grace window on top of the one that already elapsed.
		result, enqueueErr := w.jobQueue.Enqueue(ctx, ItemCleanupArgs{
			LinkedItemID: item.ID,
			UserID:       item.UserID,
			// Recomputed rather than persisted: the row's CURRENT state is what
			// ClaimItemForCleanup will re-evaluate anyway, and Reason is a label for logs and
			// dedup args, not an authorisation.
			Reason: w.strandedCleanupReason(ctx, item),
		}, queue.WithUnique(*uniqueOpts))
		if enqueueErr != nil {
			// Deliberately NOT releasing the stamp here (unlike the candidate loop above, where THIS
			// run set it): the stamp is what keeps this row selectable by this very backstop next run.
			w.logger.Error("failed to re-enqueue a stranded item cleanup job; it stays selectable for the next run",
				"job_id", jobID,
				"item_id", item.ID,
				"error", enqueueErr,
			)
			errorCount++
			continue
		}
		if result.UniqueSkipped {
			// Not stranded after all: a live job already exists for this item.
			w.logger.Debug("stranded-cleanup backstop: item already has a live cleanup job",
				"job_id", jobID, "item_id", item.ID)
			continue
		}
		w.logger.Warn("re-enqueued a STRANDED item cleanup job — a previous attempt left this item stamped for cleanup with no job in flight, so Plaid was still billing for it",
			"job_id", jobID,
			"item_id", item.ID,
			"user_id", item.UserID,
			"cleanup_scheduled_at", item.CleanupScheduledAt,
			"cleanup_job_id", result.ID,
		)
		requeued++
	}
	return requeued, errorCount
}

// strandedCleanupReason labels a re-enqueued stranded job. Wraps determineCleanupReasonAndTiming and
// discards its isImmediate flag: a stranded row's grace period has already elapsed by construction
// (GetItemsPendingCleanup requires cleanup_scheduled_at < NOW()), so every re-enqueue is immediate
// regardless of leg, and reusing the flag would only invite a caller to re-defer it.
func (w *ItemCleanupCheckWorker) strandedCleanupReason(ctx context.Context, item domain.LinkedItem) string {
	reason, _ := w.determineCleanupReasonAndTiming(ctx, item)
	return reason
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
