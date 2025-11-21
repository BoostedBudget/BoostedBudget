package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/labstack/echo/v4"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/stretchr/testify/assert"
)

// mockCustomCategoryService is a mock implementation of services.CustomCategoryService
type mockCustomCategoryService struct {
	createCategoryFunc      func(ctx context.Context, userID string, req *domain.CreateCustomCategoryRequest) (*domain.CustomCategory, error)
	getUserCategoriesFunc   func(ctx context.Context, userID string) ([]*domain.CustomCategory, error)
	getCategoryByIDFunc     func(ctx context.Context, categoryID string) (*domain.CustomCategory, error)
	updateCategoryFunc      func(ctx context.Context, userID, categoryID string, req *domain.UpdateCustomCategoryRequest) (*domain.CustomCategory, error)
	deleteCategoryFunc      func(ctx context.Context, userID, categoryID string) error
	getOrCreateCategoryFunc func(ctx context.Context, userID, categoryName string) (*domain.CustomCategory, error)
}

func (m *mockCustomCategoryService) CreateCategory(ctx context.Context, userID string, req *domain.CreateCustomCategoryRequest) (*domain.CustomCategory, error) {
	if m.createCategoryFunc != nil {
		return m.createCategoryFunc(ctx, userID, req)
	}
	return nil, nil
}

func (m *mockCustomCategoryService) GetUserCategories(ctx context.Context, userID string) ([]*domain.CustomCategory, error) {
	if m.getUserCategoriesFunc != nil {
		return m.getUserCategoriesFunc(ctx, userID)
	}
	return nil, nil
}

func (m *mockCustomCategoryService) GetCategoryByID(ctx context.Context, categoryID string) (*domain.CustomCategory, error) {
	if m.getCategoryByIDFunc != nil {
		return m.getCategoryByIDFunc(ctx, categoryID)
	}
	return nil, nil
}

func (m *mockCustomCategoryService) UpdateCategory(ctx context.Context, userID, categoryID string, req *domain.UpdateCustomCategoryRequest) (*domain.CustomCategory, error) {
	if m.updateCategoryFunc != nil {
		return m.updateCategoryFunc(ctx, userID, categoryID, req)
	}
	return nil, nil
}

func (m *mockCustomCategoryService) DeleteCategory(ctx context.Context, userID, categoryID string) error {
	if m.deleteCategoryFunc != nil {
		return m.deleteCategoryFunc(ctx, userID, categoryID)
	}
	return nil
}

func (m *mockCustomCategoryService) GetOrCreateCategory(ctx context.Context, userID, categoryName string) (*domain.CustomCategory, error) {
	if m.getOrCreateCategoryFunc != nil {
		return m.getOrCreateCategoryFunc(ctx, userID, categoryName)
	}
	return nil, nil
}

// TestCategoryHandler_CreateCategory tests successful category creation
func TestCategoryHandler_CreateCategory(t *testing.T) {
	e := echo.New()
	mockService := &mockCustomCategoryService{
		createCategoryFunc: func(ctx context.Context, userID string, req *domain.CreateCustomCategoryRequest) (*domain.CustomCategory, error) {
			return &domain.CustomCategory{
				ID:     "cat-123",
				UserID: userID,
				Name:   req.Name,
			}, nil
		},
	}
	handler := NewCategoryHandler(mockService)

	reqBody := `{"name": "Groceries"}`
	req := httptest.NewRequest(http.MethodPost, "/categories", strings.NewReader(reqBody))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.Set("user_id", "user-123")

	err := handler.CreateCategory(c)

	assert.NoError(t, err)
	assert.Equal(t, http.StatusCreated, rec.Code)

	var response domain.CustomCategory
	err = json.Unmarshal(rec.Body.Bytes(), &response)
	assert.NoError(t, err)
	assert.Equal(t, "Groceries", response.Name)
}

// TestCategoryHandler_CreateCategory_LimitExceeded tests category creation when limit is exceeded
func TestCategoryHandler_CreateCategory_LimitExceeded(t *testing.T) {
	e := echo.New()
	mockService := &mockCustomCategoryService{
		createCategoryFunc: func(ctx context.Context, userID string, req *domain.CreateCustomCategoryRequest) (*domain.CustomCategory, error) {
			return nil, domain.ErrCategoryLimitExceeded
		},
	}
	handler := NewCategoryHandler(mockService)

	reqBody := `{"name": "Another Category"}`
	req := httptest.NewRequest(http.MethodPost, "/categories", strings.NewReader(reqBody))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.Set("user_id", "user-123")

	err := handler.CreateCategory(c)

	httpErr, ok := err.(*echo.HTTPError)
	assert.True(t, ok)
	assert.Equal(t, http.StatusForbidden, httpErr.Code)
}

