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

// TestJobHealthRepository_Integration_MarkedUserWithLiveItemAndLiveComp_StillCounted is the Task X
// M3 RED test (Codex adversarial review round on PR #1443): a user with a marker set AND a live
// linked item AND a CURRENTLY-LIVE manual tier override must still count toward this gauge.
//
// GetUsersNeedingPlaidDisconnectRepair correctly excludes such a user from being acted on THIS
// run — that predicate answers "whom do I repair right now", and a live comp must pause repair.
// But this gauge answers a different question — "how much cleanup is outstanding" — and a comp
// can live for months. A comped user genuinely has outstanding cleanup; they are merely exempt
// from being acted on right now. Hiding them from the gauge for the entire duration of the comp
// defeats its whole purpose of making a stuck backlog visible.
//
// Falsifiability: this test fails against the pre-fix query (which appended
// noActiveManualOverridePredicate to this gauge's WHERE clause) — the comped user vanishes from
// the count. It passes once that predicate is removed from GetOutstandingPlaidCleanupStats
// (the predicate itself, and the repair query's use of it, are unchanged and correct).
func TestJobHealthRepository_Integration_MarkedUserWithLiveItemAndLiveComp_StillCounted(t *testing.T) {
	pool := connectTestDB(t)
	enc := newTestFieldEncryptor(t)
	repo := NewJobHealthRepository(pool)
	adminRepo := NewAdminUserRepository(pool, enc)
	ctx := context.Background()

	skipIfPlaidCleanupBaselineNonZero(t, repo, ctx)

	suffix := fmt.Sprintf("jh_plaid_livecomp_%d", time.Now().UnixNano())
	user := createPlaidCleanupTestUser(t, pool, enc, suffix, "cancelled")

	seedLinkedItem(t, pool, user.ID, "item-"+suffix, "active")
	setPlaidCleanupPendingAt(t, pool, user.ID, time.Now().Add(-5*24*time.Hour))

	// Grant a currently-live comp through the real production writer (AdminUserRepository), not
	// raw SQL — this is a state a production admin action genuinely reaches.
	compEnds := time.Now().Add(48 * time.Hour) // still live
	_, err := adminRepo.UpdateUserTier(ctx, "", user.ID, "household_2", "test: live comp must not hide backlog", &compEnds)
	require.NoError(t, err)

	count, age, err := repo.GetOutstandingPlaidCleanupStats(ctx)
	require.NoError(t, err)
	assert.Equal(t, 1, count, "a user under a CURRENTLY-LIVE comp still has outstanding cleanup and must be counted — the comp only exempts them from being repaired THIS run, not from the outstanding-backlog gauge")
	assert.InDelta(t, (5 * 24 * time.Hour).Seconds(), age.Seconds(), 30,
		"age must still reflect the actual time since plaid_cleanup_pending_at was set")
}

// TestJobHealthRepository_Integration_MarkedUserWithNullStatusLiveItem_Counted is the NULL-status
// RED test this file was missing: linked_items.status is nullable (migration 000013), and a row
// inserted without an explicit status (or predating the 'active' default) can be genuinely live
// with a NULL status. GetUsersNeedingPlaidDisconnectRepair's own candidate query has always treated
// NULL as live (round-7 fix, TestPlaidDisconnectRepair_Integration_NullStatusItemIsLive,
// profile_repository_plaid_disconnect_repair_integration_test.go) via the shared
// liveLinkedItemExistsClause Go constant — but until now nothing pinned THIS gauge's query to that
// same rule specifically; every fixture above seeds an "active" item
// (createPlaidCleanupTestUser + seedLinkedItem), so a hand-copy of the clause that quietly dropped
// the `status IS NULL OR` arm (e.g. "simplified" to a bare `status NOT IN (...)`) would pass every
// other test in this file while silently undercounting the backlog for exactly the legacy
// migration-000013 population that predates the 'active' default.
//
// Falsifiability: replacing GetOutstandingPlaidCleanupStats's `AND ` + liveLinkedItemExistsClause +
// ` with the bare `linked_items.status NOT IN ('disconnected', 'replaced')` form makes this test
// fail — SQL's three-valued logic evaluates `NULL NOT IN (...)` as UNKNOWN, not TRUE, so the
// EXISTS subquery no longer matches this row and the marked user silently vanishes from the count.
func TestJobHealthRepository_Integration_MarkedUserWithNullStatusLiveItem_Counted(t *testing.T) {
	pool := connectTestDB(t)
	enc := newTestFieldEncryptor(t)
	repo := NewJobHealthRepository(pool)
	ctx := context.Background()

	skipIfPlaidCleanupBaselineNonZero(t, repo, ctx)

	suffix := fmt.Sprintf("jh_plaid_nullstatus_%d", time.Now().UnixNano())
	user := createPlaidCleanupTestUser(t, pool, enc, suffix, "cancelled")

	seedLinkedItemNullStatus(t, pool, user.ID, "item-"+suffix)
	setPlaidCleanupPendingAt(t, pool, user.ID, time.Now().Add(-5*24*time.Hour))

	count, age, err := repo.GetOutstandingPlaidCleanupStats(ctx)
	require.NoError(t, err)
	assert.Equal(t, 1, count, "a marked user whose only linked item has a NULL status must still be "+
		"counted — NULL is not 'disconnected' or 'replaced', so it counts as live")
	assert.InDelta(t, (5 * 24 * time.Hour).Seconds(), age.Seconds(), 30,
		"age must reflect the actual time since plaid_cleanup_pending_at was set, within a 30s test-execution tolerance")
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
