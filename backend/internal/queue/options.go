package queue

import "time"

// DefaultEnqueueOptions returns the default options for enqueueing jobs.
func DefaultEnqueueOptions() EnqueueOptions {
	return EnqueueOptions{
		Queue:       "default",
		Priority:    2, // Normal priority (1=high, 4=low)
		MaxAttempts: 5,
	}
}

// ApplyOptions applies all options to the base options struct.
func ApplyOptions(opts []EnqueueOption) EnqueueOptions {
	options := DefaultEnqueueOptions()
	for _, opt := range opts {
		opt(&options)
	}
	return options
}

// WithQueue sets the queue name for the job.
// Available queues: "critical", "default", "notifications", "low"
//
// Queue purposes:
//   - critical: Item errors, auth issues, high-priority alerts (10 workers)
//   - default: Plaid sync, categorization, budget eval (20 workers)
//   - notifications: Email, in-app notifications (5 workers)
//   - low: Deletions, analytics, digests (3 workers)
func WithQueue(queue string) EnqueueOption {
	return func(o *EnqueueOptions) {
		o.Queue = queue
	}
}

// WithPriority sets the job priority (1-4).
// Priority 1 is highest, 4 is lowest.
// Jobs with higher priority are processed first within the same queue.
func WithPriority(priority int) EnqueueOption {
	return func(o *EnqueueOptions) {
		if priority < 1 {
			priority = 1
		}
		if priority > 4 {
			priority = 4
		}
		o.Priority = priority
	}
}

// WithHighPriority sets priority to 1 (highest).
func WithHighPriority() EnqueueOption {
	return WithPriority(1)
}

// WithLowPriority sets priority to 4 (lowest).
func WithLowPriority() EnqueueOption {
	return WithPriority(4)
}

// WithScheduledAt schedules the job for future execution.
// The job will not be processed until after this time.
func WithScheduledAt(t time.Time) EnqueueOption {
	return func(o *EnqueueOptions) {
		o.ScheduledAt = t
	}
}

// WithDelay schedules the job to run after the specified duration.
// Equivalent to WithScheduledAt(time.Now().Add(delay)).
func WithDelay(delay time.Duration) EnqueueOption {
	return func(o *EnqueueOptions) {
		o.ScheduledAt = time.Now().Add(delay)
	}
}

// WithMaxAttempts sets the maximum number of attempts for the job.
// After this many failures, the job is moved to the dead letter queue.
// Set to 1 for jobs that should not be retried.
func WithMaxAttempts(attempts int) EnqueueOption {
	return func(o *EnqueueOptions) {
		if attempts < 1 {
			attempts = 1
		}
		o.MaxAttempts = attempts
	}
}

// WithUnique enables job deduplication with the specified options.
// Only one job matching the unique criteria can exist at a time.
func WithUnique(unique UniqueOpts) EnqueueOption {
	return func(o *EnqueueOptions) {
		o.Unique = &unique
	}
}

// WithUniqueByArgs deduplicates jobs with identical arguments.
// Jobs with the same Kind and Args are considered duplicates.
func WithUniqueByArgs() EnqueueOption {
	return func(o *EnqueueOptions) {
		if o.Unique == nil {
			o.Unique = &UniqueOpts{}
		}
		o.Unique.ByArgs = true
	}
}

// WithUniqueByPeriod sets a time window for uniqueness.
// Only one job per time period is allowed.
// Example: WithUniqueByPeriod(5*time.Minute) allows one job per 5 minutes.
func WithUniqueByPeriod(period time.Duration) EnqueueOption {
	return func(o *EnqueueOptions) {
		if o.Unique == nil {
			o.Unique = &UniqueOpts{}
		}
		o.Unique.ByPeriod = period
	}
}

// WithUniqueByArgsAndPeriod combines args and period uniqueness.
// This is the most common uniqueness pattern for rate-limiting jobs.
func WithUniqueByArgsAndPeriod(period time.Duration) EnqueueOption {
	return func(o *EnqueueOptions) {
		if o.Unique == nil {
			o.Unique = &UniqueOpts{}
		}
		o.Unique.ByArgs = true
		o.Unique.ByPeriod = period
	}
}

// WithTags adds tags to the job for filtering and querying.
// Tags are useful for grouping related jobs or adding metadata.
func WithTags(tags ...string) EnqueueOption {
	return func(o *EnqueueOptions) {
		o.Tags = append(o.Tags, tags...)
	}
}

// WithMetadata adds metadata to the job.
// Metadata is stored as JSON and can be used for tracking or debugging.
func WithMetadata(key string, value any) EnqueueOption {
	return func(o *EnqueueOptions) {
		if o.Metadata == nil {
			o.Metadata = make(map[string]any)
		}
		o.Metadata[key] = value
	}
}

// WithUserID adds a user_id to job metadata.
// This is commonly used to track which user triggered the job.
func WithUserID(userID string) EnqueueOption {
	return WithMetadata("user_id", userID)
}

// Queue name constants for type safety.
const (
	// QueueCritical is for urgent jobs like error handling and auth issues.
	QueueCritical = "critical"

	// QueueDefault is for standard processing jobs.
	QueueDefault = "default"

	// QueueNotifications is for email and in-app notifications.
	QueueNotifications = "notifications"

	// QueueLow is for low-priority background tasks.
	QueueLow = "low"
)
