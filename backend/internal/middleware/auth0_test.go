package middleware

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/repository"
)

// Test RSA key pair - generated once for all tests
var (
	testPrivateKey *rsa.PrivateKey
	testPublicKey  *rsa.PublicKey
	testKid        = "test-key-id-123"
)

func init() {
	// Generate test RSA key pair
	var err error
	testPrivateKey, err = rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		panic("Failed to generate test RSA key: " + err.Error())
	}
	testPublicKey = &testPrivateKey.PublicKey
}

// mockIdentityRepository is a mock implementation of UserIdentityRepository for testing
type mockIdentityRepository struct {
	identities map[string]*domain.UserIdentity // Map: auth0_user_id -> UserIdentity
}

func newMockIdentityRepository() *mockIdentityRepository {
	return &mockIdentityRepository{
		identities: make(map[string]*domain.UserIdentity),
	}
}

func (m *mockIdentityRepository) GetByAuth0ID(ctx context.Context, auth0UserID string) (*domain.UserIdentity, error) {
	identity, ok := m.identities[auth0UserID]
	if !ok {
		return nil, repository.ErrIdentityNotFound
	}
	return identity, nil
}

func (m *mockIdentityRepository) GetByUserID(ctx context.Context, userID string) ([]*domain.UserIdentity, error) {
	var result []*domain.UserIdentity
	for _, identity := range m.identities {
		if identity.UserID == userID {
			result = append(result, identity)
		}
	}
	return result, nil
}

func (m *mockIdentityRepository) Create(ctx context.Context, identity *domain.UserIdentity) error {
	if _, exists := m.identities[identity.Auth0UserID]; exists {
		return repository.ErrDuplicateIdentity
	}
	m.identities[identity.Auth0UserID] = identity
	return nil
}

func (m *mockIdentityRepository) UpdateLastUsed(ctx context.Context, auth0UserID string) error {
	identity, ok := m.identities[auth0UserID]
	if !ok {
		return repository.ErrIdentityNotFound
	}
	now := time.Now()
	identity.LastUsedAt = &now
	return nil
}

// Helper to add a test identity to the mock repository
func (m *mockIdentityRepository) addTestIdentity(auth0UserID, userID string) {
	now := time.Now()
	m.identities[auth0UserID] = &domain.UserIdentity{
		ID:             uuid.New().String(),
		UserID:         userID,
		Auth0UserID:    auth0UserID,
		Provider:       "auth0",
		ProviderUserID: "123456",
		Email:          "test@example.com",
		EmailVerified:  true,
		IsPrimary:      true,
		CreatedAt:      now,
		UpdatedAt:      now,
	}
}

// setupMockJWKS creates a mock JWKS HTTP server for testing
func setupMockJWKS(t *testing.T) *httptest.Server {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/.well-known/jwks.json" {
			http.NotFound(w, r)
			return
		}

		// Convert public key to JWK format
		n := base64.RawURLEncoding.EncodeToString(testPublicKey.N.Bytes())
		e := base64.RawURLEncoding.EncodeToString(big.NewInt(int64(testPublicKey.E)).Bytes())

		jwks := JWKS{
			Keys: []JSONWebKey{
				{
					Kty: "RSA",
					Use: "sig",
					Kid: testKid,
					N:   n,
					E:   e,
				},
			},
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(jwks)
	}))

	// Clear cache before each test to ensure fresh JWKS fetch
	cache.mu.Lock()
	cache.keys = make(map[string]*rsa.PublicKey)
	cache.expiresAt = time.Time{}
	cache.mu.Unlock()

	return server
}

