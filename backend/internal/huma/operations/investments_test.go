package operations

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/danielgtaylor/huma/v2/humatest"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/huma/inputs"
	phclient "github.com/maxnussbaum/BoostedBudget/internal/integration/posthog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ============================================================================
// Mock services
// ============================================================================

type mockInvestmentService struct {
	holdings       []*domain.HoldingView
	holdingsErr    error
	summary        *domain.PortfolioSummary
	summaryErr     error
	history        []domain.PortfolioHistoryPoint
	historyErr     error
	txns           []domain.InvestmentTransaction
	txnsNextCursor *string
	txnsErr        error
}

func (m *mockInvestmentService) ListHoldings(_ context.Context, _ string, _ *string) ([]*domain.HoldingView, error) {
	return m.holdings, m.holdingsErr
}
func (m *mockInvestmentService) GetPortfolioSummary(_ context.Context, _ string) (*domain.PortfolioSummary, error) {
	return m.summary, m.summaryErr
}
func (m *mockInvestmentService) GetPortfolioHistory(_ context.Context, _ string, _, _ time.Time) ([]domain.PortfolioHistoryPoint, error) {
	return m.history, m.historyErr
}
func (m *mockInvestmentService) ListInvestmentTransactions(_ context.Context, _ string, _ domain.InvestmentTransactionFilters, _ *domain.InvestmentTransactionCursor, _ int) ([]domain.InvestmentTransaction, *string, error) {
	return m.txns, m.txnsNextCursor, m.txnsErr
}
func (m *mockInvestmentService) SetAnalytics(_ phclient.Analytics) {}

type mockInvestmentAnalyticsService struct {
	allocation    *domain.AllocationBreakdown
	allocationErr error
	topMovers     *domain.TopMovers
	topMoversErr  error
	gainLoss      *domain.GainLossSummary
	gainLossErr   error
	income        *domain.IncomeSummary
	incomeErr     error
}

func (m *mockInvestmentAnalyticsService) GetAllocationBreakdown(_ context.Context, _ string, _ domain.AllocationOptions) (*domain.AllocationBreakdown, error) {
	return m.allocation, m.allocationErr
}
func (m *mockInvestmentAnalyticsService) GetTopMovers(_ context.Context, _ string, _ int) (*domain.TopMovers, error) {
	return m.topMovers, m.topMoversErr
}
func (m *mockInvestmentAnalyticsService) GetGainLossSummary(_ context.Context, _ string) (*domain.GainLossSummary, error) {
	return m.gainLoss, m.gainLossErr
}
func (m *mockInvestmentAnalyticsService) GetIncomeSummary(_ context.Context, _ string, _, _ time.Time) (*domain.IncomeSummary, error) {
	return m.income, m.incomeErr
}

// ============================================================================
// GetHoldings
// ============================================================================

func TestInvestmentHandler_GetHoldings_Success(t *testing.T) {
	ticker := "AAPL"
	holdings := []*domain.HoldingView{
		{ID: "h1", UserID: "user-1", AccountID: "acct-1", TickerSymbol: &ticker},
		{ID: "h2", UserID: "user-1", AccountID: "acct-2"},
	}
	svc := &mockInvestmentService{holdings: holdings}
	analytSvc := &mockInvestmentAnalyticsService{}
	handler := NewInvestmentHandler(svc, analytSvc)

	input := &GetHoldingsInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-1"},
	}

	out, err := handler.GetHoldings(context.Background(), input)

	require.NoError(t, err)
	assert.Len(t, out.Body.Holdings, 2)
	assert.Equal(t, 2, out.Body.Count)
}

func TestInvestmentHandler_GetHoldings_Empty(t *testing.T) {
	svc := &mockInvestmentService{holdings: []*domain.HoldingView{}}
	handler := NewInvestmentHandler(svc, &mockInvestmentAnalyticsService{})

	input := &GetHoldingsInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-empty"},
	}

	out, err := handler.GetHoldings(context.Background(), input)

	require.NoError(t, err)
	assert.Empty(t, out.Body.Holdings)
	assert.Equal(t, 0, out.Body.Count)
}

