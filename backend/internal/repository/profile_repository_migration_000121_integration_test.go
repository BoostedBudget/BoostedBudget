//go:build integration

package repository

// Integration tests for migration 000121 (backfill_plaid_cleanup_pending_at_excluded_overrides) —
// Issue #1413 round-14.
//
// 000119's original backfill WHERE ended `AND (manual_tier_override IS NULL OR
// manual_tier_override = '')`, which skipped EVERY row carrying any override value — live OR
// already-lapsed (000119 had no manual_tier_ends_at check at all). 000120 cannot repair these:
// its own WHERE requires plaid_cleanup_pending_at IS NOT NULL, so it only re-stamps a marker that
// already exists. Round 13 (36455e78) restated the invariant this column maintains to be
// override-independent — non-NULL exactly when a raw-status ineligibility episode has begun and
// cleanup has not completed — so every row 000119 skipped for carrying an override should have
// carried a marker all along. Migration 000121 is the data half of that already-shipped rule
// change.
//
// Fixture honesty note (Issue #1413 round-14 task brief): the target row shape here — raw
// subscription_status IN ('inactive','cancelled') with plaid_cleanup_pending_at IS NULL — is
// reachable through CreateUserTx with ZERO raw SQL, because CreateUserTx's INSERT column list
// never includes plaid_cleanup_pending_at (see TestPlaidDisconnectRepair_Integration_NullMarker_Excluded
// in this package, which already establishes this exact construction as this file's convention for
// "no marker" fixtures). That is NOT, however, how such a row could arise via a REALISTIC
// active/trial -> cancelled TRANSITION under TODAY's code: ExpireTrialIfNoOverride,
// the deleted CancelIfPastDueOccurrenceWithRetention (#1652), and UpdateSubscriptionFromPaddle all now stamp
// plaid_cleanup_pending_at atomically with the SAME statement that flips subscription_status into
// the repair-eligible set (round 13), regardless of any override, live or lapsed — see each
// method's doc comment in profile_repository.go. So a fresh signup run through any of today's real
// transition writers can NEVER end up in the shape this migration targets; that shape can only be
// historical residue predating round 13 (or created directly, as here, to stand in for it). This
// is the honest finding the task brief asked to surface explicitly: seeding this fixture uses only
// production writers (CreateUserTx + AdminUserRepository.UpdateUserTier, never a raw UPDATE against
// plaid_cleanup_pending_at), but no SEQUENCE of today's writers, starting from a normal signup,
// could recreate it — the residual population migration 000121 repairs is historical only.
//
// Each scenario applies the real 000121 up SQL (read from the migration file, not a hand-copied
// restatement), mirroring migration_000120_integration_test.go's (internal/database) established
// pattern, adapted to this package so GetUsersNeedingPlaidDisconnectRepair itself can be exercised
// before and after.
//
// Run with:
//
//	DATABASE_URL=postgres://... go test ./internal/repository/... \
//	  -run TestMigration000121 -tags integration -v
//	  (also covered by the CI integration path: ./internal/repository -tags integration -p 1)

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
)

// migration000121UpSQL reads the real 000121 up migration file from backend/migrations/ — go test
// sets CWD to the package directory, so runtime.Caller is used (matching
// internal/database/migration_000100_integration_test.go's migrationDir helper) rather than a bare
// relative path, for stability independent of how the test binary is invoked.
func migration000121UpSQL(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	require.True(t, ok, "runtime.Caller failed")
	dir := filepath.Join(filepath.Dir(thisFile), "..", "..", "migrations")
	b, err := os.ReadFile(filepath.Join(dir, "000121_backfill_plaid_cleanup_pending_at_excluded_overrides.up.sql"))
	require.NoError(t, err, "failed to read migration 000121 up.sql")
	return string(b)
}

