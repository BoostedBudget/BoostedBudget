// Package jobs contains River job definitions and workers for async processing.
package jobs

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/maxnussbaum/BoostedBudget/internal/queue"
	"github.com/riverqueue/river"
)

// NotificationEmailArgs contains the arguments for a notification email job.
// This job sends email notifications to users for various events
// (budget alerts, item errors, etc.).
type NotificationEmailArgs struct {
	// NotificationID is the ID of the notification to email
	NotificationID string `json:"notification_id"`

	// UserID is the ID of the user to send the email to
	UserID string `json:"user_id"`

	// NotificationType helps with logging and metrics
	NotificationType string `json:"notification_type"`
}

// Kind returns the unique identifier for this job type.
func (NotificationEmailArgs) Kind() string { return "notification_email" }

// EmailService interface for sending emails.
// This allows for dependency injection and testing.
type EmailService interface {
	// SendNotificationEmail sends an email for the given notification
	SendNotificationEmail(ctx context.Context, notificationID, userID string) error
}

// NotificationEmailWorker processes notification email jobs.
type NotificationEmailWorker struct {
	river.WorkerDefaults[NotificationEmailArgs]

	emailService EmailService
	logger       *slog.Logger
}

// NotificationEmailWorkerDeps contains the dependencies for NotificationEmailWorker.
type NotificationEmailWorkerDeps struct {
	EmailService EmailService
	Logger       *slog.Logger
}

// NewNotificationEmailWorker creates a new notification email worker.
func NewNotificationEmailWorker(deps NotificationEmailWorkerDeps) *NotificationEmailWorker {
	logger := deps.Logger
	if logger == nil {
		logger = slog.Default()
	}
	return &NotificationEmailWorker{
		emailService: deps.EmailService,
		logger:       logger,
	}
}

// Work processes a notification email job.
//
// The worker:
//  1. Retrieves the notification details
//  2. Sends the email via the configured email provider
//  3. Updates the notification as email sent
//
// Transient failures (network, provider errors) will be retried.
// Permanent failures (invalid email, notification not found) won't be retried.
func (w *NotificationEmailWorker) Work(ctx context.Context, job *river.Job[NotificationEmailArgs]) error {
	args := job.Args

	w.logger.Info("starting notification email job",
		"job_id", job.ID,
		"notification_id", args.NotificationID,
		"user_id", args.UserID,
		"notification_type", args.NotificationType,
		"attempt", job.Attempt,
	)

	startTime := time.Now()

	err := w.emailService.SendNotificationEmail(ctx, args.NotificationID, args.UserID)
	if err != nil {
		w.logger.Error("notification email failed",
			"job_id", job.ID,
			"notification_id", args.NotificationID,
			"user_id", args.UserID,
			"error", err,
			"duration_ms", time.Since(startTime).Milliseconds(),
		)
		return fmt.Errorf("failed to send notification email: %w", err)
	}

	w.logger.Info("notification email sent",
		"job_id", job.ID,
		"notification_id", args.NotificationID,
		"user_id", args.UserID,
		"notification_type", args.NotificationType,
		"duration_ms", time.Since(startTime).Milliseconds(),
	)

	return nil
}

// NotificationEmailUniqueOpts returns the recommended uniqueness options.
// This prevents duplicate emails for the same notification within a 5-minute window.
func NotificationEmailUniqueOpts() *queue.UniqueOpts {
	return &queue.UniqueOpts{
		ByArgs:   true,
		ByPeriod: 5 * time.Minute,
		ByState: []queue.JobState{
			queue.JobStateAvailable,
			queue.JobStateScheduled,
			queue.JobStateRunning,
			queue.JobStateRetryable,
			queue.JobStatePending,
			queue.JobStateCompleted, // Don't resend completed emails
		},
	}
}
