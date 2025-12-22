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

// mockDashboardService is a mock implementation of services.DashboardService
type mockDashboardService struct {
	balanceSummary   *domain.BalanceSummaryResponse
	balanceHistory   *domain.BalanceHistoryResponse
	accountTypes     *domain.AccountTypesResponse
	summaryErr       error
	historyErr       error
	typesErr         error
}

func (m *mockDashboardService) GetBalanceSummary(ctx context.Context, userID string, filters *domain.DashboardFilters) (*domain.BalanceSummaryResponse, error) {
	if m.summaryErr != nil {
		return nil, m.summaryErr
	}
	return m.balanceSummary, nil
}

func (m *mockDashboardService) GetBalanceHistory(ctx context.Context, userID string, filters *domain.DashboardFilters) (*domain.BalanceHistoryResponse, error) {
	if m.historyErr != nil {
		return nil, m.historyErr
	}
	return m.balanceHistory, nil
}

func (m *mockDashboardService) GetAccountTypes(ctx context.Context, userID string) (*domain.AccountTypesResponse, error) {
	if m.typesErr != nil {
		return nil, m.typesErr
	}
	return m.accountTypes, nil
}

// Helper functions to create test data
func createTestBalanceSummary() *domain.BalanceSummaryResponse {
	return &domain.BalanceSummaryResponse{
		AsOfDate:     time.Now(),
		TotalBalance: 10000.00,
		ByType: map[string]*domain.AccountTypeBalance{
			"checking": {
				Balance:      5000.00,
				AccountCount: 1,
				Accounts:     []domain.AccountBalanceDetail{},
			},
			"savings": {
				Balance:      5000.00,
				AccountCount: 1,
				Accounts:     []domain.AccountBalanceDetail{},
			},
		},
	}
}

func createTestBalanceHistory() *domain.BalanceHistoryResponse {
	return &domain.BalanceHistoryResponse{
		StartDate:   time.Now().AddDate(0, -1, 0),
		EndDate:     time.Now(),
		Granularity: "daily",
		Snapshots: []domain.BalanceHistorySnapshot{
			{
				Date:    time.Now().AddDate(0, 0, -1),
				Balance: 9500.00,
			},
			{
				Date:    time.Now(),
				Balance: 10000.00,
			},
		},
	}
}

func createTestAccountTypes() *domain.AccountTypesResponse {
	return &domain.AccountTypesResponse{
		Types: []domain.AccountTypeInfo{
			{Key: "checking", Label: "Checking", Count: 1, TotalBalance: 5000.00},
			{Key: "savings", Label: "Savings", Count: 1, TotalBalance: 5000.00},
		},
	}
}

// TestGetBalanceSummary_Success tests successful retrieval of balance summary.
func TestGetBalanceSummary_Success(t *testing.T) {
	mockService := &mockDashboardService{
		balanceSummary: createTestBalanceSummary(),
	}

	handler := NewDashboardHandler(mockService)

	input := &GetBalanceSummaryInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-123"},
	}

	output, err := handler.GetBalanceSummary(context.Background(), input)

	require.NoError(t, err)
	require.NotNil(t, output)
	assert.Equal(t, 10000.00, output.Body.TotalBalance)
	assert.Len(t, output.Body.ByType, 2)
}

// TestGetBalanceSummary_WithFilters tests balance summary with account type filters.
func TestGetBalanceSummary_WithFilters(t *testing.T) {
	mockService := &mockDashboardService{
		balanceSummary: createTestBalanceSummary(),
	}

	handler := NewDashboardHandler(mockService)

	input := &GetBalanceSummaryInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-123"},
		AccountTypes:       "checking,savings",
		AsOfDate:           "2024-01-15",
	}

	output, err := handler.GetBalanceSummary(context.Background(), input)

	require.NoError(t, err)
	require.NotNil(t, output)
}

// TestGetBalanceSummary_InvalidDate tests error handling for invalid date format.
func TestGetBalanceSummary_InvalidDate(t *testing.T) {
	mockService := &mockDashboardService{
		balanceSummary: createTestBalanceSummary(),
	}

	handler := NewDashboardHandler(mockService)

	input := &GetBalanceSummaryInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-123"},
		AsOfDate:           "invalid-date",
	}

	output, err := handler.GetBalanceSummary(context.Background(), input)

	assert.Nil(t, output)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "Invalid as_of_date")
}

// TestGetBalanceSummary_ServiceError tests error handling for service errors.
func TestGetBalanceSummary_ServiceError(t *testing.T) {
	mockService := &mockDashboardService{
		summaryErr: errors.New("database error"),
	}

	handler := NewDashboardHandler(mockService)

	input := &GetBalanceSummaryInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-123"},
	}

	output, err := handler.GetBalanceSummary(context.Background(), input)

	assert.Nil(t, output)
	require.Error(t, err)
}

