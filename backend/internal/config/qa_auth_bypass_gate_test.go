package config

import (
	"strings"
	"testing"
)

// Issue #1432 (batch ED, finding B2-1): the QA auth-bypass route is the ONLY unauthenticated
// path to admin authority in this codebase — it mints a five-minute Clerk sign-in token for
// QA_ADMIN_CLERK_USER_ID when role:"admin" is requested, it carries no Clerk security
// annotation, and it sits outside /api/v1/admin so the admin middleware (which keys purely on
// that path prefix) never sees it.
//
// Before this fix, registration turned on ONE string: ENVIRONMENT == "qa" || "staging", plus
// three non-empty QA settings. A production deployment mislabelled with exactly that string —
// or one that had QA secrets left in its secret store from a copied config — exposed it.
//
// These tests pin the hardened gate. The gate is deliberately a conjunction of FOUR
// independent facts, so no single mislabelled value can open it:
//
//  1. ENVIRONMENT was set EXPLICITLY (an unset/blank value must never enable it),
//  2. ENVIRONMENT is on an explicit two-value allowlist {qa, staging},
//  3. QA_AUTH_BYPASS_ENABLED is set to true — an opt-in a production deployment never carries,
//  4. no PRODUCTION-ONLY SIGNAL is present: a Clerk live secret key (sk_live_) or
//     PADDLE_ENVIRONMENT=production. These are the two values that are structurally
//     impossible in a real QA/staging deployment (backend/fly.toml sets
//     PADDLE_ENVIRONMENT="production"; backend/fly.staging.toml sets "sandbox", and
//     boosted-budget-fly-deployment.md gives staging sk_test_ and production sk_live_), and
//     they are exactly what a MISLABELLED production still carries.
//
// Point 4 is the load-bearing one: 1-3 are all things an operator sets, and an operator who
// mislabels the environment may mislabel the opt-in too. The Clerk secret key and the Paddle
// environment are not labels — they decide which real Clerk instance the sign-in token is
// minted in and which real Paddle catalogue is billed. If those say production, the deployment
// IS production whatever ENVIRONMENT claims.

// qaEnabledConfig returns a Config for which the gate opens, so each test below can change
// exactly one fact and bind the refusal to that specific cause.
func qaEnabledConfig() *Config {
	return &Config{
		Environment:              "qa",
		EnvironmentExplicitlySet: true,
		QAAuthBypassEnabled:      true,
		QABypassSecret:           "qa-bypass-secret",
		QAAdminClerkUserID:       "user_qa_admin",
		QAPremiumClerkUserID:     "user_qa_premium",
		ClerkSecretKey:           "sk_test_abc123",
		PaddleEnvironment:        "sandbox",
	}
}

// TestQAAuthBypassRefusalReason_AllowsGenuineQAAndStaging is the MANDATORY sibling: the
// hardening must not break the route's legitimate use. Its only real consumer is the
// qa-login skill (.claude/skills/qa-login/SKILL.md), which curls it against the QA and
// staging backends; the Playwright E2E suite does NOT use it (it authenticates through
// Clerk's test mode — see apps/web/e2e/auth.setup.ts).
func TestQAAuthBypassRefusalReason_AllowsGenuineQAAndStaging(t *testing.T) {
	for _, env := range []string{"qa", "staging", "  QA  ", "Staging"} {
		t.Run(env, func(t *testing.T) {
			cfg := qaEnabledConfig()
			cfg.Environment = env
			if reason := cfg.QAAuthBypassRefusalReason(); reason != "" {
				t.Fatalf("a genuine QA/staging deployment must keep the bypass route; got refusal %q", reason)
			}
		})
	}
}

