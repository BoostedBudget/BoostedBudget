//go:build integration

package jobs

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
)

// deleteUserCascadeSourcePath is DeleteUserCascade's file, relative to this package directory.
// The explicit-DELETE half of the destruction set is read out of the source rather than listed
// here, for the same reason the cascade half is read out of pg_constraint: a hand-copied list of
// what a delete destroys is exactly the artefact that went stale and produced #1581.
const deleteUserCascadeSourcePath = "../../repository/profile_repository.go"

// sqlLineComment strips `-- ...` spans out of an SQL literal before it is scanned for DELETE
// statements. DeleteUserCascade's big literals explain themselves in SQL comments, several of
// which say the words "DELETE FROM users"; a comment about a delete is not a delete.
var sqlLineComment = regexp.MustCompile(`--[^\n]*`)

var deleteFromStatement = regexp.MustCompile(`(?is)\bDELETE\s+FROM\s+([a-z_][a-z0-9_]*)`)

// TestRetentionNoticeEnumerationCoversEverythingDeleted is the #1581 guard.
//
// The retention sweep is the ONE automatic destroyer
// docs/decisions/2026-08-24-financial-history-retained-by-default.md Decision 3 permits, and it is
// permitted SPECIFICALLY because the 30-day and 7-day notices tell the user what it destroys. That
// makes the enumeration load-bearing rather than cosmetic: when the schema grows a new table that
// `DELETE FROM users` destroys, and nobody widens the copy, the notices stop being a published
// retention policy and the permission quietly expires. #1569 did exactly that at scale — it newly
// retained holdings, holding snapshots, liability statements and APR schedules, hand-logged savings
// contributions, boost and rule execution trails, portfolio snapshots and account status history
// past the events that used to destroy them, which made this sweep the first thing that destroys
// them, while the copy still named three categories.
//
// The two pre-existing tests (TestRetentionCleanupCheck_30dNotice_DisclosesWhatGetsDeleted and its
// 7-day sibling) assert that three phrases are PRESENT. Presence cannot see drift: they were green
// throughout. This test compares the enumeration against the destruction set re-derived from the
// live database and from DeleteUserCascade's own source, so a table that nothing claims is a
// failure, and a claim about a table that no longer exists is also a failure.
func TestRetentionNoticeEnumerationCoversEverythingDeleted(t *testing.T) {
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		t.Skip("DATABASE_URL not set — skipping integration test")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	require.NoError(t, err, "connect to test database")
	defer pool.Close()

	// The transitive ON DELETE CASCADE closure from `users`. Postgres is the only thing that knows
	// the FINAL delete action after ~170 migrations, several of which drop and re-add the same
	// constraint.
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

	destroyed := map[string]string{} // table -> how we know it is destroyed
	for rows.Next() {
		var tbl string
		require.NoError(t, rows.Scan(&tbl))
		destroyed[tbl] = "ON DELETE CASCADE closure from users"
	}
	require.NoError(t, rows.Err())

	// Fail closed. An unmigrated database would otherwise report a tiny closure, pass, and read as
	// protection — and would then report most of the enumeration as stale, which at least says so.
	require.Greater(t, len(destroyed), 30,
		"only %d tables in the CASCADE closure from users — the database is not migrated, and a "+
			"vacuous pass here would read as protection", len(destroyed))

	for _, tbl := range explicitDeletesInDeleteUserCascade(t) {
		if _, already := destroyed[tbl]; !already {
			destroyed[tbl] = "explicit DELETE FROM in DeleteUserCascade"
		}
	}

	claimed := map[string]int{} // table -> index of the item claiming it
	for i, item := range retentionDeletionItems {
		require.NotEmpty(t, strings.TrimSpace(item.phrase), "retentionDeletionItems[%d] has no phrase", i)
		require.NotEmpty(t, item.tables,
			"retentionDeletionItems[%d] (%q) claims no tables — a phrase that covers nothing cannot "+
				"keep the enumeration honest", i, item.phrase)
		for _, tbl := range item.tables {
			if prev, dup := claimed[tbl]; dup {
				t.Errorf("table %q is claimed twice: by %q and by %q — the user is told about it "+
					"in two different words", tbl, retentionDeletionItems[prev].phrase, item.phrase)
			}
			claimed[tbl] = i
		}
	}

	var unenumerated []string
	for tbl, why := range destroyed {
		if _, ok := claimed[tbl]; !ok {
			unenumerated = append(unenumerated, tbl+" ("+why+")")
		}
	}
	sort.Strings(unenumerated)
	if len(unenumerated) > 0 {
		t.Errorf("%d table(s) are destroyed by DeleteUserCascade but no retention-notice phrase "+
			"claims them (#1581).\n"+
			"The retention sweep is the only AUTOMATIC destroyer the retention ADR permits, and it "+
			"is permitted because the 30-day and 7-day notices enumerate what it destroys. Every "+
			"table below is destroyed without the user being told. Add it to an existing "+
			"retentionDeletionItem's tables, or add an item with wording that covers it:\n    %s",
			len(unenumerated), strings.Join(unenumerated, "\n    "))
	}

	var stale []string
	for tbl := range claimed {
		if _, ok := destroyed[tbl]; !ok {
			stale = append(stale, tbl)
		}
	}
	sort.Strings(stale)
	if len(stale) > 0 {
		t.Errorf("%d table(s) named in retentionDeletionItems are not destroyed by "+
			"DeleteUserCascade at all (#1581).\n"+
			"Either the table was renamed or dropped — in which case the phrase may now be telling "+
			"the user about nothing — or its FK stopped cascading, in which case the sweep leaks "+
			"it. Re-derive before editing:\n    %s",
			len(stale), strings.Join(stale, "\n    "))
	}

	// Backstop: the rendered bodies are what actually reaches the user. Rendering them from
	// retentionDeletionItems makes this near-tautological TODAY, which is the point — it is here so
	// that hand-writing either body back into place cannot silently detach the copy from the
	// coverage the rest of this test checks.
	for _, body := range []struct{ name, text string }{
		{"30-day notice", retentionNotice30dBody},
		{"7-day notice", retentionNotice7dBody},
	} {
		for _, item := range retentionDeletionItems {
			require.Contains(t, body.text, item.phrase,
				"the %s does not contain the phrase %q, so the coverage this test verifies is not "+
					"the coverage the user is sent", body.name, item.phrase)
		}
	}
}

