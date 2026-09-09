// Package jobs contains River job definitions and workers for async processing.
package jobs

// Periodic balance snapshotting (#1626 Phase 3, Defect 3 / Task G) — a DISPATCHER plus a per-item
// CAPTURE worker.
//
// ⚠ WHY TWO WORKERS AND NOT ONE, because the obvious mirror is the wrong one. PortfolioSnapshotWorker
// looks like the template — it is a daily periodic job that touches every user — but it is ONE
// global job that loops users INLINE, and a River periodic spec builds ONE JobArgs, not a fleet.
// Copying it here would mean one job iterating every linked item and calling Plaid for each, in
// which case one item's provider failure either aborts the remaining items or is swallowed. Both are
// unacceptable for this job specifically: swallowing is the exact defect this task exists to remove,
// and aborting loses the rest of the day's items with no backfill to recover them.
//
// So: a periodic DISPATCHER enumerates eligible items and enqueues one CAPTURE job each, and River's
// own per-job retry then applies PER ITEM. That is the entire reason for the split — there is no
// Plaid backoff or rate-limit machinery anywhere in this tree (no 429 handling, no backoff), so
// River's per-job retry is the only retry there is, and the split is what makes it granular.

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/riverqueue/river"

	"github.com/maxnussbaum/BoostedBudget/internal/cache"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/observability"
	"github.com/maxnussbaum/BoostedBudget/internal/queue"
	"github.com/maxnussbaum/BoostedBudget/internal/services"
)

// ─── args ──────────────────────────────────────────────────────────────────────────────────────

// BalanceSnapshotDispatchArgs is the periodic job that fans out one capture job per eligible linked
// item.
type BalanceSnapshotDispatchArgs struct {
	// TriggerSource indicates what triggered this run (e.g., "periodic", "manual").
	TriggerSource string `json:"trigger_source,omitempty"`
}

// Kind returns the unique identifier for this job type.
func (BalanceSnapshotDispatchArgs) Kind() string { return "balance_snapshot_dispatch" }

// BalanceSnapshotCaptureArgs captures the current UTC day's balances for exactly ONE linked item.
type BalanceSnapshotCaptureArgs struct {
	// UserID is the owner the dispatcher enumerated alongside ItemID. Carried so the capture can
	// refuse an item that changed hands, and so the cache bump has a scope without a second read.
	UserID string `json:"user_id"`
	// ItemID is the INTERNAL UUID of the linked item, not Plaid's item_id.
	ItemID string `json:"item_id"`
}

// Kind returns the unique identifier for this job type.
func (BalanceSnapshotCaptureArgs) Kind() string { return "balance_snapshot_capture" }

// ─── cadence, stagger and bounds ───────────────────────────────────────────────────────────────

// BalanceSnapshotDispatchHourUTC is the UTC hour the daily dispatch runs at.
//
// ⚠ THE HOUR IS A DECISION WITH A REASON, and the reason matters more than the number:
//
//   - LATE in the day, because a net-worth series should mean something close to a closing balance.
//   - But NOT 23:30. A run that fails at the boundary has no room for River's retry budget before
//     midnight UTC, and a failed run at the boundary loses the day PERMANENTLY — there is no
//     backfill, by design. 21:00 leaves three hours of retry headroom.
//
// ⚠ IT IS A UTC HOUR, which lands at a different LOCAL time for every user: a UTC-11 user's
// 21:00-UTC row is stamped with a date that is still "tomorrow" for them. For a daily balance series
// that is accepted; silently ambiguous day boundaries are not, which is why every date decision on
// this path — the cohort question, the snapshot instant, the uniqueness key — is spelled UTC.
const BalanceSnapshotDispatchHourUTC = 21

