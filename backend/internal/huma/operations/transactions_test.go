package operations

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/danielgtaylor/huma/v2/humatest"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/huma/inputs"
	"github.com/maxnussbaum/BoostedBudget/internal/repository"
	"github.com/maxnussbaum/BoostedBudget/internal/services"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockTransactionService is a mock implementation of services.TransactionService
type mockTransactionService struct {
	listResponse     *services.TransactionListResponse
	listErr          error
	transaction      *domain.Transaction
	getErr           error
	updateTagsErr    error
	updateCategoryErr error
	categories       []string
	categoriesErr    error
}

func (m *mockTransactionService) ListTransactions(
	ctx context.Context,
	userID string,
	filters domain.TransactionFilters,
	limit int,
	offset int,
) (*services.TransactionListResponse, error) {
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

func (m *mockTransactionService) UpdateTransactionCategory(ctx context.Context, userID, transactionID string, category []string) (*domain.Transaction, error) {
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
		Amount:       -42.50,
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
			Transactions: []domain.Transaction{*tx},
			TotalCount:   1,
			Limit:        50,
			Offset:       0,
			HasMore:      false,
		},
	}

	handler := NewTransactionHandler(mockService)

	input := &GetTransactionsInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: userID},
		PaginationInput:    inputs.PaginationInput{Limit: 50, Offset: 0},
	}

	output, err := handler.GetTransactions(context.Background(), input)

	require.NoError(t, err)
	require.NotNil(t, output)
	assert.Len(t, output.Body.Transactions, 1)
	assert.Equal(t, 1, output.Body.TotalCount)
	assert.False(t, output.Body.HasMore)
}

// TestGetTransactions_WithFilters tests transaction listing with filters.
func TestGetTransactions_WithFilters(t *testing.T) {
	userID := "user-123"
	mockService := &mockTransactionService{
		listResponse: &services.TransactionListResponse{
			Transactions: []domain.Transaction{},
			TotalCount:   0,
			Limit:        50,
			Offset:       0,
			HasMore:      false,
		},
	}

	handler := NewTransactionHandler(mockService)

	input := &GetTransactionsInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: userID},
		PaginationInput:    inputs.PaginationInput{Limit: 50, Offset: 0},
		AccountIDs:         "acc-1,acc-2,acc-3",
		Categories:         "Food,Shopping",
		Search:             "coffee",
		Pending:            "true",
		MinAmount:          "10.00",
		MaxAmount:          "100.00",
		AmountType:         "expense",
		StartDate:          "2024-01-01",
		EndDate:            "2024-12-31",
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
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: userID},
		PaginationInput:    inputs.PaginationInput{Limit: 50, Offset: 0},
		StartDate:          "invalid-date",
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
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: userID},
		PaginationInput:    inputs.PaginationInput{Limit: 50, Offset: 0},
		MinAmount:          "not-a-number",
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
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: userID},
		PaginationInput:    inputs.PaginationInput{Limit: 50, Offset: 0},
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

	input := &UpdateTransactionCategoryInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: userID},
		ID:                 "tx-123",
		Body: UpdateTransactionCategoryBody{
			Category: []string{"Food and Drink", "Restaurants"},
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

	input := &UpdateTransactionCategoryInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-123"},
		ID:                 "nonexistent",
		Body: UpdateTransactionCategoryBody{
			Category: []string{"Food"},
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
			Transactions: []domain.Transaction{},
			TotalCount:   0,
			Limit:        50,
			Offset:       0,
			HasMore:      false,
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
			Transactions: []domain.Transaction{*tx},
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
