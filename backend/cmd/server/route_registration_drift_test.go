package main

import (
	"os"
	"regexp"
	"sort"
	"testing"
)

// TestRouteRegistrationNoDrift guards against the spec-vs-runtime route divergence
// that caused #1247.
//
// The OpenAPI spec is generated from operations.RegisterAllRoutesForSpec
// (backend/internal/huma/operations/register.go), while the live server registers
// routes independently in cmd/server/main.go. When RegisterUserAccountRoutes was
// listed for spec generation but never wired into main.go, the generated spec — and
// every downstream client (Orval, the MCP Worker's bundled spec) — advertised
// GET /api/v1/user-accounts, which the running server returned 404 for.
//
// Invariant: every RegisterXxxRoutes group used for spec generation MUST also be
// registered by the runtime server. Otherwise the contract lies to clients.
func TestRouteRegistrationNoDrift(t *testing.T) {
	reCall := regexp.MustCompile(`Register([A-Za-z]+Routes)\(`)

	extract := func(path string) map[string]bool {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		out := map[string]bool{}
		for _, m := range reCall.FindAllStringSubmatch(string(data), -1) {
			out[m[1]] = true
		}
		return out
	}

	spec := extract("../../internal/huma/operations/register.go")
	runtime := extract("main.go")

	if len(spec) == 0 || len(runtime) == 0 {
		t.Fatalf("failed to extract route registrations (spec=%d runtime=%d) — did the files move?", len(spec), len(runtime))
	}

	var missing []string
	for name := range spec {
		if !runtime[name] {
			missing = append(missing, name)
		}
	}
	sort.Strings(missing)

	if len(missing) > 0 {
		t.Errorf("route groups registered for the OpenAPI spec but NOT by the runtime server (clients get 404): %v\n"+
			"Register them in cmd/server/main.go so the served API matches the generated spec (see #1247).", missing)
	}
}
