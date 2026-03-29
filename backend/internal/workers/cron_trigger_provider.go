// Package workers provides background job processing for automation rules.
// Issue #37: Rule Scheduler - CronTriggerProvider implementation.
package workers

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
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
	logger          *slog.Logger
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
	logger *slog.Logger,
) *CronTriggerProvider {
	if logger == nil {
		logger = slog.Default()
	}
	return &CronTriggerProvider{
		logger:          logger,
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
	logger *slog.Logger,
) *CronTriggerProvider {
	p := NewCronTriggerProvider(repo, executor, subscriptionSvc, refreshInterval, logger)
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

	p.logger.Info("cron trigger provider started", "scheduled_rules", len(p.scheduledRules))
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
		p.logger.Warn("stop timed out waiting for jobs")
	}

	p.started = false
	p.scheduledRules = make(map[string]*scheduledRule)

	p.logger.Info("cron trigger provider stopped")
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
			p.logger.Info("removed rule from schedule", "rule_id", ruleID)
		}
	}

	// Add or update rules
	for _, rule := range rules {
		cronExpr := rule.TriggerConfig.Cron

		// Validate cron expression (use appropriate parser based on mode)
		if err := p.validateCronExpression(cronExpr); err != nil {
			p.logger.Warn("skipping rule with invalid cron expression", "rule_id", rule.ID, "error", err)
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
			p.logger.Error("failed to schedule rule", "rule_id", rule.ID, "error", err)
			continue
		}

		p.scheduledRules[rule.ID] = &scheduledRule{
			ruleID:  rule.ID,
			userID:  rule.UserID,
			cron:    cronExpr,
			entryID: entryID,
		}

		p.logger.Info("scheduled rule", "rule_id", rule.ID, "cron", fullCronExpr)
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
			p.logger.Info("rule not found, skipping execution", "rule_id", ruleID)
			return
		}
		p.logger.Error("error fetching rule", "rule_id", ruleID, "error", err)
		return
	}

	// Check if rule is still enabled
	if !rule.Enabled {
		p.logger.Info("rule is disabled, skipping execution", "rule_id", ruleID)
		return
	}

	// Check daily execution limit
	allowed, remaining, err := p.subscriptionSvc.CheckDailyExecutionLimit(ctx, userID)
	if err != nil {
		p.logger.Error("error checking execution limit", "user_id", userID, "error", err)
		return
	}

	if !allowed {
		p.logger.Warn("user exceeded daily execution limit, skipping rule", "user_id", userID, "rule_id", ruleID)
		return
	}

	p.logger.Info("executing rule", "rule_id", ruleID, "user_id", userID, "remaining", remaining)

	// Increment daily executions
	if err := p.subscriptionSvc.IncrementDailyExecutions(ctx, userID); err != nil {
		p.logger.Error("error incrementing daily executions", "user_id", userID, "error", err)
		// Continue with execution anyway
	}

	// Execute the rule
	triggerData := map[string]interface{}{
		"trigger_type": "schedule",
		"triggered_at": time.Now().UTC(),
	}

	execution, err := p.executor.ExecuteRule(ctx, rule, triggerData)
	if err != nil {
		p.logger.Error("error executing rule", "rule_id", ruleID, "error", err)
		// Update execution count even on failure
		_ = p.repo.IncrementExecutionCount(ctx, ruleID)
		return
	}

	// Update execution statistics
	if err := p.repo.IncrementExecutionCount(ctx, ruleID); err != nil {
		p.logger.Error("error updating execution count", "rule_id", ruleID, "error", err)
	}

	if execution.Status == domain.ExecutionStatusCompleted {
		if err := p.repo.UpdateLastSuccessful(ctx, ruleID); err != nil {
			p.logger.Error("error updating last successful", "rule_id", ruleID, "error", err)
		}
	}

	p.logger.Info("rule execution completed", "rule_id", ruleID, "status", execution.Status)
}

// refreshLoop periodically refreshes the scheduled rules from the database.
func (p *CronTriggerProvider) refreshLoop(ctx context.Context) {
	ticker := time.NewTicker(p.refreshInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			if err := p.Refresh(ctx); err != nil {
				p.logger.Error("error refreshing rules", "error", err)
			}
		case <-p.stopCh:
			return
		case <-ctx.Done():
			return
		}
	}
}
