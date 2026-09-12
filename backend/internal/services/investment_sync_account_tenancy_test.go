package services

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/plaid/plaid-go/v23/plaid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
)

// TestSyncHoldings_AccountOwnedByAnotherUser_IsSkipped closes the last gap in #1566/#1568's tenancy
// story. Issue #1537 round 9, finding F14.
//
// THE GAP. The two repository-level assertions each prove something true and neither proves this:
// `assertActorOwnsRows` checks every row's UserID equals the actor, and `assertRowsTargetAccount`
// checks every row's AccountID equals the scalar accountID. Nothing checks that the ACCOUNT belongs
// to the ACTOR. And the two values enter from different places: `SyncHoldings` takes UserID from
// `linkedItem.UserID` and AccountID from `accountRepo.GetByLinkedItemID`, which is keyed on
// `linked_item_id` and is NOT user-scoped. So if `accounts.user_id` ever diverged from
// `linked_items.user_id`, every row would carry a consistent-looking (actor, foreign-account) pair,
// both assertions would pass, and `holdings` has only separate FKs — nothing downstream notices.
//
// Whether that divergence is reachable today is unresolved: no constraint enforces the two columns
// agree, and no constraint forbids it either. That is exactly the situation where a cheap guard at
// the point where the two values MEET is worth more than another assertion further down, which is
// why this fix lives in the service rather than in the repository.
func TestSyncHoldings_AccountOwnedByAnotherUser_IsSkipped(t *testing.T) {
	svc, deps := setupInvestmentSyncService(t)
	ctx := context.Background()

	actorUserID := uuid.New().String()
	strangerUserID := uuid.New().String()
	linkedItem := makeLinkedItem("item-tenancy", actorUserID)

	ownedPlaidAcct, ownedInternal := "plaid-acct-owned", uuid.New().String()
	foreignPlaidAcct, foreignInternal := "plaid-acct-foreign", uuid.New().String()

	plaidSecurityID, internalSecurityID := "plaid-sec-001", uuid.New().String()
	sec := makePlaidSecurity(plaidSecurityID, "Apple Inc.", "AAPL")

	resp := plaid.NewInvestmentsHoldingsGetResponse(
		[]plaid.AccountBase{},
		[]plaid.Holding{
			makePlaidHolding(ownedPlaidAcct, plaidSecurityID, 10, 165, 1650, 1500),
			makePlaidHolding(foreignPlaidAcct, plaidSecurityID, 99, 165, 16335, 15000),
		},
		[]plaid.Security{sec},
		makeMinimalPlaidItem(),
		"req-tenancy",
	)
	deps.plaidSvc.On("InvestmentsHoldingsGet", ctx, linkedItem.AccessToken).Return(resp, nil)
	deps.securityRepo.On("UpsertBatch", ctx, mock.Anything).
		Return(map[string]string{plaidSecurityID: internalSecurityID}, nil)

	// The linked item resolves to TWO accounts, one of which is owned by somebody else.
	deps.accountRepo.On("GetByLinkedItemID", ctx, linkedItem.ID).Return([]domain.Account{
		{ID: ownedInternal, PlaidAccountID: ownedPlaidAcct, UserID: actorUserID, Type: "investment"},
		{ID: foreignInternal, PlaidAccountID: foreignPlaidAcct, UserID: strangerUserID, Type: "investment"},
	}, nil)

	// Permissive, so a write to the foreign account is a readable assertion failure, not a panic.
	deps.holdingRepo.On("ReplaceForAccount", ctx, mock.Anything, mock.Anything, mock.Anything).Return(nil)

	result, err := svc.SyncHoldings(ctx, linkedItem)
	require.NoError(t, err, "one foreign account must not fail the whole sync")

	deps.holdingRepo.AssertCalled(t, "ReplaceForAccount", ctx, ownedInternal, actorUserID, mock.Anything)
	deps.holdingRepo.AssertNotCalled(t, "ReplaceForAccount", ctx, foreignInternal, actorUserID, mock.Anything)

	for _, c := range deps.holdingRepo.Calls {
		if c.Method == "ReplaceForAccount" {
			assert.NotEqual(t, foreignInternal, c.Arguments.Get(1),
				"holdings were written for account %s, which belongs to user %s, under the actor %s. "+
					"The (actor, account) pair looks self-consistent to both repository assertions, so "+
					"this must be rejected where the two values meet.", foreignInternal, strangerUserID, actorUserID)
		}
	}
	assert.Equal(t, 1, result.AccountsProcessed, "only the actor's own account should be processed")
}