// TestAuth0Middleware_ValidToken tests that a valid JWT token is accepted
// and resolves to internal user UUID
func TestAuth0Middleware_ValidToken(t *testing.T) {
	// Setup mock JWKS server
	jwksServer := setupMockJWKS(t)
	defer jwksServer.Close()

	// Setup mock identity repository
	mockRepo := newMockIdentityRepository()
	testUserID := uuid.New().String()
	mockRepo.addTestIdentity("auth0|123456", testUserID)

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/protected", nil)

	// Create a valid test JWT token
	token := createTestToken(t, "auth0|123456", "user@example.com", jwksServer.URL)
	req.Header.Set("Authorization", "Bearer "+token)

	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	// Create middleware with mock issuer and identity repository
	middleware := Auth0Middleware(jwksServer.URL+"/", "https://api.test.com", mockRepo)

	// Define a test handler that checks if user_id (UUID) is in context
	handler := func(c echo.Context) error {
		userID := c.Get("user_id")
		if userID == nil {
			t.Error("Expected user_id in context, got nil")
		}
		// Should be the internal UUID, NOT the Auth0 ID
		if userID != testUserID {
			t.Errorf("Expected user_id '%s' (UUID), got '%v'", testUserID, userID)
		}
		// Auth0 ID should also be available in context
		auth0ID := c.Get("auth0_user_id")
		if auth0ID != "auth0|123456" {
			t.Errorf("Expected auth0_user_id 'auth0|123456', got '%v'", auth0ID)
		}
		return c.JSON(http.StatusOK, map[string]string{"status": "ok"})
	}

	// Execute middleware
	err := middleware(handler)(c)
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}

	// Verify response
	if rec.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", rec.Code)
	}
}

// TestAuth0Middleware_InvalidToken tests that an invalid JWT token is rejected
func TestAuth0Middleware_InvalidToken(t *testing.T) {
	mockRepo := newMockIdentityRepository()

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/protected", nil)
	req.Header.Set("Authorization", "Bearer invalid.token.here")
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	middleware := Auth0Middleware("https://test-tenant.auth0.com/", "https://api.test.com", mockRepo)

	handler := func(c echo.Context) error {
		t.Error("Handler should not be called with invalid token")
		return c.JSON(http.StatusOK, map[string]string{"status": "ok"})
	}

	err := middleware(handler)(c)

	// Middleware should return an error for invalid token
	if err == nil {
		t.Error("Expected error for invalid token, got nil")
	}

	// Should be an HTTP error with status 401
	if he, ok := err.(*echo.HTTPError); ok {
		if he.Code != http.StatusUnauthorized {
			t.Errorf("Expected status 401, got %d", he.Code)
		}
	} else {
		t.Error("Expected echo.HTTPError")
	}
}

// TestAuth0Middleware_ExpiredToken tests that an expired JWT token is rejected
func TestAuth0Middleware_ExpiredToken(t *testing.T) {
	// Setup mock JWKS server (needed to verify signature even though token is expired)
	jwksServer := setupMockJWKS(t)
	defer jwksServer.Close()

	mockRepo := newMockIdentityRepository()
	testUserID := uuid.New().String()
	mockRepo.addTestIdentity("auth0|123456", testUserID)

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/protected", nil)

	// Create an expired token (expired 1 hour ago)
	token := createExpiredTestToken(t, "auth0|123456", "user@example.com", jwksServer.URL)
	req.Header.Set("Authorization", "Bearer "+token)

	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	middleware := Auth0Middleware(jwksServer.URL+"/", "https://api.test.com", mockRepo)

	handler := func(c echo.Context) error {
		t.Error("Handler should not be called with expired token")
		return c.JSON(http.StatusOK, map[string]string{"status": "ok"})
	}

	err := middleware(handler)(c)

	if err == nil {
		t.Error("Expected error for expired token, got nil")
	}

	if he, ok := err.(*echo.HTTPError); ok {
		if he.Code != http.StatusUnauthorized {
			t.Errorf("Expected status 401, got %d", he.Code)
		}
	}
}

// TestAuth0Middleware_MissingAuthHeader tests that requests without Authorization header are rejected
func TestAuth0Middleware_MissingAuthHeader(t *testing.T) {
	mockRepo := newMockIdentityRepository()

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/protected", nil)
	// No Authorization header
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	middleware := Auth0Middleware("https://test-tenant.auth0.com/", "https://api.test.com", mockRepo)

	handler := func(c echo.Context) error {
		t.Error("Handler should not be called without auth header")
		return c.JSON(http.StatusOK, map[string]string{"status": "ok"})
	}

	err := middleware(handler)(c)

	if err == nil {
		t.Error("Expected error for missing auth header, got nil")
	}

	if he, ok := err.(*echo.HTTPError); ok {
		if he.Code != http.StatusUnauthorized {
			t.Errorf("Expected status 401, got %d", he.Code)
		}
	}
}

