// Package services provides tests for rule execution service.
// Issue #35: Rules Engine Core - execution with condition evaluation.
package services

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Mock repositories for testing.

type mockRuleExecutionRepository struct {
	executions          []*domain.RuleExecution
	todayExecutionCount int
	createError         error
	updateError         error
}

func (m *mockRuleExecutionRepository) Create(ctx context.Context, execution *domain.RuleExecution) error {
	if m.createError != nil {
		return m.createError
	}
	m.executions = append(m.executions, execution)
	return nil
}

func (m *mockRuleExecutionRepository) Update(ctx context.Context, execution *domain.RuleExecution) error {
	if m.updateError != nil {
		return m.updateError
	}
	for i, e := range m.executions {
		if e.ID == execution.ID {
			m.executions[i] = execution
			return nil
		}
	}
	return errors.New("execution not found")
}

func (m *mockRuleExecutionRepository) GetByID(ctx context.Context, id string) (*domain.RuleExecution, error) {
	for _, e := range m.executions {
		if e.ID == id {
			return e, nil
		}
	}
	return nil, errors.New("not found")
}

func (m *mockRuleExecutionRepository) GetByRuleID(ctx context.Context, ruleID string, limit, offset int) ([]*domain.RuleExecution, error) {
	var result []*domain.RuleExecution
	for _, e := range m.executions {
		if e.RuleID == ruleID {
			result = append(result, e)
		}
	}
	return result, nil
}

func (m *mockRuleExecutionRepository) GetTodayExecutionCount(ctx context.Context, userID string) (int, error) {
	return m.todayExecutionCount, nil
}

func (m *mockRuleExecutionRepository) GetLastExecutionForRule(ctx context.Context, ruleID string) (*domain.RuleExecution, error) {
	var latest *domain.RuleExecution
	for _, e := range m.executions {
		if e.RuleID == ruleID {
			if latest == nil || e.StartedAt.After(latest.StartedAt) {
				latest = e
			}
		}
	}
	if latest == nil {
		return nil, errors.New("not found")
	}
	return latest, nil
}

func (m *mockRuleExecutionRepository) GetPendingExecutions(ctx context.Context, limit int) ([]*domain.RuleExecution, error) {
	var result []*domain.RuleExecution
	for _, e := range m.executions {
		if e.Status == domain.ExecutionStatusPending {
			result = append(result, e)
		}
	}
	return result, nil
}

func (m *mockRuleExecutionRepository) GetFailedExecutions(ctx context.Context, ruleID string, limit int) ([]*domain.RuleExecution, error) {
	var result []*domain.RuleExecution
	for _, e := range m.executions {
		if e.RuleID == ruleID && e.Status == domain.ExecutionStatusFailed {
			result = append(result, e)
		}
	}
	return result, nil
}

// GetByRuleIDWithFilter implements RuleExecutionRepository with optional status filter.
// Issue #38: Rules API Endpoints - Execution history with filtering.
func (m *mockRuleExecutionRepository) GetByRuleIDWithFilter(ctx context.Context, ruleID string, status *domain.ExecutionStatus, limit, offset int) ([]*domain.RuleExecution, int, error) {
	var result []*domain.RuleExecution
	for _, e := range m.executions {
		if e.RuleID == ruleID {
			if status == nil || e.Status == *status {
				result = append(result, e)
			}
		}
	}

	total := len(result)

	// Apply pagination
	if offset >= len(result) {
		return []*domain.RuleExecution{}, total, nil
	}
	result = result[offset:]
	if limit > 0 && limit < len(result) {
		result = result[:limit]
	}

	return result, total, nil
}

type mockTransferSuggestionRepository struct {
	suggestions []*domain.TransferSuggestion
	createError error
}

func (m *mockTransferSuggestionRepository) Create(ctx context.Context, suggestion *domain.TransferSuggestion) error {
	if m.createError != nil {
		return m.createError
	}
	m.suggestions = append(m.suggestions, suggestion)
	return nil
}

func (m *mockTransferSuggestionRepository) GetByID(ctx context.Context, id string) (*domain.TransferSuggestion, error) {
	for _, s := range m.suggestions {
		if s.ID == id {
			return s, nil
		}
	}
	return nil, domain.ErrTransferSuggestionNotFound
}

func (m *mockTransferSuggestionRepository) GetByUserID(ctx context.Context, userID string, status *domain.SuggestionStatus, limit, offset int) ([]*domain.TransferSuggestion, error) {
	var result []*domain.TransferSuggestion
	for _, s := range m.suggestions {
		if s.UserID == userID {
			if status == nil || s.Status == *status {
				result = append(result, s)
			}
		}
	}
	return result, nil
}

func (m *mockTransferSuggestionRepository) GetPendingByUserID(ctx context.Context, userID string) ([]*domain.TransferSuggestion, error) {
	status := domain.SuggestionStatusPending
	return m.GetByUserID(ctx, userID, &status, 100, 0)
}

func (m *mockTransferSuggestionRepository) UpdateStatus(ctx context.Context, id string, status domain.SuggestionStatus, userNotes *string) error {
	for _, s := range m.suggestions {
		if s.ID == id {
			s.Status = status
			s.UserNotes = userNotes
			return nil
		}
	}
	return domain.ErrTransferSuggestionNotFound
}

func (m *mockTransferSuggestionRepository) Update(ctx context.Context, suggestion *domain.TransferSuggestion) error {
	for i, s := range m.suggestions {
		if s.ID == suggestion.ID {
			m.suggestions[i] = suggestion
			return nil
		}
	}
	return domain.ErrTransferSuggestionNotFound
}

func (m *mockTransferSuggestionRepository) ExpirePending(ctx context.Context) (int64, error) {
	return 0, nil
}

func (m *mockTransferSuggestionRepository) CountPendingByUserID(ctx context.Context, userID string) (int, error) {
	count := 0
	for _, s := range m.suggestions {
		if s.UserID == userID && s.Status == domain.SuggestionStatusPending {
			count++
		}
	}
	return count, nil
}

func (m *mockTransferSuggestionRepository) GetByRuleID(ctx context.Context, ruleID string, limit, offset int) ([]*domain.TransferSuggestion, error) {
	var result []*domain.TransferSuggestion
	for _, s := range m.suggestions {
		if s.RuleID != nil && *s.RuleID == ruleID {
			result = append(result, s)
		}
	}
	return result, nil
}

