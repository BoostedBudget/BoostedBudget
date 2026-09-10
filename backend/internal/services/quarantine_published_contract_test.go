package services

import (
	"context"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/observability"
	"github.com/maxnussbaum/BoostedBudget/internal/rules"
)

// zeroConditionRule classifies as no_conditions_matches_everything: an ENFORCEMENT-class reason
// that is unreachable through the ordinary API, which makes it the cleanest fixture for "the
// engine refuses this rule outright" — and, with the kill switch off, for "the engine runs it and
// it matches EVERY transaction".
func zeroConditionRule(userID, ruleID string) *domain.CategorizationRule {
	return &domain.CategorizationRule{
		ID:               ruleID,
		UserID:           userID,
		Name:             "Broken rule",
		Enabled:          true,
		Conditions:       nil,
		ConditionLogic:   "AND",
		TargetKind:       ptrStr("pfc"),
		TargetPFCPrimary: ptrStr("FOOD_AND_DRINK"),
		TagBehavior:      "add",
		Source:           "manual",
	}
}

func quarantineContractTx() *domain.Transaction {
	merchant := "STARBUCKS COFFEE 42"
	return &domain.Transaction{
		ID:           "t1",
		Name:         merchant,
		MerchantName: &merchant,
		Amount:       -5.50,
		AccountID:    "acc-1",
		Date:         time.Date(2026, 1, 15, 0, 0, 0, 0, time.UTC),
		Account:      &domain.Account{Type: "depository"},
	}
}

// TestQuarantineFalseDoesNotMeanRepaired is #1625's second fix wave H2/M3, and it lives HERE
// rather than in internal/rules for the reason the fix existed: the claim under test is that the
// PUBLISHED read model and the engine cannot describe the same rule differently, and the read
// model is services.annotateQuarantineStatus. An earlier revision of this test reconstructed the
// expected expression locally — `rules.QuarantineEnforced() &&
// observability.QuarantineReasonStopsMatching(reason)` — which meant reverting
// annotateQuarantineStatus to reason-only semantics left it GREEN. It asserted the formula, not
// the product.
//
// So it now drives the service's public read path, and the engine, for the SAME rule, in BOTH
// switch positions, and pins them as exact complements:
//
//	enforcement ON  -> quarantined=true,  and the engine matches nothing
//	enforcement OFF -> quarantined=false, and the engine matches EVERYTHING
//
// The off row is the one that names the test: `quarantined=false` there does not mean the rule
// was repaired, it means the server has stopped refusing a rule that is still broken.
//
// It is the engine half that distinguishes this from
// TestCategorizationRuleService_QuarantineBadgeFollowsTheKillSwitch, which pins the badge alone.
func TestQuarantineFalseDoesNotMeanRepaired(t *testing.T) {
	ctx := context.Background()
	userID := "user-123"
	ruleID := "rule-quarantined"

	if reason := rules.QuarantineReasonFor(zeroConditionRule(userID, ruleID)); reason != observability.QuarantineNoConditions {
		t.Fatalf("fixture classified %q; this test no longer exercises an enforcement-class reason", reason)
	}

	for _, enforcing := range []bool{true, false} {
		t.Run(map[bool]string{true: "enforcing", false: "kill switch off"}[enforcing], func(t *testing.T) {
			restore := rules.SetQuarantineEnforcementForTest(enforcing)
			t.Cleanup(restore)

			rule := zeroConditionRule(userID, ruleID)

			mockRepo := new(mockRuleRepository)
			service := NewCategorizationRuleService(mockRepo, new(MockCustomCategoryRepository))
			mockRepo.On("GetByID", ctx, ruleID).Return(rule, nil)

			published, err := service.GetRuleByID(ctx, userID, ruleID)
			require.NoError(t, err)

			if published.QuarantineReason != string(observability.QuarantineNoConditions) {
				t.Fatalf("the published reason is %q; the switch gates ENFORCEMENT, never diagnosis",
					published.QuarantineReason)
			}
			if published.Quarantined != enforcing {
				t.Fatalf("the published read model reports quarantined=%v with %s=%v; that field is the "+
					"claim \"the server is stopping this rule RIGHT NOW\", so it must follow the switch",
					published.Quarantined, rules.QuarantineEnforceEnvVar, enforcing)
			}

			matched, err := rules.NewRuleEngine().RuleMatches(quarantineContractTx(), rule)
			require.NoError(t, err)

			if matched == published.Quarantined {
				t.Fatalf("the read model published quarantined=%v while the engine returned matched=%v. "+
					"These are the same sentence from two sides and must always be complements: a rule "+
					"the user is told is not running must match nothing, and a rule the user is told is "+
					"running must be able to match.", published.Quarantined, matched)
			}
			if !enforcing && !matched {
				t.Fatal("the fixture is not falsifiable: with enforcement off a zero-condition rule must " +
					"match every transaction, which is the whole reason quarantined=false cannot be read " +
					"as 'repaired'")
			}
		})
	}
}

// TestPublishedQuarantineDocTagsRejectRetractedPhrases is a PHRASE BLOCKLIST, and its name now
// says so. It was called ...AreTrueInBothSwitchStates, which claimed something no test can do: a
// string comparison cannot decide whether English prose is true, and this one never changed or
// observed either switch state. Arbitrary false prose that happens to name the env var passed it.
//
// What it can honestly guarantee is regression-shaped and worth keeping: three specific sentences
// were retracted in #1625 because they were false with RULE_QUARANTINE_ENFORCE off, and they must
// not come back; and the tag must keep naming the switch, because the field's value depends on it.
//
// The behavioural half of the contract — that the field really does follow the switch, in both
// positions, in agreement with the engine — is TestQuarantineFalseDoesNotMeanRepaired directly
// above, which is why the two sit in one file.
func TestPublishedQuarantineDocTagsRejectRetractedPhrases(t *testing.T) {
	typ := reflect.TypeOf(domain.CategorizationRule{})

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
					"reading the spec.", field, phrase, rules.QuarantineEnforceEnvVar)
			}
		}

		if !strings.Contains(doc, rules.QuarantineEnforceEnvVar) {
			t.Errorf("%s's doc tag never names %s. The value of this field depends on that switch, "+
				"so a reader who does not know it exists cannot tell what the field means.",
				field, rules.QuarantineEnforceEnvVar)
		}
	}
}
