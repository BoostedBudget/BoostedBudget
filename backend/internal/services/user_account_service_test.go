package services

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/repository"
	"github.com/maxnussbaum/BoostedBudget/internal/testsupport/mockassert"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

// MockUserAccountRepository is a mock implementation of repository.UserAccountRepository
// Issue #171 - Plaid Re-Link Handling
type MockUserAccountRepository struct {
	mock.Mock
}

func (m *MockUserAccountRepository) Create(ctx context.Context, userAccount *domain.UserAccount) error {
	args := m.Called(ctx, userAccount)
	return args.Error(0)
}

func (m *MockUserAccountRepository) GetByID(ctx context.Context, id string) (*domain.UserAccount, error) {
	args := m.Called(ctx, id)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*domain.UserAccount), args.Error(1)
}

func (m *MockUserAccountRepository) GetByIDAndUser(ctx context.Context, id, userID string) (*domain.UserAccount, error) {
	args := m.Called(ctx, id, userID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*domain.UserAccount), args.Error(1)
}

func (m *MockUserAccountRepository) GetByUserID(ctx context.Context, userID string) ([]domain.UserAccount, error) {
	args := m.Called(ctx, userID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]domain.UserAccount), args.Error(1)
}

func (m *MockUserAccountRepository) FindByInstitutionAndMask(ctx context.Context, userID, institutionID string, mask *string, identityType, identitySubtype *string) (*domain.UserAccount, error) {
	args := m.Called(ctx, userID, institutionID, mask, identityType, identitySubtype)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*domain.UserAccount), args.Error(1)
}

func (m *MockUserAccountRepository) Update(ctx context.Context, userAccount *domain.UserAccount) error {
	args := m.Called(ctx, userAccount)
	return args.Error(0)
}

func (m *MockUserAccountRepository) UpdatePrimaryTypeAndSubtype(ctx context.Context, id, userID, primaryType, primarySubtype string) error {
	args := m.Called(ctx, id, userID, primaryType, primarySubtype)
	return args.Error(0)
}

func (m *MockUserAccountRepository) SetHiddenBySystem(ctx context.Context, id, userID string) error {
	args := m.Called(ctx, id, userID)
	return args.Error(0)
}

func (m *MockUserAccountRepository) ClearHidden(ctx context.Context, id, userID string) error {
	args := m.Called(ctx, id, userID)
	return args.Error(0)
}

