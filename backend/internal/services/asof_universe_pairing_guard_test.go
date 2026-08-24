package services

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

// Issue #1569 task 21, item E — a guard for the failure class C2 belongs to.
//
// THE CLASS. Task 19 built TestNoAccountProjectionOmitsStatus to catch a projection that FORGETS
// `status`. The more damaging class is the opposite: a read that interprets a PAST date while
// resolving its account universe against `status = 'active'` TODAY. It produces no error, no empty
// column, no type mismatch — just a confidently-labelled number that is too small. C2 was three
// instances of it in one file, and `as_of_date` is an unvalidated public query param on two
// endpoints that are both MCP `read` capabilities.
//
// THE INVARIANT, stated so it can be checked rather than admired. Inside one function, every
// point-in-time SNAPSHOT read is anchored on a cutoff. The ACCOUNT UNIVERSE that interprets those
// snapshots must be resolved at that same cutoff. So: for each function, the set of cutoffs handed
// to an as-of snapshot read must be a SUBSET of the cutoffs handed to an as-of account-universe
// read. A cutoff that appears on the snapshot side and nowhere on the account side is a snapshot
// read with no matching universe — which is exactly and only the defect.
//
// WHY THIS FORM AND NOT THE OBVIOUS ONES. Two weaker guards were built first and both were
// VACUOUS against the real pre-fix code, which is why they are recorded here rather than shipped:
//
//   - "a function that reads AsOfDate must not call GetByUserID" — the FIXED code still calls
//     GetByUserID (it is correct for the current leg, and the as-of universe only augments it), so
//     this flags the fix and not the defect.
//   - "a function that reads AsOfDate and calls an active-now read must also call an as-of read" —
//     pre-fix GetBalanceSummary DID call historicalAccountMap, for its baseline leg. The guard
//     passes over the defect. This is the "seeded a fixture built for the mechanism under test"
//     trap that let C2 survive twenty reviews, in guard form.
//
// The cutoff-pairing form is the first one that separates them, because it is keyed on the thing
// that actually differed: the baseline cutoff had a universe and the current cutoff did not.
//
// WHAT IT CANNOT SEE, measured rather than guessed:
//
//  1. A cutoff computed inline at BOTH call sites (`endOfDay(asOfDate)` written out twice rather
//     than hoisted) normalizes to the same identifier and is accepted — correctly, since it names
//     the same instant. But a cutoff passed as a literal or a field selector rather than an
//     identifier is skipped entirely, and the function is judged on its remaining cutoffs.
//  2. It proves the cutoffs MATCH, not that the universe is the RIGHT one. A function could pair
//     `previousCutoff` with an as-of read that ignores its argument. That is what the behavioural
//     tests are for (TestDashboard_PastAsOfDate_..., TestDashboardHousehold_PastAsOfDate_...).
//  3. Scope is internal/services. A repository that pairs them wrongly on its own is not seen here.
//
// Mutation-checked: removing either augmentation call from dashboard_service.go turns this test
// red naming the function and the unmatched cutoff — for the personal paths and for the household
// paths independently. See task-21-report.md for the before/after output.

// asOfSnapshotReads are the point-in-time snapshot reads. Their LAST argument is the cutoff.
var asOfSnapshotReads = map[string]bool{
	"GetSnapshotsAsOfForUser":      true,
	"GetSnapshotsAsOfForHousehold": true,
}

// asOfUniverseReads resolve an account universe AT AN INSTANT. Every identifier among their
// arguments counts as a cutoff they cover; naming the cutoff variable is the whole point of these
// calls, so this is deliberately generous on the covering side and strict on the demanding side.
//
// GetForHouseholdScopeActiveNow is NOT here, and that is the single most load-bearing line in this
// file: its own doc comment says its date argument scopes the share grants and the joint-account
// winner and NOT account status. Treating it as an as-of universe read is precisely the mistake
// #1537 round 10 and C2 both made.
var asOfUniverseReads = map[string]bool{
	"GetForHouseholdScopeLiveAsOf":       true,
	"GetAccountsWithInstitutionFiltered": true,
	"historicalAccountMap":               true,
	"augmentWithAsOfUniverse":            true,
	"augmentHouseholdWithAsOfUniverse":   true,
}

// cutoffName normalizes a cutoff expression to the identifier that names the instant.
// `previousCutoff` -> "previousCutoff"; `endOfDay(asOfDate)` -> "asOfDate". Anything else -> "".
func cutoffName(expr ast.Expr) string {
	switch e := expr.(type) {
	case *ast.Ident:
		return e.Name
	case *ast.CallExpr:
		if len(e.Args) == 1 {
			return cutoffName(e.Args[0])
		}
	case *ast.UnaryExpr: // &asOf
		return cutoffName(e.X)
	}
	return ""
}

func calleeName(call *ast.CallExpr) string {
	switch fn := call.Fun.(type) {
	case *ast.Ident:
		return fn.Name
	case *ast.SelectorExpr:
		return fn.Sel.Name
	}
	return ""
}

func TestAsOfSnapshotReadsHaveAMatchingAccountUniverse(t *testing.T) {
	root := servicesPackageRoot(t)
	var offenders []string
	checked := 0

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
			demanded := map[string]token.Pos{}
			covered := map[string]bool{}

			ast.Inspect(fn.Body, func(n ast.Node) bool {
				call, ok := n.(*ast.CallExpr)
				if !ok {
					return true
				}
				name := calleeName(call)
				switch {
				case asOfSnapshotReads[name] && len(call.Args) > 0:
					if c := cutoffName(call.Args[len(call.Args)-1]); c != "" {
						demanded[c] = call.Pos()
					}
				case asOfUniverseReads[name]:
					for _, arg := range call.Args {
						if c := cutoffName(arg); c != "" {
							covered[c] = true
						}
					}
				}
				return true
			})

			for cutoff, pos := range demanded {
				checked++
				if !covered[cutoff] {
					offenders = append(offenders, rel+"#"+fn.Name.Name+
						": snapshots read as of `"+cutoff+"` ("+fset.Position(pos).String()+
						") but no account universe is resolved at `"+cutoff+"`")
				}
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walking %s: %v", root, err)
	}

	sort.Strings(offenders)
	if len(offenders) > 0 {
		t.Errorf("a point-in-time snapshot read is being interpreted by an account universe "+
			"resolved at a DIFFERENT instant — almost always `status = 'active'` today (#1569 C2).\n"+
			"The balance comes from the snapshot; the universe decides whether it is counted at "+
			"all, so the figure comes back silently too small.\n  %s",
			strings.Join(offenders, "\n  "))
	}

	// Vacuity floor. If the snapshot reads are renamed or moved, this guard would pass over an
	// empty universe of its own. The count is the number of (function, cutoff) pairs actually
	// judged, and it must not silently fall to zero.
	if checked == 0 {
		t.Error("this guard examined ZERO snapshot-read cutoffs — asOfSnapshotReads no longer " +
			"matches anything in internal/services, so it is passing vacuously")
	}
}

// servicesPackageRoot returns the absolute path of internal/services.
func servicesPackageRoot(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	return wd
}
