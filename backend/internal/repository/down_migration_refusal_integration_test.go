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

		// Bounded rather than unbounded: this subtest empties a shared table and then DROPs it,
		// which needs ACCESS EXCLUSIVE. On a shared integration database a concurrent writer turns
		// that into a wait, and an unbounded wait surfaces as an opaque `deadlock detected`
		// (SQLSTATE 40P01) that reads like a defect in the migration rather than contention.
		// Observed exactly once, when two `go test -tags integration` invocations were accidentally
		// run against the same database at the same time — `-p 1` serialises packages WITHIN one
		// invocation, not BETWEEN two.
		_, err = tx.Exec(ctx, `SET LOCAL lock_timeout = '10s'`)
		require.NoError(t, err)

		// ⚠ NO `DROP TRIGGER` HERE, deliberately. An earlier version dropped 000152's trigger from
		// `accounts` first, on the theory that it must go before the table it writes to. It does
		// not: nothing in this subtest inserts an account or changes a status, so the trigger never
		// fires, and `DROP TABLE account_status_history` removes it along with the table. What the
		// DROP TRIGGER did do was take ACCESS EXCLUSIVE on ACCOUNTS — the busiest table in the
		// schema — BEFORE this transaction touched account_status_history, i.e. in the opposite
		// order from every ordinary writer (accounts row lock, then the trigger's insert). That is
		// the lock inversion that deadlocked.
		_, err = tx.Exec(ctx, `DELETE FROM account_status_history`)
		require.NoError(t, err)

		_, err = tx.Exec(ctx, down)
		require.NoError(t, err, "an empty trail destroys nothing; the rollback must still run")
	})
}

