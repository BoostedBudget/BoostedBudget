package domain

import (
	"time"

	"github.com/google/uuid"
)

// User represents a user account in the system with GDPR-compliant data residency
type User struct {
	// Primary identifier
	ID string `json:"id" db:"id"`

	// Clerk user ID (from JWT sub claim)
	ClerkUserID string `json:"clerk_user_id" db:"clerk_user_id"`

	// Basic user information
	Email         string `json:"email" db:"email"`
	EmailVerified bool   `json:"email_verified" db:"email_verified"`

	// Subscription information
	SubscriptionTier      string     `json:"subscription_tier" db:"subscription_tier"`     // premium, household_2, household_3, household_4, household_5
	SubscriptionStatus    string     `json:"subscription_status" db:"subscription_status"` // active, inactive, trial, cancelled, past_due
	SubscriptionStartedAt *time.Time `json:"subscription_started_at,omitempty" db:"subscription_started_at"`
	SubscriptionEndsAt    *time.Time `json:"subscription_ends_at,omitempty" db:"subscription_ends_at"`

	// Manual tier overrides (Issue #641)
	ManualTierOverride *string    `json:"manual_tier_override,omitempty" db:"manual_tier_override"`
	ManualTierEndsAt   *time.Time `json:"manual_tier_ends_at,omitempty" db:"manual_tier_ends_at"`
	EffectiveTier      string     `json:"effective_tier,omitempty"` // Computed field, not stored in DB

	// Effective status/comp fields (Issue #1413) — computed, not stored in DB.
	// Populated by a later task (see EffectiveTier precedent above).
	EffectiveStatus string     `json:"effective_status,omitempty"`
	IsComped        bool       `json:"is_comped"` // No omitempty: frontend must always see this key, never distinguish false from absent.
	CompTier        string     `json:"comp_tier,omitempty"`
	CompEndsAt      *time.Time `json:"comp_ends_at,omitempty"`

	// Trial information (Issue #679)
	TrialStartedAt     *time.Time `json:"trial_started_at,omitempty" db:"trial_started_at"`
	TrialEndsAt        *time.Time `json:"trial_ends_at,omitempty" db:"trial_ends_at"`
	HadTrial           bool       `json:"had_trial" db:"had_trial"`
	TrialDaysRemaining *int       `json:"trial_days_remaining,omitempty"` // Computed field, not stored in DB

	// Paddle customer ID (for payments)
	PaddleCustomerID *string `json:"paddle_customer_id,omitempty" db:"paddle_customer_id"`

	// Paddle subscription ID (for subscription management)
	PaddleSubscriptionID *string `json:"paddle_subscription_id,omitempty" db:"paddle_subscription_id"`

	// GDPR: Primary region for data residency (Fly.io IATA codes)
	PrimaryRegion string `json:"primary_region" db:"primary_region"` // see config/regions.yaml

	// Account status
	IsActive  bool `json:"is_active" db:"is_active"`
	IsDeleted bool `json:"is_deleted" db:"is_deleted"`

	// GDPR: Account deletion (30-day grace period)
	DeletionRequestedAt *time.Time `json:"deletion_requested_at,omitempty" db:"deletion_requested_at"`
	DeletionScheduledAt *time.Time `json:"deletion_scheduled_at,omitempty" db:"deletion_scheduled_at"`

	// GDPR: Retention tracking - 6 months read-only, then auto-delete (Issue #930)
	RetentionStartedAt       *time.Time `json:"retention_started_at,omitempty" db:"retention_started_at"`
	RetentionExpiresAt       *time.Time `json:"retention_expires_at,omitempty" db:"retention_expires_at"`
	RetentionNotice30dSentAt *time.Time `json:"retention_notice_30d_sent_at,omitempty" db:"retention_notice_30d_sent_at"`
	RetentionNotice7dSentAt  *time.Time `json:"retention_notice_7d_sent_at,omitempty" db:"retention_notice_7d_sent_at"`

	// Dunning: tracks when a user entered past_due state (Issue #898)
	PastDueStartedAt *time.Time `json:"past_due_started_at,omitempty" db:"past_due_started_at"`

	// PlaidCleanupPendingAt is the Issue #1413 round-10 explicit marker for
	// RetentionCleanupCheckWorker.repairPlaidDisconnects's grace-window anchor — see
	// GetUsersNeedingPlaidDisconnectRepair's doc comment (profile_repository.go) for why this
	// REPLACES RetentionStartedAt (the derived anchor rounds 8/9 showed could not be made sound).
	//
	// THE INVARIANT (Issue #1413 round-13 — read this before touching ANY writer or clearer of
	// this column):
	//
	//   plaid_cleanup_pending_at is non-NULL EXACTLY WHEN an ineligibility EPISODE has begun —
	//   i.e. domain.SubscriptionStatus(subscription_status).IsEligibleForPlaidDisconnectRepair()
	//   is true (raw status is 'inactive' or 'cancelled') — AND outstanding Plaid cleanup for that
	//   episode MAY not yet have completed. Its value is the instant the CURRENT episode began —
	//   never an earlier episode's (a stale write must not resurrect a value a newer episode already
	//   superseded) and never a later one's (a fresh episode must get its OWN fresh value, not the
	//   survivor of a stale COALESCE).
	//
	//   Round-15: note "MAY not yet have completed" — the marker is deliberately OPTIMISTIC, and
	//   this is a weakening of the round-13/round-14 wording, made knowingly. Writers cannot decide
	//   reliably whether cleanup is still outstanding, because that depends on the linked_items
	//   lifecycle, which they do not serialize against: the override-removal path's liveness-gated
	//   stamp lost a race with LinkAccounts (its EXISTS is a snapshot, and FOR UPDATE locks the
	//   users row, not linked_items — so a held lock merely delays the concurrent insert until after
	//   the admin's commit, producing the leak rather than preventing it). Between "missed stamp =
	//   permanent, unrecoverable leak (live bank connections never disconnected)" and "spurious
	//   stamp = phantom row the repair query already filters out", writers must err toward stamping.
	//   So: a non-NULL marker means cleanup MIGHT be outstanding, and
	//   GetUsersNeedingPlaidDisconnectRepair's live-item EXISTS is the sole authority on whether it
	//   actually is. A marker with zero live items is an accepted phantom (#1453 — which this makes
	//   an ongoing population rather than a historical one), NOT a violation. What remains strict,
	//   and what every writer must still hold, is the LEAK direction: never a NULL marker while an
	//   episode is open, and never a marker while no episode is open.
	//
	//   Deliberately NOT part of this invariant: manual_tier_override. Whether the user carries a
	//   currently-active admin comp is a function of WALL-CLOCK TIME (HasActiveManualOverride()
	//   compares manual_tier_ends_at against time.Now() on every read), not of any write — an
	//   override can transition from live to lapsed with NO writer ever running. Round-11 through
	//   round-12 tied the marker's SET-time value to "is there a live override right now", which
	//   silently required some LATER writer (the comp-expiry job, or an admin clicking Remove
	//   Override) to re-stamp the marker the instant the override lapsed — and if that later
	//   writer never runs (a degraded/dead-lettered expiry job, or an admin who never clicks),
	//   the marker stays wrong FOREVER, with no writer at fault: the invariant's own truth value
	//   changed on the clock, not on a write, so no write-time trigger could have kept it correct.
	//   That is the round-13 root-cause finding: comping an already inactive/cancelled user (a
	//   plausible real action — comping a churned account) nulled the marker on grant, the comp
	//   later lapsed with no write, and the user's live Plaid connections became permanently
	//   invisible to every query in the codebase.
	//
	//   The fix is to keep the time-varying term OUT of this column entirely: the marker records
	//   only the durable fact "an ineligibility episode began, cleanup outstanding" — a pure
	//   function of raw subscription_status transitions. A comp grant does not touch raw
	//   subscription_status, so it must not touch this column either; a comp lapsing is a
	//   read-time fact with nothing to write. CURRENT eligibility (raw status AND whether a
	//   manual override is presently live) is evaluated separately, entirely at query time, by
	//   GetUsersNeedingPlaidDisconnectRepair's own noActiveManualOverridePredicate — never baked
	//   into this column's value.
	//
	// Every writer must leave the column in a state satisfying this invariant when it returns —
	// not merely "correct for the one transition this writer handles in isolation". That
	// isolated-reasoning failure mode is exactly how several prior remediation rounds on this
	// branch each closed the previous reviewer's finding while introducing a new one. As of
	// round-13 the known SET sites are: UpdateSubscriptionFromPaddle,
	// CancelIfPastDueWithRetention(Occurrence), ExpireTrialIfNoOverride (all
	// profile_repository.go), and AdminUserRepository's removeUserTierOverrideUpdate
	// (admin_user_repository.go, shared by RemoveUserTierOverride/RemoveUserTierOverrideIfMatches).
	// Round-14 finding M1, and its round-15 REVERSAL — read both halves, the second supersedes the
	// first: round-14 gated this last site's should_mark_plaid_cleanup_pending CASE on
	// liveLinkedItemExistsClause, because firing for ANY prior override on an inactive/cancelled
	// user re-armed a phantom marker for an already-ended episode (cancel -> repair completes and
	// clears -> comp granted over the cleaned-up user -> comp removed while raw status is still
	// ineligible). Round-15 REMOVED that gate again and restored the unconditional stamp: the gate
	// introduced a strictly worse bug, losing a race with LinkAccounts as described in the invariant
	// note above, and no lock available to this repository can close it (LinkAccounts' insert is an
	// independent autocommit statement, so locking only reorders the interleaving into the leak).
	// A phantom marker is untidy; a missed one is an unrecoverable leak. The stamp still serves its
	// original purpose (a user whose raw status was already ineligible before ever gaining a marker,
	// e.g. an imported/backfilled row) and is a no-op COALESCE whenever the marker is already set.
	// The two round-14 M1 regression tests were renamed and flipped (Nil -> NotNil) rather than left
	// silently broken, and assertPlaidMarkerInvariant was relaxed to match — see its own comment. Not
	// a SET/CLEAR site: UpdateSubscriptionStatus (Issue #1413 round-14 finding L1) — it writes
	// subscription_status directly but never touches this column at all; see its own doc comment
	// for why it must not be used for any transition into or out of a read-only status. The known
	// CLEAR sites are: UpdateSubscriptionFromPaddle, ClearRetentionDates,
	// ClearPlaidCleanupPending/ClearPlaidCleanupPendingIfMatches, UpdateTrialStatus (all
	// profile_repository.go). AdminUserRepository's updateUserTierQuery (comp grant/extend)
	// deliberately does NOT touch this column at all as of round-13 — see its own doc comment.
	// See each site's own doc comment for its justification against this invariant, and
	// GetUsersNeedingPlaidDisconnectRepair's doc comment for the query whose correctness depends
	// entirely on it holding.
	PlaidCleanupPendingAt *time.Time `json:"plaid_cleanup_pending_at,omitempty" db:"plaid_cleanup_pending_at"`

	// Timezone settings (Issue #972)
	// Timezone stores the user's IANA timezone string (default "UTC").
	// TimezoneSource tracks how it was set: "default", "auto" (browser-detected), or "user" (explicit choice).
	Timezone       string `json:"timezone" db:"timezone"`
	TimezoneSource string `json:"timezone_source" db:"timezone_source"`

	// Timestamps
	CreatedAt   time.Time  `json:"created_at" db:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at" db:"updated_at"`
	LastLoginAt *time.Time `json:"last_login_at,omitempty" db:"last_login_at"`
}

