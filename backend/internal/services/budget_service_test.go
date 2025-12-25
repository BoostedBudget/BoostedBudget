package services

import (
	"context"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

// MockBudgetRepository is a mock implementation of repository.BudgetRepository
type MockBudgetRepository struct {
	mock.Mock
}

func (m *MockBudgetRepository) Create(ctx context.Context, budget *domain.Budget) error {
	args := m.Called(ctx, budget)
	return args.Error(0)
}

func (m *MockBudgetRepository) GetByID(ctx context.Context, id string) (*domain.Budget, error) {
	args := m.Called(ctx, id)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*domain.Budget), args.Error(1)
}

func (m *MockBudgetRepository) GetByUserID(ctx context.Context, userID string) ([]*domain.Budget, error) {
	args := m.Called(ctx, userID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]*domain.Budget), args.Error(1)
}

func (m *MockBudgetRepository) GetActiveByUserID(ctx context.Context, userID string) ([]*domain.Budget, error) {
	args := m.Called(ctx, userID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]*domain.Budget), args.Error(1)
}

func (m *MockBudgetRepository) GetByName(ctx context.Context, userID, name string) (*domain.Budget, error) {
	args := m.Called(ctx, userID, name)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*domain.Budget), args.Error(1)
}

func (m *MockBudgetRepository) Update(ctx context.Context, budget *domain.Budget) error {
	args := m.Called(ctx, budget)
	return args.Error(0)
}

func (m *MockBudgetRepository) Delete(ctx context.Context, id, userID string) error {
	args := m.Called(ctx, id, userID)
	return args.Error(0)
}

func (m *MockBudgetRepository) CountByUserID(ctx context.Context, userID string) (int, error) {
	args := m.Called(ctx, userID)
	return args.Int(0), args.Error(1)
}

func (m *MockBudgetRepository) CreateCategory(ctx context.Context, category *domain.BudgetCategory) error {
	args := m.Called(ctx, category)
	return args.Error(0)
}

func (m *MockBudgetRepository) GetCategoriesByBudgetID(ctx context.Context, budgetID string) ([]domain.BudgetCategory, error) {
	args := m.Called(ctx, budgetID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]domain.BudgetCategory), args.Error(1)
}

func (m *MockBudgetRepository) UpdateCategory(ctx context.Context, category *domain.BudgetCategory) error {
	args := m.Called(ctx, category)
	return args.Error(0)
}

func (m *MockBudgetRepository) UpdateCategorySpentAmount(ctx context.Context, categoryID string, spentAmount float64) error {
	args := m.Called(ctx, categoryID, spentAmount)
	return args.Error(0)
}

func (m *MockBudgetRepository) DeleteCategory(ctx context.Context, id, budgetID string) error {
	args := m.Called(ctx, id, budgetID)
	return args.Error(0)
}

func (m *MockBudgetRepository) GetCategoriesMatchingTransaction(ctx context.Context, userID string, transactionCategory []string) ([]domain.BudgetCategory, error) {
	args := m.Called(ctx, userID, transactionCategory)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]domain.BudgetCategory), args.Error(1)
}

// Envelope budgeting methods (Issue #491)
func (m *MockBudgetRepository) UpdateBudgetIncome(ctx context.Context, budgetID string, incomeAmount float64) error {
	args := m.Called(ctx, budgetID, incomeAmount)
	return args.Error(0)
}

func (m *MockBudgetRepository) AddIncomeSource(ctx context.Context, budgetID string, source domain.IncomeSource) error {
	args := m.Called(ctx, budgetID, source)
	return args.Error(0)
}

func (m *MockBudgetRepository) RemoveIncomeSource(ctx context.Context, budgetID string, sourceIndex int) error {
	args := m.Called(ctx, budgetID, sourceIndex)
	return args.Error(0)
}

// Envelope budgeting rollover method (Issue #36: Rule Executors)
func (m *MockBudgetRepository) AdjustCategoryAllocatedAmount(ctx context.Context, categoryID string, delta float64) error {
	args := m.Called(ctx, categoryID, delta)
	return args.Error(0)
}

// MockTransactionRepositoryForBudget is a minimal mock for transaction repository
// Only mocking methods needed by BudgetService
type MockTransactionRepositoryForBudget struct {
	mock.Mock
}

