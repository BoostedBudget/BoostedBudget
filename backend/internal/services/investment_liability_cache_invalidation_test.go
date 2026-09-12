package services

// Issue #1531 Task 10b-2a-ii — webhook fan-out gap.
//
// #1537 CORRECTION: "GetNetWorthTrend ... folds in investment holdings, investment transactions,
// and liability data" is FALSE. GetNetWorthTrend reads balance_snapshots ONLY (analytics_service.go
// -> balanceSnapshotRepo.GetAggregatedHistory[ForHousehold]); holdings, investment_transactions and
// portfolio_snapshots are read by no cache.GetOrLoad-backed path at all. So these tests do not
// guard a staleness bug in GetNetWorthTrend — they pin that the sync paths bump, which is kept as
// cheap insurance (see SetCacheInvalidation in investment_sync_service.go). Do not cite this bump
// as the reason an investment write is invalidated.
//
// What follows is the original rationale, retained for history. A Plaid HOLDINGS/INVESTMENTS_TRANSACTIONS/LIABILITIES webhook — routed
// through InvestmentSyncService.ProcessWebhook / LiabilitySyncService.ProcessWebhook, both of
// which call the SAME SyncHoldings/SyncInvestmentTransactions/SyncLiabilities methods their own
// periodic queue jobs call — used to commit new data to Postgres while never bumping the cached
// net-worth aggregate. These tests drive the real service (real mocked repos/Plaid, a real
// cache.Generations backed by fake Redis) and assert the user's generation genuinely changed,
// the same falsifiable standard as 2a-i's transaction-sync tests.

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/plaid/plaid-go/v23/plaid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/maxnussbaum/BoostedBudget/internal/cache"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/redisclient"
)

// ============================================================================
// InvestmentSyncService — SyncHoldings
// ============================================================================

func TestInvestmentSyncService_SyncHoldings_BumpsUserCacheGenerationAfterSuccess(t *testing.T) {
	svc, deps := setupInvestmentSyncService(t)
	ctx := context.Background()

	plaidAccountID := "plaid-acct-cache"
	internalAccountID := uuid.New().String()
	plaidSecurityID := "plaid-sec-cache"
	internalSecurityID := uuid.New().String()
	userID := uuid.New().String()
	linkedItem := makeLinkedItem("item-cache", userID)

	sec := makePlaidSecurity(plaidSecurityID, "Apple Inc.", "AAPL")
	holding := makePlaidHolding(plaidAccountID, plaidSecurityID, 10.0, 165.0, 1650.0, 1500.0)
	holdingsResp := plaid.NewInvestmentsHoldingsGetResponse(
		[]plaid.AccountBase{}, []plaid.Holding{holding}, []plaid.Security{sec},
		makeMinimalPlaidItem(), "req-cache",
	)
	deps.plaidSvc.On("InvestmentsHoldingsGet", ctx, linkedItem.AccessToken).Return(holdingsResp, nil)
	deps.securityRepo.On("UpsertBatch", ctx, mock.Anything).Return(map[string]string{plaidSecurityID: internalSecurityID}, nil)
	deps.accountRepo.On("GetByLinkedItemID", ctx, linkedItem.ID).Return([]domain.Account{
		{ID: internalAccountID, PlaidAccountID: plaidAccountID, UserID: userID, Type: "investment"},
	}, nil)
	deps.holdingRepo.On("ReplaceForAccount", ctx, internalAccountID, userID, mock.Anything).Return(nil)

	fake := redisclient.NewFake("cache")
	c := cache.NewCache(cache.Options{Client: fake})
	svc.SetCacheInvalidation(c.Generations())

	preGen, err := c.Generations().Get(ctx, cache.ScopeUser, userID)
	require.NoError(t, err)

	_, err = svc.SyncHoldings(ctx, linkedItem)
	require.NoError(t, err)

	postGen, err := c.Generations().Get(ctx, cache.ScopeUser, userID)
	require.NoError(t, err)
	assert.NotEqual(t, preGen, postGen,
		"a successful holdings sync must bump the user's cache generation so GetNetWorthTrend "+
			"cannot keep serving the pre-sync holdings value")
}