// UpdateProfileRequest represents the request body for updating a user profile
// Only allows updating specific fields (email, primary_region, timezone, timezone_source)
// Subscription fields are managed separately through payment webhooks
type UpdateProfileRequest struct {
	Email          *string `json:"email,omitempty"`
	PrimaryRegion  *string `json:"primary_region,omitempty"`
	Timezone       *string `json:"timezone,omitempty"`
	TimezoneSource *string `json:"timezone_source,omitempty"`
}

// ComputeTrialDaysRemaining calculates and sets the TrialDaysRemaining field
// Returns the number of days remaining in trial, or nil if not on trial
func (u *User) ComputeTrialDaysRemaining() {
	if u.SubscriptionStatus != "trial" || u.TrialEndsAt == nil {
		u.TrialDaysRemaining = nil
		return
	}

	daysRemaining := int(time.Until(*u.TrialEndsAt).Hours() / 24)
	if daysRemaining < 0 {
		daysRemaining = 0
	}
	u.TrialDaysRemaining = &daysRemaining
}

// hasActiveManualOverrideAt reports whether a manual tier override is set and has not expired
// as of the given timestamp. Extracted, character-for-character, from the original
// ComputeEffectiveTier condition, so that ComputeEffectiveTier and ComputeEffectiveStatus share
// a single predicate and can never disagree about whether an override is currently active.
func (u *User) hasActiveManualOverrideAt(now time.Time) bool {
	if u.ManualTierOverride != nil && *u.ManualTierOverride != "" {
		if u.ManualTierEndsAt == nil || !u.ManualTierEndsAt.Before(now) {
			return true
		}
	}
	return false
}

