package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/clerk/clerk-sdk-go/v2"
	"github.com/jackc/pgx/v5"
	"github.com/labstack/echo/v4"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/services"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestGetUserID tests the GetUserID helper function
func TestGetUserID_Success(t *testing.T) {
	// Arrange
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	expectedUserID := "user-uuid-12345"
	c.Set("user_id", expectedUserID)

	// Act
	userID, err := GetUserID(c)

	// Assert
	assert.NoError(t, err)
	assert.Equal(t, expectedUserID, userID)
}

func TestGetUserID_NotAuthenticated(t *testing.T) {
	// Arrange
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	// Don't set user_id

	// Act
	userID, err := GetUserID(c)

	// Assert
	assert.Error(t, err)
	assert.Equal(t, "", userID)
	assert.Contains(t, err.Error(), "not authenticated")
}

func TestGetUserID_InvalidType(t *testing.T) {
	// Arrange
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	// Set user_id as wrong type
	c.Set("user_id", 12345)

	// Act
	userID, err := GetUserID(c)

	// Assert
	assert.Error(t, err)
	assert.Equal(t, "", userID)
	assert.Contains(t, err.Error(), "invalid user_id type")
}

// TestGetClerkUserID tests the GetClerkUserID helper function
func TestGetClerkUserID_Success(t *testing.T) {
	// Arrange
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	expectedClerkID := "user_2abc123def456"
	c.Set("clerk_user_id", expectedClerkID)

	// Act
	clerkID, err := GetClerkUserID(c)

	// Assert
	assert.NoError(t, err)
	assert.Equal(t, expectedClerkID, clerkID)
}

func TestGetClerkUserID_NotFound(t *testing.T) {
	// Arrange
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	// Don't set clerk_user_id

	// Act
	clerkID, err := GetClerkUserID(c)

	// Assert
	assert.Error(t, err)
	assert.Equal(t, "", clerkID)
	assert.Contains(t, err.Error(), "clerk_user_id not found")
}

func TestGetClerkUserID_InvalidType(t *testing.T) {
	// Arrange
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	// Set clerk_user_id as wrong type
	c.Set("clerk_user_id", 12345)

	// Act
	clerkID, err := GetClerkUserID(c)

	// Assert
	assert.Error(t, err)
	assert.Equal(t, "", clerkID)
	assert.Contains(t, err.Error(), "invalid clerk_user_id type")
}

// TestGetClerkClaims tests the GetClerkClaims helper function
func TestGetClerkClaims_Success(t *testing.T) {
	// Arrange
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	expectedClaims := &clerk.SessionClaims{
		RegisteredClaims: clerk.RegisteredClaims{
			Subject: "user_abc123",
		},
		Custom: map[string]interface{}{
			"email": "test@example.com",
		},
	}
	c.Set("claims", expectedClaims)

	// Act
	claims, err := GetClerkClaims(c)

	// Assert
	assert.NoError(t, err)
	assert.NotNil(t, claims)
	assert.Equal(t, "user_abc123", claims.Subject)
}

func TestGetClerkClaims_NotFound(t *testing.T) {
	// Arrange
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	// Don't set claims

	// Act
	claims, err := GetClerkClaims(c)

	// Assert
	assert.Error(t, err)
	assert.Nil(t, claims)
	assert.Contains(t, err.Error(), "claims not found")
}

func TestGetClerkClaims_InvalidType(t *testing.T) {
	// Arrange
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	// Set claims as wrong type
	c.Set("claims", "invalid")

	// Act
	claims, err := GetClerkClaims(c)

	// Assert
	assert.Error(t, err)
	assert.Nil(t, claims)
	assert.Contains(t, err.Error(), "invalid claims type")
}

// TestGetEmail tests the GetEmail helper function
func TestGetEmail_Success(t *testing.T) {
	// Arrange
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	claims := &clerk.SessionClaims{
		Custom: map[string]interface{}{
			"email": "test@example.com",
		},
	}
	c.Set("claims", claims)

	// Act
	email, err := GetEmail(c)

	// Assert
	assert.NoError(t, err)
	assert.Equal(t, "test@example.com", email)
}

func TestGetEmail_NoClaims(t *testing.T) {
	// Arrange
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	// Don't set claims

	// Act
	email, err := GetEmail(c)

	// Assert
	assert.Error(t, err)
	assert.Equal(t, "", email)
}

func TestGetEmail_NoEmailInClaims(t *testing.T) {
	// Arrange
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	claims := &clerk.SessionClaims{
		Custom: map[string]interface{}{
			"other_field": "value",
		},
	}
	c.Set("claims", claims)

	// Act
	email, err := GetEmail(c)

	// Assert
	assert.Error(t, err)
	assert.Equal(t, "", email)
	assert.Contains(t, err.Error(), "email not found")
}

