//go:build integration

package repository

// Issue #1630 Task B2 — the PFC-root roll-up was only ONE hop deep.
//
// A three-level chain is legal and constructible: pfc X <- custom C (parent_pfc_primary = X)
// <- custom G (parent_id = C). custom_category_service.go's validateParent and the DB trigger
// enforce_custom_category_two_levels both reject a proposed parent only when its OWN parent_id
// is set — neither rejects nesting under a row whose parent_pfc_primary is set — so this chain
// passes through the ordinary API. buildCategoryClause's "pfc" branch rolled up only
// `custom_categories WHERE parent_pfc_primary = X`, which catches C but NOT G (G's own
// parent_pfc_primary is NULL — G is parented by parent_id, not parent_pfc_primary). If a budget
// is on X and C is excluded as X's sub-allocation, a transaction filed under G appeared in NO
// top-level total.
//
// pgxmock cannot catch this: the pre-fix query was syntactically valid SQL, and a pgxmock test
// only asserts the literal SQL string/args, never executes real roll-up semantics against real
// rows and a real 2-hop join.
//
// Run with:
//
//	DATABASE_URL=postgres://... CACHE_REDIS_TEST_URL=... REDIS_TEST_URL=... \
//	  go test ./internal/repository/... -tags integration -p 1 \
//	  -run TestGetSpentSumForBudgetCategory_PFCRoot_RollsUpGrandchild -v

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
)

// seedUserAccountAndChainTx seeds one user, one linked item, one account, a pfc-rooted custom
// category C, and a custom grandchild G nested under C via parent_id. Returns the account ID
// (for filing transactions) and G's category ID.
func seedUserAccountAndChainTx(t *testing.T, ctx context.Context, tx pgx.Tx, suffix, pfcPrimary string) (userID, accountID, linkedItemID, childID, grandchildID string) {
	t.Helper()

	userID = uuid.New().String()
	_, err := tx.Exec(ctx, `
		INSERT INTO users (id, clerk_user_id, email, subscription_tier, subscription_status, primary_region)
		VALUES ($1, $2, $3, 'premium', 'active', 'iad')
	`, userID, "test|"+userID, "claim1630-b2-"+suffix+"-"+userID+"@example.com")
	require.NoError(t, err, "seed user")

	linkedItemID = uuid.New().String()
	_, err = tx.Exec(ctx, `
		INSERT INTO linked_items (id, user_id, item_id, access_token_encrypted)
		VALUES ($1, $2, $3, 'enc-token')
	`, linkedItemID, userID, "item-claim1630-b2-"+suffix+"-"+linkedItemID)
	require.NoError(t, err, "seed linked item")

	accountID = uuid.New().String()
	_, err = tx.Exec(ctx, `
		INSERT INTO accounts (id, linked_item_id, user_id, plaid_account_id, name, type)
		VALUES ($1, $2, $3, $4, 'Checking', 'depository')
	`, accountID, linkedItemID, userID, "plaid-claim1630-b2-"+suffix)
	require.NoError(t, err, "seed account")

	childID = uuid.New().String()
	_, err = tx.Exec(ctx, `
		INSERT INTO custom_categories (id, user_id, name, parent_pfc_primary)
		VALUES ($1, $2, 'Eating Out ('||$3||')', $4)
	`, childID, userID, suffix, pfcPrimary)
	require.NoError(t, err, "seed custom child C nested under pfc primary via parent_pfc_primary")

	grandchildID = uuid.New().String()
	_, err = tx.Exec(ctx, `
		INSERT INTO custom_categories (id, user_id, name, parent_id)
		VALUES ($1, $2, 'Coffee runs ('||$3||')', $4)
	`, grandchildID, userID, suffix, childID)
	require.NoError(t, err, "seed custom grandchild G nested under C via parent_id")

	return userID, accountID, linkedItemID, childID, grandchildID
}

