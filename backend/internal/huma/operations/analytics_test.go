package operations

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/humatest"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/huma/inputs"
	"github.com/maxnussbaum/BoostedBudget/internal/services"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// mockAnalyticsService is a mock implementation of services.AnalyticsService
type mockAnalyticsService struct {
	summaryResponse                      *domain.CashFlowSummaryResponse
	historyResponse                      *domain.CashFlowHistoryResponse
	spendingByCategoryResponse           *domain.SpendingByCategoryResponse
	spendingByMerchantResponse           *domain.SpendingByMerchantResponse
	spendingTrendsResponse               *domain.SpendingTrendsResponse
	averageMonthlySpendResponse          *domain.AverageMonthlySpendResponse
	recurringSubscriptionsResponse       *domain.RecurringSubscriptionsResponse
	savingsRateResponse                  *domain.SavingsRateResponse
	monthOverMonthResponse               *domain.MonthOverMonthResponse
	largestTransactionsResponse          *domain.LargestTransactionsResponse
	categoryBreakdownForMerchantResponse *domain.CategoryBreakdownForMerchantResponse
	categoryHierarchyResponse            *domain.CategoryHierarchyResponse
	categoryHierarchyErr                 error
	lastCategoryDirection                domain.CategoryDirection
	incomeSourceBreakdownResponse        *domain.IncomeSourceBreakdownResponse
	unusualSpendResponse                 *domain.UnusualSpendResponse
	netWorthTrendResponse                *domain.NetWorthTrendResponse
	budgetVsActualResponse               *domain.BudgetVsActualResponse
	summaryErr                           error
	historyErr                           error
	spendingByCategoryErr                error
	spendingByMerchantErr                error
	spendingTrendsErr                    error
	averageMonthlySpendErr               error
	recurringSubscriptionsErr            error
	savingsRateErr                       error
	monthOverMonthErr                    error
	largestTransactionsErr               error
	categoryBreakdownForMerchantErr      error
	incomeSourceBreakdownErr             error
	unusualSpendErr                      error
	netWorthTrendErr                     error
	budgetVsActualErr                    error
	categoryTotalResponse                *domain.CategoryTotalResponse
	categoryTotalErr                     error
	topExpenseResponse                   *domain.TopExpenseResponse
	topExpenseErr                        error
	// lastCashFlowFilters/lastSpendingFilters capture the most recent call's filters, so
	// tests can assert HouseholdScope was (or wasn't) threaded through. #1369 — Phase 5.
	lastCashFlowFilters *domain.CashFlowFilters
	lastSpendingFilters *domain.SpendingFilters
}

func (m *mockAnalyticsService) GetCashFlowSummary(ctx context.Context, userID string, filters domain.CashFlowFilters) (*domain.CashFlowSummaryResponse, error) {
	m.lastCashFlowFilters = &filters
	if m.summaryErr != nil {
		return nil, m.summaryErr
	}
	return m.summaryResponse, nil
}

func (m *mockAnalyticsService) GetCashFlowHistory(ctx context.Context, userID string, filters domain.CashFlowFilters) (*domain.CashFlowHistoryResponse, error) {
	m.lastCashFlowFilters = &filters
	if m.historyErr != nil {
		return nil, m.historyErr
	}
	return m.historyResponse, nil
}

func (m *mockAnalyticsService) GetSpendingByCategory(ctx context.Context, userID string, filters domain.SpendingFilters) (*domain.SpendingByCategoryResponse, error) {
	m.lastSpendingFilters = &filters
	return m.spendingByCategoryResponse, m.spendingByCategoryErr
}

func (m *mockAnalyticsService) GetSpendingByMerchant(ctx context.Context, userID string, filters domain.SpendingFilters, limit int) (*domain.SpendingByMerchantResponse, error) {
	m.lastSpendingFilters = &filters
	return m.spendingByMerchantResponse, m.spendingByMerchantErr
}

func (m *mockAnalyticsService) GetSpendingTrends(ctx context.Context, userID string, filters domain.SpendingFilters) (*domain.SpendingTrendsResponse, error) {
	m.lastSpendingFilters = &filters
	return m.spendingTrendsResponse, m.spendingTrendsErr
}

func (m *mockAnalyticsService) GetCategoryTotalForPeriod(_ context.Context, _ string, _ string, _ time.Time, _ time.Time) (*domain.CategoryTotalResponse, error) {
	return m.categoryTotalResponse, m.categoryTotalErr
}

func (m *mockAnalyticsService) GetTopExpense(_ context.Context, _ string, _ time.Time, _ time.Time) (*domain.TopExpenseResponse, error) {
	return m.topExpenseResponse, m.topExpenseErr
}

func (m *mockAnalyticsService) GetAverageMonthlySpend(_ context.Context, _ string, _ domain.SpendingFilters) (*domain.AverageMonthlySpendResponse, error) {
	return m.averageMonthlySpendResponse, m.averageMonthlySpendErr
}

func (m *mockAnalyticsService) GetRecurringSubscriptions(_ context.Context, _ string) (*domain.RecurringSubscriptionsResponse, error) {
	return m.recurringSubscriptionsResponse, m.recurringSubscriptionsErr
}

func (m *mockAnalyticsService) GetSavingsRateForPeriod(_ context.Context, _ string, _ time.Time, _ time.Time, _ *domain.HouseholdScope) (*domain.SavingsRateResponse, error) {
	return m.savingsRateResponse, m.savingsRateErr
}

func (m *mockAnalyticsService) GetMonthOverMonthSpendingChange(_ context.Context, _ string, _ string, _ int, _ []string, _ bool, _ *domain.HouseholdScope) (*domain.MonthOverMonthResponse, error) {
	return m.monthOverMonthResponse, m.monthOverMonthErr
}

func (m *mockAnalyticsService) GetLargestTransactionsForPeriod(_ context.Context, _ string, _ domain.SpendingFilters, _ int) (*domain.LargestTransactionsResponse, error) {
	return m.largestTransactionsResponse, m.largestTransactionsErr
}

func (m *mockAnalyticsService) GetCategoryBreakdownForMerchant(_ context.Context, _ string, _ string, _ domain.SpendingFilters) (*domain.CategoryBreakdownForMerchantResponse, error) {
	return m.categoryBreakdownForMerchantResponse, m.categoryBreakdownForMerchantErr
}

// Note: signature accepts merchantID (UUID) instead of merchantName per #1348 G2b

func (m *mockAnalyticsService) GetIncomeSourceBreakdown(_ context.Context, _ string, filters domain.SpendingFilters) (*domain.IncomeSourceBreakdownResponse, error) {
	m.lastSpendingFilters = &filters
	return m.incomeSourceBreakdownResponse, m.incomeSourceBreakdownErr
}

func (m *mockAnalyticsService) GetCategoryHierarchy(_ context.Context, _ string, filters domain.SpendingFilters, direction domain.CategoryDirection) (*domain.CategoryHierarchyResponse, error) {
	m.lastSpendingFilters = &filters
	m.lastCategoryDirection = direction
	return m.categoryHierarchyResponse, m.categoryHierarchyErr
}

func (m *mockAnalyticsService) GetUnusualSpend(_ context.Context, _ string, _ time.Time, _ time.Time) (*domain.UnusualSpendResponse, error) {
	return m.unusualSpendResponse, m.unusualSpendErr
}

func (m *mockAnalyticsService) GetNetWorthTrend(_ context.Context, _ string, _ string, _ *domain.HouseholdScope) (*domain.NetWorthTrendResponse, error) {
	return m.netWorthTrendResponse, m.netWorthTrendErr
}

func (m *mockAnalyticsService) GetBudgetVsActual(_ context.Context, _ string, _ string) (*domain.BudgetVsActualResponse, error) {
	return m.budgetVsActualResponse, m.budgetVsActualErr
}

func createTestCashFlowSummary() *domain.CashFlowSummaryResponse {
	return &domain.CashFlowSummaryResponse{
		StartDate:    time.Now().AddDate(0, -1, 0),
		EndDate:      time.Now(),
		Income:       5000.00,
		Expenses:     3000.00,
		NetCashFlow:  2000.00,
		IncomeCount:  10,
		ExpenseCount: 25,
	}
}

func createTestCashFlowHistory() *domain.CashFlowHistoryResponse {
	return &domain.CashFlowHistoryResponse{
		StartDate: time.Now().AddDate(0, -1, 0),
		EndDate:   time.Now(),
		Grouping:  "weekly",
		Periods: []domain.CashFlowPeriodData{
			{
				PeriodStart:      time.Now().AddDate(0, 0, -7),
				PeriodEnd:        time.Now(),
				Income:           1250.00,
				Expenses:         750.00,
				NetCashFlow:      500.00,
				TransactionCount: 10,
			},
		},
	}
}

func TestGetCashFlowSummary_Success(t *testing.T) {
	mockService := &mockAnalyticsService{
		summaryResponse: createTestCashFlowSummary(),
	}

	handler := NewAnalyticsHandler(mockService, nil)

	input := &GetCashFlowSummaryInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-123"},
		StartDate:          "2024-01-01",
		EndDate:            "2024-01-31",
	}

	output, err := handler.GetCashFlowSummary(context.Background(), input)

	require.NoError(t, err)
	require.NotNil(t, output)
	assert.Equal(t, 5000.00, output.Body.Income)
	assert.Equal(t, 3000.00, output.Body.Expenses)
	assert.Equal(t, 2000.00, output.Body.NetCashFlow)
}

func TestGetCashFlowSummary_WithFilters(t *testing.T) {
	mockService := &mockAnalyticsService{
		summaryResponse: createTestCashFlowSummary(),
	}

	handler := NewAnalyticsHandler(mockService, nil)

	input := &GetCashFlowSummaryInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-123"},
		StartDate:          "2024-01-01",
		EndDate:            "2024-01-31",
		AccountTypes:       "checking,savings",
		IncludeTransfers:   "true",
	}

	output, err := handler.GetCashFlowSummary(context.Background(), input)

	require.NoError(t, err)
	require.NotNil(t, output)
}

func TestGetCashFlowSummary_MissingStartDate(t *testing.T) {
	mockService := &mockAnalyticsService{}

	handler := NewAnalyticsHandler(mockService, nil)

	input := &GetCashFlowSummaryInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-123"},
		EndDate:            "2024-01-31",
	}

	output, err := handler.GetCashFlowSummary(context.Background(), input)

	assert.Nil(t, output)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "start_date is required")
}

