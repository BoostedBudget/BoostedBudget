// Package jobs contains River job definitions and workers for async processing.
package jobs

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/queue"
	"github.com/maxnussbaum/BoostedBudget/internal/repository"
	"github.com/riverqueue/river"
)

// SubscriptionWebhookArgs contains the arguments for a subscription webhook processing job.
type SubscriptionWebhookArgs struct {
	WebhookID string `json:"webhook_id"` // ID of stored webhook event
	EventID   string `json:"event_id"`   // Paddle's unique event identifier
	EventType string `json:"event_type"` // Event type for routing
}

// Kind returns the unique identifier for this job type.
func (SubscriptionWebhookArgs) Kind() string { return "subscription_webhook" }

// PaddleWebhookRepository defines the methods needed to interact with Paddle webhook storage.
type PaddleWebhookRepository interface {
	GetByID(ctx context.Context, id string) (*domain.PaddleWebhookEvent, error)
	MarkProcessed(ctx context.Context, id string) error
	MarkFailed(ctx context.Context, id string, errorMsg string) error
}

// SubscriptionProfileRepository defines the methods needed to update user profiles for subscription webhooks.
type SubscriptionProfileRepository interface {
	GetUserByID(ctx context.Context, id string) (*domain.User, error)
	UpdatePaddleCustomerID(ctx context.Context, userID, paddleCustomerID string) error
	UpdateSubscriptionFromPaddle(ctx context.Context, userID, tier, status string, startedAt, endsAt *time.Time, subscriptionID *string) error
}

// ClerkService defines the methods needed to update Clerk metadata.
type ClerkService interface {
	UpdateUserMetadata(ctx context.Context, clerkUserID string, metadata map[string]interface{}) error
}

// SubscriptionWebhookWorker processes Paddle subscription webhook events from the queue.
type SubscriptionWebhookWorker struct {
	river.WorkerDefaults[SubscriptionWebhookArgs]

	paddleWebhookRepo PaddleWebhookRepository
	profileRepo       SubscriptionProfileRepository
	clerkService      ClerkService
	logger            *slog.Logger
}

// SubscriptionWebhookWorkerDeps contains the dependencies for SubscriptionWebhookWorker.
type SubscriptionWebhookWorkerDeps struct {
	PaddleWebhookRepo PaddleWebhookRepository
	ProfileRepo       SubscriptionProfileRepository
	ClerkService      ClerkService
	Logger            *slog.Logger
}

// NewSubscriptionWebhookWorker creates a new subscription webhook processing worker.
func NewSubscriptionWebhookWorker(deps SubscriptionWebhookWorkerDeps) *SubscriptionWebhookWorker {
	logger := deps.Logger
	if logger == nil {
		logger = slog.Default()
	}
	return &SubscriptionWebhookWorker{
		paddleWebhookRepo: deps.PaddleWebhookRepo,
		profileRepo:       deps.ProfileRepo,
		clerkService:      deps.ClerkService,
		logger:            logger,
	}
}

// parseWebhookPayload extracts the data field from webhook payload
func (w *SubscriptionWebhookWorker) parseWebhookPayload(webhook *domain.PaddleWebhookEvent) (map[string]interface{}, error) {
	var payload map[string]interface{}
	if err := json.Unmarshal(webhook.Payload, &payload); err != nil {
		return nil, fmt.Errorf("failed to parse webhook payload: %w", err)
	}

	data, ok := payload["data"].(map[string]interface{})
	if !ok {
		return nil, errors.New("webhook payload missing 'data' field")
	}
	return data, nil
}