func TestGetSpentSumForBudgetCategory_PFCRoot_RollsUpGrandchild(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()

	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx) //nolint:errcheck

	const pfcPrimary = "FOOD_AND_DRINK"
	userID, accountID, linkedItemID, childID, grandchildID := seedUserAccountAndChainTx(t, ctx, tx, "solo", pfcPrimary)

	periodStart := time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)
	periodEnd := time.Date(2026, time.January, 31, 23, 59, 59, 0, time.UTC)
	txDate := time.Date(2026, time.January, 15, 0, 0, 0, 0, time.UTC)

	const seededSpend = 42.75
	_, err = tx.Exec(ctx, `
		INSERT INTO transactions
			(id, user_id, account_id, linked_item_id, source, amount, date, name, pending,
			 category_override_kind, custom_category_override_id)
		VALUES ($1, $2, $3, $4, 'plaid', $5, $6, 'Coffee run', false,
			'custom', $7)
	`, uuid.New().String(), userID, accountID, linkedItemID, seededSpend, txDate, grandchildID)
	require.NoError(t, err, "seed the one transaction filed under grandchild G")

	enc := newTestFieldEncryptor(t)
	repo := NewTransactionRepository(tx, enc)

	rootPrimary := pfcPrimary
	rootCategory := &domain.BudgetCategory{
		CategoryKind: "pfc",
		PFCPrimary:   &rootPrimary,
		MatchLogic:   domain.MatchLogicCategoryOnly,
	}
	rootSum, err := repo.GetSpentSumForBudgetCategory(ctx, userID, rootCategory, periodStart, periodEnd)
	require.NoError(t, err)
	assert.Equal(t, seededSpend, rootSum,
		"#1630 Task B2: the PFC root's roll-up must reach a GRANDCHILD nested two hops down "+
			"(pfc root -> custom child via parent_pfc_primary -> custom grandchild via parent_id), "+
			"not merely a direct child. Before the fix, buildCategoryClause's pfc branch only ever "+
			"selected custom_categories WHERE parent_pfc_primary = root, which finds the child C but "+
			"never G, so this assertion failed with 0 instead of the seeded spend.")

	childCat := childID
	childCategory := &domain.BudgetCategory{
		CategoryKind:     "custom",
		CustomCategoryID: &childCat,
		MatchLogic:       domain.MatchLogicCategoryOnly,
	}
	childSum, err := repo.GetSpentSumForBudgetCategory(ctx, userID, childCategory, periodStart, periodEnd)
	require.NoError(t, err)
	assert.Equal(t, seededSpend, childSum,
		"sanity check: the custom branch already rolled up its own custom children via "+
			"parent_id — this was already correct and must remain so")
}

// TestGetSpentSumForBudgetCategory_PFCRoot_GrandchildRollupIsUserScoped proves the widened
// 2-hop join does not leak another user's grandchild spend into this user's PFC-root total.
// Two users each build an identical pfc-root -> custom child -> custom grandchild chain under
// the SAME pfc primary and file a transaction under their own grandchild; each user's root sum
// must reflect only their own transaction.
func TestGetSpentSumForBudgetCategory_PFCRoot_GrandchildRollupIsUserScoped(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()

	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx) //nolint:errcheck

	const pfcPrimary = "FOOD_AND_DRINK"
	userA, accountA, linkedItemA, _, grandchildA := seedUserAccountAndChainTx(t, ctx, tx, "userA", pfcPrimary)
	userB, accountB, linkedItemB, _, grandchildB := seedUserAccountAndChainTx(t, ctx, tx, "userB", pfcPrimary)

	periodStart := time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)
	periodEnd := time.Date(2026, time.January, 31, 23, 59, 59, 0, time.UTC)
	txDate := time.Date(2026, time.January, 15, 0, 0, 0, 0, time.UTC)

	const spendA = 10.00
	const spendB = 99.00
	_, err = tx.Exec(ctx, `
		INSERT INTO transactions
			(id, user_id, account_id, linked_item_id, source, amount, date, name, pending,
			 category_override_kind, custom_category_override_id)
		VALUES ($1, $2, $3, $4, 'plaid', $5, $6, 'A''s coffee', false, 'custom', $7)
	`, uuid.New().String(), userA, accountA, linkedItemA, spendA, txDate, grandchildA)
	require.NoError(t, err, "seed user A's grandchild transaction")

	_, err = tx.Exec(ctx, `
		INSERT INTO transactions
			(id, user_id, account_id, linked_item_id, source, amount, date, name, pending,
			 category_override_kind, custom_category_override_id)
		VALUES ($1, $2, $3, $4, 'plaid', $5, $6, 'B''s coffee', false, 'custom', $7)
	`, uuid.New().String(), userB, accountB, linkedItemB, spendB, txDate, grandchildB)
	require.NoError(t, err, "seed user B's grandchild transaction")

	enc := newTestFieldEncryptor(t)
	repo := NewTransactionRepository(tx, enc)

	rootPrimary := pfcPrimary
	rootCategory := &domain.BudgetCategory{
		CategoryKind: "pfc",
		PFCPrimary:   &rootPrimary,
		MatchLogic:   domain.MatchLogicCategoryOnly,
	}

	sumA, err := repo.GetSpentSumForBudgetCategory(ctx, userA, rootCategory, periodStart, periodEnd)
	require.NoError(t, err)
	assert.Equal(t, spendA, sumA,
		"#1630 Task B2: user A's PFC-root total must equal ONLY user A's grandchild spend — "+
			"if the widened join's user_id scoping (g.user_id = $1 in the outer WHERE, plus "+
			"c.user_id = g.user_id on the join predicate) were dropped, user B's much larger "+
			"spend would leak in and this would read spendA+spendB instead")

	sumB, err := repo.GetSpentSumForBudgetCategory(ctx, userB, rootCategory, periodStart, periodEnd)
	require.NoError(t, err)
	assert.Equal(t, spendB, sumB,
		"#1630 Task B2: user B's PFC-root total must equal ONLY user B's grandchild spend")
}
