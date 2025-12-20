package handlers

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/services"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockTransactionService is a mock implementation of TransactionService for testing
type mockTransactionService struct {
	listTransactionsFunc             func(ctx context.Context, userID string, filters domain.TransactionFilters, limit, offset int) (*services.TransactionListResponse, error)
	getTransactionByIDFunc           func(ctx context.Context, userID, transactionID string) (*domain.Transaction, error)
	updateTransactionTagsFunc        func(ctx context.Context, userID, transactionID string, tags []string) (*domain.Transaction, error)
	updateTransactionCategoryFunc    func(ctx context.Context, userID, transactionID string, category []string) (*domain.Transaction, error)
	getDistinctPlaidCategoriesFunc   func(ctx context.Context, userID string) ([]string, error)
}

func (m *mockTransactionService) ListTransactions(ctx context.Context, userID string, filters domain.TransactionFilters, limit, offset int) (*services.TransactionListResponse, error) {
	if m.listTransactionsFunc != nil {
		return m.listTransactionsFunc(ctx, userID, filters, limit, offset)
	}
	return nil, errors.New("not implemented")
}

func (m *mockTransactionService) GetTransactionByID(ctx context.Context, userID, transactionID string) (*domain.Transaction, error) {
	if m.getTransactionByIDFunc != nil {
		return m.getTransactionByIDFunc(ctx, userID, transactionID)
	}
	return nil, errors.New("not implemented")
}

func (m *mockTransactionService) UpdateTransactionTags(ctx context.Context, userID, transactionID string, tags []string) (*domain.Transaction, error) {
	if m.updateTransactionTagsFunc != nil {
		return m.updateTransactionTagsFunc(ctx, userID, transactionID, tags)
	}
	return nil, errors.New("not implemented")
}

func (m *mockTransactionService) UpdateTransactionCategory(ctx context.Context, userID, transactionID string, category []string) (*domain.Transaction, error) {
	if m.updateTransactionCategoryFunc != nil {
		return m.updateTransactionCategoryFunc(ctx, userID, transactionID, category)
	}
	return nil, errors.New("not implemented")
}

func (m *mockTransactionService) GetDistinctPlaidCategories(ctx context.Context, userID string) ([]string, error) {
	if m.getDistinctPlaidCategoriesFunc != nil {
		return m.getDistinctPlaidCategoriesFunc(ctx, userID)
	}
	return nil, errors.New("not implemented")
}

// TestGetTransactions_Unauthorized tests that requests without authentication are rejected
func TestGetTransactions_Unauthorized(t *testing.T) {
	// Given: Handler with mock service
	mockService := &mockTransactionService{}
	handler := NewTransactionHandler(mockService)

	// When: Making GET request WITHOUT user_id in context (not authenticated)
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/transactions", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	// Note: NOT setting user_id in context

	err := handler.GetTransactions(c)

	// Then: Returns 401 Unauthorized error
	require.Error(t, err, "Expected error when user not authenticated")
	httpErr, ok := err.(*echo.HTTPError)
	require.True(t, ok, "Expected echo.HTTPError")
	assert.Equal(t, http.StatusUnauthorized, httpErr.Code, "Expected 401 Unauthorized")
}

// TestGetTransactions_Success tests successful transaction retrieval with default parameters
func TestGetTransactions_Success(t *testing.T) {
	// Given: Mock service that returns transactions
	externalID := "txn-1"
	expectedResponse := &services.TransactionListResponse{
		Transactions: []domain.Transaction{
			{
				ID:                    "id-1",
				UserID:                "user-123",
				AccountID:             "account-1",
				LinkedItemID:          "item-1",
				Source:                domain.SourcePlaid,
				ExternalTransactionID: &externalID,
				Amount:                -25.50,
				Date:                  time.Now(),
				Name:                  "Grocery Store",
			},
		},
		TotalCount: 1,
		Limit:      50,
		Offset:     0,
		HasMore:    false,
	}

	mockService := &mockTransactionService{
		listTransactionsFunc: func(ctx context.Context, userID string, filters domain.TransactionFilters, limit, offset int) (*services.TransactionListResponse, error) {
			assert.Equal(t, "user-123", userID)
			assert.Equal(t, 50, limit) // Default limit
			assert.Equal(t, 0, offset)  // Default offset
			return expectedResponse, nil
		},
	}

	handler := NewTransactionHandler(mockService)

	// When: Making GET request with authenticated user (no query params)
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/transactions", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.Set("user_id", "user-123")

	err := handler.GetTransactions(c)

	// Then: Returns 200 with transactions
	require.NoError(t, err, "Expected no error from handler")
	assert.Equal(t, http.StatusOK, rec.Code, "Expected 200 OK")
	assert.Contains(t, rec.Body.String(), "Grocery Store", "Response should contain transaction name")
	assert.Contains(t, rec.Body.String(), "\"total_count\":1", "Response should contain total count")
	assert.Contains(t, rec.Body.String(), "\"has_more\":false", "Response should contain has_more flag")
}

