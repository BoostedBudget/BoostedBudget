package rules

import (
	"reflect"
	"testing"
	"time"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/observability"
)

// quarantineTx is deliberately rich enough to exercise EVERY evaluator shape: text, number,
// date, range and enum. The merchant name ends in "42" so that a numeric text value coerces into
// something that genuinely MATCHES — a coercion test whose repaired value cannot match would
// pass just as well against no coercion at all.
func quarantineTx() *domain.Transaction {
	merchant := "STARBUCKS COFFEE 42"
	channel := "in store"
	return &domain.Transaction{
		ID:             "t1",
		Name:           merchant,
		MerchantName:   &merchant,
		Amount:         -5.50,
		PaymentChannel: &channel,
		AccountID:      "acc-1",
		Date:           time.Date(2026, 1, 15, 0, 0, 0, 0, time.UTC),
		Account:        &domain.Account{Type: "depository"},
	}
}

// validSibling MATCHES quarantineTx on its own. Under OR it is what carries a rule whose other
// condition is broken — which is exactly how the bug this test exists to catch presented:
// OR(merchant_contains(42), merchant_contains("starbucks")) classified
// invalid_condition_value_shape, matchText returned a bare false with no error, evaluateOR
// reached the sibling, and the rule FIRED while the card said "Not running".
func validSibling() domain.RuleCondition {
	return domain.RuleCondition{Type: domain.ConditionMerchantContains, Value: "starbucks"}
}

type shapeCase struct {
	shape string
	cond  domain.RuleCondition
}

// Every reason below is listed per EVALUATOR SHAPE, not once per reason. Round 2 of #1625
// measured one representative shape and generalised, and the generalisation was false: matchText
// returns a bare bool where matchNumber returns (bool, error), so two conditions with the same
// quarantine reason behaved oppositely at runtime. A single-shape table cannot see that.

func unnormalizableShapes() []shapeCase {
	return []shapeCase{
		{"unknown fused type", domain.RuleCondition{Type: "a_type_that_does_not_exist", Value: "x"}},
		{"unknown field", domain.RuleCondition{Field: "not_a_field", Operator: "eq", Value: "x"}},
		{"text operator on a number field", domain.RuleCondition{Field: domain.FieldAmount, Operator: "contains", Value: "5"}},
		{"range operator on a text field", domain.RuleCondition{Field: domain.FieldMerchant, Operator: "between", Value: "x"}},
		{"date operator on an enum field", domain.RuleCondition{Field: domain.FieldDirection, Operator: "between", Value: "x"}},
		{"field with no operator at all", domain.RuleCondition{Field: domain.FieldMerchant, Value: "starbucks"}},
	}
}

func uncoercibleShapes() []shapeCase {
	return []shapeCase{
		{"text: an object where a string is required",
			domain.RuleCondition{Field: domain.FieldMerchant, Operator: "contains", Value: map[string]interface{}{"a": 1}}},
		{"text: an empty array names nothing",
			domain.RuleCondition{Field: domain.FieldMerchant, Operator: "in", Value: []interface{}{}}},
		{"number: a non-numeric string",
			domain.RuleCondition{Field: domain.FieldAmount, Operator: "gt", Value: "not-a-number"}},
		{"number: an array where a scalar is required",
			domain.RuleCondition{Field: domain.FieldSignedAmount, Operator: "lt", Value: []interface{}{1, 2}}},
		{"number_range: a scalar where an object is required",
			domain.RuleCondition{Field: domain.FieldAmount, Operator: "between", Value: "not-an-object"}},
		{"number_range: an inverted range",
			domain.RuleCondition{Field: domain.FieldAmount, Operator: "between", Value: map[string]interface{}{"min": 500.0, "max": 1.0}}},
		{"date: a number where a calendar day is required",
			domain.RuleCondition{Field: domain.FieldDate, Operator: "eq", Value: 20260115}},
		{"date_range: an unreadable bound",
			domain.RuleCondition{Field: domain.FieldDate, Operator: "between", Value: map[string]interface{}{"start": "yesterday"}}},
		{"enum: a value outside the closed set",
			domain.RuleCondition{Field: domain.FieldDirection, Operator: "eq", Value: "inflows"}},
		{"enum: an unknown PFC primary",
			domain.RuleCondition{Field: domain.FieldPFCPrimary, Operator: "eq", Value: "FOOD"}},
	}
}

