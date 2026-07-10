package operations

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/humatest"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/huma/inputs"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// mockDashboardService is a mock implementation of services.DashboardService
type mockDashboardService struct {
	balanceSummary  *domain.BalanceSummaryResponse
	balanceHistory  *domain.BalanceHistoryResponse
	accountTypes    *domain.AccountTypesResponse
	netWorth        *domain.NetWorthResponse
	netWorthHistory *domain.NetWorthHistoryResponse
	summaryErr      error
	historyErr      error
	typesErr        error
	netWorthErr     error
	netWorthHistErr error
	// lastFilters captures the filters passed into the most recent scoped call, so
	// tests can assert HouseholdScope was (or wasn't) threaded through. #1369 — Phase 5.
	lastFilters *domain.DashboardFilters
}

func (m *mockDashboardService) GetBalanceSummary(ctx context.Context, userID string, filters *domain.DashboardFilters) (*domain.BalanceSummaryResponse, error) {
	m.lastFilters = filters
	if m.summaryErr != nil {
		return nil, m.summaryErr
	}
	return m.balanceSummary, nil
}

func (m *mockDashboardService) GetBalanceHistory(ctx context.Context, userID string, filters *domain.DashboardFilters) (*domain.BalanceHistoryResponse, error) {
	m.lastFilters = filters
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

func (m *mockDashboardService) GetNetWorth(ctx context.Context, userID string, filters *domain.DashboardFilters) (*domain.NetWorthResponse, error) {
	m.lastFilters = filters
	if m.netWorthErr != nil {
		return nil, m.netWorthErr
	}
	return m.netWorth, nil
}

func (m *mockDashboardService) GetNetWorthHistory(ctx context.Context, userID string, filters *domain.DashboardFilters) (*domain.NetWorthHistoryResponse, error) {
	m.lastFilters = filters
	if m.netWorthHistErr != nil {
		return nil, m.netWorthHistErr
	}
	return m.netWorthHistory, nil
}

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

func TestGetBalanceSummary_Success(t *testing.T) {
	mockService := &mockDashboardService{
		balanceSummary: createTestBalanceSummary(),
	}

	handler := NewDashboardHandler(mockService, nil)

	input := &GetBalanceSummaryInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-123"},
	}

	output, err := handler.GetBalanceSummary(context.Background(), input)

	require.NoError(t, err)
	require.NotNil(t, output)
	assert.Equal(t, 10000.00, output.Body.TotalBalance)
	assert.Len(t, output.Body.ByType, 2)
}

func TestGetBalanceSummary_WithFilters(t *testing.T) {
	mockService := &mockDashboardService{
		balanceSummary: createTestBalanceSummary(),
	}

	handler := NewDashboardHandler(mockService, nil)

	input := &GetBalanceSummaryInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-123"},
		AccountTypes:       "checking,savings",
		AsOfDate:           "2024-01-15",
	}

	output, err := handler.GetBalanceSummary(context.Background(), input)

	require.NoError(t, err)
	require.NotNil(t, output)
}

func TestGetBalanceSummary_InvalidDate(t *testing.T) {
	mockService := &mockDashboardService{
		balanceSummary: createTestBalanceSummary(),
	}

	handler := NewDashboardHandler(mockService, nil)

	input := &GetBalanceSummaryInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-123"},
		AsOfDate:           "invalid-date",
	}

	output, err := handler.GetBalanceSummary(context.Background(), input)

	assert.Nil(t, output)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "Invalid as_of_date")
}

func TestGetBalanceSummary_ServiceError(t *testing.T) {
	mockService := &mockDashboardService{
		summaryErr: errors.New("database error"),
	}

	handler := NewDashboardHandler(mockService, nil)

	input := &GetBalanceSummaryInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-123"},
	}

	output, err := handler.GetBalanceSummary(context.Background(), input)

	assert.Nil(t, output)
	require.Error(t, err)
}

func TestGetBalanceHistory_Success(t *testing.T) {
	mockService := &mockDashboardService{
		balanceHistory: createTestBalanceHistory(),
	}

	handler := NewDashboardHandler(mockService, nil)

	input := &GetBalanceHistoryInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-123"},
	}

	output, err := handler.GetBalanceHistory(context.Background(), input)

	require.NoError(t, err)
	require.NotNil(t, output)
	assert.Equal(t, "daily", output.Body.Granularity)
	assert.Len(t, output.Body.Snapshots, 2)
}

