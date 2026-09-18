package middleware

import (
	"container/list"
	"context"
	"log/slog"
	"net"
	"net/http"
	"net/netip"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/observability"
	"golang.org/x/time/rate"
)

// defaultRedisLimiterTimeout bounds how long IPRateLimiter waits on the injected primary
// (Redis-backed) RateLimitBackend before treating it as failed and falling back to memory. Task
// 7/8 (main.go wiring) may make this configurable; until then this fixed value keeps the primary
// call bounded regardless of the caller's own request context (see fallbackLimiter's doc comment
// on why the probe is detached from that context entirely).
const defaultRedisLimiterTimeout = 50 * time.Millisecond

// RateLimitConfig configures the rate limiting middleware.
type RateLimitConfig struct {
	// IPLimit is the maximum number of requests per window per IP address.
	// Default: 100
	IPLimit int

	// AuthEndpointLimit is the stricter limit applied to sensitive paths like
	// /webhooks/* and /api/v1/qa/auth/*. Default: 10
	AuthEndpointLimit int

	// ExemptPathLimit is the budget backing isExemptPath paths (/health) in their OWN dedicated
	// memory store, entirely separate from IPLimit's regular-tier store. Default (<=0): IPLimit.
	//
	// Exempt paths always use the memory backend directly (see isExemptPath's callers), never the
	// Redis-backed primary — a Redis blip must never threaten Fly's liveness probe. In production,
	// IPLimit is built from cfg.RateLimitIP DIVIDED by cfg.RateLimitFallbackDivisor (see
	// cmd/server's rateLimitFallbackLimit): N machines each independently falling back to memory
	// must not multiply a FLEET-WIDE budget by N. But /health's budget was never fleet-wide to
	// begin with — it's Fly's own single-machine liveness check — so applying that divisor to it
	// simply halves its real budget in completely normal operation, no outage required (Issue
	// #1531 L3). ExemptPathLimit lets the caller pass the UNDIVIDED cfg.RateLimitIP here instead,
	// giving /health its own capacity independent of both public regular-tier traffic (already true
	// pre-#1531 via isExemptPath's per-source keying) and the fleet-machine-count divisor.
	ExemptPathLimit int

	// WindowDuration is the rate limit window. Default: 1 minute
	WindowDuration time.Duration

	// CleanupInterval controls how often stale limiters are purged. Default: 5 min
	CleanupInterval time.Duration

	// MaxEntries hard-caps the number of distinct identities each limiter store
	// tracks. Once a store holds MaxEntries entries, adding a new identity
	// evicts the least-recently-seen entry first, so the store's size never
	// exceeds MaxEntries — the cap is enforced on every insert, not just at
	// cleanup time. Default: 20000.
	MaxEntries int

	// PrecheckLimit enables an in-process token bucket that is consulted BEFORE Backend, purely
	// to bound COST. Backend is billed per command (Redis), and IPRateLimiter consults it on
	// every request — including the ones it goes on to reject — so before this existed, a flood
	// against an endpoint with no Cloudflare rate-limit rule cost one billable command per
	// request, without bound. /webhooks/ is exactly such an endpoint: it is deliberately excluded
	// from the edge rate-limit rule so real provider deliveries are never dropped. With
	// PrecheckLimit set, once one client identity exhausts its bucket, further requests are
	// rejected without touching Backend at all.
	//
	// 🔴 INVARIANT: the pre-check must NEVER deny a request Backend would have allowed. It is a
	// cost ceiling, not a second enforcement layer.
	//
	// ⚠ The first draft of this mechanism was a SINGLE bucket keyed on the RAW identity, and two
	// independent adversarial reviewers each EXECUTED reproductions breaking that invariant three
	// ways. All three had one root cause — one bucket, raw key, standing in for THREE
	// independently-budgeted authoritative buckets keyed on a CANONICAL identity:
	//
	//   1. CROSS-TIER. Backend namespaces its buckets by tier (ratelimitstore bucketKey), so one
	//      identity legitimately holds regular + auth allowance. A shared bucket charged both to
	//      one budget: 100 regular requests then a single legitimate POST /webhooks/plaid → 429,
	//      on the traffic that must never be dropped.
	//   2. SENTINEL. RATE_LIMIT_SENTINEL_LIMIT is a documented escape hatch for RAISING the
	//      sentinel budget during a live Cloudflare header anomaly. A ceiling derived from the
	//      regular limit silently neutered that knob during the exact incident it exists for,
	//      affecting every customer at once (they all share the sentinel key).
	//   3. REFILL. Backend derives GCRA's emission interval by INTEGER microsecond division
	//      (ratelimitstore.emissionAndDVT), so for a limit that does not divide the window evenly
	//      — 34/min is 1_764_705us vs 1_764_705.88us — truncation makes GCRA fractionally MORE
	//      permissive than a float-rate token bucket.
	//
	// So each tier now gets its OWN pre-check store, sized from that tier's own UNDIVIDED
	// authoritative limit and multiplied by precheckHeadroomFactor. The headroom is the answer to
	// (3) and it is deliberate: a COST ceiling does not need to be tight, and slack makes "never
	// stricter" robust rather than arithmetically lucky — it also absorbs clock skew and the fact
	// that this bucket is per-MACHINE while Backend's is fleet-wide. Bounded at
	// factor × limit commands per identity per window is still bounded; unbounded was the bug.
	//
	// This is also why the existing per-tier memory stores cannot serve as the pre-check: IPLimit
	// is already fleet-DIVIDED by the time it reaches them (see IPLimit's doc comment), so they
	// reject well below the authoritative fleet-wide limit — correct as an outage fallback, wrong
	// as a pre-check. Pass the UNDIVIDED per-tier limits here.
	//
	// ALL THREE ARE OPTIONAL and independent. A tier whose limit is <=0 has its pre-check
	// DISABLED and consults Backend on every request exactly as before — deliberately NOT
	// defaulted to one of the others, because a silent default is how (2) happened. Every existing
	// call site and test that omits these fields is unchanged.
	PrecheckLimit         int // regular tier
	PrecheckAuthLimit     int // auth tier (isAuthEndpoint: /webhooks/*, /api/v1/qa/auth/*)
	PrecheckSentinelLimit int // sentinel tier (unresolved client identity)

	// Backend is the primary (typically Redis-backed, fleet-wide) rate-limit backend. OPTIONAL:
	// nil means memory-only, exactly today's pre-#1531 behaviour — every existing call site and
	// test that constructs a RateLimitConfig without this field compiles and behaves unchanged.
	// When set, IPRateLimiter wraps it in a fallback decorator (see fallbackLimiter) so a Backend
	// outage degrades to the in-memory limiter rather than failing the request path open or
	// closed. Exempt paths (see isExemptPath) never consult Backend at all, by design.
	Backend RateLimitBackend

	// Done, when non-nil, stops the background cleanup goroutine when closed. OPTIONAL: nil means
	// "never stop" — a nil channel blocks forever in a select, which is exactly today's behaviour
	// (the goroutine ran for the life of the process either way). Set this in tests, or from a
	// caller with its own shutdown signal, to avoid leaking the goroutine.
	Done <-chan struct{}

	// Logger receives fallback-activation/recovery log lines (see fallbackLimiter). OPTIONAL: nil
	// uses slog.Default().
	Logger *slog.Logger

	// PrimaryTimeout bounds how long a single Allow call waits on Backend before treating it as
	// failed and falling back to memory (see fallbackLimiter's doc comment). Default (<=0):
	// defaultRedisLimiterTimeout (50ms).
	//
	// Issue #1531 L7: this used to be the unconditional, uncustomizable defaultRedisLimiterTimeout
	// constant, with no RateLimitConfig field feeding it at all -- so cfg.RedisTimeoutMs
	// (REDIS_TIMEOUT_MS), which DOES reach the go-redis client's own read/write timeouts (see
	// cmd/server's newRedisClientOptions), had no way to reach this budget too. Raising
	// REDIS_TIMEOUT_MS during a real latency incident changed nothing about the limiter's 50ms
	// cutoff, which kept tripping the circuit breaker regardless of the "operative knob" an
	// operator had just turned. main.go now passes cfg.RedisTimeoutMs here explicitly.
	PrimaryTimeout time.Duration
}

