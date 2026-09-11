package services

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/plaid/plaid-go/v23/plaid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// ============================================================================
// #1631 (M4) — exclude_investment_sync is an INGESTION filter, not only a read filter
//
// These tests pin the contract change #1631 makes deliberately. Before it,
// user_accounts.exclude_investment_sync hid rows at read time and the sync kept storing them; the
// user's reason for setting the flag (Plaid reports one brokerage cash account through BOTH
// /transactions/sync and /investments/transactions/get) is a reason not to STORE the duplicate at
// all. See docs/decisions/2026-08-24-financial-history-retained-by-default.md § "Non-ingestion is
// not destruction" for why this does not violate the retention ADR, and for the availability cost
// it does carry.
// ============================================================================

// excludedSiblingFixture is the shape every test below needs: one linked item carrying TWO
// brokerage accounts, one excluded and one not. The whole point of the milestone is that these two
// are treated differently while sharing an item.
type excludedSiblingFixture struct {
	userID           string
	linkedItem       *domain.LinkedItem
	excludedPlaidID  string
	includedPlaidID  string
	excludedInternal string
	includedInternal string
	excludedChainID  string
	includedChainID  string
}

func newExcludedSiblingFixture() excludedSiblingFixture {
	userID := uuid.New().String()
	return excludedSiblingFixture{
		userID:           userID,
		linkedItem:       makeLinkedItem("item-excl-"+uuid.New().String(), userID),
		excludedPlaidID:  "plaid-acct-excluded",
		includedPlaidID:  "plaid-acct-included",
		excludedInternal: uuid.New().String(),
		includedInternal: uuid.New().String(),
		excludedChainID:  uuid.New().String(),
		includedChainID:  uuid.New().String(),
	}
}

func (f excludedSiblingFixture) accounts() []domain.Account {
	return []domain.Account{
		{
			ID:                    f.excludedInternal,
			PlaidAccountID:        f.excludedPlaidID,
			UserID:                f.userID,
			UserAccountID:         &f.excludedChainID,
			ExcludeInvestmentSync: true,
		},
		{
			ID:                    f.includedInternal,
			PlaidAccountID:        f.includedPlaidID,
			UserID:                f.userID,
			UserAccountID:         &f.includedChainID,
			ExcludeInvestmentSync: false,
		},
	}
}

// newSyncServiceWithUserAccountRepo rebuilds the service around a FRESH user-account repository
// mock.
//
// ⚠ Necessary, not stylistic. setupInvestmentSyncService registers a permissive
// PendingInvestmentBackfills -> {} default so the ~15 pre-existing tests need no edit, and testify
// matches expectations in REGISTRATION order — so a specific expectation added afterwards is never
// reached, and a backfill test would quietly assert nothing.
func newSyncServiceWithUserAccountRepo(deps investmentSyncDeps, uaRepo *MockUserAccountRepository) InvestmentSyncService {
	return NewInvestmentSyncService(
		deps.webhookRepo,
		deps.linkedItemRepo,
		deps.accountRepo,
		deps.securityRepo,
		deps.holdingRepo,
		deps.invTxnRepo,
		deps.snapshotRepo,
		uaRepo,
		deps.plaidSvc,
		nil,
	)
}

