package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

// MockPrivacyService is a mock implementation of PrivacyService
type MockPrivacyService struct {
	mock.Mock
}

func (m *MockPrivacyService) GetPrivacySettings(ctx context.Context, userID string) (*domain.PrivacySettings, error) {
	args := m.Called(ctx, userID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*domain.PrivacySettings), args.Error(1)
}

func (m *MockPrivacyService) UpdatePrivacySettings(ctx context.Context, userID string, req *domain.UpdatePrivacySettingsRequest) (*domain.PrivacySettings, error) {
	args := m.Called(ctx, userID, req)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*domain.PrivacySettings), args.Error(1)
}

// TestGetPrivacySettings_Success tests successful retrieval of privacy settings
func TestGetPrivacySettings_Success(t *testing.T) {
	// Setup
	mockPrivacyService := new(MockPrivacyService)
	mockProfileService := new(MockProfileService)
	handler := NewPrivacyHandler(mockPrivacyService, mockProfileService)

	auth0UserID := "auth0|507f1f77bcf86cd799439011"
	email := "test@example.com"
	settingsID := uuid.New().String()
	now := time.Now()

	expectedSettings := &domain.PrivacySettings{
		ID:                     settingsID,
		UserID:                 "user-uuid-from-profile",
		GDPRNecessaryConsent:   true,
		GDPRAnalyticsConsent:   true,
		GDPRAdvertisingConsent: false,
		GDPRConsentTimestamp:   now,
		MarketingEmailsEnabled: false,
		ProductUpdatesEnabled:  true,
		SecurityAlertsEnabled:  true,
		DataSharingEnabled:     false,
		CreatedAt:              now,
		UpdatedAt:              now,
	}

	// Mock expectations - service uses the internal user UUID, not auth0 ID
	mockPrivacyService.On("GetPrivacySettings", mock.Anything, "user-uuid-from-profile").Return(expectedSettings, nil)

	// Setup context
	c, rec := setupTestContext("GET", "/user/privacy", "")
	setAuthContext(c, auth0UserID, email)
	// Simulate profile lookup that would happen in real middleware/handler
	c.Set("user_uuid", "user-uuid-from-profile")

	// Execute
	err := handler.GetPrivacySettings(c)

	// Assert
	assert.NoError(t, err)
	assert.Equal(t, http.StatusOK, rec.Code)

	var response domain.PrivacySettings
	json.Unmarshal(rec.Body.Bytes(), &response)
	assert.Equal(t, settingsID, response.ID)
	assert.True(t, response.GDPRNecessaryConsent)
	assert.True(t, response.GDPRAnalyticsConsent)
	mockPrivacyService.AssertExpectations(t)
}

// TestGetPrivacySettings_Unauthorized tests unauthorized access
func TestGetPrivacySettings_Unauthorized(t *testing.T) {
	// Setup
	mockPrivacyService := new(MockPrivacyService)
	mockProfileService := new(MockProfileService)
	handler := NewPrivacyHandler(mockPrivacyService, mockProfileService)

	// Setup context WITHOUT auth (no user_id set)
	c, _ := setupTestContext("GET", "/user/privacy", "")
	// Don't call setAuthContext - simulates missing auth

	// Execute
	err := handler.GetPrivacySettings(c)

	// Assert
	assert.Error(t, err)
	httpErr, ok := err.(*echo.HTTPError)
	assert.True(t, ok)
	assert.Equal(t, http.StatusUnauthorized, httpErr.Code)
	mockPrivacyService.AssertNotCalled(t, "GetPrivacySettings")
}

// TestUpdatePrivacySettings_Success tests successful update
func TestUpdatePrivacySettings_Success(t *testing.T) {
	// Setup
	mockPrivacyService := new(MockPrivacyService)
	mockProfileService := new(MockProfileService)
	handler := NewPrivacyHandler(mockPrivacyService, mockProfileService)

	auth0UserID := "auth0|507f1f77bcf86cd799439011"
	email := "test@example.com"
	now := time.Now()

	requestBody := `{
		"gdpr_analytics_consent": true,
		"gdpr_advertising_consent": true,
		"marketing_emails_enabled": true
	}`

	updatedSettings := &domain.PrivacySettings{
		ID:                     uuid.New().String(),
		UserID:                 "user-uuid-from-profile",
		GDPRNecessaryConsent:   true,
		GDPRAnalyticsConsent:   true,
		GDPRAdvertisingConsent: true,
		GDPRConsentTimestamp:   now,
		MarketingEmailsEnabled: true,
		ProductUpdatesEnabled:  true,
		SecurityAlertsEnabled:  true,
		DataSharingEnabled:     false,
		CreatedAt:              now,
		UpdatedAt:              now,
	}

	// Mock expectations
	mockPrivacyService.On("UpdatePrivacySettings", mock.Anything, "user-uuid-from-profile", mock.AnythingOfType("*domain.UpdatePrivacySettingsRequest")).Return(updatedSettings, nil)

	// Setup context
	c, rec := setupTestContext("PUT", "/user/privacy", requestBody)
	setAuthContext(c, auth0UserID, email)
	c.Set("user_uuid", "user-uuid-from-profile")

	// Execute
	err := handler.UpdatePrivacySettings(c)

	// Assert
	assert.NoError(t, err)
	assert.Equal(t, http.StatusOK, rec.Code)

	var response domain.PrivacySettings
	json.Unmarshal(rec.Body.Bytes(), &response)
	assert.True(t, response.GDPRAnalyticsConsent)
	assert.True(t, response.GDPRAdvertisingConsent)
	assert.True(t, response.MarketingEmailsEnabled)
	mockPrivacyService.AssertExpectations(t)
}

