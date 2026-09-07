package repository

import (
	"go/ast"
	"go/parser"
	"go/token"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
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
				// #1609. Without these four the customization overlay folds to the
				// placeholder, and TestNoAccountCustomizationReadWithoutTheOverlay — whose
				// whole design is to strip the renderer's OWN OUTPUT — would have nothing to
				// strip and nothing to find. Same reason accountBalanceProjection is here:
				// this file is package `repository`, so these calls return the exact text the
				// production query gets rather than a restatement that could drift.
				case "accountCustomizationProjection":
					return accountCustomizationProjection(arg), true
				case "accountCustomizationJoin":
					return accountCustomizationJoin(arg), true
				case "accountHiddenExpr":
					return accountHiddenExpr(arg), true
				case "accountDisplayOrderExpr":
					return accountDisplayOrderExpr(arg), true
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

// TestFoldSQLExpr is the folder's own self-test.
//
// Two source-scanning guards now depend entirely on this function, and it was shipped with no test
// of its own — so a folding bug would have silently blinded both while every guard stayed green.
// That is the same shape as the defect it was written to repair, one level down.
func TestFoldSQLExpr(t *testing.T) {
	// mustParseExpr is deliberately not shared: each case is a standalone Go expression.
	fold := func(t *testing.T, src string) (string, bool) {
		t.Helper()
		expr, err := parser.ParseExpr(src)
		if err != nil {
			t.Fatalf("parsing %q: %v", src, err)
		}
		return foldSQLExpr(expr)
	}

	t.Run("a plain literal is itself", func(t *testing.T) {
		got, ok := fold(t, "`SELECT id FROM accounts`")
		require.True(t, ok)
		require.Equal(t, "SELECT id FROM accounts", got)
	})

	t.Run("a concatenation is joined — the whole point", func(t *testing.T) {
		got, ok := fold(t, "`SELECT id, ` + `status FROM accounts`")
		require.True(t, ok)
		require.Equal(t, "SELECT id, status FROM accounts", got)
		require.Regexp(t, `(?is)\bSELECT\b.*\bFROM\b`, got,
			"a per-literal scan sees no SELECT...FROM span here at all; that is the blindness this fixes")
	})

	t.Run("this package's renderers are CALLED, not approximated", func(t *testing.T) {
		got, ok := fold(t, "`SELECT ` + accountBalanceProjection(\"a\") + ` FROM accounts a`")
		require.True(t, ok)
		require.Contains(t, got, accountBalanceProjection("a"),
			"the folded text must be byte-identical to what the production query gets")
		require.Contains(t, got, "CASE WHEN a.status = 'active'")

		got, ok = fold(t, "`WHERE ` + currentStateAccountLeg(\"a\")")
		require.True(t, ok)
		require.Equal(t, "WHERE "+currentStateAccountLeg("a"), got)
	})

	t.Run("an unknown call becomes the placeholder, which has no word characters", func(t *testing.T) {
		got, ok := fold(t, "`SELECT ` + someUnknownRenderer(x) + ` FROM accounts`")
		require.True(t, ok)
		require.Contains(t, got, unfoldableSQLFragment)
		require.NotRegexp(t, `\bcurrent_balance\b`, got)
		// The span still resolves, so the guards judge the KNOWN part rather than skipping the query.
		require.Regexp(t, `(?is)\bSELECT\b.*\bFROM\b`, got)
	})

	t.Run("a bare identifier is not string-shaped", func(t *testing.T) {
		_, ok := fold(t, "someQueryConst")
		require.False(t, ok, "an opaque identifier must not be reported as folded empty text — "+
			"that would let a guard treat an unknown query as an empty select list")
	})

	t.Run("non-ADD operators are not folded", func(t *testing.T) {
		_, ok := fold(t, "`a` == `b`")
		require.False(t, ok)
	})

	t.Run("sqlStringExprs folds a concatenation ONCE, not per fragment", func(t *testing.T) {
		file, err := parser.ParseFile(token.NewFileSet(), "x.go",
			"package p\nfunc f() string { return `SELECT plaid_account_id, ` + `status FROM accounts` }", 0)
		require.NoError(t, err)

		var accountShaped []string
		for _, e := range sqlStringExprs(file) {
			if containsAccountProjectionMarker(e.SQL) {
				accountShaped = append(accountShaped, e.SQL)
			}
		}
		require.Len(t, accountShaped, 1,
			"descending into the fragments would hand the guards the same halves that fooled them")
		require.Equal(t, "SELECT plaid_account_id, status FROM accounts", accountShaped[0])
	})
}
