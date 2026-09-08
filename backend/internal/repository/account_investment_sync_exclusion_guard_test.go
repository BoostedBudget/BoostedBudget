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

// Issue #1618a — the scoping guard for domain.Account.ExcludeInvestmentSync.
// Issue #1618b — this file is the FIX ROUND. #1618a's original guard in this file (a test named
// "NoAccountCustomizationJoinReadOmitsExcludeInvestmentSync", since replaced by the one below) only
// inspected SELECTs that ALREADY carried accountCustomizationJoin — which means a read that omitted the join
// ENTIRELY was invisible to it by construction. Verified against the tree at the time: of 12
// accountRepository methods returning domain.Account, 5 carried no join at all —
// GetByLinkedItemIDIncludingRetained (a REAL production caller,
// investment_sync_service.SyncInvestmentTransactions — the very subsystem this flag is about),
// GetByUserID (also a real caller — dashboard_service.go, account_chain_resolver.go,
// savings_goal_service.go), and the three household-scoped reads. All five read
// Account.ExcludeInvestmentSync back as unconditional `false`, indistinguishable from "this chain
// does not exclude investment sync" — the same zero-value-reads-as-a-real-answer defect class
// TestNoAccountProjectionOmitsStatus exists to catch for `status`.
//
// THE FIX SHAPE. Rather than re-scope to "joined selects", this guard now follows
// TestNoAccountProjectionOmitsStatus's OWN shape exactly: every account-shaped SELECT (the same
// plaid_account_id marker) must project exclude_investment_sync, UNCONDITIONALLY, unless an
// explicit ALLOWLIST entry excuses it with a written reason — the same discipline
// customizationOverlayExceptions uses, and for the same reason: a silently-false field is not the
// same thing as a documented "not applicable here", and the difference must be visible in the
// code, not inferred from which SELECTs happen to have a join today.
//
// THE ONE ALLOWLISTED CASE: the household-scoped reads (GetForHouseholdScopeActiveNow /
// GetForHouseholdScopeLiveAsOf / getForHouseholdScope, all built by householdScopeQuery), scoped by
// the SAME "JOIN household_accounts ha" marker customizationOverlayExceptions already keys its own
// household entry on. See excludeInvestmentSyncExceptions below for why this one is legitimate
// rather than assumed.
//
// GetByLinkedItemIDIncludingRetained and GetByUserID are FIXED, not excepted: both gained
// accountCustomizationJoin("a") plus the projected column, the same two-leg LEFT JOIN #1618a added
// to GetByLinkedItemID, keyed on BOTH `id = a.user_account_id` AND `user_id = a.user_id` — a join
// missing the user_id leg would be a cross-tenant leak. Neither method's returned ROWS changed:
// GetByLinkedItemIDIncludingRetained's whole reason to exist is that it does NOT filter status, and
// a LEFT JOIN adds a column, never removes a row.
//
// PER SELECT, NOT PER FILE — the same lesson every sibling guard in this package learned.
//
// Mutation-checked: this guard was run against the tree BEFORE the two methods above were fixed
// and found exactly two offenders (GetByLinkedItemIDIncludingRetained, GetByUserID); see
// t2b-report.md for the captured RED output.
var accountExcludeInvestmentSyncColumn = regexp.MustCompile(`(?i)\bexclude_investment_sync\b`)

// excludeInvestmentSyncException excuses one account-shaped read from projecting
// exclude_investment_sync. Entries carry a written reason, following
// customizationOverlayException's rule (and history_delete_guard_test.go's, before it) that an
// allowlist without reasons becomes a list of things nobody remembers deciding.
//
// ⚠ A reason is prose and nothing checks that it is TRUE — the same caveat CLAUDE.md records for
// every allowlist in this package. Re-derive before relying on one.
type excludeInvestmentSyncException struct {
	// File is the path relative to backend/internal.
	File string
	// Marker is a substring of the (folded) SQL literal that identifies the query.
	Marker string
	Reason string
}

