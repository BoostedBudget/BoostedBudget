package jobs

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/riverqueue/river"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/observability"
	"github.com/maxnussbaum/BoostedBudget/internal/queue"
)

// PlaidOrphanTokenPurgeArgs is the periodic sweep that DESTROYS retained Plaid credentials whose
// retention deadline has passed. Issue #1497.
//
// ⚠ THIS JOB EXISTS SEPARATELY FROM THE RETRY JOB ON PURPOSE, AND THE SEPARATION IS THE FEATURE.
// Migration 000135 promises that a token retained past its owner's GDPR erasure is destroyed when its
// deadline passes "whether or not the removal ever succeeded and whether or not the retry job ever
// ran". If purging were part of the retry job — the obvious design — then that promise would be
// conditional on the retry job being registered, wired, leader-elected, holding a Plaid client, and not
// starved behind a backlog. It is not conditional on any of those, so the purge is its own job with its
// own registration and its own dependency: the database, and nothing else.
//
// In particular this worker takes NO Plaid client and NO service. It is deliberately NOT nil-guarded on
// Plaid being configured in cmd/server, because orphan records (and therefore retained credentials) are
// written by DeleteUserCascade regardless of whether Plaid is configured now. A deployment that has
// turned Plaid off must still keep its retention promise.
type PlaidOrphanTokenPurgeArgs struct {
	// TriggerSource indicates what triggered this run (e.g., "periodic", "manual").
	TriggerSource string `json:"trigger_source,omitempty"`
}

// Kind returns the unique identifier for this job type.
func (PlaidOrphanTokenPurgeArgs) Kind() string { return "plaid_orphan_token_purge" }

// maxOrphanTokenPurgesPerSweep bounds how many rows ONE run rewrites.
//
// It is 1,000 for the same reason maxItemsPerCleanupSweep and maxUsersPerRetentionSweep are: one
// blast-radius number to reason about across the branch's sweeps rather than three that invite
// comparison. Unlike those two the per-row action here is not irreversible in the harmful direction —
// destroying a credential is the SAFE direction, and the cost of processing too few rows is that a
// credential lives a little past its deadline, which is the only cost worth bounding at all.
//
// It is therefore set low not to limit risk but to bound one statement's write volume, and the drain
// arithmetic is trivial: migration 000134 expects this table to hold single digits of rows, so the cap
// is unreachable in practice and hitting it is itself the alarming signal. The WARN below says so.
const maxOrphanTokenPurgesPerSweep = 1000

// OrphanTokenPurger is the one capability this worker needs. Narrow on purpose: a worker that could
// also read, retry or resolve would invite someone to make the purge conditional on one of those.
type OrphanTokenPurger interface {
	PurgeExpiredTokens(ctx context.Context, asOf time.Time, limit int) (int, error)
}

// PlaidOrphanTokenPurgeWorker runs the purge.
type PlaidOrphanTokenPurgeWorker struct {
	river.WorkerDefaults[PlaidOrphanTokenPurgeArgs]

	repo      OrphanTokenPurger
	logger    *slog.Logger
	maxPerRun int
}

// PlaidOrphanTokenPurgeWorkerDeps contains the dependencies for PlaidOrphanTokenPurgeWorker.
type PlaidOrphanTokenPurgeWorkerDeps struct {
	Repo   OrphanTokenPurger
	Logger *slog.Logger
	// MaxPerRun overrides maxOrphanTokenPurgesPerSweep. Zero/unset uses the production default; exists
	// so tests can exercise the cap without seeding a thousand rows. Production should never set it.
	MaxPerRun int
}

// NewPlaidOrphanTokenPurgeWorker creates the worker. Repo may be nil, in which case Work fails loudly
// rather than silently doing nothing — a purge sweep that cannot purge must not report success.
func NewPlaidOrphanTokenPurgeWorker(deps PlaidOrphanTokenPurgeWorkerDeps) *PlaidOrphanTokenPurgeWorker {
	logger := deps.Logger
	if logger == nil {
		logger = slog.Default()
	}
	return &PlaidOrphanTokenPurgeWorker{
		repo:      deps.Repo,
		logger:    logger,
		maxPerRun: intOrDefault(deps.MaxPerRun, maxOrphanTokenPurgesPerSweep),
	}
}

