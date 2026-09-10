package repository

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// Issue #1569 round 14 finding 3 — the rollback-refusal roster was hand-maintained.
//
// down_migration_refusal_integration_test.go drives a FIXED LIST of down migrations
// (downGuardCases, plus three individually-named tests for 000150/000151/000035) against real
// Postgres to prove each one refuses while the data it would destroy exists, and proceeds when it
// does not. That is real, valuable coverage — but it is a list a human types, and round 13 found
// 000035 unconditionally deleting retained orphan transactions in a down migration that was NOT on
// it. A guard that is only as complete as whoever last remembered to update a list is "protected in
// principle, absent from a list" — the exact bypass class this branch's own guards exist to close
// everywhere else.
//
// THIS FILE IS THE DISCOVERY HALF, not a replacement for the integration behavioural tests. It runs
// as a normal `go test ./...` (no `-tags integration`, no database), so it cannot verify a guard
// actually FIRES at runtime the way the integration suite does — but it can, and does, enumerate
// EVERY `.down.sql` in the tree and CHECK, rather than assume, that every destructive one is
// accounted for. A migration cannot be both destructive and simply unlisted here, because there is
// no list to fall off of: every file is visited, every time.
//
// ⚠ WHAT "DESTRUCTIVE" MEANS HERE, PRECISELY — because a static text scanner has to draw the line
// somewhere and this guard does not claim to catch everything a down migration could do wrong.
// THREE shapes today. The first two are exactly what the existing roster was built to guard
// against (read every entry in downGuardCases and the three named tests: every one is one of
// them); the third was added by #1625 final review B3, and this paragraph said "two shapes" while
// listing three until #1625 second fix wave M5 corrected it:
//
//  1. `DROP TABLE` naming a history-bearing table — total, unconditional loss of everything in it.
//  2. An `ALTER TABLE <history-bearing table> ... ON DELETE CASCADE` — re-arming a cascade that a
//     forward migration disarmed, so the NEXT ordinary delete of the parent silently destroys this
//     table's rows, not the rollback itself.
//  3. `ALTER TABLE <history-bearing table> DROP COLUMN <a LIFECYCLE column>` — #1625 final review
//     B3. NOT "any DROP COLUMN": most dropped columns are recomputable, or are the thing the
//     rollback exists to remove. The narrow set in lifecycleStateColumns is different in kind —
//     each is the ONLY surviving record of which universe a retained row belongs to (live,
//     tombstoned, removed, or a closed accounting period), so dropping it does not restore a prior
//     behaviour, it silently promotes retained rows back into a live universe that then
//     miscounts. 000180 (deleted_at) and 000190 (archived_at) are both exactly this shape, on the
//     same table, and 000190 shipped unguarded WITH a comment asserting the opposite — found by
//     adversarial review, because nothing scanned for it.
//
// A FOURTH shape — an unconditional or unqualified `DELETE FROM`/`TRUNCATE` of a history-bearing
// table — is NOT re-derived here. It already has its own EXHAUSTIVE, checked guard:
// TestNoUnallowlistedMigrationHistoryDelete (G4, history_delete_sql_guard_test.go) scans every
// migration file, up and down, and requires an allowlist entry for exactly this shape. Re-deriving
// it a second time here would be two independently-maintained copies of the same rule, which is the
// failure mode #1569 round 14 task 2 already spent an effort closing elsewhere in this package.
// This file's job is the three shapes G4 explicitly disclaims ("a DROP TABLE in a down migration is
// out of scope here and is G1's business (the schema half) plus review" — history_delete_sql_guard_
// test.go) and that G1 ALSO does not cover, because G1 (history_cascade_guard_test.go) queries the
// LIVE, currently-migrated-up schema, never a down migration's text — a cascade that only exists
// after a rollback is invisible to a guard that only ever looks at pg_constraint on a fully-up
// database.
//
// ⚠ THE CLASSIFICATION IS CHECKED, NOT DECLARED. An entry in
// deliberatelyUnguardedDestructiveDownMigrations does not exempt a file by naming it — the test
// still requires that file to actually be destructive-and-textually-unguarded before the entry
// counts as used, and reports a STALE entry (same discipline as every other allowlist in this
// package) the moment the file stops matching. Nothing here trusts a claim of "non-destructive": a
// file is only ever "non-destructive" by NOT appearing in the destructive scan's output at all —
// there is no separate hand-typed claim for that bucket to be wrong about.
//
// WHAT THIS ROUND FOUND. Re-derived 2026-08-29 by running this exact scan against the full
// migrations tree: FOUR files are destructive-and-textually-unguarded that are NOT anywhere in
// down_migration_refusal_integration_test.go's roster — 000007_create_accounts_table.down.sql,
// 000010_create_transactions_table.down.sql, 000018_add_transaction_categories_and_tags.down.sql
// and 000022_create_budgets_and_savings_goals.down.sql. 000007/000010/000022 were found by manual
// re-derivation before this scanner was written; 000018 was found by the scanner ITSELF on its
// first run against the real tree, after the other three were already recorded — proof this is
// discovery, not a second hand-typed list with the same blind spot. Each is an unconditional
// `DROP TABLE ... CASCADE`, three with only a comment ("WARNING: this will delete ALL ...")
// standing in for a control — the exact anti-pattern this branch's own guard-family header
// (down_migration_refusal_integration_test.go) opens by naming. They are recorded below with a
// reason, but the reason is NOT an engineering endorsement that leaving them unguarded is correct —
// see the doc comment on deliberatelyUnguardedDestructiveDownMigrations for why a judgment call
// was made NOT to silently add a runtime refusal to them in this round.
//
// WHAT THE THIRD SHAPE FOUND, 2026-09-09 (#1625 final review B3). Adding the lifecycle-column
// DROP COLUMN scan surfaced FIVE more files on its first run against the real tree —
// 000149, 000154, 000156, 000158 and 000159 — none of which was on any roster, and two of which
// (000158/000159) #1569 round 12 task 4 had explicitly considered and left DOCUMENTED rather than
// enforced, asking in their own headers for exactly this detector to be built first. They are
// recorded as checked exceptions, with derived reasons.
//
// ⚠ THAT INCONSISTENCY IS NOW RESOLVED, and this paragraph described the old state until #1625.
// B3 left the tree split — 000180/000190 refusing, five others merely documented — and said
// settling it was a separate decision. It was settled by EXECUTION rather than by rereading the
// files: on a scratch database rebuilt from scratch and holding exactly the row each header talks
// about, a real sequential `migrate down` reached and COMPLETED 000154, 000156, 000158 and 000159,
// so each loss was reachable rather than hypothetical. All four now carry a runtime refusal on the
// predicate their own header describes, and appear in this file's `guarded` sample. 000149 alone
// remains a checked exception, and for a different reason: it needs no guard, because its own
// re-added status CHECK aborts the rollback before its DROP COLUMN is reached.

