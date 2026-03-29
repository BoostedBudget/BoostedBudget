// Package repository provides data access for automation rules engine.
// Issue #35: Rules Engine Core - separate from categorization rules (Issue #346).
package repository

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
)

// AutomationRulesRepository defines the interface for automation rule data access.
type AutomationRulesRepository interface {
	// Create creates a new automation rule in the database.
	Create(ctx context.Context, rule *domain.AutomationRule) error

	// GetByID retrieves a rule by its ID.
	GetByID(ctx context.Context, id string) (*domain.AutomationRule, error)

	// GetByUserID retrieves all rules for a specific user, ordered by priority (desc).
	GetByUserID(ctx context.Context, userID string) ([]*domain.AutomationRule, error)

	// GetEnabledByUserID retrieves all enabled rules for a user, ordered by priority (desc).
	GetEnabledByUserID(ctx context.Context, userID string) ([]*domain.AutomationRule, error)

	// GetByName retrieves a rule by user ID and name.
	GetByName(ctx context.Context, userID, name string) (*domain.AutomationRule, error)

	// Update updates an existing rule.
	Update(ctx context.Context, rule *domain.AutomationRule) error

	// Delete removes a rule from the database.
	Delete(ctx context.Context, id string) error

	// CountByUserID counts the total number of automation rules for a user.
	CountByUserID(ctx context.Context, userID string) (int, error)

	// IncrementExecutionCount increments the execution count and updates last_executed_at.
	IncrementExecutionCount(ctx context.Context, ruleID string) error

	// UpdateLastSuccessful updates the last_successful_at timestamp.
	UpdateLastSuccessful(ctx context.Context, ruleID string) error

	// GetRulesByEventTrigger retrieves enabled rules that trigger on a specific event type.
	GetRulesByEventTrigger(ctx context.Context, userID, eventType string) ([]*domain.AutomationRule, error)

	// GetRulesByType retrieves enabled rules of a specific type.
	GetRulesByType(ctx context.Context, userID string, ruleType domain.RuleType) ([]*domain.AutomationRule, error)

	// GetRulesByScheduleTrigger retrieves all enabled rules with schedule triggers.
	// Used by CronTriggerProvider to load all scheduled rules on startup and refresh.
	// Issue #37: Rule Scheduler
	GetRulesByScheduleTrigger(ctx context.Context) ([]*domain.AutomationRule, error)
}

// automationRulesRepository implements AutomationRulesRepository using PostgreSQL.
type automationRulesRepository struct {
	db DB
}

// NewAutomationRulesRepository creates a new AutomationRulesRepository instance.
func NewAutomationRulesRepository(db DB) AutomationRulesRepository {
	return &automationRulesRepository{db: db}
}

// Create creates a new automation rule in the database.
func (r *automationRulesRepository) Create(ctx context.Context, rule *domain.AutomationRule) error {
	// Generate UUID if not already set
	if rule.ID == "" {
		rule.ID = uuid.New().String()
	}

	// Set timestamps
	now := time.Now()
	rule.CreatedAt = now
	rule.UpdatedAt = now

	// Set defaults
	if rule.ConditionLogic == "" {
		rule.ConditionLogic = "AND"
	}

	// Marshal JSON fields
	triggerJSON, err := json.Marshal(rule.TriggerConfig)
	if err != nil {
		return err
	}

	conditionsJSON, err := json.Marshal(rule.Conditions)
	if err != nil {
		return err
	}

	query := `
		INSERT INTO rules (
			id, user_id, name, description, enabled, priority,
			rule_type, trigger_config, conditions, condition_logic,
			actions, execution_count, last_executed_at, last_successful_at,
			created_at, updated_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16)
	`

	_, err = r.db.Exec(ctx, query,
		rule.ID,
		rule.UserID,
		rule.Name,
		rule.Description,
		rule.Enabled,
		rule.Priority,
		rule.RuleType,
		triggerJSON,
		conditionsJSON,
		rule.ConditionLogic,
		rule.Actions,
		rule.ExecutionCount,
		rule.LastExecutedAt,
		rule.LastSuccessfulAt,
		rule.CreatedAt,
		rule.UpdatedAt,
	)

	if err != nil {
		// Check for unique constraint violations
		if strings.Contains(err.Error(), "unique_automation_rule_name_per_user") ||
			strings.Contains(err.Error(), "duplicate key") {
			return domain.ErrAutomationRuleAlreadyExists
		}
		return err
	}

	return nil
}

// GetByID retrieves a rule by its ID.
func (r *automationRulesRepository) GetByID(ctx context.Context, id string) (*domain.AutomationRule, error) {
	query := `
		SELECT id, user_id, name, description, enabled, priority,
			rule_type, trigger_config, conditions, condition_logic,
			actions, execution_count, last_executed_at, last_successful_at,
			created_at, updated_at
		FROM rules
		WHERE id = $1
	`

	return r.scanRule(r.db.QueryRow(ctx, query, id))
}

