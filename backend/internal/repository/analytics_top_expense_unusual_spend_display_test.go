package repository

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/pashagolub/pgxmock/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// GetTopExpense and GetUnusualSpend had no repository-level test at all (#1626 Phase 2, gap item
// 1): TestGetIncomeSourceBreakdown and TestGetCategoryBreakdownForMerchant
// (analytics_spending_repository_test.go) pin that a custom category's row scans its stored name
// and that a NULL display scans to "" rather than erroring, but nothing pinned either of these two
// reads doing the same. Both select category_display through sql.NullString for exactly the same
// reason: the column is denormalised onto transactions and refreshed ASYNCHRONOUSLY by
// effective_category_refresh, so NULL is a reachable production state, not a fixture artifact.

func TestGetTopExpense_ScansCategoryDisplay(t *testing.T) {
	ctx := context.Background()
	userID := "user-123"
	start := time.Date(2024, 3, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2024, 3, 31, 0, 0, 0, 0, time.UTC)
	txDate := time.Date(2024, 3, 15, 0, 0, 0, 0, time.UTC)

	t.Run("named custom category's stored display survives the scan", func(t *testing.T) {
		mock, repo := setupAnalyticsMockDB(t)
		defer mock.Close()

		rows := pgxmock.NewRows([]string{"id", "merchant_name", "amount", "category", "category_display", "date"}).
			AddRow("txn-1", "Blue Bottle", 42.50, "custom:9f1c2b3a-0000-4000-8000-000000000001", "Coffee runs", txDate)

		mock.ExpectQuery(`SELECT`).
			WithArgs(userID, start, end).
			WillReturnRows(rows)

		row, err := repo.GetTopExpense(ctx, userID, start, end)
		require.NoError(t, err)
		require.NotNil(t, row)
		assert.Equal(t, "Coffee runs", row.CategoryDisplay)
		require.NoError(t, mock.ExpectationsWereMet(), "GetTopExpense takes exactly 3 args")
	})

	// ⚠ RENAMED (#1626 Phase 2 pgxmock sweep): this subtest used to be named "NULL display scans to
	// empty string, not an error" and read as proof that GetTopExpense's sql.NullString scan
	// tolerates a NULL category_display. It is not: pgxmock scans a `nil` AddRow cell into a plain
	// Go `string` WITHOUT error (mutation-confirmed — reverting GetTopExpense's `sql.NullString` to
	// a plain `string` leaves this subtest green), so it cannot distinguish the current, correct
	// scan from a reverted, broken one. What it still shows is real: a NULL display cell does not
	// disturb the OTHER scanned fields (category, in particular). The genuine NULL-tolerance proof,
	// against real Postgres, is TestAnalyticsReads_NullCategoryDisplay_ToleratedBySqlNullStringReads's
	// "GetTopExpense" subtest (analytics_null_category_display_integration_test.go, integration tag).
	t.Run("NULL display cell does not disturb the rest of the row (pgxmock cannot prove the scan itself tolerates NULL)", func(t *testing.T) {
		mock, repo := setupAnalyticsMockDB(t)
		defer mock.Close()

		// The column is app-maintained asynchronously, so NULL is reachable for a fresh custom
		// category whose effective_category_refresh job has not run yet.
		rows := pgxmock.NewRows([]string{"id", "merchant_name", "amount", "category", "category_display", "date"}).
			AddRow("txn-2", "Unknown Merchant", 10.0, "custom:9f1c2b3a-0000-4000-8000-000000000002", nil, txDate)

		mock.ExpectQuery(`SELECT`).
			WithArgs(userID, start, end).
			WillReturnRows(rows)

		row, err := repo.GetTopExpense(ctx, userID, start, end)
		require.NoError(t, err)
		require.NotNil(t, row)
		assert.Empty(t, row.CategoryDisplay)
		assert.Equal(t, "custom:9f1c2b3a-0000-4000-8000-000000000002", row.Category)
	})

	t.Run("PFC row's raw enum is scanned verbatim, not resolved here", func(t *testing.T) {
		mock, repo := setupAnalyticsMockDB(t)
		defer mock.Close()

		// Display resolution (raw enum -> registry label) is the SERVICE's job
		// (resolveCategoryDisplay); the repository must hand the raw value through unchanged.
		rows := pgxmock.NewRows([]string{"id", "merchant_name", "amount", "category", "category_display", "date"}).
			AddRow("txn-3", "Amazon", 299.99, "GENERAL_MERCHANDISE", "GENERAL_MERCHANDISE", txDate)

		mock.ExpectQuery(`SELECT`).
			WithArgs(userID, start, end).
			WillReturnRows(rows)

		row, err := repo.GetTopExpense(ctx, userID, start, end)
		require.NoError(t, err)
		require.NotNil(t, row)
		assert.Equal(t, "GENERAL_MERCHANDISE", row.CategoryDisplay)
	})

	t.Run("returns nil when no expenses in period", func(t *testing.T) {
		mock, repo := setupAnalyticsMockDB(t)
		defer mock.Close()

		mock.ExpectQuery(`SELECT`).
			WithArgs(userID, start, end).
			WillReturnError(pgx.ErrNoRows)

		row, err := repo.GetTopExpense(ctx, userID, start, end)
		require.NoError(t, err)
		assert.Nil(t, row)
	})

	t.Run("db error is returned", func(t *testing.T) {
		mock, repo := setupAnalyticsMockDB(t)
		defer mock.Close()

		mock.ExpectQuery(`SELECT`).
			WithArgs(userID, start, end).
			WillReturnError(assert.AnError)

		_, err := repo.GetTopExpense(ctx, userID, start, end)
		assert.Error(t, err)
	})
}

