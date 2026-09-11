package services

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/queue"
	"github.com/maxnussbaum/BoostedBudget/internal/repository"
)

// effectiveCategoryRefreshJobArgs mirrors jobs.EffectiveCategoryRefreshArgs to avoid import cycle.
type effectiveCategoryRefreshJobArgs struct {
	UserID           string `json:"user_id"`
	CustomCategoryID string `json:"custom_category_id"`
}

func (effectiveCategoryRefreshJobArgs) Kind() string { return "effective_category_refresh" }

// budgetHierarchyResyncJobArgs mirrors jobs.BudgetHierarchyResyncArgs to avoid an import cycle
// (jobs imports services). Enqueued when a custom category's ParentID/ParentPFCPrimary changes
// (Issue #1630 Task B1) — see UpdateCategory below. Distinct from effectiveCategoryRefreshJobArgs
// deliberately: that worker only rewrites transactions.effective_category_name and never touches
// budget_categories.spent_amount (effective_category_refresh.go:157), so reusing it would leave
// the stale-total half of this gap unfixed while looking fixed.
type budgetHierarchyResyncJobArgs struct {
	UserID           string `json:"user_id"`
	CustomCategoryID string `json:"custom_category_id"`
	// OldParentCustomCategoryID/OldParentPFCPrimary carry the PRE-reparent parent identity,
	// captured before the write below — by the time the worker runs, custom_categories already
	// reflects the NEW parent, so the OLD one (whose own budgeted row, if any, needs its
	// hierarchical rollup recomputed just as much as the new one's does) would otherwise be lost.
	OldParentCustomCategoryID *string `json:"old_parent_custom_category_id,omitempty"`
	OldParentPFCPrimary       *string `json:"old_parent_pfc_primary,omitempty"`
}

func (budgetHierarchyResyncJobArgs) Kind() string { return "budget_hierarchy_resync" }

type CustomCategoryService interface {
	CreateCategory(ctx context.Context, userID string, req *domain.CreateCustomCategoryRequest) (*domain.CustomCategory, error)

	GetUserCategories(ctx context.Context, userID string) ([]*domain.CustomCategory, error)

	GetCategoryByID(ctx context.Context, categoryID string) (*domain.CustomCategory, error)

	UpdateCategory(ctx context.Context, userID, categoryID string, req *domain.UpdateCustomCategoryRequest) (*domain.CustomCategory, error)

	DeleteCategory(ctx context.Context, userID, categoryID string) error

	// GetCategoryUsage returns live reference counts for a category (Issue #1203),
	// used to warn the user before deletion.
	GetCategoryUsage(ctx context.Context, userID, categoryID string) (*domain.CategoryUsage, error)

	GetOrCreateCategory(ctx context.Context, userID, categoryName string) (*domain.CustomCategory, error)

	// SetJobQueue injects the job queue for async effective_category_name refreshes.
	// Called after the River client is created (see main.go). Safe to skip in tests
	// that don't exercise the rename path.
	SetJobQueue(jq queue.JobQueue)

	// SetAuditLogRepository wires the custom_category.deleted audit entry, which
	// CustomCategoryRepository.DeleteWithReassignment writes INSIDE its own transaction so the
	// entry and the tombstone commit or roll back together (Issue #1569 Task 9, following #1507).
	// Optional: unset, the delete still works and writes no entry.
	SetAuditLogRepository(ar repository.AuditLogRepository)

	// SetBudgetCategoryRepository wires the budget-side lookup UpdateCategory needs to refuse a
	// reparent that would create an escaping (not-cleanly-contained) nested budget pair, and to
	// find the budgets a resync job must touch (Issue #1630 Task B1). Mirrors SetJobQueue /
	// SetAuditLogRepository's setter-injection pattern above, so CustomCategoryService's own
	// interface stays unaware of the budget domain.
	//
	// Injection is optional; PROCEEDING WITHOUT IT IS NOT. This is money-integrity enforcement,
	// not observability (contrast SetAuditLogRepository, which fails open by design) — a reparent
	// is refused, not silently allowed, when this is unset. See UpdateCategory's
	// checkReparentDoesNotEscape, which mirrors budget_service.go:651's goalOwnershipChecker
	// fail-closed precedent for a required (if optionally-wired) collaborator.
	SetBudgetCategoryRepository(r BudgetCategoryRepository)
}

