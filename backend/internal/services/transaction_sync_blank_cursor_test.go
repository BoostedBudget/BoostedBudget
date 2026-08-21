package services

import (
	"context"
	"testing"

	"github.com/plaid/plaid-go/v23/plaid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
)

// TestSyncTransactions_BlankNextCursor_DoesNotOverwriteStoredCursor pins the LIVE path against a
// blank cursor write. Issue #1537 round 9.
//
// WHY THIS IS NOT A DUPLICATE OF AN EXISTING GUARD. Round 4 added exactly this check to
// workers/plaid_sync_provider.go — but `NewPlaidSyncProvider` has **no non-test caller** (its own
// comprehensive test says so at plaid_sync_provider_comprehensive_test.go:777), so the guard went
// into dead code while the path that actually runs kept the bug. The live path is
// transactionSyncService.SyncTransactions, reached from PlaidSyncWorker (queue/jobs/plaid_sync.go)
// and wired in cmd/server/main.go.
//
// THE FAILURE THIS PREVENTS, traced rather than assumed: an empty stored cursor makes
// SyncTransactions take the no-cursor branch on the next run (the `if cursor == ""` historical-depth
// lookup), and plaid_service.go then sends the request with initial-sync historical options and no
// cursor. Plaid replays the whole configured historical window through CreateBatch — so a single
// blank write converts every subsequent incremental sync into a full re-sync.
//
// ⚠ The blank write must stay legal on ONE path: the PlaidErrorInvalidCursor branch resets the
// cursor to "" deliberately, to force exactly that full re-sync. TestSyncTransactions_InvalidCursor…
// pins it, and it must keep passing — which is why the guard belongs at the per-page update site and
// NOT inside UpdateCursor.
func TestSyncTransactions_BlankNextCursor_DoesNotOverwriteStoredCursor(t *testing.T) {
	ctx := context.Background()

	mockPlaidService := new(MockPlaidService)
	mockLinkedItemRepo := new(MockLinkedItemRepository)
	mockTransactionRepo := new(MockTransactionRepository)
	mockAccountRepo := new(MockAccountRepository)

	service := NewTransactionSyncService(
		nil, // webhookRepo
		mockLinkedItemRepo,
		mockTransactionRepo,
		mockAccountRepo,
		nil, // profileRepo — unreached: a non-empty cursor skips the historical-depth lookup
		nil, // balanceSnapshotRepo
		mockPlaidService,
		nil, // subscriptionService
		nil, // ruleApplicationService
		nil, // budgetSyncService
		nil, // transactionMonitorService
		nil, // merchantRepo
		nil, // logger
	)

	goodCursor := "cursor-earned-over-many-syncs"
	linkedItem := &domain.LinkedItem{
		ID:          "linked-item-123",
		UserID:      "user-123",
		ItemID:      "plaid-item-123",
		AccessToken: "access-token",
		Cursor:      &goodCursor,
	}

	mockAccountRepo.On("GetByLinkedItemID", ctx, "linked-item-123").
		Return([]domain.Account{{ID: "acc-uuid-1", PlaidAccountID: "plaid-acc-1", UserID: "user-123"}}, nil)

	// A successful page that carries NO next_cursor. This is the shape under test.
	resp := &plaid.TransactionsSyncResponse{
		Added:    []plaid.Transaction{},
		Modified: []plaid.Transaction{},
		Removed:  []plaid.RemovedTransaction{},
	}
	resp.SetNextCursor("")
	resp.SetHasMore(false)
	mockPlaidService.On("TransactionsSync", ctx, "access-token", goodCursor, 0).Return(resp, nil)

	// Permissive so an unexpected call is a readable ASSERTION failure rather than a mock panic.
	mockLinkedItemRepo.On("UpdateCursor", mock.Anything, mock.Anything, mock.Anything).Return(nil).Maybe()

	result, err := service.SyncTransactions(ctx, linkedItem)
	require.NoError(t, err)
	require.NotNil(t, result)

	mockLinkedItemRepo.AssertNotCalled(t, "UpdateCursor", ctx, "linked-item-123", "")

	for _, c := range mockLinkedItemRepo.Calls {
		if c.Method == "UpdateCursor" {
			assert.NotEmpty(t, c.Arguments.Get(2),
				"UpdateCursor was called with an EMPTY cursor, overwriting %q. Plaid sent no "+
					"next_cursor on a successful page; persisting that blank makes the next sync take "+
					"the no-cursor branch and replay the item's entire historical window.", goodCursor)
		}
	}
}