func TestInvestmentHandler_GetHoldings_ServiceError(t *testing.T) {
	svc := &mockInvestmentService{holdingsErr: errors.New("db error")}
	handler := NewInvestmentHandler(svc, &mockInvestmentAnalyticsService{})

	input := &GetHoldingsInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-1"},
	}

	_, err := handler.GetHoldings(context.Background(), input)
	assert.Error(t, err)
}

func TestInvestmentHandler_GetHoldings_Unauthorized(t *testing.T) {
	_, api := humatest.New(t)
	handler := NewInvestmentHandler(&mockInvestmentService{}, &mockInvestmentAnalyticsService{})
	RegisterInvestmentRoutes(api, handler)

	resp := api.Get("/api/v1/investments/holdings")
	assert.Equal(t, http.StatusUnauthorized, resp.Code)
}

// ============================================================================
// GetPortfolioSummary
// ============================================================================

func TestInvestmentHandler_GetPortfolioSummary_Success(t *testing.T) {
	dc := 100.0
	summary := &domain.PortfolioSummary{
		TotalValue:     5000.0,
		TotalCostBasis: 4000.0,
		TotalGainLoss:  1000.0,
		DayChange:      &dc,
		HoldingsCount:  3,
	}
	svc := &mockInvestmentService{summary: summary}
	handler := NewInvestmentHandler(svc, &mockInvestmentAnalyticsService{})

	input := &GetPortfolioSummaryInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-1"},
	}

	out, err := handler.GetPortfolioSummary(context.Background(), input)

	require.NoError(t, err)
	assert.Equal(t, 5000.0, out.Body.TotalValue)
	require.NotNil(t, out.Body.DayChange)
	assert.Equal(t, 100.0, *out.Body.DayChange)
}

func TestInvestmentHandler_GetPortfolioSummary_NilDayChange(t *testing.T) {
	// DayChange=nil when no prior snapshot — must survive as nil, not be converted to 0.
	summary := &domain.PortfolioSummary{
		TotalValue:    1000.0,
		DayChange:     nil,
		HoldingsCount: 1,
	}
	svc := &mockInvestmentService{summary: summary}
	handler := NewInvestmentHandler(svc, &mockInvestmentAnalyticsService{})

	input := &GetPortfolioSummaryInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-1"},
	}

	out, err := handler.GetPortfolioSummary(context.Background(), input)

	require.NoError(t, err)
	assert.Nil(t, out.Body.DayChange, "DayChange must be nil (not 0) when no prior snapshot")
}

// ============================================================================
// GetPortfolioHistory
// ============================================================================

func TestInvestmentHandler_GetPortfolioHistory_Success(t *testing.T) {
	pts := []domain.PortfolioHistoryPoint{
		{Date: "2025-01-01", TotalValue: 1000.0},
		{Date: "2025-01-31", TotalValue: 1100.0},
	}
	svc := &mockInvestmentService{history: pts}
	handler := NewInvestmentHandler(svc, &mockInvestmentAnalyticsService{})

	input := &GetPortfolioHistoryInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-1"},
		StartDate:          "2025-01-01",
		EndDate:            "2025-01-31",
	}

	out, err := handler.GetPortfolioHistory(context.Background(), input)

	require.NoError(t, err)
	assert.Len(t, out.Body.History, 2)
	assert.Equal(t, 1000.0, out.Body.History[0].TotalValue)
	// #1631: date must be the plain calendar-day string, not a timestamp. Asserting equality
	// (not Contains) so a regression back to "2025-01-01T00:00:00Z" actually fails this test.
	assert.Equal(t, "2025-01-01", out.Body.History[0].Date)
}

func TestInvestmentHandler_GetPortfolioHistory_MissingDates(t *testing.T) {
	handler := NewInvestmentHandler(&mockInvestmentService{}, &mockInvestmentAnalyticsService{})

	input := &GetPortfolioHistoryInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-1"},
		// Missing start_date and end_date
	}

	_, err := handler.GetPortfolioHistory(context.Background(), input)
	assert.Error(t, err)
}

// ============================================================================
// GetInvestmentTransactions
// ============================================================================

