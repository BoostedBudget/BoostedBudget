package main

// TestCacheInvalidationSetterWiringNoDrift is the general form of the "dead-setter" bug class
// Issue #1531 Task 10b-2a-iii actually hit: it added SetCacheInvalidation bumps to
// EffectiveCategoryRefreshWorker and HouseholdOverCapacityCheckWorker, every unit test for both
// passed (each test constructs the worker directly and calls the setter itself), but main.go
// never called either setter — both fixes were dead code in production while CI stayed green.
// That was caught by chance during review, not by any test.
//
// This guard generalizes the fix so the NEXT such setter cannot ship inert by chance either.
// Rather than hardcoding "these five setters must be called" (which itself needs updating by
// hand every time a new bump is added — the exact step that was skipped last time), it:
//
//  1. Scans every non-test .go file under the backend module for a method literally named
//     SetCacheInvalidation, and records the receiver's type name. This is the "family" of types
//     this guard polices — anything with this exact method name, wherever it lives.
//  2. For each such type, looks for a call to a constructor named "New<TypeName>" anywhere in
//     main.go (any package qualifier). If main.go never calls it, that type is not "constructed
//     in main.go" and is out of scope for this guard (nothing to wire).
//  3. If it IS constructed, finds every variable name main.go binds to that constructor's
//     result (an AssignStmt or ValueSpec whose matching Rhs/Values slot is exactly that call).
//  4. Requires that AT LEAST ONE of those variable names has `<name>.SetCacheInvalidation(...)`
//     called somewhere in main.go.
//
// This is a naming-convention scan, not a type-checked one (no go/types importer, no new
// dependency) — see the "What this does NOT prove" section below for exactly what that costs.
//
// WHAT THIS DOES NOT PROVE (read before trusting a green run):
//   - It does NOT prove the setter is called with the CORRECT arguments — SetCacheInvalidation
//     (cacheInstance.Generations()) and SetCacheInvalidation(nil) both satisfy it equally, and
//     transactionSyncService.SetCacheInvalidation's second (householdRepo) argument is not
//     inspected at all.
//   - It does NOT prove the setter is called BEFORE the constructed value is used — a call
//     placed after the worker has already started processing (or after River registration)
//     would still satisfy this guard.
//   - It does NOT prove the call runs on every code path (e.g. inside an `if cfg.X != ""` block
//     that main.go may never enter) — only that the CallExpr is present in the source at all.
//   - It relies on the "New<TypeName>" constructor-naming convention holding. A type constructed
//     through a factory, a builder method, or a constructor not named exactly "New"+TypeName will
//     not be recognised as "constructed in main.go" and will silently fall outside this guard's
//     scope (a false negative, not a false positive) until it is added to
//     cacheInvalidationExemptions or its constructor is renamed to match.
//   - It is FILE-LEVEL/NAME-LEVEL, not control-flow-level: it cannot tell a real wiring call from
//     one buried in dead code (an `if false` block, an unreachable branch) — see
//     issue1477_sweep_cursor_wiring_drift_test.go's header for the deeper bypasses that class of
//     guard closes, none of which this one attempts. This guard exists to catch the ONE bug class
//     that has actually happened (setter simply never called at all), not every conceivable
//     wiring bypass.
//
// EXEMPTIONS: if a type legitimately should not be wired in main.go (e.g. it exists purely as a
// test seam and main.go never constructs a production instance of it, or SetCacheInvalidation is
// optional for that type and main.go intentionally leaves it unset), add an entry to
// cacheInvalidationExemptions with a non-empty reason. An empty reason fails
// TestCacheInvalidationExemptionsHaveReasons — matching how the Task 10b-3 cache-completeness
// guards (internal/queue/jobs/cache_completeness_jobs_test.go,
// internal/repository/cache_completeness_repo_test.go) require a mandatory, non-empty
// dispositionNoCacheImpact{reason} rather than a bare boolean skip.
//
// ADVERSARIAL VALIDATION (recorded 2026-08-14, see task-10b-4-report.md for the full transcript):
// commented out `householdOverCapacityCheckWorker.SetCacheInvalidation(cacheInstance.Generations())`
// (main.go ~:905) — this test FAILED, naming HouseholdOverCapacityCheckWorker specifically and
// listing householdOverCapacityCheckWorker as the unwired variable. Restored the line — this test
// PASSED again.

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"unicode"
)

// cacheInvalidationExemptions lists types that expose SetCacheInvalidation, ARE constructed in
// main.go (by the New<TypeName> convention this guard follows), but are deliberately not wired
// there. Every entry MUST carry a non-empty reason — enforced by
// TestCacheInvalidationExemptionsHaveReasons below.
var cacheInvalidationExemptions = map[string]string{
	// (none at time of writing — all five known SetCacheInvalidation types are wired)
}

