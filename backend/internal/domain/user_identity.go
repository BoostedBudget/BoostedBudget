package domain

import (
	"time"
)

// UserIdentity represents an external identity linked to a user account
// Supports multiple authentication providers per user (Google, Microsoft, Apple, etc.)
// Implements Issue #148 Phase 2 - Multi-Provider Authentication
type UserIdentity struct {
	// Primary identifier
	ID string `json:"id" db:"id"`

	// Foreign key to users table
	UserID string `json:"user_id" db:"user_id"`

	// Clerk identity (e.g., "user_xxx" for Clerk, "google-oauth2|123456" for legacy)
	ClerkUserID string `json:"clerk_user_id" db:"clerk_user_id"`

	// Provider information
	Provider       string `json:"provider" db:"provider"`              // clerk, google-oauth2, microsoft, apple, github
	ProviderUserID string `json:"provider_user_id" db:"provider_user_id"` // Extracted from ClerkUserID (e.g., "xxx")

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

// NewUserIdentity creates a new user identity for a given Clerk user ID
// Extracts provider and provider_user_id from the clerk_user_id format
func NewUserIdentity(userID, clerkUserID, email string, emailVerified, isPrimary bool) *UserIdentity {
	now := time.Now()

	// Extract provider and provider_user_id from clerk_user_id
	provider, providerUserID := ParseExternalUserID(clerkUserID)

	return &UserIdentity{
		UserID:         userID,
		ClerkUserID:    clerkUserID,
		Provider:       provider,
		ProviderUserID: providerUserID,
		Email:          email,
		EmailVerified:  emailVerified,
		IsPrimary:      isPrimary,
		CreatedAt:      now,
		UpdatedAt:      now,
	}
}

// ParseExternalUserID extracts the provider and provider_user_id from an external user ID
// Legacy Auth0 IDs follow the format: "provider|user_id"
// Clerk IDs follow the format: "user_xxx" (no pipe separator)
// Examples:
//   - "google-oauth2|123456" -> ("google-oauth2", "123456")
//   - "microsoft|789" -> ("microsoft", "789")
//   - "user_abc123" -> ("clerk", "abc123")
func ParseExternalUserID(externalUserID string) (provider string, providerUserID string) {
	// Find the pipe character separator (legacy Auth0 format)
	for i, ch := range externalUserID {
		if ch == '|' {
			provider = externalUserID[:i]
			providerUserID = externalUserID[i+1:]
			return
		}
	}

	// No pipe found - check for Clerk format (user_xxx)
	if len(externalUserID) > 5 && externalUserID[:5] == "user_" {
		return "clerk", externalUserID[5:]
	}

	// Fallback: treat entire string as clerk provider
	// This handles new Clerk IDs or other formats gracefully
	return "clerk", externalUserID
}
