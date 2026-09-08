package middleware

import (
	"errors"
	"log/slog"
	"strings"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/maxnussbaum/BoostedBudget/internal/observability"
)

// sensitiveQueryParams lists URL query parameter names whose values must be redacted from logs.
var sensitiveQueryParams = map[string]struct{}{
	"token":         {},
	"password":      {},
	"key":           {},
	"secret":        {},
	"authorization": {},
	"access_token":  {},
	"refresh_token": {},
	"api_key":       {},
	"apikey":        {},
}

// redactQueryParams replaces values of sensitive query parameters with [REDACTED].
// The comparison is case-insensitive. Non-sensitive parameters are returned unchanged.
// Builds the result string manually to avoid URL-encoding the [REDACTED] marker.
func redactQueryParams(rawQuery string) string {
	if rawQuery == "" {
		return rawQuery
	}

	pairs := strings.Split(rawQuery, "&")
	hasRedaction := false

	for i, pair := range pairs {
		k, _, found := strings.Cut(pair, "=")
		if !found {
			continue
		}
		if _, sensitive := sensitiveQueryParams[strings.ToLower(k)]; sensitive {
			pairs[i] = k + "=[REDACTED]"
			hasRedaction = true
		}
	}

	if !hasRedaction {
		return rawQuery
	}
	return strings.Join(pairs, "&")
}

// StructuredLogging creates an Echo middleware that logs HTTP requests using slog.
//
// The middleware logs the following fields:
// - type: "http_request"
// - method: HTTP method (GET, POST, etc.)
// - path: Request path
// - status: HTTP status code
// - duration_ms: Request duration in milliseconds
// - request_id: Unique request identifier (from context, set by RequestID middleware)
// - user_id: Authenticated user ID (from context, set by Clerk middleware)
// - remote_ip: Client IP address
// - user_agent: Client user agent
// - query: Query parameters (if present, sensitive values redacted)
// - error: Error message (if request failed)
//
// Usage:
//
//	e := echo.New()
//	e.Use(RequestID())  // Should be first to generate request IDs
//	e.Use(StructuredLogging(logger))
func StructuredLogging(logger *slog.Logger) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			start := time.Now()
			req := c.Request()
			ctx := req.Context()

			err := next(c)

			duration := time.Since(start)
			status := c.Response().Status

			// Issue #1609 Item 4: when the handler RETURNS an *echo.HTTPError instead of
			// writing a response, Echo's HTTPErrorHandler writes the real status only after
			// this middleware chain unwinds — so c.Response().Status still holds the
			// unwritten default. Production logs showed the result directly:
			//
			//	{"level":"WARN","path":"/","status":200,"error":"code=404, message=Not Found"}
			//
			// status 200 alongside a 404. The level was already escalated correctly for this
			// case (below), but the STATUS FIELD kept the stale value, so any dashboard or
			// alert counting by status silently undercounted 4xx/5xx. Every propagated error
			// that reaches THIS middleware is affected — router 404s and Huma handler errors,
			// both of which return without writing a response.
			//
			// This fix cannot, and does not, cover IPRateLimiter's 429 or CloudflareOnly's
			// 403-via-gate-rejection: IPRateLimiter short-circuits earlier in the middleware
			// chain and never calls next(c), so a rate-limited request produces no HTTP log
			// line here at all — that absence is itself a real observability gap, tracked
			// separately rather than papered over by this comment.
			//
			// Prefer the error's own code whenever the response has not been committed.
			// c.Response().Committed is the authoritative "did anything get written" signal;
			// a handler that writes its own status before returning keeps that status
			// verbatim (CloudflareOnly's c.String(403, ...) is used below purely as a
			// synthetic example of that write-then-return shape — in production a 403
			// from CloudflareOnly never reaches this middleware at all, per the note above).
			// Pinned by TestStructuredLogging_PropagatedHTTPErrorLogsRealStatus.
			if err != nil && !c.Response().Committed {
				var httpErr *echo.HTTPError
				if errors.As(err, &httpErr) && httpErr.Code != 0 {
					status = httpErr.Code
				}
			}

			level := slog.LevelInfo
			if status >= 500 {
				level = slog.LevelError
			} else if status >= 400 {
				level = slog.LevelWarn
			} else if err != nil {
				level = slog.LevelWarn
			}

			attrs := []slog.Attr{
				slog.String("type", "http_request"),
				slog.String("method", req.Method),
				slog.String("path", req.URL.Path),
				slog.Int("status", status),
				slog.Int64("duration_ms", duration.Milliseconds()),
			}

			if requestID := observability.RequestIDFromContext(ctx); requestID != "" {
				attrs = append(attrs, slog.String("request_id", requestID))
			}
			if userID := observability.UserIDFromContext(ctx); userID != "" {
				attrs = append(attrs, slog.String("user_id", userID))
			}
			if remoteAddr := req.RemoteAddr; remoteAddr != "" {
				if idx := strings.LastIndex(remoteAddr, ":"); idx != -1 {
					attrs = append(attrs, slog.String("remote_ip", remoteAddr[:idx]))
				}
			}
			if ua := req.UserAgent(); ua != "" {
				attrs = append(attrs, slog.String("user_agent", ua))
			}
			if q := req.URL.RawQuery; q != "" {
				attrs = append(attrs, slog.String("query", redactQueryParams(q)))
			}
			if err != nil {
				attrs = append(attrs, slog.String("error", err.Error()))
			}

			logger.LogAttrs(ctx, level, "HTTP request processed", attrs...)

			return err
		}
	}
}
