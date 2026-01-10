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

	// CountActiveByLinkedItemID counts active accounts for a linked item.
	// Only counts accounts with status='active', excludes 'replaced' accounts.
	// Used for zero-account item detection and cleanup.
	// Issue #668: Plaid Item Management for Cost Optimization
	CountActiveByLinkedItemID(ctx context.Context, linkedItemID string) (int, error)

	// Delete removes an account from the database with ownership validation.
	// The account is only deleted if both the account ID and user ID match (WHERE id = $1 AND user_id = $2).
	// This prevents users from deleting accounts they don't own.
	//
	// Returns ErrAccountNotFound if:
	//   - The account doesn't exist
	//   - The account exists but belongs to a different user (ownership check fails)
	Delete(ctx context.Context, accountID, userID string) error

	// GetByIDAndUser retrieves an account by ID with ownership validation.
	// Returns ErrAccountNotFound if the account doesn't exist or belongs to a different user.
	GetByIDAndUser(ctx context.Context, accountID, userID string) (*domain.Account, error)

	// Update updates account customization fields with ownership validation.
	// Returns ErrAccountNotFound if the account doesn't exist or user doesn't own it.
	Update(ctx context.Context, account *domain.Account, userID string) error

	// UpdateDisplayOrders updates the display_order for multiple accounts atomically.
	// All accounts must belong to the specified user.
	// Returns ErrAccountNotFound if any account doesn't exist or user doesn't own it.
	UpdateDisplayOrders(ctx context.Context, userID string, orders []domain.AccountOrder) error

	// GetAccountsWithInstitutionFiltered retrieves accounts with institution names and hidden filtering.
	// If includeHidden is false, only returns accounts where is_hidden = false.
	// Results are sorted by display_order ASC (nulls last), then created_at ASC.
	GetAccountsWithInstitutionFiltered(ctx context.Context, userID string, includeHidden bool) ([]domain.Account, error)

	// GetByUserAccountAndStatus retrieves accounts by user_account_id and status.
	// This is used for querying accounts across re-links using stable user_account reference.
	// Issue #171: Plaid Re-Link Handling
	//
	// Returns empty slice if no accounts found (not an error).
	GetByUserAccountAndStatus(ctx context.Context, userAccountID, status string) ([]domain.Account, error)

	// GetByUserAccountID retrieves all accounts for a user_account (any status).
	// This returns both active and replaced accounts for historical view.
	// Issue #171: Plaid Re-Link Handling
	GetByUserAccountID(ctx context.Context, userAccountID string) ([]domain.Account, error)

	// UpdateStatus updates an account's status and soft-delete fields with ownership validation.
	// Used when marking accounts as replaced during re-link.
	// Issue #171: Plaid Re-Link Handling
	//
	// Returns ErrAccountNotFound if account doesn't exist or user doesn't own it.
	UpdateStatus(ctx context.Context, accountID, userID string, status domain.AccountStatus, replacedByID *string) error
}

// accountRepository implements AccountRepository using PostgreSQL
type accountRepository struct {
	db DB
}

