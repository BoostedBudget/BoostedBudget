// Package services provides business logic and external service integrations.
// This file implements PlaidItemCleanupService for managing Plaid item removal.
// Issue #668: Plaid Item Management for Cost Optimization
package services

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/repository"
)

// PlaidItemCleanupService handles removal of Plaid items to minimize billing costs.
// It calls Plaid's /item/remove endpoint to invalidate access tokens and stop
// monthly subscription billing for Items.
//
// This service is used for:
// - User deletion: Remove all Plaid items before deleting user data
// - Orphaned item cleanup: Remove items in error state or not synced for extended periods
// - Subscription downgrade: Remove excess items when user loses premium access
// - Trial/subscription expiration: Disconnect items while preserving transaction history
//
// Issue #668: Plaid Item Management for Cost Optimization
// Issue #679, #680: Trial expiration
type PlaidItemCleanupService interface {
	// RemoveAllUserItems calls Plaid /item/remove for all of a user's linked items.
	// This should be called BEFORE deleting user data from the database to stop Plaid billing.
	//
	// Errors are logged but don't stop processing (graceful degradation):
	// - If one item fails to remove, the service continues with remaining items
	// - This ensures best-effort cleanup even if some items are already invalid
	//
	// Returns error only if the initial repository query fails.
	// Plaid API and decryption errors are logged but the function continues.
	RemoveAllUserItems(ctx context.Context, userID string) error

	// RemoveItem calls Plaid /item/remove for a single linked item.
	// Used for individual item cleanup (orphaned items, subscription downgrade).
	//
	// Returns error if:
	// - Item is not found in the database
	// - Access token decryption fails
	//
	// Plaid API errors are logged but not returned (graceful degradation).
	// This is because the access token may already be invalid.
	RemoveItem(ctx context.Context, linkedItemID string) error

	// DisconnectAllUserItems calls Plaid /item/remove for all items, then marks
	// them as 'disconnected' in the database. This preserves transaction history
	// while stopping Plaid billing.
	//
	// Issue #679, #680: Used when trial/subscription expires to:
	// 1. Stop Plaid billing by calling /item/remove
	// 2. Preserve accounts and transactions by NOT deleting from database
	// 3. Mark items as 'disconnected' status
	//
	// Returns error only if the initial repository query fails.
	// Plaid API errors are logged but processing continues.
	DisconnectAllUserItems(ctx context.Context, userID string) error
}

// linkedItemRepoForCleanup is a minimal interface for the repository operations
// needed by PlaidItemCleanupService. This avoids importing the full repository package.
type linkedItemRepoForCleanup interface {
	GetByUserID(ctx context.Context, userID string) ([]domain.LinkedItem, error)
	GetByID(ctx context.Context, id string) (*domain.LinkedItem, error)
	GetActiveItemsByUserID(ctx context.Context, userID string) ([]domain.LinkedItem, error)
	MarkAllUserItemsDisconnected(ctx context.Context, userID string) error
}

// plaidServiceForCleanup is a minimal interface for the Plaid operations
// needed by PlaidItemCleanupService.
type plaidServiceForCleanup interface {
	RemoveItem(ctx context.Context, accessToken string) error
}

// encryptionServiceForCleanup is a minimal interface for the encryption operations
// needed by PlaidItemCleanupService.
type encryptionServiceForCleanup interface {
	Decrypt(ciphertext string) (string, error)
}

// plaidItemCleanupService implements PlaidItemCleanupService
type plaidItemCleanupService struct {
	linkedItemRepo linkedItemRepoForCleanup
	plaidService   plaidServiceForCleanup
	encryptionSvc  encryptionServiceForCleanup
	logger         *slog.Logger
}

// NewPlaidItemCleanupService creates a new PlaidItemCleanupService.
// Parameters:
//   - linkedItemRepo: Repository for querying linked items
//   - plaidService: Service for calling Plaid API
//   - encryptionSvc: Service for decrypting access tokens
//
// Returns the cleanup service ready to use.
func NewPlaidItemCleanupService(
	linkedItemRepo linkedItemRepoForCleanup,
	plaidService plaidServiceForCleanup,
	encryptionSvc encryptionServiceForCleanup,
) PlaidItemCleanupService {
	return &plaidItemCleanupService{
		linkedItemRepo: linkedItemRepo,
		plaidService:   plaidService,
		encryptionSvc:  encryptionSvc,
		logger:         slog.Default(),
	}
}

