package repository

import (
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
//
// ⚠⚠ NAMED, PERMANENT BLIND SPOT (#1569 round 12 task 5): this guard's AST scan cannot see
// data_export_repository.go's GDPR export, which assembles `FROM custom_categories` at RUNTIME
// via fmt.Sprintf across two separate Go statements. Full reasoning lives on configTombstoneTables'
// doc comment (config_tombstone_guard_test.go) — this is the same mechanism, same blind spot,
// deliberately left unaddressed because the export's behaviour (returning tombstoned rows for
// Article 15) is correct as-is.
var customCategoryTombstoneTables = []string{"custom_categories"}

func TestNoCustomCategoryReadForgetsTheTombstoneFilter(t *testing.T) {
	root := repoInternalRoot(t)

	deleteRef := regexp.MustCompile(
		`(?is)\bDELETE\s+FROM\s+(` + strings.Join(customCategoryTombstoneTables, "|") + `)\b`)

	var offenders []string
	pfiles := goProductionFiles(t, root, true, false)
	filesScanned := len(pfiles)
	var refsSeen int

	for _, pf := range pfiles {
		for _, u := range literalUnitsIn(pf.file) {
			line := pf.fset.Position(u.pos).Line
			sql := u.sql
			refsSeen += tombstoneRefsSeen(sql, customCategoryTombstoneTables, deleteRef)

			bad := tombstoneReadOffenders(sql, customCategoryTombstoneTables, deleteRef)
			bad = append(bad, tombstoneWriteOffenders(sql, customCategoryTombstoneTables)...)
			for _, ref := range bad {
				offenders = append(offenders, pf.rel+":"+strconv.Itoa(line)+" ("+ref+")")
			}
		}
	}

	// #1685/#1686 T29: re-measured live corpus is 460 files / 51 refs (was 374/29), so the old
	// floor's band had widened to 49% — half-vacated per the FLOOR POLICY above. Recalibrated to
	// floor(0.9*live): 414/45, with was*=live. This RAISES the floor; nothing is lowered.
	requireTombstoneCorpus(t, "TestNoCustomCategoryReadForgetsTheTombstoneFilter", filesScanned, refsSeen, 414, 45, 460, 51)

	if len(offenders) > 0 {
		t.Fatalf(
			"%d read reference(s) to %v carry no tombstone predicate OF THEIR OWN (#1569).\n"+
				"Add `AND <that reference's alias>.deleted_at IS NULL` -- another table's predicate in\n"+
				"the same statement does NOT count -- or, if the read must deliberately see\n"+
				"tombstoned categories (historical name resolution, history filters), an SQL comment\n"+
				"an SQL comment naming THIS reference inside the literal:\n"+
				"  -- tombstone-exempt(custom_categories[ <alias>]): <why>            (excuses a READ reference)\n"+
				"  -- tombstone-exempt(write:custom_categories[ <alias>]): <why>      (excuses an UPDATE/DELETE\n"+
				"     TARGET; the two key spaces are separate, and an offender reported as\n"+
				"     `write:...` is only excused by the `write:` form)\n%s",
			len(offenders), customCategoryTombstoneTables, strings.Join(offenders, "\n  "))
	}
}