var excludeInvestmentSyncExceptions = []excludeInvestmentSyncException{
	{
		File:   "repository/account_repository.go",
		Marker: "JOIN household_accounts ha",
		Reason: "householdScopeQuery, shared by GetForHouseholdScopeActiveNow, " +
			"GetForHouseholdScopeLiveAsOf and getForHouseholdScope. This is the SAME query " +
			"customizationOverlayExceptions excuses from accountCustomizationJoin entirely (see " +
			"that file's household entry and householdScopeQuery's own '⚠ DELIBERATELY WITHOUT " +
			"the #1609 customization overlay' comment, citing the Issue #1369 decision 3 / " +
			"migration 000110 privacy ruling: is_hidden is the owner's PERSONAL-VIEW control and " +
			"must not travel into a household member's shared view). exclude_investment_sync is " +
			"not one of the six accountCustomizationColumns, but domain.Account.ExcludeInvestmentSync " +
			"is populated by the SAME join (accountCustomizationJoin), not a separate one — see " +
			"that field's own doc comment in domain/account.go. A query that structurally carries " +
			"no join at all has nothing to project the field FROM; adding one here only to surface " +
			"this single field would also require projecting (or deliberately still omitting) the " +
			"six customization columns the join exists for, reopening the exact privacy question " +
			"the ruling above already closed. And nothing downstream of these three reads NEEDS the " +
			"field: they feed household net-worth/balance aggregation (dashboard_service.go), which " +
			"never reads Account.ExcludeInvestmentSync — the actual investment-transaction exclusion " +
			"is enforced directly in SQL by investmentSyncExclusionCondition (transaction_repository.go), " +
			"never through this Go field. So excepting these three costs nothing today. If a " +
			"household-facing read ever needs this field, it must NOT be reached by silently adding " +
			"accountCustomizationJoin here — that would also leak is_hidden/nickname into the " +
			"household view, which is the regression the ruling exists to prevent.",
	},
}

func (e excludeInvestmentSyncException) matches(rel, sql string) bool {
	return e.File == rel && strings.Contains(sql, e.Marker)
}

// accountSelectsMissingExcludeInvestmentSync returns, for one folded SQL string, the select lists
// that are account-shaped (name plaid_account_id) but do not project exclude_investment_sync —
// UNLESS rel/sql matches an excludeInvestmentSyncExceptions entry, in which case the whole literal
// is excused (mirroring customizationOverlayOffences' allowlist scoping).
func accountSelectsMissingExcludeInvestmentSync(rel, sql string) []string {
	excused := false
	for _, e := range excludeInvestmentSyncExceptions {
		if e.matches(rel, sql) {
			excused = true
			break
		}
	}
	if excused {
		return nil
	}

	var offenders []string
	for _, m := range accountProjectionSelect.FindAllStringSubmatch(sql, -1) {
		list := m[1]
		if !accountProjectionMarker.MatchString(list) {
			continue // not account-shaped
		}
		if accountExcludeInvestmentSyncColumn.MatchString(list) {
			continue // already projects it
		}
		offenders = append(offenders, strings.Join(strings.Fields(list), " "))
	}
	return offenders
}

