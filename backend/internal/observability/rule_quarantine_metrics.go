package observability

import (
	"log/slog"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// ruleQuarantineCandidates counts categorization rules a quarantine pass finds broken, by reason
// (Issue #1625).
//
// ⚠ It is NO LONGER a shadow-mode-only counter — but it is still a CLASSIFICATION counter, not a
// refusal counter, and #1625 second fix wave M3 corrected this comment for saying otherwise.
// reportQuarantineCandidates increments it for every rule that classifies, regardless of the kill
// switch. Since Task 2 the reasons for which QuarantineReasonStopsMatching is true are refused in
// RuleMatches *while RULE_QUARANTINE_ENFORCE is on* (the default); with it OFF those same rules
// run, and a zero-condition rule matches every transaction. So a sample under an
// enforcement-class label is a rule that did not run ONLY in the default position — which is
// exactly backwards during the incident the switch exists for. The remaining reason,
// invalid_condition_value_shape, is repaired by read-path coercion and its rules DO still execute
// in either position. Read the label AND the switch, not the metric name, before concluding a
// rule ran. Guarded by TestQuarantineMetricHelpDoesNotAssertRefusal.
//
// ⚠ The `reason` label is a fixed, closed vocabulary from quarantineReason(). It must never
// carry a rule id, a rule name, a user id or a condition value: Prometheus labels are
// high-cardinality poison and, worse here, user financial content.
var ruleQuarantineCandidates = promauto.NewCounterVec(prometheus.CounterOpts{
	Name: "categorization_rule_quarantine_candidates_total",
	Help: "Categorization rules found broken by the quarantine classifier, by reason. It counts CLASSIFICATION, not refusal: whether an enforcement-class label (no_conditions_matches_everything, invalid_condition_logic_defaults_to_and, unnormalizable_condition, uncoercible_condition_value) means the rule was actually stopped depends on RULE_QUARANTINE_ENFORCE — with that switch off those rules still execute, and a zero-condition rule then matches every transaction. invalid_condition_value_shape is never enforced in either position; it is repaired by read-path coercion and always executes. This is a per-OBSERVATION counter (it increments once every time a rule is re-evaluated, e.g. once per sync pass) and therefore cannot answer how many DISTINCT rules are affected — one rule observed 1000 times and 1000 distinct rules look identical here. For that question, see categorization_rule_quarantine_population.",
}, []string{"reason"})

// ⚠ #1625 final review I4: it counts every rule in the table, ENABLED OR NOT
// (ListAllForQuarantineCensus has no `enabled` predicate, deliberately — a disabled broken rule is
// still broken and still shows the size of the repair backlog). So this gauge is NOT "how many
// running rules enforcement stops"; the help string says so.
//
// ruleQuarantinePopulation is the DISTINCT-rule census #1625 Task 18 adds. Unlike
// ruleQuarantineCandidates above (a Counter that increments once per observation and therefore
// conflates "one rule seen 1000 times" with "1000 rules"), this is a Gauge set from a periodic
// full-table census (services.RunRuleQuarantineCensus) that visits every rule EXACTLY once and
// counts distinct rule ids per reason. Set (not Inc) is what makes it self-correcting: a repaired
// rule's count drops on the next census run instead of accumulating forever.
//
// ⚠ Do NOT put rule ids in this (or any) Prometheus label. Cardinality is unbounded (one series
// per rule, forever, since Prometheus never forgets a label combination that was ever
// observed) and a rule id is user data — neither belongs in a metrics label. "reason" is a fixed,
// small, closed vocabulary (AllQuarantineReasons()), which is what makes it safe as a label here.
var ruleQuarantinePopulation = promauto.NewGaugeVec(prometheus.GaugeOpts{
	Name: "categorization_rule_quarantine_population",
	Help: "Current DISTINCT count of categorization rules the quarantine classifier finds broken, by reason, as of the last census. Unlike categorization_rule_quarantine_candidates_total (which counts observations, not rules), this counts rules — set from a full-table scan, not incremented per sync pass. NOTE: the scan has no `enabled` predicate, so this counts DISABLED rules too; it answers 'how many broken rules exist', not 'how many running rules enforcement would stop'.",
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
	// QuarantineUncoercibleValue is the half of invalid_condition_value_shape that read-path
	// coercion cannot repair — an object where a scalar is required, an empty array, a
	// non-numeric string for a number. It is a SEPARATE reason rather than a flag on the old one
	// because QuarantineReasonStopsMatching is a function of the reason alone: one reason that
	// sometimes stops a rule and sometimes does not cannot be answered by it, and the card would
	// have to guess.
	QuarantineUncoercibleValue QuarantineReason = "uncoercible_condition_value"
)

// QuarantineReasonStopsMatching reports whether a rule with this reason is genuinely incapable of
// matching anything — which is the ONLY condition under which a UI may say it is not running.
//
// #1625, membership corrected twice. The first revision reported all four reasons as "Not
// running", which was false for half. The second measured the engine — and generalised from ONE
// evaluator shape per reason, which was also wrong: `OR(merchant_contains(value=42),
// merchant_contains(value="starbucks"))` classifies invalid_condition_value_shape, but matchText
// returns a bare bool rather than an error, so the valid sibling carried the rule and it FIRED
// while the card said "Not running". matchNumber errors on the same class of value; matchText
// does not. Same reason, opposite behaviour.
//
// The membership below is therefore no longer a description of what the evaluators happen to do.
// RuleMatches ENFORCES it: a reason listed here stops the rule outright, at one site, before any
// evaluator runs. Three of the four are safe to enforce because they cannot be produced through
// the API — CategorizationRule.Validate has rejected them since dedac0aa (#346):
//
//	no_conditions             enforced. Unreachable via the API; matches every transaction if run.
//	invalid_condition_logic   enforced. Unreachable via the API; silently degrades to AND if run.
//	unnormalizable_condition  enforced. Largely unreachable; the junk condition can never match,
//	                          and under OR a valid sibling was carrying a rule that is not the
//	                          rule the user wrote.
//	uncoercible_condition_value  enforced. The condition value cannot be repaired into the shape
//	                          its field requires, so there is no honest reading of it.
//
//	invalid_condition_value_shape  NOT enforced — REPAIRED. #346 validated only the condition
//	                          TYPE and never the value shape, so a wrong-typed value persisted
//	                          through the ordinary public API for the entire life of the feature.
//	                          Real user rules live here. Where the intent is unambiguous ("1000"
//	                          for a number, 42 for text) the value is coerced at READ time and the
//	                          rule keeps running, reported as a problem rather than switched off.
//
// So both states survive, with corrected membership. Pinned to real behaviour by
// TestQuarantineEnforcementMatchesTheAdvertisedSet in internal/rules, which drives RuleMatches
// for EVERY evaluator shape (text, number, date, range, enum) under BOTH AND and OR rather than
// one representative shape — generalising from one shape is what produced the defect above.
func QuarantineReasonStopsMatching(r QuarantineReason) bool {
	switch r {
	case QuarantineNoConditions, QuarantineInvalidLogic, QuarantineUnnormalizable,
		QuarantineUncoercibleValue:
		return true
	default:
		return false
	}
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
		QuarantineUncoercibleValue,
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
		// The population gauge gets the same pre-warm treatment and for the same reason: a
		// GaugeVec label combination is likewise absent from /metrics until first Set, so an
		// empty scrape before the first census run would be indistinguishable from "the census
		// has run and found zero quarantined rules."
		ruleQuarantinePopulation.WithLabelValues(string(r)).Set(0)
	}
	// "other" too: an unrecognised reason folds into it, and a scrape that omits the bucket
	// entirely cannot be distinguished from one reporting no unrecognised reasons.
	//
	// The population gauge has NO "other" bucket: unlike RecordRuleQuarantineCandidate (a public
	// entry point that can be handed anything), SetRuleQuarantinePopulation is fed exclusively by
	// the census's own classifier, which returns only members of AllQuarantineReasons() or "" —
	// there is no third-party input for an "other" bucket to defend against.
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

// SetRuleQuarantinePopulation replaces the entire distinct-rule census: every known reason's
// gauge is set to counts[reason], defaulting to 0 for a reason absent from the map. #1625 Task 18.
//
// Set, not Add — this is the whole point. RecordRuleQuarantineCandidate above answers "how many
// times has a broken rule been observed"; this answers "how many distinct rules are broken right
// now", which requires overwriting the prior census wholesale rather than accumulating on top of
// it. A rule that gets repaired between two census runs must see its reason's count go DOWN, not
// stay inflated forever the way an ever-incrementing counter would.
//
// counts is expected to be built by classifying every rule with the SAME classifier
// RecordRuleQuarantineCandidate's callers use (rules.QuarantineReasonFor via
// services.quarantineReason) — see services.RunRuleQuarantineCensus — so a reason key outside
// AllQuarantineReasons() cannot occur from that caller. Defensively, an unknown key is simply
// ignored rather than creating a new unbounded label series: the closed vocabulary is what keeps
// this metric's cardinality bounded, and a caller-side bug must not be able to defeat that by
// handing this function an arbitrary string.
func SetRuleQuarantinePopulation(counts map[QuarantineReason]int) {
	for _, r := range AllQuarantineReasons() {
		ruleQuarantinePopulation.WithLabelValues(string(r)).Set(float64(counts[r]))
	}
}