// RemoveAllUserItems implements PlaidItemCleanupService.RemoveAllUserItems
func (s *plaidItemCleanupService) RemoveAllUserItems(ctx context.Context, userID string) error {
	// Get all linked items for the user
	items, err := s.linkedItemRepo.GetByUserID(ctx, userID)
	if err != nil {
		return fmt.Errorf("failed to get linked items for user %s: %w", userID, err)
	}

	// No items to remove
	if len(items) == 0 {
		s.logger.Info("no linked items to remove for user", "user_id", userID)
		return nil
	}

	s.logger.Info("removing Plaid items for user",
		"user_id", userID,
		"item_count", len(items),
	)

	// Process each item - continue on errors (graceful degradation)
	var successCount, failCount int
	for _, item := range items {
		if err := s.removeItemInternal(ctx, &item); err != nil {
			s.logger.Warn("failed to remove Plaid item",
				"item_id", item.ID,
				"plaid_item_id", item.ItemID,
				"user_id", userID,
				"error", err,
			)
			failCount++
		} else {
			successCount++
		}
	}

	s.logger.Info("Plaid item removal completed",
		"user_id", userID,
		"success_count", successCount,
		"fail_count", failCount,
	)

	// Return nil even if some items failed (graceful degradation)
	// The user deletion should proceed regardless
	return nil
}

// RemoveItem implements PlaidItemCleanupService.RemoveItem
func (s *plaidItemCleanupService) RemoveItem(ctx context.Context, linkedItemID string) error {
	// Get the linked item
	item, err := s.linkedItemRepo.GetByID(ctx, linkedItemID)
	if err != nil {
		return fmt.Errorf("failed to get linked item %s: %w", linkedItemID, err)
	}

	// Remove the item (logs Plaid errors but doesn't return them)
	if err := s.removeItemInternal(ctx, item); err != nil {
		s.logger.Warn("failed to remove Plaid item",
			"item_id", linkedItemID,
			"plaid_item_id", item.ItemID,
			"error", err,
		)
		// Don't return Plaid errors - graceful degradation
	}

	return nil
}

// removeItemInternal handles the actual Plaid /item/remove call for a single item.
// Returns error if decryption fails or Plaid API fails.
func (s *plaidItemCleanupService) removeItemInternal(ctx context.Context, item *domain.LinkedItem) error {
	// Decrypt the access token
	accessToken, err := s.encryptionSvc.Decrypt(item.AccessTokenEncrypted)
	if err != nil {
		return fmt.Errorf("failed to decrypt access token for item %s: %w", item.ID, err)
	}

	// Call Plaid /item/remove
	if err := s.plaidService.RemoveItem(ctx, accessToken); err != nil {
		return fmt.Errorf("plaid RemoveItem failed for item %s: %w", item.ID, err)
	}

	s.logger.Info("successfully removed Plaid item",
		"item_id", item.ID,
		"plaid_item_id", item.ItemID,
	)

	return nil
}

// IsItemNotFoundError checks if an error indicates an item was not found.
// Used by cleanup workers to determine if cleanup can be skipped (item already removed).
func IsItemNotFoundError(err error) bool {
	return errors.Is(err, repository.ErrLinkedItemNotFound)
}

// DisconnectAllUserItems implements PlaidItemCleanupService.DisconnectAllUserItems
// Issue #679, #680: Trial expiration - disconnect items while preserving history
func (s *plaidItemCleanupService) DisconnectAllUserItems(ctx context.Context, userID string) error {
	// Get all active linked items for the user (non-replaced, non-disconnected)
	items, err := s.linkedItemRepo.GetActiveItemsByUserID(ctx, userID)
	if err != nil {
		return fmt.Errorf("failed to get active linked items for user %s: %w", userID, err)
	}

	// No items to disconnect
	if len(items) == 0 {
		s.logger.Info("no active linked items to disconnect for user", "user_id", userID)
		return nil
	}

	s.logger.Info("disconnecting Plaid items for user",
		"user_id", userID,
		"item_count", len(items),
	)

	// First, call Plaid /item/remove for each item to stop billing
	// Continue on errors (graceful degradation)
	var successCount, failCount int
	for _, item := range items {
		if err := s.removeItemInternal(ctx, &item); err != nil {
			s.logger.Warn("failed to remove Plaid item from Plaid",
				"item_id", item.ID,
				"plaid_item_id", item.ItemID,
				"user_id", userID,
				"error", err,
			)
			failCount++
		} else {
			successCount++
		}
	}

	// Now mark all items as disconnected in the database
	// This preserves accounts and transactions
	if err := s.linkedItemRepo.MarkAllUserItemsDisconnected(ctx, userID); err != nil {
		s.logger.Error("failed to mark items as disconnected in database",
			"user_id", userID,
			"error", err,
		)
		return fmt.Errorf("failed to mark items as disconnected: %w", err)
	}

	s.logger.Info("Plaid item disconnection completed",
		"user_id", userID,
		"plaid_removed_success", successCount,
		"plaid_removed_fail", failCount,
		"db_status_updated", true,
	)

	return nil
}
