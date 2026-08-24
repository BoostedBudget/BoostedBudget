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
