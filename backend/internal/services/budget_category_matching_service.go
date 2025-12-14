package services

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
)

// =============================================================================
// Issue #493: Budget Category Matching Service
// =============================================================================

// SpentAmountCalculator is a minimal interface for calculating spent amounts.
// This follows the minimal dependency interface pattern used throughout the codebase.
type SpentAmountCalculator interface {
	GetSpentSumForBudgetCategory(ctx context.Context, userID string, category []string, tags []string, matchLogic domain.MatchLogic, startDate, endDate time.Time) (float64, error)
}

// BudgetCategoryMatchingService calculates category spending with flexible matching logic.
// It supports four match logic types:
//   - category_only: Match transactions where category contains the budget category
//   - tag_only: Match transactions where tags contain ANY of the budget tags
//   - and: Match transactions where BOTH category AND tags match
//   - or: Match transactions where EITHER category OR tags match
type BudgetCategoryMatchingService interface {
	// CalculateCategorySpent returns the total spent amount for a budget category
	// within the given date range, using the category's match logic.
	CalculateCategorySpent(ctx context.Context, userID string, category *domain.BudgetCategory, startDate, endDate time.Time) (float64, error)

	// DoesTransactionMatchCategory checks if a transaction matches a budget category
	// based on the category's match logic. Useful for real-time UI updates.
	DoesTransactionMatchCategory(transaction *domain.Transaction, category *domain.BudgetCategory) bool
}

// budgetCategoryMatchingService implements BudgetCategoryMatchingService
type budgetCategoryMatchingService struct {
	spentCalculator SpentAmountCalculator
}

// NewBudgetCategoryMatchingService creates a new BudgetCategoryMatchingService instance
func NewBudgetCategoryMatchingService(spentCalculator SpentAmountCalculator) BudgetCategoryMatchingService {
	return &budgetCategoryMatchingService{spentCalculator: spentCalculator}
}

// CalculateCategorySpent returns the total spent amount for a budget category
// within the given date range, using the category's match logic.
func (s *budgetCategoryMatchingService) CalculateCategorySpent(ctx context.Context, userID string, category *domain.BudgetCategory, startDate, endDate time.Time) (float64, error) {
	if category == nil {
		return 0, errors.New("category is required")
	}

	matchLogic := category.MatchLogic
	if matchLogic == "" {
		matchLogic = domain.MatchLogicCategoryOnly
	}

	return s.spentCalculator.GetSpentSumForBudgetCategory(ctx, userID, category.Category, category.Tags, matchLogic, startDate, endDate)
}

// DoesTransactionMatchCategory checks if a transaction matches a budget category
// based on the category's match logic.
func (s *budgetCategoryMatchingService) DoesTransactionMatchCategory(transaction *domain.Transaction, category *domain.BudgetCategory) bool {
	if transaction == nil || category == nil {
		return false
	}

	matchLogic := category.MatchLogic
	if matchLogic == "" {
		matchLogic = domain.MatchLogicCategoryOnly
	}

	switch matchLogic {
	case domain.MatchLogicCategoryOnly:
		return s.matchesCategory(transaction, category.Category)
	case domain.MatchLogicTagOnly:
		return s.matchesTags(transaction, category.Tags)
	case domain.MatchLogicAnd:
		return s.matchesCategory(transaction, category.Category) && s.matchesTags(transaction, category.Tags)
	case domain.MatchLogicOr:
		return s.matchesCategory(transaction, category.Category) || s.matchesTags(transaction, category.Tags)
	default:
		return false
	}
}

// matchesCategory checks if transaction's category or category_override contains all elements of budget category.
// This implements JSONB containment (@>) semantics: transaction category must contain all budget category elements.
// For example:
//   - Budget category ["Food and Drink"] matches transaction ["Food and Drink", "Restaurants"] (hierarchy match)
//   - Budget category ["Food and Drink", "Restaurants"] matches transaction ["Food and Drink", "Restaurants"] (exact match)
//   - Budget category ["Food and Drink", "Restaurants"] does NOT match transaction ["Food and Drink"] (subcategory doesn't exist)
func (s *budgetCategoryMatchingService) matchesCategory(tx *domain.Transaction, budgetCategory []string) bool {
	if len(budgetCategory) == 0 {
		return true // Empty budget category matches everything
	}

	// Check category_override first (user preference takes precedence)
	if tx.CategoryOverride != nil {
		var override []string
		if err := json.Unmarshal([]byte(*tx.CategoryOverride), &override); err == nil {
			if containsAll(override, budgetCategory) {
				return true
			}
		}
	}

	// Then check original category
	if tx.Category != nil {
		var category []string
		if err := json.Unmarshal([]byte(*tx.Category), &category); err == nil {
			return containsAll(category, budgetCategory)
		}
	}

	return false
}

// matchesTags checks if transaction has ANY of the budget category's tags.
// Case-insensitive matching.
func (s *budgetCategoryMatchingService) matchesTags(tx *domain.Transaction, budgetTags []string) bool {
	if len(tx.Tags) == 0 || len(budgetTags) == 0 {
		return false
	}

	// Build a set of budget tags for O(1) lookup
	tagSet := make(map[string]bool)
	for _, tag := range budgetTags {
		tagSet[strings.ToLower(tag)] = true
	}

	// Check if any transaction tag matches
	for _, txTag := range tx.Tags {
		if tagSet[strings.ToLower(txTag)] {
			return true
		}
	}
	return false
}

// containsAll checks if slice a contains all elements of slice b.
// Case-insensitive comparison.
func containsAll(a, b []string) bool {
	if len(b) == 0 {
		return true
	}

	aSet := make(map[string]bool)
	for _, item := range a {
		aSet[strings.ToLower(item)] = true
	}

	for _, item := range b {
		if !aSet[strings.ToLower(item)] {
			return false
		}
	}
	return true
}
