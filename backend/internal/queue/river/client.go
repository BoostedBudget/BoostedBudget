// Package river provides the River implementation of the queue interfaces.
//
// River is a PostgreSQL-backed job queue that provides reliable, transactional
// job processing with support for uniqueness constraints, priorities, and
// scheduled execution.
//
// # Features
//
//   - Transactional job insertion (jobs committed with your database operations)
//   - Unique job constraints (deduplication)
//   - Priority queues (1-4, where 1 is highest)
//   - Scheduled jobs (run at a specific time)
//   - Automatic retries with exponential backoff
//   - Dead letter queue for failed jobs
//
// # Usage
//
//	client, err := river.NewClient(pool, river.DefaultConfig())
//	if err != nil {
//	    return err
//	}
//
//	// Register workers
//	river.AddWorker(workers, &PlaidSyncWorker{...})
//
//	// Start processing
//	go client.Start(ctx)
//
//	// Enqueue jobs
//	client.Enqueue(ctx, PlaidSyncArgs{...})
package river

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/maxnussbaum/BoostedBudget/internal/queue"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"
	"github.com/riverqueue/river/rivertype"
)

// Client wraps a River client and implements the queue interfaces.
type Client struct {
	client *river.Client[pgx.Tx]
	pool   *pgxpool.Pool
	config Config
}

// Ensure Client implements the interfaces
var _ queue.JobQueue = (*Client)(nil)
var _ queue.JobRunner = (*Client)(nil)

// NewClient creates a new River client with the given configuration.
// The workers parameter should be created with river.NewWorkers() and
// populated with AddWorker before calling NewClient.
func NewClient(pool *pgxpool.Pool, workers *river.Workers, cfg Config) (*Client, error) {
	if cfg.Workers == nil {
		cfg.Workers = DefaultWorkerConfig()
	}
	if cfg.RetryPolicy == nil {
		cfg.RetryPolicy = DefaultRetryPolicy()
	}

	riverConfig := &river.Config{
		Queues:               cfg.Workers.ToRiverQueues(),
		Workers:              workers,
		JobTimeout:           cfg.JobTimeout,
		RescueStuckJobsAfter: cfg.RescueStuckJobsAfter,
		FetchCooldown:        cfg.FetchCooldown,
		FetchPollInterval:    cfg.FetchPollInterval,
		PeriodicJobs:         cfg.PeriodicJobs,
		ErrorHandler:         cfg.ErrorHandler,
	}

	riverClient, err := river.NewClient(riverpgxv5.New(pool), riverConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create river client: %w", err)
	}

	return &Client{
		client: riverClient,
		pool:   pool,
		config: cfg,
	}, nil
}

// Enqueue adds a job to the queue for processing.
func (c *Client) Enqueue(ctx context.Context, job queue.Job, opts ...queue.EnqueueOption) (*queue.JobResult, error) {
	// Get the underlying River job args
	riverJob, ok := job.(river.JobArgs)
	if !ok {
		return nil, fmt.Errorf("job must implement river.JobArgs interface")
	}

	options := queue.ApplyOptions(opts)
	riverOpts := c.toRiverInsertOpts(options)

	result, err := c.client.Insert(ctx, riverJob, riverOpts)
	if err != nil {
		return nil, fmt.Errorf("failed to enqueue job: %w", err)
	}

	return c.toJobResult(result), nil
}

// EnqueueTx adds a job within a database transaction.
func (c *Client) EnqueueTx(ctx context.Context, tx pgx.Tx, job queue.Job, opts ...queue.EnqueueOption) (*queue.JobResult, error) {
	// Get the underlying River job args
	riverJob, ok := job.(river.JobArgs)
	if !ok {
		return nil, fmt.Errorf("job must implement river.JobArgs interface")
	}

	options := queue.ApplyOptions(opts)
	riverOpts := c.toRiverInsertOpts(options)

	result, err := c.client.InsertTx(ctx, tx, riverJob, riverOpts)
	if err != nil {
		return nil, fmt.Errorf("failed to enqueue job in transaction: %w", err)
	}

	return c.toJobResult(result), nil
}

// EnqueueMany adds multiple jobs to the queue in a single operation.
func (c *Client) EnqueueMany(ctx context.Context, jobs []queue.Job, opts ...queue.EnqueueOption) ([]*queue.JobResult, error) {
	options := queue.ApplyOptions(opts)
	riverOpts := c.toRiverInsertOpts(options)

	params := make([]river.InsertManyParams, len(jobs))
	for i, job := range jobs {
		riverJob, ok := job.(river.JobArgs)
		if !ok {
			return nil, fmt.Errorf("job at index %d must implement river.JobArgs interface", i)
		}
		params[i] = river.InsertManyParams{
			Args:       riverJob,
			InsertOpts: riverOpts,
		}
	}

	results, err := c.client.InsertMany(ctx, params)
	if err != nil {
		return nil, fmt.Errorf("failed to enqueue jobs: %w", err)
	}

	jobResults := make([]*queue.JobResult, len(results))
	for i, result := range results {
		jobResults[i] = c.toJobResult(result)
	}

	return jobResults, nil
}