// TestGetTransactions_ParsesLimitQueryParam tests that limit parameter is parsed correctly
func TestGetTransactions_ParsesLimitQueryParam(t *testing.T) {
	// Given: Mock service that captures limit parameter
	mockService := &mockTransactionService{
		listTransactionsFunc: func(ctx context.Context, userID string, filters domain.TransactionFilters, limit, offset int) (*services.TransactionListResponse, error) {
			assert.Equal(t, 25, limit, "Should pass custom limit")
			return &services.TransactionListResponse{
				Transactions: []domain.Transaction{},
				TotalCount:   0,
				Limit:        25,
				Offset:       0,
				HasMore:      false,
			}, nil
		},
	}

	handler := NewTransactionHandler(mockService)

	// When: Making GET request with limit query parameter
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/transactions?limit=25", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.Set("user_id", "user-123")

	err := handler.GetTransactions(c)

	// Then: Returns 200 with custom limit
	require.NoError(t, err, "Expected no error from handler")
	assert.Equal(t, http.StatusOK, rec.Code, "Expected 200 OK")
}

// TestGetTransactions_ParsesOffsetQueryParam tests that offset parameter is parsed correctly
func TestGetTransactions_ParsesOffsetQueryParam(t *testing.T) {
	// Given: Mock service that captures offset parameter
	mockService := &mockTransactionService{
		listTransactionsFunc: func(ctx context.Context, userID string, filters domain.TransactionFilters, limit, offset int) (*services.TransactionListResponse, error) {
			assert.Equal(t, 100, offset, "Should pass custom offset")
			return &services.TransactionListResponse{
				Transactions: []domain.Transaction{},
				TotalCount:   0,
				Limit:        50,
				Offset:       100,
				HasMore:      false,
			}, nil
		},
	}

	handler := NewTransactionHandler(mockService)

	// When: Making GET request with offset query parameter
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/transactions?offset=100", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.Set("user_id", "user-123")

	err := handler.GetTransactions(c)

	// Then: Returns 200 with custom offset
	require.NoError(t, err, "Expected no error from handler")
	assert.Equal(t, http.StatusOK, rec.Code, "Expected 200 OK")
}

// TestGetTransactions_ParsesDateQueryParams tests that date parameters are parsed correctly
func TestGetTransactions_ParsesDateQueryParams(t *testing.T) {
	// Given: Mock service that captures date filter
	mockService := &mockTransactionService{
		listTransactionsFunc: func(ctx context.Context, userID string, filters domain.TransactionFilters, limit, offset int) (*services.TransactionListResponse, error) {
			require.NotNil(t, filters.StartDate, "Should have start date filter")
			require.NotNil(t, filters.EndDate, "Should have end date filter")
			assert.Equal(t, 2025, filters.StartDate.Year())
			assert.Equal(t, time.January, filters.StartDate.Month())
			assert.Equal(t, 1, filters.StartDate.Day())
			assert.Equal(t, 2025, filters.EndDate.Year())
			assert.Equal(t, time.December, filters.EndDate.Month())
			assert.Equal(t, 31, filters.EndDate.Day())
			return &services.TransactionListResponse{
				Transactions: []domain.Transaction{},
				TotalCount:   0,
				Limit:        50,
				Offset:       0,
				HasMore:      false,
			}, nil
		},
	}

	handler := NewTransactionHandler(mockService)

	// When: Making GET request with date query parameters
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/transactions?start_date=2025-01-01&end_date=2025-12-31", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.Set("user_id", "user-123")

	err := handler.GetTransactions(c)

	// Then: Returns 200 with date filters applied
	require.NoError(t, err, "Expected no error from handler")
	assert.Equal(t, http.StatusOK, rec.Code, "Expected 200 OK")
}

