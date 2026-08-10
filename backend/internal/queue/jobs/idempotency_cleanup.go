package jobs

// IdempotencyCleanupWorker deletes expired idempotency_keys rows (24-hour TTL).
// Issue #1087 R4-7: migration 000086 promises periodic maintenance.
// Runs daily to prevent unbounded table growth.

import (
	"context"
	"log/slog"
	"time"

	"github.com/maxnussbaum/BoostedBudget/internal/queue"
	"github.com/riverqueue/river"
)

// IdempotencyCleanupRepository is the subset of IdempotencyRepository this worker needs.
type IdempotencyCleanupRepository interface {
	// DeleteExpired removes all rows past their expires_at timestamp.
	DeleteExpired(ctx context.Context) (int64, error)
}

// IdempotencyCleanupArgs contains arguments for the idempotency cleanup job.
// No parameters needed — the cleanup is always "delete WHERE expires_at < NOW()".
type IdempotencyCleanupArgs struct {
	// TriggerSource indicates how this job was triggered (e.g., "periodic", "manual").
	TriggerSource string `json:"trigger_source,omitempty"`
}

// Kind returns the unique identifier for this job type (used by River for routing).
func (IdempotencyCleanupArgs) Kind() string { return "idempotency_cleanup" }

// IdempotencyCleanupWorker deletes expired idempotency_keys rows.
type IdempotencyCleanupWorker struct {
	river.WorkerDefaults[IdempotencyCleanupArgs]
	repo   IdempotencyCleanupRepository
	logger *slog.Logger
}

// IdempotencyCleanupWorkerDeps contains the dependencies for IdempotencyCleanupWorker.
type IdempotencyCleanupWorkerDeps struct {
	Repo   IdempotencyCleanupRepository
	Logger *slog.Logger
}

// NewIdempotencyCleanupWorker creates a new IdempotencyCleanupWorker.
func NewIdempotencyCleanupWorker(deps IdempotencyCleanupWorkerDeps) *IdempotencyCleanupWorker {
	logger := deps.Logger
	if logger == nil {
		logger = slog.Default()
	}
	return &IdempotencyCleanupWorker{
		repo:   deps.Repo,
		logger: logger,
	}
}

// Work performs the idempotency key cleanup.
func (w *IdempotencyCleanupWorker) Work(ctx context.Context, job *river.Job[IdempotencyCleanupArgs]) error {
	w.logger.Info("idempotency cleanup job started",
		slog.String("trigger_source", job.Args.TriggerSource),
		slog.Int64("job_id", job.ID),
	)

	deleted, err := w.repo.DeleteExpired(ctx)
	if err != nil {
		w.logger.Error("idempotency cleanup failed",
			slog.String("error", err.Error()),
		)
		return err
	}

	w.logger.Info("idempotency cleanup completed",
		slog.Int64("rows_deleted", deleted),
	)
	return nil
}

// IdempotencyCleanupUniqueOpts returns the recommended uniqueness options.
// Only one cleanup job should run at a time — this job is naturally idempotent (delete WHERE
// expires_at < NOW() is a no-op the second time), but dedup still avoids duplicate lock
// contention / wasted table scans from an overlapping run.
//
// Issue #1448: this helper did not exist at all — main.go's periodic registration carried
// no dedup and RunOnStart: false, so this daily job could be enqueued zero times ever on a
// deploy cadence faster than its interval.
func IdempotencyCleanupUniqueOpts() *queue.UniqueOpts {
	return &queue.UniqueOpts{
		ByArgs: false, // All cleanup jobs are equivalent
		// 23 hours — avoids overlap on daily schedule
		ByPeriod: 23 * time.Hour,
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

// IdempotencyCleanupInsertOpts returns the *river.InsertOpts this job's periodic
// registration in main.go must use — Queue: "low" plus the uniqueness options from
// IdempotencyCleanupUniqueOpts(), converted to river's UniqueOpts type.
func IdempotencyCleanupInsertOpts() *river.InsertOpts {
	return riverInsertOptsFromUnique(queue.QueueLow, IdempotencyCleanupUniqueOpts())
}

// IdempotencyCleanupPeriodicOpts returns the *river.PeriodicJobOpts this job's periodic
// registration in main.go must use. RunOnStart: true is safe both because this job is
// naturally idempotent AND because IdempotencyCleanupInsertOpts() above carries
// IdempotencyCleanupUniqueOpts()'s dedup. ID is set so River restores nextRunAt across
// restarts/leader elections instead of recomputing now+interval every time (Issue #1448).
func IdempotencyCleanupPeriodicOpts() *river.PeriodicJobOpts {
	return &river.PeriodicJobOpts{RunOnStart: true, ID: IdempotencyCleanupArgs{}.Kind()}
}
