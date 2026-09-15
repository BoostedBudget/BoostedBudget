// Package jobs contains River job definitions and workers for async processing.
package jobs

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/maxnussbaum/BoostedBudget/internal/config"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	phclient "github.com/maxnussbaum/BoostedBudget/internal/integration/posthog"
	"github.com/maxnussbaum/BoostedBudget/internal/observability"
	"github.com/maxnussbaum/BoostedBudget/internal/queue"
	"github.com/maxnussbaum/BoostedBudget/internal/repository"
	"github.com/maxnussbaum/BoostedBudget/internal/services"
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
	// #930: Retention tracking — both dates are computed in Postgres (clock_timestamp() +
	// make_interval); see ProfileRepository.SetRetentionDates's doc comment (profile_repository.go)
	// for why a Go-computed date is never passed in, and RetentionClockGuard's doc comment for why
	// the authorising condition is passed in by this caller rather than hardcoded in the repository.
	SetRetentionDates(ctx context.Context, userID string, guard repository.RetentionClockGuard, retentionMonths int) error
	ClearRetentionDates(ctx context.Context, userID string) error
	// #898: Dunning — past_due timestamp tracking
	SetPastDueStartedAt(ctx context.Context, userID string, t time.Time) error
	ClearPastDueStartedAt(ctx context.Context, userID string) error
	// ClearPlaidCleanupPendingIfMatches clears plaid_cleanup_pending_at ONLY if it still equals
	// expected — Issue #1413 round-12 Fix 1. Called by processSubscriptionCanceled once a
	// synchronous Plaid disconnect (and the retention-clock write) has actually SUCCEEDED, so the
	// marker no longer overstates outstanding cleanup for this episode. The CAS guard (not the
	// unconditional ClearPlaidCleanupPending) matters here for the same reason it matters for
	// repairPlaidDisconnects: an admin comp grant-then-removal between this flow's own re-read and
	// this call could otherwise start a NEW episode whose fresh marker this call must not erase.
	ClearPlaidCleanupPendingIfMatches(ctx context.Context, userID string, expected time.Time) (bool, error)
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
	// DisconnectAllUserItemsIfEligible disconnects every one of the user's Plaid items, invoking
	// recheck immediately before EVERY remaining item; the first (false, nil) stops the loop and
	// reports services.ErrCleanupAborted. See
	// services.PlaidItemCleanupService.DisconnectAllUserItemsIfEligible for the full contract.
	//
	// THERE IS DELIBERATELY NO nil-RECHECK COUNTERPART (Issue #1500). This interface used to declare
	// DisconnectAllUserItems and processSubscriptionCanceled was one of its last two callers
	// anywhere; both the declaration and the underlying service method are gone rather than merely
	// unused, because the failure mode being closed is silence — while a recheck-free entry point
	// exists, a future caller gets no per-item recheck by default and nothing complains.
	DisconnectAllUserItemsIfEligible(ctx context.Context, userID string, recheck func(ctx context.Context) (bool, error)) error
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

// subscriptionCancelledCleanupClaimLease is the lease duration requested for
// ActionSubscriptionCancelledCleanup — matches ExecuteDeletion's accountDeletionClaimLease and
// the deleted PastDueService's pastDueClaimLease (Issue #1413, #1652): this path also makes a Plaid call, and a
// generous lease means an over-run is detectable (via the fencing token) rather than silently
// re-granted to a second worker.
const subscriptionCancelledCleanupClaimLease = 15 * time.Minute

// SubscriptionWebhookWorker processes Paddle subscription webhook events from the queue.
type SubscriptionWebhookWorker struct {
	revenueCatAuthority bool
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
	// claimsRepo backs the durable work-claims mechanism processSubscriptionCanceled requires for
	// EXCLUSIVITY on ActionSubscriptionCancelledCleanup before the Plaid disconnect / retention-
	// clock side effects (Issue #1413 High finding H7, Codex review round 4). See
	// SetWorkClaimsRepo — like DeletionService's, this is not an optional capability.
	//
	// Typed as the wider WorkClaimsOutcomeRepository (round-3 finding H3), not the base
	// WorkClaimsRepository other #1413 callers use: this worker MUST distinguish "another
	// worker holds this lease right now" (retryable) from "already completed" (terminal) —
	// see processSubscriptionCanceled's ClaimOutcome switch below.
	claimsRepo repository.WorkClaimsOutcomeRepository
	logger     *slog.Logger
}

