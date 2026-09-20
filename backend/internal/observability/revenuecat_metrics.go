package observability

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// RevenueCat lifecycle metrics (#1652 todo 12).
//
// This file is the Prometheus half of the RevenueCat migration's operability: every lifecycle
// stage the plan's todo 5 classified `operational` publishes a metric here, and the counters are
// incremented AT THE EVENT SITE while the gauges are read AT SCRAPE TIME by
// cmd/server/main.go's startJobHealthMetricsCollector. The distinction is load-bearing and is
// stated per metric below: a value derived from a QUERY is a gauge, because declaring it a
// `*_total` counter would produce a counter that RESETS whenever the query's answer changes —
// a silent alerting bug (an `increase()` rule fires on every fluctuation of a stock).
//
// ⚠ CARDINALITY IS A HARD RULE HERE. No user id, email, Plaid item id, RevenueCat customer id, or
// any other unbounded value may be a label. Every label value is normalized through one of the
// fixed allowlists below, and TestRevenueCatMetrics_UseOnlyBoundedLabelNames walks the metric
// registry and fails if any `revenuecat_*` metric carries a label name outside
// revenueCatBoundedLabelNames — with TestRevenueCatLabelGuard_FailsOnAnUnboundedLabel proving the
// guard itself fails when a `user_id` label is introduced.
//
// ⚠ WHAT IS DELIBERATELY ABSENT, and why:
//   - `revenuecat_users_by_access_state` — access state is not a stored column; it is derived
//     per-user in Go by domain.RevenueCatState.ResolveAccess over revenuecat_customers.grants
//     (jsonb). A gauge would need a full-table scan plus per-row Go resolution on a schedule and
//     no collector here is budgeted for it. The same information is already available on demand
//     from the admin/system-health surfaces.
//   - `revenuecat_bootstrap_records_total` — cmd/revenuecat-bootstrap is a short-lived CLI with no
//     Prometheus surface and no pushgateway anywhere in backend/, so a counter registered there is
//     scraped by nobody. Bootstrap outcomes are recorded in the LOG (slog, counts per run).
//   - a `revenuecat_`-prefixed duplicate of the Plaid-cleanup safety net — that net is ALREADY
//     measured by PlaidCleanupOutstandingUsers / PlaidCleanupOldestOutstandingSeconds /
//     PlaidCleanupBackstopFailuresTotal above; duplicating it would create a second series for one
//     number. The only genuinely new signal is RevenueCatRetentionEpisodesArmedTotal (flow).
//   - a RevenueCat-specific erasure metric — the RC erasure stage is already covered by
//     posthog_erasure_* (posthog_erasure_metrics.go). See docs/operations/posthog-rollout.md.
var (
	// RevenueCatAPIRequestsTotal counts RevenueCat API client operations by bounded operation and
	// outcome status. It is incremented at the client's public method boundary (client.go), so one
	// increment is one LOGICAL provider operation — a paginated `list` call issues several HTTP
	// requests but counts once, because an operator reasons about "how often do we call
	// fetch_entitlements", not "how many pages did it walk".
	//
	// ⚠ LABEL HYGIENE, NON-NEGOTIABLE: `operation` is one of revenueCatOperations (a fixed set) and
	// `status` is one of success|error|rate_limited. NEVER the request path — it embeds the
	// RevenueCat customer id (customerPath(id)) and would create one series per user.
	RevenueCatAPIRequestsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "revenuecat_api_requests_total",
		Help: "Total RevenueCat API client operations by bounded operation and status (success, error, rate_limited). One increment is one logical provider operation; a paginated list issues several HTTP requests but counts once. Never labelled by request path or customer id.",
	}, []string{"operation", "status"})

	// RevenueCatAPIRequestDurationSeconds tracks how long each logical RevenueCat API operation
	// takes, by the same bounded operation enum.
	RevenueCatAPIRequestDurationSeconds = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "revenuecat_api_request_duration_seconds",
		Help:    "Duration of logical RevenueCat API client operations in seconds, by bounded operation. A paginated list spans several HTTP round trips under one observation.",
		Buckets: prometheus.DefBuckets,
	}, []string{"operation"})

	// RevenueCatAPIRateLimitedTotal counts provider 429s. Deliberately unlabelled: the rate-limit
	// condition is a transport-wide signal and the per-operation breakdown is already available
	// from RevenueCatAPIRequestsTotal{status="rate_limited"}.
	RevenueCatAPIRateLimitedTotal = promauto.NewCounter(prometheus.CounterOpts{
		Name: "revenuecat_api_rate_limited_total",
		Help: "Total RevenueCat API operations rejected with HTTP 429. Alert on a sustained rate: the provider is throttling us and refresh work is being deferred.",
	})

	// RevenueCatWebhookReceivedTotal counts NEW accepted webhook receipts, by normalized event
	// type.
	//
	// ⚠ REDELIVERIES DO NOT COUNT. repository.RevenueCatRepository.AcceptWebhook dedupes on
	// event_id and bumps `deliveries` on a repeat, so a counter incremented on every delivery would
	// measure provider retry pressure, not throughput. This counter increments only when the insert
	// created a NEW row (the accepted==true path). The redelivery count is separately visible as
	// sum(revenuecat_webhooks.deliveries) on the admin RevenueCat health surface.
	//
	// ⚠ event_type is stored RAW from the payload and the handler length-checks it (<=100) without
	// checking membership in a fixed set, so it is NOT bounded as-is. It is normalized through
	// NormalizeRevenueCatEventType before labelling; an unrecognised type becomes "other".
	RevenueCatWebhookReceivedTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "revenuecat_webhook_received_total",
		Help: "Total NEW accepted RevenueCat webhook receipts by normalized event type. Redeliveries (deduped by event_id) are deliberately NOT counted — this measures throughput, not provider retry pressure. Unrecognised event types are folded into \"other\".",
	}, []string{"event_type"})

	// RevenueCatWebhookProcessingDurationSeconds tracks how long the synchronous webhook ingestion
	// path takes — signature verification, identity resolution, and the transactional inbox write
	// plus job enqueue — as observed by the HTTP handler. It is measured there because the handler
	// is the only place that knows the bounded event type without threading it through the River
	// job args (which would change the args bytes and therefore the dedup key) or paying an extra
	// DB read per job. The ASYNCHRONOUS worker's processing duration is the generic
	// job_duration_seconds{kind="revenuecat_sync"} every River worker emits.
	RevenueCatWebhookProcessingDurationSeconds = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "revenuecat_webhook_processing_duration_seconds",
		Help:    "Duration of the synchronous RevenueCat webhook ingestion path (verify, resolve identity, transactional inbox write + enqueue), by normalized event type. The async worker's duration is job_duration_seconds{kind=\"revenuecat_sync\"}.",
		Buckets: prometheus.DefBuckets,
	}, []string{"event_type"})

	// RevenueCatWebhookFailedTotal counts webhook deliveries the handler REJECTED, by normalized
	// event type and a FIXED reason class.
	//
	// ⚠ reason_class is a fixed enum (invalid_signature, malformed_payload, unresolved_customer,
	// unavailable, queue_error, other) derived from WHERE the rejection happened — never a raw or
	// wrapped error string, which would reintroduce exactly the unbounded label this file's
	// cardinality guard exists to catch.
	//
	// Scope boundary: this counts INGESTION rejections. An accepted delivery whose asynchronous
	// processing later fails is not counted here (the worker does not know the event type); it is
	// visible as a stuck row in RevenueCatWebhookInboxPending plus the row's `last_error`, and in
	// the generic River job failure/dead-letter metrics.
	RevenueCatWebhookFailedTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "revenuecat_webhook_failed_total",
		Help: "Total RevenueCat webhook deliveries rejected during ingestion, by normalized event type and fixed reason class (invalid_signature, malformed_payload, unresolved_customer, unavailable, queue_error, other). Asynchronous processing failures are not counted here; see revenuecat_webhook_inbox_pending.",
	}, []string{"event_type", "reason_class"})

	// RevenueCatResolutionTotal counts entitlement-resolution outcomes (todo 5's read-path signal):
	// resolved from the cached projection, unresolved after a successful refresh, or error when the
	// refresh itself failed.
	RevenueCatResolutionTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "revenuecat_resolution_total",
		Help: "Total RevenueCat entitlement-resolution attempts by outcome (resolved, unresolved, error). resolved = answered from the cached projection or a successful refresh; unresolved = refresh succeeded but the state still does not grant; error = refresh failed.",
	}, []string{"outcome"})

	// RevenueCatGrantsAppliedTotal counts grant intents successfully applied to the provider, by
	// grant class (trial, comp). Incremented at ProcessOperation's success path — the same
	// transaction that calls FinishOperation(stage="applied").
	RevenueCatGrantsAppliedTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "revenuecat_grants_applied_total",
		Help: "Total RevenueCat grant intents successfully applied to the provider, by grant class (trial, comp).",
	}, []string{"class"})

	// RevenueCatGrantsExpiredTotal counts grant lifecycle endings, by grant class (trial, comp,
	// paid). Incremented on the same once-per-transition predicate as the access-loss product
	// events, so a repeated inactive refresh does not re-count.
	RevenueCatGrantsExpiredTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "revenuecat_grants_expired_total",
		Help: "Total RevenueCat grants that ended (expired or lapsed), by grant class (trial, comp, paid).",
	}, []string{"class"})

	// RevenueCatOperationFailuresTotal counts FAILED RevenueCat operation applications, by grant
	// class. This is the FLOW companion to RevenueCatOperationsErrored (the STOCK): one increments
	// at the failure write site, the other is a query read at scrape time.
	//
	// ⚠ These are deliberately two different metrics. Declaring the query-derived count a
	// `*_total` counter would yield a counter that resets whenever the stock falls — a silent
	// alerting bug the plan's counter-vs-gauge rule calls out explicitly.
	RevenueCatOperationFailuresTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "revenuecat_operation_failures_total",
		Help: "Total RevenueCat operation applications that failed, by grant class. The flow companion to revenuecat_operations_errored (the stock).",
	}, []string{"class"})

	// RevenueCatOperationAbandonmentsTotal counts operations written to the TERMINAL 'abandoned'
	// stage after reaching the attempt cap, by grant class. This is deliberately separate from
	// RevenueCatOperationFailuresTotal: the failure counter is the retryable-failure FLOW whose
	// stock companion (revenuecat_operations_errored) counts only rows STILL stage='pending', and an
	// abandoned row has left that population for good. Folding abandonment into the failure counter
	// would overstate the retry flow and make a permanently stuck operation indistinguishable from a
	// transient one. This counter is the alertable signal for "an operation is not converging".
	RevenueCatOperationAbandonmentsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "revenuecat_operation_abandonments_total",
		Help: "Total RevenueCat operations abandoned (terminal stage) after exceeding the attempt cap, by grant class. Distinct from revenuecat_operation_failures_total, which counts retryable failures still pending.",
	}, []string{"class"})

	// RevenueCatRefreshesTotal counts access-refresh outcomes. `not_due` is a refresh call that
	// found a still-valid cached projection and did no provider I/O — healthy and expected on a
	// duplicate webhook or a coalesced repair pass, so it is NOT an error.
	RevenueCatRefreshesTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "revenuecat_refreshes_total",
		Help: "Total RevenueCat access refreshes by outcome (success, error, not_due). not_due means a still-valid cached projection was found and no provider I/O was done — healthy, not an error.",
	}, []string{"outcome"})

	// RevenueCatRefreshDurationSeconds tracks how long access refreshes take, across all outcomes.
	RevenueCatRefreshDurationSeconds = promauto.NewHistogram(prometheus.HistogramOpts{
		Name:    "revenuecat_refresh_duration_seconds",
		Help:    "Duration of RevenueCat access refreshes in seconds.",
		Buckets: prometheus.DefBuckets,
	})

	// RevenueCatRetentionEpisodesArmedTotal counts retention episodes ARMED by access loss — the
	// moment a previously-granting user flips to an inactive projection and the safety net starts
	// its clock (retention_started_at / plaid_cleanup_pending_at stamped).
	//
	// This is the ONLY genuinely new signal in the retention/Plaid safety net, and it is the FLOW
	// companion to the existing PlaidCleanupOutstandingUsers /
	// PlaidCleanupOldestOutstandingSeconds gauges, which show the STOCK. No existing metric
	// captures the arming rate, so no metric is duplicated. Incremented at the service's arming
	// decision (revenuecat_service.go), independent of whether analytics is configured.
	RevenueCatRetentionEpisodesArmedTotal = promauto.NewCounter(prometheus.CounterOpts{
		Name: "revenuecat_retention_episodes_armed_total",
		Help: "Total retention episodes armed by RevenueCat access loss (a granting user flipped inactive and the Plaid-cleanup safety net started its clock). The flow companion to plaid_cleanup_outstanding_users (the stock).",
	})

	// --- Backlog STOCK gauges, all query-derived and therefore UnmeasuredGauges ---
	//
	// Every gauge below is fed by cmd/server/main.go's startJobHealthMetricsCollector from a
	// repository.JobHealthRepository method, and is ABSENT from the scrape until that collector has
	// actually read the database. That is deliberate and is the UnmeasuredGauge contract (see
	// metrics.go): a plain Gauge would export a confident 0 from process start, and for a backlog
	// gauge "no backlog" is exactly the reading an operator is most likely to trust without
	// checking when it was last refreshed. Each declaration carries its own absent() rule.

	// RevenueCatWebhookInboxPending is the count of accepted webhook receipts not yet processed
	// (revenuecat_webhooks.processed_at IS NULL). A stuck-but-not-failed inbox is a silent access
	// desync that the flow counters cannot see.
	//
	//	revenuecat_webhook_inbox_pending > 0
	//	  for: 30m
	//	absent(revenuecat_webhook_inbox_pending)
	//	  for: 1h
	RevenueCatWebhookInboxPending = UnmeasuredGauge(prometheus.GaugeOpts{
		Name: "revenuecat_webhook_inbox_pending",
		Help: "Number of accepted RevenueCat webhook receipts not yet processed (processed_at IS NULL); 0 when the inbox is drained. A stuck-but-not-failed inbox is a silent access desync. Absent until the collector has actually read the database, so alert on absence too.",
	})

	// RevenueCatOldestUnprocessedWebhookSeconds is the age of the OLDEST unprocessed receipt. It
	// is the chronic-failure signal the pending count alone cannot express.
	RevenueCatOldestUnprocessedWebhookSeconds = UnmeasuredGauge(prometheus.GaugeOpts{
		Name: "revenuecat_oldest_unprocessed_webhook_seconds",
		Help: "Age in seconds of the oldest unprocessed RevenueCat webhook receipt; 0 when none are pending. Absent until the collector has actually read the database, so alert on absence too.",
	})

	// RevenueCatOperationsPending is the count of RevenueCat operation intents awaiting application
	// (revenuecat_operations.stage = 'pending').
	RevenueCatOperationsPending = UnmeasuredGauge(prometheus.GaugeOpts{
		Name: "revenuecat_operations_pending",
		Help: "Number of RevenueCat operation intents awaiting application (stage = 'pending'); 0 when the queue is drained. Absent until the collector has actually read the database, so alert on absence too.",
	})

	// RevenueCatOldestPendingOperationAgeSeconds is the age of the OLDEST pending operation intent.
	RevenueCatOldestPendingOperationAgeSeconds = UnmeasuredGauge(prometheus.GaugeOpts{
		Name: "revenuecat_oldest_pending_operation_age_seconds",
		Help: "Age in seconds of the oldest pending RevenueCat operation intent; 0 when none are pending. Absent until the collector has actually read the database, so alert on absence too.",
	})

	// RevenueCatOperationsErrored is the STOCK of currently-errored operation intents
	// (stage = 'pending' AND last_error <> ''). There is deliberately no `failed` stage: a failure
	// stays pending with last_error set, and is retried.
	//
	// ⚠ This is a gauge, not a counter, precisely because it is derived from a query. Its FLOW
	// companion is RevenueCatOperationFailuresTotal, incremented at the failure write site.
	//
	//	revenuecat_operations_errored > 0
	//	  for: 30m
	//	absent(revenuecat_operations_errored)
	//	  for: 1h
	RevenueCatOperationsErrored = UnmeasuredGauge(prometheus.GaugeOpts{
		Name: "revenuecat_operations_errored",
		Help: "Number of RevenueCat operation intents that are still pending but have recorded a last_error (stage = 'pending' AND last_error <> ''); there is no 'failed' stage, a failure stays pending and is retried. The STOCK companion to revenuecat_operation_failures_total (the flow). Absent until the collector has actually read the database, so alert on absence too.",
	})

	// RevenueCatCompGrantsOutstanding is the count of currently-live comp_* grants in RevenueCat's
	// stored projection. It is the population whose health the existing
	// RevenueCatOldestExpiredCompGrantSeconds gauge measures the worst case of.
	RevenueCatCompGrantsOutstanding = UnmeasuredGauge(prometheus.GaugeOpts{
		Name: "revenuecat_comp_grants_outstanding",
		Help: "Number of currently-live comp_* grants in RevenueCat's stored projection; 0 when none are comped. Absent until the collector has actually read the database, so alert on absence too.",
	})

	// RevenueCatRefreshOverdueUsers is the count of customers whose next_refresh_at is in the past
	// — the refresh backlog. A steadily growing value means the refresh/repair worker is not
	// keeping up (or has stopped), independent of whether its own job metrics are being scraped.
	RevenueCatRefreshOverdueUsers = UnmeasuredGauge(prometheus.GaugeOpts{
		Name: "revenuecat_refresh_overdue_users",
		Help: "Number of RevenueCat customers whose next_refresh_at is in the past (refresh backlog); 0 when every customer is within its refresh window. Absent until the collector has actually read the database, so alert on absence too.",
	})
)

