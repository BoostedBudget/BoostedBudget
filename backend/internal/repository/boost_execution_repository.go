// Package repository provides data access for boost execution tracking.
// Issue #35: Boosts Engine Core - execution history and auditing.
package repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
)

// BoostExecutionRepository defines the interface for boost execution data access.
type BoostExecutionRepository interface {
	// Create creates a new boost execution record.
	Create(ctx context.Context, execution *domain.BoostExecution) error

	// Update updates an existing execution record.
	Update(ctx context.Context, execution *domain.BoostExecution) error

	// GetByID retrieves an execution by its ID.
	GetByID(ctx context.Context, id string) (*domain.BoostExecution, error)

	// GetByBoostID retrieves executions for a specific boost with pagination.
	GetByBoostID(ctx context.Context, boostID string, limit, offset int) ([]*domain.BoostExecution, error)

	// GetByBoostIDWithFilter retrieves executions for a boost with optional status filter and total count.
	// Issue #38: Boosts API Endpoints - Execution history with filtering.
	GetByBoostIDWithFilter(ctx context.Context, boostID string, status *domain.ExecutionStatus, limit, offset int) ([]*domain.BoostExecution, int, error)

	// GetLastExecutionForBoost retrieves the most recent execution for a boost.
	GetLastExecutionForBoost(ctx context.Context, boostID string) (*domain.BoostExecution, error)

	// GetPendingExecutions retrieves pending executions for processing queue.
	GetPendingExecutions(ctx context.Context, limit int) ([]*domain.BoostExecution, error)

	// GetFailedExecutions retrieves failed executions for retry.
	GetFailedExecutions(ctx context.Context, boostID string, limit int) ([]*domain.BoostExecution, error)

	// GetByUserID retrieves a user's whole automation history, with an optional status filter and
	// a total count, keyed on the execution row's OWN user_id rather than on its boost.
	//
	// Issue #1569 Task 14. Every other read here is keyed on `boost_id = $1`, so once
	// fk_boost_executions_boost_id became ON DELETE SET NULL an execution whose boost had been
	// purged would have been physically retained and invisible to every list in the app --
	// retention that reads as deletion. This is the reader that can still see it.
	GetByUserID(ctx context.Context, userID string, status *domain.ExecutionStatus, limit, offset int) ([]*domain.BoostExecution, int, error)
}

// boostExecutionRepository implements BoostExecutionRepository using PostgreSQL.
type boostExecutionRepository struct {
	db DB
}

// NewBoostExecutionRepository creates a new BoostExecutionRepository instance.
func NewBoostExecutionRepository(db DB) BoostExecutionRepository {
	return &boostExecutionRepository{db: db}
}

