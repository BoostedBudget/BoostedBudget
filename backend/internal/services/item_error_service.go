// Package services provides business logic and external service integrations.
// This file implements ItemErrorService for processing Plaid ITEM webhook errors.
package services

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/observability"
	"github.com/maxnussbaum/BoostedBudget/internal/repository"
)

// ErrLinkedItemNotRepairable indicates a linked item is not in a state the reconnect flow can
// repair (Issue #1609). Only an item with an error status — login_required, error,
// pending_expiration — is repairable via update-mode Link:
//   - "active" needs no repair (nothing is broken).
//   - "replaced" and "disconnected" were deliberately superseded/disconnected; a stale
//     notification's reconnect link must not silently resurrect either one.
//   - "revoked" is deliberately EXCLUDED (Issue #1609 round 2, BLOCKING 1): Plaid's own spec says
//     "[o]nce access to an Item has been revoked, it cannot be restored. If the user subsequently
//     returns to your application, a new Item must be created for the user" (plaid-go's vendored
//     openapi.yaml, USER_PERMISSION_REVOKED). Update mode re-authenticates the SAME Item; running
//     it against a revoked Item and then marking the item 'active' produces a false-healthy row —
//     the connection reports fine and silently never syncs again, which is worse than the dead
//     button this whole flow was built to replace. A revoked item needs a brand-new Item via the
//     normal Plaid Link (non-update-mode) flow, not a repair — see HandlePermissionRevoked's
//     notification, which routes there instead of here.
var ErrLinkedItemNotRepairable = errors.New("linked item is not in a repairable state")

// terminalLinkedItemStatuses are statuses a background/webhook error signal must never overwrite
// (Issue #1609 P3-T2). Passed as the terminalStatuses argument to every
// LinkedItemRepository.TransitionErrorStatus call the Handle* methods below make:
//   - 'disconnected' and 'replaced' are user-deliberate terminal states — the same exclusion
//     repairableStatuses encodes for the reconnect flow; a stale error signal must not resurrect
//     either one into an error status.
//   - 'revoked' is deliberately included too, unlike repairableStatuses' list of source states this
//     is a DESTINATION exclusion: Plaid says a revoked Item "cannot be restored" (see
//     ErrLinkedItemNotRepairable), so a background signal must not downgrade it to 'login_required'/
//     'error'/'pending_expiration' — that would make the item look repairable via update-mode Link
//     when Plaid will reject it.
var terminalLinkedItemStatuses = []domain.LinkedItemStatus{
	domain.LinkedItemStatusDisconnected,
	domain.LinkedItemStatusReplaced,
	domain.LinkedItemStatusRevoked,
}

