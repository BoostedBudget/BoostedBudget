package operations

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/danielgtaylor/huma/v2/humatest"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/huma/inputs"
	"github.com/maxnussbaum/BoostedBudget/internal/services"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ptrStr is a test helper to get a pointer to a string literal.
func ptrStr(s string) *string { return &s }

// mockBudgetService is a mock implementation of services.BudgetService
type mockBudgetService struct {
	budget             *domain.Budget
	budgets            []*domain.Budget
	category           *domain.BudgetCategory
	categories         []string
	createErr          error
	getErr             error
	getBudgetsErr      error
	getCurrentErr      error
	updateErr          error
	deleteErr          error
	addCategoryErr     error
	updateCategoryErr  error
	deleteCategoryErr  error
	getCategoriesErr   error
}

func (m *mockBudgetService) CreateBudget(ctx context.Context, userID string, req *domain.CreateBudgetRequest) (*domain.Budget, error) {
	if m.createErr != nil {
		return nil, m.createErr
	}
	return m.budget, nil
}

func (m *mockBudgetService) GetBudget(ctx context.Context, userID, budgetID string) (*domain.Budget, error) {
	if m.getErr != nil {
		return nil, m.getErr
	}
	return m.budget, nil
}

func (m *mockBudgetService) GetBudgets(ctx context.Context, userID string) ([]*domain.Budget, error) {
	if m.getBudgetsErr != nil {
		return nil, m.getBudgetsErr
	}
	return m.budgets, nil
}

func (m *mockBudgetService) GetActiveBudgets(ctx context.Context, userID string) ([]*domain.Budget, error) {
	return m.budgets, nil
}

func (m *mockBudgetService) GetCurrentBudget(ctx context.Context, userID string) (*domain.Budget, error) {
	if m.getCurrentErr != nil {
		return nil, m.getCurrentErr
	}
	return m.budget, nil
}

func (m *mockBudgetService) UpdateBudget(ctx context.Context, userID, budgetID string, req *domain.UpdateBudgetRequest) (*domain.Budget, error) {
	if m.updateErr != nil {
		return nil, m.updateErr
	}
	return m.budget, nil
}

func (m *mockBudgetService) DeleteBudget(ctx context.Context, userID, budgetID string) error {
	return m.deleteErr
}

func (m *mockBudgetService) AddCategory(ctx context.Context, userID, budgetID string, req *domain.CreateBudgetCategoryRequest) (*domain.BudgetCategory, error) {
	if m.addCategoryErr != nil {
		return nil, m.addCategoryErr
	}
	return m.category, nil
}

func (m *mockBudgetService) UpdateCategory(ctx context.Context, userID, budgetID, categoryID string, req *domain.UpdateBudgetCategoryRequest) (*domain.BudgetCategory, error) {
	if m.updateCategoryErr != nil {
		return nil, m.updateCategoryErr
	}
	return m.category, nil
}

func (m *mockBudgetService) DeleteCategory(ctx context.Context, userID, budgetID, categoryID string) error {
	return m.deleteCategoryErr
}

func (m *mockBudgetService) GetAvailableCategories(ctx context.Context, userID string) ([]string, error) {
	if m.getCategoriesErr != nil {
		return nil, m.getCategoriesErr
	}
	return m.categories, nil
}

// mockBudgetSyncService is a mock implementation of services.BudgetSyncService
type mockBudgetSyncService struct {
	recalculateErr error
}

func (m *mockBudgetSyncService) RecalculateBudgetAmounts(ctx context.Context, userID string) error {
	return nil
}

func (m *mockBudgetSyncService) RecalculateBudget(ctx context.Context, budget *domain.Budget) error {
	return m.recalculateErr
}

func (m *mockBudgetSyncService) SetEventProcessor(processor services.EventTriggerProcessor) {
	// No-op for mock
}

// mockTransactionRepoForBudget is a mock implementation of repository.TransactionRepository for budget tests
type mockTransactionRepoForBudget struct {
	transactions []domain.Transaction
	totalCount   int
	totalAmount  float64
	err          error
}

func (m *mockTransactionRepoForBudget) GetTransactionsForBudgetCategory(ctx context.Context, userID string, budgetCat *domain.BudgetCategory, startDate, endDate time.Time, limit, offset int) ([]domain.Transaction, int, float64, error) {
	if m.err != nil {
		return nil, 0, 0, m.err
	}
	return m.transactions, m.totalCount, m.totalAmount, nil
}