func TestInvestmentHandler_GetInvestmentTransactions_FirstPage(t *testing.T) {
	next := "next-cursor-token"
	txns := []domain.InvestmentTransaction{
		{ID: "itxn-1", UserID: "user-1", Amount: 100.0},
	}
	svc := &mockInvestmentService{txns: txns, txnsNextCursor: &next}
	handler := NewInvestmentHandler(svc, &mockInvestmentAnalyticsService{})

	input := &GetInvestmentTransactionsInput{
		AuthenticatedInput:    inputs.AuthenticatedInput{UserID: "user-1"},
		CursorPaginationInput: inputs.CursorPaginationInput{Limit: 50},
	}

	out, err := handler.GetInvestmentTransactions(context.Background(), input)

	require.NoError(t, err)
	assert.Len(t, out.Body.Transactions, 1)
	require.NotNil(t, out.Body.NextCursor)
	assert.Equal(t, next, *out.Body.NextCursor)
}

func TestInvestmentHandler_GetInvestmentTransactions_LastPage(t *testing.T) {
	svc := &mockInvestmentService{txns: []domain.InvestmentTransaction{}, txnsNextCursor: nil}
	handler := NewInvestmentHandler(svc, &mockInvestmentAnalyticsService{})

	input := &GetInvestmentTransactionsInput{
		AuthenticatedInput:    inputs.AuthenticatedInput{UserID: "user-1"},
		CursorPaginationInput: inputs.CursorPaginationInput{Limit: 50},
	}

	out, err := handler.GetInvestmentTransactions(context.Background(), input)

	require.NoError(t, err)
	assert.Empty(t, out.Body.Transactions)
	assert.Nil(t, out.Body.NextCursor)
}

// TestInvestmentHandler_GetInvestmentTransactions_SurfacesSecurityFields pins issue #1305's
// acceptance criterion at the endpoint boundary: ticker_symbol/security_name set by the service
// must appear in the output body (guards against a future DTO mapping that drops them).
func TestInvestmentHandler_GetInvestmentTransactions_SurfacesSecurityFields(t *testing.T) {
	ticker, secName := "AAPL", "Apple Inc."
	txns := []domain.InvestmentTransaction{
		{ID: "itxn-1", UserID: "user-1", Amount: 100.0, TickerSymbol: &ticker, SecurityName: &secName},
	}
	svc := &mockInvestmentService{txns: txns}
	handler := NewInvestmentHandler(svc, &mockInvestmentAnalyticsService{})

	input := &GetInvestmentTransactionsInput{
		AuthenticatedInput:    inputs.AuthenticatedInput{UserID: "user-1"},
		CursorPaginationInput: inputs.CursorPaginationInput{Limit: 50},
	}

	out, err := handler.GetInvestmentTransactions(context.Background(), input)

	require.NoError(t, err)
	require.Len(t, out.Body.Transactions, 1)
	require.NotNil(t, out.Body.Transactions[0].TickerSymbol)
	assert.Equal(t, "AAPL", *out.Body.Transactions[0].TickerSymbol)
	require.NotNil(t, out.Body.Transactions[0].SecurityName)
	assert.Equal(t, "Apple Inc.", *out.Body.Transactions[0].SecurityName)
}

// ── Round-6 R6-3: account_id UUID validation → 400 ───────────────────────────

// TestInvestmentHandler_GetHoldings_InvalidAccountID verifies that a malformed
// account_id produces a 400 Bad Request, not a 500. — Round-6 R6-3 RED→GREEN
func TestInvestmentHandler_GetHoldings_InvalidAccountID(t *testing.T) {
	handler := NewInvestmentHandler(&mockInvestmentService{}, &mockInvestmentAnalyticsService{})

	input := &GetHoldingsInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-1"},
		AccountID:          "not-a-uuid",
	}

	_, err := handler.GetHoldings(context.Background(), input)

	require.Error(t, err, "malformed account_id must return an error")
	// Huma 400 errors implement the huma.StatusError interface
	var statusErr interface{ GetStatus() int }
	if errors.As(err, &statusErr) {
		assert.Equal(t, http.StatusBadRequest, statusErr.GetStatus(), "account_id validation must return 400 not 500")
	} else {
		t.Errorf("expected a huma status error, got %T: %v", err, err)
	}
}