func (m *MockTransactionRepositoryForBudget) GetDistinctPlaidCategories(ctx context.Context, userID string) ([]string, error) {
	args := m.Called(ctx, userID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]string), args.Error(1)
}

// Helper to create a test logger
func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
}

// Helper to create test budget
func createTestBudgetForService(userID string) *domain.Budget {
	return &domain.Budget{
		ID:           "budget-123",
		UserID:       userID,
		Name:         "Monthly Expenses",
		Period:       domain.BudgetPeriodMonthly,
		StartDate:    time.Now().AddDate(0, 0, -15),
		Amount:       1000.00,
		CurrencyCode: "USD",
		IsActive:     true,
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
	}
}

// Helper to create test create budget request
func createTestCreateBudgetRequest() *domain.CreateBudgetRequest {
	return &domain.CreateBudgetRequest{
		Name:         "Monthly Expenses",
		Period:       domain.BudgetPeriodMonthly,
		StartDate:    time.Now(),
		Amount:       1000.00,
		CurrencyCode: "USD",
	}
}

// ============================================================================
// Budget CRUD Tests
// ============================================================================

func TestBudgetService_CreateBudget_Success(t *testing.T) {
	mockBudgetRepo := new(MockBudgetRepository)
	mockCategoryRepo := new(MockCustomCategoryRepository)
	mockTransactionRepo := new(MockTransactionRepositoryForBudget)

	service := NewBudgetService(mockBudgetRepo, mockCategoryRepo, mockTransactionRepo, testLogger())

	ctx := context.Background()
	userID := "user-123"
	req := createTestCreateBudgetRequest()

	// Setup mocks
	mockBudgetRepo.On("Create", ctx, mock.AnythingOfType("*domain.Budget")).Return(nil)

	// Execute
	budget, err := service.CreateBudget(ctx, userID, req)

	// Assert
	assert.NoError(t, err)
	assert.NotNil(t, budget)
	assert.Equal(t, userID, budget.UserID)
	assert.Equal(t, req.Name, budget.Name)
	assert.Equal(t, req.Period, budget.Period)
	assert.Equal(t, req.Amount, budget.Amount)
	mockBudgetRepo.AssertExpectations(t)
}

func TestBudgetService_CreateBudget_ValidationError(t *testing.T) {
	mockBudgetRepo := new(MockBudgetRepository)
	mockCategoryRepo := new(MockCustomCategoryRepository)
	mockTransactionRepo := new(MockTransactionRepositoryForBudget)

	service := NewBudgetService(mockBudgetRepo, mockCategoryRepo, mockTransactionRepo, testLogger())

	ctx := context.Background()
	userID := "user-123"

	// Request with missing required fields
	req := &domain.CreateBudgetRequest{
		Name: "", // Empty name - validation error
	}

	// Execute
	budget, err := service.CreateBudget(ctx, userID, req)

	// Assert
	assert.Error(t, err)
	assert.Nil(t, budget)
	assert.Contains(t, err.Error(), "name is required")
}

func TestBudgetService_CreateBudget_DuplicateName(t *testing.T) {
	mockBudgetRepo := new(MockBudgetRepository)
	mockCategoryRepo := new(MockCustomCategoryRepository)
	mockTransactionRepo := new(MockTransactionRepositoryForBudget)

	service := NewBudgetService(mockBudgetRepo, mockCategoryRepo, mockTransactionRepo, testLogger())

	ctx := context.Background()
	userID := "user-123"
	req := createTestCreateBudgetRequest()

	// Setup mocks - simulate duplicate name error
	mockBudgetRepo.On("Create", ctx, mock.AnythingOfType("*domain.Budget")).Return(domain.ErrBudgetAlreadyExists)

	// Execute
	budget, err := service.CreateBudget(ctx, userID, req)

	// Assert
	assert.ErrorIs(t, err, domain.ErrBudgetAlreadyExists)
	assert.Nil(t, budget)
	mockBudgetRepo.AssertExpectations(t)
}

