package repository

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// Issue #1569 Task 19 — Guard G2: every hard DELETE of history is on a list, with a reason.
//
// G1 stops the SCHEMA from destroying history behind a parent delete. G2 stops the CODE from doing
// it in the open. They are different failure modes and neither implies the other: a `DELETE FROM
// holding_snapshots` in Go needs no foreign key at all.
//
// THE RULE. In non-test Go under backend/internal, every `DELETE FROM <history-bearing table>` and
// every `TRUNCATE <history-bearing table>` must appear in `allowedHistoryDeletes`, keyed by the
// function that issues it, with the reason. There is deliberately NO in-comment escape hatch of the
// kind the tombstone READ guards use. A read that forgets a filter is a slip and there are ~60 of
// them; a hard delete of financial history is a decision, there are five of them, and every one
// should be visible in a single list that a reviewer can read in ten seconds.
//
// THE GAP IN THE FUNCTION KEY, precisely. The key is (function, TABLE), so a second delete of a
// DIFFERENT history table inside an allowlisted function IS caught — that is a new key. What used
// to slip through was widening what an existing, already-blessed purge destroys: a second delete of
// the SAME table in the same function, or — far easier to do by accident — dropping a leg from the
// predicate of the delete that is already there. Since #1569 T7.5 each entry names the SQL
// fragments it blesses (see historyDeleteAllowance), and
// TestEveryAllowlistedHistoryDeleteStillCarriesItsPredicate requires EVERY statement under a key to
// carry all of them — so both shapes now fail here, with no database attached. For PurgeAccount the
// blast radius is additionally covered behaviourally, by
// TestHistoryRetentionRoundTrip_PurgeDestroysOnlyWhatItNames, which seeds a sibling institution and
// asserts the purge does not touch it.
//
// SCOPE: backend/internal only. A delete written in cmd/ would not be seen; today the only match
// there is in a test file, so there is no live gap, but the boundary is a walk of repoInternalRoot
// and not a decision that cmd/ is safe.
//
// KEYED BY FUNCTION, NOT BY LINE. Line numbers rot — this plan re-pinned one twice — and a
// file-level key would let a second, unrelated delete slip into an already-listed file. The
// function name is also the only part of the key that a reader can evaluate: `PurgeAccount` and
// `DeleteManagedForTransaction` say what they are; `account_repository.go:1031` does not.
//
// WHAT IS NOT HERE. Tombstoning (`UPDATE ... SET deleted_at = now()`) is the normal path and is not
// a delete. `DELETE FROM` a non-history table is none of this guard's business. And migrations are
// out of scope: a migration that drops history is caught by review and by G1's schema check, not by
// a scan of Go source.
//
// Mutation-checked, twice over: the constructed statement in
// TestHistoryDeleteGuard_FlagsAnUnallowlistedDelete, and a real one — a `DELETE FROM
// rule_execution_history` temporarily added to a live repository method, which turns
// TestNoUnallowlistedHistoryDelete red naming that method. See task-19-report.md.