// TestCategoryHandler_CreateCategory_AlreadyExists tests category creation when name already exists
func TestCategoryHandler_CreateCategory_AlreadyExists(t *testing.T) {
	e := echo.New()
	mockService := &mockCustomCategoryService{
		createCategoryFunc: func(ctx context.Context, userID string, req *domain.CreateCustomCategoryRequest) (*domain.CustomCategory, error) {
			return nil, domain.ErrCategoryAlreadyExists
		},
	}
	handler := NewCategoryHandler(mockService)

	reqBody := `{"name": "Duplicate"}`
	req := httptest.NewRequest(http.MethodPost, "/categories", strings.NewReader(reqBody))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.Set("user_id", "user-123")

	err := handler.CreateCategory(c)

	httpErr, ok := err.(*echo.HTTPError)
	assert.True(t, ok)
	assert.Equal(t, http.StatusConflict, httpErr.Code)
}

// TestCategoryHandler_CreateCategory_Unauthorized tests category creation without authentication
func TestCategoryHandler_CreateCategory_Unauthorized(t *testing.T) {
	e := echo.New()
	mockService := &mockCustomCategoryService{}
	handler := NewCategoryHandler(mockService)

	reqBody := `{"name": "Test"}`
	req := httptest.NewRequest(http.MethodPost, "/categories", strings.NewReader(reqBody))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	// No user_id set

	err := handler.CreateCategory(c)

	httpErr, ok := err.(*echo.HTTPError)
	assert.True(t, ok)
	assert.Equal(t, http.StatusUnauthorized, httpErr.Code)
}

