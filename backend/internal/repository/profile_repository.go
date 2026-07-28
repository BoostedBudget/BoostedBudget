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
	ErrUserNotFound   = errors.New("user not found")
	ErrDuplicateEmail = errors.New("email already exists")
)

// ProfileRepository defines the interface for user profile data access
type ProfileRepository interface {
	// GetUserByClerkID retrieves a user by their Clerk user ID
	GetUserByClerkID(ctx context.Context, clerkUserID string) (*domain.User, error)

	// GetUserByID retrieves a user by their internal UUID
	GetUserByID(ctx context.Context, id string) (*domain.User, error)

	// GetUserByPaddleSubscriptionID retrieves a user by their Paddle subscription ID
	GetUserByPaddleSubscriptionID(ctx context.Context, subscriptionID string) (*domain.User, error)

	// GetUserByEmail retrieves a user by email, checking both the blind index (encrypted rows)
	// and plaintext LOWER(email) (pre-encryption rows). Use this as a fallback when the
	// clerk_user_id blind index lookup fails for unmigrated rows (#1001).
	GetUserByEmail(ctx context.Context, email string) (*domain.User, error)

	// CreateUser creates a new user in the database
	CreateUser(ctx context.Context, user *domain.User) error

	// CreateUserTx creates a new user in the database within a transaction
	CreateUserTx(ctx context.Context, tx pgx.Tx, user *domain.User) error

	// UpdateUser updates an existing user's information
	UpdateUser(ctx context.Context, user *domain.User) error

	// UpdateLastLogin updates the last_login_at timestamp for a user
	UpdateLastLogin(ctx context.Context, clerkUserID string) error

	// Paddle-related methods (Issue #45, #46: Paddle Payment Integration)

	// UpdatePaddleCustomerID links a Paddle customer ID to a user
	UpdatePaddleCustomerID(ctx context.Context, userID, paddleCustomerID string) error

	// UpdateSubscriptionFromPaddle updates subscription details from Paddle webhooks
	UpdateSubscriptionFromPaddle(ctx context.Context, userID, tier, status string, startedAt, endsAt *time.Time, subscriptionID *string) error

	// Trial-related methods (Issue #679)

	// UpdateTrialStatus updates trial-related fields for a user
	UpdateTrialStatus(ctx context.Context, userID string, status string, trialStartedAt, trialEndsAt *time.Time, hadTrial bool) error

	// UpdateSubscriptionStatus updates just the subscription status
	UpdateSubscriptionStatus(ctx context.Context, userID string, status string) error

	// GetEffectiveSubscriptionStatusByUserID retrieves the user's EFFECTIVE subscription
	// status — "active" when a non-expired manual tier override (admin comp) is in effect,
	// otherwise the raw subscription_status (Issue #1413: an admin comp must actually unlock
	// access, not just relabel the tier while every access gate keeps reading the stale,
	// untouched status). Used by read-only middleware (#930) to avoid a full user fetch on
	// every request.
	GetEffectiveSubscriptionStatusByUserID(ctx context.Context, userID string) (string, error)

	// Retention tracking methods (#930: 6-month read-only before auto-deletion)

	// SetRetentionDates sets the retention_started_at and retention_expires_at for a user.
	SetRetentionDates(ctx context.Context, userID string, startedAt, expiresAt time.Time) error

	// ClearRetentionDates clears all retention tracking fields (called on resubscription).
	ClearRetentionDates(ctx context.Context, userID string) error

	// SetRetentionNotice30dSent marks the 30-day pre-deletion notice as sent.
	SetRetentionNotice30dSent(ctx context.Context, userID string) error

	// SetRetentionNotice7dSent marks the 7-day pre-deletion notice as sent.
	SetRetentionNotice7dSent(ctx context.Context, userID string) error

	// GetUsersNeedingRetention30dNotice returns users whose retention_expires_at is within 30 days
	// and whose 30-day notice has not yet been sent.
	GetUsersNeedingRetention30dNotice(ctx context.Context) ([]*domain.User, error)

	// GetUsersNeedingRetention7dNotice returns users whose retention_expires_at is within 7 days
	// and whose 7-day notice has not yet been sent.
	GetUsersNeedingRetention7dNotice(ctx context.Context) ([]*domain.User, error)

	// GetUsersWithExpiredRetention returns users whose retention_expires_at has passed
	// and who are not already deleted or pending deletion.
	GetUsersWithExpiredRetention(ctx context.Context) ([]*domain.User, error)

	// GetUsersWithExpiredManualTierOverride returns users whose manual_tier_override is set and
	// whose manual_tier_ends_at has strictly passed (Issue #1413 Task 9 — backs the daily job
	// that expires lapsed admin comps).
	GetUsersWithExpiredManualTierOverride(ctx context.Context) ([]*domain.User, error)

	// GetUsersWithExpiredTrials returns users with trial status but past trial_ends_at
	GetUsersWithExpiredTrials(ctx context.Context) ([]*domain.User, error)

	// GetUsersWithExpiringTrials returns users whose trial ends within N days
	GetUsersWithExpiringTrials(ctx context.Context, withinDays int) ([]*domain.User, error)

	// GetUsersWithActivePaddleSubscriptions returns users with a non-null paddle_subscription_id
	// and subscription_status not in ('inactive', 'cancelled')
	GetUsersWithActivePaddleSubscriptions(ctx context.Context) ([]*domain.User, error)

	// Deletion-related methods

	// MarkForDeletion marks a user for deletion with a scheduled date
	MarkForDeletion(ctx context.Context, userID string, scheduledAt time.Time) error

	// CancelDeletion cancels a pending deletion request
	CancelDeletion(ctx context.Context, userID string) error

	// GetUsersScheduledForDeletion returns users whose deletion date has passed
	GetUsersScheduledForDeletion(ctx context.Context) ([]*domain.User, error)

	// DeleteUserCascade performs application-level cascade deletion of all user data
	DeleteUserCascade(ctx context.Context, userID string) error

	// Dunning: past_due tracking methods (Issue #898)

	// SetPastDueStartedAt sets the past_due_started_at timestamp for a user.
	// Only sets the timestamp if it is currently NULL (does not reset on repeated calls).
	SetPastDueStartedAt(ctx context.Context, userID string, t time.Time) error

	// ClearPastDueStartedAt clears the past_due_started_at timestamp for a user (called on resubscription).
	ClearPastDueStartedAt(ctx context.Context, userID string) error

	// GetUsersInPastDue returns users with subscription_status='past_due' and non-nil past_due_started_at.
	// Used by the dunning cron job to find candidates for grace-expiry processing (Issue #898).
	GetUsersInPastDue(ctx context.Context) ([]domain.User, error)

	// CancelIfPastDueWithRetention atomically flips subscription_status to 'cancelled' AND starts
	// the retention clock (retention_started_at, retention_expires_at) in the SAME UPDATE, closing
	// Issue #1413's H5 residual on the past-due grace-cancel path. The status flip and the
	// retention-clock write used to be two separate calls (a CAS-guarded status flip, then a
	// separate SetRetentionDates call), which left a window where the CAS could succeed but the
	// retention write could be lost (a crash, a dead-lettered job) — and because GetUsersInPastDue
	// only selects subscription_status = 'past_due', the daily dunning job can never re-select a
	// user this already flipped to 'cancelled', so that user would be cancelled forever with no
	// retention clock: never warned, never deleted. Combining both writes into one statement means
	// either both land or neither does. Predicate: still 'past_due' AND carries no currently-active
	// manual tier override — the claim provides exclusivity, this predicate still provides
	// correctness. Issue #898: CAS guard against recovery race condition. Issue #1413 High finding
	// H6 (Codex review round 3): predicate extended to also close the comp race — a prior
	// read-only check of HasActiveManualOverride() cannot, by itself, protect the final status
	// flip. Returns (true, nil) if the flip happened, (false, nil) if the user has already
	// recovered (payment arrived between Plaid disconnect and this point) or an admin
	// granted/extended a comp in that same window.
	CancelIfPastDueWithRetention(ctx context.Context, userID string, retentionStarted, retentionExpires time.Time) (bool, error)

	// ExpireTrialIfNoOverride atomically flips subscription_status to 'inactive' only if the
	// user is still 'trial' AND carries no currently-active manual tier override. Returns
	// (true, nil) if the flip happened, (false, nil) if an admin granted/extended a comp between
	// the caller's earlier HasActiveManualOverride() read and this call, or the user's status has
	// otherwise already moved on (Issue #1413 High finding H5, Codex review round 3 — the earlier
	// read-only check cannot, by itself, protect the write against a comp granted in that window).
	ExpireTrialIfNoOverride(ctx context.Context, userID string) (bool, error)

	// ClaimForDeletion atomically verifies, at the moment of the claim, that a user is still
	// eligible for ExecuteDeletion's irreversible external cleanup (Paddle cancel, Plaid
	// disconnect) and cascade delete: not already deleted, and either carrying no currently-
	// active manual tier override OR having an outstanding GDPR deletion request of their own
	// (deletion_requested_at IS NOT NULL — the ratified rule that a user's own deletion request
	// beats an admin comp). Returns (true, nil) if the claim succeeded (safe to proceed with
	// the destructive work), (false, nil) if an admin granted/extended a comp between the
	// caller's earlier GetUserByID read and this call, with no outstanding GDPR request to
	// override it — a legitimate skip, not an error; the daily retention job will simply not
	// re-select this user while the comp is live. Issue #1413 Critical finding C2 (Codex review
	// round 2): the destructive work must not run on a decision made off a stale snapshot.
	//
	// Ordering note for callers: this claim must run BEFORE any external cleanup, not folded
	// into DeleteUserCascade's own final write — Plaid/Paddle IDs must still be readable when
	// the external calls run, so the claim has to come first as its own step, or the external
	// work would already be irreversibly done by the time a later guard could catch a comp
	// granted in the race window.
	ClaimForDeletion(ctx context.Context, userID string) (bool, error)

	// IsRetentionNotice30dEligible re-verifies, at the moment a caller already holds an
	// EXCLUSIVE work_claims claim on repository.ActionRetentionNotice30d, that a user is still
	// eligible for the 30-day pre-deletion notice: still inside the notice window, the notice
	// still unsent, subscription still inactive/cancelled with no outstanding GDPR deletion
	// request, and carrying no manual_tier_override at all (live or lapsed) —
	// noManualOverrideAtAllPredicate, the SAME rule GetUsersNeedingRetention30dNotice's own
	// candidate query enforces, mirrored here field-for-field so the two can never drift apart.
	// Returns (true, nil) when it is safe to send the notice email right now, (false, nil) when
	// any one of those conditions has changed since the candidate query ran (a comp granted, the
	// window closed, the notice already sent by a since-completed claim, a deletion request filed)
	// — a legitimate skip, never an error. This method performs NO write.
	//
	// Issue #1413 Medium finding M9 (Codex review round 3): replaces the earlier
	// ClaimRetentionNotice30dSent, which conflated two different concerns into one atomic UPDATE —
	// EXCLUSIVITY (now work_claims's job, via Claim/Complete/Release) and CORRECTNESS (this
	// read-only re-check). The old method stamped retention_notice_30d_sent_at BEFORE the email
	// send was even attempted, so a failed send left the marker set and the next day's candidate
	// query silently excluded the user forever — the user was never warned before deletion, the
	// exact outcome the notice exists to prevent. The corrected sequence is: Claim (exclusivity) →
	// IsRetentionNotice30dEligible (this method, correctness) → send the email → ONLY on success,
	// SetRetentionNotice30dSent (the marker) → Complete. On send failure, Release — the marker
	// stays NULL, so the next daily run retries.
	IsRetentionNotice30dEligible(ctx context.Context, userID string) (bool, error)

	// IsRetentionNotice7dEligible is IsRetentionNotice30dEligible's 7-day sibling. See that
	// method's doc for the full rationale.
	IsRetentionNotice7dEligible(ctx context.Context, userID string) (bool, error)

	// UpdateUserEmailTx updates a user's email, blind index, and resets email_verified=false
	// within an existing transaction. Returns ErrDuplicateEmail if the email is already in use.
	UpdateUserEmailTx(ctx context.Context, tx pgx.Tx, userID, plainEmail string) error

	// GetUsersNeedingRetentionClockRepair returns users who are read-only-status
	// (inactive/cancelled) but have NO retention clock started (retention_expires_at IS NULL) —
	// Issue #1413 H5 residual, belt-and-braces repair path. CancelIfPastDueWithRetention closes
	// the specific race on the past-due grace-cancel path going forward, but this query also
	// catches: (a) any row already stuck in this state from BEFORE that fix existed, and (b) any
	// other, currently-unknown path that might leave a user cancelled with no clock. Mirrors the
	// other retention queries' predicates (subscription_status IN ('inactive', 'cancelled'),
	// not deleted, no outstanding GDPR deletion request, no manual_tier_override at all — live or
	// lapsed) so a comped or already-deletion-pending user is never touched.
	GetUsersNeedingRetentionClockRepair(ctx context.Context) ([]*domain.User, error)

	// RepairRetentionClock sets the retention clock for a single user, but ONLY if
	// retention_expires_at is still NULL and the user is still read-only-status at the moment of
	// the write — a conditional UPDATE (not a claim) is sufficient here because the write is
	// purely additive and idempotent: once retention_expires_at is set, the WHERE clause itself
	// prevents any concurrent repair attempt from clobbering it, unlike the destructive external
	// side effects work_claims exists to guard. Returns (true, nil) if the clock was set, (false,
	// nil) if the user no longer matches (already repaired by a concurrent run, comp granted,
	// deletion requested, or status changed) since the candidate query ran — a legitimate skip,
	// not an error.
	RepairRetentionClock(ctx context.Context, userID string, startedAt, expiresAt time.Time) (bool, error)
}

