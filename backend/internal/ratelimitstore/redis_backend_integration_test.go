//go:build integration

package ratelimitstore_test

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	redis "github.com/redis/go-redis/v9"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/ratelimitstore"
	"github.com/maxnussbaum/BoostedBudget/internal/redisclient"
)

// redisTestURL returns the REDIS_TEST_URL env var, skipping the test when unset. Never
// REDIS_URL — that name is reserved for the application's own configuration and must never be
// silently repurposed by a test.
func redisTestURL(t *testing.T) string {
	t.Helper()
	url := os.Getenv("REDIS_TEST_URL")
	if url == "" {
		t.Skip("REDIS_TEST_URL not set, skipping integration test")
	}
	return url
}

// testIdentity scopes every key this test suite writes with the test name plus a random suffix,
// so concurrent test runs (and reruns) against a shared Redis never collide, and so nothing here
// ever needs FLUSHDB (which is banned: a shared instance may hold other suites' data).
func testIdentity(t *testing.T) string {
	t.Helper()
	buf := make([]byte, 8)
	if _, err := rand.Read(buf); err != nil {
		t.Fatalf("rand.Read: %v", err)
	}
	return fmt.Sprintf("%s-%s", t.Name(), hex.EncodeToString(buf))
}

func newIntegrationClient(t *testing.T, name string) redisclient.Client {
	t.Helper()
	url := redisTestURL(t)
	c, err := redisclient.New(redisclient.Options{URL: url, Name: name})
	if err != nil {
		t.Fatalf("redisclient.New: %v", err)
	}
	t.Cleanup(func() { _ = c.Close() })
	return c
}

// rawRedisClient opens a direct go-redis client against REDIS_TEST_URL for assertions
// (PTTL/EXISTS) that redisclient.Client's interface deliberately does not expose.
func rawRedisClient(t *testing.T) *redis.Client {
	t.Helper()
	url := redisTestURL(t)
	opts, err := redis.ParseURL(url)
	if err != nil {
		t.Fatalf("redis.ParseURL: %v", err)
	}
	rdb := redis.NewClient(opts)
	t.Cleanup(func() { _ = rdb.Close() })
	return rdb
}

func TestRedisBackendIntegration_AllowsExactlyLimitRequestsThenDenies(t *testing.T) {
	client := newIntegrationClient(t, "limiter")
	b := ratelimitstore.NewRedisBackend(client, ratelimitstore.Options{RegularLimit: 5, Window: time.Minute})
	id := testIdentity(t)
	ctx := context.Background()

	allowed := 0
	for i := 0; i < 5; i++ {
		d, err := b.Allow(ctx, domain.RateLimitTierRegular, id)
		if err != nil {
			t.Fatalf("Allow[%d]: %v", i, err)
		}
		if d.Allowed {
			allowed++
		}
	}
	if allowed != 5 {
		t.Fatalf("expected all 5 initial requests allowed, got %d", allowed)
	}

	d, err := b.Allow(ctx, domain.RateLimitTierRegular, id)
	if err != nil {
		t.Fatalf("Allow[6th]: %v", err)
	}
	if d.Allowed {
		t.Fatal("expected the 6th request to be denied")
	}
}

func TestRedisBackendIntegration_SeparateIdentitiesHaveSeparateBudgets(t *testing.T) {
	client := newIntegrationClient(t, "limiter")
	b := ratelimitstore.NewRedisBackend(client, ratelimitstore.Options{RegularLimit: 2, Window: time.Minute})
	idA := testIdentity(t) + "-a"
	idB := testIdentity(t) + "-b"
	ctx := context.Background()

	for i := 0; i < 2; i++ {
		if d, err := b.Allow(ctx, domain.RateLimitTierRegular, idA); err != nil || !d.Allowed {
			t.Fatalf("identity A request %d: allowed=%v err=%v", i, d.Allowed, err)
		}
	}
	// A is now exhausted; B must be untouched.
	if d, err := b.Allow(ctx, domain.RateLimitTierRegular, idA); err != nil {
		t.Fatalf("identity A 3rd request: %v", err)
	} else if d.Allowed {
		t.Fatal("expected identity A to be exhausted")
	}
	if d, err := b.Allow(ctx, domain.RateLimitTierRegular, idB); err != nil {
		t.Fatalf("identity B 1st request: %v", err)
	} else if !d.Allowed {
		t.Fatal("expected identity B's first request to be allowed — budgets must not be shared across identities")
	}
}

