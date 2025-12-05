package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

// MockDashboardPreferencesService is a mock implementation of DashboardPreferencesService for testing
type MockDashboardPreferencesService struct {
	mock.Mock
}

func (m *MockDashboardPreferencesService) GetPreferences(ctx context.Context, userID string) (*domain.DashboardPreferencesResponse, error) {
	args := m.Called(ctx, userID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*domain.DashboardPreferencesResponse), args.Error(1)
}

func (m *MockDashboardPreferencesService) SavePreferences(ctx context.Context, userID string, layout domain.DashboardLayout) (*domain.DashboardPreferencesResponse, error) {
	args := m.Called(ctx, userID, layout)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*domain.DashboardPreferencesResponse), args.Error(1)
}

func (m *MockDashboardPreferencesService) ResetToDefault(ctx context.Context, userID string) (*domain.DashboardPreferencesResponse, error) {
	args := m.Called(ctx, userID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*domain.DashboardPreferencesResponse), args.Error(1)
}

// TestGetPreferences_Success tests successful preferences retrieval
func TestGetPreferences_Success(t *testing.T) {
	// Setup
	e := echo.New()
	mockService := new(MockDashboardPreferencesService)
	handler := NewDashboardPreferencesHandler(mockService)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/dashboard/preferences", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	// Set user ID in context (simulating auth middleware)
	userID := "test-user-123"
	c.Set("user_id", userID)

	// Mock response
	testTime := time.Date(2024, 3, 15, 10, 0, 0, 0, time.UTC)
	mockResponse := &domain.DashboardPreferencesResponse{
		Layout: domain.DashboardLayout{
			Widgets: []domain.WidgetConfig{
				{ID: "w1", Type: domain.WidgetTypeBalanceOverview, X: 0, Y: 0, W: 4, H: 2},
				{ID: "w2", Type: domain.WidgetTypeCashFlowSummary, X: 4, Y: 0, W: 8, H: 2},
			},
			Version: 1,
		},
		UpdatedAt: testTime,
	}

	mockService.On("GetPreferences", mock.Anything, userID).Return(mockResponse, nil)

	// Execute
	err := handler.GetPreferences(c)

	// Assert
	assert.NoError(t, err)
	assert.Equal(t, http.StatusOK, rec.Code)

	var response domain.DashboardPreferencesResponse
	json.Unmarshal(rec.Body.Bytes(), &response)
	assert.Len(t, response.Layout.Widgets, 2)
	assert.Equal(t, "w1", response.Layout.Widgets[0].ID)
	assert.Equal(t, domain.WidgetTypeBalanceOverview, response.Layout.Widgets[0].Type)

	mockService.AssertExpectations(t)
}

// TestGetPreferences_DefaultLayout tests getting default layout for new user
func TestGetPreferences_DefaultLayout(t *testing.T) {
	// Setup
	e := echo.New()
	mockService := new(MockDashboardPreferencesService)
	handler := NewDashboardPreferencesHandler(mockService)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/dashboard/preferences", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	userID := "new-user-123"
	c.Set("user_id", userID)

	// Mock response with default layout
	defaultLayout := domain.DefaultDashboardLayout()
	mockResponse := &domain.DashboardPreferencesResponse{
		Layout: *defaultLayout,
		// UpdatedAt is zero for default
	}

	mockService.On("GetPreferences", mock.Anything, userID).Return(mockResponse, nil)

	// Execute
	err := handler.GetPreferences(c)

	// Assert
	assert.NoError(t, err)
	assert.Equal(t, http.StatusOK, rec.Code)

	var response domain.DashboardPreferencesResponse
	json.Unmarshal(rec.Body.Bytes(), &response)
	assert.Len(t, response.Layout.Widgets, 5) // Default has 5 widgets

	mockService.AssertExpectations(t)
}

// TestGetPreferences_Unauthorized tests unauthorized access
func TestGetPreferences_Unauthorized(t *testing.T) {
	// Setup
	e := echo.New()
	mockService := new(MockDashboardPreferencesService)
	handler := NewDashboardPreferencesHandler(mockService)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/dashboard/preferences", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	// No userID in context

	// Execute
	err := handler.GetPreferences(c)

	// Assert
	assert.Error(t, err)
	httpError, ok := err.(*echo.HTTPError)
	assert.True(t, ok)
	assert.Equal(t, http.StatusUnauthorized, httpError.Code)
}

