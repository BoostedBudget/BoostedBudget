//go:build integration

package repository

// Issue #1630 Task 5 — GetCategoryMatchBreakdown, the query that discloses a rolled-up
// preview total's per-nested-child split. pgxmock cannot validate the CASE/JOIN/GROUP BY
// shape this query relies on (it never catches aggregate/type errors), so this is an
// integration test against real Postgres rather than a pgxmock unit test.

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetCategoryMatchBreakdown_Integration(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()

	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx) //nolint:errcheck

	repo := NewTransactionRepository(tx, newTestFieldEncryptor(t))

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
		VALUES ($1,$2,$3,'premium','active','iad')`, userID, "test|"+userID, "1630-breakdown-"+userID+"@example.com")
	mustExec("user_account", `
		INSERT INTO user_accounts (id, user_id, institution_id, primary_type)
		VALUES ($1,$2,'ins-1630-breakdown','depository')`, userAccountID, userID)
	mustExec("linked_item", `
		INSERT INTO linked_items (id, user_id, item_id, access_token_encrypted)
		VALUES ($1,$2,$3,'enc-test-token')`, linkedItemID, userID, "1630-breakdown-item-"+linkedItemID)
	mustExec("account", `
		INSERT INTO accounts (id, linked_item_id, user_id, user_account_id, plaid_account_id, name, type, status)
		VALUES ($1,$2,$3,$4,$5,'1630 Checking','depository','active')`,
		accountID, linkedItemID, userID, userAccountID, "plaid-acc-1630-"+accountID)

	hoaID := uuid.New().String()
	otherID := uuid.New().String()
	mustExec("hoa custom category", `
		INSERT INTO custom_categories (id,user_id,name,parent_pfc_primary) VALUES ($1,$2,'HOA','RENT_AND_UTILITIES')`,
		hoaID, userID)
	mustExec("other custom category", `
		INSERT INTO custom_categories (id,user_id,name,parent_pfc_primary) VALUES ($1,$2,'Sprinkler Service','RENT_AND_UTILITIES')`,
		otherID, userID)
	// Deliberately NOT under RENT_AND_UTILITIES: proves the breakdown only picks up children
	// actually nested under the previewed primary.
	unrelatedID := uuid.New().String()
	mustExec("unrelated custom category", `
		INSERT INTO custom_categories (id,user_id,name,parent_pfc_primary) VALUES ($1,$2,'Streaming','ENTERTAINMENT')`,
		unrelatedID, userID)

	seedTxn := func(tag string, amount float64, primary, detailed, kind, customID *string) {
		mustExec("txn "+tag, `
			INSERT INTO transactions
			  (id,user_id,account_id,user_account_id,external_transaction_id,name,amount,date,pending,source,
			   pfc_primary,pfc_detailed,category_override_kind,custom_category_override_id)
			VALUES ($1,$2,$3,$4,$5,$6,$7,'2026-02-15',false,'plaid',$8,$9,$10,$11)`,
			uuid.New().String(), userID, accountID, userAccountID,
			"plaid-txn-1630-"+uuid.New().String(), "txn "+tag, amount,
			primary, detailed, kind, customID)
	}
	sp := func(s string) *string { return &s }

	// Reproduces the issue's own numbers: a $615.28/2-txn total, one native RENT_AND_UTILITIES
	// transaction ($192.08) plus one HOA custom-category transaction ($423.20).
	seedTxn("rent-direct", 192.08, sp("RENT_AND_UTILITIES"), sp("RENT_AND_UTILITIES_RENT"), nil, nil)
	seedTxn("hoa", 423.20, nil, nil, sp("custom"), &hoaID)
	seedTxn("sprinkler", 50.00, nil, nil, sp("custom"), &otherID)
	seedTxn("streaming", 15.00, nil, nil, sp("custom"), &unrelatedID)
	seedTxn("unrelated-primary", 999.00, sp("ENTERTAINMENT"), nil, nil, nil)

	startDate := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	endDate := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)

	t.Run("pfc primary discloses each nested custom category, and only those", func(t *testing.T) {
		pfcPrimary := "RENT_AND_UTILITIES"
		children, directCount, directAmount, err := repo.GetCategoryMatchBreakdown(
			ctx, userID, "pfc", &pfcPrimary, nil, nil, startDate, endDate,
		)
		require.NoError(t, err)

		assert.Equal(t, 1, directCount, "the native RENT_AND_UTILITIES txn is 'direct', not a child")
		assert.InDelta(t, 192.08, directAmount, 0.001)

		byName := map[string]struct {
			count  int
			amount float64
		}{}
		for _, c := range children {
			byName[c.CategoryName] = struct {
				count  int
				amount float64
			}{c.TransactionCount, c.TotalAmount}
			assert.NotEmpty(t, c.CategoryID)
		}
		require.Len(t, children, 2, "HOA and Sprinkler Service, and nothing from an unrelated primary")

		hoa, ok := byName["HOA"]
		require.True(t, ok)
		assert.Equal(t, 1, hoa.count)
		assert.InDelta(t, 423.20, hoa.amount, 0.001)

		sprinkler, ok := byName["Sprinkler Service"]
		require.True(t, ok)
		assert.Equal(t, 1, sprinkler.count)
		assert.InDelta(t, 50.00, sprinkler.amount, 0.001)

		_, sawStreaming := byName["Streaming"]
		assert.False(t, sawStreaming, "a custom category nested under a DIFFERENT primary must not leak in")
	})

	t.Run("a leaf pfc_detailed category has nothing to disclose", func(t *testing.T) {
		pfcPrimary := "RENT_AND_UTILITIES"
		pfcDetailed := "RENT_AND_UTILITIES_RENT"
		children, directCount, directAmount, err := repo.GetCategoryMatchBreakdown(
			ctx, userID, "pfc", &pfcPrimary, &pfcDetailed, nil, startDate, endDate,
		)
		require.NoError(t, err)
		assert.Empty(t, children)
		assert.Equal(t, 0, directCount)
		assert.Equal(t, 0.0, directAmount)
	})

	t.Run("a custom parent discloses its own custom children", func(t *testing.T) {
		parentID := uuid.New().String()
		childID := uuid.New().String()
		mustExec("parent custom category", `
			INSERT INTO custom_categories (id,user_id,name) VALUES ($1,$2,'Utilities Umbrella')`, parentID, userID)
		mustExec("child custom category", `
			INSERT INTO custom_categories (id,user_id,name,parent_id) VALUES ($1,$2,'Gas Bill',$3)`, childID, userID, parentID)

		seedTxn("parent-direct", 75.00, nil, nil, sp("custom"), &parentID)
		seedTxn("child", 30.00, nil, nil, sp("custom"), &childID)

		children, directCount, directAmount, err := repo.GetCategoryMatchBreakdown(
			ctx, userID, "custom", nil, nil, &parentID, startDate, endDate,
		)
		require.NoError(t, err)

		assert.Equal(t, 1, directCount, "a transaction filed under the parent itself is 'direct'")
		assert.InDelta(t, 75.00, directAmount, 0.001)

		require.Len(t, children, 1)
		assert.Equal(t, "Gas Bill", children[0].CategoryName)
		assert.Equal(t, childID, children[0].CategoryID)
		assert.Equal(t, 1, children[0].TransactionCount)
		assert.InDelta(t, 30.00, children[0].TotalAmount, 0.001)
	})
}
