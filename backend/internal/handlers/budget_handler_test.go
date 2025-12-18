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

// mockBudgetService is a mock implementation of services.BudgetService
type mockBudgetService struct {
	createBudgetFunc           func(ctx context.Context, userID string, req *domain.CreateBudgetRequest) (*domain.Budget, error)
	getBudgetFunc              func(ctx context.Context, userID, budgetID string) (*domain.Budget, error)
	getBudgetsFunc             func(ctx context.Context, userID string) ([]*domain.Budget, error)
	getActiveBudgetsFunc       func(ctx context.Context, userID string) ([]*domain.Budget, error)
	getCurrentBudgetFunc       func(ctx context.Context, userID string) (*domain.Budget, error)
	updateBudgetFunc           func(ctx context.Context, userID, budgetID string, req *domain.UpdateBudgetRequest) (*domain.Budget, error)
	deleteBudgetFunc           func(ctx context.Context, userID, budgetID string) error
	addCategoryFunc            func(ctx context.Context, userID, budgetID string, req *domain.CreateBudgetCategoryRequest) (*domain.BudgetCategory, error)
	updateCategoryFunc         func(ctx context.Context, userID, budgetID, categoryID string, req *domain.UpdateBudgetCategoryRequest) (*domain.BudgetCategory, error)
	deleteCategoryFunc         func(ctx context.Context, userID, budgetID, categoryID string) error
	getAvailableCategoriesFunc func(ctx context.Context, userID string) ([]string, error)
}

func (m *mockBudgetService) CreateBudget(ctx context.Context, userID string, req *domain.CreateBudgetRequest) (*domain.Budget, error) {
	if m.createBudgetFunc != nil {
		return m.createBudgetFunc(ctx, userID, req)
	}
	return nil, nil
}

func (m *mockBudgetService) GetBudget(ctx context.Context, userID, budgetID string) (*domain.Budget, error) {
	if m.getBudgetFunc != nil {
		return m.getBudgetFunc(ctx, userID, budgetID)
	}
	return nil, nil
}

func (m *mockBudgetService) GetBudgets(ctx context.Context, userID string) ([]*domain.Budget, error) {
	if m.getBudgetsFunc != nil {
		return m.getBudgetsFunc(ctx, userID)
	}
	return nil, nil
}

func (m *mockBudgetService) GetActiveBudgets(ctx context.Context, userID string) ([]*domain.Budget, error) {
	if m.getActiveBudgetsFunc != nil {
		return m.getActiveBudgetsFunc(ctx, userID)
	}
	return nil, nil
}

func (m *mockBudgetService) GetCurrentBudget(ctx context.Context, userID string) (*domain.Budget, error) {
	if m.getCurrentBudgetFunc != nil {
		return m.getCurrentBudgetFunc(ctx, userID)
	}
	return nil, nil
}

func (m *mockBudgetService) UpdateBudget(ctx context.Context, userID, budgetID string, req *domain.UpdateBudgetRequest) (*domain.Budget, error) {
	if m.updateBudgetFunc != nil {
		return m.updateBudgetFunc(ctx, userID, budgetID, req)
	}
	return nil, nil
}

func (m *mockBudgetService) DeleteBudget(ctx context.Context, userID, budgetID string) error {
	if m.deleteBudgetFunc != nil {
		return m.deleteBudgetFunc(ctx, userID, budgetID)
	}
	return nil
}

func (m *mockBudgetService) AddCategory(ctx context.Context, userID, budgetID string, req *domain.CreateBudgetCategoryRequest) (*domain.BudgetCategory, error) {
	if m.addCategoryFunc != nil {
		return m.addCategoryFunc(ctx, userID, budgetID, req)
	}
	return nil, nil
}

func (m *mockBudgetService) UpdateCategory(ctx context.Context, userID, budgetID, categoryID string, req *domain.UpdateBudgetCategoryRequest) (*domain.BudgetCategory, error) {
	if m.updateCategoryFunc != nil {
		return m.updateCategoryFunc(ctx, userID, budgetID, categoryID, req)
	}
	return nil, nil
}

