package services

import (
	"context"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/repository"
	"github.com/maxnussbaum/BoostedBudget/internal/testsupport/mockassert"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
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

func (m *MockBudgetRepository) Delete(ctx context.Context, id, userID string, auditLog repository.AuditLogRepository) error {
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

func (m *MockBudgetRepository) GetCategoriesByBudgetIDs(ctx context.Context, budgetIDs []string) (map[string][]domain.BudgetCategory, error) {
	args := m.Called(ctx, budgetIDs)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(map[string][]domain.BudgetCategory), args.Error(1)
}

// BeginTx is a mock implementation (Issue #1630 B3b).
func (m *MockBudgetRepository) BeginTx(ctx context.Context) (repository.Transaction, error) {
	args := m.Called(ctx)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(repository.Transaction), args.Error(1)
}

// GetCategoriesByBudgetIDLockedInTx is a mock implementation (Issue #1630 B3b).
func (m *MockBudgetRepository) GetCategoriesByBudgetIDLockedInTx(ctx context.Context, tx repository.Transaction, budgetID string) ([]domain.BudgetCategory, error) {
	args := m.Called(ctx, tx, budgetID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]domain.BudgetCategory), args.Error(1)
}

// AddCategoryChecked is a mock implementation (Issue #1630 B3). Unlike the real repository, the
// mock does NOT run domain.DuplicateParentIdentity / domain.EscapedNestedChild itself — tests
// stub the return value directly, since AddCategory's own in-memory pre-check already exercises
// those guards against the initial GetByID snapshot and the repository's dedicated tests
// (budget_repository_test.go / the B3 concurrency integration test) exercise the locked re-check.
// On a stubbed nil error, echoes candidate back (mirroring the real repository, which returns the
// same row it inserted) unless the stub supplied an explicit replacement category.
func (m *MockBudgetRepository) AddCategoryChecked(ctx context.Context, budgetID string, candidate *domain.BudgetCategory) (*domain.BudgetCategory, error) {
	args := m.Called(ctx, budgetID, candidate)
	if err := args.Error(1); err != nil {
		return nil, err
	}
	if args.Get(0) != nil {
		return args.Get(0).(*domain.BudgetCategory), nil
	}
	return candidate, nil
}

func (m *MockBudgetRepository) UpdateCategory(ctx context.Context, category *domain.BudgetCategory) error {
	args := m.Called(ctx, category)
	return args.Error(0)
}

// UpdateCategoryChecked is a mock implementation (Issue #1630 B3). It does NOT run
// domain.EscapedNestedChild itself (the same rationale as AddCategoryChecked's mock above), but it
// DOES invoke mutate against the stubbed return object before returning it — mirroring
// production's mutate-then-persist order — so a test stubbing a baseline category and asserting on
// the field mutations req applied (e.g. a new AllocatedAmount) sees them reflected in the result,
// the same way it did against the old plain UpdateCategory mock's pass-through.
func (m *MockBudgetRepository) UpdateCategoryChecked(ctx context.Context, budgetID, categoryID string, mutate func(category *domain.BudgetCategory) error) (*domain.BudgetCategory, error) {
	args := m.Called(ctx, budgetID, categoryID, mutate)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	result := args.Get(0).(*domain.BudgetCategory)
	if err := mutate(result); err != nil {
		return nil, err
	}
	return result, args.Error(1)
}

func (m *MockBudgetRepository) UpdateCategorySpentAmount(ctx context.Context, categoryID string, spentAmount float64) error {
	args := m.Called(ctx, categoryID, spentAmount)
	return args.Error(0)
}

func (m *MockBudgetRepository) DeleteCategory(ctx context.Context, id, budgetID string, auditLog repository.AuditLogRepository) error {
	args := m.Called(ctx, id, budgetID)
	return args.Error(0)
}

// Envelope budgeting methods (Issue #491)
func (m *MockBudgetRepository) UpdateBudgetIncome(ctx context.Context, budgetID string, incomeAmount float64) error {
	args := m.Called(ctx, budgetID, incomeAmount)
	return args.Error(0)
}

// Issue #972: Paginated active-budget enumeration for period-close scanner
func (m *MockBudgetRepository) GetAllActivePaginated(ctx context.Context, limit, offset int) ([]*domain.Budget, error) {
	args := m.Called(ctx, limit, offset)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]*domain.Budget), args.Error(1)
}

// Issue #972: Batch timezone lookup for period-close scanner
func (m *MockBudgetRepository) GetTimezonesByUserIDs(ctx context.Context, userIDs []string) (map[string]string, error) {
	args := m.Called(ctx, userIDs)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(map[string]string), args.Error(1)
}

// GetCategoryByIDAndUser retrieves a category scoped to a user (Issue #1270: ownership validation).
func (m *MockBudgetRepository) GetCategoryByIDAndUser(ctx context.Context, categoryID, userID string) (*domain.BudgetCategory, error) {
	args := m.Called(ctx, categoryID, userID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*domain.BudgetCategory), args.Error(1)
}

// CreateWithCategories atomically creates a budget and its categories (R5-3 / Issue #1300).
func (m *MockBudgetRepository) CreateWithCategories(ctx context.Context, budget *domain.Budget, categories []*domain.BudgetCategory) error {
	args := m.Called(ctx, budget, categories)
	return args.Error(0)
}

// Savings goal category detach tombstone stubs (Issue #1300, Codex C6 / G5 migration 000098).
// All tombstone operations now key on series_id rather than budget_id.
func (m *MockBudgetRepository) IsGoalDetachedForSeries(ctx context.Context, seriesID, goalID string) (bool, error) {
	args := m.Called(ctx, seriesID, goalID)
	return args.Bool(0), args.Error(1)
}

func (m *MockBudgetRepository) InsertGoalDetach(ctx context.Context, seriesID, budgetID, goalID string) error {
	args := m.Called(ctx, seriesID, budgetID, goalID)
	return args.Error(0)
}

func (m *MockBudgetRepository) DeleteGoalDetach(ctx context.Context, seriesID, goalID string) error {
	args := m.Called(ctx, seriesID, goalID)
	return args.Error(0)
}

// Issue #1630 Task B7 stubs.
func (m *MockBudgetRepository) AuditBudgetHierarchy(ctx context.Context, budget *domain.Budget, periodStart, periodEnd time.Time, spentCalc repository.BudgetHierarchySpentCalculator) (domain.BudgetHierarchyAuditVerdict, error) {
	args := m.Called(ctx, budget, periodStart, periodEnd, spentCalc)
	verdict, _ := args.Get(0).(domain.BudgetHierarchyAuditVerdict)
	return verdict, args.Error(1)
}

func (m *MockBudgetRepository) BudgetIDsWithUnexaminedHierarchy(ctx context.Context, limit int) ([]string, error) {
	args := m.Called(ctx, limit)
	ids, _ := args.Get(0).([]string)
	return ids, args.Error(1)
}

func (m *MockBudgetRepository) GetDetachedGoalIDsForSeries(ctx context.Context, seriesID string) ([]string, error) {
	args := m.Called(ctx, seriesID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]string), args.Error(1)
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

func (m *MockTransactionRepositoryForBudget) BulkUpdateCategory(_ context.Context, _ string, _ []string, _ *string, _ *string, _ *string, _ *string, _ domain.CategorySource) (int64, error) {
	return 0, nil
}

func (m *MockTransactionRepositoryForBudget) GetReviewSummary(_ context.Context, _ string, _ domain.ReviewSummaryFilters) (*domain.ReviewSummaryResponse, error) {
	return &domain.ReviewSummaryResponse{TopUnreviewedCategories: []domain.TopUnreviewedCategory{}}, nil
}

// Helper to create a test logger
func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
}

// Helper to create test budget
func createTestBudgetForService(userID string) *domain.Budget {
	return &domain.Budget{
		ID:           "budget-123",
		SeriesID:     "budget-123", // standalone budget is its own series (Issue #1300 G5)
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
		StartDate:    domain.FlexibleDate(time.Now()),
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
	mockBudgetRepo.On("CreateWithCategories", ctx, mock.AnythingOfType("*domain.Budget"), mock.Anything).Return(nil)

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
	mockBudgetRepo.On("CreateWithCategories", ctx, mock.AnythingOfType("*domain.Budget"), mock.Anything).Return(domain.ErrBudgetAlreadyExists)

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
	mockBudgetRepo.On("GetCategoriesByBudgetID", ctx, "budget-123").Return([]domain.BudgetCategory{}, nil)
	mockBudgetRepo.On("GetCategoriesByBudgetID", ctx, "budget-456").Return([]domain.BudgetCategory{}, nil)

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
	mockBudgetRepo.On("GetCategoriesByBudgetID", ctx, "budget-123").Return([]domain.BudgetCategory{}, nil)

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
		CategoryKind:    "pfc",
		PFCPrimary:      strPtr("FOOD_AND_DRINK"),
		AllocatedAmount: 300.00,
	}

	// Setup mocks — category validation is deferred to domain so only GetByID and
	// AddCategoryChecked are called (Issue #1630 B3: AddCategoryChecked replaces CreateCategory
	// as the atomic locked insert).
	mockBudgetRepo.On("GetByID", ctx, budgetID).Return(existingBudget, nil)
	mockBudgetRepo.On("AddCategoryChecked", ctx, budgetID, mock.AnythingOfType("*domain.BudgetCategory")).Return(nil, nil)

	// Execute
	category, err := service.AddCategory(ctx, userID, budgetID, req)

	// Assert
	assert.NoError(t, err)
	assert.NotNil(t, category)
	assert.Equal(t, budgetID, category.BudgetID)
	assert.Equal(t, req.AllocatedAmount, category.AllocatedAmount)
	mockBudgetRepo.AssertExpectations(t)
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
			CategoryKind:    "pfc",
			PFCPrimary:      strPtr("FOOD_AND_DRINK"),
			AllocatedAmount: 300.00,
		},
	}

	req := &domain.UpdateBudgetCategoryRequest{
		AllocatedAmount: &newAmount,
	}

	// Setup mocks. UpdateCategoryChecked replaces UpdateCategory as the atomic locked
	// re-read-and-persist (Issue #1630 B3); the mock invokes the service's mutate closure against
	// the fresh snapshot below, mirroring the real repository re-reading the row under lock.
	mockBudgetRepo.On("GetByID", ctx, budgetID).Return(existingBudget, nil)
	freshSnapshot := existingBudget.Categories[0]
	mockBudgetRepo.On("UpdateCategoryChecked", ctx, budgetID, categoryID, mock.Anything).Return(&freshSnapshot, nil)

	// Execute
	category, err := service.UpdateCategory(ctx, userID, budgetID, categoryID, req)

	// Assert
	assert.NoError(t, err)
	assert.NotNil(t, category)
	assert.Equal(t, newAmount, category.AllocatedAmount)
	mockBudgetRepo.AssertExpectations(t)
}

