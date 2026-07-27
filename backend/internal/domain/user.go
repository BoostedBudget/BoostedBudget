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
	SubscriptionTier      string     `json:"subscription_tier" db:"subscription_tier"`             // premium, household_2, household_3, household_4, household_5
	SubscriptionStatus    string     `json:"subscription_status" db:"subscription_status"`         // active, inactive, trial, cancelled, past_due
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
