// Package jobs contains River job definitions and workers for async processing.
// Issue #1413 Task 9: Manual tier override expiry check — the time-triggered twin of the admin
// "remove override" endpoint (Issue #1414 / Task 7).
//
// #1652 deleted the worker (ManualTierOverrideExpiryCheckWorker) and its periodic registration:
// RevenueCat is now the single entitlement authority, and its projection path
// (revenueCatSession.Save) clears manual_tier_override when access resolves inactive, so a
// separate daily sweep over the legacy local column is dead authority. Only the Args/Kind and
// ManualTierOverrideExpiryCheckUniqueOpts survive — the Kind() is still needed by the
// RetiredSubscriptionWorker drain in cmd/server/main.go, and the UniqueOpts is still needed by the
// uniqueOptsByKind registry (unique_opts_by_kind.go) so a dead-letter replay of this kind is not
// refused.
package jobs

import (
	"time"

	"github.com/maxnussbaum/BoostedBudget/internal/queue"
)

// ManualTierOverrideExpiryCheckArgs contains the arguments for the manual tier override expiry
// check job. This job ran daily to find users whose manual_tier_override had lapsed
// (manual_tier_ends_at passed) and perform the same clear/re-arm-retention/audit transition the
// admin "remove override" endpoint performs for a manual removal.
type ManualTierOverrideExpiryCheckArgs struct {
	// TriggerSource indicates what triggered this check (e.g., "periodic", "manual")
	TriggerSource string `json:"trigger_source,omitempty"`
}

// Kind returns the unique identifier for this job type.
func (ManualTierOverrideExpiryCheckArgs) Kind() string { return "manual_tier_override_expiry_check" }

// ManualTierOverrideExpiryCheckUniqueOpts returns the recommended uniqueness options.
// Only one check job should run at a time to prevent duplicate expiry processing.
//
// Issue #1413 Task W (Codex review): ByState now matches River's own default
// (rivertype.UniqueOptsByStateDefault(), river@v0.43.0) exactly: Available, Completed, Pending,
// Retryable, Running, Scheduled. It previously omitted Completed, which — per
// river.UniqueOpts.ByState's doc comment (insert_opts.go) — meant a job that had ALREADY
// completed within the current ByPeriod bucket did not block a duplicate insert. Combined with
// the job's RunOnStart: true periodic registration (a catch-up insert attempted on every process
// restart/leader election), that let this daily job run on every boot on a
// multiple-deploys-per-day cadence instead of once per ByPeriod window.
//
// Deliberately NOT Cancelled or Discarded, even though River documents both as safe to *add*:
// river's canonical default (rivertype.UniqueOptsByStateDefault()) excludes both, and — more
// importantly for this specific job — Discarded means every retry was exhausted without the run
// ever completing its clear/re-arm-retention/audit work. Treating a discarded run as "already
// handled this period" would let lapsed manual tier overrides go silently uncleared (and users
// stay stuck out of the retention pipeline) for up to river's DiscardedJobRetentionPeriodDefault
// (7 days) before another catch-up attempt could get through — exactly what RunOnStart existed to
// prevent. Cancelled is excluded for the same reasoning (and is currently unreachable in this
// codebase regardless: nothing cancels this job kind).
func ManualTierOverrideExpiryCheckUniqueOpts() *queue.UniqueOpts {
	return &queue.UniqueOpts{
		ByArgs: false, // All check jobs are equivalent
		// 23 hours period — avoids overlap on daily schedule
		ByPeriod: 23 * time.Hour,
		ByState: []queue.JobState{
			queue.JobStateAvailable,
			queue.JobStateScheduled,
			queue.JobStateRunning,
			queue.JobStateRetryable,
			queue.JobStatePending,
			queue.JobStateCompleted,
		},
	}
}
