package observability

import (
	"log/slog"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// ruleQuarantineCandidates counts categorization rules that a future ENFORCING quarantine pass
// would refuse, broken down by reason (Issue #1625).
//
// Shadow mode changes no matching behaviour on purpose: the blast radius of enforcement is
// unknown until this counter has run against real data, and silently disabling a rule someone
// depends on is a worse failure than the ones #1625 set out to fix. This metric IS the
// inventory — enforcement ships only once the counts are known and the FieldSpecs matrix has
// been widened to accept every legitimate legacy shape it turns up.
//
// ⚠ The `reason` label is a fixed, closed vocabulary from quarantineReason(). It must never
// carry a rule id, a rule name, a user id or a condition value: Prometheus labels are
// high-cardinality poison and, worse here, user financial content.
var ruleQuarantineCandidates = promauto.NewCounterVec(prometheus.CounterOpts{
	Name: "categorization_rule_quarantine_candidates_total",
	Help: "Categorization rules that an enforcing quarantine pass would refuse, by reason. Shadow mode only — these rules still execute.",
}, []string{"reason"})

// QuarantineReason is a CLOSED set. The recorder previously took a bare string and handed it
// straight to WithLabelValues, so the "fixed vocabulary" guarantee was convention only — one
// future caller passing a rule name or a condition value would blow up label cardinality AND put
// user financial content into metrics, which are far harder to redact than logs.
//
// ⚠ The named type is a SPEED BUMP, not a guarantee: Go converts an untyped string constant to
// QuarantineReason implicitly, so `RecordRuleQuarantineCandidate("anything")` still compiles.
// The runtime allowlist below is what actually bounds the label — the type just makes a
// deliberate bypass visible in review. An earlier version of this comment claimed a compile
// error; that was wrong.
type QuarantineReason string

const (
	QuarantineNoConditions   QuarantineReason = "no_conditions_matches_everything"
	QuarantineInvalidLogic   QuarantineReason = "invalid_condition_logic_defaults_to_and"
	QuarantineUnnormalizable QuarantineReason = "unnormalizable_condition"
	QuarantineInvalidValue   QuarantineReason = "invalid_condition_value_shape"
)

// AllQuarantineReasons is the SINGLE source of the vocabulary. The allowlist below is derived
// from it, so the two cannot drift — an earlier version maintained a hand-written map AND a
// hand-written list in the test, which meant adding a fifth constant and forgetting both left
// both lengths at four and the "drift guard" passing. A guard that can be defeated by the exact
// mistake it names is worse than none, because it is trusted.
func AllQuarantineReasons() []QuarantineReason {
	return []QuarantineReason{
		QuarantineNoConditions,
		QuarantineInvalidLogic,
		QuarantineUnnormalizable,
		QuarantineInvalidValue,
	}
}

var knownQuarantineReasons = func() map[QuarantineReason]struct{} {
	m := make(map[QuarantineReason]struct{})
	for _, r := range AllQuarantineReasons() {
		m[r] = struct{}{}
	}
	return m
}()

// RecordRuleQuarantineCandidate increments the shadow-mode quarantine counter for one reason.
// An unrecognised reason is recorded as "other" rather than as itself: a metric is the wrong
// place to discover that someone invented a label, and dropping the sample entirely would make
// the inventory silently undercount.
func RecordRuleQuarantineCandidate(reason QuarantineReason) {
	if _, ok := knownQuarantineReasons[reason]; !ok {
		// LOG it. Folding an unknown reason into "other" silently moves the discovery from a
		// metric to nowhere at all — the operator sees a bucket rise with no way to learn which
		// label was invented. The reason is logged, not labelled: the log can be redacted later,
		// a metric label effectively cannot.
		// ⚠ The rejected value is NOT logged. It is unrecognised precisely because nobody
		// vetted it, so it is the one string here that could carry user content — and a log is
		// no place to discover that. The length is enough to tell a typo'd constant from
		// something that should never have been passed at all.
		slog.Warn("unrecognised rule quarantine reason recorded as \"other\" — add it to AllQuarantineReasons",
			slog.Int("rejected_reason_length", len(reason)))
		ruleQuarantineCandidates.WithLabelValues("other").Inc()
		return
	}
	ruleQuarantineCandidates.WithLabelValues(string(reason)).Inc()
}
