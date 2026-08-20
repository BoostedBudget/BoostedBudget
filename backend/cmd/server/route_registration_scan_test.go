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
// order. `outsideMain` is what keeps that honest — a Register…Routes call inside a helper could run
// before the middleware while appearing after it in the file, so the guard refuses the shape outright
// and requires registration directly in func main(). All 38 of main.go's registrations already satisfy
// that, so this constrains nothing that exists today. Verifying real execution order would mean
// extracting a buildAPI() seam from a 2000-line main() — a refactor for its own PR, not a test change.

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
}

// requestScopedMiddlewareMarkers identifies the middlewares whose protection a secured route depends
// on. Matched against the SOURCE TEXT OF THE FIRST ARGUMENT of a real UseMiddleware call node, so a
// mention in a comment cannot contribute.
var requestScopedMiddlewareMarkers = []struct{ label, marker string }{
	{"clerk", "humaClerkMiddleware"},
	{"read-only", "NewReadOnlyMiddleware"},
	{"cache-invalidation", "NewCacheInvalidationMiddleware"},
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
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		line := fset.Position(call.Pos()).Line
		name := sel.Sel.Name

		switch {
		case name == "UseMiddleware":
			if len(call.Args) == 0 {
				return true
			}
			arg := text(call.Args[0])
			for _, m := range requestScopedMiddlewareMarkers {
				if strings.Contains(arg, m.marker) && line > out.middleware[m.label] {
					out.middleware[m.label] = line
				}
			}

		case name == "Register":
			// Any huma.Register, on any api identifier.
			if x, ok := sel.X.(*ast.Ident); ok && x.Name == "huma" {
				out.bareRegisters = append(out.bareRegisters, line)
			}

		case strings.HasPrefix(name, "Register") && strings.HasSuffix(name, "Routes"):
			reg := routeRegistration{group: name, line: line}
			if x, ok := sel.X.(*ast.Ident); ok {
				reg.pkg = x.Name
			}
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
		if got := scan.missingMiddleware(); len(got) != 2 {
			t.Errorf("missingMiddleware() = %v, want the two absent markers. A scan that cannot "+
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
