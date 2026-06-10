// Package workers provides background job processing for automation boosts.
// Issue #37: Boost Scheduler - modular trigger provider architecture.
package workers

import (
	"context"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
)

// BoostTriggerProvider defines the interface for boost trigger mechanisms.
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
type BoostTriggerProvider interface {
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

	// TriggerBoost manually triggers a specific boost with optional trigger data.
	// Only applicable to providers that support on-demand triggering.
	// Returns the execution result or an error.
	//
	// For manual provider: executes the boost immediately with provided data.
	// For cron provider: not supported (returns ErrManualTriggerNotSupported).
	TriggerBoost(ctx context.Context, boostID string, triggerData map[string]interface{}) (*domain.BoostExecution, error)

	// SupportsManualTrigger indicates if this provider supports TriggerBoost().
	// Cron: false (boosts run on schedule, not on-demand)
	// Manual: true (entire purpose is on-demand triggering)
	SupportsManualTrigger() bool
}
