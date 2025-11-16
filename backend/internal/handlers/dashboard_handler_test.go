package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

// MockDashboardService is a mock implementation of DashboardService for testing
type MockDashboardService struct {
	mock.Mock
}

func (m *MockDashboardService) GetBalanceSummary(ctx context.Context, userID string, filters *domain.DashboardFilters) (*domain.BalanceSummaryResponse, error) {
	args := m.Called(ctx, userID, filters)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*domain.BalanceSummaryResponse), args.Error(1)
}

func (m *MockDashboardService) GetBalanceHistory(ctx context.Context, userID string, filters *domain.DashboardFilters) (*domain.BalanceHistoryResponse, error) {
	args := m.Called(ctx, userID, filters)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*domain.BalanceHistoryResponse), args.Error(1)
}

func (m *MockDashboardService) GetAccountTypes(ctx context.Context, userID string) (*domain.AccountTypesResponse, error) {
	args := m.Called(ctx, userID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*domain.AccountTypesResponse), args.Error(1)
}

// TestGetBalanceSummary_Success tests successful balance summary retrieval
func TestGetBalanceSummary_Success(t *testing.T) {
	// Setup
	e := echo.New()
	mockService := new(MockDashboardService)
	handler := NewDashboardHandler(mockService)

	// Create test request
	req := httptest.NewRequest(http.MethodGet, "/api/v1/dashboard/balance-summary", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	// Set user ID in context (simulating auth middleware)
	userID := "test-user-123"
	c.Set("userID", userID)

	// Mock response
	asOfDate := time.Now().UTC()
	mockResponse := &domain.BalanceSummaryResponse{
		AsOfDate:     asOfDate,
		TotalBalance: 5000.00,
		ByType: map[string]*domain.AccountTypeBalance{
			"depository": {
				Balance:      3000.00,
				AccountCount: 2,
				Accounts: []domain.AccountBalanceDetail{
					{ID: "acc-1", Name: "Checking", Balance: 1500.00},
					{ID: "acc-2", Name: "Savings", Balance: 1500.00},
				},
			},
			"credit": {
				Balance:      -500.00,
				AccountCount: 1,
				Accounts: []domain.AccountBalanceDetail{
					{ID: "acc-3", Name: "Credit Card", Balance: -500.00},
				},
			},
		},
		PeriodComparison: &domain.PeriodComparisonData{
			PreviousBalance: 4800.00,
			ChangeAmount:    200.00,
			ChangePercent:   4.17,
		},
	}

	mockService.On("GetBalanceSummary", mock.Anything, userID, mock.Anything).Return(mockResponse, nil)

	// Execute
	err := handler.GetBalanceSummary(c)

	// Assert
	assert.NoError(t, err)
	assert.Equal(t, http.StatusOK, rec.Code)

	var response domain.BalanceSummaryResponse
	json.Unmarshal(rec.Body.Bytes(), &response)
	assert.Equal(t, 5000.00, response.TotalBalance)
	assert.Contains(t, response.ByType, "depository")
	assert.Contains(t, response.ByType, "credit")
	assert.NotNil(t, response.PeriodComparison)

	mockService.AssertExpectations(t)
}

// TestGetBalanceSummary_WithQueryParams tests balance summary with query parameters
func TestGetBalanceSummary_WithQueryParams(t *testing.T) {
	// Setup
	e := echo.New()
	mockService := new(MockDashboardService)
	handler := NewDashboardHandler(mockService)

	// Create test request with query params
	req := httptest.NewRequest(http.MethodGet, "/api/v1/dashboard/balance-summary?account_types=depository,investment&as_of_date=2024-01-15", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	userID := "test-user-123"
	c.Set("userID", userID)

	// Mock response
	mockResponse := &domain.BalanceSummaryResponse{
		AsOfDate:     time.Date(2024, 1, 15, 0, 0, 0, 0, time.UTC),
		TotalBalance: 3000.00,
		ByType: map[string]*domain.AccountTypeBalance{
			"depository": {Balance: 2000.00, AccountCount: 1},
			"investment": {Balance: 1000.00, AccountCount: 1},
		},
	}

	// Expect filters to be passed
	mockService.On("GetBalanceSummary", mock.Anything, userID, mock.MatchedBy(func(filters *domain.DashboardFilters) bool {
		return filters != nil &&
			len(filters.AccountTypes) == 2 &&
			filters.AccountTypes[0] == "depository" &&
			filters.AccountTypes[1] == "investment" &&
			filters.AsOfDate != nil
	})).Return(mockResponse, nil)

	// Execute
	err := handler.GetBalanceSummary(c)

	// Assert
	assert.NoError(t, err)
	assert.Equal(t, http.StatusOK, rec.Code)

	var response domain.BalanceSummaryResponse
	json.Unmarshal(rec.Body.Bytes(), &response)
	assert.Equal(t, 3000.00, response.TotalBalance)

	mockService.AssertExpectations(t)
}

// TestGetBalanceSummary_Unauthorized tests unauthorized access
func TestGetBalanceSummary_Unauthorized(t *testing.T) {
	// Setup
	e := echo.New()
	mockService := new(MockDashboardService)
	handler := NewDashboardHandler(mockService)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/dashboard/balance-summary", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	// No userID in context (simulating missing auth)

	// Execute
	err := handler.GetBalanceSummary(c)

	// Assert
	assert.Error(t, err)
	httpError, ok := err.(*echo.HTTPError)
	assert.True(t, ok)
	assert.Equal(t, http.StatusUnauthorized, httpError.Code)
}

// TestGetBalanceSummary_ServiceError tests service layer error handling
func TestGetBalanceSummary_ServiceError(t *testing.T) {
	// Setup
	e := echo.New()
	mockService := new(MockDashboardService)
	handler := NewDashboardHandler(mockService)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/dashboard/balance-summary", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	userID := "test-user-123"
	c.Set("userID", userID)

	// Mock service error
	mockService.On("GetBalanceSummary", mock.Anything, userID, mock.Anything).Return(nil, errors.New("database connection failed"))

	// Execute
	err := handler.GetBalanceSummary(c)

	// Assert
	assert.Error(t, err)
	httpError, ok := err.(*echo.HTTPError)
	assert.True(t, ok)
	assert.Equal(t, http.StatusInternalServerError, httpError.Code)

	mockService.AssertExpectations(t)
}

// TestGetBalanceHistory_Success tests successful balance history retrieval
func TestGetBalanceHistory_Success(t *testing.T) {
	// Setup
	e := echo.New()
	mockService := new(MockDashboardService)
	handler := NewDashboardHandler(mockService)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/dashboard/balance-history?start_date=2024-01-01&end_date=2024-01-31", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	userID := "test-user-123"
	c.Set("userID", userID)

	// Mock response
	mockResponse := &domain.BalanceHistoryResponse{
		StartDate:   time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
		EndDate:     time.Date(2024, 1, 31, 0, 0, 0, 0, time.UTC),
		Granularity: "daily",
		Snapshots: []domain.BalanceHistorySnapshot{
			{
				Date:    time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
				Balance: 4500.00,
				ByType:  map[string]float64{"depository": 3000.00, "credit": -500.00},
			},
			{
				Date:    time.Date(2024, 1, 15, 0, 0, 0, 0, time.UTC),
				Balance: 4800.00,
				ByType:  map[string]float64{"depository": 3300.00, "credit": -500.00},
			},
		},
	}

	mockService.On("GetBalanceHistory", mock.Anything, userID, mock.Anything).Return(mockResponse, nil)

	// Execute
	err := handler.GetBalanceHistory(c)

	// Assert
	assert.NoError(t, err)
	assert.Equal(t, http.StatusOK, rec.Code)

	var response domain.BalanceHistoryResponse
	json.Unmarshal(rec.Body.Bytes(), &response)
	assert.Equal(t, "daily", response.Granularity)
	assert.Len(t, response.Snapshots, 2)
	assert.Equal(t, 4500.00, response.Snapshots[0].Balance)

	mockService.AssertExpectations(t)
}

// TestGetBalanceHistory_InvalidDateFormat tests invalid date format handling
func TestGetBalanceHistory_InvalidDateFormat(t *testing.T) {
	// Setup
	e := echo.New()
	mockService := new(MockDashboardService)
	handler := NewDashboardHandler(mockService)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/dashboard/balance-history?start_date=invalid-date", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	userID := "test-user-123"
	c.Set("userID", userID)

	// Execute
	err := handler.GetBalanceHistory(c)

	// Assert
	assert.Error(t, err)
	httpError, ok := err.(*echo.HTTPError)
	assert.True(t, ok)
	assert.Equal(t, http.StatusBadRequest, httpError.Code)
}

// TestGetAccountTypes_Success tests successful account types retrieval
func TestGetAccountTypes_Success(t *testing.T) {
	// Setup
	e := echo.New()
	mockService := new(MockDashboardService)
	handler := NewDashboardHandler(mockService)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/dashboard/account-types", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	userID := "test-user-123"
	c.Set("userID", userID)

	// Mock response
	mockResponse := &domain.AccountTypesResponse{
		Types: []domain.AccountTypeInfo{
			{Key: "depository", Label: "Checking & Savings", Count: 2, TotalBalance: 3000.00},
			{Key: "credit", Label: "Credit Cards", Count: 1, TotalBalance: -500.00},
			{Key: "investment", Label: "Investments", Count: 1, TotalBalance: 5000.00},
		},
	}

	mockService.On("GetAccountTypes", mock.Anything, userID).Return(mockResponse, nil)

	// Execute
	err := handler.GetAccountTypes(c)

	// Assert
	assert.NoError(t, err)
	assert.Equal(t, http.StatusOK, rec.Code)

	var response domain.AccountTypesResponse
	json.Unmarshal(rec.Body.Bytes(), &response)
	assert.Len(t, response.Types, 3)
	assert.Equal(t, "depository", response.Types[0].Key)
	assert.Equal(t, "Checking & Savings", response.Types[0].Label)

	mockService.AssertExpectations(t)
}

// TestGetAccountTypes_Unauthorized tests unauthorized access
func TestGetAccountTypes_Unauthorized(t *testing.T) {
	// Setup
	e := echo.New()
	mockService := new(MockDashboardService)
	handler := NewDashboardHandler(mockService)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/dashboard/account-types", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	// No userID in context

	// Execute
	err := handler.GetAccountTypes(c)

	// Assert
	assert.Error(t, err)
	httpError, ok := err.(*echo.HTTPError)
	assert.True(t, ok)
	assert.Equal(t, http.StatusUnauthorized, httpError.Code)
}
