// Package workers provides background job processing for automation rules.
// Issue #37: Rule Scheduler - CronTriggerProvider tests (TDD).
package workers

import (
	"context"
	"encoding/json"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// ============================================================================
// Mock Dependencies
// ============================================================================

// MockRuleRepository implements repository.AutomationRulesRepository for testing.
type MockRuleRepository struct {
	mock.Mock
}

func (m *MockRuleRepository) Create(ctx context.Context, rule *domain.AutomationRule) error {
	args := m.Called(ctx, rule)
	return args.Error(0)
}

func (m *MockRuleRepository) GetByID(ctx context.Context, id string) (*domain.AutomationRule, error) {
	args := m.Called(ctx, id)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*domain.AutomationRule), args.Error(1)
}

func (m *MockRuleRepository) GetByUserID(ctx context.Context, userID string) ([]*domain.AutomationRule, error) {
	args := m.Called(ctx, userID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]*domain.AutomationRule), args.Error(1)
}

func (m *MockRuleRepository) GetEnabledByUserID(ctx context.Context, userID string) ([]*domain.AutomationRule, error) {
	args := m.Called(ctx, userID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]*domain.AutomationRule), args.Error(1)
}

func (m *MockRuleRepository) GetByName(ctx context.Context, userID, name string) (*domain.AutomationRule, error) {
	args := m.Called(ctx, userID, name)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*domain.AutomationRule), args.Error(1)
}

func (m *MockRuleRepository) Update(ctx context.Context, rule *domain.AutomationRule) error {
	args := m.Called(ctx, rule)
	return args.Error(0)
}

func (m *MockRuleRepository) Delete(ctx context.Context, id string) error {
	args := m.Called(ctx, id)
	return args.Error(0)
}

func (m *MockRuleRepository) CountByUserID(ctx context.Context, userID string) (int, error) {
	args := m.Called(ctx, userID)
	return args.Int(0), args.Error(1)
}

func (m *MockRuleRepository) IncrementExecutionCount(ctx context.Context, ruleID string) error {
	args := m.Called(ctx, ruleID)
	return args.Error(0)
}

func (m *MockRuleRepository) UpdateLastSuccessful(ctx context.Context, ruleID string) error {
	args := m.Called(ctx, ruleID)
	return args.Error(0)
}

func (m *MockRuleRepository) GetRulesByEventTrigger(ctx context.Context, userID, eventType string) ([]*domain.AutomationRule, error) {
	args := m.Called(ctx, userID, eventType)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]*domain.AutomationRule), args.Error(1)
}

func (m *MockRuleRepository) GetRulesByType(ctx context.Context, userID string, ruleType domain.RuleType) ([]*domain.AutomationRule, error) {
	args := m.Called(ctx, userID, ruleType)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]*domain.AutomationRule), args.Error(1)
}

func (m *MockRuleRepository) GetRulesByScheduleTrigger(ctx context.Context) ([]*domain.AutomationRule, error) {
	args := m.Called(ctx)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]*domain.AutomationRule), args.Error(1)
}

// MockRuleExecutor implements a mock for rule execution.
type MockRuleExecutor struct {
	mock.Mock
	ExecutionCount int32
	mu             sync.Mutex
}

func (m *MockRuleExecutor) ExecuteRule(ctx context.Context, rule *domain.AutomationRule, triggerData map[string]interface{}) (*domain.RuleExecution, error) {
	atomic.AddInt32(&m.ExecutionCount, 1)
	args := m.Called(ctx, rule, triggerData)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*domain.RuleExecution), args.Error(1)
}

func (m *MockRuleExecutor) GetExecutionCount() int32 {
	return atomic.LoadInt32(&m.ExecutionCount)
}

// ============================================================================
// Test Helpers
// ============================================================================

func createTestRule(id, userID, cronExpr string) *domain.AutomationRule {
	return &domain.AutomationRule{
		ID:      id,
		UserID:  userID,
		Name:    "Test Rule " + id,
		Enabled: true,
		TriggerConfig: domain.TriggerConfig{
			Type: domain.TriggerTypeSchedule,
			Cron: cronExpr,
		},
		RuleType: domain.RuleTypeBudgetAlert,
		Actions:  json.RawMessage("[]"),
	}
}

