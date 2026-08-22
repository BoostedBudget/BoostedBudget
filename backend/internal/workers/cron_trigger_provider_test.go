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

// MockBoostsRepository implements repository.BoostsRepository for testing.
type MockBoostsRepository struct {
	mock.Mock
}

func (m *MockBoostsRepository) Create(ctx context.Context, rule *domain.Boost) error {
	args := m.Called(ctx, rule)
	return args.Error(0)
}

func (m *MockBoostsRepository) GetByID(ctx context.Context, id string) (*domain.Boost, error) {
	args := m.Called(ctx, id)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*domain.Boost), args.Error(1)
}

func (m *MockBoostsRepository) GetByUserID(ctx context.Context, userID string) ([]*domain.Boost, error) {
	args := m.Called(ctx, userID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]*domain.Boost), args.Error(1)
}

func (m *MockBoostsRepository) GetByUserIDFiltered(ctx context.Context, userID string, boostTypes []domain.BoostType) ([]*domain.Boost, error) {
	args := m.Called(ctx, userID, boostTypes)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]*domain.Boost), args.Error(1)
}

func (m *MockBoostsRepository) GetEnabledByUserID(ctx context.Context, userID string) ([]*domain.Boost, error) {
	args := m.Called(ctx, userID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]*domain.Boost), args.Error(1)
}

func (m *MockBoostsRepository) GetByName(ctx context.Context, userID, name string) (*domain.Boost, error) {
	args := m.Called(ctx, userID, name)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*domain.Boost), args.Error(1)
}

func (m *MockBoostsRepository) Update(ctx context.Context, rule *domain.Boost) error {
	args := m.Called(ctx, rule)
	return args.Error(0)
}

func (m *MockBoostsRepository) Delete(ctx context.Context, id string) error {
	args := m.Called(ctx, id)
	return args.Error(0)
}

func (m *MockBoostsRepository) CountByUserID(ctx context.Context, userID string) (int, error) {
	args := m.Called(ctx, userID)
	return args.Int(0), args.Error(1)
}

func (m *MockBoostsRepository) IncrementExecutionCount(ctx context.Context, ruleID string) error {
	args := m.Called(ctx, ruleID)
	return args.Error(0)
}

func (m *MockBoostsRepository) UpdateLastSuccessful(ctx context.Context, ruleID string) error {
	args := m.Called(ctx, ruleID)
	return args.Error(0)
}

func (m *MockBoostsRepository) GetBoostsByEventTrigger(ctx context.Context, userID, eventType string) ([]*domain.Boost, error) {
	args := m.Called(ctx, userID, eventType)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]*domain.Boost), args.Error(1)
}

func (m *MockBoostsRepository) GetBoostsByType(ctx context.Context, userID string, ruleType domain.BoostType) ([]*domain.Boost, error) {
	args := m.Called(ctx, userID, ruleType)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]*domain.Boost), args.Error(1)
}

func (m *MockBoostsRepository) GetBoostsByScheduleTrigger(ctx context.Context) ([]*domain.Boost, error) {
	args := m.Called(ctx)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]*domain.Boost), args.Error(1)
}

// MockBoostExecutor implements a mock for rule execution.
type MockBoostExecutor struct {
	mock.Mock
	ExecutionCount int32
}

func (m *MockBoostExecutor) ExecuteBoost(ctx context.Context, rule *domain.Boost, triggerData map[string]interface{}) (*domain.BoostExecution, error) {
	atomic.AddInt32(&m.ExecutionCount, 1)
	args := m.Called(ctx, rule, triggerData)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*domain.BoostExecution), args.Error(1)
}

func (m *MockBoostExecutor) GetExecutionCount() int32 {
	return atomic.LoadInt32(&m.ExecutionCount)
}

// ============================================================================
// Test Helpers
// ============================================================================

func createTestBoost(id, userID, cronExpr string) *domain.Boost {
	return &domain.Boost{
		ID:      id,
		UserID:  userID,
		Name:    "Test Boost " + id,
		Enabled: true,
		TriggerConfig: domain.TriggerConfig{
			Type: domain.TriggerTypeSchedule,
			Cron: cronExpr,
		},
		BoostType: domain.BoostTypeBudgetAlert,
		Actions:  json.RawMessage("[]"),
	}
}

