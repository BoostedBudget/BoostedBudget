package main

// TestExplicitDestructionAuditSinkIsWiredInMain — Issue #1569 round 17 task 11.
//
// The ADR's control 2 for explicit destruction of financial history is "an audit row written in
// the SAME transaction". Every repository method implementing it takes the AuditLogRepository as a
// parameter and skips the write when it is nil; in production that parameter comes from a field
// populated by a `SetAuditLogRepository` call HERE, in cmd/server/main.go. Dropping that one line
// used to turn the control off silently — the destruction still succeeded, the trail was simply
// never written, and no unit test noticed because every unit test injects the dependency itself.
//
// Two things now stop that. The destruction paths refuse to run with a nil sink
// (repository.ErrAuditLogRepositoryNotWired), which protects the data; and this test fails in CI,
// which protects the FEATURE — a refusal a user hits at purge time is a worse way to find out.
//
// The list of things to check is DERIVED, not hand-maintained: every concrete type in
// backend/internal that declares a SetAuditLogRepository method must be constructed and wired in
// main.go. A new service or handler that takes an audit sink is therefore covered the day it is
// written, with no allowlist to forget.

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"unicode"
)

const auditSinkSetter = "SetAuditLogRepository"

// TestExplicitDestructionAuditSinkIsWiredInMain is described in this file's header comment.
func TestExplicitDestructionAuditSinkIsWiredInMain(t *testing.T) {
	declaringTypes := typesDeclaringAuditSinkSetter(t, "../../internal")
	if len(declaringTypes) == 0 {
		t.Fatalf("found no type declaring %s under backend/internal — this guard has stopped "+
			"seeing its corpus (renamed setter? moved packages?), which makes it vacuous", auditSinkSetter)
	}

	mainSrc := parseMainGo(t)
	wired := varsWithAuditSinkSetterCall(mainSrc)
	constructed := varsByConstructor(mainSrc)

	var failures []string
	for _, typeName := range declaringTypes {
		ctor := "New" + exported(typeName)
		vars, ok := constructed[ctor]
		if !ok {
			failures = append(failures, "MISSING CONSTRUCTION: no variable in main.go is assigned from "+
				ctor+"(...) — but "+typeName+" declares "+auditSinkSetter+
				". Either wire it, or rename the constructor and update this guard's derivation.")
			continue
		}
		if !anyWired(vars, wired) {
			failures = append(failures, "MISSING WIRING: "+strings.Join(vars, "/")+" (from "+ctor+
				") never has ."+auditSinkSetter+"(...) called on it in main.go — "+typeName+
				" would run with a nil audit sink, and its history-destroying paths would refuse.")
		}
	}

	if len(failures) > 0 {
		t.Errorf("audit-sink wiring drift in cmd/server/main.go (%d):\n\n%s\n\n"+
			"ADR control 2 (docs/decisions/2026-08-24-financial-history-retained-by-default.md) "+
			"requires an audit row in the same transaction as any explicit destruction of financial "+
			"history. That row is only written when the sink is wired here.",
			len(failures), strings.Join(failures, "\n"))
	}
}

// typesDeclaringAuditSinkSetter returns the sorted names of concrete types under root that declare
// a SetAuditLogRepository method in non-test source. Interface declarations of the same method are
// deliberately not collected: an interface is not constructed and not wired.
func typesDeclaringAuditSinkSetter(t *testing.T, root string) []string {
	t.Helper()

	seen := map[string]bool{}
	fset := token.NewFileSet()

	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		file, perr := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
		if perr != nil {
			return perr
		}
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Recv == nil || fn.Name.Name != auditSinkSetter {
				continue
			}
			if name := auditSinkReceiverTypeName(fn.Recv); name != "" {
				seen[name] = true
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("scanning %s: %v", root, err)
	}

	names := make([]string, 0, len(seen))
	for name := range seen {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// receiverTypeName extracts `T` from a `(x *T)` or `(x T)` receiver list.
func auditSinkReceiverTypeName(recv *ast.FieldList) string {
	if recv == nil || len(recv.List) == 0 {
		return ""
	}
	expr := recv.List[0].Type
	if star, ok := expr.(*ast.StarExpr); ok {
		expr = star.X
	}
	if ident, ok := expr.(*ast.Ident); ok {
		return ident.Name
	}
	return ""
}

func parseMainGo(t *testing.T) *ast.File {
	t.Helper()
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "main.go", nil, parser.SkipObjectResolution)
	if err != nil {
		t.Fatalf("parsing main.go: %v", err)
	}
	return file
}

// varsByConstructor maps a constructor name ("NewAccountService") to the main.go variable names
// assigned from a call to it, whether the call is qualified (services.NewAccountService) or not.
func varsByConstructor(file *ast.File) map[string][]string {
	out := map[string][]string{}
	ast.Inspect(file, func(n ast.Node) bool {
		assign, ok := n.(*ast.AssignStmt)
		if !ok {
			return true
		}
		for i, rhs := range assign.Rhs {
			call, ok := rhs.(*ast.CallExpr)
			if !ok || i >= len(assign.Lhs) {
				continue
			}
			ctor := calleeName(call.Fun)
			lhs, ok := assign.Lhs[i].(*ast.Ident)
			if ctor == "" || !ok {
				continue
			}
			out[ctor] = append(out[ctor], lhs.Name)
		}
		return true
	})
	return out
}

// varsWithAuditSinkSetterCall returns the set of main.go variable names on which
// SetAuditLogRepository is called.
func varsWithAuditSinkSetterCall(file *ast.File) map[string]bool {
	out := map[string]bool{}
	ast.Inspect(file, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || sel.Sel.Name != auditSinkSetter {
			return true
		}
		if recv, ok := sel.X.(*ast.Ident); ok {
			out[recv.Name] = true
		}
		return true
	})
	return out
}

func calleeName(fun ast.Expr) string {
	switch f := fun.(type) {
	case *ast.Ident:
		return f.Name
	case *ast.SelectorExpr:
		return f.Sel.Name
	}
	return ""
}

func anyWired(vars []string, wired map[string]bool) bool {
	for _, v := range vars {
		if wired[v] {
			return true
		}
	}
	return false
}

func exported(name string) string {
	if name == "" {
		return name
	}
	r := []rune(name)
	r[0] = unicode.ToUpper(r[0])
	return string(r)
}
