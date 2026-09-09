//go:build integration

package repository

// ─── #1626 E2 Part 2: the four reads that scanned a nullable column into a plain Go string ───────
//
// `transactions.effective_category_name` is NULLABLE. Migration 000073 leaves it that way AND
// builds a `WHERE effective_category_name IS NOT NULL` partial index, so the schema EXPECTS NULLs.
// The column is also app-maintained ASYNCHRONOUSLY (queue/jobs/effective_category_refresh.go), so a
// freshly-renamed or freshly-created custom category legitimately has no name on its rows yet.
//
// Four reads selected it and scanned it into a plain `string`. pgx v5 does not yield "" for a NULL —
// it fails the scan outright, and MapError classifies that as a 500. The whole chart is lost, not
// one row. The four, with the field each one scanned into:
//
//	GetSpendingByCategory          -> domain.SpendingByCategory.PFCPrimaryDisplay
//	getSpendingByCategoryHousehold -> the same field
//	GetSpendingTrends              -> domain.SpendingTrendRow.CategoryDisplay
//	getSpendingTrendsHousehold     -> the same field
//
// DERIVATION, so the set can be re-derived rather than believed. `t.effective_category_name` is
// selected at TEN sites in analytics_repository.go. SIX already scan through `sql.NullString` and
// take `.String` — GetTopExpense, GetLargestTransactionsForPeriod, GetCategoryBreakdownForMerchant,
// GetIncomeSourceBreakdown and its household twin, GetUnusualSpend — and a seventh tolerant read,
// GetCategoryTotalForPeriod, resolves its display from a `cc.name` scalar subquery instead of this
// column. The remaining four are these. GetSpendingByMerchant and its twin never select the column
// (their name COALESCEs to the literal 'Unknown'), and GetMonthOverMonthSpending has no display
// column at all. An earlier review put the count at TWO; it is four.
//
// ⚠ THESE MUST BE INTEGRATION TESTS, AND THE BRIEF FOR THIS TASK EXPECTED pgxmock TO SUFFICE. It
// does not, and the difference is the whole falsifiability of the RED. A pgxmock row whose display
// cell is `nil` scans into a plain `string` WITHOUT ERROR — pgxmock assigns Go values through its
// own converter and never reaches pgx's NULL handling — so the four pgxmock tests written first
// PASSED against the unfixed code. Only a real Postgres NULL reaches the scan path that fails.
//
// ⚠ CONSEQUENCE FOR AN EXISTING TEST, reported rather than fixed here:
// TestGetCategoryTotalForPeriod_CustomCategory_NullDisplayScansToEmptyString
// (analytics_category_total_display_test.go) is a pgxmock test of exactly that shape, so it does
// not in fact demonstrate the `sql.NullString` scan it names — it would pass against a plain-string
// scan too. Its SIBLING assertions (the arg count, the emitted SQL shape) are unaffected.
//
// The repository's contract is only "do not error": it normalises NULL to "" and leaves the
// PLACEHOLDER decision to the service, which already routes every one of these rows through
// resolveCategoryDisplay — the single choke point
// TestNoAnalyticsReadResolvesAGroupingKeyThroughThePFCRegistry enforces. Nothing here weakens that
// guard: it scans the `services` package, and these four scans live in `repository`.

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
)

