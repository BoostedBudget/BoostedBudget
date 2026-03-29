package services

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
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
