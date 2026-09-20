package observability

import (
	"strings"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// revenueCatMetricNames is every metric this subsystem declares. The cardinality guard's coverage
// assertion requires each of them to appear in the scrape after publishRevenueCatLabelSamples, so
// a new RC metric added without a bounded label set is caught rather than silently unchecked.
var revenueCatMetricNames = []string{
	"revenuecat_api_requests_total",
	"revenuecat_api_request_duration_seconds",
	"revenuecat_api_rate_limited_total",
	"revenuecat_webhook_received_total",
	"revenuecat_webhook_processing_duration_seconds",
	"revenuecat_webhook_failed_total",
	"revenuecat_resolution_total",
	"revenuecat_grants_applied_total",
	"revenuecat_grants_expired_total",
	"revenuecat_operation_failures_total",
	"revenuecat_operation_abandonments_total",
	"revenuecat_refreshes_total",
	"revenuecat_refresh_duration_seconds",
	"revenuecat_retention_episodes_armed_total",
	"revenuecat_webhook_inbox_pending",
	"revenuecat_oldest_unprocessed_webhook_seconds",
	"revenuecat_operations_pending",
	"revenuecat_oldest_pending_operation_age_seconds",
	"revenuecat_operations_errored",
	"revenuecat_comp_grants_outstanding",
	"revenuecat_refresh_overdue_users",
}

// publishRevenueCatLabelSamples creates at least one child on every RC metric so Gather() returns
// it. Without this the guard would be vacuous for any Vec with no label combination yet observed.
func publishRevenueCatLabelSamples() {
	RecordRevenueCatAPIRequest(RevenueCatOperationFetchEntitlements, RevenueCatAPIStatusSuccess)
	ObserveRevenueCatAPIRequestDuration(RevenueCatOperationFetchEntitlements, 0.01)
	RecordRevenueCatRateLimited()
	RecordRevenueCatWebhookReceived("INITIAL_PURCHASE")
	ObserveRevenueCatWebhookProcessingDuration("INITIAL_PURCHASE", 0.01)
	RecordRevenueCatWebhookFailed("INITIAL_PURCHASE", RevenueCatWebhookReasonInvalidSignature)
	RecordRevenueCatResolution(RevenueCatResolutionResolved)
	RecordRevenueCatGrantApplied(RevenueCatGrantClassComp)
	RecordRevenueCatGrantExpired(RevenueCatGrantClassPaid)
	RecordRevenueCatOperationFailure(RevenueCatGrantClassComp)
	RecordRevenueCatOperationAbandoned(RevenueCatGrantClassComp)
	ObserveRevenueCatRefresh(RevenueCatRefreshSuccess, 0.01)
	RecordRevenueCatRetentionEpisodeArmed()
	PublishRevenueCatWebhookInbox(1, 1)
	PublishRevenueCatOperations(1, 1, 1)
	PublishRevenueCatCompGrantsOutstanding(1)
	PublishRevenueCatRefreshOverdueUsers(1)
}

// findUnboundedRevenueCatLabels is the guard's mechanism, deliberately a function over a Gatherer
// rather than a fixed list of metric names: it walks the ACTUAL registered series and inspects each
// sample's LABEL NAMES. A guard that only checked metric names would prove nothing about label
// cardinality — the failure mode this exists to catch is `{user_id="..."}`, not a badly named
// metric.
func findUnboundedRevenueCatLabels(g prometheus.Gatherer) ([]string, error) {
	families, err := g.Gather()
	if err != nil {
		return nil, err
	}
	var violations []string
	for _, f := range families {
		if !strings.HasPrefix(f.GetName(), "revenuecat_") {
			continue
		}
		for _, m := range f.GetMetric() {
			for _, l := range m.GetLabel() {
				if !revenueCatBoundedLabelNames[l.GetName()] {
					violations = append(violations, f.GetName()+"{"+l.GetName()+"}")
				}
			}
		}
	}
	return violations, nil
}

func TestRevenueCatMetrics_UseOnlyBoundedLabelNames(t *testing.T) {
	publishRevenueCatLabelSamples()

	violations, err := findUnboundedRevenueCatLabels(prometheus.DefaultGatherer)
	require.NoError(t, err)
	assert.Empty(t, violations,
		"a revenuecat_* metric carries a label name outside the bounded allowlist; an unbounded label (user id, email, customer id, request path) creates one series per user")

	// Coverage: every declared metric must actually appear, or the assertion above is vacuous for
	// whichever metric was never published.
	families, err := prometheus.DefaultGatherer.Gather()
	require.NoError(t, err)
	present := map[string]bool{}
	for _, f := range families {
		present[f.GetName()] = true
	}
	for _, name := range revenueCatMetricNames {
		assert.True(t, present[name],
			"declared metric %q did not appear in the scrape, so the cardinality guard never inspected it", name)
	}
}

// TestRevenueCatLabelGuard_FailsOnAnUnboundedLabel is the meta-test the plan requires: it
// CONSTRUCTS the violation the guard exists to catch and proves the guard reports it. Without this,
// a guard whose allowlist accidentally contained every string would stay green forever.
func TestRevenueCatLabelGuard_FailsOnAnUnboundedLabel(t *testing.T) {
	reg := prometheus.NewRegistry()
	bad := prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "revenuecat_bad_metric_total",
		Help: "constructed violation",
	}, []string{"user_id"})
	reg.MustRegister(bad)
	bad.WithLabelValues("user-123").Inc()

	violations, err := findUnboundedRevenueCatLabels(reg)
	require.NoError(t, err)
	require.Len(t, violations, 1, "the guard must flag exactly the constructed unbounded label")
	assert.Contains(t, violations[0], "user_id")
}

