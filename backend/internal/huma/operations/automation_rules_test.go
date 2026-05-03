// Package operations provides HTTP handlers for the BoostedBudget API.
// Issue #37: Rule Scheduler - Automation rule operations tests.
package operations

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/huma/inputs"
	"github.com/maxnussbaum/BoostedBudget/internal/workers"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// ============================================================================
// Mock Services
// ============================================================================

// MockAutomationRuleService implements services.AutomationRuleService for testing.
type MockAutomationRuleService struct {
	mock.Mock
}

func (m *MockAutomationRuleService) CreateRule(ctx context.Context, userID string, req *domain.CreateAutomationRuleRequest) (*domain.AutomationRule, error) {
	args := m.Called(ctx, userID, req)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*domain.AutomationRule), args.Error(1)
}

func (m *MockAutomationRuleService) GetUserRules(ctx context.Context, userID string) ([]*domain.AutomationRule, error) {
	args := m.Called(ctx, userID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]*domain.AutomationRule), args.Error(1)
}

func (m *MockAutomationRuleService) GetRuleByID(ctx context.Context, userID, ruleID string) (*domain.AutomationRule, error) {
	args := m.Called(ctx, userID, ruleID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*domain.AutomationRule), args.Error(1)
}

func (m *MockAutomationRuleService) UpdateRule(ctx context.Context, userID, ruleID string, req *domain.UpdateAutomationRuleRequest) (*domain.AutomationRule, error) {
	args := m.Called(ctx, userID, ruleID, req)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*domain.AutomationRule), args.Error(1)
}

func (m *MockAutomationRuleService) DeleteRule(ctx context.Context, userID, ruleID string) error {
	args := m.Called(ctx, userID, ruleID)
	return args.Error(0)
}

func (m *MockAutomationRuleService) ToggleRule(ctx context.Context, userID, ruleID string, enabled bool) (*domain.AutomationRule, error) {
	args := m.Called(ctx, userID, ruleID, enabled)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*domain.AutomationRule), args.Error(1)
}

func (m *MockAutomationRuleService) GetEnabledRules(ctx context.Context, userID string) ([]*domain.AutomationRule, error) {
	args := m.Called(ctx, userID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]*domain.AutomationRule), args.Error(1)
}

func (m *MockAutomationRuleService) GetRulesByType(ctx context.Context, userID string, ruleType domain.RuleType) ([]*domain.AutomationRule, error) {
	args := m.Called(ctx, userID, ruleType)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]*domain.AutomationRule), args.Error(1)
}

// MockRuleExecutionService implements services.RuleExecutionService for testing.
// Issue #38: Rules API Endpoints.
type MockRuleExecutionService struct {
	mock.Mock
}

func (m *MockRuleExecutionService) ExecuteRule(ctx context.Context, rule *domain.AutomationRule, triggerData map[string]interface{}) (*domain.RuleExecution, error) {
	args := m.Called(ctx, rule, triggerData)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*domain.RuleExecution), args.Error(1)
}

func (m *MockRuleExecutionService) ProcessEventTrigger(ctx context.Context, userID, eventType string, eventData map[string]interface{}) error {
	args := m.Called(ctx, userID, eventType, eventData)
	return args.Error(0)
}

func (m *MockRuleExecutionService) GetExecutionHistory(ctx context.Context, ruleID string, limit, offset int) ([]*domain.RuleExecution, error) {
	args := m.Called(ctx, ruleID, limit, offset)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]*domain.RuleExecution), args.Error(1)
}

func (m *MockRuleExecutionService) GetExecutionHistoryWithFilter(ctx context.Context, ruleID string, status *domain.ExecutionStatus, limit, offset int) ([]*domain.RuleExecution, int, error) {
	args := m.Called(ctx, ruleID, status, limit, offset)
	if args.Get(0) == nil {
		return nil, args.Int(1), args.Error(2)
	}
	return args.Get(0).([]*domain.RuleExecution), args.Int(1), args.Error(2)
}

