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

// alwaysEligible is the eligibility recheck for tests whose subject is NOT the recheck. It reports
// "still authorised" unconditionally, which is what these tests were written against when
// RemoveAllUserItems passed a literal nil.
//
// ⚠ Do not reach for this in a test ABOUT authorisation revocation — a recheck that never says no
// cannot detect a missing recheck. Those live in deletion_service_authorisation_revoked_test.go and
// supply a callback that actually flips.
func alwaysEligible(context.Context) (bool, error) { return true, nil }

// Mock implementations for testing PlaidItemCleanupService

// MockLinkedItemRepoForCleanup is a mock for LinkedItemRepository
type MockLinkedItemRepoForCleanup struct {
	mock.Mock

	// Issue #1495 — MarkPlaidRemoved's recording fields. See that method for why it is not driven by
	// testify expectations like every other method on this mock.
	//
	// PlaidRemovedIDs is every id passed to MarkPlaidRemoved, flattened, in call order.
	PlaidRemovedIDs []string
	// MarkPlaidRemovedErr is what MarkPlaidRemoved returns (nil unless a test sets it).
	MarkPlaidRemovedErr error
	// CallLog names the two durable writes in the order they happened, so a test can pin that the
	// plaid_removed_at stamp precedes the 'disconnected' status write.
	CallLog []string

	// Issue #1494 — MarkPlaidRemovalFailed's recording fields, same non-testify rationale as
	// MarkPlaidRemoved's above.
	//
	// PlaidRemovalFailures is every (item id, error code) pair passed to MarkPlaidRemovalFailed, in
	// call order.
	PlaidRemovalFailures []recordedPlaidRemovalFailure
	// MarkPlaidRemovalFailedErr is what MarkPlaidRemovalFailed returns (nil unless a test sets it).
	MarkPlaidRemovalFailedErr error
}

