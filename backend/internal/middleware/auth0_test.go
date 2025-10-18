package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/labstack/echo/v4"
)

// TestAuth0Middleware_ValidToken tests that a valid JWT token is accepted
// RED: This test will fail until we implement the middleware
func TestAuth0Middleware_ValidToken(t *testing.T) {
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/protected", nil)

	// Create a valid test JWT token
	token := createTestToken(t, "auth0|123456", "user@example.com")
	req.Header.Set("Authorization", "Bearer "+token)

	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	// Create middleware
	middleware := Auth0Middleware("https://test-tenant.auth0.com/", "https://api.test.com")

	// Define a test handler that checks if user_id is in context
	handler := func(c echo.Context) error {
		userID := c.Get("user_id")
		if userID == nil {
			t.Error("Expected user_id in context, got nil")
		}
		if userID != "auth0|123456" {
			t.Errorf("Expected user_id 'auth0|123456', got '%v'", userID)
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
// RED: This test will fail until we implement the middleware
func TestAuth0Middleware_InvalidToken(t *testing.T) {
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/protected", nil)
	req.Header.Set("Authorization", "Bearer invalid.token.here")
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	middleware := Auth0Middleware("https://test-tenant.auth0.com/", "https://api.test.com")

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
// RED: This test will fail until we implement the middleware
func TestAuth0Middleware_ExpiredToken(t *testing.T) {
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/protected", nil)

	// Create an expired token (expired 1 hour ago)
	token := createExpiredTestToken(t, "auth0|123456", "user@example.com")
	req.Header.Set("Authorization", "Bearer "+token)

	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	middleware := Auth0Middleware("https://test-tenant.auth0.com/", "https://api.test.com")

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
// RED: This test will fail until we implement the middleware
func TestAuth0Middleware_MissingAuthHeader(t *testing.T) {
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/protected", nil)
	// No Authorization header
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	middleware := Auth0Middleware("https://test-tenant.auth0.com/", "https://api.test.com")

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
// RED: This test will fail until we implement the middleware
func TestAuth0Middleware_CustomClaimsExtraction(t *testing.T) {
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/protected", nil)

	// Create token with custom claims
	token := createTestTokenWithClaims(t, "auth0|123456", map[string]interface{}{
		"https://boostedbudget.com/subscription_tier": "premium",
		"https://boostedbudget.com/primary_region":    "us-east-1",
	})
	req.Header.Set("Authorization", "Bearer "+token)

	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	middleware := Auth0Middleware("https://test-tenant.auth0.com/", "https://api.test.com")

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
			if region != "us-east-1" {
				t.Errorf("Expected primary_region 'us-east-1', got '%v'", region)
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
func createTestToken(t *testing.T, userID, email string) string {
	claims := jwt.MapClaims{
		"sub":   userID,
		"email": email,
		"iss":   "https://test-tenant.auth0.com/",
		"aud":   "https://api.test.com",
		"iat":   time.Now().Unix(),
		"exp":   time.Now().Add(24 * time.Hour).Unix(),
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	tokenString, err := token.SignedString([]byte("test-secret-key"))
	if err != nil {
		t.Fatalf("Failed to create test token: %v", err)
	}

	return tokenString
}

// Helper function to create a test JWT token with custom claims
func createTestTokenWithClaims(t *testing.T, userID string, customClaims map[string]interface{}) string {
	claims := jwt.MapClaims{
		"sub": userID,
		"iss": "https://test-tenant.auth0.com/",
		"aud": "https://api.test.com",
		"iat": time.Now().Unix(),
		"exp": time.Now().Add(24 * time.Hour).Unix(),
	}

	// Add custom claims
	for key, value := range customClaims {
		claims[key] = value
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	tokenString, err := token.SignedString([]byte("test-secret-key"))
	if err != nil {
		t.Fatalf("Failed to create test token: %v", err)
	}

	return tokenString
}

// Helper function to create an expired test JWT token
func createExpiredTestToken(t *testing.T, userID, email string) string {
	claims := jwt.MapClaims{
		"sub":   userID,
		"email": email,
		"iss":   "https://test-tenant.auth0.com/",
		"aud":   "https://api.test.com",
		"iat":   time.Now().Add(-2 * time.Hour).Unix(),
		"exp":   time.Now().Add(-1 * time.Hour).Unix(), // Expired 1 hour ago
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	tokenString, err := token.SignedString([]byte("test-secret-key"))
	if err != nil {
		t.Fatalf("Failed to create expired token: %v", err)
	}

	return tokenString
}

// TestGetUserID tests the GetUserID helper function
func TestGetUserID(t *testing.T) {
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	// Test with user_id in context
	c.Set("user_id", "auth0|123456")
	userID, err := GetUserID(c)
	if err != nil {
		t.Errorf("Expected no error, got: %v", err)
	}
	if userID != "auth0|123456" {
		t.Errorf("Expected 'auth0|123456', got '%s'", userID)
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
		"https://boostedbudget.com/primary_region": "eu-west-1",
	}
	c.Set("claims", testClaims)
	region := GetPrimaryRegion(c)
	if region != "eu-west-1" {
		t.Errorf("Expected 'eu-west-1', got '%s'", region)
	}

	// Test without region in claims (should default to "us-east-1")
	c2 := e.NewContext(req, rec)
	region = GetPrimaryRegion(c2)
	if region != "us-east-1" {
		t.Errorf("Expected default 'us-east-1', got '%s'", region)
	}
}

// TestAuth0Middleware_InvalidIssuer tests that tokens with wrong issuer are rejected
func TestAuth0Middleware_InvalidIssuer(t *testing.T) {
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

	middleware := Auth0Middleware("https://test-tenant.auth0.com/", "https://api.test.com")

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

	middleware := Auth0Middleware("https://test-tenant.auth0.com/", "https://api.test.com")

	handler := func(c echo.Context) error {
		t.Error("Handler should not be called with invalid audience")
		return c.JSON(http.StatusOK, map[string]string{"status": "ok"})
	}

	err := middleware(handler)(c)
	if err == nil {
		t.Error("Expected error for invalid audience")
	}
}
