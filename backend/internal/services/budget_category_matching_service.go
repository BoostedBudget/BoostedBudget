package services

import (
	"context"
	"errors"
	"time"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
)

// =============================================================================
// Issue #493: Budget Category Matching Service
// =============================================================================

// SpentAmountCalculator is a minimal interface for calculating spent amounts.
// This follows the minimal dependency interface pattern used throughout the codebase.
type SpentAmountCalculator interface {
	GetSpentSumForBudgetCategory(ctx context.Context, userID string, budgetCat *domain.BudgetCategory, startDate, endDate time.Time) (float64, error)
}

// BudgetCategoryMatchingService calculates category spending with flexible matching logic.
// It supports four match logic types:
//   - category_only: Match transactions where effective category matches the budget category
//   - tag_only: Match transactions where tags contain ANY of the budget tags
//   - and: Match transactions where BOTH category AND tags match
//   - or: Match transactions where EITHER category OR tags match
//
// ⚠ Issue #1630 task 3: this interface used to also carry DoesTransactionMatchCategory, a
// second, independently-written implementation of the match_logic rule. It had zero production
// callers (only its own unit tests) and was genuinely divergent from the canonical rule used
// everywhere else (domain.Budget.IsSubAllocationWithin / GetBudgetVsActual's roll-up dedup): it
// matched a transaction against ONE category by exact equality only, with no roll-up of a child
// category into its budgeted parent — the opposite answer from the rule this issue pins. Removed
// rather than fixed-to-delegate: nothing in production called it, so there was no roll-up
// behaviour to preserve, and keeping a second, wrong implementation of "does this transaction
// match this budget category" around was the exact spread this task exists to close.
type BudgetCategoryMatchingService interface {
	// CalculateCategorySpent returns the total spent amount for a budget category
	// within the given date range, using the category's match logic.
	CalculateCategorySpent(ctx context.Context, userID string, category *domain.BudgetCategory, startDate, endDate time.Time) (float64, error)
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
	return s.spentCalculator.GetSpentSumForBudgetCategory(ctx, userID, category, startDate, endDate)
}
