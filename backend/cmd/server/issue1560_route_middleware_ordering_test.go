package main

import (
	"context"
	"go/ast"
	"go/parser"
	"go/token"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humaecho"
	"github.com/labstack/echo/v4"
)

// Issue #1560. `huma.Register` SNAPSHOTS the middleware chain at registration time —
// huma.go:881 calls `a.Handle(&op, api.Middlewares().Handler(...))`, and chain.go:22-32
// builds the wrapper closures eagerly, returning the bare endpoint when the chain is
// empty. A `humaAPI.UseMiddleware(...)` call therefore has NO effect on any operation
// already registered.
//
// #1560 was exactly that: `operations.RegisterDeletionRoutes` ran early in main() while
// every UseMiddleware call happens much later, so `requestDeletion` — POST, and declaring
// `Security: {{"clerk"}}` — ran with zero middlewares. No Clerk auth (so its
// AuthenticatedWithEmailInput resolver returned 401 unconditionally, leaving the GDPR
// deletion endpoint dead), no read-only enforcement despite read_only.go:87 listing that
// very path, and no cache-generation bump.
//
// The three guards in internal/{repository,queue/jobs,huma/operations} cannot see this.
// The operations guard says so itself: a non-empty `Security` field proves auth METADATA
// exists, "not that the middleware is actually registered ahead of it in main.go". This
// test is that missing check, and it is condition C4 of issue #1537's disposition model:
// `CoveredByHTTPMiddleware` is only sound for operations registered AFTER the
// cache-invalidation middleware.
//
// The anchor is the LAST of the three request-scoped middlewares to be registered, computed
// from the source rather than assumed: keying on any single one (Codex review, Low finding)
// would let a future reorder — or a fourth middleware added after the cache one — leave a
// route past the anchor but still ahead of a middleware it needs. Deliberately excludes the
// admin middleware, which gates on the /api/v1/admin path prefix (admin.go) and so is
// irrelevant to non-admin route groups.

// earlyRouteExemptions names each Register*Routes group that may legitimately be
// registered on humaAPI before the middleware chain is assembled, with the reason.
// A group is only allowed here if it registers no operation that is BOTH mutating and
// secured — which this test verifies against the operations package rather than trusting.
var earlyRouteExemptions = map[string]string{
	"RegisterHealthRoutes": "Liveness/readiness only, GET, no Security by design — Fly's http_checks " +
		"reach it directly at *.fly.dev bypassing Cloudflare, and it must not depend on Clerk.",
	"RegisterSubscriptionTiersRoutes": "Public read-only tier catalogue (GET), served before auth so " +
		"the pricing page works for anonymous visitors.",
	"RegisterRegionRoutes": "Public read-only region list (GET) for signup region selection, " +
		"pre-authentication by design.",
}

var (
	// requestScopedMiddlewares are the middlewares whose protection a secured route depends on.
	// Every one must be registered before any such route, so the guard anchors on the last.
	requestScopedMiddlewares = []*regexp.Regexp{
		regexp.MustCompile(`UseMiddleware\(\s*humaClerkMiddleware`),
		regexp.MustCompile(`UseMiddleware\(\s*humamiddleware\.NewReadOnlyMiddleware`),
		regexp.MustCompile(`UseMiddleware\(\s*humamiddleware\.NewCacheInvalidationMiddleware`),
	}
	reRouteCallOpen  = regexp.MustCompile(`operations\.(Register[A-Za-z]+Routes)\(`)
	reMutatingMethod = regexp.MustCompile(`^(POST|PUT|PATCH|DELETE)$`)
)

// requiredLateRouteGroups must be registered on humaAPI, exactly once, AFTER the anchor.
// Without this the ordering check is vacuous for the very endpoint #1560 was about: deleting
// the RegisterDeletionRoutes call entirely would satisfy "nothing is registered too early"
// while /api/v1/user/delete returned 404 (Codex review, Medium finding 1). Each entry also
// names the paths that group must expose, checked against the operations package.
var requiredLateRouteGroups = map[string][]string{
	"RegisterDeletionRoutes": {"/api/v1/user/delete", "/api/v1/user/delete/cancel/{token}"},
}

// routeCall is one operations.Register*Routes(...) call found in main.go.
type routeCall struct {
	group string
	api   string // the first argument: humaAPI, internalHumaAPI, or something unresolved
	line  int
}

