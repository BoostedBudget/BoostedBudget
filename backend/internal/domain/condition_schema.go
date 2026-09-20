package domain

import (
	"errors"
	"fmt"
	"time"

	"github.com/maxnussbaum/BoostedBudget/internal/pfc"
)

// ─── Rule condition vocabulary (#1625) ──────────────────────────────────────────────────────
//
// Conditions are expressed as a {field, operator, value} triple. The fourteen pre-#1625 FUSED
// type names (merchant_contains, amount_greater_than, …) remain a first-class input shape and
// are still EMITTED on output, so persisted rows, older clients, and the frozen
// rule_execution_history.definition_snapshot all keep working. Normalize() maps between them.
//
// Why the triple rather than more fused names: this round alone would need roughly fifteen new
// fused constants (name × 4 operators, merchant × 4, direction × 2, signed_amount × 5) against
// three new field values. The fused scheme does not survive the feature set.
//
// ⚠ RuleConditionOperator, NOT ConditionOperator: domain.ConditionOperator already exists in
// boost.go for Boost conditions. Two same-package types of one name will not compile. The
// SPELLINGS are deliberately shared with Boost so the codebase has one operator vocabulary —
// which is also why the reporter's guessed `operator: "eq"` is now simply correct. The
// validation maps are NOT shared: boost.go keeps two maps that disagree about `between` on
// purpose, and importing either would drag a Boost-specific rule into categorization.

// RuleConditionField is the transaction attribute a condition matches against.
type RuleConditionField string

const (
	FieldMerchant RuleConditionField = "merchant"
	// FieldEffectiveMerchant matches the merchant name the USER sees — their own rename, or the
	// canonical name of a merchant they merged this one into — rather than the raw provider
	// string. #1625 Task 11.
	//
	// A SEPARATE field rather than a change to FieldMerchant, deliberately: repointing `merchant`
	// would silently change what every persisted rule matches whenever a normalization, merge or
	// override changed a name, so a rule the user never edited would start or stop firing on its
	// own. As a new field, old rows keep their exact meaning and no data migration is needed.
	FieldEffectiveMerchant RuleConditionField = "effective_merchant"
	FieldName       RuleConditionField = "name"
	FieldMerchantID RuleConditionField = "merchant_id"
	// FieldAmount is the ABSOLUTE magnitude, frozen at the pre-#1625 semantics.
	FieldAmount RuleConditionField = "amount"
	// FieldSignedAmount is the raw signed amount: positive = outflow, negative = inflow.
	// A separate field rather than a flag on `amount`, because a flag defaults to false and is
	// one normalization slip away from silently redefining every persisted amount rule. No
	// legacy row can normalize to this field, which makes that class of regression
	// unrepresentable rather than merely tested-against.
	FieldSignedAmount   RuleConditionField = "signed_amount"
	FieldDirection      RuleConditionField = "direction"
	FieldAccountID      RuleConditionField = "account_id"
	FieldAccountType    RuleConditionField = "account_type"
	FieldPaymentChannel RuleConditionField = "payment_channel"
	FieldDate           RuleConditionField = "date"
	FieldPFCPrimary     RuleConditionField = "pfc_primary"
	FieldPFCDetailed    RuleConditionField = "pfc_detailed"
)

// RuleConditionOperator is the comparison a condition applies.
type RuleConditionOperator string

const (
	RuleOpEq         RuleConditionOperator = "eq"
	RuleOpNeq        RuleConditionOperator = "neq"
	RuleOpGt         RuleConditionOperator = "gt"
	RuleOpGte        RuleConditionOperator = "gte"
	RuleOpLt         RuleConditionOperator = "lt"
	RuleOpLte        RuleConditionOperator = "lte"
	RuleOpBetween    RuleConditionOperator = "between"
	RuleOpContains   RuleConditionOperator = "contains"
	RuleOpStartsWith RuleConditionOperator = "starts_with"
	RuleOpEndsWith   RuleConditionOperator = "ends_with"
	RuleOpIn         RuleConditionOperator = "in"
)

// ValueShape describes what a condition's Value may look like for a given field+operator.
type ValueShape string