// TestInvestmentSyncService_SyncInvestmentTransactions_ExcludedAccountIsNotPersisted is the
// milestone's central assertion: rows Plaid delivers for an excluded chain never reach the
// repository, while its non-excluded SIBLING on the same item still does. Storing "everything and
// filter later" is what produced the double-counted brokerage cash movement in the first place.
func TestInvestmentSyncService_SyncInvestmentTransactions_ExcludedAccountIsNotPersisted(t *testing.T) {
	svc, deps := setupInvestmentSyncService(t)
	ctx := context.Background()
	f := newExcludedSiblingFixture()

	deps.accountRepo.On("GetByLinkedItemIDIncludingRetained", ctx, f.linkedItem.ID).
		Return(f.accounts(), nil)

	txnResp := plaid.NewInvestmentsTransactionsGetResponse(
		makeMinimalPlaidItem(), nil, nil,
		[]plaid.InvestmentTransaction{
			makePlaidCashTxn("txn-excluded", f.excludedPlaidID, 100.0, "2024-01-01"),
			makePlaidCashTxn("txn-included", f.includedPlaidID, 200.0, "2024-01-02"),
		},
		2, "req-excl",
	)
	deps.plaidSvc.On("InvestmentsTransactionsGet", ctx, "tok", mock.Anything, mock.Anything, int32(0), int32(500)).
		Return(txnResp, nil)

	// Exactly ONE row is upserted, and it is the sibling's. A matcher rather than a count, because
	// "two rows were written" and "the wrong one was written" are different defects.
	deps.invTxnRepo.On("UpsertBatch", ctx, mock.MatchedBy(func(txns []domain.InvestmentTransaction) bool {
		return len(txns) == 1 && txns[0].AccountID == f.includedInternal
	})).Return(1, nil).Times(1)

	// The item watermark still advances: a deliberate exclusion is not a loss, and the siblings'
	// history would otherwise be re-fetched forever.
	deps.linkedItemRepo.On("UpdateInvestmentsLastSyncedDate", ctx, f.linkedItem.ID, mock.Anything).
		Return(nil).Times(1)

	result, err := svc.SyncInvestmentTransactions(ctx, f.linkedItem)

	require.NoError(t, err)
	assert.Equal(t, 1, result.TransactionsInserted)
	assert.Equal(t, 1, result.TransactionsExcluded,
		"the excluded row must be counted as EXCLUDED, not folded into TransactionsSkipped "+
			"(which means 'already present') and not into a drop counter (which means 'we lost it')")
	assert.Zero(t, result.TransactionsDroppedUnmappedAccount,
		"an excluded account is not an unmapped account; conflating them makes a deliberate "+
			"exclusion indistinguishable in production logs from having lost an account")
	deps.invTxnRepo.AssertExpectations(t)
	deps.linkedItemRepo.AssertExpectations(t)
}

// TestInvestmentSyncService_SyncInvestmentTransactions_AllAccountsExcluded_StillAdvancesWatermark
// covers the degenerate item where the only brokerage account is excluded. Nothing is stored and
// nothing is lost, so the watermark must still move — otherwise the item re-fetches two years of
// data on every webhook, forever.
func TestInvestmentSyncService_SyncInvestmentTransactions_AllAccountsExcluded_StillAdvancesWatermark(t *testing.T) {
	svc, deps := setupInvestmentSyncService(t)
	ctx := context.Background()
	f := newExcludedSiblingFixture()

	deps.accountRepo.On("GetByLinkedItemIDIncludingRetained", ctx, f.linkedItem.ID).Return([]domain.Account{
		{
			ID: f.excludedInternal, PlaidAccountID: f.excludedPlaidID, UserID: f.userID,
			UserAccountID: &f.excludedChainID, ExcludeInvestmentSync: true,
		},
	}, nil)

	txnResp := plaid.NewInvestmentsTransactionsGetResponse(
		makeMinimalPlaidItem(), nil, nil,
		[]plaid.InvestmentTransaction{makePlaidCashTxn("txn-only", f.excludedPlaidID, 100.0, "2024-01-01")},
		1, "req-all-excl",
	)
	deps.plaidSvc.On("InvestmentsTransactionsGet", ctx, "tok", mock.Anything, mock.Anything, int32(0), int32(500)).
		Return(txnResp, nil)
	deps.linkedItemRepo.On("UpdateInvestmentsLastSyncedDate", ctx, f.linkedItem.ID, mock.Anything).
		Return(nil).Times(1)

	result, err := svc.SyncInvestmentTransactions(ctx, f.linkedItem)

	require.NoError(t, err)
	assert.Equal(t, 0, result.TransactionsInserted)
	assert.Equal(t, 1, result.TransactionsExcluded)
	deps.invTxnRepo.AssertNotCalled(t, "UpsertBatch", mock.Anything, mock.Anything)
	deps.linkedItemRepo.AssertExpectations(t)
}