// profileRepository implements ProfileRepository using PostgreSQL
type profileRepository struct {
	db  DB
	enc *appcrypto.FieldEncryptor
}

// NewProfileRepository creates a new ProfileRepository instance.
//
// Returns the concrete *profileRepository type (not the ProfileRepository interface) — Issue
// #1413 round-4 finding P6. profileRepository grew occurrence-scoped sibling methods
// (ClearPastDueStartedAtIfMatches, CancelIfPastDueOccurrenceWithRetention,
// IsRetentionNotice30dEligibleForOccurrence/7d) that are needed only by past_due_service.go's and
// retention_cleanup_check.go's own narrow consumer interfaces, not by the broad canonical
// ProfileRepository interface every other service's test double also implements. Every existing
// call site (main.go, every _test.go in this package) already assigns via `:=` or passes the
// value straight into a function parameter, so this is a compile-compatible widening: the
// concrete type structurally satisfies ProfileRepository and every narrower consumer interface it
// is passed to. Returning the interface here would force EITHER the new methods onto the
// canonical interface (rippling boilerplate mock stubs across every unrelated service's test file
// that implements it in full) OR main.go to carry a second, separately-typed handle to the same
// repository — this return-type change avoids both at the cost of a one-line signature edit.
// Precedent: NewMerchantRepository, NewMerchantOverrideRepository, NewSystemHealthRepository, and
// NewDeadLetterJobRepository already return their unexported concrete pointer types in this same
// package.
func NewProfileRepository(db DB, enc *appcrypto.FieldEncryptor) *profileRepository {
	return &profileRepository{db: db, enc: enc}
}

const userSelectQuery = `
	SELECT id, clerk_user_id, email, email_verified,
	       subscription_tier, subscription_status, subscription_started_at, subscription_ends_at,
	       trial_started_at, trial_ends_at, had_trial,
	       paddle_customer_id, paddle_subscription_id, primary_region,
	       is_active, is_deleted,
	       deletion_requested_at, deletion_scheduled_at,
	       created_at, updated_at, last_login_at,
	       manual_tier_override, manual_tier_ends_at,
	       retention_started_at, retention_expires_at, retention_notice_30d_sent_at, retention_notice_7d_sent_at,
	       timezone, timezone_source,
	       past_due_started_at
	FROM users`

// queryUser is a helper that scans a user from a query row
func (r *profileRepository) queryUser(ctx context.Context, query string, args ...interface{}) (*domain.User, error) {
	var user domain.User
	err := r.db.QueryRow(ctx, query, args...).Scan(
		&user.ID, &user.ClerkUserID, &user.Email, &user.EmailVerified,
		&user.SubscriptionTier, &user.SubscriptionStatus, &user.SubscriptionStartedAt, &user.SubscriptionEndsAt,
		&user.TrialStartedAt, &user.TrialEndsAt, &user.HadTrial,
		&user.PaddleCustomerID, &user.PaddleSubscriptionID, &user.PrimaryRegion,
		&user.IsActive, &user.IsDeleted,
		&user.DeletionRequestedAt, &user.DeletionScheduledAt,
		&user.CreatedAt, &user.UpdatedAt, &user.LastLoginAt,
		&user.ManualTierOverride, &user.ManualTierEndsAt,
		&user.RetentionStartedAt, &user.RetentionExpiresAt, &user.RetentionNotice30dSentAt, &user.RetentionNotice7dSentAt,
		&user.Timezone, &user.TimezoneSource,
		&user.PastDueStartedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrUserNotFound
	}
	if err != nil {
		return nil, err
	}

	decryptedClerkUserID, err := r.enc.DecryptString(user.ClerkUserID)
	if err != nil {
		return nil, fmt.Errorf("failed to decrypt clerk_user_id: %w", err)
	}
	user.ClerkUserID = decryptedClerkUserID

	decryptedEmail, err := r.enc.DecryptString(user.Email)
	if err != nil {
		return nil, fmt.Errorf("failed to decrypt email: %w", err)
	}
	user.Email = decryptedEmail

	decryptedPaddleCustomerID, err := r.enc.DecryptNullableString(user.PaddleCustomerID)
	if err != nil {
		return nil, fmt.Errorf("failed to decrypt paddle_customer_id: %w", err)
	}
	user.PaddleCustomerID = decryptedPaddleCustomerID

	decryptedPaddleSubscriptionID, err := r.enc.DecryptNullableString(user.PaddleSubscriptionID)
	if err != nil {
		return nil, fmt.Errorf("failed to decrypt paddle_subscription_id: %w", err)
	}
	user.PaddleSubscriptionID = decryptedPaddleSubscriptionID

	return &user, nil
}

// queryUsers is a helper that scans multiple users from a query
func (r *profileRepository) queryUsers(ctx context.Context, query string, args ...interface{}) ([]*domain.User, error) {
	rows, err := r.db.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var users []*domain.User
	for rows.Next() {
		var user domain.User
		if err := rows.Scan(
			&user.ID, &user.ClerkUserID, &user.Email, &user.EmailVerified,
			&user.SubscriptionTier, &user.SubscriptionStatus, &user.SubscriptionStartedAt, &user.SubscriptionEndsAt,
			&user.TrialStartedAt, &user.TrialEndsAt, &user.HadTrial,
			&user.PaddleCustomerID, &user.PaddleSubscriptionID, &user.PrimaryRegion,
			&user.IsActive, &user.IsDeleted,
			&user.DeletionRequestedAt, &user.DeletionScheduledAt,
			&user.CreatedAt, &user.UpdatedAt, &user.LastLoginAt,
			&user.ManualTierOverride, &user.ManualTierEndsAt,
			&user.RetentionStartedAt, &user.RetentionExpiresAt, &user.RetentionNotice30dSentAt, &user.RetentionNotice7dSentAt,
			&user.Timezone, &user.TimezoneSource,
			&user.PastDueStartedAt,
		); err != nil {
			return nil, err
		}

		decryptedClerkUserID, err := r.enc.DecryptString(user.ClerkUserID)
		if err != nil {
			return nil, fmt.Errorf("failed to decrypt clerk_user_id: %w", err)
		}
		user.ClerkUserID = decryptedClerkUserID

		decryptedEmail, err := r.enc.DecryptString(user.Email)
		if err != nil {
			return nil, fmt.Errorf("failed to decrypt email: %w", err)
		}
		user.Email = decryptedEmail

		decryptedPaddleCustomerID, err := r.enc.DecryptNullableString(user.PaddleCustomerID)
		if err != nil {
			return nil, fmt.Errorf("failed to decrypt paddle_customer_id: %w", err)
		}
		user.PaddleCustomerID = decryptedPaddleCustomerID

		decryptedPaddleSubscriptionID, err := r.enc.DecryptNullableString(user.PaddleSubscriptionID)
		if err != nil {
			return nil, fmt.Errorf("failed to decrypt paddle_subscription_id: %w", err)
		}
		user.PaddleSubscriptionID = decryptedPaddleSubscriptionID

		users = append(users, &user)
	}
	return users, rows.Err()
}

