package services

import (
	"context"
	"errors"
	"log/slog"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/repository"
	"github.com/maxnussbaum/BoostedBudget/internal/testsupport/mockassert"
)

// Issue #1569 Phase 0 — stable account identity is a precondition for retention.
//
// Retaining accounts instead of deleting them is only safe if every account carries a
// user_account_id. Without it, accountChainKey derives DIFFERENT canonical keys for the two
// members of a re-link chain and the chain is counted TWICE in the dashboard baseline — the
// RESIDUAL LIMIT documented at dashboard_service.go:35-73. Once rows are retained forever
// rather than cascade-deleted, that seed becomes permanent.
//
// The fix is `accounts.user_account_id NOT NULL`, and that constraint is only safe once no
// writer can produce a NULL. LinkAccounts used to log a MatchOrCreate failure and CONTINUE,
// creating an account with no stable identity. These tests pin the new contract: it refuses.

// newPermissiveUserAccountService returns a UserAccountService mock that always resolves a stable
// identity. Issue #1569 made a missing user_accounts identity fatal to LinkAccounts, so every
// harness that exercises the link path must wire one — previously they passed nil and relied on
// the service being optional.
func newPermissiveUserAccountService(userID string) *MockUserAccountService {
	uaSvc := &MockUserAccountService{}
	uaSvc.On("MatchOrCreate", mock.Anything, mock.Anything, mock.Anything, mock.Anything,
		mock.Anything, mock.Anything, mock.Anything).
		Return(&domain.UserAccount{ID: uuid.New().String(), UserID: userID}, nil)
	return uaSvc
}

// newStableIdentityHarness builds a LinkAccounts harness whose UserAccountService can be made
// to fail, so the refusal path is observable end to end rather than asserted on the mock.
func newStableIdentityHarness(t *testing.T, matchErr error) (
	svc AccountService, userID string, accountRepo *MockAccountRepository,
	linkedItemRepo *MockLinkedItemRepository, uaSvc *MockUserAccountService,
) {
	t.Helper()
	ctx := context.Background()
	userID = uuid.New().String()
	institutionID := "ins_stable_identity"
	mask := "4242"

	plaidAccounts := []map[string]interface{}{
		{
			"account_id": "plaid-acct-stable-identity-1",
			"balances":   map[string]interface{}{"available": 100.0, "current": 110.0, "iso_currency_code": "USD"},
			"mask":       mask,
			"name":       "Checking",
			"subtype":    "checking",
			"type":       "depository",
		},
	}
	_, plaidClient := newTestPlaidServerWithPersistentID(
		t, "access-stable-identity", "plaid-item-stable-identity", plaidAccounts)

	mockPlaidSvc := &MockPlaidService{}
	mockPlaidSvc.On("GetClient").Return(plaidClient)

	linkedItemRepo = &MockLinkedItemRepository{}
	accountRepo = &MockAccountRepository{}
	mockUserRepo := &MockUserRepository{}
	uaSvc = &MockUserAccountService{}

	mockUserRepo.On("GetUserByID", ctx, userID).
		Return(&domain.User{ID: userID, SubscriptionTier: "premium"}, nil)
	linkedItemRepo.On("GetByUserAndInstitution", ctx, userID, institutionID, string(domain.LinkedItemStatusActive)).
		Return(nil, nil)
	linkedItemRepo.On("GetByUserAndInstitution", ctx, userID, institutionID, string(domain.LinkedItemStatusDisconnected)).
		Return(nil, repository.ErrLinkedItemNotFound)
	accountRepo.On("CheckFingerprintOwnership", mock.Anything, mock.Anything, userID).Return(nil, nil)

	newItemID := uuid.New().String()
	linkedItemRepo.On("Create", ctx, mock.Anything).Run(func(args mock.Arguments) {
		args.Get(1).(*domain.LinkedItem).ID = newItemID
	}).Return(nil)
	accountRepo.On("CreateBatch", ctx, mock.Anything, mock.Anything).Return(nil)

	if matchErr != nil {
		uaSvc.On("MatchOrCreate", ctx, userID, institutionID, mock.Anything, mock.Anything, mock.Anything, mock.Anything).
			Return(nil, matchErr)
	} else {
		uaSvc.On("MatchOrCreate", ctx, userID, institutionID, mock.Anything, mock.Anything, mock.Anything, mock.Anything).
			Return(&domain.UserAccount{ID: uuid.New().String(), UserID: userID}, nil)
	}

	svc = NewAccountService(
		mockPlaidSvc, linkedItemRepo, accountRepo, mockUserRepo, nil,
		WithEncryptor(newTestFieldEncryptor(t)),
		WithUserAccountService(uaSvc),
		WithAccountServiceLogger(slog.Default()),
	)
	return svc, userID, accountRepo, linkedItemRepo, uaSvc
}

