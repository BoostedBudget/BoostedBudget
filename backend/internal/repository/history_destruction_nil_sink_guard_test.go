package repository

import (
	"go/ast"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// TestNoProductionCallerPassesANilAuditSinkToADestructionMethod — Issue #1569 round 17, Codex
// defect 3, the ENFORCEMENT-BOUNDARY half of ADR control 2.
//
// WHAT THE GAP IS. Control 2 says an explicit destruction of financial history writes an audit row
// in the SAME transaction. All four repository methods that destroy history take the sink as a
// parameter and SKIP the write when it is nil — deliberately, and this guard does not change that:
// 19 integration tests pass nil so they can exercise the destruction with no audit fixture
// (measured 2026-08-31 across account_repository_purge_integration_test.go,
// linked_item_repository_purge_integration_test.go, savings_contribution_delete_hardening_
// integration_test.go and history_retention_roundtrip_integration_test.go —
// TestDeleteContribution_StillDestroysTheRowTheUserNamed_Integration is one). Round 17 task 11 made the control mandatory by adding a nil
// check to each of the four SERVICE/HANDLER callers. That leaves the repository boundary itself
// open: a future caller that reaches a destruction method directly, with nil, commits an unaudited
// destroy, and nothing fails.
//
// WHY THIS SHAPE AND NOT THE OTHER TWO. The obvious fix — move the refusal into the repositories —
// closes the boundary completely and breaks the 19 tests that rely on the tolerance, replacing a
// deliberate testing affordance with 19 audit fixtures whose only purpose is to satisfy a check.
// The previous implementer scoped fail-loud to the service layer for that reason and it was the
// right call. The other option — accept and document — leaves nothing that fails. This is the
// middle: the nil-tolerance stays exactly where it is, and a PRODUCTION call site that exploits it
// cannot land silently.
//
// ⚠ WHAT THIS DOES NOT PROTECT, stated plainly, because a guard that implies more than it checks is
// how the previous three rounds of this issue went wrong:
//
//   - It only sees the LITERAL `nil`. A caller passing a field, parameter or interface value that
//     happens to be nil at runtime is invisible here. That case is covered — for the four callers
//     that exist — by their own `ErrAuditLogRepositoryNotWired` checks, and for a NEW service by
//     TestExplicitDestructionAuditSinkIsWiredInMain, but only if that service declares
//     SetAuditLogRepository. A new destruction service that declares no setter and wires its own
//     field from somewhere else is outside every guard in this issue.
//   - It does not check that the value passed is the REAL audit repository, or that anything is
//     written. A no-op implementation of AuditLogRepository satisfies this and every other guard.
//   - It is keyed by METHOD NAME, not by type. A call to some unrelated four-argument method that
//     happens to be called PurgeGoal would be checked too (a false positive a reader can dismiss),
//     and a destruction method added under a name not in destructionMethodsTakingAnAuditSink is not
//     checked at all (the direction that matters — keep this list in step with the ADR's table).
//   - It scans backend/internal only, the same boundary as G2. A call written in cmd/ is not seen.
//
// So the honest summary: the repository boundary is still permissive by design; what is now closed
// is DRIFT — a second production caller quietly adopting the nil the tests use.

// destructionMethodsTakingAnAuditSink is the four explicit-destruction repository methods from the
// ADR's control table, with the total argument count of the form that carries a sink. The count is
// part of the key so that the SERVICE-layer method of the same name — accountService.PurgeAccount
// takes (ctx, accountID, userID) and no sink — is not mistaken for the repository one.
var destructionMethodsTakingAnAuditSink = map[string]int{
	"PurgeAccount":       4, // (ctx, accountID, userID, auditLog)
	"PurgeLinkedItem":    4, // (ctx, linkedItemID, userID, auditLog)
	"PurgeGoal":          4, // (ctx, goalID, userID, auditLog)
	"DeleteContribution": 5, // (ctx, userID, goalID, id, auditLog) — #1625 added goalID
}

// TestNoProductionCallerPassesANilAuditSinkToADestructionMethod is described above.
func TestNoProductionCallerPassesANilAuditSinkToADestructionMethod(t *testing.T) {
	root := repoInternalRoot(t)
	seen, offenders := scanDestructionAuditSinkArguments(t, root)

	// Fail closed. If the walk stops finding the four production call sites the ADR names, this
	// test has stopped guarding and would pass vacuously.
	if seen == 0 {
		t.Fatalf("scanned %s and found no call to any of %v with a sink argument at all — the scan "+
			"is broken (methods renamed? callers moved out of internal/?) and this guard is vacuous",
			root, sortedDestructionMethodNames())
	}

	sort.Strings(offenders)
	if len(offenders) > 0 {
		t.Errorf("%d production call site(s) pass a literal nil audit sink to an explicit-destruction "+
			"repository method (#1569 round 17).\n"+
			"The repository will skip the audit write and destroy the row anyway — that tolerance "+
			"exists for integration tests, not for production code. ADR control 2 "+
			"(docs/decisions/2026-08-24-financial-history-retained-by-default.md) requires an audit "+
			"row in the SAME transaction as the destruction. Pass the real sink, and refuse with "+
			"repository.ErrAuditLogRepositoryNotWired when your own field is nil — see "+
			"accountService.PurgeAccount for the shape.\n    %s",
			len(offenders), strings.Join(offenders, "\n    "))
	}
}

// scanDestructionAuditSinkArguments returns how many qualifying calls were inspected and the
// human-readable location of each one passing a literal nil sink.
func scanDestructionAuditSinkArguments(t *testing.T, root string) (int, []string) {
	t.Helper()
	var seen int
	var offenders []string
	for _, pf := range goProductionFiles(t, root, false, false) {
		s, o := destructionAuditSinkArgumentsInFile(pf)
		seen += s
		offenders = append(offenders, o...)
	}
	return seen, offenders
}

func destructionAuditSinkArgumentsInFile(pf goProductionFile) (int, []string) {
	var seen int
	var offenders []string
	ast.Inspect(pf.file, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		want, ok := destructionMethodsTakingAnAuditSink[sel.Sel.Name]
		if !ok || len(call.Args) != want || call.Ellipsis.IsValid() {
			return true
		}
		seen++
		if ident, ok := call.Args[want-1].(*ast.Ident); ok && ident.Name == "nil" {
			offenders = append(offenders, pf.rel+":"+
				strconv.Itoa(pf.fset.Position(call.Pos()).Line)+" "+sel.Sel.Name+"(..., nil)")
		}
		return true
	})
	return seen, offenders
}

