package services

// Issue #1609, Item 3 (task 3c, Part 2) — a re-link must stop destroying a deliberate user hide.
//
// normalizeExistingUserAccount un-hid the chain on EVERY re-link / MatchOrCreate. That is right
// for a chain the SYSTEM hid (account_service hides a chain when its last active account is
// deleted, so it does not linger as a ghost) and wrong for one the USER hid on purpose — and by
// the time a re-link asks, both have zero active accounts, so nothing in current state tells them
// apart. Migration 000185 added user_accounts.hidden_source to record the cause; this file pins
// the service half of using it.
//
// The rule, and the reason for its asymmetry: ONLY 'system' is un-hidden automatically. 'user' is
// the user's own decision. 'unknown' is 000185's conservative classification for rows it could not
// tell apart, and a NULL cause on a hidden row is treated the same way. Un-hiding an account the
// user hid is a privacy regression they cannot even detect; leaving a system hide in place is an
// annoyance they can undo. The two errors are not symmetric, so the default is not symmetric.

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/testsupport/mockassert"
)

// hiddenChainFixture builds the one shape every case here shares: a matched, hidden chain with a
// given cause, re-linked with unchanged type/subtype (so UpdatePrimaryTypeAndSubtype is not part
// of the question).
func hiddenChainFixture(source *string) *domain.UserAccount {
	depository, checking := "depository", "checking"
	return &domain.UserAccount{
		ID:             "ua-1",
		UserID:         "user-1",
		InstitutionID:  "ins_1",
		PrimaryType:    &depository,
		PrimarySubtype: &checking,
		IsHidden:       true,
		HiddenSource:   source,
	}
}

func matchOrCreateAgainst(t *testing.T, existing *domain.UserAccount) (*MockUserAccountRepository, *domain.UserAccount, error) {
	t.Helper()
	mockRepo := &MockUserAccountRepository{}
	svc := NewUserAccountService(mockRepo)
	ctx := context.Background()

	mockRepo.On("FindByInstitutionAndMask", ctx, existing.UserID, existing.InstitutionID,
		(*string)(nil), mock.Anything, mock.Anything).Return(existing, nil)
	mockRepo.On("ClearHidden", ctx, existing.ID, existing.UserID).Return(nil).Maybe()

	result, err := svc.MatchOrCreate(ctx, existing.UserID, existing.InstitutionID, nil,
		"Checking", "depository", "checking")
	return mockRepo, result, err
}

// TestMatchOrCreate_DoesNotUnhideAUserInitiatedHide is the defect this part exists to close.
func TestMatchOrCreate_DoesNotUnhideAUserInitiatedHide(t *testing.T) {
	source := domain.HiddenSourceUser
	mockRepo, result, err := matchOrCreateAgainst(t, hiddenChainFixture(&source))

	require.NoError(t, err)
	require.NotNil(t, result)
	assert.True(t, result.IsHidden, "a hide the user set must survive a re-link")
	mockassert.NoCallTo(t, &mockRepo.Mock, "ClearHidden")
}

// TestMatchOrCreate_DoesNotUnhideAnUnknownHide — 'unknown' is 000185's conservative
// classification, not a placeholder. Treating it as 'system' would silently un-hide exactly the
// accounts the migration could NOT prove were safe to un-hide.
func TestMatchOrCreate_DoesNotUnhideAnUnknownHide(t *testing.T) {
	source := domain.HiddenSourceUnknown
	mockRepo, result, err := matchOrCreateAgainst(t, hiddenChainFixture(&source))

	require.NoError(t, err)
	require.NotNil(t, result)
	assert.True(t, result.IsHidden, "an unclassifiable hide must survive a re-link")
	mockassert.NoCallTo(t, &mockRepo.Mock, "ClearHidden")
}