// TestLinkAccounts_MatchOrCreateFailure_RefusesTheLink is the core Phase 0 contract.
// Previously this path logged "Continue without user_account_id" and created the account anyway.
func TestLinkAccounts_MatchOrCreateFailure_RefusesTheLink(t *testing.T) {
	svc, userID, accountRepo, _, _ := newStableIdentityHarness(t, errors.New("user_accounts lookup exploded"))
	ctx := context.Background()

	result, err := svc.LinkAccounts(ctx, userID, "public-token", "ins_stable_identity", "Test Bank")

	require.Error(t, err,
		"a MatchOrCreate failure must FAIL the link: an account with no stable identity can never "+
			"be tracked across re-links, and under retention it becomes a permanent double-count seed")
	assert.Nil(t, result)
	accountRepo.AssertNotCalled(t, "CreateBatch", mock.Anything, mock.Anything, mock.Anything)
}

// TestLinkAccounts_MatchOrCreateFailure_ErrorIsIdentifiable pins that callers can distinguish this
// refusal from a generic Plaid/database failure, so the API can render an actionable message
// rather than a bare 500.
func TestLinkAccounts_MatchOrCreateFailure_ErrorIsIdentifiable(t *testing.T) {
	svc, userID, _, _, _ := newStableIdentityHarness(t, errors.New("user_accounts lookup exploded"))

	_, err := svc.LinkAccounts(context.Background(), userID, "public-token", "ins_stable_identity", "Test Bank")

	require.Error(t, err)
	assert.ErrorIs(t, err, ErrStableAccountIdentityUnavailable,
		"the refusal must be a sentinel error, not an opaque wrapped string")
}

// TestLinkAccounts_StableIdentityPresent_StillSucceeds is the counter-test: the happy path must
// not regress, and every created account must carry a non-nil UserAccountID.
func TestLinkAccounts_StableIdentityPresent_StillSucceeds(t *testing.T) {
	svc, userID, accountRepo, _, _ := newStableIdentityHarness(t, nil)
	ctx := context.Background()

	var created []domain.Account
	accountRepo.ExpectedCalls = nil
	accountRepo.On("CheckFingerprintOwnership", mock.Anything, mock.Anything, userID).Return(nil, nil)
	accountRepo.On("CreateBatch", ctx, mock.Anything, mock.Anything).
		Run(func(args mock.Arguments) { created = args.Get(2).([]domain.Account) }).Return(nil)

	result, err := svc.LinkAccounts(ctx, userID, "public-token", "ins_stable_identity", "Test Bank")

	require.NoError(t, err)
	require.NotNil(t, result)
	require.NotEmpty(t, created, "the happy path must still create accounts")
	for _, acct := range created {
		require.NotNil(t, acct.UserAccountID,
			"every persisted account must carry a stable identity — this is what makes the "+
				"accounts.user_account_id NOT NULL constraint safe to add")
		assert.NotEmpty(t, *acct.UserAccountID)
	}
}

// TestLinkAccounts_NoUserAccountService_RefusesTheLink closes the second NULL source. The service
// is optional today (`if s.userAccountSvc != nil`), so a wiring gap silently produced accounts
// with no stable identity — indistinguishable, in the database, from a MatchOrCreate failure.
func TestLinkAccounts_NoUserAccountService_RefusesTheLink(t *testing.T) {
	ctx := context.Background()
	userID := uuid.New().String()
	institutionID := "ins_stable_identity"

	plaidAccounts := []map[string]interface{}{
		{
			"account_id": "plaid-acct-no-uasvc",
			"balances":   map[string]interface{}{"available": 1.0, "current": 1.0, "iso_currency_code": "USD"},
			"mask":       "1111", "name": "Checking", "subtype": "checking", "type": "depository",
		},
	}
	_, plaidClient := newTestPlaidServerWithPersistentID(t, "access-no-uasvc", "item-no-uasvc", plaidAccounts)

	mockPlaidSvc := &MockPlaidService{}
	mockPlaidSvc.On("GetClient").Return(plaidClient)
	linkedItemRepo := &MockLinkedItemRepository{}
	accountRepo := &MockAccountRepository{}
	mockUserRepo := &MockUserRepository{}

	mockUserRepo.On("GetUserByID", ctx, userID).Return(&domain.User{ID: userID, SubscriptionTier: "premium"}, nil)
	linkedItemRepo.On("GetByUserAndInstitution", ctx, userID, institutionID, string(domain.LinkedItemStatusActive)).Return(nil, nil)
	linkedItemRepo.On("GetByUserAndInstitution", ctx, userID, institutionID, string(domain.LinkedItemStatusDisconnected)).
		Return(nil, repository.ErrLinkedItemNotFound)
	accountRepo.On("CheckFingerprintOwnership", mock.Anything, mock.Anything, userID).Return(nil, nil)
	linkedItemRepo.On("Create", ctx, mock.Anything).Run(func(args mock.Arguments) {
		args.Get(1).(*domain.LinkedItem).ID = uuid.New().String()
	}).Return(nil)
	accountRepo.On("CreateBatch", ctx, mock.Anything, mock.Anything).Return(nil)

	// Deliberately omit WithUserAccountService.
	svc := NewAccountService(mockPlaidSvc, linkedItemRepo, accountRepo, mockUserRepo, nil,
		WithEncryptor(newTestFieldEncryptor(t)), WithAccountServiceLogger(slog.Default()))

	_, err := svc.LinkAccounts(ctx, userID, "public-token", institutionID, "Test Bank")

	require.ErrorIs(t, err, ErrStableAccountIdentityUnavailable,
		"an unwired UserAccountService must refuse the link, not silently create identity-less accounts")
	accountRepo.AssertNotCalled(t, "CreateBatch", mock.Anything, mock.Anything, mock.Anything)
}

