package services_test

// Issue #1625 Task 17, fix round 1 — the WIRING half of closing the unguarded basis-switch path.
//
// WHAT THIS PROVES: every production construction of services.NewSavingsGoalService wires the
// basis-switch contribution ledger through SetBasisSwitchContribRepo in the same function.
//
// WHY IT EXISTS. basisSwitchRepo is optional by construction — a setter, not a constructor
// parameter — and until this round UpdateGoal quietly fell back to an unlocked, unpredicated
// goalRepo.Update when it was nil. That is the "runtime hook protecting a compile-time property"
// class: main.go happened to wire it, so every test passed, while two OTHER construction sites in
// huma/operations/savings_goal.go did not. They were Create-only and therefore harmless the day
// they were written, which is precisely the kind of latent hole that becomes a defect the moment
// somebody adds an update call beside them.
//
// The structural fix lives in savingsGoalRepository.Update, which now REFUSES a basis change
// outright, so the bypass is unrepresentable rather than merely tested against
// (TestSavingsGoalRepository_Update_RefusesABasisChange). This guard is the second, cheaper line:
// it turns "you will get a 500 at runtime" into "your build is red", and it names the file and
// line so the next person does not have to work out which of several construction sites is short.
//
// WHAT THIS DOES NOT PROVE. It is a syntactic check on one function body. It cannot tell that the
// repository handed to the setter is the RIGHT one, it does not follow a construction wrapped in a
// helper that returns the service, and it says nothing about the other optional setters. The
// repository-level refusal is the guarantee; this is the early warning.

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/maxnussbaum/BoostedBudget/internal/testsupport/sourcefiles"
)

func TestEverySavingsGoalServiceConstructionWiresTheBasisSwitchLedger(t *testing.T) {
	const repoRoot = "../../.."

	files, err := sourcefiles.Union(repoRoot, "backend", sourcefiles.HasExt(".go"))
	if err != nil {
		t.Fatalf("enumerate Go sources: %v", err)
	}

	type site struct {
		where string
		wired bool
	}
	var sites []site

	for _, rel := range files {
		// Tests construct the service hundreds of times, deliberately without the ledger, to
		// exercise the refusal itself. The property is about PRODUCTION wiring.
		if strings.HasSuffix(rel, "_test.go") {
			continue
		}
		src, readErr := os.ReadFile(filepath.Join(repoRoot, rel))
		if readErr != nil {
			t.Fatalf("read %s: %v", rel, readErr)
		}
		if !strings.Contains(string(src), "NewSavingsGoalService(") {
			continue
		}

		fset := token.NewFileSet()
		file, parseErr := parser.ParseFile(fset, rel, src, 0)
		if parseErr != nil {
			t.Fatalf("parse %s: %v", rel, parseErr)
		}

		// COUNTS, not a boolean, and per enclosing FUNCTION DECLARATION.
		//
		// A boolean was the first attempt and it was wrong in a way that mattered: both of
		// huma/operations/savings_goal.go's constructions live in one FuncDecl (one directly, one
		// inside the idempotency closure), so a single setter call anywhere in that function
		// marked BOTH wired. Deleting one site's setter left the guard green — verified by
		// mutation, which is the only reason this comment exists. Requiring at least as many
		// setter calls as constructions restores the property.
		ast.Inspect(file, func(n ast.Node) bool {
			fn, ok := n.(*ast.FuncDecl)
			if !ok || fn.Body == nil {
				return true
			}
			constructions, setters := 0, 0
			ast.Inspect(fn.Body, func(inner ast.Node) bool {
				call, ok := inner.(*ast.CallExpr)
				if !ok {
					return true
				}
				name := ""
				switch fun := call.Fun.(type) {
				case *ast.SelectorExpr:
					name = fun.Sel.Name
				case *ast.Ident:
					name = fun.Name
				}
				switch name {
				case "NewSavingsGoalService":
					constructions++
				case "SetBasisSwitchContribRepo":
					setters++
				}
				return true
			})
			for i := 0; i < constructions; i++ {
				sites = append(sites, site{
					where: fset.Position(fn.Pos()).String() + " (" + fn.Name.Name + ")",
					wired: setters >= constructions,
				})
			}
			return true
		})
	}

	// ⚠ A guard that scans nothing passes. This corpus check is not decoration: if a rename, a
	// moved file or a broken repoRoot ever makes Union return an empty or test-only set, the guard
	// above becomes a no-op that reports success forever. Three production constructions exist
	// today (cmd/server/main.go, and two in huma/operations/savings_goal.go); the floor is
	// deliberately below that so adding one does not fail the build, but zero or one cannot pass.
	if len(sites) < 3 {
		t.Fatalf("found only %d production NewSavingsGoalService construction(s); the guard's "+
			"corpus is broken, not the code — a guard that scans nothing always passes", len(sites))
	}

	var offenders []string
	for _, s := range sites {
		if !s.wired {
			offenders = append(offenders, s.where)
		}
	}
	if len(offenders) > 0 {
		t.Errorf("%d savings-goal service construction(s) do not wire the basis-switch ledger "+
			"in the same function.\n"+
			"A service without it REFUSES every progress_basis change (#1625 Task 17), because the "+
			"alternative — silently taking the unlocked, unpredicated path — is how two concurrent "+
			"switches doubled a goal and a failed seed zeroed one. Call "+
			"SetBasisSwitchContribRepo(repository.NewSavingsContributionRepository(<same handle>)) "+
			"beside the construction:\n\t%s",
			len(offenders), strings.Join(offenders, "\n\t"))
	}
}