// TestBudgetService_UpdateCategory_RejectsChildEscapingContainment pins the HIGH-severity gap
// this commit closes: a conforming nested child (category_only, contained under a category_only
// parent) can be PATCHed to a match_logic that escapes containment — recreating
// EscapedNestedChild's double-count as NEW data, not grandfathered. Issue #1630 gap analysis.
func TestBudgetService_UpdateCategory_RejectsChildEscapingContainment(t *testing.T) {
	mockBudgetRepo := new(MockBudgetRepository)
	mockCategoryRepo := new(MockCustomCategoryRepository)
	mockTransactionRepo := new(MockTransactionRepositoryForBudget)

	service := NewBudgetService(mockBudgetRepo, mockCategoryRepo, mockTransactionRepo, testLogger())

	ctx := context.Background()
	userID := "user-123"
	budgetID := "budget-123"
	childID := "cat-child"
	escapingLogic := domain.MatchLogicOr

	existingBudget := createTestBudgetForService(userID)
	existingBudget.Categories = []domain.BudgetCategory{
		{
			ID:              "cat-parent",
			BudgetID:        budgetID,
			CategoryKind:    "pfc",
			PFCPrimary:      strPtr("FOOD_AND_DRINK"),
			MatchLogic:      domain.MatchLogicCategoryOnly,
			AllocatedAmount: 300.00,
		},
		{
			ID:              childID,
			BudgetID:        budgetID,
			CategoryKind:    "pfc",
			PFCPrimary:      strPtr("FOOD_AND_DRINK"),
			PFCDetailed:     strPtr("FOOD_AND_DRINK_FAST_FOOD"),
			MatchLogic:      domain.MatchLogicCategoryOnly,
			AllocatedAmount: 50.00,
		},
	}

	req := &domain.UpdateBudgetCategoryRequest{
		MatchLogic: &escapingLogic,
		Tags:       &[]string{"splurge"},
	}

	mockBudgetRepo.On("GetByID", ctx, budgetID).Return(existingBudget, nil)

	category, err := service.UpdateCategory(ctx, userID, budgetID, childID, req)

	require.Error(t, err)
	assert.ErrorIs(t, err, domain.ErrEscapedNestedChild)
	assert.ErrorIs(t, err, domain.ErrInvalidBudget)
	assert.Contains(t, err.Error(), "match_logic")
	assert.Nil(t, category)
	mockassert.NoCallTo(t, &mockBudgetRepo.Mock, "UpdateCategoryChecked")
	mockBudgetRepo.AssertExpectations(t)
}

// TestBudgetService_UpdateCategory_RejectsParentOrphaningContainedChild pins the OTHER
// direction of the same gap: the row being edited may itself be the PARENT. Flipping a
// category_only parent to 'and' removes the containment envelope an existing, unchanged,
// still-conforming child relies on to stay excluded from top-level totals — recreating the
// same double count via the reverse edit. Previously caught by a separate
// domain.WouldOrphanContainedChildren guard; fixwave 4 widened domain.EscapedNestedChild to
// subsume that transition (see its doc comment), and removed the separate function — this test
// still passes unchanged because it only asserts errors.Is(err, ErrEscapedNestedChild), the
// sentinel both guards always wrapped.
func TestBudgetService_UpdateCategory_RejectsParentOrphaningContainedChild(t *testing.T) {
	mockBudgetRepo := new(MockBudgetRepository)
	mockCategoryRepo := new(MockCustomCategoryRepository)
	mockTransactionRepo := new(MockTransactionRepositoryForBudget)

	service := NewBudgetService(mockBudgetRepo, mockCategoryRepo, mockTransactionRepo, testLogger())

	ctx := context.Background()
	userID := "user-123"
	budgetID := "budget-123"
	parentID := "cat-parent"
	nonContainingLogic := domain.MatchLogicAnd

	existingBudget := createTestBudgetForService(userID)
	existingBudget.Categories = []domain.BudgetCategory{
		{
			ID:              parentID,
			BudgetID:        budgetID,
			CategoryKind:    "pfc",
			PFCPrimary:      strPtr("FOOD_AND_DRINK"),
			MatchLogic:      domain.MatchLogicCategoryOnly,
			AllocatedAmount: 300.00,
		},
		{
			ID:              "cat-child",
			BudgetID:        budgetID,
			CategoryKind:    "pfc",
			PFCPrimary:      strPtr("FOOD_AND_DRINK"),
			PFCDetailed:     strPtr("FOOD_AND_DRINK_FAST_FOOD"),
			MatchLogic:      domain.MatchLogicCategoryOnly,
			AllocatedAmount: 50.00,
		},
	}

	req := &domain.UpdateBudgetCategoryRequest{
		MatchLogic: &nonContainingLogic,
		Tags:       &[]string{"splurge"},
	}

	mockBudgetRepo.On("GetByID", ctx, budgetID).Return(existingBudget, nil)

	category, err := service.UpdateCategory(ctx, userID, budgetID, parentID, req)

	require.Error(t, err)
	assert.ErrorIs(t, err, domain.ErrEscapedNestedChild)
	assert.ErrorIs(t, err, domain.ErrInvalidBudget)
	assert.Contains(t, err.Error(), "match_logic")
	assert.Nil(t, category)
	mockassert.NoCallTo(t, &mockBudgetRepo.Mock, "UpdateCategoryChecked")
	mockBudgetRepo.AssertExpectations(t)
}

// TestBudgetService_UpdateCategory_AllowsUnrelatedMatchLogicChange confirms the two guards
// above do not over-reach: editing a top-level category's match_logic with no hierarchical
// relationship to any sibling remains legal.
func TestBudgetService_UpdateCategory_AllowsUnrelatedMatchLogicChange(t *testing.T) {
	mockBudgetRepo := new(MockBudgetRepository)
	mockCategoryRepo := new(MockCustomCategoryRepository)
	mockTransactionRepo := new(MockTransactionRepositoryForBudget)

	service := NewBudgetService(mockBudgetRepo, mockCategoryRepo, mockTransactionRepo, testLogger())

	ctx := context.Background()
	userID := "user-123"
	budgetID := "budget-123"
	categoryID := "cat-solo"
	newLogic := domain.MatchLogicOr

	existingBudget := createTestBudgetForService(userID)
	existingBudget.Categories = []domain.BudgetCategory{
		{
			ID:              categoryID,
			BudgetID:        budgetID,
			CategoryKind:    "pfc",
			PFCPrimary:      strPtr("FOOD_AND_DRINK"),
			MatchLogic:      domain.MatchLogicCategoryOnly,
			AllocatedAmount: 300.00,
		},
	}

	req := &domain.UpdateBudgetCategoryRequest{
		MatchLogic: &newLogic,
		Tags:       &[]string{"splurge"},
	}

	mockBudgetRepo.On("GetByID", ctx, budgetID).Return(existingBudget, nil)
	freshSnapshot := existingBudget.Categories[0]
	mockBudgetRepo.On("UpdateCategoryChecked", ctx, budgetID, categoryID, mock.Anything).Return(&freshSnapshot, nil)

	category, err := service.UpdateCategory(ctx, userID, budgetID, categoryID, req)

	assert.NoError(t, err)
	assert.NotNil(t, category)
	assert.Equal(t, newLogic, category.MatchLogic)
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
			ID:           categoryID,
			BudgetID:     budgetID,
			CategoryKind: "pfc",
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
		{ID: "cat-2", UserID: userID, Name: "Dining", ParentPFCPrimary: strPtr("FOOD_AND_DRINK")},
	}

	// Setup mocks — no GetDistinctPlaidCategories call expected (replaced by pfc.AllPrimaries())
	mockCategoryRepo.On("GetByUserID", ctx, userID).Return(customCats, nil)

	// Execute
	categories, err := service.GetAvailableCategories(ctx, userID)

	// Assert
	assert.NoError(t, err)
	assert.NotNil(t, categories)
	// Custom categories returned as-is (user-entered names)
	assert.Contains(t, categories, "Groceries")
	assert.Contains(t, categories, "Dining")
	// Curated PFCv2 primaries returned as enum values, not display strings
	assert.Contains(t, categories, "FOOD_AND_DRINK")
	assert.Contains(t, categories, "TRANSPORTATION")
	assert.Contains(t, categories, "ENTERTAINMENT")
	assert.NotContains(t, categories, "Food And Drink")
	mockCategoryRepo.AssertExpectations(t)
}

func TestGetAvailableCategories_IncludesCuratedPrimariesEvenWithoutObserved(t *testing.T) {
	mockBudgetRepo := new(MockBudgetRepository)
	mockCategoryRepo := new(MockCustomCategoryRepository)
	mockTransactionRepo := new(MockTransactionRepositoryForBudget)

	service := NewBudgetService(mockBudgetRepo, mockCategoryRepo, mockTransactionRepo, testLogger())

	ctx := context.Background()
	userID := "user-new"

	// Brand-new user with no custom categories
	mockCategoryRepo.On("GetByUserID", ctx, userID).Return([]*domain.CustomCategory{}, nil)

	categories, err := service.GetAvailableCategories(ctx, userID)

	assert.NoError(t, err)
	// All 18 PFCv2 primaries must be present as enum values
	assert.Contains(t, categories, "FOOD_AND_DRINK")
	assert.Contains(t, categories, "INCOME")
	assert.Contains(t, categories, "LOAN_DISBURSEMENTS")
	assert.Contains(t, categories, "OTHER")
	assert.GreaterOrEqual(t, len(categories), 18)
}

func TestGetAvailableCategories_DedupesCustomVsCuratedCaseInsensitive(t *testing.T) {
	mockBudgetRepo := new(MockBudgetRepository)
	mockCategoryRepo := new(MockCustomCategoryRepository)
	mockTransactionRepo := new(MockTransactionRepositoryForBudget)

	service := NewBudgetService(mockBudgetRepo, mockCategoryRepo, mockTransactionRepo, testLogger())

	ctx := context.Background()
	userID := "user-123"

	// Custom category with same name as curated primary (different case)
	customCats := []*domain.CustomCategory{
		{ID: "cat-1", UserID: userID, Name: "food and drink"},
	}
	mockCategoryRepo.On("GetByUserID", ctx, userID).Return(customCats, nil)

	categories, err := service.GetAvailableCategories(ctx, userID)

	assert.NoError(t, err)
	// Custom entry wins — curated enum version dropped (deduped by display-name match)
	assert.Contains(t, categories, "food and drink")
	// Curated "FOOD_AND_DRINK" enum must NOT appear (deduped by custom's display match)
	assert.NotContains(t, categories, "FOOD_AND_DRINK")
}

func TestGetAvailableCategories_DedupesCustomVsCuratedByEnumName(t *testing.T) {
	mockBudgetRepo := new(MockBudgetRepository)
	mockCategoryRepo := new(MockCustomCategoryRepository)
	mockTransactionRepo := new(MockTransactionRepositoryForBudget)

	service := NewBudgetService(mockBudgetRepo, mockCategoryRepo, mockTransactionRepo, testLogger())

	ctx := context.Background()
	userID := "user-123"

	// Custom category named exactly like the PFC enum string
	customCats := []*domain.CustomCategory{
		{ID: "cat-1", UserID: userID, Name: "FOOD_AND_DRINK"},
	}
	mockCategoryRepo.On("GetByUserID", ctx, userID).Return(customCats, nil)

	categories, err := service.GetAvailableCategories(ctx, userID)

	assert.NoError(t, err)
	// Custom entry stays
	assert.Contains(t, categories, "FOOD_AND_DRINK")
	// Curated enum must NOT appear as a second entry
	occurrences := 0
	for _, c := range categories {
		if c == "FOOD_AND_DRINK" {
			occurrences++
		}
	}
	assert.Equal(t, 1, occurrences, "FOOD_AND_DRINK should appear exactly once")
}

// ============================================================================
// ExpectedIncome / Amount Reconciliation Tests (Issue #1048)
// ============================================================================