func TestNormalizeRevenueCatEventType_FoldsUnknownIntoOther(t *testing.T) {
	assert.Equal(t, "INITIAL_PURCHASE", NormalizeRevenueCatEventType("INITIAL_PURCHASE"))
	assert.Equal(t, RevenueCatLabelOther, NormalizeRevenueCatEventType("SOME_FUTURE_EVENT_TYPE"))
	assert.Equal(t, RevenueCatLabelOther, NormalizeRevenueCatEventType(""))
	// A value carrying an identity must never survive as a label.
	assert.Equal(t, RevenueCatLabelOther, NormalizeRevenueCatEventType("user_2abcDEF"))
}

// TestRecordRevenueCatAPIRequest_RefusesARequestPathAsTheOperationLabel is the concrete case the
// plan calls out: customerPath(id) embeds the customer id, so labelling by path would create one
// series per user. The helper must fold it into "other".
func TestRecordRevenueCatAPIRequest_RefusesARequestPathAsTheOperationLabel(t *testing.T) {
	RevenueCatAPIRequestsTotal.Reset()
	RecordRevenueCatAPIRequest("/v2/projects/proj/customers/user_2abcDEF", RevenueCatAPIStatusSuccess)

	assert.Equal(t, float64(1), testutil.ToFloat64(
		RevenueCatAPIRequestsTotal.WithLabelValues(RevenueCatLabelOther, RevenueCatAPIStatusSuccess)))
	assert.Equal(t, 1, testutil.CollectAndCount(RevenueCatAPIRequestsTotal),
		"the path must not have created a second series")
}

func TestRecordRevenueCatAPIRequest_BoundsStatus(t *testing.T) {
	RevenueCatAPIRequestsTotal.Reset()
	RecordRevenueCatAPIRequest(RevenueCatOperationGrant, "provider said no")

	assert.Equal(t, float64(1), testutil.ToFloat64(
		RevenueCatAPIRequestsTotal.WithLabelValues(RevenueCatOperationGrant, RevenueCatAPIStatusError)))
}