const (
	// ShapeText: a string, or an array of strings meaning "any of". Arrays are ALREADY in the
	// database — merchant conditions accept them via matching.ExtractStringValues, and migration
	// 000072 wrote array account_id values — so a scalar-only matrix would quarantine live rules.
	ShapeText ValueShape = "text"
	// ShapeNumber: a single number.
	ShapeNumber ValueShape = "number"
	// ShapeNumberRange: {"min": n, "max": n}.
	ShapeNumberRange ValueShape = "number_range"
	// ShapeDate: a single YYYY-MM-DD string.
	ShapeDate ValueShape = "date"
	// ShapeDateRange: {"start": "YYYY-MM-DD", "end": "YYYY-MM-DD"} — EITHER bound may be absent.
	ShapeDateRange ValueShape = "date_range"
)

// FieldSpec declares what a field accepts. It is data, consumed by the validator and
// cross-checked by the rules registry, so the two cannot drift apart unnoticed.
type FieldSpec struct {
	Field RuleConditionField
	// Ops maps each supported operator to the value shape it requires.
	Ops map[RuleConditionOperator]ValueShape
	// EnumFn, when non-nil, returns the closed set of legal values for this field.
	EnumFn func(value string) error
}

var textOps = map[RuleConditionOperator]ValueShape{
	RuleOpEq:         ShapeText,
	RuleOpNeq:        ShapeText,
	RuleOpIn:         ShapeText,
	RuleOpContains:   ShapeText,
	RuleOpStartsWith: ShapeText,
	RuleOpEndsWith:   ShapeText,
}

// idOps: opaque identifiers are matched exactly, never by substring.
var idOps = map[RuleConditionOperator]ValueShape{
	RuleOpEq:  ShapeText,
	RuleOpNeq: ShapeText,
	RuleOpIn:  ShapeText,
}

var numberOps = map[RuleConditionOperator]ValueShape{
	RuleOpEq:      ShapeNumber,
	RuleOpNeq:     ShapeNumber,
	RuleOpGt:      ShapeNumber,
	RuleOpGte:     ShapeNumber,
	RuleOpLt:      ShapeNumber,
	RuleOpLte:     ShapeNumber,
	RuleOpBetween: ShapeNumberRange,
}

// FieldSpecs is the authoritative field × operator × value-shape matrix.
var FieldSpecs = map[RuleConditionField]FieldSpec{
	FieldMerchant:          {Field: FieldMerchant, Ops: textOps},
	FieldEffectiveMerchant: {Field: FieldEffectiveMerchant, Ops: textOps},
	FieldName:           {Field: FieldName, Ops: textOps},
	FieldMerchantID:     {Field: FieldMerchantID, Ops: idOps},
	FieldAccountID:      {Field: FieldAccountID, Ops: idOps},
	FieldAccountType:    {Field: FieldAccountType, Ops: textOps},
	FieldPaymentChannel: {Field: FieldPaymentChannel, Ops: textOps},
	FieldAmount:         {Field: FieldAmount, Ops: numberOps},
	FieldSignedAmount:   {Field: FieldSignedAmount, Ops: numberOps},
	FieldDirection: {
		Field:  FieldDirection,
		Ops:    map[RuleConditionOperator]ValueShape{RuleOpEq: ShapeText, RuleOpNeq: ShapeText, RuleOpIn: ShapeText},
		EnumFn: validateDirectionValue,
	},
	FieldDate: {
		Field: FieldDate,
		Ops: map[RuleConditionOperator]ValueShape{
			RuleOpEq:      ShapeDate,
			RuleOpGte:     ShapeDate,
			RuleOpLte:     ShapeDate,
			RuleOpBetween: ShapeDateRange,
		},
	},
	// The PFC fields reuse the existing curated-registry check rather than duplicating it.
	FieldPFCPrimary:  {Field: FieldPFCPrimary, Ops: idOps, EnumFn: func(v string) error { return validatePFCValue(v, false) }},
	FieldPFCDetailed: {Field: FieldPFCDetailed, Ops: idOps, EnumFn: func(v string) error { return validatePFCValue(v, true) }},
}

// Direction values.
const (
	DirectionInflow  = "inflow"
	DirectionOutflow = "outflow"
)

func validateDirectionValue(v string) error {
	if v != DirectionInflow && v != DirectionOutflow {
		return fmt.Errorf("direction must be %q or %q, got %q", DirectionInflow, DirectionOutflow, v)
	}
	return nil
}

