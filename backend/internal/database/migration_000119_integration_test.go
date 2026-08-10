//go:build integration

package database

// Integration tests for migration 000119 (add_plaid_cleanup_pending_at) — Issue #1457 Gap 2.
//
// 000119 both CREATES plaid_cleanup_pending_at and performs the SUBSYSTEM'S ORIGINAL backfill:
//
//	UPDATE users
//	SET plaid_cleanup_pending_at = GREATEST(
//	    COALESCE(retention_started_at, updated_at, NOW()),
//	    NOW() - INTERVAL '48 hours'
//	)
//	WHERE subscription_status IN ('inactive', 'cancelled')
//	  AND plaid_cleanup_pending_at IS NULL
//	  AND (manual_tier_override IS NULL OR manual_tier_override = '');
//
// 000120's own header comment (migration_000120_integration_test.go) explains why this anchor is
// UNSOUND: retention_started_at is written by several independent paths, is never cleared once a
// disconnect completes, and can therefore silently carry a PRIOR ineligibility episode's age — so
// 000119's population is the LEAST-VERIFIED one in this subsystem, and until this test, untested.
// 000120 corrects the SAME population by re-stamping to NOW() unconditionally; this test instead
// verifies 000119's OWN original arithmetic against the migration file exactly as it will run on a
// database that has never seen 000120 — i.e. what actually happened to every environment's data the
// first time this subsystem was deployed.
//
// Each scenario re-applies the real 000119 up SQL (read from the migration file, not a hand-copied
// restatement) inside a rolled-back transaction — mirrors migration_000120_integration_test.go's
// established pattern in this same package (Issue #1457's own instruction: copy that harness, not a
// hand-copied restatement).
//
// Run with:
//
//	DATABASE_URL=postgres://... go test ./internal/database/... \
//	  -run TestMigration000119 -tags integration -v

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// applyMigration000119 runs the real 000119 up SQL (ADD COLUMN + original backfill) in tx — reading
// from the actual file, not a hand-copied restatement, so this test exercises the production SQL
// verbatim.
func applyMigration000119(t *testing.T, tx pgx.Tx) {
	t.Helper()
	upSQL := readMigrationFile(t, "000119_add_plaid_cleanup_pending_at.up.sql")
	_, err := tx.Exec(context.Background(), upSQL)
	require.NoError(t, err, "migration 000119 failed")
}

// seedMig119User inserts a minimal users row with the given subscription_status,
// retention_started_at, updated_at, and (optional) manual tier override, returning its UUID.
// plaid_cleanup_pending_at is never set here — 000119's whole job is to populate it from NULL.
//
// updated_at is settable explicitly because migration 000001's update_users_updated_at trigger only
// fires BEFORE UPDATE, not BEFORE INSERT — an explicit INSERT value is never overwritten, which is
// exactly what lets Scenario C below control the COALESCE fallback precisely.
func seedMig119User(
	t *testing.T, tx pgx.Tx, suffix, status string,
	retentionStartedAt, updatedAt *time.Time,
	manualOverride *string,
) string {
	t.Helper()
	userID := uuid.New().String()
	_, err := tx.Exec(context.Background(), `
		INSERT INTO users (
			id, clerk_user_id, email, subscription_tier, subscription_status, primary_region,
			retention_started_at, updated_at, manual_tier_override
		)
		VALUES ($1, $2, $3, 'premium', $4, 'iad', $5, COALESCE($6, NOW()), $7)
	`, userID, "test|mig119-"+suffix+"-"+userID, "mig119-"+suffix+"-"+userID+"@example.com",
		status, retentionStartedAt, updatedAt, manualOverride)
	require.NoError(t, err)
	return userID
}

func getMig119PlaidCleanupPendingAt(t *testing.T, tx pgx.Tx, userID string) *time.Time {
	t.Helper()
	var pendingAt *time.Time
	require.NoError(t, tx.QueryRow(context.Background(),
		"SELECT plaid_cleanup_pending_at FROM users WHERE id = $1", userID).Scan(&pendingAt))
	return pendingAt
}

