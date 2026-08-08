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

// ScheduledDeletionCheckArgs contains the arguments for the scheduled deletion check job.
// This job runs periodically to find users whose grace period has expired and
// enqueues individual AccountDeletionJob for each.
type ScheduledDeletionCheckArgs struct {
	// TriggerSource indicates what triggered this check (e.g., "periodic", "manual")
	TriggerSource string `json:"trigger_source,omitempty"`
}

// Kind returns the unique identifier for this job type.
func (ScheduledDeletionCheckArgs) Kind() string { return "scheduled_deletion_check" }

// scheduledDeletionCheckPageSize bounds each GetUsersScheduledForDeletionPage query (Batch S2
// Task 4) — see that method's doc comment (profile_repository.go) for why an unbounded single
// query is unsafe now that #1440 made this a real hard delete and #1448 gave its caller
// RunOnStart: true. Work() below loops pages of this size within a single run until exhausted,
// so bounding the per-query result set does NOT, by itself, bound how many eligible users get an
// enqueue attempt in one run — it only bounds how much memory/row-count a single query call
// handles at once. The total across every page in one run is bounded separately, by
// maxUsersPerDeletionSweep below.
const scheduledDeletionCheckPageSize = 500

// maxUsersPerDeletionSweep bounds the TOTAL number of users processed across every page
// fetched within ONE Work() run (see the pagination loop below) — the safety cap
// GetUsersScheduledForDeletionPage's doc comment (profile_repository.go) promises but, until
// this fix, was never implemented: each page query was bounded to pageSize rows, but the loop
// around it kept paging until a page came back short, so a large enough backlog made ONE
// run's total work unbounded. 50,000 is chosen well above any plausible one-run backlog
// while still bounding blast radius — at pageSize=500 that is up to 100 page round-trips,
// already far more than a healthy run should ever need. Hitting the cap does NOT reschedule
// or retry anything; it only stops THIS run early and logs a WARN naming how many users were
// processed. This is not silent truncation: the existing periodic schedule
// (ScheduledDeletionCheckPeriodicOpts) already re-runs this job on its own cadence, and since
// Issue #1477 that next run RESUMES where this one stopped rather than restarting oldest-first —
// Work() below persists the timestamp it got to via sweep_cursors
// (repository/sweep_cursor_repository.go) whenever, and only whenever, it stops early on this cap.
// So users ranked past this run's cap are reached on the next run regardless of whether the ones
// this run enqueued ever complete. Before #1477 they were reached only once those completed, which
// meant never if any of them permanently failed. See Work()'s doc comment below,
// repository/sweep_cursor_repository.go for the guarantee stated with its bound, and
// GetUsersScheduledForDeletionPage's doc comment (profile_repository.go) for the query side.
const maxUsersPerDeletionSweep = 50000

// ProfileRepository defines the methods needed for deletion scheduling.
type ProfileRepository interface {
	// GetUsersScheduledForDeletionPage returns up to `limit` users whose deletion_scheduled_at
	// has passed, ordered deterministically for keyset pagination (Batch S2 Task 4). after == nil
	// starts from the beginning; pass the cursor derived from the last row of the previous page
	// to continue. See repository.ProfileRepository.GetUsersScheduledForDeletionPage's doc
	// comment for the full starvation rationale.
	GetUsersScheduledForDeletionPage(ctx context.Context, after *repository.DeletionSweepCursor, limit int) ([]*domain.User, error)
}

// ScheduledDeletionCheckWorker processes scheduled deletion check jobs.
// This worker is the coordinator that finds users ready for deletion and
// enqueues individual deletion jobs for each.
type ScheduledDeletionCheckWorker struct {
	river.WorkerDefaults[ScheduledDeletionCheckArgs]

	profileRepo ProfileRepository
	jobQueue    queue.JobQueue
	logger      *slog.Logger
	pageSize    int
	maxUsers    int
	// sweepCursors persists where a capped run stopped so the NEXT run resumes there instead of
	// restarting from the oldest row (Issue #1477). Optional/nil-safe: a nil store reproduces the
	// pre-#1477 behaviour exactly. See sweep_resume.go and repository/sweep_cursor_repository.go.
	sweepCursors sweepCursorStore
}