// TestSyncTransactions_BlankNextCursorWithHasMore_FailsClosedWithoutLooping is the case the test above
// could not see, and it is a defect the round-9 fix INTRODUCED. Issue #1537 round 10.
//
// The pagination loop is `for hasMore`, and the round-9 guard moved `cursor = newCursor` into the else
// branch. So when Plaid returns has_more=true with next_cursor="":
//
//   - cursor is not advanced,
//   - hasMore = resp.GetHasMore() stays true,
//   - the next iteration reissues the IDENTICAL request.
//
// There is no page cap anywhere in the loop (verified: the only non-error exit is hasMore going false),
// so that repeats until the context is cancelled, re-processing the same page each time. The pre-round-9
// code was wrong but TERMINATING — it persisted "" and the next scheduled sync did a full re-sync. So
// the guard turned bad data into a possible hang, which is a worse failure mode.
//
// Plaid's contract: when has_more=true the client must call again WITH next_cursor. An empty cursor
// alongside has_more=true means the page is not yet available, so there is nothing safe to do with it —
// fail closed and let the job retry.
//
// HOW THIS TEST SEES A LOOP. Asserting only "an error was returned" would pass even if the loop ran
// twice first, so the load-bearing assertion is the Plaid CALL COUNT. The second stubbed response
// terminates the loop (has_more=false) purely so a regressed implementation fails fast instead of
// hanging the suite — it is scaffolding, not the thing under test.
func TestSyncTransactions_BlankNextCursorWithHasMore_FailsClosedWithoutLooping(t *testing.T) {
	ctx := context.Background()

	mockPlaidService := new(MockPlaidService)
	mockLinkedItemRepo := new(MockLinkedItemRepository)
	mockTransactionRepo := new(MockTransactionRepository)
	mockAccountRepo := new(MockAccountRepository)

	service := NewTransactionSyncService(
		nil, mockLinkedItemRepo, mockTransactionRepo, mockAccountRepo,
		nil, nil, mockPlaidService, nil, nil, nil, nil, nil, nil,
	)

	goodCursor := "cursor-earned-over-many-syncs"
	linkedItem := &domain.LinkedItem{
		ID:          "linked-item-loop",
		UserID:      "user-loop",
		ItemID:      "plaid-item-loop",
		AccessToken: "access-token",
		Cursor:      &goodCursor,
	}

	mockAccountRepo.On("GetByLinkedItemID", ctx, "linked-item-loop").
		Return([]domain.Account{{ID: "acc-1", PlaidAccountID: "plaid-acc-1", UserID: "user-loop"}}, nil).Maybe()

	// Response 1: the contract violation under test — more pages promised, no cursor to fetch them with.
	bad := &plaid.TransactionsSyncResponse{
		Added: []plaid.Transaction{}, Modified: []plaid.Transaction{}, Removed: []plaid.RemovedTransaction{},
	}
	bad.SetNextCursor("")
	bad.SetHasMore(true)

	// Response 2 exists ONLY so a regressed implementation terminates instead of hanging the suite.
	terminating := &plaid.TransactionsSyncResponse{
		Added: []plaid.Transaction{}, Modified: []plaid.Transaction{}, Removed: []plaid.RemovedTransaction{},
	}
	terminating.SetNextCursor("cursor-next")
	terminating.SetHasMore(false)

	mockPlaidService.On("TransactionsSync", ctx, "access-token", goodCursor, 0).Return(bad, nil).Once()
	mockPlaidService.On("TransactionsSync", ctx, "access-token", goodCursor, 0).Return(terminating, nil).Maybe()

	mockLinkedItemRepo.On("UpdateCursor", mock.Anything, mock.Anything, mock.Anything).Return(nil).Maybe()

	_, err := service.SyncTransactions(ctx, linkedItem)

	syncCalls := 0
	for _, c := range mockPlaidService.Calls {
		if c.Method == "TransactionsSync" {
			syncCalls++
		}
	}
	assert.Equal(t, 1, syncCalls,
		"TransactionsSync was called %d times. With has_more=true and an empty next_cursor the cursor "+
			"cannot advance, so every extra call is the SAME request — an unbounded re-request loop, "+
			"since the page loop has no cap and its only non-error exit is has_more going false.", syncCalls)
	require.Error(t, err,
		"an empty next_cursor alongside has_more=true is a Plaid contract violation with no safe "+
			"continuation; it must fail closed so the job retries, not spin")

	for _, c := range mockLinkedItemRepo.Calls {
		if c.Method == "UpdateCursor" {
			assert.NotEmpty(t, c.Arguments.Get(2),
				"the blank cursor must still never be persisted, even on the failing path")
		}
	}
}