// Implement all required TransactionRepository methods as no-ops
func (m *mockTransactionRepoForBudget) Create(ctx context.Context, tx *domain.Transaction) error { return nil }
func (m *mockTransactionRepoForBudget) CreateBatch(ctx context.Context, txs []domain.Transaction) error { return nil }
func (m *mockTransactionRepoForBudget) Update(ctx context.Context, tx *domain.Transaction) error { return nil }
func (m *mockTransactionRepoForBudget) DeleteByExternalID(ctx context.Context, source domain.TransactionSource, externalID string, userID string) error { return nil }
func (m *mockTransactionRepoForBudget) GetByUserID(ctx context.Context, userID string, limit, offset int) ([]domain.Transaction, error) { return nil, nil }
func (m *mockTransactionRepoForBudget) GetByAccountID(ctx context.Context, accountID string) ([]domain.Transaction, error) { return nil, nil }
func (m *mockTransactionRepoForBudget) GetBySource(ctx context.Context, source domain.TransactionSource, userID string) ([]domain.Transaction, error) { return nil, nil }
func (m *mockTransactionRepoForBudget) GetByUserIDWithFilters(ctx context.Context, userID string, filters domain.TransactionFilters, limit, offset int) ([]domain.Transaction, error) { return nil, nil }
func (m *mockTransactionRepoForBudget) CountByUserID(ctx context.Context, userID string, filters domain.TransactionFilters) (int, error) { return 0, nil }
func (m *mockTransactionRepoForBudget) GetByExternalID(ctx context.Context, source domain.TransactionSource, externalID string, userID string) (*domain.Transaction, error) { return nil, nil }
func (m *mockTransactionRepoForBudget) GetByID(ctx context.Context, id string) (*domain.Transaction, error) { return nil, nil }
func (m *mockTransactionRepoForBudget) UpdateTags(ctx context.Context, id string, userID string, tags []string, tagsSource []domain.TagSource) error { return nil }
func (m *mockTransactionRepoForBudget) UpdateCategory(ctx context.Context, id string, userID string, overrideKind *string, pfcPrimaryOverride *string, pfcDetailedOverride *string, customCategoryOverrideID *string, categorySource domain.CategorySource) error { return nil }
func (m *mockTransactionRepoForBudget) GetDistinctPlaidCategories(ctx context.Context, userID string) ([]string, error) { return nil, nil }
func (m *mockTransactionRepoForBudget) GetIncomeSumByCategory(ctx context.Context, userID string, category string, startDate, endDate time.Time) (float64, error) { return 0, nil }
func (m *mockTransactionRepoForBudget) GetIncomeSumByTag(ctx context.Context, userID string, tag string, startDate, endDate time.Time) (float64, error) { return 0, nil }
func (m *mockTransactionRepoForBudget) GetSpentSumForBudgetCategory(ctx context.Context, userID string, budgetCat *domain.BudgetCategory, startDate, endDate time.Time) (float64, error) { return 0, nil }
func (m *mockTransactionRepoForBudget) GetMatchingTransactionStats(ctx context.Context, userID string, categoryKind string, pfcPrimary *string, pfcDetailed *string, customCategoryID *string, tags []string, matchLogic domain.MatchLogic, startDate, endDate time.Time) (int, float64, error) { return 0, 0, nil }

// GetByUserAccountID retrieves all transactions for a user_account.
// Issue #171 - Plaid Re-Link Handling
func (m *mockTransactionRepoForBudget) GetByUserAccountID(ctx context.Context, userID string, userAccountID string) ([]domain.Transaction, error) { return nil, nil }

// Helper function to create test budget
func createTestBudget() *domain.Budget {
	now := time.Now()
	endDate := now.AddDate(0, 1, 0)
	return &domain.Budget{
		ID:           "budget-123",
		UserID:       "user-123",
		Name:         "Monthly Budget",
		Period:       domain.BudgetPeriodMonthly,
		StartDate:    now,
		EndDate:      &endDate,
		Amount:       2000.00,
		CurrencyCode: "USD",
		IsActive:     true,
		CreatedAt:    now,
		UpdatedAt:    now,
		Categories: []domain.BudgetCategory{
			{
				ID:              "cat-123",
				BudgetID:        "budget-123",
				CategoryKind:    "pfc",
				PFCPrimary:      ptrStr("FOOD_AND_DRINK"),
				AllocatedAmount: 500.00,
				SpentAmount:     250.00,
			},
		},
	}
}

