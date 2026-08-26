package services

import (
	"context"
	"errors"
	"testing"

	"github.com/plaid/plaid-go/v23/plaid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
)

// The stranded-annotation repair and the cursor (#1586, on #1569's branch).
//
// THE DEFECT THESE PIN. pullForwardStrandedSettlements used to log its repository failure and
// return nothing, and its own doc comment said the repair "will be retried on next sync". It could
// not be. The caller advanced Plaid's cursor immediately afterwards, and /transactions/sync does
// not re-deliver a transaction the stored cursor has moved past — so a posted twin that stored fine
// while its annotation repair failed transiently left the user's category, notes, tags and review
// state on a tombstoned pending row FOREVER. The written justification was the reason the swallow
// looked acceptable, and it was false.
//
// THE CONTRACT NOW. The repair is part of storing the settlement, not a cosmetic afterthought: its
// failure returns, so the sync fails and the cursor stays where the run started, and the next run
// re-fetches the same page and repairs it. That is the same contract the surrounding code already
// applies to the writes either side of it — transactionRepo.CreateBatch and transactionRepo.Update
// failures return from these very functions, and they are the same class of transient database
// error. PullForwardSettledPendingAnnotations is idempotent, so the retry costs nothing.
//
// ⚠ THE COST IS DELIBERATE AND IS NOT ZERO. A repair that fails permanently (rather than
// transiently) now stalls the item's ingestion instead of quietly losing one user's annotations.
// That is the direction to fail in: a stalled cursor is a visible sync failure with metrics and a
// dead-letter job behind it, while the swallow was invisible to everyone including the user whose
// data it stranded. The alternative — a durable repair job — needs a River worker the sync service
// has no client for, and is worth revisiting if this ever fires on a non-transient fault.

// newSettlementPage builds a one-page sync whose single added transaction is a POSTED TWIN: it
// carries Plaid's own pending_transaction_id, which is what makes it a pull-forward candidate.
func newSettlementPage(id, pendingID, nextCursor string) *plaid.TransactionsSyncResponse {
	txn := plaid.Transaction{
		TransactionId: id, AccountId: "plaid-acc-1", Amount: 12.00, Date: "2024-03-01", Name: id,
	}
	txn.PendingTransactionId = *plaid.NewNullableString(&pendingID)

	page := &plaid.TransactionsSyncResponse{
		Added:    []plaid.Transaction{txn},
		Modified: []plaid.Transaction{},
		Removed:  []plaid.RemovedTransaction{},
	}
	page.SetNextCursor(nextCursor)
	page.SetHasMore(false)
	return page
}