func (m *MockRuleExecutionService) TestRule(ctx context.Context, rule *domain.AutomationRule, triggerData map[string]interface{}) (*domain.RuleTestResult, error) {
	args := m.Called(ctx, rule, triggerData)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*domain.RuleTestResult), args.Error(1)
}

// ============================================================================
// Test Helpers
// ============================================================================

func createMockRule(id, userID string) *domain.AutomationRule {
	return &domain.AutomationRule{
		ID:       id,
		UserID:   userID,
		Name:     "Test Rule " + id,
		Enabled:  true,
		RuleType: domain.RuleTypeBudgetAlert,
		TriggerConfig: domain.TriggerConfig{
			Type: domain.TriggerTypeSchedule,
			Cron: "0 9 * * *",
		},
		Actions: json.RawMessage("[]"),
	}
}

// ============================================================================
// Tests
// ============================================================================

func TestAutomationRuleHandler_GetAutomationRules(t *testing.T) {
	mockService := new(MockAutomationRuleService)

	rules := []*domain.AutomationRule{
		createMockRule("rule-1", "user-1"),
		createMockRule("rule-2", "user-1"),
	}

	mockService.On("GetUserRules", mock.Anything, "user-1").Return(rules, nil)

	handler := NewAutomationRuleHandler(mockService, nil)

	input := &GetAutomationRulesInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-1"},
	}

	result, err := handler.GetAutomationRules(context.Background(), input)

	require.NoError(t, err)
	assert.Len(t, result.Body, 2)
	mockService.AssertExpectations(t)
}

func TestAutomationRuleHandler_GetAutomationRules_Empty(t *testing.T) {
	mockService := new(MockAutomationRuleService)

	// Return nil to test empty handling
	mockService.On("GetUserRules", mock.Anything, "user-1").Return(nil, nil)

	handler := NewAutomationRuleHandler(mockService, nil)

	input := &GetAutomationRulesInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-1"},
	}

	result, err := handler.GetAutomationRules(context.Background(), input)

	require.NoError(t, err)
	assert.NotNil(t, result.Body)
	assert.Len(t, result.Body, 0)
}

func TestAutomationRuleHandler_GetAutomationRuleByID(t *testing.T) {
	mockService := new(MockAutomationRuleService)

	rule := createMockRule("rule-1", "user-1")
	mockService.On("GetRuleByID", mock.Anything, "user-1", "rule-1").Return(rule, nil)

	handler := NewAutomationRuleHandler(mockService, nil)

	input := &GetAutomationRuleByIDInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-1"},
		ID:                 "rule-1",
	}

	result, err := handler.GetAutomationRuleByID(context.Background(), input)

	require.NoError(t, err)
	assert.Equal(t, "rule-1", result.Body.ID)
	mockService.AssertExpectations(t)
}

func TestAutomationRuleHandler_GetAutomationRuleByID_NotFound(t *testing.T) {
	mockService := new(MockAutomationRuleService)

	mockService.On("GetRuleByID", mock.Anything, "user-1", "rule-unknown").Return(nil, domain.ErrAutomationRuleNotFound)

	handler := NewAutomationRuleHandler(mockService, nil)

	input := &GetAutomationRuleByIDInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-1"},
		ID:                 "rule-unknown",
	}

	_, err := handler.GetAutomationRuleByID(context.Background(), input)

	assert.Error(t, err)
}

