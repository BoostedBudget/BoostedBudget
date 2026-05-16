package operations

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/danielgtaylor/huma/v2/humatest"
	"github.com/google/uuid"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/huma/inputs"
	"github.com/maxnussbaum/BoostedBudget/internal/repository"
	"github.com/maxnussbaum/BoostedBudget/internal/services"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockTransactionService is a mock implementation of services.TransactionService
type mockTransactionService struct {
	listResponse      *services.TransactionListResponse
	listErr           error
	transaction       *domain.Transaction
	getErr            error
	updateTagsErr     error
	updateCategoryErr error
	categories        []string
	categoriesErr     error
	capturedFilters   domain.TransactionFilters
	capturedCursor    *domain.TransactionCursor
	capturedLimit     int
}

func (m *mockTransactionService) ListTransactions(
	ctx context.Context,
	userID string,
	filters domain.TransactionFilters,
	limit int,
	offset int,
) (*services.TransactionListResponse, error) {
	m.capturedFilters = filters
	if m.listErr != nil {
		return nil, m.listErr
	}
	return m.listResponse, nil
}

func (m *mockTransactionService) GetTransactionByID(ctx context.Context, userID, transactionID string) (*domain.Transaction, error) {
	if m.getErr != nil {
		return nil, m.getErr
	}
	return m.transaction, nil
}

func (m *mockTransactionService) UpdateTransactionTags(ctx context.Context, userID, transactionID string, tags []string) (*domain.Transaction, error) {
	if m.updateTagsErr != nil {
		return nil, m.updateTagsErr
	}
	// Return transaction with updated tags
	tx := m.transaction
	if tx != nil {
		tx.Tags = tags
	}
	return tx, nil
}

func (m *mockTransactionService) UpdateTransactionCategory(ctx context.Context, userID, transactionID string, overrideKind string, pfcPrimary *string, pfcDetailed *string, customCategoryID *string) (*domain.Transaction, error) {
	if m.updateCategoryErr != nil {
		return nil, m.updateCategoryErr
	}
	return m.transaction, nil
}

func (m *mockTransactionService) GetDistinctPlaidCategories(ctx context.Context, userID string) ([]string, error) {
	if m.categoriesErr != nil {
		return nil, m.categoriesErr
	}
	return m.categories, nil
}

func (m *mockTransactionService) ListByBudgetCategory(ctx context.Context, userID string, budgetCategory *domain.BudgetCategory, startDate, endDate time.Time, limit, offset int) ([]domain.Transaction, int, float64, error) {
	return nil, 0, 0, nil
}

func (m *mockTransactionService) ListTransactionsByCursor(ctx context.Context, userID string, filters domain.TransactionFilters, cursor *domain.TransactionCursor, limit int) (*services.TransactionListResponse, error) {
	m.capturedFilters = filters
	m.capturedCursor = cursor
	m.capturedLimit = limit
	if m.listErr != nil {
		return nil, m.listErr
	}
	return m.listResponse, nil
}

// Helper to create a test transaction
func createTestTransaction(userID string) *domain.Transaction {
	now := time.Now()
	merchantName := "Test Store"
	return &domain.Transaction{
		ID:           "tx-123",
		UserID:       userID,
		AccountID:    "acc-123",
		LinkedItemID: "item-123",
		Source:       domain.SourcePlaid,
		Amount:       42.50,
		Date:         now,
		Name:         "Test Transaction",
		MerchantName: &merchantName,
		Pending:      false,
		Tags:         []string{},
		CreatedAt:    now,
		UpdatedAt:    now,
	}
}

