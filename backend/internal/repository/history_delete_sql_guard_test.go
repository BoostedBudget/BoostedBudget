package repository

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// Issue #1569 Task 7.5 — Guard G4: the hard-delete paths G2 cannot see.
//
// G2 (history_delete_guard_test.go) parses non-test **Go** under backend/internal. That is the
// whole of its universe, and three delete paths live outside it:
//
//  1. an UP migration that hard-deletes a history table — it runs in every environment, once, and
//     nobody reviews it again afterwards;
//  2. a DOWN migration that does, which is the same statement with "and then it gets rolled back"
//     in front of it;
//  3. shell tooling that talks to the database directly, which no Go scanner will ever see.
//
// ⚠ WHAT THIS GUARD CLAIMS — read this before adding a rule to it.
//
// For an UP migration and for shell tooling the claim is the same as G2's: a hard delete of a
// history-bearing table must be on a list, with a reason a reviewer can evaluate.
//
// For a DOWN migration the claim is DELIBERATELY WEAKER, and this is the part that decides whether
// the guard survives contact with the next engineer. **A down migration that deletes what its own
// up migration inserted is a legitimate reversal, not a violation.** A rule that flagged all of
// them would fire on six correct files today and be switched off within a week. So the rule for a
// down migration is only this:
//
//	a down migration may delete from a history table, but it must SAY WHICH ROWS.
//
// i.e. the statement must carry a `WHERE`. An unqualified `DELETE FROM account_status_history;`
// cannot be a reversal of anything: it also destroys every row written by live traffic in the
// interval since the up migration ran, which by definition the up migration did not create. That
// distinction is cheap to check and is the whole of the down-migration rule.
//
// ⚠ WHAT THIS GUARD DOES NOT CLAIM.
//
//   - It does NOT verify that a qualified down-migration delete really only removes rows its up
//     migration inserted. The check is that the author was made to write a predicate, not that the
//     predicate is right: `WHERE a.id <> b.id OR true` passes, and so does any other clause that is
//     wide in meaning while being narrow in syntax. The ONE syntactic escape that is now refused is
//     a predicate that constrains nothing AT ALL — a bare `WHERE TRUE` or `WHERE 1=1`, which is the
//     shape someone reaches for to silence this guard rather than to reverse a migration
//     (isQualifiedPredicate, #1569 T7-FIX finding E). Anything with a real column in it is accepted
//     unread.
//   - Its allowlists DO carry the predicate the entry was blessed for, since #1569 T7-FIX finding
//     A, exactly as G2's do. What that claims and does not claim is G2's paragraph verbatim: every
//     fragment must still appear, as text, in every statement under the key. It cannot tell you the
//     predicate still MEANS what it meant — `WHERE user_id = '$user_id'` can acquire an `OR` beside
//     it — only that the leg has not silently disappeared.
//   - It does NOT read migrations for anything else — a `DROP TABLE` in a down migration is out of
//     scope here and is G1's business (the schema half) plus review.
//   - Its universe is `backend/migrations`, `backend/scripts` and `apps/web/scripts`. The last of
//     those was excluded for one round because another agent owned the directory, and the exclusion
//     said the file "runs only against the local e2e database created by the e2e harness". That
//     reason did not survive re-derivation (#1569 task 7.9): nothing in seed-e2e-data.sql or in
//     e2e-postgres.sh constrains which database it is pointed at — its own header documents
//     `psql $DATABASE_URL -f seed-e2e-data.sql`. What actually makes it safe is the scoping of its
//     predicates, which is now recorded per delete in allowedFrontendFixtureHistoryDeletes. The
//     rule there is backend/scripts': allowlisted or it fails.
//   - It says nothing about `UPDATE`. Tombstoning is the normal path and is not a delete.

// nonGoDeleteAllowance is G4's blessing, and it is deliberately the same shape as G2's
// historyDeleteAllowance: WHY this file may hard-delete this history table, and WHICH PREDICATE the
// blessing is scoped to.
//
// ⚠ WHY THE PREDICATE IS PART OF THE ENTRY (#1569 T7-FIX, finding A). Until this change a G4 entry
// was keyed by `file|table` and accepted EVERY matching statement, with no predicate at all. So
// changing cleanup.sh from `DELETE FROM transactions WHERE user_id = '$user_id';` to
// `DELETE FROM transactions;` kept the existing key and kept G4 green — the exact hole that the
// same round had just closed in G2, left open in the guard next door. The two guards now enforce
// the same discipline.
//
// ⚠ AND THE ONE ENTRY THAT CANNOT CARRY ONE. 000152's down migration is unqualified BY DESIGN (its
// own reason explains why), so a required-non-empty predicate would be unsatisfiable for it. Rather
// than let it name a vacuous fragment — which would read like a scope and be none —
// `unqualifiedByDesign` says out loud that this entry blesses a statement with no WHERE clause. It
// is checked to be mutually exclusive with `predicate`, so an entry cannot quietly be both.
type nonGoDeleteAllowance struct {
	// reason is why this destruction is allowed at all. Intent, not SQL: it is never matched
	// against the statement.
	reason string
	// predicate is the set of SQL fragments this blessing is scoped to. Every fragment must appear
	// in EVERY statement the key matches. Required unless unqualifiedByDesign is set.
	predicate []string
	// unqualifiedByDesign marks an entry that deliberately blesses a statement with NO predicate.
	// Exactly one entry has it, and adding a second should be an argument, not a keystroke.
	unqualifiedByDesign bool
}