// findRouteCalls scans src for operations.Register*Routes(...) calls, reading forward to the
// matching close paren so a call split across lines is still seen (Codex review, Medium
// finding 2 — the original single-line regex missed those). The first argument is captured
// rather than matched, so a call on a DIFFERENT api value surfaces as a distinct api string
// instead of silently vanishing from the scan.
func findRouteCalls(src string) []routeCall {
	var out []routeCall
	for _, m := range reRouteCallOpen.FindAllStringSubmatchIndex(src, -1) {
		group := src[m[2]:m[3]]
		depth, i := 1, m[1]
		for ; i < len(src) && depth > 0; i++ {
			switch src[i] {
			case '(':
				depth++
			case ')':
				depth--
			}
		}
		firstArg := strings.TrimSpace(src[m[1] : i-1])
		if c := strings.IndexByte(firstArg, ','); c >= 0 {
			firstArg = strings.TrimSpace(firstArg[:c])
		}
		out = append(out, routeCall{
			group: group,
			api:   firstArg,
			line:  strings.Count(src[:m[0]], "\n") + 1,
		})
	}
	return out
}

func TestMutatingRoutesRegisteredAfterMiddlewareChain(t *testing.T) {
	raw, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatalf("read main.go: %v", err)
	}
	src := string(raw)
	lines := strings.Split(src, "\n")

	// Anchor = the LAST request-scoped middleware registration. Every one must be located; a
	// middleware this guard cannot find is a guard looking at the wrong thing.
	anchor := 0
	for _, re := range requestScopedMiddlewares {
		found := 0
		for i, l := range lines {
			if re.MatchString(l) {
				found = i + 1
				break
			}
		}
		if found == 0 {
			t.Fatalf("could not locate a UseMiddleware call matching %s in main.go — this guard is "+
				"looking for the wrong thing, or that middleware was removed; a silently unanchored "+
				"ordering check is worse than no check", re)
		}
		if found > anchor {
			anchor = found
		}
	}

	// G11 (gap analysis): findRouteCalls only recognises operations.Register*Routes(...). Two shapes
	// escape it, and both are invisible today only by convention rather than by construction:
	//   (a) a bare huma.Register(humaAPI, ...) written directly in cmd/server, and
	//   (b) a Register*Routes call living in some other cmd/server file instead of main.go.
	// Either would register a route this ordering check never sees, which is exactly how #1560
	// happened — a route nobody was looking at. Fail on both rather than rely on style holding.
	if bare := regexp.MustCompile(`huma\.Register\(\s*humaAPI\b`).FindAllStringIndex(src, -1); len(bare) > 0 {
		t.Errorf("%d bare huma.Register(humaAPI, ...) call(s) in main.go. This ordering guard only "+
			"tracks operations.Register*Routes groups, so a directly-registered operation is invisible "+
			"to it — register it through a Register*Routes function, or teach this guard to resolve it.",
			len(bare))
	}
	otherFiles, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("read cmd/server dir: %v", err)
	}
	for _, e := range otherFiles {
		n := e.Name()
		if e.IsDir() || n == "main.go" || !strings.HasSuffix(n, ".go") || strings.HasSuffix(n, "_test.go") {
			continue
		}
		b, rerr := os.ReadFile(n)
		if rerr != nil {
			t.Fatalf("read %s: %v", n, rerr)
		}
		if reRouteCallOpen.MatchString(string(b)) {
			t.Errorf("%s registers routes with operations.Register*Routes, but this guard only reads "+
				"main.go — its line-ordering comparison is meaningless across files. Move the "+
				"registration into main.go or extend the guard to span cmd/server.", n)
		}
	}

	calls := findRouteCalls(src)
	if len(calls) == 0 {
		t.Fatal("scanned zero operations.Register*Routes(...) calls in main.go — the guard is " +
			"broken, not the wiring")
	}

	byGroup := operationsByGroup(t)
	secured := securedRouteGroupOperations(t, byGroup)
	var violations []string

	// (1) Nothing secured may be registered before the anchor.
	for _, c := range calls {
		if c.api != "humaAPI" {
			// internalHumaAPI is the separate internal metrics listener with its own stack;
			// anything else is unrecognised and reported rather than assumed harmless.
			if c.api != "internalHumaAPI" {
				violations = append(violations, c.group+" (main.go:"+strconv.Itoa(c.line)+
					") is registered on an unrecognised api value "+strconv.Quote(c.api)+
					" — this guard cannot reason about its middleware stack")
			}
			continue
		}
		if c.line >= anchor {
			continue
		}
		reason, exempt := earlyRouteExemptions[c.group]
		if !exempt {
			violations = append(violations, c.group+" (main.go:"+strconv.Itoa(c.line)+
				") is registered before the middleware chain is complete (main.go:"+
				strconv.Itoa(anchor)+") and is not in earlyRouteExemptions")
			continue
		}
		if strings.TrimSpace(reason) == "" {
			t.Errorf("earlyRouteExemptions[%q] has an empty reason — a bare exemption defeats the guard", c.group)
		}
		// The exemption must be TRUE, not merely asserted. ANY secured operation disqualifies a
		// group, not merely a mutating one: Clerk skips only operations with empty Security
		// (clerk.go), so an early secured GET loses authentication just as thoroughly as an
		// early secured POST loses cache invalidation (Codex review, Medium finding 2).
		if ops := secured[c.group]; len(ops) > 0 {
			sort.Strings(ops)
			violations = append(violations, c.group+" (main.go:"+strconv.Itoa(c.line)+
				") is exempted as pre-auth, but registers operation(s) declaring Security: "+
				strings.Join(ops, ", ")+" — those run with NO middleware")
		}
	}

	// (2) Required groups must actually be registered, exactly once, after the anchor.
	for group, wantPaths := range requiredLateRouteGroups {
		var seen []routeCall
		for _, c := range calls {
			if c.group == group && c.api == "humaAPI" {
				seen = append(seen, c)
			}
		}
		switch {
		case len(seen) == 0:
			violations = append(violations, group+" is not registered on humaAPI in main.go at all — "+
				"the routes it owns would 404; #1560 was about exactly this endpoint being unreachable")
		case len(seen) > 1:
			var at []string
			for _, c := range seen {
				at = append(at, strconv.Itoa(c.line))
			}
			violations = append(violations, group+" is registered "+strconv.Itoa(len(seen))+
				" times on humaAPI (main.go:"+strings.Join(at, ", ")+") — duplicate registration")
		case seen[0].line < anchor:
			violations = append(violations, group+" (main.go:"+strconv.Itoa(seen[0].line)+
				") must be registered AFTER the middleware chain is complete (main.go:"+
				strconv.Itoa(anchor)+")")
		}
		for _, want := range wantPaths {
			if !pathRegisteredByGroup(byGroup, group, want) {
				violations = append(violations, group+" no longer registers the path "+want+
					" — a caller of that endpoint would 404")
			}
		}
	}

	if len(violations) > 0 {
		sort.Strings(violations)
		t.Errorf("%d route-registration problem(s); huma snapshots the middleware chain at Register "+
			"time, so a route registered too early runs with no middleware at all:\n  - %s",
			len(violations), strings.Join(violations, "\n  - "))
	}
}