// TestConditionEvaluation tests the condition matching logic.
func TestConditionEvaluation(t *testing.T) {
	t.Run("evaluates equals operator", func(t *testing.T) {
		condition := domain.AutomationRuleCondition{
			Field:    "merchant_name",
			Operator: domain.ConditionOperatorEq,
			Value:    "Amazon",
		}

		data := map[string]interface{}{
			"merchant_name": "Amazon",
		}

		result := evaluateCondition(condition, data)
		assert.True(t, result)
	})

	t.Run("evaluates not_equals operator", func(t *testing.T) {
		condition := domain.AutomationRuleCondition{
			Field:    "merchant_name",
			Operator: domain.ConditionOperatorNeq,
			Value:    "Amazon",
		}

		data := map[string]interface{}{
			"merchant_name": "Walmart",
		}

		result := evaluateCondition(condition, data)
		assert.True(t, result)
	})

	t.Run("evaluates contains operator", func(t *testing.T) {
		condition := domain.AutomationRuleCondition{
			Field:    "merchant_name",
			Operator: domain.ConditionOperatorContains,
			Value:    "mart",
		}

		data := map[string]interface{}{
			"merchant_name": "Walmart",
		}

		result := evaluateCondition(condition, data)
		assert.True(t, result)
	})

	t.Run("evaluates greater_than operator with numbers", func(t *testing.T) {
		condition := domain.AutomationRuleCondition{
			Field:    "amount",
			Operator: domain.ConditionOperatorGt,
			Value:    "100",
		}

		data := map[string]interface{}{
			"amount": 150.0,
		}

		result := evaluateCondition(condition, data)
		assert.True(t, result)
	})

	t.Run("evaluates less_than operator with numbers", func(t *testing.T) {
		condition := domain.AutomationRuleCondition{
			Field:    "amount",
			Operator: domain.ConditionOperatorLt,
			Value:    "100",
		}

		data := map[string]interface{}{
			"amount": 50.0,
		}

		result := evaluateCondition(condition, data)
		assert.True(t, result)
	})

	t.Run("evaluates greater_than_or_equal operator", func(t *testing.T) {
		condition := domain.AutomationRuleCondition{
			Field:    "amount",
			Operator: domain.ConditionOperatorGte,
			Value:    "100",
		}

		data := map[string]interface{}{
			"amount": 100.0,
		}

		result := evaluateCondition(condition, data)
		assert.True(t, result)
	})

	t.Run("evaluates less_than_or_equal operator", func(t *testing.T) {
		condition := domain.AutomationRuleCondition{
			Field:    "amount",
			Operator: domain.ConditionOperatorLte,
			Value:    "100",
		}

		data := map[string]interface{}{
			"amount": 100.0,
		}

		result := evaluateCondition(condition, data)
		assert.True(t, result)
	})

	t.Run("returns false for missing field", func(t *testing.T) {
		condition := domain.AutomationRuleCondition{
			Field:    "merchant_name",
			Operator: domain.ConditionOperatorEq,
			Value:    "Amazon",
		}

		data := map[string]interface{}{
			"amount": 100.0,
		}

		result := evaluateCondition(condition, data)
		assert.False(t, result)
	})
}

// TestConditionLogic tests AND/OR condition logic.
func TestConditionLogic(t *testing.T) {
	t.Run("AND logic requires all conditions to match", func(t *testing.T) {
		conditions := []domain.AutomationRuleCondition{
			{Field: "merchant_name", Operator: domain.ConditionOperatorEq, Value: "Amazon"},
			{Field: "amount", Operator: domain.ConditionOperatorGt, Value: "100"},
		}

		data := map[string]interface{}{
			"merchant_name": "Amazon",
			"amount":        150.0,
		}

		result := evaluateConditions(conditions, "AND", data)
		assert.True(t, result)
	})

	t.Run("AND logic fails if any condition fails", func(t *testing.T) {
		conditions := []domain.AutomationRuleCondition{
			{Field: "merchant_name", Operator: domain.ConditionOperatorEq, Value: "Amazon"},
			{Field: "amount", Operator: domain.ConditionOperatorGt, Value: "100"},
		}

		data := map[string]interface{}{
			"merchant_name": "Amazon",
			"amount":        50.0, // Less than 100
		}

		result := evaluateConditions(conditions, "AND", data)
		assert.False(t, result)
	})

	t.Run("OR logic succeeds if any condition matches", func(t *testing.T) {
		conditions := []domain.AutomationRuleCondition{
			{Field: "merchant_name", Operator: domain.ConditionOperatorEq, Value: "Amazon"},
			{Field: "amount", Operator: domain.ConditionOperatorGt, Value: "100"},
		}

		data := map[string]interface{}{
			"merchant_name": "Walmart", // Not Amazon
			"amount":        150.0,     // Greater than 100
		}

		result := evaluateConditions(conditions, "OR", data)
		assert.True(t, result)
	})

	t.Run("OR logic fails if all conditions fail", func(t *testing.T) {
		conditions := []domain.AutomationRuleCondition{
			{Field: "merchant_name", Operator: domain.ConditionOperatorEq, Value: "Amazon"},
			{Field: "amount", Operator: domain.ConditionOperatorGt, Value: "100"},
		}

		data := map[string]interface{}{
			"merchant_name": "Walmart", // Not Amazon
			"amount":        50.0,      // Less than 100
		}

		result := evaluateConditions(conditions, "OR", data)
		assert.False(t, result)
	})

	t.Run("empty conditions always match", func(t *testing.T) {
		data := map[string]interface{}{
			"merchant_name": "Amazon",
		}

		result := evaluateConditions([]domain.AutomationRuleCondition{}, "AND", data)
		assert.True(t, result)
	})
}