// allowedMigrationHistoryDeletes lists the migration files that may hard-delete a history-bearing
// table, keyed "<file>|<table>". Required for every UP migration; required for a DOWN migration
// only when the statement is UNQUALIFIED (see the doc comment above).
//
// ⚠ A migration file is IMMUTABLE once it has run anywhere. An entry here is therefore a record of
// a decision already taken, not a permission being granted — the only lever a future reader has is
// to write a NEW migration. Say what actually happened, and do not soften it.
var allowedMigrationHistoryDeletes = map[string]nonGoDeleteAllowance{
	// Both of these are dedup PREFLIGHTS: they delete duplicate budget_categories rows so that a
	// stricter UNIQUE INDEX can be created in the same file. The rows destroyed are, by the
	// predicate, exact duplicates of a row that survives (`a.ctid > b.ctid` keeps the smallest
	// ctid per discriminated key), so no distinct budget the user set survives only in a deleted
	// row. Re-derived 2026-08-26 by reading both predicates.
	//
	// `a.ctid > b.ctid` is the leg that makes the statement a DEDUP rather than a wipe: without it
	// the self-join matches every row against itself and the file empties the table. It is named
	// first in both predicates for that reason.
	"000083_harden_budget_category_uniqueness.up.sql|budget_categories": {
		reason: "dedup preflight for the NULLS NOT DISTINCT unique index: deletes only rows that duplicate a surviving row on the full discriminated key (#1203)",
		predicate: []string{
			"USING budget_categories b", "WHERE a.ctid > b.ctid",
			"AND a.budget_id = b.budget_id", "AND a.category_kind = b.category_kind",
			"AND a.pfc_primary IS NOT DISTINCT FROM b.pfc_primary",
			"AND a.pfc_detailed IS NOT DISTINCT FROM b.pfc_detailed",
			"AND a.custom_category_id IS NOT DISTINCT FROM b.custom_category_id",
		},
	},
	"000097_savings_progress_and_budget_linkage.up.sql|budget_categories": {
		reason: "dedup preflight for the savings_goal_id-widened unique index: same shape and same predicate as 000083's, one column wider",
		predicate: []string{
			"USING budget_categories b", "WHERE a.ctid > b.ctid",
			"AND a.budget_id = b.budget_id", "AND a.category_kind = b.category_kind",
			"AND a.pfc_primary IS NOT DISTINCT FROM b.pfc_primary",
			"AND a.pfc_detailed IS NOT DISTINCT FROM b.pfc_detailed",
			"AND a.custom_category_id IS NOT DISTINCT FROM b.custom_category_id",
			"AND a.savings_goal_id IS NOT DISTINCT FROM b.savings_goal_id",
		},
	},

	// The one UNQUALIFIED delete in the tree: `DELETE FROM account_status_history;` with no WHERE.
	//
	// ⚠ It is NOT a clean reversal, and its own header says so: it deliberately does not try to
	// separate backfilled rows from rows the triggers recorded afterwards, so rolling 000152 back
	// destroys real liveness history written since it ran. The reason it is accepted rather than
	// fixed is that the rollback CHAIN makes the distinction unobservable — 000151's down.sql drops
	// the whole table immediately after — and the file is immutable. Re-derived 2026-08-26 by
	// reading 000152.down.sql and confirming 000151.down.sql drops account_status_history.
	//
	// A NEW migration must not copy this. Write the WHERE.
	"000152_backfill_account_status_history.down.sql|account_status_history": {
		reason:              "unqualified by design: 000151's down.sql drops account_status_history entirely on the next step of the same rollback chain, so a surgical predicate here would be unobservable complexity. Accepted, not endorsed — do not copy it",
		unqualifiedByDesign: true,
	},
}

// allowedShellHistoryDeletes lists the shell scripts under backend/scripts that may hard-delete a
// history-bearing table, keyed "<path under backend/>|<table>".
var allowedShellHistoryDeletes = map[string]nonGoDeleteAllowance{
	// The QA data populator. These run against a QA/local database that the populator itself
	// seeded, addressed by a user_id it just looked up by test email; they are the teardown half of
	// "create this fixture user, then remove them". They are not reachable from the product: no Go
	// code, no endpoint and no job invokes these scripts. Re-derived 2026-08-26 by reading both
	// files — every statement is `WHERE user_id = '$user_id'`.
	//
	// ⚠ THE PREDICATE IS THE WHOLE SAFETY ARGUMENT HERE, which is why it is now pinned. The reason
	// above says "scoped to one seeded test user"; strip the WHERE and the reason is a lie while
	// the key still matches. `$user_id` is named literally, not just `user_id`: swapping the shell
	// variable for something the caller supplies is the other way this scope goes wrong.
	"scripts/qa-data-populator/lib/db-operations.sh|transactions": {
		reason:    "QA fixture teardown, scoped to one seeded test user by id",
		predicate: []string{"WHERE user_id = '$user_id'"},
	},
	"scripts/qa-data-populator/lib/db-operations.sh|accounts": {
		reason:    "QA fixture teardown, scoped to one seeded test user by id",
		predicate: []string{"WHERE user_id = '$user_id'"},
	},
	"scripts/qa-data-populator/lib/db-operations.sh|balance_snapshots": {
		reason:    "QA fixture teardown, scoped to one seeded test user by id",
		predicate: []string{"WHERE user_id = '$user_id'"},
	},
	"scripts/qa-data-populator/lib/cleanup.sh|transactions": {
		reason:    "QA fixture data cleanup, scoped to one seeded test user by id (keeps the user row)",
		predicate: []string{"WHERE user_id = '$user_id'"},
	},
	"scripts/qa-data-populator/lib/cleanup.sh|accounts": {
		reason:    "QA fixture data cleanup, scoped to one seeded test user by id (keeps the user row)",
		predicate: []string{"WHERE user_id = '$user_id'"},
	},
	"scripts/qa-data-populator/lib/cleanup.sh|balance_snapshots": {
		reason:    "QA fixture data cleanup, scoped to one seeded test user by id (keeps the user row)",
		predicate: []string{"WHERE user_id = '$user_id'"},
	},
}

