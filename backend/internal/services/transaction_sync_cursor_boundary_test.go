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

// Issue #1570 (closed under #1569): the sync used to persist the Plaid cursor PER PAGE, inside the
// pagination loop, while every storage step runs AFTER the loop. Any failure in between therefore
// destroyed the rows for pages whose cursor had already moved — /transactions/sync does not
// re-deliver anything the stored cursor is past, and the worker discards the in-memory result on
// error. These tests pin the replacement contract: the cursor advances exactly once, strictly after
// the transaction-storage steps have committed, and never on any earlier error path.

// persistedCursors returns every value handed to UpdateCursor, in call order.
func persistedCursors(repo *MockLinkedItemRepository) []string {
	var out []string
	for _, c := range repo.Calls {
		if c.Method == "UpdateCursor" {
			out = append(out, c.Arguments.Get(2).(string))
		}
	}
	return out
}

func newCursorBoundaryPage(id string, nextCursor string, hasMore bool) *plaid.TransactionsSyncResponse {
	page := &plaid.TransactionsSyncResponse{
		Added: []plaid.Transaction{
			{TransactionId: id, AccountId: "plaid-acc-1", Amount: 12.00, Date: "2024-03-01", Name: id},
		},
		Modified: []plaid.Transaction{},
		Removed:  []plaid.RemovedTransaction{},
	}
	page.SetNextCursor(nextCursor)
	page.SetHasMore(hasMore)
	return page
}

// TestSyncTransactions_StorageFailureAfterMultiPageFetch_LeavesCursorUnadvanced is the data-loss
// regression for #1570. Storage fails (buildAccountMaps) after both pages were fetched; under the
// old per-page cursor writes, page 1's and page 2's cursors were already durable, so those rows were
// gone forever. Under the storage-before-cursor contract the cursor must not move at all, and the
// next run must re-fetch from the ORIGINAL cursor and store everything.
func TestSyncTransactions_StorageFailureAfterMultiPageFetch_LeavesCursorUnadvanced(t *testing.T) {
	ctx := context.Background()

	mockPlaidService := new(MockPlaidService)
	mockLinkedItemRepo := new(MockLinkedItemRepository)
	mockTransactionRepo := new(MockTransactionRepository)
	mockAccountRepo := new(MockAccountRepository)

	service := NewTransactionSyncService(
		nil, mockLinkedItemRepo, mockTransactionRepo, mockAccountRepo,
		nil, nil, mockPlaidService, nil, nil, nil, nil, nil, nil,
	)

	startCursor := "cursor-0"
	linkedItem := &domain.LinkedItem{
		ID:          "linked-item-loss",
		UserID:      "user-loss",
		ItemID:      "plaid-item-loss",
		AccessToken: "access-token",
		Cursor:      &startCursor,
	}

	page1 := newCursorBoundaryPage("txn-p1", "cursor-1", true)
	page2 := newCursorBoundaryPage("txn-p2", "cursor-2", false)

	// Both syncs fetch the same two pages, because the first run must not move the cursor.
	mockPlaidService.On("TransactionsSync", ctx, "access-token", startCursor, 0).Return(page1, nil).Twice()
	mockPlaidService.On("TransactionsSync", ctx, "access-token", "cursor-1", 0).Return(page2, nil).Twice()

	// Storage fails on the first run only.
	mockAccountRepo.On("GetByLinkedItemID", ctx, "linked-item-loss").
		Return([]domain.Account(nil), errors.New("accounts table unavailable")).Once()
	mockAccountRepo.On("GetByLinkedItemID", ctx, "linked-item-loss").
		Return([]domain.Account{{ID: "acc-1", PlaidAccountID: "plaid-acc-1", UserID: "user-loss"}}, nil)

	mockLinkedItemRepo.On("UpdateCursor", mock.Anything, mock.Anything, mock.Anything).Return(nil).Maybe()
	mockTransactionRepo.On("CreateBatch", mock.Anything, mock.Anything).
		Return(map[string]bool{"txn-p1": true, "txn-p2": true}, nil).Maybe()

	_, err := service.SyncTransactions(ctx, linkedItem)
	require.Error(t, err, "storage failed, so the sync must report failure")

	assert.Empty(t, persistedCursors(mockLinkedItemRepo),
		"the cursor MUST NOT advance when storage failed. Any value persisted here is permanent data "+
			"loss: the worker discards the in-memory result on error and Plaid will not re-deliver "+
			"rows the stored cursor has moved past.")
	assert.Empty(t, storedExternalIDs(mockTransactionRepo),
		"nothing reached storage on the failing run — which is exactly why the cursor must not move")

	// The caller (worker) leaves linkedItem.Cursor alone; the next run therefore starts from
	// startCursor and must recover every row.
	_, err2 := service.SyncTransactions(ctx, linkedItem)
	require.NoError(t, err2, "the retry must succeed once storage recovers")

	stored := storedExternalIDs(mockTransactionRepo)
	assert.Equal(t, 1, stored["txn-p1"],
		"page 1 must be recovered by the retry — this is the row the old per-page cursor write lost")
	assert.Equal(t, 1, stored["txn-p2"], "page 2 must be recovered by the retry")

	assert.Equal(t, []string{"cursor-2"}, persistedCursors(mockLinkedItemRepo),
		"only the successful run advances the cursor, and it advances once, to the final page's cursor")

	mockPlaidService.AssertExpectations(t)
}

