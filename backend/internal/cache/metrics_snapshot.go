package cache

// This file is TEST SUPPORT ONLY, added for issue #1531 Task 11a. It exists solely so a test in
// another package (internal/services, where Task 11's cache-parity harness lives) can observe the
// otherwise-unexported cacheRequestsTotal counter. No production code path in this repository may
// call Snapshot or Delta — they exist to make a test assertion possible, not to serve a runtime
// feature. See task-11-report.md's "Recommended breakdown" for why this had to be split out as its
// own prerequisite task before the parity harness could be written at all.
//
// Prometheus counters are process-global and monotonic: they only ever go up, and they are shared
// by every test that runs in the same test binary. Snapshot() therefore returns an ABSOLUTE
// point-in-time read, which is meaningless on its own — the caller MUST take one Snapshot() before
// the operation under test, another Snapshot() after it, and diff them with Delta. That diff (not
// either absolute snapshot) is what proves "exactly 1 miss then exactly 1 hit for this one
// operation", immune to whatever count some earlier test in the same process already left behind.
//
// ⚠ That immunity is to SEQUENTIAL history only, not to CONCURRENT traffic: the before/op/after
// sequence is not atomic with respect to the rest of the process. Any test using Snapshot/Delta to
// assert an exact count MUST NOT call t.Parallel() (directly or via a parallel subtest), and must
// not run alongside another goroutine that exercises this same Cache concurrently — otherwise
// unrelated traffic landing between the two snapshots can inflate or (if it happens to target the
// same namespace/result) mask the delta being asserted on. Task 11b's parity harness in particular
// must run its before-Snapshot/operation/after-Snapshot sequence with no concurrent cache traffic
// of its own.

import "github.com/prometheus/client_golang/prometheus/testutil"

// NamespaceCounts is a point-in-time (or, after Delta, a difference-of-two-points-in-time) read of
// cacheRequestsTotal for one namespace, broken out by every result label that metric can carry.
type NamespaceCounts struct {
	Hits             float64
	Misses           float64
	Errors           float64
	BypassDisabled   float64
	BypassGenError   float64
	BypassCircuit    float64
	BypassTombstoned float64
}

// CountsSnapshot is a snapshot of cacheRequestsTotal across every registered namespace, taken by
// Snapshot.
type CountsSnapshot map[Namespace]NamespaceCounts

// Snapshot returns the current (absolute, monotonic) cacheRequestsTotal counts for every
// registered namespace. Call it once before and once after the operation under test, then use
// Delta to get the change attributable to that operation alone — see the package-file doc comment
// above for why a bare Snapshot value is not itself a useful assertion target.
func Snapshot() CountsSnapshot {
	out := make(CountsSnapshot, len(namespaceRegistry))
	for _, ns := range AllNamespaces() {
		nsStr := string(ns)
		out[ns] = NamespaceCounts{
			Hits:             testutil.ToFloat64(cacheRequestsTotal.WithLabelValues(nsStr, resultHit)),
			Misses:           testutil.ToFloat64(cacheRequestsTotal.WithLabelValues(nsStr, resultMiss)),
			Errors:           testutil.ToFloat64(cacheRequestsTotal.WithLabelValues(nsStr, resultError)),
			BypassDisabled:   testutil.ToFloat64(cacheRequestsTotal.WithLabelValues(nsStr, resultBypassDisabled)),
			BypassGenError:   testutil.ToFloat64(cacheRequestsTotal.WithLabelValues(nsStr, resultBypassGenError)),
			BypassCircuit:    testutil.ToFloat64(cacheRequestsTotal.WithLabelValues(nsStr, resultBypassCircuit)),
			BypassTombstoned: testutil.ToFloat64(cacheRequestsTotal.WithLabelValues(nsStr, resultBypassTombstoned)),
		}
	}
	return out
}

// Delta returns, per namespace, (s - before): the change in each result's count between the
// before snapshot and s (the snapshot Delta is called on, i.e. the "after" snapshot). A namespace
// untouched between the two snapshots yields a zero-valued NamespaceCounts.
func (s CountsSnapshot) Delta(before CountsSnapshot) CountsSnapshot {
	out := make(CountsSnapshot, len(s))
	for ns, after := range s {
		prior := before[ns]
		out[ns] = NamespaceCounts{
			Hits:             after.Hits - prior.Hits,
			Misses:           after.Misses - prior.Misses,
			Errors:           after.Errors - prior.Errors,
			BypassDisabled:   after.BypassDisabled - prior.BypassDisabled,
			BypassGenError:   after.BypassGenError - prior.BypassGenError,
			BypassCircuit:    after.BypassCircuit - prior.BypassCircuit,
			BypassTombstoned: after.BypassTombstoned - prior.BypassTombstoned,
		}
	}
	return out
}
