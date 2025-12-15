package services

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// =============================================================================
// Issue #494: BudgetSyncService Tests
// =============================================================================

// MockBudgetSyncRepository is a mock implementation of BudgetSyncRepository
type MockBudgetSyncRepository struct {
	mock.Mock
}

func (m *MockBudgetSyncRepository) GetActiveByUserID(ctx context.Context, userID string) ([]*domain.Budget, error) {
	args := m.Called(ctx, userID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]*domain.Budget), args.Error(1)
}

func (m *MockBudgetSyncRepository) GetCategoriesByBudgetID(ctx context.Context, budgetID string) ([]domain.BudgetCategory, error) {
	args := m.Called(ctx, budgetID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]domain.BudgetCategory), args.Error(1)
}

func (m *MockBudgetSyncRepository) UpdateBudgetIncome(ctx context.Context, budgetID string, incomeAmount float64) error {
	args := m.Called(ctx, budgetID, incomeAmount)
	return args.Error(0)
}

func (m *MockBudgetSyncRepository) UpdateCategorySpentAmount(ctx context.Context, categoryID string, spentAmount float64) error {
	args := m.Called(ctx, categoryID, spentAmount)
	return args.Error(0)
}

// MockBudgetIncomeCalculator is a mock for the income calculation service
type MockBudgetIncomeCalculator struct {
	mock.Mock
}

func (m *MockBudgetIncomeCalculator) CalculateBudgetIncome(ctx context.Context, budget *domain.Budget, startDate, endDate time.Time) (float64, error) {
	args := m.Called(ctx, budget, startDate, endDate)
	return args.Get(0).(float64), args.Error(1)
}

// MockBudgetSpentCalculator is a mock for the category spent calculation service
type MockBudgetSpentCalculator struct {
	mock.Mock
}

func (m *MockBudgetSpentCalculator) CalculateCategorySpent(ctx context.Context, userID string, category *domain.BudgetCategory, startDate, endDate time.Time) (float64, error) {
	args := m.Called(ctx, userID, category, startDate, endDate)
	return args.Get(0).(float64), args.Error(1)
}

// Helper to create a test logger
func testSyncServiceLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
}

// =============================================================================
// calculateBudgetPeriodDates Tests
// =============================================================================

// TestCalculateBudgetPeriodDates_Weekly tests weekly period calculation (Sunday-Saturday)
func TestCalculateBudgetPeriodDates_Weekly(t *testing.T) {
	tests := []struct {
		name          string
		now           time.Time
		expectedStart time.Time
		expectedEnd   time.Time
	}{
		{
			name:          "mid-week Wednesday",
			now:           time.Date(2024, 1, 17, 15, 30, 0, 0, time.UTC), // Wednesday Jan 17, 2024
			expectedStart: time.Date(2024, 1, 14, 0, 0, 0, 0, time.UTC),  // Sunday Jan 14
			expectedEnd:   time.Date(2024, 1, 20, 23, 59, 59, 999999999, time.UTC), // Saturday Jan 20
		},
		{
			name:          "on Sunday (first day)",
			now:           time.Date(2024, 1, 14, 10, 0, 0, 0, time.UTC), // Sunday Jan 14, 2024
			expectedStart: time.Date(2024, 1, 14, 0, 0, 0, 0, time.UTC),  // Same Sunday
			expectedEnd:   time.Date(2024, 1, 20, 23, 59, 59, 999999999, time.UTC), // Saturday Jan 20
		},
		{
			name:          "on Saturday (last day)",
			now:           time.Date(2024, 1, 20, 23, 0, 0, 0, time.UTC), // Saturday Jan 20, 2024
			expectedStart: time.Date(2024, 1, 14, 0, 0, 0, 0, time.UTC),  // Sunday Jan 14
			expectedEnd:   time.Date(2024, 1, 20, 23, 59, 59, 999999999, time.UTC), // Same Saturday
		},
		{
			name:          "week spanning month boundary",
			now:           time.Date(2024, 2, 1, 12, 0, 0, 0, time.UTC), // Thursday Feb 1, 2024
			expectedStart: time.Date(2024, 1, 28, 0, 0, 0, 0, time.UTC),  // Sunday Jan 28
			expectedEnd:   time.Date(2024, 2, 3, 23, 59, 59, 999999999, time.UTC), // Saturday Feb 3
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			budget := &domain.Budget{Period: domain.BudgetPeriodWeekly}
			start, end := calculateBudgetPeriodDates(budget, tt.now)
			assert.Equal(t, tt.expectedStart, start, "start date mismatch")
			assert.Equal(t, tt.expectedEnd, end, "end date mismatch")
		})
	}
}