func TestRedisBackendIntegration_RegularAndAuthTiersDoNotShareABudget(t *testing.T) {
	client := newIntegrationClient(t, "limiter")
	b := ratelimitstore.NewRedisBackend(client, ratelimitstore.Options{RegularLimit: 2, AuthLimit: 2, Window: time.Minute})
	id := testIdentity(t)
	ctx := context.Background()

	for i := 0; i < 2; i++ {
		if d, err := b.Allow(ctx, domain.RateLimitTierRegular, id); err != nil || !d.Allowed {
			t.Fatalf("regular request %d: allowed=%v err=%v", i, d.Allowed, err)
		}
	}
	// Regular is now exhausted for this identity; auth tier (same identity) must be untouched.
	if d, err := b.Allow(ctx, domain.RateLimitTierRegular, id); err != nil {
		t.Fatalf("regular 3rd request: %v", err)
	} else if d.Allowed {
		t.Fatal("expected regular tier to be exhausted")
	}
	if d, err := b.Allow(ctx, domain.RateLimitTierAuth, id); err != nil {
		t.Fatalf("auth 1st request: %v", err)
	} else if !d.Allowed {
		t.Fatal("expected auth tier's first request (same identity) to be allowed — tiers must not share a budget")
	}
}

// TestRedisBackendIntegration_TwoIndependentBackendsShareOneBudget is the headline test for
// Issue #1531: two separately constructed backends (two clients, simulating two machines) against
// one Redis must share ONE budget per identity, not double it.
//
// This test is deliberately written to discriminate against a broken implementation that keeps
// independent state: a second backend is also constructed pointed at a DIFFERENT key prefix
// (simulated by using a different identity), and that variant is asserted to allow 2x the limit —
// proving the test setup here really would fail if state were independent, rather than passing
// vacuously regardless of sharing.
func TestRedisBackendIntegration_TwoIndependentBackendsShareOneBudget(t *testing.T) {
	clientA := newIntegrationClient(t, "limiter-a")
	clientB := newIntegrationClient(t, "limiter-b")
	backendA := ratelimitstore.NewRedisBackend(clientA, ratelimitstore.Options{RegularLimit: 10, Window: time.Minute})
	backendB := ratelimitstore.NewRedisBackend(clientB, ratelimitstore.Options{RegularLimit: 10, Window: time.Minute})
	ctx := context.Background()

	// --- Discrimination proof: if the two backends were pointed at DIFFERENT identities (as if
	// keyed by a different prefix — the shape a genuinely independent-state bug would produce),
	// the pair really would allow 2x the limit. This proves the shared-budget assertion below is
	// not vacuous.
	discriminationID := testIdentity(t) + "-discrimination"
	discriminationOtherID := discriminationID + "-different-prefix"
	discAllowed := 0
	for i := 0; i < 10; i++ {
		if d, err := backendA.Allow(ctx, domain.RateLimitTierRegular, discriminationID); err != nil {
			t.Fatalf("discrimination backendA: %v", err)
		} else if d.Allowed {
			discAllowed++
		}
	}
	for i := 0; i < 10; i++ {
		if d, err := backendB.Allow(ctx, domain.RateLimitTierRegular, discriminationOtherID); err != nil {
			t.Fatalf("discrimination backendB: %v", err)
		} else if d.Allowed {
			discAllowed++
		}
	}
	if discAllowed != 20 {
		t.Fatalf("discrimination setup sanity check failed: expected 20 allowed across two DIFFERENT identities (10 each), got %d — the test harness itself is broken", discAllowed)
	}

	// --- The real assertion: SAME identity, two independently constructed backends. Total allowed
	// across both must equal the limit, not 2x the limit.
	id := testIdentity(t)
	allowed := 0
	for i := 0; i < 10; i++ {
		d, err := backendA.Allow(ctx, domain.RateLimitTierRegular, id)
		if err != nil {
			t.Fatalf("backendA request %d: %v", i, err)
		}
		if d.Allowed {
			allowed++
		}
	}
	for i := 0; i < 10; i++ {
		d, err := backendB.Allow(ctx, domain.RateLimitTierRegular, id)
		if err != nil {
			t.Fatalf("backendB request %d: %v", i, err)
		}
		if d.Allowed {
			allowed++
		}
	}

	if allowed != 10 {
		t.Fatalf("two independent backends against one Redis allowed %d requests for one identity, want exactly 10 (the limit) — backends must share one budget, not keep independent state", allowed)
	}
}

