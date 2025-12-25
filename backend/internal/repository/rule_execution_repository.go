// Package repository provides data access for rule execution tracking.
// Issue #35: Rules Engine Core - execution history and auditing.
package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
)

// RuleExecutionRepository defines the interface for rule execution data access.
type RuleExecutionRepository interface {
	// Create creates a new rule execution record.
	Create(ctx context.Context, execution *domain.RuleExecution) error

	// Update updates an existing execution record.
	Update(ctx context.Context, execution *domain.RuleExecution) error

	// GetByID retrieves an execution by its ID.
	GetByID(ctx context.Context, id string) (*domain.RuleExecution, error)

	// GetByRuleID retrieves executions for a specific rule with pagination.
	GetByRuleID(ctx context.Context, ruleID string, limit, offset int) ([]*domain.RuleExecution, error)

	// GetByRuleIDWithFilter retrieves executions for a rule with optional status filter and total count.
	// Issue #38: Rules API Endpoints - Execution history with filtering.
	GetByRuleIDWithFilter(ctx context.Context, ruleID string, status *domain.ExecutionStatus, limit, offset int) ([]*domain.RuleExecution, int, error)

	// GetTodayExecutionCount counts executions for a user today (since midnight UTC).
	GetTodayExecutionCount(ctx context.Context, userID string) (int, error)

	// GetLastExecutionForRule retrieves the most recent execution for a rule.
	GetLastExecutionForRule(ctx context.Context, ruleID string) (*domain.RuleExecution, error)

	// GetPendingExecutions retrieves pending executions for processing queue.
	GetPendingExecutions(ctx context.Context, limit int) ([]*domain.RuleExecution, error)

	// GetFailedExecutions retrieves failed executions for retry.
	GetFailedExecutions(ctx context.Context, ruleID string, limit int) ([]*domain.RuleExecution, error)
}

// ruleExecutionRepository implements RuleExecutionRepository using PostgreSQL.
type ruleExecutionRepository struct {
	db *sql.DB
}

// NewRuleExecutionRepository creates a new RuleExecutionRepository instance.
func NewRuleExecutionRepository(db *sql.DB) RuleExecutionRepository {
	return &ruleExecutionRepository{db: db}
}

// Create creates a new rule execution record.
func (r *ruleExecutionRepository) Create(ctx context.Context, execution *domain.RuleExecution) error {
	// Generate UUID if not already set
	if execution.ID == "" {
		execution.ID = uuid.New().String()
	}

	// Set default values
	if execution.Status == "" {
		execution.Status = domain.ExecutionStatusPending
	}
	if execution.StartedAt.IsZero() {
		execution.StartedAt = time.Now()
	}
	if execution.MaxRetries == 0 {
		execution.MaxRetries = 3
	}

	// Marshal JSON fields (handle nil cases)
	triggerContextJSON := []byte("{}")
	if execution.TriggerContext != nil {
		triggerContextJSON = execution.TriggerContext
	}

	query := `
		INSERT INTO rule_executions (
			id, rule_id, status, started_at, completed_at,
			execution_duration_ms, trigger_context, input_data, output_data,
			error_code, error_message, retry_count, max_retries
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)
	`

	_, err := r.db.ExecContext(ctx, query,
		execution.ID,
		execution.RuleID,
		execution.Status,
		execution.StartedAt,
		execution.CompletedAt,
		execution.ExecutionDurationMs,
		triggerContextJSON,
		execution.InputData,
		execution.OutputData,
		execution.ErrorCode,
		execution.ErrorMessage,
		execution.RetryCount,
		execution.MaxRetries,
	)

	return err
}

// Update updates an existing execution record.
func (r *ruleExecutionRepository) Update(ctx context.Context, execution *domain.RuleExecution) error {
	query := `
		UPDATE rule_executions
		SET status = $1, completed_at = $2, execution_duration_ms = $3,
			output_data = $4, error_code = $5, error_message = $6, retry_count = $7
		WHERE id = $8
	`

	result, err := r.db.ExecContext(ctx, query,
		execution.Status,
		execution.CompletedAt,
		execution.ExecutionDurationMs,
		execution.OutputData,
		execution.ErrorCode,
		execution.ErrorMessage,
		execution.RetryCount,
		execution.ID,
	)
	if err != nil {
		return err
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return err
	}

	if rowsAffected == 0 {
		return sql.ErrNoRows
	}

	return nil
}