// historyDeleteAllowance is one blessing: WHY this function may hard-delete this history table,
// and WHICH PREDICATE the blessing is scoped to.
//
// ⚠ WHY THE PREDICATE IS PART OF THE ENTRY (#1569 T7.5, user ruling). Until this change an entry
// was a bare reason string keyed by (function, TABLE), so dropping `AND deleted_at IS NOT NULL`
// from PurgeAccount's savings_goals delete — or `AND c.managed_by_reconciliation = true` from
// DeleteManagedForTransaction — kept the existing blessing while widening the destruction. The
// scopes were held only by behavioural integration tests, and those SKIP without DATABASE_URL, so
// a plain `go test ./...` proved nothing about them. `predicate` closes that: an entry cannot
// survive the SQL beneath it widening, because the fragments it names must still be there.
//
// WHAT `predicate` CLAIMS, PRECISELY — and what it does not. Every fragment must appear, as a
// substring, in the normalised text of EVERY statement the key matches. Normalisation strips SQL
// comments (`--` and `/* */`) and collapses runs of whitespace, then upper-cases, so re-wrapping a
// query, re-indenting it, or writing a comment inside it cannot flip a verdict. It does NOT parse
// SQL and it does not know what the predicate MEANS: it can only tell you the text is still there.
// Two consequences worth stating out loud:
//
//   - Polarity must be named. `deleted_at IS NULL` flipped to `IS NOT NULL` is caught only because
//     the fragment spells out which one it blesses. Write the whole leg, never just the column.
//   - A fragment is not a proof of narrowness. `WHERE c.id = $1` can still sit next to a new `OR`.
//     The behavioural integration tests remain the authority on what the statement DOES; this is
//     the cheap, always-runs half that stops a leg from silently disappearing.
type historyDeleteAllowance struct {
	// reason is why this destruction is allowed at all. It describes intent and is NOT checked
	// against the SQL — only `predicate` is.
	reason string
	// predicate is the set of SQL fragments this blessing is scoped to. Must be non-empty; an
	// entry with no fragments would be a blessing of `DELETE FROM <table>`, full stop.
	predicate []string
}

