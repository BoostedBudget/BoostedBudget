package domain

import (
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/maxnussbaum/BoostedBudget/internal/pfc"
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
	ConditionDateRange          ConditionType = "date_range"

	// ConditionExistingPFCPrimary matches against the Plaid PFC primary enum (e.g. "FOOD_AND_DRINK").
	ConditionExistingPFCPrimary ConditionType = "existing_pfc_primary"
	// ConditionExistingPFCDetailed matches against the Plaid PFC detailed enum (e.g. "FOOD_AND_DRINK_FAST_FOOD").
	ConditionExistingPFCDetailed ConditionType = "existing_pfc_detailed"
)

// validConditionTypes is a set of all valid condition types.
var validConditionTypes = map[ConditionType]bool{
	ConditionMerchantContains:    true,
	ConditionMerchantEquals:      true,
	ConditionMerchantStartsWith:  true,
	ConditionMerchantEndsWith:    true,
	ConditionAmountGreaterThan:   true,
	ConditionAmountLessThan:      true,
	ConditionAmountEquals:        true,
	ConditionAmountRange:         true,
	ConditionAccountID:           true,
	ConditionAccountType:         true,
	ConditionPaymentChannel:      true,
	ConditionDateRange:           true,
	ConditionExistingPFCPrimary:  true,
	ConditionExistingPFCDetailed: true,
}

// IsValid checks if the condition type is valid.
func (ct ConditionType) IsValid() bool {
	return validConditionTypes[ct]
}

// validatePFCValue validates a PFC value against the curated registry.
func validatePFCValue(val string, isDetailed bool) error {
	if val == "" {
		return errors.New("PFC value cannot be empty")
	}
	initPFCValidationSets()
	if isDetailed {
		if _, known := validPFCDetaileds[val]; !known {
			return fmt.Errorf("unknown PFC detailed enum: %s", val)
		}
	} else {
		if _, known := validPFCPrimaries[val]; !known {
			return fmt.Errorf("unknown PFC primary enum: %s", val)
		}
	}
	return nil
}

// RuleCondition represents a single condition within a categorization rule.
//
// #1625: conditions are a {Field, Operator, Value} triple. The fourteen legacy FUSED Type names
// remain accepted on input and are still emitted on output (see ConditionTypeExpression for the
// combinations that have no fused ancestor), so persisted rows and older clients keep working
// without a data migration. Normalize() maps between the two shapes.
type RuleCondition struct {
	// Type is the legacy fused discriminator. On input it is an alias for a (Field, Operator)
	// pair; on output it is DERIVED from that pair. Sending both is fine as long as they agree —
	// a contradiction is ErrConditionShapeConflict rather than a silent choice between them.
	Type ConditionType `json:"type,omitempty" enum:"merchant_contains,merchant_equals,merchant_starts_with,merchant_ends_with,amount_greater_than,amount_less_than,amount_equals,amount_range,account_id,account_type,payment_channel,date_range,existing_pfc_primary,existing_pfc_detailed,expression" doc:"Legacy fused condition name. Send this OR field+operator, not a contradicting pair. 'expression' is RESPONSE-ONLY — the server emits it for a field+operator combination that has no legacy name, and rejects it as input; send field+operator instead."`

	// Field is the transaction attribute to match. See FieldSpecs for the field × operator matrix.
	Field RuleConditionField `json:"field,omitempty" enum:"merchant,name,merchant_id,amount,signed_amount,direction,account_id,account_type,payment_channel,date,pfc_primary,pfc_detailed" doc:"Transaction attribute to match. Not every operator is legal for every field — GET /api/v1/auto-categorization/condition-schema returns the full matrix, value shapes and semantics. Note 'amount' discards the sign; 'signed_amount' is POSITIVE for money leaving the account; 'direction' takes inflow|outflow."`

	// Operator is the comparison to apply. Before #1625 this field was declared, serialized into
	// the public schema, and then never validated or read by ANY evaluator — which is precisely
	// why callers guessed at it. It is now load-bearing.
	Operator string `json:"operator,omitempty" enum:"eq,neq,gt,gte,lt,lte,between,contains,starts_with,ends_with,in" doc:"Comparison to apply. Legal operators depend on the field — see GET /api/v1/auto-categorization/condition-schema. 'between' takes an object ({min,max} for numbers, {start,end} for dates); 'in' takes an array, or a single string as a one-element shorthand."`

	// Value is a string, an array of strings ("any of"), a number, or a bounded range object,
	// depending on the field and operator. See FieldSpecs.
	Value interface{} `json:"value"`

	// ValueDisplay is populated by PopulatePFCDisplay for PFC-typed conditions.
	ValueDisplay string `json:"value_display,omitempty"`
}