// NewAccountRepository creates a new AccountRepository instance
func NewAccountRepository(db DB) AccountRepository {
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

	_, err := r.db.Exec(ctx, query,
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
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx) // Rollback if not committed

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

		_, err := tx.Exec(ctx, query,
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
	return tx.Commit(ctx)
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

	rows, err := r.db.Query(ctx, query, linkedItemID)
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

	if accounts != nil {
		return accounts, nil
	}
	return []domain.Account{}, nil
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

	rows, err := r.db.Query(ctx, query, userID)
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

	if accounts != nil {
		return accounts, nil
	}
	return []domain.Account{}, nil
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

	rows, err := r.db.Query(ctx, query, userID)
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

	if accounts != nil {
		return accounts, nil
	}
	return []domain.Account{}, nil
}

// CountByUserID counts the total number of accounts for a user
func (r *accountRepository) CountByUserID(ctx context.Context, userID string) (int, error) {
	query := `
		SELECT COUNT(*)
		FROM accounts
		WHERE user_id = $1
	`

	var count int
	err := r.db.QueryRow(ctx, query, userID).Scan(&count)
	if err != nil {
		return 0, err
	}

	return count, nil
}

// CountActiveByLinkedItemID counts active accounts for a linked item.
// Only counts accounts with status='active', excludes 'replaced' accounts.
// Issue #668: Plaid Item Management for Cost Optimization
func (r *accountRepository) CountActiveByLinkedItemID(ctx context.Context, linkedItemID string) (int, error) {
	query := `
		SELECT COUNT(*)
		FROM accounts
		WHERE linked_item_id = $1 AND status = 'active'
	`

	var count int
	err := r.db.QueryRow(ctx, query, linkedItemID).Scan(&count)
	if err != nil {
		return 0, err
	}

	return count, nil
}

// Delete removes an account from the database with ownership validation.
//
// Security: This method validates ownership via the WHERE clause (id AND user_id must match).
// A user can only delete accounts they own. If the account doesn't exist or belongs to
// a different user, the DELETE will affect 0 rows and ErrAccountNotFound is returned.
//
// Use case: Called when a user manually deletes an individual account from the UI.
// Note: For institution-level deletion, use LinkedItemRepository.Delete which CASCADE deletes all accounts.
func (r *accountRepository) Delete(ctx context.Context, accountID, userID string) error {
	query := `
		DELETE FROM accounts
		WHERE id = $1 AND user_id = $2
	`

	ct, err := r.db.Exec(ctx, query, accountID, userID)
	if err != nil {
		return err
	}

	// Check if any rows were affected (validates ownership and existence)
	if ct.RowsAffected() == 0 {
		// Account not found OR user doesn't own it (both cases return same error for security)
		return ErrAccountNotFound
	}

	return nil
}

// GetByIDAndUser retrieves an account by ID with ownership validation.
func (r *accountRepository) GetByIDAndUser(ctx context.Context, accountID, userID string) (*domain.Account, error) {
	query := `
		SELECT id, linked_item_id, user_id, plaid_account_id,
		       name, official_name, type, subtype, mask,
		       current_balance, available_balance, currency_code,
		       nickname, custom_color, custom_icon, custom_emoji,
		       display_order, is_hidden,
		       created_at, updated_at
		FROM accounts
		WHERE id = $1 AND user_id = $2
	`

	var account domain.Account
	err := r.db.QueryRow(ctx, query, accountID, userID).Scan(
		&account.ID, &account.LinkedItemID, &account.UserID, &account.PlaidAccountID,
		&account.Name, &account.OfficialName, &account.Type, &account.Subtype, &account.Mask,
		&account.CurrentBalance, &account.AvailableBalance, &account.CurrencyCode,
		&account.Nickname, &account.CustomColor, &account.CustomIcon, &account.CustomEmoji,
		&account.DisplayOrder, &account.IsHidden,
		&account.CreatedAt, &account.UpdatedAt,
	)

	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrAccountNotFound
		}
		return nil, err
	}

	return &account, nil
}

// Update updates account customization fields with ownership validation.
func (r *accountRepository) Update(ctx context.Context, account *domain.Account, userID string) error {
	account.UpdatedAt = time.Now()

	query := `
		UPDATE accounts
		SET nickname = $1, custom_color = $2, custom_icon = $3,
		    custom_emoji = $4, display_order = $5, is_hidden = $6, updated_at = $7
		WHERE id = $8 AND user_id = $9
	`

	ct, err := r.db.Exec(ctx, query,
		account.Nickname, account.CustomColor, account.CustomIcon,
		account.CustomEmoji, account.DisplayOrder, account.IsHidden, account.UpdatedAt,
		account.ID, userID,
	)
	if err != nil {
		return err
	}

	if ct.RowsAffected() == 0 {
		return ErrAccountNotFound
	}

	return nil
}

// UpdateDisplayOrders updates the display_order for multiple accounts atomically.
func (r *accountRepository) UpdateDisplayOrders(ctx context.Context, userID string, orders []domain.AccountOrder) error {
	if len(orders) == 0 {
		return nil
	}

	tx, err := r.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	query := `
		UPDATE accounts
		SET display_order = $1, updated_at = $2
		WHERE id = $3 AND user_id = $4
	`

	now := time.Now()

	for _, order := range orders {
		ct, err := tx.Exec(ctx, query, order.DisplayOrder, now, order.AccountID, userID)
		if err != nil {
			return err
		}

		if ct.RowsAffected() == 0 {
			return ErrAccountNotFound
		}
	}

	return tx.Commit(ctx)
}