// TestUpdatePrivacySettings_BadRequest tests invalid request body
func TestUpdatePrivacySettings_BadRequest(t *testing.T) {
	// Setup
	mockPrivacyService := new(MockPrivacyService)
	mockProfileService := new(MockProfileService)
	handler := NewPrivacyHandler(mockPrivacyService, mockProfileService)

	auth0UserID := "auth0|507f1f77bcf86cd799439011"
	email := "test@example.com"

	invalidRequestBody := `{invalid json}`

	// Setup context
	c, _ := setupTestContext("PUT", "/user/privacy", invalidRequestBody)
	setAuthContext(c, auth0UserID, email)
	c.Set("user_uuid", "user-uuid-from-profile")

	// Execute
	err := handler.UpdatePrivacySettings(c)

	// Assert
	assert.Error(t, err)
	httpErr, ok := err.(*echo.HTTPError)
	assert.True(t, ok)
	assert.Equal(t, http.StatusBadRequest, httpErr.Code)
	mockPrivacyService.AssertNotCalled(t, "UpdatePrivacySettings")
}

// TestUpdatePrivacySettings_Unauthorized tests unauthorized update attempt
func TestUpdatePrivacySettings_Unauthorized(t *testing.T) {
	// Setup
	mockPrivacyService := new(MockPrivacyService)
	mockProfileService := new(MockProfileService)
	handler := NewPrivacyHandler(mockPrivacyService, mockProfileService)

	requestBody := `{"gdpr_analytics_consent": true}`

	// Setup context WITHOUT auth
	c, _ := setupTestContext("PUT", "/user/privacy", requestBody)
	// Don't call setAuthContext - simulates missing auth

	// Execute
	err := handler.UpdatePrivacySettings(c)

	// Assert
	assert.Error(t, err)
	httpErr, ok := err.(*echo.HTTPError)
	assert.True(t, ok)
	assert.Equal(t, http.StatusUnauthorized, httpErr.Code)
	mockPrivacyService.AssertNotCalled(t, "UpdatePrivacySettings")
}

// TestUpdatePrivacySettings_ValidationError tests GDPR validation failure
func TestUpdatePrivacySettings_ValidationError(t *testing.T) {
	// Setup
	mockPrivacyService := new(MockPrivacyService)
	mockProfileService := new(MockProfileService)
	handler := NewPrivacyHandler(mockPrivacyService, mockProfileService)

	auth0UserID := "auth0|507f1f77bcf86cd799439011"
	email := "test@example.com"

	// Attempt to disable necessary consent (GDPR violation)
	invalidRequestBody := `{"gdpr_necessary_consent": false}`

	// Mock service to return validation error
	validationErr := errors.New("gdpr_necessary_consent cannot be disabled - required for app functionality")
	mockPrivacyService.On("UpdatePrivacySettings", mock.Anything, "user-uuid-from-profile", mock.AnythingOfType("*domain.UpdatePrivacySettingsRequest")).Return(nil, validationErr)

	// Setup context
	c, _ := setupTestContext("PUT", "/user/privacy", invalidRequestBody)
	setAuthContext(c, auth0UserID, email)
	c.Set("user_uuid", "user-uuid-from-profile")

	// Execute
	err := handler.UpdatePrivacySettings(c)

	// Assert
	assert.Error(t, err)
	httpErr, ok := err.(*echo.HTTPError)
	assert.True(t, ok)
	assert.Equal(t, http.StatusBadRequest, httpErr.Code)
	assert.Contains(t, httpErr.Message, "necessary_consent")
	mockPrivacyService.AssertExpectations(t)
}