func (c *RateLimitConfig) withDefaults() RateLimitConfig {
	cfg := *c
	if cfg.IPLimit <= 0 {
		cfg.IPLimit = 100
	}
	if cfg.AuthEndpointLimit <= 0 {
		cfg.AuthEndpointLimit = 10
	}
	if cfg.ExemptPathLimit <= 0 {
		cfg.ExemptPathLimit = cfg.IPLimit
	}
	if cfg.WindowDuration <= 0 {
		cfg.WindowDuration = time.Minute
	}
	if cfg.CleanupInterval <= 0 {
		cfg.CleanupInterval = 5 * time.Minute
	}
	if cfg.MaxEntries <= 0 {
		cfg.MaxEntries = 20000
	}
	if cfg.PrimaryTimeout <= 0 {
		cfg.PrimaryTimeout = defaultRedisLimiterTimeout
	}
	return cfg
}

// isAuthEndpoint returns true for paths that should use the stricter rate limit.
func isAuthEndpoint(path string) bool {
	return strings.HasPrefix(path, "/webhooks/") ||
		strings.HasPrefix(path, "/api/v1/qa/auth/")
}

// identitySentinelFallback is the rate-limit bucket key used when a
// Cloudflare-gated request (GateStatusPassed) has no verifiable client
// identity — see clientIdentity. It is a fixed string that netip.ParseAddr can
// never produce from a real address (IP addresses cannot contain "!"), so it
// is guaranteed to never collide with a real per-client bucket.
//
// Deliberate trade-off, decided explicitly rather than defaulting to a
// per-source fallback: every anomalous request that reaches this branch shares
// ONE bucket, globally, for as long as the anomaly persists — worse for
// availability than a per-source bucket, on purpose. Both per-source
// alternatives considered here were rejected: Fly-Client-Ip and RemoteAddr are
// both the Fly *proxy's* address in production (not the real end client), so
// keying on either would silently collapse many unrelated customers into one
// shared bucket while looking like a legitimate per-customer identity. The
// sentinel is deliberately never mistaken for a real identity, and pairs with
// RateLimitIdentitySentinelFallbackTotal so this path is observable: it should
// be near-zero in practice, and a rising count means Cloudflare has stopped
// sending Cf-Connecting-Ip as expected and needs investigation, not that more
// users are hitting it.
const identitySentinelFallback = "!rate-limit-identity-unresolved!"

