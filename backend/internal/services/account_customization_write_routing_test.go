package services

// Issue #1609, Item 3 (task 3c, Part 1) — the customization WRITE has to follow the same chain
// the READ follows.
//
// Task 3b made every account read chain-authoritative:
//
//	CASE WHEN a.user_account_id IS NOT NULL THEN a_ua.<field> ELSE a.<field> END
//
// PATCH /api/v1/accounts/{id} nevertheless still wrote `accounts` for every account, chained or
// not. For a chained account that write now lands in a column the read no longer prefers, so the
// edit "saves" and reverts on refresh — the defect this batch exists to fix, relocated to another
// endpoint.
//
// ⚠ The frontend already routes chained accounts to PATCH /api/v1/user-accounts/{id}
// (useAccountCustomization.ts), so it is tempting to call this redundant. It is not: the API must
// not depend on client discipline. MCP tools, integrations and anyone holding the OpenAPI contract
// reach this endpoint directly, and the endpoint is documented as updating the account.

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/testsupport/mockassert"
)

// TestUpdateAccount_ChainedAccountWritesToUserAccount is the routing claim: for a chained account
// the customization goes to the user-account service — the SAME path PATCH /user-accounts/{id}
// uses — and the legacy accounts write does not happen at all.
func TestUpdateAccount_ChainedAccountWritesToUserAccount(t *testing.T) {
	mockAccountRepo := &MockAccountRepository{}
	mockUASvc := &MockUserAccountService{}
	service := &accountService{accountRepo: mockAccountRepo, userAccountSvc: mockUASvc}

	ctx := context.Background()
	userID := "user-1"
	accountID := "acct-1"
	userAccountID := "ua-1"

	nickname := "  Renamed  "
	color := "#abcdef"
	hidden := true

	existing := &domain.Account{
		ID: accountID, UserID: userID, Name: "Chase Checking", Type: "depository",
		UserAccountID: &userAccountID,
	}
	// The re-read after the write: the row the endpoint returns must be the row the chain-
	// authoritative read produces, not a locally reconstructed guess.
	persisted := &domain.Account{
		ID: accountID, UserID: userID, Name: "Chase Checking", Type: "depository",
		UserAccountID: &userAccountID,
		Nickname:      strPtr("Renamed"), CustomColor: strPtr("#ABCDEF"), IsHidden: true,
	}

	mockAccountRepo.On("GetByIDAndUser", ctx, accountID, userID).Return(existing, nil).Once()
	mockUASvc.On("UpdateCustomization", ctx, userAccountID, userID,
		mock.MatchedBy(func(req *domain.UpdateUserAccountRequest) bool {
			return req.Nickname != nil && *req.Nickname == "  Renamed  " &&
				req.CustomColor != nil && *req.CustomColor == "#ABCDEF" &&
				req.IsHidden != nil && *req.IsHidden &&
				req.CustomIcon == nil && req.CustomEmoji == nil && req.DisplayOrder == nil
		})).Return(&domain.UserAccount{ID: userAccountID, UserID: userID}, nil).Once()
	mockAccountRepo.On("GetByIDAndUser", ctx, accountID, userID).Return(persisted, nil).Once()

	result, err := service.UpdateAccount(ctx, accountID, userID, &domain.UpdateAccountRequest{
		Nickname: &nickname, CustomColor: &color, IsHidden: &hidden,
	})

	require.NoError(t, err)
	require.NotNil(t, result)
	require.NotNil(t, result.Nickname)
	assert.Equal(t, "Renamed", *result.Nickname)
	assert.True(t, result.IsHidden)
	mockUASvc.AssertExpectations(t)
	mockAccountRepo.AssertExpectations(t)
	// The legacy write must not ALSO happen: a dual write re-creates the split brain, and the
	// accounts copy is the one nothing reads any more.
	mockassert.NoCallTo(t, &mockAccountRepo.Mock, "Update")
}