// coercibleShapes are the REPAIRED half. Each is a value ValidateShape rejects, whose one honest
// reading MATCHES quarantineTx — so a rule containing it must still fire, under AND as well as
// under OR.
func coercibleShapes() []shapeCase {
	return []shapeCase{
		{"text: a number for a text field",
			domain.RuleCondition{Field: domain.FieldMerchant, Operator: "contains", Value: 42.0}},
		{"text: an integer for a text field",
			domain.RuleCondition{Field: domain.FieldMerchant, Operator: "ends_with", Value: 42}},
		{"number: a quoted number",
			domain.RuleCondition{Field: domain.FieldAmount, Operator: "gt", Value: "1"}},
		{"number: a quoted negative number for the signed field",
			domain.RuleCondition{Field: domain.FieldSignedAmount, Operator: "lt", Value: "-1"}},
		{"number_range: quoted bounds",
			domain.RuleCondition{Field: domain.FieldAmount, Operator: "between", Value: map[string]interface{}{"min": "1", "max": "10"}}},
		{"date: an unpadded day",
			domain.RuleCondition{Field: domain.FieldDate, Operator: "gte", Value: "2026-1-1"}},
		{"date: an RFC3339 timestamp",
			domain.RuleCondition{Field: domain.FieldDate, Operator: "lte", Value: "2026-12-31T00:00:00Z"}},
		{"date_range: unpadded bounds",
			domain.RuleCondition{Field: domain.FieldDate, Operator: "between",
				Value: map[string]interface{}{"start": "2026-1-1", "end": "2026-12-31"}}},
	}
}

// ⚠ The enum shape has NO coercible member, on purpose. Coercion fixes a value's TYPE; a value
// outside a closed set is a typo, and coercing `1` into "1" for `direction` would hand the
// evaluator something the write path still refuses. Asserted rather than left implicit so that
// loosening the enum check later fails here.
func TestEnumShapeIsNeverRepairedOnlyStopped(t *testing.T) {
	for _, c := range []domain.RuleCondition{
		domain.RuleCondition{Field: domain.FieldDirection, Operator: "eq", Value: 1},
		domain.RuleCondition{Field: domain.FieldDirection, Operator: "in", Value: []interface{}{"outflows"}},
		domain.RuleCondition{Field: domain.FieldPFCDetailed, Operator: "eq", Value: "FOOD_AND_DRINK"},
	} {
		if _, ok := c.CoercedValue(); ok {
			t.Errorf("%+v was repaired — a value outside a closed enum is a typo, and inventing a "+
				"member of the set is guessing at what the user meant", c)
		}
	}
}

// ⚠ THE INVARIANT THE GATE EXISTS FOR, asserted on the VALUE the evaluator receives rather than
// on the match result. A match-only assertion would pass while the value was still being
// rewritten underneath it — the rewrite only shows up on transactions nobody thought to put in
// the fixture.
//
// Coercion runs ONLY on a condition ValidateShape REJECTS. The first version of Task 2 ran it on
// every condition, which changed the meaning of rules that were never broken and that the live
// write path still accepts.
func TestAValidatingConditionReachesTheEvaluatorUntouched(t *testing.T) {
	untouched := []domain.RuleCondition{
		{Field: domain.FieldMerchant, Operator: "contains", Value: "starbucks"},
		// ⚠ The one that motivated the gate: textValues DROPS the 42 and still reports ok, so
		// this PASSES ValidateShape and is classified healthy. Coercing it to ["uber","42"] would
		// make the rule match transactions it did not match yesterday.
		{Field: domain.FieldMerchant, Operator: "in", Value: []interface{}{"uber", 42}},
		{Field: domain.FieldAccountID, Operator: "in", Value: []interface{}{"acc-1", "acc-2"}},
		{Field: domain.FieldAmount, Operator: "gt", Value: 50.0},
		{Field: domain.FieldAmount, Operator: "between", Value: map[string]interface{}{"min": 1.0, "max": 2.0}},
		{Field: domain.FieldDate, Operator: "eq", Value: "2026-01-31"},
		{Field: domain.FieldDate, Operator: "between", Value: map[string]interface{}{"start": "2026-01-01"}},
		{Field: domain.FieldDirection, Operator: "eq", Value: "outflow"},
	}

	for _, c := range untouched {
		if err := c.ValidateShape(); err != nil {
			t.Fatalf("fixture %+v is meant to be one the validator ACCEPTS: %v", c, err)
		}
		got := repairedForEvaluation(c)
		if !reflect.DeepEqual(got.Value, c.Value) {
			t.Errorf("a condition the validator accepts was rewritten before evaluation: %#v -> %#v",
				c.Value, got.Value)
		}
	}
}