// TestInvestmentHandler_GetInvestmentTransactions_InvalidAccountID verifies that a
// malformed account_id produces a 400 Bad Request, not a 500. — Round-6 R6-3 RED→GREEN
func TestInvestmentHandler_GetInvestmentTransactions_InvalidAccountID(t *testing.T) {
	handler := NewInvestmentHandler(&mockInvestmentService{}, &mockInvestmentAnalyticsService{})

	input := &GetInvestmentTransactionsInput{
		AuthenticatedInput:    inputs.AuthenticatedInput{UserID: "user-1"},
		CursorPaginationInput: inputs.CursorPaginationInput{Limit: 50},
		AccountID:             "not-a-uuid",
	}

	_, err := handler.GetInvestmentTransactions(context.Background(), input)

	require.Error(t, err, "malformed account_id must return an error")
	var statusErr interface{ GetStatus() int }
	if errors.As(err, &statusErr) {
		assert.Equal(t, http.StatusBadRequest, statusErr.GetStatus(), "account_id validation must return 400 not 500")
	} else {
		t.Errorf("expected a huma status error, got %T: %v", err, err)
	}
}

// TestInvestmentHandler_GetInvestmentTransactions_InvertedDateRange verifies that
// providing end_date before start_date returns 400 (parity with getPortfolioHistory
// and getInvestmentIncomeSummary). — Round-9 R9-2 RED→GREEN
func TestInvestmentHandler_GetInvestmentTransactions_InvertedDateRange(t *testing.T) {
	handler := NewInvestmentHandler(&mockInvestmentService{}, &mockInvestmentAnalyticsService{})

	input := &GetInvestmentTransactionsInput{
		AuthenticatedInput:    inputs.AuthenticatedInput{UserID: "user-1"},
		CursorPaginationInput: inputs.CursorPaginationInput{Limit: 50},
		StartDate:             "2025-02-01",
		EndDate:               "2025-01-01", // inverted: end < start
	}

	_, err := handler.GetInvestmentTransactions(context.Background(), input)

	require.Error(t, err, "inverted date range must return an error")
	var statusErr interface{ GetStatus() int }
	if errors.As(err, &statusErr) {
		assert.Equal(t, http.StatusBadRequest, statusErr.GetStatus(), "inverted date range must return 400 not 200")
	} else {
		t.Errorf("expected a huma status error, got %T: %v", err, err)
	}
}

// ============================================================================
// GetPortfolioAllocationBreakdown
// ============================================================================

func TestInvestmentHandler_GetPortfolioAllocationBreakdown_Success(t *testing.T) {
	alloc := &domain.AllocationBreakdown{
		TotalValue: 5000.0,
		Buckets: []domain.AllocationItem{
			{SecurityType: "stock", TotalValue: 3000.0, Percentage: 60.0},
			{SecurityType: "unknown", TotalValue: 500.0, Percentage: 10.0}, // NULL type → "unknown"
		},
	}
	analytSvc := &mockInvestmentAnalyticsService{allocation: alloc}
	handler := NewInvestmentHandler(&mockInvestmentService{}, analytSvc)

	input := &GetPortfolioAllocationBreakdownInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-1"},
	}

	out, err := handler.GetPortfolioAllocationBreakdown(context.Background(), input)

	require.NoError(t, err)
	assert.Equal(t, 5000.0, out.Body.TotalValue)
	assert.Len(t, out.Body.Buckets, 2)

	// Verify "unknown" bucket is present (covers NULL security.type).
	var hasUnknown bool
	for _, b := range out.Body.Buckets {
		if b.SecurityType == "unknown" {
			hasUnknown = true
		}
	}
	assert.True(t, hasUnknown, "allocation must include 'unknown' bucket for NULL security.type rows")
}

func TestInvestmentHandler_GetPortfolioAllocationBreakdown_Unauthorized(t *testing.T) {
	_, api := humatest.New(t)
	handler := NewInvestmentHandler(&mockInvestmentService{}, &mockInvestmentAnalyticsService{})
	RegisterInvestmentRoutes(api, handler)

	resp := api.Get("/api/v1/investments/portfolio/allocation")
	assert.Equal(t, http.StatusUnauthorized, resp.Code)
}

// ============================================================================
// GetTopMovers
// ============================================================================

