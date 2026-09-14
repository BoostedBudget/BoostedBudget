package services

import (
	"context"
	"errors"
	"testing"

	"github.com/plaid/plaid-go/v23/plaid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/maxnussbaum/BoostedBudget/internal/config"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	phclient "github.com/maxnussbaum/BoostedBudget/internal/integration/posthog"
	"github.com/maxnussbaum/BoostedBudget/internal/repository"
)

type recordingSyncProductAnalytics struct {
	userID string
	event  phclient.TypedEvent
	events []phclient.TypedEvent
}

func (r *recordingSyncProductAnalytics) CaptureEvent(_ context.Context, userID string, event phclient.TypedEvent) {
	r.userID = userID
	r.event = event
	r.events = append(r.events, event)
}

type memorySyncCompletionRecorder struct {
	completed map[string]bool
}

func (r *memorySyncCompletionRecorder) RecordTransactionSyncCompletion(_ context.Context, id, _ string, _ *string) (repository.TransactionSyncCompletionDisposition, error) {
	if r.completed == nil {
		r.completed = make(map[string]bool)
	}
	first := !r.completed[id]
	r.completed[id] = true
	if first {
		return repository.TransactionSyncCompletionInitial, nil
	}
	return repository.TransactionSyncCompletionRefresh, nil
}

func TestSyncTransactions_BlankFinalCursorUsesDurableLatchAcrossRetriesAndRelink(t *testing.T) {
	ctx := context.Background()
	tierConfig, err := config.LoadTierConfig()
	require.NoError(t, err)
	subscriptions := NewSubscriptionService(tierConfig, nil)
	plaidService := new(MockPlaidService)
	linkedItems := new(MockLinkedItemRepository)
	transactions := new(MockTransactionRepository)
	accounts := new(MockAccountRepository)
	profiles := new(mockProfileRepoForCursorTests)
	service := NewTransactionSyncService(nil, linkedItems, transactions, accounts, profiles, nil,
		plaidService, subscriptions, nil, nil, nil, nil, nil)
	analytics := &recordingSyncProductAnalytics{}
	latch := &memorySyncCompletionRecorder{}
	SetTransactionSyncProductAnalytics(service, analytics)
	SetTransactionSyncCompletionRecorder(service, latch)

	user := &domain.User{ID: "user-1", SubscriptionTier: string(domain.TierPremium)}
	profiles.On("GetUserByID", ctx, "user-1").Return(user, nil).Times(3)
	accounts.On("GetByLinkedItemIDIncludingRetained", ctx, mock.Anything).
		Return([]domain.Account{}, nil).Times(3)
	finalBlank := &plaid.TransactionsSyncResponse{}
	finalBlank.SetNextCursor("")
	finalBlank.SetHasMore(false)
	plaidService.On("TransactionsSync", ctx, "token", "", 730).Return(finalBlank, nil).Times(3)

	item := &domain.LinkedItem{ID: "item-1", UserID: "user-1", AccessToken: "token"}
	_, err = service.SyncTransactions(ctx, item)
	require.NoError(t, err)
	_, err = service.SyncTransactions(ctx, item)
	require.NoError(t, err)

	relinked := &domain.LinkedItem{ID: "item-2", UserID: "user-1", AccessToken: "token"}
	_, err = service.SyncTransactions(ctx, relinked)
	require.NoError(t, err)

	require.Len(t, analytics.events, 3)
	assert.Equal(t, phclient.FinancialDataSyncCompletedSyncKindInitial,
		analytics.events[0].(phclient.FinancialDataSyncCompletedPayload).SyncKind)
	assert.Equal(t, phclient.FinancialDataSyncCompletedSyncKindRefresh,
		analytics.events[1].(phclient.FinancialDataSyncCompletedPayload).SyncKind,
		"a blank final cursor and invalid-cursor recovery must not reclassify an already completed item as initial")
	assert.Equal(t, phclient.FinancialDataSyncCompletedSyncKindInitial,
		analytics.events[2].(phclient.FinancialDataSyncCompletedPayload).SyncKind,
		"a relink creates a new retained linked-item generation with its own first completion")
}

