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
	before := quarantineCandidateCount(t, observability.QuarantineUncoercibleValue)

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

	after := quarantineCandidateCount(t, observability.QuarantineUncoercibleValue)
	assert.Greater(t, after, before,
		"the quarantine inventory was not reached through ApplyRulesToTransaction — deleting the "+
			"call site would leave CI green while the inventory silently never ran")
}

// stubMerchantResolver records what it was asked for.
type stubMerchantResolver struct {
	names map[string]string
	calls [][]string
	err   error
}

func (s *stubMerchantResolver) ResolveEffectiveMerchantNames(_ context.Context, _ string, ids []string) (map[string]string, error) {
	s.calls = append(s.calls, ids)
	return s.names, s.err
}

// #1625 Task 11: enrichment must actually reach the EVALUATOR through ApplyRulesToBatch.
//
// The field, the evaluator case and the resolver can all be correct while nothing connects them —
// which is what "all-or-nothing" meant: enriching only the repo read paths would make a rule match
// in preview and not during sync, the same preview/reality divergence this ticket already fixed
// once.
//
// ⚠ SCOPE, stated honestly because the commit message overstated it. This starts at
// ApplyRulesToBatch with MerchantID already populated. It proves resolver -> enrichment ->
// evaluator, and nothing more. It does NOT exercise processAddedTransactions, upsertMerchants, or
// the ordering between them — that ordering is argued from the code and from the fact that the
// merchant upsert reads MerchantName and writes MerchantID while rules touch neither, not
// demonstrated here.
func TestApplyRulesToBatch_MatchesOnTheMerchantNameTheUserSees(t *testing.T) {
	merchantID := "m-1"
	raw := "SQ *BLUE BOTTLE 1234"

	rule := &domain.CategorizationRule{
		ID: "r-1", UserID: "u1", Enabled: true, ConditionLogic: "AND",
		Conditions: []domain.RuleCondition{{
			Field:    domain.FieldEffectiveMerchant,
			Operator: string(domain.RuleOpContains),
			Value:    "blue bottle coffee",
		}},
		TargetKind: strPtrSvc("pfc"), TargetPFCPrimary: strPtrSvc("FOOD_AND_DRINK"),
		TagBehavior: "add", Source: "manual",
	}

	resolver := &stubMerchantResolver{names: map[string]string{merchantID: "Blue Bottle Coffee"}}
	svc := NewRuleApplicationService(&stubRuleApplicationRepo{rules: []*domain.CategorizationRule{rule}})
	svc.SetLogger(slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil)))
	svc.SetEffectiveMerchantResolver(resolver)

	txns := []domain.Transaction{
		{ID: "t1", UserID: "u1", Name: "card purchase", MerchantName: &raw, MerchantID: &merchantID},
	}

	applied, err := svc.ApplyRulesToBatch(context.Background(), txns, "u1")
	require.NoError(t, err)
	require.Len(t, applied, 1)
	assert.True(t, applied[0].CategoryApplied,
		"the rule matched in preview but not in sync — enrichment never reached the evaluator")
	assert.Equal(t, [][]string{{merchantID}}, resolver.calls,
		"merchant ids must be resolved once for the whole batch, deduplicated")
}

// Fail-open: a resolver error must leave the sync categorising on the raw name, not abort it.
func TestApplyRulesToBatch_ResolverFailureFallsBackToTheRawName(t *testing.T) {
	merchantID := "m-1"
	raw := "STARBUCKS STORE 42"

	rule := &domain.CategorizationRule{
		ID: "r-1", UserID: "u1", Enabled: true, ConditionLogic: "AND",
		Conditions: []domain.RuleCondition{{
			Field:    domain.FieldEffectiveMerchant,
			Operator: string(domain.RuleOpContains),
			Value:    "starbucks",
		}},
		TargetKind: strPtrSvc("pfc"), TargetPFCPrimary: strPtrSvc("FOOD_AND_DRINK"),
		TagBehavior: "add", Source: "manual",
	}

	resolver := &stubMerchantResolver{err: context.DeadlineExceeded}
	svc := NewRuleApplicationService(&stubRuleApplicationRepo{rules: []*domain.CategorizationRule{rule}})
	svc.SetLogger(slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil)))
	svc.SetEffectiveMerchantResolver(resolver)

	applied, err := svc.ApplyRulesToBatch(context.Background(),
		[]domain.Transaction{{ID: "t1", UserID: "u1", Name: "x", MerchantName: &raw, MerchantID: &merchantID}}, "u1")

	require.NoError(t, err, "a merchant lookup must never fail a sync")
	require.Len(t, applied, 1)
	assert.True(t, applied[0].CategoryApplied,
		"with resolution unavailable the rule must still match on the raw provider name")
}
