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

		rows := pgxmock.NewRows([]string{"category", "total", "transaction_count"}).
			AddRow("FOOD_AND_DRINK", 250.0, 15).
			AddRow("TRANSPORTATION", 120.0, 8)

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

		rows := pgxmock.NewRows([]string{"category", "total", "transaction_count"}).
			AddRow("RESTAURANTS", 180.0, 12).
			AddRow("GROCERIES", 70.0, 3)

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

		rows := pgxmock.NewRows([]string{"category", "total", "transaction_count"})
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

		rows := pgxmock.NewRows([]string{"period_start", "category", "total"}).
			AddRow(jan, "FOOD_AND_DRINK", 200.0).
			AddRow(jan, "TRANSPORTATION", 80.0).
			AddRow(feb, "FOOD_AND_DRINK", 230.0)

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

		rows := pgxmock.NewRows([]string{"period_start", "category", "total"})
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
