//go:build integration

package database

// Integration tests for migrations 000131 (add linked_items.plaid_removed_at) and 000132 (backfill it)
// — Issue #1495.
//
// WHAT THESE PROVE THAT THE 5-SUITE MIGRATION SCRIPT CANNOT. scripts/test-migrations.sh runs against a
// near-empty database, so its data-integrity suite can only observe that migrations do not error; it
// has no rows whose backfilled VALUES it could check. The backfill's whole content is a policy
// decision — 'disconnected' rows are assumed removed, 'replaced' rows are NOT and must stay NULL,
// i.e. still owing Plaid work — and getting the 'replaced' half wrong would silently re-open the
// Critical that DeleteUserCascade's tightened C2 guard exists to close, while every existing gate
// stayed green. So the policy is asserted here on real rows.
//
// Every test runs inside its own transaction which is ALWAYS rolled back, so the committed schema and
// data are never modified. The connected database already has both migrations applied (that is the
// state `migrate up` leaves), so the up-direction tests assert re-application is a clean no-op, and the
// backfill tests re-run the real backfill SQL against rows they construct themselves.
//
// Run with:
//
//	DATABASE_URL=postgres://... go test ./internal/database/... \
//	  -run TestMigration000131 -tags integration -v -p 1
//	DATABASE_URL=postgres://... go test ./internal/database/... \
//	  -run TestMigration000132 -tags integration -v -p 1
//
// Shares connectMigrationTestDB/readMigrationFile with migration_000100_integration_test.go.

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// beginRolledBackTx opens a transaction that is rolled back when the test ends.
func beginRolledBackTx(t *testing.T) pgx.Tx {
	t.Helper()
	pool := connectMigrationTestDB(t)
	tx, err := pool.Begin(context.Background())
	require.NoError(t, err)
	t.Cleanup(func() { _ = tx.Rollback(context.Background()) })
	return tx
}

// seedPlaidRemovedAtFixtureItem inserts a user and one linked_items row with the given status and
// plaid_removed_at, returning the item id. Raw SQL rather than the repository, because the point is to
// construct states the repository deliberately cannot (a NULL stamp on a 'disconnected' row is exactly
// the pre-migration state 000132 exists to fix).
func seedPlaidRemovedAtFixtureItem(t *testing.T, tx pgx.Tx, status string, statusChangedAt *time.Time) string {
	t.Helper()
	ctx := context.Background()

	suffix := fmt.Sprintf("%d_%s", time.Now().UnixNano(), status)
	var userID string
	require.NoError(t, tx.QueryRow(ctx, `
		INSERT INTO users (clerk_user_id, email, subscription_tier, subscription_status, primary_region, is_active)
		VALUES ($1, $2, 'premium', 'active', 'iad', true) RETURNING id`,
		"clerk_pra_"+suffix, "pra-"+suffix+"@example.com").Scan(&userID))

	// A 'replaced' row must carry replaced_at — enforced by the linked_items_replaced_at_consistency
	// CHECK constraint, which is itself evidence that 'replaced' is a distinct, structured fact that
	// status alone owns (the reason plaid_removed_at had to be a separate column).
	var replacedAt *time.Time
	if status == "replaced" {
		now := time.Now()
		replacedAt = &now
	}

	var itemID string
	require.NoError(t, tx.QueryRow(ctx, `
		INSERT INTO linked_items (user_id, item_id, access_token_encrypted, status, status_changed_at, replaced_at, plaid_removed_at)
		VALUES ($1, $2, 'enc-token', $3, $4, $5, NULL) RETURNING id`,
		userID, "plaid-item-"+suffix, status, statusChangedAt, replacedAt).Scan(&itemID))
	return itemID
}

func applyMigration000131Up(t *testing.T, tx pgx.Tx) error {
	t.Helper()
	_, err := tx.Exec(context.Background(), readMigrationFile(t, "000131_add_linked_items_plaid_removed_at.up.sql"))
	return err
}