// TestCalculateBudgetPeriodDates_Monthly tests monthly period calculation
func TestCalculateBudgetPeriodDates_Monthly(t *testing.T) {
	tests := []struct {
		name          string
		now           time.Time
		expectedStart time.Time
		expectedEnd   time.Time
	}{
		{
			name:          "regular month January",
			now:           time.Date(2024, 1, 15, 12, 0, 0, 0, time.UTC),
			expectedStart: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
			expectedEnd:   time.Date(2024, 1, 31, 23, 59, 59, 999999999, time.UTC),
		},
		{
			name:          "February leap year",
			now:           time.Date(2024, 2, 10, 12, 0, 0, 0, time.UTC),
			expectedStart: time.Date(2024, 2, 1, 0, 0, 0, 0, time.UTC),
			expectedEnd:   time.Date(2024, 2, 29, 23, 59, 59, 999999999, time.UTC),
		},
		{
			name:          "February non-leap year",
			now:           time.Date(2023, 2, 15, 12, 0, 0, 0, time.UTC),
			expectedStart: time.Date(2023, 2, 1, 0, 0, 0, 0, time.UTC),
			expectedEnd:   time.Date(2023, 2, 28, 23, 59, 59, 999999999, time.UTC),
		},
		{
			name:          "30-day month April",
			now:           time.Date(2024, 4, 30, 12, 0, 0, 0, time.UTC),
			expectedStart: time.Date(2024, 4, 1, 0, 0, 0, 0, time.UTC),
			expectedEnd:   time.Date(2024, 4, 30, 23, 59, 59, 999999999, time.UTC),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			budget := &domain.Budget{Period: domain.BudgetPeriodMonthly}
			start, end := calculateBudgetPeriodDates(budget, tt.now)
			assert.Equal(t, tt.expectedStart, start, "start date mismatch")
			assert.Equal(t, tt.expectedEnd, end, "end date mismatch")
		})
	}
}

// TestCalculateBudgetPeriodDates_Quarterly tests quarterly period calculation
func TestCalculateBudgetPeriodDates_Quarterly(t *testing.T) {
	tests := []struct {
		name          string
		now           time.Time
		expectedStart time.Time
		expectedEnd   time.Time
	}{
		{
			name:          "Q1 - January",
			now:           time.Date(2024, 1, 15, 12, 0, 0, 0, time.UTC),
			expectedStart: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
			expectedEnd:   time.Date(2024, 3, 31, 23, 59, 59, 999999999, time.UTC),
		},
		{
			name:          "Q1 - March end",
			now:           time.Date(2024, 3, 31, 23, 59, 59, 0, time.UTC),
			expectedStart: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
			expectedEnd:   time.Date(2024, 3, 31, 23, 59, 59, 999999999, time.UTC),
		},
		{
			name:          "Q2 - April start",
			now:           time.Date(2024, 4, 1, 0, 0, 0, 0, time.UTC),
			expectedStart: time.Date(2024, 4, 1, 0, 0, 0, 0, time.UTC),
			expectedEnd:   time.Date(2024, 6, 30, 23, 59, 59, 999999999, time.UTC),
		},
		{
			name:          "Q3 - August mid",
			now:           time.Date(2024, 8, 15, 12, 0, 0, 0, time.UTC),
			expectedStart: time.Date(2024, 7, 1, 0, 0, 0, 0, time.UTC),
			expectedEnd:   time.Date(2024, 9, 30, 23, 59, 59, 999999999, time.UTC),
		},
		{
			name:          "Q4 - December",
			now:           time.Date(2024, 12, 25, 12, 0, 0, 0, time.UTC),
			expectedStart: time.Date(2024, 10, 1, 0, 0, 0, 0, time.UTC),
			expectedEnd:   time.Date(2024, 12, 31, 23, 59, 59, 999999999, time.UTC),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			budget := &domain.Budget{Period: domain.BudgetPeriodQuarterly}
			start, end := calculateBudgetPeriodDates(budget, tt.now)
			assert.Equal(t, tt.expectedStart, start, "start date mismatch")
			assert.Equal(t, tt.expectedEnd, end, "end date mismatch")
		})
	}
}