func TestBudgetService_CreateBudget_ExpectedIncomeWins(t *testing.T) {
	mockBudgetRepo := new(MockBudgetRepository)
	mockCategoryRepo := new(MockCustomCategoryRepository)
	mockTransactionRepo := new(MockTransactionRepositoryForBudget)

	service := NewBudgetService(mockBudgetRepo, mockCategoryRepo, mockTransactionRepo, testLogger())

	ctx := context.Background()
	userID := "user-123"

	expectedIncome := 2000.0
	req := &domain.CreateBudgetRequest{
		Name:           "Monthly Budget",
		Period:         domain.BudgetPeriodMonthly,
		StartDate:      domain.FlexibleDate(time.Now()),
		Amount:         1000.0,
		CurrencyCode:   "USD",
		ExpectedIncome: &expectedIncome,
	}

	mockBudgetRepo.On("CreateWithCategories", ctx, mock.AnythingOfType("*domain.Budget"), mock.Anything).Return(nil)

	budget, err := service.CreateBudget(ctx, userID, req)

	assert.NoError(t, err)
	assert.NotNil(t, budget)
	assert.Equal(t, 2000.0, budget.ExpectedIncome, "expected_income should win over amount")
	assert.Equal(t, 2000.0, budget.Amount, "amount should mirror expected_income")
	mockBudgetRepo.AssertExpectations(t)
}

func TestBudgetService_CreateBudget_AmountFallback(t *testing.T) {
	mockBudgetRepo := new(MockBudgetRepository)
	mockCategoryRepo := new(MockCustomCategoryRepository)
	mockTransactionRepo := new(MockTransactionRepositoryForBudget)

	service := NewBudgetService(mockBudgetRepo, mockCategoryRepo, mockTransactionRepo, testLogger())

	ctx := context.Background()
	userID := "user-123"

	req := &domain.CreateBudgetRequest{
		Name:         "Monthly Budget",
		Period:       domain.BudgetPeriodMonthly,
		StartDate:    domain.FlexibleDate(time.Now()),
		Amount:       1000.0,
		CurrencyCode: "USD",
		// ExpectedIncome is nil
	}

	mockBudgetRepo.On("CreateWithCategories", ctx, mock.AnythingOfType("*domain.Budget"), mock.Anything).Return(nil)

	budget, err := service.CreateBudget(ctx, userID, req)

	assert.NoError(t, err)
	assert.NotNil(t, budget)
	assert.Equal(t, 1000.0, budget.ExpectedIncome, "expected_income should fall back to amount")
	assert.Equal(t, 1000.0, budget.Amount, "amount should remain unchanged")
	mockBudgetRepo.AssertExpectations(t)
}

func TestBudgetService_CreateBudget_ExpectedIncomeOnly(t *testing.T) {
	mockBudgetRepo := new(MockBudgetRepository)
	mockCategoryRepo := new(MockCustomCategoryRepository)
	mockTransactionRepo := new(MockTransactionRepositoryForBudget)

	service := NewBudgetService(mockBudgetRepo, mockCategoryRepo, mockTransactionRepo, testLogger())

	ctx := context.Background()
	userID := "user-123"

	expectedIncome := 3000.0
	req := &domain.CreateBudgetRequest{
		Name:           "Monthly Budget",
		Period:         domain.BudgetPeriodMonthly,
		StartDate:      domain.FlexibleDate(time.Now()),
		Amount:         3000.0, // must be positive to pass validation
		CurrencyCode:   "USD",
		ExpectedIncome: &expectedIncome,
	}

	mockBudgetRepo.On("CreateWithCategories", ctx, mock.AnythingOfType("*domain.Budget"), mock.Anything).Return(nil)

	budget, err := service.CreateBudget(ctx, userID, req)

	assert.NoError(t, err)
	assert.NotNil(t, budget)
	assert.Equal(t, 3000.0, budget.ExpectedIncome)
	assert.Equal(t, 3000.0, budget.Amount)
	mockBudgetRepo.AssertExpectations(t)
}

// TestBudgetService_CreateBudget_RejectsZeroAmount tests that amount=0 (which
// also results in expected_income=0 after reconciliation) is rejected by Validate() (R19b).
func TestBudgetService_CreateBudget_RejectsZeroAmount(t *testing.T) {
	mockBudgetRepo := new(MockBudgetRepository)
	mockCategoryRepo := new(MockCustomCategoryRepository)
	mockTransactionRepo := new(MockTransactionRepositoryForBudget)

	service := NewBudgetService(mockBudgetRepo, mockCategoryRepo, mockTransactionRepo, testLogger())

	ctx := context.Background()
	userID := "user-123"

	req := &domain.CreateBudgetRequest{
		Name:         "Budget",
		Period:       domain.BudgetPeriodMonthly,
		StartDate:    domain.FlexibleDate(time.Now()),
		Amount:       0, // triggers "amount must be positive"
		CurrencyCode: "USD",
	}

	budget, err := service.CreateBudget(ctx, userID, req)

	assert.Error(t, err, "zero amount must be rejected by Validate()")
	assert.Nil(t, budget)
	assert.Contains(t, err.Error(), "amount must be positive")
}

// M22 — from_budget_id → inherited series_id (Issue #1300 RG3)

// TestBudgetService_CreateBudget_StandaloneSeriesID verifies standalone budget gets series_id == id
func TestBudgetService_CreateBudget_StandaloneSeriesID(t *testing.T) {
	mockBudgetRepo := new(MockBudgetRepository)
	mockCategoryRepo := new(MockCustomCategoryRepository)
	mockTransactionRepo := new(MockTransactionRepositoryForBudget)

	service := NewBudgetService(mockBudgetRepo, mockCategoryRepo, mockTransactionRepo, testLogger())

	ctx := context.Background()
	userID := "user-123"
	req := createTestCreateBudgetRequest()
	// FromBudgetID is nil (standalone)

	mockBudgetRepo.On("CreateWithCategories", ctx, mock.AnythingOfType("*domain.Budget"), mock.Anything).Return(nil)

	budget, err := service.CreateBudget(ctx, userID, req)

	assert.NoError(t, err)
	assert.NotNil(t, budget)
	assert.Equal(t, budget.ID, budget.SeriesID, "standalone budget must be its own series")
	// GetByID must NOT be called when FromBudgetID is nil.
	mockBudgetRepo.AssertNotCalled(t, "GetByID", mock.Anything, mock.Anything)
	mockBudgetRepo.AssertExpectations(t)
}

// TestBudgetService_CreateBudget_InheritsSeriesIDFromAncestor verifies budget inherits ancestor series_id
func TestBudgetService_CreateBudget_InheritsSeriesIDFromAncestor(t *testing.T) {
	mockBudgetRepo := new(MockBudgetRepository)
	mockCategoryRepo := new(MockCustomCategoryRepository)
	mockTransactionRepo := new(MockTransactionRepositoryForBudget)

	service := NewBudgetService(mockBudgetRepo, mockCategoryRepo, mockTransactionRepo, testLogger())

	ctx := context.Background()
	userID := "user-123"
	ancestorID := "ancestor-budget-id"
	ancestorSeriesID := "series-abc-123"

	ancestor := createTestBudgetForService(userID)
	ancestor.ID = ancestorID
	ancestor.SeriesID = ancestorSeriesID

	fromBudgetID := ancestorID
	req := createTestCreateBudgetRequest()
	req.FromBudgetID = &fromBudgetID

	mockBudgetRepo.On("GetByID", ctx, ancestorID).Return(ancestor, nil)
	mockBudgetRepo.On("CreateWithCategories", ctx, mock.AnythingOfType("*domain.Budget"), mock.Anything).Return(nil)

	budget, err := service.CreateBudget(ctx, userID, req)

	assert.NoError(t, err)
	assert.NotNil(t, budget)
	assert.Equal(t, ancestorSeriesID, budget.SeriesID, "budget must inherit ancestor's series_id")
	mockBudgetRepo.AssertExpectations(t)
}

// TestBudgetService_CreateBudget_CrossUserFromBudgetIDRejected verifies cross-user from_budget_id is rejected
func TestBudgetService_CreateBudget_CrossUserFromBudgetIDRejected(t *testing.T) {
	mockBudgetRepo := new(MockBudgetRepository)
	mockCategoryRepo := new(MockCustomCategoryRepository)
	mockTransactionRepo := new(MockTransactionRepositoryForBudget)

	service := NewBudgetService(mockBudgetRepo, mockCategoryRepo, mockTransactionRepo, testLogger())

	ctx := context.Background()
	userID := "user-123"
	otherUserID := "other-user-456"
	otherBudgetID := "other-user-budget-id"

	otherBudget := createTestBudgetForService(otherUserID) // different user
	otherBudget.ID = otherBudgetID

	fromBudgetID := otherBudgetID
	req := createTestCreateBudgetRequest()
	req.FromBudgetID = &fromBudgetID

	mockBudgetRepo.On("GetByID", ctx, otherBudgetID).Return(otherBudget, nil)

	budget, err := service.CreateBudget(ctx, userID, req)

	assert.ErrorIs(t, err, domain.ErrBudgetNotFound, "cross-user from_budget_id must return ErrBudgetNotFound")
	assert.Nil(t, budget)
	// CreateWithCategories must NOT be called.
	mockBudgetRepo.AssertNotCalled(t, "CreateWithCategories", mock.Anything, mock.Anything, mock.Anything)
	mockBudgetRepo.AssertExpectations(t)
}

func TestBudgetService_UpdateBudget_ExpectedIncomeUpdates(t *testing.T) {
	mockBudgetRepo := new(MockBudgetRepository)
	mockCategoryRepo := new(MockCustomCategoryRepository)
	mockTransactionRepo := new(MockTransactionRepositoryForBudget)

	service := NewBudgetService(mockBudgetRepo, mockCategoryRepo, mockTransactionRepo, testLogger())

	ctx := context.Background()
	userID := "user-123"
	budgetID := "budget-123"

	existingBudget := createTestBudgetForService(userID)

	expectedIncome := 4000.0
	req := &domain.UpdateBudgetRequest{
		ExpectedIncome: &expectedIncome,
	}

	mockBudgetRepo.On("GetByID", ctx, budgetID).Return(existingBudget, nil)
	mockBudgetRepo.On("Update", ctx, mock.AnythingOfType("*domain.Budget")).Return(nil)

	budget, err := service.UpdateBudget(ctx, userID, budgetID, req)

	assert.NoError(t, err)
	assert.NotNil(t, budget)
	assert.Equal(t, 4000.0, budget.ExpectedIncome, "expected_income should be updated")
	assert.Equal(t, 4000.0, budget.Amount, "amount should mirror expected_income")
	mockBudgetRepo.AssertExpectations(t)
}

func TestBudgetService_UpdateBudget_AmountFallback(t *testing.T) {
	mockBudgetRepo := new(MockBudgetRepository)
	mockCategoryRepo := new(MockCustomCategoryRepository)
	mockTransactionRepo := new(MockTransactionRepositoryForBudget)

	service := NewBudgetService(mockBudgetRepo, mockCategoryRepo, mockTransactionRepo, testLogger())

	ctx := context.Background()
	userID := "user-123"
	budgetID := "budget-123"

	existingBudget := createTestBudgetForService(userID)

	newAmount := 500.0
	req := &domain.UpdateBudgetRequest{
		Amount: &newAmount,
		// ExpectedIncome is nil
	}

	mockBudgetRepo.On("GetByID", ctx, budgetID).Return(existingBudget, nil)
	mockBudgetRepo.On("Update", ctx, mock.AnythingOfType("*domain.Budget")).Return(nil)

	budget, err := service.UpdateBudget(ctx, userID, budgetID, req)

	assert.NoError(t, err)
	assert.NotNil(t, budget)
	assert.Equal(t, 500.0, budget.Amount, "amount should be updated")
	assert.Equal(t, 500.0, budget.ExpectedIncome, "expected_income should mirror amount")
	mockBudgetRepo.AssertExpectations(t)
}