func applyMigration000131Down(t *testing.T, tx pgx.Tx) error {
	t.Helper()
	_, err := tx.Exec(context.Background(), readMigrationFile(t, "000131_add_linked_items_plaid_removed_at.down.sql"))
	return err
}

func applyMigration000132Up(t *testing.T, tx pgx.Tx) error {
	t.Helper()
	_, err := tx.Exec(context.Background(), readMigrationFile(t, "000132_backfill_linked_items_plaid_removed_at.up.sql"))
	return err
}

// columnIsNullableTimestamptz reports the column's data_type and is_nullable as the catalog sees them.
func columnIsNullableTimestamptz(t *testing.T, tx pgx.Tx, column string) (dataType string, isNullable string, exists bool) {
	t.Helper()
	rows, err := tx.Query(context.Background(), `
		SELECT data_type, is_nullable FROM information_schema.columns
		WHERE table_name = 'linked_items' AND column_name = $1`, column)
	require.NoError(t, err)
	defer rows.Close()
	if !rows.Next() {
		return "", "", false
	}
	require.NoError(t, rows.Scan(&dataType, &isNullable))
	return dataType, isNullable, true
}

// ─── 000131: shape, idempotency, rollback ───────────────────────────────────────────────────────

// TestMigration000131_ColumnIsNullableTimestamptzWithNoDefault pins the three properties the
// migration's lock-cost argument rests on. The NO DEFAULT part is the load-bearing one: a nullable
// ADD COLUMN with no default is a catalog-only change, whereas a DEFAULT would be the difference
// between an instant ALTER and (on a volatile default) a full table rewrite under ACCESS EXCLUSIVE.
// Squawk cannot tell us this — the repo's config runs with assume_in_transaction and an excluded_rules
// list, and it lints statements in isolation — so it is asserted against the live catalog instead.
//
// NULLABLE is equally load-bearing in the opposite direction: NULL is what "Plaid removal still owed"
// means, and the C2 guard blocks on exactly that.
func TestMigration000131_ColumnIsNullableTimestamptzWithNoDefault(t *testing.T) {
	tx := beginRolledBackTx(t)

	dataType, isNullable, exists := columnIsNullableTimestamptz(t, tx, "plaid_removed_at")
	require.True(t, exists, "linked_items.plaid_removed_at must exist once 000131 is applied")
	assert.Equal(t, "timestamp with time zone", dataType,
		"the column must be TIMESTAMPTZ (an absolute instant), not a zone-naked TIMESTAMP")
	assert.Equal(t, "YES", isNullable,
		"the column must stay NULLABLE: NULL is the value that means 'Plaid removal still owed', which is "+
			"what DeleteUserCascade's C2 guard blocks on")

	var hasDefault bool
	require.NoError(t, tx.QueryRow(context.Background(), `
		SELECT column_default IS NOT NULL FROM information_schema.columns
		WHERE table_name = 'linked_items' AND column_name = 'plaid_removed_at'`).Scan(&hasDefault))
	assert.False(t, hasDefault,
		"the column must have NO DEFAULT — that is what makes the ADD COLUMN a metadata-only change, and "+
			"a default would also silently declare every existing row's Plaid side done")
}

// TestMigration000131_ReapplyUpIsNoOp: ADD COLUMN IF NOT EXISTS, so re-running against an
// already-migrated database must succeed and change nothing. Container restarts re-run migrations, so
// this is a real production path, not a theoretical one.
func TestMigration000131_ReapplyUpIsNoOp(t *testing.T) {
	tx := beginRolledBackTx(t)

	require.NoError(t, applyMigration000131Up(t, tx), "re-applying 000131 must be a clean no-op")

	_, isNullable, exists := columnIsNullableTimestamptz(t, tx, "plaid_removed_at")
	require.True(t, exists)
	assert.Equal(t, "YES", isNullable)
}

