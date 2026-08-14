package observability

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeRedisPoolStatsSource is a RedisPoolStatsSource test double. It tracks how many times each of
// the interface's two methods is called, plus a set of methods that mimic go-redis COMMANDS
// (Get/Set/Ping) that are deliberately NOT part of RedisPoolStatsSource — they exist only so a test
// can assert they are never invoked, proving the collector never dispatches anything resembling a
// Redis command and reads local pool state exclusively via PoolStats()-shaped calls.
type fakeRedisPoolStatsSource struct {
	mu sync.Mutex

	name  string
	stats []RedisPoolStats // successive stats returned on each RedisPoolStats() call, last one repeats

	nameCalls    int32
	statsCalls   int32
	commandCalls int32 // Get/Set/Ping — must stay 0 forever; nothing in the collector can reach these
}

func newFakeRedisPoolStatsSource(name string, stats ...RedisPoolStats) *fakeRedisPoolStatsSource {
	return &fakeRedisPoolStatsSource{name: name, stats: stats}
}

func (f *fakeRedisPoolStatsSource) Name() string {
	atomic.AddInt32(&f.nameCalls, 1)
	return f.name
}

func (f *fakeRedisPoolStatsSource) RedisPoolStats() RedisPoolStats {
	atomic.AddInt32(&f.statsCalls, 1)
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.stats) == 0 {
		return RedisPoolStats{}
	}
	idx := int(f.statsCalls) - 1
	if idx >= len(f.stats) {
		idx = len(f.stats) - 1
	}
	return f.stats[idx]
}

// Get/Set/Ping are NOT part of RedisPoolStatsSource. They exist purely as tripwires: nothing in
// StartRedisMetricsCollector holds a concrete *fakeRedisPoolStatsSource (only the interface), so
// these can never be called — but the counters make that provable rather than merely structural.
func (f *fakeRedisPoolStatsSource) Get(string) { atomic.AddInt32(&f.commandCalls, 1) }
func (f *fakeRedisPoolStatsSource) Set(string) { atomic.AddInt32(&f.commandCalls, 1) }
func (f *fakeRedisPoolStatsSource) Ping()      { atomic.AddInt32(&f.commandCalls, 1) }

// countGaugeVecSamplesForLabel gathers vec into its OWN dedicated registry and counts only the
// samples whose "client" label equals labelValue. This sidesteps cross-test pollution: vec is a
// package-level singleton shared by every test in this file (as it must be, to be the real
// production metric), so a raw testutil.CollectAndCount(vec) would include every OTHER test's
// client labels too. Using a label value unique to the calling test, and filtering on it, gives
// each test an isolated view without mutating (or depending on) the global default registerer.
func countGaugeVecSamplesForLabel(t *testing.T, vec *prometheus.GaugeVec, labelValue string) int {
	t.Helper()
	reg := prometheus.NewRegistry()
	require.NoError(t, reg.Register(vec))
	families, err := reg.Gather()
	require.NoError(t, err)
	count := 0
	for _, f := range families {
		for _, m := range f.GetMetric() {
			for _, l := range m.GetLabel() {
				if l.GetName() == "client" && l.GetValue() == labelValue {
					count++
				}
			}
		}
	}
	return count
}

func waitFor(t *testing.T, timeout time.Duration, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	require.True(t, cond(), "condition not met within %s", timeout)
}

func TestStartRedisMetricsCollector_PublishesPoolGaugesOnTheFirstTickWithoutWaiting(t *testing.T) {
	client := "test-first-tick"
	fake := newFakeRedisPoolStatsSource(client, RedisPoolStats{TotalConns: 7, IdleConns: 3, StaleConns: 1, PoolSize: 10})

	c := NewRedisMetricsCollector()
	c.interval = time.Hour // if the collector needed the ticker to fire, this test would time out

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	c.Start(ctx, fake)

	waitFor(t, 50*time.Millisecond, func() bool {
		return countGaugeVecSamplesForLabel(t, redisPoolTotalConns, client) == 1
	})

	assert.Equal(t, float64(7), testutil.ToFloat64(redisPoolTotalConns.WithLabelValues(client)))
}

func TestStartRedisMetricsCollector_IssuesZeroRedisCommands(t *testing.T) {
	client := "test-zero-commands"
	fake := newFakeRedisPoolStatsSource(client,
		RedisPoolStats{TotalConns: 1},
		RedisPoolStats{TotalConns: 2},
		RedisPoolStats{TotalConns: 3},
	)

	c := NewRedisMetricsCollector()
	c.interval = 5 * time.Millisecond

	ctx, cancel := context.WithCancel(context.Background())
	c.Start(ctx, fake)

	waitFor(t, 200*time.Millisecond, func() bool {
		return atomic.LoadInt32(&fake.statsCalls) >= 3
	})
	cancel()

	assert.Equal(t, int32(0), atomic.LoadInt32(&fake.commandCalls),
		"PoolStats() reads local pool state and dispatches no command; the collector must never call anything else")
}