// TestSyncInvestmentTransactions_AccountOwnedByAnotherUser_IsSkipped covers the SECOND of the three
// sites that got the identical owner guard. Issue #1537 round 10, finding C-5.
//
// Why it needs its own test rather than trusting the first: review pointed out that the same guard was
// pasted into three places and only one was falsified. Three copies of a predicate are three
// opportunities to paste it with the comparison inverted or the wrong variable, and a passing sibling
// test proves nothing about the copy next door. This is the same lesson the branch learned when a
// tenancy predicate turned out to be tautological at 2 of 3 sites.
func TestSyncInvestmentTransactions_AccountOwnedByAnotherUser_IsSkipped(t *testing.T) {
	svc, deps := setupInvestmentSyncService(t)
	ctx := context.Background()

	actorUserID := uuid.New().String()
	strangerUserID := uuid.New().String()
	linkedItem := makeLinkedItem("item-inv-txn-tenancy", actorUserID)

	ownedPlaid, ownedInternal := "plaid-acct-owned", uuid.New().String()
	foreignPlaid, foreignInternal := "plaid-acct-foreign", uuid.New().String()

	deps.accountRepo.On("GetByLinkedItemIDIncludingRetained", ctx, linkedItem.ID).Return([]domain.Account{
		{ID: ownedInternal, PlaidAccountID: ownedPlaid, UserID: actorUserID},
		{ID: foreignInternal, PlaidAccountID: foreignPlaid, UserID: strangerUserID},
	}, nil)

	// A transaction that belongs to the FOREIGN account. With the guard working, that plaid account id
	// maps to nothing, so nothing may be written for it.
	txn := makePlaidCashTxn("txn-foreign", foreignPlaid, 500.0, "2024-01-15")
	txnResp := plaid.NewInvestmentsTransactionsGetResponse(
		makeMinimalPlaidItem(),
		[]plaid.AccountBase{},
		[]plaid.Security{},
		[]plaid.InvestmentTransaction{txn},
		1,
		"req-tenancy",
	)
	deps.plaidSvc.On("InvestmentsTransactionsGet",
		ctx, linkedItem.AccessToken,
		mock.AnythingOfType("string"), mock.AnythingOfType("string"),
		mock.AnythingOfType("int32"), mock.AnythingOfType("int32"),
	).Return(txnResp, nil)

	deps.invTxnRepo.On("UpsertBatch", ctx, mock.Anything).Return(0, nil).Maybe()
	deps.linkedItemRepo.On("UpdateInvestmentsLastSyncedDate", ctx, linkedItem.ID, mock.Anything).Return(nil).Maybe()

	_, err := svc.SyncInvestmentTransactions(ctx, linkedItem)
	require.NoError(t, err, "one foreign account must not fail the whole sync")

	for _, c := range deps.invTxnRepo.Calls {
		if c.Method != "UpsertBatch" {
			continue
		}
		txns, ok := c.Arguments.Get(1).([]domain.InvestmentTransaction)
		if !ok {
			continue
		}
		for _, tx := range txns {
			assert.NotEqual(t, foreignInternal, tx.AccountID,
				"an investment transaction was written for account %s, owned by user %s, under actor %s",
				foreignInternal, strangerUserID, actorUserID)
			assert.NotEqual(t, strangerUserID, tx.UserID,
				"an investment transaction was written carrying a foreign user_id")
		}
	}
}

