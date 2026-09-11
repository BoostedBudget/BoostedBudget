package repository

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
)

// ── Issue #1630 Task B7: auditing pre-existing budgets the write-time guards would refuse ────────
//
// domain.DuplicateParentIdentity and domain.EscapedNestedChild are write-time guards
// (AddCategoryChecked / UpdateCategoryChecked / CreateBudget's batch loop): they refuse a NEW row
// that would collide on parent identity or escape a nested parent's containment envelope, but
// neither is ever run against a row that was persisted before it existed. This file is the audit
// that goes looking for those pre-existing rows, mirroring #1569 Task 11's portfolio_snapshots
// repair (repository/portfolio_snapshot_repair.go) — a ledgered, idempotent pass that gives every
// budget it examines a verdict.
//
// WHAT IS SAFE TO REPAIR AND WHAT IS NOT (this is the whole design)
//   - spent_amount is a derived cache with one unambiguous recomputation, exactly analogous to
//     portfolio_snapshots.total_value: recomputing it makes the cache agree with the user's own
//     config, which is safe no matter what shape the budget is in. The recomputation is the narrow
//     one services.NarrowBudgetPeriodDates + BudgetSpentCalculator.CalculateCategorySpent already
//     established for Task B1's reparent resync (queue/jobs/budget_hierarchy_resync.go) — NOT
//     BudgetSyncService.RecalculateBudget, which also recalculates income, derives non-custom
//     periods from time.Now() rather than the budget's own stored anchor, fires budget_updated
//     automation that can trigger a user's boosts, and swallows per-category failures before
//     returning success. An audit built on that primitive could mutate unrelated income, misfire
//     automations, and ledger a partial repair as complete — none of which this audit may do.
//   - match_logic on an escaping pair, and the identity fields behind a duplicate-parent pair, are
//     REPORT ONLY and are NEVER rewritten. There is no correct automatic answer for either: changing
//     match_logic changes what the user's budget matches, and changing which row "owns" a colliding
//     parent identity is an equally arbitrary guess. Auto-repairing either would be worse than the
//     bug — an invisible new wrongness replacing a visible old one, and it would destroy the exact
//     evidence a human would need to ask the user what they meant. See ErrEscapedNestedChild's doc
//     comment in domain/budget.go for the same reasoning already applied to the write-time guard.
//
// WHY THIS IS NOT ONE SQL STATEMENT, unlike repairSelectSQL
// The portfolio repair's recomputation is a plain SQL aggregate over holding_snapshots, so
// diagnosis + UPDATE + ledger INSERT fit in one statement (portfolio_snapshot_repair.go:78-330).
// CalculateCategorySpent's recomputation is NOT expressible that way: it dispatches per
// category_kind/match_logic (tag matching, PFC hierarchy roll-up, custom-category roll-up) through
// Go-side query construction (transaction_repository.go's buildCategoryClause), and duplicating
// that dispatch as a second, parallel SQL template would be the exact kind of drift Task B1's
// resync job was written to avoid by calling the SAME calculator. So the shape here is: the
// diagnostic READS (loading categories, calling CalculateCategorySpent) run first, against
// currently-committed data, outside any transaction this audit owns; every WRITE this audit makes —
// every spent_amount correction AND the ledger row recording the verdict — commits in ONE
// transaction (AuditBudgetHierarchy below). A crash before that transaction's Commit leaves NEITHER
// the corrections NOR the ledger row; a crash after leaves BOTH. What the one-statement precedent
// buys is exactly this write-atomicity, not atomicity over the diagnostic read — and reading
// slightly-stale-by-milliseconds committed data before deciding what to write is the same risk every
// other resync job in this codebase (including Task B1's) already accepts.
//
// IDEMPOTENCY
// budget_hierarchy_audits has a UNIQUE index on budget_id; the ledger INSERT here is ON CONFLICT DO
// NOTHING, and each spent_amount UPDATE is guarded by IS DISTINCT FROM. A second run against a
// budget already in the ledger repeats the same recomputation (harmless — CalculateCategorySpent is
// pure over the transaction data it reads) and writes nothing: no corrections (already correct), no
// second ledger row (deduplicated). The periodic sweep never manufactures that second run in the
// first place — BudgetIDsWithUnexaminedHierarchy excludes any budget already in the ledger, exactly
// as portfolio_snapshot_repair.go's UserIDsWithUnexaminedSnapshots does for users.

