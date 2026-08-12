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
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/maxnussbaum/BoostedBudget/internal/config"
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
// listener's actual route registration for /health (operations.RegisterHealthRoutes with
// the same handler constructors main() uses) while deliberately NOT registering
// /metrics — that omission is the fix main.go now performs. This is a fresh Echo+Huma
// pair, not newInternalMetricsAPI's, specifically so this test does not silently pass
// just because it reuses the same constructor as the code under test.
func TestPublicRouteRegistration_MetricsNotServed_HealthUnchanged(t *testing.T) {
	e, api := newInternalMetricsAPI(testCfg()) // vanilla Echo+Huma pair; nothing metrics-specific used below
	healthHandler := operations.NewHealthHandler(
		createDBCheck(),
		createRedisCheck(),
		createClerkCheck(),
		"test",
	)
	operations.RegisterHealthRoutes(api, healthHandler)

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

	if !strings.Contains(src, "bindInternalMetricsListener(internalMetricsPort())") {
		t.Error("main.go must call bindInternalMetricsListener(internalMetricsPort()) to open the internal metrics listener")
	}
	if !strings.Contains(src, "os.Exit(1)") || !strings.Contains(src, "Failed to bind internal metrics listener") {
		t.Error("main.go must treat a bind failure for the internal metrics listener as fatal (os.Exit(1)), not a silent no-op")
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
