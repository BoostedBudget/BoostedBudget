package repository

import (
	"go/ast"
	"go/token"
	"strconv"
	"strings"
)

// Shared SQL-literal folding for the source-scanning guards in this package.
//
// WHY IT EXISTS, and it is not a refactor. The guards used to walk *ast.BasicLit and judge each
// string literal on its own. That is correct only while every query is ONE literal — and the moment
// a query is assembled by concatenation, `SELECT ...` and `... FROM accounts` land in different
// literals, no `SELECT ... FROM` span matches, and the query becomes INVISIBLE to the guard. It
// still passes. It passes harder than before, because there is now nothing to judge.
//
// This was not hypothetical: routing the balance columns through accountBalanceProjection (#1569
// task 21, item D) split ten account projections into concatenations, and
// TestNoAccountProjectionOmitsStatus went from catching a deleted `status` to shrugging at it —
// measured by deleting `status` from GetByUserID's projection and watching the guard stay green.
// Blinding an existing guard to make a new change fit is the one thing this branch's rules forbid
// outright, so the guards learned to fold instead.
//
// The folder is exact where it can be and LOUD where it cannot: an operand it cannot evaluate
// becomes the placeholder below, which contains no word characters and therefore cannot be mistaken
// for a column name by any guard's regex. A guard seeing the placeholder inside a select list is
// seeing a genuinely unknown fragment, not a column it can assume is fine.
//
// It evaluates this package's own SQL-fragment renderers BY CALLING THEM — these tests live in
// package `repository`, so `accountBalanceProjection("a")` here is the same text the production
// query gets, not a restatement of it that could drift.

// unfoldableSQLFragment stands in for an operand the folder cannot evaluate. Deliberately free of
// word characters so `\b<column>\b` regexes never match inside it.
const unfoldableSQLFragment = " /* ??? */ "

// foldSQLExpr renders a Go string expression to the SQL text it produces, as far as that is
// statically knowable. The bool reports whether the expression was string-shaped at all.
func foldSQLExpr(expr ast.Expr) (string, bool) {
	switch e := expr.(type) {
	case *ast.ParenExpr:
		return foldSQLExpr(e.X)

	case *ast.BasicLit:
		if e.Kind != token.STRING {
			return "", false
		}
		s, err := strconv.Unquote(e.Value)
		if err != nil {
			return "", false
		}
		return s, true

	case *ast.BinaryExpr:
		if e.Op != token.ADD {
			return "", false
		}
		left, lok := foldSQLExpr(e.X)
		right, rok := foldSQLExpr(e.Y)
		if !lok && !rok {
			return "", false
		}
		if !lok {
			left = unfoldableSQLFragment
		}
		if !rok {
			right = unfoldableSQLFragment
		}
		return left + right, true

	case *ast.CallExpr:
		// This package's SQL-fragment renderers, evaluated for real rather than approximated.
		// Anything else is unknown text.
		if fn, ok := e.Fun.(*ast.Ident); ok && len(e.Args) == 1 {
			arg, aok := foldSQLExpr(e.Args[0])
			if aok {
				switch fn.Name {
				case "accountBalanceProjection":
					return accountBalanceProjection(arg), true
				case "currentStateAccountLeg":
					return currentStateAccountLeg(arg), true
				}
			}
		}
		return unfoldableSQLFragment, true
	}
	return "", false
}

// sqlStringExprs returns every top-level string-shaped expression in a file, with its position.
//
// TOP-LEVEL is the whole point: a concatenation is folded ONCE, as a unit. Descending into it would
// hand the guards the same fragments they were already being fooled by.
func sqlStringExprs(file *ast.File) []struct {
	SQL string
	Pos token.Pos
} {
	var out []struct {
		SQL string
		Pos token.Pos
	}
	ast.Inspect(file, func(n ast.Node) bool {
		expr, ok := n.(ast.Expr)
		if !ok {
			return true
		}
		switch expr.(type) {
		case *ast.BasicLit, *ast.BinaryExpr, *ast.ParenExpr:
		default:
			return true
		}
		sql, ok := foldSQLExpr(expr)
		if !ok {
			return true
		}
		out = append(out, struct {
			SQL string
			Pos token.Pos
		}{sql, expr.Pos()})
		// Do not descend: the fragments of a folded concatenation must not be judged separately.
		return false
	})
	return out
}

// containsAccountProjectionMarker reports whether folded SQL looks like an account-shaped read.
func containsAccountProjectionMarker(sql string) bool {
	return strings.Contains(strings.ToLower(sql), "plaid_account_id")
}