// TestCalculateBudgetPeriodDates_Yearly tests yearly period calculation
func TestCalculateBudgetPeriodDates_Yearly(t *testing.T) {
	tests := []struct {
		name          string
		now           time.Time
		expectedStart time.Time
		expectedEnd   time.Time
	}{
		{
			name:          "mid-year",
			now:           time.Date(2024, 6, 15, 12, 0, 0, 0, time.UTC),
			expectedStart: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
			expectedEnd:   time.Date(2024, 12, 31, 23, 59, 59, 999999999, time.UTC),
		},
		{
			name:          "January first",
			now:           time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
			expectedStart: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
			expectedEnd:   time.Date(2024, 12, 31, 23, 59, 59, 999999999, time.UTC),
		},
		{
			name:          "December last",
			now:           time.Date(2024, 12, 31, 23, 59, 59, 0, time.UTC),
			expectedStart: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
			expectedEnd:   time.Date(2024, 12, 31, 23, 59, 59, 999999999, time.UTC),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			budget := &domain.Budget{Period: domain.BudgetPeriodYearly}
			start, end := calculateBudgetPeriodDates(budget, tt.now)
			assert.Equal(t, tt.expectedStart, start, "start date mismatch")
			assert.Equal(t, tt.expectedEnd, end, "end date mismatch")
		})
	}
}

// TestCalculateBudgetPeriodDates_Custom tests custom period using budget dates
func TestCalculateBudgetPeriodDates_Custom(t *testing.T) {
	customStart := time.Date(2024, 3, 1, 0, 0, 0, 0, time.UTC)
	customEnd := time.Date(2024, 5, 31, 23, 59, 59, 0, time.UTC)

	t.Run("with end date", func(t *testing.T) {
		budget := &domain.Budget{
			Period:    domain.BudgetPeriodCustom,
			StartDate: customStart,
			EndDate:   &customEnd,
		}
		now := time.Date(2024, 4, 15, 12, 0, 0, 0, time.UTC)

		start, end := calculateBudgetPeriodDates(budget, now)
		assert.Equal(t, customStart, start)
		assert.Equal(t, customEnd, end)
	})

	t.Run("without end date", func(t *testing.T) {
		budget := &domain.Budget{
			Period:    domain.BudgetPeriodCustom,
			StartDate: customStart,
			EndDate:   nil,
		}
		now := time.Date(2024, 4, 15, 12, 0, 0, 0, time.UTC)

		start, end := calculateBudgetPeriodDates(budget, now)
		assert.Equal(t, customStart, start)
		// End date should be far in the future when not specified
		assert.True(t, end.After(now.AddDate(50, 0, 0)), "end date should be far in future")
	})
}

// =============================================================================
// RecalculateBudget Tests
// =============================================================================

// TestRecalculateBudget_Success tests successful recalculation of income and categories
func TestRecalculateBudget_Success(t *testing.T) {
	mockRepo := new(MockBudgetSyncRepository)
	mockIncomeCalc := new(MockBudgetIncomeCalculator)
	mockSpentCalc := new(MockBudgetSpentCalculator)
	logger := testSyncServiceLogger()

	service := NewBudgetSyncService(mockRepo, mockIncomeCalc, mockSpentCalc, logger)
	ctx := context.Background()

	budget := &domain.Budget{
		ID:     "budget-123",
		UserID: "user-123",
		Period: domain.BudgetPeriodMonthly,
	}

	categories := []domain.BudgetCategory{
		{ID: "cat-1", BudgetID: "budget-123", Category: []string{"Food"}},
		{ID: "cat-2", BudgetID: "budget-123", Category: []string{"Transport"}},
	}

	// Setup expectations - use mock.Anything for time parameters since they depend on time.Now()
	mockIncomeCalc.On("CalculateBudgetIncome", ctx, budget, mock.AnythingOfType("time.Time"), mock.AnythingOfType("time.Time")).Return(5000.0, nil)
	mockRepo.On("UpdateBudgetIncome", ctx, "budget-123", 5000.0).Return(nil)
	mockRepo.On("GetCategoriesByBudgetID", ctx, "budget-123").Return(categories, nil)
	mockSpentCalc.On("CalculateCategorySpent", ctx, "user-123", &categories[0], mock.AnythingOfType("time.Time"), mock.AnythingOfType("time.Time")).Return(500.0, nil)
	mockSpentCalc.On("CalculateCategorySpent", ctx, "user-123", &categories[1], mock.AnythingOfType("time.Time"), mock.AnythingOfType("time.Time")).Return(200.0, nil)
	mockRepo.On("UpdateCategorySpentAmount", ctx, "cat-1", 500.0).Return(nil)
	mockRepo.On("UpdateCategorySpentAmount", ctx, "cat-2", 200.0).Return(nil)

	err := service.RecalculateBudget(ctx, budget)

	require.NoError(t, err)
	mockRepo.AssertExpectations(t)
	mockIncomeCalc.AssertExpectations(t)
	mockSpentCalc.AssertExpectations(t)
}

