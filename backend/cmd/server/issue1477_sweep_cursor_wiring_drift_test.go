package main

// TestIssue1477SweepCursorWiringNoDrift guards the four production injections of the
// shared sweep-cursor repository (Issue #1477).
//
// Why this needs a test at all: every one of the four workers treats SweepCursors as
// OPTIONAL and nil-safe (`if deps.SweepCursors != nil { … }` in scheduled_deletion_check.go,
// retention_cleanup_check.go, item_cleanup_check.go and plaid_orphan_retry.go). Dropping
// one injection therefore compiles, starts, serves traffic, and passes every unit test —
// while silently restoring the pre-#1477 behaviour in which a permanently-failing head of
// the candidate set starves everything behind the per-run cap. main.go:448-455 states that
// hazard in a comment; until this test, a comment was the whole of the enforcement.
//
// Shape follows the repo's existing wiring-drift guards (issue972_wiring_drift_test.go,
// issue1371_wiring_drift_test.go, issue746_production_paddle_wiring_test.go,
// route_registration_drift_test.go): read main.go from disk, assert the production
// composition contains the wiring the type system cannot demand.
//
// It parses rather than substring-scans, deliberately. The four injections are textually
// IDENTICAL lines (`SweepCursors: sweepCursorRepo,`), so a `strings.Contains` scan is
// satisfied by any ONE of them — the vacuous-pass class this branch keeps finding — and
// even a count-of-four is satisfied by deleting one site and adding a fifth somewhere it
// does not belong.
//
// It anchors on the CONSTRUCTOR CALL, not on a free-floating composite literal. An
// adversarial review of this test's first draft (Codex, this branch) demonstrated the
// difference with a working bypass: build the Deps in a variable so the literal satisfies
// a literal-only scan, then neuter it before the call —
//
//	deps := jobs.ScheduledDeletionCheckWorkerDeps{ …, SweepCursors: sweepCursorRepo }
//	deps.SweepCursors = nil                       // ← invisible to a literal-only scan
//	worker = jobs.NewScheduledDeletionCheckWorker(deps)
//
// So the test now requires each constructor's argument to be an INLINE composite literal
// (the shape main.go actually uses), which leaves no window between building the value and
// passing it, and additionally rejects any `X.SweepCursors = …` assignment anywhere in
// main.go.
//
// If a sweep is legitimately retired, delete its entry from requiredSweepWiring below —
// that edit is the deliberate, reviewable act this test exists to force.

import (
	"go/ast"
	"go/parser"
	"go/token"
	"testing"
)

// Each of the four capped sweeps: the constructor main.go must call, the Deps type its
// single argument must be an inline literal of, and why that sweep needs the cursor.
var requiredSweepWiring = []struct {
	ctor     string
	depsType string
	why      string
}{
	{
		"NewScheduledDeletionCheckWorker",
		"ScheduledDeletionCheckWorkerDeps",
		"scheduled_deletion_check: unwired, a user whose hard deletion keeps failing holds the head of the queue and nothing past maxUsersPerDeletionSweep is ever enqueued",
	},
	{
		"NewRetentionCleanupCheckWorker",
		"RetentionCleanupCheckWorkerDeps",
		"retention_cleanup_check (deleteExpiredAccounts): unwired, the sweep restarts from the oldest expired user every run and never reaches past maxUsersPerRetentionSweep",
	},
	{
		"NewItemCleanupCheckWorker",
		"ItemCleanupCheckWorkerDeps",
		"item_cleanup_check: unwired, an Item that cannot be scheduled starves every candidate behind it and Plaid keeps billing for it",
	},
	{
		"NewPlaidOrphanRetryWorker",
		"PlaidOrphanRetryWorkerDeps",
		"plaid_orphan_retry: unwired, an orphan record whose /item/remove keeps failing at Plaid holds the head and nothing past maxOrphanRetriesPerSweep is ever retried",
	},
}

const (
	sweepCursorField   = "SweepCursors"
	sweepCursorVarName = "sweepCursorRepo"
	sweepCursorCtor    = "NewSweepCursorRepository"
	jobsPkgAlias       = "jobs"
)

// callResult is what the AST walk records about one constructor call site.
type callResult struct {
	pos        string
	inlineLit  bool   // argument is an inline composite literal…
	litType    string // …of this type
	hasField   bool
	fieldValue string
}

