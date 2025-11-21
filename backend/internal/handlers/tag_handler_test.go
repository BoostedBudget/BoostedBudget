package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

// MockUserTagService is a mock implementation of services.UserTagService
type MockUserTagService struct {
	mock.Mock
}

func (m *MockUserTagService) CreateTag(ctx context.Context, userID string, req *domain.CreateUserTagRequest) (*domain.UserTag, error) {
	args := m.Called(ctx, userID, req)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*domain.UserTag), args.Error(1)
}

func (m *MockUserTagService) GetUserTags(ctx context.Context, userID string) ([]*domain.UserTag, error) {
	args := m.Called(ctx, userID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]*domain.UserTag), args.Error(1)
}

func (m *MockUserTagService) GetTagByID(ctx context.Context, tagID string) (*domain.UserTag, error) {
	args := m.Called(ctx, tagID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*domain.UserTag), args.Error(1)
}

func (m *MockUserTagService) UpdateTag(ctx context.Context, userID, tagID string, req *domain.UpdateUserTagRequest) (*domain.UserTag, error) {
	args := m.Called(ctx, userID, tagID, req)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*domain.UserTag), args.Error(1)
}

func (m *MockUserTagService) DeleteTag(ctx context.Context, userID, tagID string) error {
	args := m.Called(ctx, userID, tagID)
	return args.Error(0)
}

func (m *MockUserTagService) GetOrCreateTag(ctx context.Context, userID, tagName string) (*domain.UserTag, error) {
	args := m.Called(ctx, userID, tagName)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*domain.UserTag), args.Error(1)
}

// TestCreateTag_Success tests successful tag creation
func TestCreateTag_Success(t *testing.T) {
	mockService := new(MockUserTagService)
	handler := NewTagHandler(mockService)

	userID := uuid.New().String()
	tagID := uuid.New().String()
	now := time.Now()
	color := "#FF5733"

	expectedTag := &domain.UserTag{
		ID:         tagID,
		UserID:     userID,
		Name:       "business",
		Color:      &color,
		UsageCount: 0,
		CreatedAt:  now,
		UpdatedAt:  now,
	}

	body := `{"name": "business", "color": "#FF5733"}`
	c, rec := setupTestContext(http.MethodPost, "/tags", body)
	setAuthContext(c, userID, "test@example.com")

	mockService.On("CreateTag", mock.Anything, userID, mock.AnythingOfType("*domain.CreateUserTagRequest")).Return(expectedTag, nil)

	err := handler.CreateTag(c)

	assert.NoError(t, err)
	assert.Equal(t, http.StatusCreated, rec.Code)

	var response domain.UserTag
	err = json.Unmarshal(rec.Body.Bytes(), &response)
	assert.NoError(t, err)
	assert.Equal(t, tagID, response.ID)
	assert.Equal(t, "business", response.Name)

	mockService.AssertExpectations(t)
}

// TestCreateTag_LimitExceeded tests tag creation when limit is exceeded
func TestCreateTag_LimitExceeded(t *testing.T) {
	mockService := new(MockUserTagService)
	handler := NewTagHandler(mockService)

	userID := uuid.New().String()
	body := `{"name": "one-more-tag"}`
	c, rec := setupTestContext(http.MethodPost, "/tags", body)
	setAuthContext(c, userID, "test@example.com")

	mockService.On("CreateTag", mock.Anything, userID, mock.AnythingOfType("*domain.CreateUserTagRequest")).Return(nil, domain.ErrTagLimitExceeded)

	err := handler.CreateTag(c)

	assert.Error(t, err)
	httpErr, ok := err.(*echo.HTTPError)
	assert.True(t, ok)
	assert.Equal(t, http.StatusForbidden, httpErr.Code)

	mockService.AssertExpectations(t)
	_ = rec // Keep linter happy
}

// TestCreateTag_AlreadyExists tests tag creation with duplicate name
func TestCreateTag_AlreadyExists(t *testing.T) {
	mockService := new(MockUserTagService)
	handler := NewTagHandler(mockService)

	userID := uuid.New().String()
	body := `{"name": "existing-tag"}`
	c, rec := setupTestContext(http.MethodPost, "/tags", body)
	setAuthContext(c, userID, "test@example.com")

	mockService.On("CreateTag", mock.Anything, userID, mock.AnythingOfType("*domain.CreateUserTagRequest")).Return(nil, domain.ErrTagAlreadyExists)

	err := handler.CreateTag(c)

	assert.Error(t, err)
	httpErr, ok := err.(*echo.HTTPError)
	assert.True(t, ok)
	assert.Equal(t, http.StatusConflict, httpErr.Code)

	mockService.AssertExpectations(t)
	_ = rec
}

