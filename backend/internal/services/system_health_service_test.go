package services

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/health"
	"github.com/maxnussbaum/BoostedBudget/internal/repository"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// Mock repositories
type MockSystemHealthRepository struct {
	mock.Mock
}

func (m *MockSystemHealthRepository) GetRiverJobStats(ctx context.Context, queue string) (*domain.QueueMetrics, error) {
	args := m.Called(ctx, queue)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*domain.QueueMetrics), args.Error(1)
}

func (m *MockSystemHealthRepository) GetRiverJobsCompletedLastHour(ctx context.Context, queue string) (int64, error) {
	args := m.Called(ctx, queue)
	return args.Get(0).(int64), args.Error(1)
}

func (m *MockSystemHealthRepository) GetActiveSyncCount(ctx context.Context) (int64, error) {
	args := m.Called(ctx)
	return args.Get(0).(int64), args.Error(1)
}

func (m *MockSystemHealthRepository) GetFailedSyncsLastHour(ctx context.Context) (int64, error) {
	args := m.Called(ctx)
	return args.Get(0).(int64), args.Error(1)
}

func (m *MockSystemHealthRepository) GetAverageSyncDuration(ctx context.Context) (int64, error) {
	args := m.Called(ctx)
	return args.Get(0).(int64), args.Error(1)
}

func (m *MockSystemHealthRepository) GetActiveSubscriptionCount(ctx context.Context) (int64, error) {
	args := m.Called(ctx)
	return args.Get(0).(int64), args.Error(1)
}

func (m *MockSystemHealthRepository) GetPaddleWebhookStats24h(ctx context.Context) (int64, int64, error) {
	args := m.Called(ctx)
	return args.Get(0).(int64), args.Get(1).(int64), args.Error(2)
}

func (m *MockSystemHealthRepository) GetLastReconciliationRun(ctx context.Context) (*time.Time, error) {
	args := m.Called(ctx)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*time.Time), args.Error(1)
}

type MockDeadLetterJobRepository struct {
	mock.Mock
}

func (m *MockDeadLetterJobRepository) GetStats(ctx context.Context) (*repository.DeadLetterStats, error) {
	args := m.Called(ctx)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*repository.DeadLetterStats), args.Error(1)
}

func (m *MockDeadLetterJobRepository) GetOldestJobAge(ctx context.Context) (int64, error) {
	args := m.Called(ctx)
	return args.Get(0).(int64), args.Error(1)
}

// MockJobHealthRepository mocks repository.JobHealthRepository — the two backlog gauge queries
// backing SystemHealthResponse.JobBacklog (Issue #1458), plus the deletion-residue count backing
// SystemHealthResponse.DeletionResidue (Issue #1504).
type MockJobHealthRepository struct {
	mock.Mock
}

func (*MockJobHealthRepository) GetDeadLetterQueueSize(context.Context) (int, error) { return 0, nil }

func (m *MockJobHealthRepository) GetOldestUnclearedExpiredManualTierOverrideAge(ctx context.Context) (time.Duration, error) {
	args := m.Called(ctx)
	return args.Get(0).(time.Duration), args.Error(1)
}

func (m *MockJobHealthRepository) GetOutstandingPlaidCleanupStats(ctx context.Context) (int, time.Duration, error) {
	args := m.Called(ctx)
	return args.Int(0), args.Get(1).(time.Duration), args.Error(2)
}

// GetDeletionResidueStats backs SystemHealthResponse.DeletionResidue (Issue #1504).
func (m *MockJobHealthRepository) GetDeletionResidueStats(ctx context.Context) (int, time.Duration, error) {
	args := m.Called(ctx)
	return args.Int(0), args.Get(1).(time.Duration), args.Error(2)
}

// Mock pool for database checks
type MockPool struct {
	mock.Mock
}

func (m *MockPool) Ping(ctx context.Context) error {
	args := m.Called(ctx)
	return args.Error(0)
}

func (m *MockPool) GetStats() PoolStats {
	args := m.Called()
	return args.Get(0).(PoolStats)
}

