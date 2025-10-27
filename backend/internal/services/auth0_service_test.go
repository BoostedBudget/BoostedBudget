package services

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// TestAuth0DeleteUser_Success tests successful Auth0 user deletion
func TestAuth0DeleteUser_Success(t *testing.T) {
	ctx := context.Background()

	// Create mock token server
	tokenServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/oauth/token" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode(map[string]interface{}{
				"access_token": "mock-access-token",
				"token_type":   "Bearer",
				"expires_in":   3600,
			})
			return
		}

		// Delete user endpoint
		if strings.Contains(r.URL.Path, "/api/v2/users/") {
			assert.Equal(t, "DELETE", r.Method)
			assert.Contains(t, r.Header.Get("Authorization"), "Bearer mock-access-token")
			w.WriteHeader(http.StatusNoContent)
			return
		}

		w.WriteHeader(http.StatusNotFound)
	}))
	defer tokenServer.Close()

	// Extract host from server URL (without scheme)
	serverURL, _ := url.Parse(tokenServer.URL)

	service := NewAuth0Service(serverURL.Host, "test-client-id", "test-client-secret")

	err := service.DeleteUser(ctx, "auth0|123456")

	assert.NoError(t, err)
}

// TestAuth0DeleteUser_NotFound tests handling of non-existent users
func TestAuth0DeleteUser_NotFound(t *testing.T) {
	ctx := context.Background()

	// Create mock server that returns 404
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/oauth/token" {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]interface{}{
				"access_token": "mock-token",
				"token_type":   "Bearer",
				"expires_in":   3600,
			})
			return
		}

		// Return 404 for user deletion
		if strings.Contains(r.URL.Path, "/api/v2/users/") {
			w.WriteHeader(http.StatusNotFound)
			w.Write([]byte(`{"error": "user not found"}`))
			return
		}
	}))
	defer server.Close()

	serverURL, _ := url.Parse(server.URL)
	service := NewAuth0Service(serverURL.Host, "test-client-id", "test-client-secret")

	err := service.DeleteUser(ctx, "auth0|nonexistent")

	// Should handle 404 gracefully (treat as success)
	assert.NoError(t, err)
}

// TestAuth0DeleteUser_APIError tests Auth0 API error handling
func TestAuth0DeleteUser_APIError(t *testing.T) {
	ctx := context.Background()

	// Create mock server that returns 500 error
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/oauth/token" {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]interface{}{
				"access_token": "mock-token",
				"token_type":   "Bearer",
				"expires_in":   3600,
			})
			return
		}

		// Return 500 for user deletion
		if strings.Contains(r.URL.Path, "/api/v2/users/") {
			w.WriteHeader(http.StatusInternalServerError)
			w.Write([]byte(`{"error": "internal server error"}`))
			return
		}
	}))
	defer server.Close()

	serverURL, _ := url.Parse(server.URL)
	service := NewAuth0Service(serverURL.Host, "test-client-id", "test-client-secret")

	err := service.DeleteUser(ctx, "auth0|123456")

	// Should return error for non-success, non-404 responses
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "delete user failed with status 500")
}

// TestAuth0GetManagementToken_Success tests M2M token retrieval
func TestAuth0GetManagementToken_Success(t *testing.T) {
	ctx := context.Background()

	// Create mock token server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/oauth/token" {
			assert.Equal(t, "POST", r.Method)
			assert.Equal(t, "application/json", r.Header.Get("Content-Type"))

			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode(map[string]interface{}{
				"access_token": "test-access-token-123",
				"token_type":   "Bearer",
				"expires_in":   3600,
			})
			return
		}
	}))
	defer server.Close()

	serverURL, _ := url.Parse(server.URL)
	service := NewAuth0Service(serverURL.Host, "test-client-id", "test-client-secret")

	token, err := service.GetManagementToken(ctx)

	assert.NoError(t, err)
	assert.Equal(t, "test-access-token-123", token)
}

// TestAuth0GetManagementToken_CachesToken tests token caching
func TestAuth0GetManagementToken_CachesToken(t *testing.T) {
	ctx := context.Background()

	callCount := 0

	// Create mock server that tracks call count
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/oauth/token" {
			callCount++

			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]interface{}{
				"access_token": "cached-token",
				"token_type":   "Bearer",
				"expires_in":   3600, // 1 hour expiry
			})
			return
		}
	}))
	defer server.Close()

	serverURL, _ := url.Parse(server.URL)
	service := NewAuth0Service(serverURL.Host, "test-client-id", "test-client-secret")

	// First call should fetch token
	token1, err1 := service.GetManagementToken(ctx)
	assert.NoError(t, err1)
	assert.Equal(t, "cached-token", token1)
	assert.Equal(t, 1, callCount, "First call should hit server")

	// Second call should use cached token
	token2, err2 := service.GetManagementToken(ctx)
	assert.NoError(t, err2)
	assert.Equal(t, token1, token2)
	assert.Equal(t, 1, callCount, "Second call should use cache, not hit server")

	// Brief wait, but token shouldn't expire yet
	time.Sleep(100 * time.Millisecond)

	// Third call should still use cached token
	token3, err3 := service.GetManagementToken(ctx)
	assert.NoError(t, err3)
	assert.Equal(t, token1, token3)
	assert.Equal(t, 1, callCount, "Third call should still use cache")
}