// allowedHistoryDeletes maps "<path under internal/>#<function>|<table>" to the blessing. One entry
// per (function, table) pair.
var allowedHistoryDeletes = map[string]historyDeleteAllowance{
	// The account purge. `PurgeAccount` refuses to run on a live account and writes an audit row in
	// the same transaction; it is the user explicitly asking for one account's data to be destroyed.
	// The three cascade edges in allowedHistoryCascades are the schema half of the same decision.
	"repository/account_repository.go#PurgeAccount|balance_snapshots": {
		reason:    "explicit account purge: balance_snapshots has ON DELETE SET NULL from accounts, so the cascade would orphan rather than remove them",
		predicate: []string{"WHERE account_id = $1"},
	},
	"repository/account_repository.go#PurgeAccount|accounts": {
		reason:    "explicit account purge: this IS the purge",
		predicate: []string{"WHERE id = $1 AND user_id = $2"},
	},
	// Scoped to `deleted_at IS NOT NULL` — the user has ALREADY deleted the goal, which is exactly
	// PurgeGoal's own precondition. A live goal is not matched and still blocks the purge via
	// migration 000150's RESTRICT. Without this the purge was permanently unreachable for any
	// account that had ever had a goal: the tombstone leaves account_id set, so "delete the goal
	// first, then try again" could never be satisfied (#1569 task 21, defect C1).
	//
	// That scope is now pinned in `predicate` as well as by behaviour. The behavioural pair is
	// still the authority on what it MEANS:
	// TestAccountRepository_PurgeAccount_Integration_TombstonedSavingsGoalDoesNotBlock (a deleted
	// goal must not block) and ..._RefusesWhileSavingsGoalReferencesAccount (a live one must).
	// Inverting the predicate turns those red — and, since #1569 T7.5, turns
	// TestEveryAllowlistedHistoryDeleteStillCarriesItsPredicate red without a database.
	"repository/account_repository.go#PurgeAccount|savings_goals": {
		reason:    "explicit account purge, tombstoned goals only: the user already deleted the goal, which is PurgeGoal's precondition; a live goal still blocks via RESTRICT",
		predicate: []string{"account_id = $1", "user_id = $2", "deleted_at IS NOT NULL"},
	},

	// The linked-item purge. Same shape, one level up: the user disconnects AND purges an
	// institution. A plain disconnect soft-removes and keeps everything.
	"repository/linked_item_repository.go#PurgeLinkedItem|balance_snapshots": {
		reason:    "explicit institution purge, same reason as PurgeAccount",
		predicate: []string{"WHERE account_id IN (SELECT id FROM accounts WHERE linked_item_id = $1)"},
	},
	"repository/linked_item_repository.go#PurgeLinkedItem|savings_goals": {
		reason:    "explicit institution purge, tombstoned goals only: same scope and same reason as PurgeAccount's entry above",
		predicate: []string{"user_id = $2", "deleted_at IS NOT NULL", "account_id IN (SELECT id FROM accounts WHERE linked_item_id = $1)"},
	},

	// The savings-goal purge. Distinct from the repository's `Delete`
	// (savings_goal_repository.go:405), which TOMBSTONES (#1569 task 8). ⚠ `DeleteGoal` is the
	// SERVICE verb (services/savings_goal_service.go:499); there is no repository method by that
	// name, and this reason used to claim there was.
	//
	// The `deleted_at IS NOT NULL` precondition is deliberately NOT in this statement — the
	// SELECT ... FOR UPDATE above it already asserted it and refused a live goal, and re-stating it
	// here would only let the two predicates disagree. So `predicate` pins the ownership legs only;
	// the precondition is guarded where it lives.
	"repository/savings_goal_repository.go#PurgeGoal|savings_goals": {
		reason:    "explicit goal purge; the ordinary Delete path on this repository (service verb: DeleteGoal) tombstones instead",
		predicate: []string{"WHERE id = $1 AND user_id = $2"},
	},

	// Contributions.
	// ⚠ RE-DERIVED (#1569 T7.5). This reason used to say "one contribution THEY ENTERED", which is
	// the same true-of-most-rows-asserted-of-all error the ⚠ paragraph below retracts for the
	// sibling entry. The predicate (savings_contribution_repository.go:749-766) keys on `c.id`
	// alone — no `source` leg, no `managed_by_reconciliation` leg — so a row the RECONCILER created
	// is reachable by it too. "One contribution" and "explicit request" survive; "they entered"
	// does not.
	"repository/savings_contribution_repository.go#DeleteContribution|savings_goal_contributions": {
		reason:    "the user deleting one named contribution on a live goal of theirs — the narrowest possible explicit request. NOT necessarily a row they entered: the predicate keys on c.id alone, so a reconciler-created row is reachable by it",
		predicate: []string{"c.id = $1", "c.savings_goal_id = g.id", "g.user_id = $2", "g.deleted_at IS NULL"},
	},
	// ⚠ RE-DERIVED in the #1569 whole-branch review (C1). The reason this entry USED to carry —
	// "derived, regenerable, and not a record of anything the user did" — was FALSE, and false in
	// the same way as four other reasons on this branch: true of MOST rows the predicate matches,
	// asserted of ALL of them. UpsertFromTransaction INSERTS status='confirmed' with
	// managed_by_reconciliation=true (source='boost_match', from
	// transferCorrelationService.createBoostMatchContribution), and its ON CONFLICT DO UPDATE
	// promotes a proposed row to confirmed without clearing the flag. Such a row IS a record of the
	// user's money moving, and it is not regenerable once the boost that matched it is gone. Do not
	// re-derive this reason from ConfirmContribution alone; enumerate all four writers of
	// status='confirmed' (CreateContribution, UpsertFromTransaction, InsertManualFromTransaction,
	// ConfirmContribution).
	//
	// What actually justifies the delete is the LIVE-goal scope the predicate now carries. A live
	// contributions-basis goal recomputes progress from these rows, so a row derived from a
	// transaction the bank has withdrawn would keep counting money that is no longer there —
	// phantom progress, Codex C4. A TOMBSTONED goal recomputes nothing; its rows are only a ledger,
	// and `g.deleted_at IS NULL` now excludes them (#1569 C1,
	// TestDeleteManagedForTransaction_DeletedGoalLedgerSurvives_Integration). Hand-logged rows
	// (managed_by_reconciliation = false) never matched and still do not.
	//
	// Both of those legs are pinned in `predicate` since #1569 T7.5, so dropping either one fails
	// TestEveryAllowlistedHistoryDeleteStillCarriesItsPredicate with no database attached. What the
	// legs MEAN is still held by that integration test, in both directions.
	"repository/savings_contribution_repository.go#DeleteManagedForTransaction|savings_goal_contributions": {
		reason:    "reconciler-owned rows (managed_by_reconciliation = true) for a transaction being removed, scoped to a LIVE goal: leaving them would show phantom progress against money the bank withdrew. A tombstoned goal's ledger is excluded, and hand-logged rows never match. See the re-derivation above — the previous reason was false.",
		predicate: []string{"c.savings_goal_id = g.id", "c.transaction_id = $1", "c.managed_by_reconciliation = true", "g.deleted_at IS NULL"},
	},
}

