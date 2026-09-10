package repository

import (
	"fmt"
	"strings"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
)

// ─── The category filter group (#1626) ───────────────────────────────────────────────────────────
//
// A transaction's category is a discriminated union: `category_override_kind` is 'custom', 'pfc',
// or NULL, and the generated `effective_pfc_*` columns are NULL for a custom-kind row (migration
// 000073). So the PFC predicates and the custom predicate select DISJOINT row sets.
//
// The group now has FOUR legs: PFC primary, PFC detailed, custom category, and uncategorised
// (#1626 Phase 2).
//
// They used to be combined with AND — the PFC pair ORed with each other, then the custom predicate
// appended as its own top-level condition. The conjunction of two disjoint sets is empty, so ANY
// request carrying both kinds returned zero rows by construction. Nothing could depend on that
// behaviour, which is why widening it to OR is a fix and not a contract change.
//
// This lives in one function because the predicates exist in TWO builders — buildWhereClause and
// the keyset-paginated GetByUserIDKeysetUnified — and only the second is on the live
// transaction-list path. Fixing one and not the other is invisible in review.
// Pinned by TestBothTransactionBuildersShareTheCategoryGroup.

// categoryFilterGroup renders the OR-joined category predicate for the given filters, binding
// placeholders from argIdx onward. It returns the empty string when no category filter is set —
// never a `1=1` tautology, which would silently widen an unrelated query.
//
// $1 must be the user id: the custom-category subquery scopes ownership through it.
func categoryFilterGroup(filters domain.TransactionFilters, argIdx int, args []interface{}) (string, []interface{}, int) {
	var legs []string

	bindAll := func(values []string) string {
		phs := make([]string, len(values))
		for i, v := range values {
			phs[i] = fmt.Sprintf("$%d", argIdx)
			args = append(args, v)
			argIdx++
		}
		return strings.Join(phs, ", ")
	}

	if len(filters.PFCPrimaries) > 0 {
		legs = append(legs, "t.effective_pfc_primary IN ("+bindAll(filters.PFCPrimaries)+")")
	}
	if len(filters.PFCDetaileds) > 0 {
		legs = append(legs, "t.effective_pfc_detailed IN ("+bindAll(filters.PFCDetaileds)+")")
	}
	if filters.Uncategorized {
		// The rows the analytics layer reports under its synthesised 'UNCATEGORIZED' sentinel.
		//
		// effectiveCategoryExpr reaches that sentinel only through its ELSE arm,
		// `COALESCE(t.pfc_primary, 'UNCATEGORIZED')`, which is taken exactly when
		// category_override_kind IS NULL — and that arm consults ONLY pfc_primary. So this predicate
		// mirrors the sentinel's own definition.
		//
		// ⚠ It deliberately does NOT rest on `CHECK valid_category_override_discriminator` forbidding
		// leftover override columns on a kind-NULL row. Under SQL three-valued logic that CHECK
		// evaluates to UNKNOWN for such a row, which Postgres ACCEPTS, so those columns can exist.
		// The equivalence holds because the CASE ignores them, not because they are impossible.
		//
		// Binds no parameters, so argIdx is untouched.
		legs = append(legs, "(t.category_override_kind IS NULL AND t.pfc_primary IS NULL)")
	}

	if len(filters.CustomCategoryIDs) > 0 {
		idList := bindAll(filters.CustomCategoryIDs)
		// The id list expands to itself plus its children (parent_id), so filtering by a parent
		// custom category returns the transactions filed under its children too.
		legs = append(legs, fmt.Sprintf(
			"(t.category_override_kind = 'custom' AND t.custom_category_override_id IN ("+
				"SELECT id FROM custom_categories WHERE user_id = $1::uuid AND (id::text IN (%s) OR parent_id::text IN (%s))))"+
				" /* tombstone-exempt(custom_categories): HISTORY FILTER (#1569 Task 9). This expands a"+
				" requested category id to itself plus its children so the filter matches the"+
				" transactions already filed under it. Excluding tombstones would make a user's own"+
				" past transactions vanish from their own filtered view. The PICKER that supplies"+
				" these ids is CustomCategoryRepository.GetByUserID, which IS tombstone-filtered. */",
			idList, idList))
	}

	if len(legs) == 0 {
		return "", args, argIdx
	}
	return " AND (" + strings.Join(legs, " OR ") + ")", args, argIdx
}