// BudgetCategoryRepository is the minimal budget-side interface CustomCategoryService needs to
// enforce the containment guard on a reparent and to scope the resync job it enqueues (Issue
// #1630 Task B1). Kept narrow, mirroring BudgetSyncRepository in budget_sync_service.go, so this
// package's own CustomCategoryService interface never has to import the wider budget domain.
type BudgetCategoryRepository interface {
	// GetByUserID returns every non-deleted budget for the user, active AND inactive (Issue #1630
	// review fix). An inactive budget still holds its own budget_categories rows and can be
	// reactivated later by a plain is_active update with no hierarchy validation of its own, so
	// the reparent guard must see it too — GetActiveByUserID's active-only universe left a
	// reparent-then-reactivate path that could commit an escaping/double-counted pair with no
	// guard ever inspecting it.
	GetByUserID(ctx context.Context, userID string) ([]*domain.Budget, error)
	// GetCategoriesByBudgetID returns a budget's LIVE categories, with CustomParentID /
	// CustomParentPFCPrimary populated from the CURRENT (pre-write) custom_categories row —
	// see checkReparentDoesNotEscape for why that must be overlaid with the PROPOSED parent
	// before domain.EscapedNestedChild runs.
	GetCategoriesByBudgetID(ctx context.Context, budgetID string) ([]domain.BudgetCategory, error)
	// BeginTx starts a transaction on the budget repository's pool (Issue #1630 B3b), mirroring
	// SavingsGoalRepository.BeginTx / savingsGoalService.applyBasisSwitch's cross-repository
	// transaction composition. reparentCategoryChecked owns this transaction: it locks every
	// affected budget via GetCategoriesByBudgetIDLockedInTx AND persists the reparent's
	// custom_categories write (a DIFFERENT repository) inside it, so the guard and the write
	// commit or roll back together — closing the window where GetCategoriesByBudgetIDLocked's
	// self-contained transaction released its lock before that write committed separately.
	BeginTx(ctx context.Context) (repository.Transaction, error)
	// GetCategoriesByBudgetIDLockedInTx locks budgetID's row (SELECT ... FOR UPDATE) and re-reads
	// its live categories INSIDE the caller's tx, without committing or rolling back — the lock is
	// held until the caller does (Issue #1630 B3b). reparentCategoryChecked uses this, inside its
	// own BeginTx transaction, once per affected budget, so a reparent and a concurrent
	// AddCategory/UpdateCategory on the same budget can no longer both observe a stale snapshot of
	// each other's write and jointly violate domain.EscapedNestedChild — AND so two concurrent
	// reparents that would form an escaping pair with each other can no longer both pass the guard
	// and both commit.
	GetCategoriesByBudgetIDLockedInTx(ctx context.Context, tx repository.Transaction, budgetID string) ([]domain.BudgetCategory, error)
}

type customCategoryService struct {
	categoryRepo repository.CustomCategoryRepository
	jobQueue     queue.JobQueue // nil until SetJobQueue is called
	// auditLogRepo records the custom_category.deleted entry inside DeleteWithReassignment's own
	// transaction. nil until SetAuditLogRepository; when unset the delete writes no entry.
	auditLogRepo repository.AuditLogRepository
	// budgetCategoryRepo backs the reparent containment guard and resync scoping (Issue #1630
	// Task B1). nil until SetBudgetCategoryRepository — and, unlike auditLogRepo, that is a
	// refusal, not a silent no-op: see checkReparentDoesNotEscape.
	budgetCategoryRepo BudgetCategoryRepository
	logger             *slog.Logger
}

func NewCustomCategoryService(
	categoryRepo repository.CustomCategoryRepository,
) CustomCategoryService {
	return &customCategoryService{
		categoryRepo: categoryRepo,
		logger:       slog.Default(),
	}
}