// dropTableRefRegexp matches `DROP TABLE [IF EXISTS] [schema.]<table>`, tolerating the same
// schema-qualification and quoting shapes historyDeleteRefRegexp already tolerates (#1569 T7-FIX
// finding E) — a one-token rename to `DROP TABLE public.transactions` must not turn this guard off
// any more than it may TestNoUnallowlistedHistoryDelete's neighbour.
var dropTableRefRegexp = regexp.MustCompile(`(?is)\bDROP\s+TABLE\s+(?:IF\s+EXISTS\s+)?` +
	`(?:"?[a-z_][a-z0-9_]*"?\s*\.\s*)?"?([a-z_][a-z0-9_]*)"?`)

// alterTableTargetRegexp captures the table an ALTER TABLE statement names, for pairing with a
// same-statement `ON DELETE CASCADE`.
var alterTableTargetRegexp = regexp.MustCompile(`(?is)\bALTER\s+TABLE\s+(?:ONLY\s+)?` +
	`(?:"?[a-z_][a-z0-9_]*"?\s*\.\s*)?"?([a-z_][a-z0-9_]*)"?`)

// onDeleteCascadeRegexp matches the delete-action clause this guard's second shape is watching for.
var onDeleteCascadeRegexp = regexp.MustCompile(`(?is)\bON\s+DELETE\s+CASCADE\b`)

// dropColumnRegexp captures every column an ALTER TABLE statement drops. A single ALTER may drop
// several (`DROP COLUMN a, DROP COLUMN b`), hence FindAllStringSubmatch per statement rather than
// one match.
var dropColumnRegexp = regexp.MustCompile(`(?is)\bDROP\s+COLUMN\s+(?:IF\s+EXISTS\s+)?"?([a-z_][a-z0-9_]*)"?`)

// lifecycleStateColumns is the CLOSED set of columns whose loss silently changes which universe a
// retained row belongs to (#1625 final review B3). Each one is the only surviving record of a
// row's lifecycle state; nothing else in the schema can reconstruct it, which is why the migration
// that introduced it had to ADD a column rather than reuse an existing signal.
//
// ⚠ Keep this narrow. Widening it to "any DROP COLUMN" would flag the down migration of every
// ADD COLUMN in the tree, most of which drop a column that is recomputable or that the rollback
// exists to remove — and a guard that fires on everything is one people learn to allowlist past.
var lifecycleStateColumns = map[string]struct{}{
	"deleted_at":     {}, // the seven tombstones; retained-and-permanently-invisible
	"deleted_reason": {}, // WHY it was tombstoned — a restore must be scoped to a cause it can prove
	"removed_at":     {}, // accounts' tombstone
	"removed_reason": {}, // accounts' cause; RestoreProviderRemovedAccount reads exactly this
	"archived_at":    {}, // savings_goal_contributions: belongs to a CLOSED contributions period
	"purged_at":      {}, // reserved: same shape, so a future purge marker cannot ship unguarded
	"anonymized_at":  {}, // reserved: same shape
}