// fusedToTriple expands each legacy fused type into exactly one (field, operator) pair.
var fusedToTriple = map[ConditionType]struct {
	Field RuleConditionField
	Op    RuleConditionOperator
}{
	ConditionMerchantContains:    {FieldMerchant, RuleOpContains},
	ConditionMerchantEquals:      {FieldMerchant, RuleOpEq},
	ConditionMerchantStartsWith:  {FieldMerchant, RuleOpStartsWith},
	ConditionMerchantEndsWith:    {FieldMerchant, RuleOpEndsWith},
	ConditionAmountGreaterThan:   {FieldAmount, RuleOpGt},
	ConditionAmountLessThan:      {FieldAmount, RuleOpLt},
	ConditionAmountEquals:        {FieldAmount, RuleOpEq},
	ConditionAmountRange:         {FieldAmount, RuleOpBetween},
	ConditionAccountID:           {FieldAccountID, RuleOpEq},
	ConditionAccountType:         {FieldAccountType, RuleOpEq},
	ConditionPaymentChannel:      {FieldPaymentChannel, RuleOpEq},
	ConditionDateRange:           {FieldDate, RuleOpBetween},
	ConditionExistingPFCPrimary:  {FieldPFCPrimary, RuleOpEq},
	ConditionExistingPFCDetailed: {FieldPFCDetailed, RuleOpEq},
}

// tripleToFused is the exact inverse, built once at init so the two can never disagree.
var tripleToFused = func() map[[2]string]ConditionType {
	m := make(map[[2]string]ConditionType, len(fusedToTriple))
	for fused, t := range fusedToTriple {
		m[[2]string{string(t.Field), string(t.Op)}] = fused
	}
	return m
}()

// ConditionTypeExpression is emitted for a triple with no fused ancestor (name, merchant_id,
// signed_amount, direction, open-ended dates), and stamped by the repository onto any persisted
// condition that cannot be normalized — so a RESPONSE always carries a `type`.
//
// ⚠ The JSON tag is `omitempty` and the schema therefore no longer marks `type` required. That
// is a deliberate contract change, and it is about INPUT: a client may now send field+operator
// instead of a fused name. Responses still always populate it. A consumer that relied on `type`
// being present in a response is unaffected; one that relied on the SCHEMA marking it required
// sees a widened type. Codex review flagged this; the widening is intended, the response
// guarantee is preserved by the repository fallback above.
//
// It is response-oriented, not input-oriented: sending it with no field+operator is rejected,
// because the sentinel on its own carries no information about what to match.
const ConditionTypeExpression ConditionType = "expression"

// Condition-shape errors. All wrap ErrInvalidCondition, which wraps ErrInvalidRule, so the Huma
// error mapper can turn them into a 422/400 instead of the 500 they used to produce.
var (
	ErrInvalidRule            = errors.New("invalid categorization rule")
	ErrInvalidCondition       = fmt.Errorf("%w: invalid condition", ErrInvalidRule)
	ErrUnknownConditionField  = fmt.Errorf("%w: unknown field", ErrInvalidCondition)
	ErrUnsupportedOperator    = fmt.Errorf("%w: unsupported operator for field", ErrInvalidCondition)
	ErrInvalidConditionValue  = fmt.Errorf("%w: invalid value shape", ErrInvalidCondition)
	ErrConditionShapeConflict = fmt.Errorf("%w: type conflicts with field/operator", ErrInvalidCondition)
)

