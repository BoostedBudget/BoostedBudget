//go:build integration

package ratelimitstore_test

import (
	"context"
	"testing"
	"time"

	"golang.org/x/time/rate"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/ratelimitstore"
)

// TestRedisBackendIntegration_ConformsToXTimeRateOverASequence is the conformance test the brief
// asks for: the whole fallback design (Task 5) rests on the Redis-backed GCRA limiter and the
// existing in-memory golang.org/x/time/rate limiter being the SAME limiter — a change of state
// scope (fleet-wide vs. per-machine), not a change of behaviour. If they diverge, switching
// between primary and fallback becomes an observable behaviour change, not just an availability
// trade-off.
//
// This drives BOTH limiters through an IDENTICAL request sequence covering: the very first
// request, the full initial burst, the exact boundary request (burst+1, which must be the first
// denial), and the Retry-After/reservation delay at that boundary — and asserts the allow/deny
// DECISION at every step matches exactly. Sub-microsecond timing differences between a real Redis
// TIME call and Go's monotonic clock make exact float/duration equality across the two systems
// unreliable, so this asserts on the decision sequence (a much stronger, deterministic invariant)
// rather than weakening to "roughly similar" durations.
func TestRedisBackendIntegration_ConformsToXTimeRateOverASequence(t *testing.T) {
	const limit = 10
	window := 2 * time.Second

	client := newIntegrationClient(t, "limiter")
	redisBackend := ratelimitstore.NewRedisBackend(client, ratelimitstore.Options{RegularLimit: limit, Window: window})
	id := testIdentity(t)
	ctx := context.Background()

	// Mathematically equivalent construction per the package doc comment: burst == limit.
	memLimiter := rate.NewLimiter(rate.Every(window/time.Duration(limit)), limit)

	type step struct {
		name string
	}
	// Sequence: the very first request, the rest of the initial burst (2..limit), the exact
	// boundary request (limit+1, the first request that must be denied), and one more denial right
	// after it to confirm the deny persists rather than being a one-off fluke.
	steps := make([]step, 0, limit+2)
	steps = append(steps, step{"first"})
	for i := 2; i <= limit; i++ {
		steps = append(steps, step{"burst"})
	}
	steps = append(steps, step{"boundary (limit+1)"})
	steps = append(steps, step{"post-boundary"})

	for i, s := range steps {
		redisDecision, err := redisBackend.Allow(ctx, domain.RateLimitTierRegular, id)
		if err != nil {
			t.Fatalf("step %d (%s): RedisBackend.Allow: %v", i, s.name, err)
		}
		memAllowed := memLimiter.Allow()

		if redisDecision.Allowed != memAllowed {
			t.Fatalf("step %d (%s): decision mismatch — RedisBackend allowed=%v, x/time/rate allowed=%v", i, s.name, redisDecision.Allowed, memAllowed)
		}

		// At the exact boundary (the first denial), also sanity-check the Retry-After is
		// materially shorter than the full window — same GCRA property the plain integration
		// test pins, cross-checked here against the specific step where x/time/rate agrees it's
		// the first denial.
		if !redisDecision.Allowed {
			if redisDecision.RetryAfter <= 0 {
				t.Fatalf("step %d (%s): RetryAfter = %v, want > 0 on a denial", i, s.name, redisDecision.RetryAfter)
			}
			if redisDecision.RetryAfter >= window {
				t.Fatalf("step %d (%s): RetryAfter = %v, want well under the full window %v", i, s.name, redisDecision.RetryAfter, window)
			}
		}
	}
}
