package main

// TestRuleQuarantineCensusWorkerIsWiredInMain — #1625 Task 18 fix round 1.
//
// Round 1's finding was NOT that services.RunRuleQuarantineCensus or the worker calling it were
// broken — both were fully unit-tested and worked correctly in isolation. The finding was that
// nothing in main.go ever CONSTRUCTED and REGISTERED that worker, so the census had no production
// caller: the population gauge would sit at its pre-warmed zero forever, indistinguishable from
// "genuinely nothing is quarantined."
//
// A unit test on the worker or on buildPeriodicJobSpecs (given a hand-populated deps struct)
// cannot catch that class of regression — both would stay green even if main.go never built the
// deps struct with a real worker at all. This test, in the spirit of
// TestExplicitDestructionAuditSinkIsWiredInMain (issue1569_audit_sink_wiring_drift_test.go),
// parses main.go's own AST and proves the full chain: a variable is constructed from
// jobs.NewRuleQuarantineCensusWorker, that SAME variable is the one passed into
// periodicJobsDeps.ruleQuarantineCensusWorker, and that SAME variable is the one passed to
// river.AddWorker. Reusing an AST scan (not a text/substring scan) is deliberate: a substring
// guard passes as long as the expression appears ANYWHERE in the file, even in a comment or on
// an unrelated variable — the exact failure mode issue1569's own header comment calls out.
import (
	"go/ast"
	"testing"
)

func TestRuleQuarantineCensusWorkerIsWiredInMain(t *testing.T) {
	mainSrc := parseMainGo(t) // shared helper, defined in issue1569_audit_sink_wiring_drift_test.go

	constructed := varsByConstructor(mainSrc) // shared helper, same file
	vars, ok := constructed["NewRuleQuarantineCensusWorker"]
	if !ok || len(vars) == 0 {
		t.Fatal("no variable in main.go is assigned from jobs.NewRuleQuarantineCensusWorker(...) — " +
			"the #1625 Task 18 census worker is never constructed in production, so it can never run")
	}

	depsVar, ok := periodicJobsDepsFieldValue(mainSrc, "ruleQuarantineCensusWorker")
	if !ok {
		t.Fatal("periodicJobsDeps{...} in main.go has no ruleQuarantineCensusWorker field set — " +
			"buildPeriodicJobSpecs will never see this worker, so it will never be registered as a periodic job")
	}
	if !containsString(vars, depsVar) {
		t.Fatalf("periodicJobsDeps.ruleQuarantineCensusWorker is set to %q, which is not one of the "+
			"variable(s) constructed from jobs.NewRuleQuarantineCensusWorker (%v) — the wrong variable is wired",
			depsVar, vars)
	}

	addWorkerVars := riverAddWorkerArgs(mainSrc)
	if !containsString(addWorkerVars, depsVar) {
		t.Fatalf("river.AddWorker(riverWorkers, %s) is never called in main.go — even though the census "+
			"worker is constructed and reaches periodicJobsDeps, River has no Work() implementation "+
			"registered for its Kind(), so every periodic insert would fail with an unknown-job-kind error",
			depsVar)
	}
}

// periodicJobsDepsFieldValue finds the `periodicJobsDeps{...}` composite literal in main.go and
// returns the identifier assigned to the given field name (e.g. "ruleQuarantineCensusWorker:
// ruleQuarantineCensusWorker" -> "ruleQuarantineCensusWorker"). ok is false if the literal, or
// that field within it, is not found.
func periodicJobsDepsFieldValue(file *ast.File, fieldName string) (string, bool) {
	var value string
	var found bool
	ast.Inspect(file, func(n ast.Node) bool {
		if found {
			return false
		}
		lit, ok := n.(*ast.CompositeLit)
		if !ok {
			return true
		}
		ident, ok := lit.Type.(*ast.Ident)
		if !ok || ident.Name != "periodicJobsDeps" {
			return true
		}
		for _, elt := range lit.Elts {
			kv, ok := elt.(*ast.KeyValueExpr)
			if !ok {
				continue
			}
			key, ok := kv.Key.(*ast.Ident)
			if !ok || key.Name != fieldName {
				continue
			}
			if valIdent, ok := kv.Value.(*ast.Ident); ok {
				value = valIdent.Name
				found = true
			}
		}
		return !found
	})
	return value, found
}

// riverAddWorkerArgs returns the identifier name of the second argument to every
// `river.AddWorker(riverWorkers, <worker>)` call in main.go.
func riverAddWorkerArgs(file *ast.File) []string {
	var out []string
	ast.Inspect(file, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		pkg, ok := sel.X.(*ast.Ident)
		if !ok || pkg.Name != "river" || sel.Sel.Name != "AddWorker" {
			return true
		}
		if len(call.Args) != 2 {
			return true
		}
		if ident, ok := call.Args[1].(*ast.Ident); ok {
			out = append(out, ident.Name)
		}
		return true
	})
	return out
}

func containsString(haystack []string, needle string) bool {
	for _, s := range haystack {
		if s == needle {
			return true
		}
	}
	return false
}