// recordedPlaidRemovalFailure is one MarkPlaidRemovalFailed call as observed by the mock — the pair a
// test needs in order to assert that the verdict was recorded for the RIGHT item with the RIGHT Plaid
// error code, rather than merely that some call happened. Issue #1494.
type recordedPlaidRemovalFailure struct {
	ItemID    string
	ErrorCode string
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
//
// Issue #1495: also appends to CallLog, so a test can assert the ORDER of the two durable writes
// (MarkPlaidRemoved must come FIRST — see the call sites in plaid_item_cleanup_service.go for why
// the reverse order is unrecoverable). testify's mock.Mock records call order internally but exposes
// no assertion for "A before B", hence the explicit log.
func (m *MockLinkedItemRepoForCleanup) MarkItemsDisconnected(ctx context.Context, ids []string) error {
	m.CallLog = append(m.CallLog, "MarkItemsDisconnected")
	args := m.Called(ctx, ids)
	return args.Error(0)
}

// MarkPlaidRemoved records the plaid_removed_at stamp (Issue #1495).
//
// DELIBERATELY NOT a testify expectation-driven method. Every existing test in this file that drives
// a success path would otherwise need a new .On("MarkPlaidRemoved", ...) line purely to avoid
// testify's "mock: I don't know what to return" panic — thirty-odd mechanical edits that would say
// nothing about behaviour. Instead the calls are recorded on the struct (PlaidRemovedIDs, CallLog)
// and the return value is set by the field, so the tests that DO care assert on the recording and
// the rest are untouched.
func (m *MockLinkedItemRepoForCleanup) MarkPlaidRemoved(ctx context.Context, ids []string) error {
	m.CallLog = append(m.CallLog, "MarkPlaidRemoved")
	m.PlaidRemovedIDs = append(m.PlaidRemovedIDs, ids...)
	return m.MarkPlaidRemovedErr
}

// MarkPlaidRemovalFailed records the unrecoverable-removal verdict (Issue #1494). Recorded on the
// struct rather than driven by testify expectations for the SAME reason MarkPlaidRemoved above is:
// every existing failure-path test in this file would otherwise need a new .On(...) line purely to
// avoid a "mock: I don't know what to return" panic, and none of them are about this call.
func (m *MockLinkedItemRepoForCleanup) MarkPlaidRemovalFailed(ctx context.Context, ids []string, errorCode string) error {
	m.CallLog = append(m.CallLog, "MarkPlaidRemovalFailed")
	for _, id := range ids {
		m.PlaidRemovalFailures = append(m.PlaidRemovalFailures, recordedPlaidRemovalFailure{ItemID: id, ErrorCode: errorCode})
	}
	return m.MarkPlaidRemovalFailedErr
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
	svc.SetAccountRepo(permissiveAccountRepoForCleanup{})
	return svc
}

// permissiveAccountRepoForCleanup satisfies the account-repository requirement (Issue #1569 Task
// 20) for the tests in this file, whose subject is the Plaid/claim/status machinery rather than the
// account transition. It records nothing on purpose: the tests that ARE about the transition assert
// on a recording mock, in plaid_item_cleanup_accounts_removed_test.go, and a second recorder here
// would invite an assertion that passes because this stand-in was silently substituted for it.
type permissiveAccountRepoForCleanup struct{}

func (permissiveAccountRepoForCleanup) MarkRemovedByLinkedItemID(context.Context, string, domain.RemovalReason) error {
	return nil
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

// A nil eligibility recheck is REFUSED, not silently accepted as "no recheck wanted".
//
// This is the fail-closed half of the fix for the literal nil this method used to pass to
// cleanupItems. Making the parameter required is worth nothing on its own — a caller can pass nil to
// a required parameter just as easily as it can omit an optional one — so the refusal is what
// actually prevents the defect recurring. It must refuse BEFORE reaching Plaid or the repository,
// because a partially-completed pass with no recheck is exactly what was wrong.
func Test_RemoveAllUserItems_NilRecheckIsRefusedBeforeTouchingPlaid(t *testing.T) {
	ctx := context.Background()
	userID := "user-123"

	mockRepo := new(MockLinkedItemRepoForCleanup)
	mockPlaid := new(MockPlaidServiceForCleanup)
	service := newTestPlaidCleanupService(mockRepo, mockPlaid)

	err := service.RemoveAllUserItems(ctx, userID, nil)

	require.Error(t, err, "a nil recheck on the hard-deletion path is the defect, not a configuration")
	assert.Contains(t, err.Error(), "eligibility recheck is required")
	mockPlaid.AssertNotCalled(t, "RemoveItem", mock.Anything, mock.Anything)
	mockRepo.AssertNotCalled(t, "GetByUserID", mock.Anything, mock.Anything)

	// SIBLING ASSERTION: the refusal is about nil specifically, not about the method being broken.
	// A real callback on the same service, with the same fixtures, still does the work.
	items := []domain.LinkedItem{{ID: "item-1", UserID: userID, AccessToken: "tok-1"}}
	mockRepo.On("GetByUserID", ctx, userID).Return(items, nil)
	mockPlaid.On("RemoveItem", ctx, "tok-1").Return(nil)
	mockRepo.On("MarkPlaidRemoved", ctx, []string{"item-1"}).Return(nil).Maybe()
	mockRepo.On("MarkItemsDisconnected", ctx, []string{"item-1"}).Return(nil)

	require.NoError(t, service.RemoveAllUserItems(ctx, userID, alwaysEligible))
	mockPlaid.AssertCalled(t, "RemoveItem", ctx, "tok-1")
}

// The recheck is actually THREADED INTO THE LOOP, not merely accepted and discarded.
//
// Mutation confirmed this needs its own test: replacing `recheck` with `nil` in the cleanupItems call
// survived every other test in this package, because none of them supplied a callback that ever says
// no. This one does, on the second item, and asserts that the second item's token never reaches
// Plaid.
func Test_RemoveAllUserItems_RecheckIsConsultedBeforeEveryItem(t *testing.T) {
	ctx := context.Background()
	userID := "user-123"

	mockRepo := new(MockLinkedItemRepoForCleanup)
	mockPlaid := new(MockPlaidServiceForCleanup)

	items := []domain.LinkedItem{
		{ID: "item-1", UserID: userID, AccessToken: "tok-1"},
		{ID: "item-2", UserID: userID, AccessToken: "tok-2"},
	}
	mockRepo.On("GetByUserID", ctx, userID).Return(items, nil)
	mockPlaid.On("RemoveItem", ctx, "tok-1").Return(nil)
	// tok-2 is deliberately NOT registered: reaching it is the failure this test detects.
	mockRepo.On("MarkPlaidRemoved", ctx, []string{"item-1"}).Return(nil).Maybe()
	mockRepo.On("MarkItemsDisconnected", ctx, []string{"item-1"}).Return(nil)

	service := newTestPlaidCleanupService(mockRepo, mockPlaid)

	calls := 0
	recheck := func(context.Context) (bool, error) {
		calls++
		return calls == 1, nil // authorised for item 1, revoked before item 2
	}

	err := service.RemoveAllUserItems(ctx, userID, recheck)

	require.ErrorIs(t, err, ErrCleanupAborted,
		"stopping early is a partial completion the caller must be told about, not a silent success")
	assert.Equal(t, 2, calls, "the recheck runs before EVERY remaining item, including the first")
	mockPlaid.AssertCalled(t, "RemoveItem", ctx, "tok-1")
	mockPlaid.AssertNotCalled(t, "RemoveItem", ctx, "tok-2")
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

	err := service.RemoveAllUserItems(ctx, userID, alwaysEligible)

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

	err := service.RemoveAllUserItems(ctx, userID, alwaysEligible)

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

	err := service.RemoveAllUserItems(ctx, userID, alwaysEligible)

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

	err := service.RemoveAllUserItems(ctx, userID, alwaysEligible)

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

	err := service.RemoveAllUserItems(ctx, userID, alwaysEligible)

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
	// #1486 follow-up part A: a successful removal durably records "Plaid billing is stopped" on the row
	// itself, rather than leaning on ItemCleanupWorker.Work's best-effort row Delete. See
	// Test_RemoveItem_MarksItemDisconnectedSoASurvivingRowIsNotReadAsUncleaned for what that
	// marker protects.
	mockRepo.On("MarkItemsDisconnected", ctx, []string{itemID}).Return(nil)

	service := newTestPlaidCleanupService(mockRepo, mockPlaid)

	err := service.RemoveItem(ctx, itemID, alwaysEligible)

	assert.NoError(t, err)
	mockRepo.AssertExpectations(t)
	mockPlaid.AssertExpectations(t)
}

// Test_RemoveItem_GenuinePlaidError_IsPropagatedAndRowIsNotMarkedClean is #1486 follow-up part B's
// failing-first test.
//
// Before the fix RemoveItem logged the Plaid failure and returned nil ("Don't return Plaid errors -
// graceful degradation"), so its only caller — ItemCleanupWorker.Work — believed removal had
// succeeded and hard-deleted the local linked_items row "now that Plaid billing is stopped", which
// was false in exactly this case. DeleteUserCascade's C2 guard
// (repository.uncleanedLinkedItemExistsClause) is an EXISTS over linked_items, so the row's absence
// made the guard pass and a GDPR deletion completed while a live, still-billing Plaid Item survived
// with NO local record it ever existed — verbatim the outcome that guard's own comment says it
// exists to prevent.
//
// This test failed on assert.Error before the fix.
func Test_RemoveItem_GenuinePlaidError_IsPropagatedAndRowIsNotMarkedClean(t *testing.T) {
	ctx := context.Background()
	itemID := "item-123"

	mockRepo := new(MockLinkedItemRepoForCleanup)
	mockPlaid := new(MockPlaidServiceForCleanup)

	item := &domain.LinkedItem{
		ID:          itemID,
		AccessToken: "decrypted-token",
	}
	mockRepo.On("GetByID", ctx, itemID).Return(item, nil)

	// A Plaid outage / rate limit / network failure — NOT ITEM_NOT_FOUND. The Item is still live
	// and still billing.
	mockPlaid.On("RemoveItem", ctx, "decrypted-token").Return(errors.New("plaid error"))

	service := newTestPlaidCleanupService(mockRepo, mockPlaid)

	err := service.RemoveItem(ctx, itemID, alwaysEligible)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "plaid error")
	// Just as load-bearing as the error itself: the row must NOT be marked 'disconnected', because
	// 'disconnected' is exactly the status the C2 guard reads as "Plaid side is done" and the
	// pending-cleanup backstop reads as "nothing left to re-enqueue".
	mockRepo.AssertNotCalled(t, "MarkItemsDisconnected", mock.Anything, mock.Anything)
	mockPlaid.AssertExpectations(t)
}

// Test_RemoveItem_ItemNotFoundOnRetry_ConvergesInsteadOfStranding is #1486 follow-up part A's failing-first
// test, and the reason A is a hard PREREQUISITE for B rather than a nicety.
//
// The scenario needs no Plaid outage at all to become permanent:
//
//  1. ItemCleanupWorker.Work removes the item and clears the schedule, then its best-effort row
//     Delete fails on a transient DB blip. The row survives with cleanup_scheduled_at now NULL, so
//     the daily sweep re-selects and re-enqueues it.
//  2. On the second attempt Plaid answers ITEM_NOT_FOUND — the Item really is gone.
//
// With B's propagation but WITHOUT A, step 2 becomes a returned error: the worker never reaches
// Delete, River exhausts its 5 attempts (ItemCleanupArgs sets no MaxAttempts, so it inherits
// DefaultEnqueueOptions's MaxAttempts=5 — queue/options.go), and the job dead-letters WITH
// cleanup_scheduled_at set.
// cleanupCandidateSelectQuery requires cleanup_scheduled_at IS NULL, so no sweep can ever re-select
// the row, while the C2 guard reads it as un-cleaned — DeleteUserCascade returns (false, nil)
// forever and that user's GDPR erasure never completes. Propagating without this fix would convert
// a self-healing case into a permanent compliance block.
//
// Both halves are asserted: the genuine failure propagates (B), and the ITEM_NOT_FOUND retry
// converges to success AND records 'disconnected' (A).
func Test_RemoveItem_ItemNotFoundOnRetry_ConvergesInsteadOfStranding(t *testing.T) {
	ctx := context.Background()
	itemID := "item-already-gone-at-plaid"

	mockRepo := new(MockLinkedItemRepoForCleanup)
	mockPlaid := new(MockPlaidServiceForCleanup)

	item := &domain.LinkedItem{
		ID:          itemID,
		ItemID:      "plaid-item-abc",
		AccessToken: "decrypted-token",
	}
	mockRepo.On("GetByID", ctx, itemID).Return(item, nil)

	// Attempt 1: a genuine outage. Attempt 2: Plaid has forgotten the Item.
	mockPlaid.On("RemoveItem", ctx, "decrypted-token").Return(errors.New("503 service unavailable")).Once()
	mockPlaid.On("RemoveItem", ctx, "decrypted-token").Return(plaidItemNotFoundErr()).Once()
	mockRepo.On("MarkItemsDisconnected", ctx, []string{itemID}).Return(nil).Once()

	service := newTestPlaidCleanupService(mockRepo, mockPlaid)

	require.Error(t, service.RemoveItem(ctx, itemID, alwaysEligible), "a genuine Plaid failure must propagate (part B)")

	// The retry must be reported as SUCCESS, not as a fifth-attempt dead-letter.
	require.NoError(t, service.RemoveItem(ctx, itemID, alwaysEligible),
		"ITEM_NOT_FOUND means the Item is already gone from Plaid — billing has stopped, so this is a no-op success, not a failure")

	mockRepo.AssertExpectations(t)
	mockPlaid.AssertExpectations(t)
}

// Test_RemoveItem_MarksItemDisconnectedSoASurvivingRowIsNotReadAsUncleaned pins the SECOND half of
// part A: marking the row 'disconnected' on success rather than relying on the worker's
// best-effort Delete.
//
// This closes a strand that exists even with every Plaid call succeeding.
// repository.linkedItemRepository.Delete is a deliberate NO-OP when any accounts row (any status)
// still references the item — it must not cascade-delete historical 'replaced' accounts and their
// balance_snapshots. So on a successful removal of an item that still has accounts, the row ALWAYS
// survives. Without this marker it survives with its pre-removal status ('error'/'active'), which
// uncleanedLinkedItemExistsClause reads as still needing Plaid cleanup — blocking that user's
// DeleteUserCascade forever, even though Plaid billing genuinely stopped. With the marker,
// 'disconnected' is excluded by that clause, by cleanupCandidateSelectQuery, and by
// GetItemsPendingCleanup, so the surviving row is correctly inert on all three paths.
func Test_RemoveItem_MarksItemDisconnectedSoASurvivingRowIsNotReadAsUncleaned(t *testing.T) {
	ctx := context.Background()
	itemID := "item-with-surviving-accounts"

	mockRepo := new(MockLinkedItemRepoForCleanup)
	mockPlaid := new(MockPlaidServiceForCleanup)

	mockRepo.On("GetByID", ctx, itemID).Return(&domain.LinkedItem{
		ID:          itemID,
		Status:      domain.LinkedItemStatusError,
		AccessToken: "decrypted-token",
	}, nil)
	mockPlaid.On("RemoveItem", ctx, "decrypted-token").Return(nil)
	mockRepo.On("MarkItemsDisconnected", ctx, []string{itemID}).Return(nil)

	service := newTestPlaidCleanupService(mockRepo, mockPlaid)

	require.NoError(t, service.RemoveItem(ctx, itemID, alwaysEligible))
	mockRepo.AssertExpectations(t)
}

// Test_RemoveItem_MarkDisconnectedFailure_IsPropagated: losing the marker is NOT best-effort.
// Returning nil here would clear the worker's path to ClearCleanupSchedule + Delete, and a row
// that then survives Delete's accounts guard carries neither the stamp (so no sweep re-selects it)
// nor 'disconnected' (so C2 reads it as un-cleaned) — the permanent-block state again. Propagating
// costs one wasted /item/remove on retry, which ITEM_NOT_FOUND already maps to success.
func Test_RemoveItem_MarkDisconnectedFailure_IsPropagated(t *testing.T) {
	ctx := context.Background()
	itemID := "item-123"

	mockRepo := new(MockLinkedItemRepoForCleanup)
	mockPlaid := new(MockPlaidServiceForCleanup)

	mockRepo.On("GetByID", ctx, itemID).Return(&domain.LinkedItem{ID: itemID, AccessToken: "tok"}, nil)
	mockPlaid.On("RemoveItem", ctx, "tok").Return(nil)
	mockRepo.On("MarkItemsDisconnected", ctx, []string{itemID}).Return(errors.New("db blip"))

	service := newTestPlaidCleanupService(mockRepo, mockPlaid)

	err := service.RemoveItem(ctx, itemID, alwaysEligible)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "db blip")
}

