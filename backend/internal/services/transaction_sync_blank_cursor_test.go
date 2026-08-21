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
