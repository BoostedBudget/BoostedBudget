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

// Issue #1569 Task 19, review finding F1 — a partial SELECT feeding a liveness predicate.
//
// THE DEFECT SHAPE. An account row read without its `status` column comes back with
// `Account.Status == ""`. No `accounts` row can ever hold that value, so every downstream liveness
// check silently gets the wrong answer: `isAccountDisconnected` reads "" as *active*, and a
// disconnected account is treated as live. The read compiles, the scan succeeds, the tests pass, and
// the account is simply wrong about whether it exists any more.
//
// This is not a hypothetical class either. `GetByUserAccountIDAndUser` was the one account-returning
// method that omitted `status`; it was fixed in 8cfd4bee (#1569 residual E2) precisely because
// nothing would have caught a future caller using it as anything other than an ownership probe.
// **Zero account projections omit `status` today** — this guard is here so that stays true, which is
// what a guard is for. It is a regression guard, not a fix.
//
// THE MARKER. "Account-shaped" is defined as: a SELECT whose select list names `plaid_account_id`.
// That column is the provider's account identity and appears in no other table's projection; a
// query that pulls it is scanning an account row rather than probing for one value. Using the
// marker rather than "reads FROM accounts" keeps the guard off the many legitimate narrow reads
// (`SELECT id FROM accounts WHERE ...`, `SELECT COUNT(*) ...`, a join that needs only `a.type`),
// which is what stops it being suppressed.
//
// PER SELECT, NOT PER LITERAL OR PER FUNCTION. The same lesson the tombstone guards learned the hard
// way: an aggregated check is satisfied by a NEIGHBOURING query's `status`, which is exactly the
// bug. Each SELECT ... FROM span is judged on its own select list.
//
// NO ESCAPE HATCH, deliberately. There is no legitimate account-shaped projection that wants to omit
// status — a caller that genuinely needs three columns should select three columns and not
// `plaid_account_id`. An exemption comment would be the first thing reached for.
//
// WHAT IT CANNOT SEE, measured against constructed cases rather than guessed. Two holes remain, and
// both were found by building the case and running it:
//
//  1. `SELECT *` and `SELECT a.*` are INVISIBLE — the select list never names `plaid_account_id`, so
//     the marker never fires. There is no such account read in the tree today. A star projection
//     would in fact bring `status` along, so this is a blind spot rather than a false negative, but
//     it is a blind spot.
//  2. A function call containing FROM truncates the span, because `accountProjectionSelect` is
//     non-greedy. It cuts BOTH ways and neither is silent about it:
//     `SELECT EXTRACT(YEAR FROM created_at), plaid_account_id FROM accounts` is a MISS (the marker
//     falls outside the truncated span), and `SELECT plaid_account_id, EXTRACT(YEAR FROM created_at)
//     FROM accounts` is a FALSE POSITIVE (status falls outside it). Neither shape exists in the tree.
//
// Two holes that a first draft of this guard DID have are now closed, and both are pinned below:
// a join whose OTHER table supplies `status` (`a.plaid_account_id, li.status`) and a correlated
// subquery selecting `status` from somewhere else. Both used to satisfy a bare "contains status"
// check while the account still arrived with Status == "". They are the same defect class as the
// tombstone guards' alias-unqualified predicate, and the fix is the same one.
//
// Mutation-checked: deleting `status` from any account projection in the tree turns this test red
// with the file, line and offending column list; see task-19-report.md.

// accountProjectionSelect matches one `SELECT <list> FROM` span. Non-greedy so that nested spans are
// found separately rather than swallowed.
var accountProjectionSelect = regexp.MustCompile(`(?is)\bSELECT\b(.*?)\bFROM\b`)

var (
	// The marker, with its qualifier captured: `a.plaid_account_id` -> "a", bare -> "".
	accountProjectionMarker = regexp.MustCompile(`(?i)(?:\b([A-Za-z_][A-Za-z0-9_]*)\.)?\bplaid_account_id\b`)
	// Every `status` in the list, with its qualifier. `status_changed_at` does not match: `_` is a
	// word character, so \b does not fire after "status".
	accountProjectionStatus = regexp.MustCompile(`(?i)(?:\b([A-Za-z_][A-Za-z0-9_]*)\.)?\bstatus\b`)
)

// accountSelectsMissingStatus returns, for one SQL string, the select lists that name
// `plaid_account_id` but do not name a `status` BELONGING TO THE SAME TABLE REFERENCE.
//
// The qualifier check is the same lesson the tombstone read guards learned: `SELECT
// a.plaid_account_id, li.status FROM accounts a JOIN linked_items li ...` satisfies a bare
// "contains status" test with a DIFFERENT table's column, which is precisely the defect — the
// account still arrives with Status == "". Matching the qualifier that carries `plaid_account_id`
// costs nothing and closes it. An unqualified marker accepts an unqualified `status` (a
// single-table read) and nothing else.
func accountSelectsMissingStatus(sql string) []string {
	var offenders []string
	for _, m := range accountProjectionSelect.FindAllStringSubmatch(sql, -1) {
		list := m[1]
		marker := accountProjectionMarker.FindStringSubmatch(list)
		if marker == nil {
			continue
		}
		qualifier := marker[1]
		ok := false
		for _, st := range accountProjectionStatus.FindAllStringSubmatch(list, -1) {
			if strings.EqualFold(st[1], qualifier) {
				ok = true
				break
			}
		}
		if ok {
			continue
		}
		offenders = append(offenders, strings.Join(strings.Fields(list), " "))
	}
	return offenders
}

