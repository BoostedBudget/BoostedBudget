package operations

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/humatest"
	"github.com/google/uuid"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/huma/inputs"
	phclient "github.com/maxnussbaum/BoostedBudget/internal/integration/posthog"
	"github.com/maxnussbaum/BoostedBudget/internal/repository"
	"github.com/maxnussbaum/BoostedBudget/internal/services"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockTransactionService is a mock implementation of services.TransactionService
type mockTransactionService struct {
	listResponse         *services.TransactionListResponse
	listErr              error
	transaction          *domain.Transaction
	getErr               error
	updateTagsErr        error
	updateCategoryErr    error
	categories           []string
	categoriesErr        error
	capturedFilters      domain.TransactionFilters
	capturedCursor       *domain.TransactionCursor
	capturedLimit        int
	setReviewedFn        func(ctx context.Context, userID string, transactionIDs []string, reviewed bool) (int64, error)
	bulkUpdateCategoryFn func(ctx context.Context, userID string, ids []string, kind string, pfcPrimary, pfcDetailed, customCategoryID *string) (int64, error)
	reviewSummaryFn      func(ctx context.Context, userID string, filters domain.ReviewSummaryFilters) (*domain.ReviewSummaryResponse, error)
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

func (m *mockTransactionService) SetReviewed(ctx context.Context, userID string, transactionIDs []string, reviewed bool) (int64, error) {
	if m.setReviewedFn != nil {
		return m.setReviewedFn(ctx, userID, transactionIDs, reviewed)
	}
	return int64(len(transactionIDs)), nil
}

func (m *mockTransactionService) BulkUpdateTransactionCategory(ctx context.Context, userID string, ids []string, kind string, pfcPrimary, pfcDetailed, customCategoryID *string) (int64, error) {
	if m.bulkUpdateCategoryFn != nil {
		return m.bulkUpdateCategoryFn(ctx, userID, ids, kind, pfcPrimary, pfcDetailed, customCategoryID)
	}
	return int64(len(ids)), nil
}

func (m *mockTransactionService) GetReviewSummary(ctx context.Context, userID string, filters domain.ReviewSummaryFilters) (*domain.ReviewSummaryResponse, error) {
	if m.reviewSummaryFn != nil {
		return m.reviewSummaryFn(ctx, userID, filters)
	}
	return &domain.ReviewSummaryResponse{TopUnreviewedCategories: []domain.TopUnreviewedCategory{}}, nil
}

func (m *mockTransactionService) SetAnalytics(_ phclient.Analytics) {
	// no-op in tests
}

func (m *mockTransactionService) SetAccountRepository(_ repository.AccountRepository) {
	// no-op in tests
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
			Transactions: []*domain.ActivityItem{domain.NewBankingActivityItem(tx)},
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

// TestGetTransactions_EmbeddedAccountInResponse verifies that an investment ActivityItem
// with a populated Account field is serialized correctly through the handler.
// This is a regression guard: a serialization change that drops the account field
// would not be caught by unit tests alone.
func TestGetTransactions_EmbeddedAccountInResponse(t *testing.T) {
	userID := "user-123"
	nickname := "My Brokerage"
	acct := &domain.Account{ID: "acc-invest-1", Nickname: &nickname}
	item := &domain.ActivityItem{
		Source:    domain.FeedSourceInvestment,
		ID:        "inv-tx-1",
		UserID:    userID,
		AccountID: "acc-invest-1",
		Name:      "Dividend",
		Account:   acct,
	}

	mockService := &mockTransactionService{
		listResponse: &services.TransactionListResponse{
			Transactions: []*domain.ActivityItem{item},
			Limit:        50,
		},
	}

	handler := NewTransactionHandler(mockService)

	input := &GetTransactionsInput{
		AuthenticatedInput:    inputs.AuthenticatedInput{UserID: userID},
		CursorPaginationInput: inputs.CursorPaginationInput{Limit: 50},
	}

	output, err := handler.GetTransactions(context.Background(), input)

	require.NoError(t, err)
	require.Len(t, output.Body.Transactions, 1)
	require.NotNil(t, output.Body.Transactions[0].Account, "embedded account must not be nil for investment rows")
	assert.Equal(t, "acc-invest-1", output.Body.Transactions[0].Account.ID)
	require.NotNil(t, output.Body.Transactions[0].Account.Nickname)
	assert.Equal(t, nickname, *output.Body.Transactions[0].Account.Nickname)
}

// TestGetTransactions_WithFilters tests transaction listing with filters.
func TestGetTransactions_WithFilters(t *testing.T) {
	userID := "user-123"
	mockService := &mockTransactionService{
		listResponse: &services.TransactionListResponse{
			Transactions: []*domain.ActivityItem{},
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

// TestGetTransactions_PFCDetailedsParsing verifies that the pfc_detaileds query
// parameter is parsed into the correct slice on capturedFilters.PFCDetaileds.
func TestGetTransactions_PFCDetailedsParsing(t *testing.T) {
	userID := "user-123"
	mockService := &mockTransactionService{
		listResponse: &services.TransactionListResponse{
			Transactions: []*domain.ActivityItem{},
			Limit:        50,
		},
	}

	handler := NewTransactionHandler(mockService)

	input := &GetTransactionsInput{
		AuthenticatedInput:    inputs.AuthenticatedInput{UserID: userID},
		CursorPaginationInput: inputs.CursorPaginationInput{Limit: 50},
		PFCDetaileds:          "FOOD_AND_DRINK_RESTAURANTS,TRANSPORTATION_GAS",
	}

	output, err := handler.GetTransactions(context.Background(), input)

	require.NoError(t, err)
	require.NotNil(t, output)
	assert.Equal(t, []string{"FOOD_AND_DRINK_RESTAURANTS", "TRANSPORTATION_GAS"}, mockService.capturedFilters.PFCDetaileds)
}

// TestGetTransactions_CustomCategoryIDsParsing verifies the custom_category_ids query
// parameter is parsed into capturedFilters.CustomCategoryIDs (Issue #1203).
func TestGetTransactions_CustomCategoryIDsParsing(t *testing.T) {
	userID := "user-123"
	mockService := &mockTransactionService{
		listResponse: &services.TransactionListResponse{
			Transactions: []*domain.ActivityItem{},
			Limit:        50,
		},
	}

	handler := NewTransactionHandler(mockService)

	input := &GetTransactionsInput{
		AuthenticatedInput:    inputs.AuthenticatedInput{UserID: userID},
		CursorPaginationInput: inputs.CursorPaginationInput{Limit: 50},
		CustomCategoryIDs:     "cat-aaa,cat-bbb",
	}

	output, err := handler.GetTransactions(context.Background(), input)

	require.NoError(t, err)
	require.NotNil(t, output)
	assert.Equal(t, []string{"cat-aaa", "cat-bbb"}, mockService.capturedFilters.CustomCategoryIDs)
}

// TestGetTransactions_ReviewedFilterParsing verifies the reviewed query parameter is
// parsed into capturedFilters.Reviewed (Issue #1203).
func TestGetTransactions_ReviewedFilterParsing(t *testing.T) {
	userID := "user-123"
	mockService := &mockTransactionService{
		listResponse: &services.TransactionListResponse{
			Transactions: []*domain.ActivityItem{},
			Limit:        50,
		},
	}

	handler := NewTransactionHandler(mockService)

	input := &GetTransactionsInput{
		AuthenticatedInput:    inputs.AuthenticatedInput{UserID: userID},
		CursorPaginationInput: inputs.CursorPaginationInput{Limit: 50},
		Reviewed:              "false",
	}

	output, err := handler.GetTransactions(context.Background(), input)

	require.NoError(t, err)
	require.NotNil(t, output)
	require.NotNil(t, mockService.capturedFilters.Reviewed)
	assert.False(t, *mockService.capturedFilters.Reviewed)
}

// TestGetTransactions_InvalidReviewed tests a 400 for a non-boolean reviewed parameter.
func TestGetTransactions_InvalidReviewed(t *testing.T) {
	mockService := &mockTransactionService{}
	handler := NewTransactionHandler(mockService)

	input := &GetTransactionsInput{
		AuthenticatedInput:    inputs.AuthenticatedInput{UserID: "user-123"},
		CursorPaginationInput: inputs.CursorPaginationInput{Limit: 50},
		Reviewed:              "maybe",
	}

	output, err := handler.GetTransactions(context.Background(), input)

	assert.Nil(t, output)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "Invalid reviewed parameter")
}

// TestGetTransactions_IncludeReversedDefaultsOff pins #1569's default. A retained reversal that
// appears unbidden in a total the user has already reconciled is the same silent change the issue
// exists to stop, arriving from the other direction — so the flag has to be opt-in, and the default
// has to be pinned rather than assumed from a zero value that a later refactor could flip.
func TestGetTransactions_IncludeReversedDefaultsOff(t *testing.T) {
	mockService := &mockTransactionService{
		listResponse: &services.TransactionListResponse{
			Transactions: []*domain.ActivityItem{},
			Limit:        50,
		},
	}
	handler := NewTransactionHandler(mockService)

	_, err := handler.GetTransactions(context.Background(), &GetTransactionsInput{
		AuthenticatedInput:    inputs.AuthenticatedInput{UserID: "user-123"},
		CursorPaginationInput: inputs.CursorPaginationInput{Limit: 50},
	})

	require.NoError(t, err)
	assert.False(t, mockService.capturedFilters.IncludeReversed,
		"omitting include_reversed must not surface reversals")
}

// TestGetTransactions_IncludeReversedParsing verifies the opt-in actually reaches the filters.
func TestGetTransactions_IncludeReversedParsing(t *testing.T) {
	mockService := &mockTransactionService{
		listResponse: &services.TransactionListResponse{
			Transactions: []*domain.ActivityItem{},
			Limit:        50,
		},
	}
	handler := NewTransactionHandler(mockService)

	_, err := handler.GetTransactions(context.Background(), &GetTransactionsInput{
		AuthenticatedInput:    inputs.AuthenticatedInput{UserID: "user-123"},
		CursorPaginationInput: inputs.CursorPaginationInput{Limit: 50},
		IncludeReversed:       "true",
	})

	require.NoError(t, err)
	assert.True(t, mockService.capturedFilters.IncludeReversed)
}

// TestGetTransactions_InvalidIncludeReversed: a malformed value is REJECTED, not coerced to false.
// Coercing would hand the client a list it did not ask for while it believes it did — precisely the
// silent-divergence failure mode #1569 is about.
func TestGetTransactions_InvalidIncludeReversed(t *testing.T) {
	handler := NewTransactionHandler(&mockTransactionService{})

	output, err := handler.GetTransactions(context.Background(), &GetTransactionsInput{
		AuthenticatedInput:    inputs.AuthenticatedInput{UserID: "user-123"},
		CursorPaginationInput: inputs.CursorPaginationInput{Limit: 50},
		IncludeReversed:       "sometimes",
	})

	assert.Nil(t, output)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "Invalid include_reversed parameter")
}

// TestSetTransactionReviewed_Success verifies the single-transaction review handler
// forwards a one-element id slice and returns the updated count (Issue #1203).
func TestSetTransactionReviewed_Success(t *testing.T) {
	var gotIDs []string
	var gotReviewed bool
	mockService := &mockTransactionService{
		setReviewedFn: func(_ context.Context, _ string, ids []string, reviewed bool) (int64, error) {
			gotIDs = ids
			gotReviewed = reviewed
			return 1, nil
		},
	}

	handler := NewTransactionHandler(mockService)

	input := &SetTransactionReviewedInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-123"},
		ID:                 "tx-123",
	}
	input.Body.Reviewed = true

	output, err := handler.SetTransactionReviewed(context.Background(), input)

	require.NoError(t, err)
	require.NotNil(t, output)
	assert.Equal(t, int64(1), output.Body.Updated)
	assert.Equal(t, []string{"tx-123"}, gotIDs)
	assert.True(t, gotReviewed)
}

// TestBulkSetTransactionReviewed_Success verifies the bulk review handler forwards all
// ids and returns the updated count (Issue #1203).
func TestBulkSetTransactionReviewed_Success(t *testing.T) {
	mockService := &mockTransactionService{
		setReviewedFn: func(_ context.Context, _ string, ids []string, _ bool) (int64, error) {
			return int64(len(ids)), nil
		},
	}

	handler := NewTransactionHandler(mockService)

	input := &BulkSetTransactionReviewedInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-123"},
	}
	input.Body.TransactionIDs = []string{"tx-1", "tx-2", "tx-3"}
	input.Body.Reviewed = true

	output, err := handler.BulkSetTransactionReviewed(context.Background(), input)

	require.NoError(t, err)
	require.NotNil(t, output)
	assert.Equal(t, int64(3), output.Body.Updated)
}

// TestBulkSetTransactionReviewed_EmptyIDs tests a 400 when no transaction ids are given,
// and confirms the service is never called.
func TestBulkSetTransactionReviewed_EmptyIDs(t *testing.T) {
	called := false
	mockService := &mockTransactionService{
		setReviewedFn: func(_ context.Context, _ string, _ []string, _ bool) (int64, error) {
			called = true
			return 0, nil
		},
	}

	handler := NewTransactionHandler(mockService)

	input := &BulkSetTransactionReviewedInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-123"},
	}
	input.Body.TransactionIDs = []string{}
	input.Body.Reviewed = true

	output, err := handler.BulkSetTransactionReviewed(context.Background(), input)

	assert.Nil(t, output)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "transaction_ids must not be empty")
	assert.False(t, called, "service should not be called when ids are empty")
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

