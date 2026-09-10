package repository

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
)

// topLevelConjuncts splits a WHERE fragment on the " AND "s that sit at paren depth 0, so a test
// can ask which predicates are ANDed together versus which share one parenthesised group.
func topLevelConjuncts(clause string) []string {
	var out []string
	depth, start := 0, 0
	for i := 0; i < len(clause); i++ {
		switch clause[i] {
		case '(':
			depth++
		case ')':
			depth--
		}
		if depth == 0 && strings.HasPrefix(clause[i:], " AND ") {
			out = append(out, strings.TrimSpace(clause[start:i]))
			start = i + len(" AND ")
			i += len(" AND ") - 1
		}
	}
	out = append(out, strings.TrimSpace(clause[start:]))
	return out
}

// TestCategoryFilterGroup_OrsAllThreePredicates is the #1626 bug-C guard.
//
// pfc_primaries and pfc_detaileds were ORed with each other, but custom_category_ids was appended
// as a SEPARATE top-level AND. A transaction is either pfc-kind or custom-kind and never both —
// effective_pfc_primary is NULL on a custom row, and the custom arm requires
// category_override_kind = 'custom' — so the AND of the two matched NOTHING. Any request carrying
// both kinds returned zero rows by construction.
//
// That is why clicking a PFC parent that has custom categories nested under it could not be
// expressed as one filter, and why the Sankey under-reported on such a click.
func TestCategoryFilterGroup_OrsAllThreePredicates(t *testing.T) {
	filters := domain.TransactionFilters{
		PFCPrimaries:      []string{"FOOD_AND_DRINK"},
		PFCDetaileds:      []string{"TRANSPORTATION_RIDE_SHARE"},
		CustomCategoryIDs: []string{"9f1c2b3a-0000-4000-8000-000000000001"},
	}

	clause, args, nextIdx := categoryFilterGroup(filters, 2, []interface{}{"user-abc"})

	require.NotEmpty(t, clause, "a category group must be produced when any category filter is set")

	conjuncts := topLevelConjuncts(strings.TrimPrefix(strings.TrimSpace(clause), "AND "))
	require.Len(t, conjuncts, 1,
		"all category predicates present must share ONE top-level group; splitting them across "+
			"top-level ANDs is the #1626 defect — a row cannot be both pfc-kind and custom-kind, so "+
			"the conjunction matches nothing. Got:\n%s", strings.Join(conjuncts, "\n--AND--\n"))

	assert.Contains(t, clause, "effective_pfc_primary IN (")
	assert.Contains(t, clause, "effective_pfc_detailed IN (")
	assert.Contains(t, clause, "category_override_kind = 'custom'")
	assert.Contains(t, clause, " OR ", "the predicates must be OR-joined")

	assert.Contains(t, clause, "parent_id::text IN (",
		"custom_category_ids must keep expanding to itself plus its children — the picker that "+
			"supplies these ids is tombstone-filtered, the rows they select are history")

	require.Len(t, args, 4, "user_id plus one arg per filter value")
	assert.Equal(t, 5, nextIdx, "three values bound from $2 leaves $5 as the next free placeholder")
}

// TestCategoryFilterGroup_SinglePredicateStillWorks guards the narrow cases: with only one kind of
// category filter set there is nothing to OR, and the group must not degrade into a tautology.
func TestCategoryFilterGroup_SinglePredicateStillWorks(t *testing.T) {
	t.Run("pfc only", func(t *testing.T) {
		clause, _, _ := categoryFilterGroup(
			domain.TransactionFilters{PFCPrimaries: []string{"FOOD_AND_DRINK"}}, 2, []interface{}{"u"})
		assert.Contains(t, clause, "effective_pfc_primary IN (")
		assert.NotContains(t, clause, "category_override_kind",
			"no custom leg when no custom ids were requested")
	})

	t.Run("custom only", func(t *testing.T) {
		clause, _, _ := categoryFilterGroup(
			domain.TransactionFilters{CustomCategoryIDs: []string{"x"}}, 2, []interface{}{"u"})
		assert.Contains(t, clause, "category_override_kind = 'custom'")
		assert.NotContains(t, clause, "effective_pfc_primary IN (",
			"no PFC leg when no PFC values were requested")
	})

	t.Run("none", func(t *testing.T) {
		clause, args, idx := categoryFilterGroup(domain.TransactionFilters{}, 2, []interface{}{"u"})
		assert.Empty(t, clause, "no category filter must add no clause at all — never a 1=1 tautology")
		assert.Len(t, args, 1)
		assert.Equal(t, 2, idx)
	})
}