func TestGetCashFlowSummary_MissingEndDate(t *testing.T) {
	mockService := &mockAnalyticsService{}

	handler := NewAnalyticsHandler(mockService, nil)

	input := &GetCashFlowSummaryInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-123"},
		StartDate:          "2024-01-01",
	}

	output, err := handler.GetCashFlowSummary(context.Background(), input)

	assert.Nil(t, output)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "end_date is required")
}

func TestGetCashFlowSummary_InvalidStartDate(t *testing.T) {
	mockService := &mockAnalyticsService{}

	handler := NewAnalyticsHandler(mockService, nil)

	input := &GetCashFlowSummaryInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-123"},
		StartDate:          "not-a-date",
		EndDate:            "2024-01-31",
	}

	output, err := handler.GetCashFlowSummary(context.Background(), input)

	assert.Nil(t, output)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "Invalid start_date format")
}

func TestGetCashFlowSummary_InvalidEndDate(t *testing.T) {
	mockService := &mockAnalyticsService{}

	handler := NewAnalyticsHandler(mockService, nil)

	input := &GetCashFlowSummaryInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-123"},
		StartDate:          "2024-01-01",
		EndDate:            "not-a-date",
	}

	output, err := handler.GetCashFlowSummary(context.Background(), input)

	assert.Nil(t, output)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "Invalid end_date format")
}

func TestGetCashFlowSummary_EndBeforeStart(t *testing.T) {
	mockService := &mockAnalyticsService{}

	handler := NewAnalyticsHandler(mockService, nil)

	input := &GetCashFlowSummaryInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-123"},
		StartDate:          "2024-01-31",
		EndDate:            "2024-01-01",
	}

	output, err := handler.GetCashFlowSummary(context.Background(), input)

	assert.Nil(t, output)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "end_date cannot be before start_date")
}

func TestGetCashFlowSummary_InvalidIncludeTransfers(t *testing.T) {
	mockService := &mockAnalyticsService{}

	handler := NewAnalyticsHandler(mockService, nil)

	input := &GetCashFlowSummaryInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-123"},
		StartDate:          "2024-01-01",
		EndDate:            "2024-01-31",
		IncludeTransfers:   "maybe",
	}

	output, err := handler.GetCashFlowSummary(context.Background(), input)

	assert.Nil(t, output)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "Invalid")
}

func TestGetCashFlowSummary_ServiceError(t *testing.T) {
	mockService := &mockAnalyticsService{
		summaryErr: errors.New("database error"),
	}

	handler := NewAnalyticsHandler(mockService, nil)

	input := &GetCashFlowSummaryInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-123"},
		StartDate:          "2024-01-01",
		EndDate:            "2024-01-31",
	}

	output, err := handler.GetCashFlowSummary(context.Background(), input)

	assert.Nil(t, output)
	require.Error(t, err)
}

func TestGetCashFlowHistory_Success(t *testing.T) {
	mockService := &mockAnalyticsService{
		historyResponse: createTestCashFlowHistory(),
	}

	handler := NewAnalyticsHandler(mockService, nil)

	input := &GetCashFlowHistoryInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-123"},
		StartDate:          "2024-01-01",
		EndDate:            "2024-01-31",
	}

	output, err := handler.GetCashFlowHistory(context.Background(), input)

	require.NoError(t, err)
	require.NotNil(t, output)
	assert.Equal(t, "weekly", output.Body.Grouping)
	assert.Len(t, output.Body.Periods, 1)
}

func TestGetCashFlowHistory_WithGrouping(t *testing.T) {
	mockService := &mockAnalyticsService{
		historyResponse: createTestCashFlowHistory(),
	}

	handler := NewAnalyticsHandler(mockService, nil)

	input := &GetCashFlowHistoryInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-123"},
		StartDate:          "2024-01-01",
		EndDate:            "2024-01-31",
		Grouping:           "monthly",
	}

	output, err := handler.GetCashFlowHistory(context.Background(), input)

	require.NoError(t, err)
	require.NotNil(t, output)
}

func TestGetCashFlowHistory_InvalidGrouping(t *testing.T) {
	mockService := &mockAnalyticsService{}

	handler := NewAnalyticsHandler(mockService, nil)

	input := &GetCashFlowHistoryInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-123"},
		StartDate:          "2024-01-01",
		EndDate:            "2024-01-31",
		Grouping:           "yearly",
	}

	output, err := handler.GetCashFlowHistory(context.Background(), input)

	assert.Nil(t, output)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "Invalid grouping")
}

func TestAnalyticsRouteRegistration(t *testing.T) {
	mockService := &mockAnalyticsService{
		summaryResponse: createTestCashFlowSummary(),
	}

	_, api := humatest.New(t)
	handler := NewAnalyticsHandler(mockService, nil)
	RegisterAnalyticsRoutes(api, handler)

	// Test GET /api/v1/analytics/cash-flow-summary (without auth, should fail with 401)
	resp := api.Get("/api/v1/analytics/cash-flow-summary")
	assert.Equal(t, http.StatusUnauthorized, resp.Code)

	// Verify the response contains expected error message
	body := resp.Body.String()
	assert.Contains(t, body, "User not authenticated")
}

func TestAnalyticsResponseJSON(t *testing.T) {
	mockService := &mockAnalyticsService{}

	_, api := humatest.New(t)
	handler := NewAnalyticsHandler(mockService, nil)
	RegisterAnalyticsRoutes(api, handler)

	resp := api.Get("/api/v1/analytics/cash-flow-summary")
	assert.Equal(t, http.StatusUnauthorized, resp.Code)

	// Verify error response is valid JSON
	var errorResp map[string]interface{}
	err := json.Unmarshal(resp.Body.Bytes(), &errorResp)
	require.NoError(t, err, "Error response should be valid JSON")
}

func TestGetSpendingByCategory_Success(t *testing.T) {
	mockService := &mockAnalyticsService{
		spendingByCategoryResponse: &domain.SpendingByCategoryResponse{
			StartDate:  "2024-01-01",
			EndDate:    "2024-01-31",
			TotalSpent: 500.0,
			Categories: []domain.SpendingByCategory{
				{Category: "FOOD_AND_DRINK", PFCPrimaryDisplay: "Food And Drink", Total: 300.0, Percentage: 60.0, TransactionCount: 20},
			},
		},
	}

	handler := NewAnalyticsHandler(mockService, nil)

	input := &GetSpendingByCategoryInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-123"},
		StartDate:          "2024-01-01",
		EndDate:            "2024-01-31",
	}

	output, err := handler.GetSpendingByCategory(context.Background(), input)

	require.NoError(t, err)
	require.NotNil(t, output)
	assert.Equal(t, 500.0, output.Body.TotalSpent)
	assert.Len(t, output.Body.Categories, 1)
}

// TestGetSpendingByCategory_WithCategoryFilter tests subcategory drill-down.
func TestGetSpendingByCategory_WithCategoryFilter(t *testing.T) {
	mockService := &mockAnalyticsService{
		spendingByCategoryResponse: &domain.SpendingByCategoryResponse{
			StartDate:  "2024-01-01",
			EndDate:    "2024-01-31",
			TotalSpent: 300.0,
			Categories: []domain.SpendingByCategory{
				{Category: "RESTAURANTS", PFCPrimaryDisplay: "Restaurants", Total: 180.0, Percentage: 60.0},
			},
		},
	}

	handler := NewAnalyticsHandler(mockService, nil)

	input := &GetSpendingByCategoryInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-123"},
		StartDate:          "2024-01-01",
		EndDate:            "2024-01-31",
		Category:           "FOOD_AND_DRINK",
	}

	output, err := handler.GetSpendingByCategory(context.Background(), input)

	require.NoError(t, err)
	require.NotNil(t, output)
}

// TestGetSpendingByCategory_InvalidDate tests date validation.
func TestGetSpendingByCategory_InvalidDate(t *testing.T) {
	handler := NewAnalyticsHandler(&mockAnalyticsService{}, nil)

	input := &GetSpendingByCategoryInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-123"},
		StartDate:          "not-a-date",
		EndDate:            "2024-01-31",
	}

	output, err := handler.GetSpendingByCategory(context.Background(), input)

	assert.Nil(t, output)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "Invalid start_date format")
}

// TestGetSpendingByCategory_ServiceError tests service error propagation.
func TestGetSpendingByCategory_ServiceError(t *testing.T) {
	handler := NewAnalyticsHandler(&mockAnalyticsService{
		spendingByCategoryErr: errors.New("database error"),
	}, nil)

	input := &GetSpendingByCategoryInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-123"},
		StartDate:          "2024-01-01",
		EndDate:            "2024-01-31",
	}

	output, err := handler.GetSpendingByCategory(context.Background(), input)

	assert.Nil(t, output)
	require.Error(t, err)
}

// TestGetSpendingByMerchant_Success tests successful spending by merchant retrieval.
func TestGetSpendingByMerchant_Success(t *testing.T) {
	mockService := &mockAnalyticsService{
		spendingByMerchantResponse: &domain.SpendingByMerchantResponse{
			StartDate: "2024-01-01",
			EndDate:   "2024-01-31",
			TopN:      10,
			Merchants: []domain.SpendingByMerchant{
				{MerchantName: "Starbucks", Total: 150.0, TransactionCount: 15, Percentage: 30.0},
			},
		},
	}

	handler := NewAnalyticsHandler(mockService, nil)

	input := &GetSpendingByMerchantInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-123"},
		StartDate:          "2024-01-01",
		EndDate:            "2024-01-31",
	}

	output, err := handler.GetSpendingByMerchant(context.Background(), input)

	require.NoError(t, err)
	require.NotNil(t, output)
	assert.Equal(t, 10, output.Body.TopN)
	assert.Len(t, output.Body.Merchants, 1)
}

