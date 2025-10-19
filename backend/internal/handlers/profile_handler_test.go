package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/services"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

// MockProfileService is a mock implementation of ProfileService
type MockProfileService struct {
	mock.Mock
}

func (m *MockProfileService) GetProfile(ctx context.Context, auth0UserID string, email string) (*domain.User, error) {
	args := m.Called(ctx, auth0UserID, email)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*domain.User), args.Error(1)
}

func (m *MockProfileService) UpdateProfile(ctx context.Context, auth0UserID string, req *domain.UpdateProfileRequest) (*domain.User, error) {
	args := m.Called(ctx, auth0UserID, req)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*domain.User), args.Error(1)
}

// setupTestContext creates a test Echo context with auth middleware values
func setupTestContext(method, path, body string) (echo.Context, *httptest.ResponseRecorder) {
	e := echo.New()
	var req *http.Request
	if body != "" {
		req = httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	} else {
		req = httptest.NewRequest(method, path, nil)
	}
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	return c, rec
}

// setAuthContext sets Auth0 user ID and email in the context (simulates auth middleware)
func setAuthContext(c echo.Context, auth0UserID, email string) {
	c.Set("user_id", auth0UserID)
	claims := jwt.MapClaims{
		"sub":   auth0UserID,
		"email": email,
	}
	c.Set("claims", claims)
}

// TestGetProfile_Success tests successful profile retrieval
func TestGetProfile_Success(t *testing.T) {
	// Setup
	mockService := new(MockProfileService)
	handler := NewProfileHandler(mockService)

	auth0UserID := "auth0|507f1f77bcf86cd799439011"
	email := "test@example.com"
	userID := uuid.New().String()
	now := time.Now()

	expectedUser := &domain.User{
		ID:                 userID,
		Auth0UserID:        auth0UserID,
		Email:              email,
		EmailVerified:      true,
		SubscriptionTier:   "premium",
		SubscriptionStatus: "active",
		PrimaryRegion:      "iad",
		IsActive:           true,
		IsDeleted:          false,
		CreatedAt:          now,
		UpdatedAt:          now,
		LastLoginAt:        &now,
	}

	c, rec := setupTestContext(http.MethodGet, "/user/profile", "")
	setAuthContext(c, auth0UserID, email)

	// Mock expectations
	mockService.On("GetProfile", mock.Anything, auth0UserID, email).Return(expectedUser, nil)

	// Execute
	err := handler.GetProfile(c)

	// Assert
	assert.NoError(t, err)
	assert.Equal(t, http.StatusOK, rec.Code)

	var response domain.User
	err = json.Unmarshal(rec.Body.Bytes(), &response)
	assert.NoError(t, err)
	assert.Equal(t, userID, response.ID)
	assert.Equal(t, auth0UserID, response.Auth0UserID)
	assert.Equal(t, email, response.Email)
	assert.Equal(t, "premium", response.SubscriptionTier)

	mockService.AssertExpectations(t)
}

// TestGetProfile_FirstLogin tests profile creation on first login
func TestGetProfile_FirstLogin(t *testing.T) {
	// Setup
	mockService := new(MockProfileService)
	handler := NewProfileHandler(mockService)

	auth0UserID := "auth0|newuser123"
	email := "newuser@example.com"
	userID := uuid.New().String()
	now := time.Now()

	newUser := &domain.User{
		ID:                 userID,
		Auth0UserID:        auth0UserID,
		Email:              email,
		EmailVerified:      false,
		SubscriptionTier:   "free",
		SubscriptionStatus: "active",
		PrimaryRegion:      "iad",
		IsActive:           true,
		IsDeleted:          false,
		CreatedAt:          now,
		UpdatedAt:          now,
	}

	c, rec := setupTestContext(http.MethodGet, "/user/profile", "")
	setAuthContext(c, auth0UserID, email)

	// Mock expectations
	mockService.On("GetProfile", mock.Anything, auth0UserID, email).Return(newUser, nil)

	// Execute
	err := handler.GetProfile(c)

	// Assert
	assert.NoError(t, err)
	assert.Equal(t, http.StatusOK, rec.Code)

	var response domain.User
	err = json.Unmarshal(rec.Body.Bytes(), &response)
	assert.NoError(t, err)
	assert.Equal(t, "free", response.SubscriptionTier)
	assert.Equal(t, "iad", response.PrimaryRegion)

	mockService.AssertExpectations(t)
}

// TestGetProfile_MissingAuth tests missing auth context
func TestGetProfile_MissingAuth(t *testing.T) {
	// Setup
	mockService := new(MockProfileService)
	handler := NewProfileHandler(mockService)

	c, rec := setupTestContext(http.MethodGet, "/user/profile", "")
	_ = rec
	// Don't set auth context - simulates missing auth middleware

	// Execute
	err := handler.GetProfile(c)

	// Assert
	assert.Error(t, err)
	httpErr, ok := err.(*echo.HTTPError)
	assert.True(t, ok)
	assert.Equal(t, http.StatusUnauthorized, httpErr.Code)
}

