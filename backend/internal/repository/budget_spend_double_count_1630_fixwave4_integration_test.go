//go:build integration

package repository

// Issue #1630 fixwave 4 — a SECOND independent adversarial review found the fixwave 3 fix above
// (budget_spend_double_count_1630_integration_test.go, domain.EscapedNestedChild) was itself
// incomplete: it only ever refused a nested pair when the PARENT's own match_logic was
// "containing" (category_only/or). An 'and' or 'tag_only' parent was silently assumed safe with
// ANY nested child, because the reasoning that motivated fixwave 3 only considered the specific
// mechanism where buildCategoryClause's hierarchical roll-up feeds a parent whose OWN category
// clause is exactly "category_only". That assumption is false: an 'and' parent's clause is
// `parent_category ∧ parent_tags`, and buildCategoryClause still substitutes the nested child's
// category into the `parent_category` term before the tag filter is applied — so a transaction
// carrying the parent's own tag AND filed under the (cleanly contained) child's category is swept
// into the parent's sum by the category term, then ALSO counted as the child's own top-level row,
// because domain.IsSubAllocationWithin only excludes a child when its PARENT is containing.
//
// This test drives that exact 'and'-parent shape through the REAL repository query path (not a
// pgxmock expectation, which cannot execute real roll-up/tag-filter semantics) and shows the two
// sums are NOT disjoint: they cover the identical seeded transaction twice, even though the CHILD
// here is 'category_only' — the one match_logic that IsSubAllocationWithin treats as safely
// contained when its parent is containing. The bug is entirely on the PARENT side.
//
// Fixed the same way as fixwave 3: refusing to PERSIST this shape at write time
// (domain.EscapedNestedChild, widened in fixwave 4 to refuse ANY hierarchically-nested pair that
// is not cleanly contained — isContainingMatchLogic(parent) && isContainedMatchLogic(child) — not
// just the subset where the parent was already known to be containing). Not fixed by this query,
// which still has no sibling/match_logic context to subtract with.
//
// Run with:
//
//	DATABASE_URL=postgres://... CACHE_REDIS_TEST_URL=... REDIS_TEST_URL=... \
//	  go test ./internal/repository/... -tags integration -p 1 \
//	  -run TestGetSpentSumForBudgetCategory_AndParentDoubleCountsContainedChild -v

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
)