// allowedFrontendFixtureHistoryDeletes lists the frontend-tree fixture files under
// apps/web/scripts that may hard-delete a history-bearing table, keyed "<path from the repo
// root>|<table>".
//
// ⚠ Re-derived 2026-08-26 by reading every predicate in the file, NOT inherited from "it is E2E, so
// it is fine". That inherited reason is wrong on its own terms: this file is `psql $DATABASE_URL -f`
// with no assertion about which database $DATABASE_URL names, so "it only ever runs locally" is a
// property of today's callers, not of the file.
//
// What holds instead is that every delete is scoped to ONE hardcoded fixture identity — the literal
// email `e2e+clerk_test@boostedbudget.com`, resolved to v_user_id at the top of the DO block — or
// to ids that same block has just created. Pointed at a database full of real users, the file would
// destroy the fixture user's rows and nobody else's. And each of these three is an idempotency
// device rather than a teardown: the DELETE is immediately followed by the INSERT that replaces the
// rows it removed, so re-running the seed does not accumulate duplicates.
//
// ⚠ That argument is entirely about the PREDICATES, so since #1569 T7-FIX the predicates are part
// of the entry: an edit that drops `AND source = 'e2e_seed'` widens this file from "the fixture
// user's synthetic rows" to "everything the fixture user has", and used to keep its blessing.
var allowedFrontendFixtureHistoryDeletes = map[string]nonGoDeleteAllowance{
	"apps/web/scripts/seed-e2e-data.sql|balance_snapshots": {
		reason:    "e2e seed idempotency, doubly scoped: `account_id IN (<the three accounts this block just created>) AND source = 'e2e_seed'`. Re-inserted on the next statement",
		predicate: []string{"WHERE account_id IN (v_checking_id, v_savings_id, v_credit_id)", "AND source = 'e2e_seed'"},
	},
	"apps/web/scripts/seed-e2e-data.sql|transactions": {
		reason:    "e2e seed idempotency, scoped to the fixture user's own synthetic rows: `user_id = v_user_id AND source = 'plaid' AND external_transaction_id LIKE 'e2e-%'`. Re-inserted on the next statement",
		predicate: []string{"WHERE user_id = v_user_id", "AND source = 'plaid'", "AND external_transaction_id LIKE 'e2e-%'"},
	},
	// The loosest of the three, and the only one worth arguing about: it removes EVERY category of
	// that budget, not only the ones a previous seed run inserted. It is still scoped, because
	// v_budget_id is the budget the same DO block upserted for the fixture user two statements
	// earlier — the blast radius is one fixture budget, and the categories are re-inserted
	// immediately. ON CONFLICT is not usable here: the UNIQUE constraint carries NULLs for pfc rows.
	"apps/web/scripts/seed-e2e-data.sql|budget_categories": {
		reason:    "e2e seed idempotency, scoped to `budget_id = v_budget_id`, the fixture user's budget upserted by the same DO block; re-inserted on the next statement. Wider than the other two (it clears the budget's categories, not just seeded ones) and deliberately so — ON CONFLICT is unusable because the UNIQUE constraint has NULLs for pfc rows",
		predicate: []string{"WHERE budget_id = v_budget_id"},
	},
}

// sqlStatementEndRegexp finds the end of a statement: the first `;`.
var sqlStatementEndRegexp = regexp.MustCompile(`;`)

// trivialPredicateRegexp matches a WHERE clause that constrains NOTHING — `WHERE TRUE`, `WHERE 1=1`
// and their spacing variants, and only when the clause is the whole tail of the statement. It is
// applied to the normalised (upper-cased, whitespace-collapsed) statement.
//
// ⚠ ITS SCOPE, PRECISELY (#1569 T7-FIX finding E). This is not an attempt to judge predicates. It
// refuses the two spellings whose ONLY use is to satisfy a "must carry a WHERE" rule without
// constraining anything, because those are the shapes someone types when the guard is in their way.
// `WHERE TRUE AND account_id = $1` is a real predicate and is accepted; so is any predicate that is
// semantically total but syntactically non-trivial. See the doc comment at the top of this file.
var trivialPredicateRegexp = regexp.MustCompile(`\bWHERE\s+(?:TRUE|1\s*=\s*1)\s*$`)

