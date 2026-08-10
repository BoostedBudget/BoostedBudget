// Package jobs contains River job definitions and workers for async processing.
package jobs

import (
	"context"
	"fmt"
	"log/slog"
	"time"

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

	holdingRepo  repository.HoldingRepository
	snapshotRepo repository.PortfolioSnapshotRepository
	logger       *slog.Logger
}

// PortfolioSnapshotWorkerDeps contains the dependencies for PortfolioSnapshotWorker.
type PortfolioSnapshotWorkerDeps struct {
	HoldingRepo  repository.HoldingRepository
	SnapshotRepo repository.PortfolioSnapshotRepository
	Logger       *slog.Logger
}

// NewPortfolioSnapshotWorker creates a new PortfolioSnapshotWorker.
func NewPortfolioSnapshotWorker(deps PortfolioSnapshotWorkerDeps) *PortfolioSnapshotWorker {
	logger := deps.Logger
	if logger == nil {
		logger = slog.Default()
	}
	return &PortfolioSnapshotWorker{
		holdingRepo:  deps.HoldingRepo,
		snapshotRepo: deps.SnapshotRepo,
		logger:       logger,
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

	// Step 2: Upsert snapshot for each user (today UTC, idempotent).
	today := time.Now().UTC().Truncate(24 * time.Hour)
	var failCount int
	for _, userID := range userIDs {
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
		return fmt.Errorf("portfolio snapshot: %d/%d upserts failed", failCount, len(userIDs))
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
