package repository

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
)

// ─── The three-level category path (#1626) ───────────────────────────────────────────────────────
//
// The Sankey needs parents AND children in one payload, and a custom category's parent is only
// resolvable in SQL: it lives in custom_categories as either parent_pfc_primary (a PFC enum) or
// parent_id (a self-FK). The frontend can derive a PFC detailed's parent by string prefix — that
// relationship is enforced by CHECK valid_pfc_detailed_suffix — but it has nothing to derive a
// custom category's parent from.
//
// THE DEPTH IS THREE. custom_category_single_parent forbids a row holding both parent dimensions,
// and enforce_custom_category_two_levels blocks custom grandchildren — but that trigger fires
// BEFORE INSERT OR UPDATE OF parent_id only, and its no-grandchildren arm tests the parent's
// parent_id, never its parent_pfc_primary. So this is reachable through the ordinary API:
//
//	FOOD_AND_DRINK  ->  "Eating Out" (custom, parent_pfc_primary)  ->  "Coffee runs" (custom, parent_id)
//
// A FOURTH level is not reachable: nesting under a row that already has a parent_id is rejected,
// and so is nesting a row that already has children.
//
// DISPLAY COLUMNS CARRY CUSTOM NAMES ONLY. A PFC key is resolved to a label by the service, from
// the pfc registry — t.effective_category_name holds the RAW enum for PFC rows and so is not a
// display name (migration 000073 says so explicitly). A custom key has no registry to consult, so
// its name has to come from the join. That asymmetry is why these expressions emit NULL for a PFC
// level's display rather than echoing the enum.

// customCategoryPathJoins returns the two joins the path expressions read from.
//
// Plain LEFT JOINs on primary-key equality, deliberately NOT LATERAL: a LATERAL subquery is by
// definition correlated per outer row and forces a nested loop, whereas these let the planner
// hash-join. Both are tombstone-exempt, and each carries its own exemption comment because the
// guard requires a predicate of its reference's OWN — another table's filter in the same statement
// does not count.
func customCategoryPathJoins() string {
	return `
		LEFT JOIN custom_categories cc
		       ON cc.id = t.custom_category_override_id AND cc.user_id = t.user_id
		       /* tombstone-exempt(custom_categories cc): HISTORY FILTER (#1626). Analytics answer
		          questions about the past; an 'AND cc.deleted_at IS NULL' here would make a user's
		          own history vanish from their own report the day they tidied their category list,
		          restating a closed month. Same rule as effectiveTransferExpr. */
		LEFT JOIN custom_categories p
		       ON p.id = cc.parent_id AND p.user_id = cc.user_id
		       /* tombstone-exempt(custom_categories p): HISTORY FILTER (#1626). A child under a
		          TOMBSTONED parent must keep resolving to that parent, for the same reason. */`
}

