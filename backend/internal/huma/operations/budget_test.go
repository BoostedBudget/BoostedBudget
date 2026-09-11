package operations

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/humatest"
	"github.com/jackc/pgx/v5"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/huma/inputs"
	"github.com/maxnussbaum/BoostedBudget/internal/repository"
	"github.com/maxnussbaum/BoostedBudget/internal/services"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ptrStr is a test helper to get a pointer to a string literal.
func ptrStr(s string) *string { return &s }

// mockBudgetService is a mock implementation of services.BudgetService
type mockBudgetService struct {
	budget            *domain.Budget
	budgets           []*domain.Budget
	category          *domain.BudgetCategory
	categories        []string
	createErr         error
	getErr            error
	getBudgetsErr     error
	getCurrentErr     error
	updateErr         error
	deleteErr         error
	addCategoryErr    error
	updateCategoryErr error
	deleteCategoryErr error
	getCategoriesErr  error
	createCallCount   int // tracks CreateBudget invocations (R9-7)
}

func (m *mockBudgetService) CreateBudget(ctx context.Context, userID string, req *domain.CreateBudgetRequest) (*domain.Budget, error) {
	m.createCallCount++
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

func (m *mockBudgetService) SetGoalRepo(_ services.ActiveGoalRepo) {}

func (m *mockBudgetService) SetGoalOwnershipChecker(_ services.GoalOwnershipChecker) {}

func (m *mockBudgetService) SetAuditLogRepository(_ repository.AuditLogRepository) {}

func (m *mockBudgetService) BackfillManagedCategories(_ context.Context, _ string) error {
	return nil
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

func (m *mockBudgetSyncService) SetSavingsSpentCalc(calc services.SavingsContributionSpentCalc) {
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
func (m *mockTransactionRepoForBudget) CreateBatch(ctx context.Context, txs []domain.Transaction) (map[string]bool, error) {
	return nil, nil
}
func (m *mockTransactionRepoForBudget) Update(ctx context.Context, tx *domain.Transaction) error {
	return nil
}
func (m *mockTransactionRepoForBudget) PullForwardSettledPendingAnnotations(ctx context.Context, userID string, transactionIDs []string) (int64, error) {
	return 0, nil
}

func (m *mockTransactionRepoForBudget) TombstoneByExternalID(ctx context.Context, source domain.TransactionSource, externalID string, userID string, deletedBy string) (domain.TransactionDeletedReason, error) {
	return domain.TxnDeletedProviderRemovedPosted, nil
}
func (m *mockTransactionRepoForBudget) GetByUserID(ctx context.Context, userID string, limit, offset int) ([]domain.Transaction, error) {
	return nil, nil
}
func (m *mockTransactionRepoForBudget) GetByAccountID(ctx context.Context, accountID string) ([]domain.Transaction, error) {
	return nil, nil
}
func (m *mockTransactionRepoForBudget) GetBySource(ctx context.Context, source domain.TransactionSource, userID string) ([]domain.Transaction, error) {
	return nil, nil
}
func (m *mockTransactionRepoForBudget) GetByUserIDWithFilters(ctx context.Context, userID string, filters domain.TransactionFilters, limit, offset int) ([]domain.Transaction, error) {
	return nil, nil
}
func (m *mockTransactionRepoForBudget) CountByUserID(ctx context.Context, userID string, filters domain.TransactionFilters) (int, error) {
	return 0, nil
}
func (m *mockTransactionRepoForBudget) GetByExternalID(ctx context.Context, source domain.TransactionSource, externalID string, userID string) (*domain.Transaction, error) {
	return nil, nil
}
func (m *mockTransactionRepoForBudget) GetByID(ctx context.Context, id string) (*domain.Transaction, error) {
	return nil, nil
}
func (m *mockTransactionRepoForBudget) UpdateTags(ctx context.Context, id string, userID string, tags []string, tagsSource []domain.TagSource) error {
	return nil
}
func (m *mockTransactionRepoForBudget) UpdateCategory(ctx context.Context, id string, userID string, overrideKind *string, pfcPrimaryOverride *string, pfcDetailedOverride *string, customCategoryOverrideID *string, categorySource domain.CategorySource) error {
	return nil
}
func (m *mockTransactionRepoForBudget) SetReviewed(_ context.Context, _ string, _ []string, _ bool) (int64, error) {
	return 0, nil
}
func (m *mockTransactionRepoForBudget) BulkUpdateCategory(_ context.Context, _ string, _ []string, _ *string, _ *string, _ *string, _ *string, _ domain.CategorySource) (int64, error) {
	return 0, nil
}
func (m *mockTransactionRepoForBudget) GetReviewSummary(_ context.Context, _ string, _ domain.ReviewSummaryFilters) (*domain.ReviewSummaryResponse, error) {
	return &domain.ReviewSummaryResponse{TopUnreviewedCategories: []domain.TopUnreviewedCategory{}}, nil
}
func (m *mockTransactionRepoForBudget) GetDistinctPlaidCategories(ctx context.Context, userID string) ([]string, error) {
	return nil, nil
}
func (m *mockTransactionRepoForBudget) GetIncomeSumByCategory(ctx context.Context, userID string, category string, startDate, endDate time.Time) (float64, error) {
	return 0, nil
}
func (m *mockTransactionRepoForBudget) GetIncomeSumByTag(ctx context.Context, userID string, tag string, startDate, endDate time.Time) (float64, error) {
	return 0, nil
}
func (m *mockTransactionRepoForBudget) GetSpentSumForBudgetCategory(ctx context.Context, userID string, budgetCat *domain.BudgetCategory, startDate, endDate time.Time) (float64, error) {
	return 0, nil
}
func (m *mockTransactionRepoForBudget) GetMatchingTransactionStats(ctx context.Context, userID string, categoryKind string, pfcPrimary *string, pfcDetailed *string, customCategoryID *string, tags []string, matchLogic domain.MatchLogic, startDate, endDate time.Time) (int, float64, error) {
	return 0, 0, nil
}

// GetByUserAccountID retrieves all transactions for a user_account.
// Issue #171 - Plaid Re-Link Handling
func (m *mockTransactionRepoForBudget) GetByUserAccountID(ctx context.Context, userID string, userAccountID string) ([]domain.Transaction, error) {
	return nil, nil
}
func (m *mockTransactionRepoForBudget) UpdateEffectiveCategoryNameByCustomCategoryID(ctx context.Context, userID string, customCategoryID string) (int64, error) {
	return 0, nil
}
func (m *mockTransactionRepoForBudget) GetByUserIDKeyset(ctx context.Context, userID string, filters domain.TransactionFilters, cursor *domain.TransactionCursor, limit int) ([]domain.Transaction, *domain.TransactionCursor, *domain.FeedTotals, error) {
	return nil, nil, nil, nil
}

func (m *mockTransactionRepoForBudget) GetByUserIDKeysetUnified(ctx context.Context, userID string, filters domain.TransactionFilters, cursor *domain.TransactionCursor, limit int) ([]*domain.ActivityItem, *domain.TransactionCursor, *domain.FeedTotals, error) {
	return nil, nil, nil, nil
}

func (m *mockTransactionRepoForBudget) GetRecentInflowsByUserID(ctx context.Context, userID string, since time.Time) ([]*domain.Transaction, error) {
	return nil, nil
}

func (m *mockTransactionRepoForBudget) GetInflowTransactionsForAccount(ctx context.Context, userID, accountID string, start, end time.Time) ([]*domain.SavingsInflowTransaction, error) {
	return nil, nil
}

func (m *mockTransactionRepoForBudget) ResolveEffectiveMerchantNames(_ context.Context, _ string, _ []string) (map[string]string, error) {
	return map[string]string{}, nil
}

// mockCustomCategoryRepoForBudget is a minimal no-op mock of repository.CustomCategoryRepository
// used by budget handler tests that construct a real TransactionService.
type mockCustomCategoryRepoForBudget struct{}

func (m *mockCustomCategoryRepoForBudget) Create(ctx context.Context, category *domain.CustomCategory) error {
	return nil
}
func (m *mockCustomCategoryRepoForBudget) GetByID(ctx context.Context, id string) (*domain.CustomCategory, error) {
	return nil, nil
}
func (m *mockCustomCategoryRepoForBudget) GetByUserID(ctx context.Context, userID string) ([]*domain.CustomCategory, error) {
	return nil, nil
}
func (m *mockCustomCategoryRepoForBudget) GetByName(ctx context.Context, userID, name string) (*domain.CustomCategory, error) {
	return nil, nil
}
func (m *mockCustomCategoryRepoForBudget) Update(ctx context.Context, category *domain.CustomCategory) error {
	return nil
}
func (m *mockCustomCategoryRepoForBudget) CountByUserID(ctx context.Context, userID string) (int, error) {
	return 0, nil
}
func (m *mockCustomCategoryRepoForBudget) GetNamesByIDs(ctx context.Context, userID string, ids []string) (map[string]string, error) {
	return map[string]string{}, nil
}
func (m *mockCustomCategoryRepoForBudget) GetLiveNamesByIDs(ctx context.Context, userID string, ids []string) (map[string]string, error) {
	return map[string]string{}, nil
}
func (m *mockCustomCategoryRepoForBudget) GetChildren(ctx context.Context, parentID string) ([]*domain.CustomCategory, error) {
	return nil, nil
}
func (m *mockCustomCategoryRepoForBudget) HasChildren(ctx context.Context, id string) (bool, error) {
	return false, nil
}
func (m *mockCustomCategoryRepoForBudget) GetCategoryUsage(ctx context.Context, userID, categoryID string) (*domain.CategoryUsage, error) {
	return &domain.CategoryUsage{}, nil
}
func (m *mockCustomCategoryRepoForBudget) DeleteWithReassignment(ctx context.Context, userID, categoryID string, auditLog repository.AuditLogRepository) error {
	return nil
}

// Helper function to create test budget
func createTestBudget() *domain.Budget {
	now := time.Now()
	endDate := now.AddDate(0, 1, 0)
	b := &domain.Budget{
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
	b.PopulateDateDisplay()
	return b
}

// TestCreateBudget_Success tests successful budget creation.
func TestCreateBudget_Success(t *testing.T) {
	mockService := &mockBudgetService{
		budget: createTestBudget(),
	}
	mockSyncService := &mockBudgetSyncService{}
	mockTxRepo := &mockTransactionRepoForBudget{}

	handler := NewBudgetHandler(mockService, mockSyncService, services.NewTransactionService(mockTxRepo, &mockCustomCategoryRepoForBudget{}))

	input := &CreateBudgetInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-123"},
	}
	input.Body = domain.CreateBudgetRequest{
		Name:         "Monthly Budget",
		Period:       domain.BudgetPeriodMonthly,
		StartDate:    domain.FlexibleDate(time.Now()),
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

	handler := NewBudgetHandler(mockService, mockSyncService, services.NewTransactionService(mockTxRepo, &mockCustomCategoryRepoForBudget{}))

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

	handler := NewBudgetHandler(mockService, mockSyncService, services.NewTransactionService(mockTxRepo, &mockCustomCategoryRepoForBudget{}))

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

	handler := NewBudgetHandler(mockService, mockSyncService, services.NewTransactionService(mockTxRepo, &mockCustomCategoryRepoForBudget{}))

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

	handler := NewBudgetHandler(mockService, mockSyncService, services.NewTransactionService(mockTxRepo, &mockCustomCategoryRepoForBudget{}))

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

	handler := NewBudgetHandler(mockService, mockSyncService, services.NewTransactionService(mockTxRepo, &mockCustomCategoryRepoForBudget{}))

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

	handler := NewBudgetHandler(mockService, mockSyncService, services.NewTransactionService(mockTxRepo, &mockCustomCategoryRepoForBudget{}))

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

	handler := NewBudgetHandler(mockService, mockSyncService, services.NewTransactionService(mockTxRepo, &mockCustomCategoryRepoForBudget{}))

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

	handler := NewBudgetHandler(mockService, mockSyncService, services.NewTransactionService(mockTxRepo, &mockCustomCategoryRepoForBudget{}))

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

	handler := NewBudgetHandler(mockService, mockSyncService, services.NewTransactionService(mockTxRepo, &mockCustomCategoryRepoForBudget{}))

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

	handler := NewBudgetHandler(mockService, mockSyncService, services.NewTransactionService(mockTxRepo, &mockCustomCategoryRepoForBudget{}))

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

	handler := NewBudgetHandler(mockService, mockSyncService, services.NewTransactionService(mockTxRepo, &mockCustomCategoryRepoForBudget{}))

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

	handler := NewBudgetHandler(mockService, &mockBudgetSyncService{}, services.NewTransactionService(mockTxRepo, &mockCustomCategoryRepoForBudget{}))

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

// TestGetCategoryTransactions_WrapsServiceError tests that a service/repo error
// is mapped to a 500 response by the handler.
func TestGetCategoryTransactions_WrapsServiceError(t *testing.T) {
	budget := createTestBudget()
	mockService := &mockBudgetService{budget: budget}
	mockTxRepo := &mockTransactionRepoForBudget{
		err: errors.New("db failure"),
	}
	txService := services.NewTransactionService(mockTxRepo, &mockCustomCategoryRepoForBudget{})

	handler := NewBudgetHandler(mockService, &mockBudgetSyncService{}, txService)

	input := &GetCategoryTransactionsInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-123"},
		ID:                 "budget-123",
		CategoryID:         "cat-123",
		Limit:              20,
		Offset:             0,
	}

	output, err := handler.GetCategoryTransactions(context.Background(), input)

	assert.Nil(t, output)
	require.Error(t, err)

	var humaErr huma.StatusError
	require.ErrorAs(t, err, &humaErr)
	assert.Equal(t, http.StatusInternalServerError, humaErr.GetStatus())
}

// TestGetAvailableCategories_Success tests successful retrieval of available categories.
func TestGetAvailableCategories_Success(t *testing.T) {
	mockService := &mockBudgetService{
		categories: []string{"Food", "Transportation", "Entertainment"},
	}
	mockSyncService := &mockBudgetSyncService{}
	mockTxRepo := &mockTransactionRepoForBudget{}

	handler := NewBudgetHandler(mockService, mockSyncService, services.NewTransactionService(mockTxRepo, &mockCustomCategoryRepoForBudget{}))

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

	handler := NewBudgetHandler(mockService, mockSyncService, services.NewTransactionService(mockTxRepo, &mockCustomCategoryRepoForBudget{}))

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

	handler := NewBudgetHandler(mockService, mockSyncService, services.NewTransactionService(mockTxRepo, &mockCustomCategoryRepoForBudget{}))

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

	handler := NewBudgetHandler(mockService, mockSyncService, services.NewTransactionService(mockTxRepo, &mockCustomCategoryRepoForBudget{}))

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

	handler := NewBudgetHandler(mockService, mockSyncService, services.NewTransactionService(mockTxRepo, &mockCustomCategoryRepoForBudget{}))

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
	handler := NewBudgetHandler(mockService, mockSyncService, services.NewTransactionService(mockTxRepo, &mockCustomCategoryRepoForBudget{}))
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

	handler := NewBudgetHandler(mockService, mockSyncService, services.NewTransactionService(mockTxRepo, &mockCustomCategoryRepoForBudget{}))

	input := &CreateBudgetInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-123"},
	}
	input.Body = domain.CreateBudgetRequest{
		Name:           "Monthly Budget",
		Period:         domain.BudgetPeriodMonthly,
		StartDate:      domain.FlexibleDate(time.Now()),
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

	handler := NewBudgetHandler(mockService, mockSyncService, services.NewTransactionService(mockTxRepo, &mockCustomCategoryRepoForBudget{}))

	input := &CreateBudgetInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-123"},
	}
	input.Body = domain.CreateBudgetRequest{
		Name:         "Monthly Budget",
		Period:       domain.BudgetPeriodMonthly,
		StartDate:    domain.FlexibleDate(time.Now()),
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

	handler := NewBudgetHandler(mockService, mockSyncService, services.NewTransactionService(mockTxRepo, &mockCustomCategoryRepoForBudget{}))

	input := &CreateBudgetInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-123"},
	}
	input.Body = domain.CreateBudgetRequest{
		Name:           "Monthly Budget",
		Period:         domain.BudgetPeriodMonthly,
		StartDate:      domain.FlexibleDate(time.Now()),
		Amount:         100.00, // lower value
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

	handler := NewBudgetHandler(mockService, mockSyncService, services.NewTransactionService(mockTxRepo, &mockCustomCategoryRepoForBudget{}))

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

// --- MCP transactional branch tests (R9-7) ---

// mockBudgetIdempotencyRepository supports GetOrCreateInTx for budget tests.
// Reuses mockTx / mockDB from tags_test.go (same package).
type mockBudgetIdempotencyRepository struct {
	inTxCalls int
	cached    map[string]*domain.Budget
}

func (m *mockBudgetIdempotencyRepository) GetOrCreate(
	_ context.Context,
	key, _, _ string,
	fn func() (interface{}, error),
	result interface{},
) (bool, error) {
	if existing, ok := m.cached[key]; ok {
		*(result.(**domain.Budget)) = existing
		return true, nil
	}
	res, err := fn()
	if err != nil {
		return false, err
	}
	budget := res.(*domain.Budget)
	if m.cached == nil {
		m.cached = make(map[string]*domain.Budget)
	}
	m.cached[key] = budget
	*(result.(**domain.Budget)) = budget
	return false, nil
}

func (m *mockBudgetIdempotencyRepository) GetOrCreateInTx(
	ctx context.Context,
	_ pgx.Tx,
	key, userID, operationID string,
	fn func() (interface{}, error),
	result interface{},
) (bool, error) {
	m.inTxCalls++
	return m.GetOrCreate(ctx, key, userID, operationID, fn, result)
}

func (m *mockBudgetIdempotencyRepository) DeleteExpired(_ context.Context) (int64, error) {
	return 0, nil
}

// mockMCPAuditRepositoryForBudget succeeds on RecordInTx.
type mockMCPAuditRepositoryForBudget struct {
	recordInTxCalled bool
}

func (m *mockMCPAuditRepositoryForBudget) Record(_ context.Context, _ *domain.MCPAuditEntry) error {
	return nil
}
func (m *mockMCPAuditRepositoryForBudget) RecordInTx(_ context.Context, _ pgx.Tx, _ *domain.MCPAuditEntry) error {
	m.recordInTxCalled = true
	return nil
}

// mockMCPAuditRepositoryFailForBudget always fails on RecordInTx (rollback test).
type mockMCPAuditRepositoryFailForBudget struct{}

func (m *mockMCPAuditRepositoryFailForBudget) Record(_ context.Context, _ *domain.MCPAuditEntry) error {
	return nil
}
func (m *mockMCPAuditRepositoryFailForBudget) RecordInTx(_ context.Context, _ pgx.Tx, _ *domain.MCPAuditEntry) error {
	return fmt.Errorf("simulated audit DB failure")
}

// testBudget returns a minimal domain.Budget for use in tests.
func testBudget() *domain.Budget {
	return &domain.Budget{
		ID:        "budget-test-1",
		UserID:    "user-mcp-budget",
		Name:      "Test Budget",
		Amount:    1000,
		Period:    domain.BudgetPeriodMonthly,
		IsActive:  true,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
}

// TestCreateBudget_MCPOrigin_IdempotencyDedup verifies the MCP transactional path (R9-7 / R8-1):
// Two MCP POST /budgets requests with the same Idempotency-Key return the same body and
// only invoke GetOrCreateInTx (not double-create).
func TestCreateBudget_MCPOrigin_IdempotencyDedup(t *testing.T) {
	userID := "user-mcp-budget"
	idempotencyKey := "idem-budget-r9"
	expectedBudget := testBudget()
	const workerSecret = "test-worker-secret-r9"

	tx := &mockTx{}
	db := &mockDB{tx: tx}
	auditRepo := &mockMCPAuditRepositoryForBudget{}

	idempotencyRepo := &mockBudgetIdempotencyRepository{
		cached: map[string]*domain.Budget{
			idempotencyKey: expectedBudget,
		},
	}

	svc := &mockBudgetService{budget: expectedBudget}
	syncSvc := &mockBudgetSyncService{}
	// Use the existing repo-based transaction service (same pattern as other budget tests)
	txSvc := services.NewTransactionService(&mockTransactionRepoForBudget{}, &mockCustomCategoryRepoForBudget{})

	handler := NewBudgetHandler(svc, syncSvc, txSvc, BudgetHandlerDeps{
		IdempotencyRepo: idempotencyRepo,
		DB:              db,
		MCPAuditRepo:    auditRepo,
		MCPWorkerSecret: workerSecret,
	})

	input := &CreateBudgetInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: userID},
		IdempotencyInput:   inputs.IdempotencyInput{IdempotencyKey: idempotencyKey},
		MCPContextInput:    inputs.MCPContextInput{MCPGrantID: "grant-r9", MCPWorkerSecret: workerSecret},
		Body: domain.CreateBudgetRequest{
			Name:   "Test Budget",
			Period: domain.BudgetPeriodMonthly,
			Amount: 1000,
		},
	}

	// First POST — cache hit, returns cached budget
	out1, err := handler.CreateBudget(context.Background(), input)
	require.NoError(t, err)
	require.NotNil(t, out1)
	assert.Equal(t, expectedBudget.ID, out1.Body.ID)

	// Second POST with same key — also returns cached budget
	out2, err := handler.CreateBudget(context.Background(), input)
	require.NoError(t, err)
	require.NotNil(t, out2)
	assert.Equal(t, out1.Body.ID, out2.Body.ID)

	// GetOrCreateInTx called twice; cached result returned each time
	assert.Equal(t, 2, idempotencyRepo.inTxCalls, "GetOrCreateInTx must be called for each MCP request")
	// No audit row written on cache-hit
	assert.False(t, auditRepo.recordInTxCalled, "RecordInTx must NOT be called on cache-hit")
	// No service CreateBudget call (cache pre-seeded)
	assert.Equal(t, 0, svc.createCallCount, "budget service must not be called on cache-hit")
}

// TestCreateBudget_MCPOrigin_AuditFailureRollsBack verifies fail-closed audit (R9-7 / R8-1):
// If audit INSERT fails inside the tx, the budget must NOT be persisted (rollback).
func TestCreateBudget_MCPOrigin_AuditFailureRollsBack(t *testing.T) {
	userID := "user-mcp-audit-fail"
	const workerSecret = "test-worker-secret-fail"

	tx := &mockTx{}
	db := &mockDB{tx: tx}

	handler := NewBudgetHandler(
		&mockBudgetService{budget: testBudget()},
		&mockBudgetSyncService{},
		services.NewTransactionService(&mockTransactionRepoForBudget{}, &mockCustomCategoryRepoForBudget{}),
		BudgetHandlerDeps{
			IdempotencyRepo: &mockBudgetIdempotencyRepository{},
			DB:              db,
			MCPAuditRepo:    &mockMCPAuditRepositoryFailForBudget{},
			MCPWorkerSecret: workerSecret,
		},
	)

	input := &CreateBudgetInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: userID},
		IdempotencyInput:   inputs.IdempotencyInput{IdempotencyKey: ""},
		MCPContextInput:    inputs.MCPContextInput{MCPGrantID: "grant-fail", MCPWorkerSecret: workerSecret},
		Body: domain.CreateBudgetRequest{
			Name:      "Fail Budget",
			Period:    domain.BudgetPeriodMonthly,
			Amount:    100,
			StartDate: domain.FlexibleDate(time.Now()),
		},
	}

	out, err := handler.CreateBudget(context.Background(), input)
	assert.Error(t, err, "handler must return error when audit INSERT fails (fail-closed)")
	assert.Nil(t, out)
	assert.False(t, tx.committed, "tx must NOT be committed when audit fails")
}

// mockGoalOwnershipCheckerForBudget is a simple ownership checker that accepts/rejects
// based on a pre-configured map of (goalID → error). Used by R5-1 handler tests.
type mockGoalOwnershipCheckerForBudget struct {
	// results maps goalID to the error the checker should return (nil = owned).
	results map[string]error
	// calls records each (goalID, userID) pair passed to GetByIDForUser.
	calls []string
}

func (m *mockGoalOwnershipCheckerForBudget) GetByIDForUser(_ context.Context, id, _ string) (*domain.SavingsGoal, error) {
	m.calls = append(m.calls, id)
	if err, ok := m.results[id]; ok {
		if err != nil {
			return nil, err
		}
		return &domain.SavingsGoal{ID: id}, nil
	}
	return nil, domain.ErrSavingsGoalNotFound
}

// TestCreateBudget_MCPOrigin_SavingsGoalIDOR verifies that an MCP-origin CreateBudget
// request including a savings category that references another user's savings goal is
// rejected before any budget row is persisted (R5-1).
//
// Root cause: the MCP path in CreateBudget constructed a fresh txBudgetService with
// nil deps and never called SetGoalOwnershipChecker — so the R4-3 ownership check
// silently degraded to a WARN-log no-op on that path.
func TestCreateBudget_MCPOrigin_SavingsGoalIDOR(t *testing.T) {
	const workerSecret = "test-worker-secret-mcp-idor"
	userID := "user-mcp-idor"
	otherGoalID := "goal-other-user-mcp"
	ownGoalID := "goal-own-user-mcp"

	makeHandler := func(checker services.GoalOwnershipChecker) (*BudgetHandler, *mockTx) {
		tx := &mockTx{}
		db := &mockDB{tx: tx}
		auditRepo := &mockMCPAuditRepositoryForBudget{}
		handler := NewBudgetHandler(
			&mockBudgetService{},
			&mockBudgetSyncService{},
			services.NewTransactionService(&mockTransactionRepoForBudget{}, &mockCustomCategoryRepoForBudget{}),
			BudgetHandlerDeps{
				DB:                   db,
				MCPAuditRepo:         auditRepo,
				MCPWorkerSecret:      workerSecret,
				GoalOwnershipChecker: checker,
			},
		)
		return handler, tx
	}

	t.Run("rejects cross-user savings goal in MCP path", func(t *testing.T) {
		checker := &mockGoalOwnershipCheckerForBudget{
			results: map[string]error{
				otherGoalID: domain.ErrSavingsGoalNotFound,
			},
		}
		handler, tx := makeHandler(checker)

		_, err := handler.CreateBudget(context.Background(), &CreateBudgetInput{
			AuthenticatedInput: inputs.AuthenticatedInput{UserID: userID},
			MCPContextInput:    inputs.MCPContextInput{MCPGrantID: "grant-idor", MCPWorkerSecret: workerSecret},
			Body: domain.CreateBudgetRequest{
				Name:         "IDOR Budget",
				Period:       domain.BudgetPeriodMonthly,
				StartDate:    domain.FlexibleDate(time.Now()),
				Amount:       1000.00,
				CurrencyCode: "USD",
				Categories: []domain.CreateBudgetCategoryRequest{
					{
						CategoryKind:    "savings",
						SavingsGoalID:   &otherGoalID,
						AllocatedAmount: 200.00,
					},
				},
			},
		})

		require.Error(t, err, "MCP CreateBudget with cross-user savings goal must return an error")
		assert.False(t, tx.committed, "tx must NOT be committed — no budget should be persisted")
		// Verify the checker was actually invoked (not just any unrelated error path).
		assert.Contains(t, checker.calls, otherGoalID,
			"the ownership checker must have been called with the cross-user goal ID")
	})

	t.Run("accepts own savings goal in MCP path", func(t *testing.T) {
		checker := &mockGoalOwnershipCheckerForBudget{
			results: map[string]error{
				ownGoalID: nil, // owned by this user
			},
		}
		handler, tx := makeHandler(checker)

		out, err := handler.CreateBudget(context.Background(), &CreateBudgetInput{
			AuthenticatedInput: inputs.AuthenticatedInput{UserID: userID},
			MCPContextInput:    inputs.MCPContextInput{MCPGrantID: "grant-own", MCPWorkerSecret: workerSecret},
			Body: domain.CreateBudgetRequest{
				Name:         "Own-Goal Budget",
				Period:       domain.BudgetPeriodMonthly,
				StartDate:    domain.FlexibleDate(time.Now()),
				Amount:       1500.00,
				CurrencyCode: "USD",
				Categories: []domain.CreateBudgetCategoryRequest{
					{
						CategoryKind:    "savings",
						SavingsGoalID:   &ownGoalID,
						AllocatedAmount: 500.00,
					},
				},
			},
		})

		require.NoError(t, err, "MCP CreateBudget with own savings goal must succeed")
		require.NotNil(t, out)
		assert.True(t, tx.committed, "tx must be committed when the budget is created successfully")
	})
}

// TestBudgetResponseJSON tests JSON serialization of response.
func TestBudgetResponseJSON(t *testing.T) {
	mockService := &mockBudgetService{}
	mockSyncService := &mockBudgetSyncService{}
	mockTxRepo := &mockTransactionRepoForBudget{}

	_, api := humatest.New(t)
	handler := NewBudgetHandler(mockService, mockSyncService, services.NewTransactionService(mockTxRepo, &mockCustomCategoryRepoForBudget{}))
	RegisterBudgetRoutes(api, handler)

	resp := api.Get("/api/v1/budgets")
	assert.Equal(t, http.StatusUnauthorized, resp.Code)

	// Verify error response is valid JSON
	var errorResp map[string]interface{}
	err := json.Unmarshal(resp.Body.Bytes(), &errorResp)
	require.NoError(t, err, "Error response should be valid JSON")
}
