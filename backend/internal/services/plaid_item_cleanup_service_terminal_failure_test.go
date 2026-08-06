package services

import (
	"context"
	"errors"
	"testing"

	"github.com/plaid/plaid-go/v23/plaid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/repository"
)

// Issue #1494 — a Plaid /item/remove failure that can NEVER succeed must become a RECORDED,
// terminal fact instead of an indefinite block on the user's GDPR erasure — and it must never be
// recorded as, or mistaken for, "the Item was removed".
//
// Every test here is about one of three properties:
//
//  1. CLASSIFICATION IS NARROW. Only a verdict on the credential itself is terminal. Anything
//     unrecognised stays recoverable, because calling a transient error terminal strands a live,
//     still-billing Plaid Item labelled hopeless AND removes the retry that would have fixed it.
//  2. THE TWO FACTS STAY SEPARATE. A terminal failure writes plaid_removal_failed_at and must NEVER
//     write plaid_removed_at (or 'disconnected'), which mean "billing has stopped".
//  3. NO RECORD, NO PERMISSION. ErrPlaidRemovalUnrecoverable is the only thing that lets
//     DeletionService proceed to an irreversible erasure, so it must appear only after the verdict is
//     durably on the row, and never alongside a failure that might yet succeed.

// plaidInvalidAccessTokenErr builds a plaid.GenericOpenAPIError whose body decodes to Plaid's
// INVALID_ACCESS_TOKEN error — the motivating unrecoverable case in issue #1494. Same shape as
// plaidItemNotFoundErr (plaid_item_cleanup_service_test.go), which is deliberately the CONTRASTING
// code: ITEM_NOT_FOUND means the Item is gone (success), this one means our token is unusable and says
// nothing about the Item.
func plaidInvalidAccessTokenErr() error {
	return plaid.MakeGenericOpenAPIError(
		[]byte(`{"error_type":"INVALID_INPUT","error_code":"INVALID_ACCESS_TOKEN","error_message":"provided access token is in an invalid format"}`),
		"400 Bad Request",
		nil,
	)
}

// plaidErrWithCode builds a Plaid API error carrying an arbitrary error_code, for the codes that must
// NOT be classified terminal.
func plaidErrWithCode(errorType, errorCode string) error {
	return plaid.MakeGenericOpenAPIError(
		[]byte(`{"error_type":"`+errorType+`","error_code":"`+errorCode+`","error_message":"fixture"}`),
		"400 Bad Request",
		nil,
	)
}

