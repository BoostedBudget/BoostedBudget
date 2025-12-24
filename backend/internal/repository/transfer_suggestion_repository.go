// Package repository provides data access for transfer suggestions.
// Issue #35: Rules Engine Core - transfer suggestions (NO actual money movement).
package repository

import (
	"context"
	"database/sql"
	"time"

	"github.com/google/uuid"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
)

// TransferSuggestionRepository defines the interface for transfer suggestion data access.
type TransferSuggestionRepository interface {
	// Create creates a new transfer suggestion.
	Create(ctx context.Context, suggestion *domain.TransferSuggestion) error

	// GetByID retrieves a suggestion by its ID.
	GetByID(ctx context.Context, id string) (*domain.TransferSuggestion, error)

	// GetByUserID retrieves suggestions for a user with optional status filter and pagination.
	GetByUserID(ctx context.Context, userID string, status *domain.SuggestionStatus, limit, offset int) ([]*domain.TransferSuggestion, error)

	// GetPendingByUserID retrieves all pending suggestions for a user.
	GetPendingByUserID(ctx context.Context, userID string) ([]*domain.TransferSuggestion, error)

	// UpdateStatus updates the status of a suggestion.
	UpdateStatus(ctx context.Context, id string, status domain.SuggestionStatus, userNotes *string) error

	// Update updates a suggestion record.
	Update(ctx context.Context, suggestion *domain.TransferSuggestion) error

	// ExpirePending marks expired pending suggestions as expired.
	// Returns the number of suggestions that were expired.
	ExpirePending(ctx context.Context) (int64, error)

	// CountPendingByUserID counts pending suggestions for a user.
	CountPendingByUserID(ctx context.Context, userID string) (int, error)

	// GetByRuleID retrieves suggestions created by a specific rule.
	GetByRuleID(ctx context.Context, ruleID string, limit, offset int) ([]*domain.TransferSuggestion, error)
}

// transferSuggestionRepository implements TransferSuggestionRepository using PostgreSQL.
type transferSuggestionRepository struct {
	db *sql.DB
}

// NewTransferSuggestionRepository creates a new TransferSuggestionRepository instance.
func NewTransferSuggestionRepository(db *sql.DB) TransferSuggestionRepository {
	return &transferSuggestionRepository{db: db}
}

// Create creates a new transfer suggestion.
func (r *transferSuggestionRepository) Create(ctx context.Context, suggestion *domain.TransferSuggestion) error {
	// Generate UUID if not already set
	if suggestion.ID == "" {
		suggestion.ID = uuid.New().String()
	}

	// Set defaults
	if suggestion.CurrencyCode == "" {
		suggestion.CurrencyCode = "USD"
	}
	if suggestion.Status == "" {
		suggestion.Status = domain.SuggestionStatusPending
	}

	// Set timestamps
	now := time.Now()
	suggestion.CreatedAt = now
	suggestion.UpdatedAt = now

	query := `
		INSERT INTO transfer_suggestions (
			id, user_id, rule_id, rule_execution_id,
			from_account_id, to_account_id, amount, currency_code,
			reason, source, status, expires_at,
			user_action_at, user_notes, created_at, updated_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16)
	`

	_, err := r.db.ExecContext(ctx, query,
		suggestion.ID,
		suggestion.UserID,
		suggestion.RuleID,
		suggestion.RuleExecutionID,
		suggestion.FromAccountID,
		suggestion.ToAccountID,
		suggestion.Amount,
		suggestion.CurrencyCode,
		suggestion.Reason,
		suggestion.Source,
		suggestion.Status,
		suggestion.ExpiresAt,
		suggestion.UserActionAt,
		suggestion.UserNotes,
		suggestion.CreatedAt,
		suggestion.UpdatedAt,
	)

	return err
}

// GetByID retrieves a suggestion by its ID.
func (r *transferSuggestionRepository) GetByID(ctx context.Context, id string) (*domain.TransferSuggestion, error) {
	query := `
		SELECT id, user_id, rule_id, rule_execution_id,
			from_account_id, to_account_id, amount, currency_code,
			reason, source, status, expires_at,
			user_action_at, user_notes, created_at, updated_at
		FROM transfer_suggestions
		WHERE id = $1
	`

	return r.scanSuggestion(r.db.QueryRowContext(ctx, query, id))
}

// GetByUserID retrieves suggestions for a user with optional status filter and pagination.
func (r *transferSuggestionRepository) GetByUserID(ctx context.Context, userID string, status *domain.SuggestionStatus, limit, offset int) ([]*domain.TransferSuggestion, error) {
	var query string
	var args []interface{}

	if status != nil {
		query = `
			SELECT id, user_id, rule_id, rule_execution_id,
				from_account_id, to_account_id, amount, currency_code,
				reason, source, status, expires_at,
				user_action_at, user_notes, created_at, updated_at
			FROM transfer_suggestions
			WHERE user_id = $1 AND status = $2
			ORDER BY created_at DESC
			LIMIT $3 OFFSET $4
		`
		args = []interface{}{userID, *status, limit, offset}
	} else {
		query = `
			SELECT id, user_id, rule_id, rule_execution_id,
				from_account_id, to_account_id, amount, currency_code,
				reason, source, status, expires_at,
				user_action_at, user_notes, created_at, updated_at
			FROM transfer_suggestions
			WHERE user_id = $1
			ORDER BY created_at DESC
			LIMIT $2 OFFSET $3
		`
		args = []interface{}{userID, limit, offset}
	}

	return r.querySuggestions(ctx, query, args...)
}

