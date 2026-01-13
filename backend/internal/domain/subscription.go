package domain

import (
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

// Unlimited represents unlimited resources (stored as -1 in config)
const Unlimited = -1

// SubscriptionTier represents the full definition of a subscription tier
// Loaded from config/subscription_tiers.yaml (centralized config directory)
type SubscriptionTier struct {
	Name        TierName                   `yaml:"-" json:"name"`
	DisplayName string                     `yaml:"display_name" json:"display_name"`
	Description string                     `yaml:"description" json:"description"`
	Limits      SubscriptionLimits         `yaml:"limits" json:"limits"`
	Features    SubscriptionFeatures       `yaml:"features" json:"features"`
	Pricing     SubscriptionPricing        `yaml:",inline" json:"pricing"`
}

// SubscriptionLimits defines resource limits for a tier
type SubscriptionLimits struct {
	Accounts               int `yaml:"accounts" json:"accounts"`
	TransactionHistoryDays int `yaml:"transaction_history_days" json:"transaction_history_days"`
	ExecutionsPerDay       int `yaml:"executions_per_day" json:"executions_per_day"`
}

// SubscriptionFeatures defines feature flags for a tier
type SubscriptionFeatures struct {
	AdsEnabled        bool `yaml:"ads_enabled" json:"ads_enabled"`
	AIInsights        bool `yaml:"ai_insights" json:"ai_insights"`
	APIAccess         bool `yaml:"api_access" json:"api_access"`
	PrioritySupport   bool `yaml:"priority_support" json:"priority_support"`
	CustomCategories  bool `yaml:"custom_categories" json:"custom_categories"`
	CustomTags        bool `yaml:"custom_tags" json:"custom_tags"`              // Issue #345: Tag editing enabled
	AdvancedReports   bool `yaml:"advanced_reports" json:"advanced_reports"`
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

// IsUnlimited checks if a limit value represents unlimited resources
func IsUnlimited(value int) bool {
	return value == Unlimited || value == 999999
}

// UserSubscriptionOverride represents a custom limit override for a specific user
// Stored in database table: user_subscription_overrides
// Used for: enterprise custom deals, add-on purchases, promotional upgrades
type UserSubscriptionOverride struct {
	ID           int64      `json:"id" db:"id"`
	UserID       string     `json:"user_id" db:"user_id"`
	ResourceType string     `json:"resource_type" db:"resource_type"` // "accounts", "rules", etc.
	LimitValue   int        `json:"limit_value" db:"limit_value"`     // -1 for unlimited
	Reason       *string    `json:"reason,omitempty" db:"reason"`     // "enterprise_deal", "addon_purchase", "promo"
	ExpiresAt    *time.Time `json:"expires_at,omitempty" db:"expires_at"` // null for permanent
	CreatedAt    time.Time  `json:"created_at" db:"created_at"`
	UpdatedAt    time.Time  `json:"updated_at" db:"updated_at"`
}

// EffectiveSubscription represents the effective limits and features for a user
// Combines base tier + any user-specific overrides
type EffectiveSubscription struct {
	UserID              string               `json:"user_id"`
	Tier                TierName             `json:"tier"`
	TierDisplayName     string               `json:"tier_display_name"`
	Limits              SubscriptionLimits   `json:"limits"`
	Features            SubscriptionFeatures `json:"features"`
	HasOverrides        bool                 `json:"has_overrides"`
	ActiveOverridesCount int                 `json:"active_overrides_count,omitempty"`
}

// ResourceType represents the types of resources that can have limits
type ResourceType string

const (
	ResourceAccounts               ResourceType = "accounts"
	ResourceTransactionHistoryDays ResourceType = "transaction_history_days"
	ResourceExecutionsPerDay       ResourceType = "executions_per_day"
)

// GetLimit returns the limit value for a specific resource type
func (l *SubscriptionLimits) GetLimit(resourceType ResourceType) int {
	switch resourceType {
	case ResourceAccounts:
		return l.Accounts
	case ResourceTransactionHistoryDays:
		return l.TransactionHistoryDays
	case ResourceExecutionsPerDay:
		return l.ExecutionsPerDay
	default:
		return 0
	}
}

// SetLimit updates the limit value for a specific resource type
func (l *SubscriptionLimits) SetLimit(resourceType ResourceType, value int) {
	switch resourceType {
	case ResourceAccounts:
		l.Accounts = value
	case ResourceTransactionHistoryDays:
		l.TransactionHistoryDays = value
	case ResourceExecutionsPerDay:
		l.ExecutionsPerDay = value
	}
}

// AddOn represents an add-on purchase that modifies tier limits
// Future extensibility for "buy 5 more rules" or "buy faster execution" features
type AddOn struct {
	ID               int64      `json:"id" db:"id"`
	Name             string     `json:"name" db:"name"`
	Type             string     `json:"type" db:"type"`               // "extra_rules", "faster_execution", "more_accounts"
	ResourceType     string     `json:"resource_type" db:"resource_type"` // Which resource this affects
	QuantityModifier int        `json:"quantity_modifier" db:"quantity_modifier"` // How much to add/modify
	PriceMonthly     float64    `json:"price_monthly" db:"price_monthly"`
	PaddlePriceID    *string    `json:"paddle_price_id,omitempty" db:"paddle_price_id"`
	IsActive         bool       `json:"is_active" db:"is_active"`
	CreatedAt        time.Time  `json:"created_at" db:"created_at"`
	UpdatedAt        time.Time  `json:"updated_at" db:"updated_at"`
}

// UserAddOn represents an add-on subscription for a specific user
// Future extensibility for add-on purchases
type UserAddOn struct {
	ID             int64      `json:"id" db:"id"`
	UserID         string     `json:"user_id" db:"user_id"`
	AddOnID        int64      `json:"addon_id" db:"addon_id"`
	Status         string     `json:"status" db:"status"` // "active", "cancelled", "expired"
	SubscribedAt   time.Time  `json:"subscribed_at" db:"subscribed_at"`
	CancelledAt    *time.Time `json:"cancelled_at,omitempty" db:"cancelled_at"`
	CreatedAt      time.Time  `json:"created_at" db:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at" db:"updated_at"`
}

// LimitCheckResult represents the result of checking if a user is within limits
type LimitCheckResult struct {
	Allowed       bool   `json:"allowed"`
	CurrentUsage  int    `json:"current_usage"`
	Limit         int    `json:"limit"`
	IsUnlimited   bool   `json:"is_unlimited"`
	ResourceType  string `json:"resource_type"`
	RemainingUsage int   `json:"remaining_usage"`
}

// NewLimitCheckResult creates a new limit check result
func NewLimitCheckResult(resourceType ResourceType, currentUsage, limit int) *LimitCheckResult {
	isUnlimited := IsUnlimited(limit)
	var remaining int
	var allowed bool

	if isUnlimited {
		remaining = -1
		allowed = true
	} else {
		remaining = limit - currentUsage
		allowed = currentUsage < limit
	}

	return &LimitCheckResult{
		Allowed:        allowed,
		CurrentUsage:   currentUsage,
		Limit:          limit,
		IsUnlimited:    isUnlimited,
		ResourceType:   string(resourceType),
		RemainingUsage: remaining,
	}
}