// TestNoAccountProjectionOmitsStatus is the guard.
func TestNoAccountProjectionOmitsStatus(t *testing.T) {
	root := repoInternalRoot(t)

	var offenders []string
	seenAccountProjection := 0

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

		ast.Inspect(file, func(n ast.Node) bool {
			lit, ok := n.(*ast.BasicLit)
			if !ok || lit.Kind != token.STRING {
				return true
			}
			sql, unqErr := strconv.Unquote(lit.Value)
			if unqErr != nil {
				return true
			}
			if !strings.Contains(strings.ToLower(sql), "plaid_account_id") {
				return true
			}
			seenAccountProjection++
			for _, bad := range accountSelectsMissingStatus(sql) {
				offenders = append(offenders,
					rel+":"+strconv.Itoa(fset.Position(lit.Pos()).Line)+"\n      SELECT "+bad+" FROM ...")
			}
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatalf("walking internal/: %v", err)
	}

	// Fail closed. The repository is full of account projections; finding none means the walk or the
	// literal extraction broke, and a vacuous pass here would read as protection.
	if seenAccountProjection < 10 {
		t.Fatalf("found only %d SQL literals mentioning plaid_account_id under %s; the scan is broken",
			seenAccountProjection, root)
	}

	sort.Strings(offenders)
	if len(offenders) > 0 {
		t.Fatalf("%d account projection(s) select plaid_account_id but not status (#1569).\n"+
			"An account scanned without its status arrives with Status == \"\", a value no row can hold. "+
			"isAccountDisconnected reads that as ACTIVE, so a disconnected or replaced account is "+
			"treated as live and its retained history is rendered as current. Add `status` (and "+
			"`removed_at` if the caller renders an \"as of\" label), or stop selecting "+
			"plaid_account_id if this really is a narrow probe:\n    %s",
			len(offenders), strings.Join(offenders, "\n    "))
	}
}

// ---- Meta-tests: the constructed violations. ---------------------------------------------------

// TestAccountStatusProjectionGuard_FlagsASelectThatOmitsStatus is the case the guard exists for,
// alongside every neighbour it must NOT fire on. The negative cases carry the weight: a checker that
// reported everything would satisfy the positive one and would be turned off within a day.
func TestAccountStatusProjectionGuard_FlagsASelectThatOmitsStatus(t *testing.T) {
	cases := []struct {
		name string
		sql  string
		want int
	}{
		{"the defect", "SELECT id, plaid_account_id, name FROM accounts WHERE id = $1", 1},
		{"status present", "SELECT id, plaid_account_id, status FROM accounts WHERE id = $1", 0},
		{"alias-qualified status counts", "SELECT a.id, a.plaid_account_id, a.status FROM accounts a", 0},
		{"status_changed_at is not status", "SELECT plaid_account_id, status_changed_at FROM accounts", 1},
		{"a narrow probe is out of scope", "SELECT id FROM accounts WHERE user_id = $1", 0},
		{"a count is out of scope", "SELECT COUNT(*) FROM accounts WHERE status = 'active'", 0},
		{
			"a neighbouring query's status does NOT satisfy this one",
			"SELECT id, status FROM linked_items; SELECT plaid_account_id, name FROM accounts",
			1,
		},
		{
			"two projections are judged separately",
			"SELECT plaid_account_id, name FROM accounts UNION ALL SELECT plaid_account_id, name FROM accounts",
			2,
		},
		{"lowercase and wrapped", "select\n  plaid_account_id,\n  name\nfrom accounts", 1},
		{
			"a JOINED table's status does NOT satisfy the account's",
			"SELECT a.id, a.plaid_account_id, li.status FROM accounts a JOIN linked_items li ON li.id = a.linked_item_id",
			1,
		},
		{
			"the account's OWN status does",
			"SELECT a.id, a.plaid_account_id, a.status FROM accounts a JOIN linked_items li ON li.id = a.linked_item_id",
			0,
		},
		{
			"a correlated subquery's status does NOT",
			"SELECT a.plaid_account_id, (SELECT status FROM linked_items WHERE id = a.linked_item_id) AS s FROM accounts a",
			1,
		},
		{
			"a qualified status does not satisfy an UNQUALIFIED marker",
			"SELECT plaid_account_id, li.status FROM accounts JOIN linked_items li ON true",
			1,
		},
		// The two known holes, pinned AS holes so that closing one is a visible change rather than a
		// silent improvement nobody notices, and so the header cannot drift away from the behaviour.
		{"KNOWN HOLE: star projection is invisible", "SELECT a.* FROM accounts a", 0},
		{
			"KNOWN HOLE: a FROM inside a function truncates the span",
			"SELECT EXTRACT(YEAR FROM created_at) AS y, plaid_account_id FROM accounts",
			0,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := accountSelectsMissingStatus(tc.sql)
			if len(got) != tc.want {
				t.Fatalf("got %d (%v), want %d", len(got), got, tc.want)
			}
		})
	}
}
