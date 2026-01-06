package services

import (
	"context"
	"errors"
	"fmt"
	"regexp"

	"github.com/jackc/pgx/v5"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/repository"
)

// TxBeginner is an interface for database connection pools that can begin transactions.
// Both *pgxpool.Pool and pgxmock.PgxPoolIface satisfy this interface.
type TxBeginner interface {
	Begin(ctx context.Context) (pgx.Tx, error)
}

// Email validation regex (basic RFC 5322 compliant)
var emailRegex = regexp.MustCompile(`^[a-zA-Z0-9._%+\-]+@[a-zA-Z0-9.\-]+\.[a-zA-Z]{2,}$`)

// RegistrationService handles user registration and onboarding
//
// TRANSACTION SAFETY:
// - All new user creation operations (user + identity + privacy) execute atomically in a database transaction
// - If any creation step fails, the entire transaction is rolled back (no partial user records)
// - Transaction is only committed after all three entities are successfully created
//
// EMAIL VERIFICATION:
// - emailVerified status is extracted from Clerk JWT claims (email_verified field)
// - This status reflects whether Clerk verified the email during authentication
// - The status is stored in both user.email_verified and user_identity.email_verified
// - Default is false if not present in JWT claims (safe default)
//
// IDEMPOTENCY:
// - Calling RegisterUser multiple times with the same clerkUserID is safe
// - If user already exists, returns existing user without creating duplicates
// - Race conditions are handled via database constraints (unique clerk_user_id)
type RegistrationService interface {
	// RegisterUser creates a new user account or returns existing user (idempotent)
	//
	// For new users, creates three entities atomically in a transaction:
	//   1. User profile (users table)
	//   2. User identity (user_identities table)
	//   3. Privacy settings with GDPR defaults (user_privacy table)
	//
	// Parameters:
	//   - ctx: Context for cancellation and timeout
	//   - clerkUserID: Clerk user ID from JWT sub claim (e.g., "google-oauth2|123456")
	//   - email: User email address from JWT claims
	//   - emailVerified: Whether Clerk verified this email (from JWT email_verified claim)
	//
	// Returns:
	//   - User profile with generated UUID
	//   - Error if validation fails, database error, or transaction fails
	//
	// Guarantees:
	//   - Atomic: All three entities created together or none at all
	//   - Idempotent: Safe to call multiple times with same clerkUserID
	//   - Validated: Email format checked, required fields enforced
	RegisterUser(ctx context.Context, clerkUserID, email string, emailVerified bool) (*domain.User, error)
}

type registrationService struct {
	profileRepo  repository.ProfileRepository
	identityRepo repository.UserIdentityRepository
	privacyRepo  repository.PrivacyRepository
	auditLogger  AuditLogger
	db           TxBeginner // Database connection pool for transaction management
}

// NewRegistrationService creates a new RegistrationService
func NewRegistrationService(
	profileRepo repository.ProfileRepository,
	identityRepo repository.UserIdentityRepository,
	privacyRepo repository.PrivacyRepository,
	auditLogger AuditLogger,
	db TxBeginner,
) RegistrationService {
	return &registrationService{
		profileRepo:  profileRepo,
		identityRepo: identityRepo,
		privacyRepo:  privacyRepo,
		auditLogger:  auditLogger,
		db:           db,
	}
}