// Normalize resolves the fused/triple duality into a canonical (Field, Operator) pair and
// re-derives Type. It is idempotent, and it never mutates anything on disk — the repository
// calls it after unmarshalling so legacy rows evaluate through the new registry with no data
// migration at all.
//
// Precedence:
//   - Field+Operator present  → authoritative; Type is re-derived from them.
//   - Type only               → expanded through fusedToTriple.
//   - Both, disagreeing       → ErrConditionShapeConflict (never silently pick one).
//   - Neither                 → ErrUnknownConditionField.
//
// A populated legacy Operator with no Field is OVERWRITTEN by the one the fused type implies,
// not honoured. Operator was never validated or read by any evaluator before #1625, so whatever
// a persisted row holds there is not evidence of intent — it is residue. The fused type is what
// actually drove matching, so deriving the operator from it preserves the rule's behaviour
// exactly, and the next write repairs the residue rather than carrying it forward forever.
//
// ⚠ This means a junk operator IS replaced on the next update. That is deliberate and lossless:
// the value never affected what the rule matched. Pinned by
// TestNormalize_OverwritesUnvalidatedLegacyOperator.
func (c *RuleCondition) Normalize() error {
	if c.Field != "" {
		spec, ok := FieldSpecs[c.Field]
		if !ok {
			return fmt.Errorf("%w: %q", ErrUnknownConditionField, c.Field)
		}
		if c.Operator == "" {
			return fmt.Errorf("%w: field %q requires an operator", ErrUnsupportedOperator, c.Field)
		}
		op := RuleConditionOperator(c.Operator)
		if _, ok := spec.Ops[op]; !ok {
			return fmt.Errorf("%w: field %q does not support operator %q (supported: %s)",
				ErrUnsupportedOperator, c.Field, c.Operator, supportedOperators(spec))
		}
		derived := deriveFusedType(c.Field, op)
		if c.Type != "" && c.Type != derived {
			// A caller sending both must send a consistent pair; guessing which one they meant
			// is exactly how a rule silently matches something other than what it says.
			return fmt.Errorf("%w: type %q does not correspond to field %q + operator %q (that pair is %q)",
				ErrConditionShapeConflict, c.Type, c.Field, c.Operator, derived)
		}
		c.Type = derived
		return nil
	}

	if c.Type == "" {
		return fmt.Errorf("%w: condition needs either a type or a field+operator pair", ErrUnknownConditionField)
	}
	if c.Type == ConditionTypeExpression {
		return fmt.Errorf("%w: %q is emitted by the server, not accepted as input — send field+operator instead",
			ErrConditionShapeConflict, ConditionTypeExpression)
	}
	t, ok := fusedToTriple[c.Type]
	if !ok {
		return fmt.Errorf("%w: unknown condition type %q", ErrUnknownConditionField, c.Type)
	}
	c.Field = t.Field
	c.Operator = string(t.Op)
	return nil
}

func deriveFusedType(field RuleConditionField, op RuleConditionOperator) ConditionType {
	if fused, ok := tripleToFused[[2]string{string(field), string(op)}]; ok {
		return fused
	}
	return ConditionTypeExpression
}

func supportedOperators(spec FieldSpec) string {
	ops := make([]string, 0, len(spec.Ops))
	for op := range spec.Ops {
		ops = append(ops, string(op))
	}
	sortStrings(ops)
	out := ""
	for i, op := range ops {
		if i > 0 {
			out += ", "
		}
		out += op
	}
	return out
}

// sortStrings is a tiny insertion sort — the slices here are at most a dozen operators, and
// this keeps the package free of a sort import purely for error-message determinism.
func sortStrings(s []string) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j] < s[j-1]; j-- {
			s[j], s[j-1] = s[j-1], s[j]
		}
	}
}

// ValidateShape checks the condition's Value against the field × operator matrix.
// Normalize must have run first.
func (c *RuleCondition) ValidateShape() error {
	spec, ok := FieldSpecs[c.Field]
	if !ok {
		return fmt.Errorf("%w: %q", ErrUnknownConditionField, c.Field)
	}
	shape, ok := spec.Ops[RuleConditionOperator(c.Operator)]
	if !ok {
		return fmt.Errorf("%w: field %q does not support operator %q (supported: %s)",
			ErrUnsupportedOperator, c.Field, c.Operator, supportedOperators(spec))
	}

	switch shape {
	case ShapeText:
		return c.validateTextValue(spec)
	case ShapeNumber:
		if _, ok := asFloat(c.Value); !ok {
			return fmt.Errorf("%w: field %q with operator %q needs a number, got %T",
				ErrInvalidConditionValue, c.Field, c.Operator, c.Value)
		}
		return nil
	case ShapeNumberRange:
		return c.validateBoundedRange("min", "max", false)
	case ShapeDate:
		s, ok := c.Value.(string)
		if !ok {
			return fmt.Errorf("%w: field %q needs a YYYY-MM-DD string, got %T",
				ErrInvalidConditionValue, c.Field, c.Value)
		}
		if _, err := time.Parse("2006-01-02", s); err != nil {
			return fmt.Errorf("%w: %q is not a YYYY-MM-DD date", ErrInvalidConditionValue, s)
		}
		return nil
	case ShapeDateRange:
		return c.validateBoundedRange("start", "end", true)
	}
	return nil
}

