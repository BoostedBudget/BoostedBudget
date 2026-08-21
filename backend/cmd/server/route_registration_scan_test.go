package main

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"strings"
	"testing"
)

// Route-registration scanning for the #1560 ordering guard, rebuilt on go/ast.
//
// WHY AST AND NOT TEXT. The original scan read main.go as text and matched
// `operations.Register[A-Za-z]+Routes\(` with a regex. That made it blind in exactly the way the
// guard exists to prevent, and I verified the hole rather than taking it on report:
//
//	// operations.RegisterDeletionRoutes(humaAPI, humaDeletionHandler)
//	_ = humaDeletionHandler
//
// With that in main.go the endpoint 404s and the guard PASSED. `requiredLateRouteGroups` was added
// specifically to catch a deleted registration, and it did not catch a commented-out one, because the
// commented text still matched the regex — so the "is it registered at all?" check was satisfied by a
// comment. (The naive version, commenting the line without `_ =`, does not compile; the compiling
// variant above is what a careless refactor actually produces, and it is the one that ships.)
//
// A comment is not part of the AST. Parsing removes this entire class rather than patching one
// instance of it — which matters, because the same class had already bitten this branch twice in
// prose-scanning guards and been patched locally both times.
//
// It also fixes three narrower holes for free:
//   - the anchor took the FIRST line matching each middleware regex, so a comment mentioning
//     `UseMiddleware(humamiddleware.NewCacheInvalidationMiddleware(...))` earlier in the file dragged
//     the anchor backwards and opened a window. It now takes the LAST real call.
//   - `operations.` was required as the qualifier, so `mcp.RegisterFooRoutes(api, …)` was invisible.
//     Any selector matching Register…Routes is now seen, whatever the package.
//   - the bare-`huma.Register` check additionally required the identifier `humaAPI`, so a renamed
//     variable escaped. Any `huma.Register(...)` is now reported.
//
// REMAINING LIMIT, stated because the guard cannot see it: this is still LEXICAL order, not execution
// order. `outsideMain` is what keeps that PARTLY honest — a Register…Routes call inside a helper could
// run before the middleware while appearing after it in the file, so a helper-hosted registration is
// refused and registration directly in func main() is required.
//
// Be precise about how far that reaches, because an earlier, broader claim here ("the guard refuses
// the shape outright") is the reason three fail-open gaps survived review. What is refused is a
// registration this scan can NAME: a Register…Routes call, or a huma.Register call, appearing
// somewhere in the parsed file. The scan does not resolve calls, so a helper whose NAME says nothing
// — `wireExtras(humaAPI)` — is refused only because its body, wherever it lives in package main, is
// itself scanned and its registrations reported (see siblingFileRegistrationProblems in
// issue1560_route_middleware_ordering_test.go, which now covers bareRegisters too). A registration
// reached through a function value, an interface method, or a package outside cmd/server is still
// invisible. Verifying real execution order would mean extracting a buildAPI() seam from a 2000-line
// main() — a refactor for its own PR, not a test change.
//
// All 38 of main.go's registrations are direct calls in func main(), so none of this constrains
// anything that exists today.

// routeRegistration is one Register…Routes call found by the AST scan.
type routeRegistration struct {
	group string // the selector name, e.g. RegisterDeletionRoutes
	pkg   string // the qualifier, e.g. operations
	api   string // first argument as written, e.g. humaAPI
	line  int
}

// sourceScan is everything the ordering guard needs from one Go file.
type sourceScan struct {
	middleware    map[string]int      // label -> line of its LAST UseMiddleware call
	anchor        int                 // max over middleware lines
	anchorLabel   string              //
	calls         []routeRegistration // Register…Routes calls inside func main()
	outsideMain   []routeRegistration // ...and those that are not
	bareRegisters []int               // huma.Register(...) lines, any receiver

	// unrecognisedMiddleware is every UseMiddleware call whose first argument matches NONE of
	// requestScopedMiddlewareMarkers. Callers must fail on a non-empty result: the anchor is a max
	// over KNOWN markers, so an unknown middleware added after the last known one is a link the
	// anchor cannot see, and routes registered ahead of it pass while missing it.
	unrecognisedMiddleware []unrecognisedMiddlewareCall
}

