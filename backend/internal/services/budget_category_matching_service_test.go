package services

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// =============================================================================
// Issue #493: BudgetCategoryMatchingService Tests
// =============================================================================

// MockSpentAmountCalculator is a mock implementation of SpentAmountCalculator
type MockSpentAmountCalculator struct {
	mock.Mock
}

func (m *MockSpentAmountCalculator) GetSpentSumForBudgetCategory(ctx context.Context, userID string, budgetCat *domain.BudgetCategory, startDate, endDate time.Time) (float64, error) {
	args := m.Called(ctx, userID, budgetCat, startDate, endDate)
	return args.Get(0).(float64), args.Error(1)
}

// =============================================================================
// CalculateCategorySpent Tests
// =============================================================================

// TestCalculateCategorySpent_CategoryOnly tests delegation to repository for category_only match logic
func TestCalculateCategorySpent_CategoryOnly(t *testing.T) {
	mockCalculator := new(MockSpentAmountCalculator)
	service := NewBudgetCategoryMatchingService(mockCalculator)
	ctx := context.Background()

	userID := "user-123"
	category := &domain.BudgetCategory{
		ID:           "cat-123",
		BudgetID:     "budget-123",
		CategoryKind: "pfc",
		PFCPrimary:   ptrStr("FOOD_AND_DRINK"),
		Tags:         []string{},
		MatchLogic:   domain.MatchLogicCategoryOnly,
	}
	startDate := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	endDate := time.Date(2024, 1, 31, 23, 59, 59, 0, time.UTC)
	expectedSum := 250.50

	mockCalculator.On("GetSpentSumForBudgetCategory", ctx, userID, category, startDate, endDate).Return(expectedSum, nil)

	sum, err := service.CalculateCategorySpent(ctx, userID, category, startDate, endDate)
	require.NoError(t, err)
	assert.Equal(t, expectedSum, sum)

	mockCalculator.AssertExpectations(t)
}

// TestCalculateCategorySpent_TagOnly tests delegation to repository for tag_only match logic
func TestCalculateCategorySpent_TagOnly(t *testing.T) {
	mockCalculator := new(MockSpentAmountCalculator)
	service := NewBudgetCategoryMatchingService(mockCalculator)
	ctx := context.Background()

	userID := "user-123"
	category := &domain.BudgetCategory{
		ID:         "cat-123",
		BudgetID:   "budget-123",
		Tags:       []string{"groceries", "food"},
		MatchLogic: domain.MatchLogicTagOnly,
	}
	startDate := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	endDate := time.Date(2024, 1, 31, 23, 59, 59, 0, time.UTC)
	expectedSum := 175.25

	mockCalculator.On("GetSpentSumForBudgetCategory", ctx, userID, category, startDate, endDate).Return(expectedSum, nil)

	sum, err := service.CalculateCategorySpent(ctx, userID, category, startDate, endDate)
	require.NoError(t, err)
	assert.Equal(t, expectedSum, sum)

	mockCalculator.AssertExpectations(t)
}

// TestCalculateCategorySpent_And tests delegation to repository for and match logic
func TestCalculateCategorySpent_And(t *testing.T) {
	mockCalculator := new(MockSpentAmountCalculator)
	service := NewBudgetCategoryMatchingService(mockCalculator)
	ctx := context.Background()

	userID := "user-123"
	category := &domain.BudgetCategory{
		ID:           "cat-123",
		BudgetID:     "budget-123",
		CategoryKind: "pfc",
		PFCPrimary:   ptrStr("FOOD_AND_DRINK"),
		Tags:         []string{"dinner", "date-night"},
		MatchLogic:   domain.MatchLogicAnd,
	}
	startDate := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	endDate := time.Date(2024, 1, 31, 23, 59, 59, 0, time.UTC)
	expectedSum := 125.00

	mockCalculator.On("GetSpentSumForBudgetCategory", ctx, userID, category, startDate, endDate).Return(expectedSum, nil)

	sum, err := service.CalculateCategorySpent(ctx, userID, category, startDate, endDate)
	require.NoError(t, err)
	assert.Equal(t, expectedSum, sum)

	mockCalculator.AssertExpectations(t)
}

