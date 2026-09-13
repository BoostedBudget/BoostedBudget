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

// TestGetUserProfileDescriptionExplainsBestEffortLastLoginUpdate pins the externally published
// contract. GetProfile fetches the user before it attempts UpdateLastLogin, and deliberately
// ignores an update error, so consumers must not mistake the returned timestamp for proof that
// this request persisted a fresh login time.
func TestGetUserProfileDescriptionExplainsBestEffortLastLoginUpdate(t *testing.T) {
	mockService := &mockProfileService{user: createTestUser()}
	_, api := humatest.New(t)
	RegisterProfileRoutes(api, NewProfileHandler(mockService))

	op := api.OpenAPI().Paths["/api/v1/user/profile"].Get
	require.NotNil(t, op)
	assert.Contains(t, op.Description, "best-effort")
	assert.Contains(t, op.Description, "returned last_login_at is the value fetched before that update")
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

// fakeOverrideTierValidator returns a validator func accepting only the given tier names,
// standing in for a real *config.TierConfig-backed check without this package needing to import
// config or call config.LoadTierConfig() (a process-wide sync.Once singleton that must never be
// invoked from a test — see CLAUDE.md). Mirrors repository/profile_repository_test.go's helper
// of the same name, added for the identical reason.
func fakeOverrideTierValidator(validTiers ...string) func(string) bool {
	allowed := make(map[string]bool, len(validTiers))
	for _, t := range validTiers {
		allowed[t] = true
	}
	return func(tier string) bool { return allowed[tier] }
}

// TestGetProfile_MalformedOverride_WithValidator_NotComped is the RED test for Issue #1413
// round-5 (adversarial review finding): a manual_tier_override naming NO configured tier
// ('not_a_tier' — garbage, not a real tier name) must not make the profile response report
// is_comped=true / effective_status="active", even though the raw override is non-expired
// (future-dated). Before this fix, enrichUserProfile called
// ComputeEffectiveStatus()/HasActiveManualOverride() directly with no tier-name validation at
// all — the exact gap GetEffectiveSubscriptionStatusByUserID closed on the access-granting path,
// but this REPRESENTATION path never inherited it, so the frontend could render working write
// controls for a comp the backend would then 403.
func TestGetProfile_MalformedOverride_WithValidator_NotComped(t *testing.T) {
	user := createTestUser()
	user.SubscriptionStatus = "inactive"
	override := "not_a_tier"
	endsAt := time.Now().Add(30 * 24 * time.Hour) // future-dated: not expired
	user.ManualTierOverride = &override
	user.ManualTierEndsAt = &endsAt

	mockService := &mockProfileService{user: user}
	handler := NewProfileHandler(mockService)
	handler.SetOverrideTierValidator(fakeOverrideTierValidator("premium", "household_2", "household_3", "household_4", "household_5"))

	input := &GetProfileInput{
		AuthenticatedWithEmailInput: inputs.AuthenticatedWithEmailInput{
			AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-123"},
			Email:              "test@example.com",
		},
	}

	output, err := handler.GetProfile(context.Background(), input)

	require.NoError(t, err)
	require.NotNil(t, output)
	assert.False(t, output.Body.IsComped, "a manual_tier_override naming no configured tier must never report is_comped=true")
	assert.NotEqual(t, "active", output.Body.EffectiveStatus, "a malformed override must never force effective_status to active")
	assert.Equal(t, "inactive", output.Body.EffectiveStatus, "a rejected override must fall back to the raw subscription_status")
	assert.Empty(t, output.Body.CompTier)
	assert.Nil(t, output.Body.CompEndsAt)
	// Issue #1413 (this task) — the TIER axis of the same rule: a malformed override must not
	// leak into effective_tier either. createTestUser's own fixture sets SubscriptionTier to
	// "free" (see createTestUser above) — this is the exact assertion "Test #21" was missing.
	assert.Equal(t, "free", output.Body.EffectiveTier, "a malformed override must fall back to the raw subscription_tier, never surface the garbage override string")
}

// TestGetProfile_ValidOverride_WithValidator_StillComped is the positive control demanded
// alongside the RED test above: an over-tight validator that denies every comp — even a real,
// correctly-configured one — must not sail through unnoticed. With a validator wired, a
// genuinely valid override tier must still report is_comped=true / effective_status="active".
func TestGetProfile_ValidOverride_WithValidator_StillComped(t *testing.T) {
	user := createTestUser()
	user.SubscriptionStatus = "inactive"
	override := "household_3"
	user.ManualTierOverride = &override
	user.ManualTierEndsAt = nil // permanent comp

	mockService := &mockProfileService{user: user}
	handler := NewProfileHandler(mockService)
	handler.SetOverrideTierValidator(fakeOverrideTierValidator("premium", "household_2", "household_3", "household_4", "household_5"))

	input := &GetProfileInput{
		AuthenticatedWithEmailInput: inputs.AuthenticatedWithEmailInput{
			AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-123"},
			Email:              "test@example.com",
		},
	}

	output, err := handler.GetProfile(context.Background(), input)

	require.NoError(t, err)
	require.NotNil(t, output)
	assert.True(t, output.Body.IsComped, "a validly-configured override tier must still report is_comped=true")
	assert.Equal(t, "active", output.Body.EffectiveStatus)
	assert.Equal(t, "household_3", output.Body.CompTier)
	// Positive control (Issue #1413, this task): a VALID override must still resolve to the
	// override tier on effective_tier too — an over-tight fix that falls every comp back to the
	// raw tier would silently un-comp every legitimately comped user.
	assert.Equal(t, "household_3", output.Body.EffectiveTier)
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
