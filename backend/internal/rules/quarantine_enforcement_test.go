package rules

import (
	"testing"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/observability"
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

// TestQuarantineEnforcementMatchesTheAdvertisedSet ties observability.IsEnforcedQuarantineReason
// to what RuleMatches ACTUALLY does, for every reason in the vocabulary.
//
// #1625, found by adversarial review: the read model marked all four reasons "quarantined" and the
// card rendered "Not running", but only no_conditions is enforced. The other three still match
// transactions — so the UI asserted something the system knew to be false, which is the defect
// class this ticket exists to close.
//
// A hand-maintained list of "which reasons are enforced" would drift from the engine the first
// time someone enforced another one. This drives the engine instead: for each reason it builds a
// rule that a transaction WOULD otherwise match, and requires match == !enforced.
func TestQuarantineEnforcementMatchesTheAdvertisedSet(t *testing.T) {
	engine := NewRuleEngine()
	tx := newTestTransaction("STARBUCKS COFFEE #123", -5.50, "in store", nil)

	// Each fixture is a rule that classifies as exactly one reason AND would match `tx` if the
	// engine let it. A rule that could not match anyway would make "did not match" meaningless.
	cases := []struct {
		reason observability.QuarantineReason
		rule   *domain.CategorizationRule
	}{
		{
			observability.QuarantineNoConditions,
			&domain.CategorizationRule{ID: "r1", Enabled: true, ConditionLogic: "AND",
				Conditions: []domain.RuleCondition{}},
		},
		{
			observability.QuarantineInvalidLogic,
			&domain.CategorizationRule{ID: "r2", Enabled: true, ConditionLogic: "XOR",
				Conditions: []domain.RuleCondition{
					{Type: domain.ConditionMerchantContains, Value: "starbucks"},
				}},
		},
		{
			// Stops matching, but by a DIFFERENT mechanism than no_conditions: the evaluator
			// returns an error, so A8 skips the whole rule. Same observable outcome, so the same
			// answer is owed to the user.
			observability.QuarantineInvalidValue,
			&domain.CategorizationRule{ID: "r3", Enabled: true, ConditionLogic: "OR",
				Conditions: []domain.RuleCondition{
					{Type: domain.ConditionAmountGreaterThan, Value: "not-a-number"},
					{Type: domain.ConditionMerchantContains, Value: "starbucks"},
				}},
		},
		{
			// The case that makes this test necessary. Normalization failure returns (false, nil)
			// rather than an error, so under OR the valid sibling still matches and the rule
			// applies its action in full — while the read model would happily have called it
			// "Not running".
			observability.QuarantineUnnormalizable,
			&domain.CategorizationRule{ID: "r4", Enabled: true, ConditionLogic: "OR",
				Conditions: []domain.RuleCondition{
					{Type: "a_type_that_does_not_exist", Value: "x"},
					{Type: domain.ConditionMerchantContains, Value: "starbucks"},
				}},
		},
	}

	for _, c := range cases {
		matched, err := engine.RuleMatches(tx, c.rule)
		if err != nil {
			// An error is not a match either way; the point is whether the rule can still act.
			matched = false
		}
		enforced := observability.QuarantineReasonStopsMatching(c.reason)
		if enforced && matched {
			t.Errorf("%s is advertised as stopping the rule, but it still matched — the read "+
				"model reports quarantined and the card says \"Not running\", which is false",
				c.reason)
		}
		if !enforced && !matched {
			t.Errorf("%s is advertised as still-running but the rule did NOT match — behaviour "+
				"has changed and QuarantineReasonStopsMatching no longer describes reality",
				c.reason)
		}
	}
}
