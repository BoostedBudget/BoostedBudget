package jobs

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/maxnussbaum/BoostedBudget/internal/cache"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/observability"
	"github.com/maxnussbaum/BoostedBudget/internal/services"
	"github.com/riverqueue/river"
)

// BudgetHierarchyResyncArgs is the job payload for recomputing budget category spend after a
// custom category is reparented (Issue #1630 Task B1).
//
// custom_category_service.go's UpdateCategory can change ParentID/ParentPFCPrimary with ZERO
// query against budget_categories: a budgeted row's excluded/top-level status (see
// domain.BudgetCategory.IsSubAllocationWithin / Budget.TotalSpent, which sums only top-level rows)
// flips the instant the write commits, but the affected budget's stored spent_amount does not —
// spend visibly vanishes or doubles until an unrelated sync happens to touch that same budget.
// This job is that resync, run once per reparent.
//
// Deliberately DISTINCT from effective_category_refresh (EffectiveCategoryRefreshArgs): that
// worker only rewrites transactions.effective_category_name and bumps caches — it never touches
// budget_categories.spent_amount (effective_category_refresh.go:157) — so reusing its shape would
// leave the stale-total half of this gap unfixed while looking fixed. A reparent that also
// renames enqueues BOTH jobs (custom_category_service.go's UpdateCategory).
type BudgetHierarchyResyncArgs struct {
	UserID           string `json:"user_id"`
	CustomCategoryID string `json:"custom_category_id"`
	// OldParentCustomCategoryID/OldParentPFCPrimary carry the PRE-reparent parent identity,
	// captured by the enqueuing service before the write. By the time this job runs,
	// custom_categories already reflects the NEW parent — the OLD one is otherwise
	// unrecoverable, and its own budgeted row (if any) needs its hierarchical rollup total
	// recomputed just as much as the new parent's does (the category moved OUT of its clause).
	OldParentCustomCategoryID *string `json:"old_parent_custom_category_id,omitempty"`
	OldParentPFCPrimary       *string `json:"old_parent_pfc_primary,omitempty"`
}

// Kind returns the unique identifier for this job type.
func (BudgetHierarchyResyncArgs) Kind() string { return "budget_hierarchy_resync" }

// budgetHierarchyResyncCategoryRepo is the minimal custom-category repository this worker needs:
// the reparented category's CURRENT (post-write) parent linkage, and its direct children — the
// whole "subtree" a custom-custom nest can have. custom_category.go's Validate/validateParent caps
// custom-custom nesting at two levels (a parent may not itself have a parent_id), so there are
// never grandchildren to walk on that axis.
//
// ⚠ UPDATE (029e15d3): the paragraph this replaced described the PFC-parent 3-level shape (pfc
// root X ← custom C via parent_pfc_primary ← custom G via parent_id) as an out-of-scope
// limitation "shared" with domain.BudgetCategory.IsSubAllocationWithin. That shared premise is
// gone: IsSubAllocationWithin/EscapedNestedChild now recognize the transitive pair via
// CustomGrandparentPFCPrimary, so it is CLOSED there. It is NOT closed here. This worker's own
// Work() below builds affectedCustomIDs/affectedPFCPrimaries from the reparented category's
// DIRECT parent identity only (cc.ParentID / cc.ParentPFCPrimary, plus the OLD parent from job
// args) — if the reparented category's own parent is a CUSTOM category nested under a PFC root
// two hops away (rather than the reparented category being a direct PFC-primary child), that
// root's PFC primary is never added to affectedPFCPrimaries, so a budget row on that root is
// never flagged as "affected" and its spent_amount is never resynced, even though its rollup
// total did change. Known, currently open gap — not fixed by this comment update.
type budgetHierarchyResyncCategoryRepo interface {
	GetByID(ctx context.Context, id string) (*domain.CustomCategory, error)
	GetChildren(ctx context.Context, parentID string) ([]*domain.CustomCategory, error)
}

