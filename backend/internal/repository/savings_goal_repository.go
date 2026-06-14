package repository

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
)

// SavingsGoalRepository defines the interface for savings goal data access.
type SavingsGoalRepository interface {
	// CRUD operations
	Create(ctx context.Context, goal *domain.SavingsGoal) error
	GetByID(ctx context.Context, id string) (*domain.SavingsGoal, error)
	// GetByIDForUser retrieves a savings goal by ID with ownership validation.
	// Returns ErrSavingsGoalNotFound if the goal does not exist or belongs to a
	// different user. Use instead of GetByID in security-sensitive paths (GAP AE).
	GetByIDForUser(ctx context.Context, id, userID string) (*domain.SavingsGoal, error)
	GetByUserID(ctx context.Context, userID string) ([]*domain.SavingsGoal, error)
	GetActiveByUserID(ctx context.Context, userID string) ([]*domain.SavingsGoal, error)
	GetByName(ctx context.Context, userID, name string) (*domain.SavingsGoal, error)
	Update(ctx context.Context, goal *domain.SavingsGoal) error
	Delete(ctx context.Context, id, userID string) error
	CountByUserID(ctx context.Context, userID string) (int, error)

	// Balance sync operations (Issue #452, extended by #1300 for multi-basis progress)
	GetByAccountID(ctx context.Context, accountID string) ([]*domain.SavingsGoal, error)
	// UpdateCurrentAmount stores the recomputed current_amount and records the recompute timestamp.
	// balanceAsOf is non-nil when the basis reads account balance (balance / account_percentage).
	UpdateCurrentAmount(ctx context.Context, goalID string, currentAmount float64, lastCalculatedAt time.Time, balanceAsOf *time.Time) error
	// MarkCompleted sets is_completed and completed_at — sticky: only updates completed_at if not already set (Codex C5).
	MarkCompleted(ctx context.Context, goalID string) error
}

// savingsGoalRepository implements SavingsGoalRepository using PostgreSQL.
type savingsGoalRepository struct {
	db DB
}

// NewSavingsGoalRepository creates a new SavingsGoalRepository instance.
func NewSavingsGoalRepository(db DB) SavingsGoalRepository {
	return &savingsGoalRepository{db: db}
}

// goalSelectCols is the canonical SELECT list for savings goals to avoid duplication.
// Includes progress tracking columns added in Issue #1300.
const goalSelectCols = `
	id, user_id, name, description, target_amount, current_amount,
	currency_code, account_id, user_account_id, deadline, is_completed, completed_at,
	created_at, updated_at,
	progress_basis, account_percentage, last_calculated_at, balance_as_of
`

// Create creates a new savings goal in the database.
func (r *savingsGoalRepository) Create(ctx context.Context, goal *domain.SavingsGoal) error {
	if goal.ID == "" {
		goal.ID = uuid.New().String()
	}

	now := time.Now()
	goal.CreatedAt = now
	goal.UpdatedAt = now

	if goal.CurrencyCode == "" {
		goal.CurrencyCode = "USD"
	}

	if goal.ProgressBasis == "" {
		goal.ProgressBasis = domain.ProgressBasisManual
	}

	query := `
		INSERT INTO savings_goals (
			id, user_id, name, description, target_amount, current_amount,
			currency_code, account_id, user_account_id, deadline, is_completed, completed_at,
			created_at, updated_at,
			progress_basis, account_percentage, last_calculated_at, balance_as_of
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18)
	`

	_, err := r.db.Exec(ctx, query,
		goal.ID,
		goal.UserID,
		goal.Name,
		goal.Description,
		goal.TargetAmount,
		goal.CurrentAmount,
		goal.CurrencyCode,
		goal.AccountID,
		goal.UserAccountID,
		goal.Deadline,
		goal.IsCompleted,
		goal.CompletedAt,
		goal.CreatedAt,
		goal.UpdatedAt,
		goal.ProgressBasis,
		goal.AccountPercentage,
		goal.LastCalculatedAt,
		goal.BalanceAsOf,
	)

	if err != nil {
		if strings.Contains(err.Error(), "unique_savings_goal_name_per_user") ||
			strings.Contains(err.Error(), "duplicate key") {
			return domain.ErrSavingsGoalAlreadyExists
		}
		return err
	}

	return nil
}

// GetByID retrieves a savings goal by its ID.
func (r *savingsGoalRepository) GetByID(ctx context.Context, id string) (*domain.SavingsGoal, error) {
	query := `SELECT ` + goalSelectCols + ` FROM savings_goals WHERE id = $1`
	goal := &domain.SavingsGoal{}
	err := r.db.QueryRow(ctx, query, id).Scan(
		&goal.ID,
		&goal.UserID,
		&goal.Name,
		&goal.Description,
		&goal.TargetAmount,
		&goal.CurrentAmount,
		&goal.CurrencyCode,
		&goal.AccountID,
		&goal.UserAccountID,
		&goal.Deadline,
		&goal.IsCompleted,
		&goal.CompletedAt,
		&goal.CreatedAt,
		&goal.UpdatedAt,
		&goal.ProgressBasis,
		&goal.AccountPercentage,
		&goal.LastCalculatedAt,
		&goal.BalanceAsOf,
	)

	if errors.Is(err, pgx.ErrNoRows) {
		return nil, domain.ErrSavingsGoalNotFound
	}
	if err != nil {
		return nil, err
	}

	return goal, nil
}

