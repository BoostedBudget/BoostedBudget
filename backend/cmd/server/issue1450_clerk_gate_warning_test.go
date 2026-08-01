package main

// Issue #1450: the entire periodic-job registration block in main.go sits inside
// `if db != nil && cfg.ClerkSecretKey != "" { ... }`, so every background job — including the only
// place that runs the 30-day/7-day GDPR pre-deletion notices, deleteExpiredAccounts,
// retention-clock repair, and the Plaid-disconnect repair pass (all inside
// RetentionCleanupCheckWorker) — is silently disabled whenever CLERK_SECRET_KEY is unset.
//
// These tests assert on clerkGateWarning's REAL RETURN VALUES, across the full truth table.
//
// The previous version of this file scanned main.go as text for a `Warn(` call plus keywords within
// a 2000-byte window after an anchor string. A Codex adversarial review correctly rejected that:
// such a test can pass after the production warning is altered or removed as long as nearby text
// matches, and it cannot prove the warning comes from the intended branch. The decision was
// therefore extracted into clerk_gate_warning.go — see that file's header. Only ONE narrow text
// check remains below, and it checks WIRING (that main.go actually calls the helper), which text is
// the only way to check and which the helper's own tests cannot cover.

import (
	"os"
	"strings"
	"testing"
)

// TestClerkGateWarning_TruthTable covers every combination of the two inputs. Only the
// database-present-but-Clerk-missing case warrants a warning: with no database at all, background
// processing was never on the table and a Clerk-specific warning would misattribute the cause.
func TestClerkGateWarning_TruthTable(t *testing.T) {
	cases := []struct {
		name            string
		dbPresent       bool
		clerkConfigured bool
		wantWarranted   bool
		why             string
	}{
		{
			name: "db present, Clerk configured", dbPresent: true, clerkConfigured: true,
			wantWarranted: false,
			why:           "the gate is OPEN — background jobs are registered, so there is nothing to warn about",
		},
		{
			name: "db present, Clerk MISSING", dbPresent: true, clerkConfigured: false,
			wantWarranted: true,
			why:           "this is Issue #1450 itself: the jobs COULD have run and are silently not running",
		},
		{
			name: "no db, Clerk configured", dbPresent: false, clerkConfigured: true,
			wantWarranted: false,
			why:           "no database means background processing was never on the table",
		},
		{
			name: "no db, Clerk missing", dbPresent: false, clerkConfigured: false,
			wantWarranted: false,
			why: "the absent database is the cause and is reported elsewhere; a Clerk-specific " +
				"warning here would misattribute it",
		},
	}

	for _, c := range cases {
		c := c
		t.Run(c.name, func(t *testing.T) {
			msg, attrs, warranted := clerkGateWarning(c.dbPresent, c.clerkConfigured)

			if warranted != c.wantWarranted {
				t.Fatalf("clerkGateWarning(dbPresent=%v, clerkConfigured=%v) warranted = %v, want %v — %s",
					c.dbPresent, c.clerkConfigured, warranted, c.wantWarranted, c.why)
			}

			if !warranted {
				// A non-warranted result must be fully empty, so a caller that ignores the bool
				// cannot accidentally log a half-built warning.
				if msg != "" {
					t.Errorf("no warning warranted but msg = %q, want empty", msg)
				}
				if attrs != nil {
					t.Errorf("no warning warranted but attrs = %v, want nil", attrs)
				}
			}
		})
	}
}

// TestClerkGateWarning_NamesEveryDisabledSubsystem pins the warning's CONTENT. main.go's startup
// summary already logs `"clerk", boolStatus(...)` as a bare status flag; that line does not tell an
// operator that GDPR-mandated deletion has stopped running, which is precisely why #1450 was filed.
// So the warning must name each concrete consequence.
func TestClerkGateWarning_NamesEveryDisabledSubsystem(t *testing.T) {
	msg, attrs, warranted := clerkGateWarning(true, false)
	if !warranted {
		t.Fatal("precondition: db present + Clerk missing must warrant a warning")
	}

	if msg != clerkGateWarningMessage {
		t.Errorf("msg = %q, want the exported constant %q", msg, clerkGateWarningMessage)
	}
	for _, want := range []string{"CLERK_SECRET_KEY", "background jobs", "GDPR"} {
		if !strings.Contains(msg, want) {
			t.Errorf("headline %q must mention %q", msg, want)
		}
	}

	// Structured attributes are key/value pairs, so the slice length must be even — otherwise
	// slog silently emits a dangling key.
	if len(attrs)%2 != 0 {
		t.Fatalf("attrs has odd length %d — slog key/value pairs must come in twos", len(attrs))
	}

	keys := map[string]string{}
	for i := 0; i < len(attrs); i += 2 {
		k, ok := attrs[i].(string)
		if !ok {
			t.Fatalf("attrs[%d] = %v is not a string key", i, attrs[i])
		}
		v, ok := attrs[i+1].(string)
		if !ok {
			t.Fatalf("attrs[%d] = %v is not a string value", i+1, attrs[i+1])
		}
		keys[k] = v
	}

	// Every subsystem RetentionCleanupCheckWorker is solely responsible for must be named, plus a
	// remediation — a warning that says what broke but not what to do about it is half a warning.
	for _, k := range []string{
		"consequence_gdpr_pre_deletion_notices",
		"consequence_account_deletion",
		"consequence_retention_clock_repair",
		"consequence_plaid_disconnect_repair",
		"remediation",
	} {
		v, present := keys[k]
		if !present {
			t.Errorf("warning must carry the %q attribute — it names a subsystem that goes "+
				"silently dark when the gate closes (Issue #1450)", k)
			continue
		}
		if strings.TrimSpace(v) == "" {
			t.Errorf("attribute %q is present but empty", k)
		}
	}
}

// TestClerkGateWarning_IsWiredIntoMain is the one remaining text check, and it deliberately checks
// only WIRING: that main.go actually calls the helper. Without it, clerkGateWarning could be
// perfectly correct, perfectly tested, and never invoked — the exact "defined, tested, never wired"
// class this package's other drift tests exist to catch (see issue972/issue1371, and Issue #1448's
// finding that ~15 *UniqueOpts helpers were defined and unit-tested but never called).
func TestClerkGateWarning_IsWiredIntoMain(t *testing.T) {
	data, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatalf("could not read main.go: %v", err)
	}
	src := string(data)

	if !strings.Contains(src, "clerkGateWarning(db != nil, cfg.ClerkSecretKey != \"\")") {
		t.Error("main.go must call clerkGateWarning(db != nil, cfg.ClerkSecretKey != \"\") — " +
			"the helper is useless if nothing invokes it, and its own unit tests would still pass")
	}
	if !strings.Contains(src, "appLogger.Warn(msg, attrs...)") {
		t.Error("main.go must log the helper's returned msg/attrs via appLogger.Warn(msg, attrs...) — " +
			"computing the warning without emitting it leaves #1450 unfixed")
	}
}
