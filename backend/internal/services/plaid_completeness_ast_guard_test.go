package services

// plaid_completeness_ast_guard_test.go — Issue #1569 Task 5, fix round 1.
//
// WHY THIS GUARD EXISTS. PlaidAccountRoster makes the catastrophic mistake inconvenient, not
// impossible. The claim that the "known accounts minus what Plaid returned" loop was UNWRITABLE was
// wrong, and this file is the correction. `Present` is exported and answers exactly the membership
// question the old bare map did, so this compiles, reads as correct, and is even idiomatic in this
// package:
//
//	for _, a := range knownAccounts {
//	    if !roster.Present(a.PlaidAccountID) { markRemoved(a) }   // ← no Complete() check
//	}
//
// On a zero-account or errored response that marks EVERY account removed — permanently, since
// nothing in this codebase writes an account back to 'active'.
//
// So: `ConfirmedAbsentIn` exists so nobody has to write the loop, and this scan makes writing it
// anyway visible. It fails on any NEGATED `.Present(` call outside the one legitimate site. Negation
// is the tell: `roster.Present(x)` gates SKIPPING work (safe on any response), while
// `!roster.Present(x)` is "Plaid did not mention this" — which is only ever a licence to act if the
// response was provably complete, and that is what ConfirmedAbsent/ConfirmedAbsentIn are for.
//
// Modelled on cmd/server's wiring-drift AST scans. Adding a legitimate negated site means adding it
// to presentNegationAllowlist WITH a reason — not deleting this test.

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// presentNegationAllowlist maps "file.go:enclosingFunc" to why a negated Present is correct there.
var presentNegationAllowlist = map[string]string{
	// Task 4's holdings gate. The negation guards a SKIP (preserve existing holdings), never a
	// destructive write, so an incomplete response can only cause it to do less, never more.
	"investment_sync_service.go:SyncHoldings": "negation gates skipping a wipe, not performing one",
	// The roster's own implementation of the safe question: ConfirmedAbsent is the completeness
	// check WRAPPED AROUND this negation, which is the whole point of the type.
	"plaid_completeness.go:ConfirmedAbsent": "this is the guarded accessor the rest of the package must use",
}

func TestNoHandWrittenAbsenceLoopOutsideTheRoster(t *testing.T) {
	entries, err := os.ReadDir(".")
	require.NoError(t, err)

	fset := token.NewFileSet()
	scanned := 0
	presentCalls := 0
	var violations []string

	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(fset, filepath.Join(".", name), nil, 0)
		require.NoError(t, err, "parse %s", name)
		scanned++

		var enclosing string
		ast.Inspect(file, func(n ast.Node) bool {
			switch node := n.(type) {
			case *ast.FuncDecl:
				enclosing = node.Name.Name
			case *ast.CallExpr:
				sel, ok := node.Fun.(*ast.SelectorExpr)
				if !ok || sel.Sel.Name != "Present" {
					return true
				}
				presentCalls++
			case *ast.UnaryExpr:
				if node.Op != token.NOT {
					return true
				}
				call, ok := node.X.(*ast.CallExpr)
				if !ok {
					return true
				}
				sel, ok := call.Fun.(*ast.SelectorExpr)
				if !ok || sel.Sel.Name != "Present" {
					return true
				}
				key := name + ":" + enclosing
				if _, allowed := presentNegationAllowlist[key]; !allowed {
					violations = append(violations, key+" at "+fset.Position(node.Pos()).String())
				}
			}
			return true
		})
	}

	// Fail-closed: a scan that finds nothing to look at would pass vacuously forever.
	require.Greater(t, scanned, 20, "the scan found almost no source files — it is broken")
	require.Greater(t, presentCalls, 0,
		"no .Present( call found anywhere in package services — either the roster was renamed "+
			"(update this guard) or the scan is broken; either way it is not proving anything")

	if len(violations) > 0 {
		t.Errorf("negated .Present( found outside the allowlist: %v\n\n"+
			"`!roster.Present(id)` means \"Plaid did not mention this account\", which is UNKNOWN, "+
			"not \"gone\" — acting on it removes every account on a zero-account or errored "+
			"response, permanently. Use roster.ConfirmedAbsentIn(knownIDs) (or ConfirmedAbsent) "+
			"instead, which cannot answer affirmatively for an untrustworthy response. If the "+
			"negation genuinely gates skipping work rather than doing it, add it to "+
			"presentNegationAllowlist with a reason.", violations)
	}
}
