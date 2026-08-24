// Package operations provides HTTP handlers for the BoostedBudget API.
// Issue #37: Rule Scheduler - Automation rule operations tests.
package operations

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/danielgtaylor/huma/v2"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/huma/inputs"
	"github.com/maxnussbaum/BoostedBudget/internal/workers"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// MockBoostService implements services.BoostService for testing.
type MockBoostService struct {
	mock.Mock
}

func (m *MockBoostService) CreateBoost(ctx context.Context, userID string, req *domain.CreateBoostRequest) (*domain.Boost, error) {
	args := m.Called(ctx, userID, req)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*domain.Boost), args.Error(1)
}

func (m *MockBoostService) GetUserBoosts(ctx context.Context, userID string) ([]*domain.Boost, error) {
	args := m.Called(ctx, userID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]*domain.Boost), args.Error(1)
}

func (m *MockBoostService) GetBoostByID(ctx context.Context, userID, boostID string) (*domain.Boost, error) {
	args := m.Called(ctx, userID, boostID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*domain.Boost), args.Error(1)
}

func (m *MockBoostService) UpdateBoost(ctx context.Context, userID, boostID string, req *domain.UpdateBoostRequest) (*domain.Boost, error) {
	args := m.Called(ctx, userID, boostID, req)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*domain.Boost), args.Error(1)
}

func (m *MockBoostService) DeleteBoost(ctx context.Context, userID, boostID string) error {
	args := m.Called(ctx, userID, boostID)
	return args.Error(0)
}

func (m *MockBoostService) ToggleBoost(ctx context.Context, userID, boostID string, enabled bool) (*domain.Boost, error) {
	args := m.Called(ctx, userID, boostID, enabled)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*domain.Boost), args.Error(1)
}

func (m *MockBoostService) GetEnabledBoosts(ctx context.Context, userID string) ([]*domain.Boost, error) {
	args := m.Called(ctx, userID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]*domain.Boost), args.Error(1)
}

func (m *MockBoostService) GetUserBoostsFiltered(ctx context.Context, userID string, boostTypes []domain.BoostType) ([]*domain.Boost, error) {
	args := m.Called(ctx, userID, boostTypes)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]*domain.Boost), args.Error(1)
}

func (m *MockBoostService) GetBoostsByType(ctx context.Context, userID string, boostType domain.BoostType) ([]*domain.Boost, error) {
	args := m.Called(ctx, userID, boostType)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]*domain.Boost), args.Error(1)
}

// MockBoostExecutionService implements services.BoostExecutionService for testing.
// Issue #38: Rules API Endpoints.
type MockBoostExecutionService struct {
	mock.Mock
}

func (m *MockBoostExecutionService) ExecuteBoost(ctx context.Context, boost *domain.Boost, triggerData map[string]interface{}) (*domain.BoostExecution, error) {
	args := m.Called(ctx, boost, triggerData)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*domain.BoostExecution), args.Error(1)
}

func (m *MockBoostExecutionService) ProcessEventTrigger(ctx context.Context, userID, eventType string, eventData map[string]interface{}) error {
	args := m.Called(ctx, userID, eventType, eventData)
	return args.Error(0)
}

func (m *MockBoostExecutionService) GetBoostExecutionHistory(ctx context.Context, boostID string, limit, offset int) ([]*domain.BoostExecution, error) {
	args := m.Called(ctx, boostID, limit, offset)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]*domain.BoostExecution), args.Error(1)
}

func (m *MockBoostExecutionService) GetExecutionHistoryWithFilter(ctx context.Context, boostID string, status *domain.ExecutionStatus, limit, offset int) ([]*domain.BoostExecution, int, error) {
	args := m.Called(ctx, boostID, status, limit, offset)
	if args.Get(0) == nil {
		return nil, args.Int(1), args.Error(2)
	}
	return args.Get(0).([]*domain.BoostExecution), args.Int(1), args.Error(2)
}

