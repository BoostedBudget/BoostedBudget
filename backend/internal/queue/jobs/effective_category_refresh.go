package jobs

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/maxnussbaum/BoostedBudget/internal/cache"
	"github.com/maxnussbaum/BoostedBudget/internal/observability"
	"github.com/riverqueue/river"
)

// bumpCacheTimeout bounds the detached post-commit bump work below, matching the same constant
// name/value used throughout internal/services (e.g. transaction_sync_service.go). River cancels
// a job's context on shutdown, retry, or timeout — a cancellation landing in the narrow window
// after this job's write has committed but before the bump runs must not take the bump down with
// it. Issue #1531 Final fixes.
const bumpCacheTimeout = 5 * time.Second

// EffectiveCategoryRefreshArgs is the job payload for refreshing effective_category_name
// for all transactions assigned to a renamed custom category.
// Issue #1123: Async cascade rename of effective_category_name on custom category update.
type EffectiveCategoryRefreshArgs struct {
	UserID           string `json:"user_id"`
	CustomCategoryID string `json:"custom_category_id"`
}

func (EffectiveCategoryRefreshArgs) Kind() string { return "effective_category_refresh" }

// effectiveCategoryUpdater is the minimal repository interface needed by the worker.
type effectiveCategoryUpdater interface {
	UpdateEffectiveCategoryNameByCustomCategoryID(ctx context.Context, userID, customCategoryID string) (int64, error)
}

// EffectiveCategoryRefreshWorker handles async updates of effective_category_name.
type EffectiveCategoryRefreshWorker struct {
	river.WorkerDefaults[EffectiveCategoryRefreshArgs]

	transactionRepo effectiveCategoryUpdater
	logger          *slog.Logger
	// cacheGens is optional (nil = cache invalidation disabled/not wired); see
	// SetCacheInvalidation. Issue #1531 Task 10b-2a-iii.
	cacheGens *cache.Generations
}

// EffectiveCategoryRefreshWorkerDeps holds dependencies for EffectiveCategoryRefreshWorker.
type EffectiveCategoryRefreshWorkerDeps struct {
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

// SetCacheInvalidation wires cache generation bumping (setter injection, matching this
// codebase's established convention). Issue #1531 Task 10b-2a-iii: this job directly rewrites
// transactions.effective_category_name — the very field several cached analytics queries GROUP
// BY (spending by category/merchant, budget vs actual) — with no bump anywhere. Optional: nil is
// a safe no-op.
func (w *EffectiveCategoryRefreshWorker) SetCacheInvalidation(gens *cache.Generations) {
	w.cacheGens = gens
}

// bumpCacheForUser invalidates userID's cached analytics after this job's rename cascade has
// committed. Void by design, matching every other generation bump in this codebase.
//
// Issue #1531 Final fixes: runs on context.WithTimeout(context.WithoutCancel(ctx), bumpCacheTimeout)
// rather than the raw job ctx. River cancels a job's context on shutdown, retry, or timeout — if
// that cancellation lands after UpdateEffectiveCategoryNameByCustomCategoryID has already
// committed but before this bump runs, the raw ctx would carry the cancellation into
// cacheGens.Bump, which swallows the resulting error silently: the rename commits, the bump never
// happens, and cached spending-by-category/merchant and budget-vs-actual analytics (which GROUP
// BY this exact column) serve stale values until TTL. WithoutCancel alone would leave the bump
// unbounded (it strips the parent's deadline too), hence the explicit WithTimeout — matching
// household_service.go's bumpCacheForUser and the sync services' bumpCacheForSync.
func (w *EffectiveCategoryRefreshWorker) bumpCacheForUser(ctx context.Context, userID string) {
	if w.cacheGens == nil {
		return
	}
	bumpCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), bumpCacheTimeout)
	defer cancel()
	w.cacheGens.Bump(bumpCtx, cache.ScopeUser, userID, "effective_category_refresh")
}

// Work executes the effective category refresh job.
func (w *EffectiveCategoryRefreshWorker) Work(ctx context.Context, job *river.Job[EffectiveCategoryRefreshArgs]) (err error) {
	args := job.Args

	w.logger.Info("starting effective category refresh job",
		"job_id", job.ID,
		"user_id", args.UserID,
		"custom_category_id", args.CustomCategoryID,
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
		ctx, args.UserID, args.CustomCategoryID,
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

	// Issue #1531 Task 10b-2a-iii: bump only after the rename cascade above has committed.
	w.bumpCacheForUser(ctx, args.UserID)

	return nil
}
