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

// secondaryAPIMiddlewareExemptions names each api identifier OTHER than guardedAPIIdent whose
// UseMiddleware calls in main.go are legitimate, with the reason — the same proof-carrying shape as
// earlyRouteExemptions.
//
// It exists because the two axes of this guard used to disagree about the SAME identifier:
// earlyRegistrationViolations recognises internalHumaAPI as the internal metrics listener and
// reasons about it, while every internalHumaAPI.UseMiddleware call was an unconditional failure
// whose leading theory was "if it is a typo, it is the whole of #1560 in one token". Adding a
// middleware to the internal listener is a perfectly ordinary thing to do, and a guard that fails
// with a #1560 accusation for doing it is a guard that gets deleted. Tolerated on one axis and fatal
// on the other was unintentional, so the exemption is now explicit and testable.
//
// Exempting a receiver is NOT a blanket pass: a call whose argument matches one of
// requestScopedMiddlewareMarkers still FAILS on any receiver, because those middlewares belong to
// guardedAPIIdent's chain and moving one to another api is precisely the T49 one-token defect
// (H1a in route_registration_scan_test.go).
var secondaryAPIMiddlewareExemptions = map[string]string{
	"internalHumaAPI": "The internal metrics listener (newInternalMetricsAPI, internal_metrics.go), " +
		"a separate huma.API with its own stack that serves only RegisterMetricsRoutes. huma " +
		"snapshots per api, so middleware installed here cannot affect humaAPI's chain — and a " +
		"middleware humaAPI DOES depend on is still refused below, whatever the receiver.",
}

