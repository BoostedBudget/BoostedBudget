package middleware

// #930: Read-only enforcement middleware.
// Blocks mutating HTTP methods (POST/PUT/PATCH/DELETE) for users with inactive or cancelled subscriptions.
// GET and HEAD are always allowed (read-only access).
// Certain paths (resubscribe, self-service) are whitelisted to allow full access.

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/danielgtaylor/huma/v2"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/huma/inputs"
)

// readOnlyWhitelistedPrefixes lists paths that are always allowed regardless of subscription status.
// These paths cover resubscription flows, self-service account management, and incoming webhooks.
var readOnlyWhitelistedPrefixes = []string{
	"/api/v1/user/delete",
	"/api/v1/user/profile",
	"/api/v1/user/privacy",
	"/api/v1/subscription/", // checkout, preview-update, update — resubscription flows
	"/api/v1/paddle/webhook",
	"/api/v1/plaid/webhook",
	"/api/v1/admin/",
}

// NewReadOnlyMiddleware creates a Huma middleware that enforces read-only access for
// inactive and cancelled subscriptions. It blocks POST, PUT, PATCH, and DELETE for
// users whose subscription status is inactive or cancelled, while allowing GET and HEAD.
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

		// Check if path is whitelisted
		path := ctx.Operation().Path
		for _, prefix := range readOnlyWhitelistedPrefixes {
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