// BudgetHierarchySpentCalculator is the minimal spend-recomputation dependency this audit needs —
// structurally the same interface services.BudgetSpentCalculator declares, redeclared locally so
// this package never imports services (services already imports repository; the reverse would
// cycle). services.budgetCategoryMatchingService satisfies this with no adapter, exactly the way
// budgetHierarchyResyncCategoryRepo mirrors a services-side interface in the jobs package.
type BudgetHierarchySpentCalculator interface {
	CalculateCategorySpent(ctx context.Context, userID string, category *domain.BudgetCategory, startDate, endDate time.Time) (float64, error)
}

// budgetHierarchyAuditReasonDuplicateIdentity / EscapingPair / Repaired / Verified build the
// human-readable `reason` column. Kept as functions (not constants) because each one needs to name
// the specific rows involved — the whole point of recording a reason is that an operator can act
// without re-deriving the check by hand.
func budgetHierarchyAuditReason(outcome string, flaggedDuplicate, flaggedEscaping []string, categoriesRepaired int) string {
	switch outcome {
	case domain.BudgetHierarchyAuditOutcomeReportedDuplicateIdentity:
		return fmt.Sprintf(
			"budget_categories %v share a live parent identity (domain.DuplicateParentIdentity) — "+
				"which row \"owns\" that identity in rendering is undefined, and neither is rewritten "+
				"because there is no way to tell which one the user meant to keep. This is a write-time "+
				"guard added going forward; this budget predates it. %d categor(ies) had a stale "+
				"spent_amount corrected independently of this finding.",
			flaggedDuplicate, categoriesRepaired)
	case domain.BudgetHierarchyAuditOutcomeReportedEscapingPair:
		return fmt.Sprintf(
			"budget_categories %v form a hierarchically-nested pair whose match_logic escapes clean "+
				"containment (domain.EscapedNestedChild) — the parent's roll-up and the child's own "+
				"top-level total both count the same transactions. match_logic is never rewritten by "+
				"this audit: changing it changes what the budget matches, and there is no correct "+
				"automatic answer for what the user meant. This is a write-time guard added going "+
				"forward; this budget predates it. %d categor(ies) had a stale spent_amount corrected "+
				"independently of this finding.",
			flaggedEscaping, categoriesRepaired)
	case domain.BudgetHierarchyAuditOutcomeRepaired:
		return fmt.Sprintf(
			"no duplicate-identity or escaping-pair shape found; %d categor(ies)' stored spent_amount "+
				"disagreed with the narrow recomputation and were corrected", categoriesRepaired)
	default: // BudgetHierarchyAuditOutcomeVerified
		return "no duplicate-identity or escaping-pair shape found, and every live category's stored " +
			"spent_amount already matched the recomputation; nothing written"
	}
}

// decideBudgetHierarchyAuditOutcome picks ONE outcome for the ledger row. A budget can in principle
// carry both a duplicate-identity pair and an escaping pair at once; duplicate-identity is reported
// preferentially because it is the more fundamental ambiguity (which row IS the category at all)
// versus escaping (which transactions a category matches). Both findings, when both are present, are
// still visible in flaggedDuplicate/flaggedEscaping — only the single-valued `outcome` column has to
// choose.
func decideBudgetHierarchyAuditOutcome(flaggedDuplicate, flaggedEscaping map[string]bool, categoriesRepaired int) string {
	switch {
	case len(flaggedDuplicate) > 0:
		return domain.BudgetHierarchyAuditOutcomeReportedDuplicateIdentity
	case len(flaggedEscaping) > 0:
		return domain.BudgetHierarchyAuditOutcomeReportedEscapingPair
	case categoriesRepaired > 0:
		return domain.BudgetHierarchyAuditOutcomeRepaired
	default:
		return domain.BudgetHierarchyAuditOutcomeVerified
	}
}

