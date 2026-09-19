package repository

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ─── #1626 Phase 2 pgxmock sweep: guard against a recurrence ──────────────────────────────────────
//
// pgxmock scans a `nil` `AddRow` cell into a plain Go `string` (or `float64`, `bool`, `time.Time`)
// WITHOUT an error. Real pgx v5 against real Postgres does not: it fails the scan outright
// ("cannot scan NULL into *string"). Confirmed empirically for this sweep and, independently, by
// the four pgxmock tests analytics_null_category_display_integration_test.go replaced — they PASSED
// against genuinely unfixed, NULL-intolerant production code. A pgxmock-backed unit test that claims
// to prove NULL tolerance ("scans to empty string, not an error", "must not fail the scan", "NULL
// ... tolerates", ...) cannot fail for the reason its name or comment asserts, and is therefore
// vacuous for that specific claim — see analytics_category_total_display_test.go and
// analytics_top_expense_unusual_spend_display_test.go for the renames this sweep applied once that
// was mutation-confirmed.
//
// THE GUARD, and its limits, stated up front rather than left to be discovered. This is a TEXT scan,
// not a semantic one: it cannot verify a claim is FALSE (that requires the mutation this sweep did
// by hand — reverting a `sql.NullString`/`*string` to a plain type and rerunning), only that a
// pgxmock-backed test file contains language that SOUNDS like an undisclosed NULL-tolerance claim.
// It will not catch a claim phrased in words this scan does not recognise, and it can be satisfied
// by adding disclaiming language to a claim that is still, in substance, misleading. It is a
// tripwire for the recurring SHAPE of this mistake, not a proof that every pgxmock NULL assertion in
// this repository is honest. If a future case slips past it, extend the regexes below — do not
// declare the guard adequate because it currently passes.
//
// THE RULE: in any *_test.go file (excluding `//go:build integration` files, which run against real
// Postgres, and excluding this file itself) that both (a) imports pgxmock and (b) passes a literal
// `nil` into an `AddRow(...)` call, any nearby NULL-tolerance CLAIM (matching nullToleranceClaimRe)
// must be paired with a DISCLAIMER (matching pgxmockCannotProveNullDisclaimerRe) within the same
// small window of text — acknowledging that pgxmock cannot itself prove the claim, and (ideally)
// pointing at a real-Postgres integration test that does. An undisclosed claim fails the build.
//
// Mutation-checked: TestPgxmockNullToleranceClaimGuard_FlagsAnUndisclaimedClaim constructs exactly
// this shape (a `nil` AddRow cell plus an undisclosed NULL-tolerance claim) and proves the scanning
// function flags it; TestPgxmockNullToleranceClaimGuard_IgnoresADisclosedClaim and
// TestPgxmockNullToleranceClaimGuard_IgnoresUnrelatedNullText prove it does not false-positive on
// this sweep's own renamed tests or on ordinary "IS NULL" SQL-shape assertions.

var (
	pgxmockImportRe       = regexp.MustCompile(`(?m)"github\.com/pashagolub/pgxmock`)
	addRowNilRe           = regexp.MustCompile(`AddRow\([^)]*\bnil\b`)
	buildIntegrationTagRe = regexp.MustCompile(`(?m)^//go:build\s+integration\b`)

	// nullToleranceClaimRe matches language asserting that a NULL cell scans/tolerates/survives
	// without erroring — in either word order ("NULL ... no error" or "no error ... NULL").
	nullToleranceClaimRe = regexp.MustCompile(`(?is)` +
		`(\bnull\b[\s\S]{0,140}?\b(no error|not (an )?error|without error|does not error|` +
		`must not (fail|error)|not fail|scans? to (the )?empty string|tolerat\w*|survives? the scan)\b)` +
		`|` +
		`(\b(no error|not (an )?error|without error|does not error|must not (fail|error)|not fail|` +
		`tolerat\w*)\b[\s\S]{0,140}?\bnull\b)`)

	// pgxmockCannotProveNullDisclaimerRe matches language disclosing that pgxmock itself cannot
	// prove a NULL-scan claim — the honest alternative this sweep applied via rename.
	pgxmockCannotProveNullDisclaimerRe = regexp.MustCompile(`(?is)` +
		`pgxmock (can(not|'t)|does not|never)\b|not a null-scan proof|mutation-confirm|` +
		`cannot (tell|distinguish|prove)|cannot provide one|no null-scan proof|` +
		`pgxmock cannot prove`)

	// scanWordRe requires the CLAIM to actually be about a *scan* — a column coming back NULL and
	// being read without erroring — not just any nearby sentence that happens to say "NULL" and
	// "must not error" for an unrelated reason (an UPDATE ... = NULL idempotency test, for
	// instance: setting a column to NULL and asserting the statement itself does not error is a
	// real, sound, disclaimer-free thing to write, and has nothing to do with pgxmock's NULL-scan
	// leniency). Scoped narrowly to "scan" itself rather than a longer word list: every genuine
	// claim this sweep found used that word (see analytics_category_total_display_test.go and
	// account_repository_test.go before their fixes); a claim phrased without it is not this
	// guard's problem to find.
	scanWordRe = regexp.MustCompile(`(?i)\bscan`)
)