// TestUpdateAccount_UnchainedAccountStillWritesAccounts is the no-regression half.
// accounts.user_account_id is nullable — the startup repair skips no-match and decryption-failure
// rows — and those accounts read the ELSE leg, so they must keep writing `accounts`.
func TestUpdateAccount_UnchainedAccountStillWritesAccounts(t *testing.T) {
	mockAccountRepo := &MockAccountRepository{}
	mockUASvc := &MockUserAccountService{}
	service := &accountService{accountRepo: mockAccountRepo, userAccountSvc: mockUASvc}

	ctx := context.Background()
	userID := "user-1"
	accountID := "acct-1"

	existing := &domain.Account{ID: accountID, UserID: userID, Name: "Manual", Type: "depository"}
	mockAccountRepo.On("GetByIDAndUser", ctx, accountID, userID).Return(existing, nil).Once()
	mockAccountRepo.On("Update", ctx, mock.MatchedBy(func(acc *domain.Account) bool {
		return acc.ID == accountID && acc.Nickname != nil && *acc.Nickname == "Renamed"
	}), userID).Return(nil).Once()

	nickname := "Renamed"
	result, err := service.UpdateAccount(ctx, accountID, userID, &domain.UpdateAccountRequest{
		Nickname: &nickname,
	})

	require.NoError(t, err)
	require.NotNil(t, result)
	mockAccountRepo.AssertExpectations(t)
	mockassert.NoCallTo(t, &mockUASvc.Mock, "UpdateCustomization")
}

// TestUpdateAccount_ChainedAccountWithNoUserAccountServiceFallsBack keeps the service honest about
// its OPTIONAL dependency. userAccountSvc is nil in several construction paths and in most of this
// package's own fixtures; a nil-dereference here would turn a customization edit into a panic.
// Falling back to the legacy write is strictly better than crashing, and it is what the code did
// before this change.
func TestUpdateAccount_ChainedAccountWithNoUserAccountServiceFallsBack(t *testing.T) {
	mockAccountRepo := &MockAccountRepository{}
	service := &accountService{accountRepo: mockAccountRepo} // userAccountSvc deliberately nil

	ctx := context.Background()
	userID := "user-1"
	accountID := "acct-1"
	userAccountID := "ua-1"

	existing := &domain.Account{ID: accountID, UserID: userID, UserAccountID: &userAccountID}
	mockAccountRepo.On("GetByIDAndUser", ctx, accountID, userID).Return(existing, nil).Once()
	mockAccountRepo.On("Update", ctx, mock.Anything, userID).Return(nil).Once()

	nickname := "Renamed"
	_, err := service.UpdateAccount(ctx, accountID, userID, &domain.UpdateAccountRequest{
		Nickname: &nickname,
	})

	require.NoError(t, err)
	mockAccountRepo.AssertExpectations(t)
}

// TestUpdateAccount_ChainedWriteFailurePropagates — a failed chain write must be an error, not a
// silently-successful response echoing the values the caller sent. The whole class of bug this
// task closes is "the write did not happen and nothing said so".
func TestUpdateAccount_ChainedWriteFailurePropagates(t *testing.T) {
	mockAccountRepo := &MockAccountRepository{}
	mockUASvc := &MockUserAccountService{}
	service := &accountService{accountRepo: mockAccountRepo, userAccountSvc: mockUASvc}

	ctx := context.Background()
	userID := "user-1"
	accountID := "acct-1"
	userAccountID := "ua-1"
	boom := errors.New("chain write failed")

	mockAccountRepo.On("GetByIDAndUser", ctx, accountID, userID).
		Return(&domain.Account{ID: accountID, UserID: userID, UserAccountID: &userAccountID}, nil).Once()
	mockUASvc.On("UpdateCustomization", ctx, userAccountID, userID, mock.Anything).
		Return(nil, boom).Once()

	nickname := "Renamed"
	result, err := service.UpdateAccount(ctx, accountID, userID, &domain.UpdateAccountRequest{
		Nickname: &nickname,
	})

	require.Error(t, err)
	assert.ErrorIs(t, err, boom)
	assert.Nil(t, result)
	mockassert.NoCallTo(t, &mockAccountRepo.Mock, "Update")
}
