package middleware_test

// #930: Read-only enforcement middleware tests
// Ensures inactive/cancelled subscriptions can only perform GET/HEAD requests.
// #898: Adds past_due blocked-case tests and narrowed-whitelist regression tests.

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humachi"
	"github.com/go-chi/chi/v5"
	"github.com/maxnussbaum/BoostedBudget/internal/huma/inputs"
	humamiddleware "github.com/maxnussbaum/BoostedBudget/internal/huma/middleware"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// setupReadOnlyTestAPI creates a test API with read-only middleware and simulated Clerk middleware.
// The simulated Clerk middleware sets subscription_status from the X-Test-Subscription-Status header.
func setupReadOnlyTestAPI(t *testing.T) (*httptest.Server, huma.API) {
	router := chi.NewRouter()
	api := humachi.New(router, huma.DefaultConfig("Test API", "1.0.0"))

	// Simulate Clerk middleware: set subscription_status from test header
	api.UseMiddleware(func(ctx huma.Context, next func(huma.Context)) {
		status := ctx.Header("X-Test-Subscription-Status")
		if status != "" {
			newCtx := context.WithValue(ctx.Context(), inputs.SubscriptionStatusKey, status)
			next(huma.WithContext(ctx, newCtx))
		} else {
			next(ctx)
		}
	})

	// Add the read-only middleware
	api.UseMiddleware(humamiddleware.NewReadOnlyMiddleware())

	server := httptest.NewServer(router)
	t.Cleanup(server.Close)

	// Register test endpoints
	// Protected POST endpoint
	huma.Register(api, huma.Operation{
		OperationID: "create-something",
		Method:      http.MethodPost,
		Path:        "/api/v1/transactions",
		Security:    []map[string][]string{{"clerk": {}}},
	}, func(ctx context.Context, input *struct{}) (*struct {
		Body struct{ Message string `json:"message"` }
	}, error) {
		return &struct {
			Body struct{ Message string `json:"message"` }
		}{Body: struct{ Message string `json:"message"` }{Message: "created"}}, nil
	})

	// Protected GET endpoint
	huma.Register(api, huma.Operation{
		OperationID: "list-something",
		Method:      http.MethodGet,
		Path:        "/api/v1/transactions",
		Security:    []map[string][]string{{"clerk": {}}},
	}, func(ctx context.Context, input *struct{}) (*struct {
		Body struct{ Message string `json:"message"` }
	}, error) {
		return &struct {
			Body struct{ Message string `json:"message"` }
		}{Body: struct{ Message string `json:"message"` }{Message: "listed"}}, nil
	})

	// Whitelisted POST endpoint (user deletion flow must work even when inactive)
	huma.Register(api, huma.Operation{
		OperationID: "delete-user",
		Method:      http.MethodPost,
		Path:        "/api/v1/user/delete",
		Security:    []map[string][]string{{"clerk": {}}},
	}, func(ctx context.Context, input *struct{}) (*struct {
		Body struct{ Message string `json:"message"` }
	}, error) {
		return &struct {
			Body struct{ Message string `json:"message"` }
		}{Body: struct{ Message string `json:"message"` }{Message: "deleted"}}, nil
	})

	// Public (no security) POST endpoint — should not be gated by read-only middleware
	huma.Register(api, huma.Operation{
		OperationID: "public-webhook",
		Method:      http.MethodPost,
		Path:        "/api/v1/plaid/webhook",
	}, func(ctx context.Context, input *struct{}) (*struct {
		Body struct{ Message string `json:"message"` }
	}, error) {
		return &struct {
			Body struct{ Message string `json:"message"` }
		}{Body: struct{ Message string `json:"message"` }{Message: "webhook received"}}, nil
	})

	// Subscription checkout whitelist — resubscription must work even for cancelled/inactive users
	huma.Register(api, huma.Operation{
		OperationID: "subscription-checkout",
		Method:      http.MethodPost,
		Path:        "/api/v1/subscription/checkout",
		Security:    []map[string][]string{{"clerk": {}}},
	}, func(ctx context.Context, input *struct{}) (*struct {
		Body struct{ Message string `json:"message"` }
	}, error) {
		return &struct {
			Body struct{ Message string `json:"message"` }
		}{Body: struct{ Message string `json:"message"` }{Message: "checkout"}}, nil
	})

	// Subscription cancel whitelist — intentional cancel must work even for locked users
	huma.Register(api, huma.Operation{
		OperationID: "subscription-cancel",
		Method:      http.MethodPost,
		Path:        "/api/v1/subscription/cancel",
		Security:    []map[string][]string{{"clerk": {}}},
	}, func(ctx context.Context, input *struct{}) (*struct {
		Body struct{ Message string `json:"message"` }
	}, error) {
		return &struct {
			Body struct{ Message string `json:"message"` }
		}{Body: struct{ Message string `json:"message"` }{Message: "cancelled"}}, nil
	})

	// Subscription preview-update — plan mutation, must be BLOCKED for locked users
	huma.Register(api, huma.Operation{
		OperationID: "subscription-preview-update",
		Method:      http.MethodPost,
		Path:        "/api/v1/subscription/preview-update",
		Security:    []map[string][]string{{"clerk": {}}},
	}, func(ctx context.Context, input *struct{}) (*struct {
		Body struct{ Message string `json:"message"` }
	}, error) {
		return &struct {
			Body struct{ Message string `json:"message"` }
		}{Body: struct{ Message string `json:"message"` }{Message: "preview-update"}}, nil
	})

	// Subscription update — plan mutation, must be BLOCKED for locked users
	huma.Register(api, huma.Operation{
		OperationID: "subscription-update",
		Method:      http.MethodPost,
		Path:        "/api/v1/subscription/update",
		Security:    []map[string][]string{{"clerk": {}}},
	}, func(ctx context.Context, input *struct{}) (*struct {
		Body struct{ Message string `json:"message"` }
	}, error) {
		return &struct {
			Body struct{ Message string `json:"message"` }
		}{Body: struct{ Message string `json:"message"` }{Message: "updated"}}, nil
	})

	// Prefix-bypass test: a hypothetical route that shares a prefix with a whitelisted path.
	// Must be BLOCKED — exact-match hardening prevents /checkout-malicious from inheriting
	// the /checkout whitelist entry via strings.HasPrefix.
	huma.Register(api, huma.Operation{
		OperationID: "subscription-checkout-malicious",
		Method:      http.MethodPost,
		Path:        "/api/v1/subscription/checkout-malicious",
		Security:    []map[string][]string{{"clerk": {}}},
	}, func(ctx context.Context, input *struct{}) (*struct {
		Body struct{ Message string `json:"message"` }
	}, error) {
		return &struct {
			Body struct{ Message string `json:"message"` }
		}{Body: struct{ Message string `json:"message"` }{Message: "bypassed"}}, nil
	})

	return server, api
}

