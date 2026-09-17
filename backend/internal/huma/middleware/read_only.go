package middleware

// #930: Read-only enforcement middleware.
// Blocks mutating HTTP methods (POST/PUT/PATCH/DELETE) for users whose EFFECTIVE subscription
// status (override-aware; Issue #1413) is inactive, cancelled, or past_due.
// GET and HEAD are always allowed (read-only access).
// Certain paths (resubscribe, self-service) are explicitly whitelisted to allow full access.
// #898: Narrowed subscription whitelist — only checkout and cancel are allowed while locked.
// #1413: When the effective status could NOT be resolved (a DB error during the Clerk
// middleware's lookup), mutating requests are failed closed (503) rather than silently
// allowed through — see the SubscriptionStatusUnresolvedKey handling below.

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"github.com/danielgtaylor/huma/v2"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/huma/inputs"
)

// HouseholdCoverageChecker reports whether userID currently has active free coverage
// under a household plan (i.e. would resolve to source=household). Satisfied
// structurally by *services.HouseholdEntitlementService. #1367 — Phase 3.
type HouseholdCoverageChecker interface {
	IsCoveredByHousehold(ctx context.Context, userID string) (bool, error)
}

// readOnlyExactAllowedPaths lists paths that are whitelisted by exact match.
// Used for the subscription recovery/self-service routes where prefix matching would
// accidentally admit future mutation routes that share the same prefix
// (e.g. /subscription/checkout-session would wrongly inherit /subscription/checkout).
var readOnlyExactAllowedPaths = map[string]bool{
	"/api/v1/subscription/checkout": true, // resubscribe — locked users must be able to restart
	"/api/v1/subscription/cancel":   true, // intentional cancel — locked users may cancel in arrears
}

// readOnlyMethodExactAllowedPaths lists (method, path) pairs that are whitelisted only for
// that specific HTTP method — unlike readOnlyExactAllowedPaths (allowed regardless of
// method), these paths are also used by OTHER, still-gated mutating methods (e.g. PATCH
// /households/me renames the household and must stay blocked). #1367 — Phase 3 Round 2
// (GT-1): disbanding a household and leaving/removing a member are de-escalation actions —
// like /subscription/cancel — so a lapsed owner can disband, and a member under a lapsed
// owner can self-leave to regain access via their own subscription.
var readOnlyMethodExactAllowedPaths = map[string]map[string]bool{
	http.MethodPost: {
		"/api/v1/subscription/checkout-intents":  true, // admission performs its own fresh billing checks
		"/api/v1/subscription/authority/refresh": true, // provider confirmation is recovery, not paid-feature use
	},
	http.MethodDelete: {
		"/api/v1/subscription/checkout-intents/{intent_id}": true, // closes only a coordination lease
		"/api/v1/households/me":                             true, // disband
		"/api/v1/households/me/members/{userId}":            true, // self-leave / owner-removes
	},
}

// readOnlyPathSuffixAllowed reports whether path is a household invite accept/decline
// route (#1367 — Phase 3). These have a dynamic {token} segment in the middle
// (/api/v1/households/invites/{token}/accept|decline), so neither exact-match nor
// simple prefix-match applies; suffix-match on the fixed tail is exact enough given the
// fixed "/api/v1/households/invites/" prefix requirement below — a lapsed user must be
// able to join (or decline) a household to regain access.
func readOnlyPathSuffixAllowed(path string) bool {
	const invitesPrefix = "/api/v1/households/invites/"
	if !strings.HasPrefix(path, invitesPrefix) {
		return false
	}
	return strings.HasSuffix(path, "/accept") || strings.HasSuffix(path, "/decline")
}

// readOnlyPrefixAllowedPaths lists path prefixes that are always allowed regardless of subscription
// status. Entries here intentionally use prefix semantics — e.g. /api/v1/user/delete also covers
// the deletion-cancel token route (/api/v1/user/delete/cancel/{token}), and /api/v1/admin/ covers
// all admin sub-routes. Subscription routes are NOT listed here to prevent word-boundary bypass;
// they are in readOnlyExactAllowedPaths above.
// #1432: the webhook entries here are defence-in-depth, not the thing that actually protects
// Paddle/Plaid webhook delivery today. Both /webhooks/paddle and /webhooks/plaid are registered
// with NO Security annotation (validated instead by Paddle HMAC / Plaid JWT respectively), so
// NewReadOnlyMiddleware already short-circuits them as public routes before this whitelist is ever
// consulted (see the `len(ctx.Operation().Security) == 0` check below). Correcting these entries
// to the real paths — rather than deleting them, since they were previously dead strings that
// could never match ANY route (/api/v1/paddle/webhook and /api/v1/plaid/webhook do not exist) —
// is the honest choice: if a future change ever added a Security annotation to a webhook route,
// a corrected whitelist would still protect it, while a deleted one would not.
// TestReadOnlyPrefixAllowedPaths_CorrespondToRegisteredRoutes (read_only_route_coverage_test.go)
// asserts every entry here matches an actually-registered route, so this cannot silently drift
// into dead strings again.
var readOnlyPrefixAllowedPaths = []string{
	"/api/v1/user/delete",
	"/api/v1/user/profile",
	"/api/v1/user/privacy",
	"/webhooks/paddle",
	"/webhooks/plaid",
	"/api/v1/admin/",
}

// ReadOnlyPrefixAllowedPathsForTesting exposes readOnlyPrefixAllowedPaths to
// read_only_route_coverage_test.go (external middleware_test package). That test also needs
// operations.RegisterAllRoutesForSpec to build the real route table, and operations imports this
// middleware package (for admin RBAC checks) — so the coverage test cannot live in an internal
// `package middleware` test file without an import cycle. Hand-copying this slice into the
// external test instead would defeat the point: the two copies could drift apart exactly like the
// dead #1432 strings did. This accessor keeps the test reading the real, live slice.
func ReadOnlyPrefixAllowedPathsForTesting() []string {
	return readOnlyPrefixAllowedPaths
}