// TestRuleExecutionService tests the rule execution service.
func TestRuleExecutionService(t *testing.T) {
	ctx := context.Background()
	userID := "test-user-id"

	createTestRule := func(ruleType domain.RuleType, triggerType domain.TriggerType) *domain.AutomationRule {
		actions := RuleActions{}
		switch ruleType {
		case domain.RuleTypeTransferSuggestion:
			actions.Transfer = &domain.TransferAction{
				FromAccountID: "account-1",
				ToAccountID:   "account-2",
				AmountType:    "fixed",
				Amount:        100.0,
			}
		case domain.RuleTypeBudgetAlert:
			actions.Alert = &domain.AlertAction{
				ThresholdType:  "percentage",
				ThresholdValue: 80,
			}
		case domain.RuleTypeCategoryRollover:
			actions.Rollover = &domain.RolloverAction{
				RolloverType: "all",
			}
		}

		actionsJSON, _ := json.Marshal(actions)

		return &domain.AutomationRule{
			ID:       "rule-1",
			UserID:   userID,
			Name:     "Test Rule",
			Enabled:  true,
			Priority: 1,
			RuleType: ruleType,
			TriggerConfig: domain.TriggerConfig{
				Type:  triggerType,
				Event: "transaction_created",
			},
			Conditions: []domain.AutomationRuleCondition{
				{Field: "amount", Operator: domain.ConditionOperatorGt, Value: "50"},
			},
			ConditionLogic: "AND",
			Actions:        actionsJSON,
		}
	}

	t.Run("executes transfer suggestion rule successfully", func(t *testing.T) {
		execRepo := &mockRuleExecutionRepository{}
		suggestionRepo := &mockTransferSuggestionRepository{}
		ruleRepo := &mockAutomationRulesRepository{}

		service := NewRuleExecutionService(
			execRepo,
			suggestionRepo,
			ruleRepo,
			nil, // notification service (not needed for transfer)
		)

		rule := createTestRule(domain.RuleTypeTransferSuggestion, domain.TriggerTypeEvent)
		triggerData := map[string]interface{}{
			"amount": 100.0,
		}

		execution, err := service.ExecuteRule(ctx, rule, triggerData)

		require.NoError(t, err)
		assert.NotEmpty(t, execution.ID)
		assert.Equal(t, rule.ID, execution.RuleID)
		assert.Equal(t, domain.ExecutionStatusCompleted, execution.Status)
		assert.NotNil(t, execution.CompletedAt)

		// Verify suggestion was created
		assert.Len(t, suggestionRepo.suggestions, 1)
		suggestion := suggestionRepo.suggestions[0]
		assert.Equal(t, userID, suggestion.UserID)
		assert.NotNil(t, suggestion.RuleID)
		assert.Equal(t, rule.ID, *suggestion.RuleID)
		assert.Equal(t, 100.0, suggestion.Amount)
	})

	t.Run("skips execution when conditions don't match", func(t *testing.T) {
		execRepo := &mockRuleExecutionRepository{}
		suggestionRepo := &mockTransferSuggestionRepository{}
		ruleRepo := &mockAutomationRulesRepository{}

		service := NewRuleExecutionService(
			execRepo,
			suggestionRepo,
			ruleRepo,
			nil,
		)

		rule := createTestRule(domain.RuleTypeTransferSuggestion, domain.TriggerTypeEvent)
		triggerData := map[string]interface{}{
			"amount": 25.0, // Less than 50, condition won't match
		}

		execution, err := service.ExecuteRule(ctx, rule, triggerData)

		require.NoError(t, err)
		assert.Equal(t, ExecutionStatusSkipped, execution.Status)

		// Verify no suggestion was created
		assert.Empty(t, suggestionRepo.suggestions)
	})

	t.Run("handles disabled rule", func(t *testing.T) {
		execRepo := &mockRuleExecutionRepository{}
		suggestionRepo := &mockTransferSuggestionRepository{}
		ruleRepo := &mockAutomationRulesRepository{}

		service := NewRuleExecutionService(
			execRepo,
			suggestionRepo,
			ruleRepo,
			nil,
		)

		rule := createTestRule(domain.RuleTypeTransferSuggestion, domain.TriggerTypeEvent)
		rule.Enabled = false

		triggerData := map[string]interface{}{
			"amount": 100.0,
		}

		execution, err := service.ExecuteRule(ctx, rule, triggerData)

		require.NoError(t, err)
		assert.Equal(t, ExecutionStatusSkipped, execution.Status)
		assert.Empty(t, suggestionRepo.suggestions)
	})

	t.Run("records failed execution on action error", func(t *testing.T) {
		execRepo := &mockRuleExecutionRepository{}
		suggestionRepo := &mockTransferSuggestionRepository{
			createError: errors.New("database error"),
		}
		ruleRepo := &mockAutomationRulesRepository{}

		service := NewRuleExecutionService(
			execRepo,
			suggestionRepo,
			ruleRepo,
			nil,
		)

		rule := createTestRule(domain.RuleTypeTransferSuggestion, domain.TriggerTypeEvent)
		triggerData := map[string]interface{}{
			"amount": 100.0,
		}

		execution, err := service.ExecuteRule(ctx, rule, triggerData)

		require.Error(t, err)
		assert.Equal(t, domain.ExecutionStatusFailed, execution.Status)
		assert.NotEmpty(t, execution.ErrorMessage)
	})
}