// #1369 — Phase 5 decision #5: members must never drill into another member's raw
// transactions — reject household scope on the transaction list endpoint with 400.
func TestGetTransactions_HouseholdScope_Returns400(t *testing.T) {
	userID := "user-123"
	mockService := &mockTransactionService{}
	handler := NewTransactionHandler(mockService)

	input := &GetTransactionsInput{
		AuthenticatedInput:    inputs.AuthenticatedInput{UserID: userID},
		CursorPaginationInput: inputs.CursorPaginationInput{Limit: 50},
		Scope:                 "household",
	}

	output, err := handler.GetTransactions(context.Background(), input)

	assert.Nil(t, output)
	require.Error(t, err)
	var statusErr huma.StatusError
	require.ErrorAs(t, err, &statusErr)
	assert.Equal(t, http.StatusBadRequest, statusErr.GetStatus())
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

// #1369 — Phase 5 decision #5: reject household scope on transaction detail with 400.
func TestGetTransactionByID_HouseholdScope_Returns400(t *testing.T) {
	mockService := &mockTransactionService{}
	handler := NewTransactionHandler(mockService)

	input := &GetTransactionByIDInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-123"},
		ID:                 "some-id",
		Scope:              "household",
	}

	output, err := handler.GetTransactionByID(context.Background(), input)

	assert.Nil(t, output)
	require.Error(t, err)
	var statusErr huma.StatusError
	require.ErrorAs(t, err, &statusErr)
	assert.Equal(t, http.StatusBadRequest, statusErr.GetStatus())
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

// TestUpdateTransactionCategory_CustomMissingID asserts a 400 (not a DB 500) when
// kind=custom is sent without a custom_category_id (#1203 discriminated-union validation).
func TestUpdateTransactionCategory_CustomMissingID(t *testing.T) {
	handler := NewTransactionHandler(&mockTransactionService{})
	input := &UpdateTransactionCategoryInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-123"},
		ID:                 "tx-123",
		Body:               UpdateTransactionCategoryBody{Kind: "custom"},
	}

	output, err := handler.UpdateTransactionCategory(context.Background(), input)

	assert.Nil(t, output)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "custom_category_id is required")
}

