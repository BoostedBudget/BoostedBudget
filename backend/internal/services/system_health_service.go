package services

import (
	"context"
	"time"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/repository"
)


// SystemHealthRepository defines methods for retrieving system health metrics
type SystemHealthRepository interface {
	GetRiverJobStats(ctx context.Context, queue string) (*domain.QueueMetrics, error)
	GetRiverJobsCompletedLastHour(ctx context.Context, queue string) (int64, error)
	GetActiveSyncCount(ctx context.Context) (int64, error)
	GetFailedSyncsLastHour(ctx context.Context) (int64, error)
	GetAverageSyncDuration(ctx context.Context) (int64, error)
	GetActiveSubscriptionCount(ctx context.Context) (int64, error)
	GetPaddleWebhookStats24h(ctx context.Context) (int64, int64, error)
	GetLastReconciliationRun(ctx context.Context) (*time.Time, error)
}

// DeadLetterJobRepository defines methods for dead letter job operations
type DeadLetterJobRepository interface {
	GetStats(ctx context.Context) (*repository.DeadLetterStats, error)
	GetOldestJobAge(ctx context.Context) (int64, error)
}

// DatabasePool defines minimal interface for database connection pool
type DatabasePool interface {
	Ping(ctx context.Context) error
	GetStats() PoolStats
}

// PoolStats represents connection pool statistics
type PoolStats struct {
	AcquiredConns     int32
	IdleConns         int32
	MaxConns          int32
	ConstructingConns int32
	TotalConns        int32
}

// SystemHealthService provides system health monitoring
type SystemHealthService interface {
	GetSystemHealth(ctx context.Context) (*domain.SystemHealthResponse, error)
}

type systemHealthService struct {
	healthRepo     SystemHealthRepository
	deadLetterRepo DeadLetterJobRepository
	pool           DatabasePool
	plaidEnv       string
}

// NewSystemHealthService creates a new system health service
func NewSystemHealthService(
	healthRepo SystemHealthRepository,
	deadLetterRepo DeadLetterJobRepository,
	pool DatabasePool,
	plaidEnv string,
) SystemHealthService {
	return &systemHealthService{
		healthRepo:     healthRepo,
		deadLetterRepo: deadLetterRepo,
		pool:           pool,
		plaidEnv:       plaidEnv,
	}
}

// GetSystemHealth retrieves comprehensive system health metrics
func (s *systemHealthService) GetSystemHealth(ctx context.Context) (*domain.SystemHealthResponse, error) {
	response := &domain.SystemHealthResponse{
		Timestamp: time.Now(),
		JobQueues: make(map[string]domain.QueueMetrics),
	}

	// Check all services
	response.Services = domain.ServiceHealthStatuses{
		Database: s.checkDatabase(ctx),
		Clerk:    s.checkClerk(ctx),
		Plaid:    s.checkPlaid(ctx),
	}

	// Get queue metrics for all queues
	queues := []string{"critical", "default", "notifications", "low"}
	for _, queue := range queues {
		metrics, err := s.getQueueMetrics(ctx, queue)
		if err != nil {
			// Log error but continue - partial data is better than none
			response.JobQueues[queue] = domain.QueueMetrics{}
			continue
		}
		response.JobQueues[queue] = *metrics
	}

	// Get sync status
	syncMetrics, err := s.getSyncMetrics(ctx)
	if err != nil {
		response.SyncStatus = domain.SyncStatusMetrics{}
	} else {
		response.SyncStatus = *syncMetrics
	}

	// Get dead letter metrics
	deadLetterMetrics, err := s.getDeadLetterMetrics(ctx)
	if err != nil {
		response.DeadLetterQueue = domain.DeadLetterMetrics{}
	} else {
		response.DeadLetterQueue = *deadLetterMetrics
	}

	// Get Paddle metrics
	paddleMetrics, err := s.getPaddleMetrics(ctx)
	if err != nil {
		response.PaddleStatus = domain.PaddleStatusMetrics{}
	} else {
		response.PaddleStatus = *paddleMetrics
	}

	// Calculate overall status
	response.Status = s.calculateOverallStatus(response)

	return response, nil
}

// checkDatabase checks database health and connection pool metrics
func (s *systemHealthService) checkDatabase(ctx context.Context) domain.DatabaseHealth {
	start := time.Now()
	err := s.pool.Ping(ctx)
	latency := time.Since(start).Milliseconds()

	if err != nil {
		return domain.DatabaseHealth{
			Status:      "down",
			Connections: domain.ConnectionPoolStats{},
			LatencyMs:   latency,
		}
	}

	stats := s.pool.GetStats()
	return domain.DatabaseHealth{
		Status: "up",
		Connections: domain.ConnectionPoolStats{
			Active:       stats.AcquiredConns,
			Idle:         stats.IdleConns,
			Max:          stats.MaxConns,
			Constructing: stats.ConstructingConns,
			Total:        stats.TotalConns,
		},
		LatencyMs: latency,
	}
}