// categoryPathExprs returns the six SELECT expressions naming the path's three levels: a stable key
// and a display for each. L2/L3 are NULL where the branch is shallower — the tree is ragged.
//
// cc.user_id = t.user_id, not the requesting user: under household scope the transaction's owner
// may be a different member, and the custom-category namespace is never widened by household scope.
func categoryPathExprs() (l1Key, l1Display, l2Key, l2Display, l3Key, l3Display string) {
	// A custom row's own token, and its parent's.
	const selfTok = `'custom:' || cc.id::text`
	const parentTok = `'custom:' || cc.parent_id::text`

	// Whether a PFC detailed value is a genuine child of the primary in play. There is NO check
	// constraining pfc_detailed_override against pfc_primary_override — valid_pfc_detailed_suffix
	// covers only the RAW pair — so a mismatched override is schema-legal and would otherwise emit
	// a false taxonomy edge such as FOOD_AND_DRINK -> TRAVEL_FLIGHTS. It is dropped instead.
	//
	// The same guard doubles as the no-self-edge rule: a detailed equal to its primary, or absent,
	// yields NULL rather than a level that points at itself.
	pfcChild := func(primary, detailed string) string {
		// starts_with(), not LIKE. `detailed LIKE primary || '\_%'` would escape only the separator
		// underscore this expression appends — every underscore INSIDE the primary itself would
		// remain a LIKE single-character wildcard, so FOOD_AND_DRINK would build the pattern
		// FOOD?AND?DRINK_%. No two PFC enums collide under that today, so it produced no wrong
		// answer, but it asserts something weaker than it reads as. starts_with is an exact,
		// wildcard-free prefix test and needs no escaping at all.
		return `CASE WHEN ` + detailed + ` IS NOT NULL
		              AND ` + detailed + ` <> ` + primary + `
		              AND starts_with(` + detailed + `, ` + primary + ` || '_')
		         THEN ` + detailed + ` END`
	}

	const rawPrimary = `COALESCE(t.pfc_primary, 'UNCATEGORIZED')`
	const ovrPrimary = `COALESCE(t.pfc_primary_override, 'UNCATEGORIZED')`

	l1Key = `CASE
		WHEN t.category_override_kind = 'custom' THEN
			CASE
				WHEN cc.parent_id IS NOT NULL THEN COALESCE(p.parent_pfc_primary, ` + parentTok + `)
				WHEN cc.parent_pfc_primary IS NOT NULL THEN cc.parent_pfc_primary
				ELSE ` + selfTok + `
			END
		WHEN t.category_override_kind = 'pfc' THEN ` + ovrPrimary + `
		ELSE ` + rawPrimary + `
	END`

	// L1 has a name only when L1 is itself a custom category: the child's parent when the chain
	// stops one above it, otherwise the row's own category.
	l1Display = `CASE
		WHEN t.category_override_kind = 'custom' THEN
			CASE
				WHEN cc.parent_id IS NOT NULL AND p.parent_pfc_primary IS NULL THEN p.name
				WHEN cc.parent_id IS NULL AND cc.parent_pfc_primary IS NULL THEN cc.name
			END
	END`

	l2Key = `CASE
		WHEN t.category_override_kind = 'custom' THEN
			CASE
				WHEN cc.parent_id IS NOT NULL AND p.parent_pfc_primary IS NOT NULL THEN ` + parentTok + `
				WHEN cc.parent_id IS NOT NULL THEN ` + selfTok + `
				WHEN cc.parent_pfc_primary IS NOT NULL THEN ` + selfTok + `
			END
		WHEN t.category_override_kind = 'pfc' THEN ` + pfcChild(ovrPrimary, "t.pfc_detailed_override") + `
		ELSE ` + pfcChild(rawPrimary, "t.pfc_detailed") + `
	END`

	l2Display = `CASE
		WHEN t.category_override_kind = 'custom' THEN
			CASE
				WHEN cc.parent_id IS NOT NULL AND p.parent_pfc_primary IS NOT NULL THEN p.name
				WHEN cc.parent_id IS NOT NULL THEN cc.name
				WHEN cc.parent_pfc_primary IS NOT NULL THEN cc.name
			END
	END`

	// Only the three-level chain reaches L3: a custom child whose custom parent is itself nested
	// under a PFC primary.
	l3Key = `CASE
		WHEN t.category_override_kind = 'custom'
		     AND cc.parent_id IS NOT NULL
		     AND p.parent_pfc_primary IS NOT NULL
		THEN ` + selfTok + `
	END`

	l3Display = `CASE
		WHEN t.category_override_kind = 'custom'
		     AND cc.parent_id IS NOT NULL
		     AND p.parent_pfc_primary IS NOT NULL
		THEN cc.name
	END`

	return l1Key, l1Display, l2Key, l2Display, l3Key, l3Display
}

// GetCategoryHierarchy returns one grouped row per leaf, carrying the whole ancestor path that
// leads to it, for a single flow direction. One round trip: the Sankey builds every view mode
// (consolidated, expanded, focused) from this payload without refetching.
//
// custom_category_ids deliberately narrows the WHOLE tree to custom-kind rows. buildSpendingFilter
// appends that scope as an unconditional `AND category_override_kind = 'custom'` with no PFC arm,
// which is the right meaning for "scope this report to these custom categories" — and it is what
// the reports page's CustomCategoryFilter already does to the Sankey today. It is NOT the same
// question as the transactions-list filter, which #1626 widened to an OR, so the two must not be
// unified.
func (r *analyticsRepository) GetCategoryHierarchy(ctx context.Context, userID string, filters domain.SpendingFilters, direction domain.CategoryDirection) ([]domain.CategoryHierarchyRow, error) {
	if filters.HouseholdScope != nil {
		return r.getCategoryHierarchyHousehold(ctx, userID, filters, direction)
	}

	args := []interface{}{userID, filters.StartDate, filters.EndDate}
	filterClause, args, _ := buildSpendingFilter(filters, args)

	query := fmt.Sprintf(`
		%s
		FROM transactions t
		LEFT JOIN accounts a ON t.account_id = a.id
		%s
		WHERE t.user_id = $1
		  AND t.deleted_at IS NULL
		  AND t.date >= $2
		  AND t.date <= $3
		  AND %s
		  AND t.pending = false
		  %s
		%s
	`, categoryHierarchySelect(), customCategoryPathJoins(), directionPredicate(direction), filterClause,
		categoryHierarchyGroupAndOrder())

	return r.scanCategoryHierarchy(ctx, query, args, "category hierarchy")
}

