package domain

import "testing"

// Issue #1432 (batch ED, finding B2-1): IsQAAuthBypassEnvironment is the handler-side half of
// the QA auth-bypass gate. It is deliberately NOT IsNonProductionEnvironment:
//
//   - IsNonProductionEnvironment recognises "development" as well, because the admin-email
//     allowlist legitimately loads its test entries on a dev box.
//   - The bypass route mints a REAL Clerk sign-in token against whatever Clerk instance the
//     process is keyed to, so it exists only in the two DEPLOYED non-production environments
//     that have pre-created QA users: qa and staging.
//
// Both functions are allowlists — anything unrecognised, including "" — gets the production
// treatment. That is the fix for the fail-open `environment == "production"` shape the handler
// used to carry, which let "Production", " production ", or any typo through.
func TestIsQAAuthBypassEnvironment(t *testing.T) {
	tests := []struct {
		env  string
		want bool
	}{
		{"qa", true},
		{"staging", true},
		{"QA", true},
		{"  Staging  ", true},
		{"development", false}, // recognised non-production, but NOT a bypass environment
		{"production", false},
		{"Production", false}, // the fail-open shape's blind spot
		{" production ", false},
		{"", false},
		{"stagng", false},
		{"prod", false},
	}

	for _, tt := range tests {
		t.Run(tt.env, func(t *testing.T) {
			if got := IsQAAuthBypassEnvironment(tt.env); got != tt.want {
				t.Fatalf("IsQAAuthBypassEnvironment(%q) = %v, want %v", tt.env, got, tt.want)
			}
		})
	}
}

// TestIsQAAuthBypassEnvironment_IsStrictlyNarrowerThanNonProduction pins the relationship
// between the two allowlists so a later "simplification" that collapses them is caught: every
// bypass environment is non-production, but not every non-production environment is a bypass
// environment.
func TestIsQAAuthBypassEnvironment_IsStrictlyNarrowerThanNonProduction(t *testing.T) {
	for env := range nonProductionEnvironments {
		if IsQAAuthBypassEnvironment(env) && !IsNonProductionEnvironment(env) {
			t.Fatalf("%q is a bypass environment but not a non-production one", env)
		}
	}
	if IsQAAuthBypassEnvironment("development") {
		t.Fatal("development is non-production but must NOT be a QA-bypass environment")
	}
	if !IsNonProductionEnvironment("development") {
		t.Fatal("control failed: development must still be a recognised non-production environment")
	}
}
