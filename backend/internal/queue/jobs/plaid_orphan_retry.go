package jobs

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/riverqueue/river"

	"github.com/maxnussbaum/BoostedBudget/internal/observability"
	"github.com/maxnussbaum/BoostedBudget/internal/queue"
	"github.com/maxnussbaum/BoostedBudget/internal/repository"
	"github.com/maxnussbaum/BoostedBudget/internal/services"
)

// PlaidOrphanRetryArgs is the periodic sweep that re-attempts Plaid /item/remove for Items stranded by
// a GDPR erasure, using the credential retained on the orphan record. Issue #1497.
//
// ⚠ IT IS NOT THE PURGE, and it deliberately cannot purge. See PlaidOrphanTokenPurgeArgs: the retention
// deadline is enforced by a separate job with a separate registration and no Plaid dependency, so that
// this job being broken, unwired, or absent (Plaid unconfigured) cannot extend how long a credential
// lives. This job's only job is to make the retention USEFUL; the purge makes it BOUNDED.
type PlaidOrphanRetryArgs struct {
	// TriggerSource indicates what triggered this run (e.g., "periodic", "manual").
	TriggerSource string `json:"trigger_source,omitempty"`
}

// Kind returns the unique identifier for this job type.
func (PlaidOrphanRetryArgs) Kind() string { return "plaid_orphan_retry" }

// orphanRetryPageSize bounds one GetRetryablePage query. 500 matches itemCleanupCheckPageSize and the
// other sweeps on this branch; a page's cost is a row fetch either way.
const orphanRetryPageSize = 500

// maxOrphanRetriesPerSweep bounds the TOTAL number of Plaid /item/remove calls one run makes.
//
// 1,000, matching maxItemsPerCleanupSweep and maxUsersPerRetentionSweep so there is one blast-radius
// number across the branch's sweeps. The per-row action here is a Plaid API call for an Item whose owner
// has already been erased, so — unlike the cleanup sweep — there is no user left to harm by removing it
// too eagerly; what the cap bounds is Plaid API volume and one run's duration. Migration 000134 expects
// this table to hold single digits of rows, so the cap is unreachable in practice and hitting it is
// itself the alarming signal.
//
// Hitting it stops THIS run early and defers the remainder; since Issue #1477 the next run RESUMES
// from the timestamp key this one stopped at (sweep_cursors) instead of restarting from the oldest
// record, so a permanently-failing head no longer consumes every run's whole budget. That
// replacement guarantee is bounded, not absolute — it is stated once, with its bound, in
// repository/sweep_cursor_repository.go's header. Do not restate it as "no starvation".
const maxOrphanRetriesPerSweep = 1000

// PlaidOrphanRetryWorker runs the retry sweep.
type PlaidOrphanRetryWorker struct {
	river.WorkerDefaults[PlaidOrphanRetryArgs]

	svc         services.PlaidOrphanRetryService
	logger      *slog.Logger
	pageSize    int
	maxPerSweep int
	// sweepCursors persists where a capped run stopped so the NEXT run resumes there instead of
	// restarting from the oldest candidate (Issue #1477). Optional/nil-safe: a nil store reproduces
	// the pre-#1477 behaviour exactly. See sweep_resume.go and
	// repository/sweep_cursor_repository.go.
	sweepCursors sweepCursorStore
}

// PlaidOrphanRetryWorkerDeps contains the dependencies for PlaidOrphanRetryWorker.
type PlaidOrphanRetryWorkerDeps struct {
	Service services.PlaidOrphanRetryService
	Logger  *slog.Logger
	// PageSize / MaxPerSweep override the constants above. Zero/unset uses the production defaults;
	// they exist so tests can exercise the paging loop and the cap without seeding hundreds of rows.
	PageSize    int
	MaxPerSweep int
	// SweepCursors persists this sweep's cross-run resume position (Issue #1477). Optional and
	// nil-safe: unwired, every run restarts from the oldest candidate, which is exactly the
	// pre-#1477 starvation-prone behaviour — production MUST wire it (main.go).
	SweepCursors repository.SweepCursorRepository
}