// mig121FindUser reports whether userID is present in candidates.
func mig121FindUser(candidates []*domain.User, userID string) bool {
	for _, c := range candidates {
		if c.ID == userID {
			return true
		}
	}
	return false
}

// mig121GetPlaidCleanupPendingAt reads plaid_cleanup_pending_at directly for assertions the
// repository's own query methods don't expose (e.g. checking the stamped value's shape).
func mig121GetPlaidCleanupPendingAt(t *testing.T, repo *profileRepository, userID string) *time.Time {
	t.Helper()
	var pendingAt *time.Time
	require.NoError(t, repo.db.QueryRow(context.Background(),
		"SELECT plaid_cleanup_pending_at FROM users WHERE id = $1", userID).Scan(&pendingAt))
	return pendingAt
}

// TestMigration000121_LapsedOverrideRowNowRepaired proves the core gap this migration closes: a
// cancelled user with a LAPSED (expired, never removed) manual tier override, a live linked item,
// and a NULL plaid_cleanup_pending_at marker — exactly the row shape 000119's override exclusion
// permanently skipped — is invisible to GetUsersNeedingPlaidDisconnectRepair BEFORE migration
// 000121 runs, and becomes visible immediately AFTER it runs, with no other row property changed.
//
// Falsifiability: reintroducing 000119's manual_tier_override exclusion (NULL-or-empty-string)
// into 000121's up.sql — i.e. repeating 000119's bug — makes the "after" assertion fail: this
// exact row would be skipped again.
func TestMigration000121_LapsedOverrideRowNowRepaired(t *testing.T) {
	pool := connectTestDB(t)
	enc := newTestFieldEncryptor(t)
	repo := NewProfileRepository(pool, enc)
	adminRepo := NewAdminUserRepository(pool, enc)
	ctx := context.Background()

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	user := &domain.User{
		ClerkUserID:        "clerk_mig121gap_" + suffix,
		Email:              "mig121gap-" + suffix + "@example.com",
		SubscriptionTier:   "premium",
		SubscriptionStatus: "cancelled",
		PrimaryRegion:      "iad",
		IsActive:           true,
	}
	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	require.NoError(t, repo.CreateUserTx(ctx, tx, user))
	require.NoError(t, tx.Commit(ctx))
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, "DELETE FROM linked_items WHERE user_id = $1", user.ID)
		_, _ = pool.Exec(ctx, "DELETE FROM users WHERE id = $1", user.ID)
	})

	// Live linked item — required for both the repair query's EXISTS filter and the migration's
	// own copy of that same predicate (see 000121's up.sql header comment).
	seedLinkedItem(t, pool, user.ID, "item-mig121gap-"+suffix, "active")

	// Lapsed override granted via the real admin writer — expiresAt already in the past models a
	// comp that lapsed and was never removed, without needing to sleep in wall-clock time.
	// UpdateUserTier deliberately never touches plaid_cleanup_pending_at (see updateUserTierQuery's
	// doc comment, admin_user_repository.go) — the marker stays exactly as CreateUserTx left it.
	lapsedEnds := time.Now().Add(-24 * time.Hour)
	_, err = adminRepo.UpdateUserTier(ctx, "", user.ID, "household_2", "test: lapsed comp, never removed", &lapsedEnds)
	require.NoError(t, err)

	// plaid_cleanup_pending_at deliberately left NULL — CreateUserTx never sets it, and
	// UpdateUserTier never touches it either. This is the exact shape 000119 permanently skipped.

	// ─── Before: invisible to the repair pass, no matter the grace period. ───────────────────
	before, err := repo.GetUsersNeedingPlaidDisconnectRepair(ctx, testGracePeriod)
	require.NoError(t, err)
	assert.False(t, mig121FindUser(before, user.ID),
		"BEFORE migration 000121: a cancelled user with a lapsed override, a live item, and a NULL marker must NOT be selected — this is the bug")

	// ─── Apply the real migration 000121 up SQL. ──────────────────────────────────────────────
	// Both bounds below are fenced with Postgres's own clock rather than Go's. The migration writes
	// this column from Postgres NOW(), and CI runs Postgres in a separate container whose clock can
	// lag the runner's — a Go-side bound needs an arbitrary tolerance to absorb that skew, and the
	// sibling floor test failed CI for exactly this reason. Same-clock fences need no tolerance:
	// NOW() only advances, so floorFence <= stamped <= upperFence holds exactly.
	var floorFence time.Time
	require.NoError(t, pool.QueryRow(ctx, "SELECT NOW() - INTERVAL '48 hours'").Scan(&floorFence))

	_, err = pool.Exec(ctx, migration000121UpSQL(t))
	require.NoError(t, err, "migration 000121 failed")

	var upperFence time.Time
	require.NoError(t, pool.QueryRow(ctx, "SELECT NOW()").Scan(&upperFence))

	pendingAt := mig121GetPlaidCleanupPendingAt(t, repo, user.ID)
	require.NotNil(t, pendingAt, "migration 000121 must stamp the marker for this row")
	assert.False(t, pendingAt.Before(floorFence), "stamped value must respect the 48-hour floor")
	assert.False(t, pendingAt.After(upperFence), "stamped value must not be in the future")

	// ─── After: visible to the repair pass — the exact bug closed. ───────────────────────────
	after, err := repo.GetUsersNeedingPlaidDisconnectRepair(ctx, 0)
	require.NoError(t, err)
	assert.True(t, mig121FindUser(after, user.ID),
		"AFTER migration 000121: the same row must now be selected by the repair pass")
}

