package services

import (
	"bytes"
	"context"
	"log/slog"
	"testing"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/observability"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// quarantineCandidateCount reads the shadow-mode counter for one reason out of the DEFAULT
// gatherer. The CounterVec itself is unexported in observability and stays that way — exporting
// production surface so a test can read it would be the wrong trade.
func quarantineCandidateCount(t *testing.T, reason observability.QuarantineReason) float64 {
	t.Helper()
	families, err := prometheus.DefaultGatherer.Gather()
	require.NoError(t, err)
	for _, f := range families {
		if f.GetName() != "categorization_rule_quarantine_candidates_total" {
			continue
		}
		for _, m := range f.GetMetric() {
			for _, l := range m.GetLabel() {
				if l.GetName() == "reason" && l.GetValue() == string(reason) {
					return m.GetCounter().GetValue()
				}
			}
		}
	}
	return -1
}

// #1625 Task 9: three places degrade to a silent no-op while every test stays green.
//
// They share one shape — a RUNTIME hook protecting a COMPILE-TIME property, exercised only by
// tests that construct the concrete type directly. A test that calls the hook itself proves the
// hook works; it proves nothing about whether anything calls it.

// SetTransactionReader is injected in main.go behind a runtime type assertion, so if
// NewCategorizationRuleService ever returns a value type or a decorator, `ok` goes false, preview
// degrades to "not available" IN PRODUCTION, and every unit test still passes —
// rule_preview_service_test.go even blesses the degraded path. A compile-time assertion is the
// only thing that can fail for the right reason here.
var _ interface {
	SetTransactionReader(PreviewTransactionReader)
} = (*categorizationRuleService)(nil)

// The shadow-mode inventory is what the whole enforcement decision rests on, and NO test reached
// reportQuarantineCandidates through its callers: both existing tests call it directly, so
// deleting either call site left CI green and the inventory silently never ran.
func TestApplyRulesToTransaction_ReachesTheQuarantineInventory(t *testing.T) {
	before := quarantineCandidateCount(t, observability.QuarantineInvalidValue)

	broken := &domain.CategorizationRule{
		ID: "r-broken", UserID: "u1", Enabled: true, ConditionLogic: "AND",
		Conditions: []domain.RuleCondition{
			{Type: domain.ConditionAmountGreaterThan, Value: "not-a-number"},
		},
		TagBehavior: "add", Source: "manual",
	}

	repo := &stubRuleApplicationRepo{rules: []*domain.CategorizationRule{broken}}
	svc := NewRuleApplicationService(repo)
	svc.SetLogger(slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil)))

	tx := &domain.Transaction{ID: "t1", UserID: "u1", Name: "x", Amount: 10}
	require.NoError(t, svc.ApplyRulesToTransaction(context.Background(), tx))

	after := quarantineCandidateCount(t, observability.QuarantineInvalidValue)
	assert.Greater(t, after, before,
		"the quarantine inventory was not reached through ApplyRulesToTransaction — deleting the "+
			"call site would leave CI green while the inventory silently never ran")
}