// TestSyncTransactions_MultiPageSuccess_AdvancesCursorOnceAfterStorage pins both halves of the
// contract: exactly one cursor write, and it happens strictly after transactions were stored.
func TestSyncTransactions_MultiPageSuccess_AdvancesCursorOnceAfterStorage(t *testing.T) {
	ctx := context.Background()

	mockPlaidService := new(MockPlaidService)
	mockLinkedItemRepo := new(MockLinkedItemRepository)
	mockTransactionRepo := new(MockTransactionRepository)
	mockAccountRepo := new(MockAccountRepository)

	service := NewTransactionSyncService(
		nil, mockLinkedItemRepo, mockTransactionRepo, mockAccountRepo,
		nil, nil, mockPlaidService, nil, nil, nil, nil, nil, nil,
	)

	startCursor := "cursor-0"
	linkedItem := &domain.LinkedItem{
		ID:          "linked-item-order",
		UserID:      "user-order",
		ItemID:      "plaid-item-order",
		AccessToken: "access-token",
		Cursor:      &startCursor,
	}

	mockPlaidService.On("TransactionsSync", ctx, "access-token", startCursor, 0).
		Return(newCursorBoundaryPage("txn-p1", "cursor-1", true), nil).Once()
	mockPlaidService.On("TransactionsSync", ctx, "access-token", "cursor-1", 0).
		Return(newCursorBoundaryPage("txn-p2", "cursor-2", true), nil).Once()
	mockPlaidService.On("TransactionsSync", ctx, "access-token", "cursor-2", 0).
		Return(newCursorBoundaryPage("txn-p3", "cursor-3", false), nil).Once()

	mockAccountRepo.On("GetByLinkedItemID", ctx, "linked-item-order").
		Return([]domain.Account{{ID: "acc-1", PlaidAccountID: "plaid-acc-1", UserID: "user-order"}}, nil)

	stored := false
	mockTransactionRepo.On("CreateBatch", mock.Anything, mock.Anything).
		Run(func(mock.Arguments) { stored = true }).
		Return(map[string]bool{"txn-p1": true, "txn-p2": true, "txn-p3": true}, nil)

	mockLinkedItemRepo.On("UpdateCursor", mock.Anything, "linked-item-order", mock.Anything).
		Run(func(args mock.Arguments) {
			assert.True(t, stored,
				"UpdateCursor(%q) ran BEFORE transactions were stored. The whole point of #1570 is "+
					"that the cursor advance strictly follows the storage commit.", args.Get(2))
		}).
		Return(nil)

	result, err := service.SyncTransactions(ctx, linkedItem)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Len(t, result.Added, 3, "all three pages are accumulated before storage")

	assert.Equal(t, []string{"cursor-3"}, persistedCursors(mockLinkedItemRepo),
		"a 3-page sync must write the cursor exactly ONCE, with the final page's cursor")
	assert.Equal(t, "cursor-3", result.NewCursor)

	mockPlaidService.AssertExpectations(t)
}

