---
name: queueing
description: "River job queue patterns for BoostedBudget: job definitions, workers, periodic jobs, dead letter handling, and testing."
---

# River Job Queue Guide

## Architecture Overview

```
internal/queue/
├── queue.go                    # Core interfaces (JobQueue, Job, options)
├── options.go                  # EnqueueOption helpers
├── jobs/
│   ├── plaid_sync.go           # PlaidSyncArgs + PlaidSyncWorker
│   ├── webhook_processing.go   # WebhookProcessingArgs + Worker
│   ├── rule_evaluation.go      # RuleEvaluationArgs + Worker
│   ├── notification_email.go   # NotificationEmailArgs + Worker
│   ├── account_deletion.go     # AccountDeletionArgs + Worker
│   └── scheduled_deletion_check.go  # Periodic coordinator job
└── river/
    ├── client.go               # River implementation of JobQueue
    ├── config.go               # Queue and retry configuration
    └── dead_letter.go          # ErrorHandler for failed jobs
```

## Job Definition Pattern

### Args Structure

Every job type needs an Args struct implementing `river.JobArgs`:

```go
// internal/queue/jobs/my_job.go
package jobs

import (
    "github.com/riverqueue/river"
)

// MyJobArgs contains the arguments for the job.
type MyJobArgs struct {
    UserID    string `json:"user_id"`
    ResourceID string `json:"resource_id"`
    Priority   int    `json:"priority,omitempty"`
}

// Kind returns the unique identifier for this job type.
// Convention: lowercase with underscores
func (MyJobArgs) Kind() string { return "my_job" }
```

### Worker Structure

Workers process jobs with a standard pattern:

```go
// MyJobWorker processes my jobs.
type MyJobWorker struct {
    river.WorkerDefaults[MyJobArgs]  // Embed for default behavior

    service  services.MyService      // Dependencies
    logger   *slog.Logger
}

// MyJobWorkerDeps contains dependencies for MyJobWorker.
type MyJobWorkerDeps struct {
    Service  services.MyService
    Logger   *slog.Logger
}

// NewMyJobWorker creates a new worker.
func NewMyJobWorker(deps MyJobWorkerDeps) *MyJobWorker {
    logger := deps.Logger
    if logger == nil {
        logger = slog.Default()
    }
    return &MyJobWorker{
        service: deps.Service,
        logger:  logger,
    }
}
```

### Work Method

The Work method is where job logic lives:

```go
func (w *MyJobWorker) Work(ctx context.Context, job *river.Job[MyJobArgs]) error {
    args := job.Args

    w.logger.Info("starting job",
        "job_id", job.ID,
        "user_id", args.UserID,
        "attempt", job.Attempt,
    )

    startTime := time.Now()

    // Do the work
    result, err := w.service.DoSomething(ctx, args.UserID, args.ResourceID)
    if err != nil {
        w.logger.Error("job failed",
            "job_id", job.ID,
            "error", err,
            "duration_ms", time.Since(startTime).Milliseconds(),
        )
        return fmt.Errorf("failed to process: %w", err)
    }

    w.logger.Info("job completed",
        "job_id", job.ID,
        "result", result,
        "duration_ms", time.Since(startTime).Milliseconds(),
    )

    return nil
}
```

## Queue Configuration

### Queues

| Queue | Purpose | Max Workers |
|-------|---------|-------------|
| `critical` | Auth issues, item errors | 10 |
| `default` | Plaid sync, categorization | 20 |
| `notifications` | Email, in-app notifications | 5 |
| `low` | Deletions, analytics, digests | 3 |

### Retry Policy

Default exponential backoff: 30s → 2min → 8min → 32min → 2hr (5 attempts)

```go
// internal/queue/river/config.go
func DefaultRetryPolicy() *RetryPolicy {
    return &RetryPolicy{
        MaxAttempts:       5,
        BackoffMultiplier: 4,
        InitialBackoff:    30 * time.Second,
        MaxBackoff:        2 * time.Hour,
    }
}
```

## Enqueueing Jobs

### Basic Enqueue

```go
// Enqueue with default options
result, err := jobQueue.Enqueue(ctx, jobs.MyJobArgs{
    UserID:     userID,
    ResourceID: resourceID,
})
```

### With Options