// TestSyncTransactions_BlankNextCursorWithHasMore_KeepsAlreadyFetchedPages is the axis the round-10
// fix missed, and it is the third time this branch has traded one failure mode for a worse one.
// Issue #1537 round 11.
//
// THE LOSS. `UpdateCursor` runs INSIDE the pagination loop (per page), while storage happens AFTER it.
// So on a two-page sync where page 1 succeeds and page 2 returns has_more=true with an empty cursor:
//
//	1. page 1's transactions are appended to `result` and page 1's cursor is PERSISTED;
//	2. round 10's guard returned an error before any storage;
//	3. the worker reads `result` only on the success path (queue/jobs/plaid_sync.go) — on error it is
//	   discarded entirely.
//
// The retry then resumes from page 1's cursor, and /transactions/sync does not re-deliver page 1. Those
// transactions are gone. The condition is server-side and deterministic on retry, so all five attempts
// burn and the item dead-letters with a silent hole in its history.
//
// ⚠ This was a REGRESSION against the pre-round-9 code, which is the part I got wrong twice. That code
// set cursor="" and re-entered the loop, so Plaid replayed the window inline and everything was stored
// — expensive, but lossless. "Bad data became a possible hang" was true only for a single-page sync;
// for N>1 it became silent data loss.
//
// THE FIX, and why it is not "return the error differently": the cursor already sits at page 1, so the
// lossless move is to keep what was fetched. If we made progress, stop paginating and return the
// accumulated pages with NO error, so the worker stores them and the next sync resumes exactly where
// the cursor points. Only when we made NO progress (the very first page came back malformed) is an
// error right — otherwise the job would "succeed" having done nothing, forever, which is a silent
// stall and worse for an operator than a visible failure. Both branches are tested.
func TestSyncTransactions_BlankNextCursorWithHasMore_KeepsAlreadyFetchedPages(t *testing.T) {
	ctx := context.Background()

	mockPlaidService := new(MockPlaidService)
	mockLinkedItemRepo := new(MockLinkedItemRepository)
	mockTransactionRepo := new(MockTransactionRepository)
	mockAccountRepo := new(MockAccountRepository)

	service := NewTransactionSyncService(
		nil, mockLinkedItemRepo, mockTransactionRepo, mockAccountRepo,
		nil, nil, mockPlaidService, nil, nil, nil, nil, nil, nil,
	)

	startCursor := "cursor-page-0"
	linkedItem := &domain.LinkedItem{
		ID:          "linked-item-multipage",
		UserID:      "user-multipage",
		ItemID:      "plaid-item-multipage",
		AccessToken: "access-token",
		Cursor:      &startCursor,
	}

	mockAccountRepo.On("GetByLinkedItemID", ctx, "linked-item-multipage").
		Return([]domain.Account{{ID: "acc-1", PlaidAccountID: "plaid-acc-1", UserID: "user-multipage"}}, nil).Maybe()

	// Page 1: a normal, successful page carrying real transactions and a real cursor.
	page1 := &plaid.TransactionsSyncResponse{
		Added: []plaid.Transaction{
			{TransactionId: "txn-page1", AccountId: "plaid-acc-1", Amount: 42.00, Date: "2024-01-15", Name: "Page one"},
		},
		Modified: []plaid.Transaction{}, Removed: []plaid.RemovedTransaction{},
	}
	page1.SetNextCursor("cursor-page-1")
	page1.SetHasMore(true)

	// Page 2: the malformed shape — more promised, no cursor to fetch it with.
	page2 := &plaid.TransactionsSyncResponse{
		Added: []plaid.Transaction{}, Modified: []plaid.Transaction{}, Removed: []plaid.RemovedTransaction{},
	}
	page2.SetNextCursor("")
	page2.SetHasMore(true)

	mockPlaidService.On("TransactionsSync", ctx, "access-token", startCursor, 0).Return(page1, nil).Once()
	mockPlaidService.On("TransactionsSync", ctx, "access-token", "cursor-page-1", 0).Return(page2, nil).Once()
	mockLinkedItemRepo.On("UpdateCursor", mock.Anything, mock.Anything, mock.Anything).Return(nil).Maybe()

	// Storage must be REACHED — that is the whole point. SyncTransactions does its own CreateBatch
	// (transaction_sync_service.go:589 -> :863), so the round-10 error path returned before its own
	// storage step. Stubbing it lets us assert page 1 actually arrives there.
	mockTransactionRepo.On("CreateBatch", mock.Anything, mock.Anything).
		Return(map[string]bool{"txn-page1": true}, nil).Maybe()

	result, err := service.SyncTransactions(ctx, linkedItem)

	require.NoError(t, err,
		"page 1 was fetched and its cursor persisted, so returning an error makes the worker discard "+
			"page 1's transactions while the cursor has already moved past them — Plaid will not "+
			"re-deliver them. Having made progress, stop paginating and hand back what we have.")
	require.NotNil(t, result)
	require.Len(t, result.Added, 1,
		"page 1's transactions must survive; they are the data that would otherwise be lost")
	assert.Equal(t, "txn-page1", result.Added[0].GetTransactionId())

	// The blank cursor must still never be persisted, and page 1's real cursor must be.
	var persisted []string
	for _, c := range mockLinkedItemRepo.Calls {
		if c.Method == "UpdateCursor" {
			persisted = append(persisted, c.Arguments.Get(2).(string))
		}
	}
	assert.Equal(t, []string{"cursor-page-1"}, persisted,
		"exactly page 1's cursor should be persisted — never the blank")

	// The strongest form of the assertion: page 1's transaction reached STORAGE, not merely the
	// returned struct. Under the round-10 behaviour CreateBatch was never called at all.
	stored := 0
	for _, c := range mockTransactionRepo.Calls {
		if c.Method != "CreateBatch" {
			continue
		}
		txns, ok := c.Arguments.Get(1).([]domain.Transaction)
		if !ok {
			continue
		}
		for _, txn := range txns {
			if txn.ExternalTransactionID != nil && *txn.ExternalTransactionID == "txn-page1" {
				stored++
			}
		}
	}
	assert.Equal(t, 1, stored,
		"page 1's transaction must reach CreateBatch. Returning an error before the storage step is "+
			"what lost it, while its cursor had already been persisted.")
}