// CategorizationRule represents a user-defined rule for automatic transaction categorization.
// Rules are subject to subscription tier limits (Issue #346).
type CategorizationRule struct {
	ID                     string          `json:"id" db:"id"`
	UserID                 string          `json:"user_id" db:"user_id"`
	Name                   string          `json:"name" db:"name"`
	Description            *string         `json:"description,omitempty" db:"description"`
	Enabled                bool            `json:"enabled" db:"enabled"`
	Priority               int             `json:"priority" db:"priority"` // Higher = evaluated first
	Conditions             []RuleCondition `json:"conditions" db:"conditions"`
	ConditionLogic         string          `json:"condition_logic" db:"condition_logic"` // "AND" or "OR"
	TargetKind             *string         `json:"target_kind,omitempty" db:"target_kind"`
	TargetPFCPrimary       *string         `json:"target_pfc_primary,omitempty" db:"target_pfc_primary"`
	TargetPFCDetailed      *string         `json:"target_pfc_detailed,omitempty" db:"target_pfc_detailed"`
	TargetCustomCategoryID *string         `json:"target_custom_category_id,omitempty" db:"target_custom_category_id"`

	// PFC display names (populated by PopulatePFCDisplay, not stored in DB)
	TargetPFCPrimaryDisplay  *string `json:"target_pfc_primary_display,omitempty" db:"-"`
	TargetPFCDetailedDisplay *string `json:"target_pfc_detailed_display,omitempty" db:"-"`
	// Custom category display name (populated by service enrichment from custom_categories table, not stored in DB)
	TargetCustomCategoryDisplay *string    `json:"target_custom_category_display,omitempty" db:"-"`

	// Quarantined reports that this rule cannot be evaluated as written. RESPONSE ONLY, computed
	// at read time from the rule's own conditions and never persisted -- a stored copy could only
	// go stale relative to the conditions it describes.
	//
	// #1625: shadow-mode quarantine counted and logged, so the only party who could discover that
	// a rule was not going to run was an operator reading /metrics. The user whose rule silently
	// stopped matching could not. Surfacing this is a PRECONDITION for enforcing it.
	//
	// Deliberately no `quarantined_at`: nothing records when a rule entered this state, and a
	// timestamp filled in at response time would answer "when did you ask", not "since when has
	// this been broken". Reporting a number the system does not know is the defect class this
	// whole ticket is about.
	Quarantined bool `json:"quarantined,omitempty" db:"-" doc:"True when this rule cannot be evaluated as written and will not match anything. Computed per request."`
	// QuarantineReason is a fixed vocabulary (never user content), shared with the
	// categorization_rule_quarantine_candidates_total metric so the two cannot describe the same
	// condition differently.
	QuarantineReason string `json:"quarantine_reason,omitempty" db:"-" doc:"Why the rule cannot be evaluated. Fixed vocabulary: no_conditions_matches_everything, invalid_condition_logic_defaults_to_and, unnormalizable_condition, invalid_condition_value_shape."`
	TargetTags                  []string   `json:"target_tags,omitempty" db:"target_tags"`
	TagBehavior                 string     `json:"tag_behavior" db:"tag_behavior"` // "add" or "replace"
	Source                      string     `json:"source" db:"source"`             // "manual", "ai", or "template"
	MatchCount                  int64      `json:"match_count" db:"match_count"`
	OverrideCount               int64      `json:"override_count" db:"override_count"`
	LastMatchedAt               *time.Time `json:"last_matched_at,omitempty" db:"last_matched_at"`
	CreatedAt                   time.Time  `json:"created_at" db:"created_at"`
	UpdatedAt                   time.Time  `json:"updated_at" db:"updated_at"`
}