func (m *mockBudgetService) DeleteCategory(ctx context.Context, userID, budgetID, categoryID string) error {
	if m.deleteCategoryFunc != nil {
		return m.deleteCategoryFunc(ctx, userID, budgetID, categoryID)
	}
	return nil
}

func (m *mockBudgetService) GetAvailableCategories(ctx context.Context, userID string) ([]string, error) {
	if m.getAvailableCategoriesFunc != nil {
		return m.getAvailableCategoriesFunc(ctx, userID)
	}
	return nil, nil
}

// Helper to create test budget for handler tests
func createTestBudgetForHandler(userID string) *domain.Budget {
	return &domain.Budget{
		ID:           "budget-123",
		UserID:       userID,
		Name:         "Monthly Expenses",
		Period:       domain.BudgetPeriodMonthly,
		StartDate:    time.Now(),
		Amount:       1000.00,
		CurrencyCode: "USD",
		IsActive:     true,
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
	}
}

// ============================================================================
// Budget CRUD Handler Tests
// ============================================================================

func TestBudgetHandler_CreateBudget_Success(t *testing.T) {
	e := echo.New()
	mockService := &mockBudgetService{
		createBudgetFunc: func(ctx context.Context, userID string, req *domain.CreateBudgetRequest) (*domain.Budget, error) {
			return createTestBudgetForHandler(userID), nil
		},
	}
	handler := NewBudgetHandler(mockService, nil, nil)

	reqBody := `{"name": "Monthly Expenses", "period": "monthly", "start_date": "2024-01-01T00:00:00Z", "amount": 1000}`
	req := httptest.NewRequest(http.MethodPost, "/budgets", strings.NewReader(reqBody))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.Set("user_id", "user-123")

	err := handler.CreateBudget(c)

	assert.NoError(t, err)
	assert.Equal(t, http.StatusCreated, rec.Code)

	var response domain.Budget
	err = json.Unmarshal(rec.Body.Bytes(), &response)
	assert.NoError(t, err)
	assert.Equal(t, "Monthly Expenses", response.Name)
}

func TestBudgetHandler_CreateBudget_BadRequest(t *testing.T) {
	e := echo.New()
	mockService := &mockBudgetService{}
	handler := NewBudgetHandler(mockService, nil, nil)

	reqBody := `{"invalid json`
	req := httptest.NewRequest(http.MethodPost, "/budgets", strings.NewReader(reqBody))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.Set("user_id", "user-123")

	err := handler.CreateBudget(c)

	httpErr, ok := err.(*echo.HTTPError)
	assert.True(t, ok)
	assert.Equal(t, http.StatusBadRequest, httpErr.Code)
}

func TestBudgetHandler_CreateBudget_Conflict(t *testing.T) {
	e := echo.New()
	mockService := &mockBudgetService{
		createBudgetFunc: func(ctx context.Context, userID string, req *domain.CreateBudgetRequest) (*domain.Budget, error) {
			return nil, domain.ErrBudgetAlreadyExists
		},
	}
	handler := NewBudgetHandler(mockService, nil, nil)

	reqBody := `{"name": "Duplicate", "period": "monthly", "start_date": "2024-01-01T00:00:00Z", "amount": 1000}`
	req := httptest.NewRequest(http.MethodPost, "/budgets", strings.NewReader(reqBody))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.Set("user_id", "user-123")

	err := handler.CreateBudget(c)

	httpErr, ok := err.(*echo.HTTPError)
	assert.True(t, ok)
	assert.Equal(t, http.StatusConflict, httpErr.Code)
}

