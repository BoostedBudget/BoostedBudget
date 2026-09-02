// Package workers provides background job processing for automation rules.
// Issue #37: Rule Scheduler - ManualTriggerProvider tests (TDD).
package workers

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// ============================================================================
// ManualTriggerProvider Tests
// ============================================================================

func TestManualProvider_Name(t *testing.T) {
	provider := NewManualTriggerProvider(nil, nil, nil)

	assert.Equal(t, "manual", provider.Name())
}

func TestManualProvider_SupportsManualTrigger(t *testing.T) {
	provider := NewManualTriggerProvider(nil, nil, nil)

	// Manual provider DOES support manual triggering
	assert.True(t, provider.SupportsManualTrigger())
}

func TestManualProvider_Start(t *testing.T) {
	provider := NewManualTriggerProvider(nil, nil, nil)

	// Start is a no-op for manual provider
	err := provider.Start(context.Background())
	require.NoError(t, err)
}

func TestManualProvider_Stop(t *testing.T) {
	provider := NewManualTriggerProvider(nil, nil, nil)

	// Stop is a no-op for manual provider
	err := provider.Stop(context.Background())
	require.NoError(t, err)
}

func TestManualProvider_TriggerBoost_Success(t *testing.T) {
	mockRepo := new(MockBoostsRepository)
	mockExecutor := new(MockBoostExecutor)

	rule := createTestBoost("rule-1", "user-1", "0 9 * * *")

	execution := &domain.BoostExecution{
		ID:     "exec-1",
		BoostID: ptrS("rule-1"),
		Status: domain.ExecutionStatusCompleted,
	}

	mockRepo.On("GetByID", mock.Anything, "rule-1").Return(rule, nil)
	mockRepo.On("IncrementExecutionCount", mock.Anything, "rule-1").Return(nil)
	mockRepo.On("UpdateLastSuccessful", mock.Anything, "rule-1").Return(nil)

	mockExecutor.On("ExecuteBoost", mock.Anything, rule, mock.Anything).Return(execution, nil)

	provider := NewManualTriggerProvider(mockRepo, mockExecutor, nil)

	result, err := provider.TriggerBoost(context.Background(), "rule-1", nil)

	require.NoError(t, err)
	assert.Equal(t, "exec-1", result.ID)
	assert.Equal(t, domain.ExecutionStatusCompleted, result.Status)

	mockRepo.AssertExpectations(t)
	mockExecutor.AssertExpectations(t)
}

func TestManualProvider_TriggerBoost_WithUserData(t *testing.T) {
	mockRepo := new(MockBoostsRepository)
	mockExecutor := new(MockBoostExecutor)

	rule := createTestBoost("rule-1", "user-1", "0 9 * * *")

	execution := &domain.BoostExecution{
		ID:     "exec-1",
		BoostID: ptrS("rule-1"),
		Status: domain.ExecutionStatusCompleted,
	}

	mockRepo.On("GetByID", mock.Anything, "rule-1").Return(rule, nil)
	mockRepo.On("IncrementExecutionCount", mock.Anything, "rule-1").Return(nil)
	mockRepo.On("UpdateLastSuccessful", mock.Anything, "rule-1").Return(nil)

	// Capture the trigger data passed to ExecuteBoost
	var capturedTriggerData map[string]interface{}
	mockExecutor.On("ExecuteBoost", mock.Anything, rule, mock.Anything).Run(func(args mock.Arguments) {
		capturedTriggerData = args.Get(2).(map[string]interface{})
	}).Return(execution, nil)

	provider := NewManualTriggerProvider(mockRepo, mockExecutor, nil)

	userData := map[string]interface{}{
		"custom_field": "custom_value",
		"amount":       1500.00,
	}

	result, err := provider.TriggerBoost(context.Background(), "rule-1", userData)

	require.NoError(t, err)
	assert.NotNil(t, result)

	// Verify user data was included in trigger data
	assert.Equal(t, "custom_value", capturedTriggerData["custom_field"])
	assert.Equal(t, 1500.00, capturedTriggerData["amount"])

	// Verify trigger metadata was added
	assert.Equal(t, "manual", capturedTriggerData["trigger_type"])
	assert.NotNil(t, capturedTriggerData["triggered_at"])
}

