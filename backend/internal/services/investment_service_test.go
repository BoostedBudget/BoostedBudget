package services

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	phclient "github.com/maxnussbaum/BoostedBudget/internal/integration/posthog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
)

// ============================================================================
// InvestmentService tests
// ============================================================================

func makeTestHolding(id, userID, accountID, secID string, instValue, costBasis, instPrice *float64) domain.Holding {
	h := domain.Holding{
		ID:               id,
		UserID:           userID,
		AccountID:        accountID,
		SecurityID:       secID,
		Quantity:         10.0,
		InstitutionValue: instValue,
		CostBasis:        costBasis,
		InstitutionPrice: instPrice,
	}
	return h
}

func ptr64(v float64) *float64 { return &v }

func TestInvestmentService_ListHoldings_ReturnsViews(t *testing.T) {
	holdingRepo := new(MockHoldingRepository)
	snapshotRepo := new(MockPortfolioSnapshotRepository)
	invTxnRepo := new(MockInvestmentTransactionRepository)

	svc := NewInvestmentService(holdingRepo, snapshotRepo, invTxnRepo)
	ctx := context.Background()
	userID := "user-abc"

	holdings := []domain.Holding{
		makeTestHolding("h1", userID, "acct-1", "sec-1", ptr64(1000.0), ptr64(800.0), ptr64(100.0)),
		makeTestHolding("h2", userID, "acct-2", "sec-2", ptr64(500.0), nil, nil),
	}
	holdingRepo.On("GetByUserIDWithSecurities", ctx, userID, (*string)(nil)).Return(holdings, nil)

	views, err := svc.ListHoldings(ctx, userID, nil)

	require.NoError(t, err)
	assert.Len(t, views, 2)
	assert.Equal(t, "h1", views[0].ID)
	assert.Equal(t, "h2", views[1].ID)
	holdingRepo.AssertExpectations(t)
}

func TestInvestmentService_ListHoldings_AccountFilter(t *testing.T) {
	holdingRepo := new(MockHoldingRepository)
	snapshotRepo := new(MockPortfolioSnapshotRepository)
	invTxnRepo := new(MockInvestmentTransactionRepository)

	svc := NewInvestmentService(holdingRepo, snapshotRepo, invTxnRepo)
	ctx := context.Background()
	userID := "user-abc"
	accountID := "acct-1"

	holdings := []domain.Holding{
		makeTestHolding("h1", userID, accountID, "sec-1", ptr64(1000.0), ptr64(800.0), ptr64(100.0)),
	}
	holdingRepo.On("GetByUserIDWithSecurities", ctx, userID, &accountID).Return(holdings, nil)

	views, err := svc.ListHoldings(ctx, userID, &accountID)

	require.NoError(t, err)
	assert.Len(t, views, 1)
	holdingRepo.AssertExpectations(t)
}

func TestInvestmentService_ListHoldings_Empty(t *testing.T) {
	holdingRepo := new(MockHoldingRepository)
	snapshotRepo := new(MockPortfolioSnapshotRepository)
	invTxnRepo := new(MockInvestmentTransactionRepository)

	svc := NewInvestmentService(holdingRepo, snapshotRepo, invTxnRepo)
	ctx := context.Background()
	userID := "user-none"

	holdingRepo.On("GetByUserIDWithSecurities", ctx, userID, (*string)(nil)).Return(nil, nil)

	views, err := svc.ListHoldings(ctx, userID, nil)

	require.NoError(t, err)
	assert.Empty(t, views)
}

