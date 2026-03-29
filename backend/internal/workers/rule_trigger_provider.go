// Package workers provides background job processing for automation rules.
// Issue #37: Rule Scheduler - modular trigger provider architecture.
package workers

import (
	"context"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
)

// RuleTriggerProvider defines the interface for rule trigger mechanisms.
// This abstraction allows support for multiple trigger types (cron, manual, event, etc.)
// without changing the scheduler manager or execution logic.
//
// Current implementations:
//   - CronTriggerProvider (cron-based scheduling using robfig/cron/v3)
//   - ManualTriggerProvider (on-demand API triggering)
//
// Future implementations:
//   - WebhookTriggerProvider (external webhook triggers)
//   - ThresholdTriggerProvider (balance/budget threshold triggers)
type RuleTriggerProvider interface {
	// Name returns the unique identifier for this trigger provider.
	// Examples: "cron", "manual", "event", "webhook"
	Name() string

	// Start begins the trigger provider's operation.
	// For cron: loads scheduled rules and starts the cron scheduler.
	// For manual: no-op (manual triggers are on-demand).
	// Should be non-blocking (internal goroutines handle work).
	Start(ctx context.Context) error

	// Stop gracefully stops the trigger provider.
	// For cron: stops the scheduler and waits for running jobs to complete.
	// Should respect the context for timeout.
	Stop(ctx context.Context) error

	// TriggerRule manually triggers a specific rule with optional trigger data.
	// Only applicable to providers that support on-demand triggering.
	// Returns the execution result or an error.
	//
	// For manual provider: executes the rule immediately with provided data.
	// For cron provider: not supported (returns ErrManualTriggerNotSupported).
	TriggerRule(ctx context.Context, ruleID string, triggerData map[string]interface{}) (*domain.RuleExecution, error)

	// SupportsManualTrigger indicates if this provider supports TriggerRule().
	// Cron: false (rules run on schedule, not on-demand)
	// Manual: true (entire purpose is on-demand triggering)
	SupportsManualTrigger() bool
}