// TestUpdateTransactionCategory_PFCMissingPrimary asserts a 400 when kind=pfc is sent
// without a pfc_primary.
func TestUpdateTransactionCategory_PFCMissingPrimary(t *testing.T) {
	handler := NewTransactionHandler(&mockTransactionService{})
	input := &UpdateTransactionCategoryInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-123"},
		ID:                 "tx-123",
		Body:               UpdateTransactionCategoryBody{Kind: "pfc"},
	}

	output, err := handler.UpdateTransactionCategory(context.Background(), input)

	assert.Nil(t, output)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "pfc_primary is required")
}

// TestUpdateTransactionCategory_MismatchedFields asserts a 400 when fields don't match the
// kind (e.g. kind=pfc with a custom_category_id), instead of falling through to a DB 500.
func TestUpdateTransactionCategory_MismatchedFields(t *testing.T) {
	handler := NewTransactionHandler(&mockTransactionService{})
	pfcPrimary := "FOOD_AND_DRINK"
	customID := "cat-1"
	input := &UpdateTransactionCategoryInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-123"},
		ID:                 "tx-123",
		Body: UpdateTransactionCategoryBody{
			Kind:             "pfc",
			PFCPrimary:       &pfcPrimary,
			CustomCategoryID: &customID,
		},
	}

	output, err := handler.UpdateTransactionCategory(context.Background(), input)

	assert.Nil(t, output)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "custom_category_id must be omitted")
}

// TestUpdateTransactionCategory_ClearWithStrayField asserts a 400 (not a DB 500) when
// kind=clear is sent with a stray override field (#1203 discriminated-union validation).
func TestUpdateTransactionCategory_ClearWithStrayField(t *testing.T) {
	customID := "cat-1"
	pfcPrimary := "FOOD_AND_DRINK"
	pfcDetailed := "FOOD_AND_DRINK_RESTAURANTS"
	cases := []struct {
		name string
		body UpdateTransactionCategoryBody
	}{
		{"stray custom id", UpdateTransactionCategoryBody{Kind: "clear", CustomCategoryID: &customID}},
		{"stray pfc primary", UpdateTransactionCategoryBody{Kind: "clear", PFCPrimary: &pfcPrimary}},
		{"stray pfc detailed", UpdateTransactionCategoryBody{Kind: "clear", PFCDetailed: &pfcDetailed}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			handler := NewTransactionHandler(&mockTransactionService{})
			input := &UpdateTransactionCategoryInput{
				AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-123"},
				ID:                 "tx-123",
				Body:               tc.body,
			}

			output, err := handler.UpdateTransactionCategory(context.Background(), input)

			assert.Nil(t, output)
			require.Error(t, err)
			assert.Contains(t, err.Error(), "must be omitted when kind=clear")
			if se, ok := err.(huma.StatusError); ok {
				assert.Equal(t, http.StatusBadRequest, se.GetStatus())
			} else {
				t.Fatalf("expected a huma.StatusError, got %T", err)
			}
		})
	}
}