// TestCreateBudget_Success tests successful budget creation.
func TestCreateBudget_Success(t *testing.T) {
	mockService := &mockBudgetService{
		budget: createTestBudget(),
	}
	mockSyncService := &mockBudgetSyncService{}
	mockTxRepo := &mockTransactionRepoForBudget{}

	handler := NewBudgetHandler(mockService, mockSyncService, mockTxRepo)

	input := &CreateBudgetInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-123"},
	}
	input.Body = domain.CreateBudgetRequest{
		Name:         "Monthly Budget",
		Period:       domain.BudgetPeriodMonthly,
		StartDate:    time.Now(),
		Amount:       2000.00,
		CurrencyCode: "USD",
	}

	output, err := handler.CreateBudget(context.Background(), input)

	require.NoError(t, err)
	require.NotNil(t, output)
	assert.Equal(t, "budget-123", output.Body.ID)
	assert.Equal(t, "Monthly Budget", output.Body.Name)
}

// TestCreateBudget_Error tests error handling for budget creation.
func TestCreateBudget_Error(t *testing.T) {
	mockService := &mockBudgetService{
		createErr: domain.ErrBudgetAlreadyExists,
	}
	mockSyncService := &mockBudgetSyncService{}
	mockTxRepo := &mockTransactionRepoForBudget{}

	handler := NewBudgetHandler(mockService, mockSyncService, mockTxRepo)

	input := &CreateBudgetInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-123"},
	}
	input.Body = domain.CreateBudgetRequest{
		Name:   "Monthly Budget",
		Period: domain.BudgetPeriodMonthly,
		Amount: 2000.00,
	}

	output, err := handler.CreateBudget(context.Background(), input)

	assert.Nil(t, output)
	require.Error(t, err)
}

// TestGetBudgets_Success tests successful retrieval of budgets.
func TestGetBudgets_Success(t *testing.T) {
	mockService := &mockBudgetService{
		budgets: []*domain.Budget{createTestBudget()},
	}
	mockSyncService := &mockBudgetSyncService{}
	mockTxRepo := &mockTransactionRepoForBudget{}

	handler := NewBudgetHandler(mockService, mockSyncService, mockTxRepo)

	input := &GetBudgetsInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-123"},
	}

	output, err := handler.GetBudgets(context.Background(), input)

	require.NoError(t, err)
	require.NotNil(t, output)
	assert.Len(t, output.Body, 1)
}

// TestGetBudgets_Empty tests empty budget list returns empty array.
func TestGetBudgets_Empty(t *testing.T) {
	mockService := &mockBudgetService{
		budgets: nil,
	}
	mockSyncService := &mockBudgetSyncService{}
	mockTxRepo := &mockTransactionRepoForBudget{}

	handler := NewBudgetHandler(mockService, mockSyncService, mockTxRepo)

	input := &GetBudgetsInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-123"},
	}

	output, err := handler.GetBudgets(context.Background(), input)

	require.NoError(t, err)
	require.NotNil(t, output)
	assert.NotNil(t, output.Body)
	assert.Len(t, output.Body, 0)
}

// TestGetBudget_Success tests successful retrieval of a specific budget.
func TestGetBudget_Success(t *testing.T) {
	mockService := &mockBudgetService{
		budget: createTestBudget(),
	}
	mockSyncService := &mockBudgetSyncService{}
	mockTxRepo := &mockTransactionRepoForBudget{}

	handler := NewBudgetHandler(mockService, mockSyncService, mockTxRepo)

	input := &GetBudgetInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-123"},
		ID:                 "budget-123",
	}

	output, err := handler.GetBudget(context.Background(), input)

	require.NoError(t, err)
	require.NotNil(t, output)
	assert.Equal(t, "budget-123", output.Body.ID)
}