// registeredOp is one huma.Operation literal found in the operations package.
type registeredOp struct {
	id, method, path string
	secured          bool
}

// operationsByGroup maps each Register*Routes function in internal/huma/operations to every
// huma.Operation literal it registers.
func operationsByGroup(t *testing.T) map[string][]registeredOp {
	t.Helper()
	dir := filepath.Join("..", "..", "internal", "huma", "operations")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read operations dir: %v", err)
	}

	out := map[string][]registeredOp{}
	fset := token.NewFileSet()
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") || strings.HasSuffix(e.Name(), "_test.go") {
			continue
		}
		f, err := parser.ParseFile(fset, filepath.Join(dir, e.Name()), nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", e.Name(), err)
		}
		for _, decl := range f.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Recv != nil || !strings.HasPrefix(fn.Name.Name, "Register") ||
				!strings.HasSuffix(fn.Name.Name, "Routes") || fn.Body == nil {
				continue
			}
			ast.Inspect(fn.Body, func(n ast.Node) bool {
				lit, ok := n.(*ast.CompositeLit)
				if !ok {
					return true
				}
				sel, ok := lit.Type.(*ast.SelectorExpr)
				if !ok || sel.Sel.Name != "Operation" {
					return true
				}
				op := registeredOp{}
				for _, el := range lit.Elts {
					kv, ok := el.(*ast.KeyValueExpr)
					if !ok {
						continue
					}
					key, ok := kv.Key.(*ast.Ident)
					if !ok {
						continue
					}
					switch key.Name {
					case "OperationID":
						op.id = stringLit(kv.Value)
					case "Method":
						op.method = httpMethodName(kv.Value)
					case "Path":
						op.path = stringLit(kv.Value)
					case "Security":
						if cl, ok := kv.Value.(*ast.CompositeLit); ok {
							op.secured = len(cl.Elts) > 0
						}
					}
				}
				out[fn.Name.Name] = append(out[fn.Name.Name], op)
				return true
			})
		}
	}
	if len(out) == 0 {
		t.Fatal("found zero huma.Operation literals across the operations package — the scan is " +
			"broken; every exemption and required-path check would then pass vacuously")
	}
	return out
}