// TestCalculateCategorySpent_Or tests delegation to repository for or match logic
func TestCalculateCategorySpent_Or(t *testing.T) {
	mockCalculator := new(MockSpentAmountCalculator)
	service := NewBudgetCategoryMatchingService(mockCalculator)
	ctx := context.Background()

	userID := "user-123"
	category := &domain.BudgetCategory{
		ID:           "cat-123",
		BudgetID:     "budget-123",
		CategoryKind: "pfc",
		PFCPrimary:   ptrStr("ENTERTAINMENT"),
		Tags:         []string{"streaming", "subscriptions"},
		MatchLogic:   domain.MatchLogicOr,
	}
	startDate := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	endDate := time.Date(2024, 1, 31, 23, 59, 59, 0, time.UTC)
	expectedSum := 89.97

	mockCalculator.On("GetSpentSumForBudgetCategory", ctx, userID, category, startDate, endDate).Return(expectedSum, nil)

	sum, err := service.CalculateCategorySpent(ctx, userID, category, startDate, endDate)
	require.NoError(t, err)
	assert.Equal(t, expectedSum, sum)

	mockCalculator.AssertExpectations(t)
}

// TestCalculateCategorySpent_EmptyMatchLogic tests that empty match_logic defaults to category_only
func TestCalculateCategorySpent_EmptyMatchLogic(t *testing.T) {
	mockCalculator := new(MockSpentAmountCalculator)
	service := NewBudgetCategoryMatchingService(mockCalculator)
	ctx := context.Background()

	userID := "user-123"
	category := &domain.BudgetCategory{
		ID:           "cat-123",
		BudgetID:     "budget-123",
		CategoryKind: "pfc",
		PFCPrimary:   ptrStr("GENERAL_MERCHANDISE"),
		Tags:         []string{},
		MatchLogic:   "", // Empty defaults to category_only
	}
	startDate := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	endDate := time.Date(2024, 1, 31, 23, 59, 59, 0, time.UTC)
	expectedSum := 500.00

	// Expect category_only to be used
	mockCalculator.On("GetSpentSumForBudgetCategory", ctx, userID, category, startDate, endDate).Return(expectedSum, nil)

	sum, err := service.CalculateCategorySpent(ctx, userID, category, startDate, endDate)
	require.NoError(t, err)
	assert.Equal(t, expectedSum, sum)

	mockCalculator.AssertExpectations(t)
}

// TestCalculateCategorySpent_NilCategory tests error handling for nil category
func TestCalculateCategorySpent_NilCategory(t *testing.T) {
	mockCalculator := new(MockSpentAmountCalculator)
	service := NewBudgetCategoryMatchingService(mockCalculator)
	ctx := context.Background()

	userID := "user-123"
	startDate := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	endDate := time.Date(2024, 1, 31, 23, 59, 59, 0, time.UTC)

	sum, err := service.CalculateCategorySpent(ctx, userID, nil, startDate, endDate)
	assert.Error(t, err)
	assert.Equal(t, 0.0, sum)
	assert.Contains(t, err.Error(), "category is required")

	// No mock calls expected
	mockCalculator.AssertNotCalled(t, "GetSpentSumForBudgetCategory")
}

// TestCalculateCategorySpent_RepositoryError tests error propagation from repository
func TestCalculateCategorySpent_RepositoryError(t *testing.T) {
	mockCalculator := new(MockSpentAmountCalculator)
	service := NewBudgetCategoryMatchingService(mockCalculator)
	ctx := context.Background()

	userID := "user-123"
	category := &domain.BudgetCategory{
		ID:           "cat-123",
		BudgetID:     "budget-123",
		CategoryKind: "pfc",
		PFCPrimary:   ptrStr("FOOD_AND_DRINK"),
		Tags:         []string{},
		MatchLogic:   domain.MatchLogicCategoryOnly,
	}
	startDate := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	endDate := time.Date(2024, 1, 31, 23, 59, 59, 0, time.UTC)

	mockCalculator.On("GetSpentSumForBudgetCategory", ctx, userID, category, startDate, endDate).Return(0.0, errors.New("database connection failed"))

	sum, err := service.CalculateCategorySpent(ctx, userID, category, startDate, endDate)
	assert.Error(t, err)
	assert.Equal(t, 0.0, sum)

	mockCalculator.AssertExpectations(t)
}
