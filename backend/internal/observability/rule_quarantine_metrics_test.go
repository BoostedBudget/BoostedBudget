package observability

import (
	"os"
	"strings"
	"testing"

	"github.com/prometheus/client_golang/prometheus/testutil"
)

// The recorder had no test at all, in a repo with a mandatory-TDD rule, and its "other" branch
// is the whole point of the closed vocabulary. These cover both directions.

func TestRecordRuleQuarantineCandidate_UsesTheReasonLabel(t *testing.T) {
	ruleQuarantineCandidates.Reset()
	RecordRuleQuarantineCandidate(QuarantineNoConditions)

	const want = `
# HELP categorization_rule_quarantine_candidates_total Categorization rules found broken by the quarantine classifier, by reason. It counts CLASSIFICATION, not refusal: whether an enforcement-class label (no_conditions_matches_everything, invalid_condition_logic_defaults_to_and, unnormalizable_condition, uncoercible_condition_value) means the rule was actually stopped depends on RULE_QUARANTINE_ENFORCE — with that switch off those rules still execute, and a zero-condition rule then matches every transaction. invalid_condition_value_shape is never enforced in either position; it is repaired by read-path coercion and always executes. This is a per-OBSERVATION counter (it increments once every time a rule is re-evaluated, e.g. once per sync pass) and therefore cannot answer how many DISTINCT rules are affected — one rule observed 1000 times and 1000 distinct rules look identical here. For that question, see categorization_rule_quarantine_population.
# TYPE categorization_rule_quarantine_candidates_total counter
categorization_rule_quarantine_candidates_total{reason="no_conditions_matches_everything"} 1
`
	if err := testutil.CollectAndCompare(ruleQuarantineCandidates, strings.NewReader(want)); err != nil {
		t.Error(err)
	}
}

// An invented reason must NOT become a label. Prometheus labels are high-cardinality poison and,
// worse here, would put user financial content somewhere far harder to redact than a log.
func TestRecordRuleQuarantineCandidate_FoldsAnUnknownReasonIntoOther(t *testing.T) {
	ruleQuarantineCandidates.Reset()
	// This compiles despite the named type — Go converts an untyped string constant implicitly,
	// which is exactly why the runtime allowlist, not the type, is what bounds the label.
	RecordRuleQuarantineCandidate("Coffee at Blue Bottle")

	const want = `
# HELP categorization_rule_quarantine_candidates_total Categorization rules found broken by the quarantine classifier, by reason. It counts CLASSIFICATION, not refusal: whether an enforcement-class label (no_conditions_matches_everything, invalid_condition_logic_defaults_to_and, unnormalizable_condition, uncoercible_condition_value) means the rule was actually stopped depends on RULE_QUARANTINE_ENFORCE — with that switch off those rules still execute, and a zero-condition rule then matches every transaction. invalid_condition_value_shape is never enforced in either position; it is repaired by read-path coercion and always executes. This is a per-OBSERVATION counter (it increments once every time a rule is re-evaluated, e.g. once per sync pass) and therefore cannot answer how many DISTINCT rules are affected — one rule observed 1000 times and 1000 distinct rules look identical here. For that question, see categorization_rule_quarantine_population.
# TYPE categorization_rule_quarantine_candidates_total counter
categorization_rule_quarantine_candidates_total{reason="other"} 1
`
	if err := testutil.CollectAndCompare(ruleQuarantineCandidates, strings.NewReader(want)); err != nil {
		t.Errorf("an unrecognised reason must be folded into \"other\", never used as a label: %v", err)
	}
}

// TestKnownQuarantineReasons_IsDerivedNotDuplicated. The previous version of this test kept its
// OWN copy of the constant list, so forgetting a new constant in both the map and the test left
// both at four and passed — the exact drift it claimed to catch. The allowlist is now built from
// AllQuarantineReasons(), so this asserts the derivation rather than re-stating the data.
func TestKnownQuarantineReasons_IsDerivedNotDuplicated(t *testing.T) {
	all := AllQuarantineReasons()
	if len(all) == 0 {
		t.Fatal("AllQuarantineReasons is empty")
	}
	if len(all) != len(knownQuarantineReasons) {
		t.Fatalf("allowlist has %d entries but AllQuarantineReasons returns %d — they are supposed to be the same data",
			len(knownQuarantineReasons), len(all))
	}
	for _, r := range all {
		if _, ok := knownQuarantineReasons[r]; !ok {
			t.Errorf("%q is in AllQuarantineReasons but not the allowlist", r)
		}
		if r == "" {
			t.Error("an empty reason would record as a blank label")
		}
	}
}