func TestGetBalanceHistory_WithFilters(t *testing.T) {
	mockService := &mockDashboardService{
		balanceHistory: createTestBalanceHistory(),
	}

	handler := NewDashboardHandler(mockService, nil)

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

func TestGetBalanceHistory_InvalidStartDate(t *testing.T) {
	mockService := &mockDashboardService{
		balanceHistory: createTestBalanceHistory(),
	}

	handler := NewDashboardHandler(mockService, nil)

	input := &GetBalanceHistoryInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-123"},
		StartDate:          "not-a-date",
	}

	output, err := handler.GetBalanceHistory(context.Background(), input)

	assert.Nil(t, output)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "Invalid start_date")
}

func TestGetBalanceHistory_InvalidEndDate(t *testing.T) {
	mockService := &mockDashboardService{
		balanceHistory: createTestBalanceHistory(),
	}

	handler := NewDashboardHandler(mockService, nil)

	input := &GetBalanceHistoryInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-123"},
		EndDate:            "not-a-date",
	}

	output, err := handler.GetBalanceHistory(context.Background(), input)

	assert.Nil(t, output)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "Invalid end_date")
}

func TestGetAccountTypes_Success(t *testing.T) {
	mockService := &mockDashboardService{
		accountTypes: createTestAccountTypes(),
	}

	handler := NewDashboardHandler(mockService, nil)

	input := &GetAccountTypesInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-123"},
	}

	output, err := handler.GetAccountTypes(context.Background(), input)

	require.NoError(t, err)
	require.NotNil(t, output)
	assert.Len(t, output.Body.Types, 2)
	assert.Equal(t, "checking", output.Body.Types[0].Key)
}

func TestGetAccountTypes_ServiceError(t *testing.T) {
	mockService := &mockDashboardService{
		typesErr: errors.New("database error"),
	}

	handler := NewDashboardHandler(mockService, nil)

	input := &GetAccountTypesInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-123"},
	}

	output, err := handler.GetAccountTypes(context.Background(), input)

	assert.Nil(t, output)
	require.Error(t, err)
}

func TestDashboardRouteRegistration(t *testing.T) {
	mockService := &mockDashboardService{
		balanceSummary: createTestBalanceSummary(),
	}

	_, api := humatest.New(t)
	handler := NewDashboardHandler(mockService, nil)
	RegisterDashboardRoutes(api, handler)

	// Test GET /api/v1/dashboard/balance-summary (without auth, should fail with 401)
	resp := api.Get("/api/v1/dashboard/balance-summary")
	assert.Equal(t, http.StatusUnauthorized, resp.Code)

	// Verify the response contains expected error message
	body := resp.Body.String()
	assert.Contains(t, body, "User not authenticated")
}

func TestDashboardDefaultLayoutRoute(t *testing.T) {
	mockService := &mockDashboardService{}

	_, api := humatest.New(t)
	handler := NewDashboardHandler(mockService, nil)
	RegisterDashboardRoutes(api, handler)

	// Test GET /api/v1/dashboard/default-layout (public, should not require auth)
	resp := api.Get("/api/v1/dashboard/default-layout")

	// Should return 200 or 500 (depending on if config file exists), but NOT 401
	assert.NotEqual(t, http.StatusUnauthorized, resp.Code, "Default layout should not require authentication")
}

func TestDashboardResponseJSON(t *testing.T) {
	mockService := &mockDashboardService{}

	_, api := humatest.New(t)
	handler := NewDashboardHandler(mockService, nil)
	RegisterDashboardRoutes(api, handler)

	resp := api.Get("/api/v1/dashboard/balance-summary")
	assert.Equal(t, http.StatusUnauthorized, resp.Code)

	// Verify error response is valid JSON
	var errorResp map[string]interface{}
	err := json.Unmarshal(resp.Body.Bytes(), &errorResp)
	require.NoError(t, err, "Error response should be valid JSON")
}

func TestGetWidgetTypes_ReturnsList(t *testing.T) {
	output, err := GetWidgetTypes(context.Background(), &GetWidgetTypesInput{})
	require.NoError(t, err)
	require.NotNil(t, output)
	require.NotNil(t, output.Body)
	assert.NotEmpty(t, output.Body.WidgetTypes, "WidgetTypes should not be empty")
}

