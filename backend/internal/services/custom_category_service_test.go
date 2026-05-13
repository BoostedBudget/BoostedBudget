package services

import (
	"context"
	"testing"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/queue"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// MockCustomCategoryRepository is a mock implementation of repository.CustomCategoryRepository
type MockCustomCategoryRepository struct {
	mock.Mock
}

func (m *MockCustomCategoryRepository) Create(ctx context.Context, category *domain.CustomCategory) error {
	args := m.Called(ctx, category)
	return args.Error(0)
}

func (m *MockCustomCategoryRepository) GetByID(ctx context.Context, id string) (*domain.CustomCategory, error) {
	args := m.Called(ctx, id)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*domain.CustomCategory), args.Error(1)
}

func (m *MockCustomCategoryRepository) GetByUserID(ctx context.Context, userID string) ([]*domain.CustomCategory, error) {
	args := m.Called(ctx, userID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]*domain.CustomCategory), args.Error(1)
}

func (m *MockCustomCategoryRepository) GetByName(ctx context.Context, userID, name string) (*domain.CustomCategory, error) {
	args := m.Called(ctx, userID, name)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*domain.CustomCategory), args.Error(1)
}

func (m *MockCustomCategoryRepository) Update(ctx context.Context, category *domain.CustomCategory) error {
	args := m.Called(ctx, category)
	return args.Error(0)
}

func (m *MockCustomCategoryRepository) Delete(ctx context.Context, id string) error {
	args := m.Called(ctx, id)
	return args.Error(0)
}

func (m *MockCustomCategoryRepository) CountByUserID(ctx context.Context, userID string) (int, error) {
	args := m.Called(ctx, userID)
	return args.Int(0), args.Error(1)
}

func (m *MockCustomCategoryRepository) IncrementUsageCount(ctx context.Context, userID, categoryName string) error {
	args := m.Called(ctx, userID, categoryName)
	return args.Error(0)
}

func (m *MockCustomCategoryRepository) DecrementUsageCount(ctx context.Context, userID, categoryName string) error {
	args := m.Called(ctx, userID, categoryName)
	return args.Error(0)
}

func (m *MockCustomCategoryRepository) GetNamesByIDs(ctx context.Context, userID string, ids []string) (map[string]string, error) {
	args := m.Called(ctx, userID, ids)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(map[string]string), args.Error(1)
}

// TestCustomCategoryService_CreateCategory tests successful category creation
func TestCustomCategoryService_CreateCategory(t *testing.T) {
	mockCategoryRepo := new(MockCustomCategoryRepository)

	service := NewCustomCategoryService(mockCategoryRepo)

	ctx := context.Background()
	userID := "user-123"

	req := &domain.CreateCustomCategoryRequest{
		Name: "Groceries",
	}

	// Setup mocks - categories are unlimited, so we just need Create
	mockCategoryRepo.On("Create", ctx, mock.AnythingOfType("*domain.CustomCategory")).Return(nil)

	// Execute
	category, err := service.CreateCategory(ctx, userID, req)

	// Assert
	assert.NoError(t, err)
	assert.NotNil(t, category)
	assert.Equal(t, "Groceries", category.Name)
	assert.Equal(t, userID, category.UserID)
	mockCategoryRepo.AssertExpectations(t)
}

// TestCustomCategoryService_CreateCategory_ManyCategories tests creating categories with many existing (unlimited)
func TestCustomCategoryService_CreateCategory_ManyCategories(t *testing.T) {
	mockCategoryRepo := new(MockCustomCategoryRepository)

	service := NewCustomCategoryService(mockCategoryRepo)

	ctx := context.Background()
	userID := "user-123"

	req := &domain.CreateCustomCategoryRequest{
		Name: "unlimited-category",
	}

	// Categories are unlimited for all tiers
	mockCategoryRepo.On("Create", ctx, mock.AnythingOfType("*domain.CustomCategory")).Return(nil)

	// Execute
	category, err := service.CreateCategory(ctx, userID, req)

	// Assert
	assert.NoError(t, err)
	assert.NotNil(t, category)
	mockCategoryRepo.AssertExpectations(t)
}

