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
// It also checks the SWEEP-CURSOR CONSTRUCTOR'S OWN ARGUMENT, and every binding of the
// name `db` that feeds it. Checking only that `sweepCursorRepo` is bound to
// `repository.NewSweepCursorRepository(...)` leaves a hole one level INSIDE the call:
// `NewSweepCursorRepository(nil)` builds a real repository around a nil DB, so every
// Load and Save fails, and `sweepResume` treats a Load failure as NON-FATAL and starts the
// run FROM THE BEGINNING (see resumeSweep in queue/jobs/sweep_resume.go) — which is exactly
// the silent starvation regime this guard exists to prevent, reached without touching any
// of the four field assignments. Four bypasses were executed against the version of this
// test that lacked the check, all four PASSING it:
//
//	repository.NewSweepCursorRepository(nil)                       // literal nil
//	var cursorDB repository.DB; …; NewSweepCursorRepository(cursorDB) // nil-checked variable
//	repository.NewSweepCursorRepository(sweepCursorDB(db))         // helper returning the DB
//	db = nil; repository.NewSweepCursorRepository(db)              // the pool itself neutered
//
// The last is the worst of the four: `db` is a *pgxpool.Pool, so a nil one stored in the
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
//     nils the shared interface for all four sites. The guard cannot follow the pointer, so
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
//     main() only" would mean deciding which function is main and how the four call sites
//     are reached from it, which is the sort of clever filter this guard keeps finding
//     holes in. The fail-closed direction costs one rename; the other direction costs a
//     silent bypass.
//
// NOT covered, stated so it is not mistaken for covered: whether the four constructed
// workers are actually REGISTERED with River. That is a materially larger surface (the
// river.Workers registry, its AddWorker calls and the queue config) and belongs in its own
// guard, not bolted onto this one. The #1432 round-13 review sharpened this residual with
// the exact sites it leaves open — deleting any of main.go's four `river.AddWorker` calls
// for scheduledDeletionCheckWorker, retentionCleanupCheckWorker, itemCleanupCheckWorker or
// plaidOrphanRetryWorker leaves every assertion in this file green.
//
// If a sweep is legitimately retired, delete its entry from requiredSweepWiring below —
// that edit is the deliberate, reviewable act this test exists to force.

import (
	"fmt"
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
	var varRebinds []string
	var ctorArgFindings []string
	var dbRebinds []string
	var addressTaken []string
	var shadowingParams []string

	// noteCursorBinding records one place where the name `sweepCursorRepo` is bound to a
	// value. EVERY such binding must be the constructor call AROUND `db`; anything else —
	// `= nil`, a substitute repository, a shadowing `var` declaration, a tuple assignment
	// whose per-name value cannot be read, or the real constructor around a substituted or
	// nil DB — is drift, because after it the four workers below receive a value this test
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
				// `if deps.SweepCursors != nil` skips, and all four sweeps revert to the
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
		t.Errorf("main.go never binds %s to a call to %s.%s(...) — the value the four sites pass is "+
			"not a real sweep-cursor repository (Issue #1477), so every worker falls back to the "+
			"pre-#1477 starving behaviour.", sweepCursorVarName, sweepCursorPkg, sweepCursorCtor)
	}

	for _, at := range varRebinds {
		t.Errorf("WIRING DRIFT: main.go binds %s at %s to something other than %s.%s(...).\n"+
			"  The constructor returns an INTERFACE, so `%s = nil` yields a genuinely nil interface: "+
			"every worker's `if deps.SweepCursors != nil` skips, all four sweeps silently revert to "+
			"the pre-#1477 starving behaviour, and the four per-site literal checks below still pass "+
			"because they only look at the NAME. The name must be bound exactly once, by the "+
			"constructor, and never rebound.", sweepCursorVarName, at, sweepCursorPkg, sweepCursorCtor,
			sweepCursorVarName)
	}

	for _, at := range ctorArgFindings {
		t.Errorf("WIRING DRIFT: main.go binds %s at %s to repository.%s(...) called around something "+
			"other than the shared %q pool.\n"+
			"  The constructor is not a validator: it wraps whatever it is given. A nil or substituted "+
			"DB makes every Load and Save fail, and resumeSweep (queue/jobs/sweep_resume.go) treats a "+
			"failed Load as NON-FATAL and starts the run FROM THE BEGINNING — the pre-#1477 starving "+
			"behaviour, reached without touching any of the four %s field assignments this test checks "+
			"below. If the pool legitimately moved or was renamed, update %s in this test; that edit is "+
			"the reviewable act the guard exists to force.",
			sweepCursorVarName, at, sweepCursorCtor, dbVarName, sweepCursorField, "dbVarName")
	}

	for _, at := range dbRebinds {
		t.Errorf("WIRING DRIFT: main.go binds %q at %s in a shape this guard does not recognise.\n"+
			"  %s is checked to be built around %q by name, so a rebinding or shadowing of %q defeats "+
			"that check while the argument still reads correctly. Only two shapes are accepted: the "+
			"`var %s *pgxpool.Pool` declaration and the `%s, err = %s.%s(...)` assignment. A "+
			"*pgxpool.Pool set to nil is worse than a nil interface: stored in the repository's DB "+
			"field it is a NON-nil interface, so every worker's `!= nil` guard passes and the sweeps "+
			"panic instead of degrading.",
			dbVarName, at, sweepCursorCtor, dbVarName, dbVarName, dbVarName, dbVarName, dbPoolPkg, dbPoolCtor)
	}

	for _, at := range addressTaken {
		t.Errorf("WIRING DRIFT: main.go takes the ADDRESS of a guarded variable at %s.\n"+
			"  Every other check in this guard reads the left-hand side of an assignment as an "+
			"identifier or a selector. `*p = nil` through a pointer is neither, so it nils the shared "+
			"value for all four sweeps with nothing to see: `go build`, `go vet` and this test were all "+
			"clean against the demonstrated bypass\n"+
			"    func(p *repository.SweepCursorRepository) { *p = nil }(&%s)\n"+
			"  Since the guard cannot follow a pointer, it forbids creating one. If main.go legitimately "+
			"needs the address of %q or %q, that is the reviewable act this guard exists to force.",
			at, sweepCursorVarName, sweepCursorVarName, dbVarName)
	}

	for _, at := range shadowingParams {
		t.Errorf("WIRING DRIFT: main.go binds a guarded name as a function parameter/result/receiver at %s.\n"+
			"  A parameter is an *ast.Field, so it is invisible to the ValueSpec, AssignStmt and "+
			"RangeStmt cases that catch every other shadowing form — while the inline-literal check and "+
			"the by-name %s check below both still read as correct. Demonstrated bypass:\n"+
			"    func(%s repository.SweepCursorRepository) { … SweepCursors: %s … }(nil)\n"+
			"  %q and %q must name the shared values built at the top of main.go and nothing else.",
			at, sweepCursorField, sweepCursorVarName, sweepCursorVarName, sweepCursorVarName, dbVarName)
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
