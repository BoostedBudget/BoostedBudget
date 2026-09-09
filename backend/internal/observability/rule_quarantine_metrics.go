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

// RecordRuleQuarantineCandidate increments the shadow-mode quarantine counter for one reason.
func RecordRuleQuarantineCandidate(reason string) {
	ruleQuarantineCandidates.WithLabelValues(reason).Inc()
}