// TestQAAuthBypassRefusalReason_RefusesOnProductionOnlySignals is the core of B2-1: even a
// deployment that claims ENVIRONMENT=qa AND carries the opt-in is refused when a value that
// only production can hold is present.
func TestQAAuthBypassRefusalReason_RefusesOnProductionOnlySignals(t *testing.T) {
	tests := []struct {
		name          string
		mutate        func(*Config)
		reasonMustSay string
	}{
		{
			name:          "Clerk live secret key",
			mutate:        func(c *Config) { c.ClerkSecretKey = "sk_live_realusers" },
			reasonMustSay: "CLERK_SECRET_KEY",
		},
		{
			name:          "Clerk live secret key with surrounding whitespace",
			mutate:        func(c *Config) { c.ClerkSecretKey = "  sk_live_realusers\n" },
			reasonMustSay: "CLERK_SECRET_KEY",
		},
		{
			name:          "Paddle production catalogue",
			mutate:        func(c *Config) { c.PaddleEnvironment = "production" },
			reasonMustSay: "PADDLE_ENVIRONMENT",
		},
		{
			name:          "Paddle production catalogue, mixed case",
			mutate:        func(c *Config) { c.PaddleEnvironment = "Production" },
			reasonMustSay: "PADDLE_ENVIRONMENT",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := qaEnabledConfig()
			tt.mutate(cfg)

			reason := cfg.QAAuthBypassRefusalReason()
			if reason == "" {
				t.Fatal("a production-only signal must refuse the QA bypass route even when ENVIRONMENT says qa")
			}
			// Bind the assertion to the SPECIFIC cause: a refusal for some other reason
			// (e.g. a missing opt-in) would otherwise make this test pass for the wrong
			// reason. qaEnabledConfig() satisfies every other condition, and the sibling
			// above proves it does.
			if !strings.Contains(reason, tt.reasonMustSay) {
				t.Fatalf("refusal must name the production-only signal %s; got %q", tt.reasonMustSay, reason)
			}
		})
	}
}

// TestQAAuthBypassRefusalReason_RequiresAPositiveTestClerkKey closes the residual the EE
// re-review named: refusing sk_live_ is a NEGATIVE heuristic, so absence of that one prefix
// was being read as affirmative evidence of non-production. Anything that is not a
// recognisable Clerk TEST key — an empty key, a key from some other issuer, a future
// production prefix Clerk has not shipped yet — sailed through. The gate now demands the
// positive fact (sk_test_…), which every non-production configuration in this repository
// already carries (.env.qa, .env.example, apps/web/.env.local, .env.e2e) and which a
// production deployment by construction cannot.
func TestQAAuthBypassRefusalReason_RequiresAPositiveTestClerkKey(t *testing.T) {
	tests := []struct {
		name string
		key  string
	}{
		{name: "empty key", key: ""},
		{name: "whitespace-only key", key: "   "},
		{name: "unrecognised issuer prefix", key: "sk_prod_realusers"},
		{name: "publishable key pasted by mistake", key: "pk_test_abc123"},
		{name: "bare secret with no Clerk prefix", key: "abc123"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := qaEnabledConfig()
			cfg.ClerkSecretKey = tt.key

			reason := cfg.QAAuthBypassRefusalReason()
			if reason == "" {
				t.Fatal("the QA bypass route must require a positively-recognised Clerk TEST secret key, not merely the absence of sk_live_")
			}
			// Bind to the specific cause: qaEnabledConfig() satisfies every other
			// condition, so a refusal that does not name CLERK_SECRET_KEY would be this
			// test passing for the wrong reason.
			if !strings.Contains(reason, "CLERK_SECRET_KEY") {
				t.Fatalf("refusal must name CLERK_SECRET_KEY; got %q", reason)
			}
		})
	}
}

// TestQAAuthBypassRefusalReason_RefusesWithoutExplicitOptIn pins condition 3.
func TestQAAuthBypassRefusalReason_RefusesWithoutExplicitOptIn(t *testing.T) {
	cfg := qaEnabledConfig()
	cfg.QAAuthBypassEnabled = false

	reason := cfg.QAAuthBypassRefusalReason()
	if reason == "" {
		t.Fatal("the QA bypass route must require an explicit QA_AUTH_BYPASS_ENABLED opt-in")
	}
	if !strings.Contains(reason, "QA_AUTH_BYPASS_ENABLED") {
		t.Fatalf("refusal must name the missing opt-in; got %q", reason)
	}
}