// TestCreateTag_Unauthorized tests tag creation without auth
func TestCreateTag_Unauthorized(t *testing.T) {
	mockService := new(MockUserTagService)
	handler := NewTagHandler(mockService)

	body := `{"name": "test-tag"}`
	c, rec := setupTestContext(http.MethodPost, "/tags", body)
	// No auth context set

	err := handler.CreateTag(c)

	assert.Error(t, err)
	httpErr, ok := err.(*echo.HTTPError)
	assert.True(t, ok)
	assert.Equal(t, http.StatusUnauthorized, httpErr.Code)
	_ = rec
}

// TestGetUserTags_Success tests successful retrieval of all user tags
func TestGetUserTags_Success(t *testing.T) {
	mockService := new(MockUserTagService)
	handler := NewTagHandler(mockService)

	userID := uuid.New().String()
	now := time.Now()

	expectedTags := []*domain.UserTag{
		{ID: uuid.New().String(), UserID: userID, Name: "business", UsageCount: 5, CreatedAt: now, UpdatedAt: now},
		{ID: uuid.New().String(), UserID: userID, Name: "personal", UsageCount: 3, CreatedAt: now, UpdatedAt: now},
	}

	c, rec := setupTestContext(http.MethodGet, "/tags", "")
	setAuthContext(c, userID, "test@example.com")

	mockService.On("GetUserTags", mock.Anything, userID).Return(expectedTags, nil)

	err := handler.GetUserTags(c)

	assert.NoError(t, err)
	assert.Equal(t, http.StatusOK, rec.Code)

	var response []*domain.UserTag
	err = json.Unmarshal(rec.Body.Bytes(), &response)
	assert.NoError(t, err)
	assert.Len(t, response, 2)

	mockService.AssertExpectations(t)
}

// TestGetUserTags_Empty tests retrieval when user has no tags
func TestGetUserTags_Empty(t *testing.T) {
	mockService := new(MockUserTagService)
	handler := NewTagHandler(mockService)

	userID := uuid.New().String()

	c, rec := setupTestContext(http.MethodGet, "/tags", "")
	setAuthContext(c, userID, "test@example.com")

	mockService.On("GetUserTags", mock.Anything, userID).Return(nil, nil)

	err := handler.GetUserTags(c)

	assert.NoError(t, err)
	assert.Equal(t, http.StatusOK, rec.Code)

	var response []*domain.UserTag
	err = json.Unmarshal(rec.Body.Bytes(), &response)
	assert.NoError(t, err)
	assert.Len(t, response, 0)

	mockService.AssertExpectations(t)
}

// TestGetTagByID_Success tests successful retrieval of a specific tag
func TestGetTagByID_Success(t *testing.T) {
	mockService := new(MockUserTagService)
	handler := NewTagHandler(mockService)

	userID := uuid.New().String()
	tagID := uuid.New().String()
	now := time.Now()

	expectedTag := &domain.UserTag{
		ID:         tagID,
		UserID:     userID,
		Name:       "business",
		UsageCount: 10,
		CreatedAt:  now,
		UpdatedAt:  now,
	}

	c, rec := setupTestContext(http.MethodGet, "/tags/"+tagID, "")
	setAuthContext(c, userID, "test@example.com")
	c.SetParamNames("id")
	c.SetParamValues(tagID)

	mockService.On("GetTagByID", mock.Anything, tagID).Return(expectedTag, nil)

	err := handler.GetTagByID(c)

	assert.NoError(t, err)
	assert.Equal(t, http.StatusOK, rec.Code)

	var response domain.UserTag
	err = json.Unmarshal(rec.Body.Bytes(), &response)
	assert.NoError(t, err)
	assert.Equal(t, tagID, response.ID)

	mockService.AssertExpectations(t)
}

// TestGetTagByID_NotFound tests retrieval of non-existent tag
func TestGetTagByID_NotFound(t *testing.T) {
	mockService := new(MockUserTagService)
	handler := NewTagHandler(mockService)

	userID := uuid.New().String()
	tagID := uuid.New().String()

	c, rec := setupTestContext(http.MethodGet, "/tags/"+tagID, "")
	setAuthContext(c, userID, "test@example.com")
	c.SetParamNames("id")
	c.SetParamValues(tagID)

	mockService.On("GetTagByID", mock.Anything, tagID).Return(nil, domain.ErrTagNotFound)

	err := handler.GetTagByID(c)

	assert.Error(t, err)
	httpErr, ok := err.(*echo.HTTPError)
	assert.True(t, ok)
	assert.Equal(t, http.StatusNotFound, httpErr.Code)

	mockService.AssertExpectations(t)
	_ = rec
}

