package operations

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/danielgtaylor/huma/v2/humatest"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/huma/inputs"
	"github.com/maxnussbaum/BoostedBudget/internal/services"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockProfileService is a mock implementation of services.ProfileService
type mockProfileService struct {
	user      *domain.User
	getErr    error
	updateErr error
}

func (m *mockProfileService) GetProfile(ctx context.Context, userID, email string) (*domain.User, error) {
	if m.getErr != nil {
		return nil, m.getErr
	}
	return m.user, nil
}

func (m *mockProfileService) UpdateProfile(ctx context.Context, userID string, req *domain.UpdateProfileRequest) (*domain.User, error) {
	if m.updateErr != nil {
		return nil, m.updateErr
	}
	return m.user, nil
}

// Helper function to create test user
func createTestUser() *domain.User {
	now := time.Now()
	return &domain.User{
		ID:                 "user-123",
		ClerkUserID:        "clerk_user_123",
		Email:              "test@example.com",
		EmailVerified:      true,
		SubscriptionTier:   "free",
		SubscriptionStatus: "active",
		PrimaryRegion:      "iad",
		IsActive:           true,
		IsDeleted:          false,
		CreatedAt:          now,
		UpdatedAt:          now,
	}
}

// TestGetProfile_Success tests successful profile retrieval.
func TestGetProfile_Success(t *testing.T) {
	mockService := &mockProfileService{
		user: createTestUser(),
	}

	handler := NewProfileHandler(mockService)

	input := &GetProfileInput{
		AuthenticatedWithEmailInput: inputs.AuthenticatedWithEmailInput{
			AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-123"},
			Email:              "test@example.com",
		},
	}

	output, err := handler.GetProfile(context.Background(), input)

	require.NoError(t, err)
	require.NotNil(t, output)
	assert.Equal(t, "user-123", output.Body.ID)
	assert.Equal(t, "test@example.com", output.Body.Email)
}

// TestGetProfile_Error tests error handling for profile retrieval.
func TestGetProfile_Error(t *testing.T) {
	mockService := &mockProfileService{
		getErr: assert.AnError,
	}

	handler := NewProfileHandler(mockService)

	input := &GetProfileInput{
		AuthenticatedWithEmailInput: inputs.AuthenticatedWithEmailInput{
			AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-123"},
			Email:              "test@example.com",
		},
	}

	output, err := handler.GetProfile(context.Background(), input)

	assert.Nil(t, output)
	require.Error(t, err)
}

// TestUpdateProfile_Success tests successful profile update.
func TestUpdateProfile_Success(t *testing.T) {
	mockService := &mockProfileService{
		user: createTestUser(),
	}

	handler := NewProfileHandler(mockService)

	newEmail := "updated@example.com"
	input := &UpdateProfileInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-123"},
	}
	input.Body = domain.UpdateProfileRequest{
		Email: &newEmail,
	}

	output, err := handler.UpdateProfile(context.Background(), input)

	require.NoError(t, err)
	require.NotNil(t, output)
}

// TestUpdateProfile_InvalidEmail tests invalid email error.
func TestUpdateProfile_InvalidEmail(t *testing.T) {
	mockService := &mockProfileService{
		updateErr: services.ErrInvalidEmail,
	}

	handler := NewProfileHandler(mockService)

	input := &UpdateProfileInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-123"},
	}

	output, err := handler.UpdateProfile(context.Background(), input)

	assert.Nil(t, output)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "Invalid email address")
}

// TestUpdateProfile_InvalidRegion tests invalid region error.
func TestUpdateProfile_InvalidRegion(t *testing.T) {
	mockService := &mockProfileService{
		updateErr: services.ErrInvalidRegion,
	}

	handler := NewProfileHandler(mockService)

	input := &UpdateProfileInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-123"},
	}

	output, err := handler.UpdateProfile(context.Background(), input)

	assert.Nil(t, output)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "Invalid primary region")
}

// TestProfileRouteRegistration tests that routes are properly registered.
func TestProfileRouteRegistration(t *testing.T) {
	mockService := &mockProfileService{
		user: createTestUser(),
	}

	_, api := humatest.New(t)
	handler := NewProfileHandler(mockService)
	RegisterProfileRoutes(api, handler)

	// Test GET /api/v1/user/profile (without auth, should fail with 401)
	resp := api.Get("/api/v1/user/profile")
	assert.Equal(t, http.StatusUnauthorized, resp.Code)

	// Verify the response contains expected error message
	body := resp.Body.String()
	assert.Contains(t, body, "User not authenticated")
}

// TestProfileResponseJSON tests JSON serialization of response.
func TestProfileResponseJSON(t *testing.T) {
	mockService := &mockProfileService{}

	_, api := humatest.New(t)
	handler := NewProfileHandler(mockService)
	RegisterProfileRoutes(api, handler)

	resp := api.Get("/api/v1/user/profile")
	assert.Equal(t, http.StatusUnauthorized, resp.Code)

	// Verify error response is valid JSON
	var errorResp map[string]interface{}
	err := json.Unmarshal(resp.Body.Bytes(), &errorResp)
	require.NoError(t, err, "Error response should be valid JSON")
}