// TestNoAccountProjectionOmitsExcludeInvestmentSync is the guard. Replaces #1618a's original
// join-conditioned test in this file, whose scope was the gap #1618b exists to close.
func TestNoAccountProjectionOmitsExcludeInvestmentSync(t *testing.T) {
	root := repoInternalRoot(t)

	var offenders []string
	seenAccountProjection := 0

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
			if !containsAccountProjectionMarker(expr.SQL) {
				continue
			}
			seenAccountProjection++
			for _, bad := range accountSelectsMissingExcludeInvestmentSync(rel, expr.SQL) {
				offenders = append(offenders,
					rel+":"+strconv.Itoa(fset.Position(expr.Pos).Line)+"\n      SELECT "+bad+" FROM ...")
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walking internal/: %v", err)
	}

	// Fail closed. TestNoAccountProjectionOmitsStatus's own floor for the identical "account-shaped"
	// scan is 10; this guard walks the exact same corpus (containsAccountProjectionMarker), so the
	// same floor applies here. Finding fewer means the walk or the fold broke, and a vacuous pass
	// here would read as protection.
	if seenAccountProjection < 10 {
		t.Fatalf("found only %d SQL literal(s) mentioning plaid_account_id under %s; the scan is broken",
			seenAccountProjection, root)
	}

	sort.Strings(offenders)
	if len(offenders) > 0 {
		t.Fatalf("%d account-shaped read(s) do not project exclude_investment_sync (#1618b).\n"+
			"A read that omits this column returns Account.ExcludeInvestmentSync == false "+
			"unconditionally, which is indistinguishable from a chain that genuinely does not "+
			"exclude investment sync. Add accountCustomizationJoin(alias) plus "+
			"`COALESCE(<alias>_ua.exclude_investment_sync, FALSE) AS exclude_investment_sync` to the "+
			"select list (and the matching scan destination), or add a written-reason entry to "+
			"excludeInvestmentSyncExceptions if this read genuinely must not carry it:\n    %s",
			len(offenders), strings.Join(offenders, "\n    "))
	}
}

// ---- Meta-tests: the constructed violations and their negative-space siblings. -----------------

