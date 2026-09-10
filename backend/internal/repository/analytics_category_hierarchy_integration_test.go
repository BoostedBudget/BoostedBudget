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

	t.Run("household scope returns the household's shared tree", func(t *testing.T) {
		// ⚠ THIS CASE USED TO ASSERT ONLY THAT THE SQL EXECUTES (#1626 gap G15). Its fixture shared
		// nothing into any household, so the read returned zero rows and every claim about the
		// household twin's CTE, its inner JOIN, its grant predicate and its placeholder numbering
		// rested on "it did not error". A twin that dropped its grant predicate — or one that never
		// admitted another member's rows at all — passed it unchanged.
		//
		// The fixture now has a second member with their OWN user_account, their own custom
		// category, and one account that is deliberately NOT shared, so the read has to include,
		// exclude and label rows rather than merely run.
		householdID := uuid.New().String()
		memberID := uuid.New().String()
		memberUserAccountID := uuid.New().String()
		memberPrivateUserAccountID := uuid.New().String()
		memberLinkedItemID := uuid.New().String()
		memberAccountID := uuid.New().String()
		memberPrivateAccountID := uuid.New().String()
		memberCatID := uuid.New().String()

		mustExec("household", `
			INSERT INTO households (id, owner_user_id, name) VALUES ($1,$2,'Hier Household')`,
			householdID, userID)
		mustExec("member user", `
			INSERT INTO users (id, clerk_user_id, email, subscription_tier, subscription_status, primary_region)
			VALUES ($1,$2,$3,'premium','active','iad')`,
			memberID, "test|hier-member-"+memberID, "hier-member-"+memberID+"@example.com")
		mustExec("member user_account", `
			INSERT INTO user_accounts (id, user_id, institution_id, primary_type)
			VALUES ($1,$2,'ins-hier-member','depository')`, memberUserAccountID, memberID)
		mustExec("member private user_account", `
			INSERT INTO user_accounts (id, user_id, institution_id, primary_type)
			VALUES ($1,$2,'ins-hier-member-private','depository')`, memberPrivateUserAccountID, memberID)
		mustExec("member linked_item", `
			INSERT INTO linked_items (id, user_id, item_id, access_token_encrypted)
			VALUES ($1,$2,$3,'enc-test-token')`, memberLinkedItemID, memberID, "hier-member-item-"+memberLinkedItemID)
		mustExec("member account", `
			INSERT INTO accounts (id, linked_item_id, user_id, user_account_id, plaid_account_id, name, type, status)
			VALUES ($1,$2,$3,$4,$5,'Member Shared','depository','active')`,
			memberAccountID, memberLinkedItemID, memberID, memberUserAccountID, "plaid-acc-hm-"+memberAccountID)
		mustExec("member private account", `
			INSERT INTO accounts (id, linked_item_id, user_id, user_account_id, plaid_account_id, name, type, status)
			VALUES ($1,$2,$3,$4,$5,'Member Private','depository','active')`,
			memberPrivateAccountID, memberLinkedItemID, memberID, memberPrivateUserAccountID, "plaid-acc-hmp-"+memberPrivateAccountID)

		// Both the requesting user's account and the member's SHARED one are granted, from well
		// before the transactions' date. The member's private user_account gets no grant at all.
		grantedAt := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
		mustExec("share (requesting user)", `
			INSERT INTO household_account_shares (household_id, user_account_id, visibility, shared_at)
			VALUES ($1,$2,'shared',$3)`, householdID, userAccountID, grantedAt)
		mustExec("share (member)", `
			INSERT INTO household_account_shares (household_id, user_account_id, visibility, shared_at)
			VALUES ($1,$2,'shared',$3)`, householdID, memberUserAccountID, grantedAt)

		// The member's own custom category. categoryPathExprs joins on cc.user_id = t.user_id — the
		// TRANSACTION's owner, not the requester — so this label is only resolvable if that is
		// honoured; a join written against the requesting user would emit a NULL display here.
		mustExec("member custom category", `
			INSERT INTO custom_categories (id,user_id,name,parent_pfc_primary)
			VALUES ($1,$2,'Date Nights M','FOOD_AND_DRINK')`, memberCatID, memberID)

		seedMemberTxn := func(tag string, accountID, memberUserAcct string, amount float64, primary, kind, customID *string) {
			mustExec("member txn "+tag, `
				INSERT INTO transactions
				  (id,user_id,account_id,user_account_id,external_transaction_id,name,amount,date,pending,source,
				   pfc_primary,category_override_kind,custom_category_override_id)
				VALUES ($1,$2,$3,$4,$5,$6,$7,'2026-02-16',false,'plaid',$8,$9,$10)`,
				uuid.New().String(), memberID, accountID, memberUserAcct,
				"plaid-txn-hm-"+uuid.New().String(), "member txn "+tag, amount,
				primary, kind, customID)
		}
		seedMemberTxn("fd-direct", memberAccountID, memberUserAccountID, 70, sp("FOOD_AND_DRINK"), nil, nil)
		seedMemberTxn("date-nights", memberAccountID, memberUserAccountID, 15, nil, sp("custom"), &memberCatID)
		// On the UNSHARED account. A read that admitted "any member's rows" instead of "rows on a
		// granted account" would fold this into the tree, and 999 is large enough that it would also
		// take first place in the total-descending order.
		seedMemberTxn("private", memberPrivateAccountID, memberPrivateUserAccountID, 999, sp("TRAVEL"), nil, nil)

		hh := filters
		hh.HouseholdScope = &domain.HouseholdScope{
			HouseholdID:   householdID,
			MemberUserIDs: []string{userID, memberID},
		}
		rows, err := repo.GetCategoryHierarchy(ctx, userID, hh, domain.CategoryDirectionExpense)
		require.NoError(t, err, "the household twin must be executable, not just compilable")
		require.NotEmpty(t, rows, "the household read must return the shared tree, not nothing")

		type key struct{ l1, l2, l3 string }
		byPath := map[key]domain.CategoryHierarchyRow{}
		var total float64
		var fdRollUp float64
		var fdCount int
		for _, r := range rows {
			byPath[key{r.L1Key, r.L2Key, r.L3Key}] = r
			total += r.Total
			if r.L1Key == "FOOD_AND_DRINK" {
				fdRollUp += r.Total
				fdCount += r.TransactionCount
			}
		}

		// 725 is the personal expense total asserted above; the household adds the member's two
		// shared rows and nothing else.
		assert.InDelta(t, 725.0+70.0+15.0, total, 0.001,
			"the household tree is the requesting user's rows plus the member's SHARED rows")
		assert.InDelta(t, 40.0+60.0+100.0+25.0+70.0+15.0, fdRollUp, 0.001,
			"both members' FOOD_AND_DRINK spend must roll up to the one L1 node")
		assert.Equal(t, 6, fdCount, "and the node must count both members' transactions")

		memberLeaf, ok := byPath[key{"FOOD_AND_DRINK", "custom:" + memberCatID, ""}]
		require.True(t, ok,
			"a member's custom category must appear at L2 of the PFC primary it is nested under")
		assert.InDelta(t, 15.0, memberLeaf.Total, 0.001)
		assert.Equal(t, "Date Nights M", memberLeaf.L2Display,
			"the label comes from the TRANSACTION owner's custom_categories row (cc.user_id = t.user_id)")

		_, sawPrivate := byPath[key{"TRAVEL", "", ""}]
		assert.False(t, sawPrivate,
			"a member's account with no household grant must not enter the household tree")

		// And the personal read over the same window must still exclude the member entirely: the
		// two scopes are different questions, and the household total above is only meaningful if
		// the personal one did not already contain those rows.
		personalRows, err := repo.GetCategoryHierarchy(ctx, userID, filters, domain.CategoryDirectionExpense)
		require.NoError(t, err)
		var personalTotal float64
		for _, r := range personalRows {
			personalTotal += r.Total
			assert.NotEqual(t, "custom:"+memberCatID, r.L2Key,
				"the member's category must not appear in the requesting user's personal tree")
		}
		assert.InDelta(t, 725.0, personalTotal, 0.001,
			"personal scope is unchanged by the household fixture")
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