// pfcValidationSets holds the valid PFC enum sets built once from the curated registry.
var (
	pfcOnce           sync.Once
	validPFCPrimaries map[string]struct{}
	validPFCDetaileds map[string]struct{}
)

func initPFCValidationSets() {
	pfcOnce.Do(func() {
		validPFCPrimaries = make(map[string]struct{}, 18)
		for _, p := range pfc.AllPrimaries() {
			validPFCPrimaries[p.Enum] = struct{}{}
		}
		validPFCDetaileds = make(map[string]struct{})
		for _, d := range pfc.AllDetailed() {
			validPFCDetaileds[d.Enum] = struct{}{}
		}
	})
}

// Validate checks that the CategorizationRule has all required fields and valid values.
func (r *CategorizationRule) Validate() error {
	if r.UserID == "" {
		return fmt.Errorf("%w: user_id is required", ErrInvalidRule)
	}

	if r.Name == "" {
		return fmt.Errorf("%w: name is required", ErrInvalidRule)
	}

	if len(r.Name) > 100 {
		return fmt.Errorf("%w: name must be 100 characters or less", ErrInvalidRule)
	}

	// Validate conditions
	if len(r.Conditions) == 0 {
		return fmt.Errorf("%w: at least one condition is required", ErrInvalidRule)
	}

	// #1625: normalize each condition into its canonical (Field, Operator) pair, then check the
	// Value against the field × operator × shape matrix. Previously only `type` membership was
	// checked and Value was never shape-checked for non-PFC types, so a rule with e.g. a string
	// amount_range persisted cleanly and then silently never matched.
	for i := range r.Conditions {
		if err := r.Conditions[i].Normalize(); err != nil {
			return err
		}
		if err := r.Conditions[i].ValidateShape(); err != nil {
			return err
		}
	}

	// Validate condition logic
	if r.ConditionLogic != "AND" && r.ConditionLogic != "OR" {
		return fmt.Errorf("%w: condition_logic must be AND or OR", ErrInvalidRule)
	}

	// Validate that at least one action is set
	if !r.HasCategoryAction() && !r.HasTagAction() {
		return fmt.Errorf("%w: rule must have at least one action (category or tags)", ErrInvalidRule)
	}

	if r.TargetKind != nil {
		switch *r.TargetKind {
		case "pfc":
			if r.TargetPFCPrimary == nil || *r.TargetPFCPrimary == "" {
				return fmt.Errorf("%w: target_pfc_primary is required when target_kind is pfc", ErrInvalidRule)
			}
			if err := validatePFCValue(*r.TargetPFCPrimary, false); err != nil {
				return fmt.Errorf("%w: target_pfc_primary: %s", ErrInvalidRule, err.Error())
			}
			if r.TargetPFCDetailed != nil {
				if err := validatePFCValue(*r.TargetPFCDetailed, true); err != nil {
					return fmt.Errorf("%w: target_pfc_detailed: %s", ErrInvalidRule, err.Error())
				}
			}
			if r.TargetCustomCategoryID != nil {
				return fmt.Errorf("%w: target_custom_category_id must not be set when target_kind is pfc", ErrInvalidRule)
			}
		case "custom":
			if r.TargetCustomCategoryID == nil || *r.TargetCustomCategoryID == "" {
				return fmt.Errorf("%w: target_custom_category_id is required when target_kind is custom", ErrInvalidRule)
			}
			if r.TargetPFCPrimary != nil || r.TargetPFCDetailed != nil {
				return fmt.Errorf("%w: target_pfc_primary and target_pfc_detailed must not be set when target_kind is custom", ErrInvalidRule)
			}
		case "clear":
			if r.TargetPFCPrimary != nil || r.TargetPFCDetailed != nil || r.TargetCustomCategoryID != nil {
				return fmt.Errorf("%w: clear target_kind must not include target category fields", ErrInvalidRule)
			}
		default:
			return fmt.Errorf("%w: target_kind must be one of pfc, custom, or clear", ErrInvalidRule)
		}
	} else if r.TargetPFCPrimary != nil || r.TargetPFCDetailed != nil || r.TargetCustomCategoryID != nil {
		return fmt.Errorf("%w: target category fields require target_kind", ErrInvalidRule)
	}

	// Validate tag behavior
	if r.TagBehavior != "add" && r.TagBehavior != "replace" {
		return fmt.Errorf("%w: tag_behavior must be add or replace", ErrInvalidRule)
	}

	// Validate source
	if r.Source != "manual" && r.Source != "ai" && r.Source != "template" {
		return fmt.Errorf("%w: source must be manual, ai, or template", ErrInvalidRule)
	}

	return nil
}

