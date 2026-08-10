//go:build integration

package database

// Integration tests for migrations 000133 (add linked_items.plaid_removal_failed_at +
// plaid_removal_error_code) and 000134 (create plaid_orphaned_items) — Issue #1494.
//
// WHAT THESE PROVE THAT THE 5-SUITE MIGRATION SCRIPT CANNOT. scripts/test-migrations.sh proves the
// files apply, roll back, and leave the schema hash unchanged. It does not check the SHAPE either
// migration commits to, and both migrations' prose rests on specific shapes:
//
//   - 000133 argues its lock cost from "nullable ADD COLUMN with NO DEFAULT" — a claim about the
//     catalog, which Squawk cannot confirm for us (excluded_rules, assume_in_transaction, statements
//     linted in isolation) and which a later "helpful" DEFAULT would quietly break. A default here
//     would also declare something false about every existing row.
//   - 000134 claims plaid_orphaned_items has NO foreign key at all, so the cascade cannot reach it,
//     and that its UNIQUE constraint is what makes the cascade's ON CONFLICT DO NOTHING idempotent.
//     An FK added later would give DeleteUserCascade a CASCADE path straight through the one table
//     whose entire purpose is to outlive that cascade — and every other test would stay green,
//     because the record is written before the delete either way.
//
// Every test runs inside its own transaction which is ALWAYS rolled back, so the committed schema is
// never modified. The connected database already has both migrations applied, so the up-direction
// tests assert re-application is a clean no-op.
//
// Run with:
//
//	DATABASE_URL=postgres://... go test ./internal/database/... \
//	  -run 'TestMigration000133|TestMigration000134' -tags integration -v -p 1
//
// Shares beginRolledBackTx/readMigrationFile with the 000131/000132 tests.

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func applyMigration000133Up(t *testing.T, tx pgx.Tx) error {
	t.Helper()
	_, err := tx.Exec(context.Background(), readMigrationFile(t, "000133_add_linked_items_plaid_removal_failed.up.sql"))
	return err
}

func applyMigration000133Down(t *testing.T, tx pgx.Tx) error {
	t.Helper()
	_, err := tx.Exec(context.Background(), readMigrationFile(t, "000133_add_linked_items_plaid_removal_failed.down.sql"))
	return err
}

func applyMigration000134Up(t *testing.T, tx pgx.Tx) error {
	t.Helper()
	_, err := tx.Exec(context.Background(), readMigrationFile(t, "000134_create_plaid_orphaned_items.up.sql"))
	return err
}

func applyMigration000134Down(t *testing.T, tx pgx.Tx) error {
	t.Helper()
	_, err := tx.Exec(context.Background(), readMigrationFile(t, "000134_create_plaid_orphaned_items.down.sql"))
	return err
}

// linkedItemsColumnShape reports data_type, is_nullable and whether a default exists, as the catalog
// sees them.
func linkedItemsColumnShape(t *testing.T, tx pgx.Tx, column string) (dataType, isNullable string, hasDefault, exists bool) {
	t.Helper()
	rows, err := tx.Query(context.Background(), `
		SELECT data_type, is_nullable, column_default IS NOT NULL
		FROM information_schema.columns
		WHERE table_name = 'linked_items' AND column_name = $1`, column)
	require.NoError(t, err)
	defer rows.Close()
	if !rows.Next() {
		return "", "", false, false
	}
	require.NoError(t, rows.Scan(&dataType, &isNullable, &hasDefault))
	return dataType, isNullable, hasDefault, true
}

// ─── 000133: shape, idempotency, rollback ───────────────────────────────────────────────────────

