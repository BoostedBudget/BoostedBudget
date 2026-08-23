// Package jobs contains River job definitions and workers for async processing.
package jobs

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/observability"
	"github.com/maxnussbaum/BoostedBudget/internal/queue"
	"github.com/maxnussbaum/BoostedBudget/internal/repository"
	"github.com/riverqueue/river"
)

// PortfolioSnapshotArgs contains the arguments for the portfolio snapshot job.
// This job runs daily, computing and upserting per-user portfolio value snapshots
// from committed holdings state across all users that have holdings.
//
// Issue #138: Investment Backend — portfolio_snapshots + API endpoints.
type PortfolioSnapshotArgs struct {
	// TriggerSource indicates what triggered this job (e.g., "periodic", "manual")
	TriggerSource string `json:"trigger_source,omitempty"`
}

// Kind returns the unique identifier for this job type.
func (PortfolioSnapshotArgs) Kind() string { return "portfolio_snapshot" }

// PortfolioSnapshotWorker computes and stores daily per-user portfolio snapshots.
// It iterates all users with holdings and upserts today's snapshot via an aggregate
// SELECT ... INSERT so each snapshot reflects committed holdings state atomically.
// Runs via River periodic job + is also triggered per-user after each holdings sync.
type PortfolioSnapshotWorker struct {
	river.WorkerDefaults[PortfolioSnapshotArgs]

	holdingRepo         repository.HoldingRepository
	snapshotRepo        repository.PortfolioSnapshotRepository
	holdingSnapshotRepo repository.HoldingSnapshotRepository
	logger              *slog.Logger
}

// PortfolioSnapshotWorkerDeps contains the dependencies for PortfolioSnapshotWorker.
type PortfolioSnapshotWorkerDeps struct {
	HoldingRepo  repository.HoldingRepository
	SnapshotRepo repository.PortfolioSnapshotRepository
	// HoldingSnapshotRepo records the row-level history behind each aggregate (#1569 T6).
	// Optional: nil skips the capture rather than failing the job.
	HoldingSnapshotRepo repository.HoldingSnapshotRepository
	Logger              *slog.Logger
}

// NewPortfolioSnapshotWorker creates a new PortfolioSnapshotWorker.
func NewPortfolioSnapshotWorker(deps PortfolioSnapshotWorkerDeps) *PortfolioSnapshotWorker {
	logger := deps.Logger
	if logger == nil {
		logger = slog.Default()
	}
	return &PortfolioSnapshotWorker{
		holdingRepo:         deps.HoldingRepo,
		snapshotRepo:        deps.SnapshotRepo,
		holdingSnapshotRepo: deps.HoldingSnapshotRepo,
		logger:              logger,
	}
}

