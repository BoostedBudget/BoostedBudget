package main

// Issue #1514: /metrics was reachable from the public internet, unauthenticated —
// no Huma Security annotation, and exempted from the CloudflareOnly gate. This file
// tests the fix: /metrics moves onto a separate, internal-only Echo+Huma listener
// (newInternalMetricsAPI in internal_metrics.go) that carries no other routes, while the
// public listener keeps serving /health exactly as before.
//
// main() itself is not unit-testable (it dials a real database, loads live config,
// etc.), so — following this package's existing convention (see
// issue1450_clerk_gate_warning_test.go's header) — behavior is verified against the
// exact functions main() calls, and wiring (that main() actually calls them, in the
// right place, with the right effect) is verified with a narrow text check against
// main.go's source.

import (
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/danielgtaylor/huma/v2/adapters/humaecho"
	"github.com/labstack/echo/v4"
	"github.com/maxnussbaum/BoostedBudget/internal/config"
	"github.com/maxnussbaum/BoostedBudget/internal/health"
	humaconfig "github.com/maxnussbaum/BoostedBudget/internal/huma"
	"github.com/maxnussbaum/BoostedBudget/internal/huma/operations"
)

func testCfg() *config.Config {
	return &config.Config{Version: "test", Environment: "test"}
}

// TestInternalMetricsAPI_ServesMetrics proves the internal listener (the exact
// *echo.Echo/huma.API pair main() builds via newInternalMetricsAPI and registers
// /metrics on) returns Prometheus text format for GET /metrics.
func TestInternalMetricsAPI_ServesMetrics(t *testing.T) {
	e, api := newInternalMetricsAPI(testCfg())
	operations.RegisterMetricsRoutes(api, operations.NewMetricsHandler())

	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("GET /metrics on internal listener = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "text/plain") {
		t.Errorf("Content-Type = %q, want Prometheus text format (text/plain)", ct)
	}
	if rec.Body.Len() == 0 {
		t.Error("expected a non-empty Prometheus metrics body")
	}
}

// TestInternalMetricsAPI_DoesNotExposePublicRoutes proves the internal listener carries
// no public route surface: only /metrics is registered on it (main() never registers
// /health or anything else there), so every other path — including /health, which stays
// on the PUBLIC listener per this task's brief — must 404.
func TestInternalMetricsAPI_DoesNotExposePublicRoutes(t *testing.T) {
	e, api := newInternalMetricsAPI(testCfg())
	operations.RegisterMetricsRoutes(api, operations.NewMetricsHandler())

	for _, path := range []string{"/health", "/health/", "/api/v1/transactions", "/openapi.json", "/docs", "/"} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, req)
		if rec.Code != http.StatusNotFound {
			t.Errorf("GET %s on internal metrics listener = %d, want 404 — the internal listener must not carry the public route surface (Issue #1514)", path, rec.Code)
		}
	}
}