// TestQAAuthBypassRefusalReason_RefusesUnexplicitOrUnrecognisedEnvironment pins conditions
// 1 and 2. The "production" and "Production" cases are the mislabelling this finding is
// about, read from the other direction; the "" case is the fail-safe that the previous
// `!= "production"` shape got wrong.
func TestQAAuthBypassRefusalReason_RefusesUnexplicitOrUnrecognisedEnvironment(t *testing.T) {
	tests := []struct {
		name          string
		environment   string
		explicitlySet bool
		reasonMustSay string
	}{
		{"unset environment defaulting to development", "development", false, "ENVIRONMENT"},
		// The one case that isolates condition 1 from condition 2. Load() cannot currently
		// produce it — getEnv maps an unset OR blank ENVIRONMENT to "development", and
		// EnvironmentExplicitlySet is os.Getenv("ENVIRONMENT") != "", so !explicit always
		// implies Environment=="development", which the allowlist already refuses. Without
		// this case, deleting the EnvironmentExplicitlySet check entirely leaves every other
		// assertion here green (verified by mutation) — the check would be a dead condition
		// that reads as protection and protects nothing. It is kept as a structural invariant:
		// if Load()'s default ever changes, or a caller builds a Config by hand, the explicit
		// flag is what still refuses.
		{"recognised name but never explicitly set", "qa", false, "explicitly"},
		{"explicit development", "development", true, "ENVIRONMENT"},
		{"explicit production", "production", true, "ENVIRONMENT"},
		{"explicit blank", "", true, "ENVIRONMENT"},
		{"misspelled staging", "stagng", true, "ENVIRONMENT"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := qaEnabledConfig()
			cfg.Environment = tt.environment
			cfg.EnvironmentExplicitlySet = tt.explicitlySet

			reason := cfg.QAAuthBypassRefusalReason()
			if reason == "" {
				t.Fatalf("environment %q (explicit=%v) must not enable the QA bypass route", tt.environment, tt.explicitlySet)
			}
			if !strings.Contains(reason, tt.reasonMustSay) {
				t.Fatalf("refusal must name %s; got %q", tt.reasonMustSay, reason)
			}
		})
	}
}

// TestQAAuthBypassRefusalReason_RefusesOnMissingQASettings keeps the pre-existing condition:
// the three QA settings must all be present. Without this the handler would compare against
// an empty shared secret, and the service would mint tokens for an empty Clerk user ID.
func TestQAAuthBypassRefusalReason_RefusesOnMissingQASettings(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*Config)
		named  string
	}{
		{"missing bypass secret", func(c *Config) { c.QABypassSecret = "" }, "QA_BYPASS_SECRET"},
		{"missing admin user id", func(c *Config) { c.QAAdminClerkUserID = "" }, "QA_ADMIN_CLERK_USER_ID"},
		{"missing premium user id", func(c *Config) { c.QAPremiumClerkUserID = "" }, "QA_PREMIUM_CLERK_USER_ID"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := qaEnabledConfig()
			tt.mutate(cfg)

			reason := cfg.QAAuthBypassRefusalReason()
			if reason == "" {
				t.Fatal("an incomplete QA configuration must not register the bypass route")
			}
			if !strings.Contains(reason, tt.named) {
				t.Fatalf("refusal must name %s; got %q", tt.named, reason)
			}
		})
	}
}

// TestQAAuthBypassRefusalReason_NilConfigRefuses — fail closed on a nil receiver rather than
// panicking into whatever the caller's error handling does.
func TestQAAuthBypassRefusalReason_NilConfigRefuses(t *testing.T) {
	var cfg *Config
	if reason := cfg.QAAuthBypassRefusalReason(); reason == "" {
		t.Fatal("a nil Config must refuse the QA bypass route")
	}
}

// TestQAAuthBypassEnabled_ReadsOptInEnvVar proves the opt-in is wired to a real environment
// variable rather than only existing as a struct field the tests above set by hand.
func TestQAAuthBypassEnabled_ReadsOptInEnvVar(t *testing.T) {
	tests := []struct {
		value string
		want  bool
	}{
		{"true", true},
		{"1", true},
		{"false", false},
		{"", false},
		{"yes-please", false}, // unparseable must fall back to the safe default
	}

	for _, tt := range tests {
		t.Run("QA_AUTH_BYPASS_ENABLED="+tt.value, func(t *testing.T) {
			t.Setenv("ENVIRONMENT", "development") // keep Load() off the prod/staging validate() path
			t.Setenv("QA_AUTH_BYPASS_ENABLED", tt.value)
			cfg, err := Load()
			if err != nil {
				t.Fatalf("Load: %v", err)
			}
			if cfg.QAAuthBypassEnabled != tt.want {
				t.Fatalf("QAAuthBypassEnabled = %v, want %v", cfg.QAAuthBypassEnabled, tt.want)
			}
		})
	}
}
