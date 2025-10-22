package repository

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
)

var (
	// ErrAccountNotFound is returned when an account is not found in the database
	ErrAccountNotFound = errors.New("account not found")
	// ErrDuplicateAccount is returned when attempting to create an account that already exists
	ErrDuplicateAccount = errors.New("account already exists for this user")
)

// AccountRepository defines the interface for account data access
type AccountRepository interface {
	// Create creates a new account in the database
	Create(ctx context.Context, account *domain.Account) error

	// CreateBatch creates multiple accounts in a single transaction
	CreateBatch(ctx context.Context, accounts []domain.Account) error

	// GetByLinkedItemID retrieves all accounts for a specific linked item
	GetByLinkedItemID(ctx context.Context, linkedItemID string) ([]domain.Account, error)

	// GetByUserID retrieves all accounts for a specific user
	GetByUserID(ctx context.Context, userID string) ([]domain.Account, error)

	// GetAccountsWithInstitution retrieves all accounts for a user with institution names
	// This method performs a LEFT JOIN with linked_items to include institution_name
	GetAccountsWithInstitution(ctx context.Context, userID string) ([]domain.Account, error)

	// CountByUserID counts the total number of accounts for a user
	CountByUserID(ctx context.Context, userID string) (int, error)
}

// accountRepository implements AccountRepository using PostgreSQL
type accountRepository struct {
	db *sql.DB
}

// NewAccountRepository creates a new AccountRepository instance
func NewAccountRepository(db *sql.DB) AccountRepository {
	return &accountRepository{db: db}
}

// Create creates a new account in the database
func (r *accountRepository) Create(ctx context.Context, account *domain.Account) error {
	// Generate UUID if not already set
	if account.ID == "" {
		account.ID = uuid.New().String()
	}

	// Set timestamps
	now := time.Now()
	account.CreatedAt = now
	account.UpdatedAt = now

	query := `
		INSERT INTO accounts
		(id, linked_item_id, user_id, plaid_account_id, name, official_name,
		 type, subtype, mask, current_balance, available_balance, currency_code,
		 created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14)
	`

	_, err := r.db.ExecContext(ctx, query,
		account.ID,
		account.LinkedItemID,
		account.UserID,
		account.PlaidAccountID,
		account.Name,
		account.OfficialName,
		account.Type,
		account.Subtype,
		account.Mask,
		account.CurrentBalance,
		account.AvailableBalance,
		account.CurrencyCode,
		account.CreatedAt,
		account.UpdatedAt,
	)

	if err != nil {
		// Check for unique constraint violations
		if strings.Contains(err.Error(), "duplicate key") || strings.Contains(err.Error(), "unique constraint") {
			if strings.Contains(err.Error(), "unique_user_account") {
				return ErrDuplicateAccount
			}
		}
		return err
	}

	return nil
}

// CreateBatch creates multiple accounts in a single transaction
func (r *accountRepository) CreateBatch(ctx context.Context, accounts []domain.Account) error {
	if len(accounts) == 0 {
		return nil
	}

	// Start transaction
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback() // Rollback if not committed

	query := `
		INSERT INTO accounts
		(id, linked_item_id, user_id, plaid_account_id, name, official_name,
		 type, subtype, mask, current_balance, available_balance, currency_code,
		 created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14)
	`

	now := time.Now()

	for i := range accounts {
		account := &accounts[i]

		// Generate UUID if not already set
		if account.ID == "" {
			account.ID = uuid.New().String()
		}

		// Set timestamps
		account.CreatedAt = now
		account.UpdatedAt = now

		_, err := tx.ExecContext(ctx, query,
			account.ID,
			account.LinkedItemID,
			account.UserID,
			account.PlaidAccountID,
			account.Name,
			account.OfficialName,
			account.Type,
			account.Subtype,
			account.Mask,
			account.CurrentBalance,
			account.AvailableBalance,
			account.CurrencyCode,
			account.CreatedAt,
			account.UpdatedAt,
		)

		if err != nil {
			// Check for unique constraint violations
			if strings.Contains(err.Error(), "duplicate key") || strings.Contains(err.Error(), "unique constraint") {
				if strings.Contains(err.Error(), "unique_user_account") {
					return ErrDuplicateAccount
				}
			}
			return err
		}
	}

	// Commit transaction
	return tx.Commit()
}

