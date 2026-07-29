//go:build integration

package repository

// Integration tests for JobHealthRepository.GetOutstandingPlaidCleanupStats (Issue #1413).
//
// RetentionCleanupCheckWorker.repairPlaidDisconnects (backend/internal/queue/jobs/retention_cleanup_check.go)
// is the ONLY place a lapsed user's Plaid bank connections are ever disconnected, but a per-user
// Plaid failure there does `errorCount++; continue` while Work() still reports success overall —
// so a user can be silently re-selected on every run, forever, with no metric of any kind for the
// outstanding-cleanup backlog. This gauge is the direct measure of that hazard: the count of users
// with outstanding Plaid-disconnect cleanup, and the age of the OLDEST such marker.
//
// The trap this file exists to pin down: migration 000119 stamped plaid_cleanup_pending_at on
// EVERY inactive/cancelled user regardless of whether they had any linked Plaid item at all, and
// 000120 re-stamped those residue rows to its own NOW(). repairPlaidDisconnects's own candidate
// query (GetUsersNeedingPlaidDisconnectRepair) never reaches those rows — its live-item EXISTS
// clause filters them out before the marker is ever cleared. So "marker non-NULL" is NOT a valid
// proxy for "cleanup outstanding" — this gauge's query must join on live linked items (the SAME
// liveness definition the repair query uses, which treats a NULL linked_items.status as live), or
// it reports a large PERMANENT phantom backlog on day one and gets ignored forever.
//
// pgxmock (job_health_repository_test.go) cannot validate the EXISTS-subquery liveness join or the
// aggregate SQL (COUNT, MIN, COALESCE, EXTRACT(EPOCH FROM ...)) — a real database is required.
//
// Run with:
//
//	DATABASE_URL=postgres://... go test ./internal/repository/... -run TestJobHealthRepository_Integration_.*PlaidCleanup -tags integration -v

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	appcrypto "github.com/maxnussbaum/BoostedBudget/internal/crypto"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
)

// skipIfPlaidCleanupBaselineNonZero guards the "must be zero" assertion below against a shared dev
// DB that already has an unrelated outstanding Plaid cleanup sitting in it — mirrors
// skipIfBaselineNonZero's rationale for the manual-tier-override gauge (job_health_repository_integration_test.go):
// GetOutstandingPlaidCleanupStats intentionally scans the WHOLE users table, so it is not isolated
// from other rows the way an ID-scoped assertion would be.
func skipIfPlaidCleanupBaselineNonZero(t *testing.T, repo JobHealthRepository, ctx context.Context) {
	t.Helper()
	if count, _, err := repo.GetOutstandingPlaidCleanupStats(ctx); err == nil && count != 0 {
		t.Skipf("baseline outstanding Plaid cleanup count is already %d — another marked user exists in this shared dev DB outside this test's control", count)
	}
}

// createPlaidCleanupTestUser creates a minimal read-only-status user via ProfileRepository.CreateUserTx
// and registers its cleanup. Mirrors profile_repository_plaid_disconnect_repair_integration_test.go's
// inline fixture construction.
func createPlaidCleanupTestUser(t *testing.T, pool *pgxpool.Pool, enc *appcrypto.FieldEncryptor, suffix, status string) *domain.User {
	t.Helper()
	profileRepo := NewProfileRepository(pool, enc)
	ctx := context.Background()

	user := &domain.User{
		ClerkUserID:        "clerk_" + suffix,
		Email:              suffix + "@example.com",
		SubscriptionTier:   "premium",
		SubscriptionStatus: status,
		PrimaryRegion:      "iad",
		IsActive:           true,
	}
	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	require.NoError(t, profileRepo.CreateUserTx(ctx, tx, user))
	require.NoError(t, tx.Commit(ctx))
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, "DELETE FROM linked_items WHERE user_id = $1", user.ID)
		_, _ = pool.Exec(ctx, "DELETE FROM users WHERE id = $1", user.ID)
	})
	return user
}

// TestJobHealthRepository_Integration_NoOutstandingPlaidCleanup_ReturnsZero proves a healthy
// system (no outstanding Plaid-disconnect cleanup at all) reports both count and age zero — the
// gauge's steady state.
func TestJobHealthRepository_Integration_NoOutstandingPlaidCleanup_ReturnsZero(t *testing.T) {
	pool := connectTestDB(t)
	repo := NewJobHealthRepository(pool)
	ctx := context.Background()

	skipIfPlaidCleanupBaselineNonZero(t, repo, ctx)

	count, age, err := repo.GetOutstandingPlaidCleanupStats(ctx)
	require.NoError(t, err)
	assert.Equal(t, 0, count)
	assert.Equal(t, time.Duration(0), age)
}

