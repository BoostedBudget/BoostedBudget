// Package jobs contains River job definitions and workers for async processing.
package jobs

import (
	"context"
	"errors"
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
//  2. Calls PlaidItemCleanupService.RemoveItem() to call Plaid /item/remove, handing it that SAME
//     eligibility question as a callback so it is asked again at the point of use (Codex pass-5
//     Medium 2 — step 1's claim grants no exclusivity past its own statement, and RemoveItem does
//     its own read before calling Plaid).
//  3. Clears the cleanup schedule and hard-deletes the linked_items row (both best-effort)
//  4. Logs the cleanup for audit
//
// If the item is no longer a candidate — the user reconnected it through Plaid Link update mode,
// its accounts came back, or its error cleared — EITHER check point declines, the stale schedule is
// released (releaseDeclinedCleanupSchedule, one path for both), and the job returns nil WITHOUT
// calling Plaid. What it does NOT do is check "is the grace period expired": that is River's job,
// not this worker's (the sweep enqueues with WithScheduledAt), and nothing here re-derives it.
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
	// Nothing marks that repair on the cleanup schedule. Issue #1486 follow-up part E: there are
	// FOUR production callers of ClearCleanupSchedule, not two — re-derive with
	// `grep -rn '\.ClearCleanupSchedule(' internal | grep -v _test.go`, and see
	// TestClearCleanupScheduleProductionCallers_MatchTheDocumentedSet (item_cleanup_test.go), which
	// fails if this enumeration drifts again:
	//   1. this function, after a successful RemoveItem (post-hoc state cleanup);
	//   2. this file's releaseDeclinedCleanupSchedule, releasing the stale schedule when the
	//      eligibility question is answered "no" — by THIS claim at selection time (#1486) or by the
	//      same predicate at the point of use inside RemoveItem (Codex pass-5 Medium 2). Two callers
	//      of one helper, so still ONE call site in this file, which is what the guard test counts;
	//   3. accountService.DeleteAccount (services/account_service.go), releasing the stamp it just
	//      set when the zero-account cleanup enqueue fails;
	//   4. ItemCleanupCheckWorker.Work (item_cleanup_check.go), releasing the stamp it just set when
	//      the sweep's own enqueue fails (#1486 follow-up part D).
	// The load-bearing half is unchanged and still true: none of the four is a reconnection hook —
	// NOTHING clears cleanup_scheduled_at on reconnection. So a
	// reconnected item still carries cleanup_scheduled_at, and a recheck of "is it still
	// scheduled" would authorise removing it. The recheck therefore re-asks the sweep's own
	// question — "is this still a cleanup candidate?" — against the same shared SQL predicate the
	// sweep selected it with (repository.cleanupCandidateReasonPredicate).
	//
	// The recheck closure is defined once and reused for both the initial claim and the callback
	// passed to RemoveItem, eliminating duplication of the eligibility question.
	recheckEligibility := func(rctx context.Context) (bool, error) {
		return w.linkedItemRepo.ClaimItemForCleanup(rctx, args.LinkedItemID, w.errorDaysThreshold, w.staleDaysThreshold)
	}
	claimed, claimErr := recheckEligibility(ctx)
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
		return w.releaseDeclinedCleanupSchedule(ctx, job.ID, args, "the claim declined at selection time")
	}

	// Call cleanup service to remove the item from Plaid.
	//
	// Issue #1486 follow-up parts A+B: RemoveItem no longer swallows Plaid failures, so a nil return here now
	// genuinely means "this Item is gone from Plaid and billing has stopped" — either /item/remove
	// succeeded or Plaid answered ITEM_NOT_FOUND, and RemoveItem has already recorded 'disconnected'
	// on the row. Everything below the error branch depends on that, so do NOT restore the swallow:
	// it made this worker delete the local row while the Item was still live and still billing,
	// which is how a GDPR deletion could complete past DeleteUserCascade's C2 guard with an
	// unfindable, still-billing Plaid Item left behind.
	// The SAME eligibility question, asked again at the point of use (Codex pass-5 Medium 2). The
	// claim above is a conditional UPDATE and grants no exclusivity beyond its own statement (see
	// ClaimItemForCleanup's doc comment), and RemoveItem performs its own read before presenting the
	// token to Plaid — so a connection repair or an active-account write can commit in between, and
	// this worker would disconnect an item that had just become healthy. Passing the identical
	// predicate (not a second notion of eligibility, and not with different thresholds) means the two
	// check points cannot disagree; a repeat claim is safe to run because it consumes nothing and
	// writes nothing when it declines.
	removeErr := w.plaidCleanupSvc.RemoveItem(ctx, args.LinkedItemID, recheckEligibility)
	if removeErr != nil {
		// A use-time decline is NOT a Plaid failure: nothing was sent to Plaid and nothing was
		// recorded (see ErrItemNoLongerEligible). It gets the identical handling as a decline at
		// selection time — the same helper, so there is one behaviour here and not two.
		if errors.Is(removeErr, services.ErrItemNoLongerEligible) {
			return w.releaseDeclinedCleanupSchedule(ctx, job.ID, args, "the item stopped being a cleanup candidate between the claim and the removal")
		}
		// The LOCAL row is missing (repository.ErrLinkedItemNotFound, wrapped out of RemoveItem's
		// GetByID) — note this is a DIFFERENT error from Plaid's ITEM_NOT_FOUND, which RemoveItem now
		// handles itself as success and never surfaces here. With no row there is no access token to
		// send to Plaid and nothing to clean up, so this is a success with nothing to do; the Delete
		// below is a belt-and-braces no-op.
		if services.IsItemNotFoundError(removeErr) {
			w.logger.Info("linked item row is gone, marking job as complete",
				"job_id", job.ID,
				"linked_item_id", args.LinkedItemID,
			)
			if deleteErr := w.linkedItemRepo.Delete(ctx, args.LinkedItemID); deleteErr != nil {
				w.logger.Warn("failed to delete linked item from database after item-not-found",
					"linked_item_id", args.LinkedItemID,
					"error", deleteErr,
				)
			}
			return nil
		}

		// A genuine failure: the Item may still be LIVE and still billing. Return without touching
		// the local row — the row and its cleanup_scheduled_at are the only remaining evidence the
		// Item exists, and the C2 guard reads exactly that row to refuse an orphaning deletion.
		// River retries; if all attempts are exhausted, ItemCleanupCheckWorker's pending-cleanup
		// backstop (#1486 follow-up part C) re-enqueues from the still-stamped row on the next daily
		// sweep.
		//
		// ⚠ That is RECOVERY FOR A TRANSIENT FAILURE ONLY, and the first version of this comment
		// stopped at the sentence above as though it covered every case. It does not. If the removal
		// can NEVER succeed for this item — a permanently invalid or empty access token, see
		// PlaidItemCleanupService.RemoveItem's ⚠ note — then "River retries, the backstop re-enqueues"
		// describes an INDEFINITE daily loop, not a recovery.
		//
		// ISSUE #1494 CHANGED WHAT THAT LOOP COSTS, and this is the correction: it used to also hold
		// that user's DeleteUserCascade at (false, nil) PERMANENTLY. It no longer does. RemoveItem now
		// records the verdict (plaid_removal_failed_at) for the narrow set of failures that prove the
		// credential unusable, uncleanedLinkedItemExistsClause stops blocking on such a row, and the
		// cascade preserves the Item in plaid_orphaned_items before deleting anything. What survives
		// unchanged — deliberately — is THIS loop: the row keeps being re-attempted for as long as it
		// exists, because a recorded verdict is permission for the erasure to proceed, never permission
		// to stop trying. So the daily re-enqueue is now the self-healing mechanism against a
		// misclassification, and its residual cost is one wasted /item/remove per day plus a
		// dead-lettered job per pass — visible, bounded, and no longer a GDPR block. Tracked as the
		// remaining #1494 follow-up: alerting on a repeatedly re-enqueued item, and the admin view of
		// plaid_orphaned_items.
		//
		// Issue #1495 WIDENED the population this paragraph is about: uncleanedLinkedItemExistsClause
		// blocks on plaid_removed_at rather than status, so 'replaced' rows (which it previously waved
		// through, letting an erasure complete over a live, billing Item) are inside it too. That is
		// the intended, fail-closed direction.
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

	// Clear the cleanup schedule since cleanup is complete (best-effort).
	//
	// Best-effort is safe for any row RemoveItem actually stamped (#1486 follow-up part A, corrected
	// by Issue #1495): a cleanup_scheduled_at left set on such a row is inert rather than hazardous,
	// because cleanupCandidateSelectQuery, GetItemsPendingCleanup and uncleanedLinkedItemExistsClause
	// ALL now key off linked_items.plaid_removed_at — so no sweep re-selects it, part C's backstop
	// does not spin on it, and DeleteUserCascade is not blocked. That was NOT true before any marker
	// existed, when a swallowed failure here left a row the sweep could never re-select.
	//
	// ISSUE #1495 REMOVED THE ⚠ EXCEPTION THAT USED TO BE DOCUMENTED HERE, and what it was is worth
	// recording, because it is the reason a new column was added rather than a branch. For a
	// 'replaced' row the old marker never landed: MarkItemsDisconnected's WHERE guard excludes
	// 'replaced' (deliberately — it is a terminal soft-delete state from a re-link, pinned by
	// TestLinkedItemRepository_MarkItemsDisconnected_AlreadyReplacedIsNotReverted), so the write
	// affected zero rows, RemoveItem treated ErrNoLinkedItemsUpdated as the legitimate no-op it is,
	// and the row kept 'replaced'. #1486 fix round 1 had made GetItemsPendingCleanup INCLUDE
	// 'replaced' (correctly — such an item is still live at Plaid until removed), so the backstop
	// re-selected the row on every run until THIS clear happened to succeed: one /item/remove per run
	// answering ITEM_NOT_FOUND, self-healing but unbounded in time. The root cause was structural —
	// status is a single slot and the re-link fact already owned it, so there was NOWHERE to record
	// "Plaid side done". RemoveItem now stamps plaid_removed_at first, which a 'replaced' row CAN
	// carry, so such a row leaves every selection predicate as soon as removal is recorded. The clear
	// stays best-effort and is now genuinely inert to lose for every row shape, not all but one.
	// Overwriting 'replaced' with 'disconnected' is still NOT the fix and must not be attempted (it
	// would destroy replaced_at/replaced_by_id's meaning and reverse the pinned decision above).
	if clearErr := w.linkedItemRepo.ClearCleanupSchedule(ctx, args.LinkedItemID); clearErr != nil {
		w.logger.Warn("failed to clear cleanup schedule",
			"linked_item_id", args.LinkedItemID,
			"error", clearErr,
		)
	}

	// Delete the linked_items row now that Plaid billing really is stopped (best-effort).
	//
	// #1486 follow-up: "now that Plaid billing is stopped" used to be an assertion this code had no grounds
	// for — RemoveItem returned nil on a failed /item/remove. It is now a consequence of RemoveItem's
	// contract (success means removed-or-already-gone at Plaid). Best-effort is likewise safe now:
	// Delete is a deliberate no-op whenever accounts still reference the item, so the row often
	// survives regardless, and it survives carrying 'disconnected' — which every downstream predicate
	// reads correctly. For the 'replaced' exception to that last clause, and why it is bounded, see
	// the ⚠ note on ClearCleanupSchedule just above.
	//
	// ⚠ THE LAST CLAUSE OF THAT SENTENCE USED TO READ "since C2 excludes 'replaced' as well as
	// 'disconnected'", and Issue #1495 made it FALSE — uncleanedLinkedItemExistsClause reads no status
	// at all now, and a 'replaced' row blocks like any other until its removal is recorded. Corrected
	// rather than deleted because the survival IS still harmless, for a different and stronger reason:
	// this line is reached only after RemoveItem stamped plaid_removed_at, which is the fact every
	// downstream predicate keys off, whatever status the surviving row carries.
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

// releaseDeclinedCleanupSchedule is the single decline path for an item this worker has decided NOT
// to remove, whichever of the TWO check points declined it (Issue #1486; Codex pass-5 Medium 2 added
// the second):
//
//  1. ClaimItemForCleanup declining at selection time, before RemoveItem is called at all;
//  2. the same predicate declining at the point of use, inside RemoveItem, immediately before the
//     Plaid call — the window the claim's own statement cannot cover.
//
// Both mean the same thing (the user repaired the connection through Plaid Link update mode, its
// accounts came back, or its error cleared) and both must do the same thing, which is why this is one
// function and not two similar blocks that can drift apart.
//
// It releases the now-stale schedule. Chosen over leaving it set: the sweep's candidate predicate
// requires cleanup_scheduled_at IS NULL, so an item that keeps a schedule it will never act on is
// invisible to every future sweep — if it degrades again later it would never be cleaned up, and
// Plaid would bill for it indefinitely. Keeping the row as an audit trail is not worth that: the
// audit record lives in this log line and in the completed River job, neither of which the clear
// touches.
//
// A failure here is RETURNED, not swallowed. Returning nil would let River record success and never
// come back, permanently creating the exact state the paragraph above calls unacceptable. The retry
// is not a pointless re-run of a no-op: the rechecks it repeats are cheap and idempotent, and the
// clear — the part that actually failed — is what the retry is for. Removal stays off the table
// either way, because both check points will decline again.
func (w *ItemCleanupWorker) releaseDeclinedCleanupSchedule(ctx context.Context, jobID int64, args ItemCleanupArgs, declinedBecause string) error {
	w.logger.Info("item is no longer a cleanup candidate, skipping Plaid removal",
		"job_id", jobID,
		"linked_item_id", args.LinkedItemID,
		"user_id", args.UserID,
		"reason", args.Reason,
		"declined_because", declinedBecause,
	)

	if clearErr := w.linkedItemRepo.ClearCleanupSchedule(ctx, args.LinkedItemID); clearErr != nil {
		w.logger.Error("failed to clear stale cleanup schedule for a no-longer-eligible item, retrying",
			"job_id", jobID,
			"linked_item_id", args.LinkedItemID,
			"error", clearErr,
		)
		return fmt.Errorf("failed to clear stale cleanup schedule for item %s: %w", args.LinkedItemID, clearErr)
	}

	// A no-longer-eligible item whose schedule was released is a SUCCESS with nothing to do, not a
	// failure.
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
