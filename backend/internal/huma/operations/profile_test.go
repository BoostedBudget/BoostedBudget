package operations

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/danielgtaylor/huma/v2/humatest"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/huma/inputs"
	"github.com/maxnussbaum/BoostedBudget/internal/queue"
	"github.com/maxnussbaum/BoostedBudget/internal/repository"
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

func (m *mockProfileService) SetJobQueue(jq queue.JobQueue) {}

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

// TestUpdateProfile_InvalidTimezone_Returns422 tests that an invalid timezone error returns HTTP 422.
func TestUpdateProfile_InvalidTimezone_Returns422(t *testing.T) {
	mockService := &mockProfileService{
		updateErr: fmt.Errorf("%w: Not/A/Timezone", services.ErrInvalidTimezone),
	}

	handler := NewProfileHandler(mockService)

	tz := "Not/A/Timezone"
	input := &UpdateProfileInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-123"},
	}
	input.Body = domain.UpdateProfileRequest{
		Timezone: &tz,
	}

	output, err := handler.UpdateProfile(context.Background(), input)

	assert.Nil(t, output)
	require.Error(t, err)
	assertHTTPStatus(t, err, http.StatusUnprocessableEntity)
}

// TestUpdateProfile_InvalidTimezoneSource_Returns422 tests that an invalid timezone_source error returns HTTP 422.
func TestUpdateProfile_InvalidTimezoneSource_Returns422(t *testing.T) {
	mockService := &mockProfileService{
		updateErr: fmt.Errorf("%w: bogus", services.ErrInvalidTimezoneSource),
	}

	handler := NewProfileHandler(mockService)

	src := "bogus"
	input := &UpdateProfileInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-123"},
	}
	input.Body = domain.UpdateProfileRequest{
		TimezoneSource: &src,
	}

	output, err := handler.UpdateProfile(context.Background(), input)

	assert.Nil(t, output)
	require.Error(t, err)
	assertHTTPStatus(t, err, http.StatusUnprocessableEntity)
}

// TestGetProfile_Comped_SetsEffectiveStatusIsCompedAndCompTier tests that a user with an
// active manual tier override (Issue #1413) gets effective_status forced to "active",
// is_comped=true, and comp_tier populated from the raw override value.
func TestGetProfile_Comped_SetsEffectiveStatusIsCompedAndCompTier(t *testing.T) {
	user := createTestUser()
	user.SubscriptionStatus = "inactive"
	override := "household_2"
	endsAt := time.Now().Add(30 * 24 * time.Hour)
	user.ManualTierOverride = &override
	user.ManualTierEndsAt = &endsAt

	mockService := &mockProfileService{user: user}
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
	assert.Equal(t, "active", output.Body.EffectiveStatus)
	assert.True(t, output.Body.IsComped)
	assert.Equal(t, "household_2", output.Body.CompTier)
	require.NotNil(t, output.Body.CompEndsAt)
	assert.WithinDuration(t, endsAt, *output.Body.CompEndsAt, time.Second)
	// The raw field keeps its exact current meaning — admin UI and billing pages read it.
	assert.Equal(t, "inactive", output.Body.SubscriptionStatus)
}

// TestGetProfile_CompedPermanent_CompEndsAtOmitted tests that a comp with no end date
// (permanent comp) serializes with comp_ends_at absent from the JSON, not zero-valued.
func TestGetProfile_CompedPermanent_CompEndsAtOmitted(t *testing.T) {
	user := createTestUser()
	user.SubscriptionStatus = "inactive"
	override := "household_2"
	user.ManualTierOverride = &override
	user.ManualTierEndsAt = nil // permanent comp

	mockService := &mockProfileService{user: user}
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
	assert.True(t, output.Body.IsComped)
	assert.Nil(t, output.Body.CompEndsAt)

	raw, err := json.Marshal(output.Body)
	require.NoError(t, err)
	assert.NotContains(t, string(raw), "comp_ends_at", "permanent comp must omit comp_ends_at entirely")
}

// TestGetProfile_NotComped_EffectiveStatusEqualsRawStatus tests that a user without any
// manual override gets effective_status equal to the raw subscription_status, and that
// subscription_status itself is unchanged.
func TestGetProfile_NotComped_EffectiveStatusEqualsRawStatus(t *testing.T) {
	user := createTestUser()
	user.SubscriptionStatus = "past_due"
	user.ManualTierOverride = nil
	user.ManualTierEndsAt = nil

	mockService := &mockProfileService{user: user}
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
	assert.Equal(t, "past_due", output.Body.SubscriptionStatus)
	assert.Equal(t, "past_due", output.Body.EffectiveStatus)
	assert.False(t, output.Body.IsComped)
	assert.Empty(t, output.Body.CompTier)
	assert.Nil(t, output.Body.CompEndsAt)

	// is_comped has no omitempty — the frontend must always see the key, even when false.
	raw, err := json.Marshal(output.Body)
	require.NoError(t, err)
	assert.Contains(t, string(raw), `"is_comped":false`)
}

// TestGetProfile_ExpiredOverride_NotComped tests that an expired manual tier override
// (ManualTierEndsAt in the past) yields is_comped=false with comp_tier/comp_ends_at unset,
// even though ManualTierOverride is still populated on the row.
func TestGetProfile_ExpiredOverride_NotComped(t *testing.T) {
	user := createTestUser()
	user.SubscriptionStatus = "inactive"
	override := "household_2"
	expiredAt := time.Now().Add(-24 * time.Hour)
	user.ManualTierOverride = &override
	user.ManualTierEndsAt = &expiredAt

	mockService := &mockProfileService{user: user}
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
	assert.False(t, output.Body.IsComped)
	assert.Empty(t, output.Body.CompTier)
	assert.Nil(t, output.Body.CompEndsAt)
	assert.Equal(t, "inactive", output.Body.EffectiveStatus)
}

// TestUpdateProfile_DuplicateEmail_Returns409 tests that a duplicate email error returns HTTP 409.
func TestUpdateProfile_DuplicateEmail_Returns409(t *testing.T) {
	mockService := &mockProfileService{
		updateErr: repository.ErrDuplicateEmail,
	}

	handler := NewProfileHandler(mockService)

	newEmail := "taken@example.com"
	input := &UpdateProfileInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-123"},
	}
	input.Body = domain.UpdateProfileRequest{
		Email: &newEmail,
	}

	output, err := handler.UpdateProfile(context.Background(), input)

	assert.Nil(t, output)
	require.Error(t, err)
	assertHTTPStatus(t, err, http.StatusConflict)
}