// budgetHierarchyAuditSortedKeys returns the keys of a set map, sorted, for a deterministic ledger column and a
// deterministic reason string.
func budgetHierarchyAuditSortedKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// AuditBudgetHierarchy examines ONE budget's live categories for the two write-time-guard shapes
// (duplicate parent identity, escaping nested pair) and recomputes every live non-savings category's
// spent_amount against the budget's own stored period, all inside one transaction with the ledger
// INSERT — see this file's header comment for the full design and why this is not one raw SQL
// statement.
//
// budget must be a freshly-loaded *domain.Budget with Categories already populated (as
// BudgetRepository.GetByID / GetActiveByUserID already do) — this audit does not re-fetch it, so a
// caller iterating many budgets should load each one itself. periodStart/periodEnd must come from
// services.NarrowBudgetPeriodDates(budget) (or an equivalent that anchors on the budget's OWN
// stored period, never time.Now()) — this package cannot import services (services already imports
// repository) so it accepts the already-derived bounds rather than recomputing them.
//
// Returns domain.ErrBudgetNotFound if budget is nil or has no ID.
func (r *budgetRepository) AuditBudgetHierarchy(ctx context.Context, budget *domain.Budget, periodStart, periodEnd time.Time, spentCalc BudgetHierarchySpentCalculator) (domain.BudgetHierarchyAuditVerdict, error) {
	var verdict domain.BudgetHierarchyAuditVerdict
	if budget == nil || budget.ID == "" {
		return verdict, domain.ErrBudgetNotFound
	}

	categories := budget.Categories

	// Detect both shapes for EVERY category against every OTHER category in the same budget — not
	// just against categories seen so far. Both domain.DuplicateParentIdentity and
	// domain.EscapedNestedChild are written to be checked in either direction precisely so
	// call-site/iteration order never matters (see EscapedNestedChild's doc comment); checking each
	// row against its full set of siblings reproduces that symmetry instead of depending on
	// categories' storage order.
	flaggedDuplicate := map[string]bool{}
	flaggedEscaping := map[string]bool{}
	for i := range categories {
		candidate := categories[i]
		others := make([]domain.BudgetCategory, 0, len(categories)-1)
		for j := range categories {
			if j != i {
				others = append(others, categories[j])
			}
		}
		if domain.DuplicateParentIdentity(others, &candidate) {
			flaggedDuplicate[candidate.ID] = true
		}
		if domain.EscapedNestedChild(others, &candidate) {
			flaggedEscaping[candidate.ID] = true
		}
	}

	// Recompute spend for every LIVE non-savings category, scoped to the budget's own stored
	// period. Savings-kind spend comes from the confirmed-contribution ledger (Issue #1300), not
	// transaction matching — untouched here, exactly as budget_hierarchy_resync.go excludes it.
	type recomputedSpend struct {
		categoryID string
		newAmount  float64
	}
	recomputed := make([]recomputedSpend, 0, len(categories))
	for i := range categories {
		cat := &categories[i]
		if cat.CategoryKind == "savings" {
			continue
		}
		spent, err := spentCalc.CalculateCategorySpent(ctx, budget.UserID, cat, periodStart, periodEnd)
		if err != nil {
			return verdict, fmt.Errorf("audit budget hierarchy %s: recompute spend for category %s: %w", budget.ID, cat.ID, err)
		}
		recomputed = append(recomputed, recomputedSpend{categoryID: cat.ID, newAmount: spent})
	}

	// Every write this audit makes — the spent_amount corrections AND the ledger row — commits in
	// one transaction. A crash or error anywhere in this block leaves neither; only Commit below
	// makes either durable.
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return verdict, fmt.Errorf("audit budget hierarchy %s: begin tx: %w", budget.ID, err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck // rollback after a successful commit is a no-op

	categoriesRepaired := 0
	for _, rc := range recomputed {
		// IS DISTINCT FROM: write only a category that actually disagrees, so an already-correct
		// category comes out of this audit byte-identical, updated_at included — the same guard
		// repairSelectSQL's UPDATE uses, for the same reason: "the audit changed nothing the second
		// time" has to stay observable.
		ct, err := tx.Exec(ctx, `
			UPDATE budget_categories
			SET spent_amount = $1, updated_at = NOW()
			WHERE id = $2 AND deleted_at IS NULL
			  AND spent_amount IS DISTINCT FROM $1
		`, rc.newAmount, rc.categoryID)
		if err != nil {
			return verdict, fmt.Errorf("audit budget hierarchy %s: repair category %s: %w", budget.ID, rc.categoryID, err)
		}
		if ct.RowsAffected() > 0 {
			categoriesRepaired++
		}
	}

	outcome := decideBudgetHierarchyAuditOutcome(flaggedDuplicate, flaggedEscaping, categoriesRepaired)
	flaggedDuplicateIDs := budgetHierarchyAuditSortedKeys(flaggedDuplicate)
	flaggedEscapingIDs := budgetHierarchyAuditSortedKeys(flaggedEscaping)
	reason := budgetHierarchyAuditReason(outcome, flaggedDuplicateIDs, flaggedEscapingIDs, categoriesRepaired)

	allFlagged := map[string]bool{}
	for _, id := range flaggedDuplicateIDs {
		allFlagged[id] = true
	}
	for _, id := range flaggedEscapingIDs {
		allFlagged[id] = true
	}
	flaggedIDs := budgetHierarchyAuditSortedKeys(allFlagged)

	// ON CONFLICT (budget_id) DO NOTHING: the unique index makes a second examination of this
	// budget a no-op rather than a duplicate row or an error, whether the second call comes from a
	// direct re-invocation or from two concurrent audits racing the same never-before-examined
	// budget.
	_, err = tx.Exec(ctx, `
		INSERT INTO budget_hierarchy_audits (
			budget_id, user_id, outcome, reason, categories_examined, categories_repaired,
			flagged_category_ids
		) VALUES ($1, $2, $3, $4, $5, $6, $7)
		ON CONFLICT (budget_id) DO NOTHING
	`, budget.ID, budget.UserID, outcome, reason, len(categories), categoriesRepaired, flaggedIDs)
	if err != nil {
		return verdict, fmt.Errorf("audit budget hierarchy %s: insert ledger row: %w", budget.ID, err)
	}

	if err := tx.Commit(ctx); err != nil {
		return verdict, fmt.Errorf("audit budget hierarchy %s: commit: %w", budget.ID, err)
	}

	verdict = domain.BudgetHierarchyAuditVerdict{
		BudgetID:           budget.ID,
		UserID:             budget.UserID,
		Outcome:            outcome,
		Reason:             reason,
		CategoriesExamined: len(categories),
		CategoriesRepaired: categoriesRepaired,
		FlaggedCategoryIDs: flaggedIDs,
		ExaminedAt:         time.Now().UTC(),
	}
	return verdict, nil
}

// budgetIDsWithUnexaminedHierarchySQL enumerates LIVE budgets that have never been examined by
// AuditBudgetHierarchy, MOST BEHIND FIRST (oldest budget first) — mirroring
// UserIDsWithUnexaminedSnapshots's starvation-avoidance ordering (portfolio_snapshot_repair.go):
// without an explicit order, whichever budgets sort first would refill the cap on every run and
// everything after them would never be reached, while the job still logs a full, successful pass
// each time.
//
// Unlike the portfolio repair's per-user, per-day enumeration (which recurs daily because a NEW
// unexamined day appears every day), a budget is examined AT MOST ONCE — the ledger has no notion
// of "yesterday's budget_hierarchy_audits row is stale". So this enumeration drains the backlog of
// budgets that predate this audit and then goes permanently quiet for existing budgets; it still
// reaches every NEW budget created after this audit shipped, exactly once, whenever the periodic
// job next runs.
const budgetIDsWithUnexaminedHierarchySQL = `
	SELECT b.id::text
	FROM budgets b
	WHERE b.deleted_at IS NULL
	  AND NOT EXISTS (
	      SELECT 1 FROM budget_hierarchy_audits a WHERE a.budget_id = b.id)
	ORDER BY b.created_at ASC, b.id
	LIMIT $1::int
`

// BudgetIDsWithUnexaminedHierarchy implements BudgetRepository.BudgetIDsWithUnexaminedHierarchy —
// see that method's doc comment and budgetIDsWithUnexaminedHierarchySQL above.
func (r *budgetRepository) BudgetIDsWithUnexaminedHierarchy(ctx context.Context, limit int) ([]string, error) {
	if limit <= 0 {
		return []string{}, nil
	}
	rows, err := r.db.Query(ctx, budgetIDsWithUnexaminedHierarchySQL, limit)
	if err != nil {
		return nil, fmt.Errorf("enumerate budgets with unexamined hierarchy: %w", err)
	}
	defer rows.Close()

	out := []string{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("enumerate budgets with unexamined hierarchy: scan: %w", err)
		}
		out = append(out, id)
	}
	if rows.Err() != nil {
		return nil, fmt.Errorf("enumerate budgets with unexamined hierarchy: %w", rows.Err())
	}
	return out, nil
}
