package repository

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// Issue #1569 Task 7.
//
// transactions.deleted_at turns a provider removal from a destroyed row into a retained one. That
// only works if EVERY read excludes tombstoned rows by default — a single query that forgets is
// worse than not having the column at all, because it double-counts a settled pending against its
// posted twin and silently resurrects a reversal into a total the user already reconciled.
//
// There are ~70 SQL sites reading `transactions` across this package and the analytics/merchant/
// admin/savings/transfer repositories. Reviewing them by eye is exactly the kind of enumeration
// that goes stale the first time someone adds a query, so it is enforced mechanically here instead.
//
// THE RULE. Any STATEMENT in non-test Go source under internal/ whose string literals, taken
// together, contain `FROM transactions` or `JOIN transactions` (a DELETE's own FROM excepted — that
// is a write) must also contain either:
//
//   - the predicate `deleted_at IS NULL`, or
//   - the token `tombstone-exempt:` followed by a written reason, for the cases where the predicate
//     is genuinely supplied elsewhere (a shared WHERE-clause builder) or the query genuinely wants
//     to see tombstones.
//
// This is a syntactic check, not a semantic one — it cannot tell a correct predicate from a
// misplaced one, and it is deliberately cheap enough to never be the reason someone skips it. Its
// job is to make FORGETTING impossible; getting it RIGHT is what the behavioural tests are for.
//
// Mutation-checked: deleting `AND t.deleted_at IS NULL` from any one of the audited queries turns
// this test RED with that file and line named.

// tombstoneTableRef matches a read reference to the transactions table. `\b` after the name keeps
// `transactions_backup`-style names from matching; the (?i) is defensive, this codebase writes SQL
// keywords in upper case throughout.
var tombstoneTableRef = regexp.MustCompile(`(?is)\b(FROM|JOIN)\s+transactions\b`)

// tombstoneDeleteRef matches the FROM of a DELETE statement, which is a write and out of scope.
var tombstoneDeleteRef = regexp.MustCompile(`(?is)\bDELETE\s+FROM\s+transactions\b`)

func TestNoTransactionReadForgetsTheTombstoneFilter(t *testing.T) {
	root := repoInternalRoot(t)

	var offenders []string

	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}

		fset := token.NewFileSet()
		file, parseErr := parser.ParseFile(fset, path, nil, 0)
		if parseErr != nil {
			return parseErr
		}
		rel, _ := filepath.Rel(root, path)

		// A query is often assembled from several adjacent literals — `A` + helper + `B`, or a
		// fmt.Sprintf whose format string and fragments are separate literals. Checking each
		// literal alone would flag the half that names the table and miss the half that carries
		// the WHERE. So the unit of audit is the STATEMENT (or the const/var spec), and every
		// string literal in its subtree is concatenated before the check. Unrelated queries in
		// the same function stay separate units, which is the point: a countQuery and its
		// paginated sibling each have to carry the predicate themselves.
		ast.Inspect(file, func(n ast.Node) bool {
			var unit ast.Node
			switch n.(type) {
			case *ast.AssignStmt, *ast.ValueSpec, *ast.ReturnStmt, *ast.ExprStmt:
				unit = n
			default:
				return true
			}

			var sqlParts []string
			line := fset.Position(unit.Pos()).Line
			ast.Inspect(unit, func(m ast.Node) bool {
				lit, ok := m.(*ast.BasicLit)
				if !ok || lit.Kind != token.STRING {
					return true
				}
				if s, e := strconv.Unquote(lit.Value); e == nil {
					sqlParts = append(sqlParts, s)
				}
				return true
			})
			sql := strings.Join(sqlParts, "\n")

			// A DELETE's own FROM is a write, not a read.
			read := tombstoneDeleteRef.ReplaceAllString(sql, "")
			if !tombstoneTableRef.MatchString(read) {
				return true // keep descending: a nested unit may still contain a read
			}
			if !strings.Contains(sql, "deleted_at IS NULL") && !strings.Contains(sql, "tombstone-exempt:") {
				offenders = append(offenders, rel+":"+strconv.Itoa(line))
			}
			// Do not descend: the enclosing unit already accounts for every literal inside it,
			// so descending would report the same query again from a nested statement.
			return false
		})
		return nil
	})
	if err != nil {
		t.Fatalf("walking internal/: %v", err)
	}

	if len(offenders) > 0 {
		t.Fatalf(
			"%d SQL statement(s) read `transactions` without excluding tombstoned rows (#1569).\n"+
				"Add `AND <alias>.deleted_at IS NULL` to the query, or, if the predicate is genuinely\n"+
				"supplied elsewhere, an SQL comment `-- tombstone-exempt: <why>` inside the literal:\n  %s",
			len(offenders), strings.Join(offenders, "\n  "))
	}
}

// repoInternalRoot resolves backend/internal from this package's directory, so the test does not
// depend on the working directory `go test` happens to choose.
func repoInternalRoot(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd() // .../backend/internal/repository
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	root := filepath.Dir(wd)
	if filepath.Base(root) != "internal" {
		t.Fatalf("expected to run from backend/internal/repository, got %s", wd)
	}
	return root
}