// TestGetTransactions_Success tests successful transaction listing.
func TestGetTransactions_Success(t *testing.T) {
	userID := "user-123"
	tx := createTestTransaction(userID)

	mockService := &mockTransactionService{
		listResponse: &services.TransactionListResponse{
			Transactions: []*domain.Transaction{tx},
			Limit:        50,
		},
	}

	handler := NewTransactionHandler(mockService)

	input := &GetTransactionsInput{
		AuthenticatedInput:   inputs.AuthenticatedInput{UserID: userID},
		CursorPaginationInput: inputs.CursorPaginationInput{Limit: 50},
	}

	output, err := handler.GetTransactions(context.Background(), input)

	require.NoError(t, err)
	require.NotNil(t, output)
	assert.Len(t, output.Body.Transactions, 1)
	assert.Equal(t, 50, output.Body.Limit)
	assert.Nil(t, output.Body.NextCursor)
}

// TestGetTransactions_WithFilters tests transaction listing with filters.
func TestGetTransactions_WithFilters(t *testing.T) {
	userID := "user-123"
	mockService := &mockTransactionService{
		listResponse: &services.TransactionListResponse{
			Transactions: []*domain.Transaction{},
			Limit:        50,
		},
	}

	handler := NewTransactionHandler(mockService)

	input := &GetTransactionsInput{
		AuthenticatedInput:   inputs.AuthenticatedInput{UserID: userID},
		CursorPaginationInput: inputs.CursorPaginationInput{Limit: 50},
		AccountIDs:           "acc-1,acc-2,acc-3",
		PFCPrimaries:         "FOOD_AND_DRINK,SHOPPING",
		Search:               "coffee",
		Pending:              "true",
		MinAmount:            "10.00",
		MaxAmount:            "100.00",
		AmountType:           "expense",
		StartDate:            "2024-01-01",
		EndDate:              "2024-12-31",
	}

	output, err := handler.GetTransactions(context.Background(), input)

	require.NoError(t, err)
	require.NotNil(t, output)
}

// TestGetTransactions_InvalidDateFormat tests error for invalid date format.
func TestGetTransactions_InvalidDateFormat(t *testing.T) {
	userID := "user-123"
	mockService := &mockTransactionService{}
	handler := NewTransactionHandler(mockService)

	input := &GetTransactionsInput{
		AuthenticatedInput:   inputs.AuthenticatedInput{UserID: userID},
		CursorPaginationInput: inputs.CursorPaginationInput{Limit: 50},
		StartDate:            "invalid-date",
	}

	output, err := handler.GetTransactions(context.Background(), input)

	assert.Nil(t, output)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "Invalid start_date format")
}

// TestGetTransactions_InvalidAmount tests error for invalid amount.
func TestGetTransactions_InvalidAmount(t *testing.T) {
	userID := "user-123"
	mockService := &mockTransactionService{}
	handler := NewTransactionHandler(mockService)

	input := &GetTransactionsInput{
		AuthenticatedInput:   inputs.AuthenticatedInput{UserID: userID},
		CursorPaginationInput: inputs.CursorPaginationInput{Limit: 50},
		MinAmount:            "not-a-number",
	}

	output, err := handler.GetTransactions(context.Background(), input)

	assert.Nil(t, output)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "Invalid min_amount")
}

// TestGetTransactions_ServiceError tests that service errors are mapped.
func TestGetTransactions_ServiceError(t *testing.T) {
	userID := "user-123"
	mockService := &mockTransactionService{
		listErr: services.ErrInvalidDateRange,
	}

	handler := NewTransactionHandler(mockService)

	input := &GetTransactionsInput{
		AuthenticatedInput:   inputs.AuthenticatedInput{UserID: userID},
		CursorPaginationInput: inputs.CursorPaginationInput{Limit: 50},
	}

	output, err := handler.GetTransactions(context.Background(), input)

	assert.Nil(t, output)
	require.Error(t, err)
}

// TestGetTransactionByID_Success tests successful transaction retrieval.
func TestGetTransactionByID_Success(t *testing.T) {
	userID := "user-123"
	tx := createTestTransaction(userID)

	mockService := &mockTransactionService{
		transaction: tx,
	}

	handler := NewTransactionHandler(mockService)

	input := &GetTransactionByIDInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: userID},
		ID:                 "tx-123",
	}

	output, err := handler.GetTransactionByID(context.Background(), input)

	require.NoError(t, err)
	require.NotNil(t, output)
	assert.Equal(t, "tx-123", output.Body.ID)
	assert.Equal(t, "Test Transaction", output.Body.Name)
}

