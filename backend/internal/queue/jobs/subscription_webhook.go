// Package jobs contains River job definitions and workers for async processing.
package jobs

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/maxnussbaum/BoostedBudget/internal/config"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	phclient "github.com/maxnussbaum/BoostedBudget/internal/integration/posthog"
	"github.com/maxnussbaum/BoostedBudget/internal/observability"
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
	// #930: Retention tracking
	SetRetentionDates(ctx context.Context, userID string, startedAt, expiresAt time.Time) error
	ClearRetentionDates(ctx context.Context, userID string) error
}

// ClerkService defines the methods needed to update Clerk metadata.
type ClerkService interface {
	UpdateUserMetadata(ctx context.Context, clerkUserID string, metadata map[string]interface{}) error
}

// SubscriptionNotificationRepository defines the methods needed to create in-app notifications.
type SubscriptionNotificationRepository interface {
	Create(ctx context.Context, notification *domain.Notification) error
}

// SubscriptionPaddleTransactionRepository defines the methods needed to record Paddle transactions.
type SubscriptionPaddleTransactionRepository interface {
	Create(ctx context.Context, txn *domain.PaddleTransaction) error
}

// PlaidItemCleanupForWebhook defines the Plaid cleanup operation needed by subscription webhooks.
// Issue #896: Used to disconnect items on subscription cancellation/pause.
type PlaidItemCleanupForWebhook interface {
	DisconnectAllUserItems(ctx context.Context, userID string) error
}

// SubscriptionWebhookWorker processes Paddle subscription webhook events from the queue.
type SubscriptionWebhookWorker struct {
	river.WorkerDefaults[SubscriptionWebhookArgs]

	paddleWebhookRepo     PaddleWebhookRepository
	profileRepo           SubscriptionProfileRepository
	clerkService          ClerkService
	notificationRepo      SubscriptionNotificationRepository
	paddleTransactionRepo SubscriptionPaddleTransactionRepository
	plaidCleanupSvc       PlaidItemCleanupForWebhook
	analytics             phclient.Analytics // Optional PostHog analytics client
	tierConfig            *config.TierConfig
	retentionMonths       int // #930: Months before auto-deletion after cancellation
	logger                *slog.Logger
}

// SubscriptionWebhookWorkerDeps contains the dependencies for SubscriptionWebhookWorker.
type SubscriptionWebhookWorkerDeps struct {
	PaddleWebhookRepo     PaddleWebhookRepository
	ProfileRepo           SubscriptionProfileRepository
	ClerkService          ClerkService
	NotificationRepo      SubscriptionNotificationRepository
	PaddleTransactionRepo SubscriptionPaddleTransactionRepository
	TierConfig            *config.TierConfig
	RetentionMonths       int // #930: Months before auto-deletion after cancellation (default 6)
	Logger                *slog.Logger
}

// NewSubscriptionWebhookWorker creates a new subscription webhook processing worker.
func NewSubscriptionWebhookWorker(deps SubscriptionWebhookWorkerDeps) *SubscriptionWebhookWorker {
	logger := deps.Logger
	if logger == nil {
		logger = slog.Default()
	}
	retentionMonths := deps.RetentionMonths
	if retentionMonths <= 0 {
		retentionMonths = 6
	}
	return &SubscriptionWebhookWorker{
		paddleWebhookRepo:     deps.PaddleWebhookRepo,
		profileRepo:           deps.ProfileRepo,
		clerkService:          deps.ClerkService,
		notificationRepo:      deps.NotificationRepo,
		paddleTransactionRepo: deps.PaddleTransactionRepo,
		tierConfig:            deps.TierConfig,
		retentionMonths:       retentionMonths,
		logger:                logger,
	}
}

// SetPlaidCleanupService injects the Plaid cleanup service after construction.
// Issue #896: Follows the same setter pattern as DeletionService.
func (w *SubscriptionWebhookWorker) SetPlaidCleanupService(svc PlaidItemCleanupForWebhook) {
	w.plaidCleanupSvc = svc
}

// SetAnalytics injects the analytics client for capturing subscription events.
func (w *SubscriptionWebhookWorker) SetAnalytics(a phclient.Analytics) {
	w.analytics = a
}