// unrecognisedMiddlewareCall is one UseMiddleware call the marker list does not describe.
type unrecognisedMiddlewareCall struct {
	line int
	arg  string // source text of the first argument, so the failure names what to add
}

// requestScopedMiddlewareMarkers identifies EVERY middleware installed on humaAPI. Matched against
// the SOURCE TEXT OF THE FIRST ARGUMENT of a real UseMiddleware call node, so a mention in a comment
// cannot contribute.
//
// This list must be COMPLETE, not merely representative, because the anchor is a max over it: a
// middleware missing from here is a link the anchor cannot see, and every route registered ahead of
// it passes the guard while running without it. That is not hypothetical — `humaAdminMiddleware` was
// main.go's fourth UseMiddleware call and was deliberately left out of this list (on the reasoning
// that it self-scopes to /api/v1/admin), which left lines 1893-2017 as a window in which a route
// could register with no admin RBAC. Completeness is now ENFORCED rather than trusted:
// scanGoSource records any UseMiddleware argument matching none of these markers in
// unrecognisedMiddleware, and the ordering guard fails on it. Adding a middleware to main.go
// therefore fails this suite until it is added here too.
var requestScopedMiddlewareMarkers = []struct{ label, marker string }{
	{"clerk", "humaClerkMiddleware"},
	{"read-only", "NewReadOnlyMiddleware"},
	{"cache-invalidation", "NewCacheInvalidationMiddleware"},
	{"admin", "humaAdminMiddleware"},
}

func scanGoSource(filename, src string) (sourceScan, error) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, filename, src, 0)
	if err != nil {
		return sourceScan{}, fmt.Errorf("parse %s: %w", filename, err)
	}

	out := sourceScan{middleware: map[string]int{}}

	// Body range of func main, used to classify registrations as in-main or not.
	var mainLbrace, mainRbrace token.Pos
	for _, d := range f.Decls {
		fn, ok := d.(*ast.FuncDecl)
		if ok && fn.Recv == nil && fn.Name.Name == "main" && fn.Body != nil {
			mainLbrace, mainRbrace = fn.Body.Lbrace, fn.Body.Rbrace
		}
	}

	// Exact source text of a node. Safe because we parsed this very string.
	text := func(n ast.Node) string {
		a, b := fset.Position(n.Pos()).Offset, fset.Position(n.End()).Offset
		if a < 0 || b > len(src) || a > b {
			return ""
		}
		return src[a:b]
	}

	ast.Inspect(f, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		// Unwrap an explicit generic instantiation before looking at the callee. `huma.Register(...)`
		// has Fun of type *ast.SelectorExpr, but `huma.Register[In, Out](...)` — legal, and what
		// gopls writes when it cannot infer — has *ast.IndexListExpr (or *ast.IndexExpr for a single
		// type argument) wrapping it. Failing to unwrap made the explicit form invisible.
		fun := call.Fun
		for {
			switch ix := fun.(type) {
			case *ast.IndexExpr:
				fun = ix.X
				continue
			case *ast.IndexListExpr:
				fun = ix.X
				continue
			}
			break
		}

		// A call target is either qualified (pkg.Name) or a bare same-package identifier. Inspecting
		// only *ast.SelectorExpr meant an UNQUALIFIED `RegisterFooRoutes(humaAPI, h)` was recorded in
		// none of calls/outsideMain/bareRegisters — the guard's blindest spot, since a same-package
		// registrar is exactly what a "tidy up main.go" refactor produces.
		var name, qualifier string
		switch fn := fun.(type) {
		case *ast.SelectorExpr:
			name = fn.Sel.Name
			if x, ok := fn.X.(*ast.Ident); ok {
				qualifier = x.Name
			}
		case *ast.Ident:
			name = fn.Name
		default:
			return true
		}
		line := fset.Position(call.Pos()).Line

		switch {
		case name == "UseMiddleware":
			if len(call.Args) == 0 {
				return true
			}
			arg := text(call.Args[0])
			var known bool
			for _, m := range requestScopedMiddlewareMarkers {
				if !strings.Contains(arg, m.marker) {
					continue
				}
				known = true
				if line > out.middleware[m.label] {
					out.middleware[m.label] = line
				}
			}
			if !known {
				// Fail-CLOSED on the unknown. The anchor is a max over KNOWN markers, so a
				// middleware nobody taught this guard about is a link the anchor cannot see; every
				// route registered ahead of it then passes while missing it. Recording it here is
				// what stops the NEXT omission rather than only the one already found (the admin
				// middleware went unwatched from the day it was added).
				out.unrecognisedMiddleware = append(out.unrecognisedMiddleware,
					unrecognisedMiddlewareCall{line: line, arg: strings.TrimSpace(arg)})
			}

		case name == "Register" && qualifier == "huma":
			// Any huma.Register, on any api identifier.
			out.bareRegisters = append(out.bareRegisters, line)

		case strings.HasPrefix(name, "Register") && strings.HasSuffix(name, "Routes"):
			reg := routeRegistration{group: name, line: line, pkg: qualifier}
			if len(call.Args) > 0 {
				reg.api = strings.TrimSpace(text(call.Args[0]))
			}
			if mainLbrace == token.NoPos || call.Pos() < mainLbrace || call.Pos() > mainRbrace {
				out.outsideMain = append(out.outsideMain, reg)
			} else {
				out.calls = append(out.calls, reg)
			}
		}
		return true
	})

	for _, m := range requestScopedMiddlewareMarkers {
		if l := out.middleware[m.label]; l > out.anchor {
			out.anchor, out.anchorLabel = l, m.label
		}
	}
	return out, nil
}