func TestBudgetService_GetBudget_Success(t *testing.T) {
	mockBudgetRepo := new(MockBudgetRepository)
	mockCategoryRepo := new(MockCustomCategoryRepository)
	mockTransactionRepo := new(MockTransactionRepositoryForBudget)

	service := NewBudgetService(mockBudgetRepo, mockCategoryRepo, mockTransactionRepo, testLogger())

	ctx := context.Background()
	userID := "user-123"
	budgetID := "budget-123"

	expectedBudget := createTestBudgetForService(userID)

	// Setup mocks
	mockBudgetRepo.On("GetByID", ctx, budgetID).Return(expectedBudget, nil)

	// Execute
	budget, err := service.GetBudget(ctx, userID, budgetID)

	// Assert
	assert.NoError(t, err)
	assert.NotNil(t, budget)
	assert.Equal(t, expectedBudget.ID, budget.ID)
	mockBudgetRepo.AssertExpectations(t)
}

func TestBudgetService_GetBudget_NotFound(t *testing.T) {
	mockBudgetRepo := new(MockBudgetRepository)
	mockCategoryRepo := new(MockCustomCategoryRepository)
	mockTransactionRepo := new(MockTransactionRepositoryForBudget)

	service := NewBudgetService(mockBudgetRepo, mockCategoryRepo, mockTransactionRepo, testLogger())

	ctx := context.Background()
	userID := "user-123"
	budgetID := "nonexistent-budget"

	// Setup mocks
	mockBudgetRepo.On("GetByID", ctx, budgetID).Return(nil, domain.ErrBudgetNotFound)

	// Execute
	budget, err := service.GetBudget(ctx, userID, budgetID)

	// Assert
	assert.ErrorIs(t, err, domain.ErrBudgetNotFound)
	assert.Nil(t, budget)
	mockBudgetRepo.AssertExpectations(t)
}

func TestBudgetService_GetBudget_WrongUser(t *testing.T) {
	mockBudgetRepo := new(MockBudgetRepository)
	mockCategoryRepo := new(MockCustomCategoryRepository)
	mockTransactionRepo := new(MockTransactionRepositoryForBudget)

	service := NewBudgetService(mockBudgetRepo, mockCategoryRepo, mockTransactionRepo, testLogger())

	ctx := context.Background()
	wrongUserID := "wrong-user"
	budgetID := "budget-123"

	// Budget belongs to a different user
	budgetOwnedByOther := createTestBudgetForService("actual-owner")

	// Setup mocks
	mockBudgetRepo.On("GetByID", ctx, budgetID).Return(budgetOwnedByOther, nil)

	// Execute
	budget, err := service.GetBudget(ctx, wrongUserID, budgetID)

	// Assert
	assert.ErrorIs(t, err, domain.ErrBudgetNotFound)
	assert.Nil(t, budget)
	mockBudgetRepo.AssertExpectations(t)
}

func TestBudgetService_GetBudgets_Success(t *testing.T) {
	mockBudgetRepo := new(MockBudgetRepository)
	mockCategoryRepo := new(MockCustomCategoryRepository)
	mockTransactionRepo := new(MockTransactionRepositoryForBudget)

	service := NewBudgetService(mockBudgetRepo, mockCategoryRepo, mockTransactionRepo, testLogger())

	ctx := context.Background()
	userID := "user-123"

	expectedBudgets := []*domain.Budget{
		createTestBudgetForService(userID),
		{
			ID:           "budget-456",
			UserID:       userID,
			Name:         "Groceries Budget",
			Period:       domain.BudgetPeriodWeekly,
			StartDate:    time.Now(),
			Amount:       200.00,
			CurrencyCode: "USD",
			IsActive:     true,
		},
	}

	// Setup mocks
	mockBudgetRepo.On("GetByUserID", ctx, userID).Return(expectedBudgets, nil)

	// Execute
	budgets, err := service.GetBudgets(ctx, userID)

	// Assert
	assert.NoError(t, err)
	assert.Len(t, budgets, 2)
	mockBudgetRepo.AssertExpectations(t)
}

func TestBudgetService_GetActiveBudgets_Success(t *testing.T) {
	mockBudgetRepo := new(MockBudgetRepository)
	mockCategoryRepo := new(MockCustomCategoryRepository)
	mockTransactionRepo := new(MockTransactionRepositoryForBudget)

	service := NewBudgetService(mockBudgetRepo, mockCategoryRepo, mockTransactionRepo, testLogger())

	ctx := context.Background()
	userID := "user-123"

	expectedBudgets := []*domain.Budget{
		createTestBudgetForService(userID),
	}

	// Setup mocks
	mockBudgetRepo.On("GetActiveByUserID", ctx, userID).Return(expectedBudgets, nil)

	// Execute
	budgets, err := service.GetActiveBudgets(ctx, userID)

	// Assert
	assert.NoError(t, err)
	assert.Len(t, budgets, 1)
	assert.True(t, budgets[0].IsActive)
	mockBudgetRepo.AssertExpectations(t)
}

