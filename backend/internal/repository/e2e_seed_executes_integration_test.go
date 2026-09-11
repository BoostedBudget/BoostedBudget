//go:build integration

package repository

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
)

// #1569 Task 20 (A1). The E2E seed script silently seeded nothing.
//
// apps/web/scripts/seed-e2e-data.sql's second DO block aborted on
//
//	INSERT INTO custom_categories ... ON CONFLICT (user_id, name) DO NOTHING
//
// with "there is no unique or exclusion constraint matching the ON CONFLICT specification",
// because migration 000159 (this issue's task 9) dropped the UNIQUE CONSTRAINT
// unique_user_category_name and recreated it as a PARTIAL unique index of the same name
// (WHERE deleted_at IS NULL). Postgres refuses to infer a partial index from a bare column
// list. The budgets insert further down had the identical defect via 000158's
// unique_budget_name_per_user, so repairing only the first one just moved the abort.
//
// A DO block is one statement, so the abort rolled back all thirteen inserts in it — the seeded
// database ended up holding the user row from the FIRST DO block and nothing else: no linked
// item, no accounts, no transactions, no tags, no categories, no budget. Meanwhile
// e2e-postgres.sh printed "Test data seeded successfully".
//
// Why this test EXECUTES the script instead of pattern-matching it. The failure is a property of
// (script x live schema): neither half is wrong on its own, and the schema half is the
// accumulation of 166 migrations. A regexp over the SQL would have to re-implement Postgres's
// arbiter-inference rule to say anything true, and would go stale the next time a UNIQUE
// constraint anywhere becomes a partial index. Running it is cheaper and cannot drift.
//
// The whole script runs inside a transaction that is ALWAYS rolled back, so nothing is left on
// the shared integration database — which matters, because the script's first act is
// DELETE FROM users WHERE email = 'e2e+clerk_test@boostedbudget.com'.
func TestE2ESeedScriptSeedsEveryTable_Integration(t *testing.T) {
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		t.Skip("DATABASE_URL not set — skipping integration test")
	}

	const seedRel = "../../../apps/web/scripts/seed-e2e-data.sql"
	raw, err := os.ReadFile(filepath.Clean(seedRel))
	require.NoError(t, err, "read the E2E seed script")

	// e2e-postgres.sh's seed_e2e_data() substitutes the Clerk id before handing the file to psql;
	// reproduce that here. The value never leaves the rolled-back transaction.
	script := strings.ReplaceAll(string(raw), "__E2E_CLERK_USER_ID__", "user_guardT20SeedProbe")

	// psql meta-commands are not SQL. Two \set lines at the top are the only ones in the file;
	// fail loudly rather than silently if a third kind ever appears, since dropping an unknown
	// meta-command could change what this test is executing.
	var kept []string
	for _, line := range strings.Split(script, "\n") {
		if strings.HasPrefix(line, `\`) {
			require.True(t, strings.HasPrefix(line, `\set `),
				"unhandled psql meta-command in the seed script, this test would silently skip it: %q", line)
			continue
		}
		kept = append(kept, line)
	}
	script = strings.Join(kept, "\n")

	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	require.NoError(t, err, "connect to test database")
	defer pool.Close()

	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer func() { _ = tx.Rollback(ctx) }() //nolint:errcheck // always rolled back; nothing to salvage

	// This is the assertion that was RED: the abort surfaces here as a pgx error carrying
	// SQLSTATE 42P10, "there is no unique or exclusion constraint matching the ON CONFLICT
	// specification".
	_, err = tx.Exec(ctx, script)
	require.NoError(t, err, "the E2E seed script must execute against the fully migrated schema")

	// Executing without error is necessary but NOT sufficient: a future edit could make the block
	// no-op. Pin the same ten counts the script's own verification SELECT reports, all of which
	// were 0 except the first three while the bug was live.
	const email = "e2e+clerk_test@boostedbudget.com"
	for _, c := range []struct {
		label string
		query string
		min   int
	}{
		{"users", `SELECT COUNT(*) FROM users WHERE email = $1`, 1},
		{"user_identities", `SELECT COUNT(*) FROM user_identities ui JOIN users u ON ui.user_id = u.id WHERE u.email = $1`, 1},
		{"privacy_settings", `SELECT COUNT(*) FROM privacy_settings ps JOIN users u ON ps.user_id = u.id WHERE u.email = $1`, 1},
		{"linked_items", `SELECT COUNT(*) FROM linked_items li JOIN users u ON li.user_id = u.id WHERE u.email = $1`, 1},
		{"accounts", `SELECT COUNT(*) FROM accounts a JOIN users u ON a.user_id = u.id WHERE u.email = $1`, 3},
		{"transactions", `SELECT COUNT(*) FROM transactions t JOIN users u ON t.user_id = u.id WHERE u.email = $1`, 12},
		{"user_tags", `SELECT COUNT(*) FROM user_tags ut JOIN users u ON ut.user_id = u.id WHERE u.email = $1`, 14},
		{"custom_categories", `SELECT COUNT(*) FROM custom_categories cc JOIN users u ON cc.user_id = u.id WHERE u.email = $1`, 3},
		{"budgets", `SELECT COUNT(*) FROM budgets b JOIN users u ON b.user_id = u.id WHERE u.email = $1`, 1},
		{"budget_categories", `SELECT COUNT(*) FROM budget_categories bc JOIN budgets b ON bc.budget_id = b.id JOIN users u ON b.user_id = u.id WHERE u.email = $1`, 3},
		// #1631 added investment fixtures so the Portfolio tab and the investment activity feed
		// have something to render in E2E. This test is named SeedsEveryTable, so they belong in
		// the table — without them the seeder could stop producing them and only a Playwright
		// failure would say so.
		// ⚠ `securities` is NOT user-scoped (it is shared across tenants), so unlike every other
		// row here it cannot join to users directly. Counting it by an `e2e-sec-%` prefix would
		// need a query with no `$1`, which pgx rejects outright because this loop binds `email`
		// to every query. Reaching it through the user's OWN holdings and investment transactions
		// is both bindable and a stronger assertion: it proves the securities are REFERENCED by
		// the fixture rows, not merely present in a shared table.
		{"securities", `SELECT COUNT(DISTINCT sid) FROM (
			SELECT h.security_id AS sid FROM holdings h JOIN users u ON h.user_id = u.id WHERE u.email = $1
			UNION
			SELECT it.security_id FROM investment_transactions it JOIN users u ON it.user_id = u.id
			 WHERE u.email = $1 AND it.security_id IS NOT NULL
		) refs`, 2},
		{"holdings", `SELECT COUNT(*) FROM holdings h JOIN users u ON h.user_id = u.id WHERE u.email = $1`, 1},
		{"investment_transactions", `SELECT COUNT(*) FROM investment_transactions it JOIN users u ON it.user_id = u.id WHERE u.email = $1`, 3},
	} {
		var got int
		require.NoError(t, tx.QueryRow(ctx, c.query, email).Scan(&got), c.label)
		require.GreaterOrEqualf(t, got, c.min,
			"seed produced %d row(s) in %s, expected at least %d — the DO block aborted and rolled back",
			got, c.label, c.min)
	}

	// The seeder is re-run against an already-seeded database on every local E2E invocation, so
	// non-idempotency is a live failure mode, not a hypothetical. Run it a second time inside the
	// same transaction.
	var accountsBefore int
	require.NoError(t, tx.QueryRow(ctx,
		`SELECT COUNT(*) FROM accounts a JOIN users u ON a.user_id = u.id WHERE u.email = $1`, email).Scan(&accountsBefore))

	_, err = tx.Exec(ctx, script)
	require.NoError(t, err, "the E2E seed script must be re-runnable against an already-seeded database")

	var accountsAfter int
	require.NoError(t, tx.QueryRow(ctx,
		`SELECT COUNT(*) FROM accounts a JOIN users u ON a.user_id = u.id WHERE u.email = $1`, email).Scan(&accountsAfter))
	// ⚠ Compare BEFORE against AFTER rather than against a literal. This assertion used to read
	// `require.Equal(t, 3, accounts, ...)`, which conflated two different things: whether the
	// seeder is idempotent (what it is for) and how many accounts the fixture happens to contain
	// (what it accidentally pinned). #1631 added a fourth account — an investment one, so the
	// Portfolio tab has something to render — and the test failed while the seeder was perfectly
	// idempotent. Measuring the delta tests the property and survives the next fixture.
	require.Equal(t, accountsBefore, accountsAfter,
		"re-running the seed must not duplicate accounts: %d before the second run, %d after",
		accountsBefore, accountsAfter)

	require.NoError(t, tx.Rollback(ctx), fmt.Sprintf("roll back the seed probe on the shared %s database", "integration"))
}