func TestSystemHealthService_GetSystemHealth_Success(t *testing.T) {
	mockHealthRepo := new(MockSystemHealthRepository)
	mockDeadLetterRepo := new(MockDeadLetterJobRepository)
	mockPool := new(MockPool)

	// Setup mock expectations
	mockPool.On("Ping", mock.Anything).Return(nil)
	mockPool.On("GetStats").Return(PoolStats{
		AcquiredConns:     10,
		IdleConns:         5,
		MaxConns:          100,
		ConstructingConns: 2,
		TotalConns:        17,
	})

	// Mock queue stats for each queue
	queues := []string{"critical", "default", "notifications", "low"}
	for _, queue := range queues {
		mockHealthRepo.On("GetRiverJobStats", mock.Anything, queue).Return(&domain.QueueMetrics{
			Pending: 5,
			Running: 2,
			Failed:  0,
		}, nil)
		mockHealthRepo.On("GetRiverJobsCompletedLastHour", mock.Anything, queue).Return(int64(100), nil)
	}

	// Mock sync status
	mockHealthRepo.On("GetActiveSyncCount", mock.Anything).Return(int64(3), nil)
	mockHealthRepo.On("GetFailedSyncsLastHour", mock.Anything).Return(int64(1), nil)
	mockHealthRepo.On("GetAverageSyncDuration", mock.Anything).Return(int64(3500), nil)

	// Mock dead letter stats
	mockDeadLetterRepo.On("GetStats", mock.Anything).Return(&repository.DeadLetterStats{
		Total:       2,
		Unnotified:  1,
		Retried:     0,
		Last24Hours: 2,
	}, nil)
	mockDeadLetterRepo.On("GetOldestJobAge", mock.Anything).Return(int64(5), nil)

	// Mock Paddle metrics
	mockHealthRepo.On("GetActiveSubscriptionCount", mock.Anything).Return(int64(50), nil)
	mockHealthRepo.On("GetPaddleWebhookStats24h", mock.Anything).Return(int64(100), int64(2), nil)
	mockHealthRepo.On("GetLastReconciliationRun", mock.Anything).Return((*time.Time)(nil), nil)

	// Create service (note: we'll need to update the constructor to accept interfaces)
	service := &systemHealthService{
		healthRepo:     mockHealthRepo,
		deadLetterRepo: mockDeadLetterRepo,
		pool:           mockPool,
		plaidEnv:       "sandbox",
		// Issue #1531 Task 7d: checkClerk now reads the shared memoized Check instead of an
		// unconditional "up" placeholder. This test simulates an entirely healthy system, so wire
		// a Check that reports healthy, matching the pre-#1531 placeholder's assertion below.
		healthChecks: SharedHealthChecks{
			Clerk: func(ctx context.Context) health.Result { return health.Result{} },
		},
	}

	ctx := context.Background()
	result, err := service.GetSystemHealth(ctx)

	require.NoError(t, err)
	assert.NotNil(t, result)
	assert.Equal(t, "healthy", result.Status)
	assert.Equal(t, "up", result.Services.Database.Status)
	assert.Equal(t, "up", result.Services.Clerk.Status)
	assert.Equal(t, "up", result.Services.Plaid.Status)
	assert.Equal(t, "sandbox", result.Services.Plaid.Environment)
	assert.Equal(t, int64(3), result.SyncStatus.ActiveSyncs)
	assert.Equal(t, int64(2), result.DeadLetterQueue.Count)
	assert.Len(t, result.JobQueues, 4)

	mockHealthRepo.AssertExpectations(t)
	mockDeadLetterRepo.AssertExpectations(t)
	mockPool.AssertExpectations(t)
}

func TestSystemHealthService_GetSystemHealth_DatabaseDown(t *testing.T) {
	mockHealthRepo := new(MockSystemHealthRepository)
	mockDeadLetterRepo := new(MockDeadLetterJobRepository)
	mockPool := new(MockPool)

	// Simulate database down
	mockPool.On("Ping", mock.Anything).Return(errors.New("connection refused"))

	// Service still tries to gather other metrics
	queues := []string{"critical", "default", "notifications", "low"}
	for _, queue := range queues {
		mockHealthRepo.On("GetRiverJobStats", mock.Anything, queue).Return(&domain.QueueMetrics{}, nil)
		mockHealthRepo.On("GetRiverJobsCompletedLastHour", mock.Anything, queue).Return(int64(0), nil)
	}

	mockHealthRepo.On("GetActiveSyncCount", mock.Anything).Return(int64(0), nil)
	mockHealthRepo.On("GetFailedSyncsLastHour", mock.Anything).Return(int64(0), nil)
	mockHealthRepo.On("GetAverageSyncDuration", mock.Anything).Return(int64(0), nil)

	mockDeadLetterRepo.On("GetStats", mock.Anything).Return(&repository.DeadLetterStats{}, nil)
	mockDeadLetterRepo.On("GetOldestJobAge", mock.Anything).Return(int64(0), nil)

	// Mock Paddle metrics
	mockHealthRepo.On("GetActiveSubscriptionCount", mock.Anything).Return(int64(0), nil)
	mockHealthRepo.On("GetPaddleWebhookStats24h", mock.Anything).Return(int64(0), int64(0), nil)
	mockHealthRepo.On("GetLastReconciliationRun", mock.Anything).Return((*time.Time)(nil), nil)

	service := &systemHealthService{
		healthRepo:     mockHealthRepo,
		deadLetterRepo: mockDeadLetterRepo,
		pool:           mockPool,
		plaidEnv:       "sandbox",
	}

	ctx := context.Background()
	result, err := service.GetSystemHealth(ctx)

	require.NoError(t, err)
	assert.NotNil(t, result)
	assert.Equal(t, "unhealthy", result.Status)
	assert.Equal(t, "down", result.Services.Database.Status)

	mockPool.AssertExpectations(t)
	mockHealthRepo.AssertExpectations(t)
	mockDeadLetterRepo.AssertExpectations(t)
}