func TestStartRedisMetricsCollector_StopsWhenContextIsCancelled(t *testing.T) {
	client := "test-stop"
	fake := newFakeRedisPoolStatsSource(client, RedisPoolStats{TotalConns: 1})

	c := NewRedisMetricsCollector()
	c.interval = 5 * time.Millisecond

	ctx, cancel := context.WithCancel(context.Background())
	c.Start(ctx, fake)

	waitFor(t, 200*time.Millisecond, func() bool {
		return atomic.LoadInt32(&fake.statsCalls) >= 1
	})
	cancel()

	// Let any in-flight tick finish, then snapshot the call count and confirm it stops advancing.
	time.Sleep(20 * time.Millisecond)
	after := atomic.LoadInt32(&fake.statsCalls)
	time.Sleep(50 * time.Millisecond)
	assert.Equal(t, after, atomic.LoadInt32(&fake.statsCalls),
		"the collector goroutine must exit on ctx.Done() and stop calling RedisPoolStats()")
}

func TestStartRedisMetricsCollector_ConvertsCumulativePoolCountersIntoCounterDeltas(t *testing.T) {
	client := "test-delta"
	fake := newFakeRedisPoolStatsSource(client,
		RedisPoolStats{Hits: 5},
		RedisPoolStats{Hits: 9},
	)

	c := NewRedisMetricsCollector()
	c.interval = 10 * time.Millisecond

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	c.Start(ctx, fake)

	waitFor(t, 500*time.Millisecond, func() bool {
		return atomic.LoadInt32(&fake.statsCalls) >= 2
	})
	// Give the second tick's Add() a moment to land.
	time.Sleep(20 * time.Millisecond)

	got := testutil.ToFloat64(redisPoolHitsTotal.WithLabelValues(client))
	assert.Equal(t, float64(9), got,
		"the exported counter total must equal the raw cumulative value (9), reached by adding the delta (4), not by re-adding the raw value each tick (which would give 14)")
}

func TestStartRedisMetricsCollector_TreatsABackwardsCounterAsAResetRatherThanANegativeDelta(t *testing.T) {
	client := "test-reset"
	fake := newFakeRedisPoolStatsSource(client,
		RedisPoolStats{Hits: 9},
		RedisPoolStats{Hits: 3}, // client reconstructed: counter went backwards
	)

	c := NewRedisMetricsCollector()
	c.interval = 10 * time.Millisecond

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	require.NotPanics(t, func() {
		c.Start(ctx, fake)
		waitFor(t, 500*time.Millisecond, func() bool {
			return atomic.LoadInt32(&fake.statsCalls) >= 2
		})
		time.Sleep(20 * time.Millisecond)
	})

	got := testutil.ToFloat64(redisPoolHitsTotal.WithLabelValues(client))
	assert.Equal(t, float64(9+3), got,
		"a backwards counter must be treated as a reset: add the new value (3) on top of what was already exported (9), giving 12 — never a negative Add(3-9)")
}

func TestRedisPoolGauges_AreAbsentBeforeTheFirstObservation(t *testing.T) {
	client := "test-absent-gauge"

	assert.Equal(t, 0, countGaugeVecSamplesForLabel(t, redisPoolTotalConns, client),
		"a label combination that has never been observed must not appear in the scrape")

	redisPoolTotalConns.WithLabelValues(client).Set(42)

	assert.Equal(t, 1, countGaugeVecSamplesForLabel(t, redisPoolTotalConns, client))
}

func TestStartRedisMetricsCollector_LabelsEachClientSeparately(t *testing.T) {
	limiterClient := "test-labels-limiter"
	cacheClient := "test-labels-cache"

	limiter := newFakeRedisPoolStatsSource(limiterClient, RedisPoolStats{TotalConns: 11})
	cache := newFakeRedisPoolStatsSource(cacheClient, RedisPoolStats{TotalConns: 22})

	c := NewRedisMetricsCollector()
	c.interval = time.Hour

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	c.Start(ctx, limiter, cache)

	waitFor(t, 200*time.Millisecond, func() bool {
		return countGaugeVecSamplesForLabel(t, redisPoolTotalConns, limiterClient) == 1 &&
			countGaugeVecSamplesForLabel(t, redisPoolTotalConns, cacheClient) == 1
	})

	assert.Equal(t, float64(11), testutil.ToFloat64(redisPoolTotalConns.WithLabelValues(limiterClient)))
	assert.Equal(t, float64(22), testutil.ToFloat64(redisPoolTotalConns.WithLabelValues(cacheClient)))
}

func TestRateLimitRejections_HasBoundedTierLabels(t *testing.T) {
	for _, tier := range []string{"regular", "auth", "sentinel"} {
		RateLimitRejectionsTotal.WithLabelValues(tier).Inc()
	}

	reg := prometheus.NewRegistry()
	require.NoError(t, reg.Register(RateLimitRejectionsTotal))
	families, err := reg.Gather()
	require.NoError(t, err)
	require.Len(t, families, 1)

	seen := map[string]bool{}
	for _, m := range families[0].GetMetric() {
		for _, l := range m.GetLabel() {
			if l.GetName() == "tier" {
				seen[l.GetValue()] = true
			}
		}
	}
	for tier := range seen {
		assert.Contains(t, []string{"regular", "auth", "sentinel"}, tier,
			"ratelimit_rejections_total tier label must stay within the bounded set")
	}
	assert.ElementsMatch(t, []string{"regular", "auth", "sentinel"}, keysOf(seen))
}

func keysOf(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
