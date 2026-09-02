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

// Issue #1569 round 11 task 3 — guard: nothing UPDATEs user_accounts.identity_type or
// identity_subtype.
//
// WHAT #1569 ROUND 11 TASK 3 DECIDED. identity_type/identity_subtype are the FROZEN identity of a
// user_accounts chain — distinct from primary_type/primary_subtype, which are explicitly
// documented "from most recent Plaid link" and ARE overwritten on every re-link
// (UserAccountRepository.UpdatePrimaryTypeAndSubtype). The whole point of adding a second,
// non-overwritten pair of columns is that nothing ever rewrites them after mint
// (UserAccountRepository.Create, database/backfill.go's synthesiseUserAccountID). There is
// deliberately no UpdateIdentity method and no plan to add one — a future task changes the MATCH
// KEY to use these columns, and a key that can be silently rewritten by any UPDATE path is not a
// frozen key.
//
// THE RULE. In non-test Go under backend/internal, no `UPDATE user_accounts SET ...` statement may
// mention identity_type or identity_subtype anywhere in its SET clause (or anywhere else in the
// statement's text, since a legitimate write has no reason to reference them at all). Unlike G2
// (history deletes), there is NO allowlist here: an UPDATE of these columns is not a decision this
// branch is prepared to bless under any reason, so the guard is unconditional — any hit is a
// failure to fix, not a candidate for an entry.
//
// WHY NO ALLOWLIST, UNLIKE G2. G2's hard deletes have five legitimate exceptions (explicit user
// purges) that must stay expressible. identity_type/identity_subtype have zero: the entire feature
// is "written once, at mint, never rewritten" (see domain.UserAccount), and the moment a legitimate
// reason to rewrite them exists, the columns are no longer an identity key and the feature this
// migration exists for (000177_add_user_accounts_identity_columns.up.sql) needs to be rethought,
// not allowlisted around.
//
// WHY THIS IS SOUNDLY DERIVABLE FROM SOURCE TEXT (unlike a guard the brief warns against shipping).
// "Does this UPDATE statement's own text mention this column name" is a property of the literal SQL
// a repository method writes — it does not require inferring intent, tracking data flow between
// functions, or reasoning about what a column MEANS. It is exactly the kind of check G1/G2 already
// rely on for hard deletes; the only new part is scanning UPDATE instead of DELETE/TRUNCATE.
//
// WHAT THIS GUARD DOES NOT COVER, ON PURPOSE. A future raw-SQL write path outside backend/internal
// Go (a psql script, a different service in a different repo) is invisible to an AST scan of this
// tree. That gap is why migration 000177 ALSO adds a BEFORE UPDATE trigger
// (trg_user_accounts_identity_immutable) at the database level — see that migration's header
// comment for why the trigger is the DB-level half of this same decision, and why it is created
// only after the migration's own one-time backfill UPDATE, so the historical correction (NULL ->
// value) is not itself blocked by the guarantee it is about to start enforcing.
//
// Mutation-checked: see TestUserAccountsUpdateStatements_FlagsIdentityColumnInSetClause and
// TestUserAccountsUpdateStatements_DoesNotFlagAnUnrelatedUpdate for the constructed cases, and
// the r11-task3-report.md for the real mutation — a temporary `identity_type = $x` added to
// UpdateHidden's SQL — that turns TestNoUpdateOfUserAccountsIdentityColumns red naming that method.

// userAccountsUpdateRegexp finds the start of an `UPDATE user_accounts` statement in normalised
// (upper-cased, comment- and literal-stripped, whitespace-collapsed) SQL text. normaliseSQL and its
// helpers (sqlBlockCommentRegexp, sqlLineCommentRegexp, sqlStringLiteralRegexp, sqlDollarQuoteRegexp,
// sqlWhitespaceRegexp) are shared with history_delete_guard_test.go — one notion of "clean SQL text
// to scan", not two.
var userAccountsUpdateRegexp = regexp.MustCompile(`\bUPDATE\s+USER_ACCOUNTS\b`)

// userAccountsUpdateBoundaryRegexp ends one UPDATE user_accounts statement's slice at whichever
// comes first: the start of another UPDATE (of any table), or a semicolon. Same shape as G2's
// statementBoundaryRegexp and for the same reason (#1569 T8.2): running to the next occurrence of
// the SAME keyword only, or to end-of-string, would let a later, unrelated statement in the same Go
// string literal lend its text to this one's scope.
var userAccountsUpdateBoundaryRegexp = regexp.MustCompile(`\bUPDATE\b|;`)

// userAccountsUpdateStatements returns the normalised text of each `UPDATE user_accounts` statement
// found in sql, one entry per occurrence.
func userAccountsUpdateStatements(sql string) []string {
	norm := normaliseSQL(sql)
	var out []string
	for _, m := range userAccountsUpdateRegexp.FindAllStringIndex(norm, -1) {
		end := len(norm)
		if next := userAccountsUpdateBoundaryRegexp.FindStringIndex(norm[m[1]:]); next != nil {
			end = m[1] + next[0]
		}
		out = append(out, norm[m[0]:end])
	}
	return out
}

// userAccountsIdentityUpdateSite is one place the scan found an `UPDATE user_accounts` statement:
// the enclosing function's line and the statement's own normalised text.
type userAccountsIdentityUpdateSite struct {
	line      string
	statement string
}

