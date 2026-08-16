// Package operations provides Huma operation handlers for the BoostedBudget API.
package operations

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/maxnussbaum/BoostedBudget/internal/health"
)

// HealthInput represents the input for the health check endpoint.
// This endpoint requires no parameters.
type HealthInput struct{}

// HealthStatusResponse is the entire public /health response body. It is
// deliberately minimal: this endpoint is unauthenticated and reachable
// directly from the public internet (Fly's own http_checks hit it with no
// Cloudflare headers in front), so it must never carry anything an attacker
// could use to fingerprint the stack or observe a dependency's internals —
// no version, no timestamps, no per-check breakdown, no error text. See
// issue #1531 Task 6 for the incident this replaces.
type HealthStatusResponse struct {
	Status string `json:"status" enum:"ok" doc:"Liveness indicator. Always \"ok\" when the response is 200."`
}

// Schema implements huma.SchemaProvider so HealthStatusResponse is described
// inline in the OpenAPI response, never registered under
// #/components/schemas. That matters beyond OpenAPI aesthetics: Huma's
// SchemaLinkTransformer (installed by huma.DefaultConfig, which
// internal/huma.NewAPIConfig uses in production -- see cmd/server/main.go:324)
// only injects a `$schema` field and a `Link: ...; rel="describedBy"` header
// for responses whose schema is a `#/...` ref. An inline schema has no ref, so
// the transformer's addSchemaField skips it and neither leak happens. Without
// this, production emits {"$schema":"https://api.boostedbudget.com/schemas/
// HealthStatusResponse.json","status":"ok"} on an unauthenticated endpoint --
// a URL that fingerprints the framework and the Go type name, exactly the
// disclosure class this handler otherwise guards against. humatest.New(t)'s
// bare config has no transformer, so tests built on it can't see this; use
// the production-config harness in health_test.go to verify.
func (HealthStatusResponse) Schema(_ huma.Registry) *huma.Schema {
	return &huma.Schema{
		Type: huma.TypeObject,
		Properties: map[string]*huma.Schema{
			"status": {
				Type:        huma.TypeString,
				Enum:        []any{"ok"},
				Description: `Liveness indicator. Always "ok" when the response is 200.`,
			},
		},
		Required: []string{"status"},
	}
}

// HealthOutput represents the health check response.
type HealthOutput struct {
	Body HealthStatusResponse
}

// HealthHandler handles health check requests.
//
// GetHealth only ever reads registry.DB. That is a deliberate, structural
// choice, not a runtime filter: registry.Redis and registry.Clerk are never
// referenced anywhere in this file. Issue #1531 requires that Redis being
// down must never flip this endpoint away from 200, and the safest way to
// guarantee that on an unauthenticated, internet-reachable path is to make it
// impossible for the code to invoke those probes at all — not to invoke them
// and then discard the result. An unauthenticated caller gets zero ability to
// trigger extra probes against dependencies that can't change the response
// anyway (see TestGetHealth_DoesNotRunNonCriticalChecks).
type HealthHandler struct {
	registry health.Registry
	logger   *slog.Logger
}

// NewHealthHandler creates a new health handler. Only registry.DB is
// consulted by GetHealth; registry.Redis/registry.Clerk may be populated (the
// same Registry is shared with the admin health endpoint, see internal/health)
// but are inert here. A nil logger falls back to slog.Default().
func NewHealthHandler(logger *slog.Logger, registry health.Registry) *HealthHandler {
	if logger == nil {
		logger = slog.Default()
	}
	return &HealthHandler{
		registry: registry,
		logger:   logger,
	}
}

// GetHealth handles GET /health requests.
//
// Full failure detail (the underlying error, which for pgx/go-redis/Clerk
// clients can carry host:port, DSN fragments, or Upstash endpoint names) is
// logged server-side at Error level, never placed in the response body. The
// observability tee (see internal/observability/logger.go) forwards Error
// level logs to Sentry, so the detail is relocated rather than discarded.
func (h *HealthHandler) GetHealth(ctx context.Context, input *HealthInput) (*HealthOutput, error) {
	if h.registry.DB != nil {
		result := h.registry.DB(ctx)
		if !result.Healthy() {
			h.logger.ErrorContext(ctx, "critical health check failed", "error", result.Err)
			// Never pass err (or anything derived from it) as a variadic here:
			// huma.Error503ServiceUnavailable appends its variadic args into the
			// response's `errors` array, which would reintroduce the exact leak
			// this handler exists to close. The detail is already logged above.
			return nil, huma.Error503ServiceUnavailable("service unavailable")
		}
	}

	return &HealthOutput{Body: HealthStatusResponse{Status: "ok"}}, nil
}

// RegisterHealthRoutes registers health check routes with the Huma API.
func RegisterHealthRoutes(api huma.API, handler *HealthHandler) {
	huma.Register(api, huma.Operation{
		OperationID: "getHealth",
		Method:      http.MethodGet,
		Path:        "/health",
		Summary:     "Health check",
		Description: "Liveness check. Returns 200 with {\"status\":\"ok\"} when critical dependencies are healthy, 503 otherwise. Carries no version, timing, or dependency detail.",
		Tags:        []string{"Health"},
		Errors:      []int{http.StatusServiceUnavailable},
	}, handler.GetHealth)
}
