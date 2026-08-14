package services

import (
	"context"
	"errors"
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

// TestSystemHealth_RedisDownDoesNotChangeOverallStatus is the Issue #1531 Task 7d discrimination
// test: Redis reporting "down" (or any status at all) must never move
// calculateOverallStatus's answer. This is a DELIBERATE non-goal — see
// domain.ServiceHealthStatuses.Redis's doc comment — because /admin/health's overall status
// rollup feeding an alert must not inherit the exact "Redis blip cycles the fleet" hazard Task 6
// already closed off on the public liveness probe.
func TestSystemHealth_RedisDownDoesNotChangeOverallStatus(t *testing.T) {
	mockHealthRepo := new(MockSystemHealthRepository)
	mockDeadLetterRepo := new(MockDeadLetterJobRepository)
	mockPool := new(MockPool)

	mockPool.On("Ping", mock.Anything).Return(nil)
	mockPool.On("GetStats").Return(PoolStats{MaxConns: 100})

	queues := []string{"critical", "default", "notifications", "low"}
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
	assert.Equal(t, "healthy", result.Status, "Redis being down must never change the overall system status")
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
