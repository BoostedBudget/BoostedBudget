// Package repository provides data access for the boosts engine.
// Issue #35: Rules Engine Core - separate from categorization rules (Issue #346).
package repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
)

// BoostsRepository defines the interface for boost data access.
type BoostsRepository interface {
	// Create creates a new automation boost in the database.
	Create(ctx context.Context, boost *domain.Boost) error

	// GetByID retrieves a boost by its ID.
	GetByID(ctx context.Context, id string) (*domain.Boost, error)

	// GetByUserID retrieves all boosts for a specific user, ordered by priority (desc).
	GetByUserID(ctx context.Context, userID string) ([]*domain.Boost, error)

	// GetByUserIDFiltered retrieves boosts for a user optionally filtered by boost types.
	// When boostTypes is empty, returns all boosts (same as GetByUserID).
	GetByUserIDFiltered(ctx context.Context, userID string, boostTypes []domain.BoostType) ([]*domain.Boost, error)

	// GetEnabledByUserID retrieves all enabled boosts for a user, ordered by priority (desc).
	GetEnabledByUserID(ctx context.Context, userID string) ([]*domain.Boost, error)

	// GetByName retrieves a boost by user ID and name.
	GetByName(ctx context.Context, userID, name string) (*domain.Boost, error)

	// Update updates an existing boost.
	Update(ctx context.Context, boost *domain.Boost) error

	// Delete TOMBSTONES a boost: it sets deleted_at/deleted_reason/deleted_by instead of issuing
	// `DELETE FROM boosts`, so that boost_executions -- the automation audit trail, which
	// fk_boost_executions_boost_id would otherwise CASCADE away -- survives. Issue #1569, Task 8.
	//
	// userID is a NEW parameter. The old statement was `DELETE FROM boosts WHERE id = $1` with no
	// ownership predicate at all. That was not exploitable (boostService.DeleteBoost is the only
	// caller and does a GetByID-then-compare first, verified by grep), but it made the repository
	// method depend on a check it could not see, and every sibling repository -- savings goals,
	// budgets -- already scopes its own delete by user_id. Now this one does too.
	//
	// Runs in one transaction with a "boost.deleted" audit entry written via auditLog.LogInTx, so
	// the entry and the state change commit or roll back together. auditLog may be nil.
	//
	// Returns ErrBoostNotFound if the boost does not exist, belongs to another user, or is already
	// tombstoned.
	Delete(ctx context.Context, id, userID string, auditLog AuditLogRepository) error

	// CountByUserID counts the total number of automation boosts for a user.
	CountByUserID(ctx context.Context, userID string) (int, error)

	// IncrementExecutionCount increments the execution count and updates last_executed_at.
	IncrementExecutionCount(ctx context.Context, boostID string) error

	// UpdateLastSuccessful updates the last_successful_at timestamp.
	UpdateLastSuccessful(ctx context.Context, boostID string) error

	// GetBoostsByEventTrigger retrieves enabled boosts that trigger on a specific event type.
	GetBoostsByEventTrigger(ctx context.Context, userID, eventType string) ([]*domain.Boost, error)

	// GetBoostsByType retrieves enabled boosts of a specific type.
	GetBoostsByType(ctx context.Context, userID string, boostType domain.BoostType) ([]*domain.Boost, error)

	// GetBoostsByScheduleTrigger retrieves all enabled boosts with schedule triggers.
	// Used by CronTriggerProvider to load all scheduled boosts on startup and refresh.
	// Issue #37: Boost Scheduler
	GetBoostsByScheduleTrigger(ctx context.Context) ([]*domain.Boost, error)
}

// boostsRepository implements BoostsRepository using PostgreSQL.
type boostsRepository struct {
	db DB
}

// NewBoostsRepository creates a new BoostsRepository instance.
func NewBoostsRepository(db DB) BoostsRepository {
	return &boostsRepository{db: db}
}