// TestGetProfile_ServiceError tests service layer error handling
func TestGetProfile_ServiceError(t *testing.T) {
	// Setup
	mockService := new(MockProfileService)
	handler := NewProfileHandler(mockService)

	auth0UserID := "auth0|507f1f77bcf86cd799439011"
	email := "test@example.com"

	c, rec := setupTestContext(http.MethodGet, "/user/profile", "")
	_ = rec
	setAuthContext(c, auth0UserID, email)

	// Mock expectations
	mockService.On("GetProfile", mock.Anything, auth0UserID, email).Return(nil, errors.New("database error"))

	// Execute
	err := handler.GetProfile(c)

	// Assert
	assert.Error(t, err)
	httpErr, ok := err.(*echo.HTTPError)
	assert.True(t, ok)
	assert.Equal(t, http.StatusInternalServerError, httpErr.Code)

	mockService.AssertExpectations(t)
}

// TestUpdateProfile_Success tests successful profile update
func TestUpdateProfile_Success(t *testing.T) {
	// Setup
	mockService := new(MockProfileService)
	handler := NewProfileHandler(mockService)

	auth0UserID := "auth0|507f1f77bcf86cd799439011"
	email := "old@example.com"
	newEmail := "new@example.com"
	userID := uuid.New().String()
	now := time.Now()

	requestBody := `{"email":"new@example.com"}`

	updatedUser := &domain.User{
		ID:                 userID,
		Auth0UserID:        auth0UserID,
		Email:              newEmail,
		EmailVerified:      true,
		SubscriptionTier:   "premium",
		SubscriptionStatus: "active",
		PrimaryRegion:      "iad",
		IsActive:           true,
		IsDeleted:          false,
		CreatedAt:          now,
		UpdatedAt:          now,
	}

	c, rec := setupTestContext(http.MethodPut, "/user/profile", requestBody)
	setAuthContext(c, auth0UserID, email)

	// Mock expectations
	mockService.On("UpdateProfile", mock.Anything, auth0UserID, mock.MatchedBy(func(req *domain.UpdateProfileRequest) bool {
		return req.Email != nil && *req.Email == newEmail
	})).Return(updatedUser, nil)

	// Execute
	err := handler.UpdateProfile(c)

	// Assert
	assert.NoError(t, err)
	assert.Equal(t, http.StatusOK, rec.Code)

	var response domain.User
	err = json.Unmarshal(rec.Body.Bytes(), &response)
	assert.NoError(t, err)
	assert.Equal(t, newEmail, response.Email)

	mockService.AssertExpectations(t)
}

// TestUpdateProfile_UpdateRegion tests updating primary region
func TestUpdateProfile_UpdateRegion(t *testing.T) {
	// Setup
	mockService := new(MockProfileService)
	handler := NewProfileHandler(mockService)

	auth0UserID := "auth0|507f1f77bcf86cd799439011"
	email := "test@example.com"
	newRegion := "lhr"
	userID := uuid.New().String()
	now := time.Now()

	requestBody := `{"primary_region":"lhr"}`

	updatedUser := &domain.User{
		ID:                 userID,
		Auth0UserID:        auth0UserID,
		Email:              email,
		SubscriptionTier:   "premium",
		SubscriptionStatus: "active",
		PrimaryRegion:      newRegion,
		IsActive:           true,
		IsDeleted:          false,
		CreatedAt:          now,
		UpdatedAt:          now,
	}

	c, rec := setupTestContext(http.MethodPut, "/user/profile", requestBody)
	setAuthContext(c, auth0UserID, email)

	// Mock expectations
	mockService.On("UpdateProfile", mock.Anything, auth0UserID, mock.MatchedBy(func(req *domain.UpdateProfileRequest) bool {
		return req.PrimaryRegion != nil && *req.PrimaryRegion == newRegion
	})).Return(updatedUser, nil)

	// Execute
	err := handler.UpdateProfile(c)

	// Assert
	assert.NoError(t, err)
	assert.Equal(t, http.StatusOK, rec.Code)

	var response domain.User
	err = json.Unmarshal(rec.Body.Bytes(), &response)
	assert.NoError(t, err)
	assert.Equal(t, newRegion, response.PrimaryRegion)

	mockService.AssertExpectations(t)
}

// TestUpdateProfile_InvalidJSON tests malformed request body
func TestUpdateProfile_InvalidJSON(t *testing.T) {
	// Setup
	mockService := new(MockProfileService)
	handler := NewProfileHandler(mockService)

	auth0UserID := "auth0|507f1f77bcf86cd799439011"
	email := "test@example.com"

	requestBody := `{"email":invalid-json}`

	c, rec := setupTestContext(http.MethodPut, "/user/profile", requestBody)
	_ = rec
	setAuthContext(c, auth0UserID, email)

	// Execute
	err := handler.UpdateProfile(c)

	// Assert
	assert.Error(t, err)
	httpErr, ok := err.(*echo.HTTPError)
	assert.True(t, ok)
	assert.Equal(t, http.StatusBadRequest, httpErr.Code)
}

