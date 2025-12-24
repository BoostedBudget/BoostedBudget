package services

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
)

// =============================================================================
// Issue #494: Budget Sync Service for Envelope Budgeting
// Automatically recalculates budget income and spent amounts when transactions change
// =============================================================================

// BudgetSyncRepository is a minimal interface for budget sync operations.
// This follows the minimal dependency interface pattern used throughout the codebase.
type BudgetSyncRepository interface {
	GetActiveByUserID(ctx context.Context, userID string) ([]*domain.Budget, error)
	GetCategoriesByBudgetID(ctx context.Context, budgetID string) ([]domain.BudgetCategory, error)
	UpdateBudgetIncome(ctx context.Context, budgetID string, incomeAmount float64) error
	UpdateCategorySpentAmount(ctx context.Context, categoryID string, spentAmount float64) error
}

// BudgetIncomeCalculator is a minimal interface for income calculation.
type BudgetIncomeCalculator interface {
	CalculateBudgetIncome(ctx context.Context, budget *domain.Budget, startDate, endDate time.Time) (float64, error)
}

// BudgetSpentCalculator is a minimal interface for category spent calculation.
type BudgetSpentCalculator interface {
	CalculateCategorySpent(ctx context.Context, userID string, category *domain.BudgetCategory, startDate, endDate time.Time) (float64, error)
}

// EventTriggerProcessor is a minimal interface for processing automation rule events.
// This is an optional dependency - if not set, no automation rules will be triggered.
// Issue #35: Rules Engine Core - budget_updated event integration.
type EventTriggerProcessor interface {
	ProcessEventTrigger(ctx context.Context, userID, eventType string, eventData map[string]interface{}) error
}

// BudgetSyncService handles automatic recalculation of budget amounts.
// It integrates with the transaction sync pipeline to keep budget amounts in sync
// with the latest transaction data.
type BudgetSyncService interface {
	// RecalculateBudgetAmounts recalculates income and spent amounts for all active budgets.
	// Called after transaction sync completes to keep budgets up-to-date.
	// Errors for individual budgets are logged but don't stop other budgets from being processed.
	RecalculateBudgetAmounts(ctx context.Context, userID string) error

	// RecalculateBudget recalculates income and spent amounts for a single budget.
	// Returns error if income calculation or budget update fails.
	// Category errors are logged but don't fail the operation.
	RecalculateBudget(ctx context.Context, budget *domain.Budget) error

	// SetEventProcessor sets the optional event processor for triggering automation rules.
	// If not set, no automation rules will be triggered on budget updates.
	// Issue #35: Rules Engine Core - enables budget_updated events.
	SetEventProcessor(processor EventTriggerProcessor)
}

// budgetSyncService implements BudgetSyncService
type budgetSyncService struct {
	budgetRepo       BudgetSyncRepository
	incomeService    BudgetIncomeCalculator
	categoryService  BudgetSpentCalculator
	logger           *slog.Logger
	eventProcessor   EventTriggerProcessor // Optional: triggers automation rules on budget updates
}

// NewBudgetSyncService creates a new BudgetSyncService instance.
func NewBudgetSyncService(
	budgetRepo BudgetSyncRepository,
	incomeService BudgetIncomeCalculator,
	categoryService BudgetSpentCalculator,
	logger *slog.Logger,
) BudgetSyncService {
	return &budgetSyncService{
		budgetRepo:      budgetRepo,
		incomeService:   incomeService,
		categoryService: categoryService,
		logger:          logger,
	}
}

// SetEventProcessor sets the optional event processor for triggering automation rules.
func (s *budgetSyncService) SetEventProcessor(processor EventTriggerProcessor) {
	s.eventProcessor = processor
}

// RecalculateBudgetAmounts recalculates income and spent amounts for all active budgets.
func (s *budgetSyncService) RecalculateBudgetAmounts(ctx context.Context, userID string) error {
	budgets, err := s.budgetRepo.GetActiveByUserID(ctx, userID)
	if err != nil {
		return fmt.Errorf("failed to get active budgets: %w", err)
	}

	for _, budget := range budgets {
		if err := s.RecalculateBudget(ctx, budget); err != nil {
			s.logger.Error("failed to recalculate budget",
				"budgetID", budget.ID,
				"userID", userID,
				"error", err)
			// Continue with other budgets - partial failure is acceptable
		}
	}

	return nil
}