// TestProcessEventTrigger tests event trigger processing.
func TestProcessEventTrigger(t *testing.T) {
	ctx := context.Background()
	userID := "test-user-id"

	t.Run("processes event trigger for matching rules", func(t *testing.T) {
		execRepo := &mockRuleExecutionRepository{}
		suggestionRepo := &mockTransferSuggestionRepository{}

		actions := RuleActions{
			Transfer: &domain.TransferAction{
				FromAccountID: "account-1",
				ToAccountID:   "account-2",
				AmountType:    "fixed",
				Amount:        100.0,
			},
		}
		actionsJSON, _ := json.Marshal(actions)

		ruleRepo := &mockAutomationRulesRepository{
			rules: []*domain.AutomationRule{
				{
					ID:       "rule-1",
					UserID:   userID,
					Name:     "Test Rule",
					Enabled:  true,
					Priority: 1,
					RuleType: domain.RuleTypeTransferSuggestion,
					TriggerConfig: domain.TriggerConfig{
						Type:  domain.TriggerTypeEvent,
						Event: "transaction_created",
					},
					Conditions:     []domain.AutomationRuleCondition{},
					ConditionLogic: "AND",
					Actions:        actionsJSON,
				},
			},
		}

		service := NewRuleExecutionService(
			execRepo,
			suggestionRepo,
			ruleRepo,
			nil,
		)

		eventData := map[string]interface{}{
			"amount": 100.0,
		}

		err := service.ProcessEventTrigger(ctx, userID, "transaction_created", eventData)

		require.NoError(t, err)
		assert.Len(t, suggestionRepo.suggestions, 1)
	})

	t.Run("processes event with no matching rules", func(t *testing.T) {
		execRepo := &mockRuleExecutionRepository{}
		suggestionRepo := &mockTransferSuggestionRepository{}
		ruleRepo := &mockAutomationRulesRepository{
			// No rules configured
			rules: []*domain.AutomationRule{},
		}

		service := NewRuleExecutionService(
			execRepo,
			suggestionRepo,
			ruleRepo,
			nil,
		)

		eventData := map[string]interface{}{
			"amount": 100.0,
		}

		// Should succeed even with no matching rules
		err := service.ProcessEventTrigger(ctx, userID, "transaction_created", eventData)

		assert.NoError(t, err)
		// No suggestions created since no rules matched
		assert.Empty(t, suggestionRepo.suggestions)
	})

	t.Run("executes rule regardless of high execution count", func(t *testing.T) {
		execRepo := &mockRuleExecutionRepository{
			todayExecutionCount: 1000, // High count
		}
		suggestionRepo := &mockTransferSuggestionRepository{}

		actions := RuleActions{
			Transfer: &domain.TransferAction{
				FromAccountID: "account-1",
				ToAccountID:   "account-2",
				AmountType:    "fixed",
				Amount:        100.0,
			},
		}
		actionsJSON, _ := json.Marshal(actions)

		ruleRepo := &mockAutomationRulesRepository{
			rules: []*domain.AutomationRule{
				{
					ID:       "rule-1",
					UserID:   userID,
					Name:     "Test Rule",
					Enabled:  true,
					RuleType: domain.RuleTypeTransferSuggestion,
					TriggerConfig: domain.TriggerConfig{
						Type:  domain.TriggerTypeEvent,
						Event: "transaction_created",
					},
					Conditions:     []domain.AutomationRuleCondition{},
					ConditionLogic: "AND",
					Actions:        actionsJSON,
				},
			},
		}

		service := NewRuleExecutionService(
			execRepo,
			suggestionRepo,
			ruleRepo,
			nil,
		)

		eventData := map[string]interface{}{
			"amount": 100.0,
		}

		err := service.ProcessEventTrigger(ctx, userID, "transaction_created", eventData)

		require.NoError(t, err)
		assert.Len(t, suggestionRepo.suggestions, 1)
	})
}

// TestGetExecutionHistory tests execution history retrieval.
func TestGetExecutionHistory(t *testing.T) {
	ctx := context.Background()

	t.Run("retrieves execution history for rule", func(t *testing.T) {
		now := time.Now()
		execRepo := &mockRuleExecutionRepository{
			executions: []*domain.RuleExecution{
				{
					ID:        "exec-1",
					RuleID:    "rule-1",
					Status:    domain.ExecutionStatusCompleted,
					StartedAt: now.Add(-1 * time.Hour),
				},
				{
					ID:        "exec-2",
					RuleID:    "rule-1",
					Status:    domain.ExecutionStatusCompleted,
					StartedAt: now,
				},
			},
		}

		service := NewRuleExecutionService(
			execRepo,
			nil,
			nil,
			nil,
		)

		executions, err := service.GetExecutionHistory(ctx, "rule-1", 10, 0)

		require.NoError(t, err)
		assert.Len(t, executions, 2)
	})
}

// Mock automation rules repository for testing.
type mockAutomationRulesRepository struct {
	rules                []*domain.AutomationRule
	createError          error
	eventTriggerRules    []*domain.AutomationRule
	incrementExecCalled  bool
	updateSuccessCalled  bool
}

func (m *mockAutomationRulesRepository) Create(ctx context.Context, rule *domain.AutomationRule) error {
	if m.createError != nil {
		return m.createError
	}
	m.rules = append(m.rules, rule)
	return nil
}

func (m *mockAutomationRulesRepository) GetByID(ctx context.Context, id string) (*domain.AutomationRule, error) {
	for _, r := range m.rules {
		if r.ID == id {
			return r, nil
		}
	}
	return nil, domain.ErrAutomationRuleNotFound
}

func (m *mockAutomationRulesRepository) GetByUserID(ctx context.Context, userID string) ([]*domain.AutomationRule, error) {
	var result []*domain.AutomationRule
	for _, r := range m.rules {
		if r.UserID == userID {
			result = append(result, r)
		}
	}
	return result, nil
}

func (m *mockAutomationRulesRepository) GetEnabledByUserID(ctx context.Context, userID string) ([]*domain.AutomationRule, error) {
	var result []*domain.AutomationRule
	for _, r := range m.rules {
		if r.UserID == userID && r.Enabled {
			result = append(result, r)
		}
	}
	return result, nil
}

func (m *mockAutomationRulesRepository) GetByName(ctx context.Context, userID, name string) (*domain.AutomationRule, error) {
	for _, r := range m.rules {
		if r.UserID == userID && r.Name == name {
			return r, nil
		}
	}
	return nil, domain.ErrAutomationRuleNotFound
}

func (m *mockAutomationRulesRepository) Update(ctx context.Context, rule *domain.AutomationRule) error {
	for i, r := range m.rules {
		if r.ID == rule.ID {
			m.rules[i] = rule
			return nil
		}
	}
	return domain.ErrAutomationRuleNotFound
}

func (m *mockAutomationRulesRepository) Delete(ctx context.Context, id string) error {
	for i, r := range m.rules {
		if r.ID == id {
			m.rules = append(m.rules[:i], m.rules[i+1:]...)
			return nil
		}
	}
	return domain.ErrAutomationRuleNotFound
}

func (m *mockAutomationRulesRepository) CountByUserID(ctx context.Context, userID string) (int, error) {
	count := 0
	for _, r := range m.rules {
		if r.UserID == userID {
			count++
		}
	}
	return count, nil
}

func (m *mockAutomationRulesRepository) IncrementExecutionCount(ctx context.Context, ruleID string) error {
	m.incrementExecCalled = true
	return nil
}

func (m *mockAutomationRulesRepository) UpdateLastSuccessful(ctx context.Context, ruleID string) error {
	m.updateSuccessCalled = true
	return nil
}

func (m *mockAutomationRulesRepository) GetRulesByEventTrigger(ctx context.Context, userID, eventType string) ([]*domain.AutomationRule, error) {
	if m.eventTriggerRules != nil {
		return m.eventTriggerRules, nil
	}
	var result []*domain.AutomationRule
	for _, r := range m.rules {
		if r.UserID == userID && r.Enabled && r.TriggerConfig.Type == domain.TriggerTypeEvent && r.TriggerConfig.Event == eventType {
			result = append(result, r)
		}
	}
	return result, nil
}