// pgxmockNullToleranceViolation is one undisclosed claim found in one file.
type pgxmockNullToleranceViolation struct {
	relPath string
	line    int
	snippet string
}

// scanFileForUndisclosedNullToleranceClaims applies the rule described above to one file's raw
// source text. Split out from the filesystem walk so the meta-tests below can drive it directly
// with constructed text, with no repo file ever written to disk.
func scanFileForUndisclosedNullToleranceClaims(relPath string, src []byte) []pgxmockNullToleranceViolation {
	if buildIntegrationTagRe.Match(src) {
		return nil // real Postgres, not pgxmock — outside this guard's rule
	}
	if !pgxmockImportRe.Match(src) || !addRowNilRe.Match(src) {
		return nil
	}

	var violations []pgxmockNullToleranceViolation
	for _, loc := range nullToleranceClaimRe.FindAllIndex(src, -1) {
		start, end := loc[0], loc[1]
		windowStart := start - 800
		if windowStart < 0 {
			windowStart = 0
		}
		windowEnd := end + 800
		if windowEnd > len(src) {
			windowEnd = len(src)
		}
		if !scanWordRe.Match(src[windowStart:windowEnd]) {
			continue // not actually a scan claim — see scanWordRe's doc comment
		}
		if pgxmockCannotProveNullDisclaimerRe.Match(src[windowStart:windowEnd]) {
			continue // disclosed — this sweep's honest rename/comment pattern
		}
		line := 1 + strings.Count(string(src[:start]), "\n")
		snippet := strings.TrimSpace(string(src[start:end]))
		if len(snippet) > 160 {
			snippet = snippet[:160] + "..."
		}
		violations = append(violations, pgxmockNullToleranceViolation{
			relPath: relPath,
			line:    line,
			snippet: snippet,
		})
	}
	return violations
}

// TestNoUndisclosedPgxmockNullToleranceClaim is the guard itself: it walks backend/internal for
// *_test.go files and fails if any pgxmock-backed, nil-AddRow-using file makes an undisclosed
// NULL-tolerance claim.
func TestNoUndisclosedPgxmockNullToleranceClaim(t *testing.T) {
	root := repoInternalRoot(t)
	// This guard's own file would otherwise match itself: it necessarily contains the claim and
	// disclaimer regex source text side by side, which is prose ABOUT the rule, not a claim under
	// it. Skip it by path rather than trying to make the regexes ignore their own literals.
	selfPath, err := filepath.Abs("pgxmock_null_tolerance_claim_guard_test.go")
	require.NoError(t, err)

	var all []pgxmockNullToleranceViolation
	walkErr := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			base := info.Name()
			if base == "testdata" || base == "vendor" || base == "node_modules" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, "_test.go") {
			return nil
		}
		abs, absErr := filepath.Abs(path)
		if absErr != nil {
			return absErr
		}
		if abs == selfPath {
			return nil
		}
		src, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		rel, relErr := filepath.Rel(filepath.Dir(root), path)
		if relErr != nil {
			rel = path
		}
		all = append(all, scanFileForUndisclosedNullToleranceClaims(rel, src)...)
		return nil
	})
	require.NoError(t, walkErr)

	sort.Slice(all, func(i, j int) bool {
		if all[i].relPath != all[j].relPath {
			return all[i].relPath < all[j].relPath
		}
		return all[i].line < all[j].line
	})

	if len(all) > 0 {
		t.Fatal("undisclosed pgxmock NULL-tolerance claim(s) found — pgxmock scans a nil AddRow " +
			"cell into a plain string without error, so a claim like this cannot fail for the " +
			"reason it asserts. Either prove it against real Postgres (build tag integration) and " +
			"cite that test, or disclose the limitation (see this file's doc comment for the " +
			"phrasing this sweep used):\n" + renderPgxmockNullToleranceViolations(all))
	}
}