// TestGetTransactions_InvalidDateFormat tests that invalid date formats return 400
func TestGetTransactions_InvalidDateFormat(t *testing.T) {
	// Given: Handler with mock service
	mockService := &mockTransactionService{}
	handler := NewTransactionHandler(mockService)

	// When: Making GET request with invalid date format
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/transactions?start_date=not-a-date", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.Set("user_id", "user-123")

	err := handler.GetTransactions(c)

	// Then: Returns 400 Bad Request
	require.Error(t, err, "Expected error for invalid date format")
	httpErr, ok := err.(*echo.HTTPError)
	require.True(t, ok, "Expected echo.HTTPError")
	assert.Equal(t, http.StatusBadRequest, httpErr.Code, "Expected 400 Bad Request")
}

// TestGetTransactions_InvalidLimit tests that limit > 100 returns 400
func TestGetTransactions_InvalidLimit(t *testing.T) {
	// Given: Mock service that returns error for invalid limit
	mockService := &mockTransactionService{
		listTransactionsFunc: func(ctx context.Context, userID string, filters domain.TransactionFilters, limit, offset int) (*services.TransactionListResponse, error) {
			return nil, services.ErrInvalidLimit
		},
	}

	handler := NewTransactionHandler(mockService)

	// When: Making GET request with limit > 100
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/transactions?limit=150", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.Set("user_id", "user-123")

	err := handler.GetTransactions(c)

	// Then: Returns 400 Bad Request
	require.Error(t, err, "Expected error for invalid limit")
	httpErr, ok := err.(*echo.HTTPError)
	require.True(t, ok, "Expected echo.HTTPError")
	assert.Equal(t, http.StatusBadRequest, httpErr.Code, "Expected 400 Bad Request")
}

// TestGetTransactions_InvalidOffset tests that negative offset returns 400
func TestGetTransactions_InvalidOffset(t *testing.T) {
	// Given: Mock service that returns error for invalid offset
	mockService := &mockTransactionService{
		listTransactionsFunc: func(ctx context.Context, userID string, filters domain.TransactionFilters, limit, offset int) (*services.TransactionListResponse, error) {
			return nil, services.ErrInvalidOffset
		},
	}

	handler := NewTransactionHandler(mockService)

	// When: Making GET request with negative offset
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/transactions?offset=-10", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.Set("user_id", "user-123")

	err := handler.GetTransactions(c)

	// Then: Returns 400 Bad Request
	require.Error(t, err, "Expected error for invalid offset")
	httpErr, ok := err.(*echo.HTTPError)
	require.True(t, ok, "Expected echo.HTTPError")
	assert.Equal(t, http.StatusBadRequest, httpErr.Code, "Expected 400 Bad Request")
}

// TestGetTransactions_ParsesAllFilters tests that all filter parameters are parsed correctly
func TestGetTransactions_ParsesAllFilters(t *testing.T) {
	// Given: Mock service that captures all filters
	mockService := &mockTransactionService{
		listTransactionsFunc: func(ctx context.Context, userID string, filters domain.TransactionFilters, limit, offset int) (*services.TransactionListResponse, error) {
			require.NotNil(t, filters.AccountID, "Should have account_id filter")
			assert.Equal(t, "account-123", *filters.AccountID)

			require.NotNil(t, filters.Category, "Should have category filter")
			assert.Equal(t, "food", *filters.Category)

			require.NotNil(t, filters.Search, "Should have search filter")
			assert.Equal(t, "starbucks", *filters.Search)

			require.NotNil(t, filters.Pending, "Should have pending filter")
			assert.True(t, *filters.Pending)

			return &services.TransactionListResponse{
				Transactions: []domain.Transaction{},
				TotalCount:   0,
				Limit:        50,
				Offset:       0,
				HasMore:      false,
			}, nil
		},
	}

	handler := NewTransactionHandler(mockService)

	// When: Making GET request with all filter parameters
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/transactions?account_id=account-123&category=food&search=starbucks&pending=true", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.Set("user_id", "user-123")

	err := handler.GetTransactions(c)

	// Then: Returns 200 with all filters applied
	require.NoError(t, err, "Expected no error from handler")
	assert.Equal(t, http.StatusOK, rec.Code, "Expected 200 OK")
}

