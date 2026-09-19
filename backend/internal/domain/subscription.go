package domain

import (
	"errors"
	"time"
)

// TierName represents the subscription tier identifier
type TierName string

const (
	TierPremium    TierName = "premium"
	TierHousehold2 TierName = "household_2"
	TierHousehold3 TierName = "household_3"
	TierHousehold4 TierName = "household_4"
	TierHousehold5 TierName = "household_5"
)

// SubscriptionStatus represents the status of a user's subscription
type SubscriptionStatus string

const (
	// SubscriptionStatusActive indicates an active paid subscription
	SubscriptionStatusActive SubscriptionStatus = "active"
	// SubscriptionStatusInactive indicates a frozen account (expired subscription)
	SubscriptionStatusInactive SubscriptionStatus = "inactive"
	// SubscriptionStatusTrial indicates an active trial period
	SubscriptionStatusTrial SubscriptionStatus = "trial"
	// SubscriptionStatusTrialing indicates a paid subscription during trial period (Paddle status)
	SubscriptionStatusTrialing SubscriptionStatus = "trialing"
	// SubscriptionStatusCancelled indicates a cancelled subscription
	SubscriptionStatusCancelled SubscriptionStatus = "cancelled"
	// SubscriptionStatusPastDue indicates payment is overdue
	SubscriptionStatusPastDue SubscriptionStatus = "past_due"
)

// NormalizeSubscriptionStatus maps external Paddle status to domain spelling.
// Maps "paused" and "canceled" (single-l) to "cancelled" (double-l, #933).
// All other values pass through unchanged.
func NormalizeSubscriptionStatus(status string) string {
	switch status {
	case "paused", "canceled":
		return string(SubscriptionStatusCancelled)
	default:
		return status
	}
}

// IsActiveSubscription returns true if the status allows access to the app
func (s SubscriptionStatus) IsActiveSubscription() bool {
	return s == SubscriptionStatusActive || s == SubscriptionStatusTrial || s == SubscriptionStatusTrialing
}

// IsReadOnlySubscription returns true if the status grants read-only access (view data, no mutations)
func (s SubscriptionStatus) IsReadOnlySubscription() bool {
	return s == SubscriptionStatusInactive || s == SubscriptionStatusCancelled || s == SubscriptionStatusPastDue
}

// IsEligibleForPlaidDisconnectRepair returns true if the status is eligible for the retention
// grace-window Plaid-disconnect repair pass (RetentionCleanupCheckWorker.repairPlaidDisconnects,
// backend/internal/queue/jobs/retention_cleanup_check.go) — Issue #1413 round-8 High 4 (Codex
// review).
//
// Deliberately NARROWER than IsReadOnlySubscription() above: 'past_due' IS read-only (that method
// returns true for it), but must NOT be eligible for an IMMEDIATE Plaid-only disconnect here —
// past_due had its own dunning timeline (the deleted past_due_service.go / PastDueConfig.PlaidDisconnectDays,
// #1652) that existed precisely to give a paying-but-behind user time to catch up before Plaid access is
// touched. This repair pass's 72-hour grace window is not a substitute for that longer, dedicated
// dunning schedule; using the wider read-only definition here let an old inactive/cancelled
// candidate that transitioned into a NEW past_due cycle before the per-item re-read bypass past_due's
// own timing entirely.
//
// The SQL twin of this method is profile_repository.go's
// plaidDisconnectRepairEligibleStatusPredicate constant — the two are kept in agreement by
// TestPlaidDisconnectRepairEligibleStatus_Integration_ParityWithGo
// (profile_repository_plaid_disconnect_repair_integration_test.go), mirroring this branch's existing
// precedent for this exact failure class: noActiveManualOverridePredicate /
// domain.User.HasActiveManualOverride(), guarded by
// TestNoActiveManualOverridePredicate_Integration_ParityWithGo.
func (s SubscriptionStatus) IsEligibleForPlaidDisconnectRepair() bool {
	return s == SubscriptionStatusInactive || s == SubscriptionStatusCancelled
}

// SubscriptionTier represents the full definition of a subscription tier
// Loaded from config/subscription_tiers.yaml (centralized config directory)
type SubscriptionTier struct {
	Name        TierName             `yaml:"-" json:"name"`
	DisplayName string               `yaml:"display_name" json:"display_name"`
	Description string               `yaml:"description" json:"description"`
	Limits      SubscriptionLimits   `yaml:"limits" json:"limits"`
	Features    SubscriptionFeatures `yaml:"features" json:"features"`
	Pricing     SubscriptionPricing  `yaml:",inline" json:"pricing"`
}

// SubscriptionLimits defines resource limits for a tier
type SubscriptionLimits struct {
	TransactionHistoryDays int `yaml:"transaction_history_days" json:"transaction_history_days"`
}

