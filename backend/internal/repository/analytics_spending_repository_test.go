package repository

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/pashagolub/pgxmock/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupAnalyticsMockDB(t *testing.T) (pgxmock.PgxPoolIface, AnalyticsRepository) {
	t.Helper()
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	return mock, NewAnalyticsRepository(mock)
}

func TestGetSpendingByCategory(t *testing.T) {
	ctx := context.Background()
	userID := "user-123"
	filters := domain.SpendingFilters{
		StartDate: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
		EndDate:   time.Date(2024, 3, 31, 0, 0, 0, 0, time.UTC),
	}

	t.Run("returns category spending rows", func(t *testing.T) {
		mock, repo := setupAnalyticsMockDB(t)
		defer mock.Close()

		rows := pgxmock.NewRows([]string{"category", "category_display", "total", "transaction_count"}).
			AddRow("FOOD_AND_DRINK", "", 250.0, 15).
			AddRow("TRANSPORTATION", "", 120.0, 8)

		mock.ExpectQuery(`SELECT`).
			WithArgs(userID, filters.StartDate, filters.EndDate).
			WillReturnRows(rows)

		result, err := repo.GetSpendingByCategory(ctx, userID, filters)
		require.NoError(t, err)
		assert.Len(t, result, 2)
		assert.Equal(t, "FOOD_AND_DRINK", result[0].Category)
		assert.Equal(t, 250.0, result[0].Total)
		assert.Equal(t, 15, result[0].TransactionCount)
		assert.Equal(t, "TRANSPORTATION", result[1].Category)
	})

	t.Run("filters by primary category for subcategory drill-down", func(t *testing.T) {
		mock, repo := setupAnalyticsMockDB(t)
		defer mock.Close()

		subFilters := domain.SpendingFilters{
			StartDate: filters.StartDate,
			EndDate:   filters.EndDate,
			Category:  "FOOD_AND_DRINK",
		}

		rows := pgxmock.NewRows([]string{"category", "category_display", "total", "transaction_count"}).
			AddRow("RESTAURANTS", "", 180.0, 12).
			AddRow("GROCERIES", "", 70.0, 3)

		mock.ExpectQuery(`SELECT`).
			WithArgs(userID, subFilters.StartDate, subFilters.EndDate, subFilters.Category).
			WillReturnRows(rows)

		result, err := repo.GetSpendingByCategory(ctx, userID, subFilters)
		require.NoError(t, err)
		assert.Len(t, result, 2)
		assert.Equal(t, "RESTAURANTS", result[0].Category)
	})

	t.Run("returns empty slice when no data", func(t *testing.T) {
		mock, repo := setupAnalyticsMockDB(t)
		defer mock.Close()

		rows := pgxmock.NewRows([]string{"category", "category_display", "total", "transaction_count"})
		mock.ExpectQuery(`SELECT`).
			WithArgs(userID, filters.StartDate, filters.EndDate).
			WillReturnRows(rows)

		result, err := repo.GetSpendingByCategory(ctx, userID, filters)
		require.NoError(t, err)
		assert.Empty(t, result)
	})

	t.Run("db error is returned", func(t *testing.T) {
		mock, repo := setupAnalyticsMockDB(t)
		defer mock.Close()

		mock.ExpectQuery(`SELECT`).
			WithArgs(userID, filters.StartDate, filters.EndDate).
			WillReturnError(pgx.ErrNoRows)

		_, err := repo.GetSpendingByCategory(ctx, userID, filters)
		assert.Error(t, err)
	})
}

// TestGetSpendingByCategoryHousehold_SingleRoundTrip closes gap G2 (issue's explicit ask:
// "verify via query count in integration test"). pgxmock's ExpectationsWereMet fails if the
// code under test issues more or fewer queries than expected — exactly one ExpectQuery here
// proves the household CTE fan-out is a single SQL round trip, not N+1 across household
// accounts/members.
func TestGetSpendingByCategoryHousehold_SingleRoundTrip(t *testing.T) {
	mock, repo := setupAnalyticsMockDB(t)
	defer mock.Close()

	userID := "owner-1"
	filters := domain.SpendingFilters{
		StartDate: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
		EndDate:   time.Date(2024, 1, 31, 0, 0, 0, 0, time.UTC),
		HouseholdScope: &domain.HouseholdScope{
			HouseholdID:   "hh-1",
			MemberUserIDs: []string{"owner-1", "member-2"},
		},
	}

	rows := pgxmock.NewRows([]string{"category", "category_display", "total", "transaction_count"}).
		AddRow("FOOD_AND_DRINK", "Food and Drink", 150.0, 3)
	// Args: requestingUserID, householdID, memberUserIDs ($1-$3 via householdScopeArgs),
	// plus StartDate/EndDate ($4-$5) — see getSpendingByCategoryHousehold.
	mock.ExpectQuery(`WITH household_accounts`).
		WithArgs(userID, filters.HouseholdScope.HouseholdID, filters.HouseholdScope.MemberUserIDs, filters.StartDate, filters.EndDate).
		WillReturnRows(rows)

	_, err := repo.GetSpendingByCategory(context.Background(), userID, filters)
	require.NoError(t, err)
	require.NoError(t, mock.ExpectationsWereMet(), "household spending-by-category must be exactly one SQL round trip")
}