func TestRecordRevenueCatGrantExpired_BoundsClass(t *testing.T) {
	RevenueCatGrantsExpiredTotal.Reset()
	RecordRevenueCatGrantExpired("enterprise_platinum")
	assert.Equal(t, float64(1), testutil.ToFloat64(
		RevenueCatGrantsExpiredTotal.WithLabelValues(RevenueCatLabelOther)))

	RecordRevenueCatGrantExpired(RevenueCatGrantClassComp)
	assert.Equal(t, float64(1), testutil.ToFloat64(
		RevenueCatGrantsExpiredTotal.WithLabelValues(RevenueCatGrantClassComp)))
}

func TestRecordRevenueCatOperationAbandoned_BoundsClass(t *testing.T) {
	RevenueCatOperationAbandonmentsTotal.Reset()
	RecordRevenueCatOperationAbandoned("enterprise_platinum")
	assert.Equal(t, float64(1), testutil.ToFloat64(
		RevenueCatOperationAbandonmentsTotal.WithLabelValues(RevenueCatLabelOther)))

	RecordRevenueCatOperationAbandoned(RevenueCatGrantClassComp)
	assert.Equal(t, float64(1), testutil.ToFloat64(
		RevenueCatOperationAbandonmentsTotal.WithLabelValues(RevenueCatGrantClassComp)))
}

func TestRecordRevenueCatWebhookFailed_BoundsBothLabels(t *testing.T) {
	RevenueCatWebhookFailedTotal.Reset()
	RecordRevenueCatWebhookFailed("user_2abcDEF", "pq: duplicate key value violates unique constraint")

	assert.Equal(t, float64(1), testutil.ToFloat64(RevenueCatWebhookFailedTotal.WithLabelValues(
		RevenueCatLabelOther, RevenueCatWebhookReasonOther)))
}

func TestObserveRevenueCatRefresh_BoundsOutcome(t *testing.T) {
	RevenueCatRefreshesTotal.Reset()
	ObserveRevenueCatRefresh("provider exploded", 0.02)
	assert.Equal(t, float64(1), testutil.ToFloat64(
		RevenueCatRefreshesTotal.WithLabelValues(RevenueCatRefreshError)))
}

// The gauges must be absent until the collector has published a reading, not present carrying a
// startup zero — the UnmeasuredGauge contract.
func TestPublishRevenueCatWebhookInbox_IsAbsentUntilPublished(t *testing.T) {
	RevenueCatWebhookInboxPending.DeleteLabelValues()
	assert.Equal(t, 0, testutil.CollectAndCount(RevenueCatWebhookInboxPending),
		"an unpublished backlog gauge must not appear in the scrape")

	PublishRevenueCatWebhookInbox(3, 90)
	assert.Equal(t, float64(3), testutil.ToFloat64(RevenueCatWebhookInboxPending))
	assert.Equal(t, float64(90), testutil.ToFloat64(RevenueCatOldestUnprocessedWebhookSeconds))
}

func TestPublishRevenueCatOperations_ClampsNegativeAge(t *testing.T) {
	PublishRevenueCatOperations(2, 1, -5)
	assert.Equal(t, float64(2), testutil.ToFloat64(RevenueCatOperationsPending))
	assert.Equal(t, float64(1), testutil.ToFloat64(RevenueCatOperationsErrored))
	assert.Equal(t, float64(0), testutil.ToFloat64(RevenueCatOldestPendingOperationAgeSeconds),
		"a clock-skew negative age must never be published")
}

func TestRevenueCatBoundedLabelNames_CoversEveryDeclaredLabel(t *testing.T) {
	for _, name := range []string{"operation", "status", "event_type", "reason_class", "outcome", "class"} {
		assert.True(t, revenueCatBoundedLabelNames[name], "allowlist is missing %q", name)
	}
	assert.Len(t, revenueCatBoundedLabelNames, 6,
		"a label name was added to the allowlist; re-derive that every revenuecat_* metric using it has a bounded VALUE vocabulary")
}
