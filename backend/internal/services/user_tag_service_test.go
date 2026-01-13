package services

import (
	"context"
	"testing"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

// MockUserTagRepository is a mock implementation of repository.UserTagRepository
type MockUserTagRepository struct {
	mock.Mock
}

func (m *MockUserTagRepository) Create(ctx context.Context, tag *domain.UserTag) error {
	args := m.Called(ctx, tag)
	return args.Error(0)
}

func (m *MockUserTagRepository) GetByID(ctx context.Context, id string) (*domain.UserTag, error) {
	args := m.Called(ctx, id)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*domain.UserTag), args.Error(1)
}

func (m *MockUserTagRepository) GetByUserID(ctx context.Context, userID string) ([]*domain.UserTag, error) {
	args := m.Called(ctx, userID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]*domain.UserTag), args.Error(1)
}

func (m *MockUserTagRepository) GetByName(ctx context.Context, userID, name string) (*domain.UserTag, error) {
	args := m.Called(ctx, userID, name)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*domain.UserTag), args.Error(1)
}

func (m *MockUserTagRepository) Update(ctx context.Context, tag *domain.UserTag) error {
	args := m.Called(ctx, tag)
	return args.Error(0)
}

func (m *MockUserTagRepository) Delete(ctx context.Context, id string) error {
	args := m.Called(ctx, id)
	return args.Error(0)
}

func (m *MockUserTagRepository) CountByUserID(ctx context.Context, userID string) (int, error) {
	args := m.Called(ctx, userID)
	return args.Int(0), args.Error(1)
}

func (m *MockUserTagRepository) IncrementUsageCount(ctx context.Context, userID, tagName string) error {
	args := m.Called(ctx, userID, tagName)
	return args.Error(0)
}

func (m *MockUserTagRepository) DecrementUsageCount(ctx context.Context, userID, tagName string) error {
	args := m.Called(ctx, userID, tagName)
	return args.Error(0)
}

// TestUserTagService_CreateTag tests successful tag creation
func TestUserTagService_CreateTag(t *testing.T) {
	mockTagRepo := new(MockUserTagRepository)

	service := NewUserTagService(mockTagRepo)

	ctx := context.Background()
	userID := "user-123"

	req := &domain.CreateUserTagRequest{
		Name: "business",
	}

	// Tags are unlimited, so we just need Create
	mockTagRepo.On("Create", ctx, mock.AnythingOfType("*domain.UserTag")).Return(nil)

	// Execute
	tag, err := service.CreateTag(ctx, userID, req)

	// Assert
	assert.NoError(t, err)
	assert.NotNil(t, tag)
	assert.Equal(t, "business", tag.Name)
	assert.Equal(t, userID, tag.UserID)
	mockTagRepo.AssertExpectations(t)
}

// TestUserTagService_CreateTag_ManyTags tests tag creation with many existing tags (unlimited)
func TestUserTagService_CreateTag_ManyTags(t *testing.T) {
	mockTagRepo := new(MockUserTagRepository)

	service := NewUserTagService(mockTagRepo)

	ctx := context.Background()
	userID := "user-123"

	req := &domain.CreateUserTagRequest{
		Name: "unlimited-tag",
	}

	// Tags are unlimited for all tiers
	mockTagRepo.On("Create", ctx, mock.AnythingOfType("*domain.UserTag")).Return(nil)

	// Execute
	tag, err := service.CreateTag(ctx, userID, req)

	// Assert
	assert.NoError(t, err)
	assert.NotNil(t, tag)
	mockTagRepo.AssertExpectations(t)
}

// TestUserTagService_GetUserTags tests retrieving all user tags
func TestUserTagService_GetUserTags(t *testing.T) {
	mockTagRepo := new(MockUserTagRepository)

	service := NewUserTagService(mockTagRepo)

	ctx := context.Background()
	userID := "user-123"

	expectedTags := []*domain.UserTag{
		{ID: "tag-1", UserID: userID, Name: "business"},
		{ID: "tag-2", UserID: userID, Name: "personal"},
	}

	mockTagRepo.On("GetByUserID", ctx, userID).Return(expectedTags, nil)

	// Execute
	tags, err := service.GetUserTags(ctx, userID)

	// Assert
	assert.NoError(t, err)
	assert.Len(t, tags, 2)
	mockTagRepo.AssertExpectations(t)
}

