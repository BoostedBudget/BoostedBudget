package posthog

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/maxnussbaum/BoostedBudget/internal/observability"
)

// Issue #1652 Todo 14 — the lifecycle coverage GATE.
//
// `TestLifecycleCoverageGate` runs the checked-in matrix (`lifecycle_coverage.go`) through
// `lifecycleCoverageViolations` against the REAL catalog, the REAL liveness scan (todo 13's
// `goEmitterFiles`/`typeScriptEmitterFiles`/`eventIsLive`), the REAL Prometheus registry, and the
// REAL filesystem. It fails if:
//
//   - a required stage from `lifecycleStageNames` is missing from the definition,
//   - a stage has no mechanism,
//   - a `catalog_event` mechanism does not resolve in the catalog or has no production emitter,
//   - a `prometheus_metric` mechanism does not resolve in the default registry,
//   - a `log`/`existing_machinery` mechanism's cited file+symbol does not exist,
//   - a mechanism's cited proof test does not resolve in its cited file.
//
// The two meta-tests below construct the two violations the plan requires and prove the gate
// reports them. They are not documentation: they call the same core function the gate does.

// TestLifecycleCoverageGate is the gate. An empty violation list is the only pass.
func TestLifecycleCoverageGate(t *testing.T) {
	resolve := realLifecycleCoverageResolver(t)

	violations := lifecycleCoverageViolations(lifecycleStageNames, lifecycleCoverage, resolve)
	assert.Empty(t, violations,
		"lifecycle coverage is incomplete or a named mechanism is not live; every stage from the plan's End state must map to a live mechanism with a resolving proof test")
}

// TestLifecycleCoverageGateFailsWhenAStageIsRemoved is meta-test (a): a definition missing one
// required stage must be reported. It removes a real stage and drives the core with an
// all-resolving stub, so the ONLY possible failure is the missing stage — proving the gate's
// completeness check is what caught it, not an unrelated mechanism failure.
func TestLifecycleCoverageGateFailsWhenAStageIsRemoved(t *testing.T) {
	const removed = "checkout"

	var incomplete []lifecycleStageCoverage
	for _, stage := range lifecycleCoverage {
		if stage.Stage != removed {
			incomplete = append(incomplete, stage)
		}
	}
	require.Len(t, incomplete, len(lifecycleCoverage)-1, "precondition: exactly one stage must be removed")

	violations := lifecycleCoverageViolations(lifecycleStageNames, incomplete, allResolvingCoverageResolver())

	require.Len(t, violations, 1, "removing a stage must produce exactly the missing-stage violation")
	assert.Equal(t, removed+": lifecycle stage missing from the coverage definition", violations[0])
}

// TestLifecycleCoverageGateFailsWhenAMechanismIsFabricated is meta-test (b): a row naming a
// nonexistent event or metric must be reported. It drives the REAL resolver, so a fabricated name
// is rejected for the same reason a typo would be in production — it is not in the catalog/registry.
func TestLifecycleCoverageGateFailsWhenAMechanismIsFabricated(t *testing.T) {
	resolve := realLifecycleCoverageResolver(t)

	fabricated := []lifecycleStageCoverage{
		{
			Stage: "checkout",
			Mechanisms: []lifecycleMechanism{
				{
					Kind:       mechanismCatalogEvent,
					Identifier: "revenuecat_event_that_does_not_exist",
					ProofTest:  "TestLifecycleCoverageGate",
					ProofFile:  "backend/internal/integration/posthog/lifecycle_coverage_test.go",
				},
				{
					Kind:       mechanismPrometheusMetric,
					Identifier: "revenuecat_metric_that_does_not_exist_total",
					ProofTest:  "TestLifecycleCoverageGate",
					ProofFile:  "backend/internal/integration/posthog/lifecycle_coverage_test.go",
				},
			},
		},
	}

	violations := lifecycleCoverageViolations([]string{"checkout"}, fabricated, resolve)

	assert.Contains(t, violations,
		"checkout: mechanism catalog_event revenuecat_event_that_does_not_exist does not resolve",
		"a fabricated catalog event must be reported")
	assert.Contains(t, violations,
		"checkout: mechanism prometheus_metric revenuecat_metric_that_does_not_exist_total does not resolve",
		"a fabricated metric must be reported")
}

// TestLifecycleCoverageGateFailsWhenAProofTestDoesNotResolve is the proof-column meta-test: a
// mechanism whose cited test does not exist must be reported, so the matrix cannot carry a
// plausible-looking but vacuous proof. It overrides only `proofResolves` and leaves every mechanism
// liveness check resolving, isolating the proof dimension.
func TestLifecycleCoverageGateFailsWhenAProofTestDoesNotResolve(t *testing.T) {
	resolve := allResolvingCoverageResolver()
	resolve.proofResolves = func(test, file string) bool { return false }

	violations := lifecycleCoverageViolations(lifecycleStageNames, lifecycleCoverage, resolve)

	require.NotEmpty(t, violations, "an unresolvable proof test must be reported")
	for _, violation := range violations {
		assert.Contains(t, violation, "does not resolve")
	}
}