// GetUserByClerkID retrieves a user by their Clerk user ID
func (r *profileRepository) GetUserByClerkID(ctx context.Context, clerkUserID string) (*domain.User, error) {
	blindIdx := r.enc.BlindIndex(clerkUserID)
	return r.queryUser(ctx, userSelectQuery+" WHERE clerk_user_id_blind_idx = $1 AND is_deleted = FALSE", blindIdx)
}

// GetUserByID retrieves a user by their internal UUID
func (r *profileRepository) GetUserByID(ctx context.Context, id string) (*domain.User, error) {
	return r.queryUser(ctx, userSelectQuery+" WHERE id = $1 AND is_deleted = FALSE", id)
}

// GetUserByPaddleSubscriptionID retrieves a user by their Paddle subscription ID
func (r *profileRepository) GetUserByPaddleSubscriptionID(ctx context.Context, subscriptionID string) (*domain.User, error) {
	blindIdx := r.enc.BlindIndex(subscriptionID)
	return r.queryUser(ctx, userSelectQuery+" WHERE paddle_subscription_id_blind_idx = $1 AND is_deleted = FALSE", blindIdx)
}

// GetUserByEmail retrieves a user by email using a dual lookup:
// 1. By blind index (for encrypted rows post-migration)
// 2. By LOWER(email) plaintext (for unmigrated rows from before encryption)
// This prevents duplicate accounts when encrypt-migrate hasn't been run yet (#1001).
func (r *profileRepository) GetUserByEmail(ctx context.Context, email string) (*domain.User, error) {
	normalized := appcrypto.NormalizeEmailForBlindIndex(email)
	return r.queryUser(ctx,
		userSelectQuery+" WHERE (email_blind_idx = $1 OR LOWER(email) = $2) AND is_deleted = FALSE",
		r.enc.BlindIndex(normalized), normalized)
}

// CreateUser creates a new user in the database
func (r *profileRepository) CreateUser(ctx context.Context, user *domain.User) error {
	// Generate UUID if not already set
	if user.ID == "" {
		user.ID = uuid.New().String()
	}

	// Set timestamps
	now := time.Now()
	user.CreatedAt = now
	user.UpdatedAt = now

	encryptedEmail, err := r.enc.EncryptString(user.Email)
	if err != nil {
		return fmt.Errorf("failed to encrypt email: %w", err)
	}
	emailBlindIdx := r.enc.BlindIndex(appcrypto.NormalizeEmailForBlindIndex(user.Email))

	encryptedClerkUserID, err := r.enc.EncryptString(user.ClerkUserID)
	if err != nil {
		return fmt.Errorf("failed to encrypt clerk_user_id: %w", err)
	}
	clerkUserIDBlindIdx := r.enc.BlindIndex(user.ClerkUserID)

	encryptedPaddleCustomerID, err := r.enc.EncryptNullableString(user.PaddleCustomerID)
	if err != nil {
		return fmt.Errorf("failed to encrypt paddle_customer_id: %w", err)
	}
	encryptedPaddleSubscriptionID, err := r.enc.EncryptNullableString(user.PaddleSubscriptionID)
	if err != nil {
		return fmt.Errorf("failed to encrypt paddle_subscription_id: %w", err)
	}
	paddleSubscriptionIDBlindIdx := r.enc.BlindIndexNullable(user.PaddleSubscriptionID)

	query := `
		INSERT INTO users (
			id, clerk_user_id, email, email_verified,
			subscription_tier, subscription_status, subscription_started_at, subscription_ends_at,
			trial_started_at, trial_ends_at, had_trial,
			paddle_customer_id, paddle_subscription_id, primary_region, is_active, is_deleted,
			deletion_requested_at, deletion_scheduled_at,
			created_at, updated_at, last_login_at,
			email_blind_idx, clerk_user_id_blind_idx, paddle_subscription_id_blind_idx
		) VALUES (
			$1, $2, $3, $4,
			$5, $6, $7, $8,
			$9, $10, $11,
			$12, $13, $14, $15, $16,
			$17, $18,
			$19, $20, $21,
			$22, $23, $24
		)
	`

	_, err = r.db.Exec(ctx, query,
		user.ID,
		encryptedClerkUserID,
		encryptedEmail,
		user.EmailVerified,
		user.SubscriptionTier,
		user.SubscriptionStatus,
		user.SubscriptionStartedAt,
		user.SubscriptionEndsAt,
		user.TrialStartedAt,
		user.TrialEndsAt,
		user.HadTrial,
		encryptedPaddleCustomerID,
		encryptedPaddleSubscriptionID,
		user.PrimaryRegion,
		user.IsActive,
		user.IsDeleted,
		user.DeletionRequestedAt,
		user.DeletionScheduledAt,
		user.CreatedAt,
		user.UpdatedAt,
		user.LastLoginAt,
		emailBlindIdx,
		clerkUserIDBlindIdx,
		paddleSubscriptionIDBlindIdx,
	)

	return r.checkDuplicateError(err)
}

// checkDuplicateError maps database constraint errors to domain errors
func (r *profileRepository) checkDuplicateError(err error) error {
	if err == nil {
		return nil
	}
	if (strings.Contains(err.Error(), "duplicate key") || strings.Contains(err.Error(), "unique constraint")) &&
		(strings.Contains(err.Error(), "unique_users_email_blind") || strings.Contains(err.Error(), "email")) {
		return ErrDuplicateEmail
	}
	return err
}

// insertPrefixIndicesTx inserts prefix blind indices for a user in a single multi-row statement.
// ON CONFLICT DO NOTHING ensures idempotency — safe to call even if rows already exist.
// Returns immediately (no-op) when blindIndices is empty.
func (r *profileRepository) insertPrefixIndicesTx(
	ctx context.Context, tx pgx.Tx, userID string, blindIndices []string,
) error {
	if len(blindIndices) == 0 {
		return nil
	}
	// Single multi-row INSERT avoids per-row round trips and pgx.SendBatch footguns.
	// Parameterised as: INSERT INTO ... VALUES ($1, $2), ($1, $3), ... ON CONFLICT DO NOTHING
	// $1 is reused for user_id across all rows; $2..$N are the prefix blind indices.
	args := make([]any, 0, 1+len(blindIndices))
	args = append(args, userID)
	placeholders := make([]string, 0, len(blindIndices))
	for i, idx := range blindIndices {
		args = append(args, idx)
		placeholders = append(placeholders, fmt.Sprintf("($1, $%d)", i+2))
	}
	query := fmt.Sprintf(
		"INSERT INTO user_email_prefix_indices (user_id, prefix_blind_idx) VALUES %s ON CONFLICT DO NOTHING",
		strings.Join(placeholders, ", "),
	)
	_, err := tx.Exec(ctx, query, args...)
	return err
}

// CreateUserTx creates a new user in the database within a transaction
func (r *profileRepository) CreateUserTx(ctx context.Context, tx pgx.Tx, user *domain.User) error {
	if user.ID == "" {
		user.ID = uuid.New().String()
	}

	now := time.Now()
	user.CreatedAt = now
	user.UpdatedAt = now

	encryptedEmail, err := r.enc.EncryptString(user.Email)
	if err != nil {
		return fmt.Errorf("failed to encrypt email: %w", err)
	}
	emailBlindIdx := r.enc.BlindIndex(appcrypto.NormalizeEmailForBlindIndex(user.Email))

	encryptedClerkUserID, err := r.enc.EncryptString(user.ClerkUserID)
	if err != nil {
		return fmt.Errorf("failed to encrypt clerk_user_id: %w", err)
	}
	clerkUserIDBlindIdx := r.enc.BlindIndex(user.ClerkUserID)

	encryptedPaddleCustomerID, err := r.enc.EncryptNullableString(user.PaddleCustomerID)
	if err != nil {
		return fmt.Errorf("failed to encrypt paddle_customer_id: %w", err)
	}
	encryptedPaddleSubscriptionID, err := r.enc.EncryptNullableString(user.PaddleSubscriptionID)
	if err != nil {
		return fmt.Errorf("failed to encrypt paddle_subscription_id: %w", err)
	}
	paddleSubscriptionIDBlindIdx := r.enc.BlindIndexNullable(user.PaddleSubscriptionID)

	query := `
		INSERT INTO users (
			id, clerk_user_id, email, email_verified,
			subscription_tier, subscription_status, subscription_started_at, subscription_ends_at,
			trial_started_at, trial_ends_at, had_trial,
			paddle_customer_id, paddle_subscription_id, primary_region, is_active, is_deleted,
			deletion_requested_at, deletion_scheduled_at,
			created_at, updated_at, last_login_at,
			email_blind_idx, clerk_user_id_blind_idx, paddle_subscription_id_blind_idx
		) VALUES (
			$1, $2, $3, $4,
			$5, $6, $7, $8,
			$9, $10, $11,
			$12, $13, $14, $15, $16,
			$17, $18,
			$19, $20, $21,
			$22, $23, $24
		)
	`

	_, err = tx.Exec(ctx, query,
		user.ID, encryptedClerkUserID, encryptedEmail, user.EmailVerified,
		user.SubscriptionTier, user.SubscriptionStatus, user.SubscriptionStartedAt, user.SubscriptionEndsAt,
		user.TrialStartedAt, user.TrialEndsAt, user.HadTrial,
		encryptedPaddleCustomerID, encryptedPaddleSubscriptionID, user.PrimaryRegion, user.IsActive, user.IsDeleted,
		user.DeletionRequestedAt, user.DeletionScheduledAt,
		user.CreatedAt, user.UpdatedAt, user.LastLoginAt,
		emailBlindIdx, clerkUserIDBlindIdx, paddleSubscriptionIDBlindIdx,
	)
	if err := r.checkDuplicateError(err); err != nil {
		return err
	}

	// Insert prefix blind indices so admin starts-with search works (#1178)
	indices := r.enc.EmailPrefixBlindIndices(user.Email)
	return r.insertPrefixIndicesTx(ctx, tx, user.ID, indices)
}