// NewPlaidOrphanRetryWorker creates the worker. Service may be nil (Plaid not configured), in which case
// Work fails loudly rather than reporting a successful run that retried nothing.
func NewPlaidOrphanRetryWorker(deps PlaidOrphanRetryWorkerDeps) *PlaidOrphanRetryWorker {
	logger := deps.Logger
	if logger == nil {
		logger = slog.Default()
	}
	w := &PlaidOrphanRetryWorker{
		svc:         deps.Service,
		logger:      logger,
		pageSize:    intOrDefault(deps.PageSize, orphanRetryPageSize),
		maxPerSweep: intOrDefault(deps.MaxPerSweep, maxOrphanRetriesPerSweep),
	}
	// Guarded rather than assigned directly, for the reason given in
	// NewScheduledDeletionCheckWorker: "unwired means nil, and nil means do not persist" stays true
	// by inspection.
	if deps.SweepCursors != nil {
		w.sweepCursors = deps.SweepCursors
	}
	return w
}

// Work runs one retry sweep and logs its tally.
//
// It returns the sweep's error so River retries a run that could not read its work list. Per-ROW
// failures never reach here — the service records each one against its record and steps over it, which
// is the same asymmetry ItemCleanupCheckWorker draws and for the same reason: one unremovable Item must
// not stop the others.
func (w *PlaidOrphanRetryWorker) Work(ctx context.Context, job *river.Job[PlaidOrphanRetryArgs]) (err error) {
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

	if w.svc == nil {
		return fmt.Errorf("plaid orphan retry: no service wired; stranded Plaid Items are NOT being retried")
	}

	// Issue #1477: the same persisted cursor mechanism the other three capped sweeps use, handed to
	// the service because THIS sweep's paging loop lives there. See sweep_resume.go's header.
	resume := orphanRetryResume{newSweepResume(ctx, w.sweepCursors, repository.SweepCursorOrphanRetry, w.logger, job.ID)}

	res, err := w.svc.Sweep(ctx, time.Now().UTC(), w.pageSize, w.maxPerSweep, resume)
	// The tally is logged even on error, because a sweep that failed on page three still resolved
	// whatever pages one and two resolved, and a log line that omitted that would misdescribe the run.
	w.logger.Info("plaid orphan retry sweep completed",
		"job_id", job.ID,
		"trigger_source", job.Args.TriggerSource,
		"attempted", res.Attempted,
		"resolved", res.Resolved,
		"failed", res.Failed,
		"undecryptable", res.Undecryptable,
		"skipped", res.Skipped,
		// Non-zero means this run was long enough for a retention deadline to pass while it ran. Worth
		// an operator's eye: those Items are now unremovable by any automated path.
		"deadline_elapsed", res.DeadlineElapsed,
		"record_errors", res.RecordErrors,
		"cap_hit", res.CapHit,
		"duration_ms", time.Since(start).Milliseconds(),
	)
	if err != nil {
		return fmt.Errorf("plaid orphan retry: %w", err)
	}
	return nil
}

// PlaidOrphanRetryUniqueOpts returns the uniqueness options this job's producers pass.
//
// Uniqueness matters more here than for the purge: two concurrent runs would both read the same
// unresolved rows and both call Plaid for them. The duplicate is bounded to one wasted /item/remove
// (the second answers ITEM_NOT_FOUND, which this sweep treats as success, and MarkResolved is
// first-wins), but there is no reason to buy it. Completed is included for the RunOnStart reason — see
// PlaidOrphanTokenPurgeUniqueOpts and Issue #1448.
func PlaidOrphanRetryUniqueOpts() *queue.UniqueOpts {
	return &queue.UniqueOpts{
		ByArgs:   false,
		ByPeriod: 24 * time.Hour,
		ByState: []queue.JobState{
			queue.JobStateAvailable,
			queue.JobStateScheduled,
			queue.JobStateRunning,
			queue.JobStateRetryable,
			queue.JobStatePending,
			queue.JobStateCompleted,
		},
	}
}

// PlaidOrphanRetryInsertOpts returns the *river.InsertOpts the periodic registration must use.
func PlaidOrphanRetryInsertOpts() *river.InsertOpts {
	return riverInsertOptsFromUnique(queue.QueueLow, PlaidOrphanRetryUniqueOpts())
}

// PlaidOrphanRetryPeriodicOpts returns the *river.PeriodicJobOpts the periodic registration must use.
func PlaidOrphanRetryPeriodicOpts() *river.PeriodicJobOpts {
	return &river.PeriodicJobOpts{RunOnStart: true, ID: PlaidOrphanRetryArgs{}.Kind()}
}
