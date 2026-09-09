package observability

// This file is TEST SUPPORT ONLY, added for #1625 Task 18. It exists solely so a test in another
// package (internal/services, where RunRuleQuarantineCensus lives) can observe the otherwise
// unexported ruleQuarantinePopulation gauge. No production code path in this repository may call
// RuleQuarantinePopulationForTest — it exists to make a test assertion possible, not to serve a
// runtime feature. Follows the same pattern as internal/cache/metrics_snapshot.go (#1531 Task
// 11a) for the same reason: a package-private prometheus collector cannot otherwise be asserted
// on from outside its own package's test files.
//
// Unlike that cache counter, this IS a gauge (Set, not Inc), so — unlike Snapshot/Delta over
// there — a single point-in-time read here is meaningful on its own: it reports the CURRENT value
// as of the last SetRuleQuarantinePopulation call, not an accumulating total that needs diffing.

import "github.com/prometheus/client_golang/prometheus/testutil"

// RuleQuarantinePopulationForTest reads the current distinct-rule population gauge for one
// reason. Test support only — see file doc comment.
func RuleQuarantinePopulationForTest(reason QuarantineReason) float64 {
	return testutil.ToFloat64(ruleQuarantinePopulation.WithLabelValues(string(reason)))
}
