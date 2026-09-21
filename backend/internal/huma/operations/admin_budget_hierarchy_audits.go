package operations

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	humaerrors "github.com/maxnussbaum/BoostedBudget/internal/huma/errors"
	"github.com/maxnussbaum/BoostedBudget/internal/huma/inputs"
	humamiddleware "github.com/maxnussbaum/BoostedBudget/internal/huma/middleware"
)

// BudgetHierarchyAuditReader is the narrow read this surface needs — Issue #1630 Task B7.
// Read-only, deliberately: see AdminHandler.budgetHierarchyAuditReader's doc comment for why there
// is no corresponding write action.
type BudgetHierarchyAuditReader interface {
	ListBudgetHierarchyAuditFindings(ctx context.Context, outcomes []string, limit int) ([]domain.BudgetHierarchyAuditVerdict, error)
}

// budgetHierarchyAuditActionableOutcomes is what this endpoint returns when the caller does not
// name an outcome explicitly — the two verdicts a human is actually expected to act on.
// 'repaired'/'verified' are still reachable via ?outcome=repaired etc.; they are not the reason
// this surface exists (a spent_amount correction needs no operator, only the two reported_*
// verdicts do — see budget_hierarchy_audit.go's header comment for why each is never
// auto-repaired).
var budgetHierarchyAuditActionableOutcomes = []string{
	domain.BudgetHierarchyAuditOutcomeReportedDuplicateIdentity,
	domain.BudgetHierarchyAuditOutcomeReportedEscapingPair,
}

// ListAdminBudgetHierarchyAuditsInput is the query for the budget-hierarchy-audit admin listing.
type ListAdminBudgetHierarchyAuditsInput struct {
	inputs.AdminAuthenticatedInput
	// Outcome filters to exactly one verdict (repaired, verified, reported_duplicate_identity,
	// reported_escaping_pair, skipped_incomplete_actuals). Omit to get both reported_* outcomes —
	// the actionable subset — or pass "all" to get every verdict this audit has ever recorded,
	// including the boring ones.
	Outcome string `query:"outcome" doc:"Filter to one outcome (repaired, verified, reported_duplicate_identity, reported_escaping_pair, skipped_incomplete_actuals), or 'all' for every outcome. Omitted defaults to the two reported_* (actionable) outcomes."`
	Limit   int    `query:"limit" default:"50" minimum:"1" maximum:"500" doc:"Number of findings to return"`
}

// AdminBudgetHierarchyAuditFinding is one examined budget as this surface reports it.
type AdminBudgetHierarchyAuditFinding struct {
	BudgetID           string `json:"budget_id"`
	UserID             string `json:"user_id"`
	Outcome            string `json:"outcome"`
	Reason             string `json:"reason"`
	CategoriesExamined int    `json:"categories_examined"`
	CategoriesRepaired int    `json:"categories_repaired"`
	// FlaggedDuplicateIdentityCategoryIDs / FlaggedEscapingPairCategoryIDs are kept separate
	// (Issue #1630 review fix, MED) so a budget carrying BOTH shapes at once — a duplicate-parent-
	// identity pair AND an escaping nested pair — doesn't flatten into one array an operator can no
	// longer attribute to either check. Each is empty unless that specific shape was found; both
	// can be non-empty on the same finding regardless of which one `outcome` names.
	FlaggedDuplicateIdentityCategoryIDs []string `json:"flagged_duplicate_identity_category_ids"`
	FlaggedEscapingPairCategoryIDs      []string `json:"flagged_escaping_pair_category_ids"`
	ExaminedAt                          string   `json:"examined_at"`
}

// ListAdminBudgetHierarchyAuditsOutput is the response body.
type ListAdminBudgetHierarchyAuditsOutput struct {
	Body struct {
		Findings []AdminBudgetHierarchyAuditFinding `json:"findings"`
		Limit    int                                `json:"limit"`
		// OutcomeFilter echoes back what was actually queried (after defaulting), so a caller who
		// omitted outcome can tell they got the actionable-subset default rather than everything.
		OutcomeFilter []string `json:"outcome_filter"`
	}
}

