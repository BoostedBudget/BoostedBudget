package operations

// Issue #1531 Task 10b-3 — AST completeness guard #3 of 3.
//
// WHAT THIS PROVES: every huma.Register call in this package whose Method is POST/PUT/PATCH/DELETE
// either (a) declares a non-empty Security field, meaning it runs behind Clerk auth AND therefore
// behind NewCacheInvalidationMiddleware (internal/huma/middleware/cache_invalidation.go), which
// bumps the acting user's — and, when resolvable, their household's — cache generation after every
// such authenticated mutating request; or (b) is named in publicMutationAllowlist with a mandatory,
// non-empty reason explaining why it needs no HTTP-layer bump.
//
// WHAT THIS DOES NOT PROVE (read this before trusting a green run):
//   - A secured huma.Register proves auth METADATA exists (a non-empty Security slice), not that
//     the handler's mutation affects only the acting user, not that the middleware is actually
//     registered ahead of it in main.go, and not that the handler's write path is otherwise correct.
//     Wiring order is asserted elsewhere (cmd/server/issue1514_rate_limit_ordering_test.go and
//     internal/huma/middleware's own tests), not here.
//   - It does not inspect the handler body at all — a handler that writes to a repository directly
//     without going through anything HTTP-request-scoped would still pass, because this guard only
//     ever looks at the huma.Operation composite literal, never the handler function body.
//   - A newly added mutating operation with Security already set passes silently and correctly
//     requires no allowlist entry — this guard only forces a decision for the NO-Security case. That
//     is deliberate (Security ⇒ middleware ⇒ already covered) but it means this guard alone cannot
//     tell you whether that middleware's bump is scoped correctly for the new operation's side
//     effects; see cache_invalidation.go's own tests for that question.
//
// ADVERSARIAL VALIDATION (recorded 2026-08-14; the cited task-10b-3-report.md was never
// committed and does not exist in any commit — see docs/architecture/cache-invalidation-dispositions.md):
//   - Added a fake huma.Register call (Method: http.MethodPost, no Security, OperationID
//     "fakeUnclassifiedMutation") to a scratch file in this package: guard FAILED, reporting the new
//     operation ID as missing from publicMutationAllowlist. Removed it: guard PASSED.
//
// Issue #1531 Slice B / L9 fix (mutation-verified): the doc comment above was already accurate,
// but the CODE didn't match it — the scan set hasSecurity=true merely because a `Security:` key
// was present, regardless of its value's length, so `Security: []map[string][]string{}` (empty,
// present) passed as "covered" even though the middleware's actual gate is
// `len(ctx.Operation().Security) == 0` (cache_invalidation.go), which treats that identically to
// no Security field at all. Fixed by resolving the Security value's element count
// (securityIsNonEmpty) instead of merely checking key presence, and by reporting (rather than
// silently skipping, via mutatingHTTPMethods[""] == false) any Method/Security expression this
// AST scan cannot resolve. Verified by temporarily setting a real mutating operation's Security
// to `[]map[string][]string{}` (tags.go's createTag): guard FAILED, naming createTag specifically.
// Restored: guard PASSED.

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// publicMutationAllowlist enumerates every mutating (POST/PUT/PATCH/DELETE) huma.Register call in
// this package that declares NO Security field, together with the reason it needs none. An empty
// reason is a compile-time-visible mistake (see the test below, which rejects it), not merely a
// style nit — it is the whole point of the allowlist.
var publicMutationAllowlist = map[string]string{
	"revenuecat-webhook": "Authenticated by exact authorization and raw-body HMAC. Ingress writes only the durable receipt and queue job atomically; authorization projections and compatibility fields are materialized by the uncached RevenueCat worker, never cached financial data.",
	"paddle-webhook": "Public by design — authenticated via Paddle HMAC-SHA256 signature " +
		"verification (not Clerk), not by this middleware. The write path is async: the raw " +
		"webhook is only persisted here; queue/jobs/subscription_webhook.go processes it and is " +
		"covered by its own job-level cache-invalidation disposition (see the job guard).",
	"handlePlaidWebhook": "Public by design — authenticated via Plaid JWT signature verification " +
		"(not Clerk), not by this middleware. The write path is async: the raw webhook is only " +
		"persisted here; queue/jobs/webhook_processing.go and queue/jobs/plaid_sync.go process it " +
		"and are covered by their own job-level cache-invalidation disposition (see the job guard).",
	"recordMCPAuditEntry": "Public relative to Clerk (authenticated instead by CloudflareOnly " +
		"middleware via X-Origin-Secret, a service-to-service secret, not an end-user JWT). Writes " +
		"only to the MCP audit log (MCPAuditRepository.Record) — no cached namespace (analytics, " +
		"analytics_rows, dashprefs, identity) is touched.",
	"qaAuthBypass": "Public by design (QA/Staging test-user login flow) and creates only an " +
		"ephemeral Clerk sign-in token URL — no domain row is written, so no cached namespace is " +
		"touched.",
}

