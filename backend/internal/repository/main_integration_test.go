//go:build integration

package repository

import (
	"context"
	"os"
	"testing"

	"github.com/maxnussbaum/BoostedBudget/internal/database"
)

// TestMain tears down PostHog erasure obligations left behind by DeleteUserCascade fixtures.
// posthog_erasure_outbox deliberately has no users FK, so nothing cascades these rows; without
// this teardown a later package in the same `go test ./internal/...` run (internal/database's
// 000211 rollback guard) sees them and refuses to roll back. Best-effort: a missing or
// unreachable DATABASE_URL must not fail the package.
func TestMain(m *testing.M) {
	code := m.Run()
	cleanupPostHogErasureOutbox()
	os.Exit(code)
}

func cleanupPostHogErasureOutbox() {
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		return
	}
	ctx := context.Background()
	pool, err := database.NewPoolFromURL(ctx, dsn)
	if err != nil {
		return
	}
	defer pool.Close()
	_, _ = pool.Exec(ctx, "DELETE FROM posthog_erasure_operator_audit")
	_, _ = pool.Exec(ctx, "DELETE FROM posthog_erasure_outbox")
}
