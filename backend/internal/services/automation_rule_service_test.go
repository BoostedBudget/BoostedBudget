// Package services provides tests for automation rule service.
// Issue #1270: Task 3 — category ownership validation in CreateRule/UpdateRule.
package services

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/repository"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockAutoRulesRepo is a minimal mock for AutomationRulesRepository.
type mockAutoRulesRepo struct {
	createErr error
	updateErr error
	rule      *domain.AutomationRule
}

func (m *mockAutoRulesRepo) Create(ctx context.Context, rule *domain.AutomationRule) error {
	return m.createErr
}
func (m *mockAutoRulesRepo) GetByID(ctx context.Context, id string) (*domain.AutomationRule, error) {
	if m.rule == nil {
		return nil, domain.ErrAutomationRuleNotFound
	}
	return m.rule, nil
}
func (m *mockAutoRulesRepo) GetByUserID(ctx context.Context, userID string) ([]*domain.AutomationRule, error) {
	return nil, nil
}
func (m *mockAutoRulesRepo) GetEnabledByUserID(ctx context.Context, userID string) ([]*domain.AutomationRule, error) {
	return nil, nil
}
func (m *mockAutoRulesRepo) GetByName(ctx context.Context, userID, name string) (*domain.AutomationRule, error) {
	return nil, nil
}
func (m *mockAutoRulesRepo) Update(ctx context.Context, rule *domain.AutomationRule) error {
	return m.updateErr
}
func (m *mockAutoRulesRepo) Delete(ctx context.Context, id string) error { return nil }
func (m *mockAutoRulesRepo) CountByUserID(ctx context.Context, userID string) (int, error) {
	return 0, nil
}
func (m *mockAutoRulesRepo) IncrementExecutionCount(ctx context.Context, ruleID string) error {
	return nil
}
func (m *mockAutoRulesRepo) UpdateLastSuccessful(ctx context.Context, ruleID string) error {
	return nil
}
func (m *mockAutoRulesRepo) GetRulesByEventTrigger(ctx context.Context, userID, eventType string) ([]*domain.AutomationRule, error) {
	return nil, nil
}
func (m *mockAutoRulesRepo) GetRulesByType(ctx context.Context, userID string, ruleType domain.RuleType) ([]*domain.AutomationRule, error) {
	return nil, nil
}
func (m *mockAutoRulesRepo) GetRulesByScheduleTrigger(ctx context.Context) ([]*domain.AutomationRule, error) {
	return nil, nil
}

// Ensure mockAutoRulesRepo satisfies repository.AutomationRulesRepository at compile time.
var _ repository.AutomationRulesRepository = (*mockAutoRulesRepo)(nil)

// mockBudgetRepoForAutoRule is a minimal mock for BudgetRepository used by automation rule tests.
type mockBudgetRepoForAutoRule struct {
	getCategoryErr error
	category       *domain.BudgetCategory
	// Track calls for assertion
	calledWith [][]string
}

func (m *mockBudgetRepoForAutoRule) GetCategoryByIDAndUser(ctx context.Context, categoryID, userID string) (*domain.BudgetCategory, error) {
	m.calledWith = append(m.calledWith, []string{categoryID, userID})
	if m.getCategoryErr != nil {
		return nil, m.getCategoryErr
	}
	return m.category, nil
}