// TestRecordRuleQuarantineCandidate_EveryDeclaredReasonRecordsAsItself is the guard that a
// hand-maintained list could not give: it walks the vocabulary rather than a copy of it, so a
// constant added tomorrow is covered without anyone editing this file.
func TestRecordRuleQuarantineCandidate_EveryDeclaredReasonRecordsAsItself(t *testing.T) {
	for _, r := range AllQuarantineReasons() {
		ruleQuarantineCandidates.Reset()
		RecordRuleQuarantineCandidate(r)

		got := testutil.ToFloat64(ruleQuarantineCandidates.WithLabelValues(string(r)))
		if got != 1 {
			t.Errorf("reason %q did not record under its own label (got %v) — it would have folded into \"other\"", r, got)
		}
	}
}

// TestQuarantineMetric_SeriesExistBeforeAnythingIsRecorded is what makes shadow mode's whole
// justification hold. A CounterVec label series does not appear in /metrics until it is first
// incremented, so before this an empty scrape meant EITHER "no rules would be quarantined" OR
// "this code path never ran" — indistinguishable. The deferral of enforcement rests entirely on
// reading that inventory, so an unreadable inventory made the deferral unjustifiable.
func TestQuarantineMetric_SeriesExistBeforeAnythingIsRecorded(t *testing.T) {
	// ⚠ Deliberately does NOT call initQuarantineSeries() — see seriesCountAtStartup.
	//
	// An earlier revision called Reset() and then initQuarantineSeries() here. That tested the
	// FUNCTION and not the WIRING: deleting `func init()` from the production file left this green,
	// because the test performed the pre-warm itself. This is the second falsifiability bug in this
	// same test; the first was reading label values with WithLabelValues (see below).
	want := len(AllQuarantineReasons()) + 1 // + "other"
	if seriesCountAtStartup != want {
		t.Fatalf("at process start %d series existed, want %d — the pre-warm is not wired into "+
			"package initialization, so a real /metrics scrape before any rule is evaluated would "+
			"omit the labels entirely", seriesCountAtStartup, want)
	}

	// The rest re-establishes the same property against a freshly reset vector, which additionally
	// pins that initQuarantineSeries covers every reason rather than a stale hardcoded subset.
	ruleQuarantineCandidates.Reset()
	initQuarantineSeries()

	// ⚠ COUNT FIRST, and do NOT touch WithLabelValues before counting.
	//
	// The first version of this test looped `testutil.ToFloat64(vec.WithLabelValues(r))` and was
	// NOT falsifiable: WithLabelValues CREATES the series as a side effect of reading it, so the
	// loop instantiated exactly the thing it was supposed to be verifying, and the count passed
	// even with the pre-warm deleted. Caught by mutating initQuarantineSeries and seeing the test
	// stay green.
	if n := testutil.CollectAndCount(ruleQuarantineCandidates); n != want {
		t.Fatalf("expected %d zero-valued series (one per reason plus \"other\") to exist before "+
			"anything is recorded, got %d — an empty scrape would then be indistinguishable from "+
			"code that never ran", want, n)
	}

	// Only now is it safe to read individual values.
	for _, r := range AllQuarantineReasons() {
		if got := testutil.ToFloat64(ruleQuarantineCandidates.WithLabelValues(string(r))); got != 0 {
			t.Errorf("reason %q should start at an explicit 0, got %v", r, got)
		}
	}
}

// TestSetRuleQuarantinePopulation_SetsExactlyTheGivenCounts pins the Set-not-Add contract: calling
// it twice with different counts must leave the SECOND call's values, never a sum of both, and a
// reason omitted from the map must read back as an explicit 0 (a rule that stops being
// quarantined must disappear from the population, not linger at its last nonzero count).
func TestSetRuleQuarantinePopulation_SetsExactlyTheGivenCounts(t *testing.T) {
	ruleQuarantinePopulation.Reset()

	SetRuleQuarantinePopulation(map[QuarantineReason]int{
		QuarantineNoConditions: 3,
		QuarantineInvalidLogic: 1,
	})
	if got := testutil.ToFloat64(ruleQuarantinePopulation.WithLabelValues(string(QuarantineNoConditions))); got != 3 {
		t.Errorf("QuarantineNoConditions: got %v, want 3", got)
	}
	if got := testutil.ToFloat64(ruleQuarantinePopulation.WithLabelValues(string(QuarantineUnnormalizable))); got != 0 {
		t.Errorf("a reason absent from the map must read 0, got %v", got)
	}

	// Second census: no_conditions dropped to 0 (repaired), unnormalizable rose to 2 (newly seen).
	SetRuleQuarantinePopulation(map[QuarantineReason]int{
		QuarantineUnnormalizable: 2,
	})
	if got := testutil.ToFloat64(ruleQuarantinePopulation.WithLabelValues(string(QuarantineNoConditions))); got != 0 {
		t.Errorf("QuarantineNoConditions must drop to 0 on a census that no longer reports it, got %v (Add semantics would leave 3)", got)
	}
	if got := testutil.ToFloat64(ruleQuarantinePopulation.WithLabelValues(string(QuarantineUnnormalizable))); got != 2 {
		t.Errorf("QuarantineUnnormalizable: got %v, want 2", got)
	}
}

