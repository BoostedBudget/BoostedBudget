package main

// Issue #1531 Task GA Part 1: scripts/smoke-test.sh and scripts/verify-deployment.sh
// pinned a health-response shape and a public /metrics reachability expectation that
// #1514 (and this branch) deliberately changed:
//
//   - operations.HealthStatusResponse.Status is now ALWAYS "ok" (internal/huma/operations/
//     health.go's enum:"ok" tag; see TestGetHealth_ResponseBodyHasExactlyOneFieldNamedStatus),
//     never "healthy" — the slim body is Issue #1531 criterion 20's fix, not a regression.
//   - /metrics moved onto a separate, internal-only listener (cmd/server/internal_metrics.go,
//     port 9091). The PUBLIC listener never registers /metrics at all (see
//     TestPublicRouteRegistration_MetricsNotServed_HealthUnchanged in
//     issue1514_internal_metrics_listener_test.go), so a public /metrics probe must now see
//     404 (or connection-refused), never 200 — Issue #1514 criterion 18.
//
// deploy-to-staging.yaml:795 and deploy-production.yml:863 both invoke smoke-test.sh, so a
// stale assertion here fails the FIRST deploy of this branch, not just a lint. These tests
// read the deploy scripts' own source text and check their literal expectations against
// reality. They anchor on the `$API_URL/health` / `$API_URL/metrics` (and
// `${BACKEND_URL}/metrics`) lines rather than line numbers, so reformatting the scripts
// does not silently disable the check.
//
// Do NOT "fix" this by editing internal/huma/operations/health.go or the internal metrics
// listener — the application behavior is the intended, already-shipped security fix. The
// scripts are what is stale.

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

// actualPublicHealthStatus is the literal operations.HealthStatusResponse.Status always
// carries on a 200 response (see internal/huma/operations/health.go). Not imported as a Go
// symbol because the production code intentionally has no exported constant for it (the
// enum lives in a struct tag) — TestGetHealth_ResponseBodyHasExactlyOneFieldNamedStatus in
// that package is the source of truth this literal must track.
const actualPublicHealthStatus = "ok"

// TestSmokeTestScript_HealthStatusLiteralMatchesHandler pins smoke-test.sh's
// $API_URL/health JSON assertion to the handler's real "ok" literal.
func TestSmokeTestScript_HealthStatusLiteralMatchesHandler(t *testing.T) {
	contents := readScriptOrFatal(t, "../../../scripts/smoke-test.sh")

	re := regexp.MustCompile(`check_json_response\s+"\$API_URL/health"\s+"\.status"\s+"([^"]*)"`)
	m := re.FindStringSubmatch(contents)
	if m == nil {
		t.Fatal("scripts/smoke-test.sh: could not find the $API_URL/health .status assertion line " +
			"(check_json_response \"$API_URL/health\" \".status\" \"...\") — has it been rewritten?")
	}

	if m[1] != actualPublicHealthStatus {
		t.Errorf(`scripts/smoke-test.sh asserts $API_URL/health .status == %q, but the handler always `+
			`returns %q (Issue #1514's slim body, Issue #1531 criterion 20). The first deploy of this `+
			`branch would fail its own smoke test.`, m[1], actualPublicHealthStatus)
	}
}

