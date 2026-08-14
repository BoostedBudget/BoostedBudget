package cache

// This file declares this package's own Prometheus collectors, following the same promauto
// pattern used by internal/observability (Task 2) — see internal/observability/redis_metrics.go.
// internal/cache deliberately does NOT import internal/observability (see the Task 9 brief): a
// caching primitives package should not depend on the wider observability wiring, so its metrics
// are self-contained here.
//
// Label hygiene (same rule as Task 2): NEVER label with a cache key, user ID, IP, URL, or raw
// error string. Every label used below is a bounded, small, compile-time-known enum:
//   - namespace: the 6 registered Namespace constants
//   - result: "hit" | "miss" | "error" | "bypass_disabled" | "bypass_gen_error" | "bypass_circuit"
//   - op: "get" | "set"
//   - kind: "codec" | "schema_mismatch" | "redis"
//   - scope_kind: "user" | "household" | "clerk_id"
//   - source: caller-supplied but expected to be a short static literal (e.g. "invalidation"),
//     never a raw identifier — Task 10 owns actual call sites and must uphold this.

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	cacheRequestsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "cache_requests_total",
		Help: "Total GetOrLoad calls, by namespace and result.",
	}, []string{"namespace", "result"})

	cacheOperationDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name: "cache_operation_duration_seconds",
		Help: "Duration of individual cache operations, by namespace and op.",
	}, []string{"namespace", "op"})

	cacheErrorsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "cache_errors_total",
		Help: "Redis/codec errors encountered while serving a cache request, by namespace, op, and kind.",
	}, []string{"namespace", "op", "kind"})

	cachePayloadBytes = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "cache_payload_bytes",
		Help:    "Size in bytes of cached payloads written, by namespace.",
		Buckets: prometheus.ExponentialBuckets(64, 4, 8), // 64B .. ~1MB
	}, []string{"namespace"})

	cacheSingleflightSharedTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "cache_singleflight_shared_total",
		Help: "Number of GetOrLoad calls that shared an in-flight loader call rather than issuing their own, by namespace.",
	}, []string{"namespace"})

	cacheGenerationBumpsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "cache_generation_bumps_total",
		Help: "Number of successful generation bumps (invalidations), by scope_kind and source.",
	}, []string{"scope_kind", "source"})

	cacheGenerationMintsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "cache_generation_mints_total",
		Help: "Number of times a generation token was minted because the previous one was absent (cold start or eviction), by scope_kind.",
	}, []string{"scope_kind"})

	cacheBumpFailuresTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "cache_bump_failures_total",
		Help: "Number of generation-bump writes that failed. ALERT ON THIS: the underlying write already committed, so a failure here means stale reads may be served until the generation key is naturally lost or bumped again.",
	}, []string{"scope_kind"})

	cacheStaleEnvelopeTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "cache_stale_envelope_total",
		Help: "Number of reads that found an envelope older than twice the namespace TTL (an operator raised the TTL or restored a snapshot), by namespace.",
	}, []string{"namespace"})
)

// Result labels for cacheRequestsTotal — a closed enum, never derived from anything caller-
// supplied.
const (
	resultHit              = "hit"
	resultMiss             = "miss"
	resultError            = "error"
	resultBypassDisabled   = "bypass_disabled"
	resultBypassGenError   = "bypass_gen_error"
	resultBypassCircuit    = "bypass_circuit"
	resultBypassTombstoned = "bypass_tombstoned"
)

const (
	opGet = "get"
	opSet = "set"
)

const (
	errKindCodec          = "codec"
	errKindSchemaMismatch = "schema_mismatch"
	errKindRedis          = "redis"
)