// TestPublicRouteRegistration_MetricsNotServed_HealthUnchanged mirrors the PUBLIC
// listener's actual route-registration path: humaecho.NewV4 + humaconfig.NewAPIConfig
// (the exact call main.go makes for humaAPI, cmd/server/main.go ~:175) followed by
// operations.RegisterHealthRoutes with the same handler constructors main() uses, while
// deliberately NOT registering /metrics — that omission is the fix main.go now performs.
//
// The prior version of this test built its "public-shaped" listener via
// newInternalMetricsAPI, which is the INTERNAL listener's constructor — it deliberately
// blanks OpenAPIPath/DocsPath/SchemasPath (see internal_metrics.go), the opposite of the
// real public config. That made the test pass unconditionally: it never exercised
// anything resembling the production public API shape, so it would have passed even
// against a pre-fix main() that publicly exposed /metrics, as long as this test simply
// didn't call RegisterMetricsRoutes itself. Using the real humaconfig.NewAPIConfig here
// closes that gap for everything reachable from that config (verified below via /docs and
// /openapi.json actually resolving) — though whether main() itself calls
// RegisterMetricsRoutes on humaAPI is, and can only be, verified by
// TestInternalMetricsListener_IsWiredIntoMain's source-text check below, since main() is
// not unit-testable (dials a real database, loads live config, etc.).
func TestPublicRouteRegistration_MetricsNotServed_HealthUnchanged(t *testing.T) {
	cfg := testCfg()
	e := echo.New()
	api := humaecho.NewV4(e, humaconfig.NewAPIConfig(cfg.Version, cfg.Environment))

	// Issue #1531 Task 7c: createDBCheck(nil) now correctly 503s (a nil pool is an ERROR, not a
	// healthy no-op — see that function's doc comment), so a healthy fake pinger is used here
	// instead of nil: this test's job is to pin /health's behaviour is unchanged by the
	// metrics-listener split, not to re-exercise Task 7's nil-pool handling (see
	// health_checks_test.go for that).
	healthHandler := operations.NewHealthHandler(slog.Default(), health.Registry{
		DB: createDBCheck(alwaysHealthyPinger{}),
	})
	operations.RegisterHealthRoutes(api, healthHandler)

	// Sanity check that this is genuinely the public API's config and not a stand-in: on
	// the internal listener's config, /docs and /openapi.json are both blanked out (404).
	// If a future edit accidentally swapped in newInternalMetricsAPI here again, this
	// would fail loudly instead of the test silently reverting to being self-fulfilling.
	for _, path := range []string{"/docs", "/openapi.json"} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, req)
		if rec.Code == http.StatusNotFound {
			t.Fatalf("GET %s on the public-shaped listener = 404, want a real route — this test must build the listener with humaconfig.NewAPIConfig (the real public config), not a config that blanks out docs/openapi, or this test proves nothing", path)
		}
	}

	// /metrics must NOT be served on the public-shaped listener.
	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Errorf("GET /metrics on public-shaped listener = %d, want 404 — /metrics must not be reachable from the public listener (Issue #1514)", rec.Code)
	}

	// Regression guard: /health behaviour is unchanged.
	req2 := httptest.NewRequest(http.MethodGet, "/health", nil)
	rec2 := httptest.NewRecorder()
	e.ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusOK {
		t.Errorf("GET /health on public-shaped listener = %d, want 200 — health-check behaviour must be unchanged by this task", rec2.Code)
	}
}

// TestBindInternalMetricsListener_FailsLoudlyWhenPortTaken proves bindInternalMetricsListener
// surfaces a real, non-nil error when the port is unavailable — the precondition for
// main()'s os.Exit(1) startup-failure path (verified for wiring below) to ever fire.
//
// Reserves the port via bindInternalMetricsListener itself (0.0.0.0:0, letting the OS pick
// a free port) rather than via a plain net.Listen on 127.0.0.1: in this sandbox, binding
// 0.0.0.0:PORT does NOT conflict with an existing 127.0.0.1:PORT listener (verified
// separately — that combination silently succeeds here, unlike on a normal Linux/macOS
// host). Reusing the exact same 0.0.0.0 address for both binds sidesteps that sandbox
// networking quirk and reliably reproduces a real "address already in use" collision.
func TestBindInternalMetricsListener_FailsLoudlyWhenPortTaken(t *testing.T) {
	first, err := bindInternalMetricsListener("0")
	if err != nil {
		t.Fatalf("failed to reserve a test port via bindInternalMetricsListener: %v", err)
	}
	defer first.Close()

	port := first.Addr().(*net.TCPAddr).Port
	_, err = bindInternalMetricsListener(strconv.Itoa(port))
	if err == nil {
		t.Fatal("bindInternalMetricsListener on an already-bound port returned nil error, want a bind failure — a silent no-op here would blind Fly's metrics scraper without anyone noticing")
	}
}

// TestInternalMetricsPort_DefaultAndEnvOverride pins internalMetricsPort()'s contract:
// defaults to 9091 (matching fly.toml's [metrics] block) and honors an explicit
// INTERNAL_METRICS_PORT override.
func TestInternalMetricsPort_DefaultAndEnvOverride(t *testing.T) {
	t.Run("default", func(t *testing.T) {
		os.Unsetenv("INTERNAL_METRICS_PORT")
		if got := internalMetricsPort(); got != "9091" {
			t.Errorf("internalMetricsPort() = %q, want default %q", got, "9091")
		}
	})

	t.Run("override", func(t *testing.T) {
		t.Setenv("INTERNAL_METRICS_PORT", "9123")
		if got := internalMetricsPort(); got != "9123" {
			t.Errorf("internalMetricsPort() = %q, want override %q", got, "9123")
		}
	})
}