// --- Bounded label vocabularies -------------------------------------------------------------

// revenueCatBoundedLabelNames is the fixed allowlist of label NAMES any `revenuecat_*` metric may
// carry. The cardinality guard walks the metric registry and fails on anything else; this is the
// list it checks against. It is a NAMES allowlist, and the per-label VALUE allowlists below are
// what keep each name bounded.
var revenueCatBoundedLabelNames = map[string]bool{
	"operation":    true,
	"status":       true,
	"event_type":   true,
	"reason_class": true,
	"outcome":      true,
	"class":        true,
}

// RevenueCat operation labels. Every value is a fixed client method, never a request path.
const (
	RevenueCatOperationEnsureCustomer     = "ensure_customer"
	RevenueCatOperationFetchSnapshot      = "fetch_snapshot"
	RevenueCatOperationFetchEntitlements  = "fetch_entitlements"
	RevenueCatOperationFetchSubscriptions = "fetch_subscriptions"
	RevenueCatOperationGrant              = "grant"
	RevenueCatOperationRevoke             = "revoke"
	RevenueCatOperationDeleteCustomer     = "delete_customer"
	RevenueCatOperationCustomerExists     = "customer_exists"
)

var revenueCatOperations = map[string]bool{
	RevenueCatOperationEnsureCustomer:     true,
	RevenueCatOperationFetchSnapshot:      true,
	RevenueCatOperationFetchEntitlements:  true,
	RevenueCatOperationFetchSubscriptions: true,
	RevenueCatOperationGrant:              true,
	RevenueCatOperationRevoke:             true,
	RevenueCatOperationDeleteCustomer:     true,
	RevenueCatOperationCustomerExists:     true,
}