// GetByID retrieves an execution by its ID.
func (r *ruleExecutionRepository) GetByID(ctx context.Context, id string) (*domain.RuleExecution, error) {
	query := `
		SELECT id, rule_id, status, started_at, completed_at,
			execution_duration_ms, trigger_context, input_data, output_data,
			error_code, error_message, retry_count, max_retries
		FROM rule_executions
		WHERE id = $1
	`

	return r.scanExecution(r.db.QueryRowContext(ctx, query, id))
}

// GetByRuleID retrieves executions for a specific rule with pagination.
func (r *ruleExecutionRepository) GetByRuleID(ctx context.Context, ruleID string, limit, offset int) ([]*domain.RuleExecution, error) {
	query := `
		SELECT id, rule_id, status, started_at, completed_at,
			execution_duration_ms, trigger_context, input_data, output_data,
			error_code, error_message, retry_count, max_retries
		FROM rule_executions
		WHERE rule_id = $1
		ORDER BY started_at DESC
		LIMIT $2 OFFSET $3
	`

	return r.queryExecutions(ctx, query, ruleID, limit, offset)
}

// GetByRuleIDWithFilter retrieves executions for a rule with optional status filter and total count.
// Issue #38: Rules API Endpoints - Execution history with filtering.
func (r *ruleExecutionRepository) GetByRuleIDWithFilter(ctx context.Context, ruleID string, status *domain.ExecutionStatus, limit, offset int) ([]*domain.RuleExecution, int, error) {
	// Build count query
	countQuery := `SELECT COUNT(*) FROM rule_executions WHERE rule_id = $1`
	countArgs := []interface{}{ruleID}

	// Build data query
	dataQuery := `
		SELECT id, rule_id, status, started_at, completed_at,
			execution_duration_ms, trigger_context, input_data, output_data,
			error_code, error_message, retry_count, max_retries
		FROM rule_executions
		WHERE rule_id = $1`
	dataArgs := []interface{}{ruleID}

	// Add status filter if provided
	if status != nil {
		countQuery += ` AND status = $2`
		countArgs = append(countArgs, *status)
		dataQuery += ` AND status = $2`
		dataArgs = append(dataArgs, *status)
	}

	// Get total count
	var total int
	err := r.db.QueryRowContext(ctx, countQuery, countArgs...).Scan(&total)
	if err != nil {
		return nil, 0, err
	}

	// Add ordering and pagination
	nextArg := len(dataArgs) + 1
	dataQuery += fmt.Sprintf(` ORDER BY started_at DESC LIMIT $%d OFFSET $%d`, nextArg, nextArg+1)
	dataArgs = append(dataArgs, limit, offset)

	// Get executions
	executions, err := r.queryExecutions(ctx, dataQuery, dataArgs...)
	if err != nil {
		return nil, 0, err
	}

	return executions, total, nil
}

// GetTodayExecutionCount counts executions for a user today (since midnight UTC).
// Joins with rules table to get user_id.
func (r *ruleExecutionRepository) GetTodayExecutionCount(ctx context.Context, userID string) (int, error) {
	query := `
		SELECT COUNT(*)
		FROM rule_executions re
		JOIN rules r ON re.rule_id = r.id
		WHERE r.user_id = $1
			AND re.started_at >= $2
			AND re.status IN ('completed', 'running')
	`

	// Get start of today in UTC
	now := time.Now().UTC()
	todayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)

	var count int
	err := r.db.QueryRowContext(ctx, query, userID, todayStart).Scan(&count)
	if err != nil {
		return 0, err
	}

	return count, nil
}

