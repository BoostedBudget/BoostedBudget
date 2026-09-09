package rules

import (
	"testing"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
)

// #1625 Task 8: a rule with NO conditions must match NOTHING.
//
// RuleMatches returned true for an empty condition list, commented "No conditions means always
// match (edge case)". That is not an edge case, it is a ledger-wide recategorisation: such a rule
// applies its target to every transaction the user has.
//
// This is the one quarantine reason it is safe to enforce without a production inventory, and the
// one where leaving it alone is the RISKY choice. The check is provably unreachable from the
// product: CategorizationRule.Validate has rejected empty conditions since dedac0aa (#346) -- the
// same commit that created the file -- and no migration inserts rules. Such a row therefore
// requires a direct database edit. The table does default `conditions` to '[]', which is exactly
// the non-service insert this guard exists to catch.
//
// The other three quarantine reasons stay SURFACED but not enforced: each is "this might disable
// a rule that is currently working", where doing nothing is the conservative choice.
func TestRuleMatches_ZeroConditionRuleMatchesNothing(t *testing.T) {
	engine := NewRuleEngine()

	rule := &domain.CategorizationRule{
		ID:             "r-empty",
		Enabled:        true,
		ConditionLogic: "AND",
		Conditions:     []domain.RuleCondition{},
	}

	tx := newTestTransaction("ANYTHING AT ALL", -12.34, "in store", nil)

	matched, err := engine.RuleMatches(tx, rule)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if matched {
		t.Error("a rule with no conditions matched a transaction — it would recategorise the " +
			"user's entire ledger")
	}
}

// Enforcement is reason-SCOPED. A rule with a broken condition value is quarantine-worthy for a
// different reason, and that reason is deliberately NOT enforced yet: refusing to run it could
// disable a rule that is currently doing useful work. It must still be evaluated normally.
func TestRuleMatches_OtherQuarantineReasonsAreStillEvaluated(t *testing.T) {
	engine := NewRuleEngine()

	// invalid_condition_logic_defaults_to_and — surfaced by the read model, still runs.
	rule := &domain.CategorizationRule{
		ID:             "r-odd-logic",
		Enabled:        true,
		ConditionLogic: "SOMETHING_ELSE",
		Conditions: []domain.RuleCondition{
			{Type: domain.ConditionMerchantContains, Value: "starbucks"},
		},
	}

	tx := newTestTransaction("STARBUCKS COFFEE #123", -5.50, "in store", nil)

	matched, err := engine.RuleMatches(tx, rule)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !matched {
		t.Error("enforcement leaked beyond no_conditions — this rule still works and must run")
	}
}

// The kill switch must actually restore the previous behaviour, or it is not a kill switch.
func TestRuleMatches_ZeroConditionEnforcementCanBeDisabled(t *testing.T) {
	original := enforceZeroConditionQuarantine
	enforceZeroConditionQuarantine = false
	t.Cleanup(func() { enforceZeroConditionQuarantine = original })

	engine := NewRuleEngine()
	rule := &domain.CategorizationRule{
		ID: "r-empty", Enabled: true, ConditionLogic: "AND",
		Conditions: []domain.RuleCondition{},
	}

	matched, err := engine.RuleMatches(newTestTransaction("X", -1, "in store", nil), rule)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !matched {
		t.Error("with enforcement disabled the pre-#1625 match-everything behaviour must return")
	}
}
