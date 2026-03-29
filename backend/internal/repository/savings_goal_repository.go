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
	GetByUserID(ctx context.Context, userID string) ([]*domain.SavingsGoal, error)
	GetActiveByUserID(ctx context.Context, userID string) ([]*domain.SavingsGoal, error)
	GetByName(ctx context.Context, userID, name string) (*domain.SavingsGoal, error)
	Update(ctx context.Context, goal *domain.SavingsGoal) error
	Delete(ctx context.Context, id, userID string) error
	CountByUserID(ctx context.Context, userID string) (int, error)

	// Balance sync operations (Issue #452)
	GetByAccountID(ctx context.Context, accountID string) ([]*domain.SavingsGoal, error)
	UpdateCurrentAmount(ctx context.Context, goalID string, currentAmount float64) error
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

	query := `
		INSERT INTO savings_goals (
			id, user_id, name, description, target_amount, current_amount,
			currency_code, account_id, deadline, is_completed, completed_at,
			created_at, updated_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)
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
		goal.Deadline,
		goal.IsCompleted,
		goal.CompletedAt,
		goal.CreatedAt,
		goal.UpdatedAt,
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
	query := `
		SELECT id, user_id, name, description, target_amount, current_amount,
			currency_code, account_id, deadline, is_completed, completed_at,
			created_at, updated_at
		FROM savings_goals
		WHERE id = $1
	`

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
		&goal.Deadline,
		&goal.IsCompleted,
		&goal.CompletedAt,
		&goal.CreatedAt,
		&goal.UpdatedAt,
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
	query := `
		SELECT id, user_id, name, description, target_amount, current_amount,
			currency_code, account_id, deadline, is_completed, completed_at,
			created_at, updated_at
		FROM savings_goals
		WHERE user_id = $1
		ORDER BY created_at DESC
	`

	rows, err := r.db.Query(ctx, query, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var goals []*domain.SavingsGoal
	for rows.Next() {
		goal := &domain.SavingsGoal{}
		err := rows.Scan(
			&goal.ID,
			&goal.UserID,
			&goal.Name,
			&goal.Description,
			&goal.TargetAmount,
			&goal.CurrentAmount,
			&goal.CurrencyCode,
			&goal.AccountID,
			&goal.Deadline,
			&goal.IsCompleted,
			&goal.CompletedAt,
			&goal.CreatedAt,
			&goal.UpdatedAt,
		)
		if err != nil {
			return nil, err
		}
		goals = append(goals, goal)
	}

	if goals == nil {
		goals = []*domain.SavingsGoal{}
	}

	return goals, nil
}

// GetActiveByUserID retrieves all active (not completed) savings goals for a user.
func (r *savingsGoalRepository) GetActiveByUserID(ctx context.Context, userID string) ([]*domain.SavingsGoal, error) {
	query := `
		SELECT id, user_id, name, description, target_amount, current_amount,
			currency_code, account_id, deadline, is_completed, completed_at,
			created_at, updated_at
		FROM savings_goals
		WHERE user_id = $1 AND is_completed = false
		ORDER BY created_at DESC
	`

	rows, err := r.db.Query(ctx, query, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var goals []*domain.SavingsGoal
	for rows.Next() {
		goal := &domain.SavingsGoal{}
		err := rows.Scan(
			&goal.ID,
			&goal.UserID,
			&goal.Name,
			&goal.Description,
			&goal.TargetAmount,
			&goal.CurrentAmount,
			&goal.CurrencyCode,
			&goal.AccountID,
			&goal.Deadline,
			&goal.IsCompleted,
			&goal.CompletedAt,
			&goal.CreatedAt,
			&goal.UpdatedAt,
		)
		if err != nil {
			return nil, err
		}
		goals = append(goals, goal)
	}

	if goals == nil {
		goals = []*domain.SavingsGoal{}
	}

	return goals, nil
}

// GetByName retrieves a savings goal by user ID and name.
func (r *savingsGoalRepository) GetByName(ctx context.Context, userID, name string) (*domain.SavingsGoal, error) {
	query := `
		SELECT id, user_id, name, description, target_amount, current_amount,
			currency_code, account_id, deadline, is_completed, completed_at,
			created_at, updated_at
		FROM savings_goals
		WHERE user_id = $1 AND name = $2
	`

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
		&goal.Deadline,
		&goal.IsCompleted,
		&goal.CompletedAt,
		&goal.CreatedAt,
		&goal.UpdatedAt,
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

	query := `
		UPDATE savings_goals
		SET name = $1, description = $2, target_amount = $3, current_amount = $4,
			currency_code = $5, account_id = $6, deadline = $7, is_completed = $8,
			completed_at = $9, updated_at = $10
		WHERE id = $11 AND user_id = $12
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
// This is used for balance sync when account balances change (Issue #452).
func (r *savingsGoalRepository) GetByAccountID(ctx context.Context, accountID string) ([]*domain.SavingsGoal, error) {
	query := `
		SELECT id, user_id, name, description, target_amount, current_amount,
			currency_code, account_id, deadline, is_completed, completed_at,
			created_at, updated_at
		FROM savings_goals
		WHERE account_id = $1 AND is_completed = false
		ORDER BY created_at DESC
	`

	rows, err := r.db.Query(ctx, query, accountID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var goals []*domain.SavingsGoal
	for rows.Next() {
		goal := &domain.SavingsGoal{}
		err := rows.Scan(
			&goal.ID,
			&goal.UserID,
			&goal.Name,
			&goal.Description,
			&goal.TargetAmount,
			&goal.CurrentAmount,
			&goal.CurrencyCode,
			&goal.AccountID,
			&goal.Deadline,
			&goal.IsCompleted,
			&goal.CompletedAt,
			&goal.CreatedAt,
			&goal.UpdatedAt,
		)
		if err != nil {
			return nil, err
		}
		goals = append(goals, goal)
	}

	if goals == nil {
		goals = []*domain.SavingsGoal{}
	}

	return goals, nil
}

// UpdateCurrentAmount updates the current_amount for a savings goal.
// This is used by the balance sync service (Issue #452).
func (r *savingsGoalRepository) UpdateCurrentAmount(ctx context.Context, goalID string, currentAmount float64) error {
	query := `
		UPDATE savings_goals
		SET current_amount = $1, updated_at = NOW()
		WHERE id = $2
	`

	ct, err := r.db.Exec(ctx, query, currentAmount, goalID)
	if err != nil {
		return err
	}

	if ct.RowsAffected() == 0 {
		return domain.ErrSavingsGoalNotFound
	}

	return nil
}

// MarkCompleted marks a savings goal as completed.
// This is called when the current amount reaches the target amount.
func (r *savingsGoalRepository) MarkCompleted(ctx context.Context, goalID string) error {
	now := time.Now()

	query := `
		UPDATE savings_goals
		SET is_completed = true, completed_at = $1, updated_at = $1
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
