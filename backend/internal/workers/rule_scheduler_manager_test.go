// Package workers provides background job processing for automation rules.
// Issue #37: Rule Scheduler - RuleSchedulerManager tests (TDD).
package workers

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// ============================================================================
// Mock Provider for Testing
// ============================================================================

// MockTriggerProvider implements RuleTriggerProvider for testing.
type MockTriggerProvider struct {
	mock.Mock
	name                 string
	supportsManual       bool
	startCalled          bool
	stopCalled           bool
	mu                   sync.Mutex
}

func NewMockTriggerProvider(name string, supportsManual bool) *MockTriggerProvider {
	return &MockTriggerProvider{
		name:           name,
		supportsManual: supportsManual,
	}
}

func (m *MockTriggerProvider) Name() string {
	return m.name
}

func (m *MockTriggerProvider) SupportsManualTrigger() bool {
	return m.supportsManual
}

func (m *MockTriggerProvider) Start(ctx context.Context) error {
	m.mu.Lock()
	m.startCalled = true
	m.mu.Unlock()
	args := m.Called(ctx)
	return args.Error(0)
}

func (m *MockTriggerProvider) Stop(ctx context.Context) error {
	m.mu.Lock()
	m.stopCalled = true
	m.mu.Unlock()
	args := m.Called(ctx)
	return args.Error(0)
}

func (m *MockTriggerProvider) TriggerRule(ctx context.Context, ruleID string, triggerData map[string]interface{}) (*domain.RuleExecution, error) {
	args := m.Called(ctx, ruleID, triggerData)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*domain.RuleExecution), args.Error(1)
}

func (m *MockTriggerProvider) WasStartCalled() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.startCalled
}

func (m *MockTriggerProvider) WasStopCalled() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.stopCalled
}

// ============================================================================
// RuleSchedulerManager Tests
// ============================================================================

func TestManager_RegisterProvider(t *testing.T) {
	manager := NewRuleSchedulerManager()

	cronProvider := NewMockTriggerProvider("cron", false)
	manualProvider := NewMockTriggerProvider("manual", true)

	manager.RegisterProvider(cronProvider)
	manager.RegisterProvider(manualProvider)

	assert.Equal(t, 2, manager.ProviderCount())
	assert.True(t, manager.HasProvider("cron"))
	assert.True(t, manager.HasProvider("manual"))
	assert.False(t, manager.HasProvider("unknown"))
}

func TestManager_RegisterProvider_OverwritesDuplicate(t *testing.T) {
	manager := NewRuleSchedulerManager()

	provider1 := NewMockTriggerProvider("cron", false)
	provider2 := NewMockTriggerProvider("cron", true) // Same name, different

	manager.RegisterProvider(provider1)
	manager.RegisterProvider(provider2)

	// Should have only 1 provider (last one wins)
	assert.Equal(t, 1, manager.ProviderCount())
}

func TestManager_Start_StartsAllProviders(t *testing.T) {
	manager := NewRuleSchedulerManager()

	cronProvider := NewMockTriggerProvider("cron", false)
	manualProvider := NewMockTriggerProvider("manual", true)

	cronProvider.On("Start", mock.Anything).Return(nil)
	manualProvider.On("Start", mock.Anything).Return(nil)

	manager.RegisterProvider(cronProvider)
	manager.RegisterProvider(manualProvider)

	ctx := context.Background()
	err := manager.Start(ctx)

	require.NoError(t, err)
	assert.True(t, cronProvider.WasStartCalled())
	assert.True(t, manualProvider.WasStartCalled())
}

func TestManager_Start_ReturnsFirstError(t *testing.T) {
	manager := NewRuleSchedulerManager()

	cronProvider := NewMockTriggerProvider("cron", false)
	cronProvider.On("Start", mock.Anything).Return(errors.New("cron start failed"))

	manager.RegisterProvider(cronProvider)

	ctx := context.Background()
	err := manager.Start(ctx)

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "cron start failed")
}

func TestManager_Start_Idempotent(t *testing.T) {
	manager := NewRuleSchedulerManager()

	cronProvider := NewMockTriggerProvider("cron", false)
	cronProvider.On("Start", mock.Anything).Return(nil).Once()

	manager.RegisterProvider(cronProvider)

	ctx := context.Background()

	// First start
	err := manager.Start(ctx)
	require.NoError(t, err)

	// Second start should be no-op
	err = manager.Start(ctx)
	require.NoError(t, err)

	// Start should only be called once
	cronProvider.AssertNumberOfCalls(t, "Start", 1)
}

func TestManager_Stop_StopsAllProviders(t *testing.T) {
	manager := NewRuleSchedulerManager()

	cronProvider := NewMockTriggerProvider("cron", false)
	manualProvider := NewMockTriggerProvider("manual", true)

	cronProvider.On("Start", mock.Anything).Return(nil)
	cronProvider.On("Stop", mock.Anything).Return(nil)
	manualProvider.On("Start", mock.Anything).Return(nil)
	manualProvider.On("Stop", mock.Anything).Return(nil)

	manager.RegisterProvider(cronProvider)
	manager.RegisterProvider(manualProvider)

	ctx := context.Background()
	_ = manager.Start(ctx)

	err := manager.Stop(ctx)

	require.NoError(t, err)
	assert.True(t, cronProvider.WasStopCalled())
	assert.True(t, manualProvider.WasStopCalled())
}

