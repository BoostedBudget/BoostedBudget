package main

import (
	"testing"

	"github.com/maxnussbaum/BoostedBudget/internal/config"
	"github.com/maxnussbaum/BoostedBudget/internal/ratelimitstore"
	"github.com/maxnussbaum/BoostedBudget/internal/redisclient"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Issue #1531 Slice B / M2 — RATE_LIMIT_BACKEND was validated at config.Load() time in every
// environment but cfg.RateLimitBackend was never read anywhere: buildRateLimitBackend branched
// solely on limiterClient == nil, so the documented "memory" and "redis" kill switches
// (docs/architecture/redis-topology.md, "Kill switches") did nothing. These tests assert the
// backend actually selected per mode, not merely that config parsed.

func TestBuildRateLimitBackend_ModeMemory_ForcesMemoryEvenWithLiveClient(t *testing.T) {
	fake := redisclient.NewFake("limiter")
	cfg := &config.Config{
		RateLimitBackend: "memory",
		RateLimitIP:      100,
		RateLimitAuth:    10,
	}

	backend, err := buildRateLimitBackend(fake, cfg)
	require.NoError(t, err)
	assert.Nil(t, backend, "RATE_LIMIT_BACKEND=memory must force in-memory limiting even though a live Redis limiter client is available")
}

func TestBuildRateLimitBackend_ModeRedis_FailsFastAtStartupWhenNoClient(t *testing.T) {
	cfg := &config.Config{
		RateLimitBackend: "redis",
		RateLimitIP:      100,
		RateLimitAuth:    10,
	}

	backend, err := buildRateLimitBackend(nil, cfg)
	require.Error(t, err, "RATE_LIMIT_BACKEND=redis with no Redis limiter client available must fail fast at startup, not silently fall back to memory")
	assert.Nil(t, backend)
}

func TestBuildRateLimitBackend_ModeRedis_UsesRedisWhenClientAvailable(t *testing.T) {
	fake := redisclient.NewFake("limiter")
	cfg := &config.Config{
		RateLimitBackend: "redis",
		RateLimitIP:      100,
		RateLimitAuth:    10,
	}

	backend, err := buildRateLimitBackend(fake, cfg)
	require.NoError(t, err)
	require.NotNil(t, backend)
	_, ok := backend.(*ratelimitstore.RedisBackend)
	assert.True(t, ok, "RATE_LIMIT_BACKEND=redis with a live client must select the Redis-backed backend")
}

func TestBuildRateLimitBackend_ModeAuto_PreservesNilCheckBehaviour(t *testing.T) {
	cfg := &config.Config{
		RateLimitBackend: "auto",
		RateLimitIP:      100,
		RateLimitAuth:    10,
	}

	// No client available: auto falls back to memory (nil backend), no error.
	backend, err := buildRateLimitBackend(nil, cfg)
	require.NoError(t, err)
	assert.Nil(t, backend)

	// Client available: auto selects the Redis-backed backend.
	fake := redisclient.NewFake("limiter")
	backend, err = buildRateLimitBackend(fake, cfg)
	require.NoError(t, err)
	require.NotNil(t, backend)
	_, ok := backend.(*ratelimitstore.RedisBackend)
	assert.True(t, ok)
}