func (m *MockUserAccountRepository) PendingInvestmentBackfills(ctx context.Context, userAccountIDs []string, userID string) (map[string]time.Time, error) {
	args := m.Called(ctx, userAccountIDs, userID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(map[string]time.Time), args.Error(1)
}

func (m *MockUserAccountRepository) ClearInvestmentBackfillRequests(ctx context.Context, userAccountIDs []string, userID string, notAfter time.Time) error {
	args := m.Called(ctx, userAccountIDs, userID, notAfter)
	return args.Error(0)
}

// =============================================================================
// MatchOrCreate Tests - Core Re-Link Matching Logic
// =============================================================================

// TestUserAccountService_MatchOrCreate_ExistingMatch tests that an existing user account is
// returned when there's a match on (user_id, institution_id, mask).
// This is the core re-link scenario: user disconnects and reconnects the same bank account.
func TestUserAccountService_MatchOrCreate_ExistingMatch(t *testing.T) {
	mockRepo := new(MockUserAccountRepository)
	service := NewUserAccountService(mockRepo)

	ctx := context.Background()
	userID := "user-123"
	institutionID := "ins_bofa"
	mask := "1234"
	accountName := "BoA Checking"
	accountType := "depository"
	accountSubtype := "checking"

	existingUA := &domain.UserAccount{
		ID:              "ua-existing",
		UserID:          userID,
		InstitutionID:   institutionID,
		Mask:            &mask,
		AccountNameHint: userAccountStrPtr("Old Account Name"),
		Nickname:        userAccountStrPtr("My Checking"),
		CustomColor:     userAccountStrPtr("#FF0000"),
		IsHidden:        false,
		PrimaryType:     userAccountStrPtr("depository"),
		PrimarySubtype:  userAccountStrPtr("checking"),
		CreatedAt:       time.Now().Add(-24 * time.Hour),
		UpdatedAt:       time.Now().Add(-1 * time.Hour),
	}

	// Setup mocks - existing account found
	mockRepo.On("FindByInstitutionAndMask", ctx, userID, institutionID, &mask, mock.Anything, mock.Anything).Return(existingUA, nil)
	// No Create should be called since existing found
	// No UpdatePrimaryTypeAndSubtype should be called since type/subtype match existing

	// Execute
	result, err := service.MatchOrCreate(ctx, userID, institutionID, &mask, accountName, accountType, accountSubtype)

	// Assert
	assert.NoError(t, err)
	assert.NotNil(t, result)
	assert.Equal(t, existingUA.ID, result.ID)
	assert.Equal(t, "My Checking", *result.Nickname) // Customization preserved
	assert.Equal(t, "#FF0000", *result.CustomColor)  // Customization preserved
	mockRepo.AssertExpectations(t)
	mockRepo.AssertNotCalled(t, "Create", mock.Anything, mock.Anything)
	mockRepo.AssertNotCalled(t, "UpdatePrimaryTypeAndSubtype", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything)
}

// TestUserAccountService_MatchOrCreate_NoMatch_CreatesNew tests that a new user account
// is created when no match is found on (user_id, institution_id, mask).
func TestUserAccountService_MatchOrCreate_NoMatch_CreatesNew(t *testing.T) {
	mockRepo := new(MockUserAccountRepository)
	service := NewUserAccountService(mockRepo)

	ctx := context.Background()
	userID := "user-123"
	institutionID := "ins_chase"
	mask := "5678"
	accountName := "Chase Savings"
	accountType := "depository"
	accountSubtype := "savings"

	// Setup mocks - no existing account found
	mockRepo.On("FindByInstitutionAndMask", ctx, userID, institutionID, &mask, mock.Anything, mock.Anything).Return(nil, nil)
	mockRepo.On("Create", ctx, mock.AnythingOfType("*domain.UserAccount")).
		Run(func(args mock.Arguments) {
			ua := args.Get(1).(*domain.UserAccount)
			// Validate created user account fields
			assert.Equal(t, userID, ua.UserID)
			assert.Equal(t, institutionID, ua.InstitutionID)
			assert.Equal(t, mask, *ua.Mask)
			assert.Equal(t, accountName, *ua.AccountNameHint)
			assert.Equal(t, accountType, *ua.PrimaryType)
			assert.Equal(t, accountSubtype, *ua.PrimarySubtype)
			// #1569 round 11 task 3: identity_type/identity_subtype are written ONCE, at mint,
			// from the SAME founding account that caused this mint — never from PrimaryType/
			// PrimarySubtype, which are last-writer-wins and get rewritten on every re-link.
			if assert.NotNil(t, ua.IdentityType) && assert.NotNil(t, ua.IdentitySubtype) {
				assert.Equal(t, accountType, *ua.IdentityType)
				assert.Equal(t, accountSubtype, *ua.IdentitySubtype)
			}
			assert.False(t, ua.IsHidden) // Default
		}).Return(nil)

	// Execute
	result, err := service.MatchOrCreate(ctx, userID, institutionID, &mask, accountName, accountType, accountSubtype)

	// Assert
	assert.NoError(t, err)
	assert.NotNil(t, result)
	assert.Equal(t, userID, result.UserID)
	assert.Equal(t, institutionID, result.InstitutionID)
	mockRepo.AssertExpectations(t)
}

// TestUserAccountService_MatchOrCreate_UpdatesTypeOnMatch tests that the primary type
// is updated when it differs from the existing value on a matched user account.
func TestUserAccountService_MatchOrCreate_UpdatesTypeOnMatch(t *testing.T) {
	mockRepo := new(MockUserAccountRepository)
	service := NewUserAccountService(mockRepo)

	ctx := context.Background()
	userID := "user-123"
	institutionID := "ins_bofa"
	mask := "1234"

	existingUA := &domain.UserAccount{
		ID:             "ua-existing",
		UserID:         userID,
		InstitutionID:  institutionID,
		Mask:           &mask,
		PrimaryType:    userAccountStrPtr("depository"),
		PrimarySubtype: userAccountStrPtr("checking"),
	}

	newType := "credit" // Different from existing
	newSubtype := "credit card"

	mockRepo.On("FindByInstitutionAndMask", ctx, userID, institutionID, &mask, mock.Anything, mock.Anything).Return(existingUA, nil)
	mockRepo.On("UpdatePrimaryTypeAndSubtype", ctx, existingUA.ID, existingUA.UserID, newType, newSubtype).Return(nil)

	// Execute
	result, err := service.MatchOrCreate(ctx, userID, institutionID, &mask, "Account", newType, newSubtype)

	// Assert
	assert.NoError(t, err)
	assert.NotNil(t, result)
	mockRepo.AssertExpectations(t)
	mockRepo.AssertCalled(t, "UpdatePrimaryTypeAndSubtype", ctx, existingUA.ID, existingUA.UserID, newType, newSubtype)
}

// TestUserAccountService_MatchOrCreate_NoUpdateWhenTypeSame tests that UpdatePrimaryTypeAndSubtype
// is NOT called when the type and subtype match the existing values.
func TestUserAccountService_MatchOrCreate_NoUpdateWhenTypeSame(t *testing.T) {
	mockRepo := new(MockUserAccountRepository)
	service := NewUserAccountService(mockRepo)

	ctx := context.Background()
	userID := "user-123"
	institutionID := "ins_bofa"
	mask := "1234"

	existingUA := &domain.UserAccount{
		ID:             "ua-existing",
		UserID:         userID,
		InstitutionID:  institutionID,
		Mask:           &mask,
		PrimaryType:    userAccountStrPtr("depository"),
		PrimarySubtype: userAccountStrPtr("checking"),
	}

	// Same type and subtype
	mockRepo.On("FindByInstitutionAndMask", ctx, userID, institutionID, &mask, mock.Anything, mock.Anything).Return(existingUA, nil)
	// UpdatePrimaryTypeAndSubtype should NOT be called

	// Execute
	result, err := service.MatchOrCreate(ctx, userID, institutionID, &mask, "Account", "depository", "checking")

	// Assert
	assert.NoError(t, err)
	assert.NotNil(t, result)
	mockRepo.AssertExpectations(t)
	mockRepo.AssertNotCalled(t, "UpdatePrimaryTypeAndSubtype", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything)
}

// TestUserAccountService_MatchOrCreate_PreservesCustomizations tests that user customizations
// (nickname, color, icon, etc.) are preserved when matching an existing user account.
func TestUserAccountService_MatchOrCreate_PreservesCustomizations(t *testing.T) {
	mockRepo := new(MockUserAccountRepository)
	service := NewUserAccountService(mockRepo)

	ctx := context.Background()
	userID := "user-123"
	institutionID := "ins_bofa"
	mask := "1234"

	// Existing account with all customizations set (not hidden — testing customization preservation only)
	existingUA := &domain.UserAccount{
		ID:              "ua-existing",
		UserID:          userID,
		InstitutionID:   institutionID,
		Mask:            &mask,
		AccountNameHint: userAccountStrPtr("Original Name"),
		Nickname:        userAccountStrPtr("My Custom Name"),
		CustomColor:     userAccountStrPtr("#00FF00"),
		CustomIcon:      userAccountStrPtr("bank"),
		CustomEmoji:     userAccountStrPtr("💰"),
		DisplayOrder:    userAccountIntPtr(5),
		IsHidden:        false,
		PrimaryType:     userAccountStrPtr("depository"),
		PrimarySubtype:  userAccountStrPtr("checking"),
	}

	mockRepo.On("FindByInstitutionAndMask", ctx, userID, institutionID, &mask, mock.Anything, mock.Anything).Return(existingUA, nil)
	// Type matches, not hidden — no extra repo calls expected

	// Execute - re-link with same account info
	result, err := service.MatchOrCreate(ctx, userID, institutionID, &mask, "New Plaid Name", "depository", "checking")

	// Assert - all customizations preserved, hidden flag unchanged
	assert.NoError(t, err)
	assert.NotNil(t, result)
	assert.Equal(t, "My Custom Name", *result.Nickname)
	assert.Equal(t, "#00FF00", *result.CustomColor)
	assert.Equal(t, "bank", *result.CustomIcon)
	assert.Equal(t, "💰", *result.CustomEmoji)
	assert.Equal(t, 5, *result.DisplayOrder)
	assert.False(t, result.IsHidden)
	mockRepo.AssertExpectations(t)
	mockassert.NoCallTo(t, &mockRepo.Mock, "ClearHidden")
}

// TestUserAccountService_MatchOrCreate_UnhidesHiddenUserAccount tests that when a matching
// UserAccount has IsHidden=true (set by a prior deletion), MatchOrCreate calls ClearHidden
// to un-hide it and returns the account with IsHidden=false.
func TestUserAccountService_MatchOrCreate_UnhidesHiddenUserAccount(t *testing.T) {
	mockRepo := new(MockUserAccountRepository)
	service := NewUserAccountService(mockRepo)

	ctx := context.Background()
	userID := "user-123"
	institutionID := "ins_bofa"
	mask := "1234"
	accountName := "BoA Checking"
	accountType := "depository"
	accountSubtype := "checking"

	existingUA := &domain.UserAccount{
		ID:             "ua-existing",
		UserID:         userID,
		InstitutionID:  institutionID,
		Mask:           &mask,
		PrimaryType:    userAccountStrPtr("depository"),
		PrimarySubtype: userAccountStrPtr("checking"),
		// Previously deleted, so the SYSTEM hid the chain — #1609 recorded that cause, and it is
		// the ONLY cause a re-link lifts. A user-set hide must survive one; see
		// user_account_hidden_source_test.go.
		IsHidden:     true,
		HiddenSource: userAccountStrPtr(domain.HiddenSourceSystem),
	}

	mockRepo.On("FindByInstitutionAndMask", ctx, userID, institutionID, &mask, mock.Anything, mock.Anything).Return(existingUA, nil)
	// ClearHidden must be called to un-hide the account
	mockRepo.On("ClearHidden", ctx, existingUA.ID, existingUA.UserID).Return(nil)

	// Execute
	result, err := service.MatchOrCreate(ctx, userID, institutionID, &mask, accountName, accountType, accountSubtype)

	// Assert
	assert.NoError(t, err)
	assert.NotNil(t, result)
	assert.Equal(t, existingUA.ID, result.ID)
	assert.False(t, result.IsHidden, "re-linked account should be un-hidden")
	mockRepo.AssertExpectations(t)
	mockRepo.AssertNotCalled(t, "Create", mock.Anything, mock.Anything)
}

// TestUserAccountService_MatchOrCreate_DoesNotCallClearHiddenWhenNotHidden tests that when
// a matching UserAccount already has IsHidden=false, ClearHidden is NOT called.
func TestUserAccountService_MatchOrCreate_DoesNotCallClearHiddenWhenNotHidden(t *testing.T) {
	mockRepo := new(MockUserAccountRepository)
	service := NewUserAccountService(mockRepo)

	ctx := context.Background()
	userID := "user-123"
	institutionID := "ins_bofa"
	mask := "1234"

	existingUA := &domain.UserAccount{
		ID:             "ua-existing",
		UserID:         userID,
		InstitutionID:  institutionID,
		Mask:           &mask,
		PrimaryType:    userAccountStrPtr("depository"),
		PrimarySubtype: userAccountStrPtr("checking"),
		IsHidden:       false, // Already visible
	}

	mockRepo.On("FindByInstitutionAndMask", ctx, userID, institutionID, &mask, mock.Anything, mock.Anything).Return(existingUA, nil)
	// ClearHidden must NOT be called

	// Execute
	result, err := service.MatchOrCreate(ctx, userID, institutionID, &mask, "BoA Checking", "depository", "checking")

	// Assert
	assert.NoError(t, err)
	assert.NotNil(t, result)
	assert.False(t, result.IsHidden)
	mockRepo.AssertExpectations(t)
	mockassert.NoCallTo(t, &mockRepo.Mock, "ClearHidden")
}

// TestUserAccountService_MatchOrCreate_ClearHiddenError_StillReturnsAccountHidden tests
// that when ClearHidden fails during re-link, MatchOrCreate still returns the account
// without error, but IsHidden remains true (in-memory consistent with DB state).
func TestUserAccountService_MatchOrCreate_ClearHiddenError_StillReturnsAccountHidden(t *testing.T) {
	mockRepo := new(MockUserAccountRepository)
	service := NewUserAccountService(mockRepo)

	ctx := context.Background()
	userID := "user-123"
	institutionID := "ins_bofa"
	mask := "1234"

	existingUA := &domain.UserAccount{
		ID:             "ua-existing",
		UserID:         userID,
		InstitutionID:  institutionID,
		Mask:           &mask,
		PrimaryType:    userAccountStrPtr("depository"),
		PrimarySubtype: userAccountStrPtr("checking"),
		IsHidden:       true,
		HiddenSource:   userAccountStrPtr(domain.HiddenSourceSystem),
	}

	mockRepo.On("FindByInstitutionAndMask", ctx, userID, institutionID, &mask, mock.Anything, mock.Anything).Return(existingUA, nil)
	mockRepo.On("ClearHidden", ctx, existingUA.ID, existingUA.UserID).Return(assert.AnError)

	result, err := service.MatchOrCreate(ctx, userID, institutionID, &mask, "BoA Checking", "depository", "checking")

	// No error propagated — ClearHidden failure is non-fatal
	assert.NoError(t, err)
	assert.NotNil(t, result)
	// IsHidden remains true (DB and memory stay in sync — update failed)
	assert.True(t, result.IsHidden, "IsHidden should stay true when ClearHidden fails")
	mockRepo.AssertExpectations(t)
}

// =============================================================================
// Edge Case Tests
// =============================================================================

// TestUserAccountService_MatchOrCreate_NullMask_CreatesNew tests that accounts with NULL mask
// are handled correctly - they should still be matchable if institution matches.
func TestUserAccountService_MatchOrCreate_NullMask_NoExisting(t *testing.T) {
	mockRepo := new(MockUserAccountRepository)
	service := NewUserAccountService(mockRepo)

	ctx := context.Background()
	userID := "user-123"
	institutionID := "ins_investment"
	accountName := "Investment Account"
	accountType := "investment"
	accountSubtype := "brokerage"

	// No mask (nil)
	mockRepo.On("FindByInstitutionAndMask", ctx, userID, institutionID, (*string)(nil), mock.Anything, mock.Anything).Return(nil, nil)
	mockRepo.On("Create", ctx, mock.AnythingOfType("*domain.UserAccount")).
		Run(func(args mock.Arguments) {
			ua := args.Get(1).(*domain.UserAccount)
			assert.Nil(t, ua.Mask) // Mask should be nil
			assert.Equal(t, institutionID, ua.InstitutionID)
		}).Return(nil)

	// Execute with nil mask
	result, err := service.MatchOrCreate(ctx, userID, institutionID, nil, accountName, accountType, accountSubtype)

	// Assert
	assert.NoError(t, err)
	assert.NotNil(t, result)
	mockRepo.AssertExpectations(t)
}

// TestUserAccountService_MatchOrCreate_NullMask_ExistingMatch tests matching when both
// new and existing accounts have NULL mask.
func TestUserAccountService_MatchOrCreate_NullMask_ExistingMatch(t *testing.T) {
	mockRepo := new(MockUserAccountRepository)
	service := NewUserAccountService(mockRepo)

	ctx := context.Background()
	userID := "user-123"
	institutionID := "ins_investment"

	existingUA := &domain.UserAccount{
		ID:             "ua-existing-null",
		UserID:         userID,
		InstitutionID:  institutionID,
		Mask:           nil, // NULL mask
		PrimaryType:    userAccountStrPtr("investment"),
		PrimarySubtype: userAccountStrPtr("brokerage"),
	}

	mockRepo.On("FindByInstitutionAndMask", ctx, userID, institutionID, (*string)(nil), mock.Anything, mock.Anything).Return(existingUA, nil)

	// Execute with nil mask
	result, err := service.MatchOrCreate(ctx, userID, institutionID, nil, "Account", "investment", "brokerage")

	// Assert - should match existing
	assert.NoError(t, err)
	assert.NotNil(t, result)
	assert.Equal(t, existingUA.ID, result.ID)
	mockRepo.AssertExpectations(t)
	mockRepo.AssertNotCalled(t, "Create", mock.Anything, mock.Anything)
}

// TestUserAccountService_MatchOrCreate_DifferentInstitution_CreatesNew tests that accounts
// at different institutions are not matched, even if mask is the same.
func TestUserAccountService_MatchOrCreate_DifferentInstitution_CreatesNew(t *testing.T) {
	mockRepo := new(MockUserAccountRepository)
	service := NewUserAccountService(mockRepo)

	ctx := context.Background()
	userID := "user-123"
	institutionID := "ins_chase" // Different institution
	mask := "1234"               // Same mask as existing BoA account

	// No match for Chase with this mask
	mockRepo.On("FindByInstitutionAndMask", ctx, userID, institutionID, &mask, mock.Anything, mock.Anything).Return(nil, nil)
	mockRepo.On("Create", ctx, mock.AnythingOfType("*domain.UserAccount")).Return(nil)

	// Execute
	result, err := service.MatchOrCreate(ctx, userID, institutionID, &mask, "Chase Account", "depository", "checking")

	// Assert - should create new, not match BoA
	assert.NoError(t, err)
	assert.NotNil(t, result)
	mockRepo.AssertExpectations(t)
	mockRepo.AssertCalled(t, "Create", ctx, mock.AnythingOfType("*domain.UserAccount"))
}

// TestUserAccountService_MatchOrCreate_SameInstitutionDifferentMask_CreatesNew tests that
// accounts at the same institution but different mask create new user accounts.
func TestUserAccountService_MatchOrCreate_SameInstitutionDifferentMask_CreatesNew(t *testing.T) {
	mockRepo := new(MockUserAccountRepository)
	service := NewUserAccountService(mockRepo)

	ctx := context.Background()
	userID := "user-123"
	institutionID := "ins_bofa"
	mask := "5678" // Different mask

	// No match for this mask at BoA
	mockRepo.On("FindByInstitutionAndMask", ctx, userID, institutionID, &mask, mock.Anything, mock.Anything).Return(nil, nil)
	mockRepo.On("Create", ctx, mock.AnythingOfType("*domain.UserAccount")).Return(nil)

	// Execute
	result, err := service.MatchOrCreate(ctx, userID, institutionID, &mask, "BoA Savings", "depository", "savings")

	// Assert - should create new
	assert.NoError(t, err)
	assert.NotNil(t, result)
	mockRepo.AssertExpectations(t)
	mockRepo.AssertCalled(t, "Create", ctx, mock.AnythingOfType("*domain.UserAccount"))
}

// TestUserAccountService_MatchOrCreate_RepoError_FindByInstitutionAndMask tests error handling
// when FindByInstitutionAndMask fails.
func TestUserAccountService_MatchOrCreate_RepoError_FindByInstitutionAndMask(t *testing.T) {
	mockRepo := new(MockUserAccountRepository)
	service := NewUserAccountService(mockRepo)

	ctx := context.Background()
	mask := "1234"

	mockRepo.On("FindByInstitutionAndMask", ctx, "user-123", "ins_bofa", &mask, mock.Anything, mock.Anything).
		Return(nil, errors.New("database connection error"))

	// Execute
	result, err := service.MatchOrCreate(ctx, "user-123", "ins_bofa", &mask, "Account", "depository", "checking")

	// Assert
	assert.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "database connection error")
	mockRepo.AssertExpectations(t)
}

