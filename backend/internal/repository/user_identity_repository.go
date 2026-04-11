package repository

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	appcrypto "github.com/maxnussbaum/BoostedBudget/internal/crypto"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
)

var (
	// ErrIdentityNotFound is returned when an identity is not found in the database
	ErrIdentityNotFound = errors.New("identity not found")
	// ErrDuplicateIdentity is returned when attempting to create an identity with a duplicate clerk_user_id
	ErrDuplicateIdentity = errors.New("identity already exists for this clerk_user_id")
)

// UserIdentityRepository defines the interface for user identity data access
// Manages the mapping between Clerk identities and internal user UUIDs
// Implements Issue #148 Phase 2 - Multi-Provider Authentication Support
type UserIdentityRepository interface {
	// GetByClerkID retrieves an identity by Clerk user ID
	// Returns ErrIdentityNotFound if the identity doesn't exist
	GetByClerkID(ctx context.Context, clerkUserID string) (*domain.UserIdentity, error)

	// GetByUserID retrieves all identities for a user, ordered by primary first, then creation date
	// Returns empty slice if no identities found (not an error)
	GetByUserID(ctx context.Context, userID string) ([]*domain.UserIdentity, error)

	// Create creates a new identity in the database
	// Generates UUID if not set, sets timestamps
	// Returns ErrDuplicateIdentity if clerk_user_id already exists
	Create(ctx context.Context, identity *domain.UserIdentity) error

	// CreateTx creates a new identity in the database within a transaction
	// Generates UUID if not set, sets timestamps
	// Returns ErrDuplicateIdentity if clerk_user_id already exists
	CreateTx(ctx context.Context, tx pgx.Tx, identity *domain.UserIdentity) error

	// UpdateLastUsed updates the last_used_at and updated_at timestamps for an identity
	// Called during login to track when each identity was last used
	// Returns ErrIdentityNotFound if the identity doesn't exist
	UpdateLastUsed(ctx context.Context, clerkUserID string) error

	// UpdatePrimaryIdentityEmailTx updates the email and resets email_verified=false on the
	// primary identity (is_primary=true) for the given user, within an existing transaction.
	// If the user has no primary identity, the update is a no-op (no error).
	UpdatePrimaryIdentityEmailTx(ctx context.Context, tx pgx.Tx, userID, plainEmail string) error
}

// userIdentityRepository implements UserIdentityRepository using PostgreSQL
type userIdentityRepository struct {
	db  DB
	enc *appcrypto.FieldEncryptor
}

// NewUserIdentityRepository creates a new UserIdentityRepository instance
func NewUserIdentityRepository(db DB, enc *appcrypto.FieldEncryptor) UserIdentityRepository {
	return &userIdentityRepository{db: db, enc: enc}
}

// GetByClerkID retrieves an identity by Clerk user ID
func (r *userIdentityRepository) GetByClerkID(ctx context.Context, clerkUserID string) (*domain.UserIdentity, error) {
	blindIdx := r.enc.BlindIndex(clerkUserID)
	query := `
		SELECT id, user_id, clerk_user_id, provider, provider_user_id,
		       email, email_verified, is_primary,
		       created_at, updated_at, last_used_at
		FROM user_identities
		WHERE clerk_user_id_blind_idx = $1
	`

	var identity domain.UserIdentity
	err := r.db.QueryRow(ctx, query, blindIdx).Scan(
		&identity.ID,
		&identity.UserID,
		&identity.ClerkUserID,
		&identity.Provider,
		&identity.ProviderUserID,
		&identity.Email,
		&identity.EmailVerified,
		&identity.IsPrimary,
		&identity.CreatedAt,
		&identity.UpdatedAt,
		&identity.LastUsedAt,
	)

	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrIdentityNotFound
	}
	if err != nil {
		return nil, err
	}

	decryptedClerkUserID, err := r.enc.DecryptString(identity.ClerkUserID)
	if err != nil {
		return nil, fmt.Errorf("failed to decrypt clerk_user_id: %w", err)
	}
	identity.ClerkUserID = decryptedClerkUserID

	decryptedEmail, err := r.enc.DecryptString(identity.Email)
	if err != nil {
		return nil, fmt.Errorf("failed to decrypt email: %w", err)
	}
	identity.Email = decryptedEmail

	decryptedProviderUserID, err := r.enc.DecryptString(identity.ProviderUserID)
	if err != nil {
		return nil, fmt.Errorf("failed to decrypt provider_user_id: %w", err)
	}
	identity.ProviderUserID = decryptedProviderUserID

	return &identity, nil
}