// Test_TerminalPlaidRemovalFailure_ClassifiesOnlyTheNarrowSet pins property 1 above, in both
// directions. The false rows matter more than the true ones: each is an error whose misclassification
// would strand a recoverable, live, billing Plaid Item.
func Test_TerminalPlaidRemovalFailure_ClassifiesOnlyTheNarrowSet(t *testing.T) {
	itemWithToken := &domain.LinkedItem{ID: "item-1", AccessToken: "usable-token"}
	itemWithoutToken := &domain.LinkedItem{ID: "item-2", AccessToken: ""}

	tests := []struct {
		name         string
		item         *domain.LinkedItem
		err          error
		wantCode     string
		wantTerminal bool
	}{
		{
			name:         "Plaid INVALID_ACCESS_TOKEN is terminal — the stored credential is the only one we will ever have",
			item:         itemWithToken,
			err:          plaidInvalidAccessTokenErr(),
			wantCode:     "INVALID_ACCESS_TOKEN",
			wantTerminal: true,
		},
		{
			name:         "no access token at all is terminal — there is nothing to send and nothing can repopulate it",
			item:         itemWithoutToken,
			err:          errors.New("plaid RemoveItem failed for item item-2: access token cannot be empty"),
			wantCode:     "NO_USABLE_ACCESS_TOKEN",
			wantTerminal: true,
		},
		{
			name: "INVALID_API_KEYS is RECOVERABLE — an operator misconfiguration a config fix cures; " +
				"treating it as terminal would strand every Item in the deployment on a typo",
			item: itemWithToken,
			err:  plaidErrWithCode("INVALID_INPUT", "INVALID_API_KEYS"),
		},
		{
			name: "ITEM_LOGIN_REQUIRED is RECOVERABLE — /item/remove does not need working credentials at " +
				"the institution, and this says nothing about our token",
			item: itemWithToken,
			err:  plaidErrWithCode("ITEM_ERROR", "ITEM_LOGIN_REQUIRED"),
		},
		{
			name: "INTERNAL_SERVER_ERROR is RECOVERABLE — the textbook transient case",
			item: itemWithToken,
			err:  plaidErrWithCode("API_ERROR", "INTERNAL_SERVER_ERROR"),
		},
		{
			name: "a non-Plaid error on a row that HAS a token is RECOVERABLE — a network failure carries no verdict",
			item: itemWithToken,
			err:  errors.New("dial tcp: connection refused"),
		},
		{
			name: "a nil error is not a failure at all",
			item: itemWithToken,
			err:  nil,
		},
		{
			name: "a nil item fails closed — a mis-wired caller must not produce a terminal verdict",
			item: nil,
			err:  plaidInvalidAccessTokenErr(),
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			code, terminal := terminalPlaidRemovalFailure(tc.item, tc.err)
			assert.Equal(t, tc.wantTerminal, terminal)
			assert.Equal(t, tc.wantCode, code)
		})
	}
}

// Test_RemoveItem_InvalidAccessToken_RecordsVerdictAndNeverClaimsRemoval covers properties 1, 2 and 3
// on the single-item worker path (ItemCleanupWorker.Work → RemoveItem).
//
// The negative assertions are the load-bearing ones: writing plaid_removed_at here would assert on
// Plaid's behalf that billing had stopped, which is what #1495 established the schema must never do
// for a row whose removal was not proved.
func Test_RemoveItem_InvalidAccessToken_RecordsVerdictAndNeverClaimsRemoval(t *testing.T) {
	ctx := context.Background()
	const itemID = "item-invalid-token"

	mockRepo := new(MockLinkedItemRepoForCleanup)
	mockPlaid := new(MockPlaidServiceForCleanup)
	mockRepo.On("GetByID", ctx, itemID).Return(&domain.LinkedItem{
		ID: itemID, UserID: "user-1", ItemID: "plaid-item-1", AccessToken: "wrong-environment-token",
	}, nil)
	mockPlaid.On("RemoveItem", ctx, "wrong-environment-token").Return(plaidInvalidAccessTokenErr())

	svc := newTestPlaidCleanupService(mockRepo, mockPlaid)
	err := svc.RemoveItem(ctx, itemID)

	require.Error(t, err, "the worker must keep failing and retrying: the row still exists and re-attempting it is harmless")
	assert.ErrorIs(t, err, ErrPlaidRemovalUnrecoverable,
		"the verdict is recorded, so the error must be distinguishable from a retryable failure")
	assert.Equal(t,
		[]recordedPlaidRemovalFailure{{ItemID: itemID, ErrorCode: "INVALID_ACCESS_TOKEN"}},
		mockRepo.PlaidRemovalFailures,
		"the verdict must be recorded on the row, with the Plaid code that justified it")
	assert.Empty(t, mockRepo.PlaidRemovedIDs,
		"INVALID_ACCESS_TOKEN says our TOKEN is unusable, not that the Item is gone — stamping plaid_removed_at would assert billing stopped when Plaid never said so")
	assert.NotContains(t, mockRepo.CallLog, "MarkItemsDisconnected",
		"'disconnected' is read by cleanupItems's skip branch; writing it would stop the item ever being re-attempted")
}

