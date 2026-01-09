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
	// ErrUserAccountNotFound is returned when a user account is not found in the database
	ErrUserAccountNotFound = errors.New("user account not found")
	// ErrDuplicateUserAccount is returned when attempting to create a user account that violates uniqueness
	ErrDuplicateUserAccount = errors.New("user account already exists for this institution and mask")
)

// UserAccountRepository defines the interface for user_accounts data access.
// UserAccount provides a stable abstraction for bank accounts that survives Plaid Item re-links.
//
// Issue #171 - Plaid Re-Link Handling
type UserAccountRepository interface {
	// Create creates a new user account in the database.
	// Returns ErrDuplicateUserAccount if the (user_id, institution_id, mask) already exists.
	Create(ctx context.Context, userAccount *domain.UserAccount) error

	// GetByID retrieves a user account by its ID.
	// Returns ErrUserAccountNotFound if not found.
	GetByID(ctx context.Context, id string) (*domain.UserAccount, error)

	// GetByIDAndUser retrieves a user account by ID with ownership validation.
	// Returns ErrUserAccountNotFound if not found or user doesn't own it.
	GetByIDAndUser(ctx context.Context, id, userID string) (*domain.UserAccount, error)

	// GetByUserID retrieves all user accounts for a specific user.
	// Returns empty slice if user has no accounts.
	GetByUserID(ctx context.Context, userID string) ([]domain.UserAccount, error)

	// FindByInstitutionAndMask finds a user account by institution and mask.
	// This is the core matching function for re-link detection.
	// Returns nil, nil if no match found (not an error).
	// Pass nil for mask to search for accounts with NULL mask.
	FindByInstitutionAndMask(ctx context.Context, userID, institutionID string, mask *string) (*domain.UserAccount, error)

	// Update updates a user account's customization fields.
	// Returns ErrUserAccountNotFound if not found or user doesn't own it.
	Update(ctx context.Context, userAccount *domain.UserAccount) error

	// UpdatePrimaryTypeAndSubtype updates the type information (from most recent Plaid account).
	// Returns ErrUserAccountNotFound if not found.
	UpdatePrimaryTypeAndSubtype(ctx context.Context, id, primaryType, primarySubtype string) error
}

// userAccountRepository implements UserAccountRepository using PostgreSQL
type userAccountRepository struct {
	db DB
}

// NewUserAccountRepository creates a new UserAccountRepository instance
func NewUserAccountRepository(db DB) UserAccountRepository {
	return &userAccountRepository{db: db}
}

// Create creates a new user account in the database
func (r *userAccountRepository) Create(ctx context.Context, userAccount *domain.UserAccount) error {
	// Generate UUID if not already set
	if userAccount.ID == "" {
		userAccount.ID = uuid.New().String()
	}

	// Set timestamps
	now := time.Now()
	userAccount.CreatedAt = now
	userAccount.UpdatedAt = now

	query := `
		INSERT INTO user_accounts
		(id, user_id, institution_id, mask, account_name_hint,
		 nickname, custom_color, custom_icon, custom_emoji,
		 display_order, is_hidden, primary_type, primary_subtype,
		 created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15)
	`

	_, err := r.db.Exec(ctx, query,
		userAccount.ID,
		userAccount.UserID,
		userAccount.InstitutionID,
		userAccount.Mask,
		userAccount.AccountNameHint,
		userAccount.Nickname,
		userAccount.CustomColor,
		userAccount.CustomIcon,
		userAccount.CustomEmoji,
		userAccount.DisplayOrder,
		userAccount.IsHidden,
		userAccount.PrimaryType,
		userAccount.PrimarySubtype,
		userAccount.CreatedAt,
		userAccount.UpdatedAt,
	)

	if err != nil {
		// Check for unique constraint violation on (user_id, institution_id, mask)
		if strings.Contains(err.Error(), "duplicate key") ||
			strings.Contains(err.Error(), "unique constraint") ||
			strings.Contains(err.Error(), "unique_user_account_identity") {
			return ErrDuplicateUserAccount
		}
		return err
	}

	return nil
}

// GetByID retrieves a user account by its ID
func (r *userAccountRepository) GetByID(ctx context.Context, id string) (*domain.UserAccount, error) {
	query := `
		SELECT id, user_id, institution_id, mask, account_name_hint,
		       nickname, custom_color, custom_icon, custom_emoji,
		       display_order, is_hidden, primary_type, primary_subtype,
		       created_at, updated_at
		FROM user_accounts
		WHERE id = $1
	`

	var ua domain.UserAccount
	err := r.db.QueryRow(ctx, query, id).Scan(
		&ua.ID,
		&ua.UserID,
		&ua.InstitutionID,
		&ua.Mask,
		&ua.AccountNameHint,
		&ua.Nickname,
		&ua.CustomColor,
		&ua.CustomIcon,
		&ua.CustomEmoji,
		&ua.DisplayOrder,
		&ua.IsHidden,
		&ua.PrimaryType,
		&ua.PrimarySubtype,
		&ua.CreatedAt,
		&ua.UpdatedAt,
	)

	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrUserAccountNotFound
		}
		return nil, err
	}

	return &ua, nil
}

