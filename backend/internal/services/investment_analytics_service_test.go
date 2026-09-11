package services

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
)

// ============================================================================
// MockInvestmentAnalyticsRepository
// ============================================================================

type MockInvestmentAnalyticsRepository struct {
	mock.Mock
}

func (m *MockInvestmentAnalyticsRepository) AllocationBreakdown(ctx context.Context, userID string, opts domain.AllocationOptions) (*domain.AllocationBreakdown, error) {
	args := m.Called(ctx, userID, opts)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*domain.AllocationBreakdown), args.Error(1)
}

func (m *MockInvestmentAnalyticsRepository) TopMovers(ctx context.Context, userID string, limit int) (*domain.TopMovers, error) {
	args := m.Called(ctx, userID, limit)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*domain.TopMovers), args.Error(1)
}

func (m *MockInvestmentAnalyticsRepository) UnrealizedGainLoss(ctx context.Context, userID string) (*domain.GainLossSummary, error) {
	args := m.Called(ctx, userID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*domain.GainLossSummary), args.Error(1)
}

func (m *MockInvestmentAnalyticsRepository) IncomeSummary(ctx context.Context, userID string, start, end time.Time) (*domain.IncomeSummary, error) {
	args := m.Called(ctx, userID, start, end)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*domain.IncomeSummary), args.Error(1)
}

func (m *MockInvestmentAnalyticsRepository) PositionReconciliation(ctx context.Context, userID string) (*domain.PositionReconciliationReport, error) {
	args := m.Called(ctx, userID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*domain.PositionReconciliationReport), args.Error(1)
}

func (m *MockInvestmentAnalyticsRepository) PositionReconciliationSummary(ctx context.Context) (*domain.PositionReconciliationSummary, error) {
	args := m.Called(ctx)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*domain.PositionReconciliationSummary), args.Error(1)
}

// ============================================================================
// GetAllocationBreakdown
// ============================================================================

func TestInvestmentAnalyticsService_GetAllocationBreakdown_DelegatesAndReturns(t *testing.T) {
	repo := new(MockInvestmentAnalyticsRepository)
	svc := NewInvestmentAnalyticsService(repo)
	ctx := context.Background()
	userID := "user-alloc"

	expected := &domain.AllocationBreakdown{
		TotalValue: 5000.0,
		Buckets: []domain.AllocationItem{
			{SecurityType: "stock", TotalValue: 3000.0, Percentage: 60.0, HoldingCount: 5},
			{SecurityType: "etf", TotalValue: 2000.0, Percentage: 40.0, HoldingCount: 2},
		},
	}
	defaultOpts := domain.AllocationOptions{}
	repo.On("AllocationBreakdown", ctx, userID, defaultOpts).Return(expected, nil)

	result, err := svc.GetAllocationBreakdown(ctx, userID, defaultOpts)

	require.NoError(t, err)
	assert.Equal(t, expected, result)
	repo.AssertExpectations(t)
}

func TestInvestmentAnalyticsService_GetAllocationBreakdown_PropagatesError(t *testing.T) {
	repo := new(MockInvestmentAnalyticsRepository)
	svc := NewInvestmentAnalyticsService(repo)
	ctx := context.Background()

	opts := domain.AllocationOptions{}
	repo.On("AllocationBreakdown", ctx, "user-err", opts).Return(nil, errors.New("db error"))

	result, err := svc.GetAllocationBreakdown(ctx, "user-err", opts)

	assert.Nil(t, result)
	assert.ErrorContains(t, err, "GetAllocationBreakdown")
}

// TestInvestmentAnalyticsService_GetAllocationBreakdown_PassesOptionsThroughToRepo
// verifies that AllocationOptions are forwarded verbatim to the repository.
func TestInvestmentAnalyticsService_GetAllocationBreakdown_PassesOptionsThroughToRepo(t *testing.T) {
	repo := new(MockInvestmentAnalyticsRepository)
	svc := NewInvestmentAnalyticsService(repo)
	ctx := context.Background()
	userID := "user-opts"

	opts := domain.AllocationOptions{
		Include: []string{"cash", "liabilities"},
		GroupBy: domain.AllocationGroupByAssetClass,
		Basis:   domain.AllocationBasisNet,
	}
	expected := &domain.AllocationBreakdown{TotalValue: 10000.0, Buckets: []domain.AllocationItem{}}
	repo.On("AllocationBreakdown", ctx, userID, opts).Return(expected, nil)

	result, err := svc.GetAllocationBreakdown(ctx, userID, opts)

	require.NoError(t, err)
	assert.Equal(t, expected, result)
	repo.AssertExpectations(t)
}