// TestMatchOrCreate_DoesNotUnhideAHideWithNoRecordedCause — a hidden row with a NULL cause is the
// same claim as 'unknown': nobody recorded why. Migration 000185 classifies every hidden row it
// can see, so this is a row hidden by something that predates or bypasses the cause-writing paths;
// it must be treated conservatively rather than as an implicit 'system'.
func TestMatchOrCreate_DoesNotUnhideAHideWithNoRecordedCause(t *testing.T) {
	mockRepo, result, err := matchOrCreateAgainst(t, hiddenChainFixture(nil))

	require.NoError(t, err)
	require.NotNil(t, result)
	assert.True(t, result.IsHidden, "a hide with no recorded cause must survive a re-link")
	mockassert.NoCallTo(t, &mockRepo.Mock, "ClearHidden")
}

// TestMatchOrCreate_UnhidesASystemHide preserves the ORIGINAL intent: re-linking a bank whose
// chain was auto-hidden when its last account was deleted brings the account back.
func TestMatchOrCreate_UnhidesASystemHide(t *testing.T) {
	source := domain.HiddenSourceSystem
	existing := hiddenChainFixture(&source)
	mockRepo, result, err := matchOrCreateAgainst(t, existing)

	require.NoError(t, err)
	require.NotNil(t, result)
	assert.False(t, result.IsHidden, "a system hide must still be lifted by a re-link")
	mockRepo.AssertCalled(t, "ClearHidden", context.Background(), existing.ID, existing.UserID)
}

// ── The two writers of the cause. ─────────────────────────────────────────────────────────────

// TestSetHidden_NeverDowngradesUserToSystem — the SYSTEM hide (a chain that lost its last active
// account) goes through the statement that refuses to overwrite a cause already recorded. The
// refusal itself is a CASE expression Postgres evaluates against the row's current value, so it is
// pinned in user_account_hidden_source_integration_test.go; what is asserted HERE is that
// SetHidden(true) reaches that statement and not some cause-blind one.
func TestSetHidden_NeverDowngradesUserToSystem(t *testing.T) {
	mockRepo := &MockUserAccountRepository{}
	svc := NewUserAccountService(mockRepo)
	ctx := context.Background()

	mockRepo.On("SetHiddenBySystem", ctx, "ua-1", "user-1").Return(nil).Once()

	require.NoError(t, svc.SetHidden(ctx, "ua-1", "user-1", true))

	mockRepo.AssertExpectations(t)
	mockassert.NoCallTo(t, &mockRepo.Mock, "ClearHidden")
	mockassert.NoCallTo(t, &mockRepo.Mock, "Update")
}

// TestSetHidden_UnhidingClearsTheCause — the other direction clears flag and cause together.
func TestSetHidden_UnhidingClearsTheCause(t *testing.T) {
	mockRepo := &MockUserAccountRepository{}
	svc := NewUserAccountService(mockRepo)
	ctx := context.Background()

	mockRepo.On("ClearHidden", ctx, "ua-1", "user-1").Return(nil).Once()

	require.NoError(t, svc.SetHidden(ctx, "ua-1", "user-1", false))

	mockRepo.AssertExpectations(t)
	mockassert.NoCallTo(t, &mockRepo.Mock, "SetHiddenBySystem")
}

// TestUpdateCustomization_HidingRecordsAUserCause — PATCH /api/v1/user-accounts/{id} is the user
// speaking, so the hide it sets is the one cause a re-link must never lift.
func TestUpdateCustomization_HidingRecordsAUserCause(t *testing.T) {
	mockRepo := &MockUserAccountRepository{}
	svc := NewUserAccountService(mockRepo)
	ctx := context.Background()

	existing := &domain.UserAccount{ID: "ua-1", UserID: "user-1", InstitutionID: "ins_1"}
	mockRepo.On("GetByIDAndUser", ctx, "ua-1", "user-1").Return(existing, nil).Once()
	mockRepo.On("Update", ctx, mock.MatchedBy(func(ua *domain.UserAccount) bool {
		return ua.IsHidden && ua.HiddenSource != nil && *ua.HiddenSource == domain.HiddenSourceUser
	})).Return(nil).Once()

	hidden := true
	result, err := svc.UpdateCustomization(ctx, "ua-1", "user-1", &domain.UpdateUserAccountRequest{
		IsHidden: &hidden,
	})

	require.NoError(t, err)
	require.NotNil(t, result.HiddenSource)
	assert.Equal(t, domain.HiddenSourceUser, *result.HiddenSource)
	mockRepo.AssertExpectations(t)
}

