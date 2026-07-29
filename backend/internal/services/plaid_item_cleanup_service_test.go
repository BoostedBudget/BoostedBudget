package services

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/repository"
	"github.com/plaid/plaid-go/v23/plaid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// Mock implementations for testing PlaidItemCleanupService

// MockLinkedItemRepoForCleanup is a mock for LinkedItemRepository
type MockLinkedItemRepoForCleanup struct {
	mock.Mock
}

func (m *MockLinkedItemRepoForCleanup) GetByUserID(ctx context.Context, userID string) ([]domain.LinkedItem, error) {
	args := m.Called(ctx, userID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]domain.LinkedItem), args.Error(1)
}

func (m *MockLinkedItemRepoForCleanup) GetByID(ctx context.Context, id string) (*domain.LinkedItem, error) {
	args := m.Called(ctx, id)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*domain.LinkedItem), args.Error(1)
}

// Issue #679, #680: Trial expiration - methods for disconnecting items
func (m *MockLinkedItemRepoForCleanup) GetActiveItemsByUserID(ctx context.Context, userID string) ([]domain.LinkedItem, error) {
	args := m.Called(ctx, userID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]domain.LinkedItem), args.Error(1)
}

func (m *MockLinkedItemRepoForCleanup) MarkAllUserItemsDisconnected(ctx context.Context, userID string) error {
	args := m.Called(ctx, userID)
	return args.Error(0)
}

// MarkItemsDisconnected persists per-item cleanup outcome (Issue #1413 H2).
func (m *MockLinkedItemRepoForCleanup) MarkItemsDisconnected(ctx context.Context, ids []string) error {
	args := m.Called(ctx, ids)
	return args.Error(0)
}

// MockPlaidServiceForCleanup is a mock for PlaidService
type MockPlaidServiceForCleanup struct {
	mock.Mock
}

func (m *MockPlaidServiceForCleanup) RemoveItem(ctx context.Context, accessToken string) error {
	args := m.Called(ctx, accessToken)
	return args.Error(0)
}

// testPlaidCleanupClaimToken is the fixed fencing token newAllowingPlaidCleanupClaimsRepo's Claim
// stub returns. Tests that only care about the service's OTHER behavior can rely on it
// implicitly; tests exercising the side-effect claim mechanism itself supply their own
// MockWorkClaimsRepository (defined in deletion_service_test.go — same package).
const testPlaidCleanupClaimToken = "test-plaid-disconnect-claim-token"

// newAllowingPlaidCleanupClaimsRepo returns a MockWorkClaimsRepository stubbed to always grant
// and release (never complete — see claimSideEffectAndRun's doc comment for why)
// repository.ActionPlaidDisconnectUser, for tests that exercise RemoveAllUserItems/
// DisconnectAllUserItems's OTHER behavior and aren't themselves testing the claim mechanism.
// userID is wildcarded so one instance works across this file's whole fixture matrix.
func newAllowingPlaidCleanupClaimsRepo() *MockWorkClaimsRepository {
	m := new(MockWorkClaimsRepository)
	m.On("Claim", mock.Anything, repository.ResourceTypeUser, mock.Anything, repository.ActionPlaidDisconnectUser, plaidDisconnectSideEffectLease).
		Return(testPlaidCleanupClaimToken, true, nil).Maybe()
	m.On("Release", mock.Anything, repository.ResourceTypeUser, mock.Anything, repository.ActionPlaidDisconnectUser, testPlaidCleanupClaimToken, mock.AnythingOfType("string")).
		Return(true, nil).Maybe()
	return m
}

// newTestPlaidCleanupService constructs a service with a permissive claims repo wired in, so
// tests exercising RemoveAllUserItems/DisconnectAllUserItems's item-handling logic don't have to
// plumb claim expectations through.
func newTestPlaidCleanupService(repo linkedItemRepoForCleanup, plaidSvc plaidServiceForCleanup) PlaidItemCleanupService {
	svc := NewPlaidItemCleanupService(repo, plaidSvc)
	svc.SetWorkClaimsRepo(newAllowingPlaidCleanupClaimsRepo())
	return svc
}