// TestUpdatePrivacySettings_ServiceError tests generic service error handling
func TestUpdatePrivacySettings_ServiceError(t *testing.T) {
	// Setup
	mockPrivacyService := new(MockPrivacyService)
	mockProfileService := new(MockProfileService)
	handler := NewPrivacyHandler(mockPrivacyService, mockProfileService)

	auth0UserID := "auth0|507f1f77bcf86cd799439011"
	email := "test@example.com"

	requestBody := `{"gdpr_analytics_consent": true}`

	// Mock service to return generic error
	serviceErr := errors.New("database connection failed")
	mockPrivacyService.On("UpdatePrivacySettings", mock.Anything, "user-uuid-from-profile", mock.AnythingOfType("*domain.UpdatePrivacySettingsRequest")).Return(nil, serviceErr)

	// Setup context
	c, _ := setupTestContext("PUT", "/user/privacy", requestBody)
	setAuthContext(c, auth0UserID, email)
	c.Set("user_uuid", "user-uuid-from-profile")

	// Execute
	err := handler.UpdatePrivacySettings(c)

	// Assert
	assert.Error(t, err)
	httpErr, ok := err.(*echo.HTTPError)
	assert.True(t, ok)
	assert.Equal(t, http.StatusInternalServerError, httpErr.Code)
	mockPrivacyService.AssertExpectations(t)
}

// TestGetPrivacySettings_ProfileLookup tests the fallback path when user_uuid not in context
func TestGetPrivacySettings_ProfileLookup(t *testing.T) {
	// Setup
	mockPrivacyService := new(MockPrivacyService)
	mockProfileService := new(MockProfileService)
	handler := NewPrivacyHandler(mockPrivacyService, mockProfileService)

	auth0UserID := "auth0|507f1f77bcf86cd799439011"
	email := "test@example.com"
	userUUID := "user-uuid-from-profile-lookup"
	now := time.Now()

	expectedProfile := &domain.User{
		ID:    userUUID,
		Email: email,
	}

	expectedSettings := &domain.PrivacySettings{
		ID:                     uuid.New().String(),
		UserID:                 userUUID,
		GDPRNecessaryConsent:   true,
		GDPRAnalyticsConsent:   false,
		GDPRAdvertisingConsent: false,
		GDPRConsentTimestamp:   now,
		MarketingEmailsEnabled: false,
		ProductUpdatesEnabled:  true,
		SecurityAlertsEnabled:  true,
		DataSharingEnabled:     false,
		CreatedAt:              now,
		UpdatedAt:              now,
	}

	// Mock expectations - profile lookup, then privacy settings
	mockProfileService.On("GetProfile", mock.Anything, auth0UserID, email).Return(expectedProfile, nil)
	mockPrivacyService.On("GetPrivacySettings", mock.Anything, userUUID).Return(expectedSettings, nil)

	// Setup context WITH auth but WITHOUT user_uuid (forces profile lookup)
	c, rec := setupTestContext("GET", "/user/privacy", "")
	setAuthContext(c, auth0UserID, email)
	// DON'T set user_uuid - this forces the fallback path

	// Execute
	err := handler.GetPrivacySettings(c)

	// Assert
	assert.NoError(t, err)
	assert.Equal(t, http.StatusOK, rec.Code)

	var response domain.PrivacySettings
	json.Unmarshal(rec.Body.Bytes(), &response)
	assert.Equal(t, userUUID, response.UserID)
	assert.True(t, response.GDPRNecessaryConsent)
	mockProfileService.AssertExpectations(t)
	mockPrivacyService.AssertExpectations(t)
}

// TestGetPrivacySettings_ProfileServiceError tests error handling when profile lookup fails
func TestGetPrivacySettings_ProfileServiceError(t *testing.T) {
	// Setup
	mockPrivacyService := new(MockPrivacyService)
	mockProfileService := new(MockProfileService)
	handler := NewPrivacyHandler(mockPrivacyService, mockProfileService)

	auth0UserID := "auth0|507f1f77bcf86cd799439011"
	email := "test@example.com"

	// Mock profile service to return error
	profileErr := errors.New("profile not found")
	mockProfileService.On("GetProfile", mock.Anything, auth0UserID, email).Return(nil, profileErr)

	// Setup context WITH auth but WITHOUT user_uuid
	c, _ := setupTestContext("GET", "/user/privacy", "")
	setAuthContext(c, auth0UserID, email)

	// Execute
	err := handler.GetPrivacySettings(c)

	// Assert
	assert.Error(t, err)
	httpErr, ok := err.(*echo.HTTPError)
	assert.True(t, ok)
	assert.Equal(t, http.StatusInternalServerError, httpErr.Code)
	assert.Contains(t, httpErr.Message, "Failed to retrieve user profile")
	mockProfileService.AssertExpectations(t)
	mockPrivacyService.AssertNotCalled(t, "GetPrivacySettings")
}

