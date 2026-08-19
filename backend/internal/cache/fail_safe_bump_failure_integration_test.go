//go:build integration

// Package cache — Issue #1531 Task 11d: TestBumpFailure_BoundedByTTL.
//
// Generations.Bump is deliberately VOID: a bump-write failure never fails the caller's request,
// because the underlying data write it is invalidating-for already committed (generation.go's doc
// comment). That design choice is only defensible if the resulting staleness window has a hard
// ceiling — otherwise "never fails the request" would really mean "silently serves stale money
// forever". This test demonstrates the ceiling exists (decodeEnvelope's 2×TTL staleness check,
// codec.go) rather than merely asserting it in prose: it injects a real Bump failure against a
// real Redis, shows the stale value IS served immediately after (the honest, documented cost of a
// void bump), then advances the cache's own clock seam past 2×the namespace TTL and shows the
// SAME cache instance stops serving it — no restart, no manual intervention.
package cache_test

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/maxnussbaum/BoostedBudget/internal/cache"
	"github.com/maxnussbaum/BoostedBudget/internal/cache/cachetest"
	"github.com/maxnussbaum/BoostedBudget/internal/redisclient"
)

// failingSetClient wraps a real redisclient.Client and, once armed, fails Set calls for exactly
// one target key (the scope's generation key) — leaving every other Set (the data key writes
// GetOrLoad itself performs) untouched. This models a real, narrow Redis write failure on the
// generation key alone, which is the specific failure Generations.Bump's void-return contract is
// about.
type failingSetClient struct {
	redisclient.Client
	targetKey string
	armed     atomic.Bool
}

func (f *failingSetClient) Set(ctx context.Context, key, value string, ttl time.Duration) error {
	if f.armed.Load() && key == f.targetKey {
		return errors.New("injected: generation bump write failure")
	}
	return f.Client.Set(ctx, key, value, ttl)
}

// fakeClock is a mutable Options.Now seam: cache.Cache reads it for both the envelope-write
// timestamp and every subsequent envelope-age check, so advancing it models real elapsed time
// without an actual multi-minute sleep.
type fakeClock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *fakeClock) now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *fakeClock) advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = c.t.Add(d)
}

func TestBumpFailure_BoundedByTTL(t *testing.T) {
	container := cachetest.StartRedisContainer(t)
	ctx := context.Background()

	realClient, err := redisclient.New(redisclient.Options{URL: container.URL, Name: "cache-test-bumpfail"})
	require.NoError(t, err)
	t.Cleanup(func() { _ = realClient.Close() })

	scopeKind, scopeID := cache.ScopeUser, "bump-failure-test-user"
	genKey := cache.GenerationKey(cache.ScopeTag(scopeKind, scopeID))
	spy := &failingSetClient{Client: realClient, targetKey: genKey}

	clock := &fakeClock{t: time.Now()}
	c := cache.NewCache(cache.Options{Client: spy, Now: clock.now})
	gens := c.Generations()

	k := cache.Key{
		Namespace: cache.NamespaceAnalytics, // TTL = 120s (namespace.go); stale threshold = 240s
		ScopeKind: scopeKind,
		ScopeID:   scopeID,
		Method:    "TestMethod",
		ArgHash:   "argsBumpFailure",
	}
	const ttl = 120 * time.Second

	// Warm the cache with value A.
	gotA, hit, err := c.GetOrLoad(ctx, k, func(ctx context.Context) ([]byte, error) {
		return []byte(`{"value":"A"}`), nil
	})
	require.NoError(t, err)
	require.False(t, hit)
	require.JSONEq(t, `{"value":"A"}`, string(gotA))

	before := bumpFailuresTotal(t, string(scopeKind))

	// Simulate the mutation-plus-bump sequence a real invalidation hook runs, except the bump's
	// Redis write fails.
	spy.armed.Store(true)
	gens.Bump(ctx, scopeKind, scopeID, "test")
	spy.armed.Store(false) // subsequent generation reads/mints must succeed normally

	after := bumpFailuresTotal(t, string(scopeKind))
	assert.Equal(t, before+1, after, "a failed bump must be recorded on cache_bump_failures_total (the alerting hook this design relies on)")

	// Immediately after the failed bump: the generation token never rotated, so the SAME data key
	// is still current, and — this is the honest cost of a void bump, not a bug — the cached read
	// serves the now-stale value A rather than the fresh value B the caller's loader would produce.
	gotStale, hit, err := c.GetOrLoad(ctx, k, func(ctx context.Context) ([]byte, error) {
		return []byte(`{"value":"B"}`), nil
	})
	require.NoError(t, err)
	assert.True(t, hit, "sanity: with the bump having failed, this must still be a cache hit against the pre-mutation entry")
	assert.JSONEq(t, `{"value":"A"}`, string(gotStale), "a failed bump leaves the OLD value being served — this is the documented cost that the TTL bound below must cap")

	// Advance the cache's clock past 2xTTL (240s) — the exact threshold codec.go's decodeEnvelope
	// enforces via ErrStaleEnvelope, independent of whether the generation token ever rotated. This
	// is the real code path (not a synthetic proxy for it): the same Options.Now seam production
	// wiring would use time.Now for is what decodeEnvelope calls to compute the entry's age.
	clock.advance(2*ttl + time.Second)

	gotFresh, hit, err := c.GetOrLoad(ctx, k, func(ctx context.Context) ([]byte, error) {
		return []byte(`{"value":"B"}`), nil
	})
	require.NoError(t, err)
	assert.False(t, hit, "past 2xTTL the stale envelope must be treated as a miss even though the generation token never rotated")
	assert.JSONEq(t, `{"value":"B"}`, string(gotFresh), "staleness from a failed bump must be bounded: once the namespace's own TTL window has passed, the CURRENT value must be served")
}

// bumpFailuresTotal reads cache_bump_failures_total{scope_kind=<kind>} from the process's default
// Prometheus registry. It cannot reference the package-internal cacheBumpFailuresTotal collector
// directly (this file is package cache_test, deliberately kept external so it exercises the same
// public Cache/Generations surface a real caller would use), so it gathers by metric+label name
// instead — the same mechanism a real scrape would use.
func bumpFailuresTotal(t *testing.T, scopeKind string) float64 {
	t.Helper()
	mfs, err := prometheus.DefaultGatherer.Gather()
	require.NoError(t, err)
	for _, mf := range mfs {
		if mf.GetName() != "cache_bump_failures_total" {
			continue
		}
		for _, m := range mf.GetMetric() {
			if labelValue(m, "scope_kind") == scopeKind {
				return m.GetCounter().GetValue()
			}
		}
	}
	return 0
}

func labelValue(m *dto.Metric, name string) string {
	for _, lp := range m.GetLabel() {
		if lp.GetName() == name {
			return lp.GetValue()
		}
	}
	return ""
}