// PFC Display Enrichment Tests

// TestBudgetService_GetBudget_PopulatesPFCDisplay asserts GetBudget enriches PFC display names
func TestBudgetService_GetBudget_PopulatesPFCDisplay(t *testing.T) {
	mockBudgetRepo := new(MockBudgetRepository)
	mockCategoryRepo := new(MockCustomCategoryRepository)
	mockTransactionRepo := new(MockTransactionRepositoryForBudget)

	service := NewBudgetService(mockBudgetRepo, mockCategoryRepo, mockTransactionRepo, testLogger())

	ctx := context.Background()
	userID := "user-123"
	budgetID := "budget-pfc"

	budget := createTestBudgetForService(userID)
	budget.ID = budgetID
	budget.Categories = []domain.BudgetCategory{
		{ID: "cat-1", BudgetID: budgetID, CategoryKind: "pfc", PFCPrimary: strPtr("FOOD_AND_DRINK")},
	}

	mockBudgetRepo.On("GetByID", ctx, budgetID).Return(budget, nil)

	result, err := service.GetBudget(ctx, userID, budgetID)

	require.NoError(t, err)
	require.Len(t, result.Categories, 1)
	require.NotNil(t, result.Categories[0].PFCPrimaryDisplay, "PFCPrimaryDisplay should be populated")
	assert.Equal(t, "Food And Drink", *result.Categories[0].PFCPrimaryDisplay)
	mockBudgetRepo.AssertExpectations(t)
}

// TestBudgetService_AddCategory_PopulatesPFCDisplay asserts AddCategory enriches PFC display names
func TestBudgetService_AddCategory_PopulatesPFCDisplay(t *testing.T) {
	mockBudgetRepo := new(MockBudgetRepository)
	mockCategoryRepo := new(MockCustomCategoryRepository)
	mockTransactionRepo := new(MockTransactionRepositoryForBudget)

	service := NewBudgetService(mockBudgetRepo, mockCategoryRepo, mockTransactionRepo, testLogger())

	ctx := context.Background()
	userID := "user-123"
	budgetID := "budget-123"

	existingBudget := createTestBudgetForService(userID)

	req := &domain.CreateBudgetCategoryRequest{
		CategoryKind:    "pfc",
		PFCPrimary:      strPtr("FOOD_AND_DRINK"),
		AllocatedAmount: 200.00,
	}

	mockBudgetRepo.On("GetByID", ctx, budgetID).Return(existingBudget, nil)
	mockBudgetRepo.On("AddCategoryChecked", ctx, budgetID, mock.AnythingOfType("*domain.BudgetCategory")).Return(nil, nil)

	category, err := service.AddCategory(ctx, userID, budgetID, req)

	require.NoError(t, err)
	require.NotNil(t, category.PFCPrimaryDisplay, "PFCPrimaryDisplay should be populated")
	assert.Equal(t, "Food And Drink", *category.PFCPrimaryDisplay)
	mockBudgetRepo.AssertExpectations(t)
}

// strPtr helper is defined in rule_application_service_test.go

// MockActiveSavingsGoalRepo — narrow interface mock for carry-forward / backfill

type MockActiveSavingsGoalRepo struct {
	mock.Mock
}

func (m *MockActiveSavingsGoalRepo) GetActiveByUserID(ctx context.Context, userID string) ([]*domain.SavingsGoal, error) {
	args := m.Called(ctx, userID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]*domain.SavingsGoal), args.Error(1)
}

// CreateBudget carry-forward tests (Issue #1300)

func TestBudgetService_CreateBudget_CarriesForwardSavingsCategories(t *testing.T) {
	t.Run("creates savings category for each non-tombstoned active goal", func(t *testing.T) {
		ctx := context.Background()
		userID := "user-123"

		mockBudgetRepo := new(MockBudgetRepository)
		mockCategoryRepo := new(MockCustomCategoryRepository)
		mockTransactionRepo := new(MockTransactionRepositoryForBudget)
		mockGoalRepo := new(MockActiveSavingsGoalRepo)

		svc := NewBudgetService(mockBudgetRepo, mockCategoryRepo, mockTransactionRepo, testLogger())
		svc.SetGoalRepo(mockGoalRepo)

		req := &domain.CreateBudgetRequest{
			Name:         "Monthly Budget",
			Period:       domain.BudgetPeriodMonthly,
			Amount:       3000.00,
			CurrencyCode: "USD",
			StartDate:    domain.FlexibleDate(time.Now()),
		}

		goalID1 := "goal-1"
		goalID2 := "goal-2"

		activeGoals := []*domain.SavingsGoal{
			{ID: goalID1, UserID: userID, Name: "Vacation"},
			{ID: goalID2, UserID: userID, Name: "Emergency Fund"},
		}

		// Budget Create + GetActiveByUserID for goals
		mockBudgetRepo.On("CreateWithCategories", ctx, mock.AnythingOfType("*domain.Budget"), mock.Anything).Return(nil)
		mockGoalRepo.On("GetActiveByUserID", ctx, userID).Return(activeGoals, nil)
		// No tombstones for the new budget
		mockBudgetRepo.On("IsGoalDetachedForSeries", ctx, mock.AnythingOfType("string"), goalID1).Return(false, nil)
		mockBudgetRepo.On("IsGoalDetachedForSeries", ctx, mock.AnythingOfType("string"), goalID2).Return(false, nil)
		// CreateCategory called once per goal (savings kind)
		mockBudgetRepo.On("CreateCategory", ctx, mock.MatchedBy(func(c *domain.BudgetCategory) bool {
			return c.CategoryKind == "savings" && c.AutoCreated && c.SavingsGoalID != nil && *c.SavingsGoalID == goalID1
		})).Return(nil)
		mockBudgetRepo.On("CreateCategory", ctx, mock.MatchedBy(func(c *domain.BudgetCategory) bool {
			return c.CategoryKind == "savings" && c.AutoCreated && c.SavingsGoalID != nil && *c.SavingsGoalID == goalID2
		})).Return(nil)

		budget, err := svc.CreateBudget(ctx, userID, req)

		require.NoError(t, err)
		assert.NotNil(t, budget)
		mockBudgetRepo.AssertExpectations(t)
		mockGoalRepo.AssertExpectations(t)
	})

	t.Run("skips tombstoned goal during carry-forward", func(t *testing.T) {
		ctx := context.Background()
		userID := "user-123"

		mockBudgetRepo := new(MockBudgetRepository)
		mockCategoryRepo := new(MockCustomCategoryRepository)
		mockTransactionRepo := new(MockTransactionRepositoryForBudget)
		mockGoalRepo := new(MockActiveSavingsGoalRepo)

		svc := NewBudgetService(mockBudgetRepo, mockCategoryRepo, mockTransactionRepo, testLogger())
		svc.SetGoalRepo(mockGoalRepo)

		req := &domain.CreateBudgetRequest{
			Name:         "Monthly Budget",
			Period:       domain.BudgetPeriodMonthly,
			Amount:       3000.00,
			CurrencyCode: "USD",
			StartDate:    domain.FlexibleDate(time.Now()),
		}

		goalID1 := "goal-1"
		goalIDTombstoned := "goal-tombstoned"

		activeGoals := []*domain.SavingsGoal{
			{ID: goalID1, UserID: userID, Name: "Vacation"},
			{ID: goalIDTombstoned, UserID: userID, Name: "Old Goal"},
		}

		mockBudgetRepo.On("CreateWithCategories", ctx, mock.AnythingOfType("*domain.Budget"), mock.Anything).Return(nil)
		mockGoalRepo.On("GetActiveByUserID", ctx, userID).Return(activeGoals, nil)
		// Tombstone check is keyed on series_id (set to budget.ID in the service for new budgets).
		mockBudgetRepo.On("IsGoalDetachedForSeries", ctx, mock.AnythingOfType("string"), goalID1).Return(false, nil)
		mockBudgetRepo.On("IsGoalDetachedForSeries", ctx, mock.AnythingOfType("string"), goalIDTombstoned).Return(true, nil)
		// Only goalID1 gets a category; tombstoned goal is skipped
		mockBudgetRepo.On("CreateCategory", ctx, mock.MatchedBy(func(c *domain.BudgetCategory) bool {
			return c.CategoryKind == "savings" && *c.SavingsGoalID == goalID1
		})).Return(nil)

		budget, err := svc.CreateBudget(ctx, userID, req)

		require.NoError(t, err)
		assert.NotNil(t, budget)
		mockBudgetRepo.AssertExpectations(t)
	})

	t.Run("no carry-forward when goalRepo not set (backward compatible)", func(t *testing.T) {
		ctx := context.Background()
		userID := "user-123"

		mockBudgetRepo := new(MockBudgetRepository)
		mockCategoryRepo := new(MockCustomCategoryRepository)
		mockTransactionRepo := new(MockTransactionRepositoryForBudget)

		svc := NewBudgetService(mockBudgetRepo, mockCategoryRepo, mockTransactionRepo, testLogger())
		// No SetGoalRepo call

		req := &domain.CreateBudgetRequest{
			Name:         "Monthly Budget",
			Period:       domain.BudgetPeriodMonthly,
			Amount:       3000.00,
			CurrencyCode: "USD",
			StartDate:    domain.FlexibleDate(time.Now()),
		}

		mockBudgetRepo.On("CreateWithCategories", ctx, mock.AnythingOfType("*domain.Budget"), mock.Anything).Return(nil)
		// CreateCategory should NOT be called for savings (no goals fetched)

		budget, err := svc.CreateBudget(ctx, userID, req)

		require.NoError(t, err)
		assert.NotNil(t, budget)
		mockBudgetRepo.AssertExpectations(t)
	})
}

// DeleteCategory tombstone tests (Issue #1300, Codex C6)

func TestBudgetService_DeleteCategory_AutoCreatedSavings_WritesTombstone(t *testing.T) {
	ctx := context.Background()
	userID := "user-123"
	budgetID := "budget-123"
	categoryID := "cat-savings-1"
	goalID := "goal-1"

	mockBudgetRepo := new(MockBudgetRepository)
	mockCategoryRepo := new(MockCustomCategoryRepository)
	mockTransactionRepo := new(MockTransactionRepositoryForBudget)

	svc := NewBudgetService(mockBudgetRepo, mockCategoryRepo, mockTransactionRepo, testLogger())

	savingsCat := domain.BudgetCategory{
		ID:            categoryID,
		BudgetID:      budgetID,
		CategoryKind:  "savings",
		SavingsGoalID: func() *string { s := goalID; return &s }(),
		AutoCreated:   true,
	}
	budget := createTestBudgetForService(userID)
	budget.ID = budgetID
	budget.SeriesID = budgetID // standalone budget is its own series
	budget.Categories = []domain.BudgetCategory{savingsCat}

	mockBudgetRepo.On("GetByID", ctx, budgetID).Return(budget, nil)
	// InsertGoalDetach now takes (seriesID, budgetID, goalID) — series_id = budget.SeriesID = budgetID for standalone.
	mockBudgetRepo.On("InsertGoalDetach", ctx, budgetID, budgetID, goalID).Return(nil)
	mockBudgetRepo.On("DeleteCategory", ctx, categoryID, budgetID).Return(nil)

	err := svc.DeleteCategory(ctx, userID, budgetID, categoryID)

	require.NoError(t, err)
	mockBudgetRepo.AssertExpectations(t)
}

