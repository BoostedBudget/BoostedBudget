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
//
// Mutation-checked: restoring `a.nickname, a.custom_color, ...` at
// GetAccountsWithInstitutionFiltered turns this red naming the file, line and select list. See
// task-3b-report.md for what it printed.

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
			"direction to everything else here: this row has NO chain yet (that is the WHERE " +
			"clause), so `accounts` is the only place its hide can be, and the value is being " +
			"LIFTED onto the user_accounts row it is about to join (carryLegacyHideForward, " +
			"#1609 task 3a). Overlaying a chain that does not exist would read NULL and destroy " +
			"the hide — the exact defect this repair exists to prevent.",
	},
	{
		File:   "repository/savings_impact_repository.go",
		Marker: "AS plaid_account_name",
		Reason: "Recent-items destination naming has its OWN documented precedence — " +
			"COALESCE(NULLIF(a.nickname,''), a.name) for a Plaid-linked destination, falling back to " +
			"the user_accounts row's nickname/hint through a SEPARATE join it already carries. It is " +
			"resolving a display name across two tables, not projecting an account row, and the " +
			"overlay's CASE would silently change which of the two branches wins.",
	},
	{
		File:   "repository/transfer_suggestion_repository.go",
		Marker: "AS to_plaid_name",
		Reason: "Same shape as savings_impact, doubled: resolution order is documented at the query " +
			"as Plaid nickname > Plaid name > UA nickname > UA hint, for BOTH the from- and to-side, " +
			"each with its own user_accounts join already present. Not an account projection.",
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

		for _, expr := range sqlStringExprs(file) {
			aliases := accountsAliasesIn(expr.SQL)
			if len(aliases) == 0 {
				continue
			}
			line := strconv.Itoa(fset.Position(expr.Pos).Line)

			for _, m := range accountProjectionSelect.FindAllStringSubmatch(expr.SQL, -1) {
				list := m[1]
				// Strip the compliant form — the renderer's OWN output, for every alias in play —
				// before looking for a raw column. What remains is by definition not rendered
				// through it.
				stripped := list
				for _, alias := range aliases {
					stripped = strings.ReplaceAll(stripped, accountCustomizationProjection(alias), " ")
				}
				for _, alias := range aliases {
					if strings.Contains(list, accountCustomizationProjection(alias)) {
						guardedProjections++
						// The projection is useless without its join: a_ua would be an unknown
						// alias and the query would not even parse, but a HALF-edited query (join
						// added to one literal, projection to another) is exactly the shape a
						// partial refactor leaves behind.
						if !strings.Contains(expr.SQL, accountCustomizationJoin(alias)) {
							offenders = append(offenders, rel+":"+line+
								" renders accountCustomizationProjection("+alias+
								") without accountCustomizationJoin("+alias+")")
						}
					}
					bare := accountCustomizationColumnRe(alias).FindAllString(stripped, -1)
					if len(bare) == 0 {
						continue
					}
					excused := false
					for _, e := range customizationOverlayExceptions {
						if e.matches(rel, expr.SQL) {
							excused = true
							break
						}
					}
					if excused {
						continue
					}
					offenders = append(offenders, rel+":"+line+" ("+strings.Join(bare, ", ")+")"+
						"\n      SELECT "+strings.Join(strings.Fields(list), " ")+" FROM ...")
				}
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
	aliases := accountsAliasesIn(sql)
	got := 0
	for _, m := range accountProjectionSelect.FindAllStringSubmatch(sql, -1) {
		stripped := m[1]
		for _, alias := range aliases {
			stripped = strings.ReplaceAll(stripped, accountCustomizationProjection(alias), " ")
		}
		for _, alias := range aliases {
			if len(accountCustomizationColumnRe(alias).FindAllString(stripped, -1)) > 0 {
				got++
			}
		}
	}
	if got != want {
		t.Fatalf("got %d offence(s), want %d, for:\n%s", got, want, sql)
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