func TestAutomationRuleHandler_TriggerAutomationRule_Success(t *testing.T) {
	mockService := new(MockAutomationRuleService)

	rule := createMockRule("rule-1", "user-1")
	mockService.On("GetRuleByID", mock.Anything, "user-1", "rule-1").Return(rule, nil)

	// Create a manager with a mock manual provider
	manager := workers.NewRuleSchedulerManager(nil)
	mockProvider := &mockManualProvider{
		execution: &domain.RuleExecution{
			ID:     "exec-1",
			RuleID: "rule-1",
			Status: domain.ExecutionStatusCompleted,
		},
	}
	manager.RegisterProvider(mockProvider)
	_ = manager.Start(context.Background())
	defer manager.Stop(context.Background())

	handler := NewAutomationRuleHandler(mockService, manager)

	input := &TriggerAutomationRuleInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-1"},
		ID:                 "rule-1",
	}

	result, err := handler.TriggerAutomationRule(context.Background(), input)

	require.NoError(t, err)
	assert.Equal(t, "exec-1", result.Body.ID)
	assert.Equal(t, domain.ExecutionStatusCompleted, result.Body.Status)
}

func TestAutomationRuleHandler_TriggerAutomationRule_WithTriggerData(t *testing.T) {
	mockService := new(MockAutomationRuleService)

	rule := createMockRule("rule-1", "user-1")
	mockService.On("GetRuleByID", mock.Anything, "user-1", "rule-1").Return(rule, nil)

	manager := workers.NewRuleSchedulerManager(nil)
	mockProvider := &mockManualProvider{
		execution: &domain.RuleExecution{
			ID:     "exec-1",
			RuleID: "rule-1",
			Status: domain.ExecutionStatusCompleted,
		},
	}
	manager.RegisterProvider(mockProvider)
	_ = manager.Start(context.Background())
	defer manager.Stop(context.Background())

	handler := NewAutomationRuleHandler(mockService, manager)

	input := &TriggerAutomationRuleInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-1"},
		ID:                 "rule-1",
	}
	input.Body.TriggerData = map[string]interface{}{
		"custom_field": "value",
	}

	result, err := handler.TriggerAutomationRule(context.Background(), input)

	require.NoError(t, err)
	assert.NotNil(t, result.Body)

	// Verify trigger data was passed
	assert.Equal(t, "value", mockProvider.lastTriggerData["custom_field"])
}

func TestAutomationRuleHandler_TriggerAutomationRule_RuleNotFound(t *testing.T) {
	mockService := new(MockAutomationRuleService)

	mockService.On("GetRuleByID", mock.Anything, "user-1", "rule-unknown").Return(nil, domain.ErrAutomationRuleNotFound)

	manager := workers.NewRuleSchedulerManager(nil)
	handler := NewAutomationRuleHandler(mockService, manager)

	input := &TriggerAutomationRuleInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-1"},
		ID:                 "rule-unknown",
	}

	_, err := handler.TriggerAutomationRule(context.Background(), input)

	assert.Error(t, err)
}

func TestAutomationRuleHandler_TriggerAutomationRule_ManagerNotStarted(t *testing.T) {
	mockService := new(MockAutomationRuleService)

	rule := createMockRule("rule-1", "user-1")
	mockService.On("GetRuleByID", mock.Anything, "user-1", "rule-1").Return(rule, nil)

	// Manager not started
	manager := workers.NewRuleSchedulerManager(nil)
	handler := NewAutomationRuleHandler(mockService, manager)

	input := &TriggerAutomationRuleInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-1"},
		ID:                 "rule-1",
	}

	_, err := handler.TriggerAutomationRule(context.Background(), input)

	assert.Error(t, err)
}

func TestAutomationRuleHandler_TriggerAutomationRule_NoSchedulerManager(t *testing.T) {
	mockService := new(MockAutomationRuleService)

	rule := createMockRule("rule-1", "user-1")
	mockService.On("GetRuleByID", mock.Anything, "user-1", "rule-1").Return(rule, nil)

	// No scheduler manager
	handler := NewAutomationRuleHandler(mockService, nil)

	input := &TriggerAutomationRuleInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-1"},
		ID:                 "rule-1",
	}

	_, err := handler.TriggerAutomationRule(context.Background(), input)

	assert.Error(t, err)
}