func TestBudgetService_GetCurrentBudget_Success(t *testing.T) {
	mockBudgetRepo := new(MockBudgetRepository)
	mockCategoryRepo := new(MockCustomCategoryRepository)
	mockTransactionRepo := new(MockTransactionRepositoryForBudget)

	service := NewBudgetService(mockBudgetRepo, mockCategoryRepo, mockTransactionRepo, testLogger())

	ctx := context.Background()
	userID := "user-123"

	// Budget that covers today
	now := time.Now()
	currentBudget := &domain.Budget{
		ID:           "budget-current",
		UserID:       userID,
		Name:         "Current Month",
		Period:       domain.BudgetPeriodMonthly,
		StartDate:    now.AddDate(0, 0, -15), // Started 15 days ago
		Amount:       1000.00,
		CurrencyCode: "USD",
		IsActive:     true,
	}

	// Old inactive budget
	oldBudget := &domain.Budget{
		ID:           "budget-old",
		UserID:       userID,
		Name:         "Last Month",
		Period:       domain.BudgetPeriodMonthly,
		StartDate:    now.AddDate(0, -2, 0), // Started 2 months ago
		Amount:       800.00,
		CurrencyCode: "USD",
		IsActive:     false,
	}

	activeBudgets := []*domain.Budget{currentBudget, oldBudget}

	// Setup mocks
	mockBudgetRepo.On("GetActiveByUserID", ctx, userID).Return(activeBudgets, nil)

	// Execute
	budget, err := service.GetCurrentBudget(ctx, userID)

	// Assert
	assert.NoError(t, err)
	assert.NotNil(t, budget)
	assert.Equal(t, "budget-current", budget.ID)
	mockBudgetRepo.AssertExpectations(t)
}

func TestBudgetService_UpdateBudget_Success(t *testing.T) {
	mockBudgetRepo := new(MockBudgetRepository)
	mockCategoryRepo := new(MockCustomCategoryRepository)
	mockTransactionRepo := new(MockTransactionRepositoryForBudget)

	service := NewBudgetService(mockBudgetRepo, mockCategoryRepo, mockTransactionRepo, testLogger())

	ctx := context.Background()
	userID := "user-123"
	budgetID := "budget-123"
	newName := "Updated Budget Name"
	newAmount := 1500.00

	existingBudget := createTestBudgetForService(userID)

	req := &domain.UpdateBudgetRequest{
		Name:   &newName,
		Amount: &newAmount,
	}

	// Setup mocks
	mockBudgetRepo.On("GetByID", ctx, budgetID).Return(existingBudget, nil)
	mockBudgetRepo.On("Update", ctx, mock.AnythingOfType("*domain.Budget")).Return(nil)

	// Execute
	budget, err := service.UpdateBudget(ctx, userID, budgetID, req)

	// Assert
	assert.NoError(t, err)
	assert.NotNil(t, budget)
	assert.Equal(t, newName, budget.Name)
	assert.Equal(t, newAmount, budget.Amount)
	mockBudgetRepo.AssertExpectations(t)
}

func TestBudgetService_UpdateBudget_NotFound(t *testing.T) {
	mockBudgetRepo := new(MockBudgetRepository)
	mockCategoryRepo := new(MockCustomCategoryRepository)
	mockTransactionRepo := new(MockTransactionRepositoryForBudget)

	service := NewBudgetService(mockBudgetRepo, mockCategoryRepo, mockTransactionRepo, testLogger())

	ctx := context.Background()
	userID := "user-123"
	budgetID := "nonexistent"
	newName := "Updated"

	req := &domain.UpdateBudgetRequest{
		Name: &newName,
	}

	// Setup mocks
	mockBudgetRepo.On("GetByID", ctx, budgetID).Return(nil, domain.ErrBudgetNotFound)

	// Execute
	budget, err := service.UpdateBudget(ctx, userID, budgetID, req)

	// Assert
	assert.ErrorIs(t, err, domain.ErrBudgetNotFound)
	assert.Nil(t, budget)
	mockBudgetRepo.AssertExpectations(t)
}