// GetByIDAndUser retrieves a user account by ID with ownership validation
func (r *userAccountRepository) GetByIDAndUser(ctx context.Context, id, userID string) (*domain.UserAccount, error) {
	query := `
		SELECT id, user_id, institution_id, mask, account_name_hint,
		       nickname, custom_color, custom_icon, custom_emoji,
		       display_order, is_hidden, primary_type, primary_subtype,
		       created_at, updated_at
		FROM user_accounts
		WHERE id = $1 AND user_id = $2
	`

	var ua domain.UserAccount
	err := r.db.QueryRow(ctx, query, id, userID).Scan(
		&ua.ID,
		&ua.UserID,
		&ua.InstitutionID,
		&ua.Mask,
		&ua.AccountNameHint,
		&ua.Nickname,
		&ua.CustomColor,
		&ua.CustomIcon,
		&ua.CustomEmoji,
		&ua.DisplayOrder,
		&ua.IsHidden,
		&ua.PrimaryType,
		&ua.PrimarySubtype,
		&ua.CreatedAt,
		&ua.UpdatedAt,
	)

	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrUserAccountNotFound
		}
		return nil, err
	}

	return &ua, nil
}

// GetByUserID retrieves all user accounts for a specific user
func (r *userAccountRepository) GetByUserID(ctx context.Context, userID string) ([]domain.UserAccount, error) {
	query := `
		SELECT id, user_id, institution_id, mask, account_name_hint,
		       nickname, custom_color, custom_icon, custom_emoji,
		       display_order, is_hidden, primary_type, primary_subtype,
		       created_at, updated_at
		FROM user_accounts
		WHERE user_id = $1
		ORDER BY COALESCE(display_order, 999999) ASC, created_at ASC
	`

	rows, err := r.db.Query(ctx, query, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var accounts []domain.UserAccount
	for rows.Next() {
		var ua domain.UserAccount
		err := rows.Scan(
			&ua.ID,
			&ua.UserID,
			&ua.InstitutionID,
			&ua.Mask,
			&ua.AccountNameHint,
			&ua.Nickname,
			&ua.CustomColor,
			&ua.CustomIcon,
			&ua.CustomEmoji,
			&ua.DisplayOrder,
			&ua.IsHidden,
			&ua.PrimaryType,
			&ua.PrimarySubtype,
			&ua.CreatedAt,
			&ua.UpdatedAt,
		)
		if err != nil {
			return nil, err
		}
		accounts = append(accounts, ua)
	}

	if err = rows.Err(); err != nil {
		return nil, err
	}

	// Return empty slice instead of nil
	if accounts == nil {
		accounts = []domain.UserAccount{}
	}

	return accounts, nil
}

// FindByInstitutionAndMask finds a user account by institution and mask.
// This is the core matching function for re-link detection.
func (r *userAccountRepository) FindByInstitutionAndMask(ctx context.Context, userID, institutionID string, mask *string) (*domain.UserAccount, error) {
	var query string
	var args []interface{}

	if mask == nil {
		// Search for NULL mask
		query = `
			SELECT id, user_id, institution_id, mask, account_name_hint,
			       nickname, custom_color, custom_icon, custom_emoji,
			       display_order, is_hidden, primary_type, primary_subtype,
			       created_at, updated_at
			FROM user_accounts
			WHERE user_id = $1 AND institution_id = $2 AND mask IS NULL
		`
		args = []interface{}{userID, institutionID}
	} else {
		// Search for specific mask
		query = `
			SELECT id, user_id, institution_id, mask, account_name_hint,
			       nickname, custom_color, custom_icon, custom_emoji,
			       display_order, is_hidden, primary_type, primary_subtype,
			       created_at, updated_at
			FROM user_accounts
			WHERE user_id = $1 AND institution_id = $2 AND mask = $3
		`
		args = []interface{}{userID, institutionID, *mask}
	}

	var ua domain.UserAccount
	err := r.db.QueryRow(ctx, query, args...).Scan(
		&ua.ID,
		&ua.UserID,
		&ua.InstitutionID,
		&ua.Mask,
		&ua.AccountNameHint,
		&ua.Nickname,
		&ua.CustomColor,
		&ua.CustomIcon,
		&ua.CustomEmoji,
		&ua.DisplayOrder,
		&ua.IsHidden,
		&ua.PrimaryType,
		&ua.PrimarySubtype,
		&ua.CreatedAt,
		&ua.UpdatedAt,
	)

	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			// No match found - this is not an error, just no existing user account
			return nil, nil
		}
		return nil, err
	}

	return &ua, nil
}

// Update updates a user account's customization fields
func (r *userAccountRepository) Update(ctx context.Context, userAccount *domain.UserAccount) error {
	query := `
		UPDATE user_accounts
		SET nickname = $1, custom_color = $2, custom_icon = $3,
		    custom_emoji = $4, display_order = $5, is_hidden = $6, updated_at = $7
		WHERE id = $8 AND user_id = $9
	`

	result, err := r.db.Exec(ctx, query,
		userAccount.Nickname,
		userAccount.CustomColor,
		userAccount.CustomIcon,
		userAccount.CustomEmoji,
		userAccount.DisplayOrder,
		userAccount.IsHidden,
		time.Now(),
		userAccount.ID,
		userAccount.UserID,
	)

	if err != nil {
		return err
	}

	rowsAffected := result.RowsAffected()
	if rowsAffected == 0 {
		return ErrUserAccountNotFound
	}

	return nil
}

// UpdatePrimaryTypeAndSubtype updates the type information from the most recent Plaid account
func (r *userAccountRepository) UpdatePrimaryTypeAndSubtype(ctx context.Context, id, primaryType, primarySubtype string) error {
	query := `
		UPDATE user_accounts
		SET primary_type = $1, primary_subtype = $2, updated_at = $3
		WHERE id = $4
	`

	result, err := r.db.Exec(ctx, query,
		primaryType,
		primarySubtype,
		time.Now(),
		id,
	)

	if err != nil {
		return err
	}

	rowsAffected := result.RowsAffected()
	if rowsAffected == 0 {
		return ErrUserAccountNotFound
	}

	return nil
}