func (m *MockBoostExecutionService) TestBoost(ctx context.Context, boost *domain.Boost, triggerData map[string]interface{}) (*domain.BoostTestResult, error) {
	args := m.Called(ctx, boost, triggerData)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*domain.BoostTestResult), args.Error(1)
}

func createMockBoost(id, userID string) *domain.Boost {
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

func TestBoostHandler_GetBoosts(t *testing.T) {
	mockService := new(MockBoostService)

	boosts := []*domain.Boost{
		createMockBoost("rule-1", "user-1"),
		createMockBoost("rule-2", "user-1"),
	}

	// No boost_type filter → empty slice passed to GetUserBoostsFiltered.
	mockService.On("GetUserBoostsFiltered", mock.Anything, "user-1", []domain.BoostType(nil)).Return(boosts, nil)

	handler := NewBoostHandler(mockService, nil)

	input := &GetBoostsInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-1"},
	}

	result, err := handler.GetBoosts(context.Background(), input)

	require.NoError(t, err)
	assert.Len(t, result.Body, 2)
	mockService.AssertExpectations(t)
}

func TestBoostHandler_GetBoosts_Empty(t *testing.T) {
	mockService := new(MockBoostService)

	// Return nil to test empty handling.
	mockService.On("GetUserBoostsFiltered", mock.Anything, "user-1", []domain.BoostType(nil)).Return(nil, nil)

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
	mockService := new(MockBoostService)

	filtered := []*domain.Boost{
		{ID: "r1", UserID: "user-1", BoostType: domain.BoostTypeTransferSuggestion},
		{ID: "r2", UserID: "user-1", BoostType: domain.BoostTypeSavingsGoal},
	}

	expectedTypes := []domain.BoostType{domain.BoostTypeTransferSuggestion, domain.BoostTypeSavingsGoal}
	mockService.On("GetUserBoostsFiltered", mock.Anything, "user-1", expectedTypes).Return(filtered, nil)

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
	mockService := new(MockBoostService)
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

func TestBoostHandler_GetBoostByID(t *testing.T) {
	mockService := new(MockBoostService)

	boost := createMockBoost("rule-1", "user-1")
	mockService.On("GetBoostByID", mock.Anything, "user-1", "rule-1").Return(boost, nil)

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

func TestBoostHandler_GetBoostByID_NotFound(t *testing.T) {
	mockService := new(MockBoostService)

	mockService.On("GetBoostByID", mock.Anything, "user-1", "rule-unknown").Return(nil, domain.ErrBoostNotFound)

	handler := NewBoostHandler(mockService, nil)

	input := &GetBoostByIDInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-1"},
		ID:                 "rule-unknown",
	}

	_, err := handler.GetBoostByID(context.Background(), input)

	assert.Error(t, err)
}

func TestBoostHandler_TriggerBoost_Success(t *testing.T) {
	mockService := new(MockBoostService)

	boost := createMockBoost("rule-1", "user-1")
	mockService.On("GetBoostByID", mock.Anything, "user-1", "rule-1").Return(boost, nil)

	// Create a manager with a mock manual provider
	manager := workers.NewBoostSchedulerManager(nil)
	mockProvider := &mockManualProvider{
		execution: &domain.BoostExecution{
			ID:     "exec-1",
			BoostID: strPtr("rule-1"),
			Status: domain.ExecutionStatusCompleted,
		},
	}
	manager.RegisterProvider(mockProvider)
	_ = manager.Start(context.Background())
	defer func() { _ = manager.Stop(context.Background()) }()

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

func TestBoostHandler_TriggerBoost_WithTriggerData(t *testing.T) {
	mockService := new(MockBoostService)

	boost := createMockBoost("rule-1", "user-1")
	mockService.On("GetBoostByID", mock.Anything, "user-1", "rule-1").Return(boost, nil)

	manager := workers.NewBoostSchedulerManager(nil)
	mockProvider := &mockManualProvider{
		execution: &domain.BoostExecution{
			ID:     "exec-1",
			BoostID: strPtr("rule-1"),
			Status: domain.ExecutionStatusCompleted,
		},
	}
	manager.RegisterProvider(mockProvider)
	_ = manager.Start(context.Background())
	defer func() { _ = manager.Stop(context.Background()) }()

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

func TestBoostHandler_TriggerBoost_BoostNotFound(t *testing.T) {
	mockService := new(MockBoostService)

	mockService.On("GetBoostByID", mock.Anything, "user-1", "rule-unknown").Return(nil, domain.ErrBoostNotFound)

	manager := workers.NewBoostSchedulerManager(nil)
	handler := NewBoostHandler(mockService, manager)

	input := &TriggerBoostInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-1"},
		ID:                 "rule-unknown",
	}

	_, err := handler.TriggerBoost(context.Background(), input)

	assert.Error(t, err)
}

func TestBoostHandler_TriggerBoost_ManagerNotStarted(t *testing.T) {
	mockService := new(MockBoostService)

	boost := createMockBoost("rule-1", "user-1")
	mockService.On("GetBoostByID", mock.Anything, "user-1", "rule-1").Return(boost, nil)

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

func TestBoostHandler_TriggerBoost_NoSchedulerManager(t *testing.T) {
	mockService := new(MockBoostService)

	boost := createMockBoost("rule-1", "user-1")
	mockService.On("GetBoostByID", mock.Anything, "user-1", "rule-1").Return(boost, nil)

	// No scheduler manager
	handler := NewBoostHandler(mockService, nil)

	input := &TriggerBoostInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-1"},
		ID:                 "rule-1",
	}

	_, err := handler.TriggerBoost(context.Background(), input)

	assert.Error(t, err)
}

func TestBoostHandler_ToggleBoost(t *testing.T) {
	mockService := new(MockBoostService)

	boost := createMockBoost("rule-1", "user-1")
	boost.Enabled = false

	mockService.On("ToggleBoost", mock.Anything, "user-1", "rule-1", true).Return(boost, nil)

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

func TestBoostHandler_DeleteBoost(t *testing.T) {
	mockService := new(MockBoostService)

	mockService.On("DeleteBoost", mock.Anything, "user-1", "rule-1").Return(nil)

	handler := NewBoostHandler(mockService, nil)

	input := &DeleteBoostInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-1"},
		ID:                 "rule-1",
	}

	_, err := handler.DeleteBoost(context.Background(), input)

	require.NoError(t, err)
	mockService.AssertExpectations(t)
}

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

func (m *mockManualProvider) TriggerBoost(ctx context.Context, boostID string, triggerData map[string]interface{}) (*domain.BoostExecution, error) {
	m.lastTriggerData = triggerData
	return m.execution, nil
}

func TestBoostHandler_TestBoostExecution_Success(t *testing.T) {
	mockBoostService := new(MockBoostService)
	mockExecService := new(MockBoostExecutionService)

	boost := createMockBoost("rule-1", "user-1")
	mockBoostService.On("GetBoostByID", mock.Anything, "user-1", "rule-1").Return(boost, nil)

	testResult := &domain.BoostTestResult{
		BoostID:        "rule-1",
		BoostName:      boost.Name,
		BoostType:      boost.BoostType,
		ConditionsMet: true,
		ConditionResults: []domain.ConditionEvalResult{
			{Field: "amount", Operator: "gt", Value: "50", Actual: float64(100), Met: true},
		},
		SimulatedAction: map[string]interface{}{"action_type": "budget_alert"},
		TriggerDataUsed: map[string]interface{}{"amount": float64(100)},
	}
	mockExecService.On("TestBoost", mock.Anything, boost, mock.Anything).Return(testResult, nil)

	handler := NewBoostHandler(mockBoostService, nil)
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
	mockBoostService.AssertExpectations(t)
	mockExecService.AssertExpectations(t)
}

func TestBoostHandler_TestBoostExecution_BoostNotFound(t *testing.T) {
	mockBoostService := new(MockBoostService)
	mockExecService := new(MockBoostExecutionService)

	mockBoostService.On("GetBoostByID", mock.Anything, "user-1", "rule-unknown").Return(nil, domain.ErrBoostNotFound)

	handler := NewBoostHandler(mockBoostService, nil)
	handler.SetExecutionService(mockExecService)

	input := &TestBoostInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-1"},
		ID:                 "rule-unknown",
	}

	_, err := handler.TestBoost(context.Background(), input)

	assert.Error(t, err)
}

func TestBoostHandler_TestBoostExecution_NoExecutionService(t *testing.T) {
	mockBoostService := new(MockBoostService)

	boost := createMockBoost("rule-1", "user-1")
	mockBoostService.On("GetBoostByID", mock.Anything, "user-1", "rule-1").Return(boost, nil)

	handler := NewBoostHandler(mockBoostService, nil)
	// No execution service set

	input := &TestBoostInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-1"},
		ID:                 "rule-1",
	}

	_, err := handler.TestBoost(context.Background(), input)

	assert.Error(t, err)
}

