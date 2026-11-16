// Package citations — the Markdown half. #1569 Task 20 (A6).
package citations

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/maxnussbaum/BoostedBudget/internal/testsupport/sourcefiles"
)

// Issue #1569 Task 20 (A6). Task 19's citation guard resolves cited test names in GO COMMENTS ONLY.
// That is the wrong half of the tree for this plan's actual documentation defects.
//
// ═══ THE EVIDENCE THAT MOTIVATED THIS, AND WHAT IT ACTUALLY SAYS ════════════════════════════════
//
// All THREE documentation defects found across two review rounds shipped in MARKDOWN, in the
// retention ADR (docs/decisions/2026-08-24-financial-history-retained-by-default.md). Three for
// three. But the shape matters more than the count, and it argues AGAINST simply pointing the
// existing name-resolver at *.md and calling the gap closed:
//
//   1. a `Decision N` cross-reference that pointed at the WRONG decision;
//   2. a false factual claim about behaviour;
//   3. a second false factual claim about behaviour.
//
// NONE of the three was a `Test*` name. A name-resolver extended to Markdown would have caught
// ZERO of them. So this file does two things and is honest about the third:
//
//   RULE 1 (test names) — extends task 19's verdict to Markdown. Justified not by those three
//   defects but by measurement: 26 unresolved test-name citations exist in scoped Markdown today,
//   i.e. the rot is real here, it simply is not what the ADR reviews happened to find.
//
//   RULE 2 (numbered cross-references) — the closest mechanically checkable thing to defect 1. A
//   `Decision N` reference in a document that HAS numbered decision headings must name one that
//   exists. ⚠ This catches an OUT-OF-RANGE number. It does NOT catch defect 1 as it actually
//   shipped, which was a reference to a decision that EXISTS and says something else. Nothing
//   mechanical can catch that: it requires reading both the reference and its target and judging
//   whether they are about the same thing. Stated here rather than glossed, because a guard whose
//   docstring implies it covers the motivating defect is itself a false claim.
//
//   RULE 3 (file paths) — a backticked repository path in scoped Markdown must resolve on disk.
//   Same class as the Go guard's highest-yield rule.
//
// ═══ WHAT IS NOT MECHANICALLY CHECKABLE, PLAINLY ════════════════════════════════════════════════
//
//   - A FALSE FACTUAL CLAIM about behaviour — defects 2 and 3, i.e. TWO OF THE THREE. "X is safe
//     because Y" is a statement about the code, and deciding it requires the reasoning the claim
//     was supposed to have done. No regexp reaches it. The only control that has actually worked on
//     this branch is a reviewer reproducing the claim.
//   - A cross-reference that resolves but MISLEADS — defect 1's real shape, above.
//   - A CROSS-DOCUMENT `Decision N` ("see the decision record's Decision 1"). Rule 2 fires only
//     inside a document that numbers its own decisions, precisely so this common and legitimate
//     form is not flagged; the cost is that its target is unchecked.
//   - LINE NUMBERS. Same reasoning task 19 gave: right until the next edit above them.
//
// So: this closes the Markdown half of a name-and-path guard. It does not close "the ADR was
// wrong", and no guard in this repository does.
//
// SCOPE, AND WHAT IT LEAVES OUT. Tracked *.md at the repository ROOT or under docs/ — 50 of the
// 430 tracked Markdown files. Vendored skill trees (.agent*/, .claude/) and the sdd working notes
// (.superpowers/) are excluded deliberately: third-party or scratch, and neither is a claim this
// repository makes.
//
// ⚠ But roughly two dozen PROJECT-OWNED Markdown files are outside that scope and are therefore
// unguarded: backend/docs/, apps/web/, .github/, apps/mcp-server/, conductor/. Do not read this
// guard as "Markdown is now covered" — it covers the root and docs/. Widening it is a matter of
// changing mdInScope and absorbing whatever baseline that produces; it was not done here because
// the baselines were measured against this scope.
//
// A RATCHET, like task 19's, for rules 1 and 3. Rule 2 is a flat ban: it is clean today.

// mdScopedRoots decides which tracked Markdown this guard speaks for.
func mdInScope(rel string) bool {
	if strings.HasPrefix(rel, "docs/") {
		return true
	}
	return !strings.Contains(rel, "/")
}