// TestUpdateTransactionCategory_ClearSucceeds asserts a clear with no override fields is accepted.
func TestUpdateTransactionCategory_ClearSucceeds(t *testing.T) {
	userID := "user-123"
	mockService := &mockTransactionService{transaction: createTestTransaction(userID)}
	handler := NewTransactionHandler(mockService)

	input := &UpdateTransactionCategoryInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: userID},
		ID:                 "tx-123",
		Body:               UpdateTransactionCategoryBody{Kind: "clear"},
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
			Transactions: []*domain.ActivityItem{},
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
			Transactions: []*domain.ActivityItem{domain.NewBankingActivityItem(tx)},
			TotalCount:   ptrTo(1),
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
			Transactions: []*domain.ActivityItem{},
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
			Transactions: []*domain.ActivityItem{domain.NewBankingActivityItem(tx)},
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
			Transactions: []*domain.ActivityItem{},
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

// TestGetTransactions_CursorVersionUnsupported verifies that a cursor with V=3 (unknown future
// version) yields a 400. V=2 is now a valid version (unified feed); we use V=3 to test rejection.
func TestGetTransactions_CursorVersionUnsupported(t *testing.T) {
	userID := "user-cursor-ver"

	// Manually encode a cursor with V=3 (future/unknown version) bypassing DecodeTransactionCursor
	v2Cursor := domain.TransactionCursor{
		V:  3,
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

// TestGetTransactions_CursorMissingRequiredFields verifies that a cursor with valid base64 and
// valid JSON but missing required fields (k, d, id) is rejected with a 400. The decoder
// validates K is one of {date,merchant,amount,category}; an empty K fails that check.
func TestGetTransactions_CursorMissingRequiredFields(t *testing.T) {
	userID := "user-cursor-missing"
	mockService := &mockTransactionService{}
	handler := NewTransactionHandler(mockService)

	// Encode {"v":1} — valid base64/JSON but K and D default to "" which fail enum validation.
	partialJSON, err := json.Marshal(map[string]interface{}{"v": 1})
	require.NoError(t, err)
	encodedCursor := base64.RawURLEncoding.EncodeToString(partialJSON)

	input := &GetTransactionsInput{
		AuthenticatedInput:    inputs.AuthenticatedInput{UserID: userID},
		CursorPaginationInput: inputs.CursorPaginationInput{Cursor: encodedCursor, Limit: 10},
		SortBy:                "date",
		SortDir:               "desc",
	}

	output, err := handler.GetTransactions(context.Background(), input)

	assert.Nil(t, output)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid cursor")
}

// TestGetTransactions_OldOffsetParamSilentlyIgnored verifies that a request including the
// legacy ?offset=10 query param is not rejected (Huma ignores unregistered query params).
// Expected: 401 (auth failure) — not 422 with "unexpected query parameter" about offset.
func TestGetTransactions_OldOffsetParamSilentlyIgnored(t *testing.T) {
	mockService := &mockTransactionService{
		listResponse: &services.TransactionListResponse{
			Transactions: []*domain.ActivityItem{},
			Limit:        50,
		},
	}
	_, api := humatest.New(t)
	handler := NewTransactionHandler(mockService)
	RegisterTransactionRoutes(api, handler)

	resp := api.Get("/api/v1/transactions?offset=10&limit=20")

	// Auth fails in test, so we get 401 — but NOT 422 from an "unknown param" rejection.
	assert.Equal(t, http.StatusUnauthorized, resp.Code)
	body := resp.Body.String()
	assert.NotContains(t, body, "unknown query parameter")
	assert.NotContains(t, body, "unexpected query parameter")
	assert.NotContains(t, body, "\"offset\"")
}

// TestGetTransactions_CategorySourceFilterParsing verifies that the category_source query
// parameter is parsed into the correct slice on capturedFilters.CategorySources (Issue #1210).
func TestGetTransactions_CategorySourceFilterParsing(t *testing.T) {
	userID := "user-123"
	mockService := &mockTransactionService{
		listResponse: &services.TransactionListResponse{
			Transactions: []*domain.ActivityItem{},
			Limit:        50,
		},
	}

	handler := NewTransactionHandler(mockService)

	input := &GetTransactionsInput{
		AuthenticatedInput:    inputs.AuthenticatedInput{UserID: userID},
		CursorPaginationInput: inputs.CursorPaginationInput{Limit: 50},
		CategorySources:       "plaid,rule",
	}

	output, err := handler.GetTransactions(context.Background(), input)

	require.NoError(t, err)
	require.NotNil(t, output)
	assert.Equal(t, []string{"plaid", "rule"}, mockService.capturedFilters.CategorySources)
}

// TestGetTransactions_CategorySourceSingleValue verifies a single category_source value parses correctly.
func TestGetTransactions_CategorySourceSingleValue(t *testing.T) {
	userID := "user-123"
	mockService := &mockTransactionService{
		listResponse: &services.TransactionListResponse{
			Transactions: []*domain.ActivityItem{},
			Limit:        50,
		},
	}

	handler := NewTransactionHandler(mockService)

	input := &GetTransactionsInput{
		AuthenticatedInput:    inputs.AuthenticatedInput{UserID: userID},
		CursorPaginationInput: inputs.CursorPaginationInput{Limit: 50},
		CategorySources:       "manual",
	}

	output, err := handler.GetTransactions(context.Background(), input)

	require.NoError(t, err)
	require.NotNil(t, output)
	assert.Equal(t, []string{"manual"}, mockService.capturedFilters.CategorySources)
}

// TestGetTransactions_InvalidCategorySource verifies that an unknown token returns 400 (Issue #1210).
func TestGetTransactions_InvalidCategorySource(t *testing.T) {
	mockService := &mockTransactionService{}
	handler := NewTransactionHandler(mockService)

	input := &GetTransactionsInput{
		AuthenticatedInput:    inputs.AuthenticatedInput{UserID: "user-123"},
		CursorPaginationInput: inputs.CursorPaginationInput{Limit: 50},
		CategorySources:       "plaid,bogus",
	}

	output, err := handler.GetTransactions(context.Background(), input)

	assert.Nil(t, output)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "Invalid category_source")
}

// TestGetTransactions_EmptyCategorySource verifies that an empty category_source is ignored (no filter set).
func TestGetTransactions_EmptyCategorySource(t *testing.T) {
	userID := "user-123"
	mockService := &mockTransactionService{
		listResponse: &services.TransactionListResponse{
			Transactions: []*domain.ActivityItem{},
			Limit:        50,
		},
	}

	handler := NewTransactionHandler(mockService)

	input := &GetTransactionsInput{
		AuthenticatedInput:    inputs.AuthenticatedInput{UserID: userID},
		CursorPaginationInput: inputs.CursorPaginationInput{Limit: 50},
		CategorySources:       "",
	}

	output, err := handler.GetTransactions(context.Background(), input)

	require.NoError(t, err)
	require.NotNil(t, output)
	assert.Empty(t, mockService.capturedFilters.CategorySources)
}

// ─────────────────────────────────────────────────────────────────────────────
// BulkUpdateTransactionCategory tests (Issue #1210)
// ─────────────────────────────────────────────────────────────────────────────

// TestBulkUpdateTransactionCategory_Success verifies the happy path: valid pfc kind returns
// {requested, updated} with the correct values.
func TestBulkUpdateTransactionCategory_Success(t *testing.T) {
	userID := "user-123"
	var gotIDs []string
	var gotKind string
	mockService := &mockTransactionService{
		bulkUpdateCategoryFn: func(_ context.Context, _ string, ids []string, kind string, _, _, _ *string) (int64, error) {
			gotIDs = ids
			gotKind = kind
			return int64(len(ids)), nil
		},
	}
	handler := NewTransactionHandler(mockService)

	pfcPrimary := "FOOD_AND_DRINK"
	input := &BulkUpdateTransactionCategoryInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: userID},
	}
	input.Body.TransactionIDs = []string{"tx-1", "tx-2"}
	input.Body.Kind = "pfc"
	input.Body.PFCPrimary = &pfcPrimary

	output, err := handler.BulkUpdateTransactionCategory(context.Background(), input)

	require.NoError(t, err)
	require.NotNil(t, output)
	assert.Equal(t, int64(2), output.Body.Requested)
	assert.Equal(t, int64(2), output.Body.Updated)
	assert.Equal(t, []string{"tx-1", "tx-2"}, gotIDs)
	assert.Equal(t, "pfc", gotKind)
}

