//go:build integration

package database

// Integration tests for migration 000120 (fix_plaid_cleanup_pending_at_backfill) — Issue #1413
// round-12 Fix 5.
//
// 000119's original backfill computed
// GREATEST(COALESCE(retention_started_at, updated_at, NOW()), NOW() - INTERVAL '48 hours'), which
// could adopt a timestamp belonging to an EARLIER Plaid-ineligibility episode via the unsound
// retention_started_at proxy — see 000119's own header comment. 000120 corrects this by
// re-stamping plaid_cleanup_pending_at to the instant IT runs, for every currently-eligible,
// uncomped user with cleanup still outstanding (marker already non-NULL) — guaranteeing a full
// fresh grace window regardless of what 000119 (or any other stale write) computed.
//
// Each scenario re-applies the real 000120 up SQL (read from the migration file, not a
// hand-copied restatement) inside a rolled-back transaction, mirroring
// migration_000100_integration_test.go's established pattern in this same package.
//
// Run with:
//
//	DATABASE_URL=postgres://... go test ./internal/database/... \
//	  -run TestMigration000120 -tags integration -v

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/maxnussbaum/BoostedBudget/internal/testsupport/pgtime"
)

// applyMigration000120 runs the real 000120 up SQL in tx — reading from the actual file (not a
// hand-copied restatement) so this test exercises the production SQL verbatim.
func applyMigration000120(t *testing.T, tx pgx.Tx) {
	t.Helper()
	upSQL := readMigrationFile(t, "000120_fix_plaid_cleanup_pending_at_backfill.up.sql")
	_, err := tx.Exec(context.Background(), upSQL)
	require.NoError(t, err, "migration 000120 failed")
}

// seedMig120User inserts a minimal users row with the given subscription_status,
// plaid_cleanup_pending_at, and (optional) manual tier override, returning its UUID.
func seedMig120User(
	t *testing.T, tx pgx.Tx, suffix, status string,
	pendingAt *time.Time, manualOverride *string, overrideEndsAt *time.Time,
) string {
	t.Helper()
	userID := uuid.New().String()
	_, err := tx.Exec(context.Background(), `
		INSERT INTO users (
			id, clerk_user_id, email, subscription_tier, subscription_status, primary_region,
			plaid_cleanup_pending_at, manual_tier_override, manual_tier_ends_at
		)
		VALUES ($1, $2, $3, 'premium', $4, 'iad', $5, $6, $7)
	`, userID, "test|mig120-"+suffix+"-"+userID, "mig120-"+suffix+"-"+userID+"@example.com",
		status, pendingAt, manualOverride, overrideEndsAt)
	require.NoError(t, err)
	return userID
}

func getMig120PlaidCleanupPendingAt(t *testing.T, tx pgx.Tx, userID string) *time.Time {
	t.Helper()
	var pendingAt *time.Time
	require.NoError(t, tx.QueryRow(context.Background(),
		"SELECT plaid_cleanup_pending_at FROM users WHERE id = $1", userID).Scan(&pendingAt))
	return pendingAt
}

