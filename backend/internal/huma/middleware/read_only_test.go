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

	// Simulate Clerk middleware: set subscription_status (and user_id) from test headers
	api.UseMiddleware(func(ctx huma.Context, next func(huma.Context)) {
		newCtx := ctx.Context()
		if status := ctx.Header("X-Test-Subscription-Status"); status != "" {
			newCtx = context.WithValue(newCtx, inputs.SubscriptionStatusKey, status)
		}
		if userID := ctx.Header("X-Test-User-Id"); userID != "" {
			newCtx = context.WithValue(newCtx, inputs.UserIDKey, userID)
		}
		next(huma.WithContext(ctx, newCtx))
	})

	// Add the read-only middleware. nil checker preserves pre-#1367 behavior for all
	// existing tests in this file — see setupReadOnlyTestAPIWithHouseholdChecker for the
	// household-aware variant.
	api.UseMiddleware(humamiddleware.NewReadOnlyMiddleware(nil))

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
		}{Message: "created"}}, nil
	})

	// Protected GET endpoint
	huma.Register(api, huma.Operation{
		OperationID: "list-something",
		Method:      http.MethodGet,
		Path:        "/api/v1/transactions",
		Security:    []map[string][]string{{"clerk": {}}},
	}, func(ctx context.Context, input *struct{}) (*struct {
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
		}{Message: "listed"}}, nil
	})

	// Whitelisted POST endpoint (user deletion flow must work even when inactive)
	huma.Register(api, huma.Operation{
		OperationID: "delete-user",
		Method:      http.MethodPost,
		Path:        "/api/v1/user/delete",
		Security:    []map[string][]string{{"clerk": {}}},
	}, func(ctx context.Context, input *struct{}) (*struct {
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
		}{Message: "deleted"}}, nil
	})

	// Public (no security) POST endpoint — should not be gated by read-only middleware
	huma.Register(api, huma.Operation{
		OperationID: "public-webhook",
		Method:      http.MethodPost,
		Path:        "/api/v1/plaid/webhook",
	}, func(ctx context.Context, input *struct{}) (*struct {
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
		}{Message: "webhook received"}}, nil
	})

	// Subscription checkout whitelist — resubscription must work even for cancelled/inactive users
	huma.Register(api, huma.Operation{
		OperationID: "subscription-checkout",
		Method:      http.MethodPost,
		Path:        "/api/v1/subscription/checkout",
		Security:    []map[string][]string{{"clerk": {}}},
	}, func(ctx context.Context, input *struct{}) (*struct {
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
		}{Message: "checkout"}}, nil
	})

	// Subscription cancel whitelist — intentional cancel must work even for locked users
	huma.Register(api, huma.Operation{
		OperationID: "subscription-cancel",
		Method:      http.MethodPost,
		Path:        "/api/v1/subscription/cancel",
		Security:    []map[string][]string{{"clerk": {}}},
	}, func(ctx context.Context, input *struct{}) (*struct {
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
		}{Message: "cancelled"}}, nil
	})

	// Subscription preview-update — plan mutation, must be BLOCKED for locked users
	huma.Register(api, huma.Operation{
		OperationID: "subscription-preview-update",
		Method:      http.MethodPost,
		Path:        "/api/v1/subscription/preview-update",
		Security:    []map[string][]string{{"clerk": {}}},
	}, func(ctx context.Context, input *struct{}) (*struct {
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
		}{Message: "preview-update"}}, nil
	})

	// Subscription update — plan mutation, must be BLOCKED for locked users
	huma.Register(api, huma.Operation{
		OperationID: "subscription-update",
		Method:      http.MethodPost,
		Path:        "/api/v1/subscription/update",
		Security:    []map[string][]string{{"clerk": {}}},
	}, func(ctx context.Context, input *struct{}) (*struct {
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
		}{Message: "updated"}}, nil
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
		}{Message: "bypassed"}}, nil
	})

	// #1367 — Phase 3: accept/decline must work even for a lapsed user rejoining a
	// household to regain access.
	huma.Register(api, huma.Operation{
		OperationID: "household-invite-accept",
		Method:      http.MethodPost,
		Path:        "/api/v1/households/invites/tok123/accept",
		Security:    []map[string][]string{{"clerk": {}}},
	}, func(ctx context.Context, input *struct{}) (*struct {
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
		}{Message: "accepted"}}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "household-invite-decline",
		Method:      http.MethodPost,
		Path:        "/api/v1/households/invites/tok123/decline",
		Security:    []map[string][]string{{"clerk": {}}},
	}, func(ctx context.Context, input *struct{}) (*struct {
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
		}{Message: "declined"}}, nil
	})

	// Non-whitelisted household mutation — must still be BLOCKED for locked users
	// (only accept/decline are whitelisted, not e.g. renaming the household).
	huma.Register(api, huma.Operation{
		OperationID: "household-rename",
		Method:      http.MethodPatch,
		Path:        "/api/v1/households/me",
		Security:    []map[string][]string{{"clerk": {}}},
	}, func(ctx context.Context, input *struct{}) (*struct {
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
		}{Message: "renamed"}}, nil
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

// ─── #1367 — Phase 3: household invite accept/decline whitelist ─────────────

func TestReadOnlyMiddleware_POST_HouseholdAccept_Inactive_Allowed(t *testing.T) {
	server, _ := setupReadOnlyTestAPI(t)

	resp := doRequest(t, server, http.MethodPost, "/api/v1/households/invites/tok123/accept", "inactive")
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode, "a lapsed user must be able to join a household to regain access")
}