// TestInvestmentSyncService_SyncInvestmentTransactions_BackfillMarkerWidensWindowAndClears pins the
// consumption half of the durable obligation: a chain carrying
// user_accounts.investment_backfill_requested_at makes the sync ignore the incremental watermark
// and re-fetch the full lookback, and the marker is cleared only after that run stored everything.
func TestInvestmentSyncService_SyncInvestmentTransactions_BackfillMarkerWidensWindowAndClears(t *testing.T) {
	_, deps := setupInvestmentSyncService(t)
	uaRepo := new(MockUserAccountRepository)
	svc := newSyncServiceWithUserAccountRepo(deps, uaRepo)
	ctx := context.Background()
	f := newExcludedSiblingFixture()

	// An item that has synced recently: without the marker the start date would be yesterday.
	lastSynced := time.Now().UTC().AddDate(0, 0, -3)
	f.linkedItem.InvestmentsLastSyncedDate = &lastSynced

	deps.accountRepo.On("GetByLinkedItemIDIncludingRetained", ctx, f.linkedItem.ID).Return([]domain.Account{
		{
			ID: f.includedInternal, PlaidAccountID: f.includedPlaidID, UserID: f.userID,
			UserAccountID: &f.includedChainID, ExcludeInvestmentSync: false,
		},
	}, nil)

	requestedAt := time.Now().UTC().Add(-time.Hour)
	uaRepo.On("PendingInvestmentBackfills", ctx, []string{f.includedChainID}, f.userID).
		Return(map[string]time.Time{f.includedChainID: requestedAt}, nil)

	fullLookbackStart := time.Now().UTC().AddDate(-investmentTxnLookbackYears, 0, 0).Format("2006-01-02")
	txnResp := plaid.NewInvestmentsTransactionsGetResponse(
		makeMinimalPlaidItem(), nil, nil,
		[]plaid.InvestmentTransaction{makePlaidCashTxn("txn-backfilled", f.includedPlaidID, 42.0, "2023-06-01")},
		1, "req-backfill",
	)
	// The start date is the assertion: the marker must beat the incremental watermark.
	deps.plaidSvc.On("InvestmentsTransactionsGet", ctx, "tok", fullLookbackStart, mock.Anything, int32(0), int32(500)).
		Return(txnResp, nil).Times(1)
	deps.invTxnRepo.On("UpsertBatch", ctx, mock.Anything).Return(1, nil)
	deps.linkedItemRepo.On("UpdateInvestmentsLastSyncedDate", ctx, f.linkedItem.ID, mock.Anything).Return(nil)

	// Cleared with the timestamp the sync READ, not "now": a marker set again while this run was
	// in flight covers a window this run did not fetch and must survive.
	uaRepo.On("ClearInvestmentBackfillRequests", ctx, []string{f.includedChainID}, f.userID, requestedAt).
		Return(nil).Times(1)

	result, err := svc.SyncInvestmentTransactions(ctx, f.linkedItem)

	require.NoError(t, err)
	assert.Equal(t, 1, result.TransactionsInserted)
	assert.Equal(t, 1, result.BackfilledChains)
	deps.plaidSvc.AssertExpectations(t)
	uaRepo.AssertExpectations(t)
}

// TestInvestmentSyncService_SyncInvestmentTransactions_BackfillMarkerSurvivesALossyRun is the
// recoverability guarantee. If the run dropped anything, the backfill did not actually complete, so
// the obligation must stay on the row for the next run to pick up. This is also the property that
// makes the obligation survive a crash or a failed enqueue: nothing but a clean run clears it.
func TestInvestmentSyncService_SyncInvestmentTransactions_BackfillMarkerSurvivesALossyRun(t *testing.T) {
	_, deps := setupInvestmentSyncService(t)
	uaRepo := new(MockUserAccountRepository)
	svc := newSyncServiceWithUserAccountRepo(deps, uaRepo)
	ctx := context.Background()
	f := newExcludedSiblingFixture()

	deps.accountRepo.On("GetByLinkedItemIDIncludingRetained", ctx, f.linkedItem.ID).Return([]domain.Account{
		{
			ID: f.includedInternal, PlaidAccountID: f.includedPlaidID, UserID: f.userID,
			UserAccountID: &f.includedChainID, ExcludeInvestmentSync: false,
		},
	}, nil)
	uaRepo.On("PendingInvestmentBackfills", ctx, []string{f.includedChainID}, f.userID).
		Return(map[string]time.Time{f.includedChainID: time.Now().UTC()}, nil)

	// One good row and one with an unparsable date → the run is lossy.
	bad := makePlaidCashTxn("txn-bad-date", f.includedPlaidID, 1.0, "not-a-date")
	txnResp := plaid.NewInvestmentsTransactionsGetResponse(
		makeMinimalPlaidItem(), nil, nil,
		[]plaid.InvestmentTransaction{
			makePlaidCashTxn("txn-good", f.includedPlaidID, 2.0, "2024-01-01"),
			bad,
		},
		2, "req-lossy",
	)
	deps.plaidSvc.On("InvestmentsTransactionsGet", ctx, "tok", mock.Anything, mock.Anything, int32(0), int32(500)).
		Return(txnResp, nil)
	deps.invTxnRepo.On("UpsertBatch", ctx, mock.Anything).Return(1, nil)

	result, err := svc.SyncInvestmentTransactions(ctx, f.linkedItem)

	require.NoError(t, err)
	assert.Equal(t, 1, result.TransactionsDroppedUnparsableDate)
	uaRepo.AssertNotCalled(t, "ClearInvestmentBackfillRequests",
		mock.Anything, mock.Anything, mock.Anything, mock.Anything)
	deps.linkedItemRepo.AssertNotCalled(t, "UpdateInvestmentsLastSyncedDate",
		mock.Anything, mock.Anything, mock.Anything)
}