func TestInvestmentSyncService_SyncHoldings_NonInvestmentSkipDoesNotBumpUserCacheGeneration(t *testing.T) {
	svc, deps := setupInvestmentSyncService(t)
	ctx := context.Background()
	userID := uuid.New().String()
	linkedItem := makeLinkedItem("item-cache-type-skip", userID)
	plaidAccountID := "plaid-depository-cache-skip"
	plaidSecurityID := "plaid-security-cache-skip"

	response := plaid.NewInvestmentsHoldingsGetResponse(
		[]plaid.AccountBase{{AccountId: plaidAccountID}},
		[]plaid.Holding{makePlaidHolding(plaidAccountID, plaidSecurityID, 1, 100, 100, 100)},
		[]plaid.Security{makePlaidSecurity(plaidSecurityID, "Cash position", "CASH")},
		makeMinimalPlaidItem(),
		"req-cache-type-skip",
	)
	deps.plaidSvc.On("InvestmentsHoldingsGet", ctx, linkedItem.AccessToken).Return(response, nil)
	deps.securityRepo.On("UpsertBatch", ctx, mock.Anything).Return(map[string]string{plaidSecurityID: uuid.New().String()}, nil)
	deps.accountRepo.On("GetByLinkedItemID", ctx, linkedItem.ID).Return([]domain.Account{{
		ID: uuid.New().String(), PlaidAccountID: plaidAccountID, UserID: userID, Type: "depository",
	}}, nil)

	fake := redisclient.NewFake("cache")
	c := cache.NewCache(cache.Options{Client: fake})
	svc.SetCacheInvalidation(c.Generations())
	preGen, err := c.Generations().Get(ctx, cache.ScopeUser, userID)
	require.NoError(t, err)

	result, err := svc.SyncHoldings(ctx, linkedItem)
	require.NoError(t, err)
	assert.Equal(t, 1, result.AccountsSkippedNonInvestment)
	deps.holdingRepo.AssertNotCalled(t, "ReplaceForAccount", mock.Anything, mock.Anything, mock.Anything, mock.Anything)

	postGen, err := c.Generations().Get(ctx, cache.ScopeUser, userID)
	require.NoError(t, err)
	assert.Equal(t, preGen, postGen, "ignoring a provider row commits no holdings state and needs no cache bump")
}

