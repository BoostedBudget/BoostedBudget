package domain

import (
	"errors"
	"fmt"
	"time"
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
	FieldMerchant       RuleConditionField = "merchant"
	FieldName           RuleConditionField = "name"
	FieldMerchantID     RuleConditionField = "merchant_id"
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
	FieldMerchant:     {Field: FieldMerchant, Ops: textOps},
	FieldName:         {Field: FieldName, Ops: textOps},
	FieldMerchantID:   {Field: FieldMerchantID, Ops: idOps},
	FieldAccountID:    {Field: FieldAccountID, Ops: idOps},
	FieldAccountType:  {Field: FieldAccountType, Ops: textOps},
	FieldPaymentChannel: {Field: FieldPaymentChannel, Ops: textOps},
	FieldAmount:       {Field: FieldAmount, Ops: numberOps},
	FieldSignedAmount: {Field: FieldSignedAmount, Ops: numberOps},
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
// signed_amount, direction, open-ended dates). `type` is a REQUIRED response field, so emitting
// a reserved value keeps the contract intact for clients that read it; it is response-only and
// is never something a client needs to send.
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
// A populated legacy Operator with no Field is IGNORED, not honoured: Operator was never
// validated or read before #1625, so a persisted value there is not evidence of intent. It is
// preserved verbatim in LegacyOperator so a read-modify-write cycle cannot silently drop it.
func (c *RuleCondition) Normalize() error {
	if c.Operator != "" && c.Field == "" && c.LegacyOperator == "" {
		c.LegacyOperator = c.Operator
	}

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
// database. Numbers are accepted and stringified because payment_channel/account_type values
// have historically been written loosely.
func textValues(v interface{}) ([]string, bool) {
	switch val := v.(type) {
	case string:
		return []string{val}, true
	case []string:
		return val, true
	case []interface{}:
		out := make([]string, 0, len(val))
		for _, item := range val {
			s, ok := item.(string)
			if !ok {
				return nil, false
			}
			out = append(out, s)
		}
		return out, true
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