// setCacheInvalidationMethodName is the exact method name this guard scans for. Any type
// exposing a method with this literal name is in-scope, regardless of its package.
const setCacheInvalidationMethodName = "SetCacheInvalidation"

// findCacheInvalidationTypes walks every non-test .go file under root (the backend module root)
// and returns the set of type names that declare a method named setCacheInvalidationMethodName.
// It intentionally does not resolve which PACKAGE each type lives in: two types of the same name
// in different packages would be conflated, but no such collision exists today (verified by hand
// against the five known types), and main.go's own constructor-name scan below is what actually
// disambiguates which constructor is being looked for.
func findCacheInvalidationTypes(t *testing.T, root string) map[string]bool {
	t.Helper()
	types := map[string]bool{}
	fset := token.NewFileSet()

	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			switch d.Name() {
			case ".git", "node_modules", "vendor":
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}

		file, perr := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
		if perr != nil {
			// A file that fails to parse is a build break the compiler will already catch;
			// don't let this scan mask it as "no SetCacheInvalidation methods found".
			return fmt.Errorf("parsing %s: %w", path, perr)
		}

		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Recv == nil || fn.Name.Name != setCacheInvalidationMethodName {
				continue
			}
			if len(fn.Recv.List) != 1 {
				continue
			}
			typeName := receiverTypeName(fn.Recv.List[0].Type)
			if typeName != "" {
				types[typeName] = true
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walking %s for SetCacheInvalidation methods: %v", root, err)
	}
	return types
}

// upperFirstRune upper-cases the first rune of s, leaving the rest untouched — "accountService"
// becomes "AccountService". Used to bridge an (often unexported) receiver type name to the
// exported "New<TypeName>" constructor convention this guard follows.
func upperFirstRune(s string) string {
	if s == "" {
		return s
	}
	r := []rune(s)
	r[0] = unicode.ToUpper(r[0])
	return string(r)
}

// receiverTypeName extracts "Foo" from either `Foo` or `*Foo` receiver type expressions.
func receiverTypeName(expr ast.Expr) string {
	switch e := expr.(type) {
	case *ast.Ident:
		return e.Name
	case *ast.StarExpr:
		return receiverTypeName(e.X)
	default:
		return ""
	}
}

// mainGoCallsConstructor reports whether main.go's AST contains a call to a function named
// ctorName, via any selector (pkg.CtorName(...)) — the package qualifier itself is not checked,
// matching this guard's stated naming-convention-only scope.
func mainGoCallsConstructor(file *ast.File, ctorName string) bool {
	found := false
	ast.Inspect(file, func(n ast.Node) bool {
		if found {
			return false
		}
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || sel.Sel.Name != ctorName {
			return true
		}
		found = true
		return false
	})
	return found
}

