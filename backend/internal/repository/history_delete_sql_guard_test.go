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
//     migration inserted. `WHERE true` would pass. The check is that the author was made to write a
//     predicate, not that the predicate is right.
//   - It does NOT read migrations for anything else — a `DROP TABLE` in a down migration is out of
//     scope here and is G1's business (the schema half) plus review.
//   - Its universe is `backend/migrations` and `backend/scripts`. `apps/web/scripts/seed-e2e-data.sql`
//     also deletes from `balance_snapshots`, `transactions` and `budget_categories` and is
//     deliberately NOT scanned: it is the Playwright fixture, it runs only against the local e2e
//     database created by the e2e harness, and it is owned by the frontend tree. Re-derived
//     2026-08-26; if it ever runs anywhere else, widen this universe rather than arguing about it.
//   - It says nothing about `UPDATE`. Tombstoning is the normal path and is not a delete.

// allowedMigrationHistoryDeletes lists the migration files that may hard-delete a history-bearing
// table, keyed "<file>|<table>", with the reason. Required for every UP migration; required for a
// DOWN migration only when the statement is UNQUALIFIED (see the doc comment above).
//
// ⚠ A migration file is IMMUTABLE once it has run anywhere. An entry here is therefore a record of
// a decision already taken, not a permission being granted — the only lever a future reader has is
// to write a NEW migration. Say what actually happened, and do not soften it.
var allowedMigrationHistoryDeletes = map[string]string{
	// Both of these are dedup PREFLIGHTS: they delete duplicate budget_categories rows so that a
	// stricter UNIQUE INDEX can be created in the same file. The rows destroyed are, by the
	// predicate, exact duplicates of a row that survives (`a.ctid > b.ctid` keeps the smallest
	// ctid per discriminated key), so no distinct budget the user set survives only in a deleted
	// row. Re-derived 2026-08-26 by reading both predicates.
	"000083_harden_budget_category_uniqueness.up.sql|budget_categories":   "dedup preflight for the NULLS NOT DISTINCT unique index: deletes only rows that duplicate a surviving row on the full discriminated key (#1203)",
	"000097_savings_progress_and_budget_linkage.up.sql|budget_categories": "dedup preflight for the savings_goal_id-widened unique index: same shape and same predicate as 000083's, one column wider",

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
	"000152_backfill_account_status_history.down.sql|account_status_history": "unqualified by design: 000151's down.sql drops account_status_history entirely on the next step of the same rollback chain, so a surgical predicate here would be unobservable complexity. Accepted, not endorsed — do not copy it",
}

// allowedShellHistoryDeletes lists the shell scripts under backend/scripts that may hard-delete a
// history-bearing table, keyed "<path under backend/>|<table>", with the reason.
var allowedShellHistoryDeletes = map[string]string{
	// The QA data populator. These run against a QA/local database that the populator itself
	// seeded, addressed by a user_id it just looked up by test email; they are the teardown half of
	// "create this fixture user, then remove them". They are not reachable from the product: no Go
	// code, no endpoint and no job invokes these scripts. Re-derived 2026-08-26 by reading both
	// files — every statement is `WHERE user_id = '$user_id'`.
	"scripts/qa-data-populator/lib/db-operations.sh|transactions":      "QA fixture teardown, scoped to one seeded test user by id",
	"scripts/qa-data-populator/lib/db-operations.sh|accounts":          "QA fixture teardown, scoped to one seeded test user by id",
	"scripts/qa-data-populator/lib/db-operations.sh|balance_snapshots": "QA fixture teardown, scoped to one seeded test user by id",
	"scripts/qa-data-populator/lib/cleanup.sh|transactions":            "QA fixture data cleanup, scoped to one seeded test user by id (keeps the user row)",
	"scripts/qa-data-populator/lib/cleanup.sh|accounts":                "QA fixture data cleanup, scoped to one seeded test user by id (keeps the user row)",
	"scripts/qa-data-populator/lib/cleanup.sh|balance_snapshots":       "QA fixture data cleanup, scoped to one seeded test user by id (keeps the user row)",
}

// sqlStatementEndRegexp finds the end of a statement: the first `;`.
var sqlStatementEndRegexp = regexp.MustCompile(`;`)

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
				qualified: strings.Contains(stmt, "WHERE"),
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

// backendDirRoot resolves backend/ from this package's directory.
func backendDirRoot(t *testing.T) string {
	t.Helper()
	return filepath.Dir(repoInternalRoot(t))
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
	var offenders []string
	for _, s := range sites {
		key := filepath.Base(s.file) + "|" + s.table
		isDown := strings.HasSuffix(s.file, ".down.sql")
		if _, ok := allowedMigrationHistoryDeletes[key]; ok {
			matched[key] = true
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
	var offenders []string
	for _, s := range sites {
		key := s.file + "|" + s.table
		if _, ok := allowedShellHistoryDeletes[key]; ok {
			matched[key] = true
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
}

// TestG4AllowlistEntriesHaveReasons keeps either allowlist from becoming a bare suppression list.
func TestG4AllowlistEntriesHaveReasons(t *testing.T) {
	for name, list := range map[string]map[string]string{
		"allowedMigrationHistoryDeletes": allowedMigrationHistoryDeletes,
		"allowedShellHistoryDeletes":     allowedShellHistoryDeletes,
	} {
		for key, reason := range list {
			if strings.TrimSpace(reason) == "" {
				t.Errorf("%s[%q] has no reason; the reason is the point", name, key)
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
