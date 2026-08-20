package middleware_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humachi"
	"github.com/go-chi/chi/v5"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	humamiddleware "github.com/maxnussbaum/BoostedBudget/internal/huma/middleware"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// stubOAuthVerifier is a test double for the Clerk OAuth token verifier (#1247).
type stubOAuthVerifier struct {
	userID string
	err    error
	calls  int
}

func (s *stubOAuthVerifier) VerifyAccessToken(_ context.Context, _ string) (string, error) {
	s.calls++
	return s.userID, s.err
}

func setupMCPTestServer(
	t *testing.T,
	repo *mockUserIdentityRepository,
	svc *mockRegistrationService,
	workerSecret string,
	verifier humamiddleware.OAuthTokenVerifier,
) *httptest.Server {
	t.Helper()
	router := chi.NewRouter()
	api := humachi.New(router, huma.DefaultConfig("Test API", "1.0.0"))

	mw := humamiddleware.NewClerkMiddleware("sk_test_secret", repo, svc, nil, &humamiddleware.MCPAuthConfig{
		WorkerSecret: workerSecret,
		Verifier:     verifier,
	}, nil)
	api.UseMiddleware(mw)

	huma.Register(api, huma.Operation{
		OperationID: "mcp-protected",
		Method:      http.MethodGet,
		Path:        "/test/protected",
		Security:    []map[string][]string{{"BearerAuth": {}}},
	}, func(_ context.Context, _ *struct{}) (*struct {
		Body struct {
			Message string `json:"message"`
		}
	}, error) {
		return &struct {
			Body struct {
				Message string `json:"message"`
			}
		}{}, nil
	})

	server := httptest.NewServer(router)
	t.Cleanup(server.Close)
	return server
}

func mcpRequest(t *testing.T, baseURL, workerSecret, bearer string) *http.Response {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, baseURL+"/test/protected", nil)
	require.NoError(t, err)
	if workerSecret != "" {
		req.Header.Set("X-MCP-Worker-Secret", workerSecret)
	}
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	resp, err := (&http.Client{}).Do(req)
	require.NoError(t, err)
	return resp
}

// Valid worker secret + Clerk-verified token → authenticated (the #1247 fix).
func TestClerkMiddleware_MCPOrigin_ValidSecretAndToken(t *testing.T) {
	repo := &mockUserIdentityRepository{
		getByClerkIDFunc: func(_ context.Context, _ string) (*domain.UserIdentity, error) {
			return &domain.UserIdentity{UserID: "internal-uuid-1"}, nil
		},
	}
	verifier := &stubOAuthVerifier{userID: "user_abc"}
	server := setupMCPTestServer(t, repo, &mockRegistrationService{}, "worker-secret", verifier)

	resp := mcpRequest(t, server.URL, "worker-secret", "opaque-oauth-token")
	defer func() { _ = resp.Body.Close() }()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, 1, verifier.calls, "the OAuth token must be verified with Clerk")
}

// Valid worker secret but the token fails Clerk verification → 401.
func TestClerkMiddleware_MCPOrigin_VerifierRejects(t *testing.T) {
	verifier := &stubOAuthVerifier{err: errors.New("invalid token")}
	server := setupMCPTestServer(t, &mockUserIdentityRepository{}, &mockRegistrationService{}, "worker-secret", verifier)

	resp := mcpRequest(t, server.URL, "worker-secret", "bad-token")
	defer func() { _ = resp.Body.Close() }()

	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
}

// Valid worker secret but NO bearer token → 401, and verification is never attempted.
func TestClerkMiddleware_MCPOrigin_MissingBearer(t *testing.T) {
	verifier := &stubOAuthVerifier{userID: "user_abc"}
	server := setupMCPTestServer(t, &mockUserIdentityRepository{}, &mockRegistrationService{}, "worker-secret", verifier)

	resp := mcpRequest(t, server.URL, "worker-secret", "")
	defer func() { _ = resp.Body.Close() }()

	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
	assert.Equal(t, 0, verifier.calls)
}

// Defense-in-depth: a WRONG worker secret must NOT enter the MCP branch (verifier never
// called) and falls through to the JWT path, where an opaque token is rejected (401).
func TestClerkMiddleware_MCPOrigin_WrongSecretDoesNotBypass(t *testing.T) {
	verifier := &stubOAuthVerifier{userID: "user_abc"}
	server := setupMCPTestServer(t, &mockUserIdentityRepository{}, &mockRegistrationService{}, "worker-secret", verifier)

	resp := mcpRequest(t, server.URL, "wrong-secret", "opaque-oauth-token")
	defer func() { _ = resp.Body.Close() }()

	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
	assert.Equal(t, 0, verifier.calls, "verifier must not run when the worker secret is invalid")
}