// TestUserTagService_UpdateTag tests successful tag update
func TestUserTagService_UpdateTag(t *testing.T) {
	mockTagRepo := new(MockUserTagRepository)

	service := NewUserTagService(mockTagRepo)

	ctx := context.Background()
	userID := "user-123"
	tagID := "tag-456"
	newName := "updated-name"
	newColor := "#FF0000"

	existingTag := &domain.UserTag{
		ID:     tagID,
		UserID: userID,
		Name:   "old-name",
	}

	req := &domain.UpdateUserTagRequest{
		Name:  &newName,
		Color: &newColor,
	}

	mockTagRepo.On("GetByID", ctx, tagID).Return(existingTag, nil)
	mockTagRepo.On("Update", ctx, mock.AnythingOfType("*domain.UserTag")).Return(nil)

	// Execute
	tag, err := service.UpdateTag(ctx, userID, tagID, req)

	// Assert
	assert.NoError(t, err)
	assert.NotNil(t, tag)
	assert.Equal(t, newName, tag.Name)
	assert.Equal(t, &newColor, tag.Color)
	mockTagRepo.AssertExpectations(t)
}

// TestUserTagService_UpdateTag_WrongOwner tests update attempt by wrong user
func TestUserTagService_UpdateTag_WrongOwner(t *testing.T) {
	mockTagRepo := new(MockUserTagRepository)

	service := NewUserTagService(mockTagRepo)

	ctx := context.Background()
	userID := "user-123"
	wrongUserID := "user-456"
	tagID := "tag-789"
	newName := "hacker-name"

	existingTag := &domain.UserTag{
		ID:     tagID,
		UserID: userID, // Different from wrongUserID
		Name:   "original",
	}

	req := &domain.UpdateUserTagRequest{
		Name: &newName,
	}

	mockTagRepo.On("GetByID", ctx, tagID).Return(existingTag, nil)

	// Execute - attempt to update with wrong user ID
	tag, err := service.UpdateTag(ctx, wrongUserID, tagID, req)

	// Assert
	assert.ErrorIs(t, err, domain.ErrTagNotFound)
	assert.Nil(t, tag)
	mockTagRepo.AssertExpectations(t)
}

// TestUserTagService_DeleteTag tests successful tag deletion
func TestUserTagService_DeleteTag(t *testing.T) {
	mockTagRepo := new(MockUserTagRepository)

	service := NewUserTagService(mockTagRepo)

	ctx := context.Background()
	userID := "user-123"
	tagID := "tag-456"

	existingTag := &domain.UserTag{
		ID:     tagID,
		UserID: userID,
		Name:   "to-delete",
	}

	mockTagRepo.On("GetByID", ctx, tagID).Return(existingTag, nil)
	mockTagRepo.On("Delete", ctx, tagID).Return(nil)

	// Execute
	err := service.DeleteTag(ctx, userID, tagID)

	// Assert
	assert.NoError(t, err)
	mockTagRepo.AssertExpectations(t)
}

// TestUserTagService_GetOrCreateTag_Existing tests getting an existing tag
func TestUserTagService_GetOrCreateTag_Existing(t *testing.T) {
	mockTagRepo := new(MockUserTagRepository)

	service := NewUserTagService(mockTagRepo)

	ctx := context.Background()
	userID := "user-123"
	tagName := "existing-tag"

	existingTag := &domain.UserTag{
		ID:     "tag-123",
		UserID: userID,
		Name:   tagName,
	}

	mockTagRepo.On("GetByName", ctx, userID, tagName).Return(existingTag, nil)

	// Execute
	tag, err := service.GetOrCreateTag(ctx, userID, tagName)

	// Assert
	assert.NoError(t, err)
	assert.NotNil(t, tag)
	assert.Equal(t, tagName, tag.Name)
	mockTagRepo.AssertExpectations(t)
}

// TestUserTagService_GetOrCreateTag_New tests creating a new tag when it doesn't exist
func TestUserTagService_GetOrCreateTag_New(t *testing.T) {
	mockTagRepo := new(MockUserTagRepository)

	service := NewUserTagService(mockTagRepo)

	ctx := context.Background()
	userID := "user-123"
	tagName := "new-tag"

	// Tag doesn't exist
	mockTagRepo.On("GetByName", ctx, userID, tagName).Return(nil, domain.ErrTagNotFound)
	// CreateTag path - tags are unlimited
	mockTagRepo.On("Create", ctx, mock.AnythingOfType("*domain.UserTag")).Return(nil)

	// Execute
	tag, err := service.GetOrCreateTag(ctx, userID, tagName)

	// Assert
	assert.NoError(t, err)
	assert.NotNil(t, tag)
	assert.Equal(t, tagName, tag.Name)
	mockTagRepo.AssertExpectations(t)
}