// historyDeleteRefRegexp matches a hard delete of one of the named tables. Alternatives are sorted
// longest-first so that `savings_goals` cannot claim the prefix of `savings_goal_contributions`.
func historyDeleteRefRegexp(tables map[string]string) *regexp.Regexp {
	names := make([]string, 0, len(tables))
	for t := range tables {
		names = append(names, t)
	}
	sort.Slice(names, func(i, j int) bool {
		if len(names[i]) != len(names[j]) {
			return len(names[i]) > len(names[j])
		}
		return names[i] < names[j]
	})
	return regexp.MustCompile(`(?is)\b(?:DELETE\s+FROM|TRUNCATE(?:\s+TABLE)?)\s+(?:ONLY\s+)?(` +
		strings.Join(names, "|") + `)\b`)
}

// sqlBlockCommentRegexp, sqlLineCommentRegexp and sqlWhitespaceRegexp normalise SQL so that
// re-wrapping a query, re-indenting it, or writing a comment inside it cannot flip a predicate
// verdict. Comments are stripped BEFORE whitespace is collapsed, and stripped at all because four
// of the eight blessed statements carry a `-- tombstone-exempt(...)` comment that quotes the very
// predicate legs the guard is looking for — leaving them in would let a comment satisfy the check
// for SQL that no longer contains it.
var (
	sqlBlockCommentRegexp = regexp.MustCompile(`(?s)/\*.*?\*/`)
	sqlLineCommentRegexp  = regexp.MustCompile(`--[^\n]*`)
	sqlWhitespaceRegexp   = regexp.MustCompile(`\s+`)
	// anyDeleteRegexp is the STATEMENT BOUNDARY, not the table matcher: it matches a delete of any
	// table at all, so that a `DELETE FROM holdings` sitting between two history deletes still ends
	// the preceding slice.
	anyDeleteRegexp = regexp.MustCompile(`\b(?:DELETE FROM|TRUNCATE)\b`)
)

// normaliseSQL strips SQL comments, collapses whitespace and upper-cases.
func normaliseSQL(sql string) string {
	sql = sqlBlockCommentRegexp.ReplaceAllString(sql, " ")
	sql = sqlLineCommentRegexp.ReplaceAllString(sql, " ")
	sql = sqlWhitespaceRegexp.ReplaceAllString(sql, " ")
	return strings.ToUpper(strings.TrimSpace(sql))
}

// historyDeleteStatements returns, per history table, the normalised text of each hard delete of it
// found in sql. A "statement" runs from the DELETE/TRUNCATE keyword to the start of the next one
// (or to the end of the input) — deliberately generous at the tail, because over-reading can only
// make the predicate check MORE permissive, never produce a false failure on legitimate SQL.
func historyDeleteStatements(sql string, tables map[string]string) map[string][]string {
	norm := normaliseSQL(sql)
	out := map[string][]string{}
	for _, m := range historyDeleteRefRegexp(tables).FindAllStringSubmatchIndex(norm, -1) {
		table := strings.ToLower(norm[m[2]:m[3]])
		end := len(norm)
		if next := anyDeleteRegexp.FindStringIndex(norm[m[1]:]); next != nil {
			end = m[1] + next[0]
		}
		out[table] = append(out[table], norm[m[0]:end])
	}
	return out
}

// historyDeleteSites returns the history tables hard-deleted by one SQL string.
func historyDeleteSites(sql string, tables map[string]string) []string {
	var out []string
	for table := range historyDeleteStatements(sql, tables) {
		out = append(out, table)
	}
	sort.Strings(out)
	return out
}

