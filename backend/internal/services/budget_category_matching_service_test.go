package services

import (
	"context"
	"encoding/json"
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

func (m *MockSpentAmountCalculator) GetSpentSumForBudgetCategory(ctx context.Context, userID string, category []string, tags []string, matchLogic domain.MatchLogic, startDate, endDate time.Time) (float64, error) {
	args := m.Called(ctx, userID, category, tags, matchLogic, startDate, endDate)
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
		ID:         "cat-123",
		BudgetID:   "budget-123",
		Category:   []string{"Food and Drink"},
		Tags:       []string{},
		MatchLogic: domain.MatchLogicCategoryOnly,
	}
	startDate := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	endDate := time.Date(2024, 1, 31, 23, 59, 59, 0, time.UTC)
	expectedSum := 250.50

	mockCalculator.On("GetSpentSumForBudgetCategory", ctx, userID, category.Category, category.Tags, domain.MatchLogicCategoryOnly, startDate, endDate).Return(expectedSum, nil)

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
		Category:   []string{},
		Tags:       []string{"groceries", "food"},
		MatchLogic: domain.MatchLogicTagOnly,
	}
	startDate := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	endDate := time.Date(2024, 1, 31, 23, 59, 59, 0, time.UTC)
	expectedSum := 175.25

	mockCalculator.On("GetSpentSumForBudgetCategory", ctx, userID, category.Category, category.Tags, domain.MatchLogicTagOnly, startDate, endDate).Return(expectedSum, nil)

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
		ID:         "cat-123",
		BudgetID:   "budget-123",
		Category:   []string{"Food and Drink", "Restaurants"},
		Tags:       []string{"dinner", "date-night"},
		MatchLogic: domain.MatchLogicAnd,
	}
	startDate := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	endDate := time.Date(2024, 1, 31, 23, 59, 59, 0, time.UTC)
	expectedSum := 125.00

	mockCalculator.On("GetSpentSumForBudgetCategory", ctx, userID, category.Category, category.Tags, domain.MatchLogicAnd, startDate, endDate).Return(expectedSum, nil)

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
		ID:         "cat-123",
		BudgetID:   "budget-123",
		Category:   []string{"Entertainment"},
		Tags:       []string{"streaming", "subscriptions"},
		MatchLogic: domain.MatchLogicOr,
	}
	startDate := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	endDate := time.Date(2024, 1, 31, 23, 59, 59, 0, time.UTC)
	expectedSum := 89.97

	mockCalculator.On("GetSpentSumForBudgetCategory", ctx, userID, category.Category, category.Tags, domain.MatchLogicOr, startDate, endDate).Return(expectedSum, nil)

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
		ID:         "cat-123",
		BudgetID:   "budget-123",
		Category:   []string{"Shopping"},
		Tags:       []string{},
		MatchLogic: "", // Empty defaults to category_only
	}
	startDate := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	endDate := time.Date(2024, 1, 31, 23, 59, 59, 0, time.UTC)
	expectedSum := 500.00

	// Expect category_only to be used
	mockCalculator.On("GetSpentSumForBudgetCategory", ctx, userID, category.Category, category.Tags, domain.MatchLogicCategoryOnly, startDate, endDate).Return(expectedSum, nil)

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
		ID:         "cat-123",
		BudgetID:   "budget-123",
		Category:   []string{"Food and Drink"},
		Tags:       []string{},
		MatchLogic: domain.MatchLogicCategoryOnly,
	}
	startDate := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	endDate := time.Date(2024, 1, 31, 23, 59, 59, 0, time.UTC)

	mockCalculator.On("GetSpentSumForBudgetCategory", ctx, userID, category.Category, category.Tags, domain.MatchLogicCategoryOnly, startDate, endDate).Return(0.0, errors.New("database connection failed"))

	sum, err := service.CalculateCategorySpent(ctx, userID, category, startDate, endDate)
	assert.Error(t, err)
	assert.Equal(t, 0.0, sum)

	mockCalculator.AssertExpectations(t)
}

// =============================================================================
// DoesTransactionMatchCategory Tests
// =============================================================================

// Helper function to create a category JSON string pointer
func categoryJSON(cats []string) *string {
	b, _ := json.Marshal(cats)
	s := string(b)
	return &s
}

// TestDoesTransactionMatchCategory_CategoryOnly_MatchesPrimaryCategory tests category_only with primary category match
func TestDoesTransactionMatchCategory_CategoryOnly_MatchesPrimaryCategory(t *testing.T) {
	mockCalculator := new(MockSpentAmountCalculator)
	service := NewBudgetCategoryMatchingService(mockCalculator)

	transaction := &domain.Transaction{
		ID:       "tx-123",
		Category: categoryJSON([]string{"Food and Drink", "Restaurants"}),
	}
	budgetCategory := &domain.BudgetCategory{
		Category:   []string{"Food and Drink"},
		MatchLogic: domain.MatchLogicCategoryOnly,
	}

	matches := service.DoesTransactionMatchCategory(transaction, budgetCategory)
	assert.True(t, matches)
}

