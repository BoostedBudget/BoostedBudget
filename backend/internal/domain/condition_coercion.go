package domain

import (
	"strconv"
	"strings"
	"time"
)

// CoercedValue returns a READ-PATH repair of the condition's value, or ok=false when the value
// has no single honest reading.
//
// #1625 Task 2. Of the FIVE quarantine reasons (observability.AllQuarantineReasons(); this said
// "four" because it was written in the very commit that added the fifth,
// uncoercible_condition_value — #1625 final review I1), `invalid_condition_value_shape` is the one
// COMMON to real user rules: dedac0aa (#346) — the commit that created categorization rules —
// validated `cond.Type.IsValid()` and NOTHING about the value, so a wrong-typed value has
// persisted through the ordinary public API for the entire life of the feature. Enforcing that
// reason would switch off rules people wrote and use. Most of those values are a number sent as
// a string or the reverse, which has exactly one reading, so they are repaired instead.
//
// ⚠ #1625 second fix wave L7: it is NOT the only reason an API-created rule can be in, and this
// comment said it was. The same absent validation admitted values that coercion CANNOT repair —
// an object where a scalar is required, an empty array, a non-numeric string for a number — which
// is precisely why quarantine.go carries uncoercible_condition_value as a separate, enforced
// reason instead of folding it into this one. The honest statement is that repairable shape
// errors are the COMMON case in the legacy corpus, not the only reachable one.
//
// Three properties this function must keep, each of which has a test:
//
//  1. READ-PATH ONLY. It never writes to the receiver. The caller applies the result to its own
//     copy of the condition, so the stored row — and rule_execution_history.definition_snapshot,
//     which is an immutable audit record — is untouched.
//  2. IT DOES NOT WIDEN THE WRITE PATH. ValidateShape stays strict; a value that only survives
//     coercion is still refused by Validate. If both loosened, the corpus of malformed rows would
//     keep growing and this accommodation would become permanent.
//  3. IT DOES NOT GUESS. A two-element array is not a range (which end is which?), "$1,000" is
//     not 1000 (whose thousands separator?), and 01/02/2026 is not a date (which calendar?).
//     Inventing intent is the same class of defect as the silent match-everything this ticket
//     exists to remove — so those return false and the rule is quarantined and stopped.
//
// A value that already validates is returned in an equivalent, still-valid form, because this
// runs on every evaluation of every rule and must be a no-op for healthy ones.
func (c *RuleCondition) CoercedValue() (interface{}, bool) {
	spec, ok := FieldSpecs[c.Field]
	if !ok {
		// An unknown field is `unnormalizable_condition`, classified and enforced elsewhere.
		// There is no shape to coerce towards, so leave the value exactly as it is.
		return c.Value, true
	}
	shape, ok := spec.Ops[RuleConditionOperator(c.Operator)]
	if !ok {
		return c.Value, true
	}

	switch shape {
	case ShapeText:
		return coerceTextShape(c.Value, spec)
	case ShapeNumber:
		return coerceNumberShape(c.Value)
	case ShapeNumberRange:
		return coerceNumberRangeShape(c.Value)
	case ShapeDate:
		s, ok := coerceDateString(c.Value)
		if !ok {
			return nil, false
		}
		return s, true
	case ShapeDateRange:
		return coerceDateRangeShape(c.Value)
	}
	return c.Value, true
}

// coerceTextShape normalizes to []string so the evaluators' ExtractStringValues sees one shape.
// A non-string MEMBER of an array is coerced rather than dropped, so a value that reaches here at
// all is repaired whole rather than half.
//
// ⚠ #1625 final review I5: this docstring used to say the coercion FIXES `["uber", 7]` matching
// only "uber". It does not, and must not. textValues DROPS the 7 and still reports ok, so that
// value PASSES ValidateShape — and repairedForEvaluation only calls CoercedValue on a condition
// ValidateShape REJECTS, precisely so a rule the write path still accepts cannot silently start
// matching transactions it did not match yesterday. The silent drop may well be a bug; fixing it
// belongs at the WRITE path, in its own change. This function never sees that value.
func coerceTextShape(v interface{}, spec FieldSpec) (interface{}, bool) {
	var raw []interface{}
	switch val := v.(type) {
	case []interface{}:
		raw = val
	case []string:
		raw = make([]interface{}, 0, len(val))
		for _, s := range val {
			raw = append(raw, s)
		}
	default:
		raw = []interface{}{v}
	}

	out := make([]string, 0, len(raw))
	for _, item := range raw {
		s, ok := coerceScalarToText(item)
		if !ok {
			return nil, false
		}
		out = append(out, s)
	}
	if len(out) == 0 {
		// An empty array names nothing to match. There is no repair for "match none of these".
		return nil, false
	}
	if spec.EnumFn != nil {
		for _, s := range out {
			if err := spec.EnumFn(s); err != nil {
				// A value outside a closed set is a typo, not a shape problem — coercing the
				// TYPE would hand the evaluator something the write path still refuses.
				return nil, false
			}
		}
	}
	return out, true
}

