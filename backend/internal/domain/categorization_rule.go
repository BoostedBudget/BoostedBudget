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

	// ConditionExistingPFCPrimary matches the Plaid PFC primary enum (e.g. "FOOD_AND_DRINK") as the
	// bank ORIGINALLY reported it — the raw provider value, before any override. This is NOT the
	// effective category shown in the app: a rule's own action can never rewrite what it matches.
	ConditionExistingPFCPrimary ConditionType = "existing_pfc_primary"
	// ConditionExistingPFCDetailed matches the Plaid PFC detailed enum (e.g.
	// "FOOD_AND_DRINK_FAST_FOOD") as the bank ORIGINALLY reported it — the raw provider value,
	// before any override. This is NOT the effective category shown in the app: a rule's own
	// action can never rewrite what it matches.
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
	Field RuleConditionField `json:"field,omitempty" enum:"merchant,effective_merchant,name,merchant_id,amount,signed_amount,direction,account_id,account_type,payment_channel,date,pfc_primary,pfc_detailed" doc:"Transaction attribute to match. Not every operator is legal for every field — GET /api/v1/auto-categorization/condition-schema returns the full matrix, value shapes and semantics. Note 'amount' discards the sign; 'signed_amount' is POSITIVE for money leaving the account; 'direction' takes inflow|outflow."`

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
	TargetCustomCategoryDisplay *string `json:"target_custom_category_display,omitempty" db:"-"`

	// Quarantined reports that this rule cannot be evaluated as written. RESPONSE ONLY, computed
	// at read time from the rule's own conditions and never persisted -- a stored copy could only
	// go stale relative to the conditions it describes.
	//
	// #1625: shadow-mode quarantine counted and logged, so the only party who could discover that
	// a rule was not going to run was an operator reading /metrics. The user whose rule silently
	// stopped matching could not. Surfacing this is a PRECONDITION for enforcing it.
	//
	// ⚠ THREE STATES, and every collapse of them has shipped at least once.
	//
	// #1625 second fix wave H2/M3 settled the semantics ONCE, and every surface now derives from
	// it: `quarantined` means "the server is CURRENTLY preventing this rule from matching
	// anything". It is deliberately switch-dependent — with RULE_QUARANTINE_ENFORCE off the
	// engine runs enforcement-class rules, so reporting quarantined=true would be a false claim
	// about the user's own ledger. See internal/services/quarantine_published_contract_test.go for
	// the rejected alternative and why.
	//
	// The cost of that choice, and the reason the previous version of this comment was WRONG:
	// `quarantined=false` does NOT mean "repaired". It means "quarantine is not what is stopping
	// this rule right now" — either the problem is one read-path coercion repairs
	// (invalid_condition_value_shape, the only reason ordinary API-created rules commonly hold),
	// or enforcement is off and the rule is running as written, in which case a zero-condition
	// rule matches EVERY transaction. Pinned by TestQuarantineFalseDoesNotMeanRepaired.
	//
	// ⚠ #1625 final review I4: this pair says NOTHING about `enabled`, and annotateQuarantineStatus
	// never reads it. A DISABLED rule is stopped by RuleMatches' first line, before quarantine is
	// consulted at all. Both doc tags now say so, because they are published to MCP and an agent
	// reading "it IS still matching" off a disabled rule would be told something false.
	//
	// Deliberately no `quarantined_at`: nothing records when a rule entered this state, and a
	// timestamp filled in at response time would answer "when did you ask", not "since when has
	// this been broken". Reporting a number the system does not know is the defect class this
	// whole ticket is about.
	Quarantined bool `json:"quarantined,omitempty" db:"-" doc:"True when the server is CURRENTLY preventing this rule from matching anything: it is refused before any condition is evaluated, so it applies to nothing. This is a statement about right now, not about the rule's shape. It is FALSE whenever quarantine enforcement is disabled server-wide (RULE_QUARANTINE_ENFORCE, default on), because the engine then runs the rule despite its problem. So false WITH a quarantine_reason does NOT mean the rule is healthy or that the server repaired it — it means quarantine is not what is stopping it: either the problem is one the server repairs at read time (a wrong-typed condition value with one honest reading is coerced), or enforcement is off and the rule is running exactly as written, in which case a rule with no conditions matches EVERY transaction. ⚠ This field says nothing about enabled: a disabled rule matches nothing regardless, because the engine returns early on the enabled flag before quarantine is consulted. Read enabled AND NOT quarantined for the question is the server letting this rule run — which is not the same as is it matching, because a condition the engine cannot evaluate still fails at evaluation time. Computed per request; never persisted."`
	// QuarantineReason is a fixed vocabulary (never user content), shared with the
	// categorization_rule_quarantine_candidates_total metric so the two cannot describe the same
	// condition differently.
	QuarantineReason string     `json:"quarantine_reason,omitempty" db:"-" doc:"Why the rule is broken. Fixed vocabulary, never user content. Four values are in the ENFORCEMENT CLASS — the rule cannot be evaluated honestly as written, and while quarantine enforcement is on (RULE_QUARANTINE_ENFORCE, default on) the server refuses it and quarantined is true: no_conditions_matches_everything, invalid_condition_logic_defaults_to_and, unnormalizable_condition, uncoercible_condition_value. With enforcement OFF those same rules RUN and quarantined is false — nothing about them was repaired. The fifth, invalid_condition_value_shape, is never enforced in either state: a condition value of the wrong type whose intended reading is unambiguous, coerced at read time only, so the rule keeps running. Read quarantined, not this field, for whether the server is currently stopping the rule — and read enabled too, which both fields ignore entirely. The stored rule is never rewritten; edit it to make the repair permanent."`
	TargetTags       []string   `json:"target_tags,omitempty" db:"target_tags"`
	TagBehavior      string     `json:"tag_behavior" db:"tag_behavior"` // "add" or "replace"
	Source           string     `json:"source" db:"source"`             // "manual", "ai", or "template"
	MatchCount       int64      `json:"match_count" db:"match_count"`
	OverrideCount    int64      `json:"override_count" db:"override_count"`
	LastMatchedAt    *time.Time `json:"last_matched_at,omitempty" db:"last_matched_at"`
	CreatedAt        time.Time  `json:"created_at" db:"created_at"`
	UpdatedAt        time.Time  `json:"updated_at" db:"updated_at"`

	// Version is the optimistic-concurrency token for USER-facing updates (#1625 final review B2,
	// migration 000192).
	//
	// ⚠ It is NOT "how many times this row was written". Task 19 originally keyed the lock on
	// UpdatedAt, and that was wrong for exactly that reason: a BEFORE UPDATE trigger stamps
	// updated_at on every write, and IncrementMatchCount is a write — engine bookkeeping during an
	// ordinary sync, no user involved. Racing one produced "This rule was changed by another
	// request" for a change nobody made, and the more transactions a rule matched the likelier it
	// got. The test a write must pass is "would the user SEE this change if they were looking at
	// the rule?" — so a competing visible change conflicts and a match count does not.
	//
	// ⚠ #1625 third fix wave F1: this said Version "moves only when Update runs". It has had TWO
	// production writers since the second fix wave's H1 fix:
	// repository.categorizationRuleRepository.Update (UpdateRule, ToggleRule) and
	// repository.customCategoryRepository's category tombstone, which disables the rules that
	// targeted the deleted category — a change the user sees as the card flipping to "Disabled".
	// Do not add a third by matching that list; add one by satisfying the test above.
	//
	// Response-visible so a client can round-trip it, but the server never trusts a client-supplied
	// value: Update reads the version off the copy the service itself loaded via GetByID.
	Version int64 `json:"version" db:"version" doc:"Optimistic-concurrency token. Incremented by user-facing updates only; background bookkeeping such as the match counter deliberately does not move it, so an ordinary sync cannot make your save look like a conflict."`
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