// GetAccountsWithInstitutionFiltered retrieves accounts with institution names and hidden filtering.
func (r *accountRepository) GetAccountsWithInstitutionFiltered(ctx context.Context, userID string, includeHidden bool) ([]domain.Account, error) {
	hiddenClause := ""
	if !includeHidden {
		hiddenClause = " AND a.is_hidden = FALSE"
	}

	query := `
		SELECT a.id, a.linked_item_id, a.user_id, a.plaid_account_id,
		       a.name, a.official_name, a.type, a.subtype, a.mask,
		       a.current_balance, a.available_balance, a.currency_code,
		       a.nickname, a.custom_color, a.custom_icon, a.custom_emoji,
		       a.display_order, a.is_hidden,
		       a.created_at, a.updated_at, l.institution_name
		FROM accounts a
		LEFT JOIN linked_items l ON a.linked_item_id = l.id
		WHERE a.user_id = $1` + hiddenClause + `
		ORDER BY COALESCE(a.display_order, 999999) ASC, a.created_at ASC`

	rows, err := r.db.Query(ctx, query, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var accounts []domain.Account
	for rows.Next() {
		var account domain.Account
		err := rows.Scan(
			&account.ID, &account.LinkedItemID, &account.UserID, &account.PlaidAccountID,
			&account.Name, &account.OfficialName, &account.Type, &account.Subtype, &account.Mask,
			&account.CurrentBalance, &account.AvailableBalance, &account.CurrencyCode,
			&account.Nickname, &account.CustomColor, &account.CustomIcon, &account.CustomEmoji,
			&account.DisplayOrder, &account.IsHidden,
			&account.CreatedAt, &account.UpdatedAt, &account.InstitutionName,
		)
		if err != nil {
			return nil, err
		}
		accounts = append(accounts, account)
	}

	if err = rows.Err(); err != nil {
		return nil, err
	}

	if accounts != nil {
		return accounts, nil
	}
	return []domain.Account{}, nil
}

// GetByUserAccountAndStatus retrieves accounts by user_account_id and status.
// Issue #171: Plaid Re-Link Handling
func (r *accountRepository) GetByUserAccountAndStatus(ctx context.Context, userAccountID, status string) ([]domain.Account, error) {
	query := `
		SELECT id, linked_item_id, user_id, plaid_account_id,
		       name, official_name, type, subtype, mask,
		       current_balance, available_balance, currency_code,
		       user_account_id, status, replaced_at, replaced_by_id,
		       nickname, custom_color, custom_icon, custom_emoji, display_order, is_hidden,
		       created_at, updated_at
		FROM accounts
		WHERE user_account_id = $1 AND status = $2
		ORDER BY created_at ASC
	`

	rows, err := r.db.Query(ctx, query, userAccountID, status)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var accounts []domain.Account
	for rows.Next() {
		var account domain.Account
		err := rows.Scan(
			&account.ID, &account.LinkedItemID, &account.UserID, &account.PlaidAccountID,
			&account.Name, &account.OfficialName, &account.Type, &account.Subtype, &account.Mask,
			&account.CurrentBalance, &account.AvailableBalance, &account.CurrencyCode,
			&account.UserAccountID, &account.Status, &account.ReplacedAt, &account.ReplacedByID,
			&account.Nickname, &account.CustomColor, &account.CustomIcon, &account.CustomEmoji,
			&account.DisplayOrder, &account.IsHidden,
			&account.CreatedAt, &account.UpdatedAt,
		)
		if err != nil {
			return nil, err
		}
		accounts = append(accounts, account)
	}

	if err = rows.Err(); err != nil {
		return nil, err
	}

	if accounts != nil {
		return accounts, nil
	}
	return []domain.Account{}, nil
}

// GetByUserAccountID retrieves all accounts for a user_account (any status).
// Issue #171: Plaid Re-Link Handling
func (r *accountRepository) GetByUserAccountID(ctx context.Context, userAccountID string) ([]domain.Account, error) {
	query := `
		SELECT id, linked_item_id, user_id, plaid_account_id,
		       name, official_name, type, subtype, mask,
		       current_balance, available_balance, currency_code,
		       user_account_id, status, replaced_at, replaced_by_id,
		       nickname, custom_color, custom_icon, custom_emoji, display_order, is_hidden,
		       created_at, updated_at
		FROM accounts
		WHERE user_account_id = $1
		ORDER BY created_at ASC
	`

	rows, err := r.db.Query(ctx, query, userAccountID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var accounts []domain.Account
	for rows.Next() {
		var account domain.Account
		err := rows.Scan(
			&account.ID, &account.LinkedItemID, &account.UserID, &account.PlaidAccountID,
			&account.Name, &account.OfficialName, &account.Type, &account.Subtype, &account.Mask,
			&account.CurrentBalance, &account.AvailableBalance, &account.CurrencyCode,
			&account.UserAccountID, &account.Status, &account.ReplacedAt, &account.ReplacedByID,
			&account.Nickname, &account.CustomColor, &account.CustomIcon, &account.CustomEmoji,
			&account.DisplayOrder, &account.IsHidden,
			&account.CreatedAt, &account.UpdatedAt,
		)
		if err != nil {
			return nil, err
		}
		accounts = append(accounts, account)
	}

	if err = rows.Err(); err != nil {
		return nil, err
	}

	if accounts != nil {
		return accounts, nil
	}
	return []domain.Account{}, nil
}

// UpdateStatus updates an account's status and soft-delete fields with ownership validation.
// Issue #171: Plaid Re-Link Handling
func (r *accountRepository) UpdateStatus(ctx context.Context, accountID, userID string, status domain.AccountStatus, replacedByID *string) error {
	now := time.Now()

	// Set replaced_at only when transitioning to replaced status
	var replacedAt *time.Time
	if status == domain.AccountStatusReplaced {
		replacedAt = &now
	}

	query := `
		UPDATE accounts
		SET status = $1, replaced_at = $2, replaced_by_id = $3, updated_at = $4
		WHERE id = $5 AND user_id = $6
	`

	result, err := r.db.Exec(ctx, query, status, replacedAt, replacedByID, now, accountID, userID)
	if err != nil {
		return err
	}

	if result.RowsAffected() == 0 {
		return ErrAccountNotFound
	}

	return nil
}