// TestCategoryHandler_GetUserCategories tests successful retrieval of user categories
func TestCategoryHandler_GetUserCategories(t *testing.T) {
	e := echo.New()
	mockService := &mockCustomCategoryService{
		getUserCategoriesFunc: func(ctx context.Context, userID string) ([]*domain.CustomCategory, error) {
			return []*domain.CustomCategory{
				{ID: "cat-1", UserID: userID, Name: "Groceries"},
				{ID: "cat-2", UserID: userID, Name: "Entertainment"},
			}, nil
		},
	}
	handler := NewCategoryHandler(mockService)

	req := httptest.NewRequest(http.MethodGet, "/categories", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.Set("user_id", "user-123")

	err := handler.GetUserCategories(c)

	assert.NoError(t, err)
	assert.Equal(t, http.StatusOK, rec.Code)

	var response []*domain.CustomCategory
	err = json.Unmarshal(rec.Body.Bytes(), &response)
	assert.NoError(t, err)
	assert.Len(t, response, 2)
}

// TestCategoryHandler_GetUserCategories_Empty tests retrieval when user has no categories
func TestCategoryHandler_GetUserCategories_Empty(t *testing.T) {
	e := echo.New()
	mockService := &mockCustomCategoryService{
		getUserCategoriesFunc: func(ctx context.Context, userID string) ([]*domain.CustomCategory, error) {
			return nil, nil
		},
	}
	handler := NewCategoryHandler(mockService)

	req := httptest.NewRequest(http.MethodGet, "/categories", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.Set("user_id", "user-123")

	err := handler.GetUserCategories(c)

	assert.NoError(t, err)
	assert.Equal(t, http.StatusOK, rec.Code)

	var response []*domain.CustomCategory
	err = json.Unmarshal(rec.Body.Bytes(), &response)
	assert.NoError(t, err)
	assert.Len(t, response, 0)
}

// TestCategoryHandler_GetCategoryByID tests successful retrieval of a category by ID
func TestCategoryHandler_GetCategoryByID(t *testing.T) {
	e := echo.New()
	mockService := &mockCustomCategoryService{
		getCategoryByIDFunc: func(ctx context.Context, categoryID string) (*domain.CustomCategory, error) {
			return &domain.CustomCategory{
				ID:     categoryID,
				UserID: "user-123",
				Name:   "Groceries",
			}, nil
		},
	}
	handler := NewCategoryHandler(mockService)

	req := httptest.NewRequest(http.MethodGet, "/categories/cat-456", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.SetParamNames("id")
	c.SetParamValues("cat-456")
	c.Set("user_id", "user-123")

	err := handler.GetCategoryByID(c)

	assert.NoError(t, err)
	assert.Equal(t, http.StatusOK, rec.Code)

	var response domain.CustomCategory
	err = json.Unmarshal(rec.Body.Bytes(), &response)
	assert.NoError(t, err)
	assert.Equal(t, "Groceries", response.Name)
}

// TestCategoryHandler_GetCategoryByID_NotFound tests retrieval when category doesn't exist
func TestCategoryHandler_GetCategoryByID_NotFound(t *testing.T) {
	e := echo.New()
	mockService := &mockCustomCategoryService{
		getCategoryByIDFunc: func(ctx context.Context, categoryID string) (*domain.CustomCategory, error) {
			return nil, domain.ErrCategoryNotFound
		},
	}
	handler := NewCategoryHandler(mockService)

	req := httptest.NewRequest(http.MethodGet, "/categories/nonexistent", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.SetParamNames("id")
	c.SetParamValues("nonexistent")
	c.Set("user_id", "user-123")

	err := handler.GetCategoryByID(c)

	httpErr, ok := err.(*echo.HTTPError)
	assert.True(t, ok)
	assert.Equal(t, http.StatusNotFound, httpErr.Code)
}

// TestCategoryHandler_GetCategoryByID_WrongOwner tests retrieval when category belongs to another user
func TestCategoryHandler_GetCategoryByID_WrongOwner(t *testing.T) {
	e := echo.New()
	mockService := &mockCustomCategoryService{
		getCategoryByIDFunc: func(ctx context.Context, categoryID string) (*domain.CustomCategory, error) {
			return &domain.CustomCategory{
				ID:     categoryID,
				UserID: "other-user", // Different from requesting user
				Name:   "Private",
			}, nil
		},
	}
	handler := NewCategoryHandler(mockService)

	req := httptest.NewRequest(http.MethodGet, "/categories/cat-456", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.SetParamNames("id")
	c.SetParamValues("cat-456")
	c.Set("user_id", "user-123")

	err := handler.GetCategoryByID(c)

	httpErr, ok := err.(*echo.HTTPError)
	assert.True(t, ok)
	assert.Equal(t, http.StatusNotFound, httpErr.Code)
}

// TestCategoryHandler_UpdateCategory tests successful category update
func TestCategoryHandler_UpdateCategory(t *testing.T) {
	e := echo.New()
	mockService := &mockCustomCategoryService{
		updateCategoryFunc: func(ctx context.Context, userID, categoryID string, req *domain.UpdateCustomCategoryRequest) (*domain.CustomCategory, error) {
			return &domain.CustomCategory{
				ID:     categoryID,
				UserID: userID,
				Name:   *req.Name,
			}, nil
		},
	}
	handler := NewCategoryHandler(mockService)

	reqBody := `{"name": "Updated Name"}`
	req := httptest.NewRequest(http.MethodPut, "/categories/cat-456", strings.NewReader(reqBody))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.SetParamNames("id")
	c.SetParamValues("cat-456")
	c.Set("user_id", "user-123")

	err := handler.UpdateCategory(c)

	assert.NoError(t, err)
	assert.Equal(t, http.StatusOK, rec.Code)

	var response domain.CustomCategory
	err = json.Unmarshal(rec.Body.Bytes(), &response)
	assert.NoError(t, err)
	assert.Equal(t, "Updated Name", response.Name)
}

// TestCategoryHandler_UpdateCategory_NotFound tests update when category doesn't exist
func TestCategoryHandler_UpdateCategory_NotFound(t *testing.T) {
	e := echo.New()
	mockService := &mockCustomCategoryService{
		updateCategoryFunc: func(ctx context.Context, userID, categoryID string, req *domain.UpdateCustomCategoryRequest) (*domain.CustomCategory, error) {
			return nil, domain.ErrCategoryNotFound
		},
	}
	handler := NewCategoryHandler(mockService)

	reqBody := `{"name": "Updated"}`
	req := httptest.NewRequest(http.MethodPut, "/categories/nonexistent", strings.NewReader(reqBody))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.SetParamNames("id")
	c.SetParamValues("nonexistent")
	c.Set("user_id", "user-123")

	err := handler.UpdateCategory(c)

	httpErr, ok := err.(*echo.HTTPError)
	assert.True(t, ok)
	assert.Equal(t, http.StatusNotFound, httpErr.Code)
}

// TestCategoryHandler_DeleteCategory tests successful category deletion
func TestCategoryHandler_DeleteCategory(t *testing.T) {
	e := echo.New()
	mockService := &mockCustomCategoryService{
		deleteCategoryFunc: func(ctx context.Context, userID, categoryID string) error {
			return nil
		},
	}
	handler := NewCategoryHandler(mockService)

	req := httptest.NewRequest(http.MethodDelete, "/categories/cat-456", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.SetParamNames("id")
	c.SetParamValues("cat-456")
	c.Set("user_id", "user-123")

	err := handler.DeleteCategory(c)

	assert.NoError(t, err)
	assert.Equal(t, http.StatusNoContent, rec.Code)
}

// TestCategoryHandler_DeleteCategory_NotFound tests deletion when category doesn't exist
func TestCategoryHandler_DeleteCategory_NotFound(t *testing.T) {
	e := echo.New()
	mockService := &mockCustomCategoryService{
		deleteCategoryFunc: func(ctx context.Context, userID, categoryID string) error {
			return domain.ErrCategoryNotFound
		},
	}
	handler := NewCategoryHandler(mockService)

	req := httptest.NewRequest(http.MethodDelete, "/categories/nonexistent", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.SetParamNames("id")
	c.SetParamValues("nonexistent")
	c.Set("user_id", "user-123")

	err := handler.DeleteCategory(c)

	httpErr, ok := err.(*echo.HTTPError)
	assert.True(t, ok)
	assert.Equal(t, http.StatusNotFound, httpErr.Code)
}