// balanceCaptureItemsPerRun bounds how many items ONE dispatch run may enqueue.
//
// Unlike this codebase's cleanup sweeps, a truncated run here is NOT resumable from a cursor: the
// items it does not reach simply have no row for today, and there is no backfill. The cap is
// therefore set high enough that reaching it is a capacity signal rather than routine, and the run
// logs cap_reached so that signal is visible instead of silent.
const balanceCaptureItemsPerRun = 5000

// balanceCaptureStagger / balanceCapturePerStaggerWindow spread one run's enqueues over time instead
// of releasing every capture job at one instant.
//
// ⚠ WHAT THIS IS AND IS NOT. It is a release-rate bound on PROVIDER calls: at most
// balanceCapturePerStaggerWindow capture jobs become available per balanceCaptureStagger, so the
// full cap of 5000 items is released over about seven minutes rather than instantaneously. It is NOT
// a hard concurrency limit — River's own per-queue worker count is what caps simultaneous execution,
// and these jobs go on the low-priority queue with everything else that runs daily. There is no
// Plaid backoff machinery in this tree to lean on, so the honest description is "paced", not "rate
// limited", and this comment says so rather than implying a guarantee the code does not make.
const (
	balanceCaptureStagger          = 2 * time.Second
	balanceCapturePerStaggerWindow = 25
)

// dailyUTCSchedule is a river.PeriodicSchedule that fires once per day at a fixed UTC hour.
//
// River ships PeriodicInterval (every N since the last run) and nothing else, and an interval cannot
// express "21:00 UTC": with RunOnStart, a 24-hour interval anchors the daily run to whenever the
// process last started, so a deploy at 04:00 would move every subsequent capture to 04:00. That
// would silently discard the closing-balance reasoning behind the hour.
type dailyUTCSchedule struct{ hour int }

// Next returns the next occurrence of the configured UTC hour strictly AFTER current.
//
// Strictly after, deliberately: returning `current` itself when called exactly on the hour would
// make River's enqueuer see a schedule that never advances.
func (s dailyUTCSchedule) Next(current time.Time) time.Time {
	c := current.UTC()
	next := time.Date(c.Year(), c.Month(), c.Day(), s.hour, 0, 0, 0, time.UTC)
	if !next.After(c) {
		next = next.AddDate(0, 0, 1)
	}
	return next
}

// BalanceSnapshotDispatchSchedule returns the periodic schedule for the daily dispatch.
func BalanceSnapshotDispatchSchedule() river.PeriodicSchedule {
	return dailyUTCSchedule{hour: BalanceSnapshotDispatchHourUTC}
}

// ─── the dispatcher ────────────────────────────────────────────────────────────────────────────

// BalanceCaptureItemReader is the one repository method the dispatcher needs. A narrow,
// consumer-defined interface, matching this package's convention — and narrow on purpose: the
// dispatcher must not be able to read or write anything else.
type BalanceCaptureItemReader interface {
	GetItemsForBalanceCapture(ctx context.Context, limit int) ([]domain.BalanceCaptureTarget, error)
}

// BalanceSnapshotDispatchWorker enumerates eligible linked items and enqueues one capture job each.
type BalanceSnapshotDispatchWorker struct {
	river.WorkerDefaults[BalanceSnapshotDispatchArgs]

	items    BalanceCaptureItemReader
	jobQueue queue.JobQueue
	logger   *slog.Logger
}

// BalanceSnapshotDispatchWorkerDeps holds the dispatcher's dependencies.
type BalanceSnapshotDispatchWorkerDeps struct {
	ItemRepo BalanceCaptureItemReader
	// JobQueue is set after the River client exists (circular dependency), via SetJobQueue.
	JobQueue queue.JobQueue
	Logger   *slog.Logger
}

// NewBalanceSnapshotDispatchWorker constructs the dispatcher.
func NewBalanceSnapshotDispatchWorker(deps BalanceSnapshotDispatchWorkerDeps) *BalanceSnapshotDispatchWorker {
	logger := deps.Logger
	if logger == nil {
		logger = slog.Default()
	}
	return &BalanceSnapshotDispatchWorker{items: deps.ItemRepo, jobQueue: deps.JobQueue, logger: logger}
}

