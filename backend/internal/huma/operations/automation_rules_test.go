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

func (m *MockAutomationRuleService) TierAllowsEventTriggers(ctx context.Context, userID string) (bool, error) {
	args := m.Called(ctx, userID)
	return args.Bool(0), args.Error(1)
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
	manager := workers.NewRuleSchedulerManager()
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

	manager := workers.NewRuleSchedulerManager()
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

	manager := workers.NewRuleSchedulerManager()
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
	manager := workers.NewRuleSchedulerManager()
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