// plaidItemNotFoundErr builds a plaid.GenericOpenAPIError whose body decodes to Plaid's
// ITEM_NOT_FOUND error — the exact shape plaid.ToPlaidError expects (see
// isPlaidItemAlreadyGone's doc comment for the verified error_code, cross-referenced against
// Plaid's public error documentation).
func plaidItemNotFoundErr() error {
	return plaid.MakeGenericOpenAPIError(
		[]byte(`{"error_type":"ITEM_ERROR","error_code":"ITEM_NOT_FOUND","error_message":"the requested item could not be found"}`),
		"404 Not Found",
		nil,
	)
}

// Test_RemoveAllUserItems_CallsPlaidRemoveForEachItem verifies that
// RemoveAllUserItems calls Plaid /item/remove for each linked item.
// AccessToken is populated by the repository layer (repo-layer encryption).
func Test_RemoveAllUserItems_CallsPlaidRemoveForEachItem(t *testing.T) {
	ctx := context.Background()
	userID := "user-123"

	mockRepo := new(MockLinkedItemRepoForCleanup)
	mockPlaid := new(MockPlaidServiceForCleanup)

	items := []domain.LinkedItem{
		{ID: "item-1", UserID: userID, AccessToken: "decrypted-token-1"},
		{ID: "item-2", UserID: userID, AccessToken: "decrypted-token-2"},
	}
	mockRepo.On("GetByUserID", ctx, userID).Return(items, nil)

	mockPlaid.On("RemoveItem", ctx, "decrypted-token-1").Return(nil)
	mockPlaid.On("RemoveItem", ctx, "decrypted-token-2").Return(nil)
	mockRepo.On("MarkItemsDisconnected", ctx, mock.MatchedBy(func(ids []string) bool {
		return len(ids) == 2 && contains(ids, "item-1") && contains(ids, "item-2")
	})).Return(nil)

	service := newTestPlaidCleanupService(mockRepo, mockPlaid)

	err := service.RemoveAllUserItems(ctx, userID)

	assert.NoError(t, err)
	mockRepo.AssertExpectations(t)
	mockPlaid.AssertExpectations(t)
	mockPlaid.AssertNumberOfCalls(t, "RemoveItem", 2)
}

// Test_RemoveAllUserItems_AggregatesErrorButAttemptsAllItems is a corrected test
// (Issue #1413 finding H6). It previously asserted the OPPOSITE — named
// "...ContinuesOnPlaidError" and asserting assert.NoError — encoding the "graceful
// degradation" behavior that made ExecuteDeletion's abort-on-cleanup-failure dead code:
// RemoveAllUserItems always returned nil, so the caller's `if err != nil { abort }` never
// fired, and the deletion cascade proceeded to clear the local Plaid references, orphaning
// the still-active (still-billing) Plaid Item with nothing left able to retry it.
//
// RemoveAllUserItems's only production caller (DeletionService.ExecuteDeletion) needs a
// genuine per-item failure to abort the deletion, so this function is now strict: it still
// attempts every item (a single bad item must not strand the rest, still billing), but
// aggregates any failures into a non-nil returned error.
func Test_RemoveAllUserItems_AggregatesErrorButAttemptsAllItems(t *testing.T) {
	ctx := context.Background()
	userID := "user-123"

	mockRepo := new(MockLinkedItemRepoForCleanup)
	mockPlaid := new(MockPlaidServiceForCleanup)

	items := []domain.LinkedItem{
		{ID: "item-1", UserID: userID, AccessToken: "decrypted-token-1"},
		{ID: "item-2", UserID: userID, AccessToken: "decrypted-token-2"},
	}
	mockRepo.On("GetByUserID", ctx, userID).Return(items, nil)

	plaidErr := errors.New("plaid API error")
	mockPlaid.On("RemoveItem", ctx, "decrypted-token-1").Return(plaidErr)
	mockPlaid.On("RemoveItem", ctx, "decrypted-token-2").Return(nil)
	// Only item-2 succeeded — only it gets persisted as clean.
	mockRepo.On("MarkItemsDisconnected", ctx, []string{"item-2"}).Return(nil)

	service := newTestPlaidCleanupService(mockRepo, mockPlaid)

	err := service.RemoveAllUserItems(ctx, userID)

	require.Error(t, err)
	assert.ErrorIs(t, err, plaidErr, "the aggregate error must wrap the underlying Plaid failure")
	mockPlaid.AssertNumberOfCalls(t, "RemoveItem", 2)
	mockRepo.AssertExpectations(t)
}

