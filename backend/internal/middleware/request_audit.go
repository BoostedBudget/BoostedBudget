package middleware

import (
	"context"
	"net/netip"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
)

// requestAuditContextKey is the unexported context key under which RequestAudit stores the
// per-request audit metadata. It is a struct type rather than a string so no other package can
// collide with it.
type requestAuditContextKey struct{}

// requestAudit is the per-request audit metadata captured by RequestAudit.
type requestAudit struct {
	ip        netip.Addr
	userAgent string
	requestID uuid.UUID
}

// RequestAudit is an Echo middleware (#1683 Task 4) that captures the request metadata an audit
// row may carry: the client IP, the User-Agent, and a validated request ID. It is deliberately
// INERT until the privacy write path (Task 5) consumes it through RequestAuditFrom.
//
// IP SOURCE IS LOCKED DOWN. The client IP is taken ONLY from a single-valued Cf-Connecting-Ip
// header, and ONLY when CloudflareOnly recorded GateStatusPassed for this request. RemoteAddr and
// Fly-Client-Ip are never used: RemoteAddr is the TCP peer (the Fly proxy, not the client) and
// Fly-Client-Ip is CloudflareOnly's own input, not something a downstream writer may trust
// independently. A request that skipped the gate (bypass environment, exempt path, or a chain with
// no CloudflareOnly) therefore records NO IP rather than a plausible-but-untrustworthy one.
//
// The request ID is parsed from X-Request-ID with uuid.Parse. A malformed or absent header yields
// a server-generated UUID; malformed input is never propagated into the audit INSERT (a non-UUID
// would be rejected by the UUID column, turning an audit write into a failed transaction).
//
// Register this AFTER CloudflareOnly so GateStatusFrom reports the real gate outcome; see
// registerCloudflareAndRateLimitMiddlewareWithBackend in cmd/server/main.go, pinned by
// TestRequestAuditMiddlewareOrdering.
func RequestAudit() echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			meta := requestAudit{userAgent: c.Request().UserAgent()}

			if GateStatusFrom(c) == GateStatusPassed {
				if addr, ok := SingleValuedHeaderAddr(c.Request(), "Cf-Connecting-Ip"); ok {
					meta.ip = addr
				}
			}

			if id, err := uuid.Parse(c.Request().Header.Get("X-Request-ID")); err == nil {
				meta.requestID = id
			} else {
				meta.requestID = uuid.New()
			}

			ctx := context.WithValue(c.Request().Context(), requestAuditContextKey{}, meta)
			c.SetRequest(c.Request().WithContext(ctx))
			return next(c)
		}
	}
}

// RequestAuditFrom returns the audit metadata RequestAudit captured for this request. It reads the
// request context, which is what a Huma operation handler's context.Context descends from, so the
// service layer (Task 5) can consume it without reaching for the Echo context. When the middleware
// did not run, all three values are zero (invalid IP, empty User-Agent, uuid.Nil) rather than a
// panic.
func RequestAuditFrom(ctx context.Context) (ip netip.Addr, ua string, requestID uuid.UUID) {
	meta, _ := ctx.Value(requestAuditContextKey{}).(requestAudit)
	return meta.ip, meta.userAgent, meta.requestID
}