func TestAutomationRuleHandler_ToggleAutomationRule(t *testing.T) {
	mockService := new(MockAutomationRuleService)

	rule := createMockRule("rule-1", "user-1")
	rule.Enabled = false

	mockService.On("ToggleRule", mock.Anything, "user-1", "rule-1", true).Return(rule, nil)

	handler := NewAutomationRuleHandler(mockService, nil)

	input := &ToggleAutomationRuleInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-1"},
		ID:                 "rule-1",
	}
	input.Body.Enabled = true

	result, err := handler.ToggleAutomationRule(context.Background(), input)

	require.NoError(t, err)
	assert.Equal(t, "rule-1", result.Body.ID)
	mockService.AssertExpectations(t)
}

func TestAutomationRuleHandler_DeleteAutomationRule(t *testing.T) {
	mockService := new(MockAutomationRuleService)

	mockService.On("DeleteRule", mock.Anything, "user-1", "rule-1").Return(nil)

	handler := NewAutomationRuleHandler(mockService, nil)

	input := &DeleteAutomationRuleInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-1"},
		ID:                 "rule-1",
	}

	_, err := handler.DeleteAutomationRule(context.Background(), input)

	require.NoError(t, err)
	mockService.AssertExpectations(t)
}

// ============================================================================
// Mock Manual Provider for Testing
// ============================================================================

type mockManualProvider struct {
	execution       *domain.RuleExecution
	lastTriggerData map[string]interface{}
}

func (m *mockManualProvider) Name() string {
	return "manual"
}

func (m *mockManualProvider) SupportsManualTrigger() bool {
	return true
}

func (m *mockManualProvider) Start(ctx context.Context) error {
	return nil
}

func (m *mockManualProvider) Stop(ctx context.Context) error {
	return nil
}

func (m *mockManualProvider) TriggerRule(ctx context.Context, ruleID string, triggerData map[string]interface{}) (*domain.RuleExecution, error) {
	m.lastTriggerData = triggerData
	return m.execution, nil
}

// ============================================================================
// Issue #38: Test, Enable, Disable, Executions Handler Tests
// ============================================================================

func TestAutomationRuleHandler_TestAutomationRule_Success(t *testing.T) {
	mockRuleService := new(MockAutomationRuleService)
	mockExecService := new(MockRuleExecutionService)

	rule := createMockRule("rule-1", "user-1")
	mockRuleService.On("GetRuleByID", mock.Anything, "user-1", "rule-1").Return(rule, nil)

	testResult := &domain.RuleTestResult{
		RuleID:        "rule-1",
		RuleName:      rule.Name,
		RuleType:      rule.RuleType,
		ConditionsMet: true,
		ConditionResults: []domain.ConditionEvalResult{
			{Field: "amount", Operator: "gt", Value: "50", Actual: float64(100), Met: true},
		},
		SimulatedAction: map[string]interface{}{"action_type": "budget_alert"},
		TriggerDataUsed: map[string]interface{}{"amount": float64(100)},
	}
	mockExecService.On("TestRule", mock.Anything, rule, mock.Anything).Return(testResult, nil)

	handler := NewAutomationRuleHandler(mockRuleService, nil)
	handler.SetExecutionService(mockExecService)

	input := &TestAutomationRuleInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-1"},
		ID:                 "rule-1",
	}
	input.Body.TriggerData = map[string]interface{}{"amount": float64(100)}

	result, err := handler.TestAutomationRule(context.Background(), input)

	require.NoError(t, err)
	assert.Equal(t, "rule-1", result.Body.RuleID)
	assert.True(t, result.Body.ConditionsMet)
	assert.Len(t, result.Body.ConditionResults, 1)
	mockRuleService.AssertExpectations(t)
	mockExecService.AssertExpectations(t)
}

func TestAutomationRuleHandler_TestAutomationRule_RuleNotFound(t *testing.T) {
	mockRuleService := new(MockAutomationRuleService)
	mockExecService := new(MockRuleExecutionService)

	mockRuleService.On("GetRuleByID", mock.Anything, "user-1", "rule-unknown").Return(nil, domain.ErrAutomationRuleNotFound)

	handler := NewAutomationRuleHandler(mockRuleService, nil)
	handler.SetExecutionService(mockExecService)

	input := &TestAutomationRuleInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-1"},
		ID:                 "rule-unknown",
	}

	_, err := handler.TestAutomationRule(context.Background(), input)

	assert.Error(t, err)
}

