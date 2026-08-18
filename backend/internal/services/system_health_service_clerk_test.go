package services

import (
	"context"
	"fmt"
	"testing"

	"github.com/maxnussbaum/BoostedBudget/internal/health"
	"github.com/stretchr/testify/assert"
)

// TestSystemHealth_ClerkWiredButUnconfiguredReportsNotConfigured mirrors the #1531 L2 fix for
// Redis, applied to Clerk: in production (cmd/server/health_checks.go's createClerkCheck),
// registry.Clerk is NEVER a nil Check even when CLERK_SECRET_KEY is unset -- the Check's
// underlying PROBE returns an error wrapping health.ErrNotConfigured instead. Before this fix,
// checkClerk had no way to see that distinction and fell through to the generic "down" branch,
// which calculateOverallStatus then escalates to a permanent top-line "degraded" for any
// deployment (local `go run`, QA) that never sets CLERK_SECRET_KEY.
func TestSystemHealth_ClerkWiredButUnconfiguredReportsNotConfigured(t *testing.T) {
	clerkCheck := health.Memoize(health.Options{Name: "clerk-unconfigured-test", TTL: -1}, func(ctx context.Context) error {
		return fmt.Errorf("clerk: not configured (CLERK_SECRET_KEY unset): %w", health.ErrNotConfigured)
	})
	svc := &systemHealthService{
		healthChecks: SharedHealthChecks{Clerk: clerkCheck},
	}

	result := svc.checkClerk(context.Background())

	assert.Equal(t, "not_configured", result.Status)
}

// TestSystemHealth_ClerkDownStillReportsDown pins that a genuinely failing (not merely
// unconfigured) Clerk check still reports "down", so the errors.Is branch added for
// not_configured doesn't accidentally swallow real failures too.
func TestSystemHealth_ClerkDownStillReportsDown(t *testing.T) {
	clerkCheck := health.Memoize(health.Options{Name: "clerk-down-test", TTL: -1}, func(ctx context.Context) error {
		return fmt.Errorf("clerk jwks request: unexpected status 500")
	})
	svc := &systemHealthService{
		healthChecks: SharedHealthChecks{Clerk: clerkCheck},
	}

	result := svc.checkClerk(context.Background())

	assert.Equal(t, "down", result.Status)
}