// TestMigration000133_ColumnsAreNullableWithNoDefault pins the properties 000133's lock-cost argument
// and its semantics both rest on.
//
// NO DEFAULT is load-bearing twice over: it is what makes the ADD COLUMN a catalog-only change, and a
// default on plaid_removal_failed_at would declare every existing row's Plaid removal hopeless — the
// most destructive single value this schema could acquire, since that is the fact C2 stops blocking on.
// NULLABLE is equally load-bearing: NULL means "we have NOT concluded this is unremovable", i.e. keep
// trying, which is the safe default for every row.
func TestMigration000133_ColumnsAreNullableWithNoDefault(t *testing.T) {
	tx := beginRolledBackTx(t)

	dataType, isNullable, hasDefault, exists := linkedItemsColumnShape(t, tx, "plaid_removal_failed_at")
	require.True(t, exists, "linked_items.plaid_removal_failed_at must exist once 000133 is applied")
	assert.Equal(t, "timestamp with time zone", dataType,
		"an absolute instant, not a zone-naked TIMESTAMP — this value is compared against nothing but NULL today, "+
			"but it is shown to operators and copied into plaid_orphaned_items")
	assert.Equal(t, "YES", isNullable,
		"NULL must remain expressible: it is what 'no unrecoverable verdict has been reached' means")
	assert.False(t, hasDefault,
		"NO DEFAULT — a default would both force a table rewrite and declare every existing row unremovable")

	dataType, isNullable, hasDefault, exists = linkedItemsColumnShape(t, tx, "plaid_removal_error_code")
	require.True(t, exists, "linked_items.plaid_removal_error_code must exist once 000133 is applied")
	assert.Equal(t, "character varying", dataType,
		"VARCHAR(100), matching linked_items.error_code (migration 000013), which holds Plaid error codes for the sync path")
	assert.Equal(t, "YES", isNullable)
	assert.False(t, hasDefault)

	var maxLen int
	require.NoError(t, tx.QueryRow(context.Background(), `
		SELECT character_maximum_length FROM information_schema.columns
		WHERE table_name = 'linked_items' AND column_name = 'plaid_removal_error_code'`).Scan(&maxLen))
	assert.Equal(t, 100, maxLen)
}

// TestMigration000133_ReapplyUpIsNoOp: ADD COLUMN IF NOT EXISTS, so re-running against an
// already-migrated database must succeed and change nothing. Container restarts re-run migrations, so
// this is a real production path.
func TestMigration000133_ReapplyUpIsNoOp(t *testing.T) {
	tx := beginRolledBackTx(t)

	require.NoError(t, applyMigration000133Up(t, tx), "re-applying 000133 must be a clean no-op")

	_, isNullable, _, exists := linkedItemsColumnShape(t, tx, "plaid_removal_failed_at")
	require.True(t, exists)
	assert.Equal(t, "YES", isNullable)
}

// TestMigration000133_DownThenUpRestoresBothColumns walks the rollback cycle. The DOWN is also asserted
// idempotent (DROP COLUMN IF EXISTS), which matters because a failed deploy can re-run it.
func TestMigration000133_DownThenUpRestoresBothColumns(t *testing.T) {
	tx := beginRolledBackTx(t)

	require.NoError(t, applyMigration000133Down(t, tx))
	_, _, _, failedExists := linkedItemsColumnShape(t, tx, "plaid_removal_failed_at")
	_, _, _, codeExists := linkedItemsColumnShape(t, tx, "plaid_removal_error_code")
	require.False(t, failedExists, "the down migration must drop plaid_removal_failed_at")
	require.False(t, codeExists, "the down migration must drop plaid_removal_error_code too — a lone orphan column would be worse than either endpoint")

	require.NoError(t, applyMigration000133Down(t, tx), "the down migration must be idempotent")

	require.NoError(t, applyMigration000133Up(t, tx))
	dataType, isNullable, _, exists := linkedItemsColumnShape(t, tx, "plaid_removal_failed_at")
	require.True(t, exists, "up must restore the columns after a rollback")
	assert.Equal(t, "timestamp with time zone", dataType)
	assert.Equal(t, "YES", isNullable)
	_, _, _, codeExists = linkedItemsColumnShape(t, tx, "plaid_removal_error_code")
	assert.True(t, codeExists)
}

// ─── 000134: the structural promises the record's survival depends on ───────────────────────────

