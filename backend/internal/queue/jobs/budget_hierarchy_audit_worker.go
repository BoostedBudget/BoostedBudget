// Package jobs contains River job definitions and workers for async processing.
package jobs

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/maxnussbaum/BoostedBudget/internal/cache"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/observability"
	"github.com/maxnussbaum/BoostedBudget/internal/queue"
	"github.com/maxnussbaum/BoostedBudget/internal/repository"
	"github.com/maxnussbaum/BoostedBudget/internal/services"
	"github.com/riverqueue/river"
)

// BudgetHierarchyAuditArgs is the job payload for the Issue #1630 Task B7 audit: a periodic sweep
// over budgets that domain.DuplicateParentIdentity / domain.EscapedNestedChild would refuse if
// created today, but which predate those write-time guards. See
// internal/repository/budget_hierarchy_audit.go's header comment for the full design.
type BudgetHierarchyAuditArgs struct {
	// TriggerSource indicates what triggered this job (e.g., "periodic", "manual"), mirroring
	// PortfolioSnapshotArgs.
	TriggerSource string `json:"trigger_source,omitempty"`
}

// Kind returns the unique identifier for this job type.
func (BudgetHierarchyAuditArgs) Kind() string { return "budget_hierarchy_audit" }

// budgetHierarchyAuditBudgetRepo is the minimal budget-repository dependency this worker needs.
type budgetHierarchyAuditBudgetRepo interface {
	GetByID(ctx context.Context, id string) (*domain.Budget, error)
	AuditBudgetHierarchy(ctx context.Context, budget *domain.Budget, periodStart, periodEnd time.Time, spentCalc repository.BudgetHierarchySpentCalculator) (domain.BudgetHierarchyAuditVerdict, error)
	BudgetIDsWithUnexaminedHierarchy(ctx context.Context, limit int) ([]string, error)
}

// budgetHierarchyAuditUnexaminedPerRun bounds one run's audit pass, mirroring
// portfolioRepairUsersPerRun (portfolio_snapshot_worker.go): the pass is resumable by
// construction (a budget it does not reach keeps its unexamined status and is picked up on the
// next run via BudgetIDsWithUnexaminedHierarchy's most-behind-first ordering), so the cap costs
// nothing but keeps a first run over a large backlog from stretching indefinitely.
const budgetHierarchyAuditUnexaminedPerRun = 200

// BudgetHierarchyAuditWorker runs the Task B7 audit over budgets the ledger has never examined.
type BudgetHierarchyAuditWorker struct {
	river.WorkerDefaults[BudgetHierarchyAuditArgs]

	budgetRepo budgetHierarchyAuditBudgetRepo
	spentCalc  services.BudgetSpentCalculator
	logger     *slog.Logger
	// cacheGens is optional (nil = cache invalidation disabled/not wired); see
	// SetCacheInvalidation. spent_amount is summed by the cached GetBudgetVsActual analytics query
	// (ScopeUser) — mirroring BudgetHierarchyResyncWorker, which bumps for the identical write
	// (repository/cache_completeness_repo_test.go's "BudgetRepository.UpdateCategorySpentAmount"
	// entry names both jobs as independent bumping callers).
	cacheGens *cache.Generations
}

// BudgetHierarchyAuditWorkerDeps holds dependencies for BudgetHierarchyAuditWorker.
type BudgetHierarchyAuditWorkerDeps struct {
	BudgetRepo budgetHierarchyAuditBudgetRepo
	SpentCalc  services.BudgetSpentCalculator
	Logger     *slog.Logger
}

// NewBudgetHierarchyAuditWorker constructs a worker from its dependencies.
func NewBudgetHierarchyAuditWorker(deps BudgetHierarchyAuditWorkerDeps) *BudgetHierarchyAuditWorker {
	logger := deps.Logger
	if logger == nil {
		logger = slog.Default()
	}
	return &BudgetHierarchyAuditWorker{
		budgetRepo: deps.BudgetRepo,
		spentCalc:  deps.SpentCalc,
		logger:     logger,
	}
}

