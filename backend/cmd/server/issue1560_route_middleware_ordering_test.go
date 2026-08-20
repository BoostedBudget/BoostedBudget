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
// #1560 was exactly that: `operations.RegisterDeletionRoutes` ran at main.go:975 while
// every UseMiddleware call happens at 1869+, so `requestDeletion` — POST, and declaring
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
// Deliberately keyed on the cache-invalidation middleware rather than the Clerk one: it is
// registered LAST of the three request-scoped middlewares (1883 > 1874 > 1869), so
// clearing it clears all of them.

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
	reCacheMW           = regexp.MustCompile(`UseMiddleware\(\s*humamiddleware\.NewCacheInvalidationMiddleware`)
	reRouteCall         = regexp.MustCompile(`operations\.(Register[A-Za-z]+Routes)\(\s*humaAPI\b`)
	reMutatingMethod    = regexp.MustCompile(`^(POST|PUT|PATCH|DELETE)$`)
)

func TestMutatingRoutesRegisteredAfterCacheInvalidationMiddleware(t *testing.T) {
	data, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatalf("read main.go: %v", err)
	}
	lines := strings.Split(string(data), "\n")

	mwLine := 0
	for i, l := range lines {
		if reCacheMW.MatchString(l) {
			mwLine = i + 1
			break
		}
	}
	if mwLine == 0 {
		t.Fatal("could not locate the NewCacheInvalidationMiddleware UseMiddleware call in main.go — " +
			"this guard is looking for the wrong thing, or the middleware was removed; a silently " +
			"unanchored ordering check is worse than no check")
	}

	type call struct {
		group string
		line  int
	}
	var early []call
	total := 0
	for i, l := range lines {
		m := reRouteCall.FindStringSubmatch(l)
		if m == nil {
			continue
		}
		total++
		if i+1 < mwLine {
			early = append(early, call{group: m[1], line: i + 1})
		}
	}
	if total == 0 {
		t.Fatal("scanned zero operations.Register*Routes(humaAPI, ...) calls in main.go — the guard " +
			"is broken, not the wiring")
	}

	secured := securedMutatingRouteGroups(t)

	var violations []string
	for _, c := range early {
		reason, exempt := earlyRouteExemptions[c.group]
		if !exempt {
			violations = append(violations, c.group+" (main.go:"+strconv.Itoa(c.line)+
				") is registered before the cache-invalidation middleware (main.go:"+strconv.Itoa(mwLine)+
				") and is not in earlyRouteExemptions")
			continue
		}
		if strings.TrimSpace(reason) == "" {
			t.Errorf("earlyRouteExemptions[%q] has an empty reason — a bare exemption defeats the guard", c.group)
		}
		// The exemption must be TRUE, not merely asserted.
		if ops := secured[c.group]; len(ops) > 0 {
			sort.Strings(ops)
			violations = append(violations, c.group+" (main.go:"+strconv.Itoa(c.line)+
				") is exempted as pre-auth, but registers mutating operation(s) that declare Security: "+
				strings.Join(ops, ", ")+" — those run with NO middleware")
		}
	}

	if len(violations) > 0 {
		sort.Strings(violations)
		t.Errorf("%d route group(s) registered before the middleware chain is assembled; huma "+
			"snapshots the chain at Register time, so their middlewares never run:\n  - %s",
			len(violations), strings.Join(violations, "\n  - "))
	}
}

// securedMutatingRouteGroups maps each Register*Routes function in internal/huma/operations
// to the OperationIDs it registers that are both mutating and declare a non-empty Security.
func securedMutatingRouteGroups(t *testing.T) map[string][]string {
	t.Helper()
	dir := filepath.Join("..", "..", "internal", "huma", "operations")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read operations dir: %v", err)
	}

	out := map[string][]string{}
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
				opID, method := "", ""
				securityNonEmpty := false
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
						opID = stringLit(kv.Value)
					case "Method":
						method = httpMethodName(kv.Value)
					case "Security":
						if cl, ok := kv.Value.(*ast.CompositeLit); ok {
							securityNonEmpty = len(cl.Elts) > 0
						}
					}
				}
				if securityNonEmpty && reMutatingMethod.MatchString(method) {
					if opID == "" {
						opID = "<unresolved OperationID>"
					}
					out[fn.Name.Name] = append(out[fn.Name.Name], opID)
				}
				return true
			})
		}
	}
	if len(out) == 0 {
		t.Fatal("found zero secured mutating operations across the operations package — the scan is " +
			"broken; every exemption below would then pass vacuously")
	}
	return out
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

// TestHumaSnapshotsMiddlewareChainAtRegistration pins the huma behaviour that makes the
// ordering guard above necessary at all. Without this, a reader has to take the claim on
// trust, and if a future huma upgrade resolved middlewares lazily at request time the
// guard would silently become cargo cult.
//
// Uses the same constructor main.go does (humaecho.NewV4 over echo/v4) so the result is
// about this deployment's adapter, not huma in the abstract.
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
