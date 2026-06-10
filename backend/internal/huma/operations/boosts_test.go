// Package operations provides HTTP handlers for the BoostedBudget API.
// Issue #37: Rule Scheduler - Automation rule operations tests.
package operations

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/danielgtaylor/huma/v2"
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

// MockAutomationRuleService implements services.BoostService for testing.
type MockAutomationRuleService struct {
	mock.Mock
}

func (m *MockAutomationRuleService) CreateRule(ctx context.Context, userID string, req *domain.CreateBoostRequest) (*domain.Boost, error) {
	args := m.Called(ctx, userID, req)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*domain.Boost), args.Error(1)
}

func (m *MockAutomationRuleService) GetUserRules(ctx context.Context, userID string) ([]*domain.Boost, error) {
	args := m.Called(ctx, userID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]*domain.Boost), args.Error(1)
}

func (m *MockAutomationRuleService) GetRuleByID(ctx context.Context, userID, ruleID string) (*domain.Boost, error) {
	args := m.Called(ctx, userID, ruleID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*domain.Boost), args.Error(1)
}

func (m *MockAutomationRuleService) UpdateRule(ctx context.Context, userID, ruleID string, req *domain.UpdateBoostRequest) (*domain.Boost, error) {
	args := m.Called(ctx, userID, ruleID, req)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*domain.Boost), args.Error(1)
}

func (m *MockAutomationRuleService) DeleteRule(ctx context.Context, userID, ruleID string) error {
	args := m.Called(ctx, userID, ruleID)
	return args.Error(0)
}

func (m *MockAutomationRuleService) ToggleRule(ctx context.Context, userID, ruleID string, enabled bool) (*domain.Boost, error) {
	args := m.Called(ctx, userID, ruleID, enabled)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*domain.Boost), args.Error(1)
}

func (m *MockAutomationRuleService) GetEnabledRules(ctx context.Context, userID string) ([]*domain.Boost, error) {
	args := m.Called(ctx, userID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]*domain.Boost), args.Error(1)
}

func (m *MockAutomationRuleService) GetUserRulesFiltered(ctx context.Context, userID string, boostTypes []domain.BoostType) ([]*domain.Boost, error) {
	args := m.Called(ctx, userID, boostTypes)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]*domain.Boost), args.Error(1)
}

func (m *MockAutomationRuleService) GetRulesByType(ctx context.Context, userID string, ruleType domain.BoostType) ([]*domain.Boost, error) {
	args := m.Called(ctx, userID, ruleType)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]*domain.Boost), args.Error(1)
}

// MockRuleExecutionService implements services.BoostExecutionService for testing.
// Issue #38: Rules API Endpoints.
type MockRuleExecutionService struct {
	mock.Mock
}

func (m *MockRuleExecutionService) ExecuteBoost(ctx context.Context, rule *domain.Boost, triggerData map[string]interface{}) (*domain.BoostExecution, error) {
	args := m.Called(ctx, rule, triggerData)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*domain.BoostExecution), args.Error(1)
}

func (m *MockRuleExecutionService) ProcessEventTrigger(ctx context.Context, userID, eventType string, eventData map[string]interface{}) error {
	args := m.Called(ctx, userID, eventType, eventData)
	return args.Error(0)
}

func (m *MockRuleExecutionService) GetBoostExecutionHistory(ctx context.Context, ruleID string, limit, offset int) ([]*domain.BoostExecution, error) {
	args := m.Called(ctx, ruleID, limit, offset)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]*domain.BoostExecution), args.Error(1)
}

func (m *MockRuleExecutionService) GetExecutionHistoryWithFilter(ctx context.Context, ruleID string, status *domain.ExecutionStatus, limit, offset int) ([]*domain.BoostExecution, int, error) {
	args := m.Called(ctx, ruleID, status, limit, offset)
	if args.Get(0) == nil {
		return nil, args.Int(1), args.Error(2)
	}
	return args.Get(0).([]*domain.BoostExecution), args.Int(1), args.Error(2)
}

