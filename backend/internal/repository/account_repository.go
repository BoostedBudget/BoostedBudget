package repository

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	appcrypto "github.com/maxnussbaum/BoostedBudget/internal/crypto"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/observability"
)

var (
	// ErrAccountNotFound is returned when an account is not found in the database
	ErrAccountNotFound = errors.New("account not found")
	// ErrDuplicateAccount is returned when attempting to create an account that already exists
	ErrDuplicateAccount = errors.New("account already exists for this user")
	// ErrAccountOwnedByAnotherUser is returned when a fingerprint is already held by a different user (Issue #1010)
	ErrAccountOwnedByAnotherUser = errors.New("one or more accounts are already linked by another user")
)

// FingerprintOwnership holds information about who owns a fingerprint and the state of their linked item.
// Used during cross-user uniqueness check in LinkAccounts (Issue #1010).
type FingerprintOwnership struct {
	UserID          string
	LinkedItemID    string
	ItemStatus      domain.LinkedItemStatus
	StatusChangedAt *time.Time
}

// AccountFilterOptions configures filtering for GetAccountsWithInstitutionFiltered (Issue #1109).
type AccountFilterOptions struct {
	IncludeHidden   bool   // if true, includes hidden accounts
	IncludeReplaced bool   // if true, returns accounts regardless of status (not just active)
	UserAccountID   string // if non-empty, filters to accounts sharing this user_account_id
}

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

	// GetAccountsWithInstitutionFiltered retrieves accounts with institution names and flexible filtering.
	// Results are sorted by display_order ASC (nulls last), then created_at ASC.
	GetAccountsWithInstitutionFiltered(ctx context.Context, userID string, opts AccountFilterOptions) ([]domain.Account, error)

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

	// CheckFingerprintOwnership checks if a fingerprint blind index is already held by a different user.
	// Returns ownership info (user, linked item, item status) if another user owns the fingerprint.
	// Returns nil, nil if the fingerprint is unclaimed or only claimed by the requesting user.
	// Issue #1010: Enforce Plaid item uniqueness across all users.
	CheckFingerprintOwnership(ctx context.Context, fingerprintBlindIdx, excludeUserID string) (*FingerprintOwnership, error)

	// ExpireAccountsByFingerprint marks all active accounts matching a fingerprint as 'expired'.
	// This releases the partial UNIQUE index constraint so a new user can link the same account.
	// Called when the 90-day TTL expires for a disconnected/revoked item.
	// Issue #1010: Enforce Plaid item uniqueness across all users.
	ExpireAccountsByFingerprint(ctx context.Context, fingerprintBlindIdx string) error
}

// accountRepository implements AccountRepository using PostgreSQL
type accountRepository struct {
	db  DB
	enc *appcrypto.FieldEncryptor
}

// NewAccountRepository creates a new AccountRepository instance
func NewAccountRepository(db DB, enc *appcrypto.FieldEncryptor) AccountRepository {
	return &accountRepository{db: db, enc: enc}
}

// encryptAccountPIIFields encrypts PII fields on an account before writing to the database.
// Blind indexes must be computed from plaintext BEFORE calling this function.
func (r *accountRepository) encryptAccountPIIFields(a *domain.Account) error {
	encryptedPlaidAccountID, err := r.enc.EncryptString(a.PlaidAccountID)
	if err != nil {
		return err
	}
	a.PlaidAccountID = encryptedPlaidAccountID

	encryptedName, err := r.enc.EncryptString(a.Name)
	if err != nil {
		return err
	}
	a.Name = encryptedName

	encryptedOfficialName, err := r.enc.EncryptNullableString(a.OfficialName)
	if err != nil {
		return err
	}
	a.OfficialName = encryptedOfficialName

	encryptedMask, err := r.enc.EncryptNullableString(a.Mask)
	if err != nil {
		return err
	}
	a.Mask = encryptedMask

	return nil
}

