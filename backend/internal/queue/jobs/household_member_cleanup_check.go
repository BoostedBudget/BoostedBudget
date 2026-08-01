// Package jobs contains River job definitions and workers for async processing.
// Issue #1371 — Phase 7: Household member purge check — permanently deletes
// household_members rows that have sat in status='removed' for 30+ days.
package jobs

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/observability"
	"github.com/maxnussbaum/BoostedBudget/internal/queue"
	"github.com/riverqueue/river"
)

// householdMemberPurgeAfterDays is how long a removed member's row lingers before this
// worker permanently purges it. Matches HouseholdService's removalPurgeWindow
// (30 days) — see household_service.go's PurgeRemovedMembership doc comment. #1371 —
// Phase 7 (Task 2).
const householdMemberPurgeAfterDays = 30

// HouseholdMemberCleanupCheckArgs contains the arguments for the household member
// cleanup check job. This job runs daily to permanently purge household_members rows
// that have been status='removed' for more than 30 days — deleting the membership row,
// its household_account_shares, and its household-scoped subscription_entitlement, but
// never the member's own accounts/transactions/budgets (#1371 Task 0's
// PurgeRemovedMembership guarantees this by construction).
type HouseholdMemberCleanupCheckArgs struct {
	// TriggerSource indicates what triggered this check (e.g., "periodic", "manual")
	TriggerSource string `json:"trigger_source,omitempty"`
}

// Kind returns the unique identifier for this job type.
func (HouseholdMemberCleanupCheckArgs) Kind() string { return "household_member_cleanup_check" }

// HouseholdMemberCleanupRepository defines the repository method needed for household
// member cleanup: finding purge candidates. Deliberately narrow — this worker must
// never touch multi-table SQL directly (that lives in HouseholdMemberPurgeService).
type HouseholdMemberCleanupRepository interface {
	// ListMembershipsPastPurgeWindow returns household_members rows with
	// status='removed' whose removed_at is older than purgeAfterDays days ago.
	ListMembershipsPastPurgeWindow(ctx context.Context, purgeAfterDays int) ([]*domain.HouseholdMember, error)
}

// HouseholdMemberPurgeService defines the purge method needed for household member
// cleanup. The worker delegates all multi-table deletion logic to this service — it
// contains no SQL of its own beyond the scan query above.
type HouseholdMemberPurgeService interface {
	// PurgeRemovedMembership permanently deletes a removed household_members row
	// (identified by its own id), along with its shares and entitlement. householdID
	// is used to lock the household row first (lock-ordering convention).
	PurgeRemovedMembership(ctx context.Context, householdID, membershipID string) error
}

// HouseholdMemberCleanupCheckWorker processes household member cleanup check jobs.
// It finds household_members rows that have been removed for 30+ days and purges them
// via HouseholdMemberPurgeService — never touching multi-table SQL itself.
type HouseholdMemberCleanupCheckWorker struct {
	river.WorkerDefaults[HouseholdMemberCleanupCheckArgs]

	repo     HouseholdMemberCleanupRepository
	purgeSvc HouseholdMemberPurgeService
	logger   *slog.Logger
}

// HouseholdMemberCleanupCheckWorkerDeps contains the dependencies for
// HouseholdMemberCleanupCheckWorker.
type HouseholdMemberCleanupCheckWorkerDeps struct {
	Repo     HouseholdMemberCleanupRepository
	PurgeSvc HouseholdMemberPurgeService
	Logger   *slog.Logger
}

// NewHouseholdMemberCleanupCheckWorker creates a new household member cleanup check worker.
func NewHouseholdMemberCleanupCheckWorker(deps HouseholdMemberCleanupCheckWorkerDeps) *HouseholdMemberCleanupCheckWorker {
	logger := deps.Logger
	if logger == nil {
		logger = slog.Default()
	}
	return &HouseholdMemberCleanupCheckWorker{
		repo:     deps.Repo,
		purgeSvc: deps.PurgeSvc,
		logger:   logger,
	}
}

// Work processes a household member cleanup check job.
//
// The worker:
//  1. Scans for household_members rows removed 30+ days ago
//  2. Calls PurgeRemovedMembership for each candidate
//
// Partial purge failures cause the job to return an error so River retries the run.
func (w *HouseholdMemberCleanupCheckWorker) Work(ctx context.Context, job *river.Job[HouseholdMemberCleanupCheckArgs]) (err error) {
	args := job.Args

	w.logger.Info("starting household member cleanup check",
		"job_id", job.ID,
		"trigger_source", args.TriggerSource,
		"attempt", job.Attempt,
	)

	start := time.Now()
	defer func() {
		status := "success"
		if err != nil {
			status = "failed"
		} else {
			observability.RecordJobSuccess(job.Kind) // Issue #1413 P4: last-success heartbeat
		}
		observability.JobsCompleted.WithLabelValues(job.Kind, job.Queue, status).Inc()
		observability.JobDuration.WithLabelValues(job.Kind).Observe(time.Since(start).Seconds())
	}()

	// Purge household members removed 30+ days ago.
	// Propagate partial failure so River retries — failed rows will be picked up again.
	if purgeErr := w.purgeExpiredMemberships(ctx, job.ID); purgeErr != nil {
		w.logger.Error("failed to process household member purge",
			"job_id", job.ID,
			"error", purgeErr,
		)
		err = purgeErr
	}

	w.logger.Info("household member cleanup check completed",
		"job_id", job.ID,
		"duration_ms", time.Since(start).Milliseconds(),
	)

	return err
}

