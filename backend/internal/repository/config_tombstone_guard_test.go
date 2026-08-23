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

// Issue #1569 Task 8.
//
// Migration 000158 turns four destructive config DELETEs into tombstones: savings_goals, boosts,
// budgets and budget_categories are now soft-deleted so that savings_goal_contributions,
// boost_executions, rollover_history and budget_period_events survive the delete instead of being
// cascaded away.
//
// That only works if EVERY read excludes tombstoned rows by default. A single query that forgets
// is worse than not having the column at all: the user deletes a goal, the UI keeps showing it,
// and the delete button appears not to work. There are ~60 SQL sites reading these four tables
// across the repository, database and analytics packages, and reviewing them by eye is exactly the
// enumeration that goes stale the first time someone adds a query.
//
// THE RULE — deliberately identical to TestNoTransactionReadForgetsTheTombstoneFilter, which is the
// same guard for transactions. Any STATEMENT in non-test Go source under internal/ whose string
// literals, taken together, contain `FROM <table>` or `JOIN <table>` for one of the four (a
// DELETE's own FROM excepted — that is a write) must also contain either:
//
//   - the predicate `deleted_at IS NULL`, or
//   - the token `tombstone-exempt:` followed by a written reason.
//
// Syntactic, not semantic: it cannot tell a correct predicate from a misplaced one. Its job is to
// make FORGETTING impossible; getting it RIGHT is what the behavioural tests are for.
//
// Mutation-checked: deleting `AND deleted_at IS NULL` from any one of the audited queries turns
// this test RED with that file and line named.
var configTombstoneTables = []string{
	"savings_goals",
	"boosts",
	"budgets",
	"budget_categories",
}

func TestNoConfigReadForgetsTheTombstoneFilter(t *testing.T) {
	root := repoInternalRoot(t)

	// `\b` after the name stops `budgets` from matching inside `budget_categories` and
	// `savings_goals` from matching `savings_goal_contributions`; the alternation is ordered
	// longest-first for the same reason.
	readRef := regexp.MustCompile(`(?is)\b(FROM|JOIN)\s+(` + strings.Join(configTombstoneTables, "|") + `)\b`)
	deleteRef := regexp.MustCompile(`(?is)\bDELETE\s+FROM\s+(` + strings.Join(configTombstoneTables, "|") + `)\b`)

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

		ast.Inspect(file, func(n ast.Node) bool {
			switch n.(type) {
			case *ast.AssignStmt, *ast.ValueSpec, *ast.ReturnStmt, *ast.ExprStmt:
			default:
				return true
			}
			unit := n

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

			read := deleteRef.ReplaceAllString(sql, "")
			if !readRef.MatchString(read) {
				return true // keep descending: a nested unit may still contain a read
			}
			if !strings.Contains(sql, "deleted_at IS NULL") && !strings.Contains(sql, "tombstone-exempt:") {
				offenders = append(offenders, rel+":"+strconv.Itoa(line))
			}
			return false
		})
		return nil
	})
	if err != nil {
		t.Fatalf("walking internal/: %v", err)
	}

	if len(offenders) > 0 {
		t.Fatalf(
			"%d SQL statement(s) read one of %v without excluding tombstoned rows (#1569).\n"+
				"Add `AND <alias>.deleted_at IS NULL` to the query, or, if the predicate is genuinely\n"+
				"supplied elsewhere, an SQL comment `-- tombstone-exempt: <why>` inside the literal:\n  %s",
			len(offenders), configTombstoneTables, strings.Join(offenders, "\n  "))
	}
}
