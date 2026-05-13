package jobs

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/maxnussbaum/BoostedBudget/internal/observability"
	"github.com/riverqueue/river"
)

// EffectiveCategoryRefreshArgs is the job payload for refreshing effective_category_name
// for all transactions assigned to a renamed custom category.
// Issue #1123: Async cascade rename of effective_category_name on custom category update.
type EffectiveCategoryRefreshArgs struct {
	UserID           string `json:"user_id"`
	CustomCategoryID string `json:"custom_category_id"`
	NewName          string `json:"new_name"`
}

func (EffectiveCategoryRefreshArgs) Kind() string { return "effective_category_refresh" }

// effectiveCategoryUpdater is the minimal repository interface needed by the worker.
// Using a narrow interface instead of the full TransactionRepository keeps the worker
// decoupled and makes tests simpler.
type effectiveCategoryUpdater interface {
	UpdateEffectiveCategoryNameByCustomCategoryID(ctx context.Context, userID, customCategoryID, newName string) (int64, error)
}

// EffectiveCategoryRefreshWorker handles the async update of effective_category_name
// after a custom category is renamed.
type EffectiveCategoryRefreshWorker struct {
	river.WorkerDefaults[EffectiveCategoryRefreshArgs]

	transactionRepo effectiveCategoryUpdater
	logger          *slog.Logger
}

// EffectiveCategoryRefreshWorkerDeps holds dependencies for EffectiveCategoryRefreshWorker.
type EffectiveCategoryRefreshWorkerDeps struct {
	// TransactionRepo only needs UpdateEffectiveCategoryNameByCustomCategoryID.
	// The full repository.TransactionRepository satisfies this interface.
	TransactionRepo effectiveCategoryUpdater
	Logger          *slog.Logger
}

// NewEffectiveCategoryRefreshWorker constructs a worker from its dependencies.
func NewEffectiveCategoryRefreshWorker(deps EffectiveCategoryRefreshWorkerDeps) *EffectiveCategoryRefreshWorker {
	logger := deps.Logger
	if logger == nil {
		logger = slog.Default()
	}
	return &EffectiveCategoryRefreshWorker{
		transactionRepo: deps.TransactionRepo,
		logger:          logger,
	}
}

// Work executes the effective category refresh job.
func (w *EffectiveCategoryRefreshWorker) Work(ctx context.Context, job *river.Job[EffectiveCategoryRefreshArgs]) (err error) {
	args := job.Args

	w.logger.Info("starting effective category refresh job",
		"job_id", job.ID,
		"user_id", args.UserID,
		"custom_category_id", args.CustomCategoryID,
		"new_name", args.NewName,
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

	rowsUpdated, err := w.transactionRepo.UpdateEffectiveCategoryNameByCustomCategoryID(
		ctx, args.UserID, args.CustomCategoryID, args.NewName,
	)
	if err != nil {
		w.logger.Error("effective category refresh failed",
			"job_id", job.ID,
			"user_id", args.UserID,
			"custom_category_id", args.CustomCategoryID,
			"error", err,
			"duration_ms", time.Since(start).Milliseconds(),
		)
		return fmt.Errorf("effective category refresh failed: %w", err)
	}

	w.logger.Info("effective category refresh completed",
		"job_id", job.ID,
		"user_id", args.UserID,
		"custom_category_id", args.CustomCategoryID,
		"rows_updated", rowsUpdated,
		"duration_ms", time.Since(start).Milliseconds(),
	)

	return nil
}
