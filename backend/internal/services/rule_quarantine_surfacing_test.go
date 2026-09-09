package services

import (
	"context"
	"testing"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func newRuleServiceForQuarantineTest(t *testing.T, rules ...*domain.CategorizationRule) CategorizationRuleService {
	t.Helper()
	repo := new(mockRuleRepository)
	repo.On("GetByUserID", mock.Anything, "u1").Return(rules, nil)
	return NewCategorizationRuleService(repo, new(MockCustomCategoryRepository))
}

// #1625 Task 7: quarantine must be SURFACED before it is enforced.
//
// Shadow mode counted and logged, so the only party who could learn that a rule was not going to
// run was an operator reading /metrics. The user whose rule silently stopped matching could not.
// Enforcing invisibly is the precise failure mode this ticket exists to close, so surfacing is a
// prerequisite for Task 8 rather than a follow-up to it.
//
// The status is computed at READ time and never persisted: it is a function of the rule's own
// conditions, so a stored copy could only go stale. It is classified by the SAME quarantineReason
// used for the metric -- two classifiers would eventually disagree, and the disagreement would be
// invisible.
func TestGetUserRules_SurfacesQuarantineStatus(t *testing.T) {
	broken := &domain.CategorizationRule{
		ID: "r-broken", UserID: "u1", Enabled: true, ConditionLogic: "AND",
		Conditions: []domain.RuleCondition{
			{Type: domain.ConditionAmountGreaterThan, Value: "not-a-number"},
		},
	}
	healthy := &domain.CategorizationRule{
		ID: "r-ok", UserID: "u1", Enabled: true, ConditionLogic: "AND",
		Conditions: []domain.RuleCondition{
			{Type: domain.ConditionMerchantContains, Value: "amazon"},
		},
	}

	svc := newRuleServiceForQuarantineTest(t, broken, healthy)

	rules, err := svc.GetUserRules(context.Background(), "u1")
	require.NoError(t, err)
	require.Len(t, rules, 2)

	byID := map[string]*domain.CategorizationRule{}
	for _, r := range rules {
		byID[r.ID] = r
	}

	assert.True(t, byID["r-broken"].Quarantined,
		"a rule that cannot be evaluated must say so, or the user just sees it stop working")
	assert.NotEmpty(t, byID["r-broken"].QuarantineReason,
		"the reason must be reported, not merely the fact")

	assert.False(t, byID["r-ok"].Quarantined)
	assert.Empty(t, byID["r-ok"].QuarantineReason)
}

// A zero-condition rule matches EVERY transaction (engine.go RuleMatches returns true when the
// list is empty). It is the one case where leaving the rule alone is the risky choice, so it must
// be visible before Task 8 starts refusing to run it.
func TestGetUserRules_SurfacesTheMatchesEverythingRule(t *testing.T) {
	empty := &domain.CategorizationRule{
		ID: "r-empty", UserID: "u1", Enabled: true, ConditionLogic: "AND",
		Conditions: []domain.RuleCondition{},
	}

	svc := newRuleServiceForQuarantineTest(t, empty)

	rules, err := svc.GetUserRules(context.Background(), "u1")
	require.NoError(t, err)
	require.Len(t, rules, 1)

	assert.True(t, rules[0].Quarantined)
	assert.Equal(t, "no_conditions_matches_everything", rules[0].QuarantineReason,
		"the reason must be the SAME vocabulary the metric uses, not a second wording")
}

// The single-rule read is a separate handler and would otherwise report a broken rule as healthy
// while the list reported it as quarantined -- the two views of one rule disagreeing.
func TestGetRuleByID_SurfacesQuarantineStatusToo(t *testing.T) {
	broken := &domain.CategorizationRule{
		ID: "r-broken", UserID: "u1", Enabled: true, ConditionLogic: "SOMETHING_ELSE",
		Conditions: []domain.RuleCondition{
			{Type: domain.ConditionMerchantContains, Value: "amazon"},
		},
	}
	repo := new(mockRuleRepository)
	repo.On("GetByID", mock.Anything, "r-broken").Return(broken, nil)
	svc := NewCategorizationRuleService(repo, new(MockCustomCategoryRepository))

	got, err := svc.GetRuleByID(context.Background(), "u1", "r-broken")
	require.NoError(t, err)
	assert.True(t, got.Quarantined)
	assert.Equal(t, "invalid_condition_logic_defaults_to_and", got.QuarantineReason)
}