// migrationNumberRegexp pulls the zero-padded numeric prefix off a migration filename, e.g.
// "000164" from "000164_audit_trail_survives_definition_purge.down.sql".
var migrationNumberRegexp = regexp.MustCompile(`^(\d+)_`)

// downMigrationDestruction is what the discovery scan found for one `.down.sql`: which
// history-bearing tables it destroys — by DROP TABLE, by re-arming ON DELETE CASCADE onto one, or
// by dropping a lifecycleStateColumns column off one — and whether the file's OWN text carries a
// runtime refusal naming its own migration number.
//
// ⚠ #1625 second fix wave M5: this comment, scanDownMigrationDestruction's, and the failure
// message below all still described only the first two shapes after B3 added the third, so a
// reader trusting any of them would have concluded a DROP COLUMN was out of scope for a scanner
// that has been flagging them since B3.
type downMigrationDestruction struct {
	file    string // basename
	tables  []string
	guarded bool
}

// deliberatelyUnguardedDestructiveDownMigration is one CHECKED exception: a destructive down
// migration that carries no runtime `RAISE EXCEPTION ... refusing to roll back <N>` guard, on
// purpose, for a reason a reviewer can evaluate.
type deliberatelyUnguardedDestructiveDownMigration struct {
	reason string
}

// deliberatelyUnguardedDestructiveDownMigrations is #1569 round 14 finding 3's checked exception
// list — the discovery-mechanism analogue of allowedMigrationHistoryDeletes and
// allowedHistoryCascades. An entry here does not mean "safe"; it means "found, looked at, and not
// silently decided" — the report for #1569 round 14 task 5 says so explicitly of the entries it
// found, and nothing in this map is endorsed as correct by this guard's existence.
//
// ⚠ DO NOT WRITE THE NUMBER OF ENTRIES INTO THIS COMMENT. It said "four" while the map held five
// (#1625 added 000149 and removed 000154/000156/000158/000159), making it the third hand-maintained
// count in this ticket to go stale — the same failure 000190's down was fixed for. State the
// membership PREDICATE, which cannot rot, and let a reader re-derive the roster from the map
// itself: an entry is a down migration this file's scanner finds destructive (DROP TABLE, DELETE
// FROM, or a DROP COLUMN of a lifecycle column) that carries no runtime
// `RAISE EXCEPTION ... refusing to roll back <N>`, and that a named round looked at and chose not
// to guard.
//
// ⚠ #1569 round 14 task 6, finding 5 — PRECISELY WHAT THIS KEY DOES AND DOES NOT COVER. The map is
// keyed by FILENAME ONLY, not by which tables/statements were found destructive in it. The
// staleness check in TestNoUndiscoveredDestructiveDownMigration (destructiveUnguardedByFile,
// below) is correspondingly by-file: it re-derives whether the NAMED FILE still scans as
// destructive-and-unguarded at all, and complains if it stops — but it does NOT re-check that the
// file still destroys the SAME tables this entry's reason was written about. Concretely: if
// 000007_create_accounts_table.down.sql (today: `DROP TABLE accounts CASCADE`, reasoned about
// exactly that) later ALSO gained an unrelated `DROP TABLE some_other_history_table`, this test
// would keep passing on the strength of the existing filename-only entry, and the entry's own
// reason text — written about `accounts` — would silently understate the file's real blast radius.
// Nothing here re-derives "does the reason's prose still match the tables found" the way G2's
// TestEveryAllowlistedHistoryDeleteStillCarriesItsPredicate re-derives a delete's SQL predicate
// against its allowlist fragment.
//
// This was deliberately NOT tightened to key by file+tables (e.g. "file|sorted,table,list") in this
// round: migration files are immutable by convention in this repo (a shipped migration is not
// edited after the fact — a schema change ships as a NEW migration), so the scenario above requires
// someone to violate that convention on a file this map already flags. Widening the key
// right after fixing a scanner that has already blinded itself once this task (see
// blankSQLCommentsAndLiterals's doc comment) trades a low-probability, convention-guarded gap for
// real new surface area in the very mechanism just repaired. If migration immutability ever stops
// being reliably true in this repo, key by file+tables instead of adding more prose here.
//
// ⚠ WHY THESE ARE NOT SIMPLY GIVEN A RUNTIME GUARD IN THIS ROUND, THE WAY 000035 WAS IN ROUND 13.
// 000035/000150/000151's guards protect a NARROWING migration mid-table-lifetime: the table already
// existed, held real rows, and the down migration's destructive act is a SIDE EFFECT of reversing an
// unrelated schema tightening — nothing about "undo this constraint change" obviously implies
// "and also destroy the table". 000007/000010/000022 are different in kind: they are the migrations
// that FIRST CREATE accounts, transactions and the budget tables respectively. Rolling one of them
// back means undoing the existence of the feature, and by the time such a rollback reaches this
// migration in a `down -all` sequence, every later migration that depends on the table has already
// been rolled back too — there is no partial, guarded way to "un-create a table" that leaves its
// rows anywhere to refuse over. A runtime refusal here would either (a) make
// scripts/test-migrations.sh's full down-then-up rollback suite permanently red on a database that
// has ever held a single account, or (b) have to special-case "empty" the same way 000150/000151
// already do — which is a real option, just one this task is not the right place to decide
// unilaterally for three foundational migrations across the whole schema. Flagged, not fixed;
// see the round 14 task 5 report for the explicit call-out.
var deliberatelyUnguardedDestructiveDownMigrations = map[string]deliberatelyUnguardedDestructiveDownMigration{
	"000007_create_accounts_table.down.sql": {
		reason: "unconditional `DROP TABLE accounts CASCADE`, comment-only warning, no runtime " +
			"refusal. Foundational CREATE-TABLE migration (accounts) rather than a mid-lifetime " +
			"tightening: undoing the migration that created the feature necessarily destroys the " +
			"feature's data, and every later migration referencing accounts has already been rolled " +
			"back by the time a `down -all` reaches this one. Flagged 2026-08-29 (#1569 round 14 " +
			"finding 3) for an explicit human decision — NOT endorsed as safe by this entry's " +
			"existence.",
	},
	"000010_create_transactions_table.down.sql": {
		reason: "unconditional `DROP TABLE transactions CASCADE`, comment-only warning, no runtime " +
			"refusal. Same shape and same reasoning as 000007: foundational CREATE-TABLE migration " +
			"for transactions, not a mid-lifetime tightening. Flagged 2026-08-29 (#1569 round 14 " +
			"finding 3) for an explicit human decision — NOT endorsed as safe by this entry's " +
			"existence.",
	},
	"000022_create_budgets_and_savings_goals.down.sql": {
		reason: "unconditional `DROP TABLE savings_goals/budget_categories/budgets CASCADE`, " +
			"comment-only intent (no WARNING comment at all, unlike 000007/000010), no runtime " +
			"refusal. Same foundational-migration reasoning as 000007/000010, times three tables in " +
			"one file. Flagged 2026-08-29 (#1569 round 14 finding 3) for an explicit human decision " +
			"— NOT endorsed as safe by this entry's existence.",
	},
	// ── The DROP COLUMN shape (#1625 final review B3) discovered five files on its first run.
	// Four of them were guarded by #1625 and are gone from this map; only 000149 remains. ──
	//
	// ⚠ This shape is RESURRECTION, not destruction: every row survives the rollback physically,
	// but the column that says which universe it belongs to does not, so retained rows silently
	// become live again and start counting. #1569 round 12 task 4 looked at 000158/000159 under
	// exactly this lens, decided to DOCUMENT rather than enforce, and asked a future round for
	// "its own mechanism (e.g. detecting whether any row has deleted_at IS NOT NULL and refusing
	// on THAT basis)". This scan was that detector; #1625 then built the refusals it asked for, so
	// 000154/000156/000158/000159 now sit in the `guarded` sample instead of here.
	//
	// 000149 is the one file this shape found that needs no refusal, which is why it alone is
	// still an exception. Do not "finish the job" by adding one to it without first re-running
	// TestDownMigration000149_AbortsBeforeItCanDropRemovedAt_Integration: its DROP COLUMN is
	// already unreachable whenever there is anything to lose.
	"000149_add_accounts_removed_status.down.sql": {
		reason: "drops accounts.removed_at with no runtime refusal — but the DROP COLUMN is " +
			"UNREACHABLE whenever there is anything to lose, and THIS ENTRY SAID THE OPPOSITE " +
			"until #1625 second fix wave M5 re-derived it. The retracted claim was 'without it a " +
			"removed account is indistinguishable from a live one and its balance re-enters every " +
			"current-state total'. Re-derivation, from the file's own bytes: Step 2 re-adds " +
			"accounts_valid_status as CHECK (status IN ('active','replaced','expired')), and " +
			"Postgres VALIDATES a newly added CHECK against existing rows, so the rollback ABORTS " +
			"on the first account with status='removed' — before Step 3's DROP COLUMN is reached " +
			"at all. 000149's own Step 3 constraint (accounts_removed_at_consistency) makes " +
			"status='removed' imply removed_at IS NOT NULL, so any account that would lose a " +
			"meaningful removed_at is exactly an account that trips the CHECK first. The real " +
			"hazard is therefore a HARD FAILURE partway through a `down -all`, reported as a " +
			"constraint violation that says nothing about retention — not silent resurrection. " +
			"Recorded rather than guarded because the abort already IS a refusal, just an " +
			"unhelpfully worded one; a `RAISE EXCEPTION` here would only improve the message. " +
			"Re-derived 2026-09-09 (#1625 second fix wave M5) and EXECUTED by " +
			"TestDownMigration000149_AbortsBeforeItCanDropRemovedAt_Integration — NOT endorsed as " +
			"safe.",
	},
	"000018_add_transaction_categories_and_tags.down.sql": {
		reason: "unconditional `DROP TABLE custom_categories CASCADE`, no runtime refusal, no " +
			"warning comment at all. Same foundational-migration reasoning as 000007/000010/000022: " +
			"this is the migration that FIRST CREATES custom_categories (alongside user_tags, not " +
			"history-bearing), not a mid-lifetime tightening. Found by this discovery test itself on " +
			"first run — it was NOT in the fixed roster and nobody had looked at it under this lens " +
			"before. Flagged 2026-08-29 (#1569 round 14 finding 3) for an explicit human decision — " +
			"NOT endorsed as safe by this entry's existence.",
	},
}