func TestGetNetWorth_Success(t *testing.T) {
	expected := &domain.NetWorthResponse{
		NetWorth: 50000.00,
	}
	mockService := &mockDashboardService{netWorth: expected}
	handler := NewDashboardHandler(mockService, nil)

	input := &GetNetWorthInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-123"},
	}
	output, err := handler.GetNetWorth(context.Background(), input)

	require.NoError(t, err)
	require.NotNil(t, output)
	assert.Equal(t, expected, output.Body)
}

func TestGetNetWorth_InvalidDate(t *testing.T) {
	mockService := &mockDashboardService{}
	handler := NewDashboardHandler(mockService, nil)

	input := &GetNetWorthInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-123"},
		AsOfDate:           "not-a-date",
	}
	output, err := handler.GetNetWorth(context.Background(), input)

	assert.Nil(t, output)
	require.Error(t, err)
}

func TestGetNetWorth_ServiceError(t *testing.T) {
	mockService := &mockDashboardService{netWorthErr: errors.New("service failed")}
	handler := NewDashboardHandler(mockService, nil)

	input := &GetNetWorthInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-123"},
	}
	output, err := handler.GetNetWorth(context.Background(), input)

	assert.Nil(t, output)
	require.Error(t, err)
}

func TestGetNetWorthHistory_Success(t *testing.T) {
	expected := &domain.NetWorthHistoryResponse{
		Granularity: "monthly",
	}
	mockService := &mockDashboardService{netWorthHistory: expected}
	handler := NewDashboardHandler(mockService, nil)

	input := &GetNetWorthHistoryInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-123"},
		StartDate:          "2024-01-01",
		EndDate:            "2024-12-31",
	}
	output, err := handler.GetNetWorthHistory(context.Background(), input)

	require.NoError(t, err)
	require.NotNil(t, output)
	assert.Equal(t, expected, output.Body)
}

func TestGetNetWorthHistory_InvalidStartDate(t *testing.T) {
	mockService := &mockDashboardService{}
	handler := NewDashboardHandler(mockService, nil)

	input := &GetNetWorthHistoryInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-123"},
		StartDate:          "bad-date",
		EndDate:            "2024-12-31",
	}
	output, err := handler.GetNetWorthHistory(context.Background(), input)

	assert.Nil(t, output)
	require.Error(t, err)
}

func TestGetNetWorthHistory_ServiceError(t *testing.T) {
	mockService := &mockDashboardService{netWorthHistErr: errors.New("service failed")}
	handler := NewDashboardHandler(mockService, nil)

	input := &GetNetWorthHistoryInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-123"},
		StartDate:          "2024-01-01",
		EndDate:            "2024-12-31",
	}
	output, err := handler.GetNetWorthHistory(context.Background(), input)

	assert.Nil(t, output)
	require.Error(t, err)
}

// ─── #1369 — Phase 5: scope=household resolution ─────────────────────────────

func TestGetBalanceSummary_HouseholdScope_ResolvesAndAttachesToFilters(t *testing.T) {
	mockService := &mockDashboardService{balanceSummary: createTestBalanceSummary()}
	mockHousehold := &mockHouseholdService{}
	scope := &domain.HouseholdScope{HouseholdID: "hh-1", MemberUserIDs: []string{"user-123", "member-2"}}
	mockHousehold.On("ResolveScope", mock.Anything, "user-123", "household").Return(scope, nil)

	handler := NewDashboardHandler(mockService, mockHousehold)
	input := &GetBalanceSummaryInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-123"},
		Scope:              "household",
	}
	output, err := handler.GetBalanceSummary(context.Background(), input)

	require.NoError(t, err)
	require.NotNil(t, output)
	require.NotNil(t, mockService.lastFilters)
	assert.Same(t, scope, mockService.lastFilters.HouseholdScope, "resolved scope must be attached to the filters passed to the service")
	mockHousehold.AssertExpectations(t)
}