// ============================================================================
// GetTopMovers
// ============================================================================

func TestInvestmentAnalyticsService_GetTopMovers_DelegatesWithLimit(t *testing.T) {
	repo := new(MockInvestmentAnalyticsRepository)
	svc := NewInvestmentAnalyticsService(repo)
	ctx := context.Background()
	userID := "user-movers"

	dc := -50.0
	expected := &domain.TopMovers{
		Gainers: []domain.TopMoverItem{{HoldingID: "h1", DayChange: ptr64(120.0)}},
		Losers:  []domain.TopMoverItem{{HoldingID: "h2", DayChange: &dc}},
	}
	repo.On("TopMovers", ctx, userID, 3).Return(expected, nil)

	result, err := svc.GetTopMovers(ctx, userID, 3)

	require.NoError(t, err)
	assert.Equal(t, expected, result)
	assert.Len(t, result.Gainers, 1)
	assert.Len(t, result.Losers, 1)
	repo.AssertExpectations(t)
}

// TestInvestmentAnalyticsService_GetTopMovers_DefaultsLimitToFive verifies
// that a limit of 0 or negative is replaced with the default of 5.
func TestInvestmentAnalyticsService_GetTopMovers_DefaultsLimitToFive(t *testing.T) {
	repo := new(MockInvestmentAnalyticsRepository)
	svc := NewInvestmentAnalyticsService(repo)
	ctx := context.Background()
	userID := "user-default"

	expected := &domain.TopMovers{Gainers: nil, Losers: nil}
	// Service must default 0 → 5
	repo.On("TopMovers", ctx, userID, 5).Return(expected, nil)

	_, err := svc.GetTopMovers(ctx, userID, 0)

	require.NoError(t, err)
	repo.AssertExpectations(t)
}

func TestInvestmentAnalyticsService_GetTopMovers_NegativeLimitDefaultsToFive(t *testing.T) {
	repo := new(MockInvestmentAnalyticsRepository)
	svc := NewInvestmentAnalyticsService(repo)
	ctx := context.Background()
	userID := "user-neg"

	repo.On("TopMovers", ctx, userID, 5).Return(&domain.TopMovers{}, nil)

	_, err := svc.GetTopMovers(ctx, userID, -1)

	require.NoError(t, err)
	repo.AssertExpectations(t)
}

// ============================================================================
// GetGainLossSummary — unrealized only, realized never supported
// ============================================================================

func TestInvestmentAnalyticsService_GetGainLossSummary_UnrealizedOnly(t *testing.T) {
	repo := new(MockInvestmentAnalyticsRepository)
	svc := NewInvestmentAnalyticsService(repo)
	ctx := context.Background()
	userID := "user-gl"

	pct := 12.5
	expected := &domain.GainLossSummary{
		TotalUnrealizedGainLoss:    500.0,
		TotalUnrealizedGainLossPct: &pct,
		TotalCostBasis:             4000.0,
		TotalValue:                 4500.0,
		RealizedGainLossSupported:  false, // always false
	}
	repo.On("UnrealizedGainLoss", ctx, userID).Return(expected, nil)

	result, err := svc.GetGainLossSummary(ctx, userID)

	require.NoError(t, err)
	assert.Equal(t, expected, result)
	assert.False(t, result.RealizedGainLossSupported,
		"RealizedGainLossSupported MUST be false — Plaid does not provide per-lot cost basis")
	repo.AssertExpectations(t)
}

func TestInvestmentAnalyticsService_GetGainLossSummary_PropagatesError(t *testing.T) {
	repo := new(MockInvestmentAnalyticsRepository)
	svc := NewInvestmentAnalyticsService(repo)
	ctx := context.Background()

	repo.On("UnrealizedGainLoss", ctx, "user-err").Return(nil, errors.New("query failed"))

	result, err := svc.GetGainLossSummary(ctx, "user-err")

	assert.Nil(t, result)
	assert.ErrorContains(t, err, "GetGainLossSummary")
}

