package jobs

import (
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestBudgetHierarchyResyncIncludesGroupAndRemainderOnlyBudgets(t *testing.T) {
	for _, kind := range []string{"group", "remainder"} {
		require.True(t, budgetHierarchyNeedsResync([]domain.BudgetCategory{{CategoryKind: kind}}, nil, nil))
	}
	require.False(t, budgetHierarchyNeedsResync([]domain.BudgetCategory{{CategoryKind: "savings"}}, nil, nil))
}
