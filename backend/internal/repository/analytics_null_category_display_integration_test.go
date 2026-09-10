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
// ⚠ CONSEQUENCE FOR AN EXISTING TEST, since fixed by RENAME rather than by integration coverage
// (#1626 Phase 2 pgxmock sweep — see TestAnalyticsReads_NullCategoryDisplay_ToleratedBySqlNullStringReads
// below for the real proof instead): what is now
// TestGetCategoryTotalForPeriod_CustomCategory_BindsFiveArgsWhenDisplayIsNull
// (analytics_category_total_display_test.go) was a pgxmock test of exactly this shape named as if
// it demonstrated the `sql.NullString` scan — it would have passed against a plain-string scan
// too. Its SIBLING assertion (the five-arg bind, enforced by pgxmock's WithArgs) is real and is
// what the new name credits.
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

// ─── #1626 Phase 2 pgxmock sweep: the SIX "already tolerant" reads had vacuous unit tests too ─────
//
// The doc comment above says SIX sites already scan through `sql.NullString` (GetTopExpense,
// GetLargestTransactionsForPeriod, GetCategoryBreakdownForMerchant, GetIncomeSourceBreakdown and
// its household twin, GetUnusualSpend) plus a seventh tolerant read, GetCategoryTotalForPeriod,
// which resolves display from a `cc.name` scalar subquery instead. That is true of the PRODUCTION
// code — but the pgxmock unit tests written to PROVE it are just as vacuous as the four this file
// already fixes: pgxmock scans a `nil` cell into a plain Go `string` without error (see the doc
// comment above), so a test asserting "NULL scans without error" cannot tell a correct
// sql.NullString scan from a reverted, unfixed plain-string one. Confirmed by mutation: reverting
// GetCategoryTotalForPeriod, GetTopExpense, GetUnusualSpend or GetIncomeSourceBreakdown's
// `sql.NullString` to a plain `string` left their pgxmock tests green.
//
// This test closes that gap for three of them (GetCategoryTotalForPeriod's two token forms,
// GetTopExpense, GetIncomeSourceBreakdown). GetUnusualSpend needs a >=3-row same-category baseline
// with a non-zero standard deviation before its NULL-display row is even reachable; that was
// judged disproportionate and its pgxmock test renamed instead, and #1626 gap G6 retracted the
// judgement — the baseline is three rows, and it is now proven against real Postgres by
// TestGetUnusualSpend_NullCategoryDisplay_ToleratedAgainstRealPostgres at the foot of this file.
func TestAnalyticsReads_NullCategoryDisplay_ToleratedBySqlNullStringReads(t *testing.T) {
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
	customCategoryID := uuid.New().String()
	otherUserID := uuid.New().String()
	otherUsersCustomCategoryID := uuid.New().String() // owned by otherUserID, referenced by userID's txn

	mustExec := func(what, sql string, args ...any) {
		t.Helper()
		_, execErr := tx.Exec(ctx, sql, args...)
		require.NoError(t, execErr, "seed %s", what)
	}

	mustExec("user", `
		INSERT INTO users (id, clerk_user_id, email, subscription_tier, subscription_status, primary_region)
		VALUES ($1,$2,$3,'premium','active','iad')`,
		userID, "test|nulltolerant-"+userID, "nulltolerant-"+userID+"@example.com")
	mustExec("user_account", `
		INSERT INTO user_accounts (id, user_id, institution_id, primary_type)
		VALUES ($1,$2,'ins-nulltolerant','depository')`, userAccountID, userID)
	mustExec("linked_item", `
		INSERT INTO linked_items (id, user_id, item_id, access_token_encrypted)
		VALUES ($1,$2,$3,'enc-test-token')`, linkedItemID, userID, "nulltolerant-item-"+linkedItemID)
	mustExec("account", `
		INSERT INTO accounts (id, linked_item_id, user_id, user_account_id, plaid_account_id, name, type, status)
		VALUES ($1,$2,$3,$4,$5,'Null Tolerant Checking','depository','active')`,
		accountID, linkedItemID, userID, userAccountID, "plaid-acc-nt-"+accountID)
	// Real custom_categories rows: fk_transactions_custom_category_override is ON DELETE RESTRICT
	// and custom categories are only ever soft-deleted (#1569), so no transaction can reference an
	// id with no backing row — a "gone category" state is not constructible via a real transaction,
	// and custom_categories.name is NOT NULL, so cc.name is never itself NULL for a row that exists.
	mustExec("custom_category", `
		INSERT INTO custom_categories (id, user_id, name, parent_pfc_primary)
		VALUES ($1,$2,'Not Yet Refreshed','FOOD_AND_DRINK')`, customCategoryID, userID)
	// The genuinely-reachable NULL-display path for the custom-token branch is a user_id mismatch:
	// categoryTotalQuery's scalar subquery filters `cc.user_id = $1` (the QUERYING user), and the FK
	// on custom_category_override_id only requires the id to exist — it does not require the
	// referenced category to belong to the same user as the transaction. A row like this should
	// never occur through the service layer, but the repository must still not 500 if one does; it
	// is exactly the kind of defensive-scan case sql.NullString exists for.
	mustExec("other user", `
		INSERT INTO users (id, clerk_user_id, email, subscription_tier, subscription_status, primary_region)
		VALUES ($1,$2,$3,'premium','active','iad')`,
		otherUserID, "test|nulltolerant-other-"+otherUserID, "nulltolerant-other-"+otherUserID+"@example.com")
	mustExec("other user's custom_category", `
		INSERT INTO custom_categories (id, user_id, name, parent_pfc_primary)
		VALUES ($1,$2,'Belongs To Someone Else','GENERAL_MERCHANDISE')`, otherUsersCustomCategoryID, otherUserID)

	start := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2026, 5, 31, 0, 0, 0, 0, time.UTC)

	// ⚠ effective_category_name IS DELIBERATELY OMITTED on both rows below, exactly like the RED
	// fixture above: it is nullable and refreshed asynchronously, so a fresh custom-categorised
	// transaction legitimately has no name yet, independent of whether the category itself exists.
	mustExec("expense txn (custom category, name not yet refreshed)", `
		INSERT INTO transactions
		  (id, user_id, account_id, user_account_id, external_transaction_id, name, amount, date,
		   pending, source, category_override_kind, custom_category_override_id)
		VALUES ($1,$2,$3,$4,$5,'Not-yet-refreshed expense',257.14,'2026-05-12',false,'plaid','custom',$6)`,
		uuid.New().String(), userID, accountID, userAccountID,
		"plaid-txn-nt-expense-"+uuid.New().String(), customCategoryID)

	// Income row: same category, negative amount, so GetIncomeSourceBreakdown's `t.amount < 0`
	// predicate picks it up. Reuses the same category id — the point under test is the scan, not a
	// second fixture shape.
	mustExec("income txn (custom category, name not yet refreshed)", `
		INSERT INTO transactions
		  (id, user_id, account_id, user_account_id, external_transaction_id, name, amount, date,
		   pending, source, category_override_kind, custom_category_override_id)
		VALUES ($1,$2,$3,$4,$5,'Not-yet-refreshed income',-88.50,'2026-05-14',false,'plaid','custom',$6)`,
		uuid.New().String(), userID, accountID, userAccountID,
		"plaid-txn-nt-income-"+uuid.New().String(), customCategoryID)

	// A transaction whose custom_category_override_id points at ANOTHER user's category row. The
	// FK only checks the id exists; it does not check the owning user matches.
	mustExec("expense txn (custom category owned by another user)", `
		INSERT INTO transactions
		  (id, user_id, account_id, user_account_id, external_transaction_id, name, amount, date,
		   pending, source, category_override_kind, custom_category_override_id)
		VALUES ($1,$2,$3,$4,$5,'Cross-user category expense',63.00,'2026-05-20',false,'plaid','custom',$6)`,
		uuid.New().String(), userID, accountID, userAccountID,
		"plaid-txn-nt-crossuser-"+uuid.New().String(), otherUsersCustomCategoryID)

	wantKey := "custom:" + customCategoryID

	t.Run("GetCategoryTotalForPeriod custom token, category owned by another user", func(t *testing.T) {
		row, err := repo.GetCategoryTotalForPeriod(ctx, userID, "custom:"+otherUsersCustomCategoryID, start, end)
		require.NoError(t, err, "a NULL cc.name subquery result must not fail the scan")
		require.NotNil(t, row)
		assert.InDelta(t, 63.00, row.Total, 0.001)
		assert.Equal(t, 1, row.TransactionCount)
		assert.Empty(t, row.CategoryDisplay,
			"cc.user_id != the querying user means the scalar subquery finds no row, scanned "+
				"through sql.NullString rather than erroring")
	})

	t.Run("GetCategoryTotalForPeriod PFC token, display NULL by construction", func(t *testing.T) {
		row, err := repo.GetCategoryTotalForPeriod(ctx, userID, "FOOD_AND_DRINK", start, end)
		require.NoError(t, err, "a PFC token's category_display is NULL::text by construction")
		require.NotNil(t, row)
		assert.Equal(t, 0, row.TransactionCount, "no PFC-categorised transaction was seeded")
		assert.Empty(t, row.CategoryDisplay)
	})

	t.Run("GetTopExpense", func(t *testing.T) {
		row, err := repo.GetTopExpense(ctx, userID, start, end)
		require.NoError(t, err, "a NULL effective_category_name must not fail the scan")
		require.NotNil(t, row)
		assert.Equal(t, wantKey, row.Category)
		assert.Empty(t, row.CategoryDisplay,
			"NULL normalises to \"\" through sql.NullString, proven against real Postgres")
		assert.InDelta(t, 257.14, row.Amount, 0.001)
	})

	t.Run("GetIncomeSourceBreakdown", func(t *testing.T) {
		filters := domain.SpendingFilters{StartDate: start, EndDate: end}
		rows, err := repo.GetIncomeSourceBreakdown(ctx, userID, filters)
		require.NoError(t, err, "a NULL effective_category_name must not fail the income scan")
		require.Len(t, rows, 1)
		assert.Equal(t, wantKey, rows[0].Category)
		assert.Empty(t, rows[0].CategoryDisplay)
		assert.InDelta(t, 88.50, rows[0].Total, 0.001)
	})
}

