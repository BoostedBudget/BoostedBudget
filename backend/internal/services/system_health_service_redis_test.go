package services

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/health"
	"github.com/maxnussbaum/BoostedBudget/internal/redisclient"
	"github.com/maxnussbaum/BoostedBudget/internal/repository"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// TestSystemHealth_RedisDownReportsDegradedOverallStatus is the Issue #1531 round-2 gap-analysis
// fix (task GA Part 2) for Issue #1531 criterion 12: "Redis remains non-critical — Redis down
// yields degraded + HTTP 200, never 503." This test previously asserted the OPPOSITE — that
// result.Status stayed "healthy" with Redis down — under the name
// TestSystemHealth_RedisDownDoesNotChangeOverallStatus. That assertion enshrined the exact defect
// the issue calls out: "/admin/health reporting green regardless of state is worse than no
// dashboard, because it gets trusted during an incident." Four prior review passes read this test
// passing and took it as evidence the requirement was met; it was actually pinning the bug.
//
// What is still true, and still tested by this file: Redis down must NEVER produce "unhealthy"
// (only Database can, and only Database is checked before Redis in calculateOverallStatus) — a
// Redis blip 503ing this probe would cycle every machine via Fly's http_checks, the exact hazard
// Task 6 closed off on the PUBLIC /health endpoint. This test now asserts the correct middle
// ground: "down" Redis moves the result to "degraded", not "healthy" (the old, wrong assertion)
// and not "unhealthy" (which would reopen the Task 6 hazard one level up, on the admin surface).
func TestSystemHealth_RedisDownReportsDegradedOverallStatus(t *testing.T) {
	mockHealthRepo := new(MockSystemHealthRepository)
	mockDeadLetterRepo := new(MockDeadLetterJobRepository)
	mockPool := new(MockPool)

	mockPool.On("Ping", mock.Anything).Return(nil)
	mockPool.On("GetStats").Return(PoolStats{MaxConns: 100})

	queues := []string{"critical", "default", "notifications", "low", "revenuecat"}
	for _, q := range queues {
		mockHealthRepo.On("GetRiverJobStats", mock.Anything, q).Return(&domain.QueueMetrics{}, nil)
		mockHealthRepo.On("GetRiverJobsCompletedLastHour", mock.Anything, q).Return(int64(0), nil)
	}
	mockHealthRepo.On("GetActiveSyncCount", mock.Anything).Return(int64(0), nil)
	mockHealthRepo.On("GetFailedSyncsLastHour", mock.Anything).Return(int64(0), nil)
	mockHealthRepo.On("GetAverageSyncDuration", mock.Anything).Return(int64(0), nil)
	mockDeadLetterRepo.On("GetStats", mock.Anything).Return(&repository.DeadLetterStats{}, nil)
	mockDeadLetterRepo.On("GetOldestJobAge", mock.Anything).Return(int64(0), nil)
	mockHealthRepo.On("GetActiveSubscriptionCount", mock.Anything).Return(int64(0), nil)
	mockHealthRepo.On("GetPaddleWebhookStats24h", mock.Anything).Return(int64(0), int64(0), nil)
	mockHealthRepo.On("GetLastReconciliationRun", mock.Anything).Return((*time.Time)(nil), nil)

	redisCheck := health.Memoize(health.Options{Name: "redis-down-test", TTL: -1}, func(ctx context.Context) error {
		return errors.New("dial tcp 10.1.2.3:6379: connect: connection refused")
	})

	svc := &systemHealthService{
		healthRepo:     mockHealthRepo,
		deadLetterRepo: mockDeadLetterRepo,
		pool:           mockPool,
		plaidEnv:       "sandbox",
		healthChecks: SharedHealthChecks{
			Redis: redisCheck,
		},
	}

	result, err := svc.GetSystemHealth(context.Background())
	require.NoError(t, err)
	require.NotNil(t, result)

	assert.Equal(t, "down", result.Services.Redis.Status)
	assert.Equal(t, "degraded", result.Status,
		"Redis being down must surface as degraded (Issue #1531 criterion 12), never healthy — "+
			"and never unhealthy either, since only Database may 503 this probe")
}