// SetJobQueue injects the job queue after construction (Issue #1123).
func (s *customCategoryService) SetJobQueue(jq queue.JobQueue) {
	s.jobQueue = jq
}

// SetAuditLogRepository injects the audit-log repository. See the interface method.
func (s *customCategoryService) SetAuditLogRepository(ar repository.AuditLogRepository) {
	s.auditLogRepo = ar
}

// SetBudgetCategoryRepository injects the budget-side repository. See the interface method.
func (s *customCategoryService) SetBudgetCategoryRepository(r BudgetCategoryRepository) {
	s.budgetCategoryRepo = r
}

func (s *customCategoryService) CreateCategory(ctx context.Context, userID string, req *domain.CreateCustomCategoryRequest) (*domain.CustomCategory, error) {
	category := &domain.CustomCategory{
		UserID:           userID,
		Name:             req.Name,
		ParentPFCPrimary: emptyStringToNil(req.ParentPFCPrimary),
		ParentID:         emptyStringToNil(req.ParentID),
		Icon:             req.Icon,
		Color:            req.Color,
		UsageCount:       0,
	}

	if err := category.Validate(); err != nil {
		return nil, err
	}

	parent, err := s.validateParent(ctx, userID, "", category.ParentID)
	if err != nil {
		return nil, err
	}

	if err := s.categoryRepo.Create(ctx, category); err != nil {
		return nil, err
	}

	category.PopulatePFCDisplay()
	if parent != nil {
		category.ParentName = &parent.Name
	}
	return category, nil
}

// emptyStringToNil converts an empty-string pointer to nil so callers can clear a
// parent (parent_id or parent_pfc_primary) by sending "". This is essential for the
// mutual-exclusivity CHECK: an empty string is NOT NULL and would violate it (#1203).
func emptyStringToNil(id *string) *string {
	if id == nil || *id == "" {
		return nil
	}
	return id
}

// validateParent verifies a proposed custom parent exists, is owned by the same
// user, and is itself top-level (2-level limit). selfID is the id of the category
// being mutated (empty on create) and is rejected as its own parent. Returns the
// fetched parent (nil when parentID is nil) so callers can reuse its name.
func (s *customCategoryService) validateParent(ctx context.Context, userID, selfID string, parentID *string) (*domain.CustomCategory, error) {
	if parentID == nil {
		return nil, nil
	}
	if selfID != "" && *parentID == selfID {
		return nil, domain.ErrCategoryInvalidParent
	}
	parent, err := s.categoryRepo.GetByID(ctx, *parentID)
	if err != nil {
		if err == domain.ErrCategoryNotFound {
			return nil, domain.ErrCategoryInvalidParent
		}
		return nil, err
	}
	if parent.UserID != userID {
		// Don't leak existence of another user's category.
		return nil, domain.ErrCategoryInvalidParent
	}
	if parent.ParentID != nil {
		return nil, domain.ErrCategoryParentHasParent
	}
	return parent, nil
}

func (s *customCategoryService) GetUserCategories(ctx context.Context, userID string) ([]*domain.CustomCategory, error) {
	categories, err := s.categoryRepo.GetByUserID(ctx, userID)
	if err != nil {
		return nil, err
	}
	for _, c := range categories {
		c.PopulatePFCDisplay()
	}
	return categories, nil
}

func (s *customCategoryService) GetCategoryByID(ctx context.Context, categoryID string) (*domain.CustomCategory, error) {
	c, err := s.categoryRepo.GetByID(ctx, categoryID)
	if err != nil {
		return nil, err
	}
	c.PopulatePFCDisplay()
	return c, nil
}