func (m *MockRuleExecutionService) TestBoost(ctx context.Context, rule *domain.Boost, triggerData map[string]interface{}) (*domain.BoostTestResult, error) {
	args := m.Called(ctx, rule, triggerData)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*domain.BoostTestResult), args.Error(1)
}

// ============================================================================
// Test Helpers
// ============================================================================

func createMockRule(id, userID string) *domain.Boost {
	return &domain.Boost{
		ID:       id,
		UserID:   userID,
		Name:     "Test Rule " + id,
		Enabled:  true,
		BoostType: domain.BoostTypeBudgetAlert,
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

	rules := []*domain.Boost{
		createMockRule("rule-1", "user-1"),
		createMockRule("rule-2", "user-1"),
	}

	// No rule_type filter → empty slice passed to GetUserRulesFiltered.
	mockService.On("GetUserRulesFiltered", mock.Anything, "user-1", []domain.BoostType(nil)).Return(rules, nil)

	handler := NewBoostHandler(mockService, nil)

	input := &GetBoostsInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-1"},
	}

	result, err := handler.GetBoosts(context.Background(), input)

	require.NoError(t, err)
	assert.Len(t, result.Body, 2)
	mockService.AssertExpectations(t)
}

func TestAutomationRuleHandler_GetAutomationRules_Empty(t *testing.T) {
	mockService := new(MockAutomationRuleService)

	// Return nil to test empty handling.
	mockService.On("GetUserRulesFiltered", mock.Anything, "user-1", []domain.BoostType(nil)).Return(nil, nil)

	handler := NewBoostHandler(mockService, nil)

	input := &GetBoostsInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-1"},
	}

	result, err := handler.GetBoosts(context.Background(), input)

	require.NoError(t, err)
	assert.NotNil(t, result.Body)
	assert.Len(t, result.Body, 0)
}

// R8-1b: boost_type filter returns only requested types.
func TestBoostHandler_GetBoosts_WithBoostTypeFilter(t *testing.T) {
	mockService := new(MockAutomationRuleService)

	filtered := []*domain.Boost{
		{ID: "r1", UserID: "user-1", BoostType: domain.BoostTypeTransferSuggestion},
		{ID: "r2", UserID: "user-1", BoostType: domain.BoostTypeSavingsGoal},
	}

	expectedTypes := []domain.BoostType{domain.BoostTypeTransferSuggestion, domain.BoostTypeSavingsGoal}
	mockService.On("GetUserRulesFiltered", mock.Anything, "user-1", expectedTypes).Return(filtered, nil)

	handler := NewBoostHandler(mockService, nil)

	input := &GetBoostsInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-1"},
		BoostType:          []string{"transfer_suggestion", "savings_goal"},
	}

	result, err := handler.GetBoosts(context.Background(), input)

	require.NoError(t, err)
	assert.Len(t, result.Body, 2)
	mockService.AssertExpectations(t)
}

// R8-1b: invalid boost_type value returns 422.
func TestBoostHandler_GetBoosts_InvalidBoostType(t *testing.T) {
	mockService := new(MockAutomationRuleService)
	handler := NewBoostHandler(mockService, nil)

	input := &GetBoostsInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-1"},
		BoostType:          []string{"transfer_suggestion", "invalid_type"},
	}

	_, err := handler.GetBoosts(context.Background(), input)

	require.Error(t, err)
	var humaErr huma.StatusError
	require.ErrorAs(t, err, &humaErr)
	assert.Equal(t, 422, humaErr.GetStatus())
}

func TestAutomationRuleHandler_GetAutomationRuleByID(t *testing.T) {
	mockService := new(MockAutomationRuleService)

	rule := createMockRule("rule-1", "user-1")
	mockService.On("GetRuleByID", mock.Anything, "user-1", "rule-1").Return(rule, nil)

	handler := NewBoostHandler(mockService, nil)

	input := &GetBoostByIDInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-1"},
		ID:                 "rule-1",
	}

	result, err := handler.GetBoostByID(context.Background(), input)

	require.NoError(t, err)
	assert.Equal(t, "rule-1", result.Body.ID)
	mockService.AssertExpectations(t)
}

