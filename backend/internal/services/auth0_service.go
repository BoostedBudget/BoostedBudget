package services

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
)

// Auth0Service defines the interface for Auth0 Management API operations
type Auth0Service interface {
	// DeleteUser deletes a user from Auth0 via Management API
	DeleteUser(ctx context.Context, auth0UserID string) error
	// GetManagementToken retrieves an M2M access token for Auth0 Management API
	GetManagementToken(ctx context.Context) (string, error)
}

// auth0Service implements Auth0Service
type auth0Service struct {
	domain       string
	clientID     string
	clientSecret string
	httpClient   *http.Client

	// Token cache
	tokenMu    sync.RWMutex
	token      string
	tokenExpAt time.Time
}

// tokenResponse represents Auth0 M2M token response
type tokenResponse struct {
	AccessToken string `json:"access_token"`
	TokenType   string `json:"token_type"`
	ExpiresIn   int    `json:"expires_in"`
}

// NewAuth0Service creates a new Auth0Service instance
func NewAuth0Service(domain, clientID, clientSecret string) Auth0Service {
	return &auth0Service{
		domain:       domain,
		clientID:     clientID,
		clientSecret: clientSecret,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// NewAuth0ServiceWithClient creates a new Auth0Service with a custom HTTP client (for testing)
func NewAuth0ServiceWithClient(domain, clientID, clientSecret string, httpClient *http.Client) Auth0Service {
	return &auth0Service{
		domain:       domain,
		clientID:     clientID,
		clientSecret: clientSecret,
		httpClient:   httpClient,
	}
}

// GetManagementToken retrieves and caches Auth0 Management API token
func (s *auth0Service) GetManagementToken(ctx context.Context) (string, error) {
	// Check cache first
	s.tokenMu.RLock()
	if s.token != "" && time.Now().Before(s.tokenExpAt) {
		token := s.token
		s.tokenMu.RUnlock()
		return token, nil
	}
	s.tokenMu.RUnlock()

	// Acquire write lock to fetch new token
	s.tokenMu.Lock()
	defer s.tokenMu.Unlock()

	// Double-check after acquiring write lock
	if s.token != "" && time.Now().Before(s.tokenExpAt) {
		return s.token, nil
	}

	// Fetch new token using client credentials flow
	scheme := s.getScheme()
	audience := fmt.Sprintf("%s://%s/api/v2/", scheme, s.domain)
	tokenURL := fmt.Sprintf("%s://%s/oauth/token", scheme, s.domain)

	payload := map[string]string{
		"client_id":     s.clientID,
		"client_secret": s.clientSecret,
		"audience":      audience,
		"grant_type":    "client_credentials",
	}

	jsonPayload, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("failed to marshal token request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", tokenURL, bytes.NewBuffer(jsonPayload))
	if err != nil {
		return "", fmt.Errorf("failed to create token request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to fetch token: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("token request failed with status %d: %s", resp.StatusCode, string(body))
	}

	var tokenResp tokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&tokenResp); err != nil {
		return "", fmt.Errorf("failed to decode token response: %w", err)
	}

	// Cache token (expire 5 minutes before actual expiration for safety)
	s.token = tokenResp.AccessToken
	s.tokenExpAt = time.Now().Add(time.Duration(tokenResp.ExpiresIn-300) * time.Second)

	return s.token, nil
}

// DeleteUser deletes a user from Auth0 using Management API
func (s *auth0Service) DeleteUser(ctx context.Context, auth0UserID string) error {
	// Get management token
	token, err := s.GetManagementToken(ctx)
	if err != nil {
		return fmt.Errorf("failed to get management token: %w", err)
	}

	// Construct delete URL
	scheme := s.getScheme()
	deleteURL := fmt.Sprintf("%s://%s/api/v2/users/%s", scheme, s.domain, auth0UserID)

	req, err := http.NewRequestWithContext(ctx, "DELETE", deleteURL, nil)
	if err != nil {
		return fmt.Errorf("failed to create delete request: %w", err)
	}

	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", token))

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to delete user: %w", err)
	}
	defer resp.Body.Close()

	// Handle response codes
	switch resp.StatusCode {
	case http.StatusNoContent, http.StatusOK:
		// Success - user deleted
		return nil
	case http.StatusNotFound:
		// User not found - treat as success (already deleted)
		return nil
	default:
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("delete user failed with status %d: %s", resp.StatusCode, string(body))
	}
}

// getScheme returns "http" for localhost/testing, "https" for production
func (s *auth0Service) getScheme() string {
	// Use HTTP for local testing (httptest servers)
	if strings.Contains(s.domain, "127.0.0.1") || strings.Contains(s.domain, "localhost") {
		return "http"
	}
	return "https"
}