// UpdateUser updates an existing user's information
// Only updates allowed fields: email, email_verified, primary_region, timezone, timezone_source
// Subscription fields are managed separately through payment webhooks
func (r *profileRepository) UpdateUser(ctx context.Context, user *domain.User) error {
	user.UpdatedAt = time.Now()

	encryptedEmail, err := r.enc.EncryptString(user.Email)
	if err != nil {
		return fmt.Errorf("failed to encrypt email: %w", err)
	}
	emailBlindIdx := r.enc.BlindIndex(appcrypto.NormalizeEmailForBlindIndex(user.Email))

	query := `
		UPDATE users
		SET email = $1,
		    email_verified = $2,
		    primary_region = $3,
		    updated_at = $4,
		    email_blind_idx = $5,
		    timezone = $6,
		    timezone_source = $7
		WHERE id = $8
	`

	ct, err := r.db.Exec(ctx, query,
		encryptedEmail, user.EmailVerified, user.PrimaryRegion, user.UpdatedAt, emailBlindIdx,
		user.Timezone, user.TimezoneSource, user.ID,
	)

	if err != nil {
		return r.checkDuplicateError(err)
	}

	if ct.RowsAffected() == 0 {
		return ErrUserNotFound
	}
	return nil
}

// UpdateUserEmailTx updates a user's email, blind index, and resets email_verified=false
// within an existing transaction. Returns ErrDuplicateEmail if the email is already in use.
func (r *profileRepository) UpdateUserEmailTx(ctx context.Context, tx pgx.Tx, userID, plainEmail string) error {
	encryptedEmail, err := r.enc.EncryptString(plainEmail)
	if err != nil {
		return fmt.Errorf("failed to encrypt email: %w", err)
	}
	emailBlindIdx := r.enc.BlindIndex(appcrypto.NormalizeEmailForBlindIndex(plainEmail))
	now := time.Now()

	query := `
		UPDATE users
		SET email = $1,
		    email_blind_idx = $2,
		    email_verified = false,
		    updated_at = $3
		WHERE id = $4
	`
	ct, err := tx.Exec(ctx, query, encryptedEmail, emailBlindIdx, now, userID)
	if err != nil {
		return r.checkDuplicateError(err)
	}
	if ct.RowsAffected() == 0 {
		return ErrUserNotFound
	}

	// Delete stale prefix rows, then insert rows for the new email.
	// CRITICAL: This code appends AFTER the UPDATE users above, maintaining the
	// lock-ordering invariant (users row locked first → prefix rows second),
	// which prevents deadlocks with the startup backfill.
	if _, err := tx.Exec(ctx,
		"DELETE FROM user_email_prefix_indices WHERE user_id = $1", userID); err != nil {
		return fmt.Errorf("delete old prefix indices: %w", err)
	}
	indices := r.enc.EmailPrefixBlindIndices(plainEmail)
	return r.insertPrefixIndicesTx(ctx, tx, userID, indices)
}

// UpdateLastLogin updates the last_login_at timestamp for a user
func (r *profileRepository) UpdateLastLogin(ctx context.Context, clerkUserID string) error {
	now := time.Now()

	blindIdx := r.enc.BlindIndex(clerkUserID)

	query := `
		UPDATE users
		SET last_login_at = $1,
		    updated_at = $2
		WHERE clerk_user_id_blind_idx = $3
	`

	ct, err := r.db.Exec(ctx, query, now, now, blindIdx)
	if err != nil {
		return err
	}

	// Check if any rows were affected
	if ct.RowsAffected() == 0 {
		return ErrUserNotFound
	}

	return nil
}

// UpdatePaddleCustomerID links a Paddle customer ID to a user
// Issue #45: Paddle Payment Service Implementation
func (r *profileRepository) UpdatePaddleCustomerID(ctx context.Context, userID, paddleCustomerID string) error {
	now := time.Now()

	encryptedPaddleCustomerID, err := r.enc.EncryptString(paddleCustomerID)
	if err != nil {
		return fmt.Errorf("failed to encrypt paddle_customer_id: %w", err)
	}

	query := `
		UPDATE users
		SET paddle_customer_id = $1,
		    updated_at = $2
		WHERE id = $3 AND is_deleted = FALSE
	`

	ct, err := r.db.Exec(ctx, query, encryptedPaddleCustomerID, now, userID)
	if err != nil {
		return err
	}

	if ct.RowsAffected() == 0 {
		return ErrUserNotFound
	}

	return nil
}

// UpdateSubscriptionFromPaddle updates subscription details from Paddle webhooks.
// Applies status normalization (#933) as a defense-in-depth backstop before DB write.
// When subscriptionID is nil, preserves existing values (not included in SET clause).
func (r *profileRepository) UpdateSubscriptionFromPaddle(
	ctx context.Context,
	userID, tier, status string,
	startedAt, endsAt *time.Time,
	subscriptionID *string,
) error {
	// #933: Normalize status at repo layer as defense-in-depth backstop.
	status = domain.NormalizeSubscriptionStatus(status)

	now := time.Now()

	var query string
	var args []interface{}

	if subscriptionID != nil {
		// Overwrite both paddle_subscription_id columns.
		enc, err := r.enc.EncryptString(*subscriptionID)
		if err != nil {
			return fmt.Errorf("failed to encrypt paddle_subscription_id: %w", err)
		}
		blind := r.enc.BlindIndex(*subscriptionID)
		query = `UPDATE users SET subscription_tier = $1, subscription_status = $2, subscription_started_at = $3, subscription_ends_at = $4, paddle_subscription_id = $5, paddle_subscription_id_blind_idx = $6, updated_at = $7 WHERE id = $8 AND is_deleted = FALSE`
		args = []interface{}{tier, status, startedAt, endsAt, enc, blind, now, userID}
	} else {
		// Preserve existing paddle_subscription_id values (omit from SET clause).
		query = `UPDATE users SET subscription_tier = $1, subscription_status = $2, subscription_started_at = $3, subscription_ends_at = $4, updated_at = $5 WHERE id = $6 AND is_deleted = FALSE`
		args = []interface{}{tier, status, startedAt, endsAt, now, userID}
	}

	ct, err := r.db.Exec(ctx, query, args...)
	if err != nil {
		return err
	}

	if ct.RowsAffected() == 0 {
		return ErrUserNotFound
	}

	return nil
}

// MarkForDeletion marks a user for deletion with a scheduled date
func (r *profileRepository) MarkForDeletion(ctx context.Context, userID string, scheduledAt time.Time) error {
	now := time.Now()

	query := `
		UPDATE users
		SET deletion_requested_at = $1,
		    deletion_scheduled_at = $2,
		    updated_at = $3
		WHERE id = $4 AND is_deleted = FALSE
	`

	ct, err := r.db.Exec(ctx, query, now, scheduledAt, now, userID)
	if err != nil {
		return err
	}

	if ct.RowsAffected() == 0 {
		return ErrUserNotFound
	}

	return nil
}

// CancelDeletion cancels a pending deletion request
func (r *profileRepository) CancelDeletion(ctx context.Context, userID string) error {
	now := time.Now()

	query := `
		UPDATE users
		SET deletion_requested_at = NULL,
		    deletion_scheduled_at = NULL,
		    updated_at = $1
		WHERE id = $2 AND is_deleted = FALSE
	`

	ct, err := r.db.Exec(ctx, query, now, userID)
	if err != nil {
		return err
	}

	if ct.RowsAffected() == 0 {
		return ErrUserNotFound
	}

	return nil
}

// GetUsersScheduledForDeletion returns users whose deletion date has passed
func (r *profileRepository) GetUsersScheduledForDeletion(ctx context.Context) ([]*domain.User, error) {
	query := userSelectQuery + `
		WHERE deletion_scheduled_at IS NOT NULL
		  AND deletion_scheduled_at <= $1
		  AND is_deleted = FALSE
		ORDER BY deletion_scheduled_at ASC
	`
	return r.queryUsers(ctx, query, time.Now())
}

