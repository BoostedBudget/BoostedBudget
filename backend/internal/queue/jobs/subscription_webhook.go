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
	// #898: Dunning — past_due timestamp tracking
	SetPastDueStartedAt(ctx context.Context, userID string, t time.Time) error
	ClearPastDueStartedAt(ctx context.Context, userID string) error
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
// Issue #896: Disconnects all Plaid items on subscription cancellation. Pause is not a supported
// product feature (#933); subscription.paused is treated as cancellation and uses this same path.
type PlaidItemCleanupForWebhook interface {
	DisconnectAllUserItems(ctx context.Context, userID string) error
}

// HouseholdOverCapacityRepository defines the single, non-transactional household
// lookup the webhook worker's best-effort over-capacity reconcile step
// (reconcileOverCapacity/clearOverCapacityIfOwner) needs to cheaply determine whether a
// user owns a household at all, on every tier-change/cancellation/past-due webhook.
// Deliberately narrower than repository.HouseholdRepository — mirrors this file's
// existing narrow-interface convention (SubscriptionProfileRepository, etc.). Once a
// household IS found and owned, the reconcile step upgrades to the full
// repository.HouseholdRepository (via repository.NewHouseholdRepository(tx)) inside its
// own short-lived transaction, rather than widening this interface. #1371 — Phase 7
// (Task 6, Part B).
type HouseholdOverCapacityRepository interface {
	GetHouseholdForUser(ctx context.Context, userID string) (household *domain.Household, isOwner bool, found bool, err error)
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
	// householdRepo + db back the best-effort household over-capacity reconcile step
	// (#1371 — Task 6, Part B). Both nil-safe: reconcileOverCapacity/
	// clearOverCapacityIfOwner no-op when householdRepo is nil, matching this
	// codebase's analytics/email nil-safe convention.
	householdRepo HouseholdOverCapacityRepository
	db            repository.DB
	logger        *slog.Logger
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
	// HouseholdRepo + DB wire the best-effort household over-capacity reconcile step
	// (#1371 — Task 6, Part B). Optional — leave both nil to disable (no-op).
	HouseholdRepo HouseholdOverCapacityRepository
	DB            repository.DB
	Logger        *slog.Logger
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
		householdRepo:         deps.HouseholdRepo,
		db:                    deps.DB,
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

// notificationParams holds parameters for creating an in-app notification.
type notificationParams struct {
	userID             string
	notificationType   string
	title              string
	message            string
	actionURL          *string
	actionLabel        *string
	interactionPattern string
}

// createNotification creates an in-app notification for a user.
// Logs and swallows errors so notification failures don't abort webhook processing.
func (w *SubscriptionWebhookWorker) createNotification(ctx context.Context, params notificationParams) {
	if w.notificationRepo == nil {
		return
	}
	if err := w.notificationRepo.Create(ctx, &domain.Notification{
		ID:                 uuid.New().String(),
		UserID:             params.userID,
		Type:               params.notificationType,
		Title:              params.title,
		Message:            params.message,
		ActionURL:          params.actionURL,
		ActionLabel:        params.actionLabel,
		InteractionPattern: params.interactionPattern,
	}); err != nil {
		w.logger.Error("failed to create notification",
			"user_id", params.userID,
			"type", params.notificationType,
			"error", err,
		)
	}
}

// stringPtr returns a pointer to a string value.
func stringPtr(s string) *string { return &s }

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

// reconcileOverCapacity is the fast-path, best-effort household over-capacity check.
// Callers invoke this AFTER a tier-change webhook has already durably written the
// user's new subscription_tier/subscription_status (processSubscriptionUpdated/
// Activated) — this method only OBSERVES that already-committed state; it never runs
// inside the same transaction as the tier write, and there is no "prior tier" to diff
// against, because this feature stores no tier snapshot (see
// household_entitlement_service.go — the resolver is deliberately unchanged by #1371).
//
// No-op (nil error) when householdRepo isn't wired, or when ownerUserID doesn't own a
// household. Otherwise locks the household row, recomputes the owner's CURRENT
// effective-tier seat count (same technique as paddle_checkout.go's
// checkDowngradeSeatCapacity — Task 5), and either starts or clears the
// over_capacity_since grace clock via the idempotent SetOverCapacity/ClearOverCapacity
// repository methods (Task 1). Every write here is safe to call twice —
// SetOverCapacity's COALESCE-based UPDATE is a no-op once grace is already running
// (returning the already-set value rather than overwriting it), and ClearOverCapacity
// is a plain idempotent null-out.
//
// The daily household_over_capacity_check worker (Task 6, Part C) is the AUTHORITATIVE
// backstop for anything this fast path misses or races against — callers must log
// (never propagate) any error this returns; see the call sites below.
func (w *SubscriptionWebhookWorker) reconcileOverCapacity(ctx context.Context, ownerUserID string) error {
	if w.householdRepo == nil {
		return nil
	}
	household, isOwner, found, err := w.householdRepo.GetHouseholdForUser(ctx, ownerUserID)
	if err != nil {
		return fmt.Errorf("failed to look up household for user %s: %w", ownerUserID, err)
	}
	if !found || !isOwner {
		return nil
	}
	if w.db == nil {
		return fmt.Errorf("household over-capacity reconcile: DB not configured (household_id=%s)", household.ID)
	}

	owner, err := w.profileRepo.GetUserByID(ctx, ownerUserID)
	if err != nil {
		return fmt.Errorf("failed to get owner user %s: %w", ownerUserID, err)
	}
	tier, err := w.tierConfig.GetTier(domain.TierName(owner.ComputeEffectiveTier()))
	if err != nil {
		return fmt.Errorf("failed to resolve tier for owner %s: %w", ownerUserID, err)
	}
	seatLimit := tier.Pricing.UsersIncluded

	tx, err := w.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("failed to begin over-capacity reconcile transaction: %w", err)
	}
	defer func() {
		_ = tx.Rollback(ctx)
	}()

	hhRepo := repository.NewHouseholdRepository(tx)
	locked, err := hhRepo.LockHouseholdByID(ctx, household.ID)
	if err != nil {
		return fmt.Errorf("failed to lock household %s: %w", household.ID, err)
	}
	activeCount, err := hhRepo.CountActiveMembers(ctx, household.ID)
	if err != nil {
		return fmt.Errorf("failed to count active members for household %s: %w", household.ID, err)
	}

	switch {
	case activeCount > seatLimit && locked.OverCapacitySince == nil:
		// This call site already holds a row lock on the household via
		// LockHouseholdByID above, so it cannot lose a race against a concurrent
		// writer — the returned effective timestamp is discarded on purpose.
		if _, err := hhRepo.SetOverCapacity(ctx, household.ID, time.Now()); err != nil {
			return fmt.Errorf("failed to set over-capacity for household %s: %w", household.ID, err)
		}
		if w.analytics != nil {
			w.analytics.Capture(ownerUserID, phclient.EventHouseholdOverCapacityEntered, map[string]interface{}{
				"household_id": household.ID,
				"active_count": activeCount,
				"seat_limit":   seatLimit,
			})
		}
	case activeCount <= seatLimit && locked.OverCapacitySince != nil:
		if err := hhRepo.ClearOverCapacity(ctx, household.ID); err != nil {
			return fmt.Errorf("failed to clear over-capacity for household %s: %w", household.ID, err)
		}
	}

	return tx.Commit(ctx)
}