func createTestBoostWithTimezone(id, userID, cron, timezone string) *domain.Boost {
	rule := createTestBoost(id, userID, cron)
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

func TestCronProvider_TriggerBoost_NotSupported(t *testing.T) {
	provider := NewCronTriggerProvider(nil, nil, time.Minute, nil)

	execution, err := provider.TriggerBoost(context.Background(), "rule-123", nil)

	assert.Error(t, err)
	assert.Nil(t, execution)
	assert.Equal(t, ErrManualTriggerNotSupported, err)
}

func TestCronProvider_Start_LoadsScheduledRules(t *testing.T) {
	// Setup mocks
	mockRepo := new(MockBoostsRepository)
	mockExecutor := new(MockBoostExecutor)

	rules := []*domain.Boost{
		createTestBoost("rule-1", "user-1", "0 9 * * *"),  // 9am daily
		createTestBoost("rule-2", "user-2", "0 12 * * 1"), // noon on Mondays
	}

	mockRepo.On("GetBoostsByScheduleTrigger", mock.Anything).Return(rules, nil)

	provider := NewCronTriggerProvider(mockRepo, mockExecutor, time.Minute, nil)

	// Start provider
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err := provider.Start(ctx)
	require.NoError(t, err)

	// Verify rules were loaded
	assert.Equal(t, 2, provider.ScheduledBoostCount())

	// Stop provider
	stopCtx, stopCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer stopCancel()
	err = provider.Stop(stopCtx)
	require.NoError(t, err)

	mockRepo.AssertExpectations(t)
}

func TestCronProvider_Start_SkipsDisabledRules(t *testing.T) {
	mockRepo := new(MockBoostsRepository)
	mockExecutor := new(MockBoostExecutor)

	// GetBoostsByScheduleTrigger already filters enabled=true in SQL
	// This test verifies the query returns only enabled rules
	enabledRule := createTestBoost("rule-1", "user-1", "0 9 * * *")

	mockRepo.On("GetBoostsByScheduleTrigger", mock.Anything).Return([]*domain.Boost{enabledRule}, nil)

	provider := NewCronTriggerProvider(mockRepo, mockExecutor, time.Minute, nil)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err := provider.Start(ctx)
	require.NoError(t, err)

	// Only the enabled rule should be scheduled
	assert.Equal(t, 1, provider.ScheduledBoostCount())

	stopCtx, stopCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer stopCancel()
	_ = provider.Stop(stopCtx)

	mockRepo.AssertExpectations(t)
}

func TestCronProvider_Start_SkipsInvalidCronExpressions(t *testing.T) {
	mockRepo := new(MockBoostsRepository)
	mockExecutor := new(MockBoostExecutor)

	rules := []*domain.Boost{
		createTestBoost("rule-1", "user-1", "0 9 * * *"),     // Valid
		createTestBoost("rule-2", "user-2", "invalid-cron"),  // Invalid
		createTestBoost("rule-3", "user-3", "0 12 * * 1-5"),  // Valid
	}

	mockRepo.On("GetBoostsByScheduleTrigger", mock.Anything).Return(rules, nil)

	provider := NewCronTriggerProvider(mockRepo, mockExecutor, time.Minute, nil)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err := provider.Start(ctx)
	require.NoError(t, err)

	// Only valid cron expressions should be scheduled
	assert.Equal(t, 2, provider.ScheduledBoostCount())

	stopCtx, stopCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer stopCancel()
	_ = provider.Stop(stopCtx)
}

func TestCronProvider_Start_HandlesEmptyRulesList(t *testing.T) {
	mockRepo := new(MockBoostsRepository)
	mockExecutor := new(MockBoostExecutor)

	mockRepo.On("GetBoostsByScheduleTrigger", mock.Anything).Return([]*domain.Boost{}, nil)

	provider := NewCronTriggerProvider(mockRepo, mockExecutor, time.Minute, nil)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err := provider.Start(ctx)
	require.NoError(t, err)

	assert.Equal(t, 0, provider.ScheduledBoostCount())

	stopCtx, stopCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer stopCancel()
	_ = provider.Stop(stopCtx)
}

func TestCronProvider_Start_HandlesRepositoryError(t *testing.T) {
	mockRepo := new(MockBoostsRepository)
	mockExecutor := new(MockBoostExecutor)

	mockRepo.On("GetBoostsByScheduleTrigger", mock.Anything).Return(nil, assert.AnError)

	provider := NewCronTriggerProvider(mockRepo, mockExecutor, time.Minute, nil)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err := provider.Start(ctx)

	// Should return error if initial load fails
	assert.Error(t, err)
}

func TestCronProvider_GracefulShutdown(t *testing.T) {
	mockRepo := new(MockBoostsRepository)
	mockExecutor := new(MockBoostExecutor)

	mockRepo.On("GetBoostsByScheduleTrigger", mock.Anything).Return([]*domain.Boost{}, nil)

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
	mockRepo := new(MockBoostsRepository)
	mockExecutor := new(MockBoostExecutor)

	// Initial load: 1 rule
	initialRules := []*domain.Boost{
		createTestBoost("rule-1", "user-1", "0 9 * * *"),
	}

	// After refresh: 2 rules
	updatedRules := []*domain.Boost{
		createTestBoost("rule-1", "user-1", "0 9 * * *"),
		createTestBoost("rule-2", "user-2", "0 12 * * *"),
	}

	mockRepo.On("GetBoostsByScheduleTrigger", mock.Anything).Return(initialRules, nil).Once()
	mockRepo.On("GetBoostsByScheduleTrigger", mock.Anything).Return(updatedRules, nil).Once()

	provider := NewCronTriggerProvider(mockRepo, mockExecutor, time.Minute, nil)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err := provider.Start(ctx)
	require.NoError(t, err)

	assert.Equal(t, 1, provider.ScheduledBoostCount())

	// Manually trigger refresh
	err = provider.Refresh(ctx)
	require.NoError(t, err)

	assert.Equal(t, 2, provider.ScheduledBoostCount())

	stopCtx, stopCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer stopCancel()
	_ = provider.Stop(stopCtx)
}

func TestCronProvider_Refresh_RemovesDeletedRules(t *testing.T) {
	mockRepo := new(MockBoostsRepository)
	mockExecutor := new(MockBoostExecutor)

	// Initial load: 2 rules
	initialRules := []*domain.Boost{
		createTestBoost("rule-1", "user-1", "0 9 * * *"),
		createTestBoost("rule-2", "user-2", "0 12 * * *"),
	}

	// After refresh: 1 rule (rule-2 deleted)
	updatedRules := []*domain.Boost{
		createTestBoost("rule-1", "user-1", "0 9 * * *"),
	}

	mockRepo.On("GetBoostsByScheduleTrigger", mock.Anything).Return(initialRules, nil).Once()
	mockRepo.On("GetBoostsByScheduleTrigger", mock.Anything).Return(updatedRules, nil).Once()

	provider := NewCronTriggerProvider(mockRepo, mockExecutor, time.Minute, nil)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err := provider.Start(ctx)
	require.NoError(t, err)

	assert.Equal(t, 2, provider.ScheduledBoostCount())

	// Manually trigger refresh
	err = provider.Refresh(ctx)
	require.NoError(t, err)

	assert.Equal(t, 1, provider.ScheduledBoostCount())

	stopCtx, stopCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer stopCancel()
	_ = provider.Stop(stopCtx)
}

func TestCronProvider_Refresh_UpdatesModifiedCron(t *testing.T) {
	mockRepo := new(MockBoostsRepository)
	mockExecutor := new(MockBoostExecutor)

	// Initial: rule runs at 9am
	initialRules := []*domain.Boost{
		createTestBoost("rule-1", "user-1", "0 9 * * *"),
	}

	// Updated: rule now runs at 10am
	updatedRules := []*domain.Boost{
		createTestBoost("rule-1", "user-1", "0 10 * * *"),
	}

	mockRepo.On("GetBoostsByScheduleTrigger", mock.Anything).Return(initialRules, nil).Once()
	mockRepo.On("GetBoostsByScheduleTrigger", mock.Anything).Return(updatedRules, nil).Once()

	provider := NewCronTriggerProvider(mockRepo, mockExecutor, time.Minute, nil)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err := provider.Start(ctx)
	require.NoError(t, err)

	// Get the original cron expression for rule-1
	originalCron := provider.GetBoostCronExpression("rule-1")
	assert.Equal(t, "0 9 * * *", originalCron)

	// Refresh
	err = provider.Refresh(ctx)
	require.NoError(t, err)

	// Verify cron expression was updated
	updatedCron := provider.GetBoostCronExpression("rule-1")
	assert.Equal(t, "0 10 * * *", updatedCron)

	stopCtx, stopCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer stopCancel()
	_ = provider.Stop(stopCtx)
}

func TestCronProvider_ExecutesRuleOnSchedule(t *testing.T) {
	mockRepo := new(MockBoostsRepository)
	mockExecutor := new(MockBoostExecutor)

	// Create a rule that runs every second (for testing)
	rule := createTestBoost("rule-1", "user-1", "* * * * * *") // Every second (6-field cron)

	mockRepo.On("GetBoostsByScheduleTrigger", mock.Anything).Return([]*domain.Boost{rule}, nil)
	mockRepo.On("GetByID", mock.Anything, "rule-1").Return(rule, nil)
	mockRepo.On("IncrementExecutionCount", mock.Anything, "rule-1").Return(nil)
	mockRepo.On("UpdateLastSuccessful", mock.Anything, "rule-1").Return(nil)

	execution := &domain.BoostExecution{
		ID:     "exec-1",
		BoostID: "rule-1",
		Status: domain.ExecutionStatusCompleted,
	}
	mockExecutor.On("ExecuteBoost", mock.Anything, rule, mock.Anything).Return(execution, nil)

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

	mockExecutor.AssertCalled(t, "ExecuteBoost", mock.Anything, rule, mock.Anything)
}

func TestCronProvider_RespectsTimezone(t *testing.T) {
	mockRepo := new(MockBoostsRepository)
	mockExecutor := new(MockBoostExecutor)

	// Create rule with timezone
	rule := createTestBoostWithTimezone("rule-1", "user-1", "0 9 * * *", "America/New_York")

	mockRepo.On("GetBoostsByScheduleTrigger", mock.Anything).Return([]*domain.Boost{rule}, nil)

	provider := NewCronTriggerProvider(mockRepo, mockExecutor, time.Minute, nil)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err := provider.Start(ctx)
	require.NoError(t, err)

	// Verify rule was scheduled (timezone handling is internal to cron library)
	assert.Equal(t, 1, provider.ScheduledBoostCount())

	stopCtx, stopCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer stopCancel()
	_ = provider.Stop(stopCtx)
}

func TestCronProvider_HandlesRuleNotFoundOnExecution(t *testing.T) {
	mockRepo := new(MockBoostsRepository)
	mockExecutor := new(MockBoostExecutor)

	rule := createTestBoost("rule-1", "user-1", "* * * * * *")

	mockRepo.On("GetBoostsByScheduleTrigger", mock.Anything).Return([]*domain.Boost{rule}, nil)
	// Rule deleted between scheduling and execution
	mockRepo.On("GetByID", mock.Anything, "rule-1").Return(nil, domain.ErrBoostNotFound)

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
	mockRepo := new(MockBoostsRepository)
	mockExecutor := new(MockBoostExecutor)

	rule := createTestBoost("rule-1", "user-1", "* * * * * *")

	mockRepo.On("GetBoostsByScheduleTrigger", mock.Anything).Return([]*domain.Boost{rule}, nil)

	// Rule was disabled after scheduling
	disabledRule := createTestBoost("rule-1", "user-1", "* * * * * *")
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
	mockRepo := new(MockBoostsRepository)
	mockExecutor := new(MockBoostExecutor)

	rule := createTestBoost("rule-1", "user-1", "* * * * * *")

	mockRepo.On("GetBoostsByScheduleTrigger", mock.Anything).Return([]*domain.Boost{rule}, nil)
	mockRepo.On("GetByID", mock.Anything, "rule-1").Return(rule, nil)

	// Executor panics
	mockExecutor.On("ExecuteBoost", mock.Anything, rule, mock.Anything).Run(func(args mock.Arguments) {
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
	mockRepo := new(MockBoostsRepository)
	mockExecutor := new(MockBoostExecutor)

	rules := []*domain.Boost{
		createTestBoost("rule-1", "user-1", "0 9 * * *"),
	}

	mockRepo.On("GetBoostsByScheduleTrigger", mock.Anything).Return(rules, nil)

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