func TestBudgetHandler_GetBudgets_Success(t *testing.T) {
	e := echo.New()
	mockService := &mockBudgetService{
		getBudgetsFunc: func(ctx context.Context, userID string) ([]*domain.Budget, error) {
			return []*domain.Budget{createTestBudgetForHandler(userID)}, nil
		},
	}
	handler := NewBudgetHandler(mockService, nil, nil)

	req := httptest.NewRequest(http.MethodGet, "/budgets", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.Set("user_id", "user-123")

	err := handler.GetBudgets(c)

	assert.NoError(t, err)
	assert.Equal(t, http.StatusOK, rec.Code)

	var response []*domain.Budget
	err = json.Unmarshal(rec.Body.Bytes(), &response)
	assert.NoError(t, err)
	assert.Len(t, response, 1)
}

func TestBudgetHandler_GetBudget_Success(t *testing.T) {
	e := echo.New()
	mockService := &mockBudgetService{
		getBudgetFunc: func(ctx context.Context, userID, budgetID string) (*domain.Budget, error) {
			return createTestBudgetForHandler(userID), nil
		},
	}
	handler := NewBudgetHandler(mockService, nil, nil)

	req := httptest.NewRequest(http.MethodGet, "/budgets/budget-123", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.Set("user_id", "user-123")
	c.SetParamNames("id")
	c.SetParamValues("budget-123")

	err := handler.GetBudget(c)

	assert.NoError(t, err)
	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestBudgetHandler_GetBudget_NotFound(t *testing.T) {
	e := echo.New()
	mockService := &mockBudgetService{
		getBudgetFunc: func(ctx context.Context, userID, budgetID string) (*domain.Budget, error) {
			return nil, domain.ErrBudgetNotFound
		},
	}
	handler := NewBudgetHandler(mockService, nil, nil)

	req := httptest.NewRequest(http.MethodGet, "/budgets/nonexistent", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.Set("user_id", "user-123")
	c.SetParamNames("id")
	c.SetParamValues("nonexistent")

	err := handler.GetBudget(c)

	httpErr, ok := err.(*echo.HTTPError)
	assert.True(t, ok)
	assert.Equal(t, http.StatusNotFound, httpErr.Code)
}

func TestBudgetHandler_GetCurrentBudget_Success(t *testing.T) {
	e := echo.New()
	mockService := &mockBudgetService{
		getCurrentBudgetFunc: func(ctx context.Context, userID string) (*domain.Budget, error) {
			return createTestBudgetForHandler(userID), nil
		},
	}
	handler := NewBudgetHandler(mockService, nil, nil)

	req := httptest.NewRequest(http.MethodGet, "/budgets/current", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.Set("user_id", "user-123")

	err := handler.GetCurrentBudget(c)

	assert.NoError(t, err)
	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestBudgetHandler_UpdateBudget_Success(t *testing.T) {
	e := echo.New()
	mockService := &mockBudgetService{
		updateBudgetFunc: func(ctx context.Context, userID, budgetID string, req *domain.UpdateBudgetRequest) (*domain.Budget, error) {
			budget := createTestBudgetForHandler(userID)
			if req.Name != nil {
				budget.Name = *req.Name
			}
			return budget, nil
		},
	}
	handler := NewBudgetHandler(mockService, nil, nil)

	reqBody := `{"name": "Updated Budget"}`
	req := httptest.NewRequest(http.MethodPut, "/budgets/budget-123", strings.NewReader(reqBody))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.Set("user_id", "user-123")
	c.SetParamNames("id")
	c.SetParamValues("budget-123")

	err := handler.UpdateBudget(c)

	assert.NoError(t, err)
	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestBudgetHandler_DeleteBudget_Success(t *testing.T) {
	e := echo.New()
	mockService := &mockBudgetService{
		deleteBudgetFunc: func(ctx context.Context, userID, budgetID string) error {
			return nil
		},
	}
	handler := NewBudgetHandler(mockService, nil, nil)

	req := httptest.NewRequest(http.MethodDelete, "/budgets/budget-123", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.Set("user_id", "user-123")
	c.SetParamNames("id")
	c.SetParamValues("budget-123")

	err := handler.DeleteBudget(c)

	assert.NoError(t, err)
	assert.Equal(t, http.StatusNoContent, rec.Code)
}

// ============================================================================
// Category Handler Tests
// ============================================================================

func TestBudgetHandler_AddCategory_Success(t *testing.T) {
	e := echo.New()
	mockService := &mockBudgetService{
		addCategoryFunc: func(ctx context.Context, userID, budgetID string, req *domain.CreateBudgetCategoryRequest) (*domain.BudgetCategory, error) {
			return &domain.BudgetCategory{
				ID:              "cat-123",
				BudgetID:        budgetID,
				Category:        req.Category,
				AllocatedAmount: req.AllocatedAmount,
			}, nil
		},
	}
	handler := NewBudgetHandler(mockService, nil, nil)

	reqBody := `{"category": ["Food", "Dining"], "allocated_amount": 300}`
	req := httptest.NewRequest(http.MethodPost, "/budgets/budget-123/categories", strings.NewReader(reqBody))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.Set("user_id", "user-123")
	c.SetParamNames("id")
	c.SetParamValues("budget-123")

	err := handler.AddCategory(c)

	assert.NoError(t, err)
	assert.Equal(t, http.StatusCreated, rec.Code)
}

func TestBudgetHandler_UpdateCategory_Success(t *testing.T) {
	e := echo.New()
	mockService := &mockBudgetService{
		updateCategoryFunc: func(ctx context.Context, userID, budgetID, categoryID string, req *domain.UpdateBudgetCategoryRequest) (*domain.BudgetCategory, error) {
			return &domain.BudgetCategory{
				ID:              categoryID,
				BudgetID:        budgetID,
				AllocatedAmount: *req.AllocatedAmount,
			}, nil
		},
	}
	handler := NewBudgetHandler(mockService, nil, nil)

	reqBody := `{"allocated_amount": 500}`
	req := httptest.NewRequest(http.MethodPut, "/budgets/budget-123/categories/cat-123", strings.NewReader(reqBody))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.Set("user_id", "user-123")
	c.SetParamNames("id", "categoryId")
	c.SetParamValues("budget-123", "cat-123")

	err := handler.UpdateCategory(c)

	assert.NoError(t, err)
	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestBudgetHandler_DeleteCategory_Success(t *testing.T) {
	e := echo.New()
	mockService := &mockBudgetService{
		deleteCategoryFunc: func(ctx context.Context, userID, budgetID, categoryID string) error {
			return nil
		},
	}
	handler := NewBudgetHandler(mockService, nil, nil)

	req := httptest.NewRequest(http.MethodDelete, "/budgets/budget-123/categories/cat-123", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.Set("user_id", "user-123")
	c.SetParamNames("id", "categoryId")
	c.SetParamValues("budget-123", "cat-123")

	err := handler.DeleteCategory(c)

	assert.NoError(t, err)
	assert.Equal(t, http.StatusNoContent, rec.Code)
}

func TestBudgetHandler_GetAvailableCategories_Success(t *testing.T) {
	e := echo.New()
	mockService := &mockBudgetService{
		getAvailableCategoriesFunc: func(ctx context.Context, userID string) ([]string, error) {
			return []string{"Food And Drink", "Transportation", "Groceries"}, nil
		},
	}
	handler := NewBudgetHandler(mockService, nil, nil)

	req := httptest.NewRequest(http.MethodGet, "/budgets/categories/available", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.Set("user_id", "user-123")

	err := handler.GetAvailableCategories(c)

	assert.NoError(t, err)
	assert.Equal(t, http.StatusOK, rec.Code)

	var response []string
	err = json.Unmarshal(rec.Body.Bytes(), &response)
	assert.NoError(t, err)
	assert.Len(t, response, 3)
}

func TestBudgetHandler_Unauthorized(t *testing.T) {
	e := echo.New()
	mockService := &mockBudgetService{}
	handler := NewBudgetHandler(mockService, nil, nil)

	req := httptest.NewRequest(http.MethodGet, "/budgets", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	// Not setting user_id - simulating unauthorized request

	err := handler.GetBudgets(c)

	httpErr, ok := err.(*echo.HTTPError)
	assert.True(t, ok)
	assert.Equal(t, http.StatusUnauthorized, httpErr.Code)
}