// TestBuildWhereClause_PFCAndCustomAreSatisfiableTogether is the same property at the level a
// caller actually sees.
func TestBuildWhereClause_PFCAndCustomAreSatisfiableTogether(t *testing.T) {
	whereClause, _ := buildWhereClause("user-abc", domain.TransactionFilters{
		PFCPrimaries:      []string{"FOOD_AND_DRINK"},
		CustomCategoryIDs: []string{"9f1c2b3a-0000-4000-8000-000000000001"},
	})

	// Find the conjunct FIRST and require it to exist. An assertion nested inside a loop
	// condition is vacuous in exactly the failure mode that matters: drop the PFC predicate
	// entirely and the loop body never runs, so the test passes while `pfc_primaries` is ignored.
	conjuncts := topLevelConjuncts(whereClause)
	var categoryConjunct string
	for _, c := range conjuncts {
		if strings.Contains(c, "effective_pfc_primary IN (") {
			require.Empty(t, categoryConjunct, "the PFC predicate must appear in exactly one conjunct")
			categoryConjunct = c
		}
	}
	require.NotEmpty(t, categoryConjunct,
		"the PFC predicate must be present at all — without this check, removing it would make "+
			"the assertion below unreachable and this test would pass on a broken filter. Got:\n%s",
		strings.Join(conjuncts, "\n--AND--\n"))

	assert.Contains(t, categoryConjunct, "category_override_kind = 'custom'",
		"the PFC and custom predicates must sit in the SAME conjunct, OR-joined; "+
			"ANDing them yields the empty set (#1626)")
}

// TestBothTransactionBuildersShareTheCategoryGroup is the divergence guard.
//
// This file's subject exists in TWO parallel builders: buildWhereClause and the keyset-paginated
// GetByUserIDKeysetUnified, and only the second is on the live transaction-list path. A divergence
// between them is invisible in review and in most tests, because a fix applied to the first one
// leaves user-facing behaviour untouched. Rather than assert the same SQL shape twice, this pins
// that neither builder hand-rolls the category predicates any more: both must delegate.
func TestBothTransactionBuildersShareTheCategoryGroup(t *testing.T) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "transaction_repository.go", nil, 0)
	require.NoError(t, err)

	const shared = "categoryFilterGroup"
	for _, fn := range []string{"buildWhereClause", "GetByUserIDKeysetUnified"} {
		t.Run(fn, func(t *testing.T) {
			decl := findFuncByName(t, file, fn)
			body := renderNode(t, fset, decl)

			assert.Contains(t, body, shared+"(",
				"%s must build its category predicates through %s, or the two builders drift apart "+
					"and only one of them gets fixed (#1626)", fn, shared)
			assert.NotContains(t, body, "effective_pfc_primary IN (",
				"%s must not hand-roll the PFC predicate beside the shared helper", fn)
		})
	}
}

// findFuncByName returns the named top-level func (or method) declaration.
func findFuncByName(t *testing.T, file *ast.File, name string) *ast.FuncDecl {
	t.Helper()
	for _, d := range file.Decls {
		if fn, ok := d.(*ast.FuncDecl); ok && fn.Name.Name == name {
			return fn
		}
	}
	t.Fatalf("no func named %q in transaction_repository.go", name)
	return nil
}

