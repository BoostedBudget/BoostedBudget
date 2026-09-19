package middleware_test

// Issue #1432 (batch ED, finding B2-2): the admin middleware gates on ONE thing —
// strings.HasPrefix(ctx.Operation().Path, "/api/v1/admin") (admin.go). Nothing asserted that
// every privileged operation actually sits under that prefix, and nothing asserted that the
// operations which no middleware gates at all are the ones somebody reviewed.
//
// That is exactly how finding B2-1's route escaped: POST /api/v1/qa/auth/bypass mints a
// five-minute Clerk ADMIN sign-in token, carries no Clerk security annotation, and lives
// outside /api/v1/admin — so neither middleware ever sees it, and no test noticed.
//
// These tests copy the shape of read_only_route_coverage_test.go in this package: build the
// SAME route table OpenAPI spec generation uses (operations.RegisterAllRoutesForSpec) and
// reason over api.OpenAPI().Paths. They live in the external middleware_test package for the
// same reason that one does — operations imports internal/huma/middleware, so an internal test
// file cannot import operations without an import cycle.
//
// ── What "privileged" means MECHANICALLY ────────────────────────────────────────────────────
//
// A hand-maintained list of privileged operations would have the denominator problem this
// round has already hit twice: whatever is missing from the list is invisible. So privilege is
// DERIVED from the spec, two independent ways, and the two tests below are complementary:
//
//   1. DECLARED privilege (TestAdminPrivilegedOperations_SitUnderAdminPrefix): an operation is
//      admin-privileged if its own metadata says so — any tag containing "admin", or an
//      operationID containing "admin" (both case-insensitively). The route author writes both
//      as a matter of course. This catches an admin operation registered at the wrong path.
//
//   2. UNGATED-ness (TestUnauthenticatedOperations_MatchReviewedInventory): an operation with
//      no Security requirement is skipped outright by the Clerk middleware
//      ("Skip auth for routes without Security annotation" — clerk.go), and if it is also
//      outside /api/v1/admin the admin middleware never sees it either. Such an operation is
//      gated by NOTHING the framework provides; whatever authority it grants, it grants to
//      anonymous callers. The denominator there is EVERY registered operation, so a new
//      unauthenticated route cannot be invisible — it fails the test and forces a decision.
//
// Rule 2 is the one that catches B2-1's class. Rule 1 alone would not: that route is tagged
// "QA", and an author who puts a privileged route outside the prefix is unlikely to tag it
// "Admin". Rule 2 does not care what it is called.

import (
	"fmt"
	"sort"
	"strings"
	"testing"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/humatest"
	"github.com/maxnussbaum/BoostedBudget/internal/huma/operations"
)

// adminPathPrefix is the prefix NewAdminMiddleware keys on. Kept as a literal rather than
// exported from the middleware so that a change to the middleware's prefix has to be made
// deliberately in two places, both of which are security-relevant.
const adminPathPrefix = "/api/v1/admin"

// specOperation is one registered operation, flattened out of the OpenAPI path item.
type specOperation struct {
	path string
	op   *huma.Operation
}

// registeredOperations builds the real route table and flattens every method on every path.
// It fails the test outright on an empty table — a zero-route spec would make every assertion
// below vacuously true, which is the failure mode that makes coverage tests worthless.
func registeredOperations(t *testing.T) []specOperation {
	t.Helper()

	_, api := humatest.New(t)
	operations.RegisterAllRoutesForSpec(api)

	paths := api.OpenAPI().Paths
	if len(paths) == 0 {
		t.Fatal("RegisterAllRoutesForSpec registered zero routes — did the registration helper move? " +
			"(this test cannot prove anything against an empty route table)")
	}

	var ops []specOperation
	for path, item := range paths {
		if item == nil {
			continue
		}
		for _, op := range []*huma.Operation{
			item.Get, item.Post, item.Put, item.Patch, item.Delete,
			item.Head, item.Options, item.Trace,
		} {
			if op != nil {
				ops = append(ops, specOperation{path: path, op: op})
			}
		}
	}
	if len(ops) == 0 {
		t.Fatal("flattened zero operations out of a non-empty path table — the flattening is broken")
	}
	return ops
}