// SetCacheInvalidation wires cache generation bumping (setter injection, matching
// BudgetHierarchyResyncWorker.SetCacheInvalidation beside it). Optional: nil is a safe no-op — but
// leaving it unwired means a spent_amount correction this audit makes can serve stale through
// GetBudgetVsActual until TTL, same as any other write to
// BudgetRepository.UpdateCategorySpentAmount-equivalent SQL left unbumped would.
func (w *BudgetHierarchyAuditWorker) SetCacheInvalidation(gens *cache.Generations) {
	w.cacheGens = gens
}

// bumpCacheForUser invalidates userID's cached analytics after a repair has committed. Runs on a
// detached, timeout-bounded context so a cancellation racing the job's own completion cannot
// swallow the bump silently, mirroring BudgetHierarchyResyncWorker.bumpCacheForUser.
func (w *BudgetHierarchyAuditWorker) bumpCacheForUser(ctx context.Context, userID string) {
	if w.cacheGens == nil {
		return
	}
	bumpCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), bumpCacheTimeout)
	defer cancel()
	w.cacheGens.Bump(bumpCtx, cache.ScopeUser, userID, "budget_hierarchy_audit")
}

// Work runs the audit over the budgets BudgetIDsWithUnexaminedHierarchy reports, most-behind-first,
// capped at budgetHierarchyAuditUnexaminedPerRun.
//
// Unlike PortfolioSnapshotWorker's repair pass (a supplementary step that never fails the job —
// its aggregate write is the job's real contract), auditing pre-existing budgets IS this job's
// entire contract, so a nonzero failure count DOES fail the job so River retries — the same
// behavior PortfolioSnapshotWorker.Work itself uses for its own primary per-user loop.
func (w *BudgetHierarchyAuditWorker) Work(ctx context.Context, job *river.Job[BudgetHierarchyAuditArgs]) (err error) {
	args := job.Args

	w.logger.Info("starting budget hierarchy audit job",
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
			observability.RecordJobSuccess(job.Kind)
		}
		observability.JobsCompleted.WithLabelValues(job.Kind, job.Queue, status).Inc()
		observability.JobDuration.WithLabelValues(job.Kind).Observe(time.Since(start).Seconds())
	}()

	budgetIDs, err := w.budgetRepo.BudgetIDsWithUnexaminedHierarchy(ctx, budgetHierarchyAuditUnexaminedPerRun)
	if err != nil {
		return fmt.Errorf("budget hierarchy audit: enumerate unexamined budgets: %w", err)
	}

	if len(budgetIDs) == 0 {
		w.logger.Info("budget hierarchy audit: no unexamined budgets, skipping", "job_id", job.ID)
		return nil
	}

	var summary domain.BudgetHierarchyAuditSummary
	var failCount int
	for _, budgetID := range budgetIDs {
		budget, err := w.budgetRepo.GetByID(ctx, budgetID)
		if err != nil {
			w.logger.Error("budget hierarchy audit: failed to load budget; skipping",
				"job_id", job.ID, "budget_id", budgetID, "error", err)
			failCount++
			continue
		}

		// Anchored on the budget's OWN stored period, never time.Now() — the same primitive Task
		// B1's reparent resync uses, and deliberately not RecalculateBudget's time.Now()-derived
		// non-custom periods (see budget_hierarchy_audit.go's header comment).
		periodStart, periodEnd := services.NarrowBudgetPeriodDates(budget)

		verdict, err := w.budgetRepo.AuditBudgetHierarchy(ctx, budget, periodStart, periodEnd, w.spentCalc)
		if err != nil {
			w.logger.Error("budget hierarchy audit: failed for budget; its ledger row stays unexamined",
				"job_id", job.ID, "budget_id", budgetID, "error", err)
			failCount++
			continue
		}
		summary.Add(verdict.Outcome, 1)

		if verdict.CategoriesRepaired > 0 {
			w.bumpCacheForUser(ctx, verdict.UserID)
		}

		// reported_* outcomes are the human-facing finding this audit exists to surface — logged at
		// WARN, individually, so an operator watching this job's logs sees each one rather than
		// only the aggregate count. The admin listing (RegisterAdminBudgetHierarchyAuditOperations)
		// is the durable, queryable path to the same finding; this line is the paging/log-alerting
		// path for anyone who does not poll that endpoint.
		switch verdict.Outcome {
		case domain.BudgetHierarchyAuditOutcomeReportedDuplicateIdentity, domain.BudgetHierarchyAuditOutcomeReportedEscapingPair:
			w.logger.Warn("budget hierarchy audit: flagged a budget for human review",
				"job_id", job.ID,
				"budget_id", budgetID,
				"user_id", verdict.UserID,
				"outcome", verdict.Outcome,
				"flagged_duplicate_identity_category_ids", verdict.FlaggedDuplicateIdentityCategoryIDs,
				"flagged_escaping_pair_category_ids", verdict.FlaggedEscapingPairCategoryIDs,
				"reason", verdict.Reason,
			)
		case domain.BudgetHierarchyAuditOutcomeSkippedIncompleteActuals:
			// Not a human-facing finding, but an operational one: the audit declined to examine this
			// budget, and the ledger row it just wrote makes that permanent — the periodic sweep
			// never re-selects a budget already in the ledger, and the admin surface is GET-only.
			w.logger.Warn("budget hierarchy audit: skipped a budget whose actuals are incomplete; it is now permanently exempt from re-examination",
				"job_id", job.ID,
				"budget_id", budgetID,
				"user_id", verdict.UserID,
				"outcome", verdict.Outcome,
				"reason", verdict.Reason,
			)
		}
	}

	// cap_reached is the backlog signal — see portfolioRepairUsersPerRun's own comment for why this
	// line exists: without it, draining the first 200 of a much larger backlog reads identically to
	// having drained the whole thing.
	w.logger.Info("budget hierarchy audit pass complete",
		"job_id", job.ID,
		"budgets_examined", len(budgetIDs),
		"cap_reached", len(budgetIDs) >= budgetHierarchyAuditUnexaminedPerRun,
		"failed_budgets", failCount,
		"repaired", summary.Repaired,
		"verified", summary.Verified,
		"reported_duplicate_identity", summary.ReportedDuplicateIdentity,
		"reported_escaping_pair", summary.ReportedEscapingPair,
		"skipped_incomplete_actuals", summary.SkippedIncompleteActuals,
		"duration_ms", time.Since(start).Milliseconds(),
	)

	if failCount > 0 {
		return fmt.Errorf("budget hierarchy audit: %d/%d budget audits failed", failCount, len(budgetIDs))
	}
	return nil
}

