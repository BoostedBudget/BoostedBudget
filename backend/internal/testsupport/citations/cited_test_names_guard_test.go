// Package citations holds the guard for cross-references written in Go comments.
package citations

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"

	"github.com/maxnussbaum/BoostedBudget/internal/testsupport/sourcefiles"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// Issue #1569 Task 19 — citation rot.
//
// This plan hit three separate citation incidents: drifted anchors, FABRICATED TEST NAMES, and
// eight wrong line numbers — plus two citations that were already stale at HEAD before the plan
// started. A comment saying "pinned by TestFoo" is load-bearing: it is how the next reader
// decides a behaviour is already covered, and how a reviewer decides a change is safe. When
// TestFoo does not exist, the comment is worse than absent, because it is believed.
//
// WHAT THIS GUARD DOES: every `TestXxx` identifier appearing in a comment in Go source under
// backend/ — a Go comment, or an SQL comment inside a string literal (#1569 T8) — must resolve to
// something real — an actual `func TestXxx(`, a declared identifier
// of that name, or a PREFIX of a real test name.
//
// WHY THE PREFIX RULE. `go test -run TestAccountRepository_PurgeAccount` is the normal way to cite a
// FAMILY of tests, and gofmt wraps long names across lines. Both are legitimate and neither names a
// function that exists. The rule is deliberately lenient in this one direction: a guard that fires
// on every `-run` line in the tree gets suppressed, and then it catches nothing.
//
// WHAT IT DOES NOT DO, and this is the honest limit: it says NOTHING about line numbers. The bare
// `:NNNN` form is unswept corpus-wide, a line number is right only until the next edit above it,
// and a guard that re-checked them would fail on every unrelated commit. Names are checked because
// names are stable and a wrong one is unambiguously wrong. The existing method-attribution guard
// covers a different axis again.
//
// SCOPE: tracked Go source only. Markdown is scanned by its own sibling guard,
// markdown_citations_guard_test.go (#1569 task 20, A6), which shares this file's citationVerdict
// and adds two rules Go does not need: intra-document `Decision N` cross-references, and backticked
// repository paths. Read that file's header before assuming Markdown is now covered — two of the
// three documentation defects that motivated it were false factual claims, which no guard catches.
//
// A RATCHET, not a ban. 33 broken citations exist at HEAD; they are listed below rather than fixed,
// because fixing one means finding out what the author MEANT, which is 33 separate investigations
// and not this task. The number must not go up.
//
// Mutation-checked: citing a nonexistent test name from any tracked Go comment turns this test red
// naming the file and line; see task-19-report.md.

