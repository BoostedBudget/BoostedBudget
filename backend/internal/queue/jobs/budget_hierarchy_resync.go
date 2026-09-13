package jobs

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/maxnussbaum/BoostedBudget/internal/cache"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/observability"
	"github.com/maxnussbaum/BoostedBudget/internal/repository"
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

func budgetHierarchyNeedsResync(categories []domain.BudgetCategory, customIDs, pfcPrimaries map[string]bool) bool {
	for _, cat := range categories {
		// Groups and Other depend on the complete hierarchy and all sibling matches;
		// conservatively recompute them for any reparent rather than miss a complement.
		if cat.CategoryKind == "group" || cat.CategoryKind == "remainder" {
			return true
		}
		if cat.CategoryKind == "custom" && cat.CustomCategoryID != nil && customIDs[*cat.CustomCategoryID] {
			return true
		}
		if cat.CategoryKind == "pfc" && cat.PFCPrimary != nil && pfcPrimaries[*cat.PFCPrimary] {
			return true
		}
	}
	return false
}

// budgetHierarchyResyncCategoryRepo is the minimal custom-category repository this worker needs:
// the reparented category's CURRENT (post-write) parent linkage, and its direct children — the
// whole "subtree" a custom-custom nest can have. custom_category.go's Validate/validateParent caps
// custom-custom nesting at two levels (a parent may not itself have a parent_id), so there are
// never grandchildren to walk on that axis.
//
// ⚠ UPDATE (#1630 review round 2): the paragraph this replaced described the PFC-parent 3-level
// shape (pfc root X ← custom C via parent_pfc_primary ← custom G via parent_id) as a known,
// currently-open gap in THIS worker — Work() only resolved the reparented category's own direct
// parent identity (cc.ParentID / cc.ParentPFCPrimary, plus the OLD parent from job args), never
// walking the grandparent hop to find X's PFC primary when the parent added to affectedCustomIDs
// is itself a custom category nested under a PFC root. That gap is now CLOSED: Work() resolves
// each affected custom parent's own ParentPFCPrimary via a fresh GetByID and folds it into
// affectedPFCPrimaries, so a budget row on X is correctly resynced even though neither C nor G is
// itself a budgeted row. See resolveParentPFCPrimary in Work() below.
type budgetHierarchyResyncCategoryRepo interface {
	GetByID(ctx context.Context, id string) (*domain.CustomCategory, error)
	GetChildren(ctx context.Context, parentID string) ([]*domain.CustomCategory, error)
}

// BudgetHierarchyResyncWorker recomputes spent_amount for every LIVE budget category in every
// active budget affected by a custom-category reparent. It uses the shared current UTC
// period, clipped to stored dates, while preserving income read under the budget lock.
// It deliberately does not emit budget_updated automation events. A failed sibling
// calculation aborts the batch and propagates the error so River can retry safely.
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

	// Grandparent hop: the OLD and NEW custom parents just added to affectedCustomIDs may
	// themselves be nested under a PFC root (pfc root X <- custom C via ParentPFCPrimary <-
	// custom G via ParentID). A reparent of G changes what X's roll-up includes, but neither C
	// nor G is necessarily a budgeted row itself — X's PFC primary must be resolved from C (or
	// whichever custom category is the actual parent) and added here, or a budget row on X is
	// never flagged as affected and its spent_amount is never resynced.
	resolveParentPFCPrimary := func(customParentID string) error {
		parent, err := w.customCategoryRepo.GetByID(ctx, customParentID)
		if err != nil {
			return fmt.Errorf("load custom parent %s: %w", customParentID, err)
		}
		if parent.ParentPFCPrimary != nil {
			affectedPFCPrimaries[*parent.ParentPFCPrimary] = true
		}
		return nil
	}
	if args.OldParentCustomCategoryID != nil {
		if err := resolveParentPFCPrimary(*args.OldParentCustomCategoryID); err != nil {
			return err
		}
	}
	if cc.ParentID != nil {
		if err := resolveParentPFCPrimary(*cc.ParentID); err != nil {
			return err
		}
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

		if !budgetHierarchyNeedsResync(categories, affectedCustomIDs, affectedPFCPrimaries) {
			continue
		}

		// Whole-budget recompute, not just the affected rows: the reparent shifts what a
		// PARENT's hierarchical category clause rolls up, so a sibling row's total can shift too
		// even when its own identity fields never appear in affectedCustomIDs/affectedPFCPrimaries.
		if atomic, ok := w.budgetRepo.(repository.BudgetActualsRepository); ok {
			updated, err := atomic.RecalculateBudgetActuals(ctx, args.UserID, budget.ID, false)
			if err != nil {
				return fmt.Errorf("atomic hierarchy resync for budget %s: %w", budget.ID, err)
			}
			if updated == nil {
				continue
			}
			budgetsResynced++
			categoriesResynced += len(updated.Categories)
			continue
		}
		period := domain.CurrentBudgetActualsRange(budget, time.Now())
		if !period.Eligible {
			continue
		}
		periodStart, periodEnd := period.Start, period.ActualThrough
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