// TestUserAccountService_MatchOrCreate_RepoError_Create tests error handling when Create fails
// with a non-duplicate error (e.g. network failure, constraint violation).
func TestUserAccountService_MatchOrCreate_RepoError_Create(t *testing.T) {
	mockRepo := new(MockUserAccountRepository)
	service := NewUserAccountService(mockRepo)

	ctx := context.Background()
	mask := "1234"
	genericErr := errors.New("database connection error")

	mockRepo.On("FindByInstitutionAndMask", ctx, "user-123", "ins_bofa", &mask, mock.Anything, mock.Anything).Return(nil, nil)
	mockRepo.On("Create", ctx, mock.AnythingOfType("*domain.UserAccount")).
		Return(genericErr)

	// Execute
	result, err := service.MatchOrCreate(ctx, "user-123", "ins_bofa", &mask, "Account", "depository", "checking")

	// Assert
	assert.Error(t, err)
	assert.Nil(t, result)
	assert.ErrorIs(t, err, genericErr)
	mockRepo.AssertExpectations(t)
}

// TestMatchOrCreate_DuplicateError_ReturnsExistingRow tests the concurrent race recovery path:
// when Create returns ErrDuplicateUserAccount (lost a race), MatchOrCreate should re-find
// the winning row and return it instead of propagating the error.
func TestMatchOrCreate_DuplicateError_ReturnsExistingRow(t *testing.T) {
	mockRepo := new(MockUserAccountRepository)
	service := NewUserAccountService(mockRepo)

	ctx := context.Background()
	userID := "user-123"
	institutionID := "ins_bofa"
	mask := "1234"
	accountType := "depository"
	accountSubtype := "checking"

	existingUA := &domain.UserAccount{
		ID:             "ua-winner",
		UserID:         userID,
		InstitutionID:  institutionID,
		Mask:           &mask,
		PrimaryType:    userAccountStrPtr("depository"),
		PrimarySubtype: userAccountStrPtr("checking"),
		IsHidden:       false,
	}

	// First call: no existing row (both goroutines see nil simultaneously)
	// Second call: re-find after duplicate error returns the winner's row
	mockRepo.On("FindByInstitutionAndMask", ctx, userID, institutionID, &mask, mock.Anything, mock.Anything).
		Return(nil, nil).Once()
	mockRepo.On("Create", ctx, mock.AnythingOfType("*domain.UserAccount")).
		Return(repository.ErrDuplicateUserAccount)
	mockRepo.On("FindByInstitutionAndMask", ctx, userID, institutionID, &mask, mock.Anything, mock.Anything).
		Return(existingUA, nil).Once()

	// Execute
	result, err := service.MatchOrCreate(ctx, userID, institutionID, &mask, "BoA Checking", accountType, accountSubtype)

	// Assert: no error, winner's row returned
	assert.NoError(t, err)
	assert.NotNil(t, result)
	assert.Equal(t, "ua-winner", result.ID)

	// FindByInstitutionAndMask must have been called exactly twice
	mockRepo.AssertNumberOfCalls(t, "FindByInstitutionAndMask", 2)
	mockRepo.AssertExpectations(t)
}