// historyDeleteSiteFound is what the scan saw for one "<rel>#<func>|<table>" key: the line(s) the
// enclosing function starts on, and the normalised text of each delete statement.
type historyDeleteSiteFound struct {
	lines      []string
	statements []string
}

// scanHistoryDeletes walks non-test Go under root and returns every "<rel>#<func>|<table>" key found.
// Only string LITERALS are inspected, so prose in a comment that quotes `DELETE FROM budgets` — and
// four files do exactly that, explaining why they no longer do it — is correctly ignored.
func scanHistoryDeletes(t *testing.T, root string, tables map[string]string) map[string]*historyDeleteSiteFound {
	t.Helper()
	found := map[string]*historyDeleteSiteFound{}

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
		rel = filepath.ToSlash(rel)

		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Body == nil {
				continue
			}
			var parts []string
			ast.Inspect(fn.Body, func(n ast.Node) bool {
				lit, ok := n.(*ast.BasicLit)
				if !ok || lit.Kind != token.STRING {
					return true
				}
				if s, e := strconv.Unquote(lit.Value); e == nil {
					parts = append(parts, s)
				}
				return true
			})
			for tbl, stmts := range historyDeleteStatements(strings.Join(parts, "\n"), tables) {
				key := rel + "#" + fn.Name.Name + "|" + tbl
				if found[key] == nil {
					found[key] = &historyDeleteSiteFound{}
				}
				found[key].lines = append(found[key].lines, strconv.Itoa(fset.Position(fn.Pos()).Line))
				found[key].statements = append(found[key].statements, stmts...)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walking internal/: %v", err)
	}
	return found
}

// TestNoUnallowlistedHistoryDelete is guard G2 itself.
func TestNoUnallowlistedHistoryDelete(t *testing.T) {
	root := repoInternalRoot(t)
	found := scanHistoryDeletes(t, root, historyBearingTables)

	// Fail closed: the allowlist is non-empty, so a scan that finds nothing means the walk broke.
	if len(found) == 0 {
		t.Fatalf("scanned %s and found no history DELETE at all, but %d are allowlisted — the scan is "+
			"broken and would pass vacuously", root, len(allowedHistoryDeletes))
	}

	var unallowlisted, stale []string
	for key, site := range found {
		if _, ok := allowedHistoryDeletes[key]; !ok {
			unallowlisted = append(unallowlisted, key+" (func at line "+strings.Join(site.lines, ",")+")")
		}
	}
	for key := range allowedHistoryDeletes {
		if _, ok := found[key]; !ok {
			stale = append(stale, key)
		}
	}
	sort.Strings(unallowlisted)
	sort.Strings(stale)

	if len(unallowlisted) > 0 {
		t.Errorf("%d hard DELETE(s) of history-bearing tables are not allowlisted (#1569).\n"+
			"#1569 decided that financial history is retained unless the user explicitly asks for it "+
			"to go. If this delete IS that explicit request, add it to allowedHistoryDeletes with the "+
			"reason. If it is not, tombstone instead (UPDATE ... SET deleted_at = now()) or narrow the "+
			"predicate.\n"+
			"⚠ FOLLOW-THROUGH ON THE TOMBSTONE REMEDY, or you trade this failure for another one. "+
			"For a table one of the tombstone read-guards audits (transactions, savings_goals, "+
			"boosts, budgets, budget_categories, custom_categories) the tombstoning UPDATE is itself "+
			"a write target with no `deleted_at IS NULL` of its own — and it MUST NOT have one, since "+
			"adding it would change which rows the write affects. Taken literally this remedy clears "+
			"G2 and turns TestNoTransactionReadForgetsTheTombstoneFilter red. Carry the write-scoped "+
			"exemption with it:\n"+
			"    -- tombstone-exempt(write:<table>): tombstoning IS the write; narrowing it here "+
			"would change WHICH ROWS the statement affects\n"+
			"See transaction_repository.go's UpdateTags for the shape.\n    %s",
			len(unallowlisted), strings.Join(unallowlisted, "\n    "))
	}
	if len(stale) > 0 {
		t.Errorf("%d allowlisted history delete(s) no longer exist (#1569).\n"+
			"A stale entry reads as a standing permission to destroy this history. Delete it:\n    %s",
			len(stale), strings.Join(stale, "\n    "))
	}
}