// DeleteUserCascade performs application-level cascade deletion of all user data
// This implements the 10-step deletion process to maintain audit trail
func (r *profileRepository) DeleteUserCascade(ctx context.Context, userID string) error {
	// Start a transaction for atomicity
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	now := time.Now()

	// Step 1: Delete all boosts
	_, err = tx.Exec(ctx, `DELETE FROM boosts WHERE user_id = $1`, userID)
	if err != nil {
		return err
	}

	// Step 2: Delete all transactions
	_, err = tx.Exec(ctx, `DELETE FROM transactions WHERE user_id = $1`, userID)
	if err != nil {
		return err
	}

	// Step 3: Delete all linked items (bank connections)
	// NOTE: This does NOT call Plaid /item/remove - that's handled by DeletionService
	// before calling this method to stop Plaid billing
	_, err = tx.Exec(ctx, `DELETE FROM linked_items WHERE user_id = $1`, userID)
	if err != nil {
		return err
	}

	// Step 4: Delete all accounts
	_, err = tx.Exec(ctx, `DELETE FROM accounts WHERE user_id = $1`, userID)
	if err != nil {
		return err
	}

	// Step 5: Delete all recurring patterns
	_, err = tx.Exec(ctx, `DELETE FROM recurring_patterns WHERE user_id = $1`, userID)
	if err != nil {
		return err
	}

	// Step 6: Delete all user preferences
	_, err = tx.Exec(ctx, `DELETE FROM user_preferences WHERE user_id = $1`, userID)
	if err != nil {
		return err
	}

	// Step 7: Delete all notifications
	_, err = tx.Exec(ctx, `DELETE FROM notifications WHERE user_id = $1`, userID)
	if err != nil {
		return err
	}

	// Step 8: Anonymize audit logs (keep for legal/compliance, but remove PII)
	_, err = tx.Exec(ctx, `
		UPDATE audit_logs
		SET metadata = jsonb_set(
			COALESCE(metadata, '{}'::jsonb),
			'{anonymized}',
			'true'::jsonb
		)
		WHERE user_id = $1
	`, userID)
	if err != nil {
		return err
	}

	// Step 9: Delete user sessions
	_, err = tx.Exec(ctx, `DELETE FROM user_sessions WHERE user_id = $1`, userID)
	if err != nil {
		return err
	}

	// Step 10: Mark user as deleted — encrypt the tombstone values and clear blind indexes
	deletedEmail := fmt.Sprintf("deleted_%s@deleted.local", userID)
	deletedClerkUserID := fmt.Sprintf("deleted_%s", userID)

	encryptedDeletedEmail, err := r.enc.EncryptString(deletedEmail)
	if err != nil {
		return fmt.Errorf("failed to encrypt deleted email: %w", err)
	}

	encryptedDeletedClerkUserID, err := r.enc.EncryptString(deletedClerkUserID)
	if err != nil {
		return fmt.Errorf("failed to encrypt deleted clerk_user_id: %w", err)
	}

	_, err = tx.Exec(ctx, `
		UPDATE users
		SET is_deleted = TRUE,
		    is_active = FALSE,
		    email = $1,
		    clerk_user_id = $2,
		    paddle_customer_id = NULL,
		    paddle_subscription_id = NULL,
		    email_blind_idx = NULL,
		    clerk_user_id_blind_idx = NULL,
		    paddle_subscription_id_blind_idx = NULL,
		    deletion_requested_at = NULL,
		    deletion_scheduled_at = NULL,
		    updated_at = $3
		WHERE id = $4
	`, encryptedDeletedEmail, encryptedDeletedClerkUserID, now, userID)
	if err != nil {
		return err
	}

	// Commit the transaction
	if err = tx.Commit(ctx); err != nil {
		return err
	}

	return nil
}

// claimForDeletionPredicate expresses the ratified product rule that a user's own GDPR
// deletion request beats an admin comp: the claim succeeds when EITHER the user has an
// outstanding deletion request of their own, OR there is no manual tier override AT ALL (live or
// already lapsed).
//
// This reuses noManualOverrideAtAllPredicate unmodified, wrapped in "TRUE <fragment>" so its
// leading "AND (...)" becomes a standalone boolean usable inside this OR — the same splicing
// trick previously used (incorrectly, for this predicate) with noActiveManualOverridePredicate.
//
// Issue #1413 Critical finding C1 (third round of remediation — the first two rounds left this
// on the live-only rule): this predicate MUST agree with the retention queries
// (GetUsersWithExpiredRetention et al.), which already exclude ANY override, live or lapsed, via
// noManualOverrideAtAllPredicate. Before this fix, claimForDeletionPredicate used
// noActiveManualOverridePredicate ("still live right now") instead — the exact rule that's
// correct for point-in-time transitions like CancelIfPastDueWithRetention, but wrong here: a comp that lapsed
// moments before the independently-scheduled manual_tier_override_expiry_check job clears the
// column would pass the live-only check and be hard-deleted, with no warning, in the ordering gap
// between that job and the retention worker. That is the original data-loss bug this whole branch
// (#1413) exists to fix — see noManualOverrideAtAllPredicate's doc comment for the full race, AND
// for why this is honestly a delay only while manual_tier_override_expiry_check keeps running
// successfully every day, not a guaranteed bound — that job's own failures are the thing to watch.
// TestClaimForDeletion_Integration_ExpiredOverride_NoDeletionRequest_NotClaimed /
// TestClaimForDeletion_Integration_ClearedOverride_Claims are the real-DB proof this is a delay
// while the expiry job is healthy, not a permanent exemption.
//
// Deliberately still a SEPARATE predicate from noActiveManualOverridePredicate, which stays
// exactly as it was for its own callers (CancelIfPastDueWithRetention, ClaimPlaidDisconnectIfPastDue,
// ExpireTrialIfNoOverride) — those are point-in-time transitions guarded against a comp granted
// in a race window, not a cross-job-ordering hazard like this one.
const claimForDeletionPredicate = `
	  AND (
	    deletion_requested_at IS NOT NULL
	    OR (TRUE` + noManualOverrideAtAllPredicate + `)
	  )
`

// ClaimForDeletion atomically verifies, at the moment of the claim, that a user is still
// eligible for ExecuteDeletion's irreversible work. See the ProfileRepository interface doc for
// the full rationale and ordering requirement (Issue #1413 Critical finding C2, Codex review
// round 2). This is a self-assigning no-op write (updated_at only) purely to obtain an atomic,
// row-level check via a single UPDATE ... WHERE statement, mirroring
// ClaimPlaidDisconnectIfPastDue's shape — without adding a new "claimed" column.
func (r *profileRepository) ClaimForDeletion(ctx context.Context, userID string) (bool, error) {
	ct, err := r.db.Exec(ctx, `
		UPDATE users
		SET updated_at = NOW()
		WHERE id = $1 AND is_deleted = FALSE
	`+claimForDeletionPredicate,
		userID)
	if err != nil {
		return false, fmt.Errorf("ClaimForDeletion: %w", err)
	}
	return ct.RowsAffected() > 0, nil
}

// UpdateTrialStatus updates trial-related fields for a user
// Issue #679: Trial service and auto-grant logic
func (r *profileRepository) UpdateTrialStatus(ctx context.Context, userID string, status string, trialStartedAt, trialEndsAt *time.Time, hadTrial bool) error {
	now := time.Now()

	query := `
		UPDATE users
		SET subscription_status = $1,
		    subscription_tier = 'premium',
		    trial_started_at = $2,
		    trial_ends_at = $3,
		    had_trial = $4,
		    updated_at = $5
		WHERE id = $6 AND is_deleted = FALSE
	`

	ct, err := r.db.Exec(ctx, query, status, trialStartedAt, trialEndsAt, hadTrial, now, userID)
	if err != nil {
		return err
	}

	if ct.RowsAffected() == 0 {
		return ErrUserNotFound
	}

	return nil
}

// UpdateSubscriptionStatus updates just the subscription status for a user
// Issue #679, #680: Trial expiration
func (r *profileRepository) UpdateSubscriptionStatus(ctx context.Context, userID string, status string) error {
	now := time.Now()

	query := `
		UPDATE users
		SET subscription_status = $1,
		    updated_at = $2
		WHERE id = $3 AND is_deleted = FALSE
	`

	ct, err := r.db.Exec(ctx, query, status, now, userID)
	if err != nil {
		return err
	}

	if ct.RowsAffected() == 0 {
		return ErrUserNotFound
	}

	return nil
}

// GetEffectiveSubscriptionStatusByUserID retrieves the EFFECTIVE subscription status for a
// user — "active" when a non-expired manual tier override (admin comp) is in effect,
// otherwise the raw subscription_status (Issue #1413). Used by the read-only middleware
// (#930) for efficient per-request status checks.
//
// The override predicate is computed in Go via domain.User.ComputeEffectiveStatus rather
// than in SQL, so this row-level read shares exactly one definition of "is the override
// active" with every other caller of ComputeEffectiveStatus/ComputeEffectiveTier.
//
// NOTE: this rename (from GetSubscriptionStatusByUserID) is deliberate, not cosmetic — the
// old name described a raw column read; this method returns a computed value. Keeping the
// old name would risk a future "simplification" silently reverting to the raw column.
func (r *profileRepository) GetEffectiveSubscriptionStatusByUserID(ctx context.Context, userID string) (string, error) {
	var u domain.User
	err := r.db.QueryRow(ctx,
		`SELECT subscription_status, manual_tier_override, manual_tier_ends_at FROM users WHERE id = $1 AND is_deleted = FALSE`,
		userID,
	).Scan(&u.SubscriptionStatus, &u.ManualTierOverride, &u.ManualTierEndsAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrUserNotFound
	}
	if err != nil {
		return "", err
	}
	return u.ComputeEffectiveStatus(), nil
}

// GetUsersWithExpiredTrials returns users with trial status but past trial_ends_at
// Issue #680: Trial expiration cron job
func (r *profileRepository) GetUsersWithExpiredTrials(ctx context.Context) ([]*domain.User, error) {
	query := userSelectQuery + `
		WHERE subscription_status = 'trial'
		  AND trial_ends_at IS NOT NULL
		  AND trial_ends_at <= $1
		  AND is_deleted = FALSE
		ORDER BY trial_ends_at ASC
	`
	return r.queryUsers(ctx, query, time.Now())
}

// GetUsersWithExpiringTrials returns users whose trial ends within N days
// Issue #680: Trial expiration notification (3 days before)
func (r *profileRepository) GetUsersWithExpiringTrials(ctx context.Context, withinDays int) ([]*domain.User, error) {
	now := time.Now()
	threshold := now.Add(time.Duration(withinDays) * 24 * time.Hour)

	query := userSelectQuery + `
		WHERE subscription_status = 'trial'
		  AND trial_ends_at IS NOT NULL
		  AND trial_ends_at > $1
		  AND trial_ends_at <= $2
		  AND is_deleted = FALSE
		ORDER BY trial_ends_at ASC
	`
	return r.queryUsers(ctx, query, now, threshold)
}

// GetUsersWithActivePaddleSubscriptions returns users with active Paddle subscriptions
// Issue #762: Paddle admin tools reconciliation
func (r *profileRepository) GetUsersWithActivePaddleSubscriptions(ctx context.Context) ([]*domain.User, error) {
	query := userSelectQuery + ` WHERE paddle_subscription_id_blind_idx IS NOT NULL AND subscription_status NOT IN ('inactive', 'cancelled') AND is_deleted = FALSE`
	users, err := r.queryUsers(ctx, query)
	if err != nil {
		return nil, err
	}
	if users == nil {
		return []*domain.User{}, nil
	}
	return users, nil
}