func TestAutomationRuleHandler_GetAutomationRuleByID_NotFound(t *testing.T) {
	mockService := new(MockAutomationRuleService)

	mockService.On("GetRuleByID", mock.Anything, "user-1", "rule-unknown").Return(nil, domain.ErrBoostNotFound)

	handler := NewBoostHandler(mockService, nil)

	input := &GetBoostByIDInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-1"},
		ID:                 "rule-unknown",
	}

	_, err := handler.GetBoostByID(context.Background(), input)

	assert.Error(t, err)
}

func TestAutomationRuleHandler_TriggerAutomationRule_Success(t *testing.T) {
	mockService := new(MockAutomationRuleService)

	rule := createMockRule("rule-1", "user-1")
	mockService.On("GetRuleByID", mock.Anything, "user-1", "rule-1").Return(rule, nil)

	// Create a manager with a mock manual provider
	manager := workers.NewBoostSchedulerManager(nil)
	mockProvider := &mockManualProvider{
		execution: &domain.BoostExecution{
			ID:     "exec-1",
			BoostID: "rule-1",
			Status: domain.ExecutionStatusCompleted,
		},
	}
	manager.RegisterProvider(mockProvider)
	_ = manager.Start(context.Background())
	defer manager.Stop(context.Background())

	handler := NewBoostHandler(mockService, manager)

	input := &TriggerBoostInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-1"},
		ID:                 "rule-1",
	}

	result, err := handler.TriggerBoost(context.Background(), input)

	require.NoError(t, err)
	assert.Equal(t, "exec-1", result.Body.ID)
	assert.Equal(t, domain.ExecutionStatusCompleted, result.Body.Status)
}

func TestAutomationRuleHandler_TriggerAutomationRule_WithTriggerData(t *testing.T) {
	mockService := new(MockAutomationRuleService)

	rule := createMockRule("rule-1", "user-1")
	mockService.On("GetRuleByID", mock.Anything, "user-1", "rule-1").Return(rule, nil)

	manager := workers.NewBoostSchedulerManager(nil)
	mockProvider := &mockManualProvider{
		execution: &domain.BoostExecution{
			ID:     "exec-1",
			BoostID: "rule-1",
			Status: domain.ExecutionStatusCompleted,
		},
	}
	manager.RegisterProvider(mockProvider)
	_ = manager.Start(context.Background())
	defer manager.Stop(context.Background())

	handler := NewBoostHandler(mockService, manager)

	input := &TriggerBoostInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-1"},
		ID:                 "rule-1",
	}
	input.Body.TriggerData = map[string]interface{}{
		"custom_field": "value",
	}

	result, err := handler.TriggerBoost(context.Background(), input)

	require.NoError(t, err)
	assert.NotNil(t, result.Body)

	// Verify trigger data was passed
	assert.Equal(t, "value", mockProvider.lastTriggerData["custom_field"])
}

func TestAutomationRuleHandler_TriggerAutomationRule_RuleNotFound(t *testing.T) {
	mockService := new(MockAutomationRuleService)

	mockService.On("GetRuleByID", mock.Anything, "user-1", "rule-unknown").Return(nil, domain.ErrBoostNotFound)

	manager := workers.NewBoostSchedulerManager(nil)
	handler := NewBoostHandler(mockService, manager)

	input := &TriggerBoostInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-1"},
		ID:                 "rule-unknown",
	}

	_, err := handler.TriggerBoost(context.Background(), input)

	assert.Error(t, err)
}

func TestAutomationRuleHandler_TriggerAutomationRule_ManagerNotStarted(t *testing.T) {
	mockService := new(MockAutomationRuleService)

	rule := createMockRule("rule-1", "user-1")
	mockService.On("GetRuleByID", mock.Anything, "user-1", "rule-1").Return(rule, nil)

	// Manager not started
	manager := workers.NewBoostSchedulerManager(nil)
	handler := NewBoostHandler(mockService, manager)

	input := &TriggerBoostInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-1"},
		ID:                 "rule-1",
	}

	_, err := handler.TriggerBoost(context.Background(), input)

	assert.Error(t, err)
}