// TestResolveInternalMetricsPort_CollisionWithPublicPort is the fix for the coordinator's
// review finding: if INTERNAL_METRICS_PORT is misconfigured to equal cfg.Port (8080 is
// cfg.Port's own default — see config.go's `getEnv("PORT", "8080")`), the internal
// listener would bind that port first and succeed, then the public listener's own
// e.Start() bind on the same port fails — but main.go only logs that, it doesn't exit.
// The process would stay up with the public API silently dead. This test proves
// resolveInternalMetricsPort refuses that configuration outright, rather than letting it
// reach bindInternalMetricsListener at all.
func TestResolveInternalMetricsPort_CollisionWithPublicPort(t *testing.T) {
	_, err := resolveInternalMetricsPort("8080", "8080")
	if err == nil {
		t.Fatal("resolveInternalMetricsPort(\"8080\", \"8080\") returned nil error, want a collision error — " +
			"binding the same port as the public listener would silently take the public API offline while /metrics kept working")
	}
	if !strings.Contains(err.Error(), "8080") {
		t.Errorf("collision error %q should name the colliding port value", err.Error())
	}
}

// TestResolveInternalMetricsPort_NoCollisionWhenDistinct proves the happy path: distinct,
// valid ports pass validation unchanged.
func TestResolveInternalMetricsPort_NoCollisionWhenDistinct(t *testing.T) {
	got, err := resolveInternalMetricsPort("9091", "8080")
	if err != nil {
		t.Fatalf("resolveInternalMetricsPort(\"9091\", \"8080\") returned unexpected error: %v", err)
	}
	if got != "9091" {
		t.Errorf("resolveInternalMetricsPort(\"9091\", \"8080\") = %q, want %q", got, "9091")
	}
}

// TestResolveInternalMetricsPort_RejectsInvalidValues proves malformed or out-of-range
// INTERNAL_METRICS_PORT values are rejected loudly (a startup error) rather than being
// passed through to net.Listen, where the failure mode would be less clear.
func TestResolveInternalMetricsPort_RejectsInvalidValues(t *testing.T) {
	cases := []struct {
		name string
		raw  string
	}{
		{"non-numeric", "not-a-port"},
		{"empty string", ""},
		{"zero", "0"},
		{"negative", "-1"},
		{"too large", "65536"},
		{"way too large", "999999"},
		{"float", "9091.5"},
		{"trailing garbage after valid digits", "9091abc"},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := resolveInternalMetricsPort(c.raw, "8080")
			if err == nil {
				t.Errorf("resolveInternalMetricsPort(%q, \"8080\") returned nil error, want a validation error", c.raw)
			}
		})
	}
}

// TestResolveInternalMetricsPort_AcceptsAtoiQuirks proves the genuine "leading plus" / "leading
// zero" cases that the mislabeled case above used to claim to test: strconv.Atoi happily parses
// "+9091" and "09091" as 9091, and resolveInternalMetricsPort has no additional strictness on top
// of Atoi, so both are accepted (and normalized to "9091") rather than rejected. Harmless either
// way since a real INTERNAL_METRICS_PORT env var is never written with a leading "+" or "0", but
// pinning it here means a future tightening of the parser is a deliberate choice, not a surprise.
func TestResolveInternalMetricsPort_AcceptsAtoiQuirks(t *testing.T) {
	cases := []struct {
		name string
		raw  string
	}{
		{"leading plus", "+9091"},
		{"leading zero", "09091"},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := resolveInternalMetricsPort(c.raw, "8080")
			if err != nil {
				t.Fatalf("resolveInternalMetricsPort(%q, \"8080\") returned error %v, want nil", c.raw, err)
			}
			if got != "9091" {
				t.Errorf("resolveInternalMetricsPort(%q, \"8080\") = %q, want %q", c.raw, got, "9091")
			}
		})
	}
}