// TestMigration000134_HasNoForeignKeyAndNoUserIdentifier is the test that matters most in this file.
//
// plaid_orphaned_items exists to outlive a user's erasure. Two schema properties make that true, and
// both are the kind of thing a later migration adds "for referential integrity" without realising what
// it breaks: a foreign key to users would give DeleteUserCascade a CASCADE path through this table, and
// a user_id column would make the record a surviving pointer to an erased person (and expose it to the
// audit_logs-style redaction the cascade already runs by user id). Neither would be caught by any other
// test, because the record is written before the delete either way.
func TestMigration000134_HasNoForeignKeyAndNoUserIdentifier(t *testing.T) {
	tx := beginRolledBackTx(t)
	ctx := context.Background()

	var fkCount int
	require.NoError(t, tx.QueryRow(ctx, `
		SELECT COUNT(*) FROM information_schema.table_constraints
		WHERE table_name = 'plaid_orphaned_items' AND constraint_type = 'FOREIGN KEY'`).Scan(&fkCount))
	assert.Equal(t, 0, fkCount,
		"no foreign key at all: the cascade must not be able to reach the one table whose job is to survive it")

	var userishColumns int
	require.NoError(t, tx.QueryRow(ctx, `
		SELECT COUNT(*) FROM information_schema.columns
		WHERE table_name = 'plaid_orphaned_items'
		  AND column_name IN ('user_id', 'clerk_user_id', 'email', 'actor_user_id')`).Scan(&userishColumns))
	assert.Equal(t, 0, userishColumns,
		"the record must identify a vendor resource, never the erased person")
}

// TestMigration000134_UniqueLinkedItemMakesTheCascadeInsertIdempotent pins the constraint the cascade's
// ON CONFLICT (linked_item_id) DO NOTHING targets. Without it that clause is not merely useless — it is
// a runtime error ("there is no unique or exclusion constraint matching the ON CONFLICT
// specification"), which would make this the statement that fails the very cascade it was added to
// protect.
func TestMigration000134_UniqueLinkedItemMakesTheCascadeInsertIdempotent(t *testing.T) {
	tx := beginRolledBackTx(t)
	ctx := context.Background()

	_, err := tx.Exec(ctx, `
		INSERT INTO plaid_orphaned_items (linked_item_id, plaid_item_id_encrypted)
		VALUES ('11111111-1111-1111-1111-111111111111', 'enc-item-1')`)
	require.NoError(t, err)

	// The same linked_items row must not be able to produce two records. Wrapped in a SAVEPOINT: a
	// constraint violation aborts the enclosing transaction (SQLSTATE 25P02 on everything after it),
	// so without this the ON CONFLICT assertion below would fail for the wrong reason.
	_, err = tx.Exec(ctx, "SAVEPOINT before_duplicate")
	require.NoError(t, err)
	_, err = tx.Exec(ctx, `
		INSERT INTO plaid_orphaned_items (linked_item_id, plaid_item_id_encrypted)
		VALUES ('11111111-1111-1111-1111-111111111111', 'enc-item-1')`)
	require.Error(t, err, "linked_item_id must be UNIQUE")
	_, err = tx.Exec(ctx, "ROLLBACK TO SAVEPOINT before_duplicate")
	require.NoError(t, err)

	// And the cascade's own spelling turns that into a no-op rather than a failure. Asserted with the
	// same ON CONFLICT clause DeleteUserCascade uses, because the clause is what needs the constraint.
	_, err = tx.Exec(ctx, `
		INSERT INTO plaid_orphaned_items (linked_item_id, plaid_item_id_encrypted)
		VALUES ('11111111-1111-1111-1111-111111111111', 'enc-item-1')
		ON CONFLICT (linked_item_id) DO NOTHING`)
	require.NoError(t, err, "ON CONFLICT (linked_item_id) must resolve — otherwise a retried cascade fails on this statement")

	var count int
	require.NoError(t, tx.QueryRow(ctx, `
		SELECT COUNT(*) FROM plaid_orphaned_items
		WHERE linked_item_id = '11111111-1111-1111-1111-111111111111'`).Scan(&count))
	assert.Equal(t, 1, count)
}