// ── #1569 T10-FIX item 1: a refused re-link must not strand the predecessor ────────────────────

// newRelinkRefusalHarness is newStableIdentityHarness's re-link sibling: an ACTIVE linked item
// already exists at the institution, so LinkAccounts takes the step-5b retirement path.
func newRelinkRefusalHarness(t *testing.T, matchErr error) (
	svc AccountService, userID string, oldItemID string, oldAccountIDs []string,
	accountRepo *MockAccountRepository, linkedItemRepo *MockLinkedItemRepository,
) {
	t.Helper()
	ctx := context.Background()
	userID = uuid.New().String()
	institutionID := "ins_relink_refusal"

	plaidAccounts := []map[string]interface{}{
		{
			"account_id": "plaid-acct-relink-refusal-1",
			"balances":   map[string]interface{}{"available": 100.0, "current": 110.0, "iso_currency_code": "USD"},
			"mask":       "4242", "name": "Checking", "subtype": "checking", "type": "depository",
		},
	}
	_, plaidClient := newTestPlaidServerWithPersistentID(
		t, "access-relink-refusal", "plaid-item-relink-refusal", plaidAccounts)

	mockPlaidSvc := &MockPlaidService{}
	mockPlaidSvc.On("GetClient").Return(plaidClient)

	linkedItemRepo = &MockLinkedItemRepository{}
	accountRepo = &MockAccountRepository{}
	mockUserRepo := &MockUserRepository{}
	uaSvc := &MockUserAccountService{}

	oldItemID = uuid.New().String()
	oldAccountIDs = []string{uuid.New().String(), uuid.New().String()}
	sharedUA := uuid.New().String()
	oldAccounts := make([]domain.Account, 0, len(oldAccountIDs))
	for i, id := range oldAccountIDs {
		oldAccounts = append(oldAccounts, domain.Account{
			ID: id, UserID: userID, LinkedItemID: oldItemID, Name: "Old " + id,
			Type: "depository", Status: domain.AccountStatusActive, UserAccountID: &sharedUA,
		})
		_ = i
	}

	mockUserRepo.On("GetUserByID", ctx, userID).
		Return(&domain.User{ID: userID, SubscriptionTier: "premium"}, nil)
	linkedItemRepo.On("GetByUserAndInstitution", ctx, userID, institutionID, string(domain.LinkedItemStatusActive)).
		Return(&domain.LinkedItem{ID: oldItemID, UserID: userID, InstitutionID: institutionID,
			Status: domain.LinkedItemStatusActive}, nil)
	accountRepo.On("CheckFingerprintOwnership", mock.Anything, mock.Anything, userID).Return(nil, nil)
	linkedItemRepo.On("Create", ctx, mock.Anything).Run(func(args mock.Arguments) {
		args.Get(1).(*domain.LinkedItem).ID = uuid.New().String()
	}).Return(nil)
	linkedItemRepo.On("Update", ctx, mock.Anything).Return(nil)
	accountRepo.On("GetByLinkedItemID", ctx, oldItemID).Return(oldAccounts, nil)
	accountRepo.On("UpdateStatus", ctx, mock.Anything, userID, domain.AccountStatusReplaced, mock.Anything).Return(nil)
	accountRepo.On("CreateBatch", ctx, mock.Anything, mock.Anything).Return(nil)

	if matchErr != nil {
		uaSvc.On("MatchOrCreate", ctx, userID, institutionID, mock.Anything, mock.Anything, mock.Anything, mock.Anything).
			Return(nil, matchErr)
	} else {
		uaSvc.On("MatchOrCreate", ctx, userID, institutionID, mock.Anything, mock.Anything, mock.Anything, mock.Anything).
			Return(&domain.UserAccount{ID: uuid.New().String(), UserID: userID}, nil)
	}

	svc = NewAccountService(
		mockPlaidSvc, linkedItemRepo, accountRepo, mockUserRepo, nil,
		WithEncryptor(newTestFieldEncryptor(t)),
		WithUserAccountService(uaSvc),
		WithAccountServiceLogger(slog.Default()),
	)
	return svc, userID, oldItemID, oldAccountIDs, accountRepo, linkedItemRepo
}

