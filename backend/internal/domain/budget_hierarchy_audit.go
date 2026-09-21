package domain

import "time"

// Issue #1630 Task B7: the vocabulary of the budget-hierarchy audit/repair.
//
// domain.DuplicateParentIdentity and domain.EscapedNestedChild (Task 4 / the HIGH-severity review
// fix documented on ErrEscapedNestedChild) are write-time guards only. Every budget that already
// held one of those shapes before its guard existed — including the production budget issue #1630
// names, bf6676b1-7bbd-4f06-b765-62756ce0094e — keeps it forever unless something goes looking. This
// audit is that something, mirroring #1569 Task 11's portfolio_snapshots repair
// (domain/portfolio_snapshot_repair.go): every budget it looks at gets a verdict, and every verdict
// is recorded — including the budgets it declines to rewrite.
const (
	// BudgetHierarchyAuditOutcomeRepaired — no duplicate-identity or escaping-pair shape was found
	// on this budget, and at least one LIVE category's stored spent_amount disagreed with the narrow
	// recomputation (services.NarrowBudgetPeriodDates + CalculateCategorySpent, scoped to the
	// budget's own stored period — the same primitive Task B1's resync job uses, and deliberately
	// NOT BudgetSyncService.RecalculateBudget: see that method's doc comment for the income
	// recalculation, time.Now()-derived periods, budget_updated automation and swallowed
	// per-category failures a repair built on it would inherit). The disagreeing rows were
	// corrected.
	BudgetHierarchyAuditOutcomeRepaired = "repaired"
	// BudgetHierarchyAuditOutcomeVerified — no shape found, and every live category's stored
	// spent_amount already matched the recomputation. Nothing was written, not even updated_at.
	BudgetHierarchyAuditOutcomeVerified = "verified"
	// BudgetHierarchyAuditOutcomeReportedDuplicateIdentity — this budget has a LIVE pair of
	// categories that collide on parent identity (domain.DuplicateParentIdentity). There is no
	// automatic way to decide which row is authoritative — that would require guessing which one the
	// user actually meant — so the identity fields are NEVER touched. spent_amount corrections (if
	// any were found) still apply: they make a derived cache agree with the user's own config, which
	// is safe regardless of the identity ambiguity.
	BudgetHierarchyAuditOutcomeReportedDuplicateIdentity = "reported_duplicate_identity"
	// BudgetHierarchyAuditOutcomeReportedEscapingPair — this budget has a LIVE hierarchically-nested
	// pair whose match_logic escapes clean containment (domain.EscapedNestedChild). match_logic is
	// NEVER touched: rewriting it would silently change what the user's budget matches while
	// destroying the only evidence available to ask what they meant — worse than the visible
	// wrongness it would replace. spent_amount corrections (if any) still apply, for the same reason
	// as above.
	BudgetHierarchyAuditOutcomeReportedEscapingPair = "reported_escaping_pair"
	// BudgetHierarchyAuditOutcomeSkippedIncompleteActuals — this budget's spent_amount could not be
	// recomputed at all: CalculateCategorySpent returned domain.ErrBudgetActualsIncomplete, so the
	// ledger is ambiguous or degraded and no trustworthy figure exists to compare against. The audit
	// records the skip and commits; it applies NO partial repair, discarding every category it had
	// already recomputed before the sentinel fired. Informational, not actionable: there is nothing
	// for an operator to do that the user's own next config write will not do.
	//
	// ⚠ ACCEPTED LIMITATION — PERMANENT EXEMPTION. The ledger row makes this budget EXAMINED, and
	// BudgetIDsWithUnexaminedHierarchy excludes any budget already in the ledger, so a skipped budget
	// is never re-selected even after the ambiguity clears. The admin surface is GET-only, so there
	// is no operator re-audit path either. This is deliberate (a re-audit loop would need its own
	// starvation/backoff design) and is recorded in the #1685 decision doc; do not describe it as
	// mitigated.
	BudgetHierarchyAuditOutcomeSkippedIncompleteActuals = "skipped_incomplete_actuals"
)

// BudgetHierarchyAuditVerdict is one examined budget — one row of budget_hierarchy_audits.
type BudgetHierarchyAuditVerdict struct {
	BudgetID string `json:"budget_id"`
	UserID   string `json:"user_id"`
	Outcome  string `json:"outcome"`
	Reason   string `json:"reason"`

	CategoriesExamined int `json:"categories_examined"`
	CategoriesRepaired int `json:"categories_repaired"`

	// FlaggedDuplicateIdentityCategoryIDs / FlaggedEscapingPairCategoryIDs are the
	// budget_categories.id values that tripped, respectively, domain.DuplicateParentIdentity and
	// domain.EscapedNestedChild against this budget's live category set. A budget can trip BOTH
	// checks at once (Issue #1630 review fix, MED) — decideBudgetHierarchyAuditOutcome picks only
	// ONE preferred `outcome` for the ledger row, but these two fields stay independently
	// populated regardless of which outcome won, so an operator can always tell a duplicate-
	// identity id from an escaping-pair id instead of finding both flattened into one typeless
	// union. Each is non-empty only when that specific shape was found.
	FlaggedDuplicateIdentityCategoryIDs []string `json:"flagged_duplicate_identity_category_ids,omitempty"`
	FlaggedEscapingPairCategoryIDs      []string `json:"flagged_escaping_pair_category_ids,omitempty"`

	ExaminedAt time.Time `json:"examined_at"`
}

// BudgetHierarchyAuditSummary counts one audit run's verdicts, across however many budgets it
// examined.
type BudgetHierarchyAuditSummary struct {
	Examined                  int `json:"examined"`
	Repaired                  int `json:"repaired"`
	Verified                  int `json:"verified"`
	ReportedDuplicateIdentity int `json:"reported_duplicate_identity"`
	ReportedEscapingPair      int `json:"reported_escaping_pair"`
	SkippedIncompleteActuals  int `json:"skipped_incomplete_actuals"`
}

// Add folds one verdict into the summary.
func (s *BudgetHierarchyAuditSummary) Add(outcome string, n int) {
	s.Examined += n
	switch outcome {
	case BudgetHierarchyAuditOutcomeRepaired:
		s.Repaired += n
	case BudgetHierarchyAuditOutcomeVerified:
		s.Verified += n
	case BudgetHierarchyAuditOutcomeReportedDuplicateIdentity:
		s.ReportedDuplicateIdentity += n
	case BudgetHierarchyAuditOutcomeReportedEscapingPair:
		s.ReportedEscapingPair += n
	case BudgetHierarchyAuditOutcomeSkippedIncompleteActuals:
		s.SkippedIncompleteActuals += n
	}
}

// Merge folds another run's counts in, for a caller auditing many budgets in one pass.
func (s *BudgetHierarchyAuditSummary) Merge(o BudgetHierarchyAuditSummary) {
	s.Examined += o.Examined
	s.Repaired += o.Repaired
	s.Verified += o.Verified
	s.ReportedDuplicateIdentity += o.ReportedDuplicateIdentity
	s.ReportedEscapingPair += o.ReportedEscapingPair
	s.SkippedIncompleteActuals += o.SkippedIncompleteActuals
}
