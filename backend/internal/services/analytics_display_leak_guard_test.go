package services

// Issue #1626 — the forward-looking guard for the UUID-label defect class.
//
// The reported bug was ONE symptom of a mechanism that existed in SIX places: an analytics read
// groups by a synthetic key (`custom:<uuid>` for a custom override) and something hands that key
// straight to the PFC registry, which title-cases anything it does not recognise. The UUID then IS
// the category's name, on screen and in the API.
//
// Fixing the six call sites does not stop a seventh. Every one of them looked reasonable in
// isolation, none had a test that fed a custom token, and the whole suite stayed green throughout —
// so review and coverage both demonstrably failed to catch this class. Hence a mechanical guard:
// resolveCategoryDisplay is the single choke point, and this fails when anything else reaches the
// registry with an argument that is not provably a registry value.
//
// ⚠ THE POLARITY IS DELIBERATE, and it is the correction of a near-vacuous first attempt. That
// version listed five hardcoded argument SPELLINGS ("row.Category", "c.Category", …) and scanned
// ONE file, so it fired only on a literal copy-paste of a site that had just been fixed:
// `PFCPrimaryDisplayName(key)`, `(item.Category)`, a two-argument call, or the same mistake made in
// analytics_service_cached.go or analytics_category_hierarchy.go all sailed past it. What it
// actually guarded was not the class at all. This version inverts it — EVERY argument to a
// registry-facing function must be on an allowlist of provably-safe expressions, across EVERY
// non-test file in the package — so a new caller has to be spelled in a provably safe way or fail.
//
// WHAT IS ALLOWED, and why each form cannot be a grouping key:
//   - a string literal                       — written by us, not derived from a row
//   - `pfc.Something`                        — a registry constant
//   - `x.Enum` / `x.PrimaryEnum` / `x.DetailedEnum` — the enum fields of pfc.Primary / pfc.Detailed,
//     which the registry itself populated
//
// A grouping key is none of these: it arrives as a plain identifier or as a field like
// `row.Category`, and is therefore rejected by construction rather than by enumeration.

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// registryEntryPoints are the functions that can turn a string into a display name by consulting
// (or falling back past) the PFC registry. Both the package-local wrappers and the direct
// `pfc.*` forms are listed: banning only the wrappers would leave "call the registry directly" as
// an open bypass.
var registryEntryPoints = map[string]bool{
	"PFCPrimaryDisplayName":   true, // package wrapper, plaid_categories.go
	"PFCDetailedDisplayName":  true, // package wrapper; DetailedDisplayName title-cases an unknown suffix too
	"pfcDisplayName":          true, // analytics_service.go's primary-or-detailed resolver
	"pfc.PrimaryDisplayName":  true,
	"pfc.DetailedDisplayName": true,
}

// chokePointFuncs are the functions whose whole job IS to consult the registry, so a call inside
// one of them is the guarded path rather than a bypass of it. resolveCategoryDisplay short-circuits
// custom tokens BEFORE the registry sees them; the other three are the thin wrappers it and the
// callers outside this package go through.
var chokePointFuncs = map[string]bool{
	"resolveCategoryDisplay": true,
	"pfcDisplayName":         true,
	"PFCPrimaryDisplayName":  true,
	"PFCDetailedDisplayName": true,
}

// safeRegistryEnumFields are the enum-bearing fields of pfc.Primary / pfc.Detailed. A value read
// out of a registry entry cannot be a `custom:<uuid>` grouping key.
var safeRegistryEnumFields = map[string]bool{
	"Enum":         true,
	"PrimaryEnum":  true,
	"DetailedEnum": true,
}

// registryCalleeName renders a call's function as either `Name` or `pkg.Name`, and "" for anything else
// (a method value, a func literal, an index expression). "" is never in registryEntryPoints, so an
// exotic callee is simply not this guard's business.
func registryCalleeName(fun ast.Expr) string {
	switch f := fun.(type) {
	case *ast.Ident:
		return f.Name
	case *ast.SelectorExpr:
		if x, ok := f.X.(*ast.Ident); ok {
			return x.Name + "." + f.Sel.Name
		}
	}
	return ""
}

// isProvablySafeArg reports whether an argument expression cannot possibly be an analytics
// grouping key. It is deliberately conservative: anything it cannot prove is reported.
func isProvablySafeArg(arg ast.Expr) bool {
	switch a := arg.(type) {
	case *ast.BasicLit:
		return a.Kind == token.STRING
	case *ast.SelectorExpr:
		if x, ok := a.X.(*ast.Ident); ok && x.Name == "pfc" {
			return true // a registry constant or variable
		}
		return safeRegistryEnumFields[a.Sel.Name]
	}
	return false
}