// TestInvestmentService_GetPortfolioSummary_WithPriorSnapshot verifies that
// DayChange is computed as totalValue − prior.TotalValueInvestmentOnly (#1613 P4-T3: the
// TYPE-GATED snapshot total, because totalValue is itself type-gated; prior.TotalValue is the
// deliberately UNGATED historical series and is NOT comparable with it). Here the user holds
// nothing outside an investment account, so the two snapshot totals coincide.
func TestInvestmentService_GetPortfolioSummary_WithPriorSnapshot(t *testing.T) {
	holdingRepo := new(MockHoldingRepository)
	snapshotRepo := new(MockPortfolioSnapshotRepository)
	invTxnRepo := new(MockInvestmentTransactionRepository)

	svc := NewInvestmentService(holdingRepo, snapshotRepo, invTxnRepo)
	ctx := context.Background()
	userID := "user-sum"

	holdings := []domain.Holding{
		makeTestHolding("h1", userID, "acct-1", "sec-1", ptr64(1200.0), ptr64(1000.0), nil),
	}
	holdingRepo.On("GetByUserIDWithSecurities", ctx, userID, (*string)(nil)).Return(holdings, nil)

	today := time.Now().UTC().Truncate(24 * time.Hour)
	priorSnap := &domain.PortfolioSnapshot{
		UserID:                   userID,
		TotalValue:               1100.0,
		TotalValueInvestmentOnly: ptr64(1100.0),
	}
	snapshotRepo.On("GetLatestBefore", ctx, userID, today).Return(priorSnap, nil)

	summary, err := svc.GetPortfolioSummary(ctx, userID)

	require.NoError(t, err)
	require.NotNil(t, summary)
	assert.Equal(t, 1200.0, summary.TotalValue)
	assert.Equal(t, 1000.0, summary.TotalCostBasis)
	assert.InDelta(t, 200.0, summary.TotalGainLoss, 0.01)
	require.NotNil(t, summary.TotalGainLossPct, "should have gain/loss pct when cost_basis > 0")
	assert.InDelta(t, 20.0, *summary.TotalGainLossPct, 0.01)

	require.NotNil(t, summary.DayChange, "DayChange should be set when prior snapshot exists")
	assert.InDelta(t, 100.0, *summary.DayChange, 0.01) // 1200 − 1100
	require.NotNil(t, summary.DayChangePct)
	assert.InDelta(t, 9.09, *summary.DayChangePct, 0.1) // 100/1100*100

	assert.Equal(t, 1, summary.HoldingsCount)
	holdingRepo.AssertExpectations(t)
	snapshotRepo.AssertExpectations(t)
}

// TestInvestmentService_GetPortfolioSummary_NoPriorSnapshot verifies that
// DayChange is nil (not 0) when no prior snapshot exists.
func TestInvestmentService_GetPortfolioSummary_NoPriorSnapshot(t *testing.T) {
	holdingRepo := new(MockHoldingRepository)
	snapshotRepo := new(MockPortfolioSnapshotRepository)
	invTxnRepo := new(MockInvestmentTransactionRepository)

	svc := NewInvestmentService(holdingRepo, snapshotRepo, invTxnRepo)
	ctx := context.Background()
	userID := "user-first"

	holdings := []domain.Holding{
		makeTestHolding("h1", userID, "acct-1", "sec-1", ptr64(500.0), nil, nil),
	}
	holdingRepo.On("GetByUserIDWithSecurities", ctx, userID, (*string)(nil)).Return(holdings, nil)

	today := time.Now().UTC().Truncate(24 * time.Hour)
	snapshotRepo.On("GetLatestBefore", ctx, userID, today).Return(nil, nil)

	summary, err := svc.GetPortfolioSummary(ctx, userID)

	require.NoError(t, err)
	require.NotNil(t, summary)
	assert.Equal(t, 500.0, summary.TotalValue)
	assert.Nil(t, summary.DayChange, "DayChange must be nil (not 0) when no prior snapshot")
	assert.Nil(t, summary.DayChangePct)
	assert.Nil(t, summary.TotalGainLossPct, "no cost_basis → gain/loss pct nil")
}

// TestInvestmentService_GetPortfolioSummary_NoCostBasis verifies that
// TotalGainLossPct is nil when cost_basis is 0 (prevents division by zero).
func TestInvestmentService_GetPortfolioSummary_NoCostBasis(t *testing.T) {
	holdingRepo := new(MockHoldingRepository)
	snapshotRepo := new(MockPortfolioSnapshotRepository)
	invTxnRepo := new(MockInvestmentTransactionRepository)

	svc := NewInvestmentService(holdingRepo, snapshotRepo, invTxnRepo)
	ctx := context.Background()
	userID := "user-nocb"

	// cost_basis = nil → totals to 0
	holdings := []domain.Holding{
		makeTestHolding("h1", userID, "acct-1", "sec-1", ptr64(800.0), nil, nil),
	}
	holdingRepo.On("GetByUserIDWithSecurities", ctx, userID, (*string)(nil)).Return(holdings, nil)

	today := time.Now().UTC().Truncate(24 * time.Hour)
	snapshotRepo.On("GetLatestBefore", ctx, userID, today).Return(nil, nil)

	summary, err := svc.GetPortfolioSummary(ctx, userID)

	require.NoError(t, err)
	assert.Nil(t, summary.TotalGainLossPct, "TotalGainLossPct must be nil when cost_basis=0")
}