// TestSyncInvestmentTransactions_RetainedAndOwnedByAnotherUser_IsSkipped is the composite the
// round-12 task-6 brief demanded: round 11 switched SyncInvestmentTransactions's account map to
// GetByLinkedItemIDIncludingRetained (see TestInvestmentSyncService_SyncInvestmentTransactions_
// RetainedAccount_StillStored, which uses the actor's OWN retained account) so a retained account's
// transactions are no longer silently dropped. But cross-user protection on that same path is a Go
// `if a.UserID != linkedItem.UserID` check (~:517-525), not a SQL predicate — the query has no
// user_id filter. TestSyncInvestmentTransactions_AccountOwnedByAnotherUser_IsSkipped proves the
// tenancy axis alone, using a zero-value ("") status. Neither existing test proves the guard still
// fires once BOTH axes are true on the SAME account: retained AND foreign.
//
// If this ever regresses — e.g. the guard were rewritten to skip only accounts with
// Status == AccountStatusActive && a.UserID != linkedItem.UserID, or reordered so a status check
// short-circuits before the ownership check — a retained foreign account's transaction would be
// written under the actor. Mutation-check: deleting the `if a.UserID != linkedItem.UserID`
// continue block (or negating its condition) at investment_sync_service.go:~522 must turn this RED.
func TestSyncInvestmentTransactions_RetainedAndOwnedByAnotherUser_IsSkipped(t *testing.T) {
	svc, deps := setupInvestmentSyncService(t)
	ctx := context.Background()

	actorUserID := uuid.New().String()
	strangerUserID := uuid.New().String()
	linkedItem := makeLinkedItem("item-inv-txn-retained-foreign", actorUserID)

	// The foreign account is BOTH retained (status=removed) AND owned by a different user —
	// the one composite the brief says was never falsified.
	foreignPlaid, foreignInternal := "plaid-acct-retained-foreign", uuid.New().String()

	deps.accountRepo.On("GetByLinkedItemIDIncludingRetained", ctx, linkedItem.ID).Return([]domain.Account{
		{ID: foreignInternal, PlaidAccountID: foreignPlaid, UserID: strangerUserID, Status: domain.AccountStatusRemoved},
	}, nil)

	// A transaction Plaid still reports for the retained, foreign-owned account.
	txn := makePlaidCashTxn("txn-retained-foreign", foreignPlaid, 750.0, "2024-01-15")
	txnResp := plaid.NewInvestmentsTransactionsGetResponse(
		makeMinimalPlaidItem(),
		[]plaid.AccountBase{},
		[]plaid.Security{},
		[]plaid.InvestmentTransaction{txn},
		1,
		"req-retained-foreign",
	)
	deps.plaidSvc.On("InvestmentsTransactionsGet",
		ctx, linkedItem.AccessToken,
		mock.AnythingOfType("string"), mock.AnythingOfType("string"),
		mock.AnythingOfType("int32"), mock.AnythingOfType("int32"),
	).Return(txnResp, nil)

	// Return the batch's own length rather than a hardcoded 0: with a constant 0 the
	// TransactionsInserted assertion below could never fail, mutant or not, and would be inert
	// scaffolding dressed as a check (#1569 round 12 task 6 review).
	deps.invTxnRepo.On("UpsertBatch", ctx, mock.Anything).
		Return(func(_ context.Context, txns []domain.InvestmentTransaction) int { return len(txns) },
			func(_ context.Context, _ []domain.InvestmentTransaction) error { return nil }).Maybe()
	deps.linkedItemRepo.On("UpdateInvestmentsLastSyncedDate", ctx, linkedItem.ID, mock.Anything).Return(nil).Maybe()

	result, err := svc.SyncInvestmentTransactions(ctx, linkedItem)
	require.NoError(t, err, "a retained foreign account must not fail the whole sync")

	assert.Equal(t, 0, result.TransactionsInserted,
		"a retained account owned by another user must still be skipped, not stored")

	for _, c := range deps.invTxnRepo.Calls {
		if c.Method != "UpsertBatch" {
			continue
		}
		txns, ok := c.Arguments.Get(1).([]domain.InvestmentTransaction)
		if !ok {
			continue
		}
		for _, tx := range txns {
			assert.NotEqual(t, foreignInternal, tx.AccountID,
				"an investment transaction was written for a RETAINED account %s, owned by user %s, "+
					"under actor %s — the tenancy guard must fire even when the account is retained",
				foreignInternal, strangerUserID, actorUserID)
		}
	}
}
