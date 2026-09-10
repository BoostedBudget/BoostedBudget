//go:build integration

package repository

// Issue #1630 HIGH-severity adversarial-review finding.
//
// buildCategoryClause (transaction_repository.go, "pfc" branch) rolls up a budgeted PFC
// primary's spend HIERARCHICALLY: every transaction filed under a custom category nested under
// that primary (custom_categories.parent_pfc_primary) is included in the parent's own
// GetSpentSumForBudgetCategory sum — unconditionally, with no match_logic parameter and no
// sibling context at all.
//
// domain.IsSubAllocationWithin (Issue #1630's own fix) decides whether that SAME nested child is
// excluded from top-level budget totals, but it decides that ONLY from the child's own
// match_logic: a child is excluded (treated as already-rolled-up) only when its match_logic is
// 'category_only' or 'and'. A child whose match_logic is 'or' or 'tag_only' is NOT excluded — it
// is counted as its own top-level row. Both a legal BudgetCategory.Validate() pass (an
// 'or'/'tag_only' custom category needs no tags when 'or', and 'tag_only' requires only a
// non-empty tags array, both satisfiable independent of nesting).
//
// The consequence: a nested child with an escaping match_logic is counted through BOTH paths for
// the exact same transactions — once inside the parent's SpentAmount (via this hierarchical,
// match_logic-blind roll-up), and again as its own top-level SpentAmount. This test drives BOTH
// sums through the REAL repository query path (no fixture spent_amount values, which would make
// this overlap untestable — pgxmock can't catch it either, since it never executes real roll-up
// semantics against real rows) and shows they are NOT disjoint: they cover the identical seeded
// transaction twice.
//
// This double-count is NOT fixed by this SQL query (buildCategoryClause has no sibling/match_logic
// context to subtract with) — it is fixed by refusing to PERSIST the escaping shape at write time
// (domain.EscapedNestedChild, wired into budget_service.go's CreateBudget and AddCategory). This
// test seeds the shape directly via SQL, bypassing the service layer entirely, to prove the
// underlying query-level double-count is real and to guard against any future change to
// buildCategoryClause/GetSpentSumForBudgetCategory silently reintroducing it once the write-time
// guard is (correctly) the only thing standing between a request and this shape.
//
// Run with:
//
//	DATABASE_URL=postgres://... CACHE_REDIS_TEST_URL=... REDIS_TEST_URL=... \
//	  go test ./internal/repository/... -tags integration -p 1 \
//	  -run TestGetSpentSumForBudgetCategory_EscapedNestedChildDoubleCounts -v

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
)

