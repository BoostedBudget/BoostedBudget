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
	// Every item is attempted even after an earlier one fails — a single bad item must not
	// strand the rest of the user's items still-linked and still-billing. But this is NOT
	// best-effort: genuine per-item failures are aggregated and returned as a non-nil error.
	//
	// Issue #1413 finding H6: this function previously always returned nil ("graceful
	// degradation... deletion should proceed regardless"), which made its only production
	// caller's abort-on-failure logic (DeletionService.ExecuteDeletion) dead code — the
	// deletion cascade would proceed and clear the local Plaid item/token references even
	// after a failed /item/remove call, orphaning the still-active (still-billing) Plaid
	// Item with nothing left able to retry it. That caller needs strict semantics, and today
	// it is the ONLY caller, so the contract changed for it directly rather than adding a
	// parallel strict variant nobody else would use. If a FUTURE caller legitimately wants
	// best-effort semantics, make that an explicit, separate choice at that call site (e.g.
	// a dedicated method or an explicit parameter) — do not revert this function to always
	// returning nil, which would silently reopen H6.
	//
	// Returns a non-nil error if the initial repository query fails, or if one or more items
	// failed to be removed from Plaid (wrapping each underlying error via errors.Join).
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

// plaidItemCleanupService implements PlaidItemCleanupService
type plaidItemCleanupService struct {
	linkedItemRepo linkedItemRepoForCleanup
	plaidService   plaidServiceForCleanup
	logger         *slog.Logger
}

// NewPlaidItemCleanupService creates a new PlaidItemCleanupService.
// Parameters:
//   - linkedItemRepo: Repository for querying linked items
//   - plaidService: Service for calling Plaid API
//
// Returns the cleanup service ready to use.
func NewPlaidItemCleanupService(
	linkedItemRepo linkedItemRepoForCleanup,
	plaidService plaidServiceForCleanup,
) PlaidItemCleanupService {
	return &plaidItemCleanupService{
		linkedItemRepo: linkedItemRepo,
		plaidService:   plaidService,
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

	// Process every item - continue on errors so a single bad item doesn't strand the rest
	// still-linked and still-billing, but track failures to aggregate below (Issue #1413 H6).
	var successCount, failCount int
	var itemErrs []error
	for _, item := range items {
		if err := s.removeItemInternal(ctx, &item); err != nil {
			s.logger.Warn("failed to remove Plaid item",
				"item_id", item.ID,
				"plaid_item_id", item.ItemID,
				"user_id", userID,
				"error", err,
			)
			failCount++
			itemErrs = append(itemErrs, err)
		} else {
			successCount++
		}
	}

	s.logger.Info("Plaid item removal completed",
		"user_id", userID,
		"success_count", successCount,
		"fail_count", failCount,
	)

	if len(itemErrs) > 0 {
		return fmt.Errorf("failed to remove %d of %d Plaid item(s) for user %s: %w",
			failCount, len(items), userID, errors.Join(itemErrs...))
	}

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
// Returns error if Plaid API fails.
func (s *plaidItemCleanupService) removeItemInternal(ctx context.Context, item *domain.LinkedItem) error {
	// Call Plaid /item/remove using the plaintext access token populated by the repo layer
	if err := s.plaidService.RemoveItem(ctx, item.AccessToken); err != nil {
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