// NewReadOnlyMiddleware creates a Huma middleware that enforces read-only access for
// inactive, cancelled, and past_due subscriptions. It blocks POST, PUT, PATCH, and DELETE for
// users whose EFFECTIVE subscription status is read-only (see domain.IsReadOnlySubscription),
// while allowing GET and HEAD.
//
// Public routes (no Security annotation) are always passed through.
// Whitelisted paths are always allowed to support resubscription and self-service flows.
//
// checker is optional (nil-safe) — when set, a caller whose raw subscription status is
// read-only but who is actively covered by a household plan (#1367 — Phase 3) is treated
// as having access rather than being blocked. nil fails closed to pre-#1367 behavior.
//
// #1413: three context states are distinguished for mutating requests:
//  1. EffectiveSubscriptionStatusKey present → evaluated as today.
//  2. SubscriptionStatusUnresolvedKey true (status lookup failed, e.g. a DB error) → the
//     request is failed closed with 503 (after the same whitelist checks below), never
//     silently allowed through.
//  3. Neither key set (no status reader configured at all) → allowed through, exactly as
//     before #1413 — this is NOT the same as case 2 and must not be blocked.
func NewReadOnlyMiddleware(checker HouseholdCoverageChecker) func(ctx huma.Context, next func(huma.Context)) {
	return func(ctx huma.Context, next func(huma.Context)) {
		// Skip middleware for public routes (no Security annotation)
		if len(ctx.Operation().Security) == 0 {
			next(ctx)
			return
		}

		// GET and HEAD are always allowed — read-only access is the point
		method := ctx.Operation().Method
		if method == http.MethodGet || method == http.MethodHead {
			next(ctx)
			return
		}
		if IsReadEquivalentOperation(ctx.Operation()) {
			next(ctx)
			return
		}

		// For mutating methods, check the effective subscription status.
		effectiveStatus, ok := ctx.Context().Value(inputs.EffectiveSubscriptionStatusKey).(string)
		unresolved, _ := ctx.Context().Value(inputs.SubscriptionStatusUnresolvedKey).(bool)

		if ok && effectiveStatus != "" {
			// Only restrict inactive and cancelled subscriptions
			status := domain.SubscriptionStatus(effectiveStatus)
			if !status.IsReadOnlySubscription() {
				next(ctx)
				return
			}
		} else if !unresolved {
			// No effective status in context and the lookup was never attempted/never
			// failed (no status reader configured) — cannot determine restriction, allow
			// through. This preserves pre-#1413 behavior exactly.
			next(ctx)
			return
		}
		// Falls through to the whitelist checks below in both remaining cases: the
		// effective status is read-only, OR it could not be resolved (unresolved == true).

		// Check if path is whitelisted — exact-match first, then method-scoped exact
		// match (GT-1), then prefix, then the household invite accept/decline suffix
		// pattern (#1367 — Phase 3).
		path := ctx.Operation().Path
		if readOnlyExactAllowedPaths[path] {
			next(ctx)
			return
		}
		if readOnlyMethodExactAllowedPaths[method][path] {
			next(ctx)
			return
		}
		for _, prefix := range readOnlyPrefixAllowedPaths {
			if strings.HasPrefix(path, prefix) {
				next(ctx)
				return
			}
		}
		if readOnlyPathSuffixAllowed(path) {
			next(ctx)
			return
		}

		// #1413: fail closed when the effective status could not be resolved (e.g. a DB
		// error during the Clerk middleware's lookup). This check MUST run after every
		// allow-list check above — blocking earlier would also block Paddle/Plaid webhooks,
		// all /api/v1/admin/ routes, and every subscription-recovery path during exactly
		// the kind of database blip this is meant to guard against. Distinguished from the
		// normal 403 lockout below: a 503 "please retry" is honest about an outage, whereas
		// "your subscription has ended" would be a lie.
		if unresolved {
			writeReadOnlyJSONError(ctx, http.StatusServiceUnavailable,
				"We're temporarily unable to verify your subscription status. Please retry shortly.")
			return
		}

		// #1367 — Phase 3: a covered household member's own subscription_status is never
		// mutated while covered, so it can itself be read-only (e.g. they joined while
		// already lapsed). Consult the effective entitlement before blocking — this is a
		// single extra lookup only on this already-narrow (read-only-status, mutating
		// method, non-whitelisted path) branch, not on every request.
		if checker != nil {
			if userID, ok := ctx.Context().Value(inputs.UserIDKey).(string); ok && userID != "" {
				covered, err := checker.IsCoveredByHousehold(ctx.Context(), userID)
				if err != nil {
					slog.Default().Warn("read-only middleware: household coverage check failed; failing closed", "user_id", userID, "error", err)
				} else if covered {
					next(ctx)
					return
				}
			}
		}

		// Block the request — subscription is read-only
		writeReadOnlyJSONError(ctx, http.StatusForbidden,
			"Your subscription has ended. Resubscribe at boostedbudget.com/pricing to restore full access.")
	}
}

// writeReadOnlyJSONError writes an RFC 9457 Problem Details error response.
func writeReadOnlyJSONError(ctx huma.Context, status int, message string) {
	ctx.SetStatus(status)
	ctx.SetHeader("Content-Type", "application/problem+json")
	response := fmt.Sprintf(`{"status":%d,"title":"%s","detail":"%s"}`, status, http.StatusText(status), message)
	_, _ = ctx.BodyWriter().Write([]byte(response))
}