// renderNode returns the source text of a node, using its positions against the original file.
func renderNode(t *testing.T, fset *token.FileSet, decl *ast.FuncDecl) string {
	t.Helper()
	raw, err := os.ReadFile("transaction_repository.go")
	require.NoError(t, err)
	src := string(raw)
	start := fset.Position(decl.Pos()).Offset
	end := fset.Position(decl.End()).Offset
	require.Less(t, start, end)
	require.LessOrEqual(t, end, len(src))
	return src[start:end]
}

// TestCategoryFilterGroup_UncategorizedIsAFourthOrLeg is the #1626 Phase 2 guard.
//
// The Sankey renders an "Uncategorized" node from a sentinel the ANALYTICS layer synthesises:
// effectiveCategoryExpr's ELSE arm coalesces a NULL pfc_primary to the literal 'UNCATEGORIZED'.
// That string is not a value any column holds — effective_pfc_primary is NULL for those rows — so
// `pfc_primaries=UNCATEGORIZED` matches nothing and the node had to be inert.
//
// The predicate mirrors the sentinel's own definition: that ELSE arm is reached exactly when
// category_override_kind IS NULL, and it consults only pfc_primary. Note it deliberately does NOT
// rely on the discriminator CHECK forbidding "ghost" override columns on a kind-NULL row — under
// SQL three-valued logic that CHECK evaluates to UNKNOWN there, which Postgres accepts. The
// equivalence holds because the CASE ignores those columns, not because they cannot exist.
func TestCategoryFilterGroup_UncategorizedIsAFourthOrLeg(t *testing.T) {
	t.Run("alone", func(t *testing.T) {
		clause, args, idx := categoryFilterGroup(
			domain.TransactionFilters{Uncategorized: true}, 2, []interface{}{"u"})

		assert.Contains(t, clause, "t.category_override_kind IS NULL")
		assert.Contains(t, clause, "t.pfc_primary IS NULL")
		assert.NotContains(t, clause, "effective_pfc_primary IN (",
			"no PFC leg when no PFC values were requested")
		assert.Len(t, args, 1, "the predicate binds no parameters of its own")
		assert.Equal(t, 2, idx, "no placeholders consumed")
	})

	t.Run("false is a no-op, never an exclusion", func(t *testing.T) {
		clause, _, _ := categoryFilterGroup(
			domain.TransactionFilters{Uncategorized: false}, 2, []interface{}{"u"})
		assert.Empty(t, clause,
			"uncategorized=false must add nothing; treating it as 'exclude uncategorized' would be "+
				"a second, unrequested feature")
	})

	t.Run("unions with the other three rather than intersecting them", func(t *testing.T) {
		filters := domain.TransactionFilters{
			PFCPrimaries:      []string{"FOOD_AND_DRINK"},
			PFCDetaileds:      []string{"TRANSPORTATION_RIDE_SHARE"},
			CustomCategoryIDs: []string{"9f1c2b3a-0000-4000-8000-000000000001"},
			Uncategorized:     true,
		}
		clause, args, _ := categoryFilterGroup(filters, 2, []interface{}{"u"})

		conjuncts := topLevelConjuncts(strings.TrimPrefix(strings.TrimSpace(clause), "AND "))
		require.Len(t, conjuncts, 1,
			"all FOUR category predicates must share ONE top-level group. A row is pfc-kind, "+
				"custom-kind, or uncategorised — never two at once — so splitting them across "+
				"top-level ANDs selects the empty set, which is the #1626 part C defect. Got:\n%s",
			strings.Join(conjuncts, "\n--AND--\n"))

		for _, want := range []string{
			"effective_pfc_primary IN (",
			"effective_pfc_detailed IN (",
			"category_override_kind = 'custom'",
			"t.pfc_primary IS NULL",
		} {
			assert.Contains(t, clause, want)
		}
		require.Len(t, args, 4, "user_id plus one arg per bound value; the uncategorized leg binds none")
	})
}
