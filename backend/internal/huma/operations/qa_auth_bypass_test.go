package operations

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/danielgtaylor/huma/v2/humatest"
	"github.com/maxnussbaum/BoostedBudget/internal/services"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockQAAuthBypassService is a hand-written mock for testing.
type mockQAAuthBypassService struct {
	result *services.QAAuthBypassResult
	err    error
}

func (m *mockQAAuthBypassService) CreateSignInToken(ctx context.Context, role string) (*services.QAAuthBypassResult, error) {
	return m.result, m.err
}

const validSecret = "test-secret-123"

func newTestBypassAPI(t *testing.T, svc services.QAAuthBypassService, environment string) humatest.TestAPI {
	t.Helper()
	_, api := humatest.New(t)
	handler := NewQAAuthBypassHandler(svc, validSecret, environment)
	RegisterQAAuthBypassRoutes(api, handler)
	return api
}

func postBypass(api humatest.TestAPI, secret, role string) *httptest.ResponseRecorder {
	body := `{"secret":"` + secret + `","role":"` + role + `"}`
	return api.Post("/api/v1/qa/auth/bypass", strings.NewReader(body))
}

func TestBypass_ProductionEnvironment_Returns404(t *testing.T) {
	mock := &mockQAAuthBypassService{result: &services.QAAuthBypassResult{URL: "http://x", Email: "qa@test", Tier: "premium"}}
	api := newTestBypassAPI(t, mock, "production")

	resp := postBypass(api, validSecret, "admin")

	assert.Equal(t, http.StatusNotFound, resp.Code)
}

// TestBypass_UnrecognisedEnvironment_Returns404 is the RED for finding B2-1's fail-open
// handler shape (#1432, batch ED). The guard used to be `h.environment == "production"`, so
// EVERY value that is not that exact lowercase string fell through to minting a five-minute
// Clerk sign-in token — including "Production", " production ", a typo, and the empty string.
// The gate is now an allowlist (domain.IsQAAuthBypassEnvironment): only qa and staging pass.
//
// Each case asserts 404 specifically, not merely "not 200": a 401 would mean the request died
// on the shared-secret comparison instead of the environment gate, and would make this test
// pass for the wrong reason. postBypass sends the VALID secret precisely so the secret check
// cannot be what produces the failure.
func TestBypass_UnrecognisedEnvironment_Returns404(t *testing.T) {
	for _, env := range []string{"Production", " production ", "prod", "", "development", "stagng"} {
		t.Run("environment="+env, func(t *testing.T) {
			mock := &mockQAAuthBypassService{result: &services.QAAuthBypassResult{URL: "http://x", Email: "qa@test", Tier: "premium"}}
			api := newTestBypassAPI(t, mock, env)

			resp := postBypass(api, validSecret, "admin")

			assert.Equal(t, http.StatusNotFound, resp.Code,
				"environment %q is not a recognised QA-bypass environment and must 404, not mint an admin sign-in token", env)
		})
	}
}

// TestBypass_RecognisedEnvironmentCaseInsensitive_StillWorks is the mandatory sibling: the
// hardening must not break QA/staging's legitimate use (the qa-login skill).
func TestBypass_RecognisedEnvironmentCaseInsensitive_StillWorks(t *testing.T) {
	for _, env := range []string{"qa", "staging", "  QA  ", "Staging"} {
		t.Run("environment="+env, func(t *testing.T) {
			expected := &services.QAAuthBypassResult{URL: "https://clerk.example.com/x", Email: "qa-admin@boostedbudget.com", Tier: "premium"}
			api := newTestBypassAPI(t, &mockQAAuthBypassService{result: expected}, env)

			resp := postBypass(api, validSecret, "admin")

			require.Equal(t, http.StatusOK, resp.Code)
		})
	}
}

func TestBypass_InvalidSecret_Returns401(t *testing.T) {
	mock := &mockQAAuthBypassService{result: &services.QAAuthBypassResult{URL: "http://x", Email: "qa@test", Tier: "premium"}}
	api := newTestBypassAPI(t, mock, "qa")

	resp := postBypass(api, "wrong-secret", "admin")

	assert.Equal(t, http.StatusUnauthorized, resp.Code)
}

func TestBypass_AdminRole_Success(t *testing.T) {
	expected := &services.QAAuthBypassResult{
		URL:   "https://clerk.example.com/sign-in?token=abc",
		Email: "qa-admin@boostedbudget.com",
		Tier:  "premium",
	}
	mock := &mockQAAuthBypassService{result: expected}
	api := newTestBypassAPI(t, mock, "qa")

	resp := postBypass(api, validSecret, "admin")

	require.Equal(t, http.StatusOK, resp.Code)
	var body QAAuthBypassResponseBody
	err := json.Unmarshal(resp.Body.Bytes(), &body)
	require.NoError(t, err)
	assert.Equal(t, expected.URL, body.URL)
	assert.Equal(t, expected.Email, body.Email)
	assert.Equal(t, expected.Tier, body.Tier)
}

func TestBypass_PremiumRole_Success(t *testing.T) {
	expected := &services.QAAuthBypassResult{
		URL:   "https://clerk.example.com/sign-in?token=xyz",
		Email: "qa-premium@boostedbudget.com",
		Tier:  "premium",
	}
	mock := &mockQAAuthBypassService{result: expected}
	api := newTestBypassAPI(t, mock, "staging")

	resp := postBypass(api, validSecret, "premium")

	require.Equal(t, http.StatusOK, resp.Code)
	var body QAAuthBypassResponseBody
	err := json.Unmarshal(resp.Body.Bytes(), &body)
	require.NoError(t, err)
	assert.Equal(t, expected.URL, body.URL)
}

func TestBypass_ServiceError_Returns500(t *testing.T) {
	mock := &mockQAAuthBypassService{err: errors.New("clerk unreachable")}
	api := newTestBypassAPI(t, mock, "qa")

	resp := postBypass(api, validSecret, "admin")

	assert.Equal(t, http.StatusInternalServerError, resp.Code)
}

func TestBypass_InvalidRole_Returns400(t *testing.T) {
	mock := &mockQAAuthBypassService{err: services.ErrInvalidQARole}
	api := newTestBypassAPI(t, mock, "qa")

	resp := postBypass(api, validSecret, "admin")

	assert.Equal(t, http.StatusBadRequest, resp.Code)
}

func TestQAAuthBypassRouteRegistration(t *testing.T) {
	_, api := humatest.New(t)
	handler := NewQAAuthBypassHandler(&mockQAAuthBypassService{}, validSecret, "qa")
	RegisterQAAuthBypassRoutes(api, handler)

	// GET on a POST-only route returns 405 Method Not Allowed, confirming route exists.
	resp := api.Get("/api/v1/qa/auth/bypass")
	assert.Equal(t, http.StatusMethodNotAllowed, resp.Code)
}