func TestSyncTransactions_NormalInitialCompletionThenRefreshUsesDurableLatch(t *testing.T) {
	ctx := context.Background()
	tierConfig, err := config.LoadTierConfig()
	require.NoError(t, err)
	plaidService := new(MockPlaidService)
	linkedItems := new(MockLinkedItemRepository)
	accounts := new(MockAccountRepository)
	profiles := new(mockProfileRepoForCursorTests)
	service := NewTransactionSyncService(nil, linkedItems, new(MockTransactionRepository), accounts, profiles, nil,
		plaidService, NewSubscriptionService(tierConfig, nil), nil, nil, nil, nil, nil)
	analytics := &recordingSyncProductAnalytics{}
	SetTransactionSyncProductAnalytics(service, analytics)
	SetTransactionSyncCompletionRecorder(service, &memorySyncCompletionRecorder{})

	profiles.On("GetUserByID", ctx, "user-1").
		Return(&domain.User{ID: "user-1", SubscriptionTier: string(domain.TierPremium)}, nil).Once()
	accounts.On("GetByLinkedItemIDIncludingRetained", ctx, "item-1").Return([]domain.Account{}, nil).Twice()
	initial := &plaid.TransactionsSyncResponse{}
	initial.SetNextCursor("cursor-1")
	initial.SetHasMore(false)
	refresh := &plaid.TransactionsSyncResponse{}
	refresh.SetNextCursor("cursor-2")
	refresh.SetHasMore(false)
	plaidService.On("TransactionsSync", ctx, "token", "", 730).Return(initial, nil).Once()
	plaidService.On("TransactionsSync", ctx, "token", "cursor-1", 0).Return(refresh, nil).Once()

	item := &domain.LinkedItem{ID: "item-1", UserID: "user-1", AccessToken: "token"}
	_, err = service.SyncTransactions(ctx, item)
	require.NoError(t, err)
	cursor := "cursor-1"
	item.Cursor = &cursor
	_, err = service.SyncTransactions(ctx, item)
	require.NoError(t, err)

	require.Len(t, analytics.events, 2)
	assert.Equal(t, phclient.FinancialDataSyncCompletedSyncKindInitial,
		analytics.events[0].(phclient.FinancialDataSyncCompletedPayload).SyncKind)
	assert.Equal(t, phclient.FinancialDataSyncCompletedSyncKindRefresh,
		analytics.events[1].(phclient.FinancialDataSyncCompletedPayload).SyncKind)
}

func TestSyncTransactions_CompletionFinalizationFailureEmitsNothing(t *testing.T) {
	ctx := context.Background()
	plaidService := new(MockPlaidService)
	linkedItems := new(MockLinkedItemRepository)
	accounts := new(MockAccountRepository)
	service := NewTransactionSyncService(nil, linkedItems, new(MockTransactionRepository), accounts, nil, nil,
		plaidService, nil, nil, nil, nil, nil, nil)
	analytics := &recordingSyncProductAnalytics{}
	SetTransactionSyncProductAnalytics(service, analytics)
	SetTransactionSyncCompletionRecorder(service, linkedItems)
	cursor := "cursor-before"
	item := &domain.LinkedItem{ID: "item-1", UserID: "user-1", AccessToken: "token", Cursor: &cursor}
	accounts.On("GetByLinkedItemIDIncludingRetained", ctx, "item-1").Return([]domain.Account{}, nil).Once()
	response := &plaid.TransactionsSyncResponse{}
	response.SetNextCursor("cursor-after")
	response.SetHasMore(false)
	plaidService.On("TransactionsSync", ctx, "token", cursor, 0).Return(response, nil).Once()
	after := "cursor-after"
	linkedItems.On("RecordTransactionSyncCompletion", ctx, "item-1", cursor, &after).
		Return(repository.TransactionSyncCompletionRefresh, errors.New("commit failed")).Once()

	_, err := service.SyncTransactions(ctx, item)
	require.ErrorContains(t, err, "finalize transaction sync completion")
	assert.Empty(t, analytics.events, "an uncommitted completion cannot be reported as delivered")
}