func TestGetBalanceSummary_HouseholdScope_NotInHousehold_Returns404(t *testing.T) {
	mockService := &mockDashboardService{balanceSummary: createTestBalanceSummary()}
	mockHousehold := &mockHouseholdService{}
	mockHousehold.On("ResolveScope", mock.Anything, "user-123", "household").Return(nil, domain.ErrHouseholdNotFound)

	handler := NewDashboardHandler(mockService, mockHousehold)
	input := &GetBalanceSummaryInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-123"},
		Scope:              "household",
	}
	output, err := handler.GetBalanceSummary(context.Background(), input)

	assert.Nil(t, output)
	require.Error(t, err)
	var statusErr huma.StatusError
	require.ErrorAs(t, err, &statusErr)
	assert.Equal(t, http.StatusNotFound, statusErr.GetStatus())
}

func TestGetBalanceSummary_PersonalScope_NeverCallsHouseholdService(t *testing.T) {
	mockService := &mockDashboardService{balanceSummary: createTestBalanceSummary()}
	mockHousehold := &mockHouseholdService{}
	// No .On(...) set up — if the handler called ResolveScope for personal scope, this
	// mock would panic ("I don't know what to return").

	handler := NewDashboardHandler(mockService, mockHousehold)
	input := &GetBalanceSummaryInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-123"},
	}
	output, err := handler.GetBalanceSummary(context.Background(), input)

	require.NoError(t, err)
	require.NotNil(t, output)
	require.NotNil(t, mockService.lastFilters)
	assert.Nil(t, mockService.lastFilters.HouseholdScope)
	mockHousehold.AssertNotCalled(t, "ResolveScope", mock.Anything, mock.Anything, mock.Anything)
}

func TestGetNetWorth_HouseholdScope_ResolvesAndAttachesToFilters(t *testing.T) {
	mockService := &mockDashboardService{netWorth: &domain.NetWorthResponse{NetWorth: 1500.00, TotalAssets: 1500.00}}
	mockHousehold := &mockHouseholdService{}
	scope := &domain.HouseholdScope{HouseholdID: "hh-1", MemberUserIDs: []string{"user-123", "member-2"}}
	mockHousehold.On("ResolveScope", mock.Anything, "user-123", "household").Return(scope, nil)

	handler := NewDashboardHandler(mockService, mockHousehold)
	input := &GetNetWorthInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-123"},
		Scope:              "household",
	}
	output, err := handler.GetNetWorth(context.Background(), input)

	require.NoError(t, err)
	require.NotNil(t, output)
	require.NotNil(t, mockService.lastFilters)
	assert.Same(t, scope, mockService.lastFilters.HouseholdScope)
}

func TestGetBalanceHistory_HouseholdScope_ResolvesAndAttachesToFilters(t *testing.T) {
	mockService := &mockDashboardService{balanceHistory: createTestBalanceHistory()}
	mockHousehold := &mockHouseholdService{}
	scope := &domain.HouseholdScope{HouseholdID: "hh-1", MemberUserIDs: []string{"user-123", "member-2"}}
	mockHousehold.On("ResolveScope", mock.Anything, "user-123", "household").Return(scope, nil)

	handler := NewDashboardHandler(mockService, mockHousehold)
	input := &GetBalanceHistoryInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-123"},
		Scope:              "household",
	}
	output, err := handler.GetBalanceHistory(context.Background(), input)

	require.NoError(t, err)
	require.NotNil(t, output)
	require.NotNil(t, mockService.lastFilters)
	assert.Same(t, scope, mockService.lastFilters.HouseholdScope)
}

func TestGetNetWorthHistory_HouseholdScope_ResolvesAndAttachesToFilters(t *testing.T) {
	mockService := &mockDashboardService{netWorthHistory: &domain.NetWorthHistoryResponse{Snapshots: []domain.NetWorthHistorySnapshot{{NetWorth: 1500.00}}}}
	mockHousehold := &mockHouseholdService{}
	scope := &domain.HouseholdScope{HouseholdID: "hh-1", MemberUserIDs: []string{"user-123", "member-2"}}
	mockHousehold.On("ResolveScope", mock.Anything, "user-123", "household").Return(scope, nil)

	handler := NewDashboardHandler(mockService, mockHousehold)
	input := &GetNetWorthHistoryInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-123"},
		StartDate:          "2024-01-01",
		EndDate:            "2024-12-31",
		Scope:              "household",
	}
	output, err := handler.GetNetWorthHistory(context.Background(), input)

	require.NoError(t, err)
	require.NotNil(t, output)
	require.NotNil(t, mockService.lastFilters)
	assert.Same(t, scope, mockService.lastFilters.HouseholdScope)
}
