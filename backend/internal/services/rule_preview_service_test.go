package services

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type stubTxReader struct {
	txns      []domain.Transaction
	lastLimit int
	err       error
}

func (s *stubTxReader) GetByUserIDWithFilters(_ context.Context, _ string, _ domain.TransactionFilters, limit, _ int) ([]domain.Transaction, error) {
	s.lastLimit = limit
	return s.txns, s.err
}

func previewService(reader PreviewTransactionReader) *categorizationRuleService {
	svc := &categorizationRuleService{}
	svc.SetTransactionReader(reader)
	return svc
}

func candidateRule() *domain.CategorizationRule {
	return &domain.CategorizationRule{
		Name:       "Zelle to Jack",
		Conditions: []domain.RuleCondition{{Field: domain.FieldName, Operator: "contains", Value: "jack liu"}},
		TargetKind: func() *string { s := "clear"; return &s }(),
	}
}

func sampleTxns() []domain.Transaction {
	return []domain.Transaction{
		{ID: "t1", UserID: "u1", Name: "Zelle payment to Jack Liu", Amount: 1000, Date: time.Now()},
		{ID: "t2", UserID: "u1", Name: "Coffee", Amount: 4, Date: time.Now()},
	}
}

func TestPreviewRule_ReturnsMatchingTransactions(t *testing.T) {
	reader := &stubTxReader{txns: sampleTxns()}
	res, err := previewService(reader).PreviewRule(context.Background(), "u1", candidateRule(), 0)

	require.NoError(t, err)
	assert.Equal(t, 2, res.ScannedCount)
	assert.Equal(t, 1, res.MatchCount)
	require.Len(t, res.Matches, 1)
	assert.Equal(t, "t1", res.Matches[0].TransactionID)
}

// TestPreviewRule_ForcesEnabledOnCandidate — RuleMatches short-circuits to false for a disabled
// rule, and a candidate built from a create request has Enabled at its zero value. Without the
// override every preview would return zero matches: correct-looking, silent, and useless.
func TestPreviewRule_ForcesEnabledOnCandidate(t *testing.T) {
	reader := &stubTxReader{txns: sampleTxns()}
	rule := candidateRule()
	require.False(t, rule.Enabled, "a create-request candidate starts disabled — that is the trap")

	res, err := previewService(reader).PreviewRule(context.Background(), "u1", rule, 0)

	require.NoError(t, err)
	assert.Equal(t, 1, res.MatchCount, "a disabled candidate must still preview its matches")
}

// A preview is a READ. These three pin that it stays one: the service has no rule repository
// wired at all here, so any attempt to persist, bump a match count, or write execution history
// would nil-panic rather than pass.
func TestPreviewRule_HasNoWriteSideEffects(t *testing.T) {
	reader := &stubTxReader{txns: sampleTxns()}
	svc := previewService(reader)
	require.Nil(t, svc.ruleRepo, "preview must not need a rule repository")

	res, err := svc.PreviewRule(context.Background(), "u1", candidateRule(), 0)

	require.NoError(t, err)
	assert.Equal(t, 1, res.MatchCount)
}

// Preview validates exactly as create does, so the rule builder can use it as a validation
// oracle instead of the user discovering the error only on save.
func TestPreviewRule_InvalidCandidateReturnsTheSameValidationError(t *testing.T) {
	reader := &stubTxReader{txns: sampleTxns()}
	rule := candidateRule()
	rule.Conditions = []domain.RuleCondition{{Type: "amount", Value: 1000}} // the reported payload

	_, err := previewService(reader).PreviewRule(context.Background(), "u1", rule, 0)

	require.Error(t, err)
	assert.True(t, errors.Is(err, domain.ErrInvalidCondition),
		"preview must fail the way create fails, so it can be used to check a rule before saving")
}

func TestPreviewRule_RespectsScanWindowCap(t *testing.T) {
	reader := &stubTxReader{txns: sampleTxns()}
	svc := previewService(reader)

	_, err := svc.PreviewRule(context.Background(), "u1", candidateRule(), 0)
	require.NoError(t, err)
	assert.Equal(t, PreviewScanDefault, reader.lastLimit)

	_, err = svc.PreviewRule(context.Background(), "u1", candidateRule(), 10_000)
	require.NoError(t, err)
	assert.Equal(t, PreviewScanMax, reader.lastLimit, "an unbounded scan must be capped")
}

func TestPreviewRule_WithoutAReaderReportsUnavailable(t *testing.T) {
	_, err := (&categorizationRuleService{}).PreviewRule(context.Background(), "u1", candidateRule(), 0)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not available")
}