// TestCustomCategoryService_GetUserCategories tests retrieving all user categories
func TestCustomCategoryService_GetUserCategories(t *testing.T) {
	mockCategoryRepo := new(MockCustomCategoryRepository)

	service := NewCustomCategoryService(mockCategoryRepo)

	ctx := context.Background()
	userID := "user-123"

	expectedCategories := []*domain.CustomCategory{
		{ID: "cat-1", UserID: userID, Name: "Groceries"},
		{ID: "cat-2", UserID: userID, Name: "Entertainment"},
	}

	mockCategoryRepo.On("GetByUserID", ctx, userID).Return(expectedCategories, nil)

	// Execute
	categories, err := service.GetUserCategories(ctx, userID)

	// Assert
	assert.NoError(t, err)
	assert.Len(t, categories, 2)
	mockCategoryRepo.AssertExpectations(t)
}

// TestCustomCategoryService_UpdateCategory tests successful category update
func TestCustomCategoryService_UpdateCategory(t *testing.T) {
	mockCategoryRepo := new(MockCustomCategoryRepository)

	service := NewCustomCategoryService(mockCategoryRepo)

	ctx := context.Background()
	userID := "user-123"
	categoryID := "cat-456"
	newName := "Updated Name"
	newColor := "#FF0000"

	existingCategory := &domain.CustomCategory{
		ID:     categoryID,
		UserID: userID,
		Name:   "Old Name",
	}

	req := &domain.UpdateCustomCategoryRequest{
		Name:  &newName,
		Color: &newColor,
	}

	mockCategoryRepo.On("GetByID", ctx, categoryID).Return(existingCategory, nil)
	mockCategoryRepo.On("Update", ctx, mock.AnythingOfType("*domain.CustomCategory")).Return(nil)

	// Execute
	category, err := service.UpdateCategory(ctx, userID, categoryID, req)

	// Assert
	assert.NoError(t, err)
	assert.NotNil(t, category)
	assert.Equal(t, newName, category.Name)
	assert.Equal(t, &newColor, category.Color)
	mockCategoryRepo.AssertExpectations(t)
}

// TestCustomCategoryService_UpdateCategory_WrongOwner tests update attempt by wrong user
func TestCustomCategoryService_UpdateCategory_WrongOwner(t *testing.T) {
	mockCategoryRepo := new(MockCustomCategoryRepository)

	service := NewCustomCategoryService(mockCategoryRepo)

	ctx := context.Background()
	userID := "user-123"
	wrongUserID := "user-456"
	categoryID := "cat-789"
	newName := "hacker-name"

	existingCategory := &domain.CustomCategory{
		ID:     categoryID,
		UserID: userID, // Different from wrongUserID
		Name:   "Original",
	}

	req := &domain.UpdateCustomCategoryRequest{
		Name: &newName,
	}

	mockCategoryRepo.On("GetByID", ctx, categoryID).Return(existingCategory, nil)

	// Execute - attempt to update with wrong user ID
	category, err := service.UpdateCategory(ctx, wrongUserID, categoryID, req)

	// Assert
	assert.ErrorIs(t, err, domain.ErrCategoryNotFound)
	assert.Nil(t, category)
	mockCategoryRepo.AssertExpectations(t)
}

// TestCustomCategoryService_DeleteCategory tests successful category deletion
func TestCustomCategoryService_DeleteCategory(t *testing.T) {
	mockCategoryRepo := new(MockCustomCategoryRepository)

	service := NewCustomCategoryService(mockCategoryRepo)

	ctx := context.Background()
	userID := "user-123"
	categoryID := "cat-456"

	existingCategory := &domain.CustomCategory{
		ID:     categoryID,
		UserID: userID,
		Name:   "to-delete",
	}

	mockCategoryRepo.On("GetByID", ctx, categoryID).Return(existingCategory, nil)
	mockCategoryRepo.On("Delete", ctx, categoryID).Return(nil)

	// Execute
	err := service.DeleteCategory(ctx, userID, categoryID)

	// Assert
	assert.NoError(t, err)
	mockCategoryRepo.AssertExpectations(t)
}