// SetRetentionDates sets the retention_started_at and retention_expires_at for a user.
// Called when a user's subscription is cancelled or trial expires (#930).
func (r *profileRepository) SetRetentionDates(ctx context.Context, userID string, startedAt, expiresAt time.Time) error {
	now := time.Now()
	ct, err := r.db.Exec(ctx, `
		UPDATE users
		SET retention_started_at = $1,
		    retention_expires_at = $2,
		    updated_at = $3
		WHERE id = $4 AND is_deleted = FALSE`,
		startedAt, expiresAt, now, userID,
	)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return ErrUserNotFound
	}
	return nil
}

// ClearRetentionDates clears all retention tracking fields on resubscription (#930).
func (r *profileRepository) ClearRetentionDates(ctx context.Context, userID string) error {
	now := time.Now()
	ct, err := r.db.Exec(ctx, `
		UPDATE users
		SET retention_started_at = NULL,
		    retention_expires_at = NULL,
		    retention_notice_30d_sent_at = NULL,
		    retention_notice_7d_sent_at = NULL,
		    updated_at = $1
		WHERE id = $2 AND is_deleted = FALSE`,
		now, userID,
	)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return ErrUserNotFound
	}
	return nil
}

// SetRetentionNotice30dSent marks the 30-day pre-deletion notice as sent (#930).
func (r *profileRepository) SetRetentionNotice30dSent(ctx context.Context, userID string) error {
	now := time.Now()
	ct, err := r.db.Exec(ctx, `
		UPDATE users
		SET retention_notice_30d_sent_at = $1,
		    updated_at = $1
		WHERE id = $2 AND is_deleted = FALSE`,
		now, userID,
	)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return ErrUserNotFound
	}
	return nil
}

// SetRetentionNotice7dSent marks the 7-day pre-deletion notice as sent (#930).
func (r *profileRepository) SetRetentionNotice7dSent(ctx context.Context, userID string) error {
	now := time.Now()
	ct, err := r.db.Exec(ctx, `
		UPDATE users
		SET retention_notice_7d_sent_at = $1,
		    updated_at = $1
		WHERE id = $2 AND is_deleted = FALSE`,
		now, userID,
	)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return ErrUserNotFound
	}
	return nil
}

// IsRetentionNotice30dEligible re-verifies FULL 30-day notice eligibility for a single user,
// mirroring GetUsersNeedingRetention30dNotice's WHERE clause field-for-field (Issue #1413 Medium
// finding M9, Codex review round 3) so the candidate query and this per-user re-check can never
// silently drift apart. Performs NO write — see the ProfileRepository interface doc for why the
// claim / eligibility-check / send / stamp sequence is deliberately split this way.
func (r *profileRepository) IsRetentionNotice30dEligible(ctx context.Context, userID string) (bool, error) {
	var eligible bool
	err := r.db.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM users
			WHERE id = $1
			  AND is_deleted = FALSE
			  AND retention_expires_at IS NOT NULL
			  AND retention_expires_at - INTERVAL '30 days' <= NOW()
			  AND retention_expires_at > NOW()
			  AND retention_notice_30d_sent_at IS NULL
			  AND subscription_status IN ('inactive', 'cancelled')
			  AND deletion_requested_at IS NULL
	`+noManualOverrideAtAllPredicate+`
		)
	`, userID).Scan(&eligible)
	if err != nil {
		return false, fmt.Errorf("IsRetentionNotice30dEligible: %w", err)
	}
	return eligible, nil
}

// IsRetentionNotice7dEligible is IsRetentionNotice30dEligible's 7-day sibling. See that method's
// doc for the full rationale.
func (r *profileRepository) IsRetentionNotice7dEligible(ctx context.Context, userID string) (bool, error) {
	var eligible bool
	err := r.db.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM users
			WHERE id = $1
			  AND is_deleted = FALSE
			  AND retention_expires_at IS NOT NULL
			  AND retention_expires_at - INTERVAL '7 days' <= NOW()
			  AND retention_expires_at > NOW()
			  AND retention_notice_7d_sent_at IS NULL
			  AND subscription_status IN ('inactive', 'cancelled')
			  AND deletion_requested_at IS NULL
	`+noManualOverrideAtAllPredicate+`
		)
	`, userID).Scan(&eligible)
	if err != nil {
		return false, fmt.Errorf("IsRetentionNotice7dEligible: %w", err)
	}
	return eligible, nil
}

// IsRetentionNotice30dEligibleForOccurrence is IsRetentionNotice30dEligible's occurrence-scoped
// sibling (Issue #1413 round-4 finding M1). IsRetentionNotice30dEligible re-verifies
// window/sent-state/status/override but NOT that the row's CURRENT retention_expires_at still
// equals the value the caller claimed. retention_cleanup_check.go's processRetentionNotices
// claims using the CANDIDATE's retention_expires_at (E1), but the row can have been RE-ARMED to a
// DIFFERENT value (E2) by the time this re-check runs — resubscribe (ClearRetentionDates), then
// cancel again, restarting the clock from a later point. With a short (e.g. one-month) retention
// configuration, two workers can hold E1 and E2 and BOTH see the current clock as eligible (E2 is
// ALSO genuinely in-window, unsent, uncomped right now) — the user gets a DUPLICATE notice.
// Requiring `retention_expires_at = expectedRetentionExpiresAt` closes this: the check only
// passes for the EXACT occurrence the caller claimed.
//
// Consequence is milder than H1/H3 (this branch already accepts at-least-once for notices over
// never-warn — see processRetentionNotices' doc comment), but the same discipline applies: the
// eligibility check must validate the discriminator it claimed.
func (r *profileRepository) IsRetentionNotice30dEligibleForOccurrence(ctx context.Context, userID string, expectedRetentionExpiresAt time.Time) (bool, error) {
	var eligible bool
	err := r.db.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM users
			WHERE id = $1
			  AND is_deleted = FALSE
			  AND retention_expires_at = $2
			  AND retention_expires_at - INTERVAL '30 days' <= NOW()
			  AND retention_expires_at > NOW()
			  AND retention_notice_30d_sent_at IS NULL
			  AND subscription_status IN ('inactive', 'cancelled')
			  AND deletion_requested_at IS NULL
	`+noManualOverrideAtAllPredicate+`
		)
	`, userID, expectedRetentionExpiresAt).Scan(&eligible)
	if err != nil {
		return false, fmt.Errorf("IsRetentionNotice30dEligibleForOccurrence: %w", err)
	}
	return eligible, nil
}

// IsRetentionNotice7dEligibleForOccurrence is IsRetentionNotice30dEligibleForOccurrence's 7-day
// sibling. See that method's doc for the full rationale.
func (r *profileRepository) IsRetentionNotice7dEligibleForOccurrence(ctx context.Context, userID string, expectedRetentionExpiresAt time.Time) (bool, error) {
	var eligible bool
	err := r.db.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM users
			WHERE id = $1
			  AND is_deleted = FALSE
			  AND retention_expires_at = $2
			  AND retention_expires_at - INTERVAL '7 days' <= NOW()
			  AND retention_expires_at > NOW()
			  AND retention_notice_7d_sent_at IS NULL
			  AND subscription_status IN ('inactive', 'cancelled')
			  AND deletion_requested_at IS NULL
	`+noManualOverrideAtAllPredicate+`
		)
	`, userID, expectedRetentionExpiresAt).Scan(&eligible)
	if err != nil {
		return false, fmt.Errorf("IsRetentionNotice7dEligibleForOccurrence: %w", err)
	}
	return eligible, nil
}

// noActiveManualOverridePredicate excludes users with a currently-active admin manual tier
// override ("comp") from set-based retention queries (Issue #1413). A comped user must never
// be warned about, or auto-deleted for, retention expiry.
//
// This is the SQL-side twin of domain.User.HasActiveManualOverride() — that Go predicate
// governs row-level reads, this one governs set-based queries where fetching every candidate
// row just to filter in Go would be wasteful. The two must agree on every case; see
// TestNoActiveManualOverridePredicate_Integration_ParityWithGo (a real-database test) for the
// guard that keeps them from drifting apart.
//
// Boundary note: `manual_tier_ends_at >= NOW()` deliberately matches Go's
// `!ends_at.Before(now)` (i.e. "not yet expired" is true AT the expiry instant, not only
// strictly before it). Using `>` here would disagree with the Go predicate at the exact
// expiry instant.
//
// As of the Issue #1413 C1 fix this predicate had no production caller — the three retention
// queries below use noManualOverrideAtAllPredicate instead (see that constant's comment for why),
// and TestNoActiveManualOverridePredicate_Integration_ParityWithGo /
// TestNoActiveManualOverridePredicate_Integration_ExactExpiryBoundary kept exercising it purely
// so the exact production string wouldn't silently drift from a hand-copied restatement. As of
// the Issue #1413 Codex review round 3 fix (High findings H5/H6), it now has real production
// callers too — CancelIfPastDueWithRetention and ExpireTrialIfNoOverride splice this constant into a
// conditional UPDATE's WHERE clause, per the claim-then-act pattern (a third former caller,
// ClaimPlaidDisconnectIfPastDue, was deleted in round 4 — see past_due_service.go's H6a fix,
// which replaced its self-assigning-update "claim" with a real work_claims claim): this is the
// one place the "still live right now" rule (as opposed to
// noManualOverrideAtAllPredicate's "not even a lapsed one") is actually the correct rule to
// enforce, because these are point-in-time transitions guarded against a comp granted in a race
// window, not the retention-notice/hard-delete queries' cross-job-ordering hazard.
const noActiveManualOverridePredicate = `
	  AND NOT (
	      manual_tier_override IS NOT NULL
	      AND manual_tier_override <> ''
	      AND (manual_tier_ends_at IS NULL OR manual_tier_ends_at >= NOW())
	  )
`