// purgeExpiredMemberships finds household_members rows removed 30+ days ago and purges each.
func (w *HouseholdMemberCleanupCheckWorker) purgeExpiredMemberships(ctx context.Context, jobID int64) error {
	candidates, err := w.repo.ListMembershipsPastPurgeWindow(ctx, householdMemberPurgeAfterDays)
	if err != nil {
		return fmt.Errorf("failed to fetch memberships past purge window: %w", err)
	}

	if len(candidates) == 0 {
		w.logger.Info("no household memberships past purge window", "job_id", jobID)
		return nil
	}

	w.logger.Info("found household memberships past purge window", "job_id", jobID, "candidate_count", len(candidates))

	var purgedCount, errorCount int
	for _, member := range candidates {
		if purgeErr := w.purgeSvc.PurgeRemovedMembership(ctx, member.HouseholdID, member.ID); purgeErr != nil {
			w.logger.Error("failed to purge household membership",
				"job_id", jobID, "membership_id", member.ID, "error", purgeErr)
			errorCount++
			continue
		}

		w.logger.Info("household membership purged", "job_id", jobID, "membership_id", member.ID)
		purgedCount++
	}

	w.logger.Info("household membership purges processed",
		"job_id", jobID, "purged", purgedCount, "errors", errorCount)
	if errorCount > 0 {
		return fmt.Errorf("household membership purge: %d of %d purges failed (see logs for details)", errorCount, len(candidates))
	}
	return nil
}

// HouseholdMemberCleanupCheckUniqueOpts returns the recommended uniqueness options. Only
// one check job may exist in any of the listed non-terminal states AT ALL, regardless of
// how much time has elapsed since it was inserted — see
// HouseholdOverCapacityCheckUniqueOpts's doc comment for the full Round 8 rationale
// (deliberately not time-windowed via ByPeriod, which could let a still-running instance
// bridge past a period-bucket boundary and reopen a duplicate-processing window).
//
// ⚠️ DO NOT ADD queue.JobStateCompleted HERE. The final branch-wide Codex review of Issue
// #1448 flagged all ten newly-RunOnStart:true jobs for omitting Completed, and for the eight
// that carry a real ByPeriod that is correct — without it, a run that finished minutes ago
// does not suppress the start-of-process catch-up insert, so every deploy enqueues a
// duplicate. This job is one of the two exceptions, because ByPeriod is deliberately UNSET
// (above): with no time bucket, Completed would mean "a completed instance exists, therefore
// never enqueue again" — permanently, since completed rows persist. The job would run exactly
// once in the lifetime of the database and then silently stop forever.
// TestHouseholdMemberCleanupCheckUniqueOpts's `assert.Zero(t, opts.ByPeriod)` plus its
// ElementsMatch on exactly these five states is what catches that mistake; it caught this
// author making it.
//
// The RunOnStart duplicate this omission permits is bounded and harmless here: dedup still
// covers every non-terminal state, so a catch-up insert can only land once the previous run
// has already finished — never concurrently with it.
func HouseholdMemberCleanupCheckUniqueOpts() *queue.UniqueOpts {
	return &queue.UniqueOpts{
		ByArgs: false, // All check jobs are equivalent
		ByState: []queue.JobState{
			queue.JobStateAvailable,
			queue.JobStateScheduled,
			queue.JobStateRunning,
			queue.JobStateRetryable,
			queue.JobStatePending,
		},
	}
}

// HouseholdMemberCleanupCheckInsertOpts returns the *river.InsertOpts this job's periodic
// registration in main.go must use — Queue: "low" plus the uniqueness options from
// HouseholdMemberCleanupCheckUniqueOpts(), converted to river's UniqueOpts type.
//
// Issue #1448 (following on from issue1371_wiring_drift_test.go's Round 8 finding):
// main.go previously hand-duplicated this UniqueOpts shape as an inline river.UniqueOpts{}
// literal instead of calling this package's own helper, so nothing enforced the two stay in
// sync. main.go now calls this function directly.
func HouseholdMemberCleanupCheckInsertOpts() *river.InsertOpts {
	return riverInsertOptsFromUnique(queue.QueueLow, HouseholdMemberCleanupCheckUniqueOpts())
}

// HouseholdMemberCleanupCheckPeriodicOpts returns the *river.PeriodicJobOpts this job's
// periodic registration in main.go must use. RunOnStart: true is only safe because
// HouseholdMemberCleanupCheckInsertOpts() above always carries
// HouseholdMemberCleanupCheckUniqueOpts()'s dedup — previously this job used
// RunOnStart: false, meaning it could be enqueued zero times ever on a deploy cadence
// faster than its interval (Issue #1448). ID is set so River restores nextRunAt across
// restarts/leader elections instead of recomputing now+interval every time.
func HouseholdMemberCleanupCheckPeriodicOpts() *river.PeriodicJobOpts {
	return &river.PeriodicJobOpts{RunOnStart: true, ID: HouseholdMemberCleanupCheckArgs{}.Kind()}
}