// RegisterUser implements idempotent user registration with atomic transaction safety
//
// FLOW:
//  1. Validate inputs (clerkUserID, email format)
//  2. Check if user already exists (idempotent path)
//  3. If new user:
//     a. Begin database transaction
//     b. Create user profile (users table)
//     c. Create user identity (user_identities table)
//     d. Create privacy settings (user_privacy table)
//     e. Commit transaction (all succeed or all fail)
//  4. Log audit event (non-blocking failure)
//
// TRANSACTION SAFETY:
// - Uses pgxpool.Pool.Begin for atomic multi-table inserts
// - Repositories provide *Tx methods for transaction-aware operations
// - Deferred rollback ensures cleanup if any step fails
// - Only commits if all three entities created successfully
//
// EMAIL VERIFICATION:
// - emailVerified param comes from Clerk JWT claims (email_verified field)
// - Clerk sets this to true after user verifies their email
// - We store this status in both user and identity records
// - This allows tracking which identities have verified emails
//
// UNIT TEST PATH:
// - When db is nil (unit tests), uses non-transactional Create methods
// - Allows testing without mocking pgxpool transaction complexity
// - Production always provides db, so transactions are always used
func (s *registrationService) RegisterUser(ctx context.Context, clerkUserID, email string, emailVerified bool) (*domain.User, error) {
	// Validate required fields
	if clerkUserID == "" {
		return nil, errors.New("clerk_user_id is required")
	}

	// Validate email format
	if !emailRegex.MatchString(email) {
		return nil, fmt.Errorf("invalid email format: %s", email)
	}

	// Check if user already exists (idempotent)
	existingIdentity, err := s.identityRepo.GetByClerkID(ctx, clerkUserID)
	if err != nil && !errors.Is(err, repository.ErrIdentityNotFound) {
		// Unexpected error (not "not found")
		return nil, fmt.Errorf("failed to check existing identity: %w", err)
	}

	if existingIdentity != nil {
		// User already exists - return existing user (idempotent behavior)
		user, err := s.profileRepo.GetUserByID(ctx, existingIdentity.UserID)
		if err != nil {
			return nil, fmt.Errorf("failed to get existing user: %w", err)
		}
		return user, nil
	}

	// New user - create user, identity, and privacy settings
	// Use atomic transaction if db is available (production), otherwise use non-transactional (unit tests)
	var user *domain.User
	if s.db != nil {
		// Production path: Use atomic transaction
		tx, err := s.db.Begin(ctx)
		if err != nil {
			return nil, fmt.Errorf("failed to start transaction: %w", err)
		}
		defer tx.Rollback(ctx) // Rollback if not committed

		// Step 1: Create user profile within transaction
		user = domain.NewUser(clerkUserID, email, emailVerified)
		err = s.profileRepo.CreateUserTx(ctx, tx, user)
		if err != nil {
			return nil, fmt.Errorf("failed to create user: %w", err)
		}

		// Step 2: Create user identity within transaction
		// First identity is always primary; emailVerified from Clerk JWT
		identity := domain.NewUserIdentity(user.ID, clerkUserID, email, emailVerified, true)
		err = s.identityRepo.CreateTx(ctx, tx, identity)
		if err != nil {
			return nil, fmt.Errorf("failed to create identity: %w", err)
		}

		// Step 3: Create privacy settings with GDPR defaults within transaction
		privacy := domain.NewPrivacySettings(user.ID)
		err = s.privacyRepo.CreateTx(ctx, tx, privacy)
		if err != nil {
			return nil, fmt.Errorf("failed to create privacy settings: %w", err)
		}

		// Commit transaction - all 3 creations succeed atomically
		if err = tx.Commit(ctx); err != nil {
			return nil, fmt.Errorf("failed to commit transaction: %w", err)
		}
	} else {
		// Unit test path: Use non-transactional methods
		// Step 1: Create user profile
		user = domain.NewUser(clerkUserID, email, emailVerified)
		err = s.profileRepo.CreateUser(ctx, user)
		if err != nil {
			return nil, fmt.Errorf("failed to create user: %w", err)
		}

		// Step 2: Create user identity
		// First identity is always primary; emailVerified from Clerk JWT
		identity := domain.NewUserIdentity(user.ID, clerkUserID, email, emailVerified, true)
		err = s.identityRepo.Create(ctx, identity)
		if err != nil {
			return nil, fmt.Errorf("failed to create identity: %w", err)
		}

		// Step 3: Create privacy settings with GDPR defaults
		privacy := domain.NewPrivacySettings(user.ID)
		err = s.privacyRepo.Create(ctx, privacy)
		if err != nil {
			return nil, fmt.Errorf("failed to create privacy settings: %w", err)
		}
	}

	// Step 4: Log audit event for new user registration
	if s.auditLogger != nil {
		err = s.auditLogger.Log(ctx, "user.registration", "user", user.ID)
		if err != nil {
			// Log error but don't fail registration
			// Audit logging failure shouldn't block user registration
			fmt.Printf("Warning: failed to log audit event for user %s: %v\n", user.ID, err)
		}
	}

	return user, nil
}