func TestMigration000119_AddPlaidCleanupPendingAtBackfill(t *testing.T) {
	pool := connectMigrationTestDB(t)

	// ─── Scenario A: retention_started_at is RECENT (well within the 48h cap) — GREATEST must pick
	// retention_started_at itself, not the cap. ──────────────────────────────────────────────────
	t.Run("ScenarioA_RecentRetentionStartedAtWins", func(t *testing.T) {
		ctx := context.Background()
		suffix := fmt.Sprintf("A-%d", time.Now().UnixNano())

		tx, err := pool.Begin(ctx)
		require.NoError(t, err)
		defer tx.Rollback(ctx) //nolint:errcheck

		retentionStarted := time.Now().Add(-10 * time.Hour).Truncate(time.Microsecond)
		userID := seedMig119User(t, tx, suffix, "cancelled", &retentionStarted, nil, nil)

		applyMigration000119(t, tx)

		got := getMig119PlaidCleanupPendingAt(t, tx, userID)
		require.NotNil(t, got, "an eligible user with plaid_cleanup_pending_at NULL must be backfilled")
		assert.True(t, retentionStarted.Equal(*got),
			"GREATEST(retention_started_at, NOW()-48h) must pick retention_started_at itself when it is more recent than the 48h cap")
	})

	// ─── Scenario B: retention_started_at is OLDER than the 48h cap — GREATEST must pick the cap
	// (NOW() - 48h), not the stale retention_started_at value. This is the "at least 24 hours of
	// grace" bound 000120's own header comment describes as insufficient — verified directly here. ─
	t.Run("ScenarioB_StaleRetentionStartedAtCappedAt48Hours", func(t *testing.T) {
		ctx := context.Background()
		suffix := fmt.Sprintf("B-%d", time.Now().UnixNano())

		tx, err := pool.Begin(ctx)
		require.NoError(t, err)
		defer tx.Rollback(ctx) //nolint:errcheck

		var fenceBefore time.Time
		require.NoError(t, tx.QueryRow(ctx, "SELECT NOW()").Scan(&fenceBefore))

		retentionStarted := time.Now().Add(-200 * time.Hour).Truncate(time.Microsecond)
		userID := seedMig119User(t, tx, suffix, "cancelled", &retentionStarted, nil, nil)

		applyMigration000119(t, tx)

		got := getMig119PlaidCleanupPendingAt(t, tx, userID)
		require.NotNil(t, got)
		assert.False(t, got.Equal(retentionStarted),
			"a retention_started_at older than the 48h cap must NOT survive uncapped into the marker")
		expectedCap := fenceBefore.Add(-48 * time.Hour)
		assert.WithinDuration(t, expectedCap, *got, 2*time.Second,
			"the capped value must be ~NOW()-48h (measured at the migration's own NOW()), not the stale 200h-old retention_started_at")
	})

	// ─── Scenario C: retention_started_at is NULL — COALESCE must fall through to updated_at. ─────
	t.Run("ScenarioC_NullRetentionStartedAtFallsBackToUpdatedAt", func(t *testing.T) {
		ctx := context.Background()
		suffix := fmt.Sprintf("C-%d", time.Now().UnixNano())

		tx, err := pool.Begin(ctx)
		require.NoError(t, err)
		defer tx.Rollback(ctx) //nolint:errcheck

		updatedAt := time.Now().Add(-5 * time.Hour).Truncate(time.Microsecond)
		userID := seedMig119User(t, tx, suffix, "inactive", nil, &updatedAt, nil)

		applyMigration000119(t, tx)

		got := getMig119PlaidCleanupPendingAt(t, tx, userID)
		require.NotNil(t, got)
		assert.True(t, updatedAt.Equal(*got),
			"with retention_started_at NULL, COALESCE must fall through to updated_at (more recent than the 48h cap)")
	})

	// ─── Scenario D: plaid_cleanup_pending_at is ALREADY non-NULL — must be left completely
	// untouched. 000119 only ever backfills a NULL marker, never overwrites an existing one. ───────
	t.Run("ScenarioD_AlreadySetMarkerUntouched", func(t *testing.T) {
		ctx := context.Background()
		suffix := fmt.Sprintf("D-%d", time.Now().UnixNano())

		tx, err := pool.Begin(ctx)
		require.NoError(t, err)
		defer tx.Rollback(ctx) //nolint:errcheck

		retentionStarted := time.Now().Add(-10 * time.Hour).Truncate(time.Microsecond)
		userID := seedMig119User(t, tx, suffix, "cancelled", &retentionStarted, nil, nil)

		existingMarker := time.Now().Add(-1000 * time.Hour).Truncate(time.Microsecond)
		_, err = tx.Exec(ctx, "UPDATE users SET plaid_cleanup_pending_at = $1 WHERE id = $2", existingMarker, userID)
		require.NoError(t, err)

		applyMigration000119(t, tx)

		got := getMig119PlaidCleanupPendingAt(t, tx, userID)
		require.NotNil(t, got)
		assert.True(t, existingMarker.Equal(*got),
			"a marker that is already non-NULL must never be overwritten by this backfill")
	})

	// ─── Scenario E: an ACTIVE (Plaid-eligible) user must never be touched, regardless of
	// retention_started_at/updated_at values. ──────────────────────────────────────────────────────
	t.Run("ScenarioE_ActiveUserUntouched", func(t *testing.T) {
		ctx := context.Background()
		suffix := fmt.Sprintf("E-%d", time.Now().UnixNano())

		tx, err := pool.Begin(ctx)
		require.NoError(t, err)
		defer tx.Rollback(ctx) //nolint:errcheck

		retentionStarted := time.Now().Add(-10 * time.Hour).Truncate(time.Microsecond)
		userID := seedMig119User(t, tx, suffix, "active", &retentionStarted, nil, nil)

		applyMigration000119(t, tx)

		got := getMig119PlaidCleanupPendingAt(t, tx, userID)
		assert.Nil(t, got, "an active (not inactive/cancelled) user must never have a marker invented for them")
	})

	// ─── Scenario F: a NON-EMPTY manual_tier_override excludes a user from this backfill —
	// regardless of manual_tier_ends_at. 000119's WHERE clause checks ONLY for override presence
	// (`manual_tier_override IS NULL OR manual_tier_override = ''`), unlike the round-12
	// noActiveManualOverridePredicate semantics later migrations/queries use — it has no
	// manual_tier_ends_at term at all, so even a value naming no configured tier (a malformed
	// override, Issue #1453) still excludes the row here. ──────────────────────────────────────────
	t.Run("ScenarioF_AnyNonEmptyOverrideExcluded", func(t *testing.T) {
		ctx := context.Background()
		suffix := fmt.Sprintf("F-%d", time.Now().UnixNano())

		tx, err := pool.Begin(ctx)
		require.NoError(t, err)
		defer tx.Rollback(ctx) //nolint:errcheck

		retentionStarted := time.Now().Add(-10 * time.Hour).Truncate(time.Microsecond)
		override := "household_2"
		userID := seedMig119User(t, tx, suffix, "cancelled", &retentionStarted, nil, &override)

		applyMigration000119(t, tx)

		got := getMig119PlaidCleanupPendingAt(t, tx, userID)
		assert.Nil(t, got, "a user with ANY non-empty manual_tier_override (live or lapsed — 000119 does not check manual_tier_ends_at at all) must be excluded from this backfill")
	})

	// ─── Scenario G: idempotency — running the up SQL twice must not error, and must not touch a
	// row the first pass already backfilled (marker is non-NULL after pass 1, so pass 2's own WHERE
	// plaid_cleanup_pending_at IS NULL excludes it). ────────────────────────────────────────────────
	t.Run("ScenarioG_IdempotentReapply", func(t *testing.T) {
		ctx := context.Background()
		suffix := fmt.Sprintf("G-%d", time.Now().UnixNano())

		tx, err := pool.Begin(ctx)
		require.NoError(t, err)
		defer tx.Rollback(ctx) //nolint:errcheck

		retentionStarted := time.Now().Add(-10 * time.Hour).Truncate(time.Microsecond)
		userID := seedMig119User(t, tx, suffix, "cancelled", &retentionStarted, nil, nil)

		applyMigration000119(t, tx)
		firstPass := getMig119PlaidCleanupPendingAt(t, tx, userID)
		require.NotNil(t, firstPass)

		applyMigration000119(t, tx) // re-apply — must not error
		secondPass := getMig119PlaidCleanupPendingAt(t, tx, userID)
		require.NotNil(t, secondPass)
		assert.True(t, firstPass.Equal(*secondPass), "re-applying must not change an already-backfilled marker")
	})
}