// HasActiveManualOverride reports whether the user currently has a non-expired manual tier
// override in effect, evaluated against the current time.
func (u *User) HasActiveManualOverride() bool {
	return u.hasActiveManualOverrideAt(time.Now())
}

// HasAnyManualOverride reports whether the user carries a manual tier override AT ALL — live or
// already lapsed. Unlike HasActiveManualOverride, expiry is irrelevant here: this is the Go twin
// of the SQL noManualOverrideAtAllPredicate (profile_repository.go), used by belt-and-braces
// guards that must agree with the retention queries' "exclude on any override, live or lapsed"
// rule (Issue #1413 Critical finding C1). A comp that lapsed but has not yet been cleared by the
// daily manual_tier_override_expiry_check job must still block auto-deletion, not just a
// currently-active one — using HasActiveManualOverride for that guard was the C1 bug.
func (u *User) HasAnyManualOverride() bool {
	return u.ManualTierOverride != nil && *u.ManualTierOverride != ""
}

// computeEffectiveTierAt returns the effective tier for the user as of the given timestamp.
// If an active manual override is in effect at that instant, its tier is returned; otherwise
// the base SubscriptionTier is returned.
func (u *User) computeEffectiveTierAt(now time.Time) string {
	if u.hasActiveManualOverrideAt(now) {
		return *u.ManualTierOverride
	}
	return u.SubscriptionTier
}