// GetLastExecutionForRule retrieves the most recent execution for a rule.
func (r *ruleExecutionRepository) GetLastExecutionForRule(ctx context.Context, ruleID string) (*domain.RuleExecution, error) {
	query := `
		SELECT id, rule_id, status, started_at, completed_at,
			execution_duration_ms, trigger_context, input_data, output_data,
			error_code, error_message, retry_count, max_retries
		FROM rule_executions
		WHERE rule_id = $1
		ORDER BY started_at DESC
		LIMIT 1
	`

	return r.scanExecution(r.db.QueryRowContext(ctx, query, ruleID))
}

// GetPendingExecutions retrieves pending executions for processing queue.
func (r *ruleExecutionRepository) GetPendingExecutions(ctx context.Context, limit int) ([]*domain.RuleExecution, error) {
	query := `
		SELECT id, rule_id, status, started_at, completed_at,
			execution_duration_ms, trigger_context, input_data, output_data,
			error_code, error_message, retry_count, max_retries
		FROM rule_executions
		WHERE status = 'pending'
		ORDER BY started_at ASC
		LIMIT $1
	`

	return r.queryExecutions(ctx, query, limit)
}

// GetFailedExecutions retrieves failed executions for retry.
func (r *ruleExecutionRepository) GetFailedExecutions(ctx context.Context, ruleID string, limit int) ([]*domain.RuleExecution, error) {
	query := `
		SELECT id, rule_id, status, started_at, completed_at,
			execution_duration_ms, trigger_context, input_data, output_data,
			error_code, error_message, retry_count, max_retries
		FROM rule_executions
		WHERE rule_id = $1 AND status = 'failed' AND retry_count < max_retries
		ORDER BY started_at DESC
		LIMIT $2
	`

	return r.queryExecutions(ctx, query, ruleID, limit)
}

// scanExecution scans a single execution from a row.
func (r *ruleExecutionRepository) scanExecution(row *sql.Row) (*domain.RuleExecution, error) {
	execution := &domain.RuleExecution{}
	var triggerContext, inputData, outputData []byte

	err := row.Scan(
		&execution.ID,
		&execution.RuleID,
		&execution.Status,
		&execution.StartedAt,
		&execution.CompletedAt,
		&execution.ExecutionDurationMs,
		&triggerContext,
		&inputData,
		&outputData,
		&execution.ErrorCode,
		&execution.ErrorMessage,
		&execution.RetryCount,
		&execution.MaxRetries,
	)

	if err != nil {
		if err == sql.ErrNoRows {
			return nil, err
		}
		return nil, err
	}

	// Assign raw JSON fields
	execution.TriggerContext = json.RawMessage(triggerContext)
	if inputData != nil {
		execution.InputData = json.RawMessage(inputData)
	}
	if outputData != nil {
		execution.OutputData = json.RawMessage(outputData)
	}

	return execution, nil
}

// queryExecutions is a helper to query and scan multiple executions.
func (r *ruleExecutionRepository) queryExecutions(ctx context.Context, query string, args ...interface{}) ([]*domain.RuleExecution, error) {
	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var executions []*domain.RuleExecution
	for rows.Next() {
		execution := &domain.RuleExecution{}
		var triggerContext, inputData, outputData []byte

		err := rows.Scan(
			&execution.ID,
			&execution.RuleID,
			&execution.Status,
			&execution.StartedAt,
			&execution.CompletedAt,
			&execution.ExecutionDurationMs,
			&triggerContext,
			&inputData,
			&outputData,
			&execution.ErrorCode,
			&execution.ErrorMessage,
			&execution.RetryCount,
			&execution.MaxRetries,
		)
		if err != nil {
			return nil, err
		}

		// Assign raw JSON fields
		execution.TriggerContext = json.RawMessage(triggerContext)
		if inputData != nil {
			execution.InputData = json.RawMessage(inputData)
		}
		if outputData != nil {
			execution.OutputData = json.RawMessage(outputData)
		}

		executions = append(executions, execution)
	}

	if err = rows.Err(); err != nil {
		return nil, err
	}

	return executions, nil
}