// Create creates a new automation boost in the database.
func (r *boostsRepository) Create(ctx context.Context, boost *domain.Boost) error {
	// Generate UUID if not already set
	if boost.ID == "" {
		boost.ID = uuid.New().String()
	}

	// Set timestamps
	now := time.Now()
	boost.CreatedAt = now
	boost.UpdatedAt = now

	// Set defaults
	if boost.ConditionLogic == "" {
		boost.ConditionLogic = "AND"
	}

	// Marshal JSON fields
	triggerJSON, err := json.Marshal(boost.TriggerConfig)
	if err != nil {
		return err
	}

	conditionsJSON, err := json.Marshal(boost.Conditions)
	if err != nil {
		return err
	}

	query := `
		INSERT INTO boosts (
			id, user_id, name, description, enabled, priority,
			boost_type, trigger_config, conditions, condition_logic,
			actions, execution_count, last_executed_at, last_successful_at,
			created_at, updated_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16)
	`

	_, err = r.db.Exec(ctx, query,
		boost.ID,
		boost.UserID,
		boost.Name,
		boost.Description,
		boost.Enabled,
		boost.Priority,
		boost.BoostType,
		triggerJSON,
		conditionsJSON,
		boost.ConditionLogic,
		boost.Actions,
		boost.ExecutionCount,
		boost.LastExecutedAt,
		boost.LastSuccessfulAt,
		boost.CreatedAt,
		boost.UpdatedAt,
	)

	if err != nil {
		// Check for unique constraint violations
		if strings.Contains(err.Error(), "unique_boost_name_per_user") ||
			strings.Contains(err.Error(), "duplicate key") {
			return domain.ErrBoostAlreadyExists
		}
		return err
	}

	return nil
}

// GetByID retrieves a boost by its ID.
func (r *boostsRepository) GetByID(ctx context.Context, id string) (*domain.Boost, error) {
	query := `
		SELECT id, user_id, name, description, enabled, priority,
			boost_type, trigger_config, conditions, condition_logic,
			actions, execution_count, last_executed_at, last_successful_at,
			created_at, updated_at
		FROM boosts
		WHERE id = $1 AND deleted_at IS NULL
	`

	return r.scanBoost(r.db.QueryRow(ctx, query, id))
}

// GetByUserID retrieves all boosts for a specific user, ordered by priority (desc).
func (r *boostsRepository) GetByUserID(ctx context.Context, userID string) ([]*domain.Boost, error) {
	query := `
		SELECT id, user_id, name, description, enabled, priority,
			boost_type, trigger_config, conditions, condition_logic,
			actions, execution_count, last_executed_at, last_successful_at,
			created_at, updated_at
		FROM boosts
		WHERE user_id = $1 AND deleted_at IS NULL
		ORDER BY priority DESC, created_at ASC
	`

	return r.queryBoosts(ctx, query, userID)
}

// GetByUserIDFiltered retrieves boosts for a user with an optional boost-type filter.
// When boostTypes is empty, all boosts are returned (same as GetByUserID).
func (r *boostsRepository) GetByUserIDFiltered(ctx context.Context, userID string, boostTypes []domain.BoostType) ([]*domain.Boost, error) {
	if len(boostTypes) == 0 {
		return r.GetByUserID(ctx, userID)
	}

	// Convert to string slice for pgx text array binding.
	typeStrs := make([]string, len(boostTypes))
	for i, bt := range boostTypes {
		typeStrs[i] = string(bt)
	}


	query := `
		SELECT id, user_id, name, description, enabled, priority,
			boost_type, trigger_config, conditions, condition_logic,
			actions, execution_count, last_executed_at, last_successful_at,
			created_at, updated_at
		FROM boosts
		WHERE user_id = $1 AND boost_type = ANY($2::text[]) AND deleted_at IS NULL
		ORDER BY priority DESC, created_at ASC
	`

	return r.queryBoosts(ctx, query, userID, typeStrs)
}

// GetEnabledByUserID retrieves all enabled boosts for a user, ordered by priority (desc).
func (r *boostsRepository) GetEnabledByUserID(ctx context.Context, userID string) ([]*domain.Boost, error) {
	query := `
		SELECT id, user_id, name, description, enabled, priority,
			boost_type, trigger_config, conditions, condition_logic,
			actions, execution_count, last_executed_at, last_successful_at,
			created_at, updated_at
		FROM boosts
		WHERE user_id = $1 AND enabled = true AND deleted_at IS NULL
		ORDER BY priority DESC, created_at ASC
	`

	return r.queryBoosts(ctx, query, userID)
}

// GetByName retrieves a boost by user ID and name.
func (r *boostsRepository) GetByName(ctx context.Context, userID, name string) (*domain.Boost, error) {
	query := `
		SELECT id, user_id, name, description, enabled, priority,
			boost_type, trigger_config, conditions, condition_logic,
			actions, execution_count, last_executed_at, last_successful_at,
			created_at, updated_at
		FROM boosts
		WHERE user_id = $1 AND name = $2 AND deleted_at IS NULL
	`

	return r.scanBoost(r.db.QueryRow(ctx, query, userID, name))
}