// TestMigration000134_DefaultsMakeAMinimalInsertComplete pins the two defaults the cascade relies on by
// NOT supplying them: id (gen_random_uuid()) and orphaned_at (NOW()). The INSERT ... SELECT names
// neither column, so if either default disappeared the cascade would fail with a NOT NULL violation at
// the worst possible moment — mid-erasure.
func TestMigration000134_DefaultsMakeAMinimalInsertComplete(t *testing.T) {
	tx := beginRolledBackTx(t)
	ctx := context.Background()

	var id string
	var orphanedAtIsSet bool
	require.NoError(t, tx.QueryRow(ctx, `
		INSERT INTO plaid_orphaned_items (linked_item_id, plaid_item_id_encrypted)
		VALUES ('22222222-2222-2222-2222-222222222222', 'enc-item-2')
		RETURNING id, orphaned_at IS NOT NULL`).Scan(&id, &orphanedAtIsSet))
	assert.NotEmpty(t, id, "id must default to gen_random_uuid() — the cascade never supplies it")
	assert.True(t, orphanedAtIsSet, "orphaned_at must default to NOW() — the cascade never supplies it")

	// plaid_item_id_encrypted is the one column with no defensible NULL: a record with no Plaid Item id
	// names nothing an operator could act on.
	_, err := tx.Exec(ctx, `
		INSERT INTO plaid_orphaned_items (linked_item_id, plaid_item_id_encrypted)
		VALUES ('33333333-3333-3333-3333-333333333333', NULL)`)
	assert.Error(t, err, "plaid_item_id_encrypted must be NOT NULL")
}

// TestMigration000134_ReapplyUpIsNoOp: CREATE TABLE IF NOT EXISTS plus unconditional COMMENTs, all
// idempotent. Re-run on every container restart.
func TestMigration000134_ReapplyUpIsNoOp(t *testing.T) {
	tx := beginRolledBackTx(t)

	require.NoError(t, applyMigration000134Up(t, tx), "re-applying 000134 must be a clean no-op")

	var exists bool
	require.NoError(t, tx.QueryRow(context.Background(),
		`SELECT EXISTS (SELECT 1 FROM information_schema.tables WHERE table_name = 'plaid_orphaned_items')`).Scan(&exists))
	assert.True(t, exists)
}

// TestMigration000134_DownThenUpRestoresTheTable walks the rollback cycle, and asserts the DOWN is
// idempotent (DROP TABLE IF EXISTS) because a failed deploy can re-run it.
func TestMigration000134_DownThenUpRestoresTheTable(t *testing.T) {
	tx := beginRolledBackTx(t)
	ctx := context.Background()

	tableExists := func() bool {
		var exists bool
		require.NoError(t, tx.QueryRow(ctx,
			`SELECT EXISTS (SELECT 1 FROM information_schema.tables WHERE table_name = 'plaid_orphaned_items')`).Scan(&exists))
		return exists
	}

	require.NoError(t, applyMigration000134Down(t, tx))
	require.False(t, tableExists(), "the down migration must actually drop the table")
	require.NoError(t, applyMigration000134Down(t, tx), "the down migration must be idempotent")

	require.NoError(t, applyMigration000134Up(t, tx))
	require.True(t, tableExists(), "up must restore the table after a rollback")

	// Restored with its promises intact, not just its name.
	var fkCount, uniqueCount int
	require.NoError(t, tx.QueryRow(ctx, `
		SELECT COUNT(*) FROM information_schema.table_constraints
		WHERE table_name = 'plaid_orphaned_items' AND constraint_type = 'FOREIGN KEY'`).Scan(&fkCount))
	require.NoError(t, tx.QueryRow(ctx, `
		SELECT COUNT(*) FROM information_schema.table_constraints
		WHERE table_name = 'plaid_orphaned_items' AND constraint_type = 'UNIQUE'`).Scan(&uniqueCount))
	assert.Equal(t, 0, fkCount)
	assert.Equal(t, 1, uniqueCount)
}
