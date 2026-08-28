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

// Issue #1569 Round 12 Task 2.
//
// Migration 000180 turns DeleteManagedForTransaction's hard `DELETE FROM
// savings_goal_contributions` into a tombstone: a Plaid `removed` event on the transaction a
// reconciler-owned contribution was derived from now sets deleted_at/deleted_reason instead of
// destroying the row ("the provider is not the user").
//
// That only works if every read of this table excludes tombstoned rows by default. A single sum,
// list or ownership probe that forgets is worse than not having the column at all: the row would
// still count toward a goal's progress, or a rejected/confirm action would silently succeed against
// a row the product is supposed to treat as gone.
//
// THE RULE is the same mechanism as TestNoConfigReadForgetsTheTombstoneFilter,
// TestNoTransactionReadForgetsTheTombstoneFilter and TestNoCustomCategoryReadForgetsTheTombstoneFilter
// -- it reuses tombstoneReadOffenders/tombstoneWriteOffenders verbatim rather than restating them, so
// a future hardening of the shared mechanism hardens all four guards at once. For every FROM, JOIN
// or USING reference to savings_goal_contributions in non-test Go source under internal/ (a
// DELETE's own FROM excepted -- that is a write), the enclosing statement must carry either a
// predicate `<that reference's alias>.deleted_at IS NULL`, or a written `tombstone-exempt:` reason
// naming it. Every UPDATE/DELETE write TARGET on this table must carry the same, in the separate
// `write:` key space.
//
// THIS TABLE HAS EXACTLY ONE EXEMPTION, AND IT IS DELIBERATE: DeleteContribution
// (savings_contribution_repository.go) is the pre-existing, user-facing, explicit hard delete
// (G2-allowlisted) that this round's brief named out of scope -- "Do NOT change DeleteContribution".
// Its own DELETE target carries a `tombstone-exempt(write:savings_goal_contributions c)` token
// rather than a predicate, because adding one would be exactly the kind of quiet SQL change the
// brief forbade, not because the target is unconsidered.
//
// Every other reference and write target on this table is FILTERED, not exempted: the sums that
// feed goal progress, the lists a user sees, the ownership/confirm/reject probes, and the tombstone
// write itself (idempotency: DeleteManagedForTransaction's own `c.deleted_at IS NULL` stops a
// re-delivered Plaid `removed` event from re-stamping the timestamp).
//
// Mutation-checked: deleting `AND c.deleted_at IS NULL` (or the unaliased form) from any one of the
// filtered queries turns this test RED with that file, line and alias named.
var savingsContributionTombstoneTables = []string{"savings_goal_contributions"}

func TestNoSavingsContributionReadForgetsTheTombstoneFilter(t *testing.T) {
	root := repoInternalRoot(t)

	deleteRef := regexp.MustCompile(
		`(?is)\bDELETE\s+FROM\s+(` + strings.Join(savingsContributionTombstoneTables, "|") + `)\b`)

	var offenders []string
	var filesScanned, refsSeen int

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
		filesScanned++

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
			refsSeen += tombstoneRefsSeen(sql, savingsContributionTombstoneTables, deleteRef)

			bad := tombstoneReadOffenders(sql, savingsContributionTombstoneTables, deleteRef)
			bad = append(bad, tombstoneWriteOffenders(sql, savingsContributionTombstoneTables)...)
			if len(bad) == 0 {
				// Nothing to report here, but a nested unit may still contain a read -- unless this
				// unit already matched a read, in which case descending would double-report it.
				return !tombstoneReadRefRegexp(savingsContributionTombstoneTables).MatchString(
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

	// Floor set at the corpus measured when this guard was written: 11 read/write references across
	// savings_contribution_repository.go. Below this the walk or the reference pattern is broken and
	// a pass here would be vacuous (#1569 whole-branch review I3's lesson, applied up front rather
	// than learned the same way a fourth time).
	requireTombstoneCorpus(t, "TestNoSavingsContributionReadForgetsTheTombstoneFilter", filesScanned, refsSeen, 300, 11, 362, 11)

	if len(offenders) > 0 {
		t.Fatalf(
			"%d read/write reference(s) to %v carry no tombstone predicate OF THEIR OWN (#1569 "+
				"round 12 task 2).\n"+
				"Add `AND <that reference's alias>.deleted_at IS NULL` -- another table's predicate "+
				"in the same statement does NOT count -- or, if the reference must deliberately see "+
				"or leave alone a tombstoned contribution (only DeleteContribution qualifies today), "+
				"an SQL comment naming THIS reference inside the literal:\n"+
				"  -- tombstone-exempt(savings_goal_contributions[ <alias>]): <why>            "+
				"(excuses a READ reference)\n"+
				"  -- tombstone-exempt(write:savings_goal_contributions[ <alias>]): <why>      "+
				"(excuses an UPDATE/DELETE TARGET; the two key spaces are separate)\n%s",
			len(offenders), savingsContributionTombstoneTables, strings.Join(offenders, "\n  "))
	}
}
