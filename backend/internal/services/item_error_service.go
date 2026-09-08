// Package services provides business logic and external service integrations.
// This file implements ItemErrorService for processing Plaid ITEM webhook errors.
package services

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/repository"
)

// ErrLinkedItemNotRepairable indicates a linked item is not in a state the reconnect flow can
// repair (Issue #1609). Only an item with an error status — login_required, error,
// pending_expiration, revoked — is repairable:
//   - "active" needs no repair (nothing is broken).
//   - "replaced" and "disconnected" were deliberately superseded/disconnected; a stale
//     notification's reconnect link must not silently resurrect either one.
var ErrLinkedItemNotRepairable = errors.New("linked item is not in a repairable state")

// ItemErrorService handles Plaid ITEM webhook error processing.
// Issue #257: Item Error Handler - Process ITEM Webhooks and User Notifications
//
// Processes ITEM webhook errors from Plaid including:
// - LOGIN_REQUIRED: User needs to re-authenticate (credentials changed, MFA, session expired)
// - ERROR: General connection error
// - PENDING_EXPIRATION: Item access expiring soon
// - USER_PERMISSION_REVOKED: User revoked access at their bank
//
// For each error type, the service:
// 1. Updates LinkedItem status and error details in database
// 2. Creates update mode link token for re-authentication (if applicable)
// 3. Creates in-app notification for the user
// 4. Sends email notification (stub in MVP, real email in Phase 3+)
//
// Error handling follows graceful degradation:
// - Notification/email failures do NOT block webhook processing
// - Errors are logged but webhook returns 200 OK to Plaid
type ItemErrorService interface {
	// HandleLoginRequired processes LOGIN_REQUIRED webhook error.
	// This occurs when user needs to re-authenticate due to:
	// - Credentials changed at bank
	// - MFA required
	// - Session expired
	//
	// Actions:
	// 1. Updates LinkedItem status to "login_required" with error details
	// 2. Creates notification with action URL (item_id only — Issue #1609; the reconnect page
	//    mints its own update-mode link token on demand instead of one embedded here)
	// 3. Sends email notification (non-blocking)
	//
	// Parameters:
	//   - ctx: Context for cancellation and timeout
	//   - linkedItem: The LinkedItem that needs re-authentication
	//   - errorCode: Plaid error code (e.g., "ITEM_LOGIN_REQUIRED")
	//   - errorMessage: Human-readable error message
	//
	// Returns error only if critical operations fail (DB update).
	// Notification/email failures are logged but don't return error.
	HandleLoginRequired(ctx context.Context, linkedItem *domain.LinkedItem, errorCode string, errorMessage string) error

	// HandleError processes general ERROR webhook.
	// For general connection errors that may be transient or require user action.
	//
	// Actions:
	// 1. Updates LinkedItem status to "error" with error details
	// 2. Creates notification informing user of the error
	// 3. Sends email notification (non-blocking)
	//
	// Note: No link token needed for general errors (unlike LOGIN_REQUIRED).
	HandleError(ctx context.Context, linkedItem *domain.LinkedItem, errorCode string, errorMessage string) error

	// HandlePendingExpiration processes PENDING_EXPIRATION webhook.
	// Alerts user that Item access is expiring soon and needs re-authentication.
	//
	// Actions:
	// 1. Updates LinkedItem status to "pending_expiration" with error details
	// 2. Creates notification with action URL (item_id only — Issue #1609; see HandleLoginRequired)
	// 3. Sends email notification (non-blocking)
	HandlePendingExpiration(ctx context.Context, linkedItem *domain.LinkedItem, errorCode string, errorMessage string) error

	// HandlePermissionRevoked processes USER_PERMISSION_REVOKED webhook.
	// User explicitly revoked access at their bank - requires re-linking.
	//
	// Actions:
	// 1. Updates LinkedItem status to "revoked" with error details
	// 2. Creates notification with action URL (item_id only — Issue #1609; see HandleLoginRequired)
	// 3. Sends email notification (non-blocking)
	HandlePermissionRevoked(ctx context.Context, linkedItem *domain.LinkedItem, errorCode string, errorMessage string) error

	// ProcessWebhook is the main orchestrator for ITEM webhook processing.
	// Routes webhook to appropriate handler based on error type.
	//
	// Called by WebhookProcessor when ITEM webhook is received.
	//
	// Parameters:
	//   - ctx: Context for cancellation and timeout
	//   - itemID: Plaid Item ID from webhook
	//   - errorType: Webhook error type (e.g., "ITEM_LOGIN_REQUIRED", "ERROR")
	//   - errorCode: Plaid error code
	//   - errorMessage: Human-readable error message
	//
	// Returns error only for critical failures. Graceful degradation for notifications.
	ProcessWebhook(ctx context.Context, itemID string, errorType string, errorCode string, errorMessage string) error

	// CreateReconnectLinkToken mints an update-mode Plaid Link token for a user repairing a
	// broken bank connection from the /accounts/reconnect page (Issue #1609). Replaces the old
	// design where the notification's action_url embedded a token minted hours earlier by the
	// Handle* methods — that token could easily be stale (Plaid Link tokens expire in ~4h) by the
	// time the user clicked the notification.
	//
	// Authorization: itemID is a Plaid item_id, which GetByItemID resolves with no user
	// predicate, so ownership is enforced here by comparing the resolved item's UserID to the
	// authenticated caller's userID. A Plaid item_id is not a secret — ExchangePublicToken already
	// returns it to the client — so this ownership check is necessary AND sufficient
	// authorization; no additional secret comparison is needed.
	//
	// State: only a repairable item (see ErrLinkedItemNotRepairable) may mint a token.
	//
	// Returns repository.ErrLinkedItemNotFound if the item doesn't exist OR belongs to a
	// different user (same non-disclosure shape as GetByIDAndUser elsewhere in this codebase —
	// the caller cannot distinguish "doesn't exist" from "not yours").
	// Returns ErrLinkedItemNotRepairable if the item is not in a repairable state.
	CreateReconnectLinkToken(ctx context.Context, userID string, itemID string) (*domain.LinkTokenResponse, error)

	// RepairItem clears a linked item's error state after the user has successfully completed
	// the update-mode Plaid Link flow from the /accounts/reconnect page (Issue #1609). This is
	// the ONLY user-facing mechanism that resets status back to "active" outside of brand-new
	// link creation (AccountService.LinkAccounts) — nothing else in the codebase writes to these
	// fields except this method, the LOGIN_REPAIRED webhook path (HandleLoginRepaired), and the
	// Handle* methods that set the error in the first place.
	//
	// Authorization and state preconditions mirror CreateReconnectLinkToken exactly — a caller
	// must complete Link (which requires a token from CreateReconnectLinkToken) before repairing,
	// so the two share the same ownership/state gate.
	RepairItem(ctx context.Context, userID string, itemID string) error

	// HandleLoginRepaired processes the Plaid LOGIN_REPAIRED webhook (Issue #1609). Plaid sends
	// this when an Item's login issue is resolved through ANY path — not just our reconnect page
	// (e.g. the user re-authenticates via Plaid's own UI, or another app sharing the Item). Without
	// this, an item repaired outside our page would stay marked broken forever, since
	// ItemErrorService is the only writer of the error fields it set.
	//
	// Unlike RepairItem, this is system-triggered (no authenticated user), so there is no
	// ownership check — GetByItemID's caller (ProcessWebhook) has already resolved the item
	// unambiguously by Plaid's own item_id. If the item is not currently in a repairable state
	// (e.g. it's already active, or it was replaced/disconnected), this is a no-op: an unrelated
	// webhook must never resurrect an item the user deliberately superseded or disconnected, and
	// re-repairing an already-active item is meaningless. Returns nil in both the repaired and
	// no-op cases — this is not a webhook failure, so it must not go to retry/DLQ.
	HandleLoginRepaired(ctx context.Context, linkedItem *domain.LinkedItem) error
}

