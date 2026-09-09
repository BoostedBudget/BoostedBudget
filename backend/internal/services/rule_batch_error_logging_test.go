package services

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Codex gap analysis A7, then #1625 A8.
//
// A7: ApplyRulesToBatch's comment claimed "log and continue" while the code only continued — the
// evaluator error was discarded with no record anywhere.
//
// A8 changed what there is to say. EvaluateRules used to return on the FIRST rule's error, so one
// bad condition stopped every other rule for that transaction; the header here claimed "other
// rules still evaluated" while the one-rule fixture could not test it and the code did not do it.
// Errors are rule-local now, so the log is per-RULE rather than per-transaction, and the fixture
// below carries a second, working rule that must still apply. That is the property the old header
// promised.
//
// Task 2 changed it again: this rule's value is uncoercible, so it is now refused by enforcement
// rather than failing in the evaluator, and the record is the quarantine line. The property under
// test — one broken rule must not suppress the rules after it, and must not vanish silently —
// is unchanged. The evaluator-error path itself is still exercised, in
// internal/rules TestRuleEngine_EvaluateRules_OneBrokenRuleDoesNotSuppressTheRest with the
// enforcement kill switch off, which is the only way it can still be reached.
func TestApplyRulesToBatch_SkipsOnlyTheBrokenRuleAndLogsIt(t *testing.T) {
	var logBuf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logBuf, &slog.HandlerOptions{Level: slog.LevelWarn}))

	// A condition whose VALUE the evaluator cannot use. It is written the way a pre-#1625 row
	// could be: the fused type is valid, so it loads, but the value shape makes the amount
	// evaluator return an error.
	brokenRule := &domain.CategorizationRule{
		ID:             "rule-broken",
		UserID:         "u1",
		Enabled:        true,
		ConditionLogic: "AND",
		Conditions:     []domain.RuleCondition{{Type: domain.ConditionAmountGreaterThan, Value: "not-a-number"}},
		TargetKind:     strPtrSvc("clear"),
		TagBehavior:    "add",
		Source:         "manual",
	}

	// The rule that must still run. Without a second rule the "others still applied" property is
	// untestable, which is exactly why the original version of this test could not see A8.
	workingRule := &domain.CategorizationRule{
		ID:             "rule-good",
		UserID:         "u1",
		Enabled:        true,
		ConditionLogic: "AND",
		// Matches both fixture transactions (amounts 10 and 20). NOT merchant_contains: these
		// fixtures have no merchant name, so such a rule would match nothing and the assertion
		// below would pass vacuously for the wrong reason.
		Conditions:     []domain.RuleCondition{{Type: domain.ConditionAmountGreaterThan, Value: 5}},
		TargetKind:       strPtrSvc("pfc"),
		TargetPFCPrimary: strPtrSvc("FOOD_AND_DRINK"),
		TagBehavior:      "add",
		Source:           "manual",
	}

	repo := &stubRuleApplicationRepo{rules: []*domain.CategorizationRule{brokenRule, workingRule}}
	svc := NewRuleApplicationService(repo)
	svc.SetLogger(logger)

	txns := []domain.Transaction{
		{ID: "t1", UserID: "u1", Name: "one", Amount: 10},
		{ID: "t2", UserID: "u1", Name: "two", Amount: 20},
	}

	updated, err := svc.ApplyRulesToBatch(context.Background(), txns, "u1")

	require.NoError(t, err, "a broken rule must not fail the whole batch")

	out := logBuf.String()

	// ⚠ #1625 Task 2 moved WHERE this is reported, and the move is deliberate. "not-a-number"
	// for an amount is an UNCOERCIBLE value, so RuleMatches refuses the rule before any
	// evaluator runs: there is no longer an evaluation error to log. The report is now the
	// quarantine line — which is strictly better, because it names the reason and says whether
	// the rule stopped, and it is emitted once per batch rather than once per transaction.
	assert.Contains(t, out, "categorization rule is quarantined",
		"a rule refused by enforcement must still be recorded, not silently skipped")
	assert.Equal(t, 1, strings.Count(out, "categorization rule is quarantined"),
		"quarantine is a property of the RULE, so it is reported once per batch, not once per transaction")
	assert.Contains(t, out, "reason=uncoercible_condition_value")
	assert.Contains(t, out, "stopped_matching=true",
		"the operator cannot tell an enforced reason from a repaired one without this")
	assert.NotContains(t, out, "rule evaluation failed",
		"enforcement returns (false, nil); reporting it ALSO as an evaluation failure would "+
			"double-count one rule under two mechanisms")
	assert.Contains(t, out, "rule-broken",
		"the log must name WHICH rule was refused — the pre-A8 transaction-level line could not")
	assert.NotContains(t, out, "rule-good",
		"the working rule is healthy and must not be reported as if it had a problem")

	// The property the old header claimed and the old fixture could not test.
	require.Len(t, updated, 2)
	for _, applied := range updated {
		assert.True(t, applied.CategoryApplied,
			"tx %s got no category: the broken rule suppressed the working one",
			applied.TransactionID)
		assert.Equal(t, "rule-good", applied.CategoryRuleID,
			"the surviving rule must be the one credited")
	}
}

// stubRuleApplicationRepo is the narrow RuleApplicationRepository the service needs.
type stubRuleApplicationRepo struct {
	rules []*domain.CategorizationRule
}

func (s *stubRuleApplicationRepo) GetEnabledByUserID(_ context.Context, _ string) ([]*domain.CategorizationRule, error) {
	return s.rules, nil
}

func (s *stubRuleApplicationRepo) IncrementMatchCount(_ context.Context, _ string) error { return nil }