// TestSystemHealth_RedisReasonIsClassifiedNotRawErrorText pins Issue #1531 Task 7d's tightened
// requirement: RedisHealth.Reason is a SANITIZED CLASSIFICATION, never the raw error text — admin
// screens get screenshotted into support tickets and pasted into Slack, and the full detail
// (host:port, credentials-adjacent context) belongs in slog/Sentry, findable by request ID.
func TestSystemHealth_RedisReasonIsClassifiedNotRawErrorText(t *testing.T) {
	tests := []struct {
		name       string
		err        error
		wantReason string
	}{
		{
			name:       "connection refused",
			err:        errors.New("dial tcp 10.1.2.3:6379: connect: connection refused"),
			wantReason: "connection_refused",
		},
		{
			name:       "context deadline exceeded",
			err:        context.DeadlineExceeded,
			wantReason: "timeout",
		},
		{
			name:       "breaker open",
			err:        redisclient.ErrBreakerOpen,
			wantReason: "timeout",
		},
		{
			name:       "auth failure",
			err:        errors.New("NOAUTH Authentication required."),
			wantReason: "auth_failed",
		},
		{
			name:       "unrecognized error",
			err:        errors.New("something truly unexpected happened"),
			wantReason: "unknown",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			redisCheck := health.Memoize(health.Options{Name: "redis-classify-" + tt.name, TTL: -1}, func(ctx context.Context) error {
				return tt.err
			})
			svc := &systemHealthService{healthChecks: SharedHealthChecks{Redis: redisCheck}}

			result := svc.checkRedis(context.Background())

			assert.Equal(t, "down", result.Status)
			assert.Equal(t, tt.wantReason, result.Reason)
			// The raw error text (host:port in particular) must never leak into Reason.
			assert.NotContains(t, result.Reason, "10.1.2.3")
			assert.NotContains(t, result.Reason, "dial tcp")
		})
	}
}

// TestSystemHealth_RedisNotConfiguredReportsNotConfigured pins the nil-safe "unwired optional
// collaborator reports unavailable, never a confident zero" convention for Redis specifically.
func TestSystemHealth_RedisNotConfiguredReportsNotConfigured(t *testing.T) {
	svc := &systemHealthService{}

	result := svc.checkRedis(context.Background())

	assert.Equal(t, "not_configured", result.Status)
	assert.Equal(t, "not_configured", result.Reason)
	assert.Nil(t, result.PoolStats)
}

// TestSystemHealth_RedisWiredButUnconfiguredReportsNotConfigured is the #1531 L2 regression.
// In production (cmd/server/health_checks.go's createRedisCheck), registry.Redis is NEVER a nil
// Check -- an unwired Redis client makes the Check's underlying PROBE return an error, not the
// Check itself be nil. TestSystemHealth_RedisNotConfiguredReportsNotConfigured above only covers
// the (production-unreachable) nil-Check branch. This test wires a non-nil Check whose probe
// fails with the shared health.ErrNotConfigured sentinel -- exactly what createRedisCheck now
// produces -- and pins that checkRedis still reports "not_configured", not "down". Before the
// fix, this fell through to the generic "down" branch, and calculateOverallStatus (Issue #1531
// criterion 12) then reported the whole dashboard "degraded" for an intentionally memory-only
// deployment: local `go run`, QA, or any deploy that never wires Redis at all.
func TestSystemHealth_RedisWiredButUnconfiguredReportsNotConfigured(t *testing.T) {
	redisCheck := health.Memoize(health.Options{Name: "redis-unconfigured-test", TTL: -1}, func(ctx context.Context) error {
		return fmt.Errorf("redis: not configured: %w", health.ErrNotConfigured)
	})
	svc := &systemHealthService{
		healthChecks: SharedHealthChecks{Redis: redisCheck},
	}

	result := svc.checkRedis(context.Background())

	assert.Equal(t, "not_configured", result.Status)
	assert.Equal(t, "not_configured", result.Reason)
	assert.Nil(t, result.PoolStats)
}