// TestMatchOrCreate_DuplicateError_RunsNormalization tests that the re-found row after a
// concurrent race is run through the same normalization as the initial-find path:
// type/subtype update and un-hide are both applied.
func TestMatchOrCreate_DuplicateError_RunsNormalization(t *testing.T) {
	mockRepo := new(MockUserAccountRepository)
	service := NewUserAccountService(mockRepo)

	ctx := context.Background()
	userID := "user-123"
	institutionID := "ins_bofa"
	mask := "1234"
	newType := "credit"
	newSubtype := "credit card"

	// Re-found row has stale type AND is hidden — both normalizations should fire
	existingUA := &domain.UserAccount{
		ID:             "ua-winner",
		UserID:         userID,
		InstitutionID:  institutionID,
		Mask:           &mask,
		PrimaryType:    userAccountStrPtr("depository"), // differs from newType
		PrimarySubtype: userAccountStrPtr("checking"),   // differs from newSubtype
		IsHidden:       true,                            // hidden — should be un-hidden
		HiddenSource:   userAccountStrPtr(domain.HiddenSourceSystem),
	}

	mockRepo.On("FindByInstitutionAndMask", ctx, userID, institutionID, &mask, mock.Anything, mock.Anything).
		Return(nil, nil).Once()
	mockRepo.On("Create", ctx, mock.AnythingOfType("*domain.UserAccount")).
		Return(repository.ErrDuplicateUserAccount)
	mockRepo.On("FindByInstitutionAndMask", ctx, userID, institutionID, &mask, mock.Anything, mock.Anything).
		Return(existingUA, nil).Once()
	mockRepo.On("UpdatePrimaryTypeAndSubtype", ctx, existingUA.ID, existingUA.UserID, newType, newSubtype).Return(nil)
	mockRepo.On("ClearHidden", ctx, existingUA.ID, existingUA.UserID).Return(nil)

	// Execute
	result, err := service.MatchOrCreate(ctx, userID, institutionID, &mask, "BoA", newType, newSubtype)

	// Assert: normalization was applied
	assert.NoError(t, err)
	assert.NotNil(t, result)
	assert.False(t, result.IsHidden, "re-found row should be un-hidden")
	mockRepo.AssertExpectations(t)
	mockRepo.AssertCalled(t, "UpdatePrimaryTypeAndSubtype", ctx, existingUA.ID, existingUA.UserID, newType, newSubtype)
	mockRepo.AssertCalled(t, "ClearHidden", ctx, existingUA.ID, existingUA.UserID)
}