// Test_RemoveAllUserItems_AllItemsFail_AggregatesBothErrors proves the aggregate error
// isn't just "the first failure" — every failed item's error is preserved and every item
// is still attempted, even when none of them succeed.
func Test_RemoveAllUserItems_AllItemsFail_AggregatesBothErrors(t *testing.T) {
	ctx := context.Background()
	userID := "user-123"

	mockRepo := new(MockLinkedItemRepoForCleanup)
	mockPlaid := new(MockPlaidServiceForCleanup)

	items := []domain.LinkedItem{
		{ID: "item-1", UserID: userID, AccessToken: "decrypted-token-1"},
		{ID: "item-2", UserID: userID, AccessToken: "decrypted-token-2"},
	}
	mockRepo.On("GetByUserID", ctx, userID).Return(items, nil)

	err1 := errors.New("plaid API error 1")
	err2 := errors.New("plaid API error 2")
	mockPlaid.On("RemoveItem", ctx, "decrypted-token-1").Return(err1)
	mockPlaid.On("RemoveItem", ctx, "decrypted-token-2").Return(err2)

	service := newTestPlaidCleanupService(mockRepo, mockPlaid)

	err := service.RemoveAllUserItems(ctx, userID)

	require.Error(t, err)
	assert.ErrorIs(t, err, err1)
	assert.ErrorIs(t, err, err2)
	mockPlaid.AssertNumberOfCalls(t, "RemoveItem", 2)
	// Nothing succeeded — MarkItemsDisconnected must never be called.
	mockRepo.AssertNotCalled(t, "MarkItemsDisconnected", mock.Anything, mock.Anything)
}

// Test_RemoveAllUserItems_HandlesNoLinkedItems verifies that
// RemoveAllUserItems succeeds silently when user has no linked items
func Test_RemoveAllUserItems_HandlesNoLinkedItems(t *testing.T) {
	ctx := context.Background()
	userID := "user-123"

	mockRepo := new(MockLinkedItemRepoForCleanup)
	mockPlaid := new(MockPlaidServiceForCleanup)

	mockRepo.On("GetByUserID", ctx, userID).Return([]domain.LinkedItem{}, nil)

	// No items means the side-effect claim is never even attempted — a claims repo of nil
	// would still work here, but wiring the allowing one keeps this test focused on the
	// "no items" behavior rather than claim wiring.
	service := newTestPlaidCleanupService(mockRepo, mockPlaid)

	err := service.RemoveAllUserItems(ctx, userID)

	assert.NoError(t, err)
	mockRepo.AssertExpectations(t)
	mockPlaid.AssertNotCalled(t, "RemoveItem")
}

// Test_RemoveAllUserItems_HandlesRepoError verifies that
// RemoveAllUserItems returns error when repository fails
func Test_RemoveAllUserItems_HandlesRepoError(t *testing.T) {
	ctx := context.Background()
	userID := "user-123"

	mockRepo := new(MockLinkedItemRepoForCleanup)
	mockPlaid := new(MockPlaidServiceForCleanup)

	mockRepo.On("GetByUserID", ctx, userID).Return(nil, errors.New("database error"))

	service := newTestPlaidCleanupService(mockRepo, mockPlaid)

	err := service.RemoveAllUserItems(ctx, userID)

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "database error")
}

// Test_RemoveItem_Success verifies that RemoveItem removes a single Plaid item.
// AccessToken is populated by the repository layer (repo-layer encryption).
func Test_RemoveItem_Success(t *testing.T) {
	ctx := context.Background()
	itemID := "item-123"
	userID := "user-456"

	mockRepo := new(MockLinkedItemRepoForCleanup)
	mockPlaid := new(MockPlaidServiceForCleanup)

	item := &domain.LinkedItem{
		ID:          itemID,
		UserID:      userID,
		AccessToken: "decrypted-token",
	}
	mockRepo.On("GetByID", ctx, itemID).Return(item, nil)

	mockPlaid.On("RemoveItem", ctx, "decrypted-token").Return(nil)

	service := newTestPlaidCleanupService(mockRepo, mockPlaid)

	err := service.RemoveItem(ctx, itemID)

	assert.NoError(t, err)
	mockRepo.AssertExpectations(t)
	mockPlaid.AssertExpectations(t)
}