// TestDoesTransactionMatchCategory_CategoryOnly_MatchesFullHierarchy tests exact hierarchy match
func TestDoesTransactionMatchCategory_CategoryOnly_MatchesFullHierarchy(t *testing.T) {
	mockCalculator := new(MockSpentAmountCalculator)
	service := NewBudgetCategoryMatchingService(mockCalculator)

	transaction := &domain.Transaction{
		ID:       "tx-123",
		Category: categoryJSON([]string{"Food and Drink", "Restaurants"}),
	}
	budgetCategory := &domain.BudgetCategory{
		Category:   []string{"Food and Drink", "Restaurants"},
		MatchLogic: domain.MatchLogicCategoryOnly,
	}

	matches := service.DoesTransactionMatchCategory(transaction, budgetCategory)
	assert.True(t, matches)
}

// TestDoesTransactionMatchCategory_CategoryOnly_UsesCategoryOverride tests override takes precedence
func TestDoesTransactionMatchCategory_CategoryOnly_UsesCategoryOverride(t *testing.T) {
	mockCalculator := new(MockSpentAmountCalculator)
	service := NewBudgetCategoryMatchingService(mockCalculator)

	// Original category doesn't match, but override does
	transaction := &domain.Transaction{
		ID:               "tx-123",
		Category:         categoryJSON([]string{"Shopping", "Electronics"}),
		CategoryOverride: categoryJSON([]string{"Food and Drink", "Coffee"}),
	}
	budgetCategory := &domain.BudgetCategory{
		Category:   []string{"Food and Drink"},
		MatchLogic: domain.MatchLogicCategoryOnly,
	}

	matches := service.DoesTransactionMatchCategory(transaction, budgetCategory)
	assert.True(t, matches)
}

// TestDoesTransactionMatchCategory_CategoryOnly_NoMatch tests no category match
func TestDoesTransactionMatchCategory_CategoryOnly_NoMatch(t *testing.T) {
	mockCalculator := new(MockSpentAmountCalculator)
	service := NewBudgetCategoryMatchingService(mockCalculator)

	transaction := &domain.Transaction{
		ID:       "tx-123",
		Category: categoryJSON([]string{"Shopping", "Electronics"}),
	}
	budgetCategory := &domain.BudgetCategory{
		Category:   []string{"Food and Drink"},
		MatchLogic: domain.MatchLogicCategoryOnly,
	}

	matches := service.DoesTransactionMatchCategory(transaction, budgetCategory)
	assert.False(t, matches)
}

// TestDoesTransactionMatchCategory_TagOnly_MatchesSingleTag tests tag_only with single tag match
func TestDoesTransactionMatchCategory_TagOnly_MatchesSingleTag(t *testing.T) {
	mockCalculator := new(MockSpentAmountCalculator)
	service := NewBudgetCategoryMatchingService(mockCalculator)

	transaction := &domain.Transaction{
		ID:   "tx-123",
		Tags: []string{"groceries"},
	}
	budgetCategory := &domain.BudgetCategory{
		Tags:       []string{"groceries", "food"},
		MatchLogic: domain.MatchLogicTagOnly,
	}

	matches := service.DoesTransactionMatchCategory(transaction, budgetCategory)
	assert.True(t, matches)
}

// TestDoesTransactionMatchCategory_TagOnly_MatchesAnyTag tests tag_only with any tag match
func TestDoesTransactionMatchCategory_TagOnly_MatchesAnyTag(t *testing.T) {
	mockCalculator := new(MockSpentAmountCalculator)
	service := NewBudgetCategoryMatchingService(mockCalculator)

	transaction := &domain.Transaction{
		ID:   "tx-123",
		Tags: []string{"dinner", "weekend"},
	}
	budgetCategory := &domain.BudgetCategory{
		Tags:       []string{"lunch", "dinner", "breakfast"},
		MatchLogic: domain.MatchLogicTagOnly,
	}

	matches := service.DoesTransactionMatchCategory(transaction, budgetCategory)
	assert.True(t, matches)
}

