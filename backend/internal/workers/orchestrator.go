package workers

import (
	"context"
	"fmt"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/repository"
)

// TransactionSyncOrchestrator manages transaction syncing across multiple providers.
// It routes linked items to the appropriate provider based on the provider field.
type TransactionSyncOrchestrator struct {
	providers map[domain.TransactionSource]TransactionSyncProvider
}

// NewTransactionSyncOrchestrator creates a new orchestrator
func NewTransactionSyncOrchestrator() *TransactionSyncOrchestrator {
	return &TransactionSyncOrchestrator{
		providers: make(map[domain.TransactionSource]TransactionSyncProvider),
	}
}

// RegisterProvider registers a transaction sync provider
func (o *TransactionSyncOrchestrator) RegisterProvider(provider TransactionSyncProvider) {
	source := provider.GetProviderName()
	o.providers[source] = provider
}

// SyncForLinkedItem syncs transactions for a specific linked item by routing to the correct provider
func (o *TransactionSyncOrchestrator) SyncForLinkedItem(ctx context.Context, linkedItemRepo repository.LinkedItemRepository, linkedItemID string) (int, error) {
	// Fetch linked item to determine provider
	linkedItem, err := linkedItemRepo.GetByID(ctx, linkedItemID)
	if err != nil {
		return 0, fmt.Errorf("failed to fetch linked item: %w", err)
	}

	// Get provider for this linked item
	providerSource := domain.TransactionSource(linkedItem.Provider)
	provider, exists := o.providers[providerSource]
	if !exists {
		return 0, fmt.Errorf("no provider registered for source: %s", linkedItem.Provider)
	}

	// Sync using the appropriate provider
	count, err := provider.SyncForLinkedItem(ctx, linkedItemID)
	if err != nil {
		return 0, fmt.Errorf("provider sync failed for %s: %w", linkedItem.Provider, err)
	}

	return count, nil
}

// SyncForUser syncs all linked items for a specific user
func (o *TransactionSyncOrchestrator) SyncForUser(ctx context.Context, linkedItemRepo repository.LinkedItemRepository, userID string) (int, error) {
	// Fetch all linked items for user
	linkedItems, err := linkedItemRepo.GetByUserID(ctx, userID)
	if err != nil {
		return 0, fmt.Errorf("failed to fetch user's linked items: %w", err)
	}

	totalCount := 0

	// Sync each linked item
	for _, item := range linkedItems {
		providerSource := domain.TransactionSource(item.Provider)
		provider, exists := o.providers[providerSource]
		if !exists {
			// Skip items with unregistered providers (log in production)
			continue
		}

		count, err := provider.SyncForLinkedItem(ctx, item.ID)
		if err != nil {
			// Log error but continue syncing other items
			// In production, would use structured logging
			continue
		}

		totalCount += count
	}

	return totalCount, nil
}

// SyncAll syncs all linked items across all users
// Note: This method would need a way to fetch all linked items across all users
// For now, it's a placeholder. In production, this would:
// 1. Fetch all linked items (possibly in batches)
// 2. Route each to appropriate provider
// 3. Track errors and successes
func (o *TransactionSyncOrchestrator) SyncAll(ctx context.Context) (int, error) {
	// TODO: Implement when needed for scheduled background sync
	// Would require either:
	// - A method to get all linked items across users
	// - Or iterate through all users and call SyncForUser
	return 0, fmt.Errorf("SyncAll not yet implemented - use SyncForUser instead")
}
