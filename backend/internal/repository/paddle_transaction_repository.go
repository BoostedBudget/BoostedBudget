package repository

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
)

var (
	// ErrPaddleTransactionNotFound is returned when a transaction is not found in the database.
	ErrPaddleTransactionNotFound = errors.New("paddle transaction not found")

	// ErrPaddleTransactionDuplicate is returned when a transaction with the same transaction_id already exists.
	ErrPaddleTransactionDuplicate = errors.New("paddle transaction with this transaction_id already exists")
)

// PaddleTransactionRepository defines the interface for Paddle transaction data access.
// Issues #747 #748: Paddle webhook handlers — payment audit trail
type PaddleTransactionRepository interface {
	// Create stores a new Paddle transaction in the database.
	// Returns ErrPaddleTransactionDuplicate if transaction_id already exists.
	Create(ctx context.Context, txn *domain.PaddleTransaction) error

	// GetByTransactionID retrieves a transaction by Paddle's transaction ID.
	// Returns ErrPaddleTransactionNotFound if not found.
	GetByTransactionID(ctx context.Context, transactionID string) (*domain.PaddleTransaction, error)

	// GetByUserID retrieves all transactions for a user, ordered by created_at DESC.
	GetByUserID(ctx context.Context, userID string) ([]domain.PaddleTransaction, error)
}

// paddleTransactionRepository implements PaddleTransactionRepository using PostgreSQL.
type paddleTransactionRepository struct {
	db DB
}

// NewPaddleTransactionRepository creates a new PaddleTransactionRepository instance.
func NewPaddleTransactionRepository(db DB) PaddleTransactionRepository {
	return &paddleTransactionRepository{db: db}
}

const paddleTransactionSelectFields = `
	id, user_id, transaction_id, subscription_id, customer_id,
	amount, currency, status, payment_method_type,
	billing_period_start, billing_period_end, payload,
	created_at, updated_at
`

// Create stores a new Paddle transaction in the database.
func (r *paddleTransactionRepository) Create(ctx context.Context, txn *domain.PaddleTransaction) error {
	if txn.ID == "" {
		txn.ID = uuid.New().String()
	}

	query := `
		INSERT INTO paddle_transactions (
			id, user_id, transaction_id, subscription_id, customer_id,
			amount, currency, status, payment_method_type,
			billing_period_start, billing_period_end, payload
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
		RETURNING created_at, updated_at
	`

	err := r.db.QueryRow(ctx, query,
		txn.ID,
		txn.UserID,
		txn.TransactionID,
		txn.SubscriptionID,
		txn.CustomerID,
		txn.Amount,
		txn.Currency,
		txn.Status,
		txn.PaymentMethodType,
		txn.BillingPeriodStart,
		txn.BillingPeriodEnd,
		txn.Payload,
	).Scan(&txn.CreatedAt, &txn.UpdatedAt)

	if err != nil {
		if pgErr, ok := err.(*pgconn.PgError); ok && pgErr.Code == "23505" {
			return ErrPaddleTransactionDuplicate
		}
		return fmt.Errorf("failed to create paddle transaction: %w", err)
	}

	return nil
}

// GetByTransactionID retrieves a transaction by Paddle's transaction ID.
func (r *paddleTransactionRepository) GetByTransactionID(ctx context.Context, transactionID string) (*domain.PaddleTransaction, error) {
	query := `SELECT ` + paddleTransactionSelectFields + ` FROM paddle_transactions WHERE transaction_id = $1`

	var txn domain.PaddleTransaction
	err := r.db.QueryRow(ctx, query, transactionID).Scan(
		&txn.ID, &txn.UserID, &txn.TransactionID, &txn.SubscriptionID, &txn.CustomerID,
		&txn.Amount, &txn.Currency, &txn.Status, &txn.PaymentMethodType,
		&txn.BillingPeriodStart, &txn.BillingPeriodEnd, &txn.Payload,
		&txn.CreatedAt, &txn.UpdatedAt,
	)

	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrPaddleTransactionNotFound
		}
		return nil, fmt.Errorf("failed to get paddle transaction by transaction_id: %w", err)
	}

	return &txn, nil
}

// GetByUserID retrieves all transactions for a user, ordered by created_at DESC.
func (r *paddleTransactionRepository) GetByUserID(ctx context.Context, userID string) ([]domain.PaddleTransaction, error) {
	query := `SELECT ` + paddleTransactionSelectFields + ` FROM paddle_transactions WHERE user_id = $1 ORDER BY created_at DESC`

	rows, err := r.db.Query(ctx, query, userID)
	if err != nil {
		return nil, fmt.Errorf("failed to get paddle transactions by user_id: %w", err)
	}
	defer rows.Close()

	var txns []domain.PaddleTransaction
	for rows.Next() {
		var txn domain.PaddleTransaction
		if err := rows.Scan(
			&txn.ID, &txn.UserID, &txn.TransactionID, &txn.SubscriptionID, &txn.CustomerID,
			&txn.Amount, &txn.Currency, &txn.Status, &txn.PaymentMethodType,
			&txn.BillingPeriodStart, &txn.BillingPeriodEnd, &txn.Payload,
			&txn.CreatedAt, &txn.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("failed to scan paddle transaction: %w", err)
		}
		txns = append(txns, txn)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating paddle transactions: %w", err)
	}

	return txns, nil
}
