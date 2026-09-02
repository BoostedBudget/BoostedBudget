package operations

// savings_goal_deleted_contributions_test.go — Issue #1569 T2.
//
// GET /api/v1/savings-goals/deleted/{goalID}/contributions is the read that makes the DELETE
// endpoint's promise ("every contribution still logged against the goal is retained") observable —
// still logged, because deleting one contribution explicitly is a hard delete and no goal
// tombstone brings it back. It is a
// SEPARATE, explicitly opted-into route rather than a widening of
// GET /savings-goals/{goalID}/contributions: the client asks for a deleted goal's ledger only from
// the deleted-goals surface, and no live path can accidentally serve a tombstoned goal.

import (
	"context"
	"errors"
	"testing"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// TestListDeletedGoalContributions_ReturnsItemisedLedgerAndTotal verifies the handler routes to the
// tombstone-exempt reads (NOT ListByGoal/SumConfirmedByGoal) and returns both the entries and the
// confirmed total.
func TestListDeletedGoalContributions_ReturnsItemisedLedgerAndTotal(t *testing.T) {
	goalID := "goal-deleted-1"
	userID := "user-deleted-1"

	contribRepo := new(mockSavingsContributionRepo)
	handler := NewContributionHandler(contribRepo, nil, nil)

	contribs := []*domain.SavingsGoalContribution{makeTestContribution(goalID)}
	contribRepo.On("ListByDeletedGoal", mock.Anything, userID, goalID).Return(contribs, nil)
	contribRepo.On("SumConfirmedByDeletedGoal", mock.Anything, userID, goalID).Return(425.5, nil)

	output, err := handler.ListDeletedGoalContributions(context.Background(),
		&ListDeletedGoalContributionsInput{
			AuthenticatedInput: authenticatedInput(userID),
			GoalID:             goalID,
		})

	require.NoError(t, err)
	require.Len(t, output.Body.Contributions, 1)
	assert.Equal(t, "contrib-test-1", output.Body.Contributions[0].ID)
	assert.InDelta(t, 425.5, output.Body.TotalConfirmed, 0.001)

	// The live reads must not be touched by this route at all.
	contribRepo.AssertNotCalled(t, "ListByGoal", mock.Anything, userID, goalID)
	contribRepo.AssertNotCalled(t, "SumConfirmedByGoal", mock.Anything, userID, goalID)
	contribRepo.AssertExpectations(t)
}

// TestListDeletedGoalContributions_EmptyIsAnEmptyArray guards the JSON shape: a nil slice would
// serialise as `null`, which the client cannot distinguish from a failure.
func TestListDeletedGoalContributions_EmptyIsAnEmptyArray(t *testing.T) {
	contribRepo := new(mockSavingsContributionRepo)
	handler := NewContributionHandler(contribRepo, nil, nil)

	contribRepo.On("ListByDeletedGoal", mock.Anything, "u", "g").
		Return([]*domain.SavingsGoalContribution(nil), nil)
	contribRepo.On("SumConfirmedByDeletedGoal", mock.Anything, "u", "g").Return(0.0, nil)

	output, err := handler.ListDeletedGoalContributions(context.Background(),
		&ListDeletedGoalContributionsInput{
			AuthenticatedInput: authenticatedInput("u"),
			GoalID:             "g",
		})

	require.NoError(t, err)
	require.NotNil(t, output.Body.Contributions)
	assert.Empty(t, output.Body.Contributions)
}

// TestListDeletedGoalContributions_ListErrorPropagates verifies the read error is mapped rather
// than being flattened into an empty ledger — the exact ambiguity this task exists to remove.
func TestListDeletedGoalContributions_ListErrorPropagates(t *testing.T) {
	contribRepo := new(mockSavingsContributionRepo)
	handler := NewContributionHandler(contribRepo, nil, nil)

	contribRepo.On("ListByDeletedGoal", mock.Anything, "u", "g").
		Return([]*domain.SavingsGoalContribution(nil), errors.New("boom"))

	output, err := handler.ListDeletedGoalContributions(context.Background(),
		&ListDeletedGoalContributionsInput{
			AuthenticatedInput: authenticatedInput("u"),
			GoalID:             "g",
		})

	require.Error(t, err)
	assert.Nil(t, output)
	contribRepo.AssertNotCalled(t, "SumConfirmedByDeletedGoal", mock.Anything, "u", "g")
}

// TestListDeletedGoalContributions_SumErrorPropagates — same, for the total.
func TestListDeletedGoalContributions_SumErrorPropagates(t *testing.T) {
	contribRepo := new(mockSavingsContributionRepo)
	handler := NewContributionHandler(contribRepo, nil, nil)

	contribRepo.On("ListByDeletedGoal", mock.Anything, "u", "g").
		Return([]*domain.SavingsGoalContribution{makeTestContribution("g")}, nil)
	contribRepo.On("SumConfirmedByDeletedGoal", mock.Anything, "u", "g").
		Return(0.0, errors.New("boom"))

	output, err := handler.ListDeletedGoalContributions(context.Background(),
		&ListDeletedGoalContributionsInput{
			AuthenticatedInput: authenticatedInput("u"),
			GoalID:             "g",
		})

	require.Error(t, err)
	assert.Nil(t, output)
}