func TestAutomationRuleHandler_TriggerAutomationRule_NoSchedulerManager(t *testing.T) {
	mockService := new(MockAutomationRuleService)

	rule := createMockRule("rule-1", "user-1")
	mockService.On("GetRuleByID", mock.Anything, "user-1", "rule-1").Return(rule, nil)

	// No scheduler manager
	handler := NewBoostHandler(mockService, nil)

	input := &TriggerBoostInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-1"},
		ID:                 "rule-1",
	}

	_, err := handler.TriggerBoost(context.Background(), input)

	assert.Error(t, err)
}

func TestAutomationRuleHandler_ToggleAutomationRule(t *testing.T) {
	mockService := new(MockAutomationRuleService)

	rule := createMockRule("rule-1", "user-1")
	rule.Enabled = false

	mockService.On("ToggleRule", mock.Anything, "user-1", "rule-1", true).Return(rule, nil)

	handler := NewBoostHandler(mockService, nil)

	input := &ToggleBoostInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-1"},
		ID:                 "rule-1",
	}
	input.Body.Enabled = true

	result, err := handler.ToggleBoost(context.Background(), input)

	require.NoError(t, err)
	assert.Equal(t, "rule-1", result.Body.ID)
	mockService.AssertExpectations(t)
}

func TestAutomationRuleHandler_DeleteAutomationRule(t *testing.T) {
	mockService := new(MockAutomationRuleService)

	mockService.On("DeleteRule", mock.Anything, "user-1", "rule-1").Return(nil)

	handler := NewBoostHandler(mockService, nil)

	input := &DeleteBoostInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-1"},
		ID:                 "rule-1",
	}

	_, err := handler.DeleteBoost(context.Background(), input)

	require.NoError(t, err)
	mockService.AssertExpectations(t)
}

// ============================================================================
// Mock Manual Provider for Testing
// ============================================================================

type mockManualProvider struct {
	execution       *domain.BoostExecution
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

func (m *mockManualProvider) TriggerRule(ctx context.Context, ruleID string, triggerData map[string]interface{}) (*domain.BoostExecution, error) {
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

	testResult := &domain.BoostTestResult{
		BoostID:        "rule-1",
		BoostName:      rule.Name,
		BoostType:      rule.BoostType,
		ConditionsMet: true,
		ConditionResults: []domain.ConditionEvalResult{
			{Field: "amount", Operator: "gt", Value: "50", Actual: float64(100), Met: true},
		},
		SimulatedAction: map[string]interface{}{"action_type": "budget_alert"},
		TriggerDataUsed: map[string]interface{}{"amount": float64(100)},
	}
	mockExecService.On("TestBoost", mock.Anything, rule, mock.Anything).Return(testResult, nil)

	handler := NewBoostHandler(mockRuleService, nil)
	handler.SetExecutionService(mockExecService)

	input := &TestBoostInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-1"},
		ID:                 "rule-1",
	}
	input.Body.TriggerData = map[string]interface{}{"amount": float64(100)}

	result, err := handler.TestBoost(context.Background(), input)

	require.NoError(t, err)
	assert.Equal(t, "rule-1", result.Body.BoostID)
	assert.True(t, result.Body.ConditionsMet)
	assert.Len(t, result.Body.ConditionResults, 1)
	mockRuleService.AssertExpectations(t)
	mockExecService.AssertExpectations(t)
}

func TestAutomationRuleHandler_TestAutomationRule_RuleNotFound(t *testing.T) {
	mockRuleService := new(MockAutomationRuleService)
	mockExecService := new(MockRuleExecutionService)

	mockRuleService.On("GetRuleByID", mock.Anything, "user-1", "rule-unknown").Return(nil, domain.ErrBoostNotFound)

	handler := NewBoostHandler(mockRuleService, nil)
	handler.SetExecutionService(mockExecService)

	input := &TestBoostInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-1"},
		ID:                 "rule-unknown",
	}

	_, err := handler.TestBoost(context.Background(), input)

	assert.Error(t, err)
}

