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

// QuarantineReasonStopsMatching reports whether a rule with this reason is genuinely incapable of
// matching anything — which is the ONLY condition under which a UI may say it is not running.
//
// #1625, corrected after measuring the engine rather than reasoning about it. The four reasons do
// NOT behave alike, and an earlier revision of this code reported all of them as "Not running",
// which was false for half:
//
//	no_conditions            never matches — enforced outright in RuleMatches.
//	invalid_condition_value  never matches — the evaluator ERRORS, so the whole rule is skipped.
//	                         Not enforcement, but the same observable outcome.
//	invalid_condition_logic  STILL MATCHES. An unrecognised logic value falls back to AND.
//	unnormalizable_condition STILL MATCHES under OR: the junk condition returns (false, nil) and a
//	                         valid sibling can carry the rule on its own. Under AND it does not.
//
// So the last two are reported as "has a problem", never as "not running". Telling a user their
// rule is off while it is quietly recategorising their transactions is the exact failure this
// ticket exists to remove.
//
// Pinned to real behaviour by TestQuarantineEnforcementMatchesTheAdvertisedSet in internal/rules,
// which drives RuleMatches for every reason instead of trusting this list.
func QuarantineReasonStopsMatching(r QuarantineReason) bool {
	return r == QuarantineNoConditions || r == QuarantineInvalidValue
}

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

// initQuarantineSeries creates an explicit zero-valued series for every reason at startup.
//
// A CounterVec label combination is absent from /metrics until first incremented, so without this
// an empty scrape is ambiguous: no rules would be quarantined, or this code never executed? Shadow
// mode exists solely to build that inventory before enforcement, so the ambiguity made the whole
// deferral unjustifiable — a reviewer called it "unsafe" on exactly that ground.
//
// Derived from AllQuarantineReasons(), so a reason added later is pre-warmed without anyone
// remembering this function exists.
func initQuarantineSeries() {
	for _, r := range AllQuarantineReasons() {
		ruleQuarantineCandidates.WithLabelValues(string(r)).Add(0)
	}
	// "other" too: an unrecognised reason folds into it, and a scrape that omits the bucket
	// entirely cannot be distinguished from one reporting no unrecognised reasons.
	ruleQuarantineCandidates.WithLabelValues("other").Add(0)
}

func init() { initQuarantineSeries() }

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
