package operations

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/danielgtaylor/huma/v2/humatest"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/huma/inputs"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockUserTagService is a mock implementation of services.UserTagService
type mockUserTagService struct {
	tags          []*domain.UserTag
	tag           *domain.UserTag
	createErr     error
	getErr        error
	updateErr     error
	deleteErr     error
	getOrCreateTag *domain.UserTag
	getOrCreateErr error
}

func (m *mockUserTagService) GetUserTags(ctx context.Context, userID string) ([]*domain.UserTag, error) {
	if m.getErr != nil {
		return nil, m.getErr
	}
	return m.tags, nil
}

func (m *mockUserTagService) GetTagByID(ctx context.Context, tagID string) (*domain.UserTag, error) {
	if m.getErr != nil {
		return nil, m.getErr
	}
	return m.tag, nil
}

func (m *mockUserTagService) CreateTag(ctx context.Context, userID string, req *domain.CreateUserTagRequest) (*domain.UserTag, error) {
	if m.createErr != nil {
		return nil, m.createErr
	}
	return m.tag, nil
}

func (m *mockUserTagService) UpdateTag(ctx context.Context, userID, tagID string, req *domain.UpdateUserTagRequest) (*domain.UserTag, error) {
	if m.updateErr != nil {
		return nil, m.updateErr
	}
	return m.tag, nil
}

func (m *mockUserTagService) DeleteTag(ctx context.Context, userID, tagID string) error {
	return m.deleteErr
}

func (m *mockUserTagService) GetOrCreateTag(ctx context.Context, userID, tagName string) (*domain.UserTag, error) {
	if m.getOrCreateErr != nil {
		return nil, m.getOrCreateErr
	}
	return m.getOrCreateTag, nil
}

// Helper to create a string pointer
func strPtr(s string) *string {
	return &s
}

// Helper to create a test tag
func createTestTag(userID, name, color string) *domain.UserTag {
	now := time.Now()
	return &domain.UserTag{
		ID:        "tag-123",
		UserID:    userID,
		Name:      name,
		Color:     strPtr(color),
		CreatedAt: now,
		UpdatedAt: now,
	}
}

// TestGetUserTags_Success tests successful retrieval of user tags.
func TestGetUserTags_Success(t *testing.T) {
	// Create mock service with test data
	mockService := &mockUserTagService{
		tags: []*domain.UserTag{
			createTestTag("user-123", "Groceries", "#FF5733"),
			createTestTag("user-123", "Entertainment", "#33FF57"),
		},
	}

	// Create handler
	handler := NewTagHandler(mockService)

	// Call handler directly (bypassing middleware)
	input := &GetUserTagsInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-123"},
	}

	output, err := handler.GetUserTags(context.Background(), input)

	// Assert
	require.NoError(t, err)
	require.NotNil(t, output)
	assert.Len(t, output.Body, 2)
	assert.Equal(t, "Groceries", output.Body[0].Name)
	assert.Equal(t, "Entertainment", output.Body[1].Name)
}

// TestGetUserTags_EmptyList tests that empty array is returned instead of null.
func TestGetUserTags_EmptyList(t *testing.T) {
	mockService := &mockUserTagService{
		tags: nil, // No tags
	}

	handler := NewTagHandler(mockService)

	input := &GetUserTagsInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-123"},
	}

	output, err := handler.GetUserTags(context.Background(), input)

	require.NoError(t, err)
	require.NotNil(t, output)
	assert.NotNil(t, output.Body, "Should return empty array, not nil")
	assert.Len(t, output.Body, 0)
}

// TestGetTagByID_Success tests successful retrieval of a tag by ID.
func TestGetTagByID_Success(t *testing.T) {
	userID := "user-123"
	mockService := &mockUserTagService{
		tag: createTestTag(userID, "Groceries", "#FF5733"),
	}

	handler := NewTagHandler(mockService)

	input := &GetTagByIDInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: userID},
		ID:                 "tag-123",
	}

	output, err := handler.GetTagByID(context.Background(), input)

	require.NoError(t, err)
	require.NotNil(t, output)
	assert.Equal(t, "Groceries", output.Body.Name)
	assert.Equal(t, "#FF5733", *output.Body.Color)
}

// TestGetTagByID_NotFound tests 404 when tag doesn't exist.
func TestGetTagByID_NotFound(t *testing.T) {
	mockService := &mockUserTagService{
		getErr: domain.ErrTagNotFound,
	}

	handler := NewTagHandler(mockService)

	input := &GetTagByIDInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-123"},
		ID:                 "nonexistent-tag",
	}

	output, err := handler.GetTagByID(context.Background(), input)

	assert.Nil(t, output)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

// TestGetTagByID_WrongOwner tests 404 when user doesn't own the tag.
func TestGetTagByID_WrongOwner(t *testing.T) {
	mockService := &mockUserTagService{
		tag: createTestTag("other-user", "Groceries", "#FF5733"), // Different owner
	}

	handler := NewTagHandler(mockService)

	input := &GetTagByIDInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-123"}, // Different user
		ID:                 "tag-123",
	}

	output, err := handler.GetTagByID(context.Background(), input)

	assert.Nil(t, output)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