// SetJobQueue sets the job queue for enqueueing capture jobs, resolving the worker-registration
// before-client-creation circular dependency.
func (w *BalanceSnapshotDispatchWorker) SetJobQueue(jq queue.JobQueue) { w.jobQueue = jq }

// Work enumerates eligible items and enqueues one capture job each, staggered.
//
// ⚠ A FAILED ENQUEUE DOES NOT ABORT THE RUN. The loop continues and the failures are counted and
// returned together at the end. That is the dispatcher half of the per-item isolation the split
// exists for: one item's problem must never cost the rest of the users their day, and there is no
// backfill to repair the ones a bail-out would skip.
//
// The run DOES return an error when any enqueue failed, so River retries — the capture jobs that
// were already enqueued are idempotent (the cohort skip makes a re-run cheap and migration 000191's
// unique index makes a duplicate write a no-op), so a retry of the whole dispatch is safe.
func (w *BalanceSnapshotDispatchWorker) Work(ctx context.Context, job *river.Job[BalanceSnapshotDispatchArgs]) (err error) {
	start := time.Now()
	defer func() {
		status := "success"
		if err != nil {
			status = "failed"
		} else {
			observability.RecordJobSuccess(job.Kind)
		}
		observability.JobsCompleted.WithLabelValues(job.Kind, job.Queue, status).Inc()
		observability.JobDuration.WithLabelValues(job.Kind).Observe(time.Since(start).Seconds())
	}()

	w.logger.Info("starting balance snapshot dispatch",
		"job_id", job.ID, "trigger_source", job.Args.TriggerSource, "attempt", job.Attempt)

	if w.jobQueue == nil {
		// Not a silent no-op: a dispatcher with no queue cannot do its only job, and on a daily
		// schedule that would mean no balance history for anybody, indefinitely.
		return errors.New("balance snapshot dispatch: no job queue wired")
	}

	targets, err := w.items.GetItemsForBalanceCapture(ctx, balanceCaptureItemsPerRun)
	if err != nil {
		return fmt.Errorf("balance snapshot dispatch: enumerate items: %w", err)
	}
	if len(targets) == 0 {
		w.logger.Info("balance snapshot dispatch: no eligible items", "job_id", job.ID)
		return nil
	}

	now := time.Now()
	var enqueued int
	var failures []error
	for i, target := range targets {
		// The release schedule: each block of balanceCapturePerStaggerWindow items becomes available
		// one balanceCaptureStagger later than the previous block. Integer division, so the FIRST
		// block is available immediately — a run that dispatches a handful of items pays no delay at
		// all.
		delay := time.Duration(i/balanceCapturePerStaggerWindow) * balanceCaptureStagger

		_, enqueueErr := w.jobQueue.Enqueue(ctx,
			BalanceSnapshotCaptureArgs{UserID: target.UserID, ItemID: target.LinkedItemID},
			queue.WithQueue(queue.QueueLow),
			queue.WithScheduledAt(now.Add(delay)),
			queue.WithUnique(*BalanceSnapshotCaptureUniqueOpts()),
		)
		if enqueueErr != nil {
			failures = append(failures, fmt.Errorf("item %s: %w", target.LinkedItemID, enqueueErr))
			continue
		}
		enqueued++
	}

	// cap_reached is the backlog signal. Without it this line reads identically whether the run
	// dispatched every eligible item or merely the first 5000 of more — and unlike this codebase's
	// cursor-resumed sweeps, the items past the cap are not picked up later, they just have no row
	// for today.
	w.logger.Info("balance snapshot dispatch complete",
		"job_id", job.ID,
		"items", len(targets),
		"enqueued", enqueued,
		"failed", len(failures),
		"cap_reached", len(targets) >= balanceCaptureItemsPerRun,
		"duration_ms", time.Since(start).Milliseconds(),
	)

	if len(failures) > 0 {
		return fmt.Errorf("balance snapshot dispatch: %d of %d enqueues failed: %w",
			len(failures), len(targets), errors.Join(failures...))
	}
	return nil
}

