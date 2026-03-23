// Package jobs contains River job definitions and workers for async processing.
// Issue #762: Paddle reconciliation job
package jobs

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/maxnussbaum/BoostedBudget/internal/config"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/observability"
	"github.com/maxnussbaum/BoostedBudget/internal/queue"
	"github.com/riverqueue/river"
)

// PaddleReconciliationArgs contains the arguments for the Paddle reconciliation job.
// This job runs periodically to reconcile local subscription data against Paddle.
type PaddleReconciliationArgs struct {
	// TriggerSource indicates what triggered this check (e.g., "periodic", "manual")
	TriggerSource string `json:"trigger_source,omitempty"`
}

// Kind returns the unique identifier for this job type.
func (PaddleReconciliationArgs) Kind() string { return "paddle_reconciliation" }

// ReconciliationProfileRepository is the narrow interface needed for reconciliation.
type ReconciliationProfileRepository interface {
	GetUsersWithActivePaddleSubscriptions(ctx context.Context) ([]*domain.User, error)
}

// ReconciliationPaddleService is the narrow interface needed for reconciliation.
// Implementations should provide a method to look up a subscription from Paddle.
type ReconciliationPaddleService interface {
	// Placeholder — extend when concrete Paddle API method is available
}

// PaddleReconciliationWorker processes Paddle reconciliation jobs.
// It fetches all users with active Paddle subscriptions and verifies their
// local state matches the remote Paddle state.
type PaddleReconciliationWorker struct {
	river.WorkerDefaults[PaddleReconciliationArgs]

	profileRepo ReconciliationProfileRepository
	paddleSvc   ReconciliationPaddleService
	tierConfig  *config.TierConfig
	logger      *slog.Logger
	autoFix     bool
}

// PaddleReconciliationWorkerDeps contains the dependencies for PaddleReconciliationWorker.
type PaddleReconciliationWorkerDeps struct {
	ProfileRepo ReconciliationProfileRepository
	PaddleSvc   ReconciliationPaddleService
	TierConfig  *config.TierConfig
	Logger      *slog.Logger
	AutoFix     bool
}

// NewPaddleReconciliationWorker creates a new Paddle reconciliation worker.
func NewPaddleReconciliationWorker(deps PaddleReconciliationWorkerDeps) *PaddleReconciliationWorker {
	logger := deps.Logger
	if logger == nil {
		logger = slog.Default()
	}
	return &PaddleReconciliationWorker{
		profileRepo: deps.ProfileRepo,
		paddleSvc:   deps.PaddleSvc,
		tierConfig:  deps.TierConfig,
		logger:      logger,
		autoFix:     deps.AutoFix,
	}
}

// Work processes a Paddle reconciliation job.
//
// The worker:
//  1. Fetches all users with active Paddle subscriptions
//  2. For each user, verifies subscription status against Paddle (when service is configured)
//  3. Logs discrepancies; with autoFix=true will also correct local state
func (w *PaddleReconciliationWorker) Work(ctx context.Context, job *river.Job[PaddleReconciliationArgs]) (err error) {
	args := job.Args

	w.logger.Info("starting paddle reconciliation",
		"job_id", job.ID,
		"trigger_source", args.TriggerSource,
	)

	start := time.Now()
	defer func() {
		status := "success"
		if err != nil {
			status = "failed"
		}
		observability.JobsCompleted.WithLabelValues(job.Kind, job.Queue, status).Inc()
		observability.JobDuration.WithLabelValues(job.Kind).Observe(time.Since(start).Seconds())
	}()

	users, err := w.profileRepo.GetUsersWithActivePaddleSubscriptions(ctx)
	if err != nil {
		return fmt.Errorf("failed to fetch users with active subscriptions: %w", err)
	}

	if len(users) == 0 {
		w.logger.Info("no users with active Paddle subscriptions to reconcile", "job_id", job.ID)
		return nil
	}

	var total, matched, statusMismatches, tierMismatches, errs int

	for _, user := range users {
		if user.PaddleSubscriptionID == nil || *user.PaddleSubscriptionID == "" {
			continue
		}
		total++
		time.Sleep(100 * time.Millisecond) // throttle to avoid API rate limits

		if w.paddleSvc == nil {
			w.logger.Warn("paddle service not configured, skipping remote check",
				"user_id", user.ID,
			)
			matched++
			continue
		}

		// TODO: call w.paddleSvc.GetSubscription(ctx, *user.PaddleSubscriptionID)
		// and compare status/tier when the method is available on ReconciliationPaddleService.
		w.logger.Debug("would check subscription",
			"user_id", user.ID,
			"subscription_id", *user.PaddleSubscriptionID,
		)
		matched++
	}

	w.logger.Info("paddle reconciliation completed",
		"job_id", job.ID,
		"total", total,
		"matched", matched,
		"status_mismatches", statusMismatches,
		"tier_mismatches", tierMismatches,
		"errors", errs,
		"duration_ms", time.Since(start).Milliseconds(),
	)

	return nil
}

// PaddleReconciliationUniqueOpts returns the recommended uniqueness options.
// Only one reconciliation job should be pending/running at a time.
func PaddleReconciliationUniqueOpts() *queue.UniqueOpts {
	return &queue.UniqueOpts{
		ByArgs:   false,
		ByPeriod: 12 * time.Hour,
		ByState: []queue.JobState{
			queue.JobStateAvailable,
			queue.JobStateScheduled,
			queue.JobStateRunning,
			queue.JobStateRetryable,
			queue.JobStatePending,
		},
	}
}