// isQualifiedPredicate reports whether a normalised statement carries a predicate worth the name.
func isQualifiedPredicate(stmt string) bool {
	return strings.Contains(stmt, "WHERE") && !trivialPredicateRegexp.MatchString(stmt)
}

// shellCommentLineRegexp matches a line whose first non-space character is `#`. Only whole comment
// LINES are dropped, never a trailing `#`, because `#` inside a shell string is not a comment and
// stripping it would corrupt the SQL this guard is reading.
var shellCommentLineRegexp = regexp.MustCompile(`(?m)^[ \t]*#[^\n]*$`)

// nonGoDeleteSite is one hard delete found in a .sql or .sh file.
type nonGoDeleteSite struct {
	file      string // path relative to backend/
	table     string
	line      int
	statement string // normalised
	qualified bool   // the statement carries a WHERE
}

// scanNonGoHistoryDeletes walks root for files matching exts and returns every hard delete of a
// history-bearing table, one entry per statement.
func scanNonGoHistoryDeletes(t *testing.T, backendDir, sub string, ext string, stripShellComments bool) []nonGoDeleteSite {
	t.Helper()
	var out []nonGoDeleteSite
	root := filepath.Join(backendDir, sub)

	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() || !strings.HasSuffix(path, ext) {
			return nil
		}
		raw, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		text := string(raw)
		if stripShellComments {
			text = shellCommentLineRegexp.ReplaceAllString(text, "")
		}
		// Strip SQL comments before matching, so a comment quoting a delete is not a delete — and
		// keep a line-number map by counting newlines in the ORIGINAL text up to the match, which
		// requires matching on a comment-blanked copy of the SAME LENGTH.
		blanked := blankSQLComments(text)
		rel, _ := filepath.Rel(backendDir, path)
		rel = filepath.ToSlash(rel)

		for _, m := range historyDeleteRefRegexp(historyBearingTables).FindAllStringSubmatchIndex(blanked, -1) {
			end := len(blanked)
			if semi := sqlStatementEndRegexp.FindStringIndex(blanked[m[1]:]); semi != nil {
				end = m[1] + semi[0]
			}
			stmt := normaliseSQL(blanked[m[0]:end])
			out = append(out, nonGoDeleteSite{
				file:      rel,
				table:     strings.ToLower(blanked[m[2]:m[3]]),
				line:      strings.Count(text[:m[0]], "\n") + 1,
				statement: stmt,
				qualified: isQualifiedPredicate(stmt),
			})
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walking %s: %v", root, err)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].file != out[j].file {
			return out[i].file < out[j].file
		}
		return out[i].line < out[j].line
	})
	return out
}

// blankSQLComments replaces the contents of SQL comments with spaces, preserving byte offsets so
// that line numbers computed against the original text stay correct.
func blankSQLComments(s string) string {
	blank := func(m string) string {
		return strings.Map(func(r rune) rune {
			if r == '\n' {
				return r
			}
			return ' '
		}, m)
	}
	s = sqlBlockCommentRegexp.ReplaceAllStringFunc(s, blank)
	return sqlLineCommentRegexp.ReplaceAllStringFunc(s, blank)
}

// repoRootDir resolves the repository root, one level above backend/. G4's universe reaches
// outside the Go tree, so the scan needs a root that does too.
func repoRootDir(t *testing.T) string {
	t.Helper()
	return filepath.Dir(backendDirRoot(t))
}

// backendDirRoot resolves backend/ from this package's directory.
func backendDirRoot(t *testing.T) string {
	t.Helper()
	return filepath.Dir(repoInternalRoot(t))
}

// widenedNonGoDelete reports one statement that still matches an allowlist key but no longer
// carries the predicate that key was blessed for. Returns "" when the statement is still in scope.
//
// This is G4's half of the discipline G2 gained in T7.5: the key alone means "this file may delete
// from this table", which is not the decision anyone actually took. The decision was "this file may
// delete THESE ROWS from this table", and that lives in the predicate.
func widenedNonGoDelete(key string, a nonGoDeleteAllowance, s nonGoDeleteSite) string {
	if a.unqualifiedByDesign {
		return ""
	}
	if !s.qualified {
		return key + " (" + s.file + ":" + itoa(s.line) + ")\n        the statement carries no " +
			"predicate at all, but its entry is scoped to: " + strings.Join(a.predicate, " | ") +
			"\n        statement: " + s.statement
	}
	if missing := missingPredicateFragments(s.statement, a.predicate); len(missing) > 0 {
		return key + " (" + s.file + ":" + itoa(s.line) + ")\n        missing: " +
			strings.Join(missing, " | ") + "\n        statement: " + s.statement
	}
	return ""
}

