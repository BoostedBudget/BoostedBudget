package repository

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
)

// InvestmentTransactionRepository defines the interface for investment transaction data access.
type InvestmentTransactionRepository interface {
	// UpsertBatch inserts investment transactions, skipping duplicates by plaid_investment_transaction_id.
	// Returns the number of rows actually inserted (duplicates are silently skipped).
	UpsertBatch(ctx context.Context, transactions []domain.InvestmentTransaction) (int, error)

	// GetByUserID retrieves investment transactions for a user, ordered by date descending.
	GetByUserID(ctx context.Context, userID string, limit, offset int) ([]domain.InvestmentTransaction, error)

	// GetByAccountID retrieves investment transactions for a specific account.
	GetByAccountID(ctx context.Context, accountID string) ([]domain.InvestmentTransaction, error)

	// GetByUserAccountID returns all investment transactions for a logical account (all chain members)
	// identified by the stable user_account_id that survives Plaid re-links.
	GetByUserAccountID(ctx context.Context, userAccountID string, limit, offset int) ([]domain.InvestmentTransaction, error)
}

type investmentTransactionRepository struct {
	db DB
}

// NewInvestmentTransactionRepository creates a new InvestmentTransactionRepository instance.
func NewInvestmentTransactionRepository(db DB) InvestmentTransactionRepository {
	return &investmentTransactionRepository{db: db}
}

// UpsertBatch inserts investment transactions using ON CONFLICT DO NOTHING for idempotency.
// Returns the count of actually-inserted rows (duplicates are skipped and not counted).
func (r *investmentTransactionRepository) UpsertBatch(ctx context.Context, transactions []domain.InvestmentTransaction) (int, error) {
	if len(transactions) == 0 {
		return 0, nil
	}

	totalInserted := 0

	for i := range transactions {
		txn := &transactions[i]
		if txn.ID == "" {
			txn.ID = uuid.New().String()
		}
		txn.CreatedAt = time.Now()

		// COUNT(*) FILTER trick: returns 1 if inserted, 0 if skipped (ON CONFLICT DO NOTHING)
		query := `
			WITH ins AS (
				INSERT INTO investment_transactions (
					id, user_id, account_id, security_id,
					plaid_investment_transaction_id,
					type, subtype, name,
					quantity, amount, price, fees, date,
					created_at
				) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14)
				ON CONFLICT (plaid_investment_transaction_id) DO NOTHING
				RETURNING id
			)
			SELECT COUNT(*) AS inserted FROM ins
		`

		var inserted int
		err := r.db.QueryRow(ctx, query,
			txn.ID,
			txn.UserID,
			txn.AccountID,
			txn.SecurityID,
			txn.PlaidInvestmentTransactionID,
			txn.Type,
			txn.Subtype,
			txn.Name,
			txn.Quantity,
			txn.Amount,
			txn.Price,
			txn.Fees,
			txn.Date,
			txn.CreatedAt,
		).Scan(&inserted)
		if err != nil {
			return totalInserted, err
		}
		totalInserted += inserted
	}

	return totalInserted, nil
}

// GetByUserID retrieves investment transactions for a user with pagination.
func (r *investmentTransactionRepository) GetByUserID(ctx context.Context, userID string, limit, offset int) ([]domain.InvestmentTransaction, error) {
	// JOIN accounts to filter out replaced accounts — after a Plaid re-link the old
	// account's status becomes 'replaced', so this join ensures only active transactions appear.
	query := `
		SELECT investment_transactions.id, investment_transactions.user_id, investment_transactions.account_id,
			investment_transactions.user_account_id, investment_transactions.security_id,
			investment_transactions.plaid_investment_transaction_id,
			investment_transactions.type, investment_transactions.subtype, investment_transactions.name,
			investment_transactions.quantity, investment_transactions.amount, investment_transactions.price,
			investment_transactions.fees, investment_transactions.date,
			investment_transactions.created_at
		FROM investment_transactions
		JOIN accounts a ON a.id = investment_transactions.account_id
		WHERE investment_transactions.user_id = $1
		  AND a.status = 'active'
		ORDER BY investment_transactions.date DESC
		LIMIT $2 OFFSET $3
	`

	rows, err := r.db.Query(ctx, query, userID, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var txns []domain.InvestmentTransaction
	for rows.Next() {
		txn := domain.InvestmentTransaction{}
		if err := rows.Scan(
			&txn.ID,
			&txn.UserID,
			&txn.AccountID,
			&txn.UserAccountID,
			&txn.SecurityID,
			&txn.PlaidInvestmentTransactionID,
			&txn.Type,
			&txn.Subtype,
			&txn.Name,
			&txn.Quantity,
			&txn.Amount,
			&txn.Price,
			&txn.Fees,
			&txn.Date,
			&txn.CreatedAt,
		); err != nil {
			return nil, err
		}
		txns = append(txns, txn)
	}

	if txns == nil {
		txns = []domain.InvestmentTransaction{}
	}
	return txns, nil
}

// GetByAccountID retrieves investment transactions for a specific account.
func (r *investmentTransactionRepository) GetByAccountID(ctx context.Context, accountID string) ([]domain.InvestmentTransaction, error) {
	query := `
		SELECT id, user_id, account_id, security_id,
			plaid_investment_transaction_id,
			type, subtype, name,
			quantity, amount, price, fees, date,
			created_at
		FROM investment_transactions
		WHERE account_id = $1
		ORDER BY date DESC
	`

	rows, err := r.db.Query(ctx, query, accountID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var txns []domain.InvestmentTransaction
	for rows.Next() {
		txn := domain.InvestmentTransaction{}
		if err := rows.Scan(
			&txn.ID,
			&txn.UserID,
			&txn.AccountID,
			&txn.SecurityID,
			&txn.PlaidInvestmentTransactionID,
			&txn.Type,
			&txn.Subtype,
			&txn.Name,
			&txn.Quantity,
			&txn.Amount,
			&txn.Price,
			&txn.Fees,
			&txn.Date,
			&txn.CreatedAt,
		); err != nil {
			return nil, err
		}
		txns = append(txns, txn)
	}

	if txns == nil {
		txns = []domain.InvestmentTransaction{}
	}
	return txns, nil
}

func (r *investmentTransactionRepository) GetByUserAccountID(ctx context.Context, userAccountID string, limit, offset int) ([]domain.InvestmentTransaction, error) {
	// Returns all investment transactions for a logical account identified by the stable user_account_id.
	// This covers all physical account_ids in a re-link chain.
	query := `
		SELECT id, user_id, account_id, user_account_id, security_id,
			plaid_investment_transaction_id,
			type, subtype, name,
			quantity, amount, price, fees, date,
			created_at
		FROM investment_transactions
		WHERE user_account_id = $1
		ORDER BY date DESC
		LIMIT $2 OFFSET $3
	`

	rows, err := r.db.Query(ctx, query, userAccountID, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var txns []domain.InvestmentTransaction
	for rows.Next() {
		txn := domain.InvestmentTransaction{}
		if err := rows.Scan(
			&txn.ID,
			&txn.UserID,
			&txn.AccountID,
			&txn.UserAccountID,
			&txn.SecurityID,
			&txn.PlaidInvestmentTransactionID,
			&txn.Type,
			&txn.Subtype,
			&txn.Name,
			&txn.Quantity,
			&txn.Amount,
			&txn.Price,
			&txn.Fees,
			&txn.Date,
			&txn.CreatedAt,
		); err != nil {
			return nil, err
		}
		txns = append(txns, txn)
	}

	if txns == nil {
		txns = []domain.InvestmentTransaction{}
	}
	return txns, nil
}
