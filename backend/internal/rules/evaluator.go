package rules

import (
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
)

// ConditionEvaluator evaluates a single condition against a transaction.
type ConditionEvaluator interface {
	// Evaluate returns true if the transaction matches the condition.
	Evaluate(tx *domain.Transaction, condition domain.RuleCondition) (bool, error)
}

// ConditionRegistry manages condition evaluators by type.
type ConditionRegistry struct {
	evaluators map[domain.ConditionType]ConditionEvaluator
}

// NewConditionRegistry creates a registry.
//
// #1625: the fourteen per-fused-type evaluator structs are GONE, not merely un-registered. Every
// built-in condition — legacy fused or new triple — now resolves through the single field-based
// implementation in field_evaluator.go, so "merchant contains" has exactly one meaning rather
// than two implementations that can drift apart.
//
// ⚠ #1625 final review I1: this comment said the structs were "retained (they are still directly
// unit-tested, and Register() can inject them)" while the comment 15 lines below said they "are
// gone". Both could not be true; a later commit on this branch deleted them and falsified this
// half. Re-derived: `grep -rn 'Evaluator struct' internal/rules/` returns nothing. Register()
// still exists and still works — for a CUSTOM type a caller supplies, which is the only thing it
// was ever needed for.
func NewConditionRegistry() *ConditionRegistry {
	return &ConditionRegistry{
		evaluators: make(map[domain.ConditionType]ConditionEvaluator),
	}
}

// Evaluate evaluates a condition.
//
// #1625: dispatch is on the normalized (Field, Operator) pair. The fourteen legacy fused types
// normalize into the same pairs, so a persisted rule and a new triple share one implementation
// rather than two that can drift. A condition arriving un-normalized (a hand-built struct in a
// test, say) is normalized here first — normalization is idempotent and never touches the row.
//
// A registered per-type evaluator still wins when one exists, so anything injected through
// Register() for a custom type keeps working. #1625: the fourteen BUILT-IN per-type evaluators
// that used to be registrable are gone — they duplicated evaluateField, nothing in production
// ever registered them, and because a registered evaluator wins, registering one would have
// silently beaten the unified path. That is the drift the rework existed to remove, and keeping
// them left it as a latent trap.
//
// ⚠ #1625 final review I5: COERCION IS APPLIED BEFORE DISPATCH, not inside the fallback. It used
// to sit only on the evaluateField call, so a registered evaluator received the RAW value while
// the built-in path received the repaired one — the same rule meaning two different things
// depending on whether someone had called Register(). That is the identical trap the paragraph
// above documents for the deleted structs, one line further down.
//
// Returns (false, nil) for an unknown field rather than an error. Note the ORIGINAL reason no
// longer holds: EvaluateRules used to abort a transaction's entire rule set on an error, and
// since #1625 A8 an error is local to its own rule. This stays fail-quiet deliberately, because
// under OR logic an error would skip the whole rule where a non-match lets the rule's other
// conditions decide. Making junk visible is shadow-mode quarantine's job, not this function's.
func (r *ConditionRegistry) Evaluate(tx *domain.Transaction, condition domain.RuleCondition) (bool, error) {
	if condition.Field == "" {
		if err := condition.Normalize(); err != nil {
			return false, nil
		}
	}
	condition = repairedForEvaluation(condition)
	if evaluator, ok := r.evaluators[condition.Type]; ok && condition.Type != domain.ConditionTypeExpression {
		return evaluator.Evaluate(tx, condition)
	}
	return evaluateField(tx, condition)
}

// repairedForEvaluation returns the condition the evaluator should actually read.
//
// #1625 Task 2: a wrong-typed value with one honest reading is repaired here rather than left to
// match nothing. Two properties, both of which have cost this branch a defect already:
//
//  1. ⚠ GATED ON ValidateShape. Coercion runs ONLY on a condition the strict validator REJECTS.
//     The first version ran it unconditionally, which silently changed rules that were never
//     broken: textValues DROPS a non-string array member and still reports ok, so
//     ["uber", 42] PASSES ValidateShape, is classified healthy, and is accepted by the live write
//     path today — and coercing it to ["uber","42"] made such a rule match transactions it did
//     not match yesterday. That is the same mistake #1625 refused to make when it added
//     effective_merchant as a NEW field instead of repointing merchant. Every value the validator
//     accepts now reaches the evaluator byte-identical to the stored row. (The silent drop may
//     well be a bug; fixing it belongs at the WRITE path, in its own change.)
//  2. READ-PATH ONLY. `condition` is a COPY — the parameter is by value and Value is replaced,
//     not mutated in place — so the stored row and rule_execution_history.definition_snapshot are
//     untouched. Evaluate reassigns its own local `condition` from the return value for the same
//     reason: the repair must reach every dispatch target without ever reaching the row.
//
// An uncoercible value is left exactly as it was: it is quarantined and enforced in RuleMatches,
// and rewriting it here would only change which error the evaluator reports.
func repairedForEvaluation(condition domain.RuleCondition) domain.RuleCondition {
	if err := condition.ValidateShape(); err == nil {
		return condition
	}
	if coerced, ok := condition.CoercedValue(); ok {
		condition.Value = coerced
	}
	return condition
}

// Register adds or replaces an evaluator for a condition type.
func (r *ConditionRegistry) Register(conditionType domain.ConditionType, evaluator ConditionEvaluator) {
	r.evaluators[conditionType] = evaluator
}

// ============================================================================
// Helper Functions
// ============================================================================

// Note: extractStringValues moved to internal/matching package (Issue #495)

// extractFloat extracts a float64 from interface{}.
func extractFloat(v interface{}) (float64, bool) {
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