// TestInvestmentService_GetPortfolioSummary_NullValueKnownBasis verifies that a holding
// with cost_basis set but institution_value NULL does NOT inflate (or deflate) the gain/loss.
// Scenario: A(value=1000, basis=800) + B(value=nil, basis=500).
// Expected: TotalGainLoss = 200 (knownBasis pair only), NOT -300 (phantom loss). — Round-6 R6-1 RED
func TestInvestmentService_GetPortfolioSummary_NullValueKnownBasis(t *testing.T) {
	holdingRepo := new(MockHoldingRepository)
	snapshotRepo := new(MockPortfolioSnapshotRepository)
	invTxnRepo := new(MockInvestmentTransactionRepository)

	svc := NewInvestmentService(holdingRepo, snapshotRepo, invTxnRepo)
	ctx := context.Background()
	userID := "user-nullval"

	holdings := []domain.Holding{
		// A: both non-nil → contributes to knownBasisValue + knownBasisCost
		makeTestHolding("h1", userID, "acct-1", "sec-1", ptr64(1000.0), ptr64(800.0), nil),
		// B: institution_value nil, cost_basis set → must NOT produce phantom loss
		makeTestHolding("h2", userID, "acct-1", "sec-2", nil, ptr64(500.0), nil),
	}
	holdingRepo.On("GetByUserIDWithSecurities", ctx, userID, (*string)(nil)).Return(holdings, nil)

	today := time.Now().UTC().Truncate(24 * time.Hour)
	snapshotRepo.On("GetLatestBefore", ctx, userID, today).Return(nil, nil)

	summary, err := svc.GetPortfolioSummary(ctx, userID)

	require.NoError(t, err)
	require.NotNil(t, summary)

	// TotalValue: only A contributes (B has nil institution_value)
	assert.InDelta(t, 1000.0, summary.TotalValue, 0.01, "TotalValue must only sum non-nil institution_value")
	// TotalCostBasis: both A and B contribute (all non-nil cost_basis, mirrors snapshot Upsert)
	assert.InDelta(t, 1300.0, summary.TotalCostBasis, 0.01, "TotalCostBasis must sum all non-nil cost_basis")
	// TotalGainLoss: ONLY over both-non-nil pair (A: 1000-800=200). NOT -300 (1000-1300 phantom loss).
	assert.InDelta(t, 200.0, summary.TotalGainLoss, 0.01, "TotalGainLoss must NOT be a phantom loss (-300)")
	// TotalGainLossPct: 200/800 * 100 = 25.0% (denominator is knownBasisCost=800, not totalCostBasis=1300)
	require.NotNil(t, summary.TotalGainLossPct, "TotalGainLossPct must be non-nil when any known-basis holding exists")
	assert.InDelta(t, 25.0, *summary.TotalGainLossPct, 0.01, "pct = 200/800*100 = 25%, not -23% (phantom loss pct)")

	assert.Equal(t, 2, summary.HoldingsCount)
	holdingRepo.AssertExpectations(t)
	snapshotRepo.AssertExpectations(t)
}

