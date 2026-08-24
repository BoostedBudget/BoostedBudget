//go:build integration

package repository

// Issue #1569 Task 20 (N10/N11). Two `.down.sql` files are history-destruction paths that this
// branch's forward migrations exist to close, and neither said so at the point where it matters.
//
//	000151.down.sql  DROP TABLE account_status_history — discards the whole status trail, including
//	                 the 000152 backfill and everything the live trigger has recorded since.
//	000150.down.sql  RE-ARMS both ON DELETE CASCADE foreign keys that 000150 disarmed. Deleting one
//	                 account would once again destroy its balance_snapshots and its savings_goals —
//	                 the latter being money the user logged BY HAND, unrecoverable from any provider.
//
// A comment is not a control: a rollback is run by an operator under pressure, from a CLI, and
// golang-migrate prints nothing from a SQL comment. So both now REFUSE when the data they would
// destroy actually exists, and say what to decide before retrying.
//
// ⚠ WHY THE REFUSAL IS CONDITIONED ON ROWS AND NOT UNCONDITIONAL. scripts/test-migrations.sh runs a
// full `down -all` → `up` cycle as its rollback suite, and an unconditional refusal would make that
// suite permanently red — which would get the refusal removed, not respected. It runs against a
// clean_database, so a data-conditioned refusal is silent there and loud exactly when it matters.
// That is also the honest boundary: rolling back a schema change on an empty database destroys
// nothing.
//
// These tests execute the REAL .down.sql text inside a transaction that is always rolled back, so
// they cannot drift from the files and cannot leave the shared database altered.

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func readDownMigration(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "..", "migrations", name))
	require.NoError(t, err, "read %s", name)
	require.NotEmpty(t, b)
	return string(b)
}

// 000150.down.sql must refuse while any of the rows its re-armed cascades would make destructible
// still exist.
func TestDownMigration000150_RefusesWhileCascadableHistoryExists_Integration(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()
	down := readDownMigration(t, "000150_preserve_history_on_account_delete.down.sql")

	t.Run("refuses when balance_snapshots exist", func(t *testing.T) {
		tx, err := pool.Begin(ctx)
		require.NoError(t, err)
		defer tx.Rollback(ctx) //nolint:errcheck // always rolled back

		// A real snapshot row, and its existence asserted, so the refusal below cannot be a
		// refusal over an empty table — the failure mode that made an earlier probe in this task
		// pass while proving nothing.
		userID, itemID, acctID := seedDownProbeFixture(t, ctx, tx, "n10-snap")
		_, err = tx.Exec(ctx, `
			INSERT INTO balance_snapshots (account_id, user_id, current_balance, currency_code, snapshot_at, source)
			VALUES ($1, $2, 42.00, 'USD', NOW(), 'manual')`, acctID, userID)
		require.NoError(t, err)
		var n int
		require.NoError(t, tx.QueryRow(ctx, `SELECT COUNT(*) FROM balance_snapshots WHERE account_id = $1`, acctID).Scan(&n))
		require.Equal(t, 1, n, "the fixture must exist or this test proves nothing")
		_ = itemID

		_, err = tx.Exec(ctx, down)
		require.Error(t, err, "000150.down.sql must refuse while balance_snapshots exist — re-arming "+
			"ON DELETE CASCADE would make every one of them destructible by the next account delete")
		assert.Contains(t, err.Error(), "refusing to roll back 000150")
	})

	t.Run("proceeds on a database with nothing to destroy", func(t *testing.T) {
		// The discriminating case. Without it, a down migration that refused unconditionally would
		// satisfy the test above and would break scripts/test-migrations.sh's rollback suite.
		tx, err := pool.Begin(ctx)
		require.NoError(t, err)
		defer tx.Rollback(ctx) //nolint:errcheck // always rolled back

		_, err = tx.Exec(ctx, `DELETE FROM balance_snapshots`)
		require.NoError(t, err)
		_, err = tx.Exec(ctx, `DELETE FROM savings_goals`)
		require.NoError(t, err)

		_, err = tx.Exec(ctx, down)
		require.NoError(t, err, "with no rows to destroy, the rollback must run — otherwise the "+
			"migration suite's down -all cycle can never pass and the guard gets deleted")
	})
}

