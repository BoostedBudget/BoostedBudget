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

func TestBypass_InvalidSecret_Returns401(t *testing.T) {
	mock := &mockQAAuthBypassService{result: &services.QAAuthBypassResult{URL: "http://x", Email: "qa@test", Tier: "premium"}}
	api := newTestBypassAPI(t, mock, "qa")

	resp := postBypass(api, "wrong-secret", "admin")

	assert.Equal(t, http.StatusUnauthorized, resp.Code)
}

func TestBypass_AdminRole_Success(t *testing.T) {
	expected := &services.QAAuthBypassResult{
		URL:   "https://clerk.example.com/sign-in?token=abc",
		Email: "qa-admin@boostedbudget.test",
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
		Email: "qa-premium@boostedbudget.test",
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