// itemErrorService implements ItemErrorService
type itemErrorService struct {
	logger           *slog.Logger
	linkedItemRepo   repository.LinkedItemRepository
	notificationRepo repository.NotificationRepository
	userRepo         UserRepository
	plaidService     PlaidService
	emailService     EmailService
}

// NewItemErrorService creates a new ItemErrorService instance
func NewItemErrorService(
	linkedItemRepo repository.LinkedItemRepository,
	notificationRepo repository.NotificationRepository,
	userRepo UserRepository,
	plaidService PlaidService,
	emailService EmailService,
	logger *slog.Logger,
) ItemErrorService {
	if logger == nil {
		logger = slog.Default()
	}
	return &itemErrorService{
		logger:           logger,
		linkedItemRepo:   linkedItemRepo,
		notificationRepo: notificationRepo,
		userRepo:         userRepo,
		plaidService:     plaidService,
		emailService:     emailService,
	}
}

// sendNotificationEmail sends an email notification with the user's email address.
// Non-blocking: errors are logged but don't fail the operation.
func (s *itemErrorService) sendNotificationEmail(ctx context.Context, userID string, notification *domain.Notification, itemID string) {
	user, err := s.userRepo.GetUserByID(ctx, userID)
	if err != nil {
		s.logger.Warn("failed to fetch user email for notification", "item_id", itemID, "error", err)
		return
	}
	if user.Email == "" {
		s.logger.Warn("user has no email address, skipping notification email", "user_id", userID, "item_id", itemID)
		return
	}
	if err := s.emailService.SendNotificationEmail(ctx, user.Email, notification); err != nil {
		s.logger.Warn("failed to send email notification", "item_id", itemID, "error", err)
	}
}