// TestGetBudget_NotFound tests error when budget is not found.
func TestGetBudget_NotFound(t *testing.T) {
	mockService := &mockBudgetService{
		getErr: domain.ErrBudgetNotFound,
	}
	mockSyncService := &mockBudgetSyncService{}
	mockTxRepo := &mockTransactionRepoForBudget{}

	handler := NewBudgetHandler(mockService, mockSyncService, mockTxRepo)

	input := &GetBudgetInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-123"},
		ID:                 "nonexistent",
	}

	output, err := handler.GetBudget(context.Background(), input)

	assert.Nil(t, output)
	require.Error(t, err)
}

// TestGetCurrentBudget_Success tests successful retrieval of current budget.
func TestGetCurrentBudget_Success(t *testing.T) {
	mockService := &mockBudgetService{
		budget: createTestBudget(),
	}
	mockSyncService := &mockBudgetSyncService{}
	mockTxRepo := &mockTransactionRepoForBudget{}

	handler := NewBudgetHandler(mockService, mockSyncService, mockTxRepo)

	input := &GetCurrentBudgetInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-123"},
	}

	output, err := handler.GetCurrentBudget(context.Background(), input)

	require.NoError(t, err)
	require.NotNil(t, output)
	assert.Equal(t, "budget-123", output.Body.ID)
}

// TestUpdateBudget_Success tests successful budget update.
func TestUpdateBudget_Success(t *testing.T) {
	mockService := &mockBudgetService{
		budget: createTestBudget(),
	}
	mockSyncService := &mockBudgetSyncService{}
	mockTxRepo := &mockTransactionRepoForBudget{}

	handler := NewBudgetHandler(mockService, mockSyncService, mockTxRepo)

	newName := "Updated Budget"
	input := &UpdateBudgetInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-123"},
		ID:                 "budget-123",
	}
	input.Body = domain.UpdateBudgetRequest{
		Name: &newName,
	}

	output, err := handler.UpdateBudget(context.Background(), input)

	require.NoError(t, err)
	require.NotNil(t, output)
}

// TestDeleteBudget_Success tests successful budget deletion.
func TestDeleteBudget_Success(t *testing.T) {
	mockService := &mockBudgetService{}
	mockSyncService := &mockBudgetSyncService{}
	mockTxRepo := &mockTransactionRepoForBudget{}

	handler := NewBudgetHandler(mockService, mockSyncService, mockTxRepo)

	input := &DeleteBudgetInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-123"},
		ID:                 "budget-123",
	}

	_, err := handler.DeleteBudget(context.Background(), input)

	require.NoError(t, err)
}

// TestAddCategory_Success tests successful category addition.
func TestAddCategory_Success(t *testing.T) {
	mockService := &mockBudgetService{
		category: &domain.BudgetCategory{
			ID:              "cat-new",
			BudgetID:        "budget-123",
			CategoryKind:    "pfc",
			PFCPrimary:      ptrStr("FOOD_AND_DRINK"),
			AllocatedAmount: 300.00,
		},
	}
	mockSyncService := &mockBudgetSyncService{}
	mockTxRepo := &mockTransactionRepoForBudget{}

	handler := NewBudgetHandler(mockService, mockSyncService, mockTxRepo)

	input := &AddCategoryInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-123"},
		ID:                 "budget-123",
	}
	input.Body = domain.CreateBudgetCategoryRequest{
		CategoryKind:    "pfc",
		PFCPrimary:      ptrStr("FOOD_AND_DRINK"),
		AllocatedAmount: 300.00,
	}

	output, err := handler.AddCategory(context.Background(), input)

	require.NoError(t, err)
	require.NotNil(t, output)
	assert.Equal(t, "cat-new", output.Body.ID)
}

// TestUpdateBudgetCategory_Success tests successful category update.
func TestUpdateBudgetCategory_Success(t *testing.T) {
	mockService := &mockBudgetService{
		category: &domain.BudgetCategory{
			ID:              "cat-123",
			BudgetID:        "budget-123",
			CategoryKind:    "pfc",
			PFCPrimary:      ptrStr("FOOD_AND_DRINK"),
			AllocatedAmount: 400.00,
		},
	}
	mockSyncService := &mockBudgetSyncService{}
	mockTxRepo := &mockTransactionRepoForBudget{}

	handler := NewBudgetHandler(mockService, mockSyncService, mockTxRepo)

	newAmount := 400.00
	input := &UpdateBudgetCategoryInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-123"},
		ID:                 "budget-123",
		CategoryID:         "cat-123",
	}
	input.Body = domain.UpdateBudgetCategoryRequest{
		AllocatedAmount: &newAmount,
	}

	output, err := handler.UpdateCategory(context.Background(), input)

	require.NoError(t, err)
	require.NotNil(t, output)
	assert.Equal(t, 400.00, output.Body.AllocatedAmount)
}