// Test_RemoveItem_MarkDisconnectedZeroRows_IsSuccess: ErrNoLinkedItemsUpdated is an idempotent
// no-op (the row was already 'disconnected'/'replaced'), not a failure — the same distinction
// cleanupItems already draws on this sentinel. Escalating it would turn a converged state into a
// permanent retry loop, since every retry would hit the same zero-row UPDATE.
func Test_RemoveItem_MarkDisconnectedZeroRows_IsSuccess(t *testing.T) {
	ctx := context.Background()
	itemID := "item-already-disconnected"

	mockRepo := new(MockLinkedItemRepoForCleanup)
	mockPlaid := new(MockPlaidServiceForCleanup)

	mockRepo.On("GetByID", ctx, itemID).Return(&domain.LinkedItem{ID: itemID, AccessToken: "tok"}, nil)
	mockPlaid.On("RemoveItem", ctx, "tok").Return(nil)
	mockRepo.On("MarkItemsDisconnected", ctx, []string{itemID}).
		Return(fmt.Errorf("%w: 0 of 1 given id(s) matched", repository.ErrNoLinkedItemsUpdated))

	service := newTestPlaidCleanupService(mockRepo, mockPlaid)

	require.NoError(t, service.RemoveItem(ctx, itemID, alwaysEligible))
}

