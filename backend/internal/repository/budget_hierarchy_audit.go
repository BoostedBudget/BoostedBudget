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
// diagnostic READS (loading categories, calling CalculateCategorySpent) and every WRITE this audit
// makes — every spent_amount correction AND the ledger row recording the verdict — all happen
// INSIDE the one transaction AuditBudgetHierarchy opens, AFTER that transaction locks every live
// category row for this budget FOR UPDATE. A crash before that transaction's Commit leaves NEITHER
// the corrections NOR the ledger row; a crash after leaves BOTH. Locking the category rows before
// reading them (Issue #1630 review fix, HIGH — see AuditBudgetHierarchy's own doc comment) is what
// ties the later write to the snapshot the diagnostic read produced: a concurrent writer to the
// SAME rows (BudgetRepository.UpdateCategorySpentAmount, called by Task B1's own resync job and by
// BudgetSyncService.RecalculateBudget) either commits before this audit's lock query runs — so this
// audit's own read already reflects it — or blocks until this audit's transaction ends, so its
// write lands strictly after and is never clobbered.
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

// lockLiveCategoriesForAuditInTx locks every live budget_categories row for budgetID FOR UPDATE,
// inside tx, without reading their columns — the read that matters (getCategoriesByBudgetIDQuerier)
// runs separately, right after this returns, so it observes exactly what this lock query already
// froze. Deliberately NOT `FOR UPDATE` tacked onto getCategoriesByBudgetIDQuerier's own query: that
// query LEFT JOINs custom_categories/savings_goals, and locking would need `FOR UPDATE OF bc` to
// avoid touching the nullable side of those joins — simpler, and cheaper for every OTHER caller of
// that shared query, to keep the lock as its own narrow statement used only here.
//
// Blocks until any other transaction already holding one of these row locks commits or rolls
// back — in particular BudgetRepository.UpdateCategorySpentAmount's bare
// `UPDATE budget_categories ... WHERE id = $2`, which takes the same implicit row lock for the
// duration of its own (separate) transaction. See AuditBudgetHierarchy's doc comment for why this,
// not lockBudgetRowInTx (which only locks the budgets table and would not serialize with that
// UPDATE at all).
func (r *budgetRepository) lockLiveCategoriesForAuditInTx(ctx context.Context, tx Querier, budgetID string) error {
	rows, err := tx.Query(ctx, `
		SELECT id FROM budget_categories WHERE budget_id = $1 AND deleted_at IS NULL FOR UPDATE
	`, budgetID)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var discard string
		if err := rows.Scan(&discard); err != nil {
			return err
		}
	}
	return rows.Err()
}