// Work processes a subscription webhook processing job.
func (w *SubscriptionWebhookWorker) Work(ctx context.Context, job *river.Job[SubscriptionWebhookArgs]) error {
	args := job.Args

	w.logger.Info("starting subscription webhook processing job",
		"job_id", job.ID,
		"webhook_id", args.WebhookID,
		"event_id", args.EventID,
		"event_type", args.EventType,
		"attempt", job.Attempt,
	)

	startTime := time.Now()

	webhook, err := w.paddleWebhookRepo.GetByID(ctx, args.WebhookID)
	if err != nil {
		w.logger.Error("failed to fetch Paddle webhook",
			"webhook_id", args.WebhookID,
			"error", err,
		)
		if errors.Is(err, repository.ErrPaddleWebhookNotFound) {
			return nil
		}
		return fmt.Errorf("failed to fetch Paddle webhook: %w", err)
	}

	if webhook.Processed {
		w.logger.Info("Paddle webhook already processed, skipping",
			"webhook_id", args.WebhookID,
		)
		return nil
	}

	var processErr error
	switch webhook.EventType {
	case "subscription.created":
		processErr = w.processSubscriptionCreated(ctx, webhook)
	case "subscription.activated":
		processErr = w.processSubscriptionActivated(ctx, webhook)
	case "subscription.updated":
		processErr = w.processSubscriptionUpdated(ctx, webhook)
	case "subscription.canceled":
		processErr = w.processSubscriptionCanceled(ctx, webhook)
	case "subscription.paused":
		processErr = w.processSubscriptionPaused(ctx, webhook)
	case "subscription.past_due":
		processErr = w.processSubscriptionPastDue(ctx, webhook)
	case "transaction.completed":
		processErr = w.processTransactionCompleted(ctx, webhook)
	case "transaction.payment_failed":
		processErr = w.processTransactionPaymentFailed(ctx, webhook)
	default:
		w.logger.Warn("unknown Paddle event type", "event_type", webhook.EventType)
		_ = w.paddleWebhookRepo.MarkProcessed(ctx, args.WebhookID)
		return nil
	}

	if processErr != nil {
		w.logger.Error("Paddle webhook processing failed",
			"webhook_id", args.WebhookID,
			"event_type", webhook.EventType,
			"error", processErr,
			"duration_ms", time.Since(startTime).Milliseconds(),
		)
		_ = w.paddleWebhookRepo.MarkFailed(ctx, args.WebhookID, processErr.Error())
		return fmt.Errorf("webhook processing failed: %w", processErr)
	}

	if err := w.paddleWebhookRepo.MarkProcessed(ctx, args.WebhookID); err != nil {
		w.logger.Error("failed to mark webhook as processed",
			"webhook_id", args.WebhookID,
			"error", err,
		)
	}

	w.logger.Info("Paddle webhook processed successfully",
		"job_id", job.ID,
		"webhook_id", args.WebhookID,
		"event_id", args.EventID,
		"event_type", webhook.EventType,
		"duration_ms", time.Since(startTime).Milliseconds(),
	)

	return nil
}

// processSubscriptionCreated handles subscription.created events.
func (w *SubscriptionWebhookWorker) processSubscriptionCreated(ctx context.Context, webhook *domain.PaddleWebhookEvent) error {
	data, err := w.parseWebhookPayload(webhook)
	if err != nil {
		return err
	}

	customerID, _ := data["customer_id"].(string)
	customData, _ := data["custom_data"].(map[string]interface{})
	userID, _ := customData["user_id"].(string)
	// Issue #48: Extract subscription ID from webhook data
	subscriptionID, _ := data["id"].(string)

	if userID == "" || customerID == "" {
		return errors.New("webhook payload missing user_id or customer_id")
	}
	if subscriptionID == "" {
		return errors.New("webhook payload missing subscription id")
	}

	w.logger.Info("processing subscription.created",
		"user_id", userID,
		"customer_id", customerID,
		"subscription_id", subscriptionID,
	)

	if err := w.profileRepo.UpdatePaddleCustomerID(ctx, userID, customerID); err != nil {
		return fmt.Errorf("failed to update paddle_customer_id: %w", err)
	}

	var endsAt, startedAt *time.Time
	tier := "premium"

	if billingCycle, ok := data["billing_cycle"].(map[string]interface{}); ok {
		if startsAtStr, ok := billingCycle["starts_at"].(string); ok {
			if t, err := time.Parse(time.RFC3339, startsAtStr); err == nil {
				startedAt = &t
			}
		}
		if endsAtStr, ok := billingCycle["ends_at"].(string); ok {
			if t, err := time.Parse(time.RFC3339, endsAtStr); err == nil {
				endsAt = &t
			}
		}
	}

	// Issue #48: Pass subscription ID to repository for storage
	if err := w.profileRepo.UpdateSubscriptionFromPaddle(ctx, userID, tier, "active", startedAt, endsAt, &subscriptionID); err != nil {
		return fmt.Errorf("failed to update subscription: %w", err)
	}

	if w.clerkService != nil {
		user, err := w.profileRepo.GetUserByID(ctx, userID)
		if err != nil {
			return fmt.Errorf("failed to get user for Clerk update: %w", err)
		}

		metadata := map[string]interface{}{
			"subscription_tier":   tier,
			"subscription_status": "active",
		}

		if err := w.clerkService.UpdateUserMetadata(ctx, user.ClerkUserID, metadata); err != nil {
			w.logger.Error("failed to update Clerk metadata", "user_id", userID, "error", err)
		}
	}

	return nil
}

// processSubscriptionActivated handles subscription.activated events.
func (w *SubscriptionWebhookWorker) processSubscriptionActivated(ctx context.Context, webhook *domain.PaddleWebhookEvent) error {
	return w.processSubscriptionCreated(ctx, webhook)
}