// missingMiddleware reports the request-scoped middlewares the scan could not find. An unanchored
// ordering check is worse than no check, so callers must fail on a non-empty result.
func (s sourceScan) missingMiddleware() []string {
	var out []string
	for _, m := range requestScopedMiddlewareMarkers {
		if s.middleware[m.label] == 0 {
			out = append(out, m.marker)
		}
	}
	return out
}

// TestRouteRegistrationScanIsNotFooledByComments is the regression test for the verified hole.
//
// Every case is a mutation of the real shape in main.go, held as a fixture string rather than applied
// to main.go itself — mutating the real file to prove a test works leaves the repo one failed cleanup
// away from a corrupted main.go, and this session has already lost uncommitted work that way.
func TestRouteRegistrationScanIsNotFooledByComments(t *testing.T) {
	const preamble = `package main

func main() {
	humaAPI.UseMiddleware(humaClerkMiddleware)
	humaAPI.UseMiddleware(humamiddleware.NewReadOnlyMiddleware(householdEnabled))
	humaAPI.UseMiddleware(humamiddleware.NewCacheInvalidationMiddleware(gens, hh, log))
`

	t.Run("a commented-out registration is NOT a registration", func(t *testing.T) {
		// THE VERIFIED HOLE. Pre-fix, the regex matched the comment and the guard reported
		// RegisterDeletionRoutes as present while /api/v1/user/delete returned 404.
		scan, err := scanGoSource("main.go", preamble+`
	// operations.RegisterDeletionRoutes(humaAPI, humaDeletionHandler)
	_ = humaDeletionHandler
}
`)
		if err != nil {
			t.Fatalf("scan: %v", err)
		}
		for _, c := range scan.calls {
			if c.group == "RegisterDeletionRoutes" {
				t.Errorf("a commented-out registration was counted as real (main.go:%d) — the guard "+
					"would report the endpoint as wired while it 404s. This is the #1560 defect.", c.line)
			}
		}
	})

	t.Run("a comment mentioning a middleware does not move the anchor", func(t *testing.T) {
		// Pre-fix the anchor took the FIRST matching line, so this comment pulled it backwards and
		// opened a window in which a secured mutating route could register uninvalidated.
		withComment := `package main

func main() {
	// TODO: humaAPI.UseMiddleware(humamiddleware.NewCacheInvalidationMiddleware(gens, hh, log))
	humaAPI.UseMiddleware(humaClerkMiddleware)
	humaAPI.UseMiddleware(humamiddleware.NewReadOnlyMiddleware(householdEnabled))
	humaAPI.UseMiddleware(humamiddleware.NewCacheInvalidationMiddleware(gens, hh, log))
	operations.RegisterDeletionRoutes(humaAPI, h)
}
`
		scan, err := scanGoSource("main.go", withComment)
		if err != nil {
			t.Fatalf("scan: %v", err)
		}
		if got, want := scan.anchor, 7; got != want {
			t.Errorf("anchor = %d, want %d (the LAST real UseMiddleware). A comment on line 4 must "+
				"not anchor the chain — that would declare the chain complete three lines early.",
				got, want)
		}
	})

	t.Run("duplicate middleware calls anchor on the last, not the first", func(t *testing.T) {
		scan, err := scanGoSource("main.go", `package main

func main() {
	humaAPI.UseMiddleware(humaClerkMiddleware)
	humaAPI.UseMiddleware(humamiddleware.NewReadOnlyMiddleware(x))
	humaAPI.UseMiddleware(humamiddleware.NewCacheInvalidationMiddleware(a, b, c))
	operations.RegisterFooRoutes(humaAPI, h)
	humaAPI.UseMiddleware(humamiddleware.NewCacheInvalidationMiddleware(a, b, c))
}
`)
		if err != nil {
			t.Fatalf("scan: %v", err)
		}
		if got, want := scan.anchor, 8; got != want {
			t.Errorf("anchor = %d, want %d — with the middleware installed twice the chain is not "+
				"complete until the LAST call, and RegisterFooRoutes on line 7 is therefore early",
				got, want)
		}
	})

	t.Run("a non-operations qualifier is still seen", func(t *testing.T) {
		scan, err := scanGoSource("main.go", preamble+`
	mcp.RegisterAuditRoutes(humaAPI, h)
}
`)
		if err != nil {
			t.Fatalf("scan: %v", err)
		}
		var found bool
		for _, c := range scan.calls {
			if c.group == "RegisterAuditRoutes" && c.pkg == "mcp" {
				found = true
			}
		}
		if !found {
			t.Errorf("mcp.RegisterAuditRoutes was invisible to the scan; got %+v. Requiring the "+
				"literal `operations.` qualifier means a route group in any other package registers "+
				"unwatched — which is how #1560 happened.", scan.calls)
		}
	})

	t.Run("a bare huma.Register on a renamed api is still reported", func(t *testing.T) {
		scan, err := scanGoSource("main.go", preamble+`
	huma.Register(apiV2, op, handler)
}
`)
		if err != nil {
			t.Fatalf("scan: %v", err)
		}
		if len(scan.bareRegisters) != 1 {
			t.Errorf("bareRegisters = %v, want one entry. The old check required the identifier "+
				"`humaAPI`, so renaming the variable hid a directly-registered operation.",
				scan.bareRegisters)
		}
	})

	t.Run("registration inside a helper is classified as outsideMain", func(t *testing.T) {
		// Lexical order cannot see execution order: wireEarly() runs BEFORE the middleware while its
		// body sits after it in the file. The guard refuses the shape instead of pretending to check it.
		scan, err := scanGoSource("main.go", `package main

func main() {
	wireEarly(humaAPI)
	humaAPI.UseMiddleware(humaClerkMiddleware)
	humaAPI.UseMiddleware(humamiddleware.NewReadOnlyMiddleware(x))
	humaAPI.UseMiddleware(humamiddleware.NewCacheInvalidationMiddleware(a, b, c))
}

func wireEarly(api huma.API) {
	operations.RegisterTagRoutes(api, h)
}
`)
		if err != nil {
			t.Fatalf("scan: %v", err)
		}
		if len(scan.outsideMain) != 1 || scan.outsideMain[0].group != "RegisterTagRoutes" {
			t.Errorf("outsideMain = %+v, want RegisterTagRoutes. A registration reachable from a "+
				"helper executes wherever the helper is CALLED, which a line-order comparison "+
				"cannot see; it must be refused rather than silently trusted.", scan.outsideMain)
		}
		if len(scan.calls) != 0 {
			t.Errorf("calls = %+v, want none in main()", scan.calls)
		}
	})

	t.Run("missing middleware fails closed", func(t *testing.T) {
		scan, err := scanGoSource("main.go", `package main

func main() {
	humaAPI.UseMiddleware(humaClerkMiddleware)
}
`)
		if err != nil {
			t.Fatalf("scan: %v", err)
		}
		if got := scan.missingMiddleware(); len(got) != 3 {
			t.Errorf("missingMiddleware() = %v, want the three absent markers. A scan that cannot "+
				"find a middleware must say so; anchoring on whatever it did find silently narrows "+
				"the window it checks.", got)
		}
	})

	t.Run("the real main.go still scans, and richly", func(t *testing.T) {
		// Guards against the whole thing passing vacuously against a fixture-only diet.
		b, err := readMainGo()
		if err != nil {
			t.Fatalf("read main.go: %v", err)
		}
		scan, err := scanGoSource("main.go", b)
		if err != nil {
			t.Fatalf("scan main.go: %v", err)
		}
		if len(scan.calls) < 20 {
			t.Errorf("only %d Register…Routes calls found in the real main.go — the scan is broken, "+
				"and every ordering check built on it would pass vacuously", len(scan.calls))
		}
		if got := scan.missingMiddleware(); len(got) > 0 {
			t.Errorf("could not locate %v in the real main.go", got)
		}
	})
}

