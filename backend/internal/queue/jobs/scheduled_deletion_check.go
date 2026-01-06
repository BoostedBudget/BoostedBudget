// Package jobs contains River job definitions and workers for async processing.
package jobs

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/queue"
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

// ProfileRepository defines the methods needed for deletion scheduling.
type ProfileRepository interface {
	// GetUsersScheduledForDeletion returns users whose deletion_scheduled_at has passed
	GetUsersScheduledForDeletion(ctx context.Context) ([]*domain.User, error)
}

// ScheduledDeletionCheckWorker processes scheduled deletion check jobs.
// This worker is the coordinator that finds users ready for deletion and
// enqueues individual deletion jobs for each.
type ScheduledDeletionCheckWorker struct {
	river.WorkerDefaults[ScheduledDeletionCheckArgs]

	profileRepo ProfileRepository
	jobQueue    queue.JobQueue
	logger      *slog.Logger
}

// ScheduledDeletionCheckWorkerDeps contains the dependencies for ScheduledDeletionCheckWorker.
type ScheduledDeletionCheckWorkerDeps struct {
	ProfileRepo ProfileRepository
	JobQueue    queue.JobQueue
	Logger      *slog.Logger
}

// NewScheduledDeletionCheckWorker creates a new scheduled deletion check worker.
// Note: JobQueue can be nil initially and set later via SetJobQueue() to handle
// circular dependency when configuring periodic jobs.
func NewScheduledDeletionCheckWorker(deps ScheduledDeletionCheckWorkerDeps) *ScheduledDeletionCheckWorker {
	logger := deps.Logger
	if logger == nil {
		logger = slog.Default()
	}
	return &ScheduledDeletionCheckWorker{
		profileRepo: deps.ProfileRepo,
		jobQueue:    deps.JobQueue,
		logger:      logger,
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
//  1. Fetches users scheduled for deletion (grace period expired)
//  2. Enqueues an AccountDeletionJob for each user
//  3. Uses unique constraints to prevent duplicate deletion jobs
//
// Errors in individual deletion enqueueing don't fail the entire job.
func (w *ScheduledDeletionCheckWorker) Work(ctx context.Context, job *river.Job[ScheduledDeletionCheckArgs]) error {
	args := job.Args

	w.logger.Info("starting scheduled deletion check",
		"job_id", job.ID,
		"trigger_source", args.TriggerSource,
		"attempt", job.Attempt,
	)

	startTime := time.Now()

	// Get users scheduled for deletion
	users, err := w.profileRepo.GetUsersScheduledForDeletion(ctx)
	if err != nil {
		w.logger.Error("failed to fetch users scheduled for deletion",
			"job_id", job.ID,
			"error", err,
		)
		return fmt.Errorf("failed to fetch users scheduled for deletion: %w", err)
	}

	if len(users) == 0 {
		w.logger.Info("no users scheduled for deletion",
			"job_id", job.ID,
			"duration_ms", time.Since(startTime).Milliseconds(),
		)
		return nil
	}

	w.logger.Info("found users scheduled for deletion",
		"job_id", job.ID,
		"user_count", len(users),
	)

	// Enqueue deletion job for each user
	var enqueuedCount, skippedCount, errorCount int
	for _, user := range users {
		deletionArgs := AccountDeletionArgs{
			UserID:    user.ID,
			RequestID: fmt.Sprintf("scheduled-check-%d-%d", job.ID, time.Now().Unix()),
		}

		uniqueOpts := AccountDeletionUniqueOpts()
		result, err := w.jobQueue.Enqueue(ctx, deletionArgs, queue.WithUnique(*uniqueOpts))
		if err != nil {
			w.logger.Error("failed to enqueue account deletion job",
				"job_id", job.ID,
				"user_id", user.ID,
				"error", err,
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

	w.logger.Info("scheduled deletion check completed",
		"job_id", job.ID,
		"total_users", len(users),
		"enqueued", enqueuedCount,
		"skipped", skippedCount,
		"errors", errorCount,
		"duration_ms", time.Since(startTime).Milliseconds(),
	)

	// Don't fail the job if some enqueuings failed - they'll be retried next check
	return nil
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
		},
	}
}
