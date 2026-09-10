package rules

import (
	"reflect"
	"strings"
	"testing"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/observability"
)

// quarantine_published_contract_test.go — #1625 second fix wave H2/M3.
//
// THE SEMANTICS THIS BRANCH SETTLED ON, once, so every surface can derive from it:
//
//	`quarantined` = "the server is currently preventing this rule from matching anything".
//
// It is deliberately SWITCH-DEPENDENT. RULE_QUARANTINE_ENFORCE off means the engine runs
// enforcement-class rules, so reporting quarantined=true would be a false claim about the user's
// own ledger — which is the defect #1625 exists to remove, not a state to re-create in the read
// model. The cost of this choice is that `quarantined=false` no longer implies "healthy or
// repaired", and every surface that used to make that leap had to stop: the card
// (AutoCategorizationRuleCard.quarantineSentence), the two published doc tags, and the metric
// help. This file is the guard for the two written surfaces; the card has its own tests.
//
// The rejected alternative was (a): `quarantined` = "classified as stopping", switch-independent,
// plus a second field for "actually stopped now". It is defensible, but it makes the FIRST field
// the one an agent reads and the SECOND the one that is true, and the whole ticket is about a
// published contract that was not true.

// TestQuarantineFalseDoesNotMeanRepaired is the concrete falsification of the claim the
// `quarantined` doc tag used to publish: "False WITH a quarantine_reason means the rule has a
// problem the server repaired well enough to keep running it".
//
// With the kill switch off, a zero-condition rule is reported quarantined=false and matches EVERY
// transaction. Nothing was repaired; enforcement was simply not applied.
func TestQuarantineFalseDoesNotMeanRepaired(t *testing.T) {
	tx := quarantineTx()
	engine := NewRuleEngine()
	rule := ruleWith("AND") // no conditions

	reason := QuarantineReasonFor(rule)
	if reason != observability.QuarantineNoConditions {
		t.Fatalf("fixture classified %q; this test no longer exercises an enforcement-class reason", reason)
	}

	restore := SetQuarantineEnforcementForTest(false)
	t.Cleanup(restore)

	// What the read model publishes in this state (services.quarantineStopsMatching, derived from
	// the same two inputs).
	advertised := QuarantineEnforced() && observability.QuarantineReasonStopsMatching(reason)
	if advertised {
		t.Fatal("with enforcement off the read model must report quarantined=false")
	}

	matched, err := engine.RuleMatches(tx, rule)
	if err != nil {
		t.Fatalf("RuleMatches errored: %v", err)
	}
	if !matched {
		t.Fatal("fixture is not falsifiable: a zero-condition rule with enforcement off must match " +
			"every transaction, which is the whole reason quarantined=false cannot mean 'repaired'")
	}
}

// TestPublishedQuarantineDocTagsAreTrueInBothSwitchStates guards the two doc tags that reach
// api/openapi.yaml and the MCP spec index — the surface an agent reads and cannot verify.
//
// It cannot check prose for truth, so it checks the two specific things that made the prose false:
// a claim keyed to the REASON alone, and no mention of the switch that decides the answer.
func TestPublishedQuarantineDocTagsAreTrueInBothSwitchStates(t *testing.T) {
	typ := reflect.TypeOf(domain.CategorizationRule{})

	// Sentences retracted by this wave. Each was true only with enforcement ON.
	retracted := []string{
		"repaired well enough",
		"ENFORCED (quarantined=true",
		"Four values are ENFORCED",
	}

	for _, field := range []string{"Quarantined", "QuarantineReason"} {
		f, ok := typ.FieldByName(field)
		if !ok {
			t.Fatalf("domain.CategorizationRule has no field %s — this guard scans nothing", field)
		}
		doc := f.Tag.Get("doc")
		if strings.TrimSpace(doc) == "" {
			t.Fatalf("%s carries no doc tag; the published contract is what this guard exists to check", field)
		}

		for _, phrase := range retracted {
			if strings.Contains(doc, phrase) {
				t.Errorf("%s's doc tag still publishes %q. That sentence is true only while "+
					"%s is on; with it off the rule RUNS and the claim is false to every agent "+
					"reading the spec.", field, phrase, quarantineEnforceEnvVar)
			}
		}

		if !strings.Contains(doc, quarantineEnforceEnvVar) {
			t.Errorf("%s's doc tag never names %s. The value of this field depends on that switch, "+
				"so a reader who does not know it exists cannot tell what the field means.",
				field, quarantineEnforceEnvVar)
		}
	}
}
