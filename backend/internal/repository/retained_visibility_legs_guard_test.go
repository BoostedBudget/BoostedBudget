package repository

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// Issue #1569 task 21, defect C4 — the two account-visibility legs must never be called apart.
//
// THE DEFECT SHAPE, precisely. A dated row (a transaction, an investment transaction) belonging to
// a non-active account is visible for exactly one of two reasons, and they are COMPLEMENTS:
//
//	activeChainSiblingLeg              an ACTIVE account shares the row's user_account_id
//	retainedAccountNoLiveSuccessorLeg  NO active account shares it (its NOT EXISTS says so)
//
// A read that carries only the second covers only the disconnected-and-never-re-linked case; the
// entire RE-LINK case falls through both. That is not a corner: it is every user who has ever
// re-linked a bank. IncomeSummary and investmentTransactionRepository.GetByUserID each carried only
// the second leg, and each returned 0.00 over rows the unified feed happily listed.
//
// WHY A GUARD AND NOT JUST A FIX. Nothing about `retainedAccountNoLiveSuccessorLeg(alias, dateCol)`
// at a call site announces that it is half a predicate. The next read that needs "retained rows
// stay visible" will reach for the name that says exactly that, and will be wrong in the same way,
// silently, for the same population. So the halves are unexported-by-convention: the ONLY caller of
// either is retainedAccountVisibilityLegs, which renders both.
//
// WHAT IT CANNOT SEE. A read that writes the leg's SQL out by hand instead of calling the helper is
// invisible here — this guard is about the helpers, not about SQL text. That hole is covered from
// the other side by TestRetentionUniverses_RelinkedBrokerage_IncomeSurvives_Integration, which is
// behavioural and does not care how the predicate was produced.
//
// Mutation-checked: pointing IncomeSummary at retainedAccountNoLiveSuccessorLeg directly (the exact
// pre-fix defect) turns this test red naming investment_analytics_repository.go#IncomeSummary, and
// turns the integration test above red with TotalIncome 120.00 -> 0.00. See task-21-report.md.
func TestRetainedVisibilityLegsTravelTogether(t *testing.T) {
	const combiner = "retainedAccountVisibilityLegs"
	halves := map[string]bool{
		"activeChainSiblingLeg":             true,
		"retainedAccountNoLiveSuccessorLeg": true,
	}

	root := repoInternalRoot(t)
	var offenders []string
	sawCombiner := map[string]bool{}

	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		fset := token.NewFileSet()
		file, perr := parser.ParseFile(fset, path, nil, 0)
		if perr != nil {
			return perr
		}
		rel, _ := filepath.Rel(root, path)

		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Body == nil {
				continue
			}
			ast.Inspect(fn.Body, func(n ast.Node) bool {
				call, ok := n.(*ast.CallExpr)
				if !ok {
					return true
				}
				ident, ok := call.Fun.(*ast.Ident)
				if !ok {
					return true
				}
				if halves[ident.Name] {
					if fn.Name.Name != combiner {
						offenders = append(offenders,
							rel+"#"+fn.Name.Name+" calls "+ident.Name)
					} else {
						sawCombiner[ident.Name] = true
					}
				}
				return true
			})
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walking %s: %v", root, err)
	}

	sort.Strings(offenders)
	if len(offenders) > 0 {
		t.Errorf("half of the account-visibility disjunction is being rendered on its own.\n"+
			"Both legs are complements — one alone drops the entire re-link case (#1569 C4).\n"+
			"Call %s instead:\n  %s", combiner, strings.Join(offenders, "\n  "))
	}

	// A guard that cannot fail is worse than none: if the combiner ever stops calling both halves,
	// every call site is trivially "compliant" and this test would pass over the defect it exists
	// to catch.
	for name := range halves {
		if !sawCombiner[name] {
			t.Errorf("%s does not call %s — the combiner no longer renders both legs, "+
				"so this guard is now vacuous", combiner, name)
		}
	}
}
