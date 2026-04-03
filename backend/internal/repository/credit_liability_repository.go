package repository

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
)

// CreditLiabilityRepository defines the interface for credit liability data access.
type CreditLiabilityRepository interface {
	// ReplaceForAccount atomically replaces the credit liability for an account.
	// Deletes APR rows first, then deletes the existing credit liability, then inserts the new one with its APRs.
	// Wrapped in a single DB transaction.
	ReplaceForAccount(ctx context.Context, accountID, userID string, liability *domain.CreditLiability) error

	// GetByUserID retrieves all credit liabilities for a user (without APR rows).
	GetByUserID(ctx context.Context, userID string) ([]domain.CreditLiability, error)

	// GetByAccountID retrieves the credit liability for a specific account (without APR rows).
	GetByAccountID(ctx context.Context, accountID string) (*domain.CreditLiability, error)
}

type creditLiabilityRepository struct {
	db DB
}

// NewCreditLiabilityRepository creates a new CreditLiabilityRepository instance.
func NewCreditLiabilityRepository(db DB) CreditLiabilityRepository {
	return &creditLiabilityRepository{db: db}
}

// ReplaceForAccount atomically replaces the credit liability for an account.
func (r *creditLiabilityRepository) ReplaceForAccount(ctx context.Context, accountID, userID string, liability *domain.CreditLiability) error {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	// Delete APRs first (explicit even though CASCADE would handle it)
	_, err = tx.Exec(ctx,
		`DELETE FROM credit_liability_aprs WHERE credit_liability_id IN (SELECT id FROM credit_liabilities WHERE account_id = $1 AND user_id = $2)`,
		accountID, userID)
	if err != nil {
		return err
	}

	// Delete existing credit liability
	_, err = tx.Exec(ctx,
		`DELETE FROM credit_liabilities WHERE account_id = $1 AND user_id = $2`,
		accountID, userID)
	if err != nil {
		return err
	}

	// Insert new credit liability
	if liability.ID == "" {
		liability.ID = uuid.New().String()
	}
	now := time.Now()
	liability.SyncedAt = now
	liability.CreatedAt = now
	liability.UpdatedAt = now

	_, err = tx.Exec(ctx,
		`INSERT INTO credit_liabilities (id, user_id, account_id, is_overdue, last_payment_amount, last_payment_date, last_statement_issue_date, last_statement_balance, minimum_payment_amount, next_payment_due_date, synced_at, created_at, updated_at) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)`,
		liability.ID, liability.UserID, liability.AccountID,
		liability.IsOverdue, liability.LastPaymentAmount, liability.LastPaymentDate,
		liability.LastStatementIssueDate, liability.LastStatementBalance,
		liability.MinimumPaymentAmount, liability.NextPaymentDueDate,
		liability.SyncedAt, liability.CreatedAt, liability.UpdatedAt)
	if err != nil {
		return err
	}

	// Insert APRs
	for i := range liability.APRs {
		apr := &liability.APRs[i]
		if apr.ID == "" {
			apr.ID = uuid.New().String()
		}
		apr.CreditLiabilityID = liability.ID
		now2 := time.Now()
		apr.CreatedAt = now2
		apr.UpdatedAt = now2
		_, err = tx.Exec(ctx,
			`INSERT INTO credit_liability_aprs (id, credit_liability_id, rate_percentage, rate_type, balance_subject_to_rate, interest_charge_amount, created_at, updated_at) VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
			apr.ID, apr.CreditLiabilityID, apr.RatePercentage, apr.RateType,
			apr.BalanceSubjectToRate, apr.InterestChargeAmount,
			apr.CreatedAt, apr.UpdatedAt)
		if err != nil {
			return err
		}
	}

	return tx.Commit(ctx)
}

// GetByUserID retrieves all credit liabilities for a user (without APRs).
func (r *creditLiabilityRepository) GetByUserID(ctx context.Context, userID string) ([]domain.CreditLiability, error) {
	query := `
		SELECT id, user_id, account_id, is_overdue,
			last_payment_amount, last_payment_date,
			last_statement_issue_date, last_statement_balance,
			minimum_payment_amount, next_payment_due_date,
			synced_at, created_at, updated_at
		FROM credit_liabilities
		WHERE user_id = $1
		ORDER BY account_id
	`

	rows, err := r.db.Query(ctx, query, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	liabilities := []domain.CreditLiability{}
	for rows.Next() {
		cl := domain.CreditLiability{}
		if err := rows.Scan(
			&cl.ID, &cl.UserID, &cl.AccountID, &cl.IsOverdue,
			&cl.LastPaymentAmount, &cl.LastPaymentDate,
			&cl.LastStatementIssueDate, &cl.LastStatementBalance,
			&cl.MinimumPaymentAmount, &cl.NextPaymentDueDate,
			&cl.SyncedAt, &cl.CreatedAt, &cl.UpdatedAt,
		); err != nil {
			return nil, err
		}
		liabilities = append(liabilities, cl)
	}

	return liabilities, nil
}

// GetByAccountID retrieves the credit liability for a specific account (without APRs).
// Returns nil, nil when not found.
func (r *creditLiabilityRepository) GetByAccountID(ctx context.Context, accountID string) (*domain.CreditLiability, error) {
	query := `
		SELECT id, user_id, account_id, is_overdue,
			last_payment_amount, last_payment_date,
			last_statement_issue_date, last_statement_balance,
			minimum_payment_amount, next_payment_due_date,
			synced_at, created_at, updated_at
		FROM credit_liabilities
		WHERE account_id = $1
	`

	rows, err := r.db.Query(ctx, query, accountID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	if !rows.Next() {
		return nil, nil
	}

	cl := &domain.CreditLiability{}
	if err := rows.Scan(
		&cl.ID, &cl.UserID, &cl.AccountID, &cl.IsOverdue,
		&cl.LastPaymentAmount, &cl.LastPaymentDate,
		&cl.LastStatementIssueDate, &cl.LastStatementBalance,
		&cl.MinimumPaymentAmount, &cl.NextPaymentDueDate,
		&cl.SyncedAt, &cl.CreatedAt, &cl.UpdatedAt,
	); err != nil {
		return nil, err
	}

	return cl, nil
}