// TestMigration000121_LiveOverrideStampedButQueryExcluded proves round 13's core design point,
// restated for this migration: it deliberately does NOT filter on manual_tier_override, so a row
// with a CURRENTLY-LIVE override is stamped exactly like a lapsed one — but
// GetUsersNeedingPlaidDisconnectRepair's own noActiveManualOverridePredicate still correctly
// exempts it at query time. The durable fact (column) and current eligibility (query) are
// deliberately independent.
func TestMigration000121_LiveOverrideStampedButQueryExcluded(t *testing.T) {
	pool := connectTestDB(t)
	enc := newTestFieldEncryptor(t)
	repo := NewProfileRepository(pool, enc)
	adminRepo := NewAdminUserRepository(pool, enc)
	ctx := context.Background()

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	user := &domain.User{
		ClerkUserID:        "clerk_mig121live_" + suffix,
		Email:              "mig121live-" + suffix + "@example.com",
		SubscriptionTier:   "premium",
		SubscriptionStatus: "cancelled",
		PrimaryRegion:      "iad",
		IsActive:           true,
	}
	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	require.NoError(t, repo.CreateUserTx(ctx, tx, user))
	require.NoError(t, tx.Commit(ctx))
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, "DELETE FROM linked_items WHERE user_id = $1", user.ID)
		_, _ = pool.Exec(ctx, "DELETE FROM users WHERE id = $1", user.ID)
	})

	seedLinkedItem(t, pool, user.ID, "item-mig121live-"+suffix, "active")

	liveEnds := time.Now().Add(24 * time.Hour)
	_, err = adminRepo.UpdateUserTier(ctx, "", user.ID, "household_2", "test: currently-live comp", &liveEnds)
	require.NoError(t, err)

	_, err = pool.Exec(ctx, migration000121UpSQL(t))
	require.NoError(t, err, "migration 000121 failed")

	pendingAt := mig121GetPlaidCleanupPendingAt(t, repo, user.ID)
	require.NotNil(t, pendingAt, "a LIVE override must NOT exempt a row from being stamped — that exclusion is 000119's bug")

	candidates, err := repo.GetUsersNeedingPlaidDisconnectRepair(ctx, 0)
	require.NoError(t, err)
	assert.False(t, mig121FindUser(candidates, user.ID),
		"a CURRENTLY-LIVE override must still exclude the user from the repair pass at query time, even though the column is now stamped")
}