// SubscriptionFeatures defines feature flags for a tier
type SubscriptionFeatures struct {
	APIAccess        bool `yaml:"api_access" json:"api_access"`
	PrioritySupport  bool `yaml:"priority_support" json:"priority_support"`
	CustomCategories bool `yaml:"custom_categories" json:"custom_categories"`
	CustomTags       bool `yaml:"custom_tags" json:"custom_tags"` // Issue #345: Tag editing enabled
	AdvancedReports  bool `yaml:"advanced_reports" json:"advanced_reports"`
}

// SubscriptionPricing defines pricing information for a tier
type SubscriptionPricing struct {
	PriceMonthly         float64 `yaml:"price_monthly" json:"price_monthly"`
	PriceYearly          float64 `yaml:"price_yearly" json:"price_yearly"`
	MobilePriceYearly    float64 `yaml:"mobile_price_yearly" json:"mobile_price_yearly"`
	UsersIncluded        int     `yaml:"users_included" json:"users_included"`
	PaddlePriceIDMonthly *string `yaml:"paddle_price_id_monthly" json:"paddle_price_id_monthly,omitempty"`
	PaddlePriceIDYearly  *string `yaml:"paddle_price_id_yearly" json:"paddle_price_id_yearly,omitempty"`
}


// UserSubscriptionOverride represents a custom limit override for a specific user
// Stored in database table: user_subscription_overrides
// Used for: enterprise custom deals, add-on purchases, promotional upgrades
type UserSubscriptionOverride struct {
	ID           int64      `json:"id" db:"id"`
	UserID       string     `json:"user_id" db:"user_id"`
	ResourceType string     `json:"resource_type" db:"resource_type"`     // see ResourceType constants: "transaction_history_days"
	LimitValue   int        `json:"limit_value" db:"limit_value"`         // -1 for unlimited
	Reason       *string    `json:"reason,omitempty" db:"reason"`         // "enterprise_deal", "addon_purchase", "promo"
	ExpiresAt    *time.Time `json:"expires_at,omitempty" db:"expires_at"` // null for permanent
	CreatedAt    time.Time  `json:"created_at" db:"created_at"`
	UpdatedAt    time.Time  `json:"updated_at" db:"updated_at"`
}

// EffectiveSubscription represents the effective limits and features for a user
// Combines base tier + any user-specific overrides
type EffectiveSubscription struct {
	UserID               string               `json:"user_id"`
	Tier                 TierName             `json:"tier"`
	TierDisplayName      string               `json:"tier_display_name"`
	Limits               SubscriptionLimits   `json:"limits"`
	Features             SubscriptionFeatures `json:"features"`
	HasOverrides         bool                 `json:"has_overrides"`
	ActiveOverridesCount int                  `json:"active_overrides_count,omitempty"`
	UsersIncluded        int                  `json:"users_included"`
}

// ResourceType represents the types of resources that can have limits
type ResourceType string

const (
	ResourceTransactionHistoryDays ResourceType = "transaction_history_days"
)

// AllResourceTypes is the canonical list of override-able resource types.
// It must stay in sync with the const block above and the GetLimit/SetLimit switches.
// The admin override endpoint's enum tag is tested against this slice (issue #1288).
var AllResourceTypes = []ResourceType{
	ResourceTransactionHistoryDays,
}

// GetLimit returns the limit value for a specific resource type
func (l *SubscriptionLimits) GetLimit(resourceType ResourceType) int {
	switch resourceType {
	case ResourceTransactionHistoryDays:
		return l.TransactionHistoryDays
	default:
		return 0
	}
}

// SetLimit updates the limit value for a specific resource type
func (l *SubscriptionLimits) SetLimit(resourceType ResourceType, value int) {
	switch resourceType {
	case ResourceTransactionHistoryDays:
		l.TransactionHistoryDays = value
	}
}

// AddOn represents an add-on purchase that modifies tier limits
// Future extensibility for "buy 5 more rules" or "buy faster execution" features
type AddOn struct {
	ID               int64     `json:"id" db:"id"`
	Name             string    `json:"name" db:"name"`
	Type             string    `json:"type" db:"type"`                           // "extra_rules", "faster_execution", "more_accounts"
	ResourceType     string    `json:"resource_type" db:"resource_type"`         // Which resource this affects
	QuantityModifier int       `json:"quantity_modifier" db:"quantity_modifier"` // How much to add/modify
	PriceMonthly     float64   `json:"price_monthly" db:"price_monthly"`
	PaddlePriceID    *string   `json:"paddle_price_id,omitempty" db:"paddle_price_id"`
	IsActive         bool      `json:"is_active" db:"is_active"`
	CreatedAt        time.Time `json:"created_at" db:"created_at"`
	UpdatedAt        time.Time `json:"updated_at" db:"updated_at"`
}

