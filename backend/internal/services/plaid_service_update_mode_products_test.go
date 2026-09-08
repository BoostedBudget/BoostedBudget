package services

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/maxnussbaum/BoostedBudget/internal/config"
	"github.com/plaid/plaid-go/v23/plaid"
	"github.com/stretchr/testify/require"
)

// TestCreateUpdateModeLinkToken_OmitsProducts is the Issue #1609 P3-T8 (Phase 3 review H1)
// regression. Plaid's vendored spec (github.com/plaid/plaid-go/v23, api/openapi.yaml, the
// `products` field on LinkTokenCreateRequest) says verbatim: "List of Plaid product(s) you wish
// to use. If launching Link in update mode, should be omitted (unless you are using update mode
// to add Income or Assets to an Item); required otherwise." CreateUpdateModeLinkToken sets
// SetAccessToken (correct for update mode) but was ALSO calling SetProducts([transactions]) —
// we add neither Income nor Assets, so `products` must be omitted entirely.
//
// `optional_products` carries NO such restriction in the spec (its description never mentions
// update mode), so it is deliberately left in place and this test asserts it is STILL present —
// proving the fix is scoped to `products` only, not a blanket removal of both fields.
//
// pgxmock-style unit tests cannot catch this: nothing about SetProducts/SetOptionalProducts
// fails locally — Plaid enforces the restriction server-side. This test intercepts the actual
// HTTP request body sent to /link/token/create via an httptest server standing in for Plaid.
func TestCreateUpdateModeLinkToken_OmitsProducts(t *testing.T) {
	var capturedBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.NoError(t, json.NewDecoder(r.Body).Decode(&capturedBody))
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"link_token":"link-sandbox-test","expiration":"2099-01-01T00:00:00Z","request_id":"req-test"}`))
	}))
	defer server.Close()

	cfg := &config.Config{
		PlaidClientID:    "test_client_id",
		PlaidSecret:      "test_secret",
		PlaidEnv:         "sandbox",
		PlaidWebhookURL:  "https://api.boostedbudget.com/webhooks/plaid",
		PlaidRedirectURI: "https://boostedbudget.com/oauth-callback",
	}

	svc, err := NewPlaidService(cfg)
	require.NoError(t, err)

	// Point the client at our stand-in server instead of real Plaid.
	svc.GetClient().GetConfig().Servers = plaid.ServerConfigurations{
		{URL: server.URL},
	}

	_, err = svc.CreateUpdateModeLinkToken(context.Background(), "user_test-123", "access-sandbox-test-token")
	require.NoError(t, err)

	require.NotNil(t, capturedBody, "the stand-in server must have received a request body")
	_, hasProducts := capturedBody["products"]
	require.False(t, hasProducts, "update mode must NOT send `products` — Plaid's spec says it "+
		"should be omitted unless adding Income or Assets, which we do not do here")

	optionalProducts, hasOptionalProducts := capturedBody["optional_products"]
	require.True(t, hasOptionalProducts, "optional_products carries no update-mode restriction in "+
		"Plaid's spec and must still be sent — this proves the fix removed only `products`, not "+
		"both fields")
	require.ElementsMatch(t, []any{"liabilities", "investments"}, optionalProducts,
		"optional_products must still list liabilities and investments, unchanged")
}