func TestInvestmentService_GetPortfolioHistory_MapsToDTOs(t *testing.T) {
	holdingRepo := new(MockHoldingRepository)
	snapshotRepo := new(MockPortfolioSnapshotRepository)
	invTxnRepo := new(MockInvestmentTransactionRepository)

	svc := NewInvestmentService(holdingRepo, snapshotRepo, invTxnRepo)
	ctx := context.Background()
	userID := "user-hist"

	start := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2025, 1, 31, 0, 0, 0, 0, time.UTC)

	snaps := []domain.PortfolioSnapshot{
		{SnapshotDate: time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC), TotalValue: 1000.0},
		{SnapshotDate: time.Date(2025, 1, 15, 0, 0, 0, 0, time.UTC), TotalValue: 1050.0},
		{SnapshotDate: time.Date(2025, 1, 31, 0, 0, 0, 0, time.UTC), TotalValue: 1100.0},
	}
	snapshotRepo.On("GetByUserDateRange", ctx, userID, start, end).Return(snaps, nil)

	pts, err := svc.GetPortfolioHistory(ctx, userID, start, end)

	require.NoError(t, err)
	// #1631 M8a fix round 2 (I-2): the result is the whole SERIES now — points plus the currency
	// they are plotted in — so the points live under .Points. These snapshots state no currency,
	// which is unknown rather than a conflict, so the series names none and is not mixed.
	assert.Len(t, pts.Points, 3)
	assert.Equal(t, 1000.0, pts.Points[0].TotalValue)
	assert.Equal(t, 1050.0, pts.Points[1].TotalValue)
	assert.Equal(t, 1100.0, pts.Points[2].TotalValue)
	assert.Nil(t, pts.CurrencyCode)
	assert.False(t, pts.IsMixedCurrency)
	snapshotRepo.AssertExpectations(t)
}

func TestInvestmentService_GetPortfolioHistory_Empty(t *testing.T) {
	holdingRepo := new(MockHoldingRepository)
	snapshotRepo := new(MockPortfolioSnapshotRepository)
	invTxnRepo := new(MockInvestmentTransactionRepository)

	svc := NewInvestmentService(holdingRepo, snapshotRepo, invTxnRepo)
	ctx := context.Background()
	userID := "user-empty"

	start := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2025, 1, 31, 0, 0, 0, 0, time.UTC)

	snapshotRepo.On("GetByUserDateRange", ctx, userID, start, end).Return(nil, nil)

	pts, err := svc.GetPortfolioHistory(ctx, userID, start, end)

	require.NoError(t, err)
	require.NotNil(t, pts, "an empty range is still a series, not a nil one")
	assert.Empty(t, pts.Points)
	assert.Nil(t, pts.CurrencyCode)
	assert.False(t, pts.IsMixedCurrency)
}

func TestInvestmentService_ListInvestmentTransactions_EncodesNextCursor(t *testing.T) {
	holdingRepo := new(MockHoldingRepository)
	snapshotRepo := new(MockPortfolioSnapshotRepository)
	invTxnRepo := new(MockInvestmentTransactionRepository)

	svc := NewInvestmentService(holdingRepo, snapshotRepo, invTxnRepo)
	ctx := context.Background()
	userID := "user-txn"

	filters := domain.InvestmentTransactionFilters{}
	txDate := time.Date(2025, 3, 15, 0, 0, 0, 0, time.UTC)
	txns := []domain.InvestmentTransaction{
		{ID: "itxn-1", UserID: userID, Date: txDate, Amount: 150.0},
	}
	nextCursor := &domain.InvestmentTransactionCursor{
		V:   1,
		SVT: txDate,
		ID:  uuid.MustParse("00000000-0000-0000-0000-000000000001"),
	}
	invTxnRepo.On("GetByUserIDKeyset", ctx, userID, filters, (*domain.InvestmentTransactionCursor)(nil), 10).
		Return(txns, nextCursor, nil, nil)

	result, encodedCursor, err := svc.ListInvestmentTransactions(ctx, userID, filters, nil, 10)

	require.NoError(t, err)
	assert.Len(t, result, 1)
	require.NotNil(t, encodedCursor, "should return encoded cursor when repo returns next cursor")
	assert.NotEmpty(t, *encodedCursor)
	invTxnRepo.AssertExpectations(t)
}

func TestInvestmentService_ListInvestmentTransactions_LastPage(t *testing.T) {
	holdingRepo := new(MockHoldingRepository)
	snapshotRepo := new(MockPortfolioSnapshotRepository)
	invTxnRepo := new(MockInvestmentTransactionRepository)

	svc := NewInvestmentService(holdingRepo, snapshotRepo, invTxnRepo)
	ctx := context.Background()
	userID := "user-last"

	filters := domain.InvestmentTransactionFilters{}
	invTxnRepo.On("GetByUserIDKeyset", ctx, userID, filters, (*domain.InvestmentTransactionCursor)(nil), 10).
		Return([]domain.InvestmentTransaction{}, (*domain.InvestmentTransactionCursor)(nil), nil, nil)

	result, encodedCursor, err := svc.ListInvestmentTransactions(ctx, userID, filters, nil, 10)

	require.NoError(t, err)
	assert.Empty(t, result)
	assert.Nil(t, encodedCursor, "nil cursor → last page, no next cursor in response")
}