func doRequest(t *testing.T, server *httptest.Server, method, path, subscriptionStatus string) *http.Response {
	t.Helper()
	req, err := http.NewRequest(method, server.URL+path, nil)
	require.NoError(t, err)
	if subscriptionStatus != "" {
		req.Header.Set("X-Test-Subscription-Status", subscriptionStatus)
	}
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	return resp
}

// TestReadOnlyMiddleware_GET_Inactive_Allowed tests that GET requests are allowed for inactive users.
func TestReadOnlyMiddleware_GET_Inactive_Allowed(t *testing.T) {
	server, _ := setupReadOnlyTestAPI(t)

	resp := doRequest(t, server, http.MethodGet, "/api/v1/transactions", "inactive")
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
}

// TestReadOnlyMiddleware_POST_Inactive_Blocked tests that POST requests are blocked for inactive users.
func TestReadOnlyMiddleware_POST_Inactive_Blocked(t *testing.T) {
	server, _ := setupReadOnlyTestAPI(t)

	resp := doRequest(t, server, http.MethodPost, "/api/v1/transactions", "inactive")
	defer resp.Body.Close()

	assert.Equal(t, http.StatusForbidden, resp.StatusCode)
}

// TestReadOnlyMiddleware_POST_Active_Allowed tests that POST requests are allowed for active users.
func TestReadOnlyMiddleware_POST_Active_Allowed(t *testing.T) {
	server, _ := setupReadOnlyTestAPI(t)

	resp := doRequest(t, server, http.MethodPost, "/api/v1/transactions", "active")
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
}

// TestReadOnlyMiddleware_POST_Cancelled_Blocked tests that POST requests are blocked for cancelled users.
func TestReadOnlyMiddleware_POST_Cancelled_Blocked(t *testing.T) {
	server, _ := setupReadOnlyTestAPI(t)

	resp := doRequest(t, server, http.MethodPost, "/api/v1/transactions", "cancelled")
	defer resp.Body.Close()

	assert.Equal(t, http.StatusForbidden, resp.StatusCode)
}

// TestReadOnlyMiddleware_GET_Cancelled_Allowed tests that GET requests are allowed for cancelled users.
func TestReadOnlyMiddleware_GET_Cancelled_Allowed(t *testing.T) {
	server, _ := setupReadOnlyTestAPI(t)

	resp := doRequest(t, server, http.MethodGet, "/api/v1/transactions", "cancelled")
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
}