func TestSystemHealthService_GetSystemHealth_DegradedHighPendingJobs(t *testing.T) {
	mockHealthRepo := new(MockSystemHealthRepository)
	mockDeadLetterRepo := new(MockDeadLetterJobRepository)
	mockPool := new(MockPool)

	mockPool.On("Ping", mock.Anything).Return(nil)
	mockPool.On("GetStats").Return(PoolStats{
		AcquiredConns: 10,
		IdleConns:     5,
		MaxConns:      100,
		TotalConns:    15,
	})

	// Mock high pending jobs
	queues := []string{"critical", "default", "notifications", "low"}
	for _, queue := range queues {
		if queue == "default" {
			mockHealthRepo.On("GetRiverJobStats", mock.Anything, queue).Return(&domain.QueueMetrics{
				Pending: 150, // Above threshold
				Running: 2,
				Failed:  0,
			}, nil)
		} else {
			mockHealthRepo.On("GetRiverJobStats", mock.Anything, queue).Return(&domain.QueueMetrics{
				Pending: 5,
				Running: 2,
				Failed:  0,
			}, nil)
		}
		mockHealthRepo.On("GetRiverJobsCompletedLastHour", mock.Anything, queue).Return(int64(100), nil)
	}

	mockHealthRepo.On("GetActiveSyncCount", mock.Anything).Return(int64(3), nil)
	mockHealthRepo.On("GetFailedSyncsLastHour", mock.Anything).Return(int64(1), nil)
	mockHealthRepo.On("GetAverageSyncDuration", mock.Anything).Return(int64(3500), nil)

	mockDeadLetterRepo.On("GetStats", mock.Anything).Return(&repository.DeadLetterStats{Total: 2}, nil)
	mockDeadLetterRepo.On("GetOldestJobAge", mock.Anything).Return(int64(5), nil)

	// Mock Paddle metrics (healthy)
	mockHealthRepo.On("GetActiveSubscriptionCount", mock.Anything).Return(int64(50), nil)
	mockHealthRepo.On("GetPaddleWebhookStats24h", mock.Anything).Return(int64(100), int64(2), nil)
	mockHealthRepo.On("GetLastReconciliationRun", mock.Anything).Return((*time.Time)(nil), nil)

	service := &systemHealthService{
		healthRepo:     mockHealthRepo,
		deadLetterRepo: mockDeadLetterRepo,
		pool:           mockPool,
		plaidEnv:       "sandbox",
	}

	ctx := context.Background()
	result, err := service.GetSystemHealth(ctx)

	require.NoError(t, err)
	assert.NotNil(t, result)
	assert.Equal(t, "degraded", result.Status)

	mockHealthRepo.AssertExpectations(t)
	mockDeadLetterRepo.AssertExpectations(t)
	mockPool.AssertExpectations(t)
}

func TestSystemHealthService_CheckDatabase_LatencyMeasurement(t *testing.T) {
	mockPool := new(MockPool)

	mockPool.On("Ping", mock.Anything).Return(nil)
	mockPool.On("GetStats").Return(PoolStats{
		AcquiredConns: 10,
		IdleConns:     5,
		MaxConns:      100,
		TotalConns:    15,
	})

	service := &systemHealthService{
		pool: mockPool,
	}

	ctx := context.Background()
	health := service.checkDatabase(ctx)

	assert.Equal(t, "up", health.Status)
	assert.GreaterOrEqual(t, health.LatencyMs, int64(0))
	assert.Equal(t, int32(10), health.Connections.Active)
	assert.Equal(t, int32(5), health.Connections.Idle)
	assert.Equal(t, int32(100), health.Connections.Max)

	mockPool.AssertExpectations(t)
}