// brokenCitationBaseline is the set of cited test names that do not resolve today, each with where
// it is cited. Remove an entry when you fix the citation — the guard will tell you to.
//
// These are not merely cosmetic: each one is a claim that some behaviour is pinned by a test, made
// to a reader who has no way to check it short of grepping.
var brokenCitationBaseline = map[string]bool{
	"TestAnalyticsRepository_ActiveAccountFilter_RejectsReplacedAccounts":                    true,
	"TestCacheInvalidation_GenuineFailureDoesNotBump":                                        true,
	"TestCloudflareOnly_MetricsBypassed":                                                     true,
	"TestCloudflareOnly_MetricsTrailingSlashBypassed":                                        true,
	"TestConfirmTransferSuggestionMatch_PassesObservedReservationToRepo":                     true,
	"TestDeleteUserCascade_Integration_ReplacedStatusLinkedItemDoesNotBlock_Deletes":         true,
	"TestDisconnectInstitution_PlaidFailureButDatabaseSucceeds":                              true,
	"TestExecuteDeletion_CascadeFailsTransientlyAfterPaddleCancel_RecordsNoResidue":          true,
	"TestExecuteDeletion_Integration_OrdinarySuccess_LeavesNoResidueRow":                     true,
	"TestExecuteDeletion_ResidueWriteFails_StillReturnsTheLegitimateOutcome":                 true,
	"TestExistingCategoryEvaluator_Evaluate":                                                 true,
	"TestGetUsersWithExpiredRetention_Integration":                                           true,
	"TestJobArgsKindConsistency":                                                             true,
	"TestJobBacklogMetrics_FieldsRoundTrip":                                                  true,
	"TestJobBacklogMetrics_Validation":                                                       true,
	"TestLinkedItemRepository_GetByUserID_ExcludesReplacedByDefault":                         true,
	"TestPlaidCleanup_DisconnectIfEligible_NewItemAppearsButNotLive_NotAborted":              true,
	"TestPlaidDisconnectRepair_Integration_LapsedCompStillRepaired":                          true,
	"TestRateLimitRejections_HasBoundedTierLabels":                                           true,
	"TestReadOnlyMiddleware_POST_CompedUser_EffectiveActive_Allowed":                         true,
	"TestRecordDeletionResidue_Integration_CustomerArchived_IsCountedByTheExistingNamespace": true,
	"TestRegisterUser_TransactionAtomicity":                                                  true,
	"TestResolveEffectiveMerchantNames_NoMerchantID_Integration":                             true,
	"TestRetentionCleanupCheck_PlaidRepairNilCleanup_FailsJob_HeartbeatNotAdvanced":          true,
	"TestRetentionCleanupCheck_PlaidRepair_ClearsMarkersOnEligibleStatus_":                   true,
	"TestRiver_ClientRegistration":                                                           true,
	"TestSavingsProgress_StickyCompletion_NotClearedOnDrop":                                  true,
	"TestSystemHealth_RedisDownDoesNotChangeOverallStatus":                                   true,
	"TestUpdateUserTier_Integration_ClearsPlaidCleanupPending":                               true,
	"TestUserAccountService_MatchOrCreate_NullMask_CreatesNew":                               true,
	"TestWorkClaims_Claim_EligibleResourceIsProcessedEndToEnd":                               true,
	"TestWorker_SubscriptionCanceled_ClaimLost_SkipsDestructive":                             true,
}

var citedTestName = regexp.MustCompile(`\bTest[A-Z][A-Za-z0-9_]{4,}\b`)

// sqlComment matches the two SQL comment forms as they appear inside a Go string literal.
var sqlComment = regexp.MustCompile(`(?s)--[^\n]*|/\*.*?\*/`)

// sqlCommentCitations returns the test names cited from SQL comments inside one string literal.
// Pulled out as a function so TestSQLCommentCitations_AreExtracted can construct the case; the
// alternative is a meta-test that can only assert the corpus is green, which is what let this hole
// sit open.
func sqlCommentCitations(text string) []string {
	var out []string
	for _, span := range sqlComment.FindAllString(text, -1) {
		out = append(out, citedTestName.FindAllString(span, -1)...)
	}
	return out
}

// citationVerdict is the whole decision, pure so the meta-tests can construct each case.
// `defined` is the set of real `func TestXxx` names; `declared` is every other identifier of that
// shape (a struct called TestBoostOutput is not a citation).
func citationVerdict(cited string, defined, declared map[string]bool) bool {
	if defined[cited] || declared[cited] {
		return true
	}
	for name := range defined {
		if strings.HasPrefix(name, cited) {
			return true
		}
	}
	return false
}

