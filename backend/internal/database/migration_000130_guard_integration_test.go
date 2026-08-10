//go:build integration

package database

// Integration tests for migration 000130's idempotency guard
// (user_subscription_overrides.{created_at,updated_at,expires_at}: naked TIMESTAMP ->
// TIMESTAMPTZ) — Issue #1464 follow-up.
//
// The guard originally inspected only created_at to decide whether the whole DO block
// should run. That is sufficient for the two states the migration's own comment claims are
// reachable (all three naked, or all three converted, since the three ALTER COLUMN TYPE
// statements execute inside one atomic DO block), but it is silently wrong for the third,
// "should be unreachable" state: a MIXED state where created_at disagrees with
// updated_at/expires_at (e.g. produced by manual intervention, a restored dump, or a future
// edit that splits the atomic block). Depending on which column is out of step, that state
// makes the guard either silently no-op (leaving the naked column(s) forever unconverted) or
// re-run `AT TIME ZONE 'UTC'` against an already-zoned column.
//
// This file makes the guard falsifiable: it forces exactly that mixed state directly (bypassing
// the up/down SQL, which cannot itself produce a mixed state per the atomicity argument above)
// and asserts the guard now refuses to proceed silently, plus that ordinary double-application
// (the two states the guard IS meant to handle) still no-ops cleanly in both directions.
//
// Run with:
//
//	DATABASE_URL=postgres://... go test ./internal/database/... \
//	  -run TestMigration000130Guard -tags integration -v -p 1
//
// This file shares connectMigrationTestDB/readMigrationFile with
// migration_000100_integration_test.go (same package).

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// getColumnDataType returns information_schema.columns.data_type for the given column of
// user_subscription_overrides, as observed inside tx.
func getColumnDataType(t *testing.T, tx pgx.Tx, column string) string {
	t.Helper()
	var dataType string
	err := tx.QueryRow(context.Background(), `
		SELECT data_type FROM information_schema.columns
		WHERE table_name = 'user_subscription_overrides' AND column_name = $1
	`, column).Scan(&dataType)
	require.NoError(t, err, "failed to read data_type for column %s", column)
	return dataType
}

// applyMigration000130Up runs the real 000130 up SQL in tx.
func applyMigration000130Up(t *testing.T, tx pgx.Tx) error {
	t.Helper()
	upSQL := readMigrationFile(t, "000130_user_subscription_overrides_timestamptz.up.sql")
	_, err := tx.Exec(context.Background(), upSQL)
	return err
}

// applyMigration000130Down runs the real 000130 down SQL in tx.
func applyMigration000130Down(t *testing.T, tx pgx.Tx) error {
	t.Helper()
	downSQL := readMigrationFile(t, "000130_user_subscription_overrides_timestamptz.down.sql")
	_, err := tx.Exec(context.Background(), downSQL)
	return err
}

// ─── Idempotency: applying 000130 twice (either direction) is a no-op the second time ────────

func TestMigration000130Guard_ReapplyUpIsNoOp(t *testing.T) {
	pool := connectMigrationTestDB(t)
	ctx := context.Background()

	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx) //nolint:errcheck

	// The connected database already has 000130 applied (all three columns TIMESTAMPTZ).
	// Re-running the up SQL a second time must succeed and change nothing.
	require.NoError(t, applyMigration000130Up(t, tx), "re-applying 000130 up must be a no-op, not an error")

	for _, col := range []string{"created_at", "updated_at", "expires_at"} {
		assert.Equal(t, "timestamp with time zone", getColumnDataType(t, tx, col),
			"column %s must still be TIMESTAMPTZ after re-applying 000130 up", col)
	}
}

func TestMigration000130Guard_ReapplyDownIsNoOp(t *testing.T) {
	pool := connectMigrationTestDB(t)
	ctx := context.Background()

	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx) //nolint:errcheck

	require.NoError(t, applyMigration000130Down(t, tx), "first 000130 down application failed")
	for _, col := range []string{"created_at", "updated_at", "expires_at"} {
		require.Equal(t, "timestamp without time zone", getColumnDataType(t, tx, col),
			"column %s must be naked TIMESTAMP after 000130 down", col)
	}

	// Re-running the down SQL a second time must succeed and change nothing.
	require.NoError(t, applyMigration000130Down(t, tx), "re-applying 000130 down must be a no-op, not an error")

	for _, col := range []string{"created_at", "updated_at", "expires_at"} {
		assert.Equal(t, "timestamp without time zone", getColumnDataType(t, tx, col),
			"column %s must still be naked TIMESTAMP after re-applying 000130 down", col)
	}
}

// ─── Mixed state: a column disagreeing with its siblings must raise, not silently no-op ──────
//
// The up/down SQL itself cannot produce these states (the three ALTER COLUMN TYPE statements
// run inside one atomic DO block per the migration's own comment) — they are forced directly
// here to prove the guard treats an otherwise-unreachable mixed state as a hard error instead
// of silently repairing or ignoring it.

func TestMigration000130Guard_MixedState_UpRaisesException(t *testing.T) {
	pool := connectMigrationTestDB(t)
	ctx := context.Background()

	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx) //nolint:errcheck

	// Starting schema already has 000130 applied (all three TIMESTAMPTZ). Force updated_at back
	// to naked TIMESTAMP directly, leaving created_at/expires_at as TIMESTAMPTZ — a mixed state
	// the guard (which historically only inspected created_at) would not detect via created_at
	// alone, since created_at itself is untouched here.
	_, err = tx.Exec(ctx, `
		ALTER TABLE user_subscription_overrides
			ALTER COLUMN updated_at TYPE TIMESTAMP USING (updated_at AT TIME ZONE 'UTC')
	`)
	require.NoError(t, err, "failed to force mixed state (updated_at reverted)")

	err = applyMigration000130Up(t, tx)
	require.Error(t, err, "000130 up must raise when created_at/expires_at and updated_at disagree, "+
		"not silently no-op and leave updated_at naked")
	assert.Contains(t, err.Error(), "updated_at", "exception must name the disagreeing column")
}

func TestMigration000130Guard_MixedState_DownRaisesException(t *testing.T) {
	pool := connectMigrationTestDB(t)
	ctx := context.Background()

	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx) //nolint:errcheck

	// Starting schema already has 000130 applied (all three TIMESTAMPTZ). Force created_at back
	// to naked TIMESTAMP directly, leaving updated_at/expires_at as TIMESTAMPTZ — the down
	// guard (which historically only inspected created_at) would read created_at as "already
	// rolled back" and skip the DO block entirely, silently stranding updated_at/expires_at as
	// TIMESTAMPTZ forever.
	_, err = tx.Exec(ctx, `
		ALTER TABLE user_subscription_overrides
			ALTER COLUMN created_at TYPE TIMESTAMP USING (created_at AT TIME ZONE 'UTC')
	`)
	require.NoError(t, err, "failed to force mixed state (created_at reverted)")

	err = applyMigration000130Down(t, tx)
	require.Error(t, err, "000130 down must raise when created_at disagrees with updated_at/expires_at, "+
		"not silently no-op and leave updated_at/expires_at as TIMESTAMPTZ")
	assert.Contains(t, err.Error(), "created_at", "exception must name the disagreeing column")
}