func (c *RuleCondition) validateTextValue(spec FieldSpec) error {
	values, ok := textValues(c.Value)
	if !ok || len(values) == 0 {
		return fmt.Errorf("%w: field %q needs a string or a non-empty array of strings, got %T",
			ErrInvalidConditionValue, c.Field, c.Value)
	}
	if spec.EnumFn != nil {
		for _, v := range values {
			if err := spec.EnumFn(v); err != nil {
				return fmt.Errorf("%w: %s", ErrInvalidConditionValue, err.Error())
			}
		}
	}
	return nil
}

// validateBoundedRange covers both {min,max} and {start,end}. dateBounds selects date parsing
// and, per the pre-#1625 date_range evaluator, allows EITHER bound to be omitted.
func (c *RuleCondition) validateBoundedRange(loKey, hiKey string, dateBounds bool) error {
	m, ok := c.Value.(map[string]interface{})
	if !ok {
		return fmt.Errorf("%w: field %q with operator between needs an object with %q and %q, got %T",
			ErrInvalidConditionValue, c.Field, loKey, hiKey, c.Value)
	}
	lo, hasLo := m[loKey]
	hi, hasHi := m[hiKey]
	if !hasLo && !hasHi {
		return fmt.Errorf("%w: range for field %q needs at least one of %q or %q",
			ErrInvalidConditionValue, c.Field, loKey, hiKey)
	}

	if dateBounds {
		var loT, hiT time.Time
		if hasLo {
			t, err := parseDateValue(lo)
			if err != nil {
				return fmt.Errorf("%w: %q bound: %s", ErrInvalidConditionValue, loKey, err.Error())
			}
			loT = t
		}
		if hasHi {
			t, err := parseDateValue(hi)
			if err != nil {
				return fmt.Errorf("%w: %q bound: %s", ErrInvalidConditionValue, hiKey, err.Error())
			}
			hiT = t
		}
		if hasLo && hasHi && hiT.Before(loT) {
			return fmt.Errorf("%w: %q must not be before %q", ErrInvalidConditionValue, hiKey, loKey)
		}
		return nil
	}

	// A numeric range requires BOTH bounds — the pre-#1625 amount_range evaluator rejects a
	// half-open one, so accepting it at write time would persist a rule that never matches.
	if !hasLo || !hasHi {
		return fmt.Errorf("%w: numeric range for field %q needs both %q and %q",
			ErrInvalidConditionValue, c.Field, loKey, hiKey)
	}
	loF, okLo := asFloat(lo)
	hiF, okHi := asFloat(hi)
	if !okLo || !okHi {
		return fmt.Errorf("%w: %q and %q must both be numbers", ErrInvalidConditionValue, loKey, hiKey)
	}
	if hiF < loF {
		return fmt.Errorf("%w: %q (%v) must not be less than %q (%v)", ErrInvalidConditionValue, hiKey, hiF, loKey, loF)
	}
	return nil
}

func parseDateValue(v interface{}) (time.Time, error) {
	s, ok := v.(string)
	if !ok {
		return time.Time{}, fmt.Errorf("expected a YYYY-MM-DD string, got %T", v)
	}
	t, err := time.Parse("2006-01-02", s)
	if err != nil {
		return time.Time{}, fmt.Errorf("%q is not a YYYY-MM-DD date", s)
	}
	return t, nil
}

// textValues accepts a scalar string or an array of strings — the two shapes already in the
// database.
//
// ⚠ A MIXED array silently drops its non-string members rather than failing. That mirrors
// matching.ExtractStringValues, which is what actually runs at evaluation time: a persisted
// `["uber", 42]` still matches "uber" today. Rejecting it here would leave such a rule
// matchable but permanently un-updatable — the user could not save an unrelated edit to it —
// which is precisely the quarantine-a-working-rule failure this validator is meant to avoid.
// An array with NO usable string member is still rejected, because that one can never match.
func textValues(v interface{}) ([]string, bool) {
	switch val := v.(type) {
	case string:
		return []string{val}, true
	case []string:
		return val, true
	case []interface{}:
		out := make([]string, 0, len(val))
		for _, item := range val {
			if s, ok := item.(string); ok {
				out = append(out, s)
			}
		}
		return out, len(out) > 0
	default:
		return nil, false
	}
}

