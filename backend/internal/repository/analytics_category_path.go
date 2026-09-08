package repository

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
		return `CASE WHEN ` + detailed + ` IS NOT NULL
		              AND ` + detailed + ` <> ` + primary + `
		              AND ` + detailed + ` LIKE ` + primary + ` || '\_%'
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