// Test_RemoveItem_TransientPlaidFailure_RecordsNoVerdict is the falsifying counterpart: the same code
// path, an error that is NOT in the narrow set, and no verdict written. Without this, an
// implementation that marked EVERY failure terminal would pass every other test in this file.
func Test_RemoveItem_TransientPlaidFailure_RecordsNoVerdict(t *testing.T) {
	ctx := context.Background()
	const itemID = "item-outage"

	mockRepo := new(MockLinkedItemRepoForCleanup)
	mockPlaid := new(MockPlaidServiceForCleanup)
	mockRepo.On("GetByID", ctx, itemID).Return(&domain.LinkedItem{
		ID: itemID, UserID: "user-1", ItemID: "plaid-item-1", AccessToken: "perfectly-good-token",
	}, nil)
	mockPlaid.On("RemoveItem", ctx, "perfectly-good-token").Return(plaidErrWithCode("API_ERROR", "INTERNAL_SERVER_ERROR"))

	svc := newTestPlaidCleanupService(mockRepo, mockPlaid)
	err := svc.RemoveItem(ctx, itemID)

	require.Error(t, err)
	assert.NotErrorIs(t, err, ErrPlaidRemovalUnrecoverable,
		"a Plaid outage must stay retryable — the sentinel is permission to complete an irreversible erasure")
	assert.Empty(t, mockRepo.PlaidRemovalFailures,
		"a transient failure must not be recorded as an unrecoverable verdict; the row must keep blocking and keep being retried")
	assert.Empty(t, mockRepo.PlaidRemovedIDs)
}

// Test_RemoveAllUserItems_AllFailuresUnrecoverable_ReportsPermissionAfterRecording is the deletion
// path (DeletionService.ExecuteDeletion → RemoveAllUserItems → cleanupItems). This is the pass that
// #1494 needs to stop being a permanent abort.
func Test_RemoveAllUserItems_AllFailuresUnrecoverable_ReportsPermissionAfterRecording(t *testing.T) {
	ctx := context.Background()
	const userID = "user-1"

	items := []domain.LinkedItem{
		{ID: "item-1", UserID: userID, AccessToken: "bad-token-1", Status: domain.LinkedItemStatusActive},
		{ID: "item-2", UserID: userID, AccessToken: "", Status: domain.LinkedItemStatusReplaced},
	}

	mockRepo := new(MockLinkedItemRepoForCleanup)
	mockPlaid := new(MockPlaidServiceForCleanup)
	mockRepo.On("GetByUserID", ctx, userID).Return(items, nil)
	mockPlaid.On("RemoveItem", ctx, "bad-token-1").Return(plaidInvalidAccessTokenErr())
	mockPlaid.On("RemoveItem", ctx, "").Return(errors.New("access token cannot be empty"))

	svc := newTestPlaidCleanupService(mockRepo, mockPlaid)
	err := svc.RemoveAllUserItems(ctx, userID)

	require.Error(t, err, "the pass did not succeed and must not report nil — nothing was removed")
	assert.ErrorIs(t, err, ErrPlaidRemovalUnrecoverable)
	assert.NotErrorIs(t, err, ErrCleanupAborted,
		"ErrCleanupAborted means 'items were never attempted'; both of these were attempted and answered")
	assert.ElementsMatch(t,
		[]recordedPlaidRemovalFailure{
			{ItemID: "item-1", ErrorCode: "INVALID_ACCESS_TOKEN"},
			{ItemID: "item-2", ErrorCode: "NO_USABLE_ACCESS_TOKEN"},
		},
		mockRepo.PlaidRemovalFailures,
		"every item the sentinel covers must have its own recorded verdict — the cascade's C2 guard re-reads each row individually")
	assert.Empty(t, mockRepo.PlaidRemovedIDs,
		"a 'replaced' row with no token is exactly the row #1495 warned about: it must not acquire a plaid_removed_at it did not earn")
}

