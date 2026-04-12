package jobs

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	phclient "github.com/maxnussbaum/BoostedBudget/internal/integration/posthog"
	"github.com/riverqueue/river"
)

// EmailChangePropagationArgs contains the arguments for an email change propagation job.
// Email is intentionally NOT stored here to avoid plaintext PII in the river_job table.
// The worker fetches the current email from the encrypted users table at execution time,
// which also makes concurrent jobs naturally idempotent.
type EmailChangePropagationArgs struct {
	UserID      string `json:"user_id"`
	ClerkUserID string `json:"clerk_user_id"`
}

func (EmailChangePropagationArgs) Kind() string { return "email_change_propagation" }

// EmailPropagationProfileRepo defines the profile repo methods needed by this worker.
type EmailPropagationProfileRepo interface {
	GetUserByID(ctx context.Context, id string) (*domain.User, error)
}

// EmailPropagationClerkService defines the Clerk methods needed by this worker.
type EmailPropagationClerkService interface {
	UpdateUserEmail(ctx context.Context, clerkUserID, newEmail string) error
}

// EmailPropagationPaddleService defines the Paddle methods needed by this worker.
type EmailPropagationPaddleService interface {
	UpdateCustomerEmail(ctx context.Context, customerID, email string) error
}

// EmailChangePropagationWorker propagates email changes to Clerk and Paddle.
type EmailChangePropagationWorker struct {
	river.WorkerDefaults[EmailChangePropagationArgs]

	profileRepo   EmailPropagationProfileRepo
	clerkService  EmailPropagationClerkService
	paddleService EmailPropagationPaddleService
	analytics     phclient.Analytics // Optional PostHog analytics client
	logger        *slog.Logger
}

// EmailChangePropagationWorkerDeps contains dependencies for EmailChangePropagationWorker.
type EmailChangePropagationWorkerDeps struct {
	ProfileRepo   EmailPropagationProfileRepo
	ClerkService  EmailPropagationClerkService
	PaddleService EmailPropagationPaddleService
	Logger        *slog.Logger
}

// NewEmailChangePropagationWorker creates a new email change propagation worker.
func NewEmailChangePropagationWorker(deps EmailChangePropagationWorkerDeps) *EmailChangePropagationWorker {
	logger := deps.Logger
	if logger == nil {
		logger = slog.Default()
	}
	return &EmailChangePropagationWorker{
		profileRepo:   deps.ProfileRepo,
		clerkService:  deps.ClerkService,
		paddleService: deps.PaddleService,
		logger:        logger,
	}
}

// SetAnalytics injects the analytics client for capturing email change events.
func (w *EmailChangePropagationWorker) SetAnalytics(a phclient.Analytics) {
	w.analytics = a
}

// Work processes the email change propagation job.
func (w *EmailChangePropagationWorker) Work(ctx context.Context, job *river.Job[EmailChangePropagationArgs]) error {
	args := job.Args

	user, err := w.profileRepo.GetUserByID(ctx, args.UserID)
	if err != nil {
		return fmt.Errorf("email propagation: failed to fetch user %s: %w", args.UserID, err)
	}

	// Propagate the current DB email to external systems. Fetching from DB at execution
	// time avoids plaintext PII in river_job table and makes concurrent jobs idempotent.
	if err := w.clerkService.UpdateUserEmail(ctx, args.ClerkUserID, user.Email); err != nil {
		return fmt.Errorf("email propagation: Clerk update failed for user %s: %w", args.UserID, err)
	}

	// Propagate to Paddle only if service configured and user has a Paddle customer record.
	if w.paddleService != nil && user.PaddleCustomerID != nil && *user.PaddleCustomerID != "" {
		if err := w.paddleService.UpdateCustomerEmail(ctx, *user.PaddleCustomerID, user.Email); err != nil {
			return fmt.Errorf("email propagation: Paddle update failed for user %s: %w", args.UserID, err)
		}
	}

	if w.analytics != nil {
		w.analytics.Capture(args.UserID, phclient.EventEmailChanged, map[string]interface{}{
			"propagated_to_clerk":  true,
			"propagated_to_paddle": w.paddleService != nil && user.PaddleCustomerID != nil && *user.PaddleCustomerID != "",
		})
	}

	w.logger.Info("email_change_propagation: completed", "user_id", args.UserID)
	return nil
}
