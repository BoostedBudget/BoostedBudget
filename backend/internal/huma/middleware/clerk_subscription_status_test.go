package middleware_test

// #1413: Tests for the SubscriptionStatusReader branch added to clerk.go's attach() (the
// EFFECTIVE-subscription-status lookup that replaced the stale raw subscription_status).
// These drive the REAL Clerk middleware (not the simulated header-injection double used by
// read_only_test.go) via the MCP-origin auth branch, which reaches the same attach() code path
// as the JWT path without requiring a real, Clerk-signed JWT (see clerk_mcp_test.go, which
// established this pattern for testing attach() end-to-end).

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humachi"
	"github.com/go-chi/chi/v5"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/huma/inputs"
	humamiddleware "github.com/maxnussbaum/BoostedBudget/internal/huma/middleware"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// stubSubscriptionStatusReader is a test double for SubscriptionStatusReader, standing in for
// profile_repository.go's GetEffectiveSubscriptionStatusByUserID.
type stubSubscriptionStatusReader struct {
	status string
	err    error
	calls  int
}

func (s *stubSubscriptionStatusReader) GetEffectiveSubscriptionStatusByUserID(_ context.Context, _ string) (string, error) {
	s.calls++
	return s.status, s.err
}

// subStatusProbeBody mirrors the JSON body returned by the probe endpoint registered in
// setupSubStatusProbeServer, letting tests observe what attach() actually stored in context.
type subStatusProbeBody struct {
	EffectiveStatus     string `json:"effective_status"`
	HasEffectiveStatus  bool   `json:"has_effective_status"`
	Unresolved          bool   `json:"unresolved"`
	HasUnresolvedStatus bool   `json:"has_unresolved_status"`
}

// setupSubStatusProbeServer wires the REAL Clerk middleware via the MCP-origin auth branch
// (stub identity repo + stub OAuth verifier, as in clerk_mcp_test.go) so attach() runs for real,
// with reader passed through as the SubscriptionStatusReader. Passing a nil reader omits the
// variadic profileRepo argument entirely, matching the "no status reader configured" case.
func setupSubStatusProbeServer(t *testing.T, reader humamiddleware.SubscriptionStatusReader) *httptest.Server {
	t.Helper()
	router := chi.NewRouter()
	api := humachi.New(router, huma.DefaultConfig("Test API", "1.0.0"))

	repo := &mockUserIdentityRepository{
		getByClerkIDFunc: func(_ context.Context, _ string) (*domain.UserIdentity, error) {
			return &domain.UserIdentity{UserID: "internal-uuid-substatus"}, nil
		},
	}
	verifier := &stubOAuthVerifier{userID: "user_substatus"}
	mcpAuth := &humamiddleware.MCPAuthConfig{WorkerSecret: "worker-secret", Verifier: verifier}

	var mw func(huma.Context, func(huma.Context))
	if reader != nil {
		mw = humamiddleware.NewClerkMiddleware("sk_test_secret", repo, &mockRegistrationService{}, nil, mcpAuth, nil, reader)
	} else {
		mw = humamiddleware.NewClerkMiddleware("sk_test_secret", repo, &mockRegistrationService{}, nil, mcpAuth, nil)
	}
	api.UseMiddleware(mw)

	huma.Register(api, huma.Operation{
		OperationID: "sub-status-probe",
		Method:      http.MethodGet,
		Path:        "/test/sub-status-probe",
		Security:    []map[string][]string{{"BearerAuth": {}}},
	}, func(ctx context.Context, _ *struct{}) (*struct {
		Body subStatusProbeBody
	}, error) {
		out := &struct {
			Body subStatusProbeBody
		}{}
		if v, ok := ctx.Value(inputs.EffectiveSubscriptionStatusKey).(string); ok {
			out.Body.EffectiveStatus = v
			out.Body.HasEffectiveStatus = true
		}
		if v, ok := ctx.Value(inputs.SubscriptionStatusUnresolvedKey).(bool); ok {
			out.Body.Unresolved = v
			out.Body.HasUnresolvedStatus = true
		}
		return out, nil
	})

	server := httptest.NewServer(router)
	t.Cleanup(server.Close)
	return server
}

func subStatusProbeRequest(t *testing.T, baseURL string) *http.Response {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, baseURL+"/test/sub-status-probe", nil)
	require.NoError(t, err)
	req.Header.Set("X-MCP-Worker-Secret", "worker-secret")
	req.Header.Set("Authorization", "Bearer opaque-oauth-token")
	resp, doErr := (&http.Client{}).Do(req)
	require.NoError(t, doErr)
	return resp
}

// TestClerkMiddleware_SubscriptionStatus_LookupSucceeds_SetsEffectiveStatus covers state 1:
// lookup succeeds -> EffectiveSubscriptionStatusKey is set to the returned value, and
// SubscriptionStatusUnresolvedKey is absent.
func TestClerkMiddleware_SubscriptionStatus_LookupSucceeds_SetsEffectiveStatus(t *testing.T) {
	reader := &stubSubscriptionStatusReader{status: "active"}
	server := setupSubStatusProbeServer(t, reader)

	resp := subStatusProbeRequest(t, server.URL)
	defer func() { _ = resp.Body.Close() }()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	var body subStatusProbeBody
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&body))

	assert.Equal(t, 1, reader.calls)
	assert.True(t, body.HasEffectiveStatus, "EffectiveSubscriptionStatusKey must be set on lookup success")
	assert.Equal(t, "active", body.EffectiveStatus)
	assert.False(t, body.HasUnresolvedStatus, "SubscriptionStatusUnresolvedKey must be absent on lookup success")
}