// HandleLoginRequired processes LOGIN_REQUIRED webhook error
func (s *itemErrorService) HandleLoginRequired(
	ctx context.Context,
	linkedItem *domain.LinkedItem,
	errorCode string,
	errorMessage string,
) error {
	// Issue #1609: the update-mode link token used to be minted here and embedded in the
	// notification's action_url. That token expires in ~4h while the notification row lives
	// forever (a stale notification read the next morning carried a dead token), it was
	// persisted in a DB row and re-sent on every notification fetch, and it was logged verbatim
	// by email_service.go. The reconnect page now mints a fresh token on demand instead (see
	// PlaidHandler's reconnect-link-token endpoint). Dropping the eager mint here also means a
	// Plaid outage no longer stops the user being told their connection broke.

	// 2. Update LinkedItem status to "login_required" (critical - return error if fails)
	now := time.Now()
	linkedItem.Status = "login_required"
	linkedItem.StatusChangedAt = &now // Issue #1010: track status change time
	linkedItem.ErrorCode = &errorCode
	linkedItem.ErrorMessage = &errorMessage
	linkedItem.LastErrorAt = &now

	err := s.linkedItemRepo.Update(ctx, linkedItem)
	if err != nil {
		return fmt.Errorf("failed to update linked item status for item %s: %w", linkedItem.ItemID, err)
	}

	// 3. Create notification with action URL (non-blocking - log error but continue)
	actionURL := fmt.Sprintf("/accounts/reconnect?item_id=%s", linkedItem.ItemID)
	actionLabel := "Reconnect Bank"

	notification := &domain.Notification{
		UserID:             linkedItem.UserID,
		Type:               "login_required",
		Title:              "Reconnect Your Bank",
		Message:            fmt.Sprintf("We need you to reconnect %s to continue syncing transactions. This usually happens when your credentials change or when your bank requires additional verification.", linkedItem.InstitutionName),
		ActionURL:          &actionURL,
		ActionLabel:        &actionLabel,
		InteractionPattern: "action_required",
		Read:               false,
		SentEmail:          false,
	}

	err = s.notificationRepo.Create(ctx, notification)
	if err != nil {
		// Non-blocking: log error but don't return (graceful degradation)
		s.logger.Warn("failed to create notification", "item_id", linkedItem.ItemID, "error", err)
		return nil
	}

	// 5. Send email notification (non-blocking)
	s.sendNotificationEmail(ctx, linkedItem.UserID, notification, linkedItem.ItemID)

	return nil
}