func TestReadOnlyMiddleware_POST_HouseholdDecline_Cancelled_Allowed(t *testing.T) {
	server, _ := setupReadOnlyTestAPI(t)

	resp := doRequest(t, server, http.MethodPost, "/api/v1/households/invites/tok123/decline", "cancelled")
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
}

func TestReadOnlyMiddleware_POST_HouseholdRename_Inactive_Blocked(t *testing.T) {
	server, _ := setupReadOnlyTestAPI(t)

	resp := doRequest(t, server, http.MethodPatch, "/api/v1/households/me", "inactive")
	defer resp.Body.Close()

	assert.Equal(t, http.StatusForbidden, resp.StatusCode,
		"only accept/decline are whitelisted — other household mutations stay blocked while locked")
}

// ─── #1367 — Phase 3: household-aware read-only enforcement ─────────────────

// mockHouseholdCoverageChecker is a minimal test double for
// humamiddleware.HouseholdCoverageChecker.
type mockHouseholdCoverageChecker struct {
	covered map[string]bool
}

func (m *mockHouseholdCoverageChecker) IsCoveredByHousehold(ctx context.Context, userID string) (bool, error) {
	return m.covered[userID], nil
}

// setupReadOnlyTestAPIWithHouseholdChecker mirrors setupReadOnlyTestAPI but wires a
// real (test-double) HouseholdCoverageChecker so the middleware's household-aware
// branch can be exercised. Kept separate from setupReadOnlyTestAPI so the 20+
// pre-#1367 tests above are unaffected.
func setupReadOnlyTestAPIWithHouseholdChecker(t *testing.T, checker humamiddleware.HouseholdCoverageChecker) (*httptest.Server, huma.API) {
	router := chi.NewRouter()
	api := humachi.New(router, huma.DefaultConfig("Test API", "1.0.0"))

	api.UseMiddleware(func(ctx huma.Context, next func(huma.Context)) {
		newCtx := ctx.Context()
		if status := ctx.Header("X-Test-Subscription-Status"); status != "" {
			newCtx = context.WithValue(newCtx, inputs.SubscriptionStatusKey, status)
		}
		if userID := ctx.Header("X-Test-User-Id"); userID != "" {
			newCtx = context.WithValue(newCtx, inputs.UserIDKey, userID)
		}
		next(huma.WithContext(ctx, newCtx))
	})
	api.UseMiddleware(humamiddleware.NewReadOnlyMiddleware(checker))

	server := httptest.NewServer(router)
	t.Cleanup(server.Close)

	huma.Register(api, huma.Operation{
		OperationID: "create-something-covered",
		Method:      http.MethodPost,
		Path:        "/api/v1/transactions",
		Security:    []map[string][]string{{"clerk": {}}},
	}, func(ctx context.Context, input *struct{}) (*struct {
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
		}{Message: "created"}}, nil
	})

	return server, api
}

func doRequestWithUser(t *testing.T, server *httptest.Server, method, path, subscriptionStatus, userID string) *http.Response {
	t.Helper()
	req, err := http.NewRequest(method, server.URL+path, nil)
	require.NoError(t, err)
	if subscriptionStatus != "" {
		req.Header.Set("X-Test-Subscription-Status", subscriptionStatus)
	}
	if userID != "" {
		req.Header.Set("X-Test-User-Id", userID)
	}
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	return resp
}

func TestReadOnlyMiddleware_POST_ReadOnlyStatus_CoveredByHousehold_Allowed(t *testing.T) {
	checker := &mockHouseholdCoverageChecker{covered: map[string]bool{"member-1": true}}
	server, _ := setupReadOnlyTestAPIWithHouseholdChecker(t, checker)

	resp := doRequestWithUser(t, server, http.MethodPost, "/api/v1/transactions", "inactive", "member-1")
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode,
		"a covered household member must not be locked out by their own stale read-only status")
}

func TestReadOnlyMiddleware_POST_ReadOnlyStatus_NotCoveredByHousehold_Blocked(t *testing.T) {
	checker := &mockHouseholdCoverageChecker{covered: map[string]bool{}}
	server, _ := setupReadOnlyTestAPIWithHouseholdChecker(t, checker)

	resp := doRequestWithUser(t, server, http.MethodPost, "/api/v1/transactions", "inactive", "solo-user-1")
	defer resp.Body.Close()

	assert.Equal(t, http.StatusForbidden, resp.StatusCode)
}

func TestReadOnlyMiddleware_POST_ReadOnlyStatus_NilChecker_Blocked(t *testing.T) {
	// nil checker (household service unavailable) must fail closed to the pre-#1367
	// behavior, not panic and not silently allow everyone through.
	server, _ := setupReadOnlyTestAPIWithHouseholdChecker(t, nil)

	resp := doRequestWithUser(t, server, http.MethodPost, "/api/v1/transactions", "inactive", "member-1")
	defer resp.Body.Close()

	assert.Equal(t, http.StatusForbidden, resp.StatusCode)
}