// Work purges every retained credential whose deadline has passed, up to this run's cap.
//
// IT RETURNS AN ERROR ON FAILURE, unlike the item-cleanup sweep which logs and continues. The reason is
// the asymmetry between the two: a failed cleanup sweep costs Plaid fees and the next run recovers,
// whereas a failed purge means personal data is being retained past a documented deadline. River
// retrying it is the recovery, and a green job record for a run that purged nothing would be exactly
// the "state marked, work never happened" pattern this job exists to avoid.
func (w *PlaidOrphanTokenPurgeWorker) Work(ctx context.Context, job *river.Job[PlaidOrphanTokenPurgeArgs]) (err error) {
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

	if w.repo == nil {
		return fmt.Errorf("plaid orphan token purge: no repository wired; retained credentials are NOT being purged")
	}

	// One instant decides and stamps every purge in this run — see PurgeExpiredTokens on why asOf is a
	// parameter rather than the database's NOW().
	asOf := time.Now().UTC()

	purged, err := w.repo.PurgeExpiredTokens(ctx, asOf, w.maxPerRun)
	if err != nil {
		w.logger.Error("failed to purge expired retained Plaid credentials; they are still in the database past their retention deadline",
			"job_id", job.ID, "error", err)
		return fmt.Errorf("plaid orphan token purge: %w", err)
	}

	if purged == 0 {
		w.logger.Debug("no retained Plaid credentials were past their deadline",
			"job_id", job.ID, "trigger_source", job.Args.TriggerSource)
		return nil
	}

	// WARN, not Info: every row here is a Plaid Item that was NOT successfully removed within its
	// retention window, so each one has just become an item on an operator's manual to-do list at Plaid.
	w.logger.Warn("purged retained Plaid credentials at their retention deadline; the affected orphan records now require manual intervention in Plaid",
		"job_id", job.ID, "purged", purged, "trigger_source", job.Args.TriggerSource,
		"duration_ms", time.Since(start).Milliseconds())

	if purged >= w.maxPerRun {
		w.logger.Warn("plaid orphan token purge hit its per-run cap; the remainder is deferred to the next run, and a table this size is itself unexpected",
			"job_id", job.ID, "cap", w.maxPerRun)
	}
	return nil
}

// PlaidOrphanTokenPurgeUniqueOpts returns the uniqueness options this job's producers pass.
//
// Two concurrent purge runs would be harmless (the statement is idempotent — it requires
// access_token_encrypted IS NOT NULL), so this is about not queueing redundant work rather than about
// correctness. Completed is included because the periodic registration uses RunOnStart: true, so
// without it every deploy or leader election enqueues a duplicate of a run that finished minutes ago —
// the Issue #1448 lesson.
//
// ⚠ ByPeriod IS THE SWEEP INTERVAL AND MUST NOT EXCEED IT. Because Completed is in ByState, River
// discards any insert falling in the same period as a run that already finished — so a ByPeriod longer
// than cmd/server's registration interval silently throttles the sweep back to ByPeriod and the
// interval becomes decorative. It was 24 hours when the interval was; both now read
// domain.OrphanedItemTokenPurgeInterval, and TestBuildPeriodicJobSpecs_PurgeRunsHourlyAndItsDedupDoes-
// NotSuppressThat (cmd/server) fails if they diverge in that direction. This matters more here than for
// most jobs: this interval is the published upper bound on how long an erased user's bank credential
// can still exist past its retention deadline.
func PlaidOrphanTokenPurgeUniqueOpts() *queue.UniqueOpts {
	return &queue.UniqueOpts{
		ByArgs:   false,
		ByPeriod: domain.OrphanedItemTokenPurgeInterval,
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

// PlaidOrphanTokenPurgeInsertOpts returns the *river.InsertOpts the periodic registration must use.
func PlaidOrphanTokenPurgeInsertOpts() *river.InsertOpts {
	return riverInsertOptsFromUnique(queue.QueueLow, PlaidOrphanTokenPurgeUniqueOpts())
}

// PlaidOrphanTokenPurgePeriodicOpts returns the *river.PeriodicJobOpts the periodic registration must
// use. RunOnStart is safe only because PlaidOrphanTokenPurgeInsertOpts always carries the dedup above.
func PlaidOrphanTokenPurgePeriodicOpts() *river.PeriodicJobOpts {
	return &river.PeriodicJobOpts{RunOnStart: true, ID: PlaidOrphanTokenPurgeArgs{}.Kind()}
}