// HandleError processes general ERROR webhook
func (s *itemErrorService) HandleError(
	ctx context.Context,
	linkedItem *domain.LinkedItem,
	errorCode string,
	errorMessage string,
) error {
	// 1. Update LinkedItem status to "error" (critical - return error if fails)
	now := time.Now()
	linkedItem.Status = "error"
	linkedItem.StatusChangedAt = &now // Issue #1010: track status change time
	linkedItem.ErrorCode = &errorCode
	linkedItem.ErrorMessage = &errorMessage
	linkedItem.LastErrorAt = &now

	err := s.linkedItemRepo.Update(ctx, linkedItem)
	if err != nil {
		return fmt.Errorf("failed to update linked item status for item %s: %w", linkedItem.ItemID, err)
	}

	// 2. Create notification (non-blocking - log error but continue)
	// Note: No action URL for general errors (unlike LOGIN_REQUIRED)
	notification := &domain.Notification{
		UserID:             linkedItem.UserID,
		Type:               "item_error",
		Title:              "Connection Error",
		Message:            fmt.Sprintf("We're experiencing a connection issue with %s: %s. We'll automatically retry, but you can also try reconnecting your account if the issue persists.", linkedItem.InstitutionName, errorMessage),
		InteractionPattern: "informational",
		Read:               false,
		SentEmail:          false,
	}

	err = s.notificationRepo.Create(ctx, notification)
	if err != nil {
		// Non-blocking: log error but don't return (graceful degradation)
		s.logger.Warn("failed to create notification", "item_id", linkedItem.ItemID, "error", err)
		return nil
	}

	// 3. Send email notification (non-blocking)
	s.sendNotificationEmail(ctx, linkedItem.UserID, notification, linkedItem.ItemID)

	return nil
}

// HandlePendingExpiration processes PENDING_EXPIRATION webhook
func (s *itemErrorService) HandlePendingExpiration(
	ctx context.Context,
	linkedItem *domain.LinkedItem,
	errorCode string,
	errorMessage string,
) error {
	// Issue #1609: see the doc comment in HandleLoginRequired — the update-mode link token is no
	// longer minted eagerly here; the reconnect page mints one on demand instead.

	// 2. Update LinkedItem status to "pending_expiration" (critical)
	now := time.Now()
	linkedItem.Status = "pending_expiration"
	linkedItem.StatusChangedAt = &now // Issue #1010: track status change time
	linkedItem.ErrorCode = &errorCode
	linkedItem.ErrorMessage = &errorMessage
	linkedItem.LastErrorAt = &now

	err := s.linkedItemRepo.Update(ctx, linkedItem)
	if err != nil {
		return fmt.Errorf("failed to update linked item status for item %s: %w", linkedItem.ItemID, err)
	}

	// 3. Create notification with action URL (non-blocking)
	actionURL := fmt.Sprintf("/accounts/reconnect?item_id=%s", linkedItem.ItemID)
	actionLabel := "Reconnect Bank"

	notification := &domain.Notification{
		UserID:             linkedItem.UserID,
		Type:               "pending_expiration",
		Title:              "Bank Connection Expiring Soon",
		Message:            fmt.Sprintf("Your connection to %s is expiring soon. Please reconnect your account to ensure uninterrupted access to your financial data.", linkedItem.InstitutionName),
		ActionURL:          &actionURL,
		ActionLabel:        &actionLabel,
		InteractionPattern: "action_required",
		Read:               false,
		SentEmail:          false,
	}

	err = s.notificationRepo.Create(ctx, notification)
	if err != nil {
		// Non-blocking: log error but don't return (graceful degradation)
		s.logger.Warn("failed to create notification", "item_id", linkedItem.ItemID, "error", err)
		return nil
	}

	// 5. Send email notification (non-blocking)
	s.sendNotificationEmail(ctx, linkedItem.UserID, notification, linkedItem.ItemID)

	return nil
}