// declaresAdminPrivilege derives "this operation is admin-privileged" from the operation's own
// metadata: any tag containing "admin", or an operationID containing "admin".
func declaresAdminPrivilege(op *huma.Operation) bool {
	if strings.Contains(strings.ToLower(op.OperationID), "admin") {
		return true
	}
	for _, tag := range op.Tags {
		if strings.Contains(strings.ToLower(tag), "admin") {
			return true
		}
	}
	return false
}

// TestAdminPrivilegedOperations_SitUnderAdminPrefix — every operation that declares itself
// admin-privileged must sit under the prefix the admin middleware keys on. There is no
// exception list: an admin operation outside the prefix is unreachable by the only middleware
// that checks for domain.RoleAdmin, so it would be served to any authenticated user.
func TestAdminPrivilegedOperations_SitUnderAdminPrefix(t *testing.T) {
	ops := registeredOperations(t)

	privileged := 0
	for _, o := range ops {
		if !declaresAdminPrivilege(o.op) {
			continue
		}
		privileged++
		if !strings.HasPrefix(o.path, adminPathPrefix) {
			t.Errorf("operation %q at %q declares admin privilege but sits outside %q — NewAdminMiddleware "+
				"gates purely on that prefix, so nothing checks for domain.RoleAdmin before this handler runs (#1432 B2-2)",
				o.op.OperationID, o.path, adminPathPrefix)
		}
	}

	// Control: the loop above is only meaningful if it actually classified something as
	// privileged. If the derivation stops matching (a tag rename, an operationID convention
	// change), this catches it instead of the test silently passing over an empty set.
	if privileged == 0 {
		t.Fatal("derived zero admin-privileged operations from the spec — the tag/operationID derivation has " +
			"stopped matching and this test is no longer checking anything")
	}
}

// TestAdminPrefixOperations_DeclareClerkSecurity — the complement. Everything under the admin
// prefix must ALSO carry the clerk security requirement, because the Clerk middleware skips
// any operation with no Security (clerk.go), which would leave the admin middleware looking up
// a Clerk user ID that nothing ever set.
func TestAdminPrefixOperations_DeclareClerkSecurity(t *testing.T) {
	ops := registeredOperations(t)

	checked := 0
	for _, o := range ops {
		if !strings.HasPrefix(o.path, adminPathPrefix) {
			continue
		}
		checked++

		hasClerk := false
		for _, requirement := range o.op.Security {
			if _, ok := requirement["clerk"]; ok {
				hasClerk = true
				break
			}
		}
		if !hasClerk {
			t.Errorf("admin operation %q at %q does not require the \"clerk\" security scheme — the Clerk "+
				"middleware skips operations with no Security, so no Clerk user ID would ever reach the admin "+
				"middleware (#1432 B2-2)", o.op.OperationID, o.path)
		}
	}

	if checked == 0 {
		t.Fatalf("found zero operations under %q — the admin routes are not in the spec route table", adminPathPrefix)
	}
}