// TestGetSubscriptionTier tests the GetSubscriptionTier helper function
func TestGetSubscriptionTier_Success(t *testing.T) {
	// Arrange
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	claims := &clerk.SessionClaims{
		Custom: map[string]interface{}{
			"subscription_tier": "premium",
		},
	}
	c.Set("claims", claims)

	// Act
	tier := GetSubscriptionTier(c)

	// Assert
	assert.Equal(t, "premium", tier)
}

func TestGetSubscriptionTier_DefaultsToFree_NoClaims(t *testing.T) {
	// Arrange
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	// Don't set claims

	// Act
	tier := GetSubscriptionTier(c)

	// Assert
	assert.Equal(t, "free", tier)
}

func TestGetSubscriptionTier_DefaultsToFree_NoTierInClaims(t *testing.T) {
	// Arrange
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	claims := &clerk.SessionClaims{
		Custom: map[string]interface{}{
			"email": "test@example.com",
		},
	}
	c.Set("claims", claims)

	// Act
	tier := GetSubscriptionTier(c)

	// Assert
	assert.Equal(t, "free", tier)
}

// TestGetPrimaryRegion tests the GetPrimaryRegion helper function
func TestGetPrimaryRegion_Success(t *testing.T) {
	// Arrange
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	claims := &clerk.SessionClaims{
		Custom: map[string]interface{}{
			"primary_region": "eu-west-1",
		},
	}
	c.Set("claims", claims)

	// Act
	region := GetPrimaryRegion(c)

	// Assert
	assert.Equal(t, "eu-west-1", region)
}

func TestGetPrimaryRegion_DefaultsToIad_NoClaims(t *testing.T) {
	// Arrange
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	// Don't set claims

	// Act
	region := GetPrimaryRegion(c)

	// Assert
	assert.Equal(t, "iad", region)
}

func TestGetPrimaryRegion_DefaultsToIad_NoRegionInClaims(t *testing.T) {
	// Arrange
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	claims := &clerk.SessionClaims{
		Custom: map[string]interface{}{
			"email": "test@example.com",
		},
	}
	c.Set("claims", claims)

	// Act
	region := GetPrimaryRegion(c)

	// Assert
	assert.Equal(t, "iad", region)
}

// TestClerkMiddleware_MissingAuthHeader tests that middleware rejects requests without auth header
func TestClerkMiddleware_MissingAuthHeader(t *testing.T) {
	// Arrange - Create mock repository and service
	mockRepo := &mockUserIdentityRepository{}
	mockService := &mockRegistrationService{}

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	// Don't set Authorization header
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	handler := func(c echo.Context) error {
		t.Fatal("Handler should not be called")
		return nil
	}

	middleware := ClerkMiddleware("sk_test_secret", mockRepo, mockService)
	h := middleware(handler)

	// Act
	err := h(c)

	// Assert
	require.Error(t, err)
	httpErr, ok := err.(*echo.HTTPError)
	require.True(t, ok, "Expected HTTPError")
	assert.Equal(t, http.StatusUnauthorized, httpErr.Code)
	assert.Equal(t, "Missing authorization header", httpErr.Message)
}

// TestClerkMiddleware_InvalidBearerFormat tests that middleware rejects invalid bearer format
func TestClerkMiddleware_InvalidBearerFormat(t *testing.T) {
	testCases := []struct {
		name       string
		authHeader string
	}{
		{"No Bearer prefix", "some-token-here"},
		{"Wrong prefix", "Basic abc123"},
		{"Bearer without token", "Bearer"},
		{"Empty bearer token", "Bearer "},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Arrange
			mockRepo := &mockUserIdentityRepository{}
			mockService := &mockRegistrationService{}

			e := echo.New()
			req := httptest.NewRequest(http.MethodGet, "/test", nil)
			req.Header.Set("Authorization", tc.authHeader)
			rec := httptest.NewRecorder()
			c := e.NewContext(req, rec)

			handler := func(c echo.Context) error {
				t.Fatal("Handler should not be called")
				return nil
			}

			middleware := ClerkMiddleware("sk_test_secret", mockRepo, mockService)
			h := middleware(handler)

			// Act
			err := h(c)

			// Assert
			require.Error(t, err)
			httpErr, ok := err.(*echo.HTTPError)
			require.True(t, ok, "Expected HTTPError")
			assert.Equal(t, http.StatusUnauthorized, httpErr.Code)
		})
	}
}

// TestClerkMiddleware_InvalidToken tests that middleware rejects invalid JWT tokens
// Note: This test uses an obviously invalid token that the Clerk SDK will reject
func TestClerkMiddleware_InvalidToken(t *testing.T) {
	// Arrange
	mockRepo := &mockUserIdentityRepository{}
	mockService := &mockRegistrationService{}

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set("Authorization", "Bearer invalid.jwt.token")
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	handler := func(c echo.Context) error {
		t.Fatal("Handler should not be called for invalid token")
		return nil
	}

	middleware := ClerkMiddleware("sk_test_secret", mockRepo, mockService)
	h := middleware(handler)

	// Act
	err := h(c)

	// Assert
	require.Error(t, err)
	httpErr, ok := err.(*echo.HTTPError)
	require.True(t, ok, "Expected HTTPError")
	assert.Equal(t, http.StatusUnauthorized, httpErr.Code)
	assert.Equal(t, "Invalid or expired token", httpErr.Message)
}