// noManualOverrideAtAllPredicate excludes a user with ANY non-empty manual_tier_override —
// live OR already-lapsed — from the retention notice/hard-delete queries (Issue #1413 Critical
// finding C1, Codex review).
//
// Why this must differ from noActiveManualOverridePredicate above: a comp *pauses* the
// retention clock while live, and is re-armed by the separate, independently-scheduled
// manual_tier_override_expiry_check daily job the moment the comp lapses. That job has NO
// ordering guarantee relative to the retention_cleanup_check job that runs these queries. If
// retention selection used the "only exclude while still live" rule
// (noActiveManualOverridePredicate), a user whose comp lapsed moments before the expiry job
// re-arms their clock would be selected here using a stale retention_expires_at from BEFORE the
// comp even started — hard-deleted with no 30-day/7-day warning ever sent, in the race window
// between the two jobs.
//
// Excluding on ANY non-empty override, live or lapsed, never causes a wrongful deletion — but the
// delay this buys is bounded ONLY by manual_tier_override_expiry_check actually running
// successfully every day, not by anything in this predicate. That job is best-effort: it is
// registered with PeriodicJobOpts{RunOnStart: false}, and a failed run's retries eventually land
// in the dead-letter queue with NO automatic replay (Issue #1413 Critical finding C1's "unbounded
// exclusion" follow-up). If that job silently stops working, this predicate keeps excluding the
// user indefinitely — "never deleted", not "delayed by a day" or "one job cycle" — until someone
// notices the dead-lettered job and replays it. See manual_tier_override_expiry_check.go's Work()
// for the failure metric/log this honesty depends on staying observable. See
// TestRetentionQueries_Integration_ExpiredOverride_ExcludedUntilCleared for proof this is a delay
// WHILE THE EXPIRY JOB KEEPS RUNNING, not a permanent exemption by itself, and
// TestGetUsersNeedingRetention30dNotice_Integration_ExcludesActiveComp (and its 7d/expired
// siblings) for proof an ordinary never-comped user is unaffected.
//
// Deliberately a SEPARATE predicate from noActiveManualOverridePredicate, which stays exactly as
// it was — it is the SQL twin of domain.User.HasActiveManualOverride() (row-level reads) and a
// parity test guards that pairing. Two predicates, two different jobs, two different correctness
// requirements: one governs "is the comp live right now", the other governs "is it safe to
// notice/delete this account".
const noManualOverrideAtAllPredicate = `
	  AND (manual_tier_override IS NULL OR manual_tier_override = '')
`

// GetUsersNeedingRetention30dNotice returns users whose retention_expires_at is within 30 days
// and who have not yet received a 30-day pre-deletion warning (#930).
func (r *profileRepository) GetUsersNeedingRetention30dNotice(ctx context.Context) ([]*domain.User, error) {
	query := userSelectQuery + `
		WHERE retention_expires_at IS NOT NULL
		  AND retention_expires_at - INTERVAL '30 days' <= NOW()
		  AND retention_expires_at > NOW()
		  AND retention_notice_30d_sent_at IS NULL
		  AND subscription_status IN ('inactive', 'cancelled')
		  AND is_deleted = FALSE
		  AND deletion_requested_at IS NULL
	` + noManualOverrideAtAllPredicate + `
		ORDER BY retention_expires_at ASC
	`
	users, err := r.queryUsers(ctx, query)
	if err != nil {
		return nil, err
	}
	if users == nil {
		return []*domain.User{}, nil
	}
	return users, nil
}

// GetUsersNeedingRetention7dNotice returns users whose retention_expires_at is within 7 days
// and who have not yet received a 7-day pre-deletion warning (#930).
func (r *profileRepository) GetUsersNeedingRetention7dNotice(ctx context.Context) ([]*domain.User, error) {
	query := userSelectQuery + `
		WHERE retention_expires_at IS NOT NULL
		  AND retention_expires_at - INTERVAL '7 days' <= NOW()
		  AND retention_expires_at > NOW()
		  AND retention_notice_7d_sent_at IS NULL
		  AND subscription_status IN ('inactive', 'cancelled')
		  AND is_deleted = FALSE
		  AND deletion_requested_at IS NULL
	` + noManualOverrideAtAllPredicate + `
		ORDER BY retention_expires_at ASC
	`
	users, err := r.queryUsers(ctx, query)
	if err != nil {
		return nil, err
	}
	if users == nil {
		return []*domain.User{}, nil
	}
	return users, nil
}

// GetUsersWithExpiredRetention returns users whose retention_expires_at has passed
// and who are not already deleted or pending deletion (#930).
func (r *profileRepository) GetUsersWithExpiredRetention(ctx context.Context) ([]*domain.User, error) {
	query := userSelectQuery + `
		WHERE retention_expires_at IS NOT NULL
		  AND retention_expires_at <= NOW()
		  AND subscription_status IN ('inactive', 'cancelled')
		  AND is_deleted = FALSE
		  AND deletion_requested_at IS NULL
	` + noManualOverrideAtAllPredicate + `
		ORDER BY retention_expires_at ASC
	`
	users, err := r.queryUsers(ctx, query)
	if err != nil {
		return nil, err
	}
	if users == nil {
		return []*domain.User{}, nil
	}
	return users, nil
}

// GetUsersNeedingRetentionClockRepair implements the Issue #1413 H5 residual repair path — see
// the interface doc comment for the full rationale. Mirrors GetUsersWithExpiredRetention's
// predicate shape except retention_expires_at IS NULL instead of IS NOT NULL/expired.
func (r *profileRepository) GetUsersNeedingRetentionClockRepair(ctx context.Context) ([]*domain.User, error) {
	query := userSelectQuery + `
		WHERE retention_expires_at IS NULL
		  AND subscription_status IN ('inactive', 'cancelled')
		  AND is_deleted = FALSE
		  AND deletion_requested_at IS NULL
	` + noManualOverrideAtAllPredicate + `
		ORDER BY updated_at ASC
	`
	users, err := r.queryUsers(ctx, query)
	if err != nil {
		return nil, err
	}
	if users == nil {
		return []*domain.User{}, nil
	}
	return users, nil
}

// RepairRetentionClock implements the Issue #1413 H5 residual repair write — see the interface
// doc comment for why a conditional UPDATE (not a work_claims claim) is sufficient here.
func (r *profileRepository) RepairRetentionClock(ctx context.Context, userID string, startedAt, expiresAt time.Time) (bool, error) {
	ct, err := r.db.Exec(ctx, `
		UPDATE users
		SET retention_started_at = $2,
		    retention_expires_at = $3,
		    updated_at = NOW()
		WHERE id = $1
		  AND retention_expires_at IS NULL
		  AND subscription_status IN ('inactive', 'cancelled')
		  AND is_deleted = FALSE
		  AND deletion_requested_at IS NULL
	`+noManualOverrideAtAllPredicate,
		userID, startedAt, expiresAt)
	if err != nil {
		return false, fmt.Errorf("RepairRetentionClock: %w", err)
	}
	return ct.RowsAffected() > 0, nil
}

// expiredManualTierOverridePredicate selects users with a manual tier override that has
// strictly passed (Issue #1413 Task 9 — the daily job that expires lapsed admin comps,
// performing the exact same clear-override/re-arm-retention/audit transition
// AdminUserRepository.RemoveUserTierOverride (Issue #1414 / Task 7) performs for
// admin-initiated removal, rather than duplicating that logic here).
//
// Boundary note: strictly past (`<`, not `<=`) to agree with
// domain.User.HasActiveManualOverride()'s `!ends_at.Before(now)` semantics — an override ending
// exactly now is still ACTIVE per the read path, so this predicate must not select it (see
// TestGetUsersWithExpiredManualTierOverride_Integration_ExactExpiryBoundary).
//
// manual_tier_ends_at IS NOT NULL additionally excludes PERMANENT overrides (no expiry date
// set) — those never lapse on their own and must only ever be cleared by an explicit admin
// removal.
const expiredManualTierOverridePredicate = `
	  manual_tier_override IS NOT NULL
	  AND manual_tier_override <> ''
	  AND manual_tier_ends_at IS NOT NULL
	  AND manual_tier_ends_at < NOW()
	  AND is_deleted = FALSE
`

// GetUsersWithExpiredManualTierOverride returns users whose manual_tier_override is set and
// whose manual_tier_ends_at has strictly passed (Issue #1413 Task 9).
func (r *profileRepository) GetUsersWithExpiredManualTierOverride(ctx context.Context) ([]*domain.User, error) {
	query := userSelectQuery + `
		WHERE ` + expiredManualTierOverridePredicate + `
		ORDER BY manual_tier_ends_at ASC
	`
	users, err := r.queryUsers(ctx, query)
	if err != nil {
		return nil, err
	}
	if users == nil {
		return []*domain.User{}, nil
	}
	return users, nil
}

// SetPastDueStartedAt sets the past_due_started_at timestamp for a user.
// Only sets the timestamp if it is currently NULL (does not reset on repeated calls).
// Issue #898: dunning job grace-period tracking.
func (r *profileRepository) SetPastDueStartedAt(ctx context.Context, userID string, t time.Time) error {
	_, err := r.db.Exec(ctx, `
		UPDATE users SET past_due_started_at = $2, updated_at = NOW()
		WHERE id = $1 AND past_due_started_at IS NULL`,
		userID, t,
	)
	if err != nil {
		return err
	}
	// Either not found or already set — both are acceptable (idempotent).
	return nil
}

// ClearPastDueStartedAt clears the past_due_started_at timestamp for a user.
// Called on resubscription so the grace period resets cleanly.
// Issue #898: dunning job grace-period tracking.
func (r *profileRepository) ClearPastDueStartedAt(ctx context.Context, userID string) error {
	ct, err := r.db.Exec(ctx, `
		UPDATE users SET past_due_started_at = NULL, updated_at = NOW()
		WHERE id = $1`,
		userID,
	)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return ErrUserNotFound
	}
	return nil
}