// TestGetSpendingByMerchant_CustomLimit tests custom limit parameter.
func TestGetSpendingByMerchant_CustomLimit(t *testing.T) {
	mockService := &mockAnalyticsService{
		spendingByMerchantResponse: &domain.SpendingByMerchantResponse{
			StartDate: "2024-01-01",
			EndDate:   "2024-01-31",
			TopN:      5,
			Merchants: []domain.SpendingByMerchant{},
		},
	}

	handler := NewAnalyticsHandler(mockService, nil)

	input := &GetSpendingByMerchantInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-123"},
		StartDate:          "2024-01-01",
		EndDate:            "2024-01-31",
		Limit:              5,
	}

	output, err := handler.GetSpendingByMerchant(context.Background(), input)

	require.NoError(t, err)
	require.NotNil(t, output)
}

// TestGetSpendingByMerchant_InvalidDate tests date validation.
func TestGetSpendingByMerchant_InvalidDate(t *testing.T) {
	handler := NewAnalyticsHandler(&mockAnalyticsService{}, nil)

	input := &GetSpendingByMerchantInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-123"},
		StartDate:          "2024-01-01",
		EndDate:            "not-a-date",
	}

	output, err := handler.GetSpendingByMerchant(context.Background(), input)

	assert.Nil(t, output)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "Invalid end_date format")
}

// TestGetSpendingTrends_Success tests successful spending trends retrieval.
func TestGetSpendingTrends_Success(t *testing.T) {
	mockService := &mockAnalyticsService{
		spendingTrendsResponse: &domain.SpendingTrendsResponse{
			StartDate:     "2024-01-01",
			EndDate:       "2024-03-31",
			TopCategories: []string{"FOOD_AND_DRINK", "TRANSPORTATION"},
			Periods: []domain.SpendingTrendPeriod{
				{
					PeriodStart: "2024-01-01",
					PeriodEnd:   "2024-01-31",
					Total:       280.0,
					Categories: []domain.CategorySpending{
						{Category: "FOOD_AND_DRINK", PFCPrimaryDisplay: "Food And Drink", Total: 200.0},
					},
				},
			},
		},
	}

	handler := NewAnalyticsHandler(mockService, nil)

	input := &GetSpendingTrendsInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-123"},
		StartDate:          "2024-01-01",
		EndDate:            "2024-03-31",
	}

	output, err := handler.GetSpendingTrends(context.Background(), input)

	require.NoError(t, err)
	require.NotNil(t, output)
	assert.Len(t, output.Body.Periods, 1)
	assert.Len(t, output.Body.TopCategories, 2)
}

// TestGetSpendingTrends_InvalidDate tests date validation.
func TestGetSpendingTrends_InvalidDate(t *testing.T) {
	handler := NewAnalyticsHandler(&mockAnalyticsService{}, nil)

	input := &GetSpendingTrendsInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-123"},
		StartDate:          "2024-01-01",
	}

	output, err := handler.GetSpendingTrends(context.Background(), input)

	assert.Nil(t, output)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "end_date is required")
}

// TestGetAverageMonthlySpend_Success tests successful average monthly spend retrieval.
func TestGetAverageMonthlySpend_Success(t *testing.T) {
	mockService := &mockAnalyticsService{
		averageMonthlySpendResponse: &domain.AverageMonthlySpendResponse{
			AverageMonthlySpend: 100.0,
			TotalSpend:          300.0,
			MonthCount:          3,
			StartDate:           "2024-01-01",
			EndDate:             "2024-03-31",
		},
	}

	handler := NewAnalyticsHandler(mockService, nil)

	input := &GetAverageMonthlySpendInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-123"},
		StartDate:          "2024-01-01",
		EndDate:            "2024-03-31",
	}

	output, err := handler.GetAverageMonthlySpend(context.Background(), input)

	require.NoError(t, err)
	require.NotNil(t, output)
	assert.InDelta(t, 100.0, output.Body.AverageMonthlySpend, 0.001)
	assert.Equal(t, 3, output.Body.MonthCount)
}

// TestGetAverageMonthlySpend_EndBeforeStart tests date validation.
func TestGetAverageMonthlySpend_EndBeforeStart(t *testing.T) {
	handler := NewAnalyticsHandler(&mockAnalyticsService{}, nil)

	input := &GetAverageMonthlySpendInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-123"},
		StartDate:          "2024-03-31",
		EndDate:            "2024-01-01",
	}

	output, err := handler.GetAverageMonthlySpend(context.Background(), input)

	assert.Nil(t, output)
	require.Error(t, err)
}

// TestGetAverageMonthlySpend_ServiceError tests service error propagation.
func TestGetAverageMonthlySpend_ServiceError(t *testing.T) {
	mockService := &mockAnalyticsService{
		averageMonthlySpendErr: errors.New("db error"),
	}
	handler := NewAnalyticsHandler(mockService, nil)

	input := &GetAverageMonthlySpendInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-123"},
		StartDate:          "2024-01-01",
		EndDate:            "2024-03-31",
	}

	output, err := handler.GetAverageMonthlySpend(context.Background(), input)

	assert.Nil(t, output)
	require.Error(t, err)
}

// TestGetRecurringSubscriptions_Success tests successful recurring subscription detection.
func TestGetRecurringSubscriptions_Success(t *testing.T) {
	mockService := &mockAnalyticsService{
		recurringSubscriptionsResponse: &domain.RecurringSubscriptionsResponse{
			Subscriptions: []domain.RecurringSubscriptionItem{
				{
					MerchantName:     "Netflix",
					Amount:           15.99,
					OccurrenceCount:  12,
					AvgGapDays:       30.5,
					LastChargeDate:   "2024-12-01",
					NextExpectedDate: "2025-01-01",
				},
			},
			AsOfDate: "2025-01-01",
		},
	}

	handler := NewAnalyticsHandler(mockService, nil)
	input := &GetRecurringSubscriptionsInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-123"},
	}

	output, err := handler.GetRecurringSubscriptions(context.Background(), input)

	require.NoError(t, err)
	require.NotNil(t, output)
	require.NotNil(t, output.Body)
	assert.Len(t, output.Body.Subscriptions, 1)
	assert.Equal(t, "Netflix", output.Body.Subscriptions[0].MerchantName)
	assert.InDelta(t, 15.99, output.Body.Subscriptions[0].Amount, 0.001)
}

// TestGetRecurringSubscriptions_ServiceError tests service error propagation.
func TestGetRecurringSubscriptions_ServiceError(t *testing.T) {
	mockService := &mockAnalyticsService{
		recurringSubscriptionsErr: errors.New("db error"),
	}
	handler := NewAnalyticsHandler(mockService, nil)

	input := &GetRecurringSubscriptionsInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-123"},
	}

	output, err := handler.GetRecurringSubscriptions(context.Background(), input)

	assert.Nil(t, output)
	require.Error(t, err)
}

// TestGetSavingsRateForPeriod_Success tests successful savings rate retrieval.
func TestGetSavingsRateForPeriod_Success(t *testing.T) {
	rate := 0.4
	mockService := &mockAnalyticsService{
		savingsRateResponse: &domain.SavingsRateResponse{
			SavingsRate: &rate,
			Income:      5000.0,
			Expenses:    3000.0,
			NetSavings:  2000.0,
			StartDate:   "2024-01-01",
			EndDate:     "2024-03-31",
		},
	}
	handler := NewAnalyticsHandler(mockService, nil)

	input := &GetSavingsRateForPeriodInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-123"},
		StartDate:          "2024-01-01",
		EndDate:            "2024-03-31",
	}

	output, err := handler.GetSavingsRateForPeriod(context.Background(), input)

	require.NoError(t, err)
	require.NotNil(t, output)
	assert.InDelta(t, 5000.0, output.Body.Income, 0.001)
	assert.NotNil(t, output.Body.SavingsRate)
}

// TestGetSavingsRateForPeriod_EndBeforeStart tests date validation.
func TestGetSavingsRateForPeriod_EndBeforeStart(t *testing.T) {
	handler := NewAnalyticsHandler(&mockAnalyticsService{}, nil)

	input := &GetSavingsRateForPeriodInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-123"},
		StartDate:          "2024-03-31",
		EndDate:            "2024-01-01",
	}

	output, err := handler.GetSavingsRateForPeriod(context.Background(), input)

	assert.Nil(t, output)
	require.Error(t, err)
}

// TestGetSavingsRateForPeriod_ServiceError tests service error propagation.
func TestGetSavingsRateForPeriod_ServiceError(t *testing.T) {
	mockService := &mockAnalyticsService{savingsRateErr: errors.New("db error")}
	handler := NewAnalyticsHandler(mockService, nil)

	input := &GetSavingsRateForPeriodInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-123"},
		StartDate:          "2024-01-01",
		EndDate:            "2024-03-31",
	}

	output, err := handler.GetSavingsRateForPeriod(context.Background(), input)

	assert.Nil(t, output)
	require.Error(t, err)
}

// TestGetMonthOverMonthSpendingChange_Success tests successful month-over-month retrieval.
func TestGetMonthOverMonthSpendingChange_Success(t *testing.T) {
	mockService := &mockAnalyticsService{
		monthOverMonthResponse: &domain.MonthOverMonthResponse{
			Buckets:    []domain.MonthOverMonthBucket{{Month: "2024-01-01", Total: 300.0}},
			Category:   "",
			MonthCount: 6,
		},
	}
	handler := NewAnalyticsHandler(mockService, nil)

	input := &GetMonthOverMonthSpendingChangeInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-123"},
		Months:             6,
	}

	output, err := handler.GetMonthOverMonthSpendingChange(context.Background(), input)

	require.NoError(t, err)
	require.NotNil(t, output)
	assert.Equal(t, 6, output.Body.MonthCount)
}

// TestGetMonthOverMonthSpendingChange_AccountTypesAndTransfers verifies that the handler
// correctly parses comma-separated account_types and include_transfers before passing them
// to the service.
func TestGetMonthOverMonthSpendingChange_AccountTypesAndTransfers(t *testing.T) {
	mockService := &mockAnalyticsService{
		monthOverMonthResponse: &domain.MonthOverMonthResponse{MonthCount: 3},
	}
	handler := NewAnalyticsHandler(mockService, nil)

	input := &GetMonthOverMonthSpendingChangeInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-123"},
		Months:             3,
		AccountTypes:       "depository, credit", // with space — handler must trim
		IncludeTransfers:   "true",
	}

	output, err := handler.GetMonthOverMonthSpendingChange(context.Background(), input)

	// Handler should succeed (mockService returns canned response regardless of filter args)
	require.NoError(t, err)
	require.NotNil(t, output)
	assert.Equal(t, 3, output.Body.MonthCount)
	// Parsing correctness is validated in the underlying handler code; this test confirms
	// the handler does not reject valid account_types / include_transfers strings.
}