// allResolvingCoverageResolver returns a resolver where every dimension resolves, for meta-tests
// that want to isolate one failure. It is deliberately not usable by the gate itself.
func allResolvingCoverageResolver() coverageResolver {
	return coverageResolver{
		catalogEventResolves: func(string) bool { return true },
		catalogEventLive:     func(string) bool { return true },
		metricResolves:       func(string) bool { return true },
		fileContainsSymbol:   func(string, string) bool { return true },
		proofResolves:        func(string, string) bool { return true },
	}
}

// realLifecycleCoverageResolver wires the gate to the real tree. Every dimension is the real
// thing: the catalog YAML, todo 13's liveness scan, the default Prometheus registry, and the
// filesystem. The liveness scan is the SAME code path `TestEveryCatalogEventHasALiveEmitter` uses,
// so an event this gate accepts is an event that guard accepts too.
func realLifecycleCoverageResolver(t *testing.T) coverageResolver {
	t.Helper()
	root := repoRoot(t)

	events := parseCatalog(t)
	byName := make(map[string]catalogEventEntry, len(events))
	for _, event := range events {
		byName[event.Name] = event
	}
	goSymbols, tsSymbols := livenessSymbols(events)
	goEmitters, err := goEmitterFiles(filepath.Join(root, "backend"), goSymbols)
	require.NoError(t, err)
	tsEmitters, err := typeScriptEmitterFiles(filepath.Join(root, "apps", "web", "src"), tsSymbols)
	require.NoError(t, err)

	// Prime every metric the matrix names so Gather() returns it. An unpopulated CounterVec
	// produces no family, so without this the registry check would be vacuous for any metric whose
	// labels have not yet been observed. A fabricated name is not primed and therefore absent.
	for _, stage := range lifecycleCoverage {
		for _, mechanism := range stage.Mechanisms {
			if mechanism.Kind == mechanismPrometheusMetric {
				primeLifecycleMetric(mechanism.Identifier)
			}
		}
	}
	families, err := prometheus.DefaultGatherer.Gather()
	require.NoError(t, err)
	presentMetrics := make(map[string]bool, len(families))
	for _, family := range families {
		presentMetrics[family.GetName()] = true
	}

	return coverageResolver{
		catalogEventResolves: func(name string) bool {
			_, ok := byName[name]
			return ok
		},
		catalogEventLive: func(name string) bool {
			event, ok := byName[name]
			if !ok {
				return false
			}
			return eventIsLive(event, goEmitters, tsEmitters)
		},
		metricResolves: func(name string) bool { return presentMetrics[name] },
		fileContainsSymbol: func(path, symbol string) bool {
			source, readErr := os.ReadFile(filepath.Join(root, path))
			return readErr == nil && strings.Contains(string(source), symbol)
		},
		proofResolves: func(test, file string) bool {
			source, readErr := os.ReadFile(filepath.Join(root, file))
			if readErr != nil {
				return false
			}
			if strings.HasSuffix(file, ".go") {
				return strings.Contains(string(source), "func "+test+"(")
			}
			// TypeScript proof: the test or suite title appears verbatim in the cited test file.
			return strings.Contains(string(source), test)
		},
	}
}

// primeLifecycleMetric publishes a sample on the named metric so it appears in the scrape. The
// switch is the allowlist: a name not listed here is not primed and therefore cannot resolve,
// which is exactly the fabricated-metric failure the meta-test proves. Each helper normalizes its
// labels through the bounded vocabularies, so priming cannot widen a label set.
func primeLifecycleMetric(name string) {
	switch name {
	case "revenuecat_webhook_received_total":
		observability.RecordRevenueCatWebhookReceived("INITIAL_PURCHASE")
	case "revenuecat_webhook_failed_total":
		observability.RecordRevenueCatWebhookFailed("INITIAL_PURCHASE", observability.RevenueCatWebhookReasonInvalidSignature)
	case "revenuecat_resolution_total":
		observability.RecordRevenueCatResolution(observability.RevenueCatResolutionResolved)
	case "revenuecat_retention_episodes_armed_total":
		observability.RecordRevenueCatRetentionEpisodeArmed()
	case "revenuecat_comp_grants_outstanding":
		observability.PublishRevenueCatCompGrantsOutstanding(1)
	case "revenuecat_grants_expired_total":
		observability.RecordRevenueCatGrantExpired(observability.RevenueCatGrantClassPaid)
	case "revenuecat_oldest_expired_comp_grant_seconds":
		observability.RevenueCatOldestExpiredCompGrantSeconds.WithLabelValues().Set(1)
	case "plaid_cleanup_outstanding_users":
		observability.PlaidCleanupOutstandingUsers.WithLabelValues().Set(1)
	case "plaid_cleanup_oldest_outstanding_seconds":
		observability.PlaidCleanupOldestOutstandingSeconds.WithLabelValues().Set(1)
	}
}
