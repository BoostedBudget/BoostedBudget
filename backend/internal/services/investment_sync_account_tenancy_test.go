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
		{ID: ownedInternal, PlaidAccountID: ownedPlaidAcct, UserID: actorUserID},
		{ID: foreignInternal, PlaidAccountID: foreignPlaidAcct, UserID: strangerUserID},
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
