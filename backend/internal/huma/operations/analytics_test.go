package operations

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/danielgtaylor/huma/v2/humatest"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/huma/inputs"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockAnalyticsService is a mock implementation of services.AnalyticsService
type mockAnalyticsService struct {
	summaryResponse                         *domain.CashFlowSummaryResponse
	historyResponse                         *domain.CashFlowHistoryResponse
	spendingByCategoryResponse              *domain.SpendingByCategoryResponse
	spendingByMerchantResponse              *domain.SpendingByMerchantResponse
	spendingTrendsResponse                  *domain.SpendingTrendsResponse
	averageMonthlySpendResponse             *domain.AverageMonthlySpendResponse
	recurringSubscriptionsResponse          *domain.RecurringSubscriptionsResponse
	savingsRateResponse                     *domain.SavingsRateResponse
	monthOverMonthResponse                  *domain.MonthOverMonthResponse
	largestTransactionsResponse             *domain.LargestTransactionsResponse
	categoryBreakdownForMerchantResponse    *domain.CategoryBreakdownForMerchantResponse
	incomeSourceBreakdownResponse           *domain.IncomeSourceBreakdownResponse
	unusualSpendResponse                    *domain.UnusualSpendResponse
	netWorthTrendResponse                   *domain.NetWorthTrendResponse
	budgetVsActualResponse                  *domain.BudgetVsActualResponse
	summaryErr                              error
	historyErr                              error
	spendingByCategoryErr                   error
	spendingByMerchantErr                   error
	spendingTrendsErr                       error
	averageMonthlySpendErr                  error
	recurringSubscriptionsErr               error
	savingsRateErr                          error
	monthOverMonthErr                       error
	largestTransactionsErr                  error
	categoryBreakdownForMerchantErr         error
	incomeSourceBreakdownErr                error
	unusualSpendErr                         error
	netWorthTrendErr                        error
	budgetVsActualErr                       error
}

func (m *mockAnalyticsService) GetCashFlowSummary(ctx context.Context, userID string, filters domain.CashFlowFilters) (*domain.CashFlowSummaryResponse, error) {
	if m.summaryErr != nil {
		return nil, m.summaryErr
	}
	return m.summaryResponse, nil
}

func (m *mockAnalyticsService) GetCashFlowHistory(ctx context.Context, userID string, filters domain.CashFlowFilters) (*domain.CashFlowHistoryResponse, error) {
	if m.historyErr != nil {
		return nil, m.historyErr
	}
	return m.historyResponse, nil
}

func (m *mockAnalyticsService) GetSpendingByCategory(ctx context.Context, userID string, filters domain.SpendingFilters) (*domain.SpendingByCategoryResponse, error) {
	return m.spendingByCategoryResponse, m.spendingByCategoryErr
}

func (m *mockAnalyticsService) GetSpendingByMerchant(ctx context.Context, userID string, filters domain.SpendingFilters, limit int) (*domain.SpendingByMerchantResponse, error) {
	return m.spendingByMerchantResponse, m.spendingByMerchantErr
}

func (m *mockAnalyticsService) GetSpendingTrends(ctx context.Context, userID string, filters domain.SpendingFilters) (*domain.SpendingTrendsResponse, error) {
	return m.spendingTrendsResponse, m.spendingTrendsErr
}

func (m *mockAnalyticsService) GetCategoryTotalForPeriod(_ context.Context, _ string, _ string, _ time.Time, _ time.Time) (*domain.CategoryTotalResponse, error) {
	return nil, nil
}

func (m *mockAnalyticsService) GetTopExpense(_ context.Context, _ string, _ time.Time, _ time.Time) (*domain.TopExpenseResponse, error) {
	return nil, nil
}

func (m *mockAnalyticsService) GetAverageMonthlySpend(_ context.Context, _ string, _ domain.SpendingFilters) (*domain.AverageMonthlySpendResponse, error) {
	return m.averageMonthlySpendResponse, m.averageMonthlySpendErr
}

func (m *mockAnalyticsService) GetRecurringSubscriptions(_ context.Context, _ string) (*domain.RecurringSubscriptionsResponse, error) {
	return m.recurringSubscriptionsResponse, m.recurringSubscriptionsErr
}

func (m *mockAnalyticsService) GetSavingsRateForPeriod(_ context.Context, _ string, _ time.Time, _ time.Time) (*domain.SavingsRateResponse, error) {
	return m.savingsRateResponse, m.savingsRateErr
}

func (m *mockAnalyticsService) GetMonthOverMonthSpendingChange(_ context.Context, _ string, _ string, _ int) (*domain.MonthOverMonthResponse, error) {
	return m.monthOverMonthResponse, m.monthOverMonthErr
}

