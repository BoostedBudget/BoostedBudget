package services

import (
	"context"
	"errors"
	"time"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/matching"
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
	return s.spentCalculator.GetSpentSumForBudgetCategory(ctx, userID, category, startDate, endDate)
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
		return s.matchesCategory(transaction, category)
	case domain.MatchLogicTagOnly:
		return s.matchesTags(transaction, category.Tags)
	case domain.MatchLogicAnd:
		return s.matchesCategory(transaction, category) && s.matchesTags(transaction, category.Tags)
	case domain.MatchLogicOr:
		return s.matchesCategory(transaction, category) || s.matchesTags(transaction, category.Tags)
	default:
		return false
	}
}

// matchesCategory checks if a transaction's effective category matches the budget category.
// Uses the discriminated union: PFC kind matches by pfc_primary (and optionally pfc_detailed),
// custom kind matches by custom_category_override_id.
func (s *budgetCategoryMatchingService) matchesCategory(tx *domain.Transaction, budgetCat *domain.BudgetCategory) bool {
	if budgetCat == nil {
		return true
	}
	effective := tx.GetEffectiveCategory()
	if effective == nil {
		return false
	}
	switch budgetCat.CategoryKind {
	case "pfc":
		if effective.Kind != domain.EffectiveCategoryKindPFC || effective.PFCPrimary == nil || budgetCat.PFCPrimary == nil {
			return false
		}
		if *effective.PFCPrimary != *budgetCat.PFCPrimary {
			return false
		}
		if budgetCat.PFCDetailed != nil {
			if effective.PFCDetailed == nil || *effective.PFCDetailed != *budgetCat.PFCDetailed {
				return false
			}
		}
		return true
	case "custom":
		if effective.Kind != domain.EffectiveCategoryKindCustom || effective.CustomCategoryID == nil || budgetCat.CustomCategoryID == nil {
			return false
		}
		return *effective.CustomCategoryID == *budgetCat.CustomCategoryID
	default:
		return false
	}
}

// matchesTags checks if transaction has ANY of the budget category's tags.
// Case-insensitive matching. Uses matching.ContainsAny (Issue #495).
func (s *budgetCategoryMatchingService) matchesTags(tx *domain.Transaction, budgetTags []string) bool {
	return matching.ContainsAny(tx.Tags, budgetTags)
}