func TestAutomationRuleHandler_TestAutomationRule_NoExecutionService(t *testing.T) {
	mockRuleService := new(MockAutomationRuleService)

	rule := createMockRule("rule-1", "user-1")
	mockRuleService.On("GetRuleByID", mock.Anything, "user-1", "rule-1").Return(rule, nil)

	handler := NewAutomationRuleHandler(mockRuleService, nil)
	// No execution service set

	input := &TestAutomationRuleInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-1"},
		ID:                 "rule-1",
	}

	_, err := handler.TestAutomationRule(context.Background(), input)

	assert.Error(t, err)
}

func TestAutomationRuleHandler_TestAutomationRule_PFCDisplayFieldsPassThrough(t *testing.T) {
	mockRuleService := new(MockAutomationRuleService)
	mockExecService := new(MockRuleExecutionService)

	rule := createMockRule("rule-1", "user-1")
	mockRuleService.On("GetRuleByID", mock.Anything, "user-1", "rule-1").Return(rule, nil)

	testResult := &domain.RuleTestResult{
		RuleID:        "rule-1",
		RuleName:      rule.Name,
		RuleType:      rule.RuleType,
		ConditionsMet: true,
		ConditionResults: []domain.ConditionEvalResult{
			{
				Field:         "transaction.pfc_primary",
				Operator:      "eq",
				Value:         "FOOD_AND_DRINK",
				ValueDisplay:  "Food And Drink",
				Actual:        "FOOD_AND_DRINK",
				ActualDisplay: "Food And Drink",
				Met:           true,
			},
		},
		TriggerDataUsed: map[string]interface{}{"transaction.pfc_primary": "FOOD_AND_DRINK"},
	}
	mockExecService.On("TestRule", mock.Anything, rule, mock.Anything).Return(testResult, nil)

	handler := NewAutomationRuleHandler(mockRuleService, nil)
	handler.SetExecutionService(mockExecService)

	input := &TestAutomationRuleInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-1"},
		ID:                 "rule-1",
	}
	input.Body.TriggerData = map[string]interface{}{"transaction.pfc_primary": "FOOD_AND_DRINK"}

	result, err := handler.TestAutomationRule(context.Background(), input)

	require.NoError(t, err)
	require.Len(t, result.Body.ConditionResults, 1)
	assert.Equal(t, "Food And Drink", result.Body.ConditionResults[0].ValueDisplay)
	assert.Equal(t, "Food And Drink", result.Body.ConditionResults[0].ActualDisplay)
	mockRuleService.AssertExpectations(t)
	mockExecService.AssertExpectations(t)
}

func TestAutomationRuleHandler_EnableAutomationRule(t *testing.T) {
	mockService := new(MockAutomationRuleService)

	rule := createMockRule("rule-1", "user-1")
	rule.Enabled = true

	mockService.On("ToggleRule", mock.Anything, "user-1", "rule-1", true).Return(rule, nil)

	handler := NewAutomationRuleHandler(mockService, nil)

	input := &EnableAutomationRuleInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-1"},
		ID:                 "rule-1",
	}

	result, err := handler.EnableAutomationRule(context.Background(), input)

	require.NoError(t, err)
	assert.Equal(t, "rule-1", result.Body.ID)
	assert.True(t, result.Body.Enabled)
	mockService.AssertExpectations(t)
}