// remoteAddrIdentity canonicalizes the raw TCP peer address (r.RemoteAddr),
// stripping the port. Unlike any HTTP header, this value is not something an
// HTTP client can set at the HTTP layer — it's the actual socket peer address
// as observed by this process.
func remoteAddrIdentity(r *http.Request) string {
	host := r.RemoteAddr
	if h, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		host = h
	}
	if addr, err := netip.ParseAddr(host); err == nil {
		return addr.Unmap().String()
	}
	// r.RemoteAddr is not client-supplied header data, so falling back to the
	// unparsed string here (e.g. a non-standard test transport) cannot be used
	// by an attacker to fabricate unlimited distinct identities the way a
	// header value could — it's still one fixed string per real connection.
	return host
}

// clientIdentity picks the per-request rate-limit bucket key. The trustworthy
// source depends on how this request cleared CloudflareOnly, which is recorded
// on the echo.Context via GateStatus before IPRateLimiter ever runs (Cloudflare
// gate is registered earlier in the middleware chain, see cmd/server/main.go):
//
//   - GateStatusPassed: CloudflareOnly already verified the immediate peer is a
//     genuine Cloudflare edge IP and the X-Origin-Secret matched, so the
//     Cf-Connecting-Ip header Cloudflare itself attaches (the real end-client
//     IP) is trustworthy. If it's missing, duplicated, or malformed, this does
//     NOT fall back to Fly-Client-Ip or RemoteAddr — both are the Fly proxy's
//     address in production, not the real client, so either would silently
//     merge unrelated customers into one bucket while looking legitimate.
//     Instead it uses identitySentinelFallback, a single fixed bucket shared by
//     all such anomalies, and increments
//     observability.RateLimitIdentitySentinelFallbackTotal so the anomaly is
//     observable rather than silent.
//   - GateStatusExemptPath: CloudflareOnly did not validate anything for this
//     request (e.g. /health, /health/ — see cloudflare.go's exemptedPaths; /metrics
//     was removed from exemptedPaths in #1531), so no header on it has been
//     verified. Fly-Client-Ip is used when present:
//     CloudflareOnly's own Layer 1 check already depends on Fly-Client-Ip being
//     set by Fly's edge from the real TCP connection rather than by the HTTP
//     client (cloudflare.go:161-190) — this is an assumption inherited from
//     that pre-existing gate, not something re-verified here, and if it were
//     false, CloudflareOnly's CIDR check would already be broken, independent
//     of this middleware. When Fly-Client-Ip is absent (e.g. a local probe with
//     no Fly proxy in front of it) fall back to RemoteAddr so the probe is
//     never rejected for a missing header.
//   - GateStatusBypassEnv / GateStatusUnknown: no gate ran at all (dev/local/
//     test/qa bypass, or CloudflareOnly isn't wired into this chain at all).
//     Trust nothing but the raw RemoteAddr.
//
// X-Forwarded-For and X-Real-IP are intentionally never consulted for identity
// in any branch: both are attacker-settable end-to-end, since Cloudflare
// appends to rather than replaces an inbound X-Forwarded-For.
func clientIdentity(c echo.Context) string {
	r := c.Request()
	switch GateStatusFrom(c) {
	case GateStatusPassed:
		if addr, ok := SingleValuedHeaderAddr(r, "Cf-Connecting-Ip"); ok {
			return addr.String()
		}
		observability.RateLimitIdentitySentinelFallbackTotal.Inc()
		return identitySentinelFallback
	case GateStatusExemptPath:
		if addr, ok := SingleValuedHeaderAddr(r, "Fly-Client-Ip"); ok {
			return addr.String()
		}
		return remoteAddrIdentity(r)
	default: // GateStatusBypassEnv, GateStatusUnknown
		return remoteAddrIdentity(r)
	}
}