// Validate checks that the CategorizationRule has all required fields and valid values. Every
// condition is judged, which is what a CREATE (or an update that rewrites the conditions) needs:
// all of the data is data this request is writing.
func (r *CategorizationRule) Validate() error { return r.validate(true) }

// ValidateForUpdate is Validate with ONE difference: when the request supplied no `conditions`, the
// stored conditions are not re-judged against the shape matrix.
//
// #1625 final review B1. Validating the MERGED rule made every legacy rule permanently
// un-editable. `{amount_greater_than, "1000"}` is not hypothetical — dedac0aa (#346) checked the
// condition TYPE and nothing about the value, so wrong-typed values have persisted through the
// ordinary public API for the whole life of the feature; condition_coercion.go REPAIRS them at read
// time precisely so those rules keep running, and the rule card tells the user to "edit the rule to
// confirm that is what you meant". Re-judging them on an unrelated PATCH turned that advice into a
// 422 on `{"name": "..."}` — the same "matchable but permanently un-updatable" failure
// condition_schema.go's textValues comment already refuses to create one function over. (ToggleRule
// escaped only because it skipped Validate entirely.)
//
// ⚠ This is NOT "the update path is lenient". A condition the request SUPPLIES is new data and is
// checked exactly as strictly as on create — `conditionsSupplied` is the caller's `req.Conditions
// != nil`, the same expression that gates the merge. A user renaming a rule is not asked to first
// repair data the server wrote for them. Everything else (name, logic, actions, targets) is
// validated in both modes, because those ARE re-derived from the merged rule on every update.
//
// ⚠ "As strictly as on create" is exact, and #1625 second fix wave L7 corrected the stronger
// claim that used to sit here ("a user submitting a bad shape is still refused"). CREATE is not
// total either: condition_schema.go's textValues accepts a MIXED array and silently drops its
// non-string members, so `["uber", 42]` is accepted on both paths and evaluates as `["uber"]`.
// That hole is deliberate and documented at textValues — it mirrors
// matching.ExtractStringValues, which is what actually runs — but it means the guarantee here is
// "the update path is no weaker than the create path", not "malformed conditions cannot be
// written".
//
// Normalize is skipped with the shape check rather than kept, deliberately: an unnormalizable
// stored condition is the same class of un-editable rule, and the repository already normalizes
// what it loads (normalizeLoadedConditions), so nothing downstream depends on this call site.
func (r *CategorizationRule) ValidateForUpdate(conditionsSupplied bool) error {
	return r.validate(conditionsSupplied)
}