// TestBulkUpdateTransactionCategory_DeduplicatesIDs verifies duplicate ids are de-duped in
// the requested count but only unique ids are forwarded to the service.
func TestBulkUpdateTransactionCategory_DeduplicatesIDs(t *testing.T) {
	var gotIDs []string
	mockService := &mockTransactionService{
		bulkUpdateCategoryFn: func(_ context.Context, _ string, ids []string, _ string, _, _, _ *string) (int64, error) {
			gotIDs = ids
			return int64(len(ids)), nil
		},
	}
	handler := NewTransactionHandler(mockService)

	pfcPrimary := "FOOD_AND_DRINK"
	input := &BulkUpdateTransactionCategoryInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-123"},
	}
	input.Body.TransactionIDs = []string{"tx-1", "tx-2", "tx-1"} // tx-1 duplicated
	input.Body.Kind = "pfc"
	input.Body.PFCPrimary = &pfcPrimary

	output, err := handler.BulkUpdateTransactionCategory(context.Background(), input)

	require.NoError(t, err)
	require.NotNil(t, output)
	assert.Equal(t, int64(2), output.Body.Requested, "requested should count unique ids")
	assert.Len(t, gotIDs, 2, "service should receive de-duplicated ids")
}

// TestBulkUpdateTransactionCategory_EmptyIDs tests a 400 when no transaction ids are given.
func TestBulkUpdateTransactionCategory_EmptyIDs(t *testing.T) {
	handler := NewTransactionHandler(&mockTransactionService{})
	input := &BulkUpdateTransactionCategoryInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-123"},
	}
	input.Body.TransactionIDs = []string{}
	input.Body.Kind = "pfc"

	output, err := handler.BulkUpdateTransactionCategory(context.Background(), input)

	assert.Nil(t, output)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "transaction_ids must not be empty")
}

// TestBulkUpdateTransactionCategory_OverLimit tests a 400 when more than 500 ids are given.
func TestBulkUpdateTransactionCategory_OverLimit(t *testing.T) {
	handler := NewTransactionHandler(&mockTransactionService{})
	ids := make([]string, 501)
	for i := range ids {
		ids[i] = fmt.Sprintf("tx-%d", i)
	}
	input := &BulkUpdateTransactionCategoryInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-123"},
	}
	input.Body.TransactionIDs = ids
	input.Body.Kind = "pfc"

	output, err := handler.BulkUpdateTransactionCategory(context.Background(), input)

	assert.Nil(t, output)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "500")
}

// TestBulkUpdateTransactionCategory_CustomMissingID validates kind=custom requires custom_category_id.
func TestBulkUpdateTransactionCategory_CustomMissingID(t *testing.T) {
	handler := NewTransactionHandler(&mockTransactionService{})
	input := &BulkUpdateTransactionCategoryInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-123"},
	}
	input.Body.TransactionIDs = []string{"tx-1"}
	input.Body.Kind = "custom"

	output, err := handler.BulkUpdateTransactionCategory(context.Background(), input)

	assert.Nil(t, output)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "custom_category_id is required")
}

// TestBulkUpdateTransactionCategory_PFCMissingPrimary validates kind=pfc requires pfc_primary.
func TestBulkUpdateTransactionCategory_PFCMissingPrimary(t *testing.T) {
	handler := NewTransactionHandler(&mockTransactionService{})
	input := &BulkUpdateTransactionCategoryInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-123"},
	}
	input.Body.TransactionIDs = []string{"tx-1"}
	input.Body.Kind = "pfc"

	output, err := handler.BulkUpdateTransactionCategory(context.Background(), input)

	assert.Nil(t, output)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "pfc_primary is required")
}