// ipLimiterEntry holds a rate limiter, the identity key it belongs to (needed
// to remove it from ipLimiterStore.entries on eviction/cleanup), and the last
// time it was accessed.
type ipLimiterEntry struct {
	key      string
	limiter  *rate.Limiter
	lastSeen time.Time
}

// ipLimiterStore manages per-identity rate limiters with automatic cleanup of
// stale entries and a hard cap on the number of entries it will hold.
//
// entries and order together form an intrusive LRU: order is a doubly linked
// list with the most-recently-seen entry at the front and the
// least-recently-seen at the back, and entries maps each identity key straight
// to its *list.Element so get() never needs to scan. Every access moves its
// element to the front and refreshes its lastSeen, so list order and lastSeen
// order are always identical — which is what lets both eviction (evict the
// back) and cleanup (walk from the back until a non-stale entry is found) run
// in time proportional to work done, not to the number of entries in the
// store.
type ipLimiterStore struct {
	mu         sync.Mutex
	entries    map[string]*list.Element
	order      *list.List
	limit      rate.Limit
	burst      int
	maxEntries int
}

// precheckHeadroomFactor multiplies every pre-check budget above its authoritative counterpart.
//
// It exists because the pre-check must never be STRICTER than the authoritative backend, and
// exact parity is fragile in at least three independent ways:
//
//   - Arithmetic. The backend derives GCRA's emission interval by integer microsecond division
//     (ratelimitstore.emissionAndDVT); the pre-check is a float-rate token bucket. For a limit
//     that does not divide the window evenly (34/min → 1_764_705us vs 1_764_705.88us) truncation
//     makes GCRA fractionally more permissive, so a request at exactly the backend's interval can
//     be allowed there and refused here.
//   - Scope. The backend's bucket is FLEET-wide; this one is per-machine. Parity would make a
//     single machine's bucket the fleet's ceiling.
//   - Clocks. Redis TIME versus this process's monotonic clock.
//
// A cost ceiling does not need to be tight. Doubling keeps the bound a bound — factor × limit
// billable commands per identity per window instead of unbounded, which was the actual defect —
// while making "never stricter" robust instead of arithmetically lucky.
const precheckHeadroomFactor = 2

// precheckStores holds the per-tier cost pre-check buckets. A nil member means that tier's
// pre-check is disabled and the backend is consulted on every request, exactly as before this
// mechanism existed.
//
// ⚠ Per-TIER, mirroring ratelimitstore's bucketKey, because the authoritative backend budgets each
// tier independently. A single shared bucket charged an identity's regular traffic against its
// auth allowance and returned 429 for a legitimate POST /webhooks/plaid — see
// RateLimitConfig.PrecheckLimit for the full account of what that broke.
type precheckStores struct {
	regular  *ipLimiterStore
	auth     *ipLimiterStore
	sentinel *ipLimiterStore
}

// newPrecheckStore builds one tier's pre-check store, or nil when that tier's limit is unset.
// ⚠ Deliberately NOT defaulting an unset tier to another tier's limit: a silent default is exactly
// how the sentinel-override defect shipped.
func newPrecheckStore(limit int, window time.Duration, maxEntries int) *ipLimiterStore {
	if limit <= 0 {
		return nil
	}
	return newIPLimiterStore(limit*precheckHeadroomFactor, window, maxEntries)
}

// forTier selects the pre-check store for a tier, mirroring memoryBackend's storeFor and
// ratelimitstore's bucketKey so all three agree about what a tier is.
func (p precheckStores) forTier(tier domain.RateLimitTier) *ipLimiterStore {
	switch tier {
	case domain.RateLimitTierAuth:
		return p.auth
	case domain.RateLimitTierSentinel:
		return p.sentinel
	default:
		return p.regular
	}
}

func (p precheckStores) cleanup(olderThan time.Duration) {
	for _, s := range []*ipLimiterStore{p.regular, p.auth, p.sentinel} {
		if s != nil {
			s.cleanup(olderThan)
		}
	}
}