// Mock implementations for testing
// These implement the repository.UserIdentityRepository and services.RegistrationService interfaces

type mockUserIdentityRepository struct {
	getByClerkIDFunc  func(ctx context.Context, clerkUserID string) (*domain.UserIdentity, error)
	getByUserIDFunc   func(ctx context.Context, userID string) ([]*domain.UserIdentity, error)
	createFunc        func(ctx context.Context, identity *domain.UserIdentity) error
	createTxFunc      func(ctx context.Context, tx pgx.Tx, identity *domain.UserIdentity) error
	updateLastUsedErr error
}

func (m *mockUserIdentityRepository) GetByClerkID(ctx context.Context, clerkUserID string) (*domain.UserIdentity, error) {
	if m.getByClerkIDFunc != nil {
		return m.getByClerkIDFunc(ctx, clerkUserID)
	}
	return nil, nil
}

func (m *mockUserIdentityRepository) GetByUserID(ctx context.Context, userID string) ([]*domain.UserIdentity, error) {
	if m.getByUserIDFunc != nil {
		return m.getByUserIDFunc(ctx, userID)
	}
	return nil, nil
}

func (m *mockUserIdentityRepository) Create(ctx context.Context, identity *domain.UserIdentity) error {
	if m.createFunc != nil {
		return m.createFunc(ctx, identity)
	}
	return nil
}

func (m *mockUserIdentityRepository) CreateTx(ctx context.Context, tx pgx.Tx, identity *domain.UserIdentity) error {
	if m.createTxFunc != nil {
		return m.createTxFunc(ctx, tx, identity)
	}
	return nil
}

func (m *mockUserIdentityRepository) UpdateLastUsed(ctx context.Context, clerkUserID string) error {
	return m.updateLastUsedErr
}

type mockRegistrationService struct {
	registerUserFunc func(ctx context.Context, clerkUserID, email string, emailVerified bool) (*domain.User, error)
}

func (m *mockRegistrationService) RegisterUser(ctx context.Context, clerkUserID, email string, emailVerified bool) (*domain.User, error) {
	if m.registerUserFunc != nil {
		return m.registerUserFunc(ctx, clerkUserID, email, emailVerified)
	}
	return nil, nil
}

// Issue #679: SetTrialGranter for auto-granting trials on registration
func (m *mockRegistrationService) SetTrialGranter(_ services.TrialGranter) {
	// No-op for tests
}

// Integration test note:
// Full middleware integration tests (testing the complete flow with valid JWT tokens)
// would require either:
// 1. Clerk test environment with pre-signed test tokens
// 2. A test mode configuration in Clerk SDK
// 3. Refactoring to make jwt.Verify injectable (not recommended for production code)
//
// The current tests cover:
// - All helper functions (GetUserID, GetClerkUserID, GetClerkClaims, GetEmail, etc.)
// - Error paths that don't require valid JWT (missing header, invalid format, invalid token)
// - Context value handling
//
// For end-to-end testing with real tokens, see the E2E test suite in apps/web/e2e/

// TestClerkMiddleware_ContextValuesSet verifies context values are accessible
// This test simulates what happens after successful JWT verification
func TestClerkMiddleware_ContextValuesSet(t *testing.T) {
	// Arrange
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	// Simulate what middleware sets after successful auth
	testUserID := "user-uuid-abc123"
	testClerkID := "user_2xyz789"
	expiry := time.Now().Add(time.Hour).Unix()
	testClaims := &clerk.SessionClaims{
		RegisteredClaims: clerk.RegisteredClaims{
			Subject: testClerkID,
			Expiry:  &expiry,
		},
		Custom: map[string]interface{}{
			"email":             "test@example.com",
			"subscription_tier": "premium_lite",
			"primary_region":    "eu-central-1",
		},
	}

	c.Set("user_id", testUserID)
	c.Set("clerk_user_id", testClerkID)
	c.Set("claims", testClaims)

	// Act & Assert - All helper functions should work correctly
	userID, err := GetUserID(c)
	assert.NoError(t, err)
	assert.Equal(t, testUserID, userID)

	clerkID, err := GetClerkUserID(c)
	assert.NoError(t, err)
	assert.Equal(t, testClerkID, clerkID)

	claims, err := GetClerkClaims(c)
	assert.NoError(t, err)
	assert.NotNil(t, claims)
	assert.Equal(t, testClerkID, claims.Subject)

	email, err := GetEmail(c)
	assert.NoError(t, err)
	assert.Equal(t, "test@example.com", email)

	tier := GetSubscriptionTier(c)
	assert.Equal(t, "premium_lite", tier)

	region := GetPrimaryRegion(c)
	assert.Equal(t, "eu-central-1", region)
}