// HandlePermissionRevoked processes USER_PERMISSION_REVOKED webhook
func (s *itemErrorService) HandlePermissionRevoked(
	ctx context.Context,
	linkedItem *domain.LinkedItem,
	errorCode string,
	errorMessage string,
) error {
	// Issue #1609: see the doc comment in HandleLoginRequired — the update-mode link token is no
	// longer minted eagerly here; the reconnect page mints one on demand instead.

	// 2. Update LinkedItem status to "revoked" (critical)
	now := time.Now()
	linkedItem.Status = "revoked"
	linkedItem.StatusChangedAt = &now // Issue #1010: TTL clock for fingerprint release
	linkedItem.ErrorCode = &errorCode
	linkedItem.ErrorMessage = &errorMessage
	linkedItem.LastErrorAt = &now

	err := s.linkedItemRepo.Update(ctx, linkedItem)
	if err != nil {
		return fmt.Errorf("failed to update linked item status for item %s: %w", linkedItem.ItemID, err)
	}

	// 3. Create notification with action URL (non-blocking)
	actionURL := fmt.Sprintf("/accounts/reconnect?item_id=%s", linkedItem.ItemID)
	actionLabel := "Reconnect Bank"

	notification := &domain.Notification{
		UserID:             linkedItem.UserID,
		Type:               "permission_revoked",
		Title:              "Bank Access Revoked",
		Message:            fmt.Sprintf("You have revoked BoostedBudget's access to %s at your bank. To restore access and continue syncing your transactions, please reconnect your account.", linkedItem.InstitutionName),
		ActionURL:          &actionURL,
		ActionLabel:        &actionLabel,
		InteractionPattern: "action_required",
		Read:               false,
		SentEmail:          false,
	}

	err = s.notificationRepo.Create(ctx, notification)
	if err != nil {
		// Non-blocking: log error but don't return (graceful degradation)
		s.logger.Warn("failed to create notification", "item_id", linkedItem.ItemID, "error", err)
		return nil
	}

	// 5. Send email notification (non-blocking)
	s.sendNotificationEmail(ctx, linkedItem.UserID, notification, linkedItem.ItemID)

	return nil
}

// ProcessWebhook is the main orchestrator for ITEM webhook processing
func (s *itemErrorService) ProcessWebhook(
	ctx context.Context,
	itemID string,
	errorType string,
	errorCode string,
	errorMessage string,
) error {
	// 1. Look up LinkedItem by Plaid item ID (critical - return error if fails)
	linkedItem, err := s.linkedItemRepo.GetByItemID(ctx, itemID)
	if err != nil {
		return fmt.Errorf("failed to lookup linked item for item_id %s: %w", itemID, err)
	}

	// 2. Route to appropriate handler based on error type
	switch errorType {
	case "ITEM_LOGIN_REQUIRED":
		return s.HandleLoginRequired(ctx, linkedItem, errorCode, errorMessage)
	case "ERROR":
		return s.HandleError(ctx, linkedItem, errorCode, errorMessage)
	case "PENDING_EXPIRATION":
		return s.HandlePendingExpiration(ctx, linkedItem, errorCode, errorMessage)
	case "USER_PERMISSION_REVOKED":
		return s.HandlePermissionRevoked(ctx, linkedItem, errorCode, errorMessage)
	case "LOGIN_REPAIRED":
		// Issue #1609: previously unhandled — fell into the default case below and errored,
		// which sent the webhook to retry/DLQ forever, and left the item marked broken even
		// after the user fixed it (through any path, not just our reconnect page).
		return s.HandleLoginRepaired(ctx, linkedItem)
	default:
		// Unknown error type - log and return error (webhook cannot be processed)
		return fmt.Errorf("unsupported error type %s for item_id %s", errorType, itemID)
	}
}