// RevenueCat API status labels.
const (
	RevenueCatAPIStatusSuccess     = "success"
	RevenueCatAPIStatusError       = "error"
	RevenueCatAPIStatusRateLimited = "rate_limited"
)

var revenueCatAPIStatuses = map[string]bool{
	RevenueCatAPIStatusSuccess:     true,
	RevenueCatAPIStatusError:       true,
	RevenueCatAPIStatusRateLimited: true,
}

// RevenueCat webhook failure reason classes, derived from WHERE the rejection happened.
const (
	RevenueCatWebhookReasonInvalidSignature   = "invalid_signature"
	RevenueCatWebhookReasonMalformedPayload   = "malformed_payload"
	RevenueCatWebhookReasonUnresolvedCustomer = "unresolved_customer"
	RevenueCatWebhookReasonUnavailable        = "unavailable"
	RevenueCatWebhookReasonQueueError         = "queue_error"
	RevenueCatWebhookReasonOther              = RevenueCatLabelOther
)

// RevenueCatLabelOther is the shared fail-safe bucket every bounded vocabulary folds an
// unrecognised value into.
const RevenueCatLabelOther = "other"

var revenueCatWebhookReasonClasses = map[string]bool{
	RevenueCatWebhookReasonInvalidSignature:   true,
	RevenueCatWebhookReasonMalformedPayload:   true,
	RevenueCatWebhookReasonUnresolvedCustomer: true,
	RevenueCatWebhookReasonUnavailable:        true,
	RevenueCatWebhookReasonQueueError:         true,
	RevenueCatWebhookReasonOther:              true,
}