// Test_RemoveItem_ItemNotFound verifies that RemoveItem handles missing items
func Test_RemoveItem_ItemNotFound(t *testing.T) {
	ctx := context.Background()
	itemID := "nonexistent-item"

	mockRepo := new(MockLinkedItemRepoForCleanup)
	mockPlaid := new(MockPlaidServiceForCleanup)

	mockRepo.On("GetByID", ctx, itemID).Return(nil, errors.New("not found"))

	service := newTestPlaidCleanupService(mockRepo, mockPlaid)

	err := service.RemoveItem(ctx, itemID, alwaysEligible)

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
	service.SetAccountRepo(permissiveAccountRepoForCleanup{})

	err := service.RemoveAllUserItems(ctx, userID, alwaysEligible)

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
	service.SetAccountRepo(permissiveAccountRepoForCleanup{})

	err := service.RemoveAllUserItems(ctx, userID, alwaysEligible)

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

	err := service.RemoveAllUserItems(ctx, userID, alwaysEligible)

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

	err := service.RemoveAllUserItems(ctx, userID, alwaysEligible)

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

	err := service.DisconnectAllUserItemsIfEligible(ctx, userID, alwaysEligible)

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

	err := service.DisconnectAllUserItemsIfEligible(ctx, userID, alwaysEligible)

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

	err := service.RemoveAllUserItems(ctx, userID, alwaysEligible)
	require.Error(t, err)

	// Second pass (the "retry"): GetByUserID now reflects the DB state a real retry would see —
	// item-2 already 'disconnected' (from MarkItemsDisconnected above), item-1 still active/
	// outstanding. This mock call stands in for what the REAL repository would return; the
	// repository-level proof that a disconnected item is actually excluded from
	// GetActiveItemsByUserID lives in linked_item_repository_mark_disconnected_integration_test.go.
	// Deliberately NOT .Once(): Issue #1413 (Codex review, PR #1443 follow-up — H1 residue)
	// cleanupItems now re-fetches once more AFTER the loop, before reporting success, to verify no
	// new item appeared mid-pass (see the H1-residue tests below for the dedicated coverage). This
	// second pass's fetch is therefore called TWICE — once to drive the loop, once to verify — and
	// an unlimited stub lets both calls return the same (fully-settled) item set.
	secondPassItems := []domain.LinkedItem{
		{ID: "item-1", UserID: userID, AccessToken: "tok-1"}, // still outstanding
		{ID: "item-2", UserID: userID, AccessToken: "tok-2", Status: domain.LinkedItemStatusDisconnected},
	}
	mockRepo.On("GetByUserID", ctx, userID).Return(secondPassItems, nil)
	mockPlaid.On("RemoveItem", ctx, "tok-1").Return(nil).Once()
	mockRepo.On("MarkItemsDisconnected", ctx, []string{"item-1"}).Return(nil).Once()

	err = service.RemoveAllUserItems(ctx, userID, alwaysEligible)
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
	service.SetAccountRepo(permissiveAccountRepoForCleanup{})

	err := service.RemoveAllUserItems(ctx, userID, alwaysEligible)

	require.NoError(t, err)
	// GetByUserID appears twice: once to drive the loop, once more for cleanupItems's post-loop
	// re-verification (Issue #1413 H1 residue, Codex review PR #1443 follow-up) that no new item
	// appeared mid-pass — see the dedicated H1-residue tests below. Both calls happen after Claim.
	require.Equal(t, []string{"Claim", "GetByUserID", "GetByUserID"}, callOrder,
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

// ─── Issue #1413 round-7: per-item eligibility recheck (DisconnectAllUserItemsIfEligible) ─────
//
// The retention-cleanup Plaid-disconnect repair pass re-verifies a user is still read-only
// immediately before removing their Plaid items. Before round-7, that re-verification happened
// ONCE, before the whole per-user DisconnectAllUserItems call — but a disconnect fans out into one
// Plaid API call per linked item, so a user who resubscribes mid-loop (between item 1 and item 2)
// would still have every remaining item removed. DisconnectAllUserItemsIfEligible closes this by
// re-invoking a caller-supplied recheck callback immediately before EACH item, aborting the
// remainder the first time it reports ineligible.

// TestPlaidCleanup_DisconnectIfEligible_NilRecheck_IsRefused inverts what this test used to assert.
// It read "a nil recheck reproduces DisconnectAllUserItems' existing behavior exactly" — which was
// true, and was the problem: it pinned "no per-item recheck" as a supported, silent default. Issue
// #1500 deleted the nil-recheck twin and made a nil here a REFUSAL, so the assertion that used to
// prove the old behaviour now proves it is gone.
//
// Nothing may be touched before the refusal: no repository read, no Plaid call. Both mocks are left
// entirely unprogrammed, so any contact with either would surface as a testify panic rather than a
// quiet pass.
func TestPlaidCleanup_DisconnectIfEligible_NilRecheck_IsRefused(t *testing.T) {
	ctx := context.Background()
	userID := "user-nil-recheck"

	mockRepo := new(MockLinkedItemRepoForCleanup)
	mockPlaid := new(MockPlaidServiceForCleanup)

	service := newTestPlaidCleanupService(mockRepo, mockPlaid)

	err := service.DisconnectAllUserItemsIfEligible(ctx, userID, nil)

	require.Error(t, err, "a nil recheck is the defect this method refuses, not a configuration meaning 'no recheck'")
	assert.Contains(t, err.Error(), "eligibility recheck is required")
	mockRepo.AssertNotCalled(t, "GetActiveItemsByUserID", mock.Anything, mock.Anything)
	mockPlaid.AssertNotCalled(t, "RemoveItem", mock.Anything, mock.Anything)
}

// TestPlaidCleanup_RemoveAllUserItems_NilRecheck_IsRefused is the sibling refusal on the other
// public entry point. Without it, "nil is refused" is proven for one of the two methods that share
// cleanupItems and merely assumed for the other.
func TestPlaidCleanup_RemoveAllUserItems_NilRecheck_IsRefused(t *testing.T) {
	ctx := context.Background()

	mockRepo := new(MockLinkedItemRepoForCleanup)
	mockPlaid := new(MockPlaidServiceForCleanup)

	service := newTestPlaidCleanupService(mockRepo, mockPlaid)

	err := service.RemoveAllUserItems(ctx, "user-nil-recheck-remove-all", nil)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "eligibility recheck is required")
	mockRepo.AssertNotCalled(t, "GetByUserID", mock.Anything, mock.Anything)
	mockPlaid.AssertNotCalled(t, "RemoveItem", mock.Anything, mock.Anything)
}

// TestPlaidCleanup_DisconnectIfEligible_BecomesIneligibleMidLoop_RemainingItemsSpared is the round-7
// RED test: a user who becomes ineligible (e.g. resubscribed) between item 1 and item 2 must have
// item 2 spared — proving the recheck runs PER ITEM, not once for the whole call.
//
// Issue #1413 round-14 H1 (Codex review): this test previously asserted require.NoError(t, err) —
// encoding the exact defect H1 fixes. A nil return here is indistinguishable, to a caller, from
// "every item was attempted and none remained": repairPlaidDisconnects (retention_cleanup_check.go)
// treated that nil as full completion and cleared plaid_cleanup_pending_at, permanently hiding a
// user with item-2 still live from the only code path that ever disconnects them. The function must
// now return ErrCleanupAborted instead — still not a failure (no Warn log, no failure counter), but
// no longer indistinguishable from "nothing left to do".
//
// Falsifiability: on a version of cleanupItems that only rechecks once before the loop (or not at
// all), this test fails on the RemoveItem call-count assertions below — mockPlaid.
// AssertNumberOfCalls("RemoveItem", 1) would see 2 calls instead. On today's (pre-H1-fix) code, this
// test fails on the ErrorIs assertion — err is nil, not ErrCleanupAborted.
func TestPlaidCleanup_DisconnectIfEligible_BecomesIneligibleMidLoop_RemainingItemsSpared(t *testing.T) {
	ctx := context.Background()
	userID := "user-becomes-ineligible"

	mockRepo := new(MockLinkedItemRepoForCleanup)
	mockPlaid := new(MockPlaidServiceForCleanup)

	items := []domain.LinkedItem{
		{ID: "item-1", UserID: userID, AccessToken: "tok-1"},
		{ID: "item-2", UserID: userID, AccessToken: "tok-2"},
	}
	mockRepo.On("GetActiveItemsByUserID", ctx, userID).Return(items, nil)
	mockPlaid.On("RemoveItem", ctx, "tok-1").Return(nil)
	mockRepo.On("MarkItemsDisconnected", ctx, []string{"item-1"}).Return(nil)

	callCount := 0
	recheck := func(context.Context) (bool, error) {
		callCount++
		// Eligible for item 1, ineligible by the time item 2 is reached.
		return callCount == 1, nil
	}

	service := newTestPlaidCleanupService(mockRepo, mockPlaid)

	err := service.DisconnectAllUserItemsIfEligible(ctx, userID, recheck)

	require.Error(t, err, "a mid-loop abort is a legitimate partial completion, but must be surfaced — not returned as nil, which a caller could mistake for full completion")
	assert.ErrorIs(t, err, ErrCleanupAborted)
	mockPlaid.AssertNumberOfCalls(t, "RemoveItem", 1)
	mockPlaid.AssertNotCalled(t, "RemoveItem", ctx, "tok-2")
	assert.Equal(t, 2, callCount, "recheck must be invoked before EACH item, not once for the whole call")
	// item-1 succeeded before the abort and must still be durably marked disconnected.
	mockRepo.AssertCalled(t, "MarkItemsDisconnected", ctx, []string{"item-1"})
}

// TestPlaidCleanup_DisconnectIfEligible_IneligibleBeforeFirstItem_NoPlaidCallAtAll proves the
// recheck runs before the FIRST item too (not just between items 2..N) — a user already ineligible
// by the time the loop starts must have zero Plaid calls.
//
// Issue #1413 round-14 H1: this must also return ErrCleanupAborted, not nil — see
// TestPlaidCleanup_DisconnectIfEligible_BecomesIneligibleMidLoop_RemainingItemsSpared's doc comment
// for why a nil return here would be indistinguishable from full completion to a caller.
func TestPlaidCleanup_DisconnectIfEligible_IneligibleBeforeFirstItem_NoPlaidCallAtAll(t *testing.T) {
	ctx := context.Background()
	userID := "user-ineligible-from-start"

	mockRepo := new(MockLinkedItemRepoForCleanup)
	mockPlaid := new(MockPlaidServiceForCleanup)

	items := []domain.LinkedItem{{ID: "item-1", UserID: userID, AccessToken: "tok-1"}}
	mockRepo.On("GetActiveItemsByUserID", ctx, userID).Return(items, nil)

	recheck := func(context.Context) (bool, error) { return false, nil }

	service := newTestPlaidCleanupService(mockRepo, mockPlaid)

	err := service.DisconnectAllUserItemsIfEligible(ctx, userID, recheck)

	require.Error(t, err)
	assert.ErrorIs(t, err, ErrCleanupAborted)
	mockPlaid.AssertNotCalled(t, "RemoveItem", mock.Anything, mock.Anything)
	mockRepo.AssertNotCalled(t, "MarkItemsDisconnected", mock.Anything, mock.Anything)
}

// TestPlaidCleanup_DisconnectIfEligible_RecheckErrors_FailsClosedAbortsRemainder proves the recheck
// erroring (eligibility could not be determined at all) fails closed: the remainder is aborted and
// the error is surfaced, rather than proceeding as though eligible.
func TestPlaidCleanup_DisconnectIfEligible_RecheckErrors_FailsClosedAbortsRemainder(t *testing.T) {
	ctx := context.Background()
	userID := "user-recheck-errors"

	mockRepo := new(MockLinkedItemRepoForCleanup)
	mockPlaid := new(MockPlaidServiceForCleanup)

	items := []domain.LinkedItem{{ID: "item-1", UserID: userID, AccessToken: "tok-1"}}
	mockRepo.On("GetActiveItemsByUserID", ctx, userID).Return(items, nil)

	recheckErr := errors.New("db unavailable")
	recheck := func(context.Context) (bool, error) { return false, recheckErr }

	service := newTestPlaidCleanupService(mockRepo, mockPlaid)

	err := service.DisconnectAllUserItemsIfEligible(ctx, userID, recheck)

	require.Error(t, err)
	assert.ErrorIs(t, err, recheckErr)
	mockPlaid.AssertNotCalled(t, "RemoveItem", mock.Anything, mock.Anything)
}

// ─── Issue #1413 H1 residue (Codex review, PR #1443 follow-up) ────────────────
//
// Round-14's ErrCleanupAborted fix (above) closed the mid-loop-recheck-abort case: abortedCount is
// computed over the items slice fetched at the top of cleanupItems, so a recheck-triggered
// `break itemLoop` correctly returns ErrCleanupAborted, not nil. But abortedCount is STILL computed
// only over that same originally-fetched slice — an item created by a path that does not
// participate in this call's side-effect claim (LinkAccounts, account_service.go:~413 — the only
// production inserter into linked_items; confirmed by an audit of every repository.
// LinkedItemRepository consumer) AFTER the fetch is invisible to the whole function. A loop that
// successfully attempts (or correctly skips, as already-disconnected) every item it DID see still
// reports nil — indistinguishable, to repairPlaidDisconnects, from "nothing left to do" — and the
// worker clears plaid_cleanup_pending_at, the only marker that makes a read-only user a repair
// candidate again, permanently hiding the untouched new item.
//
// The fix: cleanupItems now re-fetches once more, AFTER the loop (or the "no items" branch)
// finishes, before ever reporting nil. Every item from the ORIGINAL fetch has, by that point,
// either been durably marked 'disconnected' (a fresh success) or already carried that status
// (skipped) — so any item this second fetch returns whose ID was NOT in the original fetch must be
// one this pass never attempted. Reuses ErrCleanupAborted (not a new sentinel): every existing
// caller already treats it generically as "not actually done, do not clear the marker, this is
// benign and expected" — repairPlaidDisconnects's errors.Is(disconnectErr, ErrCleanupAborted)
// branch does not distinguish WHY the loop didn't cover every item, and RemoveAllUserItems/
// DisconnectAllUserItems's callers (deletion, past-due dunning, the cancellation webhook) already
// treat any non-ErrSideEffectClaimBusy error as "abort this attempt, retry next run" — exactly the
// safe behavior for a newly-appeared live item this pass never touched.

// TestPlaidCleanup_RemoveAllUserItems_NewItemAppearsAfterFetch_ReturnsAbortedNotNil is the
// RemoveAllUserItems-path RED test for the H1 residue.
//
// Falsifiability: on cleanupItems as it existed before this fix (single fetch, no post-loop
// re-verification), this test fails on the ErrorIs assertion below — err is nil, not
// ErrCleanupAborted — even though item-2 (returned only by the SECOND GetByUserID stub, standing in
// for a row LinkAccounts inserted after this pass's initial fetch) was never attempted.
func TestPlaidCleanup_RemoveAllUserItems_NewItemAppearsAfterFetch_ReturnsAbortedNotNil(t *testing.T) {
	ctx := context.Background()
	userID := "user-new-item-appears"

	mockRepo := new(MockLinkedItemRepoForCleanup)
	mockPlaid := new(MockPlaidServiceForCleanup)

	initialItems := []domain.LinkedItem{{ID: "item-1", UserID: userID, AccessToken: "tok-1"}}
	mockRepo.On("GetByUserID", ctx, userID).Return(initialItems, nil).Once()
	mockPlaid.On("RemoveItem", ctx, "tok-1").Return(nil)
	mockRepo.On("MarkItemsDisconnected", ctx, []string{"item-1"}).Return(nil)

	// Simulates the state a concurrent LinkAccounts call left behind by the time cleanupItems
	// re-fetches: item-1 now disconnected (just marked above), plus a brand-new item-2 this pass's
	// original fetch never saw.
	postLoopItems := []domain.LinkedItem{
		{ID: "item-1", UserID: userID, AccessToken: "tok-1", Status: domain.LinkedItemStatusDisconnected},
		{ID: "item-2", UserID: userID, AccessToken: "tok-2"},
	}
	mockRepo.On("GetByUserID", ctx, userID).Return(postLoopItems, nil).Once()

	service := newTestPlaidCleanupService(mockRepo, mockPlaid)

	err := service.RemoveAllUserItems(ctx, userID, alwaysEligible)

	require.Error(t, err, "a live item created after the initial fetch must not be silently reported as full completion")
	assert.ErrorIs(t, err, ErrCleanupAborted)
	mockPlaid.AssertNotCalled(t, "RemoveItem", ctx, "tok-2")
}

// TestPlaidCleanup_DisconnectIfEligible_NewItemAppearsAfterLoopCompletes_ReturnsAbortedNotNil is the
// repair-pass-shaped RED test for the H1 residue: this is the exact concrete scenario the finding
// describes — a comp granted and removed inside one repairPlaidDisconnects iteration lets
// LinkAccounts insert a new item this pass's fetch never saw, even though recheck reports the user
// eligible for every item it DID attempt.
func TestPlaidCleanup_DisconnectIfEligible_NewItemAppearsAfterLoopCompletes_ReturnsAbortedNotNil(t *testing.T) {
	ctx := context.Background()
	userID := "user-new-item-if-eligible"

	mockRepo := new(MockLinkedItemRepoForCleanup)
	mockPlaid := new(MockPlaidServiceForCleanup)

	initialItems := []domain.LinkedItem{{ID: "item-1", UserID: userID, AccessToken: "tok-1"}}
	mockRepo.On("GetActiveItemsByUserID", ctx, userID).Return(initialItems, nil).Once()
	mockPlaid.On("RemoveItem", ctx, "tok-1").Return(nil)
	mockRepo.On("MarkItemsDisconnected", ctx, []string{"item-1"}).Return(nil)

	postLoopItems := []domain.LinkedItem{
		{ID: "item-2", UserID: userID, AccessToken: "tok-2"},
	}
	mockRepo.On("GetActiveItemsByUserID", ctx, userID).Return(postLoopItems, nil).Once()

	recheck := func(context.Context) (bool, error) { return true, nil }

	service := newTestPlaidCleanupService(mockRepo, mockPlaid)

	err := service.DisconnectAllUserItemsIfEligible(ctx, userID, recheck)

	require.Error(t, err, "the repair pass's marker must not be cleared while a newly-appeared item is still live")
	assert.ErrorIs(t, err, ErrCleanupAborted)
	mockPlaid.AssertNotCalled(t, "RemoveItem", ctx, "tok-2")
}

// TestPlaidCleanup_NoItemsInitially_NewItemAppearsBeforeVerify_ReturnsAbortedNotNil closes the
// narrowest form of the same race: the initial fetch finds nothing (the "no linked items" branch),
// but a new item appears before the post-loop verification runs. This must not report nil either.
func TestPlaidCleanup_NoItemsInitially_NewItemAppearsBeforeVerify_ReturnsAbortedNotNil(t *testing.T) {
	ctx := context.Background()
	userID := "user-empty-then-new-item"

	mockRepo := new(MockLinkedItemRepoForCleanup)
	mockPlaid := new(MockPlaidServiceForCleanup)

	mockRepo.On("GetByUserID", ctx, userID).Return([]domain.LinkedItem{}, nil).Once()
	postLoopItems := []domain.LinkedItem{{ID: "item-new", UserID: userID, AccessToken: "tok-new"}}
	mockRepo.On("GetByUserID", ctx, userID).Return(postLoopItems, nil).Once()

	service := newTestPlaidCleanupService(mockRepo, mockPlaid)

	err := service.RemoveAllUserItems(ctx, userID, alwaysEligible)

	require.Error(t, err)
	assert.ErrorIs(t, err, ErrCleanupAborted)
	mockPlaid.AssertNotCalled(t, "RemoveItem")
}

// TestPlaidCleanup_GenuineFailureTakesPriority_NoRefetchAttempted proves the existing precedence
// rule still holds with the post-loop check added: a genuine per-item failure must be reported as
// itself, and cleanupItems must not even attempt the post-loop re-verification fetch in that case.
func TestPlaidCleanup_GenuineFailureTakesPriority_NoRefetchAttempted(t *testing.T) {
	ctx := context.Background()
	userID := "user-genuine-failure-priority"

	mockRepo := new(MockLinkedItemRepoForCleanup)
	mockPlaid := new(MockPlaidServiceForCleanup)

	items := []domain.LinkedItem{{ID: "item-1", UserID: userID, AccessToken: "tok-1"}}
	// .Once() is deliberate: if cleanupItems regressed to ALWAYS re-fetching — even after a genuine
	// per-item failure, which must return immediately per the existing priority rule — this stub
	// would already be exhausted by the initial fetch, and the unexpected second call would panic
	// ("mock: I don't know what to return"), failing this test loudly rather than silently passing.
	mockRepo.On("GetByUserID", ctx, userID).Return(items, nil).Once()

	plaidErr := errors.New("plaid API error")
	mockPlaid.On("RemoveItem", ctx, "tok-1").Return(plaidErr)

	service := newTestPlaidCleanupService(mockRepo, mockPlaid)

	err := service.RemoveAllUserItems(ctx, userID, alwaysEligible)

	require.Error(t, err)
	assert.ErrorIs(t, err, plaidErr, "a genuine per-item failure must still take priority over the new-item check")
	assert.False(t, errors.Is(err, ErrCleanupAborted), "a genuine failure must be reported as itself, not conflated with the benign new-item-appeared signal")
}

// TestPlaidCleanup_VerifyRefetchFails_ReturnsError proves a failure of the post-loop verification
// fetch itself (a genuine infrastructure error, not "new item found") is surfaced as a real error —
// not silently swallowed into a nil "success".
func TestPlaidCleanup_VerifyRefetchFails_ReturnsError(t *testing.T) {
	ctx := context.Background()
	userID := "user-refetch-fails"

	mockRepo := new(MockLinkedItemRepoForCleanup)
	mockPlaid := new(MockPlaidServiceForCleanup)

	items := []domain.LinkedItem{{ID: "item-1", UserID: userID, AccessToken: "tok-1"}}
	mockRepo.On("GetByUserID", ctx, userID).Return(items, nil).Once()
	mockPlaid.On("RemoveItem", ctx, "tok-1").Return(nil)
	mockRepo.On("MarkItemsDisconnected", ctx, []string{"item-1"}).Return(nil)

	refetchErr := errors.New("database unavailable")
	mockRepo.On("GetByUserID", ctx, userID).Return(nil, refetchErr).Once()

	service := newTestPlaidCleanupService(mockRepo, mockPlaid)

	err := service.RemoveAllUserItems(ctx, userID, alwaysEligible)

	require.Error(t, err)
	assert.ErrorIs(t, err, refetchErr)
	assert.False(t, errors.Is(err, ErrCleanupAborted))
}

// ─── Issue #1413 #1453 (Codex review, PR #1443 follow-up): status-aware new-item check ────────
//
// verifyNoNewLinkedItemAppeared's post-loop re-fetch (added for the H1 residue above) used to
// compare IDs alone: any ID absent from the original fetch tripped ErrCleanupAborted, regardless
// of that item's CURRENT status. But RemoveAllUserItems's underlying GetByUserID fetch returns
// items in ANY status, so the re-fetch can legitimately surface an item that was created AND
// ALREADY disconnected/replaced by some other path before the re-fetch ran — phantom residue
// (#1453), not a leak. Falsely aborting on that residue matters more than its severity label
// suggests: DeletionService aborts an ENTIRE GDPR deletion on any non-nil error from
// RemoveAllUserItems, so a false positive here is a compliance failure, not mere untidiness.

// TestPlaidCleanup_RemoveAllUserItems_NewItemAppearsAlreadyDisconnected_NotAborted proves a new ID
// whose CURRENT status is already 'disconnected' does not trip ErrCleanupAborted.
//
// Falsifiability: on a version of verifyNoNewLinkedItemAppeared that compares IDs alone (pre-#1453
// fix), this test fails — err would be ErrCleanupAborted, not nil.
func TestPlaidCleanup_RemoveAllUserItems_NewItemAppearsAlreadyDisconnected_NotAborted(t *testing.T) {
	ctx := context.Background()
	userID := "user-new-item-already-disconnected"

	mockRepo := new(MockLinkedItemRepoForCleanup)
	mockPlaid := new(MockPlaidServiceForCleanup)

	initialItems := []domain.LinkedItem{{ID: "item-1", UserID: userID, AccessToken: "tok-1"}}
	mockRepo.On("GetByUserID", ctx, userID).Return(initialItems, nil).Once()
	mockPlaid.On("RemoveItem", ctx, "tok-1").Return(nil)
	mockRepo.On("MarkItemsDisconnected", ctx, []string{"item-1"}).Return(nil)

	// item-phantom is a NEW id (never in the original fetch) — but it is ALREADY 'disconnected' by
	// the time the post-loop re-fetch runs, e.g. disconnected by some other path in the window
	// between the initial fetch and this re-fetch. No live item was ever missed.
	postLoopItems := []domain.LinkedItem{
		{ID: "item-1", UserID: userID, AccessToken: "tok-1", Status: domain.LinkedItemStatusDisconnected},
		{ID: "item-phantom", UserID: userID, AccessToken: "tok-phantom", Status: domain.LinkedItemStatusDisconnected},
	}
	mockRepo.On("GetByUserID", ctx, userID).Return(postLoopItems, nil).Once()

	service := newTestPlaidCleanupService(mockRepo, mockPlaid)

	err := service.RemoveAllUserItems(ctx, userID, alwaysEligible)

	require.NoError(t, err, "a NEW item ID that is already disconnected must not trip ErrCleanupAborted — no live item was ever missed")
	mockPlaid.AssertNotCalled(t, "RemoveItem", ctx, "tok-phantom")
}

// TestPlaidCleanup_RemoveAllUserItems_NewItemAppearsAlreadyReplaced_NotAborted is the 'replaced'
// sibling of the test above — the liveness definition excludes both terminal statuses.
func TestPlaidCleanup_RemoveAllUserItems_NewItemAppearsAlreadyReplaced_NotAborted(t *testing.T) {
	ctx := context.Background()
	userID := "user-new-item-already-replaced"

	mockRepo := new(MockLinkedItemRepoForCleanup)
	mockPlaid := new(MockPlaidServiceForCleanup)

	initialItems := []domain.LinkedItem{{ID: "item-1", UserID: userID, AccessToken: "tok-1"}}
	mockRepo.On("GetByUserID", ctx, userID).Return(initialItems, nil).Once()
	mockPlaid.On("RemoveItem", ctx, "tok-1").Return(nil)
	mockRepo.On("MarkItemsDisconnected", ctx, []string{"item-1"}).Return(nil)

	postLoopItems := []domain.LinkedItem{
		{ID: "item-1", UserID: userID, AccessToken: "tok-1", Status: domain.LinkedItemStatusDisconnected},
		{ID: "item-phantom", UserID: userID, AccessToken: "tok-phantom", Status: domain.LinkedItemStatusReplaced},
	}
	mockRepo.On("GetByUserID", ctx, userID).Return(postLoopItems, nil).Once()

	service := newTestPlaidCleanupService(mockRepo, mockPlaid)

	err := service.RemoveAllUserItems(ctx, userID, alwaysEligible)

	require.NoError(t, err, "a NEW item ID that is already replaced must not trip ErrCleanupAborted — no live item was ever missed")
	mockPlaid.AssertNotCalled(t, "RemoveItem", ctx, "tok-phantom")
}

// TestPlaidCleanup_RemoveAllUserItems_NewItemAppearsStillLive_StillAborted is the positive control:
// preserving what the previous round got right. A genuinely LIVE new item must still abort — this
// is TestPlaidCleanup_RemoveAllUserItems_NewItemAppearsAfterFetch_ReturnsAbortedNotNil's exact
// scenario, restated here (with an explicit non-terminal status) directly alongside the new
// not-live tests above so the contrast between the two outcomes is unambiguous in one place.
func TestPlaidCleanup_RemoveAllUserItems_NewItemAppearsStillLive_StillAborted(t *testing.T) {
	ctx := context.Background()
	userID := "user-new-item-still-live"

	mockRepo := new(MockLinkedItemRepoForCleanup)
	mockPlaid := new(MockPlaidServiceForCleanup)

	initialItems := []domain.LinkedItem{{ID: "item-1", UserID: userID, AccessToken: "tok-1"}}
	mockRepo.On("GetByUserID", ctx, userID).Return(initialItems, nil).Once()
	mockPlaid.On("RemoveItem", ctx, "tok-1").Return(nil)
	mockRepo.On("MarkItemsDisconnected", ctx, []string{"item-1"}).Return(nil)

	postLoopItems := []domain.LinkedItem{
		{ID: "item-1", UserID: userID, AccessToken: "tok-1", Status: domain.LinkedItemStatusDisconnected},
		{ID: "item-2-live", UserID: userID, AccessToken: "tok-live", Status: domain.LinkedItemStatusActive},
	}
	mockRepo.On("GetByUserID", ctx, userID).Return(postLoopItems, nil).Once()

	service := newTestPlaidCleanupService(mockRepo, mockPlaid)

	err := service.RemoveAllUserItems(ctx, userID, alwaysEligible)

	require.Error(t, err, "a genuinely LIVE new item must still abort — status-awareness must not weaken this")
	assert.ErrorIs(t, err, ErrCleanupAborted)
	mockPlaid.AssertNotCalled(t, "RemoveItem", ctx, "tok-live")
}

// TestPlaidCleanup_DisconnectIfEligible_NewItemAppearsButNotLive_NotAborted was DELETED (Issue #1413
// Finding 2, Codex review, PR #1443 follow-up, not renamed — see this task's report) rather than
// fixed: it stubbed GetActiveItemsByUserID's post-loop re-fetch to return an item with status
// domain.LinkedItemStatusReplaced, a state that repository method's own SQL can never actually
// produce. GetActiveItemsByUserID's query (linked_item_repository.go) is
// `WHERE user_id = $1 AND (status IS NULL OR status NOT IN ('disconnected', 'replaced'))` — it
// excludes 'disconnected'/'replaced' rows at the DATABASE level, so EVERY row a real call can ever
// return already satisfies isLiveLinkedItemStatus (plaid_item_cleanup_service.go). That makes
// verifyNoNewLinkedItemAppeared's "new ID, but not live, so not aborted" branch unreachable for this
// specific caller (DisconnectAllUserItemsIfEligible, whose fetch IS GetActiveItemsByUserID) — the
// mock was modeling a return value production code cannot produce, so the test did not prove what
// its name claimed about the repair path.
//
// The intended coverage — "a new item ID that already carries a terminal status must not trip
// ErrCleanupAborted" — is NOT missing: it is already reachably covered by
// TestPlaidCleanup_RemoveAllUserItems_NewItemAppearsAlreadyDisconnected_NotAborted and
// TestPlaidCleanup_RemoveAllUserItems_NewItemAppearsAlreadyReplaced_NotAborted above, whose fetch
// (GetByUserID) genuinely has no status filter and CAN return a disconnected/replaced row post-loop.
// The repair-pass path's OWN positive control for a genuinely live new item — the state
// GetActiveItemsByUserID CAN produce — is
// TestPlaidCleanup_DisconnectIfEligible_NewItemAppearsAfterLoopCompletes_ReturnsAbortedNotNil above,
// left untouched.

// TestPlaidCleanup_NoItemsInitially_NewItemAppearsButNotLive_NotAborted closes the narrowest form:
// the initial fetch finds nothing (the "no linked items" branch), and the item that appears before
// the post-loop verification is not live either.
func TestPlaidCleanup_NoItemsInitially_NewItemAppearsButNotLive_NotAborted(t *testing.T) {
	ctx := context.Background()
	userID := "user-empty-then-new-item-not-live"

	mockRepo := new(MockLinkedItemRepoForCleanup)
	mockPlaid := new(MockPlaidServiceForCleanup)

	mockRepo.On("GetByUserID", ctx, userID).Return([]domain.LinkedItem{}, nil).Once()
	postLoopItems := []domain.LinkedItem{{ID: "item-new", UserID: userID, AccessToken: "tok-new", Status: domain.LinkedItemStatusDisconnected}}
	mockRepo.On("GetByUserID", ctx, userID).Return(postLoopItems, nil).Once()

	service := newTestPlaidCleanupService(mockRepo, mockPlaid)

	err := service.RemoveAllUserItems(ctx, userID, alwaysEligible)

	require.NoError(t, err, "a new but already-disconnected item must not trip ErrCleanupAborted when the initial fetch found nothing")
	mockPlaid.AssertNotCalled(t, "RemoveItem")
}

// ─── Issue #1413 Medium finding (PR #1443 Codex review round 4): MarkItemsDisconnected RowsAffected ─

// TestPlaidCleanup_MarkItemsDisconnected_ZeroRowsAffected_LoggedNotEscalated proves cleanupItems
// treats repository.ErrNoLinkedItemsUpdated (Issue #1413 Medium finding) as a logged anomaly, not a
// genuine per-item failure — an idempotent retry that legitimately passes a now-stale successIDs
// set (e.g. every item already 'disconnected'/'replaced' by the time the write runs) must not start
// failing the whole cleanup pass.
//
// Falsifiability: reverting cleanupItems to append EVERY MarkItemsDisconnected error to itemErrs
// (regardless of errors.Is(markErr, repository.ErrNoLinkedItemsUpdated)) makes this test fail — err
// would be non-nil instead of nil.
func TestPlaidCleanup_MarkItemsDisconnected_ZeroRowsAffected_LoggedNotEscalated(t *testing.T) {
	ctx := context.Background()
	userID := "user-mark-zero-rows"

	mockRepo := new(MockLinkedItemRepoForCleanup)
	mockPlaid := new(MockPlaidServiceForCleanup)

	items := []domain.LinkedItem{{ID: "item-1", UserID: userID, AccessToken: "tok-1"}}
	mockRepo.On("GetByUserID", ctx, userID).Return(items, nil)
	mockPlaid.On("RemoveItem", ctx, "tok-1").Return(nil)
	mockRepo.On("MarkItemsDisconnected", ctx, []string{"item-1"}).
		Return(fmt.Errorf("%w: 0 of 1 given id(s) matched", repository.ErrNoLinkedItemsUpdated))

	service := newTestPlaidCleanupService(mockRepo, mockPlaid)

	err := service.RemoveAllUserItems(ctx, userID, alwaysEligible)

	require.NoError(t, err, "a zero-rows-affected MarkItemsDisconnected outcome must be logged, not treated as a genuine per-item failure")
}

// TestPlaidCleanup_MarkItemsDisconnected_GenuineError_StillEscalated proves the OTHER branch of the
// same decision: a genuine (non-ErrNoLinkedItemsUpdated) MarkItemsDisconnected error must still be
// escalated exactly as before — only the zero-rows anomaly is demoted, not every error from this
// call.
func TestPlaidCleanup_MarkItemsDisconnected_GenuineError_StillEscalated(t *testing.T) {
	ctx := context.Background()
	userID := "user-mark-genuine-error"

	mockRepo := new(MockLinkedItemRepoForCleanup)
	mockPlaid := new(MockPlaidServiceForCleanup)

	items := []domain.LinkedItem{{ID: "item-1", UserID: userID, AccessToken: "tok-1"}}
	mockRepo.On("GetByUserID", ctx, userID).Return(items, nil).Once()
	mockPlaid.On("RemoveItem", ctx, "tok-1").Return(nil)
	dbErr := errors.New("connection reset by peer")
	mockRepo.On("MarkItemsDisconnected", ctx, []string{"item-1"}).Return(dbErr)

	service := newTestPlaidCleanupService(mockRepo, mockPlaid)

	err := service.RemoveAllUserItems(ctx, userID, alwaysEligible)

	require.Error(t, err, "a genuine MarkItemsDisconnected database error must still be escalated as a per-item failure")
	assert.ErrorIs(t, err, dbErr)
}

// ─── Issue #1495: plaid_removed_at — "Plaid removal succeeded" gets a slot of its own ───────────

// Test_RemoveItem_StampsPlaidRemovedBeforeMarkingDisconnected pins the ORDER of the two durable
// writes, which is the property that stops this new piece of state from becoming the fifth "state
// marked, work never happened, mark survives forever" defect on this branch.
//
// The stamp must come FIRST. If it fails, status is still live, so ItemCleanupWorker.Work's retry
// re-calls Plaid (ITEM_NOT_FOUND → success) and reaches the stamp again: converges, at the cost of one
// wasted API call, while DeleteUserCascade's C2 guard keeps blocking — fail-closed. In the REVERSE
// order the row would already say 'disconnected', cleanupItems skips 'disconnected' rows without
// calling Plaid, and no retry would ever reach the stamp again: a row claiming cleanup happened,
// carrying no proof of it, permanently blocking that user's erasure.
//
// Falsifiability, and stated as what was actually run rather than as what sounds convincing: removing
// the MarkPlaidRemoved call from RemoveItem makes this test RED (verified — CallLog becomes
// ["MarkItemsDisconnected"] and PlaidRemovedIDs empty). A reordering is caught by the same assertion by
// construction, since it compares the exact sequence rather than membership.
func Test_RemoveItem_StampsPlaidRemovedBeforeMarkingDisconnected(t *testing.T) {
	ctx := context.Background()
	itemID := "item-order"

	mockRepo := new(MockLinkedItemRepoForCleanup)
	mockPlaid := new(MockPlaidServiceForCleanup)

	mockRepo.On("GetByID", ctx, itemID).Return(&domain.LinkedItem{ID: itemID, AccessToken: "tok"}, nil)
	mockPlaid.On("RemoveItem", ctx, "tok").Return(nil)
	mockRepo.On("MarkItemsDisconnected", ctx, []string{itemID}).Return(nil)

	service := newTestPlaidCleanupService(mockRepo, mockPlaid)

	require.NoError(t, service.RemoveItem(ctx, itemID, alwaysEligible))

	assert.Equal(t, []string{itemID}, mockRepo.PlaidRemovedIDs,
		"a successful Plaid removal must be recorded in plaid_removed_at — it is the ONLY fact "+
			"DeleteUserCascade's C2 guard, GetItemsPendingCleanup and the cleanup sweep now read")
	assert.Equal(t, []string{"MarkPlaidRemoved", "MarkItemsDisconnected"}, mockRepo.CallLog,
		"the stamp must precede the status write: the reverse order leaves a 'disconnected' row with no "+
			"proof of removal that nothing will ever re-attempt")
}

// Test_RemoveItem_ItemNotFound_StillStampsPlaidRemoved: the ITEM_NOT_FOUND / already-gone case is a
// SUCCESS — Plaid has forgotten the Item, so billing has stopped — and it must therefore stamp too.
//
// This is not a cosmetic detail. With C2 now demanding the stamp, an already-gone item that did NOT
// get stamped would block its user's GDPR erasure forever over an Item that provably no longer exists.
// It is also the case that makes retries converge at all: a retry after any downstream failure meets
// ITEM_NOT_FOUND, and only because that answers "success" does it reach the stamp.
//
// Falsifiability: moving the MarkPlaidRemoved call inside an `if removeErr == nil` branch makes this
// test fail while Test_RemoveItem_StampsPlaidRemovedBeforeMarkingDisconnected still passes.
func Test_RemoveItem_ItemNotFound_StillStampsPlaidRemoved(t *testing.T) {
	ctx := context.Background()
	itemID := "item-already-gone"

	mockRepo := new(MockLinkedItemRepoForCleanup)
	mockPlaid := new(MockPlaidServiceForCleanup)

	mockRepo.On("GetByID", ctx, itemID).Return(&domain.LinkedItem{ID: itemID, AccessToken: "tok"}, nil)
	mockPlaid.On("RemoveItem", ctx, "tok").Return(plaidItemNotFoundErr())
	mockRepo.On("MarkItemsDisconnected", ctx, []string{itemID}).Return(nil)

	service := newTestPlaidCleanupService(mockRepo, mockPlaid)

	require.NoError(t, service.RemoveItem(ctx, itemID, alwaysEligible))
	assert.Equal(t, []string{itemID}, mockRepo.PlaidRemovedIDs,
		"ITEM_NOT_FOUND means the Item is gone and billing has stopped — that is a removal, and it must "+
			"be recorded, or C2 blocks this user's erasure forever over an Item that does not exist")
}

// Test_RemoveItem_GenuinePlaidError_DoesNotStamp is the sibling that stops the two tests above from
// passing for the wrong reason: a stamp written unconditionally would satisfy both. A genuine Plaid
// failure leaves the Item LIVE, so recording a removal would be a lie — and precisely the lie that
// lets a GDPR erasure complete over a billing Item.
func Test_RemoveItem_GenuinePlaidError_DoesNotStamp(t *testing.T) {
	ctx := context.Background()
	itemID := "item-plaid-down"

	mockRepo := new(MockLinkedItemRepoForCleanup)
	mockPlaid := new(MockPlaidServiceForCleanup)

	mockRepo.On("GetByID", ctx, itemID).Return(&domain.LinkedItem{ID: itemID, AccessToken: "tok"}, nil)
	mockPlaid.On("RemoveItem", ctx, "tok").Return(errors.New("plaid 500"))

	service := newTestPlaidCleanupService(mockRepo, mockPlaid)

	require.Error(t, service.RemoveItem(ctx, itemID, alwaysEligible))
	assert.Empty(t, mockRepo.PlaidRemovedIDs,
		"a failed removal must NOT be recorded as done — the Item is still live and still billing")
	assert.Empty(t, mockRepo.CallLog, "neither durable write may run when the Plaid call failed")
}

// Test_RemoveItem_StampFailure_IsPropagatedAndStatusNotMarked pins the failed-column-write case
// explicitly, because it is the one that decides whether this new state can become the fifth
// "mark survives forever" defect:
//
//   - the error PROPAGATES (so the worker retries rather than believing cleanup finished), and
//   - MarkItemsDisconnected is NOT called, so status stays live and the retry actually re-attempts the
//     item instead of being skipped as already-done.
//
// Falsifiability: changing the stamp failure to a Warn-and-continue makes the CallLog assertion fail;
// returning nil makes the require.Error fail.
func Test_RemoveItem_StampFailure_IsPropagatedAndStatusNotMarked(t *testing.T) {
	ctx := context.Background()
	itemID := "item-stamp-fails"

	mockRepo := new(MockLinkedItemRepoForCleanup)
	mockPlaid := new(MockPlaidServiceForCleanup)
	mockRepo.MarkPlaidRemovedErr = errors.New("db blip")

	mockRepo.On("GetByID", ctx, itemID).Return(&domain.LinkedItem{ID: itemID, AccessToken: "tok"}, nil)
	mockPlaid.On("RemoveItem", ctx, "tok").Return(nil)

	service := newTestPlaidCleanupService(mockRepo, mockPlaid)

	err := service.RemoveItem(ctx, itemID, alwaysEligible)
	require.Error(t, err, "losing the stamp after a successful Plaid removal must fail loudly — the row now "+
		"carries no proof of removal, and silence would let the caller delete it")
	assert.Contains(t, err.Error(), "plaid_removed_at")
	assert.Equal(t, []string{"MarkPlaidRemoved"}, mockRepo.CallLog,
		"status must NOT be marked 'disconnected' when the stamp failed: cleanupItems skips 'disconnected' "+
			"rows without calling Plaid, so writing it here would make the row permanently unreachable")
	mockRepo.AssertNotCalled(t, "MarkItemsDisconnected", mock.Anything, mock.Anything)
}

// Test_RemoveItem_StampZeroRows_IsSuccess: ErrNoLinkedItemsUpdated from the stamp means the row no
// longer exists (MarkPlaidRemoved has no status guard that could filter a live row out), so there is
// nothing left to record and nothing left for C2's EXISTS to block on. Escalating it would spin a
// worker forever on a row that is gone.
func Test_RemoveItem_StampZeroRows_IsSuccess(t *testing.T) {
	ctx := context.Background()
	itemID := "item-vanished"

	mockRepo := new(MockLinkedItemRepoForCleanup)
	mockPlaid := new(MockPlaidServiceForCleanup)
	mockRepo.MarkPlaidRemovedErr = fmt.Errorf("%w (MarkPlaidRemoved): 0 of 1 given id(s) matched", repository.ErrNoLinkedItemsUpdated)

	mockRepo.On("GetByID", ctx, itemID).Return(&domain.LinkedItem{ID: itemID, AccessToken: "tok"}, nil)
	mockPlaid.On("RemoveItem", ctx, "tok").Return(nil)

	service := newTestPlaidCleanupService(mockRepo, mockPlaid)

	require.NoError(t, service.RemoveItem(ctx, itemID, alwaysEligible))
	mockRepo.AssertNotCalled(t, "MarkItemsDisconnected", mock.Anything, mock.Anything)
}

// Test_RemoveItem_ReplacedRow_IsStampedEvenThoughStatusWriteIsANoOp is the service-level statement of
// the defect this change closes. A 'replaced' row reaching RemoveItem gets:
//
//   - a zero-row MarkItemsDisconnected (the guard refuses to overwrite 'replaced' — deliberately), and
//   - a SUCCESSFUL stamp, which is the fact everything downstream reads.
//
// Before #1495 the first bullet was the only durable write attempted, so nothing recorded that Plaid
// was done and the row was re-enqueued by the backstop on every run while C2 waved the erasure through
// on the strength of the 'replaced' status alone. Both halves are asserted here so neither can regress
// alone. The real-Postgres proof of the stamp landing on such a row is
// TestLinkedItemRepository_MarkPlaidRemoved_Integration_StampsReplacedRowWithoutRevertingStatus.
func Test_RemoveItem_ReplacedRow_IsStampedEvenThoughStatusWriteIsANoOp(t *testing.T) {
	ctx := context.Background()
	itemID := "item-replaced"

	mockRepo := new(MockLinkedItemRepoForCleanup)
	mockPlaid := new(MockPlaidServiceForCleanup)

	mockRepo.On("GetByID", ctx, itemID).Return(&domain.LinkedItem{
		ID:          itemID,
		AccessToken: "tok",
		Status:      domain.LinkedItemStatusReplaced,
	}, nil)
	mockPlaid.On("RemoveItem", ctx, "tok").Return(nil)
	// What the real MarkItemsDisconnected does to a 'replaced' row: nothing, reported as the sentinel.
	mockRepo.On("MarkItemsDisconnected", ctx, []string{itemID}).
		Return(fmt.Errorf("%w (MarkItemsDisconnected): 0 of 1 given id(s) matched", repository.ErrNoLinkedItemsUpdated))

	service := newTestPlaidCleanupService(mockRepo, mockPlaid)

	require.NoError(t, service.RemoveItem(ctx, itemID, alwaysEligible))
	assert.Equal(t, []string{itemID}, mockRepo.PlaidRemovedIDs,
		"the 'replaced' row is EXACTLY the row status cannot speak for — if the stamp does not land here, "+
			"nothing records that Plaid is done and C2 blocks this user's erasure forever")
}

// TestPlaidCleanup_CleanupItems_StampsPlaidRemovedForEverySuccess covers the OTHER Plaid-removal path,
// and it is not mere symmetry with RemoveItem: cleanupItems (via RemoveAllUserItems) is the DELETION
// flow's only Plaid cleanup path, so without this call tightening C2 would block every account
// deletion rather than only the 'replaced' ones.
func TestPlaidCleanup_CleanupItems_StampsPlaidRemovedForEverySuccess(t *testing.T) {
	ctx := context.Background()
	userID := "user-1"

	mockRepo := new(MockLinkedItemRepoForCleanup)
	mockPlaid := new(MockPlaidServiceForCleanup)

	mockRepo.On("GetByUserID", ctx, userID).Return([]domain.LinkedItem{
		{ID: "item-1", UserID: userID, AccessToken: "tok-1"},
	}, nil)
	mockPlaid.On("RemoveItem", ctx, "tok-1").Return(nil)
	mockRepo.On("MarkItemsDisconnected", ctx, []string{"item-1"}).Return(nil)

	service := newTestPlaidCleanupService(mockRepo, mockPlaid)

	require.NoError(t, service.RemoveAllUserItems(ctx, userID, alwaysEligible))
	assert.Equal(t, []string{"item-1"}, mockRepo.PlaidRemovedIDs,
		"cleanupItems must stamp every successfully-removed item, or DeleteUserCascade's C2 guard blocks "+
			"the deletion it was just asked to prepare for")
	assert.Equal(t, []string{"MarkPlaidRemoved", "MarkItemsDisconnected"}, mockRepo.CallLog,
		"same ordering rule as RemoveItem, for the same reason")
}

// TestPlaidCleanup_CleanupItems_StampFailure_SkipsMarkDisconnectedAndFails is cleanupItems's version of
// the failed-column-write case. A genuine stamp failure must abort the status write for that batch —
// writing 'disconnected' would make the loop's own skip branch decline to ever re-attempt those items —
// and must surface as a per-item failure so the pass is retried.
func TestPlaidCleanup_CleanupItems_StampFailure_SkipsMarkDisconnectedAndFails(t *testing.T) {
	ctx := context.Background()
	userID := "user-2"

	mockRepo := new(MockLinkedItemRepoForCleanup)
	mockPlaid := new(MockPlaidServiceForCleanup)
	mockRepo.MarkPlaidRemovedErr = errors.New("db blip")

	mockRepo.On("GetByUserID", ctx, userID).Return([]domain.LinkedItem{
		{ID: "item-1", UserID: userID, AccessToken: "tok-1"},
	}, nil)
	mockPlaid.On("RemoveItem", ctx, "tok-1").Return(nil)

	service := newTestPlaidCleanupService(mockRepo, mockPlaid)

	err := service.RemoveAllUserItems(ctx, userID, alwaysEligible)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "plaid_removed_at")
	assert.Equal(t, []string{"MarkPlaidRemoved"}, mockRepo.CallLog,
		"'disconnected' must not be written when the stamp failed — cleanupItems skips 'disconnected' rows "+
			"without calling Plaid, so it would strand them")
	mockRepo.AssertNotCalled(t, "MarkItemsDisconnected", mock.Anything, mock.Anything)
}

// #1626 Phase 3, Defect 3: the periodic balance-capture dispatcher's global item enumeration.
// Nothing in this test file's subject reaches it; the method exists so the mock still satisfies
// repository.LinkedItemRepository.
func (m *MockLinkedItemRepoForCleanup) GetItemsForBalanceCapturePage(ctx context.Context, after *repository.BalanceCaptureSweepCursor, limit int) ([]domain.BalanceCaptureTarget, error) {
	args := m.Called(ctx, after, limit)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]domain.BalanceCaptureTarget), args.Error(1)
}
