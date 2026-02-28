package domain

import "time"

// AdminUserListItem represents a user in the admin list view.
// Contains summarized user information with aggregated counts.
type AdminUserListItem struct {
	ID                   string     `json:"id"`
	Email                string     `json:"email"`
	SubscriptionTier     string     `json:"subscription_tier"`
	SubscriptionStatus   string     `json:"subscription_status"`
	ManualTierOverride   *string    `json:"manual_tier_override,omitempty"`
	PrimaryRegion        string     `json:"primary_region"`
	CreatedAt            time.Time  `json:"created_at"`
	LastLoginAt          *time.Time `json:"last_login_at,omitempty"`
	IsActive             bool       `json:"is_active"`
	IsAdmin              bool       `json:"is_admin"`
	LinkedAccountsCount  int        `json:"linked_accounts_count"`
	AutomationRulesCount int        `json:"automation_rules_count"`
	ReferralsCount       int        `json:"referrals_count"`
}

// AdminUserDetail extends AdminUserListItem with additional statistics.
// Used for the detailed user view in admin panel.
type AdminUserDetail struct {
	AdminUserListItem
	TransactionsCount int        `json:"transactions_count"`
	BudgetsCount      int        `json:"budgets_count"`
	ManualTierEndsAt  *time.Time `json:"manual_tier_ends_at,omitempty"`
}

// AdminUserFilters contains filter parameters for the admin user list.
type AdminUserFilters struct {
	// Search is a partial email match (ILIKE search)
	Search string
	// Tier filters by subscription_tier
	Tier string
}

// AdminUserListResponse contains paginated user list results for admin view.
type AdminUserListResponse struct {
	Users      []AdminUserListItem `json:"users"`
	TotalCount int                 `json:"total_count"`
	Page       int                 `json:"page"`
	PageSize   int                 `json:"page_size"`
	TotalPages int                 `json:"total_pages"`
	HasMore    bool                `json:"has_more"`
}