// TestGetTransactionByID_NotFound tests 404 for non-existent transaction.
func TestGetTransactionByID_NotFound(t *testing.T) {
	mockService := &mockTransactionService{
		getErr: repository.ErrTransactionNotFound,
	}

	handler := NewTransactionHandler(mockService)

	input := &GetTransactionByIDInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-123"},
		ID:                 "nonexistent",
	}

	output, err := handler.GetTransactionByID(context.Background(), input)

	assert.Nil(t, output)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

// TestUpdateTransactionTags_Success tests successful tag update.
func TestUpdateTransactionTags_Success(t *testing.T) {
	userID := "user-123"
	tx := createTestTransaction(userID)

	mockService := &mockTransactionService{
		transaction: tx,
	}

	handler := NewTransactionHandler(mockService)

	input := &UpdateTransactionTagsInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: userID},
		ID:                 "tx-123",
		Body: UpdateTransactionTagsBody{
			Tags: []string{"groceries", "monthly"},
		},
	}

	output, err := handler.UpdateTransactionTags(context.Background(), input)

	require.NoError(t, err)
	require.NotNil(t, output)
	assert.Equal(t, []string{"groceries", "monthly"}, output.Body.Tags)
}

// TestUpdateTransactionTags_NilTags tests that nil tags are handled.
func TestUpdateTransactionTags_NilTags(t *testing.T) {
	userID := "user-123"
	tx := createTestTransaction(userID)

	mockService := &mockTransactionService{
		transaction: tx,
	}

	handler := NewTransactionHandler(mockService)

	input := &UpdateTransactionTagsInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: userID},
		ID:                 "tx-123",
		Body: UpdateTransactionTagsBody{
			Tags: nil,
		},
	}

	output, err := handler.UpdateTransactionTags(context.Background(), input)

	require.NoError(t, err)
	require.NotNil(t, output)
	assert.Empty(t, output.Body.Tags)
}

// TestUpdateTransactionTags_NotFound tests 404 for non-existent transaction.
func TestUpdateTransactionTags_NotFound(t *testing.T) {
	mockService := &mockTransactionService{
		updateTagsErr: repository.ErrTransactionNotFound,
	}

	handler := NewTransactionHandler(mockService)

	input := &UpdateTransactionTagsInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-123"},
		ID:                 "nonexistent",
		Body: UpdateTransactionTagsBody{
			Tags: []string{"tag1"},
		},
	}

	output, err := handler.UpdateTransactionTags(context.Background(), input)

	assert.Nil(t, output)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

// TestUpdateTransactionCategory_Success tests successful category update.
func TestUpdateTransactionCategory_Success(t *testing.T) {
	userID := "user-123"
	tx := createTestTransaction(userID)

	mockService := &mockTransactionService{
		transaction: tx,
	}

	handler := NewTransactionHandler(mockService)

	pfcPrimary := "FOOD_AND_DRINK"
	input := &UpdateTransactionCategoryInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: userID},
		ID:                 "tx-123",
		Body: UpdateTransactionCategoryBody{
			Kind:       "pfc",
			PFCPrimary: &pfcPrimary,
		},
	}

	output, err := handler.UpdateTransactionCategory(context.Background(), input)

	require.NoError(t, err)
	require.NotNil(t, output)
}