// Test_RemoveItem_PlaidError verifies that RemoveItem handles Plaid errors gracefully
func Test_RemoveItem_PlaidError(t *testing.T) {
	ctx := context.Background()
	itemID := "item-123"

	mockRepo := new(MockLinkedItemRepoForCleanup)
	mockPlaid := new(MockPlaidServiceForCleanup)

	item := &domain.LinkedItem{
		ID:          itemID,
		AccessToken: "decrypted-token",
	}
	mockRepo.On("GetByID", ctx, itemID).Return(item, nil)

	mockPlaid.On("RemoveItem", ctx, "decrypted-token").Return(errors.New("plaid error"))

	service := newTestPlaidCleanupService(mockRepo, mockPlaid)

	err := service.RemoveItem(ctx, itemID)

	assert.NoError(t, err)
}

// Test_RemoveItem_ItemNotFound verifies that RemoveItem handles missing items
func Test_RemoveItem_ItemNotFound(t *testing.T) {
	ctx := context.Background()
	itemID := "nonexistent-item"

	mockRepo := new(MockLinkedItemRepoForCleanup)
	mockPlaid := new(MockPlaidServiceForCleanup)

	mockRepo.On("GetByID", ctx, itemID).Return(nil, errors.New("not found"))

	service := newTestPlaidCleanupService(mockRepo, mockPlaid)

	err := service.RemoveItem(ctx, itemID)

	assert.Error(t, err)
}

// TestIsItemNotFoundError verifies the IsItemNotFoundError helper function
func TestIsItemNotFoundError(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{
			name: "ErrLinkedItemNotFound",
			err:  repository.ErrLinkedItemNotFound,
			want: true,
		},
		{
			name: "wrapped ErrLinkedItemNotFound",
			err:  fmt.Errorf("operation failed: %w", repository.ErrLinkedItemNotFound),
			want: true,
		},
		{
			name: "different error",
			err:  errors.New("some other error"),
			want: false,
		},
		{
			name: "nil error",
			err:  nil,
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, IsItemNotFoundError(tt.err))
		})
	}
}

// ─── Issue #1413 H1: side-effect claim gating ──────────────────────────────────

// TestPlaidCleanup_SideEffectClaimLost_PlaidNeverCalled proves the H1 gating itself: when
// another flow already holds repository.ActionPlaidDisconnectUser, RemoveAllUserItems must
// return ErrSideEffectClaimBusy WITHOUT calling Plaid at all — not silently proceed as though
// the disconnect had happened.
func TestPlaidCleanup_SideEffectClaimLost_PlaidNeverCalled(t *testing.T) {
	ctx := context.Background()
	userID := "user-busy"

	mockRepo := new(MockLinkedItemRepoForCleanup)
	mockPlaid := new(MockPlaidServiceForCleanup)
	mockClaims := new(MockWorkClaimsRepository)

	items := []domain.LinkedItem{{ID: "item-1", UserID: userID, AccessToken: "tok"}}
	mockRepo.On("GetByUserID", ctx, userID).Return(items, nil)
	mockClaims.On("Claim", ctx, repository.ResourceTypeUser, userID, repository.ActionPlaidDisconnectUser, plaidDisconnectSideEffectLease).
		Return("", false, nil)

	service := NewPlaidItemCleanupService(mockRepo, mockPlaid)
	service.SetWorkClaimsRepo(mockClaims)

	err := service.RemoveAllUserItems(ctx, userID)

	require.Error(t, err)
	assert.ErrorIs(t, err, ErrSideEffectClaimBusy)
	mockPlaid.AssertNotCalled(t, "RemoveItem", mock.Anything, mock.Anything)
	mockRepo.AssertNotCalled(t, "MarkItemsDisconnected", mock.Anything, mock.Anything)
	mockClaims.AssertNotCalled(t, "Release", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything)
}

