//go:build integration

package repository

// Issue #1569 follow-up (#1586) — the settlement move must also work when the posted twin arrives
// in a LATER sync than the removal.
//
// WHAT WAS OBSERVED BEFORE THIS FILE EXISTED, not assumed from the ticket. A probe run against
// freshcheck at v171 settled the pending row with no twin stored, then delivered the twin:
//
//	pending after sync N: deleted_at set, deleted_reason=provider_removed_pending, category_source=manual
//	posted twin pointer present: true
//	posted twin: notes=<nil> kind=<nil> pfcOverride=<nil> source=plaid reviewed=false tags=[]
//
// So all three of the ticket's premises hold: the pointer IS present on the twin, the pending row
// IS already tombstoned when the twin lands, and TombstoneByExternalID's `moved` CTE is the only
// code that ever moves anything — it ran in sync N against a twin that did not exist yet and
// matched nothing. The annotations are retained on a row that renders nowhere, which is the exact
// "retention that reads as deletion" shape #1569 exists to remove.
//
// WHY INTEGRATION AND NOT pgxmock. Every claim here is semantics a mock cannot evaluate: whether
// the join finds an ALREADY-TOMBSTONED row, whether the refusal predicates fire against a twin a
// rule already categorised, and whether the annotation-history row is actually written by the CTE.

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
)

// lateSettle performs sync N (the removal, with no twin stored) and then sync N+1 (the twin), and
// returns the twin's id. It deliberately uses the same production write paths the sync service
// uses: TombstoneByExternalID for the removal and CreateBatch for the added twin.
func lateSettle(t *testing.T, ctx context.Context, f *settlementFixture, twin domain.Transaction) {
	t.Helper()
	f.settle(t, ctx) // sync N: removal arrives first, twin does not exist
	twin.PendingTransactionID = &f.pendingExtID
	f.addPostedTwin(t, ctx, twin) // sync N+1
}

func (f *settlementFixture) pullForward(t *testing.T, ctx context.Context, ids ...string) int64 {
	t.Helper()
	if len(ids) == 0 {
		ids = []string{f.postedID}
	}
	n, err := f.repo.PullForwardSettledPendingAnnotations(ctx, f.userID, ids)
	require.NoError(t, err)
	return n
}

func (f *settlementFixture) settlementHistoryFor(t *testing.T, ctx context.Context, txnID string) []string {
	t.Helper()
	rows, err := f.tx.Query(ctx, `
		SELECT change_source FROM transaction_annotation_history
		WHERE transaction_id = $1 ORDER BY changed_at`, txnID)
	require.NoError(t, err)
	defer rows.Close()
	var out []string
	for rows.Next() {
		var s string
		require.NoError(t, rows.Scan(&s))
		out = append(out, s)
	}
	require.NoError(t, rows.Err())
	return out
}

// TestLateSettlement_AnnotationsPullForwardWhenTwinArrivesLater_Integration is the headline claim of
// #1586: the acceptance criterion "a removal delivered in sync N whose posted twin arrives in sync
// N+1 ends with the annotations on the posted row".
func TestLateSettlement_AnnotationsPullForwardWhenTwinArrivesLater_Integration(t *testing.T) {
	ctx := context.Background()
	f := seedSettlementFixture(t)

	lateSettle(t, ctx, f, domain.Transaction{})

	moved := f.pullForward(t, ctx)
	assert.Equal(t, int64(1), moved, "exactly the one stranded twin is pulled forward")

	posted, err := f.repo.GetByID(ctx, f.postedID)
	require.NoError(t, err)

	require.NotNil(t, posted.UserNotes)
	assert.Equal(t, "split with Sam, they owe me half", *posted.UserNotes)
	require.NotNil(t, posted.CategoryOverrideKind)
	assert.Equal(t, "pfc", *posted.CategoryOverrideKind)
	require.NotNil(t, posted.PFCPrimaryOverride)
	assert.Equal(t, "FOOD_AND_DRINK", *posted.PFCPrimaryOverride)
	assert.Equal(t, domain.CategorySourceManual, posted.CategorySourceField)
	require.NotNil(t, posted.EffectiveCategoryName)
	assert.Equal(t, "FOOD_AND_DRINK", *posted.EffectiveCategoryName)
	assert.ElementsMatch(t, []string{"holiday", "shared"}, posted.Tags)
	require.Len(t, posted.TagsSource, 2)
	assert.True(t, posted.Reviewed)
	require.NotNil(t, posted.ReviewedAt)
	require.NotNil(t, posted.CategoryOverrideDate,
		"the date the user's own edit recorded must travel too; a fresh NOW() would claim they filed it today")

	// The pending row is untouched by the pull-forward: the move COPIES, exactly as the
	// tombstone-side move does, so the retained row stays available for forensics.
	pending := f.readAnyRow(t, ctx, f.pendingID)
	require.NotNil(t, pending.DeletedAt)
	require.NotNil(t, pending.DeletedReason)
	assert.Equal(t, domain.TxnDeletedProviderRemovedPending, *pending.DeletedReason)
	assert.Equal(t, domain.CategorySourceManual, pending.CategorySourceField)
}