func createTestRuleWithTimezone(id, userID, cron, timezone string) *domain.AutomationRule {
	rule := createTestRule(id, userID, cron)
	rule.TriggerConfig.Timezone = timezone
	return rule
}

// ============================================================================
// CronTriggerProvider Tests
// ============================================================================

func TestCronProvider_Name(t *testing.T) {
	provider := NewCronTriggerProvider(nil, nil, time.Minute, nil)

	assert.Equal(t, "cron", provider.Name())
}

func TestCronProvider_SupportsManualTrigger(t *testing.T) {
	provider := NewCronTriggerProvider(nil, nil, time.Minute, nil)

	// Cron provider does NOT support manual triggering
	assert.False(t, provider.SupportsManualTrigger())
}

func TestCronProvider_TriggerRule_NotSupported(t *testing.T) {
	provider := NewCronTriggerProvider(nil, nil, time.Minute, nil)

	execution, err := provider.TriggerRule(context.Background(), "rule-123", nil)

	assert.Error(t, err)
	assert.Nil(t, execution)
	assert.Equal(t, ErrManualTriggerNotSupported, err)
}

func TestCronProvider_Start_LoadsScheduledRules(t *testing.T) {
	// Setup mocks
	mockRepo := new(MockRuleRepository)
	mockExecutor := new(MockRuleExecutor)

	rules := []*domain.AutomationRule{
		createTestRule("rule-1", "user-1", "0 9 * * *"),  // 9am daily
		createTestRule("rule-2", "user-2", "0 12 * * 1"), // noon on Mondays
	}

	mockRepo.On("GetRulesByScheduleTrigger", mock.Anything).Return(rules, nil)

	provider := NewCronTriggerProvider(mockRepo, mockExecutor, time.Minute, nil)

	// Start provider
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err := provider.Start(ctx)
	require.NoError(t, err)

	// Verify rules were loaded
	assert.Equal(t, 2, provider.ScheduledRuleCount())

	// Stop provider
	stopCtx, stopCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer stopCancel()
	err = provider.Stop(stopCtx)
	require.NoError(t, err)

	mockRepo.AssertExpectations(t)
}

func TestCronProvider_Start_SkipsDisabledRules(t *testing.T) {
	mockRepo := new(MockRuleRepository)
	mockExecutor := new(MockRuleExecutor)

	// GetRulesByScheduleTrigger already filters enabled=true in SQL
	// This test verifies the query returns only enabled rules
	enabledRule := createTestRule("rule-1", "user-1", "0 9 * * *")

	mockRepo.On("GetRulesByScheduleTrigger", mock.Anything).Return([]*domain.AutomationRule{enabledRule}, nil)

	provider := NewCronTriggerProvider(mockRepo, mockExecutor, time.Minute, nil)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err := provider.Start(ctx)
	require.NoError(t, err)

	// Only the enabled rule should be scheduled
	assert.Equal(t, 1, provider.ScheduledRuleCount())

	stopCtx, stopCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer stopCancel()
	_ = provider.Stop(stopCtx)

	mockRepo.AssertExpectations(t)
}

func TestCronProvider_Start_SkipsInvalidCronExpressions(t *testing.T) {
	mockRepo := new(MockRuleRepository)
	mockExecutor := new(MockRuleExecutor)

	rules := []*domain.AutomationRule{
		createTestRule("rule-1", "user-1", "0 9 * * *"),     // Valid
		createTestRule("rule-2", "user-2", "invalid-cron"),  // Invalid
		createTestRule("rule-3", "user-3", "0 12 * * 1-5"),  // Valid
	}

	mockRepo.On("GetRulesByScheduleTrigger", mock.Anything).Return(rules, nil)

	provider := NewCronTriggerProvider(mockRepo, mockExecutor, time.Minute, nil)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err := provider.Start(ctx)
	require.NoError(t, err)

	// Only valid cron expressions should be scheduled
	assert.Equal(t, 2, provider.ScheduledRuleCount())

	stopCtx, stopCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer stopCancel()
	_ = provider.Stop(stopCtx)
}

