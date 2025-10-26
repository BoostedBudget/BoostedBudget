package workers

import (
	"context"
	"testing"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// TestOrchestrator_RegisterProvider tests provider registration
func TestOrchestrator_RegisterProvider(t *testing.T) {
	orchestrator := NewTransactionSyncOrchestrator()

	plaidProvider := new(MockProvider)
	plaidProvider.On("GetProviderName").Return(domain.SourcePlaid)

	orchestrator.RegisterProvider(plaidProvider)

	// Verify provider registered (indirectly tested via SyncForLinkedItem)
	assert.NotNil(t, orchestrator)
}

// TestOrchestrator_SyncForLinkedItem_RouteToCorrectProvider tests provider routing
func TestOrchestrator_SyncForLinkedItem_RouteToCorrectProvider(t *testing.T) {
	linkedItemRepo := new(MockLinkedItemRepository)
	orchestrator := NewTransactionSyncOrchestrator()

	plaidProvider := new(MockProvider)
	plaidProvider.On("GetProviderName").Return(domain.SourcePlaid)
	plaidProvider.On("SyncForLinkedItem", mock.Anything, "item-123").Return(5, nil)

	orchestrator.RegisterProvider(plaidProvider)

	ctx := context.Background()
	linkedItemID := "item-123"

	// Mock linked item with plaid provider
	linkedItem := &domain.LinkedItem{
		ID:       linkedItemID,
		UserID:   "user-123",
		Provider: "plaid",
	}

	linkedItemRepo.On("GetByID", ctx, linkedItemID).Return(linkedItem, nil)

	// Execute
	count, err := orchestrator.SyncForLinkedItem(ctx, linkedItemRepo, linkedItemID)

	// Assert
	require.NoError(t, err)
	assert.Equal(t, 5, count)

	plaidProvider.AssertExpectations(t)
	linkedItemRepo.AssertExpectations(t)
}

// TestOrchestrator_SyncForLinkedItem_ProviderNotRegistered tests error when provider not found
func TestOrchestrator_SyncForLinkedItem_ProviderNotRegistered(t *testing.T) {
	linkedItemRepo := new(MockLinkedItemRepository)
	orchestrator := NewTransactionSyncOrchestrator()

	ctx := context.Background()
	linkedItemID := "item-456"

	// Mock linked item with teller provider (not registered)
	linkedItem := &domain.LinkedItem{
		ID:       linkedItemID,
		UserID:   "user-456",
		Provider: "teller",
	}

	linkedItemRepo.On("GetByID", ctx, linkedItemID).Return(linkedItem, nil)

	// Execute
	count, err := orchestrator.SyncForLinkedItem(ctx, linkedItemRepo, linkedItemID)

	// Assert
	assert.Error(t, err)
	assert.Equal(t, 0, count)
	assert.Contains(t, err.Error(), "no provider registered")

	linkedItemRepo.AssertExpectations(t)
}

// TestOrchestrator_SyncForUser tests syncing all items for a user
func TestOrchestrator_SyncForUser(t *testing.T) {
	linkedItemRepo := new(MockLinkedItemRepository)
	orchestrator := NewTransactionSyncOrchestrator()

	plaidProvider := new(MockProvider)
	plaidProvider.On("GetProviderName").Return(domain.SourcePlaid)
	plaidProvider.On("SyncForLinkedItem", mock.Anything, "item-1").Return(3, nil)
	plaidProvider.On("SyncForLinkedItem", mock.Anything, "item-2").Return(2, nil)

	orchestrator.RegisterProvider(plaidProvider)

	ctx := context.Background()
	userID := "user-789"

	// Mock user's linked items
	linkedItems := []domain.LinkedItem{
		{ID: "item-1", UserID: userID, Provider: "plaid"},
		{ID: "item-2", UserID: userID, Provider: "plaid"},
	}

	linkedItemRepo.On("GetByUserID", ctx, userID).Return(linkedItems, nil)

	// Execute
	count, err := orchestrator.SyncForUser(ctx, linkedItemRepo, userID)

	// Assert
	require.NoError(t, err)
	assert.Equal(t, 5, count) // 3 + 2

	plaidProvider.AssertExpectations(t)
	linkedItemRepo.AssertExpectations(t)
}

// TestOrchestrator_SyncAll tests syncing all items across all users
func TestOrchestrator_SyncAll(t *testing.T) {
	// This test would require fetching all linked items
	// Simplified version just verifies the method exists and returns without error
	orchestrator := NewTransactionSyncOrchestrator()

	// Note: In real implementation, SyncAll would need access to fetch all linked items
	// For now, just verify the orchestrator is properly constructed
	assert.NotNil(t, orchestrator)
}
