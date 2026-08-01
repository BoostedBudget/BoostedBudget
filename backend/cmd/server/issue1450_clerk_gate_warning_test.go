package main

// TestIssue1450ClerkGateEmitsLoudWarning guards against Issue #1450: the entire
// periodic-job registration block in main.go sits inside
// `if db != nil && cfg.ClerkSecretKey != "" { ... }`, so every background job —
// including the only place that runs the 30-day/7-day GDPR pre-deletion notices,
// deleteExpiredAccounts, retention-clock repair, and the Plaid-disconnect repair pass
// (all inside RetentionCleanupCheckWorker) — is silently disabled whenever
// CLERK_SECRET_KEY is unset, with nothing reporting that fact.
//
// main.go's main() is not directly unit-testable (it connects to a real DB, calls
// os.Exit, etc.), so — matching this package's existing drift-test convention
// (issue972_wiring_drift_test.go, issue1371_wiring_drift_test.go,
// route_registration_drift_test.go) — this test scans main.go as text and asserts a
// loud, specific warning is emitted on the db != nil-but-no-Clerk-secret path, naming
// the concrete GDPR-relevant subsystems that go silently dark. main.go:95 already logs
// `"clerk", boolStatus(cfg.ClerkSecretKey != "")` as part of a generic startup config
// summary — that is a status line, not a warning about consequences, and does not
// satisfy this test.
import (
	"os"
	"strings"
	"testing"
)

func TestIssue1450ClerkGateEmitsLoudWarning(t *testing.T) {
	data, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatalf("could not read main.go: %v", err)
	}
	src := string(data)

	const gate = `if db != nil && cfg.ClerkSecretKey != "" {`
	if !strings.Contains(src, gate) {
		t.Fatalf("could not find the Clerk gate %q in main.go — did it move or get rewritten? "+
			"update this test's expectations if the gate's shape changed intentionally", gate)
	}

	const closeAnchor = "appLogger.Info(\"Authenticated routes enabled (Clerk + Database)\")"
	closeIdx := strings.Index(src, closeAnchor)
	if closeIdx == -1 {
		t.Fatalf("could not find the Clerk gate's closing anchor %q in main.go — update this "+
			"test's anchor if the surrounding code changed", closeAnchor)
	}

	// The warning must live within a short window after the gate's closing log line (i.e. an
	// else-branch attached to the same if/else, or an equivalent standalone db!=nil check
	// immediately following) — not merely exist anywhere in the 2000+ line file.
	afterGate := src[closeIdx:]
	window := afterGate
	if len(window) > 2000 {
		window = window[:2000]
	}

	if !strings.Contains(window, "Warn(") {
		t.Fatalf("expected a Warn-level log call near the Clerk gate's closing brace naming the "+
			"disabled subsystems (Issue #1450) — none found within %d bytes after the gate's "+
			"closing log line", len(window))
	}

	// The warning must name the concrete GDPR-relevant consequences, not just restate that
	// Clerk is unconfigured — a generic "Clerk not configured" line does not tell an operator
	// that GDPR-mandated deletion has silently stopped running.
	mustContain := []string{"GDPR", "retention", "deletion", "Plaid"}
	for _, s := range mustContain {
		if !strings.Contains(window, s) {
			t.Errorf("expected the Clerk-gate warning (within %d bytes after the gate's closing "+
				"log line) to mention %q — the warning must name the concrete GDPR-relevant "+
				"consequence of the gate being closed, not just log a generic status flag "+
				"(Issue #1450)", len(window), s)
		}
	}
}