// BudgetHierarchyResyncWorker recomputes spent_amount for every LIVE budget category in every
// active budget affected by a custom-category reparent. It deliberately does NOT call
// services.BudgetSyncService.RecalculateBudget — see services.NarrowBudgetPeriodDates's doc
// comment for the hazards that call would reintroduce (income recalculation, time.Now()-derived
// periods for non-custom budgets, budget_updated automation that can trigger boosts, and
// swallowed per-category failures). This worker touches only spent_amount, derives its period
// from the budget's own stored anchor fields, fires no events, and propagates the first error it
// hits rather than swallowing it — the whole job retries, which is safe because recomputing spend
// is idempotent.
type BudgetHierarchyResyncWorker struct {
	river.WorkerDefaults[BudgetHierarchyResyncArgs]

	customCategoryRepo budgetHierarchyResyncCategoryRepo
	budgetRepo         services.BudgetSyncRepository
	spentCalc          services.BudgetSpentCalculator
	logger             *slog.Logger
	// cacheGens is optional (nil = cache invalidation disabled/not wired); see
	// SetCacheInvalidation. spent_amount is summed by the cached GetBudgetVsActual analytics query
	// (ScopeUser) — repository/cache_completeness_repo_test.go's
	// "BudgetRepository.UpdateCategorySpentAmount" entry, which is why this worker bumps rather
	// than relying on the HTTP-middleware coverage that entry's OTHER caller enjoys.
	cacheGens *cache.Generations
}

// BudgetHierarchyResyncWorkerDeps holds dependencies for BudgetHierarchyResyncWorker.
type BudgetHierarchyResyncWorkerDeps struct {
	CustomCategoryRepo budgetHierarchyResyncCategoryRepo
	BudgetRepo         services.BudgetSyncRepository
	SpentCalc          services.BudgetSpentCalculator
	Logger             *slog.Logger
}

// NewBudgetHierarchyResyncWorker constructs a worker from its dependencies.
func NewBudgetHierarchyResyncWorker(deps BudgetHierarchyResyncWorkerDeps) *BudgetHierarchyResyncWorker {
	logger := deps.Logger
	if logger == nil {
		logger = slog.Default()
	}
	return &BudgetHierarchyResyncWorker{
		customCategoryRepo: deps.CustomCategoryRepo,
		budgetRepo:         deps.BudgetRepo,
		spentCalc:          deps.SpentCalc,
		logger:             logger,
	}
}

// SetCacheInvalidation wires cache generation bumping (setter injection, matching
// EffectiveCategoryRefreshWorker's SetCacheInvalidation). Optional: nil is a safe no-op — but
// leaving it unwired means the cached spending-by-category/budget-vs-actual analytics this job's
// own write feeds can serve a stale total until TTL, same as any other write to
// BudgetRepository.UpdateCategorySpentAmount left unbumped would.
func (w *BudgetHierarchyResyncWorker) SetCacheInvalidation(gens *cache.Generations) {
	w.cacheGens = gens
}

// bumpCacheForUser invalidates userID's cached analytics after this job's resync has committed.
// Runs on a detached, timeout-bounded context so a cancellation racing the job's own completion
// cannot swallow the bump silently, mirroring EffectiveCategoryRefreshWorker.bumpCacheForUser.
func (w *BudgetHierarchyResyncWorker) bumpCacheForUser(ctx context.Context, userID string) {
	if w.cacheGens == nil {
		return
	}
	bumpCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), bumpCacheTimeout)
	defer cancel()
	w.cacheGens.Bump(bumpCtx, cache.ScopeUser, userID, "budget_hierarchy_resync")
}

