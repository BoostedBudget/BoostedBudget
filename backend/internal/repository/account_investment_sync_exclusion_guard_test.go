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
//
// THE SCOPING DECISION. exclude_investment_sync lives on user_accounts, so reading it costs
// nothing beyond the LEFT JOIN accountCustomizationJoin already renders for the six customization
// columns (nickname, custom_color, custom_icon, custom_emoji, display_order, is_hidden) — see
// account_liveness_sql.go. Rather than force EVERY account-shaped read (including the
// household-scoped ones, which deliberately do NOT carry that join at all — see
// accountCustomizationJoin's own "NOT applied to householdScopeQuery" note) to add a join it has no
// other use for, this field is scoped to exactly the reads that already pay for the join: wherever
// accountCustomizationJoin(alias) appears in an account-shaped SELECT, that SELECT must also
// project exclude_investment_sync. A household read never carries the join, so it can never
// violate this guard — the scope is structural, not a hand-maintained list.
//
// THE DEFECT SHAPE this catches. A future account read that adds accountCustomizationJoin (say, to
// pick up nickname/is_hidden) but forgets exclude_investment_sync compiles, scans and passes every
// existing test — Account.ExcludeInvestmentSync simply reads `false`, which is indistinguishable
// from "this chain does not exclude investment sync". That is the same invisible-zero-value defect
// TestNoAccountProjectionOmitsStatus exists to catch for `status`, one field over.
//
// THE MARKER. Account-shaped is the same test that guard uses: the select list names
// plaid_account_id. Customization-joined is: the SQL contains accountCustomizationJoin(alias)'s
// OWN RENDERED TEXT for some alias — matched structurally (the exact
// "LEFT JOIN user_accounts <a>_ua ON <a>_ua.id = <a>.user_account_id AND <a>_ua.user_id =
// <a>.user_id" shape with the alias captured and required to agree on both sides), not a loose
// "user_accounts" substring search — so a query that joins user_accounts for an unrelated reason
// does not spuriously trip this guard.
//
// WHY NOT REUSE accountSelectsMissingStatus's helper directly: that one is unconditional (every
// account-shaped read must have status); this one is CONDITIONAL on the join already being
// present, which is the whole point of the scoping decision above.
//
// PER SELECT, NOT PER FILE — the same lesson every sibling guard in this package learned. Two
// account-shaped, customization-joined queries in one file (transfer_suggestion_repository.go has
// TWO account aliases, to_acc and from_acc, though neither is account-shaped — see below) must each
// carry the projection independently.
//
// WHAT IT DOES NOT COVER, by construction rather than oversight: savings_impact_repository.go and
// transfer_suggestion_repository.go both call accountCustomizationJoin (#1611, the chain-nickname
// fix) but neither SELECT is account-shaped — they resolve a single display-name string, not a
// domain.Account row, so plaid_account_id never appears in their select lists and this guard never
// looks at them. That is correct: there is no Account.ExcludeInvestmentSync for those reads to
// populate.
// Go's RE2 engine (regexp) has no backreferences, so the two alias occurrences are captured
// separately and compared in Go — see accountCustomizationJoinPattern.MatchString below, which is
// a method value wrapping that comparison rather than a bare regexp.
var accountCustomizationJoinRe = regexp.MustCompile(
	`(?is)LEFT\s+JOIN\s+user_accounts\s+([A-Za-z_][A-Za-z0-9_]*)_ua\s+ON\s+` +
		`([A-Za-z_][A-Za-z0-9_]*)_ua\.id\s*=\s*([A-Za-z_][A-Za-z0-9_]*)\.user_account_id\s+AND\s+` +
		`([A-Za-z_][A-Za-z0-9_]*)_ua\.user_id\s*=\s*([A-Za-z_][A-Za-z0-9_]*)\.user_id`)

// accountCustomizationJoinPattern reports whether sql contains an accountCustomizationJoin(alias)
// rendering for SOME single alias — all five captured alias occurrences (the join's own alias,
// repeated three times, and the base table's alias, repeated twice) must agree with each other.
// A mismatched pair (a copy-paste error joining a_ua onto b's columns) must NOT match.
var accountCustomizationJoinPattern = joinPatternMatcher{accountCustomizationJoinRe}

type joinPatternMatcher struct{ re *regexp.Regexp }

func (m joinPatternMatcher) MatchString(sql string) bool {
	return len(m.FindAllStringSubmatch(sql)) > 0
}