// GetByLinkedItemID retrieves all accounts for a specific linked item
func (r *accountRepository) GetByLinkedItemID(ctx context.Context, linkedItemID string) ([]domain.Account, error) {
	query := `
		SELECT id, linked_item_id, user_id, plaid_account_id,
		       name, official_name, type, subtype, mask,
		       current_balance, available_balance, currency_code,
		       created_at, updated_at
		FROM accounts
		WHERE linked_item_id = $1
		ORDER BY created_at ASC
	`

	rows, err := r.db.QueryContext(ctx, query, linkedItemID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var accounts []domain.Account
	for rows.Next() {
		var account domain.Account
		err := rows.Scan(
			&account.ID,
			&account.LinkedItemID,
			&account.UserID,
			&account.PlaidAccountID,
			&account.Name,
			&account.OfficialName,
			&account.Type,
			&account.Subtype,
			&account.Mask,
			&account.CurrentBalance,
			&account.AvailableBalance,
			&account.CurrencyCode,
			&account.CreatedAt,
			&account.UpdatedAt,
		)
		if err != nil {
			return nil, err
		}
		accounts = append(accounts, account)
	}

	if err = rows.Err(); err != nil {
		return nil, err
	}

	// Return empty slice instead of nil if no accounts found
	if accounts == nil {
		accounts = []domain.Account{}
	}

	return accounts, nil
}

// GetByUserID retrieves all accounts for a specific user
func (r *accountRepository) GetByUserID(ctx context.Context, userID string) ([]domain.Account, error) {
	query := `
		SELECT id, linked_item_id, user_id, plaid_account_id,
		       name, official_name, type, subtype, mask,
		       current_balance, available_balance, currency_code,
		       created_at, updated_at
		FROM accounts
		WHERE user_id = $1
		ORDER BY created_at ASC
	`

	rows, err := r.db.QueryContext(ctx, query, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var accounts []domain.Account
	for rows.Next() {
		var account domain.Account
		err := rows.Scan(
			&account.ID,
			&account.LinkedItemID,
			&account.UserID,
			&account.PlaidAccountID,
			&account.Name,
			&account.OfficialName,
			&account.Type,
			&account.Subtype,
			&account.Mask,
			&account.CurrentBalance,
			&account.AvailableBalance,
			&account.CurrencyCode,
			&account.CreatedAt,
			&account.UpdatedAt,
		)
		if err != nil {
			return nil, err
		}
		accounts = append(accounts, account)
	}

	if err = rows.Err(); err != nil {
		return nil, err
	}

	// Return empty slice instead of nil if no accounts found
	if accounts == nil {
		accounts = []domain.Account{}
	}

	return accounts, nil
}

// GetAccountsWithInstitution retrieves all accounts for a user with institution names from linked_items
func (r *accountRepository) GetAccountsWithInstitution(ctx context.Context, userID string) ([]domain.Account, error) {
	query := `
		SELECT a.id, a.linked_item_id, a.user_id, a.plaid_account_id,
		       a.name, a.official_name, a.type, a.subtype, a.mask,
		       a.current_balance, a.available_balance, a.currency_code,
		       a.created_at, a.updated_at, l.institution_name
		FROM accounts a
		LEFT JOIN linked_items l ON a.linked_item_id = l.id
		WHERE a.user_id = $1
		ORDER BY a.created_at ASC
	`

	rows, err := r.db.QueryContext(ctx, query, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var accounts []domain.Account
	for rows.Next() {
		var account domain.Account
		err := rows.Scan(
			&account.ID,
			&account.LinkedItemID,
			&account.UserID,
			&account.PlaidAccountID,
			&account.Name,
			&account.OfficialName,
			&account.Type,
			&account.Subtype,
			&account.Mask,
			&account.CurrentBalance,
			&account.AvailableBalance,
			&account.CurrencyCode,
			&account.CreatedAt,
			&account.UpdatedAt,
			&account.InstitutionName,
		)
		if err != nil {
			return nil, err
		}
		accounts = append(accounts, account)
	}

	if err = rows.Err(); err != nil {
		return nil, err
	}

	// Return empty slice instead of nil if no accounts found
	if accounts == nil {
		accounts = []domain.Account{}
	}

	return accounts, nil
}

// CountByUserID counts the total number of accounts for a user
func (r *accountRepository) CountByUserID(ctx context.Context, userID string) (int, error) {
	query := `
		SELECT COUNT(*)
		FROM accounts
		WHERE user_id = $1
	`

	var count int
	err := r.db.QueryRowContext(ctx, query, userID).Scan(&count)
	if err != nil {
		return 0, err
	}

	return count, nil
}