// TestGetTransactions_ServiceError tests handling of service-layer failures
func TestGetTransactions_ServiceError(t *testing.T) {
	// Given: Mock service that returns an error
	mockService := &mockTransactionService{
		listTransactionsFunc: func(ctx context.Context, userID string, filters domain.TransactionFilters, limit, offset int) (*services.TransactionListResponse, error) {
			return nil, errors.New("database connection error")
		},
	}

	handler := NewTransactionHandler(mockService)

	// When: Making GET request with authenticated user
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/transactions", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.Set("user_id", "user-123")

	err := handler.GetTransactions(c)

	// Then: Returns 500 Internal Server Error
	require.Error(t, err, "Expected error when service fails")
	httpErr, ok := err.(*echo.HTTPError)
	require.True(t, ok, "Expected echo.HTTPError")
	assert.Equal(t, http.StatusInternalServerError, httpErr.Code, "Expected 500 Internal Server Error")
}

// TestGetTransactions_PaginationMetadata tests that pagination metadata is included in response
func TestGetTransactions_PaginationMetadata(t *testing.T) {
	// Given: Mock service that returns paginated results
	mockService := &mockTransactionService{
		listTransactionsFunc: func(ctx context.Context, userID string, filters domain.TransactionFilters, limit, offset int) (*services.TransactionListResponse, error) {
			return &services.TransactionListResponse{
				Transactions: []domain.Transaction{},
				TotalCount:   150,
				Limit:        50,
				Offset:       0,
				HasMore:      true,
			}, nil
		},
	}

	handler := NewTransactionHandler(mockService)

	// When: Making GET request
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/transactions", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.Set("user_id", "user-123")

	err := handler.GetTransactions(c)

	// Then: Returns 200 with pagination metadata
	require.NoError(t, err, "Expected no error from handler")
	assert.Equal(t, http.StatusOK, rec.Code, "Expected 200 OK")
	assert.Contains(t, rec.Body.String(), "\"total_count\":150", "Response should contain total count")
	assert.Contains(t, rec.Body.String(), "\"limit\":50", "Response should contain limit")
	assert.Contains(t, rec.Body.String(), "\"offset\":0", "Response should contain offset")
	assert.Contains(t, rec.Body.String(), "\"has_more\":true", "Response should contain has_more flag")
}

// =============================================================================
// Issue #529: Extended Filter Parameters Tests
// =============================================================================

// TestGetTransactions_ParsesAccountIdsQueryParam tests that comma-separated account IDs are parsed correctly
func TestGetTransactions_ParsesAccountIdsQueryParam(t *testing.T) {
	// Given: Mock service that captures account_ids filter
	mockService := &mockTransactionService{
		listTransactionsFunc: func(ctx context.Context, userID string, filters domain.TransactionFilters, limit, offset int) (*services.TransactionListResponse, error) {
			require.NotNil(t, filters.AccountIDs, "Should have account_ids filter")
			assert.Len(t, filters.AccountIDs, 3, "Should have 3 account IDs")
			assert.Contains(t, filters.AccountIDs, "acc-1")
			assert.Contains(t, filters.AccountIDs, "acc-2")
			assert.Contains(t, filters.AccountIDs, "acc-3")
			return &services.TransactionListResponse{
				Transactions: []domain.Transaction{},
				TotalCount:   0,
				Limit:        50,
				Offset:       0,
				HasMore:      false,
			}, nil
		},
	}

	handler := NewTransactionHandler(mockService)

	// When: Making GET request with comma-separated account_ids
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/transactions?account_ids=acc-1,acc-2,acc-3", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.Set("user_id", "user-123")

	err := handler.GetTransactions(c)

	// Then: Returns 200 with account_ids filter applied
	require.NoError(t, err, "Expected no error from handler")
	assert.Equal(t, http.StatusOK, rec.Code, "Expected 200 OK")
}