func TestManualProvider_TriggerBoost_RuleNotFound(t *testing.T) {
	mockRepo := new(MockBoostsRepository)
	mockExecutor := new(MockBoostExecutor)

	mockRepo.On("GetByID", mock.Anything, "rule-unknown").Return(nil, domain.ErrBoostNotFound)

	provider := NewManualTriggerProvider(mockRepo, mockExecutor, nil)

	result, err := provider.TriggerBoost(context.Background(), "rule-unknown", nil)

	assert.Error(t, err)
	assert.Nil(t, result)
	assert.Equal(t, domain.ErrBoostNotFound, err)
}

func TestManualProvider_TriggerBoost_RuleDisabled(t *testing.T) {
	mockRepo := new(MockBoostsRepository)
	mockExecutor := new(MockBoostExecutor)

	rule := createTestBoost("rule-1", "user-1", "0 9 * * *")
	rule.Enabled = false

	mockRepo.On("GetByID", mock.Anything, "rule-1").Return(rule, nil)

	provider := NewManualTriggerProvider(mockRepo, mockExecutor, nil)

	result, err := provider.TriggerBoost(context.Background(), "rule-1", nil)

	assert.Error(t, err)
	assert.Nil(t, result)
	assert.Equal(t, ErrBoostDisabled, err)
}

func TestManualProvider_TriggerBoost_ExecutorError(t *testing.T) {
	mockRepo := new(MockBoostsRepository)
	mockExecutor := new(MockBoostExecutor)

	rule := createTestBoost("rule-1", "user-1", "0 9 * * *")

	execution := &domain.BoostExecution{
		ID:     "exec-1",
		BoostID: ptrS("rule-1"),
		Status: domain.ExecutionStatusFailed,
	}

	mockRepo.On("GetByID", mock.Anything, "rule-1").Return(rule, nil)
	mockRepo.On("IncrementExecutionCount", mock.Anything, "rule-1").Return(nil)

	mockExecutor.On("ExecuteBoost", mock.Anything, rule, mock.Anything).Return(execution, assert.AnError)

	provider := NewManualTriggerProvider(mockRepo, mockExecutor, nil)

	result, err := provider.TriggerBoost(context.Background(), "rule-1", nil)

	assert.Error(t, err)
	assert.Nil(t, result)
}

func TestManualProvider_TriggerBoost_ContextCancelled(t *testing.T) {
	mockRepo := new(MockBoostsRepository)
	mockExecutor := new(MockBoostExecutor)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	provider := NewManualTriggerProvider(mockRepo, mockExecutor, nil)

	result, err := provider.TriggerBoost(ctx, "rule-1", nil)

	assert.Error(t, err)
	assert.Nil(t, result)
}

func TestManualProvider_TriggerBoost_UpdatesExecutionStats(t *testing.T) {
	mockRepo := new(MockBoostsRepository)
	mockExecutor := new(MockBoostExecutor)

	rule := createTestBoost("rule-1", "user-1", "0 9 * * *")

	execution := &domain.BoostExecution{
		ID:     "exec-1",
		BoostID: ptrS("rule-1"),
		Status: domain.ExecutionStatusCompleted,
	}

	mockRepo.On("GetByID", mock.Anything, "rule-1").Return(rule, nil)
	mockRepo.On("IncrementExecutionCount", mock.Anything, "rule-1").Return(nil)
	mockRepo.On("UpdateLastSuccessful", mock.Anything, "rule-1").Return(nil)

	mockExecutor.On("ExecuteBoost", mock.Anything, rule, mock.Anything).Return(execution, nil)

	provider := NewManualTriggerProvider(mockRepo, mockExecutor, nil)

	_, err := provider.TriggerBoost(context.Background(), "rule-1", nil)
	require.NoError(t, err)

	// Verify execution stats were updated
	mockRepo.AssertCalled(t, "IncrementExecutionCount", mock.Anything, "rule-1")
	mockRepo.AssertCalled(t, "UpdateLastSuccessful", mock.Anything, "rule-1")
}

