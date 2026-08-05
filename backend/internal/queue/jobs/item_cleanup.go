// Package jobs contains River job definitions and workers for async processing.
package jobs

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/maxnussbaum/BoostedBudget/internal/observability"
	"github.com/maxnussbaum/BoostedBudget/internal/queue"
	"github.com/maxnussbaum/BoostedBudget/internal/services"
	"github.com/riverqueue/river"
)

// ItemCleanupArgs contains the arguments for an item cleanup job.
// This job removes a Plaid item via /item/remove API and updates the database.
//
// Issue #668: Plaid Item Management for Cost Optimization
type ItemCleanupArgs struct {
	// LinkedItemID is the ID of the linked item to clean up
	LinkedItemID string `json:"linked_item_id"`

	// UserID is the owner of the item (for validation)
	UserID string `json:"user_id"`

	// Reason indicates why this item is being cleaned up
	// Possible values: "long_term_error", "revoked", "stale", "subscription_downgrade", "user_deletion"
	Reason string `json:"reason"`
}

// Kind returns the unique identifier for this job type.
func (ItemCleanupArgs) Kind() string { return "item_cleanup" }

// ItemCleanupWorker processes item cleanup jobs.
// This worker removes Plaid items to stop billing and updates the database.
type ItemCleanupWorker struct {
	river.WorkerDefaults[ItemCleanupArgs]

	plaidCleanupSvc services.PlaidItemCleanupService
	linkedItemRepo  LinkedItemCleanupRepository
	logger          *slog.Logger
	// errorDaysThreshold / staleDaysThreshold are the eligibility thresholds the per-item recheck
	// (ClaimItemForCleanup) re-evaluates with. They MUST match the sweep's — see
	// defaultCleanupErrorDays in item_cleanup_check.go for what a mismatch costs.
	errorDaysThreshold int
	staleDaysThreshold int
}

// ItemCleanupWorkerDeps contains the dependencies for ItemCleanupWorker.
type ItemCleanupWorkerDeps struct {
	PlaidCleanupSvc services.PlaidItemCleanupService
	LinkedItemRepo  LinkedItemCleanupRepository
	Logger          *slog.Logger
	// ErrorDaysThreshold / StaleDaysThreshold must be given the SAME values as
	// ItemCleanupCheckWorkerDeps' fields of the same names (Issue #1486). Zero/unset uses the
	// shared defaultCleanupErrorDays / defaultCleanupStaleDays, which is what keeps the two
	// workers in step. Prefer leaving both unset — cmd/server/main.go does, so production parity is
	// structural rather than enforced by a comment on two separate literals. Only tests that need a
	// specific threshold set these.
	ErrorDaysThreshold int
	StaleDaysThreshold int
}

// NewItemCleanupWorker creates a new item cleanup worker.
func NewItemCleanupWorker(deps ItemCleanupWorkerDeps) *ItemCleanupWorker {
	logger := deps.Logger
	if logger == nil {
		logger = slog.Default()
	}
	return &ItemCleanupWorker{
		plaidCleanupSvc:    deps.PlaidCleanupSvc,
		linkedItemRepo:     deps.LinkedItemRepo,
		logger:             logger,
		errorDaysThreshold: intOrDefault(deps.ErrorDaysThreshold, defaultCleanupErrorDays),
		staleDaysThreshold: intOrDefault(deps.StaleDaysThreshold, defaultCleanupStaleDays),
	}
}

