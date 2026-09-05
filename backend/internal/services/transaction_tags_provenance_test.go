package services

import (
	"context"
	"testing"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// Issue #1569 Task 20 (S11). UpdateTransactionTags rebuilt tags_source from scratch with
// Source: "manual" for every tag — the code literally said "Build tags source - all manual for
// now" — so any tag an automation rule had applied was relabelled the moment the user touched the
// tag list for any reason, including adding an unrelated tag.
//
// tags_source exists for exactly one purpose: to say WHICH tags the user chose and which a rule
// chose, and to name the rule (RuleID). ruleApplicationService.applyTagsToTransaction depends on
// it — its whole merge is "manual tags are always preserved, rule tags are re-derived" — so a
// wholesale relabel does not merely lose a label, it converts rule output into user intent that
// no later rule run may touch. That is the destruction: the provenance is not recomputable,
// because nothing records that the tag came from a rule once the row says "manual".
//
// ⚠ THE HONEST LIMIT, stated because the fix cannot remove it. The API takes a whole tag LIST, not
// a delta, so "the user kept the rule's tag" and "the user typed the same word" are the same
// request. The rule chosen is to PRESERVE the recorded source for a tag that was already present
// with one, and to mark only genuinely NEW tags manual. That is the direction that loses no
// information: a wrong "rule" can be corrected by removing the tag, a wrong "manual" cannot be
// corrected at all.

func TestUpdateTransactionTags_PreservesRuleProvenanceForTagsThatSurvive(t *testing.T) {
	ctx := context.Background()
	mockRepo := new(MockTransactionRepository)
	service := NewTransactionService(mockRepo, new(MockCustomCategoryRepository))

	ruleID := "rule-coffee-42"
	existing := &domain.Transaction{
		ID:     "txn-prov",
		UserID: "user-1",
		Tags:   []string{"coffee", "morning"},
		TagsSource: []domain.TagSource{
			{Tag: "coffee", Source: "rule", RuleID: &ruleID},
			{Tag: "morning", Source: "manual"},
		},
	}
	// The user adds one tag and keeps both existing ones.
	newTags := []string{"coffee", "morning", "treat"}

	var written []domain.TagSource
	mockRepo.On("GetByID", ctx, "txn-prov").Return(existing, nil)
	mockRepo.On("UpdateTags", ctx, "txn-prov", "user-1", newTags, mock.AnythingOfType("[]domain.TagSource")).
		Run(func(a mock.Arguments) { written = a.Get(4).([]domain.TagSource) }).
		Return(nil)

	result, err := service.UpdateTransactionTags(ctx, "user-1", "txn-prov", newTags)
	require.NoError(t, err)

	byTag := map[string]domain.TagSource{}
	for _, ts := range written {
		byTag[ts.Tag] = ts
	}
	require.Len(t, written, 3, "one entry per tag, in the order the caller gave them")

	assert.Equal(t, "rule", byTag["coffee"].Source,
		"a tag a rule applied and the user did not remove keeps its provenance")
	require.NotNil(t, byTag["coffee"].RuleID, "the rule id must survive too — a source with no rule "+
		"names nothing, and ruleApplicationService's merge reads both")
	assert.Equal(t, ruleID, *byTag["coffee"].RuleID)

	assert.Equal(t, "manual", byTag["morning"].Source, "an already-manual tag stays manual")
	assert.Nil(t, byTag["morning"].RuleID)

	assert.Equal(t, "manual", byTag["treat"].Source, "a genuinely new tag is the user's")
	assert.Nil(t, byTag["treat"].RuleID)

	// The returned object must agree with what was persisted, or the UI shows one provenance and
	// the database holds another.
	assert.Equal(t, written, result.TagsSource)
	mockRepo.AssertExpectations(t)
}

// Removing a tag must genuinely remove its provenance entry — otherwise "preserve the old source"
// degenerates into "never forget anything", and tags_source grows entries for tags that no longer
// exist. This is the discriminating case: an implementation that merged the old slice in wholesale
// would satisfy the test above.
func TestUpdateTransactionTags_DroppedTagLosesItsProvenanceEntry(t *testing.T) {
	ctx := context.Background()
	mockRepo := new(MockTransactionRepository)
	service := NewTransactionService(mockRepo, new(MockCustomCategoryRepository))

	ruleID := "rule-coffee-42"
	existing := &domain.Transaction{
		ID:     "txn-drop",
		UserID: "user-1",
		Tags:   []string{"coffee", "morning"},
		TagsSource: []domain.TagSource{
			{Tag: "coffee", Source: "rule", RuleID: &ruleID},
			{Tag: "morning", Source: "manual"},
		},
	}
	newTags := []string{"morning"}

	var written []domain.TagSource
	mockRepo.On("GetByID", ctx, "txn-drop").Return(existing, nil)
	mockRepo.On("UpdateTags", ctx, "txn-drop", "user-1", newTags, mock.AnythingOfType("[]domain.TagSource")).
		Run(func(a mock.Arguments) { written = a.Get(4).([]domain.TagSource) }).
		Return(nil)

	_, err := service.UpdateTransactionTags(ctx, "user-1", "txn-drop", newTags)
	require.NoError(t, err)

	require.Len(t, written, 1, "exactly the surviving tags")
	assert.Equal(t, "morning", written[0].Tag)
	mockRepo.AssertExpectations(t)
}

// A transaction that has never been touched by a rule must behave exactly as before: every tag
// manual. Without this, a fix that defaulted the wrong way would go unnoticed.
func TestUpdateTransactionTags_NoPriorProvenanceMeansAllManual(t *testing.T) {
	ctx := context.Background()
	mockRepo := new(MockTransactionRepository)
	service := NewTransactionService(mockRepo, new(MockCustomCategoryRepository))

	existing := &domain.Transaction{ID: "txn-fresh", UserID: "user-1"}
	newTags := []string{"a", "b"}

	var written []domain.TagSource
	mockRepo.On("GetByID", ctx, "txn-fresh").Return(existing, nil)
	mockRepo.On("UpdateTags", ctx, "txn-fresh", "user-1", newTags, mock.AnythingOfType("[]domain.TagSource")).
		Run(func(a mock.Arguments) { written = a.Get(4).([]domain.TagSource) }).
		Return(nil)

	_, err := service.UpdateTransactionTags(ctx, "user-1", "txn-fresh", newTags)
	require.NoError(t, err)

	require.Len(t, written, 2)
	for _, ts := range written {
		assert.Equal(t, "manual", ts.Source)
		assert.Nil(t, ts.RuleID)
	}
	mockRepo.AssertExpectations(t)
}
