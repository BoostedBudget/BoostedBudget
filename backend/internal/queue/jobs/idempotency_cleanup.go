package jobs

// IdempotencyCleanupWorker deletes expired idempotency_keys rows (24-hour TTL).
// Issue #1087 R4-7: migration 000086 promises periodic maintenance.
// Runs daily to prevent unbounded table growth.

import (
	"context"
	"log/slog"

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