func TestBudgetService_DeleteCategory_ManualSavings_NoTombstone(t *testing.T) {
	ctx := context.Background()
	userID := "user-123"
	budgetID := "budget-123"
	categoryID := "cat-savings-manual"
	goalID := "goal-1"

	mockBudgetRepo := new(MockBudgetRepository)
	mockCategoryRepo := new(MockCustomCategoryRepository)
	mockTransactionRepo := new(MockTransactionRepositoryForBudget)

	svc := NewBudgetService(mockBudgetRepo, mockCategoryRepo, mockTransactionRepo, testLogger())

	// manual savings category (AutoCreated = false)
	savingsCat := domain.BudgetCategory{
		ID:            categoryID,
		BudgetID:      budgetID,
		CategoryKind:  "savings",
		SavingsGoalID: func() *string { s := goalID; return &s }(),
		AutoCreated:   false, // user-created, no tombstone
	}
	budget := createTestBudgetForService(userID)
	budget.ID = budgetID
	budget.Categories = []domain.BudgetCategory{savingsCat}

	mockBudgetRepo.On("GetByID", ctx, budgetID).Return(budget, nil)
	mockBudgetRepo.On("DeleteCategory", ctx, categoryID, budgetID).Return(nil)
	// InsertGoalDetach must NOT be called for manual categories

	err := svc.DeleteCategory(ctx, userID, budgetID, categoryID)

	require.NoError(t, err)
	mockBudgetRepo.AssertExpectations(t)
}

// BackfillManagedCategories tests (Issue #1300, Codex C7)

func TestBudgetService_BackfillManagedCategories(t *testing.T) {
	t.Run("creates missing savings categories for active goals x active budgets", func(t *testing.T) {
		ctx := context.Background()
		userID := "user-123"

		mockBudgetRepo := new(MockBudgetRepository)
		mockCategoryRepo := new(MockCustomCategoryRepository)
		mockTransactionRepo := new(MockTransactionRepositoryForBudget)
		mockGoalRepo := new(MockActiveSavingsGoalRepo)

		svc := NewBudgetService(mockBudgetRepo, mockCategoryRepo, mockTransactionRepo, testLogger())
		svc.SetGoalRepo(mockGoalRepo)

		budgetID := "budget-1"
		goalID := "goal-1"

		activeBudgets := []*domain.Budget{
			// SeriesID == ID for a standalone budget.
			{ID: budgetID, SeriesID: budgetID, UserID: userID, IsActive: true, Categories: []domain.BudgetCategory{}},
		}
		activeGoals := []*domain.SavingsGoal{
			{ID: goalID, UserID: userID, Name: "Vacation"},
		}

		mockBudgetRepo.On("GetActiveByUserID", ctx, userID).Return(activeBudgets, nil)
		mockGoalRepo.On("GetActiveByUserID", ctx, userID).Return(activeGoals, nil)
		// Tombstone check keyed on series_id (= budgetID for standalone budgets).
		mockBudgetRepo.On("IsGoalDetachedForSeries", ctx, budgetID, goalID).Return(false, nil)
		// Missing category → CreateCategory called
		mockBudgetRepo.On("CreateCategory", ctx, mock.MatchedBy(func(c *domain.BudgetCategory) bool {
			return c.CategoryKind == "savings" && c.AutoCreated &&
				c.SavingsGoalID != nil && *c.SavingsGoalID == goalID &&
				c.BudgetID == budgetID
		})).Return(nil)

		err := svc.BackfillManagedCategories(ctx, userID)

		require.NoError(t, err)
		mockBudgetRepo.AssertExpectations(t)
		mockGoalRepo.AssertExpectations(t)
	})

	t.Run("skips already-existing savings categories (idempotent)", func(t *testing.T) {
		ctx := context.Background()
		userID := "user-123"

		mockBudgetRepo := new(MockBudgetRepository)
		mockCategoryRepo := new(MockCustomCategoryRepository)
		mockTransactionRepo := new(MockTransactionRepositoryForBudget)
		mockGoalRepo := new(MockActiveSavingsGoalRepo)

		svc := NewBudgetService(mockBudgetRepo, mockCategoryRepo, mockTransactionRepo, testLogger())
		svc.SetGoalRepo(mockGoalRepo)

		budgetID := "budget-1"
		goalID := "goal-1"

		// Budget already has a savings category for this goal
		existingCat := domain.BudgetCategory{
			ID:            "cat-existing",
			BudgetID:      budgetID,
			CategoryKind:  "savings",
			SavingsGoalID: func() *string { s := goalID; return &s }(),
		}
		activeBudgets := []*domain.Budget{
			{ID: budgetID, SeriesID: budgetID, UserID: userID, IsActive: true, Categories: []domain.BudgetCategory{existingCat}},
		}
		activeGoals := []*domain.SavingsGoal{
			{ID: goalID, UserID: userID, Name: "Vacation"},
		}

		mockBudgetRepo.On("GetActiveByUserID", ctx, userID).Return(activeBudgets, nil)
		mockGoalRepo.On("GetActiveByUserID", ctx, userID).Return(activeGoals, nil)
		// IsGoalDetachedForSeries must NOT be called (existing category found first)
		// CreateCategory must NOT be called

		err := svc.BackfillManagedCategories(ctx, userID)

		require.NoError(t, err)
		mockBudgetRepo.AssertExpectations(t)
	})

	t.Run("skips tombstoned (budget, goal) pairs", func(t *testing.T) {
		ctx := context.Background()
		userID := "user-123"

		mockBudgetRepo := new(MockBudgetRepository)
		mockCategoryRepo := new(MockCustomCategoryRepository)
		mockTransactionRepo := new(MockTransactionRepositoryForBudget)
		mockGoalRepo := new(MockActiveSavingsGoalRepo)

		svc := NewBudgetService(mockBudgetRepo, mockCategoryRepo, mockTransactionRepo, testLogger())
		svc.SetGoalRepo(mockGoalRepo)

		budgetID := "budget-1"
		goalID := "goal-1"

		activeBudgets := []*domain.Budget{
			{ID: budgetID, SeriesID: budgetID, UserID: userID, IsActive: true, Categories: []domain.BudgetCategory{}},
		}
		activeGoals := []*domain.SavingsGoal{
			{ID: goalID, UserID: userID, Name: "Old Goal"},
		}

		mockBudgetRepo.On("GetActiveByUserID", ctx, userID).Return(activeBudgets, nil)
		mockGoalRepo.On("GetActiveByUserID", ctx, userID).Return(activeGoals, nil)
		// Tombstone exists for this series → skip
		mockBudgetRepo.On("IsGoalDetachedForSeries", ctx, budgetID, goalID).Return(true, nil)
		// CreateCategory must NOT be called

		err := svc.BackfillManagedCategories(ctx, userID)

		require.NoError(t, err)
		mockBudgetRepo.AssertExpectations(t)
	})

	t.Run("no-op when goalRepo not set", func(t *testing.T) {
		ctx := context.Background()
		userID := "user-123"

		mockBudgetRepo := new(MockBudgetRepository)
		mockCategoryRepo := new(MockCustomCategoryRepository)
		mockTransactionRepo := new(MockTransactionRepositoryForBudget)

		svc := NewBudgetService(mockBudgetRepo, mockCategoryRepo, mockTransactionRepo, testLogger())
		// No SetGoalRepo call

		err := svc.BackfillManagedCategories(ctx, userID)

		require.NoError(t, err)
		mockBudgetRepo.AssertExpectations(t)
	})
}

// M15 (Issue #1300 G5): detach persists across series periods (keyed on series_id, not budget_id)

func TestBudgetService_DeleteCategory_DetachTombstoneKeyedBySeries(t *testing.T) {
	t.Run("tombstone uses budget.SeriesID not budget.ID", func(t *testing.T) {
		ctx := context.Background()
		userID := "user-123"
		budgetID := "budget-period-A"
		seriesID := "series-shared" // series that spans multiple periods
		categoryID := "cat-savings-1"
		goalID := "goal-1"

		mockBudgetRepo := new(MockBudgetRepository)
		mockCategoryRepo := new(MockCustomCategoryRepository)
		mockTransactionRepo := new(MockTransactionRepositoryForBudget)

		svc := NewBudgetService(mockBudgetRepo, mockCategoryRepo, mockTransactionRepo, testLogger())

		savingsCat := domain.BudgetCategory{
			ID:            categoryID,
			BudgetID:      budgetID,
			CategoryKind:  "savings",
			SavingsGoalID: func() *string { s := goalID; return &s }(),
			AutoCreated:   true,
		}
		// Budget belongs to a shared series (period A of a recurring budget).
		budget := createTestBudgetForService(userID)
		budget.ID = budgetID
		budget.SeriesID = seriesID // <-- not equal to budget.ID
		budget.Categories = []domain.BudgetCategory{savingsCat}

		mockBudgetRepo.On("GetByID", ctx, budgetID).Return(budget, nil)
		// InsertGoalDetach must be called with seriesID (not budgetID) as the first arg.
		mockBudgetRepo.On("InsertGoalDetach", ctx, seriesID, budgetID, goalID).Return(nil)
		mockBudgetRepo.On("DeleteCategory", ctx, categoryID, budgetID).Return(nil)

		err := svc.DeleteCategory(ctx, userID, budgetID, categoryID)

		require.NoError(t, err)
		mockBudgetRepo.AssertExpectations(t)
	})
}