// brokenMDTestCitations — RULE 1's baseline. Each entry is "TestName @ path". Keyed by BOTH so a
// name that is broken in an old planning document cannot excuse the same name in an ADR.
var brokenMDTestCitations = map[string]bool{
	"TestAccountFingerprint @ docs/superpowers/specs/2026-04-09-plaid-item-uniqueness-design.md":                              true,
	"TestAnomalyDetection_QuickChurn @ docs/designs/account-deletion-transaction-management.md":                               true,
	"TestCronProvider_EnforcesExecutionLimit @ docs/superpowers/specs/2026-04-03-simplify-880-56-design.md":                   true,
	"TestExecuteDeletion_PaddleArchiveFails_DeletionContinues @ docs/superpowers/plans/2026-04-20-paddle-deletion-cleanup.md": true,
	"TestExecuteDeletion_PaddleCancelFails_DeletionContinues @ docs/superpowers/plans/2026-04-20-paddle-deletion-cleanup.md":  true,
	"TestExecuteDeletion_PaddleRunsBeforePlaidAndDB @ docs/superpowers/plans/2026-04-20-paddle-deletion-cleanup.md":           true,
	"TestFullDeletionCycle @ docs/designs/account-deletion-transaction-management.md":                                         true,
	"TestLifetimeLimitEnforcement @ docs/designs/account-deletion-transaction-management.md":                                  true,
	"TestManualProvider_TriggerRule_ExecutionLimitExceeded @ docs/superpowers/specs/2026-04-03-simplify-880-56-design.md":     true,
	"TestMultiRegionUserCreation @ docs/planning/boosted-budget-complete-plan.md":                                             true,
	"TestSoftDeleteAccount @ docs/designs/account-deletion-transaction-management.md":                                         true,
	"TestWorker_FindsScheduledUsers @ docs/superpowers/plans/1464-codex-round5-residuals-ledger.md":                           true,
}