// TestCustomCategoryService_GetUserCategories_PopulatesPFCDisplay asserts that GetUserCategories
// enriches each category with ParentPFCPrimaryDisplay before returning.
func TestCustomCategoryService_GetUserCategories_PopulatesPFCDisplay(t *testing.T) {
	mockCategoryRepo := new(MockCustomCategoryRepository)
	service := NewCustomCategoryService(mockCategoryRepo)

	ctx := context.Background()
	userID := "user-123"
	parent := "FOOD_AND_DRINK"

	categories := []*domain.CustomCategory{
		{ID: "cat-1", UserID: userID, Name: "My Food", ParentPFCPrimary: &parent},
	}

	mockCategoryRepo.On("GetByUserID", ctx, userID).Return(categories, nil)

	result, err := service.GetUserCategories(ctx, userID)

	require.NoError(t, err)
	require.Len(t, result, 1)
	require.NotNil(t, result[0].ParentPFCPrimaryDisplay, "ParentPFCPrimaryDisplay should be populated")
	assert.Equal(t, "Food And Drink", *result[0].ParentPFCPrimaryDisplay)
	mockCategoryRepo.AssertExpectations(t)
}

// TestCustomCategoryService_GetCategoryByID_PopulatesPFCDisplay asserts that GetCategoryByID
// enriches the category with ParentPFCPrimaryDisplay before returning.
func TestCustomCategoryService_GetCategoryByID_PopulatesPFCDisplay(t *testing.T) {
	mockCategoryRepo := new(MockCustomCategoryRepository)
	service := NewCustomCategoryService(mockCategoryRepo)

	ctx := context.Background()
	categoryID := "cat-pfc"
	parent := "TRANSPORTATION"

	category := &domain.CustomCategory{
		ID:               categoryID,
		UserID:           "user-123",
		Name:             "My Transport",
		ParentPFCPrimary: &parent,
	}

	mockCategoryRepo.On("GetByID", ctx, categoryID).Return(category, nil)

	result, err := service.GetCategoryByID(ctx, categoryID)

	require.NoError(t, err)
	require.NotNil(t, result.ParentPFCPrimaryDisplay, "ParentPFCPrimaryDisplay should be populated")
	assert.Equal(t, "Transportation", *result.ParentPFCPrimaryDisplay)
	mockCategoryRepo.AssertExpectations(t)
}

// ===========================================================================
// Issue #1123: Effective category name cascade rename tests
// ===========================================================================

// TestCustomCategoryService_UpdateCategory_EnqueuesRefreshJobOnRename asserts that
// when the category name changes, an effectiveCategoryRefreshJobArgs job is enqueued.
func TestCustomCategoryService_UpdateCategory_EnqueuesRefreshJobOnRename(t *testing.T) {
	mockCategoryRepo := new(MockCustomCategoryRepository)
	mockQueue := &MockJobQueue{}

	svc := NewCustomCategoryService(mockCategoryRepo)
	svc.SetJobQueue(mockQueue)

	ctx := context.Background()
	userID := "user-123"
	categoryID := "cat-456"
	oldName := "Fast Food"
	newName := "Dining Out"

	existingCategory := &domain.CustomCategory{
		ID:     categoryID,
		UserID: userID,
		Name:   oldName,
	}

	req := &domain.UpdateCustomCategoryRequest{
		Name: &newName,
	}

	mockCategoryRepo.On("GetByID", ctx, categoryID).Return(existingCategory, nil)
	mockCategoryRepo.On("Update", ctx, mock.AnythingOfType("*domain.CustomCategory")).Return(nil)
	mockQueue.On("Enqueue", ctx,
		mock.MatchedBy(func(job queue.Job) bool {
			args, ok := job.(effectiveCategoryRefreshJobArgs)
			return ok &&
				args.UserID == userID &&
				args.CustomCategoryID == categoryID
		}),
		mock.Anything,
	).Return(&queue.JobResult{ID: 1, Kind: "effective_category_refresh"}, nil)

	category, err := svc.UpdateCategory(ctx, userID, categoryID, req)

	require.NoError(t, err)
	assert.Equal(t, newName, category.Name)
	mockCategoryRepo.AssertExpectations(t)
	mockQueue.AssertExpectations(t)
}