func TestAutomationRuleHandler_DisableAutomationRule(t *testing.T) {
	mockService := new(MockAutomationRuleService)

	rule := createMockRule("rule-1", "user-1")
	rule.Enabled = false

	mockService.On("ToggleRule", mock.Anything, "user-1", "rule-1", false).Return(rule, nil)

	handler := NewAutomationRuleHandler(mockService, nil)

	input := &DisableAutomationRuleInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-1"},
		ID:                 "rule-1",
	}

	result, err := handler.DisableAutomationRule(context.Background(), input)

	require.NoError(t, err)
	assert.Equal(t, "rule-1", result.Body.ID)
	assert.False(t, result.Body.Enabled)
	mockService.AssertExpectations(t)
}

func TestAutomationRuleHandler_GetRuleExecutions_Success(t *testing.T) {
	mockRuleService := new(MockAutomationRuleService)
	mockExecService := new(MockRuleExecutionService)

	rule := createMockRule("rule-1", "user-1")
	mockRuleService.On("GetRuleByID", mock.Anything, "user-1", "rule-1").Return(rule, nil)

	executions := []*domain.RuleExecution{
		{ID: "exec-1", RuleID: "rule-1", Status: domain.ExecutionStatusCompleted},
		{ID: "exec-2", RuleID: "rule-1", Status: domain.ExecutionStatusCompleted},
	}
	mockExecService.On("GetExecutionHistoryWithFilter", mock.Anything, "rule-1", (*domain.ExecutionStatus)(nil), 20, 0).Return(executions, 2, nil)

	handler := NewAutomationRuleHandler(mockRuleService, nil)
	handler.SetExecutionService(mockExecService)

	input := &GetRuleExecutionsInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-1"},
		ID:                 "rule-1",
		Limit:              20,
		Offset:             0,
	}

	result, err := handler.GetRuleExecutions(context.Background(), input)

	require.NoError(t, err)
	assert.Len(t, result.Body.Executions, 2)
	assert.Equal(t, 2, result.Body.Total)
	assert.Equal(t, 20, result.Body.Limit)
	assert.Equal(t, 0, result.Body.Offset)
	mockRuleService.AssertExpectations(t)
	mockExecService.AssertExpectations(t)
}

func TestAutomationRuleHandler_GetRuleExecutions_WithStatusFilter(t *testing.T) {
	mockRuleService := new(MockAutomationRuleService)
	mockExecService := new(MockRuleExecutionService)

	rule := createMockRule("rule-1", "user-1")
	mockRuleService.On("GetRuleByID", mock.Anything, "user-1", "rule-1").Return(rule, nil)

	completedStatus := domain.ExecutionStatusCompleted
	executions := []*domain.RuleExecution{
		{ID: "exec-1", RuleID: "rule-1", Status: domain.ExecutionStatusCompleted},
	}
	mockExecService.On("GetExecutionHistoryWithFilter", mock.Anything, "rule-1", &completedStatus, 20, 0).Return(executions, 1, nil)

	handler := NewAutomationRuleHandler(mockRuleService, nil)
	handler.SetExecutionService(mockExecService)

	input := &GetRuleExecutionsInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-1"},
		ID:                 "rule-1",
		Status:             "completed",
		Limit:              20,
		Offset:             0,
	}

	result, err := handler.GetRuleExecutions(context.Background(), input)

	require.NoError(t, err)
	assert.Len(t, result.Body.Executions, 1)
	assert.Equal(t, 1, result.Body.Total)
}

func TestAutomationRuleHandler_GetRuleExecutions_RuleNotFound(t *testing.T) {
	mockRuleService := new(MockAutomationRuleService)
	mockExecService := new(MockRuleExecutionService)

	mockRuleService.On("GetRuleByID", mock.Anything, "user-1", "rule-unknown").Return(nil, domain.ErrAutomationRuleNotFound)

	handler := NewAutomationRuleHandler(mockRuleService, nil)
	handler.SetExecutionService(mockExecService)

	input := &GetRuleExecutionsInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-1"},
		ID:                 "rule-unknown",
		Limit:              20,
		Offset:             0,
	}

	_, err := handler.GetRuleExecutions(context.Background(), input)

	assert.Error(t, err)
}
