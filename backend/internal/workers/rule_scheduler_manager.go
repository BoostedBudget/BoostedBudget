// Package workers provides background job processing for automation rules.
// Issue #37: Rule Scheduler - RuleSchedulerManager orchestrates all trigger providers.
package workers

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"sync"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
)

// Errors for RuleSchedulerManager.
var (
	ErrNoManualTriggerProvider = errors.New("no provider supports manual triggering")
	ErrManagerNotStarted       = errors.New("scheduler manager is not started")
)

// RuleSchedulerManager orchestrates multiple RuleTriggerProvider implementations.
// It manages the lifecycle of all providers and routes manual trigger requests
// to the appropriate provider.
type RuleSchedulerManager struct {
	logger    *slog.Logger
	providers map[string]RuleTriggerProvider
	mu        sync.RWMutex
	started   bool
}

// NewRuleSchedulerManager creates a new RuleSchedulerManager instance.
func NewRuleSchedulerManager(logger *slog.Logger) *RuleSchedulerManager {
	if logger == nil {
		logger = slog.Default()
	}
	return &RuleSchedulerManager{
		logger:    logger,
		providers: make(map[string]RuleTriggerProvider),
	}
}

// RegisterProvider adds a trigger provider to the manager.
// If a provider with the same name already exists, it will be replaced.
func (m *RuleSchedulerManager) RegisterProvider(provider RuleTriggerProvider) {
	m.mu.Lock()
	defer m.mu.Unlock()

	name := provider.Name()
	if existing, ok := m.providers[name]; ok {
		m.logger.Warn("replacing existing provider", "name", name)
		_ = existing // Could call Stop() if needed, but we don't track started state per provider
	}

	m.providers[name] = provider
	m.logger.Info("registered provider", "name", name, "supports_manual", provider.SupportsManualTrigger())
}

// Start starts all registered providers.
// Returns the first error encountered, if any.
func (m *RuleSchedulerManager) Start(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.started {
		return nil
	}

	m.logger.Info("starting providers", "count", len(m.providers))

	for name, provider := range m.providers {
		m.logger.Info("starting provider", "name", name)
		if err := provider.Start(ctx); err != nil {
			return fmt.Errorf("failed to start provider %s: %w", name, err)
		}
	}

	m.started = true
	m.logger.Info("all providers started successfully")
	return nil
}

// Stop stops all registered providers.
// Attempts to stop all providers even if some fail, returns first error.
func (m *RuleSchedulerManager) Stop(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if !m.started {
		return nil
	}

	m.logger.Info("stopping providers", "count", len(m.providers))

	var firstErr error
	for name, provider := range m.providers {
		m.logger.Info("stopping provider", "name", name)
		if err := provider.Stop(ctx); err != nil {
			m.logger.Error("error stopping provider", "name", name, "error", err)
			if firstErr == nil {
				firstErr = fmt.Errorf("failed to stop provider %s: %w", name, err)
			}
		}
	}

	m.started = false
	m.logger.Info("all providers stopped")
	return firstErr
}

// TriggerRule manually triggers a rule execution.
// Routes to the first provider that supports manual triggering.
func (m *RuleSchedulerManager) TriggerRule(ctx context.Context, ruleID string, triggerData map[string]interface{}) (*domain.RuleExecution, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if !m.started {
		return nil, ErrManagerNotStarted
	}

	// Find a provider that supports manual triggering
	for _, provider := range m.providers {
		if provider.SupportsManualTrigger() {
			return provider.TriggerRule(ctx, ruleID, triggerData)
		}
	}

	return nil, ErrNoManualTriggerProvider
}

// ProviderCount returns the number of registered providers.
func (m *RuleSchedulerManager) ProviderCount() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.providers)
}

// HasProvider checks if a provider with the given name is registered.
func (m *RuleSchedulerManager) HasProvider(name string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	_, ok := m.providers[name]
	return ok
}

// GetProvider returns a provider by name, or nil if not found.
func (m *RuleSchedulerManager) GetProvider(name string) RuleTriggerProvider {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.providers[name]
}

// ListProviders returns the names of all registered providers (sorted).
func (m *RuleSchedulerManager) ListProviders() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	names := make([]string, 0, len(m.providers))
	for name := range m.providers {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// IsStarted returns whether the manager has been started.
func (m *RuleSchedulerManager) IsStarted() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.started
}
