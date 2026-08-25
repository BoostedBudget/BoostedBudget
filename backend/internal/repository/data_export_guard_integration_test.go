//go:build integration

package repository_test

import (
	"context"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/maxnussbaum/BoostedBudget/internal/repository"
)

// These guards are the #1587 export's answer to the failure mode that produced #1581: a hand-written
// list of "the tables that matter" which nobody widens when a migration adds one. The retention
// notice went stale that way while three presence assertions stayed green throughout. An export has
// the same shape of exposure — a user-scoped table nobody added is silently unportable, and unlike a
// missing notice phrase, nothing in the product ever looks wrong.
//
// The universe is re-derived from the live database and from DeleteUserCascade's own source on every
// run, exactly as retention_notice_enumeration_integration_test.go does, so the guards cannot be
// satisfied by editing a list.

const deleteUserCascadeSourceForExport = "profile_repository.go"

var (
	sqlLineCommentForExport   = regexp.MustCompile(`--[^\n]*`)
	deleteFromForExport       = regexp.MustCompile(`(?is)\bDELETE\s+FROM\s+([a-z_][a-z0-9_]*)`)
	secretShapedColumnPattern = regexp.MustCompile(`(?i)token|secret|password|clerk|encrypt|cipher|blind_idx|hash`)
)

// userScopedUniverse re-derives every table a user's own erasure destroys: the transitive
// ON DELETE CASCADE closure from `users`, plus the tables DeleteUserCascade deletes from explicitly
// (those have no cascading FK, so the closure cannot see them).
//
// "Destroyed by this user's erasure" is used as the definition of "this user's own data" on purpose.
// It is the same closure the deletion side already trusts, it is derived rather than asserted, and
// the two sides therefore cannot disagree about what the user owns.
func userScopedUniverse(t *testing.T, ctx context.Context, pool *pgxpool.Pool) map[string]string {
	t.Helper()

	rows, err := pool.Query(ctx, `
		WITH RECURSIVE closure AS (
			SELECT 'users'::text AS tbl
			UNION
			SELECT c.conrelid::regclass::text
			FROM pg_constraint c
			JOIN closure cl ON c.confrelid::regclass::text = cl.tbl
			WHERE c.contype = 'f' AND c.confdeltype = 'c'
		)
		SELECT tbl FROM closure`)
	require.NoError(t, err)
	defer rows.Close()

	universe := map[string]string{}
	for rows.Next() {
		var tbl string
		require.NoError(t, rows.Scan(&tbl))
		universe[tbl] = "ON DELETE CASCADE closure from users"
	}
	require.NoError(t, rows.Err())

	// Fail closed. An unmigrated database reports a tiny closure, which would pass every assertion
	// below and read as protection.
	require.Greater(t, len(universe), 30,
		"only %d tables in the CASCADE closure from users — the database is not migrated, and a "+
			"vacuous pass here would read as protection", len(universe))

	for _, tbl := range explicitDeletesInDeleteUserCascadeForExport(t) {
		if _, already := universe[tbl]; !already {
			universe[tbl] = "explicit DELETE FROM in DeleteUserCascade"
		}
	}
	return universe
}

// explicitDeletesInDeleteUserCascadeForExport reads the `DELETE FROM` statements out of
// DeleteUserCascade's own body.
//
// ⚠ Same scope limits as the #1581 guard this is modelled on, repeated so a reader here does not
// over-trust it: only *ast.BasicLit nodes inside that function's OWN body are scanned, so a DELETE
// moved into a helper or assembled with fmt.Sprintf is invisible; `--` stripping is only safe
// because those literals are raw backtick strings; and a schema-qualified name would register as
// the schema.
func explicitDeletesInDeleteUserCascadeForExport(t *testing.T) []string {
	t.Helper()

	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, deleteUserCascadeSourceForExport, nil, 0)
	require.NoError(t, err, "parse %s", deleteUserCascadeSourceForExport)

	var body *ast.BlockStmt
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if ok && fn.Name.Name == "DeleteUserCascade" && fn.Body != nil {
			body = fn.Body
			break
		}
	}
	require.NotNil(t, body,
		"DeleteUserCascade not found in %s — re-point this test rather than deleting it, or the "+
			"explicit-delete half of the universe silently empties", deleteUserCascadeSourceForExport)

	seen := map[string]bool{}
	var tables []string
	ast.Inspect(body, func(n ast.Node) bool {
		lit, ok := n.(*ast.BasicLit)
		if !ok || lit.Kind != token.STRING {
			return true
		}
		for _, m := range deleteFromForExport.FindAllStringSubmatch(sqlLineCommentForExport.ReplaceAllString(lit.Value, ""), -1) {
			tbl := strings.ToLower(m[1])
			if !seen[tbl] {
				seen[tbl] = true
				tables = append(tables, tbl)
			}
		}
		return true
	})
	sort.Strings(tables)
	return tables
}