func (s *customCategoryService) UpdateCategory(ctx context.Context, userID, categoryID string, req *domain.UpdateCustomCategoryRequest) (*domain.CustomCategory, error) {
	category, err := s.categoryRepo.GetByID(ctx, categoryID)
	if err != nil {
		return nil, err
	}

	if category.UserID != userID {
		return nil, domain.ErrCategoryNotFound
	}

	// Issue #1630 Task B1: capture the PRE-reparent parent identity before any mutation. Once
	// this method persists below, custom_categories only remembers the NEW parent — the resync
	// job enqueued at the end needs the OLD one too, to find the old parent's own budgeted row
	// (if any), whose hierarchical rollup total needs recomputing just as much as the new
	// parent's does.
	originalParentID := category.ParentID
	originalParentPFCPrimary := category.ParentPFCPrimary

	nameChanged := req.Name != nil && *req.Name != category.Name
	if req.Name != nil {
		category.Name = *req.Name
	}
	if req.ParentPFCPrimary != nil {
		category.ParentPFCPrimary = emptyStringToNil(req.ParentPFCPrimary)
	}
	if req.ParentID != nil {
		category.ParentID = emptyStringToNil(req.ParentID)
		// Setting a PFC parent and a custom parent is mutually exclusive; if a
		// custom parent is being set, clear any stale PFC parent.
		if category.ParentID != nil {
			category.ParentPFCPrimary = nil
		}
	}
	if req.Icon != nil {
		category.Icon = req.Icon
	}
	if req.Color != nil {
		category.Color = req.Color
	}

	if err := category.Validate(); err != nil {
		return nil, err
	}

	// When nesting this category under a custom parent, validate the parent and
	// ensure this category is not itself a parent (2-level limit).
	var parent *domain.CustomCategory
	if category.ParentID != nil {
		hasChildren, err := s.categoryRepo.HasChildren(ctx, categoryID)
		if err != nil {
			return nil, err
		}
		if hasChildren {
			return nil, domain.ErrCategoryHasChildren
		}
		parent, err = s.validateParent(ctx, userID, categoryID, category.ParentID)
		if err != nil {
			return nil, err
		}
	}

	// Issue #1630 Task B1: this is the one write path that changes a custom category's parent
	// linkage with ZERO query against budget_categories. Refuse the escaping shape BEFORE
	// persisting anything — resync of the (legitimate) shapes happens AFTER, once the write has
	// committed, via the enqueued job below.
	parentChanged := !stringPtrEqual(category.ParentID, originalParentID) ||
		!stringPtrEqual(category.ParentPFCPrimary, originalParentPFCPrimary)

	if parentChanged {
		// reparentCategoryChecked runs the containment guard AND persists this write in ONE
		// transaction (Issue #1630 B3b) — see its doc comment for why a separate guard-then-write
		// left a race open.
		if err := s.reparentCategoryChecked(ctx, userID, category); err != nil {
			return nil, err
		}
	} else if err := s.categoryRepo.Update(ctx, category); err != nil {
		return nil, err
	}

	// Issue #1123: enqueue async refresh of effective_category_name when the name changed. This
	// stays best-effort (log, don't fail): a stale effective_category_name is a cosmetic label
	// lag, not a money-correctness gap, and the write it would roll back (the rename itself) has
	// already committed.
	if nameChanged && s.jobQueue != nil {
		if _, enqErr := s.jobQueue.Enqueue(ctx, effectiveCategoryRefreshJobArgs{
			UserID:           userID,
			CustomCategoryID: categoryID,
		}); enqErr != nil {
			// Log but don't fail — rename is already persisted
			s.logger.Error("failed to enqueue effective category refresh job",
				"user_id", userID,
				"custom_category_id", categoryID,
				"error", enqErr,
			)
		}
	}

	// Issue #1630 Task B1 (review fix, fail-closed): enqueue async budget resync when the parent
	// linkage changed — reparenting flips a budgeted row's excluded/top-level status with no
	// query against budget_categories at all, so without this resync the affected budget's stored
	// spent_amount goes stale (spend visibly vanishes or doubles) until an unrelated sync happens
	// to touch it, which may never happen.
	//
	// This used to be best-effort like the rename job above (log, don't fail) — a nil jobQueue
	// skipped the enqueue entirely, and a failed enqueue was logged and swallowed. Both silently
	// left the resync's own stale-totals gap open on the one path the resync exists to close, and
	// the caller's 200 response gave no signal that anything was wrong. Money-integrity
	// enforcement is not observability (mirrors reparentCategoryChecked's own fail-closed
	// precedent, below, for its required-but-optionally-wired budgetCategoryRepo): the reparent
	// itself has already committed by this point (via reparentCategoryChecked's own transaction),
	// so this cannot roll that back, but it CAN and now DOES refuse to report success while the
	// resync that would fix the resulting stale totals never got scheduled — surfacing a 5xx tells
	// the caller (and any retry/alerting built on it) that the budget-side totals need attention,
	// instead of looking like a clean success.
	//
	// A transactional outbox (enqueue the resync row in the SAME transaction as the reparent
	// write, then a separate dispatcher moves it to the queue) would close the remaining gap this
	// doesn't: a crash between reparentCategoryChecked's commit and this Enqueue call still drops
	// the resync silently. That is a bigger structural change than this fix warrants on its own —
	// no other write path in this codebase uses an outbox, and introducing the pattern for one job
	// type is out of scope for a fail-closed review fix. Fail-closed here is deliberately the
	// smaller, immediate fix: it cannot eliminate a mid-request crash window, but it does turn
	// "queue full" / "queue down" / "mis-wired jobQueue" — the ordinary-operation cases the review
	// flagged — from a silent gap into a visible failure.
	if parentChanged {
		if s.jobQueue == nil {
			return nil, fmt.Errorf("job queue not configured: cannot enqueue budget hierarchy resync for reparented category %s — refusing to report success while the stale-totals gap this resync exists to close goes unscheduled", categoryID)
		}
		if _, enqErr := s.jobQueue.Enqueue(ctx, budgetHierarchyResyncJobArgs{
			UserID:                    userID,
			CustomCategoryID:          categoryID,
			OldParentCustomCategoryID: originalParentID,
			OldParentPFCPrimary:       originalParentPFCPrimary,
		}); enqErr != nil {
			return nil, fmt.Errorf("failed to enqueue budget hierarchy resync for reparented category %s: %w", categoryID, enqErr)
		}
	}

	category.PopulatePFCDisplay()
	if parent != nil {
		category.ParentName = &parent.Name
	}
	return category, nil
}

