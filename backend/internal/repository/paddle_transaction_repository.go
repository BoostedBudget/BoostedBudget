package repository

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	appcrypto "github.com/maxnussbaum/BoostedBudget/internal/crypto"
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

	// GetFailedTransactions retrieves failed/past_due/canceled transactions (admin view), ordered by created_at DESC.
	// Returns the transactions and total count for pagination.
	GetFailedTransactions(ctx context.Context, limit, offset int) ([]domain.PaddleTransaction, int, error)
}

// paddleTransactionRepository implements PaddleTransactionRepository using PostgreSQL.
type paddleTransactionRepository struct {
	db  DB
	enc *appcrypto.FieldEncryptor
}

// NewPaddleTransactionRepository creates a new PaddleTransactionRepository instance.
func NewPaddleTransactionRepository(db DB, enc *appcrypto.FieldEncryptor) PaddleTransactionRepository {
	return &paddleTransactionRepository{db: db, enc: enc}
}

const paddleTransactionSelectFields = `
	id, user_id, transaction_id, subscription_id, customer_id,
	amount, currency, status, payment_method_type,
	billing_period_start, billing_period_end, payload,
	created_at, updated_at
`

// scanPaddleTransaction scans a row into a PaddleTransaction struct and decrypts sensitive fields.
func (r *paddleTransactionRepository) scanPaddleTransaction(row interface {
	Scan(...interface{}) error
}, txn *domain.PaddleTransaction) error {
	var rawPayload string
	if err := row.Scan(
		&txn.ID, &txn.UserID, &txn.TransactionID, &txn.SubscriptionID, &txn.CustomerID,
		&txn.Amount, &txn.Currency, &txn.Status, &txn.PaymentMethodType,
		&txn.BillingPeriodStart, &txn.BillingPeriodEnd, &rawPayload,
		&txn.CreatedAt, &txn.UpdatedAt,
	); err != nil {
		return err
	}

	var err error
	txn.Payload, err = r.enc.DecryptJSON(rawPayload)
	if err != nil {
		return fmt.Errorf("decrypt payload: %w", err)
	}

	txn.TransactionID, err = r.enc.DecryptString(txn.TransactionID)
	if err != nil {
		return fmt.Errorf("decrypt transaction_id: %w", err)
	}

	txn.SubscriptionID, err = r.enc.DecryptNullableString(txn.SubscriptionID)
	if err != nil {
		return fmt.Errorf("decrypt subscription_id: %w", err)
	}

	txn.CustomerID, err = r.enc.DecryptNullableString(txn.CustomerID)
	if err != nil {
		return fmt.Errorf("decrypt customer_id: %w", err)
	}

	return nil
}

// Create stores a new Paddle transaction in the database.
func (r *paddleTransactionRepository) Create(ctx context.Context, txn *domain.PaddleTransaction) error {
	if txn.ID == "" {
		txn.ID = uuid.New().String()
	}

	encPayload, err := r.enc.EncryptJSON(txn.Payload)
	if err != nil {
		return fmt.Errorf("encrypt payload: %w", err)
	}

	encTransactionID, err := r.enc.EncryptString(txn.TransactionID)
	if err != nil {
		return fmt.Errorf("encrypt transaction_id: %w", err)
	}
	transactionIDBlindIdx := r.enc.BlindIndex(txn.TransactionID)

	encSubscriptionID, err := r.enc.EncryptNullableString(txn.SubscriptionID)
	if err != nil {
		return fmt.Errorf("encrypt subscription_id: %w", err)
	}

	encCustomerID, err := r.enc.EncryptNullableString(txn.CustomerID)
	if err != nil {
		return fmt.Errorf("encrypt customer_id: %w", err)
	}

	query := `
		INSERT INTO paddle_transactions (
			id, user_id, transaction_id, transaction_id_blind_idx, subscription_id, customer_id,
			amount, currency, status, payment_method_type,
			billing_period_start, billing_period_end, payload
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)
		RETURNING created_at, updated_at
	`

	err = r.db.QueryRow(ctx, query,
		txn.ID,
		txn.UserID,
		encTransactionID,
		transactionIDBlindIdx,
		encSubscriptionID,
		encCustomerID,
		txn.Amount,
		txn.Currency,
		txn.Status,
		txn.PaymentMethodType,
		txn.BillingPeriodStart,
		txn.BillingPeriodEnd,
		encPayload,
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
	query := `SELECT ` + paddleTransactionSelectFields + ` FROM paddle_transactions WHERE transaction_id_blind_idx = $1`

	var txn domain.PaddleTransaction
	err := r.scanPaddleTransaction(r.db.QueryRow(ctx, query, r.enc.BlindIndex(transactionID)), &txn)
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
		if err := r.scanPaddleTransaction(rows, &txn); err != nil {
			return nil, fmt.Errorf("failed to scan paddle transaction: %w", err)
		}
		txns = append(txns, txn)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating paddle transactions: %w", err)
	}

	return txns, nil
}

// GetFailedTransactions retrieves failed/past_due/canceled transactions for admin view.
func (r *paddleTransactionRepository) GetFailedTransactions(ctx context.Context, limit, offset int) ([]domain.PaddleTransaction, int, error) {
	const failedStatuses = `('failed', 'past_due', 'canceled')`

	var total int
	if err := r.db.QueryRow(ctx, `SELECT count(*) FROM paddle_transactions WHERE status IN `+failedStatuses).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("failed to count failed transactions: %w", err)
	}

	rows, err := r.db.Query(ctx, `SELECT `+paddleTransactionSelectFields+` FROM paddle_transactions WHERE status IN `+failedStatuses+` ORDER BY created_at DESC LIMIT $1 OFFSET $2`, limit, offset)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to get failed transactions: %w", err)
	}
	defer rows.Close()

	var txns []domain.PaddleTransaction
	for rows.Next() {
		var txn domain.PaddleTransaction
		if err := r.scanPaddleTransaction(rows, &txn); err != nil {
			return nil, 0, fmt.Errorf("failed to scan failed transaction: %w", err)
		}
		txns = append(txns, txn)
	}

	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("error iterating failed transactions: %w", err)
	}

	return txns, total, nil
}