// TestRedisBackendIntegration_ConcurrentCallersNeverExceedTheLimit is the atomicity proof: 200
// goroutines racing against a limit of 50 must produce exactly 50 allows, proving the script's
// single round-trip GET+SET is race-free under real concurrency (run with -race).
func TestRedisBackendIntegration_ConcurrentCallersNeverExceedTheLimit(t *testing.T) {
	client := newIntegrationClient(t, "limiter")
	b := ratelimitstore.NewRedisBackend(client, ratelimitstore.Options{RegularLimit: 50, Window: time.Minute})
	id := testIdentity(t)
	ctx := context.Background()

	const goroutines = 200
	var allowedCount int64
	var wg sync.WaitGroup
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			d, err := b.Allow(ctx, domain.RateLimitTierRegular, id)
			if err != nil {
				t.Errorf("Allow: %v", err)
				return
			}
			if d.Allowed {
				atomic.AddInt64(&allowedCount, 1)
			}
		}()
	}
	wg.Wait()

	if got := atomic.LoadInt64(&allowedCount); got != 50 {
		t.Fatalf("200 concurrent callers against limit 50 allowed %d requests, want exactly 50", got)
	}
}

func TestRedisBackendIntegration_EveryWriteLeavesATTLOnTheKey(t *testing.T) {
	client := newIntegrationClient(t, "limiter")
	rdb := rawRedisClient(t)
	b := ratelimitstore.NewRedisBackend(client, ratelimitstore.Options{RegularLimit: 5, Window: time.Minute})
	id := testIdentity(t)
	ctx := context.Background()
	key := fmt.Sprintf("rl:v2:regular:%s", id)

	// Create path: first Allow call for this identity.
	if d, err := b.Allow(ctx, domain.RateLimitTierRegular, id); err != nil || !d.Allowed {
		t.Fatalf("first Allow: allowed=%v err=%v", d.Allowed, err)
	}
	pttl, err := rdb.PTTL(ctx, key).Result()
	if err != nil {
		t.Fatalf("PTTL after create: %v", err)
	}
	if pttl <= 0 {
		t.Fatalf("PTTL after create path = %v, want > 0", pttl)
	}

	// Update path: a subsequent Allow call for the same identity.
	if d, err := b.Allow(ctx, domain.RateLimitTierRegular, id); err != nil || !d.Allowed {
		t.Fatalf("second Allow: allowed=%v err=%v", d.Allowed, err)
	}
	pttl2, err := rdb.PTTL(ctx, key).Result()
	if err != nil {
		t.Fatalf("PTTL after update: %v", err)
	}
	if pttl2 <= 0 {
		t.Fatalf("PTTL after update path = %v, want > 0", pttl2)
	}
}

func TestRedisBackendIntegration_KeyDisappearsAfterTheBucketDrains(t *testing.T) {
	client := newIntegrationClient(t, "limiter")
	rdb := rawRedisClient(t)
	// A short window so the test doesn't have to sleep long for the bucket to fully drain.
	window := 2 * time.Second
	b := ratelimitstore.NewRedisBackend(client, ratelimitstore.Options{RegularLimit: 2, Window: window})
	id := testIdentity(t)
	ctx := context.Background()
	key := fmt.Sprintf("rl:v2:regular:%s", id)

	if d, err := b.Allow(ctx, domain.RateLimitTierRegular, id); err != nil || !d.Allowed {
		t.Fatalf("Allow: allowed=%v err=%v", d.Allowed, err)
	}

	// The script's TTL is derived from (new_tat - now) + a 1s buffer. With a 2s window and limit 2,
	// emission = 1s, so the key should fully expire well within ~3-4s.
	deadline := time.Now().Add(6 * time.Second)
	for time.Now().Before(deadline) {
		n, err := rdb.Exists(ctx, key).Result()
		if err != nil {
			t.Fatalf("EXISTS: %v", err)
		}
		if n == 0 {
			return
		}
		time.Sleep(200 * time.Millisecond)
	}
	t.Fatal("expected the bucket key to expire (EXISTS == 0) after draining, but it never did within the deadline")
}

