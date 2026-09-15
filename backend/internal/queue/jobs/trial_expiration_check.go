// Package jobs contains River job definitions and workers for async processing.
// Issue #679, #680: Trial expiration check job
package jobs

import (
	"context"
	"time"

	"github.com/maxnussbaum/BoostedBudget/internal/queue"
	"github.com/riverqueue/river"
)

// TrialExpirationCheckArgs contains the arguments for the trial expiration check job.
//
// Issue #1652: RevenueCat is now the access authority, so the legacy trial-expiration worker
// (and the TrialService it drove) has been deleted. This Args type and its Kind() are RETAINED
// deliberately: jobs queued before the cutover are drained by RetiredSubscriptionWorker (see
// main.go), and unique_opts_by_kind.go's registry is keyed by this Kind().
type TrialExpirationCheckArgs struct {
	// TriggerSource indicates what triggered this check (e.g., "periodic", "manual")
	TriggerSource string `json:"trigger_source,omitempty"`
}

// RetiredSubscriptionWorker drains jobs queued before RevenueCat became the access
// authority. Keeping the original kinds registered avoids endless unknown-worker
// retries; these jobs must never invoke their former access or cleanup services.
type RetiredSubscriptionWorker[T river.JobArgs] struct {
	river.WorkerDefaults[T]
}

func (*RetiredSubscriptionWorker[T]) Work(context.Context, *river.Job[T]) error {
	return nil
}

// Kind returns the unique identifier for this job type.
func (TrialExpirationCheckArgs) Kind() string { return "trial_expiration_check" }

// TrialExpirationCheckUniqueOpts returns the recommended uniqueness options.
// Only one check job should run at a time to prevent duplicate expiration processing.
func TrialExpirationCheckUniqueOpts() *queue.UniqueOpts {
	return &queue.UniqueOpts{
		ByArgs: false, // All check jobs are equivalent
		// 1 hour period - typical check frequency
		ByPeriod: 1 * time.Hour,
		ByState: []queue.JobState{
			queue.JobStateAvailable,
			queue.JobStateScheduled,
			queue.JobStateRunning,
			queue.JobStateRetryable,
			queue.JobStatePending,
			// Completed is REQUIRED because this job is registered with RunOnStart: true
			// (Issue #1448). Without it, a run that COMPLETED minutes ago does not
			// deduplicate the start-of-process catch-up insert, so every deploy or leader
			// election enqueues a redundant duplicate. Matches the two jobs that already
			// had RunOnStart: true before #1448 (RetentionCleanupCheckUniqueOpts,
			// ManualTierOverrideExpiryCheckUniqueOpts). Found by the final branch-wide
			// Codex review.
			queue.JobStateCompleted,
		},
	}
}
