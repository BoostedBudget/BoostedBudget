package handlers

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/labstack/echo/v4"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

// =============================================================================
// Issue #497: Category Preview Handler Tests
// =============================================================================

// MockCategoryPreviewService is a mock implementation of CategoryPreviewService
type MockCategoryPreviewService struct {
	mock.Mock
}

func (m *MockCategoryPreviewService) PreviewCategoryMatches(ctx context.Context, userID string, req *domain.CategoryPreviewRequest) (*domain.CategoryPreviewResponse, error) {
	args := m.Called(ctx, userID, req)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*domain.CategoryPreviewResponse), args.Error(1)
}

// TestCategoryPreviewHandler_PreviewCategoryMatches_Success tests successful preview
func TestCategoryPreviewHandler_PreviewCategoryMatches_Success(t *testing.T) {
	e := echo.New()
	mockService := new(MockCategoryPreviewService)
	handler := NewCategoryPreviewHandler(mockService)

	body := `{
		"categories": [["Food and Drink", "Restaurants"]],
		"tags": [],
		"match_logic": "category_only",
		"start_date": "2024-01-01",
		"end_date": "2024-01-31"
	}`

	req := httptest.NewRequest(http.MethodPost, "/budgets/categories/preview", strings.NewReader(body))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	// Set user ID in context
	c.Set("user_id", "user-123")

	mockService.On("PreviewCategoryMatches", mock.Anything, "user-123", mock.AnythingOfType("*domain.CategoryPreviewRequest")).
		Return(&domain.CategoryPreviewResponse{
			TransactionCount: 5,
			TotalAmount:      250.50,
		}, nil)

	err := handler.PreviewCategoryMatches(c)

	assert.NoError(t, err)
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), "transaction_count")
	assert.Contains(t, rec.Body.String(), "250.5")
	mockService.AssertExpectations(t)
}

// TestCategoryPreviewHandler_PreviewCategoryMatches_Unauthenticated tests unauthenticated request
func TestCategoryPreviewHandler_PreviewCategoryMatches_Unauthenticated(t *testing.T) {
	e := echo.New()
	mockService := new(MockCategoryPreviewService)
	handler := NewCategoryPreviewHandler(mockService)

	body := `{
		"categories": [["Food"]],
		"tags": [],
		"match_logic": "category_only",
		"start_date": "2024-01-01",
		"end_date": "2024-01-31"
	}`

	req := httptest.NewRequest(http.MethodPost, "/budgets/categories/preview", strings.NewReader(body))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	// Don't set user_id - should fail auth

	err := handler.PreviewCategoryMatches(c)

	httpErr, ok := err.(*echo.HTTPError)
	assert.True(t, ok)
	assert.Equal(t, http.StatusUnauthorized, httpErr.Code)
}

// TestCategoryPreviewHandler_PreviewCategoryMatches_InvalidBody tests invalid request body
func TestCategoryPreviewHandler_PreviewCategoryMatches_InvalidBody(t *testing.T) {
	e := echo.New()
	mockService := new(MockCategoryPreviewService)
	handler := NewCategoryPreviewHandler(mockService)

	body := `invalid json`

	req := httptest.NewRequest(http.MethodPost, "/budgets/categories/preview", strings.NewReader(body))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.Set("user_id", "user-123")

	err := handler.PreviewCategoryMatches(c)

	httpErr, ok := err.(*echo.HTTPError)
	assert.True(t, ok)
	assert.Equal(t, http.StatusBadRequest, httpErr.Code)
}

// TestCategoryPreviewHandler_PreviewCategoryMatches_ValidationError tests validation errors from service
func TestCategoryPreviewHandler_PreviewCategoryMatches_ValidationError(t *testing.T) {
	e := echo.New()
	mockService := new(MockCategoryPreviewService)
	handler := NewCategoryPreviewHandler(mockService)

	body := `{
		"categories": [],
		"tags": [],
		"match_logic": "category_only",
		"start_date": "2024-01-01",
		"end_date": "2024-01-31"
	}`

	req := httptest.NewRequest(http.MethodPost, "/budgets/categories/preview", strings.NewReader(body))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.Set("user_id", "user-123")

	mockService.On("PreviewCategoryMatches", mock.Anything, "user-123", mock.AnythingOfType("*domain.CategoryPreviewRequest")).
		Return(nil, errors.New("at least one category is required"))

	err := handler.PreviewCategoryMatches(c)

	httpErr, ok := err.(*echo.HTTPError)
	assert.True(t, ok)
	assert.Equal(t, http.StatusBadRequest, httpErr.Code)
	assert.Contains(t, httpErr.Message, "at least one category is required")
	mockService.AssertExpectations(t)
}

