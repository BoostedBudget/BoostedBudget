// Package domain defines the core domain types for savings impact tracking.
// Issue #972: Savings Transfer Automation — daily aggregate stats.
package domain

import "time"

// SavingsImpactSnapshot represents a daily aggregate snapshot of savings transfer
// automation impact across all users who have opted into aggregate statistics.
// One snapshot per UTC calendar day (enforced by unique index on DATE(computed_at AT TIME ZONE 'UTC')).
type SavingsImpactSnapshot struct {
	ID             string    `json:"id"              db:"id"`
	ComputedAt     time.Time `json:"computed_at"     db:"computed_at"`
	TotalAmount    float64   `json:"total_amount"    db:"total_amount"`
	UserCount      int       `json:"user_count"      db:"user_count"`
	CompletedCount int       `json:"completed_count" db:"completed_count"`
}

// UserSavingsImpact holds per-user savings impact totals and recent activity.
type UserSavingsImpact struct {
	TotalAmount     float64                      `json:"total_amount"`
	CompletedCount  int                          `json:"completed_count"`
	MonthAmount     float64                      `json:"month_amount"`      // current month
	PrevMonthAmount float64                      `json:"prev_month_amount"` // previous month
	RecentItems     []*CompletedSuggestionSummary `json:"recent_items"`      // last 5
}

// CompletedSuggestionSummary is a lightweight summary of a completed transfer suggestion
// returned as part of UserSavingsImpact.
type CompletedSuggestionSummary struct {
	ID               string     `json:"id"`
	Amount           float64    `json:"amount"`
	ActualAmount     float64    `json:"actual_amount"`
	ToAccountID      *string    `json:"to_account_id,omitempty"`
	ToUserAccountID  *string    `json:"to_user_account_id,omitempty"`
	// ToAccountName is the resolved display name of the destination account.
	// For Plaid-linked accounts this is COALESCE(nickname, name) from the accounts
	// table. For manual accounts it is COALESCE(nickname, decrypted account_name_hint)
	// from user_accounts. Nil when no name can be resolved (e.g. manual account with
	// no nickname and no account_name_hint). The handler passes the domain struct
	// directly (no DTO), so this field flows automatically into the OpenAPI spec and
	// the generated api-client. GAP CA (#972).
	ToAccountName    *string    `json:"to_account_name,omitempty"`
	CompletedAt      *time.Time `json:"completed_at,omitempty"`
	CompletionSource *string    `json:"completion_source,omitempty"`
}
