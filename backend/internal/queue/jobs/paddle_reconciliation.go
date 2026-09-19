// Package jobs contains River job definitions and workers for async processing.
// Issue #762: Paddle reconciliation job
package jobs

import (
	"time"

	"github.com/maxnussbaum/BoostedBudget/internal/queue"
)

// PaddleReconciliationArgs contains the arguments for the Paddle reconciliation job.
// This job runs periodically to reconcile local subscription data against Paddle.
//
// #1652 deleted the worker (PaddleReconciliationWorker) and its periodic registration: RevenueCat
// is now the single entitlement authority, so reconciling local subscription columns against
// Paddle is dead authority. Only the Args/Kind and PaddleReconciliationUniqueOpts survive — the
// Kind() is still needed by the RetiredSubscriptionWorker drain in cmd/server/main.go, and
// PaddleReconciliationUniqueOpts is still needed by the uniqueOptsByKind registry
// (unique_opts_by_kind.go) so a dead-letter replay of this kind is not refused.
type PaddleReconciliationArgs struct {
	// TriggerSource indicates what triggered this check (e.g., "periodic", "manual")
	TriggerSource string `json:"trigger_source,omitempty"`
}

// Kind returns the unique identifier for this job type.
func (PaddleReconciliationArgs) Kind() string { return "paddle_reconciliation" }

// PaddleReconciliationUniqueOpts returns the recommended uniqueness options.
// Only one reconciliation job should be pending/running at a time.
func PaddleReconciliationUniqueOpts() *queue.UniqueOpts {
	return &queue.UniqueOpts{
		ByArgs:   false,
		ByPeriod: 12 * time.Hour,
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
