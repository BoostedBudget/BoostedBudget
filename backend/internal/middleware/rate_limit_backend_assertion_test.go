package middleware_test

import (
	"github.com/maxnussbaum/BoostedBudget/internal/middleware"
	"github.com/maxnussbaum/BoostedBudget/internal/ratelimitstore"
)

// This file exists solely to prove *ratelimitstore.RedisBackend satisfies
// middleware.RateLimitBackend, WITHOUT internal/middleware importing internal/ratelimitstore (or
// go-redis) in production code — the whole point of declaring the interface consumer-side. Test
// files are allowed to import ratelimitstore because doing so here creates no production import
// cycle: internal/middleware's non-test code never references internal/ratelimitstore. Same
// convention as internal/workers/mocks_test.go:17-23.
var _ middleware.RateLimitBackend = (*ratelimitstore.RedisBackend)(nil)