// EnqueueManyTx adds multiple jobs within a database transaction.
func (c *Client) EnqueueManyTx(ctx context.Context, tx pgx.Tx, jobs []queue.Job, opts ...queue.EnqueueOption) ([]*queue.JobResult, error) {
	options := queue.ApplyOptions(opts)
	riverOpts := c.toRiverInsertOpts(options)

	params := make([]river.InsertManyParams, len(jobs))
	for i, job := range jobs {
		riverJob, ok := job.(river.JobArgs)
		if !ok {
			return nil, fmt.Errorf("job at index %d must implement river.JobArgs interface", i)
		}
		params[i] = river.InsertManyParams{
			Args:       riverJob,
			InsertOpts: riverOpts,
		}
	}

	results, err := c.client.InsertManyTx(ctx, tx, params)
	if err != nil {
		return nil, fmt.Errorf("failed to enqueue jobs in transaction: %w", err)
	}

	jobResults := make([]*queue.JobResult, len(results))
	for i, result := range results {
		jobResults[i] = c.toJobResult(result)
	}

	return jobResults, nil
}

// Start begins processing jobs from all configured queues.
func (c *Client) Start(ctx context.Context) error {
	return c.client.Start(ctx)
}

// Stop gracefully shuts down the job runner.
func (c *Client) Stop(ctx context.Context) error {
	return c.client.Stop(ctx)
}

// StopAndCancel immediately stops the job runner, canceling in-progress jobs.
func (c *Client) StopAndCancel(ctx context.Context) error {
	return c.client.StopAndCancel(ctx)
}

// RiverClient returns the underlying River client for advanced operations.
// Use this when you need River-specific features not exposed by the abstraction.
func (c *Client) RiverClient() *river.Client[pgx.Tx] {
	return c.client
}

// toRiverInsertOpts converts queue options to River insert options.
func (c *Client) toRiverInsertOpts(opts queue.EnqueueOptions) *river.InsertOpts {
	riverOpts := &river.InsertOpts{
		Queue:       opts.Queue,
		MaxAttempts: opts.MaxAttempts,
		Tags:        opts.Tags,
	}

	if opts.Priority != 0 {
		riverOpts.Priority = opts.Priority
	}

	if !opts.ScheduledAt.IsZero() {
		riverOpts.ScheduledAt = opts.ScheduledAt
	}

	if opts.Metadata != nil {
		// River expects Metadata as []byte (JSON), so we serialize our map
		metadataBytes, err := json.Marshal(opts.Metadata)
		if err == nil {
			riverOpts.Metadata = metadataBytes
		}
	}

	if opts.Unique != nil {
		riverOpts.UniqueOpts = c.toRiverUniqueOpts(opts.Unique)
	}

	return riverOpts
}

// toRiverUniqueOpts converts queue uniqueness options to River options.
func (c *Client) toRiverUniqueOpts(unique *queue.UniqueOpts) river.UniqueOpts {
	opts := river.UniqueOpts{
		ByPeriod: unique.ByPeriod,
	}

	if unique.ByArgs {
		opts.ByArgs = true
	}

	if unique.ByQueue {
		opts.ByQueue = true
	}

	if len(unique.ByState) > 0 {
		states := make([]rivertype.JobState, len(unique.ByState))
		for i, state := range unique.ByState {
			states[i] = rivertype.JobState(state)
		}
		opts.ByState = states
	}

	return opts
}

// toJobResult converts a River insert result to our JobResult.
func (c *Client) toJobResult(result *rivertype.JobInsertResult) *queue.JobResult {
	return &queue.JobResult{
		ID:            result.Job.ID,
		Kind:          result.Job.Kind,
		Queue:         result.Job.Queue,
		State:         queue.JobState(result.Job.State),
		ScheduledAt:   result.Job.ScheduledAt,
		UniqueSkipped: result.UniqueSkippedAsDuplicate,
	}
}

// CustomRetryPolicy implements River's RetryPolicy interface with our exponential backoff.
type CustomRetryPolicy struct {
	policy *RetryPolicy
}

// NewCustomRetryPolicy creates a retry policy from our configuration.
func NewCustomRetryPolicy(policy *RetryPolicy) *CustomRetryPolicy {
	if policy == nil {
		policy = DefaultRetryPolicy()
	}
	return &CustomRetryPolicy{policy: policy}
}

// NextRetry implements river.RetryPolicy.
func (p *CustomRetryPolicy) NextRetry(job *rivertype.JobRow) time.Time {
	backoff := p.policy.NextRetry(job.Attempt)
	return time.Now().Add(backoff)
}
