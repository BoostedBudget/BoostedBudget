// Package services provides business logic and external service integrations.
// This file implements PastDueService for dunning grace-expiry and Plaid-disconnect logic.
// Issue #898: past_due lifecycle management.
package services

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
)

// ErrPaddleAlreadyCancelled is returned by PastDuePaddleClient.CancelSubscription when
// the subscription is already in a cancelled state in Paddle. This error is tolerated —
// the flow continues because the local state still needs to be updated.
var ErrPaddleAlreadyCancelled = errors.New("paddle subscription already cancelled")

// PastDueProfileRepository defines the repository methods needed by PastDueService.
type PastDueProfileRepository interface {
	// GetUsersInPastDue returns users with subscription_status='past_due' and non-nil past_due_started_at.
	GetUsersInPastDue(ctx context.Context) ([]domain.User, error)

	// GetUserByID retrieves a user by their internal UUID (used for race-condition re-read).
	GetUserByID(ctx context.Context, userID string) (*domain.User, error)

	// CancelIfPastDue atomically flips subscription_status to 'cancelled' only if the user is
	// still 'past_due'. Returns (true, nil) if the flip happened, (false, nil) if the user
	// has already recovered (payment arrived between Plaid disconnect and this point).
	CancelIfPastDue(ctx context.Context, userID string) (bool, error)

	// SetRetentionDates starts the 6-month retention clock.
	SetRetentionDates(ctx context.Context, userID string, started, expires time.Time) error

	// ClearPastDueStartedAt clears the past_due_started_at timestamp after auto-cancel.
	ClearPastDueStartedAt(ctx context.Context, userID string) error
}

// PastDuePlaidCleanup defines the Plaid cleanup operation needed by PastDueService.
type PastDuePlaidCleanup interface {
	// DisconnectAllUserItems calls Plaid /item/remove for all items
	// then marks them as 'disconnected' (preserves transaction history).
	DisconnectAllUserItems(ctx context.Context, userID string) error
}

// PastDuePaddleClient defines the Paddle cancel operation needed by PastDueService.
// The implementation must tolerate paddle.ErrSubscriptionIsCanceledActionInvalid by
// mapping it to ErrPaddleAlreadyCancelled.
type PastDuePaddleClient interface {
	// CancelSubscription cancels the given Paddle subscription ID immediately.
	// Returns ErrPaddleAlreadyCancelled if already cancelled (caller should tolerate).
	CancelSubscription(ctx context.Context, subscriptionID string) error
}

// PastDueClerkClient defines the Clerk metadata update needed by PastDueService.
type PastDueClerkClient interface {
	// UpdateUserMetadata sets public metadata on a Clerk user.
	UpdateUserMetadata(ctx context.Context, clerkUserID string, metadata map[string]interface{}) error
}

// PastDueNotificationRepository defines the notification creation needed by PastDueService.
type PastDueNotificationRepository interface {
	// Create persists a new in-app notification.
	Create(ctx context.Context, n *domain.Notification) error
}

// PastDueService manages the dunning lifecycle for users with past_due subscriptions.
// It is called daily by the dunning cron job.
//
// Ordering contract (non-negotiable for retryability):
//   1. Disconnect Plaid — if this fails, stop; user stays past_due and retries tomorrow.
//   2. Cancel Paddle — tolerate already-cancelled.
//   3. Flip subscription_status → cancelled (CAS: only if still past_due).
//   4. Start retention clock.
//   5. Clear past_due_started_at.
//   6. Sync Clerk metadata + create notification.
type PastDueService struct {
	repo                PastDueProfileRepository
	plaidCleanup        PastDuePlaidCleanup
	paddleClient        PastDuePaddleClient
	clerkClient         PastDueClerkClient
	notificationRepo    PastDueNotificationRepository
	graceDays           int
	plaidDisconnectDays int
	retentionMonths     int
	logger              *slog.Logger
}

