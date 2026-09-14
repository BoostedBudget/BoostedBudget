package operations

import (
	"context"
	"testing"

	"github.com/maxnussbaum/BoostedBudget/internal/repository"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakePostHogErasureOperator struct{ evidence, retarget bool }

func (f *fakePostHogErasureOperator) RecordOperatorEvidence(context.Context, string, string, string, string) (string, error) {
	f.evidence = true
	return "manual_pending", nil
}
func (f *fakePostHogErasureOperator) RetargetUnconfigured(context.Context, string, string, string, string) (bool, error) {
	f.retarget = true
	return true, nil
}
func (f *fakePostHogErasureOperator) ListOutstanding(context.Context, int) ([]repository.PostHogErasureOperatorRow, error) {
	return []repository.PostHogErasureOperatorRow{{ID: "row"}}, nil
}
func (f *fakePostHogErasureOperator) RevealOperatorIdentity(context.Context, string, string) (repository.PostHogErasureOperatorIdentity, error) {
	return repository.PostHogErasureOperatorIdentity{DistinctIDs: []string{"secret"}}, nil
}
func (f *fakePostHogErasureOperator) RequeueDeadLetter(context.Context, string, string) (bool, error) {
	return true, nil
}

func TestAdminPostHogErasureOperatorActionsAreExecutable(t *testing.T) {
	fake := &fakePostHogErasureOperator{}
	h := (&AdminHandler{}).WithPostHogErasureOperator(fake)
	_, err := h.RecordPostHogErasureEvidence(context.Background(), &PostHogErasureEvidenceInput{ErasureID: "11111111-1111-4111-8111-111111111111", Body: PostHogErasureEvidenceBody{Surface: "support", Evidence: "provider_zero"}})
	require.NoError(t, err)
	_, err = h.RetargetPostHogErasure(context.Background(), &PostHogErasureRetargetInput{ErasureID: "11111111-1111-4111-8111-111111111111", Body: PostHogErasureRetargetBody{ProjectID: "42", Region: "us"}})
	require.NoError(t, err)
	assert.True(t, fake.evidence)
	assert.True(t, fake.retarget)
	listed, err := h.ListPostHogErasures(context.Background(), &ListPostHogErasuresInput{Limit: 50})
	require.NoError(t, err)
	assert.Equal(t, "no-store", listed.CacheControl)
	revealed, err := h.RevealPostHogErasure(context.Background(), &RevealPostHogErasureInput{ErasureID: "11111111-1111-4111-8111-111111111111"})
	require.NoError(t, err)
	assert.Equal(t, "no-store", revealed.CacheControl)
	_, err = h.ReplayPostHogErasure(context.Background(), &RevealPostHogErasureInput{ErasureID: "11111111-1111-4111-8111-111111111111"})
	require.NoError(t, err)
}
