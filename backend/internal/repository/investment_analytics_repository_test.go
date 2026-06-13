package repository

import (
	"context"
	"regexp"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/pashagolub/pgxmock/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
)

// setupInvestmentAnalyticsMockDB creates a mock pool and a fresh investmentAnalyticsRepository.
func setupInvestmentAnalyticsMockDB(t *testing.T) (pgxmock.PgxPoolIface, InvestmentAnalyticsRepository) {
	t.Helper()
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	repo := NewInvestmentAnalyticsRepository(mock)
	return mock, repo
}

// incomeSubtypeAnyArgs returns a slice of pgxmock.AnyArg() matchers sized for the
// IncomeSummary query: 3 base args (userID, start, end) + one per IncomeSubtype.
func incomeSubtypeAnyArgs() []interface{} {
	n := 3 + len(domain.IncomeSubtypes)
	args := make([]interface{}, n)
	for i := range args {
		args[i] = pgxmock.AnyArg()
	}
	return args
}

// TestInvestmentAnalytics_AllocationBreakdown_ReturnsGroupedBuckets tests that AllocationBreakdown
// groups rows into buckets and computes percentages correctly.
func TestInvestmentAnalytics_AllocationBreakdown_ReturnsGroupedBuckets(t *testing.T) {
	mock, repo := setupInvestmentAnalyticsMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	userID := uuid.New().String()

	rows := pgxmock.NewRows([]string{"security_type", "holding_count", "total_value"}).
		AddRow("stock", 3, 10000.0).
		AddRow("etf", 2, 5000.0)

	mock.ExpectQuery(regexp.QuoteMeta(`COALESCE(sec.type, 'unknown')`)).
		WithArgs(pgxmock.AnyArg()).
		WillReturnRows(rows)

	result, err := repo.AllocationBreakdown(ctx, userID)

	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Len(t, result.Buckets, 2)
	assert.InDelta(t, 15000.0, result.TotalValue, 0.01)

	// stock bucket: 10000 / 15000 * 100 ≈ 66.67%
	assert.Equal(t, "stock", result.Buckets[0].SecurityType)
	assert.Equal(t, 3, result.Buckets[0].HoldingCount)
	assert.InDelta(t, 10000.0, result.Buckets[0].TotalValue, 0.01)
	assert.InDelta(t, 66.67, result.Buckets[0].Percentage, 0.01)

	// etf bucket: 5000 / 15000 * 100 ≈ 33.33%
	assert.Equal(t, "etf", result.Buckets[1].SecurityType)
	assert.Equal(t, 2, result.Buckets[1].HoldingCount)
	assert.InDelta(t, 5000.0, result.Buckets[1].TotalValue, 0.01)
	assert.InDelta(t, 33.33, result.Buckets[1].Percentage, 0.01)

	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestInvestmentAnalytics_AllocationBreakdown_EmptyPortfolio tests that an empty result
// returns an empty Buckets slice and zero TotalValue.
func TestInvestmentAnalytics_AllocationBreakdown_EmptyPortfolio(t *testing.T) {
	mock, repo := setupInvestmentAnalyticsMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	userID := uuid.New().String()

	rows := pgxmock.NewRows([]string{"security_type", "holding_count", "total_value"})

	mock.ExpectQuery(regexp.QuoteMeta(`COALESCE(sec.type, 'unknown')`)).
		WithArgs(pgxmock.AnyArg()).
		WillReturnRows(rows)

	result, err := repo.AllocationBreakdown(ctx, userID)

	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Empty(t, result.Buckets)
	assert.InDelta(t, 0.0, result.TotalValue, 0.01)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestInvestmentAnalytics_TopMovers_ReturnsSplit tests that TopMovers correctly splits
// rows into Gainers and Losers based on the mover_type column.
func TestInvestmentAnalytics_TopMovers_ReturnsSplit(t *testing.T) {
	mock, repo := setupInvestmentAnalyticsMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	userID := uuid.New().String()
	limit := 5

	holdingID1 := uuid.New().String()
	holdingID2 := uuid.New().String()
	accountID1 := uuid.New().String()
	accountID2 := uuid.New().String()
	ticker1 := "AAPL"
	ticker2 := "XYZ"
	secName1 := "Apple Inc."
	secName2 := "XYZ Corp"
	dayChange1 := 100.0
	dayChangePct1 := 2.5
	dayChange2 := -50.0
	dayChangePct2 := -1.2
	totalGL1 := 500.0
	totalGL2 := -200.0

	rows := pgxmock.NewRows([]string{
		"holding_id", "account_id", "ticker_symbol", "security_name",
		"day_change", "day_change_pct", "total_gain_loss", "mover_type",
	}).
		AddRow(holdingID1, accountID1, &ticker1, &secName1, &dayChange1, &dayChangePct1, &totalGL1, "gainer").
		AddRow(holdingID2, accountID2, &ticker2, &secName2, &dayChange2, &dayChangePct2, &totalGL2, "loser")

	mock.ExpectQuery(regexp.QuoteMeta(`WITH movers AS`)).
		WithArgs(pgxmock.AnyArg(), pgxmock.AnyArg()).
		WillReturnRows(rows)

	result, err := repo.TopMovers(ctx, userID, limit)

	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Len(t, result.Gainers, 1)
	assert.Len(t, result.Losers, 1)

	gainer := result.Gainers[0]
	assert.Equal(t, holdingID1, gainer.HoldingID)
	assert.Equal(t, accountID1, gainer.AccountID)
	require.NotNil(t, gainer.DayChange)
	assert.InDelta(t, 100.0, *gainer.DayChange, 0.01)

	loser := result.Losers[0]
	assert.Equal(t, holdingID2, loser.HoldingID)
	assert.Equal(t, accountID2, loser.AccountID)
	require.NotNil(t, loser.DayChange)
	assert.InDelta(t, -50.0, *loser.DayChange, 0.01)

	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestInvestmentAnalytics_UnrealizedGainLoss_AggregatesAccounts tests that UnrealizedGainLoss
// sums account rows into total figures, sets RealizedGainLossSupported=false.
func TestInvestmentAnalytics_UnrealizedGainLoss_AggregatesAccounts(t *testing.T) {
	mock, repo := setupInvestmentAnalyticsMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	userID := uuid.New().String()

	accountID1 := uuid.New().String()
	accountID2 := uuid.New().String()
	// Account 1: value=8000, cost_basis=6000, unrealized_gl=2000, pct=33.33%
	pct1 := 33.33
	// Account 2: value=4000, cost_basis=5000, unrealized_gl=-1000, pct=-20.0%
	pct2 := -20.0

	// Column order matches the repository scan: account_id, value, unrealized_gl, unrealized_gl_pct, cost_basis_known
	rows := pgxmock.NewRows([]string{
		"account_id", "value", "unrealized_gl", "unrealized_gl_pct", "cost_basis_known",
	}).
		AddRow(accountID1, 8000.0, 2000.0, &pct1, 6000.0).
		AddRow(accountID2, 4000.0, -1000.0, &pct2, 5000.0)

	mock.ExpectQuery(regexp.QuoteMeta(`h.account_id::text`)).
		WithArgs(pgxmock.AnyArg()).
		WillReturnRows(rows)

	result, err := repo.UnrealizedGainLoss(ctx, userID)

	require.NoError(t, err)
	require.NotNil(t, result)

	// Totals: value=12000, costBasis=11000, unrealizedGL=1000
	assert.InDelta(t, 12000.0, result.TotalValue, 0.01)
	assert.InDelta(t, 11000.0, result.TotalCostBasis, 0.01)
	assert.InDelta(t, 1000.0, result.TotalUnrealizedGainLoss, 0.01)
	// totalCostBasis > 0 so pct should be non-nil: 1000/11000*100 ≈ 9.09%
	require.NotNil(t, result.TotalUnrealizedGainLossPct)
	assert.InDelta(t, 9.09, *result.TotalUnrealizedGainLossPct, 0.01)

	assert.False(t, result.RealizedGainLossSupported)
	assert.Len(t, result.ByAccount, 2)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestInvestmentAnalytics_IncomeSummary_GroupsByType tests that IncomeSummary aggregates
// income rows into ByType entries and computes TotalIncome correctly.
//
// The mock rows simulate what the real DB returns AFTER the COALESCE(-SUM(it.amount), 0)
// negation in the query. Plaid stores income (dividends, interest) as negative amounts
// (cash-credit convention); the SQL negates them so the DB returns positive magnitudes.
// The mock feeds those already-positive values — the unit test covers Go scanning logic.
// Real-sign correctness is locked by TestInvestment_IncomeSummary_NegativePlaidAmounts_Integration.
func TestInvestmentAnalytics_IncomeSummary_GroupsByType(t *testing.T) {
	mock, repo := setupInvestmentAnalyticsMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	userID := uuid.New().String()
	start := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2024, 3, 31, 23, 59, 59, 0, time.UTC)

	// Positive values — represent post-negation DB output (COALESCE(-SUM(amount), 0) = +200/+50).
	rows := pgxmock.NewRows([]string{"income_type", "count", "total_amount"}).
		AddRow("dividend", 5, 200.0).
		AddRow("interest", 3, 50.0)

	mock.ExpectQuery(regexp.QuoteMeta(`income_type`)).
		WithArgs(incomeSubtypeAnyArgs()...).
		WillReturnRows(rows)

	result, err := repo.IncomeSummary(ctx, userID, start, end)

	require.NoError(t, err)
	require.NotNil(t, result)
	assert.InDelta(t, 250.0, result.TotalIncome, 0.01)
	assert.Len(t, result.ByType, 2)

	assert.Equal(t, "dividend", result.ByType[0].IncomeType)
	assert.Equal(t, 5, result.ByType[0].Count)
	assert.InDelta(t, 200.0, result.ByType[0].TotalAmount, 0.01)

	assert.Equal(t, "interest", result.ByType[1].IncomeType)
	assert.Equal(t, 3, result.ByType[1].Count)
	assert.InDelta(t, 50.0, result.ByType[1].TotalAmount, 0.01)

	assert.Equal(t, start, result.StartDate)
	assert.Equal(t, end, result.EndDate)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestInvestmentAnalytics_IncomeSummary_EmptyRange tests that IncomeSummary returns
// empty ByType and zero TotalIncome when no transactions fall in the date range.
func TestInvestmentAnalytics_IncomeSummary_EmptyRange(t *testing.T) {
	mock, repo := setupInvestmentAnalyticsMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	userID := uuid.New().String()
	start := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2020, 1, 31, 23, 59, 59, 0, time.UTC)

	rows := pgxmock.NewRows([]string{"income_type", "count", "total_amount"})

	mock.ExpectQuery(regexp.QuoteMeta(`income_type`)).
		WithArgs(incomeSubtypeAnyArgs()...).
		WillReturnRows(rows)

	result, err := repo.IncomeSummary(ctx, userID, start, end)

	require.NoError(t, err)
	require.NotNil(t, result)
	assert.InDelta(t, 0.0, result.TotalIncome, 0.01)
	assert.Empty(t, result.ByType)
	assert.Equal(t, start, result.StartDate)
	assert.Equal(t, end, result.EndDate)
	assert.NoError(t, mock.ExpectationsWereMet())
}
