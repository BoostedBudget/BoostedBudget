// Package services provides business logic and external service integrations.
// This file implements ItemErrorService for processing Plaid ITEM webhook errors.
package services

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/repository"
)

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
	// 2. Creates update mode link token for re-authentication
	// 3. Creates notification with action URL (link token embedded)
	// 4. Sends email notification (non-blocking)
	//
	// Parameters:
	//   - ctx: Context for cancellation and timeout
	//   - linkedItem: The LinkedItem that needs re-authentication
	//   - errorCode: Plaid error code (e.g., "ITEM_LOGIN_REQUIRED")
	//   - errorMessage: Human-readable error message
	//
	// Returns error only if critical operations fail (DB update, link token creation).
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
	// 2. Creates update mode link token for re-authentication
	// 3. Creates notification with action URL (link token embedded)
	// 4. Sends email notification (non-blocking)
	HandlePendingExpiration(ctx context.Context, linkedItem *domain.LinkedItem, errorCode string, errorMessage string) error

	// HandlePermissionRevoked processes USER_PERMISSION_REVOKED webhook.
	// User explicitly revoked access at their bank - requires re-linking.
	//
	// Actions:
	// 1. Updates LinkedItem status to "revoked" with error details
	// 2. Creates update mode link token for re-linking
	// 3. Creates notification with action URL (link token embedded)
	// 4. Sends email notification (non-blocking)
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
}

// itemErrorService implements ItemErrorService
type itemErrorService struct {
	logger            *slog.Logger
	linkedItemRepo    repository.LinkedItemRepository
	notificationRepo  repository.NotificationRepository
	userRepo          UserRepository
	plaidService      PlaidService
	encryptionService EncryptionService
	emailService      EmailService
}

