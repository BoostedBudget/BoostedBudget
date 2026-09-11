package jobs

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/maxnussbaum/BoostedBudget/internal/queue"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// kindLiteralsDeclaredInPackage parses every non-test file in this package and returns the string
// literal each `func (XArgs) Kind() string { return "..." }` declares.
//
// AST rather than grep, for the same reason item_cleanup_test.go's caller census uses the AST: a
// literal is a literal however it is spelled and whatever whitespace or comments surround it, and
// nothing in a comment can fake one. This is the enumeration the completeness checks below are
// measured against, so it must not be a hand-maintained list — a hand-maintained list would have to
// be updated by the same person who forgot to update the registry.
func kindLiteralsDeclaredInPackage(t *testing.T) map[string]string {
	t.Helper()

	fset := token.NewFileSet()
	entries, err := os.ReadDir(".")
	require.NoError(t, err)

	// ParseFile per entry rather than parser.ParseDir: ParseDir is deprecated as of Go 1.25 (it
	// ignores build tags when grouping files into packages), and golangci-lint's staticcheck fails
	// the build on SA1019. Nothing in this package is behind a build tag, so per-file parsing of
	// every non-test .go file is the same file set ParseDir would have produced.
	kinds := map[string]string{} // kind literal -> receiver type name
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(fset, name, nil, 0)
		require.NoError(t, err)
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Recv == nil || fn.Name.Name != "Kind" || fn.Body == nil {
				continue
			}
			if fn.Type.Results == nil || len(fn.Type.Results.List) != 1 {
				continue
			}
			if ident, ok := fn.Type.Results.List[0].Type.(*ast.Ident); !ok || ident.Name != "string" {
				continue
			}
			for _, stmt := range fn.Body.List {
				ret, ok := stmt.(*ast.ReturnStmt)
				if !ok || len(ret.Results) != 1 {
					continue
				}
				lit, ok := ret.Results[0].(*ast.BasicLit)
				if !ok || lit.Kind != token.STRING {
					// A Kind() that returns something other than a literal (queue.RawJobArgs.Kind
					// returns a field) cannot be enumerated statically — that is #1486's point. No
					// such method exists in THIS package; if one appears, this test under-counts and
					// the assertion below stops being a real completeness check, so fail loudly.
					t.Fatalf("Kind() in %s returns a non-literal; the kind enumeration is no longer complete",
						fset.Position(fn.Pos()))
				}
				kind, err := strconv.Unquote(lit.Value)
				require.NoError(t, err)
				recvType := ""
				if len(fn.Recv.List) == 1 {
					if ident, ok := fn.Recv.List[0].Type.(*ast.Ident); ok {
						recvType = ident.Name
					}
				}
				kinds[kind] = recvType
			}
		}
	}
	// Sanity floor on the scan itself: if the parse silently matched nothing, every "is covered"
	// assertion below would pass vacuously.
	require.Greater(t, len(kinds), 20, "the AST scan found implausibly few job kinds")
	return kinds
}

// TestUniqueOptsByKind_CoversEveryDeclaredKind is the anti-rot guard. A new job kind added without a
// uniqueOptsByKind entry would be silently unreplayable (the replay path fails closed on an unknown
// kind), and a stale entry for a deleted kind is dead weight that reads as coverage.
func TestUniqueOptsByKind_CoversEveryDeclaredKind(t *testing.T) {
	declared := kindLiteralsDeclaredInPackage(t)

	for kind, recvType := range declared {
		_, known := UniqueOptsForKind(kind)
		assert.True(t, known,
			"%s.Kind() = %q has no uniqueOptsByKind entry, so a dead-letter replay of it is refused (#1492)",
			recvType, kind)
	}
	for kind := range uniqueOptsByKind {
		assert.Contains(t, declared, kind,
			"uniqueOptsByKind has an entry for %q but no args type in this package declares that kind", kind)
	}
}

