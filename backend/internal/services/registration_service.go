package services

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/mail"

	"github.com/jackc/pgx/v5"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	phclient "github.com/maxnussbaum/BoostedBudget/internal/integration/posthog"
	"github.com/maxnussbaum/BoostedBudget/internal/repository"
)

var ErrRegistrationErasureFenced = errors.New("registration refused for erased Clerk identity")

type ClerkErasureFenceReader interface {
	IsClerkIdentityErasureFenced(context.Context, string) (bool, error)
}

// RegistrationAuthority prevents a valid but stale Clerk token from recreating an erased account.
type RegistrationAuthority struct {
	ErasureFence    ClerkErasureFenceReader
	ClerkUserGetter ClerkUserGetter
}

// TxBeginner is an interface for database connection pools that can begin transactions.
// Both *pgxpool.Pool and pgxmock.PgxPoolIface satisfy this interface.
type TxBeginner interface {
	Begin(ctx context.Context) (pgx.Tx, error)
}

// TrialGranter defines the interface for granting trials to users
// Issue #679: Used to auto-grant trial during registration
type TrialGranter interface {
	// GrantTrial grants a trial to the user
	GrantTrial(ctx context.Context, userID string) error
}

// TransactionalTrialGranter records eligibility and durable remote grant intent
// alongside registration. It must not call the provider inside this transaction.
type TransactionalTrialGranter interface {
	GrantTrialTx(context.Context, pgx.Tx, string) error
}

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

	// SetTrialGranter sets the trial granter for auto-granting trials on registration
	// Issue #679: Called during server initialization to wire up the trial service
	SetTrialGranter(tg TrialGranter)

	// SetAnalytics sets the analytics client for capturing registration events
	SetAnalytics(a phclient.Analytics)
}

type registrationService struct {
	profileRepo  repository.ProfileRepository
	identityRepo repository.UserIdentityRepository
	privacyRepo  repository.PrivacyRepository
	auditLogger  AuditLogger
	db           TxBeginner         // Database connection pool for transaction management
	trialGranter TrialGranter       // Issue #679: Optional trial granter for auto-grant on registration
	analytics    phclient.Analytics // Optional PostHog analytics client
	erasureFence ClerkErasureFenceReader
	clerkGetter  ClerkUserGetter
}

// NewRegistrationService creates a new RegistrationService
func NewRegistrationService(
	profileRepo repository.ProfileRepository,
	identityRepo repository.UserIdentityRepository,
	privacyRepo repository.PrivacyRepository,
	auditLogger AuditLogger,
	db TxBeginner,
	authorities ...RegistrationAuthority,
) RegistrationService {
	var authority RegistrationAuthority
	if len(authorities) > 0 {
		authority = authorities[0]
	}
	return &registrationService{
		profileRepo:  profileRepo,
		identityRepo: identityRepo,
		privacyRepo:  privacyRepo,
		auditLogger:  auditLogger,
		db:           db,
		erasureFence: authority.ErasureFence,
		clerkGetter:  authority.ClerkUserGetter,
	}
}

// SetTrialGranter sets the trial granter for auto-granting trials on registration
// Issue #679: Called during server initialization to wire up the trial service
func (s *registrationService) SetTrialGranter(tg TrialGranter) {
	s.trialGranter = tg
}