// unresolvedMDPathCitations — RULE 3's baseline, same "citation @ path" keying and same reason.
// Concentrated in historical planning and status documents that record a tree that has since been
// refactored; they are listed rather than rewritten because rewriting them would mean deciding what
// each author meant, which is 64 separate investigations.
var unresolvedMDPathCitations = map[string]bool{
	"api/transactionsApi.ts @ docs/planning/issue-25-transactions-list-ui.md":                                                             true,
	"apps/web/e2e/account-deletion.spec.ts @ docs/designs/account-deletion-transaction-management.md":                                     true,
	"apps/web/src/features/accounts/UpgradeBanner.tsx @ docs/superpowers/plans/2026-06-25-remove-tier-limits.md":                          true,
	"apps/web/src/features/accounts/UpgradeBanner.tsx @ docs/superpowers/specs/2026-06-25-remove-tier-limits-design.md":                   true,
	"apps/web/src/features/accounts/components/AccountLimitsBanner.tsx @ docs/designs/account-deletion-transaction-management.md":         true,
	"apps/web/src/features/accounts/components/DeleteAccountModal.tsx @ docs/designs/account-deletion-transaction-management.md":          true,
	"apps/web/src/features/rules/components/RuleBuilder.tsx @ docs/superpowers/specs/2026-04-03-simplify-880-56-design.md":                true,
	"apps/web/src/features/rules/components/RuleTable.tsx @ docs/superpowers/specs/2026-05-28-transactions-spacing-filter-sort-design.md": true,
	"apps/web/src/features/rules/components/RulesPage.tsx @ docs/superpowers/specs/2026-04-03-simplify-880-56-design.md":                  true,
	"apps/web/src/features/rules/components/TriggerSelector.tsx @ docs/superpowers/specs/2026-04-03-simplify-880-56-design.md":            true,
	"apps/web/src/features/rules/hooks/useRuleTierLimits.test.ts @ docs/superpowers/specs/2026-04-03-simplify-880-56-design.md":           true,
	"apps/web/src/features/rules/hooks/useRuleTierLimits.ts @ docs/superpowers/specs/2026-04-03-simplify-880-56-design.md":                true,
	"apps/web/src/features/rules/index.ts @ docs/superpowers/specs/2026-04-03-simplify-880-56-design.md":                                  true,
	"apps/web/src/features/rules/types.ts @ docs/superpowers/specs/2026-04-03-simplify-880-56-design.md":                                  true,
	"apps/web/src/features/subscription/hooks/useTierLimits.test.ts @ docs/superpowers/plans/2026-06-25-remove-tier-limits.md":            true,
	"apps/web/src/features/subscription/hooks/useTierLimits.test.ts @ docs/superpowers/specs/2026-06-25-remove-tier-limits-design.md":     true,
	"apps/web/src/features/subscription/hooks/useTierLimits.ts @ docs/superpowers/plans/2026-06-25-remove-tier-limits.md":                 true,
	"apps/web/src/features/subscription/hooks/useTierLimits.ts @ docs/superpowers/specs/2026-06-25-remove-tier-limits-design.md":          true,
	"apps/web/src/features/transactions/api/transactionsApi.test.ts @ docs/planning/issue-25-transactions-list-ui.md":                     true,
	"apps/web/src/features/transactions/api/transactionsApi.ts @ docs/planning/issue-25-transactions-list-ui.md":                          true,
	"apps/web/src/features/transactions/hooks/useTransactions.test.tsx @ docs/planning/issue-25-transactions-list-ui.md":                  true,
	"apps/web/src/features/transactions/hooks/useTransactions.ts @ docs/planning/issue-25-transactions-list-ui.md":                        true,
	"apps/web/src/shared/charts/__tests__/useDarkMode.test.ts @ docs/superpowers/plans/2026-04-07-dark-mode-toggle-css-consolidation.md":  true,
	"apps/web/src/test/mocks/rule-tier-limits.ts @ docs/superpowers/specs/2026-04-03-simplify-880-56-design.md":                           true,
	"backend/internal/domain/automation_rule.go @ docs/superpowers/specs/2026-04-03-simplify-880-56-design.md":                            true,
	"backend/internal/domain/automation_rule_test.go @ docs/superpowers/specs/2026-04-03-simplify-880-56-design.md":                       true,
	"backend/internal/handlers/plaid_handler.go @ docs/planning/issue-25-transactions-list-ui.md":                                         true,
	"backend/internal/handlers/transaction_handler.go @ docs/planning/issue-25-transactions-list-ui.md":                                   true,
	"backend/internal/huma/operations/automation_rules.go @ docs/superpowers/specs/2026-04-03-simplify-880-56-design.md":                  true,
	"backend/internal/services/anomaly_detector.go @ docs/designs/account-deletion-transaction-management.md":                             true,
	"backend/internal/services/rule_execution_service.go @ docs/superpowers/specs/2026-04-03-simplify-880-56-design.md":                   true,
	"backend/internal/services/rule_execution_service_test.go @ docs/superpowers/specs/2026-04-03-simplify-880-56-design.md":              true,
	"backend/internal/workers/deletion_worker.go @ docs/decisions/2026-08-01-gdpr-erasure-hard-delete.md":                                 true,
	"backend/internal/workers/subscription_limit_adapter.go @ docs/superpowers/specs/2026-04-03-simplify-880-56-design.md":                true,
	"backend/internal/workers/subscription_limit_adapter_test.go @ docs/superpowers/specs/2026-04-03-simplify-880-56-design.md":           true,
	"backend/migrations/000010_create_transactions_table.sql @ docs/planning/issue-25-transactions-list-ui.md":                            true,
	"e2e/transactions-list.spec.ts @ docs/planning/issue-25-transactions-list-ui.md":                                                      true,
	"filters/TransactionAccountFilter.tsx @ docs/superpowers/plans/2026-04-07-dark-mode-toggle-css-consolidation.md":                      true,
	"filters/TransactionAmountFilter.tsx @ docs/superpowers/plans/2026-04-07-dark-mode-toggle-css-consolidation.md":                       true,
	"filters/TransactionCategoryFilter.tsx @ docs/superpowers/plans/2026-04-07-dark-mode-toggle-css-consolidation.md":                     true,
	"filters/TransactionStatusFilter.tsx @ docs/superpowers/plans/2026-04-07-dark-mode-toggle-css-consolidation.md":                       true,
	"filters/TransactionTypeFilter.tsx @ docs/superpowers/plans/2026-04-07-dark-mode-toggle-css-consolidation.md":                         true,
	"hooks/useTransactions.ts @ docs/planning/issue-25-transactions-list-ui.md":                                                           true,
	"internal/handlers/transaction_handler.go @ docs/planning/issue-25-transactions-list-ui.md":                                           true,
	"internal/handlers/transaction_handler_test.go @ docs/planning/issue-25-transactions-list-ui.md":                                      true,
	"internal/workers/deletion_worker.go @ docs/decisions/2026-08-01-gdpr-erasure-hard-delete.md":                                         true,
	"requirements-gap-analysis/SKILL.md @ AGENTS.md":                                                                                      true,
	"requirements-gap-analysis/SKILL.md @ CLAUDE.md":                                                                                      true,
	"scripts/test-real-implementations.sh @ docs/superpowers/specs/2026-04-03-simplify-880-56-design.md":                                  true,
}