func (r *CategorizationRule) validate(checkConditions bool) error {
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
	if checkConditions {
		for i := range r.Conditions {
			if err := r.Conditions[i].Normalize(); err != nil {
				return err
			}
			if err := r.Conditions[i].ValidateShape(); err != nil {
				return err
			}
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

	// ErrRuleConcurrentlyModified is returned when a read-modify-write (UpdateRule or ToggleRule)
	// loses an optimistic-concurrency race: the row's `version` no longer matches the value this
	// write observed when it read the rule, because another USER-FACING write moved it in between.
	//
	// ⚠ It is keyed on Version, NOT UpdatedAt — see that field's comment. Keying on updated_at
	// (as Task 19 originally did) reported this to users for changes they did not make, because a
	// BEFORE UPDATE trigger stamps updated_at on background bookkeeping too.
	// #1625 Task 19: without this, ToggleRule and UpdateRule both read-modify-write the WHOLE row,
	// so a toggle racing a repair could write the repair's own PRIOR (malformed) definition back
	// over it — last-writer-wins across every column, not just the one either caller meant to
	// change. It is a PRECONDITION failure, not a fault: the honest answer is 409 and "read it
	// again, then retry" — same shape as domain.ErrSavingsGoalBasisConflict. It must never
	// surface as a 500 (see internal/huma/errors/mapper.go).
	ErrRuleConcurrentlyModified = errors.New("categorization rule was modified concurrently")
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
	TransactionID string `json:"transaction_id"`
	// Date is the matched transaction UTC calendar day (YYYY-MM-DD), formatted at the one place
	// this type is built (rulePreviewService.Preview).
	//
	// A plain string rather than a time.Time, unlike Transaction.Date which needs the split: this
	// is a pure read-only view type with no bind param, scan target or cursor behind it, so there
	// is nothing for a time.Time to serve. transactions.date is a DATE column, and rendering it
	// through a timestamp shifts it back a day west of UTC (#1631; see domain/date_display.go).
	Date            string   `json:"date" format:"date" doc:"UTC calendar day (YYYY-MM-DD) of the matched transaction"`
	Name            string   `json:"name"`
	MerchantName    *string  `json:"merchant_name,omitempty"`
	Amount          float64  `json:"amount"`
	CurrentCategory string   `json:"current_category"`
	WouldAddTags    []string `json:"would_add_tags,omitempty"`
}
