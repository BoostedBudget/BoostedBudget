// Package jobs contains River job definitions and workers for async processing.
package jobs

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/maxnussbaum/BoostedBudget/internal/observability"
	"github.com/maxnussbaum/BoostedBudget/internal/queue"
	"github.com/maxnussbaum/BoostedBudget/internal/services"
	"github.com/riverqueue/river"
)

// AccountDeletionArgs contains the arguments for an account deletion job.
// This job permanently deletes a user's account and all associated data
// after the GDPR grace period has passed.
type AccountDeletionArgs struct {
	// UserID is the ID of the user to delete
	UserID string `json:"user_id"`

	// RequestID is the unique identifier for the deletion request (for audit)
	RequestID string `json:"request_id,omitempty"`
}

// Kind returns the unique identifier for this job type.
func (AccountDeletionArgs) Kind() string { return "account_deletion" }

// AccountDeletionWorker processes account deletion jobs.
// This worker handles the final deletion of user accounts after the grace period.
type AccountDeletionWorker struct {
	river.WorkerDefaults[AccountDeletionArgs]

	deletionService services.DeletionService
	logger          *slog.Logger
}

// AccountDeletionWorkerDeps contains the dependencies for AccountDeletionWorker.
type AccountDeletionWorkerDeps struct {
	DeletionService services.DeletionService
	Logger          *slog.Logger
}

// NewAccountDeletionWorker creates a new account deletion worker.
func NewAccountDeletionWorker(deps AccountDeletionWorkerDeps) *AccountDeletionWorker {
	logger := deps.Logger
	if logger == nil {
		logger = slog.Default()
	}
	return &AccountDeletionWorker{
		deletionService: deps.DeletionService,
		logger:          logger,
	}
}

// Work processes an account deletion job.
//
// The worker:
//  1. Validates the user is still scheduled for deletion (not cancelled)
//  2. Deletes all user data (transactions, accounts, budgets, rules, etc.)
//  3. Deletes the user profile
//  4. Logs the deletion for audit compliance
//
// This is a critical operation - failures will be retried with exponential backoff.
// After max attempts, the job moves to dead letter queue for manual review.
func (w *AccountDeletionWorker) Work(ctx context.Context, job *river.Job[AccountDeletionArgs]) (err error) {
	args := job.Args

	w.logger.Info("starting account deletion job",
		"job_id", job.ID,
		"user_id", args.UserID,
		"request_id", args.RequestID,
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

	startTime := time.Now()

	// Execute the deletion through the service layer
	// This handles all cascading deletions and audit logging
	err = w.deletionService.ExecuteDeletion(ctx, args.UserID)
	if err != nil {
		w.logger.Error("account deletion failed",
			"job_id", job.ID,
			"user_id", args.UserID,
			"request_id", args.RequestID,
			"error", err,
			"duration_ms", time.Since(startTime).Milliseconds(),
		)
		return fmt.Errorf("failed to delete account: %w", err)
	}

	w.logger.Info("account deletion completed",
		"job_id", job.ID,
		"user_id", args.UserID,
		"request_id", args.RequestID,
		"duration_ms", time.Since(startTime).Milliseconds(),
	)

	return nil
}

// AccountDeletionUniqueOpts returns the recommended uniqueness options: intended to keep only one
// deletion job per user queued/running at a time.
//
// Batch S2 Task 3 correction: before this task, the SOLE production caller
// (ScheduledDeletionCheckWorker.Work, scheduled_deletion_check.go) built AccountDeletionArgs with
// a RequestID embedding the enqueuing job's own river job.ID and time.Now().Unix() — both differ
// on every single enqueue. Since ByArgs (below) is true with NO ByPeriod, River can only dedup
// two enqueues whose full Args are byte-identical; a RequestID that changes every time defeats
// this uniqueness constraint UNCONDITIONALLY, for every user, not as an edge case. Fixed by
// making RequestID a deterministic function of the user alone
// (see TestScheduledDeletionCheckWorker_Work_SameUserAcrossRuns_ProducesIdenticalArgs).
//
// Severity of the pre-fix bug was Medium, not Critical: ExecuteDeletion (deletion_service.go)
// acquires an independent work-claims lease (ActionAccountDeletion) BEFORE any destructive or
// external work, and ClaimForDeletion (profile_repository.go) atomically re-verifies eligibility
// at claim time — so a genuinely concurrent duplicate job blocks on the lease and a
// sequential duplicate (running after the first released its lease) simply no-ops rather than
// double-deleting. The real cost of the pre-fix bug was duplicate queued work, repeated
// external-service calls (Paddle cancel, Plaid disconnect — both run BEFORE the lease is released,
// so a duplicate whose lease-acquisition window doesn't overlap the first attempt's can still
// re-run them) per the interface doc on DeleteUserCascade, and log/error noise — never a second
// hard delete.
func AccountDeletionUniqueOpts() *queue.UniqueOpts {
	return &queue.UniqueOpts{
		ByArgs: true,
		// No time period - each user can only have one pending deletion
		ByState: []queue.JobState{
			queue.JobStateAvailable,
			queue.JobStateScheduled,
			queue.JobStateRunning,
			queue.JobStateRetryable,
			queue.JobStatePending,
		},
	}
}
