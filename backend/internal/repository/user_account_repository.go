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
	// Returns ErrDuplicateUserAccount if (user_id, institution_id, mask, identity_type,
	// identity_subtype) already exists — #1569 round 11 task 4 added identity to this key;
	// unique_user_account_identity (the old plaintext-only (user_id, institution_id, mask)
	// constraint) was DROPPED by migration 000178, superseded by the blind-index constraint.
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

	// FindByInstitutionAndMask finds a user account by institution, mask, AND identity
	// (identity_type/identity_subtype — #1569 round 11 task 4). This is the core matching
	// function for re-link detection. Returns nil, nil if no match found (not an error).
	// Pass nil for mask to search for accounts with NULL mask.
	//
	// identityType/identitySubtype narrow the match so a brokerage and an IRA at one institution
	// (same institution, both maskless — the shape 000100's NULLS-NOT-DISTINCT index used to force
	// onto one row) are no longer treated as the same chain. Matched with IS NOT DISTINCT FROM so
	// a legacy orphan row with NULL identity (see migration 000177's header) can still be found by
	// an equally-NULL lookup, though in practice callers always pass a concrete identityType.
	FindByInstitutionAndMask(ctx context.Context, userID, institutionID string, mask *string, identityType, identitySubtype *string) (*domain.UserAccount, error)

	// Update updates a user account's customization fields.
	// Returns ErrUserAccountNotFound if not found or user doesn't own it.
	Update(ctx context.Context, userAccount *domain.UserAccount) error

	// UpdatePrimaryTypeAndSubtype updates the type information (from most recent Plaid account).
	// Scoped by (id, userID); returns ErrUserAccountNotFound if the pair matches no row.
	UpdatePrimaryTypeAndSubtype(ctx context.Context, id, userID, primaryType, primarySubtype string) error

	// UpdateHidden sets the is_hidden flag on a user account by ID.
	// Scoped by (id, userID); returns ErrUserAccountNotFound if the pair matches no row,
	// which includes the case of an id owned by a different user (issue #1566).
	UpdateHidden(ctx context.Context, id, userID string, hidden bool) error
}

// userAccountRepository implements UserAccountRepository using PostgreSQL
type userAccountRepository struct {
	db  DB
	enc *appcrypto.FieldEncryptor
}

// NewUserAccountRepository creates a new UserAccountRepository instance
func NewUserAccountRepository(db DB, enc *appcrypto.FieldEncryptor) UserAccountRepository {
	return &userAccountRepository{db: db, enc: enc}
}

// encryptUserAccountPIIFields encrypts PII fields on a user account before writing to the database.
// Blind indexes must be computed from plaintext BEFORE calling this function.
func (r *userAccountRepository) encryptUserAccountPIIFields(ua *domain.UserAccount) error {
	encryptedInstitutionID, err := r.enc.EncryptString(ua.InstitutionID)
	if err != nil {
		return err
	}
	ua.InstitutionID = encryptedInstitutionID

	encryptedMask, err := r.enc.EncryptNullableString(ua.Mask)
	if err != nil {
		return err
	}
	ua.Mask = encryptedMask

	encryptedAccountNameHint, err := r.enc.EncryptNullableString(ua.AccountNameHint)
	if err != nil {
		return err
	}
	ua.AccountNameHint = encryptedAccountNameHint

	return nil
}

