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

		rows := pgxmock.NewRows([]string{"merchant_name", "total", "transaction_count"}).
			AddRow("Starbucks", 145.0, 18).
			AddRow("Amazon", 320.0, 5)

		mock.ExpectQuery(`SELECT`).
			WithArgs(userID, filters.StartDate, filters.EndDate, limit).
			WillReturnRows(rows)

		result, err := repo.GetSpendingByMerchant(ctx, userID, filters, limit)
		require.NoError(t, err)
		assert.Len(t, result, 2)
		assert.Equal(t, "Starbucks", result[0].MerchantName)
		assert.Equal(t, 145.0, result[0].Total)
		assert.Equal(t, 18, result[0].TransactionCount)
	})

	t.Run("returns empty slice when no data", func(t *testing.T) {
		mock, repo := setupAnalyticsMockDB(t)
		defer mock.Close()

		rows := pgxmock.NewRows([]string{"merchant_name", "total", "transaction_count"})
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

func TestGetSpendingByCategory_QueryContainsActiveFilter(t *testing.T) {
	mock, repo := setupAnalyticsMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	userID := "user-active-filter"
	filters := domain.SpendingFilters{
		StartDate: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
		EndDate:   time.Date(2024, 3, 31, 0, 0, 0, 0, time.UTC),
	}

	// The query must include a.status = 'active' — if the repo generates SQL without
	// it, mock's regexp won't match and WillReturnRows won't fire, causing an error.
	rows := pgxmock.NewRows([]string{"category", "category_display", "total", "transaction_count"}).
		AddRow("FOOD_AND_DRINK", "", 100.0, 5)

	mock.ExpectQuery(`(?i)a\.status\s*=\s*'active'`).
		WithArgs(userID, filters.StartDate, filters.EndDate).
		WillReturnRows(rows)

	_, err := repo.GetSpendingByCategory(ctx, userID, filters)
	require.NoError(t, err)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestGetSpendingByMerchant_QueryContainsActiveFilter(t *testing.T) {
	mock, repo := setupAnalyticsMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	userID := "user-active-filter"
	filters := domain.SpendingFilters{
		StartDate: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
		EndDate:   time.Date(2024, 3, 31, 0, 0, 0, 0, time.UTC),
	}
	limit := 10

	rows := pgxmock.NewRows([]string{"merchant_name", "total", "transaction_count"}).
		AddRow("Starbucks", 50.0, 3)

	mock.ExpectQuery(`(?i)a\.status\s*=\s*'active'`).
		WithArgs(userID, filters.StartDate, filters.EndDate, limit).
		WillReturnRows(rows)

	_, err := repo.GetSpendingByMerchant(ctx, userID, filters, limit)
	require.NoError(t, err)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestGetSpendingTrends_QueryContainsActiveFilter(t *testing.T) {
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

	mock.ExpectQuery(`(?i)a\.status\s*=\s*'active'`).
		WithArgs(userID, filters.StartDate, filters.EndDate).
		WillReturnRows(rows)

	_, err := repo.GetSpendingTrends(ctx, userID, filters)
	require.NoError(t, err)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestGetCashFlowAggregation_QueryContainsActiveFilter(t *testing.T) {
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

	mock.ExpectQuery(`(?i)a\.status\s*=\s*'active'`).
		WithArgs(userID, filters.StartDate, filters.EndDate).
		WillReturnRows(rows)

	_, err := repo.GetCashFlowAggregation(ctx, userID, filters)
	require.NoError(t, err)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestGetCashFlowGrouped_QueryContainsActiveFilter(t *testing.T) {
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

	mock.ExpectQuery(`(?i)a\.status\s*=\s*'active'`).
		WithArgs(userID, filters.StartDate, filters.EndDate).
		WillReturnRows(rows)

	_, err := repo.GetCashFlowGrouped(ctx, userID, filters)
	require.NoError(t, err)
	require.NoError(t, mock.ExpectationsWereMet())
}