// missingPredicateFragments returns the fragments of predicate that are NOT present in statement.
// statement is expected to be normalised already; each fragment is normalised here so that an entry
// can be written in ordinary, readable SQL casing and spacing.
func missingPredicateFragments(statement string, predicate []string) []string {
	var missing []string
	for _, frag := range predicate {
		if !strings.Contains(statement, normaliseSQL(frag)) {
			missing = append(missing, frag)
		}
	}
	return missing
}

// TestEveryAllowlistedHistoryDeleteStillCarriesItsPredicate is the second half of guard G2: an
// allowlist entry may not outlive the scope it was granted for.
//
// #1569 T7.5. Before this test, `allowedHistoryDeletes` blessed a (function, TABLE) pair with no
// predicate at all, so deleting `AND deleted_at IS NOT NULL` from PurgeAccount's savings_goals
// delete — one line — kept the blessing and turned a narrow cleanup into a hard delete of LIVE
// goals and their hand-logged contributions. Nothing in `go test ./...` noticed; the only thing
// holding those scopes was a `//go:build integration` test that SKIPS without DATABASE_URL.
//
// Read the failure message, not just the diff: a fragment can also go missing because the statement
// was legitimately rewritten. In that case update the entry's `predicate` — deliberately, naming
// the new legs — rather than deleting the fragment to get green.
func TestEveryAllowlistedHistoryDeleteStillCarriesItsPredicate(t *testing.T) {
	found := scanHistoryDeletes(t, repoInternalRoot(t), historyBearingTables)

	var widened []string
	for key, allowance := range allowedHistoryDeletes {
		site, ok := found[key]
		if !ok {
			continue // TestNoUnallowlistedHistoryDelete reports this as a stale entry.
		}
		// EVERY statement under the key, not just one: a second, wider delete of the same table in
		// the same function is precisely the shape the (function, TABLE) key cannot see.
		for _, stmt := range site.statements {
			if missing := missingPredicateFragments(stmt, allowance.predicate); len(missing) > 0 {
				widened = append(widened, key+"\n        missing: "+strings.Join(missing, " | ")+
					"\n        statement: "+stmt)
			}
		}
	}
	sort.Strings(widened)

	if len(widened) > 0 {
		t.Errorf("%d allowlisted history delete(s) no longer carry the predicate they were "+
			"blessed for (#1569).\n"+
			"The entry in allowedHistoryDeletes still authorises this destruction, but the SQL "+
			"beneath it has widened — which is the exact failure the predicate exists to catch. "+
			"Either restore the missing leg, or change the entry deliberately and say why in its "+
			"reason.\n    %s", len(widened), strings.Join(widened, "\n    "))
	}
}

// ---- Meta-tests: the constructed violations. ---------------------------------------------------

// TestHistoryDeleteGuard_FlagsAnUnallowlistedDelete is the case G2 exists for.
func TestHistoryDeleteGuard_FlagsAnUnallowlistedDelete(t *testing.T) {
	got := historyDeleteSites("DELETE FROM holding_snapshots WHERE account_id = $1", historyBearingTables)
	if len(got) != 1 || got[0] != "holding_snapshots" {
		t.Fatalf("expected the delete to be reported, got %v", got)
	}
}