// NewPastDueService creates a new PastDueService.
//
//   - graceDays: days past_due before auto-cancel (typically 21).
//   - plaidDisconnectDays: days past_due before Plaid is pre-emptively disconnected (typically 14).
func NewPastDueService(
	repo PastDueProfileRepository,
	plaidCleanup PastDuePlaidCleanup,
	paddleClient PastDuePaddleClient,
	clerkClient PastDueClerkClient,
	notificationRepo PastDueNotificationRepository,
	graceDays, plaidDisconnectDays int,
) *PastDueService {
	return &PastDueService{
		repo:                repo,
		plaidCleanup:        plaidCleanup,
		paddleClient:        paddleClient,
		clerkClient:         clerkClient,
		notificationRepo:    notificationRepo,
		graceDays:           graceDays,
		plaidDisconnectDays: plaidDisconnectDays,
		retentionMonths:     6,
		logger:              slog.Default(),
	}
}

// SetPlaidCleanup wires in the Plaid cleanup service (called after Plaid is configured).
func (s *PastDueService) SetPlaidCleanup(svc PastDuePlaidCleanup) {
	s.plaidCleanup = svc
}

// GetUsersInPastDue returns users whose subscription_status is 'past_due'
// and whose past_due_started_at is non-nil.
func (s *PastDueService) GetUsersInPastDue(ctx context.Context) ([]domain.User, error) {
	return s.repo.GetUsersInPastDue(ctx)
}

// ProcessPastDueUser evaluates one user's dunning state and takes action if thresholds are met.
//
// now is the current wall-clock time (injected so callers can test with arbitrary clocks).
func (s *PastDueService) ProcessPastDueUser(ctx context.Context, user domain.User, now time.Time) error {
	if user.PastDueStartedAt == nil {
		// Guard: nothing to do for users without a stamp.
		return nil
	}

	age := now.Sub(*user.PastDueStartedAt)
	graceDuration := time.Duration(s.graceDays) * 24 * time.Hour
	plaidDisconnectDuration := time.Duration(s.plaidDisconnectDays) * 24 * time.Hour

	switch {
	case age >= graceDuration:
		return s.processGraceExpiry(ctx, user, now)
	case age >= plaidDisconnectDuration:
		return s.processPlaidDisconnectWindow(ctx, user)
	default:
		// Too early — no action.
		return nil
	}
}

// processGraceExpiry handles auto-cancellation when grace period has elapsed.
// Ordering: Plaid → Paddle → CAS flip → retention → clear stamp → notify.
func (s *PastDueService) processGraceExpiry(ctx context.Context, user domain.User, now time.Time) error {
	s.logger.Info("past_due grace period expired, auto-cancelling",
		"user_id", user.ID,
		"past_due_started_at", user.PastDueStartedAt,
	)

	// Step 1: Disconnect Plaid FIRST.
	// If this fails, return the error so the caller (cron job) retries tomorrow.
	// The user stays past_due — no partial cancellation.
	if s.plaidCleanup != nil {
		if err := s.plaidCleanup.DisconnectAllUserItems(ctx, user.ID); err != nil {
			return fmt.Errorf("failed to disconnect Plaid items for past_due user %s: %w", user.ID, err)
		}
	}

	// Step 2: Cancel Paddle subscription.
	// Tolerate already-cancelled — the subscription may have already been cancelled
	// via Paddle's own dunning before our job ran.
	if s.paddleClient != nil && user.PaddleSubscriptionID != nil {
		if err := s.paddleClient.CancelSubscription(ctx, *user.PaddleSubscriptionID); err != nil {
			if !errors.Is(err, ErrPaddleAlreadyCancelled) {
				return fmt.Errorf("failed to cancel Paddle subscription for past_due user %s: %w", user.ID, err)
			}
			s.logger.Info("Paddle subscription already cancelled, continuing",
				"user_id", user.ID,
				"subscription_id", *user.PaddleSubscriptionID,
			)
		}
	}

	// Step 3: CAS flip subscription_status → cancelled.
	// Only flips if the user is still 'past_due' — guards against a concurrent
	// subscription.activated webhook that may have restored the user to 'active'
	// between the Plaid disconnect above and this point.
	flipped, err := s.repo.CancelIfPastDue(ctx, user.ID)
	if err != nil {
		return fmt.Errorf("CancelIfPastDue: %w", err)
	}
	if !flipped {
		// User recovered (payment arrived between Plaid disconnect and this point).
		// Log and return — do NOT continue with retention/notify/clear.
		s.logger.Info("past_due user recovered before cancel deadline, skipping cancellation",
			slog.String("user_id", user.ID))
		return nil
	}

	// Step 4: Start 6-month retention clock.
	retentionExpires := now.AddDate(0, s.retentionMonths, 0)
	if retErr := s.repo.SetRetentionDates(ctx, user.ID, now, retentionExpires); retErr != nil {
		s.logger.Warn("failed to set retention dates on past_due auto-cancel",
			"user_id", user.ID, "error", retErr)
	}

	// Step 5: Clear the past_due_started_at stamp.
	if clearErr := s.repo.ClearPastDueStartedAt(ctx, user.ID); clearErr != nil {
		s.logger.Warn("failed to clear past_due_started_at on auto-cancel",
			"user_id", user.ID, "error", clearErr)
	}

	// Step 6a: Sync Clerk metadata.
	s.syncClerkMetadata(ctx, user, "cancelled")

	// Step 6b: Create in-app notification.
	s.createCancelledNotification(ctx, user)

	s.logger.Info("past_due auto-cancel complete",
		"user_id", user.ID,
		"tier", user.SubscriptionTier,
	)

	return nil
}

