// Package workers provides background job processing for automation rules.
// Issue #37: Rule Scheduler - Subscription limit adapter for execution limits.
package workers

import (
	"context"
	"log"
	"sync"
	"time"
)

// SubscriptionLimitAdapter implements SubscriptionLimitChecker with in-memory tracking.
// This is a temporary implementation for Issue #37. A proper implementation
// should use Redis or the database for persistent tracking across restarts.
//
// TODO: Replace with persistent storage (Redis or database) for production.
type SubscriptionLimitAdapter struct {
	// dailyExecutions tracks user_id -> count for today
	dailyExecutions map[string]int
	// dailyLimits tracks tier -> limit
	dailyLimits map[string]int
	// lastReset tracks when the daily counts were last reset
	lastReset time.Time
	mu        sync.RWMutex
}

// DefaultDailyLimits returns the default daily execution limits per tier.
// These match the tier limits from config/subscription_tiers.yaml.
func DefaultDailyLimits() map[string]int {
	return map[string]int{
		"free":         1,    // Free tier: 1 execution per day
		"premium_lite": 10,   // Premium Lite: 10 executions per day
		"premium":      50,   // Premium: 50 executions per day
		"premium_plus": 1000, // Premium+: 1000 executions per day (effectively unlimited)
	}
}

// NewSubscriptionLimitAdapter creates a new adapter with default limits.
func NewSubscriptionLimitAdapter() *SubscriptionLimitAdapter {
	return &SubscriptionLimitAdapter{
		dailyExecutions: make(map[string]int),
		dailyLimits:     DefaultDailyLimits(),
		lastReset:       time.Now().UTC(),
	}
}

// NewSubscriptionLimitAdapterWithLimits creates a new adapter with custom limits.
func NewSubscriptionLimitAdapterWithLimits(limits map[string]int) *SubscriptionLimitAdapter {
	adapter := NewSubscriptionLimitAdapter()
	adapter.dailyLimits = limits
	return adapter
}

// CheckDailyExecutionLimit checks if a user can execute more rules today.
// For now, it uses a default limit since we don't have access to user tier info.
// TODO: Integrate with subscription service to get actual user tier.
func (a *SubscriptionLimitAdapter) CheckDailyExecutionLimit(ctx context.Context, userID string) (allowed bool, remaining int, err error) {
	a.mu.Lock()
	defer a.mu.Unlock()

	// Reset counts if it's a new day
	a.resetIfNewDay()

	// Get current execution count for user
	current := a.dailyExecutions[userID]

	// Use a default limit (premium_lite equivalent) for now
	// TODO: Look up user's actual tier from subscription service
	limit := a.dailyLimits["premium_lite"]

	remaining = limit - current
	if remaining < 0 {
		remaining = 0
	}

	return current < limit, remaining, nil
}

// IncrementDailyExecutions increments the daily execution count for a user.
func (a *SubscriptionLimitAdapter) IncrementDailyExecutions(ctx context.Context, userID string) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	// Reset counts if it's a new day
	a.resetIfNewDay()

	a.dailyExecutions[userID]++
	log.Printf("[SubscriptionLimitAdapter] User %s execution count: %d", userID, a.dailyExecutions[userID])
	return nil
}

// resetIfNewDay resets the daily execution counts if it's a new day.
// Must be called with mu held.
func (a *SubscriptionLimitAdapter) resetIfNewDay() {
	now := time.Now().UTC()
	if now.Day() != a.lastReset.Day() || now.Month() != a.lastReset.Month() || now.Year() != a.lastReset.Year() {
		log.Printf("[SubscriptionLimitAdapter] Resetting daily execution counts for new day")
		a.dailyExecutions = make(map[string]int)
		a.lastReset = now
	}
}

// GetUserExecutionCount returns the current execution count for a user (for testing/debugging).
func (a *SubscriptionLimitAdapter) GetUserExecutionCount(userID string) int {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.dailyExecutions[userID]
}

// SetUserLimit sets a custom limit for a specific user (for testing/overrides).
func (a *SubscriptionLimitAdapter) SetUserLimit(userID string, limit int) {
	a.mu.Lock()
	defer a.mu.Unlock()
	// This would be used for per-user overrides
	// For now, it's a no-op since we use tier-based limits
	log.Printf("[SubscriptionLimitAdapter] Setting custom limit for user %s: %d", userID, limit)
}
