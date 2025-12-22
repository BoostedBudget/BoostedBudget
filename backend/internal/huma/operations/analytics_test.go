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
	summaryResponse *domain.CashFlowSummaryResponse
	historyResponse *domain.CashFlowHistoryResponse
	summaryErr      error
	historyErr      error
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