// GetByUserID retrieves all rules for a specific user, ordered by priority (desc).
func (r *automationRulesRepository) GetByUserID(ctx context.Context, userID string) ([]*domain.AutomationRule, error) {
	query := `
		SELECT id, user_id, name, description, enabled, priority,
			rule_type, trigger_config, conditions, condition_logic,
			actions, execution_count, last_executed_at, last_successful_at,
			created_at, updated_at
		FROM rules
		WHERE user_id = $1
		ORDER BY priority DESC, created_at ASC
	`

	return r.queryRules(ctx, query, userID)
}

// GetEnabledByUserID retrieves all enabled rules for a user, ordered by priority (desc).
func (r *automationRulesRepository) GetEnabledByUserID(ctx context.Context, userID string) ([]*domain.AutomationRule, error) {
	query := `
		SELECT id, user_id, name, description, enabled, priority,
			rule_type, trigger_config, conditions, condition_logic,
			actions, execution_count, last_executed_at, last_successful_at,
			created_at, updated_at
		FROM rules
		WHERE user_id = $1 AND enabled = true
		ORDER BY priority DESC, created_at ASC
	`

	return r.queryRules(ctx, query, userID)
}

// GetByName retrieves a rule by user ID and name.
func (r *automationRulesRepository) GetByName(ctx context.Context, userID, name string) (*domain.AutomationRule, error) {
	query := `
		SELECT id, user_id, name, description, enabled, priority,
			rule_type, trigger_config, conditions, condition_logic,
			actions, execution_count, last_executed_at, last_successful_at,
			created_at, updated_at
		FROM rules
		WHERE user_id = $1 AND name = $2
	`

	return r.scanRule(r.db.QueryRow(ctx, query, userID, name))
}

// Update updates an existing rule.
func (r *automationRulesRepository) Update(ctx context.Context, rule *domain.AutomationRule) error {
	rule.UpdatedAt = time.Now()

	// Marshal JSON fields
	triggerJSON, err := json.Marshal(rule.TriggerConfig)
	if err != nil {
		return err
	}

	conditionsJSON, err := json.Marshal(rule.Conditions)
	if err != nil {
		return err
	}

	query := `
		UPDATE rules
		SET name = $1, description = $2, enabled = $3, priority = $4,
			trigger_config = $5, conditions = $6, condition_logic = $7,
			actions = $8, updated_at = $9
		WHERE id = $10
	`

	ct, err := r.db.Exec(ctx, query,
		rule.Name,
		rule.Description,
		rule.Enabled,
		rule.Priority,
		triggerJSON,
		conditionsJSON,
		rule.ConditionLogic,
		rule.Actions,
		rule.UpdatedAt,
		rule.ID,
	)
	if err != nil {
		// Check for unique constraint violations
		if strings.Contains(err.Error(), "unique_automation_rule_name_per_user") ||
			strings.Contains(err.Error(), "duplicate key") {
			return domain.ErrAutomationRuleAlreadyExists
		}
		return err
	}

	if ct.RowsAffected() == 0 {
		return domain.ErrAutomationRuleNotFound
	}

	return nil
}

// Delete removes a rule from the database.
func (r *automationRulesRepository) Delete(ctx context.Context, id string) error {
	query := `DELETE FROM rules WHERE id = $1`

	ct, err := r.db.Exec(ctx, query, id)
	if err != nil {
		return err
	}

	if ct.RowsAffected() == 0 {
		return domain.ErrAutomationRuleNotFound
	}

	return nil
}

// CountByUserID counts the total number of automation rules for a user.
func (r *automationRulesRepository) CountByUserID(ctx context.Context, userID string) (int, error) {
	query := `SELECT COUNT(*) FROM rules WHERE user_id = $1`

	var count int
	err := r.db.QueryRow(ctx, query, userID).Scan(&count)
	if err != nil {
		return 0, err
	}

	return count, nil
}

// IncrementExecutionCount increments the execution count and updates last_executed_at.
func (r *automationRulesRepository) IncrementExecutionCount(ctx context.Context, ruleID string) error {
	query := `
		UPDATE rules
		SET execution_count = execution_count + 1, last_executed_at = $1, updated_at = $1
		WHERE id = $2
	`

	ct, err := r.db.Exec(ctx, query, time.Now(), ruleID)
	if err != nil {
		return err
	}

	if ct.RowsAffected() == 0 {
		return domain.ErrAutomationRuleNotFound
	}

	return nil
}

// UpdateLastSuccessful updates the last_successful_at timestamp.
func (r *automationRulesRepository) UpdateLastSuccessful(ctx context.Context, ruleID string) error {
	query := `
		UPDATE rules
		SET last_successful_at = $1, updated_at = $1
		WHERE id = $2
	`

	ct, err := r.db.Exec(ctx, query, time.Now(), ruleID)
	if err != nil {
		return err
	}

	if ct.RowsAffected() == 0 {
		return domain.ErrAutomationRuleNotFound
	}

	return nil
}

