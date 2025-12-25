// Package workers provides background job processing for automation rules.
// Issue #37: Rule Scheduler - ManualTriggerProvider for on-demand rule execution.
package workers

import (
	"context"
	"errors"
	"log"
	"time"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
)

// Errors for ManualTriggerProvider.
var (
	ErrRuleDisabled            = errors.New("rule is disabled")
	ErrExecutionLimitExceeded  = errors.New("daily execution limit exceeded")
)

// ManualTriggerProvider implements RuleTriggerProvider for on-demand rule execution.
// It allows rules to be triggered manually via API with optional user-provided data.
type ManualTriggerProvider struct {
	repo            RuleRepositoryInterface
	executor        RuleExecutorInterface
	subscriptionSvc SubscriptionLimitChecker
}

// NewManualTriggerProvider creates a new ManualTriggerProvider.
func NewManualTriggerProvider(
	repo RuleRepositoryInterface,
	executor RuleExecutorInterface,
	subscriptionSvc SubscriptionLimitChecker,
) *ManualTriggerProvider {
	return &ManualTriggerProvider{
		repo:            repo,
		executor:        executor,
		subscriptionSvc: subscriptionSvc,
	}
}

// Name returns the unique identifier for this trigger provider.
func (p *ManualTriggerProvider) Name() string {
	return "manual"
}

// SupportsManualTrigger returns true as this provider exists for manual triggering.
func (p *ManualTriggerProvider) SupportsManualTrigger() bool {
	return true
}

// Start is a no-op for manual provider (no background work needed).
func (p *ManualTriggerProvider) Start(ctx context.Context) error {
	log.Printf("[ManualTriggerProvider] Started")
	return nil
}

// Stop is a no-op for manual provider (nothing to stop).
func (p *ManualTriggerProvider) Stop(ctx context.Context) error {
	log.Printf("[ManualTriggerProvider] Stopped")
	return nil
}

// TriggerRule executes a rule on-demand with optional user-provided data.
// The triggerData is merged with system-generated metadata before execution.
func (p *ManualTriggerProvider) TriggerRule(ctx context.Context, ruleID string, triggerData map[string]interface{}) (*domain.RuleExecution, error) {
	// Check context first
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}

	// Fetch the rule
	rule, err := p.repo.GetByID(ctx, ruleID)
	if err != nil {
		return nil, err
	}

	// Verify rule is enabled
	if !rule.Enabled {
		return nil, ErrRuleDisabled
	}

	// Check daily execution limit
	allowed, remaining, err := p.subscriptionSvc.CheckDailyExecutionLimit(ctx, rule.UserID)
	if err != nil {
		log.Printf("[ManualTriggerProvider] Error checking execution limit for user %s: %v", rule.UserID, err)
		return nil, err
	}

	if !allowed {
		log.Printf("[ManualTriggerProvider] User %s has exceeded daily execution limit", rule.UserID)
		return nil, ErrExecutionLimitExceeded
	}

	log.Printf("[ManualTriggerProvider] Triggering rule %s for user %s (remaining: %d)", ruleID, rule.UserID, remaining)

	// Increment daily executions
	if err := p.subscriptionSvc.IncrementDailyExecutions(ctx, rule.UserID); err != nil {
		log.Printf("[ManualTriggerProvider] Error incrementing daily executions for user %s: %v", rule.UserID, err)
		// Continue with execution anyway
	}

	// Build trigger data by merging user data with system metadata
	mergedTriggerData := p.buildTriggerData(ruleID, rule.UserID, triggerData)

	// Execute the rule
	execution, err := p.executor.ExecuteRule(ctx, rule, mergedTriggerData)
	if err != nil {
		log.Printf("[ManualTriggerProvider] Error executing rule %s: %v", ruleID, err)
		// Update execution count even on failure
		_ = p.repo.IncrementExecutionCount(ctx, ruleID)
		return nil, err
	}

	// Update execution statistics
	if err := p.repo.IncrementExecutionCount(ctx, ruleID); err != nil {
		log.Printf("[ManualTriggerProvider] Error updating execution count for rule %s: %v", ruleID, err)
	}

	if execution.Status == domain.ExecutionStatusCompleted {
		if err := p.repo.UpdateLastSuccessful(ctx, ruleID); err != nil {
			log.Printf("[ManualTriggerProvider] Error updating last successful for rule %s: %v", ruleID, err)
		}
	}

	log.Printf("[ManualTriggerProvider] Rule %s execution completed with status: %s", ruleID, execution.Status)
	return execution, nil
}

// buildTriggerData merges user-provided data with system-generated metadata.
func (p *ManualTriggerProvider) buildTriggerData(ruleID, userID string, userData map[string]interface{}) map[string]interface{} {
	// Start with user data if provided, otherwise empty map
	result := make(map[string]interface{})
	for k, v := range userData {
		result[k] = v
	}

	// Add system metadata (these can overwrite user data for consistency)
	result["trigger_type"] = "manual"
	result["triggered_at"] = time.Now().UTC()
	result["rule_id"] = ruleID
	result["user_id"] = userID

	return result
}