// TestInvestmentService_ListInvestmentTransactions_ForwardsSecurityFields verifies that
// TickerSymbol and SecurityName set by the repository are forwarded unchanged by the
// service (guards against enrichment-stripping in future service changes).
func TestInvestmentService_ListInvestmentTransactions_ForwardsSecurityFields(t *testing.T) {
	holdingRepo := new(MockHoldingRepository)
	snapshotRepo := new(MockPortfolioSnapshotRepository)
	invTxnRepo := new(MockInvestmentTransactionRepository)

	svc := NewInvestmentService(holdingRepo, snapshotRepo, invTxnRepo)
	ctx := context.Background()
	userID := "user-security"

	filters := domain.InvestmentTransactionFilters{}
	txns := []domain.InvestmentTransaction{
		{
			ID:           "itxn-sec",
			UserID:       userID,
			Date:         time.Date(2025, 6, 1, 0, 0, 0, 0, time.UTC),
			Amount:       500.0,
			TickerSymbol: ptrStr("MSFT"),
			SecurityName: ptrStr("Microsoft Corporation"),
		},
	}
	invTxnRepo.On("GetByUserIDKeyset", ctx, userID, filters, (*domain.InvestmentTransactionCursor)(nil), 10).
		Return(txns, (*domain.InvestmentTransactionCursor)(nil), nil, nil)

	result, _, err := svc.ListInvestmentTransactions(ctx, userID, filters, nil, 10)

	require.NoError(t, err)
	require.Len(t, result, 1)
	require.NotNil(t, result[0].TickerSymbol, "service must forward TickerSymbol from repo")
	assert.Equal(t, "MSFT", *result[0].TickerSymbol)
	require.NotNil(t, result[0].SecurityName, "service must forward SecurityName from repo")
	assert.Equal(t, "Microsoft Corporation", *result[0].SecurityName)
	invTxnRepo.AssertExpectations(t)
}

// mockAnalyticsForInvestment is a minimal phclient.Analytics stub for setter tests.
type mockAnalyticsForInvestment struct{}

func (m *mockAnalyticsForInvestment) Capture(distinctID, event string, properties map[string]interface{}) {
}
func (m *mockAnalyticsForInvestment) Identify(distinctID string, properties map[string]interface{}) {}
func (m *mockAnalyticsForInvestment) Close()                                                        {}

var _ phclient.Analytics = (*mockAnalyticsForInvestment)(nil)

func TestInvestmentService_SetAnalytics_DoesNotPanic(t *testing.T) {
	holdingRepo := new(MockHoldingRepository)
	snapshotRepo := new(MockPortfolioSnapshotRepository)
	invTxnRepo := new(MockInvestmentTransactionRepository)
	svc := NewInvestmentService(holdingRepo, snapshotRepo, invTxnRepo)
	is := svc.(*investmentService)
	require.NotPanics(t, func() {
		svc.SetAnalytics(&mockAnalyticsForInvestment{})
	}, "SetAnalytics with a valid analytics must not panic")
	assert.NotNil(t, is.analytics, "analytics field should be set after SetAnalytics(non-nil)")
	require.NotPanics(t, func() {
		svc.SetAnalytics(nil)
	}, "SetAnalytics with nil must not panic")
	assert.Nil(t, is.analytics, "analytics field should be nil after SetAnalytics(nil)")
}

// ── #1613 P4-T3 item I: DayChange must not subtract two different account universes ─────────
//
// The defect this pins: `totalValue` is summed from GetByUserIDWithSecurities, which is
// TYPE-GATED (holding_repository.go: `AND a.type = 'investment'`), while `prior.TotalValue` is
// read from portfolio_snapshots, whose writer is deliberately UNGATED
// (portfolio_snapshot_repository.go — gating it mid-stream would step the historical series down
// on deploy day with no backfill). Subtracting one from the other made DayChange wrong by the
// full value of every depository account carrying holdings, EVERY DAY — the exact cash-management
// account shape this work stream exists to fix.
//
// The fix compares gated-live against the gated column the snapshot now also writes.