// Work processes a portfolio snapshot job.
//
// Algorithm:
//  1. Enumerate all distinct user IDs that currently have holdings.
//  2. For each user, call PortfolioSnapshotRepository.Upsert (aggregate-at-write SQL)
//     to write today's snapshot. The ON CONFLICT DO UPDATE makes reruns idempotent.
//  3. Emit observability metrics.
//
// Per-user errors are logged and counted but do not abort the run — a single user's
// failure should not prevent the remaining users from getting a snapshot.
func (w *PortfolioSnapshotWorker) Work(ctx context.Context, job *river.Job[PortfolioSnapshotArgs]) (err error) {
	args := job.Args

	w.logger.Info("starting portfolio snapshot job",
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

	// Step 0 (#1569 Task 11, S6/E4): repair the PAST days that were recorded before the account
	// liveness predicate was corrected. Bounded, ledger-gated and idempotent — see
	// repository.RepairUserFromHistory. It runs here because this job already runs daily and
	// already holds the snapshot repository, and because a day it does not reach today is still
	// unexamined tomorrow, so the pass needs no retry logic of its own.
	w.repairRecordedHistory(ctx, job.ID)

	// Step 1: Get all users with holdings.
	userIDs, err := w.holdingRepo.GetDistinctUserIDsWithHoldings(ctx)
	if err != nil {
		w.logger.Error("failed to enumerate users with holdings",
			"job_id", job.ID,
			"error", err,
		)
		return fmt.Errorf("portfolio snapshot: enumerate users: %w", err)
	}

	w.logger.Info("portfolio snapshot: users enumerated",
		"job_id", job.ID,
		"user_count", len(userIDs),
	)

	if len(userIDs) == 0 {
		w.logger.Info("portfolio snapshot: no users with holdings, skipping", "job_id", job.ID)
		return nil
	}

	// Step 2: Capture the row-level history and upsert the aggregate for each user (today UTC,
	// idempotent).
	//
	// goClockToday is only the fallback for a build where the history repository is not wired.
	// When it IS wired, the day comes back from CaptureForUser — i.e. from the SERVER — and the
	// aggregate is filed under that same day. Taking the aggregate's day from the Go clock while
	// the rows took theirs from Postgres meant that across UTC midnight, with the ~16 ms
	// host/container skew this project already documents, the aggregate and the rows behind it
	// landed on DIFFERENT days: precisely the "aggregate with no rows behind it" state this
	// capture exists to prevent.
	goClockToday := time.Now().UTC().Truncate(24 * time.Hour)
	var failCount int
	for _, userID := range userIDs {
		today := goClockToday

		// #1569 T6: record the row-level history FIRST and SKIP the aggregate if it fails. A
		// failure here loses a day of history — the exact loss this task exists to stop — so it
		// counts as a job failure and River retries (the capture is idempotent within the UTC
		// day). Writing the aggregate anyway would manufacture the very state the capture is
		// here to prevent: a portfolio_snapshots row with nothing behind it for task 11 to
		// rebuild from. One user's failure never aborts the rest.
		if w.holdingSnapshotRepo != nil {
			capturedDay, capErr := w.holdingSnapshotRepo.CaptureForUser(ctx, userID)
			if capErr != nil {
				w.logger.Error("holding snapshot capture failed for user; skipping this user's aggregate",
					"job_id", job.ID,
					"user_id", userID,
					"error", capErr,
				)
				failCount++
				continue
			}
			today = capturedDay
		}

		if _, upsertErr := w.snapshotRepo.Upsert(ctx, userID, today); upsertErr != nil {
			w.logger.Error("portfolio snapshot upsert failed for user",
				"job_id", job.ID,
				"user_id", userID,
				"error", upsertErr,
			)
			failCount++
		}
	}

	w.logger.Info("portfolio snapshot job complete",
		"job_id", job.ID,
		"total_users", len(userIDs),
		"failed_users", failCount,
		"duration_ms", time.Since(start).Milliseconds(),
	)

	if failCount > 0 {
		return fmt.Errorf("portfolio snapshot: %d/%d per-user writes failed", failCount, len(userIDs))
	}
	return nil
}

// PortfolioSnapshotUniqueOpts returns the recommended uniqueness options. Constrains to one
// snapshot job per 24h period — defense in depth on top of Upsert's own ON CONFLICT DO
// UPDATE (idempotent per user per day).
//
// Issue #1448: main.go's periodic registration previously built this ByPeriod-only
// river.UniqueOpts{} literal inline with no named helper.
func PortfolioSnapshotUniqueOpts() *queue.UniqueOpts {
	return &queue.UniqueOpts{
		ByPeriod: 24 * time.Hour,
	}
}

// PortfolioSnapshotInsertOpts returns the *river.InsertOpts this job's periodic
// registration in main.go must use — Queue: queue.QueueLow plus the uniqueness options from
// PortfolioSnapshotUniqueOpts(), converted to river's UniqueOpts type.
func PortfolioSnapshotInsertOpts() *river.InsertOpts {
	return riverInsertOptsFromUnique(queue.QueueLow, PortfolioSnapshotUniqueOpts())
}

// PortfolioSnapshotPeriodicOpts returns the *river.PeriodicJobOpts this job's periodic
// registration in main.go must use. RunOnStart: true is safe because
// PortfolioSnapshotInsertOpts() above carries PortfolioSnapshotUniqueOpts()'s ByPeriod
// dedup AND Upsert's own upsert is naturally idempotent. ID is set so River restores
// nextRunAt across restarts/leader elections instead of recomputing now+interval every
// time (Issue #1448).
func PortfolioSnapshotPeriodicOpts() *river.PeriodicJobOpts {
	return &river.PeriodicJobOpts{RunOnStart: true, ID: PortfolioSnapshotArgs{}.Kind()}
}

// portfolioRepairUsersPerRun bounds one run's repair pass. The pass is resumable by construction —
// a user it does not reach keeps their unexamined days and is picked up on the next daily run — so
// the cap costs nothing but keeps a first run over a large backlog from stretching the daily job.
const portfolioRepairUsersPerRun = 200

// repairRecordedHistory runs the #1569 Task 11 repair over the users the ledger still owes a
// verdict.
//
// It NEVER fails the job, deliberately. The aggregate write is this job's contract; the repair is
// catch-up work over rows that are already wrong and will still be there tomorrow. Returning an
// error here would make River retry the capture and the aggregate as well, and would eventually
// dead-letter a job whose actual work succeeded — trading a stale historical row for a missing
// current one.
//
// The user list comes from UserIDsWithUnexaminedSnapshots, NOT from the holdings enumeration this
// job uses for its own work: that one joins a.status = 'active' and so skips a user who has
// disconnected every institution, whose recorded curve is the one most likely to be wrong.
func (w *PortfolioSnapshotWorker) repairRecordedHistory(ctx context.Context, jobID int64) {
	userIDs, err := w.snapshotRepo.UserIDsWithUnexaminedSnapshots(ctx, portfolioRepairUsersPerRun)
	if err != nil {
		w.logger.Error("#1569 portfolio history repair: could not enumerate users; skipping this run",
			"job_id", jobID, "error", err)
		return
	}
	if len(userIDs) == 0 {
		return
	}

	var total domain.PortfolioRepairSummary
	var failed int
	for _, userID := range userIDs {
		summary, repairErr := w.snapshotRepo.RepairUserFromHistory(ctx, userID)
		if repairErr != nil {
			w.logger.Error("#1569 portfolio history repair failed for user; their days stay unexamined",
				"job_id", jobID, "user_id", userID, "error", repairErr)
			failed++
			continue
		}
		total.Merge(summary)
	}

	w.logger.Info("#1569 portfolio history repair pass complete",
		"job_id", jobID,
		"users", len(userIDs),
		"failed_users", failed,
		"days_examined", total.Examined,
		"days_repaired", total.Repaired,
		"days_verified", total.Verified,
		"days_refused_no_coverage", total.RefusedNoCoverage,
		"days_refused_partial_coverage", total.RefusedPartialCoverage,
	)
}
