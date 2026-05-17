// Package jobs contains River job definitions and workers for async processing.
// Issue #930: Retention cleanup check — 6-month auto-deletion of inactive/cancelled accounts.
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

// RetentionCleanupCheckArgs contains the arguments for the retention cleanup check job.
// This job runs daily to:
//  1. Send 30-day pre-deletion warning emails
//  2. Send 7-day pre-deletion warning emails
//  3. Auto-delete users whose retention period has expired
type RetentionCleanupCheckArgs struct {
	// TriggerSource indicates what triggered this check (e.g., "periodic", "manual")
	TriggerSource string `json:"trigger_source,omitempty"`
}

// Kind returns the unique identifier for this job type.
func (RetentionCleanupCheckArgs) Kind() string { return "retention_cleanup_check" }

// RetentionProfileRepository defines the repository methods needed for retention cleanup.
type RetentionProfileRepository interface {
	// GetUsersNeedingRetention30dNotice returns users whose retention expires within 30 days and 30d notice not sent.
	GetUsersNeedingRetention30dNotice(ctx context.Context) ([]*domain.User, error)
	// GetUsersNeedingRetention7dNotice returns users whose retention expires within 7 days and 7d notice not sent.
	GetUsersNeedingRetention7dNotice(ctx context.Context) ([]*domain.User, error)
	// GetUsersWithExpiredRetention returns users whose retention_expires_at has passed and are not deleted/pending.
	GetUsersWithExpiredRetention(ctx context.Context) ([]*domain.User, error)
	// SetRetentionNotice30dSent marks the 30-day notice as sent for a user.
	SetRetentionNotice30dSent(ctx context.Context, userID string) error
	// SetRetentionNotice7dSent marks the 7-day notice as sent for a user.
	SetRetentionNotice7dSent(ctx context.Context, userID string) error
}

// RetentionDeletionService defines the deletion methods needed for retention cleanup.
type RetentionDeletionService interface {
	// ExecuteDeletion executes the full deletion process for a user.
	ExecuteDeletion(ctx context.Context, userID string) error
}

// RetentionEmailService defines the email methods needed for retention notices.
type RetentionEmailService interface {
	// SendRetentionNoticeEmail sends a retention notice email to a user.
	SendRetentionNoticeEmail(ctx context.Context, toEmail, subject, body string) error
}

// RetentionCleanupCheckWorker processes retention cleanup check jobs.
// It handles the daily lifecycle of inactive/cancelled accounts:
//   - 30d before expiry: warn user via email
//   - 7d before expiry: final warning via email
//   - After expiry: auto-delete account data
type RetentionCleanupCheckWorker struct {
	river.WorkerDefaults[RetentionCleanupCheckArgs]

	profileRepo     RetentionProfileRepository
	deletionService RetentionDeletionService
	emailService    RetentionEmailService
	retentionMonths int
	logger          *slog.Logger
}

// RetentionCleanupCheckWorkerDeps contains the dependencies for RetentionCleanupCheckWorker.
type RetentionCleanupCheckWorkerDeps struct {
	ProfileRepo     RetentionProfileRepository
	DeletionService RetentionDeletionService
	EmailService    RetentionEmailService
	RetentionMonths int
	Logger          *slog.Logger
}

// NewRetentionCleanupCheckWorker creates a new retention cleanup check worker.
func NewRetentionCleanupCheckWorker(deps RetentionCleanupCheckWorkerDeps) *RetentionCleanupCheckWorker {
	logger := deps.Logger
	if logger == nil {
		logger = slog.Default()
	}
	retentionMonths := deps.RetentionMonths
	if retentionMonths <= 0 {
		retentionMonths = 6 // Default: 6-month retention period
	}
	return &RetentionCleanupCheckWorker{
		profileRepo:     deps.ProfileRepo,
		deletionService: deps.DeletionService,
		emailService:    deps.EmailService,
		retentionMonths: retentionMonths,
		logger:          logger,
	}
}

