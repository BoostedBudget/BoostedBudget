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

// Codex gap analysis A7. ApplyRulesToBatch's comment claimed "log and continue" while the code
// only continued — the evaluator error was discarded with no record anywhere. Because
// EvaluateRules returns on the FIRST rule's error, one bad condition silently stopped every
// other rule from applying to that transaction, and nothing said so.
//
// This asserts the log line exists AND that processing carries on, which is the pair that makes
// the behaviour defensible rather than merely quiet.
func TestApplyRulesToBatch_LogsEvaluatorErrorAndKeepsGoing(t *testing.T) {
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

	repo := &stubRuleApplicationRepo{rules: []*domain.CategorizationRule{brokenRule}}
	svc := NewRuleApplicationService(repo)
	svc.SetLogger(logger)

	txns := []domain.Transaction{
		{ID: "t1", UserID: "u1", Name: "one", Amount: 10},
		{ID: "t2", UserID: "u1", Name: "two", Amount: 20},
	}

	_, err := svc.ApplyRulesToBatch(context.Background(), txns, "u1")

	require.NoError(t, err, "a broken rule must not fail the whole batch")
	out := logBuf.String()
	assert.Contains(t, out, "rule evaluation failed",
		"the swallowed evaluator error must actually be logged — the comment claimed it was, the code did not")
	assert.Equal(t, 2, strings.Count(out, "rule evaluation failed"),
		"processing must CONTINUE to the next transaction, logging each one")
}

// stubRuleApplicationRepo is the narrow RuleApplicationRepository the service needs.
type stubRuleApplicationRepo struct {
	rules []*domain.CategorizationRule
}

func (s *stubRuleApplicationRepo) GetEnabledByUserID(_ context.Context, _ string) ([]*domain.CategorizationRule, error) {
	return s.rules, nil
}

func (s *stubRuleApplicationRepo) IncrementMatchCount(_ context.Context, _ string) error { return nil }