// TestMigration000131_DownThenUpRestoresTheColumn walks the rollback cycle inside one rolled-back
// transaction: the column disappears, then comes back with the same shape. The DOWN is also asserted to
// be idempotent (DROP COLUMN IF EXISTS), which matters because a failed deploy can re-run it.
func TestMigration000131_DownThenUpRestoresTheColumn(t *testing.T) {
	tx := beginRolledBackTx(t)

	require.NoError(t, applyMigration000131Down(t, tx))
	_, _, exists := columnIsNullableTimestamptz(t, tx, "plaid_removed_at")
	require.False(t, exists, "the down migration must actually drop the column")

	require.NoError(t, applyMigration000131Down(t, tx), "the down migration must be idempotent")

	require.NoError(t, applyMigration000131Up(t, tx))
	dataType, isNullable, exists := columnIsNullableTimestamptz(t, tx, "plaid_removed_at")
	require.True(t, exists, "up must restore the column after a rollback")
	assert.Equal(t, "timestamp with time zone", dataType)
	assert.Equal(t, "YES", isNullable)
}

// ─── 000132: the backfill POLICY, which is the part that can silently re-open the Critical ──────

// TestMigration000132_BackfillsDisconnectedButNotReplaced is the test the whole backfill hinges on, and
// the 'replaced' half is the one that matters most:
//
//   - a 'disconnected' row IS stamped. Its only production writer, MarkItemsDisconnected, runs only
//     after a successful Plaid /item/remove (or ITEM_NOT_FOUND), so removal may be ASSUMED. Without
//     this, tightening C2 would block the erasure of every user whose cleanup genuinely already ran.
//   - a 'replaced' row is NOT stamped. A re-link sets 'replaced' WITHOUT calling Plaid, so those Items
//     are still live and still billing. Stamping them would hand the tightened guard a false proof and
//     re-create the exact Critical this change closes — silently, since nothing else would fail.
//   - every other status is left NULL too: no evidence of removal exists for them either.
//
// Falsifiability: widening the backfill's WHERE to `status IN ('disconnected','replaced')`, or dropping
// the WHERE altogether, makes this test fail while scripts/test-migrations.sh stays green (it has no
// rows to check).
func TestMigration000132_BackfillsDisconnectedButNotReplaced(t *testing.T) {
	tx := beginRolledBackTx(t)
	ctx := context.Background()

	changed := time.Now().Add(-72 * time.Hour).UTC().Truncate(time.Microsecond)
	disconnectedID := seedPlaidRemovedAtFixtureItem(t, tx, "disconnected", &changed)
	replacedID := seedPlaidRemovedAtFixtureItem(t, tx, "replaced", &changed)
	activeID := seedPlaidRemovedAtFixtureItem(t, tx, "active", nil)
	errorID := seedPlaidRemovedAtFixtureItem(t, tx, "error", &changed)

	require.NoError(t, applyMigration000132Up(t, tx))

	var disconnectedStamp *time.Time
	require.NoError(t, tx.QueryRow(ctx,
		`SELECT plaid_removed_at FROM linked_items WHERE id = $1`, disconnectedID).Scan(&disconnectedStamp))
	require.NotNil(t, disconnectedStamp,
		"a 'disconnected' row must be backfilled: its status is written only after a successful Plaid "+
			"removal, and leaving it NULL would block that user's erasure over work already done")
	assert.True(t, changed.Equal(disconnectedStamp.UTC()),
		"the backfilled value must be the row's own status_changed_at (%v), not a fresh clock read (%v) — "+
			"the migration is explicit that this is an ASSUMED instant, and status_changed_at is the closest "+
			"recorded proxy available", changed, disconnectedStamp.UTC())

	for name, id := range map[string]string{"replaced": replacedID, "active": activeID, "error": errorID} {
		var stamp *time.Time
		require.NoError(t, tx.QueryRow(ctx,
			`SELECT plaid_removed_at FROM linked_items WHERE id = $1`, id).Scan(&stamp))
		assert.Nil(t, stamp,
			"a %q row must stay NULL — no Plaid removal was ever recorded for it. For 'replaced' "+
				"specifically this is the Critical: a re-link never calls /item/remove, so stamping it would "+
				"tell DeleteUserCascade a live, billing Item had been removed", name)
	}
}

