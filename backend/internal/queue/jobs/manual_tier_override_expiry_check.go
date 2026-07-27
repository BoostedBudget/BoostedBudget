// Package jobs contains River job definitions and workers for async processing.
// Issue #1413 Task 9: Manual tier override expiry check — the time-triggered twin of the admin
// "remove override" endpoint (Issue #1414 / Task 7). HasActiveManualOverride() derives comp
// state at READ time only: once manual_tier_ends_at passes, reads simply start reporting the
// base tier/status again, but nothing fires to (1) re-arm a retention clock a comp had paused,
// (2) re-sync Clerk, or (3) audit the lapse. This job closes that gap by running the SAME
// idempotent transition RemoveUserTierOverride performs for admin-initiated removal, once a
// day, for every override that has lapsed on its own.
package jobs

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/observability"
	"github.com/maxnussbaum/BoostedBudget/internal/queue"
	"github.com/maxnussbaum/BoostedBudget/internal/repository"
	"github.com/riverqueue/river"
)

// expireManualTierOverrideReason is the audit-log "reason" recorded for every system-initiated
// expiry, distinguishing it (alongside the NULL actor) from an admin-supplied reason string.
const expireManualTierOverrideReason = "manual tier override expired"

// ManualTierOverrideExpiryCheckArgs contains the arguments for the manual tier override expiry
// check job. This job runs daily to find users whose manual_tier_override has lapsed
// (manual_tier_ends_at has passed) and perform the same clear/re-arm-retention/audit transition
// the admin "remove override" endpoint performs for a manual removal.
type ManualTierOverrideExpiryCheckArgs struct {
	// TriggerSource indicates what triggered this check (e.g., "periodic", "manual")
	TriggerSource string `json:"trigger_source,omitempty"`
}

// Kind returns the unique identifier for this job type.
func (ManualTierOverrideExpiryCheckArgs) Kind() string { return "manual_tier_override_expiry_check" }

// ManualTierOverrideProfileRepository defines the repository method needed to find users whose
// manual tier override has expired.
type ManualTierOverrideProfileRepository interface {
	// GetUsersWithExpiredManualTierOverride returns users whose manual_tier_override is set and
	// whose manual_tier_ends_at has strictly passed.
	GetUsersWithExpiredManualTierOverride(ctx context.Context) ([]*domain.User, error)
}

// ManualTierOverrideRemover is the single admin-repository method this job reuses to perform the
// exact same idempotent clear-override/re-arm-retention/audit transition
// AdminUserRepository.RemoveUserTierOverride implements for admin-initiated removal (Issue #1414
// / Task 7) — this job is the time-triggered twin, not a new policy, so it calls the same method
// rather than duplicating its CTE.
//
// Known, deliberate limitation (documented rather than worked around by duplicating SQL): the
// underlying query hardcodes the audit action as 'remove_manual_tier'. An expiry-specific action
// (e.g. 'expire_manual_tier') would require adding an action parameter to
// AdminUserRepository.RemoveUserTierOverride, which lives in admin_user_repository.go — a file
// owned by a different, concurrently-in-flight task in this plan round, and out of this job's
// scope fence. Every automatic expiry therefore audits as 'remove_manual_tier', distinguishable
// from an admin-initiated removal only by its NULL actor (see expireManualTierOverrideReason).
type ManualTierOverrideRemover interface {
	RemoveUserTierOverride(ctx context.Context, actorUserID, targetUserID, reason string, retentionMonths int) (repository.RemoveTierOverrideRow, error)
}

// ManualTierOverrideExpiryCheckWorker processes manual tier override expiry check jobs.
type ManualTierOverrideExpiryCheckWorker struct {
	river.WorkerDefaults[ManualTierOverrideExpiryCheckArgs]

	profileRepo     ManualTierOverrideProfileRepository
	remover         ManualTierOverrideRemover
	clerkService    ClerkService
	retentionMonths int
	logger          *slog.Logger
}

// ManualTierOverrideExpiryCheckWorkerDeps contains the dependencies for
// ManualTierOverrideExpiryCheckWorker.
type ManualTierOverrideExpiryCheckWorkerDeps struct {
	ProfileRepo     ManualTierOverrideProfileRepository
	Remover         ManualTierOverrideRemover
	ClerkService    ClerkService // optional — nil-safe, matches SubscriptionWebhookWorker's convention
	RetentionMonths int
	Logger          *slog.Logger
}

// NewManualTierOverrideExpiryCheckWorker creates a new manual tier override expiry check worker.
func NewManualTierOverrideExpiryCheckWorker(deps ManualTierOverrideExpiryCheckWorkerDeps) *ManualTierOverrideExpiryCheckWorker {
	logger := deps.Logger
	if logger == nil {
		logger = slog.Default()
	}
	retentionMonths := deps.RetentionMonths
	if retentionMonths <= 0 {
		retentionMonths = 6 // Default: 6-month retention period
	}
	return &ManualTierOverrideExpiryCheckWorker{
		profileRepo:     deps.ProfileRepo,
		remover:         deps.Remover,
		clerkService:    deps.ClerkService,
		retentionMonths: retentionMonths,
		logger:          logger,
	}
}