// decryptUserAccountPIIFields decrypts PII fields on a user account after reading from the database.
func (r *userAccountRepository) decryptUserAccountPIIFields(ua *domain.UserAccount) error {
	decryptedInstitutionID, err := r.enc.DecryptString(ua.InstitutionID)
	if err != nil {
		return err
	}
	ua.InstitutionID = decryptedInstitutionID

	decryptedMask, err := r.enc.DecryptNullableString(ua.Mask)
	if err != nil {
		return err
	}
	ua.Mask = decryptedMask

	decryptedAccountNameHint, err := r.enc.DecryptNullableString(ua.AccountNameHint)
	if err != nil {
		return err
	}
	ua.AccountNameHint = decryptedAccountNameHint

	return nil
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

	// Compute blind indexes from plaintext before encrypting
	institutionIDBlindIdx := r.enc.BlindIndex(userAccount.InstitutionID)
	maskBlindIdx := r.enc.BlindIndexNullable(userAccount.Mask)

	if err := r.encryptUserAccountPIIFields(userAccount); err != nil {
		return err
	}

	query := `
		INSERT INTO user_accounts
		(id, user_id, institution_id, institution_id_blind_idx, mask, mask_blind_idx, account_name_hint,
		 nickname, custom_color, custom_icon, custom_emoji,
		 display_order, is_hidden, primary_type, primary_subtype, identity_type, identity_subtype,
		 created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19)
	`

	// identity_type/identity_subtype are written HERE, at mint, and never again (#1569 round 11
	// task 3). See domain.UserAccount for the full reasoning and the guards that enforce it.
	_, err := r.db.Exec(ctx, query,
		userAccount.ID,
		userAccount.UserID,
		userAccount.InstitutionID,
		institutionIDBlindIdx,
		userAccount.Mask,
		maskBlindIdx,
		userAccount.AccountNameHint,
		userAccount.Nickname,
		userAccount.CustomColor,
		userAccount.CustomIcon,
		userAccount.CustomEmoji,
		userAccount.DisplayOrder,
		userAccount.IsHidden,
		userAccount.PrimaryType,
		userAccount.PrimarySubtype,
		userAccount.IdentityType,
		userAccount.IdentitySubtype,
		userAccount.CreatedAt,
		userAccount.UpdatedAt,
	)

	if err != nil {
		// Check for unique constraint violation on (user_id, institution_id_blind_idx,
		// mask_blind_idx, identity_type, identity_subtype) — #1569 round 11 task 4. The literal
		// plaintext (user_id, institution_id, mask) constraint (unique_user_account_identity) no
		// longer exists (dropped by migration 000178); the string check below is retained only
		// because it is harmless if the constraint name ever appears in an error message again.
		if strings.Contains(err.Error(), "duplicate key") ||
			strings.Contains(err.Error(), "unique constraint") ||
			strings.Contains(err.Error(), "unique_user_account_identity") ||
			// #1569 round 11 task 4: unique_user_accounts_inst_mask_blind (000100) was replaced by
			// unique_user_accounts_inst_mask_identity_blind, which also narrows on
			// identity_type/identity_subtype — see migration 000178.
			strings.Contains(err.Error(), "unique_user_accounts_inst_mask_blind") ||
			strings.Contains(err.Error(), "unique_user_accounts_inst_mask_identity_blind") {
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
		       display_order, is_hidden, primary_type, primary_subtype, identity_type, identity_subtype,
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
		&ua.IdentityType,
		&ua.IdentitySubtype,
		&ua.CreatedAt,
		&ua.UpdatedAt,
	)

	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrUserAccountNotFound
		}
		return nil, err
	}

	if err := r.decryptUserAccountPIIFields(&ua); err != nil {
		return nil, err
	}

	return &ua, nil
}

// GetByIDAndUser retrieves a user account by ID with ownership validation
func (r *userAccountRepository) GetByIDAndUser(ctx context.Context, id, userID string) (*domain.UserAccount, error) {
	query := `
		SELECT id, user_id, institution_id, mask, account_name_hint,
		       nickname, custom_color, custom_icon, custom_emoji,
		       display_order, is_hidden, primary_type, primary_subtype, identity_type, identity_subtype,
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
		&ua.IdentityType,
		&ua.IdentitySubtype,
		&ua.CreatedAt,
		&ua.UpdatedAt,
	)

	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrUserAccountNotFound
		}
		return nil, err
	}

	if err := r.decryptUserAccountPIIFields(&ua); err != nil {
		return nil, err
	}

	return &ua, nil
}

// GetByUserID retrieves all user accounts for a specific user
func (r *userAccountRepository) GetByUserID(ctx context.Context, userID string) ([]domain.UserAccount, error) {
	query := `
		SELECT id, user_id, institution_id, mask, account_name_hint,
		       nickname, custom_color, custom_icon, custom_emoji,
		       display_order, is_hidden, primary_type, primary_subtype, identity_type, identity_subtype,
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

	accounts := make([]domain.UserAccount, 0)
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
			&ua.IdentityType,
			&ua.IdentitySubtype,
			&ua.CreatedAt,
			&ua.UpdatedAt,
		)
		if err != nil {
			return nil, err
		}
		if err := r.decryptUserAccountPIIFields(&ua); err != nil {
			return nil, err
		}
		accounts = append(accounts, ua)
	}

	if err = rows.Err(); err != nil {
		return nil, err
	}

	return accounts, nil
}

