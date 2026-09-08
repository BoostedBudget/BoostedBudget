package repository

import (
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

// Issue #1609, Item 3 — the mechanical half of "customization is read from the chain".
//
// THE RULE. In non-test Go under backend/internal, a SELECT span that names one of the six
// customization columns QUALIFIED BY AN ALIAS BOUND TO `accounts` must render them through
// accountCustomizationProjection, and the SQL literal containing it must carry
// accountCustomizationJoin for that same alias.
//
// WHY A GUARD RATHER THAN TRUST, and it is the same argument as its two siblings in this package.
// The defect is invisible by construction: a stale nickname is a well-formed string in the right
// column, a stale colour renders perfectly, and a stale is_hidden is just a boolean. Nothing about
// any of them looks wrong in a review, in a response body, or on screen — they are only wrong in
// the sense that they are not what the user last asked for. FIVE reads carried this defect
// simultaneously and it survived every review each of them had; a sixth read added tomorrow would
// carry it too, because the raw column is the obvious thing to type.
//
// WHY IT IS EXACT AND CANNOT DRIFT. This test lives in package `repository`, so it CALLS
// accountCustomizationProjection and accountCustomizationJoin and strips their own returned text.
// There is no restatement to drift — which is the hole
// account_status_projection_guard_test.go's own header confesses to in its hand-written
// accountBalanceProjectionFragment regex, and the reason this guard does not repeat it.
//
// PER SELECT SPAN, not per file and not per function. The same lesson the tombstone and balance
// guards learned: an aggregated check is satisfied by a NEIGHBOURING compliant query, which is
// exactly the bug.
//
// SCOPE — ALIASES BOUND TO `accounts`, and nothing else. The joined `user_accounts <a>_ua` is the
// SOURCE of the overlay, so its own columns are not offences; `_` is a word character, so
// `\baccounts` does not match inside `user_accounts` and the reference scanner never binds the
// joined alias in the first place.
//
// WHAT IT CANNOT SEE, stated rather than discovered later:
//
//  1. `SELECT *` / `SELECT a.*`. The select list never names a column, so nothing matches. There is
//     no such account read in the tree; a star projection would bring the RAW accounts columns
//     along, so this is a genuine hole rather than merely a blind spot. Pinned as a hole below.
//  2. An UNQUALIFIED column over an unaliased `FROM accounts`. The rule is deliberately keyed on
//     the qualifier, because a bare `nickname` in a query over `user_accounts` is correct and a
//     bare `nickname` over `accounts` is not, and only the qualifier tells them apart. All five
//     account reads are aliased as of this change, precisely so the guard can see them.
//  3. Whether the CASE's predicate is RIGHT — only that the fragment is verbatim. The behavioural
//     half is account_customization_overlay_integration_test.go, against real Postgres.
//  4. ⚠ WAS a hole, CLOSED in fix round 1 (F2), and recorded because the closing is the point.
//     Everything above judges SELECT LISTS only, so reverting A1's filter to
//     `AND a.is_hidden = FALSE` or its ordering to `COALESCE(a.display_order, 999999)` left the
//     projection and the join intact and the guard green — producing the "worst of both" state
//     accountHiddenExpr's own comment warns about, and doing it to the one read whose
//     requirements single those two clauses out. customizationOverlayOffences now has a second
//     half for exactly that, keyed on internal consistency: a literal that overlays an alias may
//     not read that alias's raw columns anywhere else in the statement. Mutation-checked against
//     real production code — reverting BOTH clauses in
//     GetAccountsWithInstitutionFiltered turns this red naming both lines.
//     What is STILL not seen: an alias in a file that overlays NOTHING (judged by the select-list
//     rule alone), and a predicate assembled by fmt.Sprintf or a helper the folder cannot
//     evaluate, which folds to the unknown-fragment placeholder.
//
// Mutation-checked, three times, each against real production code and each observed red on its
// own assertion: (1) restoring `a.nickname, a.custom_color, ...` at
// GetAccountsWithInstitutionFiltered; (2) dropping the join from GetByIDAndUser while keeping its
// projection; (3) reverting A1's hidden filter AND its ORDER BY while leaving projection and join
// intact. See task-3b-report.md for what each printed.

// accountCustomizationColumnRe matches any of the six columns carrying the given qualifier.
func accountCustomizationColumnRe(qualifier string) *regexp.Regexp {
	return regexp.MustCompile(`(?i)\b` + regexp.QuoteMeta(qualifier) + `\.(?:` +
		strings.Join(accountCustomizationColumns, "|") + `)\b`)
}

// customizationOverlayException excuses one deliberate non-overlay read. Entries carry a written
// reason, following history_delete_guard_test.go's rule that an allowlist without reasons becomes a
// list of things nobody remembers deciding.
//
// ⚠ A reason is prose and nothing checks that it is TRUE — the same caveat CLAUDE.md records for
// the cascade allowlist. Re-derive before relying on one.
type customizationOverlayException struct {
	// File is the path relative to backend/internal.
	File string
	// Marker is a substring of the SQL literal that identifies the query. Keyed on the SQL rather
	// than the file so that a compliant neighbour in the same file is still judged.
	Marker string
	Reason string
}

var customizationOverlayExceptions = []customizationOverlayException{
	{
		File:   "repository/account_repository.go",
		Marker: "JOIN household_accounts ha",
		Reason: "householdScopeQuery. is_hidden is the OWNER'S PERSONAL-VIEW control and must not " +
			"travel into a household view — see householdGrantsCTE in analytics_repository.go, which " +
			"records the ruling (Issue #1369 decision 3, migration 000110's documented contract). " +
			"Overlaying it here would be a privacy regression, not a completion. ⚠ This entry does " +
			"not fire today: the query projects NONE of the six. It is here so that the next person " +
			"who adds one finds the ruling instead of the overlay.",
	},
	{
		File:   "database/backfill.go",
		Marker: "WHERE a.user_account_id IS NULL",
		Reason: "The startup user_account repair, and it reads a.is_hidden in the OPPOSITE " +
			"direction to everything else here: its WHERE clause is `a.user_account_id IS NULL`, " +
			"so by construction this row has NO chain, and `accounts` is the only place its hide " +
			"can be. The value is being LIFTED onto the user_accounts row the repair is about to " +
			"attach (carryLegacyHideForward, #1609 task 3a). The overlay is therefore a SEMANTIC " +
			"NO-OP on this query — every row it returns takes the CASE's ELSE leg and yields " +
			"a.is_hidden unchanged — so requiring it would buy nothing and would add a pointless " +
			"join dependency to a repair scan that runs on every boot. " +
			"⚠ CORRECTED in fix round 1 (F1): this reason previously claimed the overlay 'would " +
			"read NULL and destroy the hide'. That was FALSE — NULL is what COALESCE would have " +
			"done, and the whole point of the CASE is that it does not. CLAUDE.md records that " +
			"two false allowlist reasons have already shipped on this branch and that no guard " +
			"checks a reason is true; this one is corrected rather than left as the third.",
	},
	{
		File:   "repository/user_account_repository.go",
		Marker: "FROM user_accounts",
		Reason: "user_accounts IS the source of truth these reads overlay FROM; overlaying it onto " +
			"itself is meaningless. ⚠ This entry does not fire today either — those queries carry no " +
			"`accounts` reference at all, so the scanner binds no alias — and is recorded so the " +
			"question is answered rather than re-litigated.",
	},
}

func (e customizationOverlayException) matches(rel, sql string) bool {
	return e.File == rel && strings.Contains(sql, e.Marker)
}

// customizationOverlayOffences judges ONE SQL literal and is the SINGLE definition of the rule.
//
// ⚠ THE WALKER AND THE META-TESTS BOTH CALL THIS. The first draft had the meta-tests
// re-implement the alias-scan / strip / match loop by hand, which meant nine subtests were
// validating a COPY — structurally the same drift hole this file's header criticises the status
// guard for, one level down. Two branches (the allowlist, and projection-without-join) were
// exercised by NO meta-test at all as a direct result. Anything added here is therefore
// automatically under meta-test, and a meta-test that passes is a statement about the code the
// walker runs.
//
// rel is the path relative to backend/internal and is used ONLY for allowlist matching; pass ""
// for a synthetic case, which no allowlist entry can match. Returned offences carry no file or
// line — the walker prefixes those — and each begins with a space so the prefix reads naturally.
//
// The int is the number of overlaid projections seen, which feeds the fail-closed floor.
// overlaidAliasesIn returns the accounts aliases whose projection this literal renders.
func overlaidAliasesIn(sql string) map[string]bool {
	out := map[string]bool{}
	for _, alias := range accountsAliasesIn(sql) {
		if strings.Contains(sql, accountCustomizationProjection(alias)) {
			out[alias] = true
		}
	}
	return out
}

func customizationOverlayOffences(rel, sql string, overlaidInFile map[string]bool) ([]string, int) {
	aliases := accountsAliasesIn(sql)
	// ⚠ NOT `len(aliases) == 0 -> return`. The optional-predicate literal this guard's second
	// half exists to catch (`" AND a.is_hidden = FALSE"`, built as its own Go string) binds no
	// alias of its own — an early return on that basis is precisely what let mutant 3's WHERE
	// revert through while its ORDER BY revert was caught. Measured, not reasoned.
	if len(aliases) == 0 && len(overlaidInFile) == 0 {
		return nil, 0
	}
	excused := false
	for _, e := range customizationOverlayExceptions {
		if e.matches(rel, sql) {
			excused = true
			break
		}
	}

	var offences []string
	guarded := 0
	overlaidAliases := map[string]bool{}

	for _, m := range accountProjectionSelect.FindAllStringSubmatch(sql, -1) {
		list := m[1]
		// Strip the compliant form — the renderer's OWN output, for every alias in play — before
		// looking for a raw column. What remains is by definition not rendered through it.
		stripped := list
		for _, alias := range aliases {
			stripped = strings.ReplaceAll(stripped, accountCustomizationProjection(alias), " ")
		}
		for _, alias := range aliases {
			if strings.Contains(list, accountCustomizationProjection(alias)) {
				guarded++
				overlaidAliases[alias] = true
				// The projection is useless without its join: a_ua would be an unknown alias and
				// the query would not even parse, but a HALF-edited literal (join added to one,
				// projection to another) is exactly the shape a partial refactor leaves behind.
				if !strings.Contains(sql, accountCustomizationJoin(alias)) {
					offences = append(offences, " renders accountCustomizationProjection("+alias+
						") without accountCustomizationJoin("+alias+")")
				}
			}
			bare := accountCustomizationColumnRe(alias).FindAllString(stripped, -1)
			if len(bare) == 0 || excused {
				continue
			}
			offences = append(offences, " ("+strings.Join(bare, ", ")+")"+
				"\n      SELECT "+strings.Join(strings.Fields(list), " ")+" FROM ...")
		}
	}

	// ── The WHERE / ORDER BY half (fix round 1, F2). ────────────────────────────────────────
	//
	// Everything above judges SELECT LISTS, and that left a hole the requirements themselves
	// single out: reverting A1's filter to `AND a.is_hidden = FALSE`, or its ordering to
	// `COALESCE(a.display_order, 999999)`, leaves the projection and the join intact and every
	// check above green — while producing the "worst of both" state accountHiddenExpr's own
	// comment warns about, where the list renders the row as hidden and the query keeps
	// returning it.
	//
	// SCOPED TO LITERALS THAT ALREADY RENDER THE OVERLAY, deliberately. A query that never
	// overlays anything is judged by the select-list rule alone; this is about internal
	// CONSISTENCY — once a literal has decided the chain is authoritative for its projection, it
	// may not consult the raw column anywhere else in the same statement.
	//
	// The select lists are removed first (they are judged above, and re-reporting them here
	// would double-count), then every renderer's own output is stripped for the alias in
	// question — the projection first, because accountHiddenExpr's text is a SUBSTRING of it.
	//
	// ⚠ THE ALIAS SET IS FILE-SCOPED, and that is a deliberate exception to this file's own
	// "judge each literal on its own" rule. Measured, not assumed: reverting A1's filter to
	// `AND a.is_hidden = FALSE` was NOT caught by the literal-scoped form, because that clause
	// is assembled in a SEPARATE Go string (`hiddenClause`) which folds on its own and carries
	// no `FROM accounts`, so no alias is bound in it at all. An optional predicate built in a
	// sibling literal is the ordinary shape in this package, so a literal-scoped rule is
	// structurally blind to the exact regression this half exists to catch. Widening to the file
	// is safe in the direction that matters: it can only ADD offences, the allowlist still
	// applies per literal, and the select-list rule above stays strictly per-span.
	for alias := range overlaidInFile {
		overlaidAliases[alias] = true
	}
	if len(overlaidAliases) > 0 && !excused {
		rest := sql
		for _, span := range accountProjectionSelect.FindAllString(sql, -1) {
			rest = strings.Replace(rest, span, " FROM ", 1)
		}
		for alias := range overlaidAliases {
			r := rest
			r = strings.ReplaceAll(r, accountCustomizationProjection(alias), " ")
			for _, f := range accountCustomizationColumns {
				r = strings.ReplaceAll(r, accountCustomizationExpr(alias, f), " ")
			}
			if bare := accountCustomizationColumnRe(alias).FindAllString(r, -1); len(bare) > 0 {
				offences = append(offences, " overlays its projection but reads "+
					strings.Join(bare, ", ")+" RAW outside the select list (a WHERE or ORDER BY "+
					"regression: the row renders as the chain says and is filtered/sorted as "+
					"`accounts` says)")
			}
		}
	}

	return offences, guarded
}

func TestNoAccountCustomizationReadWithoutTheOverlay(t *testing.T) {
	root := repoInternalRoot(t)

	var offenders []string
	guardedProjections := 0

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

		exprs := sqlStringExprs(file)
		// Pass 1: which aliases does this FILE overlay anywhere? See the file-scope note in
		// customizationOverlayOffences for why the second rule needs this and the first does not.
		fileOverlaid := map[string]bool{}
		for _, expr := range exprs {
			for alias := range overlaidAliasesIn(expr.SQL) {
				fileOverlaid[alias] = true
			}
		}
		// Pass 2: judge.
		for _, expr := range exprs {
			offences, guarded := customizationOverlayOffences(rel, expr.SQL, fileOverlaid)
			guardedProjections += guarded
			line := strconv.Itoa(fset.Position(expr.Pos).Line)
			for _, o := range offences {
				offenders = append(offenders, rel+":"+line+o)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walking internal/: %v", err)
	}

	sort.Strings(offenders)
	if len(offenders) > 0 {
		t.Fatalf("%d account read(s) project a customization column straight off `accounts` "+
			"(#1609 Item 3).\n"+
			"PATCH /api/v1/user-accounts/{id} writes those six fields to user_accounts and ONLY to "+
			"user_accounts, so a read that takes them from `accounts` shows the user the value they "+
			"had BEFORE their last edit — the edit reverts on refresh, which is the defect this "+
			"ruling exists to end. Render them through accountCustomizationProjection(alias) and add "+
			"accountCustomizationJoin(alias) — see account_liveness_sql.go. If this read genuinely "+
			"must not overlay, add an entry with a written reason to "+
			"customizationOverlayExceptions:\n    %s", len(offenders), strings.Join(offenders, "\n    "))
	}

	// Fail closed. Five reads render the overlay; zero means the folding, the alias scanner or the
	// renderer changed shape and this guard is examining nothing — which is the failure mode it
	// exists to prevent, applied to itself.
	if guardedProjections < 5 {
		t.Fatalf("this guard checked only %d overlaid account projection(s) under %s; there are "+
			"five, so the scan is broken and passing means nothing", guardedProjections, root)
	}
}

// accountsAliasesIn returns every alias bound to the `accounts` table by a FROM/JOIN in sql,
// deduplicated and in stable order. An UNALIASED reference is addressed by the table name, which is
// also how a qualified column would name it.
//
// Reuses accountsTableRef / sqlKeywordAfterAccounts from account_balance_projection_guard_test.go
// deliberately: one definition of "what is a reference to accounts" for both guards, so a fix to
// one cannot leave the other reading a keyword as an alias.
func accountsAliasesIn(sql string) []string {
	var out []string
	seen := map[string]bool{}
	for _, m := range accountsTableRef.FindAllStringSubmatch(sql, -1) {
		alias := m[1]
		if sqlKeywordAfterAccounts[strings.ToLower(alias)] {
			alias = ""
		}
		if alias == "" {
			alias = "accounts"
		}
		if seen[alias] {
			continue
		}
		seen[alias] = true
		out = append(out, alias)
	}
	return out
}

// ---- Meta-tests: the constructed violations. ---------------------------------------------------

// TestAccountCustomizationOverlayGuard_Recognises builds each case rather than describing it. The
// negative cases carry the weight: a checker that reported everything would satisfy the positive
// ones and would be switched off within a day.
func TestAccountCustomizationOverlayGuard_Recognises(t *testing.T) {
	compliant := "SELECT a.id, a.plaid_account_id, a.status, " +
		accountCustomizationProjection("a") + " FROM accounts a" + accountCustomizationJoin("a")

	t.Run("the defect: a raw qualified column", func(t *testing.T) {
		sql := "SELECT a.id, a.nickname, a.is_hidden FROM accounts a"
		assertOverlayOffences(t, sql, 1)
	})
	t.Run("the compliant form is clean", func(t *testing.T) {
		assertOverlayOffences(t, compliant, 0)
	})
	t.Run("the projection's own a_ua columns are not offences", func(t *testing.T) {
		// The joined user_accounts alias is the SOURCE of the overlay. If `\baccounts` matched
		// inside `user_accounts`, a_ua would be bound as an accounts alias and every compliant
		// query in the tree would be an offender.
		require := accountsAliasesIn(compliant)
		if len(require) != 1 || require[0] != "a" {
			t.Fatalf("the joined user_accounts alias must NOT bind as an accounts alias; got %v", require)
		}
	})
	t.Run("a neighbouring compliant query does not excuse this one", func(t *testing.T) {
		sql := compliant + "; SELECT b.id, b.nickname FROM accounts b"
		assertOverlayOffences(t, sql, 1)
	})
	t.Run("the same six columns over user_accounts are out of scope", func(t *testing.T) {
		assertOverlayOffences(t, "SELECT ua.id, ua.nickname, ua.is_hidden FROM user_accounts ua", 0)
	})
	t.Run("a narrow probe naming none of the six is out of scope", func(t *testing.T) {
		assertOverlayOffences(t, "SELECT a.id, a.status FROM accounts a WHERE a.user_id = $1", 0)
	})
	t.Run("all six columns are covered, one at a time", func(t *testing.T) {
		for _, col := range accountCustomizationColumns {
			assertOverlayOffences(t, "SELECT a.id, a."+col+" FROM accounts a", 1)
		}
	})
	t.Run("a projection WITHOUT its join is an offence", func(t *testing.T) {
		// The half-edited shape: the select list was migrated, the FROM clause was not. It cannot
		// run — a_ua is an unknown alias — but a partial refactor produces exactly this, and a
		// guard that only checks the select list would call it compliant.
		sql := "SELECT a.id, a.plaid_account_id, a.status, " +
			accountCustomizationProjection("a") + " FROM accounts a"
		assertOverlayOffences(t, sql, 1)
	})
	t.Run("the allowlist excuses a raw column, and ONLY for its own file", func(t *testing.T) {
		sql := "SELECT a.id, a.is_hidden FROM accounts a WHERE a.user_account_id IS NULL"
		assertOverlayOffencesIn(t, "database/backfill.go", sql, 0)
		assertOverlayOffencesIn(t, "repository/some_other_file.go", sql, 1)
	})
	// ── The WHERE / ORDER BY half (fix round 1, F2). ────────────────────────────────────────
	t.Run("a reverted WHERE filter is an offence even with a perfect projection", func(t *testing.T) {
		sql := compliant + " WHERE a.user_id = $1 AND a.is_hidden = FALSE"
		assertOverlayOffences(t, sql, 1)
	})
	t.Run("a reverted ORDER BY is an offence even with a perfect projection", func(t *testing.T) {
		sql := compliant + " ORDER BY COALESCE(a.display_order, 999999) ASC"
		assertOverlayOffences(t, sql, 1)
	})
	t.Run("the overlaid WHERE and ORDER BY forms are clean", func(t *testing.T) {
		sql := compliant +
			" WHERE a.user_id = $1 AND " + accountHiddenExpr("a") + " = FALSE" +
			" ORDER BY COALESCE(" + accountDisplayOrderExpr("a") + ", 999999) ASC, a.created_at ASC"
		assertOverlayOffences(t, sql, 0)
	})
	t.Run("a literal that overlays NOTHING is not subject to the WHERE rule", func(t *testing.T) {
		// The consistency rule is about a literal contradicting ITSELF. A query that never
		// overlays is judged by the select-list rule alone, which is what keeps this off the
		// allowlisted repair scan's own `WHERE a.user_account_id IS NULL`.
		assertOverlayOffences(t, "SELECT a.id, a.status FROM accounts a WHERE a.is_hidden = FALSE", 0)
	})
	t.Run("an optional predicate built in a SEPARATE literal is still judged", func(t *testing.T) {
		// The shape that defeated the literal-scoped form: `hiddenClause` is its own Go string,
		// binds no alias, and folds separately from the query it is concatenated into. The file
		// overlays `a`, so the fragment is judged against that.
		frag := " AND a.is_hidden = FALSE"
		if got, _ := customizationOverlayOffences("", frag, nil); len(got) != 0 {
			t.Fatalf("with no file context the fragment is not judgeable; got %q", got)
		}
		got, _ := customizationOverlayOffences("", frag, map[string]bool{"a": true})
		if len(got) != 1 {
			t.Fatalf("a file that overlays `a` must not also filter on a raw a.is_hidden; got %d %q",
				len(got), got)
		}
		// And the overlaid form of the same fragment is clean.
		ok, _ := customizationOverlayOffences("",
			" AND "+accountHiddenExpr("a")+" = FALSE", map[string]bool{"a": true})
		if len(ok) != 0 {
			t.Fatalf("the overlaid predicate must be clean; got %q", ok)
		}
	})
	t.Run("KNOWN HOLE: a star projection is invisible", func(t *testing.T) {
		assertOverlayOffences(t, "SELECT a.* FROM accounts a", 0)
	})
	t.Run("KNOWN HOLE: an unqualified column over an unaliased table is invisible", func(t *testing.T) {
		assertOverlayOffences(t, "SELECT id, nickname FROM accounts", 0)
	})
}

// assertOverlayOffences runs the guard's per-SELECT logic over one SQL string, with no file and no
// allowlist, and asserts the offence count.
func assertOverlayOffences(t *testing.T, sql string, want int) {
	t.Helper()
	assertOverlayOffencesIn(t, "", sql, want)
}

func assertOverlayOffencesIn(t *testing.T, rel, sql string, want int) {
	t.Helper()
	got, _ := customizationOverlayOffences(rel, sql, nil)
	if len(got) != want {
		t.Fatalf("got %d offence(s) %q, want %d, for:\n%s", len(got), got, want, sql)
	}
}

// TestCustomizationOverlayExceptionsCarryReasons is the allowlist's own discipline: an entry
// without a real reason is a suppression nobody can audit later.
func TestCustomizationOverlayExceptionsCarryReasons(t *testing.T) {
	for _, e := range customizationOverlayExceptions {
		if e.File == "" || e.Marker == "" {
			t.Fatalf("exception %+v must name both a file and a SQL marker", e)
		}
		if len(strings.Fields(e.Reason)) < 12 {
			t.Fatalf("exception %s (%s) needs a REASON, not a label: %q", e.File, e.Marker, e.Reason)
		}
	}
}