// Work executes the budget hierarchy resync job.
func (w *BudgetHierarchyResyncWorker) Work(ctx context.Context, job *river.Job[BudgetHierarchyResyncArgs]) (err error) {
	args := job.Args

	w.logger.Info("starting budget hierarchy resync job",
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

	cc, err := w.customCategoryRepo.GetByID(ctx, args.CustomCategoryID)
	if err != nil {
		return fmt.Errorf("load reparented category %s: %w", args.CustomCategoryID, err)
	}
	children, err := w.customCategoryRepo.GetChildren(ctx, args.CustomCategoryID)
	if err != nil {
		return fmt.Errorf("load children of %s: %w", args.CustomCategoryID, err)
	}

	// The affected subtree: the reparented category, its direct children, its OLD parent
	// identity (captured in job args) and its NEW parent identity (read fresh, since the write
	// already committed by the time this job runs).
	affectedCustomIDs := map[string]bool{args.CustomCategoryID: true}
	for _, c := range children {
		affectedCustomIDs[c.ID] = true
	}
	if args.OldParentCustomCategoryID != nil {
		affectedCustomIDs[*args.OldParentCustomCategoryID] = true
	}
	if cc.ParentID != nil {
		affectedCustomIDs[*cc.ParentID] = true
	}

	affectedPFCPrimaries := map[string]bool{}
	if args.OldParentPFCPrimary != nil {
		affectedPFCPrimaries[*args.OldParentPFCPrimary] = true
	}
	if cc.ParentPFCPrimary != nil {
		affectedPFCPrimaries[*cc.ParentPFCPrimary] = true
	}

	budgets, err := w.budgetRepo.GetActiveByUserID(ctx, args.UserID)
	if err != nil {
		return fmt.Errorf("load active budgets for user %s: %w", args.UserID, err)
	}

	var budgetsResynced, categoriesResynced int
	for _, budget := range budgets {
		categories, err := w.budgetRepo.GetCategoriesByBudgetID(ctx, budget.ID)
		if err != nil {
			return fmt.Errorf("load categories for budget %s: %w", budget.ID, err)
		}

		affected := false
		for i := range categories {
			cat := &categories[i]
			if cat.CategoryKind == "custom" && cat.CustomCategoryID != nil && affectedCustomIDs[*cat.CustomCategoryID] {
				affected = true
				break
			}
			if cat.CategoryKind == "pfc" && cat.PFCPrimary != nil && affectedPFCPrimaries[*cat.PFCPrimary] {
				affected = true
				break
			}
		}
		if !affected {
			continue
		}

		// Whole-budget recompute, not just the affected rows: the reparent shifts what a
		// PARENT's hierarchical category clause rolls up, so a sibling row's total can shift too
		// even when its own identity fields never appear in affectedCustomIDs/affectedPFCPrimaries.
		periodStart, periodEnd := services.NarrowBudgetPeriodDates(budget)
		for i := range categories {
			cat := &categories[i]
			if cat.CategoryKind == "savings" {
				// Savings spend comes from the confirmed-contribution ledger (Issue #1300), not
				// transaction matching — untouched by a custom-category reparent.
				continue
			}

			spent, err := w.spentCalc.CalculateCategorySpent(ctx, args.UserID, cat, periodStart, periodEnd)
			if err != nil {
				return fmt.Errorf("recompute spend for category %s in budget %s: %w", cat.ID, budget.ID, err)
			}
			if err := w.budgetRepo.UpdateCategorySpentAmount(ctx, cat.ID, spent); err != nil {
				return fmt.Errorf("update spent amount for category %s in budget %s: %w", cat.ID, budget.ID, err)
			}
			categoriesResynced++
		}
		budgetsResynced++
	}

	w.logger.Info("budget hierarchy resync completed",
		"job_id", job.ID,
		"user_id", args.UserID,
		"custom_category_id", args.CustomCategoryID,
		"budgets_resynced", budgetsResynced,
		"categories_resynced", categoriesResynced,
		"duration_ms", time.Since(start).Milliseconds(),
	)

	if budgetsResynced > 0 {
		w.bumpCacheForUser(ctx, args.UserID)
	}

	return nil
}
