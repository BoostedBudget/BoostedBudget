package domain

import "time"

// AdminUserListItem represents a user in the admin list view.
// Contains summarized user information with aggregated counts.
type AdminUserListItem struct {
	ID                 string `json:"id"`
	Email              string `json:"email"`
	SubscriptionTier   string `json:"subscription_tier" doc:"Observed own-access projection used for tier filtering and sorting; see effective_tier for live aggregate access"`
	SubscriptionStatus string `json:"subscription_status" doc:"Observed own-access projection used for sorting; see effective_status and access_resolved for live authority"`
	// EffectiveStatus is the server-computed effective subscription status (Issue #1413) — it
	// forces "active" when a live manual tier override exists, otherwise mirrors
	// SubscriptionStatus. No omitempty: the frontend must always see this key so it never
	// distinguishes "empty" from "absent" (mirrors domain.User.IsComped's precedent).
	EffectiveStatus    string     `json:"effective_status"`
	EffectiveTier      string     `json:"effective_tier,omitempty"`
	AccessResolved     *bool      `json:"access_resolved,omitempty"`
	ManualTierOverride *string    `json:"manual_tier_override,omitempty"`
	ManualTierEndsAt   *time.Time `json:"manual_tier_ends_at,omitempty"`
	// DeletionRequestedAt/DeletionScheduledAt surface a pending GDPR account deletion (Issue
	// #1413) so the admin override modal can warn that a comp will not save an account already
	// scheduled for deletion. Absent (omitempty) means no pending deletion.
	DeletionRequestedAt *time.Time `json:"deletion_requested_at,omitempty"`
	DeletionScheduledAt *time.Time `json:"deletion_scheduled_at,omitempty"`
	PrimaryRegion       string     `json:"primary_region"`
	CreatedAt           time.Time  `json:"created_at"`
	LastLoginAt         *time.Time `json:"last_login_at,omitempty"`
	IsActive            bool       `json:"is_active"`
	IsAdmin             bool       `json:"is_admin"`
	LinkedAccountsCount int        `json:"linked_accounts_count"`
	BoostsCount         int        `json:"boosts_count"`
	ReferralsCount      int        `json:"referrals_count"`
}

// AdminUserDetail extends AdminUserListItem with additional statistics.
// Used for the detailed user view in admin panel.
type AdminUserDetail struct {
	AdminUserListItem
	TransactionsCount int `json:"transactions_count"`
	BudgetsCount      int `json:"budgets_count"`
	// AsOf is the server instant EffectiveStatus (on the embedded AdminUserListItem) was computed
	// from (Issue #1462 follow-up G2) — lets UserDetailModal.tsx anchor its own
	// isOverrideActive/getOverrideStatus liveness arithmetic to this snapshot instant instead of
	// the browser's clock, exactly like AdminSubscriptionDetailOutputBody.AsOf already does for
	// SubscriptionOverrideModal. Lives on the response (this struct), not on AdminUserListItem
	// itself — there is one server instant for the whole detail view, not one per embedded item.
	AsOf time.Time `json:"as_of"`
}

// AdminUserFilters contains filter parameters for the admin user list.
type AdminUserFilters struct {
	Search string // Prefix email match (starts-with, min 3 chars, case-insensitive)
	// Tier filters by subscription_tier
	Tier string
	// SortBy is the column to sort by; validated enum: created_at|last_login_at|subscription_tier|subscription_status
	SortBy string
	// SortDir is the sort direction; validated enum: asc|desc
	SortDir string
}

// AdminUserListResponse contains paginated user list results for admin view.
type AdminUserListResponse struct {
	Users      []AdminUserListItem `json:"users"`
	TotalCount int                 `json:"total_count"`
	Page       int                 `json:"page"`
	PageSize   int                 `json:"page_size"`
	TotalPages int                 `json:"total_pages"`
	HasMore    bool                `json:"has_more"`
	// AsOf is the single server instant every row's EffectiveStatus on this page was computed
	// from (Issue #1462 follow-up G2) — a page-level snapshot, not a per-row one:
	// UsersPage.tsx's hasLiveOverride/getOverrideStatus calls (one per row) all anchor to this
	// ONE instant so they cannot disagree with each other, or with the server, due to
	// browser/server clock skew or request latency near an expiry boundary. Deliberately not
	// added to AdminUserListItem — that would be one field per row for a value that is identical
	// across the whole response.
	AsOf time.Time `json:"as_of"`
}