// RecalculateBudget recalculates income and spent amounts for a single budget.
func (s *budgetSyncService) RecalculateBudget(ctx context.Context, budget *domain.Budget) error {
	// Calculate current period dates based on budget period type
	startDate, endDate := calculateBudgetPeriodDates(budget, time.Now())

	// Step 1: Calculate and update income
	incomeAmount, err := s.incomeService.CalculateBudgetIncome(ctx, budget, startDate, endDate)
	if err != nil {
		return fmt.Errorf("failed to calculate income: %w", err)
	}

	if err := s.budgetRepo.UpdateBudgetIncome(ctx, budget.ID, incomeAmount); err != nil {
		return fmt.Errorf("failed to update budget income: %w", err)
	}

	// Step 2: Get categories for this budget
	categories, err := s.budgetRepo.GetCategoriesByBudgetID(ctx, budget.ID)
	if err != nil {
		return fmt.Errorf("failed to get budget categories: %w", err)
	}

	// Step 3: Calculate and update spent amount for each category
	var totalSpent float64
	for i := range categories {
		spentAmount, err := s.categoryService.CalculateCategorySpent(
			ctx, budget.UserID, &categories[i], startDate, endDate)
		if err != nil {
			// Log but continue - one category failure shouldn't stop others
			s.logger.Error("failed to calculate category spent",
				"categoryID", categories[i].ID,
				"budgetID", budget.ID,
				"error", err)
			continue
		}

		totalSpent += spentAmount

		if err := s.budgetRepo.UpdateCategorySpentAmount(ctx, categories[i].ID, spentAmount); err != nil {
			s.logger.Error("failed to update category spent amount",
				"categoryID", categories[i].ID,
				"budgetID", budget.ID,
				"error", err)
			continue
		}
	}

	// Step 4: Trigger automation rules if event processor is set
	// Issue #35: Rules Engine Core - budget_updated event
	if s.eventProcessor != nil {
		// Calculate percent used for condition evaluation
		var percentUsed float64
		if incomeAmount > 0 {
			percentUsed = (totalSpent / incomeAmount) * 100
		}

		eventData := map[string]interface{}{
			"budget_id":     budget.ID,
			"budget_name":   budget.Name,
			"income_amount": incomeAmount,
			"total_spent":   totalSpent,
			"percent_used":  percentUsed,
		}

		// Fire and forget - rule execution failures shouldn't fail budget sync
		if err := s.eventProcessor.ProcessEventTrigger(ctx, budget.UserID, "budget_updated", eventData); err != nil {
			s.logger.Warn("failed to process budget_updated event",
				"budgetID", budget.ID,
				"userID", budget.UserID,
				"error", err)
		}
	}

	return nil
}

// calculateBudgetPeriodDates calculates the start and end dates for the current budget period.
// This is a helper function that determines the date range for income and spent calculations
// based on the budget's period type.
//
// Period types:
// - Weekly: Sunday to Saturday (US convention)
// - Monthly: 1st to last day of current month
// - Quarterly: Q1 (Jan-Mar), Q2 (Apr-Jun), Q3 (Jul-Sep), Q4 (Oct-Dec)
// - Yearly: Jan 1 to Dec 31 of current year
// - Custom: Uses budget's explicit StartDate and EndDate
//
// TODO: Future enhancement - make week start day user-configurable
func calculateBudgetPeriodDates(budget *domain.Budget, now time.Time) (startDate, endDate time.Time) {
	// Create end of day (23:59:59.999999999) for consistent end dates
	endOfDay := func(t time.Time) time.Time {
		return time.Date(t.Year(), t.Month(), t.Day(), 23, 59, 59, 999999999, t.Location())
	}

	switch budget.Period {
	case domain.BudgetPeriodWeekly:
		// Sunday start (US convention, default for USA-based app)
		// Sunday=0, Monday=1, ..., Saturday=6
		weekday := int(now.Weekday())
		startDate = time.Date(now.Year(), now.Month(), now.Day()-weekday, 0, 0, 0, 0, now.Location())
		endDate = endOfDay(startDate.AddDate(0, 0, 6)) // Saturday

	case domain.BudgetPeriodMonthly:
		startDate = time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, now.Location())
		lastDay := startDate.AddDate(0, 1, -1)
		endDate = endOfDay(lastDay)

	case domain.BudgetPeriodQuarterly:
		// Q1: Jan-Mar (months 1-3), Q2: Apr-Jun (months 4-6)
		// Q3: Jul-Sep (months 7-9), Q4: Oct-Dec (months 10-12)
		quarter := (int(now.Month()) - 1) / 3
		startMonth := time.Month(quarter*3 + 1)
		startDate = time.Date(now.Year(), startMonth, 1, 0, 0, 0, 0, now.Location())
		lastDay := startDate.AddDate(0, 3, -1)
		endDate = endOfDay(lastDay)

	case domain.BudgetPeriodYearly:
		startDate = time.Date(now.Year(), 1, 1, 0, 0, 0, 0, now.Location())
		endDate = endOfDay(time.Date(now.Year(), 12, 31, 0, 0, 0, 0, now.Location()))

	case domain.BudgetPeriodCustom:
		startDate = budget.StartDate
		if budget.EndDate != nil {
			endDate = *budget.EndDate
		} else {
			// If no end date specified, use far future
			endDate = now.AddDate(100, 0, 0)
		}

	default:
		// Default to monthly if unknown period type
		startDate = time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, now.Location())
		lastDay := startDate.AddDate(0, 1, -1)
		endDate = endOfDay(lastDay)
	}

	return startDate, endDate
}