// securedRouteGroupOperations maps each Register*Routes function to the OperationIDs it
// registers that declare a non-empty Security, regardless of HTTP method.
func securedRouteGroupOperations(t *testing.T, byGroup map[string][]registeredOp) map[string][]string {
	t.Helper()
	out := map[string][]string{}
	anySecured := false
	for group, ops := range byGroup {
		for _, op := range ops {
			if !op.secured {
				continue
			}
			anySecured = true
			id := op.id
			if id == "" {
				id = "<unresolved OperationID>"
			}
			if reMutatingMethod.MatchString(op.method) {
				id += " (" + op.method + ")"
			}
			out[group] = append(out[group], id)
		}
	}
	if !anySecured {
		t.Fatal("found zero secured operations across the operations package — the Security scan is " +
			"broken; every exemption cross-check would then pass vacuously")
	}
	return out
}

func pathRegisteredByGroup(byGroup map[string][]registeredOp, group, path string) bool {
	for _, op := range byGroup[group] {
		if op.path == path {
			return true
		}
	}
	return false
}

func stringLit(e ast.Expr) string {
	bl, ok := e.(*ast.BasicLit)
	if !ok || bl.Kind != token.STRING {
		return ""
	}
	return strings.Trim(bl.Value, `"`)
}

// httpMethodName resolves `http.MethodPost` -> "POST" and a bare "POST" literal alike.
func httpMethodName(e ast.Expr) string {
	if s := stringLit(e); s != "" {
		return strings.ToUpper(s)
	}
	sel, ok := e.(*ast.SelectorExpr)
	if !ok {
		return ""
	}
	return strings.ToUpper(strings.TrimPrefix(sel.Sel.Name, "Method"))
}

func TestHumaSnapshotsMiddlewareChainAtRegistration(t *testing.T) {
	for _, tc := range []struct {
		name            string
		registerFirst   bool
		wantMiddlewareR bool
	}{
		{"registered BEFORE UseMiddleware — middleware must NOT run", true, false},
		{"registered AFTER UseMiddleware — middleware must run", false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := echo.New()
			api := humaecho.NewV4(e, huma.DefaultConfig("ordering-test", "1.0.0"))

			var ran bool
			mw := func(ctx huma.Context, next func(huma.Context)) {
				ran = true
				next(ctx)
			}
			op := huma.Operation{
				OperationID: "orderingProbe",
				Method:      http.MethodPost,
				Path:        "/ordering-probe",
				Security:    []map[string][]string{{"clerk": {}}},
			}

			if tc.registerFirst {
				huma.Register(api, op, orderingProbeHandler)
				api.UseMiddleware(mw)
			} else {
				api.UseMiddleware(mw)
				huma.Register(api, op, orderingProbeHandler)
			}

			rec := httptest.NewRecorder()
			e.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/ordering-probe", nil))

			if ran != tc.wantMiddlewareR {
				t.Errorf("middleware ran = %v, want %v (status %d). If the BEFORE case now runs the "+
					"middleware, huma resolves the chain lazily and the ordering guard above is "+
					"obsolete; if the AFTER case does not, middleware wiring is broken outright",
					ran, tc.wantMiddlewareR, rec.Code)
			}
		})
	}
}

type orderingProbeInput struct{}

type orderingProbeOutput struct {
	Body struct {
		OK bool `json:"ok"`
	}
}

func orderingProbeHandler(context.Context, *orderingProbeInput) (*orderingProbeOutput, error) {
	out := &orderingProbeOutput{}
	out.Body.OK = true
	return out, nil
}