// ============================================================================
// #1631 (M7, merged here by controller ruling) — the sync must not drop rows silently and then
// advance the cursor past them. Each reason is counted; any drop pins the watermark.
// ============================================================================

// TestInvestmentSyncService_SyncInvestmentTransactions_UnresolvedSecurityIsCountedNotSwallowed
// covers the reason that was previously entirely silent: a securityMap miss left SecurityID nil,
// which is the schema's encoding of "cash-only transaction" — so the row was stored WRONG and
// indistinguishable from a legitimate one. SyncHoldings refuses to write a wrong row for the same
// class of failure; the ledger path now does too.
func TestInvestmentSyncService_SyncInvestmentTransactions_UnresolvedSecurityIsCountedNotSwallowed(t *testing.T) {
	svc, deps := setupInvestmentSyncService(t)
	ctx := context.Background()
	f := newExcludedSiblingFixture()

	deps.accountRepo.On("GetByLinkedItemIDIncludingRetained", ctx, f.linkedItem.ID).Return([]domain.Account{
		{
			ID: f.includedInternal, PlaidAccountID: f.includedPlaidID, UserID: f.userID,
			UserAccountID: &f.includedChainID, ExcludeInvestmentSync: false,
		},
	}, nil)

	// A BUY referencing a security Plaid did not include in the response → not in securityMap.
	orphan := makePlaidInvTxn("txn-orphan-sec", f.includedPlaidID, "plaid-sec-missing", 900.0, "2024-03-01")
	txnResp := plaid.NewInvestmentsTransactionsGetResponse(
		makeMinimalPlaidItem(), nil, []plaid.Security{},
		[]plaid.InvestmentTransaction{orphan}, 1, "req-orphan-sec",
	)
	deps.plaidSvc.On("InvestmentsTransactionsGet", ctx, "tok", mock.Anything, mock.Anything, int32(0), int32(500)).
		Return(txnResp, nil)

	result, err := svc.SyncInvestmentTransactions(ctx, f.linkedItem)

	require.NoError(t, err)
	assert.Equal(t, 1, result.TransactionsDroppedUnresolvedSecurity)
	assert.Equal(t, 0, result.TransactionsInserted)
	deps.invTxnRepo.AssertNotCalled(t, "UpsertBatch", mock.Anything, mock.Anything)
	deps.linkedItemRepo.AssertNotCalled(t, "UpdateInvestmentsLastSyncedDate",
		mock.Anything, mock.Anything, mock.Anything)
}

// TestInvestmentSyncService_SyncInvestmentTransactions_UnmappedAccountIsCountedAndPinsWatermark
// covers the reason that already warned but was never counted, and whose loss was made permanent
// by the watermark advancing past it.
func TestInvestmentSyncService_SyncInvestmentTransactions_UnmappedAccountIsCountedAndPinsWatermark(t *testing.T) {
	svc, deps := setupInvestmentSyncService(t)
	ctx := context.Background()
	f := newExcludedSiblingFixture()

	deps.accountRepo.On("GetByLinkedItemIDIncludingRetained", ctx, f.linkedItem.ID).Return([]domain.Account{
		{
			ID: f.includedInternal, PlaidAccountID: f.includedPlaidID, UserID: f.userID,
			UserAccountID: &f.includedChainID, ExcludeInvestmentSync: false,
		},
	}, nil)

	txnResp := plaid.NewInvestmentsTransactionsGetResponse(
		makeMinimalPlaidItem(), nil, nil,
		[]plaid.InvestmentTransaction{
			makePlaidCashTxn("txn-stranger", "plaid-acct-we-never-stored", 5.0, "2024-01-01"),
		},
		1, "req-unmapped",
	)
	deps.plaidSvc.On("InvestmentsTransactionsGet", ctx, "tok", mock.Anything, mock.Anything, int32(0), int32(500)).
		Return(txnResp, nil)

	result, err := svc.SyncInvestmentTransactions(ctx, f.linkedItem)

	require.NoError(t, err)
	assert.Equal(t, 1, result.TransactionsDroppedUnmappedAccount)
	deps.linkedItemRepo.AssertNotCalled(t, "UpdateInvestmentsLastSyncedDate",
		mock.Anything, mock.Anything, mock.Anything)
}

