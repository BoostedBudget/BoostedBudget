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

func TestNoAnalyticsReadResolvesAGroupingKeyThroughThePFCRegistry(t *testing.T) {
	entries, err := os.ReadDir(".")
	require.NoError(t, err)

	fset := token.NewFileSet()
	var offenders []string
	scanned := 0

	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		src, readErr := os.ReadFile(name)
		require.NoError(t, readErr)
		parsed, parseErr := parser.ParseFile(fset, name, src, parser.ParseComments)
		require.NoError(t, parseErr, "%s must parse", name)
		scanned++

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

	require.NotZero(t, scanned, "the guard must actually have files to scan — a zero here means the "+
		"walk broke and the guard silently stopped guarding, which is the failure mode this file's "+
		"header is about")

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
// guard's blindness to other files was as much of the vacuity as its argument allowlist. If this
// ever finds fewer than the handful of files that actually define these reads, the walk has broken.
func TestDisplayLeakGuardScansTheWholePackage(t *testing.T) {
	entries, err := os.ReadDir(".")
	require.NoError(t, err)

	present := map[string]bool{}
	for _, e := range entries {
		if !e.IsDir() {
			present[e.Name()] = true
		}
	}

	// The files that host a category-shaped analytics read or the display resolution itself. The
	// first attempt at this guard scanned only the first one.
	for _, must := range []string{
		"analytics_service.go",
		"analytics_service_cached.go",
		"analytics_category_hierarchy.go",
		"analytics_category_display.go",
		"plaid_categories.go",
	} {
		require.True(t, present[must],
			"%s no longer exists in this package; the guard's coverage claim is stale and its "+
				"file list must be re-derived rather than quietly shrinking", filepath.Base(must))
	}
}
