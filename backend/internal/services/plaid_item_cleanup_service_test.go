package services

import (
	"context"
	"errors"
	"testing"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
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

// MockEncryptionServiceForCleanup is a mock for EncryptionService
type MockEncryptionServiceForCleanup struct {
	mock.Mock
}

func (m *MockEncryptionServiceForCleanup) Decrypt(ciphertext string) (string, error) {
	args := m.Called(ciphertext)
	return args.String(0), args.Error(1)
}

// Test_RemoveAllUserItems_CallsPlaidRemoveForEachItem verifies that
// RemoveAllUserItems calls Plaid /item/remove for each linked item
func Test_RemoveAllUserItems_CallsPlaidRemoveForEachItem(t *testing.T) {
	ctx := context.Background()
	userID := "user-123"

	// Setup mocks
	mockRepo := new(MockLinkedItemRepoForCleanup)
	mockPlaid := new(MockPlaidServiceForCleanup)
	mockEncryption := new(MockEncryptionServiceForCleanup)

	// Mock: user has 2 linked items
	items := []domain.LinkedItem{
		{ID: "item-1", UserID: userID, AccessTokenEncrypted: "encrypted-token-1"},
		{ID: "item-2", UserID: userID, AccessTokenEncrypted: "encrypted-token-2"},
	}
	mockRepo.On("GetByUserID", ctx, userID).Return(items, nil)

	// Mock: decrypt returns decrypted tokens
	mockEncryption.On("Decrypt", "encrypted-token-1").Return("decrypted-token-1", nil)
	mockEncryption.On("Decrypt", "encrypted-token-2").Return("decrypted-token-2", nil)

	// Mock: Plaid RemoveItem succeeds for both
	mockPlaid.On("RemoveItem", ctx, "decrypted-token-1").Return(nil)
	mockPlaid.On("RemoveItem", ctx, "decrypted-token-2").Return(nil)

	// Create service
	service := NewPlaidItemCleanupService(mockRepo, mockPlaid, mockEncryption)

	// Execute
	err := service.RemoveAllUserItems(ctx, userID)

	// Verify
	assert.NoError(t, err)
	mockRepo.AssertExpectations(t)
	mockEncryption.AssertExpectations(t)
	mockPlaid.AssertExpectations(t)

	// Verify Plaid was called for each item
	mockPlaid.AssertNumberOfCalls(t, "RemoveItem", 2)
}

// Test_RemoveAllUserItems_ContinuesOnPlaidError verifies that
// RemoveAllUserItems continues processing other items even if one Plaid call fails
func Test_RemoveAllUserItems_ContinuesOnPlaidError(t *testing.T) {
	ctx := context.Background()
	userID := "user-123"

	mockRepo := new(MockLinkedItemRepoForCleanup)
	mockPlaid := new(MockPlaidServiceForCleanup)
	mockEncryption := new(MockEncryptionServiceForCleanup)

	// Mock: user has 2 linked items
	items := []domain.LinkedItem{
		{ID: "item-1", UserID: userID, AccessTokenEncrypted: "encrypted-token-1"},
		{ID: "item-2", UserID: userID, AccessTokenEncrypted: "encrypted-token-2"},
	}
	mockRepo.On("GetByUserID", ctx, userID).Return(items, nil)

	// Mock: decrypt succeeds for both
	mockEncryption.On("Decrypt", "encrypted-token-1").Return("decrypted-token-1", nil)
	mockEncryption.On("Decrypt", "encrypted-token-2").Return("decrypted-token-2", nil)

	// Mock: First Plaid call fails, second succeeds
	mockPlaid.On("RemoveItem", ctx, "decrypted-token-1").Return(errors.New("plaid API error"))
	mockPlaid.On("RemoveItem", ctx, "decrypted-token-2").Return(nil)

	service := NewPlaidItemCleanupService(mockRepo, mockPlaid, mockEncryption)

	// Execute - should not return error (graceful degradation)
	err := service.RemoveAllUserItems(ctx, userID)

	// Verify - error is logged but not returned (graceful degradation)
	assert.NoError(t, err)

	// Verify both items were processed
	mockPlaid.AssertNumberOfCalls(t, "RemoveItem", 2)
}

// Test_RemoveAllUserItems_HandlesNoLinkedItems verifies that
// RemoveAllUserItems succeeds silently when user has no linked items
func Test_RemoveAllUserItems_HandlesNoLinkedItems(t *testing.T) {
	ctx := context.Background()
	userID := "user-123"

	mockRepo := new(MockLinkedItemRepoForCleanup)
	mockPlaid := new(MockPlaidServiceForCleanup)
	mockEncryption := new(MockEncryptionServiceForCleanup)

	// Mock: user has no linked items
	mockRepo.On("GetByUserID", ctx, userID).Return([]domain.LinkedItem{}, nil)

	service := NewPlaidItemCleanupService(mockRepo, mockPlaid, mockEncryption)

	// Execute
	err := service.RemoveAllUserItems(ctx, userID)

	// Verify - no error for empty list
	assert.NoError(t, err)
	mockRepo.AssertExpectations(t)

	// Verify Plaid was never called
	mockPlaid.AssertNotCalled(t, "RemoveItem")
}

// Test_RemoveAllUserItems_HandlesDecryptionError verifies that
// RemoveAllUserItems continues processing other items even if decryption fails for one
func Test_RemoveAllUserItems_HandlesDecryptionError(t *testing.T) {
	ctx := context.Background()
	userID := "user-123"

	mockRepo := new(MockLinkedItemRepoForCleanup)
	mockPlaid := new(MockPlaidServiceForCleanup)
	mockEncryption := new(MockEncryptionServiceForCleanup)

	// Mock: user has 2 linked items
	items := []domain.LinkedItem{
		{ID: "item-1", UserID: userID, AccessTokenEncrypted: "encrypted-token-1"},
		{ID: "item-2", UserID: userID, AccessTokenEncrypted: "encrypted-token-2"},
	}
	mockRepo.On("GetByUserID", ctx, userID).Return(items, nil)

	// Mock: first decryption fails, second succeeds
	mockEncryption.On("Decrypt", "encrypted-token-1").Return("", errors.New("decryption failed"))
	mockEncryption.On("Decrypt", "encrypted-token-2").Return("decrypted-token-2", nil)

	// Mock: Plaid RemoveItem only called for second item
	mockPlaid.On("RemoveItem", ctx, "decrypted-token-2").Return(nil)

	service := NewPlaidItemCleanupService(mockRepo, mockPlaid, mockEncryption)

	// Execute - should not return error (graceful degradation)
	err := service.RemoveAllUserItems(ctx, userID)

	// Verify
	assert.NoError(t, err)

	// Verify Plaid was only called for the item with successful decryption
	mockPlaid.AssertNumberOfCalls(t, "RemoveItem", 1)
}

// Test_RemoveAllUserItems_HandlesRepoError verifies that
// RemoveAllUserItems returns error when repository fails
func Test_RemoveAllUserItems_HandlesRepoError(t *testing.T) {
	ctx := context.Background()
	userID := "user-123"

	mockRepo := new(MockLinkedItemRepoForCleanup)
	mockPlaid := new(MockPlaidServiceForCleanup)
	mockEncryption := new(MockEncryptionServiceForCleanup)

	// Mock: repository returns error
	mockRepo.On("GetByUserID", ctx, userID).Return(nil, errors.New("database error"))

	service := NewPlaidItemCleanupService(mockRepo, mockPlaid, mockEncryption)

	// Execute
	err := service.RemoveAllUserItems(ctx, userID)

	// Verify - repo error should be returned
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "database error")
}