// The other side of the gate: a condition the validator REJECTS is still repaired.
func TestARejectedConditionIsStillRepairedBeforeEvaluation(t *testing.T) {
	c := domain.RuleCondition{Field: domain.FieldAmount, Operator: "gt", Value: "1000"}
	if err := c.ValidateShape(); err == nil {
		t.Fatal("fixture is meant to be one the validator rejects")
	}
	if got := repairedForEvaluation(c).Value; got != 1000.0 {
		t.Errorf("a broken value reached the evaluator as %#v — the gate has closed on the rows "+
			"coercion exists for", got)
	}
}

// This test's own earlier revision asserted the opposite of what it asserts now, and has been
// INVERTED. The first version of Task 2 asserted that coercion rescued the dropped member; that
// was a semantics change to rules the
// product sanctions today, made outside the quarantine set. The behaviour being preserved — and
// therefore the behaviour under test — is the silent drop.
//
// This is NOT an endorsement of it. `["uber", 42]` says one thing and matches another, which is
// a real defect. It belongs in its own change, at the WRITE path, rejecting new mixed arrays —
// not riding in on a quarantine commit that promises not to change what existing rules match.
func TestMixedArrayMemberIsStillSilentlyDropped(t *testing.T) {
	c := domain.RuleCondition{Field: domain.FieldMerchant, Operator: "contains", Value: []interface{}{"uber", 42}}
	if err := c.ValidateShape(); err != nil {
		t.Fatalf("a mixed array is accepted by the validator today: %v", err)
	}

	registry := NewConditionRegistry()
	matched, err := registry.Evaluate(quarantineTx(), c) // merchant is "STARBUCKS COFFEE 42"
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if matched {
		t.Error("the dropped numeric member was resurrected by coercion: this rule matched a " +
			"transaction on \"42\", which it did not match before #1625 Task 2")
	}

	// And the value itself, not merely the outcome.
	if got := repairedForEvaluation(c).Value; !reflect.DeepEqual(got, c.Value) {
		t.Errorf("the stored array was rewritten to %#v before evaluation", got)
	}
}

func ruleWith(logic string, conds ...domain.RuleCondition) *domain.CategorizationRule {
	return &domain.CategorizationRule{ID: "r", Enabled: true, ConditionLogic: logic, Conditions: conds}
}