func (m *mockAutomationRulesRepository) GetRulesByType(ctx context.Context, userID string, ruleType domain.RuleType) ([]*domain.AutomationRule, error) {
	var result []*domain.AutomationRule
	for _, r := range m.rules {
		if r.UserID == userID && r.Enabled && r.RuleType == ruleType {
			result = append(result, r)
		}
	}
	return result, nil
}

func (m *mockAutomationRulesRepository) GetRulesByScheduleTrigger(ctx context.Context) ([]*domain.AutomationRule, error) {
	var result []*domain.AutomationRule
	for _, r := range m.rules {
		if r.Enabled && r.TriggerConfig.Type == domain.TriggerTypeSchedule && r.TriggerConfig.Cron != "" {
			result = append(result, r)
		}
	}
	return result, nil
}

// ============================================================================
// Issue #36: Savings Goal Executor Tests (TDD)
// ============================================================================

// mockSavingsGoalRepository for testing savings goal executor.
type mockSavingsGoalRepository struct {
	goals     []*domain.SavingsGoal
	findError error
}

func (m *mockSavingsGoalRepository) Create(ctx context.Context, goal *domain.SavingsGoal) error {
	m.goals = append(m.goals, goal)
	return nil
}

func (m *mockSavingsGoalRepository) GetByID(ctx context.Context, id string) (*domain.SavingsGoal, error) {
	if m.findError != nil {
		return nil, m.findError
	}
	for _, g := range m.goals {
		if g.ID == id {
			return g, nil
		}
	}
	return nil, domain.ErrSavingsGoalNotFound
}

func (m *mockSavingsGoalRepository) GetByUserID(ctx context.Context, userID string) ([]*domain.SavingsGoal, error) {
	var result []*domain.SavingsGoal
	for _, g := range m.goals {
		if g.UserID == userID {
			result = append(result, g)
		}
	}
	return result, nil
}

func (m *mockSavingsGoalRepository) GetActiveByUserID(ctx context.Context, userID string) ([]*domain.SavingsGoal, error) {
	var result []*domain.SavingsGoal
	for _, g := range m.goals {
		if g.UserID == userID && !g.IsCompleted {
			result = append(result, g)
		}
	}
	return result, nil
}

func (m *mockSavingsGoalRepository) GetByName(ctx context.Context, userID, name string) (*domain.SavingsGoal, error) {
	for _, g := range m.goals {
		if g.UserID == userID && g.Name == name {
			return g, nil
		}
	}
	return nil, domain.ErrSavingsGoalNotFound
}

func (m *mockSavingsGoalRepository) Update(ctx context.Context, goal *domain.SavingsGoal) error {
	for i, g := range m.goals {
		if g.ID == goal.ID {
			m.goals[i] = goal
			return nil
		}
	}
	return domain.ErrSavingsGoalNotFound
}

func (m *mockSavingsGoalRepository) Delete(ctx context.Context, id, userID string) error {
	for i, g := range m.goals {
		if g.ID == id {
			m.goals = append(m.goals[:i], m.goals[i+1:]...)
			return nil
		}
	}
	return domain.ErrSavingsGoalNotFound
}

func (m *mockSavingsGoalRepository) CountByUserID(ctx context.Context, userID string) (int, error) {
	count := 0
	for _, g := range m.goals {
		if g.UserID == userID {
			count++
		}
	}
	return count, nil
}

func (m *mockSavingsGoalRepository) GetByAccountID(ctx context.Context, accountID string) ([]*domain.SavingsGoal, error) {
	var result []*domain.SavingsGoal
	for _, g := range m.goals {
		if g.AccountID == accountID {
			result = append(result, g)
		}
	}
	return result, nil
}

func (m *mockSavingsGoalRepository) UpdateCurrentAmount(ctx context.Context, goalID string, currentAmount float64) error {
	for _, g := range m.goals {
		if g.ID == goalID {
			g.CurrentAmount = currentAmount
			return nil
		}
	}
	return domain.ErrSavingsGoalNotFound
}

func (m *mockSavingsGoalRepository) MarkCompleted(ctx context.Context, goalID string) error {
	for _, g := range m.goals {
		if g.ID == goalID {
			g.IsCompleted = true
			return nil
		}
	}
	return domain.ErrSavingsGoalNotFound
}

