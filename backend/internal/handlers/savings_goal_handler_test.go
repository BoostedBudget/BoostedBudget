package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/stretchr/testify/assert"
)

// mockSavingsGoalService is a mock implementation of services.SavingsGoalService
type mockSavingsGoalService struct {
	createGoalFunc     func(ctx context.Context, userID string, req *domain.CreateSavingsGoalRequest) (*domain.SavingsGoal, error)
	getGoalFunc        func(ctx context.Context, userID, goalID string) (*domain.SavingsGoal, error)
	getGoalsFunc       func(ctx context.Context, userID string) ([]*domain.SavingsGoal, error)
	getActiveGoalsFunc func(ctx context.Context, userID string) ([]*domain.SavingsGoal, error)
	updateGoalFunc     func(ctx context.Context, userID, goalID string, req *domain.UpdateSavingsGoalRequest) (*domain.SavingsGoal, error)
	deleteGoalFunc     func(ctx context.Context, userID, goalID string) error
}

func (m *mockSavingsGoalService) CreateGoal(ctx context.Context, userID string, req *domain.CreateSavingsGoalRequest) (*domain.SavingsGoal, error) {
	if m.createGoalFunc != nil {
		return m.createGoalFunc(ctx, userID, req)
	}
	return nil, nil
}

func (m *mockSavingsGoalService) GetGoal(ctx context.Context, userID, goalID string) (*domain.SavingsGoal, error) {
	if m.getGoalFunc != nil {
		return m.getGoalFunc(ctx, userID, goalID)
	}
	return nil, nil
}

func (m *mockSavingsGoalService) GetGoals(ctx context.Context, userID string) ([]*domain.SavingsGoal, error) {
	if m.getGoalsFunc != nil {
		return m.getGoalsFunc(ctx, userID)
	}
	return nil, nil
}

func (m *mockSavingsGoalService) GetActiveGoals(ctx context.Context, userID string) ([]*domain.SavingsGoal, error) {
	if m.getActiveGoalsFunc != nil {
		return m.getActiveGoalsFunc(ctx, userID)
	}
	return nil, nil
}

func (m *mockSavingsGoalService) UpdateGoal(ctx context.Context, userID, goalID string, req *domain.UpdateSavingsGoalRequest) (*domain.SavingsGoal, error) {
	if m.updateGoalFunc != nil {
		return m.updateGoalFunc(ctx, userID, goalID, req)
	}
	return nil, nil
}

func (m *mockSavingsGoalService) DeleteGoal(ctx context.Context, userID, goalID string) error {
	if m.deleteGoalFunc != nil {
		return m.deleteGoalFunc(ctx, userID, goalID)
	}
	return nil
}

// Helper to create test savings goal for handler tests
func createTestSavingsGoalForHandler(userID string) *domain.SavingsGoal {
	return &domain.SavingsGoal{
		ID:            "goal-123",
		UserID:        userID,
		Name:          "Vacation Fund",
		TargetAmount:  5000.00,
		CurrentAmount: 1000.00,
		CurrencyCode:  "USD",
		AccountID:     "account-123",
		IsCompleted:   false,
		CreatedAt:     time.Now(),
		UpdatedAt:     time.Now(),
	}
}

// ============================================================================
// Savings Goal Handler Tests
// ============================================================================

func TestSavingsGoalHandler_CreateGoal_Success(t *testing.T) {
	e := echo.New()
	mockService := &mockSavingsGoalService{
		createGoalFunc: func(ctx context.Context, userID string, req *domain.CreateSavingsGoalRequest) (*domain.SavingsGoal, error) {
			return createTestSavingsGoalForHandler(userID), nil
		},
	}
	handler := NewSavingsGoalHandler(mockService)

	reqBody := `{"name": "Vacation Fund", "target_amount": 5000, "account_id": "account-123"}`
	req := httptest.NewRequest(http.MethodPost, "/savings-goals", strings.NewReader(reqBody))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.Set("user_id", "user-123")

	err := handler.CreateGoal(c)

	assert.NoError(t, err)
	assert.Equal(t, http.StatusCreated, rec.Code)

	var response domain.SavingsGoal
	err = json.Unmarshal(rec.Body.Bytes(), &response)
	assert.NoError(t, err)
	assert.Equal(t, "Vacation Fund", response.Name)
}

func TestSavingsGoalHandler_CreateGoal_BadRequest(t *testing.T) {
	e := echo.New()
	mockService := &mockSavingsGoalService{}
	handler := NewSavingsGoalHandler(mockService)

	reqBody := `{"invalid json`
	req := httptest.NewRequest(http.MethodPost, "/savings-goals", strings.NewReader(reqBody))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.Set("user_id", "user-123")

	err := handler.CreateGoal(c)

	httpErr, ok := err.(*echo.HTTPError)
	assert.True(t, ok)
	assert.Equal(t, http.StatusBadRequest, httpErr.Code)
}

func TestSavingsGoalHandler_CreateGoal_Conflict(t *testing.T) {
	e := echo.New()
	mockService := &mockSavingsGoalService{
		createGoalFunc: func(ctx context.Context, userID string, req *domain.CreateSavingsGoalRequest) (*domain.SavingsGoal, error) {
			return nil, domain.ErrSavingsGoalAlreadyExists
		},
	}
	handler := NewSavingsGoalHandler(mockService)

	reqBody := `{"name": "Duplicate", "target_amount": 5000, "account_id": "account-123"}`
	req := httptest.NewRequest(http.MethodPost, "/savings-goals", strings.NewReader(reqBody))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.Set("user_id", "user-123")

	err := handler.CreateGoal(c)

	httpErr, ok := err.(*echo.HTTPError)
	assert.True(t, ok)
	assert.Equal(t, http.StatusConflict, httpErr.Code)
}