// SetAnalytics sets the analytics client for capturing registration events
func (s *registrationService) SetAnalytics(a phclient.Analytics) {
	s.analytics = a
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
	if s.erasureFence != nil {
		fenced, err := s.erasureFence.IsClerkIdentityErasureFenced(ctx, clerkUserID)
		if err != nil {
			return nil, fmt.Errorf("check Clerk erasure fence: %w", err)
		}
		if fenced {
			return nil, ErrRegistrationErasureFenced
		}
	}

	// Validate email format
	if _, err := mail.ParseAddress(email); err != nil {
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
	if s.clerkGetter != nil {
		_, err = s.clerkGetter.GetUser(ctx, clerkUserID)
		if errors.Is(err, ErrClerkUserNotFound) {
			return nil, ErrRegistrationErasureFenced
		}
		if err != nil {
			return nil, fmt.Errorf("verify Clerk user before registration: %w", err)
		}
	}

	// #1001: Fallback — blind-index lookup misses rows that were created before encryption
	// was deployed (they have NULL clerk_user_id_blind_idx). Check by plaintext email to
	// prevent creating a duplicate account for the same person.
	existingUser, _ := s.profileRepo.GetUserByEmail(ctx, email)
	if existingUser != nil {
		slog.WarnContext(ctx, "found unmigrated pre-encryption user via email fallback — skipping duplicate creation",
			"clerk_user_id", clerkUserID)
		return existingUser, nil
	}

	// New user - create user, identity, and privacy settings
	txTrialGranter, transactionalTrial := s.trialGranter.(TransactionalTrialGranter)
	if transactionalTrial && s.db == nil {
		return nil, errors.New("transactional trial registration requires a database")
	}
	user := domain.NewUser(clerkUserID, email, emailVerified)
	if transactionalTrial {
		// Only a confirmed RevenueCat projection can activate this account.
		user.SubscriptionStatus = string(domain.SubscriptionStatusInactive)
	}
	identity := domain.NewUserIdentity(user.ID, clerkUserID, email, emailVerified, true)
	privacy := domain.NewPrivacySettings(user.ID)

	// Use atomic transaction if db is available (production), otherwise use non-transactional (unit tests)
	if s.db != nil {
		// Production path: Use atomic transaction
		tx, err := s.db.Begin(ctx)
		if err != nil {
			return nil, fmt.Errorf("failed to start transaction: %w", err)
		}
		//nolint:errcheck // rollback is a no-op once Commit succeeds; on the error paths the
		// deferred rollback is best-effort cleanup and its error has nothing to act on.
		defer tx.Rollback(ctx)

		// Step 1: Create user profile within transaction
		if err = s.profileRepo.CreateUserTx(ctx, tx, user); err != nil {
			return nil, fmt.Errorf("failed to create user: %w", err)
		}

		// Step 2: Create user identity within transaction
		// First identity is always primary; emailVerified from Clerk JWT
		if err = s.identityRepo.CreateTx(ctx, tx, identity); err != nil {
			return nil, fmt.Errorf("failed to create identity: %w", err)
		}

		// Step 3: Create privacy settings with GDPR defaults within transaction
		if err = s.privacyRepo.CreateTx(ctx, tx, privacy); err != nil {
			return nil, fmt.Errorf("failed to create privacy settings: %w", err)
		}

		if transactionalTrial {
			if err = txTrialGranter.GrantTrialTx(ctx, tx, user.ID); err != nil {
				return nil, fmt.Errorf("failed to record trial intent: %w", err)
			}
			user.HadTrial = true
		}

		// Commit registration and trial intent together; queue repair can recover
		// remote issuance without ever resetting the fixed trial expiry.
		if err = tx.Commit(ctx); err != nil {
			return nil, fmt.Errorf("failed to commit transaction: %w", err)
		}
	} else {
		// Unit test path: Use non-transactional methods
		if err := s.profileRepo.CreateUser(ctx, user); err != nil {
			return nil, fmt.Errorf("failed to create user: %w", err)
		}
		if err := s.identityRepo.Create(ctx, identity); err != nil {
			return nil, fmt.Errorf("failed to create identity: %w", err)
		}
		if err := s.privacyRepo.Create(ctx, privacy); err != nil {
			return nil, fmt.Errorf("failed to create privacy settings: %w", err)
		}
	}

	// Issue #679: Auto-grant trial to new users
	// Step 4: Grant trial if trial granter is configured
	if s.trialGranter != nil && !transactionalTrial {
		if err := s.trialGranter.GrantTrial(ctx, user.ID); err != nil {
			// Non-fatal — a failed trial grant must not block a registration whose user
			// row has already been committed — but never silent: the empty error branch
			// that used to stand here made a user silently land on no trial at all with
			// nothing recorded. Same class of gap as Step 6's audit logger (Issue #1505).
			slog.ErrorContext(ctx, "failed to auto-grant trial to new user; user registered without a trial",
				"clerk_user_id", clerkUserID,
				"user_id", user.ID,
				"error", err,
			)
		}
	}

	// Step 5: Capture analytics event for new user registration
	if s.analytics != nil {
		s.analytics.Capture(user.ID, phclient.EventUserRegistered, nil)
	}

	// Step 6: Log audit event for new user registration
	if s.auditLogger != nil {
		// Non-fatal — a failed audit write must not block a registration whose user row has
		// already been committed — but never silent: the empty error branch that used to stand
		// here was indistinguishable from the stub logger that never failed. Issue #1505.
		logAuditFailure(ctx, s.auditLogger.Log(ctx, "user.registration", "user", user.ID),
			"user.registration", user.ID)
	}

	return user, nil
}
