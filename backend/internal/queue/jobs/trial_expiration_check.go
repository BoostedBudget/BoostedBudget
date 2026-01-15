// Package jobs contains River job definitions and workers for async processing.
// Issue #679, #680: Trial expiration check job
package jobs

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/queue"
	"github.com/riverqueue/river"
)

// TrialExpirationCheckArgs contains the arguments for the trial expiration check job.
// This job runs periodically (daily) to find users whose trial has expired and
// expires their trial (sets status to inactive and disconnects Plaid items).
type TrialExpirationCheckArgs struct {
	// TriggerSource indicates what triggered this check (e.g., "periodic", "manual")
	TriggerSource string `json:"trigger_source,omitempty"`
}

// Kind returns the unique identifier for this job type.
func (TrialExpirationCheckArgs) Kind() string { return "trial_expiration_check" }

// TrialProfileRepository defines the methods needed for trial expiration checks.
type TrialProfileRepository interface {
	// GetUsersWithExpiredTrials returns users with subscription_status='trial' AND trial_ends_at < now
	GetUsersWithExpiredTrials(ctx context.Context) ([]*domain.User, error)
}

// TrialService defines the methods needed for trial expiration.
type TrialService interface {
	// GrantTrial grants a 60-day premium trial to a user
	GrantTrial(ctx context.Context, userID string) error
	// IsTrialActive returns true if user has an active trial
	IsTrialActive(user *domain.User) bool
	// GetTrialDaysRemaining returns days remaining in trial
	GetTrialDaysRemaining(user *domain.User) int
	// ExpireTrial expires the user's trial (sets status to inactive and disconnects Plaid items)
	ExpireTrial(ctx context.Context, userID string) error
	// CanGrantTrial returns true if user is eligible for a trial
	CanGrantTrial(user *domain.User) bool
}

// TrialExpirationCheckWorker processes trial expiration check jobs.
// This worker finds users whose trial has expired and calls ExpireTrial for each.
type TrialExpirationCheckWorker struct {
	river.WorkerDefaults[TrialExpirationCheckArgs]

	profileRepo  TrialProfileRepository
	trialService TrialService
	logger       *slog.Logger
}

// TrialExpirationCheckWorkerDeps contains the dependencies for TrialExpirationCheckWorker.
type TrialExpirationCheckWorkerDeps struct {
	ProfileRepo  TrialProfileRepository
	TrialService TrialService
	Logger       *slog.Logger
}

// NewTrialExpirationCheckWorker creates a new trial expiration check worker.
func NewTrialExpirationCheckWorker(deps TrialExpirationCheckWorkerDeps) *TrialExpirationCheckWorker {
	logger := deps.Logger
	if logger == nil {
		logger = slog.Default()
	}
	return &TrialExpirationCheckWorker{
		profileRepo:  deps.ProfileRepo,
		trialService: deps.TrialService,
		logger:       logger,
	}
}

// Work processes a trial expiration check job.
//
// The worker:
//  1. Fetches users with expired trials (subscription_status='trial' AND trial_ends_at < now)
//  2. Calls ExpireTrial for each user (sets status to inactive and disconnects Plaid items)
//
// Errors in individual expirations don't fail the entire job.
func (w *TrialExpirationCheckWorker) Work(ctx context.Context, job *river.Job[TrialExpirationCheckArgs]) error {
	args := job.Args

	w.logger.Info("starting trial expiration check",
		"job_id", job.ID,
		"trigger_source", args.TriggerSource,
		"attempt", job.Attempt,
	)

	startTime := time.Now()

	// Get users with expired trials
	users, err := w.profileRepo.GetUsersWithExpiredTrials(ctx)
	if err != nil {
		w.logger.Error("failed to fetch users with expired trials",
			"job_id", job.ID,
			"error", err,
		)
		return fmt.Errorf("failed to fetch users with expired trials: %w", err)
	}

	if len(users) == 0 {
		w.logger.Info("no users with expired trials",
			"job_id", job.ID,
			"duration_ms", time.Since(startTime).Milliseconds(),
		)
		return nil
	}

	w.logger.Info("found users with expired trials",
		"job_id", job.ID,
		"user_count", len(users),
	)

	// Expire trial for each user
	var expiredCount, errorCount int
	for _, user := range users {
		err := w.trialService.ExpireTrial(ctx, user.ID)
		if err != nil {
			w.logger.Error("failed to expire trial for user",
				"job_id", job.ID,
				"user_id", user.ID,
				"error", err,
			)
			errorCount++
			continue
		}

		w.logger.Info("expired trial for user",
			"user_id", user.ID,
		)
		expiredCount++
	}

	w.logger.Info("trial expiration check completed",
		"job_id", job.ID,
		"total_users", len(users),
		"expired", expiredCount,
		"errors", errorCount,
		"duration_ms", time.Since(startTime).Milliseconds(),
	)

	// Don't fail the job if some expirations failed - they'll be retried next check
	return nil
}

// TrialExpirationCheckUniqueOpts returns the recommended uniqueness options.
// Only one check job should run at a time to prevent duplicate expiration processing.
func TrialExpirationCheckUniqueOpts() *queue.UniqueOpts {
	return &queue.UniqueOpts{
		ByArgs: false, // All check jobs are equivalent
		// 1 hour period - typical check frequency
		ByPeriod: 1 * time.Hour,
		ByState: []queue.JobState{
			queue.JobStateAvailable,
			queue.JobStateScheduled,
			queue.JobStateRunning,
			queue.JobStateRetryable,
			queue.JobStatePending,
		},
	}
}