// TestAuth0Middleware_CustomClaimsExtraction tests that custom claims are extracted correctly
func TestAuth0Middleware_CustomClaimsExtraction(t *testing.T) {
	// Setup mock JWKS server
	jwksServer := setupMockJWKS(t)
	defer jwksServer.Close()

	mockRepo := newMockIdentityRepository()
	testUserID := uuid.New().String()
	mockRepo.addTestIdentity("auth0|123456", testUserID)

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/protected", nil)

	// Create token with custom claims
	token := createTestTokenWithClaims(t, "auth0|123456", map[string]interface{}{
		"https://boostedbudget.com/subscription_tier": "premium",
		"https://boostedbudget.com/primary_region":    "iad",
	}, jwksServer.URL)
	req.Header.Set("Authorization", "Bearer "+token)

	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	middleware := Auth0Middleware(jwksServer.URL+"/", "https://api.test.com", mockRepo)

	handler := func(c echo.Context) error {
		// Check if custom claims are available in context
		claims := c.Get("claims")
		if claims == nil {
			t.Error("Expected claims in context, got nil")
		}

		// Verify subscription tier
		if claimsMap, ok := claims.(jwt.MapClaims); ok {
			tier := claimsMap["https://boostedbudget.com/subscription_tier"]
			if tier != "premium" {
				t.Errorf("Expected subscription_tier 'premium', got '%v'", tier)
			}

			region := claimsMap["https://boostedbudget.com/primary_region"]
			if region != "iad" {
				t.Errorf("Expected primary_region 'iad', got '%v'", region)
			}
		}

		return c.JSON(http.StatusOK, map[string]string{"status": "ok"})
	}

	err := middleware(handler)(c)
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}
}

// Helper function to create a valid test JWT token
func createTestToken(t *testing.T, userID, email, issuer string) string {
	claims := jwt.MapClaims{
		"sub":   userID,
		"email": email,
		"iss":   issuer + "/",
		"aud":   "https://api.test.com",
		"iat":   time.Now().Unix(),
		"exp":   time.Now().Add(24 * time.Hour).Unix(),
	}

	token := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	// Set kid header
	token.Header["kid"] = testKid

	tokenString, err := token.SignedString(testPrivateKey)
	if err != nil {
		t.Fatalf("Failed to create test token: %v", err)
	}

	return tokenString
}

// Helper function to create a test JWT token with custom claims
func createTestTokenWithClaims(t *testing.T, userID string, customClaims map[string]interface{}, issuer string) string {
	claims := jwt.MapClaims{
		"sub": userID,
		"iss": issuer + "/",
		"aud": "https://api.test.com",
		"iat": time.Now().Unix(),
		"exp": time.Now().Add(24 * time.Hour).Unix(),
	}

	// Add custom claims
	for key, value := range customClaims {
		claims[key] = value
	}

	token := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	// Set kid header
	token.Header["kid"] = testKid

	tokenString, err := token.SignedString(testPrivateKey)
	if err != nil {
		t.Fatalf("Failed to create test token: %v", err)
	}

	return tokenString
}

// Helper function to create an expired test JWT token
func createExpiredTestToken(t *testing.T, userID, email, issuer string) string {
	claims := jwt.MapClaims{
		"sub":   userID,
		"email": email,
		"iss":   issuer + "/",
		"aud":   "https://api.test.com",
		"iat":   time.Now().Add(-2 * time.Hour).Unix(),
		"exp":   time.Now().Add(-1 * time.Hour).Unix(), // Expired 1 hour ago
	}

	token := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	// Set kid header
	token.Header["kid"] = testKid

	tokenString, err := token.SignedString(testPrivateKey)
	if err != nil {
		t.Fatalf("Failed to create expired token: %v", err)
	}

	return tokenString
}

// TestGetUserID tests the GetUserID helper function
// BREAKING CHANGE: Now expects UUID instead of Auth0 ID
func TestGetUserID(t *testing.T) {
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	// Test with user_id (UUID) in context
	testUUID := uuid.New().String()
	c.Set("user_id", testUUID)
	userID, err := GetUserID(c)
	if err != nil {
		t.Errorf("Expected no error, got: %v", err)
	}
	if userID != testUUID {
		t.Errorf("Expected UUID '%s', got '%s'", testUUID, userID)
	}

	// Test without user_id in context
	c2 := e.NewContext(req, rec)
	_, err = GetUserID(c2)
	if err == nil {
		t.Error("Expected error when user_id not in context")
	}
}