// Remaining BudgetRepository interface methods — all no-ops for these tests.
func (m *mockBudgetRepoForAutoRule) Create(ctx context.Context, budget *domain.Budget) error {
	return nil
}
func (m *mockBudgetRepoForAutoRule) GetByID(ctx context.Context, id string) (*domain.Budget, error) {
	return nil, nil
}
func (m *mockBudgetRepoForAutoRule) GetByUserID(ctx context.Context, userID string) ([]*domain.Budget, error) {
	return nil, nil
}
func (m *mockBudgetRepoForAutoRule) GetActiveByUserID(ctx context.Context, userID string) ([]*domain.Budget, error) {
	return nil, nil
}
func (m *mockBudgetRepoForAutoRule) GetByName(ctx context.Context, userID, name string) (*domain.Budget, error) {
	return nil, nil
}
func (m *mockBudgetRepoForAutoRule) Update(ctx context.Context, budget *domain.Budget) error {
	return nil
}
func (m *mockBudgetRepoForAutoRule) Delete(ctx context.Context, id, userID string) error { return nil }
func (m *mockBudgetRepoForAutoRule) CountByUserID(ctx context.Context, userID string) (int, error) {
	return 0, nil
}
func (m *mockBudgetRepoForAutoRule) CreateCategory(ctx context.Context, category *domain.BudgetCategory) error {
	return nil
}
func (m *mockBudgetRepoForAutoRule) GetCategoriesByBudgetID(ctx context.Context, budgetID string) ([]domain.BudgetCategory, error) {
	return nil, nil
}
func (m *mockBudgetRepoForAutoRule) UpdateCategory(ctx context.Context, category *domain.BudgetCategory) error {
	return nil
}
func (m *mockBudgetRepoForAutoRule) UpdateCategorySpentAmount(ctx context.Context, categoryID string, spentAmount float64) error {
	return nil
}
func (m *mockBudgetRepoForAutoRule) DeleteCategory(ctx context.Context, id, budgetID string) error {
	return nil
}
func (m *mockBudgetRepoForAutoRule) GetCategoriesMatchingTransaction(ctx context.Context, userID string, effective *domain.EffectiveCategory) ([]domain.BudgetCategory, error) {
	return nil, nil
}
func (m *mockBudgetRepoForAutoRule) UpdateBudgetIncome(ctx context.Context, budgetID string, incomeAmount float64) error {
	return nil
}
func (m *mockBudgetRepoForAutoRule) AddIncomeSource(ctx context.Context, budgetID string, source domain.IncomeSource) error {
	return nil
}
func (m *mockBudgetRepoForAutoRule) RemoveIncomeSource(ctx context.Context, budgetID string, sourceIndex int) error {
	return nil
}
func (m *mockBudgetRepoForAutoRule) AdjustCategoryAllocatedAmount(ctx context.Context, categoryID string, delta float64) error {
	return nil
}

// Ensure mockBudgetRepoForAutoRule satisfies repository.BudgetRepository at compile time.
var _ repository.BudgetRepository = (*mockBudgetRepoForAutoRule)(nil)

// buildTransferActions returns a valid JSON actions blob for a transfer_suggestion rule.
func buildTransferActions(t *testing.T) json.RawMessage {
	t.Helper()
	action := domain.TransferAction{
		FromAccountID: "acc-from",
		ToAccountID:   "acc-to",
		AmountType:    "fixed",
		Amount:        100,
	}
	b, err := json.Marshal(action)
	require.NoError(t, err)
	return b
}

// TestAutomationRuleService_CreateRule_CategoryOwnership tests that CreateRule
// validates category ownership when conditions reference a budget_category field.
func TestAutomationRuleService_CreateRule_CategoryOwnership(t *testing.T) {
	ctx := context.Background()
	userID := "user-123"
	categoryID := "cat-owned-456"

	t.Run("foreign category_id in condition is rejected", func(t *testing.T) {
		ruleRepo := &mockAutoRulesRepo{}
		budgetRepo := &mockBudgetRepoForAutoRule{
			getCategoryErr: errors.New("category not found or not owned by user"),
		}

		svc := NewAutomationRuleService(ruleRepo)
		svc.(*automationRuleService).SetBudgetRepo(budgetRepo)

		conditions := []domain.AutomationRuleCondition{
			{
				Field:      "budget_category.remaining_amount",
				Operator:   domain.ConditionOperatorLt,
				Value:      "50",
				CategoryID: "cat-foreign-999",
			},
		}

		req := &domain.CreateAutomationRuleRequest{
			Name:     "Budget Rule",
			RuleType: domain.RuleTypeTransferSuggestion,
			TriggerConfig: domain.TriggerConfig{
				Type:  domain.TriggerTypeEvent,
				Event: domain.EventBudgetUpdated,
			},
			Conditions:     conditions,
			ConditionLogic: "AND",
			Actions:        buildTransferActions(t),
		}

		_, err := svc.CreateRule(ctx, userID, req)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "condition category_id does not belong to this user")
	})

	t.Run("owned category_id in condition is accepted", func(t *testing.T) {
		ruleRepo := &mockAutoRulesRepo{}
		budgetRepo := &mockBudgetRepoForAutoRule{
			category: &domain.BudgetCategory{
				ID:       categoryID,
				BudgetID: "budget-789",
			},
		}

		svc := NewAutomationRuleService(ruleRepo)
		svc.(*automationRuleService).SetBudgetRepo(budgetRepo)

		conditions := []domain.AutomationRuleCondition{
			{
				Field:      "budget_category.remaining_amount",
				Operator:   domain.ConditionOperatorLt,
				Value:      "50",
				CategoryID: categoryID,
			},
		}

		req := &domain.CreateAutomationRuleRequest{
			Name:     "Budget Rule",
			RuleType: domain.RuleTypeTransferSuggestion,
			TriggerConfig: domain.TriggerConfig{
				Type:  domain.TriggerTypeEvent,
				Event: domain.EventBudgetUpdated,
			},
			Conditions:     conditions,
			ConditionLogic: "AND",
			Actions:        buildTransferActions(t),
		}

		_, err := svc.CreateRule(ctx, userID, req)
		require.NoError(t, err)
	})

	t.Run("no budget_category condition skips ownership check", func(t *testing.T) {
		ruleRepo := &mockAutoRulesRepo{}
		// budgetRepo.GetCategoryByIDAndUser returns error, but it should NOT be called
		budgetRepo := &mockBudgetRepoForAutoRule{
			getCategoryErr: errors.New("should not be called"),
		}

		svc := NewAutomationRuleService(ruleRepo)
		svc.(*automationRuleService).SetBudgetRepo(budgetRepo)

		// Scheduled trigger — conditions not evaluated; no category_id present
		req := &domain.CreateAutomationRuleRequest{
			Name:     "No Category Rule",
			RuleType: domain.RuleTypeTransferSuggestion,
			TriggerConfig: domain.TriggerConfig{
				Type: domain.TriggerTypeSchedule,
				Cron: "0 0 * * *",
			},
			ConditionLogic: "AND",
			Actions:        buildTransferActions(t),
		}

		_, err := svc.CreateRule(ctx, userID, req)
		require.NoError(t, err)
		assert.Empty(t, budgetRepo.calledWith, "GetCategoryByIDAndUser should not be called for non-category conditions")
	})

	t.Run("no budget_repo set — condition with category_id still passes (nil guard)", func(t *testing.T) {
		ruleRepo := &mockAutoRulesRepo{}

		// Don't set budget repo — service is initialized without it
		svc := NewAutomationRuleService(ruleRepo)
		// No SetBudgetRepo call

		// Scheduled trigger without conditions — should succeed with nil budgetRepo
		req := &domain.CreateAutomationRuleRequest{
			Name:     "No Repo Rule",
			RuleType: domain.RuleTypeTransferSuggestion,
			TriggerConfig: domain.TriggerConfig{
				Type: domain.TriggerTypeSchedule,
				Cron: "0 0 * * *",
			},
			ConditionLogic: "AND",
			Actions:        buildTransferActions(t),
		}

		_, err := svc.CreateRule(ctx, userID, req)
		require.NoError(t, err)
	})
}

