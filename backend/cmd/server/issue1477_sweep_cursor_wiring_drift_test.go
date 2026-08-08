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
// does not belong. Parsing binds each injection to the specific worker's Deps literal, so
// removing any single site fails with that site named.
//
// If a sweep is legitimately retired, delete its entry from requiredSweepWiring below —
// that edit is the deliberate, reviewable act this test exists to force.

import (
	"go/ast"
	"go/parser"
	"go/token"
	"testing"
)

// The Deps struct literal (in package jobs) that each of the four capped sweeps is
// constructed from, and why that sweep needs the cursor.
var requiredSweepWiring = []struct {
	depsType string
	why      string
}{
	{
		"ScheduledDeletionCheckWorkerDeps",
		"scheduled_deletion_check: unwired, a user whose hard deletion keeps failing holds the head of the queue and nothing past maxUsersPerDeletionSweep is ever enqueued",
	},
	{
		"RetentionCleanupCheckWorkerDeps",
		"retention_cleanup_check (deleteExpiredAccounts): unwired, the sweep restarts from the oldest expired user every run and never reaches past maxUsersPerRetentionSweep",
	},
	{
		"ItemCleanupCheckWorkerDeps",
		"item_cleanup_check: unwired, an Item that cannot be scheduled starves every candidate behind it and Plaid keeps billing for it",
	},
	{
		"PlaidOrphanRetryWorkerDeps",
		"plaid_orphan_retry: unwired, an orphan record whose /item/remove keeps failing at Plaid holds the head and nothing past maxOrphanRetriesPerSweep is ever retried",
	},
}

// The field name inside those Deps structs, and the local variable main.go must pass.
const (
	sweepCursorField   = "SweepCursors"
	sweepCursorVarName = "sweepCursorRepo"
	sweepCursorCtor    = "NewSweepCursorRepository"
)

func TestIssue1477SweepCursorWiringNoDrift(t *testing.T) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "main.go", nil, parser.SkipObjectResolution)
	if err != nil {
		t.Fatalf("could not parse main.go: %v", err)
	}

	// found[depsType] = list of per-literal results, one entry per composite literal of
	// that type appearing in main.go.
	type litResult struct {
		pos      string
		hasField bool
		value    string // source-ish rendering of the field's value, "" when absent
	}
	found := map[string][]litResult{}

	// A control on the parse itself: if the walk somehow sees no jobs.*WorkerDeps
	// literals at all, the per-type zero checks below would each fire, but this counter
	// makes "the scan found nothing" distinguishable from "the wiring was removed".
	totalJobsDepsLiterals := 0

	sawCursorConstruction := false

	ast.Inspect(file, func(n ast.Node) bool {
		switch node := n.(type) {
		case *ast.CallExpr:
			// repository.NewSweepCursorRepository(db) — the value all four sites share.
			if sel, ok := node.Fun.(*ast.SelectorExpr); ok && sel.Sel.Name == sweepCursorCtor {
				sawCursorConstruction = true
			}
		case *ast.CompositeLit:
			sel, ok := node.Type.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			pkg, ok := sel.X.(*ast.Ident)
			if !ok || pkg.Name != "jobs" {
				return true
			}
			typeName := sel.Sel.Name
			if len(typeName) < len("WorkerDeps") || typeName[len(typeName)-len("WorkerDeps"):] != "WorkerDeps" {
				return true
			}
			totalJobsDepsLiterals++

			res := litResult{pos: fset.Position(node.Lbrace).String()}
			for _, elt := range node.Elts {
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
					res.value = id.Name
				} else {
					res.value = "<non-identifier expression>"
				}
			}
			found[typeName] = append(found[typeName], res)
		}
		return true
	})

	// Fail closed if the scan found nothing to check. An empty scan is not a pass:
	// a rename of the jobs package alias, or main.go being moved/split, would otherwise
	// silence this guard entirely while it still reported success.
	if totalJobsDepsLiterals == 0 {
		t.Fatalf("found ZERO jobs.*WorkerDeps composite literals in main.go — the scan is vacuous, "+
			"not clean. main.go was parsed (%d top-level decls), so this means the worker "+
			"construction moved or the `jobs` import alias changed; update this test to follow it.",
			len(file.Decls))
	}

	if !sawCursorConstruction {
		t.Errorf("main.go never calls repository.%s(...) — there is no sweep-cursor repository to inject "+
			"at any of the four sites (Issue #1477).", sweepCursorCtor)
	}

	for _, req := range requiredSweepWiring {
		lits := found[req.depsType]
		if len(lits) == 0 {
			t.Errorf("no jobs.%s composite literal found in main.go at all — this sweep is no longer "+
				"constructed here, so its cursor wiring cannot be checked. %s",
				req.depsType, req.why)
			continue
		}
		for _, lit := range lits {
			switch {
			case !lit.hasField:
				t.Errorf("WIRING DRIFT: jobs.%s at %s does not set %s.\n  %s\n"+
					"  The worker nil-guards this field, so the omission is invisible at compile time, "+
					"at startup, and to every unit test.",
					req.depsType, lit.pos, sweepCursorField, req.why)
			case lit.value == "nil":
				t.Errorf("WIRING DRIFT: jobs.%s at %s sets %s to a literal nil.\n  %s",
					req.depsType, lit.pos, sweepCursorField, req.why)
			case lit.value != sweepCursorVarName:
				t.Errorf("WIRING DRIFT: jobs.%s at %s sets %s to %q, not the shared %q built by "+
					"repository.%s.\n  %s\n  All four sweeps must share ONE repository value; a "+
					"per-sweep substitute is a change that needs review, not a silent edit.",
					req.depsType, lit.pos, sweepCursorField, lit.value, sweepCursorVarName,
					sweepCursorCtor, req.why)
			}
		}
	}
}
