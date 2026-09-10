package main

// TestIssue1477SweepCursorWiringNoDrift guards the FIVE production injections of the
// shared sweep-cursor repository (Issue #1477).
//
// Why this needs a test at all: every one of the five workers treats SweepCursors as
// OPTIONAL and nil-safe (`if deps.SweepCursors != nil { … }` in scheduled_deletion_check.go,
// retention_cleanup_check.go, item_cleanup_check.go, plaid_orphan_retry.go and
// balance_snapshot_capture.go). Dropping
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
// It parses rather than substring-scans, deliberately. The five injections are textually
// IDENTICAL lines (`SweepCursors: sweepCursorRepo,`), so a `strings.Contains` scan is
// satisfied by any ONE of them — the vacuous-pass class this branch keeps finding — and
// even a count-of-N is satisfied by deleting one site and adding another somewhere it
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
// It also checks the SWEEP-CURSOR CONSTRUCTOR'S OWN ARGUMENT, and every binding of the
// name `db` that feeds it. Checking only that `sweepCursorRepo` is bound to
// `repository.NewSweepCursorRepository(...)` leaves a hole one level INSIDE the call:
// `NewSweepCursorRepository(nil)` builds a real repository around a nil DB, so every
// Load and Save fails, and `sweepResume` treats a Load failure as NON-FATAL and starts the
// run FROM THE BEGINNING (see resumeSweep in queue/jobs/sweep_resume.go) — which is exactly
// the silent starvation regime this guard exists to prevent, reached without touching any
// of the five field assignments. Four bypasses were executed against the version of this
// test that lacked the check, all four PASSING it:
//
//	repository.NewSweepCursorRepository(nil)                       // literal nil
//	var cursorDB repository.DB; …; NewSweepCursorRepository(cursorDB) // nil-checked variable
//	repository.NewSweepCursorRepository(sweepCursorDB(db))         // helper returning the DB
//	db = nil; repository.NewSweepCursorRepository(db)              // the pool itself neutered
//
// The last is the worst of those four: `db` is a *pgxpool.Pool, so a nil one stored in the
// repository's DB interface field is a NON-nil interface — the workers' `!= nil` guards all
// pass and the calls panic instead of degrading.
//
// The check is fail-closed in the same shape as the rest of this test: the argument must be
// the bare identifier `db`, and every binding of `db` in main.go must be one of the two
// shapes that exist today (`var db *pgxpool.Pool` and `db, err = database.NewPool(...)`).
// Anything else is reported rather than assumed benign.
//
// TWO MORE HOLES, ONE LEVEL OVER FROM THE `= nil` ONE, CLOSED BY THE SAME FAIL-CLOSED SHAPE.
// Both were found by the #1432 round-13 Codex review and both were EXECUTED against the
// version of this test that lacked them — `go build` clean, `go vet` clean, this test PASSING:
//
//  1. INDIRECT ASSIGNMENT THROUGH A POINTER. Every check above reads the LHS of an
//     AssignStmt as an *ast.Ident or an *ast.SelectorExpr. A *ast.StarExpr is neither:
//
//     func(p *repository.SweepCursorRepository) { *p = nil }(&sweepCursorRepo)
//
//     leaves sawCursorConstruction true, produces no Ident and no Selector on any LHS, and
//     nils the shared interface for all five sites. The guard cannot follow the pointer, so
//     it forbids the step that CREATES one: taking the address of `sweepCursorRepo` or of
//     `db` anywhere in main.go is drift. `&` is the only way to obtain such a pointer (even
//     reflect and unsafe need one), so this is a genuine choke point rather than a filter.
//     main.go takes neither address today, verified.
//
//  2. A PARAMETER OF THAT NAME. Parameters are *ast.Field, so they are not a ValueSpec, not
//     an AssignStmt and not a RangeStmt — the three binding forms the walk knew about:
//
//     func(sweepCursorRepo repository.SweepCursorRepository) {
//     worker = jobs.NewScheduledDeletionCheckWorker(jobs.…Deps{ …, SweepCursors: sweepCursorRepo })
//     }(nil)
//
//     satisfies the inline-literal check and the by-name field check while the worker
//     receives nil. The same hole existed for `db`. Both names are now rejected as
//     parameters, results and receivers anywhere in main.go.
//
//     ⚠ KNOWN OVER-REACH, recorded rather than trimmed. The scan covers EVERY FuncDecl in
//     main.go, so a future top-level helper — `func checkHealth(db *pgxpool.Pool)` — would
//     be reported even though it is outside main() and cannot shadow main's local. The
//     round-13 Codex re-review raised this and judged it nuisance-level: renaming the
//     parameter to `pool` satisfies the guard in one edit. Narrowing the rule to "inside
//     main() only" would mean deciding which function is main and how the five call sites
//     are reached from it, which is the sort of clever filter this guard keeps finding
//     holes in. The fail-closed direction costs one rename; the other direction costs a
//     silent bypass.
//
// WHY THERE IS A SECOND TEST IN THIS FILE (#1432 round 14, findings CB-1 and CB-2).
// Every bypass listed above was demonstrated by HAND, against a throwaway copy of main.go,
// and then closed. Nothing re-ran them. That left the classifier in the position this whole
// branch keeps objecting to: its discrimination was provable by READING and by nothing else,
// so a future edit that made `classifyCursorBinding` or the walk accept one of these shapes
// again would be caught by no test — the real-main.go assertion only fails if PRODUCTION
// drifts, never if the GUARD does. `bindingBadCtorArg` in particular had five occurrences,
// all of them in the classifier itself and none in any assertion.
//
// TestIssue1477SweepCursorClassifierFixtures closes that by driving the SAME walk over
// synthetic in-test sources, one per bypass shape, each asserting the specific rejection
// reason rather than merely "an error occurred", plus a POSITIVE CONTROL whose correct
// wiring must produce no findings at all. Without the positive control every negative case
// would still pass if the classifier simply rejected everything.
//
// The two tests are complementary and neither replaces the other: the real-main.go
// assertion catches drift in PRODUCTION WIRING, the fixtures catch drift in the GUARD.
//
// NOT covered, stated so it is not mistaken for covered: whether the five constructed
// workers are actually REGISTERED with River. That is a materially larger surface (the
// river.Workers registry, its AddWorker calls and the queue config) and belongs in its own
// guard, not bolted onto this one. The #1432 round-13 review sharpened this residual with
// the exact sites it leaves open — deleting any of main.go's `river.AddWorker` calls for
// scheduledDeletionCheckWorker, retentionCleanupCheckWorker, itemCleanupCheckWorker,
// plaidOrphanRetryWorker or balanceSnapshotDispatchWorker leaves every assertion in this
// file green.
//
// If a sweep is legitimately retired, delete its entry from requiredSweepWiring below —
// that edit is the deliberate, reviewable act this test exists to force.

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"sort"
	"strings"
	"testing"
)