// TestAutomationRuleService_UpdateRule_CategoryOwnership tests that UpdateRule
// validates category ownership when conditions reference a budget_category field.
func TestAutomationRuleService_UpdateRule_CategoryOwnership(t *testing.T) {
	ctx := context.Background()
	userID := "user-123"
	ruleID := "rule-456"

	actions := buildTransferActions(t)

	existingRule := &domain.AutomationRule{
		ID:       ruleID,
		UserID:   userID,
		Name:     "Existing Rule",
		RuleType: domain.RuleTypeTransferSuggestion,
		Enabled:  true,
		TriggerConfig: domain.TriggerConfig{
			Type:  domain.TriggerTypeEvent,
			Event: domain.EventBudgetUpdated,
		},
		ConditionLogic: "AND",
		Actions:        actions,
	}

	t.Run("foreign category_id in updated condition is rejected", func(t *testing.T) {
		ruleRepo := &mockAutoRulesRepo{rule: existingRule}
		budgetRepo := &mockBudgetRepoForAutoRule{
			getCategoryErr: errors.New("category not found or not owned by user"),
		}

		svc := NewAutomationRuleService(ruleRepo)
		svc.(*automationRuleService).SetBudgetRepo(budgetRepo)

		newConditions := []domain.AutomationRuleCondition{
			{
				Field:      "budget_category.remaining_amount",
				Operator:   domain.ConditionOperatorLt,
				Value:      "50",
				CategoryID: "cat-foreign-999",
			},
		}

		req := &domain.UpdateAutomationRuleRequest{
			Conditions: &newConditions,
		}

		_, err := svc.UpdateRule(ctx, userID, ruleID, req)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "condition category_id does not belong to this user")
	})

	t.Run("owned category_id in updated condition is accepted", func(t *testing.T) {
		ruleRepo := &mockAutoRulesRepo{rule: existingRule}
		budgetRepo := &mockBudgetRepoForAutoRule{
			category: &domain.BudgetCategory{
				ID:       "cat-owned",
				BudgetID: "budget-789",
			},
		}

		svc := NewAutomationRuleService(ruleRepo)
		svc.(*automationRuleService).SetBudgetRepo(budgetRepo)

		newConditions := []domain.AutomationRuleCondition{
			{
				Field:      "budget_category.remaining_amount",
				Operator:   domain.ConditionOperatorLt,
				Value:      "50",
				CategoryID: "cat-owned",
			},
		}

		req := &domain.UpdateAutomationRuleRequest{
			Conditions: &newConditions,
		}

		_, err := svc.UpdateRule(ctx, userID, ruleID, req)
		require.NoError(t, err)
	})
}