```go
import "github.com/maxnussbaum/BoostedBudget/internal/queue"

// High priority, specific queue
result, err := jobQueue.Enqueue(ctx, jobs.MyJobArgs{...},
    queue.WithQueue("critical"),
    queue.WithPriority(1),
)

// Scheduled for later
result, err := jobQueue.Enqueue(ctx, jobs.MyJobArgs{...},
    queue.WithScheduledAt(time.Now().Add(1 * time.Hour)),
)

// With uniqueness constraint
result, err := jobQueue.Enqueue(ctx, jobs.MyJobArgs{...},
    queue.WithUnique(queue.UniqueOpts{
        ByArgs:   true,
        ByPeriod: 5 * time.Minute,
        ByState:  []queue.JobState{queue.JobStateAvailable, queue.JobStateRunning},
    }),
)
```

### Transactional Enqueue

Enqueue jobs that commit only if the transaction succeeds:

```go
tx, err := db.Begin(ctx)
if err != nil {
    return err
}
defer tx.Rollback(ctx)

// Create record
if err := repo.Create(ctx, tx, record); err != nil {
    return err
}

// Enqueue job in same transaction
result, err := jobQueue.EnqueueTx(ctx, tx, jobs.MyJobArgs{...})
if err != nil {
    return err
}

return tx.Commit(ctx)
```

## Uniqueness Constraints

Prevent duplicate jobs with unique options:

```go
// Recommended uniqueness for each job type
func MyJobUniqueOpts() *queue.UniqueOpts {
    return &queue.UniqueOpts{
        ByArgs:   true,  // One per unique args combination
        ByPeriod: 5 * time.Minute,  // Debounce period
        ByState: []queue.JobState{
            queue.JobStateAvailable,
            queue.JobStateScheduled,
            queue.JobStateRunning,
            queue.JobStateRetryable,
            queue.JobStatePending,
        },
    }
}
```

### Common Uniqueness Patterns

| Job Type | ByArgs | ByPeriod | Purpose |
|----------|--------|----------|---------|
| PlaidSync | true | 5min | Debounce rapid webhook events |
| BudgetEval | true | 1min | Debounce transaction updates |
| AccountDeletion | true | 24hr | One deletion per user |
| ScheduledDeletionCheck | false | 1hr | Only one coordinator running |

## Periodic Jobs

### Configuration

Configure periodic jobs in main.go:

```go
riverConfig.PeriodicJobs = []*river.PeriodicJob{
    river.NewPeriodicJob(
        river.PeriodicInterval(1*time.Hour),
        func() (river.JobArgs, *river.InsertOpts) {
            return jobs.ScheduledDeletionCheckArgs{
                TriggerSource: "periodic",
            }, &river.InsertOpts{
                Queue: "low",
            }
        },
        &river.PeriodicJobOpts{RunOnStart: true},
    ),
}
```

### Periodic Job Workers

Periodic jobs that spawn other jobs need circular dependency handling:

```go
type CoordinatorWorker struct {
    river.WorkerDefaults[CoordinatorArgs]
    jobQueue queue.JobQueue  // Set after client creation
}

// SetJobQueue handles circular dependency
func (w *CoordinatorWorker) SetJobQueue(jq queue.JobQueue) {
    w.jobQueue = jq
}
```

## Dead Letter Queue

### How It Works

Jobs that exhaust all retry attempts are moved to `dead_letter_jobs` table:

```sql
CREATE TABLE dead_letter_jobs (
    id BIGSERIAL PRIMARY KEY,
    original_job_id BIGINT NOT NULL,
    kind VARCHAR(255) NOT NULL,
    queue VARCHAR(255) NOT NULL,
    args JSONB NOT NULL,
    errors JSONB NOT NULL,      -- Error history from each attempt
    max_attempts INT NOT NULL,
    failed_at TIMESTAMPTZ NOT NULL,
    user_id VARCHAR(255),       -- For user notification
    notified BOOLEAN DEFAULT FALSE,
    notes TEXT                  -- Admin notes
);
```

### ErrorHandler

