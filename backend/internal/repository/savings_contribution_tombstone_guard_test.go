package repository

import (
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
// THIS TABLE NOW HAS NO EXEMPTION AT ALL (#1569 round 17 task 6). It used to have exactly one:
// DeleteContribution's own DELETE target carried a `tombstone-exempt(write:savings_goal_contributions c)`
// token rather than a predicate, because round 12's brief named that function out of scope. The
// token was doing real damage, not just documenting a deferral -- without `c.deleted_at IS NULL`
// the user-facing hard delete could destroy a contribution DeleteManagedForTransaction had already
// tombstoned and retained, which is precisely the row the user cannot see and therefore cannot have
// named. Round 17 task 6 added the predicate and deleted the token, so this guard now covers every
// reference and every write target on this table with no exception.
//
// Every reference and write target on this table is FILTERED, not exempted: the sums that
// feed goal progress, the lists a user sees, the ownership/confirm/reject probes, and the tombstone
// write itself (idempotency: DeleteManagedForTransaction's own `c.deleted_at IS NULL` stops a
// re-delivered Plaid `removed` event from re-stamping the timestamp).
//
// Mutation-checked: deleting `AND c.deleted_at IS NULL` (or the unaliased form) from any one of the
// filtered queries turns this test RED with that file, line and alias named.
//
// ⚠⚠ NAMED, PERMANENT BLIND SPOT (#1569 round 12 task 5): this guard's AST scan cannot see
// data_export_repository.go's GDPR export, which assembles `FROM savings_goal_contributions` at
// RUNTIME via fmt.Sprintf across two separate Go statements. Full reasoning lives on
// configTombstoneTables' doc comment (config_tombstone_guard_test.go) — this is the same
// mechanism, same blind spot, deliberately left unaddressed because the export's behaviour
// (returning tombstoned rows for Article 15) is correct as-is.
var savingsContributionTombstoneTables = []string{"savings_goal_contributions"}

func TestNoSavingsContributionReadForgetsTheTombstoneFilter(t *testing.T) {
	root := repoInternalRoot(t)

	deleteRef := regexp.MustCompile(
		`(?is)\bDELETE\s+FROM\s+(` + strings.Join(savingsContributionTombstoneTables, "|") + `)\b`)

	var offenders []string
	pfiles := goProductionFiles(t, root, true, false)
	filesScanned := len(pfiles)
	var refsSeen int

	for _, pf := range pfiles {
		for _, u := range literalUnitsIn(pf.file) {
			line := pf.fset.Position(u.pos).Line
			sql := u.sql
			refsSeen += tombstoneRefsSeen(sql, savingsContributionTombstoneTables, deleteRef)

			bad := tombstoneReadOffenders(sql, savingsContributionTombstoneTables, deleteRef)
			bad = append(bad, tombstoneWriteOffenders(sql, savingsContributionTombstoneTables)...)
			for _, ref := range bad {
				offenders = append(offenders, pf.rel+":"+strconv.Itoa(line)+" ("+ref+")")
			}
		}
	}

	// Floor set at the corpus measured when this guard was written: 11 read/write references across
	// savings_contribution_repository.go. Below this the walk or the reference pattern is broken and
	// a pass here would be vacuous (#1569 whole-branch review I3's lesson, applied up front rather
	// than learned the same way a fourth time).
	// #1685/#1686 T29: re-measured live corpus is 460 files / 17 refs (was 374/15), so the old
	// floor's band had widened to 24% — half-vacated per the FLOOR POLICY above. Recalibrated to
	// floor(0.9*live): 414/15, with was*=live. This RAISES the floor; nothing is lowered.
	requireTombstoneCorpus(t, "TestNoSavingsContributionReadForgetsTheTombstoneFilter", filesScanned, refsSeen, 414, 15, 460, 17)

	if len(offenders) > 0 {
		t.Fatalf(
			"%d read/write reference(s) to %v carry no tombstone predicate OF THEIR OWN (#1569 "+
				"round 12 task 2).\n"+
				"Add `AND <that reference's alias>.deleted_at IS NULL` -- another table's predicate "+
				"in the same statement does NOT count -- or, if the reference must deliberately see "+
				"or leave alone a tombstoned contribution (nothing qualifies today -- see the "+
				"doc comment), "+
				"an SQL comment naming THIS reference inside the literal:\n"+
				"  -- tombstone-exempt(savings_goal_contributions[ <alias>]): <why>            "+
				"(excuses a READ reference)\n"+
				"  -- tombstone-exempt(write:savings_goal_contributions[ <alias>]): <why>      "+
				"(excuses an UPDATE/DELETE TARGET; the two key spaces are separate)\n%s",
			len(offenders), savingsContributionTombstoneTables, strings.Join(offenders, "\n  "))
	}
}