var (
	mdCitedTestName = regexp.MustCompile(`\bTest[A-Z][A-Za-z0-9_]{4,}\b`)
	// A backticked repository path, optionally with :NNN or :NNN-NNN.
	mdCitedPath = regexp.MustCompile("`([A-Za-z0-9_./-]+\\.(?:go|sql|ts|tsx|md|yaml|yml|sh|json))(?::[0-9]+(?:-[0-9]+)?)?`")
	// "## 3. Plaid cannot delete anything", "## Decision 3 — ...", "### Decision 3: ...".
	mdDecisionHeading = regexp.MustCompile(`(?m)^#{2,4}\s+(?:Decision\s+)?([0-9]+)\s*[.:\x{2014}\x{2013}-]`)
	mdDecisionRef     = regexp.MustCompile(`\bDecision ([0-9]+)\b`)
)

// mdPathResolves mirrors the prose conventions actually used: a path may be written from the
// repository root or relative to backend/, backend/internal/, apps/web/ or apps/web/src/.
func mdPathResolves(repoRoot, cited string) bool {
	for _, r := range []string{".", "backend", "backend/internal", "apps/web", "apps/web/src"} {
		if _, err := os.Stat(filepath.Join(repoRoot, r, cited)); err == nil {
			return true
		}
	}
	return false
}

func scopedMarkdownFiles(t *testing.T, repoRoot string) []string {
	t.Helper()
	all, err := sourcefiles.Union(repoRoot, ".", sourcefiles.HasExt(".md"))
	if err != nil {
		t.Fatalf("listing Markdown: %v — this guard must fail closed rather than scan nothing", err)
	}
	var files []string
	for _, rel := range all {
		if mdInScope(rel) {
			files = append(files, rel)
		}
	}
	sort.Strings(files)
	return files
}

// definedGoTestNames is task 19's `defined` set, rebuilt here rather than shared, because that one
// is a local inside its guard. Both read the same corpus (sourcefiles.Union), so they cannot
// disagree about what exists.
func definedGoTestNames(t *testing.T, repoRoot string) map[string]bool {
	t.Helper()
	files, err := sourcefiles.Union(repoRoot, "backend", sourcefiles.HasExt("_test.go"))
	if err != nil {
		t.Fatalf("listing backend tests: %v", err)
	}
	re := regexp.MustCompile(`(?m)^func (Test[A-Za-z0-9_]+)\(`)
	defined := map[string]bool{}
	for _, rel := range files {
		b, err := os.ReadFile(filepath.Join(repoRoot, rel))
		if err != nil {
			continue
		}
		for _, m := range re.FindAllStringSubmatch(string(b), -1) {
			defined[m[1]] = true
		}
	}
	return defined
}

const mdRepoRoot = "../../../.."

// RULE 1.
func TestMarkdownCitedTestNamesResolve(t *testing.T) {
	files := scopedMarkdownFiles(t, mdRepoRoot)
	if len(files) < 20 {
		t.Fatalf("only %d scoped Markdown file(s) found; the tree has dozens. The scan is broken and "+
			"a pass here would mean nothing", len(files))
	}
	defined := definedGoTestNames(t, mdRepoRoot)
	if len(defined) < 1000 {
		t.Fatalf("found only %d Go test functions; the tree has thousands. Every citation would be "+
			"reported unresolved", len(defined))
	}

	seen := map[string]bool{}
	var added []string
	for _, rel := range files {
		b, err := os.ReadFile(filepath.Join(mdRepoRoot, rel))
		if err != nil {
			t.Fatalf("reading %s: %v", rel, err)
		}
		for _, name := range mdCitedTestName.FindAllString(string(b), -1) {
			if citationVerdict(name, defined, map[string]bool{}) {
				continue
			}
			key := name + " @ " + rel
			seen[key] = true
			if !brokenMDTestCitations[key] {
				added = append(added, key)
			}
		}
	}
	sort.Strings(added)
	if len(added) > 0 {
		t.Errorf("%d Markdown citation(s) name a test that does not exist (#1569 A6).\n"+
			"Documentation saying a behaviour is pinned by TestFoo is how the next reader decides not "+
			"to write that test:\n    %s", len(added), strings.Join(added, "\n    "))
	}
	var fixed []string
	for key := range brokenMDTestCitations {
		if !seen[key] {
			fixed = append(fixed, key)
		}
	}
	sort.Strings(fixed)
	if len(fixed) > 0 {
		t.Errorf("%d baselined Markdown test citation(s) now resolve — remove them from "+
			"brokenMDTestCitations so the fix is locked in:\n    %s", len(fixed), strings.Join(fixed, "\n    "))
	}
}

