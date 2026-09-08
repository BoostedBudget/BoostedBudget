//go:build integration

package repository

// Issue #1626 — GetCategoryHierarchy, the read the Sankey's three-level view is built from.

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
)

func TestGetCategoryHierarchy_Integration(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()

	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx) //nolint:errcheck

	repo := &analyticsRepository{db: tx}

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
		VALUES ($1,$2,$3,'premium','active','iad')`, userID, "test|"+userID, "hier-1626-"+userID+"@example.com")
	mustExec("user_account", `
		INSERT INTO user_accounts (id, user_id, institution_id, primary_type)
		VALUES ($1,$2,'ins-hier-1626','depository')`, userAccountID, userID)
	mustExec("linked_item", `
		INSERT INTO linked_items (id, user_id, item_id, access_token_encrypted)
		VALUES ($1,$2,$3,'enc-test-token')`, linkedItemID, userID, "hier-item-"+linkedItemID)
	mustExec("account", `
		INSERT INTO accounts (id, linked_item_id, user_id, user_account_id, plaid_account_id, name, type, status)
		VALUES ($1,$2,$3,$4,$5,'Hier Checking','depository','active')`,
		accountID, linkedItemID, userID, userAccountID, "plaid-acc-h-"+accountID)

	eatingOutID := uuid.New().String()
	coffeeID := uuid.New().String()
	mustExec("eatingOut", `INSERT INTO custom_categories (id,user_id,name,parent_pfc_primary) VALUES ($1,$2,'Eating Out H','FOOD_AND_DRINK')`, eatingOutID, userID)
	mustExec("coffee", `INSERT INTO custom_categories (id,user_id,name,parent_id) VALUES ($1,$2,'Coffee Runs H',$3)`, coffeeID, userID, eatingOutID)

	seedTxn := func(tag string, amount float64, primary, detailed, kind, customID *string, effName *string) {
		mustExec("txn "+tag, `
			INSERT INTO transactions
			  (id,user_id,account_id,user_account_id,external_transaction_id,name,amount,date,pending,source,
			   pfc_primary,pfc_detailed,category_override_kind,custom_category_override_id,effective_category_name)
			VALUES ($1,$2,$3,$4,$5,$6,$7,'2026-02-15',false,'plaid',$8,$9,$10,$11,$12)`,
			uuid.New().String(), userID, accountID, userAccountID,
			"plaid-txn-h-"+uuid.New().String(), "txn "+tag, amount,
			primary, detailed, kind, customID, effName)
	}
	sp := func(s string) *string { return &s }

	// FOOD_AND_DRINK gets three kinds of descendant plus DIRECT spend of its own — the shape that
	// makes direct_total load-bearing.
	seedTxn("fd-direct", 40, sp("FOOD_AND_DRINK"), nil, nil, nil, sp("FOOD_AND_DRINK"))
	seedTxn("fd-fastfood", 60, sp("FOOD_AND_DRINK"), sp("FOOD_AND_DRINK_FAST_FOOD"), nil, nil, sp("FOOD_AND_DRINK"))
	seedTxn("eating-out-direct", 100, nil, nil, sp("custom"), &eatingOutID, sp("Eating Out H"))
	seedTxn("coffee", 25, nil, nil, sp("custom"), &coffeeID, sp("Coffee Runs H"))
	seedTxn("transport", 500, sp("TRANSPORTATION"), nil, nil, nil, sp("TRANSPORTATION"))
	// Income, to prove direction actually partitions the ledger.
	seedTxn("salary", -3000, sp("INCOME"), sp("INCOME_WAGES"), nil, nil, sp("INCOME"))

	filters := domain.SpendingFilters{
		StartDate: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		EndDate:   time.Date(2026, 3, 31, 0, 0, 0, 0, time.UTC),
	}

	t.Run("expense direction returns the whole ragged tree", func(t *testing.T) {
		rows, err := repo.GetCategoryHierarchy(ctx, userID, filters, domain.CategoryDirectionExpense)
		require.NoError(t, err)

		type key struct{ l1, l2, l3 string }
		byPath := map[key]domain.CategoryHierarchyRow{}
		var total float64
		for _, r := range rows {
			byPath[key{r.L1Key, r.L2Key, r.L3Key}] = r
			total += r.Total
		}

		assert.InDelta(t, 725.0, total, 0.001, "every expense row must appear exactly once")

		custom := func(id string) string { return "custom:" + id }

		fdDirect, ok := byPath[key{"FOOD_AND_DRINK", "", ""}]
		require.True(t, ok, "a primary's own direct spend must be its own leaf row, not folded into a child")
		assert.InDelta(t, 40.0, fdDirect.Total, 0.001)

		fastFood, ok := byPath[key{"FOOD_AND_DRINK", "FOOD_AND_DRINK_FAST_FOOD", ""}]
		require.True(t, ok)
		assert.InDelta(t, 60.0, fastFood.Total, 0.001)

		eatingOut, ok := byPath[key{"FOOD_AND_DRINK", custom(eatingOutID), ""}]
		require.True(t, ok, "a custom category nested under a PFC primary sits at L2 of that primary")
		assert.InDelta(t, 100.0, eatingOut.Total, 0.001)
		assert.Equal(t, "Eating Out H", eatingOut.L2Display)

		coffee, ok := byPath[key{"FOOD_AND_DRINK", custom(eatingOutID), custom(coffeeID)}]
		require.True(t, ok, "THE three-level chain must survive grouping")
		assert.InDelta(t, 25.0, coffee.Total, 0.001)
		assert.Equal(t, "Coffee Runs H", coffee.L3Display)

		_, hasIncome := byPath[key{"INCOME", "INCOME_WAGES", ""}]
		assert.False(t, hasIncome, "income must not leak into the expense direction")
	})

	t.Run("income direction is the complement", func(t *testing.T) {
		rows, err := repo.GetCategoryHierarchy(ctx, userID, filters, domain.CategoryDirectionIncome)
		require.NoError(t, err)
		require.Len(t, rows, 1)
		assert.Equal(t, "INCOME", rows[0].L1Key)
		assert.Equal(t, "INCOME_WAGES", rows[0].L2Key)
		assert.InDelta(t, 3000.0, rows[0].Total, 0.001,
			"income is amount < 0 and is reported as a positive magnitude")
	})

	t.Run("ordering is deterministic and total-first", func(t *testing.T) {
		rows, err := repo.GetCategoryHierarchy(ctx, userID, filters, domain.CategoryDirectionExpense)
		require.NoError(t, err)
		require.NotEmpty(t, rows)
		for i := 1; i < len(rows); i++ {
			assert.GreaterOrEqual(t, rows[i-1].Total, rows[i].Total,
				"rows must arrive in descending total order")
		}
		assert.InDelta(t, 500.0, rows[0].Total, 0.001, "the largest leaf comes first")
	})

	t.Run("household scope runs and is a strict subset", func(t *testing.T) {
		// Nothing is shared into a household here, so the household read must return no rows at
		// all. The point of the case is that the twin EXECUTES — its CTE, its join and its
		// placeholder numbering are different from the personal read's, and that is where a
		// personal/household pair has silently diverged before.
		hh := filters
		hh.HouseholdScope = &domain.HouseholdScope{
			HouseholdID:   uuid.New().String(),
			MemberUserIDs: []string{userID},
		}
		rows, err := repo.GetCategoryHierarchy(ctx, userID, hh, domain.CategoryDirectionExpense)
		require.NoError(t, err, "the household twin must be executable, not just compilable")
		assert.Empty(t, rows, "nothing is shared into this household")
	})

	t.Run("custom_category_ids narrows the tree to custom rows", func(t *testing.T) {
		scoped := filters
		scoped.CustomCategoryIDs = []string{eatingOutID}
		scoped.RollUpChildren = true

		rows, err := repo.GetCategoryHierarchy(ctx, userID, scoped, domain.CategoryDirectionExpense)
		require.NoError(t, err)

		var total float64
		for _, r := range rows {
			total += r.Total
			assert.Contains(t, fmt.Sprintf("%s|%s|%s", r.L1Key, r.L2Key, r.L3Key), "custom:",
				"scoping to a custom category must not return PFC-only branches")
		}
		assert.InDelta(t, 125.0, total, 0.001,
			"roll-up includes the parent's own spend plus its child's")
	})
}
