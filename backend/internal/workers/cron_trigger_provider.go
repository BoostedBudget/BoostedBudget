// Package workers provides background job processing for automation rules.
// Issue #37: Rule Scheduler - CronTriggerProvider implementation.
package workers

import (
	"context"
	"errors"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/robfig/cron/v3"
)

// ErrManualTriggerNotSupported is returned when TriggerRule is called on a provider
// that doesn't support manual triggering.
var ErrManualTriggerNotSupported = errors.New("manual triggering is not supported by this provider")

// RuleExecutorInterface defines the interface for executing automation rules.
// This abstraction allows the CronTriggerProvider to work with any executor implementation.
type RuleExecutorInterface interface {
	ExecuteRule(ctx context.Context, rule *domain.AutomationRule, triggerData map[string]interface{}) (*domain.RuleExecution, error)
}

// SubscriptionLimitChecker defines the interface for checking subscription limits.
type SubscriptionLimitChecker interface {
	CheckDailyExecutionLimit(ctx context.Context, userID string) (allowed bool, remaining int, err error)
	IncrementDailyExecutions(ctx context.Context, userID string) error
}

// RuleRepositoryInterface defines the subset of repository methods needed by CronTriggerProvider.
type RuleRepositoryInterface interface {
	GetRulesByScheduleTrigger(ctx context.Context) ([]*domain.AutomationRule, error)
	GetByID(ctx context.Context, id string) (*domain.AutomationRule, error)
	IncrementExecutionCount(ctx context.Context, ruleID string) error
	UpdateLastSuccessful(ctx context.Context, ruleID string) error
}

// scheduledRule tracks a scheduled rule and its cron entry.
type scheduledRule struct {
	ruleID  string
	userID  string
	cron    string
	entryID cron.EntryID
}

// CronTriggerProvider implements RuleTriggerProvider using robfig/cron/v3.
// It schedules automation rules to run at specified cron intervals.
type CronTriggerProvider struct {
	repo            RuleRepositoryInterface
	executor        RuleExecutorInterface
	subscriptionSvc SubscriptionLimitChecker
	refreshInterval time.Duration

	cron           *cron.Cron
	scheduledRules map[string]*scheduledRule // ruleID -> scheduledRule
	mu             sync.RWMutex
	started        bool
	stopCh         chan struct{}
	useSeconds     bool // For testing with second-level cron expressions
}

// NewCronTriggerProvider creates a new CronTriggerProvider.
func NewCronTriggerProvider(
	repo RuleRepositoryInterface,
	executor RuleExecutorInterface,
	subscriptionSvc SubscriptionLimitChecker,
	refreshInterval time.Duration,
) *CronTriggerProvider {
	return &CronTriggerProvider{
		repo:            repo,
		executor:        executor,
		subscriptionSvc: subscriptionSvc,
		refreshInterval: refreshInterval,
		scheduledRules:  make(map[string]*scheduledRule),
		stopCh:          make(chan struct{}),
		useSeconds:      false,
	}
}

// NewCronTriggerProviderWithSeconds creates a CronTriggerProvider that supports
// second-level cron expressions. This is primarily for testing purposes.
func NewCronTriggerProviderWithSeconds(
	repo RuleRepositoryInterface,
	executor RuleExecutorInterface,
	subscriptionSvc SubscriptionLimitChecker,
	refreshInterval time.Duration,
) *CronTriggerProvider {
	p := NewCronTriggerProvider(repo, executor, subscriptionSvc, refreshInterval)
	p.useSeconds = true
	return p
}

// Name returns the unique identifier for this trigger provider.
func (p *CronTriggerProvider) Name() string {
	return "cron"
}

// SupportsManualTrigger returns false as cron provider doesn't support on-demand triggering.
func (p *CronTriggerProvider) SupportsManualTrigger() bool {
	return false
}

// TriggerRule returns an error as cron provider doesn't support manual triggering.
func (p *CronTriggerProvider) TriggerRule(ctx context.Context, ruleID string, triggerData map[string]interface{}) (*domain.RuleExecution, error) {
	return nil, ErrManualTriggerNotSupported
}