var mutatingHTTPMethods = map[string]bool{
	"POST":   true,
	"PUT":    true,
	"PATCH":  true,
	"DELETE": true,
}

// httpMethodConstants maps the *ast.SelectorExpr spelling (http.MethodPost, etc.) used by most
// call sites in this package to the verb it evaluates to, so the guard doesn't need to import
// net/http and can stay a pure syntax check.
var httpMethodConstants = map[string]string{
	"MethodGet":     "GET",
	"MethodHead":    "HEAD",
	"MethodPost":    "POST",
	"MethodPut":     "PUT",
	"MethodPatch":   "PATCH",
	"MethodDelete":  "DELETE",
	"MethodOptions": "OPTIONS",
}

// humaOperationRegistration is one huma.Register(api, huma.Operation{...}, handler) call found by
// AST walk, with just the fields this guard needs.
type humaOperationRegistration struct {
	operationID string
	method      string // resolved HTTP verb, "" if unresolvable
	// methodResolved is false when Method is present but this scan could not resolve it to a
	// known verb (e.g. an expression shape resolveMethodExpr doesn't recognise). Issue #1531
	// Slice B / L9: the doc comment above claims unresolvable Method expressions are reported,
	// but the original test folded "" (unresolvable) into "not a mutating method" via
	// mutatingHTTPMethods[""] == false and silently `continue`d — an operation whose Method this
	// guard cannot read was indistinguishable from a GET. This field lets the test tell the two
	// apart and actually report the former instead of skipping it.
	methodResolved bool
	// hasSecurity mirrors the middleware's ACTUAL gate (cache_invalidation.go:
	// len(ctx.Operation().Security) == 0 means "no authenticated actor, don't bump"), not merely
	// whether a `Security:` key is present in the composite literal. Issue #1531 Slice B / L9:
	// the original scan set this true just because the key existed, so
	// `Security: []map[string][]string{}` (present but empty) passed this guard as "covered" while
	// the middleware itself would treat it identically to no Security field at all and skip the
	// bump — a false negative this guard could not catch.
	hasSecurity bool
	// securityResolved is false when a Security field is present but its value isn't a literal
	// this scan can measure the length of (e.g. built from a variable/function call) — reported
	// rather than silently assumed covered.
	securityResolved bool
	file             string
	line             int
}

// scanHumaRegistrations walks every non-test .go file directly in dir (no recursion — this package
// has no subpackages) looking for huma.Register(api, huma.Operation{...}, ...) calls, and returns
// one humaOperationRegistration per call found.
//
// This is a syntax-only AST walk (go/parser, no type-checking, no go/packages load): it matches
// calls shaped like `huma.Register(<anything>, huma.Operation{...}, <anything>)` by identifier
// name, not by resolved type. A local shadowing of an identifier named `huma` would fool it; there
// is none in this codebase today.
func scanHumaRegistrations(dir string) ([]humaOperationRegistration, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}

	var out []humaOperationRegistration
	fset := token.NewFileSet()
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		path := filepath.Join(dir, name)
		file, err := parser.ParseFile(fset, path, nil, parser.ParseComments)
		if err != nil {
			return nil, err
		}

		ast.Inspect(file, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			pkgIdent, ok := sel.X.(*ast.Ident)
			if !ok || pkgIdent.Name != "huma" || sel.Sel.Name != "Register" {
				return true
			}
			if len(call.Args) < 2 {
				return true
			}
			lit, ok := call.Args[1].(*ast.CompositeLit)
			if !ok {
				return true
			}

			// securityResolved defaults to true: an operation with NO Security key at all is a
			// fully-resolved "no security" (hasSecurity's zero value, false, is already correct
			// for that case) — only a Security key present with an unresolvable value should
			// flip this to false. See the "Security" case below and securityIsNonEmpty's doc
			// comment.
			reg := humaOperationRegistration{
				file:             name,
				line:             fset.Position(call.Pos()).Line,
				securityResolved: true,
			}
			for _, elt := range lit.Elts {
				kv, ok := elt.(*ast.KeyValueExpr)
				if !ok {
					continue
				}
				key, ok := kv.Key.(*ast.Ident)
				if !ok {
					continue
				}
				switch key.Name {
				case "OperationID":
					if bl, ok := kv.Value.(*ast.BasicLit); ok && bl.Kind == token.STRING {
						reg.operationID = strings.Trim(bl.Value, `"`)
					}
				case "Method":
					reg.method, reg.methodResolved = resolveMethodExpr(kv.Value)
				case "Security":
					reg.hasSecurity, reg.securityResolved = securityIsNonEmpty(kv.Value)
				}
			}
			out = append(out, reg)
			return true
		})
	}
	return out, nil
}