// ComputeEffectiveTier returns the effective tier for the user.
// If a non-expired manual override is set, that tier is returned.
// Otherwise the base SubscriptionTier is returned.
func (u *User) ComputeEffectiveTier() string {
	return u.computeEffectiveTierAt(time.Now())
}

// computeEffectiveStatusAt returns the effective subscription status for the user as of the
// given timestamp. An active manual override forces the status to active — an admin "comp"
// must actually unlock access, not just relabel the tier while every access gate keeps reading
// the stale, untouched SubscriptionStatus (Issue #1413).
func (u *User) computeEffectiveStatusAt(now time.Time) string {
	if u.hasActiveManualOverrideAt(now) {
		return string(SubscriptionStatusActive)
	}
	return u.SubscriptionStatus
}

// ComputeEffectiveStatus returns the effective subscription status for the user.
// If an active manual tier override is set, the status is forced to active (Issue #1413).
// Otherwise the base SubscriptionStatus is returned unchanged.
func (u *User) ComputeEffectiveStatus() string {
	return u.computeEffectiveStatusAt(time.Now())
}

// EffectiveStatusWithActiveOverride returns the same value ComputeEffectiveStatus() would,
// plus the raw manual_tier_override string when (and only when) a currently-active override is
// what produced that status.
//
// Added for Issue #1413 G2: manual_tier_override is an unconstrained VARCHAR(50) with no CHECK
// constraint, so its value can be garbage (a legacy row, a direct SQL write, a write made while
// the tier config was nil). A caller on the access path — e.g.
// GetEffectiveSubscriptionStatusByUserID (backend/internal/repository/profile_repository.go) —
// needs to validate that the override names a real configured tier before trusting "active",
// which domain cannot do itself without importing config (forbidden: domain must stay
// config-agnostic; see CLAUDE.md).
//
// Both values are computed from a single time.Now() read, exactly like ClerkPublicMetadata does
// above, rather than having the caller separately call ComputeEffectiveStatus() and
// HasActiveManualOverride() (each reading time.Now() independently) — that would risk the two
// disagreeing about whether the override was active in the nanoseconds around the expiry
// boundary. See TestEffectiveTierAndStatusAt_AgreeAtSharedBoundary for why that boundary matters.
//
// hasOverride is false whenever there is no CURRENTLY ACTIVE override (none set, or expired); in
// that case status is simply the raw SubscriptionStatus and overrideValue is "".
func (u *User) EffectiveStatusWithActiveOverride() (status string, overrideValue string, hasOverride bool) {
	now := time.Now()
	if u.hasActiveManualOverrideAt(now) {
		return string(SubscriptionStatusActive), *u.ManualTierOverride, true
	}
	return u.SubscriptionStatus, "", false
}