func TestBudgetService_CarryForward_DetachPersistsAcrossSeriesPeriods(t *testing.T) {
	t.Run("goal tombstoned for series not re-added in a new period of the same series", func(t *testing.T) {
		ctx := context.Background()
		userID := "user-123"
		seriesID := "series-shared"
		budgetIDPeriodB := "budget-period-B"
		goalIDDetached := "goal-detached"
		goalIDOther := "goal-other"

		mockBudgetRepo := new(MockBudgetRepository)
		mockCategoryRepo := new(MockCustomCategoryRepository)
		mockTransactionRepo := new(MockTransactionRepositoryForBudget)
		mockGoalRepo := new(MockActiveSavingsGoalRepo)

		svc := NewBudgetService(mockBudgetRepo, mockCategoryRepo, mockTransactionRepo, testLogger())
		svc.SetGoalRepo(mockGoalRepo)

		req := &domain.CreateBudgetRequest{
			Name:         "Monthly Budget — Period B",
			Period:       domain.BudgetPeriodMonthly,
			Amount:       3000.00,
			CurrencyCode: "USD",
			StartDate:    domain.FlexibleDate(time.Now()),
		}

		activeGoals := []*domain.SavingsGoal{
			{ID: goalIDDetached, UserID: userID, Name: "Detached Goal"},
			{ID: goalIDOther, UserID: userID, Name: "Other Goal"},
		}

		mockBudgetRepo.On("CreateWithCategories", ctx, mock.AnythingOfType("*domain.Budget"), mock.Anything).Return(nil)
		mockGoalRepo.On("GetActiveByUserID", ctx, userID).Return(activeGoals, nil)
		// Detached goal is tombstoned for this series — skip it.
		mockBudgetRepo.On("IsGoalDetachedForSeries", ctx, mock.AnythingOfType("string"), goalIDDetached).Return(true, nil)
		// Other goal is not tombstoned — add it.
		mockBudgetRepo.On("IsGoalDetachedForSeries", ctx, mock.AnythingOfType("string"), goalIDOther).Return(false, nil)
		// Only the non-tombstoned goal gets a category.
		mockBudgetRepo.On("CreateCategory", ctx, mock.MatchedBy(func(c *domain.BudgetCategory) bool {
			return c.CategoryKind == "savings" && c.AutoCreated &&
				c.SavingsGoalID != nil && *c.SavingsGoalID == goalIDOther
		})).Return(nil)

		budget, err := svc.CreateBudget(ctx, userID, req)

		require.NoError(t, err)
		require.NotNil(t, budget)
		// The created budget must have a series_id set (standalone new budget → its own series).
		assert.NotEmpty(t, budget.SeriesID)
		mockBudgetRepo.AssertExpectations(t)
		mockGoalRepo.AssertExpectations(t)

		// Demonstrate that the series concept works: if the budget HAD inherited seriesID
		// then tombstone for goalIDDetached under that series would still prevent re-creation.
		// That inheritance path is tested via BackfillManagedCategories (seriesID passed in
		// from an existing budget record), not via CreateBudget (which always creates a new
		// standalone series in the current API).
		_ = seriesID
		_ = budgetIDPeriodB
	})

	t.Run("BackfillManagedCategories skips detached goal across budgets sharing series", func(t *testing.T) {
		ctx := context.Background()
		userID := "user-123"
		seriesID := "series-shared"
		budgetA := "budget-period-A"
		budgetB := "budget-period-B"
		goalIDDetached := "goal-detached"
		goalIDOther := "goal-other"

		mockBudgetRepo := new(MockBudgetRepository)
		mockCategoryRepo := new(MockCustomCategoryRepository)
		mockTransactionRepo := new(MockTransactionRepositoryForBudget)
		mockGoalRepo := new(MockActiveSavingsGoalRepo)

		svc := NewBudgetService(mockBudgetRepo, mockCategoryRepo, mockTransactionRepo, testLogger())
		svc.SetGoalRepo(mockGoalRepo)

		// Two budgets in the same series (e.g. Jan and Feb). goalIDDetached was detached.
		activeBudgets := []*domain.Budget{
			{ID: budgetA, SeriesID: seriesID, UserID: userID, IsActive: true, Categories: []domain.BudgetCategory{}},
			{ID: budgetB, SeriesID: seriesID, UserID: userID, IsActive: true, Categories: []domain.BudgetCategory{}},
		}
		activeGoals := []*domain.SavingsGoal{
			{ID: goalIDDetached, UserID: userID, Name: "Detached"},
			{ID: goalIDOther, UserID: userID, Name: "Other"},
		}

		mockBudgetRepo.On("GetActiveByUserID", ctx, userID).Return(activeBudgets, nil)
		mockGoalRepo.On("GetActiveByUserID", ctx, userID).Return(activeGoals, nil)

		// For both budgets, goalIDDetached is tombstoned for their shared series.
		mockBudgetRepo.On("IsGoalDetachedForSeries", ctx, seriesID, goalIDDetached).Return(true, nil).Times(2)
		// goalIDOther is not tombstoned — gets a category in each budget.
		mockBudgetRepo.On("IsGoalDetachedForSeries", ctx, seriesID, goalIDOther).Return(false, nil).Times(2)
		mockBudgetRepo.On("CreateCategory", ctx, mock.MatchedBy(func(c *domain.BudgetCategory) bool {
			return c.CategoryKind == "savings" && c.AutoCreated &&
				c.SavingsGoalID != nil && *c.SavingsGoalID == goalIDOther &&
				(c.BudgetID == budgetA || c.BudgetID == budgetB)
		})).Return(nil).Times(2)

		err := svc.BackfillManagedCategories(ctx, userID)

		require.NoError(t, err)
		mockBudgetRepo.AssertExpectations(t)
		mockGoalRepo.AssertExpectations(t)
	})
}

// MockGoalOwnershipChecker — narrow interface mock for IDOR protection (Issue #1300, Codex C1)

type MockGoalOwnershipChecker struct {
	mock.Mock
}

func (m *MockGoalOwnershipChecker) GetByIDForUser(ctx context.Context, id, userID string) (*domain.SavingsGoal, error) {
	args := m.Called(ctx, id, userID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*domain.SavingsGoal), args.Error(1)
}

// AddCategory — savings kind tests (Issue #1300, Codex C1)

func TestBudgetService_AddCategory_SavingsKind(t *testing.T) {
	goalID := "goal-abc"
	budgetID := "budget-123"
	userID := "user-123"

	makeService := func() (*budgetService, *MockBudgetRepository) {
		mockBudgetRepo := new(MockBudgetRepository)
		mockCategoryRepo := new(MockCustomCategoryRepository)
		mockTransactionRepo := new(MockTransactionRepositoryForBudget)
		svc := NewBudgetService(mockBudgetRepo, mockCategoryRepo, mockTransactionRepo, testLogger())
		return svc.(*budgetService), mockBudgetRepo
	}

	t.Run("copies SavingsGoalID into created category", func(t *testing.T) {
		svc, mockBudgetRepo := makeService()
		ctx := context.Background()
		existingBudget := createTestBudgetForService(userID)

		req := &domain.CreateBudgetCategoryRequest{
			CategoryKind:    "savings",
			SavingsGoalID:   &goalID,
			AllocatedAmount: 100.00,
		}

		mockChecker := new(MockGoalOwnershipChecker)
		mockChecker.On("GetByIDForUser", ctx, goalID, userID).Return(&domain.SavingsGoal{ID: goalID, UserID: userID, Name: "Emergency Fund"}, nil)
		svc.SetGoalOwnershipChecker(mockChecker)

		mockBudgetRepo.On("GetByID", ctx, budgetID).Return(existingBudget, nil)
		mockBudgetRepo.On("AddCategoryChecked", ctx, budgetID, mock.AnythingOfType("*domain.BudgetCategory")).Return(nil, nil)

		category, err := svc.AddCategory(ctx, userID, budgetID, req)

		require.NoError(t, err)
		require.NotNil(t, category)
		require.NotNil(t, category.SavingsGoalID, "SavingsGoalID must be set on returned category")
		assert.Equal(t, goalID, *category.SavingsGoalID)
		mockBudgetRepo.AssertExpectations(t)
		mockChecker.AssertExpectations(t)
	})

	t.Run("rejects savings category when goal does not belong to user (IDOR)", func(t *testing.T) {
		svc, mockBudgetRepo := makeService()
		ctx := context.Background()
		existingBudget := createTestBudgetForService(userID)

		req := &domain.CreateBudgetCategoryRequest{
			CategoryKind:    "savings",
			SavingsGoalID:   &goalID,
			AllocatedAmount: 100.00,
		}

		mockChecker := new(MockGoalOwnershipChecker)
		mockChecker.On("GetByIDForUser", ctx, goalID, userID).Return(nil, domain.ErrSavingsGoalNotFound)
		svc.SetGoalOwnershipChecker(mockChecker)

		mockBudgetRepo.On("GetByID", ctx, budgetID).Return(existingBudget, nil)
		// AddCategoryChecked must NOT be called

		_, err := svc.AddCategory(ctx, userID, budgetID, req)

		require.Error(t, err)
		assert.ErrorIs(t, err, domain.ErrSavingsGoalNotFound)
		mockBudgetRepo.AssertExpectations(t)
		mockChecker.AssertExpectations(t)
	})

	t.Run("succeeds with valid ownership", func(t *testing.T) {
		svc, mockBudgetRepo := makeService()
		ctx := context.Background()
		existingBudget := createTestBudgetForService(userID)

		req := &domain.CreateBudgetCategoryRequest{
			CategoryKind:    "savings",
			SavingsGoalID:   &goalID,
			AllocatedAmount: 200.00,
		}

		mockChecker := new(MockGoalOwnershipChecker)
		mockChecker.On("GetByIDForUser", ctx, goalID, userID).Return(&domain.SavingsGoal{ID: goalID, UserID: userID, Name: "Vacation"}, nil)
		svc.SetGoalOwnershipChecker(mockChecker)

		mockBudgetRepo.On("GetByID", ctx, budgetID).Return(existingBudget, nil)
		mockBudgetRepo.On("AddCategoryChecked", ctx, budgetID, mock.AnythingOfType("*domain.BudgetCategory")).Return(nil, nil)

		category, err := svc.AddCategory(ctx, userID, budgetID, req)

		require.NoError(t, err)
		require.NotNil(t, category)
		assert.Equal(t, "savings", category.CategoryKind)
		assert.Equal(t, 200.00, category.AllocatedAmount)
		mockBudgetRepo.AssertExpectations(t)
		mockChecker.AssertExpectations(t)
	})

	t.Run("returns error when no checker configured (fail-closed)", func(t *testing.T) {
		// When goalOwnershipChecker is nil, AddCategory must fail closed — not silently allow a
		// cross-user savings_goal_id. This was previously a WARN-and-continue (R5-1 Codex finding).
		svc, mockBudgetRepo := makeService()
		ctx := context.Background()
		existingBudget := createTestBudgetForService(userID)
		// No SetGoalOwnershipChecker call

		req := &domain.CreateBudgetCategoryRequest{
			CategoryKind:    "savings",
			SavingsGoalID:   &goalID,
			AllocatedAmount: 50.00,
		}

		mockBudgetRepo.On("GetByID", ctx, budgetID).Return(existingBudget, nil)

		category, err := svc.AddCategory(ctx, userID, budgetID, req)

		require.Error(t, err, "AddCategory with nil checker and a savings category must return an error (fail-closed)")
		assert.Nil(t, category)
		assert.Contains(t, err.Error(), "ownership checker not configured")
		// AddCategoryChecked must NOT be called — the IDOR check blocks the write.
		mockBudgetRepo.AssertNotCalled(t, "AddCategoryChecked")
	})
}

// =============================================================================
// R5-2: CreateBudget per-category domain validation (Issue #1300 Round-5)
// =============================================================================

// TestBudgetService_CreateBudget_ValidatesCategories verifies that CreateBudget calls
// domain.BudgetCategory.Validate() for each requested category before persisting anything
// (R5-2). This matches the behaviour of AddCategory / UpdateCategory and ensures the
// transactional persist (CreateWithCategories) is never reached when a category is invalid.
func TestBudgetService_CreateBudget_ValidatesCategories(t *testing.T) {
	makeService := func() (*budgetService, *MockBudgetRepository) {
		mockBudgetRepo := new(MockBudgetRepository)
		mockCategoryRepo := new(MockCustomCategoryRepository)
		mockTransactionRepo := new(MockTransactionRepositoryForBudget)
		svc := NewBudgetService(mockBudgetRepo, mockCategoryRepo, mockTransactionRepo, testLogger())
		return svc.(*budgetService), mockBudgetRepo
	}

	t.Run("invalid category kind returns validation error before any persist", func(t *testing.T) {
		svc, mockBudgetRepo := makeService()
		ctx := context.Background()

		req := &domain.CreateBudgetRequest{
			Name:         "Budget With Bad Category",
			Period:       domain.BudgetPeriodMonthly,
			StartDate:    domain.FlexibleDate(time.Now()),
			Amount:       1500.00,
			CurrencyCode: "USD",
			Categories: []domain.CreateBudgetCategoryRequest{
				{
					// "unknown_kind" is not pfc/custom/savings and MatchLogic is not tag_only
					// → Validate() returns "category_kind must be pfc, custom, or savings"
					CategoryKind:    "unknown_kind",
					AllocatedAmount: 200.00,
				},
			},
		}

		_, err := svc.CreateBudget(ctx, "user-abc", req)

		require.Error(t, err, "invalid category kind must cause CreateBudget to return an error")
		assert.Contains(t, err.Error(), "category_kind must be pfc, custom, or savings")
		// R5-2/R5-3: no DB write must occur when validation fails
		mockBudgetRepo.AssertNotCalled(t, "CreateWithCategories", mock.Anything, mock.Anything, mock.Anything)
	})

	t.Run("pfc category missing pfc_primary returns validation error before any persist", func(t *testing.T) {
		svc, mockBudgetRepo := makeService()
		ctx := context.Background()

		req := &domain.CreateBudgetRequest{
			Name:         "Budget With Invalid PFC",
			Period:       domain.BudgetPeriodMonthly,
			StartDate:    domain.FlexibleDate(time.Now()),
			Amount:       1500.00,
			CurrencyCode: "USD",
			Categories: []domain.CreateBudgetCategoryRequest{
				{
					CategoryKind:    "pfc",
					PFCPrimary:      nil, // missing → Validate() returns "pfc_primary is required"
					AllocatedAmount: 100.00,
				},
			},
		}

		_, err := svc.CreateBudget(ctx, "user-abc", req)

		require.Error(t, err, "pfc category without pfc_primary must cause CreateBudget to return an error")
		assert.Contains(t, err.Error(), "pfc_primary is required")
		mockBudgetRepo.AssertNotCalled(t, "CreateWithCategories", mock.Anything, mock.Anything, mock.Anything)
	})
}

