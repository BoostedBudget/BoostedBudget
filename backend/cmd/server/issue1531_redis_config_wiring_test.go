package main

// Issue #1531 Blocker 4 — REDIS_TIMEOUT_MS / REDIS_POOL_SIZE were loaded, validated, and
// documented (internal/config/config.go ~:89-92,249-250 — the comment even claims they are "read
// by redisclient.Options construction") but neither main() call site
// (redisclient.New(redisclient.Options{...})) actually passed them through: both sent only
// URL/Name, so the knobs validated at boot were dead — every deploy silently ran on
// redisclient's hardcoded defaults (200ms read/write timeout, pool size 10) regardless of what an
// operator configured.
//
// newRedisClientOptions (main.go) is the fix: both call sites now build their Options through it.
// This test proves the mapping itself is correct — the one part of the fix a real-Redis
// integration test (which only sees the Client interface, with no Options() accessor) cannot
// observe directly. redisclient's own TestNew_ExplicitOptionsOverrideDefaults already proves,
// against the real go-redis client's rdb.Options(), that whatever an Options struct carries DOES
// reach the wire — so once this test pins that cfg.RedisTimeoutMs/RedisPoolSize land correctly in
// the Options struct handed to redisclient.New, the two tests together close the whole chain:
// config -> Options -> real client.
//
// ADVERSARIAL VALIDATION: temporarily reverted newRedisClientOptions to the pre-fix shape (only
// URL/Name set, ReadTimeout/WriteTimeout/PoolSize left zero) — TestNewRedisClientOptions_
// WiresConfiguredTimeoutAndPoolSize FAILED (zero != configured values). Restored the fix — test
// PASSED again.

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/maxnussbaum/BoostedBudget/internal/config"
)

func TestNewRedisClientOptions_WiresConfiguredTimeoutAndPoolSize(t *testing.T) {
	cfg := &config.Config{
		RedisTimeoutMs: 350,
		RedisPoolSize:  17,
	}

	opts := newRedisClientOptions(cfg, "redis://example.invalid:6379", "cache")

	assert.Equal(t, "redis://example.invalid:6379", opts.URL)
	assert.Equal(t, "cache", opts.Name)
	assert.Equal(t, 350*time.Millisecond, opts.ReadTimeout,
		"REDIS_TIMEOUT_MS must reach Options.ReadTimeout")
	assert.Equal(t, 350*time.Millisecond, opts.WriteTimeout,
		"REDIS_TIMEOUT_MS must reach Options.WriteTimeout")
	assert.Equal(t, 17, opts.PoolSize, "REDIS_POOL_SIZE must reach Options.PoolSize")
}

func TestNewRedisClientOptions_UsedForBothLimiterAndCacheNames(t *testing.T) {
	cfg := &config.Config{RedisTimeoutMs: 200, RedisPoolSize: 10}

	limiterOpts := newRedisClientOptions(cfg, "redis://limiter.invalid:6379", "limiter")
	cacheOpts := newRedisClientOptions(cfg, "redis://cache.invalid:6379", "cache")

	assert.Equal(t, "limiter", limiterOpts.Name)
	assert.Equal(t, "redis://limiter.invalid:6379", limiterOpts.URL)
	assert.Equal(t, "cache", cacheOpts.Name)
	assert.Equal(t, "redis://cache.invalid:6379", cacheOpts.URL)
	// Both share the same timeout/pool-size knobs — there is only one REDIS_TIMEOUT_MS /
	// REDIS_POOL_SIZE pair in config, applied to both clients (see config.go's doc comment).
	assert.Equal(t, limiterOpts.ReadTimeout, cacheOpts.ReadTimeout)
	assert.Equal(t, limiterOpts.PoolSize, cacheOpts.PoolSize)
}
