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

	// The write rule (plan lines 489-493). It was dropped from T8's first pass and the note in
	// transaction_repository.go that DOCUMENTED the hole was edited in the same commit.

	t.Run("an UPDATE target with no predicate is an offender", func(t *testing.T) {
		got := tombstoneWriteOffenders(`UPDATE transactions SET deleted_at = NOW() WHERE id = $1`, tables)
		want := []string{"write:transactions"}
		if !equalStrings(got, want) {
			t.Fatalf("an UPDATE target is still invisible:\n  got  %v\n  want %v", got, want)
		}
	})

	t.Run("a DELETE target with no predicate is an offender", func(t *testing.T) {
		got := tombstoneWriteOffenders(`DELETE FROM transactions WHERE id = $1`, tables)
		want := []string{"write:transactions"}
		if !equalStrings(got, want) {
			t.Fatalf("a DELETE target is still invisible:\n  got  %v\n  want %v", got, want)
		}
	})

	t.Run("an aliased UPDATE target needs its own alias-qualified predicate", func(t *testing.T) {
		guarded := `UPDATE transactions t SET x = 1 WHERE t.id = $1 AND t.deleted_at IS NULL`
		if got := tombstoneWriteOffenders(guarded, tables); len(got) != 0 {
			t.Fatalf("a guarded UPDATE target was reported: %v", got)
		}
		other := `UPDATE transactions t SET x = 1 FROM accounts a WHERE a.deleted_at IS NULL`
		if got := tombstoneWriteOffenders(other, tables); !equalStrings(got, []string{"write:transactions t"}) {
			t.Fatalf("another table's predicate satisfied the write rule: %v", got)
		}
	})

	// THE READ KEY SPACE AND THE WRITE KEY SPACE ARE SEPARATE (#1569 whole-branch review I4).
	//
	// They used to be one. `tombstone-exempt(transactions)` therefore excused the statement's
	// UPDATE/DELETE target AND every unfiltered read reference to the same table, in either
	// direction, however narrow the reason underneath it was. Measured on a live statement:
	// transaction_repository.go's tombstoning UPDATE carries a token whose reason is explicitly
	// about the WRITE target, and injecting a wholly unfiltered
	// `EXISTS (SELECT 1 FROM transactions ...)` read into it left all three guards green. The
	// per-reference fix of T8 closed the ALIASED case only because an alias happens to differ
	// between the two roles; twelve live tokens were unaliased and blanketed both.
	//
	// A write target is now named `write:<table>[ <alias>]`. The two remedies differ — a read
	// forgetting the filter shows a deleted row, a write forgetting it mutates one — so an author
	// who considered one has not thereby considered the other.
	t.Run("a named write exemption covers the write target", func(t *testing.T) {
		sql := `-- tombstone-exempt(write:transactions): the purge finishes what the tombstone deferred.
			DELETE FROM transactions WHERE id = $1`
		if got := tombstoneWriteOffenders(sql, tables); len(got) != 0 {
			t.Fatalf("named write exemption not honoured: %v", got)
		}
	})

	t.Run("a READ exemption does not cover the write target", func(t *testing.T) {
		sql := `-- tombstone-exempt(transactions): this READ of the tombstone listing is deliberate.
			UPDATE transactions SET reviewed = true WHERE id = $1`
		got := tombstoneWriteOffenders(sql, tables)
		want := []string{"write:transactions"}
		if !equalStrings(got, want) {
			t.Fatalf("a read-scoped exemption still blankets the write target:\n  got  %v\n  want %v",
				got, want)
		}
	})

	t.Run("a WRITE exemption does not cover an unfiltered read", func(t *testing.T) {
		// UNALIASED on purpose: this is the shape the old shared key space could not tell apart.
		// With one key space `tombstone-exempt(transactions)` excused both roles; with two, a
		// write-scoped token leaves the read reference reported.
		sql := `-- tombstone-exempt(write:transactions): tombstoning IS the write.
			UPDATE transactions SET deleted_at = NOW()
			WHERE id = $1 AND EXISTS (SELECT 1 FROM transactions WHERE user_id = $2)`
		got := tombstoneReadOffenders(sql, tables, deleteRef)
		want := []string{"transactions"}
		if !equalStrings(got, want) {
			t.Fatalf("a write-scoped exemption still blankets a read reference:\n  got  %v\n  want %v",
				got, want)
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

// TestEveryTombstoneExemptTokenNamesItsReference rejects the old bare form outright, rejects a
// parenthesised token that names a table no guard audits (a typo exempts nothing, silently), and
// rejects a DEAD exemption — one whose named reference does not appear anywhere in the enclosing
// statement.
//
// The dead-exemption rule matters because an exemption that names nothing is indistinguishable
// from one that names the wrong thing, and both read as "this was considered". The check is done
// at STATEMENT scope rather than per string literal, because a query is assembled from fragments
// and the fragment carrying the reason is not always the fragment carrying the FROM.
func TestEveryTombstoneExemptTokenNamesItsReference(t *testing.T) {
	root := repoInternalRoot(t)

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

			if namedExemptIsBare(s, "tombstone-exempt", tombstoneExemptToken) {
				offenders = append(offenders,
					at+" uses `tombstone-exempt` in a form other than "+
						"`tombstone-exempt(<table>[ <alias>], ...):`, which excuses nothing")
			}
			for _, m := range tombstoneExemptToken.FindAllStringSubmatch(s, -1) {
				for _, ref := range strings.Split(m[1], ",") {
					fields := strings.Fields(strings.ToLower(strings.TrimSpace(ref)))
					if len(fields) == 0 || len(fields) > 2 {
						offenders = append(offenders,
							at+" exemption target "+strconv.Quote(ref)+" is not "+
								"`[write:]<table>[ <alias>]`")
						continue
					}
					// `write:` scopes the exemption to the statement's UPDATE/DELETE TARGET; without
					// it the token excuses read references only (#1569 whole-branch review I4). A
					// near-miss prefix (`writes:`, `w:`) must be reported rather than read as a
					// table name, or a typo silently re-opens the write rule.
					table := strings.TrimPrefix(fields[0], "write:")
					if table != fields[0] && table == "" {
						offenders = append(offenders,
							at+" exemption target "+strconv.Quote(ref)+" names no table after `write:`")
						continue
					}
					if !audited[table] {
						offenders = append(offenders,
							at+" exempts "+strconv.Quote(table)+", which no tombstone guard audits")
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

// TestNoDeadTombstoneExemption reports an exemption whose named reference is nowhere in its
// statement. Statement scope, and over the UNION of every audited table, so an exemption written
// for one guard is not called dead by another.
func TestNoDeadTombstoneExemption(t *testing.T) {
	root := repoInternalRoot(t)
	all := allTombstoneAuditedTables
	readRef := tombstoneReadRefRegexp(all)
	writeRef := tombstoneWriteTargetRegexp(all)

	var offenders []string
	statements := 0

	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() && info.Name() == "testdata" {
			return filepath.SkipDir
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
			switch n.(type) {
			case *ast.AssignStmt, *ast.ValueSpec, *ast.ReturnStmt, *ast.ExprStmt:
			default:
				return true
			}
			var parts []string
			ast.Inspect(n, func(m ast.Node) bool {
				lit, ok := m.(*ast.BasicLit)
				if ok && lit.Kind == token.STRING {
					if str, e := strconv.Unquote(lit.Value); e == nil {
						parts = append(parts, str)
					}
				}
				return true
			})
			sql := strings.Join(parts, "\n")
			exempt := tombstoneExemptRefs(sql)
			if len(exempt) == 0 {
				return true
			}
			statements++

			// READ references and WRITE targets live in separate key spaces (#1569 whole-branch
			// review I4), so deadness is judged per space: a `write:` token is dead unless the
			// statement really has that write target, and a plain token is dead unless it really
			// has that read reference. Judging both against one merged set would call a token
			// alive because the OTHER role happens to name the same table — which is precisely the
			// conflation the prefix exists to remove.
			present := map[string]bool{}
			mark := func(prefix, table, alias string) {
				if sqlKeywordsAfterTableName[strings.ToLower(alias)] {
					alias = ""
				}
				present[prefix+strings.ToLower(table)] = true
				if alias != "" {
					present[prefix+strings.ToLower(table+" "+alias)] = true
				}
			}
			code := stripSQLComments(sql)
			for _, m := range readRef.FindAllStringSubmatch(code, -1) {
				mark("", m[len(m)-2], m[len(m)-1])
			}
			for _, m := range writeRef.FindAllStringSubmatch(code, -1) {
				mark("write:", m[len(m)-2], m[len(m)-1])
			}
			line := fset.Position(n.Pos()).Line
			for ref := range exempt {
				if !present[ref] {
					offenders = append(offenders,
						rel+":"+strconv.Itoa(line)+" exempts "+strconv.Quote(ref)+
							", which is not a reference in that statement")
				}
			}
			return false
		})
		return nil
	})
	if err != nil {
		t.Fatalf("walking internal/: %v", err)
	}
	if statements < 20 {
		t.Fatalf("only %d statement(s) carry an exemption; there were 28 when this was written. "+
			"The walk is broken and a pass here would be vacuous", statements)
	}

	if len(offenders) > 0 {
		sort.Strings(offenders)
		t.Fatalf("%d dead tombstone exemption(s) (#1569 T8). An exemption that names nothing reads "+
			"as \"this was considered\" and excuses nothing. Delete it, or fix the name:\n  %s",
			len(offenders), strings.Join(offenders, "\n  "))
	}
}