// processPlaidDisconnectWindow handles the pre-emptive Plaid disconnect before grace expiry.
// Re-reads the user's current status first to avoid racing a recovery webhook.
func (s *PastDueService) processPlaidDisconnectWindow(ctx context.Context, user domain.User) error {
	// Re-read to guard against a concurrent recovery webhook that may have flipped
	// the user back to active between when GetUsersInPastDue ran and now.
	current, err := s.repo.GetUserByID(ctx, user.ID)
	if err != nil {
		return fmt.Errorf("failed to re-read user %s for Plaid disconnect check: %w", user.ID, err)
	}
	if current.SubscriptionStatus != string(domain.SubscriptionStatusPastDue) {
		s.logger.Info("user recovered before Plaid disconnect window action; skipping",
			"user_id", user.ID,
			"current_status", current.SubscriptionStatus,
		)
		return nil
	}

	s.logger.Info("past_due Plaid disconnect window reached, disconnecting",
		"user_id", user.ID,
	)

	if s.plaidCleanup != nil {
		if err := s.plaidCleanup.DisconnectAllUserItems(ctx, user.ID); err != nil {
			// Idempotent — log but continue; the daily job will retry.
			s.logger.Warn("failed to disconnect Plaid items in past_due window",
				"user_id", user.ID, "error", err)
		}
	}

	return nil
}

// syncClerkMetadata updates the Clerk public metadata for the user's new subscription state.
// Logs and swallows errors so Clerk failures don't abort the dunning flow.
func (s *PastDueService) syncClerkMetadata(ctx context.Context, user domain.User, status string) {
	if s.clerkClient == nil {
		return
	}
	if err := s.clerkClient.UpdateUserMetadata(ctx, user.ClerkUserID, map[string]interface{}{
		"subscription_tier":   user.SubscriptionTier,
		"subscription_status": status,
	}); err != nil {
		s.logger.Error("failed to sync Clerk metadata on past_due auto-cancel",
			"user_id", user.ID, "error", err)
	}
}

// createCancelledNotification creates an in-app notification for an auto-cancelled subscription.
// Logs and swallows errors so notification failures don't abort the dunning flow.
func (s *PastDueService) createCancelledNotification(ctx context.Context, user domain.User) {
	if s.notificationRepo == nil {
		return
	}
	actionURL := "/billing"
	actionLabel := "Resubscribe"
	if err := s.notificationRepo.Create(ctx, &domain.Notification{
		ID:                 uuid.New().String(),
		UserID:             user.ID,
		Type:               "subscription_cancelled",
		Title:              "Subscription Cancelled",
		Message:            "Your subscription has been cancelled due to non-payment. Resubscribe to restore access.",
		ActionURL:          &actionURL,
		ActionLabel:        &actionLabel,
		InteractionPattern: "action_required",
	}); err != nil {
		s.logger.Error("failed to create subscription_cancelled notification",
			"user_id", user.ID, "error", err)
	}
}