// Work processes an item cleanup job.
//
// The worker:
//  1. Re-verifies that the item is STILL a cleanup candidate (ClaimItemForCleanup) — Issue #1486.
//     Until then this comment claimed a validation the body did not perform.
//  2. Calls PlaidItemCleanupService.RemoveItem() to call Plaid /item/remove
//  3. Clears the cleanup schedule and hard-deletes the linked_items row (both best-effort)
//  4. Logs the cleanup for audit
//
// If the item is no longer a candidate — the user reconnected it through Plaid Link update mode,
// its accounts came back, or its error cleared — step 1 declines, the stale schedule is released,
// and the job returns nil WITHOUT calling Plaid. What it does NOT do is check "is the grace period
// expired": that is River's job, not this worker's (the sweep enqueues with WithScheduledAt), and
// nothing here re-derives it.
func (w *ItemCleanupWorker) Work(ctx context.Context, job *river.Job[ItemCleanupArgs]) (err error) {
	args := job.Args

	w.logger.Info("starting item cleanup",
		"job_id", job.ID,
		"linked_item_id", args.LinkedItemID,
		"user_id", args.UserID,
		"reason", args.Reason,
		"attempt", job.Attempt,
	)

	start := time.Now()
	defer func() {
		status := "success"
		if err != nil {
			status = "failed"
		}
		observability.JobsCompleted.WithLabelValues(job.Kind, job.Queue, status).Inc()
		observability.JobDuration.WithLabelValues(job.Kind).Observe(time.Since(start).Seconds())
	}()

	startTime := time.Now()

	// Issue #1486: re-verify ELIGIBILITY before the irreversible Plaid /item/remove.
	//
	// This job may have been queued for a long time — the error/login_required/stale legs schedule
	// it a full gracePeriodDays out (ItemCleanupCheckWorker), and River retries add more — and the
	// user can repair the connection through Plaid Link update mode at any point in that window.
	// Nothing marks that repair on the cleanup schedule: the only production callers of
	// ClearCleanupSchedule are both inside this function — one after a successful RemoveItem, and
	// one releasing the stale schedule when this very claim declines (grep
	// `\.ClearCleanupSchedule(`). NOTHING clears it on reconnection. So a
	// reconnected item still carries cleanup_scheduled_at, and a recheck of "is it still
	// scheduled" would authorise removing it. The recheck therefore re-asks the sweep's own
	// question — "is this still a cleanup candidate?" — against the same shared SQL predicate the
	// sweep selected it with (repository.cleanupCandidateReasonPredicate).
	claimed, claimErr := w.linkedItemRepo.ClaimItemForCleanup(ctx, args.LinkedItemID, w.errorDaysThreshold, w.staleDaysThreshold)
	if claimErr != nil {
		// Fail CLOSED. "We could not tell" is not "still eligible", and /item/remove cannot be
		// undone — returning an error leaves the schedule intact and lets River retry.
		w.logger.Error("failed to re-verify item cleanup eligibility, not removing item",
			"job_id", job.ID,
			"linked_item_id", args.LinkedItemID,
			"error", claimErr,
		)
		return fmt.Errorf("failed to re-verify cleanup eligibility for item %s: %w", args.LinkedItemID, claimErr)
	}
	if !claimed {
		w.logger.Info("item is no longer a cleanup candidate, skipping Plaid removal",
			"job_id", job.ID,
			"linked_item_id", args.LinkedItemID,
			"user_id", args.UserID,
			"reason", args.Reason,
		)

		// Release the now-stale schedule. Chosen over leaving it set: the sweep's candidate
		// predicate requires cleanup_scheduled_at IS NULL, so an item that keeps a schedule it will
		// never act on is invisible to every future sweep — if it degrades again later it would
		// never be cleaned up, and Plaid would bill for it indefinitely. Keeping the row as an audit
		// trail is not worth that: the audit record lives in this log line and in the completed
		// River job, neither of which the clear touches.
		//
		// A failure here is RETURNED, not swallowed. Returning nil would let River record success
		// and never come back, permanently creating the exact state the paragraph above calls
		// unacceptable. The retry is not a pointless re-run of a no-op: the recheck it repeats is
		// cheap and idempotent, and the clear — the part that actually failed — is what the retry
		// is for. Removal stays off the table either way, because the claim will decline again.
		if clearErr := w.linkedItemRepo.ClearCleanupSchedule(ctx, args.LinkedItemID); clearErr != nil {
			w.logger.Error("failed to clear stale cleanup schedule for a no-longer-eligible item, retrying",
				"job_id", job.ID,
				"linked_item_id", args.LinkedItemID,
				"error", clearErr,
			)
			return fmt.Errorf("failed to clear stale cleanup schedule for item %s: %w", args.LinkedItemID, clearErr)
		}

		// A no-longer-eligible item whose schedule was released is a SUCCESS with nothing to do,
		// not a failure.
		return nil
	}

	// Call cleanup service to remove the item from Plaid
	removeErr := w.plaidCleanupSvc.RemoveItem(ctx, args.LinkedItemID)
	if removeErr != nil {
		// Check if item not found - this means it was already cleaned up or removed
		// In this case, we consider it successful
		if services.IsItemNotFoundError(removeErr) {
			w.logger.Info("item already removed, marking job as complete",
				"job_id", job.ID,
				"linked_item_id", args.LinkedItemID,
			)
			// Item already gone from Plaid — still clean up the DB row (best-effort)
			if deleteErr := w.linkedItemRepo.Delete(ctx, args.LinkedItemID); deleteErr != nil {
				w.logger.Warn("failed to delete linked item from database after item-not-found",
					"linked_item_id", args.LinkedItemID,
					"error", deleteErr,
				)
			}
			return nil
		}

		w.logger.Error("item cleanup failed",
			"job_id", job.ID,
			"linked_item_id", args.LinkedItemID,
			"user_id", args.UserID,
			"reason", args.Reason,
			"error", removeErr,
			"duration_ms", time.Since(startTime).Milliseconds(),
		)
		return fmt.Errorf("failed to cleanup item: %w", removeErr)
	}

	// Clear the cleanup schedule since cleanup is complete (best-effort)
	if clearErr := w.linkedItemRepo.ClearCleanupSchedule(ctx, args.LinkedItemID); clearErr != nil {
		w.logger.Warn("failed to clear cleanup schedule",
			"linked_item_id", args.LinkedItemID,
			"error", clearErr,
		)
	}

	// Delete the linked_items row now that Plaid billing is stopped (best-effort)
	if deleteErr := w.linkedItemRepo.Delete(ctx, args.LinkedItemID); deleteErr != nil {
		w.logger.Warn("failed to delete linked item from database after Plaid removal",
			"linked_item_id", args.LinkedItemID,
			"error", deleteErr,
		)
	}

	w.logger.Info("item cleanup completed",
		"job_id", job.ID,
		"linked_item_id", args.LinkedItemID,
		"user_id", args.UserID,
		"reason", args.Reason,
		"duration_ms", time.Since(startTime).Milliseconds(),
	)

	return nil
}

// ItemCleanupUniqueOpts returns the recommended uniqueness options.
// Only one cleanup job per item can be queued/running at a time.
// This prevents duplicate cleanup attempts.
func ItemCleanupUniqueOpts() *queue.UniqueOpts {
	return &queue.UniqueOpts{
		ByArgs: true, // Each item can only have one pending cleanup
		// No time period - each item can only have one pending cleanup
		ByState: []queue.JobState{
			queue.JobStateAvailable,
			queue.JobStateScheduled,
			queue.JobStateRunning,
			queue.JobStateRetryable,
			queue.JobStatePending,
		},
	}
}