// =============================================================================
// GetByUserID Tests
// =============================================================================

// TestUserAccountService_GetByUserID_Success tests retrieving all user accounts for a user.
func TestUserAccountService_GetByUserID_Success(t *testing.T) {
	mockRepo := new(MockUserAccountRepository)
	service := NewUserAccountService(mockRepo)

	ctx := context.Background()
	userID := "user-123"

	mask1 := "1234"
	mask2 := "5678"
	expectedAccounts := []domain.UserAccount{
		{
			ID:            "ua-1",
			UserID:        userID,
			InstitutionID: "ins_bofa",
			Mask:          &mask1,
			IsHidden:      false,
		},
		{
			ID:            "ua-2",
			UserID:        userID,
			InstitutionID: "ins_chase",
			Mask:          &mask2,
			IsHidden:      true,
		},
	}

	mockRepo.On("GetByUserID", ctx, userID).Return(expectedAccounts, nil)

	// Execute - include hidden
	result, err := service.GetByUserID(ctx, userID, true)

	// Assert
	assert.NoError(t, err)
	assert.Len(t, result, 2)
	mockRepo.AssertExpectations(t)
}

// TestUserAccountService_GetByUserID_ExcludesHidden tests that hidden accounts are excluded
// when includeHidden is false.
func TestUserAccountService_GetByUserID_ExcludesHidden(t *testing.T) {
	mockRepo := new(MockUserAccountRepository)
	service := NewUserAccountService(mockRepo)

	ctx := context.Background()
	userID := "user-123"

	mask1 := "1234"
	mask2 := "5678"
	allAccounts := []domain.UserAccount{
		{
			ID:            "ua-1",
			UserID:        userID,
			InstitutionID: "ins_bofa",
			Mask:          &mask1,
			IsHidden:      false,
		},
		{
			ID:            "ua-2",
			UserID:        userID,
			InstitutionID: "ins_chase",
			Mask:          &mask2,
			IsHidden:      true, // Hidden
		},
	}

	mockRepo.On("GetByUserID", ctx, userID).Return(allAccounts, nil)

	// Execute - exclude hidden
	result, err := service.GetByUserID(ctx, userID, false)

	// Assert - only visible account returned
	assert.NoError(t, err)
	assert.Len(t, result, 1)
	assert.Equal(t, "ua-1", result[0].ID)
	assert.False(t, result[0].IsHidden)
	mockRepo.AssertExpectations(t)
}

// TestUserAccountService_GetByUserID_Empty tests that an empty slice is returned when
// user has no accounts.
func TestUserAccountService_GetByUserID_Empty(t *testing.T) {
	mockRepo := new(MockUserAccountRepository)
	service := NewUserAccountService(mockRepo)

	ctx := context.Background()
	userID := "user-new"

	mockRepo.On("GetByUserID", ctx, userID).Return([]domain.UserAccount{}, nil)

	// Execute
	result, err := service.GetByUserID(ctx, userID, true)

	// Assert
	assert.NoError(t, err)
	assert.NotNil(t, result)
	assert.Empty(t, result)
	mockRepo.AssertExpectations(t)
}

// TestUserAccountService_GetByUserID_RepoError tests error handling when GetByUserID fails.
func TestUserAccountService_GetByUserID_RepoError(t *testing.T) {
	mockRepo := new(MockUserAccountRepository)
	service := NewUserAccountService(mockRepo)

	ctx := context.Background()
	userID := "user-123"

	mockRepo.On("GetByUserID", ctx, userID).Return(nil, errors.New("database error"))

	// Execute
	result, err := service.GetByUserID(ctx, userID, true)

	// Assert
	assert.Error(t, err)
	assert.Nil(t, result)
	mockRepo.AssertExpectations(t)
}