// TestRecalculateBudget_NoCategories tests recalculation with no categories
func TestRecalculateBudget_NoCategories(t *testing.T) {
	mockRepo := new(MockBudgetSyncRepository)
	mockIncomeCalc := new(MockBudgetIncomeCalculator)
	mockSpentCalc := new(MockBudgetSpentCalculator)
	logger := testSyncServiceLogger()

	service := NewBudgetSyncService(mockRepo, mockIncomeCalc, mockSpentCalc, logger)
	ctx := context.Background()

	budget := &domain.Budget{
		ID:     "budget-123",
		UserID: "user-123",
		Period: domain.BudgetPeriodMonthly,
	}

	mockIncomeCalc.On("CalculateBudgetIncome", ctx, budget, mock.AnythingOfType("time.Time"), mock.AnythingOfType("time.Time")).Return(5000.0, nil)
	mockRepo.On("UpdateBudgetIncome", ctx, "budget-123", 5000.0).Return(nil)
	mockRepo.On("GetCategoriesByBudgetID", ctx, "budget-123").Return([]domain.BudgetCategory{}, nil)

	err := service.RecalculateBudget(ctx, budget)

	require.NoError(t, err)
	mockRepo.AssertExpectations(t)
	mockIncomeCalc.AssertExpectations(t)
	// SpentCalc should NOT be called since no categories
	mockSpentCalc.AssertNotCalled(t, "CalculateCategorySpent")
}

// TestRecalculateBudget_CategoryError tests that category errors are logged but don't fail the operation
func TestRecalculateBudget_CategoryError(t *testing.T) {
	mockRepo := new(MockBudgetSyncRepository)
	mockIncomeCalc := new(MockBudgetIncomeCalculator)
	mockSpentCalc := new(MockBudgetSpentCalculator)
	logger := testSyncServiceLogger()

	service := NewBudgetSyncService(mockRepo, mockIncomeCalc, mockSpentCalc, logger)
	ctx := context.Background()

	budget := &domain.Budget{
		ID:     "budget-123",
		UserID: "user-123",
		Period: domain.BudgetPeriodMonthly,
	}

	categories := []domain.BudgetCategory{
		{ID: "cat-1", BudgetID: "budget-123", Category: []string{"Food"}},
		{ID: "cat-2", BudgetID: "budget-123", Category: []string{"Transport"}},
	}

	mockIncomeCalc.On("CalculateBudgetIncome", ctx, budget, mock.AnythingOfType("time.Time"), mock.AnythingOfType("time.Time")).Return(5000.0, nil)
	mockRepo.On("UpdateBudgetIncome", ctx, "budget-123", 5000.0).Return(nil)
	mockRepo.On("GetCategoriesByBudgetID", ctx, "budget-123").Return(categories, nil)
	// First category calculation fails
	mockSpentCalc.On("CalculateCategorySpent", ctx, "user-123", &categories[0], mock.AnythingOfType("time.Time"), mock.AnythingOfType("time.Time")).Return(0.0, errors.New("calculation error"))
	// Second category succeeds
	mockSpentCalc.On("CalculateCategorySpent", ctx, "user-123", &categories[1], mock.AnythingOfType("time.Time"), mock.AnythingOfType("time.Time")).Return(200.0, nil)
	mockRepo.On("UpdateCategorySpentAmount", ctx, "cat-2", 200.0).Return(nil)

	err := service.RecalculateBudget(ctx, budget)

	// Should NOT return error - category errors are logged but don't fail the operation
	require.NoError(t, err)
	mockRepo.AssertExpectations(t)
	mockSpentCalc.AssertExpectations(t)
	// cat-1 should NOT have its spent amount updated due to calculation error
	mockRepo.AssertNotCalled(t, "UpdateCategorySpentAmount", ctx, "cat-1", mock.Anything)
}

