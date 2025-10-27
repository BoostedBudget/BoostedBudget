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
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

// MockDeletionService for testing
type MockDeletionService struct {
	mock.Mock
}

func (m *MockDeletionService) RequestDeletion(ctx context.Context, userID, email string) (string, time.Time, error) {
	args := m.Called(ctx, userID, email)
	return args.String(0), args.Get(1).(time.Time), args.Error(2)
}

func (m *MockDeletionService) CancelDeletion(ctx context.Context, token string) error {
	args := m.Called(ctx, token)
	return args.Error(0)
}

func (m *MockDeletionService) ExecuteDeletion(ctx context.Context, userID string) error {
	args := m.Called(ctx, userID)
	return args.Error(0)
}

// TestRequestDeletion_RequiresAuth tests that the endpoint requires authentication
func TestRequestDeletion_RequiresAuth(t *testing.T) {
	e := echo.New()
	mockService := new(MockDeletionService)

	handler := NewDeletionHandler(mockService)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/user/delete", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	// No user_id in context (not authenticated)
	err := handler.RequestDeletion(c)

	// Should return error indicating auth is required
	assert.Error(t, err)
	httpError, ok := err.(*echo.HTTPError)
	assert.True(t, ok)
	assert.Equal(t, http.StatusUnauthorized, httpError.Code)
}

// TestRequestDeletion_Success tests successful deletion request
func TestRequestDeletion_Success(t *testing.T) {
	e := echo.New()
	mockService := new(MockDeletionService)

	handler := NewDeletionHandler(mockService)

	userID := "user-123"
	email := "test@example.com"
	token := "cancellation-token-xyz"
	scheduledAt := time.Now().Add(30 * 24 * time.Hour)

	mockService.On("RequestDeletion", mock.Anything, userID, email).Return(token, scheduledAt, nil)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/user/delete", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	// Set authenticated user in context
	c.Set("user_id", userID)
	c.Set("email", email)

	err := handler.RequestDeletion(c)

	assert.NoError(t, err)
	assert.Equal(t, http.StatusOK, rec.Code)

	// Verify response contains scheduled date and token
	var response map[string]interface{}
	json.Unmarshal(rec.Body.Bytes(), &response)
	assert.Contains(t, response, "message")
	assert.Contains(t, response, "scheduled_at")
	assert.Contains(t, response, "cancellation_token")

	mockService.AssertExpectations(t)
}

// TestRequestDeletion_ServiceError tests handling of service errors
func TestRequestDeletion_ServiceError(t *testing.T) {
	e := echo.New()
	mockService := new(MockDeletionService)

	handler := NewDeletionHandler(mockService)

	userID := "user-123"
	email := "test@example.com"

	mockService.On("RequestDeletion", mock.Anything, userID, email).Return("", time.Time{}, errors.New("service error"))

	req := httptest.NewRequest(http.MethodPost, "/api/v1/user/delete", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	c.Set("user_id", userID)
	c.Set("email", email)

	err := handler.RequestDeletion(c)

	assert.Error(t, err)
	httpError, ok := err.(*echo.HTTPError)
	assert.True(t, ok)
	assert.Equal(t, http.StatusInternalServerError, httpError.Code)

	mockService.AssertExpectations(t)
}

// TestCancelDeletion_ValidToken tests successful cancellation
func TestCancelDeletion_ValidToken(t *testing.T) {
	e := echo.New()
	mockService := new(MockDeletionService)

	handler := NewDeletionHandler(mockService)

	token := "valid-cancellation-token"

	mockService.On("CancelDeletion", mock.Anything, token).Return(nil)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/user/delete/cancel/"+token, nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.SetPath("/api/v1/user/delete/cancel/:token")
	c.SetParamNames("token")
	c.SetParamValues(token)

	err := handler.CancelDeletion(c)

	assert.NoError(t, err)
	assert.Equal(t, http.StatusOK, rec.Code)

	var response map[string]interface{}
	json.Unmarshal(rec.Body.Bytes(), &response)
	assert.Contains(t, response, "message")
	assert.Equal(t, "Account deletion cancelled", response["message"])

	mockService.AssertExpectations(t)
}

// TestCancelDeletion_InvalidToken tests invalid token handling
func TestCancelDeletion_InvalidToken(t *testing.T) {
	e := echo.New()
	mockService := new(MockDeletionService)

	handler := NewDeletionHandler(mockService)

	invalidToken := "invalid-token"

	mockService.On("CancelDeletion", mock.Anything, invalidToken).Return(errors.New("invalid token"))

	req := httptest.NewRequest(http.MethodGet, "/api/v1/user/delete/cancel/"+invalidToken, nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.SetPath("/api/v1/user/delete/cancel/:token")
	c.SetParamNames("token")
	c.SetParamValues(invalidToken)

	err := handler.CancelDeletion(c)

	assert.Error(t, err)
	httpError, ok := err.(*echo.HTTPError)
	assert.True(t, ok)
	assert.Equal(t, http.StatusBadRequest, httpError.Code)

	mockService.AssertExpectations(t)
}