// Test_RemoveItem_Success verifies that RemoveItem removes a single Plaid item
func Test_RemoveItem_Success(t *testing.T) {
	ctx := context.Background()
	itemID := "item-123"
	userID := "user-456"

	mockRepo := new(MockLinkedItemRepoForCleanup)
	mockPlaid := new(MockPlaidServiceForCleanup)
	mockEncryption := new(MockEncryptionServiceForCleanup)

	// Mock: repository returns the item
	item := &domain.LinkedItem{
		ID:                   itemID,
		UserID:               userID,
		AccessTokenEncrypted: "encrypted-token",
	}
	mockRepo.On("GetByID", ctx, itemID).Return(item, nil)

	// Mock: decrypt succeeds
	mockEncryption.On("Decrypt", "encrypted-token").Return("decrypted-token", nil)

	// Mock: Plaid RemoveItem succeeds
	mockPlaid.On("RemoveItem", ctx, "decrypted-token").Return(nil)

	service := NewPlaidItemCleanupService(mockRepo, mockPlaid, mockEncryption)

	// Execute
	err := service.RemoveItem(ctx, itemID)

	// Verify
	assert.NoError(t, err)
	mockRepo.AssertExpectations(t)
	mockEncryption.AssertExpectations(t)
	mockPlaid.AssertExpectations(t)
}

// Test_RemoveItem_PlaidError verifies that RemoveItem handles Plaid errors gracefully
func Test_RemoveItem_PlaidError(t *testing.T) {
	ctx := context.Background()
	itemID := "item-123"

	mockRepo := new(MockLinkedItemRepoForCleanup)
	mockPlaid := new(MockPlaidServiceForCleanup)
	mockEncryption := new(MockEncryptionServiceForCleanup)

	item := &domain.LinkedItem{
		ID:                   itemID,
		AccessTokenEncrypted: "encrypted-token",
	}
	mockRepo.On("GetByID", ctx, itemID).Return(item, nil)
	mockEncryption.On("Decrypt", "encrypted-token").Return("decrypted-token", nil)

	// Mock: Plaid RemoveItem fails
	mockPlaid.On("RemoveItem", ctx, "decrypted-token").Return(errors.New("plaid error"))

	service := NewPlaidItemCleanupService(mockRepo, mockPlaid, mockEncryption)

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
	mockEncryption := new(MockEncryptionServiceForCleanup)

	// Mock: repository returns not found
	mockRepo.On("GetByID", ctx, itemID).Return(nil, errors.New("not found"))

	service := NewPlaidItemCleanupService(mockRepo, mockPlaid, mockEncryption)

	// Execute
	err := service.RemoveItem(ctx, itemID)

	// Verify - not found error should be returned
	assert.Error(t, err)
}
