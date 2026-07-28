package services

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/repository"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// Mock implementations for testing PlaidItemCleanupService

// MockLinkedItemRepoForCleanup is a mock for LinkedItemRepository
type MockLinkedItemRepoForCleanup struct {
	mock.Mock
}

func (m *MockLinkedItemRepoForCleanup) GetByUserID(ctx context.Context, userID string) ([]domain.LinkedItem, error) {
	args := m.Called(ctx, userID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]domain.LinkedItem), args.Error(1)
}

func (m *MockLinkedItemRepoForCleanup) GetByID(ctx context.Context, id string) (*domain.LinkedItem, error) {
	args := m.Called(ctx, id)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*domain.LinkedItem), args.Error(1)
}

// Issue #679, #680: Trial expiration - methods for disconnecting items
func (m *MockLinkedItemRepoForCleanup) GetActiveItemsByUserID(ctx context.Context, userID string) ([]domain.LinkedItem, error) {
	args := m.Called(ctx, userID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]domain.LinkedItem), args.Error(1)
}

func (m *MockLinkedItemRepoForCleanup) MarkAllUserItemsDisconnected(ctx context.Context, userID string) error {
	args := m.Called(ctx, userID)
	return args.Error(0)
}

// MockPlaidServiceForCleanup is a mock for PlaidService
type MockPlaidServiceForCleanup struct {
	mock.Mock
}

func (m *MockPlaidServiceForCleanup) RemoveItem(ctx context.Context, accessToken string) error {
	args := m.Called(ctx, accessToken)
	return args.Error(0)
}

// Test_RemoveAllUserItems_CallsPlaidRemoveForEachItem verifies that
// RemoveAllUserItems calls Plaid /item/remove for each linked item.
// AccessToken is populated by the repository layer (repo-layer encryption).
func Test_RemoveAllUserItems_CallsPlaidRemoveForEachItem(t *testing.T) {
	ctx := context.Background()
	userID := "user-123"

	// Setup mocks
	mockRepo := new(MockLinkedItemRepoForCleanup)
	mockPlaid := new(MockPlaidServiceForCleanup)

	// Mock: user has 2 linked items with AccessToken already decrypted by repo
	items := []domain.LinkedItem{
		{ID: "item-1", UserID: userID, AccessToken: "decrypted-token-1"},
		{ID: "item-2", UserID: userID, AccessToken: "decrypted-token-2"},
	}
	mockRepo.On("GetByUserID", ctx, userID).Return(items, nil)

	// Mock: Plaid RemoveItem succeeds for both
	mockPlaid.On("RemoveItem", ctx, "decrypted-token-1").Return(nil)
	mockPlaid.On("RemoveItem", ctx, "decrypted-token-2").Return(nil)

	// Create service
	service := NewPlaidItemCleanupService(mockRepo, mockPlaid)

	// Execute
	err := service.RemoveAllUserItems(ctx, userID)

	// Verify
	assert.NoError(t, err)
	mockRepo.AssertExpectations(t)
	mockPlaid.AssertExpectations(t)

	// Verify Plaid was called for each item
	mockPlaid.AssertNumberOfCalls(t, "RemoveItem", 2)
}

