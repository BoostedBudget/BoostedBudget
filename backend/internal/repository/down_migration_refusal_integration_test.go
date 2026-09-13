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
	"strings"
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
	// empty" subtest executes down.sql. For example, 000051 runs sequentially
	// AFTER 000160's (000160 > 000051), so by the time it really executes, 000160's down has
	// already DROPped holding_snapshots/liability_statements — but this test always runs
	// migration 180's full schema in isolation, where those two tables still exist and still
	// hold an unrelated FK onto `securities`. Dropping them here reproduces the schema 000051
	// actually sees on a genuine `down -all`, rather than a schema no real rollback order
	// produces.
	beforeProceed func(t *testing.T, ctx context.Context, tx pgx.Tx)
	// clearRefusingState, when set, REPLACES the `DELETE FROM <table>` loop in the
	// "proceeds when empty" half. The four #1625 lifecycle-column cases need it: their guards
	// refuse on a PREDICATE over a table the rest of the schema depends on (accounts,
	// transactions, savings_goals and friends, custom_categories), not on the table being
	// populated at all. Emptying those outright fails on unrelated FK dependents and would
	// anyway test a state no rollback ever sees; clearing the predicate is the state the guard
	// is actually about. tables stays populated for the refusal message.
	clearRefusingState func(t *testing.T, ctx context.Context, tx pgx.Tx)
}

// config158Tables are the four tables 000158's down migration counts tombstones in, in the order
// its RAISE message names them.
var config158Tables = []string{"savings_goals", "boosts", "budgets", "budget_categories"}

// unwindGroupedBudgetDependencies reproduces the rollback order: 000205's
// triggers must be removed before 000158/159 can drop the columns they reference.
func unwindGroupedBudgetDependencies(t *testing.T, ctx context.Context, tx pgx.Tx) {
	t.Helper()
	_, err := tx.Exec(ctx, readDownMigration(t, "000205_budget_groups.down.sql"))
	require.NoError(t, err, "unwind later grouped-budget migration before testing lifecycle-column rollback")
}

// clear158Tombstones clears the tombstone PREDICATE on all four of 000158's tables. It is the
// "proceeds when empty" half for every one of the four cases: the guard refuses on deleted_at over
// tables the rest of the schema depends on, not on those tables being populated.
func clear158Tombstones(t *testing.T, ctx context.Context, tx pgx.Tx) {
	t.Helper()
	for _, table := range config158Tables {
		_, err := tx.Exec(ctx, //nolint:gosec // fixed literals from config158Tables, never user input
			`UPDATE `+table+` SET deleted_at = NULL, deleted_reason = NULL, deleted_by = NULL`)
		require.NoErrorf(t, err, "clear %s tombstones", table)
	}
}