func TestBudgetService_DeleteBudget_Success(t *testing.T) {
	mockBudgetRepo := new(MockBudgetRepository)
	mockCategoryRepo := new(MockCustomCategoryRepository)
	mockTransactionRepo := new(MockTransactionRepositoryForBudget)

	service := NewBudgetService(mockBudgetRepo, mockCategoryRepo, mockTransactionRepo, testLogger())

	ctx := context.Background()
	userID := "user-123"
	budgetID := "budget-123"

	existingBudget := createTestBudgetForService(userID)

	// Setup mocks
	mockBudgetRepo.On("GetByID", ctx, budgetID).Return(existingBudget, nil)
	mockBudgetRepo.On("Delete", ctx, budgetID, userID).Return(nil)

	// Execute
	err := service.DeleteBudget(ctx, userID, budgetID)

	// Assert
	assert.NoError(t, err)
	mockBudgetRepo.AssertExpectations(t)
}

func TestBudgetService_DeleteBudget_NotFound(t *testing.T) {
	mockBudgetRepo := new(MockBudgetRepository)
	mockCategoryRepo := new(MockCustomCategoryRepository)
	mockTransactionRepo := new(MockTransactionRepositoryForBudget)

	service := NewBudgetService(mockBudgetRepo, mockCategoryRepo, mockTransactionRepo, testLogger())

	ctx := context.Background()
	userID := "user-123"
	budgetID := "nonexistent"

	// Setup mocks
	mockBudgetRepo.On("GetByID", ctx, budgetID).Return(nil, domain.ErrBudgetNotFound)

	// Execute
	err := service.DeleteBudget(ctx, userID, budgetID)

	// Assert
	assert.ErrorIs(t, err, domain.ErrBudgetNotFound)
	mockBudgetRepo.AssertExpectations(t)
}

// ============================================================================
// Category Tests
// ============================================================================

func TestBudgetService_AddCategory_Success(t *testing.T) {
	mockBudgetRepo := new(MockBudgetRepository)
	mockCategoryRepo := new(MockCustomCategoryRepository)
	mockTransactionRepo := new(MockTransactionRepositoryForBudget)

	service := NewBudgetService(mockBudgetRepo, mockCategoryRepo, mockTransactionRepo, testLogger())

	ctx := context.Background()
	userID := "user-123"
	budgetID := "budget-123"

	existingBudget := createTestBudgetForService(userID)

	req := &domain.CreateBudgetCategoryRequest{
		Category:        []string{"Food And Drink"},
		AllocatedAmount: 300.00,
	}

	// Custom category with same name exists
	customCats := []*domain.CustomCategory{
		{ID: "cat-1", UserID: userID, Name: "Groceries"},
	}

	// Plaid categories include Food And Drink
	plaidCats := []string{"FOOD_AND_DRINK", "TRANSPORTATION"}

	// Setup mocks
	mockBudgetRepo.On("GetByID", ctx, budgetID).Return(existingBudget, nil)
	mockCategoryRepo.On("GetByUserID", ctx, userID).Return(customCats, nil)
	mockTransactionRepo.On("GetDistinctPlaidCategories", ctx, userID).Return(plaidCats, nil)
	mockBudgetRepo.On("CreateCategory", ctx, mock.AnythingOfType("*domain.BudgetCategory")).Return(nil)

	// Execute
	category, err := service.AddCategory(ctx, userID, budgetID, req)

	// Assert
	assert.NoError(t, err)
	assert.NotNil(t, category)
	assert.Equal(t, budgetID, category.BudgetID)
	assert.Equal(t, req.AllocatedAmount, category.AllocatedAmount)
	mockBudgetRepo.AssertExpectations(t)
	mockCategoryRepo.AssertExpectations(t)
	mockTransactionRepo.AssertExpectations(t)
}