// GetByIDForUser retrieves a savings goal by ID with ownership validation.
// Returns ErrSavingsGoalNotFound if the goal does not exist or belongs to a different user.
func (r *savingsGoalRepository) GetByIDForUser(ctx context.Context, id, userID string) (*domain.SavingsGoal, error) {
	query := `SELECT ` + goalSelectCols + ` FROM savings_goals WHERE id = $1 AND user_id = $2`
	goal := &domain.SavingsGoal{}
	err := r.db.QueryRow(ctx, query, id, userID).Scan(
		&goal.ID,
		&goal.UserID,
		&goal.Name,
		&goal.Description,
		&goal.TargetAmount,
		&goal.CurrentAmount,
		&goal.CurrencyCode,
		&goal.AccountID,
		&goal.UserAccountID,
		&goal.Deadline,
		&goal.IsCompleted,
		&goal.CompletedAt,
		&goal.CreatedAt,
		&goal.UpdatedAt,
		&goal.ProgressBasis,
		&goal.AccountPercentage,
		&goal.LastCalculatedAt,
		&goal.BalanceAsOf,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, domain.ErrSavingsGoalNotFound
	}
	if err != nil {
		return nil, err
	}
	return goal, nil
}

// GetByUserID retrieves all savings goals for a specific user.
func (r *savingsGoalRepository) GetByUserID(ctx context.Context, userID string) ([]*domain.SavingsGoal, error) {
	query := `SELECT ` + goalSelectCols + ` FROM savings_goals WHERE user_id = $1 ORDER BY created_at DESC`
	return r.scanGoals(ctx, query, userID)
}

// GetActiveByUserID retrieves all active (not completed) savings goals for a user.
func (r *savingsGoalRepository) GetActiveByUserID(ctx context.Context, userID string) ([]*domain.SavingsGoal, error) {
	query := `SELECT ` + goalSelectCols + ` FROM savings_goals WHERE user_id = $1 AND is_completed = false ORDER BY created_at DESC`
	return r.scanGoals(ctx, query, userID)
}

// GetByName retrieves a savings goal by user ID and name.
func (r *savingsGoalRepository) GetByName(ctx context.Context, userID, name string) (*domain.SavingsGoal, error) {
	query := `SELECT ` + goalSelectCols + ` FROM savings_goals WHERE user_id = $1 AND name = $2`
	goal := &domain.SavingsGoal{}
	err := r.db.QueryRow(ctx, query, userID, name).Scan(
		&goal.ID,
		&goal.UserID,
		&goal.Name,
		&goal.Description,
		&goal.TargetAmount,
		&goal.CurrentAmount,
		&goal.CurrencyCode,
		&goal.AccountID,
		&goal.UserAccountID,
		&goal.Deadline,
		&goal.IsCompleted,
		&goal.CompletedAt,
		&goal.CreatedAt,
		&goal.UpdatedAt,
		&goal.ProgressBasis,
		&goal.AccountPercentage,
		&goal.LastCalculatedAt,
		&goal.BalanceAsOf,
	)

	if errors.Is(err, pgx.ErrNoRows) {
		return nil, domain.ErrSavingsGoalNotFound
	}
	if err != nil {
		return nil, err
	}

	return goal, nil
}

// Update updates an existing savings goal.
func (r *savingsGoalRepository) Update(ctx context.Context, goal *domain.SavingsGoal) error {
	goal.UpdatedAt = time.Now()

	if goal.ProgressBasis == "" {
		goal.ProgressBasis = domain.ProgressBasisManual
	}

	query := `
		UPDATE savings_goals
		SET name = $1, description = $2, target_amount = $3, current_amount = $4,
			currency_code = $5, account_id = $6, deadline = $7, is_completed = $8,
			completed_at = $9, updated_at = $10, user_account_id = $11,
			progress_basis = $12, account_percentage = $13, last_calculated_at = $14,
			balance_as_of = $15
		WHERE id = $16 AND user_id = $17
	`

	ct, err := r.db.Exec(ctx, query,
		goal.Name,
		goal.Description,
		goal.TargetAmount,
		goal.CurrentAmount,
		goal.CurrencyCode,
		goal.AccountID,
		goal.Deadline,
		goal.IsCompleted,
		goal.CompletedAt,
		goal.UpdatedAt,
		goal.UserAccountID,
		goal.ProgressBasis,
		goal.AccountPercentage,
		goal.LastCalculatedAt,
		goal.BalanceAsOf,
		goal.ID,
		goal.UserID,
	)

	if err != nil {
		if strings.Contains(err.Error(), "unique_savings_goal_name_per_user") ||
			strings.Contains(err.Error(), "duplicate key") {
			return domain.ErrSavingsGoalAlreadyExists
		}
		return err
	}

	if ct.RowsAffected() == 0 {
		return domain.ErrSavingsGoalNotFound
	}

	return nil
}