// =============================================================================
// UpdateCustomization Tests
// =============================================================================

// TestUserAccountService_UpdateCustomization_Success tests successful customization update.
func TestUserAccountService_UpdateCustomization_Success(t *testing.T) {
	mockRepo := new(MockUserAccountRepository)
	service := NewUserAccountService(mockRepo)

	ctx := context.Background()
	userID := "user-123"
	uaID := "ua-456"
	mask := "1234"

	existingUA := &domain.UserAccount{
		ID:            uaID,
		UserID:        userID,
		InstitutionID: "ins_bofa",
		Mask:          &mask,
		Nickname:      userAccountStrPtr("Old Name"),
		IsHidden:      false,
	}

	newNickname := "New Custom Name"
	newColor := "#0000FF"
	newHidden := true

	req := &domain.UpdateUserAccountRequest{
		Nickname:    &newNickname,
		CustomColor: &newColor,
		IsHidden:    &newHidden,
	}

	mockRepo.On("GetByIDAndUser", ctx, uaID, userID).Return(existingUA, nil)
	mockRepo.On("Update", ctx, mock.AnythingOfType("*domain.UserAccount")).
		Run(func(args mock.Arguments) {
			ua := args.Get(1).(*domain.UserAccount)
			assert.Equal(t, newNickname, *ua.Nickname)
			assert.Equal(t, newColor, *ua.CustomColor)
			assert.True(t, ua.IsHidden)
		}).Return(nil)

	// Execute
	result, err := service.UpdateCustomization(ctx, uaID, userID, req)

	// Assert
	assert.NoError(t, err)
	assert.NotNil(t, result)
	assert.Equal(t, newNickname, *result.Nickname)
	assert.Equal(t, newColor, *result.CustomColor)
	assert.True(t, result.IsHidden)
	mockRepo.AssertExpectations(t)
}

// TestUserAccountService_UpdateCustomization_NotFound tests error when user account not found.
func TestUserAccountService_UpdateCustomization_NotFound(t *testing.T) {
	mockRepo := new(MockUserAccountRepository)
	service := NewUserAccountService(mockRepo)

	ctx := context.Background()
	userID := "user-123"
	uaID := "ua-nonexistent"

	req := &domain.UpdateUserAccountRequest{
		Nickname: userAccountStrPtr("New Name"),
	}

	mockRepo.On("GetByIDAndUser", ctx, uaID, userID).Return(nil, repository.ErrUserAccountNotFound)

	// Execute
	result, err := service.UpdateCustomization(ctx, uaID, userID, req)

	// Assert
	assert.Error(t, err)
	assert.Nil(t, result)
	assert.ErrorIs(t, err, repository.ErrUserAccountNotFound)
	mockRepo.AssertExpectations(t)
	mockRepo.AssertNotCalled(t, "Update", mock.Anything, mock.Anything)
}

// TestUserAccountService_UpdateCustomization_WrongUser tests that a user cannot update
// another user's account.
func TestUserAccountService_UpdateCustomization_WrongUser(t *testing.T) {
	mockRepo := new(MockUserAccountRepository)
	service := NewUserAccountService(mockRepo)

	ctx := context.Background()
	ownerID := "user-owner"
	attackerID := "user-attacker"
	uaID := "ua-456"

	req := &domain.UpdateUserAccountRequest{
		Nickname: userAccountStrPtr("Hacked Name"),
	}

	// GetByIDAndUser enforces ownership - returns not found for wrong user
	mockRepo.On("GetByIDAndUser", ctx, uaID, attackerID).Return(nil, repository.ErrUserAccountNotFound)

	// Execute - attacker tries to update owner's account
	result, err := service.UpdateCustomization(ctx, uaID, attackerID, req)

	// Assert
	assert.Error(t, err)
	assert.Nil(t, result)
	assert.ErrorIs(t, err, repository.ErrUserAccountNotFound)
	mockRepo.AssertExpectations(t)
	mockRepo.AssertNotCalled(t, "Update", mock.Anything, mock.Anything)

	// Verify owner is not involved
	mockRepo.AssertNotCalled(t, "GetByIDAndUser", ctx, uaID, ownerID)
}

// TestUserAccountService_UpdateCustomization_ValidationError tests validation of request.
func TestUserAccountService_UpdateCustomization_ValidationError(t *testing.T) {
	mockRepo := new(MockUserAccountRepository)
	service := NewUserAccountService(mockRepo)

	ctx := context.Background()
	userID := "user-123"
	uaID := "ua-456"
	mask := "1234"

	existingUA := &domain.UserAccount{
		ID:            uaID,
		UserID:        userID,
		InstitutionID: "ins_bofa",
		Mask:          &mask,
	}

	// Invalid color format
	invalidColor := "not-a-hex-color"
	req := &domain.UpdateUserAccountRequest{
		CustomColor: &invalidColor,
	}

	mockRepo.On("GetByIDAndUser", ctx, uaID, userID).Return(existingUA, nil)

	// Execute
	result, err := service.UpdateCustomization(ctx, uaID, userID, req)

	// Assert
	assert.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "color") // Should mention color validation
	mockRepo.AssertExpectations(t)
	mockRepo.AssertNotCalled(t, "Update", mock.Anything, mock.Anything)
}

// TestUserAccountService_UpdateCustomization_PartialUpdate tests that only provided fields
// are updated, leaving others unchanged.
func TestUserAccountService_UpdateCustomization_PartialUpdate(t *testing.T) {
	mockRepo := new(MockUserAccountRepository)
	service := NewUserAccountService(mockRepo)

	ctx := context.Background()
	userID := "user-123"
	uaID := "ua-456"
	mask := "1234"

	existingUA := &domain.UserAccount{
		ID:            uaID,
		UserID:        userID,
		InstitutionID: "ins_bofa",
		Mask:          &mask,
		Nickname:      userAccountStrPtr("Original Name"),
		CustomColor:   userAccountStrPtr("#FF0000"),
		CustomIcon:    userAccountStrPtr("bank"),
		IsHidden:      false,
	}

	// Only update nickname
	newNickname := "Updated Name"
	req := &domain.UpdateUserAccountRequest{
		Nickname: &newNickname,
		// Other fields nil = no change
	}

	mockRepo.On("GetByIDAndUser", ctx, uaID, userID).Return(existingUA, nil)
	mockRepo.On("Update", ctx, mock.AnythingOfType("*domain.UserAccount")).
		Run(func(args mock.Arguments) {
			ua := args.Get(1).(*domain.UserAccount)
			assert.Equal(t, newNickname, *ua.Nickname)  // Updated
			assert.Equal(t, "#FF0000", *ua.CustomColor) // Preserved
			assert.Equal(t, "bank", *ua.CustomIcon)     // Preserved
			assert.False(t, ua.IsHidden)                // Preserved
		}).Return(nil)

	// Execute
	result, err := service.UpdateCustomization(ctx, uaID, userID, req)

	// Assert
	assert.NoError(t, err)
	assert.NotNil(t, result)
	mockRepo.AssertExpectations(t)
}