func (m joinPatternMatcher) FindAllStringSubmatch(sql string) [][]string {
	var out [][]string
	for _, g := range m.re.FindAllStringSubmatch(sql, -1) {
		// g[1..5] are the five alias captures: join-alias x3 (from the join's own text), base
		// x2 (the accounts-side alias). All must be the SAME identifier for this to be a genuine
		// accountCustomizationJoin(alias) rendering rather than a coincidental lookalike.
		joinAlias, base := g[1], g[3]
		if g[2] != joinAlias || g[4] != joinAlias || g[5] != base || joinAlias != base {
			continue
		}
		out = append(out, g)
	}
	return out
}

// accountExcludeInvestmentSyncColumn matches a projected exclude_investment_sync column, however
// it is aliased back (the production form is
// `COALESCE(<alias>_ua.exclude_investment_sync, FALSE) AS exclude_investment_sync`, but the guard
// only needs to know the column was read from SOMEWHERE in the select list).
var accountExcludeInvestmentSyncColumn = regexp.MustCompile(`(?i)\bexclude_investment_sync\b`)

// accountSelectsJoinedButMissingExcludeInvestmentSync returns, for one folded SQL string, the
// select lists that are account-shaped (name plaid_account_id), carry AT LEAST ONE
// accountCustomizationJoin, but do not project exclude_investment_sync.
func accountSelectsJoinedButMissingExcludeInvestmentSync(sql string) []string {
	if !accountCustomizationJoinPattern.MatchString(sql) {
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

// TestNoAccountCustomizationJoinReadOmitsExcludeInvestmentSync is the guard.
func TestNoAccountCustomizationJoinReadOmitsExcludeInvestmentSync(t *testing.T) {
	root := repoInternalRoot(t)

	var offenders []string
	seenJoinedAccountProjection := 0

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
			if !containsAccountProjectionMarker(expr.SQL) || !accountCustomizationJoinPattern.MatchString(expr.SQL) {
				continue
			}
			seenJoinedAccountProjection++
			for _, bad := range accountSelectsJoinedButMissingExcludeInvestmentSync(expr.SQL) {
				offenders = append(offenders,
					rel+":"+strconv.Itoa(fset.Position(expr.Pos).Line)+"\n      SELECT "+bad+" FROM ...")
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walking internal/: %v", err)
	}

	// Fail closed. #1618a scoped this field to exactly the account-shaped, customization-joined
	// reads (7 today: GetByIDAndUser, GetByUserAccountIDAndUser, GetAccountsWithInstitutionFiltered,
	// GetByUserAccountAndStatus, GetByUserAccountID, GetByLinkedItemID, GetAccountsWithInstitution).
	// Finding none means the walk or the fold broke, and a vacuous pass here would read as
	// protection.
	if seenJoinedAccountProjection < 7 {
		t.Fatalf("found only %d account-shaped, customization-joined SQL literal(s) under %s; the scan is broken",
			seenJoinedAccountProjection, root)
	}

	sort.Strings(offenders)
	if len(offenders) > 0 {
		t.Fatalf("%d account-shaped read(s) carry accountCustomizationJoin but do not project "+
			"exclude_investment_sync (#1618a).\n"+
			"The join is already paid for; a read that carries it but omits this column returns "+
			"Account.ExcludeInvestmentSync == false unconditionally, which is indistinguishable from "+
			"a chain that genuinely does not exclude investment sync. Add "+
			"`COALESCE(<alias>_ua.exclude_investment_sync, FALSE) AS exclude_investment_sync` to the "+
			"select list (and the matching scan destination), or drop the join if this read never "+
			"needed the chain overlay at all:\n    %s",
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
		got := accountSelectsJoinedButMissingExcludeInvestmentSync(sql)
		if len(got) != 1 {
			t.Fatalf("want 1 offence, got %d: %v", len(got), got)
		}
	})

	t.Run("the compliant form is clean", func(t *testing.T) {
		got := accountSelectsJoinedButMissingExcludeInvestmentSync(compliant)
		if len(got) != 0 {
			t.Fatalf("want 0 offences, got %d: %v", len(got), got)
		}
	})

	t.Run("no join at all is out of scope, even without the column", func(t *testing.T) {
		// GetByUserID's shape: account-shaped, no customization join, no exclude_investment_sync.
		// This is the whole point of the scoping decision — no join means no cost-free field to
		// forget, so it must not be flagged.
		sql := "SELECT id, plaid_account_id, status FROM accounts"
		got := accountSelectsJoinedButMissingExcludeInvestmentSync(sql)
		if len(got) != 0 {
			t.Fatalf("want 0 offences (out of scope), got %d: %v", len(got), got)
		}
	})

	t.Run("household-shaped join is a DIFFERENT join and stays out of scope", func(t *testing.T) {
		// householdAccountsCTE-style joins do not match accountCustomizationJoin's exact shape
		// (different table, different ON clause), so they must not trip this guard even though
		// they also join something onto an accounts alias.
		sql := "SELECT a.id, a.plaid_account_id, a.status " +
			"FROM accounts a JOIN household_accounts ha ON ha.account_id = a.id"
		got := accountSelectsJoinedButMissingExcludeInvestmentSync(sql)
		if len(got) != 0 {
			t.Fatalf("want 0 offences (different join), got %d: %v", len(got), got)
		}
	})

	t.Run("a name-resolution read (not account-shaped) is out of scope even though it joins", func(t *testing.T) {
		// The #1611 shape: savings_impact_repository.go / transfer_suggestion_repository.go call
		// accountCustomizationJoin to fix the chain-nickname bug, but neither SELECT names
		// plaid_account_id — there is no domain.Account being scanned here, so nothing to omit.
		sql := "SELECT ts.id, COALESCE(NULLIF(" + accountCustomizationExpr("a", "nickname") +
			", ''), a.name) AS plaid_account_name FROM transfer_suggestions ts " +
			"LEFT JOIN accounts a ON a.id = ts.to_account_id" + accountCustomizationJoin("a")
		got := accountSelectsJoinedButMissingExcludeInvestmentSync(sql)
		if len(got) != 0 {
			t.Fatalf("want 0 offences (not account-shaped), got %d: %v", len(got), got)
		}
	})

	t.Run("two aliases in one literal are judged independently", func(t *testing.T) {
		// transfer_suggestion_repository.go's shape, but hypothetically account-shaped on both
		// sides and only one side fixed — each SELECT span stands on its own.
		joinedCompliant := "SELECT a.id, a.plaid_account_id, " +
			"COALESCE(a_ua.exclude_investment_sync, FALSE) AS exclude_investment_sync " +
			"FROM accounts a" + accountCustomizationJoin("a")
		joinedOffender := "; SELECT b.id, b.plaid_account_id FROM accounts b" + accountCustomizationJoin("b")
		got := accountSelectsJoinedButMissingExcludeInvestmentSync(joinedCompliant + joinedOffender)
		if len(got) != 1 {
			t.Fatalf("want 1 offence (only the unfixed alias), got %d: %v", len(got), got)
		}
	})

	t.Run("a neighbouring compliant query does not excuse an offending one", func(t *testing.T) {
		offender := "SELECT a.id, a.plaid_account_id FROM accounts a" + accountCustomizationJoin("a")
		got := accountSelectsJoinedButMissingExcludeInvestmentSync(compliant + "; " + offender)
		if len(got) != 1 {
			t.Fatalf("want 1 offence, got %d: %v", len(got), got)
		}
	})
}

// TestAccountCustomizationJoinPattern_MatchesExactlyItsOwnRenderer proves the regex used to detect
// "this read already carries the join" agrees with accountCustomizationJoin's actual output —
// otherwise this guard could drift silently from the renderer it is meant to key off of, the same
// class of hole account_status_projection_guard_test.go's header warns about for its own hand-written
// balance fragment.
func TestAccountCustomizationJoinPattern_MatchesExactlyItsOwnRenderer(t *testing.T) {
	for _, alias := range []string{"a", "to_acc", "from_acc"} {
		rendered := accountCustomizationJoin(alias)
		if !accountCustomizationJoinPattern.MatchString(rendered) {
			t.Fatalf("pattern does not match accountCustomizationJoin(%q): %q", alias, rendered)
		}
	}
	// A mismatched alias pair (copy-paste error) must NOT match — the \1 backreference is the
	// whole point.
	mismatched := "LEFT JOIN user_accounts a_ua ON a_ua.id = b.user_account_id AND a_ua.user_id = b.user_id"
	if accountCustomizationJoinPattern.MatchString(mismatched) {
		t.Fatalf("pattern must not match a mismatched alias pair: %q", mismatched)
	}
}