// resolveMethodExpr resolves a huma.Operation.Method value written either as a raw string literal
// ("POST", used by paddle_checkout.go and paddle_webhook.go) or as an http.MethodX selector (used
// everywhere else in this package). Returns ("", false) when the expression shape isn't one of
// those two forms — the caller is responsible for REPORTING that (Issue #1531 Slice B / L9),
// rather than silently treating it the same as a resolved non-mutating verb.
func resolveMethodExpr(expr ast.Expr) (string, bool) {
	switch v := expr.(type) {
	case *ast.BasicLit:
		if v.Kind == token.STRING {
			return strings.Trim(v.Value, `"`), true
		}
	case *ast.SelectorExpr:
		if ident, ok := v.X.(*ast.Ident); ok && ident.Name == "http" {
			if verb, ok := httpMethodConstants[v.Sel.Name]; ok {
				return verb, true
			}
		}
	}
	return "", false
}

// securityIsNonEmpty reports whether a huma.Operation.Security field's value is a non-empty
// slice literal, mirroring the middleware's ACTUAL gate (cache_invalidation.go:
// len(ctx.Operation().Security) == 0) rather than merely "the Security key was present in the
// composite literal" (Issue #1531 Slice B / L9 — the original scan's mistake). Returns
// (nonEmpty, resolved): resolved is false when expr isn't a composite literal this scan can count
// the elements of (e.g. built from a variable), and the caller reports that rather than assuming
// coverage either way.
func securityIsNonEmpty(expr ast.Expr) (nonEmpty bool, resolved bool) {
	lit, ok := expr.(*ast.CompositeLit)
	if !ok {
		return false, false
	}
	return len(lit.Elts) > 0, true
}

// TestEveryMutatingHumaOperationIsCoveredByMiddleware is completeness guard #3 (see the package
// doc comment above this test file's own header for what it does and does NOT prove).
func TestEveryMutatingHumaOperationIsCoveredByMiddleware(t *testing.T) {
	regs, err := scanHumaRegistrations(".")
	if err != nil {
		t.Fatalf("scanning huma.Register calls: %v", err)
	}
	if len(regs) == 0 {
		t.Fatal("scanned zero huma.Register calls — guard is almost certainly looking at the wrong " +
			"directory or the AST match broke; a silently-empty scan is worse than no guard at all")
	}

	var uncovered []string
	for _, r := range regs {
		// Issue #1531 Slice B / L9: a Method this scan could not resolve is NOT the same as a
		// resolved non-mutating verb (GET/HEAD/OPTIONS) — the original code folded both into
		// "skip" via mutatingHTTPMethods[""] == false, silently passing an operation whose Method
		// it never actually read. Report it instead of skipping it.
		if !r.methodResolved {
			uncovered = append(uncovered, fmt.Sprintf("%s (%s:%d): Method expression could not be "+
				"resolved by this AST scan — cannot determine whether this operation is mutating",
				r.operationID, r.file, r.line))
			continue
		}
		if !mutatingHTTPMethods[r.method] {
			continue
		}
		// Likewise, a Security field present but not resolvable to a literal this scan can count
		// is reported rather than silently treated as "covered" (the failure mode this guard is
		// meant to close: an unresolved/empty Security value passing as if it were non-empty).
		if !r.securityResolved {
			uncovered = append(uncovered, fmt.Sprintf("%s (%s:%d): Security expression could not be "+
				"resolved by this AST scan — cannot determine whether this mutating operation is "+
				"actually covered by NewCacheInvalidationMiddleware", r.operationID, r.file, r.line))
			continue
		}
		if r.hasSecurity {
			continue
		}
		reason, ok := publicMutationAllowlist[r.operationID]
		if !ok {
			uncovered = append(uncovered, fmt.Sprintf("%s (%s:%d, method=%s): no Security field (or "+
				"an empty one — the middleware gates on len(Security) == 0, same as an absent field) "+
				"and not in publicMutationAllowlist", r.operationID, r.file, r.line, r.method))
			continue
		}
		if strings.TrimSpace(reason) == "" {
			t.Errorf("publicMutationAllowlist[%q] has an empty reason — a bare entry defeats the "+
				"point of this guard", r.operationID)
		}
	}
	if len(uncovered) > 0 {
		t.Errorf("%d mutating huma operation(s) have neither a Security field nor an explicit "+
			"publicMutationAllowlist entry — a mutation with no authenticated actor never reaches "+
			"NewCacheInvalidationMiddleware, so nothing bumps cache on write:\n%s",
			len(uncovered), strings.Join(uncovered, "\n"))
	}
}
