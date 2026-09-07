package middleware

import (
	"strings"

	"github.com/labstack/echo/v4"
)

// CORSErrorHeaders sets Access-Control-Allow-Origin (and -Credentials, Vary: Origin, and
// Expose-Headers) on EVERY request whose Origin is present and allowed — including one
// that a later gate (CloudflareOnly, IPRateLimiter) is about to reject with a 403/429. It
// is registered BEFORE that gate in cmd/server/main.go, specifically so those two
// hard-coded rejection paths (c.String(403, ...) in cloudflare.go, echo.NewHTTPError(429,
// ...) in rate_limit.go) still carry CORS headers: without them the browser cannot read
// the response at all and reports an opaque CORS failure, masking the real 429/403
// (issue #1609).
//
// Expose-Headers specifically matters here because the real echomiddleware.CORSWithConfig
// instance (registered at main.go's LAST position, after the gate, PanicRecovery, Sentry,
// OTEL, StructuredLogging, Metrics and BodyLimit) never runs at all on a request the gate
// rejects — Echo's middleware chain unwinds without reaching it. So Expose-Headers:
// Retry-After from GetCORSConfig alone (cors.go) is not sufficient on its own to make
// IPRateLimiter's Retry-After header (rate_limit.go) readable cross-origin on a 429; this
// middleware must set it too, from the SAME config, so the browser can back off correctly
// even on the rejected path.
//
// The real echomiddleware.CORSWithConfig instance still owns preflight handling
// (Allow-Methods/-Headers, OPTIONS 204s) for any request that passes the gate — this
// middleware deliberately does none of that. It:
//
//  1. Never inspects or special-cases the request method. In particular it does NOT
//     short-circuit OPTIONS. An adversarial review established that Echo's CORS
//     middleware returns 204 for FAR more than genuine preflights — OPTIONS with no
//     Origin at all, and OPTIONS from a disallowed origin, both get a 204 from
//     echomiddleware.CORSWithConfig. Doing the same here, before the gate, would let any
//     OPTIONS request (non-browser, malformed, hostile origin) bypass both the
//     Cloudflare gate and the rate limiter — an unmetered perimeter bypass. This
//     middleware ONLY ever calls next(c); it can set headers, but it can never itself
//     produce a response.
//  2. Sets nothing when Origin is absent, or present but not in the allowlist.
//  3. Echoes the SPECIFIC request Origin back (never "*"), because AllowCredentials is
//     always true here and "*" is invalid alongside credentials per the Fetch spec — even
//     when the underlying config is configured as a wildcard ("*"/empty), matching
//     GetCORSConfig's own wildcard-with-credentials handling.
//
// allowedOrigins is the raw CORS_ALLOWED_ORIGINS config value — the same string passed to
// GetCORSConfig. Both parse the origin allowlist through parseCORSAllowedOrigins (cors.go)
// so the two allowlists cannot drift; the exposed-headers list is read directly from
// GetCORSConfig's own ExposeHeaders field for the same reason.
func CORSErrorHeaders(allowedOrigins string) echo.MiddlewareFunc {
	origins := parseCORSAllowedOrigins(allowedOrigins)
	wildcard := len(origins) == 1 && origins[0] == "*"

	allowed := make(map[string]struct{}, len(origins))
	for _, o := range origins {
		allowed[o] = struct{}{}
	}

	exposeHeaders := strings.Join(GetCORSConfig(allowedOrigins).ExposeHeaders, ",")

	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			origin := c.Request().Header.Get(echo.HeaderOrigin)
			if origin != "" {
				if _, ok := allowed[origin]; ok || wildcard {
					header := c.Response().Header()
					header.Set(echo.HeaderAccessControlAllowOrigin, origin)
					header.Set(echo.HeaderAccessControlAllowCredentials, "true")
					header.Add(echo.HeaderVary, echo.HeaderOrigin)
					if exposeHeaders != "" {
						header.Set(echo.HeaderAccessControlExposeHeaders, exposeHeaders)
					}
				}
			}
			return next(c)
		}
	}
}
