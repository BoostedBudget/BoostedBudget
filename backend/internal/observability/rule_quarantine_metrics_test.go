package observability

import (
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
# HELP categorization_rule_quarantine_candidates_total Categorization rules that an enforcing quarantine pass would refuse, by reason. Shadow mode only — these rules still execute.
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
# HELP categorization_rule_quarantine_candidates_total Categorization rules that an enforcing quarantine pass would refuse, by reason. Shadow mode only — these rules still execute.
# TYPE categorization_rule_quarantine_candidates_total counter
categorization_rule_quarantine_candidates_total{reason="other"} 1
`
	if err := testutil.CollectAndCompare(ruleQuarantineCandidates, strings.NewReader(want)); err != nil {
		t.Errorf("an unrecognised reason must be folded into \"other\", never used as a label: %v", err)
	}
}

// TestKnownQuarantineReasons_MatchesTheConstants guards the drift the reviewer flagged: the
// allowlist duplicates the const block, so adding a fifth constant and forgetting the map would
// silently record it as "other" while the log said the real reason.
func TestKnownQuarantineReasons_MatchesTheConstants(t *testing.T) {
	declared := []QuarantineReason{
		QuarantineNoConditions,
		QuarantineInvalidLogic,
		QuarantineUnnormalizable,
		QuarantineInvalidValue,
	}
	if len(declared) != len(knownQuarantineReasons) {
		t.Fatalf("knownQuarantineReasons has %d entries, %d constants are declared — one was not updated",
			len(knownQuarantineReasons), len(declared))
	}
	for _, r := range declared {
		if _, ok := knownQuarantineReasons[r]; !ok {
			t.Errorf("constant %q is missing from knownQuarantineReasons, so it would record as \"other\"", r)
		}
	}
}