// Delete removes a savings goal from the database.
func (r *savingsGoalRepository) Delete(ctx context.Context, id, userID string) error {
	query := `DELETE FROM savings_goals WHERE id = $1 AND user_id = $2`

	ct, err := r.db.Exec(ctx, query, id, userID)
	if err != nil {
		return err
	}

	if ct.RowsAffected() == 0 {
		return domain.ErrSavingsGoalNotFound
	}

	return nil
}

// CountByUserID counts the total number of savings goals for a user.
func (r *savingsGoalRepository) CountByUserID(ctx context.Context, userID string) (int, error) {
	query := `SELECT COUNT(*) FROM savings_goals WHERE user_id = $1`

	var count int
	err := r.db.QueryRow(ctx, query, userID).Scan(&count)
	if err != nil {
		return 0, err
	}

	return count, nil
}

// GetByAccountID retrieves all savings goals linked to a specific account.
// Issue #1109: Chain-expand via user_account_id to include pre-relink goals.
func (r *savingsGoalRepository) GetByAccountID(ctx context.Context, accountID string) ([]*domain.SavingsGoal, error) {
	query := `SELECT ` + goalSelectCols + ` FROM savings_goals
	WHERE (
		user_account_id = (SELECT user_account_id FROM accounts WHERE id = $1 AND user_account_id IS NOT NULL)
		OR (account_id = $1 AND user_account_id IS NULL)
	) AND is_completed = false ORDER BY created_at DESC`
	return r.scanGoals(ctx, query, accountID)
}

// scanGoals is a helper that scans savings goals from a query result.
func (r *savingsGoalRepository) scanGoals(ctx context.Context, query string, args ...interface{}) ([]*domain.SavingsGoal, error) {
	rows, err := r.db.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var goals []*domain.SavingsGoal
	for rows.Next() {
		goal := &domain.SavingsGoal{}
		if err := rows.Scan(
			&goal.ID,
			&goal.UserID,
			&goal.Name,
			&goal.Description,
			&goal.TargetAmount,
			&goal.CurrentAmount,
			&goal.CurrencyCode,
			&goal.AccountID,
			&goal.UserAccountID,
			&goal.Deadline,
			&goal.IsCompleted,
			&goal.CompletedAt,
			&goal.CreatedAt,
			&goal.UpdatedAt,
			&goal.ProgressBasis,
			&goal.AccountPercentage,
			&goal.LastCalculatedAt,
			&goal.BalanceAsOf,
		); err != nil {
			return nil, err
		}
		goals = append(goals, goal)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	if len(goals) == 0 {
		return []*domain.SavingsGoal{}, nil
	}
	return goals, nil
}

// UpdateCurrentAmount stores the recomputed current_amount and records the recompute timestamp.
// balanceAsOf is non-nil when the basis reads account balance (balance / account_percentage).
// This is used by the savings progress service (Issue #452, extended by #1300).
func (r *savingsGoalRepository) UpdateCurrentAmount(ctx context.Context, goalID string, currentAmount float64, lastCalculatedAt time.Time, balanceAsOf *time.Time) error {
	query := `
		UPDATE savings_goals
		SET current_amount = $1, last_calculated_at = $2, balance_as_of = $3, updated_at = NOW()
		WHERE id = $4
	`

	ct, err := r.db.Exec(ctx, query, currentAmount, lastCalculatedAt, balanceAsOf, goalID)
	if err != nil {
		return err
	}

	if ct.RowsAffected() == 0 {
		return domain.ErrSavingsGoalNotFound
	}

	return nil
}

// MarkCompleted marks a savings goal as completed.
// completed_at is sticky: it is only set if not already set (Codex C5).
// This is called when the current amount reaches the target amount.
func (r *savingsGoalRepository) MarkCompleted(ctx context.Context, goalID string) error {
	now := time.Now()

	// completed_at is only written if NULL (sticky once set).
	query := `
		UPDATE savings_goals
		SET is_completed = true,
		    completed_at = CASE WHEN completed_at IS NULL THEN $1 ELSE completed_at END,
		    updated_at = $1
		WHERE id = $2
	`

	ct, err := r.db.Exec(ctx, query, now, goalID)
	if err != nil {
		return err
	}

	if ct.RowsAffected() == 0 {
		return domain.ErrSavingsGoalNotFound
	}

	return nil
}