func sortedDestructionMethodNames() []string {
	out := make([]string, 0, len(destructionMethodsTakingAnAuditSink))
	for k := range destructionMethodsTakingAnAuditSink {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// TestNilAuditSinkGuard_FlagsTheConstructedViolation is the meta-test: the guard reports the exact
// call it exists to catch, and does not report the three shapes that are legitimate.
func TestNilAuditSinkGuard_FlagsTheConstructedViolation(t *testing.T) {
	dir := t.TempDir()
	src := "package fake\n\n" +
		"func Offend(repo any, ctx any, a, b string) {\n" +
		"\trepo.PurgeAccount(ctx, a, b, nil)\n" +
		"}\n\n" +
		"func Fine(repo any, ctx any, a, b, c string, sink any) {\n" +
		"\trepo.PurgeGoal(ctx, a, b, sink)\n" +
		"\trepo.DeleteContribution(ctx, a, b, c, sink)\n" +
		// The service-layer method of the same name: three args, no sink, must not be inspected.
		"\trepo.PurgeAccount(ctx, a, b)\n" +
		// A method not on the list at all.
		"\trepo.DeleteGoal(ctx, a, b, nil)\n" +
		"}\n"
	if err := os.WriteFile(filepath.Join(dir, "fake.go"), []byte(src), 0o600); err != nil {
		t.Fatalf("writing fixture: %v", err)
	}

	seen, offenders := scanDestructionAuditSinkArguments(t, dir)
	if seen != 3 {
		t.Fatalf("expected 3 sink-carrying calls to be inspected (the 3-arg PurgeAccount and the "+
			"unlisted DeleteGoal must be skipped), got %d", seen)
	}
	if len(offenders) != 1 || !strings.Contains(offenders[0], "PurgeAccount(..., nil)") {
		t.Fatalf("expected exactly the nil PurgeAccount call to be reported, got %v", offenders)
	}
}