```go
// internal/queue/river/dead_letter.go
type DeadLetterHandler struct {
    repo   *repository.DeadLetterJobRepository
    logger *slog.Logger
}

func (h *DeadLetterHandler) HandleError(ctx context.Context, job *rivertype.JobRow, err error) *river.ErrorHandlerResult {
    // Only process jobs that exhausted retries
    if job.Attempt < job.MaxAttempts {
        return nil
    }

    // Store in dead letter table
    // ...
    return nil
}
```

## Worker Registration

### In main.go

```go
// Create workers
webhookWorker := jobs.NewWebhookProcessingWorker(deps)
deletionWorker := jobs.NewAccountDeletionWorker(deps)

// Create River workers registry
riverWorkers := river.NewWorkers()
river.AddWorker(riverWorkers, webhookWorker)
river.AddWorker(riverWorkers, deletionWorker)

// Create client
riverClient, err := riverqueue.NewClient(db, riverWorkers, riverConfig)

// Wire up dependencies after client creation
deletionCheckWorker.SetJobQueue(riverClient)
```

## Testing Pattern

### Mock JobQueue

```go
type MockJobQueue struct {
    mock.Mock
}

func (m *MockJobQueue) Enqueue(ctx context.Context, job queue.Job, opts ...queue.EnqueueOption) (*queue.JobResult, error) {
    args := m.Called(ctx, job, opts)
    if args.Get(0) == nil {
        return nil, args.Error(1)
    }
    return args.Get(0).(*queue.JobResult), args.Error(1)
}
```

### Worker Test

```go
func TestMyWorker_Work_Success(t *testing.T) {
    mockService := new(MockMyService)

    worker := NewMyJobWorker(MyJobWorkerDeps{
        Service: mockService,
        Logger:  slog.Default(),
    })

    mockService.On("DoSomething", mock.Anything, "user-1", "resource-1").
        Return(&Result{Success: true}, nil)

    job := &river.Job[MyJobArgs]{
        JobRow: &rivertype.JobRow{ID: 1, Attempt: 1},
        Args:   MyJobArgs{UserID: "user-1", ResourceID: "resource-1"},
    }

    err := worker.Work(context.Background(), job)

    require.NoError(t, err)
    mockService.AssertExpectations(t)
}
```

### Testing UniqueSkipped

```go
func TestMyWorker_UniqueSkipped(t *testing.T) {
    mockQueue := new(MockJobQueue)

    // Simulate unique constraint skip
    mockQueue.On("Enqueue", mock.Anything, mock.Anything, mock.Anything).
        Return(&queue.JobResult{ID: 0, UniqueSkipped: true}, nil)

    // Test that worker handles skip correctly
    // ...
}
```

## Job Chaining Pattern

Jobs can enqueue follow-up jobs:

```go
func (w *SyncWorker) Work(ctx context.Context, job *river.Job[SyncArgs]) error {
    result, err := w.syncService.Sync(ctx, job.Args.ItemID)
    if err != nil {
        return err
    }

    // Chain to categorization job
    if len(result.TransactionIDs) > 0 {
        _, err := w.jobQueue.Enqueue(ctx, jobs.CategorizeArgs{
            UserID:         job.Args.UserID,
            TransactionIDs: result.TransactionIDs,
        })
        if err != nil {
            w.logger.Warn("failed to enqueue categorization", "error", err)
            // Don't fail the sync job for chain failure
        }
    }

    return nil
}
```

## Graceful Shutdown

```go
// main.go shutdown sequence
if riverRunner != nil {
    log.Println("Stopping River queue runner...")
    stopCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
    if err := riverRunner.Stop(stopCtx); err != nil {
        // Try force stop
        riverRunner.StopAndCancel(stopCtx)
    }
    cancel()
}
```

## Quick Reference

| Action | Code |
|--------|------|
| Create job args | Implement `Kind() string` |
| Create worker | Embed `river.WorkerDefaults[Args]` |
| Enqueue job | `jobQueue.Enqueue(ctx, args, opts...)` |
| Transactional enqueue | `jobQueue.EnqueueTx(ctx, tx, args, opts...)` |
| Set queue | `queue.WithQueue("low")` |
| Set priority | `queue.WithPriority(1)` |
| Schedule job | `queue.WithScheduledAt(time)` |
| Add uniqueness | `queue.WithUnique(opts)` |
| Check if skipped | `result.UniqueSkipped` |
| Register worker | `river.AddWorker(workers, worker)` |
