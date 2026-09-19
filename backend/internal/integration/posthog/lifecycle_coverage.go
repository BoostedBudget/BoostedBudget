package posthog

import "sort"

// Issue #1652 Todo 14 — the RevenueCat lifecycle coverage matrix, as a checked-in data structure.
//
// WHAT THIS IS. Every lifecycle stage the plan's End state section names is declared here as one
// row, each row carrying at least one observability MECHANISM (a catalog event, a Prometheus
// metric, a log call site, or an existing machinery surface) and, per mechanism, the test that
// proves it. `lifecycle_coverage_test.go` is the GATE: it asserts every required stage appears in
// this table with ≥1 mechanism, that every mechanism resolves to a live surface, and that every
// cited proof test resolves. A prose table in an evidence file cannot fail; this one can.
//
// WHY A TABLE AND NOT PROSE. The plan's success criterion 9 says lifecycle coverage must be
// "mechanically gated, not a prose table — zero unmapped stages, proven by a test that fails when a
// stage or mechanism is removed". The two meta-tests in `lifecycle_coverage_test.go` construct both
// violations and prove the gate reports them.
//
// THE STAGES ARE FROZEN. `lifecycleStageNames` is the canonical enumeration, derived from the
// plan's End state list. `lifecycleCoverageViolations` compares the table against it, so deleting a
// stage from `lifecycleCoverage` (or never adding one) is a gate failure — not a silent gap.

// lifecycleMechanismKind classifies where a stage's observability mechanism lives. It also selects
// how the gate proves the mechanism is live:
//
//   - catalog_event      — the identifier must resolve in `config/posthog_events.yaml` AND have a
//     production emitter, reusing the liveness scan (`eventIsLive` + the real
//     `goEmitterFiles`/`typeScriptEmitterFiles` walk) that todo 13 built.
//   - prometheus_metric  — the identifier must resolve in the default Prometheus registry.
//   - log                — the cited file must exist and contain the cited symbol (the log call
//     site is the mechanism; a short-lived CLI has no Prometheus surface by design).
//   - existing_machinery — same file+symbol resolution as `log`; used for pre-existing surfaces
//     this plan deliberately REUSES rather than duplicates (erasure, the Plaid-cleanup gauges).
type lifecycleMechanismKind string

const (
	mechanismCatalogEvent      lifecycleMechanismKind = "catalog_event"
	mechanismPrometheusMetric  lifecycleMechanismKind = "prometheus_metric"
	mechanismLog               lifecycleMechanismKind = "log"
	mechanismExistingMachinery lifecycleMechanismKind = "existing_machinery"
)

// lifecycleMechanism is one way a stage is observable. `Identifier` is the event name, metric
// name, log site, or machinery name. `CallSite`/`Symbol` are required for the `log` and
// `existing_machinery` kinds (there is no registry or catalog to resolve them against).
// `ProofTest`/`ProofFile` are required for every mechanism: the test that proves the mechanism is
// wired, resolved against `ProofFile`.
type lifecycleMechanism struct {
	Kind       lifecycleMechanismKind
	Identifier string
	CallSite   string
	Symbol     string
	ProofTest  string
	ProofFile  string
}

// lifecycleStageCoverage is one row: a lifecycle stage and every mechanism that makes it observable.
type lifecycleStageCoverage struct {
	Stage      string
	Mechanisms []lifecycleMechanism
}

// lifecycleStageNames is the frozen, canonical enumeration of every lifecycle stage the plan's End
// state section names. The gate requires each name to appear as a `lifecycleStageCoverage.Stage`.
// Do not remove an entry to make the gate pass — that is the mutation the gate exists to catch.
var lifecycleStageNames = []string{
	"webhook ingestion",
	"grant application/refresh",
	"access resolution",
	"access-loss → retention → Plaid-cleanup safety net",
	"comp grant",
	"comp expiry",
	"paid-grant lapse",
	"trial start",
	"trial end",
	"refund/revocation",
	"erasure/offboarding",
	"bootstrap/import",
	"household projection/capacity",
	"checkout",
}