// TestPlaidCleanup_SideEffectClaimWon_PlaidCalledExactlyOnceThenReleased proves the positive
// direction: a won claim runs the Plaid call exactly once and ALWAYS releases (never completes)
// afterward — see claimSideEffectAndRun's doc comment for why Complete would be wrong here.
func TestPlaidCleanup_SideEffectClaimWon_PlaidCalledExactlyOnceThenReleased(t *testing.T) {
	ctx := context.Background()
	userID := "user-won"

	mockRepo := new(MockLinkedItemRepoForCleanup)
	mockPlaid := new(MockPlaidServiceForCleanup)
	mockClaims := new(MockWorkClaimsRepository)

	items := []domain.LinkedItem{{ID: "item-1", UserID: userID, AccessToken: "tok"}}
	mockRepo.On("GetByUserID", ctx, userID).Return(items, nil)
	mockClaims.On("Claim", ctx, repository.ResourceTypeUser, userID, repository.ActionPlaidDisconnectUser, plaidDisconnectSideEffectLease).
		Return("side-effect-token", true, nil)
	mockPlaid.On("RemoveItem", ctx, "tok").Return(nil)
	mockRepo.On("MarkItemsDisconnected", ctx, []string{"item-1"}).Return(nil)
	mockClaims.On("Release", mock.Anything, repository.ResourceTypeUser, userID, repository.ActionPlaidDisconnectUser, "side-effect-token", mock.AnythingOfType("string")).
		Return(true, nil)

	service := NewPlaidItemCleanupService(mockRepo, mockPlaid)
	service.SetWorkClaimsRepo(mockClaims)

	err := service.RemoveAllUserItems(ctx, userID)

	require.NoError(t, err)
	mockPlaid.AssertNumberOfCalls(t, "RemoveItem", 1)
	mockClaims.AssertCalled(t, "Release", mock.Anything, repository.ResourceTypeUser, userID, repository.ActionPlaidDisconnectUser, "side-effect-token", mock.AnythingOfType("string"))
	mockClaims.AssertNotCalled(t, "Complete", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything)
}

// TestPlaidCleanup_NoClaimsRepoConfigured_FailsLoudly proves this is a required capability, not
// an optional one — mirroring DeletionService/PastDueService's nil-claims-repo behavior.
func TestPlaidCleanup_NoClaimsRepoConfigured_FailsLoudly(t *testing.T) {
	ctx := context.Background()
	userID := "user-no-claims"

	mockRepo := new(MockLinkedItemRepoForCleanup)
	mockPlaid := new(MockPlaidServiceForCleanup)

	items := []domain.LinkedItem{{ID: "item-1", UserID: userID, AccessToken: "tok"}}
	mockRepo.On("GetByUserID", ctx, userID).Return(items, nil)

	service := NewPlaidItemCleanupService(mockRepo, mockPlaid) // SetWorkClaimsRepo never called

	err := service.RemoveAllUserItems(ctx, userID)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "work claims repository not configured")
	mockPlaid.AssertNotCalled(t, "RemoveItem", mock.Anything, mock.Anything)
}

// ─── Issue #1413 H2: provider idempotency and per-item retry-resume ────────────

// TestPlaidCleanup_ItemAlreadyGone_TreatedAsSuccess proves the core H2 fix: Plaid's
// ITEM_NOT_FOUND response for a repeat /item/remove call must be treated as success — no error
// surfaced, and the item persisted as durably clean — not as a failure that would abort a
// deletion or leave the item stuck "outstanding" forever.
func TestPlaidCleanup_ItemAlreadyGone_TreatedAsSuccess(t *testing.T) {
	ctx := context.Background()
	userID := "user-already-gone"

	mockRepo := new(MockLinkedItemRepoForCleanup)
	mockPlaid := new(MockPlaidServiceForCleanup)

	items := []domain.LinkedItem{{ID: "item-1", UserID: userID, AccessToken: "tok"}}
	mockRepo.On("GetByUserID", ctx, userID).Return(items, nil)
	mockPlaid.On("RemoveItem", ctx, "tok").Return(plaidItemNotFoundErr())
	mockRepo.On("MarkItemsDisconnected", ctx, []string{"item-1"}).Return(nil)

	service := newTestPlaidCleanupService(mockRepo, mockPlaid)

	err := service.RemoveAllUserItems(ctx, userID)

	assert.NoError(t, err, "ITEM_NOT_FOUND must be treated as success, not surfaced as an error")
	mockRepo.AssertCalled(t, "MarkItemsDisconnected", ctx, []string{"item-1"})
}

