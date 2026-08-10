package main

import (
	"os"
	"strings"
	"testing"
)

// Issue #1432 (batch ED, finding B2-1): QA_AUTH_BYPASS_ENABLED is the explicit opt-in that the
// QA auth-bypass route now requires on top of ENVIRONMENT, so a production deployment
// mislabelled "qa"/"staging" cannot expose the only unauthenticated path to admin authority.
//
// An opt-in nobody sets breaks QA; an opt-in production sets defeats the point. Both halves are
// asserted here, against the SHIPPED deployment configs rather than fixtures, because the
// exposure and the breakage both live in the data.
func TestQAAuthBypassOptInIsWiredInNonProductionDeploymentsOnly(t *testing.T) {
	t.Run("staging Fly config carries the opt-in", func(t *testing.T) {
		requireDeploymentConfigContains(t, "../../fly.staging.toml", `QA_AUTH_BYPASS_ENABLED = "true"`)
	})

	t.Run("QA compose stack carries the opt-in", func(t *testing.T) {
		requireDeploymentConfigContains(t, "../../../docker-compose.qa.yml", `QA_AUTH_BYPASS_ENABLED: "true"`)
	})

	t.Run("QA deploy workflow carries the opt-in", func(t *testing.T) {
		requireDeploymentConfigContains(t, "../../../.github/workflows/deploy-qa.yml", "QA_AUTH_BYPASS_ENABLED=true")
	})

	// The control that gives the three assertions above their meaning: the same string must be
	// absent from the production Fly config. Without this, "the opt-in is wired everywhere"
	// would also pass if somebody pasted it into fly.toml.
	t.Run("production Fly config does NOT carry the opt-in", func(t *testing.T) {
		contents, err := os.ReadFile("../../fly.toml")
		if err != nil {
			t.Fatalf("read production Fly config: %v", err)
		}
		for _, line := range strings.Split(string(contents), "\n") {
			trimmed := strings.TrimSpace(line)
			if strings.HasPrefix(trimmed, "#") {
				continue
			}
			if strings.Contains(trimmed, "QA_AUTH_BYPASS_ENABLED") {
				t.Fatalf("backend/fly.toml must never set QA_AUTH_BYPASS_ENABLED — it would opt production into the "+
					"unauthenticated admin-token-minting route (#1432 B2-1); found: %q", trimmed)
			}
		}
	})
}

// TestQAAuthBypassRegistrationUsesTheTestedGate guards against the registration condition in
// main.go drifting back to an inline ENVIRONMENT comparison. The gate's reasoning is unit-tested
// in internal/config/qa_auth_bypass_gate_test.go; an inline equality in main.go is not testable
// at all, which is how B2-1 survived for as long as it did.
func TestQAAuthBypassRegistrationUsesTheTestedGate(t *testing.T) {
	contents, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatalf("read main.go: %v", err)
	}
	source := string(contents)

	if !strings.Contains(source, "cfg.QAAuthBypassRefusalReason()") {
		t.Fatal("main.go must gate RegisterQAAuthBypassRoutes on cfg.QAAuthBypassRefusalReason() (#1432 B2-1)")
	}
	// Comment lines are excluded deliberately — the fixed code documents the old shape by
	// quoting it, and a test that could not tell code from prose would be pinning the comment.
	for lineNo, line := range strings.Split(source, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "//") {
			continue
		}
		for _, forbidden := range []string{`cfg.Environment == "qa"`, `cfg.Environment == "staging"`} {
			if strings.Contains(trimmed, forbidden) {
				t.Fatalf("main.go:%d must not gate on the raw comparison %s — a mislabelled ENVIRONMENT is exactly "+
					"the exposure #1432 B2-1 closed; use cfg.QAAuthBypassRefusalReason()", lineNo+1, forbidden)
			}
		}
	}
}

func requireDeploymentConfigContains(t *testing.T, path, required string) {
	t.Helper()

	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read deployment config %s: %v", path, err)
	}
	for _, line := range strings.Split(string(contents), "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "#") {
			continue
		}
		if trimmed == required {
			return
		}
	}
	t.Fatalf("deployment config %s must contain %q so the QA auth-bypass route keeps working there (#1432 B2-1)", path, required)
}