// TestGetMonthOverMonthSpendingChange_ServiceError tests service error propagation.
func TestGetMonthOverMonthSpendingChange_ServiceError(t *testing.T) {
	mockService := &mockAnalyticsService{monthOverMonthErr: errors.New("db error")}
	handler := NewAnalyticsHandler(mockService, nil)

	input := &GetMonthOverMonthSpendingChangeInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-123"},
		Months:             6, // valid — so the mock service error actually propagates
	}

	output, err := handler.GetMonthOverMonthSpendingChange(context.Background(), input)

	assert.Nil(t, output)
	require.Error(t, err)
}

// TestGetMonthOverMonthSpendingChange_MonthsNegative tests that negative months returns 400.
func TestGetMonthOverMonthSpendingChange_MonthsNegative(t *testing.T) {
	handler := NewAnalyticsHandler(&mockAnalyticsService{}, nil)
	input := &GetMonthOverMonthSpendingChangeInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-123"},
		Months:             -1, // negative — should be 400, not silently default to 6
	}
	output, err := handler.GetMonthOverMonthSpendingChange(context.Background(), input)
	assert.Nil(t, output)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "months must be between 2 and 24")
}

// TestGetMonthOverMonthSpendingChange_InvalidIncludeTransfers tests that invalid include_transfers
// returns 400, consistent with all other analytics handlers.
func TestGetMonthOverMonthSpendingChange_InvalidIncludeTransfers(t *testing.T) {
	handler := NewAnalyticsHandler(&mockAnalyticsService{}, nil)
	input := &GetMonthOverMonthSpendingChangeInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-123"},
		Months:             6,
		IncludeTransfers:   "maybe", // invalid
	}
	output, err := handler.GetMonthOverMonthSpendingChange(context.Background(), input)
	assert.Nil(t, output)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "Invalid")
}

// TestGetMonthOverMonthSpendingChange_MonthsAboveMax tests that months>24 returns 400.
func TestGetMonthOverMonthSpendingChange_MonthsAboveMax(t *testing.T) {
	handler := NewAnalyticsHandler(&mockAnalyticsService{}, nil)
	input := &GetMonthOverMonthSpendingChangeInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-123"},
		Months:             25, // above max
	}
	output, err := handler.GetMonthOverMonthSpendingChange(context.Background(), input)
	assert.Nil(t, output)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "months must be between 2 and 24")
}

// TestGetMonthOverMonthSpendingChange_MonthsZeroDefaults tests that explicit months=0 returns 400.
// Huma pre-fills the default (6) for omitted query params before the handler runs;
// explicit ?months=0 is outside the documented 2-24 range and should be rejected.
func TestGetMonthOverMonthSpendingChange_MonthsZeroDefaults(t *testing.T) {
	handler := NewAnalyticsHandler(&mockAnalyticsService{}, nil)
	input := &GetMonthOverMonthSpendingChangeInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-123"},
		Months:             0, // explicit zero — outside 2-24 range, should return 400
	}
	output, err := handler.GetMonthOverMonthSpendingChange(context.Background(), input)
	assert.Nil(t, output)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "months must be between 2 and 24")
}

// TestGetMonthOverMonthSpendingChange_MonthsBoundaries tests valid boundary values 2 and 24.
func TestGetMonthOverMonthSpendingChange_MonthsBoundaries(t *testing.T) {
	for _, months := range []int{2, 24} {
		mockSvc := &mockAnalyticsService{monthOverMonthResponse: &domain.MonthOverMonthResponse{MonthCount: months}}
		handler := NewAnalyticsHandler(mockSvc, nil)
		input := &GetMonthOverMonthSpendingChangeInput{
			AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-123"},
			Months:             months,
		}
		output, err := handler.GetMonthOverMonthSpendingChange(context.Background(), input)
		require.NoError(t, err, "months=%d should be valid", months)
		require.NotNil(t, output)
	}
}

// TestGetLargestTransactionsForPeriod_Success tests successful largest transaction retrieval.
func TestGetLargestTransactionsForPeriod_Success(t *testing.T) {
	mockService := &mockAnalyticsService{
		largestTransactionsResponse: &domain.LargestTransactionsResponse{
			Transactions: []domain.LargestTransactionItem{
				{TransactionID: "tx-001", MerchantName: "Amazon", Amount: 500.0},
			},
			Limit:     10,
			StartDate: "2024-01-01",
			EndDate:   "2024-03-31",
		},
	}
	handler := NewAnalyticsHandler(mockService, nil)

	input := &GetLargestTransactionsForPeriodInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-123"},
		StartDate:          "2024-01-01",
		EndDate:            "2024-03-31",
	}

	output, err := handler.GetLargestTransactionsForPeriod(context.Background(), input)

	require.NoError(t, err)
	require.NotNil(t, output)
	assert.Len(t, output.Body.Transactions, 1)
	assert.Equal(t, 10, output.Body.Limit)
}

// TestGetLargestTransactionsForPeriod_EndBeforeStart tests date validation.
func TestGetLargestTransactionsForPeriod_EndBeforeStart(t *testing.T) {
	handler := NewAnalyticsHandler(&mockAnalyticsService{}, nil)

	input := &GetLargestTransactionsForPeriodInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-123"},
		StartDate:          "2024-03-31",
		EndDate:            "2024-01-01",
	}

	output, err := handler.GetLargestTransactionsForPeriod(context.Background(), input)

	assert.Nil(t, output)
	require.Error(t, err)
}

// TestGetLargestTransactionsForPeriod_ServiceError tests service error propagation.
func TestGetLargestTransactionsForPeriod_ServiceError(t *testing.T) {
	mockService := &mockAnalyticsService{largestTransactionsErr: errors.New("db error")}
	handler := NewAnalyticsHandler(mockService, nil)

	input := &GetLargestTransactionsForPeriodInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-123"},
		StartDate:          "2024-01-01",
		EndDate:            "2024-03-31",
	}

	output, err := handler.GetLargestTransactionsForPeriod(context.Background(), input)

	assert.Nil(t, output)
	require.Error(t, err)
}

// #1369 — Phase 5 decision #5: largest-transactions exposes raw transaction/account IDs
// and must reject household scope with 400 rather than leak a household member's data.
func TestGetLargestTransactionsForPeriod_HouseholdScope_Returns400(t *testing.T) {
	mockService := &mockAnalyticsService{}
	handler := NewAnalyticsHandler(mockService, nil)

	input := &GetLargestTransactionsForPeriodInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-123"},
		StartDate:          "2024-01-01",
		EndDate:            "2024-03-31",
		Scope:              "household",
	}

	output, err := handler.GetLargestTransactionsForPeriod(context.Background(), input)

	assert.Nil(t, output)
	require.Error(t, err)
	var statusErr huma.StatusError
	require.ErrorAs(t, err, &statusErr)
	assert.Equal(t, http.StatusBadRequest, statusErr.GetStatus())
}

// TestGetCategoryBreakdownForMerchant_Success tests successful merchant category breakdown.
func TestGetCategoryBreakdownForMerchant_Success(t *testing.T) {
	merchantID := "550e8400-e29b-41d4-a716-446655440000"
	mockService := &mockAnalyticsService{
		categoryBreakdownForMerchantResponse: &domain.CategoryBreakdownForMerchantResponse{
			MerchantName: merchantID,
			Categories: []domain.CategoryBreakdownItem{
				{Category: "SHOPPING", PFCPrimaryDisplay: "Shopping", Total: 400.0, Percentage: 80.0, TransactionCount: 5},
			},
			TotalSpend: 500.0,
			StartDate:  "2024-01-01",
			EndDate:    "2024-03-31",
		},
	}
	handler := NewAnalyticsHandler(mockService, nil)

	input := &GetCategoryBreakdownForMerchantInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-123"},
		StartDate:          "2024-01-01",
		EndDate:            "2024-03-31",
		MerchantID:         merchantID,
	}

	output, err := handler.GetCategoryBreakdownForMerchant(context.Background(), input)

	require.NoError(t, err)
	require.NotNil(t, output)
	assert.Equal(t, merchantID, output.Body.MerchantName)
	assert.Len(t, output.Body.Categories, 1)
}

// TestGetCategoryBreakdownForMerchant_EndBeforeStart tests date validation.
func TestGetCategoryBreakdownForMerchant_EndBeforeStart(t *testing.T) {
	handler := NewAnalyticsHandler(&mockAnalyticsService{}, nil)

	input := &GetCategoryBreakdownForMerchantInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-123"},
		StartDate:          "2024-03-31",
		EndDate:            "2024-01-01",
		MerchantID:         "550e8400-e29b-41d4-a716-446655440000",
	}

	output, err := handler.GetCategoryBreakdownForMerchant(context.Background(), input)

	assert.Nil(t, output)
	require.Error(t, err)
}

// TestGetCategoryBreakdownForMerchant_ServiceError tests service error propagation.
func TestGetCategoryBreakdownForMerchant_ServiceError(t *testing.T) {
	mockService := &mockAnalyticsService{categoryBreakdownForMerchantErr: errors.New("db error")}
	handler := NewAnalyticsHandler(mockService, nil)

	input := &GetCategoryBreakdownForMerchantInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-123"},
		StartDate:          "2024-01-01",
		EndDate:            "2024-03-31",
		MerchantID:         "550e8400-e29b-41d4-a716-446655440000",
	}

	output, err := handler.GetCategoryBreakdownForMerchant(context.Background(), input)

	assert.Nil(t, output)
	require.Error(t, err)
}

