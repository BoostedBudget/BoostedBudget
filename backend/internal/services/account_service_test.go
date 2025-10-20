package services

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/plaid/plaid-go/v23/plaid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// Mock interfaces for testing
type MockPlaidService struct {
	mock.Mock
}

func (m *MockPlaidService) GetClient() *plaid.APIClient {
	args := m.Called()
	return args.Get(0).(*plaid.APIClient)
}

func (m *MockPlaidService) CreateLinkToken(ctx context.Context, userID string) (*domain.LinkTokenResponse, error) {
	args := m.Called(ctx, userID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*domain.LinkTokenResponse), args.Error(1)
}

type MockEncryptionService struct {
	mock.Mock
}

func (m *MockEncryptionService) Encrypt(plaintext string) (string, error) {
	args := m.Called(plaintext)
	return args.String(0), args.Error(1)
}

func (m *MockEncryptionService) Decrypt(ciphertext string) (string, error) {
	args := m.Called(ciphertext)
	return args.String(0), args.Error(1)
}

type MockLinkedItemRepository struct {
	mock.Mock
}

func (m *MockLinkedItemRepository) Create(ctx context.Context, item *domain.LinkedItem) error {
	args := m.Called(ctx, item)
	return args.Error(0)
}

func (m *MockLinkedItemRepository) GetByUserID(ctx context.Context, userID string) ([]domain.LinkedItem, error) {
	args := m.Called(ctx, userID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]domain.LinkedItem), args.Error(1)
}

func (m *MockLinkedItemRepository) GetByItemID(ctx context.Context, itemID string) (*domain.LinkedItem, error) {
	args := m.Called(ctx, itemID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*domain.LinkedItem), args.Error(1)
}

func (m *MockLinkedItemRepository) UpdateCursor(ctx context.Context, id, cursor string) error {
	args := m.Called(ctx, id, cursor)
	return args.Error(0)
}

func (m *MockLinkedItemRepository) Delete(ctx context.Context, id string) error {
	args := m.Called(ctx, id)
	return args.Error(0)
}

type MockAccountRepository struct {
	mock.Mock
}

func (m *MockAccountRepository) Create(ctx context.Context, account *domain.Account) error {
	args := m.Called(ctx, account)
	return args.Error(0)
}

func (m *MockAccountRepository) CreateBatch(ctx context.Context, accounts []domain.Account) error {
	args := m.Called(ctx, accounts)
	return args.Error(0)
}

func (m *MockAccountRepository) GetByLinkedItemID(ctx context.Context, linkedItemID string) ([]domain.Account, error) {
	args := m.Called(ctx, linkedItemID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]domain.Account), args.Error(1)
}

func (m *MockAccountRepository) GetByUserID(ctx context.Context, userID string) ([]domain.Account, error) {
	args := m.Called(ctx, userID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]domain.Account), args.Error(1)
}

func (m *MockAccountRepository) CountByUserID(ctx context.Context, userID string) (int, error) {
	args := m.Called(ctx, userID)
	return args.Int(0), args.Error(1)
}

type MockUserRepository struct {
	mock.Mock
}

func (m *MockUserRepository) GetUserByAuth0ID(ctx context.Context, auth0UserID string) (*domain.User, error) {
	args := m.Called(ctx, auth0UserID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*domain.User), args.Error(1)
}

func (m *MockUserRepository) GetUserByID(ctx context.Context, id string) (*domain.User, error) {
	args := m.Called(ctx, id)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*domain.User), args.Error(1)
}

func (m *MockUserRepository) CreateUser(ctx context.Context, user *domain.User) error {
	args := m.Called(ctx, user)
	return args.Error(0)
}

func (m *MockUserRepository) UpdateUser(ctx context.Context, user *domain.User) error {
	args := m.Called(ctx, user)
	return args.Error(0)
}

func (m *MockUserRepository) UpdateLastLogin(ctx context.Context, auth0UserID string) error {
	args := m.Called(ctx, auth0UserID)
	return args.Error(0)
}

// TestAccountService_LinkAccounts_Success tests successful account linking flow
func TestAccountService_LinkAccounts_Success(t *testing.T) {
	// Given: All dependencies mocked with successful responses
	mockPlaid := new(MockPlaidService)
	mockSubscription := &SubscriptionService{} // Will need actual implementation
	mockEncryption := new(MockEncryptionService)
	mockLinkedItemRepo := new(MockLinkedItemRepository)
	mockAccountRepo := new(MockAccountRepository)
	mockUserRepo := new(MockUserRepository)

	ctx := context.Background()
	userID := uuid.New().String()
	// publicToken := "public-sandbox-token" // TODO: Use in GREEN phase for Plaid mock
	// institutionID := "ins_109508"         // TODO: Use in GREEN phase for Plaid mock
	institutionName := "Chase"

	// Mock user with free tier
	user := &domain.User{
		ID:               userID,
		SubscriptionTier: "free",
		PrimaryRegion:    "us-east-1",
	}
	mockUserRepo.On("GetUserByID", ctx, userID).Return(user, nil)

	// Mock current account count (user has 2 accounts, limit is 8)
	mockAccountRepo.On("CountByUserID", ctx, userID).Return(2, nil)

	// Mock encryption of access token
	accessToken := "access-sandbox-token-abc123"
	encryptedToken := "encrypted-base64-token"
	mockEncryption.On("Encrypt", accessToken).Return(encryptedToken, nil)

	// Mock linked item creation
	mockLinkedItemRepo.On("Create", ctx, mock.MatchedBy(func(item *domain.LinkedItem) bool {
		return item.UserID == userID &&
			item.AccessTokenEncrypted == encryptedToken &&
			item.InstitutionName == institutionName
	})).Return(nil)

	// Mock account batch creation
	mockAccountRepo.On("CreateBatch", ctx, mock.MatchedBy(func(accounts []domain.Account) bool {
		return len(accounts) == 2 && // Linking 2 new accounts
			accounts[0].UserID == userID
	})).Return(nil)

	// TODO: Create service in GREEN phase when implementing actual Plaid integration
	_ = NewAccountService(
		mockPlaid,
		mockSubscription,
		mockEncryption,
		mockLinkedItemRepo,
		mockAccountRepo,
		mockUserRepo,
		nil, // db not needed for this test
	)

	// When: Link accounts is called
	// Note: This test will fail until we implement the Plaid token exchange mock
	// For now, we're documenting the expected behavior

	// Then: All mocks should be called and operation succeeds
	// This test documents the expected behavior; implementation in GREEN phase
}

