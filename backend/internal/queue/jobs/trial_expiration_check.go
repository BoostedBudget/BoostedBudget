// Package jobs contains River job definitions and workers for async processing.
// Issue #679, #680: Trial expiration check job
package jobs

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/observability"
	"github.com/maxnussbaum/BoostedBudget/internal/queue"
	"github.com/riverqueue/river"
)

// TrialExpirationCheckArgs contains the arguments for the trial expiration check job.
// This job runs periodically (daily) to find users whose trial has expired and expires their
// trial (sets status to inactive and starts the retention clock). Issue #1413 round-7: it does
// NOT disconnect Plaid — that is deferred to RetentionCleanupCheckWorker's grace-window repair
// pass, which runs only after retention_started_at (set here) is old enough. See
// TrialService.ExpireTrial's doc comment for why.
type TrialExpirationCheckArgs struct {
	// TriggerSource indicates what triggered this check (e.g., "periodic", "manual")
	TriggerSource string `json:"trigger_source,omitempty"`
}

// RetiredSubscriptionWorker drains jobs queued before RevenueCat became the access
// authority. Keeping the original kinds registered avoids endless unknown-worker
// retries; these jobs must never invoke their former access or cleanup services.
type RetiredSubscriptionWorker[T river.JobArgs] struct {
	river.WorkerDefaults[T]
}

func (*RetiredSubscriptionWorker[T]) Work(context.Context, *river.Job[T]) error {
	return nil
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
	// ExpireTrial expires the user's trial (sets status to inactive and starts the retention
	// clock — see trial_service.go's ExpireTrial doc comment for why it does not disconnect Plaid)
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
//  2. Calls ExpireTrial for each user (sets status to inactive and starts the retention clock —
//     the Plaid disconnect happens later, via RetentionCleanupCheckWorker's grace-window repair)
//
// Errors in individual expirations don't fail the entire job.
func (w *TrialExpirationCheckWorker) Work(ctx context.Context, job *river.Job[TrialExpirationCheckArgs]) (err error) {
	args := job.Args

	w.logger.Info("starting trial expiration check",
		"job_id", job.ID,
		"trigger_source", args.TriggerSource,
		"attempt", job.Attempt,
	)

	start := time.Now()
	defer func() {
		status := "success"
		if err != nil {
			status = "failed"
		} else {
			observability.RecordJobSuccess(job.Kind) // Issue #1413 P4: last-success heartbeat
		}
		observability.JobsCompleted.WithLabelValues(job.Kind, job.Queue, status).Inc()
		observability.JobDuration.WithLabelValues(job.Kind).Observe(time.Since(start).Seconds())
	}()

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
		expireErr := w.trialService.ExpireTrial(ctx, user.ID)
		if expireErr != nil {
			w.logger.Error("failed to expire trial for user",
				"job_id", job.ID,
				"user_id", user.ID,
				"error", expireErr,
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
			// Completed is REQUIRED because this job is registered with RunOnStart: true
			// (Issue #1448). Without it, a run that COMPLETED minutes ago does not
			// deduplicate the start-of-process catch-up insert, so every deploy or leader
			// election enqueues a redundant duplicate. Matches the two jobs that already
			// had RunOnStart: true before #1448 (RetentionCleanupCheckUniqueOpts,
			// ManualTierOverrideExpiryCheckUniqueOpts). Found by the final branch-wide
			// Codex review.
			queue.JobStateCompleted,
		},
	}
}

// TrialExpirationCheckInsertOpts returns the *river.InsertOpts this job's periodic
// registration in main.go must use — Queue: "low" plus the uniqueness options from
// TrialExpirationCheckUniqueOpts(), converted to river's UniqueOpts type.
//
// Issue #1448: main.go's periodic registration previously built a bare
// &river.InsertOpts{Queue: "low"} and never called TrialExpirationCheckUniqueOpts() at
// all — even though its PeriodicJobOpts already set RunOnStart: true, the unsafe
// RunOnStart-without-dedup combination this package's retention/manual-tier helpers
// document explicitly.
func TrialExpirationCheckInsertOpts() *river.InsertOpts {
	return riverInsertOptsFromUnique(queue.QueueLow, TrialExpirationCheckUniqueOpts())
}

// TrialExpirationCheckPeriodicOpts returns the *river.PeriodicJobOpts this job's periodic
// registration in main.go must use. RunOnStart: true is only safe because
// TrialExpirationCheckInsertOpts() above always carries TrialExpirationCheckUniqueOpts()'s
// dedup. ID is set so River restores nextRunAt across restarts/leader elections instead of
// recomputing now+interval every time — Issue #1448.
func TrialExpirationCheckPeriodicOpts() *river.PeriodicJobOpts {
	return &river.PeriodicJobOpts{RunOnStart: true, ID: TrialExpirationCheckArgs{}.Kind()}
}