// Start begins the cron scheduler and loads all scheduled rules from the database.
func (p *CronTriggerProvider) Start(ctx context.Context) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.started {
		return nil
	}

	// Create cron scheduler with options
	opts := []cron.Option{
		cron.WithLocation(time.UTC),
		cron.WithChain(
			cron.Recover(cron.DefaultLogger), // Panic recovery
		),
	}

	if p.useSeconds {
		opts = append(opts, cron.WithSeconds())
	}

	p.cron = cron.New(opts...)

	// Load initial rules
	if err := p.loadRulesLocked(ctx); err != nil {
		return fmt.Errorf("failed to load scheduled rules: %w", err)
	}

	// Start the cron scheduler
	p.cron.Start()
	p.started = true
	p.stopCh = make(chan struct{})

	// Start refresh goroutine
	go p.refreshLoop(ctx)

	log.Printf("[CronTriggerProvider] Started with %d scheduled rules", len(p.scheduledRules))
	return nil
}

// Stop gracefully stops the cron scheduler.
func (p *CronTriggerProvider) Stop(ctx context.Context) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if !p.started {
		return nil
	}

	// Signal refresh loop to stop
	select {
	case <-p.stopCh:
		// Already closed
	default:
		close(p.stopCh)
	}

	// Stop cron scheduler and wait for running jobs
	stopCtx := p.cron.Stop()
	select {
	case <-stopCtx.Done():
		// All jobs finished
	case <-ctx.Done():
		// Context timeout
		log.Printf("[CronTriggerProvider] Stop timed out waiting for jobs")
	}

	p.started = false
	p.scheduledRules = make(map[string]*scheduledRule)

	log.Printf("[CronTriggerProvider] Stopped")
	return nil
}

// Refresh reloads all scheduled rules from the database.
// This allows picking up new rules, removing deleted rules, and updating modified cron expressions.
func (p *CronTriggerProvider) Refresh(ctx context.Context) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if !p.started {
		return nil
	}

	return p.loadRulesLocked(ctx)
}

// ScheduledRuleCount returns the number of currently scheduled rules.
func (p *CronTriggerProvider) ScheduledRuleCount() int {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return len(p.scheduledRules)
}

// GetRuleCronExpression returns the cron expression for a scheduled rule.
func (p *CronTriggerProvider) GetRuleCronExpression(ruleID string) string {
	p.mu.RLock()
	defer p.mu.RUnlock()

	if sr, ok := p.scheduledRules[ruleID]; ok {
		return sr.cron
	}
	return ""
}

// validateCronExpression validates a cron expression using the appropriate parser.
// Uses 6-field parser (with seconds) when useSeconds is true, otherwise 5-field parser.
func (p *CronTriggerProvider) validateCronExpression(expr string) error {
	var parser cron.Parser
	if p.useSeconds {
		parser = cron.NewParser(cron.Second | cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow)
	} else {
		parser = cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow)
	}
	_, err := parser.Parse(expr)
	if err != nil {
		return fmt.Errorf("invalid cron expression '%s': %w", expr, err)
	}
	return nil
}

// loadRulesLocked loads rules from the database and schedules them.
// Must be called with p.mu held.
func (p *CronTriggerProvider) loadRulesLocked(ctx context.Context) error {
	rules, err := p.repo.GetRulesByScheduleTrigger(ctx)
	if err != nil {
		return err
	}

	// Track which rules are in the new set
	newRuleIDs := make(map[string]bool)
	for _, rule := range rules {
		newRuleIDs[rule.ID] = true
	}

	// Remove rules that no longer exist
	for ruleID, sr := range p.scheduledRules {
		if !newRuleIDs[ruleID] {
			p.cron.Remove(sr.entryID)
			delete(p.scheduledRules, ruleID)
			log.Printf("[CronTriggerProvider] Removed rule %s from schedule", ruleID)
		}
	}

	// Add or update rules
	for _, rule := range rules {
		cronExpr := rule.TriggerConfig.Cron

		// Validate cron expression (use appropriate parser based on mode)
		if err := p.validateCronExpression(cronExpr); err != nil {
			log.Printf("[CronTriggerProvider] Skipping rule %s: invalid cron expression: %v", rule.ID, err)
			continue
		}

		// Check if rule already exists with same cron
		if existing, ok := p.scheduledRules[rule.ID]; ok {
			if existing.cron == cronExpr {
				// No change needed
				continue
			}
			// Remove old entry before adding new one
			p.cron.Remove(existing.entryID)
		}

		// Build cron expression with timezone if specified
		fullCronExpr := cronExpr
		if rule.TriggerConfig.Timezone != "" {
			fullCronExpr = fmt.Sprintf("CRON_TZ=%s %s", rule.TriggerConfig.Timezone, cronExpr)
		}

		// Schedule the rule
		entryID, err := p.cron.AddFunc(fullCronExpr, p.createJobFunc(rule.ID, rule.UserID))
		if err != nil {
			log.Printf("[CronTriggerProvider] Failed to schedule rule %s: %v", rule.ID, err)
			continue
		}

		p.scheduledRules[rule.ID] = &scheduledRule{
			ruleID:  rule.ID,
			userID:  rule.UserID,
			cron:    cronExpr,
			entryID: entryID,
		}

		log.Printf("[CronTriggerProvider] Scheduled rule %s with cron '%s'", rule.ID, fullCronExpr)
	}

	return nil
}

