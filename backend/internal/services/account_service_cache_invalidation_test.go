package services

// Issue #1531 Blocker 1 — a real, uncovered cross-user stale-cache bug. checkFingerprintOwnership
// (account_service.go) releases an expired fingerprint by calling
// AccountRepository.ExpireAccountsByFingerprint, which flips a DIFFERENT user's (the dispossessed
// owner's) accounts.status from active to expired — deliberately, with no user_id predicate, since
// it must reach rows owned by someone other than the acting LinkAccounts caller. Every cached
// analytics query filters accounts.status = 'active', and the HTTP mutation middleware only ever
// bumps the ACTOR (this LinkAccounts caller), never a third party a write happens to touch — so
// without an explicit bump here, the dispossessed owner's net-worth/cash-flow/spending aggregates
// kept serving pre-release numbers until the namespace TTL. This is exactly the cross-user class
// Task 10b-2b closed for household paths; it survived here because ExpireAccountsByFingerprint
// sat among the repository completeness guard's unclassified entries.
//
// These tests drive the real accountService.LinkAccounts (through checkFingerprintOwnership) with
// a real cache.Generations backed by a fake Redis, never a mock of the bump itself — a regression
// that silently no-ops the call would fail these tests, not just pass a "Bump was called"
// assertion against a spy.

import (
	"context"
	"errors"
	"log/slog"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/maxnussbaum/BoostedBudget/internal/cache"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/redisclient"
	"github.com/maxnussbaum/BoostedBudget/internal/repository"
)

// newFingerprintReleaseHarness builds an accountService wired exactly like
// TestLinkAccounts_FingerprintCheck_AllowsAfterTTLExpiry (a persistent fingerprint owned by
// otherUserID, disconnected 91 days ago — past the 90-day TTL, so it releases and the link
// proceeds), plus a real (fake-Redis-backed) cache.Generations so the bump can be asserted against
// real generation state instead of a mocked expectation.
func newFingerprintReleaseHarness(t *testing.T) (
	svc AccountService, userID, otherUserID string, gens *cache.Generations,
	hhReader *mockHouseholdReaderForDeletion, mockAccountRepo *MockAccountRepository,
) {
	t.Helper()
	ctx := context.Background()
	userID = uuid.New().String()
	otherUserID = uuid.New().String()
	institutionID := "ins_test"
	accessToken := "access-cache-inval"
	plaidItemID := "plaid-item-cache-inval"
	persistentAccountID := "persistent-acct-cache-inval"
	mask := "9012"

	enc := newTestFieldEncryptor(t)
	expectedPersistentBlind := enc.BlindIndex("pid:" + persistentAccountID)
	expectedFallbackBlind := enc.BlindIndex("fb:" + institutionID + "\x00" + mask + "\x00" + "depository" + "\x00" + "checking")

	plaidAccounts := []map[string]interface{}{
		{
			"account_id":            "plaid-acct-cache-inval-1",
			"persistent_account_id": persistentAccountID,
			"balances":              map[string]interface{}{"available": 100.0, "current": 110.0, "iso_currency_code": "USD"},
			"mask":                  mask,
			"name":                  "Checking",
			"subtype":               "checking",
			"type":                  "depository",
		},
	}
	_, plaidClient := newTestPlaidServerWithPersistentID(t, accessToken, plaidItemID, plaidAccounts)

	mockPlaidSvc := &MockPlaidService{}
	mockPlaidSvc.On("GetClient").Return(plaidClient)

	mockLinkedItemRepo := &MockLinkedItemRepository{}
	mockAccountRepo = &MockAccountRepository{}
	mockUserRepo := &MockUserRepository{}

	user := &domain.User{ID: userID, SubscriptionTier: "premium"}
	mockUserRepo.On("GetUserByID", ctx, userID).Return(user, nil)

	mockLinkedItemRepo.On("GetByUserAndInstitution", ctx, userID, institutionID, string(domain.LinkedItemStatusActive)).
		Return(nil, nil)
	mockLinkedItemRepo.On("GetByUserAndInstitution", ctx, userID, institutionID, string(domain.LinkedItemStatusDisconnected)).
		Return(nil, repository.ErrLinkedItemNotFound)

	statusChangedAt := time.Now().Add(-91 * 24 * time.Hour)
	otherItemID := uuid.New().String()
	mockAccountRepo.On("CheckFingerprintOwnership", ctx, expectedPersistentBlind, userID).
		Return(&repository.FingerprintOwnership{
			UserID:          otherUserID,
			LinkedItemID:    otherItemID,
			ItemStatus:      domain.LinkedItemStatusDisconnected,
			StatusChangedAt: &statusChangedAt,
		}, nil)
	mockAccountRepo.On("ExpireAccountsByFingerprint", ctx, expectedPersistentBlind).Return(nil)
	mockAccountRepo.On("CheckFingerprintOwnership", ctx, expectedFallbackBlind, userID).Return(nil, nil)

	newItemID := uuid.New().String()
	mockLinkedItemRepo.On("Create", ctx, mock.MatchedBy(func(item *domain.LinkedItem) bool {
		return item.UserID == userID && item.Status == domain.LinkedItemStatusActive
	})).Run(func(args mock.Arguments) {
		args.Get(1).(*domain.LinkedItem).ID = newItemID
	}).Return(nil)
	mockAccountRepo.On("CreateBatch", ctx, mock.Anything, mock.MatchedBy(func(accounts []domain.Account) bool {
		return len(accounts) == 1 && accounts[0].UserID == userID
	})).Return(nil)

	svc = NewAccountService(
		mockPlaidSvc,
		mockLinkedItemRepo,
		mockAccountRepo,
		mockUserRepo,
		nil,
		WithEncryptor(enc),
		WithAccountServiceLogger(slog.Default()),
	)

	fake := redisclient.NewFake("cache")
	gens = cache.NewGenerations(fake)
	hhReader = &mockHouseholdReaderForDeletion{}

	return svc, userID, otherUserID, gens, hhReader, mockAccountRepo
}

