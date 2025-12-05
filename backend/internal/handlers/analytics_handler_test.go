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

// MockAnalyticsService is a mock implementation of AnalyticsService for testing
type MockAnalyticsService struct {
	mock.Mock
}

func (m *MockAnalyticsService) GetCashFlowSummary(ctx context.Context, userID string, filters domain.CashFlowFilters) (*domain.CashFlowSummaryResponse, error) {
	args := m.Called(ctx, userID, filters)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*domain.CashFlowSummaryResponse), args.Error(1)
}

func (m *MockAnalyticsService) GetCashFlowHistory(ctx context.Context, userID string, filters domain.CashFlowFilters) (*domain.CashFlowHistoryResponse, error) {
	args := m.Called(ctx, userID, filters)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*domain.CashFlowHistoryResponse), args.Error(1)
}

// TestGetCashFlowSummary_Success tests successful cash flow summary retrieval
func TestGetCashFlowSummary_Success(t *testing.T) {
	// Setup
	e := echo.New()
	mockService := new(MockAnalyticsService)
	handler := NewAnalyticsHandler(mockService)

	// Create test request with required query params
	req := httptest.NewRequest(http.MethodGet, "/api/v1/analytics/cash-flow-summary?start_date=2024-03-01&end_date=2024-03-31", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	// Set user ID in context (simulating auth middleware)
	userID := "test-user-123"
	c.Set("user_id", userID)

	// Mock response
	mockResponse := &domain.CashFlowSummaryResponse{
		StartDate:    time.Date(2024, 3, 1, 0, 0, 0, 0, time.UTC),
		EndDate:      time.Date(2024, 3, 31, 0, 0, 0, 0, time.UTC),
		Income:       5000.00,
		Expenses:     3000.00,
		NetCashFlow:  2000.00,
		IncomeCount:  10,
		ExpenseCount: 25,
		PeriodComparison: &domain.CashFlowPeriodComparison{
			PreviousIncome:       4000.00,
			PreviousExpenses:     3500.00,
			PreviousNetCashFlow:  500.00,
			IncomeChangeAmount:   1000.00,
			IncomeChangePercent:  25.0,
			ExpenseChangeAmount:  -500.00,
			ExpenseChangePercent: -14.29,
			NetChangeAmount:      1500.00,
			NetChangePercent:     300.0,
		},
	}

	mockService.On("GetCashFlowSummary", mock.Anything, userID, mock.MatchedBy(func(filters domain.CashFlowFilters) bool {
		return filters.StartDate.Year() == 2024 && filters.StartDate.Month() == 3 && filters.StartDate.Day() == 1 &&
			filters.EndDate.Year() == 2024 && filters.EndDate.Month() == 3 && filters.EndDate.Day() == 31
	})).Return(mockResponse, nil)

	// Execute
	err := handler.GetCashFlowSummary(c)

	// Assert
	assert.NoError(t, err)
	assert.Equal(t, http.StatusOK, rec.Code)

	var response domain.CashFlowSummaryResponse
	json.Unmarshal(rec.Body.Bytes(), &response)
	assert.Equal(t, 5000.00, response.Income)
	assert.Equal(t, 3000.00, response.Expenses)
	assert.Equal(t, 2000.00, response.NetCashFlow)
	assert.NotNil(t, response.PeriodComparison)

	mockService.AssertExpectations(t)
}

// TestGetCashFlowSummary_WithOptionalParams tests cash flow summary with optional query parameters
func TestGetCashFlowSummary_WithOptionalParams(t *testing.T) {
	// Setup
	e := echo.New()
	mockService := new(MockAnalyticsService)
	handler := NewAnalyticsHandler(mockService)

	// Create test request with optional params
	req := httptest.NewRequest(http.MethodGet, "/api/v1/analytics/cash-flow-summary?start_date=2024-03-01&end_date=2024-03-31&account_types=depository,credit&include_transfers=true", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	userID := "test-user-123"
	c.Set("user_id", userID)

	mockResponse := &domain.CashFlowSummaryResponse{
		StartDate:   time.Date(2024, 3, 1, 0, 0, 0, 0, time.UTC),
		EndDate:     time.Date(2024, 3, 31, 0, 0, 0, 0, time.UTC),
		Income:      3000.00,
		Expenses:    2000.00,
		NetCashFlow: 1000.00,
	}

	// Expect filters to be passed with account types and include_transfers
	mockService.On("GetCashFlowSummary", mock.Anything, userID, mock.MatchedBy(func(filters domain.CashFlowFilters) bool {
		return len(filters.AccountTypes) == 2 &&
			filters.AccountTypes[0] == "depository" &&
			filters.AccountTypes[1] == "credit" &&
			filters.IncludeTransfers == true
	})).Return(mockResponse, nil)

	// Execute
	err := handler.GetCashFlowSummary(c)

	// Assert
	assert.NoError(t, err)
	assert.Equal(t, http.StatusOK, rec.Code)

	mockService.AssertExpectations(t)
}

// TestGetCashFlowSummary_MissingStartDate tests missing start_date parameter
func TestGetCashFlowSummary_MissingStartDate(t *testing.T) {
	// Setup
	e := echo.New()
	mockService := new(MockAnalyticsService)
	handler := NewAnalyticsHandler(mockService)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/analytics/cash-flow-summary?end_date=2024-03-31", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	userID := "test-user-123"
	c.Set("user_id", userID)

	// Execute
	err := handler.GetCashFlowSummary(c)

	// Assert
	assert.Error(t, err)
	httpError, ok := err.(*echo.HTTPError)
	assert.True(t, ok)
	assert.Equal(t, http.StatusBadRequest, httpError.Code)
}

// TestGetCashFlowSummary_MissingEndDate tests missing end_date parameter
func TestGetCashFlowSummary_MissingEndDate(t *testing.T) {
	// Setup
	e := echo.New()
	mockService := new(MockAnalyticsService)
	handler := NewAnalyticsHandler(mockService)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/analytics/cash-flow-summary?start_date=2024-03-01", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	userID := "test-user-123"
	c.Set("user_id", userID)

	// Execute
	err := handler.GetCashFlowSummary(c)

	// Assert
	assert.Error(t, err)
	httpError, ok := err.(*echo.HTTPError)
	assert.True(t, ok)
	assert.Equal(t, http.StatusBadRequest, httpError.Code)
}

// TestGetCashFlowSummary_InvalidDateFormat tests invalid date format
func TestGetCashFlowSummary_InvalidDateFormat(t *testing.T) {
	// Setup
	e := echo.New()
	mockService := new(MockAnalyticsService)
	handler := NewAnalyticsHandler(mockService)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/analytics/cash-flow-summary?start_date=invalid-date&end_date=2024-03-31", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	userID := "test-user-123"
	c.Set("user_id", userID)

	// Execute
	err := handler.GetCashFlowSummary(c)

	// Assert
	assert.Error(t, err)
	httpError, ok := err.(*echo.HTTPError)
	assert.True(t, ok)
	assert.Equal(t, http.StatusBadRequest, httpError.Code)
}

// TestGetCashFlowSummary_EndDateBeforeStartDate tests end_date before start_date
func TestGetCashFlowSummary_EndDateBeforeStartDate(t *testing.T) {
	// Setup
	e := echo.New()
	mockService := new(MockAnalyticsService)
	handler := NewAnalyticsHandler(mockService)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/analytics/cash-flow-summary?start_date=2024-03-31&end_date=2024-03-01", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	userID := "test-user-123"
	c.Set("user_id", userID)

	// Execute
	err := handler.GetCashFlowSummary(c)

	// Assert
	assert.Error(t, err)
	httpError, ok := err.(*echo.HTTPError)
	assert.True(t, ok)
	assert.Equal(t, http.StatusBadRequest, httpError.Code)
}

// TestGetCashFlowSummary_InvalidIncludeTransfers tests invalid include_transfers value
func TestGetCashFlowSummary_InvalidIncludeTransfers(t *testing.T) {
	// Setup
	e := echo.New()
	mockService := new(MockAnalyticsService)
	handler := NewAnalyticsHandler(mockService)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/analytics/cash-flow-summary?start_date=2024-03-01&end_date=2024-03-31&include_transfers=invalid", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	userID := "test-user-123"
	c.Set("user_id", userID)

	// Execute
	err := handler.GetCashFlowSummary(c)

	// Assert
	assert.Error(t, err)
	httpError, ok := err.(*echo.HTTPError)
	assert.True(t, ok)
	assert.Equal(t, http.StatusBadRequest, httpError.Code)
}

// TestGetCashFlowSummary_Unauthorized tests unauthorized access
func TestGetCashFlowSummary_Unauthorized(t *testing.T) {
	// Setup
	e := echo.New()
	mockService := new(MockAnalyticsService)
	handler := NewAnalyticsHandler(mockService)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/analytics/cash-flow-summary?start_date=2024-03-01&end_date=2024-03-31", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	// No userID in context (simulating missing auth)

	// Execute
	err := handler.GetCashFlowSummary(c)

	// Assert
	assert.Error(t, err)
	httpError, ok := err.(*echo.HTTPError)
	assert.True(t, ok)
	assert.Equal(t, http.StatusUnauthorized, httpError.Code)
}

// TestGetCashFlowSummary_ServiceError tests service layer error handling
func TestGetCashFlowSummary_ServiceError(t *testing.T) {
	// Setup
	e := echo.New()
	mockService := new(MockAnalyticsService)
	handler := NewAnalyticsHandler(mockService)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/analytics/cash-flow-summary?start_date=2024-03-01&end_date=2024-03-31", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	userID := "test-user-123"
	c.Set("user_id", userID)

	// Mock service error
	mockService.On("GetCashFlowSummary", mock.Anything, userID, mock.Anything).Return(nil, errors.New("database connection failed"))

	// Execute
	err := handler.GetCashFlowSummary(c)

	// Assert
	assert.Error(t, err)
	httpError, ok := err.(*echo.HTTPError)
	assert.True(t, ok)
	assert.Equal(t, http.StatusInternalServerError, httpError.Code)

	mockService.AssertExpectations(t)
}

// TestGetCashFlowHistory_Success tests successful cash flow history retrieval
func TestGetCashFlowHistory_Success(t *testing.T) {
	// Setup
	e := echo.New()
	mockService := new(MockAnalyticsService)
	handler := NewAnalyticsHandler(mockService)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/analytics/cash-flow-history?start_date=2024-03-01&end_date=2024-03-31", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	userID := "test-user-123"
	c.Set("user_id", userID)

	// Mock response
	mockResponse := &domain.CashFlowHistoryResponse{
		StartDate: time.Date(2024, 3, 1, 0, 0, 0, 0, time.UTC),
		EndDate:   time.Date(2024, 3, 31, 0, 0, 0, 0, time.UTC),
		Grouping:  "weekly",
		Periods: []domain.CashFlowPeriodData{
			{
				PeriodStart:      time.Date(2024, 3, 4, 0, 0, 0, 0, time.UTC),
				PeriodEnd:        time.Date(2024, 3, 10, 0, 0, 0, 0, time.UTC),
				Income:           1200.00,
				Expenses:         800.00,
				NetCashFlow:      400.00,
				TransactionCount: 10,
			},
			{
				PeriodStart:      time.Date(2024, 3, 11, 0, 0, 0, 0, time.UTC),
				PeriodEnd:        time.Date(2024, 3, 17, 0, 0, 0, 0, time.UTC),
				Income:           1500.00,
				Expenses:         1000.00,
				NetCashFlow:      500.00,
				TransactionCount: 12,
			},
		},
	}

	mockService.On("GetCashFlowHistory", mock.Anything, userID, mock.Anything).Return(mockResponse, nil)

	// Execute
	err := handler.GetCashFlowHistory(c)

	// Assert
	assert.NoError(t, err)
	assert.Equal(t, http.StatusOK, rec.Code)

	var response domain.CashFlowHistoryResponse
	json.Unmarshal(rec.Body.Bytes(), &response)
	assert.Equal(t, "weekly", response.Grouping)
	assert.Len(t, response.Periods, 2)
	assert.Equal(t, 1200.00, response.Periods[0].Income)

	mockService.AssertExpectations(t)
}

// TestGetCashFlowHistory_WithGrouping tests cash flow history with grouping parameter
func TestGetCashFlowHistory_WithGrouping(t *testing.T) {
	// Setup
	e := echo.New()
	mockService := new(MockAnalyticsService)
	handler := NewAnalyticsHandler(mockService)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/analytics/cash-flow-history?start_date=2024-03-01&end_date=2024-03-31&grouping=daily", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	userID := "test-user-123"
	c.Set("user_id", userID)

	mockResponse := &domain.CashFlowHistoryResponse{
		StartDate: time.Date(2024, 3, 1, 0, 0, 0, 0, time.UTC),
		EndDate:   time.Date(2024, 3, 31, 0, 0, 0, 0, time.UTC),
		Grouping:  "daily",
		Periods:   []domain.CashFlowPeriodData{},
	}

	// Expect grouping to be passed
	mockService.On("GetCashFlowHistory", mock.Anything, userID, mock.MatchedBy(func(filters domain.CashFlowFilters) bool {
		return filters.Grouping == "daily"
	})).Return(mockResponse, nil)

	// Execute
	err := handler.GetCashFlowHistory(c)

	// Assert
	assert.NoError(t, err)
	assert.Equal(t, http.StatusOK, rec.Code)

	mockService.AssertExpectations(t)
}

// TestGetCashFlowHistory_InvalidGrouping tests invalid grouping parameter
func TestGetCashFlowHistory_InvalidGrouping(t *testing.T) {
	// Setup
	e := echo.New()
	mockService := new(MockAnalyticsService)
	handler := NewAnalyticsHandler(mockService)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/analytics/cash-flow-history?start_date=2024-03-01&end_date=2024-03-31&grouping=invalid", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	userID := "test-user-123"
	c.Set("user_id", userID)

	// Execute
	err := handler.GetCashFlowHistory(c)

	// Assert
	assert.Error(t, err)
	httpError, ok := err.(*echo.HTTPError)
	assert.True(t, ok)
	assert.Equal(t, http.StatusBadRequest, httpError.Code)
}

// TestGetCashFlowHistory_Unauthorized tests unauthorized access
func TestGetCashFlowHistory_Unauthorized(t *testing.T) {
	// Setup
	e := echo.New()
	mockService := new(MockAnalyticsService)
	handler := NewAnalyticsHandler(mockService)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/analytics/cash-flow-history?start_date=2024-03-01&end_date=2024-03-31", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	// No userID in context

	// Execute
	err := handler.GetCashFlowHistory(c)

	// Assert
	assert.Error(t, err)
	httpError, ok := err.(*echo.HTTPError)
	assert.True(t, ok)
	assert.Equal(t, http.StatusUnauthorized, httpError.Code)
}

// TestGetCashFlowHistory_ServiceError tests service layer error handling
func TestGetCashFlowHistory_ServiceError(t *testing.T) {
	// Setup
	e := echo.New()
	mockService := new(MockAnalyticsService)
	handler := NewAnalyticsHandler(mockService)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/analytics/cash-flow-history?start_date=2024-03-01&end_date=2024-03-31", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	userID := "test-user-123"
	c.Set("user_id", userID)

	// Mock service error
	mockService.On("GetCashFlowHistory", mock.Anything, userID, mock.Anything).Return(nil, errors.New("database error"))

	// Execute
	err := handler.GetCashFlowHistory(c)

	// Assert
	assert.Error(t, err)
	httpError, ok := err.(*echo.HTTPError)
	assert.True(t, ok)
	assert.Equal(t, http.StatusInternalServerError, httpError.Code)

	mockService.AssertExpectations(t)
}