// ItemErrorService handles Plaid ITEM webhook error processing.
// Issue #257: Item Error Handler - Process ITEM Webhooks and User Notifications
//
// Processes ITEM webhook errors from Plaid including:
//   - ERROR with error_code ITEM_LOGIN_REQUIRED or INVALID_ACCESS_TOKEN: user needs to
//     re-authenticate (credentials changed, MFA, session expired) — Issue #1621: this is
//     ProcessWebhook's routing, keyed on error_code, NOT a webhook_code named LOGIN_REQUIRED;
//     Plaid has no such webhook_code.
//   - ERROR (any other error_code): general connection error
//   - PENDING_EXPIRATION / PENDING_DISCONNECT: item access expiring/disconnecting soon
//   - USER_PERMISSION_REVOKED: user revoked access at their bank
//
// For each error type, the service:
//  1. Updates LinkedItem status and error details in database
//  2. Creates in-app notification for the user (Issue #1609: the reconnect page mints its own
//     update-mode link token on demand — no token is minted or embedded here)
//  3. Sends email notification (stub in MVP, real email in Phase 3+)
//
// Error handling follows graceful degradation:
// - Notification/email failures do NOT block webhook processing
// - Errors are logged but webhook returns 200 OK to Plaid
type ItemErrorService interface {
	// HandleLoginRequired processes a login-required signal (Issue #1621: routed from
	// ProcessWebhook's "ERROR" case by error_code ITEM_LOGIN_REQUIRED/INVALID_ACCESS_TOKEN —
	// there is no Plaid webhook_code named LOGIN_REQUIRED — and also called directly by the sync
	// path for the same two error codes; see plaid_permanent_error_notify.go).
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
	// User explicitly revoked access at their bank. Per Plaid's spec this is UNRECOVERABLE for
	// this Item (see ErrLinkedItemNotRepairable) — requires a brand-new Item via the normal Link
	// flow, not update-mode repair.
	//
	// Actions:
	// 1. Updates LinkedItem status to "revoked" with error details
	// 2. Creates notification routed to the normal connect-a-bank flow (/accounts, NOT
	//    /accounts/reconnect — Issue #1609 round 2, BLOCKING 1: update mode cannot restore a
	//    revoked Item, and marking it 'active' after "repairing" it would be false-healthy)
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

	// 2. Atomically transition LinkedItem status to "login_required" (critical - return error if
	// the DB call itself fails). Issue #1609 P3-T2: see TransitionErrorStatus's doc comment — this
	// replaces a full-row Update that was last-write-wins-unsafe once a second producer (the sync
	// worker) can call this same method for the same item.
	transitioned, err := s.linkedItemRepo.TransitionErrorStatus(
		ctx, linkedItem.ID, domain.LinkedItemStatusLoginRequired, errorCode, errorMessage, terminalLinkedItemStatuses,
	)
	if err != nil {
		return fmt.Errorf("failed to update linked item status for item %s: %w", linkedItem.ItemID, err)
	}
	if !transitioned {
		// Already login_required (duplicate signal from another producer), or in a terminal
		// status a background signal must not overwrite — either way, no notification.
		s.logger.Info("login_required signal for a linked item already in that status or a terminal state; skipping duplicate notification",
			"item_id", linkedItem.ItemID,
			"status", linkedItem.Status,
		)
		return nil
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
	// 1. Atomically transition LinkedItem status to "error" (critical - return error if the DB
	// call itself fails). Issue #1609 P3-T2: see TransitionErrorStatus's doc comment.
	transitioned, err := s.linkedItemRepo.TransitionErrorStatus(
		ctx, linkedItem.ID, domain.LinkedItemStatusError, errorCode, errorMessage, terminalLinkedItemStatuses,
	)
	if err != nil {
		return fmt.Errorf("failed to update linked item status for item %s: %w", linkedItem.ItemID, err)
	}
	if !transitioned {
		s.logger.Info("error signal for a linked item already in that status or a terminal state; skipping duplicate notification",
			"item_id", linkedItem.ItemID,
			"status", linkedItem.Status,
		)
		return nil
	}

	// 2. Create notification (non-blocking - log error but continue)
	// Issue #1609 P3-T8, M5: 'error' IS a repairableStatuses member (see that var above), so this
	// is the one state most likely to be reached by a sync-discovered failure — and until this fix
	// it was the only repairable-status notification with no ActionURL, i.e. no button. Give it the
	// same /accounts/reconnect CTA the login-required path uses (HandleLoginRequired above).
	// Issue #1609 P3-T3: errorMessage may legitimately be empty — webhook_processing.go has no
	// stable ErrorMessage field to forward for every webhook shape, so an absent message is an
	// expected input, not a bug. Omit the ": <message>" clause entirely rather than rendering a
	// dangling "with Test Bank: ." sentence.
	message := fmt.Sprintf("We're experiencing a connection issue with %s.", linkedItem.InstitutionName)
	if errorMessage != "" {
		message = fmt.Sprintf("We're experiencing a connection issue with %s: %s.", linkedItem.InstitutionName, errorMessage)
	}
	actionURL := fmt.Sprintf("/accounts/reconnect?item_id=%s", linkedItem.ItemID)
	actionLabel := "Reconnect Bank"
	notification := &domain.Notification{
		UserID:             linkedItem.UserID,
		Type:               "item_error",
		Title:              "Connection Error",
		Message:            message + " We'll automatically retry, but you can also try reconnecting your account if the issue persists.",
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

	// 2. Atomically transition LinkedItem status to "pending_expiration" (critical). Issue #1609
	// P3-T2: see TransitionErrorStatus's doc comment.
	transitioned, err := s.linkedItemRepo.TransitionErrorStatus(
		ctx, linkedItem.ID, domain.LinkedItemStatusPendingExpiration, errorCode, errorMessage, terminalLinkedItemStatuses,
	)
	if err != nil {
		return fmt.Errorf("failed to update linked item status for item %s: %w", linkedItem.ItemID, err)
	}
	if !transitioned {
		s.logger.Info("pending_expiration signal for a linked item already in that status or a terminal state; skipping duplicate notification",
			"item_id", linkedItem.ItemID,
			"status", linkedItem.Status,
		)
		return nil
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

	// 2. Atomically transition LinkedItem status to "revoked" (critical). Issue #1609 P3-T2: see
	// TransitionErrorStatus's doc comment. terminalLinkedItemStatuses includes 'revoked' itself as
	// a DESTINATION exclusion for the OTHER three handlers; here it is a no-op restatement — an
	// item already 'revoked' fails the newStatus == 'revoked' equality check regardless.
	transitioned, err := s.linkedItemRepo.TransitionErrorStatus(
		ctx, linkedItem.ID, domain.LinkedItemStatusRevoked, errorCode, errorMessage, terminalLinkedItemStatuses,
	)
	if err != nil {
		return fmt.Errorf("failed to update linked item status for item %s: %w", linkedItem.ItemID, err)
	}
	if !transitioned {
		s.logger.Info("permission_revoked signal for a linked item already in that status or a terminal state; skipping duplicate notification",
			"item_id", linkedItem.ItemID,
			"status", linkedItem.Status,
		)
		return nil
	}

	// 3. Create notification with action URL (non-blocking).
	//
	// Issue #1609 round 2, BLOCKING 1: deliberately NOT /accounts/reconnect. That page mints an
	// update-mode Link token for THIS item — but Plaid says a revoked Item "cannot be restored"
	// and "a new Item must be created" (see ErrLinkedItemNotRepairable). Sending the user to
	// update mode for a revoked item would either fail at Plaid, or — the whole reason
	// isRepairableStatus/RepairStatus now refuse 'revoked' — mark this permanently-dead item
	// 'active' again, a false-healthy state. Routing to /accounts instead puts the user on the
	// page that hosts PlaidLinkButton, the NORMAL (non-update-mode) Link flow that creates a
	// fresh Item; the old item stays 'revoked' and is superseded the way any other re-link is.
	actionURL := "/accounts"
	actionLabel := "Connect Your Bank"

	notification := &domain.Notification{
		UserID:             linkedItem.UserID,
		Type:               "permission_revoked",
		Title:              "Bank Access Revoked",
		Message:            fmt.Sprintf("You have revoked BoostedBudget's access to %s at your bank. To restore access and continue syncing your transactions, please connect your bank again.", linkedItem.InstitutionName),
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

	// 2. Route to appropriate handler based on the ITEM webhook's `webhook_code` (Issue #1621:
	// `errorType` here IS the webhook_code — see webhook_processing.go's processItemWebhook,
	// which passes *webhook.WebhookCode as this parameter. `ITEM_LOGIN_REQUIRED` is never a Plaid
	// webhook_code; per the vendored spec it is only ever an error_code nested inside an `ERROR`
	// webhook's `error` object (Plaid's ITEM_LOGIN_REQUIRED webhook was deprecated in favor of
	// this shape). A case keyed on errorType == "ITEM_LOGIN_REQUIRED" therefore could never match
	// in production; login failures are routed below, by errorCode, inside the "ERROR" case.
	switch errorType {
	case "ERROR":
		switch errorCode {
		case "ITEM_LOGIN_REQUIRED", "INVALID_ACCESS_TOKEN":
			// Routing parity with the sync path (plaid_permanent_error_notify.go): both codes
			// mean the same thing — only the user can fix this by re-authenticating — so both
			// route to HandleLoginRequired there, and must here too, or the two producers of
			// this same signal diverge (the exact defect class this branch keeps finding).
			return s.HandleLoginRequired(ctx, linkedItem, errorCode, errorMessage)
		default:
			return s.HandleError(ctx, linkedItem, errorCode, errorMessage)
		}
	case "PENDING_EXPIRATION", "PENDING_DISCONNECT":
		// PENDING_DISCONNECT ("fired when an Item is expected to be disconnected") is absent
		// from the vendored Plaid spec but live in Plaid's docs and observed in production
		// (Issue #1621). It deserves the same treatment as PENDING_EXPIRATION: warn the user to
		// reconnect before data stops, rather than falling into the unknown-code default below.
		return s.HandlePendingExpiration(ctx, linkedItem, errorCode, errorMessage)
	case "USER_PERMISSION_REVOKED":
		return s.HandlePermissionRevoked(ctx, linkedItem, errorCode, errorMessage)
	case "LOGIN_REPAIRED":
		// Issue #1609: previously unhandled — fell into the default case below and errored,
		// which sent the webhook to retry/DLQ forever, and left the item marked broken even
		// after the user fixed it (through any path, not just our reconnect page).
		return s.HandleLoginRepaired(ctx, linkedItem)
	default:
		// Unhandled/unknown ITEM webhook_code (Issue #1621). Known-unhandled codes that land
		// here today: USER_ACCOUNT_REVOKED, NEW_ACCOUNTS_AVAILABLE, and
		// WEBHOOK_UPDATE_ACKNOWLEDGED (self-inflicted — plaid_service.go sets a webhook URL,
		// which the spec says triggers this webhook). Any future code not yet given an explicit
		// case above also lands here.
		//
		// Log and return nil rather than erroring, mirroring webhook_processing.go:215-220's
		// handling of an unknown webhook TYPE one level up: an error here means River retries the
		// job five times and then DLQs it forever, which is no better than never having a case
		// at all. This is only safe because of the counter below — a Warn log alone is not an
		// alert, and could silently swallow a future code the way this one nearly did for
		// PENDING_DISCONNECT before this fix added it explicitly above.
		observability.UnknownItemWebhookCodeTotal.WithLabelValues(errorType).Inc()
		s.logger.Warn("unknown ITEM webhook code, skipping",
			"item_id", itemID,
			"webhook_code", errorType,
			"error_code", errorCode,
		)
		return nil
	}
}

// repairableStatuses is the single source of truth for which statuses a USER-INITIATED repair
// (CreateReconnectLinkToken / RepairItem) accepts. Issue #1609 round 2, BLOCKING 1: 'revoked' is
// deliberately absent — see ErrLinkedItemNotRepairable's doc comment. HandleLoginRepaired does
// NOT use this list; it uses its own, narrower predicate (should-fix 1 — see its doc comment).
var repairableStatuses = []domain.LinkedItemStatus{
	domain.LinkedItemStatusLoginRequired,
	domain.LinkedItemStatusError,
	domain.LinkedItemStatusPendingExpiration,
}

// isRepairableStatus reports whether a linked item's status represents a broken connection that
// the reconnect flow may repair via update-mode Link. See ErrLinkedItemNotRepairable and
// repairableStatuses. This is a best-effort PRE-check only (read-then-decide, not atomic) — used
// to fail fast and with a specific error before touching Plaid or the database. The actual state
// transition is guarded atomically by LinkedItemRepository.RepairStatus, which re-checks status
// (and ownership) in the same UPDATE statement; see RepairItem's doc comment for why the
// pre-check alone would be TOCTOU-unsafe.
func isRepairableStatus(status domain.LinkedItemStatus) bool {
	for _, s := range repairableStatuses {
		if status == s {
			return true
		}
	}
	return false
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
//
// The isRepairableStatus check below is a fast-fail pre-check only — it lets an obviously-bad
// call (wrong user, already-active item) return without a database write. The actual transition
// is made atomically by RepairStatus, which re-checks status AND ownership inside the same
// UPDATE (Issue #1609 round 2, BLOCKING 2): a full read-then-Update sequence here was TOCTOU-
// unsafe — a concurrent disconnect/re-link landing between the read and the write could be
// silently overwritten by a stale full-row Update. RepairStatus returning false (item vanished,
// or its FRESH status is no longer repairable) is treated identically to the pre-check failing.
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

	repaired, err := s.linkedItemRepo.RepairStatus(ctx, linkedItem.ID, userID, repairableStatuses)
	if err != nil {
		return fmt.Errorf("failed to clear error state for item %s: %w", itemID, err)
	}
	if !repaired {
		return ErrLinkedItemNotRepairable
	}
	return nil
}

// HandleLoginRepaired processes the Plaid LOGIN_REPAIRED webhook. See the interface doc comment
// for why this is a no-op (not an error) when the item is not in a repairable state.
//
// Issue #1609 should-fix 1: Plaid documents LOGIN_REPAIRED as meaning the Item specifically
// exited ITEM_LOGIN_REQUIRED — not a general "any error cleared" signal. Restricting the atomic
// predicate to LinkedItemStatusLoginRequired ALONE (rather than the wider repairableStatuses set
// RepairItem uses) matters: a delayed or duplicate webhook arriving after the item has since
// moved to a CURRENT 'error' or 'pending_expiration' must not clobber that current state — the
// webhook only proves the login issue was fixed, not that whatever is broken NOW is fixed.
//
// No ownership check and no user_id filter (empty string passed to RepairStatus): this is
// system-triggered (no authenticated user), and ProcessWebhook has already resolved the item
// unambiguously by Plaid's own item_id — there is no caller-supplied userID to compare against.
//
// Issue #1609 round 2, BLOCKING 2: like RepairItem, this used to read-then-check-then-full-row-
// Update. RepairStatus makes the transition atomic; RepairStatus returning false means the
// item's FRESH status was not login_required (already active, or superseded/disconnected/
// revoked concurrently), which is exactly the no-op case this method has always documented.
func (s *itemErrorService) HandleLoginRepaired(ctx context.Context, linkedItem *domain.LinkedItem) error {
	repaired, err := s.linkedItemRepo.RepairStatus(
		ctx, linkedItem.ID, "", []domain.LinkedItemStatus{domain.LinkedItemStatusLoginRequired},
	)
	if err != nil {
		return fmt.Errorf("failed to clear error state for item %s: %w", linkedItem.ItemID, err)
	}
	if !repaired {
		s.logger.Info("LOGIN_REPAIRED webhook received for a linked item not in a repairable state; ignoring",
			"item_id", linkedItem.ItemID,
			"status", linkedItem.Status,
		)
	}
	return nil
}