// TestBulkUpdateTransactionCategory_ClearSucceeds verifies kind=clear with no override fields is accepted.
func TestBulkUpdateTransactionCategory_ClearSucceeds(t *testing.T) {
	mockService := &mockTransactionService{
		bulkUpdateCategoryFn: func(_ context.Context, _ string, ids []string, _ string, _, _, _ *string) (int64, error) {
			return int64(len(ids)), nil
		},
	}
	handler := NewTransactionHandler(mockService)
	input := &BulkUpdateTransactionCategoryInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-123"},
	}
	input.Body.TransactionIDs = []string{"tx-1"}
	input.Body.Kind = "clear"

	output, err := handler.BulkUpdateTransactionCategory(context.Background(), input)

	require.NoError(t, err)
	require.NotNil(t, output)
}

// TestBulkUpdateTransactionCategory_MCPOriginWritesAudit verifies the MCP-origin path
// records exactly one audit entry (Issue #1210).
func TestBulkUpdateTransactionCategory_MCPOriginWritesAudit(t *testing.T) {
	const workerSecret = "test-bulk-cat-secret"
	auditRepo := &mockMCPAuditRepository{}
	pfcPrimary := "FOOD_AND_DRINK"
	mockService := &mockTransactionService{
		bulkUpdateCategoryFn: func(_ context.Context, _ string, ids []string, _ string, _, _, _ *string) (int64, error) {
			return int64(len(ids)), nil
		},
	}
	handler := NewTransactionHandler(mockService, TransactionHandlerDeps{
		MCPAuditRepo:    auditRepo,
		MCPWorkerSecret: workerSecret,
	})

	input := &BulkUpdateTransactionCategoryInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-mcp"},
		MCPContextInput:    inputs.MCPContextInput{MCPGrantID: "grant-bulk", MCPWorkerSecret: workerSecret},
	}
	input.Body.TransactionIDs = []string{"tx-1"}
	input.Body.Kind = "pfc"
	input.Body.PFCPrimary = &pfcPrimary

	_, err := handler.BulkUpdateTransactionCategory(context.Background(), input)

	require.NoError(t, err)
	require.NotNil(t, auditRepo.recordedEntry, "should record an MCP audit entry")
	assert.Equal(t, "bulkUpdateTransactionCategory", auditRepo.recordedEntry.OperationID)
	assert.Equal(t, "write", auditRepo.recordedEntry.OperationClass)
	assert.Equal(t, "grant-bulk", auditRepo.recordedEntry.GrantID)
}

// TestBulkUpdateTransactionCategory_NonMCPNoAudit verifies that a non-MCP-origin call does
// not write an MCP audit entry.
func TestBulkUpdateTransactionCategory_NonMCPNoAudit(t *testing.T) {
	const workerSecret = "test-bulk-cat-secret-2"
	auditRepo := &mockMCPAuditRepository{}
	pfcPrimary := "FOOD_AND_DRINK"
	mockService := &mockTransactionService{
		bulkUpdateCategoryFn: func(_ context.Context, _ string, ids []string, _ string, _, _, _ *string) (int64, error) {
			return int64(len(ids)), nil
		},
	}
	handler := NewTransactionHandler(mockService, TransactionHandlerDeps{
		MCPAuditRepo:    auditRepo,
		MCPWorkerSecret: workerSecret,
	})

	// No MCPContextInput / wrong secret
	input := &BulkUpdateTransactionCategoryInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-regular"},
	}
	input.Body.TransactionIDs = []string{"tx-1"}
	input.Body.Kind = "pfc"
	input.Body.PFCPrimary = &pfcPrimary

	_, err := handler.BulkUpdateTransactionCategory(context.Background(), input)

	require.NoError(t, err)
	assert.Nil(t, auditRepo.recordedEntry, "non-MCP call must not write audit entry")
}

// ─────────────────────────────────────────────────────────────────────────────
// GetTransactionReviewSummary tests (Issue #1210)
// ─────────────────────────────────────────────────────────────────────────────

// TestGetTransactionReviewSummary_Success verifies the happy path: the handler
// calls the service and returns the structured summary.
func TestGetTransactionReviewSummary_Success(t *testing.T) {
	userID := "user-rs-1"
	pfcKey := "FOOD_AND_DRINK"
	pfcDisplay := "Food and Drink"
	avgSec := 3600.0
	summary := &domain.ReviewSummaryResponse{
		UnreviewedCount:        5,
		ReviewedByRuleCount:    10,
		ReviewedByManualCount:  3,
		ReviewedByPlaidCount:   2,
		AvgIngestionToReviewSec: &avgSec,
		TopUnreviewedCategories: []domain.TopUnreviewedCategory{
			{CategoryKey: pfcKey, DisplayName: pfcDisplay, Count: 5},
		},
	}
	mockService := &mockTransactionService{
		reviewSummaryFn: func(_ context.Context, _ string, _ domain.ReviewSummaryFilters) (*domain.ReviewSummaryResponse, error) {
			return summary, nil
		},
	}
	handler := NewTransactionHandler(mockService)

	input := &GetTransactionReviewSummaryInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: userID},
	}

	output, err := handler.GetTransactionReviewSummary(context.Background(), input)

	require.NoError(t, err)
	require.NotNil(t, output)
	assert.Equal(t, int64(5), output.Body.UnreviewedCount)
	assert.Equal(t, int64(10), output.Body.ReviewedByRuleCount)
	assert.Equal(t, int64(3), output.Body.ReviewedByManualCount)
	assert.Equal(t, int64(2), output.Body.ReviewedByPlaidCount)
	require.NotNil(t, output.Body.AvgIngestionToReviewSec)
	assert.InDelta(t, 3600.0, *output.Body.AvgIngestionToReviewSec, 0.001)
	require.Len(t, output.Body.TopUnreviewedCategories, 1)
	assert.Equal(t, pfcKey, output.Body.TopUnreviewedCategories[0].CategoryKey)
}

// TestGetTransactionReviewSummary_ServiceError verifies that a service error maps to 500.
func TestGetTransactionReviewSummary_ServiceError(t *testing.T) {
	mockService := &mockTransactionService{
		reviewSummaryFn: func(_ context.Context, _ string, _ domain.ReviewSummaryFilters) (*domain.ReviewSummaryResponse, error) {
			return nil, errors.New("db error")
		},
	}
	handler := NewTransactionHandler(mockService)

	input := &GetTransactionReviewSummaryInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-err"},
	}

	output, err := handler.GetTransactionReviewSummary(context.Background(), input)

	assert.Nil(t, output)
	require.Error(t, err)
}