// reportWidenedNonGoDeletes is the shared failure message for all three G4 halves.
func reportWidenedNonGoDeletes(t *testing.T, listName string, widened []string) {
	t.Helper()
	if len(widened) == 0 {
		return
	}
	sort.Strings(widened)
	t.Errorf("%d allowlisted delete(s) in %s no longer carry the predicate they were blessed for "+
		"(#1569 G4).\n"+
		"The entry still authorises this destruction, but the SQL beneath it has widened — which is "+
		"the exact failure the predicate exists to catch. A key of the form <file>|<table> says "+
		"\"this file may delete from this table\"; nobody ever decided that. Either restore the "+
		"missing leg, or change the entry deliberately and say why in its reason.\n    %s",
		len(widened), listName, strings.Join(widened, "\n    "))
}

// TestNoUnallowlistedMigrationHistoryDelete is guard G4's migration half.
func TestNoUnallowlistedMigrationHistoryDelete(t *testing.T) {
	sites := scanNonGoHistoryDeletes(t, backendDirRoot(t), "migrations", ".sql", false)

	// Fail closed: the allowlist is non-empty, so finding nothing means the walk broke.
	if len(sites) == 0 {
		t.Fatalf("scanned backend/migrations and found no history DELETE at all, but %d are "+
			"allowlisted — the scan is broken and would pass vacuously", len(allowedMigrationHistoryDeletes))
	}

	matched := map[string]bool{}
	var offenders, widened []string
	for _, s := range sites {
		key := filepath.Base(s.file) + "|" + s.table
		isDown := strings.HasSuffix(s.file, ".down.sql")
		if allowance, ok := allowedMigrationHistoryDeletes[key]; ok {
			matched[key] = true
			if w := widenedNonGoDelete(key, allowance, s); w != "" {
				widened = append(widened, w)
			}
			continue
		}
		if isDown && s.qualified {
			continue // a reversal that says which rows — allowed without an entry.
		}
		why := "an UP migration hard-deletes a history-bearing table"
		if isDown {
			why = "a DOWN migration hard-deletes a history-bearing table with NO WHERE clause, so it " +
				"cannot be a reversal of what its up migration inserted — it also destroys rows " +
				"written after the migration ran"
		}
		offenders = append(offenders, s.file+":"+itoa(s.line)+" -> "+s.table+" ("+why+")\n        "+s.statement)
	}

	var stale []string
	for key := range allowedMigrationHistoryDeletes {
		if !matched[key] {
			stale = append(stale, key)
		}
	}
	sort.Strings(stale)

	if len(offenders) > 0 {
		t.Errorf("%d migration history delete(s) are not accounted for (#1569 G4).\n"+
			"#1569 decided that financial history is retained unless the user explicitly asks for "+
			"it to go, and a migration is the one place that decision is invisible to code review "+
			"after the fact. Either qualify the statement so it says which rows it removes, or add "+
			"it to allowedMigrationHistoryDeletes with a reason you have re-derived.\n    %s",
			len(offenders), strings.Join(offenders, "\n    "))
	}
	if len(stale) > 0 {
		t.Errorf("%d allowlisted migration delete(s) no longer exist (#1569 G4).\n"+
			"Migration files are immutable, so a stale entry means the key is wrong, not that the "+
			"delete went away. Fix the key:\n    %s", len(stale), strings.Join(stale, "\n    "))
	}
	reportWidenedNonGoDeletes(t, "allowedMigrationHistoryDeletes", widened)
}

// TestNoUnallowlistedShellHistoryDelete is guard G4's shell half. Unlike a migration, a shell script
// is mutable and re-runnable, so there is no qualified-statement escape hatch here: every hard
// delete of history from backend/scripts is on the list or it fails.
func TestNoUnallowlistedShellHistoryDelete(t *testing.T) {
	sites := scanNonGoHistoryDeletes(t, backendDirRoot(t), "scripts", ".sh", true)

	if len(sites) == 0 {
		t.Fatalf("scanned backend/scripts and found no history DELETE at all, but %d are "+
			"allowlisted — the scan is broken and would pass vacuously", len(allowedShellHistoryDeletes))
	}

	matched := map[string]bool{}
	var offenders, widened []string
	for _, s := range sites {
		key := s.file + "|" + s.table
		if allowance, ok := allowedShellHistoryDeletes[key]; ok {
			matched[key] = true
			if w := widenedNonGoDelete(key, allowance, s); w != "" {
				widened = append(widened, w)
			}
			continue
		}
		offenders = append(offenders, s.file+":"+itoa(s.line)+" -> "+s.table+"\n        "+s.statement)
	}

	var stale []string
	for key := range allowedShellHistoryDeletes {
		if !matched[key] {
			stale = append(stale, key)
		}
	}
	sort.Strings(stale)

	if len(offenders) > 0 {
		t.Errorf("%d shell history delete(s) under backend/scripts are not allowlisted (#1569 G4).\n"+
			"A shell script talking to the database directly is invisible to every Go guard. If this "+
			"delete is fixture teardown against a QA database, say so in allowedShellHistoryDeletes "+
			"and scope it. If it can reach a real user's data, it is a defect.\n    %s",
			len(offenders), strings.Join(offenders, "\n    "))
	}
	if len(stale) > 0 {
		t.Errorf("%d allowlisted shell delete(s) no longer exist (#1569 G4).\n"+
			"A stale entry reads as a standing permission to destroy this history. Delete it:\n    %s",
			len(stale), strings.Join(stale, "\n    "))
	}
	reportWidenedNonGoDeletes(t, "allowedShellHistoryDeletes", widened)
}