// Test_RemoveAllUserItems_UnrecoverablePlusTransient_ReportsGenuineFailure pins the precedence rule.
// One retryable failure anywhere in the pass means the pass is unfinished, so no caller may read it as
// permission to proceed — even though another item in the same pass was correctly judged unremovable.
func Test_RemoveAllUserItems_UnrecoverablePlusTransient_ReportsGenuineFailure(t *testing.T) {
	ctx := context.Background()
	const userID = "user-1"

	items := []domain.LinkedItem{
		{ID: "item-terminal", UserID: userID, AccessToken: "bad-token", Status: domain.LinkedItemStatusActive},
		{ID: "item-transient", UserID: userID, AccessToken: "good-token", Status: domain.LinkedItemStatusActive},
	}

	mockRepo := new(MockLinkedItemRepoForCleanup)
	mockPlaid := new(MockPlaidServiceForCleanup)
	mockRepo.On("GetByUserID", ctx, userID).Return(items, nil)
	mockPlaid.On("RemoveItem", ctx, "bad-token").Return(plaidInvalidAccessTokenErr())
	mockPlaid.On("RemoveItem", ctx, "good-token").Return(plaidErrWithCode("API_ERROR", "INTERNAL_SERVER_ERROR"))

	svc := newTestPlaidCleanupService(mockRepo, mockPlaid)
	err := svc.RemoveAllUserItems(ctx, userID)

	require.Error(t, err)
	assert.NotErrorIs(t, err, ErrPlaidRemovalUnrecoverable,
		"a retryable failure alongside a terminal one must NOT grant permission to erase — item-transient may still be removable")
	assert.Equal(t,
		[]recordedPlaidRemovalFailure{{ItemID: "item-terminal", ErrorCode: "INVALID_ACCESS_TOKEN"}},
		mockRepo.PlaidRemovalFailures,
		"the terminal item's verdict is still recorded — it is true regardless of what the other item did")
	// Both causes belong in the message an operator reads. Matched on the item ids, not the Plaid
	// error codes: a plaid.GenericOpenAPIError's Error() is its HTTP status line ("400 Bad Request"),
	// so the code lives in the recorded verdict above and in the log line, not in this string.
	assert.Contains(t, err.Error(), "item-terminal")
	assert.Contains(t, err.Error(), "item-transient")
}

// Test_RemoveAllUserItems_TerminalFailureRecordingFails_DoesNotGrantPermissionToProceed is this
// batch's ordering pin, the analogue of #1495's stamp-before-status test: the sentinel is permission
// to erase, and permission may not exist before the record does.
//
// Without this rule the failure shape is the one four earlier fixes on this branch each closed, with
// the pieces swapped: the caller is told "proceed", the row carries no verdict, and the erasure
// either proceeds over an unrecorded Item (if C2 were ever loosened to trust the sentinel) or bounces
// off C2 with the reason invisible. Reporting an ordinary retryable failure keeps C2 blocking and
// brings the retry back to this line.
func Test_RemoveAllUserItems_TerminalFailureRecordingFails_DoesNotGrantPermissionToProceed(t *testing.T) {
	ctx := context.Background()
	const userID = "user-1"

	items := []domain.LinkedItem{
		{ID: "item-1", UserID: userID, AccessToken: "bad-token", Status: domain.LinkedItemStatusActive},
	}

	mockRepo := new(MockLinkedItemRepoForCleanup)
	mockPlaid := new(MockPlaidServiceForCleanup)
	mockRepo.On("GetByUserID", ctx, userID).Return(items, nil)
	mockPlaid.On("RemoveItem", ctx, "bad-token").Return(plaidInvalidAccessTokenErr())
	mockRepo.MarkPlaidRemovalFailedErr = errors.New("connection reset by peer")

	svc := newTestPlaidCleanupService(mockRepo, mockPlaid)
	err := svc.RemoveAllUserItems(ctx, userID)

	require.Error(t, err)
	assert.NotErrorIs(t, err, ErrPlaidRemovalUnrecoverable,
		"an unrecorded verdict must never be reported as permission to proceed")
	assert.Contains(t, err.Error(), "connection reset by peer",
		"the recording failure is the actionable cause and must reach the caller")
}