// TestSystemHealth_RedisWiredButUnconfiguredDoesNotDegradeOverallStatus is the end-to-end version
// of the test above: it drives GetSystemHealth (not just checkRedis) to pin that an unconfigured
// Redis never reaches calculateOverallStatus as "down" and therefore never forces the top-line
// status to "degraded".
func TestSystemHealth_RedisWiredButUnconfiguredDoesNotDegradeOverallStatus(t *testing.T) {
	mockHealthRepo := new(MockSystemHealthRepository)
	mockDeadLetterRepo := new(MockDeadLetterJobRepository)
	mockPool := new(MockPool)

	mockPool.On("Ping", mock.Anything).Return(nil)
	mockPool.On("GetStats").Return(PoolStats{MaxConns: 100})

	queues := []string{"critical", "default", "notifications", "low", "revenuecat"}
	for _, q := range queues {
		mockHealthRepo.On("GetRiverJobStats", mock.Anything, q).Return(&domain.QueueMetrics{}, nil)
		mockHealthRepo.On("GetRiverJobsCompletedLastHour", mock.Anything, q).Return(int64(0), nil)
	}
	mockHealthRepo.On("GetActiveSyncCount", mock.Anything).Return(int64(0), nil)
	mockHealthRepo.On("GetFailedSyncsLastHour", mock.Anything).Return(int64(0), nil)
	mockHealthRepo.On("GetAverageSyncDuration", mock.Anything).Return(int64(0), nil)
	mockDeadLetterRepo.On("GetStats", mock.Anything).Return(&repository.DeadLetterStats{}, nil)
	mockDeadLetterRepo.On("GetOldestJobAge", mock.Anything).Return(int64(0), nil)
	mockHealthRepo.On("GetActiveSubscriptionCount", mock.Anything).Return(int64(0), nil)
	mockHealthRepo.On("GetPaddleWebhookStats24h", mock.Anything).Return(int64(0), int64(0), nil)
	mockHealthRepo.On("GetLastReconciliationRun", mock.Anything).Return((*time.Time)(nil), nil)

	redisCheck := health.Memoize(health.Options{Name: "redis-unconfigured-e2e-test", TTL: -1}, func(ctx context.Context) error {
		return fmt.Errorf("redis: not configured: %w", health.ErrNotConfigured)
	})

	svc := &systemHealthService{
		healthRepo:     mockHealthRepo,
		deadLetterRepo: mockDeadLetterRepo,
		pool:           mockPool,
		plaidEnv:       "sandbox",
		healthChecks: SharedHealthChecks{
			Redis: redisCheck,
		},
	}

	result, err := svc.GetSystemHealth(context.Background())
	require.NoError(t, err)
	require.NotNil(t, result)

	assert.Equal(t, "not_configured", result.Services.Redis.Status)
	assert.Equal(t, "healthy", result.Status,
		"an unwired Redis must never force the top-line status to degraded (Issue #1531 L2)")
}

// TestSystemHealth_RedisUpIncludesPoolStatsAndObservedAgo pins that a healthy Redis check
// populates PoolStats (when wired) and a non-negative ObservedAgoMs, so the admin dashboard can
// show how stale a cached probe result is (Issue #1531 Task 7d).
func TestSystemHealth_RedisUpIncludesPoolStatsAndObservedAgo(t *testing.T) {
	redisCheck := health.Memoize(health.Options{Name: "redis-up-test", TTL: -1}, func(ctx context.Context) error {
		return nil
	})
	svc := &systemHealthService{
		healthChecks: SharedHealthChecks{
			Redis: redisCheck,
			RedisPoolStats: func() redisclient.PoolStats {
				return redisclient.PoolStats{Hits: 10, Misses: 2, TotalConns: 5, IdleConns: 3, PoolSize: 10}
			},
		},
	}

	result := svc.checkRedis(context.Background())

	assert.Equal(t, "up", result.Status)
	assert.Empty(t, result.Reason)
	assert.GreaterOrEqual(t, result.ObservedAgoMs, int64(0))
	require.NotNil(t, result.PoolStats)
	assert.Equal(t, uint32(10), result.PoolStats.Hits)
	assert.Equal(t, 10, result.PoolStats.PoolSize)
}
