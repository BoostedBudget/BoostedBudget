package operations

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/humatest"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/huma/inputs"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockPrivacyService is a mock implementation of services.PrivacyService
type mockPrivacyService struct {
	settings  *domain.PrivacySettings
	getErr    error
	updateErr error
}

func (m *mockPrivacyService) GetPrivacySettings(ctx context.Context, userID string) (*domain.PrivacySettings, error) {
	if m.getErr != nil {
		return nil, m.getErr
	}
	return m.settings, nil
}

func (m *mockPrivacyService) UpdatePrivacySettings(ctx context.Context, userID, clerkSubject string, req *domain.UpdatePrivacySettingsRequest) (*domain.PrivacySettings, error) {
	if m.updateErr != nil {
		return nil, m.updateErr
	}
	return m.settings, nil
}

// Helper function to create test privacy settings
func createTestPrivacySettings() *domain.PrivacySettings {
	now := time.Now()
	return &domain.PrivacySettings{
		ID:                     "privacy-123",
		UserID:                 "user-123",
		GDPRNecessaryConsent:   true,
		GDPRAnalyticsConsent:   false,
		GDPRConsentTimestamp:   now,
		MarketingEmailsEnabled: false,
		ProductUpdatesEnabled:  true,
		SecurityAlertsEnabled:  true,
		DataSharingEnabled:     false,
		CreatedAt:              now,
		UpdatedAt:              now,
	}
}

// TestGetPrivacySettings_Success tests successful privacy settings retrieval.
func TestGetPrivacySettings_Success(t *testing.T) {
	mockService := &mockPrivacyService{
		settings: createTestPrivacySettings(),
	}

	handler := NewPrivacyHandler(mockService)

	input := &GetPrivacySettingsInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-123"},
	}

	output, err := handler.GetPrivacySettings(context.Background(), input)

	require.NoError(t, err)
	require.NotNil(t, output)
	assert.Equal(t, "privacy-123", output.Body.ID)
	assert.Equal(t, "user-123", output.Body.UserID)
	assert.True(t, output.Body.GDPRNecessaryConsent)
}

// TestGetPrivacySettings_Error tests error handling for privacy settings retrieval.
func TestGetPrivacySettings_Error(t *testing.T) {
	mockService := &mockPrivacyService{
		getErr: assert.AnError,
	}

	handler := NewPrivacyHandler(mockService)

	input := &GetPrivacySettingsInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-123"},
	}

	output, err := handler.GetPrivacySettings(context.Background(), input)

	assert.Nil(t, output)
	require.Error(t, err)
}

// TestUpdatePrivacySettings_Success tests successful privacy settings update.
func TestUpdatePrivacySettings_Success(t *testing.T) {
	mockService := &mockPrivacyService{
		settings: createTestPrivacySettings(),
	}

	handler := NewPrivacyHandler(mockService)

	analyticsConsent := true
	input := &UpdatePrivacySettingsInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-123"},
	}
	input.Body = domain.UpdatePrivacySettingsRequest{
		GDPRAnalyticsConsent: &analyticsConsent,
	}

	ctx := context.WithValue(context.Background(), inputs.ClerkUserIDKey, "user_clerk_123")
	output, err := handler.UpdatePrivacySettings(ctx, input)

	require.NoError(t, err)
	require.NotNil(t, output)
}

// TestUpdatePrivacySettings_NecessaryConsentError tests GDPR necessary consent error.
func TestUpdatePrivacySettings_NecessaryConsentError(t *testing.T) {
	mockService := &mockPrivacyService{
		updateErr: errors.New("gdpr_necessary_consent cannot be disabled"),
	}

	handler := NewPrivacyHandler(mockService)

	necessaryConsent := false
	input := &UpdatePrivacySettingsInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-123"},
	}
	input.Body = domain.UpdatePrivacySettingsRequest{
		GDPRNecessaryConsent: &necessaryConsent,
	}

	ctx := context.WithValue(context.Background(), inputs.ClerkUserIDKey, "user_clerk_123")
	output, err := handler.UpdatePrivacySettings(ctx, input)

	assert.Nil(t, output)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "necessary_consent")
}