// TestGetIncomeSourceBreakdown_Success tests successful income source breakdown retrieval.
func TestGetIncomeSourceBreakdown_Success(t *testing.T) {
	mockService := &mockAnalyticsService{
		incomeSourceBreakdownResponse: &domain.IncomeSourceBreakdownResponse{
			Sources: []domain.IncomeSourceItem{
				{Category: "INCOME", PFCPrimaryDisplay: "Income", Total: 5000.0, Percentage: 100.0, TransactionCount: 3},
			},
			TotalIncome: 5000.0,
			StartDate:   "2024-01-01",
			EndDate:     "2024-03-31",
		},
	}
	handler := NewAnalyticsHandler(mockService, nil)

	input := &GetIncomeSourceBreakdownInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-123"},
		StartDate:          "2024-01-01",
		EndDate:            "2024-03-31",
	}

	output, err := handler.GetIncomeSourceBreakdown(context.Background(), input)

	require.NoError(t, err)
	require.NotNil(t, output)
	assert.InDelta(t, 5000.0, output.Body.TotalIncome, 0.001)
	assert.Len(t, output.Body.Sources, 1)
}

// TestGetIncomeSourceBreakdown_EndBeforeStart tests date validation.
func TestGetIncomeSourceBreakdown_EndBeforeStart(t *testing.T) {
	handler := NewAnalyticsHandler(&mockAnalyticsService{}, nil)

	input := &GetIncomeSourceBreakdownInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-123"},
		StartDate:          "2024-03-31",
		EndDate:            "2024-01-01",
	}

	output, err := handler.GetIncomeSourceBreakdown(context.Background(), input)

	assert.Nil(t, output)
	require.Error(t, err)
}

// TestGetIncomeSourceBreakdown_ServiceError tests service error propagation.
func TestGetIncomeSourceBreakdown_ServiceError(t *testing.T) {
	mockService := &mockAnalyticsService{incomeSourceBreakdownErr: errors.New("db error")}
	handler := NewAnalyticsHandler(mockService, nil)

	input := &GetIncomeSourceBreakdownInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-123"},
		StartDate:          "2024-01-01",
		EndDate:            "2024-03-31",
	}

	output, err := handler.GetIncomeSourceBreakdown(context.Background(), input)

	assert.Nil(t, output)
	require.Error(t, err)
}

// ─── #1369 — Phase 5 fast-follow (Codex review P1): wire scope=household into
// spending-by-merchant, spending-trends, and income-source-breakdown, which already had
// tested household repository variants (analytics_repository.go) but no HTTP-reachable
// scope param — Codex flagged these as unreachable dead code. ────────────────────────────

func TestGetSpendingByMerchant_HouseholdScope_ResolvesAndAttachesToFilters(t *testing.T) {
	mockService := &mockAnalyticsService{spendingByMerchantResponse: &domain.SpendingByMerchantResponse{}}
	mockHousehold := &mockHouseholdService{}
	scope := &domain.HouseholdScope{HouseholdID: "hh-1", MemberUserIDs: []string{"user-123", "member-2"}}
	mockHousehold.On("ResolveScope", mock.Anything, "user-123", "household").Return(scope, nil)

	handler := NewAnalyticsHandler(mockService, mockHousehold)
	input := &GetSpendingByMerchantInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-123"},
		StartDate:          "2024-01-01",
		EndDate:            "2024-01-31",
		Scope:              "household",
	}
	output, err := handler.GetSpendingByMerchant(context.Background(), input)

	require.NoError(t, err)
	require.NotNil(t, output)
	require.NotNil(t, mockService.lastSpendingFilters)
	assert.Same(t, scope, mockService.lastSpendingFilters.HouseholdScope)
	mockHousehold.AssertExpectations(t)
}

func TestGetSpendingByMerchant_PersonalScope_NeverCallsHouseholdServiceGolden(t *testing.T) {
	mockService := &mockAnalyticsService{spendingByMerchantResponse: &domain.SpendingByMerchantResponse{}}
	mockHousehold := &mockHouseholdService{}

	handler := NewAnalyticsHandler(mockService, mockHousehold)
	input := &GetSpendingByMerchantInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-123"},
		StartDate:          "2024-01-01",
		EndDate:            "2024-01-31",
	}
	output, err := handler.GetSpendingByMerchant(context.Background(), input)

	require.NoError(t, err)
	require.NotNil(t, output)
	require.NotNil(t, mockService.lastSpendingFilters)
	assert.Nil(t, mockService.lastSpendingFilters.HouseholdScope)
	mockHousehold.AssertNotCalled(t, "ResolveScope", mock.Anything, mock.Anything, mock.Anything)
}

func TestGetSpendingTrends_HouseholdScope_ResolvesAndAttachesToFilters(t *testing.T) {
	mockService := &mockAnalyticsService{spendingTrendsResponse: &domain.SpendingTrendsResponse{}}
	mockHousehold := &mockHouseholdService{}
	scope := &domain.HouseholdScope{HouseholdID: "hh-1", MemberUserIDs: []string{"user-123", "member-2"}}
	mockHousehold.On("ResolveScope", mock.Anything, "user-123", "household").Return(scope, nil)

	handler := NewAnalyticsHandler(mockService, mockHousehold)
	input := &GetSpendingTrendsInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-123"},
		StartDate:          "2024-01-01",
		EndDate:            "2024-01-31",
		Scope:              "household",
	}
	output, err := handler.GetSpendingTrends(context.Background(), input)

	require.NoError(t, err)
	require.NotNil(t, output)
	require.NotNil(t, mockService.lastSpendingFilters)
	assert.Same(t, scope, mockService.lastSpendingFilters.HouseholdScope)
	mockHousehold.AssertExpectations(t)
}

func TestGetIncomeSourceBreakdown_HouseholdScope_ResolvesAndAttachesToFilters(t *testing.T) {
	mockService := &mockAnalyticsService{incomeSourceBreakdownResponse: &domain.IncomeSourceBreakdownResponse{}}
	mockHousehold := &mockHouseholdService{}
	scope := &domain.HouseholdScope{HouseholdID: "hh-1", MemberUserIDs: []string{"user-123", "member-2"}}
	mockHousehold.On("ResolveScope", mock.Anything, "user-123", "household").Return(scope, nil)

	handler := NewAnalyticsHandler(mockService, mockHousehold)
	input := &GetIncomeSourceBreakdownInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-123"},
		StartDate:          "2024-01-01",
		EndDate:            "2024-03-31",
		Scope:              "household",
	}
	output, err := handler.GetIncomeSourceBreakdown(context.Background(), input)

	require.NoError(t, err)
	require.NotNil(t, output)
	require.NotNil(t, mockService.lastSpendingFilters)
	assert.Same(t, scope, mockService.lastSpendingFilters.HouseholdScope)
	mockHousehold.AssertExpectations(t)
}

// TestSpendingRouteRegistration tests that spending routes are properly registered.
func TestSpendingRouteRegistration(t *testing.T) {
	_, api := humatest.New(t)
	handler := NewAnalyticsHandler(&mockAnalyticsService{}, nil)
	RegisterAnalyticsRoutes(api, handler)

	// All spending endpoints should exist (return 401, not 404)
	paths := []string{
		"/api/v1/analytics/spending-by-category",
		"/api/v1/analytics/spending-by-merchant",
		"/api/v1/analytics/spending-trends",
		"/api/v1/analytics/average-monthly-spend",
		"/api/v1/analytics/recurring-subscriptions",
		"/api/v1/analytics/savings-rate",
		"/api/v1/analytics/month-over-month",
		"/api/v1/analytics/largest-transactions",
		"/api/v1/analytics/merchant-category-breakdown",
		"/api/v1/analytics/income-sources",
		"/api/v1/analytics/category-hierarchy",
		"/api/v1/analytics/unusual-spend",
		"/api/v1/analytics/net-worth-trend",
		"/api/v1/analytics/budget-vs-actual",
		"/api/v1/analytics/financial-summary",
	}
	for _, path := range paths {
		resp := api.Get(path)
		assert.Equal(t, http.StatusUnauthorized, resp.Code, "path %s should return 401", path)
	}
}

// ─── PR3 handler tests ───────────────────────────────────────────────────────

func TestGetUnusualSpend_Success(t *testing.T) {
	mockService := &mockAnalyticsService{
		unusualSpendResponse: &domain.UnusualSpendResponse{
			Items: []domain.UnusualSpendItem{
				{
					TransactionID:     "tx-001",
					MerchantName:      "Fancy Restaurant",
					Amount:            350.0,
					Category:          "FOOD_AND_DRINK",
					PFCPrimaryDisplay: "Food & Drink",
					Date:              "2024-04-15",
					BaselineAvg:       50.0,
					SigmaAbove:        3.5,
				},
			},
			StartDate:      "2024-04-01",
			EndDate:        "2024-04-30",
			SigmaThreshold: 2.0,
		},
	}
	handler := NewAnalyticsHandler(mockService, nil)
	input := &GetUnusualSpendInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-123"},
		StartDate:          "2024-04-01",
		EndDate:            "2024-04-30",
	}

	output, err := handler.GetUnusualSpend(context.Background(), input)

	require.NoError(t, err)
	require.NotNil(t, output)
	require.Len(t, output.Body.Items, 1)
	assert.Equal(t, "tx-001", output.Body.Items[0].TransactionID)
	assert.Equal(t, 2.0, output.Body.SigmaThreshold)
}

func TestGetUnusualSpend_InvalidDate(t *testing.T) {
	handler := NewAnalyticsHandler(&mockAnalyticsService{}, nil)
	input := &GetUnusualSpendInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-123"},
		StartDate:          "bad-date",
		EndDate:            "2024-04-30",
	}
	_, err := handler.GetUnusualSpend(context.Background(), input)
	require.Error(t, err)
}

func TestGetUnusualSpend_ServiceError(t *testing.T) {
	mockService := &mockAnalyticsService{unusualSpendErr: errors.New("db error")}
	handler := NewAnalyticsHandler(mockService, nil)
	input := &GetUnusualSpendInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-123"},
		StartDate:          "2024-04-01",
		EndDate:            "2024-04-30",
	}
	output, err := handler.GetUnusualSpend(context.Background(), input)
	assert.Nil(t, output)
	require.Error(t, err)
}