// ScheduledDeletionCheckWorkerDeps contains the dependencies for ScheduledDeletionCheckWorker.
type ScheduledDeletionCheckWorkerDeps struct {
	ProfileRepo ProfileRepository
	JobQueue    queue.JobQueue
	Logger      *slog.Logger
	// PageSize overrides scheduledDeletionCheckPageSize (Batch S2 Task 4). Zero/unset uses the
	// production default. Exists so tests can exercise the multi-page keyset-cursor loop without
	// seeding hundreds of fixture users — production should never set this.
	PageSize int
	// MaxUsersPerSweep overrides maxUsersPerDeletionSweep. Zero/unset uses the production
	// default. Exists so tests can exercise the total-processed safety cap without seeding
	// tens of thousands of fixture users — production should never set this.
	MaxUsersPerSweep int
	// SweepCursors persists this sweep's cross-run resume position (Issue #1477). Optional and
	// nil-safe: unwired, every run restarts from the oldest eligible row, which is exactly the
	// pre-#1477 starvation-prone behaviour — production MUST wire it (main.go).
	SweepCursors repository.SweepCursorRepository
}

// NewScheduledDeletionCheckWorker creates a new scheduled deletion check worker.
// Note: JobQueue can be nil initially and set later via SetJobQueue() to handle
// circular dependency when configuring periodic jobs.
func NewScheduledDeletionCheckWorker(deps ScheduledDeletionCheckWorkerDeps) *ScheduledDeletionCheckWorker {
	logger := deps.Logger
	if logger == nil {
		logger = slog.Default()
	}
	pageSize := deps.PageSize
	if pageSize <= 0 {
		pageSize = scheduledDeletionCheckPageSize
	}
	maxUsers := deps.MaxUsersPerSweep
	if maxUsers <= 0 {
		maxUsers = maxUsersPerDeletionSweep
	}
	w := &ScheduledDeletionCheckWorker{
		profileRepo: deps.ProfileRepo,
		jobQueue:    deps.JobQueue,
		logger:      logger,
		pageSize:    pageSize,
		maxUsers:    maxUsers,
	}
	// Guarded rather than assigned directly: a nil repository.SweepCursorRepository stored into the
	// narrower sweepCursorStore field would still be a nil interface, but writing it explicitly
	// keeps "unwired means nil, and nil means do not persist" true by inspection.
	if deps.SweepCursors != nil {
		w.sweepCursors = deps.SweepCursors
	}
	return w
}

// SetJobQueue sets the job queue after worker creation.
// This is needed because of circular dependency: worker must be registered
// before client creation, but needs client to enqueue jobs.
func (w *ScheduledDeletionCheckWorker) SetJobQueue(jq queue.JobQueue) {
	w.jobQueue = jq
}