// Work processes a retention cleanup check job.
//
// The worker:
//  1. Finds users needing 30-day notice → sends email → marks notice sent
//  2. Finds users needing 7-day notice → sends email → marks notice sent
//  3. Finds users with expired retention → calls ExecuteDeletion for each
//
// Partial deletion failures cause the job to return an error so River retries the run.
func (w *RetentionCleanupCheckWorker) Work(ctx context.Context, job *river.Job[RetentionCleanupCheckArgs]) (err error) {
	args := job.Args

	w.logger.Info("starting retention cleanup check",
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

	// Step 1: Send 30-day pre-deletion warnings
	if notice30Err := w.send30dNotices(ctx, job.ID); notice30Err != nil {
		w.logger.Error("failed to process 30d retention notices",
			"job_id", job.ID,
			"error", notice30Err,
		)
		// Don't fail job — continue with other steps
	}

	// Step 2: Send 7-day pre-deletion warnings
	if notice7Err := w.send7dNotices(ctx, job.ID); notice7Err != nil {
		w.logger.Error("failed to process 7d retention notices",
			"job_id", job.ID,
			"error", notice7Err,
		)
		// Don't fail job — continue with other steps
	}

	// Step 3: Auto-delete users with expired retention
	// Propagate partial failure so River retries — failed users will be picked up again
	if deleteErr := w.deleteExpiredAccounts(ctx, job.ID); deleteErr != nil {
		w.logger.Error("failed to process expired retention deletions",
			"job_id", job.ID,
			"error", deleteErr,
		)
		err = deleteErr
	}

	w.logger.Info("retention cleanup check completed",
		"job_id", job.ID,
		"duration_ms", time.Since(start).Milliseconds(),
	)

	return err
}

// send30dNotices finds users needing 30-day warning and sends emails.
func (w *RetentionCleanupCheckWorker) send30dNotices(ctx context.Context, jobID int64) error {
	users, err := w.profileRepo.GetUsersNeedingRetention30dNotice(ctx)
	if err != nil {
		return fmt.Errorf("failed to fetch users needing 30d notice: %w", err)
	}

	if len(users) == 0 {
		w.logger.Info("no users needing 30d retention notice", "job_id", jobID)
		return nil
	}

	w.logger.Info("found users needing 30d retention notice", "job_id", jobID, "user_count", len(users))

	const subject = "Your BoostedBudget data will be deleted in 30 days"
	const body = "Your account data is preserved in read-only mode. Your data will be permanently deleted in 30 days unless you resubscribe. Visit boostedbudget.com/pricing to restore full access."

	var sentCount, errorCount int
	for _, user := range users {
		if sendErr := w.sendNoticeEmail(ctx, user, subject, body); sendErr != nil {
			w.logger.Warn("failed to send 30d retention notice",
				"job_id", jobID, "user_id", user.ID, "error", sendErr)
			errorCount++
			continue
		}

		if markErr := w.profileRepo.SetRetentionNotice30dSent(ctx, user.ID); markErr != nil {
			w.logger.Warn("failed to mark 30d retention notice sent",
				"job_id", jobID, "user_id", user.ID, "error", markErr)
			errorCount++
			continue
		}

		sentCount++
	}

	w.logger.Info("30d retention notices processed",
		"job_id", jobID, "sent", sentCount, "errors", errorCount)
	return nil
}

// send7dNotices finds users needing 7-day warning and sends emails.
func (w *RetentionCleanupCheckWorker) send7dNotices(ctx context.Context, jobID int64) error {
	users, err := w.profileRepo.GetUsersNeedingRetention7dNotice(ctx)
	if err != nil {
		return fmt.Errorf("failed to fetch users needing 7d notice: %w", err)
	}

	if len(users) == 0 {
		w.logger.Info("no users needing 7d retention notice", "job_id", jobID)
		return nil
	}

	w.logger.Info("found users needing 7d retention notice", "job_id", jobID, "user_count", len(users))

	const subject = "Final notice: BoostedBudget data deletion in 7 days"
	const body = "This is your final warning. Your BoostedBudget account data will be permanently deleted in 7 days. Resubscribe at boostedbudget.com/pricing to keep your data."

	var sentCount, errorCount int
	for _, user := range users {
		if sendErr := w.sendNoticeEmail(ctx, user, subject, body); sendErr != nil {
			w.logger.Warn("failed to send 7d retention notice",
				"job_id", jobID, "user_id", user.ID, "error", sendErr)
			errorCount++
			continue
		}

		if markErr := w.profileRepo.SetRetentionNotice7dSent(ctx, user.ID); markErr != nil {
			w.logger.Warn("failed to mark 7d retention notice sent",
				"job_id", jobID, "user_id", user.ID, "error", markErr)
			errorCount++
			continue
		}

		sentCount++
	}

	w.logger.Info("7d retention notices processed",
		"job_id", jobID, "sent", sentCount, "errors", errorCount)
	return nil
}

// deleteExpiredAccounts finds users with expired retention and executes deletion.
func (w *RetentionCleanupCheckWorker) deleteExpiredAccounts(ctx context.Context, jobID int64) error {
	users, err := w.profileRepo.GetUsersWithExpiredRetention(ctx)
	if err != nil {
		return fmt.Errorf("failed to fetch users with expired retention: %w", err)
	}

	if len(users) == 0 {
		w.logger.Info("no users with expired retention", "job_id", jobID)
		return nil
	}

	w.logger.Info("found users with expired retention", "job_id", jobID, "user_count", len(users))

	var deletedCount, errorCount int
	for _, user := range users {
		if deleteErr := w.deletionService.ExecuteDeletion(ctx, user.ID); deleteErr != nil {
			w.logger.Error("failed to execute retention deletion",
				"job_id", jobID, "user_id", user.ID, "error", deleteErr)
			errorCount++
			continue
		}

		w.logger.Info("retention deletion executed", "job_id", jobID, "user_id", user.ID)
		deletedCount++
	}

	w.logger.Info("retention deletions processed",
		"job_id", jobID, "deleted", deletedCount, "errors", errorCount)
	if errorCount > 0 {
		return fmt.Errorf("retention deletion: %d of %d deletions failed (see logs for details)", errorCount, len(users))
	}
	return nil
}

// sendNoticeEmail sends a retention notice email to the user.
// If emailService is nil or email is empty, the send is skipped (no-op safe).
func (w *RetentionCleanupCheckWorker) sendNoticeEmail(ctx context.Context, user *domain.User, subject, body string) error {
	if w.emailService == nil {
		return nil // No email service configured — skip sending
	}
	if user.Email == "" {
		w.logger.Warn("user has no email, skipping retention notice", "user_id", user.ID)
		return nil
	}
	return w.emailService.SendRetentionNoticeEmail(ctx, user.Email, subject, body)
}

// RetentionCleanupCheckUniqueOpts returns the recommended uniqueness options.
// Only one check job should run at a time to prevent duplicate deletion processing.
func RetentionCleanupCheckUniqueOpts() *queue.UniqueOpts {
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