func TestInvestmentHandler_GetTopMovers_Success(t *testing.T) {
	dc := 120.0
	movers := &domain.TopMovers{
		Gainers: []domain.TopMoverItem{{HoldingID: "h1", DayChange: &dc}},
		Losers:  []domain.TopMoverItem{},
	}
	analytSvc := &mockInvestmentAnalyticsService{topMovers: movers}
	handler := NewInvestmentHandler(&mockInvestmentService{}, analytSvc)

	input := &GetTopMoversInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-1"},
		Limit:              5,
	}

	out, err := handler.GetTopMovers(context.Background(), input)

	require.NoError(t, err)
	assert.Len(t, out.Body.Gainers, 1)
	assert.Empty(t, out.Body.Losers)
}

// ============================================================================
// GetInvestmentGainLossSummary
// ============================================================================

func TestInvestmentHandler_GetInvestmentGainLossSummary_UnrealizedOnly(t *testing.T) {
	pct := 25.0
	gl := &domain.GainLossSummary{
		TotalUnrealizedGainLoss:    1000.0,
		TotalUnrealizedGainLossPct: &pct,
		TotalCostBasis:             4000.0,
		TotalValue:                 5000.0,
		RealizedGainLossSupported:  false, // always false
	}
	analytSvc := &mockInvestmentAnalyticsService{gainLoss: gl}
	handler := NewInvestmentHandler(&mockInvestmentService{}, analytSvc)

	input := &GetInvestmentGainLossSummaryInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-1"},
	}

	out, err := handler.GetInvestmentGainLossSummary(context.Background(), input)

	require.NoError(t, err)
	assert.False(t, out.Body.RealizedGainLossSupported,
		"realized_gain_loss_supported MUST be false in response — Plaid provides no per-lot cost basis")
	assert.Equal(t, 1000.0, out.Body.TotalUnrealizedGainLoss)
}

// ============================================================================
// GetInvestmentIncomeSummary
// ============================================================================

func TestInvestmentHandler_GetInvestmentIncomeSummary_Success(t *testing.T) {
	start := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2025, 3, 31, 0, 0, 0, 0, time.UTC)
	income := &domain.IncomeSummary{
		TotalIncome: 350.0,
		StartDate:   start,
		EndDate:     end,
		ByType: []domain.IncomeByType{
			{IncomeType: "dividend", TotalAmount: 250.0, Count: 5},
			{IncomeType: "dividend_reinvestment", TotalAmount: 100.0, Count: 2},
		},
	}
	analytSvc := &mockInvestmentAnalyticsService{income: income}
	handler := NewInvestmentHandler(&mockInvestmentService{}, analytSvc)

	input := &GetInvestmentIncomeSummaryInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-1"},
		StartDate:          "2025-01-01",
		EndDate:            "2025-03-31",
	}

	out, err := handler.GetInvestmentIncomeSummary(context.Background(), input)

	require.NoError(t, err)
	assert.Equal(t, 350.0, out.Body.TotalIncome)
	assert.Len(t, out.Body.ByType, 2)
}

func TestInvestmentHandler_GetInvestmentIncomeSummary_MissingDates(t *testing.T) {
	handler := NewInvestmentHandler(&mockInvestmentService{}, &mockInvestmentAnalyticsService{})

	input := &GetInvestmentIncomeSummaryInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-1"},
	}

	_, err := handler.GetInvestmentIncomeSummary(context.Background(), input)
	assert.Error(t, err)
}

// ============================================================================
// Route registration: verify all 8 ops are registered
// ============================================================================

func TestInvestmentRoutes_AreRegistered(t *testing.T) {
	_, api := humatest.New(t)
	handler := NewInvestmentHandler(&mockInvestmentService{}, &mockInvestmentAnalyticsService{})
	RegisterInvestmentRoutes(api, handler)

	// All 8 routes should return 401 (not 404) — proving they're registered.
	routes := []string{
		"/api/v1/investments/holdings",
		"/api/v1/investments/portfolio/summary",
		"/api/v1/investments/portfolio/history?start_date=2025-01-01&end_date=2025-01-31",
		"/api/v1/investments/investment-transactions",
		"/api/v1/investments/portfolio/allocation",
		"/api/v1/investments/portfolio/top-movers",
		"/api/v1/investments/portfolio/gain-loss",
		"/api/v1/investments/portfolio/income?start_date=2025-01-01&end_date=2025-01-31",
	}
	for _, route := range routes {
		resp := api.Get(route)
		assert.Equal(t, http.StatusUnauthorized, resp.Code, "route %s should be 401, not 404", route)
	}
}