// ClearPastDueStartedAtIfMatches is ClearPastDueStartedAt's occurrence-scoped sibling (Issue
// #1413 round-4 finding H3). ClearPastDueStartedAt above is UNCONDITIONAL (WHERE id = $1) —
// correct for subscription_webhook.go's recovery-path callers, where Paddle telling us the
// subscription is no longer past_due IS the authorization. It is WRONG for
// processGraceExpiry's auto-cancel step (past_due_service.go), which captured a SPECIFIC dunning
// cycle's timestamp earlier in its own run: sequence — the CAS (CancelIfPastDueOccurrenceWithRetention)
// succeeds and the flow reaches this clear step; meanwhile the user recovers, then re-enters
// past_due, and the webhook stamps a NEW past_due_started_at (SetPastDueStartedAt) for the NEW
// cycle; the ORIGINAL flow, still holding its stale snapshot, then reaches an unconditional clear
// and would erase the NEW cycle's timestamp too. Because GetUsersInPastDue requires
// past_due_started_at IS NOT NULL, that user would never be selected again — stuck past-due
// forever with no dunning.
//
// The fix is a compare-and-swap: only clear when past_due_started_at STILL equals expected, the
// value the caller actually acted on.
//
// Distinguishes two zero-rows-affected causes that a single bool cannot tell apart:
//   - The user genuinely does not exist: (false, ErrUserNotFound), matching the plain
//     ClearPastDueStartedAt's existing contract — this is an anomaly worth surfacing as an error.
//   - The user exists but past_due_started_at has already moved on to a newer cycle: (false, nil)
//     — a legitimate, expected skip, NOT an error. The newer cycle's timestamp is exactly what
//     must survive untouched so a future dunning run can still select and process it; treating
//     this as an error would make a caller retry/alert on what is actually the fix working
//     correctly.
//
// Implemented as one round trip: the `existing` CTE independently proves whether the row exists
// at all, while the `updated` CTE performs the actual CAS write; a data-modifying CTE referenced
// exactly once always executes exactly once, so there is no double-write hazard here.
//
// NOTE: this is a NEW, separate method rather than a signature change to ClearPastDueStartedAt —
// subscription_webhook.go's three recovery-path callers need the unconditional version's
// semantics (no "expected occurrence" to compare against there), and per the #1413 P6 task's
// scope fence, that file is owned by a concurrent task. Once that surface settles, folding these
// back into one CAS-shaped method (with the unconditional case expressed as "no expectation
// supplied") would be a reasonable follow-up cleanup.
func (r *profileRepository) ClearPastDueStartedAtIfMatches(ctx context.Context, userID string, expected time.Time) (bool, error) {
	var userExists, cleared bool
	err := r.db.QueryRow(ctx, `
		WITH existing AS (
			SELECT 1 FROM users WHERE id = $1
		), updated AS (
			UPDATE users SET past_due_started_at = NULL, updated_at = NOW()
			WHERE id = $1 AND past_due_started_at = $2
			RETURNING 1
		)
		SELECT EXISTS(SELECT 1 FROM existing), EXISTS(SELECT 1 FROM updated)
	`, userID, expected).Scan(&userExists, &cleared)
	if err != nil {
		return false, fmt.Errorf("ClearPastDueStartedAtIfMatches: %w", err)
	}
	if !userExists {
		return false, ErrUserNotFound
	}
	return cleared, nil
}

// GetUsersInPastDue returns users with subscription_status='past_due' and non-nil past_due_started_at.
// Called by the daily dunning cron job to find candidates for grace-expiry or Plaid-disconnect processing.
// Issue #898: dunning job grace-period tracking.
func (r *profileRepository) GetUsersInPastDue(ctx context.Context) ([]domain.User, error) {
	query := userSelectQuery + `
		WHERE subscription_status = 'past_due'
		  AND past_due_started_at IS NOT NULL
		  AND is_deleted = FALSE
		ORDER BY past_due_started_at ASC
	`
	rows, err := r.db.Query(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var users []domain.User
	for rows.Next() {
		var user domain.User
		if err := rows.Scan(
			&user.ID, &user.ClerkUserID, &user.Email, &user.EmailVerified,
			&user.SubscriptionTier, &user.SubscriptionStatus, &user.SubscriptionStartedAt, &user.SubscriptionEndsAt,
			&user.TrialStartedAt, &user.TrialEndsAt, &user.HadTrial,
			&user.PaddleCustomerID, &user.PaddleSubscriptionID, &user.PrimaryRegion,
			&user.IsActive, &user.IsDeleted,
			&user.DeletionRequestedAt, &user.DeletionScheduledAt,
			&user.CreatedAt, &user.UpdatedAt, &user.LastLoginAt,
			&user.ManualTierOverride, &user.ManualTierEndsAt,
			&user.RetentionStartedAt, &user.RetentionExpiresAt, &user.RetentionNotice30dSentAt, &user.RetentionNotice7dSentAt,
			&user.Timezone, &user.TimezoneSource,
			&user.PastDueStartedAt,
		); err != nil {
			return nil, err
		}

		decryptedClerkUserID, err := r.enc.DecryptString(user.ClerkUserID)
		if err != nil {
			return nil, fmt.Errorf("failed to decrypt clerk_user_id: %w", err)
		}
		user.ClerkUserID = decryptedClerkUserID

		decryptedEmail, err := r.enc.DecryptString(user.Email)
		if err != nil {
			return nil, fmt.Errorf("failed to decrypt email: %w", err)
		}
		user.Email = decryptedEmail

		decryptedPaddleCustomerID, err := r.enc.DecryptNullableString(user.PaddleCustomerID)
		if err != nil {
			return nil, fmt.Errorf("failed to decrypt paddle_customer_id: %w", err)
		}
		user.PaddleCustomerID = decryptedPaddleCustomerID

		decryptedPaddleSubscriptionID, err := r.enc.DecryptNullableString(user.PaddleSubscriptionID)
		if err != nil {
			return nil, fmt.Errorf("failed to decrypt paddle_subscription_id: %w", err)
		}
		user.PaddleSubscriptionID = decryptedPaddleSubscriptionID

		users = append(users, user)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if users == nil {
		return []domain.User{}, nil
	}
	return users, nil
}

// CancelIfPastDueWithRetention atomically flips subscription_status to 'cancelled', starting the
// retention clock in the same statement — see the interface doc comment for the full Issue #1413
// H5 residual rationale. Predicate: still 'past_due' and carries no currently-active manual tier
// override (noActiveManualOverridePredicate). Issue #898: CAS guard against recovery race
// condition. Issue #1413 High finding H6 (Codex review round 3): predicate extended with
// noActiveManualOverridePredicate — a prior read-only HasActiveManualOverride() check in the
// caller cannot, by itself, protect this write, since the two are not atomic.
func (r *profileRepository) CancelIfPastDueWithRetention(ctx context.Context, userID string, retentionStarted, retentionExpires time.Time) (bool, error) {
	ct, err := r.db.Exec(ctx, `
		UPDATE users
		SET subscription_status = 'cancelled',
		    retention_started_at = $2,
		    retention_expires_at = $3,
		    updated_at = NOW()
		WHERE id = $1 AND subscription_status = 'past_due'
	`+noActiveManualOverridePredicate,
		userID, retentionStarted, retentionExpires)
	if err != nil {
		return false, fmt.Errorf("CancelIfPastDueWithRetention: %w", err)
	}
	return ct.RowsAffected() > 0, nil
}

// CancelIfPastDueOccurrenceWithRetention is CancelIfPastDueWithRetention's occurrence-scoped
// sibling (Issue #1413 round-4 finding H1). CancelIfPastDueWithRetention's predicate — still
// 'past_due' and no currently-active manual override — is necessary but not sufficient once a
// worker can be racing a NEWER past_due cycle for the SAME user: a batch snapshot captures cycle
// A's past_due_started_at, the user recovers, then re-enters past_due as cycle B (a genuinely NEW
// past_due_started_at) — cycle B is ALSO 'past_due' with no override, so the old predicate would
// happily flip it even though the worker only ever verified cycle A's grace period, not cycle B's
// (round-4 finding H1). Adding `AND past_due_started_at = expectedPastDueStartedAt` closes this:
// the CAS now only succeeds for the EXACT cycle the caller claimed and re-verified.
//
// Returns (true, nil) when the flip happened, (false, nil) whenever the predicate misses for ANY
// reason — recovered, comped, or (this fix) a different cycle now occupies the row. All three are
// the same "someone else's business, not an error" outcome for this caller: past_due_service.go's
// processGraceExpiry Releases (never Completes) its claim and lets a future run handle whatever
// cycle is actually current.
func (r *profileRepository) CancelIfPastDueOccurrenceWithRetention(ctx context.Context, userID string, expectedPastDueStartedAt, retentionStarted, retentionExpires time.Time) (bool, error) {
	ct, err := r.db.Exec(ctx, `
		UPDATE users
		SET subscription_status = 'cancelled',
		    retention_started_at = $2,
		    retention_expires_at = $3,
		    updated_at = NOW()
		WHERE id = $1 AND subscription_status = 'past_due' AND past_due_started_at = $4
	`+noActiveManualOverridePredicate,
		userID, retentionStarted, retentionExpires, expectedPastDueStartedAt)
	if err != nil {
		return false, fmt.Errorf("CancelIfPastDueOccurrenceWithRetention: %w", err)
	}
	return ct.RowsAffected() > 0, nil
}

// ExpireTrialIfNoOverride atomically flips subscription_status to 'inactive' only if the user is
// still 'trial' and carries no currently-active manual tier override. Returns (true, nil) if the
// flip happened, (false, nil) if an admin granted/extended a comp between the caller's earlier
// HasActiveManualOverride() read and this call, or the user's status has otherwise already moved
// on. Issue #1413 High finding H5, Codex review round 3: follows CancelIfPastDueWithRetention's
// shape — the predicate that authorises the transition lives in the UPDATE's WHERE clause,
// evaluated against committed state at the moment of the write, not in an earlier, non-atomic
// read-then-check.
func (r *profileRepository) ExpireTrialIfNoOverride(ctx context.Context, userID string) (bool, error) {
	ct, err := r.db.Exec(ctx, `
		UPDATE users
		SET subscription_status = 'inactive', updated_at = NOW()
		WHERE id = $1 AND subscription_status = 'trial'
	`+noActiveManualOverridePredicate,
		userID)
	if err != nil {
		return false, fmt.Errorf("ExpireTrialIfNoOverride: %w", err)
	}
	return ct.RowsAffected() > 0, nil
}