// TestDeleteBudgetCategory_Success tests successful category deletion.
func TestDeleteBudgetCategory_Success(t *testing.T) {
	mockService := &mockBudgetService{}
	mockSyncService := &mockBudgetSyncService{}
	mockTxRepo := &mockTransactionRepoForBudget{}

	handler := NewBudgetHandler(mockService, mockSyncService, mockTxRepo)

	input := &DeleteBudgetCategoryInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-123"},
		ID:                 "budget-123",
		CategoryID:         "cat-123",
	}

	_, err := handler.DeleteCategory(context.Background(), input)

	require.NoError(t, err)
}

// TestGetCategoryTransactions_EnrichesPFCDisplay tests that GetCategoryTransactions
// populates pfc_primary_display on response transactions (Gap 3 — #1080).
func TestGetCategoryTransactions_EnrichesPFCDisplay(t *testing.T) {
	pfcPrimary := "FOOD_AND_DRINK"
	mockTxRepo := &mockTransactionRepoForBudget{
		transactions: []domain.Transaction{
			{ID: "tx-1", UserID: "user-123", PFCPrimary: &pfcPrimary},
		},
		totalCount:  1,
		totalAmount: 42.00,
	}
	budget := createTestBudget()
	mockService := &mockBudgetService{budget: budget}

	handler := NewBudgetHandler(mockService, &mockBudgetSyncService{}, mockTxRepo)

	input := &GetCategoryTransactionsInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-123"},
		ID:                 "budget-123",
		CategoryID:         "cat-123",
		Limit:              20,
	}

	output, err := handler.GetCategoryTransactions(context.Background(), input)

	require.NoError(t, err)
	require.NotNil(t, output)
	require.Len(t, output.Body.Transactions, 1)

	tx := output.Body.Transactions[0]
	require.NotNil(t, tx.PFCPrimaryDisplay, "PFCPrimaryDisplay must be populated by handler")
	assert.Equal(t, "Food And Drink", *tx.PFCPrimaryDisplay)
}

// TestGetAvailableCategories_Success tests successful retrieval of available categories.
func TestGetAvailableCategories_Success(t *testing.T) {
	mockService := &mockBudgetService{
		categories: []string{"Food", "Transportation", "Entertainment"},
	}
	mockSyncService := &mockBudgetSyncService{}
	mockTxRepo := &mockTransactionRepoForBudget{}

	handler := NewBudgetHandler(mockService, mockSyncService, mockTxRepo)

	input := &GetAvailableCategoriesInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-123"},
	}

	output, err := handler.GetAvailableCategories(context.Background(), input)

	require.NoError(t, err)
	require.NotNil(t, output)
	assert.Len(t, output.Body, 3)
}

// TestSyncBudget_Success tests successful budget sync.
func TestSyncBudget_Success(t *testing.T) {
	mockService := &mockBudgetService{
		budget: createTestBudget(),
	}
	mockSyncService := &mockBudgetSyncService{}
	mockTxRepo := &mockTransactionRepoForBudget{}

	handler := NewBudgetHandler(mockService, mockSyncService, mockTxRepo)

	input := &SyncBudgetInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-123"},
		ID:                 "budget-123",
	}

	output, err := handler.SyncBudget(context.Background(), input)

	require.NoError(t, err)
	require.NotNil(t, output)
	assert.Equal(t, "budget-123", output.Body.ID)
}

// TestSyncBudget_SyncError tests error handling when sync fails.
func TestSyncBudget_SyncError(t *testing.T) {
	mockService := &mockBudgetService{
		budget: createTestBudget(),
	}
	mockSyncService := &mockBudgetSyncService{
		recalculateErr: errors.New("sync failed"),
	}
	mockTxRepo := &mockTransactionRepoForBudget{}

	handler := NewBudgetHandler(mockService, mockSyncService, mockTxRepo)

	input := &SyncBudgetInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-123"},
		ID:                 "budget-123",
	}

	output, err := handler.SyncBudget(context.Background(), input)

	assert.Nil(t, output)
	require.Error(t, err)
}

