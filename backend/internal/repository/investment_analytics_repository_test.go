package repository

import (
	"context"
	"math"
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

// TestApplyNetBasis_OptionA tests that applyNetBasis implements Option-A semantics:
//   - TotalValue (headline) = asset_total − liabilities (true net worth)
//   - Slice percentages use the asset denominator so the donut ring sums to 100%
//   - Liability bucket is removed from the output slice
//
// Fixture: stock=5000, cash=3000, liabilities=1200; grandTotal=9200 (all positive).
// Expected: netTotal=6800 (5000+3000−1200), stock%≈62.5%, cash%≈37.5% (both /8000).
func TestApplyNetBasis_OptionA(t *testing.T) {
	buckets := []domain.AllocationItem{
		{SecurityType: "stock", AssetClass: "investments", TotalValue: 5000, HoldingCount: 1},
		{SecurityType: "cash", AssetClass: "cash", TotalValue: 3000, HoldingCount: 1},
		{SecurityType: "liabilities", AssetClass: "liabilities", TotalValue: 1200, HoldingCount: 1},
	}
	result, total := applyNetBasis(buckets)

	// Option A: headline = assets − liabilities = (9200−1200) − 1200 = 6800.
	assert.InDelta(t, 6800.0, total, 0.01, "net total must be asset_total minus liabilities")

	// Liabilities bucket must be removed.
	for _, b := range result {
		assert.NotEqual(t, "liabilities", b.SecurityType, "liabilities bucket must not appear in output")
	}

	// Exactly 2 asset buckets.
	require.Len(t, result, 2)

	var stockBucket, cashBucket *domain.AllocationItem
	for i := range result {
		switch result[i].SecurityType {
		case "stock":
			b := result[i]
			stockBucket = &b
		case "cash":
			b := result[i]
			cashBucket = &b
		}
	}
	require.NotNil(t, stockBucket, "stock bucket must be present")
	require.NotNil(t, cashBucket, "cash bucket must be present")

	// Percentages on asset denominator (8000), so ring sums to 100%.
	assert.InDelta(t, 5000.0/8000.0*100, stockBucket.Percentage, 0.1, "stock pct on asset denominator")
	assert.InDelta(t, 3000.0/8000.0*100, cashBucket.Percentage, 0.1, "cash pct on asset denominator")

	var totalPct float64
	for _, b := range result {
		totalPct += b.Percentage
	}
	assert.InDelta(t, 100.0, totalPct, 0.1, "percentages must sum to 100%%")
}

// TestApplyNetBasis_AccountMode tests that applyNetBasis correctly identifies liability
// buckets in group_by=account mode, where AssetClass is "credit" or "loan" (not SecurityType).
func TestApplyNetBasis_AccountMode(t *testing.T) {
	buckets := []domain.AllocationItem{
		{SecurityType: "Brokerage", AssetClass: "investment", TotalValue: 5000},
		{SecurityType: "Checking", AssetClass: "depository", TotalValue: 3000},
		{SecurityType: "Visa Card", AssetClass: "credit", TotalValue: 1200},
	}
	result, total := applyNetBasis(buckets)

	assert.InDelta(t, 6800.0, total, 0.01, "net total = asset_total − liabilities")
	require.Len(t, result, 2)

	var totalPct float64
	for _, b := range result {
		assert.NotEqual(t, "credit", b.AssetClass, "credit account must be removed")
		totalPct += b.Percentage
	}
	assert.InDelta(t, 100.0, totalPct, 0.1, "percentages must sum to 100%%")
}

// TestApplyNetBasis_NoLiabilities tests that applyNetBasis is a no-op when there are
// no liability buckets (basis=net with include=[] — investment-only allocation).
func TestApplyNetBasis_NoLiabilities(t *testing.T) {
	buckets := []domain.AllocationItem{
		{SecurityType: "stock", TotalValue: 5000, HoldingCount: 2},
		{SecurityType: "etf", TotalValue: 3000, HoldingCount: 1},
	}
	result, total := applyNetBasis(buckets)

	// No liabilities → net = assets = sum of bucket values; percentages unchanged.
	assert.InDelta(t, 8000.0, total, 0.01, "net total == asset total when no liabilities")
	require.Len(t, result, 2)

	var totalPct float64
	for _, b := range result {
		totalPct += b.Percentage
	}
	assert.InDelta(t, 100.0, totalPct, 0.1, "percentages must sum to 100%%")
}

// TestApplyNetBasis_OracleNetArithmetic is an oracle-style test: expected values are derived
// independently from raw input sums, not from the function's intermediate variables.
// This guards against implementation changes that alter the internal computation path
// (e.g. sign-convention changes in the SQL union query) without breaking correctness.
//
//	assetTotal     = stock + cash       = 5000 + 3000 = 8000
//	netTotal       = assetTotal − liab  = 8000 − 1200 = 6800
//	stock%         = 5000 / 8000 * 100  = 62.5
//	cash%          = 3000 / 8000 * 100  = 37.5
//	Σ percentages  = 100.0
func TestApplyNetBasis_OracleNetArithmetic(t *testing.T) {
	const (
		stockValue = 5000.0
		cashValue  = 3000.0
		liabValue  = 1200.0
	)

	buckets := []domain.AllocationItem{
		{SecurityType: "stock", AssetClass: "investments", TotalValue: stockValue, HoldingCount: 1},
		{SecurityType: "cash", AssetClass: "cash", TotalValue: cashValue, HoldingCount: 1},
		{SecurityType: "liabilities", AssetClass: "liabilities", TotalValue: liabValue, HoldingCount: 1},
	}

	// Oracle: compute expected outputs from raw inputs, independent of any intermediate variable.
	expectedAssetTotal := stockValue + cashValue              // 8000
	expectedNetTotal := expectedAssetTotal - liabValue        // 6800
	expectedStockPct := stockValue / expectedAssetTotal * 100 // 62.5
	expectedCashPct := cashValue / expectedAssetTotal * 100   // 37.5

	result, total := applyNetBasis(buckets)

	assert.InDelta(t, expectedNetTotal, total, 0.01, "net total must equal sum(assets) − sum(liabilities)")

	require.Len(t, result, 2, "only asset buckets must be returned")
	var stockPct, cashPct, sumPct float64
	for _, b := range result {
		require.NotEqual(t, "liabilities", b.SecurityType, "liabilities must be excluded from output")
		sumPct += b.Percentage
		switch b.SecurityType {
		case "stock":
			stockPct = b.Percentage
		case "cash":
			cashPct = b.Percentage
		}
	}
	assert.InDelta(t, expectedStockPct, stockPct, 0.1, "stock pct must be stock / asset_total * 100")
	assert.InDelta(t, expectedCashPct, cashPct, 0.1, "cash pct must be cash / asset_total * 100")
	assert.InDelta(t, 100.0, sumPct, 0.1, "slice percentages must sum to 100%%")
}

// TestApplyNetBasis_AllLiabilities tests the degenerate case where every bucket is a liability.
// assetTotal = 0, so the guard prevents divide-by-zero; net total is negative (user is underwater).
func TestApplyNetBasis_AllLiabilities(t *testing.T) {
	buckets := []domain.AllocationItem{
		{SecurityType: "credit-card", AssetClass: "credit", TotalValue: 2000},
		{SecurityType: "auto-loan", AssetClass: "loan", TotalValue: 5000},
	}
	result, total := applyNetBasis(buckets)

	assert.Empty(t, result, "no asset buckets expected when all inputs are liabilities")
	assert.InDelta(t, -7000.0, total, 0.01, "net total is negative when no assets")
	for _, b := range result {
		assert.True(t, !math.IsNaN(b.Percentage) && !math.IsInf(b.Percentage, 0), "no NaN/Inf percentages")
	}
}

// TestInvestmentAnalytics_AllocationBreakdown_ReturnsGroupedBuckets tests that AllocationBreakdown
// groups rows into buckets and computes percentages correctly.
func TestInvestmentAnalytics_AllocationBreakdown_ReturnsGroupedBuckets(t *testing.T) {
	mock, repo := setupInvestmentAnalyticsMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	userID := uuid.New().String()

	// currency_codes (#1631 M8a) is the set of DISTINCT currencies backing each bucket; both
	// buckets here are USD, so the breakdown names USD and is not mixed.
	rows := pgxmock.NewRows([]string{"security_type", "holding_count", "total_value", "currency_codes"}).
		AddRow("stock", 3, 10000.0, []string{"USD"}).
		AddRow("etf", 2, 5000.0, []string{"USD"})

	mock.ExpectQuery(regexp.QuoteMeta(`COALESCE(sec.type, 'unknown')`)).
		WithArgs(pgxmock.AnyArg()).
		WillReturnRows(rows)

	result, err := repo.AllocationBreakdown(ctx, userID, domain.AllocationOptions{})

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

	rows := pgxmock.NewRows([]string{"security_type", "holding_count", "total_value", "currency_codes"})

	mock.ExpectQuery(regexp.QuoteMeta(`COALESCE(sec.type, 'unknown')`)).
		WithArgs(pgxmock.AnyArg()).
		WillReturnRows(rows)

	result, err := repo.AllocationBreakdown(ctx, userID, domain.AllocationOptions{})

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
	rankingCcy := "USD"

	rows := pgxmock.NewRows([]string{
		"holding_id", "account_id", "ticker_symbol", "security_name",
		"day_change", "day_change_pct", "total_gain_loss", "currency_code", "mover_type",
		// #1631 CX1: the ranking SCOPE, constant across every row.
		"ranking_currency_code", "excluded_by_currency_count",
	}).
		AddRow(holdingID1, accountID1, &ticker1, &secName1, &dayChange1, &dayChangePct1, &totalGL1, (*string)(nil), "gainer", &rankingCcy, 2).
		AddRow(holdingID2, accountID2, &ticker2, &secName2, &dayChange2, &dayChangePct2, &totalGL2, (*string)(nil), "loser", &rankingCcy, 2)

	mock.ExpectQuery(regexp.QuoteMeta(`WITH movers AS`)).
		WithArgs(pgxmock.AnyArg(), pgxmock.AnyArg()).
		WillReturnRows(rows)

	result, err := repo.TopMovers(ctx, userID, limit)

	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Len(t, result.Gainers, 1)
	assert.Len(t, result.Losers, 1)
	// The ranking scope must survive the scan, or a client presents a currency-scoped ranking as
	// a whole-portfolio one (#1631 CX1).
	require.NotNil(t, result.RankingCurrencyCode)
	assert.Equal(t, "USD", *result.RankingCurrencyCode)
	assert.Equal(t, 2, result.ExcludedByCurrencyCount)

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

	// Column order matches the repository scan (R9-1: added cost_basis_all as 6th column):
	// account_id, value, unrealized_gl, unrealized_gl_pct, cost_basis_known_pair, cost_basis_all.
	// In this test all holdings have both values set, so known_pair == cost_basis_all.
	rows := pgxmock.NewRows([]string{
		"account_id", "value", "unrealized_gl", "unrealized_gl_pct", "cost_basis_known_pair", "cost_basis_all",
		"currency_codes",
	}).
		AddRow(accountID1, 8000.0, 2000.0, &pct1, 6000.0, 6000.0, []string{"USD"}).
		AddRow(accountID2, 4000.0, -1000.0, &pct2, 5000.0, 5000.0, []string{"USD"})

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
	rows := pgxmock.NewRows([]string{"income_type", "count", "total_amount", "currency_codes"}).
		AddRow("dividend", 5, 200.0, []string{"USD"}).
		AddRow("interest", 3, 50.0, []string{"USD"})

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

	rows := pgxmock.NewRows([]string{"income_type", "count", "total_amount", "currency_codes"})

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

// ============================================================================
// PositionReconciliationSummary (#1631 M8b N2)
// ============================================================================

// TestInvestmentAnalytics_PositionReconciliationSummary_ScansAggregateRow proves the summary
// issues ONE aggregate query (QueryRow, not Query+loop) and scans exactly the three counters
// domain.PositionReconciliationSummary needs — the whole point of pushing classification into
// SQL rather than streaming every (account, security) pair into Go on /admin/health's 30-second
// poll (see buildPositionReconciliationSummaryQuery's doc comment).
func TestInvestmentAnalytics_PositionReconciliationSummary_ScansAggregateRow(t *testing.T) {
	mock, repo := setupInvestmentAnalyticsMockDB(t)
	defer mock.Close()

	ctx := context.Background()

	rows := pgxmock.NewRows([]string{"drift_count", "suppressed_count", "total_checked"}).
		AddRow(int64(3), int64(40), int64(120))

	// The reinvest bucket's structural/subtype-literal predicate carries 4 placeholder args
	// (reinvestSubtypes) — see domain.TradeTypeToSQLPredicate. No user_id arg: this is the
	// GLOBAL summary, unscoped by design.
	mock.ExpectQuery(regexp.QuoteMeta("WITH")).
		WithArgs(anyArgs(4)...).
		WillReturnRows(rows)

	summary, err := repo.PositionReconciliationSummary(ctx)

	require.NoError(t, err)
	assert.Equal(t, int64(3), summary.DriftCount)
	assert.Equal(t, int64(40), summary.SuppressedPreWindowCount)
	assert.Equal(t, int64(120), summary.TotalPositionsChecked)
	assert.True(t, summary.Available)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestInvestmentAnalytics_PositionReconciliationSummary_QueryError_IsUnavailable proves a failed
// query surfaces as an error (SystemHealthService turns this into Available=false — see
// system_health_service_position_reconciliation_test.go) rather than a silently-zeroed summary.
func TestInvestmentAnalytics_PositionReconciliationSummary_QueryError_IsUnavailable(t *testing.T) {
	mock, repo := setupInvestmentAnalyticsMockDB(t)
	defer mock.Close()

	ctx := context.Background()

	mock.ExpectQuery(regexp.QuoteMeta("WITH")).
		WithArgs(anyArgs(4)...).
		WillReturnError(assert.AnError)

	summary, err := repo.PositionReconciliationSummary(ctx)

	assert.Nil(t, summary)
	assert.Error(t, err)
	assert.NoError(t, mock.ExpectationsWereMet())
}