// otherReceiverMiddlewareViolations reports every UseMiddleware call that is not on
// guardedAPIIdent and is not covered by a truthful secondaryAPIMiddlewareExemptions entry.
// Extracted from the test body so both the tolerated and the fatal case can be driven from
// fixtures rather than from whatever main.go happens to contain today.
func otherReceiverMiddlewareViolations(calls []middlewareCall) []string {
	var violations []string
	for _, m := range calls {
		recv := m.receiver
		if recv == "" {
			recv = "<not a plain identifier>"
		}
		// A middleware guardedAPIIdent depends on is refused on ANY receiver, exempt or not:
		// that is #1560 in one token, and missingMiddleware() catching it too is not a reason to
		// let the more precise message go unsaid.
		for _, marker := range requestScopedMiddlewareMarkers {
			if strings.Contains(m.arg, marker.marker) {
				violations = append(violations, "main.go:"+strconv.Itoa(m.line)+" installs "+
					guardedAPIIdent+"'s "+marker.label+" middleware ("+marker.marker+
					") on "+recv+" instead: "+recv+".UseMiddleware("+m.arg+"). huma snapshots the "+
					"chain per api, so this installs NOTHING on the api the guarded routes register "+
					"on — every route past the anchor then runs without it. No "+
					"secondaryAPIMiddlewareExemptions entry can wave this through.")
			}
		}
		reason, exempt := secondaryAPIMiddlewareExemptions[recv]
		if exempt && strings.TrimSpace(reason) == "" {
			violations = append(violations, "secondaryAPIMiddlewareExemptions["+strconv.Quote(recv)+
				"] has an empty reason — a bare exemption defeats the guard")
			continue
		}
		if exempt {
			continue
		}
		violations = append(violations, "main.go:"+strconv.Itoa(m.line)+" installs middleware on "+
			recv+", not on "+guardedAPIIdent+": "+recv+".UseMiddleware("+m.arg+"). huma snapshots "+
			"the chain per api, so this installs NOTHING on the api the guarded routes register on. "+
			"If it is a typo, it is the whole of #1560 in one token. If that api deliberately has "+
			"its own middleware stack, add it to secondaryAPIMiddlewareExemptions in this file with "+
			"the reason why its routes do not depend on "+guardedAPIIdent+"'s chain (see the "+
			"internalHumaAPI entry) — a middleware from requestScopedMiddlewareMarkers is still "+
			"refused there.")
	}
	return violations
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
	for _, problem := range otherReceiverMiddlewareViolations(scan.middlewareOtherReceiver) {
		t.Error(problem)
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

// TestOtherReceiverMiddlewareIsExemptedOnlyWhenSafe pins the resolution of the guard's
// two-axis asymmetry (T62 finding 3): internalHumaAPI is recognised by
// earlyRegistrationViolations for route registrations, and is now recognised here too — but the
// exemption is bounded, and everything else on another receiver still fails with a message that
// names the exemption and what to do about it.
func TestOtherReceiverMiddlewareIsExemptedOnlyWhenSafe(t *testing.T) {
	fixture := func(t *testing.T, body string) []middlewareCall {
		t.Helper()
		scan, err := scanGoSource("main.go", "package main\n\nfunc main() {\n"+body+"\n}\n")
		if err != nil {
			t.Fatalf("scan: %v", err)
		}
		if len(scan.middlewareOtherReceiver) == 0 {
			t.Fatalf("fixture produced no other-receiver call — it proves nothing:\n%s", body)
		}
		return scan.middlewareOtherReceiver
	}

	t.Run("a middleware of its own on internalHumaAPI is tolerated", func(t *testing.T) {
		got := otherReceiverMiddlewareViolations(fixture(t,
			"\tinternalHumaAPI.UseMiddleware(internalOnlyRateLimitMiddleware)"))
		if len(got) != 0 {
			t.Errorf("violations = %v, want none. internalHumaAPI is the internal metrics "+
				"listener with its own stack; failing here with a #1560 accusation for an ordinary "+
				"addition is the false-failure this exemption removes — and it is exactly the "+
				"reasoning earlyRegistrationViolations already applies to the same identifier.", got)
		}
	})

	t.Run("humaAPI's own middleware on internalHumaAPI is still fatal", func(t *testing.T) {
		got := otherReceiverMiddlewareViolations(fixture(t,
			"\tinternalHumaAPI.UseMiddleware(humamiddleware.NewCacheInvalidationMiddleware(a, b, c))"))
		if len(got) != 1 {
			t.Fatalf("violations = %v, want exactly one. The exemption must not swallow the T49/H1a "+
				"one-token defect: moving humaAPI's cache-invalidation middleware to another api "+
				"leaves every post-anchor mutating route uninvalidated.", got)
		}
		for _, want := range []string{"cache-invalidation", "NewCacheInvalidationMiddleware", "internalHumaAPI"} {
			if !strings.Contains(got[0], want) {
				t.Errorf("violation %q does not name %q", got[0], want)
			}
		}
	})

	t.Run("an unrecognised api is fatal and says what to do", func(t *testing.T) {
		got := otherReceiverMiddlewareViolations(fixture(t,
			"\tsomeOtherAPI.UseMiddleware(loggingMiddleware)"))
		if len(got) != 1 {
			t.Fatalf("violations = %v, want exactly one — an unrecognised api must not be assumed "+
				"harmless", got)
		}
		for _, want := range []string{"someOtherAPI", "secondaryAPIMiddlewareExemptions", "internalHumaAPI"} {
			if !strings.Contains(got[0], want) {
				t.Errorf("violation %q does not name %q. A guard that fails without naming its own "+
					"exemption hatch is the guard that gets deleted instead of taught.", got[0], want)
			}
		}
	})

	t.Run("a receiver that is not a plain identifier is fatal", func(t *testing.T) {
		got := otherReceiverMiddlewareViolations(fixture(t,
			"\tapis[0].UseMiddleware(loggingMiddleware)"))
		if len(got) != 1 || !strings.Contains(got[0], "<not a plain identifier>") {
			t.Errorf("violations = %v, want one naming the unresolvable receiver — an exemption is "+
				"keyed by identifier, and a receiver with no identifier can match none of them", got)
		}
	})

	t.Run("an exemption with an empty reason is refused", func(t *testing.T) {
		secondaryAPIMiddlewareExemptions["bareAPI"] = "   "
		t.Cleanup(func() { delete(secondaryAPIMiddlewareExemptions, "bareAPI") })
		got := otherReceiverMiddlewareViolations(fixture(t,
			"\tbareAPI.UseMiddleware(loggingMiddleware)"))
		if len(got) != 1 || !strings.Contains(got[0], "empty reason") {
			t.Errorf("violations = %v, want the empty-reason refusal — an unexplained exemption is "+
				"how the registration axis' exemptions would have rotted too", got)
		}
	})

	t.Run("every exemption in the map carries a reason", func(t *testing.T) {
		if len(secondaryAPIMiddlewareExemptions) == 0 {
			t.Skip("no exemptions to check")
		}
		for api, reason := range secondaryAPIMiddlewareExemptions {
			if strings.TrimSpace(reason) == "" {
				t.Errorf("secondaryAPIMiddlewareExemptions[%q] has no reason", api)
			}
		}
	})
}

// --- #1683 Task 4: request-audit middleware ordering on the Echo chain --------------------------
//
// #1683 adds middleware.RequestAudit(), an ECHO middleware (e.Use) that captures the client IP for
// audit rows. Its IP rule is trustworthy ONLY when CloudflareOnly has already run and recorded
// GateStatusPassed, so the ordering is load-bearing in the same way CloudflareOnly-before-
// IPRateLimiter is: registered first, RequestAudit would observe GateStatusUnknown for every
// request and silently record no IP at all. The #1560 scan above watches humaAPI.UseMiddleware
// (the Huma chain); this is the Echo chain, so it needs its own check.

// echoUseCall is one `e.Use(...)` argument (or one argument of one) on the Echo chain.
type echoUseCall struct {
	line int
	arg  string
}

// requestAuditOrderingViolations reports the ordering problems request-audit middleware installs
// can have: missing CloudflareOnly, missing/duplicated RequestAudit, or RequestAudit registered
// BEFORE CloudflareOnly. Extracted so a fixture with the wrong order can drive it.
func requestAuditOrderingViolations(calls []echoUseCall) []string {
	var cfLine, auditLine, auditCount int
	for _, c := range calls {
		switch {
		case strings.Contains(c.arg, "middleware.CloudflareOnly"):
			cfLine = c.line
		case strings.Contains(c.arg, "middleware.RequestAudit"):
			auditLine = c.line
			auditCount++
		}
	}
	var out []string
	if cfLine == 0 {
		out = append(out, "main.go: no e.Use(middleware.CloudflareOnly(...)) call found — the gate "+
			"RequestAudit's IP rule depends on is not installed on the Echo chain")
	}
	switch {
	case auditCount == 0:
		out = append(out, "main.go: no e.Use(middleware.RequestAudit()) call found — #1683's "+
			"request-audit middleware is not installed")
	case auditCount > 1:
		out = append(out, "main.go: e.Use(middleware.RequestAudit()) appears more than once — "+
			"duplicate middleware install")
	}
	if cfLine != 0 && auditLine != 0 && auditLine < cfLine {
		out = append(out, "main.go: RequestAudit (line "+strconv.Itoa(auditLine)+") is registered "+
			"BEFORE CloudflareOnly (line "+strconv.Itoa(cfLine)+") — GateStatusFrom would always "+
			"read GateStatusUnknown and every audit row would silently record no IP")
	}
	return out
}

// TestRequestAuditMiddlewareOrdering pins the real main.go: RequestAudit is installed on the Echo
// chain, exactly once, AFTER CloudflareOnly.
func TestRequestAuditMiddlewareOrdering(t *testing.T) {
	raw, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatalf("read main.go: %v", err)
	}
	src := string(raw)

	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "main.go", src, 0)
	if err != nil {
		t.Fatalf("parse main.go: %v", err)
	}

	var calls []echoUseCall
	ast.Inspect(f, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || sel.Sel.Name != "Use" {
			return true
		}
		recv, ok := sel.X.(*ast.Ident)
		if !ok || recv.Name != "e" {
			return true
		}
		for _, a := range call.Args {
			start, end := fset.Position(a.Pos()).Offset, fset.Position(a.End()).Offset
			if start < 0 || end > len(src) || start > end {
				continue
			}
			calls = append(calls, echoUseCall{
				line: fset.Position(call.Pos()).Line,
				arg:  strings.TrimSpace(src[start:end]),
			})
		}
		return true
	})

	for _, v := range requestAuditOrderingViolations(calls) {
		t.Error(v)
	}
}