// GetByUserID retrieves all identities for a user
// Returns identities ordered by is_primary DESC (primary first), then created_at ASC (oldest first)
func (r *userIdentityRepository) GetByUserID(ctx context.Context, userID string) ([]*domain.UserIdentity, error) {
	query := `
		SELECT id, user_id, clerk_user_id, provider, provider_user_id,
		       email, email_verified, is_primary,
		       created_at, updated_at, last_used_at
		FROM user_identities
		WHERE user_id = $1
		ORDER BY is_primary DESC, created_at ASC
	`

	rows, err := r.db.Query(ctx, query, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	// Initialize as empty slice (not nil) to ensure consistent return type
	identities := make([]*domain.UserIdentity, 0)
	for rows.Next() {
		var identity domain.UserIdentity
		err := rows.Scan(
			&identity.ID,
			&identity.UserID,
			&identity.ClerkUserID,
			&identity.Provider,
			&identity.ProviderUserID,
			&identity.Email,
			&identity.EmailVerified,
			&identity.IsPrimary,
			&identity.CreatedAt,
			&identity.UpdatedAt,
			&identity.LastUsedAt,
		)
		if err != nil {
			return nil, err
		}

		decryptedClerkUserID, err := r.enc.DecryptString(identity.ClerkUserID)
		if err != nil {
			return nil, fmt.Errorf("failed to decrypt clerk_user_id: %w", err)
		}
		identity.ClerkUserID = decryptedClerkUserID

		decryptedEmail, err := r.enc.DecryptString(identity.Email)
		if err != nil {
			return nil, fmt.Errorf("failed to decrypt email: %w", err)
		}
		identity.Email = decryptedEmail

		decryptedProviderUserID, err := r.enc.DecryptString(identity.ProviderUserID)
		if err != nil {
			return nil, fmt.Errorf("failed to decrypt provider_user_id: %w", err)
		}
		identity.ProviderUserID = decryptedProviderUserID

		identities = append(identities, &identity)
	}

	if err = rows.Err(); err != nil {
		return nil, err
	}

	return identities, nil
}

// Create creates a new identity in the database
func (r *userIdentityRepository) Create(ctx context.Context, identity *domain.UserIdentity) error {
	// Generate UUID if not already set
	if identity.ID == "" {
		identity.ID = uuid.New().String()
	}

	// Set timestamps
	now := time.Now()
	identity.CreatedAt = now
	identity.UpdatedAt = now

	encryptedClerkUserID, err := r.enc.EncryptString(identity.ClerkUserID)
	if err != nil {
		return fmt.Errorf("failed to encrypt clerk_user_id: %w", err)
	}
	clerkUserIDBlindIdx := r.enc.BlindIndex(identity.ClerkUserID)

	encryptedEmail, err := r.enc.EncryptString(identity.Email)
	if err != nil {
		return fmt.Errorf("failed to encrypt email: %w", err)
	}

	encryptedProviderUserID, err := r.enc.EncryptString(identity.ProviderUserID)
	if err != nil {
		return fmt.Errorf("failed to encrypt provider_user_id: %w", err)
	}

	query := `
		INSERT INTO user_identities (
			id, user_id, clerk_user_id, provider, provider_user_id,
			email, email_verified, is_primary,
			created_at, updated_at, last_used_at, clerk_user_id_blind_idx
		) VALUES (
			$1, $2, $3, $4, $5,
			$6, $7, $8,
			$9, $10, $11, $12
		)
	`

	_, err = r.db.Exec(ctx, query,
		identity.ID,
		identity.UserID,
		encryptedClerkUserID,
		identity.Provider,
		encryptedProviderUserID,
		encryptedEmail,
		identity.EmailVerified,
		identity.IsPrimary,
		identity.CreatedAt,
		identity.UpdatedAt,
		identity.LastUsedAt,
		clerkUserIDBlindIdx,
	)

	if err != nil {
		// Check for unique constraint violations
		if strings.Contains(err.Error(), "duplicate key") || strings.Contains(err.Error(), "unique constraint") {
			if strings.Contains(err.Error(), "clerk_user_id") {
				return ErrDuplicateIdentity
			}
		}
		return err
	}

	return nil
}

// CreateTx creates a new identity in the database within a transaction
func (r *userIdentityRepository) CreateTx(ctx context.Context, tx pgx.Tx, identity *domain.UserIdentity) error {
	// Generate UUID if not already set
	if identity.ID == "" {
		identity.ID = uuid.New().String()
	}

	// Set timestamps
	now := time.Now()
	identity.CreatedAt = now
	identity.UpdatedAt = now

	encryptedClerkUserID, err := r.enc.EncryptString(identity.ClerkUserID)
	if err != nil {
		return fmt.Errorf("failed to encrypt clerk_user_id: %w", err)
	}
	clerkUserIDBlindIdx := r.enc.BlindIndex(identity.ClerkUserID)

	encryptedEmail, err := r.enc.EncryptString(identity.Email)
	if err != nil {
		return fmt.Errorf("failed to encrypt email: %w", err)
	}

	encryptedProviderUserID, err := r.enc.EncryptString(identity.ProviderUserID)
	if err != nil {
		return fmt.Errorf("failed to encrypt provider_user_id: %w", err)
	}

	query := `
		INSERT INTO user_identities (
			id, user_id, clerk_user_id, provider, provider_user_id,
			email, email_verified, is_primary,
			created_at, updated_at, last_used_at, clerk_user_id_blind_idx
		) VALUES (
			$1, $2, $3, $4, $5,
			$6, $7, $8,
			$9, $10, $11, $12
		)
	`

	_, err = tx.Exec(ctx, query,
		identity.ID,
		identity.UserID,
		encryptedClerkUserID,
		identity.Provider,
		encryptedProviderUserID,
		encryptedEmail,
		identity.EmailVerified,
		identity.IsPrimary,
		identity.CreatedAt,
		identity.UpdatedAt,
		identity.LastUsedAt,
		clerkUserIDBlindIdx,
	)

	if err != nil {
		// Check for unique constraint violations
		if strings.Contains(err.Error(), "duplicate key") || strings.Contains(err.Error(), "unique constraint") {
			if strings.Contains(err.Error(), "clerk_user_id") {
				return ErrDuplicateIdentity
			}
		}
		return err
	}

	return nil
}

// UpdatePrimaryIdentityEmailTx updates the email and resets email_verified=false on the
// primary identity for the given user, within an existing transaction.
// 0 rows affected is a no-op (not an error).
func (r *userIdentityRepository) UpdatePrimaryIdentityEmailTx(ctx context.Context, tx pgx.Tx, userID, plainEmail string) error {
	encryptedEmail, err := r.enc.EncryptString(plainEmail)
	if err != nil {
		return fmt.Errorf("failed to encrypt identity email: %w", err)
	}
	now := time.Now()

	query := `
		UPDATE user_identities
		SET email = $1,
		    email_verified = false,
		    updated_at = $2
		WHERE user_id = $3
		  AND is_primary = true
	`
	_, err = tx.Exec(ctx, query, encryptedEmail, now, userID)
	return err
}

// UpdateLastUsed updates the last_used_at and updated_at timestamps for an identity
func (r *userIdentityRepository) UpdateLastUsed(ctx context.Context, clerkUserID string) error {
	now := time.Now()
	blindIdx := r.enc.BlindIndex(clerkUserID)

	query := `
		UPDATE user_identities
		SET last_used_at = $1,
		    updated_at = $2
		WHERE clerk_user_id_blind_idx = $3
	`

	ct, err := r.db.Exec(ctx, query, now, now, blindIdx)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return ErrIdentityNotFound
	}
	return nil
}