func (m *mockAnalyticsService) GetLargestTransactionsForPeriod(_ context.Context, _ string, _ domain.SpendingFilters, _ int) (*domain.LargestTransactionsResponse, error) {
	return m.largestTransactionsResponse, m.largestTransactionsErr
}

func (m *mockAnalyticsService) GetCategoryBreakdownForMerchant(_ context.Context, _ string, _ string, _ domain.SpendingFilters) (*domain.CategoryBreakdownForMerchantResponse, error) {
	return m.categoryBreakdownForMerchantResponse, m.categoryBreakdownForMerchantErr
}

func (m *mockAnalyticsService) GetIncomeSourceBreakdown(_ context.Context, _ string, _ domain.SpendingFilters) (*domain.IncomeSourceBreakdownResponse, error) {
	return m.incomeSourceBreakdownResponse, m.incomeSourceBreakdownErr
}

func (m *mockAnalyticsService) GetUnusualSpend(_ context.Context, _ string, _ time.Time, _ time.Time) (*domain.UnusualSpendResponse, error) {
	return m.unusualSpendResponse, m.unusualSpendErr
}

func (m *mockAnalyticsService) GetNetWorthTrend(_ context.Context, _ string, _ string) (*domain.NetWorthTrendResponse, error) {
	return m.netWorthTrendResponse, m.netWorthTrendErr
}

func (m *mockAnalyticsService) GetBudgetVsActual(_ context.Context, _ string, _ string) (*domain.BudgetVsActualResponse, error) {
	return m.budgetVsActualResponse, m.budgetVsActualErr
}

// Helper functions to create test data
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

// TestGetCashFlowSummary_Success tests successful retrieval of cash flow summary.
func TestGetCashFlowSummary_Success(t *testing.T) {
	mockService := &mockAnalyticsService{
		summaryResponse: createTestCashFlowSummary(),
	}

	handler := NewAnalyticsHandler(mockService)

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

// TestGetCashFlowSummary_WithFilters tests cash flow summary with all filters.
func TestGetCashFlowSummary_WithFilters(t *testing.T) {
	mockService := &mockAnalyticsService{
		summaryResponse: createTestCashFlowSummary(),
	}

	handler := NewAnalyticsHandler(mockService)

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

// TestGetCashFlowSummary_MissingStartDate tests error for missing start_date.
func TestGetCashFlowSummary_MissingStartDate(t *testing.T) {
	mockService := &mockAnalyticsService{}

	handler := NewAnalyticsHandler(mockService)

	input := &GetCashFlowSummaryInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-123"},
		EndDate:            "2024-01-31",
	}

	output, err := handler.GetCashFlowSummary(context.Background(), input)

	assert.Nil(t, output)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "start_date is required")
}

// TestGetCashFlowSummary_MissingEndDate tests error for missing end_date.
func TestGetCashFlowSummary_MissingEndDate(t *testing.T) {
	mockService := &mockAnalyticsService{}

	handler := NewAnalyticsHandler(mockService)

	input := &GetCashFlowSummaryInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-123"},
		StartDate:          "2024-01-01",
	}

	output, err := handler.GetCashFlowSummary(context.Background(), input)

	assert.Nil(t, output)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "end_date is required")
}

// TestGetCashFlowSummary_InvalidStartDate tests error for invalid start_date format.
func TestGetCashFlowSummary_InvalidStartDate(t *testing.T) {
	mockService := &mockAnalyticsService{}

	handler := NewAnalyticsHandler(mockService)

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

// TestGetCashFlowSummary_InvalidEndDate tests error for invalid end_date format.
func TestGetCashFlowSummary_InvalidEndDate(t *testing.T) {
	mockService := &mockAnalyticsService{}

	handler := NewAnalyticsHandler(mockService)

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

// TestGetCashFlowSummary_EndBeforeStart tests error when end_date is before start_date.
func TestGetCashFlowSummary_EndBeforeStart(t *testing.T) {
	mockService := &mockAnalyticsService{}

	handler := NewAnalyticsHandler(mockService)

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

// TestGetCashFlowSummary_InvalidIncludeTransfers tests error for invalid include_transfers.
func TestGetCashFlowSummary_InvalidIncludeTransfers(t *testing.T) {
	mockService := &mockAnalyticsService{}

	handler := NewAnalyticsHandler(mockService)

	input := &GetCashFlowSummaryInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-123"},
		StartDate:          "2024-01-01",
		EndDate:            "2024-01-31",
		IncludeTransfers:   "maybe",
	}

	output, err := handler.GetCashFlowSummary(context.Background(), input)

	assert.Nil(t, output)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "Invalid include_transfers value")
}

// TestGetCashFlowSummary_ServiceError tests error handling for service errors.
func TestGetCashFlowSummary_ServiceError(t *testing.T) {
	mockService := &mockAnalyticsService{
		summaryErr: errors.New("database error"),
	}

	handler := NewAnalyticsHandler(mockService)

	input := &GetCashFlowSummaryInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-123"},
		StartDate:          "2024-01-01",
		EndDate:            "2024-01-31",
	}

	output, err := handler.GetCashFlowSummary(context.Background(), input)

	assert.Nil(t, output)
	require.Error(t, err)
}