// ─── #1626 gap G6: GetUnusualSpend's NULL scan, proven instead of asserted ────────────────────────
//
// The sweep above left GetUnusualSpend as the one `sql.NullString` read with no real-NULL proof, on
// the ground that its baseline was disproportionate to build. That judgement is retracted: the
// baseline is three rows in one category, and the read was the LAST one whose NULL tolerance rested
// on a pgxmock test that cannot fail (pgxmock scans a `nil` cell into a plain Go `string` without
// error, so the mock test passes against a reverted plain-string scan — confirmed by mutation).
//
// WHAT THE FIXTURE HAS TO SATISFY before the NULL cell is even reachable, all of it read off the
// query rather than guessed: the baseline CTE needs `COUNT(*) >= 3` rows of ONE effective category
// inside [baselineStart, startDate), all `amount > 0`, settled, and non-TRANSFER; their
// `STDDEV_POP` must be non-zero (NULLIF(...,0) plus `b.stddev_amount IS NOT NULL` drop a
// zero-variance category); and the reported row must be in [startDate, endDate] with
// (amount − mean) / stddev above the threshold.
//
// The category is a `custom:` token for the same reason as every case above: a custom row's
// effective_category_name is filled in by the async refresh job, so its absence is the production
// state rather than a fixture contrivance.
func TestGetUnusualSpend_NullCategoryDisplay_ToleratedAgainstRealPostgres(t *testing.T) {
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
	customCategoryID := uuid.New().String()

	mustExec := func(what, sql string, args ...any) {
		t.Helper()
		_, execErr := tx.Exec(ctx, sql, args...)
		require.NoError(t, execErr, "seed %s", what)
	}

	mustExec("user", `
		INSERT INTO users (id, clerk_user_id, email, subscription_tier, subscription_status, primary_region)
		VALUES ($1,$2,$3,'premium','active','iad')`,
		userID, "test|unusual-"+userID, "unusual-"+userID+"@example.com")
	mustExec("user_account", `
		INSERT INTO user_accounts (id, user_id, institution_id, primary_type)
		VALUES ($1,$2,'ins-unusual','depository')`, userAccountID, userID)
	mustExec("linked_item", `
		INSERT INTO linked_items (id, user_id, item_id, access_token_encrypted)
		VALUES ($1,$2,$3,'enc-test-token')`, linkedItemID, userID, "unusual-item-"+linkedItemID)
	mustExec("account", `
		INSERT INTO accounts (id, linked_item_id, user_id, user_account_id, plaid_account_id, name, type, status)
		VALUES ($1,$2,$3,$4,$5,'Unusual Checking','depository','active')`,
		accountID, linkedItemID, userID, userAccountID, "plaid-acc-us-"+accountID)
	mustExec("custom_category", `
		INSERT INTO custom_categories (id, user_id, name, parent_pfc_primary)
		VALUES ($1,$2,'Not Yet Refreshed Unusual','FOOD_AND_DRINK')`, customCategoryID, userID)

	// ⚠ effective_category_name IS DELIBERATELY OMITTED on every row, so the scanned cell is a real
	// Postgres NULL on the reported row.
	seed := func(tag string, amount float64, date string) {
		mustExec("txn "+tag, `
			INSERT INTO transactions
			  (id, user_id, account_id, user_account_id, external_transaction_id, name, amount, date,
			   pending, source, category_override_kind, custom_category_override_id)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8::date,false,'plaid','custom',$9)`,
			uuid.New().String(), userID, accountID, userAccountID,
			"plaid-txn-us-"+uuid.New().String(), "unusual "+tag, amount, date, customCategoryID)
	}
	// Baseline: three rows, mean 20, STDDEV_POP sqrt(200/3) ≈ 8.1650 — deliberately not zero, or
	// NULLIF would drop the whole category.
	seed("baseline-1", 10, "2026-07-05")
	seed("baseline-2", 20, "2026-07-10")
	seed("baseline-3", 30, "2026-07-15")
	// The outlier, inside the reporting window: (100 − 20) / 8.1650 ≈ 9.80 sigma.
	seed("outlier", 100, "2026-08-10")

	// The fixture's own premise, asserted rather than assumed: nothing (no trigger, no generated
	// column) has filled the display column in. Without this the test could pass on a non-NULL cell
	// and prove nothing about the scan.
	var nullDisplays int
	require.NoError(t, tx.QueryRow(ctx,
		`SELECT COUNT(*) FROM transactions WHERE user_id = $1 AND effective_category_name IS NULL`,
		userID).Scan(&nullDisplays))
	require.Equal(t, 4, nullDisplays, "every seeded row's display column must really be NULL")

	baselineStart := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	start := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2026, 8, 31, 0, 0, 0, 0, time.UTC)

	rows, err := repo.GetUnusualSpend(ctx, userID, baselineStart, start, end, 2.0)
	require.NoError(t, err,
		"a real NULL effective_category_name must not fail the scan — this is the assertion the "+
			"pgxmock test could not make, because pgxmock never reaches pgx's NULL handling")
	require.Len(t, rows, 1, "only the outlier is above 2 sigma")
	assert.Equal(t, "custom:"+customCategoryID, rows[0].Category)
	assert.Empty(t, rows[0].CategoryDisplay,
		"NULL normalises to \"\" through sql.NullString; the placeholder is the service's decision")
	assert.InDelta(t, 100.0, rows[0].Amount, 0.001)
	assert.InDelta(t, 20.0, rows[0].BaselineAvg, 0.001, "the baseline mean is the three baseline rows'")
	assert.InDelta(t, 9.798, rows[0].SigmaAbove, 0.01,
		"(100 - 20) / STDDEV_POP(10,20,30) — pinned numerically so a baseline built from the wrong "+
			"window would not merely shift the count")
}
