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
// so bounding the per-query result set does NOT bound how many eligible users get an enqueue
// attempt in one run — it only bounds how much memory/row-count a single query call handles at
// once.
const scheduledDeletionCheckPageSize = 500

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
	return &ScheduledDeletionCheckWorker{
		profileRepo: deps.ProfileRepo,
		jobQueue:    deps.JobQueue,
		logger:      logger,
		pageSize:    pageSize,
	}
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
//  4. Advances a keyset cursor and repeats until a page comes back short (exhausted) — Batch S2
//     Task 4: bounding each individual query does NOT bound the total users processed in one
//     run, so a permanently-stuck oldest user cannot starve later users out of every batch
//     forever (see GetUsersScheduledForDeletionPage's doc comment).
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

	var cursor *repository.DeletionSweepCursor
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
			break
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
		cursor = &repository.DeletionSweepCursor{
			ScheduledAt: derefDeletionScheduledAt(last),
			UserID:      last.ID,
		}

		// A short page means this was the last one — no need for another round-trip to confirm.
		if len(users) < w.pageSize {
			break
		}
	}

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