// TestSavingsGoalExecutor tests the savings goal rule executor.
func TestSavingsGoalExecutor(t *testing.T) {
	ctx := context.Background()
	userID := "test-user-id"
	goalID := "savings-goal-123"
	accountID := "savings-account-456"
	fromAccountID := "checking-account-789"

	createSavingsGoalRule := func(action domain.SavingsGoalAction) *domain.AutomationRule {
		actions := RuleActions{
			SavingsGoal: &action,
		}
		actionsJSON, _ := json.Marshal(actions)

		return &domain.AutomationRule{
			ID:       "rule-savings-1",
			UserID:   userID,
			Name:     "Savings Goal Contribution",
			Enabled:  true,
			Priority: 1,
			RuleType: domain.RuleTypeSavingsGoal,
			TriggerConfig: domain.TriggerConfig{
				Type:  domain.TriggerTypeEvent,
				Event: "transaction_created",
			},
			Conditions:     []domain.AutomationRuleCondition{},
			ConditionLogic: "AND",
			Actions:        actionsJSON,
		}
	}

	t.Run("executes savings goal rule with fixed amount", func(t *testing.T) {
		execRepo := &mockRuleExecutionRepository{}
		suggestionRepo := &mockTransferSuggestionRepository{}
		ruleRepo := &mockAutomationRulesRepository{}
		goalRepo := &mockSavingsGoalRepository{
			goals: []*domain.SavingsGoal{
				{
					ID:            goalID,
					UserID:        userID,
					Name:          "Emergency Fund",
					AccountID:     accountID,
					TargetAmount:  10000.0,
					CurrentAmount: 2500.0,
				},
			},
		}

		service := NewRuleExecutionService(
			execRepo,
			suggestionRepo,
			ruleRepo,
			nil,
		)
		service.SetSavingsGoalRepo(goalRepo)

		action := domain.SavingsGoalAction{
			SavingsGoalID: goalID,
			FromAccountID: fromAccountID,
			AmountType:    "fixed",
			Amount:        200.0,
		}
		rule := createSavingsGoalRule(action)

		triggerData := map[string]interface{}{
			"amount": 1000.0, // Paycheck
		}

		execution, err := service.ExecuteRule(ctx, rule, triggerData)

		require.NoError(t, err)
		assert.Equal(t, domain.ExecutionStatusCompleted, execution.Status)

		// Verify suggestion was created
		require.Len(t, suggestionRepo.suggestions, 1)
		suggestion := suggestionRepo.suggestions[0]
		assert.Equal(t, userID, suggestion.UserID)
		assert.Equal(t, fromAccountID, *suggestion.FromAccountID)
		assert.Equal(t, accountID, *suggestion.ToAccountID) // Savings goal's linked account
		assert.Equal(t, 200.0, suggestion.Amount)           // Fixed amount
		assert.Contains(t, *suggestion.Reason, "Emergency Fund")
	})

	t.Run("executes savings goal rule with percentage amount", func(t *testing.T) {
		execRepo := &mockRuleExecutionRepository{}
		suggestionRepo := &mockTransferSuggestionRepository{}
		ruleRepo := &mockAutomationRulesRepository{}
		goalRepo := &mockSavingsGoalRepository{
			goals: []*domain.SavingsGoal{
				{
					ID:            goalID,
					UserID:        userID,
					Name:          "Vacation Fund",
					AccountID:     accountID,
					TargetAmount:  5000.0,
					CurrentAmount: 1000.0,
				},
			},
		}

		service := NewRuleExecutionService(
			execRepo,
			suggestionRepo,
			ruleRepo,
			nil,
		)
		service.SetSavingsGoalRepo(goalRepo)

		percentage := 10.0
		action := domain.SavingsGoalAction{
			SavingsGoalID: goalID,
			FromAccountID: fromAccountID,
			AmountType:    "percentage",
			Percentage:    &percentage,
		}
		rule := createSavingsGoalRule(action)

		triggerData := map[string]interface{}{
			"amount": 500.0, // 10% of 500 = 50
		}

		execution, err := service.ExecuteRule(ctx, rule, triggerData)

		require.NoError(t, err)
		assert.Equal(t, domain.ExecutionStatusCompleted, execution.Status)

		require.Len(t, suggestionRepo.suggestions, 1)
		assert.Equal(t, 50.0, suggestionRepo.suggestions[0].Amount) // 10% of 500
	})

	t.Run("executes savings goal rule with remaining amount", func(t *testing.T) {
		execRepo := &mockRuleExecutionRepository{}
		suggestionRepo := &mockTransferSuggestionRepository{}
		ruleRepo := &mockAutomationRulesRepository{}
		goalRepo := &mockSavingsGoalRepository{
			goals: []*domain.SavingsGoal{
				{
					ID:            goalID,
					UserID:        userID,
					Name:          "New Car Fund",
					AccountID:     accountID,
					TargetAmount:  20000.0,
					CurrentAmount: 5000.0,
				},
			},
		}

		service := NewRuleExecutionService(
			execRepo,
			suggestionRepo,
			ruleRepo,
			nil,
		)
		service.SetSavingsGoalRepo(goalRepo)

		action := domain.SavingsGoalAction{
			SavingsGoalID: goalID,
			FromAccountID: fromAccountID,
			AmountType:    "remaining",
			Threshold:     1000.0, // Keep at least $1000, transfer rest
		}
		rule := createSavingsGoalRule(action)

		triggerData := map[string]interface{}{
			"balance": 1500.0, // Balance after paycheck
		}

		execution, err := service.ExecuteRule(ctx, rule, triggerData)

		require.NoError(t, err)
		assert.Equal(t, domain.ExecutionStatusCompleted, execution.Status)

		require.Len(t, suggestionRepo.suggestions, 1)
		assert.Equal(t, 500.0, suggestionRepo.suggestions[0].Amount) // 1500 - 1000 = 500
	})

	t.Run("fails when savings goal action is missing", func(t *testing.T) {
		execRepo := &mockRuleExecutionRepository{}
		suggestionRepo := &mockTransferSuggestionRepository{}
		ruleRepo := &mockAutomationRulesRepository{}

		service := NewRuleExecutionService(
			execRepo,
			suggestionRepo,
			ruleRepo,
			nil,
		)

		// Create rule without savings goal action
		actions := RuleActions{}
		actionsJSON, _ := json.Marshal(actions)
		rule := &domain.AutomationRule{
			ID:             "rule-savings-bad",
			UserID:         userID,
			Name:           "Bad Savings Rule",
			Enabled:        true,
			RuleType:       domain.RuleTypeSavingsGoal,
			TriggerConfig:  domain.TriggerConfig{Type: domain.TriggerTypeEvent, Event: "test"},
			ConditionLogic: "AND",
			Actions:        actionsJSON,
		}

		execution, err := service.ExecuteRule(ctx, rule, map[string]interface{}{})

		require.Error(t, err)
		assert.Contains(t, err.Error(), "savings goal action not defined")
		assert.Equal(t, domain.ExecutionStatusFailed, execution.Status)
	})

	t.Run("fails when savings goal not found", func(t *testing.T) {
		execRepo := &mockRuleExecutionRepository{}
		suggestionRepo := &mockTransferSuggestionRepository{}
		ruleRepo := &mockAutomationRulesRepository{}
		goalRepo := &mockSavingsGoalRepository{
			goals: []*domain.SavingsGoal{}, // Empty - goal not found
		}

		service := NewRuleExecutionService(
			execRepo,
			suggestionRepo,
			ruleRepo,
			nil,
		)
		service.SetSavingsGoalRepo(goalRepo)

		action := domain.SavingsGoalAction{
			SavingsGoalID: "non-existent-goal",
			FromAccountID: fromAccountID,
			AmountType:    "fixed",
			Amount:        100.0,
		}
		rule := createSavingsGoalRule(action)

		execution, err := service.ExecuteRule(ctx, rule, map[string]interface{}{})

		require.Error(t, err)
		assert.Contains(t, err.Error(), "savings goal not found")
		assert.Equal(t, domain.ExecutionStatusFailed, execution.Status)
	})

	t.Run("skips when calculated amount is zero or negative", func(t *testing.T) {
		execRepo := &mockRuleExecutionRepository{}
		suggestionRepo := &mockTransferSuggestionRepository{}
		ruleRepo := &mockAutomationRulesRepository{}
		goalRepo := &mockSavingsGoalRepository{
			goals: []*domain.SavingsGoal{
				{
					ID:        goalID,
					UserID:    userID,
					Name:      "Test Fund",
					AccountID: accountID,
				},
			},
		}

		service := NewRuleExecutionService(
			execRepo,
			suggestionRepo,
			ruleRepo,
			nil,
		)
		service.SetSavingsGoalRepo(goalRepo)

		action := domain.SavingsGoalAction{
			SavingsGoalID: goalID,
			FromAccountID: fromAccountID,
			AmountType:    "remaining",
			Threshold:     1000.0,
		}
		rule := createSavingsGoalRule(action)

		triggerData := map[string]interface{}{
			"balance": 500.0, // Less than threshold
		}

		execution, err := service.ExecuteRule(ctx, rule, triggerData)

		require.NoError(t, err)
		assert.Equal(t, domain.ExecutionStatusCompleted, execution.Status)
		// No suggestion created when amount <= 0
		assert.Empty(t, suggestionRepo.suggestions)
	})
}