// TestSyncTransactions_StrandedSettlementRepairFailure_LeavesCursorUnadvanced.
//
// The posted twin stores successfully and the annotation repair fails. If the cursor advanced here,
// Plaid would never hand us this transaction again and the repair would never get a second chance —
// the annotations would be stranded on the tombstoned pending row permanently.
func TestSyncTransactions_StrandedSettlementRepairFailure_LeavesCursorUnadvanced(t *testing.T) {
	ctx := context.Background()

	mockPlaidService := new(MockPlaidService)
	mockLinkedItemRepo := new(MockLinkedItemRepository)
	mockTransactionRepo := new(MockTransactionRepository)
	mockAccountRepo := new(MockAccountRepository)

	service := NewTransactionSyncService(
		nil, mockLinkedItemRepo, mockTransactionRepo, mockAccountRepo,
		nil, nil, mockPlaidService, nil, nil, nil, nil, nil, nil,
	)

	startCursor := "cursor-settle-0"
	linkedItem := &domain.LinkedItem{
		ID:          "linked-item-settle",
		UserID:      "user-settle",
		ItemID:      "plaid-item-settle",
		AccessToken: "access-token",
		Cursor:      &startCursor,
	}

	page := newSettlementPage("txn-posted-twin", "plaid-pending-777", "cursor-settle-1")
	// Both runs fetch the same page, because the failing run must not move the cursor.
	mockPlaidService.On("TransactionsSync", ctx, "access-token", startCursor, 0).Return(page, nil).Twice()

	mockAccountRepo.On("GetByLinkedItemIDIncludingRetained", ctx, "linked-item-settle").
		Return([]domain.Account{{ID: "acc-1", PlaidAccountID: "plaid-acc-1", UserID: "user-settle"}}, nil)
	mockTransactionRepo.On("CreateBatch", mock.Anything, mock.Anything).
		Return(map[string]bool{"txn-posted-twin": true}, nil)
	mockLinkedItemRepo.On("UpdateCursor", mock.Anything, mock.Anything, mock.Anything).Return(nil).Maybe()

	// The repair fails transiently on the first run, then succeeds.
	mockTransactionRepo.On("PullForwardSettledPendingAnnotations", mock.Anything, "user-settle", mock.Anything).
		Return(int64(0), errors.New("deadlock detected")).Once()
	mockTransactionRepo.On("PullForwardSettledPendingAnnotations", mock.Anything, "user-settle", mock.Anything).
		Return(int64(1), nil).Once()

	_, err := service.SyncTransactions(ctx, linkedItem)
	require.Error(t, err,
		"the settlement repair failed, so the sync must report failure rather than claim success")

	assert.Empty(t, persistedCursors(mockLinkedItemRepo),
		"the cursor MUST NOT advance when the stranded-annotation repair failed. Plaid does not "+
			"re-deliver a transaction the stored cursor is past, so advancing here strands the "+
			"user's category, notes, tags and review state on the tombstoned pending row FOREVER.")

	// The retry gets the same page and repairs it, which is the whole reason holding the cursor is
	// the right answer rather than merely a stricter one.
	_, err2 := service.SyncTransactions(ctx, linkedItem)
	require.NoError(t, err2, "the retry must succeed once the repair recovers")
	assert.Equal(t, []string{"cursor-settle-1"}, persistedCursors(mockLinkedItemRepo),
		"only the successful run advances the cursor")

	mockTransactionRepo.AssertExpectations(t)
	mockPlaidService.AssertExpectations(t)
}

// TestProcessModifiedTransactions_StrandedSettlementRepairFailureReturns.
//
// A `modified` delivery is the other way a stored row learns a pending pointer, so it is the other
// way the annotations can strand. The swallow was in the shared helper, so both callers had it.
func TestProcessModifiedTransactions_StrandedSettlementRepairFailureReturns(t *testing.T) {
	mockTransactionRepo := new(MockTransactionRepository)

	service := NewTransactionSyncService(
		nil, nil, mockTransactionRepo, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil,
	).(*transactionSyncService)

	ctx := context.Background()
	linkedItem := &domain.LinkedItem{ID: "linked-item-1586d", UserID: "user-1586d"}
	accountMap := map[string]string{"plaid-acc-1": "account-uuid-1"}

	txn := plaid.Transaction{
		TransactionId: "plaid-posted-2", AccountId: "plaid-acc-1", Amount: 10,
		Date: "2024-01-15", Name: "Posted twin",
	}
	pending := "plaid-pending-888"
	txn.PendingTransactionId = *plaid.NewNullableString(&pending)

	mockTransactionRepo.On("GetByExternalID", ctx, domain.SourcePlaid, "plaid-posted-2", "user-1586d").
		Return((*domain.Transaction)(nil), nil)
	mockTransactionRepo.On("CreateBatch", ctx, mock.Anything).Return(map[string]bool{}, nil)
	mockTransactionRepo.On("PullForwardSettledPendingAnnotations", ctx, "user-1586d", mock.Anything).
		Return(int64(0), errors.New("deadlock detected"))

	err := service.processModifiedTransactions(ctx, []plaid.Transaction{txn}, linkedItem, accountMap, nil)
	require.Error(t, err,
		"a failed settlement repair on the modified path must return, so the caller holds the cursor")
	mockTransactionRepo.AssertExpectations(t)
}
