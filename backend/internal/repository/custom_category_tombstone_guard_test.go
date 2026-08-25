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

// Issue #1569 Task 9.
//
// Migration 000159 turns the destructive custom-category DELETE into a tombstone. Deleting a
// category used to rewrite every transaction filed under it -- clearing the override, resetting
// category_source to 'plaid', and wiping the user's hand-curated `reviewed` state. Now the row is
// tombstoned and the history is left alone.
//
// That only works if the reads that make up PICKERS, FILTERS and LISTS exclude tombstoned rows.
// A single picker that forgets is worse than not having the column at all: the user deletes a
// category, it keeps appearing in the dropdown, and the delete button appears not to work.
//
// THE RULE is the same mechanism as TestNoConfigReadForgetsTheTombstoneFilter and
// TestNoTransactionReadForgetsTheTombstoneFilter -- it reuses tombstoneReadOffenders verbatim
// rather than restating it, so a future hardening of the mechanism hardens all three guards at
// once. For every FROM, JOIN or USING reference to custom_categories in non-test Go source under
// internal/ (a DELETE's own FROM excepted -- that is a write), the enclosing statement must carry
// either a predicate `<that reference's alias>.deleted_at IS NULL`, or a written
// `tombstone-exempt:` reason.
//
// THIS TABLE HAS MORE EXEMPTIONS THAN THE OTHER TWO, ON PURPOSE, and the split is the whole design:
//
//   - Reads that ANSWER "which categories does this user have?" -- GetByUserID, GetByName,
//     CountByUserID, GetChildren, HasChildren, GetCategoryUsage, GetByID -- are FILTERED. They are
//     the pickers, the tier counter, and the write-time ownership gate.
//   - Reads that ANSWER "what was this historical row filed under?" -- the effective_category_name
//     subqueries in transaction_repository, the category-id expansions in the transaction and
//     analytics filter builders, the budget-category parent linkage join, and
//     GetNamesByIDs -- are EXEMPT. The product decision for #1569 is that a tombstoned category's
//     name still resolves on the transactions that reference it, and that a filter over history
//     still matches them. Filtering those would make a user's own past transactions disappear from
//     their own filtered views, which is the failure this issue exists to end.
//
// Every exemption states which of those two it is. A future edit that "tidies up" an exemption by
// adding the predicate fails the behavioural tests in
// custom_category_tombstone_integration_test.go, not this one -- this guard only makes FORGETTING
// impossible.
//
// Mutation-checked: deleting `AND deleted_at IS NULL` from any one of the filtered queries turns
// this test RED with that file, line and alias named.
var customCategoryTombstoneTables = []string{"custom_categories"}

func TestNoCustomCategoryReadForgetsTheTombstoneFilter(t *testing.T) {
	root := repoInternalRoot(t)

	deleteRef := regexp.MustCompile(
		`(?is)\bDELETE\s+FROM\s+(` + strings.Join(customCategoryTombstoneTables, "|") + `)\b`)

	var offenders []string

	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() && info.Name() == "testdata" {
			// Deliberately-broken samples for other guards; they are not production SQL.
			return filepath.SkipDir
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

			bad := tombstoneReadOffenders(sql, customCategoryTombstoneTables, deleteRef)
			bad = append(bad, tombstoneWriteOffenders(sql, customCategoryTombstoneTables)...)
			if len(bad) == 0 {
				// Nothing to report here, but a nested unit may still contain a read -- unless this
				// unit already matched a read, in which case descending would double-report it.
				return !tombstoneReadRefRegexp(customCategoryTombstoneTables).MatchString(
					deleteRef.ReplaceAllString(sql, ""))
			}
			for _, ref := range bad {
				offenders = append(offenders, rel+":"+strconv.Itoa(line)+" ("+ref+")")
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
			"%d read reference(s) to %v carry no tombstone predicate OF THEIR OWN (#1569).\n"+
				"Add `AND <that reference's alias>.deleted_at IS NULL` -- another table's predicate in\n"+
				"the same statement does NOT count -- or, if the read must deliberately see\n"+
				"tombstoned categories (historical name resolution, history filters), an SQL comment\n"+
				"an SQL comment naming THIS reference inside the literal:\n"+
				"  -- tombstone-exempt(custom_categories[ <alias>]): <why>\n%s",
			len(offenders), customCategoryTombstoneTables, strings.Join(offenders, "\n  "))
	}
}