func TestAutomationRuleHandler_TestAutomationRule_NoExecutionService(t *testing.T) {
	mockRuleService := new(MockAutomationRuleService)

	rule := createMockRule("rule-1", "user-1")
	mockRuleService.On("GetRuleByID", mock.Anything, "user-1", "rule-1").Return(rule, nil)

	handler := NewBoostHandler(mockRuleService, nil)
	// No execution service set

	input := &TestBoostInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-1"},
		ID:                 "rule-1",
	}

	_, err := handler.TestBoost(context.Background(), input)

	assert.Error(t, err)
}

func TestAutomationRuleHandler_TestAutomationRule_PFCDisplayFieldsPassThrough(t *testing.T) {
	mockRuleService := new(MockAutomationRuleService)
	mockExecService := new(MockRuleExecutionService)

	rule := createMockRule("rule-1", "user-1")
	mockRuleService.On("GetRuleByID", mock.Anything, "user-1", "rule-1").Return(rule, nil)

	testResult := &domain.BoostTestResult{
		BoostID:        "rule-1",
		BoostName:      rule.Name,
		BoostType:      rule.BoostType,
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
	mockExecService.On("TestBoost", mock.Anything, rule, mock.Anything).Return(testResult, nil)

	handler := NewBoostHandler(mockRuleService, nil)
	handler.SetExecutionService(mockExecService)

	input := &TestBoostInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-1"},
		ID:                 "rule-1",
	}
	input.Body.TriggerData = map[string]interface{}{"transaction.pfc_primary": "FOOD_AND_DRINK"}

	result, err := handler.TestBoost(context.Background(), input)

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

	handler := NewBoostHandler(mockService, nil)

	input := &EnableBoostInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-1"},
		ID:                 "rule-1",
	}

	result, err := handler.EnableBoost(context.Background(), input)

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

	handler := NewBoostHandler(mockService, nil)

	input := &DisableBoostInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-1"},
		ID:                 "rule-1",
	}

	result, err := handler.DisableBoost(context.Background(), input)

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

	executions := []*domain.BoostExecution{
		{ID: "exec-1", BoostID: "rule-1", Status: domain.ExecutionStatusCompleted},
		{ID: "exec-2", BoostID: "rule-1", Status: domain.ExecutionStatusCompleted},
	}
	mockExecService.On("GetExecutionHistoryWithFilter", mock.Anything, "rule-1", (*domain.ExecutionStatus)(nil), 20, 0).Return(executions, 2, nil)

	handler := NewBoostHandler(mockRuleService, nil)
	handler.SetExecutionService(mockExecService)

	input := &GetBoostExecutionsInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-1"},
		ID:                 "rule-1",
		Limit:              20,
		Offset:             0,
	}

	result, err := handler.GetBoostExecutions(context.Background(), input)

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
	executions := []*domain.BoostExecution{
		{ID: "exec-1", BoostID: "rule-1", Status: domain.ExecutionStatusCompleted},
	}
	mockExecService.On("GetExecutionHistoryWithFilter", mock.Anything, "rule-1", &completedStatus, 20, 0).Return(executions, 1, nil)

	handler := NewBoostHandler(mockRuleService, nil)
	handler.SetExecutionService(mockExecService)

	input := &GetBoostExecutionsInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-1"},
		ID:                 "rule-1",
		Status:             "completed",
		Limit:              20,
		Offset:             0,
	}

	result, err := handler.GetBoostExecutions(context.Background(), input)

	require.NoError(t, err)
	assert.Len(t, result.Body.Executions, 1)
	assert.Equal(t, 1, result.Body.Total)
}

func TestAutomationRuleHandler_GetRuleExecutions_RuleNotFound(t *testing.T) {
	mockRuleService := new(MockAutomationRuleService)
	mockExecService := new(MockRuleExecutionService)

	mockRuleService.On("GetRuleByID", mock.Anything, "user-1", "rule-unknown").Return(nil, domain.ErrBoostNotFound)

	handler := NewBoostHandler(mockRuleService, nil)
	handler.SetExecutionService(mockExecService)

	input := &GetBoostExecutionsInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-1"},
		ID:                 "rule-unknown",
		Limit:              20,
		Offset:             0,
	}

	_, err := handler.GetBoostExecutions(context.Background(), input)

	assert.Error(t, err)
}
