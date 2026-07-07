package middleware

// #930: Read-only enforcement middleware.
// Blocks mutating HTTP methods (POST/PUT/PATCH/DELETE) for users with inactive, cancelled, or past_due subscriptions.
// GET and HEAD are always allowed (read-only access).
// Certain paths (resubscribe, self-service) are explicitly whitelisted to allow full access.
// #898: Narrowed subscription whitelist — only checkout and cancel are allowed while locked.

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/danielgtaylor/huma/v2"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/huma/inputs"
)

// readOnlyExactAllowedPaths lists paths that are whitelisted by exact match.
// Used for the subscription recovery/self-service routes where prefix matching would
// accidentally admit future mutation routes that share the same prefix
// (e.g. /subscription/checkout-session would wrongly inherit /subscription/checkout).
var readOnlyExactAllowedPaths = map[string]bool{
	"/api/v1/subscription/checkout": true, // resubscribe — locked users must be able to restart
	"/api/v1/subscription/cancel":   true, // intentional cancel — locked users may cancel in arrears
}

// readOnlyPrefixAllowedPaths lists path prefixes that are always allowed regardless of subscription
// status. Entries here intentionally use prefix semantics — e.g. /api/v1/user/delete also covers
// the deletion-cancel token route (/api/v1/user/delete/cancel/{token}), and /api/v1/admin/ covers
// all admin sub-routes. Subscription routes are NOT listed here to prevent word-boundary bypass;
// they are in readOnlyExactAllowedPaths above.
var readOnlyPrefixAllowedPaths = []string{
	"/api/v1/user/delete",
	"/api/v1/user/profile",
	"/api/v1/user/privacy",
	"/api/v1/paddle/webhook",
	"/api/v1/plaid/webhook",
	"/api/v1/admin/",
}

// NewReadOnlyMiddleware creates a Huma middleware that enforces read-only access for
// inactive, cancelled, and past_due subscriptions. It blocks POST, PUT, PATCH, and DELETE for
// users whose subscription status is read-only (see domain.IsReadOnlySubscription), while
// allowing GET and HEAD.
//
// Public routes (no Security annotation) are always passed through.
// Whitelisted paths are always allowed to support resubscription and self-service flows.
func NewReadOnlyMiddleware() func(ctx huma.Context, next func(huma.Context)) {
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

		// For mutating methods, check subscription status
		subStatus, ok := ctx.Context().Value(inputs.SubscriptionStatusKey).(string)
		if !ok || subStatus == "" {
			// No subscription status in context — cannot determine restriction, allow through
			next(ctx)
			return
		}

		// Only restrict inactive and cancelled subscriptions
		status := domain.SubscriptionStatus(subStatus)
		if !status.IsReadOnlySubscription() {
			next(ctx)
			return
		}

		// Check if path is whitelisted — exact-match first, then prefix.
		path := ctx.Operation().Path
		if readOnlyExactAllowedPaths[path] {
			next(ctx)
			return
		}
		for _, prefix := range readOnlyPrefixAllowedPaths {
			if strings.HasPrefix(path, prefix) {
				next(ctx)
				return
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
	ctx.BodyWriter().Write([]byte(response))
}
