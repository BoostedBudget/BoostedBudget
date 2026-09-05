//go:build integration

package repository

// #1569 gap G12 — UpdateTags overwrote a user annotation and recorded nothing, and the guard that
// exists to notice that could not see it.
//
// WHY TAGS ARE AN ANNOTATION HERE, when #1579's own text says "category/review overwrite". The
// codebase already answered it before this file existed: the settlement move
// (TombstoneByExternalID) carries `tags` and `tags_source` to the posted twin as part of the user's
// work — two of the twelve columns it moves — and PullForwardSettledPendingAnnotations carries them
// the other way for a late twin. A column that is worth following a transaction from one row to
// another because the user authored it is worth recording when it is overwritten. Nothing else in
// the system holds a previous tag set; UpdateTags is a whole-array replace, so the tags dropped from
// the array are gone the moment it returns.
//
// AND THE GUARD WAS SILENT, WHICH IS THE WORSE HALF. annotationTrackedColumns is derived from
// annotationCapturedColumns, which listed only the category and review columns, so
// TestAnnotationCaptureIsUsedByEveryAnnotationWritingStatement never looked for `SET tags =` at all.
// UpdateTags appeared in neither the captured set nor the exemption list: not excused, simply
// invisible. An absent reason is worse than a false one — six false written justifications shipped
// on this branch, and every one of them at least prompted the next reader to check.
//
// ⚠ UpdateTags' own comment ALREADY claimed a capture it did not have: "would make this statement
// disagree with the annotation capture that mirrors it". There was no capture to disagree with.
// That is the seventh.

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
)

// tagsHistoryRow is one recorded tag overwrite.
type tagsHistoryRow struct {
	userID       string
	changeSource string
	priorTags    []byte
	newTags      []byte
	priorSource  []byte
	newSource    []byte
}

func (f *settlementFixture) tagsHistoryFor(t *testing.T, ctx context.Context, txnID string) []tagsHistoryRow {
	t.Helper()
	rows, err := f.tx.Query(ctx, `
		SELECT user_id, change_source, prior_tags, new_tags, prior_tags_source, new_tags_source
		FROM transaction_annotation_history
		WHERE transaction_id = $1 AND change_source = 'tags'
		ORDER BY changed_at`, txnID)
	require.NoError(t, err)
	defer rows.Close()
	var out []tagsHistoryRow
	for rows.Next() {
		var r tagsHistoryRow
		require.NoError(t, rows.Scan(&r.userID, &r.changeSource, &r.priorTags, &r.newTags, &r.priorSource, &r.newSource))
		out = append(out, r)
	}
	require.NoError(t, rows.Err())
	return out
}

// countTagsHistory answers "was anything recorded at all" without naming a column that a
// pre-migration database does not have, so the RED this test was written for fails on its own
// assertion rather than on a missing column.
func (f *settlementFixture) countTagsHistory(t *testing.T, ctx context.Context, txnID string) int {
	t.Helper()
	var n int
	require.NoError(t, f.tx.QueryRow(ctx, `
		SELECT COUNT(*) FROM transaction_annotation_history
		WHERE transaction_id = $1 AND change_source = 'tags'`, txnID).Scan(&n))
	return n
}

// TestUpdateTags_RecordsTheTagsItOverwrites_Integration is the headline claim of G12.
func TestUpdateTags_RecordsTheTagsItOverwrites_Integration(t *testing.T) {
	ctx := context.Background()
	f := seedSettlementFixture(t)

	// The seed already tagged the pending row {holiday, shared} through the user's own endpoint.
	// This is the user replacing that set: "shared" survives, "holiday" is dropped, "work" is new.
	require.NoError(t, f.repo.UpdateTags(ctx, f.pendingID, f.userID,
		[]string{"shared", "work"},
		[]domain.TagSource{{Tag: "shared", Source: "manual"}, {Tag: "work", Source: "rule"}}))

	require.Equal(t, 2, f.countTagsHistory(t, ctx, f.pendingID),
		"both tag writes must be recorded: the seed's (nothing -> holiday+shared) and this one")

	recorded := f.tagsHistoryFor(t, ctx, f.pendingID)
	require.Len(t, recorded, 2)

	overwrite := recorded[1]
	assert.Equal(t, f.userID, overwrite.userID,
		"per-row attribution comes from the UPDATE's own RETURNING, never from client input")
	assert.JSONEq(t, `["holiday", "shared"]`, string(overwrite.priorTags),
		"the tag set the user just lost is the whole point of the record")
	assert.JSONEq(t, `["shared", "work"]`, string(overwrite.newTags))
	assert.JSONEq(t, `[{"tag":"holiday","source":"manual"},{"tag":"shared","source":"manual"}]`,
		string(overwrite.priorSource),
		"tags_source travels with tags: 'the rule put that there' is not recoverable from the array alone")
	assert.JSONEq(t, `[{"tag":"shared","source":"manual"},{"tag":"work","source":"rule"}]`,
		string(overwrite.newSource))
}

// TestUpdateTags_RecordsNothingWhenTheTagsDoNotChange_Integration: the capture's IS DISTINCT FROM
// filter must treat tags the way it treats every other column. An append-only trail that grows on
// no-ops buries the overwrites it exists to show.
func TestUpdateTags_RecordsNothingWhenTheTagsDoNotChange_Integration(t *testing.T) {
	ctx := context.Background()
	f := seedSettlementFixture(t)

	before := f.countTagsHistory(t, ctx, f.pendingID)
	require.NoError(t, f.repo.UpdateTags(ctx, f.pendingID, f.userID,
		[]string{"holiday", "shared"},
		[]domain.TagSource{{Tag: "holiday", Source: "manual"}, {Tag: "shared", Source: "manual"}}))

	assert.Equal(t, before, f.countTagsHistory(t, ctx, f.pendingID),
		"re-writing the same tags overwrote nothing, so there is nothing to record")
}

// TestUpdateTags_RecordsNothingForAnotherUsersTransaction_Integration: tenancy is inherited from the
// write's own predicate, so an id the caller does not own must produce neither an update nor a row.
func TestUpdateTags_RecordsNothingForAnotherUsersTransaction_Integration(t *testing.T) {
	ctx := context.Background()
	f := seedSettlementFixture(t)

	err := f.repo.UpdateTags(ctx, f.pendingID, uuid.New().String(),
		[]string{"stolen"}, []domain.TagSource{{Tag: "stolen", Source: "manual"}})
	require.ErrorIs(t, err, ErrTransactionNotFound,
		"a foreign id must still report not-found, exactly as before the capture")

	recorded := f.tagsHistoryFor(t, ctx, f.pendingID)
	for _, r := range recorded {
		assert.Equal(t, f.userID, r.userID, "no row may be recorded for a tenant the UPDATE did not match")
	}
	assert.Len(t, recorded, 1, "only the seed's own tag write is recorded")
}