func TestGetSpentSumForBudgetCategory_EscapedNestedChildDoubleCounts(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()

	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx) //nolint:errcheck

	userID := uuid.New().String()
	_, err = tx.Exec(ctx, `
		INSERT INTO users (id, clerk_user_id, email, subscription_tier, subscription_status, primary_region)
		VALUES ($1, $2, $3, 'premium', 'active', 'iad')
	`, userID, "test|"+userID, "claim1630-highfix-"+userID+"@example.com")
	require.NoError(t, err, "seed user")

	linkedItemID := uuid.New().String()
	_, err = tx.Exec(ctx, `
		INSERT INTO linked_items (id, user_id, item_id, access_token_encrypted)
		VALUES ($1, $2, $3, 'enc-token')
	`, linkedItemID, userID, "item-claim1630-highfix-"+linkedItemID)
	require.NoError(t, err, "seed linked item")

	accountID := uuid.New().String()
	_, err = tx.Exec(ctx, `
		INSERT INTO accounts (id, linked_item_id, user_id, plaid_account_id, name, type)
		VALUES ($1, $2, $3, 'plaid-claim1630-highfix', 'Checking', 'depository')
	`, accountID, linkedItemID, userID)
	require.NoError(t, err, "seed account")

	// A custom category "Splurge Rent Stuff" nested under RENT_AND_UTILITIES — the legal shape
	// from the issue: parent is a budgeted category_only PFC primary, child is a custom category
	// hierarchically nested under it via parent_pfc_primary, but the CHILD's own budget line will
	// be given match_logic 'or' (escaping containment).
	customCatID := uuid.New().String()
	_, err = tx.Exec(ctx, `
		INSERT INTO custom_categories (id, user_id, name, parent_pfc_primary)
		VALUES ($1, $2, 'Splurge Rent Stuff', 'RENT_AND_UTILITIES')
	`, customCatID, userID)
	require.NoError(t, err, "seed custom category nested under RENT_AND_UTILITIES")

	periodStart := time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)
	periodEnd := time.Date(2026, time.January, 31, 23, 59, 59, 0, time.UTC)
	txDate := time.Date(2026, time.January, 15, 0, 0, 0, 0, time.UTC)

	// ONE real transaction, filed under the nested custom category. amount > 0 in this schema
	// means an outflow/expense (see GetSpentSumForBudgetCategory's `amount > 0` filter).
	const seededSpend = 275.50
	_, err = tx.Exec(ctx, `
		INSERT INTO transactions
			(id, user_id, account_id, linked_item_id, source, amount, date, name, pending,
			 category_override_kind, custom_category_override_id)
		VALUES ($1, $2, $3, $4, 'plaid', $5, $6, 'Fancy Rent Add-on', false,
			'custom', $7)
	`, uuid.New().String(), userID, accountID, linkedItemID, seededSpend, txDate, customCatID)
	require.NoError(t, err, "seed the one transaction both sums will independently claim")

	enc := newTestFieldEncryptor(t)
	repo := NewTransactionRepository(tx, enc)

	// The PARENT's own budget line: a budgeted 'category_only' PFC primary. This is exactly the
	// shape domain.budgetedParentSetsFor treats as an "eligible to adopt children" parent
	// (isContainingMatchLogic('category_only') == true).
	parentPrimary := "RENT_AND_UTILITIES"
	parentCategory := &domain.BudgetCategory{
		CategoryKind: "pfc",
		PFCPrimary:   &parentPrimary,
		MatchLogic:   domain.MatchLogicCategoryOnly,
	}
	parentSum, err := repo.GetSpentSumForBudgetCategory(ctx, userID, parentCategory, periodStart, periodEnd)
	require.NoError(t, err)

	// The CHILD's own budget line: the nested custom category, but with match_logic 'or' — which
	// escapes containment (domain.isContainedMatchLogic('or') == false), so
	// domain.BudgetCategory.IsSubAllocationWithin does NOT treat it as a sub-allocation. It
	// therefore also counts at the TOP LEVEL in domain.Budget.TotalSpent alongside the parent.
	childCategory := &domain.BudgetCategory{
		CategoryKind:     "custom",
		CustomCategoryID: &customCatID,
		MatchLogic:       domain.MatchLogicOr,
		Tags:             nil, // 'or' needs no tags to pass BudgetCategory.Validate — that's the point
	}
	childSum, err := repo.GetSpentSumForBudgetCategory(ctx, userID, childCategory, periodStart, periodEnd)
	require.NoError(t, err)

	// Each sum, taken alone, is correct: the parent's hierarchical roll-up legitimately includes
	// the nested child's transaction, and the child's own query legitimately matches its own
	// transaction.
	assert.Equal(t, seededSpend, parentSum,
		"#1630: the parent PFC primary's spend must roll up its nested custom child's transactions "+
			"(buildCategoryClause's \"pfc\" branch) — that part of the roll-up is correct and unrelated "+
			"to the HIGH-severity double-count this test demonstrates")
	assert.Equal(t, seededSpend, childSum,
		"#1630: the child's own budget line must match the transaction filed directly under it")

	// THE DEFECT: domain.IsSubAllocationWithin (and TotalSpent/GetBudgetVsActual, which apply the
	// same rule) do NOT exclude this child from the top-level total, because its match_logic
	// ('or') escapes containment. So the same $275.50 transaction is counted TWICE when a caller
	// sums parent.SpentAmount + child.SpentAmount over top-level rows — exactly what
	// domain.Budget.TotalSpent and analytics_repository.GetBudgetVsActual do. This is the
	// double-count an adversarial review of Issue #1630's own containment fix found; it is
	// addressed at WRITE time (domain.EscapedNestedChild refuses to persist this shape going
	// forward), not by changing this query, which has no sibling context to subtract with.
	require.False(t, childCategory.IsSubAllocationWithin(
		map[string]bool{parentPrimary: true}, // budgetedPFCPrimaries: the parent IS budgeted+containing
		map[string]bool{},
	), "sanity check: an 'or' child must NOT be treated as a sub-allocation (Issue #1630's own rule) "+
		"— if this assertion ever fails, the double-count this test pins no longer applies to this shape")
	assert.Equal(t, 2*seededSpend, parentSum+childSum,
		"#1630 HIGH-severity finding: parentSum + childSum double-counts the one seeded transaction "+
			"because the parent's roll-up is match_logic-blind while the child, whose own match_logic "+
			"escapes containment, is never excluded from the top-level sum. If this assertion starts "+
			"failing because the sum is no longer double the seeded spend, buildCategoryClause's "+
			"hierarchical roll-up (or IsSubAllocationWithin's containment rule) changed in a way that "+
			"deserves its own dedicated regression test before this one is deleted.")
}