// TestCategoryPreviewHandler_PreviewCategoryMatches_ZeroResults tests zero results response
func TestCategoryPreviewHandler_PreviewCategoryMatches_ZeroResults(t *testing.T) {
	e := echo.New()
	mockService := new(MockCategoryPreviewService)
	handler := NewCategoryPreviewHandler(mockService)

	body := `{
		"categories": [["Nonexistent", "Category"]],
		"tags": [],
		"match_logic": "category_only",
		"start_date": "2024-01-01",
		"end_date": "2024-01-31"
	}`

	req := httptest.NewRequest(http.MethodPost, "/budgets/categories/preview", strings.NewReader(body))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.Set("user_id", "user-123")

	mockService.On("PreviewCategoryMatches", mock.Anything, "user-123", mock.AnythingOfType("*domain.CategoryPreviewRequest")).
		Return(&domain.CategoryPreviewResponse{
			TransactionCount: 0,
			TotalAmount:      0,
		}, nil)

	err := handler.PreviewCategoryMatches(c)

	assert.NoError(t, err)
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), `"transaction_count":0`)
	assert.Contains(t, rec.Body.String(), `"total_amount":0`)
	mockService.AssertExpectations(t)
}

// TestCategoryPreviewHandler_PreviewCategoryMatches_TagOnly tests tag_only match logic
func TestCategoryPreviewHandler_PreviewCategoryMatches_TagOnly(t *testing.T) {
	e := echo.New()
	mockService := new(MockCategoryPreviewService)
	handler := NewCategoryPreviewHandler(mockService)

	body := `{
		"categories": [],
		"tags": ["Essential", "groceries"],
		"match_logic": "tag_only",
		"start_date": "2024-01-01",
		"end_date": "2024-01-31"
	}`

	req := httptest.NewRequest(http.MethodPost, "/budgets/categories/preview", strings.NewReader(body))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.Set("user_id", "user-123")

	mockService.On("PreviewCategoryMatches", mock.Anything, "user-123", mock.AnythingOfType("*domain.CategoryPreviewRequest")).
		Return(&domain.CategoryPreviewResponse{
			TransactionCount: 8,
			TotalAmount:      400.00,
		}, nil)

	err := handler.PreviewCategoryMatches(c)

	assert.NoError(t, err)
	assert.Equal(t, http.StatusOK, rec.Code)
	mockService.AssertExpectations(t)
}

// TestCategoryPreviewHandler_PreviewCategoryMatches_MultipleCategories tests multiple categories
func TestCategoryPreviewHandler_PreviewCategoryMatches_MultipleCategories(t *testing.T) {
	e := echo.New()
	mockService := new(MockCategoryPreviewService)
	handler := NewCategoryPreviewHandler(mockService)

	body := `{
		"categories": [
			["Food and Drink", "Restaurants"],
			["Food and Drink", "Groceries"]
		],
		"tags": [],
		"match_logic": "category_only",
		"start_date": "2024-01-01",
		"end_date": "2024-01-31"
	}`

	req := httptest.NewRequest(http.MethodPost, "/budgets/categories/preview", strings.NewReader(body))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.Set("user_id", "user-123")

	mockService.On("PreviewCategoryMatches", mock.Anything, "user-123", mock.AnythingOfType("*domain.CategoryPreviewRequest")).
		Return(&domain.CategoryPreviewResponse{
			TransactionCount: 12,
			TotalAmount:      650.00,
		}, nil)

	err := handler.PreviewCategoryMatches(c)

	assert.NoError(t, err)
	assert.Equal(t, http.StatusOK, rec.Code)
	mockService.AssertExpectations(t)
}