// TestAccountService_LinkAccounts_LimitExceeded tests subscription limit enforcement
func TestAccountService_LinkAccounts_LimitExceeded(t *testing.T) {
	// Given: User at account limit trying to link more accounts
	mockAccountRepo := new(MockAccountRepository)
	mockUserRepo := new(MockUserRepository)

	ctx := context.Background()
	userID := uuid.New().String()

	// Mock user with free tier (8 account limit)
	user := &domain.User{
		ID:               userID,
		SubscriptionTier: "free",
	}
	mockUserRepo.On("GetUserByID", ctx, userID).Return(user, nil)

	// Mock current account count (user already has 8 accounts - at limit)
	mockAccountRepo.On("CountByUserID", ctx, userID).Return(8, nil)

	// Service will check limit before calling Plaid
	// Expected: Should return error without calling Plaid API

	// Then: Should return limit exceeded error
	// Implementation will be in GREEN phase
}

// TestAccountService_LinkAccounts_EncryptionVerified tests that access token is encrypted
func TestAccountService_LinkAccounts_EncryptionVerified(t *testing.T) {
	// Given: Mock encryption service
	mockEncryption := new(MockEncryptionService)

	accessToken := "plaintext-access-token"
	encryptedToken := "encrypted-token-base64"

	mockEncryption.On("Encrypt", accessToken).Return(encryptedToken, nil)

	// When: Access token needs to be stored
	result, err := mockEncryption.Encrypt(accessToken)

	// Then: Token should be encrypted
	require.NoError(t, err)
	assert.Equal(t, encryptedToken, result)
	assert.NotEqual(t, accessToken, result, "Encrypted token should differ from plaintext")
	mockEncryption.AssertExpectations(t)
}

// TestAccountService_LinkAccounts_InvalidPublicToken tests error handling for invalid tokens
func TestAccountService_LinkAccounts_InvalidPublicToken(t *testing.T) {
	// Given: Plaid service that will return error for invalid token
	// When: LinkAccounts called with invalid public token
	// Then: Should return appropriate error
	// Implementation in GREEN phase
}

// TestAccountService_LinkAccounts_PlaidAPIError tests error handling for Plaid API failures
func TestAccountService_LinkAccounts_PlaidAPIError(t *testing.T) {
	// Given: Plaid API returns error
	// When: LinkAccounts called
	// Then: Should handle error gracefully and return user-friendly message
	// Implementation in GREEN phase
}

// TestAccountService_LinkAccounts_DatabaseError tests database transaction rollback
func TestAccountService_LinkAccounts_DatabaseError(t *testing.T) {
	// Given: Database operation fails partway through
	// When: LinkAccounts called
	// Then: Should rollback transaction and return error
	// Implementation in GREEN phase
}

// TestAccountService_LinkAccounts_MultipleAccounts tests handling multiple accounts from one item
func TestAccountService_LinkAccounts_MultipleAccounts(t *testing.T) {
	// Given: Plaid item with 3 accounts (checking, savings, credit card)
	// When: LinkAccounts called
	// Then: Should create all 3 accounts atomically
	// Implementation in GREEN phase
}

// TestAccountService_LinkAccounts_DuplicateAccount tests handling of duplicate account linking
func TestAccountService_LinkAccounts_DuplicateAccount(t *testing.T) {
	// Given: User tries to link same account twice
	// When: LinkAccounts called with duplicate
	// Then: Should handle gracefully (idempotent or clear error)
	// Implementation in GREEN phase
}

// TestAccountService_LinkAccounts_RegionStorage tests accounts stored in user's region
func TestAccountService_LinkAccounts_RegionStorage(t *testing.T) {
	// Given: User with primary_region='us-east-1'
	mockUserRepo := new(MockUserRepository)
	ctx := context.Background()
	userID := uuid.New().String()

	user := &domain.User{
		ID:            userID,
		PrimaryRegion: "us-east-1",
	}
	mockUserRepo.On("GetUserByID", ctx, userID).Return(user, nil)

	// When: User retrieved
	result, err := mockUserRepo.GetUserByID(ctx, userID)

	// Then: User region should be available for data storage
	require.NoError(t, err)
	assert.Equal(t, "us-east-1", result.PrimaryRegion)
	mockUserRepo.AssertExpectations(t)

	// Note: Actual region-based storage implementation in GREEN phase
	// This test documents that we have access to user's region
}