func TestMigration000120_FixPlaidCleanupPendingAtBackfill(t *testing.T) {
	pool := connectMigrationTestDB(t)

	// ─── Scenario A: a stale marker (simulating 000119's flawed retention_started_at-derived
	// backfill) on a currently-eligible, uncomped user must be refreshed to a FULL fresh grace
	// window — not merely bumped forward by some partial amount. ───────────────────────────────
	t.Run("ScenarioA_StaleMarkerRefreshedToFullGraceWindow", func(t *testing.T) {
		ctx := context.Background()
		suffix := fmt.Sprintf("A-%d", time.Now().UnixNano())

		tx, err := pool.Begin(ctx)
		require.NoError(t, err)
		defer tx.Rollback(ctx) //nolint:errcheck

		// Stale marker: 100 hours ago — already well past the 72h repair grace window, exactly
		// the "disconnected almost immediately after deploy" defect this migration closes.
		stale := time.Now().Add(-100 * time.Hour)
		userID := seedMig120User(t, tx, suffix, "cancelled", &stale, nil, nil)

		var fence time.Time
		require.NoError(t, tx.QueryRow(ctx, "SELECT NOW()").Scan(&fence))

		applyMigration000120(t, tx)

		got := getMig120PlaidCleanupPendingAt(t, tx, userID)
		require.NotNil(t, got, "marker must remain non-NULL after correction")
		assert.False(t, got.Before(fence),
			"the corrected marker must be fresh (>= the fence taken before the migration ran), not merely bumped forward from the stale value")
	})

	// ─── Scenario B: a user with NO cleanup pending (marker already NULL — e.g. already
	// repaired, or never eligible) must be left completely untouched. The migration must never
	// INVENT pending work where the invariant says none exists (Issue #1413 round-12 Fix 4's exact
	// hazard, applied to this migration). ──────────────────────────────────────────────────────
	t.Run("ScenarioB_NullMarkerLeftUntouched", func(t *testing.T) {
		ctx := context.Background()
		suffix := fmt.Sprintf("B-%d", time.Now().UnixNano())

		tx, err := pool.Begin(ctx)
		require.NoError(t, err)
		defer tx.Rollback(ctx) //nolint:errcheck

		userID := seedMig120User(t, tx, suffix, "cancelled", nil, nil, nil)

		applyMigration000120(t, tx)

		got := getMig120PlaidCleanupPendingAt(t, tx, userID)
		assert.Nil(t, got, "a user with no cleanup pending (NULL marker) must not have one invented by this migration")
	})

	// ─── Scenario C: an active (Plaid-eligible) user must never be touched, regardless of
	// whatever stray value plaid_cleanup_pending_at might hold. ────────────────────────────────
	t.Run("ScenarioC_ActiveUserUntouched", func(t *testing.T) {
		ctx := context.Background()
		suffix := fmt.Sprintf("C-%d", time.Now().UnixNano())

		tx, err := pool.Begin(ctx)
		require.NoError(t, err)
		defer tx.Rollback(ctx) //nolint:errcheck

		// Exercise Linux-style nanosecond precision even on a microsecond-resolution host clock.
		stray := time.Now().Add(-200 * time.Hour).Truncate(time.Microsecond).Add(527 * time.Nanosecond)
		userID := seedMig120User(t, tx, suffix, "active", &stray, nil, nil)

		applyMigration000120(t, tx)

		got := getMig120PlaidCleanupPendingAt(t, tx, userID)
		require.NotNil(t, got)
		assert.Equal(t, pgtime.Stored(stray), got.UTC(), "an active user's marker must be left completely untouched by this migration")
	})

	// ─── Scenario D: a LAPSED (expired, never cleared) manual tier override must NOT exempt a
	// user's stale marker from correction — Issue #1413 round-12 Fix 2's live-only override
	// semantics, mirrored here. ─────────────────────────────────────────────────────────────────
	t.Run("ScenarioD_LapsedOverrideStillCorrected", func(t *testing.T) {
		ctx := context.Background()
		suffix := fmt.Sprintf("D-%d", time.Now().UnixNano())

		tx, err := pool.Begin(ctx)
		require.NoError(t, err)
		defer tx.Rollback(ctx) //nolint:errcheck

		stale := time.Now().Add(-100 * time.Hour)
		lapsedEnds := time.Now().Add(-24 * time.Hour)
		override := "household_2"
		userID := seedMig120User(t, tx, suffix, "cancelled", &stale, &override, &lapsedEnds)

		var fence time.Time
		require.NoError(t, tx.QueryRow(ctx, "SELECT NOW()").Scan(&fence))

		applyMigration000120(t, tx)

		got := getMig120PlaidCleanupPendingAt(t, tx, userID)
		require.NotNil(t, got)
		assert.False(t, got.Before(fence), "a LAPSED override must not exempt a user's stale marker from correction")
	})

	// ─── Scenario E: a LIVE manual tier override must exempt a user's marker from correction —
	// mirrors GetUsersNeedingPlaidDisconnectRepair's noActiveManualOverridePredicate exactly. ────
	t.Run("ScenarioE_LiveOverrideUntouched", func(t *testing.T) {
		ctx := context.Background()
		suffix := fmt.Sprintf("E-%d", time.Now().UnixNano())

		tx, err := pool.Begin(ctx)
		require.NoError(t, err)
		defer tx.Rollback(ctx) //nolint:errcheck

		stale := time.Now().Add(-100 * time.Hour)
		liveEnds := time.Now().Add(24 * time.Hour)
		override := "household_2"
		userID := seedMig120User(t, tx, suffix, "cancelled", &stale, &override, &liveEnds)

		applyMigration000120(t, tx)

		got := getMig120PlaidCleanupPendingAt(t, tx, userID)
		require.NotNil(t, got)
		assert.Equal(t, pgtime.Stored(stale), got.UTC(), "a user with a CURRENTLY-ACTIVE override must be left untouched by this migration")
	})

	// ─── Scenario F: idempotency — running the up SQL twice must not error and must leave a
	// still-eligible user's marker freshly stamped both times. ─────────────────────────────────
	t.Run("ScenarioF_IdempotentReapply", func(t *testing.T) {
		ctx := context.Background()
		suffix := fmt.Sprintf("F-%d", time.Now().UnixNano())

		tx, err := pool.Begin(ctx)
		require.NoError(t, err)
		defer tx.Rollback(ctx) //nolint:errcheck

		stale := time.Now().Add(-100 * time.Hour)
		userID := seedMig120User(t, tx, suffix, "cancelled", &stale, nil, nil)

		applyMigration000120(t, tx)
		firstPass := getMig120PlaidCleanupPendingAt(t, tx, userID)
		require.NotNil(t, firstPass)

		applyMigration000120(t, tx) // re-apply — must not error
		secondPass := getMig120PlaidCleanupPendingAt(t, tx, userID)
		require.NotNil(t, secondPass)
		assert.False(t, secondPass.Before(*firstPass), "re-applying must not move the marker backwards")
	})
}