// TestSyncTransactions_BlankFinalPageCursor_PersistsLastGoodCursorAfterStorage keeps preserved
// behaviour 3 honest under the new boundary: a blank next_cursor on the final page must never be
// persisted, but the last real cursor earned this run still gets written — once, after storage.
func TestSyncTransactions_BlankFinalPageCursor_PersistsLastGoodCursorAfterStorage(t *testing.T) {
	ctx := context.Background()

	mockPlaidService := new(MockPlaidService)
	mockLinkedItemRepo := new(MockLinkedItemRepository)
	mockTransactionRepo := new(MockTransactionRepository)
	mockAccountRepo := new(MockAccountRepository)

	service := NewTransactionSyncService(
		nil, mockLinkedItemRepo, mockTransactionRepo, mockAccountRepo,
		nil, nil, mockPlaidService, nil, nil, nil, nil, nil, nil,
	)

	startCursor := "cursor-0"
	linkedItem := &domain.LinkedItem{
		ID:          "linked-item-blankfinal",
		UserID:      "user-blankfinal",
		ItemID:      "plaid-item-blankfinal",
		AccessToken: "access-token",
		Cursor:      &startCursor,
	}

	mockPlaidService.On("TransactionsSync", ctx, "access-token", startCursor, 0).
		Return(newCursorBoundaryPage("txn-p1", "cursor-1", true), nil).Once()
	mockPlaidService.On("TransactionsSync", ctx, "access-token", "cursor-1", 0).
		Return(newCursorBoundaryPage("txn-p2", "", false), nil).Once()

	mockAccountRepo.On("GetByLinkedItemID", ctx, "linked-item-blankfinal").
		Return([]domain.Account{{ID: "acc-1", PlaidAccountID: "plaid-acc-1", UserID: "user-blankfinal"}}, nil)
	mockTransactionRepo.On("CreateBatch", mock.Anything, mock.Anything).
		Return(map[string]bool{"txn-p1": true, "txn-p2": true}, nil)
	mockLinkedItemRepo.On("UpdateCursor", mock.Anything, mock.Anything, mock.Anything).Return(nil).Maybe()

	_, err := service.SyncTransactions(ctx, linkedItem)
	require.NoError(t, err)

	assert.Equal(t, []string{"cursor-1"}, persistedCursors(mockLinkedItemRepo),
		"the blank must never be persisted (it would force a full historical re-sync), but page 1's "+
			"cursor is real and was earned by a committed store")

	stored := storedExternalIDs(mockTransactionRepo)
	assert.Equal(t, 1, stored["txn-p1"])
	assert.Equal(t, 1, stored["txn-p2"], "the final page's rows are stored even though its cursor was blank")

	mockPlaidService.AssertExpectations(t)
}

// TestSyncTransactions_SinglePageBlankCursor_PersistsNothing is preserved behaviour 3 in its
// degenerate form: nothing was earned this run, so nothing is written.
func TestSyncTransactions_SinglePageBlankCursor_PersistsNothing(t *testing.T) {
	ctx := context.Background()

	mockPlaidService := new(MockPlaidService)
	mockLinkedItemRepo := new(MockLinkedItemRepository)
	mockTransactionRepo := new(MockTransactionRepository)
	mockAccountRepo := new(MockAccountRepository)

	service := NewTransactionSyncService(
		nil, mockLinkedItemRepo, mockTransactionRepo, mockAccountRepo,
		nil, nil, mockPlaidService, nil, nil, nil, nil, nil, nil,
	)

	startCursor := "cursor-earned-over-many-syncs"
	linkedItem := &domain.LinkedItem{
		ID:          "linked-item-single-blank",
		UserID:      "user-single-blank",
		ItemID:      "plaid-item-single-blank",
		AccessToken: "access-token",
		Cursor:      &startCursor,
	}

	mockPlaidService.On("TransactionsSync", ctx, "access-token", startCursor, 0).
		Return(newCursorBoundaryPage("txn-only", "", false), nil).Once()
	mockAccountRepo.On("GetByLinkedItemID", ctx, "linked-item-single-blank").
		Return([]domain.Account{{ID: "acc-1", PlaidAccountID: "plaid-acc-1", UserID: "user-single-blank"}}, nil)
	mockTransactionRepo.On("CreateBatch", mock.Anything, mock.Anything).
		Return(map[string]bool{"txn-only": true}, nil)
	mockLinkedItemRepo.On("UpdateCursor", mock.Anything, mock.Anything, mock.Anything).Return(nil).Maybe()

	_, err := service.SyncTransactions(ctx, linkedItem)
	require.NoError(t, err)

	assert.Empty(t, persistedCursors(mockLinkedItemRepo),
		"no real cursor was returned this run, so the stored one stays exactly as it was")

	mockPlaidService.AssertExpectations(t)
}

