// Package repository provides data access for rollover history tracking.
// Issue #36: Rule Executors - Envelope budgeting rollover tracking.
package repository

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
)

// RolloverHistoryRepository defines the interface for rollover history data access.
type RolloverHistoryRepository interface {
	// Create creates a new rollover history record.
	Create(ctx context.Context, rollover *domain.RolloverHistory) error

	// GetByID retrieves a rollover history by its ID.
	GetByID(ctx context.Context, id string) (*domain.RolloverHistory, error)

	// GetByUserID retrieves rollover history for a user with pagination.
	GetByUserID(ctx context.Context, userID string, limit, offset int) ([]*domain.RolloverHistory, error)

	// GetByBudgetID retrieves rollover history for a budget with pagination.
	GetByBudgetID(ctx context.Context, budgetID string, limit, offset int) ([]*domain.RolloverHistory, error)

	// GetPendingByUserID retrieves all pending rollovers for a user.
	GetPendingByUserID(ctx context.Context, userID string) ([]*domain.RolloverHistory, error)

	// UpdateStatus updates the status of a rollover (and optionally applied_at timestamp).
	UpdateStatus(ctx context.Context, id string, status domain.RolloverStatus, appliedAt *time.Time) error

	// GetByBudgetAndPeriod retrieves rollovers for a budget within a specific period.
	GetByBudgetAndPeriod(ctx context.Context, budgetID string, periodStart, periodEnd time.Time) ([]*domain.RolloverHistory, error)

	// GetBySourceCategory retrieves rollovers for a source category with pagination.
	GetBySourceCategory(ctx context.Context, sourceCategoryID string, limit, offset int) ([]*domain.RolloverHistory, error)

	// CountPendingByUserID counts pending rollovers for a user.
	CountPendingByUserID(ctx context.Context, userID string) (int, error)
}

// rolloverHistoryRepository implements RolloverHistoryRepository using PostgreSQL.
type rolloverHistoryRepository struct {
	db DB
}

// NewRolloverHistoryRepository creates a new RolloverHistoryRepository instance.
func NewRolloverHistoryRepository(db DB) RolloverHistoryRepository {
	return &rolloverHistoryRepository{db: db}
}

// Create creates a new rollover history record.
func (r *rolloverHistoryRepository) Create(ctx context.Context, rollover *domain.RolloverHistory) error {
	// Generate UUID if not already set
	if rollover.ID == "" {
		rollover.ID = uuid.New().String()
	}

	// Set defaults
	if rollover.Status == "" {
		rollover.Status = domain.RolloverStatusPending
	}

	// Set timestamp
	rollover.CreatedAt = time.Now()

	query := `
		INSERT INTO rollover_history (
			id, user_id, budget_id, rule_id, rule_execution_id,
			source_budget_category_id, target_budget_category_id,
			amount, rollover_type, budget_period_start, budget_period_end,
			status, applied_at, created_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14)
	`

	_, err := r.db.Exec(ctx, query,
		rollover.ID,
		rollover.UserID,
		rollover.BudgetID,
		rollover.RuleID,
		rollover.RuleExecutionID,
		rollover.SourceBudgetCategoryID,
		rollover.TargetBudgetCategoryID,
		rollover.Amount,
		rollover.RolloverType,
		rollover.BudgetPeriodStart,
		rollover.BudgetPeriodEnd,
		rollover.Status,
		rollover.AppliedAt,
		rollover.CreatedAt,
	)

	return err
}

// GetByID retrieves a rollover history by its ID.
func (r *rolloverHistoryRepository) GetByID(ctx context.Context, id string) (*domain.RolloverHistory, error) {
	query := `
		SELECT id, user_id, budget_id, rule_id, rule_execution_id,
			source_budget_category_id, target_budget_category_id,
			amount, rollover_type, budget_period_start, budget_period_end,
			status, applied_at, created_at
		FROM rollover_history
		WHERE id = $1
	`

	return r.scanRollover(r.db.QueryRow(ctx, query, id))
}

// GetByUserID retrieves rollover history for a user with pagination.
func (r *rolloverHistoryRepository) GetByUserID(ctx context.Context, userID string, limit, offset int) ([]*domain.RolloverHistory, error) {
	query := `
		SELECT id, user_id, budget_id, rule_id, rule_execution_id,
			source_budget_category_id, target_budget_category_id,
			amount, rollover_type, budget_period_start, budget_period_end,
			status, applied_at, created_at
		FROM rollover_history
		WHERE user_id = $1
		ORDER BY created_at DESC
		LIMIT $2 OFFSET $3
	`

	return r.queryRollovers(ctx, query, userID, limit, offset)
}

// GetByBudgetID retrieves rollover history for a budget with pagination.
func (r *rolloverHistoryRepository) GetByBudgetID(ctx context.Context, budgetID string, limit, offset int) ([]*domain.RolloverHistory, error) {
	query := `
		SELECT id, user_id, budget_id, rule_id, rule_execution_id,
			source_budget_category_id, target_budget_category_id,
			amount, rollover_type, budget_period_start, budget_period_end,
			status, applied_at, created_at
		FROM rollover_history
		WHERE budget_id = $1
		ORDER BY created_at DESC
		LIMIT $2 OFFSET $3
	`

	return r.queryRollovers(ctx, query, budgetID, limit, offset)
}