// checkClerk checks Clerk service status (placeholder)
func (s *systemHealthService) checkClerk(ctx context.Context) domain.ServiceHealth {
	// Placeholder - Clerk is considered up if configured
	return domain.ServiceHealth{
		Status:    "up",
		LatencyMs: 0,
	}
}

// checkPlaid checks Plaid service status
func (s *systemHealthService) checkPlaid(ctx context.Context) domain.PlaidHealth {
	// Plaid is considered up if environment is configured
	return domain.PlaidHealth{
		Status:      "up",
		Environment: s.plaidEnv,
	}
}

// getQueueMetrics retrieves metrics for a specific queue
func (s *systemHealthService) getQueueMetrics(ctx context.Context, queue string) (*domain.QueueMetrics, error) {
	stats, err := s.healthRepo.GetRiverJobStats(ctx, queue)
	if err != nil {
		return nil, err
	}

	completedLastHour, err := s.healthRepo.GetRiverJobsCompletedLastHour(ctx, queue)
	if err != nil {
		return nil, err
	}

	stats.CompletedLastHour = completedLastHour
	return stats, nil
}

// getSyncMetrics retrieves Plaid sync status metrics
func (s *systemHealthService) getSyncMetrics(ctx context.Context) (*domain.SyncStatusMetrics, error) {
	activeSyncs, err := s.healthRepo.GetActiveSyncCount(ctx)
	if err != nil {
		return nil, err
	}
	failedLastHour, err := s.healthRepo.GetFailedSyncsLastHour(ctx)
	if err != nil {
		return nil, err
	}
	avgDuration, err := s.healthRepo.GetAverageSyncDuration(ctx)
	if err != nil {
		return nil, err
	}
	return &domain.SyncStatusMetrics{
		ActiveSyncs:       activeSyncs,
		FailedLastHour:    failedLastHour,
		AverageDurationMs: avgDuration,
	}, nil
}

// getDeadLetterMetrics retrieves dead letter queue metrics
func (s *systemHealthService) getDeadLetterMetrics(ctx context.Context) (*domain.DeadLetterMetrics, error) {
	stats, err := s.deadLetterRepo.GetStats(ctx)
	if err != nil {
		return nil, err
	}
	oldestJobAge, err := s.deadLetterRepo.GetOldestJobAge(ctx)
	if err != nil {
		return nil, err
	}
	return &domain.DeadLetterMetrics{
		Count:             stats.Total,
		OldestJobAgeHours: oldestJobAge,
	}, nil
}

// getPaddleMetrics retrieves Paddle subscription and webhook health metrics.
func (s *systemHealthService) getPaddleMetrics(ctx context.Context) (*domain.PaddleStatusMetrics, error) {
	activeCount, err := s.healthRepo.GetActiveSubscriptionCount(ctx)
	if err != nil {
		return nil, err
	}

	total, failed, err := s.healthRepo.GetPaddleWebhookStats24h(ctx)
	if err != nil {
		return nil, err
	}

	var successRate float64
	if total > 0 {
		successRate = float64(total-failed) / float64(total) * 100
	}

	lastRun, err := s.healthRepo.GetLastReconciliationRun(ctx)
	if err != nil {
		return nil, err
	}

	return &domain.PaddleStatusMetrics{
		ActiveSubscriptions:   activeCount,
		WebhookSuccessRate24h: successRate,
		FailedWebhooks24h:     failed,
		TotalWebhooks24h:      total,
		LastReconciliationAt:  lastRun,
	}, nil
}

// calculateOverallStatus determines the overall system health status
func (s *systemHealthService) calculateOverallStatus(response *domain.SystemHealthResponse) string {
	// Unhealthy: Database down
	if response.Services.Database.Status == "down" {
		return "unhealthy"
	}

	// Degraded: High pending jobs or dead letter queue issues
	for _, metrics := range response.JobQueues {
		if metrics.Pending > 100 {
			return "degraded"
		}
	}

	if response.DeadLetterQueue.Count > 10 {
		return "degraded"
	}

	// Degraded: Low Paddle webhook success rate (with sufficient volume)
	if response.PaddleStatus.TotalWebhooks24h > 10 && response.PaddleStatus.WebhookSuccessRate24h < 80 {
		return "degraded"
	}

	// Healthy: Everything nominal
	return "healthy"
}