func renderPgxmockNullToleranceViolations(vs []pgxmockNullToleranceViolation) string {
	var b strings.Builder
	b.WriteString("undisclosed pgxmock NULL-tolerance claim(s):\n")
	for _, v := range vs {
		b.WriteString(v.relPath)
		b.WriteString(":")
		b.WriteString(itoaSimple(v.line))
		b.WriteString(": ")
		b.WriteString(v.snippet)
		b.WriteString("\n")
	}
	return b.String()
}

func itoaSimple(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var digits []byte
	for n > 0 {
		digits = append([]byte{byte('0' + n%10)}, digits...)
		n /= 10
	}
	if neg {
		return "-" + string(digits)
	}
	return string(digits)
}

// ─── Meta-tests: prove the guard can actually fail, and does not false-positive ────────────────────

func TestPgxmockNullToleranceClaimGuard_FlagsAnUndisclaimedClaim(t *testing.T) {
	src := []byte(`package repository

import (
	"testing"

	"github.com/pashagolub/pgxmock/v4"
)

func TestSomething_NullDisplayScansToEmptyString(t *testing.T) {
	mock, _ := pgxmock.NewPool()
	rows := pgxmock.NewRows([]string{"a"}).AddRow(nil)
	_ = rows
	// NULL must not error: the repository scans it to the empty string.
	_ = mock
}
`)
	violations := scanFileForUndisclosedNullToleranceClaims("fake_test.go", src)
	assert.NotEmpty(t, violations, "an undisclosed NULL-tolerance claim next to a nil AddRow cell must be flagged")
}

func TestPgxmockNullToleranceClaimGuard_IgnoresADisclosedClaim(t *testing.T) {
	src := []byte(`package repository

import (
	"testing"

	"github.com/pashagolub/pgxmock/v4"
)

// This subtest's NULL claim is NOT a NULL-scan proof — pgxmock cannot prove that on its own; see
// the real-Postgres integration test instead.
func TestSomething_NullDisplayCellDoesNotDisturbTheRest(t *testing.T) {
	mock, _ := pgxmock.NewPool()
	rows := pgxmock.NewRows([]string{"a"}).AddRow(nil)
	_ = rows
	// NULL must not error, per the disclaimer above.
	_ = mock
}
`)
	violations := scanFileForUndisclosedNullToleranceClaims("fake_test.go", src)
	assert.Empty(t, violations, "a claim paired with the pgxmock-cannot-prove disclaimer must not be flagged")
}

