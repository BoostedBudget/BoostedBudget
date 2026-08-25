package repository

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// Issue #1569 T8 — the exemption is PER REFERENCE, and until this file it was not.
//
// tombstoneReadOffenders used to open with `if strings.Contains(sql, "tombstone-exempt:")
// { return nil }`. One token anywhere in a statement therefore excused EVERY reference in it,
// including references written years later by someone who never read the reason. That is not a
// theoretical hole: injecting a second, wholly unfiltered `transactions unguarded_probe` into
// UpdateCategory — which carries a token about category-name resolution, nothing to do with
// transactions — left TestNoTransactionReadForgetsTheTombstoneFilter GREEN.
//
// The token now has to NAME what it excuses: `tombstone-exempt(<table>[ <alias>], ...): <reason>`.
// A bare `tombstone-exempt:` excuses nothing, and is itself reported by
// TestEveryTombstoneExemptTokenNamesItsReference, so the old form cannot come back quietly.
//
// TestTombstoneExemptionIsPerReference below is the meta-test: it builds the exact violation and
// asserts the checker reports it. It fails if the blanket short-circuit is ever restored.

func TestTombstoneExemptionIsPerReference(t *testing.T) {
	deleteRef := regexp.MustCompile(`(?is)\bDELETE\s+FROM\s+transactions\b`)
	tables := []string{"transactions"}

	t.Run("a named exemption excuses only the reference it names", func(t *testing.T) {
		sql := `SELECT 1
			FROM transactions prior
			-- tombstone-exempt(transactions prior): the pre-image is deliberately visible.
			JOIN transactions unguarded_probe ON unguarded_probe.id = prior.id`

		got := tombstoneReadOffenders(sql, tables, deleteRef)
		want := []string{"transactions unguarded_probe"}
		if !equalStrings(got, want) {
			t.Fatalf("per-reference exemption leaked:\n  got  %v\n  want %v", got, want)
		}
	})

	t.Run("a bare token excuses nothing", func(t *testing.T) {
		sql := `SELECT 1
			FROM transactions prior
			-- tombstone-exempt: a reason that names no reference.
			JOIN transactions unguarded_probe ON unguarded_probe.id = prior.id`

		got := tombstoneReadOffenders(sql, tables, deleteRef)
		want := []string{"transactions prior", "transactions unguarded_probe"}
		if !equalStrings(got, want) {
			t.Fatalf("bare token still blanket-exempts:\n  got  %v\n  want %v", got, want)
		}
	})

	t.Run("an unaliased reference is named by its table alone", func(t *testing.T) {
		sql := `SELECT 1 FROM transactions
			-- tombstone-exempt(transactions): the listing of tombstones itself.
			WHERE user_id = $1`

		if got := tombstoneReadOffenders(sql, tables, deleteRef); len(got) != 0 {
			t.Fatalf("unaliased exemption not honoured: %v", got)
		}
	})

	// Two further holes closed in the same pass, both of which had live instances.

	t.Run("prose inside an SQL comment is not a predicate", func(t *testing.T) {
		sql := `SELECT 1
			FROM transactions prior
			-- Adding prior.deleted_at IS NULL here would narrow WHICH ROWS THE UPDATE AFFECTS.
			WHERE prior.id = $1`

		got := tombstoneReadOffenders(sql, tables, deleteRef)
		want := []string{"transactions prior"}
		if !equalStrings(got, want) {
			t.Fatalf("a comment EXPLAINING the absent predicate was accepted as the predicate:\n"+
				"  got  %v\n  want %v", got, want)
		}
	})

	t.Run("an aliased reference in a comma join list is a reference", func(t *testing.T) {
		sql := `UPDATE transactions AS twin SET x = 1
			FROM tombstoned s, transactions prior
			WHERE prior.id = twin.id AND twin.deleted_at IS NULL`

		got := tombstoneReadOffenders(sql, tables, deleteRef)
		want := []string{"transactions prior"}
		if !equalStrings(got, want) {
			t.Fatalf("comma join list still invisible:\n  got  %v\n  want %v", got, want)
		}
	})

	t.Run("a comma before a qualified column is not a reference", func(t *testing.T) {
		sql := `INSERT INTO transactions (id) VALUES ($1)
			ON CONFLICT (id) DO UPDATE SET name = COALESCE(EXCLUDED.name, transactions.name)`

		if got := tombstoneReadOffenders(sql, tables, deleteRef); len(got) != 0 {
			t.Fatalf("qualified column read as a join-list reference: %v", got)
		}
	})

	t.Run("an exemption naming a different table does not cover this one", func(t *testing.T) {
		sql := `SELECT 1
			FROM transactions t
			-- tombstone-exempt(custom_categories cc): historical name resolution.
			JOIN custom_categories cc ON cc.id = t.custom_category_override_id`

		got := tombstoneReadOffenders(sql, tables, deleteRef)
		want := []string{"transactions t"}
		if !equalStrings(got, want) {
			t.Fatalf("cross-table exemption leaked:\n  got  %v\n  want %v", got, want)
		}
	})
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	x, y := append([]string(nil), a...), append([]string(nil), b...)
	sort.Strings(x)
	sort.Strings(y)
	for i := range x {
		if x[i] != y[i] {
			return false
		}
	}
	return true
}

