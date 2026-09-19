package services

import (
	"context"
	"testing"
	"time"

	"github.com/maxnussbaum/BoostedBudget/internal/cache"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/repository"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// Issue #1531 Task 11e: SystemHealthResponse.CacheEnabled is the ONE live signal this task's E2E
// cache-parity/fail-safe suite (apps/web/e2e/cache-*.spec.ts) can gate on before running. It did
// not exist before this task — the public /health endpoint deliberately dropped its component
// breakdown (Task 6), so the guard has to live on the admin-gated endpoint instead, and nothing on
// that endpoint reported cache status either. See domain.SystemHealthResponse.CacheEnabled's doc
// comment for why it reads cache.Cache.Enabled(NamespaceAnalytics) rather than cfg.CacheEnabled
// directly.

type fakeCacheStatusReader struct {
	enabled bool
}

func (f fakeCacheStatusReader) Enabled(cache.Namespace) bool { return f.enabled }

func TestSystemHealth_CacheEnabled_ReflectsCacheStatusReader(t *testing.T) {
	tests := []struct {
		name string
		want bool
	}{
		{name: "cache reports enabled", want: true},
		{name: "cache reports disabled", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := &systemHealthService{cacheStatus: fakeCacheStatusReader{enabled: tt.want}}

			result := svc.checkCacheEnabled()

			assert.Equal(t, tt.want, result)
		})
	}
}

// TestSystemHealth_CacheEnabled_NilReaderReportsFalse pins the nil-safe convention this file's
// other optional collaborators already follow (plaidOrphanRepo, jobHealthRepo, etc.) — an unwired
// cacheStatus (e.g. an older test construction of this struct, or a deployment with no cache
// client at all) must report "disabled", never panic and never a confident "enabled".
//
// Unlike the OTHER optional collaborators on this service (which distinguish "unavailable" from a
// confident zero via a separate Available bool), a nil cacheStatus and a real, disabled cache are
// not actually ambiguous here: cache.NewNoopCache().Enabled() itself always returns false (see
// noop.go), so "no cache wired" and "cache wired but disabled" are the SAME true state, not two
// states this field needs to tell apart.
func TestSystemHealth_CacheEnabled_NilReaderReportsFalse(t *testing.T) {
	svc := &systemHealthService{}

	result := svc.checkCacheEnabled()

	assert.False(t, result)
}

// TestSystemHealth_CacheEnabled_WiredIntoGetSystemHealth confirms the field actually reaches the
// top-level GetSystemHealth response, not just the internal checkCacheEnabled helper — this is
// the exact JSON field (`cache_enabled`) the E2E guard reads over the wire.
func TestSystemHealth_CacheEnabled_WiredIntoGetSystemHealth(t *testing.T) {
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

	svc := &systemHealthService{
		healthRepo:     mockHealthRepo,
		deadLetterRepo: mockDeadLetterRepo,
		pool:           mockPool,
		plaidEnv:       "sandbox",
		cacheStatus:    fakeCacheStatusReader{enabled: true},
	}

	result, err := svc.GetSystemHealth(context.Background())
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.True(t, result.CacheEnabled)
}
