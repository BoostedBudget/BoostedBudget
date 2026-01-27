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
// This job processes Paddle subscription webhooks that have been stored in the database.
//
// Issue #46: Paddle Webhook Handler Implementation
type SubscriptionWebhookArgs struct {
	// WebhookID is the ID of the stored webhook event to process (our UUID)
	WebhookID string `json:"webhook_id"`

	// EventID is Paddle's unique event identifier
	EventID string `json:"event_id"`

	// EventType helps with routing to the correct processor
	// Examples: subscription.created, subscription.updated, transaction.completed
	EventType string `json:"event_type"`
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
	UpdateSubscriptionFromPaddle(ctx context.Context, userID, tier, status string, startedAt, endsAt *time.Time) error
}

// ClerkService defines the methods needed to update Clerk metadata.
type ClerkService interface {
	UpdateUserMetadata(ctx context.Context, clerkUserID string, metadata map[string]interface{}) error
}

// SubscriptionWebhookWorker processes Paddle subscription webhook events from the queue.
//
// Issue #46: Paddle Webhook Handler Implementation
// Handles subscription lifecycle events and updates user subscription status.
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
//
// The worker:
//  1. Fetches the webhook from the database
//  2. Routes to the appropriate processor based on event type
//  3. Updates user subscription status and Clerk metadata
//  4. Marks webhook as processed or failed
//  5. Returns errors for retry (temporary) or cancellation (permanent)
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

	// Fetch webhook from database
	webhook, err := w.paddleWebhookRepo.GetByID(ctx, args.WebhookID)
	if err != nil {
		w.logger.Error("failed to fetch Paddle webhook",
			"job_id", job.ID,
			"webhook_id", args.WebhookID,
			"error", err,
		)
		// If webhook not found, don't retry (permanent failure)
		if errors.Is(err, repository.ErrPaddleWebhookNotFound) {
			return nil // Cancel job - webhook doesn't exist
		}
		return fmt.Errorf("failed to fetch Paddle webhook: %w", err)
	}

	// Skip if already processed (idempotency)
	if webhook.Processed {
		w.logger.Info("Paddle webhook already processed, skipping",
			"job_id", job.ID,
			"webhook_id", args.WebhookID,
			"event_id", args.EventID,
		)
		return nil
	}

	// Route to appropriate processor based on event type
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
		w.logger.Warn("unknown Paddle event type, skipping",
			"job_id", job.ID,
			"event_type", webhook.EventType,
		)
		// Mark as processed even though we didn't handle it
		_ = w.paddleWebhookRepo.MarkProcessed(ctx, args.WebhookID)
		return nil // Don't retry unknown types
	}

	if processErr != nil {
		w.logger.Error("Paddle webhook processing failed",
			"job_id", job.ID,
			"webhook_id", args.WebhookID,
			"event_id", args.EventID,
			"event_type", webhook.EventType,
			"error", processErr,
			"duration_ms", time.Since(startTime).Milliseconds(),
		)

		// Mark as failed for tracking
		_ = w.paddleWebhookRepo.MarkFailed(ctx, args.WebhookID, processErr.Error())

		return fmt.Errorf("webhook processing failed: %w", processErr)
	}

	// Mark webhook as successfully processed
	if err := w.paddleWebhookRepo.MarkProcessed(ctx, args.WebhookID); err != nil {
		w.logger.Error("failed to mark webhook as processed",
			"webhook_id", args.WebhookID,
			"error", err,
		)
		// Don't fail the job - processing succeeded
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
// Links Paddle customer to user and sets subscription status to active.
func (w *SubscriptionWebhookWorker) processSubscriptionCreated(ctx context.Context, webhook *domain.PaddleWebhookEvent) error {
	data, err := w.parseWebhookPayload(webhook)
	if err != nil {
		return err
	}

	// Extract subscription details
	customerID, _ := data["customer_id"].(string)
	subscriptionID, _ := data["id"].(string)
	status, _ := data["status"].(string)

	// Extract custom_data to find user_id
	customData, _ := data["custom_data"].(map[string]interface{})
	userID, _ := customData["user_id"].(string)

	if userID == "" || customerID == "" {
		return errors.New("webhook payload missing user_id or customer_id")
	}

	w.logger.Info("processing subscription.created",
		"user_id", userID,
		"customer_id", customerID,
		"subscription_id", subscriptionID,
		"status", status,
	)

	// Link Paddle customer to user
	if err := w.profileRepo.UpdatePaddleCustomerID(ctx, userID, customerID); err != nil {
		return fmt.Errorf("failed to update paddle_customer_id: %w", err)
	}

	// Extract billing dates and tier from items
	var endsAt *time.Time
	tier := "premium" // Default tier
	var startedAt *time.Time

	if items, ok := data["items"].([]interface{}); ok && len(items) > 0 {
		if item, ok := items[0].(map[string]interface{}); ok {
			if _, ok := item["price"].(map[string]interface{}); ok {
				// TODO: Map price ID to tier (will need subscription_tiers.yaml lookup)
				// For now, extract from price description or product
				tier = "premium"
			}
		}
	}

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

	// Update subscription status
	if err := w.profileRepo.UpdateSubscriptionFromPaddle(ctx, userID, tier, "active", startedAt, endsAt); err != nil {
		return fmt.Errorf("failed to update subscription: %w", err)
	}

	// Update Clerk metadata (optional - only if service is configured)
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
			w.logger.Error("failed to update Clerk metadata",
				"user_id", userID,
				"error", err,
			)
			// Don't fail the job - Clerk metadata is not critical
		}
	}

	return nil
}