// scanDownMigrationDestruction reads every `.down.sql` under backend/migrations and reports, per
// file, which history-bearing tables it destroys (DROP TABLE, an ON DELETE CASCADE re-arm, or a
// DROP COLUMN of a lifecycleStateColumns column) and whether the file's own text refuses for
// itself.
func scanDownMigrationDestruction(t *testing.T) []downMigrationDestruction {
	t.Helper()
	dir := filepath.Join(backendDirRoot(t), "migrations")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("reading %s: %v", dir, err)
	}

	var out []downMigrationDestruction
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".down.sql") {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			t.Fatalf("reading %s: %v", e.Name(), err)
		}
		text := string(raw)

		tables := destructiveHistoryTables(text)
		if len(tables) == 0 {
			continue
		}

		out = append(out, downMigrationDestruction{
			file:    e.Name(),
			tables:  tables,
			guarded: downMigrationGuardsItself(text, e.Name()),
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].file < out[j].file })
	return out
}

// destructiveHistoryTables returns every history-bearing table a down migration's text either DROPs
// or re-arms an ON DELETE CASCADE onto, scanning comment-and-literal-blanked text (#1569 round 14
// finding 1's scanner) so that a comment or string merely mentioning either shape is not mistaken
// for one.
func destructiveHistoryTables(text string) []string {
	blanked := blankSQLCommentsAndLiterals(text)
	found := map[string]bool{}

	for _, m := range dropTableRefRegexp.FindAllStringSubmatch(blanked, -1) {
		tbl := strings.ToLower(m[1])
		if _, ok := historyBearingTables[tbl]; ok {
			found[tbl] = true
		}
	}

	for _, stmt := range splitSQLStatements(blanked) {
		m := alterTableTargetRegexp.FindStringSubmatch(stmt)
		if m == nil {
			continue
		}
		tbl := strings.ToLower(m[1])
		if _, ok := historyBearingTables[tbl]; !ok {
			continue
		}
		if onDeleteCascadeRegexp.MatchString(stmt) {
			found[tbl] = true
			continue
		}
		for _, dc := range dropColumnRegexp.FindAllStringSubmatch(stmt, -1) {
			if _, ok := lifecycleStateColumns[strings.ToLower(dc[1])]; ok {
				found[tbl] = true
				break
			}
		}
	}

	out := make([]string, 0, len(found))
	for tbl := range found {
		out = append(out, tbl)
	}
	sort.Strings(out)
	return out
}