// BudgetHierarchyAuditUniqueOpts returns the recommended uniqueness options — one audit job per
// 24h period, mirroring PortfolioSnapshotUniqueOpts.
func BudgetHierarchyAuditUniqueOpts() *queue.UniqueOpts {
	return &queue.UniqueOpts{
		ByPeriod: 24 * time.Hour,
	}
}

// BudgetHierarchyAuditInsertOpts returns the *river.InsertOpts this job's periodic registration in
// main.go must use.
func BudgetHierarchyAuditInsertOpts() *river.InsertOpts {
	return riverInsertOptsFromUnique(queue.QueueLow, BudgetHierarchyAuditUniqueOpts())
}

// BudgetHierarchyAuditPeriodicOpts returns the *river.PeriodicJobOpts this job's periodic
// registration in main.go must use. RunOnStart: true is safe because the audit is fully
// idempotent (BudgetIDsWithUnexaminedHierarchy excludes ledgered budgets; AuditBudgetHierarchy's
// own writes are guarded by IS DISTINCT FROM and ON CONFLICT DO NOTHING).
func BudgetHierarchyAuditPeriodicOpts() *river.PeriodicJobOpts {
	return &river.PeriodicJobOpts{RunOnStart: true, ID: BudgetHierarchyAuditArgs{}.Kind()}
}