// TestInvestmentSyncService_SyncInvestmentTransactions_BackfillIsIdempotentAcrossRuns is the
// "re-running the backfill produces no duplicate rows" requirement, asserted where the decision
// actually lives: the second run no longer sees the obligation, so it asks Plaid for the ordinary
// incremental window instead of two more years, and the rows it does re-deliver come back from the
// upsert as ALREADY PRESENT rather than inserted again.
//
// The row-level half of idempotency — a second UpsertBatch with the same
// plaid_investment_transaction_id updating rather than duplicating — is already pinned against real
// Postgres by TestInvestmentTransactionRepository_UpsertBatch_ConflictSemantics_Integration, so
// it is deliberately not re-asserted through mocks here.
func TestInvestmentSyncService_SyncInvestmentTransactions_BackfillIsIdempotentAcrossRuns(t *testing.T) {
	_, deps := setupInvestmentSyncService(t)
	uaRepo := new(MockUserAccountRepository)
	svc := newSyncServiceWithUserAccountRepo(deps, uaRepo)
	ctx := context.Background()
	f := newExcludedSiblingFixture()

	lastSynced := time.Now().UTC().AddDate(0, 0, -3)
	f.linkedItem.InvestmentsLastSyncedDate = &lastSynced

	deps.accountRepo.On("GetByLinkedItemIDIncludingRetained", ctx, f.linkedItem.ID).Return([]domain.Account{
		{
			ID: f.includedInternal, PlaidAccountID: f.includedPlaidID, UserID: f.userID,
			UserAccountID: &f.includedChainID, ExcludeInvestmentSync: false,
		},
	}, nil)

	requestedAt := time.Now().UTC().Add(-time.Hour)
	txnResp := plaid.NewInvestmentsTransactionsGetResponse(
		makeMinimalPlaidItem(), nil, nil,
		[]plaid.InvestmentTransaction{makePlaidCashTxn("txn-idem", f.includedPlaidID, 7.0, "2023-06-01")},
		1, "req-idem",
	)
	deps.plaidSvc.On("InvestmentsTransactionsGet", ctx, "tok", mock.Anything, mock.Anything, int32(0), int32(500)).
		Return(txnResp, nil)
	deps.linkedItemRepo.On("UpdateInvestmentsLastSyncedDate", ctx, f.linkedItem.ID, mock.Anything).Return(nil)

	// Run 1 owes a backfill and discharges it; run 2 finds nothing owed, because the discharge is
	// what makes re-running safe.
	uaRepo.On("PendingInvestmentBackfills", ctx, []string{f.includedChainID}, f.userID).
		Return(map[string]time.Time{f.includedChainID: requestedAt}, nil).Once()
	uaRepo.On("ClearInvestmentBackfillRequests", ctx, []string{f.includedChainID}, f.userID, requestedAt).
		Return(nil).Once()
	deps.invTxnRepo.On("UpsertBatch", ctx, mock.Anything).Return(1, nil).Once()

	first, err := svc.SyncInvestmentTransactions(ctx, f.linkedItem)
	require.NoError(t, err)
	require.Equal(t, 1, first.BackfilledChains)
	require.Equal(t, 1, first.TransactionsInserted)

	uaRepo.On("PendingInvestmentBackfills", ctx, []string{f.includedChainID}, f.userID).
		Return(map[string]time.Time{}, nil).Once()
	// The same row again: the upsert reports zero inserted, and the sync reports it as skipped
	// ("already present"), never as a second insert and never as a drop.
	deps.invTxnRepo.On("UpsertBatch", ctx, mock.Anything).Return(0, nil).Once()

	second, err := svc.SyncInvestmentTransactions(ctx, f.linkedItem)
	require.NoError(t, err)
	assert.Equal(t, 0, second.BackfilledChains,
		"a discharged obligation must not be honoured a second time, or every subsequent sync "+
			"re-fetches two years of history forever")
	assert.Equal(t, 0, second.TransactionsInserted)
	assert.Equal(t, 1, second.TransactionsSkipped)
	uaRepo.AssertExpectations(t)
}
