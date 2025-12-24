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

		userGetter := &mockUserGetterSimple{
			userID: userID,
		}

		subChecker := &mockSubscriptionCheckerSimple{
			limits: domain.SubscriptionLimits{
				ExecutionsPerDay:            10,
				MinExecutionIntervalMinutes: 240, // Premium tier
			},
		}

		service := NewRuleExecutionService(
			execRepo,
			suggestionRepo,
			ruleRepo,
			nil,
		)
		service.SetUserGetter(userGetter)
		service.SetSubscriptionChecker(subChecker)

		eventData := map[string]interface{}{
			"amount": 100.0,
		}

		err := service.ProcessEventTrigger(ctx, userID, "transaction_created", eventData)

		require.NoError(t, err)
		assert.Len(t, suggestionRepo.suggestions, 1)
	})

	t.Run("enforces daily execution limit", func(t *testing.T) {
		execRepo := &mockRuleExecutionRepository{
			todayExecutionCount: 10, // Already at limit
		}
		suggestionRepo := &mockTransferSuggestionRepository{}
		ruleRepo := &mockAutomationRulesRepository{}

		userGetter := &mockUserGetterSimple{
			userID: userID,
		}

		subChecker := &mockSubscriptionCheckerSimple{
			limits: domain.SubscriptionLimits{
				ExecutionsPerDay:            10, // Limit reached
				MinExecutionIntervalMinutes: 240,
			},
		}

		service := NewRuleExecutionService(
			execRepo,
			suggestionRepo,
			ruleRepo,
			nil,
		)
		service.SetUserGetter(userGetter)
		service.SetSubscriptionChecker(subChecker)

		eventData := map[string]interface{}{
			"amount": 100.0,
		}

		err := service.ProcessEventTrigger(ctx, userID, "transaction_created", eventData)

		assert.ErrorIs(t, err, domain.ErrExecutionLimitExceeded)
	})

	t.Run("silently skips for free tier (scheduled only)", func(t *testing.T) {
		execRepo := &mockRuleExecutionRepository{}
		suggestionRepo := &mockTransferSuggestionRepository{}
		ruleRepo := &mockAutomationRulesRepository{}

		userGetter := &mockUserGetterSimple{
			userID: userID,
		}

		subChecker := &mockSubscriptionCheckerSimple{
			limits: domain.SubscriptionLimits{
				ExecutionsPerDay:            1,
				MinExecutionIntervalMinutes: 1440, // Free tier = scheduled only
			},
		}

		service := NewRuleExecutionService(
			execRepo,
			suggestionRepo,
			ruleRepo,
			nil,
		)
		service.SetUserGetter(userGetter)
		service.SetSubscriptionChecker(subChecker)

		eventData := map[string]interface{}{
			"amount": 100.0,
		}

		// Should not error, just silently skip
		err := service.ProcessEventTrigger(ctx, userID, "transaction_created", eventData)

		assert.NoError(t, err)
		// No rules executed
		assert.Empty(t, suggestionRepo.suggestions)
	})

	t.Run("unlimited executions with -1 limit", func(t *testing.T) {
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

		userGetter := &mockUserGetterSimple{
			userID: userID,
		}

		subChecker := &mockSubscriptionCheckerSimple{
			limits: domain.SubscriptionLimits{
				ExecutionsPerDay:            -1, // Unlimited
				MinExecutionIntervalMinutes: 10, // Premium+
			},
		}

		service := NewRuleExecutionService(
			execRepo,
			suggestionRepo,
			ruleRepo,
			nil,
		)
		service.SetUserGetter(userGetter)
		service.SetSubscriptionChecker(subChecker)

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

// Simple mock implementations for UserGetter and SubscriptionChecker interfaces.

type mockUserGetterSimple struct {
	userID string
}

func (m *mockUserGetterSimple) GetUserByID(ctx context.Context, userID string) (*domain.User, error) {
	return &domain.User{ID: m.userID}, nil
}

type mockSubscriptionCheckerSimple struct {
	limits domain.SubscriptionLimits
}

func (m *mockSubscriptionCheckerSimple) GetEffectiveSubscription(ctx context.Context, user *domain.User) (*domain.EffectiveSubscription, error) {
	return &domain.EffectiveSubscription{
		UserID: user.ID,
		Limits: m.limits,
	}, nil
}