// TestInternalMetricsListener_IsWiredIntoMain is the one remaining text check, and it
// deliberately checks only WIRING: that main.go actually binds the internal listener,
// treats a bind failure as fatal, registers /metrics on the INTERNAL api (not the public
// one), and shuts the internal listener down alongside the public one. Without it,
// internal_metrics.go's functions could be perfectly correct and never invoked — the
// exact "defined, tested, never wired" class of bug this package's other drift tests
// exist to catch (see issue972/issue1371/route_registration_drift).
func TestInternalMetricsListener_IsWiredIntoMain(t *testing.T) {
	data, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatalf("could not read main.go: %v", err)
	}
	src := string(data)

	if !strings.Contains(src, "resolveInternalMetricsPort(internalMetricsPort(), cfg.Port)") {
		t.Error("main.go must validate the internal metrics port against cfg.Port via resolveInternalMetricsPort(internalMetricsPort(), cfg.Port) BEFORE binding — otherwise a collision with cfg.Port can silently take the public API offline")
	}
	if !strings.Contains(src, "Invalid internal metrics port configuration") {
		t.Error("main.go must treat an invalid/colliding internal metrics port as fatal, with a distinct log message from the bind-failure path")
	}
	if !strings.Contains(src, "bindInternalMetricsListener(resolvedInternalMetricsPort)") {
		t.Error("main.go must call bindInternalMetricsListener with the VALIDATED port (resolvedInternalMetricsPort), not the raw, unvalidated internalMetricsPort() value")
	}
	// Previously this checked strings.Contains(src, "os.Exit(1)") and
	// strings.Contains(src, "Failed to bind internal metrics listener") independently —
	// both strings merely had to exist SOMEWHERE in main.go, which main.go's OTHER fatal
	// paths (config load, tier config, region config) already satisfy for "os.Exit(1)" on
	// their own. That check would pass even if the bind-failure branch never called
	// os.Exit(1) at all. This regex instead requires the log call and os.Exit(1) to be the
	// next two statements after each other — i.e. in the same `if err != nil { ... }`
	// branch as main.go actually has it (cmd/server/main.go ~:200) — not merely present
	// independently anywhere in the file.
	// CAUTION: this regex requires os.Exit(1) to be the LITERAL next statement after the
	// appLogger.Error(...) call in main.go (only whitespace/newlines may sit between them). An
	// otherwise-harmless comment inserted between those two lines in main.go will make this
	// match fail even though behavior is unchanged — that tightness is intentional (see the
	// comment above), so fix main.go's formatting rather than loosening this pattern.
	bindFailureIsFatal := regexp.MustCompile(
		`appLogger\.Error\(\s*"Failed to bind internal metrics listener"\s*,\s*"error"\s*,\s*err\s*\)\s*\n\s*os\.Exit\(1\)`,
	)
	if !bindFailureIsFatal.MatchString(src) {
		t.Error("main.go must treat a bind failure for the internal metrics listener as fatal: the " +
			`appLogger.Error("Failed to bind internal metrics listener", ...) call and os.Exit(1) must be ` +
			"adjacent statements in the same branch, not merely both present somewhere in the file")
	}
	if !strings.Contains(src, "operations.RegisterMetricsRoutes(internalHumaAPI, humaMetricsHandler)") {
		t.Error("main.go must register /metrics on internalHumaAPI (the internal-only listener), not the public humaAPI")
	}
	if strings.Contains(src, "operations.RegisterMetricsRoutes(humaAPI, humaMetricsHandler)") {
		t.Error("main.go must NOT register /metrics on the public humaAPI — that is the exact public exposure Issue #1514 fixes")
	}
	if !strings.Contains(src, "internalMetricsEcho.Shutdown(ctx)") {
		t.Error("main.go must shut down the internal metrics listener alongside the public one")
	}
	if !strings.Contains(src, "operations.RegisterHealthRoutes(humaAPI, healthHandler)") {
		t.Error("main.go must still register /health on the PUBLIC humaAPI, unchanged — this task must not move or gate /health (regression guard)")
	}
}