// 000151.down.sql must refuse while the status trail it would DROP TABLE still holds rows.
func TestDownMigration000151_RefusesWhileStatusHistoryExists_Integration(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()
	down := readDownMigration(t, "000151_create_account_status_history.down.sql")

	t.Run("refuses when account_status_history has rows", func(t *testing.T) {
		tx, err := pool.Begin(ctx)
		require.NoError(t, err)
		defer tx.Rollback(ctx) //nolint:errcheck // always rolled back

		userID, _, acctID := seedDownProbeFixture(t, ctx, tx, "n11-hist")
		// No hand-written history row: 000152's trigger already opened an 'active' interval for the
		// account seeded above. Using the trigger's own output rather than an invented shape means
		// this fixture cannot disagree with production about what a history row looks like — and an
		// earlier draft that invented one was rejected by chk_ash_interval, which is the constraint
		// saying so.
		_ = userID
		var n int
		require.NoError(t, tx.QueryRow(ctx,
			`SELECT COUNT(*) FROM account_status_history WHERE account_id = $1`, acctID).Scan(&n))
		require.GreaterOrEqual(t, n, 1, "the fixture must exist or this test proves nothing")

		_, err = tx.Exec(ctx, down)
		require.Error(t, err, "000151.down.sql must refuse while the status trail holds rows")
		assert.Contains(t, err.Error(), "refusing to roll back 000151")
	})

	t.Run("proceeds when the trail is empty", func(t *testing.T) {
		tx, err := pool.Begin(ctx)
		require.NoError(t, err)
		defer tx.Rollback(ctx) //nolint:errcheck // always rolled back

		// 000152's trigger writes to this table on every account status change, so it must go
		// first — exactly as golang-migrate orders the real rollback.
		_, err = tx.Exec(ctx, `DROP TRIGGER IF EXISTS trg_account_status_history ON accounts`)
		require.NoError(t, err)
		_, err = tx.Exec(ctx, `DELETE FROM account_status_history`)
		require.NoError(t, err)

		_, err = tx.Exec(ctx, down)
		require.NoError(t, err, "an empty trail destroys nothing; the rollback must still run")
	})
}

// seedDownProbeFixture inserts a user, linked item and account inside tx and returns their ids.
// Everything it writes is discarded when the caller rolls back.
func seedDownProbeFixture(t *testing.T, ctx context.Context, tx pgx.Tx, tag string) (userID, itemID, accountID string) {
	t.Helper()
	userID = uuid.New().String()
	itemID = uuid.New().String()
	accountID = uuid.New().String()

	_, err := tx.Exec(ctx, `
		INSERT INTO users (id, clerk_user_id, email, subscription_tier, subscription_status, primary_region)
		VALUES ($1, $2, $3, 'premium', 'active', 'iad')`,
		userID, "test|"+tag+"-"+userID, tag+"-"+userID+"@example.com")
	require.NoError(t, err, "seed user")

	_, err = tx.Exec(ctx, `
		INSERT INTO linked_items (id, user_id, item_id, access_token_encrypted)
		VALUES ($1, $2, $3, 'enc-test-token')`, itemID, userID, tag+"-item-"+itemID)
	require.NoError(t, err, "seed linked_item")

	_, err = tx.Exec(ctx, `
		INSERT INTO accounts (id, linked_item_id, user_id, plaid_account_id, name, type, status)
		VALUES ($1, $2, $3, $4, 'Probe', 'depository', 'active')`,
		accountID, itemID, userID, "plaid-"+tag+"-"+accountID)
	require.NoError(t, err, "seed account")

	return userID, itemID, accountID
}