// Each of the five capped sweeps: the constructor main.go must call, the Deps type its
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
	{
		"NewBalanceSnapshotDispatchWorker",
		"BalanceSnapshotDispatchWorkerDeps",
		"balance_snapshot_dispatch (#1626): unwired, every run re-enumerates from the oldest active linked item, so above balanceCaptureItemsPerRun the same prefix is captured every day and every newer item accrues no balance snapshot on ANY day. Unlike the four above, this one needs NO per-row failure to starve — its candidate set is every active item and never shrinks, so the healthy path starves it",
	},
}

const (
	sweepCursorField   = "SweepCursors"
	sweepCursorVarName = "sweepCursorRepo"
	sweepCursorCtor    = "NewSweepCursorRepository"
	sweepCursorPkg     = "repository"
	jobsPkgAlias       = "jobs"

	// dbVarName is the pool variable the sweep-cursor repository must be built around, and
	// dbPoolPkg/dbPoolCtor/dbPoolType are the only two shapes allowed to bind that name.
	dbVarName  = "db"
	dbPoolPkg  = "database"
	dbPoolCtor = "NewPool"
	dbPoolType = "Pool"
)

// cursorBinding classifies one binding of `sweepCursorRepo`.
type cursorBinding int

const (
	// bindingOK: bound by repository.NewSweepCursorRepository(db) — the only correct shape.
	bindingOK cursorBinding = iota
	// bindingNotTheCtor: bound to something that is not the constructor at all.
	bindingNotTheCtor
	// bindingBadCtorArg: the constructor, called around something other than `db`.
	bindingBadCtorArg
)