// TestRecalculateBudget_IncomeError tests that income calculation errors fail the operation
func TestRecalculateBudget_IncomeError(t *testing.T) {
	mockRepo := new(MockBudgetSyncRepository)
	mockIncomeCalc := new(MockBudgetIncomeCalculator)
	mockSpentCalc := new(MockBudgetSpentCalculator)
	logger := testSyncServiceLogger()

	service := NewBudgetSyncService(mockRepo, mockIncomeCalc, mockSpentCalc, logger)
	ctx := context.Background()

	budget := &domain.Budget{
		ID:     "budget-123",
		UserID: "user-123",
		Period: domain.BudgetPeriodMonthly,
	}

	mockIncomeCalc.On("CalculateBudgetIncome", ctx, budget, mock.AnythingOfType("time.Time"), mock.AnythingOfType("time.Time")).Return(0.0, errors.New("income error"))

	err := service.RecalculateBudget(ctx, budget)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to calculate income")
	mockIncomeCalc.AssertExpectations(t)
	// No further calls should be made
	mockRepo.AssertNotCalled(t, "UpdateBudgetIncome")
	mockRepo.AssertNotCalled(t, "GetCategoriesByBudgetID")
}

// TestRecalculateBudget_UpdateIncomeError tests that income update errors fail the operation
func TestRecalculateBudget_UpdateIncomeError(t *testing.T) {
	mockRepo := new(MockBudgetSyncRepository)
	mockIncomeCalc := new(MockBudgetIncomeCalculator)
	mockSpentCalc := new(MockBudgetSpentCalculator)
	logger := testSyncServiceLogger()

	service := NewBudgetSyncService(mockRepo, mockIncomeCalc, mockSpentCalc, logger)
	ctx := context.Background()

	budget := &domain.Budget{
		ID:     "budget-123",
		UserID: "user-123",
		Period: domain.BudgetPeriodMonthly,
	}

	mockIncomeCalc.On("CalculateBudgetIncome", ctx, budget, mock.AnythingOfType("time.Time"), mock.AnythingOfType("time.Time")).Return(5000.0, nil)
	mockRepo.On("UpdateBudgetIncome", ctx, "budget-123", 5000.0).Return(errors.New("db error"))

	err := service.RecalculateBudget(ctx, budget)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to update budget income")
	mockRepo.AssertNotCalled(t, "GetCategoriesByBudgetID")
}

// =============================================================================
// RecalculateBudgetAmounts Tests
// =============================================================================

// TestRecalculateBudgetAmounts_MultipleBudgets tests recalculation of multiple active budgets
func TestRecalculateBudgetAmounts_MultipleBudgets(t *testing.T) {
	mockRepo := new(MockBudgetSyncRepository)
	mockIncomeCalc := new(MockBudgetIncomeCalculator)
	mockSpentCalc := new(MockBudgetSpentCalculator)
	logger := testSyncServiceLogger()

	service := NewBudgetSyncService(mockRepo, mockIncomeCalc, mockSpentCalc, logger)
	ctx := context.Background()

	budgets := []*domain.Budget{
		{ID: "budget-1", UserID: "user-123", Period: domain.BudgetPeriodMonthly},
		{ID: "budget-2", UserID: "user-123", Period: domain.BudgetPeriodWeekly},
	}

	mockRepo.On("GetActiveByUserID", ctx, "user-123").Return(budgets, nil)

	// Budget 1 expectations
	mockIncomeCalc.On("CalculateBudgetIncome", ctx, budgets[0], mock.AnythingOfType("time.Time"), mock.AnythingOfType("time.Time")).Return(5000.0, nil)
	mockRepo.On("UpdateBudgetIncome", ctx, "budget-1", 5000.0).Return(nil)
	mockRepo.On("GetCategoriesByBudgetID", ctx, "budget-1").Return([]domain.BudgetCategory{}, nil)

	// Budget 2 expectations
	mockIncomeCalc.On("CalculateBudgetIncome", ctx, budgets[1], mock.AnythingOfType("time.Time"), mock.AnythingOfType("time.Time")).Return(1000.0, nil)
	mockRepo.On("UpdateBudgetIncome", ctx, "budget-2", 1000.0).Return(nil)
	mockRepo.On("GetCategoriesByBudgetID", ctx, "budget-2").Return([]domain.BudgetCategory{}, nil)

	err := service.RecalculateBudgetAmounts(ctx, "user-123")

	require.NoError(t, err)
	mockRepo.AssertExpectations(t)
	mockIncomeCalc.AssertExpectations(t)
}