// createNotification creates an in-app notification for a user.
// Logs and swallows errors so notification failures don't abort webhook processing.
func (w *SubscriptionWebhookWorker) createNotification(ctx context.Context, userID, notifType, title, message string) {
	if w.notificationRepo == nil {
		return
	}
	if err := w.notificationRepo.Create(ctx, &domain.Notification{
		ID:      uuid.New().String(),
		UserID:  userID,
		Type:    notifType,
		Title:   title,
		Message: message,
	}); err != nil {
		w.logger.Error("failed to create notification",
			"user_id", userID,
			"type", notifType,
			"error", err,
		)
	}
}

// syncClerkMetadata updates a user's Clerk public metadata with their current tier and status.
// Logs and swallows errors so Clerk failures don't abort webhook processing.
func (w *SubscriptionWebhookWorker) syncClerkMetadata(ctx context.Context, userID, tier, status string) {
	if w.clerkService == nil {
		return
	}
	user, err := w.profileRepo.GetUserByID(ctx, userID)
	if err != nil {
		w.logger.Error("failed to get user for Clerk sync", "user_id", userID, "error", err)
		return
	}
	if err := w.clerkService.UpdateUserMetadata(ctx, user.ClerkUserID, map[string]interface{}{
		"subscription_tier":   tier,
		"subscription_status": status,
	}); err != nil {
		w.logger.Error("failed to update Clerk metadata", "user_id", userID, "error", err)
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

// getStringFromMap safely extracts a string value from a map with type assertion
func getStringFromMap(m map[string]interface{}, key string) string {
	val, _ := m[key].(string)
	return val
}

// getMapFromMap safely extracts a nested map from a map with type assertion
func getMapFromMap(m map[string]interface{}, key string) map[string]interface{} {
	val, _ := m[key].(map[string]interface{})
	return val
}

// extractDateRange extracts start and end dates from a nested map with "starts_at" and "ends_at" fields.
// Returns (nil, nil) if fields are missing or malformed.
func extractDateRange(m map[string]interface{}) (*time.Time, *time.Time) {
	var startedAt, endsAt *time.Time
	if s, ok := m["starts_at"].(string); ok {
		if t, err := time.Parse(time.RFC3339, s); err == nil {
			startedAt = &t
		}
	}
	if e, ok := m["ends_at"].(string); ok {
		if t, err := time.Parse(time.RFC3339, e); err == nil {
			endsAt = &t
		}
	}
	return startedAt, endsAt
}

// resolveTierFromItems extracts the price_id from webhook items and resolves it to a tier name.
// Falls back to premium tier if price_id is not found or unknown. Issue #772: GAP 3
func (w *SubscriptionWebhookWorker) resolveTierFromItems(data map[string]interface{}) domain.TierName {
	items, ok := data["items"].([]interface{})
	if !ok || len(items) == 0 {
		w.logger.Warn("webhook data missing items array, falling back to premium tier")
		return domain.TierPremium
	}

	firstItem, ok := items[0].(map[string]interface{})
	if !ok {
		w.logger.Warn("webhook items[0] is not a map, falling back to premium tier")
		return domain.TierPremium
	}

	price, ok := firstItem["price"].(map[string]interface{})
	if !ok {
		w.logger.Warn("webhook items[0].price is not a map, falling back to premium tier")
		return domain.TierPremium
	}

	priceID, ok := price["id"].(string)
	if !ok || priceID == "" {
		w.logger.Warn("webhook items[0].price.id is missing or empty, falling back to premium tier")
		return domain.TierPremium
	}

	if w.tierConfig == nil {
		w.logger.Warn("TierConfig not available, falling back to premium tier")
		return domain.TierPremium
	}

	tier, err := w.tierConfig.GetTierByPriceID(priceID)
	if err != nil {
		w.logger.Warn("failed to resolve tier from price_id, falling back to premium tier",
			"price_id", priceID, "error", err)
		return domain.TierPremium
	}

	w.logger.Info("resolved tier from price_id", "price_id", priceID, "tier", tier)
	return tier
}

// Work processes a subscription webhook processing job.
func (w *SubscriptionWebhookWorker) Work(ctx context.Context, job *river.Job[SubscriptionWebhookArgs]) (err error) {
	args := job.Args

	w.logger.Info("starting subscription webhook processing job",
		"job_id", job.ID,
		"webhook_id", args.WebhookID,
		"event_id", args.EventID,
		"event_type", args.EventType,
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

	webhook, fetchErr := w.paddleWebhookRepo.GetByID(ctx, args.WebhookID)
	if fetchErr != nil {
		w.logger.Error("failed to fetch Paddle webhook",
			"webhook_id", args.WebhookID,
			"error", fetchErr,
		)
		if errors.Is(fetchErr, repository.ErrPaddleWebhookNotFound) {
			return nil
		}
		return fmt.Errorf("failed to fetch Paddle webhook: %w", fetchErr)
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
		// #930: Map paused to cancelled — subscriptions are either active or cancelled
		processErr = w.processSubscriptionCanceled(ctx, webhook)
	case "subscription.past_due":
		processErr = w.processSubscriptionPastDue(ctx, webhook)
	case "subscription.resumed":
		processErr = w.processSubscriptionResumed(ctx, webhook)
	case "subscription.trialing":
		processErr = w.processSubscriptionTrialing(ctx, webhook)
	case "transaction.completed":
		processErr = w.processTransactionCompleted(ctx, webhook)
	case "transaction.payment_failed":
		processErr = w.processTransactionPaymentFailed(ctx, webhook)
	case "transaction.past_due":
		processErr = w.processTransactionPastDue(ctx, webhook)
	case "transaction.canceled":
		processErr = w.processTransactionCanceled(ctx, webhook)
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
			"duration_ms", time.Since(start).Milliseconds(),
		)
		_ = w.paddleWebhookRepo.MarkFailed(ctx, args.WebhookID, processErr.Error())
		return fmt.Errorf("webhook processing failed: %w", processErr)
	}

	if markErr := w.paddleWebhookRepo.MarkProcessed(ctx, args.WebhookID); markErr != nil {
		w.logger.Error("failed to mark webhook as processed",
			"webhook_id", args.WebhookID,
			"error", markErr,
		)
	}

	w.logger.Info("Paddle webhook processed successfully",
		"job_id", job.ID,
		"webhook_id", args.WebhookID,
		"event_id", args.EventID,
		"event_type", webhook.EventType,
		"duration_ms", time.Since(start).Milliseconds(),
	)

	return nil
}

// processSubscriptionCreated handles subscription.created events.
func (w *SubscriptionWebhookWorker) processSubscriptionCreated(ctx context.Context, webhook *domain.PaddleWebhookEvent) error {
	data, err := w.parseWebhookPayload(webhook)
	if err != nil {
		return err
	}

	customerID := getStringFromMap(data, "customer_id")
	customData := getMapFromMap(data, "custom_data")
	userID := getStringFromMap(customData, "user_id")
	// Issue #48: Extract subscription ID from webhook data
	subscriptionID := getStringFromMap(data, "id")

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

	// Issue #772: Resolve tier from Paddle price_id instead of hardcoding "premium"
	tier := w.resolveTierFromItems(data)

	var startedAt, endsAt *time.Time
	if billingCycle, ok := data["billing_cycle"].(map[string]interface{}); ok {
		startedAt, endsAt = extractDateRange(billingCycle)
	}

	// Extract status from webhook data (e.g., "active", "trialing")
	status := getStringFromMap(data, "status")
	if status == "" {
		status = "active" // fallback for older webhooks
	}

	// Issue #48: Pass subscription ID to repository for storage
	if err := w.profileRepo.UpdateSubscriptionFromPaddle(ctx, userID, string(tier), status, startedAt, endsAt, &subscriptionID); err != nil {
		return fmt.Errorf("failed to update subscription: %w", err)
	}

	w.syncClerkMetadata(ctx, userID, string(tier), status)

	// #930: Clear retention clock — subscription created, user is no longer pending deletion
	if retErr := w.profileRepo.ClearRetentionDates(ctx, userID); retErr != nil {
		w.logger.Warn("failed to clear retention dates on subscription created", "user_id", userID, "error", retErr)
	}

	if w.analytics != nil {
		w.analytics.Capture(userID, phclient.EventSubscriptionCreated, map[string]interface{}{
			"tier":            string(tier),
			"status":          status,
			"subscription_id": subscriptionID,
		})
	}

	return nil
}

// processSubscriptionActivated handles subscription.activated events.
// Dedicated handler (not delegating to created): updates subscription, notifies user, syncs Clerk.
func (w *SubscriptionWebhookWorker) processSubscriptionActivated(ctx context.Context, webhook *domain.PaddleWebhookEvent) error {
	data, err := w.parseWebhookPayload(webhook)
	if err != nil {
		return err
	}

	customData := getMapFromMap(data, "custom_data")
	userID := getStringFromMap(customData, "user_id")
	subscriptionID := getStringFromMap(data, "id")

	if userID == "" {
		return errors.New("webhook payload missing user_id")
	}
	if subscriptionID == "" {
		return errors.New("webhook payload missing subscription id")
	}

	tier := w.resolveTierFromItems(data)
	status := getStringFromMap(data, "status")
	if status == "" {
		status = "active"
	}

	if err := w.profileRepo.UpdateSubscriptionFromPaddle(ctx, userID, string(tier), status, nil, nil, &subscriptionID); err != nil {
		return fmt.Errorf("failed to update subscription: %w", err)
	}

	w.createNotification(ctx, userID, "subscription_activated",
		"Subscription Activated",
		"Your BoostedBudget subscription is now active. Welcome aboard!",
	)
	w.syncClerkMetadata(ctx, userID, string(tier), status)

	// #930: Clear retention clock — user resubscribed, data is no longer pending deletion
	if retErr := w.profileRepo.ClearRetentionDates(ctx, userID); retErr != nil {
		w.logger.Warn("failed to clear retention dates on activation", "user_id", userID, "error", retErr)
	}

	return nil
}

// processSubscriptionUpdated handles subscription.updated events.
func (w *SubscriptionWebhookWorker) processSubscriptionUpdated(ctx context.Context, webhook *domain.PaddleWebhookEvent) error {
	data, err := w.parseWebhookPayload(webhook)
	if err != nil {
		return err
	}

	customData := getMapFromMap(data, "custom_data")
	userID := getStringFromMap(customData, "user_id")
	// Issue #48: Extract subscription ID from webhook data
	subscriptionID := getStringFromMap(data, "id")

	if userID == "" {
		return errors.New("webhook payload missing user_id")
	}

	status, _ := data["status"].(string)
	// Issue #772: Resolve tier from Paddle price_id instead of hardcoding "premium"
	tier := w.resolveTierFromItems(data)

	var endsAt *time.Time
	if billingCycle, ok := data["billing_cycle"].(map[string]interface{}); ok {
		_, endsAt = extractDateRange(billingCycle)
	}

	// Issue #48: Pass subscription ID to repository (may be empty string if not present)
	var subIDPtr *string
	if subscriptionID != "" {
		subIDPtr = &subscriptionID
	}
	if err := w.profileRepo.UpdateSubscriptionFromPaddle(ctx, userID, string(tier), status, nil, endsAt, subIDPtr); err != nil {
		return fmt.Errorf("failed to update subscription: %w", err)
	}

	// Issues #747 #748: Sync Clerk metadata on subscription updates
	w.syncClerkMetadata(ctx, userID, string(tier), status)

	// #930: Clear retention clock if the updated status is non-read-only (e.g., user resubscribed)
	if !domain.SubscriptionStatus(status).IsReadOnlySubscription() && status != "" {
		if retErr := w.profileRepo.ClearRetentionDates(ctx, userID); retErr != nil {
			w.logger.Warn("failed to clear retention dates on subscription updated", "user_id", userID, "error", retErr)
		}
	}

	return nil
}

// processSubscriptionCanceled handles subscription.canceled events.
func (w *SubscriptionWebhookWorker) processSubscriptionCanceled(ctx context.Context, webhook *domain.PaddleWebhookEvent) error {
	data, err := w.parseWebhookPayload(webhook)
	if err != nil {
		return err
	}

	customData := getMapFromMap(data, "custom_data")
	userID := getStringFromMap(customData, "user_id")

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

	// Issue #896: Disconnect Plaid items to stop billing on cancellation
	if w.plaidCleanupSvc != nil {
		if err := w.plaidCleanupSvc.DisconnectAllUserItems(ctx, userID); err != nil {
			w.logger.Warn("failed to disconnect Plaid items on cancellation",
				"user_id", userID, "error", err)
		}
	}

	// Issues #747 #748: Sync Clerk metadata (no notification — user-initiated)
	w.syncClerkMetadata(ctx, userID, user.SubscriptionTier, "cancelled")

	// #930: Start 6-month retention clock — data preserved read-only until auto-deletion
	now := time.Now()
	if retErr := w.profileRepo.SetRetentionDates(ctx, userID, now, now.AddDate(0, w.retentionMonths, 0)); retErr != nil {
		w.logger.Warn("failed to set retention dates on cancellation", "user_id", userID, "error", retErr)
	}

	if w.analytics != nil {
		w.analytics.Capture(userID, phclient.EventSubscriptionCancelled, nil)
	}

	return nil
}


// processSubscriptionPastDue handles subscription.past_due events.
// Issues #747 #748: Notifies user of overdue payment and syncs Clerk.
func (w *SubscriptionWebhookWorker) processSubscriptionPastDue(ctx context.Context, webhook *domain.PaddleWebhookEvent) error {
	data, err := w.parseWebhookPayload(webhook)
	if err != nil {
		return err
	}

	customData := getMapFromMap(data, "custom_data")
	userID := getStringFromMap(customData, "user_id")

	if userID == "" {
		return errors.New("webhook payload missing user_id")
	}

	user, err := w.profileRepo.GetUserByID(ctx, userID)
	if err != nil {
		return fmt.Errorf("failed to get user: %w", err)
	}

	if err := w.profileRepo.UpdateSubscriptionFromPaddle(ctx, userID, user.SubscriptionTier, string(domain.SubscriptionStatusPastDue), nil, nil, nil); err != nil {
		return fmt.Errorf("failed to update subscription status: %w", err)
	}

	w.createNotification(ctx, userID, "subscription_past_due",
		"Payment Overdue",
		"Your subscription payment is overdue. Please update your payment method to maintain access.",
	)
	w.syncClerkMetadata(ctx, userID, user.SubscriptionTier, string(domain.SubscriptionStatusPastDue))

	return nil
}

// processSubscriptionResumed handles subscription.resumed events.
// Issues #747 #748: New handler — updates status to active, notifies user, syncs Clerk.
func (w *SubscriptionWebhookWorker) processSubscriptionResumed(ctx context.Context, webhook *domain.PaddleWebhookEvent) error {
	data, err := w.parseWebhookPayload(webhook)
	if err != nil {
		return err
	}

	customData := getMapFromMap(data, "custom_data")
	userID := getStringFromMap(customData, "user_id")
	subscriptionID := getStringFromMap(data, "id")

	if userID == "" {
		return errors.New("webhook payload missing user_id")
	}

	tier := w.resolveTierFromItems(data)

	var subIDPtr *string
	if subscriptionID != "" {
		subIDPtr = &subscriptionID
	}

	if err := w.profileRepo.UpdateSubscriptionFromPaddle(ctx, userID, string(tier), string(domain.SubscriptionStatusActive), nil, nil, subIDPtr); err != nil {
		return fmt.Errorf("failed to update subscription status: %w", err)
	}

	w.createNotification(ctx, userID, "subscription_resumed",
		"Subscription Resumed",
		"Your BoostedBudget subscription has been resumed. Welcome back!",
	)
	w.syncClerkMetadata(ctx, userID, string(tier), string(domain.SubscriptionStatusActive))

	// #930: Clear retention clock — user resubscribed, data is no longer pending deletion
	if retErr := w.profileRepo.ClearRetentionDates(ctx, userID); retErr != nil {
		w.logger.Warn("failed to clear retention dates on resume", "user_id", userID, "error", retErr)
	}

	return nil
}

// processSubscriptionTrialing handles subscription.trialing events.
// Issues #747 #748: New handler — updates status to trialing, notifies user, syncs Clerk.
func (w *SubscriptionWebhookWorker) processSubscriptionTrialing(ctx context.Context, webhook *domain.PaddleWebhookEvent) error {
	data, err := w.parseWebhookPayload(webhook)
	if err != nil {
		return err
	}

	customData := getMapFromMap(data, "custom_data")
	userID := getStringFromMap(customData, "user_id")
	subscriptionID := getStringFromMap(data, "id")

	if userID == "" {
		return errors.New("webhook payload missing user_id")
	}

	tier := w.resolveTierFromItems(data)

	// Issue #748 gap: Extract trial period dates from current_billing_period
	var startedAt, endsAt *time.Time
	if cbp, ok := data["current_billing_period"].(map[string]interface{}); ok {
		startedAt, endsAt = extractDateRange(cbp)
	}

	var subIDPtr *string
	if subscriptionID != "" {
		subIDPtr = &subscriptionID
	}
	if err := w.profileRepo.UpdateSubscriptionFromPaddle(ctx, userID, string(tier), string(domain.SubscriptionStatusTrialing), startedAt, endsAt, subIDPtr); err != nil {
		return fmt.Errorf("failed to update subscription status: %w", err)
	}

	w.createNotification(ctx, userID, "subscription_trialing", "Trial Started",
		"Your BoostedBudget trial is now active. Enjoy full access during your trial period!")
	w.syncClerkMetadata(ctx, userID, string(tier), string(domain.SubscriptionStatusTrialing))

	// #930: Clear retention clock — trialing is an active state, user should not be auto-deleted
	if retErr := w.profileRepo.ClearRetentionDates(ctx, userID); retErr != nil {
		w.logger.Warn("failed to clear retention dates on subscription trialing", "user_id", userID, "error", retErr)
	}

	return nil
}

// processTransactionCompleted handles transaction.completed events.
// Issues #747 #748: Records the completed transaction in the audit log.
func (w *SubscriptionWebhookWorker) processTransactionCompleted(ctx context.Context, webhook *domain.PaddleWebhookEvent) error {
	data, err := w.parseWebhookPayload(webhook)
	if err != nil {
		return err
	}

	txn := w.buildPaddleTransaction(data, webhook.Payload, "completed")
	if txn == nil {
		w.logger.Warn("transaction.completed missing transaction_id or user_id, skipping record", "event_id", webhook.EventID)
		return nil
	}

	if w.paddleTransactionRepo != nil {
		if err := w.paddleTransactionRepo.Create(ctx, txn); err != nil && !errors.Is(err, repository.ErrPaddleTransactionDuplicate) {
			return fmt.Errorf("failed to record completed transaction: %w", err)
		}
	}

	if w.analytics != nil {
		w.analytics.Capture(txn.UserID, phclient.EventPaymentSucceeded, map[string]interface{}{
			"amount":         txn.Amount,
			"currency":       txn.Currency,
			"transaction_id": txn.TransactionID,
		})
	}

	return nil
}

// processTransactionPaymentFailed handles transaction.payment_failed events.
// Issues #747 #748: Records the failed transaction and notifies the user.
func (w *SubscriptionWebhookWorker) processTransactionPaymentFailed(ctx context.Context, webhook *domain.PaddleWebhookEvent) error {
	data, err := w.parseWebhookPayload(webhook)
	if err != nil {
		return err
	}

	txn := w.buildPaddleTransaction(data, webhook.Payload, "failed")
	if txn == nil {
		w.logger.Warn("transaction.payment_failed missing transaction_id or user_id, skipping record", "event_id", webhook.EventID)
		return nil
	}

	if w.paddleTransactionRepo != nil {
		if err := w.paddleTransactionRepo.Create(ctx, txn); err != nil && !errors.Is(err, repository.ErrPaddleTransactionDuplicate) {
			return fmt.Errorf("failed to record failed transaction: %w", err)
		}
	}

	w.createNotification(ctx, txn.UserID, "payment_failed",
		"Payment Failed",
		"We couldn't process your payment. Please update your payment method to keep your subscription active.",
	)

	user, err := w.profileRepo.GetUserByID(ctx, txn.UserID)
	if err == nil {
		w.syncClerkMetadata(ctx, txn.UserID, user.SubscriptionTier, user.SubscriptionStatus)
	}

	if w.analytics != nil {
		w.analytics.Capture(txn.UserID, phclient.EventPaymentFailed, map[string]interface{}{
			"transaction_id": txn.TransactionID,
		})
	}

	return nil
}

// processTransactionPastDue handles transaction.past_due events.
// Issues #747 gap: Records the past-due transaction and notifies the user.
func (w *SubscriptionWebhookWorker) processTransactionPastDue(ctx context.Context, webhook *domain.PaddleWebhookEvent) error {
	data, err := w.parseWebhookPayload(webhook)
	if err != nil {
		return err
	}

	txn := w.buildPaddleTransaction(data, webhook.Payload, "past_due")
	if txn == nil {
		w.logger.Warn("transaction.past_due missing transaction_id or user_id, skipping record", "event_id", webhook.EventID)
		return nil
	}

	if w.paddleTransactionRepo != nil {
		if err := w.paddleTransactionRepo.Create(ctx, txn); err != nil && !errors.Is(err, repository.ErrPaddleTransactionDuplicate) {
			return fmt.Errorf("failed to record past-due transaction: %w", err)
		}
	}

	w.createNotification(ctx, txn.UserID, "transaction_past_due",
		"Payment Past Due",
		"Your subscription payment is past due. Please update your payment method to avoid service interruption.",
	)

	return nil
}

// processTransactionCanceled handles transaction.canceled events.
// Issues #747 #748: Records the canceled transaction and notifies the user.
func (w *SubscriptionWebhookWorker) processTransactionCanceled(ctx context.Context, webhook *domain.PaddleWebhookEvent) error {
	data, err := w.parseWebhookPayload(webhook)
	if err != nil {
		return err
	}

	txn := w.buildPaddleTransaction(data, webhook.Payload, "canceled")
	if txn == nil {
		w.logger.Warn("transaction.canceled missing transaction_id or user_id, skipping record", "event_id", webhook.EventID)
		return nil
	}

	if w.paddleTransactionRepo != nil {
		if err := w.paddleTransactionRepo.Create(ctx, txn); err != nil && !errors.Is(err, repository.ErrPaddleTransactionDuplicate) {
			return fmt.Errorf("failed to record canceled transaction: %w", err)
		}
	}

	w.createNotification(ctx, txn.UserID, "payment_failed",
		"Transaction Canceled",
		"Your transaction was canceled. Contact support if you have any questions.",
	)

	return nil
}

// buildPaddleTransaction extracts transaction fields from webhook data and builds a PaddleTransaction.
// Returns nil if essential fields (user_id, transaction_id) are missing.
func (w *SubscriptionWebhookWorker) buildPaddleTransaction(data map[string]interface{}, rawPayload json.RawMessage, status string) *domain.PaddleTransaction {
	customData := getMapFromMap(data, "custom_data")
	userID := getStringFromMap(customData, "user_id")
	transactionID := getStringFromMap(data, "id")

	if userID == "" || transactionID == "" {
		return nil
	}

	subscriptionID := getStringFromMap(data, "subscription_id")
	customerID := getStringFromMap(data, "customer_id")
	currency := getStringFromMap(data, "currency_code")
	if currency == "" {
		currency = "USD"
	}

	// Extract amount from details.totals.grand_total (in cents as string)
	var amount float64
	if details, ok := data["details"].(map[string]interface{}); ok {
		if totals, ok := details["totals"].(map[string]interface{}); ok {
			if grandTotal, ok := totals["grand_total"].(string); ok {
				fmt.Sscanf(grandTotal, "%f", &amount) //nolint:errcheck
				amount = amount / 100.0               // cents to dollars
			}
		}
	}

	// Extract payment method type
	var paymentMethodType *string
	if payments, ok := data["payments"].([]interface{}); ok && len(payments) > 0 {
		if payment, ok := payments[0].(map[string]interface{}); ok {
			if methodDetails, ok := payment["method_details"].(map[string]interface{}); ok {
				if pmt := getStringFromMap(methodDetails, "type"); pmt != "" {
					paymentMethodType = &pmt
				}
			}
		}
	}

	// Extract billing period
	var billingStart, billingEnd *time.Time
	if bp, ok := data["billing_period"].(map[string]interface{}); ok {
		if s, ok := bp["starts_at"].(string); ok {
			if t, err := time.Parse(time.RFC3339, s); err == nil {
				billingStart = &t
			}
		}
		if e, ok := bp["ends_at"].(string); ok {
			if t, err := time.Parse(time.RFC3339, e); err == nil {
				billingEnd = &t
			}
		}
	}

	txn := &domain.PaddleTransaction{
		UserID:             userID,
		TransactionID:      transactionID,
		Amount:             amount,
		Currency:           currency,
		Status:             status,
		PaymentMethodType:  paymentMethodType,
		BillingPeriodStart: billingStart,
		BillingPeriodEnd:   billingEnd,
		Payload:            rawPayload,
	}

	if subscriptionID != "" {
		txn.SubscriptionID = &subscriptionID
	}
	if customerID != "" {
		txn.CustomerID = &customerID
	}

	return txn
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
