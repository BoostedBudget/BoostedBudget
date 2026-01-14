package domain

import (
	"time"
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

	// Trial information (Issue #679)
	TrialStartedAt     *time.Time `json:"trial_started_at,omitempty" db:"trial_started_at"`
	TrialEndsAt        *time.Time `json:"trial_ends_at,omitempty" db:"trial_ends_at"`
	HadTrial           bool       `json:"had_trial" db:"had_trial"`
	TrialDaysRemaining *int       `json:"trial_days_remaining,omitempty"` // Computed field, not stored in DB

	// Paddle customer ID (for payments)
	PaddleCustomerID *string `json:"paddle_customer_id,omitempty" db:"paddle_customer_id"`

	// GDPR: Primary region for data residency (Fly.io IATA codes)
	PrimaryRegion string `json:"primary_region" db:"primary_region"` // iad, lhr, syd

	// Account status
	IsActive  bool `json:"is_active" db:"is_active"`
	IsDeleted bool `json:"is_deleted" db:"is_deleted"`

	// GDPR: Account deletion (30-day grace period)
	DeletionRequestedAt *time.Time `json:"deletion_requested_at,omitempty" db:"deletion_requested_at"`
	DeletionScheduledAt *time.Time `json:"deletion_scheduled_at,omitempty" db:"deletion_scheduled_at"`

	// Timestamps
	CreatedAt   time.Time  `json:"created_at" db:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at" db:"updated_at"`
	LastLoginAt *time.Time `json:"last_login_at,omitempty" db:"last_login_at"`
}

// UpdateProfileRequest represents the request body for updating a user profile
// Only allows updating specific fields (email, primary_region)
// Subscription fields are managed separately through payment webhooks
type UpdateProfileRequest struct {
	Email         *string `json:"email,omitempty"`
	PrimaryRegion *string `json:"primary_region,omitempty"`
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

// NewUser creates a new user with default values (premium trial, iad region)
// Used when a user logs in for the first time via Clerk
func NewUser(clerkUserID, email string, emailVerified bool) *User {
	now := time.Now()
	return &User{
		ClerkUserID:        clerkUserID,
		Email:              email,
		EmailVerified:      emailVerified,
		SubscriptionTier:   "premium",
		SubscriptionStatus: "trial",
		PrimaryRegion:      DefaultRegion, // iad - uses constant from regions.go
		IsActive:           true,
		IsDeleted:          false,
		CreatedAt:          now,
		UpdatedAt:          now,
	}
}