func TestGetSpentSumForBudgetCategory_AndParentDoubleCountsContainedChild(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()

	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx) //nolint:errcheck

	userID := uuid.New().String()
	_, err = tx.Exec(ctx, `
		INSERT INTO users (id, clerk_user_id, email, subscription_tier, subscription_status, primary_region)
		VALUES ($1, $2, $3, 'premium', 'active', 'iad')
	`, userID, "test|"+userID, "claim1630-fixwave4-"+userID+"@example.com")
	require.NoError(t, err, "seed user")

	linkedItemID := uuid.New().String()
	_, err = tx.Exec(ctx, `
		INSERT INTO linked_items (id, user_id, item_id, access_token_encrypted)
		VALUES ($1, $2, $3, 'enc-token')
	`, linkedItemID, userID, "item-claim1630-fixwave4-"+linkedItemID)
	require.NoError(t, err, "seed linked item")

	accountID := uuid.New().String()
	_, err = tx.Exec(ctx, `
		INSERT INTO accounts (id, linked_item_id, user_id, plaid_account_id, name, type)
		VALUES ($1, $2, $3, 'plaid-claim1630-fixwave4', 'Checking', 'depository')
	`, accountID, linkedItemID, userID)
	require.NoError(t, err, "seed account")

	// A custom category "Big Ticket Rent Stuff" nested under RENT_AND_UTILITIES — the CHILD is
	// 'category_only', which IsSubAllocationWithin treats as safely contained WHEN ITS PARENT IS
	// CONTAINING. The parent here is deliberately 'and', not 'category_only'/'or'.
	customCatID := uuid.New().String()
	_, err = tx.Exec(ctx, `
		INSERT INTO custom_categories (id, user_id, name, parent_pfc_primary)
		VALUES ($1, $2, 'Big Ticket Rent Stuff', 'RENT_AND_UTILITIES')
	`, customCatID, userID)
	require.NoError(t, err, "seed custom category nested under RENT_AND_UTILITIES")

	periodStart := time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)
	periodEnd := time.Date(2026, time.January, 31, 23, 59, 59, 0, time.UTC)
	txDate := time.Date(2026, time.January, 15, 0, 0, 0, 0, time.UTC)

	// ONE real transaction: filed under the nested custom category AND carrying the tag the
	// 'and' parent's own budget line matches on. amount > 0 means an outflow/expense (see
	// GetSpentSumForBudgetCategory's `amount > 0` filter).
	const seededSpend = 412.25
	_, err = tx.Exec(ctx, `
		INSERT INTO transactions
			(id, user_id, account_id, linked_item_id, source, amount, date, name, pending,
			 category_override_kind, custom_category_override_id, tags)
		VALUES ($1, $2, $3, $4, 'plaid', $5, $6, 'Big Ticket Rent Item', false,
			'custom', $7, '["splurge"]'::jsonb)
	`, uuid.New().String(), userID, accountID, linkedItemID, seededSpend, txDate, customCatID)
	require.NoError(t, err, "seed the one transaction both sums will independently claim")

	enc := newTestFieldEncryptor(t)
	repo := NewTransactionRepository(tx, enc)

	// The PARENT's own budget line: a budgeted 'and' PFC primary (category_only/or containment
	// rule does NOT apply here — this is exactly the parent shape the first fixwave-3 guard
	// silently assumed was safe with any nested child).
	parentPrimary := "RENT_AND_UTILITIES"
	parentCategory := &domain.BudgetCategory{
		CategoryKind: "pfc",
		PFCPrimary:   &parentPrimary,
		MatchLogic:   domain.MatchLogicAnd,
		Tags:         []string{"splurge"},
	}
	parentSum, err := repo.GetSpentSumForBudgetCategory(ctx, userID, parentCategory, periodStart, periodEnd)
	require.NoError(t, err)

	// The CHILD's own budget line: the nested custom category, 'category_only' — the ONE
	// match_logic IsSubAllocationWithin treats as a safe sub-allocation when its parent is
	// containing. Its parent here is NOT containing, so this child is still counted at the top
	// level too.
	childCategory := &domain.BudgetCategory{
		CategoryKind:     "custom",
		CustomCategoryID: &customCatID,
		MatchLogic:       domain.MatchLogicCategoryOnly,
	}
	childSum, err := repo.GetSpentSumForBudgetCategory(ctx, userID, childCategory, periodStart, periodEnd)
	require.NoError(t, err)

	assert.Equal(t, seededSpend, parentSum,
		"#1630 fixwave 4: the 'and' parent's own clause (`parent_category ∧ parent_tags`) legitimately "+
			"matches this transaction — buildCategoryClause rolls the nested child's category into the "+
			"category term, and the transaction carries the parent's tag too")
	assert.Equal(t, seededSpend, childSum,
		"#1630 fixwave 4: the child's own budget line must match the transaction filed directly under it")

	// THE DEFECT: domain.IsSubAllocationWithin (Issue #1630's original fix) does NOT exclude this
	// child from the top-level total, because its PARENT's match_logic ('and') is not
	// "containing" — budgetedParentSetsFor only ever adds a 'category_only'/'or' parent to the
	// eligible-parent sets. So the same $412.25 transaction is counted TWICE when a caller sums
	// parent.SpentAmount + child.SpentAmount over top-level rows, exactly as
	// domain.Budget.TotalSpent and analytics_repository.GetBudgetVsActual do. This is addressed at
	// WRITE time (domain.EscapedNestedChild, widened in fixwave 4 to refuse this exact shape),
	// not by changing this query, which has no sibling context to subtract with.
	require.False(t, childCategory.IsSubAllocationWithin(
		map[string]bool{}, // budgetedPFCPrimaries: EMPTY — the 'and' parent is deliberately NOT
		// eligible-parent per budgetedParentSetsFor, because isContainingMatchLogic('and') is false
		map[string]bool{},
	), "sanity check: a category_only child nested under a NON-containing ('and') parent must NOT "+
		"be treated as a sub-allocation — if this assertion ever fails, the double-count this test "+
		"pins no longer applies to this shape")
	assert.Equal(t, 2*seededSpend, parentSum+childSum,
		"#1630 fixwave 4 HIGH-severity finding: parentSum + childSum double-counts the one seeded "+
			"transaction because the 'and' parent's own clause overlaps the nested (cleanly-contained) "+
			"child's category, while IsSubAllocationWithin's containment rule only ever excludes a "+
			"child whose PARENT is 'category_only'/'or'. If this assertion starts failing because the "+
			"sum is no longer double the seeded spend, buildCategoryClause's roll-up (or "+
			"IsSubAllocationWithin's containment rule) changed in a way that deserves its own dedicated "+
			"regression test before this one is deleted.")
}