// TestPlaidCleanup_ItemAlreadyGone_DisconnectPath proves the same H2 idempotency mapping applies
// to DisconnectAllUserItems (used by past_due/webhook/trial flows), not just RemoveAllUserItems.
func TestPlaidCleanup_ItemAlreadyGone_DisconnectPath(t *testing.T) {
	ctx := context.Background()
	userID := "user-already-gone-disconnect"

	mockRepo := new(MockLinkedItemRepoForCleanup)
	mockPlaid := new(MockPlaidServiceForCleanup)

	items := []domain.LinkedItem{{ID: "item-1", UserID: userID, AccessToken: "tok"}}
	mockRepo.On("GetActiveItemsByUserID", ctx, userID).Return(items, nil)
	mockPlaid.On("RemoveItem", ctx, "tok").Return(plaidItemNotFoundErr())
	mockRepo.On("MarkItemsDisconnected", ctx, []string{"item-1"}).Return(nil)

	service := newTestPlaidCleanupService(mockRepo, mockPlaid)

	err := service.DisconnectAllUserItems(ctx, userID)

	assert.NoError(t, err)
}

// TestPlaidCleanup_DisconnectAllUserItems_GenuineFailure_AggregatesError proves
// DisconnectAllUserItems is no longer "graceful degradation" (silently swallowing every Plaid
// error and unconditionally marking all items disconnected regardless). A genuine (non-
// ITEM_NOT_FOUND) Plaid failure must be surfaced, and the failed item must NOT be marked
// disconnected — it stays visible to the next GetActiveItemsByUserID call for a retry.
func TestPlaidCleanup_DisconnectAllUserItems_GenuineFailure_AggregatesError(t *testing.T) {
	ctx := context.Background()
	userID := "user-disconnect-fail"

	mockRepo := new(MockLinkedItemRepoForCleanup)
	mockPlaid := new(MockPlaidServiceForCleanup)

	items := []domain.LinkedItem{
		{ID: "item-1", UserID: userID, AccessToken: "tok-1"},
		{ID: "item-2", UserID: userID, AccessToken: "tok-2"},
	}
	mockRepo.On("GetActiveItemsByUserID", ctx, userID).Return(items, nil)

	plaidErr := errors.New("plaid 500")
	mockPlaid.On("RemoveItem", ctx, "tok-1").Return(plaidErr)
	mockPlaid.On("RemoveItem", ctx, "tok-2").Return(nil)
	mockRepo.On("MarkItemsDisconnected", ctx, []string{"item-2"}).Return(nil)

	service := newTestPlaidCleanupService(mockRepo, mockPlaid)

	err := service.DisconnectAllUserItems(ctx, userID)

	require.Error(t, err, "a genuine Plaid failure must now be surfaced, not silently swallowed")
	assert.ErrorIs(t, err, plaidErr)
	// item-1 (the failure) must never be marked disconnected — only item-2 was persisted.
	mockRepo.AssertCalled(t, "MarkItemsDisconnected", ctx, []string{"item-2"})
}

// TestPlaidCleanup_RetryAfterPartialFailure_OnlyOutstandingItemsAttempted is the H2 per-item
// durable-state test: a SECOND cleanup pass (simulating a retry after the first attempt's
// partial failure) must call Plaid ONLY for the item that is still outstanding — the
// already-marked-disconnected item is skipped without a second Plaid call, and would be excluded
// from GetActiveItemsByUserID's real-DB result in production (proven separately at the
// repository level).
func TestPlaidCleanup_RetryAfterPartialFailure_OnlyOutstandingItemsAttempted(t *testing.T) {
	ctx := context.Background()
	userID := "user-retry"

	mockRepo := new(MockLinkedItemRepoForCleanup)
	mockPlaid := new(MockPlaidServiceForCleanup)

	// First pass: item-1 fails, item-2 succeeds.
	firstPassItems := []domain.LinkedItem{
		{ID: "item-1", UserID: userID, AccessToken: "tok-1"},
		{ID: "item-2", UserID: userID, AccessToken: "tok-2"},
	}
	mockRepo.On("GetByUserID", ctx, userID).Return(firstPassItems, nil).Once()

	plaidErr := errors.New("transient plaid error")
	mockPlaid.On("RemoveItem", ctx, "tok-1").Return(plaidErr).Once()
	mockPlaid.On("RemoveItem", ctx, "tok-2").Return(nil).Once()
	mockRepo.On("MarkItemsDisconnected", ctx, []string{"item-2"}).Return(nil).Once()

	service := newTestPlaidCleanupService(mockRepo, mockPlaid)

	err := service.RemoveAllUserItems(ctx, userID)
	require.Error(t, err)

	// Second pass (the "retry"): GetByUserID now reflects the DB state a real retry would see —
	// item-2 already 'disconnected' (from MarkItemsDisconnected above), item-1 still active/
	// outstanding. This mock call stands in for what the REAL repository would return; the
	// repository-level proof that a disconnected item is actually excluded from
	// GetActiveItemsByUserID lives in linked_item_repository_mark_disconnected_integration_test.go.
	secondPassItems := []domain.LinkedItem{
		{ID: "item-1", UserID: userID, AccessToken: "tok-1"}, // still outstanding
		{ID: "item-2", UserID: userID, AccessToken: "tok-2", Status: domain.LinkedItemStatusDisconnected},
	}
	mockRepo.On("GetByUserID", ctx, userID).Return(secondPassItems, nil).Once()
	mockPlaid.On("RemoveItem", ctx, "tok-1").Return(nil).Once()
	mockRepo.On("MarkItemsDisconnected", ctx, []string{"item-1"}).Return(nil).Once()

	err = service.RemoveAllUserItems(ctx, userID)
	require.NoError(t, err)

	// Across BOTH passes, tok-1 was attempted twice (fail, then succeed) but tok-2 was attempted
	// only ONCE (in the first pass) — the retry must not call Plaid again for the already-clean
	// item.
	mockPlaid.AssertNumberOfCalls(t, "RemoveItem", 3)
	mockRepo.AssertExpectations(t)
}