// getCategoryHierarchyHousehold is the household-scope twin. Every category-dimensioned read in
// this file has one, and they are spelled out separately on purpose: a divergence between the two
// members of a pair is how a defect hid on this branch before.
func (r *analyticsRepository) getCategoryHierarchyHousehold(ctx context.Context, requestingUserID string, filters domain.SpendingFilters, direction domain.CategoryDirection) ([]domain.CategoryHierarchyRow, error) {
	args := householdScopeArgs(requestingUserID, filters.HouseholdScope)
	args = append(args, filters.StartDate, filters.EndDate)
	filterClause, args, _ := buildSpendingFilter(filters, args)

	query := fmt.Sprintf(`
		WITH %s
		%s
		FROM transactions t
		JOIN accounts a ON t.account_id = a.id
		%s
		WHERE %s
		  AND t.date >= $4::date
		  AND t.deleted_at IS NULL
		  AND t.date <= $5
		  AND %s
		  AND t.pending = false
		  %s
		%s
	`, householdGrantsCTE(), categoryHierarchySelect(), customCategoryPathJoins(),
		householdSharedTransactionPredicate("t", "a"), directionPredicate(direction), filterClause,
		categoryHierarchyGroupAndOrder())

	return r.scanCategoryHierarchy(ctx, query, args, "household category hierarchy")
}

// directionPredicate renders the amount test for a flow direction. The sign convention is this
// file's: income is amount < 0, expense is amount > 0.
func directionPredicate(direction domain.CategoryDirection) string {
	if direction == domain.CategoryDirectionIncome {
		return "t.amount < 0"
	}
	return "t.amount > 0"
}

// categoryHierarchySelect renders the SELECT list: the six path columns, a leaf display used for
// ordering, and the aggregates.
func categoryHierarchySelect() string {
	l1k, l1d, l2k, l2d, l3k, l3d := categoryPathExprs()
	return fmt.Sprintf(`SELECT
			(%s) AS l1_key, (%s) AS l1_display,
			(%s) AS l2_key, (%s) AS l2_display,
			(%s) AS l3_key, (%s) AS l3_display,
			COALESCE(%s, %s, %s, %s, %s) AS category_display,
			COALESCE(SUM(ABS(t.amount)), 0) AS total,
			COUNT(*) AS transaction_count`,
		l1k, l1d, l2k, l2d, l3k, l3d,
		// The deepest name available for the row's own (leaf) category, falling back to the leaf
		// KEY so a PFC leaf — which has no stored name — still sorts alphabetically rather than
		// collapsing every PFC row onto a single NULL.
		l3d, l2d, l1d, fmt.Sprintf("COALESCE(%s, %s)", l3k, l2k), l1k)
}

// categoryHierarchyGroupAndOrder renders the GROUP BY and the #1585 deterministic ORDER BY.
//
// `category_display ASC` is the sanctioned alphabetical leg the tie-order guard looks for; the
// three keys after it are what actually separate two rows whose leaf names are identical, which is
// reachable because custom category names are not unique across parents.
func categoryHierarchyGroupAndOrder() string {
	return `GROUP BY l1_key, l1_display, l2_key, l2_display, l3_key, l3_display, category_display
		ORDER BY total DESC, category_display ASC, l1_key ASC, l2_key ASC, l3_key ASC`
}

func (r *analyticsRepository) scanCategoryHierarchy(ctx context.Context, query string, args []interface{}, what string) ([]domain.CategoryHierarchyRow, error) {
	rows, err := r.db.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to get %s: %w", what, err)
	}
	defer rows.Close()

	results := make([]domain.CategoryHierarchyRow, 0)
	for rows.Next() {
		var row domain.CategoryHierarchyRow
		// Every path column past L1 is nullable — the tree is ragged — and the display columns are
		// null for PFC levels by design.
		var l1d, l2k, l2d, l3k, l3d, leafDisplay sql.NullString
		if err := rows.Scan(&row.L1Key, &l1d, &l2k, &l2d, &l3k, &l3d, &leafDisplay,
			&row.Total, &row.TransactionCount); err != nil {
			return nil, fmt.Errorf("failed to scan %s row: %w", what, err)
		}
		row.L1Display, row.L2Key, row.L2Display = l1d.String, l2k.String, l2d.String
		row.L3Key, row.L3Display = l3k.String, l3d.String
		results = append(results, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating %s rows: %w", what, err)
	}
	return results, nil
}