// lifecycleCoverage is the matrix. Each stage's mechanisms are the surfaces that make it
// observable; each mechanism's proof is the test that fails if the wiring is removed. The mappings
// follow the plan's todo 5 classification and todo 12 operational routing tables.
var lifecycleCoverage = []lifecycleStageCoverage{
	{
		Stage: "webhook ingestion",
		Mechanisms: []lifecycleMechanism{
			{
				Kind:       mechanismCatalogEvent,
				Identifier: "revenuecat_webhook_access_changed",
				ProofTest:  "TestRevenueCatWebhookEmitsAccessChangeThroughRealGateway",
				ProofFile:  "backend/internal/huma/operations/revenuecat_webhook_test.go",
			},
			{
				Kind:       mechanismPrometheusMetric,
				Identifier: "revenuecat_webhook_received_total",
				ProofTest:  "TestRevenueCatWebhookRecordsBoundedIngestionMetrics",
				ProofFile:  "backend/internal/huma/operations/revenuecat_webhook_test.go",
			},
			{
				Kind:       mechanismPrometheusMetric,
				Identifier: "revenuecat_webhook_failed_total",
				ProofTest:  "TestRevenueCatWebhookRecordsBoundedIngestionMetrics",
				ProofFile:  "backend/internal/huma/operations/revenuecat_webhook_test.go",
			},
		},
	},
	{
		Stage: "grant application/refresh",
		Mechanisms: []lifecycleMechanism{
			{
				Kind:       mechanismCatalogEvent,
				Identifier: "revenuecat_operation_queued",
				ProofTest:  "TestRevenueCatAnalyticsDistinguishesQueuedFromAppliedAndOmitsReason",
				ProofFile:  "backend/internal/services/revenuecat_analytics_test.go",
			},
			{
				Kind:       mechanismCatalogEvent,
				Identifier: "revenuecat_operation_applied",
				ProofTest:  "TestRevenueCatAnalyticsDistinguishesQueuedFromAppliedAndOmitsReason",
				ProofFile:  "backend/internal/services/revenuecat_analytics_test.go",
			},
			{
				Kind:       mechanismCatalogEvent,
				Identifier: "revenuecat_operation_failed",
				ProofTest:  "TestRevenueCatAnalyticsDistinguishesQueuedFromAppliedAndOmitsReason",
				ProofFile:  "backend/internal/services/revenuecat_analytics_test.go",
			},
			{
				Kind:       mechanismCatalogEvent,
				Identifier: "admin_subscription_override_applied_server",
				ProofTest:  "TestRevenueCatAdminOverrideIsScopedToAdminInitiatedGrants",
				ProofFile:  "backend/internal/services/revenuecat_analytics_test.go",
			},
			{
				Kind:       mechanismCatalogEvent,
				Identifier: "admin_subscription_override_removed_server",
				ProofTest:  "TestRevenueCatAdminOverrideIsScopedToAdminInitiatedGrants",
				ProofFile:  "backend/internal/services/revenuecat_analytics_test.go",
			},
		},
	},
	{
		Stage: "access resolution",
		Mechanisms: []lifecycleMechanism{
			{
				Kind:       mechanismPrometheusMetric,
				Identifier: "revenuecat_resolution_total",
				ProofTest:  "TestRevenueCatService_RecordsResolutionOutcome",
				ProofFile:  "backend/internal/services/revenuecat_metrics_wiring_test.go",
			},
			{
				Kind:       mechanismCatalogEvent,
				Identifier: "revenuecat_access_inactive",
				ProofTest:  "TestRevenueCatAccessInactiveAndRetentionArmedThroughRealGateway",
				ProofFile:  "backend/internal/services/revenuecat_analytics_test.go",
			},
		},
	},
	{
		Stage: "access-loss → retention → Plaid-cleanup safety net",
		Mechanisms: []lifecycleMechanism{
			{
				Kind:       mechanismCatalogEvent,
				Identifier: "revenuecat_retention_armed",
				ProofTest:  "TestRevenueCatAccessInactiveAndRetentionArmedThroughRealGateway",
				ProofFile:  "backend/internal/services/revenuecat_analytics_test.go",
			},
			{
				Kind:       mechanismCatalogEvent,
				Identifier: "revenuecat_plaid_cleanup_disconnected",
				ProofTest:  "TestRetentionCleanupCheck_C9Chain_RevenueCatExpiryDisconnectsPlaid",
				ProofFile:  "backend/internal/queue/jobs/retention_cleanup_check_c9_chain_integration_test.go",
			},
			{
				Kind:       mechanismPrometheusMetric,
				Identifier: "revenuecat_retention_episodes_armed_total",
				ProofTest:  "TestRevenueCatService_RecordsLifecycleFlowCountersOnAccessLoss",
				ProofFile:  "backend/internal/services/revenuecat_metrics_wiring_test.go",
			},
			{
				Kind:       mechanismPrometheusMetric,
				Identifier: "plaid_cleanup_outstanding_users",
				ProofTest:  "TestJobHealthRepository_GetOutstandingPlaidCleanupStats_Success",
				ProofFile:  "backend/internal/repository/job_health_repository_test.go",
			},
			{
				Kind:       mechanismPrometheusMetric,
				Identifier: "plaid_cleanup_oldest_outstanding_seconds",
				ProofTest:  "TestJobHealthRepository_GetOutstandingPlaidCleanupStats_Success",
				ProofFile:  "backend/internal/repository/job_health_repository_test.go",
			},
		},
	},
	{
		Stage: "comp grant",
		Mechanisms: []lifecycleMechanism{
			{
				Kind:       mechanismCatalogEvent,
				Identifier: "revenuecat_operation_queued",
				ProofTest:  "TestRevenueCatAnalyticsDistinguishesQueuedFromAppliedAndOmitsReason",
				ProofFile:  "backend/internal/services/revenuecat_analytics_test.go",
			},
			{
				Kind:       mechanismPrometheusMetric,
				Identifier: "revenuecat_comp_grants_outstanding",
				ProofTest:  "TestJobHealthRepository_GetRevenueCatCompGrantsOutstanding_Success",
				ProofFile:  "backend/internal/repository/job_health_repository_test.go",
			},
			{
				Kind:       mechanismPrometheusMetric,
				Identifier: "revenuecat_oldest_expired_comp_grant_seconds",
				ProofTest:  "TestStartJobHealthMetricsCollector_PublishesAMeasurementBeforeTheFirstTick",
				ProofFile:  "backend/cmd/server/issue1504_job_health_startup_read_test.go",
			},
		},
	},
	{
		Stage: "comp expiry",
		Mechanisms: []lifecycleMechanism{
			{
				Kind:       mechanismCatalogEvent,
				Identifier: "revenuecat_comp_expired",
				ProofTest:  "TestRevenueCatCompExpiry_ArmsRetentionAndClearsOverride",
				ProofFile:  "backend/internal/repository/revenuecat_comp_expiry_integration_test.go",
			},
			{
				Kind:       mechanismPrometheusMetric,
				Identifier: "revenuecat_grants_expired_total",
				ProofTest:  "TestRevenueCatService_RecordsLifecycleFlowCountersOnAccessLoss",
				ProofFile:  "backend/internal/services/revenuecat_metrics_wiring_test.go",
			},
		},
	},
	{
		Stage: "paid-grant lapse",
		Mechanisms: []lifecycleMechanism{
			{
				Kind:       mechanismCatalogEvent,
				Identifier: "revenuecat_paid_grant_lapsed",
				ProofTest:  "TestRevenueCatPaidGrantLapse_KeepsLinkWhileActiveThenArmsRetention",
				ProofFile:  "backend/internal/repository/revenuecat_paid_grant_lapse_integration_test.go",
			},
			{
				Kind:       mechanismPrometheusMetric,
				Identifier: "revenuecat_grants_expired_total",
				ProofTest:  "TestRevenueCatService_RecordsLifecycleFlowCountersOnAccessLoss",
				ProofFile:  "backend/internal/services/revenuecat_metrics_wiring_test.go",
			},
		},
	},
	{
		Stage: "trial start",
		Mechanisms: []lifecycleMechanism{
			{
				Kind:       mechanismCatalogEvent,
				Identifier: "revenuecat_trial_started",
				ProofTest:  "TestRevenueCatRepository_TrialRollbackAndConcurrency",
				ProofFile:  "backend/internal/repository/revenuecat_repository_integration_test.go",
			},
		},
	},
	{
		Stage: "trial end",
		Mechanisms: []lifecycleMechanism{
			{
				Kind:       mechanismCatalogEvent,
				Identifier: "revenuecat_trial_ended",
				ProofTest:  "TestEveryRevenueCatLifecycleEventDeliversExactlyItsBoundedContract",
				ProofFile:  "backend/internal/integration/posthog/revenuecat_delivery_guard_test.go",
			},
			{
				Kind:       mechanismPrometheusMetric,
				Identifier: "revenuecat_grants_expired_total",
				ProofTest:  "TestRevenueCatService_RecordsLifecycleFlowCountersOnAccessLoss",
				ProofFile:  "backend/internal/services/revenuecat_metrics_wiring_test.go",
			},
		},
	},
	{
		Stage: "refund/revocation",
		Mechanisms: []lifecycleMechanism{
			{
				Kind:       mechanismCatalogEvent,
				Identifier: "revenuecat_access_revoked",
				ProofTest:  "TestRevenueCatRefundRevocationEmitsThroughRealDBAndGateway",
				ProofFile:  "backend/internal/services/revenuecat_refund_revocation_integration_test.go",
			},
		},
	},
	{
		Stage: "erasure/offboarding",
		Mechanisms: []lifecycleMechanism{
			// Reused, not re-created: the plan forbids a parallel RevenueCat-erasure metric. The
			// mapping `erasure/offboarding → posthog_erasure_*` is exactly what todo 12 recorded.
			{
				Kind:       mechanismExistingMachinery,
				Identifier: "posthog_erasure_attempts_total",
				CallSite:   "backend/internal/observability/posthog_erasure_metrics.go",
				Symbol:     "posthog_erasure_attempts_total",
				ProofTest:  "TestRecordPostHogErasureAttemptBoundsLabels",
				ProofFile:  "backend/internal/observability/posthog_erasure_metrics_test.go",
			},
			{
				Kind:       mechanismExistingMachinery,
				Identifier: "posthog_erasure_rows",
				CallSite:   "backend/internal/observability/posthog_erasure_metrics.go",
				Symbol:     "posthog_erasure_rows",
				ProofTest:  "TestPublishPostHogErasureBacklogSetsEveryStateAndAge",
				ProofFile:  "backend/internal/observability/posthog_erasure_metrics_test.go",
			},
			{
				Kind:       mechanismExistingMachinery,
				Identifier: "posthog_erasure_oldest_outstanding_seconds",
				CallSite:   "backend/internal/observability/posthog_erasure_metrics.go",
				Symbol:     "posthog_erasure_oldest_outstanding_seconds",
				ProofTest:  "TestPublishPostHogErasureBacklogSetsEveryStateAndAge",
				ProofFile:  "backend/internal/observability/posthog_erasure_metrics_test.go",
			},
		},
	},
	{
		Stage: "bootstrap/import",
		Mechanisms: []lifecycleMechanism{
			// The batch mechanics are a LOG by deliberate scope boundary: the bootstrap CLI is
			// short-lived and backend/ has no pushgateway, so a counter there is scraped by nobody.
			{
				Kind:       mechanismLog,
				Identifier: "revenuecat bootstrap run completed",
				CallSite:   "backend/cmd/revenuecat-bootstrap/main.go",
				Symbol:     "revenuecat bootstrap run completed",
				ProofTest:  "TestRevenueCatBootstrapApplyIsConcurrentIdempotentAndPreservesExpiry",
				ProofFile:  "backend/internal/repository/revenuecat_bootstrap_integration_test.go",
			},
			// A user's access established by import runs through the operation queue and emits the
			// trial-start event (todo 9's decision).
			{
				Kind:       mechanismCatalogEvent,
				Identifier: "revenuecat_trial_started",
				ProofTest:  "TestRevenueCatRepository_TrialRollbackAndConcurrency",
				ProofFile:  "backend/internal/repository/revenuecat_repository_integration_test.go",
			},
		},
	},
	{
		Stage: "household projection/capacity",
		Mechanisms: []lifecycleMechanism{
			{
				Kind:       mechanismCatalogEvent,
				Identifier: "household_entitlement_projected",
				ProofTest:  "TestHouseholdEntitlementProjectedThroughRealGateway",
				ProofFile:  "backend/internal/services/household_entitlement_projection_analytics_test.go",
			},
			{
				Kind:       mechanismCatalogEvent,
				Identifier: "household_over_capacity_entered",
				ProofTest:  "TestHouseholdOverCapacityEnteredThroughRealGateway",
				ProofFile:  "backend/internal/queue/jobs/household_over_capacity_analytics_test.go",
			},
		},
	},
	{
		Stage: "checkout",
		Mechanisms: []lifecycleMechanism{
			{
				Kind:       mechanismCatalogEvent,
				Identifier: "checkout_started",
				ProofTest:  "survives the real normalizer with no PII-bearing property",
				ProofFile:  "apps/web/src/shared/analytics/revenueCatFrontendEvents.test.ts",
			},
			{
				Kind:       mechanismCatalogEvent,
				Identifier: "checkout_completed",
				ProofTest:  "survives the real normalizer with no PII-bearing property",
				ProofFile:  "apps/web/src/shared/analytics/revenueCatFrontendEvents.test.ts",
			},
			{
				Kind:       mechanismCatalogEvent,
				Identifier: "checkout_cancelled",
				ProofTest:  "survives the real normalizer with no PII-bearing property",
				ProofFile:  "apps/web/src/shared/analytics/revenueCatFrontendEvents.test.ts",
			},
			{
				Kind:       mechanismCatalogEvent,
				Identifier: "checkout_confirmation_pending",
				ProofTest:  "survives the real normalizer with no PII-bearing property",
				ProofFile:  "apps/web/src/shared/analytics/revenueCatFrontendEvents.test.ts",
			},
			{
				Kind:       mechanismCatalogEvent,
				Identifier: "subscription_managed",
				ProofTest:  "fires subscription_managed when the resolved management link is actioned",
				ProofFile:  "apps/web/src/features/billing/components/BillingPortalButton.test.tsx",
			},
		},
	},
}