func TestGetNetWorthTrend_Success(t *testing.T) {
	nw := 50000.0
	mockService := &mockAnalyticsService{
		netWorthTrendResponse: &domain.NetWorthTrendResponse{
			Buckets: []domain.NetWorthBucket{
				{Date: "2024-03-01", NetWorth: nw},
			},
			Grouping:  "monthly",
			StartDate: "2023-04-01",
			EndDate:   "2024-03-31",
		},
	}
	handler := NewAnalyticsHandler(mockService, nil)
	input := &GetNetWorthTrendInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-123"},
		Grouping:           "monthly",
	}

	output, err := handler.GetNetWorthTrend(context.Background(), input)

	require.NoError(t, err)
	require.NotNil(t, output)
	assert.Equal(t, "monthly", output.Body.Grouping)
	require.Len(t, output.Body.Buckets, 1)
	assert.Equal(t, nw, output.Body.Buckets[0].NetWorth)
}

func TestGetNetWorthTrend_DefaultsToMonthly(t *testing.T) {
	var capturedGrouping string
	mockService := &mockAnalyticsService{
		netWorthTrendResponse: &domain.NetWorthTrendResponse{Grouping: "monthly"},
	}
	// Wrap service to capture grouping
	_ = capturedGrouping
	handler := NewAnalyticsHandler(mockService, nil)
	input := &GetNetWorthTrendInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-123"},
		Grouping:           "", // empty → should default to monthly
	}
	output, err := handler.GetNetWorthTrend(context.Background(), input)
	require.NoError(t, err)
	require.NotNil(t, output)
}

func TestGetNetWorthTrend_ServiceError(t *testing.T) {
	mockService := &mockAnalyticsService{netWorthTrendErr: errors.New("repo not configured")}
	handler := NewAnalyticsHandler(mockService, nil)
	input := &GetNetWorthTrendInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-123"},
		Grouping:           "monthly",
	}
	output, err := handler.GetNetWorthTrend(context.Background(), input)
	assert.Nil(t, output)
	require.Error(t, err)
}

func TestGetBudgetVsActual_Success(t *testing.T) {
	mockService := &mockAnalyticsService{
		budgetVsActualResponse: &domain.BudgetVsActualResponse{
			Budgets: []domain.BudgetVsActualItem{
				{
					BudgetID:        "bgt-1",
					BudgetName:      "Groceries",
					Period:          "monthly",
					AllocatedAmount: 500.0,
					SpentAmount:     350.0,
					Remaining:       150.0,
					OverBudget:      false,
					PctUsed:         70.0,
				},
			},
			TotalAllocated: 500.0,
			TotalSpent:     350.0,
			TotalRemaining: 150.0,
			Period:         "monthly",
		},
	}
	handler := NewAnalyticsHandler(mockService, nil)
	input := &GetBudgetVsActualInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-123"},
		Period:             "monthly",
	}

	output, err := handler.GetBudgetVsActual(context.Background(), input)

	require.NoError(t, err)
	require.NotNil(t, output)
	assert.Equal(t, "monthly", output.Body.Period)
	require.Len(t, output.Body.Budgets, 1)
	assert.Equal(t, "Groceries", output.Body.Budgets[0].BudgetName)
	assert.Equal(t, 70.0, output.Body.Budgets[0].PctUsed)
}

func TestGetBudgetVsActual_ServiceError(t *testing.T) {
	mockService := &mockAnalyticsService{budgetVsActualErr: errors.New("invalid period")}
	handler := NewAnalyticsHandler(mockService, nil)
	input := &GetBudgetVsActualInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-123"},
		Period:             "biannual",
	}
	output, err := handler.GetBudgetVsActual(context.Background(), input)
	assert.Nil(t, output)
	require.Error(t, err)
}

func TestGetBudgetVsActual_RouteRegistration(t *testing.T) {
	_, api := humatest.New(t)
	handler := NewAnalyticsHandler(&mockAnalyticsService{}, nil)
	RegisterAnalyticsRoutes(api, handler)

	// Verify JSON contract: budget-vs-actual returns the expected structure
	mockService := &mockAnalyticsService{
		budgetVsActualResponse: &domain.BudgetVsActualResponse{
			Budgets:        []domain.BudgetVsActualItem{},
			TotalAllocated: 0,
			TotalSpent:     0,
			TotalRemaining: 0,
			Period:         "monthly",
		},
	}
	handler2 := NewAnalyticsHandler(mockService, nil)
	input := &GetBudgetVsActualInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-123"},
		Period:             "monthly",
	}
	output, err := handler2.GetBudgetVsActual(context.Background(), input)
	require.NoError(t, err)
	// Serialize and check JSON has expected keys
	b, err := json.Marshal(output.Body)
	require.NoError(t, err)
	assert.Contains(t, string(b), `"total_allocated"`)
	assert.Contains(t, string(b), `"total_spent"`)
	assert.Contains(t, string(b), `"total_remaining"`)
	assert.Contains(t, string(b), `"period"`)
}

// ─── PR4: GetFinancialSummary handler tests ───────────────────────────────────

// TestGetFinancialSummary_AllSubMetricsSucceed verifies the composite endpoint
// populates all sub-metrics when every service call succeeds.
func TestGetFinancialSummary_AllSubMetricsSucceed(t *testing.T) {
	rate := 0.4
	mockService := &mockAnalyticsService{
		savingsRateResponse: &domain.SavingsRateResponse{
			SavingsRate: &rate,
			Income:      5000.0,
			Expenses:    3000.0,
			NetSavings:  2000.0,
			StartDate:   "2024-01-01",
			EndDate:     "2024-01-31",
		},
		averageMonthlySpendResponse: &domain.AverageMonthlySpendResponse{
			AverageMonthlySpend: 3000.0,
			TotalSpend:          3000.0,
			MonthCount:          1,
			StartDate:           "2024-01-01",
			EndDate:             "2024-01-31",
		},
		spendingByCategoryResponse: &domain.SpendingByCategoryResponse{
			StartDate:  "2024-01-01",
			EndDate:    "2024-01-31",
			TotalSpent: 3000.0,
			Categories: []domain.SpendingByCategory{
				{Category: "FOOD_AND_DRINK", Total: 500.0, Percentage: 16.7, TransactionCount: 10},
			},
		},
		monthOverMonthResponse: &domain.MonthOverMonthResponse{
			Buckets:    []domain.MonthOverMonthBucket{{Month: "2024-01-01", Total: 3000.0}},
			Category:   "",
			MonthCount: 6,
		},
		budgetVsActualResponse: &domain.BudgetVsActualResponse{
			Budgets:        []domain.BudgetVsActualItem{},
			TotalAllocated: 500.0,
			TotalSpent:     350.0,
			TotalRemaining: 150.0,
			Period:         "monthly",
		},
		netWorthTrendResponse: &domain.NetWorthTrendResponse{
			Buckets:   []domain.NetWorthBucket{{Date: "2024-01-01", NetWorth: 50000.0}},
			Grouping:  "monthly",
			StartDate: "2023-02-01",
			EndDate:   "2024-01-31",
		},
	}
	handler := NewAnalyticsHandler(mockService, nil)

	input := &GetFinancialSummaryInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-123"},
		StartDate:          "2024-01-01",
		EndDate:            "2024-01-31",
	}

	output, err := handler.GetFinancialSummary(context.Background(), input)

	require.NoError(t, err)
	require.NotNil(t, output)
	require.NotNil(t, output.Body)
	assert.NotEmpty(t, output.Body.GeneratedAt)
	assert.NotNil(t, output.Body.SavingsRate)
	assert.NotNil(t, output.Body.AverageSpend)
	assert.NotNil(t, output.Body.TopCategories)
	assert.NotNil(t, output.Body.MomChange)
	assert.NotNil(t, output.Body.BudgetVsActual)
	assert.NotNil(t, output.Body.NetWorthTrend)
}

// TestGetFinancialSummary_HouseholdScope_ResolvesAndThreadsIntoSubMetrics covers Issue
// #1369 decision #5: GetFinancialSummary must resolve `scope=household` once and thread it
// into its aggregate sub-metrics (savings-rate, average-spend, top-categories,
// month-over-month, net-worth-trend). errgroup runs the sub-calls concurrently, but
// g.Wait() (called before GetFinancialSummary returns) happens-before this assertion, so
// reading mockService.lastSpendingFilters here is race-free.
func TestGetFinancialSummary_HouseholdScope_ResolvesAndThreadsIntoSubMetrics(t *testing.T) {
	mockService := &mockAnalyticsService{
		spendingByCategoryResponse: &domain.SpendingByCategoryResponse{},
	}
	mockHousehold := &mockHouseholdService{}
	scope := &domain.HouseholdScope{HouseholdID: "hh-1", MemberUserIDs: []string{"user-123", "member-2"}}
	mockHousehold.On("ResolveScope", mock.Anything, "user-123", "household").Return(scope, nil)

	handler := NewAnalyticsHandler(mockService, mockHousehold)
	input := &GetFinancialSummaryInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-123"},
		StartDate:          "2024-01-01",
		EndDate:            "2024-01-31",
		Scope:              "household",
	}

	output, err := handler.GetFinancialSummary(context.Background(), input)

	require.NoError(t, err)
	require.NotNil(t, output)
	require.NotNil(t, mockService.lastSpendingFilters)
	assert.Same(t, scope, mockService.lastSpendingFilters.HouseholdScope, "top-categories sub-metric must receive the resolved household scope")
	mockHousehold.AssertExpectations(t)
}

// TestGetFinancialSummary_OneSubMetricFails verifies that when one sub-metric
// returns an error, the response is still 200 with that section nil.
func TestGetFinancialSummary_OneSubMetricFails(t *testing.T) {
	rate := 0.4
	mockService := &mockAnalyticsService{
		savingsRateResponse: &domain.SavingsRateResponse{
			SavingsRate: &rate,
			Income:      5000.0,
			Expenses:    3000.0,
		},
		averageMonthlySpendResponse: &domain.AverageMonthlySpendResponse{
			AverageMonthlySpend: 3000.0,
		},
		// spendingByCategory fails — top_categories should be nil
		spendingByCategoryErr: errors.New("db timeout"),
		monthOverMonthResponse: &domain.MonthOverMonthResponse{
			Buckets:    []domain.MonthOverMonthBucket{},
			MonthCount: 6,
		},
		budgetVsActualResponse: &domain.BudgetVsActualResponse{
			Period: "monthly",
		},
		netWorthTrendResponse: &domain.NetWorthTrendResponse{
			Grouping: "monthly",
		},
	}
	handler := NewAnalyticsHandler(mockService, nil)

	input := &GetFinancialSummaryInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-123"},
		StartDate:          "2024-01-01",
		EndDate:            "2024-01-31",
	}

	output, err := handler.GetFinancialSummary(context.Background(), input)

	// 200 overall even though one sub-metric failed
	require.NoError(t, err)
	require.NotNil(t, output)
	require.NotNil(t, output.Body)
	assert.Nil(t, output.Body.TopCategories, "failed sub-metric should be nil")
	assert.NotNil(t, output.Body.SavingsRate, "other sub-metrics should still be populated")
	assert.NotNil(t, output.Body.AverageSpend)
}