// Create creates a new boost execution record.
func (r *boostExecutionRepository) Create(ctx context.Context, execution *domain.BoostExecution) error {
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

	// nil (not []byte{}) so the column is SQL NULL rather than invalid JSON when unset.
	var definitionSnapshot []byte
	if len(execution.DefinitionSnapshot) > 0 {
		definitionSnapshot = execution.DefinitionSnapshot
	}

	// user_id, boost_name and definition_snapshot are written ONCE, here, from the definition as
	// it stands at execution time. Update() never touches them: the point of a snapshot is that a
	// later rename, edit or purge of the boost does not rewrite what history says happened.
	// Issue #1569 Task 14.
	query := `
		INSERT INTO boost_executions (
			id, boost_id, user_id, boost_name, definition_snapshot, status, started_at, completed_at,
			execution_duration_ms, trigger_context, input_data, output_data,
			error_code, error_message, retry_count, max_retries
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16)
	`

	_, err := r.db.Exec(ctx, query,
		execution.ID,
		execution.BoostID,
		execution.UserID,
		execution.BoostName,
		definitionSnapshot,
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
func (r *boostExecutionRepository) Update(ctx context.Context, execution *domain.BoostExecution) error {
	query := `
		UPDATE boost_executions
		SET status = $1, completed_at = $2, execution_duration_ms = $3,
			output_data = $4, error_code = $5, error_message = $6, retry_count = $7
		WHERE id = $8
	`

	ct, err := r.db.Exec(ctx, query,
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

	if ct.RowsAffected() == 0 {
		return pgx.ErrNoRows
	}

	return nil
}

// GetByID retrieves an execution by its ID.
func (r *boostExecutionRepository) GetByID(ctx context.Context, id string) (*domain.BoostExecution, error) {
	query := `
		SELECT id, boost_id, user_id, boost_name, definition_snapshot, status, started_at, completed_at,
			execution_duration_ms, trigger_context, input_data, output_data,
			error_code, error_message, retry_count, max_retries
		FROM boost_executions
		WHERE id = $1
	`

	return r.scanExecution(r.db.QueryRow(ctx, query, id))
}

// GetByBoostID retrieves executions for a specific boost with pagination.
func (r *boostExecutionRepository) GetByBoostID(ctx context.Context, boostID string, limit, offset int) ([]*domain.BoostExecution, error) {
	query := `
		SELECT id, boost_id, user_id, boost_name, definition_snapshot, status, started_at, completed_at,
			execution_duration_ms, trigger_context, input_data, output_data,
			error_code, error_message, retry_count, max_retries
		FROM boost_executions
		WHERE boost_id = $1
		ORDER BY started_at DESC
		LIMIT $2 OFFSET $3
	`

	return r.queryExecutions(ctx, query, boostID, limit, offset)
}

// GetByBoostIDWithFilter retrieves executions for a boost with optional status filter and total count.
// Issue #38: Boosts API Endpoints - Execution history with filtering.
func (r *boostExecutionRepository) GetByBoostIDWithFilter(ctx context.Context, boostID string, status *domain.ExecutionStatus, limit, offset int) ([]*domain.BoostExecution, int, error) {
	// Build count query
	countQuery := `SELECT COUNT(*) FROM boost_executions WHERE boost_id = $1`
	countArgs := []interface{}{boostID}

	// Build data query
	dataQuery := `
		SELECT id, boost_id, user_id, boost_name, definition_snapshot, status, started_at, completed_at,
			execution_duration_ms, trigger_context, input_data, output_data,
			error_code, error_message, retry_count, max_retries
		FROM boost_executions
		WHERE boost_id = $1`
	dataArgs := []interface{}{boostID}

	// Add status filter if provided
	if status != nil {
		countQuery += ` AND status = $2`
		countArgs = append(countArgs, *status)
		dataQuery += ` AND status = $2`
		dataArgs = append(dataArgs, *status)
	}

	// Get total count
	var total int
	err := r.db.QueryRow(ctx, countQuery, countArgs...).Scan(&total)
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

// GetLastExecutionForBoost retrieves the most recent execution for a rule.
func (r *boostExecutionRepository) GetLastExecutionForBoost(ctx context.Context, boostID string) (*domain.BoostExecution, error) {
	query := `
		SELECT id, boost_id, user_id, boost_name, definition_snapshot, status, started_at, completed_at,
			execution_duration_ms, trigger_context, input_data, output_data,
			error_code, error_message, retry_count, max_retries
		FROM boost_executions
		WHERE boost_id = $1
		ORDER BY started_at DESC
		LIMIT 1
	`

	return r.scanExecution(r.db.QueryRow(ctx, query, boostID))
}

// GetPendingExecutions retrieves pending executions for processing queue.
func (r *boostExecutionRepository) GetPendingExecutions(ctx context.Context, limit int) ([]*domain.BoostExecution, error) {
	query := `
		SELECT id, boost_id, user_id, boost_name, definition_snapshot, status, started_at, completed_at,
			execution_duration_ms, trigger_context, input_data, output_data,
			error_code, error_message, retry_count, max_retries
		FROM boost_executions
		WHERE status = 'pending'
		ORDER BY started_at ASC
		LIMIT $1
	`

	return r.queryExecutions(ctx, query, limit)
}

// GetFailedExecutions retrieves failed executions for retry.
func (r *boostExecutionRepository) GetFailedExecutions(ctx context.Context, boostID string, limit int) ([]*domain.BoostExecution, error) {
	query := `
		SELECT id, boost_id, user_id, boost_name, definition_snapshot, status, started_at, completed_at,
			execution_duration_ms, trigger_context, input_data, output_data,
			error_code, error_message, retry_count, max_retries
		FROM boost_executions
		WHERE boost_id = $1 AND status = 'failed' AND retry_count < max_retries
		ORDER BY started_at DESC
		LIMIT $2
	`

	return r.queryExecutions(ctx, query, boostID, limit)
}

// GetByUserID retrieves every execution belonging to a user, orphans included.
//
// Issue #1569 Task 14: this is the only read of boost_executions not keyed on boost_id, and so the
// only one that can return a row whose boost has been purged. BoostName/DefinitionSnapshot are what
// a caller shows in place of the missing parent.
func (r *boostExecutionRepository) GetByUserID(
	ctx context.Context,
	userID string,
	status *domain.ExecutionStatus,
	limit, offset int,
) ([]*domain.BoostExecution, int, error) {
	countQuery := `SELECT COUNT(*) FROM boost_executions WHERE user_id = $1`
	countArgs := []interface{}{userID}

	dataQuery := `
		SELECT id, boost_id, user_id, boost_name, definition_snapshot, status, started_at, completed_at,
			execution_duration_ms, trigger_context, input_data, output_data,
			error_code, error_message, retry_count, max_retries
		FROM boost_executions
		WHERE user_id = $1`
	dataArgs := []interface{}{userID}

	if status != nil {
		countQuery += ` AND status = $2`
		countArgs = append(countArgs, *status)
		dataQuery += ` AND status = $2`
		dataArgs = append(dataArgs, *status)
	}

	var total int
	if err := r.db.QueryRow(ctx, countQuery, countArgs...).Scan(&total); err != nil {
		return nil, 0, err
	}

	nextArg := len(dataArgs) + 1
	dataQuery += fmt.Sprintf(` ORDER BY started_at DESC LIMIT $%d OFFSET $%d`, nextArg, nextArg+1)
	dataArgs = append(dataArgs, limit, offset)

	executions, err := r.queryExecutions(ctx, dataQuery, dataArgs...)
	if err != nil {
		return nil, 0, err
	}

	return executions, total, nil
}

// scanExecution scans a single execution from a row.
func (r *boostExecutionRepository) scanExecution(row pgx.Row) (*domain.BoostExecution, error) {
	execution := &domain.BoostExecution{}
	var triggerContext, inputData, outputData, definitionSnapshot []byte

	err := row.Scan(
		&execution.ID,
		&execution.BoostID,
		&execution.UserID,
		&execution.BoostName,
		&definitionSnapshot,
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
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, err
		}
		return nil, err
	}

	// Assign raw JSON fields
	execution.TriggerContext = json.RawMessage(triggerContext)
	if definitionSnapshot != nil {
		execution.DefinitionSnapshot = json.RawMessage(definitionSnapshot)
	}
	if inputData != nil {
		execution.InputData = json.RawMessage(inputData)
	}
	if outputData != nil {
		execution.OutputData = json.RawMessage(outputData)
	}

	return execution, nil
}

// queryExecutions is a helper to query and scan multiple executions.
func (r *boostExecutionRepository) queryExecutions(ctx context.Context, query string, args ...interface{}) ([]*domain.BoostExecution, error) {
	rows, err := r.db.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var executions []*domain.BoostExecution
	for rows.Next() {
		execution := &domain.BoostExecution{}
		var triggerContext, inputData, outputData, definitionSnapshot []byte

		err := rows.Scan(
			&execution.ID,
			&execution.BoostID,
			&execution.UserID,
			&execution.BoostName,
			&definitionSnapshot,
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
		if definitionSnapshot != nil {
			execution.DefinitionSnapshot = json.RawMessage(definitionSnapshot)
		}
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