// Work processes a scheduled deletion check job.
//
// The worker:
//  1. Fetches users scheduled for deletion (grace period expired), one bounded page at a time
//  2. Enqueues an AccountDeletionJob for each user
//  3. Uses unique constraints to prevent duplicate deletion jobs
//  4. Advances a keyset cursor and repeats until a page comes back short (exhausted), OR the
//     total processed reaches maxUsersPerDeletionSweep — Batch S2 Task 4: bounding each
//     individual query only bounds that ONE page's size, not the total processed across every
//     page fetched in this run; the loop below re-queries until exhaustion or the cap, so a
//     permanently-stuck oldest user cannot starve later users out of every page forever (see
//     GetUsersScheduledForDeletionPage's doc comment). Hitting the cap logs a WARN and stops this
//     run early; it does not schedule anything new. Issue #1477: it also PERSISTS the timestamp
//     this run got to (sweep_cursors, via sweepResume.finish below), so the next periodic run
//     resumes there instead of restarting from the oldest still-eligible row. A run that instead
//     reaches the end of the candidate set CLEARS that position, and a run whose persisted
//     position turns out to be past the end wraps to the beginning within the same run — the two
//     resets that stop a forward-only cursor from silently stalling the sweep forever. The
//     guarantee this buys, and its bound, are stated once in
//     repository/sweep_cursor_repository.go's header; do not restate it here as "no starvation".
//
// Errors in individual deletion enqueueing don't fail the entire job.
func (w *ScheduledDeletionCheckWorker) Work(ctx context.Context, job *river.Job[ScheduledDeletionCheckArgs]) (err error) {
	args := job.Args

	w.logger.Info("starting scheduled deletion check",
		"job_id", job.ID,
		"trigger_source", args.TriggerSource,
		"attempt", job.Attempt,
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

	// Issue #1477: resume where the last CAPPED run stopped, instead of restarting from the oldest
	// row every time. See sweep_resume.go and repository/sweep_cursor_repository.go for the design,
	// the two reset rules, and the starvation guarantee stated with its bound.
	resume := newSweepResume(ctx, w.sweepCursors, repository.SweepCursorScheduledDeletion, w.logger, job.ID)
	cursor := deletionCursorFrom(resume.start)
	var reached repository.SweepPosition
	cappedStop := false

	var totalUsers, enqueuedCount, skippedCount, errorCount int

	for {
		users, pageErr := w.profileRepo.GetUsersScheduledForDeletionPage(ctx, cursor, w.pageSize)
		if pageErr != nil {
			w.logger.Error("failed to fetch users scheduled for deletion",
				"job_id", job.ID,
				"error", pageErr,
			)
			return fmt.Errorf("failed to fetch users scheduled for deletion: %w", pageErr)
		}

		if len(users) == 0 {
			// An empty page at a RESUMED position means the persisted cursor is past the end, not
			// that there is no work: clear it and re-query from the beginning within this same run.
			if resume.wrapNow(ctx, totalUsers) {
				cursor = nil
				continue
			}
			break
		}

		// fetchedCount is the page's length AS RETURNED by the repository, before any
		// maxUsersPerDeletionSweep truncation below — the only reliable signal for whether the
		// repository itself is exhausted (fetchedCount < w.pageSize means there was nothing left
		// to return, regardless of the cap).
		fetchedCount := len(users)

		// maxUsersPerDeletionSweep: if this page would push totalUsers to or past the cap,
		// truncate it now so the enqueue loop below never processes more than the cap allows —
		// "at most maxUsers enqueued", not "cap-or-more".
		if remaining := w.maxUsers - totalUsers; fetchedCount >= remaining {
			// remaining <= 0 only if totalUsers already reached the cap on a prior
			// iteration, which the break at the bottom of this loop prevents from
			// happening — guarded explicitly anyway, since users[:remaining] would
			// panic on a negative slice length.
			if remaining <= 0 {
				break
			}
			users = users[:remaining]
		}

		totalUsers += len(users)
		for _, user := range users {
			deletionArgs := AccountDeletionArgs{
				UserID: user.ID,
				// Batch S2 Task 3: RequestID is deterministic PER USER — no enqueuing job.ID, no
				// wall-clock component — so it stays byte-identical across every separate periodic
				// check run that finds the same still-eligible user. AccountDeletionUniqueOpts()
				// dedups via ByArgs: true with NO ByPeriod (account_deletion.go), so River's
				// uniqueness constraint is only as good as whether two enqueues' full Args actually
				// match. The previous "scheduled-check-%d-%d" (job.ID, time.Now().Unix()) form
				// differed on every single enqueue, defeating that dedup unconditionally — see
				// TestScheduledDeletionCheckWorker_Work_SameUserAcrossRuns_ProducesIdenticalArgs.
				RequestID: fmt.Sprintf("scheduled-check-%s", user.ID),
			}

			uniqueOpts := AccountDeletionUniqueOpts()
			result, enqueueErr := w.jobQueue.Enqueue(ctx, deletionArgs, queue.WithUnique(*uniqueOpts))
			if enqueueErr != nil {
				w.logger.Error("failed to enqueue account deletion job",
					"job_id", job.ID,
					"user_id", user.ID,
					"error", enqueueErr,
				)
				errorCount++
				continue
			}

			if result.UniqueSkipped {
				w.logger.Debug("account deletion job already queued, skipping",
					"user_id", user.ID,
				)
				skippedCount++
			} else {
				w.logger.Info("enqueued account deletion job",
					"user_id", user.ID,
					"deletion_job_id", result.ID,
				)
				enqueuedCount++
			}
		}

		last := users[len(users)-1]
		lastScheduledAt := derefDeletionScheduledAt(last)
		cursor = &repository.DeletionSweepCursor{
			ScheduledAt: lastScheduledAt,
			UserID:      last.ID,
		}
		// reached records only the TIMESTAMP half of that position — never last.ID. That is the
		// whole reason sweep_cursors can outlive a GDPR erasure of the very user this sweep is
		// about to delete: see repository/sweep_cursor_repository.go's header.
		reached = repository.SweepPosition{TS1: &lastScheduledAt}

		// exhausted mirrors the pre-existing short-page check below, but computed from
		// fetchedCount (pre-truncation) so it still reflects what the REPOSITORY actually told
		// us, even on the page that also happens to trip the cap.
		exhausted := fetchedCount < w.pageSize

		// maxUsersPerDeletionSweep hit: stop THIS run now — but only warn if the repository
		// itself did NOT already tell us it was exhausted (fetchedCount == w.pageSize, a full
		// page). If a genuinely short/final page happens to land exactly on the cap, there is no
		// real remainder to defer, and warning "deferring remainder" would be a false alarm; the
		// unconditional `if exhausted { break }` below already covers that case correctly.
		//
		// No new scheduling is needed for the true-remainder case: the existing periodic
		// registration (ScheduledDeletionCheckPeriodicOpts) already re-runs this job on its own
		// cadence, and that later run's pagination starts over from the oldest still-eligible
		// row — which reaches users past this run's cap once the ones already enqueued here are
		// actually deleted (or, if not yet deleted, are cheaply skipped again via
		// AccountDeletionUniqueOpts' dedup rather than double-processed). The WARN below is what
		// keeps this from reading as silent truncation.
		if totalUsers >= w.maxUsers && !exhausted {
			w.logger.Warn("scheduled deletion sweep hit its total-processed safety cap; deferring remainder to next periodic run",
				"job_id", job.ID,
				"processed", totalUsers,
				"cap", w.maxUsers,
			)
			cappedStop = true
			break
		}

		// A short page means this was the last one — no need for another round-trip to confirm.
		if exhausted {
			break
		}
	}

	// Persist the remainder position (cap hit) or clear it (end of set reached) — Issue #1477.
	resume.finish(ctx, cappedStop, reached)

	if totalUsers == 0 {
		w.logger.Info("no users scheduled for deletion",
			"job_id", job.ID,
			"duration_ms", time.Since(startTime).Milliseconds(),
		)
		return nil
	}

	w.logger.Info("scheduled deletion check completed",
		"job_id", job.ID,
		"total_users", totalUsers,
		"enqueued", enqueuedCount,
		"skipped", skippedCount,
		"errors", errorCount,
		"duration_ms", time.Since(startTime).Milliseconds(),
	)

	// Don't fail the job if some enqueuings failed - they'll be retried next check
	return nil
}

// derefDeletionScheduledAt safely reads user.DeletionScheduledAt for cursor advancement.
// GetUsersScheduledForDeletionPage's WHERE clause guarantees every returned row has a non-nil
// DeletionScheduledAt, but this stays defensive rather than assuming that invariant holds forever
// (e.g. under a future test double that doesn't enforce it) — falling back to the zero time
// would simply make the cursor's ScheduledAt component overly permissive, never miss real rows.
func derefDeletionScheduledAt(user *domain.User) time.Time {
	if user.DeletionScheduledAt == nil {
		return time.Time{}
	}
	return *user.DeletionScheduledAt
}

// ScheduledDeletionCheckUniqueOpts returns the recommended uniqueness options.
// Only one check job should run at a time to prevent duplicate deletion processing.
func ScheduledDeletionCheckUniqueOpts() *queue.UniqueOpts {
	return &queue.UniqueOpts{
		ByArgs: false, // All check jobs are equivalent
		// 1 hour period - matches typical check frequency
		ByPeriod: 1 * time.Hour,
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

// ScheduledDeletionCheckInsertOpts returns the *river.InsertOpts this job's periodic
// registration in main.go must use — Queue: "low" plus the uniqueness options from
// ScheduledDeletionCheckUniqueOpts(), converted to river's UniqueOpts type.
//
// Issue #1448: main.go's periodic registration previously built a bare
// &river.InsertOpts{Queue: "low"} and never called ScheduledDeletionCheckUniqueOpts() at
// all — even though its PeriodicJobOpts already set RunOnStart: true. RunOnStart without a
// paired dedup guarantee is exactly the combination this package's retention/manual-tier
// override helpers document as unsafe: a start-of-process catch-up insert can double-fire
// alongside an already-scheduled run, re-running an account-deletion sweep on every
// restart or leader election.
func ScheduledDeletionCheckInsertOpts() *river.InsertOpts {
	return riverInsertOptsFromUnique(queue.QueueLow, ScheduledDeletionCheckUniqueOpts())
}

// ScheduledDeletionCheckPeriodicOpts returns the *river.PeriodicJobOpts this job's periodic
// registration in main.go must use. RunOnStart: true is only safe because
// ScheduledDeletionCheckInsertOpts() above always carries ScheduledDeletionCheckUniqueOpts()'s
// dedup. ID is set so River restores nextRunAt across restarts/leader elections instead of
// recomputing now+interval every time (river@v0.38.0
// internal/maintenance/periodic_job_enqueuer.go:409-414) — Issue #1448.
func ScheduledDeletionCheckPeriodicOpts() *river.PeriodicJobOpts {
	return &river.PeriodicJobOpts{RunOnStart: true, ID: ScheduledDeletionCheckArgs{}.Kind()}
}