// allTombstoneAuditedTables is the union of every table the three tombstone read-guards audit. The
// token check below needs the union because one statement can carry exemptions for tables belonging
// to different guards, and a token naming a table NO guard audits is a typo that would silently
// excuse nothing.
var allTombstoneAuditedTables = func() []string {
	seen := map[string]bool{}
	var out []string
	for _, group := range [][]string{
		transactionTombstoneTables, configTombstoneTables, customCategoryTombstoneTables,
	} {
		for _, tbl := range group {
			if !seen[tbl] {
				seen[tbl] = true
				out = append(out, tbl)
			}
		}
	}
	sort.Strings(out)
	return out
}()

// TestEveryTombstoneExemptTokenNamesItsReference rejects the old bare form outright, and rejects a
// parenthesised token that names a table no guard audits (a typo exempts nothing, silently).
//
// It deliberately does NOT check that the named reference exists in the statement: a query is
// assembled from fragments, and the fragment carrying the reason is not always the fragment
// carrying the FROM.
func TestEveryTombstoneExemptTokenNamesItsReference(t *testing.T) {
	root := repoInternalRoot(t)

	bare := regexp.MustCompile(`tombstone-exempt\s*:`)
	audited := map[string]bool{}
	for _, tbl := range allTombstoneAuditedTables {
		audited[tbl] = true
	}

	var offenders []string

	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		fset := token.NewFileSet()
		file, parseErr := parser.ParseFile(fset, path, nil, 0)
		if parseErr != nil {
			return parseErr
		}
		rel, _ := filepath.Rel(root, path)

		ast.Inspect(file, func(n ast.Node) bool {
			lit, ok := n.(*ast.BasicLit)
			if !ok || lit.Kind != token.STRING {
				return true
			}
			s, e := strconv.Unquote(lit.Value)
			if e != nil || !strings.Contains(s, "tombstone-exempt") {
				return true
			}
			at := rel + ":" + strconv.Itoa(fset.Position(lit.Pos()).Line)

			if bare.MatchString(s) {
				offenders = append(offenders,
					at+" uses the bare `tombstone-exempt:` form, which excuses nothing")
			}
			for _, m := range tombstoneExemptToken.FindAllStringSubmatch(s, -1) {
				for _, ref := range strings.Split(m[1], ",") {
					fields := strings.Fields(strings.ToLower(strings.TrimSpace(ref)))
					if len(fields) == 0 || len(fields) > 2 {
						offenders = append(offenders,
							at+" exemption target "+strconv.Quote(ref)+" is not `<table>[ <alias>]`")
						continue
					}
					if !audited[fields[0]] {
						offenders = append(offenders,
							at+" exempts "+strconv.Quote(fields[0])+", which no tombstone guard audits")
					}
				}
			}
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatalf("walking internal/: %v", err)
	}

	if len(offenders) > 0 {
		t.Fatalf(
			"%d malformed tombstone exemption(s) (#1569 T8). The token must NAME what it excuses:\n"+
				"  -- tombstone-exempt(<table>[ <alias>], ...): <reason>\n"+
				"A bare token blankets every reference in the statement, including ones added later:\n  %s",
			len(offenders), strings.Join(offenders, "\n  "))
	}
}