func TestGetSpendingByMerchant(t *testing.T) {
	ctx := context.Background()
	userID := "user-123"
	filters := domain.SpendingFilters{
		StartDate: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
		EndDate:   time.Date(2024, 3, 31, 0, 0, 0, 0, time.UTC),
	}
	limit := 10

	t.Run("returns merchant spending rows ordered by total desc", func(t *testing.T) {
		mock, repo := setupAnalyticsMockDB(t)
		defer mock.Close()

		starbucksID := "uuid-starbucks-001"
		rows := pgxmock.NewRows([]string{"merchant_name", "merchant_id", "total", "transaction_count"}).
			AddRow("Starbucks", &starbucksID, 145.0, 18).
			AddRow("Amazon", nil, 320.0, 5)

		mock.ExpectQuery(`SELECT`).
			WithArgs(userID, filters.StartDate, filters.EndDate, limit).
			WillReturnRows(rows)

		result, err := repo.GetSpendingByMerchant(ctx, userID, filters, limit)
		require.NoError(t, err)
		assert.Len(t, result, 2)
		assert.Equal(t, "Starbucks", result[0].MerchantName)
		assert.Equal(t, 145.0, result[0].Total)
		assert.Equal(t, 18, result[0].TransactionCount)
		require.NotNil(t, result[0].MerchantID)
		assert.Equal(t, starbucksID, *result[0].MerchantID)
		assert.Nil(t, result[1].MerchantID)
	})

	t.Run("returns empty slice when no data", func(t *testing.T) {
		mock, repo := setupAnalyticsMockDB(t)
		defer mock.Close()

		rows := pgxmock.NewRows([]string{"merchant_name", "merchant_id", "total", "transaction_count"})
		mock.ExpectQuery(`SELECT`).
			WithArgs(userID, filters.StartDate, filters.EndDate, limit).
			WillReturnRows(rows)

		result, err := repo.GetSpendingByMerchant(ctx, userID, filters, limit)
		require.NoError(t, err)
		assert.Empty(t, result)
	})

	t.Run("db error is returned", func(t *testing.T) {
		mock, repo := setupAnalyticsMockDB(t)
		defer mock.Close()

		mock.ExpectQuery(`SELECT`).
			WithArgs(userID, filters.StartDate, filters.EndDate, limit).
			WillReturnError(pgx.ErrNoRows)

		_, err := repo.GetSpendingByMerchant(ctx, userID, filters, limit)
		assert.Error(t, err)
	})

	// G2 structural tests — verify the SQL uses the full override join chain,
	// CASE-based synthetic GROUP BY, and canonical target display name.

	t.Run("query contains merchant_name_overrides join", func(t *testing.T) {
		mock, repo := setupAnalyticsMockDB(t)
		defer mock.Close()

		rows := pgxmock.NewRows([]string{"merchant_name", "merchant_id", "total", "transaction_count"})
		mock.ExpectQuery(`(?i)merchant_name_overrides\s+mno`).
			WithArgs(userID, filters.StartDate, filters.EndDate, limit).
			WillReturnRows(rows)

		_, err := repo.GetSpendingByMerchant(ctx, userID, filters, limit)
		require.NoError(t, err)
		require.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("query contains target override join mno_t", func(t *testing.T) {
		mock, repo := setupAnalyticsMockDB(t)
		defer mock.Close()

		rows := pgxmock.NewRows([]string{"merchant_name", "merchant_id", "total", "transaction_count"})
		mock.ExpectQuery(`(?i)merchant_name_overrides\s+mno_t`).
			WithArgs(userID, filters.StartDate, filters.EndDate, limit).
			WillReturnRows(rows)

		_, err := repo.GetSpendingByMerchant(ctx, userID, filters, limit)
		require.NoError(t, err)
		require.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("query groups by synthetic CASE key not raw merchant_name", func(t *testing.T) {
		mock, repo := setupAnalyticsMockDB(t)
		defer mock.Close()

		rows := pgxmock.NewRows([]string{"merchant_name", "merchant_id", "total", "transaction_count"})
		// Must contain GROUP BY CASE WHEN t.merchant_id IS NOT NULL
		mock.ExpectQuery(`(?i)GROUP BY\s+CASE`).
			WithArgs(userID, filters.StartDate, filters.EndDate, limit).
			WillReturnRows(rows)

		_, err := repo.GetSpendingByMerchant(ctx, userID, filters, limit)
		require.NoError(t, err)
		require.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("query selects COALESCE merged id not MAX(m.id)", func(t *testing.T) {
		mock, repo := setupAnalyticsMockDB(t)
		defer mock.Close()

		rows := pgxmock.NewRows([]string{"merchant_name", "merchant_id", "total", "transaction_count"})
		// Must contain COALESCE(mno.merged_into_merchant_id for the merchant_id SELECT
		mock.ExpectQuery(`(?i)COALESCE\(mno\.merged_into_merchant_id`).
			WithArgs(userID, filters.StartDate, filters.EndDate, limit).
			WillReturnRows(rows)

		_, err := repo.GetSpendingByMerchant(ctx, userID, filters, limit)
		require.NoError(t, err)
		require.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("query uses canonical target display name via mno_t.display_name", func(t *testing.T) {
		mock, repo := setupAnalyticsMockDB(t)
		defer mock.Close()

		rows := pgxmock.NewRows([]string{"merchant_name", "merchant_id", "total", "transaction_count"})
		// Must use mno_t.display_name as first COALESCE arg for the merchant_name column
		mock.ExpectQuery(`(?i)mno_t\.display_name`).
			WithArgs(userID, filters.StartDate, filters.EndDate, limit).
			WillReturnRows(rows)

		_, err := repo.GetSpendingByMerchant(ctx, userID, filters, limit)
		require.NoError(t, err)
		require.NoError(t, mock.ExpectationsWereMet())
	})
}

func TestGetSpendingTrends(t *testing.T) {
	ctx := context.Background()
	userID := "user-123"
	filters := domain.SpendingFilters{
		StartDate: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
		EndDate:   time.Date(2024, 3, 31, 0, 0, 0, 0, time.UTC),
	}

	t.Run("returns monthly category rows", func(t *testing.T) {
		mock, repo := setupAnalyticsMockDB(t)
		defer mock.Close()

		jan := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
		feb := time.Date(2024, 2, 1, 0, 0, 0, 0, time.UTC)

		rows := pgxmock.NewRows([]string{"period_start", "category", "category_display", "total"}).
			AddRow(jan, "FOOD_AND_DRINK", "", 200.0).
			AddRow(jan, "TRANSPORTATION", "", 80.0).
			AddRow(feb, "FOOD_AND_DRINK", "", 230.0)

		mock.ExpectQuery(`SELECT`).
			WithArgs(userID, filters.StartDate, filters.EndDate).
			WillReturnRows(rows)

		result, err := repo.GetSpendingTrends(ctx, userID, filters)
		require.NoError(t, err)
		assert.Len(t, result, 3)
		assert.True(t, result[0].PeriodStart.Equal(jan))
		assert.Equal(t, "FOOD_AND_DRINK", result[0].Category)
		assert.Equal(t, 200.0, result[0].Total)
	})

	t.Run("returns empty slice when no data", func(t *testing.T) {
		mock, repo := setupAnalyticsMockDB(t)
		defer mock.Close()

		rows := pgxmock.NewRows([]string{"period_start", "category", "category_display", "total"})
		mock.ExpectQuery(`SELECT`).
			WithArgs(userID, filters.StartDate, filters.EndDate).
			WillReturnRows(rows)

		result, err := repo.GetSpendingTrends(ctx, userID, filters)
		require.NoError(t, err)
		assert.Empty(t, result)
	})

	t.Run("db error is returned", func(t *testing.T) {
		mock, repo := setupAnalyticsMockDB(t)
		defer mock.Close()

		mock.ExpectQuery(`SELECT`).
			WithArgs(userID, filters.StartDate, filters.EndDate).
			WillReturnError(pgx.ErrNoRows)

		_, err := repo.GetSpendingTrends(ctx, userID, filters)
		assert.Error(t, err)
	})
}

func TestGetSpendingByCategory_CustomCategory(t *testing.T) {
	ctx := context.Background()
	userID := "user-123"
	filters := domain.SpendingFilters{
		StartDate: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
		EndDate:   time.Date(2024, 3, 31, 0, 0, 0, 0, time.UTC),
	}

	t.Run("populates PFCPrimaryDisplay from category_display column for custom rows", func(t *testing.T) {
		mock, repo := setupAnalyticsMockDB(t)
		defer mock.Close()

		rows := pgxmock.NewRows([]string{"category", "category_display", "total", "transaction_count"}).
			AddRow("custom:7a93a4f0-dead-beef-1234-abcdef012345", "My Coffee Budget", 150.0, 5)

		mock.ExpectQuery(`SELECT`).
			WithArgs(userID, filters.StartDate, filters.EndDate).
			WillReturnRows(rows)

		result, err := repo.GetSpendingByCategory(ctx, userID, filters)
		require.NoError(t, err)
		require.Len(t, result, 1)
		assert.Equal(t, "custom:7a93a4f0-dead-beef-1234-abcdef012345", result[0].Category)
		assert.Equal(t, "My Coffee Budget", result[0].PFCPrimaryDisplay)
		assert.Equal(t, 150.0, result[0].Total)
	})
}

func TestGetSpendingTrends_CustomCategory(t *testing.T) {
	ctx := context.Background()
	userID := "user-123"
	filters := domain.SpendingFilters{
		StartDate: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
		EndDate:   time.Date(2024, 3, 31, 0, 0, 0, 0, time.UTC),
	}

	t.Run("populates CategoryDisplay for custom rows in trend query", func(t *testing.T) {
		mock, repo := setupAnalyticsMockDB(t)
		defer mock.Close()

		jan := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
		rows := pgxmock.NewRows([]string{"period_start", "category", "category_display", "total"}).
			AddRow(jan, "custom:uuid-aaa-111", "My Coffee Budget", 150.0)

		mock.ExpectQuery(`SELECT`).
			WithArgs(userID, filters.StartDate, filters.EndDate).
			WillReturnRows(rows)

		result, err := repo.GetSpendingTrends(ctx, userID, filters)
		require.NoError(t, err)
		require.Len(t, result, 1)
		assert.Equal(t, "custom:uuid-aaa-111", result[0].Category)
		assert.Equal(t, "My Coffee Budget", result[0].CategoryDisplay)
		assert.Equal(t, 150.0, result[0].Total)
	})
}

// TestAnalyticsRepository_ActiveAccountFilter_RejectsReplacedAccounts verifies
// that all spending/cash-flow aggregations filter to active accounts only,
// ensuring re-linked (replaced) accounts are excluded from analytics.
// These tests use pgxmock and inspect the generated SQL for the status filter.

// TestGetSpendingByCategory_IncludesReplacedAccountTransactions verifies that
// spending queries use LEFT JOIN (no status filter) so pre-relink transactions
// from replaced accounts are still counted in historical analytics.
func TestGetSpendingByCategory_IncludesReplacedAccountTransactions(t *testing.T) {
	mock, repo := setupAnalyticsMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	userID := "user-active-filter"
	filters := domain.SpendingFilters{
		StartDate: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
		EndDate:   time.Date(2024, 3, 31, 0, 0, 0, 0, time.UTC),
	}

	rows := pgxmock.NewRows([]string{"category", "category_display", "total", "transaction_count"}).
		AddRow("FOOD_AND_DRINK", "", 100.0, 5)

	// Query must use LEFT JOIN and must NOT filter by a.status so that
	// transactions belonging to replaced accounts remain in analytics.
	mock.ExpectQuery(`(?i)LEFT JOIN accounts`).
		WithArgs(userID, filters.StartDate, filters.EndDate).
		WillReturnRows(rows)

	_, err := repo.GetSpendingByCategory(ctx, userID, filters)
	require.NoError(t, err)
	require.NoError(t, mock.ExpectationsWereMet())
}

// TestGetSpendingByMerchant_IncludesReplacedAccountTransactions verifies that
// merchant queries use LEFT JOIN so pre-relink history is preserved.
func TestGetSpendingByMerchant_IncludesReplacedAccountTransactions(t *testing.T) {
	mock, repo := setupAnalyticsMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	userID := "user-active-filter"
	filters := domain.SpendingFilters{
		StartDate: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
		EndDate:   time.Date(2024, 3, 31, 0, 0, 0, 0, time.UTC),
	}
	limit := 10

	rows := pgxmock.NewRows([]string{"merchant_name", "merchant_id", "total", "transaction_count"}).
		AddRow("Starbucks", nil, 50.0, 3)

	mock.ExpectQuery(`(?i)LEFT JOIN accounts`).
		WithArgs(userID, filters.StartDate, filters.EndDate, limit).
		WillReturnRows(rows)

	_, err := repo.GetSpendingByMerchant(ctx, userID, filters, limit)
	require.NoError(t, err)
	require.NoError(t, mock.ExpectationsWereMet())
}

// TestGetSpendingTrends_IncludesReplacedAccountTransactions verifies that
// trend queries use LEFT JOIN so pre-relink history is preserved.
func TestGetSpendingTrends_IncludesReplacedAccountTransactions(t *testing.T) {
	mock, repo := setupAnalyticsMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	userID := "user-active-filter"
	filters := domain.SpendingFilters{
		StartDate: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
		EndDate:   time.Date(2024, 3, 31, 0, 0, 0, 0, time.UTC),
	}

	jan := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	rows := pgxmock.NewRows([]string{"period_start", "category", "category_display", "total"}).
		AddRow(jan, "FOOD_AND_DRINK", "", 200.0)

	mock.ExpectQuery(`(?i)LEFT JOIN accounts`).
		WithArgs(userID, filters.StartDate, filters.EndDate).
		WillReturnRows(rows)

	_, err := repo.GetSpendingTrends(ctx, userID, filters)
	require.NoError(t, err)
	require.NoError(t, mock.ExpectationsWereMet())
}

// TestGetCashFlowAggregation_IncludesReplacedAccountTransactions verifies that
// cash-flow aggregation uses LEFT JOIN so pre-relink transactions remain visible.
func TestGetCashFlowAggregation_IncludesReplacedAccountTransactions(t *testing.T) {
	mock, repo := setupAnalyticsMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	userID := "user-active-filter"
	filters := domain.CashFlowFilters{
		StartDate: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
		EndDate:   time.Date(2024, 3, 31, 0, 0, 0, 0, time.UTC),
	}

	rows := pgxmock.NewRows([]string{"income", "income_count", "expenses", "expense_count"}).
		AddRow(500.0, 2, 300.0, 5)

	mock.ExpectQuery(`(?i)LEFT JOIN accounts`).
		WithArgs(userID, filters.StartDate, filters.EndDate).
		WillReturnRows(rows)

	_, err := repo.GetCashFlowAggregation(ctx, userID, filters)
	require.NoError(t, err)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestGetAverageMonthlySpend(t *testing.T) {
	ctx := context.Background()
	userID := "user-123"
	filters := domain.SpendingFilters{
		StartDate: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
		EndDate:   time.Date(2024, 3, 31, 0, 0, 0, 0, time.UTC),
	}

	t.Run("returns total spend row", func(t *testing.T) {
		mock, repo := setupAnalyticsMockDB(t)
		defer mock.Close()

		rows := pgxmock.NewRows([]string{"total"}).
			AddRow(300.0)

		mock.ExpectQuery(`SELECT`).
			WithArgs(userID, filters.StartDate, filters.EndDate).
			WillReturnRows(rows)

		result, err := repo.GetAverageMonthlySpend(ctx, userID, filters)
		require.NoError(t, err)
		assert.Equal(t, 300.0, result.Total)
	})

	t.Run("returns zero when no spend in period", func(t *testing.T) {
		mock, repo := setupAnalyticsMockDB(t)
		defer mock.Close()

		rows := pgxmock.NewRows([]string{"total"}).
			AddRow(0.0)

		mock.ExpectQuery(`SELECT`).
			WithArgs(userID, filters.StartDate, filters.EndDate).
			WillReturnRows(rows)

		result, err := repo.GetAverageMonthlySpend(ctx, userID, filters)
		require.NoError(t, err)
		assert.Equal(t, 0.0, result.Total)
	})

	t.Run("db error is returned", func(t *testing.T) {
		mock, repo := setupAnalyticsMockDB(t)
		defer mock.Close()

		mock.ExpectQuery(`SELECT`).
			WithArgs(userID, filters.StartDate, filters.EndDate).
			WillReturnError(pgx.ErrNoRows)

		_, err := repo.GetAverageMonthlySpend(ctx, userID, filters)
		assert.Error(t, err)
	})

	t.Run("uses JOIN accounts for account-type filtering", func(t *testing.T) {
		mock, repo := setupAnalyticsMockDB(t)
		defer mock.Close()

		rows := pgxmock.NewRows([]string{"total"}).AddRow(150.0)

		// Query must use LEFT JOIN accounts so pre-relink transactions remain visible (matches all other spending queries)
		mock.ExpectQuery(`(?i)LEFT JOIN accounts`).
			WithArgs(userID, filters.StartDate, filters.EndDate).
			WillReturnRows(rows)

		_, err := repo.GetAverageMonthlySpend(ctx, userID, filters)
		require.NoError(t, err)
		require.NoError(t, mock.ExpectationsWereMet())
	})
}

func TestGetRecurringSubscriptions(t *testing.T) {
	ctx := context.Background()
	userID := "user-123"

	t.Run("returns recurring subscriptions", func(t *testing.T) {
		mock, repo := setupAnalyticsMockDB(t)
		defer mock.Close()

		lookbackStart := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
		lastCharge := time.Date(2024, 12, 1, 0, 0, 0, 0, time.UTC)
		nextExpected := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)

		rows := pgxmock.NewRows([]string{
			"merchant_name", "amount", "occurrence_count",
			"avg_gap_days", "stddev_gap_days", "last_charge_date", "next_expected_date",
		}).
			AddRow("Netflix", 15.99, 12, 30.5, 1.2, lastCharge, nextExpected).
			AddRow("Spotify", 9.99, 12, 29.8, 0.9, lastCharge, nextExpected)

		mock.ExpectQuery(`SELECT`).
			WithArgs(userID, lookbackStart).
			WillReturnRows(rows)

		result, err := repo.GetRecurringSubscriptions(ctx, userID, lookbackStart)
		require.NoError(t, err)
		assert.Len(t, result, 2)
		assert.Equal(t, "Netflix", result[0].MerchantName)
		assert.InDelta(t, 15.99, result[0].Amount, 0.001)
		assert.Equal(t, 12, result[0].OccurrenceCount)
		assert.InDelta(t, 30.5, result[0].AvgGapDays, 0.001)
		assert.InDelta(t, 1.2, result[0].StddevGapDays, 0.001)
		assert.True(t, result[0].LastChargeDate.Equal(lastCharge))
		assert.True(t, result[0].NextExpectedDate.Equal(nextExpected))
		assert.Equal(t, "Spotify", result[1].MerchantName)
	})

	t.Run("returns empty slice on no results", func(t *testing.T) {
		mock, repo := setupAnalyticsMockDB(t)
		defer mock.Close()

		lookbackStart := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
		rows := pgxmock.NewRows([]string{
			"merchant_name", "amount", "occurrence_count",
			"avg_gap_days", "stddev_gap_days", "last_charge_date", "next_expected_date",
		})

		mock.ExpectQuery(`SELECT`).
			WithArgs(userID, lookbackStart).
			WillReturnRows(rows)

		result, err := repo.GetRecurringSubscriptions(ctx, userID, lookbackStart)
		require.NoError(t, err)
		assert.Empty(t, result)
	})

	t.Run("propagates db error", func(t *testing.T) {
		mock, repo := setupAnalyticsMockDB(t)
		defer mock.Close()

		lookbackStart := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)

		mock.ExpectQuery(`SELECT`).
			WithArgs(userID, lookbackStart).
			WillReturnError(pgx.ErrNoRows)

		_, err := repo.GetRecurringSubscriptions(ctx, userID, lookbackStart)
		assert.Error(t, err)
	})
}

func TestGetSavingsRateForPeriod(t *testing.T) {
	ctx := context.Background()
	userID := "user-123"
	startDate := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	endDate := time.Date(2024, 3, 31, 0, 0, 0, 0, time.UTC)

	t.Run("returns income and expenses row", func(t *testing.T) {
		mock, repo := setupAnalyticsMockDB(t)
		defer mock.Close()

		rows := pgxmock.NewRows([]string{"income", "expenses"}).
			AddRow(5000.0, 3000.0)

		mock.ExpectQuery(`SELECT`).
			WithArgs(userID, startDate, endDate).
			WillReturnRows(rows)

		result, err := repo.GetSavingsRateForPeriod(ctx, userID, startDate, endDate, nil)
		require.NoError(t, err)
		assert.InDelta(t, 5000.0, result.Income, 0.001)
		assert.InDelta(t, 3000.0, result.Expenses, 0.001)
	})

	t.Run("returns zero income and expenses when no transactions", func(t *testing.T) {
		mock, repo := setupAnalyticsMockDB(t)
		defer mock.Close()

		rows := pgxmock.NewRows([]string{"income", "expenses"}).
			AddRow(0.0, 0.0)

		mock.ExpectQuery(`SELECT`).
			WithArgs(userID, startDate, endDate).
			WillReturnRows(rows)

		result, err := repo.GetSavingsRateForPeriod(ctx, userID, startDate, endDate, nil)
		require.NoError(t, err)
		assert.Equal(t, 0.0, result.Income)
		assert.Equal(t, 0.0, result.Expenses)
	})

	t.Run("db error is returned", func(t *testing.T) {
		mock, repo := setupAnalyticsMockDB(t)
		defer mock.Close()

		mock.ExpectQuery(`SELECT`).
			WithArgs(userID, startDate, endDate).
			WillReturnError(pgx.ErrNoRows)

		_, err := repo.GetSavingsRateForPeriod(ctx, userID, startDate, endDate, nil)
		assert.Error(t, err)
	})
}

func TestGetMonthOverMonthSpending(t *testing.T) {
	ctx := context.Background()
	userID := "user-123"
	startDate := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)

	t.Run("returns monthly bucket rows", func(t *testing.T) {
		mock, repo := setupAnalyticsMockDB(t)
		defer mock.Close()

		jan := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
		feb := time.Date(2024, 2, 1, 0, 0, 0, 0, time.UTC)

		rows := pgxmock.NewRows([]string{"month", "total"}).
			AddRow(jan, 300.0).
			AddRow(feb, 250.0)

		mock.ExpectQuery(`SELECT`).
			WithArgs(userID, startDate).
			WillReturnRows(rows)

		result, err := repo.GetMonthOverMonthSpending(ctx, userID, startDate, "", domain.SpendingFilters{})
		require.NoError(t, err)
		assert.Len(t, result, 2)
		assert.True(t, result[0].Month.Equal(jan))
		assert.InDelta(t, 300.0, result[0].Total, 0.001)
		assert.True(t, result[1].Month.Equal(feb))
	})

	t.Run("with category filter passes category arg", func(t *testing.T) {
		mock, repo := setupAnalyticsMockDB(t)
		defer mock.Close()

		jan := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
		rows := pgxmock.NewRows([]string{"month", "total"}).
			AddRow(jan, 150.0)

		mock.ExpectQuery(`SELECT`).
			WithArgs(userID, startDate, "FOOD_AND_DRINK").
			WillReturnRows(rows)

		result, err := repo.GetMonthOverMonthSpending(ctx, userID, startDate, "FOOD_AND_DRINK", domain.SpendingFilters{})
		require.NoError(t, err)
		assert.Len(t, result, 1)
	})

	t.Run("returns empty slice when no data", func(t *testing.T) {
		mock, repo := setupAnalyticsMockDB(t)
		defer mock.Close()

		rows := pgxmock.NewRows([]string{"month", "total"})
		mock.ExpectQuery(`SELECT`).
			WithArgs(userID, startDate).
			WillReturnRows(rows)

		result, err := repo.GetMonthOverMonthSpending(ctx, userID, startDate, "", domain.SpendingFilters{})
		require.NoError(t, err)
		assert.Empty(t, result)
	})

	t.Run("db error is returned", func(t *testing.T) {
		mock, repo := setupAnalyticsMockDB(t)
		defer mock.Close()

		mock.ExpectQuery(`SELECT`).
			WithArgs(userID, startDate).
			WillReturnError(pgx.ErrNoRows)

		_, err := repo.GetMonthOverMonthSpending(ctx, userID, startDate, "", domain.SpendingFilters{})
		assert.Error(t, err)
	})

	t.Run("filters by account types when provided", func(t *testing.T) {
		mock, repo := setupAnalyticsMockDB(t)
		defer mock.Close()

		rows := pgxmock.NewRows([]string{"month", "total"}).
			AddRow(time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC), 200.0)
		// userID=$1, startDate=$2, then account type=$3
		mock.ExpectQuery(`(?i)LEFT JOIN accounts`).
			WithArgs("user-123", pgxmock.AnyArg(), "depository").
			WillReturnRows(rows)
		filters := domain.SpendingFilters{AccountTypes: []string{"depository"}}
		_, err := repo.GetMonthOverMonthSpending(ctx, "user-123", time.Now().AddDate(0, -6, 0), "", filters)
		require.NoError(t, err)
		require.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("include_transfers=true does not add transfer filter", func(t *testing.T) {
		mock, repo := setupAnalyticsMockDB(t)
		defer mock.Close()

		rows := pgxmock.NewRows([]string{"month", "total"}).
			AddRow(time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC), 500.0)
		// When include_transfers=true, no TRANSFER% predicate in query
		mock.ExpectQuery(`(?i)FROM transactions`).
			WithArgs("user-123", pgxmock.AnyArg()).
			WillReturnRows(rows)
		filters := domain.SpendingFilters{IncludeTransfers: true}
		_, err := repo.GetMonthOverMonthSpending(ctx, "user-123", time.Now().AddDate(0, -6, 0), "", filters)
		require.NoError(t, err)
		require.NoError(t, mock.ExpectationsWereMet())
	})
}

func TestGetLargestTransactionsForPeriod(t *testing.T) {
	ctx := context.Background()
	userID := "user-123"
	filters := domain.SpendingFilters{
		StartDate: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
		EndDate:   time.Date(2024, 3, 31, 0, 0, 0, 0, time.UTC),
	}
	limit := 5

	t.Run("returns largest transaction rows ordered by amount desc", func(t *testing.T) {
		mock, repo := setupAnalyticsMockDB(t)
		defer mock.Close()

		txDate := time.Date(2024, 2, 15, 0, 0, 0, 0, time.UTC)
		rows := pgxmock.NewRows([]string{"id", "merchant_name", "amount", "category", "date", "account_id"}).
			AddRow("tx-001", "Amazon", 500.0, "SHOPPING", txDate, "acc-001").
			AddRow("tx-002", "Whole Foods", 200.0, "FOOD_AND_DRINK", txDate, "acc-001")

		mock.ExpectQuery(`SELECT`).
			WithArgs(userID, filters.StartDate, filters.EndDate, limit).
			WillReturnRows(rows)

		result, err := repo.GetLargestTransactionsForPeriod(ctx, userID, filters, limit)
		require.NoError(t, err)
		assert.Len(t, result, 2)
		assert.Equal(t, "tx-001", result[0].TransactionID)
		assert.Equal(t, "Amazon", result[0].MerchantName)
		assert.InDelta(t, 500.0, result[0].Amount, 0.001)
		assert.Equal(t, "acc-001", result[0].AccountID)
	})

	t.Run("returns empty slice when no data", func(t *testing.T) {
		mock, repo := setupAnalyticsMockDB(t)
		defer mock.Close()

		rows := pgxmock.NewRows([]string{"id", "merchant_name", "amount", "category", "date", "account_id"})
		mock.ExpectQuery(`SELECT`).
			WithArgs(userID, filters.StartDate, filters.EndDate, limit).
			WillReturnRows(rows)

		result, err := repo.GetLargestTransactionsForPeriod(ctx, userID, filters, limit)
		require.NoError(t, err)
		assert.NotNil(t, result)
		assert.Empty(t, result)
	})

	t.Run("db error is returned", func(t *testing.T) {
		mock, repo := setupAnalyticsMockDB(t)
		defer mock.Close()

		mock.ExpectQuery(`SELECT`).
			WithArgs(userID, filters.StartDate, filters.EndDate, limit).
			WillReturnError(pgx.ErrNoRows)

		_, err := repo.GetLargestTransactionsForPeriod(ctx, userID, filters, limit)
		assert.Error(t, err)
	})

	t.Run("uses LEFT JOIN accounts for account-type filtering", func(t *testing.T) {
		mock, repo := setupAnalyticsMockDB(t)
		defer mock.Close()

		rows := pgxmock.NewRows([]string{"id", "merchant_name", "amount", "category", "date", "account_id"})
		mock.ExpectQuery(`(?i)LEFT JOIN accounts`).
			WithArgs(userID, filters.StartDate, filters.EndDate, limit).
			WillReturnRows(rows)

		_, err := repo.GetLargestTransactionsForPeriod(ctx, userID, filters, limit)
		require.NoError(t, err)
		require.NoError(t, mock.ExpectationsWereMet())
	})
}

func TestGetCategoryBreakdownForMerchant(t *testing.T) {
	ctx := context.Background()
	userID := "user-123"
	merchantID := "550e8400-e29b-41d4-a716-446655440000"
	filters := domain.SpendingFilters{
		StartDate: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
		EndDate:   time.Date(2024, 3, 31, 0, 0, 0, 0, time.UTC),
	}

	t.Run("returns category breakdown rows for merchant by UUID", func(t *testing.T) {
		mock, repo := setupAnalyticsMockDB(t)
		defer mock.Close()

		rows := pgxmock.NewRows([]string{"category", "total", "count"}).
			AddRow("SHOPPING", 400.0, 5).
			AddRow("GENERAL_MERCHANDISE", 200.0, 3)

		mock.ExpectQuery(`SELECT`).
			WithArgs(userID, filters.StartDate, filters.EndDate, merchantID).
			WillReturnRows(rows)

		result, err := repo.GetCategoryBreakdownForMerchant(ctx, userID, merchantID, filters)
		require.NoError(t, err)
		assert.Len(t, result, 2)
		assert.Equal(t, "SHOPPING", result[0].Category)
		assert.InDelta(t, 400.0, result[0].Total, 0.001)
		assert.Equal(t, 5, result[0].Count)
	})

	t.Run("returns empty slice when no data", func(t *testing.T) {
		mock, repo := setupAnalyticsMockDB(t)
		defer mock.Close()

		rows := pgxmock.NewRows([]string{"category", "total", "count"})
		mock.ExpectQuery(`SELECT`).
			WithArgs(userID, filters.StartDate, filters.EndDate, merchantID).
			WillReturnRows(rows)

		result, err := repo.GetCategoryBreakdownForMerchant(ctx, userID, merchantID, filters)
		require.NoError(t, err)
		assert.Empty(t, result)
	})

	t.Run("db error is returned", func(t *testing.T) {
		mock, repo := setupAnalyticsMockDB(t)
		defer mock.Close()

		mock.ExpectQuery(`SELECT`).
			WithArgs(userID, filters.StartDate, filters.EndDate, merchantID).
			WillReturnError(pgx.ErrNoRows)

		_, err := repo.GetCategoryBreakdownForMerchant(ctx, userID, merchantID, filters)
		assert.Error(t, err)
	})

	t.Run("query uses merchant_id UUID cast and 4-join override chain", func(t *testing.T) {
		mock, repo := setupAnalyticsMockDB(t)
		defer mock.Close()

		rows := pgxmock.NewRows([]string{"category", "total", "count"})
		mock.ExpectQuery(`(?i)COALESCE\(mno\.merged_into_merchant_id.*::uuid`).
			WithArgs(userID, filters.StartDate, filters.EndDate, merchantID).
			WillReturnRows(rows)

		_, err := repo.GetCategoryBreakdownForMerchant(ctx, userID, merchantID, filters)
		require.NoError(t, err)
		require.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("uses LEFT JOIN accounts for account-type filtering", func(t *testing.T) {
		mock, repo := setupAnalyticsMockDB(t)
		defer mock.Close()

		rows := pgxmock.NewRows([]string{"category", "total", "count"})
		mock.ExpectQuery(`(?i)LEFT JOIN accounts`).
			WithArgs(userID, filters.StartDate, filters.EndDate, merchantID).
			WillReturnRows(rows)

		_, err := repo.GetCategoryBreakdownForMerchant(ctx, userID, merchantID, filters)
		require.NoError(t, err)
		require.NoError(t, mock.ExpectationsWereMet())
	})
}

func TestGetIncomeSourceBreakdown(t *testing.T) {
	ctx := context.Background()
	userID := "user-123"
	filters := domain.SpendingFilters{
		StartDate: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
		EndDate:   time.Date(2024, 3, 31, 0, 0, 0, 0, time.UTC),
	}

	t.Run("returns income source rows", func(t *testing.T) {
		mock, repo := setupAnalyticsMockDB(t)
		defer mock.Close()

		rows := pgxmock.NewRows([]string{"category", "total", "count"}).
			AddRow("INCOME", 5000.0, 3).
			AddRow("TRANSFER_IN", 1000.0, 2)

		mock.ExpectQuery(`SELECT`).
			WithArgs(userID, filters.StartDate, filters.EndDate).
			WillReturnRows(rows)

		result, err := repo.GetIncomeSourceBreakdown(ctx, userID, filters)
		require.NoError(t, err)
		assert.Len(t, result, 2)
		assert.Equal(t, "INCOME", result[0].Category)
		assert.InDelta(t, 5000.0, result[0].Total, 0.001)
		assert.Equal(t, 3, result[0].Count)
	})

	t.Run("returns empty slice when no data", func(t *testing.T) {
		mock, repo := setupAnalyticsMockDB(t)
		defer mock.Close()

		rows := pgxmock.NewRows([]string{"category", "total", "count"})
		mock.ExpectQuery(`SELECT`).
			WithArgs(userID, filters.StartDate, filters.EndDate).
			WillReturnRows(rows)

		result, err := repo.GetIncomeSourceBreakdown(ctx, userID, filters)
		require.NoError(t, err)
		assert.Empty(t, result)
	})

	t.Run("db error is returned", func(t *testing.T) {
		mock, repo := setupAnalyticsMockDB(t)
		defer mock.Close()

		mock.ExpectQuery(`SELECT`).
			WithArgs(userID, filters.StartDate, filters.EndDate).
			WillReturnError(pgx.ErrNoRows)

		_, err := repo.GetIncomeSourceBreakdown(ctx, userID, filters)
		assert.Error(t, err)
	})

	t.Run("uses LEFT JOIN accounts for account-type filtering", func(t *testing.T) {
		mock, repo := setupAnalyticsMockDB(t)
		defer mock.Close()

		rows := pgxmock.NewRows([]string{"category", "total", "count"})
		mock.ExpectQuery(`(?i)LEFT JOIN accounts`).
			WithArgs(userID, filters.StartDate, filters.EndDate).
			WillReturnRows(rows)

		_, err := repo.GetIncomeSourceBreakdown(ctx, userID, filters)
		require.NoError(t, err)
		require.NoError(t, mock.ExpectationsWereMet())
	})
}

// TestGetCashFlowGrouped_IncludesReplacedAccountTransactions verifies that
// grouped cash-flow uses LEFT JOIN so pre-relink transactions remain visible.
func TestGetCashFlowGrouped_IncludesReplacedAccountTransactions(t *testing.T) {
	mock, repo := setupAnalyticsMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	userID := "user-active-filter"
	filters := domain.CashFlowFilters{
		StartDate: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
		EndDate:   time.Date(2024, 3, 31, 0, 0, 0, 0, time.UTC),
		Grouping:  "monthly",
	}

	jan := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	rows := pgxmock.NewRows([]string{"period_start", "income", "expenses", "transaction_count"}).
		AddRow(jan, 500.0, 300.0, 7)

	mock.ExpectQuery(`(?i)LEFT JOIN accounts`).
		WithArgs(userID, filters.StartDate, filters.EndDate).
		WillReturnRows(rows)

	_, err := repo.GetCashFlowGrouped(ctx, userID, filters)
	require.NoError(t, err)
	require.NoError(t, mock.ExpectationsWereMet())
}