// TestQuarantineEnforcementMatchesTheAdvertisedSet drives RuleMatches for every reason × every
// evaluator shape × both AND and OR, and compares the OBSERVED outcome with what
// QuarantineReasonStopsMatching advertises and the card renders. It is the guard that the read
// model and the engine cannot describe the same rule differently.
//
// OR always carries a valid sibling that matches on its own, so for an enforced reason the OR row
// is the falsifiable one: without enforcement the sibling fires the rule.
func TestQuarantineEnforcementMatchesTheAdvertisedSet(t *testing.T) {
	tx := quarantineTx()
	engine := NewRuleEngine()

	type reasonGroup struct {
		reason observability.QuarantineReason
		shapes []shapeCase
	}

	groups := []reasonGroup{
		{observability.QuarantineNoConditions, []shapeCase{{"no conditions at all", domain.RuleCondition{}}}},
		{observability.QuarantineUnnormalizable, unnormalizableShapes()},
		{observability.QuarantineUncoercibleValue, uncoercibleShapes()},
		{observability.QuarantineInvalidValue, coercibleShapes()},
	}

	for _, g := range groups {
		for _, sc := range g.shapes {
			for _, logic := range []string{"AND", "OR"} {
				t.Run(string(g.reason)+"/"+sc.shape+"/"+logic, func(t *testing.T) {
					var rule *domain.CategorizationRule
					if g.reason == observability.QuarantineNoConditions {
						rule = ruleWith(logic)
					} else if logic == "OR" {
						rule = ruleWith(logic, sc.cond, validSibling())
					} else {
						rule = ruleWith(logic, sc.cond)
					}

					if got := QuarantineReasonFor(rule); got != g.reason {
						t.Fatalf("classified %q, want %q — the table no longer tests what it claims",
							got, g.reason)
					}

					matched, err := engine.RuleMatches(tx, rule)
					if err != nil {
						// An evaluator error skips the rule in EvaluateRules, which the user
						// sees as "did not match".
						matched = false
					}

					stops := observability.QuarantineReasonStopsMatching(g.reason)
					if stops && matched {
						t.Errorf("%s is advertised as stopping the rule, but it MATCHED — the read "+
							"model reports quarantined and the card says \"Not running\", which is false",
							g.reason)
					}
					if !stops && !matched {
						t.Errorf("%s is advertised as still running, but the rule did NOT match — "+
							"either coercion failed to repair a value with one honest reading, or "+
							"QuarantineReasonStopsMatching no longer describes reality", g.reason)
					}
				})
			}
		}
	}
}

// invalid_condition_logic is shape-independent — it is the LOGIC that is broken, not a value — so
// it gets its own table. It is the reason with the largest behavioural change: an unrecognised
// logic silently fell back to AND and the rule kept running and kept recategorising.
func TestInvalidConditionLogicStopsEveryShape(t *testing.T) {
	tx := quarantineTx()
	engine := NewRuleEngine()

	healthy := []shapeCase{
		{"text", validSibling()},
		{"number", domain.RuleCondition{Type: domain.ConditionAmountGreaterThan, Value: 1.0}},
		{"number_range", domain.RuleCondition{Type: domain.ConditionAmountRange, Value: map[string]interface{}{"min": 1.0, "max": 10.0}}},
		{"date_range", domain.RuleCondition{Type: domain.ConditionDateRange, Value: map[string]interface{}{"start": "2026-01-01"}}},
		{"enum", domain.RuleCondition{Field: domain.FieldDirection, Operator: "eq", Value: "inflow"}},
	}

	for _, sc := range healthy {
		t.Run(sc.shape, func(t *testing.T) {
			rule := ruleWith("XOR", sc.cond)
			if got := QuarantineReasonFor(rule); got != observability.QuarantineInvalidLogic {
				t.Fatalf("classified %q, want invalid_condition_logic", got)
			}
			// Falsifiability: every condition here MATCHES, so without enforcement the AND
			// fallback fires the rule.
			if matched, err := engine.RuleMatches(tx, ruleWith("AND", sc.cond)); err != nil || !matched {
				t.Fatalf("fixture is not falsifiable: the same condition under AND did not match (%v, %v)",
					matched, err)
			}
			matched, err := engine.RuleMatches(tx, rule)
			if err != nil {
				matched = false
			}
			if matched {
				t.Error("a rule whose condition_logic is neither AND nor OR still matched — it is " +
					"running as something the user never wrote")
			}
		})
	}
}