func newIPLimiterStore(requestsPerWindow int, window time.Duration, maxEntries int) *ipLimiterStore {
	rps := rate.Limit(float64(requestsPerWindow) / window.Seconds())
	return &ipLimiterStore{
		entries:    make(map[string]*list.Element),
		order:      list.New(),
		limit:      rps,
		burst:      requestsPerWindow, // burst equals the window limit
		maxEntries: maxEntries,
	}
}

// get returns the limiter for key, creating one if needed. If the store is at
// capacity and key is not already present, the least-recently-seen entry is
// evicted first. This guarantees len(s.entries) never exceeds s.maxEntries
// after this call returns, unconditionally (evict-before-insert, under the same
// mutex, on every call) — not merely "eventually" via the cleanup goroutine.
func (s *ipLimiterStore) get(key string) *rate.Limiter {
	s.mu.Lock()
	defer s.mu.Unlock()

	if elem, ok := s.entries[key]; ok {
		entry := elem.Value.(*ipLimiterEntry)
		entry.lastSeen = time.Now()
		s.order.MoveToFront(elem)
		return entry.limiter
	}

	if s.maxEntries > 0 && len(s.entries) >= s.maxEntries {
		s.evictStalestLocked()
	}

	entry := &ipLimiterEntry{
		key:      key,
		limiter:  rate.NewLimiter(s.limit, s.burst),
		lastSeen: time.Now(),
	}
	elem := s.order.PushFront(entry)
	s.entries[key] = elem
	return entry.limiter
}

// evictStalestLocked removes the single least-recently-seen entry (the back of
// s.order). Callers must hold s.mu. O(1): the LRU list already tracks the
// stalest entry at its tail, so this needs neither a map scan nor a re-sort —
// unlike a plain map-of-timestamps, which would need an O(n) scan to find it.
func (s *ipLimiterStore) evictStalestLocked() {
	back := s.order.Back()
	if back == nil {
		return
	}
	entry := back.Value.(*ipLimiterEntry)
	delete(s.entries, entry.key)
	s.order.Remove(back)
}

// len reports the current number of tracked entries (test/introspection use).
func (s *ipLimiterStore) len() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.entries)
}

// cleanup purges entries not seen within maxAge. Because s.order is kept in
// strict lastSeen order (see the ipLimiterStore doc comment), this only needs
// to walk from the back until it finds a non-stale entry — the moment one
// entry isn't stale, nothing further toward the front can be either.
func (s *ipLimiterStore) cleanup(maxAge time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()
	cutoff := time.Now().Add(-maxAge)
	for {
		back := s.order.Back()
		if back == nil {
			return
		}
		entry := back.Value.(*ipLimiterEntry)
		if !entry.lastSeen.Before(cutoff) {
			return
		}
		delete(s.entries, entry.key)
		s.order.Remove(back)
	}
}

// memoryBackend adapts the three per-tier ipLimiterStore instances to RateLimitBackend. It never
// modifies ipLimiterStore itself (that type has three dedicated internal tests and a documented,
// correct evict-before-insert invariant that this task must not touch) — it only wraps it.
//
// ⚠ Retry-After is always window, exactly the fixed WindowDuration, never the actual remaining
// wait computed by golang.org/x/time/rate. That is deliberate: it is what the pre-#1531 limiter
// always returned, and TestIPRateLimiter_429HasRetryAfterHeader (and every other existing
// Retry-After assertion) must keep passing unmodified. See fallbackLimiter's doc comment for the
// resulting Retry-After inconsistency against the Redis backend, and why it is left as a
// documented follow-up rather than unified here.
type memoryBackend struct {
	regular, auth, sentinel, exempt *ipLimiterStore
	window                          time.Duration
}

// Name identifies this backend for logs/metrics.
func (m *memoryBackend) Name() string { return "memory" }

// storeFor returns the per-tier store backing tier.
func (m *memoryBackend) storeFor(tier domain.RateLimitTier) *ipLimiterStore {
	switch tier {
	case domain.RateLimitTierAuth:
		return m.auth
	case domain.RateLimitTierSentinel:
		return m.sentinel
	default:
		return m.regular
	}
}

// Allow never returns an error: golang.org/x/time/rate.Limiter.Allow cannot fail, which is exactly
// what makes this a safe unconditional fallback for fallbackLimiter.
func (m *memoryBackend) Allow(_ context.Context, tier domain.RateLimitTier, key string) (domain.RateLimitDecision, error) {
	limiter := m.storeFor(tier).get(key)
	if limiter.Allow() {
		return domain.RateLimitDecision{Allowed: true}, nil
	}
	return domain.RateLimitDecision{Allowed: false, RetryAfter: m.window}, nil
}

