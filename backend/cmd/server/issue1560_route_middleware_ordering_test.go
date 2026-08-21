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
// The anchor is the LAST middleware installed on humaAPI, computed from the source rather than
// assumed. Keying on any single one (Codex review, Low finding) would let a reorder leave a route
// past the anchor but still ahead of a middleware it needs.
//
// A previous version of this comment claimed that taking the LAST also handles "a fourth middleware
// added after the cache one". IT DOES NOT, and saying so is how the gap survived review: the max is
// taken over requestScopedMiddlewareMarkers, a HAND-MAINTAINED list, so a fourth middleware only
// moves the anchor once somebody adds it there. The fourth middleware in main.go —
// humaAdminMiddleware, line 2018 — was instead hand-EXCLUDED, on the reasoning that it self-scopes
// to /api/v1/admin (admin.go:47-50). The reasoning is true of the middleware and irrelevant to the
// anchor: with the anchor at the cache-invalidation call (1892), lines 1893-2017 were a window in
// which any route — including an admin one — could register and pass. It is included now, and
// scanGoSource fails on any UseMiddleware argument NOT in the marker list, so the list can no longer
// silently fall behind main.go.
//
// Three things the anchor is NOT allowed to be, each of which produced a green guard over an api with
// middleware missing (T49/T50, demonstrated against the real main.go text before being fixed):
// a call on another RECEIVER (`internalHumaAPI.UseMiddleware(cache…)` — one token, and every
// post-anchor mutating route loses cache invalidation), a call OUTSIDE func main() (the four hoisted
// into an uncalled helper collapse the anchor to the top of the file, so everything reads as late
// while nothing is installed), and a marker match on Args[0] only (UseMiddleware is variadic, so a
// new middleware entering as the second argument was invisible). All three now FAIL here rather than
// moving or shrinking the anchor. What is still beyond the scan: it does not resolve calls or
// aliases, so a middleware installed through a value it cannot name is neither seen nor claimed —
// see the limits list in route_registration_scan_test.go.

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
	// The middleware markers and the route-call scan now live in route_registration_scan_test.go,
	// on go/ast. Only the mutating-method matcher is still a regex, and it matches a method name
	// already extracted from an AST node.
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