// TestPgxmockNullToleranceClaimGuard_IgnoresUnrelatedNullText proves the CLAIM regex, not the
// scope gate, is what exonerates ordinary "IS NULL" SQL-shape prose. Until the final #1626 gap
// analysis this fixture's only AddRow was AddRow("value"), so the scanner returned at the
// addRowNilRe scope gate before nullToleranceClaimRe was ever consulted — the false-positive
// property this test's name asserts was never evaluated, and widening the claim regex to match any
// "NULL" text left it green. The fixture therefore now carries a nil AddRow cell (in scope), the
// word "scan" (past scanWordRe), and no disclaimer, so only nullToleranceClaimRe declining to match
// the IS NULL prose keeps the violation list empty. Mutation-confirmed: replacing
// nullToleranceClaimRe with a bare `(?is)\bnull\b` makes THIS test fail while the disclosed-claim
// meta-test stays green.
func TestPgxmockNullToleranceClaimGuard_IgnoresUnrelatedNullText(t *testing.T) {
	src := []byte(`package repository

import (
	"testing"

	"github.com/pashagolub/pgxmock/v4"
)

// Ordinary SQL-shape assertions mention NULL constantly (WHERE x IS NULL) as a predicate the query
// must carry, never as a statement about what a scan does with a NULL cell — these must not trip
// the guard. The nil AddRow cell below keeps this fixture IN the guard's scope on purpose.
func TestSomething_QueryUsesIsNullPredicate(t *testing.T) {
	mock, _ := pgxmock.NewPool()
	rows := pgxmock.NewRows([]string{"a"}).AddRow(nil)
	_ = rows
	_ = mock
	// The scan under test here asserts only the SQL shape: the query filters WHERE x IS NULL and
	// orders by x NULLS LAST, exactly as the repository spells it.
	// mock.ExpectQuery("WHERE x IS NULL")
}
`)
	violations := scanFileForUndisclosedNullToleranceClaims("fake_test.go", src)
	assert.Empty(t, violations,
		"an ordinary IS NULL SQL-shape assertion beside a nil AddRow cell is in scope but is not a "+
			"NULL-tolerance claim, and must not be flagged")
}

func TestPgxmockNullToleranceClaimGuard_IgnoresNullColumnAssignmentUpdates(t *testing.T) {
	src := []byte(`package repository

import (
	"context"
	"testing"

	"github.com/pashagolub/pgxmock/v4"
)

// ClearOverCapacity is idempotent: calling it on a row not currently set (0 rows changed by the
// NULL assignments, or the row simply already all-NULL) must not error. This is an UPDATE ... =
// NULL idempotency claim, not a column-read claim, and must not trip this guard even though the
// file elsewhere uses a nil AddRow cell for something unrelated.
func TestSomething_ClearOverCapacity_NotInGrace_NoOp(t *testing.T) {
	mock, _ := pgxmock.NewPool()
	mock.ExpectExec("UPDATE households SET over_capacity_since = NULL").
		WillReturnResult(pgxmock.NewResult("UPDATE", 0))
	_ = mock
}

func TestSomething_UnrelatedNilAddRowElsewhereInFile(t *testing.T) {
	rows := pgxmock.NewRows([]string{"a"}).AddRow(nil)
	_ = rows
	_ = context.Background()
}
`)
	violations := scanFileForUndisclosedNullToleranceClaims("fake_test.go", src)
	assert.Empty(t, violations, "an UPDATE ... = NULL idempotency claim is not a scan claim and must not be flagged")
}

func TestPgxmockNullToleranceClaimGuard_IgnoresIntegrationTestFiles(t *testing.T) {
	src := []byte(`//go:build integration

package repository

import (
	"testing"
)

// A real-Postgres test may legitimately say "NULL must not error" because it is actually proven
// here, against a real driver — this guard's rule only applies to pgxmock-backed files.
func TestSomething_RealPostgresNullMustNotError(t *testing.T) {
	_ = t
}
`)
	violations := scanFileForUndisclosedNullToleranceClaims("fake_integration_test.go", src)
	assert.Empty(t, violations, "a //go:build integration file runs against real Postgres and is out of scope")
}