// TestLateSettlement_PullForwardIsRecordedAsASettlement_Integration: the pull-forward overwrites the
// twin's own category columns, so it must record what it overwrote under the same change_source the
// tombstone-side move uses. It is the same event arriving by a different route.
func TestLateSettlement_PullForwardIsRecordedAsASettlement_Integration(t *testing.T) {
	ctx := context.Background()
	f := seedSettlementFixture(t)

	// The twin arrives already carrying a RULE category — the common case, since rules run in place
	// before CreateBatch. That is precisely the state settlementCategoryMoveCond admits and
	// therefore the state whose loss must be recorded.
	lateSettle(t, ctx, f, domain.Transaction{
		CategoryOverrideKind: strPtr("pfc"),
		PFCPrimaryOverride:   strPtr("ENTERTAINMENT"),
		CategorySourceField:  domain.CategorySourceRule,
		Reviewed:             true,
	})

	require.Equal(t, int64(1), f.pullForward(t, ctx))

	assert.Equal(t, []string{domain.TransactionAnnotationChangeSettlement},
		f.settlementHistoryFor(t, ctx, f.postedID),
		"the rule category the pull-forward replaced must leave a trail")

	var priorPFC, newPFC *string
	require.NoError(t, f.tx.QueryRow(ctx, `
		SELECT prior_pfc_primary_override, new_pfc_primary_override
		FROM transaction_annotation_history WHERE transaction_id = $1`, f.postedID).
		Scan(&priorPFC, &newPFC))
	require.NotNil(t, priorPFC)
	assert.Equal(t, "ENTERTAINMENT", *priorPFC, "what the twin had before")
	require.NotNil(t, newPFC)
	assert.Equal(t, "FOOD_AND_DRINK", *newPFC, "what the settlement put there")
}

// TestLateSettlement_ManualWorkOnTheTwinIsNeverOverwritten_Integration reuses the refusal semantics
// verbatim: newer manual work about the same charge outranks the older pending annotation.
func TestLateSettlement_ManualWorkOnTheTwinIsNeverOverwritten_Integration(t *testing.T) {
	ctx := context.Background()
	f := seedSettlementFixture(t)

	f.settle(t, ctx)
	f.addPostedTwin(t, ctx, domain.Transaction{PendingTransactionID: &f.pendingExtID})
	// The user recategorises the posted row themselves, through the production write path.
	require.NoError(t, f.repo.UpdateCategory(ctx, f.postedID, f.userID,
		strPtr("pfc"), strPtr("TRANSPORTATION"), nil, nil, domain.CategorySourceManual))

	f.pullForward(t, ctx)

	posted, err := f.repo.GetByID(ctx, f.postedID)
	require.NoError(t, err)
	require.NotNil(t, posted.PFCPrimaryOverride)
	assert.Equal(t, "TRANSPORTATION", *posted.PFCPrimaryOverride,
		"a manual category on the twin is newer user work and wins")
}

// TestLateSettlement_LivePendingRowIsNotPulledForward_Integration scopes the hook. If the pending
// row is still live, the twin is visible alongside it and the removal has not arrived yet; the
// tombstone-side move is what handles that ordering. Firing here would write the user's annotation
// onto a second VISIBLE row.
func TestLateSettlement_LivePendingRowIsNotPulledForward_Integration(t *testing.T) {
	ctx := context.Background()
	f := seedSettlementFixture(t)

	f.addPostedTwin(t, ctx, domain.Transaction{PendingTransactionID: &f.pendingExtID})

	assert.Equal(t, int64(0), f.pullForward(t, ctx),
		"nothing is stranded while the pending row is still live")

	posted, err := f.repo.GetByID(ctx, f.postedID)
	require.NoError(t, err)
	assert.Nil(t, posted.PFCPrimaryOverride)
	assert.Nil(t, posted.UserNotes)
}