// TestHistoryDeleteGuard_SeesTheFormsThatDoNotLookLikeADelete covers the three shapes that a
// substring search for "DELETE FROM holding_snapshots" would miss, and the one it must NOT match.
func TestHistoryDeleteGuard_SeesTheFormsThatDoNotLookLikeADelete(t *testing.T) {
	cases := []struct {
		name string
		sql  string
		want []string
	}{
		{"USING form", "DELETE FROM savings_goal_contributions c USING savings_goals g WHERE c.savings_goal_id = g.id", []string{"savings_goal_contributions"}},
		{"truncate", "TRUNCATE TABLE balance_snapshots", []string{"balance_snapshots"}},
		{"truncate only", "TRUNCATE ONLY rollover_history", []string{"rollover_history"}},
		{"lowercase, newline-separated", "delete\n  from\n  boost_executions\nwhere id = $1", []string{"boost_executions"}},
		{"prefix must not match the longer name", "DELETE FROM savings_goal_contributions WHERE id = $1", []string{"savings_goal_contributions"}},
		{"budgets must not match budget_categories", "DELETE FROM budget_categories WHERE id = $1", []string{"budget_categories"}},
		{"a read is not a delete", "SELECT * FROM holding_snapshots WHERE account_id = $1", nil},
		{"a tombstone is not a delete", "UPDATE budgets SET deleted_at = now() WHERE id = $1", nil},
		{"another table is not our business", "DELETE FROM notifications WHERE user_id = $1", nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := historyDeleteSites(tc.sql, historyBearingTables)
			if strings.Join(got, ",") != strings.Join(tc.want, ",") {
				t.Fatalf("got %v, want %v", got, tc.want)
			}
		})
	}
}

// TestHistoryDeleteGuard_AllowlistEntriesAreWellFormed keeps the allowlist from silently ceasing to
// mean anything: a key whose table is not history-bearing can never match, and an entry with no
// reason is just a suppression.
func TestHistoryDeleteGuard_AllowlistEntriesAreWellFormed(t *testing.T) {
	for key, allowance := range allowedHistoryDeletes {
		if strings.TrimSpace(allowance.reason) == "" {
			t.Errorf("allowlist entry %q has no reason; the reason is the point", key)
		}
		if len(allowance.predicate) == 0 {
			t.Errorf("allowlist entry %q names no predicate. An entry with no predicate blesses "+
				"`DELETE FROM <table>` full stop, which is what #1569 T7.5 removed. Name the legs "+
				"that make the destruction narrow — including their polarity.", key)
		}
		for _, frag := range allowance.predicate {
			if strings.TrimSpace(frag) == "" {
				t.Errorf("allowlist entry %q has an empty predicate fragment, which every "+
					"statement trivially satisfies", key)
			}
		}
		i := strings.LastIndex(key, "|")
		if i < 0 || !strings.Contains(key[:i], "#") {
			t.Errorf("allowlist key %q is not of the form <path>#<func>|<table>", key)
			continue
		}
		if _, ok := historyBearingTables[key[i+1:]]; !ok {
			t.Errorf("allowlist key %q names table %q, which is not history-bearing — the entry can "+
				"never match and is permanently stale", key, key[i+1:])
		}
	}
}

// TestHistoryDeletePredicate_FlagsAWidenedStatement is the constructed violation for the predicate
// half of G2: the real PurgeAccount statement with its `deleted_at IS NOT NULL` scope removed, and
// with the `-- tombstone-exempt` comment that QUOTES that scope left in place, because leaving a
// comment behind is exactly how a widening would arrive in a review.
func TestHistoryDeletePredicate_FlagsAWidenedStatement(t *testing.T) {
	entry := allowedHistoryDeletes["repository/account_repository.go#PurgeAccount|savings_goals"]

	intact := historyDeleteStatements(`
		-- tombstone-exempt(write:savings_goals): the predicate is INVERTED on purpose and is right above --
		-- deleted_at IS NOT NULL.
		DELETE FROM savings_goals
		WHERE account_id = $1 AND user_id = $2 AND deleted_at IS NOT NULL
	`, historyBearingTables)["savings_goals"]
	if len(intact) != 1 {
		t.Fatalf("expected one statement, got %d", len(intact))
	}
	if missing := missingPredicateFragments(intact[0], entry.predicate); len(missing) > 0 {
		t.Fatalf("the intact statement should satisfy its own predicate, missing %v", missing)
	}

	widened := historyDeleteStatements(`
		-- tombstone-exempt(write:savings_goals): the predicate is INVERTED on purpose and is right above --
		-- deleted_at IS NOT NULL.
		DELETE FROM savings_goals
		WHERE account_id = $1 AND user_id = $2
	`, historyBearingTables)["savings_goals"]
	missing := missingPredicateFragments(widened[0], entry.predicate)
	if len(missing) != 1 || missing[0] != "deleted_at IS NOT NULL" {
		t.Fatalf("dropping the tombstone scope must be reported as exactly that fragment; got %v "+
			"(statement: %s)", missing, widened[0])
	}
}