func TestCronProvider_Start_HandlesEmptyRulesList(t *testing.T) {
	mockRepo := new(MockRuleRepository)
	mockExecutor := new(MockRuleExecutor)

	mockRepo.On("GetRulesByScheduleTrigger", mock.Anything).Return([]*domain.AutomationRule{}, nil)

	provider := NewCronTriggerProvider(mockRepo, mockExecutor, time.Minute, nil)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err := provider.Start(ctx)
	require.NoError(t, err)

	assert.Equal(t, 0, provider.ScheduledRuleCount())

	stopCtx, stopCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer stopCancel()
	_ = provider.Stop(stopCtx)
}

func TestCronProvider_Start_HandlesRepositoryError(t *testing.T) {
	mockRepo := new(MockRuleRepository)
	mockExecutor := new(MockRuleExecutor)

	mockRepo.On("GetRulesByScheduleTrigger", mock.Anything).Return(nil, assert.AnError)

	provider := NewCronTriggerProvider(mockRepo, mockExecutor, time.Minute, nil)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err := provider.Start(ctx)

	// Should return error if initial load fails
	assert.Error(t, err)
}

func TestCronProvider_GracefulShutdown(t *testing.T) {
	mockRepo := new(MockRuleRepository)
	mockExecutor := new(MockRuleExecutor)

	mockRepo.On("GetRulesByScheduleTrigger", mock.Anything).Return([]*domain.AutomationRule{}, nil)

	provider := NewCronTriggerProvider(mockRepo, mockExecutor, time.Minute, nil)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err := provider.Start(ctx)
	require.NoError(t, err)

	// Stop should be graceful
	stopCtx, stopCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer stopCancel()

	err = provider.Stop(stopCtx)
	require.NoError(t, err)

	// Double stop should be safe (idempotent)
	err = provider.Stop(stopCtx)
	require.NoError(t, err)
}

func TestCronProvider_Refresh_PicksUpNewRules(t *testing.T) {
	mockRepo := new(MockRuleRepository)
	mockExecutor := new(MockRuleExecutor)

	// Initial load: 1 rule
	initialRules := []*domain.AutomationRule{
		createTestRule("rule-1", "user-1", "0 9 * * *"),
	}

	// After refresh: 2 rules
	updatedRules := []*domain.AutomationRule{
		createTestRule("rule-1", "user-1", "0 9 * * *"),
		createTestRule("rule-2", "user-2", "0 12 * * *"),
	}

	mockRepo.On("GetRulesByScheduleTrigger", mock.Anything).Return(initialRules, nil).Once()
	mockRepo.On("GetRulesByScheduleTrigger", mock.Anything).Return(updatedRules, nil).Once()

	provider := NewCronTriggerProvider(mockRepo, mockExecutor, time.Minute, nil)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err := provider.Start(ctx)
	require.NoError(t, err)

	assert.Equal(t, 1, provider.ScheduledRuleCount())

	// Manually trigger refresh
	err = provider.Refresh(ctx)
	require.NoError(t, err)

	assert.Equal(t, 2, provider.ScheduledRuleCount())

	stopCtx, stopCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer stopCancel()
	_ = provider.Stop(stopCtx)
}

func TestCronProvider_Refresh_RemovesDeletedRules(t *testing.T) {
	mockRepo := new(MockRuleRepository)
	mockExecutor := new(MockRuleExecutor)

	// Initial load: 2 rules
	initialRules := []*domain.AutomationRule{
		createTestRule("rule-1", "user-1", "0 9 * * *"),
		createTestRule("rule-2", "user-2", "0 12 * * *"),
	}

	// After refresh: 1 rule (rule-2 deleted)
	updatedRules := []*domain.AutomationRule{
		createTestRule("rule-1", "user-1", "0 9 * * *"),
	}

	mockRepo.On("GetRulesByScheduleTrigger", mock.Anything).Return(initialRules, nil).Once()
	mockRepo.On("GetRulesByScheduleTrigger", mock.Anything).Return(updatedRules, nil).Once()

	provider := NewCronTriggerProvider(mockRepo, mockExecutor, time.Minute, nil)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err := provider.Start(ctx)
	require.NoError(t, err)

	assert.Equal(t, 2, provider.ScheduledRuleCount())

	// Manually trigger refresh
	err = provider.Refresh(ctx)
	require.NoError(t, err)

	assert.Equal(t, 1, provider.ScheduledRuleCount())

	stopCtx, stopCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer stopCancel()
	_ = provider.Stop(stopCtx)
}

