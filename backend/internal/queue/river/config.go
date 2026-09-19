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

	// PeriodicJobs configures recurring jobs that run on a schedule.
	PeriodicJobs []*river.PeriodicJob

	// ErrorHandler handles job errors and panics.
	// Used for dead letter queue integration and custom error handling.
	ErrorHandler river.ErrorHandler

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

	// RevenueCat has its own small pool so repairs do not crowd out paid/grant
	// confirmations or fan out at the concurrency used for general background work.
	RevenueCat int
}

// DefaultConfig returns the default configuration.
func DefaultConfig() Config {
	return Config{
		Workers:              DefaultWorkerConfig(),
		JobTimeout:           30 * time.Minute,
		RescueStuckJobsAfter: 1 * time.Hour,
		FetchCooldown:        200 * time.Millisecond,
		FetchPollInterval:    500 * time.Millisecond,
	}
}

// DefaultWorkerConfig returns the default worker configuration.
func DefaultWorkerConfig() *WorkerConfig {
	return &WorkerConfig{
		Critical:      10,
		Default:       20,
		Notifications: 5,
		Low:           3,
		RevenueCat:    2,
	}
}

// TotalWorkers returns the total number of workers across all queues.
func (wc *WorkerConfig) TotalWorkers() int {
	return wc.Critical + wc.Default + wc.Notifications + wc.Low + wc.RevenueCat
}

// ToRiverQueues converts worker config to River queue configuration.
func (wc *WorkerConfig) ToRiverQueues() map[string]river.QueueConfig {
	queues := map[string]river.QueueConfig{
		"critical":      {MaxWorkers: wc.Critical},
		"default":       {MaxWorkers: wc.Default},
		"notifications": {MaxWorkers: wc.Notifications},
		"low":           {MaxWorkers: wc.Low},
	}
	if wc.RevenueCat > 0 {
		queues["revenuecat"] = river.QueueConfig{MaxWorkers: wc.RevenueCat}
	}
	return queues
}