// TestGetPrivacySettings_ServiceError tests error handling when privacy service fails
func TestGetPrivacySettings_ServiceError(t *testing.T) {
	// Setup
	mockPrivacyService := new(MockPrivacyService)
	mockProfileService := new(MockProfileService)
	handler := NewPrivacyHandler(mockPrivacyService, mockProfileService)

	auth0UserID := "auth0|507f1f77bcf86cd799439011"
	email := "test@example.com"
	userUUID := "user-uuid-from-profile"

	// Mock privacy service to return error
	privacyErr := errors.New("database connection failed")
	mockPrivacyService.On("GetPrivacySettings", mock.Anything, userUUID).Return(nil, privacyErr)

	// Setup context
	c, _ := setupTestContext("GET", "/user/privacy", "")
	setAuthContext(c, auth0UserID, email)
	c.Set("user_uuid", userUUID)

	// Execute
	err := handler.GetPrivacySettings(c)

	// Assert
	assert.Error(t, err)
	httpErr, ok := err.(*echo.HTTPError)
	assert.True(t, ok)
	assert.Equal(t, http.StatusInternalServerError, httpErr.Code)
	assert.Contains(t, httpErr.Message, "Failed to retrieve privacy settings")
	mockPrivacyService.AssertExpectations(t)
}

// TestUpdatePrivacySettings_ProfileLookup tests the fallback path when user_uuid not in context
func TestUpdatePrivacySettings_ProfileLookup(t *testing.T) {
	// Setup
	mockPrivacyService := new(MockPrivacyService)
	mockProfileService := new(MockProfileService)
	handler := NewPrivacyHandler(mockPrivacyService, mockProfileService)

	auth0UserID := "auth0|507f1f77bcf86cd799439011"
	email := "test@example.com"
	userUUID := "user-uuid-from-profile-lookup"
	now := time.Now()

	requestBody := `{"gdpr_analytics_consent": true}`

	expectedProfile := &domain.User{
		ID:    userUUID,
		Email: email,
	}

	updatedSettings := &domain.PrivacySettings{
		ID:                     uuid.New().String(),
		UserID:                 userUUID,
		GDPRNecessaryConsent:   true,
		GDPRAnalyticsConsent:   true,
		GDPRAdvertisingConsent: false,
		GDPRConsentTimestamp:   now,
		MarketingEmailsEnabled: false,
		ProductUpdatesEnabled:  true,
		SecurityAlertsEnabled:  true,
		DataSharingEnabled:     false,
		CreatedAt:              now,
		UpdatedAt:              now,
	}

	// Mock expectations - profile lookup, then privacy update
	mockProfileService.On("GetProfile", mock.Anything, auth0UserID, email).Return(expectedProfile, nil)
	mockPrivacyService.On("UpdatePrivacySettings", mock.Anything, userUUID, mock.AnythingOfType("*domain.UpdatePrivacySettingsRequest")).Return(updatedSettings, nil)

	// Setup context WITH auth but WITHOUT user_uuid
	c, rec := setupTestContext("PUT", "/user/privacy", requestBody)
	setAuthContext(c, auth0UserID, email)
	// DON'T set user_uuid - forces fallback path

	// Execute
	err := handler.UpdatePrivacySettings(c)

	// Assert
	assert.NoError(t, err)
	assert.Equal(t, http.StatusOK, rec.Code)

	var response domain.PrivacySettings
	json.Unmarshal(rec.Body.Bytes(), &response)
	assert.Equal(t, userUUID, response.UserID)
	assert.True(t, response.GDPRAnalyticsConsent)
	mockProfileService.AssertExpectations(t)
	mockPrivacyService.AssertExpectations(t)
}

// TestUpdatePrivacySettings_ProfileServiceError tests error handling when profile lookup fails
func TestUpdatePrivacySettings_ProfileServiceError(t *testing.T) {
	// Setup
	mockPrivacyService := new(MockPrivacyService)
	mockProfileService := new(MockProfileService)
	handler := NewPrivacyHandler(mockPrivacyService, mockProfileService)

	auth0UserID := "auth0|507f1f77bcf86cd799439011"
	email := "test@example.com"

	requestBody := `{"gdpr_analytics_consent": true}`

	// Mock profile service to return error
	profileErr := errors.New("profile not found")
	mockProfileService.On("GetProfile", mock.Anything, auth0UserID, email).Return(nil, profileErr)

	// Setup context WITH auth but WITHOUT user_uuid
	c, _ := setupTestContext("PUT", "/user/privacy", requestBody)
	setAuthContext(c, auth0UserID, email)

	// Execute
	err := handler.UpdatePrivacySettings(c)

	// Assert
	assert.Error(t, err)
	httpErr, ok := err.(*echo.HTTPError)
	assert.True(t, ok)
	assert.Equal(t, http.StatusInternalServerError, httpErr.Code)
	assert.Contains(t, httpErr.Message, "Failed to retrieve user profile")
	mockProfileService.AssertExpectations(t)
	mockPrivacyService.AssertNotCalled(t, "UpdatePrivacySettings")
}