// ─── Issue #1413 round-4 finding C2: item list re-read INSIDE the claim ────────

// TestPlaidCleanup_C2_ItemListReadAfterClaimNotBefore proves the round-4 C2 fix directly: the
// item list must be fetched AFTER the side-effect claim is won, not before. Before this fix,
// RemoveAllUserItems called linkedItemRepo.GetByUserID BEFORE claiming
// repository.ActionPlaidDisconnectUser, so an item created by a concurrent LinkAccounts call in
// the window between that read and the claim would never be seen by this pass and so would
// never be sent to Plaid for removal — orphaning it. Recording call order via .Run callbacks and
// asserting Claim precedes GetByUserID is what makes this test fail on the pre-fix ordering
// (proven separately by temporarily reordering the production code).
func TestPlaidCleanup_C2_ItemListReadAfterClaimNotBefore(t *testing.T) {
	ctx := context.Background()
	userID := "user-c2-reread"

	mockRepo := new(MockLinkedItemRepoForCleanup)
	mockPlaid := new(MockPlaidServiceForCleanup)
	mockClaims := new(MockWorkClaimsRepository)

	var callOrder []string

	mockClaims.On("Claim", ctx, repository.ResourceTypeUser, userID, repository.ActionPlaidDisconnectUser, plaidDisconnectSideEffectLease).
		Run(func(args mock.Arguments) { callOrder = append(callOrder, "Claim") }).
		Return("c2-token", true, nil)

	// This item stands in for one LinkAccounts creates AFTER some earlier reference point (e.g.
	// a snapshot a caller took before claiming) — proving it's still picked up requires the read
	// to happen fresh, INSIDE the claim, not from a stale pre-claim snapshot.
	items := []domain.LinkedItem{{ID: "item-created-late", UserID: userID, AccessToken: "tok-late"}}
	mockRepo.On("GetByUserID", ctx, userID).
		Run(func(args mock.Arguments) { callOrder = append(callOrder, "GetByUserID") }).
		Return(items, nil)

	mockPlaid.On("RemoveItem", ctx, "tok-late").Return(nil)
	mockRepo.On("MarkItemsDisconnected", ctx, []string{"item-created-late"}).Return(nil)
	mockClaims.On("Release", mock.Anything, repository.ResourceTypeUser, userID, repository.ActionPlaidDisconnectUser, "c2-token", mock.AnythingOfType("string")).
		Return(true, nil)

	service := NewPlaidItemCleanupService(mockRepo, mockPlaid)
	service.SetWorkClaimsRepo(mockClaims)

	err := service.RemoveAllUserItems(ctx, userID)

	require.NoError(t, err)
	require.Equal(t, []string{"Claim", "GetByUserID"}, callOrder,
		"the item list must be read AFTER the side-effect claim is won, not before — a stale pre-claim snapshot would miss an item LinkAccounts created in the gap")
	mockPlaid.AssertCalled(t, "RemoveItem", ctx, "tok-late")
}

// contains is a tiny membership helper for mock.MatchedBy predicates in this file.
func contains(ss []string, s string) bool {
	for _, v := range ss {
		if v == s {
			return true
		}
	}
	return false
}
