package workers

import (
	"context"
	"testing"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/stretchr/testify/assert"
)

// Simple tests for Plaid sync provider - focusing on interface compliance
// Full integration tests with mock Plaid API will be added in follow-up

func TestPlaidSyncProvider_GetProviderName_Simple(t *testing.T) {
	linkedItemRepo := new(MockLinkedItemRepository)
	txnRepo := new(MockTransactionRepository)
	plaidClient := new(MockPlaidClient)
	emitter := new(MockEventEmitter)

	provider := NewPlaidSyncProvider(linkedItemRepo, txnRepo, plaidClient, emitter)

	assert.Equal(t, domain.SourcePlaid, provider.GetProviderName())
}

func TestPlaidSyncProvider_SupportsIncrementalSync_Simple(t *testing.T) {
	linkedItemRepo := new(MockLinkedItemRepository)
	txnRepo := new(MockTransactionRepository)
	plaidClient := new(MockPlaidClient)
	emitter := new(MockEventEmitter)

	provider := NewPlaidSyncProvider(linkedItemRepo, txnRepo, plaidClient, emitter)

	assert.True(t, provider.SupportsIncrementalSync())
}

func TestPlaidSyncProvider_SyncForLinkedItem_LinkedItemNotFound_Simple(t *testing.T) {
	linkedItemRepo := new(MockLinkedItemRepository)
	txnRepo := new(MockTransactionRepository)
	plaidClient := new(MockPlaidClient)
	emitter := new(MockEventEmitter)

	provider := NewPlaidSyncProvider(linkedItemRepo, txnRepo, plaidClient, emitter)
	ctx := context.Background()

	linkedItemID := "nonexistent-item"

	// Mock repository error
	linkedItemRepo.On("GetByID", ctx, linkedItemID).Return(nil, ErrLinkedItemNotFound)

	// Execute
	count, err := provider.SyncForLinkedItem(ctx, linkedItemID)

	// Assert
	assert.Error(t, err)
	assert.Equal(t, 0, count)

	linkedItemRepo.AssertExpectations(t)
}
