package domain

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestBudgetHierarchyAuditSummaryCountsSkippedIncompleteActuals pins the #1685 todo 12 counter: a
// skipped budget is still EXAMINED (so a run's total stays honest) and is counted under its own
// field by both Add and Merge. Before this, both switches had no case for the outcome, so a skipped
// budget silently vanished from every counter except Examined.
func TestBudgetHierarchyAuditSummaryCountsSkippedIncompleteActuals(t *testing.T) {
	var summary BudgetHierarchyAuditSummary
	summary.Add(BudgetHierarchyAuditOutcomeSkippedIncompleteActuals, 2)
	assert.Equal(t, 2, summary.Examined)
	assert.Equal(t, 2, summary.SkippedIncompleteActuals)

	var other BudgetHierarchyAuditSummary
	other.Add(BudgetHierarchyAuditOutcomeSkippedIncompleteActuals, 3)
	summary.Merge(other)
	assert.Equal(t, 5, summary.Examined)
	assert.Equal(t, 5, summary.SkippedIncompleteActuals)
}