// scanUserAccountsUpdates walks non-test Go under root and returns every `UPDATE user_accounts`
// statement found, keyed by "<rel>#<func>". Literals are grouped per assembly unit via
// literalUnitsInFunc (shared with history_delete_guard_test.go), so a help string or an unrelated
// second query elsewhere in the same function cannot lend text into a statement's scope — the same
// protection G2 relies on (#1569 T8.2 Item B).
func scanUserAccountsUpdates(t *testing.T, root string) map[string][]userAccountsIdentityUpdateSite {
	t.Helper()
	found := map[string][]userAccountsIdentityUpdateSite{}

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
		rel = filepath.ToSlash(rel)

		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Body == nil {
				continue
			}
			for _, unit := range literalUnitsInFunc(fset, fn.Body) {
				for _, stmt := range userAccountsUpdateStatements(unit) {
					key := rel + "#" + fn.Name.Name
					found[key] = append(found[key], userAccountsIdentityUpdateSite{
						line:      strconv.Itoa(fset.Position(fn.Pos()).Line),
						statement: stmt,
					})
				}
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walking internal/: %v", err)
	}
	return found
}

// TestNoUpdateOfUserAccountsIdentityColumns is the guard itself.
func TestNoUpdateOfUserAccountsIdentityColumns(t *testing.T) {
	root := repoInternalRoot(t)
	found := scanUserAccountsUpdates(t, root)

	// Fail closed: user_account_repository.go has at least three UPDATE user_accounts statements
	// today (Update, UpdatePrimaryTypeAndSubtype, UpdateHidden). A scan that finds none means the
	// walk itself is broken and this guard would pass vacuously.
	if len(found) == 0 {
		t.Fatalf("scanned %s and found no `UPDATE user_accounts` statement at all, but the "+
			"repository is known to have several — the scan is broken and would pass vacuously", root)
	}

	var offenders []string
	for key, sites := range found {
		for _, s := range sites {
			if strings.Contains(s.statement, "IDENTITY_TYPE") || strings.Contains(s.statement, "IDENTITY_SUBTYPE") {
				offenders = append(offenders, key+" (func at line "+s.line+"): "+s.statement)
			}
		}
	}
	sort.Strings(offenders)

	if len(offenders) > 0 {
		t.Errorf("%d UPDATE user_accounts statement(s) touch identity_type/identity_subtype "+
			"(#1569 round 11 task 3).\n"+
			"These columns are the FROZEN identity of a re-link chain — written once at mint "+
			"(UserAccountRepository.Create, database/backfill.go's synthesiseUserAccountID) and "+
			"NEVER rewritten. There is no allowlist for this guard: an UPDATE of these columns "+
			"means the columns are no longer a frozen key, which is a design problem to fix, not a "+
			"reason to add here. If you need to change type/subtype after mint, write to "+
			"primary_type/primary_subtype instead (UpdatePrimaryTypeAndSubtype) — that pair is "+
			"the intentionally-mutable one.\n    %s",
			len(offenders), strings.Join(offenders, "\n    "))
	}
}

// TestUserAccountsUpdateStatements_FlagsIdentityColumnInSetClause is a constructed-violation check
// on the extraction/detection mechanism itself, independent of the live repository file.
func TestUserAccountsUpdateStatements_FlagsIdentityColumnInSetClause(t *testing.T) {
	sql := `UPDATE user_accounts SET identity_type = $1, updated_at = $2 WHERE id = $3`
	stmts := userAccountsUpdateStatements(sql)
	if len(stmts) != 1 {
		t.Fatalf("expected exactly 1 statement, got %d: %v", len(stmts), stmts)
	}
	if !strings.Contains(stmts[0], "IDENTITY_TYPE") {
		t.Fatalf("expected the extracted statement to contain IDENTITY_TYPE, got %q", stmts[0])
	}
}

// TestUserAccountsUpdateStatements_DoesNotFlagAnUnrelatedUpdate proves the guard does not fire on
// the legitimate UPDATE statements this repository actually ships today — a guard that flags
// legitimate code gets deleted by the next engineer (see the brief this task was dispatched under).
func TestUserAccountsUpdateStatements_DoesNotFlagAnUnrelatedUpdate(t *testing.T) {
	sqls := []string{
		`UPDATE user_accounts
			SET nickname = $1, custom_color = $2, custom_icon = $3,
			    custom_emoji = $4, display_order = $5, is_hidden = $6, updated_at = $7
			WHERE id = $8 AND user_id = $9`,
		`UPDATE user_accounts
			SET primary_type = $1, primary_subtype = $2, updated_at = $3
			WHERE id = $4 AND user_id = $5`,
		`UPDATE user_accounts
			SET is_hidden = $1, updated_at = $2
			WHERE id = $3 AND user_id = $4`,
	}
	for _, sql := range sqls {
		for _, stmt := range userAccountsUpdateStatements(sql) {
			if strings.Contains(stmt, "IDENTITY_TYPE") || strings.Contains(stmt, "IDENTITY_SUBTYPE") {
				t.Fatalf("legitimate UPDATE incorrectly flagged as touching identity columns: %q", stmt)
			}
		}
	}
}

// TestUserAccountsUpdateStatements_StopsAtTheNextUpdate proves the statement boundary works: text
// belonging to a SECOND, unrelated UPDATE (even one that itself mentions identity_type) must not be
// attributed to the FIRST statement's slice.
func TestUserAccountsUpdateStatements_StopsAtTheNextUpdate(t *testing.T) {
	sql := `UPDATE user_accounts SET is_hidden = $1 WHERE id = $2; UPDATE some_other_table SET identity_type = $1`
	stmts := userAccountsUpdateStatements(sql)
	if len(stmts) != 1 {
		t.Fatalf("expected exactly 1 UPDATE user_accounts statement, got %d: %v", len(stmts), stmts)
	}
	if strings.Contains(stmts[0], "IDENTITY_TYPE") {
		t.Fatalf("statement boundary leaked text from a later, unrelated UPDATE: %q", stmts[0])
	}
}
