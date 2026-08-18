package middleware

import (
	"context"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
)

// RateLimitBackend is the seam between IPRateLimiter and whatever actually decides "allowed or
// not" for a given (tier, key) pair — the in-memory per-process limiter today, and (Issue #1531)
// a Redis-backed fleet-wide GCRA limiter (internal/ratelimitstore.RedisBackend).
//
// Declared consumer-side, here, over domain types rather than in internal/ratelimitstore: that
// keeps this package free of any import of internal/ratelimitstore or go-redis in production code,
// so internal/middleware's existing hermetic, network-free test suite stays that way. A
// compile-time assertion that *ratelimitstore.RedisBackend satisfies this interface lives in a
// _test.go file (test files may import internal/ratelimitstore without creating a production
// import cycle — see internal/workers/mocks_test.go:17-23 for the same convention).
type RateLimitBackend interface {
	// Allow evaluates whether a request for tier/key may proceed right now.
	Allow(ctx context.Context, tier domain.RateLimitTier, key string) (domain.RateLimitDecision, error)
	// Name identifies this backend for logs and metric labels: "memory" or "redis".
	Name() string
}