// splitSQLStatements splits already comment-and-literal-blanked SQL on `;`. It is not statement
// boundary detection in the sense statementBoundaryRegexp is (history_delete_guard_test.go) — this
// guard only needs "does this ALTER TABLE's own statement carry ON DELETE CASCADE", and blanked
// text has no live semicolons inside comments or literals left to confuse a plain split.
func splitSQLStatements(blanked string) []string {
	return strings.Split(blanked, ";")
}

// downMigrationGuardsItself reports whether a down migration's OWN text carries a runtime refusal
// naming ITS OWN migration number — the pattern every currently-guarded migration in the tree
// actually uses (`RAISE EXCEPTION ... 'refusing to roll back <NNNNNN>...'`). Comments are stripped
// first so a comment merely mentioning the phrase cannot satisfy this — but literals are NOT
// blanked here, unlike destructiveHistoryTables' scan, because the evidence this function is
// looking for IS the content of a string literal (the RAISE EXCEPTION message itself); blanking
// literals would blank away the only thing being checked for.
func downMigrationGuardsItself(text, filename string) bool {
	m := migrationNumberRegexp.FindStringSubmatch(filename)
	if m == nil {
		return false
	}
	number := m[1]

	stripped := sqlBlockCommentRegexp.ReplaceAllString(text, " ")
	stripped = sqlLineCommentRegexp.ReplaceAllString(stripped, " ")

	needle := "refusing to roll back " + number
	return strings.Contains(strings.ToLower(stripped), strings.ToLower(needle))
}