// TestUpdatePrivacySettings_GenericError tests generic error handling.
func TestUpdatePrivacySettings_GenericError(t *testing.T) {
	mockService := &mockPrivacyService{
		updateErr: assert.AnError,
	}

	handler := NewPrivacyHandler(mockService)

	input := &UpdatePrivacySettingsInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-123"},
	}

	ctx := context.WithValue(context.Background(), inputs.ClerkUserIDKey, "user_clerk_123")
	output, err := handler.UpdatePrivacySettings(ctx, input)

	assert.Nil(t, output)
	require.Error(t, err)
}

// TestUpdatePrivacySettings_InteractionCaptureRequiresAnalytics_Returns422 pins the dedicated
// 422 mapping: a merged state that enables interaction capture without analytics consent is a
// semantic refusal (422), not the necessary_consent 400 and not a 500.
func TestUpdatePrivacySettings_InteractionCaptureRequiresAnalytics_Returns422(t *testing.T) {
	mockService := &mockPrivacyService{
		updateErr: domain.ErrInteractionCaptureRequiresAnalytics,
	}

	handler := NewPrivacyHandler(mockService)

	interactionConsent := true
	analyticsConsent := false
	input := &UpdatePrivacySettingsInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-123"},
	}
	input.Body = domain.UpdatePrivacySettingsRequest{
		GDPRInteractionCaptureConsent: &interactionConsent,
		GDPRAnalyticsConsent:          &analyticsConsent,
	}

	ctx := context.WithValue(context.Background(), inputs.ClerkUserIDKey, "user_clerk_123")
	output, err := handler.UpdatePrivacySettings(ctx, input)

	assert.Nil(t, output)
	require.Error(t, err)

	var statusErr huma.StatusError
	require.ErrorAs(t, err, &statusErr)
	assert.Equal(t, http.StatusUnprocessableEntity, statusErr.GetStatus())
	assert.NotEqual(t, http.StatusBadRequest, statusErr.GetStatus())
	assert.NotEqual(t, http.StatusInternalServerError, statusErr.GetStatus())
}

// TestUpdatePrivacySettings_MissingClerkSubject_Returns401 pins the fail-closed subject check: a
// request that reached the handler without a Clerk subject must be refused BEFORE any service
// call, rather than silently skipping the PostHog person-property plumbing.
func TestUpdatePrivacySettings_MissingClerkSubject_Returns401(t *testing.T) {
	mockService := &mockPrivacyService{
		settings: createTestPrivacySettings(),
	}

	handler := NewPrivacyHandler(mockService)

	analyticsConsent := true
	input := &UpdatePrivacySettingsInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-123"},
	}
	input.Body = domain.UpdatePrivacySettingsRequest{
		GDPRAnalyticsConsent: &analyticsConsent,
	}

	output, err := handler.UpdatePrivacySettings(context.Background(), input)

	assert.Nil(t, output)
	require.Error(t, err)

	var statusErr huma.StatusError
	require.ErrorAs(t, err, &statusErr)
	assert.Equal(t, http.StatusUnauthorized, statusErr.GetStatus())
}

// TestPrivacyRouteRegistration tests that routes are properly registered.
func TestPrivacyRouteRegistration(t *testing.T) {
	mockService := &mockPrivacyService{
		settings: createTestPrivacySettings(),
	}

	_, api := humatest.New(t)
	handler := NewPrivacyHandler(mockService)
	RegisterPrivacyRoutes(api, handler)

	// Test GET /api/v1/user/privacy (without auth, should fail with 401)
	resp := api.Get("/api/v1/user/privacy")
	assert.Equal(t, http.StatusUnauthorized, resp.Code)

	// Verify the response contains expected error message
	body := resp.Body.String()
	assert.Contains(t, body, "User not authenticated")
}

// TestPrivacyResponseJSON tests JSON serialization of response.
func TestPrivacyResponseJSON(t *testing.T) {
	mockService := &mockPrivacyService{}

	_, api := humatest.New(t)
	handler := NewPrivacyHandler(mockService)
	RegisterPrivacyRoutes(api, handler)

	resp := api.Get("/api/v1/user/privacy")
	assert.Equal(t, http.StatusUnauthorized, resp.Code)

	// Verify error response is valid JSON
	var errorResp map[string]interface{}
	err := json.Unmarshal(resp.Body.Bytes(), &errorResp)
	require.NoError(t, err, "Error response should be valid JSON")
}
