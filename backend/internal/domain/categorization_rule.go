package domain

import (
	"errors"
	"time"
)

// ConditionType represents the type of condition for rule matching.
type ConditionType string

// Supported condition types for categorization rules.
const (
	ConditionMerchantContains   ConditionType = "merchant_contains"
	ConditionMerchantEquals     ConditionType = "merchant_equals"
	ConditionMerchantStartsWith ConditionType = "merchant_starts_with"
	ConditionMerchantEndsWith   ConditionType = "merchant_ends_with"
	ConditionAmountGreaterThan  ConditionType = "amount_greater_than"
	ConditionAmountLessThan     ConditionType = "amount_less_than"
	ConditionAmountEquals       ConditionType = "amount_equals"
	ConditionAmountRange        ConditionType = "amount_range"
	ConditionAccountID          ConditionType = "account_id"
	ConditionAccountType        ConditionType = "account_type"
	ConditionPaymentChannel     ConditionType = "payment_channel"
	ConditionExistingCategory   ConditionType = "existing_category"
	ConditionDateRange          ConditionType = "date_range"
)

// validConditionTypes is a set of all valid condition types.
var validConditionTypes = map[ConditionType]bool{
	ConditionMerchantContains:   true,
	ConditionMerchantEquals:     true,
	ConditionMerchantStartsWith: true,
	ConditionMerchantEndsWith:   true,
	ConditionAmountGreaterThan:  true,
	ConditionAmountLessThan:     true,
	ConditionAmountEquals:       true,
	ConditionAmountRange:        true,
	ConditionAccountID:          true,
	ConditionAccountType:        true,
	ConditionPaymentChannel:     true,
	ConditionExistingCategory:   true,
	ConditionDateRange:          true,
}

// IsValid checks if the condition type is valid.
func (ct ConditionType) IsValid() bool {
	return validConditionTypes[ct]
}

// RuleCondition represents a single condition within a categorization rule.
type RuleCondition struct {
	Type     ConditionType `json:"type"`
	Value    interface{}   `json:"value"`              // string, number, or object depending on type
	Operator string        `json:"operator,omitempty"` // for amount comparisons
}

// CategorizationRule represents a user-defined rule for automatic transaction categorization.
// Rules are subject to subscription tier limits (Issue #346).
type CategorizationRule struct {
	ID             string          `json:"id" db:"id"`
	UserID         string          `json:"user_id" db:"user_id"`
	Name           string          `json:"name" db:"name"`
	Description    *string         `json:"description,omitempty" db:"description"`
	Enabled        bool            `json:"enabled" db:"enabled"`
	Priority       int             `json:"priority" db:"priority"` // Higher = evaluated first
	Conditions     []RuleCondition `json:"conditions" db:"conditions"`
	ConditionLogic string          `json:"condition_logic" db:"condition_logic"` // "AND" or "OR"
	TargetCategory []string        `json:"target_category,omitempty" db:"target_category"`
	TargetTags     []string        `json:"target_tags,omitempty" db:"target_tags"`
	TagBehavior    string          `json:"tag_behavior" db:"tag_behavior"` // "add" or "replace"
	Source         string          `json:"source" db:"source"`             // "manual", "ai", or "template"
	MatchCount     int64           `json:"match_count" db:"match_count"`
	OverrideCount  int64           `json:"override_count" db:"override_count"`
	LastMatchedAt  *time.Time      `json:"last_matched_at,omitempty" db:"last_matched_at"`
	CreatedAt      time.Time       `json:"created_at" db:"created_at"`
	UpdatedAt      time.Time       `json:"updated_at" db:"updated_at"`
}

// Validate checks that the CategorizationRule has all required fields and valid values.
func (r *CategorizationRule) Validate() error {
	if r.UserID == "" {
		return errors.New("user_id is required")
	}

	if r.Name == "" {
		return errors.New("name is required")
	}

	if len(r.Name) > 100 {
		return errors.New("name must be 100 characters or less")
	}

	// Validate conditions
	if len(r.Conditions) == 0 {
		return errors.New("at least one condition is required")
	}

	for _, cond := range r.Conditions {
		if !cond.Type.IsValid() {
			return errors.New("invalid condition type: " + string(cond.Type))
		}
	}

	// Validate condition logic
	if r.ConditionLogic != "AND" && r.ConditionLogic != "OR" {
		return errors.New("condition_logic must be AND or OR")
	}

	// Validate that at least one action is set
	if !r.HasCategoryAction() && !r.HasTagAction() {
		return errors.New("rule must have at least one action (category or tags)")
	}

	// Validate tag behavior
	if r.TagBehavior != "add" && r.TagBehavior != "replace" {
		return errors.New("tag_behavior must be add or replace")
	}

	// Validate source
	if r.Source != "manual" && r.Source != "ai" && r.Source != "template" {
		return errors.New("source must be manual, ai, or template")
	}

	return nil
}

// HasCategoryAction returns true if the rule sets a category.
func (r *CategorizationRule) HasCategoryAction() bool {
	return len(r.TargetCategory) > 0
}

// HasTagAction returns true if the rule adds tags.
func (r *CategorizationRule) HasTagAction() bool {
	return len(r.TargetTags) > 0
}

// CreateCategorizationRuleRequest represents the request to create a new categorization rule.
type CreateCategorizationRuleRequest struct {
	Name           string          `json:"name" validate:"required,max=100"`
	Description    *string         `json:"description,omitempty"`
	Priority       int             `json:"priority,omitempty"`
	Conditions     []RuleCondition `json:"conditions" validate:"required,min=1"`
	ConditionLogic string          `json:"condition_logic,omitempty" validate:"omitempty,oneof=AND OR"`
	TargetCategory []string        `json:"target_category,omitempty"`
	TargetTags     []string        `json:"target_tags,omitempty"`
	TagBehavior    string          `json:"tag_behavior,omitempty" validate:"omitempty,oneof=add replace"`
}

// UpdateCategorizationRuleRequest represents the request to update a categorization rule.
type UpdateCategorizationRuleRequest struct {
	Name           *string          `json:"name,omitempty" validate:"omitempty,max=100"`
	Description    *string          `json:"description,omitempty"`
	Enabled        *bool            `json:"enabled,omitempty"`
	Priority       *int             `json:"priority,omitempty"`
	Conditions     *[]RuleCondition `json:"conditions,omitempty" validate:"omitempty,min=1"`
	ConditionLogic *string          `json:"condition_logic,omitempty" validate:"omitempty,oneof=AND OR"`
	TargetCategory *[]string        `json:"target_category,omitempty"`
	TargetTags     *[]string        `json:"target_tags,omitempty"`
	TagBehavior    *string          `json:"tag_behavior,omitempty" validate:"omitempty,oneof=add replace"`
}

// ToggleCategorizationRuleRequest represents the request to enable/disable a rule.
type ToggleCategorizationRuleRequest struct {
	Enabled bool `json:"enabled"`
}

// TestCategorizationRuleRequest represents the request to test a rule against transactions.
type TestCategorizationRuleRequest struct {
	Limit int `json:"limit,omitempty" validate:"omitempty,min=1,max=100"`
}

// Categorization rule-related errors
var (
	ErrRuleNotFound      = errors.New("categorization rule not found")
	ErrRuleAlreadyExists = errors.New("categorization rule with this name already exists")
	ErrRuleLimitExceeded = errors.New("categorization rule limit exceeded for subscription tier")
)