// ============================================================================
// Issue #38: Test Rule Execution (TDD - Dry-Run)
// ============================================================================

// TestTestRule tests the dry-run rule execution for the test endpoint.
func TestTestRule(t *testing.T) {
	ctx := context.Background()
	userID := "test-user-id"

	createTestTransferRule := func() *domain.AutomationRule {
		actions := RuleActions{
			Transfer: &domain.TransferAction{
				FromAccountID: "account-1",
				ToAccountID:   "account-2",
				AmountType:    "fixed",
				Amount:        100.0,
			},
		}
		actionsJSON, _ := json.Marshal(actions)

		return &domain.AutomationRule{
			ID:       "rule-1",
			UserID:   userID,
			Name:     "Test Transfer Rule",
			Enabled:  true,
			Priority: 1,
			RuleType: domain.RuleTypeTransferSuggestion,
			TriggerConfig: domain.TriggerConfig{
				Type:  domain.TriggerTypeEvent,
				Event: "transaction_created",
			},
			Conditions: []domain.AutomationRuleCondition{
				{Field: "amount", Operator: domain.ConditionOperatorGt, Value: "50"},
			},
			ConditionLogic: "AND",
			Actions:        actionsJSON,
		}
	}

	createTestAlertRule := func() *domain.AutomationRule {
		actions := RuleActions{
			Alert: &domain.AlertAction{
				NotificationChannels: []string{"email", "in_app"},
				ThresholdType:        "percentage",
				ThresholdValue:       75.0,
			},
		}
		actionsJSON, _ := json.Marshal(actions)

		return &domain.AutomationRule{
			ID:       "rule-2",
			UserID:   userID,
			Name:     "Budget Alert Rule",
			Enabled:  true,
			Priority: 1,
			RuleType: domain.RuleTypeBudgetAlert,
			TriggerConfig: domain.TriggerConfig{
				Type:  domain.TriggerTypeEvent,
				Event: "budget_updated",
			},
			Conditions: []domain.AutomationRuleCondition{
				{Field: "percent_used", Operator: domain.ConditionOperatorGte, Value: "75"},
			},
			ConditionLogic: "AND",
			Actions:        actionsJSON,
		}
	}

	t.Run("returns test result with condition evaluations when conditions met", func(t *testing.T) {
		execRepo := &mockRuleExecutionRepository{}
		suggestionRepo := &mockTransferSuggestionRepository{}
		ruleRepo := &mockAutomationRulesRepository{}

		service := NewRuleExecutionService(
			execRepo,
			suggestionRepo,
			ruleRepo,
			nil,
		)

		rule := createTestTransferRule()
		triggerData := map[string]interface{}{
			"amount": 100.0, // Greater than 50
		}

		result, err := service.TestRule(ctx, rule, triggerData)

		require.NoError(t, err)
		assert.Equal(t, rule.ID, result.RuleID)
		assert.Equal(t, rule.Name, result.RuleName)
		assert.Equal(t, rule.RuleType, result.RuleType)
		assert.True(t, result.ConditionsMet)
		assert.Len(t, result.ConditionResults, 1)
		assert.True(t, result.ConditionResults[0].Met)
		assert.Equal(t, "amount", result.ConditionResults[0].Field)
		assert.NotEmpty(t, result.SimulatedAction)
		assert.NotEmpty(t, result.TriggerDataUsed)
	})

	t.Run("returns test result with conditions not met", func(t *testing.T) {
		execRepo := &mockRuleExecutionRepository{}
		suggestionRepo := &mockTransferSuggestionRepository{}
		ruleRepo := &mockAutomationRulesRepository{}

		service := NewRuleExecutionService(
			execRepo,
			suggestionRepo,
			ruleRepo,
			nil,
		)

		rule := createTestTransferRule()
		triggerData := map[string]interface{}{
			"amount": 25.0, // Less than 50, condition not met
		}

		result, err := service.TestRule(ctx, rule, triggerData)

		require.NoError(t, err)
		assert.False(t, result.ConditionsMet)
		assert.Len(t, result.ConditionResults, 1)
		assert.False(t, result.ConditionResults[0].Met)
		assert.Empty(t, result.SimulatedAction) // No action simulated when conditions not met
	})

	t.Run("does not persist execution record", func(t *testing.T) {
		execRepo := &mockRuleExecutionRepository{}
		suggestionRepo := &mockTransferSuggestionRepository{}
		ruleRepo := &mockAutomationRulesRepository{}

		service := NewRuleExecutionService(
			execRepo,
			suggestionRepo,
			ruleRepo,
			nil,
		)

		rule := createTestTransferRule()
		triggerData := map[string]interface{}{
			"amount": 100.0,
		}

		_, err := service.TestRule(ctx, rule, triggerData)

		require.NoError(t, err)
		// Verify no execution record was created
		assert.Empty(t, execRepo.executions)
	})

	t.Run("does not create transfer suggestions", func(t *testing.T) {
		execRepo := &mockRuleExecutionRepository{}
		suggestionRepo := &mockTransferSuggestionRepository{}
		ruleRepo := &mockAutomationRulesRepository{}

		service := NewRuleExecutionService(
			execRepo,
			suggestionRepo,
			ruleRepo,
			nil,
		)

		rule := createTestTransferRule()
		triggerData := map[string]interface{}{
			"amount": 100.0,
		}

		_, err := service.TestRule(ctx, rule, triggerData)

		require.NoError(t, err)
		// Verify no suggestions were created
		assert.Empty(t, suggestionRepo.suggestions)
	})

	t.Run("works on disabled rules for preview", func(t *testing.T) {
		execRepo := &mockRuleExecutionRepository{}
		suggestionRepo := &mockTransferSuggestionRepository{}
		ruleRepo := &mockAutomationRulesRepository{}

		service := NewRuleExecutionService(
			execRepo,
			suggestionRepo,
			ruleRepo,
			nil,
		)

		rule := createTestTransferRule()
		rule.Enabled = false // Disabled rule

		triggerData := map[string]interface{}{
			"amount": 100.0,
		}

		result, err := service.TestRule(ctx, rule, triggerData)

		require.NoError(t, err)
		// Test should still work on disabled rules
		assert.Equal(t, rule.ID, result.RuleID)
		assert.True(t, result.ConditionsMet)
	})

	t.Run("simulates budget alert action", func(t *testing.T) {
		execRepo := &mockRuleExecutionRepository{}
		suggestionRepo := &mockTransferSuggestionRepository{}
		ruleRepo := &mockAutomationRulesRepository{}

		service := NewRuleExecutionService(
			execRepo,
			suggestionRepo,
			ruleRepo,
			nil,
		)

		rule := createTestAlertRule()
		triggerData := map[string]interface{}{
			"percent_used": 82.5,
		}

		result, err := service.TestRule(ctx, rule, triggerData)

		require.NoError(t, err)
		assert.True(t, result.ConditionsMet)
		assert.NotEmpty(t, result.SimulatedAction)
		// Check simulated action contains notification channels
		action, ok := result.SimulatedAction["notification_channels"]
		assert.True(t, ok)
		assert.NotNil(t, action)
	})

	t.Run("uses provided trigger data", func(t *testing.T) {
		execRepo := &mockRuleExecutionRepository{}
		suggestionRepo := &mockTransferSuggestionRepository{}
		ruleRepo := &mockAutomationRulesRepository{}

		service := NewRuleExecutionService(
			execRepo,
			suggestionRepo,
			ruleRepo,
			nil,
		)

		rule := createTestTransferRule()
		triggerData := map[string]interface{}{
			"amount":      100.0,
			"custom_field": "custom_value",
		}

		result, err := service.TestRule(ctx, rule, triggerData)

		require.NoError(t, err)
		assert.Equal(t, triggerData, result.TriggerDataUsed)
	})

	t.Run("evaluates multiple conditions with AND logic", func(t *testing.T) {
		execRepo := &mockRuleExecutionRepository{}
		suggestionRepo := &mockTransferSuggestionRepository{}
		ruleRepo := &mockAutomationRulesRepository{}

		service := NewRuleExecutionService(
			execRepo,
			suggestionRepo,
			ruleRepo,
			nil,
		)

		rule := createTestTransferRule()
		rule.Conditions = []domain.AutomationRuleCondition{
			{Field: "amount", Operator: domain.ConditionOperatorGt, Value: "50"},
			{Field: "merchant", Operator: domain.ConditionOperatorEq, Value: "Amazon"},
		}
		rule.ConditionLogic = "AND"

		triggerData := map[string]interface{}{
			"amount":   100.0,
			"merchant": "Amazon",
		}

		result, err := service.TestRule(ctx, rule, triggerData)

		require.NoError(t, err)
		assert.True(t, result.ConditionsMet)
		assert.Len(t, result.ConditionResults, 2)
		assert.True(t, result.ConditionResults[0].Met)
		assert.True(t, result.ConditionResults[1].Met)
	})

	t.Run("evaluates multiple conditions with OR logic", func(t *testing.T) {
		execRepo := &mockRuleExecutionRepository{}
		suggestionRepo := &mockTransferSuggestionRepository{}
		ruleRepo := &mockAutomationRulesRepository{}

		service := NewRuleExecutionService(
			execRepo,
			suggestionRepo,
			ruleRepo,
			nil,
		)

		rule := createTestTransferRule()
		rule.Conditions = []domain.AutomationRuleCondition{
			{Field: "amount", Operator: domain.ConditionOperatorGt, Value: "500"}, // Will not match
			{Field: "merchant", Operator: domain.ConditionOperatorEq, Value: "Amazon"}, // Will match
		}
		rule.ConditionLogic = "OR"

		triggerData := map[string]interface{}{
			"amount":   100.0,  // Less than 500
			"merchant": "Amazon",
		}

		result, err := service.TestRule(ctx, rule, triggerData)

		require.NoError(t, err)
		assert.True(t, result.ConditionsMet) // OR logic - at least one matches
		assert.Len(t, result.ConditionResults, 2)
	})
}