// TestGetClaims tests the GetClaims helper function
func TestGetClaims(t *testing.T) {
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	// Test with claims in context
	testClaims := jwt.MapClaims{"sub": "auth0|123456"}
	c.Set("claims", testClaims)
	claims, err := GetClaims(c)
	if err != nil {
		t.Errorf("Expected no error, got: %v", err)
	}
	if claims["sub"] != "auth0|123456" {
		t.Errorf("Expected sub 'auth0|123456', got '%v'", claims["sub"])
	}

	// Test without claims in context
	c2 := e.NewContext(req, rec)
	_, err = GetClaims(c2)
	if err == nil {
		t.Error("Expected error when claims not in context")
	}
}

// TestGetSubscriptionTier tests the GetSubscriptionTier helper function
func TestGetSubscriptionTier(t *testing.T) {
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	// Test with premium tier in claims
	testClaims := jwt.MapClaims{
		"https://boostedbudget.com/subscription_tier": "premium",
	}
	c.Set("claims", testClaims)
	tier := GetSubscriptionTier(c)
	if tier != "premium" {
		t.Errorf("Expected 'premium', got '%s'", tier)
	}

	// Test without tier in claims (should default to "free")
	c2 := e.NewContext(req, rec)
	tier = GetSubscriptionTier(c2)
	if tier != "free" {
		t.Errorf("Expected default 'free', got '%s'", tier)
	}
}

// TestGetPrimaryRegion tests the GetPrimaryRegion helper function
func TestGetPrimaryRegion(t *testing.T) {
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	// Test with region in claims
	testClaims := jwt.MapClaims{
		"https://boostedbudget.com/primary_region": "lhr",
	}
	c.Set("claims", testClaims)
	region := GetPrimaryRegion(c)
	if region != "lhr" {
		t.Errorf("Expected 'lhr', got '%s'", region)
	}

	// Test without region in claims (should default to "iad")
	c2 := e.NewContext(req, rec)
	region = GetPrimaryRegion(c2)
	if region != "iad" {
		t.Errorf("Expected default 'iad', got '%s'", region)
	}
}

// TestAuth0Middleware_InvalidIssuer tests that tokens with wrong issuer are rejected
func TestAuth0Middleware_InvalidIssuer(t *testing.T) {
	mockRepo := newMockIdentityRepository()

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/protected", nil)

	// Create token with wrong issuer
	claims := jwt.MapClaims{
		"sub": "auth0|123456",
		"iss": "https://wrong-tenant.auth0.com/",
		"aud": "https://api.test.com",
		"iat": time.Now().Unix(),
		"exp": time.Now().Add(24 * time.Hour).Unix(),
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	tokenString, _ := token.SignedString([]byte("test-secret-key"))

	req.Header.Set("Authorization", "Bearer "+tokenString)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	middleware := Auth0Middleware("https://test-tenant.auth0.com/", "https://api.test.com", mockRepo)

	handler := func(c echo.Context) error {
		t.Error("Handler should not be called with invalid issuer")
		return c.JSON(http.StatusOK, map[string]string{"status": "ok"})
	}

	err := middleware(handler)(c)
	if err == nil {
		t.Error("Expected error for invalid issuer")
	}
}

// TestAuth0Middleware_InvalidAudience tests that tokens with wrong audience are rejected
func TestAuth0Middleware_InvalidAudience(t *testing.T) {
	mockRepo := newMockIdentityRepository()

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/protected", nil)

	// Create token with wrong audience
	claims := jwt.MapClaims{
		"sub": "auth0|123456",
		"iss": "https://test-tenant.auth0.com/",
		"aud": "https://api.wrong.com",
		"iat": time.Now().Unix(),
		"exp": time.Now().Add(24 * time.Hour).Unix(),
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	tokenString, _ := token.SignedString([]byte("test-secret-key"))

	req.Header.Set("Authorization", "Bearer "+tokenString)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	middleware := Auth0Middleware("https://test-tenant.auth0.com/", "https://api.test.com", mockRepo)

	handler := func(c echo.Context) error {
		t.Error("Handler should not be called with invalid audience")
		return c.JSON(http.StatusOK, map[string]string{"status": "ok"})
	}

	err := middleware(handler)(c)
	if err == nil {
		t.Error("Expected error for invalid audience")
	}
}
