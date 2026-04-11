package jobs

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/maxnussbaum/BoostedBudget/internal/observability"
	"github.com/maxnussbaum/BoostedBudget/internal/queue"
	"github.com/riverqueue/river"
)

// DLQCleanupCheckArgs contains arguments for the DLQ cleanup job.
// This job runs daily and deletes dead letter entries older than retentionDays,
// skipping entries that have admin notes (they may be under investigation).
type DLQCleanupCheckArgs struct {
	// TriggerSource indicates what triggered this check (e.g., "periodic", "manual")
	TriggerSource string `json:"trigger_source,omitempty"`
}

// Kind returns the unique identifier for this job type.
func (DLQCleanupCheckArgs) Kind() string { return "dlq_cleanup_check" }

// DLQCleanupRepository defines the repository methods the cleanup worker needs.
type DLQCleanupRepository interface {
	// CleanupOldJobs deletes DLQ entries older than retentionDays, skipping entries with notes.
	CleanupOldJobs(ctx context.Context, retentionDays int) (int64, error)
}

// DLQCleanupCheckWorker processes DLQ cleanup check jobs.
type DLQCleanupCheckWorker struct {
	river.WorkerDefaults[DLQCleanupCheckArgs]

	repo          DLQCleanupRepository
	retentionDays int
	logger        *slog.Logger
}

// DLQCleanupCheckWorkerDeps contains the dependencies for DLQCleanupCheckWorker.
type DLQCleanupCheckWorkerDeps struct {
	Repo          DLQCleanupRepository
	RetentionDays int
	Logger        *slog.Logger
}

// NewDLQCleanupCheckWorker creates a new DLQ cleanup worker.
func NewDLQCleanupCheckWorker(deps DLQCleanupCheckWorkerDeps) *DLQCleanupCheckWorker {
	logger := deps.Logger
	if logger == nil {
		logger = slog.Default()
	}
	retentionDays := deps.RetentionDays
	if retentionDays <= 0 {
		retentionDays = 30 // Default: 30-day retention
	}
	return &DLQCleanupCheckWorker{
		repo:          deps.Repo,
		retentionDays: retentionDays,
		logger:        logger,
	}
}

// Work processes a DLQ cleanup job, deleting old entries and logging the result.
func (w *DLQCleanupCheckWorker) Work(ctx context.Context, job *river.Job[DLQCleanupCheckArgs]) (err error) {
	w.logger.Info("starting DLQ cleanup check",
		"job_id", job.ID,
		"trigger_source", job.Args.TriggerSource,
		"retention_days", w.retentionDays,
		"attempt", job.Attempt,
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

	count, err := w.repo.CleanupOldJobs(ctx, w.retentionDays)
	if err != nil {
		return fmt.Errorf("DLQ cleanup failed: %w", err)
	}

	if count > 0 {
		w.logger.Info("DLQ cleanup completed",
			"job_id", job.ID,
			"deleted_count", count,
			"retention_days", w.retentionDays,
		)
	} else {
		w.logger.Info("DLQ cleanup: no old entries to delete", "job_id", job.ID)
	}

	return nil
}

// DLQCleanupUniqueOpts returns recommended uniqueness options for the cleanup job.
// Only one cleanup job should run at a time to prevent duplicate deletions.
func DLQCleanupUniqueOpts() *queue.UniqueOpts {
	return &queue.UniqueOpts{
		ByArgs: false,
		// 23 hours — avoids overlap on daily schedule
		ByPeriod: 23 * time.Hour,
		ByState: []queue.JobState{
			queue.JobStateAvailable,
			queue.JobStateScheduled,
			queue.JobStateRunning,
			queue.JobStateRetryable,
			queue.JobStatePending,
		},
	}
}