// AllowExempt applies the dedicated exempt-path store (m.exempt), built from
// RateLimitConfig.ExemptPathLimit — deliberately NOT storeFor's tier-based routing, and therefore
// never subject to whatever division IPLimit underwent for the regular tier's fallback budget.
// See Issue #1531 L3 and RateLimitConfig.ExemptPathLimit's doc comment.
func (m *memoryBackend) AllowExempt(key string) domain.RateLimitDecision {
	if m.exempt.get(key).Allow() {
		return domain.RateLimitDecision{Allowed: true}
	}
	return domain.RateLimitDecision{Allowed: false, RetryAfter: m.window}
}

// tierForRequest picks the domain.RateLimitTier for a request, given its path and already-resolved
// client identity key. Sentinel takes precedence over the path-based auth/regular split: the
// sentinel bucket exists specifically because identity resolution failed (see
// identitySentinelFallback), which is orthogonal to which path was hit, and the fleet-wide
// sentinel bucket is deliberately coarse regardless of path.
//
// ⚠ This function is the ONLY thing that selects a domain.RateLimitTier for a live request, and it
// can only ever return one of the three named constants (RateLimitTierRegular/Auth/Sentinel) by
// construction — never an out-of-range value. That matters because
// internal/ratelimitstore.RedisBackend.limitFor silently maps any out-of-range tier to
// RegularLimit via a default branch (Task 4 review carry-forward); this function is what prevents
// an unmapped tier from ever reaching it. See TestTierForRequest_NeverProducesAnUnmappedTier.
func tierForRequest(path, key string) domain.RateLimitTier {
	if key == identitySentinelFallback {
		return domain.RateLimitTierSentinel
	}
	if isAuthEndpoint(path) {
		return domain.RateLimitTierAuth
	}
	return domain.RateLimitTierRegular
}

// IPRateLimiter returns an Echo-level middleware that enforces per-identity rate limiting.
//
// Two backends compose here:
//   - memoryBackend: an in-memory token bucket (golang.org/x/time/rate), always present.
//   - cfg.Backend: OPTIONAL primary (in production, the Redis-backed fleet-wide GCRA limiter).
//     nil means memory-only — today's pre-#1531 behaviour, unchanged.
//
// When cfg.Backend is set, it is wrapped in a fallbackLimiter so a primary outage degrades to the
// per-machine memory backend rather than failing the request path open or closed. See
// fallbackLimiter's doc comment for the full security and observability rationale.
//
// ⚠ Exempt paths (isExemptPath — /health today) ALWAYS use the memory backend directly, bypassing
// the fallbackLimiter (and therefore cfg.Backend) entirely. Coupling /health — Fly's liveness
// probe — to Redis availability is exactly what "never fail closed" forbids, and the probe path is
// low-volume enough that fleet-wide accounting buys nothing. See
// TestIPRateLimiter_ExemptPathsNeverTouchThePrimaryBackend.
//
// Three separate per-tier stores are maintained (see memoryBackend/tierForRequest): regular, auth
// (stricter limit for /webhooks/*, /api/v1/qa/auth/*), and sentinel (the fleet-wide bucket used
// when client identity could not be resolved — see identitySentinelFallback). All three are
// hard-capped at RateLimitConfig.MaxEntries with stale-first eviction (see ipLimiterStore.get),
// bounding worst-case memory regardless of how many distinct identities are seen.
//
// Per-request identity is source-aware — see clientIdentity — and depends on whether CloudflareOnly
// ran ahead of this middleware in the chain and what it found. If CloudflareOnly is not registered
// ahead of IPRateLimiter, every request is treated as ungated (RemoteAddr only).
//
// A panic in the rate-limit DECISION logic (identity resolution, tier selection, or an injected
// backend) is recovered, logged, and treated as "allow" rather than crashing the connection:
// IPRateLimiter sits ahead of PanicRecovery and sentryecho in the middleware chain (see
// cmd/server/main.go), so a panic here would otherwise be caught by nobody. That recover is
// deliberately scoped to ONLY the decision logic (see ipRateLimiterDecide) and does NOT wrap
// next(ec): if it did, and chain ordering ever regressed so a downstream panic unwound into this
// recover instead of being caught by PanicRecovery first, next(ec) would be invoked a SECOND time
// by this recover on top of whatever partial execution the first (panicking) call already did —
// duplicate handler execution, double writes, double side effects, on a financial application.
// Scoping the recover to end before next(ec) is called at all makes that failure mode impossible
// regardless of chain ordering.
// cleanupGoroutineStopped, when non-nil, is called immediately before IPRateLimiter's background
// cleanup goroutine returns after cfg.Done fires. Always nil in production. Wired only from tests,
// via SetCleanupGoroutineStoppedHookForTest in export_test.go (compiled for tests only) — see that
// function's doc comment for why this exists instead of counting goroutines.
var cleanupGoroutineStopped func()