// processSubscriptionActivated handles subscription.activated events.
func (w *SubscriptionWebhookWorker) processSubscriptionActivated(ctx context.Context, webhook *domain.PaddleWebhookEvent) error {
	// Similar to subscription.created, but subscription moved from trial/paused to active
	return w.processSubscriptionCreated(ctx, webhook)
}

// processSubscriptionUpdated handles subscription.updated events.
// Handles tier changes, billing updates, and proration.
func (w *SubscriptionWebhookWorker) processSubscriptionUpdated(ctx context.Context, webhook *domain.PaddleWebhookEvent) error {
	data, err := w.parseWebhookPayload(webhook)
	if err != nil {
		return err
	}

	// Extract custom_data to find user_id
	customData, _ := data["custom_data"].(map[string]interface{})
	userID, _ := customData["user_id"].(string)

	if userID == "" {
		return errors.New("webhook payload missing user_id")
	}

	// Extract updated subscription details
	status, _ := data["status"].(string)
	tier := "premium" // TODO: Map price_id to tier

	var endsAt *time.Time
	if billingCycle, ok := data["billing_cycle"].(map[string]interface{}); ok {
		if endsAtStr, ok := billingCycle["ends_at"].(string); ok {
			if t, err := time.Parse(time.RFC3339, endsAtStr); err == nil {
				endsAt = &t
			}
		}
	}

	// Update subscription (startedAt is nil for updates)
	if err := w.profileRepo.UpdateSubscriptionFromPaddle(ctx, userID, tier, status, nil, endsAt); err != nil {
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

	// Set status to cancelled, but don't change tier yet
	// Subscription remains active until billing period ends
	user, err := w.profileRepo.GetUserByID(ctx, userID)
	if err != nil {
		return fmt.Errorf("failed to get user: %w", err)
	}

	if err := w.profileRepo.UpdateSubscriptionFromPaddle(ctx, userID, user.SubscriptionTier, "cancelled", nil, nil); err != nil {
		return fmt.Errorf("failed to update subscription status: %w", err)
	}

	return nil
}

// processSubscriptionPaused handles subscription.paused events.
func (w *SubscriptionWebhookWorker) processSubscriptionPaused(ctx context.Context, webhook *domain.PaddleWebhookEvent) error {
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

	if err := w.profileRepo.UpdateSubscriptionFromPaddle(ctx, userID, user.SubscriptionTier, "inactive", nil, nil); err != nil {
		return fmt.Errorf("failed to update subscription status: %w", err)
	}

	return nil
}

// processSubscriptionPastDue handles subscription.past_due events.
// Payment failed, grace period started.
func (w *SubscriptionWebhookWorker) processSubscriptionPastDue(ctx context.Context, webhook *domain.PaddleWebhookEvent) error {
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

	if err := w.profileRepo.UpdateSubscriptionFromPaddle(ctx, userID, user.SubscriptionTier, "past_due", nil, nil); err != nil {
		return fmt.Errorf("failed to update subscription status: %w", err)
	}

	// TODO: Send email notification about payment failure

	return nil
}

// processTransactionCompleted handles transaction.completed events.
// Records successful payment in audit log.
func (w *SubscriptionWebhookWorker) processTransactionCompleted(ctx context.Context, webhook *domain.PaddleWebhookEvent) error {
	w.logger.Info("transaction completed",
		"event_id", webhook.EventID,
	)

	// TODO: Store transaction in paddle_transactions table for audit

	return nil
}

// processTransactionPaymentFailed handles transaction.payment_failed events.
func (w *SubscriptionWebhookWorker) processTransactionPaymentFailed(ctx context.Context, webhook *domain.PaddleWebhookEvent) error {
	w.logger.Info("transaction payment failed",
		"event_id", webhook.EventID,
	)

	// TODO: Send email notification about payment failure
	// TODO: Store failed transaction in audit log

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