// TestLinkAccounts_RelinkRefused_LeavesThePredecessorLive is #1569 T10-FIX item 1.
//
// Step 5b retired the predecessor — old item 'replaced', every old account 'replaced' — and step 6
// then converted the Plaid accounts, any one of which can refuse the whole link. On that path the
// new item holds NO accounts, the old accounts are all inactive, and no successor pointer exists
// anywhere. NEITHER repair mechanism can reach it: supersedeStrandedChainSiblings and the
// succession closure both run inside CreateBatch, which never executed. Every balance the user has
// at that institution is now attached to rows that read as retired, with nothing live to carry
// them.
//
// The retirement is therefore deferred to the last possible moment — immediately before
// CreateBatch, once every account has a stable identity and the batch is about to be written.
func TestLinkAccounts_RelinkRefused_LeavesThePredecessorLive(t *testing.T) {
	svc, userID, _, _, accountRepo, linkedItemRepo := newRelinkRefusalHarness(
		t, errors.New("user_accounts lookup exploded"))

	_, err := svc.LinkAccounts(context.Background(), userID, "public-token", "ins_relink_refusal", "Test Bank")

	require.ErrorIs(t, err, ErrStableAccountIdentityUnavailable)
	accountRepo.AssertNotCalled(t, "CreateBatch", mock.Anything, mock.Anything, mock.Anything)
	// A refused link must leave the predecessor accounts ACTIVE. Marking them 'replaced' before the
	// successor exists strands every balance at the institution: the new item has no accounts, and
	// both repair paths live inside the CreateBatch that never ran.
	mockassert.NoCallTo(t, &accountRepo.Mock, "UpdateStatus")
	// The same applies to the linked_item: retiring it points a 'replaced' item at an item with no
	// accounts, and stops the connection that is still the user's only live one.
	mockassert.NoCallTo(t, &linkedItemRepo.Mock, "Update")
}

// TestLinkAccounts_RelinkSucceeds_StillRetiresThePredecessor is the counter-test. Deferring the
// retirement must not delete it: on the happy path the predecessor is still retired, and still
// BEFORE CreateBatch, because that is what lets the successor insert claim the fingerprint.
func TestLinkAccounts_RelinkSucceeds_StillRetiresThePredecessor(t *testing.T) {
	svc, userID, oldItemID, oldAccountIDs, accountRepo, linkedItemRepo := newRelinkRefusalHarness(t, nil)
	ctx := context.Background()

	var order []string
	accountRepo.ExpectedCalls = nil
	accountRepo.On("CheckFingerprintOwnership", mock.Anything, mock.Anything, userID).Return(nil, nil)
	accountRepo.On("GetByLinkedItemID", ctx, oldItemID).Return([]domain.Account{
		{ID: oldAccountIDs[0], UserID: userID, LinkedItemID: oldItemID, Status: domain.AccountStatusActive},
		{ID: oldAccountIDs[1], UserID: userID, LinkedItemID: oldItemID, Status: domain.AccountStatusActive},
	}, nil)
	accountRepo.On("UpdateStatus", ctx, mock.Anything, userID, domain.AccountStatusReplaced, mock.Anything).
		Run(func(args mock.Arguments) { order = append(order, "retire:"+args.String(1)) }).Return(nil)
	accountRepo.On("CreateBatch", ctx, mock.Anything, mock.Anything).
		Run(func(mock.Arguments) { order = append(order, "createbatch") }).Return(nil)

	result, err := svc.LinkAccounts(ctx, userID, "public-token", "ins_relink_refusal", "Test Bank")
	require.NoError(t, err)
	require.NotNil(t, result)

	require.Len(t, order, 3, "both predecessors must still be retired, and the batch still written")
	assert.Equal(t, "createbatch", order[2],
		"the retirement must still happen BEFORE CreateBatch: the successor insert needs the "+
			"predecessor's fingerprint released, and supersedeStrandedChainSiblings is the backstop "+
			"for a retirement that failed, not a substitute for one that never ran")
	linkedItemRepo.AssertCalled(t, "Update", ctx, mock.Anything)
}