// Test_RemoveAllUserItems_RecordingMatchedZeroRows_StillReportsPermission covers the benign zero-row
// case, which is NOT a recording failure: MarkPlaidRemovalFailed's guard matches nothing when the row
// is gone (nothing left to block or preserve) or when plaid_removed_at is already set (removal was
// proved by some other attempt — strictly better news). Treating that as a failure would reintroduce
// the permanent block for rows in a state where nothing is owed.
func Test_RemoveAllUserItems_RecordingMatchedZeroRows_StillReportsPermission(t *testing.T) {
	ctx := context.Background()
	const userID = "user-1"

	items := []domain.LinkedItem{
		{ID: "item-1", UserID: userID, AccessToken: "bad-token", Status: domain.LinkedItemStatusActive},
	}

	mockRepo := new(MockLinkedItemRepoForCleanup)
	mockPlaid := new(MockPlaidServiceForCleanup)
	mockRepo.On("GetByUserID", ctx, userID).Return(items, nil)
	mockPlaid.On("RemoveItem", ctx, "bad-token").Return(plaidInvalidAccessTokenErr())
	mockRepo.MarkPlaidRemovalFailedErr = repository.ErrNoLinkedItemsUpdated

	svc := newTestPlaidCleanupService(mockRepo, mockPlaid)
	err := svc.RemoveAllUserItems(ctx, userID)

	require.Error(t, err)
	assert.ErrorIs(t, err, ErrPlaidRemovalUnrecoverable)
}

// Test_RemoveAllUserItems_NewLiveItemAppeared_OutranksUnrecoverableSentinel pins the other precedence
// edge. A live item created after the initial fetch was never attempted at all — no Plaid call, no
// verdict, nothing recorded — so reporting the sentinel would tell DeletionService it may erase over an
// Item nobody has even tried to remove.
func Test_RemoveAllUserItems_NewLiveItemAppeared_OutranksUnrecoverableSentinel(t *testing.T) {
	ctx := context.Background()
	const userID = "user-1"

	original := []domain.LinkedItem{
		{ID: "item-1", UserID: userID, AccessToken: "bad-token", Status: domain.LinkedItemStatusActive},
	}
	afterwards := []domain.LinkedItem{
		{ID: "item-1", UserID: userID, AccessToken: "bad-token", Status: domain.LinkedItemStatusActive},
		{ID: "item-new", UserID: userID, AccessToken: "fresh-token", Status: domain.LinkedItemStatusActive},
	}

	mockRepo := new(MockLinkedItemRepoForCleanup)
	mockPlaid := new(MockPlaidServiceForCleanup)
	mockRepo.On("GetByUserID", ctx, userID).Return(original, nil).Once()
	mockRepo.On("GetByUserID", ctx, userID).Return(afterwards, nil).Once()
	mockPlaid.On("RemoveItem", ctx, "bad-token").Return(plaidInvalidAccessTokenErr())

	svc := newTestPlaidCleanupService(mockRepo, mockPlaid)
	err := svc.RemoveAllUserItems(ctx, userID)

	require.Error(t, err)
	assert.ErrorIs(t, err, ErrCleanupAborted,
		"an unattempted live item makes the pass incomplete, whatever else happened in it")
	assert.NotErrorIs(t, err, ErrPlaidRemovalUnrecoverable,
		"permission to erase must not be reported while an item exists that nothing has attempted")
	// The terminal item's own verdict is still correct and still recorded — it is the PASS that is
	// incomplete, not that classification.
	assert.Equal(t,
		[]recordedPlaidRemovalFailure{{ItemID: "item-1", ErrorCode: "INVALID_ACCESS_TOKEN"}},
		mockRepo.PlaidRemovalFailures)
	mockRepo.AssertExpectations(t)
}