// Update updates an existing boost.
func (r *boostsRepository) Update(ctx context.Context, boost *domain.Boost) error {
	boost.UpdatedAt = time.Now()

	// Marshal JSON fields
	triggerJSON, err := json.Marshal(boost.TriggerConfig)
	if err != nil {
		return err
	}

	conditionsJSON, err := json.Marshal(boost.Conditions)
	if err != nil {
		return err
	}

	query := `
		UPDATE boosts
		SET name = $1, description = $2, enabled = $3, priority = $4,
			trigger_config = $5, conditions = $6, condition_logic = $7,
			actions = $8, updated_at = $9
		WHERE id = $10
	`

	ct, err := r.db.Exec(ctx, query,
		boost.Name,
		boost.Description,
		boost.Enabled,
		boost.Priority,
		triggerJSON,
		conditionsJSON,
		boost.ConditionLogic,
		boost.Actions,
		boost.UpdatedAt,
		boost.ID,
	)
	if err != nil {
		// Check for unique constraint violations
		if strings.Contains(err.Error(), "unique_boost_name_per_user") ||
			strings.Contains(err.Error(), "duplicate key") {
			return domain.ErrBoostAlreadyExists
		}
		return err
	}

	if ct.RowsAffected() == 0 {
		return domain.ErrBoostNotFound
	}

	return nil
}

// Delete implements BoostsRepository.Delete — see that method's doc comment for why this is a
// tombstone rather than a DELETE, and why it now takes userID.
func (r *boostsRepository) Delete(ctx context.Context, id, userID string, auditLog AuditLogRepository) error {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx) //nolint:errcheck // rollback after a successful commit is a no-op; its error is not actionable

	ct, err := tx.Exec(ctx, `
		UPDATE boosts
		SET deleted_at = NOW(), deleted_reason = $3, deleted_by = $4, updated_at = NOW()
		WHERE id = $1 AND user_id = $2 AND deleted_at IS NULL
	`, id, userID, domain.SoftDeleteReasonUser, domain.SoftDeletedByUser)
	if err != nil {
		return fmt.Errorf("delete boost %s: %w", id, err)
	}
	if ct.RowsAffected() == 0 {
		return domain.ErrBoostNotFound
	}

	if auditLog != nil {
		if err := auditLog.LogInTx(ctx, tx, "boost.deleted", "boost", id); err != nil {
			return fmt.Errorf("delete boost %s: audit log: %w", id, err)
		}
	}

	return tx.Commit(ctx)
}

// CountByUserID counts the total number of automation boosts for a user.
func (r *boostsRepository) CountByUserID(ctx context.Context, userID string) (int, error) {
	query := `SELECT COUNT(*) FROM boosts WHERE user_id = $1 AND deleted_at IS NULL`

	var count int
	err := r.db.QueryRow(ctx, query, userID).Scan(&count)
	if err != nil {
		return 0, err
	}

	return count, nil
}

// IncrementExecutionCount increments the execution count and updates last_executed_at.
func (r *boostsRepository) IncrementExecutionCount(ctx context.Context, boostID string) error {
	query := `
		UPDATE boosts
		SET execution_count = execution_count + 1, last_executed_at = $1, updated_at = $1
		WHERE id = $2
	`

	ct, err := r.db.Exec(ctx, query, time.Now(), boostID)
	if err != nil {
		return err
	}

	if ct.RowsAffected() == 0 {
		return domain.ErrBoostNotFound
	}

	return nil
}

// UpdateLastSuccessful updates the last_successful_at timestamp.
func (r *boostsRepository) UpdateLastSuccessful(ctx context.Context, boostID string) error {
	query := `
		UPDATE boosts
		SET last_successful_at = $1, updated_at = $1
		WHERE id = $2
	`

	ct, err := r.db.Exec(ctx, query, time.Now(), boostID)
	if err != nil {
		return err
	}

	if ct.RowsAffected() == 0 {
		return domain.ErrBoostNotFound
	}

	return nil
}