// Test_RemoveAllUserItems_AggregatesErrorButAttemptsAllItems is a corrected test
// (Issue #1413 finding H6). It previously asserted the OPPOSITE — named
// "...ContinuesOnPlaidError" and asserting assert.NoError — encoding the "graceful
// degradation" behavior that made ExecuteDeletion's abort-on-cleanup-failure dead code:
// RemoveAllUserItems always returned nil, so the caller's `if err != nil { abort }` never
// fired, and the deletion cascade proceeded to clear the local Plaid references, orphaning
// the still-active (still-billing) Plaid Item with nothing left able to retry it.
//
// RemoveAllUserItems's only production caller (DeletionService.ExecuteDeletion) needs a
// genuine per-item failure to abort the deletion, so this function is now strict: it still
// attempts every item (a single bad item must not strand the rest, still billing), but
// aggregates any failures into a non-nil returned error.
func Test_RemoveAllUserItems_AggregatesErrorButAttemptsAllItems(t *testing.T) {
	ctx := context.Background()
	userID := "user-123"

	mockRepo := new(MockLinkedItemRepoForCleanup)
	mockPlaid := new(MockPlaidServiceForCleanup)

	// Mock: user has 2 linked items with AccessToken already decrypted by repo
	items := []domain.LinkedItem{
		{ID: "item-1", UserID: userID, AccessToken: "decrypted-token-1"},
		{ID: "item-2", UserID: userID, AccessToken: "decrypted-token-2"},
	}
	mockRepo.On("GetByUserID", ctx, userID).Return(items, nil)

	// Mock: First Plaid call fails, second succeeds
	plaidErr := errors.New("plaid API error")
	mockPlaid.On("RemoveItem", ctx, "decrypted-token-1").Return(plaidErr)
	mockPlaid.On("RemoveItem", ctx, "decrypted-token-2").Return(nil)

	service := NewPlaidItemCleanupService(mockRepo, mockPlaid)

	err := service.RemoveAllUserItems(ctx, userID)

	// The failure must be surfaced so a caller like ExecuteDeletion can abort instead of
	// silently proceeding to destroy the local record of the still-failed item.
	require.Error(t, err)
	assert.ErrorIs(t, err, plaidErr, "the aggregate error must wrap the underlying Plaid failure")

	// Both items must still have been attempted — one bad item must not strand the rest.
	mockPlaid.AssertNumberOfCalls(t, "RemoveItem", 2)
}

// Test_RemoveAllUserItems_AllItemsFail_AggregatesBothErrors proves the aggregate error
// isn't just "the first failure" — every failed item's error is preserved and every item
// is still attempted, even when none of them succeed.
func Test_RemoveAllUserItems_AllItemsFail_AggregatesBothErrors(t *testing.T) {
	ctx := context.Background()
	userID := "user-123"

	mockRepo := new(MockLinkedItemRepoForCleanup)
	mockPlaid := new(MockPlaidServiceForCleanup)

	items := []domain.LinkedItem{
		{ID: "item-1", UserID: userID, AccessToken: "decrypted-token-1"},
		{ID: "item-2", UserID: userID, AccessToken: "decrypted-token-2"},
	}
	mockRepo.On("GetByUserID", ctx, userID).Return(items, nil)

	err1 := errors.New("plaid API error 1")
	err2 := errors.New("plaid API error 2")
	mockPlaid.On("RemoveItem", ctx, "decrypted-token-1").Return(err1)
	mockPlaid.On("RemoveItem", ctx, "decrypted-token-2").Return(err2)

	service := NewPlaidItemCleanupService(mockRepo, mockPlaid)

	err := service.RemoveAllUserItems(ctx, userID)

	require.Error(t, err)
	assert.ErrorIs(t, err, err1)
	assert.ErrorIs(t, err, err2)
	mockPlaid.AssertNumberOfCalls(t, "RemoveItem", 2)
}

// Test_RemoveAllUserItems_HandlesNoLinkedItems verifies that
// RemoveAllUserItems succeeds silently when user has no linked items
func Test_RemoveAllUserItems_HandlesNoLinkedItems(t *testing.T) {
	ctx := context.Background()
	userID := "user-123"

	mockRepo := new(MockLinkedItemRepoForCleanup)
	mockPlaid := new(MockPlaidServiceForCleanup)

	// Mock: user has no linked items
	mockRepo.On("GetByUserID", ctx, userID).Return([]domain.LinkedItem{}, nil)

	service := NewPlaidItemCleanupService(mockRepo, mockPlaid)

	// Execute
	err := service.RemoveAllUserItems(ctx, userID)

	// Verify - no error for empty list
	assert.NoError(t, err)
	mockRepo.AssertExpectations(t)

	// Verify Plaid was never called
	mockPlaid.AssertNotCalled(t, "RemoveItem")
}