// TestCustomCategoryService_UpdateCategory_NoJobWhenNameUnchanged asserts that
// no job is enqueued when the update request does not change the name.
func TestCustomCategoryService_UpdateCategory_NoJobWhenNameUnchanged(t *testing.T) {
	mockCategoryRepo := new(MockCustomCategoryRepository)
	mockQueue := &MockJobQueue{}

	svc := NewCustomCategoryService(mockCategoryRepo)
	svc.SetJobQueue(mockQueue)

	ctx := context.Background()
	userID := "user-123"
	categoryID := "cat-456"
	sameName := "Groceries"
	newColor := "#00FF00"

	existingCategory := &domain.CustomCategory{
		ID:     categoryID,
		UserID: userID,
		Name:   sameName,
	}

	req := &domain.UpdateCustomCategoryRequest{
		Name:  &sameName, // Same name — no rename
		Color: &newColor,
	}

	mockCategoryRepo.On("GetByID", ctx, categoryID).Return(existingCategory, nil)
	mockCategoryRepo.On("Update", ctx, mock.AnythingOfType("*domain.CustomCategory")).Return(nil)

	category, err := svc.UpdateCategory(ctx, userID, categoryID, req)

	require.NoError(t, err)
	assert.Equal(t, sameName, category.Name)
	mockCategoryRepo.AssertExpectations(t)
	// Enqueue must NOT have been called.
	mockQueue.AssertNotCalled(t, "Enqueue", mock.Anything, mock.Anything, mock.Anything)
}

// TestCustomCategoryService_UpdateCategory_NoJobWhenQueueNotSet asserts that
// when SetJobQueue has not been called, a rename still succeeds without panicking.
func TestCustomCategoryService_UpdateCategory_NoJobWhenQueueNotSet(t *testing.T) {
	mockCategoryRepo := new(MockCustomCategoryRepository)

	// No SetJobQueue call — jobQueue is nil.
	svc := NewCustomCategoryService(mockCategoryRepo)

	ctx := context.Background()
	userID := "user-123"
	categoryID := "cat-456"
	newName := "Renamed Category"

	existingCategory := &domain.CustomCategory{
		ID:     categoryID,
		UserID: userID,
		Name:   "Old Name",
	}

	req := &domain.UpdateCustomCategoryRequest{
		Name: &newName,
	}

	mockCategoryRepo.On("GetByID", ctx, categoryID).Return(existingCategory, nil)
	mockCategoryRepo.On("Update", ctx, mock.AnythingOfType("*domain.CustomCategory")).Return(nil)

	category, err := svc.UpdateCategory(ctx, userID, categoryID, req)

	require.NoError(t, err)
	assert.Equal(t, newName, category.Name)
	mockCategoryRepo.AssertExpectations(t)
}

// TestCustomCategoryService_UpdateCategory_EnqueueErrorDoesNotFail asserts that
// when enqueuing fails, the UpdateCategory call still succeeds (best-effort enqueue).
func TestCustomCategoryService_UpdateCategory_EnqueueErrorDoesNotFail(t *testing.T) {
	mockCategoryRepo := new(MockCustomCategoryRepository)
	mockQueue := &MockJobQueue{}

	svc := NewCustomCategoryService(mockCategoryRepo)
	svc.SetJobQueue(mockQueue)

	ctx := context.Background()
	userID := "user-123"
	categoryID := "cat-456"
	newName := "Renamed"

	existingCategory := &domain.CustomCategory{
		ID:     categoryID,
		UserID: userID,
		Name:   "Original",
	}

	req := &domain.UpdateCustomCategoryRequest{
		Name: &newName,
	}

	mockCategoryRepo.On("GetByID", ctx, categoryID).Return(existingCategory, nil)
	mockCategoryRepo.On("Update", ctx, mock.AnythingOfType("*domain.CustomCategory")).Return(nil)
	mockQueue.On("Enqueue", ctx,
		mock.MatchedBy(func(job queue.Job) bool {
			_, ok := job.(effectiveCategoryRefreshJobArgs)
			return ok
		}),
		mock.Anything,
	).Return(nil, assert.AnError)

	category, err := svc.UpdateCategory(ctx, userID, categoryID, req)

	// Rename persisted — enqueue failure does not propagate.
	require.NoError(t, err)
	assert.Equal(t, newName, category.Name)
	mockCategoryRepo.AssertExpectations(t)
	mockQueue.AssertExpectations(t)
}