// Enforcement must not leak. A healthy rule keeps matching, and a healthy rule that does not
// match keeps not matching.
func TestEnforcementDoesNotLeakIntoHealthyRules(t *testing.T) {
	tx := quarantineTx()
	engine := NewRuleEngine()

	for _, tc := range []struct {
		name string
		rule *domain.CategorizationRule
		want bool
	}{
		{"AND of two matching conditions", ruleWith("AND", validSibling(),
			domain.RuleCondition{Type: domain.ConditionAmountGreaterThan, Value: 1.0}), true},
		{"OR with one matching condition", ruleWith("OR",
			domain.RuleCondition{Type: domain.ConditionMerchantContains, Value: "wholefoods"},
			validSibling()), true},
		{"AND that genuinely does not match", ruleWith("AND",
			domain.RuleCondition{Type: domain.ConditionMerchantContains, Value: "wholefoods"}), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if r := QuarantineReasonFor(tc.rule); r != "" {
				t.Fatalf("healthy fixture was classified %q", r)
			}
			matched, err := engine.RuleMatches(tx, tc.rule)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if matched != tc.want {
				t.Errorf("matched=%v, want %v", matched, tc.want)
			}
		})
	}
}

// TestQuarantineEnforcementKillSwitchCoversEveryEnforcedReason. An untested kill switch is not a
// kill switch. Each case is a rule that MATCHES once enforcement is off, so the switch is
// observed doing something rather than merely being read.
func TestQuarantineEnforcementKillSwitchCoversEveryEnforcedReason(t *testing.T) {
	tx := quarantineTx()
	engine := NewRuleEngine()

	cases := []struct {
		reason observability.QuarantineReason
		rule   *domain.CategorizationRule
	}{
		{observability.QuarantineNoConditions, ruleWith("AND")},
		{observability.QuarantineInvalidLogic, ruleWith("XOR", validSibling())},
		{observability.QuarantineUnnormalizable,
			ruleWith("OR", domain.RuleCondition{Type: "a_type_that_does_not_exist", Value: "x"}, validSibling())},
		// The sibling comes FIRST here on purpose: with enforcement off, an uncoercible number
		// makes matchNumber return an error, and OR is evaluated in order.
		{observability.QuarantineUncoercibleValue,
			ruleWith("OR", validSibling(), domain.RuleCondition{Field: domain.FieldAmount, Operator: "gt", Value: "not-a-number"})},
	}

	// Every reason that stops matching must appear above, or the switch has grown a hole.
	covered := map[observability.QuarantineReason]bool{}
	for _, c := range cases {
		covered[c.reason] = true
	}
	for _, r := range observability.AllQuarantineReasons() {
		if observability.QuarantineReasonStopsMatching(r) && !covered[r] {
			t.Errorf("%q is enforced but the kill switch is not tested against it", r)
		}
	}

	for _, c := range cases {
		t.Run(string(c.reason), func(t *testing.T) {
			if got := QuarantineReasonFor(c.rule); got != c.reason {
				t.Fatalf("classified %q, want %q", got, c.reason)
			}
			if matched, _ := engine.RuleMatches(tx, c.rule); matched {
				t.Fatal("the rule matched WITH enforcement on — the fixture cannot show the switch doing anything")
			}

			original := enforceQuarantine
			enforceQuarantine = false
			t.Cleanup(func() { enforceQuarantine = original })

			matched, err := engine.RuleMatches(tx, c.rule)
			if err != nil {
				t.Fatalf("unexpected error with enforcement disabled: %v", err)
			}
			if !matched {
				t.Errorf("with RULE_QUARANTINE_ENFORCE=false the pre-#1625 behaviour must return for %q",
					c.reason)
			}
		})
	}
}

// The kill switch gates ENFORCEMENT, never COERCION. Turning off a repair would restore a defect,
// not a behaviour, so a coercible rule matches identically in both positions.
func TestKillSwitchDoesNotDisableCoercion(t *testing.T) {
	tx := quarantineTx()
	engine := NewRuleEngine()
	rule := ruleWith("AND", domain.RuleCondition{Field: domain.FieldAmount, Operator: "gt", Value: "1"})

	for _, enforcing := range []bool{true, false} {
		original := enforceQuarantine
		enforceQuarantine = enforcing
		matched, err := engine.RuleMatches(tx, rule)
		enforceQuarantine = original
		if err != nil {
			t.Fatalf("enforcement=%v: unexpected error: %v", enforcing, err)
		}
		if !matched {
			t.Errorf("enforcement=%v: a coercible value stopped matching — coercion must not be "+
				"gated by the enforcement switch", enforcing)
		}
	}
}