// Work processes a manual tier override expiry check job.
//
// For each candidate returned by the repository query:
//  1. Belt-and-braces guard: re-check domain.User.HasActiveManualOverride() (mirrors
//     retention_cleanup_check.go's comped-user guard) — protects against a stale candidate list
//     when an admin extends/re-applies the override between the query and this loop, and keeps
//     boundary agreement with the read path (an override ending exactly now is still active).
//  2. Call the shared RemoveUserTierOverride repository method with a system (empty) actor,
//     which the callee coerces to SQL NULL.
//  3. Best-effort re-sync Clerk to the reverted tier/status.
//
// One user's failure is logged and does not stop the batch; a non-empty error count causes the
// job to return an error so River retries (failed users are naturally re-selected next run).
// A second pass over an already-cleared user is a no-op: the repository query no longer returns
// them (manual_tier_override is NULL), and even a concurrent duplicate call to
// RemoveUserTierOverride is itself idempotent (FOR UPDATE-locked, Removed=false on a no-op).
func (w *ManualTierOverrideExpiryCheckWorker) Work(ctx context.Context, job *river.Job[ManualTierOverrideExpiryCheckArgs]) (err error) {
	args := job.Args

	w.logger.Info("starting manual tier override expiry check",
		"job_id", job.ID,
		"trigger_source", args.TriggerSource,
		"attempt", job.Attempt,
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

	users, fetchErr := w.profileRepo.GetUsersWithExpiredManualTierOverride(ctx)
	if fetchErr != nil {
		err = fmt.Errorf("failed to fetch users with expired manual tier override: %w", fetchErr)
		w.logger.Error("failed to fetch users with expired manual tier override",
			"job_id", job.ID, "error", fetchErr)
		return err
	}

	if len(users) == 0 {
		w.logger.Info("no users with expired manual tier override", "job_id", job.ID)
		return nil
	}

	w.logger.Info("found users with expired manual tier override", "job_id", job.ID, "user_count", len(users))

	var expiredCount, errorCount int
	for _, user := range users {
		if user.HasActiveManualOverride() {
			w.logger.Info("skipping manual tier override expiry — override is still active",
				"job_id", job.ID, "user_id", user.ID)
			continue
		}

		row, removeErr := w.remover.RemoveUserTierOverride(ctx, "", user.ID, expireManualTierOverrideReason, w.retentionMonths)
		if removeErr != nil {
			w.logger.Error("failed to expire manual tier override",
				"job_id", job.ID, "user_id", user.ID, "error", removeErr)
			errorCount++
			continue
		}

		w.syncClerkMetadata(ctx, job.ID, user.ID, row)
		expiredCount++
	}

	w.logger.Info("manual tier override expiry processed",
		"job_id", job.ID, "expired", expiredCount, "errors", errorCount)
	if errorCount > 0 {
		err = fmt.Errorf("manual tier override expiry: %d of %d expirations failed (see logs for details)", errorCount, len(users))
	}
	return err
}

// syncClerkMetadata best-effort re-syncs Clerk to the reverted tier/status returned by
// RemoveUserTierOverride. Logs and swallows errors — matches
// AdminUserService.RemoveUserTierOverride's own sync semantics (Issue #1413 Task 7), and this
// package's existing nil-safe ClerkService convention (SubscriptionWebhookWorker).
func (w *ManualTierOverrideExpiryCheckWorker) syncClerkMetadata(ctx context.Context, jobID int64, userID string, row repository.RemoveTierOverrideRow) {
	if w.clerkService == nil || row.ClerkUserID == "" {
		return
	}
	scratch := &domain.User{
		SubscriptionTier:   row.RevertedTier,
		SubscriptionStatus: row.RevertedStatus,
	}
	if syncErr := w.clerkService.UpdateUserMetadata(ctx, row.ClerkUserID, scratch.ClerkPublicMetadata()); syncErr != nil {
		w.logger.Warn("failed to sync reverted tier to clerk metadata",
			"job_id", jobID, "user_id", userID, "clerk_user_id", row.ClerkUserID, "error", syncErr)
	}
}

// ManualTierOverrideExpiryCheckUniqueOpts returns the recommended uniqueness options.
// Only one check job should run at a time to prevent duplicate expiry processing.
func ManualTierOverrideExpiryCheckUniqueOpts() *queue.UniqueOpts {
	return &queue.UniqueOpts{
		ByArgs: false, // All check jobs are equivalent
		// 23 hours period — avoids overlap on daily schedule
		ByPeriod: 23 * time.Hour,
		ByState: []queue.JobState{
			queue.JobStateAvailable,
			queue.JobStateScheduled,
			queue.JobStateRunning,
			queue.JobStateRetryable,
			queue.JobStatePending,
		},
	}
}