// SetRevenueCatAuthority retains verified Paddle receipts and transaction audits,
// but prevents Paddle subscription events from independently changing access.
// Configure before starting workers.
func (w *SubscriptionWebhookWorker) SetRevenueCatAuthority() {
	w.revenueCatAuthority = true
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

// SetWorkClaimsRepo wires the durable work-claims mechanism processSubscriptionCanceled requires
// for EXCLUSIVITY on ActionSubscriptionCancelledCleanup before its Plaid disconnect / retention-
// clock side effects (Issue #1413 High finding H7, Codex review round 4). Mirrors
// DeletionService.SetWorkClaimsRepo / the deleted PastDueService.SetWorkClaimsRepo — a nil claims repo makes
// that destructive path fail loudly rather than silently falling back to guard-only behavior.
//
// Takes the wider WorkClaimsOutcomeRepository (issue #1413 round-3 finding H3) — see the
// claimsRepo field doc comment. repository.NewWorkClaimsOutcomeRepository constructs a value
// that satisfies this; every other #1413 caller keeps using the narrower
// repository.NewWorkClaimsRepository / WorkClaimsRepository unchanged.
func (w *SubscriptionWebhookWorker) SetWorkClaimsRepo(repo repository.WorkClaimsOutcomeRepository) {
	w.claimsRepo = repo
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

// releaseClaimOnFailure releases a work claim and logs any release failure.
// Used to consolidate the repeated Release + Warn pattern in subscription-cancelled cleanup.
//
// userID and resourceID are both taken (rather than deriving one from the other) because, since
// Issue #1413 P5, resourceID is a composite "{userID}:{Paddle event ID}" built by
// repository.OccurrenceResourceID — userID stays available for clean, greppable log fields while
// resourceID is what actually goes into the Release call.
func (w *SubscriptionWebhookWorker) releaseClaimOnFailure(ctx context.Context, userID, resourceID, action, token, reason string) {
	if _, releaseErr := w.claimsRepo.Release(ctx, repository.ResourceTypeUser, resourceID, action, token, reason); releaseErr != nil {
		w.logger.Warn("failed to release work claim", "user_id", userID, "resource_id", resourceID, "action", action, "reason", reason, "error", releaseErr)
	}
}

// stringPtr returns a pointer to a string value.
func stringPtr(s string) *string { return &s }

// effectiveTierName returns owner's effective tier name, falling back to the raw SubscriptionTier
// when an active manual_tier_override names no tier configured in w.tierConfig (Issue #1413 —
// the tier axis of the malformed-override rule, this task). Mirrors
// HouseholdOverCapacityCheckWorker.effectiveTierName's identical rationale.
func (w *SubscriptionWebhookWorker) effectiveTierName(owner *domain.User) string {
	tier, overrideValue, hasOverride := owner.EffectiveTierWithActiveOverride()
	if hasOverride && w.tierConfig != nil {
		if _, err := w.tierConfig.GetTier(domain.TierName(overrideValue)); err != nil {
			w.logger.Warn("rejecting manual tier override on tier axis: value does not name a configured tier",
				"user_id", owner.ID, "manual_tier_override", overrideValue)
			return owner.SubscriptionTier
		}
	}
	return tier
}

// syncClerkMetadata updates a user's Clerk public metadata with their current EFFECTIVE tier and
// status. Logs and swallows errors so Clerk failures don't abort webhook processing.
//
// Re-reads the user AFTER the caller's UpdateSubscriptionFromPaddle write commits, so the row is
// fresh, and builds the payload from ClerkPublicMetadata() — the EFFECTIVE tier/status —
// computed from that fresh row, rather than from whatever raw values this webhook just wrote.
// This matters because a comped user (active manual_tier_override) must not have their comp
// clobbered by a Paddle webhook's raw values (Issue #1413 Defect 3).
//
// If the re-read fails, the sync attempt is abandoned for this call: without a fresh row we know
// neither the user's clerk_user_id nor their current effective tier/status, so there is nothing
// safe to sync. The next webhook or sync opportunity retries.
func (w *SubscriptionWebhookWorker) syncClerkMetadata(ctx context.Context, userID string) {
	// Billing failures remain auditable, but legacy profile fields must not be
	// published as an independent access projection once RevenueCat is authoritative.
	if w.revenueCatAuthority {
		return
	}
	if w.clerkService == nil {
		return
	}

	fresh, err := w.profileRepo.GetUserByID(ctx, userID)
	if err != nil {
		w.logger.Error("failed to get user for Clerk sync; skipping sync", "user_id", userID, "error", err)
		return
	}
	if fresh.ClerkUserID == "" {
		return
	}

	// Issue #1413 (this task — tier axis, plus a status-axis leak specific to this call site):
	// manual_tier_override is an unconstrained VARCHAR(50) with no CHECK constraint, so its
	// value can be garbage. domain.User.ClerkPublicMetadata() cannot validate it itself (domain
	// must not import config), so it would otherwise write the raw garbage tier string AND a
	// forced "active" status (its computeEffectiveStatusAt treats ANY live override as a comp,
	// valid or not) straight into Clerk's publicMetadata. Sanitize a scratch copy here — a
	// rejected override is nulled out entirely so ClerkPublicMetadata()'s own computation falls
	// back to the raw subscription_tier/status, exactly like every other representation path
	// this task fixes.
	scratch := *fresh
	if _, overrideValue, hasOverride := fresh.EffectiveStatusWithActiveOverride(); hasOverride && w.tierConfig != nil {
		if _, tierErr := w.tierConfig.GetTier(domain.TierName(overrideValue)); tierErr != nil {
			w.logger.Warn("rejecting manual tier override in clerk metadata sync: value does not name a configured tier",
				"user_id", userID, "manual_tier_override", overrideValue)
			scratch.ManualTierOverride = nil
			scratch.ManualTierEndsAt = nil
		}
	}

	if err := w.clerkService.UpdateUserMetadata(ctx, fresh.ClerkUserID, scratch.ClerkPublicMetadata()); err != nil {
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
// effective-tier seat count, and either starts or clears the
// over_capacity_since grace clock via the idempotent SetOverCapacity/ClearOverCapacity
// repository methods (Task 1). Every write here is safe to call twice —
// SetOverCapacity's CTE only executes the UPDATE on a real NULL-to-value transition,
// so a no-op once grace is already running returns the already-set value without
// overwriting it, and ClearOverCapacity is a plain idempotent null-out.
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
	// Issue #1413 (this task — tier axis): a malformed manual_tier_override is benign here by
	// explicit design (this method's own doc comment above: callers only log, never propagate,
	// its error) — made consistent with every other tierConfig.GetTier(...ComputeEffectiveTier())
	// call site in this codebase anyway, since the fix was already at hand (w.effectiveTierName).
	tier, err := w.tierConfig.GetTier(domain.TierName(w.effectiveTierName(owner)))
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

	// Issue #1514: this guard reads webhook.SignatureValid and refuses to dispatch when it is
	// false. See HandlePaddleWebhook and ReplayPaddleWebhook for the primary guards.
	//
	// A retry re-fetches the row via GetByID and re-reads SignatureValid; nothing in this file
	// modifies that field, so it cancels the job via river.JobCancel rather than returning nil.
	// In River v0.43.0, returning nil from Work is *successful completion*, not cancellation (see
	// river.JobCancel's doc comment: it "cancel[s] the job at the end of execution"); returning
	// nil here would record a refused webhook as a completed job and emit
	// jobs_completed_total{status="success"} for it.
	//
	// No analytics.Capture here: EventID and EventType are supplied by the caller and are not
	// validated by this guard; emitting them to PostHog would ship unverified data into
	// analytics. See webhook_processing.go's guard for the analogous Plaid-side handling.
	if !webhook.SignatureValid {
		observability.InvalidWebhookAttemptsTotal.WithLabelValues("paddle_worker_guard").Inc()
		// Logs only webhook_id, which this function uses solely as a lookup key (GetByID,
		// MarkFailed) and never reads as content. event_id and event_type are Paddle's own
		// payload values, supplied by the caller, and are omitted from this log.
		w.logger.Error("refusing to process Paddle webhook with invalid signature; cancelling job",
			"webhook_id", args.WebhookID,
		)
		if failErr := w.paddleWebhookRepo.MarkFailed(ctx, args.WebhookID, "rejected: signature_valid=false"); failErr != nil {
			// Persistence failed: return a retryable error instead of swallowing it.
			w.logger.Error("failed to mark Paddle webhook as failed after signature guard; will retry",
				"webhook_id", args.WebhookID,
				"error", failErr,
			)
			return fmt.Errorf("failed to record invalid-signature refusal: %w", failErr)
		}
		return river.JobCancel(errInvalidWebhookSignature)
	}

	if w.revenueCatAuthority && strings.HasPrefix(webhook.EventType, "subscription.") {
		if err := w.paddleWebhookRepo.MarkProcessed(ctx, args.WebhookID); err != nil {
			return fmt.Errorf("record Paddle billing receipt: %w", err)
		}
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

	w.syncClerkMetadata(ctx, userID)

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
	w.syncClerkMetadata(ctx, userID)

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
	w.syncClerkMetadata(ctx, userID)

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

// cancellationCleanupStillAuthorised re-asks, against the freshest possible read, the ONE question
// every destructive act in processSubscriptionCanceled is authorised by: is this user STILL
// cancelled, and is there STILL no active compensating override? Both conjuncts must hold; a read
// error is fail-closed (false plus the error), never an optimistic "carry on".
//
// It exists as a single named function — rather than being written out at each consumer — because
// this flow now asks it from TWO places that must never be able to disagree: the per-item recheck
// handed to DisconnectAllUserItemsIfEligible, and the gate immediately before SetRetentionDates.
// Two hand-written copies of an authorisation question is exactly the drift this branch has had to
// repair repeatedly; `expiredRetentionClockPredicate` (profile_repository.go) is the local
// precedent for splicing one definition into both consumers instead.
//
// See the long comment at the disconnect call site for WHY these are the two conjuncts, and why
// occurrence identity is deliberately not a third.
func (w *SubscriptionWebhookWorker) cancellationCleanupStillAuthorised(ctx context.Context, userID string) (bool, error) {
	fresh, readErr := w.profileRepo.GetUserByID(ctx, userID)
	if readErr != nil {
		return false, fmt.Errorf("failed to re-read user %s during subscription-cancelled cleanup: %w", userID, readErr)
	}
	if fresh.SubscriptionStatus != string(domain.SubscriptionStatusCancelled) {
		return false, nil
	}
	return !fresh.HasActiveManualOverride(), nil
}

// processSubscriptionCanceled handles subscription.canceled and subscription.paused events.
// Pause is not supported (#933); paused subscriptions are treated as terminal cancellations.
// Issue #1413 High finding H7: a real work claim on ActionSubscriptionCancelledCleanup wraps
// Plaid disconnect and retention writes for EXCLUSIVITY; unconditional writes (cancelled status,
// Clerk sync, clear past_due) stay outside the claim as they record billing truth.
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

	// Preserve paddle_subscription_id (needed for resubscribe flow). Unconditional — billing truth.
	if err := w.profileRepo.UpdateSubscriptionFromPaddle(ctx, userID, user.SubscriptionTier, "cancelled", nil, nil, nil); err != nil {
		return fmt.Errorf("failed to update subscription status: %w", err)
	}

	// Re-read AFTER the write commits (mirrors the deleted PastDueService.reReadStillPastDue's established
	// re-read-before-act convention) so the destructive-side-effect decision below is based on
	// fresh state, not the stale snapshot from before the write. A failed re-read fails the whole
	// webhook (River retries) rather than silently falling back to the stale `user`.
	freshUser, err := w.profileRepo.GetUserByID(ctx, userID)
	if err != nil {
		return fmt.Errorf("failed to re-read user after cancellation write: %w", err)
	}

	// Unconditional trio (billing truth / non-destructive) — always runs regardless of the claim
	// outcome below, per the H7 comment above.
	w.syncClerkMetadata(ctx, userID)
	if clearErr := w.profileRepo.ClearPastDueStartedAt(ctx, userID); clearErr != nil {
		w.logger.Warn("failed to clear past_due_started_at on cancellation", "user_id", userID, "error", clearErr)
	}
	if w.analytics != nil {
		w.analytics.Capture(userID, phclient.EventSubscriptionCancelled, nil)
	}
	// #1371 — Task 6, Part B: clear any lingering over-capacity grace flag — the owner just lost
	// active coverage entirely, so grace is moot (members already fall back to their own tier via
	// the resolver's unchanged owner-read-only branch).
	w.clearOverCapacityIfOwner(ctx, userID)

	// #1413: a comped user can still carry a paddle_subscription_id (the admin comp never clears
	// it), so this cancellation webhook is fully reachable for them. The comp is an access-layer
	// overlay, not a rewriting of billing truth — the destructive side effects (Plaid disconnect,
	// retention clock) below are gated on the comp, but everything above stays unconditional.
	if freshUser.HasActiveManualOverride() {
		return nil
	}

	if w.claimsRepo == nil {
		return fmt.Errorf("processSubscriptionCanceled: work claims repository not configured")
	}
	// Issue #1413 P5: the claim key must identify not just WHO (userID) and WHAT
	// (ActionSubscriptionCancelledCleanup) but WHICH OCCURRENCE — otherwise cancel -> resubscribe
	// -> cancel again would find this (user, action) pair already permanently completed from the
	// FIRST cancellation, and this webhook would be silently acked without ever disconnecting
	// Plaid or starting the retention clock on the second cancellation. webhook.EventID is
	// Paddle's own idempotency key for this exact event (paddle_webhook_events.event_id is
	// NOT NULL UNIQUE — see migration 000043): a redelivery of the SAME event reuses the same
	// EventID (so a retry still collides, which is the whole point of the claim), while a
	// genuinely new cancellation is a different Paddle event with a different EventID (even if
	// it is the same user, or even the same subscription_id being cancelled a second time after a
	// resubscribe).
	resourceID, err := repository.OccurrenceResourceID(userID, webhook.EventID)
	if err != nil {
		return fmt.Errorf("processSubscriptionCanceled: %w", err)
	}
	// #1413 round-3 finding H3: ClaimOutcome (not bare Claim) because this caller — unlike the
	// daily past-due/retention cron jobs, which re-run tomorrow regardless — MUST NOT treat
	// "another worker holds this lease right now" the same as "already completed." Collapsing
	// both into a skip here would get THIS webhook delivery marked processed even when the
	// in-flight holder later crashes: nothing else would ever retrigger the Plaid disconnect or
	// retention-clock write for this user again.
	token, outcome, err := w.claimsRepo.ClaimOutcome(ctx, repository.ResourceTypeUser, resourceID, repository.ActionSubscriptionCancelledCleanup, subscriptionCancelledCleanupClaimLease)
	if err != nil {
		return fmt.Errorf("failed to claim subscription-cancelled cleanup for user %s: %w", userID, err)
	}
	switch outcome {
	case repository.ClaimAlreadyCompleted:
		// Terminal: the cleanup already ran to completion for this user. A genuine skip.
		w.logger.Info("subscription-cancelled cleanup already completed, skipping", "user_id", userID)
		return nil
	case repository.ClaimHeldByOther:
		// Retryable, NOT a skip: another worker holds an unexpired, incomplete lease right now.
		// Return an error so River retries the whole webhook — by the time the retry lands, the
		// lease will have either expired (crashed holder, now reclaimable) or completed (finished
		// holder, now ClaimAlreadyCompleted above). Acking this webhook here instead would silently
		// drop the Plaid disconnect / retention-clock work if the current holder crashed.
		w.logger.Warn("subscription-cancelled cleanup claim held by another in-flight worker; retrying webhook rather than acking",
			"user_id", userID)
		return fmt.Errorf("subscription-cancelled cleanup claim held by another in-flight worker for user %s", userID)
	}

	// Re-verify the comp predicate against the freshest possible read, immediately after winning
	// the claim and before the Plaid disconnect / retention-clock writes — already as late as
	// this check can be made; it sits directly above the Plaid disconnect call below, with only
	// comments and log statements in between.
	//
	// Issue #1413 round-4 finding H2: a comp granted in the window between this re-check and the
	// Plaid disconnect call is a DOCUMENTED, ACCEPTED residual, not something further tightening
	// here can close — no database mechanism can make that window zero, and a worker already
	// inside a Plaid call cannot be recalled. Unlike C1's residual (irreversible account
	// deletion), this one is acceptable: a Plaid disconnect is recoverable (the user relinks),
	// and the retention clock this run is about to start is cleared the moment the comp is
	// applied — inconvenience, not data loss. See the deleted past_due_service.go's processGraceExpiry for
	// the matching rationale on that flow's own equivalent re-check.
	reVerified, err := w.profileRepo.GetUserByID(ctx, userID)
	if err != nil {
		w.releaseClaimOnFailure(ctx, userID, resourceID, repository.ActionSubscriptionCancelledCleanup, token, err.Error())
		return fmt.Errorf("failed to re-verify user %s before subscription-cancelled cleanup: %w", userID, err)
	}
	if reVerified.HasActiveManualOverride() {
		w.releaseClaimOnFailure(ctx, userID, resourceID, repository.ActionSubscriptionCancelledCleanup, token, "precondition lost — comp granted since last read")
		w.logger.Info("subscription-cancelled cleanup skipped — comp granted since last read", "user_id", userID)
		return nil
	}

	// Issue #896: Disconnect Plaid items to stop billing on cancellation. A failure here now
	// ABORTS (Release + propagate) instead of being logged and swallowed — the work claim exists
	// so this can be safely retried (Release makes it immediately reclaimable) rather than
	// orphaning a still-connected, still-billing Plaid item with no automatic retry.
	//
	// Issue #1413 H1: the disconnect can return services.ErrSideEffectClaimBusy when
	// another flow (the past-due grace-cancel cron, the Plaid-disconnect window pass, or another
	// cancellation webhook delivery) currently holds the OPERATION-keyed
	// ActionPlaidDisconnectUser claim for this user. Logged distinctly at Info — this is expected
	// contention, not a Plaid failure — but this webhook delivery is one-shot (unlike the daily
	// cron jobs), so it must still release this flow claim and return an ERROR so River retries
	// the whole webhook, exactly like the ClaimHeldByOther handling above for the flow claim
	// itself.
	//
	// Issue #1500: stillEligible is the PER-ITEM recheck, and passing it is the point. This call site
	// used the nil-recheck entry point until now, so the comp re-verification directly above covered
	// the whole loop rather than each item in it — and the loop is one Plaid /item/remove per linked
	// item, so a comp granted after the first item was removed still cost the user every remaining
	// bank connection.
	//
	// IT RE-ASKS THIS FLOW'S OWN AUTHORISING CONDITION — NOT the past-due question the dunning flows
	// re-ask. Nothing in this flow selects on past_due (it CLEARS past_due_started_at
	// unconditionally, further up), so re-asking that here would be inventing an authorisation rule
	// rather than re-asking the one this call was authorised under.
	//
	// That condition has TWO conjuncts, and the predicate must re-ask BOTH (Codex pass 7, Medium):
	//
	//  1. No active compensating override — the comp/entitlement question `reVerified.
	//     HasActiveManualOverride()` answers immediately above.
	//  2. The user is STILL CANCELLED. This one is the authorising state itself, and re-asking it
	//     invents nothing: this handler's own UpdateSubscriptionFromPaddle write (far above) put the
	//     row into 'cancelled', and every destructive act below exists only because of it. Webhook
	//     jobs deliberately carry NO River uniqueness options (unique_opts_by_kind.go) — webhooks
	//     must never be deduplicated away — so a later subscription.activated/.updated delivery can
	//     legitimately restore an active or trialing status WHILE this older cancellation job is
	//     still walking the user's Items. Asking only conjunct 1 let that stale job keep
	//     disconnecting the remaining Items of a now-active, paying customer, and then write
	//     six-month retention/deletion dates over the top. Compare
	//     the deleted PastDueService.reReadStillPastDue, which likewise re-asks status AND override AND the
	//     exact occurrence.
	//
	// Occurrence identity is NOT re-asked here, unlike reReadStillPastDue: this flow's occurrence is
	// webhook.EventID (Paddle's own idempotency key, used for the work-claim resourceID above), which
	// is not a column on the user row, and the Paddle subscription id in the payload is not a
	// dependable per-item conjunct (paddle_subscription_id is preserved-not-rewritten on
	// cancellation, and may be NULL for legacy rows, so comparing it would fail CLOSED and silently
	// skip legitimate cleanups). The cancel→resubscribe→cancel-again interleaving that occurrence
	// identity would catch is already harmless once conjunct 2 holds: the reactivation flips the
	// status and stops this job, and the SECOND cancellation is a different Paddle event with its own
	// claim doing exactly the same work.
	//
	// A read error is fail-closed: cleanupItems stops the loop and surfaces the error rather than
	// continuing.
	stillEligible := func(rctx context.Context) (bool, error) {
		return w.cancellationCleanupStillAuthorised(rctx, userID)
	}
	if w.plaidCleanupSvc != nil {
		if err := w.plaidCleanupSvc.DisconnectAllUserItemsIfEligible(ctx, userID, stillEligible); err != nil {
			// An authorisation-triggered abort is not a Plaid failure and must not be reported as one.
			// services.ErrCleanupAborted has TWO causes (see its doc comment) demanding opposite
			// handling, so the predicate is asked once more to say which: this flow's authorisation
			// was lost mid-loop — a comp was granted, or the subscription became active again — which
			// is a legitimate stop (release the claim and return nil, WITHOUT starting the retention
			// clock below, exactly as the pre-loop comp branch above returns nil without it) versus a
			// new linked item having appeared mid-pass (genuinely incomplete work — falls through to
			// the error path, so River retries the whole webhook). An error from the recheck itself
			// falls through too, which is the fail-closed direction. Same shape as
			// the deleted PastDueService.processGraceExpiry and DeletionService.ExecuteDeletion.
			if errors.Is(err, services.ErrCleanupAborted) {
				if eligible, checkErr := stillEligible(ctx); checkErr == nil && !eligible {
					w.releaseClaimOnFailure(ctx, userID, resourceID, repository.ActionSubscriptionCancelledCleanup, token, "cancellation cleanup no longer authorised mid Plaid disconnect (comp granted, or subscription no longer cancelled) — remaining items left connected")
					w.logger.Info("subscription-cancelled cleanup stopped partway through the Plaid disconnect — it is no longer authorised (a comp was granted, or the subscription is no longer cancelled); the remaining items stay connected and no retention clock is started",
						"user_id", userID)
					return nil
				}
			}
			if errors.Is(err, services.ErrSideEffectClaimBusy) {
				w.logger.Info("subscription-cancelled Plaid disconnect deferred — side-effect claim held by another flow",
					"user_id", userID)
			}
			w.releaseClaimOnFailure(ctx, userID, resourceID, repository.ActionSubscriptionCancelledCleanup, token, err.Error())
			return fmt.Errorf("failed to disconnect Plaid items on cancellation for user %s: %w", userID, err)
		}
	}

	// #930: Start 6-month retention clock — data preserved read-only until auto-deletion.
	// #1413 round-3 finding H5: this write is REQUIRED, not best-effort. Both the retention-
	// notice jobs and hard deletion require retention_expires_at IS NOT NULL — a swallowed
	// failure here left this user cancelled with no clock: never warned, never deleted,
	// indefinitely. Release (not Complete) and propagate so River retries the whole webhook;
	// unlike the past-due grace path, nothing here gates re-entry on subscription_status, so a
	// retry naturally reaches this write again.
	//
	// Gated on one last evaluation of this flow's authorising condition, immediately before the write
	// (Issue #1500 residual). Until now this write was protected ONLY through the disconnect loop's
	// abort path — the predicate was asked once per linked Item — so the two populations for whom the
	// loop body never executes got no protection at all: a user with ZERO linked Items (common: plenty
	// of accounts have no bank connections), and a deployment with no Plaid cleanup collaborator wired.
	// For them a reactivation, or a comp, landing while this stale cancellation job was in flight still
	// stamped six-month retention/deletion dates onto an active, paying customer. The retention
	// deletion claim now re-verifies subscription status, so such a user cannot be hard-deleted on the
	// strength of these dates, but the stored state was still wrong and other consumers read those
	// columns (retention notices, the retention-clock repair pass).
	//
	// Deliberately NOT gated on w.plaidCleanupSvc != nil: whether the retention clock may be armed is a
	// question about this user's billing state, not about whether Plaid happens to be configured.
	// Coupling the two would be the same mistake as gating a GDPR retention limit on a feature flag.
	//
	// Same three outcomes as the loop's own abort handling, for the same reasons: no longer authorised
	// is a legitimate stop (release the claim, return nil, write nothing — like the pre-loop comp branch
	// above); a failed read is fail-closed (release and propagate so River retries the whole webhook);
	// authorised proceeds. Stopping here also skips the marker clear below, exactly as the mid-loop
	// abort does — a run that lost its authorisation leaves plaid_cleanup_pending_at for the repair
	// pass rather than declaring this episode finished.
	authorised, authErr := w.cancellationCleanupStillAuthorised(ctx, userID)
	if authErr != nil {
		w.releaseClaimOnFailure(ctx, userID, resourceID, repository.ActionSubscriptionCancelledCleanup, token, authErr.Error())
		return fmt.Errorf("failed to re-verify user %s before starting the retention clock: %w", userID, authErr)
	}
	if !authorised {
		w.releaseClaimOnFailure(ctx, userID, resourceID, repository.ActionSubscriptionCancelledCleanup, token, "cancellation cleanup no longer authorised before the retention-clock write (comp granted, or subscription no longer cancelled) — no retention dates written")
		w.logger.Info("subscription-cancelled retention clock not started — the cleanup is no longer authorised (a comp was granted, or the subscription is no longer cancelled)",
			"user_id", userID)
		return nil
	}
	//
	// The gate above narrowed that window to roughly one Go statement; it did not close it, and no
	// read-then-write gate can. The write's OWN guard is what closes it: SetRetentionDates is handed
	// THIS flow's authorising condition — still 'cancelled' — and re-evaluates it inside the
	// statement, against committed state (see RetentionClockGuard, repository/profile_repository.go).
	// Both are load-bearing: the gate stops the WORK (the per-item Plaid loop, which SQL cannot
	// short-circuit), the guard stops the WRITE.
	//
	// A lost precondition (ErrRetentionClockPreconditionLost) is therefore handled EXACTLY as the
	// gate's own negative outcome one line above — release the claim, log, return nil, write nothing
	// — because it is the same event, merely detected microseconds later: an activation or comp
	// landed while this stale cancellation job was in flight. Returning an error instead would make
	// River retry a webhook whose whole purpose has been overtaken. Every other error stays a real
	// failure and propagates.
	if retErr := w.profileRepo.SetRetentionDates(ctx, userID, repository.RetentionClockRequiresStatus(domain.SubscriptionStatusCancelled), w.retentionMonths); retErr != nil {
		if errors.Is(retErr, repository.ErrRetentionClockPreconditionLost) {
			w.releaseClaimOnFailure(ctx, userID, resourceID, repository.ActionSubscriptionCancelledCleanup, token, "cancellation cleanup no longer authorised at the retention-clock write itself (the subscription is no longer cancelled) — no retention dates written")
			w.logger.Info("subscription-cancelled retention clock not started — the write's own guard found the subscription is no longer cancelled",
				"user_id", userID)
			return nil
		}
		w.releaseClaimOnFailure(ctx, userID, resourceID, repository.ActionSubscriptionCancelledCleanup, token, retErr.Error())
		return fmt.Errorf("failed to set retention dates on cancellation for user %s: %w", userID, retErr)
	}

	// Issue #1413 round-12 Fix 1 (Blocker): a SUCCESSFUL synchronous Plaid disconnect (above) means
	// cleanup for THIS episode is already done — per the invariant
	// (domain.User.PlaidCleanupPendingAt's doc comment), the marker must go back to NULL, or it
	// permanently overstates "cleanup outstanding" for a user whose Plaid items are already
	// disconnected.
	//
	// Correction (Issue #1413 Finding 1, Codex review, PR #1443 follow-up, headline finding): this
	// comment used to claim a stuck marker here "can never cause a WRONG disconnect (there is nothing
	// left to disconnect)". That is FALSE, and was the exact belief Finding 1 overturned. A stuck
	// marker CAN cause a wrong disconnect later, through a different user action entirely: if this
	// clear (below) never succeeds, the stale marker survives, and — at the time this was found — an
	// admin could grant a comp, the user could relink a bank (a genuinely NEW live item, under the OLD
	// stale timestamp), and removing the comp resurrected that stale timestamp instead of starting a
	// fresh episode via removeUserTierOverrideUpdate's then-COALESCE (admin_user_repository.go), so
	// repairPlaidDisconnects's next pass saw a live item paired with an already-expired grace window
	// and disconnected the newly-linked bank immediately, with zero grace. ROUND 18
	// (admin_user_repository.go) independently removed that COALESCE, so a comp granted and removed
	// over this exact stuck marker no longer resurrects it either way — but a stuck marker on a user
	// no comp is EVER granted over gets no help from that fix.
	// RetentionCleanupCheckWorker.repairPlaidDisconnects's own self-healing backstop
	// (ClearCompletedPlaidCleanupMarkers, profile_repository.go) is what actually closes THAT
	// remaining population — see its doc comment for the full scenario — rather than the stuck
	// marker being inert on its own.
	//
	// Gated on w.plaidCleanupSvc != nil: a nil collaborator means NO disconnect call was made at all
	// (Issue #896's existing nil-guard, tested by TestWorker_SubscriptionCanceled_PlaidCleanupNil_
	// Succeeds) — cleanup is genuinely still outstanding in that case, so the marker must stay set for
	// RetentionCleanupCheckWorker.repairPlaidDisconnects to pick up later. A failed disconnect already
	// returned above (before reaching this line), so a stuck marker after a FAILED attempt is exactly
	// the existing, correct "repair pass will retry" behavior — untouched by this fix.
	//
	// Uses freshUser.PlaidCleanupPendingAt (captured by the re-read right after THIS call's own
	// UpdateSubscriptionFromPaddle write, before the comp/claim logic above) as the CAS "expected"
	// value — ClearPlaidCleanupPendingIfMatches, not the unconditional ClearPlaidCleanupPending, so a
	// brand-new episode's marker (e.g. an admin comp granted then removed between that re-read and
	// this point) survives untouched rather than being erased. Losing this write cannot cause "never
	// warned, never deleted" — unlike SetRetentionDates above — but leaving it set FOREVER is real
	// phantom residue (#1453), not harmless: since the disconnect that just succeeded removed the
	// very live linked item GetUsersNeedingPlaidDisconnectRepair's own EXISTS check requires,
	// repairPlaidDisconnects can never reselect this user again to retry the clear either. Issue #1413
	// #1453 (Codex review, PR #1443 follow-up): a single, swallowed clear attempt used to leave the
	// marker stuck non-NULL on any transient DB error at exactly this moment — bring this into line
	// with the identical fix already applied to RetentionCleanupCheckWorker.repairPlaidDisconnects's
	// own marker clear (retention_cleanup_check.go, same package): retry up to
	// plaidCleanupMarkerClearMaxAttempts times, then Error-log (not Warn) plus the shared failure
	// counter if every attempt still fails. Still not escalated to fail the whole webhook — this
	// handler's cancellation/retention writes already succeeded regardless of whether the marker
	// clears, and retrying the whole webhook would just redo an already-idempotent disconnect for no
	// benefit.
	if w.plaidCleanupSvc != nil && freshUser.PlaidCleanupPendingAt != nil {
		expectedMarker := *freshUser.PlaidCleanupPendingAt
		var cleared bool
		var clearErr error
		for attempt := 1; attempt <= plaidCleanupMarkerClearMaxAttempts; attempt++ {
			cleared, clearErr = w.profileRepo.ClearPlaidCleanupPendingIfMatches(ctx, userID, expectedMarker)
			if clearErr == nil {
				break
			}
			if attempt < plaidCleanupMarkerClearMaxAttempts {
				w.logger.Warn("failed to clear plaid_cleanup_pending_at after subscription-cancelled Plaid disconnect — retrying",
					"user_id", userID, "attempt", attempt, "error", clearErr)
				time.Sleep(plaidCleanupMarkerClearRetryDelay)
			}
		}
		if clearErr != nil {
			w.logger.Error("Plaid disconnect succeeded but failed to clear plaid_cleanup_pending_at after all retries — this user's Plaid items are already disconnected, so GetUsersNeedingPlaidDisconnectRepair's live-item requirement will never match them again; the marker may now be stuck non-NULL permanently unless cleared through another path",
				"user_id", userID, "attempts", plaidCleanupMarkerClearMaxAttempts, "error", clearErr)
			observability.PlaidDisconnectRepairUserFailuresTotal.Inc()
		} else if !cleared {
			w.logger.Info("Plaid disconnect succeeded, but plaid_cleanup_pending_at had already moved on to a new episode — leaving the new episode's marker untouched",
				"user_id", userID)
		}
	}

	// Mark the claim durably complete. A false ok (no error) means our lease was reclaimed by
	// another worker while we were doing the work above — that worker may have ALSO run the Plaid
	// disconnect concurrently, which is precisely what this mechanism exists to make visible
	// instead of silent.
	completed, err := w.claimsRepo.Complete(ctx, repository.ResourceTypeUser, resourceID, repository.ActionSubscriptionCancelledCleanup, token)
	if err != nil {
		w.logger.Error("failed to mark subscription-cancelled cleanup claim complete", "user_id", userID, "action", repository.ActionSubscriptionCancelledCleanup, "error", err)
	} else if !completed {
		w.logger.Error("subscription-cancelled cleanup claim lease lost mid-flight — Plaid disconnect/retention may have run twice",
			"user_id", userID, "action", repository.ActionSubscriptionCancelledCleanup)
	}

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
	w.syncClerkMetadata(ctx, userID)

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
	w.syncClerkMetadata(ctx, userID)

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

	w.syncClerkMetadata(ctx, txn.UserID)

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
