//go:build integration

package repository

// Issue #1630 Task B5 — GetCategoryMatchBreakdown, the query that discloses a REAL
// PARTITION of the rolled-up preview total: category-direct + nested-descendant +
// tag-only-outside-category, always summing to exactly what GetMatchingTransactionStats
// computes for the same inputs. pgxmock cannot validate the CASE/JOIN/GROUP BY shape this
// query relies on (it never catches aggregate/type errors), so this is an integration test
// against real Postgres rather than a pgxmock unit test.

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
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
	seedTaggedTxn := func(tag string, amount float64, primary, detailed, kind, customID *string, tagsJSON string) {
		mustExec("txn "+tag, `
			INSERT INTO transactions
			  (id,user_id,account_id,user_account_id,external_transaction_id,name,amount,date,pending,source,
			   pfc_primary,pfc_detailed,category_override_kind,custom_category_override_id,tags)
			VALUES ($1,$2,$3,$4,$5,$6,$7,'2026-02-15',false,'plaid',$8,$9,$10,$11,$12::jsonb)`,
			uuid.New().String(), userID, accountID, userAccountID,
			"plaid-txn-1630-"+uuid.New().String(), "txn "+tag, amount,
			primary, detailed, kind, customID, tagsJSON)
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
		children, directCount, directAmount, tagOnlyCount, tagOnlyAmount, err := repo.GetCategoryMatchBreakdown(
			ctx, userID, "pfc", &pfcPrimary, nil, nil, nil, domain.MatchLogicCategoryOnly, startDate, endDate,
		)
		require.NoError(t, err)

		assert.Equal(t, 1, directCount, "the native RENT_AND_UTILITIES txn is 'direct', not a child")
		assert.InDelta(t, 192.08, directAmount, 0.001)
		assert.Equal(t, 0, tagOnlyCount, "category_only never has a tag-only-outside-category bucket")
		assert.Equal(t, 0.0, tagOnlyAmount)

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

	t.Run("a leaf pfc_detailed category has nothing to disclose under category_only", func(t *testing.T) {
		pfcPrimary := "RENT_AND_UTILITIES"
		pfcDetailed := "RENT_AND_UTILITIES_RENT"
		children, directCount, directAmount, tagOnlyCount, tagOnlyAmount, err := repo.GetCategoryMatchBreakdown(
			ctx, userID, "pfc", &pfcPrimary, &pfcDetailed, nil, nil, domain.MatchLogicCategoryOnly, startDate, endDate,
		)
		require.NoError(t, err)
		assert.Empty(t, children)
		assert.Equal(t, 0, directCount)
		assert.Equal(t, 0.0, directAmount)
		assert.Equal(t, 0, tagOnlyCount)
		assert.Equal(t, 0.0, tagOnlyAmount)
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

		children, directCount, directAmount, tagOnlyCount, tagOnlyAmount, err := repo.GetCategoryMatchBreakdown(
			ctx, userID, "custom", nil, nil, &parentID, nil, domain.MatchLogicCategoryOnly, startDate, endDate,
		)
		require.NoError(t, err)

		assert.Equal(t, 1, directCount, "a transaction filed under the parent itself is 'direct'")
		assert.InDelta(t, 75.00, directAmount, 0.001)
		assert.Equal(t, 0, tagOnlyCount)
		assert.Equal(t, 0.0, tagOnlyAmount)

		require.Len(t, children, 1)
		assert.Equal(t, "Gas Bill", children[0].CategoryName)
		assert.Equal(t, childID, children[0].CategoryID)
		assert.Equal(t, 1, children[0].TransactionCount)
		assert.InDelta(t, 30.00, children[0].TotalAmount, 0.001)
	})

	t.Run("or logic: an unrelated tag match is disclosed as tag-only-outside-category, and the three buckets sum to the total", func(t *testing.T) {
		pfcPrimary := "FOOD_AND_DRINK"
		tags := []string{"Essential-B5-Or"}

		// category-direct: matches the pfc primary itself, no tag.
		seedTxn("food-direct", 40.00, sp("FOOD_AND_DRINK"), sp("FOOD_AND_DRINK_GROCERIES"), nil, nil)
		// nested-descendant: a custom category nested under the primary, no tag.
		foodChildID := uuid.New().String()
		mustExec("food child custom category", `
			INSERT INTO custom_categories (id,user_id,name,parent_pfc_primary) VALUES ($1,$2,'Coffee Club','FOOD_AND_DRINK')`,
			foodChildID, userID)
		seedTxn("food-child", 20.00, nil, nil, sp("custom"), &foodChildID)
		// tag-only-outside-category: a DIFFERENT primary entirely, matched only by the tag.
		seedTaggedTxn("or-tag-only", 15.00, sp("ENTERTAINMENT"), nil, nil, nil, `["Essential-B5-Or"]`)

		children, directCount, directAmount, tagOnlyCount, tagOnlyAmount, err := repo.GetCategoryMatchBreakdown(
			ctx, userID, "pfc", &pfcPrimary, nil, nil, tags, domain.MatchLogicOr, startDate, endDate,
		)
		require.NoError(t, err)

		assert.Equal(t, 1, directCount)
		assert.InDelta(t, 40.00, directAmount, 0.001)
		require.Len(t, children, 1)
		assert.Equal(t, "Coffee Club", children[0].CategoryName)
		assert.InDelta(t, 20.00, children[0].TotalAmount, 0.001)
		assert.Equal(t, 1, tagOnlyCount, "the ENTERTAINMENT txn matched only via the tag clause, never the category clause")
		assert.InDelta(t, 15.00, tagOnlyAmount, 0.001)

		total, totalAmount, statsErr := repo.GetMatchingTransactionStats(
			ctx, userID, "pfc", &pfcPrimary, nil, nil, tags, domain.MatchLogicOr, startDate, endDate,
		)
		require.NoError(t, statsErr)
		var childrenSum float64
		var childCount int
		for _, c := range children {
			childrenSum += c.TotalAmount
			childCount += c.TransactionCount
		}
		assert.Equal(t, directCount+childCount+tagOnlyCount, total, "the three buckets' counts must sum to the total count")
		assert.InDelta(t, directAmount+childrenSum+tagOnlyAmount, totalAmount, 0.001,
			"the three buckets must be a REAL PARTITION of total_amount")
	})

	t.Run("and logic: the breakdown is scoped to rows that also carry the tag, so it can never exceed the total", func(t *testing.T) {
		pfcPrimary := "FOOD_AND_DRINK"
		tags := []string{"Splurge-B5-And"}

		// Matches category AND tag: counted.
		seedTaggedTxn("and-match", 12.00, sp("FOOD_AND_DRINK"), sp("FOOD_AND_DRINK_RESTAURANT"), nil, nil, `["Splurge-B5-And"]`)
		// Matches category but NOT the tag: must be excluded from an 'and' breakdown, unlike
		// the pre-B5 tag-blind query which would have inflated direct/children with this row.
		seedTxn("and-category-only-no-tag", 999.00, sp("FOOD_AND_DRINK"), sp("FOOD_AND_DRINK_GROCERIES"), nil, nil)

		children, directCount, directAmount, tagOnlyCount, tagOnlyAmount, err := repo.GetCategoryMatchBreakdown(
			ctx, userID, "pfc", &pfcPrimary, nil, nil, tags, domain.MatchLogicAnd, startDate, endDate,
		)
		require.NoError(t, err)

		assert.Equal(t, 1, directCount, "only the tagged FOOD_AND_DRINK txn counts under 'and'")
		assert.InDelta(t, 12.00, directAmount, 0.001)
		assert.Empty(t, children)
		assert.Equal(t, 0, tagOnlyCount, "'and' requires the category clause on every row: bucket 3 is always empty")
		assert.Equal(t, 0.0, tagOnlyAmount)

		_, totalAmount, statsErr := repo.GetMatchingTransactionStats(
			ctx, userID, "pfc", &pfcPrimary, nil, nil, tags, domain.MatchLogicAnd, startDate, endDate,
		)
		require.NoError(t, statsErr)
		assert.InDelta(t, directAmount, totalAmount, 0.001, "with no children and no tag-only bucket, direct alone must equal the total")
	})

	t.Run("tag_only with no category_kind: the whole total lands in the tag-only-outside-category bucket", func(t *testing.T) {
		tags := []string{"Weekend-B5-TagOnly"}
		seedTaggedTxn("tag-only-no-category", 25.00, sp("TRANSPORTATION"), nil, nil, nil, `["Weekend-B5-TagOnly"]`)

		children, directCount, directAmount, tagOnlyCount, tagOnlyAmount, err := repo.GetCategoryMatchBreakdown(
			ctx, userID, "", nil, nil, nil, tags, domain.MatchLogicTagOnly, startDate, endDate,
		)
		require.NoError(t, err)

		assert.Empty(t, children)
		assert.Equal(t, 0, directCount)
		assert.Equal(t, 0.0, directAmount)
		assert.Equal(t, 1, tagOnlyCount)
		assert.InDelta(t, 25.00, tagOnlyAmount, 0.001)
	})

	t.Run("a leaf pfc_detailed category still discloses a tag-only-outside-category bucket under or", func(t *testing.T) {
		// A primary/detailed pair not touched by any other subtest in this test function, so
		// this can't pick up an unrelated row seeded elsewhere (they all share one date window
		// and one user).
		pfcPrimary := "GENERAL_MERCHANDISE"
		pfcDetailed := "GENERAL_MERCHANDISE_PET_SUPPLIES"
		tags := []string{"Splurge2-B5-Leaf"}

		seedTxn("leaf-direct", 10.00, sp("GENERAL_MERCHANDISE"), sp("GENERAL_MERCHANDISE_PET_SUPPLIES"), nil, nil)
		seedTaggedTxn("leaf-tag-only", 5.00, sp("ENTERTAINMENT"), nil, nil, nil, `["Splurge2-B5-Leaf"]`)

		children, directCount, directAmount, tagOnlyCount, tagOnlyAmount, err := repo.GetCategoryMatchBreakdown(
			ctx, userID, "pfc", &pfcPrimary, &pfcDetailed, nil, tags, domain.MatchLogicOr, startDate, endDate,
		)
		require.NoError(t, err)

		assert.Empty(t, children, "a leaf category has no nested-descendant concept")
		assert.Equal(t, 1, directCount)
		assert.InDelta(t, 10.00, directAmount, 0.001)
		assert.Equal(t, 1, tagOnlyCount)
		assert.InDelta(t, 5.00, tagOnlyAmount, 0.001)
	})
}
