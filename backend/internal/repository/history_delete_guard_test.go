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
// DIFFERENT history table inside an allowlisted function IS caught — that is a new key. What slips
// through is a second delete of the SAME table in the same function, i.e. widening what an existing
// purge destroys. For PurgeAccount that case is covered behaviourally instead, by
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

// allowedHistoryDeletes maps "<path under internal/>#<function>" to the history table it may hard
// delete, and why. One entry per (function, table) pair.
var allowedHistoryDeletes = map[string]string{
	// The account purge. `PurgeAccount` refuses to run on a live account and writes an audit row in
	// the same transaction; it is the user explicitly asking for one account's data to be destroyed.
	// The three cascade edges in allowedHistoryCascades are the schema half of the same decision.
	"repository/account_repository.go#PurgeAccount|balance_snapshots": "explicit account purge: balance_snapshots has ON DELETE SET NULL from accounts, so the cascade would orphan rather than remove them",
	"repository/account_repository.go#PurgeAccount|accounts":          "explicit account purge: this IS the purge",

	// The linked-item purge. Same shape, one level up: the user disconnects AND purges an
	// institution. A plain disconnect soft-removes and keeps everything.
	"repository/linked_item_repository.go#PurgeLinkedItem|balance_snapshots": "explicit institution purge, same reason as PurgeAccount",

	// The savings-goal purge. Distinct from DeleteGoal, which tombstones (#1569 task 8).
	"repository/savings_goal_repository.go#PurgeGoal|savings_goals": "explicit goal purge; the ordinary DeleteGoal path tombstones instead",

	// Contributions.
	"repository/savings_contribution_repository.go#DeleteContribution|savings_goal_contributions":          "the user deleting one contribution they entered — the narrowest possible explicit request",
	"repository/savings_contribution_repository.go#DeleteManagedForTransaction|savings_goal_contributions": "reconciler-OWNED rows (managed_by_reconciliation = true) for a transaction being removed: derived, regenerable, and not a record of anything the user did. User-entered contributions do not match the predicate.",
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

// historyDeleteSites returns the history tables hard-deleted by one SQL string.
func historyDeleteSites(sql string, tables map[string]string) []string {
	var out []string
	seen := map[string]bool{}
	for _, m := range historyDeleteRefRegexp(tables).FindAllStringSubmatch(sql, -1) {
		if !seen[m[1]] {
			seen[m[1]] = true
			out = append(out, m[1])
		}
	}
	sort.Strings(out)
	return out
}

// scanHistoryDeletes walks non-test Go under root and returns every "<rel>#<func>|<table>" key found.
// Only string LITERALS are inspected, so prose in a comment that quotes `DELETE FROM budgets` — and
// four files do exactly that, explaining why they no longer do it — is correctly ignored.
func scanHistoryDeletes(t *testing.T, root string, tables map[string]string) map[string][]string {
	t.Helper()
	found := map[string][]string{}

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
			for _, tbl := range historyDeleteSites(strings.Join(parts, "\n"), tables) {
				key := rel + "#" + fn.Name.Name + "|" + tbl
				found[key] = append(found[key], strconv.Itoa(fset.Position(fn.Pos()).Line))
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
	for key, lines := range found {
		if _, ok := allowedHistoryDeletes[key]; !ok {
			unallowlisted = append(unallowlisted, key+" (func at line "+strings.Join(lines, ",")+")")
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
			"predicate:\n    %s", len(unallowlisted), strings.Join(unallowlisted, "\n    "))
	}
	if len(stale) > 0 {
		t.Errorf("%d allowlisted history delete(s) no longer exist (#1569).\n"+
			"A stale entry reads as a standing permission to destroy this history. Delete it:\n    %s",
			len(stale), strings.Join(stale, "\n    "))
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
	for key, reason := range allowedHistoryDeletes {
		if strings.TrimSpace(reason) == "" {
			t.Errorf("allowlist entry %q has no reason; the reason is the point", key)
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