// TestGetTransactions_ParsesCategoriesQueryParam tests that comma-separated categories are parsed correctly
func TestGetTransactions_ParsesCategoriesQueryParam(t *testing.T) {
	// Given: Mock service that captures categories filter
	mockService := &mockTransactionService{
		listTransactionsFunc: func(ctx context.Context, userID string, filters domain.TransactionFilters, limit, offset int) (*services.TransactionListResponse, error) {
			require.NotNil(t, filters.Categories, "Should have categories filter")
			assert.Len(t, filters.Categories, 2, "Should have 2 categories")
			assert.Contains(t, filters.Categories, "Food and Drink")
			assert.Contains(t, filters.Categories, "Transportation")
			return &services.TransactionListResponse{
				Transactions: []domain.Transaction{},
				TotalCount:   0,
				Limit:        50,
				Offset:       0,
				HasMore:      false,
			}, nil
		},
	}

	handler := NewTransactionHandler(mockService)

	// When: Making GET request with comma-separated categories
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/transactions?categories=Food+and+Drink,Transportation", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.Set("user_id", "user-123")

	err := handler.GetTransactions(c)

	// Then: Returns 200 with categories filter applied
	require.NoError(t, err, "Expected no error from handler")
	assert.Equal(t, http.StatusOK, rec.Code, "Expected 200 OK")
}

// TestGetTransactions_ParsesMinAmountQueryParam tests that min_amount is parsed correctly
func TestGetTransactions_ParsesMinAmountQueryParam(t *testing.T) {
	// Given: Mock service that captures min_amount filter
	mockService := &mockTransactionService{
		listTransactionsFunc: func(ctx context.Context, userID string, filters domain.TransactionFilters, limit, offset int) (*services.TransactionListResponse, error) {
			require.NotNil(t, filters.MinAmount, "Should have min_amount filter")
			assert.Equal(t, 10.50, *filters.MinAmount, "min_amount should be 10.50")
			return &services.TransactionListResponse{
				Transactions: []domain.Transaction{},
				TotalCount:   0,
				Limit:        50,
				Offset:       0,
				HasMore:      false,
			}, nil
		},
	}

	handler := NewTransactionHandler(mockService)

	// When: Making GET request with min_amount
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/transactions?min_amount=10.50", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.Set("user_id", "user-123")

	err := handler.GetTransactions(c)

	// Then: Returns 200 with min_amount filter applied
	require.NoError(t, err, "Expected no error from handler")
	assert.Equal(t, http.StatusOK, rec.Code, "Expected 200 OK")
}

// TestGetTransactions_ParsesMaxAmountQueryParam tests that max_amount is parsed correctly
func TestGetTransactions_ParsesMaxAmountQueryParam(t *testing.T) {
	// Given: Mock service that captures max_amount filter
	mockService := &mockTransactionService{
		listTransactionsFunc: func(ctx context.Context, userID string, filters domain.TransactionFilters, limit, offset int) (*services.TransactionListResponse, error) {
			require.NotNil(t, filters.MaxAmount, "Should have max_amount filter")
			assert.Equal(t, 500.00, *filters.MaxAmount, "max_amount should be 500.00")
			return &services.TransactionListResponse{
				Transactions: []domain.Transaction{},
				TotalCount:   0,
				Limit:        50,
				Offset:       0,
				HasMore:      false,
			}, nil
		},
	}

	handler := NewTransactionHandler(mockService)

	// When: Making GET request with max_amount
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/transactions?max_amount=500.00", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.Set("user_id", "user-123")

	err := handler.GetTransactions(c)

	// Then: Returns 200 with max_amount filter applied
	require.NoError(t, err, "Expected no error from handler")
	assert.Equal(t, http.StatusOK, rec.Code, "Expected 200 OK")
}

// TestGetTransactions_ParsesAmountRangeQueryParams tests that both min and max amount are parsed
func TestGetTransactions_ParsesAmountRangeQueryParams(t *testing.T) {
	// Given: Mock service that captures amount range filters
	mockService := &mockTransactionService{
		listTransactionsFunc: func(ctx context.Context, userID string, filters domain.TransactionFilters, limit, offset int) (*services.TransactionListResponse, error) {
			require.NotNil(t, filters.MinAmount, "Should have min_amount filter")
			require.NotNil(t, filters.MaxAmount, "Should have max_amount filter")
			assert.Equal(t, 25.00, *filters.MinAmount)
			assert.Equal(t, 100.00, *filters.MaxAmount)
			return &services.TransactionListResponse{
				Transactions: []domain.Transaction{},
				TotalCount:   0,
				Limit:        50,
				Offset:       0,
				HasMore:      false,
			}, nil
		},
	}

	handler := NewTransactionHandler(mockService)

	// When: Making GET request with min_amount and max_amount
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/transactions?min_amount=25&max_amount=100", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.Set("user_id", "user-123")

	err := handler.GetTransactions(c)

	// Then: Returns 200 with amount range filter applied
	require.NoError(t, err, "Expected no error from handler")
	assert.Equal(t, http.StatusOK, rec.Code, "Expected 200 OK")
}