// TestReadOnlyMiddleware_POST_WhitelistedPath_Inactive_Allowed tests that whitelisted paths
// (e.g. user deletion) are allowed even for inactive users.
func TestReadOnlyMiddleware_POST_WhitelistedPath_Inactive_Allowed(t *testing.T) {
	server, _ := setupReadOnlyTestAPI(t)

	resp := doRequest(t, server, http.MethodPost, "/api/v1/user/delete", "inactive")
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
}

// TestReadOnlyMiddleware_POST_WhitelistedPath_Cancelled_Allowed tests that subscription checkout
// is allowed even for cancelled users (so they can resubscribe).
func TestReadOnlyMiddleware_POST_WhitelistedPath_Cancelled_Allowed(t *testing.T) {
	server, _ := setupReadOnlyTestAPI(t)

	resp := doRequest(t, server, http.MethodPost, "/api/v1/subscription/checkout", "cancelled")
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
}

// TestReadOnlyMiddleware_PublicRoute_NoSecurity_Passthrough tests that public routes (no security)
// are not affected by the read-only middleware.
func TestReadOnlyMiddleware_PublicRoute_NoSecurity_Passthrough(t *testing.T) {
	server, _ := setupReadOnlyTestAPI(t)

	// Plaid webhook has no Security annotation — should pass through even with inactive status
	resp := doRequest(t, server, http.MethodPost, "/api/v1/plaid/webhook", "inactive")
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
}

// TestReadOnlyMiddleware_POST_NoSubscriptionStatus_Active_Passthrough tests that requests
// with no subscription status in context are treated as non-read-only (pass through).
func TestReadOnlyMiddleware_POST_NoSubscriptionStatus_Active_Passthrough(t *testing.T) {
	server, _ := setupReadOnlyTestAPI(t)

	// No subscription status header — middleware should not block
	resp := doRequest(t, server, http.MethodPost, "/api/v1/transactions", "")
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
}

// --- #898: past_due tests ---

// TestReadOnlyMiddleware_POST_PastDue_Blocked tests that general POST mutations are blocked
// for users with past_due subscription status.
func TestReadOnlyMiddleware_POST_PastDue_Blocked(t *testing.T) {
	server, _ := setupReadOnlyTestAPI(t)

	resp := doRequest(t, server, http.MethodPost, "/api/v1/transactions", "past_due")
	defer resp.Body.Close()

	assert.Equal(t, http.StatusForbidden, resp.StatusCode)
}

// TestReadOnlyMiddleware_GET_PastDue_Allowed tests that GET requests are allowed for past_due users.
func TestReadOnlyMiddleware_GET_PastDue_Allowed(t *testing.T) {
	server, _ := setupReadOnlyTestAPI(t)

	resp := doRequest(t, server, http.MethodGet, "/api/v1/transactions", "past_due")
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
}

// TestReadOnlyMiddleware_POST_Checkout_PastDue_Allowed tests that resubscription checkout
// is allowed for past_due users.
func TestReadOnlyMiddleware_POST_Checkout_PastDue_Allowed(t *testing.T) {
	server, _ := setupReadOnlyTestAPI(t)

	resp := doRequest(t, server, http.MethodPost, "/api/v1/subscription/checkout", "past_due")
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
}

// TestReadOnlyMiddleware_POST_Cancel_PastDue_Allowed tests that intentional cancellation
// is allowed for past_due users (so they can cancel a subscription in arrears).
func TestReadOnlyMiddleware_POST_Cancel_PastDue_Allowed(t *testing.T) {
	server, _ := setupReadOnlyTestAPI(t)

	resp := doRequest(t, server, http.MethodPost, "/api/v1/subscription/cancel", "past_due")
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
}

// TestReadOnlyMiddleware_POST_PreviewUpdate_PastDue_Blocked tests that plan-change preview
// is blocked for past_due users (must resolve payment first).
func TestReadOnlyMiddleware_POST_PreviewUpdate_PastDue_Blocked(t *testing.T) {
	server, _ := setupReadOnlyTestAPI(t)

	resp := doRequest(t, server, http.MethodPost, "/api/v1/subscription/preview-update", "past_due")
	defer resp.Body.Close()

	assert.Equal(t, http.StatusForbidden, resp.StatusCode)
}

// TestReadOnlyMiddleware_POST_Update_PastDue_Blocked tests that plan-change mutations
// are blocked for past_due users (must resolve payment first).
func TestReadOnlyMiddleware_POST_Update_PastDue_Blocked(t *testing.T) {
	server, _ := setupReadOnlyTestAPI(t)

	resp := doRequest(t, server, http.MethodPost, "/api/v1/subscription/update", "past_due")
	defer resp.Body.Close()

	assert.Equal(t, http.StatusForbidden, resp.StatusCode)
}

