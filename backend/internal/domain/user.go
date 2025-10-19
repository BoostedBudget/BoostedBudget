package domain

import (
	"time"
)

// User represents a user account in the system with GDPR-compliant data residency
type User struct {
	// Primary identifier
	ID string `json:"id" db:"id"`

	// Auth0 user ID (from JWT sub claim)
	Auth0UserID string `json:"auth0_user_id" db:"auth0_user_id"`

	// Basic user information
	Email         string `json:"email" db:"email"`
	EmailVerified bool   `json:"email_verified" db:"email_verified"`

	// Subscription information
	SubscriptionTier   string     `json:"subscription_tier" db:"subscription_tier"`       // free, premium, premium_plus
	SubscriptionStatus string     `json:"subscription_status" db:"subscription_status"`   // active, cancelled, past_due, unpaid
	SubscriptionStartedAt *time.Time `json:"subscription_started_at,omitempty" db:"subscription_started_at"`
	SubscriptionEndsAt    *time.Time `json:"subscription_ends_at,omitempty" db:"subscription_ends_at"`

	// Stripe customer ID (for payments)
	StripeCustomerID *string `json:"stripe_customer_id,omitempty" db:"stripe_customer_id"`

	// GDPR: Primary region for data residency
	PrimaryRegion string `json:"primary_region" db:"primary_region"` // us-east-1, eu-west-1, ap-southeast-1

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

// NewUser creates a new user with default values (free tier, us-east-1 region)
// Used when a user logs in for the first time via Auth0
func NewUser(auth0UserID, email string) *User {
	now := time.Now()
	return &User{
		Auth0UserID:        auth0UserID,
		Email:              email,
		EmailVerified:      false,
		SubscriptionTier:   "free",
		SubscriptionStatus: "active",
		PrimaryRegion:      "us-east-1",
		IsActive:           true,
		IsDeleted:          false,
		CreatedAt:          now,
		UpdatedAt:          now,
	}
}