// GetBoostsByEventTrigger retrieves enabled boosts that trigger on a specific event type.
// Uses JSONB containment to match trigger_config.event field.
func (r *boostsRepository) GetBoostsByEventTrigger(ctx context.Context, userID, eventType string) ([]*domain.Boost, error) {
	query := `
		SELECT id, user_id, name, description, enabled, priority,
			boost_type, trigger_config, conditions, condition_logic,
			actions, execution_count, last_executed_at, last_successful_at,
			created_at, updated_at
		FROM boosts
		WHERE user_id = $1
			AND enabled = true
			AND trigger_config->>'type' = 'event'
			AND trigger_config->>'event' = $2
			AND deleted_at IS NULL
		ORDER BY priority DESC, created_at ASC
	`

	return r.queryBoosts(ctx, query, userID, eventType)
}

// GetBoostsByType retrieves enabled boosts of a specific type.
func (r *boostsRepository) GetBoostsByType(ctx context.Context, userID string, boostType domain.BoostType) ([]*domain.Boost, error) {
	query := `
		SELECT id, user_id, name, description, enabled, priority,
			boost_type, trigger_config, conditions, condition_logic,
			actions, execution_count, last_executed_at, last_successful_at,
			created_at, updated_at
		FROM boosts
		WHERE user_id = $1 AND enabled = true AND boost_type = $2 AND deleted_at IS NULL
		ORDER BY priority DESC, created_at ASC
	`

	return r.queryBoosts(ctx, query, userID, boostType)
}

// GetBoostsByScheduleTrigger retrieves all enabled boosts with schedule triggers.
// Issue #37: Boost Scheduler - used by CronTriggerProvider.
func (r *boostsRepository) GetBoostsByScheduleTrigger(ctx context.Context) ([]*domain.Boost, error) {
	query := `
		SELECT id, user_id, name, description, enabled, priority,
			boost_type, trigger_config, conditions, condition_logic,
			actions, execution_count, last_executed_at, last_successful_at,
			created_at, updated_at
		FROM boosts
		WHERE enabled = true
			AND trigger_config->>'type' = 'schedule'
			AND trigger_config->>'cron' IS NOT NULL
			AND deleted_at IS NULL
		ORDER BY priority DESC, created_at ASC
	`

	return r.queryBoosts(ctx, query)
}

// scanBoost scans a single boost from a row.
func (r *boostsRepository) scanBoost(row pgx.Row) (*domain.Boost, error) {
	boost := &domain.Boost{}
	var triggerJSON, conditionsJSON []byte

	err := row.Scan(
		&boost.ID,
		&boost.UserID,
		&boost.Name,
		&boost.Description,
		&boost.Enabled,
		&boost.Priority,
		&boost.BoostType,
		&triggerJSON,
		&conditionsJSON,
		&boost.ConditionLogic,
		&boost.Actions,
		&boost.ExecutionCount,
		&boost.LastExecutedAt,
		&boost.LastSuccessfulAt,
		&boost.CreatedAt,
		&boost.UpdatedAt,
	)

	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrBoostNotFound
		}
		return nil, err
	}

	// Unmarshal JSON fields
	if err := json.Unmarshal(triggerJSON, &boost.TriggerConfig); err != nil {
		return nil, err
	}
	if err := json.Unmarshal(conditionsJSON, &boost.Conditions); err != nil {
		return nil, err
	}

	return boost, nil
}

// queryBoosts is a helper to query and scan multiple boosts.
func (r *boostsRepository) queryBoosts(ctx context.Context, query string, args ...interface{}) ([]*domain.Boost, error) {
	rows, err := r.db.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var boosts []*domain.Boost
	for rows.Next() {
		boost := &domain.Boost{}
		var triggerJSON, conditionsJSON []byte

		err := rows.Scan(
			&boost.ID,
			&boost.UserID,
			&boost.Name,
			&boost.Description,
			&boost.Enabled,
			&boost.Priority,
			&boost.BoostType,
			&triggerJSON,
			&conditionsJSON,
			&boost.ConditionLogic,
			&boost.Actions,
			&boost.ExecutionCount,
			&boost.LastExecutedAt,
			&boost.LastSuccessfulAt,
			&boost.CreatedAt,
			&boost.UpdatedAt,
		)
		if err != nil {
			return nil, err
		}

		// Unmarshal JSON fields
		if err := json.Unmarshal(triggerJSON, &boost.TriggerConfig); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(conditionsJSON, &boost.Conditions); err != nil {
			return nil, err
		}

		boosts = append(boosts, boost)
	}

	if err = rows.Err(); err != nil {
		return nil, err
	}

	return boosts, nil
}
