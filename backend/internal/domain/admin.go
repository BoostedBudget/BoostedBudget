// Package domain contains domain models and types for the BoostedBudget application.
package domain

// AdminRole represents a role that can be assigned to users for admin access.
type AdminRole string

const (
	// RoleAdmin is the admin role that grants access to admin endpoints.
	RoleAdmin AdminRole = "admin"
)

// AdminPrivateMetadata represents the structure of Clerk user privateMetadata
// that contains role information.
// Expected format in Clerk Dashboard: {"roles": ["admin", "support"]}
type AdminPrivateMetadata struct {
	Roles []string `json:"roles"`
}

// AdminEmailConfig holds the configuration for hardcoded admin emails.
// These emails are loaded from config/admin_emails.yaml and grant admin access
// regardless of Clerk privateMetadata settings.
type AdminEmailConfig struct {
	AdminEmails []string `yaml:"admin_emails"`
}

// CachedRole stores cached role information for a Clerk user.
// Used by AdminService to avoid repeated Clerk API calls.
type CachedRole struct {
	ClerkUserID string
	Roles       []AdminRole
	CachedAt    int64 // Unix timestamp in seconds
}