// TestUpdateTransactionCategory_NotFound tests 404 for non-existent transaction.
func TestUpdateTransactionCategory_NotFound(t *testing.T) {
	mockService := &mockTransactionService{
		updateCategoryErr: repository.ErrTransactionNotFound,
	}

	handler := NewTransactionHandler(mockService)

	pfcPrimary := "FOOD_AND_DRINK"
	input := &UpdateTransactionCategoryInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-123"},
		ID:                 "nonexistent",
		Body: UpdateTransactionCategoryBody{
			Kind:       "pfc",
			PFCPrimary: &pfcPrimary,
		},
	}

	output, err := handler.UpdateTransactionCategory(context.Background(), input)

	assert.Nil(t, output)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

// TestGetPlaidCategories_Success tests successful category retrieval.
func TestGetPlaidCategories_Success(t *testing.T) {
	userID := "user-123"
	mockService := &mockTransactionService{
		categories: []string{"Food and Drink", "Shopping", "Travel"},
	}

	handler := NewTransactionHandler(mockService)

	input := &GetPlaidCategoriesInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: userID},
	}

	output, err := handler.GetPlaidCategories(context.Background(), input)

	require.NoError(t, err)
	require.NotNil(t, output)
	assert.Len(t, output.Body, 3)
	assert.Contains(t, output.Body, "Food and Drink")
}

// TestGetPlaidCategories_EmptyList tests that empty array is returned instead of null.
func TestGetPlaidCategories_EmptyList(t *testing.T) {
	userID := "user-123"
	mockService := &mockTransactionService{
		categories: nil,
	}

	handler := NewTransactionHandler(mockService)

	input := &GetPlaidCategoriesInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: userID},
	}

	output, err := handler.GetPlaidCategories(context.Background(), input)

	require.NoError(t, err)
	require.NotNil(t, output)
	assert.NotNil(t, output.Body, "Should return empty array, not nil")
	assert.Len(t, output.Body, 0)
}

// TestTransactionRouteRegistration tests that routes are properly registered.
func TestTransactionRouteRegistration(t *testing.T) {
	mockService := &mockTransactionService{
		listResponse: &services.TransactionListResponse{
			Transactions: []*domain.Transaction{},
			Limit:        50,
		},
	}

	_, api := humatest.New(t)
	handler := NewTransactionHandler(mockService)
	RegisterTransactionRoutes(api, handler)

	// Test GET /api/v1/transactions (without auth, should fail with 401)
	resp := api.Get("/api/v1/transactions")
	assert.Equal(t, http.StatusUnauthorized, resp.Code)

	// Verify the response contains expected error message
	body := resp.Body.String()
	assert.Contains(t, body, "User not authenticated")
}

