package services

import (
	"context"
	"strings"
	"testing"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/observability"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestRunRuleQuarantineCensus_CountsDistinctRulesNotObservations is the core proof for #1625
// Task 18: the SAME rule appearing in the repository's result must count once, and a census run
// must replace (not accumulate on top of) the gauge's prior value. Two runs over the identical
// input must therefore report the identical count — this is exactly what an ever-incrementing
// counter (the defect this task fixes) could never do.
func TestRunRuleQuarantineCensus_CountsDistinctRulesNotObservations(t *testing.T) {
	repo := new(mockRuleRepository)

	noConditionsRule := &domain.CategorizationRule{ID: "rule-no-conditions", ConditionLogic: "AND"}
	badLogicRule := &domain.CategorizationRule{
		ID:             "rule-bad-logic",
		ConditionLogic: "XOR", // neither AND nor OR
		Conditions:     []domain.RuleCondition{{Type: domain.ConditionMerchantContains, Value: "x"}},
	}
	healthyRule := &domain.CategorizationRule{
		ID:             "rule-healthy",
		ConditionLogic: "AND",
		Conditions:     []domain.RuleCondition{{Type: domain.ConditionMerchantContains, Value: "starbucks"}},
	}

	rules := []*domain.CategorizationRule{noConditionsRule, badLogicRule, healthyRule}
	repo.On("ListAllForQuarantineCensus", context.Background()).Return(rules, nil)

	require.NoError(t, RunRuleQuarantineCensus(context.Background(), repo))

	assert.Equal(t, float64(1), observability.RuleQuarantinePopulationForTest(observability.QuarantineNoConditions))
	assert.Equal(t, float64(1), observability.RuleQuarantinePopulationForTest(observability.QuarantineInvalidLogic))
	assert.Equal(t, float64(0), observability.RuleQuarantinePopulationForTest(observability.QuarantineUnnormalizable))
	assert.Equal(t, float64(0), observability.RuleQuarantinePopulationForTest(observability.QuarantineInvalidValue))
	assert.Equal(t, float64(0), observability.RuleQuarantinePopulationForTest(observability.QuarantineUncoercibleValue))

	// Running the census AGAIN over the identical input must report the SAME count, not double
	// it — proving Set semantics, not Add. This is the exact failure mode #1625 Task 18 exists to
	// fix: a counter would read 2 here.
	require.NoError(t, RunRuleQuarantineCensus(context.Background(), repo))
	assert.Equal(t, float64(1), observability.RuleQuarantinePopulationForTest(observability.QuarantineNoConditions))
	assert.Equal(t, float64(1), observability.RuleQuarantinePopulationForTest(observability.QuarantineInvalidLogic))

	repo.AssertExpectations(t)
}

// TestRunRuleQuarantineCensus_ARepairedRuleDropsOutOnTheNextRun proves the self-correcting
// property Set (not Add/Inc) provides: a rule that stops being quarantined between two census
// runs must see its reason's population go DOWN, never stay inflated forever.
func TestRunRuleQuarantineCensus_ARepairedRuleDropsOutOnTheNextRun(t *testing.T) {
	repo := new(mockRuleRepository)
	broken := &domain.CategorizationRule{ID: "rule-1", ConditionLogic: "AND"} // no conditions

	repo.On("ListAllForQuarantineCensus", context.Background()).Return(
		[]*domain.CategorizationRule{broken}, nil).Once()
	require.NoError(t, RunRuleQuarantineCensus(context.Background(), repo))
	assert.Equal(t, float64(1), observability.RuleQuarantinePopulationForTest(observability.QuarantineNoConditions))

	repaired := &domain.CategorizationRule{
		ID:             "rule-1",
		ConditionLogic: "AND",
		Conditions:     []domain.RuleCondition{{Type: domain.ConditionMerchantContains, Value: "fixed"}},
	}
	repo.On("ListAllForQuarantineCensus", context.Background()).Return(
		[]*domain.CategorizationRule{repaired}, nil).Once()
	require.NoError(t, RunRuleQuarantineCensus(context.Background(), repo))
	assert.Equal(t, float64(0), observability.RuleQuarantinePopulationForTest(observability.QuarantineNoConditions),
		"a repaired rule must drop OUT of the population on the next census, not stay counted forever")

	repo.AssertExpectations(t)
}

// TestRunRuleQuarantineCensus_PropagatesRepositoryError ensures a repository failure is surfaced
// rather than silently reporting an empty (and therefore falsely reassuring) census.
func TestRunRuleQuarantineCensus_PropagatesRepositoryError(t *testing.T) {
	repo := new(mockRuleRepository)
	repo.On("ListAllForQuarantineCensus", context.Background()).
		Return(nil, assertAnError("boom"))

	err := RunRuleQuarantineCensus(context.Background(), repo)
	require.Error(t, err)
	assert.True(t, strings.Contains(err.Error(), "boom"))

	repo.AssertExpectations(t)
}

type assertAnError string

func (e assertAnError) Error() string { return string(e) }