// ─── the capture worker ────────────────────────────────────────────────────────────────────────

// BalanceCaptureHouseholdLookup resolves the household (if any) the captured user belongs to, so the
// capture also invalidates the household's cache scope. Narrow single-method interface matching this
// package's convention; satisfied structurally by *repository.householdRepository.
type BalanceCaptureHouseholdLookup interface {
	GetHouseholdForUser(ctx context.Context, userID string) (household *domain.Household, isOwner bool, found bool, err error)
}

// BalanceSnapshotCaptureWorker captures one linked item's balances for the current UTC day.
type BalanceSnapshotCaptureWorker struct {
	river.WorkerDefaults[BalanceSnapshotCaptureArgs]

	capture services.BalanceCaptureService
	logger  *slog.Logger

	// cacheGens/cacheHousehold are optional (nil = no bump); see SetCacheInvalidation.
	cacheGens      *cache.Generations
	cacheHousehold BalanceCaptureHouseholdLookup
}

// BalanceSnapshotCaptureWorkerDeps holds the capture worker's dependencies.
type BalanceSnapshotCaptureWorkerDeps struct {
	CaptureService services.BalanceCaptureService
	Logger         *slog.Logger
}

// NewBalanceSnapshotCaptureWorker constructs the capture worker.
func NewBalanceSnapshotCaptureWorker(deps BalanceSnapshotCaptureWorkerDeps) *BalanceSnapshotCaptureWorker {
	logger := deps.Logger
	if logger == nil {
		logger = slog.Default()
	}
	return &BalanceSnapshotCaptureWorker{capture: deps.CaptureService, logger: logger}
}

// SetCacheInvalidation wires cache generation bumping (setter injection, matching this codebase's
// convention).
//
// ⚠ THE GENERATION, NOT NamespaceAnalytics.SchemaVersion. An earlier draft of this design named the
// wrong mechanism and they are not interchangeable: SchemaVersion versions a cached value's SHAPE
// (bumping it orphans every entry written under the previous version, for everybody), whereas a
// GENERATION bump invalidates one scope's entries because its underlying data changed. This job
// changes data, not shapes — GetAggregatedHistory returns exactly what it returned before, there is
// simply another row behind it — so the generation is the correct and only mechanism here.
//
// Without it, a day's newly captured rows stay invisible to cached dashboard and analytics reads
// until the namespace TTL expires. GetNetWorthTrend is a cached read; the sync path's equivalent
// safety is bumpCacheForSync, and a River job never passes through the HTTP mutation middleware, so
// there is no other bump that could cover this write.
func (w *BalanceSnapshotCaptureWorker) SetCacheInvalidation(gens *cache.Generations, hh BalanceCaptureHouseholdLookup) {
	w.cacheGens = gens
	w.cacheHousehold = hh
}