// TestSyncTransactions_PageFetchFailure_LeavesCursorUnadvanced covers the other pre-storage error
// path: Plaid itself fails on page 2. The rows from page 1 are discarded by the worker, so the
// cursor must still point at the start of the run.
func TestSyncTransactions_PageFetchFailure_LeavesCursorUnadvanced(t *testing.T) {
	ctx := context.Background()

	mockPlaidService := new(MockPlaidService)
	mockLinkedItemRepo := new(MockLinkedItemRepository)
	mockTransactionRepo := new(MockTransactionRepository)
	mockAccountRepo := new(MockAccountRepository)

	service := NewTransactionSyncService(
		nil, mockLinkedItemRepo, mockTransactionRepo, mockAccountRepo,
		nil, nil, mockPlaidService, nil, nil, nil, nil, nil, nil,
	)

	startCursor := "cursor-0"
	linkedItem := &domain.LinkedItem{
		ID:          "linked-item-fetchfail",
		UserID:      "user-fetchfail",
		ItemID:      "plaid-item-fetchfail",
		AccessToken: "access-token",
		Cursor:      &startCursor,
	}

	mockPlaidService.On("TransactionsSync", ctx, "access-token", startCursor, 0).
		Return(newCursorBoundaryPage("txn-p1", "cursor-1", true), nil).Once()
	mockPlaidService.On("TransactionsSync", ctx, "access-token", "cursor-1", 0).
		Return((*plaid.TransactionsSyncResponse)(nil), errors.New("plaid 500")).Once()

	mockAccountRepo.On("GetByLinkedItemID", ctx, "linked-item-fetchfail").
		Return([]domain.Account{{ID: "acc-1", PlaidAccountID: "plaid-acc-1", UserID: "user-fetchfail"}}, nil).Maybe()
	mockTransactionRepo.On("CreateBatch", mock.Anything, mock.Anything).
		Return(map[string]bool{}, nil).Maybe()
	mockLinkedItemRepo.On("UpdateCursor", mock.Anything, mock.Anything, mock.Anything).Return(nil).Maybe()

	_, err := service.SyncTransactions(ctx, linkedItem)
	require.Error(t, err)

	assert.Empty(t, persistedCursors(mockLinkedItemRepo),
		"page 1 was fetched but never stored, so persisting its cursor would lose it outright")

	mockPlaidService.AssertExpectations(t)
}

// TestSyncTransactions_ModifiedStorageFailure_LeavesCursorUnadvanced covers Step 7, the partial-commit
// step the contract comment names explicitly: processModifiedTransactions commits one Update PER ROW
// and returns on the first failure, so a 2-of-3 commit is possible. The cursor must not move.
func TestSyncTransactions_ModifiedStorageFailure_LeavesCursorUnadvanced(t *testing.T) {
	ctx := context.Background()

	mockPlaidService := new(MockPlaidService)
	mockLinkedItemRepo := new(MockLinkedItemRepository)
	mockTransactionRepo := new(MockTransactionRepository)
	mockAccountRepo := new(MockAccountRepository)

	service := NewTransactionSyncService(
		nil, mockLinkedItemRepo, mockTransactionRepo, mockAccountRepo,
		nil, nil, mockPlaidService, nil, nil, nil, nil, nil, nil,
	)

	startCursor := "cursor-0"
	linkedItem := &domain.LinkedItem{
		ID:          "linked-item-modfail",
		UserID:      "user-modfail",
		ItemID:      "plaid-item-modfail",
		AccessToken: "access-token",
		Cursor:      &startCursor,
	}

	page := &plaid.TransactionsSyncResponse{
		Added: []plaid.Transaction{},
		Modified: []plaid.Transaction{
			{TransactionId: "txn-mod-1", AccountId: "plaid-acc-1", Amount: 1.00, Date: "2024-03-01", Name: "Mod one"},
			{TransactionId: "txn-mod-2", AccountId: "plaid-acc-1", Amount: 2.00, Date: "2024-03-02", Name: "Mod two"},
		},
		Removed: []plaid.RemovedTransaction{},
	}
	page.SetNextCursor("cursor-1")
	page.SetHasMore(false)
	mockPlaidService.On("TransactionsSync", ctx, "access-token", startCursor, 0).Return(page, nil).Once()

	mockAccountRepo.On("GetByLinkedItemID", ctx, "linked-item-modfail").
		Return([]domain.Account{{ID: "acc-1", PlaidAccountID: "plaid-acc-1", UserID: "user-modfail"}}, nil)
	mockTransactionRepo.On("GetByExternalID", mock.Anything, domain.SourcePlaid, mock.Anything, "user-modfail").
		Return(&domain.Transaction{ID: "existing"}, nil)
	// Row 1 commits, row 2 fails — the partial-commit shape.
	mockTransactionRepo.On("Update", mock.Anything, mock.Anything).Return(nil).Once()
	mockTransactionRepo.On("Update", mock.Anything, mock.Anything).Return(errors.New("update failed on row 2")).Once()
	mockLinkedItemRepo.On("UpdateCursor", mock.Anything, mock.Anything, mock.Anything).Return(nil).Maybe()

	_, err := service.SyncTransactions(ctx, linkedItem)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to process modified transactions")

	assert.Empty(t, persistedCursors(mockLinkedItemRepo),
		"Step 7 committed row 1 and failed on row 2. Advancing the cursor would strand row 2's "+
			"modification forever; holding it back re-delivers both and re-applies row 1 idempotently.")

	mockPlaidService.AssertExpectations(t)
}

