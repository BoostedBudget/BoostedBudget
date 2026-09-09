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
// #1625: the fourteen per-fused-type evaluator structs are NO LONGER pre-registered. Every
// built-in condition — legacy fused or new triple — now resolves through the single field-based
// implementation in field_evaluator.go, so "merchant contains" has exactly one meaning rather
// than two implementations that can drift apart. The structs themselves are retained (they are
// still directly unit-tested, and Register() can inject them or any replacement).
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
	if evaluator, ok := r.evaluators[condition.Type]; ok && condition.Type != domain.ConditionTypeExpression {
		return evaluator.Evaluate(tx, condition)
	}
	// #1625 Task 2: repair a wrong-typed value before reading it. `condition` is a COPY (the
	// parameter is by value and Value is replaced, not mutated in place), so the stored row and
	// rule_execution_history.definition_snapshot are untouched — coercion is read-path only.
	// An uncoercible value is left exactly as it was: it is quarantined and enforced in
	// RuleMatches, and rewriting it here would only change which error the evaluator reports.
	if coerced, ok := condition.CoercedValue(); ok {
		condition.Value = coerced
	}
	return evaluateField(tx, condition)
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