// RevenueCat resolution outcomes.
const (
	RevenueCatResolutionResolved   = "resolved"
	RevenueCatResolutionUnresolved = "unresolved"
	RevenueCatResolutionError      = "error"
)

var revenueCatResolutionOutcomes = map[string]bool{
	RevenueCatResolutionResolved:   true,
	RevenueCatResolutionUnresolved: true,
	RevenueCatResolutionError:      true,
}

// RevenueCat refresh outcomes.
const (
	RevenueCatRefreshSuccess = "success"
	RevenueCatRefreshError   = "error"
	RevenueCatRefreshNotDue  = "not_due"
)

var revenueCatRefreshOutcomes = map[string]bool{
	RevenueCatRefreshSuccess: true,
	RevenueCatRefreshError:   true,
	RevenueCatRefreshNotDue:  true,
}

// RevenueCat grant classes. `paid` only ever appears on RevenueCatGrantsExpiredTotal: a paid grant
// is applied by the provider's own purchase flow, never by our operation queue.
const (
	RevenueCatGrantClassTrial = "trial"
	RevenueCatGrantClassComp  = "comp"
	RevenueCatGrantClassPaid  = "paid"
)

var revenueCatGrantClasses = map[string]bool{
	RevenueCatGrantClassTrial: true,
	RevenueCatGrantClassComp:  true,
	RevenueCatGrantClassPaid:  true,
}