// TestNoUndisclosedPgxmockNullToleranceClaim_RepoSweepIsClean is a documentation test: it names the
// exact real-repo state this guard depends on being clean, so a future reader does not have to
// re-derive it. It duplicates no logic — TestNoUndisclosedPgxmockNullToleranceClaim above is the
// actual gate — but pins the count of pgxmock-importing test files under backend/internal at the
// time of the #1626 Phase 2 sweep, so a jump is visible in review.
//
// ⚠ IT REALLY PINS THE NUMBER NOW, and until the final whole-branch review (finding F3) it did
// not: the body counted the files and then asserted `Greater(count, 0)`, which cannot fail while a
// single pgxmock file exists and would have stayed green had the count gone 68 -> 400 or -> 1. That
// is the FIFTH instance of the same vacuity on this branch, added by the very sweep whose subject
// was vacuous tests, in the file whose own doc comment warns "do not declare the guard adequate
// because it currently passes". A `Greater(..., 0)` under a comment claiming to pin a count is not
// a weak test, it is a false comment — the defect class this branch exists to retire.
//
// pgxmockSweepFileCount is therefore the number that was actually swept, asserted with Equal.
// Adding or removing a pgxmock-backed test file turns this red ON PURPOSE: update the constant in
// the same commit that changes the set, and say in the commit message which files moved and that
// each new one is clean under the gate above. That is the whole value of the pin — the gate proves
// no file makes an undisclosed claim, and this proves nobody quietly changed WHICH files the gate
// is looking at.
//
// ⚠ This pins CARDINALITY only, not membership: it asserts how many files import pgxmock, not
// WHICH ones. Swapping one pgxmock-importing file for a different one (delete file A, add file B)
// leaves this count unchanged and green. If membership ever needs to be pinned too, parse the
// import and assert an explicit sorted manifest of paths instead of a bare count.
func TestNoUndisclosedPgxmockNullToleranceClaim_RepoSweepIsClean(t *testing.T) {
	root := repoInternalRoot(t)
	// This guard's own file matches pgxmockImportRe against its OWN source text (the regex's
	// literal pattern spells out the import path it looks for), even though this file does not
	// itself import pgxmock. Exclude it by path, the same way TestNoUndisclosedPgxmockNullToleranceClaim
	// above does, so the count below is actually the number of files that import pgxmock rather
	// than one inflated by the guard counting its own source.
	selfPath, err := filepath.Abs("pgxmock_null_tolerance_claim_guard_test.go")
	require.NoError(t, err)

	count := 0
	walkErr := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			base := info.Name()
			if base == "testdata" || base == "vendor" || base == "node_modules" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, "_test.go") {
			return nil
		}
		abs, absErr := filepath.Abs(path)
		if absErr != nil {
			return absErr
		}
		if abs == selfPath {
			return nil
		}
		src, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		if buildIntegrationTagRe.Match(src) {
			return nil
		}
		if pgxmockImportRe.Match(src) {
			count++
		}
		return nil
	})
	require.NoError(t, walkErr)
	assert.Equal(t, pgxmockSweepFileCount, count,
		"the set of pgxmock-backed, non-integration test files under backend/internal changed: "+
			"expected %d (the count swept by #1626 Phase 2 and re-derived by the final whole-branch "+
			"review), found %d. This is not a failure to route around — update "+
			"pgxmockSweepFileCount in the same commit that changes the set, and record which files "+
			"were added or removed and that each new one is clean under "+
			"TestNoUndisclosedPgxmockNullToleranceClaim above.",
		pgxmockSweepFileCount, count)
}

