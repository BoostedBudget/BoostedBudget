package main

// Issue #1531 Task 8, gap round 2: RATE_LIMIT_FALLBACK_DIVISOR exists, is correctly implemented in
// rateLimitFallbackLimit (main.go — guards a zero/negative divisor, floors the result to 1, passes
// limit <= 0 through unchanged), and is unit-tested. But neither fly.toml nor fly.staging.toml ever
// SET it, so the shipped default of 1 (no division) meant the mitigation did nothing in production:
// during a Redis outage, N machines each fall back to their own in-memory limiter and independently
// enforce the FULL configured limit, multiplying the intended fleet-wide budget by N — the exact
// defect Issue #1531 Task 2 exists to eliminate, reappearing unfixed in the degraded path.
//
// This test ties RATE_LIMIT_FALLBACK_DIVISOR to the "Intended machine count: N" comment each Fly
// config already carries, so the two facts that must agree can no longer silently drift apart: if a
// machine count changes without updating the divisor (or vice versa), this test fails instead of the
// mismatch surviving unnoticed until an outage makes it visible the hard way.
//
// Anchoring: both regexes below match on a STABLE LITERAL — the "Intended machine count:" phrase and
// the "RATE_LIMIT_FALLBACK_DIVISOR" key name — rather than a line number or exact-line string match.
// A prior implementer on this same gap round anchored a Fly-config assertion on line position; a
// harmless reformatting of the file (re-indentation, a wrapped comment, a reordered line) would have
// silently defeated that assertion without failing it. Regex-over-stable-literal survives
// reformatting: it fails loudly only when the underlying fact (the count, or the divisor value)
// actually changes, not when the file's cosmetic layout does.
//
// ⚠ This invariant holds ONLY while machine count is STATIC. Autoscaling turns "intended machine
// count" from a fixed number into a range, at which point a single fixed divisor can under- or
// over-divide depending on how many machines are actually running — tracked as follow-up
// **Issue #1535**. Whoever wires up autoscaling should find this comment (and the failing test it
// eventually produces) before they find a confusing production incident.

import (
	"os"
	"regexp"
	"strconv"
	"testing"
)

var (
	intendedMachineCountRe     = regexp.MustCompile(`Intended machine count:\s*(\d+)`)
	rateLimitFallbackDivisorRe = regexp.MustCompile(`(?m)^\s*RATE_LIMIT_FALLBACK_DIVISOR\s*=\s*"(\d+)"\s*$`)
)

func TestRateLimitFallbackDivisorMatchesIntendedMachineCount(t *testing.T) {
	for _, path := range []string{"../../fly.toml", "../../fly.staging.toml"} {
		t.Run(path, func(t *testing.T) {
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("read %s: %v", path, err)
			}
			src := string(data)

			countMatch := intendedMachineCountRe.FindStringSubmatch(src)
			if countMatch == nil {
				t.Fatalf("%s: could not find an \"Intended machine count: N\" comment to anchor this test on — "+
					"that comment is the source of truth this test ties RATE_LIMIT_FALLBACK_DIVISOR to", path)
			}
			intendedCount, err := strconv.Atoi(countMatch[1])
			if err != nil {
				t.Fatalf("%s: intended machine count %q is not an integer: %v", path, countMatch[1], err)
			}

			divisorMatch := rateLimitFallbackDivisorRe.FindStringSubmatch(src)
			if divisorMatch == nil {
				t.Fatalf("%s: [env] must set RATE_LIMIT_FALLBACK_DIVISOR (Issue #1531 Task 8). Without it, "+
					"rateLimitFallbackLimit's default divisor of 1 applies (no division), so N machines "+
					"independently falling back to in-memory rate limiting during a Redis outage each enforce "+
					"the FULL configured limit — an N× overshoot of the intended fleet-wide budget, the exact "+
					"defect Task 2 exists to eliminate, reappearing unfixed in the degraded path", path)
			}
			divisor, err := strconv.Atoi(divisorMatch[1])
			if err != nil {
				t.Fatalf("%s: RATE_LIMIT_FALLBACK_DIVISOR %q is not an integer: %v", path, divisorMatch[1], err)
			}

			if divisor != intendedCount {
				t.Fatalf("%s: RATE_LIMIT_FALLBACK_DIVISOR = %d, want %d to match this file's own "+
					"\"Intended machine count: %d\" comment — these two facts must agree, or the fallback "+
					"limiter either over-divides (needlessly tight) or under-divides (still an N× overshoot) "+
					"relative to the fleet size this file declares. (Reminder: this coupling assumes a STATIC "+
					"machine count — autoscaling breaks it in both directions; see follow-up Issue #1535.)",
					path, divisor, intendedCount, intendedCount)
			}
		})
	}
}