// decryptAccountPIIFields decrypts PII fields on an account after reading from the database.
func (r *accountRepository) decryptAccountPIIFields(a *domain.Account) error {
	decryptedPlaidAccountID, err := r.enc.DecryptString(a.PlaidAccountID)
	if err != nil {
		return err
	}
	a.PlaidAccountID = decryptedPlaidAccountID

	decryptedName, err := r.enc.DecryptString(a.Name)
	if err != nil {
		return err
	}
	a.Name = decryptedName

	decryptedOfficialName, err := r.enc.DecryptNullableString(a.OfficialName)
	if err != nil {
		return err
	}
	a.OfficialName = decryptedOfficialName

	decryptedMask, err := r.enc.DecryptNullableString(a.Mask)
	if err != nil {
		return err
	}
	a.Mask = decryptedMask

	// InstitutionName is only populated by JOIN queries; decrypt when present
	if a.InstitutionName != "" {
		decryptedInstitutionName, err := r.enc.DecryptString(a.InstitutionName)
		if err != nil {
			return err
		}
		a.InstitutionName = decryptedInstitutionName
	}

	return nil
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

	// Compute blind index from plaintext before encrypting
	plaidAccountIDBlindIdx := r.enc.BlindIndex(account.PlaidAccountID)

	if err := r.encryptAccountPIIFields(account); err != nil {
		return err
	}

	query := `
		INSERT INTO accounts
		(id, linked_item_id, user_id, plaid_account_id, plaid_account_id_blind_idx, name, official_name,
		 type, subtype, mask, current_balance, available_balance, currency_code,
		 persistent_account_id_blind_idx, fallback_fingerprint_blind_idx,
		 created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17)
	`

	_, err := r.db.Exec(ctx, query,
		account.ID,
		account.LinkedItemID,
		account.UserID,
		account.PlaidAccountID,
		plaidAccountIDBlindIdx,
		account.Name,
		account.OfficialName,
		account.Type,
		account.Subtype,
		account.Mask,
		account.CurrentBalance,
		account.AvailableBalance,
		account.CurrencyCode,
		account.PersistentAccountIDBlindIdx,
		account.FallbackFingerprintBlindIdx,
		account.CreatedAt,
		account.UpdatedAt,
	)

	if err != nil {
		// Check for unique constraint violations
		errMsg := err.Error()
		if strings.Contains(errMsg, "duplicate key") || strings.Contains(errMsg, "unique constraint") {
			if strings.Contains(errMsg, "unique_user_account") || strings.Contains(errMsg, "unique_accounts_plaid_id_blind") {
				return ErrDuplicateAccount
			}
			if strings.Contains(errMsg, "unique_persistent_account_id_active") || strings.Contains(errMsg, "unique_fallback_fingerprint_active") {
				return ErrAccountOwnedByAnotherUser
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
		(id, linked_item_id, user_id, plaid_account_id, plaid_account_id_blind_idx, name, official_name,
		 type, subtype, mask, current_balance, available_balance, currency_code,
		 persistent_account_id_blind_idx, fallback_fingerprint_blind_idx,
		 created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17)
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

		// Compute blind index from plaintext before encrypting
		plaidAccountIDBlindIdx := r.enc.BlindIndex(account.PlaidAccountID)

		if err := r.encryptAccountPIIFields(account); err != nil {
			return err
		}

		_, err := tx.Exec(ctx, query,
			account.ID,
			account.LinkedItemID,
			account.UserID,
			account.PlaidAccountID,
			plaidAccountIDBlindIdx,
			account.Name,
			account.OfficialName,
			account.Type,
			account.Subtype,
			account.Mask,
			account.CurrentBalance,
			account.AvailableBalance,
			account.CurrencyCode,
			account.PersistentAccountIDBlindIdx,
			account.FallbackFingerprintBlindIdx,
			account.CreatedAt,
			account.UpdatedAt,
		)

		if err != nil {
			// Check for unique constraint violations
			errMsg := err.Error()
			if strings.Contains(errMsg, "duplicate key") || strings.Contains(errMsg, "unique constraint") {
				if strings.Contains(errMsg, "unique_user_account") || strings.Contains(errMsg, "unique_accounts_plaid_id_blind") {
					return ErrDuplicateAccount
				}
				if strings.Contains(errMsg, "unique_persistent_account_id_active") || strings.Contains(errMsg, "unique_fallback_fingerprint_active") {
					return ErrAccountOwnedByAnotherUser
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
		WHERE linked_item_id = $1 AND status = 'active'
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
		if err := r.decryptAccountPIIFields(&account); err != nil {
			return nil, err
		}
		accounts = append(accounts, account)
	}

	if err = rows.Err(); err != nil {
		return nil, err
	}

	return accounts, nil
}

// GetByUserID retrieves all accounts for a specific user, including status and re-link fields.
// Returns active and replaced accounts (excludes expired). Status is always populated so
// callers like ListActiveByUser can filter correctly.
func (r *accountRepository) GetByUserID(ctx context.Context, userID string) ([]domain.Account, error) {
	query := `
		SELECT id, linked_item_id, user_id, plaid_account_id,
		       name, official_name, type, subtype, mask,
		       current_balance, available_balance, currency_code,
		       status, user_account_id, replaced_by_id, replaced_at,
		       created_at, updated_at
		FROM accounts
		WHERE user_id = $1 AND status = 'active'
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
			&account.Status,
			&account.UserAccountID,
			&account.ReplacedByID,
			&account.ReplacedAt,
			&account.CreatedAt,
			&account.UpdatedAt,
		)
		if err != nil {
			return nil, err
		}
		if err := r.decryptAccountPIIFields(&account); err != nil {
			return nil, err
		}
		accounts = append(accounts, account)
	}

	if err = rows.Err(); err != nil {
		return nil, err
	}

	return accounts, nil
}

// GetAccountsWithInstitution retrieves all accounts for a user with institution names from linked_items.
// Includes status and re-link fields so callers can distinguish active vs. replaced accounts.
func (r *accountRepository) GetAccountsWithInstitution(ctx context.Context, userID string) ([]domain.Account, error) {
	query := `
		SELECT a.id, a.linked_item_id, a.user_id, a.plaid_account_id,
		       a.name, a.official_name, a.type, a.subtype, a.mask,
		       a.current_balance, a.available_balance, a.currency_code,
		       a.status, a.user_account_id, a.replaced_by_id, a.replaced_at,
		       a.created_at, a.updated_at, l.institution_name
		FROM accounts a
		LEFT JOIN linked_items l ON a.linked_item_id = l.id
		WHERE a.user_id = $1 AND a.status = 'active'
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
			&account.Status,
			&account.UserAccountID,
			&account.ReplacedByID,
			&account.ReplacedAt,
			&account.CreatedAt,
			&account.UpdatedAt,
			&account.InstitutionName,
		)
		if err != nil {
			return nil, err
		}
		if err := r.decryptAccountPIIFields(&account); err != nil {
			return nil, err
		}
		accounts = append(accounts, account)
	}

	if err = rows.Err(); err != nil {
		return nil, err
	}

	return accounts, nil
}

// CountByUserID counts the total number of accounts for a user.
// Excludes accounts with status='expired' (Issue #1010: TTL-released fingerprints).
func (r *accountRepository) CountByUserID(ctx context.Context, userID string) (int, error) {
	query := `
		SELECT COUNT(*)
		FROM accounts
		WHERE user_id = $1
		AND status != 'expired'
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
	if ct.RowsAffected() == 0 {
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
		       user_account_id,
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
		&account.UserAccountID,
		&account.CreatedAt, &account.UpdatedAt,
	)

	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrAccountNotFound
		}
		return nil, err
	}

	if err := r.decryptAccountPIIFields(&account); err != nil {
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

// GetAccountsWithInstitutionFiltered retrieves accounts with institution names and flexible filtering.
// Issue #679, #680: Also includes linked_item status to show 'disconnected' when subscription expires.
// Issue #1109: Added IncludeReplaced and UserAccountID filter options.
func (r *accountRepository) GetAccountsWithInstitutionFiltered(ctx context.Context, userID string, opts AccountFilterOptions) ([]domain.Account, error) {
	// Canary counter: every active-only call filters out replaced accounts (Issue #1109).
	if !opts.IncludeReplaced {
		observability.AccountsReplacedFiltered.WithLabelValues("accounts_list").Inc()
	}
	hiddenClause := ""
	if !opts.IncludeHidden {
		hiddenClause = " AND a.is_hidden = FALSE"
	}

	statusClause := " AND a.status = 'active'"
	if opts.IncludeReplaced {
		statusClause = ""
	}

	userAccountClause := ""
	args := []any{userID}
	if opts.UserAccountID != "" {
		args = append(args, opts.UserAccountID)
		userAccountClause = " AND a.user_account_id = $2"
	}

	query := `
		SELECT a.id, a.linked_item_id, a.user_id, a.plaid_account_id,
		       a.name, a.official_name, a.type, a.subtype, a.mask,
		       a.current_balance, a.available_balance, a.currency_code,
		       a.nickname, a.custom_color, a.custom_icon, a.custom_emoji,
		       a.display_order, a.is_hidden,
		       a.user_account_id, a.status, a.replaced_at, a.replaced_by_id,
		       a.created_at, a.updated_at, l.institution_name, l.status
		FROM accounts a
		LEFT JOIN linked_items l ON a.linked_item_id = l.id
		WHERE a.user_id = $1` + statusClause + hiddenClause + userAccountClause + `
		ORDER BY COALESCE(a.display_order, 999999) ASC, a.created_at ASC`

	rows, err := r.db.Query(ctx, query, args...)
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
			&account.UserAccountID, &account.Status, &account.ReplacedAt, &account.ReplacedByID,
			&account.CreatedAt, &account.UpdatedAt, &account.InstitutionName, &account.LinkedItemStatus,
		)
		if err != nil {
			return nil, err
		}
		if err := r.decryptAccountPIIFields(&account); err != nil {
			return nil, err
		}
		accounts = append(accounts, account)
	}

	if err = rows.Err(); err != nil {
		return nil, err
	}

	return accounts, nil
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
		if err := r.decryptAccountPIIFields(&account); err != nil {
			return nil, err
		}
		accounts = append(accounts, account)
	}

	if err = rows.Err(); err != nil {
		return nil, err
	}

	return accounts, nil
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
		if err := r.decryptAccountPIIFields(&account); err != nil {
			return nil, err
		}
		accounts = append(accounts, account)
	}

	if err = rows.Err(); err != nil {
		return nil, err
	}

	return accounts, nil
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

// CheckFingerprintOwnership checks if a fingerprint blind index is already held by a different user.
// Returns ownership info if another active account (owned by a different user) matches the fingerprint.
// Returns nil, nil if the fingerprint is unclaimed or only claimed by the requesting user.
// Issue #1010: Enforce Plaid item uniqueness across all users.
func (r *accountRepository) CheckFingerprintOwnership(ctx context.Context, fingerprintBlindIdx, excludeUserID string) (*FingerprintOwnership, error) {
	query := `
		SELECT a.user_id, a.linked_item_id, li.status, li.status_changed_at
		FROM accounts a
		JOIN linked_items li ON a.linked_item_id = li.id
		WHERE (a.persistent_account_id_blind_idx = $1 OR a.fallback_fingerprint_blind_idx = $1)
		  AND a.status = 'active'
		  AND a.user_id != $2
		LIMIT 1
	`

	var ownership FingerprintOwnership
	err := r.db.QueryRow(ctx, query, fingerprintBlindIdx, excludeUserID).Scan(
		&ownership.UserID,
		&ownership.LinkedItemID,
		&ownership.ItemStatus,
		&ownership.StatusChangedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &ownership, nil
}

// ExpireAccountsByFingerprint marks all active accounts matching a fingerprint as 'expired'.
// This releases the partial UNIQUE index constraint so a new user can link the same account.
// Issue #1010: Enforce Plaid item uniqueness across all users.
func (r *accountRepository) ExpireAccountsByFingerprint(ctx context.Context, fingerprintBlindIdx string) error {
	query := `
		UPDATE accounts
		SET status = 'expired', updated_at = NOW()
		WHERE (persistent_account_id_blind_idx = $1 OR fallback_fingerprint_blind_idx = $1)
		  AND status = 'active'
	`
	_, err := r.db.Exec(ctx, query, fingerprintBlindIdx)
	return err
}