// TestTestRule_PFCConditionPopulatesDisplay verifies that TestRule wires up
// PopulatePFCDisplay so value_display and actual_display appear in the response.
func TestTestRule_PFCConditionPopulatesDisplay(t *testing.T) {
	ctx := context.Background()

	execRepo := &mockRuleExecutionRepository{}
	suggestionRepo := &mockTransferSuggestionRepository{}
	ruleRepo := &mockAutomationRulesRepository{}

	service := NewRuleExecutionService(execRepo, suggestionRepo, ruleRepo, nil)

	actions, _ := json.Marshal(RuleActions{})
	rule := &domain.AutomationRule{
		ID:   "rule-pfc-test",
		Name: "PFC dry-run",
		Conditions: []domain.AutomationRuleCondition{
			{Field: "transaction.pfc_primary", Operator: domain.ConditionOperatorEq, Value: "FOOD_AND_DRINK"},
		},
		ConditionLogic: "AND",
		Actions:        actions,
	}
	triggerData := map[string]interface{}{
		"transaction.pfc_primary": "FOOD_AND_DRINK",
	}

	result, err := service.TestRule(ctx, rule, triggerData)
	require.NoError(t, err)
	require.Len(t, result.ConditionResults, 1)
	assert.Equal(t, "Food And Drink", result.ConditionResults[0].ValueDisplay)
	assert.Equal(t, "Food And Drink", result.ConditionResults[0].ActualDisplay)
}
