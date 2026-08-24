//go:build integration

package repository

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
)

// TestNoUnallowlistedCascadeIntoHistoryTable is guard G1 against the LIVE schema (#1569 task 19).
//
// The decision procedure and the allowlist live in history_cascade_guard_test.go, where the
// constructed violations are proven to fail. This test only supplies the real edge set, so that a
// failure here is always a statement about a migration and never about the guard's logic.
//
// Reading pg_constraint rather than parsing backend/migrations/*.sql is deliberate. The schema is
// the accumulation of 166 migrations, several of which drop and re-add the same constraint; the
// FINAL delete action is the only thing that matters, and Postgres is the only thing that knows it
// without re-implementing the migration runner in a regexp.
func TestNoUnallowlistedCascadeIntoHistoryTable(t *testing.T) {
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		t.Skip("DATABASE_URL not set — skipping integration test")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	require.NoError(t, err, "connect to test database")
	defer pool.Close()

	rows, err := pool.Query(ctx, `
		SELECT c.conrelid::regclass::text  AS child,
		       c.confrelid::regclass::text AS parent,
		       c.conname
		FROM pg_constraint c
		WHERE c.contype = 'f' AND c.confdeltype = 'c'`)
	require.NoError(t, err)
	defer rows.Close()

	var edges []fkCascadeEdge
	for rows.Next() {
		var e fkCascadeEdge
		require.NoError(t, rows.Scan(&e.child, &e.parent, &e.constraint))
		edges = append(edges, e)
	}
	require.NoError(t, rows.Err())

	// Fail closed. An empty or unmigrated schema would otherwise pass this test while proving
	// nothing — and would then report every allowlist entry as stale, which at least says so loudly.
	require.NotEmpty(t, edges, "no ON DELETE CASCADE foreign keys found at all — the database is not "+
		"migrated, and a vacuous pass here would read as protection")

	// Sanity: the tables this guard is about must actually exist. Renaming one out from under the
	// guard would otherwise silently empty its scope.
	for tbl := range historyBearingTables {
		var exists bool
		require.NoError(t, pool.QueryRow(ctx,
			`SELECT EXISTS (SELECT 1 FROM information_schema.tables
			 WHERE table_schema = 'public' AND table_name = $1)`, tbl).Scan(&exists))
		require.True(t, exists, "historyBearingTables names %q, which does not exist in the live schema; "+
			"if it was renamed, rename it here too or the guard silently stops covering it", tbl)
	}
	for tbl := range deliberatelyNotHistoryBearing {
		var exists bool
		require.NoError(t, pool.QueryRow(ctx,
			`SELECT EXISTS (SELECT 1 FROM information_schema.tables
			 WHERE table_schema = 'public' AND table_name = $1)`, tbl).Scan(&exists))
		require.True(t, exists, "deliberatelyNotHistoryBearing names %q, which no longer exists", tbl)
	}

	unallowlisted, stale := historyCascadeVerdict(edges, historyBearingTables, allowedHistoryCascades)

	if len(unallowlisted) > 0 {
		t.Errorf("%d ON DELETE CASCADE foreign key(s) into history-bearing tables are not "+
			"allowlisted (#1569).\n"+
			"Each one means some parent delete now DESTROYS retained financial history. If that is "+
			"genuinely intended, add the edge to allowedHistoryCascades with the reason, in the same "+
			"commit as the migration. If it is not, the migration wants ON DELETE SET NULL or "+
			"RESTRICT instead:\n    %s",
			len(unallowlisted), strings.Join(unallowlisted, "\n    "))
	}
	if len(stale) > 0 {
		t.Errorf("%d allowlisted cascade(s) no longer exist in the schema (#1569).\n"+
			"Stale entries make a decision look current when it is not, which is how the next one "+
			"gets justified. Delete them:\n    %s",
			len(stale), strings.Join(stale, "\n    "))
	}
}

// TestEveryHistoryShapedTableIsClassified is the completeness check behind G1 and G2 (#1569 task 19,
// review finding F3).
//
// Both guards work off `historyBearingTables`, a hand-maintained list. A hand-maintained list cannot
// notice a table nobody added to it, and that is not hypothetical: `portfolio_snapshots` — per-day
// total_value, total_cost_basis and total_gain_loss, cascading from `users` — was absent from both
// maps while its own `portfolio_snapshot_repairs` audit table was protected. No live violation
// existed, so nothing was broken; nothing would have been reported either, which is the point.
//
// This test does not decide whether a table is history. It requires that SOMEONE decided, and wrote
// the reason down in one of the two maps.
func TestEveryHistoryShapedTableIsClassified(t *testing.T) {
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		t.Skip("DATABASE_URL not set — skipping integration test")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	require.NoError(t, err, "connect to test database")
	defer pool.Close()

	rows, err := pool.Query(ctx, `
		SELECT table_name FROM information_schema.tables
		WHERE table_schema = 'public' AND table_type = 'BASE TABLE'`)
	require.NoError(t, err)
	defer rows.Close()

	var tables []string
	for rows.Next() {
		var name string
		require.NoError(t, rows.Scan(&name))
		tables = append(tables, name)
	}
	require.NoError(t, rows.Err())
	require.NotEmpty(t, tables, "no tables found — the database is not migrated and this would pass "+
		"vacuously")

	// Fail closed on the FILTER as well as the scan. If the shape regexp stopped matching, every
	// table would be out of scope and the test would pass while checking nothing.
	shaped := 0
	for _, tbl := range tables {
		if historyShapedTableName.MatchString(tbl) {
			shaped++
		}
	}
	require.GreaterOrEqual(t, shaped, 10,
		"only %d of %d live tables matched historyShapedTableName; the shape filter is broken and "+
			"this test is checking nothing", shaped, len(tables))

	if unclassified := unclassifiedHistoryShapedTables(tables, historyBearingTables, deliberatelyNotHistoryBearing); len(unclassified) > 0 {
		t.Errorf("%d live table(s) are named like history but appear in NEITHER historyBearingTables "+
			"NOR deliberatelyNotHistoryBearing (#1569).\n"+
			"G1 and G2 are both blind to them: a new ON DELETE CASCADE into one, or a DELETE FROM one, "+
			"would pass unreported. Decide, and write the reason in whichever map is right — putting "+
			"it in deliberatelyNotHistoryBearing is a perfectly good answer for a marker, a receipt or "+
			"a system log:\n    %s", len(unclassified), strings.Join(unclassified, "\n    "))
	}
}
