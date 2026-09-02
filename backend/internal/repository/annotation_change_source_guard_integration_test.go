//go:build integration

package repository_test

import (
	"context"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
)

// The change-source vocabulary is stated in three places and was wrong in two of them.
//
// domain/transaction_annotation_history.go carried "pinned by the live
// chk_transaction_annotation_history_source CHECK: change_source IN ('category','review')" for the
// whole of #1569, and the struct field's own comment said the same. Both were false by two values:
// migration 000170 widened the CHECK to admit 'settlement' and 000172 to admit 'erasure', and both
// constants were sitting six lines below the sentence denying they were legal.
//
// A stale statement of what the database allows is worse than no statement — the next reader takes
// it as the authority and concludes that writing one of the missing sources would be rejected. This
// guard is what stops the sentence going stale again: the Go vocabulary is re-derived from the
// constant declarations, the SQL vocabulary is re-derived from pg_constraint, and they are compared
// in BOTH directions. Neither side is a list anyone can edit to make the failure disappear without
// closing it.

const annotationSourceConstFile = "../domain/transaction_annotation_history.go"

var (
	annotationSourceConstPrefix = "TransactionAnnotationChange"
	sqlQuotedLiteral            = regexp.MustCompile(`'([^']*)'`)
)

// declaredAnnotationChangeSources reads the string VALUES of every TransactionAnnotationChange*
// constant out of the domain package's own source.
//
// ⚠ Scope limits, stated rather than assumed: only top-level `const` declarations in that one file
// are scanned, and only ones whose value is a plain string literal. A source added as a computed
// expression, in another file, or inside a function would be invisible here — and would then be
// reported as a SQL-side-only value by the comparison below, which is a failure and not a silent
// pass.
func declaredAnnotationChangeSources(t *testing.T) []string {
	t.Helper()

	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, annotationSourceConstFile, nil, 0)
	require.NoError(t, err, "parse %s", annotationSourceConstFile)

	var values []string
	for _, decl := range file.Decls {
		gen, ok := decl.(*ast.GenDecl)
		if !ok || gen.Tok != token.CONST {
			continue
		}
		for _, spec := range gen.Specs {
			value, ok := spec.(*ast.ValueSpec)
			if !ok {
				continue
			}
			for i, name := range value.Names {
				if !strings.HasPrefix(name.Name, annotationSourceConstPrefix) || i >= len(value.Values) {
					continue
				}
				lit, ok := value.Values[i].(*ast.BasicLit)
				if !ok || lit.Kind != token.STRING {
					continue
				}
				unquoted, err := strconv.Unquote(lit.Value)
				require.NoError(t, err, "unquote %s", name.Name)
				values = append(values, unquoted)
			}
		}
	}

	// Fail closed. A rename of the constant prefix would empty this set, and an empty set compares
	// equal to nothing — which would read as agreement.
	require.NotEmpty(t, values,
		"no %s* string constants found in %s — re-point this guard rather than deleting it, or the "+
			"Go half of the comparison silently empties",
		annotationSourceConstPrefix, annotationSourceConstFile)

	sort.Strings(values)
	return values
}

// TestAnnotationChangeSourcesMatchTheLiveCheck compares the declared sources with the ones the live
// CHECK constraint actually admits.
func TestAnnotationChangeSourcesMatchTheLiveCheck(t *testing.T) {
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		t.Skip("DATABASE_URL not set — skipping integration test")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	require.NoError(t, err, "connect to test database")
	defer pool.Close()

	var definition string
	err = pool.QueryRow(ctx, `
		SELECT pg_get_constraintdef(oid)
		FROM pg_constraint
		WHERE conname = 'chk_transaction_annotation_history_source'`).Scan(&definition)
	require.NoError(t, err,
		"chk_transaction_annotation_history_source is not on the database — either the migrations "+
			"have not run or the constraint was dropped, and a guard that cannot read it must not pass")

	var allowed []string
	for _, m := range sqlQuotedLiteral.FindAllStringSubmatch(definition, -1) {
		allowed = append(allowed, m[1])
	}
	sort.Strings(allowed)
	require.NotEmpty(t, allowed, "no quoted values in %q — the constraint shape changed", definition)

	require.Equal(t, allowed, declaredAnnotationChangeSources(t),
		"the change_source vocabulary in %s disagrees with the live CHECK (%s).\n"+
			"Every direction of this disagreement is a defect: a Go constant the database will "+
			"reject is a runtime CHECK violation, and a database value with no constant is a source "+
			"nothing in Go can name or explain. #1569 shipped the second kind for two values while a "+
			"comment two lines above went on claiming only 'category' and 'review' were legal.",
		annotationSourceConstFile, definition)
}