// TestParseCommaSeparated tests the comma-separated parsing helper.
func TestParseCommaSeparated(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected []string
	}{
		{
			name:     "simple values",
			input:    "a,b,c",
			expected: []string{"a", "b", "c"},
		},
		{
			name:     "with spaces",
			input:    "a, b , c ",
			expected: []string{"a", "b", "c"},
		},
		{
			name:     "empty strings filtered",
			input:    "a,,b,,,c",
			expected: []string{"a", "b", "c"},
		},
		{
			name:     "single value",
			input:    "single",
			expected: []string{"single"},
		},
		{
			name:     "empty string",
			input:    "",
			expected: []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := parseCommaSeparated(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// TestTransactionListResponseJSON tests JSON serialization of response.
func TestTransactionListResponseJSON(t *testing.T) {
	userID := "user-123"
	tx := createTestTransaction(userID)

	mockService := &mockTransactionService{
		listResponse: &services.TransactionListResponse{
			Transactions: []*domain.Transaction{tx},
			TotalCount:   1,
			Limit:        50,
			Offset:       0,
			HasMore:      false,
		},
	}

	_, api := humatest.New(t)
	handler := NewTransactionHandler(mockService)
	RegisterTransactionRoutes(api, handler)

	// This test verifies the JSON structure when properly authenticated
	// For now, just check it's registered
	resp := api.Get("/api/v1/transactions")
	assert.Equal(t, http.StatusUnauthorized, resp.Code)

	// Verify error response is valid JSON
	var errorResp map[string]interface{}
	err := json.Unmarshal(resp.Body.Bytes(), &errorResp)
	require.NoError(t, err, "Error response should be valid JSON")
}

func TestGetTransactions_UserAccountIDFilter(t *testing.T) {
	// Verify that ?user_account_id=<uuid> is parsed from the query string
	// and forwarded to the service layer as filters.UserAccountID (Issue #1109).
	userID := "user-123"
	mockService := &mockTransactionService{
		listResponse: &services.TransactionListResponse{
			Transactions: []*domain.Transaction{},
			Limit:        50,
		},
	}

	handler := NewTransactionHandler(mockService)

	userAccountID := "some-uuid-stable-acct"
	input := &GetTransactionsInput{
		AuthenticatedInput:   inputs.AuthenticatedInput{UserID: userID},
		CursorPaginationInput: inputs.CursorPaginationInput{Limit: 50},
		UserAccountID:        userAccountID,
	}

	_, err := handler.GetTransactions(context.Background(), input)

	require.NoError(t, err)
	require.NotNil(t, mockService.capturedFilters.UserAccountID,
		"UserAccountID should be plumbed through as filters.UserAccountID")
	assert.Equal(t, userAccountID, *mockService.capturedFilters.UserAccountID,
		"captured UserAccountID should match input")
}


// TestGetTransactions_SortDefaults tests that empty sort params produce default
// SortBy="date" and SortDir="desc" in filters forwarded to the service.
func TestGetTransactions_SortDefaults(t *testing.T) {
	userID := "user-sort-def"
	mockService := &mockTransactionService{
		listResponse: &services.TransactionListResponse{},
	}
	handler := NewTransactionHandler(mockService)

	input := &GetTransactionsInput{
		AuthenticatedInput:   inputs.AuthenticatedInput{UserID: userID},
		CursorPaginationInput: inputs.CursorPaginationInput{Limit: 50},
		// SortBy and SortDir intentionally left empty — Huma default fills them
	}

	_, err := handler.GetTransactions(context.Background(), input)
	require.NoError(t, err)
	assert.Equal(t, "date", mockService.capturedFilters.SortBy,
		"default SortBy should be 'date'")
	assert.Equal(t, "desc", mockService.capturedFilters.SortDir,
		"default SortDir should be 'desc'")
}

// TestGetTransactions_SortByAmount tests that sort_by=amount&sort_dir=asc
// propagates through to filters.
func TestGetTransactions_SortByAmount(t *testing.T) {
	userID := "user-sort-amount"
	mockService := &mockTransactionService{
		listResponse: &services.TransactionListResponse{},
	}
	handler := NewTransactionHandler(mockService)

	input := &GetTransactionsInput{
		AuthenticatedInput:   inputs.AuthenticatedInput{UserID: userID},
		CursorPaginationInput: inputs.CursorPaginationInput{Limit: 50},
		SortBy:               "amount",
		SortDir:              "asc",
	}

	_, err := handler.GetTransactions(context.Background(), input)
	require.NoError(t, err)
	assert.Equal(t, "amount", mockService.capturedFilters.SortBy)
	assert.Equal(t, "asc", mockService.capturedFilters.SortDir)
}

// TestGetTransactions_SortByMerchant tests that sort_by=merchant&sort_dir=desc
// propagates through to filters.
func TestGetTransactions_SortByMerchant(t *testing.T) {
	userID := "user-sort-merchant"
	mockService := &mockTransactionService{
		listResponse: &services.TransactionListResponse{},
	}
	handler := NewTransactionHandler(mockService)

	input := &GetTransactionsInput{
		AuthenticatedInput:   inputs.AuthenticatedInput{UserID: userID},
		CursorPaginationInput: inputs.CursorPaginationInput{Limit: 50},
		SortBy:               "merchant",
		SortDir:              "desc",
	}

	_, err := handler.GetTransactions(context.Background(), input)
	require.NoError(t, err)
	assert.Equal(t, "merchant", mockService.capturedFilters.SortBy)
	assert.Equal(t, "desc", mockService.capturedFilters.SortDir)
}

// TestGetTransactions_SortByInvalid tests that an unknown sort_by value is rejected by Huma's
// enum validation. In the test environment, auth fails too, so Huma returns 401 with both
// errors in the body. The enum error being present confirms the validation fired.
func TestGetTransactions_SortByInvalid(t *testing.T) {
	mockService := &mockTransactionService{
		listResponse: &services.TransactionListResponse{},
	}
	_, api := humatest.New(t)
	handler := NewTransactionHandler(mockService)
	RegisterTransactionRoutes(api, handler)

	resp := api.Get("/api/v1/transactions?sort_by=hax")
	// Auth also fails in test, so status is 401 (higher priority than 422).
	assert.Equal(t, http.StatusUnauthorized, resp.Code)
	assert.Contains(t, resp.Body.String(), "expected value to be one of")
}

// TestGetTransactions_SortByCategory tests that sort_by=category&sort_dir=asc
// propagates through to filters.
func TestGetTransactions_SortByCategory(t *testing.T) {
	userID := "user-sort-cat"
	mockService := &mockTransactionService{
		listResponse: &services.TransactionListResponse{},
	}
	handler := NewTransactionHandler(mockService)

	input := &GetTransactionsInput{
		AuthenticatedInput:   inputs.AuthenticatedInput{UserID: userID},
		CursorPaginationInput: inputs.CursorPaginationInput{Limit: 50},
		SortBy:               "category",
		SortDir:              "asc",
	}

	_, err := handler.GetTransactions(context.Background(), input)
	require.NoError(t, err)
	assert.Equal(t, "category", mockService.capturedFilters.SortBy)
	assert.Equal(t, "asc", mockService.capturedFilters.SortDir)
}

// --- Cursor pagination handler tests (RED phase) ---

// TestGetTransactions_CursorFirstPage verifies that a request with no cursor calls the
// service with cursor=nil and returns transactions + no next_cursor.
func TestGetTransactions_CursorFirstPage(t *testing.T) {
	userID := "user-cursor-1"
	tx := createTestTransaction(userID)
	nextCursor := "opaque-cursor-token"
	mockService := &mockTransactionService{
		listResponse: &services.TransactionListResponse{
			Transactions: []*domain.Transaction{tx},
			Limit:        10,
			NextCursor:   &nextCursor,
		},
	}
	handler := NewTransactionHandler(mockService)

	input := &GetTransactionsInput{
		AuthenticatedInput:   inputs.AuthenticatedInput{UserID: userID},
		CursorPaginationInput: inputs.CursorPaginationInput{Limit: 10},
		SortBy:               "date",
		SortDir:              "desc",
	}

	output, err := handler.GetTransactions(context.Background(), input)

	require.NoError(t, err)
	require.NotNil(t, output)
	assert.Nil(t, mockService.capturedCursor, "first page should pass nil cursor to service")
	assert.Equal(t, 10, mockService.capturedLimit)
	assert.Len(t, output.Body.Transactions, 1)
	require.NotNil(t, output.Body.NextCursor)
	assert.Equal(t, nextCursor, *output.Body.NextCursor)
}

// TestGetTransactions_CursorSecondPage verifies that a valid cursor string is decoded and
// passed to the service, and that the returned next_cursor is forwarded.
func TestGetTransactions_CursorSecondPage(t *testing.T) {
	userID := "user-cursor-2"

	// Build a valid v1 cursor for sort_by=date, sort_dir=desc
	sv := "2024-01-15"
	cursor := domain.TransactionCursor{
		V:  1,
		K:  "date",
		D:  "desc",
		SV: &sv,
		ID: uuid.MustParse("aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"),
	}
	encodedCursor, err := domain.EncodeTransactionCursor(cursor)
	require.NoError(t, err)

	mockService := &mockTransactionService{
		listResponse: &services.TransactionListResponse{
			Transactions: []*domain.Transaction{},
			Limit:        10,
		},
	}
	handler := NewTransactionHandler(mockService)

	input := &GetTransactionsInput{
		AuthenticatedInput:   inputs.AuthenticatedInput{UserID: userID},
		CursorPaginationInput: inputs.CursorPaginationInput{Cursor: encodedCursor, Limit: 10},
		SortBy:               "date",
		SortDir:              "desc",
	}

	output, err := handler.GetTransactions(context.Background(), input)

	require.NoError(t, err)
	require.NotNil(t, output)
	require.NotNil(t, mockService.capturedCursor, "second page should pass decoded cursor to service")
	assert.Equal(t, "date", mockService.capturedCursor.K)
	assert.Equal(t, "desc", mockService.capturedCursor.D)
	assert.Nil(t, output.Body.NextCursor, "no more pages")
}

// TestGetTransactions_InvalidCursor verifies that a corrupt cursor string yields a 400.
func TestGetTransactions_InvalidCursor(t *testing.T) {
	userID := "user-cursor-bad"
	mockService := &mockTransactionService{}
	handler := NewTransactionHandler(mockService)

	input := &GetTransactionsInput{
		AuthenticatedInput:   inputs.AuthenticatedInput{UserID: userID},
		CursorPaginationInput: inputs.CursorPaginationInput{Cursor: "!!!not-base64!!!", Limit: 10},
		SortBy:               "date",
		SortDir:              "desc",
	}

	output, err := handler.GetTransactions(context.Background(), input)

	assert.Nil(t, output)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid cursor")
}

// TestGetTransactions_CursorSortMismatch verifies that a cursor with K="date" but
// sort_by="merchant" in the request yields a 400.
func TestGetTransactions_CursorSortMismatch(t *testing.T) {
	userID := "user-cursor-mismatch"

	// Cursor encodes sort_by=date
	sv := "2024-01-15"
	cursor := domain.TransactionCursor{
		V:  1,
		K:  "date",
		D:  "desc",
		SV: &sv,
		ID: uuid.MustParse("aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"),
	}
	encodedCursor, err := domain.EncodeTransactionCursor(cursor)
	require.NoError(t, err)

	mockService := &mockTransactionService{}
	handler := NewTransactionHandler(mockService)

	// But request says sort_by=merchant — mismatch
	input := &GetTransactionsInput{
		AuthenticatedInput:   inputs.AuthenticatedInput{UserID: userID},
		CursorPaginationInput: inputs.CursorPaginationInput{Cursor: encodedCursor, Limit: 10},
		SortBy:               "merchant",
		SortDir:              "desc",
	}

	output, err := handler.GetTransactions(context.Background(), input)

	assert.Nil(t, output)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "cursor does not match sort_by/sort_dir")
}

// TestGetTransactions_CursorVersionUnsupported verifies that a cursor with V=2 yields a 400.
func TestGetTransactions_CursorVersionUnsupported(t *testing.T) {
	userID := "user-cursor-ver"

	// Manually encode a cursor with V=2 (unsupported version) bypassing DecodeTransactionCursor
	v2Cursor := domain.TransactionCursor{
		V:  2,
		K:  "date",
		D:  "desc",
		ID: uuid.MustParse("aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"),
	}
	encodedCursor, err := domain.EncodeTransactionCursor(v2Cursor)
	require.NoError(t, err)

	mockService := &mockTransactionService{}
	handler := NewTransactionHandler(mockService)

	input := &GetTransactionsInput{
		AuthenticatedInput:   inputs.AuthenticatedInput{UserID: userID},
		CursorPaginationInput: inputs.CursorPaginationInput{Cursor: encodedCursor, Limit: 10},
		SortBy:               "date",
		SortDir:              "desc",
	}

	output, err := handler.GetTransactions(context.Background(), input)

	assert.Nil(t, output)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "cursor version not supported")
}