// FindByInstitutionAndMask finds a user account by institution, mask, AND identity.
// This is the core matching function for re-link detection.
// Uses blind indexes for lookups on encrypted fields.
//
// #1569 round 11 task 4: a row with a NON-NULL identity_type must match the incoming
// identityType/identitySubtype exactly (IS NOT DISTINCT FROM, so a genuinely subtype-less
// identity — type='other' with no subtype — still matches a NULL identitySubtype). A row with a
// NULL identity_type is an ORPHAN (migration 000177's header: a user_accounts row with no
// physical accounts ever pointing at it, so nothing could derive an identity for it) and matches
// ANY incoming identity — it has not yet claimed one, so the first real link claims it, exactly
// reproducing pre-task-4 behavior for this narrow legacy shape. ⚠⚠ ROUND 4 RE-DERIVATION (item 4):
// nothing currently WRITES identity_type onto an orphan row after this match — it cannot: both
// trg_user_accounts_identity_immutable and TestNoUpdateOfUserAccountsIdentityColumns block EVERY
// UPDATE of identity_type/identity_subtype unconditionally, including a NULL->value "claim" — so
// a SECOND, DIFFERENT-identity account arriving later at the same institution+mask matches this
// SAME still-orphan row too, putting two different real identities back on one chain and
// defeating the narrowing this whole migration exists to enforce, for exactly this one row.
// DECIDED, NOT ASSUMED: relaxing the immutability trigger/guard to allow a one-time NULL->value
// "claim" transition would close this gap correctly, but was rejected THIS round as
// disproportionate risk for a narrow, load-bearing, doubly-enforced invariant this branch treats
// as a first-class guarantee (see domain.UserAccount and TestNoUpdateOfUserAccountsIdentityColumns
// for how deliberately it is defended). The wildcard stays, accepted as a residual risk, for two
// reasons: (1) an orphan with ZERO physical accounts ever attached is not reachable through any
// normal write path today — MatchOrCreate and synthesiseUserAccountID both ALWAYS set identity at
// INSERT time, so a true orphan can only exist from historical data corruption or manual
// intervention, never from ordinary operation; (2) production currently has zero users, so the
// cost of this residual gap is zero today and any real occurrence would be a data-integrity
// anomaly worth investigating on its own, not a routine event. This is a decision to accept a
// documented, narrow risk, not an oversight — TestFindByInstitutionAndMask_
// OrphanRowMatchesAnyIdentity_ThenRefusesADifferentIdentity proves the BOUNDARY (a second,
// different identity DOES re-merge) rather than only the happy path a prior test proved.
func (r *userAccountRepository) FindByInstitutionAndMask(ctx context.Context, userID, institutionID string, mask *string, identityType, identitySubtype *string) (*domain.UserAccount, error) {
	// Compute blind indexes for lookup
	institutionIDBlindIdx := r.enc.BlindIndex(institutionID)
	maskBlindIdx := r.enc.BlindIndexNullable(mask)

	var query string
	var args []interface{}

	if maskBlindIdx == nil {
		// Search for NULL mask blind index
		query = `
			SELECT id, user_id, institution_id, mask, account_name_hint,
			       nickname, custom_color, custom_icon, custom_emoji,
			       display_order, is_hidden, primary_type, primary_subtype, identity_type, identity_subtype,
			       created_at, updated_at
			FROM user_accounts
			WHERE user_id = $1 AND institution_id_blind_idx = $2 AND mask_blind_idx IS NULL
			  AND (identity_type IS NULL
			       OR (identity_type = $3 AND identity_subtype IS NOT DISTINCT FROM $4))
			ORDER BY created_at ASC, id ASC
			LIMIT 1
		`
		args = []interface{}{userID, institutionIDBlindIdx, identityType, identitySubtype}
	} else {
		// Search for specific mask blind index
		query = `
			SELECT id, user_id, institution_id, mask, account_name_hint,
			       nickname, custom_color, custom_icon, custom_emoji,
			       display_order, is_hidden, primary_type, primary_subtype, identity_type, identity_subtype,
			       created_at, updated_at
			FROM user_accounts
			WHERE user_id = $1 AND institution_id_blind_idx = $2 AND mask_blind_idx = $3
			  AND (identity_type IS NULL
			       OR (identity_type = $4 AND identity_subtype IS NOT DISTINCT FROM $5))
			ORDER BY created_at ASC, id ASC
			LIMIT 1
		`
		args = []interface{}{userID, institutionIDBlindIdx, *maskBlindIdx, identityType, identitySubtype}
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
		&ua.IdentityType,
		&ua.IdentitySubtype,
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

	if err := r.decryptUserAccountPIIFields(&ua); err != nil {
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
	if result.RowsAffected() == 0 {
		return ErrUserAccountNotFound
	}
	return nil
}

// UpdatePrimaryTypeAndSubtype updates the type information from the most recent Plaid account.
// Scoped by (id, userID) in SQL — see UpdateHidden for why (#1566).
func (r *userAccountRepository) UpdatePrimaryTypeAndSubtype(ctx context.Context, id, userID, primaryType, primarySubtype string) error {
	query := `
		UPDATE user_accounts
		SET primary_type = $1, primary_subtype = $2, updated_at = $3
		WHERE id = $4 AND user_id = $5
	`

	result, err := r.db.Exec(ctx, query,
		primaryType,
		primarySubtype,
		time.Now(),
		id,
		userID,
	)
	if err != nil {
		return err
	}
	if result.RowsAffected() == 0 {
		return ErrUserAccountNotFound
	}
	return nil
}

// UpdateHidden sets the is_hidden flag on a user account by ID, scoped to its owner.
//
// #1566: ownership is enforced HERE, in the statement, not delegated to caller discipline. The
// previous `WHERE id = $3` meant any caller with an unvalidated id could flip a stranger's row
// while the cache-invalidation middleware bumped only the actor.
func (r *userAccountRepository) UpdateHidden(ctx context.Context, id, userID string, hidden bool) error {
	query := `
		UPDATE user_accounts
		SET is_hidden = $1, updated_at = $2
		WHERE id = $3 AND user_id = $4
	`
	result, err := r.db.Exec(ctx, query, hidden, time.Now(), id, userID)
	if err != nil {
		return err
	}
	if result.RowsAffected() == 0 {
		return ErrUserAccountNotFound
	}
	return nil
}