// revenueCatEventTypes is the bounded vocabulary of RevenueCat webhook event types we recognise.
// It mirrors integration/revenuecat.ClassifyAccessChange's switch plus the lifecycle events that
// deliberately do NOT change access (CANCELLATION, UNCANCELLATION, BILLING_ISSUE, TRANSFER) and the
// TEST delivery probe. Anything outside this set — including a type RevenueCat adds later — is
// folded into "other" rather than used as a label, which is the fail-safe direction.
var revenueCatEventTypes = map[string]bool{
	"INITIAL_PURCHASE":            true,
	"RENEWAL":                     true,
	"NON_RENEWING_PURCHASE":       true,
	"PRODUCT_CHANGE":              true,
	"SUBSCRIPTION_EXTENDED":       true,
	"TEMPORARY_ENTITLEMENT_GRANT": true,
	"EXPIRATION":                  true,
	"SUBSCRIPTION_PAUSED":         true,
	"CANCELLATION":                true,
	"UNCANCELLATION":              true,
	"BILLING_ISSUE":               true,
	"TRANSFER":                    true,
	"TEST":                        true,
	RevenueCatLabelOther:          true,
}

// NormalizeRevenueCatEventType bounds a raw event type to revenueCatEventTypes, folding anything
// unrecognised (or empty) into "other". The raw value is stored on the receipt row unchanged; this
// only governs the LABEL.
func NormalizeRevenueCatEventType(raw string) string {
	if revenueCatEventTypes[raw] {
		return raw
	}
	return RevenueCatLabelOther
}