// TestSmokeTestScript_PublicMetricsExpectsUnreachable pins smoke-test.sh's $API_URL/metrics
// check to expect the public /metrics probe to NOT succeed — Issue #1514 criterion 18 moved
// /metrics to an internal-only listener, so 200 is no longer a valid expectation on the
// public port (404 or connection-refused is the pass condition post-fix). Anchored on the
// literal "$API_URL/metrics" occurrence rather than a line number, with a bounded text
// window after it so reformatting doesn't silently disable the check.
func TestSmokeTestScript_PublicMetricsExpectsUnreachable(t *testing.T) {
	contents := readScriptOrFatal(t, "../../../scripts/smoke-test.sh")

	window := textWindowAfter(t, contents, `"$API_URL/metrics"`, "scripts/smoke-test.sh", 400)

	if regexp.MustCompile(`check_endpoint\s+"\$API_URL/metrics"\s+"200"`).MatchString(window) {
		t.Errorf(`scripts/smoke-test.sh still treats "200" as the PASS condition for the public $API_URL/metrics ` +
			`probe, but Issue #1514 criterion 18 moved /metrics to an internal-only listener ` +
			`(cmd/server/internal_metrics.go, port 9091) — the public listener never registers it ` +
			`(TestPublicRouteRegistration_MetricsNotServed_HealthUnchanged), so the public probe now 404s ` +
			`(or the port refuses the connection, curl status "000"). The assertion must expect /metrics ` +
			`is NOT publicly reachable, not that it is.`)
	}
	// The check must FAIL on 200 specifically, and must do so fatally. Deliberately not
	// asserting that it accepts "404" by name: 403 (Cloudflare), 502, and connection-refused
	// ("000") all equally mean "the registry is not being served publicly", which is the
	// property we care about. Enumerating accepted codes would make the script brittle against
	// edge behaviour without making it any safer. The one unacceptable outcome is 200.
	if !regexp.MustCompile(`\$metrics_status"?\s*==\s*"200"`).MatchString(window) &&
		!regexp.MustCompile(`"\$\{?metrics_status\}?"\s*==\s*"200"`).MatchString(window) {
		t.Errorf(`scripts/smoke-test.sh's $API_URL/metrics check does not appear to branch on a 200 ` +
			`response. 200 is the ONLY failing outcome here — it means Issue #1514 criterion 18 ` +
			`regressed and the Prometheus registry (signup counts, transaction volume, full route ` +
			`inventory) is publicly readable. Every other status means it is not served publicly.`)
	}

	// ...and it must be fatal, not a warning. The previous "warn" severity was correct when this
	// check meant "is the metrics endpoint up" (a nicety); it is wrong now that it means "did the
	// security fix hold". A warning does not fail the deploy workflow.
	if !strings.Contains(window, `print_test "fail"`) {
		t.Errorf(`scripts/smoke-test.sh's $API_URL/metrics check does not fail hard on a public 200. ` +
			`A publicly reachable /metrics is a security regression, not a degraded nicety, so it must ` +
			`fail the deploy workflow rather than emit a warning nobody reads.`)
	}
}

// TestVerifyDeploymentScript_MetricsExpectsUnreachable is verify-deployment.sh's counterpart
// to TestSmokeTestScript_PublicMetricsExpectsUnreachable above. Lower severity (manual-only
// script, not invoked by either deploy workflow) but the same stale claim.
func TestVerifyDeploymentScript_MetricsExpectsUnreachable(t *testing.T) {
	contents := readScriptOrFatal(t, "../../../scripts/verify-deployment.sh")

	re := regexp.MustCompile(`check_endpoint\s+"\$\{BACKEND_URL\}/metrics"\s+"[^"]*"\s+(\d+)`)
	m := re.FindStringSubmatch(contents)
	if m == nil {
		t.Fatal("scripts/verify-deployment.sh: could not find the ${BACKEND_URL}/metrics check_endpoint " +
			"call — has it been rewritten?")
	}

	if m[1] == "200" {
		t.Errorf(`scripts/verify-deployment.sh expects ${BACKEND_URL}/metrics to return 200, but Issue ` +
			`#1514 criterion 18 moved /metrics to an internal-only listener — the public listener never ` +
			`registers it. The assertion must expect it is NOT publicly reachable (404).`)
	}
}

// textWindowAfter returns up to maxLen characters of contents starting at the first
// occurrence of anchor, failing the test if anchor is not found. Used to keep script-sweep
// assertions anchored on stable text rather than line numbers, per this task's brief.
func textWindowAfter(t *testing.T, contents, anchor, path string, maxLen int) string {
	t.Helper()
	idx := strings.Index(contents, anchor)
	if idx == -1 {
		t.Fatalf("%s: could not find anchor %q — has the script been rewritten?", path, anchor)
	}
	end := idx + maxLen
	if end > len(contents) {
		end = len(contents)
	}
	return contents[idx:end]
}

func readScriptOrFatal(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(data)
}