func TestInvestmentSyncService_SyncHoldings_MixedAccountTypesPreserveSuccessfulSideEffects(t *testing.T) {
	_, deps := setupInvestmentSyncService(t)
	ctx := context.Background()
	userID := uuid.New().String()
	linkedItem := makeLinkedItem("item-cache-mixed-types", userID)
	investmentPlaidAccountID := "plaid-investment-cache-mixed"
	depositoryPlaidAccountID := "plaid-depository-cache-mixed"
	investmentAccountID := uuid.New().String()
	depositoryAccountID := uuid.New().String()
	investmentSecurityID := "plaid-security-investment-cache-mixed"
	depositorySecurityID := "plaid-security-depository-cache-mixed"
	internalInvestmentSecurityID := uuid.New().String()
	internalDepositorySecurityID := uuid.New().String()
	capturedDay := time.Date(2026, time.September, 11, 0, 0, 0, 0, time.UTC)

	investmentHolding := makePlaidHolding(
		investmentPlaidAccountID, investmentSecurityID, 2, 125, 250, 200,
	)
	depositoryHolding := makePlaidHolding(
		depositoryPlaidAccountID, depositorySecurityID, 1, 50, 50, 50,
	)
	response := plaid.NewInvestmentsHoldingsGetResponse(
		[]plaid.AccountBase{
			{AccountId: investmentPlaidAccountID},
			{AccountId: depositoryPlaidAccountID},
		},
		[]plaid.Holding{investmentHolding, depositoryHolding},
		[]plaid.Security{
			makePlaidSecurity(investmentSecurityID, "Investment security", "INV"),
			makePlaidSecurity(depositorySecurityID, "Depository security", "DEP"),
		},
		makeMinimalPlaidItem(),
		"req-cache-mixed-types",
	)
	deps.plaidSvc.On("InvestmentsHoldingsGet", ctx, linkedItem.AccessToken).Return(response, nil).Once()
	deps.securityRepo.On("UpsertBatch", ctx, mock.Anything).Return(map[string]string{
		investmentSecurityID: internalInvestmentSecurityID,
		depositorySecurityID: internalDepositorySecurityID,
	}, nil).Once()
	deps.accountRepo.On("GetByLinkedItemID", ctx, linkedItem.ID).Return([]domain.Account{
		{
			ID: investmentAccountID, PlaidAccountID: investmentPlaidAccountID,
			UserID: userID, Type: "investment",
		},
		{
			ID: depositoryAccountID, PlaidAccountID: depositoryPlaidAccountID,
			UserID: userID, Type: "depository",
		},
	}, nil).Once()
	deps.holdingRepo.On(
		"ReplaceForAccount",
		ctx,
		investmentAccountID,
		userID,
		mock.MatchedBy(func(holdings []domain.Holding) bool {
			return len(holdings) == 1 &&
				holdings[0].AccountID == investmentAccountID &&
				holdings[0].SecurityID == internalInvestmentSecurityID
		}),
	).Return(capturedDay, nil).Once()

	// setupInvestmentSyncService installs a permissive snapshot default. Use a fresh mock so this
	// test proves the mixed run upserts exactly once under the holding repository's captured day.
	snapshotRepo := new(MockPortfolioSnapshotRepository)
	snapshotRepo.On("Upsert", ctx, userID, capturedDay).
		Return(&domain.PortfolioSnapshot{ID: uuid.New().String(), UserID: userID}, nil).
		Once()
	svc := NewInvestmentSyncService(
		deps.webhookRepo,
		deps.linkedItemRepo,
		deps.accountRepo,
		deps.securityRepo,
		deps.holdingRepo,
		deps.invTxnRepo,
		snapshotRepo,
		deps.userAccountRepo,
		deps.plaidSvc,
		nil,
	)
	fake := redisclient.NewFake("cache")
	c := cache.NewCache(cache.Options{Client: fake})
	svc.SetCacheInvalidation(c.Generations())
	preGen, err := c.Generations().Get(ctx, cache.ScopeUser, userID)
	require.NoError(t, err)

	result, err := svc.SyncHoldings(ctx, linkedItem)

	require.NoError(t, err)
	assert.Equal(t, 1, result.AccountsProcessed)
	assert.Equal(t, 1, result.HoldingsReplaced)
	assert.Equal(t, 1, result.AccountsSkippedNonInvestment)
	assert.Equal(t, 1, result.HoldingsSkippedNonInvestment)
	assert.Empty(t, result.Errors)
	assert.Empty(t, result.SkippedIncomplete)
	deps.holdingRepo.AssertNotCalled(
		t, "ReplaceForAccount", ctx, depositoryAccountID, userID, mock.Anything,
	)
	deps.plaidSvc.AssertExpectations(t)
	deps.securityRepo.AssertExpectations(t)
	deps.accountRepo.AssertExpectations(t)
	deps.holdingRepo.AssertExpectations(t)
	snapshotRepo.AssertExpectations(t)

	postGen, err := c.Generations().Get(ctx, cache.ScopeUser, userID)
	require.NoError(t, err)
	assert.NotEqual(t, preGen, postGen,
		"a committed investment replacement must bump cache even when another account is skipped")
}

func TestInvestmentSyncService_SyncHoldings_NoBumpWhenCacheInvalidationNotWired(t *testing.T) {
	svc, deps := setupInvestmentSyncService(t)
	ctx := context.Background()
	linkedItem := makeLinkedItem("item-nowire", uuid.New().String())

	deps.plaidSvc.On("InvestmentsHoldingsGet", ctx, linkedItem.AccessToken).
		Return(plaid.NewInvestmentsHoldingsGetResponse([]plaid.AccountBase{}, []plaid.Holding{}, []plaid.Security{}, makeMinimalPlaidItem(), "req"), nil)
	deps.securityRepo.On("UpsertBatch", ctx, mock.Anything).Return(map[string]string{}, nil)
	deps.accountRepo.On("GetByLinkedItemID", ctx, linkedItem.ID).Return([]domain.Account{}, nil)

	// Deliberately do NOT call SetCacheInvalidation — must remain a nil-safe no-op.
	_, err := svc.SyncHoldings(ctx, linkedItem)
	require.NoError(t, err)
}

// ============================================================================
// InvestmentSyncService — SyncInvestmentTransactions (partial-commit-then-fail)
// ============================================================================