// GetRulesByEventTrigger retrieves enabled rules that trigger on a specific event type.
// Uses JSONB containment to match trigger_config.event field.
func (r *automationRulesRepository) GetRulesByEventTrigger(ctx context.Context, userID, eventType string) ([]*domain.AutomationRule, error) {
	query := `
		SELECT id, user_id, name, description, enabled, priority,
			rule_type, trigger_config, conditions, condition_logic,
			actions, execution_count, last_executed_at, last_successful_at,
			created_at, updated_at
		FROM rules
		WHERE user_id = $1
			AND enabled = true
			AND trigger_config->>'type' = 'event'
			AND trigger_config->>'event' = $2
		ORDER BY priority DESC, created_at ASC
	`

	return r.queryRules(ctx, query, userID, eventType)
}

// GetRulesByType retrieves enabled rules of a specific type.
func (r *automationRulesRepository) GetRulesByType(ctx context.Context, userID string, ruleType domain.RuleType) ([]*domain.AutomationRule, error) {
	query := `
		SELECT id, user_id, name, description, enabled, priority,
			rule_type, trigger_config, conditions, condition_logic,
			actions, execution_count, last_executed_at, last_successful_at,
			created_at, updated_at
		FROM rules
		WHERE user_id = $1 AND enabled = true AND rule_type = $2
		ORDER BY priority DESC, created_at ASC
	`

	return r.queryRules(ctx, query, userID, ruleType)
}

// GetRulesByScheduleTrigger retrieves all enabled rules with schedule triggers.
// Issue #37: Rule Scheduler - used by CronTriggerProvider.
func (r *automationRulesRepository) GetRulesByScheduleTrigger(ctx context.Context) ([]*domain.AutomationRule, error) {
	query := `
		SELECT id, user_id, name, description, enabled, priority,
			rule_type, trigger_config, conditions, condition_logic,
			actions, execution_count, last_executed_at, last_successful_at,
			created_at, updated_at
		FROM rules
		WHERE enabled = true
			AND trigger_config->>'type' = 'schedule'
			AND trigger_config->>'cron' IS NOT NULL
		ORDER BY priority DESC, created_at ASC
	`

	return r.queryRules(ctx, query)
}

// scanRule scans a single rule from a row.
func (r *automationRulesRepository) scanRule(row pgx.Row) (*domain.AutomationRule, error) {
	rule := &domain.AutomationRule{}
	var triggerJSON, conditionsJSON []byte

	err := row.Scan(
		&rule.ID,
		&rule.UserID,
		&rule.Name,
		&rule.Description,
		&rule.Enabled,
		&rule.Priority,
		&rule.RuleType,
		&triggerJSON,
		&conditionsJSON,
		&rule.ConditionLogic,
		&rule.Actions,
		&rule.ExecutionCount,
		&rule.LastExecutedAt,
		&rule.LastSuccessfulAt,
		&rule.CreatedAt,
		&rule.UpdatedAt,
	)

	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrAutomationRuleNotFound
		}
		return nil, err
	}

	// Unmarshal JSON fields
	if err := json.Unmarshal(triggerJSON, &rule.TriggerConfig); err != nil {
		return nil, err
	}
	if err := json.Unmarshal(conditionsJSON, &rule.Conditions); err != nil {
		return nil, err
	}

	return rule, nil
}

// queryRules is a helper to query and scan multiple rules.
func (r *automationRulesRepository) queryRules(ctx context.Context, query string, args ...interface{}) ([]*domain.AutomationRule, error) {
	rows, err := r.db.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var rules []*domain.AutomationRule
	for rows.Next() {
		rule := &domain.AutomationRule{}
		var triggerJSON, conditionsJSON []byte

		err := rows.Scan(
			&rule.ID,
			&rule.UserID,
			&rule.Name,
			&rule.Description,
			&rule.Enabled,
			&rule.Priority,
			&rule.RuleType,
			&triggerJSON,
			&conditionsJSON,
			&rule.ConditionLogic,
			&rule.Actions,
			&rule.ExecutionCount,
			&rule.LastExecutedAt,
			&rule.LastSuccessfulAt,
			&rule.CreatedAt,
			&rule.UpdatedAt,
		)
		if err != nil {
			return nil, err
		}

		// Unmarshal JSON fields
		if err := json.Unmarshal(triggerJSON, &rule.TriggerConfig); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(conditionsJSON, &rule.Conditions); err != nil {
			return nil, err
		}

		rules = append(rules, rule)
	}

	if err = rows.Err(); err != nil {
		return nil, err
	}

	return rules, nil
}