// stringPtrEqual reports whether two *string values name the same optional string — both nil, or
// both non-nil with equal contents.
func stringPtrEqual(a, b *string) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

// reparentCategoryChecked refuses a custom-category reparent that would place a BUDGETED row
// into an escaping (not-cleanly-contained) nested pair — the exact shape domain.EscapedNestedChild
// already refuses on every OTHER write path (CreateBudget, AddCategory, and UpdateCategory in
// budget_service.go) — and, only if every affected budget clears that guard, PERSISTS the
// reparent's custom_categories write. UpdateCategory here is the one remaining door: it can flip a
// category's parent with zero query against budget_categories, so a budget's own escaping guard
// never sees the change (Issue #1630 Task B1).
//
// Fails CLOSED: this is money-integrity enforcement, not observability. An unwired
// budgetCategoryRepo refuses the reparent rather than silently allowing it — mirrors
// budget_service.go:651's goalOwnershipChecker precedent for a required (if optionally-injected)
// collaborator.
//
// ⚠ Issue #1630 B3b: the guard and the write run in ONE transaction, not two. The earlier version
// of this method (checkReparentDoesNotEscape) only ran the guard, locking each budget through
// GetCategoriesByBudgetIDLocked — which opens and COMMITS its own transaction, releasing the lock
// before UpdateCategory's caller persisted the reparent afterward in a SEPARATE transaction on
// CustomCategoryRepository. That gap meant two concurrent reparents could each lock, check, and
// release against a stale view, and both go on to commit an escaping pair — the same race B3
// closed for AddCategory/UpdateCategory, left open here. This method closes it by opening ONE
// transaction via budgetCategoryRepo.BeginTx, locking every affected budget through
// GetCategoriesByBudgetIDLockedInTx (never releasing those locks until it returns), and — only if
// every budget clears the guard — persisting the write via categoryRepo.UpdateInTx before
// committing. A losing concurrent writer blocks on the row lock until the winner commits or rolls
// back, then re-evaluates against the winner's now-visible state.
//
// Evaluates the PROPOSED hierarchy, not the current one: GetCategoriesByBudgetID(Locked)(InTx)
// populates a budget row's CustomParentID/CustomParentPFCPrimary from the CURRENT (pre-write)
// custom_categories row (budget_repository.go's join). A check run before this write therefore
// describes the OLD parentage unless the candidate's overlay is set to category's NEW
// ParentID/ParentPFCPrimary before domain.EscapedNestedChild runs — which is exactly what this
// does for the reparented category's own budgeted row, below.
//
// Walks the affected subtree: also considers category's direct children (the only descendant
// depth a custom-custom nest can have — custom_category.go's Validate/validateParent caps
// custom-custom nesting at two levels, so there are no grandchildren to walk on that axis). A
// child's own custom_categories.parent_id is untouched by this edit, so its containment status
// relative to category never changes as a direct RESULT of this specific edit; it is included
// here as defence-in-depth so a future change to what this method mutates does not silently stop
// covering it, not because today's narrower edit can flip it.
//
// Scoped NARROWLY: only refuses when the SPECIFIC budgeted row this category (or a budgeted
// child of it) would form an escaping pair with, in EITHER direction — domain.EscapedNestedChild
// checks both (candidate as the non-cleanly-contained CHILD of a parent, or candidate as the
// PARENT of a not-cleanly-contained child). Two category_only categories reparenting past each
// other never trips this; only the escaping shape is blocked.
func (s *customCategoryService) reparentCategoryChecked(ctx context.Context, userID string, category *domain.CustomCategory) error {
	if s.budgetCategoryRepo == nil {
		return fmt.Errorf("budget category repository not configured: cannot validate reparent of %s against existing budgets", category.ID)
	}

	children, err := s.categoryRepo.GetChildren(ctx, category.ID)
	if err != nil {
		return err
	}
	subtree := make(map[string]bool, len(children)+1)
	subtree[category.ID] = true
	for _, c := range children {
		subtree[c.ID] = true
	}

	// Issue #1630 review fix (HIGH): the PROPOSED grandparent PFC primary — mirrors
	// budget_repository.go's "gp" join hop. Only relevant when category is nesting under another
	// CUSTOM category (ParentID); a direct PFC-primary parent (ParentPFCPrimary) has no further
	// custom grandparent to report, same as CustomGrandparentPFCPrimary's own doc comment.
	var proposedGrandparentPFCPrimary *string
	if category.ParentID != nil {
		newParent, err := s.categoryRepo.GetByID(ctx, *category.ParentID)
		if err != nil {
			return err
		}
		proposedGrandparentPFCPrimary = newParent.ParentPFCPrimary
	}

	// Issue #1630 review fix (HIGH): include INACTIVE budgets too, not just active ones. An
	// inactive budget still holds its own rows and can be reactivated later by a plain is_active
	// update with no hierarchy validation of its own — reparenting under a budgeted-but-inactive
	// PFC root, then reactivating, would otherwise yield exactly the escaping/double-counted pair
	// this guard exists to prevent. GetByUserID returns every non-deleted budget regardless of
	// is_active (mirrors budgetRepository.GetByUserID); only deleted_at IS NULL is excluded.
	budgets, err := s.budgetCategoryRepo.GetByUserID(ctx, userID)
	if err != nil {
		return err
	}

	tx, err := s.budgetCategoryRepo.BeginTx(ctx)
	if err != nil {
		return err
	}
	// Rollback after a successful commit is a no-op; its error is not actionable.
	defer tx.Rollback(ctx) //nolint:errcheck

	for _, budget := range budgets {
		// Issue #1630 B3b: locked INSIDE this shared transaction, not a self-committing helper —
		// the lock is held until tx.Commit/Rollback below, across every remaining budget's check
		// AND the custom_categories write, closing the reparent-vs-reparent race.
		cats, err := s.budgetCategoryRepo.GetCategoriesByBudgetIDLockedInTx(ctx, tx, budget.ID)
		if err != nil {
			return err
		}

		for i := range cats {
			if cats[i].CategoryKind != "custom" || cats[i].CustomCategoryID == nil || !subtree[*cats[i].CustomCategoryID] {
				continue
			}

			candidate := cats[i]
			if *cats[i].CustomCategoryID == category.ID {
				// The reparented row itself: overlay the PROPOSED hierarchy (see doc comment
				// above) — GetCategoriesByBudgetID's join still reflects the pre-write parent.
				candidate.CustomParentID = category.ParentID
				candidate.CustomParentPFCPrimary = category.ParentPFCPrimary
				// Issue #1630 review fix (HIGH): also overlay the PROPOSED grandparent — without
				// this, a reparent onto an unbudgeted intermediate custom parent that itself
				// nests under a budgeted PFC root leaves CustomGrandparentPFCPrimary stale/nil,
				// and the escaping-grandchild shape sails past the guard exactly as it did on
				// AddCategory before that fix.
				candidate.CustomGrandparentPFCPrimary = proposedGrandparentPFCPrimary
			}
			// A budgeted CHILD of the reparented category (candidate's CustomCategoryID != this
			// category's ID, but is in `subtree`) keeps pointing at the same parent — its own
			// custom_categories.parent_id is untouched by this edit — so no overlay is needed
			// for it; it is still checked, per the doc comment above.

			existing := make([]domain.BudgetCategory, 0, len(cats)-1)
			for j := range cats {
				if j != i {
					existing = append(existing, cats[j])
				}
			}

			if domain.EscapedNestedChild(existing, &candidate) {
				return fmt.Errorf("%w: reparenting %q would create a nested pair in budget %q whose match_logic escapes clean containment", domain.ErrEscapedNestedChild, category.Name, budget.ID)
			}
		}
	}

	// Every affected budget cleared the guard while its lock was held under THIS transaction —
	// persist the reparent before releasing any of them.
	if err := s.categoryRepo.UpdateInTx(ctx, tx, category); err != nil {
		return err
	}

	return tx.Commit(ctx)
}