// TestMigration000121_NoLinkedItemNotStamped proves the migration requires a live linked item —
// not merely as an optimization, but because stamping a user with NO items at all would recreate
// the exact phantom-marker residue 000119/000120 already left behind (tracked separately as Issue
// #1453): the repair pass's EXISTS filter would exclude such a user before ever reaching
// ClearPlaidCleanupPendingIfMatches, leaving a non-NULL marker with no writer able to clear it.
func TestMigration000121_NoLinkedItemNotStamped(t *testing.T) {
	pool := connectTestDB(t)
	enc := newTestFieldEncryptor(t)
	repo := NewProfileRepository(pool, enc)
	adminRepo := NewAdminUserRepository(pool, enc)
	ctx := context.Background()

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	user := &domain.User{
		ClerkUserID:        "clerk_mig121noitem_" + suffix,
		Email:              "mig121noitem-" + suffix + "@example.com",
		SubscriptionTier:   "premium",
		SubscriptionStatus: "cancelled",
		PrimaryRegion:      "iad",
		IsActive:           true,
	}
	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	require.NoError(t, repo.CreateUserTx(ctx, tx, user))
	require.NoError(t, tx.Commit(ctx))
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, "DELETE FROM users WHERE id = $1", user.ID)
	})

	lapsedEnds := time.Now().Add(-24 * time.Hour)
	_, err = adminRepo.UpdateUserTier(ctx, "", user.ID, "household_2", "test: lapsed comp, no linked items", &lapsedEnds)
	require.NoError(t, err)

	// Deliberately NO linked_items row seeded at all.

	_, err = pool.Exec(ctx, migration000121UpSQL(t))
	require.NoError(t, err, "migration 000121 failed")

	pendingAt := mig121GetPlaidCleanupPendingAt(t, repo, user.ID)
	assert.Nil(t, pendingAt,
		"a user with NO linked items at all must be left untouched — stamping them would create an unclearable marker (Issue #1453)")
}

// TestMigration000121_IdempotentReapply proves re-running the up SQL is a no-op for a row it
// already stamped — the WHERE plaid_cleanup_pending_at IS NULL guard means nothing is left to
// match on a second pass.
func TestMigration000121_IdempotentReapply(t *testing.T) {
	pool := connectTestDB(t)
	enc := newTestFieldEncryptor(t)
	repo := NewProfileRepository(pool, enc)
	adminRepo := NewAdminUserRepository(pool, enc)
	ctx := context.Background()

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	user := &domain.User{
		ClerkUserID:        "clerk_mig121idem_" + suffix,
		Email:              "mig121idem-" + suffix + "@example.com",
		SubscriptionTier:   "premium",
		SubscriptionStatus: "cancelled",
		PrimaryRegion:      "iad",
		IsActive:           true,
	}
	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	require.NoError(t, repo.CreateUserTx(ctx, tx, user))
	require.NoError(t, tx.Commit(ctx))
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, "DELETE FROM linked_items WHERE user_id = $1", user.ID)
		_, _ = pool.Exec(ctx, "DELETE FROM users WHERE id = $1", user.ID)
	})

	seedLinkedItem(t, pool, user.ID, "item-mig121idem-"+suffix, "active")

	lapsedEnds := time.Now().Add(-24 * time.Hour)
	_, err = adminRepo.UpdateUserTier(ctx, "", user.ID, "household_2", "test: idempotency", &lapsedEnds)
	require.NoError(t, err)

	upSQL := migration000121UpSQL(t)

	_, err = pool.Exec(ctx, upSQL)
	require.NoError(t, err, "first apply of migration 000121 failed")
	firstStamp := mig121GetPlaidCleanupPendingAt(t, repo, user.ID)
	require.NotNil(t, firstStamp)

	_, err = pool.Exec(ctx, upSQL) // re-apply — must not error
	require.NoError(t, err, "second apply of migration 000121 failed")
	secondStamp := mig121GetPlaidCleanupPendingAt(t, repo, user.ID)
	require.NotNil(t, secondStamp)
	assert.True(t, secondStamp.Equal(*firstStamp),
		"re-applying must not re-stamp an already-non-NULL marker — WHERE plaid_cleanup_pending_at IS NULL must guard idempotency")
}

