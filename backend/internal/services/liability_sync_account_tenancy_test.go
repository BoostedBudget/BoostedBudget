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

// TestSyncLiabilities_AccountOwnedByAnotherUser_IsSkipped covers the THIRD of the three sites that
// received the identical owner guard. Issue #1537 round 10, finding C-5.
//
// The guard exists because the (actor, account) pair is assembled from two independent sources: UserID
// comes from linkedItem.UserID, while the accounts come from GetByLinkedItemID, which is keyed on
// linked_item_id and is NOT user-scoped. A divergence would produce rows whose user and account each
// look right to the repository's own assertions (assertActorOwnsRows proves every row's user is the
// actor; assertRowsTargetAccount proves every row's account is the target) while the write lands on a
// foreign account. Nothing downstream notices, because `credit_liabilities` and its siblings carry only
// separate foreign keys.
//
// Reviewed finding this closes: the guard was pasted into three functions and only one was falsified.
// Each copy is an independent chance to invert a comparison, and a green sibling test says nothing
// about the copy next door — which is precisely how this branch's #1566 predicate turned out to be
// tautological at two of its three sites.
func TestSyncLiabilities_AccountOwnedByAnotherUser_IsSkipped(t *testing.T) {
	svc, deps := setupLiabilitySyncService(t)
	ctx := context.Background()

	actorUserID := uuid.New().String()
	strangerUserID := uuid.New().String()
	linkedItem := makeLinkedItem("item-liab-tenancy", actorUserID)

	ownedPlaid, ownedInternal := "plaid-cc-owned", uuid.New().String()
	foreignPlaid, foreignInternal := "plaid-cc-foreign", uuid.New().String()

	deps.accountRepo.On("GetByLinkedItemID", ctx, linkedItem.ID).Return([]domain.Account{
		{ID: ownedInternal, PlaidAccountID: ownedPlaid, UserID: actorUserID},
		{ID: foreignInternal, PlaidAccountID: foreignPlaid, UserID: strangerUserID},
	}, nil)

	// Plaid reports a credit liability for BOTH accounts. Only the actor's own may be written.
	resp := makePlaidLiabilitiesResponse(
		[]plaid.CreditCardLiability{
			makePlaidCreditLiability(ownedPlaid),
			makePlaidCreditLiability(foreignPlaid),
		},
		[]plaid.MortgageLiability{},
		[]plaid.StudentLoan{},
	)
	deps.plaidSvc.On("LiabilitiesGet", ctx, "tok").Return(resp, nil)

	// Permissive, so a foreign write shows up as a readable assertion failure rather than a mock panic.
	deps.creditRepo.On("ReplaceForAccount", ctx, mock.Anything, mock.Anything, mock.Anything).Return(nil).Maybe()
	deps.mortgageRepo.On("ReplaceForAccount", ctx, mock.Anything, mock.Anything, mock.Anything).Return(nil).Maybe()
	deps.studentLoanRepo.On("ReplaceForAccount", ctx, mock.Anything, mock.Anything, mock.Anything).Return(nil).Maybe()
	deps.linkedItemRepo.On("UpdateLiabilitiesLastSyncedAt", ctx, linkedItem.ID, mock.Anything).Return(nil).Maybe()

	result, err := svc.SyncLiabilities(ctx, linkedItem)
	require.NoError(t, err, "one foreign account must not fail the whole sync")

	for _, repoMock := range []*mock.Mock{
		&deps.creditRepo.Mock, &deps.mortgageRepo.Mock, &deps.studentLoanRepo.Mock,
	} {
		for _, c := range repoMock.Calls {
			if c.Method != "ReplaceForAccount" {
				continue
			}
			assert.NotEqual(t, foreignInternal, c.Arguments.Get(1),
				"a liability was written for account %s, which belongs to user %s, under actor %s. "+
					"The (actor, account) pair looks self-consistent to both repository assertions, so "+
					"it has to be rejected where the two values meet.",
				foreignInternal, strangerUserID, actorUserID)
		}
	}

	assert.Equal(t, 1, result.AccountsProcessed, "only the actor's own account should be processed")
}