func (s *customCategoryService) DeleteCategory(ctx context.Context, userID, categoryID string) error {
	category, err := s.categoryRepo.GetByID(ctx, categoryID)
	if err != nil {
		return err
	}

	if category.UserID != userID {
		return domain.ErrCategoryNotFound
	}

	// Tombstones the category, disables the rules that targeted it, reparents its children and
	// tombstones its budget lines — all in one transaction, leaving every historical transaction
	// exactly as the user left it (Issue #1569 Task 9; was a five-step destructive delete).
	return s.categoryRepo.DeleteWithReassignment(ctx, userID, categoryID, s.auditLogRepo)
}

// GetCategoryUsage returns live reference counts for a category (Issue #1203).
func (s *customCategoryService) GetCategoryUsage(ctx context.Context, userID, categoryID string) (*domain.CategoryUsage, error) {
	// Verify ownership first so we don't leak counts for another user's category.
	category, err := s.categoryRepo.GetByID(ctx, categoryID)
	if err != nil {
		return nil, err
	}
	if category.UserID != userID {
		return nil, domain.ErrCategoryNotFound
	}
	return s.categoryRepo.GetCategoryUsage(ctx, userID, categoryID)
}

func (s *customCategoryService) GetOrCreateCategory(ctx context.Context, userID, categoryName string) (*domain.CustomCategory, error) {
	category, err := s.categoryRepo.GetByName(ctx, userID, categoryName)
	if err == nil {
		category.PopulatePFCDisplay()
		return category, nil
	}

	if err == domain.ErrCategoryNotFound {
		req := &domain.CreateCustomCategoryRequest{
			Name: categoryName,
		}
		return s.CreateCategory(ctx, userID, req)
	}

	return nil, err
}