// TestDataExportClassifiesEveryUserScopedTable is the anti-drift guard for #1587.
//
// A user-scoped table that is neither exported nor deliberately withheld is data the user cannot
// obtain, in a product whose privacy policy promises data portability twice. The whole point of
// deriving the universe rather than listing it is that adding the table is what makes this test go
// green — there is no list to edit that makes the gap disappear without closing it.
func TestDataExportClassifiesEveryUserScopedTable(t *testing.T) {
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		t.Skip("DATABASE_URL not set — skipping integration test")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	require.NoError(t, err, "connect to test database")
	defer pool.Close()

	universe := userScopedUniverse(t, ctx, pool)

	exported := map[string]bool{}
	for _, name := range repository.DataExportTableNames() {
		require.False(t, exported[name], "table %q is listed twice in DataExportTables", name)
		exported[name] = true
	}

	for tbl := range repository.DataExportWithheldTables {
		require.False(t, exported[tbl],
			"table %q is both exported and withheld — one of the two entries is a lie", tbl)
		require.NotEmpty(t, strings.TrimSpace(repository.DataExportWithheldTables[tbl]),
			"withheld table %q has no reason; an unexplained exclusion is a silent portability gap", tbl)
	}

	var unclassified []string
	for tbl, why := range universe {
		if exported[tbl] {
			continue
		}
		if _, withheld := repository.DataExportWithheldTables[tbl]; withheld {
			continue
		}
		unclassified = append(unclassified, tbl+" ("+why+")")
	}
	sort.Strings(unclassified)
	if len(unclassified) > 0 {
		t.Errorf("%d user-scoped table(s) are destroyed by this user's own erasure but appear in "+
			"neither DataExportTables nor DataExportWithheldTables (#1587).\n"+
			"Data the user cannot get out is a GDPR Article 20 gap, and #1569 makes it worse with "+
			"every table it newly retains. Export the table with an ownership predicate rooted at "+
			"the user, or withhold it with a reason:\n    %s",
			len(unclassified), strings.Join(unclassified, "\n    "))
	}
}

// TestDataExportQueriesMatchTheLiveSchema executes every section's statement with LIMIT 0.
//
// pgxmock asserts the SQL string, not its meaning: an ORDER BY naming a column a migration renamed
// is a perfectly good string and a 500 in production. Six of this registry's own ORDER BY clauses
// were wrong when first written, and only running them said so.
func TestDataExportQueriesMatchTheLiveSchema(t *testing.T) {
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		t.Skip("DATABASE_URL not set — skipping integration test")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	require.NoError(t, err, "connect to test database")
	defer pool.Close()

	require.NotEmpty(t, repository.DataExportTables, "the export registry is empty")

	for _, tbl := range repository.DataExportTables {
		t.Run(tbl.Table, func(t *testing.T) {
			require.NotEmpty(t, tbl.OwnershipPredicate, "%s has no ownership predicate", tbl.Table)
			require.NotEmpty(t, tbl.OrderBy, "%s has no ORDER BY, so its export is not stable", tbl.Table)

			// The PRODUCTION builder, not a re-assembled copy of it. Several predicates carry a
			// trailing `--` comment, so how the clauses are joined is itself part of what has to be
			// correct; a test that rebuilt the string could not see a bad join.
			sql := repository.DataExportRowQuery(tbl) + "\nLIMIT 0"

			// ⚠ RUNNING THE STATEMENT IS NOT ENOUGH, and finding that out cost a mutant. Several
			// predicates end in a `-- tombstone-exempt(...)` comment; splice the clauses onto ONE
			// line and the comment swallows ORDER BY and LIMIT — and Postgres accepts the result
			// happily, because an unordered SELECT is perfectly valid SQL. The export would just
			// silently stop being stable. So the clauses that must survive the comment are checked
			// after stripping the comments, exactly as Postgres sees them.
			effective := sqlLineCommentForExport.ReplaceAllString(sql, "")
			require.Contains(t, effective, "ORDER BY "+tbl.OrderBy,
				"the ORDER BY for %s is inside an SQL comment — a `--` in the predicate has "+
					"swallowed the rest of the statement", tbl.Table)
			require.Contains(t, effective, "LIMIT 0",
				"even LIMIT is commented out for %s, so this test is running a full table scan it "+
					"believes is bounded", tbl.Table)

			rows, err := pool.Query(ctx, sql, "00000000-0000-0000-0000-000000000000")
			require.NoError(t, err, "the export statement for %s does not run against the live schema", tbl.Table)
			rows.Close()
			require.NoError(t, rows.Err())
		})
	}
}