func TestBoostHandler_TestBoostExecution_PFCDisplayFieldsPassThrough(t *testing.T) {
	mockBoostService := new(MockBoostService)
	mockExecService := new(MockBoostExecutionService)

	boost := createMockBoost("rule-1", "user-1")
	mockBoostService.On("GetBoostByID", mock.Anything, "user-1", "rule-1").Return(boost, nil)

	testResult := &domain.BoostTestResult{
		BoostID:        "rule-1",
		BoostName:      boost.Name,
		BoostType:      boost.BoostType,
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
	mockExecService.On("TestBoost", mock.Anything, boost, mock.Anything).Return(testResult, nil)

	handler := NewBoostHandler(mockBoostService, nil)
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
	mockBoostService.AssertExpectations(t)
	mockExecService.AssertExpectations(t)
}

func TestBoostHandler_EnableBoost(t *testing.T) {
	mockService := new(MockBoostService)

	boost := createMockBoost("rule-1", "user-1")
	boost.Enabled = true

	mockService.On("ToggleBoost", mock.Anything, "user-1", "rule-1", true).Return(boost, nil)

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

func TestBoostHandler_DisableBoost(t *testing.T) {
	mockService := new(MockBoostService)

	boost := createMockBoost("rule-1", "user-1")
	boost.Enabled = false

	mockService.On("ToggleBoost", mock.Anything, "user-1", "rule-1", false).Return(boost, nil)

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

func TestBoostHandler_GetBoostExecutions_Success(t *testing.T) {
	mockBoostService := new(MockBoostService)
	mockExecService := new(MockBoostExecutionService)

	boost := createMockBoost("rule-1", "user-1")
	mockBoostService.On("GetBoostByID", mock.Anything, "user-1", "rule-1").Return(boost, nil)

	executions := []*domain.BoostExecution{
		{ID: "exec-1", BoostID: strPtr("rule-1"), Status: domain.ExecutionStatusCompleted},
		{ID: "exec-2", BoostID: strPtr("rule-1"), Status: domain.ExecutionStatusCompleted},
	}
	mockExecService.On("GetExecutionHistoryWithFilter", mock.Anything, "rule-1", (*domain.ExecutionStatus)(nil), 20, 0).Return(executions, 2, nil)

	handler := NewBoostHandler(mockBoostService, nil)
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
	mockBoostService.AssertExpectations(t)
	mockExecService.AssertExpectations(t)
}

func TestBoostHandler_GetBoostExecutions_WithStatusFilter(t *testing.T) {
	mockBoostService := new(MockBoostService)
	mockExecService := new(MockBoostExecutionService)

	boost := createMockBoost("rule-1", "user-1")
	mockBoostService.On("GetBoostByID", mock.Anything, "user-1", "rule-1").Return(boost, nil)

	completedStatus := domain.ExecutionStatusCompleted
	executions := []*domain.BoostExecution{
		{ID: "exec-1", BoostID: strPtr("rule-1"), Status: domain.ExecutionStatusCompleted},
	}
	mockExecService.On("GetExecutionHistoryWithFilter", mock.Anything, "rule-1", &completedStatus, 20, 0).Return(executions, 1, nil)

	handler := NewBoostHandler(mockBoostService, nil)
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

func TestBoostHandler_GetBoostExecutions_BoostNotFound(t *testing.T) {
	mockBoostService := new(MockBoostService)
	mockExecService := new(MockBoostExecutionService)

	mockBoostService.On("GetBoostByID", mock.Anything, "user-1", "rule-unknown").Return(nil, domain.ErrBoostNotFound)

	handler := NewBoostHandler(mockBoostService, nil)
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

// --- CreateBoost tests ---

func TestBoostHandler_CreateBoost_Success(t *testing.T) {
	mockSvc := new(MockBoostService)
	handler := NewBoostHandler(mockSvc, nil)

	req := domain.CreateBoostRequest{Name: "Test Boost"}
	expected := createMockBoost("boost-1", "user-123")

	mockSvc.On("CreateBoost", mock.Anything, "user-123", &req).Return(expected, nil)

	input := &CreateBoostInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-123"},
		Body:               req,
	}
	output, err := handler.CreateBoost(context.Background(), input)

	require.NoError(t, err)
	require.NotNil(t, output)
	assert.Equal(t, expected, output.Body)
	mockSvc.AssertExpectations(t)
}

func TestBoostHandler_CreateBoost_ServiceError(t *testing.T) {
	mockSvc := new(MockBoostService)
	handler := NewBoostHandler(mockSvc, nil)

	req := domain.CreateBoostRequest{Name: "Bad Boost"}
	mockSvc.On("CreateBoost", mock.Anything, "user-123", &req).Return(nil, errors.New("service failed"))

	input := &CreateBoostInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-123"},
		Body:               req,
	}
	output, err := handler.CreateBoost(context.Background(), input)

	assert.Nil(t, output)
	require.Error(t, err)
	mockSvc.AssertExpectations(t)
}

// --- UpdateBoost tests ---

func TestBoostHandler_UpdateBoost_Success(t *testing.T) {
	mockSvc := new(MockBoostService)
	handler := NewBoostHandler(mockSvc, nil)

	req := domain.UpdateBoostRequest{Name: stringPtr("Updated Name")}
	expected := createMockBoost("boost-1", "user-123")
	expected.Name = "Updated Name"

	mockSvc.On("UpdateBoost", mock.Anything, "user-123", "boost-1", &req).Return(expected, nil)

	input := &UpdateBoostInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-123"},
		ID:                 "boost-1",
		Body:               req,
	}
	output, err := handler.UpdateBoost(context.Background(), input)

	require.NoError(t, err)
	require.NotNil(t, output)
	assert.Equal(t, expected, output.Body)
	mockSvc.AssertExpectations(t)
}

func TestBoostHandler_UpdateBoost_ServiceError(t *testing.T) {
	mockSvc := new(MockBoostService)
	handler := NewBoostHandler(mockSvc, nil)

	req := domain.UpdateBoostRequest{Name: stringPtr("Updated Name")}
	mockSvc.On("UpdateBoost", mock.Anything, "user-123", "boost-1", &req).Return(nil, errors.New("not found"))

	input := &UpdateBoostInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-123"},
		ID:                 "boost-1",
		Body:               req,
	}
	output, err := handler.UpdateBoost(context.Background(), input)

	assert.Nil(t, output)
	require.Error(t, err)
	mockSvc.AssertExpectations(t)
}

func stringPtr(s string) *string { return &s }