func TestAccountExcludeInvestmentSyncGuard_Recognises(t *testing.T) {
	// compliant is the real shape GetByIDAndUser renders: account-shaped, customization-joined,
	// and projecting exclude_investment_sync.
	compliant := "SELECT a.id, a.plaid_account_id, a.status, " +
		accountCustomizationProjection("a") + ", " +
		"COALESCE(a_ua.exclude_investment_sync, FALSE) AS exclude_investment_sync " +
		"FROM accounts a" + accountCustomizationJoin("a")

	t.Run("the defect: joined but omits the column", func(t *testing.T) {
		sql := "SELECT a.id, a.plaid_account_id, a.status, " +
			accountCustomizationProjection("a") + " FROM accounts a" + accountCustomizationJoin("a")
		got := accountSelectsMissingExcludeInvestmentSync("", sql)
		if len(got) != 1 {
			t.Fatalf("want 1 offence, got %d: %v", len(got), got)
		}
	})

	t.Run("the compliant form is clean", func(t *testing.T) {
		got := accountSelectsMissingExcludeInvestmentSync("", compliant)
		if len(got) != 0 {
			t.Fatalf("want 0 offences, got %d: %v", len(got), got)
		}
	})

	// ── The #1618b gap this file exists to close. ───────────────────────────────────────────────
	t.Run("#1618b: NO join at all is NOW an offence, not out of scope", func(t *testing.T) {
		// GetByUserID's PRE-FIX shape: account-shaped, no customization join, no
		// exclude_investment_sync. #1618a's guard called this out of scope BY DESIGN — that
		// design was the bug: this field then reads false regardless of the real chain value,
		// exactly the defect class the guard exists to prevent. It must be flagged now.
		sql := "SELECT id, plaid_account_id, status FROM accounts"
		got := accountSelectsMissingExcludeInvestmentSync("", sql)
		if len(got) != 1 {
			t.Fatalf("want 1 offence (the #1618b gap), got %d: %v", len(got), got)
		}
	})

	t.Run("household-shaped join is a DIFFERENT join and still needs the column unless excepted", func(t *testing.T) {
		// householdAccountsCTE-style joins do not match accountCustomizationJoin's exact shape
		// (different table, different ON clause). With no allowlist entry supplied (rel=""), this
		// must still be flagged — the household exception is scoped to its OWN file+marker, not
		// granted to every JOIN household_accounts anywhere.
		sql := "SELECT a.id, a.plaid_account_id, a.status " +
			"FROM accounts a JOIN household_accounts ha ON ha.account_id = a.id"
		got := accountSelectsMissingExcludeInvestmentSync("", sql)
		if len(got) != 1 {
			t.Fatalf("want 1 offence (no allowlist entry matches this rel/marker), got %d: %v", len(got), got)
		}
	})

	t.Run("the household allowlist excuses ITS OWN file+marker, and only that", func(t *testing.T) {
		sql := "SELECT a.id, a.plaid_account_id, a.status " +
			"FROM accounts a JOIN household_accounts ha ON ha.user_account_id = a.user_account_id"
		got := accountSelectsMissingExcludeInvestmentSync("repository/account_repository.go", sql)
		if len(got) != 0 {
			t.Fatalf("want 0 offences (excepted), got %d: %v", len(got), got)
		}
		gotElsewhere := accountSelectsMissingExcludeInvestmentSync("repository/some_other_file.go", sql)
		if len(gotElsewhere) != 1 {
			t.Fatalf("want 1 offence (same marker, wrong file), got %d: %v", len(gotElsewhere), gotElsewhere)
		}
	})

	t.Run("a name-resolution read (not account-shaped) is out of scope even though it joins", func(t *testing.T) {
		// The #1611 shape: savings_impact_repository.go / transfer_suggestion_repository.go call
		// accountCustomizationJoin to fix the chain-nickname bug, but neither SELECT names
		// plaid_account_id — there is no domain.Account being scanned here, so nothing to omit.
		sql := "SELECT ts.id, COALESCE(NULLIF(" + accountCustomizationExpr("a", "nickname") +
			", ''), a.name) AS plaid_account_name FROM transfer_suggestions ts " +
			"LEFT JOIN accounts a ON a.id = ts.to_account_id" + accountCustomizationJoin("a")
		got := accountSelectsMissingExcludeInvestmentSync("", sql)
		if len(got) != 0 {
			t.Fatalf("want 0 offences (not account-shaped), got %d: %v", len(got), got)
		}
	})

	t.Run("two aliases in one literal are judged independently", func(t *testing.T) {
		joinedCompliant := "SELECT a.id, a.plaid_account_id, " +
			"COALESCE(a_ua.exclude_investment_sync, FALSE) AS exclude_investment_sync " +
			"FROM accounts a" + accountCustomizationJoin("a")
		joinedOffender := "; SELECT b.id, b.plaid_account_id FROM accounts b" + accountCustomizationJoin("b")
		got := accountSelectsMissingExcludeInvestmentSync("", joinedCompliant+joinedOffender)
		if len(got) != 1 {
			t.Fatalf("want 1 offence (only the unfixed alias), got %d: %v", len(got), got)
		}
	})

	t.Run("a neighbouring compliant query does not excuse an offending one", func(t *testing.T) {
		offender := "SELECT a.id, a.plaid_account_id FROM accounts a" + accountCustomizationJoin("a")
		got := accountSelectsMissingExcludeInvestmentSync("", compliant+"; "+offender)
		if len(got) != 1 {
			t.Fatalf("want 1 offence, got %d: %v", len(got), got)
		}
	})
}

// TestExcludeInvestmentSyncExceptionsCarryReasons is the allowlist's own discipline: an entry
// without a real reason is a suppression nobody can audit later. Same rule as
// TestCustomizationOverlayExceptionsCarryReasons.
func TestExcludeInvestmentSyncExceptionsCarryReasons(t *testing.T) {
	for _, e := range excludeInvestmentSyncExceptions {
		if e.File == "" || e.Marker == "" {
			t.Fatalf("exception %+v must name both a file and a SQL marker", e)
		}
		if len(strings.Fields(e.Reason)) < 12 {
			t.Fatalf("exception %s (%s) needs a REASON, not a label: %q", e.File, e.Marker, e.Reason)
		}
	}
}
