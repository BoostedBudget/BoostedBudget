package middleware_test

// #1432: regression guard against readOnlyPrefixAllowedPaths drifting into dead strings again.
//
// Before this fix, "/api/v1/paddle/webhook" and "/api/v1/plaid/webhook" were whitelisted in
// read_only.go but neither route existed — the real routes are /webhooks/paddle and
// /webhooks/plaid (huma/operations/paddle_webhook.go, plaid_webhook.go). Both entries were dead
// strings that could never match a live route, yet they read as the thing protecting webhook
// delivery. This test builds the SAME route table OpenAPI spec generation uses
// (operations.RegisterAllRoutesForSpec — see scripts/generate-openapi/main.go and
// operations/register_test.go's TestRegisterAllRoutesForSpec_DoesNotPanic) and asserts every
// readOnlyPrefixAllowedPaths entry matches at least one ACTUALLY REGISTERED route.
//
// This test lives in the external middleware_test package, not an internal middleware test file,
// because operations imports internal/huma/middleware (for admin RBAC checks) — an internal test
// file augmenting package middleware cannot import operations without an import cycle. To read
// the real (unexported) readOnlyPrefixAllowedPaths slice without hand-copying a second list that
// could itself drift out of sync, read_only.go exports
// ReadOnlyPrefixAllowedPathsForTesting() for exactly this test.

import (
	"strings"
	"testing"

	"github.com/danielgtaylor/huma/v2/humatest"
	humamiddleware "github.com/maxnussbaum/BoostedBudget/internal/huma/middleware"
	"github.com/maxnussbaum/BoostedBudget/internal/huma/operations"
)

// TestReadOnlyPrefixAllowedPaths_CorrespondToRegisteredRoutes guards against #1432: every entry in
// readOnlyPrefixAllowedPaths must correspond to at least one route the server actually registers,
// or the whitelist entry is a dead string that reads as protection but protects nothing.
func TestReadOnlyPrefixAllowedPaths_CorrespondToRegisteredRoutes(t *testing.T) {
	_, api := humatest.New(t)
	operations.RegisterAllRoutesForSpec(api)

	registeredPaths := api.OpenAPI().Paths
	if len(registeredPaths) == 0 {
		t.Fatal("RegisterAllRoutesForSpec registered zero routes — did the registration helper move? " +
			"(this test cannot prove anything against an empty route table)")
	}

	for _, prefix := range humamiddleware.ReadOnlyPrefixAllowedPathsForTesting() {
		matched := false
		for path := range registeredPaths {
			if strings.HasPrefix(path, prefix) {
				matched = true
				break
			}
		}
		if !matched {
			t.Errorf("readOnlyPrefixAllowedPaths entry %q does not match any registered route — "+
				"this whitelist entry is a dead string (see #1432, where /api/v1/paddle/webhook and "+
				"/api/v1/plaid/webhook were exactly this: whitelisted but never registered)", prefix)
		}
	}
}