// TestQuarantinePopulation_SeriesExistBeforeAnyCensusRuns is TestQuarantineMetric_SeriesExistBeforeAnythingIsRecorded's
// counterpart for the #1625 Task 18 gauge: an empty scrape before the first census must not be
// mistaken for "the census ran and found nothing quarantined".
func TestQuarantinePopulation_SeriesExistBeforeAnyCensusRuns(t *testing.T) {
	want := len(AllQuarantineReasons()) // no "other" bucket for the population gauge — see its doc comment
	if populationSeriesCountAtStartup != want {
		t.Fatalf("at process start %d population series existed, want %d — the pre-warm is not "+
			"wired into package initialization, so a real /metrics scrape before the first census "+
			"run would omit the labels entirely", populationSeriesCountAtStartup, want)
	}

	ruleQuarantinePopulation.Reset()
	initQuarantineSeries()

	if n := testutil.CollectAndCount(ruleQuarantinePopulation); n != want {
		t.Fatalf("expected %d zero-valued population series to exist before any census has run, "+
			"got %d", want, n)
	}

	for _, r := range AllQuarantineReasons() {
		if got := testutil.ToFloat64(ruleQuarantinePopulation.WithLabelValues(string(r))); got != 0 {
			t.Errorf("reason %q population should start at an explicit 0, got %v", r, got)
		}
	}
}

// seriesCountAtStartup and populationSeriesCountAtStartup are captured in TestMain, which runs
// AFTER every package-level var and every init() but BEFORE any test — the only point at which the
// process's real startup state is still observable. A package-level var would be too early (vars
// initialize before init functions) and any Test function too late (an earlier test may have
// called Reset()).
var (
	seriesCountAtStartup           int
	populationSeriesCountAtStartup int
)

func TestMain(m *testing.M) {
	seriesCountAtStartup = testutil.CollectAndCount(ruleQuarantineCandidates)
	populationSeriesCountAtStartup = testutil.CollectAndCount(ruleQuarantinePopulation)
	os.Exit(m.Run())
}

// TestQuarantineMetricHelpDoesNotAssertRefusal is #1625 second fix wave M3.
//
// The counter increments on CLASSIFICATION (rule_application_service.reportQuarantineCandidates
// calls it for every rule with a reason, regardless of the kill switch), and its help string said
// the enforcement-class labels identified rules that "did not run". With RULE_QUARANTINE_ENFORCE
// off those rules DO run — a zero-condition rule matches every transaction — so an operator
// reading this metric during the exact incident the switch exists for was being told the
// opposite of what was happening.
//
// The chosen semantics (see internal/services/quarantine_published_contract_test.go) is that
// "stopped right now" is switch-dependent, so no surface may assert it from the reason alone.
// This help string therefore describes CLASSIFICATION and names the switch that decides refusal.
func TestQuarantineMetricHelpDoesNotAssertRefusal(t *testing.T) {
	help := helpOf(t, ruleQuarantineCandidates.WithLabelValues(string(QuarantineNoConditions)).Desc().String())

	for _, retracted := range []string{
		"Reasons that stop matching are enforced in RuleMatches",
		"did not run",
	} {
		if strings.Contains(help, retracted) {
			t.Errorf("the help string still asserts %q. That is true only while "+
				"RULE_QUARANTINE_ENFORCE is on; with it off the rule runs and the metric describes "+
				"the opposite of what happened.", retracted)
		}
	}

	if !strings.Contains(help, "RULE_QUARANTINE_ENFORCE") {
		t.Error("the help string never names RULE_QUARANTINE_ENFORCE. Whether a sample under an " +
			"enforcement-class label means the rule was refused depends entirely on that switch, " +
			"so an operator who does not know it exists cannot read this metric correctly.")
	}
}

// helpOf pulls the help text out of a prometheus Desc's String() form, which is
// `Desc{fqName: "...", help: "...", constLabels: {}, variableLabels: {...}}`. Reading the Desc
// rather than the Go literal is what makes this a check on what is PUBLISHED.
func helpOf(t *testing.T, desc string) string {
	t.Helper()
	const marker = `help: "`
	i := strings.Index(desc, marker)
	if i < 0 {
		t.Fatalf("could not find a help field in %q — this guard is scanning nothing", desc)
	}
	rest := desc[i+len(marker):]
	j := strings.Index(rest, `", constLabels`)
	if j < 0 {
		t.Fatalf("could not find the end of the help field in %q", desc)
	}
	return rest[:j]
}