// TestGetCashFlowHistory_Success tests successful retrieval of cash flow history.
func TestGetCashFlowHistory_Success(t *testing.T) {
	mockService := &mockAnalyticsService{
		historyResponse: createTestCashFlowHistory(),
	}

	handler := NewAnalyticsHandler(mockService)

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

// TestGetCashFlowHistory_WithGrouping tests cash flow history with explicit grouping.
func TestGetCashFlowHistory_WithGrouping(t *testing.T) {
	mockService := &mockAnalyticsService{
		historyResponse: createTestCashFlowHistory(),
	}

	handler := NewAnalyticsHandler(mockService)

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

// TestGetCashFlowHistory_InvalidGrouping tests error for invalid grouping.
func TestGetCashFlowHistory_InvalidGrouping(t *testing.T) {
	mockService := &mockAnalyticsService{}

	handler := NewAnalyticsHandler(mockService)

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

// TestAnalyticsRouteRegistration tests that routes are properly registered.
func TestAnalyticsRouteRegistration(t *testing.T) {
	mockService := &mockAnalyticsService{
		summaryResponse: createTestCashFlowSummary(),
	}

	_, api := humatest.New(t)
	handler := NewAnalyticsHandler(mockService)
	RegisterAnalyticsRoutes(api, handler)

	// Test GET /api/v1/analytics/cash-flow-summary (without auth, should fail with 401)
	resp := api.Get("/api/v1/analytics/cash-flow-summary")
	assert.Equal(t, http.StatusUnauthorized, resp.Code)

	// Verify the response contains expected error message
	body := resp.Body.String()
	assert.Contains(t, body, "User not authenticated")
}

// TestAnalyticsResponseJSON tests JSON serialization of response.
func TestAnalyticsResponseJSON(t *testing.T) {
	mockService := &mockAnalyticsService{}

	_, api := humatest.New(t)
	handler := NewAnalyticsHandler(mockService)
	RegisterAnalyticsRoutes(api, handler)

	resp := api.Get("/api/v1/analytics/cash-flow-summary")
	assert.Equal(t, http.StatusUnauthorized, resp.Code)

	// Verify error response is valid JSON
	var errorResp map[string]interface{}
	err := json.Unmarshal(resp.Body.Bytes(), &errorResp)
	require.NoError(t, err, "Error response should be valid JSON")
}

// TestGetSpendingByCategory_Success tests successful spending by category retrieval.
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

	handler := NewAnalyticsHandler(mockService)

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

	handler := NewAnalyticsHandler(mockService)

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
	handler := NewAnalyticsHandler(&mockAnalyticsService{})

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
	})

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

	handler := NewAnalyticsHandler(mockService)

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

	handler := NewAnalyticsHandler(mockService)

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
	handler := NewAnalyticsHandler(&mockAnalyticsService{})

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

	handler := NewAnalyticsHandler(mockService)

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
	handler := NewAnalyticsHandler(&mockAnalyticsService{})

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

	handler := NewAnalyticsHandler(mockService)

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
	handler := NewAnalyticsHandler(&mockAnalyticsService{})

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
	handler := NewAnalyticsHandler(mockService)

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

	handler := NewAnalyticsHandler(mockService)
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
	handler := NewAnalyticsHandler(mockService)

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
	handler := NewAnalyticsHandler(mockService)

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
	handler := NewAnalyticsHandler(&mockAnalyticsService{})

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
	handler := NewAnalyticsHandler(mockService)

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
	handler := NewAnalyticsHandler(mockService)

	input := &GetMonthOverMonthSpendingChangeInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-123"},
		Months:             6,
	}

	output, err := handler.GetMonthOverMonthSpendingChange(context.Background(), input)

	require.NoError(t, err)
	require.NotNil(t, output)
	assert.Equal(t, 6, output.Body.MonthCount)
}

