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

// MockEventTriggerProcessor is a mock implementation of EventTriggerProcessor
type MockEventTriggerProcessor struct {
	mock.Mock
}

func (m *MockEventTriggerProcessor) ProcessEventTrigger(ctx context.Context, userID, eventType string, eventData map[string]interface{}) error {
	args := m.Called(ctx, userID, eventType, eventData)
	return args.Error(0)
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
		{ID: "cat-1", BudgetID: "budget-123", CategoryKind: "pfc", PFCPrimary: ptrStr("FOOD_AND_DRINK")},
		{ID: "cat-2", BudgetID: "budget-123", CategoryKind: "pfc", PFCPrimary: ptrStr("TRANSPORTATION")},
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
		{ID: "cat-1", BudgetID: "budget-123", CategoryKind: "pfc", PFCPrimary: ptrStr("FOOD_AND_DRINK")},
		{ID: "cat-2", BudgetID: "budget-123", CategoryKind: "pfc", PFCPrimary: ptrStr("TRANSPORTATION")},
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

// =============================================================================
// Event Processor Wiring Tests (Issue #972)
// =============================================================================

// TestRecalculateBudget_TriggersEventProcessor tests that after SetEventProcessor is called,
// RecalculateBudget fires ProcessEventTrigger with eventType="budget_updated".
// This is the RED→GREEN test for the B1 event wiring fix.
func TestRecalculateBudget_TriggersEventProcessor(t *testing.T) {
	mockRepo := new(MockBudgetSyncRepository)
	mockIncomeCalc := new(MockBudgetIncomeCalculator)
	mockSpentCalc := new(MockBudgetSpentCalculator)
	mockEventProc := new(MockEventTriggerProcessor)
	logger := testSyncServiceLogger()

	service := NewBudgetSyncService(mockRepo, mockIncomeCalc, mockSpentCalc, logger)
	service.SetEventProcessor(mockEventProc)

	ctx := context.Background()
	budget := &domain.Budget{
		ID:     "budget-999",
		UserID: "user-999",
		Period: domain.BudgetPeriodMonthly,
	}

	categories := []domain.BudgetCategory{
		{ID: "cat-1", BudgetID: "budget-999", CategoryKind: "pfc", PFCPrimary: ptrStr("FOOD_AND_DRINK")},
	}

	mockIncomeCalc.On("CalculateBudgetIncome", ctx, budget, mock.AnythingOfType("time.Time"), mock.AnythingOfType("time.Time")).Return(3000.0, nil)
	mockRepo.On("UpdateBudgetIncome", ctx, "budget-999", 3000.0).Return(nil)
	mockRepo.On("GetCategoriesByBudgetID", ctx, "budget-999").Return(categories, nil)
	mockSpentCalc.On("CalculateCategorySpent", ctx, "user-999", &categories[0], mock.AnythingOfType("time.Time"), mock.AnythingOfType("time.Time")).Return(900.0, nil)
	mockRepo.On("UpdateCategorySpentAmount", ctx, "cat-1", 900.0).Return(nil)

	// Expect ProcessEventTrigger called with "budget_updated" (domain.EventBudgetUpdated)
	mockEventProc.On("ProcessEventTrigger", ctx, "user-999", domain.EventBudgetUpdated, mock.MatchedBy(func(data map[string]interface{}) bool {
		return data["budget_id"] == "budget-999" &&
			data["income_amount"] == 3000.0 &&
			data["total_spent"] == 900.0
	})).Return(nil)

	err := service.RecalculateBudget(ctx, budget)

	require.NoError(t, err)
	mockEventProc.AssertExpectations(t)
	mockRepo.AssertExpectations(t)
	mockIncomeCalc.AssertExpectations(t)
	mockSpentCalc.AssertExpectations(t)
}

// TestRecalculateBudget_NoEventProcessorWhenNil tests that without SetEventProcessor
// RecalculateBudget succeeds without calling any event processor (nil guard).
func TestRecalculateBudget_NoEventProcessorWhenNil(t *testing.T) {
	mockRepo := new(MockBudgetSyncRepository)
	mockIncomeCalc := new(MockBudgetIncomeCalculator)
	mockSpentCalc := new(MockBudgetSpentCalculator)
	mockEventProc := new(MockEventTriggerProcessor)
	logger := testSyncServiceLogger()

	// Intentionally do NOT call SetEventProcessor
	service := NewBudgetSyncService(mockRepo, mockIncomeCalc, mockSpentCalc, logger)

	ctx := context.Background()
	budget := &domain.Budget{
		ID:     "budget-888",
		UserID: "user-888",
		Period: domain.BudgetPeriodMonthly,
	}

	mockIncomeCalc.On("CalculateBudgetIncome", ctx, budget, mock.AnythingOfType("time.Time"), mock.AnythingOfType("time.Time")).Return(2000.0, nil)
	mockRepo.On("UpdateBudgetIncome", ctx, "budget-888", 2000.0).Return(nil)
	mockRepo.On("GetCategoriesByBudgetID", ctx, "budget-888").Return([]domain.BudgetCategory{}, nil)

	err := service.RecalculateBudget(ctx, budget)

	require.NoError(t, err)
	// ProcessEventTrigger must NOT be called when no event processor is set
	mockEventProc.AssertNotCalled(t, "ProcessEventTrigger")
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

// =============================================================================
// Issue #1270: Task 4 — EventData categories emitter tests
// =============================================================================

// TestRecalculateBudget_EmitsCategories verifies that RecalculateBudget emits the
// "categories" key in the budget_updated event payload (Issue #1270, Task 4).
func TestRecalculateBudget_EmitsCategories(t *testing.T) {
	mockRepo := new(MockBudgetSyncRepository)
	mockIncomeCalc := new(MockBudgetIncomeCalculator)
	mockSpentCalc := new(MockBudgetSpentCalculator)
	mockEvent := new(MockEventTriggerProcessor)
	logger := testSyncServiceLogger()

	svc := NewBudgetSyncService(mockRepo, mockIncomeCalc, mockSpentCalc, logger)
	svc.SetEventProcessor(mockEvent)
	ctx := context.Background()

	budget := &domain.Budget{
		ID:     "budget-abc",
		UserID: "user-abc",
		Period: domain.BudgetPeriodMonthly,
		Amount: 1000.0,
	}

	categories := []domain.BudgetCategory{
		{
			ID:              "cat-groceries",
			BudgetID:        "budget-abc",
			CategoryKind:    "pfc",
			PFCPrimary:      ptrStr("FOOD_AND_DRINK"),
			AllocatedAmount: 400.0,
			SpentAmount:     0.0, // set by mock below
		},
	}

	mockIncomeCalc.On("CalculateBudgetIncome", ctx, budget, mock.AnythingOfType("time.Time"), mock.AnythingOfType("time.Time")).Return(1000.0, nil)
	mockRepo.On("UpdateBudgetIncome", ctx, "budget-abc", 1000.0).Return(nil)
	mockRepo.On("GetCategoriesByBudgetID", ctx, "budget-abc").Return(categories, nil)
	mockSpentCalc.On("CalculateCategorySpent", ctx, "user-abc", &categories[0], mock.AnythingOfType("time.Time"), mock.AnythingOfType("time.Time")).Return(150.0, nil)
	mockRepo.On("UpdateCategorySpentAmount", ctx, "cat-groceries", 150.0).Return(nil)

	// Capture the event data passed to ProcessEventTrigger.
	var capturedEventData map[string]interface{}
	mockEvent.On("ProcessEventTrigger", ctx, "user-abc", "budget_updated", mock.AnythingOfType("map[string]interface {}")).
		Run(func(args mock.Arguments) {
			capturedEventData = args.Get(3).(map[string]interface{})
		}).
		Return(nil)

	err := svc.RecalculateBudget(ctx, budget)
	require.NoError(t, err)

	// Verify "categories" key is present in event data.
	require.NotNil(t, capturedEventData, "event data should have been captured")
	categoriesRaw, ok := capturedEventData[domain.EventDataKeyCategories]
	require.True(t, ok, "event data must contain 'categories' key")

	// Issue #1271 Item 5: BuildCategoryEventData returns map[string]map[string]float64.
	catMap, ok := categoriesRaw.(map[string]map[string]float64)
	require.True(t, ok, "categories value must be map[string]map[string]float64")
	require.Contains(t, catMap, "cat-groceries", "categories map must contain cat-groceries")

	perCat := catMap["cat-groceries"]
	assert.Equal(t, 150.0/400.0*100, perCat["percent_used"], "percent_used for cat-groceries")
	assert.Equal(t, 400.0-150.0, perCat["remaining"], "remaining for cat-groceries")
	// Issue #1271 Item 5: key renamed from "total_spent" to "spent".
	assert.Equal(t, 150.0, perCat["spent"], "spent for cat-groceries")
	assert.Equal(t, 400.0, perCat["allocated"], "allocated for cat-groceries")

	// Verify "remaining" key is present (from domain.EventDataKeyRemaining).
	_, hasRemaining := capturedEventData[domain.EventDataKeyRemaining]
	assert.True(t, hasRemaining, "event data must contain 'remaining' key")

	mockRepo.AssertExpectations(t)
	mockIncomeCalc.AssertExpectations(t)
	mockSpentCalc.AssertExpectations(t)
	mockEvent.AssertExpectations(t)
}

// TestRecalculateBudget_PercentUsedMatchesPeriodEnd verifies that budget_updated emits
// percent_used using Budget.PercentUsed() (denominator = Budget.Amount), matching what
// budget_period_end emits — even when incomeAmount != Budget.Amount. Both emitters must
// agree so that a budget.percent_used rule fires consistently regardless of which event
// triggered it. Issue #1270.
func TestRecalculateBudget_PercentUsedMatchesPeriodEnd(t *testing.T) {
	mockRepo := new(MockBudgetSyncRepository)
	mockIncomeCalc := new(MockBudgetIncomeCalculator)
	mockSpentCalc := new(MockBudgetSpentCalculator)
	mockEvent := new(MockEventTriggerProcessor)
	logger := testSyncServiceLogger()

	svc := NewBudgetSyncService(mockRepo, mockIncomeCalc, mockSpentCalc, logger)
	svc.SetEventProcessor(mockEvent)
	ctx := context.Background()

	// Budget.Amount = 4000; incomeAmount = 5000 — the denominators differ.
	// Old formula: percent_used = totalSpent/incomeAmount*100 = 2000/5000*100 = 40.0
	// Correct (Budget.PercentUsed()): totalSpent/Budget.Amount*100 = 2000/4000*100 = 50.0
	budget := &domain.Budget{
		ID:     "budget-pct",
		UserID: "user-pct",
		Period: domain.BudgetPeriodMonthly,
		Amount: 4000.0,
	}

	categories := []domain.BudgetCategory{
		{
			ID:              "cat-pct",
			BudgetID:        "budget-pct",
			CategoryKind:    "pfc",
			PFCPrimary:      ptrStr("FOOD_AND_DRINK"),
			AllocatedAmount: 2000.0,
		},
	}

	// incomeAmount (5000) != budget.Amount (4000)
	mockIncomeCalc.On("CalculateBudgetIncome", ctx, budget, mock.AnythingOfType("time.Time"), mock.AnythingOfType("time.Time")).Return(5000.0, nil)
	mockRepo.On("UpdateBudgetIncome", ctx, "budget-pct", 5000.0).Return(nil)
	mockRepo.On("GetCategoriesByBudgetID", ctx, "budget-pct").Return(categories, nil)
	mockSpentCalc.On("CalculateCategorySpent", ctx, "user-pct", &categories[0], mock.AnythingOfType("time.Time"), mock.AnythingOfType("time.Time")).Return(2000.0, nil)
	mockRepo.On("UpdateCategorySpentAmount", ctx, "cat-pct", 2000.0).Return(nil)

	var capturedData map[string]interface{}
	mockEvent.On("ProcessEventTrigger", ctx, "user-pct", domain.EventBudgetUpdated, mock.AnythingOfType("map[string]interface {}")).
		Run(func(args mock.Arguments) {
			capturedData = args.Get(3).(map[string]interface{})
		}).
		Return(nil)

	err := svc.RecalculateBudget(ctx, budget)
	require.NoError(t, err)

	require.NotNil(t, capturedData, "event data must be captured")

	// percent_used must equal Budget{Amount:4000, Categories:...}.PercentUsed() = 50.0,
	// NOT the income-based formula 2000/5000*100 = 40.0.
	expectedPct := (&domain.Budget{Amount: 4000.0, Categories: categories}).PercentUsed()
	assert.InEpsilon(t, expectedPct, capturedData[domain.EventDataKeyPercentUsed],
		0.0001, "percent_used must use Budget.PercentUsed() not incomeAmount as denominator")
	assert.NotEqual(t, 40.0, capturedData[domain.EventDataKeyPercentUsed],
		"percent_used must NOT use incomeAmount (5000) as denominator")

	mockRepo.AssertExpectations(t)
	mockIncomeCalc.AssertExpectations(t)
	mockSpentCalc.AssertExpectations(t)
	mockEvent.AssertExpectations(t)
}

// ─── Savings category spent branch (Issue #1300) ────────────────────────────

// MockSavingsContributionSpentCalc is a mock for SavingsContributionSpentCalc.
type MockSavingsContributionSpentCalc struct {
	mock.Mock
}

func (m *MockSavingsContributionSpentCalc) SumConfirmedByGoalInPeriod(ctx context.Context, goalID string, start, end time.Time) (float64, error) {
	args := m.Called(ctx, goalID, start, end)
	return args.Get(0).(float64), args.Error(1)
}

// TestRecalculateBudget_SavingsCategory_UsesContributionLedger verifies that
// a savings-kind category derives its spent amount from the contribution ledger
// (SumConfirmedByGoalInPeriod), NOT from the standard transaction-matching path
// (Plan flag A — savings counts as real envelope money; Issue #1300).
func TestRecalculateBudget_SavingsCategory_UsesContributionLedger(t *testing.T) {
	ctx := context.Background()
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))

	mockRepo := new(MockBudgetSyncRepository)
	mockIncomeCalc := new(MockBudgetIncomeCalculator)
	mockSpentCalc := new(MockBudgetSpentCalculator)
	mockSavingsCalc := new(MockSavingsContributionSpentCalc)

	svc := NewBudgetSyncService(mockRepo, mockIncomeCalc, mockSpentCalc, logger)
	svc.SetSavingsSpentCalc(mockSavingsCalc)

	goalID := "goal-abc"
	budget := &domain.Budget{
		ID:     "budget-1",
		UserID: "user-1",
		Period: domain.BudgetPeriodMonthly,
		Amount: 1000.0,
	}

	goalIDPtr := goalID
	savingsCategory := domain.BudgetCategory{
		ID:            "cat-savings",
		BudgetID:      budget.ID,
		CategoryKind:  "savings",
		SavingsGoalID: &goalIDPtr,
	}

	mockRepo.On("GetCategoriesByBudgetID", ctx, budget.ID).Return([]domain.BudgetCategory{savingsCategory}, nil)
	mockIncomeCalc.On("CalculateBudgetIncome", ctx, budget, mock.Anything, mock.Anything).Return(0.0, nil)
	mockRepo.On("UpdateBudgetIncome", ctx, budget.ID, 0.0).Return(nil)

	// Savings calc returns 300 for the goal's confirmed contributions.
	mockSavingsCalc.On("SumConfirmedByGoalInPeriod", ctx, goalID, mock.Anything, mock.Anything).Return(300.0, nil)
	mockRepo.On("UpdateCategorySpentAmount", ctx, savingsCategory.ID, 300.0).Return(nil)

	err := svc.RecalculateBudget(ctx, budget)

	require.NoError(t, err)
	// The standard spent calculator should NOT be called for a savings category.
	mockSpentCalc.AssertNotCalled(t, "CalculateCategorySpent")
	mockSavingsCalc.AssertExpectations(t)
	mockRepo.AssertExpectations(t)
}