// TestNoUndiscoveredDestructiveDownMigration is #1569 round 14 finding 3: every destructive down
// migration is either guarded by its own text or explicitly, checkedly excepted — never simply
// absent from a list, because there is no list to be absent from.
func TestNoUndiscoveredDestructiveDownMigration(t *testing.T) {
	found := scanDownMigrationDestruction(t)

	// Fail closed: the tree is known (as of this round) to contain destructive down migrations, so
	// finding none means the walk or the regexes broke.
	if len(found) == 0 {
		t.Fatalf("scanned backend/migrations and found no destructive down migration at all, but " +
			"the tree is known to contain several (000016, 000151, 000160, ...) — the scan is " +
			"broken and would pass vacuously")
	}

	matchedException := map[string]bool{}
	var unaccounted []string
	for _, d := range found {
		if d.guarded {
			continue
		}
		if _, ok := deliberatelyUnguardedDestructiveDownMigrations[d.file]; ok {
			matchedException[d.file] = true
			continue
		}
		unaccounted = append(unaccounted,
			d.file+" -> "+strings.Join(d.tables, ", ")+" (no `refusing to roll back` text of its own, "+
				"and not in deliberatelyUnguardedDestructiveDownMigrations)")
	}
	sort.Strings(unaccounted)

	var staleExceptions []string
	destructiveUnguardedByFile := map[string]bool{}
	for _, d := range found {
		if !d.guarded {
			destructiveUnguardedByFile[d.file] = true
		}
	}
	for file := range deliberatelyUnguardedDestructiveDownMigrations {
		if !destructiveUnguardedByFile[file] {
			staleExceptions = append(staleExceptions, file)
		}
	}
	sort.Strings(staleExceptions)

	if len(unaccounted) > 0 {
		t.Errorf("%d destructive down migration(s) are neither guarded nor an explicit, checked "+
			"exception (#1569 round 14 finding 3).\n"+
			"A down migration that DROPs a history-bearing table, re-arms ON DELETE CASCADE onto "+
			"one, or drops the lifecycle column that says which universe its retained rows belong "+
			"to, destroys or resurrects data a live system may since have created. Either give the migration a "+
			"runtime refusal that names its own number (`RAISE EXCEPTION '... refusing to roll back "+
			"<NNNNNN>: ...'`, conditioned on the rows actually existing — see 000151.down.sql for the "+
			"pattern), or add it to deliberatelyUnguardedDestructiveDownMigrations with a reason you "+
			"have derived yourself.\n    %s", len(unaccounted), strings.Join(unaccounted, "\n    "))
	}
	if len(staleExceptions) > 0 {
		t.Errorf("%d entries in deliberatelyUnguardedDestructiveDownMigrations no longer match a "+
			"destructive, unguarded down migration (#1569 round 14 finding 3).\n"+
			"Either the file gained a runtime refusal (good — delete the entry) or it stopped being "+
			"destructive by this scan's definition (also delete it) or the filename changed (fix the "+
			"key). A stale entry reads as a standing, reviewed exception for a file that no longer "+
			"needs one:\n    %s", len(staleExceptions), strings.Join(staleExceptions, "\n    "))
	}

	for file, exc := range deliberatelyUnguardedDestructiveDownMigrations {
		if strings.TrimSpace(exc.reason) == "" {
			t.Errorf("deliberatelyUnguardedDestructiveDownMigrations[%q] has no reason; the reason "+
				"is the point", file)
		}
	}
}

// ---- Meta-tests: the constructed shapes, and the legitimate cases that must NOT fire. -----------