// ListAdminBudgetHierarchyAudits enumerates the budget-hierarchy audit's findings — Issue #1630
// Task B7.
//
// GET /api/v1/admin/budget-hierarchy-audits
//
// THE STATE IT MAKES ADDRESSABLE. domain.DuplicateParentIdentity and domain.EscapedNestedChild are
// write-time guards: they refuse a NEW budget category that would collide on parent identity or
// escape a nested parent's containment envelope, but neither is ever run against a row that
// predates it — including the production budget issue #1630 names,
// bf6676b1-7bbd-4f06-b765-62756ce0094e. BudgetHierarchyAuditWorker (the periodic job) examines
// every budget exactly once and records a verdict in budget_hierarchy_audits; this endpoint is the
// queryable, durable path to that ledger, matching the WARN log line the worker also emits per
// flagged budget.
//
// ⚠ THIS ENDPOINT REPAIRS NOTHING AND FIXES NEITHER GUARD'S OWN LIMITATION. A
// reported_duplicate_identity or reported_escaping_pair row names a budget where the identity
// fields (which row "owns" a colliding parent) or match_logic (what the nested pair actually
// matches) cannot be corrected automatically — there is no way to infer which the user meant, and
// guessing would silently change what their budget matches while destroying the only evidence
// available to ask them. A spent_amount correction the SAME audit run made (categories_repaired)
// is safe and already applied by the time this list is read; what remains open is exactly the
// identity/match_logic ambiguity this listing surfaces for a human to resolve with the user.
func (h *AdminHandler) ListAdminBudgetHierarchyAudits(ctx context.Context, input *ListAdminBudgetHierarchyAuditsInput) (*ListAdminBudgetHierarchyAuditsOutput, error) {
	if h.budgetHierarchyAuditReader == nil {
		return nil, huma.Error503ServiceUnavailable("budget hierarchy audit reader not available")
	}

	var outcomeFilter []string
	switch input.Outcome {
	case "":
		outcomeFilter = budgetHierarchyAuditActionableOutcomes
	case "all":
		outcomeFilter = nil
	case domain.BudgetHierarchyAuditOutcomeRepaired, domain.BudgetHierarchyAuditOutcomeVerified,
		domain.BudgetHierarchyAuditOutcomeReportedDuplicateIdentity, domain.BudgetHierarchyAuditOutcomeReportedEscapingPair,
		domain.BudgetHierarchyAuditOutcomeSkippedIncompleteActuals:
		outcomeFilter = []string{input.Outcome}
	default:
		return nil, huma.Error422UnprocessableEntity(fmt.Sprintf(
			"outcome %q is not a recognized verdict; use repaired, verified, reported_duplicate_identity, "+
				"reported_escaping_pair, skipped_incomplete_actuals, or 'all'", input.Outcome))
	}

	out := &ListAdminBudgetHierarchyAuditsOutput{}
	out.Body.Limit = input.Limit
	out.Body.OutcomeFilter = outcomeFilter
	out.Body.Findings = []AdminBudgetHierarchyAuditFinding{}

	// Issue #1630 review fix (LOW): this used to run ONE query per outcome in outcomeFilter and
	// append the results, so the two-outcome default (input.Outcome == "") could return up to 2x
	// input.Limit rows, grouped by outcome rather than globally ordered by examined_at.
	// ListBudgetHierarchyAuditFindings now takes the whole filter as one slice, so this is a
	// single query with one global ORDER BY and one honest LIMIT, for every case (empty filter,
	// one outcome, or several).
	verdicts, err := h.budgetHierarchyAuditReader.ListBudgetHierarchyAuditFindings(ctx, outcomeFilter, input.Limit)
	if err != nil {
		return nil, humaerrors.LoggingMapError(ctx, fmt.Errorf("failed to list budget hierarchy audit findings: %w", err))
	}
	out.Body.Findings = renderBudgetHierarchyAuditFindings(verdicts)
	return out, nil
}

// renderBudgetHierarchyAuditFindings converts the domain verdicts this repository read returns
// into the wire shape, formatting ExaminedAt as RFC3339 for a stable, timezone-explicit contract.
func renderBudgetHierarchyAuditFindings(verdicts []domain.BudgetHierarchyAuditVerdict) []AdminBudgetHierarchyAuditFinding {
	out := make([]AdminBudgetHierarchyAuditFinding, 0, len(verdicts))
	for _, v := range verdicts {
		flaggedDuplicate := v.FlaggedDuplicateIdentityCategoryIDs
		if flaggedDuplicate == nil {
			flaggedDuplicate = []string{}
		}
		flaggedEscaping := v.FlaggedEscapingPairCategoryIDs
		if flaggedEscaping == nil {
			flaggedEscaping = []string{}
		}
		out = append(out, AdminBudgetHierarchyAuditFinding{
			BudgetID:                            v.BudgetID,
			UserID:                              v.UserID,
			Outcome:                             v.Outcome,
			Reason:                              v.Reason,
			CategoriesExamined:                  v.CategoriesExamined,
			CategoriesRepaired:                  v.CategoriesRepaired,
			FlaggedDuplicateIdentityCategoryIDs: flaggedDuplicate,
			FlaggedEscapingPairCategoryIDs:      flaggedEscaping,
			ExaminedAt:                          v.ExaminedAt.UTC().Format(time.RFC3339),
		})
	}
	return out
}

// RegisterAdminBudgetHierarchyAuditOperations registers the budget-hierarchy-audit admin listing.
//
// Named *Operations rather than *Routes for the reason RegisterAdminPlaidOrphanedItemOperations
// gives: cmd/server's route-registration drift test matches `Register…Routes(`, and a second
// `…Routes` name registered from inside RegisterAdminRoutes would look to it like a group main.go
// had failed to call.
func RegisterAdminBudgetHierarchyAuditOperations(api huma.API, handler *AdminHandler) {
	huma.Register(api, huma.Operation{
		OperationID: "listAdminBudgetHierarchyAudits",
		Method:      http.MethodGet,
		Path:        "/api/v1/admin/budget-hierarchy-audits",
		Summary:     "List budget-hierarchy audit findings (admin)",
		Description: "Returns budgets the Issue #1630 Task B7 audit has examined. " +
			"domain.DuplicateParentIdentity and domain.EscapedNestedChild are write-time guards that " +
			"refuse a new budget category colliding on parent identity or escaping a nested parent's " +
			"containment envelope; neither is ever run against a row that predates it. The periodic " +
			"BudgetHierarchyAuditWorker examines every budget once, corrects any stale spent_amount " +
			"(safe: a derived cache), and NEVER rewrites match_logic or identity fields for a flagged " +
			"pair — there is no correct automatic answer for either, and guessing would silently " +
			"change what the user's budget matches while destroying the evidence needed to ask them. " +
			"Omitting outcome defaults to the two reported_* (actionable) outcomes; pass outcome=all " +
			"for every verdict including repaired/verified. This is a read-only view: it repairs " +
			"nothing and does not resolve the identity/match_logic ambiguity a reported_* row names — " +
			"that requires asking the user what they meant. Requires admin role.",
		Tags:     []string{"Admin"},
		Security: []map[string][]string{{"clerk": {}}},
		Middlewares: huma.Middlewares{
			humamiddleware.NewAdminMiddleware(handler.config, handler.adminService),
		},
	}, handler.ListAdminBudgetHierarchyAudits)
}
