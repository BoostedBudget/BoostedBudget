//go:build integration

package repository

// Issue #1626 — the three-level category path.
//
// WHY THIS IS AN INTEGRATION TEST. The whole subject is a resolution question spread across two
// LEFT JOINs, a nullable self-FK, and a discriminated union whose generated columns are NULL for
// one arm. pgxmock asserts the query STRING and then hands back rows the test author invented, so
// it cannot tell a correct join from one keyed on the wrong column, nor a correct CASE order from
// an inverted one. Only Postgres can.
//
// THE DEPTH IS THREE, NOT TWO. enforce_custom_category_two_levels fires BEFORE INSERT OR UPDATE OF
// parent_id only, and its no-grandchildren arm tests the PARENT's parent_id — never its
// parent_pfc_primary. validateParent checks only parent.ParentID too. So a custom category may nest
// under a PFC primary AND have custom children of its own, giving
// FOOD_AND_DRINK -> "Eating Out" -> "Coffee runs". Nothing guards it. Case "custom child under a
// custom parent that is itself under a PFC primary" below constructs exactly that chain, which is
// also proof it is reachable rather than theoretical.

import (
	"context"
	"fmt"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCategoryPathExprs_ResolveEveryReachableShape_Integration(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()

	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx) //nolint:errcheck

	userID := uuid.New().String()
	accountID := uuid.New().String()
	linkedItemID := uuid.New().String()
	userAccountID := uuid.New().String()

	mustExec := func(what, sql string, args ...any) {
		t.Helper()
		_, execErr := tx.Exec(ctx, sql, args...)
		require.NoError(t, execErr, "seed %s", what)
	}

	mustExec("user", `
		INSERT INTO users (id, clerk_user_id, email, subscription_tier, subscription_status, primary_region)
		VALUES ($1, $2, $3, 'premium', 'active', 'iad')
	`, userID, "test|"+userID, "cat-path-1626-"+userID+"@example.com")
	mustExec("user_account", `
		INSERT INTO user_accounts (id, user_id, institution_id, primary_type)
		VALUES ($1, $2, 'ins-cat-path-1626', 'depository')
	`, userAccountID, userID)
	mustExec("linked_item", `
		INSERT INTO linked_items (id, user_id, item_id, access_token_encrypted)
		VALUES ($1, $2, $3, 'enc-test-token')
	`, linkedItemID, userID, "cat-path-item-"+linkedItemID)
	mustExec("account", `
		INSERT INTO accounts (id, linked_item_id, user_id, user_account_id, plaid_account_id, name, type, status)
		VALUES ($1, $2, $3, $4, $5, 'Cat Path Checking', 'depository', 'active')
	`, accountID, linkedItemID, userID, userAccountID, "plaid-acc-cp-"+accountID)

	// ── custom categories ────────────────────────────────────────────────────────────────────────
	soloID := uuid.New().String()        // no parent at all
	underPFCID := uuid.New().String()    // parent_pfc_primary = MEDICAL
	topParentID := uuid.New().String()   // top-level custom parent
	childOfTopID := uuid.New().String()  // parent_id -> topParent
	pfcParentID := uuid.New().String()   // custom parent that ITSELF sits under FOOD_AND_DRINK
	grandChildID := uuid.New().String()  // parent_id -> pfcParent  => THREE levels
	deadParentID := uuid.New().String()  // tombstoned custom parent
	childOfDeadID := uuid.New().String() // parent_id -> tombstoned parent

	mustExec("solo", `INSERT INTO custom_categories (id, user_id, name) VALUES ($1,$2,'Solo 1626')`, soloID, userID)
	mustExec("underPFC", `INSERT INTO custom_categories (id, user_id, name, parent_pfc_primary) VALUES ($1,$2,'Under PFC 1626','MEDICAL')`, underPFCID, userID)
	mustExec("topParent", `INSERT INTO custom_categories (id, user_id, name) VALUES ($1,$2,'Top Parent 1626')`, topParentID, userID)
	mustExec("childOfTop", `INSERT INTO custom_categories (id, user_id, name, parent_id) VALUES ($1,$2,'Child Of Top 1626',$3)`, childOfTopID, userID, topParentID)

	// The three-level chain (PFC primary -> custom parent -> custom child). Parent first is the only
	// valid order, and that is forced by the self-FK on parent_id, not by the trigger: the referenced
	// row has to exist. Giving the parent its parent_pfc_primary in the same INSERT is irrelevant to
	// trg_custom_category_two_levels, which inspects ONLY parent_id (migration 000078) — nesting
	// under a PFC primary does not make the parent a child for its two-level rule, which is exactly
	// why this three-level shape is reachable at all rather than rejected.
	mustExec("pfcParent", `INSERT INTO custom_categories (id, user_id, name, parent_pfc_primary) VALUES ($1,$2,'Eating Out 1626','FOOD_AND_DRINK')`, pfcParentID, userID)
	mustExec("grandChild", `INSERT INTO custom_categories (id, user_id, name, parent_id) VALUES ($1,$2,'Coffee Runs 1626',$3)`, grandChildID, userID, pfcParentID)

	mustExec("deadParent", `
		INSERT INTO custom_categories (id, user_id, name, deleted_at, deleted_reason, deleted_by)
		VALUES ($1,$2,'Dead Parent 1626', NOW(), 'user_deleted', 'user')`, deadParentID, userID)
	mustExec("childOfDead", `INSERT INTO custom_categories (id, user_id, name, parent_id) VALUES ($1,$2,'Child Of Dead 1626',$3)`, childOfDeadID, userID, deadParentID)

	// ── transactions, one per shape ──────────────────────────────────────────────────────────────
	type seed struct {
		tag                          string
		pfcPrimary, pfcDetailed      *string
		overrideKind                 *string
		primaryOverride, detOverride *string
		customID                     *string
		effectiveName                *string
	}
	sp := func(s string) *string { return &s }

	seeds := []seed{
		{tag: "pfc-raw-detailed", pfcPrimary: sp("FOOD_AND_DRINK"), pfcDetailed: sp("FOOD_AND_DRINK_FAST_FOOD"), effectiveName: sp("FOOD_AND_DRINK")},
		{tag: "pfc-raw-no-detailed", pfcPrimary: sp("TRANSPORTATION"), effectiveName: sp("TRANSPORTATION")},
		{tag: "pfc-override-matching", pfcPrimary: sp("MEDICAL"), overrideKind: sp("pfc"), primaryOverride: sp("TRAVEL"), detOverride: sp("TRAVEL_FLIGHTS"), effectiveName: sp("TRAVEL")},
		{tag: "pfc-override-mismatched", pfcPrimary: sp("MEDICAL"), overrideKind: sp("pfc"), primaryOverride: sp("FOOD_AND_DRINK"), detOverride: sp("TRAVEL_FLIGHTS"), effectiveName: sp("FOOD_AND_DRINK")},
		{tag: "no-primary-at-all"},
		{tag: "custom-solo", overrideKind: sp("custom"), customID: &soloID, effectiveName: sp("Solo 1626")},
		{tag: "custom-under-pfc", overrideKind: sp("custom"), customID: &underPFCID, effectiveName: sp("Under PFC 1626")},
		{tag: "custom-child-of-top", overrideKind: sp("custom"), customID: &childOfTopID, effectiveName: sp("Child Of Top 1626")},
		{tag: "custom-grandchild", overrideKind: sp("custom"), customID: &grandChildID, effectiveName: sp("Coffee Runs 1626")},
		{tag: "custom-child-of-tombstoned", overrideKind: sp("custom"), customID: &childOfDeadID, effectiveName: sp("Child Of Dead 1626")},
	}

	for i, s := range seeds {
		mustExec("txn "+s.tag, `
			INSERT INTO transactions
			  (id, user_id, account_id, user_account_id, external_transaction_id, name, amount, date, pending,
			   source, pfc_primary, pfc_detailed, category_override_kind, pfc_primary_override,
			   pfc_detailed_override, custom_category_override_id, effective_category_name)
			VALUES ($1,$2,$3,$4,$5,$6,$7,'2026-02-15',false,'plaid',$8,$9,$10,$11,$12,$13,$14)
		`, uuid.New().String(), userID, accountID, userAccountID,
			fmt.Sprintf("plaid-txn-cp-%d-%s", i, uuid.New().String()), "txn "+s.tag, 10.0+float64(i),
			s.pfcPrimary, s.pfcDetailed, s.overrideKind, s.primaryOverride, s.detOverride, s.customID, s.effectiveName)
	}

	// ── run the path expressions ─────────────────────────────────────────────────────────────────
	l1k, l1d, l2k, l2d, l3k, l3d := categoryPathExprs()
	query := fmt.Sprintf(`
		SELECT t.name,
		       (%s) AS l1_key, (%s) AS l1_display,
		       (%s) AS l2_key, (%s) AS l2_display,
		       (%s) AS l3_key, (%s) AS l3_display
		FROM transactions t
		%s
		WHERE t.user_id = $1
	`, l1k, l1d, l2k, l2d, l3k, l3d, customCategoryPathJoins())

	rows, err := tx.Query(ctx, query, userID)
	require.NoError(t, err, "path query must execute:\n%s", query)
	defer rows.Close()

	type path struct{ l1k, l1d, l2k, l2d, l3k, l3d string }
	got := map[string]path{}
	for rows.Next() {
		var name string
		var a, b, c, d, e, f *string
		require.NoError(t, rows.Scan(&name, &a, &b, &c, &d, &e, &f))
		deref := func(p *string) string {
			if p == nil {
				return ""
			}
			return *p
		}
		got[name] = path{deref(a), deref(b), deref(c), deref(d), deref(e), deref(f)}
	}
	require.NoError(t, rows.Err())
	require.Len(t, got, len(seeds))

	custom := func(id string) string { return "custom:" + id }

	cases := []struct {
		tag  string
		want path
		why  string
	}{
		{tag: "txn pfc-raw-detailed", want: path{l1k: "FOOD_AND_DRINK", l2k: "FOOD_AND_DRINK_FAST_FOOD"}},
		{tag: "txn pfc-raw-no-detailed", want: path{l1k: "TRANSPORTATION"},
			why: "a primary with no detailed must NOT emit a self-edge to itself"},
		{tag: "txn pfc-override-matching", want: path{l1k: "TRAVEL", l2k: "TRAVEL_FLIGHTS"}},
		{tag: "txn pfc-override-mismatched", want: path{l1k: "FOOD_AND_DRINK"},
			why: "there is NO check constraining pfc_detailed_override against its primary (only the " +
				"RAW pair has valid_pfc_detailed_suffix), so a mismatched override is schema-legal and " +
				"must be dropped rather than emitted as the false edge FOOD_AND_DRINK -> TRAVEL_FLIGHTS"},
		{tag: "txn no-primary-at-all", want: path{l1k: "UNCATEGORIZED"}},
		{tag: "txn custom-solo", want: path{l1k: custom(soloID), l1d: "Solo 1626"},
			why: "a parentless custom category is its own top level"},
		{tag: "txn custom-under-pfc", want: path{l1k: "MEDICAL", l2k: custom(underPFCID), l2d: "Under PFC 1626"}},
		{tag: "txn custom-child-of-top", want: path{
			l1k: custom(topParentID), l1d: "Top Parent 1626",
			l2k: custom(childOfTopID), l2d: "Child Of Top 1626"}},
		{tag: "txn custom-grandchild", want: path{
			l1k: "FOOD_AND_DRINK",
			l2k: custom(pfcParentID), l2d: "Eating Out 1626",
			l3k: custom(grandChildID), l3d: "Coffee Runs 1626"},
			why: "THE three-level chain: a custom parent under a PFC primary, with a custom child"},
		{tag: "txn custom-child-of-tombstoned", want: path{
			l1k: custom(deadParentID), l1d: "Dead Parent 1626",
			l2k: custom(childOfDeadID), l2d: "Child Of Dead 1626"},
			why: "analytics answer questions about the past: a child under a TOMBSTONED parent must " +
				"still resolve to it, or deleting a category would silently restate closed months"},
	}

	for _, tc := range cases {
		t.Run(tc.tag, func(t *testing.T) {
			assert.Equal(t, tc.want, got[tc.tag], tc.why)
		})
	}
}