// TestUpdateProfile_InvalidEmail tests invalid email validation
func TestUpdateProfile_InvalidEmail(t *testing.T) {
	// Setup
	mockService := new(MockProfileService)
	handler := NewProfileHandler(mockService)

	auth0UserID := "auth0|507f1f77bcf86cd799439011"
	email := "test@example.com"

	requestBody := `{"email":"not-an-email"}`

	c, rec := setupTestContext(http.MethodPut, "/user/profile", requestBody)
	_ = rec
	setAuthContext(c, auth0UserID, email)

	// Mock expectations
	mockService.On("UpdateProfile", mock.Anything, auth0UserID, mock.Anything).
		Return(nil, services.ErrInvalidEmail)

	// Execute
	err := handler.UpdateProfile(c)

	// Assert
	assert.Error(t, err)
	httpErr, ok := err.(*echo.HTTPError)
	assert.True(t, ok)
	assert.Equal(t, http.StatusBadRequest, httpErr.Code)

	mockService.AssertExpectations(t)
}

// TestUpdateProfile_InvalidRegion tests invalid region validation
func TestUpdateProfile_InvalidRegion(t *testing.T) {
	// Setup
	mockService := new(MockProfileService)
	handler := NewProfileHandler(mockService)

	auth0UserID := "auth0|507f1f77bcf86cd799439011"
	email := "test@example.com"

	requestBody := `{"primary_region":"invalid-region"}`

	c, rec := setupTestContext(http.MethodPut, "/user/profile", requestBody)
	_ = rec
	setAuthContext(c, auth0UserID, email)

	// Mock expectations
	mockService.On("UpdateProfile", mock.Anything, auth0UserID, mock.Anything).
		Return(nil, services.ErrInvalidRegion)

	// Execute
	err := handler.UpdateProfile(c)

	// Assert
	assert.Error(t, err)
	httpErr, ok := err.(*echo.HTTPError)
	assert.True(t, ok)
	assert.Equal(t, http.StatusBadRequest, httpErr.Code)

	mockService.AssertExpectations(t)
}

// TestUpdateProfile_MissingAuth tests missing auth context
func TestUpdateProfile_MissingAuth(t *testing.T) {
	// Setup
	mockService := new(MockProfileService)
	handler := NewProfileHandler(mockService)

	requestBody := `{"email":"new@example.com"}`

	c, rec := setupTestContext(http.MethodPut, "/user/profile", requestBody)
	_ = rec
	// Don't set auth context

	// Execute
	err := handler.UpdateProfile(c)

	// Assert
	assert.Error(t, err)
	httpErr, ok := err.(*echo.HTTPError)
	assert.True(t, ok)
	assert.Equal(t, http.StatusUnauthorized, httpErr.Code)
}

// TestUpdateProfile_ServiceError tests service layer error handling
func TestUpdateProfile_ServiceError(t *testing.T) {
	// Setup
	mockService := new(MockProfileService)
	handler := NewProfileHandler(mockService)

	auth0UserID := "auth0|507f1f77bcf86cd799439011"
	email := "test@example.com"

	requestBody := `{"email":"new@example.com"}`

	c, rec := setupTestContext(http.MethodPut, "/user/profile", requestBody)
	_ = rec
	setAuthContext(c, auth0UserID, email)

	// Mock expectations
	mockService.On("UpdateProfile", mock.Anything, auth0UserID, mock.Anything).
		Return(nil, errors.New("database error"))

	// Execute
	err := handler.UpdateProfile(c)

	// Assert
	assert.Error(t, err)
	httpErr, ok := err.(*echo.HTTPError)
	assert.True(t, ok)
	assert.Equal(t, http.StatusInternalServerError, httpErr.Code)

	mockService.AssertExpectations(t)
}

// TestUpdateProfile_EmptyBody tests empty update request (should succeed but do nothing)
func TestUpdateProfile_EmptyBody(t *testing.T) {
	// Setup
	mockService := new(MockProfileService)
	handler := NewProfileHandler(mockService)

	auth0UserID := "auth0|507f1f77bcf86cd799439011"
	email := "test@example.com"
	userID := uuid.New().String()
	now := time.Now()

	requestBody := `{}`

	unchangedUser := &domain.User{
		ID:                 userID,
		Auth0UserID:        auth0UserID,
		Email:              email,
		SubscriptionTier:   "premium",
		SubscriptionStatus: "active",
		PrimaryRegion:      "iad",
		IsActive:           true,
		IsDeleted:          false,
		CreatedAt:          now,
		UpdatedAt:          now,
	}

	c, rec := setupTestContext(http.MethodPut, "/user/profile", requestBody)
	setAuthContext(c, auth0UserID, email)

	// Mock expectations
	mockService.On("UpdateProfile", mock.Anything, auth0UserID, mock.Anything).Return(unchangedUser, nil)

	// Execute
	err := handler.UpdateProfile(c)

	// Assert
	assert.NoError(t, err)
	assert.Equal(t, http.StatusOK, rec.Code)

	mockService.AssertExpectations(t)
}