func TestIssue1477SweepCursorWiringNoDrift(t *testing.T) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "main.go", nil, parser.SkipObjectResolution)
	if err != nil {
		t.Fatalf("could not parse main.go: %v", err)
	}

	calls := map[string][]callResult{}
	totalJobsCtorCalls := 0
	sawCursorConstruction := false
	var fieldAssignments []string

	ast.Inspect(file, func(n ast.Node) bool {
		switch node := n.(type) {
		case *ast.AssignStmt:
			for i, lhs := range node.Lhs {
				// (a) The shared value must be BOUND to sweepCursorRepo by a call to
				// repository.NewSweepCursorRepository. Merely seeing that call somewhere
				// is not enough: `var sweepCursorRepo repository.SweepCursorRepository`
				// (a nil interface) alongside an unrelated call would satisfy a
				// call-anywhere check while all four workers received nil.
				if id, ok := lhs.(*ast.Ident); ok && id.Name == sweepCursorVarName && i < len(node.Rhs) {
					if call, ok := node.Rhs[i].(*ast.CallExpr); ok {
						if sel, ok := call.Fun.(*ast.SelectorExpr); ok && sel.Sel.Name == sweepCursorCtor {
							sawCursorConstruction = true
						}
					}
				}
				// (b) Nothing may assign to a .SweepCursors FIELD. Legitimate wiring
				// happens only inside the composite literals checked below; a
				// post-construction `deps.SweepCursors = nil` is the Codex bypass.
				if sel, ok := lhs.(*ast.SelectorExpr); ok && sel.Sel.Name == sweepCursorField {
					fieldAssignments = append(fieldAssignments, fset.Position(sel.Pos()).String())
				}
			}
		case *ast.CallExpr:
			sel, ok := node.Fun.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			pkg, ok := sel.X.(*ast.Ident)
			if !ok || pkg.Name != jobsPkgAlias {
				return true
			}
			for _, req := range requiredSweepWiring {
				if sel.Sel.Name != req.ctor {
					continue
				}
				totalJobsCtorCalls++
				res := callResult{pos: fset.Position(node.Lparen).String()}
				if len(node.Args) == 1 {
					if lit, ok := node.Args[0].(*ast.CompositeLit); ok {
						res.inlineLit = true
						if lsel, ok := lit.Type.(*ast.SelectorExpr); ok {
							res.litType = lsel.Sel.Name
						}
						for _, elt := range lit.Elts {
							kv, ok := elt.(*ast.KeyValueExpr)
							if !ok {
								continue
							}
							key, ok := kv.Key.(*ast.Ident)
							if !ok || key.Name != sweepCursorField {
								continue
							}
							res.hasField = true
							if id, ok := kv.Value.(*ast.Ident); ok {
								res.fieldValue = id.Name
							} else {
								res.fieldValue = "<non-identifier expression>"
							}
						}
					}
				}
				calls[req.ctor] = append(calls[req.ctor], res)
			}
		}
		return true
	})

	// Fail closed if the scan found nothing to check. An empty scan is not a pass:
	// a rename of the `jobs` import alias, or main.go being moved or split, would
	// otherwise silence this guard entirely while it still reported success.
	if totalJobsCtorCalls == 0 {
		t.Fatalf("found ZERO calls to any of the four jobs.New*Worker constructors in main.go — the "+
			"scan is vacuous, not clean. main.go parsed (%d top-level decls), so the worker "+
			"construction moved or the `jobs` import alias changed; update this test to follow it.",
			len(file.Decls))
	}

	if !sawCursorConstruction {
		t.Errorf("main.go never assigns %s from a call to repository.%s(...) — the value the four "+
			"sites pass is not a real sweep-cursor repository (Issue #1477). A nil interface variable "+
			"of the right name would satisfy every per-site check below while every worker fell back "+
			"to the pre-#1477 starving behaviour.", sweepCursorVarName, sweepCursorCtor)
	}

	for _, at := range fieldAssignments {
		t.Errorf("WIRING DRIFT: main.go assigns to a .%s field at %s.\n"+
			"  The cursor may only be wired inside the constructor's inline Deps literal. A "+
			"post-construction reassignment defeats every literal-level check while the worker "+
			"silently receives nil.", sweepCursorField, at)
	}

	for _, req := range requiredSweepWiring {
		sites := calls[req.ctor]
		if len(sites) == 0 {
			t.Errorf("main.go never calls jobs.%s(...) — this sweep is no longer constructed here, "+
				"so its cursor wiring cannot be checked.\n  %s", req.ctor, req.why)
			continue
		}
		for _, site := range sites {
			switch {
			case !site.inlineLit:
				t.Errorf("WIRING DRIFT: jobs.%s at %s is not called with a single INLINE "+
					"jobs.%s{...} literal.\n  %s\n"+
					"  Building the Deps in a variable first opens a window in which %s can be "+
					"reassigned after the literal has satisfied this test — a demonstrated bypass. "+
					"Keep the literal at the call site.",
					req.ctor, site.pos, req.depsType, req.why, sweepCursorField)
			case site.litType != req.depsType:
				t.Errorf("WIRING DRIFT: jobs.%s at %s is called with a jobs.%s literal, expected jobs.%s.\n  %s",
					req.ctor, site.pos, site.litType, req.depsType, req.why)
			case !site.hasField:
				t.Errorf("WIRING DRIFT: jobs.%s at %s does not set %s.\n  %s\n"+
					"  The worker nil-guards this field, so the omission is invisible at compile time, "+
					"at startup, and to every unit test.",
					req.ctor, site.pos, sweepCursorField, req.why)
			case site.fieldValue == "nil":
				t.Errorf("WIRING DRIFT: jobs.%s at %s sets %s to a literal nil.\n  %s",
					req.ctor, site.pos, sweepCursorField, req.why)
			case site.fieldValue != sweepCursorVarName:
				t.Errorf("WIRING DRIFT: jobs.%s at %s sets %s to %q, not the shared %q built by "+
					"repository.%s.\n  %s\n  All four sweeps must share ONE repository value; a "+
					"per-sweep substitute is a change that needs review, not a silent edit.",
					req.ctor, site.pos, sweepCursorField, site.fieldValue, sweepCursorVarName,
					sweepCursorCtor, req.why)
			}
		}
	}
}