// TestGetTagByID_WrongOwner tests retrieval of another user's tag
func TestGetTagByID_WrongOwner(t *testing.T) {
	mockService := new(MockUserTagService)
	handler := NewTagHandler(mockService)

	userID := uuid.New().String()
	otherUserID := uuid.New().String()
	tagID := uuid.New().String()
	now := time.Now()

	tagFromOtherUser := &domain.UserTag{
		ID:         tagID,
		UserID:     otherUserID, // Different user
		Name:       "business",
		UsageCount: 10,
		CreatedAt:  now,
		UpdatedAt:  now,
	}

	c, rec := setupTestContext(http.MethodGet, "/tags/"+tagID, "")
	setAuthContext(c, userID, "test@example.com")
	c.SetParamNames("id")
	c.SetParamValues(tagID)

	mockService.On("GetTagByID", mock.Anything, tagID).Return(tagFromOtherUser, nil)

	err := handler.GetTagByID(c)

	assert.Error(t, err)
	httpErr, ok := err.(*echo.HTTPError)
	assert.True(t, ok)
	assert.Equal(t, http.StatusNotFound, httpErr.Code)

	mockService.AssertExpectations(t)
	_ = rec
}

// TestUpdateTag_Success tests successful tag update
func TestUpdateTag_Success(t *testing.T) {
	mockService := new(MockUserTagService)
	handler := NewTagHandler(mockService)

	userID := uuid.New().String()
	tagID := uuid.New().String()
	now := time.Now()
	newColor := "#0000FF"

	updatedTag := &domain.UserTag{
		ID:         tagID,
		UserID:     userID,
		Name:       "updated-name",
		Color:      &newColor,
		UsageCount: 5,
		CreatedAt:  now,
		UpdatedAt:  now,
	}

	body := `{"name": "updated-name", "color": "#0000FF"}`
	c, rec := setupTestContext(http.MethodPut, "/tags/"+tagID, body)
	setAuthContext(c, userID, "test@example.com")
	c.SetParamNames("id")
	c.SetParamValues(tagID)

	mockService.On("UpdateTag", mock.Anything, userID, tagID, mock.AnythingOfType("*domain.UpdateUserTagRequest")).Return(updatedTag, nil)

	err := handler.UpdateTag(c)

	assert.NoError(t, err)
	assert.Equal(t, http.StatusOK, rec.Code)

	var response domain.UserTag
	err = json.Unmarshal(rec.Body.Bytes(), &response)
	assert.NoError(t, err)
	assert.Equal(t, "updated-name", response.Name)

	mockService.AssertExpectations(t)
}

// TestUpdateTag_NotFound tests update of non-existent tag
func TestUpdateTag_NotFound(t *testing.T) {
	mockService := new(MockUserTagService)
	handler := NewTagHandler(mockService)

	userID := uuid.New().String()
	tagID := uuid.New().String()

	body := `{"name": "updated-name"}`
	c, rec := setupTestContext(http.MethodPut, "/tags/"+tagID, body)
	setAuthContext(c, userID, "test@example.com")
	c.SetParamNames("id")
	c.SetParamValues(tagID)

	mockService.On("UpdateTag", mock.Anything, userID, tagID, mock.AnythingOfType("*domain.UpdateUserTagRequest")).Return(nil, domain.ErrTagNotFound)

	err := handler.UpdateTag(c)

	assert.Error(t, err)
	httpErr, ok := err.(*echo.HTTPError)
	assert.True(t, ok)
	assert.Equal(t, http.StatusNotFound, httpErr.Code)

	mockService.AssertExpectations(t)
	_ = rec
}

// TestDeleteTag_Success tests successful tag deletion
func TestDeleteTag_Success(t *testing.T) {
	mockService := new(MockUserTagService)
	handler := NewTagHandler(mockService)

	userID := uuid.New().String()
	tagID := uuid.New().String()

	c, rec := setupTestContext(http.MethodDelete, "/tags/"+tagID, "")
	setAuthContext(c, userID, "test@example.com")
	c.SetParamNames("id")
	c.SetParamValues(tagID)

	mockService.On("DeleteTag", mock.Anything, userID, tagID).Return(nil)

	err := handler.DeleteTag(c)

	assert.NoError(t, err)
	assert.Equal(t, http.StatusNoContent, rec.Code)

	mockService.AssertExpectations(t)
}

// TestDeleteTag_NotFound tests deletion of non-existent tag
func TestDeleteTag_NotFound(t *testing.T) {
	mockService := new(MockUserTagService)
	handler := NewTagHandler(mockService)

	userID := uuid.New().String()
	tagID := uuid.New().String()

	c, rec := setupTestContext(http.MethodDelete, "/tags/"+tagID, "")
	setAuthContext(c, userID, "test@example.com")
	c.SetParamNames("id")
	c.SetParamValues(tagID)

	mockService.On("DeleteTag", mock.Anything, userID, tagID).Return(domain.ErrTagNotFound)

	err := handler.DeleteTag(c)

	assert.Error(t, err)
	httpErr, ok := err.(*echo.HTTPError)
	assert.True(t, ok)
	assert.Equal(t, http.StatusNotFound, httpErr.Code)

	mockService.AssertExpectations(t)
	_ = rec
}