// TestUserAccountService_UpdateCustomization_ClearField tests that empty string clears a field.
func TestUserAccountService_UpdateCustomization_ClearField(t *testing.T) {
	mockRepo := new(MockUserAccountRepository)
	service := NewUserAccountService(mockRepo)

	ctx := context.Background()
	userID := "user-123"
	uaID := "ua-456"
	mask := "1234"

	existingUA := &domain.UserAccount{
		ID:            uaID,
		UserID:        userID,
		InstitutionID: "ins_bofa",
		Mask:          &mask,
		Nickname:      userAccountStrPtr("Has Nickname"),
		CustomColor:   userAccountStrPtr("#FF0000"),
	}

	// Clear nickname by setting empty string
	emptyNickname := ""
	req := &domain.UpdateUserAccountRequest{
		Nickname: &emptyNickname,
	}

	mockRepo.On("GetByIDAndUser", ctx, uaID, userID).Return(existingUA, nil)
	mockRepo.On("Update", ctx, mock.AnythingOfType("*domain.UserAccount")).
		Run(func(args mock.Arguments) {
			ua := args.Get(1).(*domain.UserAccount)
			// Empty string should be stored as nil or empty
			assert.True(t, ua.Nickname == nil || *ua.Nickname == "")
		}).Return(nil)

	// Execute
	result, err := service.UpdateCustomization(ctx, uaID, userID, req)

	// Assert
	assert.NoError(t, err)
	assert.NotNil(t, result)
	mockRepo.AssertExpectations(t)
}

// =============================================================================
// GetByID Tests
// =============================================================================

// TestUserAccountService_GetByID_Success tests retrieving a user account by ID.
func TestUserAccountService_GetByID_Success(t *testing.T) {
	mockRepo := new(MockUserAccountRepository)
	service := NewUserAccountService(mockRepo)

	ctx := context.Background()
	userID := "user-123"
	uaID := "ua-456"
	mask := "1234"

	expectedUA := &domain.UserAccount{
		ID:            uaID,
		UserID:        userID,
		InstitutionID: "ins_bofa",
		Mask:          &mask,
	}

	mockRepo.On("GetByIDAndUser", ctx, uaID, userID).Return(expectedUA, nil)

	// Execute
	result, err := service.GetByID(ctx, uaID, userID)

	// Assert
	assert.NoError(t, err)
	assert.NotNil(t, result)
	assert.Equal(t, uaID, result.ID)
	mockRepo.AssertExpectations(t)
}

// TestUserAccountService_GetByID_NotFound tests error when user account not found.
func TestUserAccountService_GetByID_NotFound(t *testing.T) {
	mockRepo := new(MockUserAccountRepository)
	service := NewUserAccountService(mockRepo)

	ctx := context.Background()
	userID := "user-123"
	uaID := "ua-nonexistent"

	mockRepo.On("GetByIDAndUser", ctx, uaID, userID).Return(nil, repository.ErrUserAccountNotFound)

	// Execute
	result, err := service.GetByID(ctx, uaID, userID)

	// Assert
	assert.Error(t, err)
	assert.Nil(t, result)
	assert.ErrorIs(t, err, repository.ErrUserAccountNotFound)
	mockRepo.AssertExpectations(t)
}

// =============================================================================
// Helper functions
// =============================================================================

// userAccountStrPtr returns a pointer to s (avoids conflict with existing userAccountStrPtr)
func userAccountStrPtr(s string) *string {
	return &s
}

// userAccountIntPtr returns a pointer to i (avoids conflict with existing userAccountIntPtr)
func userAccountIntPtr(i int) *int {
	return &i
}

// =============================================================================
// Cross-tenant normalization tests (#1566 follow-up)
//
// normalizeExistingUserAccount must scope its two writes to the AUTHENTICATED
// ACTOR, not to the owner read off the matched row. Passing existing.UserID
// makes the repository's `WHERE id = $x AND user_id = $y` predicate tautological
// — it can never reject — so a matched row belonging to somebody else would be
// rewritten. These tests drive MatchOrCreate with an actor that differs from the
// row owner and require (a) the actor id to reach the repository and (b) the
// resulting ErrUserAccountNotFound rejection to be observable to the caller.
// =============================================================================

// TestUserAccountService_MatchOrCreate_ForeignHiddenRow_RejectsAndPropagates covers the
// un-hide write.
func TestUserAccountService_MatchOrCreate_ForeignHiddenRow_RejectsAndPropagates(t *testing.T) {
	mockRepo := new(MockUserAccountRepository)
	service := NewUserAccountService(mockRepo)

	ctx := context.Background()
	actorID := "user-actor"
	victimID := "user-victim"
	institutionID := "ins_bofa"
	mask := "1234"

	// A row owned by somebody other than the authenticated actor.
	foreignUA := &domain.UserAccount{
		ID:             "ua-victim",
		UserID:         victimID,
		InstitutionID:  institutionID,
		Mask:           &mask,
		PrimaryType:    userAccountStrPtr("depository"),
		PrimarySubtype: userAccountStrPtr("checking"),
		IsHidden:       true,
		HiddenSource:   userAccountStrPtr(domain.HiddenSourceSystem),
	}

	mockRepo.On("FindByInstitutionAndMask", ctx, actorID, institutionID, &mask, mock.Anything, mock.Anything).Return(foreignUA, nil)
	// Scoped to the actor: the repository rejects, because the actor does not own the row.
	mockRepo.On("ClearHidden", ctx, foreignUA.ID, actorID).Return(repository.ErrUserAccountNotFound)
	// Scoped to the row owner (the tautological call): the repository happily succeeds.
	mockRepo.On("ClearHidden", ctx, foreignUA.ID, victimID).Return(nil)

	result, err := service.MatchOrCreate(ctx, actorID, institutionID, &mask, "BoA Checking", "depository", "checking")

	mockRepo.AssertNotCalled(t, "ClearHidden", ctx, foreignUA.ID, victimID)
	assert.Error(t, err, "un-hiding a row the actor does not own must not succeed silently")
	assert.ErrorIs(t, err, repository.ErrUserAccountNotFound)
	assert.Nil(t, result, "no account should be returned for a cross-tenant match")
}