// TestGetTransactionReviewSummary_ZeroRows verifies the handler returns a valid empty-counts
// response when the service returns a zeroed summary (no transactions).
func TestGetTransactionReviewSummary_ZeroRows(t *testing.T) {
	mockService := &mockTransactionService{
		reviewSummaryFn: func(_ context.Context, _ string, _ domain.ReviewSummaryFilters) (*domain.ReviewSummaryResponse, error) {
			return &domain.ReviewSummaryResponse{
				TopUnreviewedCategories: []domain.TopUnreviewedCategory{},
			}, nil
		},
	}
	handler := NewTransactionHandler(mockService)

	input := &GetTransactionReviewSummaryInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-zero"},
	}

	output, err := handler.GetTransactionReviewSummary(context.Background(), input)

	require.NoError(t, err)
	require.NotNil(t, output)
	assert.Equal(t, int64(0), output.Body.UnreviewedCount)
	assert.Empty(t, output.Body.TopUnreviewedCategories)
}

// ─────────────────────────────────────────────────────────────────────────────
// MCP audit on review toggles (Issue #1210 Slice 5)
// ─────────────────────────────────────────────────────────────────────────────

// TestSetTransactionReviewed_MCPOriginWritesAudit verifies that SetTransactionReviewed
// records an MCP audit entry when called from an MCP origin.
func TestSetTransactionReviewed_MCPOriginWritesAudit(t *testing.T) {
	const workerSecret = "test-review-mcp-secret"
	auditRepo := &mockMCPAuditRepository{}
	mockService := &mockTransactionService{
		setReviewedFn: func(_ context.Context, _ string, _ []string, _ bool) (int64, error) {
			return 1, nil
		},
	}
	handler := NewTransactionHandler(mockService, TransactionHandlerDeps{
		MCPAuditRepo:    auditRepo,
		MCPWorkerSecret: workerSecret,
	})

	input := &SetTransactionReviewedInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-mcp-review"},
		MCPContextInput:    inputs.MCPContextInput{MCPGrantID: "grant-review", MCPWorkerSecret: workerSecret},
	}
	input.ID = "tx-1"
	input.Body.Reviewed = true

	_, err := handler.SetTransactionReviewed(context.Background(), input)

	require.NoError(t, err)
	require.NotNil(t, auditRepo.recordedEntry, "should record an MCP audit entry")
	assert.Equal(t, "setTransactionReviewed", auditRepo.recordedEntry.OperationID)
	assert.Equal(t, "write", auditRepo.recordedEntry.OperationClass)
}

// TestSetTransactionReviewed_NonMCPNoAudit verifies no audit is written for non-MCP calls.
func TestSetTransactionReviewed_NonMCPNoAudit(t *testing.T) {
	const workerSecret = "test-review-mcp-secret-2"
	auditRepo := &mockMCPAuditRepository{}
	handler := NewTransactionHandler(&mockTransactionService{}, TransactionHandlerDeps{
		MCPAuditRepo:    auditRepo,
		MCPWorkerSecret: workerSecret,
	})

	input := &SetTransactionReviewedInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-regular"},
	}
	input.ID = "tx-1"
	input.Body.Reviewed = true

	_, err := handler.SetTransactionReviewed(context.Background(), input)

	require.NoError(t, err)
	assert.Nil(t, auditRepo.recordedEntry, "non-MCP call must not write audit entry")
}

// TestBulkSetTransactionReviewed_MCPOriginWritesAudit verifies MCP audit for bulk review.
func TestBulkSetTransactionReviewed_MCPOriginWritesAudit(t *testing.T) {
	const workerSecret = "test-bulk-review-mcp-secret"
	auditRepo := &mockMCPAuditRepository{}
	mockService := &mockTransactionService{
		setReviewedFn: func(_ context.Context, _ string, _ []string, _ bool) (int64, error) {
			return 2, nil
		},
	}
	handler := NewTransactionHandler(mockService, TransactionHandlerDeps{
		MCPAuditRepo:    auditRepo,
		MCPWorkerSecret: workerSecret,
	})

	input := &BulkSetTransactionReviewedInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-mcp-bulk-review"},
		MCPContextInput:    inputs.MCPContextInput{MCPGrantID: "grant-bulk-review", MCPWorkerSecret: workerSecret},
	}
	input.Body.TransactionIDs = []string{"tx-1", "tx-2"}
	input.Body.Reviewed = true

	_, err := handler.BulkSetTransactionReviewed(context.Background(), input)

	require.NoError(t, err)
	require.NotNil(t, auditRepo.recordedEntry, "should record an MCP audit entry")
	assert.Equal(t, "bulkSetTransactionReviewed", auditRepo.recordedEntry.OperationID)
	assert.Equal(t, "write", auditRepo.recordedEntry.OperationClass)
}

// TestBulkSetTransactionReviewed_NonMCPNoAudit verifies no audit for non-MCP bulk review.
func TestBulkSetTransactionReviewed_NonMCPNoAudit(t *testing.T) {
	const workerSecret = "test-bulk-review-mcp-secret-2"
	auditRepo := &mockMCPAuditRepository{}
	handler := NewTransactionHandler(&mockTransactionService{}, TransactionHandlerDeps{
		MCPAuditRepo:    auditRepo,
		MCPWorkerSecret: workerSecret,
	})

	input := &BulkSetTransactionReviewedInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-regular"},
	}
	input.Body.TransactionIDs = []string{"tx-1"}
	input.Body.Reviewed = false

	_, err := handler.BulkSetTransactionReviewed(context.Background(), input)

	require.NoError(t, err)
	assert.Nil(t, auditRepo.recordedEntry, "non-MCP call must not write audit entry")
}

// ── R7-A: include_investment_activity default-true + graceful degradation ──────