func TestBudgetService_UpdateCategory_Success(t *testing.T) {
	mockBudgetRepo := new(MockBudgetRepository)
	mockCategoryRepo := new(MockCustomCategoryRepository)
	mockTransactionRepo := new(MockTransactionRepositoryForBudget)

	service := NewBudgetService(mockBudgetRepo, mockCategoryRepo, mockTransactionRepo, testLogger())

	ctx := context.Background()
	userID := "user-123"
	budgetID := "budget-123"
	categoryID := "cat-123"
	newAmount := 500.00

	existingBudget := createTestBudgetForService(userID)
	existingBudget.Categories = []domain.BudgetCategory{
		{
			ID:              categoryID,
			BudgetID:        budgetID,
			Category:        []string{"Food And Drink"},
			AllocatedAmount: 300.00,
		},
	}

	req := &domain.UpdateBudgetCategoryRequest{
		AllocatedAmount: &newAmount,
	}

	// Setup mocks
	mockBudgetRepo.On("GetByID", ctx, budgetID).Return(existingBudget, nil)
	mockBudgetRepo.On("UpdateCategory", ctx, mock.AnythingOfType("*domain.BudgetCategory")).Return(nil)

	// Execute
	category, err := service.UpdateCategory(ctx, userID, budgetID, categoryID, req)

	// Assert
	assert.NoError(t, err)
	assert.NotNil(t, category)
	assert.Equal(t, newAmount, category.AllocatedAmount)
	mockBudgetRepo.AssertExpectations(t)
}

func TestBudgetService_DeleteCategory_Success(t *testing.T) {
	mockBudgetRepo := new(MockBudgetRepository)
	mockCategoryRepo := new(MockCustomCategoryRepository)
	mockTransactionRepo := new(MockTransactionRepositoryForBudget)

	service := NewBudgetService(mockBudgetRepo, mockCategoryRepo, mockTransactionRepo, testLogger())

	ctx := context.Background()
	userID := "user-123"
	budgetID := "budget-123"
	categoryID := "cat-123"

	existingBudget := createTestBudgetForService(userID)
	existingBudget.Categories = []domain.BudgetCategory{
		{
			ID:       categoryID,
			BudgetID: budgetID,
			Category: []string{"Food And Drink"},
		},
	}

	// Setup mocks
	mockBudgetRepo.On("GetByID", ctx, budgetID).Return(existingBudget, nil)
	mockBudgetRepo.On("DeleteCategory", ctx, categoryID, budgetID).Return(nil)

	// Execute
	err := service.DeleteCategory(ctx, userID, budgetID, categoryID)

	// Assert
	assert.NoError(t, err)
	mockBudgetRepo.AssertExpectations(t)
}

// ============================================================================
// Available Categories Tests
// ============================================================================

func TestBudgetService_GetAvailableCategories_Success(t *testing.T) {
	mockBudgetRepo := new(MockBudgetRepository)
	mockCategoryRepo := new(MockCustomCategoryRepository)
	mockTransactionRepo := new(MockTransactionRepositoryForBudget)

	service := NewBudgetService(mockBudgetRepo, mockCategoryRepo, mockTransactionRepo, testLogger())

	ctx := context.Background()
	userID := "user-123"

	// Custom categories
	customCats := []*domain.CustomCategory{
		{ID: "cat-1", UserID: userID, Name: "Groceries"},
		{ID: "cat-2", UserID: userID, Name: "Dining", ParentCategory: strPtr("Food")},
	}

	// Plaid categories
	plaidCats := []string{"FOOD_AND_DRINK", "TRANSPORTATION", "ENTERTAINMENT"}

	// Setup mocks
	mockCategoryRepo.On("GetByUserID", ctx, userID).Return(customCats, nil)
	mockTransactionRepo.On("GetDistinctPlaidCategories", ctx, userID).Return(plaidCats, nil)

	// Execute
	categories, err := service.GetAvailableCategories(ctx, userID)

	// Assert
	assert.NoError(t, err)
	assert.NotNil(t, categories)
	// Should contain merged categories (custom + plaid formatted)
	assert.Contains(t, categories, "Groceries")
	assert.Contains(t, categories, "Food > Dining")
	assert.Contains(t, categories, "Food And Drink")
	assert.Contains(t, categories, "Transportation")
	assert.Contains(t, categories, "Entertainment")
	mockCategoryRepo.AssertExpectations(t)
	mockTransactionRepo.AssertExpectations(t)
}

// strPtr helper is defined in rule_application_service_test.go