// reviewedUnauthenticatedOperations is the REVIEWED inventory of operations that deliberately
// carry no Security requirement, keyed by operationID, with the reason each one is safe.
//
// This is NOT an exception list bolted onto a partial scan: the denominator is every operation
// RegisterAllRoutesForSpec registers, and the test asserts SET EQUALITY. Adding an
// unauthenticated route fails the test; so does removing one, which stops the inventory rotting
// into dead entries the way readOnlyPrefixAllowedPaths did (#1432, the sibling test in this
// package).
//
// Each value states what stops an anonymous caller from getting authority. Any future entry
// must be able to say the same.
var reviewedUnauthenticatedOperations = map[string]string{
	"getHealth":                 "liveness probe; returns no user data",
	"getRegions":                "static region catalogue from config",
	"getSubscriptionTiers":      "static tier catalogue from config/subscription_tiers.yaml",
	"getWidgetTypes":            "static dashboard widget catalogue",
	"getDefaultLayout":          "static default dashboard layout from config",
	"getSavingsImpactAggregate": "deliberately public aggregate (#972); no per-user data",
	"cancelDeletion":            "authority comes from a signed, expiring deletion token in the path (DELETION_TOKEN_SECRET); the user is mid-deletion and cannot be asked to sign in",
	"getHouseholdInvitePreview": "authority comes from the unguessable invite token in the path; returns invite preview only",
	"handlePlaidWebhook":        "unauthenticated by protocol; verified by Plaid webhook signature in the handler",
	"paddle-webhook":            "unauthenticated by protocol; verified by Paddle webhook signature in the handler",
	"revenuecat-webhook":        "provider authentication: exact Authorization plus timestamped HMAC-SHA256 of the raw body; accepted events request canonical customer readback and never directly grant access",
	"recordMCPAuditEntry":       "MCP Worker origin proven by X-MCP-Worker-Secret plus a Clerk-verified OAuth token in the handler path",
	"qaAuthBypass": "THE DANGEROUS ONE (#1432 B2-1): mints a five-minute Clerk ADMIN sign-in token. It is " +
		"unauthenticated by necessity — it is how you obtain credentials — so it is gated at REGISTRATION " +
		"instead: config.Config.QAAuthBypassRefusalReason requires an explicitly-set ENVIRONMENT on a " +
		"two-value allowlist, the QA_AUTH_BYPASS_ENABLED opt-in, the three QA settings, and the ABSENCE of " +
		"any production-only signal (a Clerk sk_live_ key, PADDLE_ENVIRONMENT=production). The handler " +
		"re-checks the environment and compares the shared secret in constant time. Do not add a second " +
		"route like this without an equivalent gate.",
}

// TestUnauthenticatedOperations_MatchReviewedInventory is the mechanism that would have
// surfaced B2-1: it enumerates every registered operation with no Security requirement — i.e.
// every operation the Clerk middleware skips outright — and asserts that set is exactly the
// reviewed inventory above.
func TestUnauthenticatedOperations_MatchReviewedInventory(t *testing.T) {
	ops := registeredOperations(t)

	found := map[string]string{}
	for _, o := range ops {
		if len(o.op.Security) == 0 {
			found[o.op.OperationID] = o.path
		}
	}

	if len(found) == 0 {
		t.Fatal("found zero unauthenticated operations — /health is unauthenticated, so this " +
			"derivation has broken and the test is no longer checking anything")
	}

	var unreviewed, stale []string
	for id, path := range found {
		if _, ok := reviewedUnauthenticatedOperations[id]; !ok {
			unreviewed = append(unreviewed, fmt.Sprintf("%s (%s)", id, path))
		}
	}
	for id := range reviewedUnauthenticatedOperations {
		if _, ok := found[id]; !ok {
			stale = append(stale, id)
		}
	}
	sort.Strings(unreviewed)
	sort.Strings(stale)

	if len(unreviewed) > 0 {
		t.Errorf("these operations carry NO Security requirement and are not in the reviewed inventory: %v.\n"+
			"The Clerk middleware skips operations with no Security, and the admin middleware only sees "+
			"%q — so nothing the framework provides gates these. This is precisely the shape of #1432 B2-1 "+
			"(POST /api/v1/qa/auth/bypass mints an admin sign-in token from outside both middlewares).\n"+
			"Either add a Security requirement, or add an entry to reviewedUnauthenticatedOperations stating "+
			"what stops an anonymous caller from gaining authority.",
			unreviewed, adminPathPrefix)
	}
	if len(stale) > 0 {
		t.Errorf("reviewedUnauthenticatedOperations lists operations that are no longer registered as "+
			"unauthenticated: %v. Remove them — a stale entry reads as a reviewed decision but covers nothing "+
			"(same defect as the dead readOnlyPrefixAllowedPaths strings in #1432).", stale)
	}
}
