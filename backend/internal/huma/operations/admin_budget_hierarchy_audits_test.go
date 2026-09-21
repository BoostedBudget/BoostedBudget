package operations

// Issue #1630 review fix (LOW): with outcome omitted, ListAdminBudgetHierarchyAudits used to run
// TWO independently limit-bound repository queries (one per actionable outcome) and append the
// results — so limit=50 could return up to 100 rows, grouped by outcome rather than globally
// ordered by examined_at. These tests prove the handler now issues exactly ONE
// ListBudgetHierarchyAuditFindings call carrying the WHOLE outcome filter and the caller's limit,
// for every input.Outcome case, and returns exactly what that one call returns — no
// merging/truncating logic left in the handler to get wrong.

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
)

// recordingBudgetHierarchyAuditReader is a BudgetHierarchyAuditReader stub that records every call
// it receives (arguments, and how many times it was called) and returns a fixed set of verdicts —
// so a test can assert on the handler's QUERY SHAPE (one call, what filter, what limit) rather than
// just its output.
type recordingBudgetHierarchyAuditReader struct {
	calls   [][2]any // [outcomes, limit] per call
	returns []domain.BudgetHierarchyAuditVerdict
	err     error
}

func (r *recordingBudgetHierarchyAuditReader) ListBudgetHierarchyAuditFindings(ctx context.Context, outcomes []string, limit int) ([]domain.BudgetHierarchyAuditVerdict, error) {
	r.calls = append(r.calls, [2]any{outcomes, limit})
	if r.err != nil {
		return nil, r.err
	}
	return r.returns, nil
}

func newBudgetHierarchyAuditVerdict(id string, examinedAt time.Time) domain.BudgetHierarchyAuditVerdict {
	return domain.BudgetHierarchyAuditVerdict{
		BudgetID:   id,
		UserID:     "user-" + id,
		Outcome:    domain.BudgetHierarchyAuditOutcomeReportedDuplicateIdentity,
		Reason:     "test",
		ExaminedAt: examinedAt,
	}
}

func TestListAdminBudgetHierarchyAudits_1630Review_DefaultOutcomeIsOneQueryWithBothOutcomesAndTheGivenLimit(t *testing.T) {
	reader := &recordingBudgetHierarchyAuditReader{
		returns: []domain.BudgetHierarchyAuditVerdict{
			newBudgetHierarchyAuditVerdict("b1", time.Now()),
		},
	}
	handler := &AdminHandler{budgetHierarchyAuditReader: reader}

	input := &ListAdminBudgetHierarchyAuditsInput{Limit: 50} // Outcome omitted → default

	out, err := handler.ListAdminBudgetHierarchyAudits(context.Background(), input)
	require.NoError(t, err)

	require.Len(t, reader.calls, 1,
		"the handler must issue exactly ONE repository call for the default (omitted-outcome) case — "+
			"the pre-fix handler issued one call PER actionable outcome instead")
	gotOutcomes, _ := reader.calls[0][0].([]string)
	assert.ElementsMatch(t, budgetHierarchyAuditActionableOutcomes, gotOutcomes,
		"the single call must carry BOTH actionable outcomes as one filter")
	assert.Equal(t, 50, reader.calls[0][1], "the caller's own limit must be passed through untouched")

	assert.Len(t, out.Body.Findings, 1, "the handler must return exactly what the one repository call returned")
}

func TestListAdminBudgetHierarchyAudits_1630Review_SingleOutcomeIsAlsoOneQuery(t *testing.T) {
	reader := &recordingBudgetHierarchyAuditReader{}
	handler := &AdminHandler{budgetHierarchyAuditReader: reader}

	input := &ListAdminBudgetHierarchyAuditsInput{Outcome: domain.BudgetHierarchyAuditOutcomeReportedEscapingPair, Limit: 25}
	_, err := handler.ListAdminBudgetHierarchyAudits(context.Background(), input)
	require.NoError(t, err)

	require.Len(t, reader.calls, 1)
	gotOutcomes, _ := reader.calls[0][0].([]string)
	assert.Equal(t, []string{domain.BudgetHierarchyAuditOutcomeReportedEscapingPair}, gotOutcomes)
	assert.Equal(t, 25, reader.calls[0][1])
}

func TestListAdminBudgetHierarchyAudits_1630Review_AllOutcomeIsOneQueryWithNoFilter(t *testing.T) {
	reader := &recordingBudgetHierarchyAuditReader{}
	handler := &AdminHandler{budgetHierarchyAuditReader: reader}

	input := &ListAdminBudgetHierarchyAuditsInput{Outcome: "all", Limit: 100}
	_, err := handler.ListAdminBudgetHierarchyAudits(context.Background(), input)
	require.NoError(t, err)

	require.Len(t, reader.calls, 1)
	gotOutcomes, _ := reader.calls[0][0].([]string)
	assert.Empty(t, gotOutcomes, "outcome=all must pass an empty/nil filter, not a list of every outcome name")
	assert.Equal(t, 100, reader.calls[0][1])
}

// TestListAdminBudgetHierarchyAudits_1685_SkippedOutcomeIsAcceptedButNotActionable pins both halves
// of the todo 12 admin contract: the new outcome is a recognized verdict a caller can filter on
// (not a 422), and it is NOT part of the default actionable subset — a skipped budget is
// informational and has no operator action.
func TestListAdminBudgetHierarchyAudits_1685_SkippedOutcomeIsAcceptedButNotActionable(t *testing.T) {
	reader := &recordingBudgetHierarchyAuditReader{}
	handler := &AdminHandler{budgetHierarchyAuditReader: reader}

	out, err := handler.ListAdminBudgetHierarchyAudits(context.Background(), &ListAdminBudgetHierarchyAuditsInput{
		Outcome: domain.BudgetHierarchyAuditOutcomeSkippedIncompleteActuals,
		Limit:   25,
	})
	require.NoError(t, err, "the new outcome must be a recognized verdict, not a 422")
	require.Len(t, reader.calls, 1)
	gotOutcomes, _ := reader.calls[0][0].([]string)
	assert.Equal(t, []string{domain.BudgetHierarchyAuditOutcomeSkippedIncompleteActuals}, gotOutcomes)
	assert.Equal(t, []string{domain.BudgetHierarchyAuditOutcomeSkippedIncompleteActuals}, out.Body.OutcomeFilter)

	assert.NotContains(t, budgetHierarchyAuditActionableOutcomes, domain.BudgetHierarchyAuditOutcomeSkippedIncompleteActuals,
		"the skipped outcome is informational; it must not join the actionable default")
}