func TestSystemHealthService_CalculateOverallStatus(t *testing.T) {
	service := &systemHealthService{}

	tests := []struct {
		name     string
		response *domain.SystemHealthResponse
		expected string
	}{
		{
			name: "healthy system",
			response: &domain.SystemHealthResponse{
				Services: domain.ServiceHealthStatuses{
					Database: domain.DatabaseHealth{Status: "up"},
				},
				JobQueues: map[string]domain.QueueMetrics{
					"default": {Pending: 10, Running: 2, Failed: 0},
				},
				DeadLetterQueue: domain.DeadLetterMetrics{Count: 5},
			},
			expected: "healthy",
		},
		{
			name: "unhealthy - database down",
			response: &domain.SystemHealthResponse{
				Services: domain.ServiceHealthStatuses{
					Database: domain.DatabaseHealth{Status: "down"},
				},
				JobQueues:       map[string]domain.QueueMetrics{},
				DeadLetterQueue: domain.DeadLetterMetrics{Count: 0},
			},
			expected: "unhealthy",
		},
		{
			name: "degraded - high pending jobs",
			response: &domain.SystemHealthResponse{
				Services: domain.ServiceHealthStatuses{
					Database: domain.DatabaseHealth{Status: "up"},
				},
				JobQueues: map[string]domain.QueueMetrics{
					"default": {Pending: 150, Running: 2, Failed: 0},
				},
				DeadLetterQueue: domain.DeadLetterMetrics{Count: 5},
			},
			expected: "degraded",
		},
		{
			name: "degraded - high dead letter count",
			response: &domain.SystemHealthResponse{
				Services: domain.ServiceHealthStatuses{
					Database: domain.DatabaseHealth{Status: "up"},
				},
				JobQueues: map[string]domain.QueueMetrics{
					"default": {Pending: 10, Running: 2, Failed: 0},
				},
				DeadLetterQueue: domain.DeadLetterMetrics{Count: 15},
			},
			expected: "degraded",
		},
		{
			name: "degraded - low paddle webhook success rate with volume",
			response: &domain.SystemHealthResponse{
				Services: domain.ServiceHealthStatuses{
					Database: domain.DatabaseHealth{Status: "up"},
				},
				JobQueues:       map[string]domain.QueueMetrics{},
				DeadLetterQueue: domain.DeadLetterMetrics{Count: 0},
				PaddleStatus: domain.PaddleStatusMetrics{
					TotalWebhooks24h:      50,
					WebhookSuccessRate24h: 70.0, // below 80%
				},
			},
			expected: "degraded",
		},
		{
			// Issue #1531 round-2 gap-analysis fix (task GA Part 2), criterion 12: "Redis remains
			// non-critical — Redis down yields degraded + HTTP 200, never 503." Before this fix,
			// Redis was never consulted by calculateOverallStatus at all, so this case fell through
			// to "healthy" — see TestSystemHealth_RedisDownReportsDegradedOverallStatus (renamed
			// from ...DoesNotChangeOverallStatus) for the full-pipeline version of this same case.
			name: "degraded - redis down",
			response: &domain.SystemHealthResponse{
				Services: domain.ServiceHealthStatuses{
					Database: domain.DatabaseHealth{Status: "up"},
					Redis:    domain.RedisHealth{Status: "down"},
				},
				JobQueues:       map[string]domain.QueueMetrics{},
				DeadLetterQueue: domain.DeadLetterMetrics{Count: 0},
			},
			expected: "degraded",
		},
		{
			// Same fix, the Clerk half of criterion 12's requirement — the issue's GREEN criterion
			// is Redis OR Clerk down => degraded.
			name: "degraded - clerk down",
			response: &domain.SystemHealthResponse{
				Services: domain.ServiceHealthStatuses{
					Database: domain.DatabaseHealth{Status: "up"},
					Clerk:    domain.ServiceHealth{Status: "down"},
				},
				JobQueues:       map[string]domain.QueueMetrics{},
				DeadLetterQueue: domain.DeadLetterMetrics{Count: 0},
			},
			expected: "degraded",
		},
		{
			// Database down must win over Redis/Clerk being merely "degraded" — only Database may
			// ever produce "unhealthy" (see calculateOverallStatus's doc comment).
			name: "unhealthy - database down beats redis and clerk down",
			response: &domain.SystemHealthResponse{
				Services: domain.ServiceHealthStatuses{
					Database: domain.DatabaseHealth{Status: "down"},
					Redis:    domain.RedisHealth{Status: "down"},
					Clerk:    domain.ServiceHealth{Status: "down"},
				},
				JobQueues:       map[string]domain.QueueMetrics{},
				DeadLetterQueue: domain.DeadLetterMetrics{Count: 0},
			},
			expected: "unhealthy",
		},
		{
			name: "healthy - low webhook success rate but low volume",
			response: &domain.SystemHealthResponse{
				Services: domain.ServiceHealthStatuses{
					Database: domain.DatabaseHealth{Status: "up"},
				},
				JobQueues:       map[string]domain.QueueMetrics{},
				DeadLetterQueue: domain.DeadLetterMetrics{Count: 0},
				PaddleStatus: domain.PaddleStatusMetrics{
					TotalWebhooks24h:      5, // under 10 threshold
					WebhookSuccessRate24h: 60.0,
				},
			},
			expected: "healthy",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := service.calculateOverallStatus(tt.response)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func setupFullHealthMocks(
	mockHealthRepo *MockSystemHealthRepository,
	mockDeadLetterRepo *MockDeadLetterJobRepository,
	mockPool *MockPool,
	activeSubscriptions int64,
	webhookTotal int64,
	webhookFailed int64,
) {
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
	mockHealthRepo.On("GetActiveSubscriptionCount", mock.Anything).Return(activeSubscriptions, nil)
	mockHealthRepo.On("GetPaddleWebhookStats24h", mock.Anything).Return(webhookTotal, webhookFailed, nil)
	mockHealthRepo.On("GetLastReconciliationRun", mock.Anything).Return((*time.Time)(nil), nil)
}

func TestGetSystemHealth_PaddleMetricsPopulated(t *testing.T) {
	mockHealthRepo := new(MockSystemHealthRepository)
	mockDeadLetterRepo := new(MockDeadLetterJobRepository)
	mockPool := new(MockPool)

	setupFullHealthMocks(mockHealthRepo, mockDeadLetterRepo, mockPool, 42, 100, 5)

	service := &systemHealthService{
		healthRepo:     mockHealthRepo,
		deadLetterRepo: mockDeadLetterRepo,
		pool:           mockPool,
	}

	result, err := service.GetSystemHealth(context.Background())

	require.NoError(t, err)
	assert.Equal(t, int64(42), result.PaddleStatus.ActiveSubscriptions)
	assert.Equal(t, int64(100), result.PaddleStatus.TotalWebhooks24h)
	assert.Equal(t, int64(5), result.PaddleStatus.FailedWebhooks24h)
	assert.InDelta(t, 95.0, result.PaddleStatus.WebhookSuccessRate24h, 0.01)

	mockHealthRepo.AssertExpectations(t)
}

func TestGetSystemHealth_PaddleDegraded_LowWebhookRate(t *testing.T) {
	mockHealthRepo := new(MockSystemHealthRepository)
	mockDeadLetterRepo := new(MockDeadLetterJobRepository)
	mockPool := new(MockPool)

	// 50 total, 30 failed = 40% success rate
	setupFullHealthMocks(mockHealthRepo, mockDeadLetterRepo, mockPool, 10, 50, 30)

	service := &systemHealthService{
		healthRepo:     mockHealthRepo,
		deadLetterRepo: mockDeadLetterRepo,
		pool:           mockPool,
	}

	result, err := service.GetSystemHealth(context.Background())

	require.NoError(t, err)
	assert.Equal(t, "degraded", result.Status)

	mockHealthRepo.AssertExpectations(t)
}

func TestGetSystemHealth_PaddleMetricsError_UsesZeroValues(t *testing.T) {
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

	// Simulate error in Paddle metrics
	mockHealthRepo.On("GetActiveSubscriptionCount", mock.Anything).Return(int64(0), errors.New("db error"))

	service := &systemHealthService{
		healthRepo:     mockHealthRepo,
		deadLetterRepo: mockDeadLetterRepo,
		pool:           mockPool,
	}

	result, err := service.GetSystemHealth(context.Background())

	// Should not fail - paddle error uses zero values
	require.NoError(t, err)
	assert.NotNil(t, result)
	assert.Equal(t, int64(0), result.PaddleStatus.ActiveSubscriptions)
	assert.Equal(t, int64(0), result.PaddleStatus.TotalWebhooks24h)

	mockHealthRepo.AssertExpectations(t)
}

// TestGetSystemHealth_JobBacklogMetricsPopulated proves the three DB-derived backlog gauges
// (Issue #1458) are populated from repository.JobHealthRepository — the same queries the
// retention-cleanup repair job and the manual-tier-override expiry worker use, never
// duplicated SQL.
func TestGetSystemHealth_JobBacklogMetricsPopulated(t *testing.T) {
	mockHealthRepo := new(MockSystemHealthRepository)
	mockDeadLetterRepo := new(MockDeadLetterJobRepository)
	mockPool := new(MockPool)
	mockJobHealthRepo := new(MockJobHealthRepository)
	// Issue #1504: these tests predate the deletion-residue gauge and assert nothing about it, but
	// GetSystemHealth now reads it on every call. .Maybe() so an unrelated test cannot fail on a
	// call it does not care about, nor demand one.
	mockJobHealthRepo.On("GetDeletionResidueStats", mock.Anything).Return(0, time.Duration(0), nil).Maybe()

	setupFullHealthMocks(mockHealthRepo, mockDeadLetterRepo, mockPool, 0, 0, 0)
	mockJobHealthRepo.On("GetOutstandingPlaidCleanupStats", mock.Anything).Return(3, 2*time.Hour, nil)
	mockJobHealthRepo.On("GetOldestUnclearedExpiredManualTierOverrideAge", mock.Anything).Return(90*time.Minute, nil)

	service := &systemHealthService{
		healthRepo:     mockHealthRepo,
		deadLetterRepo: mockDeadLetterRepo,
		pool:           mockPool,
		jobHealthRepo:  mockJobHealthRepo,
	}

	result, err := service.GetSystemHealth(context.Background())

	require.NoError(t, err)
	assert.Equal(t, int64(3), result.JobBacklog.PlaidCleanupOutstandingUsers)
	assert.Equal(t, int64(2*60*60), result.JobBacklog.PlaidCleanupOldestOutstandingSeconds)
	assert.Equal(t, int64(90*60), result.JobBacklog.ManualTierOverrideOldestUnclearedExpiredSeconds)
	assert.True(t, result.JobBacklog.Available, "a successful query must be marked available (Issue #1458 follow-up G16)")

	mockJobHealthRepo.AssertExpectations(t)
}

// TestGetSystemHealth_JobBacklogMetrics_NilRepo_ZeroValues proves an unwired jobHealthRepo (nil)
// degrades to zero values rather than panicking — mirrors every other optional-dependency
// nil-safety convention in this codebase (e.g. SubscriptionService.overrideTierValidator).
func TestGetSystemHealth_JobBacklogMetrics_NilRepo_ZeroValues(t *testing.T) {
	mockHealthRepo := new(MockSystemHealthRepository)
	mockDeadLetterRepo := new(MockDeadLetterJobRepository)
	mockPool := new(MockPool)

	setupFullHealthMocks(mockHealthRepo, mockDeadLetterRepo, mockPool, 0, 0, 0)

	service := &systemHealthService{
		healthRepo:     mockHealthRepo,
		deadLetterRepo: mockDeadLetterRepo,
		pool:           mockPool,
		// jobHealthRepo intentionally left nil.
	}

	result, err := service.GetSystemHealth(context.Background())

	require.NoError(t, err)
	assert.Equal(t, domain.JobBacklogMetrics{}, result.JobBacklog)
}

// TestGetSystemHealth_JobBacklogMetricsError_UsesZeroValues mirrors
// TestGetSystemHealth_PaddleMetricsError_UsesZeroValues's "partial data over none" convention —
// a job-health query failure must not fail the whole /admin/health response.
//
// Issue #1458 follow-up G16: this also proves the failure is marked `Available: false`, not just
// zero-valued — see TestGetSystemHealth_JobBacklogMetrics_UnavailableVsRealZero_AreDistinguishable
// below for why the two must be distinguishable at all (a bare zero-value-struct equality check,
// which this test used before G16, cannot tell "query failed" apart from "query succeeded, found
// nothing" — both produce the exact same zero-valued domain.JobBacklogMetrics{}).
func TestGetSystemHealth_JobBacklogMetricsError_UsesZeroValues(t *testing.T) {
	mockHealthRepo := new(MockSystemHealthRepository)
	mockDeadLetterRepo := new(MockDeadLetterJobRepository)
	mockPool := new(MockPool)
	mockJobHealthRepo := new(MockJobHealthRepository)
	// Issue #1504: these tests predate the deletion-residue gauge and assert nothing about it, but
	// GetSystemHealth now reads it on every call. .Maybe() so an unrelated test cannot fail on a
	// call it does not care about, nor demand one.
	mockJobHealthRepo.On("GetDeletionResidueStats", mock.Anything).Return(0, time.Duration(0), nil).Maybe()

	setupFullHealthMocks(mockHealthRepo, mockDeadLetterRepo, mockPool, 0, 0, 0)
	mockJobHealthRepo.On("GetOutstandingPlaidCleanupStats", mock.Anything).Return(0, time.Duration(0), errors.New("db error"))

	service := &systemHealthService{
		healthRepo:     mockHealthRepo,
		deadLetterRepo: mockDeadLetterRepo,
		pool:           mockPool,
		jobHealthRepo:  mockJobHealthRepo,
	}

	result, err := service.GetSystemHealth(context.Background())

	require.NoError(t, err)
	assert.NotNil(t, result)
	assert.Equal(t, domain.JobBacklogMetrics{}, result.JobBacklog)
	assert.False(t, result.JobBacklog.Available, "a failed job-backlog query must be marked unavailable, not silently reported as a real zero")

	mockJobHealthRepo.AssertExpectations(t)
}

// TestGetSystemHealth_JobBacklogMetrics_SecondQueryError_DiscardsFirstQuerysGoodData is the
// coverage gap Codex flagged for Issue #1458 (batch R4 Task 4): every existing error test above
// stubs ONLY the FIRST query (GetOutstandingPlaidCleanupStats) to fail. Nothing pinned what
// happens when that first query SUCCEEDS with real, non-zero data and the SECOND query
// (GetOldestUnclearedExpiredManualTierOverrideAge) is the one that fails — a genuinely different
// code path through getJobBacklogMetrics's two sequential, independently-failable calls.
//
// Codex verified the implementation is already correct here (a coverage gap, not a behaviour
// bug): getJobBacklogMetrics returns (nil, err) on the second query's failure, so the first
// query's outstandingUsers/oldestOutstanding are discarded rather than surfaced as a
// partially-populated JobBacklogMetrics — see that method's own doc comment (system_health_service.go)
// for why discarding (not partial-reporting) is the deliberate choice. This test pins that
// discard behaviour so it can no longer regress silently.
func TestGetSystemHealth_JobBacklogMetrics_SecondQueryError_DiscardsFirstQuerysGoodData(t *testing.T) {
	mockHealthRepo := new(MockSystemHealthRepository)
	mockDeadLetterRepo := new(MockDeadLetterJobRepository)
	mockPool := new(MockPool)
	mockJobHealthRepo := new(MockJobHealthRepository)
	// Issue #1504: these tests predate the deletion-residue gauge and assert nothing about it, but
	// GetSystemHealth now reads it on every call. .Maybe() so an unrelated test cannot fail on a
	// call it does not care about, nor demand one.
	mockJobHealthRepo.On("GetDeletionResidueStats", mock.Anything).Return(0, time.Duration(0), nil).Maybe()

	setupFullHealthMocks(mockHealthRepo, mockDeadLetterRepo, mockPool, 0, 0, 0)
	// First query succeeds with real, non-zero data.
	mockJobHealthRepo.On("GetOutstandingPlaidCleanupStats", mock.Anything).Return(5, 3*time.Hour, nil)
	// Second query fails.
	mockJobHealthRepo.On("GetOldestUnclearedExpiredManualTierOverrideAge", mock.Anything).Return(time.Duration(0), errors.New("db error"))

	service := &systemHealthService{
		healthRepo:     mockHealthRepo,
		deadLetterRepo: mockDeadLetterRepo,
		pool:           mockPool,
		jobHealthRepo:  mockJobHealthRepo,
	}

	result, err := service.GetSystemHealth(context.Background())

	require.NoError(t, err, "a job-backlog query failure must not fail the whole /admin/health response")
	assert.Equal(t, domain.JobBacklogMetrics{}, result.JobBacklog,
		"the first query's good data (5 outstanding users, 3h oldest) must be DISCARDED, not partially surfaced, when the second query fails")
	assert.Equal(t, int64(0), result.JobBacklog.PlaidCleanupOutstandingUsers,
		"the first query's real, non-zero result must not leak through even though it succeeded")
	assert.False(t, result.JobBacklog.Available, "a failed second query must mark the whole gauge unavailable")

	mockJobHealthRepo.AssertExpectations(t)
}

// TestGetSystemHealth_JobBacklogMetrics_UnavailableVsRealZero_AreDistinguishable is the RED/GREEN
// pin for Issue #1458 follow-up G16 (the gap between #1458's own reasoning — applied to the FOUR
// metrics it excluded from this response — and these THREE, which it included but still let a
// failed query masquerade as a healthy zero).
//
// Before this fix, a query ERROR and a query that SUCCEEDS with a genuinely empty backlog were
// both represented by the exact same value: a zero-valued domain.JobBacklogMetrics{}. No field on
// the struct could tell them apart, so HealthDashboardPage.tsx rendered both as a confident green
// "0" — exactly backwards, since the failed-query case is the one moment the operator most needs
// a signal that the gauge cannot currently be trusted.
//
// This test proves the two cases are now distinguishable via `Available`: a REAL zero backlog
// (the repository call succeeds and legitimately returns 0/0) is marked `Available: true`, while a
// FAILED query is marked `Available: false` — even though every numeric field reads identically
// (0) in both cases.
func TestGetSystemHealth_JobBacklogMetrics_UnavailableVsRealZero_AreDistinguishable(t *testing.T) {
	t.Run("a successful query reporting a real, verified zero backlog is marked Available", func(t *testing.T) {
		mockHealthRepo := new(MockSystemHealthRepository)
		mockDeadLetterRepo := new(MockDeadLetterJobRepository)
		mockPool := new(MockPool)
		mockJobHealthRepo := new(MockJobHealthRepository)
		// Issue #1504: these tests predate the deletion-residue gauge and assert nothing about it, but
		// GetSystemHealth now reads it on every call. .Maybe() so an unrelated test cannot fail on a
		// call it does not care about, nor demand one.
		mockJobHealthRepo.On("GetDeletionResidueStats", mock.Anything).Return(0, time.Duration(0), nil).Maybe()

		setupFullHealthMocks(mockHealthRepo, mockDeadLetterRepo, mockPool, 0, 0, 0)
		mockJobHealthRepo.On("GetOutstandingPlaidCleanupStats", mock.Anything).Return(0, time.Duration(0), nil)
		mockJobHealthRepo.On("GetOldestUnclearedExpiredManualTierOverrideAge", mock.Anything).Return(time.Duration(0), nil)

		service := &systemHealthService{
			healthRepo:     mockHealthRepo,
			deadLetterRepo: mockDeadLetterRepo,
			pool:           mockPool,
			jobHealthRepo:  mockJobHealthRepo,
		}

		result, err := service.GetSystemHealth(context.Background())

		require.NoError(t, err)
		assert.Equal(t, int64(0), result.JobBacklog.PlaidCleanupOutstandingUsers)
		assert.True(t, result.JobBacklog.Available, "a successful query that legitimately finds zero backlog must be marked available — this is a VERIFIED zero, not an unknown")

		mockJobHealthRepo.AssertExpectations(t)
	})

	t.Run("a failed query is marked unavailable even though every numeric field is also zero", func(t *testing.T) {
		mockHealthRepo := new(MockSystemHealthRepository)
		mockDeadLetterRepo := new(MockDeadLetterJobRepository)
		mockPool := new(MockPool)
		mockJobHealthRepo := new(MockJobHealthRepository)
		// Issue #1504: these tests predate the deletion-residue gauge and assert nothing about it, but
		// GetSystemHealth now reads it on every call. .Maybe() so an unrelated test cannot fail on a
		// call it does not care about, nor demand one.
		mockJobHealthRepo.On("GetDeletionResidueStats", mock.Anything).Return(0, time.Duration(0), nil).Maybe()

		setupFullHealthMocks(mockHealthRepo, mockDeadLetterRepo, mockPool, 0, 0, 0)
		mockJobHealthRepo.On("GetOutstandingPlaidCleanupStats", mock.Anything).Return(0, time.Duration(0), errors.New("db error"))

		service := &systemHealthService{
			healthRepo:     mockHealthRepo,
			deadLetterRepo: mockDeadLetterRepo,
			pool:           mockPool,
			jobHealthRepo:  mockJobHealthRepo,
		}

		result, err := service.GetSystemHealth(context.Background())

		require.NoError(t, err)
		assert.Equal(t, int64(0), result.JobBacklog.PlaidCleanupOutstandingUsers, "numeric fields still read zero on error — Available is the only signal that distinguishes this from the real-zero case above")
		assert.False(t, result.JobBacklog.Available, "a failed query must never be marked available")

		mockJobHealthRepo.AssertExpectations(t)
	})
}