// GetPendingByUserID retrieves all pending suggestions for a user.
func (r *transferSuggestionRepository) GetPendingByUserID(ctx context.Context, userID string) ([]*domain.TransferSuggestion, error) {
	query := `
		SELECT id, user_id, rule_id, rule_execution_id,
			from_account_id, to_account_id, amount, currency_code,
			reason, source, status, expires_at,
			user_action_at, user_notes, created_at, updated_at
		FROM transfer_suggestions
		WHERE user_id = $1 AND status = 'pending'
		ORDER BY created_at DESC
	`

	return r.querySuggestions(ctx, query, userID)
}

// UpdateStatus updates the status of a suggestion.
func (r *transferSuggestionRepository) UpdateStatus(ctx context.Context, id string, status domain.SuggestionStatus, userNotes *string) error {
	now := time.Now()

	query := `
		UPDATE transfer_suggestions
		SET status = $1, user_action_at = $2, user_notes = $3, updated_at = $4
		WHERE id = $5
	`

	result, err := r.db.ExecContext(ctx, query, status, now, userNotes, now, id)
	if err != nil {
		return err
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return err
	}

	if rowsAffected == 0 {
		return domain.ErrTransferSuggestionNotFound
	}

	return nil
}

// Update updates a suggestion record.
func (r *transferSuggestionRepository) Update(ctx context.Context, suggestion *domain.TransferSuggestion) error {
	suggestion.UpdatedAt = time.Now()

	query := `
		UPDATE transfer_suggestions
		SET status = $1, user_action_at = $2, user_notes = $3, updated_at = $4
		WHERE id = $5
	`

	result, err := r.db.ExecContext(ctx, query,
		suggestion.Status,
		suggestion.UserActionAt,
		suggestion.UserNotes,
		suggestion.UpdatedAt,
		suggestion.ID,
	)
	if err != nil {
		return err
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return err
	}

	if rowsAffected == 0 {
		return domain.ErrTransferSuggestionNotFound
	}

	return nil
}

// ExpirePending marks expired pending suggestions as expired.
func (r *transferSuggestionRepository) ExpirePending(ctx context.Context) (int64, error) {
	now := time.Now()

	query := `
		UPDATE transfer_suggestions
		SET status = 'expired', updated_at = $1
		WHERE status = 'pending' AND expires_at IS NOT NULL AND expires_at < $1
	`

	result, err := r.db.ExecContext(ctx, query, now)
	if err != nil {
		return 0, err
	}

	return result.RowsAffected()
}

// CountPendingByUserID counts pending suggestions for a user.
func (r *transferSuggestionRepository) CountPendingByUserID(ctx context.Context, userID string) (int, error) {
	query := `SELECT COUNT(*) FROM transfer_suggestions WHERE user_id = $1 AND status = 'pending'`

	var count int
	err := r.db.QueryRowContext(ctx, query, userID).Scan(&count)
	if err != nil {
		return 0, err
	}

	return count, nil
}

// GetByRuleID retrieves suggestions created by a specific rule.
func (r *transferSuggestionRepository) GetByRuleID(ctx context.Context, ruleID string, limit, offset int) ([]*domain.TransferSuggestion, error) {
	query := `
		SELECT id, user_id, rule_id, rule_execution_id,
			from_account_id, to_account_id, amount, currency_code,
			reason, source, status, expires_at,
			user_action_at, user_notes, created_at, updated_at
		FROM transfer_suggestions
		WHERE rule_id = $1
		ORDER BY created_at DESC
		LIMIT $2 OFFSET $3
	`

	return r.querySuggestions(ctx, query, ruleID, limit, offset)
}

// scanSuggestion scans a single suggestion from a row.
func (r *transferSuggestionRepository) scanSuggestion(row *sql.Row) (*domain.TransferSuggestion, error) {
	suggestion := &domain.TransferSuggestion{}

	err := row.Scan(
		&suggestion.ID,
		&suggestion.UserID,
		&suggestion.RuleID,
		&suggestion.RuleExecutionID,
		&suggestion.FromAccountID,
		&suggestion.ToAccountID,
		&suggestion.Amount,
		&suggestion.CurrencyCode,
		&suggestion.Reason,
		&suggestion.Source,
		&suggestion.Status,
		&suggestion.ExpiresAt,
		&suggestion.UserActionAt,
		&suggestion.UserNotes,
		&suggestion.CreatedAt,
		&suggestion.UpdatedAt,
	)

	if err != nil {
		if err == sql.ErrNoRows {
			return nil, domain.ErrTransferSuggestionNotFound
		}
		return nil, err
	}

	return suggestion, nil
}

// querySuggestions is a helper to query and scan multiple suggestions.
func (r *transferSuggestionRepository) querySuggestions(ctx context.Context, query string, args ...interface{}) ([]*domain.TransferSuggestion, error) {
	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var suggestions []*domain.TransferSuggestion
	for rows.Next() {
		suggestion := &domain.TransferSuggestion{}

		err := rows.Scan(
			&suggestion.ID,
			&suggestion.UserID,
			&suggestion.RuleID,
			&suggestion.RuleExecutionID,
			&suggestion.FromAccountID,
			&suggestion.ToAccountID,
			&suggestion.Amount,
			&suggestion.CurrencyCode,
			&suggestion.Reason,
			&suggestion.Source,
			&suggestion.Status,
			&suggestion.ExpiresAt,
			&suggestion.UserActionAt,
			&suggestion.UserNotes,
			&suggestion.CreatedAt,
			&suggestion.UpdatedAt,
		)
		if err != nil {
			return nil, err
		}

		suggestions = append(suggestions, suggestion)
	}

	if err = rows.Err(); err != nil {
		return nil, err
	}

	return suggestions, nil
}