// GetPendingByUserID retrieves all pending rollovers for a user.
func (r *rolloverHistoryRepository) GetPendingByUserID(ctx context.Context, userID string) ([]*domain.RolloverHistory, error) {
	query := `
		SELECT id, user_id, budget_id, rule_id, rule_execution_id,
			source_budget_category_id, target_budget_category_id,
			amount, rollover_type, budget_period_start, budget_period_end,
			status, applied_at, created_at
		FROM rollover_history
		WHERE user_id = $1 AND status = 'pending'
		ORDER BY created_at DESC
	`

	return r.queryRollovers(ctx, query, userID)
}

// UpdateStatus updates the status of a rollover.
func (r *rolloverHistoryRepository) UpdateStatus(ctx context.Context, id string, status domain.RolloverStatus, appliedAt *time.Time) error {
	query := `
		UPDATE rollover_history
		SET status = $1, applied_at = $2
		WHERE id = $3
	`

	ct, err := r.db.Exec(ctx, query, status, appliedAt, id)
	if err != nil {
		return err
	}

	if ct.RowsAffected() == 0 {
		return domain.ErrRolloverHistoryNotFound
	}

	return nil
}

// GetByBudgetAndPeriod retrieves rollovers for a budget within a specific period.
func (r *rolloverHistoryRepository) GetByBudgetAndPeriod(ctx context.Context, budgetID string, periodStart, periodEnd time.Time) ([]*domain.RolloverHistory, error) {
	query := `
		SELECT id, user_id, budget_id, rule_id, rule_execution_id,
			source_budget_category_id, target_budget_category_id,
			amount, rollover_type, budget_period_start, budget_period_end,
			status, applied_at, created_at
		FROM rollover_history
		WHERE budget_id = $1
			AND budget_period_start >= $2
			AND budget_period_end <= $3
		ORDER BY created_at DESC
	`

	return r.queryRollovers(ctx, query, budgetID, periodStart, periodEnd)
}

// GetBySourceCategory retrieves rollovers for a source category with pagination.
func (r *rolloverHistoryRepository) GetBySourceCategory(ctx context.Context, sourceCategoryID string, limit, offset int) ([]*domain.RolloverHistory, error) {
	query := `
		SELECT id, user_id, budget_id, rule_id, rule_execution_id,
			source_budget_category_id, target_budget_category_id,
			amount, rollover_type, budget_period_start, budget_period_end,
			status, applied_at, created_at
		FROM rollover_history
		WHERE source_budget_category_id = $1
		ORDER BY created_at DESC
		LIMIT $2 OFFSET $3
	`

	return r.queryRollovers(ctx, query, sourceCategoryID, limit, offset)
}

// CountPendingByUserID counts pending rollovers for a user.
func (r *rolloverHistoryRepository) CountPendingByUserID(ctx context.Context, userID string) (int, error) {
	query := `SELECT COUNT(*) FROM rollover_history WHERE user_id = $1 AND status = 'pending'`

	var count int
	err := r.db.QueryRow(ctx, query, userID).Scan(&count)
	if err != nil {
		return 0, err
	}

	return count, nil
}

// scanRollover scans a single rollover from a row.
func (r *rolloverHistoryRepository) scanRollover(row pgx.Row) (*domain.RolloverHistory, error) {
	rollover := &domain.RolloverHistory{}

	err := row.Scan(
		&rollover.ID,
		&rollover.UserID,
		&rollover.BudgetID,
		&rollover.RuleID,
		&rollover.RuleExecutionID,
		&rollover.SourceBudgetCategoryID,
		&rollover.TargetBudgetCategoryID,
		&rollover.Amount,
		&rollover.RolloverType,
		&rollover.BudgetPeriodStart,
		&rollover.BudgetPeriodEnd,
		&rollover.Status,
		&rollover.AppliedAt,
		&rollover.CreatedAt,
	)

	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrRolloverHistoryNotFound
		}
		return nil, err
	}

	return rollover, nil
}

// queryRollovers is a helper to query and scan multiple rollovers.
func (r *rolloverHistoryRepository) queryRollovers(ctx context.Context, query string, args ...interface{}) ([]*domain.RolloverHistory, error) {
	rows, err := r.db.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var rollovers []*domain.RolloverHistory
	for rows.Next() {
		rollover := &domain.RolloverHistory{}

		err := rows.Scan(
			&rollover.ID,
			&rollover.UserID,
			&rollover.BudgetID,
			&rollover.RuleID,
			&rollover.RuleExecutionID,
			&rollover.SourceBudgetCategoryID,
			&rollover.TargetBudgetCategoryID,
			&rollover.Amount,
			&rollover.RolloverType,
			&rollover.BudgetPeriodStart,
			&rollover.BudgetPeriodEnd,
			&rollover.Status,
			&rollover.AppliedAt,
			&rollover.CreatedAt,
		)
		if err != nil {
			return nil, err
		}

		rollovers = append(rollovers, rollover)
	}

	if err = rows.Err(); err != nil {
		return nil, err
	}

	return rollovers, nil
}