// AuditBudgetHierarchy examines ONE budget's live categories for the two write-time-guard shapes
// (duplicate parent identity, escaping nested pair) and recomputes every live non-savings category's
// spent_amount against the budget's own stored period, all inside one transaction with the ledger
// INSERT — see this file's header comment for the full design and why this is not one raw SQL
// statement.
//
// budget need only carry a valid ID (and UserID, for the ledger row and CalculateCategorySpent) —
// budget.Categories, if populated by the caller, is IGNORED. Every read this audit acts on — the
// category list, and the spend recomputation — happens AFTER this function locks every live
// budget_categories row for this budget FOR UPDATE inside its own write transaction, not before it.
//
// Issue #1630 review fix (HIGH): the previous version read categories and recomputed spend BEFORE
// opening the write transaction, with no lock or version check tying the later write to that
// snapshot. A concurrent transaction-sync recompute (BudgetRepository.UpdateCategorySpentAmount,
// called by both queue/jobs/budget_hierarchy_resync.go's Task B1 resync and
// BudgetSyncService.RecalculateBudget) landing in that window could commit a correct NEWER
// spent_amount that this audit would then silently overwrite with its stale value — and it would
// still mark the budget as permanently examined, so BudgetIDsWithUnexaminedHierarchy would never
// revisit it to fix the clobber.
//
// UpdateCategorySpentAmount is a bare `UPDATE budget_categories ... WHERE id = $2`, with no
// transaction and no lock of its own beyond the row lock Postgres implicitly takes for the
// statement's own duration — it never touches the budgets table, so locking the BUDGET row (the
// technique lockBudgetRowInTx uses for AddCategoryChecked/UpdateCategoryChecked's structural
// guards) would not serialize with it at all. This audit instead locks the budget_categories rows
// themselves (lockLiveCategoriesForAuditInTx) — the exact rows UpdateCategorySpentAmount's UPDATE
// needs to touch — so a concurrent call to it must either (a) commit before this audit's lock
// query runs, in which case this audit's own read already reflects it, or (b) block until this
// audit's transaction commits or rolls back, in which case its write lands strictly AFTER this
// audit's and simply applies on top — never clobbered either way.
//
// periodStart/periodEnd must come from services.NarrowBudgetPeriodDates(budget) (or an equivalent
// that anchors on the budget's OWN stored period, never time.Now()) — this package cannot import
// services (services already imports repository) so it accepts the already-derived bounds rather
// than recomputing them.
//
// Returns domain.ErrBudgetNotFound if budget is nil or has no ID.
func (r *budgetRepository) AuditBudgetHierarchy(ctx context.Context, budget *domain.Budget, periodStart, periodEnd time.Time, spentCalc BudgetHierarchySpentCalculator) (domain.BudgetHierarchyAuditVerdict, error) {
	var verdict domain.BudgetHierarchyAuditVerdict
	if budget == nil || budget.ID == "" {
		return verdict, domain.ErrBudgetNotFound
	}

	// Every write this audit makes — the spent_amount corrections AND the ledger row — commits in
	// one transaction. A crash or error anywhere in this block leaves neither; only Commit below
	// makes either durable.
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return verdict, fmt.Errorf("audit budget hierarchy %s: begin tx: %w", budget.ID, err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck // rollback after a successful commit is a no-op

	// Lock every live category row for this budget FOR UPDATE, then re-read them INSIDE this
	// transaction — see this method's doc comment above for why the read must happen here, after
	// the lock, and not before this transaction opened.
	if err := r.lockLiveCategoriesForAuditInTx(ctx, tx, budget.ID); err != nil {
		return verdict, fmt.Errorf("audit budget hierarchy %s: lock categories: %w", budget.ID, err)
	}
	categories, err := r.getCategoriesByBudgetIDQuerier(ctx, tx, budget.ID)
	if err != nil {
		return verdict, fmt.Errorf("audit budget hierarchy %s: load categories: %w", budget.ID, err)
	}

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
	// period, while still holding the budget row's lock acquired above. Savings-kind spend comes
	// from the confirmed-contribution ledger (Issue #1300), not transaction matching — untouched
	// here, exactly as budget_hierarchy_resync.go excludes it.
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

// ── Issue #1630 Task B7: the human-facing surface ─────────────────────────────────────────────
//
// "A reported finding that reaches nobody is not a fix" — the periodic worker's WARN log line
// (BudgetHierarchyAuditWorker.Work) is the paging/log-alerting path; this method is the durable,
// queryable one, backing the admin listing (RegisterAdminBudgetHierarchyAuditOperations,
// internal/huma/operations/admin_budget_hierarchy_audits.go). It reads the SAME ledger table
// AuditBudgetHierarchy writes, so there is no second source of truth to drift.

// listBudgetHierarchyAuditFindingsSQL selects ledger rows, most-recently-examined first.
// flagged_category_ids is cast to text[] because pgx's default type registry has no built-in
// codec for scanning a uuid[] column into a Go []string — every category id here is already a
// UUID stored as text everywhere else this codebase surfaces one (domain.BudgetCategory.ID is a
// string), so the cast loses nothing.
const listBudgetHierarchyAuditFindingsSQL = `
	SELECT budget_id::text, user_id::text, outcome, reason, categories_examined, categories_repaired,
	       flagged_category_ids::text[], examined_at
	FROM budget_hierarchy_audits
	WHERE ($1::varchar = '' OR outcome = $1)
	ORDER BY examined_at DESC
	LIMIT $2::int
`

// ListBudgetHierarchyAuditFindings returns ledger rows, most-recently-examined first, capped at
// limit. outcome filters to exactly that outcome when non-empty; an empty outcome returns every
// verdict this audit has ever recorded (repaired and verified included, not only the reported
// ones) — the admin handler is what defaults the query to the actionable subset, since "what does
// this table contain at all" is a legitimate question this method should be able to answer too.
func (r *budgetRepository) ListBudgetHierarchyAuditFindings(ctx context.Context, outcome string, limit int) ([]domain.BudgetHierarchyAuditVerdict, error) {
	if limit <= 0 {
		return []domain.BudgetHierarchyAuditVerdict{}, nil
	}
	rows, err := r.db.Query(ctx, listBudgetHierarchyAuditFindingsSQL, outcome, limit)
	if err != nil {
		return nil, fmt.Errorf("list budget hierarchy audit findings: %w", err)
	}
	defer rows.Close()

	out := []domain.BudgetHierarchyAuditVerdict{}
	for rows.Next() {
		var v domain.BudgetHierarchyAuditVerdict
		var flagged []string
		if err := rows.Scan(&v.BudgetID, &v.UserID, &v.Outcome, &v.Reason, &v.CategoriesExamined,
			&v.CategoriesRepaired, &flagged, &v.ExaminedAt); err != nil {
			return nil, fmt.Errorf("list budget hierarchy audit findings: scan: %w", err)
		}
		v.FlaggedCategoryIDs = flagged
		out = append(out, v)
	}
	if rows.Err() != nil {
		return nil, fmt.Errorf("list budget hierarchy audit findings: %w", rows.Err())
	}
	return out, nil
}