func normalizeRevenueCatOperation(op string) string {
	if revenueCatOperations[op] {
		return op
	}
	return RevenueCatLabelOther
}

func normalizeRevenueCatAPIStatus(status string) string {
	if revenueCatAPIStatuses[status] {
		return status
	}
	return RevenueCatAPIStatusError
}

func normalizeRevenueCatWebhookReason(reason string) string {
	if revenueCatWebhookReasonClasses[reason] {
		return reason
	}
	return RevenueCatLabelOther
}

func normalizeRevenueCatResolutionOutcome(outcome string) string {
	if revenueCatResolutionOutcomes[outcome] {
		return outcome
	}
	return RevenueCatResolutionError
}

func normalizeRevenueCatRefreshOutcome(outcome string) string {
	if revenueCatRefreshOutcomes[outcome] {
		return outcome
	}
	return RevenueCatRefreshError
}

func normalizeRevenueCatGrantClass(class string) string {
	if revenueCatGrantClasses[class] {
		return class
	}
	return RevenueCatLabelOther
}

// --- Recording helpers ----------------------------------------------------------------------
//
// Every helper normalizes its label values through the allowlists above. Call sites pass the
// natural value and cannot widen the label set by accident.

// RecordRevenueCatAPIRequest records one logical client operation and its outcome.
func RecordRevenueCatAPIRequest(operation, status string) {
	RevenueCatAPIRequestsTotal.WithLabelValues(normalizeRevenueCatOperation(operation), normalizeRevenueCatAPIStatus(status)).Inc()
}

// ObserveRevenueCatAPIRequestDuration records one logical client operation's duration.
func ObserveRevenueCatAPIRequestDuration(operation string, seconds float64) {
	RevenueCatAPIRequestDurationSeconds.WithLabelValues(normalizeRevenueCatOperation(operation)).Observe(seconds)
}