// scanForDisplayLeaks IS THE GUARD. It is a helper rather than an inline loop for exactly one
// reason: the meta-test below has to be able to invoke the real scanner and inspect what it looked
// at (#1626 Phase 2, review finding X5). The version of that meta-test this replaces only checked
// that five filenames EXIST on disk, so it passed unchanged against the pre-fix ONE-FILE scanner —
// a test whose entire purpose was to prove the widening had happened, and which could not tell
// whether it had. Returning the scanned set is what makes the claim checkable at all.
//
// It returns (scanned, offenders): every non-test Go file in the package directory it actually
// parsed, and every registry call whose argument it could not prove safe.
func scanForDisplayLeaks(t *testing.T) (scanned []string, offenders []string) {
	t.Helper()

	entries, err := os.ReadDir(".")
	require.NoError(t, err)

	fset := token.NewFileSet()

	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		src, readErr := os.ReadFile(name)
		require.NoError(t, readErr)
		parsed, parseErr := parser.ParseFile(fset, name, src, parser.ParseComments)
		require.NoError(t, parseErr, "%s must parse", name)
		scanned = append(scanned, name)

		for _, decl := range parsed.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Body == nil || chokePointFuncs[fn.Name.Name] {
				continue
			}
			ast.Inspect(fn.Body, func(n ast.Node) bool {
				call, isCall := n.(*ast.CallExpr)
				if !isCall || !registryEntryPoints[registryCalleeName(call.Fun)] {
					return true
				}
				for _, arg := range call.Args {
					if isProvablySafeArg(arg) {
						continue
					}
					text := strings.TrimSpace(string(src[fset.Position(arg.Pos()).Offset:fset.Position(arg.End()).Offset]))
					offenders = append(offenders, name+":"+strconv.Itoa(fset.Position(call.Pos()).Line)+
						"  in "+fn.Name.Name+"()  "+registryCalleeName(call.Fun)+"(… "+text+" …)")
				}
				return true
			})
		}
	}
	return scanned, offenders
}

func TestNoAnalyticsReadResolvesAGroupingKeyThroughThePFCRegistry(t *testing.T) {
	scanned, offenders := scanForDisplayLeaks(t)

	require.NotEmpty(t, scanned, "the guard must actually have files to scan — an empty set here "+
		"means the walk broke and the guard silently stopped guarding, which is the failure mode "+
		"this file's header is about")

	require.Empty(t, offenders,
		"an argument that is not provably a PFC registry value reached the registry (#1626). If it "+
			"is a grouping KEY, then for a custom-categorised row it is `custom:<uuid>`, the registry "+
			"misses it, and enumToTitleCase turns the UUID into the category's NAME — on screen and "+
			"in the API. Route it through resolveCategoryDisplay(key, storedDisplay), which "+
			"short-circuits custom tokens before the registry ever sees them. If the value genuinely "+
			"cannot be a grouping key, spell it as a form this guard can prove safe (a `pfc.*` value, "+
			"a string literal, or an .Enum/.PrimaryEnum/.DetailedEnum field) rather than widening the "+
			"allowlist. Offenders:\n  %s",
		strings.Join(offenders, "\n  "))
}

// TestDisplayLeakGuardScansTheWholePackage pins the second half of the correction above: the
// guard's blindness to other files was as much of the vacuity as its argument allowlist.
//
// ⚠ IT INVOKES THE SCANNER, AND THAT IS THE WHOLE POINT (#1626 Phase 2, review finding X5). The
// version this replaces only asserted that five filenames EXIST in the package directory. Those
// files all existed before the scanner was widened, so every assertion in it passed against the
// pre-fix one-file implementation — a test added as the proof that the widening had happened, which
// could not distinguish the widened scanner from the narrow one. Third vacuous test on this branch,
// and the same class as the other two: asserting a precondition of the fix instead of the fix.
//
// What it asserts now is the scanner's OWN report of what it read: the set it scanned must be
// EXACTLY this package's non-test Go files, derived here independently. Set equality rather than
// containment, in both directions, so neither a shrunken walk (the original defect) nor a walk that
// wandered outside the package or began swallowing _test.go files can pass.
func TestDisplayLeakGuardScansTheWholePackage(t *testing.T) {
	scanned, _ := scanForDisplayLeaks(t)

	entries, err := os.ReadDir(".")
	require.NoError(t, err)
	var want []string
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		want = append(want, name)
	}
	require.NotEmpty(t, want, "this package must contain non-test Go files for the guard to scan")

	assert.ElementsMatch(t, want, scanned,
		"the guard must scan EVERY non-test Go file in the package and nothing else — %d such files "+
			"exist and the scanner reported %d. A scanner narrower than the package is how this leak "+
			"class survived its first guard", len(want), len(scanned))

	// And, named explicitly, the files that host a category-shaped analytics read or the display
	// resolution itself. Set equality above already covers them; naming them keeps the failure
	// legible when one is renamed or moved, so the reader is told which coverage claim went stale
	// instead of being handed a diff of two long lists. The first attempt at this guard scanned only
	// the first one.
	for _, must := range []string{
		"analytics_service.go",
		"analytics_service_cached.go",
		"analytics_category_hierarchy.go",
		"analytics_category_display.go",
		"plaid_categories.go",
	} {
		assert.Contains(t, scanned, must,
			"%s was not scanned by the guard; if it was renamed or moved, re-derive the guard's "+
				"coverage claim rather than letting it quietly shrink", filepath.Base(must))
	}
}
