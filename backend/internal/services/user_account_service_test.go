package services

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/repository"
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

func (m *MockUserAccountRepository) FindByInstitutionAndMask(ctx context.Context, userID, institutionID string, mask *string) (*domain.UserAccount, error) {
	args := m.Called(ctx, userID, institutionID, mask)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*domain.UserAccount), args.Error(1)
}

func (m *MockUserAccountRepository) Update(ctx context.Context, userAccount *domain.UserAccount) error {
	args := m.Called(ctx, userAccount)
	return args.Error(0)
}

func (m *MockUserAccountRepository) UpdatePrimaryTypeAndSubtype(ctx context.Context, id, primaryType, primarySubtype string) error {
	args := m.Called(ctx, id, primaryType, primarySubtype)
	return args.Error(0)
}

func (m *MockUserAccountRepository) UpdateHidden(ctx context.Context, id string, hidden bool) error {
	args := m.Called(ctx, id, hidden)
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
	mockRepo.On("FindByInstitutionAndMask", ctx, userID, institutionID, &mask).Return(existingUA, nil)
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
	mockRepo.AssertNotCalled(t, "UpdatePrimaryTypeAndSubtype", mock.Anything, mock.Anything, mock.Anything, mock.Anything)
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
	mockRepo.On("FindByInstitutionAndMask", ctx, userID, institutionID, &mask).Return(nil, nil)
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

	mockRepo.On("FindByInstitutionAndMask", ctx, userID, institutionID, &mask).Return(existingUA, nil)
	mockRepo.On("UpdatePrimaryTypeAndSubtype", ctx, existingUA.ID, newType, newSubtype).Return(nil)

	// Execute
	result, err := service.MatchOrCreate(ctx, userID, institutionID, &mask, "Account", newType, newSubtype)

	// Assert
	assert.NoError(t, err)
	assert.NotNil(t, result)
	mockRepo.AssertExpectations(t)
	mockRepo.AssertCalled(t, "UpdatePrimaryTypeAndSubtype", ctx, existingUA.ID, newType, newSubtype)
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
	mockRepo.On("FindByInstitutionAndMask", ctx, userID, institutionID, &mask).Return(existingUA, nil)
	// UpdatePrimaryTypeAndSubtype should NOT be called

	// Execute
	result, err := service.MatchOrCreate(ctx, userID, institutionID, &mask, "Account", "depository", "checking")

	// Assert
	assert.NoError(t, err)
	assert.NotNil(t, result)
	mockRepo.AssertExpectations(t)
	mockRepo.AssertNotCalled(t, "UpdatePrimaryTypeAndSubtype", mock.Anything, mock.Anything, mock.Anything, mock.Anything)
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

	// Existing account with all customizations set
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
		IsHidden:        true, // User hid this account
		PrimaryType:     userAccountStrPtr("depository"),
		PrimarySubtype:  userAccountStrPtr("checking"),
	}

	mockRepo.On("FindByInstitutionAndMask", ctx, userID, institutionID, &mask).Return(existingUA, nil)
	// Type matches, no update needed

	// Execute - re-link with same account info
	result, err := service.MatchOrCreate(ctx, userID, institutionID, &mask, "New Plaid Name", "depository", "checking")

	// Assert - all customizations preserved
	assert.NoError(t, err)
	assert.NotNil(t, result)
	assert.Equal(t, "My Custom Name", *result.Nickname)
	assert.Equal(t, "#00FF00", *result.CustomColor)
	assert.Equal(t, "bank", *result.CustomIcon)
	assert.Equal(t, "💰", *result.CustomEmoji)
	assert.Equal(t, 5, *result.DisplayOrder)
	assert.True(t, result.IsHidden)
	mockRepo.AssertExpectations(t)
}

// TestUserAccountService_MatchOrCreate_UnhidesHiddenUserAccount tests that when a matching
// UserAccount has IsHidden=true (set by a prior deletion), MatchOrCreate calls UpdateHidden
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
		IsHidden:       true, // Previously deleted (hidden)
	}

	mockRepo.On("FindByInstitutionAndMask", ctx, userID, institutionID, &mask).Return(existingUA, nil)
	// UpdateHidden must be called to un-hide the account
	mockRepo.On("UpdateHidden", ctx, existingUA.ID, false).Return(nil)

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

// TestUserAccountService_MatchOrCreate_DoesNotCallUpdateHiddenWhenNotHidden tests that when
// a matching UserAccount already has IsHidden=false, UpdateHidden is NOT called.
func TestUserAccountService_MatchOrCreate_DoesNotCallUpdateHiddenWhenNotHidden(t *testing.T) {
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

	mockRepo.On("FindByInstitutionAndMask", ctx, userID, institutionID, &mask).Return(existingUA, nil)
	// UpdateHidden must NOT be called

	// Execute
	result, err := service.MatchOrCreate(ctx, userID, institutionID, &mask, "BoA Checking", "depository", "checking")

	// Assert
	assert.NoError(t, err)
	assert.NotNil(t, result)
	assert.False(t, result.IsHidden)
	mockRepo.AssertExpectations(t)
	mockRepo.AssertNotCalled(t, "UpdateHidden", mock.Anything, mock.Anything, mock.Anything)
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
	mockRepo.On("FindByInstitutionAndMask", ctx, userID, institutionID, (*string)(nil)).Return(nil, nil)
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

	mockRepo.On("FindByInstitutionAndMask", ctx, userID, institutionID, (*string)(nil)).Return(existingUA, nil)

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
	mask := "1234"              // Same mask as existing BoA account

	// No match for Chase with this mask
	mockRepo.On("FindByInstitutionAndMask", ctx, userID, institutionID, &mask).Return(nil, nil)
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
	mockRepo.On("FindByInstitutionAndMask", ctx, userID, institutionID, &mask).Return(nil, nil)
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

	mockRepo.On("FindByInstitutionAndMask", ctx, "user-123", "ins_bofa", &mask).
		Return(nil, errors.New("database connection error"))

	// Execute
	result, err := service.MatchOrCreate(ctx, "user-123", "ins_bofa", &mask, "Account", "depository", "checking")

	// Assert
	assert.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "database connection error")
	mockRepo.AssertExpectations(t)
}

// TestUserAccountService_MatchOrCreate_RepoError_Create tests error handling when Create fails.
func TestUserAccountService_MatchOrCreate_RepoError_Create(t *testing.T) {
	mockRepo := new(MockUserAccountRepository)
	service := NewUserAccountService(mockRepo)

	ctx := context.Background()
	mask := "1234"

	mockRepo.On("FindByInstitutionAndMask", ctx, "user-123", "ins_bofa", &mask).Return(nil, nil)
	mockRepo.On("Create", ctx, mock.AnythingOfType("*domain.UserAccount")).
		Return(repository.ErrDuplicateUserAccount)

	// Execute
	result, err := service.MatchOrCreate(ctx, "user-123", "ins_bofa", &mask, "Account", "depository", "checking")

	// Assert
	assert.Error(t, err)
	assert.Nil(t, result)
	assert.ErrorIs(t, err, repository.ErrDuplicateUserAccount)
	mockRepo.AssertExpectations(t)
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
			assert.Equal(t, newNickname, *ua.Nickname)       // Updated
			assert.Equal(t, "#FF0000", *ua.CustomColor)      // Preserved
			assert.Equal(t, "bank", *ua.CustomIcon)          // Preserved
			assert.False(t, ua.IsHidden)                     // Preserved
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