// TestDownMigrationDestructionDiscovery_SeesTheShapesThatMatter runs the classifier against
// constructed fixtures rather than the tree, so each shape is exercised whether or not the tree
// happens to contain it today.
func TestDownMigrationDestructionDiscovery_SeesTheShapesThatMatter(t *testing.T) {
	cases := []struct {
		name       string
		sql        string
		wantTables []string
	}{
		{
			name:       "an unconditional DROP TABLE of a history-bearing table is seen",
			sql:        "DROP TABLE IF EXISTS transactions CASCADE;",
			wantTables: []string{"transactions"},
		},
		{
			name:       "DROP TABLE of a non-history table is not this guard's business",
			sql:        "DROP TABLE IF EXISTS notifications CASCADE;",
			wantTables: nil,
		},
		{
			name: "an ALTER TABLE re-arming ON DELETE CASCADE onto a history table is seen",
			sql: "ALTER TABLE savings_goals ADD CONSTRAINT fk_x FOREIGN KEY (account_id) " +
				"REFERENCES accounts(id) ON DELETE CASCADE;",
			wantTables: []string{"savings_goals"},
		},
		{
			name: "ON DELETE CASCADE on a non-history table's own ALTER is not this guard's business",
			sql: "ALTER TABLE notifications ADD CONSTRAINT fk_x FOREIGN KEY (user_id) " +
				"REFERENCES users(id) ON DELETE CASCADE;",
			wantTables: nil,
		},
		{
			name:       "dropping a LIFECYCLE column from a history table is seen (#1625 B3)",
			sql:        "ALTER TABLE savings_goal_contributions DROP COLUMN IF EXISTS archived_at;",
			wantTables: []string{"savings_goal_contributions"},
		},
		{
			name: "dropping an ORDINARY column from a history table is NOT this guard's business — " +
				"widening to any DROP COLUMN would flag every ADD COLUMN's rollback in the tree",
			sql:        "ALTER TABLE transactions DROP COLUMN IF EXISTS pending_transaction_id;",
			wantTables: nil,
		},
		{
			name:       "dropping a lifecycle column from a NON-history table is not this guard's business",
			sql:        "ALTER TABLE notifications DROP COLUMN IF EXISTS deleted_at;",
			wantTables: nil,
		},
		{
			name: "a multi-column DROP is seen when ANY of the dropped columns is a lifecycle one",
			sql: "ALTER TABLE transactions DROP COLUMN IF EXISTS deleted_by, " +
				"DROP COLUMN IF EXISTS deleted_at;",
			wantTables: []string{"transactions"},
		},
		{
			name:       "a DROP COLUMN spelled only in a comment is not a DROP COLUMN",
			sql:        "-- we used to ALTER TABLE transactions DROP COLUMN deleted_at here\nSELECT 1;",
			wantTables: nil,
		},
		{
			name:       "a DROP TABLE mentioned only in a comment is not a DROP TABLE",
			sql:        "-- old note: we used to DROP TABLE transactions here\nSELECT 1;",
			wantTables: nil,
		},
		{
			name:       "a DROP TABLE spelled inside a string literal is not a DROP TABLE",
			sql:        "SELECT 'DROP TABLE transactions CASCADE';",
			wantTables: nil,
		},
		{
			name: "ON DELETE CASCADE in a LATER, unrelated statement does not lend one to an " +
				"earlier ALTER TABLE on a history table with no cascade of its own",
			sql: "ALTER TABLE savings_goals ADD CONSTRAINT fk_x FOREIGN KEY (account_id) " +
				"REFERENCES accounts(id) ON DELETE SET NULL;\n" +
				"ALTER TABLE notifications ADD CONSTRAINT fk_y FOREIGN KEY (user_id) " +
				"REFERENCES users(id) ON DELETE CASCADE;",
			wantTables: nil,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := destructiveHistoryTables(tc.sql)
			if len(got) != len(tc.wantTables) {
				t.Fatalf("destructiveHistoryTables(%q) = %v, want %v", tc.sql, got, tc.wantTables)
			}
			for i := range got {
				if got[i] != tc.wantTables[i] {
					t.Fatalf("destructiveHistoryTables(%q) = %v, want %v", tc.sql, got, tc.wantTables)
				}
			}
		})
	}
}

// TestDownMigrationDestructionDiscovery_GuardTextMustNameItsOwnNumber pins the checked half of the
// "guarded" classification: a RAISE EXCEPTION mentioning some OTHER migration's number (a copy-paste
// of another file's refusal that was never updated) must not count as this file guarding itself.
func TestDownMigrationDestructionDiscovery_GuardTextMustNameItsOwnNumber(t *testing.T) {
	text := "DO $$ BEGIN RAISE EXCEPTION 'refusing to roll back 000151: has rows'; END $$;\n" +
		"DROP TABLE IF EXISTS transactions CASCADE;\n"

	if downMigrationGuardsItself(text, "000010_create_transactions_table.down.sql") {
		t.Fatal("a refusal message naming a DIFFERENT migration's number must not count as " +
			"000010 guarding itself")
	}
	if !downMigrationGuardsItself(text, "000151_create_account_status_history.down.sql") {
		t.Fatal("the refusal message names 000151 — it must be recognised for that file")
	}
}

// TestDownMigrationDestructionDiscovery_CommentMentionIsNotAGuard pins the other direction: a
// comment merely mentioning the refusal phrase (rather than a real RAISE EXCEPTION) must not count.
func TestDownMigrationDestructionDiscovery_CommentMentionIsNotAGuard(t *testing.T) {
	text := "-- TODO: consider refusing to roll back 000010 if rows exist\n" +
		"DROP TABLE IF EXISTS transactions CASCADE;\n"
	if downMigrationGuardsItself(text, "000010_create_transactions_table.down.sql") {
		t.Fatal("a comment mentioning the phrase is not a runtime guard and must not count as one")
	}
}