// TestMigration000132_ReapplyDoesNotOverwriteAnExistingStamp pins the `AND plaid_removed_at IS NULL`
// guard, which does double duty: it makes the migration idempotent (container restarts re-run
// migrations) AND it means the backfill can never clobber a genuinely MEASURED value that the
// application wrote after the migration first ran, replacing it with a synthesised status_changed_at
// copy.
func TestMigration000132_ReapplyDoesNotOverwriteAnExistingStamp(t *testing.T) {
	tx := beginRolledBackTx(t)
	ctx := context.Background()

	changed := time.Now().Add(-72 * time.Hour).UTC().Truncate(time.Microsecond)
	itemID := seedPlaidRemovedAtFixtureItem(t, tx, "disconnected", &changed)

	measured := time.Now().Add(-time.Hour).UTC().Truncate(time.Microsecond)
	_, err := tx.Exec(ctx, `UPDATE linked_items SET plaid_removed_at = $2 WHERE id = $1`, itemID, measured)
	require.NoError(t, err)

	require.NoError(t, applyMigration000132Up(t, tx), "re-applying the backfill must be a clean no-op")

	var stamp time.Time
	require.NoError(t, tx.QueryRow(ctx,
		`SELECT plaid_removed_at FROM linked_items WHERE id = $1`, itemID).Scan(&stamp))
	assert.True(t, measured.Equal(stamp.UTC()),
		"an existing stamp must survive a re-run: %v was replaced by %v. The migration's value is an "+
			"assumption; the application's is a measurement, and an assumption must never overwrite it",
		measured, stamp.UTC())
}

// TestMigration000132_FallsBackWhenStatusChangedAtIsNull pins the COALESCE chain. status_changed_at is
// NULLABLE (only rows that have transitioned status since migration 000097 carry it), so without the
// fallback a legacy 'disconnected' row would be "backfilled" to NULL — i.e. not backfilled at all —
// and would then block its user's erasure forever with nothing in the logs to say why. created_at is
// NOT NULL, which is what makes the chain total; asserted here rather than assumed.
func TestMigration000132_FallsBackWhenStatusChangedAtIsNull(t *testing.T) {
	tx := beginRolledBackTx(t)
	ctx := context.Background()

	itemID := seedPlaidRemovedAtFixtureItem(t, tx, "disconnected", nil)

	require.NoError(t, applyMigration000132Up(t, tx))

	var stamp *time.Time
	require.NoError(t, tx.QueryRow(ctx,
		`SELECT plaid_removed_at FROM linked_items WHERE id = $1`, itemID).Scan(&stamp))
	require.NotNil(t, stamp,
		"a 'disconnected' row with a NULL status_changed_at must still be stamped, via the "+
			"updated_at/created_at fallback — otherwise the backfill silently skips it and the C2 guard "+
			"blocks that user's erasure with no record of why")

	var createdAtNotNull, updatedAtNotNull bool
	require.NoError(t, tx.QueryRow(ctx, `
		SELECT
			(SELECT attnotnull FROM pg_attribute WHERE attrelid = 'linked_items'::regclass AND attname = 'created_at'),
			(SELECT attnotnull FROM pg_attribute WHERE attrelid = 'linked_items'::regclass AND attname = 'updated_at')
	`).Scan(&createdAtNotNull, &updatedAtNotNull))
	assert.True(t, createdAtNotNull,
		"created_at must be NOT NULL for the COALESCE chain to be total — if this ever changes, the backfill "+
			"can write NULL and silently skip rows")
	assert.True(t, updatedAtNotNull, "updated_at is the middle link of the same chain")
}
