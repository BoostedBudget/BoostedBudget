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

// BoostExecutorInterface defines the interface for executing boosts.
// This abstraction allows the CronTriggerProvider to work with any executor implementation.
type BoostExecutorInterface interface {
	ExecuteBoost(ctx context.Context, boost *domain.Boost, triggerData map[string]interface{}) (*domain.BoostExecution, error)
}

// BoostRepositoryInterface defines the subset of repository methods needed by CronTriggerProvider.
type BoostRepositoryInterface interface {
	GetBoostsByScheduleTrigger(ctx context.Context) ([]*domain.Boost, error)
	GetByID(ctx context.Context, id string) (*domain.Boost, error)
	IncrementExecutionCount(ctx context.Context, boostID string) error
	UpdateLastSuccessful(ctx context.Context, boostID string) error
}

// scheduledBoost tracks a scheduled boost and its cron entry.
type scheduledBoost struct {
	boostID  string
	userID  string
	cron    string
	entryID cron.EntryID
}

// CronTriggerProvider implements RuleTriggerProvider using robfig/cron/v3.
// It schedules automation rules to run at specified cron intervals.
type CronTriggerProvider struct {
	logger          *slog.Logger
	repo            BoostRepositoryInterface
	executor        BoostExecutorInterface
	refreshInterval time.Duration

	cron           *cron.Cron
	scheduledBoosts map[string]*scheduledBoost // boostID -> scheduledBoost
	mu             sync.RWMutex
	started        bool
	stopCh         chan struct{}
	useSeconds     bool // For testing with second-level cron expressions
}

// NewCronTriggerProvider creates a new CronTriggerProvider.
func NewCronTriggerProvider(
	repo BoostRepositoryInterface,
	executor BoostExecutorInterface,
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
		refreshInterval: refreshInterval,
		scheduledBoosts:  make(map[string]*scheduledBoost),
		stopCh:          make(chan struct{}),
		useSeconds:      false,
	}
}

// NewCronTriggerProviderWithSeconds creates a CronTriggerProvider that supports
// second-level cron expressions. This is primarily for testing purposes.
func NewCronTriggerProviderWithSeconds(
	repo BoostRepositoryInterface,
	executor BoostExecutorInterface,
	refreshInterval time.Duration,
	logger *slog.Logger,
) *CronTriggerProvider {
	p := NewCronTriggerProvider(repo, executor, refreshInterval, logger)
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
func (p *CronTriggerProvider) TriggerRule(ctx context.Context, boostID string, triggerData map[string]interface{}) (*domain.BoostExecution, error) {
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

	p.logger.Info("cron trigger provider started", "scheduled_rules", len(p.scheduledBoosts))
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
	p.scheduledBoosts = make(map[string]*scheduledBoost)

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
	return len(p.scheduledBoosts)
}

// GetRuleCronExpression returns the cron expression for a scheduled rule.
func (p *CronTriggerProvider) GetRuleCronExpression(boostID string) string {
	p.mu.RLock()
	defer p.mu.RUnlock()

	if sr, ok := p.scheduledBoosts[boostID]; ok {
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
	rules, err := p.repo.GetBoostsByScheduleTrigger(ctx)
	if err != nil {
		return err
	}

	// Track which rules are in the new set
	newBoostIDs := make(map[string]bool)
	for _, rule := range rules {
		newBoostIDs[rule.ID] = true
	}

	// Remove boosts that no longer exist
	for boostID, sr := range p.scheduledBoosts {
		if !newBoostIDs[boostID] {
			p.cron.Remove(sr.entryID)
			delete(p.scheduledBoosts, boostID)
			p.logger.Info("removed boost from schedule", "boost_id", boostID)
		}
	}

	// Add or update boosts
	for _, rule := range rules {
		cronExpr := rule.TriggerConfig.Cron

		// Validate cron expression (use appropriate parser based on mode)
		if err := p.validateCronExpression(cronExpr); err != nil {
			p.logger.Warn("skipping boost with invalid cron expression", "boost_id", rule.ID, "error", err)
			continue
		}

		// Check if boost already exists with same cron
		if existing, ok := p.scheduledBoosts[rule.ID]; ok {
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

		// Schedule the boost
		entryID, err := p.cron.AddFunc(fullCronExpr, p.createJobFunc(rule.ID, rule.UserID))
		if err != nil {
			p.logger.Error("failed to schedule boost", "boost_id", rule.ID, "error", err)
			continue
		}

		p.scheduledBoosts[rule.ID] = &scheduledBoost{
			boostID:  rule.ID,
			userID:  rule.UserID,
			cron:    cronExpr,
			entryID: entryID,
		}

		p.logger.Info("scheduled boost", "boost_id", rule.ID, "cron", fullCronExpr)
	}

	return nil
}

// createJobFunc creates the job function for a scheduled boost.
func (p *CronTriggerProvider) createJobFunc(boostID, userID string) func() {
	return func() {
		ctx := context.Background()
		p.executeRule(ctx, boostID, userID)
	}
}

// executeRule executes a scheduled boost.
func (p *CronTriggerProvider) executeRule(ctx context.Context, boostID, userID string) {
	// Fetch the current boost state
	rule, err := p.repo.GetByID(ctx, boostID)
	if err != nil {
		if errors.Is(err, domain.ErrBoostNotFound) {
			p.logger.Info("boost not found, skipping execution", "boost_id", boostID)
			return
		}
		p.logger.Error("error fetching boost", "boost_id", boostID, "error", err)
		return
	}

	// Check if boost is still enabled
	if !rule.Enabled {
		p.logger.Info("boost is disabled, skipping execution", "boost_id", boostID)
		return
	}

	p.logger.Info("executing boost", "boost_id", boostID, "user_id", userID)

	// Execute the boost
	triggerData := map[string]interface{}{
		"trigger_type": "schedule",
		"triggered_at": time.Now().UTC(),
	}

	execution, err := p.executor.ExecuteBoost(ctx, rule, triggerData)
	if err != nil {
		p.logger.Error("error executing boost", "boost_id", boostID, "error", err)
		// Update execution count even on failure
		_ = p.repo.IncrementExecutionCount(ctx, boostID)
		return
	}

	// Update execution statistics
	if err := p.repo.IncrementExecutionCount(ctx, boostID); err != nil {
		p.logger.Error("error updating execution count", "boost_id", boostID, "error", err)
	}

	if execution.Status == domain.ExecutionStatusCompleted {
		if err := p.repo.UpdateLastSuccessful(ctx, boostID); err != nil {
			p.logger.Error("error updating last successful", "boost_id", boostID, "error", err)
		}
	}

	p.logger.Info("boost execution completed", "boost_id", boostID, "status", execution.Status)
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