// TestInvestmentSyncService_SyncInvestmentTransactions_BumpsOnPartialCommitEvenWhenLaterPageFails
// mirrors transaction_sync_cache_invalidation_test.go's carried-forward fix: page 1 commits,
// page 2 fails — the user's cache generation must still change.
func TestInvestmentSyncService_SyncInvestmentTransactions_BumpsOnPartialCommitEvenWhenLaterPageFails(t *testing.T) {
	svc, deps := setupInvestmentSyncService(t)
	ctx := context.Background()

	userID := uuid.New().String()
	plaidAccountID := "plaid-acct-partial"
	internalAccountID := uuid.New().String()
	linkedItem := makeLinkedItem("item-partial", userID)

	deps.accountRepo.On("GetByLinkedItemIDIncludingRetained", ctx, linkedItem.ID).Return([]domain.Account{
		{ID: internalAccountID, PlaidAccountID: plaidAccountID, UserID: userID},
	}, nil)

	totalTxns := int32(1000)
	page1Txns := []plaid.InvestmentTransaction{makePlaidCashTxn("txn-p1", plaidAccountID, 10.0, "2024-01-01")}
	page1Resp := plaid.NewInvestmentsTransactionsGetResponse(
		makeMinimalPlaidItem(), []plaid.AccountBase{}, []plaid.Security{}, page1Txns, totalTxns, "req-p1",
	)

	deps.plaidSvc.On("InvestmentsTransactionsGet", ctx, "tok", mock.Anything, mock.Anything, int32(0), int32(500)).
		Return(page1Resp, nil).Once()
	// Page 1 storage SUCCEEDS — this is the commit that must not be lost.
	deps.invTxnRepo.On("UpsertBatch", ctx, mock.MatchedBy(func(txns []domain.InvestmentTransaction) bool {
		return len(txns) == 1
	})).Return(1, nil).Once()

	// Page 2 fetch FAILS after the page 1 commit above.
	deps.plaidSvc.On("InvestmentsTransactionsGet", ctx, "tok", mock.Anything, mock.Anything, int32(1), int32(500)).
		Return(nil, assert.AnError).Once()

	fake := redisclient.NewFake("cache")
	c := cache.NewCache(cache.Options{Client: fake})
	svc.SetCacheInvalidation(c.Generations())

	preGen, err := c.Generations().Get(ctx, cache.ScopeUser, userID)
	require.NoError(t, err)

	_, syncErr := svc.SyncInvestmentTransactions(ctx, linkedItem)
	require.Error(t, syncErr, "the sync as a whole must still surface the page-2 fetch failure")

	postGen, err := c.Generations().Get(ctx, cache.ScopeUser, userID)
	require.NoError(t, err)
	assert.NotEqual(t, preGen, postGen,
		"page 1 committed investment transactions before the page-2 failure — the cache MUST "+
			"be bumped even though SyncInvestmentTransactions ultimately returned an error")
}

// ============================================================================
// LiabilitySyncService — SyncLiabilities
// ============================================================================

func TestLiabilitySyncService_SyncLiabilities_BumpsUserCacheGenerationAfterSuccess(t *testing.T) {
	svc, deps := setupLiabilitySyncService(t)
	ctx := context.Background()

	plaidAccountID := "plaid-cc-cache"
	internalAccountID := uuid.New().String()
	userID := uuid.New().String()
	linkedItem := makeLinkedItem("item-liab-cache", userID)

	credit := makePlaidCreditLiability(plaidAccountID)
	resp := makePlaidLiabilitiesResponse([]plaid.CreditCardLiability{credit}, []plaid.MortgageLiability{}, []plaid.StudentLoan{}, plaidAccountID)
	deps.plaidSvc.On("LiabilitiesGet", ctx, "tok").Return(resp, nil)
	deps.accountRepo.On("GetByLinkedItemID", ctx, linkedItem.ID).Return([]domain.Account{
		{ID: internalAccountID, PlaidAccountID: plaidAccountID, UserID: userID},
	}, nil)
	deps.creditRepo.On("ReplaceForAccount", ctx, internalAccountID, userID, mock.Anything).Return(nil)
	deps.mortgageRepo.On("ReplaceForAccount", ctx, internalAccountID, userID, (*domain.MortgageLiability)(nil)).Return(nil)
	deps.studentLoanRepo.On("ReplaceForAccount", ctx, internalAccountID, userID, (*domain.StudentLoanLiability)(nil)).Return(nil)
	deps.linkedItemRepo.On("UpdateLiabilitiesLastSyncedAt", ctx, linkedItem.ID, mock.Anything).Return(nil)

	fake := redisclient.NewFake("cache")
	c := cache.NewCache(cache.Options{Client: fake})
	svc.SetCacheInvalidation(c.Generations())

	preGen, err := c.Generations().Get(ctx, cache.ScopeUser, userID)
	require.NoError(t, err)

	_, err = svc.SyncLiabilities(ctx, linkedItem)
	require.NoError(t, err)

	postGen, err := c.Generations().Get(ctx, cache.ScopeUser, userID)
	require.NoError(t, err)
	assert.NotEqual(t, preGen, postGen,
		"a successful liabilities sync must bump the user's cache generation so GetNetWorthTrend "+
			"cannot keep serving the pre-sync liability value")
}