// TestDoesTransactionMatchCategory_TagOnly_CaseInsensitive tests case-insensitive tag matching
func TestDoesTransactionMatchCategory_TagOnly_CaseInsensitive(t *testing.T) {
	mockCalculator := new(MockSpentAmountCalculator)
	service := NewBudgetCategoryMatchingService(mockCalculator)

	transaction := &domain.Transaction{
		ID:   "tx-123",
		Tags: []string{"GROCERIES"},
	}
	budgetCategory := &domain.BudgetCategory{
		Tags:       []string{"groceries"},
		MatchLogic: domain.MatchLogicTagOnly,
	}

	matches := service.DoesTransactionMatchCategory(transaction, budgetCategory)
	assert.True(t, matches)
}

// TestDoesTransactionMatchCategory_TagOnly_NoMatch tests no tag match
func TestDoesTransactionMatchCategory_TagOnly_NoMatch(t *testing.T) {
	mockCalculator := new(MockSpentAmountCalculator)
	service := NewBudgetCategoryMatchingService(mockCalculator)

	transaction := &domain.Transaction{
		ID:   "tx-123",
		Tags: []string{"entertainment"},
	}
	budgetCategory := &domain.BudgetCategory{
		Tags:       []string{"groceries", "food"},
		MatchLogic: domain.MatchLogicTagOnly,
	}

	matches := service.DoesTransactionMatchCategory(transaction, budgetCategory)
	assert.False(t, matches)
}

// TestDoesTransactionMatchCategory_And_RequiresBoth tests and logic requires both matches
func TestDoesTransactionMatchCategory_And_RequiresBoth(t *testing.T) {
	mockCalculator := new(MockSpentAmountCalculator)
	service := NewBudgetCategoryMatchingService(mockCalculator)

	transaction := &domain.Transaction{
		ID:       "tx-123",
		Category: categoryJSON([]string{"Food and Drink", "Restaurants"}),
		Tags:     []string{"dinner", "date-night"},
	}
	budgetCategory := &domain.BudgetCategory{
		Category:   []string{"Food and Drink"},
		Tags:       []string{"dinner"},
		MatchLogic: domain.MatchLogicAnd,
	}

	matches := service.DoesTransactionMatchCategory(transaction, budgetCategory)
	assert.True(t, matches)
}

// TestDoesTransactionMatchCategory_And_FailsOnMissingCategory tests and fails without category
func TestDoesTransactionMatchCategory_And_FailsOnMissingCategory(t *testing.T) {
	mockCalculator := new(MockSpentAmountCalculator)
	service := NewBudgetCategoryMatchingService(mockCalculator)

	transaction := &domain.Transaction{
		ID:       "tx-123",
		Category: categoryJSON([]string{"Shopping"}),
		Tags:     []string{"dinner"},
	}
	budgetCategory := &domain.BudgetCategory{
		Category:   []string{"Food and Drink"},
		Tags:       []string{"dinner"},
		MatchLogic: domain.MatchLogicAnd,
	}

	matches := service.DoesTransactionMatchCategory(transaction, budgetCategory)
	assert.False(t, matches)
}

// TestDoesTransactionMatchCategory_And_FailsOnMissingTag tests and fails without tag
func TestDoesTransactionMatchCategory_And_FailsOnMissingTag(t *testing.T) {
	mockCalculator := new(MockSpentAmountCalculator)
	service := NewBudgetCategoryMatchingService(mockCalculator)

	transaction := &domain.Transaction{
		ID:       "tx-123",
		Category: categoryJSON([]string{"Food and Drink", "Restaurants"}),
		Tags:     []string{"lunch"},
	}
	budgetCategory := &domain.BudgetCategory{
		Category:   []string{"Food and Drink"},
		Tags:       []string{"dinner"},
		MatchLogic: domain.MatchLogicAnd,
	}

	matches := service.DoesTransactionMatchCategory(transaction, budgetCategory)
	assert.False(t, matches)
}

// TestDoesTransactionMatchCategory_Or_MatchesCategoryOnly tests or matches with category only
func TestDoesTransactionMatchCategory_Or_MatchesCategoryOnly(t *testing.T) {
	mockCalculator := new(MockSpentAmountCalculator)
	service := NewBudgetCategoryMatchingService(mockCalculator)

	transaction := &domain.Transaction{
		ID:       "tx-123",
		Category: categoryJSON([]string{"Food and Drink", "Restaurants"}),
		Tags:     []string{"unrelated"},
	}
	budgetCategory := &domain.BudgetCategory{
		Category:   []string{"Food and Drink"},
		Tags:       []string{"dinner"},
		MatchLogic: domain.MatchLogicOr,
	}

	matches := service.DoesTransactionMatchCategory(transaction, budgetCategory)
	assert.True(t, matches)
}