// TestSyncTransactions_RemovedDeleteError_StillAdvancesCursor pins Step 8 as it actually behaves,
// which is NOT symmetric with Steps 6 and 7.
//
// processRemovedTransactions returns nil on EVERY path: a genuine DeleteByExternalID database error
// is Warn'd and counted as "skipped" for idempotency, so Step 8 can never gate the cursor. This test
// exists so the asymmetry is pinned rather than assumed — it is residual path (4) in the
// storage-before-cursor contract comment. The failure direction is safe for #1569 (the row stays
// visible rather than vanishing), and task 7 converts this path to tombstones. If someone later makes
// this path return an error, this test fails and points them at the contract comment.
func TestSyncTransactions_RemovedDeleteError_StillAdvancesCursor(t *testing.T) {
	ctx := context.Background()

	mockPlaidService := new(MockPlaidService)
	mockLinkedItemRepo := new(MockLinkedItemRepository)
	mockTransactionRepo := new(MockTransactionRepository)
	mockAccountRepo := new(MockAccountRepository)

	service := NewTransactionSyncService(
		nil, mockLinkedItemRepo, mockTransactionRepo, mockAccountRepo,
		nil, nil, mockPlaidService, nil, nil, nil, nil, nil, nil,
	)

	startCursor := "cursor-0"
	linkedItem := &domain.LinkedItem{
		ID:          "linked-item-remfail",
		UserID:      "user-remfail",
		ItemID:      "plaid-item-remfail",
		AccessToken: "access-token",
		Cursor:      &startCursor,
	}

	page := &plaid.TransactionsSyncResponse{
		Added:    []plaid.Transaction{},
		Modified: []plaid.Transaction{},
		Removed:  []plaid.RemovedTransaction{{TransactionId: stringPtr("txn-rem-1")}},
	}
	page.SetNextCursor("cursor-1")
	page.SetHasMore(false)
	mockPlaidService.On("TransactionsSync", ctx, "access-token", startCursor, 0).Return(page, nil).Once()

	mockAccountRepo.On("GetByLinkedItemID", ctx, "linked-item-remfail").
		Return([]domain.Account{{ID: "acc-1", PlaidAccountID: "plaid-acc-1", UserID: "user-remfail"}}, nil)
	mockTransactionRepo.On("DeleteByExternalID", mock.Anything, domain.SourcePlaid, "txn-rem-1", "user-remfail").
		Return(errors.New("deadlock detected")).Once()
	mockLinkedItemRepo.On("UpdateCursor", mock.Anything, mock.Anything, mock.Anything).Return(nil).Maybe()

	_, err := service.SyncTransactions(ctx, linkedItem)
	require.NoError(t, err, "processRemovedTransactions swallows delete errors and returns nil")

	assert.Equal(t, []string{"cursor-1"}, persistedCursors(mockLinkedItemRepo),
		"Step 8 cannot gate the cursor: the delete error was swallowed, so the sync 'succeeded' and "+
			"the cursor advanced past a removal that was never applied. Safe for #1569 (the row is "+
			"retained, not destroyed); tracked as residual path (4) and owned by task 7.")

	mockPlaidService.AssertExpectations(t)
}