// isRepairableStatus reports whether a linked item's status represents a broken connection that
// the reconnect flow (or the LOGIN_REPAIRED webhook) may repair. See ErrLinkedItemNotRepairable.
func isRepairableStatus(status domain.LinkedItemStatus) bool {
	switch status {
	case domain.LinkedItemStatusLoginRequired,
		domain.LinkedItemStatusError,
		domain.LinkedItemStatusPendingExpiration,
		domain.LinkedItemStatusRevoked:
		return true
	default:
		return false
	}
}

// clearErrorState resets a linked item's status to "active" and clears its error tracking fields.
// Shared by RepairItem (user completed the reconnect page's Link flow) and HandleLoginRepaired
// (Plaid's LOGIN_REPAIRED webhook — repaired through some other path). This is deliberately the
// ONLY place that writes LinkedItemStatusActive outside of brand-new link creation
// (AccountService.LinkAccounts) — Issue #1609 found no other mechanism that clears an item's error
// state, so this needed inventing rather than reusing something.
func (s *itemErrorService) clearErrorState(ctx context.Context, linkedItem *domain.LinkedItem) error {
	now := time.Now()
	linkedItem.Status = domain.LinkedItemStatusActive
	linkedItem.StatusChangedAt = &now
	linkedItem.ErrorCode = nil
	linkedItem.ErrorMessage = nil
	linkedItem.LastErrorAt = nil

	if err := s.linkedItemRepo.Update(ctx, linkedItem); err != nil {
		return fmt.Errorf("failed to clear error state for item %s: %w", linkedItem.ItemID, err)
	}
	return nil
}

// CreateReconnectLinkToken mints an update-mode Plaid Link token for the /accounts/reconnect page.
// See the interface doc comment for the authorization and state reasoning.
func (s *itemErrorService) CreateReconnectLinkToken(ctx context.Context, userID string, itemID string) (*domain.LinkTokenResponse, error) {
	linkedItem, err := s.linkedItemRepo.GetByItemID(ctx, itemID)
	if err != nil {
		return nil, err
	}
	if linkedItem.UserID != userID {
		// Deliberately the same error as "doesn't exist" — do not disclose that this item_id
		// belongs to someone else.
		return nil, repository.ErrLinkedItemNotFound
	}
	if !isRepairableStatus(linkedItem.Status) {
		return nil, ErrLinkedItemNotRepairable
	}

	return s.plaidService.CreateUpdateModeLinkToken(ctx, linkedItem.UserID, linkedItem.AccessToken)
}

// RepairItem clears a linked item's error state after the user completes the reconnect page's
// update-mode Link flow. See the interface doc comment for the authorization and state reasoning.
func (s *itemErrorService) RepairItem(ctx context.Context, userID string, itemID string) error {
	linkedItem, err := s.linkedItemRepo.GetByItemID(ctx, itemID)
	if err != nil {
		return err
	}
	if linkedItem.UserID != userID {
		return repository.ErrLinkedItemNotFound
	}
	if !isRepairableStatus(linkedItem.Status) {
		return ErrLinkedItemNotRepairable
	}

	return s.clearErrorState(ctx, linkedItem)
}

// HandleLoginRepaired processes the Plaid LOGIN_REPAIRED webhook. See the interface doc comment
// for why this is a no-op (not an error) when the item is not in a repairable state.
func (s *itemErrorService) HandleLoginRepaired(ctx context.Context, linkedItem *domain.LinkedItem) error {
	if !isRepairableStatus(linkedItem.Status) {
		s.logger.Info("LOGIN_REPAIRED webhook received for a linked item not in a repairable state; ignoring",
			"item_id", linkedItem.ItemID,
			"status", linkedItem.Status,
		)
		return nil
	}

	return s.clearErrorState(ctx, linkedItem)
}
