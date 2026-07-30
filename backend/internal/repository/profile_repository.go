package repository

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
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

	// UpdateSubscriptionStatus updates just the subscription status.
	//
	// Issue #1413 round-14 finding L1 (Codex review) — DO NOT call this for any transition into or
	// out of a read-only status ('inactive'/'cancelled'): unlike every other subscription_status
	// writer in this interface (UpdateSubscriptionFromPaddle, UpdateTrialStatus,
	// CancelIfPastDueWithRetention(Occurrence), ExpireTrialIfNoOverride), this method does NOT
	// maintain plaid_cleanup_pending_at at all — an active-to-inactive/cancelled call through this
	// method would leave the marker NULL while the user becomes a genuine repair candidate,
	// making them permanently invisible to GetUsersNeedingPlaidDisconnectRepair (the ONLY code path
	// that ever disconnects a read-only user's Plaid items — see that method's doc comment). As of
	// round-14, `rg` found NO production caller of this method anywhere in the codebase (only its
	// own implementation and test-mock mirrors of the interface) — it is very likely dead code, a
	// pre-#1413 relic superseded by the transition-aware writers above (in particular
	// ExpireTrialIfNoOverride, its likely historical trial-expiry successor — see
	// profile_repository_trial_expiry_integration_test.go's header comment). If you are about to
	// add a new caller, do not: use one of the transition-aware writers instead, or extend this
	// method to maintain the marker the same way they do FIRST. Recommended: delete this method
	// once confirmed permanently unused, rather than leaving a trap a future caller could silently
	// fall into.
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

	// DeleteUserCascade performs application-level cascade deletion of all user data, but ONLY
	// after re-verifying, INSIDE the same transaction that performs the destructive statements,
	// that deletion is STILL authorised at the instant of the act — not merely at the earlier
	// ClaimForDeletion instant, which the external-cleanup window (Paddle cancel, Plaid
	// disconnect — potentially many seconds of network calls) can separate from this call by a
	// meaningful amount of time (Issue #1413 round-4 Critical finding C1).
	//
	// expectedDeletionRequestedAt must be the deletion_requested_at value the caller observed
	// for this user BEFORE calling ClaimForDeletion (nil for the retention-triggered path, which
	// never sets it). Re-verifying this EXACT value (not merely whether it is nil) is required
	// to catch the case Codex round-4 review flagged as the most serious residual in this task:
	// reusing claimForDeletionPredicate alone is NOT sufficient, because that predicate treats
	// "no manual_tier_override at all" as sufficient authorization REGARDLESS of
	// deletion_requested_at — so an uncomped user who explicitly cancels their own deletion
	// request (CancelDeletion) during the external-cleanup window would otherwise still pass it
	// (cancelling sets deletion_requested_at to NULL, and "no override" was already true). Adding
	// `deletion_requested_at IS NOT DISTINCT FROM expectedDeletionRequestedAt` closes this: a
	// cancellation (non-null -> null) is a mismatch, and so is a same-instant cancel-then-new-
	// request (a new, later timestamp the original claim never verified) — both correctly abort,
	// leaving the user to be picked up by whichever flow's own schedule next applies to them.
	//
	// Also refuses to proceed while any of the user's linked_items rows still need Plaid-side
	// cleanup (status <> 'disconnected') — Issue #1413 round-4 Critical finding C2: deleting the
	// local linked_items rows (Step 3 below) while any remain un-cleaned would orphan a live,
	// still-billing Plaid access token with no local record it ever existed. See
	// plaid_item_cleanup_service.go's cleanupItems for the companion fix (re-reading the item
	// list INSIDE the side-effect claim, immediately before cleanup, instead of relying on a
	// snapshot taken outside it) that narrows how an item could reach this check un-cleaned in
	// the first place.
	//
	// Returns (true, nil) if the cascade ran and the user was deleted. Returns (false, nil) — a
	// NORMAL, EXPECTED outcome, NOT an error — when either guard fails: an admin granted/extended
	// a comp, the user cancelled their own deletion request, or un-cleaned Plaid items remain.
	// The caller MUST release its work claim (never Complete) on a (false, nil) result and let
	// the next scheduled run re-evaluate from scratch — it must NOT treat this as an error.
	DeleteUserCascade(ctx context.Context, userID string, expectedDeletionRequestedAt *time.Time) (bool, error)

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

	// GetUsersNeedingPlaidDisconnectRepair returns users who are read-only-status
	// (inactive/cancelled) but still have at least one LIVE linked item — status NOT
	// 'disconnected'/'replaced', where a NULL status counts as live too (linked_items.status is
	// nullable; see migration 000013 — SQL's three-valued logic makes a bare `NOT IN (...)`
	// silently exclude a NULL-status row, which would be exactly the leak this pass exists to
	// prevent, so the predicate explicitly treats NULL as live) — AND whose plaid_cleanup_pending_at
	// marker is older than gracePeriod.
	//
	// Issue #1413 round-11 correction: this doc comment previously (incorrectly) described
	// retention_started_at as the grace anchor. That was true only through round-9; round-10
	// replaced it with the explicit plaid_cleanup_pending_at marker (see
	// domain.User.PlaidCleanupPendingAt's doc comment for the invariant this column maintains, and
	// this method's own implementation doc comment below for the full round-10 rationale for why
	// retention_started_at could not be made sound as this anchor). retention_started_at plays NO
	// role in this query at all now — do not reintroduce it here.
	//
	// Issue #1413 round-7 (Codex review): this is the ratified DEFER design, not an ordering fix.
	// TrialService.ExpireTrial no longer calls Plaid at all — no ordering of a status flip and an
	// irreversible Plaid call can be safe, because /subscription/checkout is deliberately
	// whitelisted for read-only users, so becoming read-only never stops a user from becoming
	// active again. Disconnecting Plaid is deferred entirely to this repair pass
	// (RetentionCleanupCheckWorker.repairPlaidDisconnects), which only selects a user once
	// gracePeriod has elapsed since plaid_cleanup_pending_at was set (atomically alongside the SAME
	// status flip that makes the user Plaid-ineligible per the invariant) — giving a genuine
	// resubscriber a window to become active again before Plaid is ever touched, at which point
	// they simply drop out of this query (their marker is cleared, per the invariant, the moment
	// they become Plaid-eligible again). A user who resubscribes WITHIN the grace window is
	// therefore never disconnected at all, not merely disconnected after a narrower window.
	//
	// Because this predicate keys on subscription_status + plaid_cleanup_pending_at (not on which
	// flow set them), it also catches a failed/never-ran disconnect from the cancellation and
	// past-due paths, not just trial expiry — every SET site listed in
	// domain.User.PlaidCleanupPendingAt's doc comment stamps this marker at the same moment it
	// makes the user Plaid-ineligible, so any read-only user with a live item past the grace window
	// is covered uniformly regardless of which writer put them there.
	//
	// Predicate otherwise mirrors GetUsersNeedingRetentionClockRepair's shape (subscription_status
	// IN ('inactive', 'cancelled'), not deleted, no outstanding GDPR deletion request) with ONE
	// deliberate divergence: override exclusion uses noActiveManualOverridePredicate ("live right
	// now"), NOT noManualOverrideAtAllPredicate ("live or lapsed") — Issue #1413 round-12 Fix 2. A
	// lapsed-but-uncleared override must NOT permanently exempt a user from this repair pass the way
	// it deliberately does for retention/deletion: deletion is irreversible so that query fails safe
	// by staying excluded indefinitely if the expiry job stalls, but a Plaid disconnect is
	// RECOVERABLE (the user relinks), so the correct fail-safe direction here is the opposite — see
	// this method's implementation doc comment (profile_repository.go) for the full reasoning.
	GetUsersNeedingPlaidDisconnectRepair(ctx context.Context, gracePeriod time.Duration) ([]*domain.User, error)
}

// profileRepository implements ProfileRepository using PostgreSQL
type profileRepository struct {
	db                    DB
	enc                   *appcrypto.FieldEncryptor
	overrideTierValidator func(tierName string) bool
}

