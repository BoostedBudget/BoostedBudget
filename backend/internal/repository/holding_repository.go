package repository

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
)

// HoldingRepository defines the interface for holding data access.
type HoldingRepository interface {
	// ReplaceForAccount atomically replaces all holdings for an account.
	// Deletes all existing holdings for the account, then inserts the new ones.
	// The operation is wrapped in a database transaction.
	ReplaceForAccount(ctx context.Context, accountID, userID string, holdings []domain.Holding) error

	// GetByUserID retrieves all holdings for a user.
	GetByUserID(ctx context.Context, userID string) ([]domain.Holding, error)

	// GetByAccountID retrieves all holdings for a specific account.
	GetByAccountID(ctx context.Context, accountID string) ([]domain.Holding, error)
}

type holdingRepository struct {
	db DB
}

// NewHoldingRepository creates a new HoldingRepository instance.
func NewHoldingRepository(db DB) HoldingRepository {
	return &holdingRepository{db: db}
}

// ReplaceForAccount atomically replaces all holdings for an account.
// Wraps DELETE + INSERT in a single transaction.
func (r *holdingRepository) ReplaceForAccount(ctx context.Context, accountID, userID string, holdings []domain.Holding) error {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	// Delete all existing holdings for this account
	deleteQuery := `DELETE FROM holdings WHERE account_id = $1 AND user_id = $2`
	if _, err := tx.Exec(ctx, deleteQuery, accountID, userID); err != nil {
		return err
	}

	// Insert new holdings
	for i := range holdings {
		h := &holdings[i]
		if h.ID == "" {
			h.ID = uuid.New().String()
		}
		now := time.Now()
		h.CreatedAt = now
		h.UpdatedAt = now

		insertQuery := `
			INSERT INTO holdings (
				id, user_id, account_id, security_id,
				quantity, cost_basis, institution_value, institution_price,
				created_at, updated_at
			) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		`
		if _, err := tx.Exec(ctx, insertQuery,
			h.ID,
			h.UserID,
			h.AccountID,
			h.SecurityID,
			h.Quantity,
			h.CostBasis,
			h.InstitutionValue,
			h.InstitutionPrice,
			h.CreatedAt,
			h.UpdatedAt,
		); err != nil {
			return err
		}
	}

	return tx.Commit(ctx)
}

// GetByUserID retrieves all holdings for a user.
func (r *holdingRepository) GetByUserID(ctx context.Context, userID string) ([]domain.Holding, error) {
	query := `
		SELECT id, user_id, account_id, security_id,
			quantity, cost_basis, institution_value, institution_price,
			created_at, updated_at
		FROM holdings
		WHERE user_id = $1
		ORDER BY account_id, security_id
	`

	rows, err := r.db.Query(ctx, query, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var holdings []domain.Holding
	for rows.Next() {
		h := domain.Holding{}
		if err := rows.Scan(
			&h.ID,
			&h.UserID,
			&h.AccountID,
			&h.SecurityID,
			&h.Quantity,
			&h.CostBasis,
			&h.InstitutionValue,
			&h.InstitutionPrice,
			&h.CreatedAt,
			&h.UpdatedAt,
		); err != nil {
			return nil, err
		}
		holdings = append(holdings, h)
	}

	if holdings == nil {
		holdings = []domain.Holding{}
	}
	return holdings, nil
}

// GetByAccountID retrieves all holdings for a specific account.
func (r *holdingRepository) GetByAccountID(ctx context.Context, accountID string) ([]domain.Holding, error) {
	query := `
		SELECT id, user_id, account_id, security_id,
			quantity, cost_basis, institution_value, institution_price,
			created_at, updated_at
		FROM holdings
		WHERE account_id = $1
		ORDER BY security_id
	`

	rows, err := r.db.Query(ctx, query, accountID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var holdings []domain.Holding
	for rows.Next() {
		h := domain.Holding{}
		if err := rows.Scan(
			&h.ID,
			&h.UserID,
			&h.AccountID,
			&h.SecurityID,
			&h.Quantity,
			&h.CostBasis,
			&h.InstitutionValue,
			&h.InstitutionPrice,
			&h.CreatedAt,
			&h.UpdatedAt,
		); err != nil {
			return nil, err
		}
		holdings = append(holdings, h)
	}

	if holdings == nil {
		holdings = []domain.Holding{}
	}
	return holdings, nil
}