// TestRecalculateBudget_SavingsCategory_WithoutCalc_FallsBackToStandard verifies
// that a savings category falls back to the standard spent calculator when no
// SavingsContributionSpentCalc is wired (graceful degradation / backward compat).
func TestRecalculateBudget_SavingsCategory_WithoutCalc_FallsBackToStandard(t *testing.T) {
	ctx := context.Background()
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))

	mockRepo := new(MockBudgetSyncRepository)
	mockIncomeCalc := new(MockBudgetIncomeCalculator)
	mockSpentCalc := new(MockBudgetSpentCalculator)

	// No SetSavingsSpentCalc wired.
	svc := NewBudgetSyncService(mockRepo, mockIncomeCalc, mockSpentCalc, logger)

	goalID := "goal-xyz"
	budget := &domain.Budget{
		ID:     "budget-2",
		UserID: "user-2",
		Period: domain.BudgetPeriodMonthly,
		Amount: 1000.0,
	}

	goalIDPtr := goalID
	savingsCategory := domain.BudgetCategory{
		ID:            "cat-savings-2",
		BudgetID:      budget.ID,
		CategoryKind:  "savings",
		SavingsGoalID: &goalIDPtr,
	}

	mockRepo.On("GetCategoriesByBudgetID", ctx, budget.ID).Return([]domain.BudgetCategory{savingsCategory}, nil)
	mockIncomeCalc.On("CalculateBudgetIncome", ctx, budget, mock.Anything, mock.Anything).Return(0.0, nil)
	mockRepo.On("UpdateBudgetIncome", ctx, budget.ID, 0.0).Return(nil)

	// Falls back to standard path.
	mockSpentCalc.On("CalculateCategorySpent", ctx, budget.UserID, &savingsCategory, mock.Anything, mock.Anything).Return(150.0, nil)
	mockRepo.On("UpdateCategorySpentAmount", ctx, savingsCategory.ID, 150.0).Return(nil)

	err := svc.RecalculateBudget(ctx, budget)

	require.NoError(t, err)
	mockSpentCalc.AssertExpectations(t)
	mockRepo.AssertExpectations(t)
}