func TestSavingsGoalHandler_GetGoals_Success(t *testing.T) {
	e := echo.New()
	mockService := &mockSavingsGoalService{
		getGoalsFunc: func(ctx context.Context, userID string) ([]*domain.SavingsGoal, error) {
			return []*domain.SavingsGoal{createTestSavingsGoalForHandler(userID)}, nil
		},
	}
	handler := NewSavingsGoalHandler(mockService)

	req := httptest.NewRequest(http.MethodGet, "/savings-goals", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.Set("user_id", "user-123")

	err := handler.GetGoals(c)

	assert.NoError(t, err)
	assert.Equal(t, http.StatusOK, rec.Code)

	var response []*domain.SavingsGoal
	err = json.Unmarshal(rec.Body.Bytes(), &response)
	assert.NoError(t, err)
	assert.Len(t, response, 1)
}

func TestSavingsGoalHandler_GetActiveGoals_Success(t *testing.T) {
	e := echo.New()
	mockService := &mockSavingsGoalService{
		getActiveGoalsFunc: func(ctx context.Context, userID string) ([]*domain.SavingsGoal, error) {
			return []*domain.SavingsGoal{createTestSavingsGoalForHandler(userID)}, nil
		},
	}
	handler := NewSavingsGoalHandler(mockService)

	req := httptest.NewRequest(http.MethodGet, "/savings-goals/active", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.Set("user_id", "user-123")

	err := handler.GetActiveGoals(c)

	assert.NoError(t, err)
	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestSavingsGoalHandler_GetGoal_Success(t *testing.T) {
	e := echo.New()
	mockService := &mockSavingsGoalService{
		getGoalFunc: func(ctx context.Context, userID, goalID string) (*domain.SavingsGoal, error) {
			return createTestSavingsGoalForHandler(userID), nil
		},
	}
	handler := NewSavingsGoalHandler(mockService)

	req := httptest.NewRequest(http.MethodGet, "/savings-goals/goal-123", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.Set("user_id", "user-123")
	c.SetParamNames("id")
	c.SetParamValues("goal-123")

	err := handler.GetGoal(c)

	assert.NoError(t, err)
	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestSavingsGoalHandler_GetGoal_NotFound(t *testing.T) {
	e := echo.New()
	mockService := &mockSavingsGoalService{
		getGoalFunc: func(ctx context.Context, userID, goalID string) (*domain.SavingsGoal, error) {
			return nil, domain.ErrSavingsGoalNotFound
		},
	}
	handler := NewSavingsGoalHandler(mockService)

	req := httptest.NewRequest(http.MethodGet, "/savings-goals/nonexistent", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.Set("user_id", "user-123")
	c.SetParamNames("id")
	c.SetParamValues("nonexistent")

	err := handler.GetGoal(c)

	httpErr, ok := err.(*echo.HTTPError)
	assert.True(t, ok)
	assert.Equal(t, http.StatusNotFound, httpErr.Code)
}

func TestSavingsGoalHandler_UpdateGoal_Success(t *testing.T) {
	e := echo.New()
	mockService := &mockSavingsGoalService{
		updateGoalFunc: func(ctx context.Context, userID, goalID string, req *domain.UpdateSavingsGoalRequest) (*domain.SavingsGoal, error) {
			goal := createTestSavingsGoalForHandler(userID)
			if req.Name != nil {
				goal.Name = *req.Name
			}
			return goal, nil
		},
	}
	handler := NewSavingsGoalHandler(mockService)

	reqBody := `{"name": "Updated Vacation Fund"}`
	req := httptest.NewRequest(http.MethodPut, "/savings-goals/goal-123", strings.NewReader(reqBody))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.Set("user_id", "user-123")
	c.SetParamNames("id")
	c.SetParamValues("goal-123")

	err := handler.UpdateGoal(c)

	assert.NoError(t, err)
	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestSavingsGoalHandler_DeleteGoal_Success(t *testing.T) {
	e := echo.New()
	mockService := &mockSavingsGoalService{
		deleteGoalFunc: func(ctx context.Context, userID, goalID string) error {
			return nil
		},
	}
	handler := NewSavingsGoalHandler(mockService)

	req := httptest.NewRequest(http.MethodDelete, "/savings-goals/goal-123", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.Set("user_id", "user-123")
	c.SetParamNames("id")
	c.SetParamValues("goal-123")

	err := handler.DeleteGoal(c)

	assert.NoError(t, err)
	assert.Equal(t, http.StatusNoContent, rec.Code)
}

func TestSavingsGoalHandler_Unauthorized(t *testing.T) {
	e := echo.New()
	mockService := &mockSavingsGoalService{}
	handler := NewSavingsGoalHandler(mockService)

	req := httptest.NewRequest(http.MethodGet, "/savings-goals", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	// Not setting user_id - simulating unauthorized request

	err := handler.GetGoals(c)

	httpErr, ok := err.(*echo.HTTPError)
	assert.True(t, ok)
	assert.Equal(t, http.StatusUnauthorized, httpErr.Code)
}