// TestNoUnallowlistedFrontendFixtureHistoryDelete is guard G4's frontend-fixture half, added in
// #1569 task 7.9 when apps/web/scripts stopped being another agent's territory.
//
// It scans BOTH .sql and .sh under apps/web/scripts, because the e2e harness is a shell script that
// runs the .sql and could as easily inline a statement of its own. The rule is backend/scripts':
// these files are mutable and re-runnable, so there is no qualified-statement escape hatch — every
// hard delete of a history-bearing table is on the list or it fails.
func TestNoUnallowlistedFrontendFixtureHistoryDelete(t *testing.T) {
	root := repoRootDir(t)
	sites := scanNonGoHistoryDeletes(t, root, "apps/web/scripts", ".sql", false)
	sites = append(sites, scanNonGoHistoryDeletes(t, root, "apps/web/scripts", ".sh", true)...)

	if len(sites) == 0 {
		t.Fatalf("scanned apps/web/scripts and found no history DELETE at all, but %d are "+
			"allowlisted — the scan is broken and would pass vacuously", len(allowedFrontendFixtureHistoryDeletes))
	}

	matched := map[string]bool{}
	var offenders, widened []string
	for _, s := range sites {
		key := s.file + "|" + s.table
		if allowance, ok := allowedFrontendFixtureHistoryDeletes[key]; ok {
			matched[key] = true
			if w := widenedNonGoDelete(key, allowance, s); w != "" {
				widened = append(widened, w)
			}
			continue
		}
		offenders = append(offenders, s.file+":"+itoa(s.line)+" -> "+s.table+"\n        "+s.statement)
	}

	var stale []string
	for key := range allowedFrontendFixtureHistoryDeletes {
		if !matched[key] {
			stale = append(stale, key)
		}
	}
	sort.Strings(stale)

	if len(offenders) > 0 {
		t.Errorf("%d fixture history delete(s) under apps/web/scripts are not allowlisted (#1569 G4).\n"+
			"A fixture that talks to the database directly is invisible to every Go guard, and "+
			"nothing in this tree pins which database $DATABASE_URL names. Scope the statement to "+
			"the fixture user and say so in allowedFrontendFixtureHistoryDeletes, with a reason you "+
			"have re-derived from the predicate rather than from \"it is E2E\".\n    %s",
			len(offenders), strings.Join(offenders, "\n    "))
	}
	if len(stale) > 0 {
		t.Errorf("%d allowlisted fixture delete(s) no longer exist (#1569 G4).\n"+
			"A stale entry reads as a standing permission to destroy this history. Delete it:\n    %s",
			len(stale), strings.Join(stale, "\n    "))
	}
	reportWidenedNonGoDeletes(t, "allowedFrontendFixtureHistoryDeletes", widened)
}

// TestG4AllowlistEntriesHaveReasons keeps the allowlists from becoming bare suppression lists.
func TestG4AllowlistEntriesHaveReasons(t *testing.T) {
	for name, list := range map[string]map[string]nonGoDeleteAllowance{
		"allowedMigrationHistoryDeletes":       allowedMigrationHistoryDeletes,
		"allowedShellHistoryDeletes":           allowedShellHistoryDeletes,
		"allowedFrontendFixtureHistoryDeletes": allowedFrontendFixtureHistoryDeletes,
	} {
		for key, allowance := range list {
			if strings.TrimSpace(allowance.reason) == "" {
				t.Errorf("%s[%q] has no reason; the reason is the point", name, key)
			}
			switch {
			case allowance.unqualifiedByDesign && len(allowance.predicate) > 0:
				t.Errorf("%s[%q] is marked unqualifiedByDesign AND names a predicate. It cannot be "+
					"both: the mark says the statement has no WHERE at all, and the predicate says "+
					"which rows the WHERE names. Pick one deliberately.", name, key)
			case !allowance.unqualifiedByDesign && len(allowance.predicate) == 0:
				t.Errorf("%s[%q] names no predicate. A <file>|<table> key with no predicate blesses "+
					"`DELETE FROM <table>` full stop — which is the hole #1569 T7-FIX closed, and "+
					"the one G2 closed a round earlier. Name the legs that make the destruction "+
					"narrow, including their polarity. If the statement really has no WHERE, set "+
					"unqualifiedByDesign and say why in the reason.", name, key)
			}
			for _, frag := range allowance.predicate {
				if strings.TrimSpace(frag) == "" {
					t.Errorf("%s[%q] has an empty predicate fragment, which every statement "+
						"trivially satisfies", name, key)
				}
			}
			i := strings.LastIndex(key, "|")
			if i < 0 {
				t.Errorf("%s key %q is not of the form <file>|<table>", name, key)
				continue
			}
			if _, ok := historyBearingTables[key[i+1:]]; !ok {
				t.Errorf("%s key %q names table %q, which is not history-bearing — the entry can "+
					"never match and is permanently stale", name, key, key[i+1:])
			}
		}
	}
}

// ---- Meta-tests: the constructed violations, and the legitimate cases that must NOT fire. -------

