package middleware

import (
	"container/list"
	"net"
	"net/http"
	"net/netip"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/maxnussbaum/BoostedBudget/internal/observability"
	"golang.org/x/time/rate"
)

// RateLimitConfig configures the rate limiting middleware.
type RateLimitConfig struct {
	// IPLimit is the maximum number of requests per window per IP address.
	// Default: 100
	IPLimit int

	// AuthEndpointLimit is the stricter limit applied to sensitive paths like
	// /webhooks/* and /api/v1/qa/auth/*. Default: 10
	AuthEndpointLimit int

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
}

func (c *RateLimitConfig) withDefaults() RateLimitConfig {
	cfg := *c
	if cfg.IPLimit <= 0 {
		cfg.IPLimit = 100
	}
	if cfg.AuthEndpointLimit <= 0 {
		cfg.AuthEndpointLimit = 10
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

// IPRateLimiter returns an Echo-level middleware that enforces per-identity rate
// limiting. It uses an in-memory token bucket (golang.org/x/time/rate).
// Appropriate for single-region MVP; can be upgraded to Redis for multi-instance.
//
// Two separate stores are maintained:
//   - Regular store: used for all standard API paths
//   - Auth store: stricter limit for sensitive paths (/webhooks/*, /api/v1/qa/auth/*)
//
// Both stores are hard-capped at RateLimitConfig.MaxEntries with stale-first
// eviction (see ipLimiterStore.get), bounding worst-case memory regardless of
// how many distinct identities are seen.
//
// Per-request identity is source-aware — see clientIdentity — and depends on
// whether CloudflareOnly ran ahead of this middleware in the chain and what it
// found. If CloudflareOnly is not registered ahead of IPRateLimiter, every
// request is treated as ungated (RemoteAddr only).
func IPRateLimiter(cfg RateLimitConfig) echo.MiddlewareFunc {
	c := cfg.withDefaults()

	regularStore := newIPLimiterStore(c.IPLimit, c.WindowDuration, c.MaxEntries)
	authStore := newIPLimiterStore(c.AuthEndpointLimit, c.WindowDuration, c.MaxEntries)

	// Background cleanup of stale entries
	go func() {
		ticker := time.NewTicker(c.CleanupInterval)
		defer ticker.Stop()
		for range ticker.C {
			regularStore.cleanup(c.CleanupInterval * 2)
			authStore.cleanup(c.CleanupInterval * 2)
		}
	}()

	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(ec echo.Context) error {
			key := clientIdentity(ec)

			store := regularStore
			if isAuthEndpoint(ec.Request().URL.Path) {
				store = authStore
			}

			limiter := store.get(key)
			if !limiter.Allow() {
				retryAfter := strconv.Itoa(int(c.WindowDuration.Seconds()))
				ec.Response().Header().Set("Retry-After", retryAfter)
				return echo.NewHTTPError(http.StatusTooManyRequests,
					"rate limit exceeded, please retry after "+retryAfter+" seconds")
			}

			return next(ec)
		}
	}
}