// TestUserAccountService_MatchOrCreate_ForeignRowTypeChange_RejectsAndPropagates covers the
// type/subtype write.
func TestUserAccountService_MatchOrCreate_ForeignRowTypeChange_RejectsAndPropagates(t *testing.T) {
	mockRepo := new(MockUserAccountRepository)
	service := NewUserAccountService(mockRepo)

	ctx := context.Background()
	actorID := "user-actor"
	victimID := "user-victim"
	institutionID := "ins_bofa"
	mask := "1234"
	newType := "investment"
	newSubtype := "brokerage"

	foreignUA := &domain.UserAccount{
		ID:             "ua-victim",
		UserID:         victimID,
		InstitutionID:  institutionID,
		Mask:           &mask,
		PrimaryType:    userAccountStrPtr("depository"),
		PrimarySubtype: userAccountStrPtr("checking"),
		IsHidden:       false,
	}

	mockRepo.On("FindByInstitutionAndMask", ctx, actorID, institutionID, &mask, mock.Anything, mock.Anything).Return(foreignUA, nil)
	mockRepo.On("UpdatePrimaryTypeAndSubtype", ctx, foreignUA.ID, actorID, newType, newSubtype).
		Return(repository.ErrUserAccountNotFound)
	mockRepo.On("UpdatePrimaryTypeAndSubtype", ctx, foreignUA.ID, victimID, newType, newSubtype).
		Return(nil)

	result, err := service.MatchOrCreate(ctx, actorID, institutionID, &mask, "BoA", newType, newSubtype)

	mockRepo.AssertNotCalled(t, "UpdatePrimaryTypeAndSubtype", ctx, foreignUA.ID, victimID, newType, newSubtype)
	assert.Error(t, err, "rewriting type/subtype on a row the actor does not own must not succeed silently")
	assert.ErrorIs(t, err, repository.ErrUserAccountNotFound)
	assert.Nil(t, result, "no account should be returned for a cross-tenant match")
}

// =============================================================================
// exclude_investment_sync (#1609 Item 5)
// =============================================================================

// TestUserAccountService_UpdateCustomization_SetsExcludeInvestmentSync pins that the per-chain
// investment opt-out is plumbed through the same PATCH /api/v1/user-accounts/{id} path as every
// other customization: a non-nil *bool is applied, and nothing else on the row moves.
func TestUserAccountService_UpdateCustomization_SetsExcludeInvestmentSync(t *testing.T) {
	mockRepo := new(MockUserAccountRepository)
	service := NewUserAccountService(mockRepo)

	ctx := context.Background()
	userID := "user-1609"
	uaID := "ua-1609"

	existingUA := &domain.UserAccount{
		ID:                    uaID,
		UserID:                userID,
		InstitutionID:         "ins_morgan_stanley",
		IsHidden:              false,
		ExcludeInvestmentSync: false,
	}

	exclude := true
	req := &domain.UpdateUserAccountRequest{ExcludeInvestmentSync: &exclude}

	mockRepo.On("GetByIDAndUser", ctx, uaID, userID).Return(existingUA, nil)
	mockRepo.On("Update", ctx, mock.AnythingOfType("*domain.UserAccount")).
		Run(func(args mock.Arguments) {
			ua := args.Get(1).(*domain.UserAccount)
			assert.True(t, ua.ExcludeInvestmentSync,
				"the flag the user just set must reach the repository")
			assert.False(t, ua.IsHidden,
				"excluding the duplicate investment feed must not hide the account itself — "+
					"they are different user intents")
		}).Return(nil)

	result, err := service.UpdateCustomization(ctx, uaID, userID, req)

	assert.NoError(t, err)
	assert.NotNil(t, result)
	assert.True(t, result.ExcludeInvestmentSync)
	mockRepo.AssertExpectations(t)
}

// TestUserAccountService_UpdateCustomization_ClearsExcludeInvestmentSync is the reversibility half
// at the API level: the flag is a read filter, so turning it off is a first-class operation and
// must not be one-way.
func TestUserAccountService_UpdateCustomization_ClearsExcludeInvestmentSync(t *testing.T) {
	mockRepo := new(MockUserAccountRepository)
	service := NewUserAccountService(mockRepo)

	ctx := context.Background()
	userID := "user-1609"
	uaID := "ua-1609"

	existingUA := &domain.UserAccount{
		ID:                    uaID,
		UserID:                userID,
		InstitutionID:         "ins_morgan_stanley",
		ExcludeInvestmentSync: true,
	}

	exclude := false
	req := &domain.UpdateUserAccountRequest{ExcludeInvestmentSync: &exclude}

	mockRepo.On("GetByIDAndUser", ctx, uaID, userID).Return(existingUA, nil)
	mockRepo.On("Update", ctx, mock.AnythingOfType("*domain.UserAccount")).
		Run(func(args mock.Arguments) {
			assert.False(t, args.Get(1).(*domain.UserAccount).ExcludeInvestmentSync)
		}).Return(nil)

	result, err := service.UpdateCustomization(ctx, uaID, userID, req)

	assert.NoError(t, err)
	assert.False(t, result.ExcludeInvestmentSync)
	mockRepo.AssertExpectations(t)
}

// TestUserAccountService_UpdateCustomization_OmittedExcludeInvestmentSyncIsNoChange pins the nil
// contract shared by every field on this request: an unrelated PATCH (renaming the account, say)
// must not silently un-exclude a chain the user deliberately excluded.
func TestUserAccountService_UpdateCustomization_OmittedExcludeInvestmentSyncIsNoChange(t *testing.T) {
	mockRepo := new(MockUserAccountRepository)
	service := NewUserAccountService(mockRepo)

	ctx := context.Background()
	userID := "user-1609"
	uaID := "ua-1609"

	existingUA := &domain.UserAccount{
		ID:                    uaID,
		UserID:                userID,
		InstitutionID:         "ins_morgan_stanley",
		ExcludeInvestmentSync: true,
	}

	newNickname := "Platinum CashPlus"
	req := &domain.UpdateUserAccountRequest{Nickname: &newNickname}

	mockRepo.On("GetByIDAndUser", ctx, uaID, userID).Return(existingUA, nil)
	mockRepo.On("Update", ctx, mock.AnythingOfType("*domain.UserAccount")).
		Run(func(args mock.Arguments) {
			assert.True(t, args.Get(1).(*domain.UserAccount).ExcludeInvestmentSync,
				"a PATCH that says nothing about exclude_investment_sync must leave it alone")
		}).Return(nil)

	result, err := service.UpdateCustomization(ctx, uaID, userID, req)

	assert.NoError(t, err)
	assert.True(t, result.ExcludeInvestmentSync)
	mockRepo.AssertExpectations(t)
}