// RecordRevenueCatRateLimited records a provider 429.
func RecordRevenueCatRateLimited() { RevenueCatAPIRateLimitedTotal.Inc() }

// RecordRevenueCatWebhookReceived records a NEW accepted webhook receipt. Never call this for a
// deduped redelivery.
func RecordRevenueCatWebhookReceived(eventType string) {
	RevenueCatWebhookReceivedTotal.WithLabelValues(NormalizeRevenueCatEventType(eventType)).Inc()
}

// ObserveRevenueCatWebhookProcessingDuration records the synchronous ingestion duration.
func ObserveRevenueCatWebhookProcessingDuration(eventType string, seconds float64) {
	RevenueCatWebhookProcessingDurationSeconds.WithLabelValues(NormalizeRevenueCatEventType(eventType)).Observe(seconds)
}

// RecordRevenueCatWebhookFailed records an ingestion rejection.
func RecordRevenueCatWebhookFailed(eventType, reasonClass string) {
	RevenueCatWebhookFailedTotal.WithLabelValues(NormalizeRevenueCatEventType(eventType), normalizeRevenueCatWebhookReason(reasonClass)).Inc()
}

// RecordRevenueCatResolution records an entitlement-resolution outcome.
func RecordRevenueCatResolution(outcome string) {
	RevenueCatResolutionTotal.WithLabelValues(normalizeRevenueCatResolutionOutcome(outcome)).Inc()
}

// RecordRevenueCatGrantApplied records a successfully applied grant intent.
func RecordRevenueCatGrantApplied(class string) {
	RevenueCatGrantsAppliedTotal.WithLabelValues(normalizeRevenueCatGrantClass(class)).Inc()
}

// RecordRevenueCatGrantExpired records a grant that ended.
func RecordRevenueCatGrantExpired(class string) {
	RevenueCatGrantsExpiredTotal.WithLabelValues(normalizeRevenueCatGrantClass(class)).Inc()
}

// RecordRevenueCatOperationFailure records a failed operation application.
func RecordRevenueCatOperationFailure(class string) {
	RevenueCatOperationFailuresTotal.WithLabelValues(normalizeRevenueCatGrantClass(class)).Inc()
}

// RecordRevenueCatOperationAbandoned records an operation that reached its attempt cap and was
// written to the terminal 'abandoned' stage. Never call this for a retryable failure.
func RecordRevenueCatOperationAbandoned(class string) {
	RevenueCatOperationAbandonmentsTotal.WithLabelValues(normalizeRevenueCatGrantClass(class)).Inc()
}

// ObserveRevenueCatRefresh records a refresh outcome and its duration.
func ObserveRevenueCatRefresh(outcome string, seconds float64) {
	RevenueCatRefreshesTotal.WithLabelValues(normalizeRevenueCatRefreshOutcome(outcome)).Inc()
	RevenueCatRefreshDurationSeconds.Observe(seconds)
}

// RecordRevenueCatRetentionEpisodeArmed records access loss arming the retention safety net.
func RecordRevenueCatRetentionEpisodeArmed() { RevenueCatRetentionEpisodesArmedTotal.Inc() }

// PublishRevenueCatWebhookInbox publishes the inbox backlog pair.
func PublishRevenueCatWebhookInbox(pending int, oldestSeconds float64) {
	RevenueCatWebhookInboxPending.WithLabelValues().Set(float64(pending))
	RevenueCatOldestUnprocessedWebhookSeconds.WithLabelValues().Set(max(oldestSeconds, 0))
}

// PublishRevenueCatOperations publishes the operations-queue backlog triple.
func PublishRevenueCatOperations(pending, errored int, oldestSeconds float64) {
	RevenueCatOperationsPending.WithLabelValues().Set(float64(pending))
	RevenueCatOperationsErrored.WithLabelValues().Set(float64(errored))
	RevenueCatOldestPendingOperationAgeSeconds.WithLabelValues().Set(max(oldestSeconds, 0))
}

// PublishRevenueCatCompGrantsOutstanding publishes the live comp-grant count.
func PublishRevenueCatCompGrantsOutstanding(outstanding int) {
	RevenueCatCompGrantsOutstanding.WithLabelValues().Set(float64(outstanding))
}

// PublishRevenueCatRefreshOverdueUsers publishes the refresh backlog.
func PublishRevenueCatRefreshOverdueUsers(overdue int) {
	RevenueCatRefreshOverdueUsers.WithLabelValues().Set(float64(overdue))
}