// TestGetTransactions_ParsesAmountTypeIncome tests amount_type=income is parsed correctly
func TestGetTransactions_ParsesAmountTypeIncome(t *testing.T) {
	// Given: Mock service that captures amount_type filter
	mockService := &mockTransactionService{
		listTransactionsFunc: func(ctx context.Context, userID string, filters domain.TransactionFilters, limit, offset int) (*services.TransactionListResponse, error) {
			require.NotNil(t, filters.AmountType, "Should have amount_type filter")
			assert.Equal(t, "income", *filters.AmountType)
			return &services.TransactionListResponse{
				Transactions: []domain.Transaction{},
				TotalCount:   0,
				Limit:        50,
				Offset:       0,
				HasMore:      false,
			}, nil
		},
	}

	handler := NewTransactionHandler(mockService)

	// When: Making GET request with amount_type=income
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/transactions?amount_type=income", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.Set("user_id", "user-123")

	err := handler.GetTransactions(c)

	// Then: Returns 200 with amount_type filter applied
	require.NoError(t, err, "Expected no error from handler")
	assert.Equal(t, http.StatusOK, rec.Code, "Expected 200 OK")
}

// TestGetTransactions_ParsesAmountTypeExpense tests amount_type=expense is parsed correctly
func TestGetTransactions_ParsesAmountTypeExpense(t *testing.T) {
	// Given: Mock service that captures amount_type filter
	mockService := &mockTransactionService{
		listTransactionsFunc: func(ctx context.Context, userID string, filters domain.TransactionFilters, limit, offset int) (*services.TransactionListResponse, error) {
			require.NotNil(t, filters.AmountType, "Should have amount_type filter")
			assert.Equal(t, "expense", *filters.AmountType)
			return &services.TransactionListResponse{
				Transactions: []domain.Transaction{},
				TotalCount:   0,
				Limit:        50,
				Offset:       0,
				HasMore:      false,
			}, nil
		},
	}

	handler := NewTransactionHandler(mockService)

	// When: Making GET request with amount_type=expense
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/transactions?amount_type=expense", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.Set("user_id", "user-123")

	err := handler.GetTransactions(c)

	// Then: Returns 200 with amount_type filter applied
	require.NoError(t, err, "Expected no error from handler")
	assert.Equal(t, http.StatusOK, rec.Code, "Expected 200 OK")
}

// TestGetTransactions_InvalidMinAmount tests that invalid min_amount returns 400
func TestGetTransactions_InvalidMinAmount(t *testing.T) {
	// Given: Handler with mock service
	mockService := &mockTransactionService{}
	handler := NewTransactionHandler(mockService)

	// When: Making GET request with invalid min_amount
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/transactions?min_amount=not-a-number", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.Set("user_id", "user-123")

	err := handler.GetTransactions(c)

	// Then: Returns 400 Bad Request
	require.Error(t, err, "Expected error for invalid min_amount")
	httpErr, ok := err.(*echo.HTTPError)
	require.True(t, ok, "Expected echo.HTTPError")
	assert.Equal(t, http.StatusBadRequest, httpErr.Code, "Expected 400 Bad Request")
}

// TestGetTransactions_InvalidMaxAmount tests that invalid max_amount returns 400
func TestGetTransactions_InvalidMaxAmount(t *testing.T) {
	// Given: Handler with mock service
	mockService := &mockTransactionService{}
	handler := NewTransactionHandler(mockService)

	// When: Making GET request with invalid max_amount
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/transactions?max_amount=abc", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.Set("user_id", "user-123")

	err := handler.GetTransactions(c)

	// Then: Returns 400 Bad Request
	require.Error(t, err, "Expected error for invalid max_amount")
	httpErr, ok := err.(*echo.HTTPError)
	require.True(t, ok, "Expected echo.HTTPError")
	assert.Equal(t, http.StatusBadRequest, httpErr.Code, "Expected 400 Bad Request")
}