// TestUniqueOptsForKind_MapsEachKindToItsOwnProducerOptions is the double-entry check on the mapping
// itself: the table below is written independently of uniqueOptsByKind, so pointing a kind at the
// wrong *UniqueOpts function reddens it.
//
// Two limits worth naming rather than leaving to be discovered:
//
//  1. InvestmentHoldingsSyncUniqueOpts and InvestmentTransactionsSyncUniqueOpts both return
//     investmentSyncUniqueOpts(), so a swap of those two specific rows is invisible to a value
//     comparison. Nothing here can distinguish them, because nothing about them differs.
//  2. Neither this test nor any other binds the registry to the kinds' REAL PRODUCERS. It cannot: the
//     producers live in internal/services, internal/huma/operations and cmd/server, three of which
//     this package cannot import, and they pass their options in four different shapes (direct
//     *UniqueOpts() call, a services-side mirror, an opts slice built once for three kinds, a
//     periodic *InsertOpts()). So a producer that gains or drops uniqueness will NOT redden anything
//     here — the mapping rests on the audit recorded in unique_opts_by_kind.go, which is manual.
//     That audit already found three kinds (boost_evaluation, subscription_webhook,
//     notification_email) whose *UniqueOpts() helper no producer calls.
func TestUniqueOptsForKind_MapsEachKindToItsOwnProducerOptions(t *testing.T) {
	want := map[string]func() *queue.UniqueOpts{
		"plaid_sync":                        PlaidSyncUniqueOpts,
		"webhook_processing":                WebhookProcessingUniqueOpts,
		"investment_holdings_sync":          InvestmentHoldingsSyncUniqueOpts,
		"investment_transactions_sync":      InvestmentTransactionsSyncUniqueOpts,
		"item_cleanup":                      ItemCleanupUniqueOpts,
		"item_cleanup_check":                ItemCleanupCheckUniqueOpts,
		"account_deletion":                  AccountDeletionUniqueOpts,
		"scheduled_deletion_check":          ScheduledDeletionCheckUniqueOpts,
		"retention_cleanup_check":           RetentionCleanupCheckUniqueOpts,
		"trial_expiration_check":            TrialExpirationCheckUniqueOpts,
		"past_due_check":                    PastDueCheckUniqueOpts,
		"paddle_reconciliation":             PaddleReconciliationUniqueOpts,
		"manual_tier_override_expiry_check": ManualTierOverrideExpiryCheckUniqueOpts,
		"household_member_cleanup_check":    HouseholdMemberCleanupCheckUniqueOpts,
		"household_over_capacity_check":     HouseholdOverCapacityCheckUniqueOpts,
		"budget_period_close_check":         BudgetPeriodCloseCheckUniqueOpts,
		"savings_stats":                     SavingsStatsUniqueOpts,
		"portfolio_snapshot":                PortfolioSnapshotUniqueOpts,
		"budget_hierarchy_audit":            BudgetHierarchyAuditUniqueOpts,
		"dlq_cleanup_check":                 DLQCleanupUniqueOpts,
		"idempotency_cleanup":               IdempotencyCleanupUniqueOpts,
	}

	for kind, expected := range want {
		got, known := UniqueOptsForKind(kind)
		require.True(t, known, "kind %q must be registered", kind)
		require.NotNil(t, got, "kind %q must resolve to real uniqueness options", kind)
		assert.Equal(t, expected(), got, "kind %q resolves to the wrong producer's uniqueness options", kind)
	}
}

// TestUniqueOptsForKind_KindsWithNoUniqueness pins the five kinds whose producers pass no options, so
// that "known, but nil" stays a deliberate answer rather than becoming a forgotten one. Three of them
// (boost_evaluation, subscription_webhook, notification_email) DO have a *UniqueOpts() helper — it is
// simply never called by any producer, so a replay must not call it either.
func TestUniqueOptsForKind_KindsWithNoUniqueness(t *testing.T) {
	for _, kind := range []string{
		"effective_category_refresh",
		"email_change_propagation",
		"boost_evaluation",
		"subscription_webhook",
		"notification_email",
	} {
		opts, known := UniqueOptsForKind(kind)
		assert.True(t, known, "%q must be registered so a replay of it is not refused", kind)
		assert.Nil(t, opts, "%q's producers pass no uniqueness options; a replay must not invent one", kind)
	}
}