// 000035.down.sql must refuse while any transaction retains a NULL account_id or NULL
// linked_item_id — those NULLs are retained transaction history from a disconnected account or
// linked_item (ON DELETE SET NULL, added by 000035's up migration specifically as a "safety net"
// per its own header comment), not migration noise. Rolling 000035 back re-arms ON DELETE CASCADE
// and, to get there, unconditionally deletes every row carrying either NULL — exactly the kind of
// row #1569 requires this codebase to retain by default.
func TestDownMigration000035_RefusesWhileOrphanedTransactionsExist_Integration(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()
	down := readDownMigration(t, "000035_change_cascade_to_set_null.down.sql")

	insertOrphanTransaction := func(t *testing.T, ctx context.Context, tx pgx.Tx, userID string, accountID, itemID *string) {
		t.Helper()
		_, err := tx.Exec(ctx, `
			INSERT INTO transactions (id, user_id, account_id, linked_item_id, source, amount, date, name)
			VALUES ($1, $2, $3, $4, 'plaid', 42.00, CURRENT_DATE, 'Orphan Probe')`,
			uuid.New().String(), userID, accountID, itemID)
		require.NoError(t, err, "seed orphaned transaction")
	}

	t.Run("refuses when a transaction has a NULL account_id", func(t *testing.T) {
		tx, err := pool.Begin(ctx)
		require.NoError(t, err)
		defer tx.Rollback(ctx) //nolint:errcheck // always rolled back

		userID, itemID, _ := seedDownProbeFixture(t, ctx, tx, "n035-noacct")
		insertOrphanTransaction(t, ctx, tx, userID, nil, &itemID)

		var n int
		require.NoError(t, tx.QueryRow(ctx, `SELECT COUNT(*) FROM transactions WHERE account_id IS NULL`).Scan(&n))
		require.GreaterOrEqual(t, n, 1, "the fixture must exist or this test proves nothing")

		_, err = tx.Exec(ctx, down)
		require.Error(t, err, "000035.down.sql must refuse while a transaction retains a NULL account_id")
		assert.Contains(t, err.Error(), "refusing to roll back 000035")
	})

	t.Run("refuses when a transaction has a NULL linked_item_id", func(t *testing.T) {
		tx, err := pool.Begin(ctx)
		require.NoError(t, err)
		defer tx.Rollback(ctx) //nolint:errcheck // always rolled back

		userID, _, accountID := seedDownProbeFixture(t, ctx, tx, "n035-noitem")
		insertOrphanTransaction(t, ctx, tx, userID, &accountID, nil)

		var n int
		require.NoError(t, tx.QueryRow(ctx, `SELECT COUNT(*) FROM transactions WHERE linked_item_id IS NULL`).Scan(&n))
		require.GreaterOrEqual(t, n, 1, "the fixture must exist or this test proves nothing")

		_, err = tx.Exec(ctx, down)
		require.Error(t, err, "000035.down.sql must refuse while a transaction retains a NULL linked_item_id")
		assert.Contains(t, err.Error(), "refusing to roll back 000035")
	})

	t.Run("proceeds when no transaction is orphaned", func(t *testing.T) {
		tx, err := pool.Begin(ctx)
		require.NoError(t, err)
		defer tx.Rollback(ctx) //nolint:errcheck // always rolled back

		// The discriminating case, per feedback_validate_the_mutant_not_just_the_red: without it, a
		// down migration that refused unconditionally would satisfy the two subtests above and would
		// permanently break scripts/test-migrations.sh's rollback suite.
		_, err = tx.Exec(ctx, `DELETE FROM transactions WHERE account_id IS NULL OR linked_item_id IS NULL`)
		require.NoError(t, err)

		// A normal, fully-linked transaction coexists fine — it is not what the guard is watching.
		userID, itemID, accountID := seedDownProbeFixture(t, ctx, tx, "n035-clean")
		_, err = tx.Exec(ctx, `
			INSERT INTO transactions (id, user_id, account_id, linked_item_id, source, amount, date, name)
			VALUES ($1, $2, $3, $4, 'plaid', 42.00, CURRENT_DATE, 'Clean Probe')`,
			uuid.New().String(), userID, accountID, itemID)
		require.NoError(t, err, "seed a normal, fully-linked transaction")

		_, err = tx.Exec(ctx, down)
		require.NoError(t, err, "with no orphaned transaction, the rollback must run — otherwise the "+
			"migration suite's down -all cycle can never pass and the guard gets deleted")
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

type downGuardCase struct {
	label           string
	file            string
	tables          []string
	seedRefusingRow func(t *testing.T, ctx context.Context, tx pgx.Tx, f downGuardFixture)
	wantMsgContains []string
	// beforeProceed runs (if set) after tc.tables are emptied but before the "proceeds when
	// empty" subtest executes down.sql. Only 000051 needs it: its down.sql runs sequentially
	// AFTER 000160's (000160 > 000051), so by the time it really executes, 000160's down has
	// already DROPped holding_snapshots/liability_statements — but this test always runs
	// migration 180's full schema in isolation, where those two tables still exist and still
	// hold an unrelated FK onto `securities`. Dropping them here reproduces the schema 000051
	// actually sees on a genuine `down -all`, rather than a schema no real rollback order
	// produces.
	beforeProceed func(t *testing.T, ctx context.Context, tx pgx.Tx)
}

var downGuardCases = []downGuardCase{
	{
		label:  "000016_balance_snapshots",
		file:   "000016_create_balance_snapshots_table.down.sql",
		tables: []string{"balance_snapshots"},
		seedRefusingRow: func(t *testing.T, ctx context.Context, tx pgx.Tx, f downGuardFixture) {
			t.Helper()
			_, err := tx.Exec(ctx, `
				INSERT INTO balance_snapshots (account_id, user_id, current_balance, currency_code, snapshot_at, source)
				VALUES ($1, $2, 42.00, 'USD', now(), 'manual')`, f.accountID, f.userID)
			require.NoError(t, err, "seed balance_snapshots")
		},
		wantMsgContains: []string{"refusing to roll back 000016", "balance_snapshots"},
	},
	{
		label:  "000017_balance_snapshots_dedup",
		file:   "000017_migrate_to_timestamp_snapshots.down.sql",
		tables: []string{"balance_snapshots"},
		seedRefusingRow: func(t *testing.T, ctx context.Context, tx pgx.Tx, f downGuardFixture) {
			t.Helper()
			// 000017's guard fires on DATE(bs2.snapshot_at) = DATE(bs1.snapshot_at) -- a calendar-day
			// comparison. Anchoring both rows off now() (as this fixture originally did) fails for
			// roughly one hour a day: in the hour after local midnight, now() - interval '1 hour'
			// falls on the PREVIOUS calendar date, the DATE()-equality predicate can never match,
			// the guard correctly finds nothing to protect, and this test's assertion that it
			// refuses goes red -- even though the guard itself is correct (its predicate is
			// byte-identical to the DELETE it protects). Fix: pin the session to UTC for this
			// transaction and anchor both rows on a fixed, arbitrary, nowhere-near-midnight literal
			// (noon) instead of the real wall clock, so the two rows are guaranteed same-day
			// regardless of when this test actually runs. Verified directly against the failure
			// mode: with the session TimeZone forced so real now() lands at local 00:00 -- the
			// exact condition that broke this fixture -- DATE(now()) = DATE(now() - interval
			// '1 hour') evaluates false, reproducing the bug, while this fixed-anchor pair still
			// evaluates same-day true under the same forced TimeZone.
			_, err := tx.Exec(ctx, `SET LOCAL TIME ZONE 'UTC'`)
			require.NoError(t, err, "pin session TimeZone to UTC for this fixture")
			_, err = tx.Exec(ctx, `
				INSERT INTO balance_snapshots (account_id, user_id, current_balance, currency_code, snapshot_at, source)
				VALUES ($1, $2, 42.00, 'USD', TIMESTAMPTZ '2026-01-15 12:00:00+00', 'manual')`, f.accountID, f.userID)
			require.NoError(t, err, "seed balance_snapshots (latest)")
			_, err = tx.Exec(ctx, `
				INSERT INTO balance_snapshots (account_id, user_id, current_balance, currency_code, snapshot_at, source)
				VALUES ($1, $2, 10.00, 'USD', TIMESTAMPTZ '2026-01-15 12:00:00+00' - interval '1 hour', 'manual')`, f.accountID, f.userID)
			require.NoError(t, err, "seed balance_snapshots (earlier same day)")
		},
		wantMsgContains: []string{"refusing to roll back 000017", "permanently deleted", "snapshot_at"},
	},
	{
		label:  "000020_rule_execution_history",
		file:   "000020_create_categorization_rules.down.sql",
		tables: []string{"rule_execution_history"},
		seedRefusingRow: func(t *testing.T, ctx context.Context, tx pgx.Tx, f downGuardFixture) {
			t.Helper()
			_, err := tx.Exec(ctx, `
				INSERT INTO rule_execution_history (id, rule_id, transaction_id, action_type, result, user_id)
				VALUES ($1, NULL, NULL, 'category', 'applied', $2)`, uuid.New().String(), f.userID)
			require.NoError(t, err, "seed rule_execution_history")
		},
		wantMsgContains: []string{"refusing to roll back 000020", "rule_execution_history"},
	},
	{
		label:  "000027_rollover_history",
		file:   "000027_create_rollover_history_table.down.sql",
		tables: []string{"rollover_history"},
		seedRefusingRow: func(t *testing.T, ctx context.Context, tx pgx.Tx, f downGuardFixture) {
			t.Helper()
			_, err := tx.Exec(ctx, `
				INSERT INTO rollover_history (id, user_id, budget_id, source_budget_category_id, amount, rollover_type, budget_period_start, budget_period_end)
				VALUES ($1, $2, $3, $4, 10, 'full', now() - interval '1 month', now())`,
				uuid.New().String(), f.userID, f.budgetID, f.budgetCategoryID)
			require.NoError(t, err, "seed rollover_history")
		},
		wantMsgContains: []string{"refusing to roll back 000027", "rollover_history"},
	},
	{
		label:  "000051_investment_transactions",
		file:   "000051_create_investment_tables.down.sql",
		tables: []string{"investment_transactions"},
		seedRefusingRow: func(t *testing.T, ctx context.Context, tx pgx.Tx, f downGuardFixture) {
			t.Helper()
			_, err := tx.Exec(ctx, `
				INSERT INTO investment_transactions (id, user_id, account_id, security_id, plaid_investment_transaction_id, type, name, amount, date)
				VALUES ($1, $2, $3, $4, $5, 'buy', 'Probe Buy', 100.00, CURRENT_DATE)`,
				uuid.New().String(), f.userID, f.accountID, f.securityID, "plaid-inv-"+uuid.New().String())
			require.NoError(t, err, "seed investment_transactions")
		},
		wantMsgContains: []string{"refusing to roll back 000051", "investment_transactions"},
		beforeProceed: func(t *testing.T, ctx context.Context, tx pgx.Tx) {
			t.Helper()
			_, err := tx.Exec(ctx, `DROP TABLE IF EXISTS holding_snapshots CASCADE`)
			require.NoError(t, err, "simulate 000160's down (holding_snapshots) having already run")
			_, err = tx.Exec(ctx, `DROP TABLE IF EXISTS liability_statements CASCADE`)
			require.NoError(t, err, "simulate 000160's down (liability_statements) having already run")
		},
	},
	{
		label:  "000096_portfolio_snapshots",
		file:   "000096_create_portfolio_snapshots.down.sql",
		tables: []string{"portfolio_snapshots"},
		seedRefusingRow: func(t *testing.T, ctx context.Context, tx pgx.Tx, f downGuardFixture) {
			t.Helper()
			_, err := tx.Exec(ctx, `
				INSERT INTO portfolio_snapshots (id, user_id, snapshot_date, total_value, total_cost_basis, total_gain_loss)
				VALUES ($1, $2, CURRENT_DATE, 100, 90, 10)`, uuid.New().String(), f.userID)
			require.NoError(t, err, "seed portfolio_snapshots")
		},
		wantMsgContains: []string{"refusing to roll back 000096", "portfolio_snapshots"},
	},
	{
		label:  "000097_savings_goal_contributions_live_row",
		file:   "000097_savings_progress_and_budget_linkage.down.sql",
		tables: []string{"savings_goal_contributions"},
		seedRefusingRow: func(t *testing.T, ctx context.Context, tx pgx.Tx, f downGuardFixture) {
			t.Helper()
			_, err := tx.Exec(ctx, `
				INSERT INTO savings_goal_contributions (id, savings_goal_id, amount, source, occurred_on)
				VALUES ($1, $2, 25.00, 'manual', CURRENT_DATE)`, uuid.New().String(), f.savingsGoalID)
			require.NoError(t, err, "seed savings_goal_contributions")
		},
		wantMsgContains: []string{"refusing to roll back 000097", "savings_goal_contributions"},
	},
	{
		label:  "000157_rule_execution_history_orphan",
		file:   "000157_rule_execution_history_transaction_set_null.down.sql",
		tables: []string{"rule_execution_history"},
		seedRefusingRow: func(t *testing.T, ctx context.Context, tx pgx.Tx, f downGuardFixture) {
			t.Helper()
			_, err := tx.Exec(ctx, `
				INSERT INTO rule_execution_history (id, rule_id, transaction_id, action_type, result, user_id)
				VALUES ($1, NULL, NULL, 'category', 'applied', $2)`, uuid.New().String(), f.userID)
			require.NoError(t, err, "seed orphaned rule_execution_history")
		},
		wantMsgContains: []string{"refusing to roll back 000157", "transaction_id"},
	},
	{
		label:  "000160_holding_and_liability_history",
		file:   "000160_create_holding_and_liability_history.down.sql",
		tables: []string{"holding_snapshots", "liability_statements"},
		seedRefusingRow: func(t *testing.T, ctx context.Context, tx pgx.Tx, f downGuardFixture) {
			t.Helper()
			_, err := tx.Exec(ctx, `
				INSERT INTO holding_snapshots (id, user_id, account_id, security_id, as_of_date, quantity)
				VALUES ($1, $2, $3, $4, CURRENT_DATE, 5)`, uuid.New().String(), f.userID, f.accountID, f.securityID)
			require.NoError(t, err, "seed holding_snapshots")
			_, err = tx.Exec(ctx, `
				INSERT INTO liability_statements (id, user_id, account_id, liability_type, as_of_date, details)
				VALUES ($1, $2, $3, 'credit', CURRENT_DATE, '{}')`, uuid.New().String(), f.userID, f.accountID)
			require.NoError(t, err, "seed liability_statements")
		},
		wantMsgContains: []string{"refusing to roll back 000160", "holding_snapshots", "liability_statements"},
	},
	{
		label:  "000163_portfolio_snapshot_repairs",
		file:   "000163_portfolio_snapshot_repair_ledger.down.sql",
		tables: []string{"portfolio_snapshot_repairs"},
		seedRefusingRow: func(t *testing.T, ctx context.Context, tx pgx.Tx, f downGuardFixture) {
			t.Helper()
			_, err := tx.Exec(ctx, `
				INSERT INTO portfolio_snapshot_repairs
					(id, user_id, snapshot_date, outcome, reason, expected_account_count, observed_account_count,
					 previous_total_value, previous_total_cost_basis, previous_total_gain_loss)
				VALUES ($1, $2, CURRENT_DATE, 'verified', 'probe', 1, 1, 100, 90, 10)`, uuid.New().String(), f.userID)
			require.NoError(t, err, "seed portfolio_snapshot_repairs")
		},
		wantMsgContains: []string{"refusing to roll back 000163", "portfolio_snapshot_repairs"},
	},
	{
		label:  "000164_definition_snapshot_columns",
		file:   "000164_audit_trail_survives_definition_purge.down.sql",
		tables: []string{"rollover_history", "boost_executions", "rule_execution_history"},
		seedRefusingRow: func(t *testing.T, ctx context.Context, tx pgx.Tx, f downGuardFixture) {
			t.Helper()
			_, err := tx.Exec(ctx, `
				INSERT INTO boost_executions (id, boost_id, user_id, boost_name, definition_snapshot)
				VALUES ($1, NULL, $2, 'Probe Boost', '{}')`, uuid.New().String(), f.userID)
			require.NoError(t, err, "seed boost_executions with a definition_snapshot")
		},
		wantMsgContains: []string{"refusing to roll back 000164", "definition_snapshot"},
	},
	{
		// #1625 final review B3. 000190's down dropped archived_at unconditionally and its own
		// comment claimed the resulting double-count was "the pre-#1625 behaviour this reverts to,
		// not new damage". That was FALSE: archived_at is the only surviving record that a
		// contribution belongs to a CLOSED contributions period (000190 added a column precisely
		// because no other signal survives), so dropping it resurrects the 100 + 100 = 200
		// double-count for every goal that round-tripped its basis while 000190 was applied — a
		// derived FINANCIAL figure silently becoming wrong, which is the failure class this ticket
		// exists to remove. 000180 already refuses on this same table while deleted_at is set;
		// this mirrors it.
		label:  "000190_archived_contribution",
		file:   "000190_add_contribution_archived_at.down.sql",
		tables: []string{"savings_goal_contributions"},
		seedRefusingRow: func(t *testing.T, ctx context.Context, tx pgx.Tx, f downGuardFixture) {
			t.Helper()
			_, err := tx.Exec(ctx, `
				INSERT INTO savings_goal_contributions (id, savings_goal_id, amount, source, occurred_on, archived_at)
				VALUES ($1, $2, 100.00, 'manual', CURRENT_DATE, now())`, uuid.New().String(), f.savingsGoalID)
			require.NoError(t, err, "seed an ARCHIVED savings_goal_contribution")
		},
		wantMsgContains: []string{"refusing to roll back 000190", "archived_at", "double-counting"},
	},
	{
		label:  "000169_transaction_annotation_history",
		file:   "000169_record_the_prior_annotation.down.sql",
		tables: []string{"transaction_annotation_history"},
		seedRefusingRow: func(t *testing.T, ctx context.Context, tx pgx.Tx, f downGuardFixture) {
			t.Helper()
			_, err := tx.Exec(ctx, `
				INSERT INTO transaction_annotation_history (id, user_id, transaction_id, change_source, prior_reviewed, new_reviewed)
				VALUES ($1, $2, $3, 'category', false, true)`, uuid.New().String(), f.userID, f.transactionID)
			require.NoError(t, err, "seed transaction_annotation_history")
		},
		wantMsgContains: []string{"refusing to roll back 000169", "transaction_annotation_history"},
	},
}

func TestDownMigrationGuards_RefuseWithRowsProceedWhenEmpty_Integration(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()

	for _, tc := range downGuardCases {
		tc := tc
		t.Run(tc.label, func(t *testing.T) {
			down := readDownMigration(t, tc.file)

			t.Run("refuses when rows exist", func(t *testing.T) {
				tx, err := pool.Begin(ctx)
				require.NoError(t, err)
				defer tx.Rollback(ctx) //nolint:errcheck // always rolled back

				f := seedDownGuardFixture(t, ctx, tx, tc.label+"-refuse")
				tc.seedRefusingRow(t, ctx, tx, f)

				_, err = tx.Exec(ctx, down)
				require.Error(t, err, "%s must refuse while %v hold the row(s) it protects", tc.file, tc.tables)
				for _, want := range tc.wantMsgContains {
					assert.Contains(t, err.Error(), want,
						"%s refusal message must name what it refused and why (missing %q)", tc.file, want)
				}
			})

			t.Run("proceeds when empty", func(t *testing.T) {
				tx, err := pool.Begin(ctx)
				require.NoError(t, err)
				defer tx.Rollback(ctx) //nolint:errcheck // always rolled back

				_, err = tx.Exec(ctx, `SET LOCAL lock_timeout = '10s'`)
				require.NoError(t, err)

				for _, table := range tc.tables {
					_, err = tx.Exec(ctx, `DELETE FROM `+table) //nolint:gosec // table is a fixed literal from downGuardCases, never user input
					require.NoError(t, err, "empty %s before testing the proceed path", table)
				}

				if tc.beforeProceed != nil {
					tc.beforeProceed(t, ctx, tx)
				}

				_, err = tx.Exec(ctx, down)
				require.NoError(t, err, "%s must proceed once %v are empty", tc.file, tc.tables)
			})
		})
	}
}

type downGuardFixture struct {
	userID, itemID, accountID  string
	securityID                 string
	budgetID, budgetCategoryID string
	savingsGoalID              string
	transactionID              string
}

func seedDownGuardFixture(t *testing.T, ctx context.Context, tx pgx.Tx, tag string) downGuardFixture {
	t.Helper()
	userID, itemID, accountID := seedDownProbeFixture(t, ctx, tx, tag)

	securityID := uuid.New().String()
	_, err := tx.Exec(ctx, `
		INSERT INTO securities (id, plaid_security_id, name, ticker_symbol, type)
		VALUES ($1, $2, 'Probe Security', 'PRB', 'equity')`, securityID, "plaid-sec-"+tag+"-"+securityID)
	require.NoError(t, err, "seed security")

	budgetID := uuid.New().String()
	_, err = tx.Exec(ctx, `
		INSERT INTO budgets (id, user_id, name, amount, period, start_date, series_id)
		VALUES ($1, $2, $3, 500, 'monthly', CURRENT_DATE, $1)`, budgetID, userID, tag+"-budget")
	require.NoError(t, err, "seed budget")

	customCategoryID := uuid.New().String()
	_, err = tx.Exec(ctx, `
		INSERT INTO custom_categories (id, user_id, name)
		VALUES ($1, $2, $3)`, customCategoryID, userID, tag+"-cat")
	require.NoError(t, err, "seed custom_category")

	budgetCategoryID := uuid.New().String()
	_, err = tx.Exec(ctx, `
		INSERT INTO budget_categories (id, budget_id, category_kind, custom_category_id, allocated_amount)
		VALUES ($1, $2, 'custom', $3, 100)`, budgetCategoryID, budgetID, customCategoryID)
	require.NoError(t, err, "seed budget_category")

	savingsGoalID := uuid.New().String()
	_, err = tx.Exec(ctx, `
		INSERT INTO savings_goals (id, user_id, name, target_amount, account_id, progress_basis)
		VALUES ($1, $2, $3, 1000, $4, 'manual')`, savingsGoalID, userID, tag+"-goal", accountID)
	require.NoError(t, err, "seed savings_goal")

	transactionID := uuid.New().String()
	_, err = tx.Exec(ctx, `
		INSERT INTO transactions (id, user_id, account_id, source, amount, date, name)
		VALUES ($1, $2, $3, 'plaid', 42.00, CURRENT_DATE, $4)`, transactionID, userID, accountID, tag+"-txn")
	require.NoError(t, err, "seed transaction")

	return downGuardFixture{
		userID: userID, itemID: itemID, accountID: accountID,
		securityID:       securityID,
		budgetID:         budgetID,
		budgetCategoryID: budgetCategoryID,
		savingsGoalID:    savingsGoalID,
		transactionID:    transactionID,
	}
}