// ============================================================================
// GetIncomeSummary
// ============================================================================

func TestInvestmentAnalyticsService_GetIncomeSummary_DelegatesAndReturns(t *testing.T) {
	repo := new(MockInvestmentAnalyticsRepository)
	svc := NewInvestmentAnalyticsService(repo)
	ctx := context.Background()
	userID := "user-income"

	start := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2025, 3, 31, 0, 0, 0, 0, time.UTC)

	expected := &domain.IncomeSummary{
		TotalIncome: 350.0,
		StartDate:   start,
		EndDate:     end,
		ByType: []domain.IncomeByType{
			{IncomeType: "dividend", TotalAmount: 200.0, Count: 4},
			{IncomeType: "dividend_reinvestment", TotalAmount: 100.0, Count: 2},
			{IncomeType: "interest", TotalAmount: 50.0, Count: 1},
		},
	}
	repo.On("IncomeSummary", ctx, userID, start, end).Return(expected, nil)

	result, err := svc.GetIncomeSummary(ctx, userID, start, end)

	require.NoError(t, err)
	assert.Equal(t, expected, result)
	// Verify reinvested income is included (economically income per plan spec)
	var hasReinvest bool
	for _, b := range result.ByType {
		if b.IncomeType == "dividend_reinvestment" || b.IncomeType == "interest_reinvestment" {
			hasReinvest = true
		}
	}
	assert.True(t, hasReinvest, "income summary should include reinvested dividends/interest")
	repo.AssertExpectations(t)
}

func TestInvestmentAnalyticsService_GetIncomeSummary_PropagatesError(t *testing.T) {
	repo := new(MockInvestmentAnalyticsRepository)
	svc := NewInvestmentAnalyticsService(repo)
	ctx := context.Background()

	start := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2025, 3, 31, 0, 0, 0, 0, time.UTC)

	repo.On("IncomeSummary", ctx, "user-err", start, end).Return(nil, errors.New("timeout"))

	result, err := svc.GetIncomeSummary(ctx, "user-err", start, end)

	assert.Nil(t, result)
	assert.ErrorContains(t, err, "GetIncomeSummary")
}

// ============================================================================
// GetPositionReconciliation (#1631 M8b)
// ============================================================================

func TestInvestmentAnalyticsService_GetPositionReconciliation_DelegatesAndReturns(t *testing.T) {
	repo := new(MockInvestmentAnalyticsRepository)
	svc := NewInvestmentAnalyticsService(repo)
	ctx := context.Background()
	userID := "user-reconcile"

	ticker := "AGG"
	expected := &domain.PositionReconciliationReport{
		Drifts: []domain.PositionReconciliationMember{
			{
				AccountID:       "acct-1",
				SecurityID:      "sec-1",
				PlaidSecurityID: "plaid-sec-1",
				TickerSymbol:    &ticker,
				SecurityName:    "iShares Core U.S. Aggregate Bond ETF",
				HoldingQuantity: 100.0,
				LedgerQuantity:  95.0,
				Delta:           5.0,
			},
		},
		SuppressedPreWindowCount: 3,
		TotalPositionsChecked:    4,
	}
	repo.On("PositionReconciliation", ctx, userID).Return(expected, nil)

	result, err := svc.GetPositionReconciliation(ctx, userID)

	require.NoError(t, err)
	assert.Equal(t, expected, result)
	repo.AssertExpectations(t)
}

func TestInvestmentAnalyticsService_GetPositionReconciliation_PropagatesError(t *testing.T) {
	repo := new(MockInvestmentAnalyticsRepository)
	svc := NewInvestmentAnalyticsService(repo)
	ctx := context.Background()

	repo.On("PositionReconciliation", ctx, "user-err").Return(nil, errors.New("timeout"))

	result, err := svc.GetPositionReconciliation(ctx, "user-err")

	assert.Nil(t, result)
	assert.ErrorContains(t, err, "GetPositionReconciliation")
}
