// Package jobs contains River job definitions and workers for async processing.
// Issue #762: Paddle reconciliation job
package jobs

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	paddle "github.com/PaddleHQ/paddle-go-sdk/v4"
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
	UpdateSubscriptionFromPaddle(ctx context.Context, userID, tier, status string, startedAt, endsAt *time.Time, subscriptionID *string) error
}

// ReconciliationPaddleService is the narrow interface needed for reconciliation.
type ReconciliationPaddleService interface {
	GetSubscription(ctx context.Context, subscriptionID string) (*paddle.Subscription, error)
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

// normalizePaddleStatus maps Paddle's "canceled" (single-l) to the domain's "cancelled" (double-l).
// All other statuses pass through unchanged.
func normalizePaddleStatus(paddleStatus paddle.SubscriptionStatus) string {
	if paddleStatus == paddle.SubscriptionStatusCanceled {
		return string(domain.SubscriptionStatusCancelled)
	}
	return string(paddleStatus)
}

// resolveTierFromSubscription returns the tier name corresponding to the first
// price item in the Paddle subscription. Returns "" if the tier cannot be determined.
func (w *PaddleReconciliationWorker) resolveTierFromSubscription(sub *paddle.Subscription) string {
	if w.tierConfig == nil {
		return ""
	}
	if len(sub.Items) == 0 {
		return ""
	}
	priceID := sub.Items[0].Price.ID
	if priceID == "" {
		return ""
	}
	tier, err := w.tierConfig.GetTierByPriceID(priceID)
	if err != nil {
		w.logger.Warn("price ID not found in tier config, skipping tier check",
			"price_id", priceID,
		)
		return ""
	}
	return string(tier)
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
		// throttle to avoid API rate limits; respect context cancellation
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(100 * time.Millisecond):
		}

		if w.paddleSvc == nil {
			w.logger.Warn("paddle service not configured, skipping remote check",
				"user_id", user.ID,
			)
			matched++
			continue
		}

		sub, err := w.paddleSvc.GetSubscription(ctx, *user.PaddleSubscriptionID)
		if err != nil {
			w.logger.Error("failed to get subscription from Paddle",
				"user_id", user.ID,
				"subscription_id", *user.PaddleSubscriptionID,
				"error", err,
			)
			errs++
			continue
		}

		remoteStatus := normalizePaddleStatus(sub.Status)
		remoteTier := w.resolveTierFromSubscription(sub)

		statusMatch := remoteStatus == user.SubscriptionStatus
		tierMatch := remoteTier == "" || remoteTier == user.SubscriptionTier

		if statusMatch && tierMatch {
			matched++
			continue
		}

		if !statusMatch {
			statusMismatches++
			w.logger.Warn("subscription status mismatch",
				"user_id", user.ID,
				"local_status", user.SubscriptionStatus,
				"remote_status", remoteStatus,
			)
		}
		if !tierMatch {
			tierMismatches++
			w.logger.Warn("subscription tier mismatch",
				"user_id", user.ID,
				"local_tier", user.SubscriptionTier,
				"remote_tier", remoteTier,
			)
		}

		if w.autoFix {
			fixStatus := user.SubscriptionStatus
			if !statusMatch {
				fixStatus = remoteStatus
			}
			fixTier := user.SubscriptionTier
			if !tierMatch {
				fixTier = remoteTier
			}
			subID := *user.PaddleSubscriptionID
			if updateErr := w.profileRepo.UpdateSubscriptionFromPaddle(ctx, user.ID, fixTier, fixStatus, nil, nil, &subID); updateErr != nil {
				w.logger.Error("failed to auto-fix subscription from Paddle",
					"user_id", user.ID,
					"error", updateErr,
				)
				errs++
			}
		}
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