func TestMutatingRoutesRegisteredAfterMiddlewareChain(t *testing.T) {
	raw, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatalf("read main.go: %v", err)
	}

	// AST, not text. A commented-out registration is not a call, and a comment naming a middleware
	// cannot move the anchor. Both holes were verified against this guard before the rewrite; see
	// route_registration_scan_test.go for the evidence and the remaining lexical-order limit.
	scan, err := scanGoSource("main.go", string(raw))
	if err != nil {
		t.Fatalf("scan main.go: %v", err)
	}
	// The RECEIVER first, because a call on the wrong one is what made these two checks
	// meaningless: the scan used to match the method name on ANY receiver, so
	// `internalHumaAPI.UseMiddleware(cache…)` counted toward humaAPI's chain and the guard stayed
	// green with every post-anchor mutating route running uninvalidated (T49). Report it before
	// what is missing, so the failure names the cause and not only the symptom.
	for _, m := range scan.middlewareOtherReceiver {
		recv := m.receiver
		if recv == "" {
			recv = "<not a plain identifier>"
		}
		t.Errorf("main.go:%d installs middleware on %s, not on %s: %s.UseMiddleware(%s). huma "+
			"snapshots the chain per api, so this installs NOTHING on the api the guarded routes "+
			"register on — if it is "+
			"deliberate, this guard has to be taught about that api's stack; if it is a typo, it is "+
			"the whole of #1560 in one token.", m.line, recv, guardedAPIIdent, recv, m.arg)
	}
	// A middleware install outside func main() must FAIL, not quietly produce a lower anchor. The
	// four calls hoisted into an uncalled `func installHumaMiddleware(...)` above main() collapse the
	// anchor to the top of the file, which reads as "every registration is after the chain" while no
	// chain is installed at all (T49).
	for _, m := range scan.middlewareOutsideMain {
		t.Errorf("main.go:%d installs %s middleware outside func main(): %s. This guard compares "+
			"SOURCE line order within main(), so middleware installed in a helper is ordered against "+
			"nothing — and a helper that is never CALLED installs nothing while making every "+
			"registration look late. Install it directly in main().", m.line, guardedAPIIdent, m.arg)
	}
	if missing := scan.missingMiddleware(); len(missing) > 0 {
		t.Fatalf("could not locate UseMiddleware call(s) for %v in main.go — this guard is looking "+
			"for the wrong thing, or that middleware was removed; a silently unanchored ordering "+
			"check is worse than no check", missing)
	}
	if len(scan.unrecognisedMiddleware) > 0 {
		var at []string
		for _, m := range scan.unrecognisedMiddleware {
			at = append(at, "main.go:"+strconv.Itoa(m.line)+" "+m.arg)
		}
		sort.Strings(at)
		t.Errorf("%d UseMiddleware argument(s) on %s in main.go that requestScopedMiddlewareMarkers "+
			"does not describe:\n  - %s\nThe anchor is a max over KNOWN markers, so an unlisted "+
			"middleware is a link this guard cannot see and every route registered ahead of it "+
			"passes while running without it. Add a marker for it in route_registration_scan_test.go "+
			"(that is what the omitted admin middleware cost: a 125-line window with no RBAC).",
			len(scan.unrecognisedMiddleware), guardedAPIIdent, strings.Join(at, "\n  - "))
	}
	anchor := scan.anchor

	if len(scan.bareRegisters) > 0 {
		t.Errorf("%d bare huma.Register(...) call(s) in main.go (line(s) %v). This ordering guard "+
			"tracks Register*Routes groups, so a directly-registered operation is invisible to it — "+
			"register it through a Register*Routes function, or teach this guard to resolve it.",
			len(scan.bareRegisters), scan.bareRegisters)
	}

	// Lexical order cannot see execution order, so the shape is refused rather than trusted: a
	// registration inside a helper runs wherever the helper is CALLED, which may be before the
	// middleware even though the helper body sits after it in the file.
	for _, c := range scan.outsideMain {
		t.Errorf("%s is called at main.go:%d, outside func main(). This guard compares SOURCE line "+
			"order, so a registration reached through a helper could execute before the middleware "+
			"chain while appearing after it — register it directly in main().", c.group, c.line)
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
		other, serr := scanGoSource(n, string(b))
		if serr != nil {
			t.Fatalf("scan %s: %v", n, serr)
		}
		for _, problem := range siblingFileRegistrationProblems(n, other) {
			t.Error(problem)
		}
	}

	calls := scan.calls
	if len(calls) == 0 {
		t.Fatal("scanned zero Register*Routes(...) calls in main.go — the guard is " +
			"broken, not the wiring")
	}

	byGroup := operationsByGroup(t)
	secured := securedRouteGroupOperations(t, byGroup)
	// (1) Nothing secured may be registered before the anchor.
	violations := earlyRegistrationViolations(calls, anchor, secured)

	// (2) Required groups must actually be registered, exactly once, after the anchor.
	for group, wantPaths := range requiredLateRouteGroups {
		var seen []routeRegistration
		for _, c := range calls {
			if c.group == group && c.api == guardedAPIIdent {
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

// earlyRegistrationViolations reports every Register*Routes call that sits ahead of the
// middleware anchor without a truthful exemption, plus every call on an api value the guard
// cannot reason about. Extracted from the test body so the fail-open cases can be driven from
// fixtures rather than only from whatever main.go happens to contain today.
func earlyRegistrationViolations(calls []routeRegistration, anchor int, secured map[string][]string) []string {
	var violations []string
	for _, c := range calls {
		if c.api != guardedAPIIdent {
			// internalHumaAPI is the separate internal metrics listener with its own stack;
			// anything else is unrecognised and reported rather than assumed harmless.
			if c.api != "internalHumaAPI" {
				violations = append(violations, c.group+" (main.go:"+strconv.Itoa(c.line)+
					") is registered on an unrecognised api value "+strconv.Quote(c.api)+
					" — this guard cannot reason about its middleware stack")
				continue
			}
			// internalHumaAPI carries NO middleware at all and its listener binds 0.0.0.0
			// (internal_metrics.go), so a secured operation registered on it never gets Clerk at any
			// line — strictly worse than an early registration on humaAPI. Skipping it
			// unconditionally, as this guard used to, was an exemption that proved nothing, while
			// earlyRouteExemptions right below is required to prove its claim.
			if ops := secured[c.group]; len(ops) > 0 {
				sort.Strings(ops)
				violations = append(violations, c.group+" (main.go:"+strconv.Itoa(c.line)+
					") is registered on internalHumaAPI, which installs no middleware, but registers "+
					"operation(s) declaring Security: "+strings.Join(ops, ", ")+" — those run with "+
					"NO authentication on a listener bound to 0.0.0.0")
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
			violations = append(violations, "earlyRouteExemptions["+strconv.Quote(c.group)+
				"] has an empty reason — a bare exemption defeats the guard")
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
	return violations
}

// siblingFileRegistrationProblems reports route registrations found in a cmd/server file
// OTHER than main.go. This guard orders registrations against main.go's middleware lines, so
// any registration in a sibling file is outside what it can reason about and is refused.
func siblingFileRegistrationProblems(name string, s sourceScan) []string {
	var out []string
	if len(s.calls)+len(s.outsideMain) > 0 {
		out = append(out, name+" registers routes with Register*Routes, but this guard only orders "+
			"main.go — its line comparison is meaningless across files. Move the registration into "+
			"main.go or extend the guard to span cmd/server.")
	}
	// bareRegisters was collected for sibling files and then never read, which is the more dangerous
	// half: main.go's own bare huma.Register calls were reported, but the same call moved into a
	// cmd/server helper file was reported by nothing. Combined with an unqualified call site in
	// main.go, that hid a registration from BOTH halves of the guard.
	if len(s.bareRegisters) > 0 {
		var at []string
		for _, l := range s.bareRegisters {
			at = append(at, strconv.Itoa(l))
		}
		out = append(out, name+" calls huma.Register(...) directly at line(s) "+
			strings.Join(at, ", ")+
			". Operations registered outside main.go are ordered against nothing — this guard "+
			"compares main.go's line numbers only. Register through a Register*Routes function "+
			"called from main.go, after the middleware chain.")
	}
	return out
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