// TestRecalculateBudgetAmounts_NoBudgets tests no-op when user has no active budgets
func TestRecalculateBudgetAmounts_NoBudgets(t *testing.T) {
	mockRepo := new(MockBudgetSyncRepository)
	mockIncomeCalc := new(MockBudgetIncomeCalculator)
	mockSpentCalc := new(MockBudgetSpentCalculator)
	logger := testSyncServiceLogger()

	service := NewBudgetSyncService(mockRepo, mockIncomeCalc, mockSpentCalc, logger)
	ctx := context.Background()

	mockRepo.On("GetActiveByUserID", ctx, "user-123").Return([]*domain.Budget{}, nil)

	err := service.RecalculateBudgetAmounts(ctx, "user-123")

	require.NoError(t, err)
	mockRepo.AssertExpectations(t)
	// No income or spent calculations should be made
	mockIncomeCalc.AssertNotCalled(t, "CalculateBudgetIncome")
	mockSpentCalc.AssertNotCalled(t, "CalculateCategorySpent")
}

// TestRecalculateBudgetAmounts_PartialFailure tests that budget failures are logged but don't stop other budgets
func TestRecalculateBudgetAmounts_PartialFailure(t *testing.T) {
	mockRepo := new(MockBudgetSyncRepository)
	mockIncomeCalc := new(MockBudgetIncomeCalculator)
	mockSpentCalc := new(MockBudgetSpentCalculator)
	logger := testSyncServiceLogger()

	service := NewBudgetSyncService(mockRepo, mockIncomeCalc, mockSpentCalc, logger)
	ctx := context.Background()

	budgets := []*domain.Budget{
		{ID: "budget-1", UserID: "user-123", Period: domain.BudgetPeriodMonthly},
		{ID: "budget-2", UserID: "user-123", Period: domain.BudgetPeriodWeekly},
	}

	mockRepo.On("GetActiveByUserID", ctx, "user-123").Return(budgets, nil)

	// Budget 1 fails
	mockIncomeCalc.On("CalculateBudgetIncome", ctx, budgets[0], mock.AnythingOfType("time.Time"), mock.AnythingOfType("time.Time")).Return(0.0, errors.New("income error"))

	// Budget 2 succeeds
	mockIncomeCalc.On("CalculateBudgetIncome", ctx, budgets[1], mock.AnythingOfType("time.Time"), mock.AnythingOfType("time.Time")).Return(1000.0, nil)
	mockRepo.On("UpdateBudgetIncome", ctx, "budget-2", 1000.0).Return(nil)
	mockRepo.On("GetCategoriesByBudgetID", ctx, "budget-2").Return([]domain.BudgetCategory{}, nil)

	err := service.RecalculateBudgetAmounts(ctx, "user-123")

	// Should NOT return error - partial failures are logged but don't fail the operation
	require.NoError(t, err)
	mockRepo.AssertExpectations(t)
	mockIncomeCalc.AssertExpectations(t)
}

// TestRecalculateBudgetAmounts_GetBudgetsError tests that error fetching budgets returns error
func TestRecalculateBudgetAmounts_GetBudgetsError(t *testing.T) {
	mockRepo := new(MockBudgetSyncRepository)
	mockIncomeCalc := new(MockBudgetIncomeCalculator)
	mockSpentCalc := new(MockBudgetSpentCalculator)
	logger := testSyncServiceLogger()

	service := NewBudgetSyncService(mockRepo, mockIncomeCalc, mockSpentCalc, logger)
	ctx := context.Background()

	mockRepo.On("GetActiveByUserID", ctx, "user-123").Return(nil, errors.New("db error"))

	err := service.RecalculateBudgetAmounts(ctx, "user-123")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to get active budgets")
}