// TestDoesTransactionMatchCategory_Or_MatchesTagOnly tests or matches with tag only
func TestDoesTransactionMatchCategory_Or_MatchesTagOnly(t *testing.T) {
	mockCalculator := new(MockSpentAmountCalculator)
	service := NewBudgetCategoryMatchingService(mockCalculator)

	transaction := &domain.Transaction{
		ID:       "tx-123",
		Category: categoryJSON([]string{"Shopping"}),
		Tags:     []string{"dinner"},
	}
	budgetCategory := &domain.BudgetCategory{
		Category:   []string{"Food and Drink"},
		Tags:       []string{"dinner"},
		MatchLogic: domain.MatchLogicOr,
	}

	matches := service.DoesTransactionMatchCategory(transaction, budgetCategory)
	assert.True(t, matches)
}

// TestDoesTransactionMatchCategory_Or_NoMatch tests or fails without either match
func TestDoesTransactionMatchCategory_Or_NoMatch(t *testing.T) {
	mockCalculator := new(MockSpentAmountCalculator)
	service := NewBudgetCategoryMatchingService(mockCalculator)

	transaction := &domain.Transaction{
		ID:       "tx-123",
		Category: categoryJSON([]string{"Shopping"}),
		Tags:     []string{"electronics"},
	}
	budgetCategory := &domain.BudgetCategory{
		Category:   []string{"Food and Drink"},
		Tags:       []string{"dinner"},
		MatchLogic: domain.MatchLogicOr,
	}

	matches := service.DoesTransactionMatchCategory(transaction, budgetCategory)
	assert.False(t, matches)
}

// TestDoesTransactionMatchCategory_NilTransaction tests nil transaction returns false
func TestDoesTransactionMatchCategory_NilTransaction(t *testing.T) {
	mockCalculator := new(MockSpentAmountCalculator)
	service := NewBudgetCategoryMatchingService(mockCalculator)

	budgetCategory := &domain.BudgetCategory{
		Category:   []string{"Food and Drink"},
		MatchLogic: domain.MatchLogicCategoryOnly,
	}

	matches := service.DoesTransactionMatchCategory(nil, budgetCategory)
	assert.False(t, matches)
}

// TestDoesTransactionMatchCategory_NilCategory tests nil budget category returns false
func TestDoesTransactionMatchCategory_NilCategory(t *testing.T) {
	mockCalculator := new(MockSpentAmountCalculator)
	service := NewBudgetCategoryMatchingService(mockCalculator)

	transaction := &domain.Transaction{
		ID:       "tx-123",
		Category: categoryJSON([]string{"Food and Drink"}),
	}

	matches := service.DoesTransactionMatchCategory(transaction, nil)
	assert.False(t, matches)
}

// TestDoesTransactionMatchCategory_EmptyMatchLogic tests empty match logic defaults to category_only
func TestDoesTransactionMatchCategory_EmptyMatchLogic(t *testing.T) {
	mockCalculator := new(MockSpentAmountCalculator)
	service := NewBudgetCategoryMatchingService(mockCalculator)

	transaction := &domain.Transaction{
		ID:       "tx-123",
		Category: categoryJSON([]string{"Food and Drink", "Restaurants"}),
	}
	budgetCategory := &domain.BudgetCategory{
		Category:   []string{"Food and Drink"},
		MatchLogic: "", // Empty defaults to category_only
	}

	matches := service.DoesTransactionMatchCategory(transaction, budgetCategory)
	assert.True(t, matches)
}

// TestDoesTransactionMatchCategory_NilTransactionCategory tests transaction with nil category
func TestDoesTransactionMatchCategory_NilTransactionCategory(t *testing.T) {
	mockCalculator := new(MockSpentAmountCalculator)
	service := NewBudgetCategoryMatchingService(mockCalculator)

	transaction := &domain.Transaction{
		ID:       "tx-123",
		Category: nil,
	}
	budgetCategory := &domain.BudgetCategory{
		Category:   []string{"Food and Drink"},
		MatchLogic: domain.MatchLogicCategoryOnly,
	}

	matches := service.DoesTransactionMatchCategory(transaction, budgetCategory)
	assert.False(t, matches)
}

// TestDoesTransactionMatchCategory_EmptyTags tests transaction with empty tags for tag_only
func TestDoesTransactionMatchCategory_EmptyTags(t *testing.T) {
	mockCalculator := new(MockSpentAmountCalculator)
	service := NewBudgetCategoryMatchingService(mockCalculator)

	transaction := &domain.Transaction{
		ID:   "tx-123",
		Tags: []string{},
	}
	budgetCategory := &domain.BudgetCategory{
		Tags:       []string{"groceries"},
		MatchLogic: domain.MatchLogicTagOnly,
	}

	matches := service.DoesTransactionMatchCategory(transaction, budgetCategory)
	assert.False(t, matches)
}
