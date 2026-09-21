package repository

import (
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
// THE RULE. Every read REFERENCE to `transactions` in non-test Go source under internal/ — each
// `FROM`/`JOIN`/`USING transactions [alias]`, a DELETE's own FROM excepted, that being a write —
// must carry a tombstone predicate OF ITS OWN: `<its alias>.deleted_at IS NULL`, or an unqualified
// `deleted_at IS NULL` where the reference has no alias. The statement-level escape hatch is the
// token `tombstone-exempt:` followed by a written reason, for the cases where the predicate is
// genuinely supplied elsewhere (a shared WHERE-clause builder) or the query genuinely wants to see
// tombstones.
//
// ⚠ THE "OF ITS OWN" IS A TASK 8 FIX, AND IT MATTERED. Until then this guard asked only whether the
// string "deleted_at IS NULL" appeared ANYWHERE in the statement — which any OTHER table's
// predicate satisfies. Task 8 gave savings_goals/boosts/budgets/budget_categories the same
// treatment, wrote the same loose check, and review found a live site
// (transfer_suggestion_repository.go GetByUserIDStatuses) where an unfiltered `LEFT JOIN boosts r`
// was waved through by the `txn.deleted_at IS NULL` on the transactions join right below it. The
// per-reference form is now shared by both guards via tombstoneReadOffenders
// (config_tombstone_guard_test.go). Re-audited under the strict form, THIS guard reported no new
// offenders — the weakness was latent here, not exploited — but it was the same weakness.
//
// WRITES ARE AUDITED TOO, since #1569 T8. Every UPDATE and DELETE whose TARGET is one of these
// tables must carry the same alias-qualified predicate or a named exemption. A read that forgets
// the filter shows the user a row they deleted; a write that forgets it MUTATES one, which is
// worse. Fourteen live sites were surfaced when the rule was switched on, every one of them
// legitimate and every one now carrying a written reason.
//
// ⚠ KNOWN SCOPE LIMIT: an UNALIASED reference in a comma join list is still invisible.
// `FROM tombstoned s, transactions` matches no FROM/JOIN/USING and is dropped by the comma rule,
// which requires an alias -- because a bare `, transactions` also matches a qualified column
// (`EXCLUDED.x, transactions.y`) and plain English in an email template, and both produced false
// positives when it was tried. The aliased form, which is what real join lists use, IS seen.
//
// This is a syntactic check, not a semantic one — it cannot tell a correct predicate from a
// misplaced one, and it is deliberately cheap enough to never be the reason someone skips it. Its
// job is to make FORGETTING impossible; getting it RIGHT is what the behavioural tests are for.
//
// Mutation-checked: deleting `AND t.deleted_at IS NULL` from any one of the audited queries turns
// this test RED with that file and line named, and so does replacing it with another table's
// qualified predicate.
//
// ⚠⚠ NAMED, PERMANENT BLIND SPOT (#1569 round 12 task 5): this guard's AST scan cannot see
// data_export_repository.go's GDPR export, which assembles `FROM transactions` at RUNTIME via
// fmt.Sprintf across two separate Go statements. Full reasoning lives on configTombstoneTables'
// doc comment (config_tombstone_guard_test.go) — this is the same mechanism, same blind spot,
// deliberately left unaddressed because the export's behaviour (returning tombstoned rows for
// Article 15) is correct as-is.

// tombstoneDeleteRef matches the FROM of a DELETE statement, which is a write and out of scope.
var tombstoneDeleteRef = regexp.MustCompile(`(?is)\bDELETE\s+FROM\s+transactions\b`)

// transactionTombstoneTables is the single-element table list this guard audits. It exists so the
// check can share tombstoneReadOffenders with the four-table config guard
// (config_tombstone_guard_test.go) rather than keeping a second, subtly different copy.
var transactionTombstoneTables = []string{"transactions"}

func TestNoTransactionReadForgetsTheTombstoneFilter(t *testing.T) {
	root := repoInternalRoot(t)

	var offenders []string
	pfiles := goProductionFiles(t, root, true, false)
	filesScanned := len(pfiles)
	var refsSeen int

	// A query is often assembled from several adjacent literals — `A` + helper + `B`, or a
	// fmt.Sprintf whose format string and fragments are separate literals. Checking each literal
	// alone would flag the half that names the table and miss the half that carries the WHERE. So
	// the unit of audit is the argument-aware assembly unit (literalUnitsIn,
	// guard_ast_shared_test.go, #1569 round 14 task 2) and every string literal in its subtree is
	// concatenated before the check. Unrelated queries in the same function — or in different
	// arguments of the same call — stay separate units, which is the point: a countQuery and its
	// paginated sibling each have to carry the predicate themselves, and a help-text argument next
	// to a real query cannot lend it a predicate it lost.
	for _, pf := range pfiles {
		for _, u := range literalUnitsIn(pf.file) {
			line := pf.fset.Position(u.pos).Line
			sql := u.sql
			refsSeen += tombstoneRefsSeen(sql, transactionTombstoneTables, tombstoneDeleteRef)

			bad := tombstoneReadOffenders(sql, transactionTombstoneTables, tombstoneDeleteRef)
			bad = append(bad, tombstoneWriteOffenders(sql, transactionTombstoneTables)...)
			for _, ref := range bad {
				offenders = append(offenders, pf.rel+":"+strconv.Itoa(line)+" ("+ref+")")
			}
		}
	}

	// #1685/#1686 T29: re-measured live corpus is 460 files / 88 refs. The old files floor's band
	// had widened to 27%, so minFiles is recalibrated to floor(0.9*460)=414 and wasFiles=460. The
	// refs floor is LEFT ALONE: live (88) did not grow past the old was (89), and minRefs=80 is
	// already inside the 10% band. No floor is lowered.
	requireTombstoneCorpus(t, "TestNoTransactionReadForgetsTheTombstoneFilter", filesScanned, refsSeen, 414, 80, 460, 89)

	if len(offenders) > 0 {
		t.Fatalf(
			"%d read reference(s) to `transactions` carry no tombstone predicate OF THEIR OWN (#1569).\n"+
				"Add `AND <that reference's alias>.deleted_at IS NULL` — another table's predicate in\n"+
				"the same statement does NOT count — or, if the predicate is genuinely supplied\n"+
				"elsewhere, an SQL comment naming THIS reference inside the literal:\n"+
				"  -- tombstone-exempt(<table>[ <alias>]): <why>            (excuses a READ reference)\n"+
				"  -- tombstone-exempt(write:<table>[ <alias>]): <why>      (excuses an UPDATE/DELETE\n"+
				"     TARGET; the two key spaces are separate, and an offender reported as\n"+
				"     `write:...` is only excused by the `write:` form)\n%s",
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