// HasCategoryAction returns true if the rule has a category action.
func (r *CategorizationRule) HasCategoryAction() bool {
	return r.TargetKind != nil
}

// IsClearCategoryAction returns true if the rule clears the category override.
func (r *CategorizationRule) IsClearCategoryAction() bool {
	return r.TargetKind != nil && *r.TargetKind == "clear"
}

// HasTagAction returns true if the rule adds tags.
func (r *CategorizationRule) HasTagAction() bool {
	return len(r.TargetTags) > 0
}

// CreateCategorizationRuleRequest represents the request to create a new categorization rule.
type CreateCategorizationRuleRequest struct {
	Name                   string          `json:"name" validate:"required,max=100"`
	Description            *string         `json:"description,omitempty"`
	Priority               int             `json:"priority,omitempty"`
	Conditions             []RuleCondition `json:"conditions" validate:"required,min=1"`
	ConditionLogic         string          `json:"condition_logic,omitempty" validate:"omitempty,oneof=AND OR"`
	TargetKind             *string         `json:"target_kind,omitempty"`
	TargetPFCPrimary       *string         `json:"target_pfc_primary,omitempty"`
	TargetPFCDetailed      *string         `json:"target_pfc_detailed,omitempty"`
	TargetCustomCategoryID *string         `json:"target_custom_category_id,omitempty" validate:"omitempty,uuid"`
	TargetTags             []string        `json:"target_tags,omitempty"`
	TagBehavior            string          `json:"tag_behavior,omitempty" validate:"omitempty,oneof=add replace"`
}

// UpdateCategorizationRuleRequest represents the request to update a categorization rule.
type UpdateCategorizationRuleRequest struct {
	Name                   *string          `json:"name,omitempty" validate:"omitempty,max=100"`
	Description            *string          `json:"description,omitempty"`
	Enabled                *bool            `json:"enabled,omitempty"`
	Priority               *int             `json:"priority,omitempty"`
	Conditions             *[]RuleCondition `json:"conditions,omitempty" validate:"omitempty,min=1"`
	ConditionLogic         *string          `json:"condition_logic,omitempty" validate:"omitempty,oneof=AND OR"`
	TargetKind             *string          `json:"target_kind,omitempty"`
	TargetPFCPrimary       *string          `json:"target_pfc_primary,omitempty"`
	TargetPFCDetailed      *string          `json:"target_pfc_detailed,omitempty"`
	TargetCustomCategoryID *string          `json:"target_custom_category_id,omitempty" validate:"omitempty,uuid"`
	TargetTags             *[]string        `json:"target_tags,omitempty"`
	TagBehavior            *string          `json:"tag_behavior,omitempty" validate:"omitempty,oneof=add replace"`
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

// RulePreviewResult is the outcome of a dry run (#1625).
type RulePreviewResult struct {
	// ScannedCount is how many recent transactions were examined — needed to read MatchCount
	// honestly, since a preview is bounded rather than scanning all history.
	ScannedCount int                `json:"scanned_count"`
	MatchCount   int                `json:"match_count"`
	Matches      []RulePreviewMatch `json:"matches"`
}

// RulePreviewMatch is one transaction a candidate rule would match.
//
// ⚠ Every field except TransactionID is user financial data — merchant, raw description, amount.
// Any UI rendering these must mask them for session replay (data-ph-mask).
type RulePreviewMatch struct {
	TransactionID   string    `json:"transaction_id"`
	Date            time.Time `json:"date"`
	Name            string    `json:"name"`
	MerchantName    *string   `json:"merchant_name,omitempty"`
	Amount          float64   `json:"amount"`
	CurrentCategory string    `json:"current_category"`
	WouldAddTags    []string  `json:"would_add_tags,omitempty"`
}