// SetOverrideTierValidator injects a function that reports whether a manual_tier_override value
// names a real, currently-configured tier. GetEffectiveSubscriptionStatusByUserID uses it to
// deny "active" for a malformed override (Issue #1413 G2) — see that method's doc comment.
//
// Setter injection (matching this codebase's existing SetAnalytics() convention — see
// CLAUDE.md's Analytics Instrumentation section) rather than a NewProfileRepository parameter:
// NewProfileRepository has ~70 call sites across this codebase's tests, and adding a required
// constructor parameter would force every one of them to thread a tier validator through even
// when the test never touches GetEffectiveSubscriptionStatusByUserID.
//
// Nil-safe: if this is never called, GetEffectiveSubscriptionStatusByUserID skips validation
// entirely rather than denying every comp — see that method's doc comment for why "no validator"
// is deliberately treated as permissive, not fail-closed.
func (r *profileRepository) SetOverrideTierValidator(validator func(tierName string) bool) {
	r.overrideTierValidator = validator
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
	       past_due_started_at,
	       plaid_cleanup_pending_at
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
		&user.PlaidCleanupPendingAt,
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
			&user.PlaidCleanupPendingAt,
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

	// CI-observed clock-skew fix: through the fix below, `updated_at` AND the
	// plaid_cleanup_pending_at COALESCE both used a single Go-computed `now := time.Now()` passed as
	// a query parameter — the ONE writer of this column that did not use a Postgres-native clock,
	// unlike every sibling writer (CancelIfPastDueWithRetention(Occurrence), ExpireTrialIfNoOverride,
	// UpdateTrialStatus below, removeUserTierOverrideUpdate). That inconsistency is exactly what
	// failed TestUpdateSubscriptionFromPaddle_Integration_GenuineNewEpisodeGetsFreshMarker in CI: the
	// test fences a "before" bound via `SELECT NOW()` (correctly, per this file's own established
	// convention) and then asserts this method's freshly-stamped marker is not before that bound — but
	// the marker's ACTUAL value came from the test RUNNER's clock (this method's Go `now`), not
	// Postgres's, so a Postgres container clock lagging the runner's could make the fenced comparison
	// fail even though nothing was actually wrong. Writing a Postgres clock function directly in the
	// SQL below, like every other writer already does, removes the runner's clock from this column's
	// value entirely — the stamped timestamp and every test fence built from `SELECT NOW()` now share
	// one clock family, always.
	//
	// Issue #1413 Finding 2 (Medium, Codex review, PR #1443 follow-up) audited this method's own
	// plaid_cleanup_pending_at COALESCE for the SAME transaction-start-vs-statement-execution risk
	// StampMissingPlaidCleanupMarkers's own doc comment describes in full: this is a single,
	// user-id-scoped autocommit UPDATE, and a row a concurrent transaction already holds locked (e.g.
	// an admin's RemoveUserTierOverride call, mid-transaction, on the SAME row) could in principle
	// make this statement block and then still write a pre-wait NOW(). At the time, this was left as
	// NOW(), NOT clock_timestamp(), deliberately: a bare NOW()->clock_timestamp() substitution in a
	// plain UPDATE does NOT actually fix this (StampMissingPlaidCleanupMarkers's doc comment records
	// the empirical proof) — PostgreSQL evaluates the SET-list target expressions against the
	// pre-lock-wait row version regardless of which clock function is named. The genuine fix requires
	// restructuring into a `WITH candidate AS (SELECT ... FOR ... UPDATE) UPDATE ... FROM candidate`
	// shape so the lock acquisition happens as its own earlier step, and was scoped out of that round
	// as a documented follow-up.
	//
	// ROUND 23 (Issue #1413 Finding 2, High, Codex review, PR #1443 follow-up) is that follow-up:
	// restructured into exactly that shape, for the SAME reason and against the SAME empirical proof
	// as StampMissingPlaidCleanupMarkers's own fix (see that method's doc comment for the full
	// mechanism). `FOR NO KEY UPDATE`, not `FOR UPDATE` — see StampMissingPlaidCleanupMarkers's own
	// Finding 1 correction for why `FOR UPDATE` here would risk the exact same cross-user deadlock
	// against a concurrent admin_user_repository.go audit-log insert (this row can equally be an
	// audit actor). The CTE selects only `id` — every SET-list expression that reads an "old" column
	// value (`subscription_status` for the eligibility check below, `plaid_cleanup_pending_at` for the
	// COALESCE) reads it via the plain, unaliased `users` table reference in the outer UPDATE, exactly
	// as StampMissingPlaidCleanupMarkers's own restructure does for `updated_at`/`plaid_cleanup_pending_at`.
	// This is deliberately NOT a theoretical claim: verified directly against a real database (not
	// reasoned about) that this target-table column reference reflects the value AS OF the moment the
	// CTE's `FOR NO KEY UPDATE` actually acquires the lock (i.e. post-wait, current committed state),
	// the same way `clock_timestamp()` does — never a value frozen at this statement's own pre-wait
	// scan. `updated_at` keeps NOW() regardless — plain ordering, never compared as an age, not at
	// risk either way.
	//
	// This method's own CAS/existence-check semantics are unchanged by the restructure: the CTE's
	// WHERE clause (`id = $N AND is_deleted = FALSE`) is EXACTLY the predicate the bare UPDATE used —
	// no condition moved, none added — so RowsAffected() still reports 0 in precisely the same
	// circumstance (missing or soft-deleted target user) this method's own ErrUserNotFound check
	// below already depends on.
	//
	// Issue #1413 round-10: maintain plaid_cleanup_pending_at atomically with THIS SAME
	// subscription_status write, rather than in a later, separate call that could independently
	// fail or drift out of sync with the status it's supposed to describe — see
	// GetUsersNeedingPlaidDisconnectRepair's doc comment for why this column, not
	// retention_started_at, is now the repair pass's sole grace-window anchor. A transition INTO
	// the repair pass's target set (inactive/cancelled) starts the marker — but only if it isn't
	// ALREADY running (COALESCE): a redelivered/duplicate webhook reporting the same terminal
	// status again must not keep pushing the anchor forward, or a steady trickle of retries could
	// indefinitely defer the grace window. A transition to any OTHER status (active, trialing —
	// and, moot in practice since processSubscriptionPastDue only ever reaches past_due from
	// active/trialing, past_due too) clears it: that status is not one this repair pass targets,
	// so nothing can legitimately be "pending" against it.
	//
	// Issue #1413 round-12 Fix 4 (Medium): the eligible branch's CASE gates the COALESCE-start on
	// the row's OLD (pre-write) subscription_status NOT already being repair-eligible
	// (`NOT (plaidDisconnectRepairEligibleStatusPredicate)`, evaluated against the row as it stood
	// before this SET clause — Postgres resolves every right-hand-side column reference in an
	// UPDATE's SET list against the pre-update row, never a sibling assignment in the same
	// statement). This distinguishes a GENUINE transition into ineligibility (old status was
	// active/trialing/past_due — the marker legitimately starts now) from a REDUNDANT terminal
	// event for a user ALREADY ineligible (old status was already inactive/cancelled — e.g. a
	// second, distinct cancellation webhook, or subscription.updated re-reporting the same terminal
	// status). The bug this closes: after repairPlaidDisconnects completes cleanup and clears the
	// marker (ClearPlaidCleanupPendingIfMatches), a LATER, DISTINCT terminal webhook for the SAME
	// still-cancelled episode used to reach the old bare `COALESCE(plaid_cleanup_pending_at, $N)` —
	// with the marker now NULL, that COALESCE fired and manufactured a BRAND NEW marker timestamped
	// AFTER the episode actually began, for cleanup that was already finished. The ELSE branch (not
	// a genuine transition) leaves plaid_cleanup_pending_at completely untouched — whatever it
	// currently is (NULL after a completed repair, or still running) is exactly what the invariant
	// says it should be, and only a real transition is licensed to change it.
	// CancelIfPastDueWithRetention/CancelIfPastDueOccurrenceWithRetention/ExpireTrialIfNoOverride
	// need no equivalent guard: their own WHERE clauses already require the OLD status to be
	// 'past_due'/'trial' (never already repair-eligible), so every successful write there is a
	// transition into ineligibility by construction.
	//
	// Issue #1413 round-13: this eligible branch NO LONGER gates on whether the row currently
	// carries a live manual tier override — round-11 Fix 4 added exactly that gate (checking
	// hasActiveManualOverrideSQLExpr live, in this same atomic UPDATE, so a comped user's
	// cancellation would leave the marker NULL instead of starting it), and round-13's
	// investigation found it was the SAME class of bug as the round-13 P0 (admin-grant nulling the
	// marker): it tied this column's SET-time value to "is there a live override RIGHT NOW" — a
	// wall-clock-dependent fact that can flip from true to false with NO writer running. If Paddle
	// cancels a user who already carries a live comp (a real path: processSubscriptionCanceled can
	// fire for a comped user), the marker stayed NULL under the old gate; if that comp then lapsed
	// with the daily comp-expiry job degraded or never run, NOTHING would ever re-stamp the
	// marker — an identical permanent, invisible leak to the admin-grant P0, just reached via a
	// different caller ordering (comp granted BEFORE the cancellation, rather than after). Per the
	// restated invariant (domain.User.PlaidCleanupPendingAt's doc comment), this column tracks only
	// the durable, override-independent fact "a raw-status ineligibility episode began, cleanup
	// outstanding" — so a genuine transition into inactive/cancelled now ALWAYS starts/preserves
	// the marker, comped or not. CURRENT eligibility (raw status AND live override) is evaluated
	// separately, entirely at query time, by GetUsersNeedingPlaidDisconnectRepair's own
	// noActiveManualOverridePredicate — which still correctly excludes a user with a currently-live
	// comp from the repair pass, marker or no marker.
	plaidMarkerEligible := domain.SubscriptionStatus(status).IsEligibleForPlaidDisconnectRepair()

	var query string
	var args []interface{}

	if subscriptionID != nil {
		// Overwrite both paddle_subscription_id columns.
		enc, err := r.enc.EncryptString(*subscriptionID)
		if err != nil {
			return fmt.Errorf("failed to encrypt paddle_subscription_id: %w", err)
		}
		blind := r.enc.BlindIndex(*subscriptionID)
		if plaidMarkerEligible {
			query = `WITH candidate AS (SELECT id FROM users WHERE id = $7 AND is_deleted = FALSE FOR NO KEY UPDATE) UPDATE users SET subscription_tier = $1, subscription_status = $2, subscription_started_at = $3, subscription_ends_at = $4, paddle_subscription_id = $5, paddle_subscription_id_blind_idx = $6, updated_at = NOW(), plaid_cleanup_pending_at = CASE WHEN NOT (` + plaidDisconnectRepairEligibleStatusPredicate + `) THEN COALESCE(plaid_cleanup_pending_at, clock_timestamp()) ELSE plaid_cleanup_pending_at END FROM candidate WHERE users.id = candidate.id`
		} else {
			query = `UPDATE users SET subscription_tier = $1, subscription_status = $2, subscription_started_at = $3, subscription_ends_at = $4, paddle_subscription_id = $5, paddle_subscription_id_blind_idx = $6, updated_at = NOW(), plaid_cleanup_pending_at = NULL WHERE id = $7 AND is_deleted = FALSE`
		}
		args = []interface{}{tier, status, startedAt, endsAt, enc, blind, userID}
	} else {
		// Preserve existing paddle_subscription_id values (omit from SET clause).
		if plaidMarkerEligible {
			query = `WITH candidate AS (SELECT id FROM users WHERE id = $5 AND is_deleted = FALSE FOR NO KEY UPDATE) UPDATE users SET subscription_tier = $1, subscription_status = $2, subscription_started_at = $3, subscription_ends_at = $4, updated_at = NOW(), plaid_cleanup_pending_at = CASE WHEN NOT (` + plaidDisconnectRepairEligibleStatusPredicate + `) THEN COALESCE(plaid_cleanup_pending_at, clock_timestamp()) ELSE plaid_cleanup_pending_at END FROM candidate WHERE users.id = candidate.id`
		} else {
			query = `UPDATE users SET subscription_tier = $1, subscription_status = $2, subscription_started_at = $3, subscription_ends_at = $4, updated_at = NOW(), plaid_cleanup_pending_at = NULL WHERE id = $5 AND is_deleted = FALSE`
		}
		args = []interface{}{tier, status, startedAt, endsAt, userID}
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

// DeleteUserCascade performs application-level cascade deletion of all user data, guarded by a
// re-verification of eligibility INSIDE this same transaction — see the interface doc comment
// for the full Issue #1413 round-4 C1/C2 rationale. This implements the 10-step deletion process
// to maintain audit trail.
func (r *profileRepository) DeleteUserCascade(ctx context.Context, userID string, expectedDeletionRequestedAt *time.Time) (bool, error) {
	// Start a transaction for atomicity
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer tx.Rollback(ctx)

	// Issue #1413 round-4 Critical finding C1: re-verify eligibility INSIDE this transaction,
	// immediately before any destructive statement — closing the window between ClaimForDeletion
	// (which the external cleanup above this call in ExecuteDeletion can separate from here by
	// many seconds) and this cascade. FOR UPDATE locks the user row so a concurrent CancelDeletion
	// or comp-grant must either commit before this SELECT runs (and be seen by it) or block until
	// this transaction ends (by which point the outcome here is already decided and committed).
	//
	// Reuses claimForDeletionPredicate UNCHANGED for the comp-vs-GDPR-request precedence rule —
	// the SAME rule ClaimForDeletion already enforces, so a user's own outstanding GDPR deletion
	// request still beats a live comp here exactly as it does there. See that predicate's own doc
	// comment; it is deliberately not "fixed" or restated here.
	//
	// ADDITIONALLY requires deletion_requested_at to be EXACTLY the value the caller observed
	// before calling ClaimForDeletion (IS NOT DISTINCT FROM handles the NULL case) — this is the
	// part claimForDeletionPredicate cannot provide by itself: that predicate treats "no
	// manual_tier_override at all" as sufficient authorization REGARDLESS of
	// deletion_requested_at, so an uncomped user who explicitly cancels their own deletion
	// request between the claim and this point would otherwise still pass it (cancelling makes
	// deletion_requested_at NULL, and "no override" was already true) — this is the C1 finding's
	// most serious case: an explicitly-cancelled deletion must not still happen.
	var stillEligible bool
	err = tx.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM users
			WHERE id = $1 AND is_deleted = FALSE
			  AND deletion_requested_at IS NOT DISTINCT FROM $2
	`+claimForDeletionPredicate+`
			FOR UPDATE
		)
	`, userID, expectedDeletionRequestedAt).Scan(&stillEligible)
	if err != nil {
		return false, fmt.Errorf("DeleteUserCascade: eligibility re-check: %w", err)
	}
	if !stillEligible {
		// Legitimate, expected abort — NOT an error. The deferred tx.Rollback(ctx) above discards
		// everything; the caller must release its work claim and let the next scheduled run
		// re-evaluate from scratch.
		return false, nil
	}

	// Issue #1413 round-4 Critical finding C2: refuse to delete the local linked_items rows (the
	// only pointer to a live Plaid access token — Step 3 below deletes them without ever calling
	// Plaid itself, by design, since that call is DeletionService's job before this method runs)
	// while ANY item still needs Plaid-side cleanup. The FOR UPDATE lock just taken above also
	// blocks a concurrent LinkAccounts INSERT for this user from committing until this
	// transaction ends (linked_items.user_id's FK check requires a FOR KEY SHARE lock on this
	// exact users row, which conflicts with FOR UPDATE) — so this check sees any item created
	// before this point that the caller's Plaid cleanup pass could not have known about. An item
	// whose INSERT commits AFTER this transaction's own commit is a residual this check cannot
	// close (see plaid_item_cleanup_service.go's re-read-inside-claim fix for why that window is
	// already as narrow as this branch makes it) — but this check must not make that window any
	// WIDER by proceeding while a KNOWN un-cleaned item still exists.
	var uncleanedItemsExist bool
	err = tx.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM linked_items WHERE user_id = $1 AND status <> 'disconnected'
		)
	`, userID).Scan(&uncleanedItemsExist)
	if err != nil {
		return false, fmt.Errorf("DeleteUserCascade: un-cleaned Plaid item check: %w", err)
	}
	if uncleanedItemsExist {
		// Legitimate, expected abort — NOT an error. Preferring to abort and retry over silently
		// orphaning a live Plaid token: an undeleted user is recoverable, an orphaned Plaid item
		// is not.
		return false, nil
	}

	now := time.Now()

	// Step 1: Delete all boosts
	_, err = tx.Exec(ctx, `DELETE FROM boosts WHERE user_id = $1`, userID)
	if err != nil {
		return false, err
	}

	// Step 2: Delete all transactions
	_, err = tx.Exec(ctx, `DELETE FROM transactions WHERE user_id = $1`, userID)
	if err != nil {
		return false, err
	}

	// Step 3: Delete all linked items (bank connections)
	// NOTE: This does NOT call Plaid /item/remove - that's handled by DeletionService
	// before calling this method to stop Plaid billing
	_, err = tx.Exec(ctx, `DELETE FROM linked_items WHERE user_id = $1`, userID)
	if err != nil {
		return false, err
	}

	// Step 4: Delete all accounts
	_, err = tx.Exec(ctx, `DELETE FROM accounts WHERE user_id = $1`, userID)
	if err != nil {
		return false, err
	}

	// Steps 5/6/8/9 (recurring_patterns, user_preferences, an audit_logs.metadata anonymization
	// UPDATE, user_sessions) were removed here as an incidental fix while adding this task's
	// C1/C2 real-DB integration coverage: none of recurring_patterns/user_preferences/
	// user_sessions exist in this schema (no migration ever creates them — the first two appear
	// to predate a later redesign, and user_sessions predates the Clerk-JWT auth model, which
	// keeps no server-side session table), and audit_logs has never had a metadata column —
	// migration 000053 converted its former JSONB `changes` column to encrypted TEXT, and no
	// migration ever added `metadata`. Every one of these four statements unconditionally
	// errored, meaning DeleteUserCascade — and therefore every real ExecuteDeletion call — has
	// never been able to run to completion against this schema. This was invisible before
	// because no real-database integration test previously exercised this method end-to-end
	// (only pgxmock, which matches query text and cannot catch a table/column that doesn't
	// exist). Out of scope for #1413's concurrency findings — properly redacting PII from the
	// now-encrypted audit_logs.changes column is a separate design decision this fix does not
	// make — but leaving these in place would have made this task's own required "ordinary
	// eligible user still deletes" regression coverage impossible to satisfy honestly. Worth its
	// own follow-up issue: audit-log PII redaction on deletion is a real GDPR gap, just not one
	// this task can respond to by inventing a redaction scheme unilaterally.

	// Step 7: Delete all notifications
	_, err = tx.Exec(ctx, `DELETE FROM notifications WHERE user_id = $1`, userID)
	if err != nil {
		return false, err
	}

	// Step 10: Mark user as deleted — encrypt the tombstone values and clear blind indexes
	deletedEmail := fmt.Sprintf("deleted_%s@deleted.local", userID)
	deletedClerkUserID := fmt.Sprintf("deleted_%s", userID)

	encryptedDeletedEmail, err := r.enc.EncryptString(deletedEmail)
	if err != nil {
		return false, fmt.Errorf("failed to encrypt deleted email: %w", err)
	}

	encryptedDeletedClerkUserID, err := r.enc.EncryptString(deletedClerkUserID)
	if err != nil {
		return false, fmt.Errorf("failed to encrypt deleted clerk_user_id: %w", err)
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
		return false, err
	}

	// Commit the transaction
	if err = tx.Commit(ctx); err != nil {
		return false, err
	}

	return true, nil
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
//
// Issue #1413 round-11 Fix 2 (High): also clears plaid_cleanup_pending_at when status makes the
// user Plaid-eligible — this method's only production caller (TrialService.GrantTrial) always
// passes SubscriptionStatusTrial, which is never repair-eligible, so this always clears in
// practice. Deriving the CASE from the `status` parameter (mirroring
// UpdateSubscriptionFromPaddle's own status-driven clearing), rather than hardcoding NULL, keeps
// this method honest against the invariant (domain.User.PlaidCleanupPendingAt's doc comment) even
// if a future caller ever passes a different status.
//
// The bug this closes: granting a trial to a cancelled user with an outstanding marker used to
// leave the STALE marker in place — trial is Plaid-eligible, so the invariant requires NULL here.
// The stale marker then survived until this NEW trial itself later expired, at which point
// ExpireTrialIfNoOverride's COALESCE(plaid_cleanup_pending_at, NOW()) preserved the OLD
// timestamp instead of starting a fresh grace window — the user could be disconnected immediately
// against an anchor from before this trial ever existed, rather than getting the grace period a
// fresh ineligibility episode is entitled to.
//
// CI-observed clock-skew fix, same defect and same fix as UpdateSubscriptionFromPaddle's own doc
// comment describes in full: `updated_at` and the plaid_cleanup_pending_at COALESCE both used to
// share a single Go-computed `now := time.Now()` query parameter instead of a Postgres-native
// clock — this method's own sibling instance of the identical bug, caught by the same audit, fixed
// the same way, for the same reason (every fence a test builds via `SELECT NOW()` must share
// Postgres's clock with whatever this method actually stamps, never the Go runner's).
//
// Issue #1413 Finding 2 (Medium, Codex review, PR #1443 follow-up) audited this method's own marker
// COALESCE for the same transaction-start-vs-statement-execution risk as every sibling writer of
// this column; a bare substitution does not actually fix anything for a single, non-CTE UPDATE
// (verified empirically, see StampMissingPlaidCleanupMarkers's doc comment), and the real fix
// (restructuring into a locked CTE) was scoped out as a documented follow-up at the time.
//
// ROUND 23 (Issue #1413 Finding 2, High, Codex review, PR #1443 follow-up) is that follow-up.
// Fixed "for consistency," per that finding: this method is latent in production (its only caller,
// TrialService.GrantTrial, always passes SubscriptionStatusTrial, which takes the ELSE/clearing
// branch below, never this COALESCE branch — see this method's own round-11 note above), but a
// future caller could reach it, and leaving one documented-but-unfixed sibling next to four other
// now-fixed ones is exactly the kind of trap this branch has already had to clean up once. Same
// shape, same FOR NO KEY UPDATE lock mode (never FOR UPDATE — see StampMissingPlaidCleanupMarkers's
// Finding 1 correction for the cross-user deadlock that mode would risk), same verified-fresh
// target-table column read for the COALESCE, as UpdateSubscriptionFromPaddle's own ROUND 23 fix —
// see that method's doc comment for the full mechanism. This method's WHERE clause (id = $5 AND
// is_deleted = FALSE) moves into the CTE unchanged, so RowsAffected() still reports 0 in exactly
// the same circumstance (missing/soft-deleted target) this method's own ErrUserNotFound check
// depends on.
func (r *profileRepository) UpdateTrialStatus(ctx context.Context, userID string, status string, trialStartedAt, trialEndsAt *time.Time, hadTrial bool) error {
	plaidMarkerEligible := domain.SubscriptionStatus(status).IsEligibleForPlaidDisconnectRepair()

	var query string
	if plaidMarkerEligible {
		query = `
			WITH candidate AS (
				SELECT id FROM users WHERE id = $5 AND is_deleted = FALSE FOR NO KEY UPDATE
			)
			UPDATE users
			SET subscription_status = $1,
			    subscription_tier = 'premium',
			    trial_started_at = $2,
			    trial_ends_at = $3,
			    had_trial = $4,
			    updated_at = NOW(),
			    plaid_cleanup_pending_at = COALESCE(plaid_cleanup_pending_at, clock_timestamp())
			FROM candidate
			WHERE users.id = candidate.id
		`
	} else {
		query = `
			UPDATE users
			SET subscription_status = $1,
			    subscription_tier = 'premium',
			    trial_started_at = $2,
			    trial_ends_at = $3,
			    had_trial = $4,
			    updated_at = NOW(),
			    plaid_cleanup_pending_at = NULL
			WHERE id = $5 AND is_deleted = FALSE
		`
	}

	ct, err := r.db.Exec(ctx, query, status, trialStartedAt, trialEndsAt, hadTrial, userID)
	if err != nil {
		return err
	}

	if ct.RowsAffected() == 0 {
		return ErrUserNotFound
	}

	return nil
}

// UpdateSubscriptionStatus updates just the subscription status for a user.
// Issue #679, #680: Trial expiration.
//
// See the ProfileRepository interface doc comment for this method — Issue #1413 round-14 finding
// L1 (Codex review): this does NOT touch plaid_cleanup_pending_at, unlike every other
// subscription_status writer in this file, and `rg` found no production caller as of round-14. Do
// not add one without first reading that warning.
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
// The override predicate is computed in Go via domain.User.EffectiveStatusWithActiveOverride
// rather than in SQL, so this row-level read shares exactly one definition of "is the override
// active" with every other caller of ComputeEffectiveStatus/ComputeEffectiveTier.
//
// Issue #1413 G2 — malformed override fails closed on THIS access path only: manual_tier_override
// is an unconstrained VARCHAR(50) with no CHECK constraint (a migration adding one would fail on
// any existing bad row, and tolerating such rows safely at read time is the whole point here), so
// its value can be garbage — a legacy row, a direct SQL write, or a write made while the tier
// config was nil (write-time validation in admin_user_service.go can be bypassed by all three).
// When an active override is in effect, its value is checked against the injected
// overrideTierValidator (see SetOverrideTierValidator) before "active" is trusted; a rejected
// override falls back to the RAW subscription_status and is logged at warn (a silent denial here
// would be indistinguishable from the original #1413 bug from the user's point of view).
//
// Deliberately NOT applied to the retention/deletion queries (noManualOverrideAtAllPredicate,
// below): "fail closed" means the OPPOSITE thing on that axis — a malformed override must still
// block deletion, not merely access, so those queries keep treating ANY non-empty override,
// valid tier name or not, as "hands off". Unifying the two would either reopen this access hole
// or start hard-deleting accounts whose only defect is a bad string in this column.
//
// If overrideTierValidator is nil (never wired via SetOverrideTierValidator), validation is
// skipped and an active override grants "active" unconditionally — the pre-G2 behavior. This is
// deliberate, not an oversight: main.go loads the tier config once at process startup and calls
// os.Exit(1) if that fails, so in production the validator is either wired for the entire process
// lifetime or the server never starts — there is no "config unavailable at read time" case to
// handle for the real deployment. Failing closed on a nil validator instead (denying every comp
// whenever some future construction path forgets to call SetOverrideTierValidator) would turn a
// wiring oversight into an instant outage for every currently-comped user, which is its own
// class of bug this fix is not trying to introduce.
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

	status, overrideValue, hasOverride := u.EffectiveStatusWithActiveOverride()
	if hasOverride && r.overrideTierValidator != nil && !r.overrideTierValidator(overrideValue) {
		slog.WarnContext(ctx, "rejecting manual tier override on access path: value does not name a configured tier",
			"user_id", userID, "manual_tier_override", overrideValue)
		return u.SubscriptionStatus, nil
	}

	return status, nil
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

// ClearRetentionDates clears all retention tracking fields on resubscription (#930). Issue #1413
// round-10: also clears plaid_cleanup_pending_at — this is one of the call sites representing
// "user becomes Plaid-eligible again", so nothing should be left pending against the retention
// clock this call is tearing down. (UpdateSubscriptionFromPaddle's own status-driven clearing
// already covers every existing caller of this method atomically with the same status write this
// method usually follows — this is deliberate belt-and-braces, not the sole mechanism: clearing to
// NULL is idempotent no matter how many paths do it.)
//
// Issue #1413 round-11 Fix 1 (BLOCKER): the plaid_cleanup_pending_at clear is now CONDITIONAL on
// the row's LIVE subscription_status at the moment of this write — reusing
// plaidDisconnectRepairEligibleStatusPredicate, the exact same "is this status Plaid-repair-
// eligible" check UpdateSubscriptionFromPaddle's own SET clause uses. Per the invariant
// (domain.User.PlaidCleanupPendingAt's doc comment), the marker must be non-NULL whenever the
// CURRENT status is inactive/cancelled — an unconditional clear here can no longer be trusted to
// mean "the user is Plaid-eligible now", because this call can arrive LATE relative to a NEWER
// transition that already re-established ineligibility with its own fresh marker.
//
// The blocker this closes: an "active" webhook updates the user, then pauses before reaching this
// call; a NEWER cancellation webhook lands first, writing subscription_status='cancelled' plus a
// FRESH marker (UpdateSubscriptionFromPaddle's own COALESCE); that cancellation's Plaid cleanup
// fails and the webhook is retried/dead-lettered; the STALE "active" webhook then resumes and
// reaches this call. Unconditionally nulling the marker here would erase the fresh cancellation's
// marker — GetUsersNeedingPlaidDisconnectRepair requires a non-NULL marker, so the repair pass
// would never see this user again, leaving them cancelled with a live, still-billing Plaid item
// indefinitely. Checking the LIVE status (not the status this stale caller last observed) closes
// this: by the time the stale call finally runs, the row's actual status is 'cancelled', so the
// conditional clear leaves the marker untouched — "a write that no longer reflects reality must
// not win".
//
// retention_started_at/retention_expires_at/the two notice-sent flags are deliberately left
// UNCONDITIONAL — out of this fix's scope. The identical race can transiently clear a fresher
// retention clock too, but that class of loss is self-healing via
// GetUsersNeedingRetentionClockRepair/RepairRetentionClock (belt-and-braces repair pass, Issue
// #1413 round-3 finding H5 residual), unlike the marker, which has NO equivalent backstop query —
// GetUsersNeedingPlaidDisconnectRepair's own predicate REQUIRES the marker to already be
// non-NULL, so a lost marker can never be repaired by re-selecting the user. That asymmetry — one
// field's loss is permanent, the other's is a same-day repair-job blip — is why only the marker
// gets a conditional guard here, not the whole statement. See the round-11 report/PR for this
// scope decision.
func (r *profileRepository) ClearRetentionDates(ctx context.Context, userID string) error {
	now := time.Now()
	ct, err := r.db.Exec(ctx, `
		UPDATE users
		SET retention_started_at = NULL,
		    retention_expires_at = NULL,
		    retention_notice_30d_sent_at = NULL,
		    retention_notice_7d_sent_at = NULL,
		    plaid_cleanup_pending_at = CASE
		        WHEN `+plaidDisconnectRepairEligibleStatusPredicate+` THEN plaid_cleanup_pending_at
		        ELSE NULL
		    END,
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

// PlaidCleanupMarkerClearMaxAttempts and PlaidCleanupMarkerClearRetryDelay bound the retry loop
// every ClearPlaidCleanupPendingIfMatches call site uses after a Plaid disconnect completes
// synchronously — Issue #1413 Finding 3 (Codex review, PR #1443 follow-up).
//
// Before this fix, the SAME two numbers (3 attempts, 50ms base delay) existed as two independent,
// hand-copied definitions: queue/jobs/retention_cleanup_check.go's own unexported
// plaidCleanupMarkerClearMaxAttempts/plaidCleanupMarkerClearRetryDelay (which subscription_webhook.go
// — same package — also references directly, so THAT pair was never actually duplicated), and a
// second, genuinely independent pair in services/past_due_service.go, whose own doc comment said so
// explicitly: "Duplicated here (not imported) rather than shared: retention_cleanup_check.go's
// constants are unexported in a different package (queue/jobs vs services)." Codex measured the
// runtime difference this queue/jobs's context-aware exponential backoff vs. past_due_service.go's
// flat time.Sleep loop produces as cosmetic (~150ms vs ~100ms total, at most a 100ms shutdown delay)
// — the actual risk Finding 3 identifies is DRIFT between two independently-maintained copies of the
// same two numbers, not the timing shape. That is the exact failure mode that produced this branch's
// main.go wiring bug (a helper corrected while a hand-written duplicate went stale), so it is worth
// closing here too, proportionately: collapse the constants to ONE literal definition, leave every
// retry LOOP's shape untouched.
//
// This lives in `repository`, not `services` or `queue/jobs`, because `services` cannot import
// `queue/jobs` without inverting the existing dependency (queue/jobs already imports services — see
// retention_cleanup_check.go's own import of this package's sibling), while both `queue/jobs` and
// `services` already import `repository` (this is the package that defines
// ClearPlaidCleanupPendingIfMatches itself, the call every one of these retry loops wraps). Both
// queue/jobs (retention_cleanup_check.go, and by extension subscription_webhook.go in the same
// package) and services (past_due_service.go) now define their own unexported, same-named constant
// as a plain alias to these exported values (`= repository.PlaidCleanupMarkerClearMaxAttempts`, etc.)
// rather than a second independent literal — so every existing internal reference and test in both
// packages keeps working unchanged, but there is now exactly ONE place either NUMBER can be written
// down, and a future edit to one can no longer silently leave the other stale.
const (
	PlaidCleanupMarkerClearMaxAttempts = 3
	PlaidCleanupMarkerClearRetryDelay  = 50 * time.Millisecond
)

// ClearPlaidCleanupPending unconditionally clears the plaid_cleanup_pending_at marker for a user —
// Issue #1413 round-10.
//
// Issue #1413 round-11 correction: this method's own prior doc comment argued unconditional
// clearing was safe because "there is no ordering in which this call can silently erase a marker a
// concurrent, still-in-flight transition is relying on" — that reasoning was WRONG, and is exactly
// the round-11 Fix 3 finding: an admin can grant a comp (clearing the marker) and then remove it
// (setting a FRESH marker for a NEW ineligibility episode) SEQUENTIALLY, entirely between a
// repairPlaidDisconnects worker's candidate-selection read and its later unconditional call to
// this method — no concurrency required, just an intervening pair of writes this method cannot
// see. Calling this unconditionally at that point erases the new episode's marker, discarding its
// entire grace window. This method is kept for callers that genuinely have no discriminator to
// compare against (there are none in this codebase as of round-11 — every production caller now
// uses ClearPlaidCleanupPendingIfMatches below) and for direct repository-level testing; DO NOT
// wire a new production caller to this method without first re-reading
// ClearPlaidCleanupPendingIfMatches's doc comment to confirm an occurrence-scoped clear isn't what
// you actually need.
func (r *profileRepository) ClearPlaidCleanupPending(ctx context.Context, userID string) error {
	now := time.Now()
	ct, err := r.db.Exec(ctx, `
		UPDATE users SET plaid_cleanup_pending_at = NULL, updated_at = $1
		WHERE id = $2`,
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

// ClearPlaidCleanupPendingIfMatches is ClearPlaidCleanupPending's occurrence-scoped sibling —
// Issue #1413 round-11 Fix 3, mirroring ClearPastDueStartedAtIfMatches's established CAS shape
// (past_due_service.go's exact solution to the same occurrence-identity problem, applied here to
// plaid_cleanup_pending_at rather than past_due_started_at).
//
// ONLY clears the marker when it still equals expected — the value the caller observed when it
// selected this user as a repair candidate (GetUsersNeedingPlaidDisconnectRepair). Without this,
// repairPlaidDisconnects's final "cleanup succeeded, clear the marker" step would call the
// unconditional ClearPlaidCleanupPending above and could erase a DIFFERENT, newer ineligibility
// episode's marker — one that started (via a comp grant-then-removal, or any other
// clear-then-reset sequence) entirely within the window between this worker's candidate selection
// and this call, with the worker never having re-verified the marker's IDENTITY, only its
// presence. See domain.User.PlaidCleanupPendingAt's doc comment for the invariant this closes:
// "its value is the instant the CURRENT ineligibility episode began — never an earlier episode's
// [and never a later one's]".
//
// Returns (true, nil) when cleared, (false, nil) when the marker had already moved on to a new
// episode since the caller's snapshot was taken (a legitimate, expected skip — the NEW episode's
// marker is exactly what must survive untouched, so a future repair run can evaluate it on its own
// schedule), and (false, ErrUserNotFound) if the user genuinely no longer exists — mirroring
// ClearPastDueStartedAtIfMatches's three-way return contract exactly.
func (r *profileRepository) ClearPlaidCleanupPendingIfMatches(ctx context.Context, userID string, expected time.Time) (bool, error) {
	var userExists, cleared bool
	err := r.db.QueryRow(ctx, `
		WITH existing AS (
			SELECT 1 FROM users WHERE id = $1
		), updated AS (
			UPDATE users SET plaid_cleanup_pending_at = NULL, updated_at = NOW()
			WHERE id = $1 AND plaid_cleanup_pending_at = $2
			RETURNING 1
		)
		SELECT EXISTS(SELECT 1 FROM existing), EXISTS(SELECT 1 FROM updated)
	`, userID, expected).Scan(&userExists, &cleared)
	if err != nil {
		return false, fmt.Errorf("ClearPlaidCleanupPendingIfMatches: %w", err)
	}
	if !userExists {
		return false, ErrUserNotFound
	}
	return cleared, nil
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

// manualTierOverrideNonEmptyClause is the bare "does this row carry ANY override value at all"
// test — Issue #1413 Finding 3 (Medium, Codex review, PR #1443 follow-up): factored out as its own
// minimal constant so GetUsersNeedingPlaidDisconnectRepair's GREATEST guard (below) can reuse it
// instead of hand-copying a third rewording of a check noActiveManualOverridePredicate and
// admin_user_repository.go's should_rearm_retention/should_mark_plaid_cleanup_pending already embed
// inline. Deliberately NOT spliced into noActiveManualOverridePredicate's own definition, and
// neither of those two existing call sites is refactored to use it — both stay byte-for-byte as
// they were; this is a NEW, single-purpose extraction for NEW code, not a retrofit of old code.
// Says nothing about expiry — an empty-string override and a NULL override are the SAME "no real
// comp" fact this predicate exists to test; manual_tier_ends_at is meaningless without a genuine,
// non-empty manual_tier_override to pair it with (see the GREATEST guard's own doc comment below).
const manualTierOverrideNonEmptyClause = `manual_tier_override IS NOT NULL AND manual_tier_override <> ''`

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

// plaidDisconnectRepairEligibleStatusPredicate is the SQL twin of
// domain.SubscriptionStatus.IsEligibleForPlaidDisconnectRepair() — Issue #1413 round-8 High 4
// (Codex review). Deliberately NARROWER than the read-only notion used by the retention-notice/
// hard-delete queries above (which also don't include past_due, but for a different family of
// predicates entirely): 'past_due' is read-only, but must never be immediately Plaid-disconnected
// by THIS repair pass — it has its own dunning timeline (past_due_service.go,
// PastDueConfig.PlaidDisconnectDays) that exists precisely to give a paying-but-behind user time
// before Plaid access is touched. This branch already has a precedent for this exact failure class
// — an override predicate existing in both SQL and Go, kept honest by a parity test
// (noActiveManualOverridePredicate / domain.User.HasActiveManualOverride(), guarded by
// TestNoActiveManualOverridePredicate_Integration_ParityWithGo) — this constant and
// TestPlaidDisconnectRepairEligibleStatus_Integration_ParityWithGo
// (profile_repository_plaid_disconnect_repair_integration_test.go) do the same here: one shared
// definition of "which statuses are eligible" instead of a third divergent copy.
const plaidDisconnectRepairEligibleStatusPredicate = `subscription_status IN ('inactive', 'cancelled')`

// liveLinkedItemExistsClause is the EXISTS predicate for "this user has at least one still-live
// linked item" — not 'disconnected' or 'replaced', with a NULL status explicitly treated as live
// too (round-7 fix; see GetUsersNeedingPlaidDisconnectRepair's doc comment below for the
// NULL-status rationale this clause must preserve). Correlates against the unaliased `users` table
// both of its call sites SELECT FROM — GetUsersNeedingPlaidDisconnectRepair just below, and
// admin_user_repository.go's removeUserTierOverrideSelectPrior — so both must keep using this SAME
// Go constant, not a second hand-copied SQL literal, so "live" cannot silently drift apart between
// them. A second, independently-worded copy is exactly how Issue #1413 round-14 finding M1 slipped
// in: removeUserTierOverrideSelectPrior's should_mark_plaid_cleanup_pending stamped the marker for
// ANY prior override on an inactive/cancelled user, with no check on whether a live item actually
// remained — re-arming a phantom marker for an episode whose cleanup had already fully completed.
const liveLinkedItemExistsClause = `EXISTS (
	SELECT 1 FROM linked_items
	WHERE linked_items.user_id = users.id
	  AND (linked_items.status IS NULL OR linked_items.status NOT IN ('disconnected', 'replaced'))
)`

// GetUsersNeedingPlaidDisconnectRepair implements the Issue #1413 round-7 grace-window repair
// path — see the interface doc comment for the full rationale. Mirrors
// GetUsersNeedingRetentionClockRepair's predicate shape, plus:
//   - a grace-window check on plaid_cleanup_pending_at (Issue #1413 round-10) — the ONLY anchor
//     this query uses. Rounds 8 and 9 demonstrated retention_started_at cannot be made sound as
//     this grace-window anchor: it is written by several independent paths, its clearing on
//     resubscribe can fail and be swallowed by a logging-only caller, and it is never cleared once
//     a disconnect actually completes (it has to keep counting toward the separate 6-month
//     retention/deletion clock regardless of Plaid state) — so a value written by a PRIOR cycle
//     can silently survive a resubscribe-then-recancel and be mistaken for the CURRENT cycle's
//     age. plaid_cleanup_pending_at cannot suffer this: it means exactly "a raw-status
//     ineligibility episode began at this instant, cleanup outstanding" — a durable,
//     override-independent fact (see domain.User.PlaidCleanupPendingAt's doc comment) — and is
//     NULL whenever nothing is pending (see ExpireTrialIfNoOverride/
//     CancelIfPastDueWithRetention/CancelIfPastDueOccurrenceWithRetention/UpdateSubscriptionFromPaddle/
//     UpdateTrialStatus for where it's set/cleared). A NULL marker is explicitly excluded — this pass must never
//     invent work from a derived signal.
//   - an EXISTS check for at least one linked item that is still "live" — not 'disconnected' or
//     'replaced', the same definition LinkedItemRepository.GetActiveItemsByUserID uses, EXCEPT a
//     NULL status is explicitly treated as live too (round-7 fix): a bare `status NOT IN (...)`
//     evaluates to SQL's UNKNOWN (not TRUE) for a NULL status, silently excluding that row — since
//     linked_items.status is nullable (migration 000013) and defaults to 'active' only for rows
//     inserted after that default was added, a live item with a NULL status would otherwise never
//     be repaired, precisely the leak this pass exists to prevent.
//
// Issue #1413 round-12 Fix 2: uses noActiveManualOverridePredicate ("live right now"), NOT
// noManualOverrideAtAllPredicate ("not even a lapsed one") — deliberately the OPPOSITE choice from
// every retention/deletion query above. Those queries must fail safe toward NEVER hard-deleting a
// user whose comp merely lapsed uncleaned (deletion is irreversible), so they exclude on ANY
// override, live or lapsed, and stay excluded until manual_tier_override_expiry_check actually
// clears the column — an unbounded delay is the acceptable failure mode there. A Plaid disconnect
// is different in kind: it is RECOVERABLE (the user relinks), so the fail-safe direction here is
// the other way — a lapsed-but-uncleared override must not permanently exempt a user from this
// repair pass, or a stalled/dead-lettered expiry job would leave their bank connection live and
// billable indefinitely.
//
// Issue #1413 round-13/14: this predicate is the ONLY place CURRENT override state (live vs.
// lapsed vs. absent, a WALL-CLOCK fact) enters this query's reasoning — per the restated invariant
// (domain.User.PlaidCleanupPendingAt's doc comment), no SET/CLEAR site conditions the marker's
// VALUE on whether an override is presently active, which is what round-13 actually fixed (round-11
// through round-12 had tied the marker to "is there a live override right now", requiring a LATER
// writer to catch the instant it lapsed — see that doc comment for the full round-13 root cause).
//
// Round-14 correction (Issue #1413 L4, Codex review): this comment previously claimed NO SET/CLEAR
// site references manual_tier_override "in any form any more" — false. AdminUserRepository's
// removeUserTierOverrideUpdate (admin_user_repository.go, shared by RemoveUserTierOverride/
// RemoveUserTierOverrideIfMatches) still reads manual_tier_override, as a write-time "was a comp
// present at all, and is it being removed right now" gate for its should_mark_plaid_cleanup_pending
// backstop CASE (further narrowed, as of round-14 finding M1, by liveLinkedItemExistsClause — see
// that CASE's own doc comment). That is a different thing from what round-13 closed: it is not a
// live-vs-lapsed distinction, and it does not require any later writer to keep it correct — the CASE
// only fires exactly once, synchronously, in the same statement that clears the override column.
//
// CURRENT eligibility — "should this user actually be disconnected right now" — is entirely this
// query's job: plaidDisconnectRepairEligibleStatusPredicate plus this
// noActiveManualOverridePredicate, evaluated fresh on every run against live, current row state.
// That split (a durable write-time fact plus a live read-time predicate) is precisely what closes
// the round-13 P0: a comp granted over an already-marked user survives the grant (the marker is
// untouched), and once that comp lapses — with or without the daily expiry job ever running — this
// predicate simply stops excluding the row on the very next scheduled run. Do NOT "unify" this with
// noManualOverrideAtAllPredicate — see TestRetentionQueries_Integration_MalformedOverride_StillExcluded
// and this predicate's own doc comment for why the retention/deletion queries must stay as they are.
//
// Issue #1413 Finding 1 (Medium, Codex review, PR #1443 follow-up, headline finding of this round):
// the split above (a durable write-time marker plus a live read-time override predicate) closed
// round-13's P0 for CURRENT ELIGIBILITY, but left a second, narrower gap in the GRACE WINDOW itself.
// A comp SUSPENDS eligibility while live, but plaid_cleanup_pending_at keeps aging regardless — round-13
// deliberately left it untouched by a grant, so it can already be arbitrarily old by the time a LATER
// comp lapses. No writer runs when a comp simply lapses on its own clock (noActiveManualOverridePredicate
// is a read-time fact, not a write), so round-10's removal-path anchor
// (removeUserTierOverrideUpdate, admin_user_repository.go, ROUND 19/20) never reaches this: it only
// re-anchors the marker when someone actually calls RemoveUserTierOverride/RemoveUserTierOverrideIfMatches.
// Concretely: cancellation stamps the marker at T0; an admin comps the user (marker untouched, per
// round-13); the comp lapses at T_lapse with NOTHING written; before the expiry job ever runs, this
// query would see plaid_cleanup_pending_at = T0 — possibly already older than gracePeriod — and select
// the user for disconnect, even though they have been read-only (post-lapse) for far less than
// gracePeriod. The user is disconnected almost the instant they actually lose access, having been
// granted almost none of their entitled grace window.
//
// The fix: anchor the grace comparison on
// GREATEST(plaid_cleanup_pending_at, CASE WHEN manualTierOverrideNonEmptyClause THEN
// manual_tier_ends_at ELSE NULL END), not plaid_cleanup_pending_at alone — the SAME rule
// removeUserTierOverrideUpdate's own lapse-anchor CASE already expresses on the writer side (ROUND
// 19 there: anchor on manual_tier_ends_at when it is the more recent loss-of-access instant, never
// NOW()/"today" for a comp that lapsed long ago). Both sites now say "grace runs from when access
// was actually lost," from two different angles — a query predicate here, a write-time anchor
// there — and are cross-referenced so they cannot drift apart; if this GREATEST expression ever
// changes, re-check removeUserTierOverrideUpdate's CASE (and vice versa) for whether the underlying
// rule they're both expressing has actually changed too.
//
// NULL semantics, stated once, precisely, for every GREATEST-related case below: PostgreSQL's
// GREATEST() ignores NULL arguments and returns the greatest of the remaining non-NULL ones,
// returning NULL only when EVERY argument is NULL (this is NOT the same rule as `+` or plain
// comparison operators, which propagate a single NULL to the whole expression) — verified directly
// against Postgres (not merely reasoned about; see TestPlaidDisconnectRepair_Integration_* below).
// Every case that follows reduces to an instance of this one rule.
//
// Issue #1413 Finding 3 (Medium, Codex review, PR #1443 follow-up) added the CASE guard above
// (manualTierOverrideNonEmptyClause) after finding a FIFTH, malformed row shape none of the original
// four cases covered: manual_tier_override set to the empty string, so noActiveManualOverridePredicate's
// own non-empty check does NOT exclude this row (an empty string never satisfies that inequality
// test), paired with a
// non-NULL manual_tier_ends_at (e.g. a stale value left over from a prior, unrelated override that
// was cleared incompletely, or any other reachable combination — this package's own tests
// deliberately exercise independent combinations of these two columns, and there is no DB
// constraint tying them together). Without the guard, GREATEST would still pick up that ends_at
// value even though it does not represent any real, current comp, and a value set far in the future
// would defer this user's eligibility for repair indefinitely. The guard makes manual_tier_ends_at
// participate in GREATEST ONLY when manualTierOverrideNonEmptyClause holds — i.e. only when the
// override is a real, non-empty value, mirroring noActiveManualOverridePredicate's own non-empty
// test rather than re-deriving a third copy of it (see manualTierOverrideNonEmptyClause's own doc
// comment for why that constant, not noActiveManualOverridePredicate itself, is what's reused here).
// See TestPlaidDisconnectRepair_Integration_MalformedEmptyOverrideWithFutureEndsAt_GreatestGuardIgnoresEndsAt.
//
// Five cases were reasoned through against noActiveManualOverridePredicate and the CASE guard above,
// immediately below this GREATEST clause in the query, and confirmed against a real database (not
// just reasoned about, since PostgreSQL's GREATEST/NULL interaction is exactly the kind of thing an
// error here would be silent about):
//  1. manual_tier_ends_at in the FUTURE, override non-empty (a live comp): noActiveManualOverridePredicate
//     already excludes this row outright (manual_tier_override non-empty AND ends_at >= NOW()) — the
//     row never reaches the GREATEST comparison at all, so GREATEST cannot wrongly defer anything here.
//  2. manual_tier_ends_at NULL, override non-empty (a permanent, non-expiring comp):
//     noActiveManualOverridePredicate's "ends_at IS NULL OR ends_at >= NOW()" branch treats a NULL
//     end as still-active, so this row is ALSO excluded before the GREATEST comparison runs.
//  3. manual_tier_ends_at NULL, NO override (the ordinary case — the overwhelming majority of rows):
//     not excluded by noActiveManualOverridePredicate, so THIS is the case where a silent GREATEST/NULL
//     regression would be catastrophic — see the NULL-semantics paragraph above: `GREATEST(x, NULL) = x`,
//     so every ordinary user's comparison correctly reduces to plaid_cleanup_pending_at alone (the whole
//     marker being NULL would already have failed the earlier `plaid_cleanup_pending_at IS NOT NULL`
//     clause). Every pre-existing test in this file that has no override at all (the large majority)
//     is a live regression check for this case on every run.
//  4. A lapsed comp OLDER than the marker, override non-empty (manual_tier_ends_at predates
//     plaid_cleanup_pending_at — e.g. an old, never-cleared comp from a PRIOR, unrelated episode):
//     the CASE guard passes ends_at through (override is non-empty), and GREATEST picks the marker
//     (the later of the two), so this must, and does, behave identically to the pre-Finding-3,
//     marker-only comparison — see TestPlaidDisconnectRepair_Integration_LapsedCompOlderThanMarker_UnaffectedByGreatest
//     (profile_repository_plaid_disconnect_repair_integration_test.go).
//  5. manual_tier_override set to the empty string, manual_tier_ends_at set to ANY value (the
//     Finding 3 malformed shape): the CASE guard's manualTierOverrideNonEmptyClause test is false
//     (an empty string never satisfies the non-empty inequality test), so manual_tier_ends_at is
//     passed to GREATEST as NULL regardless of what it actually
//     contains — GREATEST reduces to plaid_cleanup_pending_at alone, per the SAME NULL-semantics rule
//     as case 3 above. See TestPlaidDisconnectRepair_Integration_MalformedEmptyOverrideWithFutureEndsAt_GreatestGuardIgnoresEndsAt.
//
// The bug ROUND 22 closed is a sixth, implicit case none of the above: manual_tier_ends_at in the
// past AND more recent than the marker, override non-empty — see
// TestPlaidDisconnectRepair_Integration_LapsedCompRestartsGraceFromLapseNotFromMarker for that
// scenario reproduced end to end through production writers.
//
// GetOutstandingPlaidCleanupStats (job_health_repository.go), the sibling backlog gauge, deliberately
// does NOT get this same GREATEST treatment — it measures "how much cleanup is outstanding and how
// long has it been outstanding", not "who is past grace right now", and its own doc comment already
// states it must keep counting a comped user's backlog for the ENTIRE time a comp is live (a gauge
// that went blind to that population would hide a chronic problem for as long as the comp lasts).
// Anchoring it on GREATEST(..., manual_tier_ends_at) would UNDERSTATE that backlog's age for exactly
// the population the gauge exists to keep visible, so the two are deliberately NOT aligned on this
// axis — see that method's own doc comment for the full reasoning.
//
// CI-observed clock-skew fix (fourth failure of this exact class on this branch): through the fix
// below, this method computed its grace-window cutoff in GO — `cutoff := time.Now().Add(-gracePeriod)`
// — and compared it against plaid_cleanup_pending_at, a column every writer stamps from POSTGRES's
// OWN clock (`NOW()`, or — for removeUserTierOverrideUpdate's still-live branch and
// StampMissingPlaidCleanupMarkers's global-scan heal, as of Issue #1413 Finding 2, Codex review, PR
// #1443 follow-up — `clock_timestamp()` instead, for the subset of writers actually exposed to a
// concurrent-lock-wait backdating risk in a way a bare NOW()->clock_timestamp() substitution could
// genuinely close; see StampMissingPlaidCleanupMarkers's own doc comment for why that fix needed
// more than a function-name swap, and for which OTHER writers of this column were audited and
// deliberately left on NOW() pending a larger follow-up). CI runs Postgres in a separate container
// whose clock can lag or lead the test runner's by
// single-digit milliseconds; mixing the two clocks in one comparison meant every test exercising a
// near-zero grace boundary inherited that skew, no matter how carefully the TEST fenced its own
// assertions — the mixing was in this production query, not in any test. Two tests failed in CI on
// this exact defect: TestMigration000121_LapsedOverrideRowNowRepaired (calls this method with
// gracePeriod=0 immediately after a migration stamps the column via Postgres's own NOW(); a lagging
// Go clock could put `cutoff` before the stamped value, so `plaid_cleanup_pending_at < cutoff` was
// false) and TestUpdateSubscriptionFromPaddle_Integration_GenuineNewEpisodeGetsFreshMarker (a
// DIFFERENT manifestation of the same root cause — see UpdateSubscriptionFromPaddle's own doc
// comment).
//
// The fix moves the ENTIRE comparison into Postgres: `clock_timestamp() - make_interval(secs => $1)`
// computes the cutoff using a Postgres-native clock, so the two sides are never subject to
// inter-process/inter-container skew, no matter how tight the grace period. (Issue #1413 Finding 2:
// this comparison itself was `NOW()`-based from round-10 through round-20 — switched to
// clock_timestamp() alongside the GREATEST fix above, both landing in the same round; see
// StampMissingPlaidCleanupMarkers's doc comment for why a plain autocommit SELECT does not strictly
// NEED clock_timestamp() the way a lock-waiting UPDATE does, and why using it here anyway is still
// the right call — consistency with every write-side anchor it is compared against, not a
// correctness requirement of this particular read.) The exported signature (gracePeriod
// time.Duration) is unchanged — callers are unaffected; only the number that crosses the wire
// changes, from a pre-computed absolute timestamp to a plain float64 of seconds, which
// make_interval's `secs double precision` parameter accepts directly (mirrors
// removeUserTierOverrideUpdate's own established make_interval(months => $N) precedent,
// admin_user_repository.go). Sub-second grace periods (several tests here call this with exactly
// zero) lose no precision — Duration.Seconds() is a float64, and 0 maps to 0 exactly.
func (r *profileRepository) GetUsersNeedingPlaidDisconnectRepair(ctx context.Context, gracePeriod time.Duration) ([]*domain.User, error) {
	query := userSelectQuery + `
		WHERE ` + plaidDisconnectRepairEligibleStatusPredicate + `
		  AND is_deleted = FALSE
		  AND deletion_requested_at IS NULL
		  AND plaid_cleanup_pending_at IS NOT NULL
		  AND GREATEST(
		        plaid_cleanup_pending_at,
		        CASE WHEN ` + manualTierOverrideNonEmptyClause + ` THEN manual_tier_ends_at ELSE NULL END
		      ) < clock_timestamp() - make_interval(secs => $1)
		  AND ` + liveLinkedItemExistsClause + `
	` + noActiveManualOverridePredicate + `
		ORDER BY updated_at ASC
	`
	users, err := r.queryUsers(ctx, query, gracePeriod.Seconds())
	if err != nil {
		return nil, err
	}
	if users == nil {
		return []*domain.User{}, nil
	}
	return users, nil
}

// StampMissingPlaidCleanupMarkers is Issue #1413's self-healing backstop for
// plaid_cleanup_pending_at — round 16, added after five prior review rounds each closed one way the
// marker could end up NULL while a user still has a live linked item, and opened another, because
// every attempt tried to make some WRITER maintain the marker correctly. Writers cannot: they race
// with the linked_items lifecycle they are trying to summarise (see removeUserTierOverrideSelectPrior's
// doc comment, admin_user_repository.go, and domain.User.PlaidCleanupPendingAt's doc comment, for the
// two concrete races round 16 stops trying to close by writing more carefully — a re-fetch that just
// moves its own window, and a CTE snapshot that cannot serialize against an independent, autocommit
// INSERT).
//
// This stops fixing writers and instead heals: run once at the start of EVERY repairPlaidDisconnects
// pass (retention_cleanup_check.go), before the candidate query, this is a single atomic UPDATE that
// stamps clock_timestamp() for any user who is ineligible-status, has at least one live linked item,
// and has a NULL marker. A writer that misses a stamp — for any reason, present or future, including
// a race no one has found yet — self-heals on the very next repair pass instead of leaking the
// user's bank connection forever.
//
// Issue #1413 Finding 2 (Medium, Codex review, PR #1443 follow-up): this used to write NOW()
// (transaction_timestamp()), on the argument that a single autocommit UPDATE has no EARLIER
// lock-acquiring statement in the same (implicit) transaction, so there was supposedly no wait for
// NOW() to be staled across. That argument is wrong, and the correction matters beyond this one
// call site: NOW()/transaction_timestamp() is fixed at the transaction's BEGIN, which for a bare
// autocommit statement is essentially "when the statement was submitted" — BEFORE Postgres acquires
// the row locks needed to actually perform the UPDATE, not after. If this statement's own WHERE
// clause matches a row a CONCURRENT transaction already holds locked (e.g. removeUserTierOverrideUpdate's
// FOR UPDATE, admin_user_repository.go, mid-removal on the very same row), THIS statement blocks
// waiting for that lock — and once unblocked, NOW() still reflects the pre-wait instant, not the
// moment the row was actually written. Concretely: this backstop starts at T0, blocks behind a
// concurrent removal holding the row for (say) 73 hours, then finally writes plaid_cleanup_pending_at
// = NOW() = T0 — the very next repair pass sees a marker already 73 hours stale and disconnects with
// no meaningful grace at all. The fix: clock_timestamp() — real statement-execution wall time,
// re-evaluated fresh when the statement actually runs, not when its enclosing (here: implicit,
// one-statement) transaction began. This is the SAME fix, and the SAME corrected reasoning, as
// removeUserTierOverrideUpdate's own plaid_cleanup_pending_at CASE (ROUND 20, admin_user_repository.go)
// — the "autocommit statements are exempt" argument that used to distinguish this call site from
// that one was the error; there is no such exemption for ANY statement that can block on a lock,
// autocommit or not.
//
// Why this shape, against the four properties that matter here:
//
//  1. GetUsersNeedingPlaidDisconnectRepair is left COMPLETELY UNCHANGED. This preserves
//     TestPlaidDisconnectRepair_Integration_ExcludesUserWithNoLiveItems outright: the heal requires a
//     live item — the SAME liveLinkedItemExistsClause the candidate query already requires — so a
//     user with no live items is never stamped and never selected. No pinned test needs
//     reinterpreting, and which users the candidate query ACTS on never changes.
//  2. It is ONE atomic statement, so it cannot itself race: a concurrent linked_items INSERT either
//     commits before this UPDATE's own snapshot (and that row is healed right now) or after it (and
//     it is healed on the NEXT run instead) — there is no window in between where running this
//     statement can itself observe or produce a torn, half-updated result.
//  3. The existing grace-window logic is untouched: a healed user's plaidDisconnectRepairGracePeriod
//     runs from the heal (this statement's own clock_timestamp() — see the Finding 2 correction
//     below for why NOW() would have been wrong here), exactly like every other SET site.
//  4. Reuses the SAME shared Go constants plaidDisconnectRepairEligibleStatusPredicate and
//     liveLinkedItemExistsClause the candidate query, the backlog gauge (GetOutstandingPlaidCleanupStats,
//     job_health_repository.go), and migration 000121 all already use — copied verbatim there since a
//     migration cannot import Go code — instead of a fourth hand-copied restatement of either, so the
//     heal, the candidate query, the backlog gauge, and the migration cannot silently drift apart on
//     "which statuses are eligible" or "what counts as a live item". noActiveManualOverridePredicate
//     is also reused from the SAME Go constant the candidate query uses, but is NOT part of migration
//     000121's WHERE clause — that migration deliberately omits any override filter at all (a
//     one-time historical backfill has no "next run" to rely on, so it stamps every eligible row
//     regardless of comp state); see the override paragraph below for why this backstop, which DOES
//     have a next run, can safely defer a currently-comped user to it instead.
//
// The consequence, stated plainly rather than left implicit: a marker cleared too early (or never
// stamped in the first place) does NOT get its ORIGINAL episode's grace window resumed — it gets a
// FRESH plaidDisconnectRepairGracePeriod starting from whenever this backstop next runs and notices
// the gap. In the worst case (the gap opens moments after this pass just ran), that is up to one
// ADDITIONAL full grace window of delay before the user is actually disconnected. That is strictly
// better than the alternative it replaces — never, permanently — and it is the accepted price of
// healing by re-detection rather than trying to preserve a timestamp a writer never got the chance to
// record correctly in the first place.
//
// Includes noActiveManualOverridePredicate deliberately: this mirrors GetUsersNeedingPlaidDisconnectRepair's
// own predicate set exactly (modulo the marker/grace-window gates this backstop's whole job is to
// populate), so it only ever creates a marker for a population the candidate query would ITSELF
// eventually act on — never for a currently-comped user the candidate query would never touch
// regardless of marker state. A currently-comped user's marker is instead re-armed the ordinary way
// once the comp is actually removed (removeUserTierOverrideUpdate's plaid_cleanup_pending_at CASE —
// ROUND 18/19/20, admin_user_repository.go; NOT a COALESCE, which this CASE stopped using at ROUND
// 18 precisely because a COALESCE-preserved stale value is what Finding 1, above, closes) or, if the
// comp lapses and is never explicitly removed, by the NEXT run of this same backstop once
// noActiveManualOverridePredicate stops excluding them.
//
// Called from exactly one place: RetentionCleanupCheckWorker.repairPlaidDisconnects
// (retention_cleanup_check.go), at the very start of every pass — so a user healed in THIS pass can
// also be selected for repair in the SAME pass if they happen to already be past the grace window.
//
// CAUTION — this is a global-scan WRITE, unlike every other method in this file: its WHERE clause
// carries no user-id parameter at all. Unlike a global-scan READ (which can only produce a confusing
// false test result), this can actually MUTATE another test's fixture row if that row happens to
// satisfy this predicate at the moment this runs — see this package's own integration tests for this
// method, which give every fixture a unique, immediately-`t.Cleanup`'d row for exactly this reason.
// This blast radius is intentional and load-bearing for production correctness (the whole point is to
// catch a row NO scoped query would ever be told to look at) — but it means this method must never be
// invoked from a shared test-setup helper unrelated tests also use, only from a fixture the calling
// test fully owns end-to-end.
//
// Deliberately NOT part of the exported ProfileRepository interface above: this is needed only by
// RetentionCleanupCheckWorker's own narrow RetentionProfileRepository consumer interface
// (retention_cleanup_check.go), not by the broad canonical interface every OTHER service's test
// double also implements in full — adding it there would force a new stub method onto every one of
// those unrelated mocks for a capability only one caller needs. Mirrors this file's existing
// precedent for ClearPastDueStartedAtIfMatches, CancelIfPastDueOccurrenceWithRetention, and the two
// occurrence-scoped retention-notice-eligibility methods (see NewProfileRepository's own doc comment).
func (r *profileRepository) StampMissingPlaidCleanupMarkers(ctx context.Context) (int, error) {
	// ROUND 21 correction (Issue #1413 Finding 2, Codex review, PR #1443 follow-up): a bare
	// `UPDATE ... SET plaid_cleanup_pending_at = clock_timestamp() WHERE ...` is NOT enough, and this
	// was verified empirically (against a real database, not reasoned about) while building this
	// method's own regression test. PostgreSQL evaluates an UPDATE's SET-list target expressions —
	// INCLUDING a volatile call like clock_timestamp() — against the row version obtained by the
	// scan BEFORE any row-lock wait; if a candidate row is concurrently locked by another
	// transaction, this statement blocks, but once unblocked it does NOT re-evaluate clock_timestamp()
	// against the moment the lock was actually released — the value already computed pre-wait is what
	// gets written. A bare substitution of NOW() for clock_timestamp() therefore leaves the EXACT same
	// backdating bug this fix exists to close, just with a different function name.
	//
	// The fix that actually works, mirroring removeUserTierOverrideUpdate's own already-correct shape
	// (admin_user_repository.go): acquire the row locks EXPLICITLY, as their own separate step, via a
	// `SELECT ... FOR NO KEY UPDATE` CTE — THAT step is what genuinely blocks and waits. Only once it
	// resolves (lock already held by this transaction) does the outer UPDATE run, evaluating
	// clock_timestamp() fresh, with nothing left to wait for — so it correctly reflects the
	// post-wait, statement-execution instant. This is still ONE round trip (one r.db.Exec call, one
	// implicit autocommit transaction) — the CTE only changes the ORDER PostgreSQL performs the lock
	// acquisition and the target-list evaluation in, not the transaction shape.
	//
	// ROUND 23 correction (Issue #1413 Finding 1, High, Codex review, PR #1443 follow-up): ROUND 21
	// used `FOR UPDATE` for the candidate lock. The lock MODE, not just the restructuring, was wrong,
	// and produces a real, reproducible cross-user deadlock against admin_user_repository.go's comp
	// removal path. Concrete cycle:
	//
	//  1. User B is ineligible-status with a live linked item and a NULL marker — an ordinary Stamp
	//     candidate — so this CTE's (old) `FOR UPDATE` locks B.
	//  2. This statement's candidate scan (with multiple qualifying rows and, pre-fix, no defined lock
	//     order — see the ORDER BY correction below) reaches user A, whose row is already locked by a
	//     concurrent admin_user_repository.go RemoveUserTierOverride/RemoveUserTierOverrideIfMatches
	//     call (its own "prior" CTE's `FOR UPDATE`). This statement blocks, waiting for A.
	//  3. That removal transaction, still holding A, proceeds to insert its audit_logs row — and the
	//     acting admin recorded as that row's actor happens to BE user B (audit_logs.user_id has no
	//     "must have an active subscription" constraint — an admin's own account can independently be
	//     inactive/cancelled with a live linked item, which is exactly how B qualified as a Stamp
	//     candidate at all; see the note at the end of this comment for why that is a separate,
	//     intentional contract, not something to "fix" here).
	//  4. audit_logs.user_id has a foreign key to users.id. Inserting a row that references B triggers
	//     Postgres's referential-integrity check, which requests a `FOR KEY SHARE` lock on B — the
	//     weakest lock mode, meant to coexist with everything except a lock that could change B's key.
	//  5. `FOR UPDATE` (this statement's OLD lock mode on B, step 1) conflicts with `FOR KEY SHARE`
	//     (step 4's request) — PostgreSQL's conflict table has NO row/column pair where these coexist.
	//     Now: this statement holds B and waits on A (step 2); the removal holds A and waits on B (step
	//     4). Classic deadlock cycle. PostgreSQL's deadlock detector eventually aborts one side with a
	//     serialization failure — in a billing-adjacent path, an aborted transaction is not a
	//     theoretical concern.
	//
	// The fix: `FOR NO KEY UPDATE` instead of `FOR UPDATE` for this statement's candidate lock. It is
	// the semantically correct mode regardless of the deadlock — this statement changes NO key column
	// (only plaid_cleanup_pending_at and updated_at), so `FOR UPDATE`'s key-change guarantee was never
	// needed here. And per PostgreSQL's row-lock conflict table, `FOR NO KEY UPDATE` does NOT conflict
	// with `FOR KEY SHARE` — so with this fix, step 5 above never blocks: the removal's audit insert on
	// B proceeds immediately even while this statement still holds B, breaking the cycle at its root
	// rather than papering over one specific interleaving of it. Every CTE added for Issue #1413
	// Finding 2 (the five sibling COALESCE(plaid_cleanup_pending_at, ...) writers restructured in this
	// same round — UpdateSubscriptionFromPaddle, UpdateTrialStatus,
	// CancelIfPastDueWithRetention(Occurrence), ExpireTrialIfNoOverride) uses `FOR NO KEY UPDATE` for
	// this identical reason: any of those five rows could equally be an audit actor, so `FOR UPDATE`
	// there would reproduce this exact cycle five more times over.
	//
	// Verified via a real database (internal/repository/profile_repository_plaid_disconnect_repair_integration_test.go,
	// TestStampMissingPlaidCleanupMarkers_Integration_DeadlockCycleWithConcurrentAuditActor and its
	// sibling doc comment) that this statement's `FOR NO KEY UPDATE` does not conflict with a
	// concurrent `FOR KEY SHARE` request on the same row, closing the cycle above at its root.
	//
	// Note on step 3 above: this repository does not, and must not, require an audit_logs actor to
	// have an active subscription — that would make routine admin actions fail based on the ADMIN's
	// OWN unrelated billing state, which is not a real precondition of being allowed to act as an
	// admin. The reachability of "the acting admin's own account is itself a Stamp candidate" is a
	// consequence of that (correct, unrelated) design choice, not a defect to close here.
	//
	// ROUND 23 (Issue #1413 Finding 1, High, Codex review, PR #1443 follow-up) also adds `ORDER BY id`
	// to this CTE's candidate scan. Without it, when more than one row qualifies, PostgreSQL's lock
	// acquisition order for a multi-row `FOR ... UPDATE`/`FOR NO KEY UPDATE` scan is whatever order the
	// underlying scan happens to produce (unspecified, and can differ run to run, e.g. under a
	// different query plan or after a VACUUM). Two concurrent Stamp passes could then lock the SAME two
	// qualifying rows in OPPOSITE orders — pass 1 locks row X then waits on row Y, while pass 2 locks Y
	// then waits on X — an ordinary lock-ordering deadlock between two instances of the SAME statement,
	// independent of the cross-repository cycle above. `ORDER BY id` gives every invocation of this
	// statement the same total lock order (ascending id), so two concurrent passes can only ever
	// contend for the SAME next row, never deadlock against each other waiting in opposite directions.
	//
	// Blocking (this statement's own design) vs. `SKIP LOCKED`: deliberately blocking, not skipping.
	// A locked row this pass skips would still self-heal on the NEXT scheduled run (per this method's
	// own "up to one additional grace window of delay" consequence, discussed below) — so skipping
	// would not be UNSAFE. But the realistic contention this statement meets is another SHORT-LIVED
	// transaction (an admin comp removal, a single-row CAS writer) that releases its lock in
	// milliseconds to at most a few seconds, not a chronic, indefinitely-held lock. Blocking lets THIS
	// pass finish healing that row once the brief contention clears, rather than unconditionally
	// deferring it to tomorrow's run for no reason — maximizing how much this pass actually heals, at
	// the cost of an occasional short wait, now that ROUND 23's `FOR NO KEY UPDATE` + `ORDER BY id`
	// have closed the two ways that wait could turn into an unbounded deadlock instead of an ordinary,
	// short block. `SKIP LOCKED` would only be preferable if a row could plausibly stay locked for the
	// ENTIRE remainder of a run (e.g. some other pathological long-held transaction) — that would be a
	// bug in whatever holds the lock that long, not something this statement should paper over by
	// silently skipping the row it exists specifically to catch.
	ct, err := r.db.Exec(ctx, `
		WITH candidates AS (
			SELECT id FROM users
			WHERE `+plaidDisconnectRepairEligibleStatusPredicate+`
			  AND plaid_cleanup_pending_at IS NULL
			  AND is_deleted = FALSE
			  AND deletion_requested_at IS NULL
			  AND `+liveLinkedItemExistsClause+`
			`+noActiveManualOverridePredicate+`
			ORDER BY id
			FOR NO KEY UPDATE
		)
		UPDATE users
		SET plaid_cleanup_pending_at = clock_timestamp(), updated_at = NOW()
		FROM candidates
		WHERE users.id = candidates.id
	`)
	if err != nil {
		return 0, fmt.Errorf("StampMissingPlaidCleanupMarkers: %w", err)
	}
	return int(ct.RowsAffected()), nil
}

// anyLinkedItemExistsClause is the EXISTS predicate for "this user has at least one linked_items
// row at all" — live or not. Deliberately separate from liveLinkedItemExistsClause (no status
// filter here): ClearCompletedPlaidCleanupMarkers below needs to distinguish "zero rows ever"
// (the #1453 allowance — a never-linked-Plaid cancellation or genuine migration residue, both
// legitimate and indistinguishable at the row level) from "rows exist, but none are live" (a real,
// completed episode whose marker should have been cleared). Correlates against the unaliased
// `users` table, matching liveLinkedItemExistsClause's own convention.
const anyLinkedItemExistsClause = `EXISTS (
	SELECT 1 FROM linked_items
	WHERE linked_items.user_id = users.id
)`

// ClearCompletedPlaidCleanupMarkers is StampMissingPlaidCleanupMarkers's symmetric counterpart —
// Issue #1413 Finding 1 (Codex review, PR #1443 follow-up, headline finding): the self-healing
// backstop round 16 added only heals ONE of the two ways plaid_cleanup_pending_at can violate
// domain.User.PlaidCleanupPendingAt's invariant. It stamps a marker that is wrongly NULL (episode
// open, nothing recorded); it does nothing for a marker wrongly left SET after its episode's
// cleanup has already completed. That second direction was, until this fix, believed harmless —
// several comments across this branch called it "inert" or "untidy residue" the candidate query's
// own live-item EXISTS clause filters out forever. That belief was WRONG, and verified wrong by a
// concrete five-step scenario:
//
//  1. repairPlaidDisconnects disconnects a user's items successfully, but all
//     plaidCleanupMarkerClearMaxAttempts marker-clear retries fail (a genuine, if rare, DB blip
//     lasting longer than the retry loop's total backoff window). The old marker timestamp T
//     survives, non-NULL, on a user with zero LIVE linked items.
//  2. That user has no live item, so GetUsersNeedingPlaidDisconnectRepair's own EXISTS clause never
//     selects them again — nothing revisits T. Without this method, T would sit there indefinitely.
//  3. An admin grants a comp. The user is effectively active again and relinks a bank — a genuinely
//     NEW live linked item now exists, but T (the OLD episode's timestamp) is still what is
//     recorded.
//  4. The admin removes the comp. AT THE TIME this scenario was written, removeUserTierOverrideUpdate's
//     plaid_cleanup_pending_at CASE used COALESCE(u.plaid_cleanup_pending_at, NOW()) (admin_user_repository.go),
//     which preserved the stale T instead of starting a fresh episode, because the column was never
//     NULL to begin with.
//  5. The next repairPlaidDisconnects pass now sees a live item and a marker (T) already older than
//     plaidDisconnectRepairGracePeriod — and disconnects the newly-linked bank IMMEDIATELY, with
//     ZERO grace period. The grace window exists precisely so a lapsed user has time to recover;
//     this path silently skips it entirely for a bank connection that is only hours old.
//
// Round-18 correction (admin_user_repository.go, removeUserTierOverrideSelectPrior's own doc
// comment): step 4's COALESCE was independently REMOVED by a later round, for a different reason
// (a stale marker resurrected across an ordinary ongoing-episode restart, no Clear-failure required
// at all) — should_mark_plaid_cleanup_pending's CASE now overwrites UNCONDITIONALLY (the
// manual_tier_ends_at anchor when lapsed, clock_timestamp() otherwise; see that file's ROUND
// 18/19/20 notes), so step 4 as literally described can no longer happen via THIS writer regardless
// of whether this method ever runs. This method is KEPT anyway, not made redundant by that fix: it
// still closes the population step 4 never depended on a comp for at all — a stale marker with zero
// live items that a comp is NEVER granted over, which sits uncorrected indefinitely with nothing
// else in this codebase ever revisiting it (see the asymmetry paragraph below, and
// removeUserTierOverrideSelectPrior's own "Reassessing ClearCompletedPlaidCleanupMarkers" note for
// the identical conclusion reached independently from the writer side).
//
// The fix restores the invariant's SECOND half at runtime, not just on paper:
// assertPlaidMarkerInvariant (profile_repository_plaid_marker_invariant_integration_test.go) has,
// since round 16, already classified "marker set, ineligible status, at least one linked_items row,
// none of them live" as a violation — nothing before this method ever enforced it. Symmetric with
// StampMissingPlaidCleanupMarkers:
//
//   - Stamp: ineligible status, live linked item, marker IS NULL -> set NOW().
//   - Clear (this method): ineligible status, marker IS NOT NULL, at least one linked_items row,
//     no LIVE linked_items row -> set NULL.
//
// The "at least one linked_items row" condition is MANDATORY, not optional — this is the #1453
// allowance, unchanged: a user with ZERO linked_items rows at all (an ordinary never-linked-Plaid
// cancellation, or genuine 000119/000120 migration residue — indistinguishable at the row level,
// both legitimate, both reachable) keeps whatever marker they have. Clearing those would silently
// change behavior for a population this branch has deliberately, repeatedly left alone (see
// anyLinkedItemExistsClause's own doc comment). This is why the WHERE clause below uses
// anyLinkedItemExistsClause AND NOT liveLinkedItemExistsClause, rather than a single hand-derived
// "COUNT(live) = 0" expression that could not tell the two zero-linked-item populations apart.
//
// Deliberately does NOT include noActiveManualOverridePredicate, unlike StampMissingPlaidCleanupMarkers
// above — this is the one place this method is NOT a mirror of its sibling, and the asymmetry is
// load-bearing, not an oversight. Stamp excludes a currently-comped user because stamping only ever
// arms a marker for a population the candidate query would itself eventually act on, and the
// candidate query already excludes a live comp — deferring to "re-arm the ordinary way once the comp
// is removed" is correct there because the column starts from NULL either way. Clear has the
// opposite obligation: it corrects a FACT already true about linked_items (cleanup for this episode
// is done) regardless of whether an override happens to be live right now — see
// domain.User.PlaidCleanupPendingAt's doc comment: manual_tier_override is deliberately not a term
// in this invariant at all. Belt-and-braces against removeUserTierOverrideUpdate's own writer-side
// fix (ROUND 18, admin_user_repository.go), not merely redundant with it: that CASE only ever runs
// AT a removal, gated on a LIVE linked item existing right then; a stale marker on a user who is
// comped and STAYS comped (no removal ever happens, or one happens with no live item present yet)
// gets no help from that CASE at all, and would sit uncorrected for as long as that holds if this
// WHERE clause excluded a currently-comped user too. If this WHERE clause excluded a currently-comped
// user, a comp granted in the narrow window between a failed clear (step 1) and this backstop's next
// scheduled pass would suppress the heal for as long as the comp stays active.
//
// Same four properties as StampMissingPlaidCleanupMarkers, restated for the clear direction: (1)
// GetUsersNeedingPlaidDisconnectRepair is completely unchanged — this method only ever nulls a
// marker on a user that query's own live-item EXISTS clause would already exclude, so no candidate
// this backstop could ever select is affected; (2) one atomic UPDATE, so it cannot itself race —
// the same argument StampMissingPlaidCleanupMarkers's own doc comment makes; (3) a user this method
// clears gets a genuinely FRESH plaidDisconnectRepairGracePeriod the next time an episode opens for
// them (via Stamp, or via removeUserTierOverrideUpdate's plaid_cleanup_pending_at CASE — which, as
// of ROUND 18, overwrites unconditionally rather than COALESCE-preserving whatever this method left
// behind — now seeing NULL), never the OLD episode's stale grace window resumed; (4) reuses
// anyLinkedItemExistsClause and
// liveLinkedItemExistsClause — the SAME shared Go constants the candidate query, the backstop's own
// Stamp half, and admin_user_repository.go's removeUserTierOverrideSelectPrior already use — so
// "what counts as live" and "what counts as ever-linked" cannot silently drift between this method
// and any of them.
//
// Called from exactly one place, immediately after StampMissingPlaidCleanupMarkers: see
// RetentionCleanupCheckWorker.repairPlaidDisconnects's own doc comment (retention_cleanup_check.go)
// for why running this alongside Stamp, at the very start of EVERY pass, is what actually closes the
// scenario above within one backstop interval rather than leaving it open indefinitely.
//
// Deliberately two separate statements rather than one combined `CASE`-driven UPDATE touching both
// directions atomically: the two populations (marker IS NULL vs. marker IS NOT NULL) are disjoint by
// construction, so there is no row either statement could observe differently depending on run
// order, and this is a periodic healer re-run daily, not a transaction that must commit both halves
// together or not at all — each statement is independently idempotent and independently correct.
// Two statements read as two named, separately-countable operations (`healed` vs `cleared` in
// repairPlaidDisconnects's own log lines) rather than one CASE expression a reader has to mentally
// split back into two before they can even tell how many rows moved in which direction — clearer to
// read and clearer to count, at the acceptable cost of the pass as a WHOLE no longer being one atomic
// unit (each half already was, and always was, meant to stand alone).
//
// Same CAUTION as StampMissingPlaidCleanupMarkers above: this is a global-scan WRITE with no user-id
// parameter in its WHERE clause — every integration test fixture for this method must own its row
// end-to-end (unique, `t.Cleanup`'d), never rely on a shared test-setup helper.
//
// Deliberately NOT part of the exported ProfileRepository interface, for the same reason as
// StampMissingPlaidCleanupMarkers: needed only by RetentionCleanupCheckWorker's own narrow
// RetentionProfileRepository consumer interface (retention_cleanup_check.go).
func (r *profileRepository) ClearCompletedPlaidCleanupMarkers(ctx context.Context) (int, error) {
	ct, err := r.db.Exec(ctx, `
		UPDATE users
		SET plaid_cleanup_pending_at = NULL, updated_at = NOW()
		WHERE `+plaidDisconnectRepairEligibleStatusPredicate+`
		  AND plaid_cleanup_pending_at IS NOT NULL
		  AND is_deleted = FALSE
		  AND deletion_requested_at IS NULL
		  AND `+anyLinkedItemExistsClause+`
		  AND NOT `+liveLinkedItemExistsClause+`
	`)
	if err != nil {
		return 0, fmt.Errorf("ClearCompletedPlaidCleanupMarkers: %w", err)
	}
	return int(ct.RowsAffected()), nil
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
			&user.PlaidCleanupPendingAt,
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
	// Issue #1413 round-10: plaid_cleanup_pending_at starts atomically with this SAME CAS write —
	// see GetUsersNeedingPlaidDisconnectRepair's doc comment. COALESCE guards against a retried
	// call re-pushing an already-running marker forward.
	//
	// Issue #1413 Finding 2 (Medium, Codex review, PR #1443 follow-up) audited this marker COALESCE
	// for the same risk class every writer of this column shares; a bare substitution would not
	// actually fix anything for a single, non-CTE UPDATE like this one (verified empirically; see
	// StampMissingPlaidCleanupMarkers's own doc comment for the full reasoning), and the real fix
	// (restructuring into a locked CTE) was scoped out as a documented follow-up at the time.
	//
	// ROUND 23 (Issue #1413 Finding 2, High, Codex review, PR #1443 follow-up) is that follow-up.
	// This is one of the THREE genuine CAS writers restructured this round (the other two being
	// CancelIfPastDueOccurrenceWithRetention and ExpireTrialIfNoOverride, below) — its WHERE clause
	// is this method's own correctness mechanism (past_due_service.go's processGraceExpiry relies on
	// the returned bool to detect a lost race against a concurrent recovery/comp), so the CAS
	// predicate moves into the locked CTE's SELECT UNCHANGED — same two conditions
	// (`subscription_status = 'past_due'` and noActiveManualOverridePredicate), nothing added,
	// nothing removed. `FOR NO KEY UPDATE`, never `FOR UPDATE` — see StampMissingPlaidCleanupMarkers's
	// Finding 1 correction: this row can equally be a concurrent audit-log actor, and `FOR UPDATE`
	// would risk the identical cross-user deadlock there. The CTE selects only `id`; the COALESCE
	// reads plaid_cleanup_pending_at via the plain, unaliased `users` target-table reference in the
	// outer UPDATE — verified directly against a real database that this reflects the value as of
	// the moment the CTE's lock is actually acquired (post-wait), never a value frozen at this
	// statement's own pre-wait scan, exactly like StampMissingPlaidCleanupMarkers's own
	// clock_timestamp() does for the same reason.
	//
	// RowsAffected() semantics are unchanged: the join with the CTE produces zero rows in EXACTLY the
	// circumstances the bare UPDATE's WHERE clause used to produce zero rows for (id not found,
	// status no longer 'past_due', or a currently-active override) — never more, never fewer — so
	// past_due_service.go's `flipped` check still means exactly what it meant before this restructure.
	ct, err := r.db.Exec(ctx, `
		WITH candidate AS (
			SELECT id FROM users
			WHERE id = $1 AND subscription_status = 'past_due'
		`+noActiveManualOverridePredicate+`
			FOR NO KEY UPDATE
		)
		UPDATE users
		SET subscription_status = 'cancelled',
		    retention_started_at = $2,
		    retention_expires_at = $3,
		    plaid_cleanup_pending_at = COALESCE(plaid_cleanup_pending_at, clock_timestamp()),
		    updated_at = NOW()
		FROM candidate
		WHERE users.id = candidate.id
	`,
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
	// Issue #1413 round-10: plaid_cleanup_pending_at starts atomically with this SAME CAS write —
	// see GetUsersNeedingPlaidDisconnectRepair's doc comment. COALESCE guards against a retried
	// call re-pushing an already-running marker forward.
	//
	// Issue #1413 Finding 2 (Medium, Codex review, PR #1443 follow-up) audited this marker COALESCE
	// too, for the same reason as CancelIfPastDueWithRetention's own sibling note just above (this
	// file): a bare substitution would not actually fix anything here, and the real fix was scoped
	// into the same follow-up at the time.
	//
	// ROUND 23 (Issue #1413 Finding 2, High, Codex review, PR #1443 follow-up) is that follow-up.
	// This method's WHERE clause is its OWN correctness mechanism, more precisely than its
	// non-occurrence-scoped sibling above: `past_due_started_at = $4` is exactly the extra guard this
	// method exists to add (see this method's own H1 doc comment above) against a worker racing a
	// NEWER past_due cycle for the same user, so it moves into the CTE's SELECT alongside the other
	// two conditions, UNCHANGED and in full — `subscription_status = 'past_due'`,
	// `past_due_started_at = $4`, and noActiveManualOverridePredicate, nothing added, nothing removed.
	// Same `FOR NO KEY UPDATE` lock mode and same verified-fresh target-table COALESCE read as
	// CancelIfPastDueWithRetention's own ROUND 23 fix just above — see that method's doc comment for
	// the full mechanism.
	//
	// RowsAffected() semantics are unchanged: past_due_service.go's processGraceExpiry reads the
	// returned bool to decide between Completing (flipped) and Releasing (not flipped) its claim —
	// the join with the CTE still produces zero rows in EXACTLY the circumstances (id not found,
	// status no longer 'past_due', a DIFFERENT past_due cycle now occupies the row, or a
	// currently-active override) the bare UPDATE's WHERE clause used to.
	ct, err := r.db.Exec(ctx, `
		WITH candidate AS (
			SELECT id FROM users
			WHERE id = $1 AND subscription_status = 'past_due' AND past_due_started_at = $4
		`+noActiveManualOverridePredicate+`
			FOR NO KEY UPDATE
		)
		UPDATE users
		SET subscription_status = 'cancelled',
		    retention_started_at = $2,
		    retention_expires_at = $3,
		    plaid_cleanup_pending_at = COALESCE(plaid_cleanup_pending_at, clock_timestamp()),
		    updated_at = NOW()
		FROM candidate
		WHERE users.id = candidate.id
	`,
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
	// Issue #1413 round-10: plaid_cleanup_pending_at starts atomically with this SAME CAS write —
	// see GetUsersNeedingPlaidDisconnectRepair's doc comment. COALESCE guards against a retried
	// call re-pushing an already-running marker forward (defensive; the WHERE clause already
	// makes this a one-shot transition per trial).
	//
	// Issue #1413 Finding 2 (Medium, Codex review, PR #1443 follow-up) audited this marker COALESCE
	// too, for the same reason as UpdateSubscriptionFromPaddle's own doc comment (this file)
	// explains in full: a bare substitution would not actually fix anything here, and the real fix
	// was scoped into the same follow-up at the time.
	//
	// ROUND 23 (Issue #1413 Finding 2, High, Codex review, PR #1443 follow-up) is that follow-up.
	// This method's WHERE clause (`subscription_status = 'trial'` plus noActiveManualOverridePredicate)
	// is its own CAS/correctness mechanism — see this method's own H5 doc comment above: it authorises
	// the transition against committed state at write time, closing the race a caller's earlier,
	// non-atomic HasActiveManualOverride() read cannot. Both conditions move into the locked CTE's
	// SELECT unchanged, nothing added, nothing removed. Same `FOR NO KEY UPDATE` lock mode (never
	// `FOR UPDATE` — see StampMissingPlaidCleanupMarkers's Finding 1 correction for the cross-user
	// deadlock that mode would risk) and same verified-fresh target-table COALESCE read as
	// CancelIfPastDueWithRetention's own ROUND 23 fix — see that method's doc comment for the full
	// mechanism.
	//
	// RowsAffected() semantics are unchanged: this method's own callers use the returned bool exactly
	// as CancelIfPastDueWithRetention's callers do — the join with the CTE still produces zero rows
	// in EXACTLY the circumstances (id not found, status no longer 'trial', or a currently-active
	// override — i.e. an admin granted/extended a comp between the caller's HasActiveManualOverride()
	// read and this call) the bare UPDATE's WHERE clause used to.
	ct, err := r.db.Exec(ctx, `
		WITH candidate AS (
			SELECT id FROM users
			WHERE id = $1 AND subscription_status = 'trial'
		`+noActiveManualOverridePredicate+`
			FOR NO KEY UPDATE
		)
		UPDATE users
		SET subscription_status = 'inactive', updated_at = NOW(),
		    plaid_cleanup_pending_at = COALESCE(plaid_cleanup_pending_at, clock_timestamp())
		FROM candidate
		WHERE users.id = candidate.id
	`,
		userID)
	if err != nil {
		return false, fmt.Errorf("ExpireTrialIfNoOverride: %w", err)
	}
	return ct.RowsAffected() > 0, nil
}
