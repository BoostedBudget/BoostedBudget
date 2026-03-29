// Package queue provides a job queue abstraction for async processing.
//
// This package defines interfaces for enqueueing and processing background jobs.
// The primary implementation uses River (PostgreSQL-backed), but the abstraction
// allows for future provider changes if needed.
//
// # Architecture
//
// The queue system follows a job/worker pattern:
//   - Jobs: Define the work to be done (args + kind)
//   - Workers: Execute jobs of a specific kind
//   - Queue: Routes jobs to appropriate workers
//
// # Usage
//
//	// Define a job
//	type EmailNotificationArgs struct {
//	    UserID  string `json:"user_id"`
//	    Subject string `json:"subject"`
//	    Body    string `json:"body"`
//	}
//
//	func (EmailNotificationArgs) Kind() string { return "email_notification" }
//
//	// Enqueue a job
//	result, err := queue.Enqueue(ctx, EmailNotificationArgs{
//	    UserID:  "user123",
//	    Subject: "Budget Alert",
//	    Body:    "Your grocery budget is at 90%",
//	}, queue.WithQueue("notifications"))
//
// # Transaction Safety
//
// Jobs can be enqueued within database transactions using EnqueueTx.
// This ensures atomicity - if the transaction rolls back, the job is not created.
//
//	tx, _ := pool.Begin(ctx)
//	defer tx.Rollback(ctx)
//
//	// Create entity
//	_, err = tx.Exec(ctx, "INSERT INTO transactions ...")
//
//	// Enqueue follow-up job atomically
//	_, err = queue.EnqueueTx(ctx, tx, CategorizeTransactionsArgs{...})
//
//	tx.Commit(ctx)
package queue

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"
)

// Job represents a unit of work to be processed asynchronously.
// Each job type must implement this interface and provide a unique Kind identifier.
type Job interface {
	// Kind returns a unique string identifier for this job type.
	// This is used to route jobs to the appropriate worker.
	// Example: "plaid_sync", "categorize_transactions", "send_notification"
	Kind() string
}

// JobResult contains information about an enqueued job.
type JobResult struct {
	// ID is the unique identifier for this job instance
	ID int64

	// Kind is the job type identifier
	Kind string

	// Queue is the name of the queue the job was added to
	Queue string

	// State is the current state of the job
	State JobState

	// ScheduledAt is when the job is scheduled to run
	ScheduledAt time.Time

	// UniqueSkipped is true if the job was deduplicated due to uniqueness constraints
	UniqueSkipped bool
}

// JobState represents the current state of a job.
type JobState string

const (
	// JobStateAvailable means the job is ready to be worked
	JobStateAvailable JobState = "available"

	// JobStatePending means the job is waiting for dependencies
	JobStatePending JobState = "pending"

	// JobStateScheduled means the job is scheduled for future execution
	JobStateScheduled JobState = "scheduled"

	// JobStateRunning means the job is currently being processed
	JobStateRunning JobState = "running"

	// JobStateRetryable means the job failed but can be retried
	JobStateRetryable JobState = "retryable"

	// JobStateCompleted means the job finished successfully
	JobStateCompleted JobState = "completed"

	// JobStateCancelled means the job was cancelled before completion
	JobStateCancelled JobState = "cancelled"

	// JobStateDiscarded means the job failed and will not be retried
	JobStateDiscarded JobState = "discarded"
)

// JobQueue is the primary interface for enqueueing jobs.
// It provides methods for adding jobs to the queue, both standalone and within transactions.
type JobQueue interface {
	// Enqueue adds a job to the queue for processing.
	// The job will be processed as soon as a worker is available, unless
	// scheduling options are provided.
	//
	// Options can be used to customize queue, priority, scheduling, etc.
	// See EnqueueOption for available options.
	Enqueue(ctx context.Context, job Job, opts ...EnqueueOption) (*JobResult, error)

	// EnqueueTx adds a job within a database transaction.
	// This ensures the job is only created if the transaction commits.
	// Use this when you need atomicity between database operations and job creation.
	EnqueueTx(ctx context.Context, tx pgx.Tx, job Job, opts ...EnqueueOption) (*JobResult, error)

	// EnqueueMany adds multiple jobs to the queue in a single operation.
	// This is more efficient than calling Enqueue multiple times.
	// All jobs share the same options.
	EnqueueMany(ctx context.Context, jobs []Job, opts ...EnqueueOption) ([]*JobResult, error)

	// EnqueueManyTx adds multiple jobs within a database transaction.
	EnqueueManyTx(ctx context.Context, tx pgx.Tx, jobs []Job, opts ...EnqueueOption) ([]*JobResult, error)
}

// JobRunner manages the lifecycle of job processing workers.
// It handles starting workers, graceful shutdown, and job execution.
type JobRunner interface {
	// Start begins processing jobs from all configured queues.
	// This method blocks until Stop is called or an error occurs.
	// Workers are started for all registered job types.
	Start(ctx context.Context) error

	// Stop gracefully shuts down the job runner.
	// It waits for in-progress jobs to complete (up to the configured timeout)
	// before returning. Use StopAndCancel for immediate shutdown.
	Stop(ctx context.Context) error

	// StopAndCancel immediately stops the job runner, canceling in-progress jobs.
	// Use this when you need to shut down quickly (e.g., during deployment).
	// In-progress jobs will be returned to the queue for retry.
	StopAndCancel(ctx context.Context) error
}

// UniqueOpts controls job deduplication.
// When unique options are set, only one job matching the criteria can exist
// in the specified states at a time.
type UniqueOpts struct {
	// ByArgs deduplicates jobs with identical arguments.
	// Two jobs with the same Kind and Args will be considered duplicates.
	ByArgs bool

	// ByPeriod sets a time window for uniqueness.
	// Jobs are considered duplicates only if enqueued within this period.
	// Example: 5*time.Minute means only one job per 5 minutes.
	ByPeriod time.Duration

	// ByQueue restricts uniqueness to the same queue.
	// Without this, uniqueness is global across all queues.
	ByQueue bool

	// ByState specifies which states to consider for uniqueness.
	// Defaults to all non-terminal states (available, scheduled, running, retryable, pending).
	ByState []JobState
}

// EnqueueOptions holds all options that can be set when enqueueing a job.
type EnqueueOptions struct {
	// Queue specifies which queue to add the job to.
	// Defaults to "default" if not specified.
	Queue string

	// Priority sets the job's priority (1-4, where 1 is highest).
	// Higher priority jobs are processed first within the same queue.
	Priority int

	// ScheduledAt sets when the job should become available for processing.
	// If not set, the job is available immediately.
	ScheduledAt time.Time

	// MaxAttempts overrides the default maximum retry attempts.
	// Set to 1 for jobs that should not be retried.
	MaxAttempts int

	// Unique sets uniqueness constraints for deduplication.
	Unique *UniqueOpts

	// Tags are arbitrary strings attached to the job for filtering/querying.
	Tags []string

	// Metadata is arbitrary JSON data attached to the job.
	// Useful for tracking, debugging, or passing context.
	Metadata map[string]any
}

// EnqueueOption is a functional option for configuring job enqueueing.
type EnqueueOption func(*EnqueueOptions)