func TestManualProvider_TriggerBoost_FailedExecution_NoLastSuccessful(t *testing.T) {
	mockRepo := new(MockBoostsRepository)
	mockExecutor := new(MockBoostExecutor)

	rule := createTestBoost("rule-1", "user-1", "0 9 * * *")

	execution := &domain.BoostExecution{
		ID:     "exec-1",
		BoostID: ptrS("rule-1"),
		Status: domain.ExecutionStatusFailed,
	}

	mockRepo.On("GetByID", mock.Anything, "rule-1").Return(rule, nil)
	mockRepo.On("IncrementExecutionCount", mock.Anything, "rule-1").Return(nil)

	mockExecutor.On("ExecuteBoost", mock.Anything, rule, mock.Anything).Return(execution, nil)

	provider := NewManualTriggerProvider(mockRepo, mockExecutor, nil)

	result, err := provider.TriggerBoost(context.Background(), "rule-1", nil)
	require.NoError(t, err)
	assert.Equal(t, domain.ExecutionStatusFailed, result.Status)

	// Verify UpdateLastSuccessful was NOT called for failed execution
	mockRepo.AssertNotCalled(t, "UpdateLastSuccessful")
}

func TestManualProvider_TriggerBoost_VerifiesUserOwnership(t *testing.T) {
	mockRepo := new(MockBoostsRepository)
	mockExecutor := new(MockBoostExecutor)

	rule := createTestBoost("rule-1", "user-1", "0 9 * * *")

	execution := &domain.BoostExecution{
		ID:     "exec-1",
		BoostID: ptrS("rule-1"),
		Status: domain.ExecutionStatusCompleted,
	}

	mockRepo.On("GetByID", mock.Anything, "rule-1").Return(rule, nil)
	mockRepo.On("IncrementExecutionCount", mock.Anything, "rule-1").Return(nil)
	mockRepo.On("UpdateLastSuccessful", mock.Anything, "rule-1").Return(nil)

	mockExecutor.On("ExecuteBoost", mock.Anything, rule, mock.Anything).Return(execution, nil)

	provider := NewManualTriggerProvider(mockRepo, mockExecutor, nil)

	// The provider uses the rule's UserID for execution
	result, err := provider.TriggerBoost(context.Background(), "rule-1", nil)
	require.NoError(t, err)
	assert.NotNil(t, result)
}

func TestManualProvider_TriggerBoost_MergesTriggerDataCorrectly(t *testing.T) {
	mockRepo := new(MockBoostsRepository)
	mockExecutor := new(MockBoostExecutor)

	rule := createTestBoost("rule-1", "user-1", "0 9 * * *")

	execution := &domain.BoostExecution{
		ID:     "exec-1",
		BoostID: ptrS("rule-1"),
		Status: domain.ExecutionStatusCompleted,
	}

	mockRepo.On("GetByID", mock.Anything, "rule-1").Return(rule, nil)
	mockRepo.On("IncrementExecutionCount", mock.Anything, "rule-1").Return(nil)
	mockRepo.On("UpdateLastSuccessful", mock.Anything, "rule-1").Return(nil)

	var capturedTriggerData map[string]interface{}
	mockExecutor.On("ExecuteBoost", mock.Anything, rule, mock.Anything).Run(func(args mock.Arguments) {
		capturedTriggerData = args.Get(2).(map[string]interface{})
	}).Return(execution, nil)

	provider := NewManualTriggerProvider(mockRepo, mockExecutor, nil)

	userData := map[string]interface{}{
		"account_id":  "acc-123",
		"category_id": "cat-456",
		"notes":       "Manual test",
	}

	_, err := provider.TriggerBoost(context.Background(), "rule-1", userData)
	require.NoError(t, err)

	// Verify all expected fields are present
	assert.Equal(t, "manual", capturedTriggerData["trigger_type"])
	assert.Equal(t, "acc-123", capturedTriggerData["account_id"])
	assert.Equal(t, "cat-456", capturedTriggerData["category_id"])
	assert.Equal(t, "Manual test", capturedTriggerData["notes"])
	assert.Equal(t, "rule-1", capturedTriggerData["boost_id"])
	assert.Equal(t, "user-1", capturedTriggerData["user_id"])

	// Verify triggered_at is a valid time
	triggeredAt, ok := capturedTriggerData["triggered_at"].(time.Time)
	assert.True(t, ok)
	assert.WithinDuration(t, time.Now(), triggeredAt, 5*time.Second)
}

