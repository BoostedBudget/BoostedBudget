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
//
// ⚠ WHAT IT CHECKS ABOUT THE ARGUMENT, since #1569 round 17 (Codex defect 4). It used to check only
// that the setter was CALLED, so `svc.SetAuditLogRepository(nil)` — a one-token edit — satisfied it
// while leaving the field nil and every purge endpoint returning a 500. A literal nil is now
// rejected. That is the whole of the argument check: it still does NOT verify that the value passed
// is the real repository (a no-op implementation, or a variable that is nil at runtime, passes), nor
// that anything is ever written.
//
// ⚠ AND THE LIMIT THAT NO AMOUNT OF STRENGTHENING FIXES, left acknowledged deliberately. The
// derivation key is "declares SetAuditLogRepository". A brand-new service that destroys financial
// history and never declares that setter — because it takes its sink as a constructor parameter, or
// reads it from a package-level var, or writes no audit row at all — is INVISIBLE to this guard. It
// is not invisible to every guard: the hard DELETE itself would be reported by G2
// (TestNoUnallowlistedHistoryDelete, backend/internal/repository), which is derived from the SQL
// rather than from a wiring convention and is the reason this one does not need to close that hole.
// If you are adding a destruction path, copy an existing service's setter shape so this guard can
// see it; G2 will make you notice either way.

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
	wired, wiredWithNil := varsWithAuditSinkSetterCall(mainSrc)
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
			if anyWired(vars, wiredWithNil) {
				// #1569 round 17, Codex defect 4. Reported separately from MISSING WIRING because
				// the remedy is different and the diff that produced it looks wired.
				failures = append(failures, "NIL WIRING: "+strings.Join(vars, "/")+" (from "+ctor+
					") has ."+auditSinkSetter+"(nil) called on it in main.go, which is the same as "+
					"not wiring it: "+typeName+"'s history-destroying paths would return "+
					"repository.ErrAuditLogRepositoryNotWired. Pass the real repository.")
				continue
			}
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

// varsWithAuditSinkSetterCall returns two sets of main.go variable names: those on which
// SetAuditLogRepository is called with a real argument, and those on which it is called with the
// literal `nil`. A variable appearing in both — wired properly somewhere and with nil elsewhere —
// counts as wired, because the last call wins at runtime and the guard cannot order them; that is
// a shape nothing in main.go writes and a reviewer would reject on sight.
func varsWithAuditSinkSetterCall(file *ast.File) (wired, wiredWithNil map[string]bool) {
	wired, wiredWithNil = map[string]bool{}, map[string]bool{}
	ast.Inspect(file, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || sel.Sel.Name != auditSinkSetter {
			return true
		}
		recv, ok := sel.X.(*ast.Ident)
		if !ok {
			return true
		}
		if len(call.Args) == 1 {
			if ident, ok := call.Args[0].(*ast.Ident); ok && ident.Name == "nil" {
				wiredWithNil[recv.Name] = true
				return true
			}
		}
		wired[recv.Name] = true
		return true
	})
	return wired, wiredWithNil
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