func readMainGo() (string, error) {
	b, err := os.ReadFile("main.go")
	return string(b), err
}

// TestRouteRegistrationScanDoesNotFailOpen covers the four fail-open gaps found by re-auditing
// the guard above (and confirmed independently by Codex). Each one let a route register with
// less middleware than it needs while the guard reported success — the exact failure mode of
// #1560, reintroduced through the guard rather than around it.
//
// The gaps, in the order asserted below:
//
//	G-A  Only *ast.SelectorExpr call targets were inspected, so an UNQUALIFIED same-package
//	     call — `RegisterFooRoutes(humaAPI, h)` — had Fun of type *ast.Ident and was recorded
//	     in none of calls/outsideMain/bareRegisters. An explicitly instantiated generic,
//	     `huma.Register[In, Out](...)`, has Fun of type *ast.IndexListExpr and was missed too.
//	G-B  The cross-file check looked at a sibling's calls and outsideMain but never its
//	     bareRegisters, which scanGoSource deliberately collects. With G-A that composed into a
//	     precise exploit: an unqualified call in main.go to a helper in a sibling cmd/server
//	     file whose body calls huma.Register was invisible on BOTH halves of the guard.
//	G-C  The middleware marker list was closed and already one short of main.go, which has a
//	     FOURTH UseMiddleware (humaAdminMiddleware). The anchor is a max over KNOWN markers, so
//	     a route registered between the third and the fourth passed while missing admin RBAC.
//	     The list is now closed in the other direction too: an unrecognised UseMiddleware
//	     argument fails the guard instead of being silently ignored, so the NEXT added
//	     middleware cannot recreate this gap by omission.
//	G-D  A registration on internalHumaAPI was skipped unconditionally, with no check that the
//	     group is unsecured — unlike earlyRouteExemptions, which must prove its claim.
func TestRouteRegistrationScanDoesNotFailOpen(t *testing.T) {
	t.Run("G-A an unqualified same-package registrar call is recorded, not dropped", func(t *testing.T) {
		scan, err := scanGoSource("main.go", `package main

func main() {
	humaAPI.UseMiddleware(humaClerkMiddleware)
	humaAPI.UseMiddleware(humamiddleware.NewReadOnlyMiddleware(x))
	humaAPI.UseMiddleware(humamiddleware.NewCacheInvalidationMiddleware(a, b, c))
	humaAPI.UseMiddleware(humaAdminMiddleware)
	RegisterLocalFooRoutes(humaAPI, h)
}
`)
		if err != nil {
			t.Fatalf("scan: %v", err)
		}
		var found bool
		for _, c := range scan.calls {
			if c.group == "RegisterLocalFooRoutes" && c.pkg == "" {
				found = true
			}
		}
		if !found {
			t.Errorf("calls = %+v; an unqualified RegisterLocalFooRoutes(humaAPI, h) was invisible. "+
				"Only *ast.SelectorExpr was inspected, so a same-package registrar is recorded "+
				"nowhere — not in calls, not in outsideMain, not in bareRegisters.", scan.calls)
		}
	})

	t.Run("G-A an unqualified registrar inside a helper is classified as outsideMain", func(t *testing.T) {
		scan, err := scanGoSource("main.go", `package main

func main() {
	wireEarly(humaAPI)
	humaAPI.UseMiddleware(humaClerkMiddleware)
}

func wireEarly(api huma.API) {
	RegisterLocalTagRoutes(api, h)
}
`)
		if err != nil {
			t.Fatalf("scan: %v", err)
		}
		if len(scan.outsideMain) != 1 || scan.outsideMain[0].group != "RegisterLocalTagRoutes" {
			t.Errorf("outsideMain = %+v, want RegisterLocalTagRoutes. An unqualified registrar in a "+
				"helper runs wherever the helper is CALLED — here before any middleware — and the "+
				"guard could not see it at all.", scan.outsideMain)
		}
	})

	t.Run("G-A an explicitly instantiated generic huma.Register is reported", func(t *testing.T) {
		scan, err := scanGoSource("main.go", `package main

func main() {
	huma.Register[fooInput, fooOutput](humaAPI, op, handler)
}
`)
		if err != nil {
			t.Fatalf("scan: %v", err)
		}
		if len(scan.bareRegisters) != 1 {
			t.Errorf("bareRegisters = %v, want one entry. huma.Register[In, Out](...) has Fun of "+
				"type *ast.IndexListExpr, not *ast.SelectorExpr, so writing the type arguments "+
				"explicitly hid a directly-registered operation from the guard.", scan.bareRegisters)
		}
	})

	t.Run("G-B a sibling file's bare huma.Register is reported", func(t *testing.T) {
		scan, err := scanGoSource("wiring.go", `package main

func wireExtra(api huma.API) {
	huma.Register(api, op, handler)
}
`)
		if err != nil {
			t.Fatalf("scan: %v", err)
		}
		if len(scan.bareRegisters) != 1 {
			t.Fatalf("bareRegisters = %v, want one — fixture is wrong", scan.bareRegisters)
		}
		if got := siblingFileRegistrationProblems("wiring.go", scan); len(got) == 0 {
			t.Errorf("siblingFileRegistrationProblems reported nothing for a sibling file that calls "+
				"huma.Register directly. The cross-file check reads calls and outsideMain but not "+
				"bareRegisters, so an operation registered in a cmd/server helper file is ordered "+
				"by nothing at all; got %v", got)
		}
	})

	t.Run("G-C the anchor includes the fourth (admin) middleware", func(t *testing.T) {
		scan, err := scanGoSource("main.go", `package main

func main() {
	humaAPI.UseMiddleware(humaClerkMiddleware)
	humaAPI.UseMiddleware(humamiddleware.NewReadOnlyMiddleware(x))
	humaAPI.UseMiddleware(humamiddleware.NewCacheInvalidationMiddleware(a, b, c))
	humaAPI.UseMiddleware(humaAdminMiddleware)
}
`)
		if err != nil {
			t.Fatalf("scan: %v", err)
		}
		if got, want := scan.anchor, 7; got != want {
			t.Errorf("anchor = %d (label %q), want %d — the admin middleware is the LAST link in "+
				"main.go's chain, and an anchor computed as a max over a marker list that omits it "+
				"declares the chain complete early. main.go's real admin call is at line 2018 while "+
				"the anchor sat at 1892.", got, scan.anchorLabel, want)
		}
	})

	t.Run("G-C a route registered between the third and fourth middleware fails", func(t *testing.T) {
		scan, err := scanGoSource("main.go", `package main

func main() {
	humaAPI.UseMiddleware(humaClerkMiddleware)
	humaAPI.UseMiddleware(humamiddleware.NewReadOnlyMiddleware(x))
	humaAPI.UseMiddleware(humamiddleware.NewCacheInvalidationMiddleware(a, b, c))
	operations.RegisterAdminRoutes(humaAPI, h)
	humaAPI.UseMiddleware(humaAdminMiddleware)
}
`)
		if err != nil {
			t.Fatalf("scan: %v", err)
		}
		got := earlyRegistrationViolations(scan.calls, scan.anchor, map[string][]string{})
		if len(got) == 0 {
			t.Errorf("no violation for RegisterAdminRoutes on line 7, registered between the "+
				"cache-invalidation middleware and the admin middleware. It runs with no admin "+
				"RBAC — huma snapshots the chain at Register time — yet the guard passed. Got %v", got)
		}
	})

	t.Run("G-C an unrecognised UseMiddleware argument fails rather than being ignored", func(t *testing.T) {
		scan, err := scanGoSource("main.go", `package main

func main() {
	humaAPI.UseMiddleware(humaClerkMiddleware)
	humaAPI.UseMiddleware(humamiddleware.NewReadOnlyMiddleware(x))
	humaAPI.UseMiddleware(humamiddleware.NewCacheInvalidationMiddleware(a, b, c))
	humaAPI.UseMiddleware(humaAdminMiddleware)
	humaAPI.UseMiddleware(humamiddleware.NewSomeFutureMiddleware(x))
}
`)
		if err != nil {
			t.Fatalf("scan: %v", err)
		}
		if len(scan.unrecognisedMiddleware) != 1 || scan.unrecognisedMiddleware[0].line != 8 {
			t.Errorf("unrecognisedMiddleware = %+v, want one entry on line 8. A middleware the "+
				"marker list does not know about must FAIL the guard: an anchor that is a max over "+
				"known markers silently ignores a new last link, which is precisely how the admin "+
				"middleware went unwatched from the day it was added.", scan.unrecognisedMiddleware)
		}
	})

	t.Run("G-D a secured group registered on internalHumaAPI is not waved through", func(t *testing.T) {
		calls := []routeRegistration{{group: "RegisterSecretRoutes", pkg: "operations", api: "internalHumaAPI", line: 400}}
		secured := map[string][]string{"RegisterSecretRoutes": {"getSecret"}}
		if got := earlyRegistrationViolations(calls, 1892, secured); len(got) == 0 {
			t.Errorf("internalHumaAPI was waved through with a secured operation. That API installs "+
				"NO middleware and its listener binds 0.0.0.0 (internal_metrics.go), so a secured "+
				"operation on it never gets Clerk at any line — earlyRouteExemptions has to PROVE "+
				"its groups are unsecured, and this escape hatch proves nothing. Got %v", got)
		}
	})
}
