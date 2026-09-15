// Package jobs contains River job definitions and workers for async processing.
//
// #1652: the past-due dunning worker and its PastDueService were deleted. RevenueCat is now the
// sole access authority, and the Plaid disconnect it used to perform at 14 days past due is driven
// by the RC retention path instead (revenueCatSession.Save arms plaid_cleanup_pending_at when
// access resolves inactive). PastDueCheckArgs, its Kind(), and PastDueCheckUniqueOpts are RETAINED
// because the dead-letter replay registry (unique_opts_by_kind.go) and the legacy drain
// (RetiredSubscriptionWorker, main.go) still need them.
package jobs

import (
	"time"

	"github.com/maxnussbaum/BoostedBudget/internal/queue"
)

type PastDueCheckArgs struct {
	TriggerSource string `json:"trigger_source,omitempty"`
}

func (PastDueCheckArgs) Kind() string { return "past_due_check" }

func PastDueCheckUniqueOpts() *queue.UniqueOpts {
	return &queue.UniqueOpts{
		ByArgs:   false,
		ByPeriod: 1 * time.Hour,
		ByState: []queue.JobState{
			queue.JobStateAvailable,
			queue.JobStateScheduled,
			queue.JobStateRunning,
			queue.JobStateRetryable,
			queue.JobStatePending,
			// Completed is REQUIRED because this job was registered with RunOnStart: true
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