// TestGetBalanceHistory_Success tests successful retrieval of balance history.
func TestGetBalanceHistory_Success(t *testing.T) {
	mockService := &mockDashboardService{
		balanceHistory: createTestBalanceHistory(),
	}

	handler := NewDashboardHandler(mockService)

	input := &GetBalanceHistoryInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-123"},
	}

	output, err := handler.GetBalanceHistory(context.Background(), input)

	require.NoError(t, err)
	require.NotNil(t, output)
	assert.Equal(t, "daily", output.Body.Granularity)
	assert.Len(t, output.Body.Snapshots, 2)
}

// TestGetBalanceHistory_WithFilters tests balance history with filters.
func TestGetBalanceHistory_WithFilters(t *testing.T) {
	mockService := &mockDashboardService{
		balanceHistory: createTestBalanceHistory(),
	}

	handler := NewDashboardHandler(mockService)

	input := &GetBalanceHistoryInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-123"},
		AccountTypes:       "checking",
		StartDate:          "2024-01-01",
		EndDate:            "2024-01-31",
		Granularity:        "weekly",
	}

	output, err := handler.GetBalanceHistory(context.Background(), input)

	require.NoError(t, err)
	require.NotNil(t, output)
}

// TestGetBalanceHistory_InvalidStartDate tests error handling for invalid start_date.
func TestGetBalanceHistory_InvalidStartDate(t *testing.T) {
	mockService := &mockDashboardService{
		balanceHistory: createTestBalanceHistory(),
	}

	handler := NewDashboardHandler(mockService)

	input := &GetBalanceHistoryInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-123"},
		StartDate:          "not-a-date",
	}

	output, err := handler.GetBalanceHistory(context.Background(), input)

	assert.Nil(t, output)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "Invalid start_date")
}

// TestGetBalanceHistory_InvalidEndDate tests error handling for invalid end_date.
func TestGetBalanceHistory_InvalidEndDate(t *testing.T) {
	mockService := &mockDashboardService{
		balanceHistory: createTestBalanceHistory(),
	}

	handler := NewDashboardHandler(mockService)

	input := &GetBalanceHistoryInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-123"},
		EndDate:            "not-a-date",
	}

	output, err := handler.GetBalanceHistory(context.Background(), input)

	assert.Nil(t, output)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "Invalid end_date")
}

// TestGetAccountTypes_Success tests successful retrieval of account types.
func TestGetAccountTypes_Success(t *testing.T) {
	mockService := &mockDashboardService{
		accountTypes: createTestAccountTypes(),
	}

	handler := NewDashboardHandler(mockService)

	input := &GetAccountTypesInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-123"},
	}

	output, err := handler.GetAccountTypes(context.Background(), input)

	require.NoError(t, err)
	require.NotNil(t, output)
	assert.Len(t, output.Body.Types, 2)
	assert.Equal(t, "checking", output.Body.Types[0].Key)
}

// TestGetAccountTypes_ServiceError tests error handling for service errors.
func TestGetAccountTypes_ServiceError(t *testing.T) {
	mockService := &mockDashboardService{
		typesErr: errors.New("database error"),
	}

	handler := NewDashboardHandler(mockService)

	input := &GetAccountTypesInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-123"},
	}

	output, err := handler.GetAccountTypes(context.Background(), input)

	assert.Nil(t, output)
	require.Error(t, err)
}

// TestDashboardRouteRegistration tests that routes are properly registered.
func TestDashboardRouteRegistration(t *testing.T) {
	mockService := &mockDashboardService{
		balanceSummary: createTestBalanceSummary(),
	}

	_, api := humatest.New(t)
	handler := NewDashboardHandler(mockService)
	RegisterDashboardRoutes(api, handler)

	// Test GET /api/v1/dashboard/balance-summary (without auth, should fail with 401)
	resp := api.Get("/api/v1/dashboard/balance-summary")
	assert.Equal(t, http.StatusUnauthorized, resp.Code)

	// Verify the response contains expected error message
	body := resp.Body.String()
	assert.Contains(t, body, "User not authenticated")
}

// TestDashboardDefaultLayoutRoute tests the public default-layout route.
func TestDashboardDefaultLayoutRoute(t *testing.T) {
	mockService := &mockDashboardService{}

	_, api := humatest.New(t)
	handler := NewDashboardHandler(mockService)
	RegisterDashboardRoutes(api, handler)

	// Test GET /api/v1/dashboard/default-layout (public, should not require auth)
	resp := api.Get("/api/v1/dashboard/default-layout")

	// Should return 200 or 500 (depending on if config file exists), but NOT 401
	assert.NotEqual(t, http.StatusUnauthorized, resp.Code, "Default layout should not require authentication")
}

// TestDashboardResponseJSON tests JSON serialization of response.
func TestDashboardResponseJSON(t *testing.T) {
	mockService := &mockDashboardService{}

	_, api := humatest.New(t)
	handler := NewDashboardHandler(mockService)
	RegisterDashboardRoutes(api, handler)

	resp := api.Get("/api/v1/dashboard/balance-summary")
	assert.Equal(t, http.StatusUnauthorized, resp.Code)

	// Verify error response is valid JSON
	var errorResp map[string]interface{}
	err := json.Unmarshal(resp.Body.Bytes(), &errorResp)
	require.NoError(t, err, "Error response should be valid JSON")
}