// TestRequestAuditOrderingViolations_FailClosed drives the extracted check from fixtures: the
// guard must FAIL on the wrong order and on a missing install, not merely pass on correct code.
func TestRequestAuditOrderingViolations_FailClosed(t *testing.T) {
	t.Run("reversed order fails", func(t *testing.T) {
		got := requestAuditOrderingViolations([]echoUseCall{
			{line: 1, arg: "middleware.RequestAudit()"},
			{line: 2, arg: "middleware.CloudflareOnly(cfCIDRRef, cfg.CFOriginSecret, cfg.Environment)"},
		})
		if len(got) == 0 {
			t.Error("reversed order produced no violation — the guard would stay green while every " +
				"audit row silently records no IP")
		}
	})
	t.Run("missing RequestAudit fails", func(t *testing.T) {
		got := requestAuditOrderingViolations([]echoUseCall{
			{line: 1, arg: "middleware.CloudflareOnly(cfCIDRRef, cfg.CFOriginSecret, cfg.Environment)"},
		})
		if len(got) == 0 {
			t.Error("a missing RequestAudit install produced no violation")
		}
	})
	t.Run("duplicate RequestAudit fails", func(t *testing.T) {
		got := requestAuditOrderingViolations([]echoUseCall{
			{line: 1, arg: "middleware.CloudflareOnly(x)"},
			{line: 2, arg: "middleware.RequestAudit()"},
			{line: 3, arg: "middleware.RequestAudit()"},
		})
		if len(got) == 0 {
			t.Error("a duplicate RequestAudit install produced no violation")
		}
	})
}