func IPRateLimiter(cfg RateLimitConfig) echo.MiddlewareFunc {
	c := cfg.withDefaults()
	logger := cfg.Logger
	if logger == nil {
		logger = slog.Default()
	}

	regularStore := newIPLimiterStore(c.IPLimit, c.WindowDuration, c.MaxEntries)
	authStore := newIPLimiterStore(c.AuthEndpointLimit, c.WindowDuration, c.MaxEntries)
	sentinelStore := newIPLimiterStore(c.IPLimit, c.WindowDuration, c.MaxEntries)
	// exemptStore backs ONLY isExemptPath paths (/health), built from ExemptPathLimit rather than
	// IPLimit — see RateLimitConfig.ExemptPathLimit's doc comment for why exempt paths must not
	// share the regular tier's (possibly fleet-divided) capacity. Issue #1531 L3.
	exemptStore := newIPLimiterStore(c.ExemptPathLimit, c.WindowDuration, c.MaxEntries)

	// One pre-check store PER TIER — see RateLimitConfig.PrecheckLimit for why a single shared
	// bucket broke the invariant three ways. Each is its own store rather than a reuse of the tier
	// stores above, whose budgets are fleet-divided and would reject inside the authoritative
	// limit. A tier left at <=0 stays nil, i.e. that tier's pre-check is disabled.
	precheck := precheckStores{
		regular:  newPrecheckStore(c.PrecheckLimit, c.WindowDuration, c.MaxEntries),
		auth:     newPrecheckStore(c.PrecheckAuthLimit, c.WindowDuration, c.MaxEntries),
		sentinel: newPrecheckStore(c.PrecheckSentinelLimit, c.WindowDuration, c.MaxEntries),
	}

	memBackend := &memoryBackend{regular: regularStore, auth: authStore, sentinel: sentinelStore, exempt: exemptStore, window: c.WindowDuration}
	effective := newFallbackLimiter(cfg.Backend, memBackend, c.PrimaryTimeout, logger, cfg.Done)

	// Background cleanup of stale entries. Stops when cfg.Done is closed; a nil cfg.Done blocks
	// forever in the select, which is identical to today's "never stops" behaviour.
	go func() {
		ticker := time.NewTicker(c.CleanupInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				regularStore.cleanup(c.CleanupInterval * 2)
				authStore.cleanup(c.CleanupInterval * 2)
				sentinelStore.cleanup(c.CleanupInterval * 2)
				exemptStore.cleanup(c.CleanupInterval * 2)
				precheck.cleanup(c.CleanupInterval * 2)
			case <-cfg.Done:
				// cleanupGoroutineStopped is nil in production (see its doc comment) — this is a
				// no-op there. It exists solely so a test can observe THIS specific goroutine's
				// exit via a channel it owns, instead of the process-global runtime.NumGoroutine(),
				// which is shared with every other test in the binary and flaky under -race/parallel.
				if cleanupGoroutineStopped != nil {
					cleanupGoroutineStopped()
				}
				return
			}
		}
	}()

	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(ec echo.Context) error {
			allowed, retryAfterSecs := ipRateLimiterDecide(ec, effective, memBackend, precheck, c.WindowDuration, logger)
			if !allowed {
				ec.Response().Header().Set("Retry-After", strconv.Itoa(retryAfterSecs))
				return echo.NewHTTPError(http.StatusTooManyRequests,
					"rate limit exceeded, please retry after "+strconv.Itoa(retryAfterSecs)+" seconds")
			}
			return next(ec)
		}
	}
}