// TestG4Classification_QualifiedDownMigrationIsNotAViolation is the false-positive half, and it is
// the more important one: six down migrations delete from history tables today and every one of
// them is a correct reversal. A guard that fires on them gets deleted by the next engineer.
func TestG4Classification_QualifiedDownMigrationIsNotAViolation(t *testing.T) {
	sites := scanNonGoHistoryDeletes(t, backendDirRoot(t), "migrations", ".sql", false)

	var qualifiedDowns int
	for _, s := range sites {
		if !strings.HasSuffix(s.file, ".down.sql") || !s.qualified {
			continue
		}
		qualifiedDowns++
		key := filepath.Base(s.file) + "|" + s.table
		if _, ok := allowedMigrationHistoryDeletes[key]; ok {
			t.Errorf("%s:%d deletes from %s WITH a predicate and should need no allowlist entry, "+
				"but one exists — the entry is redundant and will rot", s.file, s.line, s.table)
		}
	}
	if qualifiedDowns < 5 {
		t.Fatalf("expected the tree to still contain several qualified down-migration deletes for "+
			"this test to mean anything, found %d — re-derive before relaxing the number", qualifiedDowns)
	}
}

// TestG4Classification_SeesTheShapesThatMatter is the constructed-violation set, run against the
// classifier rather than the tree so that each shape is exercised whether or not the tree contains
// it today.
func TestG4Classification_SeesTheShapesThatMatter(t *testing.T) {
	dir := t.TempDir()
	write := func(name, body string) {
		if err := os.MkdirAll(filepath.Join(dir, "migrations"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "migrations", name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("000900_x.up.sql", "DELETE FROM balance_snapshots WHERE account_id = '1';\n")
	write("000900_x.down.sql", "-- reversal\nDELETE FROM boost_executions WHERE boost_id IS NULL;\n")
	write("000901_y.down.sql", "DELETE FROM rollover_history;\n")
	write("000902_z.up.sql", "-- DELETE FROM transactions WHERE 1=1;\nALTER TABLE transactions ADD COLUMN x int;\n")
	write("000903_w.up.sql", "DELETE FROM notifications WHERE id = '1';\n")

	sites := scanNonGoHistoryDeletes(t, dir, "migrations", ".sql", false)

	got := map[string]string{}
	for _, s := range sites {
		verdict := "unqualified"
		if s.qualified {
			verdict = "qualified"
		}
		got[s.file+"|"+s.table] = verdict
	}
	want := map[string]string{
		// An UP migration delete is seen, qualified or not — the up rule needs an entry either way.
		"migrations/000900_x.up.sql|balance_snapshots": "qualified",
		// A qualified DOWN delete is seen and classified as qualified, so it needs no entry.
		"migrations/000900_x.down.sql|boost_executions": "qualified",
		// The bare DOWN delete: seen and classified unqualified, so it needs an entry.
		"migrations/000901_y.down.sql|rollover_history": "unqualified",
		// 000902's delete is inside a COMMENT and must not be seen at all.
		// 000903 deletes a non-history table and is none of this guard's business.
	}
	if len(got) != len(want) {
		t.Fatalf("classified %v, want exactly %v", got, want)
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s: got %q, want %q", k, got[k], v)
		}
	}
}

// TestG4Classification_ShellCommentIsNotADelete pins the one shell-specific rule: a `#` comment
// line is not code, but a `#` inside a string is not a comment.
func TestG4Classification_ShellCommentIsNotADelete(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "scripts"), 0o755); err != nil {
		t.Fatal(err)
	}
	body := "# DELETE FROM transactions WHERE user_id = 'x';\n" +
		"execute_db \"DELETE FROM balance_snapshots WHERE user_id = '$id'; # not a comment\"\n"
	if err := os.WriteFile(filepath.Join(dir, "scripts", "s.sh"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	sites := scanNonGoHistoryDeletes(t, dir, "scripts", ".sh", true)
	if len(sites) != 1 || sites[0].table != "balance_snapshots" || !sites[0].qualified {
		t.Fatalf("expected exactly the balance_snapshots delete, qualified; got %+v", sites)
	}
}

// TestG4Classification_WideningAnAllowlistedStatementIsAViolation is the constructed violation for
// finding A: the shape the `file|table` key could never see.
//
// It runs the REAL entries against a mutated copy of the REAL statements, so it fails if either the
// entry or the file is edited to disagree with the other — which is the whole point of pinning the
// predicate rather than restating it here.
func TestG4Classification_WideningAnAllowlistedStatementIsAViolation(t *testing.T) {
	cases := []struct {
		name      string
		key       string
		list      map[string]nonGoDeleteAllowance
		intact    string
		widened   string
		wantInMsg string
	}{
		{
			name:      "the shell teardown loses its user scope entirely",
			key:       "scripts/qa-data-populator/lib/cleanup.sh|transactions",
			list:      allowedShellHistoryDeletes,
			intact:    "DELETE FROM transactions WHERE user_id = '$user_id'",
			widened:   "DELETE FROM transactions",
			wantInMsg: "no predicate at all",
		},
		{
			name:      "the e2e seed keeps a WHERE but drops the synthetic-row leg",
			key:       "apps/web/scripts/seed-e2e-data.sql|transactions",
			list:      allowedFrontendFixtureHistoryDeletes,
			intact:    "DELETE FROM transactions WHERE user_id = v_user_id AND source = 'plaid' AND external_transaction_id LIKE 'e2e-%'",
			widened:   "DELETE FROM transactions WHERE user_id = v_user_id",
			wantInMsg: "AND source = 'plaid'",
		},
		{
			name:      "the dedup preflight drops the ctid leg and becomes a wipe",
			key:       "000083_harden_budget_category_uniqueness.up.sql|budget_categories",
			list:      allowedMigrationHistoryDeletes,
			intact:    readMigrationDeleteStatement(t, "000083_harden_budget_category_uniqueness.up.sql", "budget_categories"),
			widened:   "DELETE FROM budget_categories a USING budget_categories b WHERE a.budget_id = b.budget_id",
			wantInMsg: "WHERE a.ctid > b.ctid",
		},
		{
			name:      "a bare WHERE TRUE does not count as a predicate",
			key:       "scripts/qa-data-populator/lib/cleanup.sh|accounts",
			list:      allowedShellHistoryDeletes,
			intact:    "DELETE FROM accounts WHERE user_id = '$user_id'",
			widened:   "DELETE FROM accounts WHERE true",
			wantInMsg: "no predicate at all",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			allowance, ok := tc.list[tc.key]
			if !ok {
				t.Fatalf("%s is no longer allowlisted — re-derive this case rather than deleting it", tc.key)
			}

			site := func(sql string) nonGoDeleteSite {
				stmt := normaliseSQL(sql)
				return nonGoDeleteSite{file: "x", line: 1, statement: stmt, qualified: isQualifiedPredicate(stmt)}
			}

			if msg := widenedNonGoDelete(tc.key, allowance, site(tc.intact)); msg != "" {
				t.Fatalf("the REAL statement must satisfy its OWN entry, got: %s", msg)
			}
			msg := widenedNonGoDelete(tc.key, allowance, site(tc.widened))
			if msg == "" {
				t.Fatalf("the widened statement was accepted — this is exactly the hole finding A reported")
			}
			if !strings.Contains(msg, tc.wantInMsg) {
				t.Errorf("the failure must name what went missing; got %q, want it to contain %q", msg, tc.wantInMsg)
			}
		})
	}
}

// readMigrationDeleteStatement pulls one real, normalised delete statement out of a migration file,
// so a case above cannot drift from the SQL it claims to quote.
func readMigrationDeleteStatement(t *testing.T, name, table string) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(backendDirRoot(t), "migrations", name))
	if err != nil {
		t.Fatalf("reading %s: %v", name, err)
	}
	text := blankSQLComments(string(raw))
	for _, m := range historyDeleteRefRegexp(historyBearingTables).FindAllStringSubmatchIndex(text, -1) {
		if !strings.EqualFold(text[m[2]:m[3]], table) {
			continue
		}
		end := len(text)
		if semi := sqlStatementEndRegexp.FindStringIndex(text[m[1]:]); semi != nil {
			end = m[1] + semi[0]
		}
		return text[m[0]:end]
	}
	t.Fatalf("no delete of %s found in %s", table, name)
	return ""
}