// TestEveryCitedTestNameResolves is the guard.
func TestEveryCitedTestNameResolves(t *testing.T) {
	// Repo root: backend/internal/testsupport/citations -> ... -> repo root.
	const repoRoot = "../../../.."

	files, err := sourcefiles.Union(repoRoot, "backend", sourcefiles.HasExt(".go"))
	if err != nil {
		t.Fatalf("listing backend Go source: %v — this guard must fail closed rather than scan nothing", err)
	}

	defined := map[string]bool{}
	declared := map[string]bool{}
	type site struct {
		file string
		line int
	}
	cites := map[string][]site{}
	scanned := 0

	for _, rel := range files {
		fset := token.NewFileSet()
		file, parseErr := parser.ParseFile(fset, filepath.Join(repoRoot, rel), nil, parser.ParseComments)
		if parseErr != nil {
			t.Fatalf("parsing %s: %v", rel, parseErr)
		}
		scanned++

		ast.Inspect(file, func(n ast.Node) bool {
			switch d := n.(type) {
			case *ast.FuncDecl:
				if strings.HasPrefix(d.Name.Name, "Test") {
					if d.Recv == nil && strings.HasSuffix(rel, "_test.go") {
						defined[d.Name.Name] = true
					}
					declared[d.Name.Name] = true
				}
			case *ast.TypeSpec:
				if strings.HasPrefix(d.Name.Name, "Test") {
					declared[d.Name.Name] = true
				}
			case *ast.ValueSpec:
				for _, id := range d.Names {
					if strings.HasPrefix(id.Name, "Test") {
						declared[id.Name] = true
					}
				}
			}
			return true
		})

		for _, group := range file.Comments {
			for _, c := range group.List {
				for _, m := range citedTestName.FindAllString(c.Text, -1) {
					line := fset.Position(c.Pos()).Line
					cites[m] = append(cites[m], site{rel, line})
				}
			}
		}

		// A COMMENT IS NOT ALWAYS A GO COMMENT. file.Comments holds `//` and `/* */` only, so a
		// citation written inside an SQL comment in a raw string literal — which is where this
		// branch's `-- tombstone-exempt:` reasons live, four of which cite a test name — was
		// invisible to this guard. savings_contribution_repository.go went as far as duplicating
		// two of them into a real Go comment specifically so they would be checked.
		//
		// Only SQL COMMENT text is scanned, not every literal: the baseline map above and this
		// file's own meta-tests are full of deliberately fabricated names as ordinary string data,
		// and scanning those would make the guard report itself.
		ast.Inspect(file, func(n ast.Node) bool {
			lit, ok := n.(*ast.BasicLit)
			if !ok || lit.Kind != token.STRING {
				return true
			}
			text, unqErr := strconv.Unquote(lit.Value)
			if unqErr != nil {
				return true
			}
			line := fset.Position(lit.Pos()).Line
			for _, m := range sqlCommentCitations(text) {
				cites[m] = append(cites[m], site{rel, line})
			}
			return true
		})
	}

	// Fail closed. Zero scanned files, or a defined-set that failed to populate, would make every
	// citation resolve-or-not for the wrong reason.
	if scanned == 0 {
		t.Fatal("scanned no Go files at all — the walk is broken")
	}
	if len(defined) < 1000 {
		t.Fatalf("found only %d test functions in %d files; the tree has thousands. The AST walk is "+
			"broken and every citation would be reported as unresolved", len(defined), scanned)
	}
	if len(cites) == 0 {
		t.Fatal("found no test-name citations at all — the comment scan is broken")
	}

	var added, fixed []string
	stillBroken := map[string]bool{}
	for name, sites := range cites {
		if citationVerdict(name, defined, declared) {
			continue
		}
		stillBroken[name] = true
		if !brokenCitationBaseline[name] {
			locs := make([]string, 0, len(sites))
			for _, s := range sites {
				locs = append(locs, s.file+":"+itoa(s.line))
			}
			sort.Strings(locs)
			added = append(added, name+" cited at "+strings.Join(locs, ", "))
		}
	}
	for name := range brokenCitationBaseline {
		if !stillBroken[name] {
			fixed = append(fixed, name)
		}
	}
	sort.Strings(added)
	sort.Strings(fixed)

	if len(added) > 0 {
		t.Errorf("%d comment(s) cite a test name that does not exist (#1569).\n"+
			"A comment saying a behaviour is pinned by TestFoo is how the next reader decides not to "+
			"write that test. If the name is a typo, fix it; if the test was renamed, re-point it; if "+
			"it never existed, delete the claim:\n    %s", len(added), strings.Join(added, "\n    "))
	}
	if len(fixed) > 0 {
		t.Errorf("%d baselined broken citation(s) now resolve — remove them from "+
			"brokenCitationBaseline so the fix is locked in:\n    %s", len(fixed), strings.Join(fixed, "\n    "))
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}

// ---- Meta-tests: the constructed cases. --------------------------------------------------------

// TestCitationVerdict_TheCasesThatMatter covers each rule, in both directions. The negative cases
// are the point: a verdict function that returned true for everything would satisfy the positive
// ones and catch nothing.
func TestCitationVerdict_TheCasesThatMatter(t *testing.T) {
	defined := map[string]bool{
		"TestAccountRepository_PurgeAccount_Integration_TransactionsSurvive": true,
		"TestSomethingElse": true,
	}
	declared := map[string]bool{"TestBoostOutput": true}

	cases := []struct {
		name  string
		cited string
		want  bool
	}{
		{"exact match resolves", "TestSomethingElse", true},
		{"a -run prefix resolves", "TestAccountRepository_PurgeAccount", true},
		{"a declared type is not a citation", "TestBoostOutput", true},
		{"a fabricated name does NOT resolve", "TestAccountRepository_PurgeAccount_KeepsEverything", false},
		{"a renamed test does NOT resolve", "TestSomethingElseEntirely", false},
		{"a superstring of a real name does NOT resolve", "TestSomethingElseX", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := citationVerdict(tc.cited, defined, declared); got != tc.want {
				t.Fatalf("citationVerdict(%q) = %v, want %v", tc.cited, got, tc.want)
			}
		})
	}
}