// UserAddOn represents an add-on subscription for a specific user
// Future extensibility for add-on purchases
type UserAddOn struct {
	ID           int64      `json:"id" db:"id"`
	UserID       string     `json:"user_id" db:"user_id"`
	AddOnID      int64      `json:"addon_id" db:"addon_id"`
	Status       string     `json:"status" db:"status"` // "active", "cancelled", "expired"
	SubscribedAt time.Time  `json:"subscribed_at" db:"subscribed_at"`
	CancelledAt  *time.Time `json:"cancelled_at,omitempty" db:"cancelled_at"`
	CreatedAt    time.Time  `json:"created_at" db:"created_at"`
	UpdatedAt    time.Time  `json:"updated_at" db:"updated_at"`
}

// ─── SubscriptionEntitlement ─────────────────────────────────────────────────

// EntitlementSource identifies whether a subscription entitlement originates from
// the user's own subscription or from being covered under a household owner's plan.
type EntitlementSource string

const (
	// EntitlementSourceOwn indicates the user pays for their own subscription.
	EntitlementSourceOwn EntitlementSource = "own_subscription"
	// EntitlementSourceHousehold indicates the user is covered under a household owner's plan.
	EntitlementSourceHousehold EntitlementSource = "household"
)

// SubscriptionEntitlement decouples a user's effective subscription coverage from
// the payer's users row, which is overwritten by Paddle webhooks on billing events.
// One row per entitlement epoch; open-ended rows have EffectiveUntil == nil.
// Phase 2 will enforce the one-active-entitlement invariant and wire this into
// GetEffectiveSubscription().
//
// Issue #1365 — Phase 1: domain struct only; no service wiring yet.
type SubscriptionEntitlement struct {
	ID             string            `json:"id" db:"id"`
	UserID         string            `json:"user_id" db:"user_id"`
	Source         EntitlementSource `json:"source" db:"source"`
	Tier           string            `json:"tier" db:"tier"`
	Status         string            `json:"status" db:"status"`
	PayerUserID    *string           `json:"payer_user_id,omitempty" db:"payer_user_id"`
	HouseholdID    *string           `json:"household_id,omitempty" db:"household_id"`
	EffectiveFrom  time.Time         `json:"effective_from" db:"effective_from"`
	EffectiveUntil *time.Time        `json:"effective_until,omitempty" db:"effective_until"`
	CreatedAt      time.Time         `json:"created_at" db:"created_at"`
	UpdatedAt      time.Time         `json:"updated_at" db:"updated_at"`
}

// Validate checks that the SubscriptionEntitlement has all required fields and valid values.
func (e *SubscriptionEntitlement) Validate() error {
	if e.UserID == "" {
		return errors.New("user_id is required")
	}
	if e.Tier == "" {
		return errors.New("tier is required")
	}
	if e.Status == "" {
		return errors.New("status is required")
	}
	switch SubscriptionStatus(e.Status) {
	case SubscriptionStatusActive, SubscriptionStatusInactive, SubscriptionStatusTrial,
		SubscriptionStatusTrialing, SubscriptionStatusCancelled, SubscriptionStatusPastDue:
	default:
		return errors.New("status must be one of: active, inactive, trial, trialing, cancelled, past_due")
	}
	switch e.Source {
	case EntitlementSourceOwn, EntitlementSourceHousehold:
	default:
		return errors.New("source must be one of: own_subscription, household")
	}
	// Shape check mirrors the DB CHECK constraint.
	if e.Source == EntitlementSourceHousehold {
		if e.HouseholdID == nil || e.PayerUserID == nil {
			return errors.New("household_id and payer_user_id are required when source is household")
		}
	}
	if e.Source == EntitlementSourceOwn {
		if e.HouseholdID != nil || e.PayerUserID != nil {
			return errors.New("household_id and payer_user_id must be nil when source is own_subscription")
		}
	}
	// Mirrors DB CHECK: effective_until > effective_from.
	if e.EffectiveUntil != nil && !e.EffectiveUntil.After(e.EffectiveFrom) {
		return errors.New("effective_until must be after effective_from")
	}
	return nil
}

// EffectiveEntitlement is the resolved coverage for a user: either their own
// subscription, or — live, never stored — the household owner's current tier+status.
// Never persisted; computed fresh on every SubscriptionService.GetEffectiveSubscription call.
type EffectiveEntitlement struct {
	Tier        string
	Status      string
	Source      EntitlementSource
	PayerUserID *string
	HouseholdID *string
}

// HouseholdEntitlementSummary is the response body for GET /api/v1/households/me/entitlement.
type HouseholdEntitlementSummary struct {
	Tier          string            `json:"tier"`
	UsersIncluded int               `json:"users_included"`
	SeatsActive   int               `json:"seats_active"`
	SeatsPending  int               `json:"seats_pending"`
	IsOwner       bool              `json:"is_owner"`
	Source        EntitlementSource `json:"source"`
}