// classifyCursorBinding reports how expr binds `sweepCursorRepo`.
//
// The package qualifier is checked: without it, a locally-defined
// `x.NewSweepCursorRepository(db)` returning anything at all would satisfy the binding
// check. The ARGUMENT is checked for the reason given in this file's header — a real
// repository around a nil or substituted DB fails every Load, and a failed Load is
// non-fatal and restarts the sweep from the beginning.
func classifyCursorBinding(expr ast.Expr) (cursorBinding, string) {
	call, ok := expr.(*ast.CallExpr)
	if !ok {
		return bindingNotTheCtor, ""
	}
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != sweepCursorCtor {
		return bindingNotTheCtor, ""
	}
	if pkg, ok := sel.X.(*ast.Ident); !ok || pkg.Name != sweepCursorPkg {
		return bindingNotTheCtor, ""
	}
	if len(call.Args) != 1 {
		return bindingBadCtorArg, fmt.Sprintf("%d arguments", len(call.Args))
	}
	id, ok := call.Args[0].(*ast.Ident)
	if !ok {
		// A call, a conversion, a selector — anything the guard cannot read as "the pool".
		return bindingBadCtorArg, "a non-identifier expression"
	}
	if id.Name != dbVarName {
		return bindingBadCtorArg, fmt.Sprintf("%q", id.Name)
	}
	return bindingOK, ""
}

// dbBindingIsRecognised reports whether one binding of the name `db` is one of the two
// shapes main.go uses today: the `var db *pgxpool.Pool` declaration, or the assignment
// whose right-hand side is `database.NewPool(...)`. decl distinguishes the two callers;
// value is nil when the binding's per-name value cannot be read (a tuple assignment).
func dbBindingIsRecognised(decl bool, declType ast.Expr, value ast.Expr, rhs []ast.Expr) bool {
	if decl {
		// `var db *pgxpool.Pool` — a pointer to a selector-qualified type, no value.
		if value != nil {
			return false
		}
		star, ok := declType.(*ast.StarExpr)
		if !ok {
			return false
		}
		sel, ok := star.X.(*ast.SelectorExpr)
		return ok && sel.Sel.Name == dbPoolType
	}
	// `db, err = database.NewPool(ctx, cfg)` — one call on the right of a tuple assign.
	if len(rhs) != 1 {
		return false
	}
	call, ok := rhs[0].(*ast.CallExpr)
	if !ok {
		return false
	}
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != dbPoolCtor {
		return false
	}
	pkg, ok := sel.X.(*ast.Ident)
	return ok && pkg.Name == dbPoolPkg
}

// callResult is what the AST walk records about one constructor call site.
type callResult struct {
	pos        string
	inlineLit  bool   // argument is an inline composite literal…
	litType    string // …of this type
	hasField   bool
	fieldValue string
}

// Stable identifiers for every rejection this guard can produce. They exist so the fixture
// test can assert the SPECIFIC reason a synthetic source was rejected — "an error occurred"
// would be satisfied by a classifier that rejected everything, including correct wiring.
const (
	findingVacuousScan      = "vacuous-scan"
	findingNoConstruction   = "no-construction"
	findingCursorRebound    = "cursor-rebound"
	findingBadCtorArg       = "bad-ctor-arg"
	findingDBRebound        = "db-rebound"
	findingAddressTaken     = "address-taken"
	findingShadowingParam   = "shadowing-param"
	findingFieldAssignment  = "field-assignment"
	findingCtorMissing      = "ctor-missing"
	findingNotInlineLiteral = "not-inline-literal"
	findingWrongLiteralType = "wrong-literal-type"
	findingMissingField     = "missing-field"
	findingNilField         = "nil-field"
	findingWrongFieldValue  = "wrong-field-value"
)

// cursorWiringFinding is one rejection: a stable kind for assertions, and the full operator
// -facing message the real-main.go test reports verbatim.
type cursorWiringFinding struct {
	kind    string
	message string
}

