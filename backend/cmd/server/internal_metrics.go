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
	"strconv"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humaecho"
	"github.com/labstack/echo/v4"
	"github.com/maxnussbaum/BoostedBudget/internal/config"
)

// defaultInternalMetricsPort is Fly's own documented convention for an app's internal
// metrics port (see backend/fly.toml and backend/fly.staging.toml's [metrics] blocks,
// which must be kept in sync with this value). It differs from cfg.Port's own default
// (8080) today, but an env override CAN still collide with cfg.Port at runtime — that is
// exactly what resolveInternalMetricsPort guards against, so this default alone must not
// be relied on to prevent a collision.
const defaultInternalMetricsPort = "9091"

// internalMetricsPort resolves the RAW (unvalidated) port the internal metrics listener
// would bind to. Reads INTERNAL_METRICS_PORT directly via os.Getenv, rather than through
// internal/config, so this task does not need to edit config.go (owned by a concurrent
// workstream on this branch — see task-4-report.md). Callers MUST pass this through
// resolveInternalMetricsPort before using it — this function does no validation at all.
func internalMetricsPort() string {
	if p := os.Getenv("INTERNAL_METRICS_PORT"); p != "" {
		return p
	}
	return defaultInternalMetricsPort
}

// resolveInternalMetricsPort validates a candidate internal metrics port against basic
// TCP constraints AND against the public listener's own port, returning an error the
// caller MUST treat as fatal (main() os.Exit(1)s on it, exactly like a bind failure).
//
// The collision check exists because of a real, non-obvious failure mode: if
// INTERNAL_METRICS_PORT is ever misconfigured to equal cfg.Port (e.g. someone sets it to
// "8080", cfg.Port's own default), the internal listener binds FIRST and succeeds — it's
// bound synchronously, before the public listener even starts. The public listener's
// later e.Start(address) call then fails to bind that same port, but main.go only LOGS
// that error (`appLogger.Error("Server stopped", "error", err)`), it does not exit. Net
// effect: the process stays up, /metrics keeps working, and the public API silently never
// serves a single request — visible only as Fly health-check failures with no obvious
// "port already in use" signal anywhere. That non-fatal e.Start() handling is pre-existing
// behaviour outside this task's scope (tracked separately); this function's job is to
// make the misconfiguration that triggers it impossible to reach in the first place.
func resolveInternalMetricsPort(raw string, publicPort string) (string, error) {
	n, err := strconv.Atoi(raw)
	if err != nil {
		return "", fmt.Errorf("INTERNAL_METRICS_PORT %q is not a valid port number: %w", raw, err)
	}
	if n < 1 || n > 65535 {
		return "", fmt.Errorf("INTERNAL_METRICS_PORT %d is out of the valid TCP port range (1-65535)", n)
	}
	if pn, perr := strconv.Atoi(publicPort); perr == nil && pn == n {
		return "", fmt.Errorf(
			"INTERNAL_METRICS_PORT %d collides with the public listener's port (PORT=%d): "+
				"the internal listener would bind it first and succeed, then the public "+
				"listener's own bind on the same port would fail silently (that failure is "+
				"only logged, not fatal) — refusing to start rather than risk the public API "+
				"going dark with metrics still working", n, pn)
	}
	return strconv.Itoa(n), nil
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
//
// ACCEPTED RESIDUAL RISK — binds 0.0.0.0, not 127.0.0.1: this listener is reachable from
// ANY machine on the same Fly organisation's private 6PN network, not merely from Fly's
// own metrics scraper. /metrics carries no authentication on this listener (Issue #1514
// only moved it off the PUBLIC internet, it did not add auth), so any other app or
// machine sharing this org's 6PN — including other environments' apps if they're on the
// same private network — can read queue depth, Plaid error rates, and GDPR-deletion
// counters unauthenticated. This is believed unavoidable with the current approach:
// Fly's 6PN is IPv6-routed to the machine, not loopback-reachable, so a bind restricted to
// 127.0.0.1 would also be unreachable to Fly's own scraper, defeating the whole point.
// Binding fly-local-6pn specifically (rather than 0.0.0.0) would exclude the public
// interface but not narrow reachability within 6PN itself — 6PN has no built-in
// per-app ACL. If this residual risk becomes unacceptable, the next step would be
// per-request auth on this listener (e.g. a shared secret header), not a narrower bind.
func bindInternalMetricsListener(port string) (net.Listener, error) {
	l, err := net.Listen("tcp", fmt.Sprintf("0.0.0.0:%s", port))
	if err != nil {
		return nil, fmt.Errorf("bind internal metrics listener on port %s: %w", port, err)
	}
	return l, nil
}