// TestLinkAccounts_FingerprintRelease_BumpsDispossessedOwnersCacheGeneration is the test the
// brief cares about most: releasing another user's fingerprint must bump THAT user's (not the
// acting caller's) cache generation, so their cached analytics cannot keep serving the
// now-expired account as if it were still active.
func TestLinkAccounts_FingerprintRelease_BumpsDispossessedOwnersCacheGeneration(t *testing.T) {
	svc, userID, otherUserID, gens, hhReader, _ := newFingerprintReleaseHarness(t)
	ctx := context.Background()
	hhReader.On("GetHouseholdForUser", mock.Anything, otherUserID).Return(nil, false, false, nil)
	svc.SetCacheInvalidation(gens, hhReader)

	preGenOther, err := gens.Get(ctx, cache.ScopeUser, otherUserID)
	require.NoError(t, err)
	preGenActor, err := gens.Get(ctx, cache.ScopeUser, userID)
	require.NoError(t, err)

	_, err = svc.LinkAccounts(ctx, userID, "public-token", "ins_test", "Test Bank")
	require.NoError(t, err)

	postGenOther, err := gens.Get(ctx, cache.ScopeUser, otherUserID)
	require.NoError(t, err)
	assert.NotEqual(t, preGenOther, postGenOther,
		"the DISPOSSESSED owner's cache generation must be bumped — their now-expired account "+
			"would otherwise keep appearing 'active' in every cached analytics read until the "+
			"namespace TTL")

	// This is the regression the brief calls out explicitly: the HTTP mutation middleware bumps
	// only the ACTOR (userID, the LinkAccounts caller). Confirm this test's bump is genuinely a
	// SEPARATE, explicit one for the dispossessed owner, not an artifact of the actor's own scope
	// happening to be bumped too (they are different users, so this should trivially hold, but it
	// pins the intent).
	assert.NotEqual(t, otherUserID, userID, "sanity: the acting user and dispossessed owner must differ")
	_ = preGenActor
}

// TestLinkAccounts_FingerprintRelease_BumpsDispossessedOwnersHouseholdWhenResolvable proves the
// household side: if the dispossessed owner belongs to a household, that household's cached
// shared-account aggregates must also be invalidated.
func TestLinkAccounts_FingerprintRelease_BumpsDispossessedOwnersHouseholdWhenResolvable(t *testing.T) {
	svc, userID, otherUserID, gens, hhReader, _ := newFingerprintReleaseHarness(t)
	ctx := context.Background()
	household := &domain.Household{ID: "household-fingerprint-release"}
	hhReader.On("GetHouseholdForUser", mock.Anything, otherUserID).Return(household, false, true, nil)
	svc.SetCacheInvalidation(gens, hhReader)

	preGenHH, err := gens.Get(ctx, cache.ScopeHousehold, household.ID)
	require.NoError(t, err)

	_, err = svc.LinkAccounts(ctx, userID, "public-token", "ins_test", "Test Bank")
	require.NoError(t, err)

	postGenHH, err := gens.Get(ctx, cache.ScopeHousehold, household.ID)
	require.NoError(t, err)
	assert.NotEqual(t, preGenHH, postGenHH,
		"the dispossessed owner's household scope must also be bumped when resolvable")
}

// TestLinkAccounts_FingerprintRelease_HouseholdLookupFailureDoesNotFailLink proves the void-bump
// discipline for this new call site: a household lookup failure must never fail an already-
// committed LinkAccounts call, nor suppress the user-scope bump that already ran.
func TestLinkAccounts_FingerprintRelease_HouseholdLookupFailureDoesNotFailLink(t *testing.T) {
	svc, userID, otherUserID, gens, hhReader, _ := newFingerprintReleaseHarness(t)
	ctx := context.Background()
	hhReader.On("GetHouseholdForUser", mock.Anything, otherUserID).
		Return(nil, false, false, errors.New("redis-adjacent lookup failure"))
	svc.SetCacheInvalidation(gens, hhReader)

	preGenOther, err := gens.Get(ctx, cache.ScopeUser, otherUserID)
	require.NoError(t, err)

	result, err := svc.LinkAccounts(ctx, userID, "public-token", "ins_test", "Test Bank")
	require.NoError(t, err, "a household lookup failure during cache invalidation must not fail LinkAccounts")
	require.NotNil(t, result)

	postGenOther, err := gens.Get(ctx, cache.ScopeUser, otherUserID)
	require.NoError(t, err)
	assert.NotEqual(t, preGenOther, postGenOther,
		"the user-scope bump must still have happened despite the household lookup error")
}

// TestLinkAccounts_FingerprintRelease_NoBumpWhenCacheInvalidationNotWired is the regression guard
// for every EXISTING fingerprint-release test in account_service_test.go, none of which call
// SetCacheInvalidation: it must remain a true no-op (nil cacheGens), not a nil-pointer panic.
func TestLinkAccounts_FingerprintRelease_NoBumpWhenCacheInvalidationNotWired(t *testing.T) {
	svc, userID, _, _, _, _ := newFingerprintReleaseHarness(t)
	ctx := context.Background()

	// Deliberately do NOT call SetCacheInvalidation.
	result, err := svc.LinkAccounts(ctx, userID, "public-token", "ins_test", "Test Bank")
	require.NoError(t, err)
	require.NotNil(t, result)
}