// Work captures one item's balances for the current UTC day.
func (w *BalanceSnapshotCaptureWorker) Work(ctx context.Context, job *river.Job[BalanceSnapshotCaptureArgs]) (err error) {
	args := job.Args
	start := time.Now()
	defer func() {
		status := "success"
		if err != nil {
			status = "failed"
		} else {
			observability.RecordJobSuccess(job.Kind)
		}
		observability.JobsCompleted.WithLabelValues(job.Kind, job.Queue, status).Inc()
		observability.JobDuration.WithLabelValues(job.Kind).Observe(time.Since(start).Seconds())
	}()

	if w.capture == nil {
		return errors.New("balance snapshot capture: no capture service wired")
	}

	outcome, err := w.capture.CaptureItemBalances(ctx, args.ItemID, args.UserID)
	if err != nil {
		// Returned so River retries. A partial capture reaches here as an error too, which is the
		// whole point: the writer this replaced logged its per-account failures and returned
		// success, so a partial cohort committed while River recorded the item as done.
		w.logger.Error("balance snapshot capture failed",
			"job_id", job.ID, "item_id", args.ItemID, "written", outcome.Written, "error", err)
		return err
	}

	// ⚠ THE BUMP IS CONDITIONAL ON A ROW HAVING BEEN WRITTEN, and that is a deliberate departure
	// from this codebase's other bumps (which fire unconditionally on success and justify it as
	// harmless over-invalidation).
	//
	// Here it would NOT be harmless. The gap-filler's overwhelmingly common outcome is
	// CohortComplete — an item a webhook already covered today — and it runs for every item every
	// day. Bumping unconditionally would therefore discard every active user's cached analytics once
	// a day for a run that wrote nothing, converting a no-op into a guaranteed daily cache miss on
	// the dashboard for the entire user base.
	//
	// The condition is safe because it is exactly "did this job change the data a cached read would
	// see". AlreadyPresent is excluded on purpose: that row was written by the capture that WON the
	// race, and that capture did its own bump.
	if outcome.Written > 0 {
		w.bumpCacheForUser(ctx, args.UserID)
	}

	w.logger.Info("balance snapshot capture complete",
		"job_id", job.ID,
		"item_id", args.ItemID,
		"day", outcome.Day.Format("2006-01-02"),
		"cohort_complete", outcome.CohortComplete,
		"ineligible", outcome.Ineligible,
		"plaid_called", outcome.PlaidCalled,
		"written", outcome.Written,
		"already_present", outcome.AlreadyPresent,
		"deduped", outcome.Deduped,
		"unmatched", outcome.Unmatched,
		"no_balance", outcome.NoBalance,
		"duration_ms", time.Since(start).Milliseconds(),
	)
	return nil
}

// bumpCacheForUser invalidates userID's cached analytics and, when resolvable, their household's.
//
// Void by design, matching every other generation bump in this codebase: the snapshot rows have
// already committed and a bump failure must never retroactively fail them.
//
// Runs on context.WithTimeout(context.WithoutCancel(ctx), bumpCacheTimeout) rather than the raw job
// context. River cancels a job's context on shutdown, retry or timeout; a cancellation landing after
// the write has committed but before the bump would otherwise be carried into Bump, which swallows
// its own errors silently — the rows would exist and be invisible to cached reads until TTL.
// WithoutCancel alone would leave the bump unbounded (it strips the parent's deadline too), hence
// the explicit timeout. Same shape as effective_category_refresh.go and the sync services'
// bumpCacheForSync.
func (w *BalanceSnapshotCaptureWorker) bumpCacheForUser(ctx context.Context, userID string) {
	if w.cacheGens == nil {
		return
	}
	bumpCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), bumpCacheTimeout)
	defer cancel()

	w.cacheGens.Bump(bumpCtx, cache.ScopeUser, userID, BalanceSnapshotCaptureArgs{}.Kind())

	if w.cacheHousehold == nil {
		return
	}
	household, _, found, err := w.cacheHousehold.GetHouseholdForUser(bumpCtx, userID)
	if err != nil {
		w.logger.Error("cache invalidation: household lookup failed after a balance capture; "+
			"household-scoped cache may serve stale data until its namespace TTL",
			"user_id", userID, "error", err)
		return
	}
	if found && household != nil {
		// The household scope matters here specifically: household balance history has its own
		// three-stage per-account/day collapse over the SAME rows this job writes, so a member's new
		// snapshot changes what every other member's cached household total should say.
		w.cacheGens.Bump(bumpCtx, cache.ScopeHousehold, household.ID, BalanceSnapshotCaptureArgs{}.Kind())
	}
}

// ─── River options ─────────────────────────────────────────────────────────────────────────────

