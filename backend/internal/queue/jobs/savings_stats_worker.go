// Package jobs contains River job definitions and workers for async processing.
package jobs

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/observability"
	"github.com/riverqueue/river"
)

// SavingsStatsArgs contains the arguments for the savings stats snapshot job.
// This job runs daily, computing and storing aggregate savings impact stats
// across all users who have opted into aggregate statistics.
//
// Issue #972: Savings Transfer Automation — daily aggregate stats worker.
type SavingsStatsArgs struct {
	// TriggerSource indicates what triggered this job (e.g., "periodic", "manual")
	TriggerSource string `json:"trigger_source,omitempty"`
}

// Kind returns the unique identifier for this job type.
func (SavingsStatsArgs) Kind() string { return "savings_stats" }

// SavingsStatsAggregateRepository defines the read method needed by the worker
// to compute the aggregate stats from the transfer_suggestions + privacy_settings tables.
type SavingsStatsAggregateRepository interface {
	// GetAggregateSavingsStats returns total_amount, user_count, and completed_count
	// for all completed suggestions where include_in_aggregate_stats = true.
	GetAggregateSavingsStats(ctx context.Context) (totalAmount float64, userCount int, completedCount int, err error)
}

// SavingsImpactWriteRepository defines the write method needed by the worker.
type SavingsImpactWriteRepository interface {
	// WriteSnapshot inserts or updates the daily aggregate snapshot (idempotent per UTC date).
	WriteSnapshot(ctx context.Context, snapshot *domain.SavingsImpactSnapshot) error
}

// SavingsStatsWorker computes and stores daily aggregate savings impact snapshots.
// It reads aggregate stats from the DB (via AggregateRepo) and writes the snapshot
// via SavingsImpactRepo. The ON CONFLICT DO UPDATE in the repo makes re-runs idempotent.
type SavingsStatsWorker struct {
	river.WorkerDefaults[SavingsStatsArgs]

	aggregateRepo    SavingsStatsAggregateRepository
	savingsImpactRepo SavingsImpactWriteRepository
	logger           *slog.Logger
}

// SavingsStatsWorkerDeps contains the dependencies for SavingsStatsWorker.
type SavingsStatsWorkerDeps struct {
	AggregateRepo    SavingsStatsAggregateRepository
	SavingsImpactRepo SavingsImpactWriteRepository
	Logger           *slog.Logger
}

// NewSavingsStatsWorker creates a new SavingsStatsWorker.
func NewSavingsStatsWorker(deps SavingsStatsWorkerDeps) *SavingsStatsWorker {
	logger := deps.Logger
	if logger == nil {
		logger = slog.Default()
	}
	return &SavingsStatsWorker{
		aggregateRepo:    deps.AggregateRepo,
		savingsImpactRepo: deps.SavingsImpactRepo,
		logger:           logger,
	}
}

// Work processes a savings stats snapshot job.
//
// Algorithm:
//  1. Query aggregate stats (total_amount, user_count, completed_count) from DB
//     via AggregateRepo. Only includes users with include_in_aggregate_stats = true.
//  2. Write snapshot via SavingsImpactRepo.WriteSnapshot — the ON CONFLICT DO UPDATE
//     makes this idempotent per UTC date.
//  3. Emit observability metrics.
func (w *SavingsStatsWorker) Work(ctx context.Context, job *river.Job[SavingsStatsArgs]) (err error) {
	args := job.Args

	w.logger.Info("starting savings stats snapshot",
		"job_id", job.ID,
		"trigger_source", args.TriggerSource,
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

	// Step 1: Query aggregate stats
	totalAmount, userCount, completedCount, err := w.aggregateRepo.GetAggregateSavingsStats(ctx)
	if err != nil {
		w.logger.Error("failed to query aggregate savings stats",
			"job_id", job.ID,
			"error", err,
		)
		return fmt.Errorf("failed to query aggregate savings stats: %w", err)
	}

	w.logger.Info("aggregate savings stats computed",
		"job_id", job.ID,
		"total_amount", totalAmount,
		"user_count", userCount,
		"completed_count", completedCount,
	)

	// Step 2: Write snapshot (idempotent upsert per UTC date)
	snapshot := &domain.SavingsImpactSnapshot{
		ComputedAt:     time.Now().UTC(),
		TotalAmount:    totalAmount,
		UserCount:      userCount,
		CompletedCount: completedCount,
	}

	if err = w.savingsImpactRepo.WriteSnapshot(ctx, snapshot); err != nil {
		w.logger.Error("failed to write savings impact snapshot",
			"job_id", job.ID,
			"error", err,
		)
		return fmt.Errorf("failed to write savings impact snapshot: %w", err)
	}

	w.logger.Info("savings stats snapshot written",
		"job_id", job.ID,
		"total_amount", totalAmount,
		"user_count", userCount,
		"completed_count", completedCount,
		"duration_ms", time.Since(start).Milliseconds(),
	)

	return nil
}