// pgxmockSweepFileCount is the number of pgxmock-importing, non-integration `_test.go` files under
// backend/internal. Re-derived for the final whole-branch review with the same walk the test
// performs (skipping testdata/vendor/node_modules directories and any file carrying
// `//go:build integration`).
//
// 67 -> 68 (#1631 gap 1): added transaction_date_only_wire_test.go, which asserts that the banking
// read paths populate the date-only display fields. It makes NO null-tolerance claim of any kind —
// its only nil assertion is that a NULL authorized_date leaves AuthorizedDateDisplay nil, which is
// a claim about the Go mapping of a scanned nil, not about the driver tolerating NULL — so it is
// clean under TestNoUndisclosedPgxmockNullToleranceClaim above. No file was removed.
//
// 68 -> 69 (#1631 Gap A1): added transaction_unified_currency_test.go, which asserts that the
// unified feed's investment arm carries investment_transactions.currency_code and that the banking
// arm fabricates none. Its nil assertion is that a banking row's ActivityItem.CurrencyCode stays
// nil — a claim about the Go mapping of a column the banking arm selects as NULL, not about the
// driver tolerating a NULL the query never asked for — so it is clean under
// TestNoUndisclosedPgxmockNullToleranceClaim above. No file was removed.
// 69 -> 73 (#1637): added budget_plan_repository_test.go, budget_expense_rows_test.go,
// budget_expense_snapshot_test.go, and savings_flow_provenance_test.go. They assert query shape,
// transaction boundaries, and mapping, not driver NULL tolerance. No files removed.
// #1652 adds services/registration_revenuecat_test.go: transaction-order tests,
// no NULL-tolerance claims. Included in TestNoUndisclosedPgxmockNullToleranceClaim.
// #1652 adds services/revenuecat_household_projection_test.go and
// revenuecat_compatibility_test.go: SQL/session behavior and typed scan fixtures,
// no claims about real PostgreSQL NULL tolerance. Both pass the claim sweep.
// #1652 additionally adds revenuecat_retention_guard_test.go and
// revenuecat_checkout_repository_test.go: lock/transaction and checkout admission
// contracts only, with no driver NULL-tolerance claims.
// 73 -> 74 (#1650): added analytics_authority_repository_test.go, which pins consent/identity
// locking and fail-closed query behavior. It makes no driver NULL-tolerance claim. No files removed.
// 74 -> 75 (#1650): added posthog_erasure_outbox_test.go, which asserts transactional statement
// ordering, encrypted identity capture, and two separated operator confirmations. It makes no
// driver NULL-tolerance claim. No files removed.
// 80 -> 81 (#1652 todo 10): added services/household_entitlement_projection_analytics_test.go,
// which drives AcceptInvite through the real PostHog Gateway and asserts the household projection
// event's name, exact properties, and consent boundary. It makes no driver NULL-tolerance claim.
// No files removed.
//
// 75 -> 76 (#1683 Task 4): added audit_log_repository_test.go, which pins the metadata INSERT shape
// (VALUES (NULL, plus all four metadata columns), the erasure redaction SET clause, and the
// pgxmock-bound metadata write including NULL binding for absent metadata. It makes no
// driver NULL-tolerance claim — it uses no AddRow at all — so it is clean under
// TestNoUndisclosedPgxmockNullToleranceClaim above. No file was removed.
//
// 76 -> 77 (#1683 Task 5): privacy_service_test.go newly imports pgxmock — it drives the service's
// Begin/Commit/Rollback transaction boundary, the locked read-modify-write statement sequence, and
// the in-transaction audit INSERT. It makes no driver NULL-tolerance claim: every AddRow seeds all
// fourteen privacy_settings columns explicitly and the audit metadata binds []byte(nil) for
// `changes`, so it is clean under TestNoUndisclosedPgxmockNullToleranceClaim above. No file was
// added or removed — the file already existed without the import.
//
// ⚠ VERIFIED (#1652 × #1688 merge): 83 is the guard-computed count, not a placeholder. It is the
// value TestNoUndisclosedPgxmockNullToleranceClaim_RepoSweepIsClean itself derives — that test's
// Equal assertion walks backend/internal exactly as described above and returns 83 — and the same
// walk reproduced independently with a shell equivalent returns 83 as well. The merge combined two
// disjoint additions to the pgxmock-importing set: this branch (#1652) added the RevenueCat
// test files — services/registration_revenuecat_test.go,
// services/revenuecat_household_projection_test.go, repository/revenuecat_compatibility_test.go,
// repository/revenuecat_retention_guard_test.go, repository/revenuecat_checkout_repository_test.go,
// and (todo 10) services/household_entitlement_projection_analytics_test.go — while staging (#1688)
// added repository/audit_log_repository_test.go and services/privacy_service_test.go. Every one of
// these is clean under TestNoUndisclosedPgxmockNullToleranceClaim above: none asserts that the
// driver tolerates a NULL cell it scanned (the RevenueCat files test transaction order, SQL/session
// shape, lock/checkout contracts, and household projection; the #1688 pair tests metadata INSERT
// shape and the Begin/Commit/Rollback boundary). No file was removed on either side.
const pgxmockSweepFileCount = 83