// TestHistoryDeletePredicate_IsRobustToFormatting pins the properties that must NOT flip a verdict:
// re-wrapping, re-indenting, case, and comments. A guard that fires when someone runs a formatter
// gets deleted by the next engineer.
func TestHistoryDeletePredicate_IsRobustToFormatting(t *testing.T) {
	predicate := []string{"c.managed_by_reconciliation = true", "g.deleted_at IS NULL"}
	forms := []string{
		"DELETE FROM savings_goal_contributions c USING savings_goals g WHERE c.managed_by_reconciliation = true AND g.deleted_at IS NULL",
		"delete from savings_goal_contributions c\n\tusing savings_goals g\n\twhere\n\t\tc.managed_by_reconciliation = true\n\t\tand g.deleted_at is null",
		"DELETE FROM savings_goal_contributions c /* why */ USING savings_goals g\n-- a comment\nWHERE c.managed_by_reconciliation = true\n  AND g.deleted_at IS NULL",
	}
	for i, form := range forms {
		stmts := historyDeleteStatements(form, historyBearingTables)["savings_goal_contributions"]
		if len(stmts) != 1 {
			t.Fatalf("form %d: expected one statement, got %d", i, len(stmts))
		}
		if missing := missingPredicateFragments(stmts[0], predicate); len(missing) > 0 {
			t.Errorf("form %d: formatting changed the verdict, missing %v", i, missing)
		}
	}

	// The negative that formatting-robustness must not swallow: a comment QUOTING the leg does not
	// substitute for the leg.
	commentOnly := historyDeleteStatements(
		"DELETE FROM savings_goal_contributions c USING savings_goals g\n-- g.deleted_at IS NULL\nWHERE c.managed_by_reconciliation = true",
		historyBearingTables)["savings_goal_contributions"]
	if missing := missingPredicateFragments(commentOnly[0], predicate); len(missing) != 1 || missing[0] != "g.deleted_at IS NULL" {
		t.Errorf("a comment must not satisfy a predicate fragment; got %v", missing)
	}
}

// TestHistoryDeleteStatements_SplitsAtTheNextDelete proves the slicing that makes a per-statement
// predicate meaningful: two deletes in one function must not lend each other their predicates.
func TestHistoryDeleteStatements_SplitsAtTheNextDelete(t *testing.T) {
	got := historyDeleteStatements(
		"DELETE FROM balance_snapshots WHERE account_id = $1\nDELETE FROM accounts WHERE id = $1 AND user_id = $2",
		historyBearingTables)
	if len(got["balance_snapshots"]) != 1 || strings.Contains(got["balance_snapshots"][0], "ACCOUNTS") {
		t.Fatalf("the balance_snapshots slice must stop at the next DELETE, got %q", got["balance_snapshots"])
	}
	if len(got["accounts"]) != 1 || strings.Contains(got["accounts"][0], "ACCOUNT_ID = $1\n") {
		t.Fatalf("the accounts slice must start at its own DELETE, got %q", got["accounts"])
	}
	// A non-history delete between two history deletes is still a boundary.
	mixed := historyDeleteStatements(
		"DELETE FROM balance_snapshots WHERE account_id = $1\nDELETE FROM holdings WHERE account_id = $2\nDELETE FROM accounts WHERE id = $1",
		historyBearingTables)
	if strings.Contains(mixed["balance_snapshots"][0], "HOLDINGS") {
		t.Fatalf("a delete of a non-history table must end the preceding slice, got %q", mixed["balance_snapshots"][0])
	}
}
