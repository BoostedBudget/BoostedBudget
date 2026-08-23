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
// THE RULE — the same mechanism as TestNoTransactionReadForgetsTheTombstoneFilter, which is this
// guard for transactions. The check is PER REFERENCE, not per statement. For every FROM, JOIN or
// USING reference to one of the four tables in non-test Go source under internal/ (a DELETE's own
// FROM excepted — that is a write), the enclosing statement must contain either:
//
//   - a predicate `<alias>.deleted_at IS NULL` qualified to THAT reference's alias, or
//   - the token `tombstone-exempt:` followed by a written reason.
//
// Per-reference is load-bearing, and this is not hypothetical. The original form asked only whether
// the statement contained the substring `deleted_at IS NULL` anywhere. That let
// transfer_suggestion_repository.go's GetByUserIDStatuses join `boosts` with NO predicate and still
// pass, because an adjacent `transactions` join — added by a different task — carried the string.
// A live read was returning tombstoned boosts while this guard was green. The transaction guard had
// the identical weakness, latent: a mutation that strips the transactions predicate while adding an
// accounts one passes the old form and fails this one.
//
// Still syntactic, not semantic: it cannot tell a correct predicate from one in the wrong clause,
// and `tombstone-exempt:` is statement-scoped, so one comment excuses every reference in that
// statement (no statement today joins two audited tables). Its job is to make FORGETTING
// impossible; getting it RIGHT is what the behavioural tests are for.
//
// Mutation-checked: deleting `AND <alias>.deleted_at IS NULL` from any one of the audited queries
// turns this test RED with that file, line and alias named.
var configTombstoneTables = []string{
	"savings_goals",
	"boosts",
	"budgets",
	"budget_categories",
}

// sqlKeywordsAfterTableName are the tokens that can legally follow a table name in this codebase's
// SQL and are NOT an alias. Without this list `FROM boosts WHERE ...` would be read as "table
// boosts aliased WHERE", and the guard would then demand `WHERE.deleted_at IS NULL`.
var sqlKeywordsAfterTableName = map[string]bool{
	"as": true, "on": true, "where": true, "set": true, "group": true, "order": true,
	"left": true, "right": true, "inner": true, "outer": true, "full": true, "cross": true,
	"join": true, "using": true, "limit": true, "offset": true, "having": true, "union": true,
	"returning": true, "values": true, "and": true, "or": true, "with": true, "for": true,
	"natural": true, "lateral": true, "window": true, "except": true, "intersect": true,
}

// tombstoneReadRef finds one read reference to a tombstoned table and captures its alias, if any.
// FROM and JOIN are the obvious two; USING is here because `DELETE FROM child c USING parent p` is
// a read of `parent` that the FROM/JOIN pair misses entirely (savings_contribution_repository.go's
// DeleteContribution is exactly that shape).
func tombstoneReadRefRegexp(tables []string) *regexp.Regexp {
	return regexp.MustCompile(
		`(?is)\b(?:FROM|JOIN|USING)\s+(` + strings.Join(tables, "|") +
			`)\b(?:\s+(?:AS\s+)?([A-Za-z_][A-Za-z0-9_]*))?`)
}

// tombstoneUnqualifiedPredicate matches `deleted_at IS NULL` that is NOT qualified by any alias —
// the form an unaliased single-table read uses. The leading `[^.\w]` guard is the entire point:
// without it, `txn.deleted_at IS NULL` would satisfy a check meant for a different table.
var tombstoneUnqualifiedPredicate = regexp.MustCompile(`(?is)(^|[^.\w])deleted_at\s+IS\s+NULL`)

// tombstoneReadOffenders returns, for one SQL statement, the read references to `tables` that carry
// no tombstone predicate OF THEIR OWN.
//
// THIS IS PER-REFERENCE AND ALIAS-QUALIFIED, and that is not a refinement — it is the whole
// guarantee. An earlier version of both this guard and task 7's asked only whether the string
// "deleted_at IS NULL" appeared ANYWHERE in the statement. That is satisfied by any other table's
// predicate, so a statement that joins `boosts` unfiltered alongside
// `JOIN transactions txn ... AND txn.deleted_at IS NULL` passed while being exactly the bug the
// guard exists to catch. It was not hypothetical: transfer_suggestion_repository.go's
// GetByUserIDStatuses was live on this branch, green under the loose guard, and RED under this one.
//
// exemptToken (`tombstone-exempt:`) still short-circuits the whole statement, because the cases it
// covers — a predicate supplied by a shared WHERE-clause builder, or a query that genuinely wants
// to see tombstones — are statement-level facts, not per-reference ones.
func tombstoneReadOffenders(sql string, tables []string, deleteRef *regexp.Regexp) []string {
	if strings.Contains(sql, "tombstone-exempt:") {
		return nil
	}
	// A DELETE's own FROM is a write, not a read.
	read := deleteRef.ReplaceAllString(sql, "")

	var offenders []string
	for _, m := range tombstoneReadRefRegexp(tables).FindAllStringSubmatch(read, -1) {
		table, alias := m[1], m[2]
		if sqlKeywordsAfterTableName[strings.ToLower(alias)] {
			alias = ""
		}
		var ok bool
		if alias != "" {
			ok = regexp.MustCompile(`(?is)\b` + regexp.QuoteMeta(alias) + `\.deleted_at\s+IS\s+NULL`).MatchString(sql)
		} else {
			ok = tombstoneUnqualifiedPredicate.MatchString(sql) ||
				regexp.MustCompile(`(?is)\b`+regexp.QuoteMeta(table)+`\.deleted_at\s+IS\s+NULL`).MatchString(sql)
		}
		if !ok {
			ref := table
			if alias != "" {
				ref = table + " " + alias
			}
			offenders = append(offenders, ref)
		}
	}
	return offenders
}

func TestNoConfigReadForgetsTheTombstoneFilter(t *testing.T) {
	root := repoInternalRoot(t)

	// `\b` after the name stops `budgets` from matching inside `budget_categories` and
	// `savings_goals` from matching `savings_goal_contributions`.
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

			bad := tombstoneReadOffenders(sql, configTombstoneTables, deleteRef)
			if len(bad) == 0 {
				// Nothing to report here, but a nested unit may still contain a read — unless this
				// unit already matched a read, in which case descending would double-report it.
				return !tombstoneReadRefRegexp(configTombstoneTables).MatchString(
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
				"Add `AND <that reference's alias>.deleted_at IS NULL` — another table's predicate in\n"+
				"the same statement does NOT count — or, if the predicate is genuinely supplied\n"+
				"elsewhere, an SQL comment `-- tombstone-exempt: <why>` inside the literal:\n  %s",
			len(offenders), configTombstoneTables, strings.Join(offenders, "\n  "))
	}
}