// ipRateLimiterDecide resolves client identity/tier and asks the appropriate backend whether this
// request may proceed. It returns whether the request is allowed and, if not, how many whole
// seconds the client should wait before retrying (always >= 1; meaningless when allowed is true).
//
// A panic anywhere in this function (identity resolution, tier selection, or an injected backend)
// is recovered HERE — scoped to this function only, never to next(ec) — logged, and treated as
// "allow" (fail open on an internal bug, the same availability bias used everywhere else in this
// file). See IPRateLimiter's doc comment for why the recover must not extend to next(ec).
// logRateLimitRejection emits the WARN log line for a 429 rejection, modelled on CloudflareOnly's
// slog.WarnContext branches (cloudflare.go:144-217) so the two gates read alike: same level, same
// "path" field name. It additionally carries "tier" (the same value already used for
// observability.RateLimitRejectionsTotal's label, so logs and metrics join up on the same key) and
// "client_identity" (the clientIdentity() bucket key — an IP address or identitySentinelFallback,
// never a header value that could carry a secret). Unlike CloudflareOnly, this gate has no secret
// to compare in the first place, so there is nothing here for a future edit to accidentally leak —
// keep it that way; never add a raw header value to this call.
func logRateLimitRejection(ec echo.Context, logger *slog.Logger, path string, tier domain.RateLimitTier, identity string, retryAfterSecs int) {
	logger.WarnContext(ec.Request().Context(), "rate-limit: blocked — request over limit",
		"path", path,
		"tier", tier.String(),
		"client_identity", identity,
		"retry_after_secs", retryAfterSecs)
}

func ipRateLimiterDecide(ec echo.Context, effective *fallbackLimiter, memBackend *memoryBackend, precheck precheckStores, window time.Duration, logger *slog.Logger) (allowed bool, retryAfterSecs int) {
	defer func() {
		if r := recover(); r != nil {
			logger.Error("rate limiter panicked; allowing request through",
				"panic", r, "path", ec.Request().URL.Path)
			allowed = true
			retryAfterSecs = 0
		}
	}()

	path := ec.Request().URL.Path
	key := clientIdentity(ec)

	if isExemptPath(path) {
		// Exempt paths (/health) get their own dedicated, never-fleet-divided store — see
		// memoryBackend.AllowExempt and RateLimitConfig.ExemptPathLimit's doc comments (Issue
		// #1531 L3). This bypasses tierForRequest/RateLimitBackend entirely, not just the
		// backend-selection override the pre-fix code did: /health was never Auth-tier traffic,
		// and giving it its own store makes that structural rather than incidental.
		decision := memBackend.AllowExempt(key)
		if !decision.Allowed {
			exemptTier := domain.RateLimitTierRegular
			observability.RateLimitRejectionsTotal.WithLabelValues(exemptTier.String()).Inc()
			secs := int(decision.RetryAfter.Seconds())
			if secs < 1 {
				secs = 1
			}
			logRateLimitRejection(ec, logger, path, exemptTier, key, secs)
			return false, secs
		}
		return true, 0
	}

	tier := tierForRequest(path, key)

	// Cost circuit breaker. Consulted BEFORE the backend so a flood from one identity stops
	// costing billable backend commands once its bucket is empty. Placed AFTER the exempt-path
	// branch above, which already bypasses the backend entirely, so /health is untouched.
	//
	// Reaching here is a genuine rate-limit rejection and is reported as one — same metric, same
	// log line, same Retry-After — because PrecheckLimit is set at or above the authoritative
	// limit, so anything it rejects is traffic the backend would have rejected too.
	//
	// ⚠ Keyed on domain.AggregateIPv6Identity(key), the SAME canonical identity Backend buckets
	// by, not the raw key. With the raw key an attacker rotating /128s inside one routed /64 —
	// precisely the attacker aggregateIPv6Identity was written about — minted a fresh full bucket
	// per address, so the cost bound did not apply to the identity Backend actually enforces.
	// Reproduced at 200 addresses; pinned by
	// TestIPRateLimiter_PrecheckBucketsIPv6ByTheSameIdentityRedisDoes.
	if store := precheck.forTier(tier); store != nil && !store.get(domain.AggregateIPv6Identity(key)).Allow() {
		observability.RateLimitRejectionsTotal.WithLabelValues(tier.String()).Inc()
		observability.RateLimitPrecheckRejectionsTotal.WithLabelValues(tier.String()).Inc()
		secs := int(window.Seconds())
		if secs < 1 {
			secs = 1
		}
		logRateLimitRejection(ec, logger, path, tier, key, secs)
		return false, secs
	}

	decision, dErr := effective.Allow(ec.Request().Context(), tier, key)
	if dErr != nil {
		// Neither memoryBackend nor fallbackLimiter (which always resolves through memoryBackend
		// on primary failure) can return an error in practice. Treat an unexpected error the same
		// as "allow" — availability over enforcement, same bias as everywhere else in this file.
		return true, 0
	}

	if !decision.Allowed {
		observability.RateLimitRejectionsTotal.WithLabelValues(tier.String()).Inc()
		secs := int(decision.RetryAfter.Seconds())
		if secs < 1 {
			secs = 1
		}
		logRateLimitRejection(ec, logger, path, tier, key, secs)
		return false, secs
	}

	return true, 0
}