// TestDataExportWithholdsEverySecretShapedColumn re-derives the sensitive columns of every exported
// table from information_schema and fails if one of them would reach the user.
//
// The export reads rows with `to_jsonb(t)`, which is what stops the registry drifting behind the
// schema — but it is also what makes a NEWLY ADDED secret column export itself. This guard is the
// other half of that trade: the column list is derived, so a migration adding `plaid_refresh_token`
// fails here on the day it lands rather than the day someone reads an export.
func TestDataExportWithholdsEverySecretShapedColumn(t *testing.T) {
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		t.Skip("DATABASE_URL not set — skipping integration test")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	require.NoError(t, err, "connect to test database")
	defer pool.Close()

	var leaks []string
	for _, tbl := range repository.DataExportTables {
		rows, err := pool.Query(ctx,
			`SELECT column_name FROM information_schema.columns
			 WHERE table_schema = 'public' AND table_name = $1`, tbl.Table)
		require.NoError(t, err)

		var columns []string
		for rows.Next() {
			var col string
			require.NoError(t, rows.Scan(&col))
			columns = append(columns, col)
		}
		rows.Close()
		require.NoError(t, rows.Err())
		require.NotEmpty(t, columns,
			"table %q has no columns in information_schema — it does not exist, so its export "+
				"section is dead", tbl.Table)

		explicit := map[string]bool{}
		for _, col := range tbl.ExcludedColumns {
			explicit[col] = true
		}
		decrypted := map[string]bool{}
		for _, col := range tbl.DecryptedColumns {
			decrypted[col] = true
		}

		for _, col := range columns {
			if !secretShapedColumnPattern.MatchString(col) {
				continue
			}
			// Withheld by the global rule, by this table's own list, or decrypted on the way out
			// (the plaintext is the user's own data — an institution name, their email).
			if repository.IsDataExportColumnGloballyExcluded(col) || explicit[col] || decrypted[col] {
				continue
			}
			leaks = append(leaks, tbl.Table+"."+col)
		}

		for _, col := range tbl.ExcludedColumns {
			require.Contains(t, columns, col,
				"%s excludes column %q, which the table does not have — the exclusion is stale and "+
					"is protecting nothing", tbl.Table, col)
		}
		for _, col := range tbl.DecryptedColumns {
			require.Contains(t, columns, col,
				"%s decrypts column %q, which the table does not have — the decrypt entry is stale",
				tbl.Table, col)
		}
	}

	sort.Strings(leaks)
	if len(leaks) > 0 {
		t.Errorf("%d secret-shaped column(s) would be exported to the user (#1587).\n"+
			"Exporting a credential is worse than not exporting at all: it hands out a live secret "+
			"and, for an at-rest ciphertext, hands out something the user cannot even read. Add each "+
			"to the table's ExcludedColumns (with the reason), to "+
			"dataExportGlobalExcludedColumns, or — if the plaintext genuinely is the user's own "+
			"data — to DecryptedColumns:\n    %s",
			len(leaks), strings.Join(leaks, "\n    "))
	}
}