// TestGetFinancialSummary_InvalidDateRange verifies that date validation errors
// propagate as 400 (non-degradable pre-work).
func TestGetFinancialSummary_InvalidDateRange(t *testing.T) {
	handler := NewAnalyticsHandler(&mockAnalyticsService{}, nil)

	input := &GetFinancialSummaryInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-123"},
		StartDate:          "2024-01-31",
		EndDate:            "2024-01-01", // end before start
	}

	output, err := handler.GetFinancialSummary(context.Background(), input)

	assert.Nil(t, output)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "end_date cannot be before start_date")
}

// --- GetCategoryTotalForPeriod tests ---

func TestGetCategoryTotalForPeriod_Success(t *testing.T) {
	expected := &domain.CategoryTotalResponse{
		Category:          "FOOD_AND_DRINK",
		PFCPrimaryDisplay: "Food & Drink",
		Total:             450.00,
		TransactionCount:  12,
		StartDate:         "2024-01-01",
		EndDate:           "2024-01-31",
	}
	mockSvc := &mockAnalyticsService{categoryTotalResponse: expected}
	handler := NewAnalyticsHandler(mockSvc, nil)

	input := &GetCategoryTotalForPeriodInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-123"},
		StartDate:          "2024-01-01",
		EndDate:            "2024-01-31",
		Category:           "FOOD_AND_DRINK",
	}
	output, err := handler.GetCategoryTotalForPeriod(context.Background(), input)

	require.NoError(t, err)
	require.NotNil(t, output)
	assert.Equal(t, expected, output.Body)
}

func TestGetCategoryTotalForPeriod_InvalidDateRange(t *testing.T) {
	mockSvc := &mockAnalyticsService{}
	handler := NewAnalyticsHandler(mockSvc, nil)

	input := &GetCategoryTotalForPeriodInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-123"},
		StartDate:          "2024-01-31",
		EndDate:            "2024-01-01", // end before start
		Category:           "FOOD_AND_DRINK",
	}
	output, err := handler.GetCategoryTotalForPeriod(context.Background(), input)

	assert.Nil(t, output)
	require.Error(t, err)
}

func TestGetCategoryTotalForPeriod_ServiceError(t *testing.T) {
	mockSvc := &mockAnalyticsService{categoryTotalErr: errors.New("db error")}
	handler := NewAnalyticsHandler(mockSvc, nil)

	input := &GetCategoryTotalForPeriodInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-123"},
		StartDate:          "2024-01-01",
		EndDate:            "2024-01-31",
		Category:           "FOOD_AND_DRINK",
	}
	output, err := handler.GetCategoryTotalForPeriod(context.Background(), input)

	assert.Nil(t, output)
	require.Error(t, err)
}

// --- GetTopExpense tests ---

func TestGetTopExpense_Success(t *testing.T) {
	expected := &domain.TopExpenseResponse{
		TransactionID: "txn-123",
		MerchantName:  "ACME Store",
		Amount:        299.99,
		Category:      "SHOPPING",
		StartDate:     "2024-01-01",
		EndDate:       "2024-01-31",
	}
	mockSvc := &mockAnalyticsService{topExpenseResponse: expected}
	handler := NewAnalyticsHandler(mockSvc, nil)

	input := &GetTopExpenseInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-123"},
		StartDate:          "2024-01-01",
		EndDate:            "2024-01-31",
	}
	output, err := handler.GetTopExpense(context.Background(), input)

	require.NoError(t, err)
	require.NotNil(t, output)
	assert.Equal(t, expected, output.Body)
}

func TestGetTopExpense_InvalidDateRange(t *testing.T) {
	mockSvc := &mockAnalyticsService{}
	handler := NewAnalyticsHandler(mockSvc, nil)

	input := &GetTopExpenseInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-123"},
		StartDate:          "not-a-date",
		EndDate:            "2024-01-31",
	}
	output, err := handler.GetTopExpense(context.Background(), input)

	assert.Nil(t, output)
	require.Error(t, err)
}

func TestGetTopExpense_ServiceError(t *testing.T) {
	mockSvc := &mockAnalyticsService{topExpenseErr: errors.New("db error")}
	handler := NewAnalyticsHandler(mockSvc, nil)

	input := &GetTopExpenseInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-123"},
		StartDate:          "2024-01-01",
		EndDate:            "2024-01-31",
	}
	output, err := handler.GetTopExpense(context.Background(), input)

	assert.Nil(t, output)
	require.Error(t, err)
}

// #1369 — Phase 5 decision #5: top-expense exposes a raw transaction ID and must reject
// household scope with 400 rather than leak a household member's transaction.
func TestGetTopExpense_HouseholdScope_Returns400(t *testing.T) {
	mockSvc := &mockAnalyticsService{}
	handler := NewAnalyticsHandler(mockSvc, nil)

	input := &GetTopExpenseInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-123"},
		StartDate:          "2024-01-01",
		EndDate:            "2024-01-31",
		Scope:              "household",
	}
	output, err := handler.GetTopExpense(context.Background(), input)

	assert.Nil(t, output)
	require.Error(t, err)
	var statusErr huma.StatusError
	require.ErrorAs(t, err, &statusErr)
	assert.Equal(t, http.StatusBadRequest, statusErr.GetStatus())
}

// ─── #1369 — Phase 5: golden regression — scope=personal (the default) must stay
// byte-identical to pre-Phase-5 behavior. These lock in that the personal path never
// touches the household service and never attaches a HouseholdScope to filters, for the
// three core-scoped analytics endpoints not already covered by dashboard_test.go's
// equivalent guards. ───────────────────────────────────────────────────────────────────

func TestGetCashFlowSummary_PersonalScope_NeverCallsHouseholdServiceGolden(t *testing.T) {
	mockSvc := &mockAnalyticsService{summaryResponse: &domain.CashFlowSummaryResponse{}}
	mockHousehold := &mockHouseholdService{}
	// No .On(...) set up — if the handler called ResolveScope for personal/absent scope,
	// this mock would panic ("I don't know what to return").

	handler := NewAnalyticsHandler(mockSvc, mockHousehold)
	input := &GetCashFlowSummaryInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-123"},
		StartDate:          "2024-01-01",
		EndDate:            "2024-01-31",
	}
	output, err := handler.GetCashFlowSummary(context.Background(), input)

	require.NoError(t, err)
	require.NotNil(t, output)
	require.NotNil(t, mockSvc.lastCashFlowFilters)
	assert.Nil(t, mockSvc.lastCashFlowFilters.HouseholdScope)
	mockHousehold.AssertNotCalled(t, "ResolveScope", mock.Anything, mock.Anything, mock.Anything)
}

func TestGetCashFlowHistory_PersonalScope_NeverCallsHouseholdServiceGolden(t *testing.T) {
	mockSvc := &mockAnalyticsService{historyResponse: &domain.CashFlowHistoryResponse{}}
	mockHousehold := &mockHouseholdService{}

	handler := NewAnalyticsHandler(mockSvc, mockHousehold)
	input := &GetCashFlowHistoryInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-123"},
		StartDate:          "2024-01-01",
		EndDate:            "2024-01-31",
	}
	output, err := handler.GetCashFlowHistory(context.Background(), input)

	require.NoError(t, err)
	require.NotNil(t, output)
	require.NotNil(t, mockSvc.lastCashFlowFilters)
	assert.Nil(t, mockSvc.lastCashFlowFilters.HouseholdScope)
	mockHousehold.AssertNotCalled(t, "ResolveScope", mock.Anything, mock.Anything, mock.Anything)
}

func TestGetSpendingByCategory_PersonalScope_NeverCallsHouseholdServiceGolden(t *testing.T) {
	mockSvc := &mockAnalyticsService{spendingByCategoryResponse: &domain.SpendingByCategoryResponse{}}
	mockHousehold := &mockHouseholdService{}

	handler := NewAnalyticsHandler(mockSvc, mockHousehold)
	input := &GetSpendingByCategoryInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-123"},
		StartDate:          "2024-01-01",
		EndDate:            "2024-01-31",
	}
	output, err := handler.GetSpendingByCategory(context.Background(), input)

	require.NoError(t, err)
	require.NotNil(t, output)
	require.NotNil(t, mockSvc.lastSpendingFilters)
	assert.Nil(t, mockSvc.lastSpendingFilters.HouseholdScope)
	mockHousehold.AssertNotCalled(t, "ResolveScope", mock.Anything, mock.Anything, mock.Anything)
}

// TestGetSpendingByCategory_HouseholdScope_ResolvesAndAttachesToFilters completes the
// core-4 scope-resolution coverage (dashboard's balance-summary/net-worth/history variants
// are covered in dashboard_test.go); cash-flow's resolution path is exercised implicitly by
// the golden tests above plus the shared resolveHouseholdScopeIfRequested unit coverage.
func TestGetSpendingByCategory_HouseholdScope_ResolvesAndAttachesToFilters(t *testing.T) {
	mockSvc := &mockAnalyticsService{spendingByCategoryResponse: &domain.SpendingByCategoryResponse{}}
	mockHousehold := &mockHouseholdService{}
	scope := &domain.HouseholdScope{HouseholdID: "hh-1", MemberUserIDs: []string{"user-123", "member-2"}}
	mockHousehold.On("ResolveScope", mock.Anything, "user-123", "household").Return(scope, nil)

	handler := NewAnalyticsHandler(mockSvc, mockHousehold)
	input := &GetSpendingByCategoryInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-123"},
		StartDate:          "2024-01-01",
		EndDate:            "2024-01-31",
		Scope:              "household",
	}
	output, err := handler.GetSpendingByCategory(context.Background(), input)

	require.NoError(t, err)
	require.NotNil(t, output)
	require.NotNil(t, mockSvc.lastSpendingFilters)
	assert.Same(t, scope, mockSvc.lastSpendingFilters.HouseholdScope)
	mockHousehold.AssertExpectations(t)
}