// TestClerkMiddleware_SubscriptionStatus_LookupErrors_SetsUnresolved covers state 2: lookup
// returns an error -> SubscriptionStatusUnresolvedKey is true, and EffectiveSubscriptionStatusKey
// is absent.
func TestClerkMiddleware_SubscriptionStatus_LookupErrors_SetsUnresolved(t *testing.T) {
	reader := &stubSubscriptionStatusReader{err: errors.New("db unavailable")}
	server := setupSubStatusProbeServer(t, reader)

	resp := subStatusProbeRequest(t, server.URL)
	defer func() { _ = resp.Body.Close() }()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	var body subStatusProbeBody
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&body))

	assert.Equal(t, 1, reader.calls)
	assert.False(t, body.HasEffectiveStatus, "EffectiveSubscriptionStatusKey must be absent on lookup failure")
	assert.True(t, body.HasUnresolvedStatus, "SubscriptionStatusUnresolvedKey must be set on lookup failure")
	assert.True(t, body.Unresolved)
}

// TestClerkMiddleware_SubscriptionStatus_NoReaderConfigured_SetsNeitherKey covers state 3: no
// reader configured (nil) -> neither key is set.
func TestClerkMiddleware_SubscriptionStatus_NoReaderConfigured_SetsNeitherKey(t *testing.T) {
	server := setupSubStatusProbeServer(t, nil)

	resp := subStatusProbeRequest(t, server.URL)
	defer func() { _ = resp.Body.Close() }()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	var body subStatusProbeBody
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&body))

	assert.False(t, body.HasEffectiveStatus, "EffectiveSubscriptionStatusKey must be absent with no reader configured")
	assert.False(t, body.HasUnresolvedStatus, "SubscriptionStatusUnresolvedKey must be absent with no reader configured")
}

// ─── #1413 end-to-end regression: real Clerk middleware + real read-only middleware ──────────

// TestClerkAndReadOnlyMiddleware_CompedUser_POST_Allowed is the honest #1413 regression test at
// this layer. It wires the REAL Clerk middleware (driven via the MCP-origin auth branch, with a
// stub SubscriptionStatusReader standing in for profile_repository.go) together with the REAL
// read-only middleware, and proves a POST succeeds for a user whose repository-computed
// EFFECTIVE status is "active" while an admin override is in effect — even though, in
// production, that user's raw subscription_status column would still read "inactive".
//
// This replaces the deleted TestReadOnlyMiddleware_POST_CompedUser_EffectiveActive_Allowed in
// read_only_test.go, which asserted the same 200 status code but used a fake Clerk double that
// injected the header directly into the context. That test never reached clerk.go or
// profile_repository.go, so it could not have caught a regression in the actual
// override-resolution wiring — this test does, because both middlewares here are real.
func TestClerkAndReadOnlyMiddleware_CompedUser_POST_Allowed(t *testing.T) {
	repo := &mockUserIdentityRepository{
		getByClerkIDFunc: func(_ context.Context, _ string) (*domain.UserIdentity, error) {
			return &domain.UserIdentity{UserID: "internal-uuid-comped"}, nil
		},
	}
	verifier := &stubOAuthVerifier{userID: "user_comped"}
	mcpAuth := &humamiddleware.MCPAuthConfig{WorkerSecret: "worker-secret", Verifier: verifier}

	// Stands in for profile_repository.go's GetEffectiveSubscriptionStatusByUserID: a comped
	// user whose raw subscription_status column is "inactive" but who has a non-expired admin
	// override, so the repository resolves and returns the EFFECTIVE status "active".
	reader := &stubSubscriptionStatusReader{status: "active"}

	router := chi.NewRouter()
	api := humachi.New(router, huma.DefaultConfig("Test API", "1.0.0"))

	clerkMW := humamiddleware.NewClerkMiddleware("sk_test_secret", repo, &mockRegistrationService{}, nil, mcpAuth, nil, reader)
	api.UseMiddleware(clerkMW)
	// nil household checker mirrors setupReadOnlyTestAPI's non-household-aware variant.
	api.UseMiddleware(humamiddleware.NewReadOnlyMiddleware(nil))

	huma.Register(api, huma.Operation{
		OperationID: "comped-user-post",
		Method:      http.MethodPost,
		Path:        "/test/comped-mutation",
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
		}{Body: struct {
			Message string `json:"message"`
		}{Message: "mutated"}}, nil
	})

	server := httptest.NewServer(router)
	t.Cleanup(server.Close)

	req, err := http.NewRequest(http.MethodPost, server.URL+"/test/comped-mutation", nil)
	require.NoError(t, err)
	req.Header.Set("X-MCP-Worker-Secret", "worker-secret")
	req.Header.Set("Authorization", "Bearer opaque-oauth-token")
	resp, err := (&http.Client{}).Do(req)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()

	assert.Equal(t, http.StatusOK, resp.StatusCode,
		"a comped user (effective status active) must be allowed to mutate through the REAL Clerk + read-only middleware chain")
	assert.Equal(t, 1, reader.calls)
}