// TestMigration000121_FloorsAt48Hours proves the grace-window floor: a row whose only available
// proxy timestamp (retention_started_at) is old enough that GREATEST(...) would otherwise adopt it
// verbatim must instead be floored at NOW() - 48 hours — mirroring 000119's own precedent (which
// cites migration 000105) — so a freshly backfilled row is never immediately past the repair
// pass's 72-hour grace window on the very first cron run after deploy.
func TestMigration000121_FloorsAt48Hours(t *testing.T) {
	pool := connectTestDB(t)
	enc := newTestFieldEncryptor(t)
	repo := NewProfileRepository(pool, enc)
	adminRepo := NewAdminUserRepository(pool, enc)
	ctx := context.Background()

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	user := &domain.User{
		ClerkUserID:        "clerk_mig121floor_" + suffix,
		Email:              "mig121floor-" + suffix + "@example.com",
		SubscriptionTier:   "premium",
		SubscriptionStatus: "cancelled",
		PrimaryRegion:      "iad",
		IsActive:           true,
	}
	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	require.NoError(t, repo.CreateUserTx(ctx, tx, user))
	require.NoError(t, tx.Commit(ctx))
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, "DELETE FROM linked_items WHERE user_id = $1", user.ID)
		_, _ = pool.Exec(ctx, "DELETE FROM users WHERE id = $1", user.ID)
	})

	seedLinkedItem(t, pool, user.ID, "item-mig121floor-"+suffix, "active")

	lapsedEnds := time.Now().Add(-24 * time.Hour)
	_, err = adminRepo.UpdateUserTier(ctx, "", user.ID, "household_2", "test: 48h floor", &lapsedEnds)
	require.NoError(t, err)

	// Back-date retention_started_at to 100 hours ago — well past the 48h floor — so GREATEST
	// would adopt it verbatim without the floor. This mirrors this package's own established
	// technique for modeling column age directly (setRetentionStartedAt / setPlaidCleanupPendingAt
	// above), not a raw UPDATE against plaid_cleanup_pending_at itself.
	setRetentionStartedAt(t, pool, user.ID, time.Now().Add(-100*time.Hour))

	// The fence MUST come from Postgres, not from Go's clock. plaid_cleanup_pending_at is written
	// by the migration as Postgres's own `NOW() - INTERVAL '48 hours'`; CI runs Postgres in a
	// separate container whose clock can lag the runner's by milliseconds, so a Go-side
	// time.Now().Add(-48h) sits *after* the value Postgres wrote and this assertion flips. That
	// passes locally (one host clock) and fails only in CI — which is exactly what happened here,
	// and on commit ff2cc4bb before it. Taking the fence before the migration also keeps the
	// comparison sound in the other direction: NOW() only advances, so the stamped value can never
	// precede this fence.
	var fence time.Time
	require.NoError(t, pool.QueryRow(ctx, "SELECT NOW() - INTERVAL '48 hours'").Scan(&fence))

	_, err = pool.Exec(ctx, migration000121UpSQL(t))
	require.NoError(t, err, "migration 000121 failed")

	pendingAt := mig121GetPlaidCleanupPendingAt(t, repo, user.ID)
	require.NotNil(t, pendingAt)
	assert.False(t, pendingAt.Before(fence),
		"a 100-hour-old retention_started_at proxy must be floored at NOW() - 48h, not adopted verbatim")
}