// EffectiveTierWithActiveOverride returns the same value ComputeEffectiveTier() would, plus the
// raw manual_tier_override string when (and only when) a currently-active override is what
// produced that tier. This is the TIER-axis twin of EffectiveStatusWithActiveOverride, added for
// Issue #1413 (this task): manual_tier_override is an unconstrained VARCHAR(50) with no CHECK
// constraint, so its value can be garbage (a legacy row, a direct SQL write, a write made while
// the tier config was nil). A caller that can look up configured tiers (e.g. via a
// *config.TierConfig it already holds, or an injected validator func) must validate overrideValue
// before trusting the returned tier, falling back to the raw SubscriptionTier on rejection —
// exactly like every tier-config-aware caller already falls back to the raw SubscriptionStatus on
// the status axis. domain cannot do this validation itself without importing config (forbidden;
// see CLAUDE.md), so it only surfaces the raw pieces for the caller to check.
//
// Both status and tier ultimately derive from the identical hasActiveManualOverrideAt check, so
// a caller that already computed EffectiveStatusWithActiveOverride() for the same user does not
// need to call this too — its overrideValue/hasOverride are exactly what this method would also
// return; reuse the status call's result to avoid a redundant time.Now() read.
//
// hasOverride is false whenever there is no CURRENTLY ACTIVE override (none set, or expired); in
// that case tier is simply the raw SubscriptionTier and overrideValue is "".
func (u *User) EffectiveTierWithActiveOverride() (tier string, overrideValue string, hasOverride bool) {
	now := time.Now()
	if u.hasActiveManualOverrideAt(now) {
		return *u.ManualTierOverride, *u.ManualTierOverride, true
	}
	return u.SubscriptionTier, "", false
}

// ClerkPublicMetadata returns the canonical Clerk publicMetadata payload for this user.
//
// The "tier" key is deliberately included with an explicit nil value rather than being
// omitted. Clerk's PATCH /v1/users/{id}/metadata endpoint performs a shallow merge of the
// supplied map into existing publicMetadata; passing an explicit null for a key deletes that
// key server-side, whereas omitting the key entirely would leave any existing value untouched.
// A pre-existing bug wrote an orphaned "tier" key into Clerk metadata, and this payload
// retroactively cleans it up on every sync.
//
// Note: no backend access gate reads Clerk metadata today — auth.ExtractSubscriptionTier's
// only callers are tests. Staleness of this payload (e.g. before a later task wires up sync)
// is therefore a UI/analytics concern, not a security one; the backend's authoritative source
// is ComputeEffectiveTier/ComputeEffectiveStatus evaluated directly against the database row.
//
// tier and status are computed from a single shared clock read rather than by calling the
// public ComputeEffectiveTier/ComputeEffectiveStatus methods (which would each read time.Now()
// independently), so the two fields can never disagree about whether an override is live.
func (u *User) ClerkPublicMetadata() map[string]interface{} {
	now := time.Now()
	return map[string]interface{}{
		"subscription_tier":   u.computeEffectiveTierAt(now),
		"subscription_status": u.computeEffectiveStatusAt(now),
		"tier":                nil,
	}
}

// NewUser creates a new user with default values (premium trial, default region)
// Used when a user logs in for the first time via Clerk
func NewUser(clerkUserID, email string, emailVerified bool) *User {
	now := time.Now()
	return &User{
		ID:                 uuid.New().String(),
		ClerkUserID:        clerkUserID,
		Email:              email,
		EmailVerified:      emailVerified,
		SubscriptionTier:   "premium",
		SubscriptionStatus: "trial",
		PrimaryRegion:      DefaultRegionCode(),
		IsActive:           true,
		IsDeleted:          false,
		Timezone:           "UTC",
		TimezoneSource:     "default",
		CreatedAt:          now,
		UpdatedAt:          now,
	}
}