// processSubscriptionUpdated handles subscription.updated events.
func (w *SubscriptionWebhookWorker) processSubscriptionUpdated(ctx context.Context, webhook *domain.PaddleWebhookEvent) error {
	data, err := w.parseWebhookPayload(webhook)
	if err != nil {
		return err
	}

	customData, _ := data["custom_data"].(map[string]interface{})
	userID, _ := customData["user_id"].(string)
	// Issue #48: Extract subscription ID from webhook data
	subscriptionID, _ := data["id"].(string)

	if userID == "" {
		return errors.New("webhook payload missing user_id")
	}

	status, _ := data["status"].(string)
	tier := "premium"

	var endsAt *time.Time
	if billingCycle, ok := data["billing_cycle"].(map[string]interface{}); ok {
		if endsAtStr, ok := billingCycle["ends_at"].(string); ok {
			if t, err := time.Parse(time.RFC3339, endsAtStr); err == nil {
				endsAt = &t
			}
		}
	}

	// Issue #48: Pass subscription ID to repository (may be empty string if not present)
	var subIDPtr *string
	if subscriptionID != "" {
		subIDPtr = &subscriptionID
	}
	if err := w.profileRepo.UpdateSubscriptionFromPaddle(ctx, userID, tier, status, nil, endsAt, subIDPtr); err != nil {
		return fmt.Errorf("failed to update subscription: %w", err)
	}

	return nil
}

// processSubscriptionCanceled handles subscription.canceled events.
func (w *SubscriptionWebhookWorker) processSubscriptionCanceled(ctx context.Context, webhook *domain.PaddleWebhookEvent) error {
	data, err := w.parseWebhookPayload(webhook)
	if err != nil {
		return err
	}

	customData, _ := data["custom_data"].(map[string]interface{})
	userID, _ := customData["user_id"].(string)

	if userID == "" {
		return errors.New("webhook payload missing user_id")
	}

	user, err := w.profileRepo.GetUserByID(ctx, userID)
	if err != nil {
		return fmt.Errorf("failed to get user: %w", err)
	}

	// Issue #48: Don't update subscription ID on cancel (keep existing ID)
	if err := w.profileRepo.UpdateSubscriptionFromPaddle(ctx, userID, user.SubscriptionTier, "cancelled", nil, nil, nil); err != nil {
		return fmt.Errorf("failed to update subscription status: %w", err)
	}

	return nil
}

// processSubscriptionPaused handles subscription.paused events.
func (w *SubscriptionWebhookWorker) processSubscriptionPaused(ctx context.Context, webhook *domain.PaddleWebhookEvent) error {
	return w.processSubscriptionStatusChange(ctx, webhook, "inactive")
}

// processSubscriptionPastDue handles subscription.past_due events.
func (w *SubscriptionWebhookWorker) processSubscriptionPastDue(ctx context.Context, webhook *domain.PaddleWebhookEvent) error {
	return w.processSubscriptionStatusChange(ctx, webhook, "past_due")
}

// processSubscriptionStatusChange is a helper for status-only updates (no subscription ID changes)
func (w *SubscriptionWebhookWorker) processSubscriptionStatusChange(ctx context.Context, webhook *domain.PaddleWebhookEvent, status string) error {
	data, err := w.parseWebhookPayload(webhook)
	if err != nil {
		return err
	}

	customData, _ := data["custom_data"].(map[string]interface{})
	userID, _ := customData["user_id"].(string)

	if userID == "" {
		return errors.New("webhook payload missing user_id")
	}

	user, err := w.profileRepo.GetUserByID(ctx, userID)
	if err != nil {
		return fmt.Errorf("failed to get user: %w", err)
	}

	if err := w.profileRepo.UpdateSubscriptionFromPaddle(ctx, userID, user.SubscriptionTier, status, nil, nil, nil); err != nil {
		return fmt.Errorf("failed to update subscription status: %w", err)
	}
	return nil
}

// processTransactionCompleted handles transaction.completed events.
func (w *SubscriptionWebhookWorker) processTransactionCompleted(ctx context.Context, webhook *domain.PaddleWebhookEvent) error {
	w.logger.Info("transaction completed", "event_id", webhook.EventID)
	return nil
}

// processTransactionPaymentFailed handles transaction.payment_failed events.
func (w *SubscriptionWebhookWorker) processTransactionPaymentFailed(ctx context.Context, webhook *domain.PaddleWebhookEvent) error {
	w.logger.Info("transaction payment failed", "event_id", webhook.EventID)
	return nil
}

// SubscriptionWebhookUniqueOpts returns the recommended uniqueness options.
// This prevents duplicate processing of the same webhook event.
func SubscriptionWebhookUniqueOpts() *queue.UniqueOpts {
	return &queue.UniqueOpts{
		ByArgs: true,
		// 10 minute period - webhooks should be processed quickly
		ByPeriod: 10 * time.Minute,
		ByState: []queue.JobState{
			queue.JobStateAvailable,
			queue.JobStateScheduled,
			queue.JobStateRunning,
			queue.JobStateRetryable,
			queue.JobStatePending,
		},
	}
}