// TestArgsTypes_NoByArgsKindHasANestedArgsField enforces the invariant that keeps every registered
// replay's river unique_key equal to its original insert's.
//
// river's buildUniqueKeyString canonicalises only the TOP-LEVEL arg keys under ByArgs; each value's
// raw JSON text is copied through as-is. A replay's args come back out of dead_letter_jobs.args, a
// JSONB column, and a JSONB round trip re-serialises nested objects in Postgres' key order
// (length, then bytewise) with spaces after colons — not the order or spacing encoding/json produces
// for a Go map. So for a kind that combines ByArgs with a nested object, replay and original can hash
// differently. TestDeadLetterReplay_NestedObjectsSurviveOnlyAtTheTopLevel demonstrates that mechanism.
//
// Today NO kind combines the two: the only non-scalar args field in the package is
// BoostEvaluationArgs.EventData, and boost_evaluation's producer passes no uniqueness at all, so its
// registry entry is noUniqueness (ByArgs never applies). This test fails if either half of that
// changes — a new nested field on a ByArgs kind, or uniqueness appearing on a nested-args kind.
func TestArgsTypes_NoByArgsKindHasANestedArgsField(t *testing.T) {
	fset := token.NewFileSet()
	entries, err := os.ReadDir(".")
	require.NoError(t, err)

	// receiver type name -> its struct declaration, for every non-test file in the package.
	structs := map[string]*ast.StructType{}
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(fset, name, nil, 0)
		require.NoError(t, err)
		for _, decl := range file.Decls {
			gen, ok := decl.(*ast.GenDecl)
			if !ok || gen.Tok != token.TYPE {
				continue
			}
			for _, spec := range gen.Specs {
				ts, ok := spec.(*ast.TypeSpec)
				if !ok {
					continue
				}
				if st, ok := ts.Type.(*ast.StructType); ok {
					structs[ts.Name.Name] = st
				}
			}
		}
	}

	// A field is "non-scalar" if encoding/json would emit an object or array for it — the shapes whose
	// internal key order/spacing river copies verbatim. time.Time and other selector types marshal to
	// a scalar string, so they are not in scope; json.RawMessage is, since it passes bytes through.
	nonScalarKinds := map[string][]string{}
	for kind, recvType := range kindLiteralsDeclaredInPackage(t) {
		st, ok := structs[recvType]
		require.True(t, ok, "no struct declaration found for %s (kind %q)", recvType, kind)
		for _, field := range st.Fields.List {
			var suspect bool
			switch ft := field.Type.(type) {
			case *ast.MapType, *ast.StructType, *ast.InterfaceType, *ast.ArrayType:
				suspect = true
			case *ast.SelectorExpr:
				suspect = ft.Sel.Name == "RawMessage"
			}
			if !suspect {
				continue
			}
			for _, n := range field.Names {
				nonScalarKinds[kind] = append(nonScalarKinds[kind], recvType+"."+n.Name)
			}
		}
	}

	// Pinned so that a NEW nested field is noticed at all, not just one that is also ByArgs — the
	// inventory is the thing the comment in unique_opts_by_kind.go depends on.
	assert.Equal(t,
		map[string][]string{"boost_evaluation": {"BoostEvaluationArgs.EventData"}},
		nonScalarKinds,
		"the inventory of args types with a nested object/array changed; re-check the ByArgs assertion below and the note in unique_opts_by_kind.go")

	// The actual safety property: nested args and ByArgs must not co-occur.
	for kind, fields := range nonScalarKinds {
		opts, known := UniqueOptsForKind(kind)
		require.True(t, known, "kind %q must be registered", kind)
		if opts == nil {
			continue
		}
		assert.False(t, opts.ByArgs,
			"kind %q has nested args (%v) AND ByArgs uniqueness: a JSONB round trip can re-order the nested keys, so its replay would hash to a different unique_key than the original insert and would not deduplicate against it",
			kind, fields)
	}
}

// TestUniqueOptsForKind_UnknownKind: the replay path distinguishes this from a nil-with-known by the
// bool alone, so the bool has to be right.
func TestUniqueOptsForKind_UnknownKind(t *testing.T) {
	opts, known := UniqueOptsForKind("kind_that_does_not_exist")
	assert.False(t, known)
	assert.Nil(t, opts)
}
