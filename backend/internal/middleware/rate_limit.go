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

// singleValuedHeaderAddr returns the canonicalized address carried by a header
// that is only trustworthy when it appears exactly once and parses as a valid
// IP. Zero occurrences, more than one occurrence (possible header-injection /
// smuggling), and unparseable values all report ok=false rather than handing
// back an opaque string to be used as a bucket key.
func singleValuedHeaderAddr(r *http.Request, canonicalHeader string) (netip.Addr, bool) {
	values := r.Header[canonicalHeader]
	if len(values) != 1 {
		return netip.Addr{}, false
	}
	addr, err := netip.ParseAddr(strings.TrimSpace(values[0]))
	if err != nil {
		return netip.Addr{}, false
	}
	return addr.Unmap(), true
}

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
//     request (e.g. /health, /metrics — see cloudflare.go's exemptedPaths), so
//     no header on it has been verified. Fly-Client-Ip is used when present:
//     CloudflareOnly's own Layer 1 check already depends on Fly-Client-Ip being
//     set by Fly's edge from the real TCP connection rather than by the HTTP
//     client (cloudflare.go:98-113) — this is an assumption inherited from
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
		if addr, ok := singleValuedHeaderAddr(r, "Cf-Connecting-Ip"); ok {
			return addr.String()
		}
		observability.RateLimitIdentitySentinelFallbackTotal.Inc()
		return identitySentinelFallback
	case GateStatusExemptPath:
		if addr, ok := singleValuedHeaderAddr(r, "Fly-Client-Ip"); ok {
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

	memBackend := &memoryBackend{regular: regularStore, auth: authStore, sentinel: sentinelStore, exempt: exemptStore, window: c.WindowDuration}
	effective := newFallbackLimiter(cfg.Backend, memBackend, defaultRedisLimiterTimeout, logger, cfg.Done)

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
			case <-cfg.Done:
				return
			}
		}
	}()

	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(ec echo.Context) error {
			allowed, retryAfterSecs := ipRateLimiterDecide(ec, effective, memBackend, logger)
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
func ipRateLimiterDecide(ec echo.Context, effective *fallbackLimiter, memBackend *memoryBackend, logger *slog.Logger) (allowed bool, retryAfterSecs int) {
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
			observability.RateLimitRejectionsTotal.WithLabelValues(domain.RateLimitTierRegular.String()).Inc()
			secs := int(decision.RetryAfter.Seconds())
			if secs < 1 {
				secs = 1
			}
			return false, secs
		}
		return true, 0
	}

	tier := tierForRequest(path, key)
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
		return false, secs
	}

	return true, 0
}