// coverageResolver is the gate's dependency boundary. The real implementation in
// `lifecycle_coverage_test.go` resolves against the real catalog, the real liveness scan, the real
// Prometheus registry and the real filesystem; the meta-tests substitute stubs to construct
// violations. Keeping it a struct of functions (rather than an interface) makes each meta-test
// able to override exactly one dimension.
type coverageResolver struct {
	catalogEventResolves func(name string) bool
	catalogEventLive     func(name string) bool
	metricResolves       func(name string) bool
	fileContainsSymbol   func(path, symbol string) bool
	proofResolves        func(test, file string) bool
}

// lifecycleCoverageViolations is the gate core. It returns the sorted list of coverage failures:
// a required stage missing from the definition, a stage with no mechanism, a mechanism that does
// not resolve to a live surface, or a mechanism whose cited proof test does not resolve. Empty
// means the matrix is complete and every named mechanism is live.
func lifecycleCoverageViolations(requiredStages []string, defined []lifecycleStageCoverage, resolve coverageResolver) []string {
	var violations []string

	definedStages := make(map[string]bool, len(defined))
	for _, stage := range defined {
		definedStages[stage.Stage] = true

		if len(stage.Mechanisms) == 0 {
			violations = append(violations, stage.Stage+": no mechanism")
		}
		for _, mechanism := range stage.Mechanisms {
			if !coverageMechanismResolves(mechanism, resolve) {
				violations = append(violations,
					stage.Stage+": mechanism "+string(mechanism.Kind)+" "+mechanism.Identifier+" does not resolve")
			}
			if mechanism.ProofTest == "" || mechanism.ProofFile == "" {
				violations = append(violations,
					stage.Stage+": mechanism "+mechanism.Identifier+" has no cited proof test")
				continue
			}
			if resolve.proofResolves != nil && !resolve.proofResolves(mechanism.ProofTest, mechanism.ProofFile) {
				violations = append(violations,
					stage.Stage+": proof "+mechanism.ProofTest+" ("+mechanism.ProofFile+") does not resolve")
			}
		}
	}

	for _, required := range requiredStages {
		if !definedStages[required] {
			violations = append(violations,
				required+": lifecycle stage missing from the coverage definition")
		}
	}

	sort.Strings(violations)
	return violations
}

// coverageMechanismResolves applies the per-kind liveness rule described on lifecycleMechanismKind.
// A mechanism whose kind is unknown, or whose identifier is empty, never resolves — the fail-safe
// direction, so a typo cannot pass as coverage.
func coverageMechanismResolves(mechanism lifecycleMechanism, resolve coverageResolver) bool {
	if mechanism.Identifier == "" {
		return false
	}
	switch mechanism.Kind {
	case mechanismCatalogEvent:
		if resolve.catalogEventResolves == nil || !resolve.catalogEventResolves(mechanism.Identifier) {
			return false
		}
		// A declared-but-unemitted event is a dead contract; reuse todo 13's liveness scan.
		return resolve.catalogEventLive != nil && resolve.catalogEventLive(mechanism.Identifier)
	case mechanismPrometheusMetric:
		return resolve.metricResolves != nil && resolve.metricResolves(mechanism.Identifier)
	case mechanismLog, mechanismExistingMachinery:
		if mechanism.CallSite == "" || mechanism.Symbol == "" {
			return false
		}
		return resolve.fileContainsSymbol != nil && resolve.fileContainsSymbol(mechanism.CallSite, mechanism.Symbol)
	default:
		return false
	}
}
