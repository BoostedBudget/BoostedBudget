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
	summaryResponse              *domain.CashFlowSummaryResponse
	historyResponse              *domain.CashFlowHistoryResponse
	spendingByCategoryResponse   *domain.SpendingByCategoryResponse
	spendingByMerchantResponse   *domain.SpendingByMerchantResponse
	spendingTrendsResponse       *domain.SpendingTrendsResponse
	summaryErr                   error
	historyErr                   error
	spendingByCategoryErr        error
	spendingByMerchantErr        error
	spendingTrendsErr            error
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

// TestSpendingRouteRegistration tests that spending routes are properly registered.
func TestSpendingRouteRegistration(t *testing.T) {
	_, api := humatest.New(t)
	handler := NewAnalyticsHandler(&mockAnalyticsService{})
	RegisterAnalyticsRoutes(api, handler)

	// All 3 spending endpoints should exist (return 401, not 404)
	paths := []string{
		"/api/v1/analytics/spending-by-category",
		"/api/v1/analytics/spending-by-merchant",
		"/api/v1/analytics/spending-trends",
	}
	for _, path := range paths {
		resp := api.Get(path)
		assert.Equal(t, http.StatusUnauthorized, resp.Code, "path %s should return 401", path)
	}
}
