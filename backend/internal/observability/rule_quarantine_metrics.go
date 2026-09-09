package observability

import (
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

// QuarantineReason is a CLOSED set. Codex review: the recorder previously took a bare string and
// handed it straight to WithLabelValues, so the "fixed vocabulary" guarantee was convention
// only — one future caller passing a rule name or a condition value would blow up label
// cardinality AND put user financial content into metrics, which are far harder to redact than
// logs. A named type with an allowlist makes that a compile-and-runtime error instead of a
// review comment.
type QuarantineReason string

const (
	QuarantineNoConditions   QuarantineReason = "no_conditions_matches_everything"
	QuarantineInvalidLogic   QuarantineReason = "invalid_condition_logic_defaults_to_and"
	QuarantineUnnormalizable QuarantineReason = "unnormalizable_condition"
	QuarantineInvalidValue   QuarantineReason = "invalid_condition_value_shape"
)

var knownQuarantineReasons = map[QuarantineReason]struct{}{
	QuarantineNoConditions:   {},
	QuarantineInvalidLogic:   {},
	QuarantineUnnormalizable: {},
	QuarantineInvalidValue:   {},
}

// RecordRuleQuarantineCandidate increments the shadow-mode quarantine counter for one reason.
// An unrecognised reason is recorded as "other" rather than as itself: a metric is the wrong
// place to discover that someone invented a label, and dropping the sample entirely would make
// the inventory silently undercount.
func RecordRuleQuarantineCandidate(reason QuarantineReason) {
	if _, ok := knownQuarantineReasons[reason]; !ok {
		ruleQuarantineCandidates.WithLabelValues("other").Inc()
		return
	}
	ruleQuarantineCandidates.WithLabelValues(string(reason)).Inc()
}