// =============================================================================
// R3-4: CreateBudget savings category IDOR protection (Issue #1300, Codex C1)
// =============================================================================

// TestBudgetService_CreateBudget_SavingsCategory_IDOR verifies that CreateBudget
// rejects categories whose SavingsGoalID belongs to another user (R3-4 / Codex C1).
// R4-3: IDOR check now runs BEFORE budgetRepo.Create so no orphaned budget is persisted.
func TestBudgetService_CreateBudget_SavingsCategory_IDOR(t *testing.T) {
	userID := "user-creator"
	otherUserGoalID := "goal-other-user"

	makeService := func() (*budgetService, *MockBudgetRepository) {
		mockBudgetRepo := new(MockBudgetRepository)
		mockCategoryRepo := new(MockCustomCategoryRepository)
		mockTransactionRepo := new(MockTransactionRepositoryForBudget)
		svc := NewBudgetService(mockBudgetRepo, mockCategoryRepo, mockTransactionRepo, testLogger())
		return svc.(*budgetService), mockBudgetRepo
	}

	t.Run("rejects savings category with another user's goal (IDOR)", func(t *testing.T) {
		svc, mockBudgetRepo := makeService()
		ctx := context.Background()

		mockChecker := new(MockGoalOwnershipChecker)
		mockChecker.On("GetByIDForUser", ctx, otherUserGoalID, userID).Return(nil, domain.ErrSavingsGoalNotFound)
		svc.SetGoalOwnershipChecker(mockChecker)

		req := &domain.CreateBudgetRequest{
			Name:         "Budget With Savings",
			Period:       domain.BudgetPeriodMonthly,
			StartDate:    domain.FlexibleDate(time.Now()),
			Amount:       2000.00,
			CurrencyCode: "USD",
			Categories: []domain.CreateBudgetCategoryRequest{
				{
					CategoryKind:    "savings",
					SavingsGoalID:   &otherUserGoalID,
					AllocatedAmount: 500.00,
				},
			},
		}

		// R4-3/R5-3: CreateWithCategories must NOT be called — IDOR check runs before any persist.
		_, err := svc.CreateBudget(ctx, userID, req)

		require.Error(t, err, "cross-user goal in CreateBudget must return an error")
		assert.ErrorIs(t, err, domain.ErrSavingsGoalNotFound)
		mockBudgetRepo.AssertNotCalled(t, "CreateWithCategories")
		mockBudgetRepo.AssertNotCalled(t, "CreateCategory")
		mockChecker.AssertExpectations(t)
	})

	t.Run("accepts savings category with own goal", func(t *testing.T) {
		svc, mockBudgetRepo := makeService()
		ctx := context.Background()
		ownGoalID := "goal-own"

		mockChecker := new(MockGoalOwnershipChecker)
		mockChecker.On("GetByIDForUser", ctx, ownGoalID, userID).Return(&domain.SavingsGoal{ID: ownGoalID, UserID: userID}, nil)
		svc.SetGoalOwnershipChecker(mockChecker)

		req := &domain.CreateBudgetRequest{
			Name:         "Budget With Own Savings",
			Period:       domain.BudgetPeriodMonthly,
			StartDate:    domain.FlexibleDate(time.Now()),
			Amount:       3000.00,
			CurrencyCode: "USD",
			Categories: []domain.CreateBudgetCategoryRequest{
				{
					CategoryKind:    "savings",
					SavingsGoalID:   &ownGoalID,
					AllocatedAmount: 800.00,
				},
			},
		}

		mockBudgetRepo.On("CreateWithCategories", ctx, mock.AnythingOfType("*domain.Budget"), mock.Anything).Return(nil)

		budget, err := svc.CreateBudget(ctx, userID, req)

		require.NoError(t, err)
		require.NotNil(t, budget)
		mockBudgetRepo.AssertExpectations(t)
		mockChecker.AssertExpectations(t)
	})
}

// =============================================================================
// Issue #1540: custom_category_id ownership validation (IDOR)
// =============================================================================

// TestBudgetService_CreateBudget_RejectsCrossUserCustomCategory verifies that CreateBudget
// rejects a "custom" category referencing another user's custom_categories row before
// persisting anything — the same IDOR protection CreateBudget already gives SavingsGoalID.
func TestBudgetService_CreateBudget_RejectsCrossUserCustomCategory(t *testing.T) {
	mockBudgetRepo := new(MockBudgetRepository)
	mockCategoryRepo := new(MockCustomCategoryRepository)
	mockTransactionRepo := new(MockTransactionRepositoryForBudget)

	service := NewBudgetService(mockBudgetRepo, mockCategoryRepo, mockTransactionRepo, testLogger())

	ctx := context.Background()
	userID := "user-123"
	otherUsersCategoryID := "other-user-category-id"

	req := createTestCreateBudgetRequest()
	req.Categories = []domain.CreateBudgetCategoryRequest{
		{
			CategoryKind:     "custom",
			CustomCategoryID: &otherUsersCategoryID,
			AllocatedAmount:  100.00,
		},
	}

	// GetLiveNamesByIDs is user-scoped AND tombstone-filtered (#1569 Task 9); a category owned
	// by someone else is simply absent from the returned map.
	mockCategoryRepo.On("GetLiveNamesByIDs", ctx, userID, []string{otherUsersCategoryID}).
		Return(map[string]string{}, nil)

	budget, err := service.CreateBudget(ctx, userID, req)

	assert.ErrorIs(t, err, domain.ErrCategoryNotFound)
	assert.Nil(t, budget)
	mockBudgetRepo.AssertNotCalled(t, "CreateWithCategories", mock.Anything, mock.Anything, mock.Anything)
	mockCategoryRepo.AssertExpectations(t)
}

// TestBudgetService_AddCategory_RejectsCrossUserCustomCategory mirrors the CreateBudget
// case for the single-category AddCategory path.
func TestBudgetService_AddCategory_RejectsCrossUserCustomCategory(t *testing.T) {
	mockBudgetRepo := new(MockBudgetRepository)
	mockCategoryRepo := new(MockCustomCategoryRepository)
	mockTransactionRepo := new(MockTransactionRepositoryForBudget)

	service := NewBudgetService(mockBudgetRepo, mockCategoryRepo, mockTransactionRepo, testLogger())

	ctx := context.Background()
	userID := "user-123"
	budgetID := "budget-123"
	otherUsersCategoryID := "other-user-category-id"

	existingBudget := createTestBudgetForService(userID)

	req := &domain.CreateBudgetCategoryRequest{
		CategoryKind:     "custom",
		CustomCategoryID: &otherUsersCategoryID,
		AllocatedAmount:  50.00,
	}

	mockBudgetRepo.On("GetByID", ctx, budgetID).Return(existingBudget, nil)
	mockCategoryRepo.On("GetLiveNamesByIDs", ctx, userID, []string{otherUsersCategoryID}).
		Return(map[string]string{}, nil)

	category, err := service.AddCategory(ctx, userID, budgetID, req)

	assert.ErrorIs(t, err, domain.ErrCategoryNotFound)
	assert.Nil(t, category)
	mockBudgetRepo.AssertNotCalled(t, "AddCategoryChecked")
	mockBudgetRepo.AssertExpectations(t)
	mockCategoryRepo.AssertExpectations(t)
}

// TestBudgetService_AddCategory_RejectsDuplicateParentIdentity_PFC pins Issue #1630 Task 4:
// budgetedParentSets keys a parent by its pfc_primary VALUE, so a second primary-only row for
// the same pfc_primary would silently collide with the first in that map. AddCategory must
// reject the second row rather than persist an ambiguous parent.
func TestBudgetService_AddCategory_RejectsDuplicateParentIdentity_PFC(t *testing.T) {
	mockBudgetRepo := new(MockBudgetRepository)
	mockCategoryRepo := new(MockCustomCategoryRepository)
	mockTransactionRepo := new(MockTransactionRepositoryForBudget)

	service := NewBudgetService(mockBudgetRepo, mockCategoryRepo, mockTransactionRepo, testLogger())

	ctx := context.Background()
	userID := "user-123"
	budgetID := "budget-123"

	existingBudget := createTestBudgetForService(userID)
	existingBudget.Categories = []domain.BudgetCategory{
		{
			ID:              "cat-existing",
			BudgetID:        budgetID,
			CategoryKind:    "pfc",
			PFCPrimary:      strPtr("FOOD_AND_DRINK"),
			AllocatedAmount: 300.00,
		},
	}

	req := &domain.CreateBudgetCategoryRequest{
		CategoryKind:    "pfc",
		PFCPrimary:      strPtr("FOOD_AND_DRINK"),
		AllocatedAmount: 100.00,
	}

	mockBudgetRepo.On("GetByID", ctx, budgetID).Return(existingBudget, nil)

	category, err := service.AddCategory(ctx, userID, budgetID, req)

	assert.ErrorIs(t, err, domain.ErrDuplicateParentCategory)
	assert.Nil(t, category)
	mockassert.NoCallTo(t, &mockBudgetRepo.Mock, "AddCategoryChecked")
	mockBudgetRepo.AssertExpectations(t)
}