// coerceScalarToText accepts strings and numbers only. A bool is refused deliberately: no
// merchant is named `true`, and rendering it as "true" would fabricate an intent nobody typed.
func coerceScalarToText(v interface{}) (string, bool) {
	switch val := v.(type) {
	case string:
		return val, true
	case int:
		return strconv.Itoa(val), true
	case int64:
		return strconv.FormatInt(val, 10), true
	case float64:
		return strconv.FormatFloat(val, 'f', -1, 64), true
	case float32:
		return strconv.FormatFloat(float64(val), 'f', -1, 32), true
	}
	return "", false
}

func coerceNumberShape(v interface{}) (interface{}, bool) {
	f, ok := coerceScalarToNumber(v)
	if !ok {
		return nil, false
	}
	return f, true
}

// coerceScalarToNumber reads a quoted number. It deliberately does NOT strip currency symbols or
// thousands separators: "$1,000" is 1000 to a US reader and malformed to everyone else, and
// "1.000" is one or one thousand depending on the locale. Neither has one reading.
func coerceScalarToNumber(v interface{}) (float64, bool) {
	if f, ok := asFloat(v); ok {
		return f, true
	}
	if s, ok := v.(string); ok {
		if f, err := strconv.ParseFloat(strings.TrimSpace(s), 64); err == nil {
			return f, true
		}
	}
	return 0, false
}

func coerceNumberRangeShape(v interface{}) (interface{}, bool) {
	m, ok := v.(map[string]interface{})
	if !ok {
		// An array is NOT a range: [100, 500] could be min/max or max/min, and a rule that
		// silently picked one would match a different set of transactions than the other reading.
		return nil, false
	}
	lo, hasLo := m["min"]
	hi, hasHi := m["max"]
	if !hasLo || !hasHi {
		return nil, false
	}
	loF, okLo := coerceScalarToNumber(lo)
	hiF, okHi := coerceScalarToNumber(hi)
	if !okLo || !okHi {
		return nil, false
	}
	if hiF < loF {
		// Swapping them is a guess about which bound the user got wrong.
		return nil, false
	}
	return map[string]interface{}{"min": loF, "max": hiF}, true
}

func coerceDateRangeShape(v interface{}) (interface{}, bool) {
	m, ok := v.(map[string]interface{})
	if !ok {
		return nil, false
	}
	out := make(map[string]interface{}, 2)
	var loT, hiT time.Time
	lo, hasLo := m["start"]
	hi, hasHi := m["end"]
	if !hasLo && !hasHi {
		return nil, false
	}
	if hasLo {
		s, ok := coerceDateString(lo)
		if !ok {
			return nil, false
		}
		out["start"] = s
		loT, _ = time.Parse(dateLayout, s)
	}
	if hasHi {
		s, ok := coerceDateString(hi)
		if !ok {
			return nil, false
		}
		out["end"] = s
		hiT, _ = time.Parse(dateLayout, s)
	}
	if hasLo && hasHi && hiT.Before(loT) {
		return nil, false
	}
	return out, true
}

const dateLayout = "2006-01-02"

// coerceDateString accepts the unambiguous spellings of a calendar day: the canonical one, an
// unpadded one, and a timestamp (whose date part is not in doubt). It refuses anything
// order-ambiguous — 01/02/2026 is January 2nd in the US and February 1st almost everywhere else,
// and picking one would silently shift a whole date-range rule by a month.
func coerceDateString(v interface{}) (string, bool) {
	s, ok := v.(string)
	if !ok {
		return "", false
	}
	s = strings.TrimSpace(s)
	for _, layout := range []string{dateLayout, "2006-1-2", time.RFC3339, "2006-01-02T15:04:05"} {
		if t, err := time.Parse(layout, s); err == nil {
			return t.Format(dateLayout), true
		}
	}
	return "", false
}
