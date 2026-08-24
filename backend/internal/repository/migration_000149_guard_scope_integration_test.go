//go:build integration

package repository

import (
	"context"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// migration000149Path points at 000149's up.sql relative to this package directory
// (backend/internal/repository -> backend/migrations).
const migration000149Path = "../../migrations/000149_add_accounts_removed_status.up.sql"

// TestMigration000149_StatusCheckGuardsAreTableScoped is #1569 residual E11.
//
// 000149's idempotency guards ask `SELECT 1 FROM pg_constraint WHERE conname =
// 'accounts_valid_status'` with no conrelid predicate. pg_constraint is database-wide and
// constraint names are unique only per table, so any OTHER table carrying that name answers the
// guard. The failure mode is not a loud error, it is a silent hole: the drop is table-scoped and
// succeeds, then the re-add's NOT EXISTS check sees the decoy, decides the constraint is already
// there, and skips — leaving `accounts` with no status CHECK at all and the migration reporting
// success. Every later 'removed'/'expired' value would then be unpoliced by the schema.
//
// The test runs the migration's OWN bytes rather than a restatement, so it exercises the SQL that
// will really run (same reasoning as readAccountStatusHistoryBackfillStatements). It is executed
// as one multi-statement batch: the file is entirely DO $$ ... $$ blocks plus a COMMENT, whose
// bodies contain semicolons, so the split-on-';' approach used for 000152 would corrupt it.
func TestMigration000149_StatusCheckGuardsAreTableScoped(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()

	raw, err := os.ReadFile(migration000149Path)
	require.NoError(t, err, "read %s to run its real SQL", migration000149Path)
	migrationSQL := string(raw)

	_, err = pool.Exec(ctx, `CREATE TABLE IF NOT EXISTS migration_000149_decoy (status TEXT)`)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `
		ALTER TABLE migration_000149_decoy
		DROP CONSTRAINT IF EXISTS accounts_valid_status`)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `
		ALTER TABLE migration_000149_decoy
		ADD CONSTRAINT accounts_valid_status CHECK (status IN ('anything'))`)
	require.NoError(t, err)

	// Drop the decoy and re-run the migration unobstructed, so that even a failing run leaves the
	// shared database with accounts' real CHECK back in place for every other test.
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DROP TABLE IF EXISTS migration_000149_decoy`)
		_, _ = pool.Exec(context.Background(), migrationSQL)
	})

	_, err = pool.Exec(ctx, migrationSQL)
	require.NoError(t, err, "000149 must apply cleanly even with a same-named constraint elsewhere")

	var def string
	err = pool.QueryRow(ctx, `
		SELECT pg_get_constraintdef(oid)
		FROM pg_constraint
		WHERE conrelid = 'accounts'::regclass AND conname = 'accounts_valid_status'`).Scan(&def)
	require.NoError(t, err,
		"accounts lost its status CHECK: 000149's guard matched a same-named constraint on "+
			"another table, dropped the real one and then skipped re-adding it")
	assert.Contains(t, def, "'removed'",
		"the re-added CHECK must be the WIDENED one this migration exists to install")

	// The removed_at column guard reads information_schema.columns with no table_schema
	// predicate; assert the column is really there for the same class of reason.
	var hasRemovedAt bool
	require.NoError(t, pool.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM information_schema.columns
			WHERE table_schema = current_schema() AND table_name = 'accounts' AND column_name = 'removed_at')`).Scan(&hasRemovedAt))
	assert.True(t, hasRemovedAt)
}