// TestBudgetService_AddCategory_RejectsDuplicateParentIdentity_Custom mirrors the PFC case
// above for custom categories, where custom_category_id itself is the parent identity.
func TestBudgetService_AddCategory_RejectsDuplicateParentIdentity_Custom(t *testing.T) {
	mockBudgetRepo := new(MockBudgetRepository)
	mockCategoryRepo := new(MockCustomCategoryRepository)
	mockTransactionRepo := new(MockTransactionRepositoryForBudget)

	service := NewBudgetService(mockBudgetRepo, mockCategoryRepo, mockTransactionRepo, testLogger())

	ctx := context.Background()
	userID := "user-123"
	budgetID := "budget-123"
	customID := "11111111-1111-1111-1111-111111111111"

	existingBudget := createTestBudgetForService(userID)
	existingBudget.Categories = []domain.BudgetCategory{
		{
			ID:               "cat-existing",
			BudgetID:         budgetID,
			CategoryKind:     "custom",
			CustomCategoryID: &customID,
			AllocatedAmount:  300.00,
		},
	}

	req := &domain.CreateBudgetCategoryRequest{
		CategoryKind:     "custom",
		CustomCategoryID: &customID,
		AllocatedAmount:  100.00,
	}

	mockBudgetRepo.On("GetByID", ctx, budgetID).Return(existingBudget, nil)
	mockCategoryRepo.On("GetLiveNamesByIDs", ctx, userID, []string{customID}).
		Return(map[string]string{customID: "Groceries"}, nil)
	mockCategoryRepo.On("GetByID", ctx, customID).
		Return(&domain.CustomCategory{ID: customID}, nil)

	category, err := service.AddCategory(ctx, userID, budgetID, req)

	assert.ErrorIs(t, err, domain.ErrDuplicateParentCategory)
	assert.Nil(t, category)
	mockassert.NoCallTo(t, &mockBudgetRepo.Mock, "AddCategoryChecked")
	mockBudgetRepo.AssertExpectations(t)
	mockCategoryRepo.AssertExpectations(t)
}

// TestBudgetService_AddCategory_AllowsSecondChildUnderSamePrimary confirms the guard above
// does not over-reach: two DETAILED children under the same budgeted primary are a normal,
// legal multi-child budget, not a duplicate parent (Issue #1630 Task 4).
func TestBudgetService_AddCategory_AllowsSecondChildUnderSamePrimary(t *testing.T) {
	mockBudgetRepo := new(MockBudgetRepository)
	mockCategoryRepo := new(MockCustomCategoryRepository)
	mockTransactionRepo := new(MockTransactionRepositoryForBudget)

	service := NewBudgetService(mockBudgetRepo, mockCategoryRepo, mockTransactionRepo, testLogger())

	ctx := context.Background()
	userID := "user-123"
	budgetID := "budget-123"

	existingBudget := createTestBudgetForService(userID)
	existingBudget.Categories = []domain.BudgetCategory{
		{
			ID:              "cat-parent",
			BudgetID:        budgetID,
			CategoryKind:    "pfc",
			PFCPrimary:      strPtr("FOOD_AND_DRINK"),
			AllocatedAmount: 300.00,
		},
		{
			ID:              "cat-child-1",
			BudgetID:        budgetID,
			CategoryKind:    "pfc",
			PFCPrimary:      strPtr("FOOD_AND_DRINK"),
			PFCDetailed:     strPtr("FOOD_AND_DRINK_FAST_FOOD"),
			AllocatedAmount: 50.00,
		},
	}

	req := &domain.CreateBudgetCategoryRequest{
		CategoryKind:    "pfc",
		PFCPrimary:      strPtr("FOOD_AND_DRINK"),
		PFCDetailed:     strPtr("FOOD_AND_DRINK_COFFEE"),
		AllocatedAmount: 20.00,
	}

	mockBudgetRepo.On("GetByID", ctx, budgetID).Return(existingBudget, nil)
	mockBudgetRepo.On("AddCategoryChecked", ctx, budgetID, mock.AnythingOfType("*domain.BudgetCategory")).Return(nil, nil)

	category, err := service.AddCategory(ctx, userID, budgetID, req)

	assert.NoError(t, err)
	assert.NotNil(t, category)
	mockBudgetRepo.AssertExpectations(t)
}

// TestBudgetService_CreateBudget_RejectsDuplicateParentIdentityWithinBatch pins the
// batch-create half of Issue #1630 Task 4: a single CreateBudget request naming the same
// parent identity twice must fail atomically, before any row is persisted.
func TestBudgetService_CreateBudget_RejectsDuplicateParentIdentityWithinBatch(t *testing.T) {
	mockBudgetRepo := new(MockBudgetRepository)
	mockCategoryRepo := new(MockCustomCategoryRepository)
	mockTransactionRepo := new(MockTransactionRepositoryForBudget)

	service := NewBudgetService(mockBudgetRepo, mockCategoryRepo, mockTransactionRepo, testLogger())

	ctx := context.Background()
	userID := "user-123"

	req := createTestCreateBudgetRequest()
	req.Categories = []domain.CreateBudgetCategoryRequest{
		{CategoryKind: "pfc", PFCPrimary: strPtr("FOOD_AND_DRINK"), AllocatedAmount: 300.00},
		{CategoryKind: "pfc", PFCPrimary: strPtr("FOOD_AND_DRINK"), AllocatedAmount: 100.00},
	}

	budget, err := service.CreateBudget(ctx, userID, req)

	assert.ErrorIs(t, err, domain.ErrDuplicateParentCategory)
	assert.Nil(t, budget)
	mockassert.NoCallTo(t, &mockBudgetRepo.Mock, "CreateWithCategories")
	mockBudgetRepo.AssertExpectations(t)
}

// TestBudgetService_AddCategory_RejectsEscapedNestedChild_PFC pins Issue #1630's HIGH-severity
// adversarial-review finding: a detailed PFC child whose match_logic escapes containment
// (here, 'or') under an existing budgeted category_only primary must be rejected at write
// time with a 400 naming match_logic, not silently persisted to double-count spend.
func TestBudgetService_AddCategory_RejectsEscapedNestedChild_PFC(t *testing.T) {
	mockBudgetRepo := new(MockBudgetRepository)
	mockCategoryRepo := new(MockCustomCategoryRepository)
	mockTransactionRepo := new(MockTransactionRepositoryForBudget)

	service := NewBudgetService(mockBudgetRepo, mockCategoryRepo, mockTransactionRepo, testLogger())

	ctx := context.Background()
	userID := "user-123"
	budgetID := "budget-123"

	existingBudget := createTestBudgetForService(userID)
	existingBudget.Categories = []domain.BudgetCategory{
		{
			ID:              "cat-parent",
			BudgetID:        budgetID,
			CategoryKind:    "pfc",
			PFCPrimary:      strPtr("FOOD_AND_DRINK"),
			MatchLogic:      domain.MatchLogicCategoryOnly,
			AllocatedAmount: 300.00,
		},
	}

	req := &domain.CreateBudgetCategoryRequest{
		CategoryKind:    "pfc",
		PFCPrimary:      strPtr("FOOD_AND_DRINK"),
		PFCDetailed:     strPtr("FOOD_AND_DRINK_FAST_FOOD"),
		AllocatedAmount: 50.00,
		MatchLogic:      domain.MatchLogicOr,
		Tags:            []string{"splurge"},
	}

	mockBudgetRepo.On("GetByID", ctx, budgetID).Return(existingBudget, nil)

	category, err := service.AddCategory(ctx, userID, budgetID, req)

	require.Error(t, err)
	assert.ErrorIs(t, err, domain.ErrEscapedNestedChild)
	assert.ErrorIs(t, err, domain.ErrInvalidBudget)
	assert.Contains(t, err.Error(), "match_logic")
	assert.Nil(t, category)
	mockassert.NoCallTo(t, &mockBudgetRepo.Mock, "AddCategoryChecked")
	mockBudgetRepo.AssertExpectations(t)
}

// TestBudgetService_AddCategory_AllowsProperlyContainedNestedChild confirms the guard above
// does not over-reach: a category_only parent with a category_only nested child stays
// well-formed (Issue #1630's original contained-nesting shape must remain legal).
func TestBudgetService_AddCategory_AllowsProperlyContainedNestedChild(t *testing.T) {
	mockBudgetRepo := new(MockBudgetRepository)
	mockCategoryRepo := new(MockCustomCategoryRepository)
	mockTransactionRepo := new(MockTransactionRepositoryForBudget)

	service := NewBudgetService(mockBudgetRepo, mockCategoryRepo, mockTransactionRepo, testLogger())

	ctx := context.Background()
	userID := "user-123"
	budgetID := "budget-123"

	existingBudget := createTestBudgetForService(userID)
	existingBudget.Categories = []domain.BudgetCategory{
		{
			ID:              "cat-parent",
			BudgetID:        budgetID,
			CategoryKind:    "pfc",
			PFCPrimary:      strPtr("FOOD_AND_DRINK"),
			MatchLogic:      domain.MatchLogicCategoryOnly,
			AllocatedAmount: 300.00,
		},
	}

	req := &domain.CreateBudgetCategoryRequest{
		CategoryKind:    "pfc",
		PFCPrimary:      strPtr("FOOD_AND_DRINK"),
		PFCDetailed:     strPtr("FOOD_AND_DRINK_FAST_FOOD"),
		AllocatedAmount: 50.00,
		MatchLogic:      domain.MatchLogicCategoryOnly,
	}

	mockBudgetRepo.On("GetByID", ctx, budgetID).Return(existingBudget, nil)
	mockBudgetRepo.On("AddCategoryChecked", ctx, budgetID, mock.AnythingOfType("*domain.BudgetCategory")).Return(nil, nil)

	category, err := service.AddCategory(ctx, userID, budgetID, req)

	assert.NoError(t, err)
	assert.NotNil(t, category)
	mockBudgetRepo.AssertExpectations(t)
}

// TestBudgetService_CreateBudget_RejectsEscapedNestedChildWithinBatch_CustomParentAfterChild
// pins the ordering half of Issue #1630's HIGH-severity finding: a batch may legally name the
// escaping CHILD before its containing PARENT in req.Categories (CreateBudget has no ordering
// requirement today), and the guard must still catch the pair regardless of which one is
// processed first.
func TestBudgetService_CreateBudget_RejectsEscapedNestedChildWithinBatch_CustomParentAfterChild(t *testing.T) {
	mockBudgetRepo := new(MockBudgetRepository)
	mockCategoryRepo := new(MockCustomCategoryRepository)
	mockTransactionRepo := new(MockTransactionRepositoryForBudget)

	service := NewBudgetService(mockBudgetRepo, mockCategoryRepo, mockTransactionRepo, testLogger())

	ctx := context.Background()
	userID := "user-123"
	childID := "22222222-2222-2222-2222-222222222222"

	req := createTestCreateBudgetRequest()
	req.Categories = []domain.CreateBudgetCategoryRequest{
		// Child named FIRST: a custom category nested (via parent_pfc_primary) under
		// FOOD_AND_DRINK, whose own match_logic is 'tag_only' — escapes containment.
		{
			CategoryKind:     "custom",
			CustomCategoryID: &childID,
			AllocatedAmount:  50.00,
			MatchLogic:       domain.MatchLogicTagOnly,
			Tags:             []string{"splurge"},
		},
		// Parent named SECOND: the budgeted category_only PFC primary the child nests under.
		{CategoryKind: "pfc", PFCPrimary: strPtr("FOOD_AND_DRINK"), AllocatedAmount: 300.00, MatchLogic: domain.MatchLogicCategoryOnly},
	}

	mockCategoryRepo.On("GetLiveNamesByIDs", ctx, userID, []string{childID}).
		Return(map[string]string{childID: "Fast Food Splurges"}, nil)
	mockCategoryRepo.On("GetByID", ctx, childID).
		Return(&domain.CustomCategory{ID: childID, ParentPFCPrimary: strPtr("FOOD_AND_DRINK")}, nil)

	budget, err := service.CreateBudget(ctx, userID, req)

	require.Error(t, err)
	assert.ErrorIs(t, err, domain.ErrEscapedNestedChild)
	assert.ErrorIs(t, err, domain.ErrInvalidBudget)
	assert.Contains(t, err.Error(), "match_logic")
	assert.Nil(t, budget)
	mockassert.NoCallTo(t, &mockBudgetRepo.Mock, "CreateWithCategories")
	mockCategoryRepo.AssertExpectations(t)
}