// TestGetTransactions_InvalidAmountType tests that invalid amount_type returns 400
func TestGetTransactions_InvalidAmountType(t *testing.T) {
	// Given: Handler with mock service
	mockService := &mockTransactionService{}
	handler := NewTransactionHandler(mockService)

	// When: Making GET request with invalid amount_type
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/transactions?amount_type=invalid", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.Set("user_id", "user-123")

	err := handler.GetTransactions(c)

	// Then: Returns 400 Bad Request
	require.Error(t, err, "Expected error for invalid amount_type")
	httpErr, ok := err.(*echo.HTTPError)
	require.True(t, ok, "Expected echo.HTTPError")
	assert.Equal(t, http.StatusBadRequest, httpErr.Code, "Expected 400 Bad Request")
}

// TestGetTransactions_InvalidAmountRange tests that min > max returns 400
func TestGetTransactions_InvalidAmountRange(t *testing.T) {
	// Given: Mock service that returns error for invalid amount range
	mockService := &mockTransactionService{
		listTransactionsFunc: func(ctx context.Context, userID string, filters domain.TransactionFilters, limit, offset int) (*services.TransactionListResponse, error) {
			return nil, services.ErrInvalidAmountRange
		},
	}

	handler := NewTransactionHandler(mockService)

	// When: Making GET request with min > max
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/transactions?min_amount=100&max_amount=50", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.Set("user_id", "user-123")

	err := handler.GetTransactions(c)

	// Then: Returns 400 Bad Request
	require.Error(t, err, "Expected error for invalid amount range")
	httpErr, ok := err.(*echo.HTTPError)
	require.True(t, ok, "Expected echo.HTTPError")
	assert.Equal(t, http.StatusBadRequest, httpErr.Code, "Expected 400 Bad Request")
}

// TestGetTransactions_CombinedExtendedFilters tests all new filters combined with existing filters
func TestGetTransactions_CombinedExtendedFilters(t *testing.T) {
	// Given: Mock service that captures all filters
	mockService := &mockTransactionService{
		listTransactionsFunc: func(ctx context.Context, userID string, filters domain.TransactionFilters, limit, offset int) (*services.TransactionListResponse, error) {
			// Check new multi-value filters
			require.NotNil(t, filters.AccountIDs, "Should have account_ids filter")
			assert.Len(t, filters.AccountIDs, 2)

			require.NotNil(t, filters.Categories, "Should have categories filter")
			assert.Len(t, filters.Categories, 1)
			assert.Equal(t, "Food", filters.Categories[0])

			// Check amount filters
			require.NotNil(t, filters.MinAmount, "Should have min_amount filter")
			assert.Equal(t, 10.00, *filters.MinAmount)

			require.NotNil(t, filters.MaxAmount, "Should have max_amount filter")
			assert.Equal(t, 200.00, *filters.MaxAmount)

			require.NotNil(t, filters.AmountType, "Should have amount_type filter")
			assert.Equal(t, "expense", *filters.AmountType)

			// Check existing filters still work
			require.NotNil(t, filters.Search, "Should have search filter")
			assert.Equal(t, "coffee", *filters.Search)

			require.NotNil(t, filters.StartDate, "Should have start_date filter")
			require.NotNil(t, filters.EndDate, "Should have end_date filter")

			return &services.TransactionListResponse{
				Transactions: []domain.Transaction{},
				TotalCount:   0,
				Limit:        50,
				Offset:       0,
				HasMore:      false,
			}, nil
		},
	}

	handler := NewTransactionHandler(mockService)

	// When: Making GET request with all filter types
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/transactions?account_ids=acc-1,acc-2&categories=Food&min_amount=10&max_amount=200&amount_type=expense&search=coffee&start_date=2025-01-01&end_date=2025-12-31", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.Set("user_id", "user-123")

	err := handler.GetTransactions(c)

	// Then: Returns 200 with all filters applied
	require.NoError(t, err, "Expected no error from handler")
	assert.Equal(t, http.StatusOK, rec.Code, "Expected 200 OK")
}