// varNamesBoundToConstructor returns every identifier name main.go binds (via `:=`, `=`, or
// `var ... = `) to a call whose function selector is named ctorName. It only follows the ONE
// hop from "constructor call" to "the variable holding its result" — it does not follow the
// value through any further reassignment or field extraction.
func varNamesBoundToConstructor(file *ast.File, ctorName string) []string {
	var names []string
	isCtorCall := func(e ast.Expr) bool {
		call, ok := e.(*ast.CallExpr)
		if !ok {
			return false
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		return ok && sel.Sel.Name == ctorName
	}

	ast.Inspect(file, func(n ast.Node) bool {
		switch node := n.(type) {
		case *ast.AssignStmt:
			if len(node.Lhs) == len(node.Rhs) {
				for i, rhs := range node.Rhs {
					if !isCtorCall(rhs) {
						continue
					}
					if id, ok := node.Lhs[i].(*ast.Ident); ok {
						names = append(names, id.Name)
					}
				}
			}
		case *ast.ValueSpec:
			if len(node.Names) == len(node.Values) {
				for i, val := range node.Values {
					if !isCtorCall(val) {
						continue
					}
					names = append(names, node.Names[i].Name)
				}
			}
		}
		return true
	})
	return names
}

// varCallsSetCacheInvalidation reports whether main.go contains `<varName>.SetCacheInvalidation(`
// for the given varName, anywhere (any argument shape — see the file header for why argument
// correctness is explicitly out of scope for this guard).
func varCallsSetCacheInvalidation(file *ast.File, varName string) bool {
	found := false
	ast.Inspect(file, func(n ast.Node) bool {
		if found {
			return false
		}
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || sel.Sel.Name != setCacheInvalidationMethodName {
			return true
		}
		id, ok := sel.X.(*ast.Ident)
		if !ok || id.Name != varName {
			return true
		}
		found = true
		return false
	})
	return found
}

func TestCacheInvalidationSetterWiringNoDrift(t *testing.T) {
	backendRoot := filepath.Join("..", "..")
	types := findCacheInvalidationTypes(t, backendRoot)

	if len(types) == 0 {
		t.Fatal("found ZERO types anywhere in the backend module with a SetCacheInvalidation " +
			"method — the scan is vacuous, not clean. Either every cache-invalidation setter was " +
			"removed (update this test) or the scan itself broke (e.g. the module root moved).")
	}

	fset := token.NewFileSet()
	mainFile, err := parser.ParseFile(fset, "main.go", nil, parser.SkipObjectResolution)
	if err != nil {
		t.Fatalf("could not parse main.go: %v", err)
	}

	sortedTypes := make([]string, 0, len(types))
	for typeName := range types {
		sortedTypes = append(sortedTypes, typeName)
	}
	sort.Strings(sortedTypes)

	for _, typeName := range sortedTypes {
		typeName := typeName
		t.Run(typeName, func(t *testing.T) {
			// Issue #1531 Slice B / M3: typeName is the receiver identifier VERBATIM. Five of
			// the seven known SetCacheInvalidation receivers are unexported (accountService,
			// householdService, transactionSyncService, investmentSyncService,
			// liabilitySyncService) — "New"+typeName for those produced "NewaccountService" etc.,
			// which never matches the real constructor (services.NewAccountService), so
			// mainGoCallsConstructor always returned false and this subtest hit the early
			// `return` below before asserting anything. Proven by mutation: commenting out
			// main.go's transactionSyncService.SetCacheInvalidation(...) left all seven subtests
			// green. Upper-casing the type name's first rune before prefixing matches the actual
			// New<TypeName> convention (verified against every constructor in
			// internal/services: NewAccountService, NewHouseholdService,
			// NewTransactionSyncService, NewInvestmentSyncService, NewLiabilitySyncService).
			ctorName := "New" + upperFirstRune(typeName)

			if !mainGoCallsConstructor(mainFile, ctorName) {
				// Not constructed in main.go by the New<TypeName> convention — out of scope.
				// (This is also what a legitimately-exempt, main.go-never-constructs-it type
				// looks like; no exemption entry is needed for those, only for types main.go
				// DOES construct but should not wire.)
				return
			}

			if reason, exempt := cacheInvalidationExemptions[typeName]; exempt {
				if reason == "" {
					t.Fatalf("cacheInvalidationExemptions[%q] has an empty reason — "+
						"see TestCacheInvalidationExemptionsHaveReasons", typeName)
				}
				return
			}

			varNames := varNamesBoundToConstructor(mainFile, ctorName)
			if len(varNames) == 0 {
				t.Fatalf("main.go calls %s(...) but this guard could not identify a variable "+
					"bound to its result (e.g. it's passed inline without assignment, or the "+
					"binding shape isn't a plain `name := %s(...)` / `name = %s(...)`) — cannot "+
					"verify SetCacheInvalidation wiring for %s. Either bind the result to a "+
					"named variable or add an exemption entry with a reason.",
					ctorName, ctorName, ctorName, typeName)
			}

			for _, varName := range varNames {
				if varCallsSetCacheInvalidation(mainFile, varName) {
					return
				}
			}

			t.Errorf("WIRING DRIFT: %s has a SetCacheInvalidation method and is constructed in "+
				"main.go via %s(...) (bound to %s), but main.go never calls "+
				"%s.SetCacheInvalidation(...). This is the exact dead-setter bug class Issue #1531 "+
				"Task 10b-2a-iii hit: the fix's own unit tests call the setter directly and pass, "+
				"while production never wires it. Add the call, or if this type deliberately "+
				"should not be wired, add cacheInvalidationExemptions[%q] with a reason.",
				typeName, ctorName, strings.Join(varNames, ", "), varNames[0], typeName)
		})
	}
}

// TestCacheInvalidationExemptionsHaveReasons guards the exemption map itself: an exemption with
// an empty reason is indistinguishable from "nobody looked", the same failure mode the Task
// 10b-3 cache-completeness guards' dispositionNoCacheImpact{reason} enforce.
func TestCacheInvalidationExemptionsHaveReasons(t *testing.T) {
	for typeName, reason := range cacheInvalidationExemptions {
		if strings.TrimSpace(reason) == "" {
			t.Errorf("cacheInvalidationExemptions[%q] has an empty reason", typeName)
		}
	}
}