// clearOverCapacityIfOwner clears a lingering over-capacity grace flag when
// ownerUserID loses active coverage entirely (cancellation/past-due) — grace is moot
// once members fall back to their own tier via the resolver's owner-read-only branch,
// unchanged by this task. Best-effort: logs and swallows its own errors, matching
// every other nice-to-have side effect in this file (Clerk sync, notifications,
// retention clock). No transaction/lock needed — ClearOverCapacity is a single
// idempotent UPDATE with no read-then-decide step. #1371 — Phase 7 (Task 6, Part B).
func (w *SubscriptionWebhookWorker) clearOverCapacityIfOwner(ctx context.Context, ownerUserID string) {
	if w.householdRepo == nil || w.db == nil {
		return
	}
	household, isOwner, found, err := w.householdRepo.GetHouseholdForUser(ctx, ownerUserID)
	if err != nil {
		w.logger.Warn("failed to look up household while clearing over-capacity", "user_id", ownerUserID, "error", err)
		return
	}
	if !found || !isOwner {
		return
	}
	hhRepo := repository.NewHouseholdRepository(w.db)
	if err := hhRepo.ClearOverCapacity(ctx, household.ID); err != nil {
		w.logger.Warn("failed to clear over-capacity on subscription loss", "household_id", household.ID, "error", err)
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
		// #933: Pause not supported; treat as cancellation to revoke access and start retention.
		processErr = w.processSubscriptionCanceled(ctx, webhook)
	case "subscription.past_due":
		processErr = w.processSubscriptionPastDue(ctx, webhook)
	case "subscription.resumed":
		// #933: Resume not supported (paused→cancelled is terminal). Acknowledge without state change.
		w.logger.Info("subscription.resumed received but not supported; acknowledging without state change",
			"webhook_id", args.WebhookID)
		if err := w.paddleWebhookRepo.MarkProcessed(ctx, args.WebhookID); err != nil {
			w.logger.Warn("failed to mark resumed webhook as processed", "webhook_id", args.WebhookID, "error", err)
		}
		return nil
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

	// #933: Normalize Paddle status (paused/canceled → cancelled) so invalid values never reach the DB.
	status := domain.NormalizeSubscriptionStatus(getStringFromMap(data, "status"))
	if status == "" {
		status = "active" // fallback for older webhooks
	}

	if err := w.profileRepo.UpdateSubscriptionFromPaddle(ctx, userID, string(tier), status, startedAt, endsAt, &subscriptionID); err != nil {
		return fmt.Errorf("failed to update subscription: %w", err)
	}

	w.syncClerkMetadata(ctx, userID, string(tier), status)

	// #930/#933: Only fire active-path side effects for genuinely-active statuses.
	// Uses IsActiveSubscription() (active/trial/trialing) — not !IsReadOnlySubscription() —
	// because created/activated also fire funnel analytics and a welcome notification that
	// require real activation. past_due/cancelled/inactive are all excluded: analytics
	// fired with those statuses would corrupt conversion funnels (#933).
	if domain.SubscriptionStatus(status).IsActiveSubscription() {
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
	// #933: Normalize Paddle status (paused/canceled → cancelled) before DB write.
	status := domain.NormalizeSubscriptionStatus(getStringFromMap(data, "status"))
	if status == "" {
		status = "active"
	}

	if err := w.profileRepo.UpdateSubscriptionFromPaddle(ctx, userID, string(tier), status, nil, nil, &subscriptionID); err != nil {
		return fmt.Errorf("failed to update subscription: %w", err)
	}

	// #930/#933: Only fire active-path side effects for genuinely-active statuses.
	// Uses IsActiveSubscription() (active/trial/trialing) — not !IsReadOnlySubscription() —
	// because the "Welcome aboard!" notification and retention-clear require real activation.
	// past_due/cancelled/inactive are excluded: sending an activation notice to a past_due
	// user is factually wrong and corrupts UX (#933).
	if domain.SubscriptionStatus(status).IsActiveSubscription() {
		w.createNotification(ctx, notificationParams{
			userID:             userID,
			notificationType:   "subscription_activated",
			title:              "Subscription Activated",
			message:            "Your BoostedBudget subscription is now active. Welcome aboard!",
			interactionPattern: "informational",
		})
		if retErr := w.profileRepo.ClearRetentionDates(ctx, userID); retErr != nil {
			w.logger.Warn("failed to clear retention dates on activation", "user_id", userID, "error", retErr)
		}
		// #898: Clear past_due stamp on recovery (user may have recovered from past_due).
		if clearErr := w.profileRepo.ClearPastDueStartedAt(ctx, userID); clearErr != nil {
			w.logger.Warn("failed to clear past_due_started_at on activation", "user_id", userID, "error", clearErr)
		}
	}
	w.syncClerkMetadata(ctx, userID, string(tier), status)

	// #1371 — Task 6, Part B: best-effort fast-path over-capacity check, run AFTER the
	// tier write above has already landed. Logged, never propagated — see
	// reconcileOverCapacity's doc comment. Round 4 Gap Analysis fix: if the new status is
	// read-only, grace is moot (an "activated" event carrying a read-only status isn't
	// expected from Paddle in practice, but this stays defensive/consistent with
	// processSubscriptionUpdated below) — clear instead of reconcile, matching
	// processSubscriptionPastDue/processSubscriptionCanceled's existing convention.
	if domain.SubscriptionStatus(status).IsReadOnlySubscription() {
		w.clearOverCapacityIfOwner(ctx, userID)
	} else if err := w.reconcileOverCapacity(ctx, userID); err != nil {
		w.logger.Warn("failed to reconcile household over-capacity after subscription activation", "user_id", userID, "error", err)
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
	subscriptionID := getStringFromMap(data, "id")

	if userID == "" {
		return errors.New("webhook payload missing user_id")
	}

	// #933: Normalize Paddle status (paused/canceled → cancelled) before DB write.
	status := domain.NormalizeSubscriptionStatus(getStringFromMap(data, "status"))
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
		// #898: Clear past_due stamp on recovery to active/trialing.
		if clearErr := w.profileRepo.ClearPastDueStartedAt(ctx, userID); clearErr != nil {
			w.logger.Warn("failed to clear past_due_started_at on subscription updated", "user_id", userID, "error", clearErr)
		}
	}

	// #1371 — Task 6, Part B: best-effort fast-path over-capacity check, run AFTER the
	// tier write above has already landed. Logged, never propagated — see
	// reconcileOverCapacity's doc comment. Round 4 Gap Analysis fix: if the new status is
	// read-only (e.g. a scheduled downgrade lands the subscription in past_due via THIS
	// generic event rather than the dedicated subscription.past_due event), grace is moot
	// — clear instead of reconcile, matching processSubscriptionPastDue/
	// processSubscriptionCanceled's existing convention (same IsReadOnlySubscription()
	// check used just above for the retention-clock guard).
	if domain.SubscriptionStatus(status).IsReadOnlySubscription() {
		w.clearOverCapacityIfOwner(ctx, userID)
	} else if err := w.reconcileOverCapacity(ctx, userID); err != nil {
		w.logger.Warn("failed to reconcile household over-capacity after subscription update", "user_id", userID, "error", err)
	}

	return nil
}

// processSubscriptionCanceled handles subscription.canceled and subscription.paused events.
// Pause is not a supported product feature (#933); paused subscriptions are treated as terminal
// cancellations — access is revoked, Plaid items disconnected, and data-retention starts.
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

	// Preserve paddle_subscription_id (needed for resubscribe flow).
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

	// #898: Clear past_due stamp on cancellation (user may have been in past_due before cancelling).
	if clearErr := w.profileRepo.ClearPastDueStartedAt(ctx, userID); clearErr != nil {
		w.logger.Warn("failed to clear past_due_started_at on cancellation", "user_id", userID, "error", clearErr)
	}

	if w.analytics != nil {
		w.analytics.Capture(userID, phclient.EventSubscriptionCancelled, nil)
	}

	// #1371 — Task 6, Part B: clear any lingering over-capacity grace flag — the owner
	// just lost active coverage entirely, so grace is moot (members already fall back
	// to their own tier via the resolver's unchanged owner-read-only branch).
	w.clearOverCapacityIfOwner(ctx, userID)

	return nil
}

// processSubscriptionPastDue handles subscription.past_due events.
// Issues #747 #748: Notifies user of overdue payment and syncs Clerk.
// #898: Only transitions from active/trialing — does not overwrite cancelled/inactive/past_due.
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

	// #898: Only transition into past_due from active or trialing states.
	// Do not overwrite cancelled, inactive, or an already-existing past_due.
	prevStatus := domain.SubscriptionStatus(user.SubscriptionStatus)
	if prevStatus != domain.SubscriptionStatusActive && prevStatus != domain.SubscriptionStatusTrialing {
		return nil
	}

	if err := w.profileRepo.UpdateSubscriptionFromPaddle(ctx, userID, user.SubscriptionTier, string(domain.SubscriptionStatusPastDue), nil, nil, nil); err != nil {
		return fmt.Errorf("failed to update subscription status: %w", err)
	}

	// #898: Stamp the time the user entered past_due (nil-safe: won't reset if already set).
	if stampErr := w.profileRepo.SetPastDueStartedAt(ctx, userID, time.Now()); stampErr != nil {
		w.logger.Warn("failed to set past_due_started_at", "user_id", userID, "error", stampErr)
	}

	w.createNotification(ctx, notificationParams{
		userID:             userID,
		notificationType:   "subscription_past_due",
		title:              "Payment Overdue",
		message:            "Your subscription payment is overdue. Please update your payment method to maintain access.",
		actionURL:          stringPtr("/billing"),
		actionLabel:        stringPtr("Update Payment"),
		interactionPattern: "action_required",
	})
	w.syncClerkMetadata(ctx, userID, user.SubscriptionTier, string(domain.SubscriptionStatusPastDue))

	// #1371 — Task 6, Part B: clear any lingering over-capacity grace flag — past_due
	// is a read-only status, so grace is moot (members already fall back to their own
	// tier via the resolver's unchanged owner-read-only branch).
	w.clearOverCapacityIfOwner(ctx, userID)

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

	w.createNotification(ctx, notificationParams{
		userID:             userID,
		notificationType:   "subscription_trialing",
		title:              "Trial Started",
		message:            "Your BoostedBudget trial is now active. Enjoy full access during your trial period!",
		interactionPattern: "informational",
	})
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

	w.createNotification(ctx, notificationParams{
		userID:             txn.UserID,
		notificationType:   "payment_failed",
		title:              "Payment Failed",
		message:            "We couldn't process your payment. Please update your payment method to keep your subscription active.",
		actionURL:          stringPtr("/billing"),
		actionLabel:        stringPtr("Update Payment"),
		interactionPattern: "action_required",
	})

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

	w.createNotification(ctx, notificationParams{
		userID:             txn.UserID,
		notificationType:   "transaction_past_due",
		title:              "Payment Past Due",
		message:            "Your subscription payment is past due. Please update your payment method to avoid service interruption.",
		actionURL:          stringPtr("/billing"),
		actionLabel:        stringPtr("Update Payment"),
		interactionPattern: "action_required",
	})

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

	w.createNotification(ctx, notificationParams{
		userID:             txn.UserID,
		notificationType:   "payment_failed",
		title:              "Transaction Canceled",
		message:            "Your transaction was canceled. Contact support if you have any questions.",
		interactionPattern: "informational",
	})

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