// TestReadOnlyMiddleware_POST_PreviewUpdate_Inactive_Blocked tests that plan-change preview
// is blocked for inactive users (must resubscribe first).
func TestReadOnlyMiddleware_POST_PreviewUpdate_Inactive_Blocked(t *testing.T) {
	server, _ := setupReadOnlyTestAPI(t)

	resp := doRequest(t, server, http.MethodPost, "/api/v1/subscription/preview-update", "inactive")
	defer resp.Body.Close()

	assert.Equal(t, http.StatusForbidden, resp.StatusCode)
}

// TestReadOnlyMiddleware_POST_Update_Inactive_Blocked tests that plan-change mutations
// are blocked for inactive users (must resubscribe first).
func TestReadOnlyMiddleware_POST_Update_Inactive_Blocked(t *testing.T) {
	server, _ := setupReadOnlyTestAPI(t)

	resp := doRequest(t, server, http.MethodPost, "/api/v1/subscription/update", "inactive")
	defer resp.Body.Close()

	assert.Equal(t, http.StatusForbidden, resp.StatusCode)
}

// TestReadOnlyMiddleware_POST_PreviewUpdate_Cancelled_Blocked tests that plan-change preview
// is blocked for cancelled users (must resubscribe first).
func TestReadOnlyMiddleware_POST_PreviewUpdate_Cancelled_Blocked(t *testing.T) {
	server, _ := setupReadOnlyTestAPI(t)

	resp := doRequest(t, server, http.MethodPost, "/api/v1/subscription/preview-update", "cancelled")
	defer resp.Body.Close()

	assert.Equal(t, http.StatusForbidden, resp.StatusCode)
}

// TestReadOnlyMiddleware_POST_Update_Cancelled_Blocked tests that plan-change mutations
// are blocked for cancelled users (must resubscribe first).
func TestReadOnlyMiddleware_POST_Update_Cancelled_Blocked(t *testing.T) {
	server, _ := setupReadOnlyTestAPI(t)

	resp := doRequest(t, server, http.MethodPost, "/api/v1/subscription/update", "cancelled")
	defer resp.Body.Close()

	assert.Equal(t, http.StatusForbidden, resp.StatusCode)
}

// TestReadOnlyMiddleware_POST_Cancel_Inactive_Allowed tests that cancel is allowed for
// inactive users (regression: narrowed whitelist must still pass cancel through).
func TestReadOnlyMiddleware_POST_Cancel_Inactive_Allowed(t *testing.T) {
	server, _ := setupReadOnlyTestAPI(t)

	resp := doRequest(t, server, http.MethodPost, "/api/v1/subscription/cancel", "inactive")
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
}

// TestReadOnlyMiddleware_POST_Cancel_Cancelled_Allowed tests that cancel is allowed for
// cancelled users (regression: narrowed whitelist must still pass cancel through).
func TestReadOnlyMiddleware_POST_Cancel_Cancelled_Allowed(t *testing.T) {
	server, _ := setupReadOnlyTestAPI(t)

	resp := doRequest(t, server, http.MethodPost, "/api/v1/subscription/cancel", "cancelled")
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
}

// TestReadOnlyMiddleware_POST_Checkout_Inactive_Allowed tests that checkout is allowed for
// inactive users (regression: narrowed whitelist must still pass checkout through).
func TestReadOnlyMiddleware_POST_Checkout_Inactive_Allowed(t *testing.T) {
	server, _ := setupReadOnlyTestAPI(t)

	resp := doRequest(t, server, http.MethodPost, "/api/v1/subscription/checkout", "inactive")
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
}

// TestReadOnlyMiddleware_POST_CheckoutPrefixBypass_Blocked proves that a route sharing only a
// prefix with the /checkout whitelist entry is NOT whitelisted.
// This is the regression test for the HasPrefix→exact-match hardening (GAP4b).
// Without exact-match, strings.HasPrefix("/api/v1/subscription/checkout-malicious", "/api/v1/subscription/checkout")
// = true, so the route would pass through incorrectly.
func TestReadOnlyMiddleware_POST_CheckoutPrefixBypass_Blocked(t *testing.T) {
	server, _ := setupReadOnlyTestAPI(t)

	resp := doRequest(t, server, http.MethodPost, "/api/v1/subscription/checkout-malicious", "past_due")
	defer resp.Body.Close()

	assert.Equal(t, http.StatusForbidden, resp.StatusCode,
		"a route that merely shares a prefix with /checkout must not inherit its whitelist entry")
}