// TestCitationRegexp_MatchesWhatACommentActuallyLooksLike pins the extraction, because a regexp that
// matched nothing would make the guard pass on a tree full of broken citations.
func TestCitationRegexp_MatchesWhatACommentActuallyLooksLike(t *testing.T) {
	got := citedTestName.FindAllString(
		"// Pinned by TestFoo_Baz and TestOther. See also `TestThird_One`; not Test or Testing.", -1)
	want := []string{"TestFoo_Baz", "TestOther", "TestThird_One"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("got %v, want %v", got, want)
	}
}

// TestCitationBaseline_IsBoundedAndNonEmpty guards the guard: an emptied baseline is how a failure
// gets "fixed", and a growing one is how the ratchet stops ratcheting.
func TestCitationBaseline_IsBoundedAndNonEmpty(t *testing.T) {
	if len(brokenCitationBaseline) == 0 {
		t.Fatal("brokenCitationBaseline is empty — either every citation was fixed (delete the " +
			"baseline and make this a flat ban) or the ratchet has been disabled")
	}
	if len(brokenCitationBaseline) > 33 {
		t.Fatalf("the baseline holds %d entries, above the 33 recorded when this guard was written; "+
			"the ratchet only turns one way", len(brokenCitationBaseline))
	}
}

// TestSQLCommentCitations_AreExtracted is the constructed case for #1569 T8. Before it, a
// `-- tombstone-exempt:` reason citing a fabricated test name left this guard green, because
// file.Comments does not contain the inside of a raw string literal. Three such reasons on this
// branch cite a test name and had no other copy anywhere a guard could see.
func TestSQLCommentCitations_AreExtracted(t *testing.T) {
	cases := []struct {
		name string
		text string
		want string
	}{
		{
			"a line comment inside SQL",
			// Split so this fixture is not itself a citation — the guard scans this file too.
			"SELECT 1 FROM t\n-- tombstone-exempt(t): pinned by " + "Test" + "Fabricated_DoesNotExist.\nWHERE x = 1",
			"Test" + "Fabricated_DoesNotExist",
		},
		{
			"a block comment inside SQL",
			"SELECT 1 /* history filter, see " + "Test" + "SomeOther_Thing */ FROM t",
			"Test" + "SomeOther_Thing",
		},
		{
			"ordinary string data is not an SQL comment",
			`brokenCitationBaseline holds "` + "Test" + `NotACitation_JustData"`,
			"",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := strings.Join(sqlCommentCitations(tc.text), ",")
			if got != tc.want {
				t.Fatalf("sqlCommentCitations = %q, want %q", got, tc.want)
			}
		})
	}
}
