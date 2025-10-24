package domain

import (
	"time"
)

// UserIdentity represents an Auth0 identity linked to a user account
// Supports multiple authentication providers per user (Google, Microsoft, Apple, etc.)
// Implements Issue #148 Phase 2 - Multi-Provider Authentication
type UserIdentity struct {
	// Primary identifier
	ID string `json:"id" db:"id"`

	// Foreign key to users table
	UserID string `json:"user_id" db:"user_id"`

	// Auth0 identity (e.g., "google-oauth2|123456", "microsoft|789")
	Auth0UserID string `json:"auth0_user_id" db:"auth0_user_id"`

	// Provider information
	Provider       string `json:"provider" db:"provider"`              // google-oauth2, microsoft, apple, github, auth0
	ProviderUserID string `json:"provider_user_id" db:"provider_user_id"` // Extracted from Auth0UserID (e.g., "123456")

	// Email from this specific provider (may differ across providers)
	Email         string `json:"email,omitempty" db:"email"`
	EmailVerified bool   `json:"email_verified" db:"email_verified"`

	// Identity status
	IsPrimary bool `json:"is_primary" db:"is_primary"` // Primary identity for this user

	// Timestamps
	CreatedAt  time.Time  `json:"created_at" db:"created_at"`
	UpdatedAt  time.Time  `json:"updated_at" db:"updated_at"`
	LastUsedAt *time.Time `json:"last_used_at,omitempty" db:"last_used_at"` // Last login with this identity
}

// NewUserIdentity creates a new user identity for a given Auth0 user ID
// Extracts provider and provider_user_id from the auth0_user_id format
func NewUserIdentity(userID, auth0UserID, email string, emailVerified, isPrimary bool) *UserIdentity {
	now := time.Now()

	// Extract provider and provider_user_id from auth0_user_id
	provider, providerUserID := ParseAuth0UserID(auth0UserID)

	return &UserIdentity{
		UserID:         userID,
		Auth0UserID:    auth0UserID,
		Provider:       provider,
		ProviderUserID: providerUserID,
		Email:          email,
		EmailVerified:  emailVerified,
		IsPrimary:      isPrimary,
		CreatedAt:      now,
		UpdatedAt:      now,
	}
}

// ParseAuth0UserID extracts the provider and provider_user_id from an Auth0 user ID
// Auth0 user IDs follow the format: "provider|user_id"
// Examples:
//   - "google-oauth2|123456" -> ("google-oauth2", "123456")
//   - "microsoft|789" -> ("microsoft", "789")
//   - "auth0|abc123" -> ("auth0", "abc123")
func ParseAuth0UserID(auth0UserID string) (provider string, providerUserID string) {
	// Find the pipe character separator
	for i, ch := range auth0UserID {
		if ch == '|' {
			provider = auth0UserID[:i]
			providerUserID = auth0UserID[i+1:]
			return
		}
	}

	// Fallback: if no pipe found, treat entire string as auth0 provider
	// This handles legacy or malformed IDs gracefully
	return "auth0", auth0UserID
}