// isolate158Tombstones makes `keep` the ONLY table that can be holding a tombstone when the case
// executes the down migration, and asserts it starts at zero so the seeded row is the whole count.
// Without this a stray tombstone left in a sibling table — by an earlier test in this shared
// database, or by the fixture itself — would make all four cases refuse no matter which counts the
// guard actually performs, which is the exact way a per-table mutant escapes detection.
func isolate158Tombstones(t *testing.T, ctx context.Context, tx pgx.Tx, keep string) {
	t.Helper()
	for _, table := range config158Tables {
		if table == keep {
			continue
		}
		_, err := tx.Exec(ctx, //nolint:gosec // fixed literals from config158Tables, never user input
			`UPDATE `+table+` SET deleted_at = NULL, deleted_reason = NULL, deleted_by = NULL`)
		require.NoErrorf(t, err, "clear %s tombstones so only %s can explain the refusal", table, keep)
	}
	var n int
	require.NoError(t, tx.QueryRow(ctx, //nolint:gosec // fixed literal from config158Tables
		`SELECT COUNT(*) FROM `+keep+` WHERE deleted_at IS NOT NULL`).Scan(&n))
	require.Zerof(t, n, "%s must hold no tombstone before this case seeds one, or the count this "+
		"case asserts on is not the row it seeded", keep)
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
	// The four entries below are #1625. Each was a CHECKED EXCEPTION in
	// down_migration_destruction_discovery_test.go: a down migration dropping a lifecycle column
	// with nothing but prose standing in for a control. What promoted them from "recorded" to
	// "guarded" was not a rereading of the SQL — the whole point of this ticket is that rereading
	// SQL produced confidently false answers twice — but a real sequential `migrate down` against
	// a scratch database holding the exact rows each rationale is about. That run reached and
	// completed all four downs, so the loss each header describes is REACHABLE, not merely
	// arguable. See the header of each .down.sql for the command and its recorded output.
	{
		label:  "000154_accounts_removed_reason",
		file:   "000154_add_accounts_removed_reason.down.sql",
		tables: []string{"accounts"},
		seedRefusingRow: func(t *testing.T, ctx context.Context, tx pgx.Tx, f downGuardFixture) {
			t.Helper()
			_, err := tx.Exec(ctx, `
				UPDATE accounts SET status = 'removed', removed_at = now(), removed_reason = 'user_disconnect'
				WHERE id = $1`, f.accountID)
			require.NoError(t, err, "seed an account removed for a RECORDED cause")
		},
		clearRefusingState: func(t *testing.T, ctx context.Context, tx pgx.Tx) {
			t.Helper()
			_, err := tx.Exec(ctx, `UPDATE accounts SET removed_reason = NULL`)
			require.NoError(t, err, "clear every recorded removal cause")
		},
		wantMsgContains: []string{"refusing to roll back 000154", "accounts", "removed_reason"},
	},
	{
		label:  "000156_transaction_tombstones",
		file:   "000156_add_transactions_tombstone.down.sql",
		tables: []string{"transactions"},
		seedRefusingRow: func(t *testing.T, ctx context.Context, tx pgx.Tx, f downGuardFixture) {
			t.Helper()
			_, err := tx.Exec(ctx, `
				UPDATE transactions
				SET deleted_at = now(), deleted_reason = 'provider_removed_posted', deleted_by = 'system'
				WHERE id = $1`, f.transactionID)
			require.NoError(t, err, "seed a TOMBSTONED transaction")
		},
		clearRefusingState: func(t *testing.T, ctx context.Context, tx pgx.Tx) {
			t.Helper()
			_, err := tx.Exec(ctx, `
				UPDATE transactions SET deleted_at = NULL, deleted_reason = NULL, deleted_by = NULL`)
			require.NoError(t, err, "clear every transaction tombstone")
		},
		wantMsgContains: []string{"refusing to roll back 000156", "transactions", "deleted_at"},
	},
	// ── 000158 drives FOUR tables, so it gets FOUR cases: one tombstone, in one table, per case. ──
	//
	// ⚠ WHY NOT ONE CASE SEEDING ALL FOUR, which is what shipped and what this replaces. 000158's
	// guard is a SUM of four independent counts. A case that seeds a tombstone in every table
	// refuses even when the DO block counts only ONE of them, so it cannot tell a complete guard
	// from a quarter of one. Concretely: with the single savings_goals-only case that stood here,
	// deleting n_cats from 000158's down left this whole package GREEN, while a real
	// `migrate goto 157` against a database holding a budget_categories tombstone completed and
	// resurrected it.
	//
	// ⚠ WHAT WAS MUTATION-VERIFIED, AND A RETRACTION. Commit d518885a's message said
	// "Mutation-verified, all five ways". That is true only at WHOLE-DO-BLOCK granularity — one
	// mutant per file, delete the block, watch that file's case go red — and it says nothing about
	// the four counts inside 000158's block, three of which no test exercised. The per-table
	// granularity was verified separately, and that is what the four cases below rest on: each of
	// n_goals / n_boosts / n_budgets / n_cats was removed from 000158's down in turn — its
	// `SELECT ... INTO`, its term in the IF sum, its RAISE argument AND its `%` slot in the format
	// string, because dropping the argument alone raises "too few parameters specified for RAISE"
	// and would have been an invalid mutant that fails for the wrong reason. Each time exactly the
	// matching case went red on its own `require.Error`, reporting "An error is expected but got
	// nil" — i.e. the mutated file ran to COMPLETION, which is what makes the mutant valid — and no
	// case went red for a mutant that was not its own.
	//
	// Every case deliberately uses a name that collides with NOTHING. A colliding pair would abort
	// on the unconditional UNIQUE (user_id, name) this file re-adds, and that pre-existing,
	// collision-dependent abort is precisely what this guard exists to generalise — so seeding a
	// collision would let a case pass against no guard at all.
	{
		label:  "000158_config_parent_tombstones/savings_goals",
		file:   "000158_soft_delete_config_parents.down.sql",
		tables: config158Tables,
		seedRefusingRow: func(t *testing.T, ctx context.Context, tx pgx.Tx, f downGuardFixture) {
			t.Helper()
			isolate158Tombstones(t, ctx, tx, "savings_goals")
			_, err := tx.Exec(ctx, `
				UPDATE savings_goals SET deleted_at = now(), deleted_reason = 'user_deleted', deleted_by = 'user'
				WHERE id = $1`, f.savingsGoalID)
			require.NoError(t, err, "seed a TOMBSTONED savings_goal with a unique name")
		},
		clearRefusingState: clear158Tombstones,
		beforeProceed:      unwindGroupedBudgetDependencies,
		wantMsgContains:    []string{"refusing to roll back 000158", "1 savings_goals", "deleted_at"},
	},
	{
		label:  "000158_config_parent_tombstones/boosts",
		file:   "000158_soft_delete_config_parents.down.sql",
		tables: config158Tables,
		seedRefusingRow: func(t *testing.T, ctx context.Context, tx pgx.Tx, f downGuardFixture) {
			t.Helper()
			isolate158Tombstones(t, ctx, tx, "boosts")
			_, err := tx.Exec(ctx, `
				INSERT INTO boosts (id, user_id, name, boost_type, enabled, deleted_at, deleted_reason, deleted_by)
				VALUES ($1, $2, $3, 'savings_goal', true, now(), 'user_deleted', 'user')`,
				uuid.New().String(), f.userID, "tombstoned-only-boost-"+f.userID)
			require.NoError(t, err, "seed a TOMBSTONED boost with a unique name")
		},
		clearRefusingState: clear158Tombstones,
		beforeProceed:      unwindGroupedBudgetDependencies,
		wantMsgContains:    []string{"refusing to roll back 000158", "1 boosts", "deleted_at"},
	},
	{
		label:  "000158_config_parent_tombstones/budgets",
		file:   "000158_soft_delete_config_parents.down.sql",
		tables: config158Tables,
		seedRefusingRow: func(t *testing.T, ctx context.Context, tx pgx.Tx, f downGuardFixture) {
			t.Helper()
			isolate158Tombstones(t, ctx, tx, "budgets")
			_, err := tx.Exec(ctx, `
				UPDATE budgets SET deleted_at = now(), deleted_reason = 'user_deleted', deleted_by = 'user'
				WHERE id = $1`, f.budgetID)
			require.NoError(t, err, "seed a TOMBSTONED budget with a unique name")
		},
		clearRefusingState: clear158Tombstones,
		beforeProceed:      unwindGroupedBudgetDependencies,
		wantMsgContains:    []string{"refusing to roll back 000158", "1 budgets", "deleted_at"},
	},
	{
		label:  "000158_config_parent_tombstones/budget_categories",
		file:   "000158_soft_delete_config_parents.down.sql",
		tables: config158Tables,
		seedRefusingRow: func(t *testing.T, ctx context.Context, tx pgx.Tx, f downGuardFixture) {
			t.Helper()
			isolate158Tombstones(t, ctx, tx, "budget_categories")
			// The table the shipped single case never touched, and the one whose mutant a real
			// `migrate goto 157` proved reachable: the row survives, deleted_at does not.
			_, err := tx.Exec(ctx, `
				UPDATE budget_categories SET deleted_at = now(), deleted_reason = 'user_deleted', deleted_by = 'user'
				WHERE id = $1`, f.budgetCategoryID)
			require.NoError(t, err, "seed a TOMBSTONED budget_category")
		},
		clearRefusingState: clear158Tombstones,
		beforeProceed:      unwindGroupedBudgetDependencies,
		wantMsgContains:    []string{"refusing to roll back 000158", "1 budget_categories", "deleted_at"},
	},
	{
		label:         "000159_custom_category_tombstones",
		file:          "000159_soft_delete_custom_categories.down.sql",
		tables:        []string{"custom_categories"},
		beforeProceed: unwindGroupedBudgetDependencies,
		seedRefusingRow: func(t *testing.T, ctx context.Context, tx pgx.Tx, f downGuardFixture) {
			t.Helper()
			// A name no live row shares, for the same reason as 000158's entry.
			_, err := tx.Exec(ctx, `
				INSERT INTO custom_categories (id, user_id, name, deleted_at, deleted_reason, deleted_by)
				VALUES ($1, $2, $3, now(), 'user_deleted', 'user')`,
				uuid.New().String(), f.userID, "tombstoned-only-"+f.userID)
			require.NoError(t, err, "seed a TOMBSTONED custom_category with a unique name")
		},
		clearRefusingState: func(t *testing.T, ctx context.Context, tx pgx.Tx) {
			t.Helper()
			_, err := tx.Exec(ctx, `
				DELETE FROM custom_categories WHERE deleted_at IS NOT NULL`)
			require.NoError(t, err, "remove every tombstoned custom_category")
		},
		wantMsgContains: []string{"refusing to roll back 000159", "custom_categories", "deleted_at"},
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

				if tc.clearRefusingState != nil {
					tc.clearRefusingState(t, ctx, tx)
				} else {
					for _, table := range tc.tables {
						_, err = tx.Exec(ctx, `DELETE FROM `+table) //nolint:gosec // table is a fixed literal from downGuardCases, never user input
						require.NoError(t, err, "empty %s before testing the proceed path", table)
					}
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

// TestDownMigration000190_RefusalIsNotTOCTOURacy_Integration is #1625 second fix wave M4.
//
// 000190's refusal did `SELECT COUNT(*)` in a DO block and then, as a SEPARATE statement,
// `ALTER TABLE ... DROP COLUMN`. Nothing held the table between them. The production archive path
// (savings_contribution_repository.ArchiveByGoal) can set archived_at inside that window: it takes
// only ROW EXCLUSIVE, which does not conflict with the ACCESS SHARE a bare COUNT(*) takes, so it
// commits, and the ALTER then acquires its ACCESS EXCLUSIVE and drops the marker the guard had
// just decided did not exist. The guard reports success and the double-count it exists to prevent
// is resurrected — the failure mode is "the refusal is silently bypassed", not "the refusal is
// noisy", which is why a comment about narrow windows is not an answer.
//
// The fix is `LOCK TABLE ... IN ACCESS EXCLUSIVE MODE` inside the same transaction, BEFORE the
// count. golang-migrate runs a migration file in one transaction, so the lock is held from before
// the count until the ALTER.
//
// Asserted two independent ways, because either alone can pass against something weaker:
//
//  1. the guard's own backend really holds AccessExclusiveLock on the table after the DO block —
//     a direct observation of the lock, not of its consequences;
//  2. a SECOND connection attempting the production archive write is BLOCKED (lock_timeout)
//     rather than committing — the consequence itself, on a real concurrent writer.
//
// Only the guard PREFIX is executed here (everything before the ALTER), which is the only way to
// observe the window: running the whole file would take ACCESS EXCLUSIVE at the ALTER regardless
// and prove nothing about the gap. The prefix is derived from the file's own bytes.
func TestDownMigration000190_RefusalIsNotTOCTOURacy_Integration(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()

	down := readDownMigration(t, "000190_add_contribution_archived_at.down.sql")
	idx := strings.Index(down, "ALTER TABLE")
	require.Greater(t, idx, 0,
		"000190's down no longer contains an ALTER TABLE — this test can no longer locate the "+
			"window between the guard and the drop, so it is proving nothing")
	guardOnly := down[:idx]
	require.Contains(t, guardOnly, "SELECT COUNT(*)",
		"the prefix must contain the guard's count, or the split picked the wrong statement")

	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx) //nolint:errcheck // always rolled back

	// The guard only proceeds past its RAISE when nothing is archived. Uncommitted, like every
	// other test in this file.
	_, err = tx.Exec(ctx, `SET LOCAL lock_timeout = '10s'`)
	require.NoError(t, err)
	_, err = tx.Exec(ctx, `DELETE FROM savings_goal_contributions`)
	require.NoError(t, err)

	_, err = tx.Exec(ctx, guardOnly)
	require.NoError(t, err, "the guard must proceed on an empty table")

	// (1) The lock itself.
	var modes []string
	rows, err := tx.Query(ctx, `
		SELECT mode FROM pg_locks
		WHERE relation = 'savings_goal_contributions'::regclass
		  AND pid = pg_backend_pid()`)
	require.NoError(t, err)
	for rows.Next() {
		var mode string
		require.NoError(t, rows.Scan(&mode))
		modes = append(modes, mode)
	}
	require.NoError(t, rows.Err())
	rows.Close()
	assert.Contains(t, modes, "AccessExclusiveLock",
		"the guard did not lock savings_goal_contributions before counting (held: %v). Without it "+
			"an archive can commit between the count and the DROP COLUMN, and the refusal is "+
			"bypassed silently.", modes)

	// (2) The consequence, on a genuinely separate connection. `WHERE false` touches no row, so a
	// row-level conflict cannot explain a block: only the TABLE lock can. It is the same
	// ROW EXCLUSIVE the production archive UPDATE takes.
	probe, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer probe.Rollback(ctx) //nolint:errcheck // always rolled back
	_, err = probe.Exec(ctx, `SET LOCAL lock_timeout = '750ms'`)
	require.NoError(t, err)
	_, err = probe.Exec(ctx, `UPDATE savings_goal_contributions SET archived_at = now() WHERE false`)
	require.Error(t, err,
		"a concurrent archive write was NOT blocked while 000190's guard held the table — it can "+
			"therefore land between the count and the DROP COLUMN")
	assert.Contains(t, err.Error(), "lock timeout",
		"the write must fail by waiting on the guard's lock, not for some other reason: %v", err)
}

// TestDownMigration000149_AbortsBeforeItCanDropRemovedAt_Integration is #1625 second fix wave M5.
//
// down_migration_destruction_discovery_test.go carries 000149 as a CHECKED EXCEPTION: a down
// migration that drops a lifecycle column (accounts.removed_at) with no `RAISE EXCEPTION` refusal.
// Its recorded reason said dropping the column makes a removed account indistinguishable from a
// live one, so its balance re-enters every current-state total.
//
// That reason was FALSE, and nothing in this repo checks that an allowlist reason is true — which
// is why this test exists rather than a third revision of the prose. Step 2 of 000149's down
// re-adds accounts_valid_status as CHECK (status IN ('active','replaced','expired')), and Postgres
// validates a newly added CHECK against existing rows. So on any database holding a removed
// account the rollback ABORTS at Step 2, and Step 3's DROP COLUMN is never reached. The hazard is
// a hard failure partway through a `down -all` with a message that says nothing about retention —
// not silent resurrection.
//
// Two assertions, because the abort alone would also be produced by an unrelated failure: the
// error must name accounts_valid_status, and removed_at must still be there afterwards.
func TestDownMigration000149_AbortsBeforeItCanDropRemovedAt_Integration(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()
	down := readDownMigration(t, "000149_add_accounts_removed_status.down.sql")

	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx) //nolint:errcheck // always rolled back

	_, _, accountID := seedDownProbeFixture(t, ctx, tx, "000149-abort")
	_, err = tx.Exec(ctx,
		`UPDATE accounts SET status = 'removed', removed_at = now(), removed_reason = 'user_disconnect' WHERE id = $1`,
		accountID)
	require.NoError(t, err, "seed a REMOVED account — the state whose loss the exception reason is about")

	_, err = tx.Exec(ctx, down)
	require.Error(t, err,
		"000149's down completed with a removed account present. If that is now true, the checked "+
			"exception's re-derived reason is wrong again and the column really is being dropped.")
	assert.Contains(t, err.Error(), "accounts_valid_status",
		"the rollback must abort on the narrowed status CHECK (Step 2), which is what makes Step "+
			"3's DROP COLUMN unreachable: %v", err)

	// The transaction is aborted, so the column check needs its own.
	probe, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer probe.Rollback(ctx) //nolint:errcheck // always rolled back
	var exists bool
	require.NoError(t, probe.QueryRow(ctx, `
		SELECT EXISTS(
			SELECT 1 FROM information_schema.columns
			WHERE table_schema = current_schema() AND table_name = 'accounts' AND column_name = 'removed_at')
	`).Scan(&exists))
	assert.True(t, exists, "accounts.removed_at must survive: the rollback never reached its DROP COLUMN")
}

// The #1625 third fix wave put four tests here — one each for 000154, 000156, 000158 and 000159 —
// and #1625 removed them deliberately. (Their names are not repeated: they no longer resolve, and
// TestEveryCitedTestNameResolves rightly rejects a comment that cites a test which does not
// exist.) Each asserted that the down COMPLETED and destroyed or resurrected the state it was
// seeded with. That was a true observation of an unguarded migration, and all four are now
// guarded, so keeping them would have meant keeping four assertions that the guards do not exist.
// Their coverage moved into downGuardCases above, which asserts both halves — refuses with rows,
// proceeds without them — instead of only the loss. 000149 keeps its own named test because it is
// NOT guarded: it aborts on a re-added CHECK, which is a different mechanism.

// TestDownMigration1625Guards_LockBeforeTheyCount_Integration is #1625.
//
// The four guards added for 000154/000156/000158/000159 are the same shape 000190's was found to
// get wrong this week: a `SELECT COUNT(*)` deciding whether a DROP COLUMN may proceed. A bare
// count takes only ACCESS SHARE, which does not conflict with the ROW EXCLUSIVE a production
// writer takes, so without a lock a concurrent disconnect, sync, or delete can create the very row
// the guard just decided did not exist — and the column is dropped anyway, with the rollback
// reporting success. The failure mode is a SILENTLY BYPASSED refusal, so a test that only checks
// the refusal fires on a quiet database cannot see it.
//
// Only the guard PREFIX is executed, for the same reason 000190's test does it: running the whole
// file takes ACCESS EXCLUSIVE at the ALTER regardless and would prove nothing about the window.
// The prefix is derived from each file's own bytes. The predicate is cleared first so the guard
// PROCEEDS — a guard that raised would abort the transaction and leave nothing to observe.
func TestDownMigration1625Guards_LockBeforeTheyCount_Integration(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()

	for _, tc := range downGuardCases {
		if tc.clearRefusingState == nil {
			continue // only the four #1625 lifecycle-column guards use that hook
		}
		tc := tc
		t.Run(tc.label, func(t *testing.T) {
			down := readDownMigration(t, tc.file)

			endOfGuard := strings.Index(down, "END $$;")
			require.Greater(t, endOfGuard, 0,
				"%s no longer opens with a DO-block guard, so this test cannot locate its prefix "+
					"and is proving nothing", tc.file)
			rest := down[endOfGuard:]
			cut := len(rest)
			for _, marker := range []string{"ALTER TABLE", "DROP INDEX", "CREATE UNIQUE"} {
				if i := strings.Index(rest, marker); i >= 0 && i < cut {
					cut = i
				}
			}
			require.Less(t, cut, len(rest),
				"%s has no schema-changing statement after its guard — the split picked nothing", tc.file)
			guardOnly := down[:endOfGuard+cut]
			require.Contains(t, guardOnly, "SELECT COUNT(*)",
				"%s: the prefix must contain the guard's count, or the split picked the wrong statement", tc.file)

			tx, err := pool.Begin(ctx)
			require.NoError(t, err)
			defer tx.Rollback(ctx) //nolint:errcheck // always rolled back

			_, err = tx.Exec(ctx, `SET LOCAL lock_timeout = '10s'`)
			require.NoError(t, err)
			tc.clearRefusingState(t, ctx, tx)

			_, err = tx.Exec(ctx, guardOnly)
			require.NoError(t, err, "%s's guard must proceed once its predicate holds nothing", tc.file)

			for _, table := range tc.tables {
				var held bool
				require.NoError(t, tx.QueryRow(ctx, `
					SELECT EXISTS(
						SELECT 1 FROM pg_locks
						WHERE relation = $1::regclass
						  AND pid = pg_backend_pid()
						  AND mode = 'AccessExclusiveLock')`, table).Scan(&held))
				assert.Truef(t, held,
					"%s's guard did not hold AccessExclusiveLock on %s while counting. Without it a "+
						"concurrent writer can create the row the guard just decided did not exist, "+
						"between the count and the DROP COLUMN, and the refusal is bypassed silently.",
					tc.file, table)
			}
		})
	}
}