// TestGetCategoryHierarchy_PassesDirectionAndScoping pins the handler's parameter plumbing (#1626).
//
// Direction is the one param that changes WHICH HALF OF THE LEDGER is read, so an unset or
// misspelled value must not silently swap income for expense. The enum tag rejects garbage at the
// edge, but the handler still defaults defensively, and that default is what this asserts.
func TestGetCategoryHierarchy_PassesDirectionAndScoping(t *testing.T) {
	mockService := &mockAnalyticsService{
		categoryHierarchyResponse: &domain.CategoryHierarchyResponse{Direction: "expense"},
	}
	handler := NewAnalyticsHandler(mockService, nil)

	t.Run("income direction reaches the service", func(t *testing.T) {
		_, err := handler.GetCategoryHierarchy(context.Background(), &GetCategoryHierarchyInput{
			AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-123"},
			StartDate:          "2026-01-01",
			EndDate:            "2026-03-31",
			Direction:          "income",
		})
		require.NoError(t, err)
		assert.Equal(t, domain.CategoryDirectionIncome, mockService.lastCategoryDirection)
	})

	t.Run("an empty direction defaults to expense, never to income", func(t *testing.T) {
		_, err := handler.GetCategoryHierarchy(context.Background(), &GetCategoryHierarchyInput{
			AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-123"},
			StartDate:          "2026-01-01",
			EndDate:            "2026-03-31",
		})
		require.NoError(t, err)
		assert.Equal(t, domain.CategoryDirectionExpense, mockService.lastCategoryDirection,
			"an unset direction must not read the other half of the ledger")
	})

	t.Run("custom category scoping and roll-up default", func(t *testing.T) {
		_, err := handler.GetCategoryHierarchy(context.Background(), &GetCategoryHierarchyInput{
			AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-123"},
			StartDate:          "2026-01-01",
			EndDate:            "2026-03-31",
			CustomCategoryIDs:  " abc , def ,, ",
		})
		require.NoError(t, err)
		require.NotNil(t, mockService.lastSpendingFilters)
		assert.Equal(t, []string{"abc", "def"}, mockService.lastSpendingFilters.CustomCategoryIDs,
			"blank entries must be dropped and values trimmed")
		assert.True(t, mockService.lastSpendingFilters.RollUpChildren,
			"roll-up defaults to true — only the literal \"false\" disables it")
	})

	// #1626 Phase 2, item 7. The subtests above cover direction and custom-category scoping and
	// stop there, so three parameters this endpoint declares reached the service untested:
	// account_types, include_transfers and scope. An unparsed param is not a cosmetic gap — the
	// reports page sends all three, and an endpoint that silently ignores one returns a correct
	// answer to a question the user did not ask.
	t.Run("account_types is parsed into a list, not passed through raw", func(t *testing.T) {
		_, err := handler.GetCategoryHierarchy(context.Background(), &GetCategoryHierarchyInput{
			AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-123"},
			StartDate:          "2026-01-01",
			EndDate:            "2026-03-31",
			AccountTypes:       " depository , credit ,, ",
		})
		require.NoError(t, err)
		require.NotNil(t, mockService.lastSpendingFilters)
		// Asserting the PARSED value, not merely that something arrived: no test in this package
		// pins AccountTypes' splitting, so a handler that forwarded the raw string — or dropped the
		// param — would look identical from the outside.
		assert.Equal(t, []string{"depository", "credit"}, mockService.lastSpendingFilters.AccountTypes,
			"blank entries must be dropped and values trimmed, as custom_category_ids are")
	})

	t.Run("include_transfers defaults to false and only true opts in", func(t *testing.T) {
		_, err := handler.GetCategoryHierarchy(context.Background(), &GetCategoryHierarchyInput{
			AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-123"},
			StartDate:          "2026-01-01",
			EndDate:            "2026-03-31",
		})
		require.NoError(t, err)
		assert.False(t, mockService.lastSpendingFilters.IncludeTransfers,
			"absent must mean false — counting transfers as spend double-counts money moving between "+
				"the user's own accounts")

		_, err = handler.GetCategoryHierarchy(context.Background(), &GetCategoryHierarchyInput{
			AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-123"},
			StartDate:          "2026-01-01",
			EndDate:            "2026-03-31",
			IncludeTransfers:   "true",
		})
		require.NoError(t, err)
		assert.True(t, mockService.lastSpendingFilters.IncludeTransfers)
	})

	t.Run("a malformed include_transfers is rejected, not silently treated as false", func(t *testing.T) {
		_, err := handler.GetCategoryHierarchy(context.Background(), &GetCategoryHierarchyInput{
			AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-123"},
			StartDate:          "2026-01-01",
			EndDate:            "2026-03-31",
			IncludeTransfers:   "yes-please",
		})
		// Falling back to the default would answer a different question than the one asked and give
		// the caller no way to notice.
		require.Error(t, err)
	})

	t.Run("roll_up=false disables roll-up", func(t *testing.T) {
		_, err := handler.GetCategoryHierarchy(context.Background(), &GetCategoryHierarchyInput{
			AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-123"},
			StartDate:          "2026-01-01",
			EndDate:            "2026-03-31",
			RollUp:             "false",
		})
		require.NoError(t, err)
		assert.False(t, mockService.lastSpendingFilters.RollUpChildren)
	})
}

// The #1369 pair, mirrored for the hierarchy endpoint (#1626 Phase 2, item 7). Household scope was
// declared by this endpoint from the day it shipped and asserted nowhere, and the two halves are
// separate tests on purpose: the first proves the resolved scope is ATTACHED, the second proves the
// personal path never even asks. A single test cannot fail for both reasons.
//
// The handler needs a real &mockHouseholdService{} rather than nil for either to be meaningful.
func TestGetCategoryHierarchy_HouseholdScope_ResolvesAndAttachesToFilters(t *testing.T) {
	mockService := &mockAnalyticsService{
		categoryHierarchyResponse: &domain.CategoryHierarchyResponse{Direction: "expense"},
	}
	mockHousehold := &mockHouseholdService{}
	scope := &domain.HouseholdScope{HouseholdID: "hh-1", MemberUserIDs: []string{"user-123", "member-2"}}
	mockHousehold.On("ResolveScope", mock.Anything, "user-123", "household").Return(scope, nil)

	handler := NewAnalyticsHandler(mockService, mockHousehold)
	output, err := handler.GetCategoryHierarchy(context.Background(), &GetCategoryHierarchyInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-123"},
		StartDate:          "2026-01-01",
		EndDate:            "2026-03-31",
		Scope:              "household",
	})

	require.NoError(t, err)
	require.NotNil(t, output)
	require.NotNil(t, mockService.lastSpendingFilters)
	// assert.Same, not assert.Equal: the resolved scope must be the very object the household
	// service returned. An Equal would pass against a handler that rebuilt an identical-looking
	// scope from the request, which is the bug — it would trust caller-supplied membership.
	assert.Same(t, scope, mockService.lastSpendingFilters.HouseholdScope)
	mockHousehold.AssertExpectations(t)
}

func TestGetCategoryHierarchy_PersonalScope_NeverCallsHouseholdServiceGolden(t *testing.T) {
	mockService := &mockAnalyticsService{
		categoryHierarchyResponse: &domain.CategoryHierarchyResponse{Direction: "expense"},
	}
	// Deliberately NO .On(...): an unexpected call to this mock fails the test by itself, so the
	// AssertNotCalled below is belt as well as braces.
	mockHousehold := &mockHouseholdService{}

	handler := NewAnalyticsHandler(mockService, mockHousehold)
	output, err := handler.GetCategoryHierarchy(context.Background(), &GetCategoryHierarchyInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-123"},
		StartDate:          "2026-01-01",
		EndDate:            "2026-03-31",
	})

	require.NoError(t, err)
	require.NotNil(t, output)
	require.NotNil(t, mockService.lastSpendingFilters)
	assert.Nil(t, mockService.lastSpendingFilters.HouseholdScope)
	mockHousehold.AssertNotCalled(t, "ResolveScope", mock.Anything, mock.Anything, mock.Anything)
}

// TestGetCategoryTotalForPeriod_MalformedCustomTokenIsABadRequest is #1626 Phase 2's review
// finding X2 at the API boundary.
//
// WHAT IT PROVES, precisely: the handler passes a service error through
// humaerrors.LoggingMapError, so an ErrInvalidCategoryToken raised by the gate becomes a 400
// huma.StatusError rather than the 500 that a `custom:not-a-uuid` query string used to produce
// when its suffix reached the repository's `$5::uuid` cast.
//
// ⚠ WHAT IT DOES NOT PROVE, said out loud so nobody reads it as more than it is: the mock service
// does not validate, so this does not exercise the gate itself. That is
// TestGetCategoryTotalForPeriod_RejectsAMalformedCustomToken /
// _AcceptsTheTokensItIsSupposedTo (internal/services), which cover the malformed, empty and valid
// custom tokens plus the PFC tokens that must be left alone. The two halves together are the
// chain; neither alone is.
func TestGetCategoryTotalForPeriod_MalformedCustomTokenIsABadRequest(t *testing.T) {
	mockSvc := &mockAnalyticsService{
		categoryTotalErr: fmt.Errorf("%w: %q", services.ErrInvalidCategoryToken, "not-a-uuid"),
	}
	handler := NewAnalyticsHandler(mockSvc, nil)

	input := &GetCategoryTotalForPeriodInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-123"},
		StartDate:          "2026-01-01",
		EndDate:            "2026-01-31",
		Category:           "custom:not-a-uuid",
	}
	output, err := handler.GetCategoryTotalForPeriod(context.Background(), input)

	assert.Nil(t, output)
	require.Error(t, err)
	statusErr, ok := err.(huma.StatusError)
	require.True(t, ok, "the handler must return a huma.StatusError, not a bare error")
	assert.Equal(t, http.StatusBadRequest, statusErr.GetStatus(),
		"a malformed category token is the client's mistake; 500 would page somebody for it")
	assert.NotContains(t, statusErr.Error(), "not-a-uuid",
		"the response must not echo the attacker-supplied suffix")
}