// TestGetPreferences_ServiceError tests service layer error handling
func TestGetPreferences_ServiceError(t *testing.T) {
	// Setup
	e := echo.New()
	mockService := new(MockDashboardPreferencesService)
	handler := NewDashboardPreferencesHandler(mockService)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/dashboard/preferences", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	userID := "test-user-123"
	c.Set("user_id", userID)

	// Mock service error
	mockService.On("GetPreferences", mock.Anything, userID).Return(nil, errors.New("database error"))

	// Execute
	err := handler.GetPreferences(c)

	// Assert
	assert.Error(t, err)
	httpError, ok := err.(*echo.HTTPError)
	assert.True(t, ok)
	assert.Equal(t, http.StatusInternalServerError, httpError.Code)

	mockService.AssertExpectations(t)
}

// TestSavePreferences_Success tests successful preferences save
func TestSavePreferences_Success(t *testing.T) {
	// Setup
	e := echo.New()
	mockService := new(MockDashboardPreferencesService)
	handler := NewDashboardPreferencesHandler(mockService)

	requestBody := `{
		"layout": {
			"widgets": [
				{"i": "w1", "type": "balance-overview", "x": 0, "y": 0, "w": 6, "h": 3},
				{"i": "w2", "type": "cash-flow-chart", "x": 6, "y": 0, "w": 6, "h": 3}
			],
			"version": 1
		}
	}`

	req := httptest.NewRequest(http.MethodPut, "/api/v1/dashboard/preferences", strings.NewReader(requestBody))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	userID := "test-user-123"
	c.Set("user_id", userID)

	// Mock response
	testTime := time.Date(2024, 3, 15, 10, 0, 0, 0, time.UTC)
	mockResponse := &domain.DashboardPreferencesResponse{
		Layout: domain.DashboardLayout{
			Widgets: []domain.WidgetConfig{
				{ID: "w1", Type: domain.WidgetTypeBalanceOverview, X: 0, Y: 0, W: 6, H: 3},
				{ID: "w2", Type: domain.WidgetTypeCashFlowChart, X: 6, Y: 0, W: 6, H: 3},
			},
			Version: 1,
		},
		UpdatedAt: testTime,
	}

	mockService.On("SavePreferences", mock.Anything, userID, mock.MatchedBy(func(layout domain.DashboardLayout) bool {
		return len(layout.Widgets) == 2 && layout.Version == 1
	})).Return(mockResponse, nil)

	// Execute
	err := handler.SavePreferences(c)

	// Assert
	assert.NoError(t, err)
	assert.Equal(t, http.StatusOK, rec.Code)

	var response domain.DashboardPreferencesResponse
	json.Unmarshal(rec.Body.Bytes(), &response)
	assert.Len(t, response.Layout.Widgets, 2)
	assert.Equal(t, testTime, response.UpdatedAt)

	mockService.AssertExpectations(t)
}

// TestSavePreferences_InvalidJSON tests invalid JSON body
func TestSavePreferences_InvalidJSON(t *testing.T) {
	// Setup
	e := echo.New()
	mockService := new(MockDashboardPreferencesService)
	handler := NewDashboardPreferencesHandler(mockService)

	requestBody := `{invalid json}`

	req := httptest.NewRequest(http.MethodPut, "/api/v1/dashboard/preferences", strings.NewReader(requestBody))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	userID := "test-user-123"
	c.Set("user_id", userID)

	// Execute
	err := handler.SavePreferences(c)

	// Assert
	assert.Error(t, err)
	httpError, ok := err.(*echo.HTTPError)
	assert.True(t, ok)
	assert.Equal(t, http.StatusBadRequest, httpError.Code)
}

