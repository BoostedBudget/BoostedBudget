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
}

// userIdentityRepository implements UserIdentityRepository using PostgreSQL
type userIdentityRepository struct {
	db DB
}

// NewUserIdentityRepository creates a new UserIdentityRepository instance
func NewUserIdentityRepository(db DB) UserIdentityRepository {
	return &userIdentityRepository{db: db}
}

// GetByClerkID retrieves an identity by Clerk user ID
func (r *userIdentityRepository) GetByClerkID(ctx context.Context, clerkUserID string) (*domain.UserIdentity, error) {
	query := `
		SELECT id, user_id, clerk_user_id, provider, provider_user_id,
		       email, email_verified, is_primary,
		       created_at, updated_at, last_used_at
		FROM user_identities
		WHERE clerk_user_id = $1
	`

	var identity domain.UserIdentity
	err := r.db.QueryRow(ctx, query, clerkUserID).Scan(
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

	query := `
		INSERT INTO user_identities (
			id, user_id, clerk_user_id, provider, provider_user_id,
			email, email_verified, is_primary,
			created_at, updated_at, last_used_at
		) VALUES (
			$1, $2, $3, $4, $5,
			$6, $7, $8,
			$9, $10, $11
		)
	`

	_, err := r.db.Exec(ctx, query,
		identity.ID,
		identity.UserID,
		identity.ClerkUserID,
		identity.Provider,
		identity.ProviderUserID,
		identity.Email,
		identity.EmailVerified,
		identity.IsPrimary,
		identity.CreatedAt,
		identity.UpdatedAt,
		identity.LastUsedAt,
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

	query := `
		INSERT INTO user_identities (
			id, user_id, clerk_user_id, provider, provider_user_id,
			email, email_verified, is_primary,
			created_at, updated_at, last_used_at
		) VALUES (
			$1, $2, $3, $4, $5,
			$6, $7, $8,
			$9, $10, $11
		)
	`

	_, err := tx.Exec(ctx, query,
		identity.ID,
		identity.UserID,
		identity.ClerkUserID,
		identity.Provider,
		identity.ProviderUserID,
		identity.Email,
		identity.EmailVerified,
		identity.IsPrimary,
		identity.CreatedAt,
		identity.UpdatedAt,
		identity.LastUsedAt,
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

// UpdateLastUsed updates the last_used_at and updated_at timestamps for an identity
func (r *userIdentityRepository) UpdateLastUsed(ctx context.Context, clerkUserID string) error {
	now := time.Now()

	query := `
		UPDATE user_identities
		SET last_used_at = $1,
		    updated_at = $2
		WHERE clerk_user_id = $3
	`

	ct, err := r.db.Exec(ctx, query, now, now, clerkUserID)
	if err != nil {
		return err
	}

	// Check if any rows were affected
	if ct.RowsAffected() == 0 {
		return ErrIdentityNotFound
	}

	return nil
}
