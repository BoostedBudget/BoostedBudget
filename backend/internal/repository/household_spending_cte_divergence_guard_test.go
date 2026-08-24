package repository

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// Issue #1569 task 23 — the household spending CTE is deliberately narrower than the household
// balance CTEs, and this guard exists so that nobody "fixes" the difference with a one-liner.
//
// THE DIVERGENCE, and that it is intentional. Decision 7 of
// docs/decisions/2026-08-24-financial-history-retained-by-default.md ("Retention never overrides
// another person's privacy bound - on the BALANCE reads") records that #1569 shipped the
// shared-interval rule on the balance / net-worth reads and NOT on the spending, income and
// savings-rate reads:
//
//	householdAccountsCTEForBalancesPerDay   admits closed grants, caller clips per day
//	householdAccountsCTEForBalancesAsOf     admits closed grants, clipped against $4
//	householdAccountsCTE                    keeps BOTH pre-#1569 predicates - narrow on purpose
//
// So a departed member's shared period survives in the household net-worth chart and is erased
// wholesale from household spending. The two reads answer about the same days and disagree. That
// was costed and deferred; it is not an oversight, and no test asserts the two agree.
//
// WHY A GUARD. The divergence is documented, which is exactly what makes it dangerous: a reader
// who gets as far as Decision 7 sees an inconsistency and an obvious repair - delete
// `AND has.revoked_at IS NULL` from householdAccountsCTE and the spending reads match the balance
// reads. That repair ships a MONEY DOUBLE-COUNT. householdAccountsCTE is
// `DISTINCT ON (COALESCE(has.physical_account_blind_idx, ua.id::text))` - one row per FINGERPRINT,
// not per user_account. Admit closed grants and a re-fingerprinted account with two closed grants
// emits TWO rows carrying the same user_account_id; every consumer joins
// `household_accounts ha ON ha.user_account_id = a.user_account_id`, so every matching transaction
// lands twice in SUM() and COUNT(). Nine date-ranged household reads consume this CTE. Showing a
// household a doubled spend total is worse than showing it too little.
//
// A date clip does not rescue the shortcut either: the DISTINCT ON collapses to a single row
// keeping the EARLIEST shared_at, so the surviving row carries no revoked_at to clip against. The
// only correct fix is the interval-emission-then-collapse restructuring the balance side got
// (see householdAccountsCTEForBalancesPerDay's comment, and
// TestHouseholdShareRange_RevocationDayKeepsOnlyPreRevocationInstants for the behaviour it buys),
// re-derived for a per-transaction rather than a per-day aggregation shape, and carried across all
// nine consumers with coverage at each. When that lands, this guard is what you delete - together
// with Decision 7 - and not before.
//
// WHY IT READS THE AST AND NOT THE FILE TEXT. A guard on this branch silently stopped guarding
// when a refactor turned a string literal into a concatenation and its substring search began
// matching nothing. This one parses the file, finds each function by name, and concatenates every
// string literal in its body - so literal, raw, concatenated and multi-return forms all read the
// same. Whitespace is normalised, so gofmt and re-indentation cannot move it either. A function
// that cannot be found is a hard failure, never a silent pass.
//
// WHAT IT CANNOT SEE. It is a statement about SQL text, not about results: a widening performed
// somewhere other than these three functions (a new CTE, a caller-side override) is invisible
// here. It also cannot tell a correct restructuring from a careless one - it only insists that
// removing the narrow predicates is a deliberate act with the double-count answered first.
//
// Mutation-checked: deleting `AND has.revoked_at IS NULL` from householdAccountsCTE - the exact
// one-line widening this guard exists to stop - turns this test red on its own assertion; see
// task-23-report.md.
func TestHouseholdSpendingCTEStaysNarrowerThanTheBalanceCTEs(t *testing.T) {
	const (
		spendingCTE      = "householdAccountsCTE"
		perDayCTE        = "householdAccountsCTEForBalancesPerDay"
		asOfCTE          = "householdAccountsCTEForBalancesAsOf"
		revokedNarrow    = "AND has.revoked_at IS NULL"
		memberNarrow     = "AND ua.user_id = ANY($3)"
		relaxed          = "OR has.revoked_at IS NOT NULL"
		fingerprintDedup = "DISTINCT ON (COALESCE(has.physical_account_blind_idx, ua.id::text))"
		consumerJoin     = "household_accounts ha ON ha.user_account_id = a.user_account_id"
	)
	const why = "\n" +
		"This divergence is INTENTIONAL - Decision 7 of\n" +
		"docs/decisions/2026-08-24-financial-history-retained-by-default.md.\n" +
		spendingCTE + " is DISTINCT ON the physical-account FINGERPRINT, not on user_account_id.\n" +
		"Admitting closed grants there makes a re-fingerprinted account emit two rows for one\n" +
		"user_account_id, and all nine consumers join on ha.user_account_id - so every matching\n" +
		"transaction is counted TWICE in SUM() and COUNT(). That is silent money inflation across\n" +
		"nine analytics endpoints, which is worse than the under-reporting it would replace.\n" +
		"A date clip does not fix it: the DISTINCT ON keeps the EARLIEST shared_at, leaving no\n" +
		"revoked_at to clip against. The correct fix is the interval-emission-then-collapse\n" +
		"restructuring " + perDayCTE + " got, re-derived for a per-transaction\n" +
		"aggregation shape and carried across all nine consumers. If you have done that, update\n" +
		"Decision 7 and delete this guard in the same change."

	path := filepath.Join(repoInternalRoot(t), "repository", "analytics_repository.go")
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, path, nil, 0)
	if err != nil {
		t.Fatalf("parsing %s: %v", path, err)
	}

	// Every string literal in a func body, concatenated and whitespace-normalised, keyed by func
	// name (methods included, so consumer SQL is visible too). Reading literals rather than file
	// text is what makes this survive a literal-to-concatenation refactor.
	bodies := map[string]string{}
	var allSQL strings.Builder
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Body == nil {
			continue
		}
		var sb strings.Builder
		ast.Inspect(fn.Body, func(n ast.Node) bool {
			lit, ok := n.(*ast.BasicLit)
			if ok && lit.Kind == token.STRING {
				if s, uerr := strconv.Unquote(lit.Value); uerr == nil {
					sb.WriteString(s)
					sb.WriteString(" ")
				}
			}
			return true
		})
		normalised := strings.Join(strings.Fields(sb.String()), " ")
		if fn.Recv == nil {
			bodies[fn.Name.Name] = normalised
		}
		allSQL.WriteString(normalised)
		allSQL.WriteString(" ")
	}

	sql := func(name string) string {
		body, ok := bodies[name]
		if !ok {
			t.Fatalf("%s is not a top-level func in %s any more.\n"+
				"This guard pins the deliberate spending/balance divergence and cannot do that\n"+
				"blind - re-point it at whatever replaced the function, or retire it along with\n"+
				"Decision 7 if the restructuring below has landed.%s", name, path, why)
		}
		if body == "" {
			t.Fatalf("%s contains no string literals - this guard reads SQL text and would now\n"+
				"pass over any change to it.%s", name, why)
		}
		return body
	}

	spending := sql(spendingCTE)
	perDay := sql(perDayCTE)
	asOf := sql(asOfCTE)

	// (1) The point of the guard: the spending CTE keeps BOTH narrow predicates.
	for _, predicate := range []string{revokedNarrow, memberNarrow} {
		if !strings.Contains(spending, predicate) {
			t.Errorf("%s no longer carries `%s`.%s", spendingCTE, predicate, why)
		}
	}

	// (2) The double-count mechanism the widening would trip is still live. If the dedup key or
	// the consumers' join column changed, the reasoning above no longer describes the code and
	// must be re-derived before this guard is trusted either way.
	if !strings.Contains(spending, fingerprintDedup) {
		t.Errorf("%s no longer dedups on `%s`.\n"+
			"That dedup key IS the double-count hazard this guard reasons about - re-derive the\n"+
			"argument against the new shape before relying on, or relaxing, the predicates.%s",
			spendingCTE, fingerprintDedup, why)
	}
	if !strings.Contains(allSQL.String(), consumerJoin) {
		t.Errorf("no read joins `%s` any more, so the shape this guard reasons about is gone.\n"+
			"Re-derive it before relaxing %s.%s", consumerJoin, spendingCTE, why)
	}

	// (3) Anti-vacuity in the other direction: the balance CTEs really are the wider universe.
	// If they ever narrow back to the spending CTE's predicates there is no divergence left to
	// pin, and assertion (1) would be guarding a rule nobody is tempted to break.
	for name, body := range map[string]string{perDayCTE: perDay, asOfCTE: asOf} {
		if !strings.Contains(body, relaxed) {
			t.Errorf("%s no longer admits closed grants via `%s`.\n"+
				"The balance reads have narrowed back to the spending universe, so the divergence\n"+
				"Decision 7 documents no longer exists and this guard is now vacuous. Fix the\n"+
				"balance read or retire both.", name, relaxed)
		}
		if strings.Contains(body, revokedNarrow) {
			t.Errorf("%s carries `%s` again - it has lost the shared-interval rule #1569 shipped\n"+
				"(see TestHouseholdShareAsOf_IntervalIsHalfOpen).", name, revokedNarrow)
		}
	}
}