// NewItemErrorService creates a new ItemErrorService instance
func NewItemErrorService(
	linkedItemRepo repository.LinkedItemRepository,
	notificationRepo repository.NotificationRepository,
	userRepo UserRepository,
	plaidService PlaidService,
	encryptionService EncryptionService,
	emailService EmailService,
	logger *slog.Logger,
) ItemErrorService {
	if logger == nil {
		logger = slog.Default()
	}
	return &itemErrorService{
		logger:            logger,
		linkedItemRepo:    linkedItemRepo,
		notificationRepo:  notificationRepo,
		userRepo:          userRepo,
		plaidService:      plaidService,
		encryptionService: encryptionService,
		emailService:      emailService,
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
	// 1. Decrypt access token (critical - return error if fails)
	decryptedAccessToken, err := s.encryptionService.Decrypt(linkedItem.AccessTokenEncrypted)
	if err != nil {
		return fmt.Errorf("failed to decrypt access token for item %s: %w", linkedItem.ItemID, err)
	}

	// 2. Create update mode link token for re-authentication (critical - return error if fails)
	linkTokenResponse, err := s.plaidService.CreateUpdateModeLinkToken(ctx, linkedItem.UserID, decryptedAccessToken)
	if err != nil {
		return fmt.Errorf("failed to create update mode link token for item %s: %w", linkedItem.ItemID, err)
	}

	// 3. Update LinkedItem status to "login_required" (critical - return error if fails)
	now := time.Now()
	linkedItem.Status = "login_required"
	linkedItem.ErrorCode = &errorCode
	linkedItem.ErrorMessage = &errorMessage
	linkedItem.LastErrorAt = &now

	err = s.linkedItemRepo.Update(ctx, linkedItem)
	if err != nil {
		return fmt.Errorf("failed to update linked item status for item %s: %w", linkedItem.ItemID, err)
	}

	// 4. Create notification with action URL (non-blocking - log error but continue)
	actionURL := fmt.Sprintf("/accounts/reconnect?link_token=%s&item_id=%s", linkTokenResponse.LinkToken, linkedItem.ItemID)
	actionLabel := "Reconnect Bank"

	notification := &domain.Notification{
		UserID:      linkedItem.UserID,
		Type:        "login_required",
		Title:       "Reconnect Your Bank",
		Message:     fmt.Sprintf("We need you to reconnect %s to continue syncing transactions. This usually happens when your credentials change or when your bank requires additional verification.", linkedItem.InstitutionName),
		ActionURL:   &actionURL,
		ActionLabel: &actionLabel,
		Read:        false,
		SentEmail:   false,
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
		UserID:    linkedItem.UserID,
		Type:      "error",
		Title:     "Connection Error",
		Message:   fmt.Sprintf("We're experiencing a connection issue with %s: %s. We'll automatically retry, but you can also try reconnecting your account if the issue persists.", linkedItem.InstitutionName, errorMessage),
		Read:      false,
		SentEmail: false,
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
	// 1. Decrypt access token (critical - return error if fails)
	decryptedAccessToken, err := s.encryptionService.Decrypt(linkedItem.AccessTokenEncrypted)
	if err != nil {
		return fmt.Errorf("failed to decrypt access token for item %s: %w", linkedItem.ItemID, err)
	}

	// 2. Create update mode link token for re-authentication (critical)
	linkTokenResponse, err := s.plaidService.CreateUpdateModeLinkToken(ctx, linkedItem.UserID, decryptedAccessToken)
	if err != nil {
		return fmt.Errorf("failed to create update mode link token for item %s: %w", linkedItem.ItemID, err)
	}

	// 3. Update LinkedItem status to "pending_expiration" (critical)
	now := time.Now()
	linkedItem.Status = "pending_expiration"
	linkedItem.ErrorCode = &errorCode
	linkedItem.ErrorMessage = &errorMessage
	linkedItem.LastErrorAt = &now

	err = s.linkedItemRepo.Update(ctx, linkedItem)
	if err != nil {
		return fmt.Errorf("failed to update linked item status for item %s: %w", linkedItem.ItemID, err)
	}

	// 4. Create notification with action URL (non-blocking)
	actionURL := fmt.Sprintf("/accounts/reconnect?link_token=%s&item_id=%s", linkTokenResponse.LinkToken, linkedItem.ItemID)
	actionLabel := "Reconnect Bank"

	notification := &domain.Notification{
		UserID:      linkedItem.UserID,
		Type:        "pending_expiration",
		Title:       "Bank Connection Expiring Soon",
		Message:     fmt.Sprintf("Your connection to %s is expiring soon. Please reconnect your account to ensure uninterrupted access to your financial data.", linkedItem.InstitutionName),
		ActionURL:   &actionURL,
		ActionLabel: &actionLabel,
		Read:        false,
		SentEmail:   false,
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
	// 1. Decrypt access token (critical - return error if fails)
	decryptedAccessToken, err := s.encryptionService.Decrypt(linkedItem.AccessTokenEncrypted)
	if err != nil {
		return fmt.Errorf("failed to decrypt access token for item %s: %w", linkedItem.ItemID, err)
	}

	// 2. Create update mode link token for re-linking (critical)
	linkTokenResponse, err := s.plaidService.CreateUpdateModeLinkToken(ctx, linkedItem.UserID, decryptedAccessToken)
	if err != nil {
		return fmt.Errorf("failed to create update mode link token for item %s: %w", linkedItem.ItemID, err)
	}

	// 3. Update LinkedItem status to "revoked" (critical)
	now := time.Now()
	linkedItem.Status = "revoked"
	linkedItem.ErrorCode = &errorCode
	linkedItem.ErrorMessage = &errorMessage
	linkedItem.LastErrorAt = &now

	err = s.linkedItemRepo.Update(ctx, linkedItem)
	if err != nil {
		return fmt.Errorf("failed to update linked item status for item %s: %w", linkedItem.ItemID, err)
	}

	// 4. Create notification with action URL (non-blocking)
	actionURL := fmt.Sprintf("/accounts/reconnect?link_token=%s&item_id=%s", linkTokenResponse.LinkToken, linkedItem.ItemID)
	actionLabel := "Reconnect Bank"

	notification := &domain.Notification{
		UserID:      linkedItem.UserID,
		Type:        "permission_revoked",
		Title:       "Bank Access Revoked",
		Message:     fmt.Sprintf("You have revoked BoostedBudget's access to %s at your bank. To restore access and continue syncing your transactions, please reconnect your account.", linkedItem.InstitutionName),
		ActionURL:   &actionURL,
		ActionLabel: &actionLabel,
		Read:        false,
		SentEmail:   false,
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
	default:
		// Unknown error type - log and return error (webhook cannot be processed)
		return fmt.Errorf("unsupported error type %s for item_id %s", errorType, itemID)
	}
}