func TestSyncTransactions_AmbiguousRolloutCompletionEmitsNothing(t *testing.T) {
	ctx := context.Background()
	plaidService := new(MockPlaidService)
	linkedItems := new(MockLinkedItemRepository)
	accounts := new(MockAccountRepository)
	service := NewTransactionSyncService(nil, linkedItems, new(MockTransactionRepository), accounts, nil, nil,
		plaidService, nil, nil, nil, nil, nil, nil)
	analytics := &recordingSyncProductAnalytics{}
	SetTransactionSyncProductAnalytics(service, analytics)
	SetTransactionSyncCompletionRecorder(service, linkedItems)
	cursor := "cursor-before"
	item := &domain.LinkedItem{ID: "item-1", UserID: "user-1", AccessToken: "token", Cursor: &cursor}
	accounts.On("GetByLinkedItemIDIncludingRetained", ctx, "item-1").Return([]domain.Account{}, nil).Once()
	response := &plaid.TransactionsSyncResponse{}
	response.SetNextCursor("cursor-after")
	response.SetHasMore(false)
	plaidService.On("TransactionsSync", ctx, "token", cursor, 0).Return(response, nil).Once()
	after := "cursor-after"
	linkedItems.On("RecordTransactionSyncCompletion", ctx, "item-1", cursor, &after).
		Return(repository.TransactionSyncCompletionSuppressedRolloutAmbiguous, nil).Once()

	_, err := service.SyncTransactions(ctx, item)
	require.NoError(t, err)
	assert.Empty(t, analytics.events)
}

