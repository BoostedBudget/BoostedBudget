package main

// Issue #1514: /metrics was reachable from the public internet, unauthenticated. It has
// no Huma `Security` annotation (Huma's Clerk middleware auto-skips routes without one),
// and cloudflare.go's exemptedPaths bypassed the CloudflareOnly gate for it too — so
// nothing on the public listener protected it. It exposes River queue depth/failures,
// Plaid error rates, dead-letter state, and GDPR-deletion internals
// (deletion_residue_records, household_owner_deletion_blocked_total).
//
// The fix that does NOT work: removing "/metrics" from cloudflare.go's exemptedPaths.
// Every scrape would then have to pass CloudflareOnly, which requires a Cloudflare-range
// Fly-Client-Ip AND the X-Origin-Secret header. Fly's own metrics scraper supplies
// neither — per Fly.io staff on the Fly community forum (thread "Protecting metrics and
// health endpoints"), health-check/metrics probes deliberately omit the headers Fly's
// public HTTP proxy layer adds (including Fly-Client-Ip), because those probes do not
// travel through fly-proxy the way public internet traffic does. The same staff reply
// recommends exactly the fix implemented here — "expose metrics on a different port...
// that's pretty common" — as the standard way to keep a metrics endpoint off the public
// surface while remaining scrapable.
//
// This file builds the internal-only listener: a second, independent *echo.Echo (and its
// own Huma wrapper) with none of the public listener's middleware and no routes besides
// /metrics. cmd/server/main.go wires its bind/start/shutdown into the process lifecycle
// alongside the public listener `e`.
import (
	"fmt"
	"net"
	"os"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humaecho"
	"github.com/labstack/echo/v4"
	"github.com/maxnussbaum/BoostedBudget/internal/config"
)

// defaultInternalMetricsPort is Fly's own documented convention for an app's internal
// metrics port (see backend/fly.toml and backend/fly.staging.toml's [metrics] blocks,
// which must be kept in sync with this value). Deliberately distinct from cfg.Port
// (8080, the public listener's port) so the two listeners can never collide.
const defaultInternalMetricsPort = "9091"

// internalMetricsPort resolves the port the internal metrics listener binds to. Reads
// INTERNAL_METRICS_PORT directly via os.Getenv, rather than through internal/config, so
// this task does not need to edit config.go (owned by a concurrent workstream on this
// branch — see task-4-report.md).
func internalMetricsPort() string {
	if p := os.Getenv("INTERNAL_METRICS_PORT"); p != "" {
		return p
	}
	return defaultInternalMetricsPort
}

// newInternalMetricsAPI constructs the internal-only Echo+Huma pair. Middleware
// (CloudflareOnly, IPRateLimiter, CORS, SecurityHeaders, MetricsMiddleware, ...) is
// registered per-*echo.Echo-instance via e.Use() in main(), so a brand-new *echo.Echo
// here starts with an empty middleware chain and an empty route table — nothing besides
// whatever the caller explicitly registers on the returned huma.API can ever be served
// from it. main() registers ONLY /metrics on it.
//
// This deliberately does NOT reuse internal/huma.NewAPIConfig (the config the public
// listener's humaAPI is built with): that shared config sets DocsPath="/docs" and
// OpenAPIPath="/openapi", and huma.NewAPI registers those as real routes on whichever
// instance it's given — see huma/v2@.../api.go's `if config.OpenAPIPath != ""` /
// `if config.DocsPath != ""` / `if config.SchemasPath != ""` guards. Reusing it here
// would silently reintroduce a second public-ish route surface on the "internal-only"
// listener. Leaving OpenAPIPath/DocsPath/SchemasPath blank disables all three.
func newInternalMetricsAPI(cfg *config.Config) (*echo.Echo, huma.API) {
	e := echo.New()
	e.HideBanner = true
	e.HidePort = true

	apiConfig := huma.DefaultConfig("BoostedBudget Internal Metrics", cfg.Version)
	apiConfig.OpenAPIPath = ""
	apiConfig.DocsPath = ""
	apiConfig.SchemasPath = ""

	api := humaecho.NewV4(e, apiConfig)
	return e, api
}

// bindInternalMetricsListener opens the TCP listener for the internal metrics server.
// Binding happens HERE, synchronously — before any goroutine starts serving — so a
// failure (e.g. the port is already in use, or disallowed by the runtime environment)
// is returned to the caller as a plain error the caller MUST treat as fatal. That is
// what makes a bind failure a loud startup error instead of a silent no-op discovered
// only when the first scrape times out days later.
func bindInternalMetricsListener(port string) (net.Listener, error) {
	l, err := net.Listen("tcp", fmt.Sprintf("0.0.0.0:%s", port))
	if err != nil {
		return nil, fmt.Errorf("bind internal metrics listener on port %s: %w", port, err)
	}
	return l, nil
}