// TestLateSettlement_PostedReversalIsNotADonor_Integration: `s.pending` is load-bearing. A row Plaid
// removed while POSTED is a reversal, not a settlement, and donating its annotations to a row that
// happens to carry a stale pointer would be the corruption the pointer design exists to avoid.
func TestLateSettlement_PostedReversalIsNotADonor_Integration(t *testing.T) {
	ctx := context.Background()
	f := seedSettlementFixture(t)

	// Re-deliver the pending row as POSTED before removing it, so the removal classifies as a
	// reversal rather than a settlement. Through CreateBatch's ON CONFLICT path, as Plaid would.
	f.addPostedTwin(t, ctx, domain.Transaction{
		ID: uuid.New().String(), ExternalTransactionID: &f.pendingExtID, Pending: false,
	})
	reason, err := f.repo.TombstoneByExternalID(ctx, domain.SourcePlaid, f.pendingExtID, f.userID,
		domain.TxnDeletedByPlaidSync)
	require.NoError(t, err)
	require.Equal(t, domain.TxnDeletedProviderRemovedPosted, reason)

	f.addPostedTwin(t, ctx, domain.Transaction{PendingTransactionID: &f.pendingExtID})

	assert.Equal(t, int64(0), f.pullForward(t, ctx),
		"a posted reversal is not a settlement and must never donate its annotations")
}

// TestLateSettlement_TenancyIsNotCrossed_Integration. The pointer is a provider identifier and
// carries no ownership; a second user is never a donor and never a recipient.
func TestLateSettlement_TenancyIsNotCrossed_Integration(t *testing.T) {
	ctx := context.Background()
	f := seedSettlementFixture(t)

	otherUser := uuid.New().String()
	otherItem := uuid.New().String()
	otherAcct := uuid.New().String()
	f.seedUser(t, ctx, otherUser, otherItem, otherAcct)

	f.settle(t, ctx)

	// The OTHER user's posted row carries a pointer with the same provider id.
	otherPosted := uuid.New().String()
	otherExt := "plaid-other-posted-" + otherPosted
	_, err := f.repo.CreateBatch(ctx, []domain.Transaction{{
		ID: otherPosted, UserID: otherUser, AccountID: otherAcct, LinkedItemID: otherItem,
		Source: domain.SourcePlaid, ExternalTransactionID: &otherExt,
		Amount: 42.00, Date: time.Now().AddDate(0, 0, -1), Name: "Posted Coffee",
		PFCPrimary: strPtr("GENERAL_SERVICES"), PendingTransactionID: &f.pendingExtID,
	}})
	require.NoError(t, err)

	n, err := f.repo.PullForwardSettledPendingAnnotations(ctx, otherUser, []string{otherPosted})
	require.NoError(t, err)
	assert.Equal(t, int64(0), n, "another user's tombstoned pending is not a donor")

	other, err := f.repo.GetByID(ctx, otherPosted)
	require.NoError(t, err)
	assert.Nil(t, other.UserNotes)
	assert.Nil(t, other.PFCPrimaryOverride)
}

// TestLateSettlement_TombstonedTwinIsNotARecipient_Integration: writing user work onto a second row
// that renders nowhere would move the annotation from one invisible row to another.
func TestLateSettlement_TombstonedTwinIsNotARecipient_Integration(t *testing.T) {
	ctx := context.Background()
	f := seedSettlementFixture(t)

	lateSettle(t, ctx, f, domain.Transaction{})
	_, err := f.repo.TombstoneByExternalID(ctx, domain.SourcePlaid, f.postedExtID, f.userID,
		domain.TxnDeletedByPlaidSync)
	require.NoError(t, err)

	assert.Equal(t, int64(0), f.pullForward(t, ctx),
		"a tombstoned twin renders nowhere; donating to it moves nothing into view")
}

// TestLateSettlement_IsIdempotent_Integration. The sync path can re-run over the same batch after a
// failure, and a second pass must not manufacture a second history row describing a no-op.
func TestLateSettlement_IsIdempotent_Integration(t *testing.T) {
	ctx := context.Background()
	f := seedSettlementFixture(t)

	lateSettle(t, ctx, f, domain.Transaction{})
	require.Equal(t, int64(1), f.pullForward(t, ctx))
	before := f.settlementHistoryFor(t, ctx, f.postedID)

	f.pullForward(t, ctx)
	assert.Equal(t, before, f.settlementHistoryFor(t, ctx, f.postedID),
		"a re-run changes nothing, so it records nothing")
}