// TestJobHealthRepository_Integration_MarkedUserWithLiveItem_CountedWithAge proves the direct-hit
// case: a user with plaid_cleanup_pending_at set AND a live linked item is counted, and the age
// reported is the actual elapsed time since the marker was set.
func TestJobHealthRepository_Integration_MarkedUserWithLiveItem_CountedWithAge(t *testing.T) {
	pool := connectTestDB(t)
	enc := newTestFieldEncryptor(t)
	repo := NewJobHealthRepository(pool)
	ctx := context.Background()

	skipIfPlaidCleanupBaselineNonZero(t, repo, ctx)

	suffix := fmt.Sprintf("jh_plaid_live_%d", time.Now().UnixNano())
	user := createPlaidCleanupTestUser(t, pool, enc, suffix, "cancelled")

	seedLinkedItem(t, pool, user.ID, "item-"+suffix, "active")
	setPlaidCleanupPendingAt(t, pool, user.ID, time.Now().Add(-5*24*time.Hour))

	count, age, err := repo.GetOutstandingPlaidCleanupStats(ctx)
	require.NoError(t, err)
	assert.Equal(t, 1, count, "a marked user with a live linked item must be counted")
	assert.InDelta(t, (5 * 24 * time.Hour).Seconds(), age.Seconds(), 30,
		"age must reflect the actual time since plaid_cleanup_pending_at was set, within a 30s test-execution tolerance")
}

// TestJobHealthRepository_Integration_MarkedUserNoLiveItems_NotCounted is the migration-residue RED
// test this file exists to pin down: a user whose marker is set (as migrations 000119/000120 did
// for EVERY inactive/cancelled user, live item or not) but who has NO live linked item at all must
// NOT be counted — otherwise this gauge reports a large permanent phantom backlog on day one.
//
// Falsifiability: dropping the EXISTS-subquery liveness join from GetOutstandingPlaidCleanupStats's
// query (counting on `plaid_cleanup_pending_at IS NOT NULL` alone) makes this test fail — the
// residue row would be counted.
func TestJobHealthRepository_Integration_MarkedUserNoLiveItems_NotCounted(t *testing.T) {
	pool := connectTestDB(t)
	enc := newTestFieldEncryptor(t)
	repo := NewJobHealthRepository(pool)
	ctx := context.Background()

	before, _, err := repo.GetOutstandingPlaidCleanupStats(ctx)
	require.NoError(t, err)

	suffix := fmt.Sprintf("jh_plaid_residue_%d", time.Now().UnixNano())
	user := createPlaidCleanupTestUser(t, pool, enc, suffix, "cancelled")
	// Deliberately NO linked_items row at all — the migration-residue shape: marker set on a user
	// who never linked (or was already fully disconnected from) any Plaid item.
	setPlaidCleanupPendingAt(t, pool, user.ID, time.Now().Add(-30*24*time.Hour))

	after, _, err := repo.GetOutstandingPlaidCleanupStats(ctx)
	require.NoError(t, err)
	assert.Equal(t, before, after, "a marked user with NO live linked items (migration-residue case) must not be counted")
}

// TestJobHealthRepository_Integration_UserWithLiveItemsNoMarker_NotCounted is the mirror-image
// negative control: a user with a perfectly live linked item but NO plaid_cleanup_pending_at
// marker (the overwhelming common case — nothing pending) must not be counted.
//
// Falsifiability: dropping the `plaid_cleanup_pending_at IS NOT NULL` clause (letting SQL's
// `NULL < ...`-style looseness default to inclusion) makes this test fail.
func TestJobHealthRepository_Integration_UserWithLiveItemsNoMarker_NotCounted(t *testing.T) {
	pool := connectTestDB(t)
	enc := newTestFieldEncryptor(t)
	repo := NewJobHealthRepository(pool)
	ctx := context.Background()

	before, _, err := repo.GetOutstandingPlaidCleanupStats(ctx)
	require.NoError(t, err)

	suffix := fmt.Sprintf("jh_plaid_nomarker_%d", time.Now().UnixNano())
	user := createPlaidCleanupTestUser(t, pool, enc, suffix, "cancelled")
	seedLinkedItem(t, pool, user.ID, "item-"+suffix, "active")
	// plaid_cleanup_pending_at deliberately left NULL (CreateUserTx's default) — nothing pending.

	after, _, err := repo.GetOutstandingPlaidCleanupStats(ctx)
	require.NoError(t, err)
	assert.Equal(t, before, after, "a user with a live linked item but NO plaid_cleanup_pending_at marker must not be counted")
}