func TestSyncTransactions_UnknownCompletionDispositionEmitsNothing(t *testing.T) {
	ctx := context.Background()
	plaidService := new(MockPlaidService)
	linkedItems := new(MockLinkedItemRepository)
	accounts := new(MockAccountRepository)
	service := NewTransactionSyncService(nil, linkedItems, new(MockTransactionRepository), accounts, nil, nil,
		plaidService, nil, nil, nil, nil, nil, nil)
	analytics := &recordingSyncProductAnalytics{}
	SetTransactionSyncProductAnalytics(service, analytics)
	SetTransactionSyncCompletionRecorder(service, linkedItems)
	cursor := "cursor-before"
	item := &domain.LinkedItem{ID: "item-1", UserID: "user-1", AccessToken: "token", Cursor: &cursor}
	accounts.On("GetByLinkedItemIDIncludingRetained", ctx, "item-1").Return([]domain.Account{}, nil).Once()
	response := &plaid.TransactionsSyncResponse{}
	response.SetNextCursor("cursor-after")
	response.SetHasMore(false)
	plaidService.On("TransactionsSync", ctx, "token", cursor, 0).Return(response, nil).Once()
	after := "cursor-after"
	linkedItems.On("RecordTransactionSyncCompletion", ctx, "item-1", cursor, &after).
		Return(repository.TransactionSyncCompletionDisposition("unexpected"), nil).Once()

	_, err := service.SyncTransactions(ctx, item)
	require.NoError(t, err)
	assert.Empty(t, analytics.events)
}

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
// pins it, and it must keep passing — which is why the guard belongs at the cursor-selection site in
// the pagination loop and NOT inside UpdateCursor. (#1570 moved the WRITE to a single post-storage
// call; the guard stayed in the loop, where it decides what that write will carry.)
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
	analytics := &recordingSyncProductAnalytics{}
	SetTransactionSyncProductAnalytics(service, analytics)
	SetTransactionSyncCompletionRecorder(service, mockLinkedItemRepo)

	goodCursor := "cursor-earned-over-many-syncs"
	linkedItem := &domain.LinkedItem{
		ID:          "linked-item-123",
		UserID:      "user-123",
		ItemID:      "plaid-item-123",
		AccessToken: "access-token",
		Cursor:      &goodCursor,
	}

	mockAccountRepo.On("GetByLinkedItemIDIncludingRetained", ctx, "linked-item-123").
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
	mockLinkedItemRepo.On("RecordTransactionSyncCompletion", ctx, "linked-item-123", goodCursor, (*string)(nil)).
		Return(repository.TransactionSyncCompletionRefresh, nil).Once()

	result, err := service.SyncTransactions(ctx, linkedItem)
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, "user-123", analytics.userID)
	payload, ok := analytics.event.(phclient.FinancialDataSyncCompletedPayload)
	require.True(t, ok)
	assert.Equal(t, phclient.FinancialDataSyncCompletedSyncKindRefresh, payload.SyncKind)

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
	analytics := &recordingSyncProductAnalytics{}
	SetTransactionSyncProductAnalytics(service, analytics)

	goodCursor := "cursor-earned-over-many-syncs"
	linkedItem := &domain.LinkedItem{
		ID:          "linked-item-loop",
		UserID:      "user-loop",
		ItemID:      "plaid-item-loop",
		AccessToken: "access-token",
		Cursor:      &goodCursor,
	}

	mockAccountRepo.On("GetByLinkedItemIDIncludingRetained", ctx, "linked-item-loop").
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
	assert.Nil(t, analytics.event, "a failed sync must not emit a completion event")

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
// THE LOSS (as it stood in round 11 — #1570 has since removed the per-page write entirely; this
// paragraph describes the world the break-instead-of-error decision was made in).
// `UpdateCursor` ran INSIDE the pagination loop (per page), while storage happens AFTER it.
// So on a two-page sync where page 1 succeeds and page 2 returns has_more=true with an empty cursor:
//
//  1. page 1's transactions are appended to `result` and page 1's cursor is PERSISTED;
//  2. round 10's guard returned an error before any storage;
//  3. the worker reads `result` only on the success path (queue/jobs/plaid_sync.go) — on error it is
//     discarded entirely.
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
// TestSyncTransactions_BlankNextCursorWithHasMore_KeepsAlreadyFetchedPages exercises the
// pages_kept break branch END TO END.
//
// #1537 T97: the previous version of this test gave page 2 EMPTY Added/Modified/Removed and ran
// exactly one sync, so it proved neither of the two things it claimed — that page N's own rows
// survive the break (page 2 had none), nor that the next sync re-delivers and re-upserts them
// (there was no next sync). Page 2 now carries a real row, and a SECOND sync runs from the cursor
// the first one persisted.
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
	analytics := &recordingSyncProductAnalytics{}
	SetTransactionSyncProductAnalytics(service, analytics)
	SetTransactionSyncCompletionRecorder(service, mockLinkedItemRepo)

	startCursor := "cursor-page-0"
	linkedItem := &domain.LinkedItem{
		ID:          "linked-item-multipage",
		UserID:      "user-multipage",
		ItemID:      "plaid-item-multipage",
		AccessToken: "access-token",
		Cursor:      &startCursor,
	}

	mockAccountRepo.On("GetByLinkedItemIDIncludingRetained", ctx, "linked-item-multipage").
		Return([]domain.Account{{ID: "acc-1", PlaidAccountID: "plaid-acc-1", UserID: "user-multipage"}}, nil).Maybe()

	page1 := &plaid.TransactionsSyncResponse{
		Added: []plaid.Transaction{
			{TransactionId: "txn-page1", AccountId: "plaid-acc-1", Amount: 42.00, Date: "2024-01-15", Name: "Page one"},
		},
		Modified: []plaid.Transaction{}, Removed: []plaid.RemovedTransaction{},
	}
	page1.SetNextCursor("cursor-page-1")
	page1.SetHasMore(true)

	// T97: page 2 carries REAL rows — an added row, a modified row, and a removed row — so the
	// break branch is proven to keep page N's own payload, not just page 1's.
	page2 := &plaid.TransactionsSyncResponse{
		Added: []plaid.Transaction{
			{TransactionId: "txn-page2", AccountId: "plaid-acc-1", Amount: 7.00, Date: "2024-01-16", Name: "Page two"},
		},
		Modified: []plaid.Transaction{
			{TransactionId: "txn-page2-mod", AccountId: "plaid-acc-1", Amount: 9.00, Date: "2024-01-17", Name: "Page two mod"},
		},
		Removed: []plaid.RemovedTransaction{{TransactionId: stringPtr("txn-page2-rem")}},
	}
	page2.SetNextCursor("")
	page2.SetHasMore(true)

	mockPlaidService.On("TransactionsSync", ctx, "access-token", startCursor, 0).Return(page1, nil).Once()
	mockPlaidService.On("TransactionsSync", ctx, "access-token", "cursor-page-1", 0).Return(page2, nil).Once()
	mockLinkedItemRepo.On("RecordTransactionSyncProgress", ctx, "linked-item-multipage", startCursor, "cursor-page-1").Return(nil).Once()

	mockTransactionRepo.On("CreateBatch", mock.Anything, mock.Anything).
		Return(map[string]bool{"txn-page1": true, "txn-page2": true}, nil).Maybe()
	mockTransactionRepo.On("GetByExternalID", mock.Anything, domain.SourcePlaid, "txn-page2-mod", "user-multipage").
		Return(&domain.Transaction{ID: "existing-mod", ExternalTransactionID: stringPtr("txn-page2-mod")}, nil).Maybe()
	mockTransactionRepo.On("Update", mock.Anything, mock.Anything).Return(nil).Maybe()
	mockTransactionRepo.On("TombstoneByExternalID", mock.Anything, domain.SourcePlaid, "txn-page2-rem", "user-multipage", domain.TxnDeletedByPlaidSync).
		Return(domain.TxnDeletedProviderRemovedPosted, nil).Maybe()

	result, err := service.SyncTransactions(ctx, linkedItem)

	require.NoError(t, err,
		"page 1 was fetched and its cursor persisted, so returning an error makes the worker discard "+
			"page 1's transactions while the cursor has already moved past them — Plaid will not "+
			"re-deliver them. Having made progress, stop paginating and hand back what we have.")
	require.NotNil(t, result)
	require.Len(t, result.Added, 2,
		"BOTH pages' added rows must survive the break — page N's rows are appended before the guard")
	assert.Equal(t, []string{"txn-page1", "txn-page2"},
		[]string{result.Added[0].GetTransactionId(), result.Added[1].GetTransactionId()})
	require.Len(t, result.Modified, 1, "page 2's modified rows must survive the break too")
	require.Len(t, result.Removed, 1, "page 2's removed rows must survive the break too")

	var persisted []string
	for _, c := range mockLinkedItemRepo.Calls {
		if c.Method == "RecordTransactionSyncProgress" {
			persisted = append(persisted, c.Arguments.Get(3).(string))
		}
	}
	assert.Equal(t, []string{"cursor-page-1"}, persisted,
		"exactly page 1's cursor should be persisted — never the blank")
	assert.Nil(t, analytics.event, "partial pagination is durable progress, not a completed sync")
	mockLinkedItemRepo.AssertNotCalled(t, "RecordTransactionSyncCompletion", mock.Anything, mock.Anything, mock.Anything, mock.Anything)

	storedInSync1 := storedExternalIDs(mockTransactionRepo)
	assert.Equal(t, 1, storedInSync1["txn-page1"],
		"page 1's transaction must reach CreateBatch. Returning an error before the storage step is "+
			"what lost it, while its cursor had already been persisted.")
	assert.Equal(t, 1, storedInSync1["txn-page2"],
		"page N's OWN transaction must reach CreateBatch as well — it was appended to result before "+
			"the break, so dropping it here would be silent loss of the last page")
	// The page-2 modify/delete are what the break branch actually commits beyond page 1.
	mockTransactionRepo.AssertCalled(t, "Update", mock.Anything, mock.Anything)
	mockTransactionRepo.AssertCalled(t, "TombstoneByExternalID", mock.Anything, domain.SourcePlaid, "txn-page2-rem", "user-multipage", domain.TxnDeletedByPlaidSync)

	// ------------------------------------------------------------------
	// T97 second sync: the cursor lags one page, so Plaid re-delivers page 2. This is the
	// redelivery the break branch's comment depends on; it was previously never exercised.
	// ------------------------------------------------------------------
	// Derive the resume point from what production ACTUALLY persisted, not from a literal: an
	// over-advanced cursor (the data-loss shape) must make this second sync visibly wrong rather
	// than silently agree with a hardcoded expectation.
	require.NotEmpty(t, persisted)
	resumedCursor := persisted[len(persisted)-1]
	linkedItem.Cursor = &resumedCursor

	page2Redelivered := &plaid.TransactionsSyncResponse{
		Added: []plaid.Transaction{
			{TransactionId: "txn-page2", AccountId: "plaid-acc-1", Amount: 7.00, Date: "2024-01-16", Name: "Page two"},
		},
		Modified: []plaid.Transaction{
			{TransactionId: "txn-page2-mod", AccountId: "plaid-acc-1", Amount: 9.00, Date: "2024-01-17", Name: "Page two mod"},
		},
		Removed: []plaid.RemovedTransaction{{TransactionId: stringPtr("txn-page2-rem")}},
	}
	page2Redelivered.SetNextCursor("cursor-page-2")
	page2Redelivered.SetHasMore(false)
	mockPlaidService.On("TransactionsSync", ctx, "access-token", resumedCursor, 0).
		Return(page2Redelivered, nil).Once()
	completedCursor := "cursor-page-2"
	mockLinkedItemRepo.On("RecordTransactionSyncCompletion", ctx, "linked-item-multipage", resumedCursor, &completedCursor).
		Return(repository.TransactionSyncCompletionInitial, nil).Once()

	result2, err2 := service.SyncTransactions(ctx, linkedItem)
	require.NoError(t, err2, "the follow-up sync must complete now that Plaid sends a real next_cursor")
	require.Len(t, result2.Added, 1)
	assert.Equal(t, "txn-page2", result2.Added[0].GetTransactionId(),
		"the second sync must pick up exactly the page the lagging cursor pointed at — no loss")

	storedTotal := storedExternalIDs(mockTransactionRepo)
	assert.Equal(t, 2, storedTotal["txn-page2"],
		"page 2 reached CreateBatch TWICE across the two syncs. That is the redelivery the break "+
			"branch relies on being safe: CreateBatch is ON CONFLICT DO UPDATE, so the second write "+
			"upserts rather than duplicating. Only TRANSACTION rows have that property — see the "+
			"pages_kept comment for the non-idempotent tail (balance snapshots, sync metrics, boosts).")
	assert.Equal(t, 1, storedTotal["txn-page1"],
		"page 1 must NOT be re-fetched: the persisted cursor is already past it")

	var persistedAfter []string
	for _, c := range mockLinkedItemRepo.Calls {
		if c.Method == "RecordTransactionSyncProgress" {
			persistedAfter = append(persistedAfter, c.Arguments.Get(3).(string))
		}
	}
	assert.Equal(t, []string{"cursor-page-1"}, persistedAfter,
		"partial progress uses UpdateCursor; full completion is finalized atomically by the completion recorder")
	payload, ok := analytics.event.(phclient.FinancialDataSyncCompletedPayload)
	require.True(t, ok)
	assert.Equal(t, phclient.FinancialDataSyncCompletedSyncKindInitial, payload.SyncKind,
		"the partial cursor must not turn the connection's first full catch-up into refresh")

	mockPlaidService.AssertExpectations(t)
}

// storedExternalIDs counts, per external transaction id, how many times a row with that id was
// handed to transactionRepo.CreateBatch across every call recorded on the mock.
func storedExternalIDs(repo *MockTransactionRepository) map[string]int {
	counts := make(map[string]int)
	for _, c := range repo.Calls {
		if c.Method != "CreateBatch" {
			continue
		}
		txns, ok := c.Arguments.Get(1).([]domain.Transaction)
		if !ok {
			continue
		}
		for _, txn := range txns {
			if txn.ExternalTransactionID != nil {
				counts[*txn.ExternalTransactionID]++
			}
		}
	}
	return counts
}