// scanSweepCursorWiring runs the whole guard over one parsed file and returns every
// rejection it found, in a deterministic order. Splitting it out from the test body is what
// lets the fixture test drive the SAME walk over synthetic sources instead of duplicating
// it — a duplicated walk would drift from this one and prove nothing about it.
//
// A findingVacuousScan result is fatal to the caller: an empty scan is not a pass.
func scanSweepCursorWiring(fset *token.FileSet, file *ast.File) []cursorWiringFinding {
	calls := map[string][]callResult{}
	totalJobsCtorCalls := 0
	sawCursorConstruction := false
	var fieldAssignments []string
	var varRebinds []string
	var ctorArgFindings []string
	var dbRebinds []string
	var addressTaken []string
	var shadowingParams []string

	// noteCursorBinding records one place where the name `sweepCursorRepo` is bound to a
	// value. EVERY such binding must be the constructor call AROUND `db`; anything else —
	// `= nil`, a substitute repository, a shadowing `var` declaration, a tuple assignment
	// whose per-name value cannot be read, or the real constructor around a substituted or
	// nil DB — is drift, because after it the workers below receive a value this test
	// never checked. This is the same fail-closed shape as fieldAssignments: the guard
	// enumerates the bindings it can prove correct and treats everything else as a finding.
	noteCursorBinding := func(pos token.Pos, value ast.Expr) {
		if value == nil {
			varRebinds = append(varRebinds, fset.Position(pos).String())
			return
		}
		switch verdict, detail := classifyCursorBinding(value); verdict {
		case bindingOK:
			sawCursorConstruction = true
		case bindingBadCtorArg:
			ctorArgFindings = append(ctorArgFindings,
				fmt.Sprintf("%s (built around %s)", fset.Position(pos).String(), detail))
		default:
			varRebinds = append(varRebinds, fset.Position(pos).String())
		}
	}

	// noteDBBinding records one place where the name `db` is bound. Only the two shapes
	// main.go uses today are accepted; anything else could put a nil or substituted pool
	// under an argument that still reads `db` at the constructor call.
	noteDBBinding := func(pos token.Pos, decl bool, declType, value ast.Expr, rhs []ast.Expr) {
		if dbBindingIsRecognised(decl, declType, value, rhs) {
			return
		}
		dbRebinds = append(dbRebinds, fset.Position(pos).String())
	}

	// noteFieldNames records any parameter, result or receiver called `sweepCursorRepo` or
	// `db`. Such a name is a binding the three statement-level cases below cannot see (a
	// parameter is an *ast.Field), and inside its function body it shadows the real variable
	// while every by-name check in this test still reads as correct.
	noteFieldNames := func(fl *ast.FieldList, what string) {
		if fl == nil {
			return
		}
		for _, f := range fl.List {
			for _, name := range f.Names {
				if name.Name == sweepCursorVarName || name.Name == dbVarName {
					shadowingParams = append(shadowingParams,
						fmt.Sprintf("%s (%s %q)", fset.Position(name.Pos()).String(), what, name.Name))
				}
			}
		}
	}

	ast.Inspect(file, func(n ast.Node) bool {
		switch node := n.(type) {
		case *ast.FuncDecl:
			noteFieldNames(node.Recv, "receiver")
			if node.Type != nil {
				noteFieldNames(node.Type.Params, "parameter")
				noteFieldNames(node.Type.Results, "result")
			}
		case *ast.FuncLit:
			if node.Type != nil {
				noteFieldNames(node.Type.Params, "parameter")
				noteFieldNames(node.Type.Results, "result")
			}
		case *ast.UnaryExpr:
			// `&sweepCursorRepo` / `&db`. The guard cannot follow a pointer, so it forbids
			// the only step that can produce one — see hole 1 in this file's header.
			if node.Op != token.AND {
				return true
			}
			if id, ok := node.X.(*ast.Ident); ok &&
				(id.Name == sweepCursorVarName || id.Name == dbVarName) {
				addressTaken = append(addressTaken,
					fmt.Sprintf("%s (&%s)", fset.Position(node.Pos()).String(), id.Name))
			}
		case *ast.RangeStmt:
			// `for db := range …` / `for sweepCursorRepo := range …` shadow the real
			// binding without ever being an AssignStmt or a ValueSpec.
			for _, e := range []ast.Expr{node.Key, node.Value} {
				id, ok := e.(*ast.Ident)
				if !ok {
					continue
				}
				switch id.Name {
				case sweepCursorVarName:
					varRebinds = append(varRebinds, fset.Position(id.Pos()).String())
				case dbVarName:
					dbRebinds = append(dbRebinds, fset.Position(id.Pos()).String())
				}
			}
		case *ast.ValueSpec:
			// `var sweepCursorRepo repository.SweepCursorRepository` — a nil interface,
			// and not an AssignStmt. Declared inside any nested scope it shadows the real
			// binding for every call site after it.
			for i, name := range node.Names {
				var value ast.Expr
				if len(node.Values) == len(node.Names) {
					value = node.Values[i]
				}
				switch name.Name {
				case sweepCursorVarName:
					noteCursorBinding(name.Pos(), value)
				case dbVarName:
					noteDBBinding(name.Pos(), true, node.Type, value, node.Values)
				}
			}
		case *ast.AssignStmt:
			for i, lhs := range node.Lhs {
				// (a) The shared value must be BOUND to sweepCursorRepo by a call to
				// repository.NewSweepCursorRepository — at EVERY binding of that name,
				// not merely at one of them. Checking that the constructor was called
				// somewhere leaves `sweepCursorRepo = nil` one line later completely
				// invisible: the constructor returns an INTERFACE, so the reassigned
				// variable is a genuinely nil interface, every worker's
				// `if deps.SweepCursors != nil` skips, and all five sweeps revert to the
				// pre-#1477 starving behaviour while this test still passes.
				if id, ok := lhs.(*ast.Ident); ok {
					var value ast.Expr
					if len(node.Lhs) == len(node.Rhs) {
						value = node.Rhs[i]
					}
					switch id.Name {
					case sweepCursorVarName:
						noteCursorBinding(id.Pos(), value)
					case dbVarName:
						// (a2) …and so must the POOL that constructor is called around.
						// `db = nil` one line above the call leaves the argument reading
						// `db` while the repository wraps a typed-nil pool — a NON-nil
						// interface, so the workers' nil guards pass and the sweeps panic
						// rather than degrade.
						noteDBBinding(id.Pos(), false, nil, value, node.Rhs)
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

	var findings []cursorWiringFinding
	add := func(kind, format string, args ...any) {
		findings = append(findings, cursorWiringFinding{kind: kind, message: fmt.Sprintf(format, args...)})
	}

	// Fail closed if the scan found nothing to check. An empty scan is not a pass:
	// a rename of the `jobs` import alias, or main.go being moved or split, would
	// otherwise silence this guard entirely while it still reported success.
	if totalJobsCtorCalls == 0 {
		add(findingVacuousScan,
			"found ZERO calls to any of the five jobs.New*Worker constructors in main.go — the "+
				"scan is vacuous, not clean. main.go parsed (%d top-level decls), so the worker "+
				"construction moved or the `jobs` import alias changed; update this test to follow it.",
			len(file.Decls))
		return findings
	}

	if !sawCursorConstruction {
		add(findingNoConstruction,
			"main.go never binds %s to a call to %s.%s(...) — the value the five sites pass is "+
				"not a real sweep-cursor repository (Issue #1477), so every worker falls back to the "+
				"pre-#1477 starving behaviour.", sweepCursorVarName, sweepCursorPkg, sweepCursorCtor)
	}

	for _, at := range varRebinds {
		add(findingCursorRebound,
			"WIRING DRIFT: main.go binds %s at %s to something other than %s.%s(...).\n"+
				"  The constructor returns an INTERFACE, so `%s = nil` yields a genuinely nil interface: "+
				"every worker's `if deps.SweepCursors != nil` skips, all five sweeps silently revert to "+
				"the pre-#1477 starving behaviour, and the five per-site literal checks below still pass "+
				"because they only look at the NAME. The name must be bound exactly once, by the "+
				"constructor, and never rebound.", sweepCursorVarName, at, sweepCursorPkg, sweepCursorCtor,
			sweepCursorVarName)
	}

	for _, at := range ctorArgFindings {
		add(findingBadCtorArg,
			"WIRING DRIFT: main.go binds %s at %s to repository.%s(...) called around something "+
				"other than the shared %q pool.\n"+
				"  The constructor is not a validator: it wraps whatever it is given. A nil or substituted "+
				"DB makes every Load and Save fail, and resumeSweep (queue/jobs/sweep_resume.go) treats a "+
				"failed Load as NON-FATAL and starts the run FROM THE BEGINNING — the pre-#1477 starving "+
				"behaviour, reached without touching any of the five %s field assignments this test checks "+
				"below. If the pool legitimately moved or was renamed, update %s in this test; that edit is "+
				"the reviewable act the guard exists to force.",
			sweepCursorVarName, at, sweepCursorCtor, dbVarName, sweepCursorField, "dbVarName")
	}

	for _, at := range dbRebinds {
		add(findingDBRebound,
			"WIRING DRIFT: main.go binds %q at %s in a shape this guard does not recognise.\n"+
				"  %s is checked to be built around %q by name, so a rebinding or shadowing of %q defeats "+
				"that check while the argument still reads correctly. Only two shapes are accepted: the "+
				"`var %s *pgxpool.Pool` declaration and the `%s, err = %s.%s(...)` assignment. A "+
				"*pgxpool.Pool set to nil is worse than a nil interface: stored in the repository's DB "+
				"field it is a NON-nil interface, so every worker's `!= nil` guard passes and the sweeps "+
				"panic instead of degrading.",
			dbVarName, at, sweepCursorCtor, dbVarName, dbVarName, dbVarName, dbVarName, dbPoolPkg, dbPoolCtor)
	}

	for _, at := range addressTaken {
		add(findingAddressTaken,
			"WIRING DRIFT: main.go takes the ADDRESS of a guarded variable at %s.\n"+
				"  Every other check in this guard reads the left-hand side of an assignment as an "+
				"identifier or a selector. `*p = nil` through a pointer is neither, so it nils the shared "+
				"value for all five sweeps with nothing to see: `go build`, `go vet` and this test were all "+
				"clean against the demonstrated bypass\n"+
				"    func(p *repository.SweepCursorRepository) { *p = nil }(&%s)\n"+
				"  Since the guard cannot follow a pointer, it forbids creating one. If main.go legitimately "+
				"needs the address of %q or %q, that is the reviewable act this guard exists to force.",
			at, sweepCursorVarName, sweepCursorVarName, dbVarName)
	}

	for _, at := range shadowingParams {
		add(findingShadowingParam,
			"WIRING DRIFT: main.go binds a guarded name as a function parameter/result/receiver at %s.\n"+
				"  A parameter is an *ast.Field, so it is invisible to the ValueSpec, AssignStmt and "+
				"RangeStmt cases that catch every other shadowing form — while the inline-literal check and "+
				"the by-name %s check below both still read as correct. Demonstrated bypass:\n"+
				"    func(%s repository.SweepCursorRepository) { … SweepCursors: %s … }(nil)\n"+
				"  %q and %q must name the shared values built at the top of main.go and nothing else.",
			at, sweepCursorField, sweepCursorVarName, sweepCursorVarName, sweepCursorVarName, dbVarName)
	}

	for _, at := range fieldAssignments {
		add(findingFieldAssignment,
			"WIRING DRIFT: main.go assigns to a .%s field at %s.\n"+
				"  The cursor may only be wired inside the constructor's inline Deps literal. A "+
				"post-construction reassignment defeats every literal-level check while the worker "+
				"silently receives nil.", sweepCursorField, at)
	}

	for _, req := range requiredSweepWiring {
		sites := calls[req.ctor]
		if len(sites) == 0 {
			add(findingCtorMissing,
				"main.go never calls jobs.%s(...) — this sweep is no longer constructed here, "+
					"so its cursor wiring cannot be checked.\n  %s", req.ctor, req.why)
			continue
		}
		for _, site := range sites {
			switch {
			case !site.inlineLit:
				add(findingNotInlineLiteral,
					"WIRING DRIFT: jobs.%s at %s is not called with a single INLINE "+
						"jobs.%s{...} literal.\n  %s\n"+
						"  Building the Deps in a variable first opens a window in which %s can be "+
						"reassigned after the literal has satisfied this test — a demonstrated bypass. "+
						"Keep the literal at the call site.",
					req.ctor, site.pos, req.depsType, req.why, sweepCursorField)
			case site.litType != req.depsType:
				add(findingWrongLiteralType,
					"WIRING DRIFT: jobs.%s at %s is called with a jobs.%s literal, expected jobs.%s.\n  %s",
					req.ctor, site.pos, site.litType, req.depsType, req.why)
			case !site.hasField:
				add(findingMissingField,
					"WIRING DRIFT: jobs.%s at %s does not set %s.\n  %s\n"+
						"  The worker nil-guards this field, so the omission is invisible at compile time, "+
						"at startup, and to every unit test.",
					req.ctor, site.pos, sweepCursorField, req.why)
			case site.fieldValue == "nil":
				add(findingNilField,
					"WIRING DRIFT: jobs.%s at %s sets %s to a literal nil.\n  %s",
					req.ctor, site.pos, sweepCursorField, req.why)
			case site.fieldValue != sweepCursorVarName:
				add(findingWrongFieldValue,
					"WIRING DRIFT: jobs.%s at %s sets %s to %q, not the shared %q built by "+
						"repository.%s.\n  %s\n  All four sweeps must share ONE repository value; a "+
						"per-sweep substitute is a change that needs review, not a silent edit.",
					req.ctor, site.pos, sweepCursorField, site.fieldValue, sweepCursorVarName,
					sweepCursorCtor, req.why)
			}
		}
	}

	return findings
}

// TestIssue1477SweepCursorWiringNoDrift is the PRODUCTION half of this guard: it runs the
// scan over the real main.go read from disk. It is what catches drift in the wiring itself.
func TestIssue1477SweepCursorWiringNoDrift(t *testing.T) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "main.go", nil, parser.SkipObjectResolution)
	if err != nil {
		t.Fatalf("could not parse main.go: %v", err)
	}

	for _, f := range scanSweepCursorWiring(fset, file) {
		if f.kind == findingVacuousScan {
			t.Fatal(f.message)
		}
		t.Error(f.message)
	}
}

// --- Synthetic-source fixtures (#1432 round 14, CB-1 / CB-2) ---------------------------
//
// Everything below drives the SAME scan over in-test sources. See the header for why: the
// real-main.go assertion cannot fail when the GUARD regresses, only when production does.

// cursorFixtureSource wraps one snippet in the smallest main.go-shaped program the scan can
// read: the two recognised `db` bindings, then the snippet (which must bind
// sweepCursorRepo), then the five worker constructions wired correctly. Only the snippet
// varies between fixtures, so any difference in the findings is attributable to it.
//
// The sources are PARSED, never compiled — go/parser does no name resolution, so undeclared
// identifiers like `ctx` and `pgxpool` are fine and no import block is needed.
func cursorFixtureSource(snippet string) string {
	return `package main

func main() {
	var db *pgxpool.Pool
	db, err = database.NewPool(ctx, cfg)
	_ = err
` + snippet + `
	w1 = jobs.NewScheduledDeletionCheckWorker(jobs.ScheduledDeletionCheckWorkerDeps{SweepCursors: sweepCursorRepo})
	w2 = jobs.NewRetentionCleanupCheckWorker(jobs.RetentionCleanupCheckWorkerDeps{SweepCursors: sweepCursorRepo})
	w3 = jobs.NewItemCleanupCheckWorker(jobs.ItemCleanupCheckWorkerDeps{SweepCursors: sweepCursorRepo})
	w4 = jobs.NewPlaidOrphanRetryWorker(jobs.PlaidOrphanRetryWorkerDeps{SweepCursors: sweepCursorRepo})
	w5 = jobs.NewBalanceSnapshotDispatchWorker(jobs.BalanceSnapshotDispatchWorkerDeps{SweepCursors: sweepCursorRepo})
}
`
}

// correctCursorBinding is the one shape the guard accepts. Every negative fixture below
// starts from it and changes exactly one thing.
const correctCursorBinding = "\tsweepCursorRepo = repository.NewSweepCursorRepository(db)"

func TestIssue1477SweepCursorClassifierFixtures(t *testing.T) {
	tests := []struct {
		name string
		// why records which round-12/13 bypass this fixture pins down.
		why string
		// snippet replaces the cursor binding (and any drift) in the fixture program.
		snippet string
		// wantKinds is the EXACT set of rejection kinds the scan must produce — not "at
		// least one error". An exact set is what makes the positive control meaningful and
		// what makes a mutation redden exactly one fixture.
		wantKinds []string
		// wantDetail, when set, must appear in the concatenated messages: it pins the
		// rejection to the specific offending construct rather than the category.
		wantDetail string
	}{
		{
			name:      "PositiveControl_CorrectWiringIsAccepted",
			why:       "MANDATORY control: without it every case below passes if the scan rejects everything",
			snippet:   correctCursorBinding,
			wantKinds: nil,
		},
		{
			name:      "CB1_PlainReassignmentToNil",
			why:       "CB-1: `sweepCursorRepo = nil` — rated Irreversible, proven by hand in round 12 and never committed until now",
			snippet:   correctCursorBinding + "\n\tsweepCursorRepo = nil",
			wantKinds: []string{findingCursorRebound},
		},
		{
			name:       "CB2_ConstructorAroundLiteralNil",
			why:        "CB-2: `NewSweepCursorRepository(nil)` builds a real repository around a nil DB; bindingBadCtorArg had zero assertions before this fixture",
			snippet:    "\tsweepCursorRepo = repository.NewSweepCursorRepository(nil)",
			wantKinds:  []string{findingNoConstruction, findingBadCtorArg},
			wantDetail: `built around "nil"`,
		},
		{
			name:      "VarSpecShadowing",
			why:       "a `var` declaration is a ValueSpec, not an AssignStmt: it shadows with a nil interface",
			snippet:   correctCursorBinding + "\n\tif cond {\n\t\tvar sweepCursorRepo repository.SweepCursorRepository\n\t\t_ = sweepCursorRepo\n\t}",
			wantKinds: []string{findingCursorRebound},
		},
		{
			name:      "RangeStmtShadowingDB",
			why:       "`for db := range …` binds the pool name without ever being an AssignStmt or a ValueSpec",
			snippet:   correctCursorBinding + "\n\tfor db := range pools {\n\t\t_ = db\n\t}",
			wantKinds: []string{findingDBRebound},
		},
		{
			name:      "PointerIndirectionViaAddressOf",
			why:       "`*p = nil` has a StarExpr LHS, invisible to every Ident/Selector check; the guard forbids taking the address instead",
			snippet:   correctCursorBinding + "\n\tfunc(p *repository.SweepCursorRepository) { *p = nil }(&sweepCursorRepo)",
			wantKinds: []string{findingAddressTaken},
		},
		{
			name:      "ParameterNamedSweepCursorRepo",
			why:       "a parameter is an *ast.Field: it shadows the real value while every by-name check still reads correct",
			snippet:   correctCursorBinding + "\n\tfunc(sweepCursorRepo repository.SweepCursorRepository) { _ = sweepCursorRepo }(nil)",
			wantKinds: []string{findingShadowingParam},
		},
		{
			name:      "ParameterNamedDB",
			why:       "the same hole existed for the pool name",
			snippet:   correctCursorBinding + "\n\tfunc(db *pgxpool.Pool) { _ = db }(nil)",
			wantKinds: []string{findingShadowingParam},
		},
		{
			name:      "PoolNeuteredBeforeConstructor",
			why:       "`db = nil` leaves the constructor argument reading `db` while it wraps a typed-nil pool — a NON-nil interface, so the workers panic instead of degrading",
			snippet:   "\tdb = nil\n" + correctCursorBinding,
			wantKinds: []string{findingDBRebound},
		},
		{
			name:      "WrongPackageConstructor",
			why:       "a locally-defined constructor of the right name returning anything at all would otherwise satisfy the binding check",
			snippet:   "\tsweepCursorRepo = fakerepo.NewSweepCursorRepository(db)",
			wantKinds: []string{findingNoConstruction, findingCursorRebound},
		},
		{
			name:       "PostConstructionFieldAssignment",
			why:        "the original Codex bypass: build the Deps in a variable, neuter the field, then call the constructor",
			snippet:    correctCursorBinding + "\n\tdeps.SweepCursors = nil",
			wantKinds:  []string{findingFieldAssignment},
			wantDetail: "assigns to a .SweepCursors field",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			fset := token.NewFileSet()
			src := cursorFixtureSource(tc.snippet)
			file, err := parser.ParseFile(fset, "fixture.go", src, parser.SkipObjectResolution)
			if err != nil {
				t.Fatalf("fixture source does not parse (%v); the fixture is broken, not the guard:\n%s", err, src)
			}

			findings := scanSweepCursorWiring(fset, file)
			var gotKinds []string
			var messages []string
			for _, f := range findings {
				gotKinds = append(gotKinds, f.kind)
				messages = append(messages, f.message)
			}
			sort.Strings(gotKinds)
			want := append([]string(nil), tc.wantKinds...)
			sort.Strings(want)

			if strings.Join(gotKinds, ",") != strings.Join(want, ",") {
				t.Fatalf("scan produced the wrong rejection reasons.\n  why this fixture exists: %s\n"+
					"  want kinds: %v\n  got kinds:  %v\n  messages:\n%s\n  source:\n%s",
					tc.why, want, gotKinds, strings.Join(messages, "\n---\n"), src)
			}
			if tc.wantDetail != "" && !strings.Contains(strings.Join(messages, "\n"), tc.wantDetail) {
				t.Errorf("rejection reason is the right CATEGORY but does not name the offending construct.\n"+
					"  want message containing: %q\n  messages:\n%s", tc.wantDetail, strings.Join(messages, "\n---\n"))
			}
		})
	}
}