// TestUpdateCustomization_HidingUpgradesASystemCause — the user hiding an already system-hidden
// chain has now said it themselves, which is strictly better information. Never a downgrade.
func TestUpdateCustomization_HidingUpgradesASystemCause(t *testing.T) {
	mockRepo := &MockUserAccountRepository{}
	svc := NewUserAccountService(mockRepo)
	ctx := context.Background()

	system := domain.HiddenSourceSystem
	existing := &domain.UserAccount{
		ID: "ua-1", UserID: "user-1", InstitutionID: "ins_1",
		IsHidden: true, HiddenSource: &system,
	}
	mockRepo.On("GetByIDAndUser", ctx, "ua-1", "user-1").Return(existing, nil).Once()
	mockRepo.On("Update", ctx, mock.MatchedBy(func(ua *domain.UserAccount) bool {
		return ua.IsHidden && ua.HiddenSource != nil && *ua.HiddenSource == domain.HiddenSourceUser
	})).Return(nil).Once()

	hidden := true
	_, err := svc.UpdateCustomization(ctx, "ua-1", "user-1", &domain.UpdateUserAccountRequest{
		IsHidden: &hidden,
	})

	require.NoError(t, err)
	mockRepo.AssertExpectations(t)
}

// TestUpdateCustomization_UnhidingClearsTheCause — a visible chain has no hide to explain, and a
// stale cause would be inherited by whatever hid the row next.
func TestUpdateCustomization_UnhidingClearsTheCause(t *testing.T) {
	mockRepo := &MockUserAccountRepository{}
	svc := NewUserAccountService(mockRepo)
	ctx := context.Background()

	user := domain.HiddenSourceUser
	existing := &domain.UserAccount{
		ID: "ua-1", UserID: "user-1", InstitutionID: "ins_1",
		IsHidden: true, HiddenSource: &user,
	}
	mockRepo.On("GetByIDAndUser", ctx, "ua-1", "user-1").Return(existing, nil).Once()
	mockRepo.On("Update", ctx, mock.MatchedBy(func(ua *domain.UserAccount) bool {
		return !ua.IsHidden && ua.HiddenSource == nil
	})).Return(nil).Once()

	hidden := false
	result, err := svc.UpdateCustomization(ctx, "ua-1", "user-1", &domain.UpdateUserAccountRequest{
		IsHidden: &hidden,
	})

	require.NoError(t, err)
	assert.Nil(t, result.HiddenSource)
	mockRepo.AssertExpectations(t)
}

// TestUpdateCustomization_UnrelatedEditLeavesTheCauseAlone — renaming a hidden account says
// nothing about why it is hidden. The cause must survive an edit that does not mention it.
func TestUpdateCustomization_UnrelatedEditLeavesTheCauseAlone(t *testing.T) {
	mockRepo := &MockUserAccountRepository{}
	svc := NewUserAccountService(mockRepo)
	ctx := context.Background()

	unknown := domain.HiddenSourceUnknown
	existing := &domain.UserAccount{
		ID: "ua-1", UserID: "user-1", InstitutionID: "ins_1",
		IsHidden: true, HiddenSource: &unknown,
	}
	mockRepo.On("GetByIDAndUser", ctx, "ua-1", "user-1").Return(existing, nil).Once()
	mockRepo.On("Update", ctx, mock.MatchedBy(func(ua *domain.UserAccount) bool {
		return ua.IsHidden && ua.HiddenSource != nil && *ua.HiddenSource == domain.HiddenSourceUnknown
	})).Return(nil).Once()

	nickname := "Renamed"
	_, err := svc.UpdateCustomization(ctx, "ua-1", "user-1", &domain.UpdateUserAccountRequest{
		Nickname: &nickname,
	})

	require.NoError(t, err)
	mockRepo.AssertExpectations(t)
}