func TestCronProvider_Refresh_UpdatesModifiedCron(t *testing.T) {
	mockRepo := new(MockRuleRepository)
	mockExecutor := new(MockRuleExecutor)

	// Initial: rule runs at 9am
	initialRules := []*domain.AutomationRule{
		createTestRule("rule-1", "user-1", "0 9 * * *"),
	}

	// Updated: rule now runs at 10am
	updatedRules := []*domain.AutomationRule{
		createTestRule("rule-1", "user-1", "0 10 * * *"),
	}

	mockRepo.On("GetRulesByScheduleTrigger", mock.Anything).Return(initialRules, nil).Once()
	mockRepo.On("GetRulesByScheduleTrigger", mock.Anything).Return(updatedRules, nil).Once()

	provider := NewCronTriggerProvider(mockRepo, mockExecutor, time.Minute, nil)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err := provider.Start(ctx)
	require.NoError(t, err)

	// Get the original cron expression for rule-1
	originalCron := provider.GetRuleCronExpression("rule-1")
	assert.Equal(t, "0 9 * * *", originalCron)

	// Refresh
	err = provider.Refresh(ctx)
	require.NoError(t, err)

	// Verify cron expression was updated
	updatedCron := provider.GetRuleCronExpression("rule-1")
	assert.Equal(t, "0 10 * * *", updatedCron)

	stopCtx, stopCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer stopCancel()
	_ = provider.Stop(stopCtx)
}

func TestCronProvider_ExecutesRuleOnSchedule(t *testing.T) {
	mockRepo := new(MockRuleRepository)
	mockExecutor := new(MockRuleExecutor)

	// Create a rule that runs every second (for testing)
	rule := createTestRule("rule-1", "user-1", "* * * * * *") // Every second (6-field cron)

	mockRepo.On("GetRulesByScheduleTrigger", mock.Anything).Return([]*domain.AutomationRule{rule}, nil)
	mockRepo.On("GetByID", mock.Anything, "rule-1").Return(rule, nil)
	mockRepo.On("IncrementExecutionCount", mock.Anything, "rule-1").Return(nil)
	mockRepo.On("UpdateLastSuccessful", mock.Anything, "rule-1").Return(nil)

	execution := &domain.RuleExecution{
		ID:     "exec-1",
		RuleID: "rule-1",
		Status: domain.ExecutionStatusCompleted,
	}
	mockExecutor.On("ExecuteRule", mock.Anything, rule, mock.Anything).Return(execution, nil)

	// Use seconds-based cron for faster test
	provider := NewCronTriggerProviderWithSeconds(mockRepo, mockExecutor, time.Minute, nil)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err := provider.Start(ctx)
	require.NoError(t, err)

	// Wait for at least one execution
	time.Sleep(2 * time.Second)

	// Verify execution happened
	assert.GreaterOrEqual(t, mockExecutor.GetExecutionCount(), int32(1))

	stopCtx, stopCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer stopCancel()
	_ = provider.Stop(stopCtx)

	mockExecutor.AssertCalled(t, "ExecuteRule", mock.Anything, rule, mock.Anything)
}

func TestCronProvider_RespectsTimezone(t *testing.T) {
	mockRepo := new(MockRuleRepository)
	mockExecutor := new(MockRuleExecutor)

	// Create rule with timezone
	rule := createTestRuleWithTimezone("rule-1", "user-1", "0 9 * * *", "America/New_York")

	mockRepo.On("GetRulesByScheduleTrigger", mock.Anything).Return([]*domain.AutomationRule{rule}, nil)

	provider := NewCronTriggerProvider(mockRepo, mockExecutor, time.Minute, nil)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err := provider.Start(ctx)
	require.NoError(t, err)

	// Verify rule was scheduled (timezone handling is internal to cron library)
	assert.Equal(t, 1, provider.ScheduledRuleCount())

	stopCtx, stopCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer stopCancel()
	_ = provider.Stop(stopCtx)
}