// Test_RemoveAllUserItems_HandlesRepoError verifies that
// RemoveAllUserItems returns error when repository fails
func Test_RemoveAllUserItems_HandlesRepoError(t *testing.T) {
	ctx := context.Background()
	userID := "user-123"

	mockRepo := new(MockLinkedItemRepoForCleanup)
	mockPlaid := new(MockPlaidServiceForCleanup)

	// Mock: repository returns error
	mockRepo.On("GetByUserID", ctx, userID).Return(nil, errors.New("database error"))

	service := NewPlaidItemCleanupService(mockRepo, mockPlaid)

	// Execute
	err := service.RemoveAllUserItems(ctx, userID)

	// Verify - repo error should be returned
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "database error")
}

// Test_RemoveItem_Success verifies that RemoveItem removes a single Plaid item.
// AccessToken is populated by the repository layer (repo-layer encryption).
func Test_RemoveItem_Success(t *testing.T) {
	ctx := context.Background()
	itemID := "item-123"
	userID := "user-456"

	mockRepo := new(MockLinkedItemRepoForCleanup)
	mockPlaid := new(MockPlaidServiceForCleanup)

	// Mock: repository returns the item with AccessToken already decrypted
	item := &domain.LinkedItem{
		ID:          itemID,
		UserID:      userID,
		AccessToken: "decrypted-token",
	}
	mockRepo.On("GetByID", ctx, itemID).Return(item, nil)

	// Mock: Plaid RemoveItem succeeds
	mockPlaid.On("RemoveItem", ctx, "decrypted-token").Return(nil)

	service := NewPlaidItemCleanupService(mockRepo, mockPlaid)

	// Execute
	err := service.RemoveItem(ctx, itemID)

	// Verify
	assert.NoError(t, err)
	mockRepo.AssertExpectations(t)
	mockPlaid.AssertExpectations(t)
}

// Test_RemoveItem_PlaidError verifies that RemoveItem handles Plaid errors gracefully
func Test_RemoveItem_PlaidError(t *testing.T) {
	ctx := context.Background()
	itemID := "item-123"

	mockRepo := new(MockLinkedItemRepoForCleanup)
	mockPlaid := new(MockPlaidServiceForCleanup)

	item := &domain.LinkedItem{
		ID:          itemID,
		AccessToken: "decrypted-token",
	}
	mockRepo.On("GetByID", ctx, itemID).Return(item, nil)

	// Mock: Plaid RemoveItem fails
	mockPlaid.On("RemoveItem", ctx, "decrypted-token").Return(errors.New("plaid error"))

	service := NewPlaidItemCleanupService(mockRepo, mockPlaid)

	// Execute - should NOT return error (graceful degradation)
	err := service.RemoveItem(ctx, itemID)

	// Verify - Plaid error logged but not returned
	assert.NoError(t, err)
}

// Test_RemoveItem_ItemNotFound verifies that RemoveItem handles missing items
func Test_RemoveItem_ItemNotFound(t *testing.T) {
	ctx := context.Background()
	itemID := "nonexistent-item"

	mockRepo := new(MockLinkedItemRepoForCleanup)
	mockPlaid := new(MockPlaidServiceForCleanup)

	// Mock: repository returns not found
	mockRepo.On("GetByID", ctx, itemID).Return(nil, errors.New("not found"))

	service := NewPlaidItemCleanupService(mockRepo, mockPlaid)

	// Execute
	err := service.RemoveItem(ctx, itemID)

	// Verify - not found error should be returned
	assert.Error(t, err)
}

// TestIsItemNotFoundError verifies the IsItemNotFoundError helper function
func TestIsItemNotFoundError(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{
			name: "ErrLinkedItemNotFound",
			err:  repository.ErrLinkedItemNotFound,
			want: true,
		},
		{
			name: "wrapped ErrLinkedItemNotFound",
			err:  fmt.Errorf("operation failed: %w", repository.ErrLinkedItemNotFound),
			want: true,
		},
		{
			name: "different error",
			err:  errors.New("some other error"),
			want: false,
		},
		{
			name: "nil error",
			err:  nil,
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, IsItemNotFoundError(tt.err))
		})
	}
}