// Coercion is READ-PATH ONLY. rule_execution_history.definition_snapshot rows are immutable audit
// records, and the live rule is shared across every transaction in a batch — a repair written
// back would change what the next transaction is matched against.
func TestEvaluationNeverRewritesTheStoredCondition(t *testing.T) {
	engine := NewRuleEngine()
	rule := ruleWith("OR",
		domain.RuleCondition{Field: domain.FieldAmount, Operator: "gt", Value: "1"},
		domain.RuleCondition{Type: domain.ConditionMerchantContains, Value: 42.0},
	)

	if _, err := engine.RuleMatches(quarantineTx(), rule); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if got := rule.Conditions[0].Value; got != "1" {
		t.Errorf("the stored numeric value was rewritten to %#v", got)
	}
	if got := rule.Conditions[1].Value; got != 42.0 {
		t.Errorf("the stored text value was rewritten to %#v", got)
	}
	if rule.Conditions[1].Field != "" {
		t.Error("evaluation normalized the live rule in place")
	}
}

// TestQuarantineEnforcementReadsTheEnvVarNotJustTheGoVar is #1625's final-review I2.
//
// Every other kill-switch test above assigns `enforceQuarantine` directly, which proves the GATE
// works and says nothing about how the variable is derived. The defect lived exactly there:
// `os.Getenv("RULE_QUARANTINE_ENFORCE") != "false"` treated `0`, `f` and `False` as ON while the
// comment beside it claimed "the same kill-switch convention as CACHE_ENABLED" — which parses all
// three as OFF. An operator reaching for this switch is, by definition, mid-incident; the switch
// silently not working is the worst possible time for it.
//
// ⚠ Drives the ENV VAR through the real parser. A table that set the Go var would be green under
// the defect.
func TestQuarantineEnforcementReadsTheEnvVarNotJustTheGoVar(t *testing.T) {
	cases := []struct {
		value string
		want  bool
		why   string
	}{
		{"", true, "unset means the default, which is ON"},
		{"false", false, "the spelling the doc comment always promised"},
		{"0", false, "ParseBool's numeric false — silently kept enforcement ON before I2"},
		{"f", false, "ParseBool's short false — same"},
		{"False", false, "ParseBool is case-sensitive only about these exact spellings"},
		{"FALSE", false, "same"},
		{"true", true, "explicitly on"},
		{"1", true, "explicitly on"},
		{"no", true, "NOT a ParseBool spelling: unparseable fails SAFE (enforcement stays on) " +
			"rather than being read as off"},
		{"disabled", true, "same — a typo must never silently disable enforcement"},
	}

	for _, c := range cases {
		t.Run(c.value, func(t *testing.T) {
			t.Setenv(quarantineEnforceEnvVar, c.value)
			if got := quarantineEnforcementFromEnv(); got != c.want {
				t.Errorf("%s=%q parsed as enforcing=%v, want %v — %s",
					quarantineEnforceEnvVar, c.value, got, c.want, c.why)
			}
		})
	}
}

// TestQuarantineEnforcedIsTheOneAnswerTheReadModelUses is #1625's final-review I3.
//
// The exported accessor exists so the READ model (the "Not running" badge) and the ENGINE cannot
// disagree while the kill switch is off. Asserted here on the accessor itself; the services-side
// half is asserted in package services, which is where the divergence actually was.
func TestQuarantineEnforcedIsTheOneAnswerTheReadModelUses(t *testing.T) {
	original := enforceQuarantine
	t.Cleanup(func() { enforceQuarantine = original })

	for _, want := range []bool{true, false} {
		enforceQuarantine = want
		if got := QuarantineEnforced(); got != want {
			t.Errorf("QuarantineEnforced() = %v while the engine's own gate is %v — the read model "+
				"would describe a rule the engine treats differently", got, want)
		}
	}
}