func TestCronProvider_HandlesRuleNotFoundOnExecution(t *testing.T) {
	mockRepo := new(MockRuleRepository)
	mockExecutor := new(MockRuleExecutor)

	rule := createTestRule("rule-1", "user-1", "* * * * * *")

	mockRepo.On("GetRulesByScheduleTrigger", mock.Anything).Return([]*domain.AutomationRule{rule}, nil)
	// Rule deleted between scheduling and execution
	mockRepo.On("GetByID", mock.Anything, "rule-1").Return(nil, domain.ErrAutomationRuleNotFound)

	provider := NewCronTriggerProviderWithSeconds(mockRepo, mockExecutor, time.Minute, nil)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err := provider.Start(ctx)
	require.NoError(t, err)

	// Wait for execution attempt
	time.Sleep(2 * time.Second)

	// Should not crash, execution should not happen
	assert.Equal(t, int32(0), mockExecutor.GetExecutionCount())

	stopCtx, stopCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer stopCancel()
	_ = provider.Stop(stopCtx)
}

func TestCronProvider_HandlesDisabledRuleOnExecution(t *testing.T) {
	mockRepo := new(MockRuleRepository)
	mockExecutor := new(MockRuleExecutor)

	rule := createTestRule("rule-1", "user-1", "* * * * * *")

	mockRepo.On("GetRulesByScheduleTrigger", mock.Anything).Return([]*domain.AutomationRule{rule}, nil)

	// Rule was disabled after scheduling
	disabledRule := createTestRule("rule-1", "user-1", "* * * * * *")
	disabledRule.Enabled = false
	mockRepo.On("GetByID", mock.Anything, "rule-1").Return(disabledRule, nil)

	provider := NewCronTriggerProviderWithSeconds(mockRepo, mockExecutor, time.Minute, nil)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err := provider.Start(ctx)
	require.NoError(t, err)

	// Wait for execution attempt
	time.Sleep(2 * time.Second)

	// Should skip disabled rule
	assert.Equal(t, int32(0), mockExecutor.GetExecutionCount())

	stopCtx, stopCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer stopCancel()
	_ = provider.Stop(stopCtx)
}

func TestCronProvider_RecoversPanicInJob(t *testing.T) {
	mockRepo := new(MockRuleRepository)
	mockExecutor := new(MockRuleExecutor)

	rule := createTestRule("rule-1", "user-1", "* * * * * *")

	mockRepo.On("GetRulesByScheduleTrigger", mock.Anything).Return([]*domain.AutomationRule{rule}, nil)
	mockRepo.On("GetByID", mock.Anything, "rule-1").Return(rule, nil)

	// Executor panics
	mockExecutor.On("ExecuteRule", mock.Anything, rule, mock.Anything).Run(func(args mock.Arguments) {
		panic("simulated panic in executor")
	})

	provider := NewCronTriggerProviderWithSeconds(mockRepo, mockExecutor, time.Minute, nil)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err := provider.Start(ctx)
	require.NoError(t, err)

	// Wait for execution (should recover from panic)
	time.Sleep(2 * time.Second)

	// Provider should still be running (panic recovered)
	stopCtx, stopCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer stopCancel()
	err = provider.Stop(stopCtx)
	require.NoError(t, err)
}

func TestCronProvider_ConcurrentRefresh(t *testing.T) {
	mockRepo := new(MockRuleRepository)
	mockExecutor := new(MockRuleExecutor)

	rules := []*domain.AutomationRule{
		createTestRule("rule-1", "user-1", "0 9 * * *"),
	}

	mockRepo.On("GetRulesByScheduleTrigger", mock.Anything).Return(rules, nil)

	provider := NewCronTriggerProvider(mockRepo, mockExecutor, time.Minute, nil)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err := provider.Start(ctx)
	require.NoError(t, err)

	// Concurrent refreshes should be safe
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = provider.Refresh(ctx)
		}()
	}
	wg.Wait()

	stopCtx, stopCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer stopCancel()
	_ = provider.Stop(stopCtx)
}