// TestGetCategoryTransactions_Success tests successful retrieval of category transactions.
func TestGetCategoryTransactions_Success(t *testing.T) {
	budget := createTestBudget()
	mockService := &mockBudgetService{
		budget: budget,
	}
	mockSyncService := &mockBudgetSyncService{}
	mockTxRepo := &mockTransactionRepoForBudget{
		transactions: []domain.Transaction{
			{ID: "tx-1", Amount: 50.00},
			{ID: "tx-2", Amount: 30.00},
		},
		totalCount:  2,
		totalAmount: 80.00,
	}

	handler := NewBudgetHandler(mockService, mockSyncService, mockTxRepo)

	input := &GetCategoryTransactionsInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-123"},
		ID:                 "budget-123",
		CategoryID:         "cat-123",
		Limit:              20,
		Offset:             0,
	}

	output, err := handler.GetCategoryTransactions(context.Background(), input)

	require.NoError(t, err)
	require.NotNil(t, output)
	assert.Len(t, output.Body.Transactions, 2)
	assert.Equal(t, 2, output.Body.TotalCount)
	assert.Equal(t, 80.00, output.Body.TotalAmount)
}

// TestGetCategoryTransactions_CategoryNotFound tests error when category is not found.
func TestGetCategoryTransactions_CategoryNotFound(t *testing.T) {
	budget := createTestBudget()
	mockService := &mockBudgetService{
		budget: budget,
	}
	mockSyncService := &mockBudgetSyncService{}
	mockTxRepo := &mockTransactionRepoForBudget{}

	handler := NewBudgetHandler(mockService, mockSyncService, mockTxRepo)

	input := &GetCategoryTransactionsInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-123"},
		ID:                 "budget-123",
		CategoryID:         "nonexistent-cat",
		Limit:              20,
		Offset:             0,
	}

	output, err := handler.GetCategoryTransactions(context.Background(), input)

	assert.Nil(t, output)
	require.Error(t, err)
}

// TestBudgetRouteRegistration tests that routes are properly registered.
func TestBudgetRouteRegistration(t *testing.T) {
	mockService := &mockBudgetService{
		budget:  createTestBudget(),
		budgets: []*domain.Budget{createTestBudget()},
	}
	mockSyncService := &mockBudgetSyncService{}
	mockTxRepo := &mockTransactionRepoForBudget{}

	_, api := humatest.New(t)
	handler := NewBudgetHandler(mockService, mockSyncService, mockTxRepo)
	RegisterBudgetRoutes(api, handler)

	// Test GET /api/v1/budgets (without auth, should fail with 401)
	resp := api.Get("/api/v1/budgets")
	assert.Equal(t, http.StatusUnauthorized, resp.Code)

	// Verify the response contains expected error message
	body := resp.Body.String()
	assert.Contains(t, body, "User not authenticated")
}

// TestCreateBudget_WithExpectedIncome tests that expected_income flows through to the service
// and is reflected in the response (Issue #1048).
func TestCreateBudget_WithExpectedIncome(t *testing.T) {
	expectedIncome := 5000.00
	budget := createTestBudget()
	budget.ExpectedIncome = expectedIncome

	mockService := &mockBudgetService{budget: budget}
	mockSyncService := &mockBudgetSyncService{}
	mockTxRepo := &mockTransactionRepoForBudget{}

	handler := NewBudgetHandler(mockService, mockSyncService, mockTxRepo)

	input := &CreateBudgetInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-123"},
	}
	input.Body = domain.CreateBudgetRequest{
		Name:           "Monthly Budget",
		Period:         domain.BudgetPeriodMonthly,
		StartDate:      time.Now(),
		Amount:         5000.00,
		CurrencyCode:   "USD",
		ExpectedIncome: &expectedIncome,
	}

	output, err := handler.CreateBudget(context.Background(), input)

	require.NoError(t, err)
	require.NotNil(t, output)
	assert.Equal(t, expectedIncome, output.Body.ExpectedIncome, "response should include expected_income")
}