// TestCreateTag_Success tests successful tag creation.
func TestCreateTag_Success(t *testing.T) {
	userID := "user-123"
	mockService := &mockUserTagService{
		tag: createTestTag(userID, "New Tag", "#AABBCC"),
	}

	handler := NewTagHandler(mockService)

	input := &CreateTagInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: userID},
		Body: domain.CreateUserTagRequest{
			Name:  "New Tag",
			Color: strPtr("#AABBCC"),
		},
	}

	output, err := handler.CreateTag(context.Background(), input)

	require.NoError(t, err)
	require.NotNil(t, output)
	assert.Equal(t, "New Tag", output.Body.Name)
	assert.Equal(t, "#AABBCC", *output.Body.Color)
}

// TestCreateTag_LimitExceeded tests 403 when tag limit is exceeded.
func TestCreateTag_LimitExceeded(t *testing.T) {
	mockService := &mockUserTagService{
		createErr: domain.ErrTagLimitExceeded,
	}

	handler := NewTagHandler(mockService)

	input := &CreateTagInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-123"},
		Body: domain.CreateUserTagRequest{
			Name:  "New Tag",
			Color: strPtr("#AABBCC"),
		},
	}

	output, err := handler.CreateTag(context.Background(), input)

	assert.Nil(t, output)
	require.Error(t, err)
	// Error should indicate forbidden (403)
}

// TestCreateTag_DuplicateName tests 409 when tag name already exists.
func TestCreateTag_DuplicateName(t *testing.T) {
	mockService := &mockUserTagService{
		createErr: domain.ErrTagAlreadyExists,
	}

	handler := NewTagHandler(mockService)

	input := &CreateTagInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-123"},
		Body: domain.CreateUserTagRequest{
			Name:  "Existing Tag",
			Color: strPtr("#AABBCC"),
		},
	}

	output, err := handler.CreateTag(context.Background(), input)

	assert.Nil(t, output)
	require.Error(t, err)
	// Error should indicate conflict (409)
}

// TestCreateTag_InvalidName tests 400 when tag name is invalid.
func TestCreateTag_InvalidName(t *testing.T) {
	mockService := &mockUserTagService{
		createErr: domain.ErrInvalidTagName,
	}

	handler := NewTagHandler(mockService)

	input := &CreateTagInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-123"},
		Body: domain.CreateUserTagRequest{
			Name:  "",
			Color: strPtr("#AABBCC"),
		},
	}

	output, err := handler.CreateTag(context.Background(), input)

	assert.Nil(t, output)
	require.Error(t, err)
	// Error should indicate bad request (400)
}

// TestUpdateTag_Success tests successful tag update.
func TestUpdateTag_Success(t *testing.T) {
	userID := "user-123"
	updatedTag := createTestTag(userID, "Updated Name", "#NEWCLR")
	mockService := &mockUserTagService{
		tag: updatedTag,
	}

	handler := NewTagHandler(mockService)

	input := &UpdateTagInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: userID},
		ID:                 "tag-123",
		Body: domain.UpdateUserTagRequest{
			Name:  strPtr("Updated Name"),
			Color: strPtr("#NEWCLR"),
		},
	}

	output, err := handler.UpdateTag(context.Background(), input)

	require.NoError(t, err)
	require.NotNil(t, output)
	assert.Equal(t, "Updated Name", output.Body.Name)
	assert.Equal(t, "#NEWCLR", *output.Body.Color)
}

// TestUpdateTag_NotFound tests 404 when updating nonexistent tag.
func TestUpdateTag_NotFound(t *testing.T) {
	mockService := &mockUserTagService{
		updateErr: domain.ErrTagNotFound,
	}

	handler := NewTagHandler(mockService)

	input := &UpdateTagInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-123"},
		ID:                 "nonexistent-tag",
		Body: domain.UpdateUserTagRequest{
			Name: strPtr("Updated Name"),
		},
	}

	output, err := handler.UpdateTag(context.Background(), input)

	assert.Nil(t, output)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

// TestDeleteTag_Success tests successful tag deletion.
func TestDeleteTag_Success(t *testing.T) {
	mockService := &mockUserTagService{
		deleteErr: nil,
	}

	handler := NewTagHandler(mockService)

	input := &DeleteTagInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-123"},
		ID:                 "tag-123",
	}

	output, err := handler.DeleteTag(context.Background(), input)

	require.NoError(t, err)
	assert.Nil(t, output) // DeleteTag returns nil, nil on success
}

// TestDeleteTag_NotFound tests 404 when deleting nonexistent tag.
func TestDeleteTag_NotFound(t *testing.T) {
	mockService := &mockUserTagService{
		deleteErr: domain.ErrTagNotFound,
	}

	handler := NewTagHandler(mockService)

	input := &DeleteTagInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-123"},
		ID:                 "nonexistent-tag",
	}

	output, err := handler.DeleteTag(context.Background(), input)

	assert.Nil(t, output)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

// TestTagsRouteRegistration tests that routes are properly registered.
func TestTagsRouteRegistration(t *testing.T) {
	mockService := &mockUserTagService{
		tags: []*domain.UserTag{},
	}

	_, api := humatest.New(t)
	handler := NewTagHandler(mockService)
	RegisterTagRoutes(api, handler)

	// Test GET /api/v1/tags (without auth, should fail with 401)
	resp := api.Get("/api/v1/tags")
	// Should return 401 because AuthenticatedInput requires user_id in context
	assert.Equal(t, http.StatusUnauthorized, resp.Code)

	// Verify the response is a proper error (check raw body contains expected message)
	body := resp.Body.String()
	assert.Contains(t, body, "User not authenticated")
}