// TestG4Classification_TrivialPredicateIsNotQualified pins the ONE syntactic escape the qualified
// check refuses, and — the more important half — the things it must keep accepting. A guard that
// second-guesses real predicates fires on correct migrations and gets switched off.
func TestG4Classification_TrivialPredicateIsNotQualified(t *testing.T) {
	cases := []struct {
		sql  string
		want bool
	}{
		{"DELETE FROM transactions", false},
		{"DELETE FROM transactions WHERE TRUE", false},
		{"delete from transactions where true", false},
		{"DELETE FROM transactions WHERE 1=1", false},
		{"DELETE FROM transactions WHERE 1 = 1", false},
		// Accepted: a real column makes it a predicate, however wide it may be in meaning. This
		// guard does not read SQL semantics and does not claim to.
		{"DELETE FROM transactions WHERE TRUE AND user_id = $1", true},
		{"DELETE FROM transactions WHERE user_id = $1", true},
		{"DELETE FROM transactions WHERE id <> '00000000-0000-0000-0000-000000000000'", true},
	}
	for _, tc := range cases {
		if got := isQualifiedPredicate(normaliseSQL(tc.sql)); got != tc.want {
			t.Errorf("isQualifiedPredicate(%q) = %v, want %v", tc.sql, got, tc.want)
		}
	}
}

// TestG4Classification_ATrivialDownMigrationPredicateStillNeedsAnEntry ties the rule above back to
// the guard: `WHERE true` is precisely what someone writes to get past "a down migration must say
// which rows", so it must not get past it.
func TestG4Classification_ATrivialDownMigrationPredicateStillNeedsAnEntry(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "migrations"), 0o755); err != nil {
		t.Fatal(err)
	}
	body := "DELETE FROM rollover_history WHERE true;\n"
	if err := os.WriteFile(filepath.Join(dir, "migrations", "000904_v.down.sql"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	sites := scanNonGoHistoryDeletes(t, dir, "migrations", ".sql", false)
	if len(sites) != 1 {
		t.Fatalf("expected one site, got %d", len(sites))
	}
	if sites[0].qualified {
		t.Fatalf("`WHERE true` must not count as saying which rows; got qualified=true for %q", sites[0].statement)
	}
}

// itoa avoids importing strconv for one call.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}