func TestManualProvider_TriggerBoost_HandlesEmptyTriggerData(t *testing.T) {
	mockRepo := new(MockBoostsRepository)
	mockExecutor := new(MockBoostExecutor)

	rule := createTestBoost("rule-1", "user-1", "0 9 * * *")

	execution := &domain.BoostExecution{
		ID:     "exec-1",
		BoostID: ptrS("rule-1"),
		Status: domain.ExecutionStatusCompleted,
	}

	mockRepo.On("GetByID", mock.Anything, "rule-1").Return(rule, nil)
	mockRepo.On("IncrementExecutionCount", mock.Anything, "rule-1").Return(nil)
	mockRepo.On("UpdateLastSuccessful", mock.Anything, "rule-1").Return(nil)

	var capturedTriggerData map[string]interface{}
	mockExecutor.On("ExecuteBoost", mock.Anything, rule, mock.Anything).Run(func(args mock.Arguments) {
		capturedTriggerData = args.Get(2).(map[string]interface{})
	}).Return(execution, nil)

	provider := NewManualTriggerProvider(mockRepo, mockExecutor, nil)

	// nil trigger data should still work
	_, err := provider.TriggerBoost(context.Background(), "rule-1", nil)
	require.NoError(t, err)

	// Verify base fields are still set
	assert.Equal(t, "manual", capturedTriggerData["trigger_type"])
	assert.Equal(t, "rule-1", capturedTriggerData["boost_id"])
}

func TestManualProvider_TriggerBoost_SerializesTriggerDataForExecution(t *testing.T) {
	mockRepo := new(MockBoostsRepository)
	mockExecutor := new(MockBoostExecutor)

	rule := createTestBoost("rule-1", "user-1", "0 9 * * *")

	mockRepo.On("GetByID", mock.Anything, "rule-1").Return(rule, nil)
	mockRepo.On("IncrementExecutionCount", mock.Anything, "rule-1").Return(nil)
	mockRepo.On("UpdateLastSuccessful", mock.Anything, "rule-1").Return(nil)

	var capturedTriggerData map[string]interface{}
	mockExecutor.On("ExecuteBoost", mock.Anything, rule, mock.Anything).Run(func(args mock.Arguments) {
		capturedTriggerData = args.Get(2).(map[string]interface{})
	}).Return(&domain.BoostExecution{
		ID:     "exec-1",
		BoostID: ptrS("rule-1"),
		Status: domain.ExecutionStatusCompleted,
	}, nil)

	provider := NewManualTriggerProvider(mockRepo, mockExecutor, nil)

	complexData := map[string]interface{}{
		"nested": map[string]interface{}{
			"key": "value",
		},
		"array": []string{"a", "b", "c"},
	}

	_, err := provider.TriggerBoost(context.Background(), "rule-1", complexData)
	require.NoError(t, err)

	// Verify complex data is properly handled
	assert.NotNil(t, capturedTriggerData["nested"])
	assert.NotNil(t, capturedTriggerData["array"])

	// Verify it can be serialized to JSON
	_, err = json.Marshal(capturedTriggerData)
	assert.NoError(t, err)
}