// TestInvestmentService_GetPortfolioSummary_DayChangeUsesTheTypeGatedSnapshotTotal is the
// regression. The prior snapshot's ungated total (5000) includes a depository holding worth 3900
// that the live read cannot see; its type-gated total is 1100. DayChange must be 1200 − 1100, not
// 1200 − 5000.
func TestInvestmentService_GetPortfolioSummary_DayChangeUsesTheTypeGatedSnapshotTotal(t *testing.T) {
	holdingRepo := new(MockHoldingRepository)
	snapshotRepo := new(MockPortfolioSnapshotRepository)
	invTxnRepo := new(MockInvestmentTransactionRepository)

	svc := NewInvestmentService(holdingRepo, snapshotRepo, invTxnRepo)
	ctx := context.Background()
	userID := "user-gated-daychange"

	// Live holdings, already type-gated by the repository: 1200 of investment-account value.
	holdings := []domain.Holding{
		makeTestHolding("h1", userID, "acct-inv", "sec-1", ptr64(1200.0), ptr64(1000.0), nil),
	}
	holdingRepo.On("GetByUserIDWithSecurities", ctx, userID, (*string)(nil)).Return(holdings, nil)

	today := time.Now().UTC().Truncate(24 * time.Hour)
	priorSnap := &domain.PortfolioSnapshot{
		UserID:                   userID,
		TotalValue:               5000.0, // ungated: 1100 investment + 3900 depository
		TotalValueInvestmentOnly: ptr64(1100.0),
	}
	snapshotRepo.On("GetLatestBefore", ctx, userID, today).Return(priorSnap, nil)

	summary, err := svc.GetPortfolioSummary(ctx, userID)

	require.NoError(t, err)
	require.NotNil(t, summary.DayChange)
	assert.InDelta(t, 100.0, *summary.DayChange, 0.01,
		"DayChange must be gated-live minus gated-snapshot (1200−1100). A value near −3800 means "+
			"the ungated total_value is still being subtracted from a type-gated live total.")
	require.NotNil(t, summary.DayChangePct)
	assert.InDelta(t, 9.09, *summary.DayChangePct, 0.1,
		"DayChangePct divides by the SAME prior total DayChange used — fixing one and leaving the "+
			"other on the ungated column just moves the defect into the percentage.")
}

// TestInvestmentService_GetPortfolioSummary_DayChangeUnavailableWhenSnapshotPredatesTheGatedColumn
// pins the NULL case. Every portfolio_snapshots row written before migration 000189 has
// total_value_investment_only NULL and there is nothing to reconstruct it from. Unavailable for a
// day is the accepted cost; falling back to the ungated column, or coercing to zero, is not —
// both would render a fabricated number the user cannot tell apart from a real one.
func TestInvestmentService_GetPortfolioSummary_DayChangeUnavailableWhenSnapshotPredatesTheGatedColumn(t *testing.T) {
	holdingRepo := new(MockHoldingRepository)
	snapshotRepo := new(MockPortfolioSnapshotRepository)
	invTxnRepo := new(MockInvestmentTransactionRepository)

	svc := NewInvestmentService(holdingRepo, snapshotRepo, invTxnRepo)
	ctx := context.Background()
	userID := "user-prehistoric-snapshot"

	holdings := []domain.Holding{
		makeTestHolding("h1", userID, "acct-inv", "sec-1", ptr64(1200.0), ptr64(1000.0), nil),
	}
	holdingRepo.On("GetByUserIDWithSecurities", ctx, userID, (*string)(nil)).Return(holdings, nil)

	today := time.Now().UTC().Truncate(24 * time.Hour)
	priorSnap := &domain.PortfolioSnapshot{
		UserID:                   userID,
		TotalValue:               1100.0,
		TotalValueInvestmentOnly: nil, // written before migration 000189
	}
	snapshotRepo.On("GetLatestBefore", ctx, userID, today).Return(priorSnap, nil)

	summary, err := svc.GetPortfolioSummary(ctx, userID)

	require.NoError(t, err)
	require.NotNil(t, summary)
	assert.Equal(t, 1200.0, summary.TotalValue, "the live totals are unaffected by the NULL")
	assert.Nil(t, summary.DayChange,
		"DayChange must be UNAVAILABLE, never computed from the ungated total_value and never 0")
	assert.Nil(t, summary.DayChangePct,
		"DayChangePct must be unavailable for exactly the same reason — one guard, both fields")
}
