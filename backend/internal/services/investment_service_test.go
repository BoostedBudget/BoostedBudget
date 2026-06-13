package services

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
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
// DayChange is computed as totalValue − prior.TotalValue.
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
		UserID:     userID,
		TotalValue: 1100.0,
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
	assert.Len(t, pts, 3)
	assert.Equal(t, 1000.0, pts[0].TotalValue)
	assert.Equal(t, 1050.0, pts[1].TotalValue)
	assert.Equal(t, 1100.0, pts[2].TotalValue)
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
	assert.Empty(t, pts)
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
		Return(txns, nextCursor, nil)

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
		Return([]domain.InvestmentTransaction{}, (*domain.InvestmentTransactionCursor)(nil), nil)

	result, encodedCursor, err := svc.ListInvestmentTransactions(ctx, userID, filters, nil, 10)

	require.NoError(t, err)
	assert.Empty(t, result)
	assert.Nil(t, encodedCursor, "nil cursor → last page, no next cursor in response")
}