// TestGetTransactions_IncludeInvestmentActivity_DefaultTrue_DateSort verifies that
// omitting include_investment_activity when sort_by=date (the default) results in the
// service being called with IncludeInvestmentActivity=true. This is the R7-A restoration
// of the plan's §6 Decision #5 (default="true").
// RED: currently fails because the handler leaves IncludeInvestmentActivity=false when
// the param is empty.
func TestGetTransactions_IncludeInvestmentActivity_DefaultTrue_DateSort(t *testing.T) {
	userID := "user-r7a-default"
	mockService := &mockTransactionService{
		listResponse: &services.TransactionListResponse{
			Transactions: []*domain.ActivityItem{},
			Limit:        50,
		},
	}
	handler := NewTransactionHandler(mockService)

	// Omit include_investment_activity entirely; sort_by defaults to "date".
	input := &GetTransactionsInput{
		AuthenticatedInput:    inputs.AuthenticatedInput{UserID: userID},
		CursorPaginationInput: inputs.CursorPaginationInput{Limit: 50},
		// IncludeInvestmentActivity: "" (omitted)
		// SortBy: "" (will resolve to "date" via handler default)
	}

	output, err := handler.GetTransactions(context.Background(), input)

	require.NoError(t, err, "omitting include_investment_activity must not error")
	require.NotNil(t, output)
	assert.True(t, mockService.capturedFilters.IncludeInvestmentActivity,
		"omitted include_investment_activity must default to true (R7-A)")
}

// TestGetTransactions_IncludeInvestmentActivity_DefaultTrue_NonDateSortDegrades verifies
// that omitting include_investment_activity with a non-date sort (e.g. sort_by=amount)
// silently degrades to false — so existing clients using non-date sorts are NOT broken
// by the default-true change. The request must succeed (no 400).
// RED: currently this returns false already (old default-false), but the test is
// written to lock the degrade semantics: it must be false AND it must not error.
func TestGetTransactions_IncludeInvestmentActivity_DefaultTrue_NonDateSortDegrades(t *testing.T) {
	userID := "user-r7a-degrade"
	mockService := &mockTransactionService{
		listResponse: &services.TransactionListResponse{
			Transactions: []*domain.ActivityItem{},
			Limit:        50,
		},
	}
	handler := NewTransactionHandler(mockService)

	input := &GetTransactionsInput{
		AuthenticatedInput:    inputs.AuthenticatedInput{UserID: userID},
		CursorPaginationInput: inputs.CursorPaginationInput{Limit: 50},
		SortBy:                "amount",
		// IncludeInvestmentActivity: "" (omitted) — must degrade to false, NOT 400
	}

	output, err := handler.GetTransactions(context.Background(), input)

	require.NoError(t, err, "omitted flag + non-date sort must NOT return 400 (graceful degradation)")
	require.NotNil(t, output)
	assert.False(t, mockService.capturedFilters.IncludeInvestmentActivity,
		"omitted flag + non-date sort must degrade to false (protect existing non-date-sort clients)")
}

// TestGetTransactions_IncludeInvestmentActivity_ExplicitTrue_NonDateSort400 verifies
// that explicitly setting include_investment_activity=true with a non-date sort
// returns 400 (you explicitly asked for an impossible combination).
func TestGetTransactions_IncludeInvestmentActivity_ExplicitTrue_NonDateSort400(t *testing.T) {
	userID := "user-r7a-explicit400"
	mockService := &mockTransactionService{
		// Mock service returns the sort-guard error (as the real service does)
		listErr: domain.ErrInvestmentMergeRequiresDateSort,
	}
	handler := NewTransactionHandler(mockService)

	input := &GetTransactionsInput{
		AuthenticatedInput:        inputs.AuthenticatedInput{UserID: userID},
		CursorPaginationInput:     inputs.CursorPaginationInput{Limit: 50},
		SortBy:                    "amount",
		IncludeInvestmentActivity: "true", // EXPLICIT true
	}

	output, err := handler.GetTransactions(context.Background(), input)

	assert.Nil(t, output)
	require.Error(t, err, "explicit include_investment_activity=true + non-date sort must error")

	var statusErr interface{ GetStatus() int }
	if errors.As(err, &statusErr) {
		assert.Equal(t, http.StatusBadRequest, statusErr.GetStatus(),
			"error must be 400 Bad Request")
	} else {
		t.Errorf("expected a status-bearing Huma error, got: %T %v", err, err)
	}
}

// TestGetTransactions_UncategorizedParsing pins all four states of the uncategorized param (#1626).
//
// The raw query string is the ONLY place absence can be told apart from an explicit false, so each
// state gets its own case. `false` must be a no-op rather than "exclude uncategorized" — the latter
// would be a second feature nobody asked for, and it would silently narrow every list that happened
// to serialise the flag.
func TestGetTransactions_UncategorizedParsing(t *testing.T) {
	newHandler := func() (*mockTransactionService, *TransactionHandler) {
		mockService := &mockTransactionService{
			listResponse: &services.TransactionListResponse{
				Transactions: []*domain.ActivityItem{},
				Limit:        50,
			},
		}
		return mockService, NewTransactionHandler(mockService)
	}

	baseInput := func(uncategorized string) *GetTransactionsInput {
		return &GetTransactionsInput{
			AuthenticatedInput:    inputs.AuthenticatedInput{UserID: "user-123"},
			CursorPaginationInput: inputs.CursorPaginationInput{Limit: 50},
			Uncategorized:         uncategorized,
		}
	}

	t.Run("true opts in", func(t *testing.T) {
		mockService, handler := newHandler()
		_, err := handler.GetTransactions(context.Background(), baseInput("true"))
		require.NoError(t, err)
		assert.True(t, mockService.capturedFilters.Uncategorized)
	})

	t.Run("absent adds no filter", func(t *testing.T) {
		mockService, handler := newHandler()
		_, err := handler.GetTransactions(context.Background(), baseInput(""))
		require.NoError(t, err)
		assert.False(t, mockService.capturedFilters.Uncategorized)
	})

	t.Run("explicit false is a no-op, not an exclusion", func(t *testing.T) {
		mockService, handler := newHandler()
		_, err := handler.GetTransactions(context.Background(), baseInput("false"))
		require.NoError(t, err)
		assert.False(t, mockService.capturedFilters.Uncategorized)
	})

	t.Run("malformed is rejected, never coerced", func(t *testing.T) {
		_, handler := newHandler()
		output, err := handler.GetTransactions(context.Background(), baseInput("maybe"))
		assert.Nil(t, output)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "Invalid uncategorized",
			"coercing a bad value to false would show the client a list it did not ask for while it "+
				"believed otherwise")
	})
}