func TestRedisBackendIntegration_RetryAfterIsShorterThanTheFullWindow(t *testing.T) {
	client := newIntegrationClient(t, "limiter")
	window := time.Minute
	limit := 5
	b := ratelimitstore.NewRedisBackend(client, ratelimitstore.Options{RegularLimit: limit, Window: window})
	id := testIdentity(t)
	ctx := context.Background()

	for i := 0; i < limit; i++ {
		if d, err := b.Allow(ctx, domain.RateLimitTierRegular, id); err != nil || !d.Allowed {
			t.Fatalf("Allow[%d]: allowed=%v err=%v", i, d.Allowed, err)
		}
	}

	d, err := b.Allow(ctx, domain.RateLimitTierRegular, id)
	if err != nil {
		t.Fatalf("Allow (denied): %v", err)
	}
	if d.Allowed {
		t.Fatal("expected this request to be denied")
	}
	// The GCRA property this guards: Retry-After should be roughly one emission interval
	// (window/limit = 12s here), NOT the full window (60s) as a fixed-window limiter would report.
	if d.RetryAfter >= window {
		t.Fatalf("RetryAfter = %v, want well under the full window (%v) — a value this large suggests a silent revert to fixed-window semantics", d.RetryAfter, window)
	}
}

func TestRedisBackendIntegration_BudgetRefillsSmoothlyRatherThanAtAWindowBoundary(t *testing.T) {
	client := newIntegrationClient(t, "limiter")
	window := 2 * time.Second
	limit := 2
	b := ratelimitstore.NewRedisBackend(client, ratelimitstore.Options{RegularLimit: limit, Window: window})
	id := testIdentity(t)
	ctx := context.Background()

	// Exhaust the budget.
	for i := 0; i < limit; i++ {
		if d, err := b.Allow(ctx, domain.RateLimitTierRegular, id); err != nil || !d.Allowed {
			t.Fatalf("Allow[%d]: allowed=%v err=%v", i, d.Allowed, err)
		}
	}
	if d, err := b.Allow(ctx, domain.RateLimitTierRegular, id); err != nil {
		t.Fatalf("Allow (should deny): %v", err)
	} else if d.Allowed {
		t.Fatal("expected the budget to be exhausted")
	}

	// emission interval = window/limit = 1s. Wait one emission interval; exactly one more request
	// should be allowed, refilling smoothly rather than waiting for the full window to elapse.
	emission := window / time.Duration(limit)
	time.Sleep(emission + 100*time.Millisecond)

	d, err := b.Allow(ctx, domain.RateLimitTierRegular, id)
	if err != nil {
		t.Fatalf("Allow (after one emission interval): %v", err)
	}
	if !d.Allowed {
		t.Fatal("expected exactly one more request to be allowed after waiting one emission interval")
	}
	// And immediately after that, the budget should be exhausted again.
	if d2, err := b.Allow(ctx, domain.RateLimitTierRegular, id); err != nil {
		t.Fatalf("Allow (immediately after refill): %v", err)
	} else if d2.Allowed {
		t.Fatal("expected the budget to be exhausted again immediately after the single refill")
	}
}

func TestRedisBackendIntegration_SurvivesScriptFlush(t *testing.T) {
	client := newIntegrationClient(t, "limiter")
	rdb := rawRedisClient(t)
	b := ratelimitstore.NewRedisBackend(client, ratelimitstore.Options{RegularLimit: 5, Window: time.Minute})
	id := testIdentity(t)
	ctx := context.Background()

	if d, err := b.Allow(ctx, domain.RateLimitTierRegular, id); err != nil || !d.Allowed {
		t.Fatalf("Allow before flush: allowed=%v err=%v", d.Allowed, err)
	}

	if err := rdb.ScriptFlush(ctx).Err(); err != nil {
		t.Fatalf("SCRIPT FLUSH: %v", err)
	}

	// go-redis's Script.Run tries EVALSHA first; after a flush that returns NOSCRIPT, and go-redis
	// falls back to EVAL automatically. This must succeed transparently.
	d, err := b.Allow(ctx, domain.RateLimitTierRegular, id)
	if err != nil {
		t.Fatalf("Allow after SCRIPT FLUSH: %v", err)
	}
	if !d.Allowed {
		t.Fatal("expected this request to still be allowed after SCRIPT FLUSH (budget not yet exhausted)")
	}
}