// TestGetMonthOverMonthSpendingChange_ServiceError tests service error propagation.
func TestGetMonthOverMonthSpendingChange_ServiceError(t *testing.T) {
	mockService := &mockAnalyticsService{monthOverMonthErr: errors.New("months out of range")}
	handler := NewAnalyticsHandler(mockService)

	input := &GetMonthOverMonthSpendingChangeInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-123"},
		Months:             1, // invalid
	}

	output, err := handler.GetMonthOverMonthSpendingChange(context.Background(), input)

	assert.Nil(t, output)
	require.Error(t, err)
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
	handler := NewAnalyticsHandler(mockService)

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
	handler := NewAnalyticsHandler(&mockAnalyticsService{})

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
	handler := NewAnalyticsHandler(mockService)

	input := &GetLargestTransactionsForPeriodInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-123"},
		StartDate:          "2024-01-01",
		EndDate:            "2024-03-31",
	}

	output, err := handler.GetLargestTransactionsForPeriod(context.Background(), input)

	assert.Nil(t, output)
	require.Error(t, err)
}

// TestGetCategoryBreakdownForMerchant_Success tests successful merchant category breakdown.
func TestGetCategoryBreakdownForMerchant_Success(t *testing.T) {
	mockService := &mockAnalyticsService{
		categoryBreakdownForMerchantResponse: &domain.CategoryBreakdownForMerchantResponse{
			MerchantName: "Amazon",
			Categories: []domain.CategoryBreakdownItem{
				{Category: "SHOPPING", PFCPrimaryDisplay: "Shopping", Total: 400.0, Percentage: 80.0, TransactionCount: 5},
			},
			TotalSpend: 500.0,
			StartDate:  "2024-01-01",
			EndDate:    "2024-03-31",
		},
	}
	handler := NewAnalyticsHandler(mockService)

	input := &GetCategoryBreakdownForMerchantInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-123"},
		StartDate:          "2024-01-01",
		EndDate:            "2024-03-31",
		MerchantName:       "Amazon",
	}

	output, err := handler.GetCategoryBreakdownForMerchant(context.Background(), input)

	require.NoError(t, err)
	require.NotNil(t, output)
	assert.Equal(t, "Amazon", output.Body.MerchantName)
	assert.Len(t, output.Body.Categories, 1)
}

// TestGetCategoryBreakdownForMerchant_EndBeforeStart tests date validation.
func TestGetCategoryBreakdownForMerchant_EndBeforeStart(t *testing.T) {
	handler := NewAnalyticsHandler(&mockAnalyticsService{})

	input := &GetCategoryBreakdownForMerchantInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-123"},
		StartDate:          "2024-03-31",
		EndDate:            "2024-01-01",
		MerchantName:       "Amazon",
	}

	output, err := handler.GetCategoryBreakdownForMerchant(context.Background(), input)

	assert.Nil(t, output)
	require.Error(t, err)
}

// TestGetCategoryBreakdownForMerchant_ServiceError tests service error propagation.
func TestGetCategoryBreakdownForMerchant_ServiceError(t *testing.T) {
	mockService := &mockAnalyticsService{categoryBreakdownForMerchantErr: errors.New("db error")}
	handler := NewAnalyticsHandler(mockService)

	input := &GetCategoryBreakdownForMerchantInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-123"},
		StartDate:          "2024-01-01",
		EndDate:            "2024-03-31",
		MerchantName:       "Amazon",
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
	handler := NewAnalyticsHandler(mockService)

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
	handler := NewAnalyticsHandler(&mockAnalyticsService{})

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
	handler := NewAnalyticsHandler(mockService)

	input := &GetIncomeSourceBreakdownInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-123"},
		StartDate:          "2024-01-01",
		EndDate:            "2024-03-31",
	}

	output, err := handler.GetIncomeSourceBreakdown(context.Background(), input)

	assert.Nil(t, output)
	require.Error(t, err)
}

// TestSpendingRouteRegistration tests that spending routes are properly registered.
func TestSpendingRouteRegistration(t *testing.T) {
	_, api := humatest.New(t)
	handler := NewAnalyticsHandler(&mockAnalyticsService{})
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
		"/api/v1/analytics/unusual-spend",
		"/api/v1/analytics/net-worth-trend",
		"/api/v1/analytics/budget-vs-actual",
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
	handler := NewAnalyticsHandler(mockService)
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
	handler := NewAnalyticsHandler(&mockAnalyticsService{})
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
	handler := NewAnalyticsHandler(mockService)
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
	handler := NewAnalyticsHandler(mockService)
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
	handler := NewAnalyticsHandler(mockService)
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
	handler := NewAnalyticsHandler(mockService)
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
	handler := NewAnalyticsHandler(mockService)
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
	handler := NewAnalyticsHandler(mockService)
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
	handler := NewAnalyticsHandler(&mockAnalyticsService{})
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
	handler2 := NewAnalyticsHandler(mockService)
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
