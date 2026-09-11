package repository

import (
	"go/ast"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// Issue #1630 task 3 — Guard: every SUM(...) over budget_categories.allocated_amount or
// .spent_amount must route through the sub-allocation dedup, or be on this list with a reason.
//
// THE BUG THIS GUARD EXISTS TO STOP FROM RECURRING. Seven places in this codebase compute a
// budget's total allocated/spent amount by summing budget_categories rows. Only two of them
// (domain.Budget.TotalSpent/TotalAllocated, and the frontend's computeBudgetMetrics) originally
// applied the sub-allocation dedup — a child category nested under a budgeted parent must be
// excluded from the sum, because the parent's own amount already rolls the child up (#1203),
// refined by #1630 to also gate on match_logic containment (parent ∈ {category_only, or} AND
// child ∈ {category_only, and}; '' counts as category_only — see domain/budget.go's
// isContainingMatchLogic/isContainedMatchLogic, which analytics_repository.go's
// GetBudgetVsActual mirrors exactly). The other five double-counted every nested child. Tasks 1
// and 2 fixed all seven; this guard is what stops an eighth site from being written the naive way.
//
// SCOPE. This half covers the BACKEND: every non-test .go file under backend/internal that issues
// a `SUM(...)` SQL aggregate whose argument names `allocated_amount` or `spent_amount`. It has a
// sibling in the frontend — apps/web/src/features/budgets/subAllocationTotalGuard.test.ts — which
// this half CANNOT see: four of the five sites Task 1/2 fixed were `.reduce()` calls in TSX/TS,
// invisible to a Go AST walk. Neither half implies the other; both must exist.
//
// THE RULE. A SUM(...) argument naming allocated_amount or spent_amount is SAFE only when the same
// query text also contains the literal marker `is_sub_allocation` (case-insensitive) — the CASE
// WHEN gate GetBudgetVsActual uses to null out a sub-allocation's contribution before summing
// (`COALESCE(SUM(CASE WHEN sub.is_sub_allocation THEN 0 ELSE bc.allocated_amount END), 0)`). A
// SUM missing that marker is either wrong (it needs the dedup) or right for a reason this guard
// cannot see (e.g. it sums a column a WHERE/JOIN has already narrowed to one row per budget) — and
// either way a human must say which, in allowedSubAllocationTotalSums, keyed by
// "<path under internal/>#<function>" with a reason. There is deliberately no way to satisfy this
// guard by rewording the SQL to merely CONTAIN the string "is_sub_allocation" without it doing
// anything — that would require inventing a lie about what the query does, and the marker itself
// is a fixed phrase lifted verbatim from the one query that is actually correct today.
//
// WHY A TEXT MARKER, NOT A SQL PARSE. Same tradeoff every other guard in this package makes: a SQL
// parser would need to understand CASE, aggregate scoping and CTEs to prove the dedup is doing its
// job; a text scanner cannot prove correctness, only that the query at least ATTEMPTS the known-good
// shape. The behavioural authority remains
// TestGetBudgetVsActual_Integration_DedupSubAllocations (analytics_repository_test.go) and the
// domain-level table tests on IsSubAllocationWithin; this guard's job is narrower and cheaper: it
// runs with no database, on every `go test ./...`, and it means the SECOND wrong SUM never ships
// silently the way the first five did.
//
// MUTATION-CHECKED. subAllocationTotalGuardMetaTests below construct the exact violation (a bare
// `SUM(bc.spent_amount)` with no dedup marker) and prove the pure decision procedure
// (subAllocationTotalVerdict) reports it. Separately — and this is the check that matters, because a
// synthetic case can pass for the wrong reason — a real `SUM(bc.spent_amount)` was temporarily
// substituted for analytics_repository.go's actual dedup'd expression and
// TestNoUnallowlistedSubAllocationTotalSum was re-run against the live corpus: it went RED, naming
// "repository/analytics_repository.go#GetBudgetVsActual", before the substitution was reverted. See
// task-3-report.md for the transcript.

// subAllocationColumnRegexp matches a bare reference to allocated_amount or spent_amount, with or
// without a table alias (`bc.allocated_amount`, `budget_categories.spent_amount`, or the bare
// column). Applied only to a SUM(...)'s OWN argument text (see sumArgsIn) — never to the whole
// query — so a query that mentions the column somewhere else entirely cannot satisfy this.
var subAllocationColumnRegexp = regexp.MustCompile(
	`(?is)\b(?:[a-z_][a-z0-9_]*\.)?(?:allocated_amount|spent_amount)\b`,
)

// subAllocationDedupMarker is the literal proof text a SUM(...)'s OWN ARGUMENT already routes
// through the shared dedup, lifted verbatim from analytics_repository.go's GetBudgetVsActual:
// `sub.is_sub_allocation`. Matched case-insensitively so re-casing the SQL cannot flip the verdict.
//
// ⚠ WHY THIS IS CHECKED AGAINST THE SUM'S ARGUMENT, NOT THE WHOLE QUERY (found while mutating this
// guard against the live corpus, before it shipped). GetBudgetVsActual's query contains the text
// "is_sub_allocation" TWICE: once inside each SUM's own CASE WHEN, and once as the LATERAL
// subquery's column alias (`) AS is_sub_allocation`) — the definition site, well outside either
// SUM(...). A first draft of this guard checked the marker against the WHOLE joined query text
// (literalUnit.sql), so replacing both SUMs' `CASE WHEN sub.is_sub_allocation THEN 0 ELSE
// bc.allocated_amount END` with a bare `bc.allocated_amount` — the exact regression this guard
// exists to catch — stayed GREEN: the alias definition alone kept the substring present in the
// query as a whole. Scoping the check to sumArgsIn's balanced-paren extraction of each SUM's own
// argument closes that: the alias definition lives outside every SUM(...), so it can no longer
// stand in for the dedup actually being applied where the sum happens. Re-run after the fix (see
// task-3-report.md): the same substitution now reports
// "repository/analytics_repository.go#GetBudgetVsActual" twice, once per column.
const subAllocationDedupMarker = "is_sub_allocation"

// sumArgsIn returns the text inside each `SUM(...)` call in sql, with balanced-paren matching (not
// a regexp, which cannot count) so that a SUM wrapping a nested expression — e.g. `SUM(CASE WHEN x
// THEN 0 ELSE y END)` or the deeper nesting COALESCE(SUM(...), 0) puts around it — is captured
// exactly at its own closing paren, not cut short by, or run past, a paren belonging to something
// else in the same statement.
func sumArgsIn(sql string) []string {
	lower := strings.ToLower(sql)
	var args []string
	for i := 0; i+4 <= len(lower); i++ {
		if lower[i:i+4] != "sum(" {
			continue
		}
		depth := 1
		j := i + 4
		for ; j < len(sql) && depth > 0; j++ {
			switch sql[j] {
			case '(':
				depth++
			case ')':
				depth--
			}
		}
		if depth == 0 {
			args = append(args, sql[i+4:j-1])
		}
	}
	return args
}

// allowedSubAllocationTotalSums is the pinned set of functions allowed to SUM(...) allocated_amount
// or spent_amount WITHOUT the is_sub_allocation dedup marker in the same query text, each with the
// reason it is still correct despite that. Empty today: the one production SQL site that sums
// either column (GetBudgetVsActual) carries the dedup itself, so nothing needs an exemption. Adding
// an entry here is a deliberate decision that some other shape of SUM is safe without the marker;
// say why, in the same commit that adds the query.
var allowedSubAllocationTotalSums = map[string]string{}

// subAllocationSite is one occurrence of the pattern this guard watches for, keyed the same way
// history_delete_guard_test.go keys allowedHistoryDeletes: by the function that issues it, not by
// line (line numbers rot; the function name is also the only part of the key a reader can
// evaluate).
type subAllocationSite struct {
	key string // "<path under internal/>#<function>"
	sql string
}

// subAllocationSitesIn extracts every subAllocationSite from a parsed production Go file, one per
// literal SQL unit inside each top-level function. Uses literalUnitsIn (guard_ast_shared_test.go)
// so this guard shares the argument-aware "what counts as one query" boundary with every other SQL
// guard in this package, rather than re-deriving its own.
func subAllocationSitesIn(files []goProductionFile) []subAllocationSite {
	var sites []subAllocationSite
	for _, f := range files {
		ast.Inspect(f.file, func(n ast.Node) bool {
			fn, ok := n.(*ast.FuncDecl)
			if !ok || fn.Body == nil || fn.Name == nil {
				return true
			}
			for _, unit := range literalUnitsIn(fn.Body) {
				for _, arg := range sumArgsIn(unit.sql) {
					if !subAllocationColumnRegexp.MatchString(arg) {
						continue
					}
					sites = append(sites, subAllocationSite{
						key: f.rel + "#" + fn.Name.Name,
						sql: arg,
					})
				}
			}
			return true
		})
	}
	return sites
}

// subAllocationTotalVerdict is the whole decision procedure, kept pure (like historyCascadeVerdict
// and historyDeleteAllowance's checks) so the meta-tests below can construct the violation each
// direction exists to catch, with no filesystem or parsing involved.
//
// A site whose SQL already contains subAllocationDedupMarker is safe and never reported, allowlisted
// or not — the allowlist is for sites that DON'T carry the marker and are correct for some other
// reason, not a place to re-bless the marker's absence on a query that has it.
func subAllocationTotalVerdict(sites []subAllocationSite, allow map[string]string) (unallowlisted []string, stale []string) {
	seen := map[string]bool{}
	for _, s := range sites {
		if strings.Contains(strings.ToLower(s.sql), subAllocationDedupMarker) {
			continue
		}
		seen[s.key] = true
		if _, ok := allow[s.key]; !ok {
			unallowlisted = append(unallowlisted, s.key)
		}
	}
	for k := range allow {
		if !seen[k] {
			stale = append(stale, k)
		}
	}
	sort.Strings(unallowlisted)
	sort.Strings(stale)
	return unallowlisted, stale
}

// ---- The live guard. ----------------------------------------------------------------------------

// TestNoUnallowlistedSubAllocationTotalSum is G-SUM: it runs against the real corpus, no database
// required. It is what actually protects the codebase; everything else in this file is either
// infrastructure for it or a meta-test proving it can fail.
func TestNoUnallowlistedSubAllocationTotalSum(t *testing.T) {
	root := repoInternalRoot(t)
	files := goProductionFiles(t, root, true, false)
	sites := subAllocationSitesIn(files)

	unallowlisted, stale := subAllocationTotalVerdict(sites, allowedSubAllocationTotalSums)

	if len(unallowlisted) != 0 {
		t.Errorf("SUM(...) over allocated_amount/spent_amount with no is_sub_allocation dedup and no "+
			"allowlist entry (Issue #1630):\n  %s\n"+
			"Either route the query through the sub-allocation dedup GetBudgetVsActual uses, or add a "+
			"reasoned entry to allowedSubAllocationTotalSums.", strings.Join(unallowlisted, "\n  "))
	}
	if len(stale) != 0 {
		t.Errorf("allowedSubAllocationTotalSums entries with no matching site (allowlist rot):\n  %s",
			strings.Join(stale, "\n  "))
	}
}

// TestSubAllocationTotalAllowlistEntriesCarryAReason mirrors
// TestHistoryCascadeGuard_PinnedAllowlistIsSelfConsistent: an allowlist entry with no reason record
// a decision nobody actually made.
func TestSubAllocationTotalAllowlistEntriesCarryAReason(t *testing.T) {
	for key, reason := range allowedSubAllocationTotalSums {
		if strings.TrimSpace(reason) == "" {
			t.Errorf("allowedSubAllocationTotalSums[%q] has no reason; the reason is the point", key)
		}
	}
}

// ---- Meta-tests: the constructed violations. ----------------------------------------------------

// TestSubAllocationTotalGuard_FlagsARawSumOverSpentAmount is the mutant this guard was written to
// catch: a new site sums spent_amount across budget_categories rows with no dedup and no allowlist
// entry. This must be reported.
func TestSubAllocationTotalGuard_FlagsARawSumOverSpentAmount(t *testing.T) {
	sites := []subAllocationSite{{
		key: "repository/fake_repository.go#GetFakeTotals",
		sql: "SELECT b.id, COALESCE(SUM(bc.spent_amount), 0) AS spent_amount FROM budgets b " +
			"LEFT JOIN budget_categories bc ON bc.budget_id = b.id WHERE b.user_id = $1 GROUP BY b.id",
	}}
	unallowlisted, stale := subAllocationTotalVerdict(sites, map[string]string{})
	if len(unallowlisted) != 1 || unallowlisted[0] != "repository/fake_repository.go#GetFakeTotals" {
		t.Fatalf("a raw SUM(bc.spent_amount) with no dedup marker and no allowlist entry must be "+
			"reported; got unallowlisted=%v stale=%v", unallowlisted, stale)
	}
	if len(stale) != 0 {
		t.Fatalf("no allowlist entries were supplied, so nothing should be reported stale; got %v", stale)
	}
}

// TestSubAllocationTotalGuard_FlagsARawSumOverAllocatedAmount is the same mutant on the sibling
// column — allocated_amount is just as double-countable as spent_amount and Task 1's fix touched
// both in the same query.
func TestSubAllocationTotalGuard_FlagsARawSumOverAllocatedAmount(t *testing.T) {
	sites := []subAllocationSite{{
		key: "repository/fake_repository.go#GetFakeAllocations",
		sql: "SELECT b.id, SUM(budget_categories.allocated_amount) FROM budget_categories " +
			"GROUP BY b.id",
	}}
	unallowlisted, _ := subAllocationTotalVerdict(sites, map[string]string{})
	if len(unallowlisted) != 1 || unallowlisted[0] != "repository/fake_repository.go#GetFakeAllocations" {
		t.Fatalf("a raw SUM(budget_categories.allocated_amount) must be reported; got %v", unallowlisted)
	}
}

// TestSubAllocationTotalGuard_IgnoresADedupedSum is the case that must NOT be flagged: the real
// shape GetBudgetVsActual uses. Without this test, someone could "fix" the regexp into a blanket
// ban on SUM(...allocated_amount...) that would demand an allowlist entry for the one query that is
// already correct.
func TestSubAllocationTotalGuard_IgnoresADedupedSum(t *testing.T) {
	sites := []subAllocationSite{{
		key: "repository/analytics_repository.go#GetBudgetVsActual",
		sql: "COALESCE(SUM(CASE WHEN sub.is_sub_allocation THEN 0 ELSE bc.allocated_amount END), 0) " +
			"AS allocated_amount",
	}}
	unallowlisted, stale := subAllocationTotalVerdict(sites, map[string]string{})
	if len(unallowlisted) != 0 || len(stale) != 0 {
		t.Fatalf("a SUM(...) carrying the is_sub_allocation dedup marker must never be reported; got "+
			"unallowlisted=%v stale=%v", unallowlisted, stale)
	}
}

// TestSubAllocationTotalGuard_FlagsAStaleAllowlistEntry is the other direction, mirroring
// TestHistoryCascadeGuard_FlagsAStaleAllowlistEntry: an allowlist entry whose site no longer exists
// reads as a decision that no longer applies to anything, and must be reported so it gets deleted.
func TestSubAllocationTotalGuard_FlagsAStaleAllowlistEntry(t *testing.T) {
	allow := map[string]string{
		"repository/long_gone_repository.go#DeletedFunc": "this site no longer exists",
	}
	unallowlisted, stale := subAllocationTotalVerdict(nil, allow)
	if len(unallowlisted) != 0 {
		t.Fatalf("no sites were supplied, so nothing should be unallowlisted; got %v", unallowlisted)
	}
	if len(stale) != 1 || stale[0] != "repository/long_gone_repository.go#DeletedFunc" {
		t.Fatalf("an allowlist entry with no matching site must be reported stale; got %v", stale)
	}
}

// TestSubAllocationTotalGuard_AllowlistedSiteIsNotReported proves the allowlist actually clears a
// site once one is added — the guard must go green when a human has explicitly blessed a site, not
// just red when they haven't.
func TestSubAllocationTotalGuard_AllowlistedSiteIsNotReported(t *testing.T) {
	sites := []subAllocationSite{{
		key: "repository/fake_repository.go#GetFakeTotals",
		sql: "SELECT SUM(bc.spent_amount) FROM budget_categories bc",
	}}
	allow := map[string]string{
		"repository/fake_repository.go#GetFakeTotals": "constructed example: this key IS the site above",
	}
	unallowlisted, stale := subAllocationTotalVerdict(sites, allow)
	if len(unallowlisted) != 0 || len(stale) != 0 {
		t.Fatalf("a site with a matching allowlist entry must be reported neither way; got "+
			"unallowlisted=%v stale=%v", unallowlisted, stale)
	}
}