// TestDownMigrationDestructionDiscovery_RealTreeSample is a light integration-free sanity check
// against the ACTUAL migrations tree: every migration confirmed as guarded (by hand, via grep,
// before this scanner existed) must still be classified guarded, and every migration recorded as
// unguarded must still be classified unguarded. If either flips, re-derive before trusting the
// scan.
//
// ⚠ #1625 second fix wave M5: this said "the three this round found unguarded" after B3 brought
// the roster to nine. The number is no longer written down at all — the hand-derived `unguarded`
// slice below is now cross-checked against deliberatelyUnguardedDestructiveDownMigrations's keys,
// so the two lists cannot silently diverge and neither can a sentence counting them.
func TestDownMigrationDestructionDiscovery_RealTreeSample(t *testing.T) {
	found := scanDownMigrationDestruction(t)
	byFile := map[string]downMigrationDestruction{}
	for _, d := range found {
		byFile[d.file] = d
	}

	guarded := []string{
		"000016_create_balance_snapshots_table.down.sql",
		"000020_create_categorization_rules.down.sql",
		"000027_create_rollover_history_table.down.sql",
		"000035_change_cascade_to_set_null.down.sql",
		"000051_create_investment_tables.down.sql",
		"000096_create_portfolio_snapshots.down.sql",
		"000097_savings_progress_and_budget_linkage.down.sql",
		"000150_preserve_history_on_account_delete.down.sql",
		"000151_create_account_status_history.down.sql",
		"000157_rule_execution_history_transaction_set_null.down.sql",
		"000160_create_holding_and_liability_history.down.sql",
		"000163_portfolio_snapshot_repair_ledger.down.sql",
		"000164_audit_trail_survives_definition_purge.down.sql",
		"000169_record_the_prior_annotation.down.sql",
		// #1625 final review B3: both are the DROP COLUMN shape on savings_goal_contributions, and
		// both refuse. 000190 did NOT until B3 — it dropped archived_at unconditionally while
		// asserting in a comment that the resulting double-count was "not new damage".
		"000180_tombstone_savings_goal_contributions.down.sql",
		"000190_add_contribution_archived_at.down.sql",
		// #1625: the four lifecycle-column DROP COLUMN downs that shipped with prose instead of a
		// control. Each now refuses on the predicate its own header describes, and each is
		// exercised both ways by downGuardCases in
		// down_migration_refusal_integration_test.go. The reachability of the loss they prevent
		// was established by a real sequential `migrate down` on a rebuilt scratch database, not
		// by rereading the SQL — see each file's own header for the command and its output.
		"000154_add_accounts_removed_reason.down.sql",
		"000156_add_transactions_tombstone.down.sql",
		"000158_soft_delete_config_parents.down.sql",
		"000159_soft_delete_custom_categories.down.sql",
	}
	for _, f := range guarded {
		d, ok := byFile[f]
		if !ok {
			t.Errorf("%s: expected to be classified destructive, but the scan found nothing there "+
				"at all", f)
			continue
		}
		if !d.guarded {
			t.Errorf("%s: expected guarded=true (a known `refusing to roll back` runtime check), "+
				"got false", f)
		}
	}

	unguarded := []string{
		"000007_create_accounts_table.down.sql",
		"000010_create_transactions_table.down.sql",
		"000018_add_transaction_categories_and_tags.down.sql",
		"000022_create_budgets_and_savings_goals.down.sql",
		// The only lifecycle-column DROP COLUMN left unguarded. 000154/000156/000158/000159 were
		// here too until #1625 gave each a runtime refusal; 000149 stays because it does not need
		// one — its own Step 2 re-adds a status CHECK that Postgres validates against existing
		// rows, so a removed account aborts the rollback before Step 3's DROP COLUMN is reached.
		// Pinned by TestDownMigration000149_AbortsBeforeItCanDropRemovedAt_Integration.
		"000149_add_accounts_removed_status.down.sql",
	}
	// The hand-derived sample and the checked-exception map must be the SAME set. Two
	// independently maintained lists of the same thing is how the previous docstring came to
	// count three of nine.
	if len(unguarded) != len(deliberatelyUnguardedDestructiveDownMigrations) {
		t.Errorf("the hand-derived unguarded sample has %d entries but "+
			"deliberatelyUnguardedDestructiveDownMigrations has %d — one of them was updated "+
			"without the other", len(unguarded), len(deliberatelyUnguardedDestructiveDownMigrations))
	}
	for _, f := range unguarded {
		if _, ok := deliberatelyUnguardedDestructiveDownMigrations[f]; !ok {
			t.Errorf("%s is in the hand-derived unguarded sample but has no checked-exception "+
				"entry", f)
		}
	}
	for f := range deliberatelyUnguardedDestructiveDownMigrations {
		found := false
		for _, g := range unguarded {
			if g == f {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("%s has a checked-exception entry but is missing from the hand-derived "+
				"unguarded sample", f)
		}
	}

	for _, f := range unguarded {
		d, ok := byFile[f]
		if !ok {
			t.Errorf("%s: expected to be classified destructive, but the scan found nothing there "+
				"at all", f)
			continue
		}
		if d.guarded {
			t.Errorf("%s: expected guarded=false (no runtime refusal today) — if this now fails, "+
				"someone added one; update deliberatelyUnguardedDestructiveDownMigrations rather "+
				"than this test", f)
		}
	}
}