// TestSavePreferences_ValidationError tests invalid layout validation
func TestSavePreferences_ValidationError(t *testing.T) {
	// Setup
	e := echo.New()
	mockService := new(MockDashboardPreferencesService)
	handler := NewDashboardPreferencesHandler(mockService)

	// Empty widget ID should fail validation
	requestBody := `{
		"layout": {
			"widgets": [
				{"i": "", "type": "balance-overview", "x": 0, "y": 0, "w": 6, "h": 3}
			],
			"version": 1
		}
	}`

	req := httptest.NewRequest(http.MethodPut, "/api/v1/dashboard/preferences", strings.NewReader(requestBody))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	userID := "test-user-123"
	c.Set("user_id", userID)

	// Mock service returns validation error
	mockService.On("SavePreferences", mock.Anything, userID, mock.Anything).Return(nil, errors.New("invalid layout: widget ID is required"))

	// Execute
	err := handler.SavePreferences(c)

	// Assert
	assert.Error(t, err)
	httpError, ok := err.(*echo.HTTPError)
	assert.True(t, ok)
	assert.Equal(t, http.StatusBadRequest, httpError.Code)

	mockService.AssertExpectations(t)
}

// TestSavePreferences_Unauthorized tests unauthorized access
func TestSavePreferences_Unauthorized(t *testing.T) {
	// Setup
	e := echo.New()
	mockService := new(MockDashboardPreferencesService)
	handler := NewDashboardPreferencesHandler(mockService)

	requestBody := `{"layout": {}}`

	req := httptest.NewRequest(http.MethodPut, "/api/v1/dashboard/preferences", strings.NewReader(requestBody))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	// No userID in context

	// Execute
	err := handler.SavePreferences(c)

	// Assert
	assert.Error(t, err)
	httpError, ok := err.(*echo.HTTPError)
	assert.True(t, ok)
	assert.Equal(t, http.StatusUnauthorized, httpError.Code)
}

// TestResetPreferences_Success tests successful preferences reset
func TestResetPreferences_Success(t *testing.T) {
	// Setup
	e := echo.New()
	mockService := new(MockDashboardPreferencesService)
	handler := NewDashboardPreferencesHandler(mockService)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/dashboard/preferences/reset", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	userID := "test-user-123"
	c.Set("user_id", userID)

	// Mock response with default layout
	defaultLayout := domain.DefaultDashboardLayout()
	testTime := time.Date(2024, 3, 15, 10, 0, 0, 0, time.UTC)
	mockResponse := &domain.DashboardPreferencesResponse{
		Layout:    *defaultLayout,
		UpdatedAt: testTime,
	}

	mockService.On("ResetToDefault", mock.Anything, userID).Return(mockResponse, nil)

	// Execute
	err := handler.ResetPreferences(c)

	// Assert
	assert.NoError(t, err)
	assert.Equal(t, http.StatusOK, rec.Code)

	var response domain.DashboardPreferencesResponse
	json.Unmarshal(rec.Body.Bytes(), &response)
	assert.Len(t, response.Layout.Widgets, 5) // Default has 5 widgets
	assert.Equal(t, 1, response.Layout.Version)

	mockService.AssertExpectations(t)
}

// TestResetPreferences_Unauthorized tests unauthorized access
func TestResetPreferences_Unauthorized(t *testing.T) {
	// Setup
	e := echo.New()
	mockService := new(MockDashboardPreferencesService)
	handler := NewDashboardPreferencesHandler(mockService)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/dashboard/preferences/reset", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	// No userID in context

	// Execute
	err := handler.ResetPreferences(c)

	// Assert
	assert.Error(t, err)
	httpError, ok := err.(*echo.HTTPError)
	assert.True(t, ok)
	assert.Equal(t, http.StatusUnauthorized, httpError.Code)
}

// TestResetPreferences_ServiceError tests service layer error handling
func TestResetPreferences_ServiceError(t *testing.T) {
	// Setup
	e := echo.New()
	mockService := new(MockDashboardPreferencesService)
	handler := NewDashboardPreferencesHandler(mockService)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/dashboard/preferences/reset", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	userID := "test-user-123"
	c.Set("user_id", userID)

	// Mock service error
	mockService.On("ResetToDefault", mock.Anything, userID).Return(nil, errors.New("database error"))

	// Execute
	err := handler.ResetPreferences(c)

	// Assert
	assert.Error(t, err)
	httpError, ok := err.(*echo.HTTPError)
	assert.True(t, ok)
	assert.Equal(t, http.StatusInternalServerError, httpError.Code)

	mockService.AssertExpectations(t)
}