// TestLiabilitySyncService_SyncLiabilities_BumpsOnPartialCommitEvenWhenLaterCategoryFails proves
// the same carried-forward "any commit, then bump" fix for the per-account, per-category loop:
// the credit-liability replace commits, then the mortgage-liability replace fails for the SAME
// account — the bump must still fire.
func TestLiabilitySyncService_SyncLiabilities_BumpsOnPartialCommitEvenWhenLaterCategoryFails(t *testing.T) {
	svc, deps := setupLiabilitySyncService(t)
	ctx := context.Background()

	plaidAccountID := "plaid-cc-partial"
	internalAccountID := uuid.New().String()
	userID := uuid.New().String()
	linkedItem := makeLinkedItem("item-liab-partial", userID)

	credit := makePlaidCreditLiability(plaidAccountID)
	resp := makePlaidLiabilitiesResponse([]plaid.CreditCardLiability{credit}, []plaid.MortgageLiability{}, []plaid.StudentLoan{}, plaidAccountID)
	deps.plaidSvc.On("LiabilitiesGet", ctx, "tok").Return(resp, nil)
	deps.accountRepo.On("GetByLinkedItemID", ctx, linkedItem.ID).Return([]domain.Account{
		{ID: internalAccountID, PlaidAccountID: plaidAccountID, UserID: userID},
	}, nil)

	// Credit replace SUCCEEDS — this is the commit that must not be lost.
	deps.creditRepo.On("ReplaceForAccount", ctx, internalAccountID, userID, mock.Anything).Return(nil)
	// Mortgage clear (no mortgage in this webhook payload) FAILS after the credit commit above.
	deps.mortgageRepo.On("ReplaceForAccount", ctx, internalAccountID, userID, (*domain.MortgageLiability)(nil)).
		Return(assert.AnError)

	fake := redisclient.NewFake("cache")
	c := cache.NewCache(cache.Options{Client: fake})
	svc.SetCacheInvalidation(c.Generations())

	preGen, err := c.Generations().Get(ctx, cache.ScopeUser, userID)
	require.NoError(t, err)

	_, syncErr := svc.SyncLiabilities(ctx, linkedItem)
	require.Error(t, syncErr, "the sync as a whole must still surface the mortgage-clear failure")

	postGen, err := c.Generations().Get(ctx, cache.ScopeUser, userID)
	require.NoError(t, err)
	assert.NotEqual(t, preGen, postGen,
		"the credit liability committed before the mortgage-clear failure — the cache MUST be "+
			"bumped even though SyncLiabilities ultimately returned an error")
}

func TestLiabilitySyncService_SyncLiabilities_NoBumpWhenCacheInvalidationNotWired(t *testing.T) {
	svc, deps := setupLiabilitySyncService(t)
	ctx := context.Background()
	linkedItem := makeLinkedItem("item-liab-nowire", uuid.New().String())

	resp := makePlaidLiabilitiesResponse([]plaid.CreditCardLiability{}, []plaid.MortgageLiability{}, []plaid.StudentLoan{})
	deps.plaidSvc.On("LiabilitiesGet", ctx, "tok").Return(resp, nil)
	deps.accountRepo.On("GetByLinkedItemID", ctx, linkedItem.ID).Return([]domain.Account{}, nil)
	deps.linkedItemRepo.On("UpdateLiabilitiesLastSyncedAt", ctx, linkedItem.ID, mock.Anything).Return(nil)

	// Deliberately do NOT call SetCacheInvalidation — must remain a nil-safe no-op.
	_, err := svc.SyncLiabilities(ctx, linkedItem)
	require.NoError(t, err)
}