// RULE 2 — a flat ban, not a ratchet: no document in scope violates it today.
func TestMarkdownDecisionCrossReferencesResolve(t *testing.T) {
	files := scopedMarkdownFiles(t, mdRepoRoot)
	var problems []string
	checked := 0
	for _, rel := range files {
		b, err := os.ReadFile(filepath.Join(mdRepoRoot, rel))
		if err != nil {
			t.Fatalf("reading %s: %v", rel, err)
		}
		text := string(b)
		numbered := map[int]bool{}
		for _, m := range mdDecisionHeading.FindAllStringSubmatch(text, -1) {
			n, _ := strconv.Atoi(m[1])
			numbered[n] = true
		}
		// A document that does not number its own decisions is out of scope for this rule: its
		// "Decision N" is either a common noun ("Architecture Decisions") or a reference into ANOTHER
		// document, and neither is resolvable here. See the file header for what that costs.
		if len(numbered) == 0 {
			continue
		}
		checked++
		for _, m := range mdDecisionRef.FindAllStringSubmatch(text, -1) {
			n, _ := strconv.Atoi(m[1])
			if !numbered[n] {
				keys := make([]int, 0, len(numbered))
				for k := range numbered {
					keys = append(keys, k)
				}
				sort.Ints(keys)
				problems = append(problems, rel+` references "Decision `+strconv.Itoa(n)+
					`" but numbers only `+joinInts(keys))
			}
		}
	}
	if checked == 0 {
		t.Fatal("no document with numbered decision headings was found — the heading pattern no " +
			"longer matches this repository's ADRs and this rule is inert")
	}
	sort.Strings(problems)
	if len(problems) > 0 {
		t.Errorf("%d dangling intra-document decision cross-reference(s) (#1569 A6):\n    %s",
			len(problems), strings.Join(problems, "\n    "))
	}
}

func joinInts(ns []int) string {
	parts := make([]string, 0, len(ns))
	for _, n := range ns {
		parts = append(parts, strconv.Itoa(n))
	}
	return strings.Join(parts, ", ")
}

// RULE 3.
func TestMarkdownCitedPathsResolve(t *testing.T) {
	files := scopedMarkdownFiles(t, mdRepoRoot)
	seen := map[string]bool{}
	var added []string
	total := 0
	for _, rel := range files {
		b, err := os.ReadFile(filepath.Join(mdRepoRoot, rel))
		if err != nil {
			t.Fatalf("reading %s: %v", rel, err)
		}
		for _, m := range mdCitedPath.FindAllStringSubmatch(string(b), -1) {
			cited := m[1]
			// A bare basename is unresolvable shorthand rather than a claim about a location; the Go
			// guard makes the same exemption for the same reason.
			if !strings.Contains(cited, "/") {
				continue
			}
			total++
			if mdPathResolves(mdRepoRoot, cited) {
				continue
			}
			key := cited + " @ " + rel
			seen[key] = true
			if !unresolvedMDPathCitations[key] {
				added = append(added, key)
			}
		}
	}
	if total < 200 {
		t.Fatalf("only %d path citations found across scoped Markdown; the corpus has hundreds. "+
			"The extraction is broken", total)
	}
	sort.Strings(added)
	if len(added) > 0 {
		t.Errorf("%d Markdown citation(s) point at a path that does not exist (#1569 A6):\n    %s",
			len(added), strings.Join(added, "\n    "))
	}
	var fixed []string
	for key := range unresolvedMDPathCitations {
		if !seen[key] {
			fixed = append(fixed, key)
		}
	}
	sort.Strings(fixed)
	if len(fixed) > 0 {
		t.Errorf("%d baselined Markdown path citation(s) now resolve — remove them from "+
			"unresolvedMDPathCitations:\n    %s", len(fixed), strings.Join(fixed, "\n    "))
	}
}

// TestMarkdownBaselines_AreBoundedAndNonEmpty guards the guards, exactly as task 19's does: an
// emptied baseline is how a failure gets "fixed", and a growing one is how a ratchet stops
// ratcheting.
func TestMarkdownBaselines_AreBoundedAndNonEmpty(t *testing.T) {
	if n := len(brokenMDTestCitations); n == 0 || n > 26 {
		t.Fatalf("brokenMDTestCitations holds %d entries; it was 26 when this guard was written, and "+
			"the ratchet only turns one way (0 means it was deleted rather than earned)", n)
	}
	if n := len(unresolvedMDPathCitations); n == 0 || n > 64 {
		t.Fatalf("unresolvedMDPathCitations holds %d entries; it was 64 when this guard was written", n)
	}
}
