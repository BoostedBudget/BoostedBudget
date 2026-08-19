package main

// GDPR/rate-limit round-6 finding P3: TestStartRedisMetricsCollector_IssuesZeroRedisCommands
// (internal/observability/redis_metrics_test.go) proves "zero Redis commands" only through the
// RedisPoolStatsSource INTERFACE, whose method set structurally cannot expose Get/Set/Ping — so
// that test is airtight against the interface but says nothing about redisPoolStatsAdapter, the
// concrete PRODUCTION type actually wired into StartRedisMetricsCollector in main(). Adding, say,
// a client.Ping() call inside redisPoolStatsAdapter.RedisPoolStats() would still pass every
// existing test in this repo.
//
// This test closes that gap by driving the real, unexported redisPoolStatsAdapter (this file is
// package main, so it has access) with redisclient.Fake, and asserting Fake.TotalCommandCalls()
// stays at 0 after RedisPoolStats() runs — i.e. it asserts at the ADAPTER level, not the
// interface level. It was verified RED by temporarily adding a client.Ping() call inside
// redisPoolStatsAdapter.RedisPoolStats(): this test failed with the expected "want 0, got 1"
// message; the line was then removed and this test was confirmed green again.

import (
	"testing"

	"github.com/maxnussbaum/BoostedBudget/internal/redisclient"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRedisPoolStatsAdapter_RedisPoolStats_IssuesZeroRedisCommands(t *testing.T) {
	fake := redisclient.NewFake("adapter-zero-commands")
	fake.Stats = redisclient.PoolStats{TotalConns: 4, IdleConns: 2, StaleConns: 1, PoolSize: 10}

	adapter := redisPoolStatsAdapter{client: fake}
	stats := adapter.RedisPoolStats()

	require.Equal(t, 0, fake.TotalCommandCalls(),
		"redisPoolStatsAdapter.RedisPoolStats must read ONLY local pool state (PoolStats) and dispatch "+
			"no Redis command (Get/Set/Ping/RunScript/...) — this is the adapter-level guarantee "+
			"TestStartRedisMetricsCollector_IssuesZeroRedisCommands cannot express, because it only sees "+
			"through the RedisPoolStatsSource interface, which has no Get/Set/Ping methods to call in the "+
			"first place")

	// Sanity: the adapter must still actually forward PoolStats' fields, so a future change that
	// satisfies the zero-commands assertion by simply returning a zero-value struct (rather than
	// reading the real pool state) would be caught here.
	assert.Equal(t, uint32(4), stats.TotalConns)
	assert.Equal(t, uint32(2), stats.IdleConns)
	assert.Equal(t, uint32(1), stats.StaleConns)
	assert.Equal(t, 10, stats.PoolSize)

	assert.Equal(t, "adapter-zero-commands", adapter.Name())
}