// TestAnalyticsReads_NullEffectiveCategoryName_DoNotError seeds ONE transaction whose grouping key
// is a `custom:` token and whose display column is NULL, then calls all four reads.
//
// A `custom:` key is used because it is the only key for which the column is reachably NULL: a PFC
// row's name is written at ingest, whereas a custom category's is filled in by the async refresh
// job, and `effectiveCategoryExpr` derives the key from category_override_kind /
// custom_category_override_id — never from the name. There is no CHECK constraint tying the name to
// the override kind (see profile_repository.go's erasure comment, which relies on the same fact), so
// the fixture state is schema-legal as well as production-reachable.
func TestAnalyticsReads_NullEffectiveCategoryName_DoNotError(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()

	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx) //nolint:errcheck

	repo := &analyticsRepository{db: tx}

	userID := uuid.New().String()
	userAccountID := uuid.New().String()
	linkedItemID := uuid.New().String()
	accountID := uuid.New().String()
	householdID := uuid.New().String()
	customCategoryID := uuid.New().String()

	mustExec := func(what, sql string, args ...any) {
		t.Helper()
		_, execErr := tx.Exec(ctx, sql, args...)
		require.NoError(t, execErr, "seed %s", what)
	}

	mustExec("user", `
		INSERT INTO users (id, clerk_user_id, email, subscription_tier, subscription_status, primary_region)
		VALUES ($1,$2,$3,'premium','active','iad')`,
		userID, "test|nulldisp-"+userID, "nulldisp-"+userID+"@example.com")
	mustExec("user_account", `
		INSERT INTO user_accounts (id, user_id, institution_id, primary_type)
		VALUES ($1,$2,'ins-nulldisp','depository')`, userAccountID, userID)
	mustExec("linked_item", `
		INSERT INTO linked_items (id, user_id, item_id, access_token_encrypted)
		VALUES ($1,$2,$3,'enc-test-token')`, linkedItemID, userID, "nulldisp-item-"+linkedItemID)
	mustExec("account", `
		INSERT INTO accounts (id, linked_item_id, user_id, user_account_id, plaid_account_id, name, type, status)
		VALUES ($1,$2,$3,$4,$5,'Null Display Checking','depository','active')`,
		accountID, linkedItemID, userID, userAccountID, "plaid-acc-nd-"+accountID)
	mustExec("custom_category", `
		INSERT INTO custom_categories (id, user_id, name, parent_pfc_primary)
		VALUES ($1,$2,'Renamed Mid-Refresh','FOOD_AND_DRINK')`, customCategoryID, userID)

	// The household half: the requesting user owns the household and their own user_account is
	// shared into it, which is all householdGrantsCTE and householdSharedTransactionPredicate need
	// (the predicate matches on accounts.user_account_id against an open grant covering the date).
	mustExec("household", `
		INSERT INTO households (id, owner_user_id, name) VALUES ($1,$2,'Null Display Household')`,
		householdID, userID)
	mustExec("share", `
		INSERT INTO household_account_shares (household_id, user_account_id, visibility, shared_at)
		VALUES ($1,$2,'shared',$3)`,
		householdID, userAccountID, time.Date(2026, 3, 1, 9, 41, 0, 0, time.UTC))

	// ⚠ effective_category_name IS DELIBERATELY OMITTED, so it is NULL. Spelling it as an explicit
	// NULL would read as a fixture quirk; omitting the column says "the writer never got here",
	// which is the production state (the refresh job runs after the rename commits).
	mustExec("transaction", `
		INSERT INTO transactions
		  (id, user_id, account_id, user_account_id, external_transaction_id, name, amount, date,
		   pending, source, category_override_kind, custom_category_override_id)
		VALUES ($1,$2,$3,$4,$5,'Null display txn',412.37,'2026-04-17',false,'plaid','custom',$6)`,
		uuid.New().String(), userID, accountID, userAccountID,
		"plaid-txn-nd-"+uuid.New().String(), customCategoryID)

	wantKey := "custom:" + customCategoryID
	personal := domain.SpendingFilters{
		StartDate: time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC),
		EndDate:   time.Date(2026, 4, 30, 0, 0, 0, 0, time.UTC),
	}
	household := personal
	household.HouseholdScope = &domain.HouseholdScope{
		HouseholdID:   householdID,
		MemberUserIDs: []string{userID},
	}

	t.Run("GetSpendingByCategory", func(t *testing.T) {
		rows, err := repo.GetSpendingByCategory(ctx, userID, personal)
		require.NoError(t, err, "a NULL effective_category_name must not 500 the spending report")
		require.Len(t, rows, 1)
		assert.Equal(t, wantKey, rows[0].Category)
		assert.Empty(t, rows[0].PFCPrimaryDisplay,
			"NULL normalises to \"\"; the placeholder is resolveCategoryDisplay's decision, not the repository's")
		assert.InDelta(t, 412.37, rows[0].Total, 0.001)
		assert.Equal(t, 1, rows[0].TransactionCount)
	})

	t.Run("getSpendingByCategoryHousehold", func(t *testing.T) {
		rows, err := repo.GetSpendingByCategory(ctx, userID, household)
		require.NoError(t, err, "the household twin has the same nullable column and the same 500")
		require.Len(t, rows, 1)
		assert.Equal(t, wantKey, rows[0].Category)
		assert.Empty(t, rows[0].PFCPrimaryDisplay)
		assert.InDelta(t, 412.37, rows[0].Total, 0.001)
	})

	t.Run("GetSpendingTrends", func(t *testing.T) {
		rows, err := repo.GetSpendingTrends(ctx, userID, personal)
		require.NoError(t, err, "a NULL effective_category_name must not 500 the trend chart")
		require.Len(t, rows, 1)
		assert.Equal(t, wantKey, rows[0].Category)
		assert.Empty(t, rows[0].CategoryDisplay)
		assert.InDelta(t, 412.37, rows[0].Total, 0.001)
	})

	t.Run("getSpendingTrendsHousehold", func(t *testing.T) {
		rows, err := repo.GetSpendingTrends(ctx, userID, household)
		require.NoError(t, err, "the household twin has the same nullable column and the same 500")
		require.Len(t, rows, 1)
		assert.Equal(t, wantKey, rows[0].Category)
		assert.Empty(t, rows[0].CategoryDisplay)
		assert.InDelta(t, 412.37, rows[0].Total, 0.001)
	})
}