// TestCreateBudget_BackCompatAmountOnly tests backward-compatibility: providing only amount
// (no expected_income) must still succeed (Issue #1048).
func TestCreateBudget_BackCompatAmountOnly(t *testing.T) {
	mockService := &mockBudgetService{budget: createTestBudget()}
	mockSyncService := &mockBudgetSyncService{}
	mockTxRepo := &mockTransactionRepoForBudget{}

	handler := NewBudgetHandler(mockService, mockSyncService, mockTxRepo)

	input := &CreateBudgetInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-123"},
	}
	input.Body = domain.CreateBudgetRequest{
		Name:         "Monthly Budget",
		Period:       domain.BudgetPeriodMonthly,
		StartDate:    time.Now(),
		Amount:       3000.00,
		CurrencyCode: "USD",
		// ExpectedIncome intentionally omitted — back-compat path
	}

	output, err := handler.CreateBudget(context.Background(), input)

	require.NoError(t, err, "amount-only request must succeed for backward compatibility")
	require.NotNil(t, output)
	assert.Equal(t, "budget-123", output.Body.ID)
	assert.Nil(t, input.Body.ExpectedIncome, "expected_income should be nil when not provided")
}

// TestCreateBudget_BothFieldsProvided_ExpectedIncomeWins asserts that when both amount
// and expected_income are provided, the handler accepts the request and the mock service
// returns the pre-set budget (service-layer reconciliation tests live in budget_service_test.go).
// This test is a handler-contract test: both fields in → no error, response well-formed (R19a).
func TestCreateBudget_BothFieldsProvided_ExpectedIncomeWins(t *testing.T) {
	expectedIncome := 500.00
	budget := createTestBudget()
	budget.Amount = 500.00
	budget.ExpectedIncome = 500.00 // mock returns reconciled values

	mockService := &mockBudgetService{budget: budget}
	mockSyncService := &mockBudgetSyncService{}
	mockTxRepo := &mockTransactionRepoForBudget{}

	handler := NewBudgetHandler(mockService, mockSyncService, mockTxRepo)

	input := &CreateBudgetInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-123"},
	}
	input.Body = domain.CreateBudgetRequest{
		Name:           "Monthly Budget",
		Period:         domain.BudgetPeriodMonthly,
		StartDate:      time.Now(),
		Amount:         100.00,          // lower value
		CurrencyCode:   "USD",
		ExpectedIncome: &expectedIncome, // higher value — should win
	}

	output, err := handler.CreateBudget(context.Background(), input)

	require.NoError(t, err, "both fields provided must be accepted")
	require.NotNil(t, output)
	assert.Equal(t, 500.00, output.Body.ExpectedIncome, "response should carry expected_income")
	assert.Equal(t, 500.00, output.Body.Amount, "amount should mirror expected_income")
}

// TestUpdateBudget_WithExpectedIncome tests that expected_income can be updated via PUT
// and is reflected in the response (Issue #1048).
func TestUpdateBudget_WithExpectedIncome(t *testing.T) {
	expectedIncome := 7000.00
	budget := createTestBudget()
	budget.ExpectedIncome = expectedIncome

	mockService := &mockBudgetService{budget: budget}
	mockSyncService := &mockBudgetSyncService{}
	mockTxRepo := &mockTransactionRepoForBudget{}

	handler := NewBudgetHandler(mockService, mockSyncService, mockTxRepo)

	input := &UpdateBudgetInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-123"},
		ID:                 "budget-123",
	}
	input.Body = domain.UpdateBudgetRequest{
		ExpectedIncome: &expectedIncome,
	}

	output, err := handler.UpdateBudget(context.Background(), input)

	require.NoError(t, err)
	require.NotNil(t, output)
	assert.Equal(t, expectedIncome, output.Body.ExpectedIncome, "response should include updated expected_income")
}

// TestBudgetResponseJSON tests JSON serialization of response.
func TestBudgetResponseJSON(t *testing.T) {
	mockService := &mockBudgetService{}
	mockSyncService := &mockBudgetSyncService{}
	mockTxRepo := &mockTransactionRepoForBudget{}

	_, api := humatest.New(t)
	handler := NewBudgetHandler(mockService, mockSyncService, mockTxRepo)
	RegisterBudgetRoutes(api, handler)

	resp := api.Get("/api/v1/budgets")
	assert.Equal(t, http.StatusUnauthorized, resp.Code)

	// Verify error response is valid JSON
	var errorResp map[string]interface{}
	err := json.Unmarshal(resp.Body.Bytes(), &errorResp)
	require.NoError(t, err, "Error response should be valid JSON")
}