func asFloat(v interface{}) (float64, bool) {
	switch val := v.(type) {
	case float64:
		return val, true
	case float32:
		return float64(val), true
	case int:
		return float64(val), true
	case int64:
		return float64(val), true
	default:
		return 0, false
	}
}

// ─── The published condition matrix (#1625 follow-up) ───────────────────────────────────────
//
// The enum tags make the VOCABULARY discoverable — which fields and which operators exist. They do
// not make the MATRIX discoverable: which operators a given field accepts, what shape each needs,
// which closed value sets apply, or what the values MEAN. Two independent reviews identified that
// as the largest remaining gap in #1625, because the reporter's own workflow ("a $1,000 Zelle
// outflow") still required guessing the sign convention, and a wrong guess VALIDATES SILENTLY and
// simply never matches.
//
// This is DERIVED from FieldSpecs — the same map the validator and the evaluator registry both
// read — so a published matrix cannot drift from the one the server enforces.

// ConditionOperatorSchema describes one legal (field, operator) pair.
type ConditionOperatorSchema struct {
	Operator string `json:"operator"`
	// ValueShape is one of: text, number, number_range, date, date_range.
	ValueShape string `json:"value_shape"`
	// Example is a well-formed value of that shape, so a caller can copy rather than infer.
	Example interface{} `json:"example,omitempty"`
}

// ConditionFieldSchema describes one field and everything it accepts.
type ConditionFieldSchema struct {
	Field     string                    `json:"field"`
	Operators []ConditionOperatorSchema `json:"operators"`
	// Values is the closed set, when there is one. Absent means free-form.
	Values []string `json:"values,omitempty"`
	// Notes carries semantics that cannot be inferred from the type — chiefly sign conventions,
	// which are the failure mode that validates silently.
	Notes string `json:"notes,omitempty"`
}

// ConditionSchemaResponse is the published matrix.
type ConditionSchemaResponse struct {
	Fields []ConditionFieldSchema `json:"fields"`
}

// fieldNotes carries the semantics an agent cannot infer from a type or an enum.
var fieldNotes = map[RuleConditionField]string{
	FieldAmount: "Absolute magnitude — the sign is discarded, so this matches an inflow and an " +
		"outflow of the same size equally. Use signed_amount or direction to distinguish them.",
	FieldSignedAmount: "Signed: POSITIVE is money leaving the account (an outflow), negative is " +
		"money arriving. A $1,000 payment out is 1000, not -1000.",
	FieldDirection: "Derived from the sign of the amount. A zero amount matches neither value.",
	FieldName: "The raw transaction description. Always present — unlike merchant, which is " +
		"frequently absent (a Zelle transfer typically has no merchant name at all).",
	FieldMerchantID: "An opaque provider identifier, matched exactly and case-SENSITIVELY.",
	FieldMerchant: "The RAW provider merchant name, matched case-insensitively. Frequently absent; " +
		"prefer name when matching transfers. This is NOT the name shown in the app if you have " +
		"renamed or merged the merchant — use effective_merchant for that.",
	FieldEffectiveMerchant: "The merchant name YOU see: your rename, or the canonical name of a " +
		"merchant you merged this one into, falling back to the raw provider name when you have " +
		"set neither. Match this when your rule is written against what the app displays.",
	// #1625 Task 20: this schema's own operation Description promises "closed value sets", but
	// closedValuesFor deliberately returns nil for both PFC fields — the promise was false as
	// written for the one caller who could not also read closedSetExemptFields in Go source.
	// Naming the actual endpoint here, not just the count, is what makes the promise true.
	FieldPFCPrimary: "Matches the category the bank ORIGINALLY reported — the raw provider value, " +
		"before any override. This is NOT the effective category shown in the app: a rule's own " +
		"action can never rewrite what it matches. Closed set of 18 curated PFCv2 primary categories. " +
		"Not inlined here — call GET /api/v1/categories/plaid-primaries once and cache the result.",
	FieldPFCDetailed: "Matches the category the bank ORIGINALLY reported — the raw provider value, " +
		"before any override. This is NOT the effective category shown in the app: a rule's own " +
		"action can never rewrite what it matches. Closed set of ~120 curated PFCv2 detailed " +
		"categories. Not inlined here — call GET /api/v1/categories/plaid-detailed once and cache " +
		"the result.",
}

