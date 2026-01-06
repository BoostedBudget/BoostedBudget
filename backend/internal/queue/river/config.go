package river

import (
	"time"

	"github.com/riverqueue/river"
)

// Config holds River queue configuration.
type Config struct {
	// Workers configures worker counts per queue.
	// If nil, defaults are used.
	Workers *WorkerConfig

	// JobTimeout is the maximum time a job can run before being cancelled.
	// Defaults to 30 minutes.
	JobTimeout time.Duration

	// RescueStuckJobsAfter is how long a job can be "running" before
	// being considered stuck and returned to the queue.
	// Defaults to 1 hour.
	RescueStuckJobsAfter time.Duration

	// FetchCooldown is how long to wait between fetch attempts when queue is empty.
	// Defaults to 200ms.
	FetchCooldown time.Duration

	// FetchPollInterval is how often to poll for new jobs.
	// Defaults to 500ms.
	FetchPollInterval time.Duration

	// RetryPolicy configures the retry backoff strategy.
	RetryPolicy *RetryPolicy

	// PeriodicJobs configures recurring jobs that run on a schedule.
	PeriodicJobs []*river.PeriodicJob

	// Logger is an optional logger for River.
	// If nil, River's default logger is used.
	Logger any // *slog.Logger
}

// WorkerConfig configures worker counts per queue.
type WorkerConfig struct {
	// Critical queue: Item errors, auth issues (default: 10)
	Critical int

	// Default queue: Plaid sync, categorization, budget eval (default: 20)
	Default int

	// Notifications queue: Email, in-app notifications (default: 5)
	Notifications int

	// Low queue: Deletions, analytics, digests (default: 3)
	Low int
}

// RetryPolicy configures retry behavior.
type RetryPolicy struct {
	// MaxAttempts is the default maximum retry attempts.
	// Defaults to 5.
	MaxAttempts int

	// BackoffMultiplier is the exponential backoff multiplier.
	// Defaults to 4 (30s, 2min, 8min, 32min, 2hr).
	BackoffMultiplier float64

	// InitialBackoff is the initial retry delay.
	// Defaults to 30 seconds.
	InitialBackoff time.Duration

	// MaxBackoff is the maximum retry delay.
	// Defaults to 2 hours.
	MaxBackoff time.Duration
}

// DefaultConfig returns the default configuration.
func DefaultConfig() Config {
	return Config{
		Workers:              DefaultWorkerConfig(),
		JobTimeout:           30 * time.Minute,
		RescueStuckJobsAfter: 1 * time.Hour,
		FetchCooldown:        200 * time.Millisecond,
		FetchPollInterval:    500 * time.Millisecond,
		RetryPolicy:          DefaultRetryPolicy(),
	}
}

// DefaultWorkerConfig returns the default worker configuration.
func DefaultWorkerConfig() *WorkerConfig {
	return &WorkerConfig{
		Critical:      10,
		Default:       20,
		Notifications: 5,
		Low:           3,
	}
}

// DefaultRetryPolicy returns the default retry policy.
// Uses exponential backoff: 30s, 2min, 8min, 32min, 2hr
func DefaultRetryPolicy() *RetryPolicy {
	return &RetryPolicy{
		MaxAttempts:       5,
		BackoffMultiplier: 4,
		InitialBackoff:    30 * time.Second,
		MaxBackoff:        2 * time.Hour,
	}
}

// TotalWorkers returns the total number of workers across all queues.
func (wc *WorkerConfig) TotalWorkers() int {
	return wc.Critical + wc.Default + wc.Notifications + wc.Low
}

// ToRiverQueues converts worker config to River queue configuration.
func (wc *WorkerConfig) ToRiverQueues() map[string]river.QueueConfig {
	return map[string]river.QueueConfig{
		"critical":      {MaxWorkers: wc.Critical},
		"default":       {MaxWorkers: wc.Default},
		"notifications": {MaxWorkers: wc.Notifications},
		"low":           {MaxWorkers: wc.Low},
	}
}

// NextRetry calculates the next retry time based on attempt number.
func (rp *RetryPolicy) NextRetry(attempt int) time.Duration {
	if attempt < 1 {
		return rp.InitialBackoff
	}

	// Exponential backoff: initial * (multiplier ^ (attempt - 1))
	backoff := float64(rp.InitialBackoff)
	for i := 1; i < attempt; i++ {
		backoff *= rp.BackoffMultiplier
	}

	result := time.Duration(backoff)
	if result > rp.MaxBackoff {
		return rp.MaxBackoff
	}
	return result
}