func TestGetUnusualSpend_ScansCategoryDisplay(t *testing.T) {
	ctx := context.Background()
	userID := "user-123"
	baselineStart := time.Date(2025, 10, 1, 0, 0, 0, 0, time.UTC)
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2026, 1, 31, 0, 0, 0, 0, time.UTC)
	txDate := time.Date(2026, 1, 15, 0, 0, 0, 0, time.UTC)
	const sigmaThreshold = 2.0

	columns := []string{
		"id", "merchant_name", "amount", "category", "category_display",
		"date", "mean_amount", "sigma_above",
	}

	t.Run("named custom category's stored display survives the scan", func(t *testing.T) {
		mock, repo := setupAnalyticsMockDB(t)
		defer mock.Close()

		rows := pgxmock.NewRows(columns).
			AddRow("txn-1", "Therapist", 200.0, "custom:9f1c2b3a-0000-4000-8000-000000000001", "Therapy", txDate, 50.0, 3.0)

		mock.ExpectQuery(`SELECT`).
			WithArgs(userID, baselineStart, start, end, sigmaThreshold).
			WillReturnRows(rows)

		result, err := repo.GetUnusualSpend(ctx, userID, baselineStart, start, end, sigmaThreshold)
		require.NoError(t, err)
		require.Len(t, result, 1)
		assert.Equal(t, "Therapy", result[0].CategoryDisplay)
		require.NoError(t, mock.ExpectationsWereMet(), "GetUnusualSpend takes exactly 5 args")
	})

	// ⚠ RENAMED (#1626 Phase 2 pgxmock sweep): this subtest used to be named "NULL display scans to
	// empty string, not an error" and read as proof that GetUnusualSpend's sql.NullString scan
	// tolerates a NULL category_display. It is not: pgxmock scans a `nil` AddRow cell into a plain
	// Go `string` WITHOUT error (mutation-confirmed — reverting GetUnusualSpend's `sql.NullString`
	// to a plain `string` leaves this subtest green).
	//
	// ⚠ IT NOW HAS AN INTEGRATION-TEST REPLACEMENT, and the sentence claiming otherwise was
	// retracted by #1626 gap G6. A real Postgres proof does need a >=3-row same-category baseline
	// with a non-zero standard deviation plus a row far enough above the mean to clear
	// sigmaThreshold — that fixture was judged disproportionate and is in fact four rows:
	// TestGetUnusualSpend_NullCategoryDisplay_ToleratedAgainstRealPostgres
	// (analytics_null_category_display_integration_test.go) is where the scan is proven. What THIS
	// subtest still shows, honestly: a NULL display cell does not disturb the OTHER scanned fields
	// on the same row.
	t.Run("NULL display cell does not disturb the rest of the row (pgxmock cannot prove the scan itself tolerates NULL)", func(t *testing.T) {
		mock, repo := setupAnalyticsMockDB(t)
		defer mock.Close()

		rows := pgxmock.NewRows(columns).
			AddRow("txn-2", "Unknown", 100.0, "custom:9f1c2b3a-0000-4000-8000-000000000002", nil, txDate, 20.0, 2.5)

		mock.ExpectQuery(`SELECT`).
			WithArgs(userID, baselineStart, start, end, sigmaThreshold).
			WillReturnRows(rows)

		result, err := repo.GetUnusualSpend(ctx, userID, baselineStart, start, end, sigmaThreshold)
		require.NoError(t, err)
		require.Len(t, result, 1)
		assert.Empty(t, result[0].CategoryDisplay)
		assert.Equal(t, "custom:9f1c2b3a-0000-4000-8000-000000000002", result[0].Category)
	})

	t.Run("PFC row's raw enum is scanned verbatim, not resolved here", func(t *testing.T) {
		mock, repo := setupAnalyticsMockDB(t)
		defer mock.Close()

		rows := pgxmock.NewRows(columns).
			AddRow("txn-3", "Whole Foods", 300.0, "FOOD_AND_DRINK", "FOOD_AND_DRINK", txDate, 100.0, 4.0)

		mock.ExpectQuery(`SELECT`).
			WithArgs(userID, baselineStart, start, end, sigmaThreshold).
			WillReturnRows(rows)

		result, err := repo.GetUnusualSpend(ctx, userID, baselineStart, start, end, sigmaThreshold)
		require.NoError(t, err)
		require.Len(t, result, 1)
		assert.Equal(t, "FOOD_AND_DRINK", result[0].CategoryDisplay)
	})

	t.Run("returns empty slice when no data", func(t *testing.T) {
		mock, repo := setupAnalyticsMockDB(t)
		defer mock.Close()

		mock.ExpectQuery(`SELECT`).
			WithArgs(userID, baselineStart, start, end, sigmaThreshold).
			WillReturnRows(pgxmock.NewRows(columns))

		result, err := repo.GetUnusualSpend(ctx, userID, baselineStart, start, end, sigmaThreshold)
		require.NoError(t, err)
		assert.Empty(t, result)
	})

	t.Run("db error is returned", func(t *testing.T) {
		mock, repo := setupAnalyticsMockDB(t)
		defer mock.Close()

		mock.ExpectQuery(`SELECT`).
			WithArgs(userID, baselineStart, start, end, sigmaThreshold).
			WillReturnError(assert.AnError)

		_, err := repo.GetUnusualSpend(ctx, userID, baselineStart, start, end, sigmaThreshold)
		assert.Error(t, err)
	})
}