// exampleFor returns a well-formed value for a shape, so callers copy rather than guess.
func exampleFor(shape ValueShape, field RuleConditionField) interface{} {
	switch shape {
	case ShapeNumber:
		return 1000
	case ShapeNumberRange:
		return map[string]interface{}{"min": 100, "max": 500}
	case ShapeDate:
		return "2026-01-31"
	case ShapeDateRange:
		return map[string]interface{}{"start": "2026-01-01", "end": "2026-01-31"}
	default:
		switch field {
		case FieldDirection:
			return DirectionOutflow
		case FieldPFCPrimary, FieldPFCDetailed:
			// #1625 N1: these fields carry an EnumFn, so a generic placeholder is REJECTED by the
			// very validator this schema exists to describe — the published example 422'd. Derive
			// the example from the registry the EnumFn checks against, so it cannot drift from it.
			return firstPFCExample(field)
		}
		return "example text"
	}
}

// firstPFCExample returns a real value from the curated PFC registry — the same source
// validatePFCValue checks against. Ordered accessors, so the published schema stays byte-stable
// across builds.
func firstPFCExample(field RuleConditionField) interface{} {
	if field == FieldPFCDetailed {
		if all := pfc.AllDetailed(); len(all) > 0 {
			return all[0].Enum
		}
		return ""
	}
	if all := pfc.AllPrimaries(); len(all) > 0 {
		return all[0].Enum
	}
	return ""
}

// closedValuesFor returns the legal values for fields that have a closed set.
// FieldSpec.EnumFn is a VALIDATOR, not an enumerator, so these are stated explicitly — and
// TestConditionSchema_ExposesTheSemanticsThatFailSilently pins that direction stays listed.
func closedValuesFor(field RuleConditionField) []string {
	switch field {
	case FieldDirection:
		return []string{DirectionInflow, DirectionOutflow}
	default:
		// PFC fields are a closed set too, but a large and separately-published one — the PFC
		// enums already reach clients through their own schema, so duplicating ~120 values here
		// would bloat every response for information callers already have.
		return nil
	}
}

// ConditionSchema builds the published matrix from FieldSpecs.
func ConditionSchema() ConditionSchemaResponse {
	fields := make([]ConditionFieldSchema, 0, len(FieldSpecs))
	for _, field := range allConditionFieldsOrdered() {
		spec, ok := FieldSpecs[field]
		if !ok {
			continue
		}
		ops := make([]ConditionOperatorSchema, 0, len(spec.Ops))
		for _, op := range allConditionOperatorsOrdered() {
			shape, allowed := spec.Ops[op]
			if !allowed {
				continue
			}
			ops = append(ops, ConditionOperatorSchema{
				Operator:   string(op),
				ValueShape: string(shape),
				Example:    exampleFor(shape, field),
			})
		}
		fields = append(fields, ConditionFieldSchema{
			Field:     string(field),
			Operators: ops,
			Values:    closedValuesFor(field),
			Notes:     fieldNotes[field],
		})
	}
	return ConditionSchemaResponse{Fields: fields}
}

// Deterministic ordering, so the response and its generated artifacts are stable across builds.
func allConditionFieldsOrdered() []RuleConditionField {
	return []RuleConditionField{
		FieldMerchant, FieldEffectiveMerchant, FieldName, FieldMerchantID, FieldAmount,
		FieldSignedAmount, FieldDirection, FieldAccountID, FieldAccountType, FieldPaymentChannel,
		FieldDate, FieldPFCPrimary, FieldPFCDetailed,
	}
}

func allConditionOperatorsOrdered() []RuleConditionOperator {
	return []RuleConditionOperator{
		RuleOpEq, RuleOpNeq, RuleOpGt, RuleOpGte, RuleOpLt, RuleOpLte,
		RuleOpBetween, RuleOpContains, RuleOpStartsWith, RuleOpEndsWith, RuleOpIn,
	}
}
