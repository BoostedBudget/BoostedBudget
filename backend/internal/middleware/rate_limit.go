package middleware

import (
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/labstack/echo/v4"
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
	return cfg
}

// isAuthEndpoint returns true for paths that should use the stricter rate limit.
func isAuthEndpoint(path string) bool {
	return strings.HasPrefix(path, "/webhooks/") ||
		strings.HasPrefix(path, "/api/v1/qa/auth/")
}

// clientIP extracts the real client IP, respecting X-Forwarded-For / X-Real-IP
// headers set by Fly.io and other reverse proxies.
func clientIP(r *http.Request) string {
	// X-Forwarded-For is a comma-separated list; take the first (original client)
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		parts := strings.SplitN(xff, ",", 2)
		if ip := strings.TrimSpace(parts[0]); ip != "" {
			return ip
		}
	}
	if xri := r.Header.Get("X-Real-IP"); xri != "" {
		return strings.TrimSpace(xri)
	}
	// Fall back to RemoteAddr, stripping port
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// ipLimiterEntry holds a rate limiter and the last time it was accessed.
type ipLimiterEntry struct {
	limiter  *rate.Limiter
	lastSeen time.Time
}

// ipLimiterStore manages per-IP rate limiters with automatic cleanup of stale entries.
type ipLimiterStore struct {
	mu      sync.Mutex
	entries map[string]*ipLimiterEntry
	limit   rate.Limit
	burst   int
}

func newIPLimiterStore(requestsPerWindow int, window time.Duration) *ipLimiterStore {
	rps := rate.Limit(float64(requestsPerWindow) / window.Seconds())
	return &ipLimiterStore{
		entries: make(map[string]*ipLimiterEntry),
		limit:   rps,
		burst:   requestsPerWindow, // burst equals the window limit
	}
}

func (s *ipLimiterStore) get(ip string) *rate.Limiter {
	s.mu.Lock()
	defer s.mu.Unlock()

	entry, ok := s.entries[ip]
	if !ok {
		entry = &ipLimiterEntry{
			limiter: rate.NewLimiter(s.limit, s.burst),
		}
		s.entries[ip] = entry
	}
	entry.lastSeen = time.Now()
	return entry.limiter
}

func (s *ipLimiterStore) cleanup(maxAge time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()
	cutoff := time.Now().Add(-maxAge)
	for ip, entry := range s.entries {
		if entry.lastSeen.Before(cutoff) {
			delete(s.entries, ip)
		}
	}
}

// IPRateLimiter returns an Echo-level middleware that enforces per-IP rate limiting.
// It uses an in-memory token bucket (golang.org/x/time/rate).
// Appropriate for single-region MVP; can be upgraded to Redis for multi-instance.
//
// Two separate stores are maintained:
//   - Regular store: used for all standard API paths
//   - Auth store: stricter limit for sensitive paths (/webhooks/*, /api/v1/qa/auth/*)
func IPRateLimiter(cfg RateLimitConfig) echo.MiddlewareFunc {
	c := cfg.withDefaults()

	regularStore := newIPLimiterStore(c.IPLimit, c.WindowDuration)
	authStore := newIPLimiterStore(c.AuthEndpointLimit, c.WindowDuration)

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
			ip := clientIP(ec.Request())

			store := regularStore
			if isAuthEndpoint(ec.Request().URL.Path) {
				store = authStore
			}

			limiter := store.get(ip)
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