func TestManager_Stop_Idempotent(t *testing.T) {
	manager := NewRuleSchedulerManager()

	cronProvider := NewMockTriggerProvider("cron", false)
	cronProvider.On("Start", mock.Anything).Return(nil)
	cronProvider.On("Stop", mock.Anything).Return(nil).Once()

	manager.RegisterProvider(cronProvider)

	ctx := context.Background()
	_ = manager.Start(ctx)

	// First stop
	err := manager.Stop(ctx)
	require.NoError(t, err)

	// Second stop should be no-op
	err = manager.Stop(ctx)
	require.NoError(t, err)

	// Stop should only be called once
	cronProvider.AssertNumberOfCalls(t, "Stop", 1)
}

func TestManager_Stop_WithoutStart(t *testing.T) {
	manager := NewRuleSchedulerManager()

	cronProvider := NewMockTriggerProvider("cron", false)
	manager.RegisterProvider(cronProvider)

	ctx := context.Background()

	// Stop without start should be no-op
	err := manager.Stop(ctx)
	require.NoError(t, err)

	// Stop should never be called
	cronProvider.AssertNotCalled(t, "Stop")
}

func TestManager_TriggerRule_RoutesToManualProvider(t *testing.T) {
	manager := NewRuleSchedulerManager()

	cronProvider := NewMockTriggerProvider("cron", false)
	manualProvider := NewMockTriggerProvider("manual", true)

	execution := &domain.RuleExecution{
		ID:     "exec-1",
		RuleID: "rule-1",
		Status: domain.ExecutionStatusCompleted,
	}

	manualProvider.On("TriggerRule", mock.Anything, "rule-1", mock.Anything).Return(execution, nil)

	cronProvider.On("Start", mock.Anything).Return(nil)
	manualProvider.On("Start", mock.Anything).Return(nil)

	manager.RegisterProvider(cronProvider)
	manager.RegisterProvider(manualProvider)

	ctx := context.Background()
	_ = manager.Start(ctx)

	result, err := manager.TriggerRule(ctx, "rule-1", nil)

	require.NoError(t, err)
	assert.Equal(t, "exec-1", result.ID)
	assert.Equal(t, domain.ExecutionStatusCompleted, result.Status)

	manualProvider.AssertCalled(t, "TriggerRule", mock.Anything, "rule-1", mock.Anything)
	cronProvider.AssertNotCalled(t, "TriggerRule")
}

func TestManager_TriggerRule_NoManualProvider(t *testing.T) {
	manager := NewRuleSchedulerManager()

	// Only cron provider, which doesn't support manual triggers
	cronProvider := NewMockTriggerProvider("cron", false)
	cronProvider.On("Start", mock.Anything).Return(nil)

	manager.RegisterProvider(cronProvider)

	ctx := context.Background()
	_ = manager.Start(ctx)

	result, err := manager.TriggerRule(ctx, "rule-1", nil)

	assert.Error(t, err)
	assert.Nil(t, result)
	assert.Equal(t, ErrNoManualTriggerProvider, err)
}

func TestManager_TriggerRule_NotStarted(t *testing.T) {
	manager := NewRuleSchedulerManager()

	manualProvider := NewMockTriggerProvider("manual", true)
	manager.RegisterProvider(manualProvider)

	ctx := context.Background()

	// TriggerRule without starting manager
	result, err := manager.TriggerRule(ctx, "rule-1", nil)

	assert.Error(t, err)
	assert.Nil(t, result)
	assert.Equal(t, ErrManagerNotStarted, err)
}

func TestManager_GetProvider(t *testing.T) {
	manager := NewRuleSchedulerManager()

	cronProvider := NewMockTriggerProvider("cron", false)
	manager.RegisterProvider(cronProvider)

	provider := manager.GetProvider("cron")
	assert.NotNil(t, provider)
	assert.Equal(t, "cron", provider.Name())

	unknown := manager.GetProvider("unknown")
	assert.Nil(t, unknown)
}

func TestManager_ListProviders(t *testing.T) {
	manager := NewRuleSchedulerManager()

	cronProvider := NewMockTriggerProvider("cron", false)
	manualProvider := NewMockTriggerProvider("manual", true)
	eventProvider := NewMockTriggerProvider("event", false)

	manager.RegisterProvider(cronProvider)
	manager.RegisterProvider(manualProvider)
	manager.RegisterProvider(eventProvider)

	names := manager.ListProviders()

	assert.Len(t, names, 3)
	assert.Contains(t, names, "cron")
	assert.Contains(t, names, "manual")
	assert.Contains(t, names, "event")
}

func TestManager_ConcurrentAccess(t *testing.T) {
	manager := NewRuleSchedulerManager()

	cronProvider := NewMockTriggerProvider("cron", false)
	manualProvider := NewMockTriggerProvider("manual", true)

	cronProvider.On("Start", mock.Anything).Return(nil)
	cronProvider.On("Stop", mock.Anything).Return(nil)
	manualProvider.On("Start", mock.Anything).Return(nil)
	manualProvider.On("Stop", mock.Anything).Return(nil)

	manager.RegisterProvider(cronProvider)
	manager.RegisterProvider(manualProvider)

	ctx := context.Background()
	_ = manager.Start(ctx)

	// Concurrent access should be safe
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = manager.ListProviders()
			_ = manager.HasProvider("cron")
			_ = manager.GetProvider("manual")
		}()
	}
	wg.Wait()

	_ = manager.Stop(ctx)
}

func TestManager_StartWithTimeout(t *testing.T) {
	manager := NewRuleSchedulerManager()

	slowProvider := NewMockTriggerProvider("slow", false)
	slowProvider.On("Start", mock.Anything).Run(func(args mock.Arguments) {
		ctx := args.Get(0).(context.Context)
		select {
		case <-ctx.Done():
			// Context cancelled
		case <-time.After(5 * time.Second):
			// Slow start
		}
	}).Return(context.Canceled)

	manager.RegisterProvider(slowProvider)

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	err := manager.Start(ctx)

	// Should return error (cancelled)
	assert.Error(t, err)
}