// explicitDeletesInDeleteUserCascade returns every table named by a `DELETE FROM` inside
// DeleteUserCascade's own body. Parsing the function rather than listing its statements is what
// makes a newly added `DELETE FROM some_table` in that method show up here as an unenumerated
// destruction instead of passing unnoticed.
//
// ⚠ Scope, so a future reader does not over-trust it. None of these is reachable today (each was
// verified absent), and each would make a real destruction invisible to this guard:
//   - only `*ast.BasicLit` nodes inside DeleteUserCascade's OWN body are scanned, so a DELETE moved
//     into a helper, assembled with fmt.Sprintf, or held in a package-level const is not seen;
//   - stripping `--` spans is safe only because these literals are raw backtick strings, where a
//     `--` can only be an SQL comment;
//   - the table name is taken verbatim, so `DELETE FROM public.audit_logs` would register as the
//     table `public` and report `audit_logs` as unenumerated while claiming a table that does not
//     exist. Schema-qualify nothing, or teach this to split on the dot.
func explicitDeletesInDeleteUserCascade(t *testing.T) []string {
	t.Helper()

	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, deleteUserCascadeSourcePath, nil, 0)
	require.NoError(t, err, "parse %s", deleteUserCascadeSourcePath)

	var body *ast.BlockStmt
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if ok && fn.Name.Name == "DeleteUserCascade" && fn.Body != nil {
			body = fn.Body
			break
		}
	}
	require.NotNil(t, body,
		"DeleteUserCascade not found in %s — if it moved or was renamed, re-point this test rather "+
			"than deleting it, or the explicit-delete half of the destruction set silently empties",
		deleteUserCascadeSourcePath)

	seen := map[string]bool{}
	var tables []string
	ast.Inspect(body, func(n ast.Node) bool {
		lit, ok := n.(*ast.BasicLit)
		if !ok || lit.Kind != token.STRING {
			return true
		}
		for _, m := range deleteFromStatement.FindAllStringSubmatch(sqlLineComment.ReplaceAllString(lit.Value, ""), -1) {
			if tbl := strings.ToLower(m[1]); !seen[tbl] {
				seen[tbl] = true
				tables = append(tables, tbl)
			}
		}
		return true
	})

	require.NotEmpty(t, tables,
		"no DELETE FROM statements found inside DeleteUserCascade — the scan broke, and an empty "+
			"explicit-delete set would silently shrink what this test requires the notices to name")
	return tables
}