// BalanceSnapshotDispatchUniqueOpts returns the dispatcher's uniqueness options.
//
// ⚠ ByPeriod IS ONE HOUR, NOT 24, AND THAT IS THE OPPOSITE OF WHAT THE OTHER DAILY JOBS DO. The
// reason is that this job's cadence comes from its SCHEDULE (21:00 UTC) rather than from an
// interval, and JobStateCompleted is in the dedup key because RunOnStart requires it:
//
//   - with ByPeriod 24h, River's dedup bucket is Truncate(24h) of the enqueue instant — i.e. the UTC
//     calendar day — so a deploy at ANY time today would suppress today's 21:00 run against the
//     already-completed RunOnStart run. On a day with a deploy, the closing-balance run would
//     silently not happen, and no test would have noticed because the dispatcher DID run.
//   - with ByPeriod 1h, the only inserts that deduplicate are ones within the same hour, which is
//     exactly the hazard RunOnStart creates (a start-of-process catch-up insert double-firing
//     alongside an already-scheduled run) and nothing else.
//
// A shorter-than-cadence ByPeriod is safe here precisely because the schedule, not the dedup window,
// is what makes the job daily.
func BalanceSnapshotDispatchUniqueOpts() *queue.UniqueOpts {
	return &queue.UniqueOpts{
		ByPeriod: 1 * time.Hour,
		ByState: []queue.JobState{
			queue.JobStateAvailable,
			queue.JobStateScheduled,
			queue.JobStateRunning,
			queue.JobStateRetryable,
			queue.JobStatePending,
			// Completed is REQUIRED because this job is registered with RunOnStart: true — without
			// it, a run that completed minutes ago does not suppress the catch-up insert.
			queue.JobStateCompleted,
		},
	}
}

// BalanceSnapshotDispatchInsertOpts returns the *river.InsertOpts the dispatcher's periodic
// registration must use.
func BalanceSnapshotDispatchInsertOpts() *river.InsertOpts {
	return riverInsertOptsFromUnique(queue.QueueLow, BalanceSnapshotDispatchUniqueOpts())
}

// BalanceSnapshotDispatchPeriodicOpts returns the *river.PeriodicJobOpts the dispatcher's periodic
// registration must use.
//
// RunOnStart: true is wanted — a deploy then fills the current day immediately instead of waiting for
// the next 21:00 window — and it is safe because BalanceSnapshotDispatchInsertOpts carries a real
// dedup guarantee AND the work itself is idempotent (the cohort skip makes a redundant run cheap and
// migration 000191's unique index makes a redundant write a no-op).
func BalanceSnapshotDispatchPeriodicOpts() *river.PeriodicJobOpts {
	return &river.PeriodicJobOpts{RunOnStart: true, ID: BalanceSnapshotDispatchArgs{}.Kind()}
}

// BalanceSnapshotCaptureUniqueOpts returns the per-item capture job's uniqueness options.
//
// ByArgs (so it is per item, not global) over the NON-TERMINAL states only. Completed is deliberately
// EXCLUDED here, unlike the dispatcher's: a completed capture must not suppress a later one, because
// a capture that ran this morning covered this morning's roster and an account added since then still
// needs a row today. What stops the redundant work is the cohort skip, which costs one indexed query
// and no provider call.
//
// ByPeriod is the window inside which an identical capture is considered a duplicate. It bounds only
// pile-up: two dispatch runs in the same hour (a deploy, say) will not queue the same item twice.
func BalanceSnapshotCaptureUniqueOpts() *queue.UniqueOpts {
	return &queue.UniqueOpts{
		ByArgs:   true,
		ByPeriod: 1 * time.Hour,
		ByState: []queue.JobState{
			queue.JobStateAvailable,
			queue.JobStateScheduled,
			queue.JobStateRunning,
			queue.JobStateRetryable,
			queue.JobStatePending,
		},
	}
}