// createJobFunc creates the job function for a scheduled rule.
func (p *CronTriggerProvider) createJobFunc(ruleID, userID string) func() {
	return func() {
		ctx := context.Background()
		p.executeRule(ctx, ruleID, userID)
	}
}

// executeRule executes a scheduled rule.
func (p *CronTriggerProvider) executeRule(ctx context.Context, ruleID, userID string) {
	// Fetch the current rule state
	rule, err := p.repo.GetByID(ctx, ruleID)
	if err != nil {
		if errors.Is(err, domain.ErrAutomationRuleNotFound) {
			log.Printf("[CronTriggerProvider] Rule %s not found, skipping execution", ruleID)
			return
		}
		log.Printf("[CronTriggerProvider] Error fetching rule %s: %v", ruleID, err)
		return
	}

	// Check if rule is still enabled
	if !rule.Enabled {
		log.Printf("[CronTriggerProvider] Rule %s is disabled, skipping execution", ruleID)
		return
	}

	// Check daily execution limit
	allowed, remaining, err := p.subscriptionSvc.CheckDailyExecutionLimit(ctx, userID)
	if err != nil {
		log.Printf("[CronTriggerProvider] Error checking execution limit for user %s: %v", userID, err)
		return
	}

	if !allowed {
		log.Printf("[CronTriggerProvider] User %s has exceeded daily execution limit, skipping rule %s", userID, ruleID)
		return
	}

	log.Printf("[CronTriggerProvider] Executing rule %s for user %s (remaining: %d)", ruleID, userID, remaining)

	// Increment daily executions
	if err := p.subscriptionSvc.IncrementDailyExecutions(ctx, userID); err != nil {
		log.Printf("[CronTriggerProvider] Error incrementing daily executions for user %s: %v", userID, err)
		// Continue with execution anyway
	}

	// Execute the rule
	triggerData := map[string]interface{}{
		"trigger_type": "schedule",
		"triggered_at": time.Now().UTC(),
	}

	execution, err := p.executor.ExecuteRule(ctx, rule, triggerData)
	if err != nil {
		log.Printf("[CronTriggerProvider] Error executing rule %s: %v", ruleID, err)
		// Update execution count even on failure
		_ = p.repo.IncrementExecutionCount(ctx, ruleID)
		return
	}

	// Update execution statistics
	if err := p.repo.IncrementExecutionCount(ctx, ruleID); err != nil {
		log.Printf("[CronTriggerProvider] Error updating execution count for rule %s: %v", ruleID, err)
	}

	if execution.Status == domain.ExecutionStatusCompleted {
		if err := p.repo.UpdateLastSuccessful(ctx, ruleID); err != nil {
			log.Printf("[CronTriggerProvider] Error updating last successful for rule %s: %v", ruleID, err)
		}
	}

	log.Printf("[CronTriggerProvider] Rule %s execution completed with status: %s", ruleID, execution.Status)
}

// refreshLoop periodically refreshes the scheduled rules from the database.
func (p *CronTriggerProvider) refreshLoop(ctx context.Context) {
	ticker := time.NewTicker(p.refreshInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			if err := p.Refresh(ctx); err != nil {
				log.Printf("[CronTriggerProvider] Error refreshing rules: %v", err)
			}
		case <-p.stopCh:
			return
		case <-ctx.Done():
			return
		}
	}
}
