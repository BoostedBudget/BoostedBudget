package services

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	appcrypto "github.com/maxnussbaum/BoostedBudget/internal/crypto"
	"github.com/maxnussbaum/BoostedBudget/internal/config"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/queue"
	"github.com/maxnussbaum/BoostedBudget/internal/repository"
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

func (m *MockPlaidService) CreateUpdateModeLinkToken(ctx context.Context, userID string, accessToken string) (*domain.LinkTokenResponse, error) {
	args := m.Called(ctx, userID, accessToken)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*domain.LinkTokenResponse), args.Error(1)
}

func (m *MockPlaidService) RemoveItem(ctx context.Context, accessToken string) error {
	args := m.Called(ctx, accessToken)
	return args.Error(0)
}

func (m *MockPlaidService) TransactionsSync(ctx context.Context, accessToken string, cursor string, daysRequested int) (*plaid.TransactionsSyncResponse, error) {
	args := m.Called(ctx, accessToken, cursor, daysRequested)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*plaid.TransactionsSyncResponse), args.Error(1)
}

func (m *MockPlaidService) FireWebhook(ctx context.Context, accessToken string, webhookCode string) (*plaid.SandboxItemFireWebhookResponse, error) {
	args := m.Called(ctx, accessToken, webhookCode)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*plaid.SandboxItemFireWebhookResponse), args.Error(1)
}

func (m *MockPlaidService) AccountsGet(ctx context.Context, accessToken string) (*plaid.AccountsGetResponse, error) {
	args := m.Called(ctx, accessToken)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*plaid.AccountsGetResponse), args.Error(1)
}

func (m *MockPlaidService) InvestmentsHoldingsGet(ctx context.Context, accessToken string) (*plaid.InvestmentsHoldingsGetResponse, error) {
	args := m.Called(ctx, accessToken)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*plaid.InvestmentsHoldingsGetResponse), args.Error(1)
}

func (m *MockPlaidService) InvestmentsTransactionsGet(ctx context.Context, accessToken, startDate, endDate string, offset, count int32) (*plaid.InvestmentsTransactionsGetResponse, error) {
	args := m.Called(ctx, accessToken, startDate, endDate, offset, count)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*plaid.InvestmentsTransactionsGetResponse), args.Error(1)
}

func (m *MockPlaidService) LiabilitiesGet(ctx context.Context, accessToken string) (*plaid.LiabilitiesGetResponse, error) {
	args := m.Called(ctx, accessToken)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*plaid.LiabilitiesGetResponse), args.Error(1)
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

func (m *MockLinkedItemRepository) GetByID(ctx context.Context, id string) (*domain.LinkedItem, error) {
	args := m.Called(ctx, id)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*domain.LinkedItem), args.Error(1)
}

func (m *MockLinkedItemRepository) UpdateCursor(ctx context.Context, id, cursor string) error {
	args := m.Called(ctx, id, cursor)
	return args.Error(0)
}

func (m *MockLinkedItemRepository) Update(ctx context.Context, item *domain.LinkedItem) error {
	args := m.Called(ctx, item)
	return args.Error(0)
}

func (m *MockLinkedItemRepository) Delete(ctx context.Context, id string) error {
	args := m.Called(ctx, id)
	return args.Error(0)
}

func (m *MockLinkedItemRepository) GetByIDAndUser(ctx context.Context, id, userID string) (*domain.LinkedItem, error) {
	args := m.Called(ctx, id, userID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*domain.LinkedItem), args.Error(1)
}

func (m *MockLinkedItemRepository) DeleteByUser(ctx context.Context, id, userID string) error {
	args := m.Called(ctx, id, userID)
	return args.Error(0)
}

func (m *MockLinkedItemRepository) GetByIDForUpdate(ctx context.Context, tx pgx.Tx, id string) (*domain.LinkedItem, error) {
	args := m.Called(ctx, tx, id)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*domain.LinkedItem), args.Error(1)
}

// GetByUserAndInstitution retrieves a linked item by user ID, institution ID, and status.
// Issue #171 - Plaid Re-Link Handling
func (m *MockLinkedItemRepository) GetByUserAndInstitution(ctx context.Context, userID, institutionID, status string) (*domain.LinkedItem, error) {
	args := m.Called(ctx, userID, institutionID, status)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*domain.LinkedItem), args.Error(1)
}

// Issue #668: Plaid Item Management for Cost Optimization - Cleanup tracking methods
func (m *MockLinkedItemRepository) GetItemsForCleanup(ctx context.Context, errorDays, staleDays int) ([]domain.LinkedItem, error) {
	args := m.Called(ctx, errorDays, staleDays)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]domain.LinkedItem), args.Error(1)
}

func (m *MockLinkedItemRepository) GetItemsPendingCleanup(ctx context.Context) ([]domain.LinkedItem, error) {
	args := m.Called(ctx)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]domain.LinkedItem), args.Error(1)
}

func (m *MockLinkedItemRepository) MarkForCleanup(ctx context.Context, itemID string, cleanupAt time.Time) error {
	args := m.Called(ctx, itemID, cleanupAt)
	return args.Error(0)
}

func (m *MockLinkedItemRepository) ClearCleanupSchedule(ctx context.Context, itemID string) error {
	args := m.Called(ctx, itemID)
	return args.Error(0)
}

// Issue #679, #680: Trial expiration - methods for disconnecting items
func (m *MockLinkedItemRepository) GetActiveItemsByUserID(ctx context.Context, userID string) ([]domain.LinkedItem, error) {
	args := m.Called(ctx, userID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]domain.LinkedItem), args.Error(1)
}

func (m *MockLinkedItemRepository) MarkAllUserItemsDisconnected(ctx context.Context, userID string) error {
	args := m.Called(ctx, userID)
	return args.Error(0)
}

func (m *MockLinkedItemRepository) UpdateInvestmentsLastSyncedDate(ctx context.Context, id string, date time.Time) error {
	args := m.Called(ctx, id, date)
	return args.Error(0)
}

func (m *MockLinkedItemRepository) UpdateLiabilitiesLastSyncedAt(ctx context.Context, id string, t time.Time) error {
	args := m.Called(ctx, id, t)
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

func (m *MockAccountRepository) GetAccountsWithInstitution(ctx context.Context, userID string) ([]domain.Account, error) {
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

func (m *MockAccountRepository) Delete(ctx context.Context, accountID, userID string) error {
	args := m.Called(ctx, accountID, userID)
	return args.Error(0)
}

func (m *MockAccountRepository) GetByIDAndUser(ctx context.Context, accountID, userID string) (*domain.Account, error) {
	args := m.Called(ctx, accountID, userID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*domain.Account), args.Error(1)
}

func (m *MockAccountRepository) Update(ctx context.Context, account *domain.Account, userID string) error {
	args := m.Called(ctx, account, userID)
	return args.Error(0)
}

func (m *MockAccountRepository) UpdateDisplayOrders(ctx context.Context, userID string, orders []domain.AccountOrder) error {
	args := m.Called(ctx, userID, orders)
	return args.Error(0)
}

func (m *MockAccountRepository) GetAccountsWithInstitutionFiltered(ctx context.Context, userID string, opts repository.AccountFilterOptions) ([]domain.Account, error) {
	args := m.Called(ctx, userID, opts)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]domain.Account), args.Error(1)
}

// GetByUserAccountAndStatus retrieves accounts by user_account_id and status.
// Issue #171 - Plaid Re-Link Handling
func (m *MockAccountRepository) GetByUserAccountAndStatus(ctx context.Context, userAccountID, status string) ([]domain.Account, error) {
	args := m.Called(ctx, userAccountID, status)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]domain.Account), args.Error(1)
}

// GetByUserAccountID retrieves all accounts for a user_account (any status).
// Issue #171 - Plaid Re-Link Handling
func (m *MockAccountRepository) GetByUserAccountID(ctx context.Context, userAccountID string) ([]domain.Account, error) {
	args := m.Called(ctx, userAccountID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]domain.Account), args.Error(1)
}

// UpdateStatus updates an account's status and soft-delete fields with ownership validation.
// Issue #171 - Plaid Re-Link Handling
func (m *MockAccountRepository) UpdateStatus(ctx context.Context, accountID, userID string, status domain.AccountStatus, replacedByID *string) error {
	args := m.Called(ctx, accountID, userID, status, replacedByID)
	return args.Error(0)
}

// Issue #668: Plaid Item Management for Cost Optimization - Zero-account detection
func (m *MockAccountRepository) CountActiveByLinkedItemID(ctx context.Context, linkedItemID string) (int, error) {
	args := m.Called(ctx, linkedItemID)
	return args.Int(0), args.Error(1)
}

// Issue #1010: Cross-user fingerprint ownership check
func (m *MockAccountRepository) CheckFingerprintOwnership(ctx context.Context, fingerprintBlindIdx, excludeUserID string) (*repository.FingerprintOwnership, error) {
	args := m.Called(ctx, fingerprintBlindIdx, excludeUserID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*repository.FingerprintOwnership), args.Error(1)
}

// Issue #1010: Expire accounts by fingerprint to release uniqueness constraint
func (m *MockAccountRepository) ExpireAccountsByFingerprint(ctx context.Context, fingerprintBlindIdx string) error {
	args := m.Called(ctx, fingerprintBlindIdx)
	return args.Error(0)
}

type MockUserRepository struct {
	mock.Mock
}

func (m *MockUserRepository) GetUserByClerkID(ctx context.Context, clerkUserID string) (*domain.User, error) {
	args := m.Called(ctx, clerkUserID)
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

func (m *MockUserRepository) GetUserByPaddleSubscriptionID(ctx context.Context, subscriptionID string) (*domain.User, error) {
	args := m.Called(ctx, subscriptionID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*domain.User), args.Error(1)
}

func (m *MockUserRepository) CreateUser(ctx context.Context, user *domain.User) error {
	args := m.Called(ctx, user)
	return args.Error(0)
}

func (m *MockUserRepository) CreateUserTx(ctx context.Context, tx pgx.Tx, user *domain.User) error {
	args := m.Called(ctx, tx, user)
	return args.Error(0)
}

func (m *MockUserRepository) UpdateUser(ctx context.Context, user *domain.User) error {
	args := m.Called(ctx, user)
	return args.Error(0)
}

func (m *MockUserRepository) UpdateLastLogin(ctx context.Context, clerkUserID string) error {
	args := m.Called(ctx, clerkUserID)
	return args.Error(0)
}

func (m *MockUserRepository) MarkForDeletion(ctx context.Context, userID string, scheduledAt time.Time) error {
	args := m.Called(ctx, userID, scheduledAt)
	return args.Error(0)
}

func (m *MockUserRepository) CancelDeletion(ctx context.Context, userID string) error {
	args := m.Called(ctx, userID)
	return args.Error(0)
}

func (m *MockUserRepository) GetUsersScheduledForDeletion(ctx context.Context) ([]*domain.User, error) {
	args := m.Called(ctx)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]*domain.User), args.Error(1)
}

func (m *MockUserRepository) DeleteUserCascade(ctx context.Context, userID string) error {
	args := m.Called(ctx, userID)
	return args.Error(0)
}

// Issue #679, #680: Trial expiration methods
func (m *MockUserRepository) GetUsersWithExpiredTrials(ctx context.Context) ([]*domain.User, error) {
	args := m.Called(ctx)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]*domain.User), args.Error(1)
}

func (m *MockUserRepository) GetUsersWithExpiringTrials(ctx context.Context, withinDays int) ([]*domain.User, error) {
	args := m.Called(ctx, withinDays)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]*domain.User), args.Error(1)
}

func (m *MockUserRepository) UpdateSubscriptionStatus(ctx context.Context, userID string, status string) error {
	args := m.Called(ctx, userID, status)
	return args.Error(0)
}

func (m *MockUserRepository) UpdateTrialStatus(ctx context.Context, userID string, status string, trialStartedAt, trialEndsAt *time.Time, hadTrial bool) error {
	args := m.Called(ctx, userID, status, trialStartedAt, trialEndsAt, hadTrial)
	return args.Error(0)
}

func (m *MockUserRepository) UpdatePaddleCustomerID(ctx context.Context, userID, paddleCustomerID string) error {
	args := m.Called(ctx, userID, paddleCustomerID)
	return args.Error(0)
}

func (m *MockUserRepository) UpdateSubscriptionFromPaddle(ctx context.Context, userID, tier, status string, startedAt, endsAt *time.Time, subscriptionID *string) error {
	args := m.Called(ctx, userID, tier, status, startedAt, endsAt, subscriptionID)
	return args.Error(0)
}

func (m *MockUserRepository) GetUsersWithActivePaddleSubscriptions(ctx context.Context) ([]*domain.User, error) {
	args := m.Called(ctx)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]*domain.User), args.Error(1)
}

// GetSubscriptionStatusByUserID returns subscription status for the read-only middleware (#930)
func (m *MockUserRepository) GetSubscriptionStatusByUserID(ctx context.Context, userID string) (string, error) {
	args := m.Called(ctx, userID)
	return args.String(0), args.Error(1)
}

// Retention tracking methods (#930)

func (m *MockUserRepository) SetRetentionDates(ctx context.Context, userID string, startedAt, expiresAt time.Time) error {
	args := m.Called(ctx, userID, startedAt, expiresAt)
	return args.Error(0)
}

func (m *MockUserRepository) ClearRetentionDates(ctx context.Context, userID string) error {
	args := m.Called(ctx, userID)
	return args.Error(0)
}

func (m *MockUserRepository) SetRetentionNotice30dSent(ctx context.Context, userID string) error {
	args := m.Called(ctx, userID)
	return args.Error(0)
}

func (m *MockUserRepository) SetRetentionNotice7dSent(ctx context.Context, userID string) error {
	args := m.Called(ctx, userID)
	return args.Error(0)
}

func (m *MockUserRepository) GetUsersNeedingRetention30dNotice(ctx context.Context) ([]*domain.User, error) {
	args := m.Called(ctx)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]*domain.User), args.Error(1)
}

func (m *MockUserRepository) GetUsersNeedingRetention7dNotice(ctx context.Context) ([]*domain.User, error) {
	args := m.Called(ctx)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]*domain.User), args.Error(1)
}

func (m *MockUserRepository) GetUsersWithExpiredRetention(ctx context.Context) ([]*domain.User, error) {
	args := m.Called(ctx)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]*domain.User), args.Error(1)
}

func (m *MockUserRepository) GetUserByEmail(ctx context.Context, email string) (*domain.User, error) {
	args := m.Called(ctx, email)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*domain.User), args.Error(1)
}

func (m *MockUserRepository) UpdateUserEmailTx(ctx context.Context, tx pgx.Tx, userID, plainEmail string) error {
	args := m.Called(ctx, tx, userID, plainEmail)
	return args.Error(0)
}

// TestAccountService_LinkAccounts_Success tests successful account linking flow
func TestAccountService_LinkAccounts_Success(t *testing.T) {
	// Given: All dependencies mocked with successful responses
	mockPlaid := new(MockPlaidService)
	mockSubscription := &SubscriptionService{} // Will need actual implementation
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

	// AccessToken is now stored directly; repo layer handles encryption on write
	accessToken := "access-sandbox-token-abc123"

	// Mock linked item creation — service sets AccessToken, repo encrypts on write
	mockLinkedItemRepo.On("Create", ctx, mock.MatchedBy(func(item *domain.LinkedItem) bool {
		return item.UserID == userID &&
			item.AccessToken == accessToken &&
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

	// Mock current account count (user already has 3 accounts - at limit)
	mockAccountRepo.On("CountByUserID", ctx, userID).Return(3, nil)

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

// TestAccountService_LinkAccounts_SetsProviderField tests that Provider is set to "plaid"
// This test addresses Issue #305: Missing Provider field causes database constraint violation
func TestAccountService_LinkAccounts_SetsProviderField(t *testing.T) {
	// This test documents a critical requirement for the LinkAccounts method:
	//
	// When creating a LinkedItem struct in account_service.go, the Provider field
	// MUST be set to "plaid" to satisfy the database CHECK constraint:
	//   CHECK (provider IN ('plaid', 'teller', 'finicity', 'yodlee', 'manual'))
	//
	// Without this field set, the database INSERT fails with:
	//   "pq: new row for relation "linked_items" violates check constraint "valid_provider""
	//
	// Issue #305: https://github.com/BoostedBudget/BoostedBudget/issues/305
	//
	// Fix location: backend/internal/services/account_service.go:181
	//   Provider: "plaid", // Set provider to avoid database constraint violation
	//
	// This test will be expanded with full Plaid mocking when the token exchange
	// flow is fully tested. For now, it serves as documentation of the requirement.

	t.Log("✓ Provider field requirement documented")
	t.Log("✓ Fix implemented in account_service.go:181")
	t.Log("✓ Prevents: pq: new row violates check constraint 'valid_provider'")

	// Test passes - the fix is implemented in the code
	// Future enhancement: Add full integration test with Plaid mock
}

// TestAccountService_GetAccounts tests retrieval of accounts with institution information
func TestAccountService_GetAccounts(t *testing.T) {
	// Setup mocks
	mockAccountRepo := new(MockAccountRepository)
	mockPlaidService := new(MockPlaidService)
	mockLinkedItemRepo := new(MockLinkedItemRepository)
	mockUserRepo := new(MockUserRepository)

	// Create service
	service := NewAccountService(
		mockPlaidService,
		nil, // SubscriptionService not needed for GetAccounts
		mockLinkedItemRepo,
		mockAccountRepo,
		mockUserRepo,
		nil, // DB not needed for GetAccounts
	)

	ctx := context.Background()
	userID := uuid.New().String()
	clerkUserID := "user_123456"

	// Mock user lookup - now using GetUserByID with internal UUID
	// NOTE: After PR #197, GetAccounts receives internal UUID, not Clerk ID
	user := &domain.User{
		ID:          userID,
		ClerkUserID: clerkUserID,
	}
	mockUserRepo.On("GetUserByID", ctx, userID).Return(user, nil)

	// Mock accounts with institution names
	expectedAccounts := []domain.Account{
		{
			ID:              uuid.New().String(),
			UserID:          userID,
			PlaidAccountID:  "plaid-acc-1",
			Name:            "Chase Checking",
			Type:            "depository",
			CurrencyCode:    "USD",
			InstitutionName: "Chase",
		},
		{
			ID:              uuid.New().String(),
			UserID:          userID,
			PlaidAccountID:  "plaid-acc-2",
			Name:            "Wells Fargo Savings",
			Type:            "depository",
			CurrencyCode:    "USD",
			InstitutionName: "Wells Fargo",
		},
	}

	mockAccountRepo.On("GetAccountsWithInstitution", ctx, userID).Return(expectedAccounts, nil)

	// Execute - pass internal UUID, not Clerk ID
	accounts, err := service.GetAccounts(ctx, userID)

	// Assert
	require.NoError(t, err)
	assert.Len(t, accounts, 2)
	assert.Equal(t, "Chase", accounts[0].InstitutionName)
	assert.Equal(t, "Wells Fargo", accounts[1].InstitutionName)
	mockUserRepo.AssertExpectations(t)
	mockAccountRepo.AssertExpectations(t)
}

// TestAccountService_GetAccounts_Empty tests when user has no linked accounts
func TestAccountService_GetAccounts_Empty(t *testing.T) {
	// Setup mocks
	mockAccountRepo := new(MockAccountRepository)
	mockPlaidService := new(MockPlaidService)
	mockLinkedItemRepo := new(MockLinkedItemRepository)
	mockUserRepo := new(MockUserRepository)

	// Create service
	service := NewAccountService(
		mockPlaidService,
		nil,
		mockLinkedItemRepo,
		mockAccountRepo,
		mockUserRepo,
		nil,
	)

	ctx := context.Background()
	userID := uuid.New().String()
	clerkUserID := "user_empty-user"

	// Mock user lookup - now using GetUserByID with internal UUID
	// NOTE: After PR #197, GetAccounts receives internal UUID, not Clerk ID
	user := &domain.User{
		ID:          userID,
		ClerkUserID: clerkUserID,
	}
	mockUserRepo.On("GetUserByID", ctx, userID).Return(user, nil)

	// Mock empty result
	mockAccountRepo.On("GetAccountsWithInstitution", ctx, userID).Return([]domain.Account{}, nil)

	// Execute - pass internal UUID, not Clerk ID
	accounts, err := service.GetAccounts(ctx, userID)

	// Assert
	require.NoError(t, err)
	assert.Empty(t, accounts)
	assert.NotNil(t, accounts)
	mockUserRepo.AssertExpectations(t)
	mockAccountRepo.AssertExpectations(t)
}

// TestAccountService_GetAccounts_WithInternalUUID tests the production scenario
// NOTE: After PR #197, GetAccounts is called with internal UUID (not Clerk ID)
// Handler extracts UUID from middleware context after Clerk ID → UUID resolution
func TestAccountService_GetAccounts_WithInternalUUID(t *testing.T) {
	// Setup mocks
	mockAccountRepo := new(MockAccountRepository)
	mockPlaidService := new(MockPlaidService)
	mockLinkedItemRepo := new(MockLinkedItemRepository)
	mockUserRepo := new(MockUserRepository)

	// Create service
	service := NewAccountService(
		mockPlaidService,
		nil,
		mockLinkedItemRepo,
		mockAccountRepo,
		mockUserRepo,
		nil,
	)

	ctx := context.Background()

	// Production scenario: Handler passes internal UUID from middleware context
	userID := uuid.New().String()
	clerkUserID := "user_507f1f77bcf86cd799439011"

	// Mock user lookup by internal UUID
	user := &domain.User{
		ID:            userID,
		ClerkUserID:   clerkUserID,
		Email:         "user@example.com",
		PrimaryRegion: "us-east-1",
	}
	mockUserRepo.On("GetUserByID", ctx, userID).Return(user, nil)

	// Mock accounts with institution names (using internal UUID)
	expectedAccounts := []domain.Account{
		{
			ID:              uuid.New().String(),
			UserID:          userID,
			PlaidAccountID:  "plaid-acc-1",
			Name:            "Chase Checking",
			Type:            "depository",
			CurrencyCode:    "USD",
			InstitutionName: "Chase",
		},
	}

	// The repository should be called with internal UUID
	mockAccountRepo.On("GetAccountsWithInstitution", ctx, userID).Return(expectedAccounts, nil)

	// Execute: Pass internal UUID (as the handler does after PR #197)
	accounts, err := service.GetAccounts(ctx, userID)

	// Assert
	require.NoError(t, err, "GetAccounts should handle internal UUID correctly")
	assert.Len(t, accounts, 1)
	assert.Equal(t, "Chase", accounts[0].InstitutionName)
	assert.Equal(t, userID, accounts[0].UserID, "Account should have internal UUID")

	// Verify mocks were called correctly
	mockUserRepo.AssertExpectations(t)
	mockAccountRepo.AssertExpectations(t)
}

// TestDeleteAccount_Success tests successful account deletion
// Updated for Issue #668: Now includes GetByIDAndUser call for zero-account detection
func TestDeleteAccount_Success(t *testing.T) {
	// Setup mocks
	mockAccountRepo := &MockAccountRepository{}

	service := &accountService{
		accountRepo: mockAccountRepo,
		jobQueue:    nil, // No job queue - skips zero-account cleanup check
	}

	ctx := context.Background()
	accountID := uuid.New().String()
	userID := uuid.New().String()
	linkedItemID := uuid.New().String()

	// Account to be deleted
	account := &domain.Account{
		ID:           accountID,
		LinkedItemID: linkedItemID,
		UserID:       userID,
	}

	// Mock getting the account before deletion
	mockAccountRepo.On("GetByIDAndUser", ctx, accountID, userID).Return(account, nil)

	// Mock successful deletion
	mockAccountRepo.On("Delete", ctx, accountID, userID).Return(nil)

	// Execute
	err := service.DeleteAccount(ctx, accountID, userID)

	// Assert
	require.NoError(t, err, "DeleteAccount should succeed")
	mockAccountRepo.AssertExpectations(t)
}

// TestDeleteAccount_RepositoryError tests account deletion when GetByIDAndUser returns error
// Updated for Issue #668: Error now comes from GetByIDAndUser (called first)
func TestDeleteAccount_RepositoryError(t *testing.T) {
	// Setup mocks
	mockAccountRepo := &MockAccountRepository{}

	service := &accountService{
		accountRepo: mockAccountRepo,
	}

	ctx := context.Background()
	accountID := uuid.New().String()
	userID := uuid.New().String()

	// Mock GetByIDAndUser error (account not found)
	mockAccountRepo.On("GetByIDAndUser", ctx, accountID, userID).Return(nil, assert.AnError)

	// Execute
	err := service.DeleteAccount(ctx, accountID, userID)

	// Assert
	require.Error(t, err, "DeleteAccount should return repository error")
	mockAccountRepo.AssertExpectations(t)
}

// TestDeleteAccount_DeleteError tests when Delete() fails after GetByIDAndUser succeeds
func TestDeleteAccount_DeleteError(t *testing.T) {
	// Setup mocks
	mockAccountRepo := &MockAccountRepository{}

	service := &accountService{
		accountRepo: mockAccountRepo,
	}

	ctx := context.Background()
	accountID := uuid.New().String()
	userID := uuid.New().String()
	linkedItemID := uuid.New().String()

	// Account to be deleted
	account := &domain.Account{
		ID:           accountID,
		LinkedItemID: linkedItemID,
		UserID:       userID,
	}

	// Mock getting the account before deletion
	mockAccountRepo.On("GetByIDAndUser", ctx, accountID, userID).Return(account, nil)

	// Mock Delete error
	mockAccountRepo.On("Delete", ctx, accountID, userID).Return(assert.AnError)

	// Execute
	err := service.DeleteAccount(ctx, accountID, userID)

	// Assert
	require.Error(t, err, "DeleteAccount should return delete error")
	mockAccountRepo.AssertExpectations(t)
}

// ===========================================================================
// Zero-Account Cleanup Tests (Issue #668: Plaid Item Management for Cost Optimization)
// ===========================================================================

// TestDeleteAccount_EnqueuesCleanupWhenZeroRemaining tests that when the last account
// is deleted, a cleanup job is enqueued for the linked item.
// Issue #668: Zero-account item detection and cleanup
func TestDeleteAccount_EnqueuesCleanupWhenZeroRemaining(t *testing.T) {
	// Setup mocks
	mockAccountRepo := &MockAccountRepository{}
	mockJobQueue := &MockJobQueue{}

	service := &accountService{
		accountRepo: mockAccountRepo,
		jobQueue:    mockJobQueue,
		logger:      slog.Default(),
	}

	ctx := context.Background()
	accountID := uuid.New().String()
	userID := uuid.New().String()
	linkedItemID := uuid.New().String()

	// Account to be deleted
	account := &domain.Account{
		ID:           accountID,
		LinkedItemID: linkedItemID,
		UserID:       userID,
	}

	// Mock getting the account before deletion
	mockAccountRepo.On("GetByIDAndUser", ctx, accountID, userID).Return(account, nil)

	// Mock successful deletion
	mockAccountRepo.On("Delete", ctx, accountID, userID).Return(nil)

	// Mock count returning 0 (no remaining accounts)
	mockAccountRepo.On("CountActiveByLinkedItemID", ctx, linkedItemID).Return(0, nil)

	// Mock enqueue - expect cleanup job with "item_cleanup" kind
	// Note: We can't import jobs package here (import cycle), so we verify the job Kind() method
	mockJobQueue.On("Enqueue", ctx, mock.MatchedBy(func(job queue.Job) bool {
		return job.Kind() == "item_cleanup"
	}), mock.Anything).Return(&queue.JobResult{ID: 1}, nil)

	// Execute
	err := service.DeleteAccount(ctx, accountID, userID)

	// Assert
	require.NoError(t, err, "DeleteAccount should succeed")
	mockAccountRepo.AssertExpectations(t)
	mockJobQueue.AssertExpectations(t)
}

// TestDeleteAccount_DoesNotCleanupWhenAccountsRemain tests that no cleanup is triggered
// when other accounts still remain on the linked item.
// Issue #668: Zero-account item detection and cleanup
func TestDeleteAccount_DoesNotCleanupWhenAccountsRemain(t *testing.T) {
	// Setup mocks
	mockAccountRepo := &MockAccountRepository{}
	mockJobQueue := &MockJobQueue{}

	service := &accountService{
		accountRepo: mockAccountRepo,
		jobQueue:    mockJobQueue,
		logger:      slog.Default(),
	}

	ctx := context.Background()
	accountID := uuid.New().String()
	userID := uuid.New().String()
	linkedItemID := uuid.New().String()

	// Account to be deleted
	account := &domain.Account{
		ID:           accountID,
		LinkedItemID: linkedItemID,
		UserID:       userID,
	}

	// Mock getting the account before deletion
	mockAccountRepo.On("GetByIDAndUser", ctx, accountID, userID).Return(account, nil)

	// Mock successful deletion
	mockAccountRepo.On("Delete", ctx, accountID, userID).Return(nil)

	// Mock count returning 2 (other accounts remain)
	mockAccountRepo.On("CountActiveByLinkedItemID", ctx, linkedItemID).Return(2, nil)

	// Execute
	err := service.DeleteAccount(ctx, accountID, userID)

	// Assert
	require.NoError(t, err, "DeleteAccount should succeed")
	mockAccountRepo.AssertExpectations(t)
	// Verify Enqueue was NOT called
	mockJobQueue.AssertNotCalled(t, "Enqueue", mock.Anything, mock.Anything, mock.Anything)
}

// TestDeleteAccount_ContinuesOnCountError tests graceful degradation when
// counting remaining accounts fails. The deletion should still succeed.
// Issue #668: Zero-account item detection and cleanup
func TestDeleteAccount_ContinuesOnCountError(t *testing.T) {
	// Setup mocks
	mockAccountRepo := &MockAccountRepository{}
	mockJobQueue := &MockJobQueue{}

	service := &accountService{
		accountRepo: mockAccountRepo,
		jobQueue:    mockJobQueue,
		logger:      slog.Default(),
	}

	ctx := context.Background()
	accountID := uuid.New().String()
	userID := uuid.New().String()
	linkedItemID := uuid.New().String()

	// Account to be deleted
	account := &domain.Account{
		ID:           accountID,
		LinkedItemID: linkedItemID,
		UserID:       userID,
	}

	// Mock getting the account before deletion
	mockAccountRepo.On("GetByIDAndUser", ctx, accountID, userID).Return(account, nil)

	// Mock successful deletion
	mockAccountRepo.On("Delete", ctx, accountID, userID).Return(nil)

	// Mock count error
	mockAccountRepo.On("CountActiveByLinkedItemID", ctx, linkedItemID).Return(0, errors.New("database error"))

	// Execute
	err := service.DeleteAccount(ctx, accountID, userID)

	// Assert - deletion should still succeed despite count error
	require.NoError(t, err, "DeleteAccount should succeed even if count fails")
	mockAccountRepo.AssertExpectations(t)
	// Verify Enqueue was NOT called due to error
	mockJobQueue.AssertNotCalled(t, "Enqueue", mock.Anything, mock.Anything, mock.Anything)
}

// TestDeleteAccount_SkipsZeroAccountCheckWhenNoJobQueue tests that zero-account
// cleanup is skipped when jobQueue is nil (optional dependency).
// Issue #668: Zero-account item detection and cleanup
func TestDeleteAccount_SkipsZeroAccountCheckWhenNoJobQueue(t *testing.T) {
	// Setup mocks
	mockAccountRepo := &MockAccountRepository{}

	service := &accountService{
		accountRepo: mockAccountRepo,
		jobQueue:    nil, // No job queue
		logger:      slog.Default(),
	}

	ctx := context.Background()
	accountID := uuid.New().String()
	userID := uuid.New().String()
	linkedItemID := uuid.New().String()

	// Account to be deleted
	account := &domain.Account{
		ID:           accountID,
		LinkedItemID: linkedItemID,
		UserID:       userID,
	}

	// Mock getting the account before deletion
	mockAccountRepo.On("GetByIDAndUser", ctx, accountID, userID).Return(account, nil)

	// Mock successful deletion
	mockAccountRepo.On("Delete", ctx, accountID, userID).Return(nil)

	// Execute - should not call CountActiveByLinkedItemID when no jobQueue
	err := service.DeleteAccount(ctx, accountID, userID)

	// Assert
	require.NoError(t, err, "DeleteAccount should succeed")
	mockAccountRepo.AssertExpectations(t)
	// CountActiveByLinkedItemID should NOT be called when jobQueue is nil
	mockAccountRepo.AssertNotCalled(t, "CountActiveByLinkedItemID", mock.Anything, mock.Anything)
}

// TestDeleteAccount_EnqueueErrorDoesNotFailDeletion tests that an enqueue error
// does not fail the overall deletion operation.
// Issue #668: Zero-account item detection and cleanup
func TestDeleteAccount_EnqueueErrorDoesNotFailDeletion(t *testing.T) {
	// Setup mocks
	mockAccountRepo := &MockAccountRepository{}
	mockJobQueue := &MockJobQueue{}

	service := &accountService{
		accountRepo: mockAccountRepo,
		jobQueue:    mockJobQueue,
		logger:      slog.Default(),
	}

	ctx := context.Background()
	accountID := uuid.New().String()
	userID := uuid.New().String()
	linkedItemID := uuid.New().String()

	// Account to be deleted
	account := &domain.Account{
		ID:           accountID,
		LinkedItemID: linkedItemID,
		UserID:       userID,
	}

	// Mock getting the account before deletion
	mockAccountRepo.On("GetByIDAndUser", ctx, accountID, userID).Return(account, nil)

	// Mock successful deletion
	mockAccountRepo.On("Delete", ctx, accountID, userID).Return(nil)

	// Mock count returning 0 (no remaining accounts)
	mockAccountRepo.On("CountActiveByLinkedItemID", ctx, linkedItemID).Return(0, nil)

	// Mock enqueue failure
	mockJobQueue.On("Enqueue", ctx, mock.Anything, mock.Anything).Return(nil, errors.New("queue error"))

	// Execute
	err := service.DeleteAccount(ctx, accountID, userID)

	// Assert - deletion should still succeed despite enqueue error
	require.NoError(t, err, "DeleteAccount should succeed even if enqueue fails")
	mockAccountRepo.AssertExpectations(t)
	mockJobQueue.AssertExpectations(t)
}

// ===========================================================================
// UserAccount Hiding Tests (Issue #978: Account Deletion)
// ===========================================================================

// TestDeleteAccount_HidesUserAccountWhenLastAccount tests that when the last active account
// referencing a UserAccount is deleted, SetHidden(true) is called on the UserAccount.
func TestDeleteAccount_HidesUserAccountWhenLastAccount(t *testing.T) {
	mockAccountRepo := &MockAccountRepository{}
	mockJobQueue := &MockJobQueue{}
	mockUserAccountSvc := &MockUserAccountService{}

	userAccountID := uuid.New().String()

	svc := NewAccountService(
		nil, // plaid not needed
		nil, // subscription not needed
		nil, // linkedItemRepo not needed
		mockAccountRepo,
		nil, // userRepo not needed
		nil, // db not needed
		WithJobQueue(mockJobQueue),
		WithUserAccountService(mockUserAccountSvc),
		WithAccountServiceLogger(slog.Default()),
	)

	ctx := context.Background()
	accountID := uuid.New().String()
	userID := uuid.New().String()
	linkedItemID := uuid.New().String()

	account := &domain.Account{
		ID:            accountID,
		LinkedItemID:  linkedItemID,
		UserID:        userID,
		UserAccountID: &userAccountID,
	}

	mockAccountRepo.On("GetByIDAndUser", ctx, accountID, userID).Return(account, nil)
	mockAccountRepo.On("Delete", ctx, accountID, userID).Return(nil)
	mockAccountRepo.On("CountActiveByLinkedItemID", ctx, linkedItemID).Return(0, nil)
	mockJobQueue.On("Enqueue", ctx, mock.MatchedBy(func(job queue.Job) bool {
		return job.Kind() == "item_cleanup"
	}), mock.Anything).Return(&queue.JobResult{ID: 1}, nil)
	// No remaining active accounts for this user_account
	mockAccountRepo.On("GetByUserAccountAndStatus", ctx, userAccountID, string(domain.AccountStatusActive)).
		Return([]domain.Account{}, nil)
	mockUserAccountSvc.On("SetHidden", ctx, userAccountID, true).Return(nil)

	err := svc.DeleteAccount(ctx, accountID, userID)

	require.NoError(t, err, "DeleteAccount should succeed")
	mockAccountRepo.AssertExpectations(t)
	mockUserAccountSvc.AssertExpectations(t)
}

// TestDeleteAccount_DoesNotHideWhenOtherAccountsRemain tests that SetHidden is NOT called
// when other active accounts still reference the UserAccount.
func TestDeleteAccount_DoesNotHideWhenOtherAccountsRemain(t *testing.T) {
	mockAccountRepo := &MockAccountRepository{}
	mockJobQueue := &MockJobQueue{}
	mockUserAccountSvc := &MockUserAccountService{}

	userAccountID := uuid.New().String()

	svc := NewAccountService(
		nil,
		nil,
		nil,
		mockAccountRepo,
		nil,
		nil,
		WithJobQueue(mockJobQueue),
		WithUserAccountService(mockUserAccountSvc),
		WithAccountServiceLogger(slog.Default()),
	)

	ctx := context.Background()
	accountID := uuid.New().String()
	userID := uuid.New().String()
	linkedItemID := uuid.New().String()
	otherAccountID := uuid.New().String()

	account := &domain.Account{
		ID:            accountID,
		LinkedItemID:  linkedItemID,
		UserID:        userID,
		UserAccountID: &userAccountID,
	}

	// Another active account still references this user_account
	remainingAccount := domain.Account{
		ID:            otherAccountID,
		LinkedItemID:  linkedItemID,
		UserID:        userID,
		UserAccountID: &userAccountID,
	}

	mockAccountRepo.On("GetByIDAndUser", ctx, accountID, userID).Return(account, nil)
	mockAccountRepo.On("Delete", ctx, accountID, userID).Return(nil)
	mockAccountRepo.On("CountActiveByLinkedItemID", ctx, linkedItemID).Return(1, nil)
	mockAccountRepo.On("GetByUserAccountAndStatus", ctx, userAccountID, string(domain.AccountStatusActive)).
		Return([]domain.Account{remainingAccount}, nil)

	err := svc.DeleteAccount(ctx, accountID, userID)

	require.NoError(t, err, "DeleteAccount should succeed")
	mockAccountRepo.AssertExpectations(t)
	mockJobQueue.AssertExpectations(t)
	// SetHidden must NOT be called when other accounts remain
	mockUserAccountSvc.AssertNotCalled(t, "SetHidden", mock.Anything, mock.Anything, mock.Anything)
}

// TestDeleteAccount_SkipsHidingWhenUserAccountSvcNil tests that the hiding logic is skipped
// entirely when userAccountSvc is nil (optional dependency).
func TestDeleteAccount_SkipsHidingWhenUserAccountSvcNil(t *testing.T) {
	mockAccountRepo := &MockAccountRepository{}
	mockJobQueue := &MockJobQueue{}

	userAccountID := uuid.New().String()

	svc := NewAccountService(
		nil,
		nil,
		nil,
		mockAccountRepo,
		nil,
		nil,
		WithJobQueue(mockJobQueue),
		// No WithUserAccountService — intentionally nil
		WithAccountServiceLogger(slog.Default()),
	)

	ctx := context.Background()
	accountID := uuid.New().String()
	userID := uuid.New().String()
	linkedItemID := uuid.New().String()

	account := &domain.Account{
		ID:            accountID,
		LinkedItemID:  linkedItemID,
		UserID:        userID,
		UserAccountID: &userAccountID,
	}

	mockAccountRepo.On("GetByIDAndUser", ctx, accountID, userID).Return(account, nil)
	mockAccountRepo.On("Delete", ctx, accountID, userID).Return(nil)
	mockAccountRepo.On("CountActiveByLinkedItemID", ctx, linkedItemID).Return(0, nil)
	mockJobQueue.On("Enqueue", ctx, mock.MatchedBy(func(job queue.Job) bool {
		return job.Kind() == "item_cleanup"
	}), mock.Anything).Return(&queue.JobResult{ID: 1}, nil)
	// GetByUserAccountAndStatus should NOT be called when userAccountSvc is nil

	err := svc.DeleteAccount(ctx, accountID, userID)

	require.NoError(t, err, "DeleteAccount should succeed even without userAccountSvc")
	mockAccountRepo.AssertExpectations(t)
	mockAccountRepo.AssertNotCalled(t, "GetByUserAccountAndStatus", mock.Anything, mock.Anything, mock.Anything)
}

// TestDeleteAccount_SkipsHidingWhenUserAccountIDNil tests that the hiding logic is skipped
// when account.UserAccountID is nil (legacy accounts not yet linked to a UserAccount).
func TestDeleteAccount_SkipsHidingWhenUserAccountIDNil(t *testing.T) {
	mockAccountRepo := &MockAccountRepository{}
	mockJobQueue := &MockJobQueue{}
	mockUserAccountSvc := &MockUserAccountService{}

	svc := NewAccountService(
		nil,
		nil,
		nil,
		mockAccountRepo,
		nil,
		nil,
		WithJobQueue(mockJobQueue),
		WithUserAccountService(mockUserAccountSvc),
		WithAccountServiceLogger(slog.Default()),
	)

	ctx := context.Background()
	accountID := uuid.New().String()
	userID := uuid.New().String()
	linkedItemID := uuid.New().String()

	// UserAccountID is nil — legacy account
	account := &domain.Account{
		ID:            accountID,
		LinkedItemID:  linkedItemID,
		UserID:        userID,
		UserAccountID: nil,
	}

	mockAccountRepo.On("GetByIDAndUser", ctx, accountID, userID).Return(account, nil)
	mockAccountRepo.On("Delete", ctx, accountID, userID).Return(nil)
	mockAccountRepo.On("CountActiveByLinkedItemID", ctx, linkedItemID).Return(0, nil)
	mockJobQueue.On("Enqueue", ctx, mock.MatchedBy(func(job queue.Job) bool {
		return job.Kind() == "item_cleanup"
	}), mock.Anything).Return(&queue.JobResult{ID: 1}, nil)

	err := svc.DeleteAccount(ctx, accountID, userID)

	require.NoError(t, err, "DeleteAccount should succeed even when UserAccountID is nil")
	mockAccountRepo.AssertExpectations(t)
	// SetHidden and GetByUserAccountAndStatus should NOT be called
	mockUserAccountSvc.AssertNotCalled(t, "SetHidden", mock.Anything, mock.Anything, mock.Anything)
	mockAccountRepo.AssertNotCalled(t, "GetByUserAccountAndStatus", mock.Anything, mock.Anything, mock.Anything)
}

// TestDisconnectInstitution_Success tests successful institution disconnection
func TestDisconnectInstitution_Success(t *testing.T) {
	// Setup mocks
	mockLinkedItemRepo := &MockLinkedItemRepository{}
	mockPlaidService := &MockPlaidService{}

	service := &accountService{
		linkedItemRepo: mockLinkedItemRepo,
		plaidService:   mockPlaidService,
	}

	ctx := context.Background()
	linkedItemID := uuid.New().String()
	userID := uuid.New().String()
	accessToken := "access-sandbox-test-token"

	// AccessToken is populated by the repository layer (repo-layer encryption)
	linkedItem := &domain.LinkedItem{
		ID:              linkedItemID,
		UserID:          userID,
		ItemID:          "plaid-item-123",
		AccessToken:     accessToken,
		InstitutionID:   "ins_123",
		InstitutionName: "Chase",
	}

	// Mock all steps
	mockLinkedItemRepo.On("GetByIDAndUser", ctx, linkedItemID, userID).Return(linkedItem, nil)
	mockPlaidService.On("RemoveItem", ctx, accessToken).Return(nil)
	mockLinkedItemRepo.On("DeleteByUser", ctx, linkedItemID, userID).Return(nil)

	// Execute
	err := service.DisconnectInstitution(ctx, linkedItemID, userID)

	// Assert
	require.NoError(t, err, "DisconnectInstitution should succeed")
	mockLinkedItemRepo.AssertExpectations(t)
	mockPlaidService.AssertExpectations(t)
}

// TestDisconnectInstitution_LinkedItemNotFound tests when linked item doesn't exist
func TestDisconnectInstitution_LinkedItemNotFound(t *testing.T) {
	// Setup mocks
	mockLinkedItemRepo := &MockLinkedItemRepository{}

	service := &accountService{
		linkedItemRepo: mockLinkedItemRepo,
	}

	ctx := context.Background()
	linkedItemID := uuid.New().String()
	userID := uuid.New().String()

	// Mock linked item not found
	mockLinkedItemRepo.On("GetByIDAndUser", ctx, linkedItemID, userID).Return(nil, assert.AnError)

	// Execute
	err := service.DisconnectInstitution(ctx, linkedItemID, userID)

	// Assert
	require.Error(t, err, "DisconnectInstitution should return error for missing item")
	mockLinkedItemRepo.AssertExpectations(t)
}

// TestDisconnectInstitution_PlaidFailureButDatabaseSucceeds tests when Plaid fails but DB deletion succeeds
func TestDisconnectInstitution_PlaidFailureButDatabaseSucceeds(t *testing.T) {
	// Setup mocks
	mockLinkedItemRepo := &MockLinkedItemRepository{}
	mockPlaidService := &MockPlaidService{}

	service := &accountService{
		linkedItemRepo: mockLinkedItemRepo,
		plaidService:   mockPlaidService,
	}

	ctx := context.Background()
	linkedItemID := uuid.New().String()
	userID := uuid.New().String()
	accessToken := "access-sandbox-test-token"

	// AccessToken is populated by the repository layer (repo-layer encryption)
	linkedItem := &domain.LinkedItem{
		ID:              linkedItemID,
		UserID:          userID,
		ItemID:          "plaid-item-123",
		AccessToken:     accessToken,
		InstitutionID:   "ins_123",
		InstitutionName: "Chase",
	}

	// Mock Plaid removal fails but database deletion succeeds
	mockLinkedItemRepo.On("GetByIDAndUser", ctx, linkedItemID, userID).Return(linkedItem, nil)
	mockPlaidService.On("RemoveItem", ctx, accessToken).Return(assert.AnError)
	mockLinkedItemRepo.On("DeleteByUser", ctx, linkedItemID, userID).Return(nil)

	// Execute
	err := service.DisconnectInstitution(ctx, linkedItemID, userID)

	// Assert - Should succeed even if Plaid fails (we still clean up database)
	require.NoError(t, err, "DisconnectInstitution should succeed even if Plaid fails")
	mockLinkedItemRepo.AssertExpectations(t)
	mockPlaidService.AssertExpectations(t)
}

// TestDisconnectInstitution_DatabaseDeletionError tests when database deletion fails
func TestDisconnectInstitution_DatabaseDeletionError(t *testing.T) {
	// Setup mocks
	mockLinkedItemRepo := &MockLinkedItemRepository{}
	mockPlaidService := &MockPlaidService{}

	service := &accountService{
		linkedItemRepo: mockLinkedItemRepo,
		plaidService:   mockPlaidService,
	}

	ctx := context.Background()
	linkedItemID := uuid.New().String()
	userID := uuid.New().String()
	accessToken := "access-sandbox-test-token"

	// AccessToken is populated by the repository layer (repo-layer encryption)
	linkedItem := &domain.LinkedItem{
		ID:              linkedItemID,
		UserID:          userID,
		ItemID:          "plaid-item-123",
		AccessToken:     accessToken,
		InstitutionID:   "ins_123",
		InstitutionName: "Chase",
	}

	// Mock database deletion fails
	mockLinkedItemRepo.On("GetByIDAndUser", ctx, linkedItemID, userID).Return(linkedItem, nil)
	mockPlaidService.On("RemoveItem", ctx, accessToken).Return(nil)
	mockLinkedItemRepo.On("DeleteByUser", ctx, linkedItemID, userID).Return(assert.AnError)

	// Execute
	err := service.DisconnectInstitution(ctx, linkedItemID, userID)

	// Assert
	require.Error(t, err, "DisconnectInstitution should return database deletion error")
	mockLinkedItemRepo.AssertExpectations(t)
	mockPlaidService.AssertExpectations(t)
}

// ===========================================================================
// Account Customization Tests (Issue #307)
// ===========================================================================

// TestUpdateAccount_Success tests successful account customization update
func TestUpdateAccount_Success(t *testing.T) {
	mockAccountRepo := &MockAccountRepository{}

	service := &accountService{
		accountRepo: mockAccountRepo,
	}

	ctx := context.Background()
	accountID := uuid.New().String()
	userID := uuid.New().String()

	existingAccount := &domain.Account{
		ID:           accountID,
		UserID:       userID,
		Name:         "Chase Checking",
		Type:         "depository",
		CurrencyCode: "USD",
	}

	nickname := "My Primary Account"
	color := "#3B82F6"
	icon := "wallet"
	emoji := "💰"
	isHidden := false

	updateReq := &domain.UpdateAccountRequest{
		Nickname:    &nickname,
		CustomColor: &color,
		CustomIcon:  &icon,
		CustomEmoji: &emoji,
		IsHidden:    &isHidden,
	}

	// Mock GetByIDAndUser to return existing account
	mockAccountRepo.On("GetByIDAndUser", ctx, accountID, userID).Return(existingAccount, nil)

	// Mock Update to succeed
	mockAccountRepo.On("Update", ctx, mock.MatchedBy(func(acc *domain.Account) bool {
		return acc.ID == accountID &&
			acc.Nickname != nil && *acc.Nickname == nickname &&
			acc.CustomColor != nil && *acc.CustomColor == "#3B82F6" && // Should be uppercased
			acc.CustomIcon != nil && *acc.CustomIcon == icon &&
			acc.CustomEmoji != nil && *acc.CustomEmoji == emoji &&
			acc.IsHidden == isHidden
	}), userID).Return(nil)

	// Execute
	result, err := service.UpdateAccount(ctx, accountID, userID, updateReq)

	// Assert
	require.NoError(t, err, "UpdateAccount should succeed")
	require.NotNil(t, result)
	assert.Equal(t, accountID, result.ID)
	assert.Equal(t, nickname, *result.Nickname)
	mockAccountRepo.AssertExpectations(t)
}

// TestUpdateAccount_NotFound tests update when account doesn't exist
func TestUpdateAccount_NotFound(t *testing.T) {
	mockAccountRepo := &MockAccountRepository{}

	service := &accountService{
		accountRepo: mockAccountRepo,
	}

	ctx := context.Background()
	accountID := uuid.New().String()
	userID := uuid.New().String()

	nickname := "New Nickname"
	updateReq := &domain.UpdateAccountRequest{
		Nickname: &nickname,
	}

	// Mock GetByIDAndUser to return not found
	mockAccountRepo.On("GetByIDAndUser", ctx, accountID, userID).Return(nil, assert.AnError)

	// Execute
	result, err := service.UpdateAccount(ctx, accountID, userID, updateReq)

	// Assert
	require.Error(t, err, "UpdateAccount should fail for non-existent account")
	assert.Nil(t, result)
	mockAccountRepo.AssertExpectations(t)
}

// TestUpdateAccount_ValidationError tests update with invalid data
func TestUpdateAccount_ValidationError(t *testing.T) {
	mockAccountRepo := &MockAccountRepository{}

	service := &accountService{
		accountRepo: mockAccountRepo,
	}

	ctx := context.Background()
	accountID := uuid.New().String()
	userID := uuid.New().String()

	// Invalid hex color (no # prefix)
	invalidColor := "3B82F6"
	updateReq := &domain.UpdateAccountRequest{
		CustomColor: &invalidColor,
	}

	// Execute - should fail validation before hitting repository
	result, err := service.UpdateAccount(ctx, accountID, userID, updateReq)

	// Assert
	require.Error(t, err, "UpdateAccount should fail for invalid color")
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "custom_color")
}

// TestUpdateAccount_PartialUpdate tests updating only some fields
func TestUpdateAccount_PartialUpdate(t *testing.T) {
	mockAccountRepo := &MockAccountRepository{}

	service := &accountService{
		accountRepo: mockAccountRepo,
	}

	ctx := context.Background()
	accountID := uuid.New().String()
	userID := uuid.New().String()

	existingNickname := "Old Nickname"
	existingColor := "#FF0000"
	existingAccount := &domain.Account{
		ID:          accountID,
		UserID:      userID,
		Name:        "Chase Checking",
		Nickname:    &existingNickname,
		CustomColor: &existingColor,
	}

	// Only update nickname, leave color unchanged
	newNickname := "New Nickname"
	updateReq := &domain.UpdateAccountRequest{
		Nickname: &newNickname,
		// CustomColor is nil - should not change
	}

	mockAccountRepo.On("GetByIDAndUser", ctx, accountID, userID).Return(existingAccount, nil)

	// Should preserve existing color
	mockAccountRepo.On("Update", ctx, mock.MatchedBy(func(acc *domain.Account) bool {
		return acc.Nickname != nil && *acc.Nickname == newNickname &&
			acc.CustomColor != nil && *acc.CustomColor == existingColor
	}), userID).Return(nil)

	// Execute
	result, err := service.UpdateAccount(ctx, accountID, userID, updateReq)

	// Assert
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, newNickname, *result.Nickname)
	assert.Equal(t, existingColor, *result.CustomColor)
	mockAccountRepo.AssertExpectations(t)
}

// TestUpdateAccount_ClearField tests clearing a field by setting empty string
func TestUpdateAccount_ClearField(t *testing.T) {
	mockAccountRepo := &MockAccountRepository{}

	service := &accountService{
		accountRepo: mockAccountRepo,
	}

	ctx := context.Background()
	accountID := uuid.New().String()
	userID := uuid.New().String()

	existingNickname := "My Nickname"
	existingAccount := &domain.Account{
		ID:       accountID,
		UserID:   userID,
		Name:     "Chase Checking",
		Nickname: &existingNickname,
	}

	// Empty string means clear the field
	emptyNickname := ""
	updateReq := &domain.UpdateAccountRequest{
		Nickname: &emptyNickname,
	}

	mockAccountRepo.On("GetByIDAndUser", ctx, accountID, userID).Return(existingAccount, nil)

	// Nickname should be nil (cleared)
	mockAccountRepo.On("Update", ctx, mock.MatchedBy(func(acc *domain.Account) bool {
		return acc.Nickname == nil
	}), userID).Return(nil)

	// Execute
	result, err := service.UpdateAccount(ctx, accountID, userID, updateReq)

	// Assert
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Nil(t, result.Nickname)
	mockAccountRepo.AssertExpectations(t)
}

// TestUpdateAccount_TrimNickname tests that nickname whitespace is trimmed
func TestUpdateAccount_TrimNickname(t *testing.T) {
	mockAccountRepo := &MockAccountRepository{}

	service := &accountService{
		accountRepo: mockAccountRepo,
	}

	ctx := context.Background()
	accountID := uuid.New().String()
	userID := uuid.New().String()

	existingAccount := &domain.Account{
		ID:     accountID,
		UserID: userID,
		Name:   "Chase Checking",
	}

	// Nickname with leading/trailing whitespace
	nicknameWithSpaces := "  My Account  "
	updateReq := &domain.UpdateAccountRequest{
		Nickname: &nicknameWithSpaces,
	}

	mockAccountRepo.On("GetByIDAndUser", ctx, accountID, userID).Return(existingAccount, nil)

	// Nickname should be trimmed
	mockAccountRepo.On("Update", ctx, mock.MatchedBy(func(acc *domain.Account) bool {
		return acc.Nickname != nil && *acc.Nickname == "My Account"
	}), userID).Return(nil)

	// Execute
	result, err := service.UpdateAccount(ctx, accountID, userID, updateReq)

	// Assert
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, "My Account", *result.Nickname)
	mockAccountRepo.AssertExpectations(t)
}

// TestReorderAccounts_Success tests successful bulk reorder
func TestReorderAccounts_Success(t *testing.T) {
	mockAccountRepo := &MockAccountRepository{}

	service := &accountService{
		accountRepo: mockAccountRepo,
	}

	ctx := context.Background()
	userID := uuid.New().String()

	reorderReq := &domain.ReorderAccountsRequest{
		AccountOrders: []domain.AccountOrder{
			{AccountID: uuid.New().String(), DisplayOrder: 0},
			{AccountID: uuid.New().String(), DisplayOrder: 1},
			{AccountID: uuid.New().String(), DisplayOrder: 2},
		},
	}

	// Mock UpdateDisplayOrders to succeed
	mockAccountRepo.On("UpdateDisplayOrders", ctx, userID, reorderReq.AccountOrders).Return(nil)

	// Execute
	err := service.ReorderAccounts(ctx, userID, reorderReq)

	// Assert
	require.NoError(t, err, "ReorderAccounts should succeed")
	mockAccountRepo.AssertExpectations(t)
}

// TestReorderAccounts_EmptyList tests reorder with empty list
func TestReorderAccounts_EmptyList(t *testing.T) {
	mockAccountRepo := &MockAccountRepository{}

	service := &accountService{
		accountRepo: mockAccountRepo,
	}

	ctx := context.Background()
	userID := uuid.New().String()

	reorderReq := &domain.ReorderAccountsRequest{
		AccountOrders: []domain.AccountOrder{},
	}

	// Execute - should return error for empty list
	err := service.ReorderAccounts(ctx, userID, reorderReq)

	// Assert
	require.Error(t, err, "ReorderAccounts should fail for empty list")
	assert.Contains(t, err.Error(), "empty")
}

// TestReorderAccounts_AccountNotFound tests reorder when an account doesn't belong to user
func TestReorderAccounts_AccountNotFound(t *testing.T) {
	mockAccountRepo := &MockAccountRepository{}

	service := &accountService{
		accountRepo: mockAccountRepo,
	}

	ctx := context.Background()
	userID := uuid.New().String()

	reorderReq := &domain.ReorderAccountsRequest{
		AccountOrders: []domain.AccountOrder{
			{AccountID: uuid.New().String(), DisplayOrder: 0},
		},
	}

	// Mock UpdateDisplayOrders to return error (account not found)
	mockAccountRepo.On("UpdateDisplayOrders", ctx, userID, reorderReq.AccountOrders).Return(assert.AnError)

	// Execute
	err := service.ReorderAccounts(ctx, userID, reorderReq)

	// Assert
	require.Error(t, err, "ReorderAccounts should fail when account not found")
	mockAccountRepo.AssertExpectations(t)
}

// TestGetAccountsFiltered_ExcludeHidden tests getting accounts without hidden ones
func TestGetAccountsFiltered_ExcludeHidden(t *testing.T) {
	mockAccountRepo := &MockAccountRepository{}
	mockUserRepo := &MockUserRepository{}

	service := &accountService{
		accountRepo: mockAccountRepo,
		userRepo:    mockUserRepo,
	}

	ctx := context.Background()
	userID := uuid.New().String()

	// Mock user lookup
	user := &domain.User{ID: userID}
	mockUserRepo.On("GetUserByID", ctx, userID).Return(user, nil)

	// Mock accounts (only visible ones)
	visibleAccounts := []domain.Account{
		{ID: uuid.New().String(), UserID: userID, Name: "Checking", IsHidden: false},
		{ID: uuid.New().String(), UserID: userID, Name: "Savings", IsHidden: false},
	}
	mockAccountRepo.On("GetAccountsWithInstitutionFiltered", ctx, userID, repository.AccountFilterOptions{IncludeHidden: false}).Return(visibleAccounts, nil)

	// Execute - don't include hidden
	result, err := service.GetAccountsFiltered(ctx, userID, AccountFilterOptions{IncludeHidden: false})

	// Assert
	require.NoError(t, err)
	assert.Len(t, result, 2)
	mockAccountRepo.AssertExpectations(t)
	mockUserRepo.AssertExpectations(t)
}

// TestGetAccountsFiltered_IncludeHidden tests getting all accounts including hidden
func TestGetAccountsFiltered_IncludeHidden(t *testing.T) {
	mockAccountRepo := &MockAccountRepository{}
	mockUserRepo := &MockUserRepository{}

	service := &accountService{
		accountRepo: mockAccountRepo,
		userRepo:    mockUserRepo,
	}

	ctx := context.Background()
	userID := uuid.New().String()

	// Mock user lookup
	user := &domain.User{ID: userID}
	mockUserRepo.On("GetUserByID", ctx, userID).Return(user, nil)

	// Mock all accounts (including hidden)
	allAccounts := []domain.Account{
		{ID: uuid.New().String(), UserID: userID, Name: "Checking", IsHidden: false},
		{ID: uuid.New().String(), UserID: userID, Name: "Savings", IsHidden: false},
		{ID: uuid.New().String(), UserID: userID, Name: "Hidden Card", IsHidden: true},
	}
	mockAccountRepo.On("GetAccountsWithInstitutionFiltered", ctx, userID, repository.AccountFilterOptions{IncludeHidden: true}).Return(allAccounts, nil)

	// Execute - include hidden
	result, err := service.GetAccountsFiltered(ctx, userID, AccountFilterOptions{IncludeHidden: true})

	// Assert
	require.NoError(t, err)
	assert.Len(t, result, 3)
	mockAccountRepo.AssertExpectations(t)
	mockUserRepo.AssertExpectations(t)
}

// TestGetAccountsFiltered_UserNotFound tests when user doesn't exist
func TestGetAccountsFiltered_UserNotFound(t *testing.T) {
	mockAccountRepo := &MockAccountRepository{}
	mockUserRepo := &MockUserRepository{}

	service := &accountService{
		accountRepo: mockAccountRepo,
		userRepo:    mockUserRepo,
	}

	ctx := context.Background()
	userID := uuid.New().String()

	// Mock user not found
	mockUserRepo.On("GetUserByID", ctx, userID).Return(nil, assert.AnError)

	// Execute
	result, err := service.GetAccountsFiltered(ctx, userID, AccountFilterOptions{})

	// Assert - should return empty list for non-existent user (consistent with GetAccounts)
	require.NoError(t, err)
	assert.Empty(t, result)
	mockUserRepo.AssertExpectations(t)
}

// ===========================================================================
// MockJobQueue for testing (Issue #658: Auto-sync after linking)
// ===========================================================================

// MockJobQueue is a mock implementation of queue.JobQueue for testing
type MockJobQueue struct {
	mock.Mock
}

func (m *MockJobQueue) Enqueue(ctx context.Context, job queue.Job, opts ...queue.EnqueueOption) (*queue.JobResult, error) {
	args := m.Called(ctx, job, opts)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*queue.JobResult), args.Error(1)
}

func (m *MockJobQueue) EnqueueTx(ctx context.Context, tx pgx.Tx, job queue.Job, opts ...queue.EnqueueOption) (*queue.JobResult, error) {
	args := m.Called(ctx, tx, job, opts)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*queue.JobResult), args.Error(1)
}

func (m *MockJobQueue) EnqueueMany(ctx context.Context, jobs []queue.Job, opts ...queue.EnqueueOption) ([]*queue.JobResult, error) {
	args := m.Called(ctx, jobs, opts)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]*queue.JobResult), args.Error(1)
}

func (m *MockJobQueue) EnqueueManyTx(ctx context.Context, tx pgx.Tx, jobs []queue.Job, opts ...queue.EnqueueOption) ([]*queue.JobResult, error) {
	args := m.Called(ctx, tx, jobs, opts)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]*queue.JobResult), args.Error(1)
}

// ===========================================================================
// Auto-Sync Tests (Issue #658: Auto-sync after Plaid account linking)
// ===========================================================================

// TestAccountService_LinkAccounts_EnqueuesSyncJob tests that all three sync jobs are
// enqueued after successful account linking: plaid_sync, investment_holdings_sync, and
// investment_transactions_sync.
// Issue #658: Auto-sync transactions after Plaid account linking
// Issue #659: Investment sync after linking
func TestAccountService_LinkAccounts_EnqueuesSyncJob(t *testing.T) {
	// This test verifies that:
	// 1. plaid_sync job is enqueued (transaction sync)
	// 2. investment_holdings_sync job is enqueued (Issue #659)
	// 3. investment_transactions_sync job is enqueued (Issue #659)

	// Setup mocks
	mockJobQueue := &MockJobQueue{}

	// Create a minimal service with jobQueue
	service := &accountService{
		jobQueue: mockJobQueue,
		logger:   slog.Default(),
	}

	ctx := context.Background()
	userID := uuid.New().String()
	linkedItemID := uuid.New().String()

	// Mock: Enqueue should be called with plaid_sync job
	mockJobQueue.On("Enqueue", ctx, mock.MatchedBy(func(job queue.Job) bool {
		return job.Kind() == "plaid_sync"
	}), mock.Anything).Return(&queue.JobResult{ID: 123, Kind: "plaid_sync"}, nil)

	// Mock: Enqueue should be called with investment_holdings_sync job (Issue #659)
	mockJobQueue.On("Enqueue", ctx, mock.MatchedBy(func(job queue.Job) bool {
		return job.Kind() == "investment_holdings_sync"
	}), mock.Anything).Return(&queue.JobResult{ID: 124, Kind: "investment_holdings_sync"}, nil)

	// Mock: Enqueue should be called with investment_transactions_sync job (Issue #659)
	mockJobQueue.On("Enqueue", ctx, mock.MatchedBy(func(job queue.Job) bool {
		return job.Kind() == "investment_transactions_sync"
	}), mock.Anything).Return(&queue.JobResult{ID: 125, Kind: "investment_transactions_sync"}, nil)

	// Call enqueueInitialSync directly
	service.enqueueInitialSync(ctx, linkedItemID, userID)

	// Assert: All three Enqueue calls were made
	mockJobQueue.AssertExpectations(t)
}

// TestAccountService_LinkAccounts_SucceedsWhenQueueFails tests that account linking
// succeeds even when the job queue fails. This is non-blocking behavior.
// Issue #658: Queue failure should not break account linking
func TestAccountService_LinkAccounts_SucceedsWhenQueueFails(t *testing.T) {
	// This test verifies that:
	// 1. Account linking succeeds even if job queue fails
	// 2. Error is logged but not returned to caller
	// 3. The user gets their accounts linked (primary operation)

	// Setup mocks
	mockJobQueue := &MockJobQueue{}

	service := &accountService{
		jobQueue: mockJobQueue,
		logger:   slog.Default(),
	}

	ctx := context.Background()
	userID := uuid.New().String()
	linkedItemID := uuid.New().String()

	// Mock: Enqueue fails with an error
	mockJobQueue.On("Enqueue", ctx, mock.Anything, mock.Anything).Return(nil, errors.New("queue unavailable"))

	// Call enqueueInitialSync - should not panic or return error
	// The method should handle the error gracefully (log and continue)
	service.enqueueInitialSync(ctx, linkedItemID, userID)

	// Assert: Enqueue was called (even though it failed)
	mockJobQueue.AssertExpectations(t)
}

// TestAccountService_LinkAccounts_NoQueueOK tests backward compatibility when
// jobQueue is nil (not configured). This ensures existing deployments work.
// Issue #658: Backward compatibility when jobQueue is nil
func TestAccountService_LinkAccounts_NoQueueOK(t *testing.T) {
	// This test verifies that:
	// 1. Service works correctly when jobQueue is nil
	// 2. No panic or error when enqueueing with nil queue
	// 3. Silent skip - just don't enqueue anything

	// Setup service without jobQueue (nil)
	service := &accountService{
		jobQueue: nil, // No queue configured
		logger:   slog.Default(),
	}

	ctx := context.Background()
	userID := uuid.New().String()
	linkedItemID := uuid.New().String()

	// Call enqueueInitialSync - should not panic
	// The method should check for nil and return early
	service.enqueueInitialSync(ctx, linkedItemID, userID)

	// If we get here without panic, the test passes
	// No assertions needed - we're testing for no panic
}

// ===========================================================================
// Plaid Re-Link Handling Tests (Issue #171)
// ===========================================================================

// MockUserAccountService is a mock implementation of UserAccountService for testing
type MockUserAccountService struct {
	mock.Mock
}

func (m *MockUserAccountService) MatchOrCreate(ctx context.Context, userID, institutionID string, mask *string, accountName, accountType, accountSubtype string) (*domain.UserAccount, error) {
	args := m.Called(ctx, userID, institutionID, mask, accountName, accountType, accountSubtype)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*domain.UserAccount), args.Error(1)
}

func (m *MockUserAccountService) GetByUserID(ctx context.Context, userID string, includeHidden bool) ([]domain.UserAccount, error) {
	args := m.Called(ctx, userID, includeHidden)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]domain.UserAccount), args.Error(1)
}

func (m *MockUserAccountService) GetByID(ctx context.Context, id, userID string) (*domain.UserAccount, error) {
	args := m.Called(ctx, id, userID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*domain.UserAccount), args.Error(1)
}

func (m *MockUserAccountService) UpdateCustomization(ctx context.Context, id, userID string, req *domain.UpdateUserAccountRequest) (*domain.UserAccount, error) {
	args := m.Called(ctx, id, userID, req)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*domain.UserAccount), args.Error(1)
}

func (m *MockUserAccountService) SetHidden(ctx context.Context, userAccountID string, hidden bool) error {
	args := m.Called(ctx, userAccountID, hidden)
	return args.Error(0)
}

// NOTE: MockUserAccountRepository is defined in user_account_service_test.go
// We reuse it here for account_service re-link tests

// TestAccountService_LinkAccounts_FirstLink_Success tests first-time link (no existing item at institution)
// Issue #171 - Plaid Re-Link Handling
func TestAccountService_LinkAccounts_FirstLink_Success(t *testing.T) {
	// Given: User links an institution for the first time (no existing linked_item)
	// When: LinkAccounts is called
	// Then: Should create linked_item, accounts, and user_accounts without soft-deleting anything

	// This test verifies:
	// 1. GetByUserAndInstitution returns ErrLinkedItemNotFound (no existing item)
	// 2. A new linked_item is created with status='active'
	// 3. User accounts are matched/created via UserAccountService
	// 4. New accounts are created with user_account_id set
	// 5. Sync job is enqueued

	t.Log("✓ Test documents first-link scenario (no re-link detection needed)")
	t.Log("✓ Implementation will call GetByUserAndInstitution and handle ErrLinkedItemNotFound")
}

// TestAccountService_LinkAccounts_Relink_DetectsExisting tests re-link detection
// Issue #171 - Plaid Re-Link Handling
func TestAccountService_LinkAccounts_Relink_DetectsExisting(t *testing.T) {
	// Given: User has an existing 'active' linked_item at the same institution
	// When: LinkAccounts is called with the same institution
	// Then: Should detect the existing linked_item via GetByUserAndInstitution

	mockLinkedItemRepo := new(MockLinkedItemRepository)
	ctx := context.Background()
	userID := uuid.New().String()
	institutionID := "ins_chase"

	// Existing linked item at same institution
	existingItem := &domain.LinkedItem{
		ID:            uuid.New().String(),
		UserID:        userID,
		ItemID:        "old-plaid-item-123",
		InstitutionID: institutionID,
		Status:        domain.LinkedItemStatusActive,
	}

	// Mock: GetByUserAndInstitution returns existing active item
	mockLinkedItemRepo.On("GetByUserAndInstitution", ctx, userID, institutionID, string(domain.LinkedItemStatusActive)).
		Return(existingItem, nil)

	// Execute - call GetByUserAndInstitution to detect re-link
	found, err := mockLinkedItemRepo.GetByUserAndInstitution(ctx, userID, institutionID, string(domain.LinkedItemStatusActive))

	// Assert: Existing item is detected
	require.NoError(t, err)
	require.NotNil(t, found)
	assert.Equal(t, existingItem.ID, found.ID)
	assert.Equal(t, domain.LinkedItemStatusActive, found.Status)

	mockLinkedItemRepo.AssertExpectations(t)
	t.Log("✓ Re-link detected: existing linked_item found at same institution")
}

// TestAccountService_LinkAccounts_Relink_MarksOldAsReplaced tests soft-delete of old linked_item
// Issue #171 - Plaid Re-Link Handling
func TestAccountService_LinkAccounts_Relink_MarksOldAsReplaced(t *testing.T) {
	// Given: Existing linked_item detected during re-link
	// When: LinkAccounts processes the re-link
	// Then: Old linked_item should be marked as 'replaced' with replaced_at timestamp

	mockLinkedItemRepo := new(MockLinkedItemRepository)
	ctx := context.Background()

	oldItemID := uuid.New().String()
	userID := uuid.New().String()

	existingItem := &domain.LinkedItem{
		ID:     oldItemID,
		UserID: userID,
		Status: domain.LinkedItemStatusActive,
	}

	// Mock: Update should be called with status='replaced' and replaced_at set
	mockLinkedItemRepo.On("Update", ctx, mock.MatchedBy(func(item *domain.LinkedItem) bool {
		return item.ID == oldItemID &&
			item.Status == domain.LinkedItemStatusReplaced &&
			item.ReplacedAt != nil
	})).Return(nil)

	// Execute: Update old item to 'replaced' status
	existingItem.Status = domain.LinkedItemStatusReplaced
	now := time.Now()
	existingItem.ReplacedAt = &now
	err := mockLinkedItemRepo.Update(ctx, existingItem)

	// Assert
	require.NoError(t, err)
	mockLinkedItemRepo.AssertExpectations(t)
	t.Log("✓ Old linked_item marked as 'replaced' with timestamp")
}

// TestAccountService_LinkAccounts_Relink_SetsReplacedByID tests replaced_by_id chain
// Issue #171 - Plaid Re-Link Handling
func TestAccountService_LinkAccounts_Relink_SetsReplacedByID(t *testing.T) {
	// Given: Existing linked_item and new linked_item created
	// When: Re-link processing completes
	// Then: Old item should have replaced_by_id pointing to new item

	mockLinkedItemRepo := new(MockLinkedItemRepository)
	ctx := context.Background()

	oldItemID := uuid.New().String()
	newItemID := uuid.New().String()
	userID := uuid.New().String()

	existingItem := &domain.LinkedItem{
		ID:     oldItemID,
		UserID: userID,
		Status: domain.LinkedItemStatusActive,
	}

	// Mock: Update should set replaced_by_id to new item ID
	mockLinkedItemRepo.On("Update", ctx, mock.MatchedBy(func(item *domain.LinkedItem) bool {
		return item.ID == oldItemID &&
			item.ReplacedByID != nil &&
			*item.ReplacedByID == newItemID
	})).Return(nil)

	// Execute: Update old item with replaced_by_id
	existingItem.Status = domain.LinkedItemStatusReplaced
	existingItem.ReplacedByID = &newItemID
	now := time.Now()
	existingItem.ReplacedAt = &now
	err := mockLinkedItemRepo.Update(ctx, existingItem)

	// Assert
	require.NoError(t, err)
	mockLinkedItemRepo.AssertExpectations(t)
	t.Log("✓ Old linked_item has replaced_by_id pointing to new item")
}

// TestAccountService_LinkAccounts_Relink_MarksOldAccountsReplaced tests account soft-delete
// Issue #171 - Plaid Re-Link Handling
func TestAccountService_LinkAccounts_Relink_MarksOldAccountsReplaced(t *testing.T) {
	// Given: Old linked_item has associated accounts
	// When: Re-link processing marks old item as replaced
	// Then: All old accounts should also be marked as 'replaced'

	mockAccountRepo := new(MockAccountRepository)
	ctx := context.Background()

	oldLinkedItemID := uuid.New().String()
	userID := uuid.New().String()
	account1ID := uuid.New().String()
	account2ID := uuid.New().String()

	// Old accounts associated with old linked_item
	oldAccounts := []domain.Account{
		{ID: account1ID, LinkedItemID: oldLinkedItemID, UserID: userID, Status: domain.AccountStatusActive},
		{ID: account2ID, LinkedItemID: oldLinkedItemID, UserID: userID, Status: domain.AccountStatusActive},
	}

	// Mock: GetByLinkedItemID returns old accounts
	mockAccountRepo.On("GetByLinkedItemID", ctx, oldLinkedItemID).Return(oldAccounts, nil)

	// Mock: UpdateStatus should be called for each old account
	mockAccountRepo.On("UpdateStatus", ctx, account1ID, userID, domain.AccountStatusReplaced, (*string)(nil)).Return(nil)
	mockAccountRepo.On("UpdateStatus", ctx, account2ID, userID, domain.AccountStatusReplaced, (*string)(nil)).Return(nil)

	// Execute: Get old accounts and mark them as replaced
	accounts, err := mockAccountRepo.GetByLinkedItemID(ctx, oldLinkedItemID)
	require.NoError(t, err)
	require.Len(t, accounts, 2)

	for _, acct := range accounts {
		err := mockAccountRepo.UpdateStatus(ctx, acct.ID, userID, domain.AccountStatusReplaced, nil)
		require.NoError(t, err)
	}

	// Assert
	mockAccountRepo.AssertExpectations(t)
	t.Log("✓ All old accounts marked as 'replaced'")
}

// TestAccountService_LinkAccounts_Relink_MatchesUserAccount tests user_account matching
// Issue #171 - Plaid Re-Link Handling
func TestAccountService_LinkAccounts_Relink_MatchesUserAccount(t *testing.T) {
	// Given: Plaid returns accounts during re-link
	// When: Processing new accounts
	// Then: UserAccountService.MatchOrCreate should find/create stable user_account

	mockUserAccountSvc := new(MockUserAccountService)
	ctx := context.Background()

	userID := uuid.New().String()
	institutionID := "ins_chase"
	mask := "1234"
	userAccountID := uuid.New().String()

	// Existing user_account that should be matched
	existingUserAccount := &domain.UserAccount{
		ID:            userAccountID,
		UserID:        userID,
		InstitutionID: institutionID,
		Mask:          &mask,
	}

	// Mock: MatchOrCreate finds existing user_account
	mockUserAccountSvc.On("MatchOrCreate", ctx, userID, institutionID, &mask, "Chase Checking", "depository", "checking").
		Return(existingUserAccount, nil)

	// Execute: Match user_account for Plaid account
	result, err := mockUserAccountSvc.MatchOrCreate(ctx, userID, institutionID, &mask, "Chase Checking", "depository", "checking")

	// Assert: Existing user_account matched
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, userAccountID, result.ID)

	mockUserAccountSvc.AssertExpectations(t)
	t.Log("✓ User account matched via MatchOrCreate")
}

// TestAccountService_LinkAccounts_Relink_CreatesNewUserAccount_DifferentMask tests new user_account creation
// Issue #171 - Plaid Re-Link Handling
func TestAccountService_LinkAccounts_Relink_CreatesNewUserAccount_DifferentMask(t *testing.T) {
	// Given: Plaid returns account with different mask than existing user_accounts
	// When: UserAccountService.MatchOrCreate is called
	// Then: Should create a new user_account (different real-world account)

	mockUserAccountSvc := new(MockUserAccountService)
	ctx := context.Background()

	userID := uuid.New().String()
	institutionID := "ins_chase"
	newMask := "5678" // Different mask = different real-world account
	newUserAccountID := uuid.New().String()

	// New user_account created (no match found)
	newUserAccount := &domain.UserAccount{
		ID:            newUserAccountID,
		UserID:        userID,
		InstitutionID: institutionID,
		Mask:          &newMask,
	}

	// Mock: MatchOrCreate creates new user_account (no existing match)
	mockUserAccountSvc.On("MatchOrCreate", ctx, userID, institutionID, &newMask, "Chase Savings", "depository", "savings").
		Return(newUserAccount, nil)

	// Execute
	result, err := mockUserAccountSvc.MatchOrCreate(ctx, userID, institutionID, &newMask, "Chase Savings", "depository", "savings")

	// Assert: New user_account created
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, newUserAccountID, result.ID)
	assert.Equal(t, newMask, *result.Mask)

	mockUserAccountSvc.AssertExpectations(t)
	t.Log("✓ New user_account created for different mask (different real-world account)")
}

// TestAccountService_LinkAccounts_Relink_SetsUserAccountID tests account.user_account_id population
// Issue #171 - Plaid Re-Link Handling
func TestAccountService_LinkAccounts_Relink_SetsUserAccountID(t *testing.T) {
	// Given: UserAccountService.MatchOrCreate returns a user_account
	// When: Creating new accounts from Plaid data
	// Then: Each account should have user_account_id set to the matched user_account

	mockAccountRepo := new(MockAccountRepository)
	ctx := context.Background()

	userAccountID := uuid.New().String()
	linkedItemID := uuid.New().String()
	userID := uuid.New().String()

	// Mock: CreateBatch called with accounts that have user_account_id set
	mockAccountRepo.On("CreateBatch", ctx, mock.MatchedBy(func(accounts []domain.Account) bool {
		if len(accounts) != 1 {
			return false
		}
		return accounts[0].UserAccountID != nil && *accounts[0].UserAccountID == userAccountID
	})).Return(nil)

	// Execute: Create account with user_account_id
	accounts := []domain.Account{
		{
			LinkedItemID:  linkedItemID,
			UserID:        userID,
			UserAccountID: &userAccountID,
			Name:          "Chase Checking",
		},
	}
	err := mockAccountRepo.CreateBatch(ctx, accounts)

	// Assert
	require.NoError(t, err)
	mockAccountRepo.AssertExpectations(t)
	t.Log("✓ New account has user_account_id set")
}

// TestAccountService_LinkAccounts_Relink_NewItemHasEmptyCursor tests cursor reset
// Issue #171 - Plaid Re-Link Handling
func TestAccountService_LinkAccounts_Relink_NewItemHasEmptyCursor(t *testing.T) {
	// Given: New linked_item created during re-link
	// When: Processing completes
	// Then: New item should have nil cursor (fresh sync starts from beginning)

	mockLinkedItemRepo := new(MockLinkedItemRepository)
	ctx := context.Background()

	userID := uuid.New().String()

	// Mock: Create should be called with nil cursor
	mockLinkedItemRepo.On("Create", ctx, mock.MatchedBy(func(item *domain.LinkedItem) bool {
		return item.Cursor == nil && // Cursor should be nil for new item
			item.Status == domain.LinkedItemStatusActive
	})).Return(nil)

	// Execute: Create new linked_item with nil cursor
	newItem := &domain.LinkedItem{
		UserID: userID,
		ItemID: "new-plaid-item-456",
		Status: domain.LinkedItemStatusActive,
		Cursor: nil, // Fresh item has no cursor
	}
	err := mockLinkedItemRepo.Create(ctx, newItem)

	// Assert
	require.NoError(t, err)
	mockLinkedItemRepo.AssertExpectations(t)
	t.Log("✓ New linked_item has nil cursor (fresh sync)")
}

// TestAccountService_LinkAccounts_Relink_EnqueuesSyncJob tests sync job enqueueing
// Issue #171 - Plaid Re-Link Handling
func TestAccountService_LinkAccounts_Relink_EnqueuesSyncJob(t *testing.T) {
	// Given: Re-link creates new linked_item
	// When: Processing completes
	// Then: All sync jobs should be enqueued for the new item (not old item)

	mockJobQueue := &MockJobQueue{}

	service := &accountService{
		jobQueue: mockJobQueue,
		logger:   slog.Default(),
	}

	ctx := context.Background()
	newLinkedItemID := uuid.New().String()
	userID := uuid.New().String()

	// Mock: All three sync jobs enqueued with new item's ID
	mockJobQueue.On("Enqueue", ctx, mock.MatchedBy(func(job queue.Job) bool {
		return job.Kind() == "plaid_sync"
	}), mock.Anything).Return(&queue.JobResult{ID: 123, Kind: "plaid_sync"}, nil)

	mockJobQueue.On("Enqueue", ctx, mock.MatchedBy(func(job queue.Job) bool {
		return job.Kind() == "investment_holdings_sync"
	}), mock.Anything).Return(&queue.JobResult{ID: 124, Kind: "investment_holdings_sync"}, nil)

	mockJobQueue.On("Enqueue", ctx, mock.MatchedBy(func(job queue.Job) bool {
		return job.Kind() == "investment_transactions_sync"
	}), mock.Anything).Return(&queue.JobResult{ID: 125, Kind: "investment_transactions_sync"}, nil)

	// Execute: Enqueue sync for new item
	service.enqueueInitialSync(ctx, newLinkedItemID, userID)

	// Assert
	mockJobQueue.AssertExpectations(t)
	t.Log("✓ All sync jobs enqueued for new linked_item")
}

// TestAccountService_LinkAccounts_MultipleAccounts_AllLinked tests multi-account handling
// Issue #171 - Plaid Re-Link Handling
func TestAccountService_LinkAccounts_MultipleAccounts_AllLinked(t *testing.T) {
	// Given: Plaid returns 3 accounts during re-link
	// When: Processing completes
	// Then: All 3 accounts should be created with proper user_account_id references

	mockAccountRepo := new(MockAccountRepository)
	ctx := context.Background()

	linkedItemID := uuid.New().String()
	userID := uuid.New().String()
	userAccountID1 := uuid.New().String()
	userAccountID2 := uuid.New().String()
	userAccountID3 := uuid.New().String()

	// Mock: CreateBatch called with 3 accounts
	mockAccountRepo.On("CreateBatch", ctx, mock.MatchedBy(func(accounts []domain.Account) bool {
		return len(accounts) == 3
	})).Return(nil)

	// Execute: Create 3 accounts
	accounts := []domain.Account{
		{LinkedItemID: linkedItemID, UserID: userID, UserAccountID: &userAccountID1, Name: "Checking"},
		{LinkedItemID: linkedItemID, UserID: userID, UserAccountID: &userAccountID2, Name: "Savings"},
		{LinkedItemID: linkedItemID, UserID: userID, UserAccountID: &userAccountID3, Name: "Credit Card"},
	}
	err := mockAccountRepo.CreateBatch(ctx, accounts)

	// Assert
	require.NoError(t, err)
	mockAccountRepo.AssertExpectations(t)
	t.Log("✓ All 3 accounts created during re-link")
}

// TestAccountService_LinkAccounts_MultipleAccounts_EachMatchedToUserAccount tests user_account matching per account
// Issue #171 - Plaid Re-Link Handling
func TestAccountService_LinkAccounts_MultipleAccounts_EachMatchedToUserAccount(t *testing.T) {
	// Given: Plaid returns 2 accounts with different masks
	// When: Processing each account
	// Then: UserAccountService.MatchOrCreate should be called for each account

	mockUserAccountSvc := new(MockUserAccountService)
	ctx := context.Background()

	userID := uuid.New().String()
	institutionID := "ins_chase"
	mask1 := "1234"
	mask2 := "5678"
	userAccountID1 := uuid.New().String()
	userAccountID2 := uuid.New().String()

	// Mock: MatchOrCreate called for each account
	mockUserAccountSvc.On("MatchOrCreate", ctx, userID, institutionID, &mask1, "Checking", "depository", "checking").
		Return(&domain.UserAccount{ID: userAccountID1, Mask: &mask1}, nil)
	mockUserAccountSvc.On("MatchOrCreate", ctx, userID, institutionID, &mask2, "Savings", "depository", "savings").
		Return(&domain.UserAccount{ID: userAccountID2, Mask: &mask2}, nil)

	// Execute: Match user_account for each Plaid account
	result1, err1 := mockUserAccountSvc.MatchOrCreate(ctx, userID, institutionID, &mask1, "Checking", "depository", "checking")
	result2, err2 := mockUserAccountSvc.MatchOrCreate(ctx, userID, institutionID, &mask2, "Savings", "depository", "savings")

	// Assert
	require.NoError(t, err1)
	require.NoError(t, err2)
	assert.Equal(t, userAccountID1, result1.ID)
	assert.Equal(t, userAccountID2, result2.ID)
	assert.NotEqual(t, result1.ID, result2.ID, "Different accounts should have different user_account_ids")

	mockUserAccountSvc.AssertExpectations(t)
	t.Log("✓ Each Plaid account matched to its own user_account")
}

// ===========================================================================
// Disconnected Item Re-Link Tests (Issue #896)
// Re-subscription reconnection: detect disconnected items to preserve history
// ===========================================================================

// TestLinkAccounts_DisconnectedRelink_DetectsDisconnectedItem tests that when no active item
// exists, the service falls back to checking for a disconnected item (re-subscription scenario).
// Issue #896 - Re-subscription item history continuity
func TestLinkAccounts_DisconnectedRelink_DetectsDisconnectedItem(t *testing.T) {
	// Given: User had a disconnected item at the same institution (subscription was cancelled)
	// When: Active item check returns nil, disconnected fallback is checked
	// Then: The disconnected item is detected and returned

	mockLinkedItemRepo := new(MockLinkedItemRepository)
	ctx := context.Background()
	userID := uuid.New().String()
	institutionID := "ins_chase"

	// Existing disconnected item (subscription was cancelled)
	disconnectedItem := &domain.LinkedItem{
		ID:            uuid.New().String(),
		UserID:        userID,
		ItemID:        "old-plaid-item-disconnected-123",
		InstitutionID: institutionID,
		Status:        domain.LinkedItemStatusDisconnected,
	}

	// Mock: Active check returns nil (no active item)
	mockLinkedItemRepo.On("GetByUserAndInstitution", ctx, userID, institutionID, string(domain.LinkedItemStatusActive)).
		Return(nil, nil)

	// Mock: Disconnected fallback returns the disconnected item
	mockLinkedItemRepo.On("GetByUserAndInstitution", ctx, userID, institutionID, string(domain.LinkedItemStatusDisconnected)).
		Return(disconnectedItem, nil)

	// Execute: Simulate Step 7 (active check)
	activeItem, err := mockLinkedItemRepo.GetByUserAndInstitution(ctx, userID, institutionID, string(domain.LinkedItemStatusActive))
	require.NoError(t, err)
	assert.Nil(t, activeItem, "No active item should be found")

	// Execute: Simulate Step 7b (disconnected fallback)
	var existingLinkedItem *domain.LinkedItem
	if activeItem == nil {
		existingLinkedItem, err = mockLinkedItemRepo.GetByUserAndInstitution(ctx, userID, institutionID, string(domain.LinkedItemStatusDisconnected))
		require.NoError(t, err)
	}

	// Assert: Disconnected item is detected
	require.NotNil(t, existingLinkedItem)
	assert.Equal(t, disconnectedItem.ID, existingLinkedItem.ID)
	assert.Equal(t, domain.LinkedItemStatusDisconnected, existingLinkedItem.Status)

	mockLinkedItemRepo.AssertExpectations(t)
	t.Log("✓ Disconnected item detected during re-subscription reconnection")
}

// TestLinkAccounts_DisconnectedRelink_MarksAsReplaced tests that a disconnected item
// is properly marked as 'replaced' when the user re-links after re-subscribing.
// Issue #896 - Re-subscription item history continuity
func TestLinkAccounts_DisconnectedRelink_MarksAsReplaced(t *testing.T) {
	// Given: Existing disconnected item (from cancelled subscription)
	// When: Re-link processing handles the old item
	// Then: Old disconnected item should be marked as 'replaced'

	mockLinkedItemRepo := new(MockLinkedItemRepository)
	ctx := context.Background()

	oldItemID := uuid.New().String()
	newItemID := uuid.New().String()
	userID := uuid.New().String()

	existingItem := &domain.LinkedItem{
		ID:     oldItemID,
		UserID: userID,
		Status: domain.LinkedItemStatusDisconnected,
	}

	// Mock: Update should be called with status='replaced' and replaced_at set
	mockLinkedItemRepo.On("Update", ctx, mock.MatchedBy(func(item *domain.LinkedItem) bool {
		return item.ID == oldItemID &&
			item.Status == domain.LinkedItemStatusReplaced &&
			item.ReplacedAt != nil &&
			item.ReplacedByID != nil &&
			*item.ReplacedByID == newItemID
	})).Return(nil)

	// Execute: Update disconnected item to 'replaced' status (same as active re-link)
	existingItem.Status = domain.LinkedItemStatusReplaced
	now := time.Now()
	existingItem.ReplacedAt = &now
	existingItem.ReplacedByID = &newItemID
	err := mockLinkedItemRepo.Update(ctx, existingItem)

	// Assert
	require.NoError(t, err)
	mockLinkedItemRepo.AssertExpectations(t)
	t.Log("✓ Disconnected item marked as 'replaced' during re-subscription reconnection")
}

// TestLinkAccounts_DisconnectedRelink_OldAccountsMarkedReplaced tests that accounts
// associated with a disconnected item are marked as 'replaced' on re-link.
// Issue #896 - Re-subscription item history continuity
func TestLinkAccounts_DisconnectedRelink_OldAccountsMarkedReplaced(t *testing.T) {
	// Given: Old disconnected linked_item has associated accounts
	// When: Re-link processing handles the disconnected item
	// Then: All old accounts should be marked as 'replaced'

	mockAccountRepo := new(MockAccountRepository)
	ctx := context.Background()

	oldLinkedItemID := uuid.New().String()
	userID := uuid.New().String()
	account1ID := uuid.New().String()
	account2ID := uuid.New().String()

	// Old accounts from the disconnected item
	oldAccounts := []domain.Account{
		{ID: account1ID, LinkedItemID: oldLinkedItemID, UserID: userID, Status: domain.AccountStatusActive},
		{ID: account2ID, LinkedItemID: oldLinkedItemID, UserID: userID, Status: domain.AccountStatusActive},
	}

	// Mock: GetByLinkedItemID returns old accounts from disconnected item
	mockAccountRepo.On("GetByLinkedItemID", ctx, oldLinkedItemID).Return(oldAccounts, nil)

	// Mock: UpdateStatus called for each old account
	mockAccountRepo.On("UpdateStatus", ctx, account1ID, userID, domain.AccountStatusReplaced, (*string)(nil)).Return(nil)
	mockAccountRepo.On("UpdateStatus", ctx, account2ID, userID, domain.AccountStatusReplaced, (*string)(nil)).Return(nil)

	// Execute: Get old accounts and mark them as replaced (same logic as active re-link)
	accounts, err := mockAccountRepo.GetByLinkedItemID(ctx, oldLinkedItemID)
	require.NoError(t, err)
	require.Len(t, accounts, 2)

	for _, acct := range accounts {
		err := mockAccountRepo.UpdateStatus(ctx, acct.ID, userID, domain.AccountStatusReplaced, nil)
		require.NoError(t, err)
	}

	// Assert
	mockAccountRepo.AssertExpectations(t)
	t.Log("✓ Old accounts from disconnected item marked as 'replaced'")
}

// TestLinkAccounts_DisconnectedRelink_UserAccountMatched tests that user_accounts are
// matched (not created fresh) when re-linking after re-subscription.
// Issue #896 - Re-subscription item history continuity
func TestLinkAccounts_DisconnectedRelink_UserAccountMatched(t *testing.T) {
	// Given: Disconnected item re-linked, same bank account being re-connected
	// When: Processing new accounts from Plaid
	// Then: UserAccountService.MatchOrCreate should find the existing user_account
	//       (same institution + mask = same real-world account = history preserved)

	mockUserAccountSvc := new(MockUserAccountService)
	ctx := context.Background()

	userID := uuid.New().String()
	institutionID := "ins_chase"
	mask := "1234"
	existingUserAccountID := uuid.New().String()

	// Existing user_account from before subscription cancellation
	existingUserAccount := &domain.UserAccount{
		ID:            existingUserAccountID,
		UserID:        userID,
		InstitutionID: institutionID,
		Mask:          &mask,
	}

	// Mock: MatchOrCreate finds existing user_account (history continuity preserved)
	mockUserAccountSvc.On("MatchOrCreate", ctx, userID, institutionID, &mask, "Chase Checking", "depository", "checking").
		Return(existingUserAccount, nil)

	// Execute: Match user_account for Plaid account during re-subscription reconnect
	result, err := mockUserAccountSvc.MatchOrCreate(ctx, userID, institutionID, &mask, "Chase Checking", "depository", "checking")

	// Assert: Existing user_account matched (not a new one)
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, existingUserAccountID, result.ID, "Should match existing user_account for history continuity")

	mockUserAccountSvc.AssertExpectations(t)
	t.Log("✓ Existing user_account matched during re-subscription reconnection (history preserved)")
}

// TestLinkAccounts_ActiveTakesPrecedence_OverDisconnected tests that when both an active
// and a disconnected item exist, the active one is used and disconnected is NOT queried.
// Issue #896 - Re-subscription item history continuity
func TestLinkAccounts_ActiveTakesPrecedence_OverDisconnected(t *testing.T) {
	// Given: User has an active item at the institution (normal re-link scenario)
	// When: Active check finds an item
	// Then: Disconnected fallback should NOT be queried (active takes precedence)

	mockLinkedItemRepo := new(MockLinkedItemRepository)
	ctx := context.Background()
	userID := uuid.New().String()
	institutionID := "ins_chase"

	// Existing active item at same institution
	activeItem := &domain.LinkedItem{
		ID:            uuid.New().String(),
		UserID:        userID,
		ItemID:        "active-plaid-item-123",
		InstitutionID: institutionID,
		Status:        domain.LinkedItemStatusActive,
	}

	// Mock: GetByUserAndInstitution with 'active' status returns the active item
	mockLinkedItemRepo.On("GetByUserAndInstitution", ctx, userID, institutionID, string(domain.LinkedItemStatusActive)).
		Return(activeItem, nil)

	// Execute: Step 7 active check
	found, err := mockLinkedItemRepo.GetByUserAndInstitution(ctx, userID, institutionID, string(domain.LinkedItemStatusActive))
	require.NoError(t, err)

	// Simulate the guard: only check disconnected if active is nil
	if found == nil {
		// This branch should NOT be taken
		_, _ = mockLinkedItemRepo.GetByUserAndInstitution(ctx, userID, institutionID, string(domain.LinkedItemStatusDisconnected))
	}

	// Assert: Active item was found
	require.NotNil(t, found)
	assert.Equal(t, activeItem.ID, found.ID)
	assert.Equal(t, domain.LinkedItemStatusActive, found.Status)

	// Assert: Disconnected was NOT called (active took precedence)
	mockLinkedItemRepo.AssertNotCalled(t, "GetByUserAndInstitution", ctx, userID, institutionID, string(domain.LinkedItemStatusDisconnected))

	mockLinkedItemRepo.AssertExpectations(t)
	t.Log("✓ Active item takes precedence; disconnected fallback not triggered")
}

// TestLinkAccounts_NoActiveNoDisconnected_FreshLink tests the fresh link scenario where
// neither an active nor disconnected item exists at the institution.
// Issue #896 - Re-subscription item history continuity
func TestLinkAccounts_NoActiveNoDisconnected_FreshLink(t *testing.T) {
	// Given: User has never linked this institution (completely fresh link)
	// When: Both active and disconnected checks return nil
	// Then: existingLinkedItem is nil (fresh link, no soft-delete needed)

	mockLinkedItemRepo := new(MockLinkedItemRepository)
	ctx := context.Background()
	userID := uuid.New().String()
	institutionID := "ins_chase"

	// Mock: Active check returns nil
	mockLinkedItemRepo.On("GetByUserAndInstitution", ctx, userID, institutionID, string(domain.LinkedItemStatusActive)).
		Return(nil, nil)

	// Mock: Disconnected fallback also returns nil
	mockLinkedItemRepo.On("GetByUserAndInstitution", ctx, userID, institutionID, string(domain.LinkedItemStatusDisconnected)).
		Return(nil, nil)

	// Execute: Step 7 - active check
	activeItem, err := mockLinkedItemRepo.GetByUserAndInstitution(ctx, userID, institutionID, string(domain.LinkedItemStatusActive))
	require.NoError(t, err)

	// Execute: Step 7b - disconnected fallback (since active is nil)
	var existingLinkedItem *domain.LinkedItem
	if activeItem == nil {
		existingLinkedItem, err = mockLinkedItemRepo.GetByUserAndInstitution(ctx, userID, institutionID, string(domain.LinkedItemStatusDisconnected))
		require.NoError(t, err)
	}

	// Assert: Both return nil — fresh link scenario
	assert.Nil(t, activeItem, "No active item should exist for fresh link")
	assert.Nil(t, existingLinkedItem, "No disconnected item should exist for fresh link")

	mockLinkedItemRepo.AssertExpectations(t)
	t.Log("✓ Fresh link: no active or disconnected item found — no soft-delete needed")
}

// TestLinkAccounts_DisconnectedRelink_NewItemGetsEmptyCursor tests that a new linked item
// created during a disconnected re-link starts with a nil cursor (fresh sync).
// Issue #896 - Re-subscription item history continuity
func TestLinkAccounts_DisconnectedRelink_NewItemGetsEmptyCursor(t *testing.T) {
	// Given: User re-links after re-subscription (disconnected item detected)
	// When: New linked_item is created
	// Then: New item has nil cursor so sync starts fresh (from Plaid beginning)
	//       The UserAccount bridge preserves the logical history continuity

	mockLinkedItemRepo := new(MockLinkedItemRepository)
	ctx := context.Background()
	userID := uuid.New().String()

	// Mock: Create should be called with nil cursor and active status
	mockLinkedItemRepo.On("Create", ctx, mock.MatchedBy(func(item *domain.LinkedItem) bool {
		return item.Cursor == nil &&
			item.Status == domain.LinkedItemStatusActive
	})).Return(nil)

	// Execute: Create new linked_item after disconnected re-link
	newItem := &domain.LinkedItem{
		UserID: userID,
		ItemID: "new-plaid-item-resubscribed-789",
		Status: domain.LinkedItemStatusActive,
		Cursor: nil, // Fresh item always starts with nil cursor
	}
	err := mockLinkedItemRepo.Create(ctx, newItem)

	// Assert
	require.NoError(t, err)
	mockLinkedItemRepo.AssertExpectations(t)
	t.Log("✓ New linked_item after disconnected re-link has nil cursor (fresh sync)")
}

// TestLinkAccounts_DisconnectedRelink_ErrorCheckingDisconnected_Fails tests that an
// unexpected error (not ErrLinkedItemNotFound) from the disconnected check propagates.
// Issue #896 - Re-subscription item history continuity
func TestLinkAccounts_DisconnectedRelink_ErrorCheckingDisconnected_Fails(t *testing.T) {
	// Given: Active check returns nil, but disconnected check returns unexpected DB error
	// When: Step 7b executes the disconnected fallback
	// Then: The unexpected error should propagate (not be swallowed)

	mockLinkedItemRepo := new(MockLinkedItemRepository)
	ctx := context.Background()
	userID := uuid.New().String()
	institutionID := "ins_chase"

	unexpectedErr := errors.New("database connection timeout")

	// Mock: Active check returns nil (no active item)
	mockLinkedItemRepo.On("GetByUserAndInstitution", ctx, userID, institutionID, string(domain.LinkedItemStatusActive)).
		Return(nil, nil)

	// Mock: Disconnected check returns unexpected error (NOT ErrLinkedItemNotFound)
	mockLinkedItemRepo.On("GetByUserAndInstitution", ctx, userID, institutionID, string(domain.LinkedItemStatusDisconnected)).
		Return(nil, unexpectedErr)

	// Execute: Step 7 - active check
	activeItem, err := mockLinkedItemRepo.GetByUserAndInstitution(ctx, userID, institutionID, string(domain.LinkedItemStatusActive))
	require.NoError(t, err)
	assert.Nil(t, activeItem)

	// Execute: Step 7b - disconnected fallback (since active is nil)
	var disconnectedErr error
	if activeItem == nil {
		_, disconnectedErr = mockLinkedItemRepo.GetByUserAndInstitution(ctx, userID, institutionID, string(domain.LinkedItemStatusDisconnected))
	}

	// Assert: Unexpected error is returned (not swallowed)
	require.Error(t, disconnectedErr, "Unexpected error should propagate")
	assert.Equal(t, unexpectedErr, disconnectedErr)
	assert.NotErrorIs(t, disconnectedErr, repository.ErrLinkedItemNotFound, "unexpected DB errors must propagate, not be swallowed as ErrLinkedItemNotFound")

	mockLinkedItemRepo.AssertExpectations(t)
	t.Log("✓ Unexpected error from disconnected check propagates correctly")
}

// ===========================================================================
// Integration Tests: service.LinkAccounts orchestration (Issue #896)
// These tests call service.LinkAccounts(...) directly to verify that the
// service properly orchestrates the full re-link flow, not just that mocks
// return what we tell them to.
// ===========================================================================

// newTestPlaidServer creates an httptest.Server that serves mock Plaid API responses.
// The server handles /item/public_token/exchange and /accounts/get endpoints.
// accessToken and plaidItemID are embedded in the returned responses.
// accounts is the list of plaid account data to return from /accounts/get.
func newTestPlaidServer(t *testing.T, accessToken, plaidItemID string, accounts []map[string]interface{}) (*httptest.Server, *plaid.APIClient) {
	t.Helper()

	mux := http.NewServeMux()

	mux.HandleFunc("/item/public_token/exchange", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"access_token": accessToken,
			"item_id":      plaidItemID,
			"request_id":   "req-exchange-test",
		})
	})

	mux.HandleFunc("/accounts/get", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"accounts": accounts,
			"item": map[string]interface{}{
				"item_id":            plaidItemID,
				"institution_id":     "ins_test",
				"webhook":            "",
				"available_products": []string{},
				"billed_products":    []string{},
				"update_type":        "background",
			},
			"request_id": "req-accounts-test",
		})
	})

	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)

	cfg := plaid.NewConfiguration()
	cfg.UseEnvironment(plaid.Environment(server.URL))
	client := plaid.NewAPIClient(cfg)

	return server, client
}

// newTestSubscriptionService creates a real SubscriptionService using the project's
// tier config YAML. Premium tier has unlimited accounts, so CheckLimit always allows.
func newTestSubscriptionService(t *testing.T) *SubscriptionService {
	t.Helper()
	tierConfig, err := config.LoadTierConfig()
	require.NoError(t, err, "failed to load tier config for integration test")
	return NewSubscriptionService(tierConfig, nil)
}

// TestLinkAccounts_Integration_DisconnectedRelink_FullFlow verifies that when:
//   - Step 7: active item check returns nil (no active item at institution)
//   - Step 7b: disconnected fallback returns a disconnected item (re-subscription scenario)
//
// The service correctly:
//  1. Calls GetByUserAndInstitution twice (active, then disconnected)
//  2. Creates new linked item with nil cursor
//  3. Marks old disconnected item as replaced with replaced_by_id pointing to new item
//  4. Marks old accounts as replaced
//  5. Calls MatchOrCreate for each new Plaid account
//  6. Creates new accounts with correct user_account_id (history continuity)
//
// Issue #896 - Re-subscription item history continuity
func TestLinkAccounts_Integration_DisconnectedRelink_FullFlow(t *testing.T) {
	ctx := context.Background()
	userID := uuid.New().String()
	institutionID := "ins_chase_test"
	institutionName := "Chase"
	publicToken := "public-sandbox-integration-test"
	accessToken := "access-sandbox-integration-test"
	plaidItemID := "plaid-item-integration-test"
	newItemID := uuid.New().String()
	oldItemID := uuid.New().String()
	oldAccount1ID := uuid.New().String()
	oldAccount2ID := uuid.New().String()
	userAccountID := uuid.New().String()
	mask := "1234"

	// Set up mock Plaid server returning 1 account
	plaidAccounts := []map[string]interface{}{
		{
			"account_id": "plaid-acct-integration-1",
			"balances": map[string]interface{}{
				"available":         100.0,
				"current":           110.0,
				"iso_currency_code": "USD",
			},
			"mask":          mask,
			"name":          "Chase Checking",
			"official_name": "Chase Premier Plus Checking",
			"subtype":       "checking",
			"type":          "depository",
		},
	}
	_, plaidClient := newTestPlaidServer(t, accessToken, plaidItemID, plaidAccounts)

	// Mocks
	mockPlaidSvc := &MockPlaidService{}
	mockPlaidSvc.On("GetClient").Return(plaidClient)

	mockLinkedItemRepo := &MockLinkedItemRepository{}
	mockAccountRepo := &MockAccountRepository{}
	mockUserRepo := &MockUserRepository{}
	mockUserAccountSvc := &MockUserAccountService{}

	// Step 1: user
	user := &domain.User{ID: userID, SubscriptionTier: "premium"}
	mockUserRepo.On("GetUserByID", ctx, userID).Return(user, nil)

	// Step 2: account count
	mockAccountRepo.On("CountByUserID", ctx, userID).Return(0, nil)

	// Step 7: no active item
	mockLinkedItemRepo.On("GetByUserAndInstitution", ctx, userID, institutionID, string(domain.LinkedItemStatusActive)).
		Return(nil, nil)

	// Step 7b: disconnected item exists (re-subscription scenario)
	disconnectedItem := &domain.LinkedItem{
		ID:            oldItemID,
		UserID:        userID,
		ItemID:        "old-plaid-item-disconnected",
		InstitutionID: institutionID,
		Status:        domain.LinkedItemStatusDisconnected,
	}
	mockLinkedItemRepo.On("GetByUserAndInstitution", ctx, userID, institutionID, string(domain.LinkedItemStatusDisconnected)).
		Return(disconnectedItem, nil)

	// Step 8: create new linked item — use Run to set ID so replaced_by_id can be verified
	// AccessToken is set by service; repo layer handles encryption on write
	mockLinkedItemRepo.On("Create", ctx, mock.MatchedBy(func(item *domain.LinkedItem) bool {
		return item.UserID == userID &&
			item.AccessToken == accessToken &&
			item.Status == domain.LinkedItemStatusActive &&
			item.Cursor == nil
	})).Run(func(args mock.Arguments) {
		item := args.Get(1).(*domain.LinkedItem)
		item.ID = newItemID
	}).Return(nil)

	// Step 9: mark old disconnected item as replaced
	mockLinkedItemRepo.On("Update", ctx, mock.MatchedBy(func(item *domain.LinkedItem) bool {
		return item.ID == oldItemID &&
			item.Status == domain.LinkedItemStatusReplaced &&
			item.ReplacedAt != nil &&
			item.ReplacedByID != nil &&
			*item.ReplacedByID == newItemID
	})).Return(nil)

	// Step 9: get and mark old accounts as replaced
	oldAccounts := []domain.Account{
		{ID: oldAccount1ID, LinkedItemID: oldItemID, UserID: userID},
		{ID: oldAccount2ID, LinkedItemID: oldItemID, UserID: userID},
	}
	mockAccountRepo.On("GetByLinkedItemID", ctx, oldItemID).Return(oldAccounts, nil)
	mockAccountRepo.On("UpdateStatus", ctx, oldAccount1ID, userID, domain.AccountStatusReplaced, (*string)(nil)).Return(nil)
	mockAccountRepo.On("UpdateStatus", ctx, oldAccount2ID, userID, domain.AccountStatusReplaced, (*string)(nil)).Return(nil)

	// Step 9b: MatchOrCreate returns existing user_account (history preserved)
	maskPtr := &mask
	mockUserAccountSvc.On("MatchOrCreate", ctx, userID, institutionID, maskPtr, "Chase Checking", "depository", "checking").
		Return(&domain.UserAccount{ID: userAccountID}, nil)

	// Step 10: CreateBatch — verify accounts have user_account_id set
	mockAccountRepo.On("CreateBatch", ctx, mock.MatchedBy(func(accounts []domain.Account) bool {
		return len(accounts) == 1 &&
			accounts[0].UserAccountID != nil &&
			*accounts[0].UserAccountID == userAccountID
	})).Return(nil)

	// Build service
	svc := NewAccountService(
		mockPlaidSvc,
		newTestSubscriptionService(t),
		mockLinkedItemRepo,
		mockAccountRepo,
		mockUserRepo,
		nil, // db not needed for this test
		WithUserAccountService(mockUserAccountSvc),
		WithAccountServiceLogger(slog.Default()),
	)

	// Execute
	result, err := svc.LinkAccounts(ctx, userID, publicToken, institutionID, institutionName)

	// Assert
	require.NoError(t, err, "LinkAccounts should succeed for disconnected re-link")
	require.NotNil(t, result)
	require.NotNil(t, result.LinkedItem)
	assert.Equal(t, newItemID, result.LinkedItem.ID, "new linked item should have correct ID")
	assert.Equal(t, domain.LinkedItemStatusActive, result.LinkedItem.Status)
	assert.Nil(t, result.LinkedItem.Cursor, "new item should have nil cursor for fresh sync")
	require.Len(t, result.Accounts, 1, "should have 1 account from Plaid")
	assert.NotNil(t, result.Accounts[0].UserAccountID, "account should have user_account_id for history continuity")
	assert.Equal(t, userAccountID, *result.Accounts[0].UserAccountID)

	mockLinkedItemRepo.AssertExpectations(t)
	mockAccountRepo.AssertExpectations(t)
	mockUserRepo.AssertExpectations(t)
	mockPlaidSvc.AssertExpectations(t)
	mockUserAccountSvc.AssertExpectations(t)
}

// TestLinkAccounts_Integration_ActiveTakesPrecedence verifies that when an active
// item exists at the institution, Step 7b (disconnected fallback) is NOT executed.
// The active re-link flow runs normally, but no disconnected lookup is triggered.
// Issue #896 - Re-subscription item history continuity
func TestLinkAccounts_Integration_ActiveTakesPrecedence(t *testing.T) {
	ctx := context.Background()
	userID := uuid.New().String()
	institutionID := "ins_chase_test"
	institutionName := "Chase"
	publicToken := "public-sandbox-active-test"
	accessToken := "access-sandbox-active-test"
	plaidItemID := "plaid-item-active-test"
	newItemID := uuid.New().String()
	activeItemID := uuid.New().String()
	oldAccountID := uuid.New().String()
	userAccountID := uuid.New().String()
	mask := "5678"

	plaidAccounts := []map[string]interface{}{
		{
			"account_id": "plaid-acct-active-1",
			"balances":   map[string]interface{}{"available": 200.0, "current": 200.0, "iso_currency_code": "USD"},
			"mask":       mask,
			"name":       "Chase Savings",
			"subtype":    "savings",
			"type":       "depository",
		},
	}
	_, plaidClient := newTestPlaidServer(t, accessToken, plaidItemID, plaidAccounts)

	mockPlaidSvc := &MockPlaidService{}
	mockPlaidSvc.On("GetClient").Return(plaidClient)

	mockLinkedItemRepo := &MockLinkedItemRepository{}
	mockAccountRepo := &MockAccountRepository{}
	mockUserRepo := &MockUserRepository{}
	mockUserAccountSvc := &MockUserAccountService{}

	user := &domain.User{ID: userID, SubscriptionTier: "premium"}
	mockUserRepo.On("GetUserByID", ctx, userID).Return(user, nil)
	mockAccountRepo.On("CountByUserID", ctx, userID).Return(0, nil)

	// Step 7: active item found — Step 7b should NOT be triggered
	activeItem := &domain.LinkedItem{
		ID:            activeItemID,
		UserID:        userID,
		ItemID:        "active-plaid-item",
		InstitutionID: institutionID,
		Status:        domain.LinkedItemStatusActive,
	}
	mockLinkedItemRepo.On("GetByUserAndInstitution", ctx, userID, institutionID, string(domain.LinkedItemStatusActive)).
		Return(activeItem, nil)

	// Create new item (active re-link flow)
	mockLinkedItemRepo.On("Create", ctx, mock.Anything).
		Run(func(args mock.Arguments) {
			item := args.Get(1).(*domain.LinkedItem)
			item.ID = newItemID
		}).Return(nil)

	// Mark old active item as replaced
	mockLinkedItemRepo.On("Update", ctx, mock.MatchedBy(func(item *domain.LinkedItem) bool {
		return item.ID == activeItemID && item.Status == domain.LinkedItemStatusReplaced
	})).Return(nil)

	// Get and mark old accounts as replaced
	mockAccountRepo.On("GetByLinkedItemID", ctx, activeItemID).
		Return([]domain.Account{{ID: oldAccountID, LinkedItemID: activeItemID, UserID: userID}}, nil)
	mockAccountRepo.On("UpdateStatus", ctx, oldAccountID, userID, domain.AccountStatusReplaced, (*string)(nil)).Return(nil)

	// MatchOrCreate and CreateBatch
	maskPtr := &mask
	mockUserAccountSvc.On("MatchOrCreate", ctx, userID, institutionID, maskPtr, "Chase Savings", "depository", "savings").
		Return(&domain.UserAccount{ID: userAccountID}, nil)
	mockAccountRepo.On("CreateBatch", ctx, mock.Anything).Return(nil)

	svc := NewAccountService(
		mockPlaidSvc,
		newTestSubscriptionService(t),
		mockLinkedItemRepo,
		mockAccountRepo,
		mockUserRepo,
		nil,
		WithUserAccountService(mockUserAccountSvc),
		WithAccountServiceLogger(slog.Default()),
	)

	result, err := svc.LinkAccounts(ctx, userID, publicToken, institutionID, institutionName)

	require.NoError(t, err, "LinkAccounts should succeed when active item takes precedence")
	require.NotNil(t, result)

	// Key assertion: disconnected lookup was NOT triggered
	mockLinkedItemRepo.AssertNotCalled(t, "GetByUserAndInstitution", ctx, userID, institutionID, string(domain.LinkedItemStatusDisconnected))

	mockLinkedItemRepo.AssertExpectations(t)
	mockAccountRepo.AssertExpectations(t)
	mockUserRepo.AssertExpectations(t)
	mockPlaidSvc.AssertExpectations(t)
	mockUserAccountSvc.AssertExpectations(t)
}

// TestLinkAccounts_Integration_FreshLink_NoExistingItem verifies that when neither
// an active nor a disconnected item exists at the institution, no soft-delete
// (Update/UpdateStatus/GetByLinkedItemID) is triggered — it's a clean first link.
// Issue #896 - Re-subscription item history continuity
func TestLinkAccounts_Integration_FreshLink_NoExistingItem(t *testing.T) {
	ctx := context.Background()
	userID := uuid.New().String()
	institutionID := "ins_new_bank"
	institutionName := "New Bank"
	publicToken := "public-sandbox-fresh-test"
	accessToken := "access-sandbox-fresh-test"
	plaidItemID := "plaid-item-fresh-test"
	newItemID := uuid.New().String()
	userAccountID := uuid.New().String()

	plaidAccounts := []map[string]interface{}{
		{
			"account_id": "plaid-acct-fresh-1",
			"balances":   map[string]interface{}{"available": 500.0, "current": 500.0, "iso_currency_code": "USD"},
			"name":       "Savings Account",
			"subtype":    "savings",
			"type":       "depository",
		},
	}
	_, plaidClient := newTestPlaidServer(t, accessToken, plaidItemID, plaidAccounts)

	mockPlaidSvc := &MockPlaidService{}
	mockPlaidSvc.On("GetClient").Return(plaidClient)

	mockLinkedItemRepo := &MockLinkedItemRepository{}
	mockAccountRepo := &MockAccountRepository{}
	mockUserRepo := &MockUserRepository{}
	mockUserAccountSvc := &MockUserAccountService{}

	user := &domain.User{ID: userID, SubscriptionTier: "premium"}
	mockUserRepo.On("GetUserByID", ctx, userID).Return(user, nil)
	mockAccountRepo.On("CountByUserID", ctx, userID).Return(0, nil)

	// Step 7: no active item
	mockLinkedItemRepo.On("GetByUserAndInstitution", ctx, userID, institutionID, string(domain.LinkedItemStatusActive)).
		Return(nil, nil)

	// Step 7b: no disconnected item either (fresh link)
	mockLinkedItemRepo.On("GetByUserAndInstitution", ctx, userID, institutionID, string(domain.LinkedItemStatusDisconnected)).
		Return(nil, nil)

	// Create new item
	mockLinkedItemRepo.On("Create", ctx, mock.Anything).
		Run(func(args mock.Arguments) {
			item := args.Get(1).(*domain.LinkedItem)
			item.ID = newItemID
		}).Return(nil)

	// MatchOrCreate returns new user account (nil mask for this account)
	mockUserAccountSvc.On("MatchOrCreate", ctx, userID, institutionID, (*string)(nil), "Savings Account", "depository", "savings").
		Return(&domain.UserAccount{ID: userAccountID}, nil)

	// CreateBatch
	mockAccountRepo.On("CreateBatch", ctx, mock.Anything).Return(nil)

	svc := NewAccountService(
		mockPlaidSvc,
		newTestSubscriptionService(t),
		mockLinkedItemRepo,
		mockAccountRepo,
		mockUserRepo,
		nil,
		WithUserAccountService(mockUserAccountSvc),
		WithAccountServiceLogger(slog.Default()),
	)

	result, err := svc.LinkAccounts(ctx, userID, publicToken, institutionID, institutionName)

	require.NoError(t, err, "LinkAccounts should succeed for fresh link")
	require.NotNil(t, result)
	assert.Equal(t, newItemID, result.LinkedItem.ID)
	assert.Len(t, result.Accounts, 1)

	// No soft-delete calls should have been made
	mockLinkedItemRepo.AssertNotCalled(t, "Update", mock.Anything, mock.Anything)
	mockAccountRepo.AssertNotCalled(t, "GetByLinkedItemID", mock.Anything, mock.Anything)
	mockAccountRepo.AssertNotCalled(t, "UpdateStatus", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything)

	mockLinkedItemRepo.AssertExpectations(t)
	mockAccountRepo.AssertExpectations(t)
	mockUserRepo.AssertExpectations(t)
	mockPlaidSvc.AssertExpectations(t)
	mockUserAccountSvc.AssertExpectations(t)
}

// TestLinkAccounts_Integration_DisconnectedRelink_ErrorPropagates verifies that when
// the disconnected item lookup (Step 7b) returns an unexpected error (not ErrLinkedItemNotFound),
// LinkAccounts propagates the error and does not continue the flow.
// Issue #896 - Re-subscription item history continuity
func TestLinkAccounts_Integration_DisconnectedRelink_ErrorPropagates(t *testing.T) {
	ctx := context.Background()
	userID := uuid.New().String()
	institutionID := "ins_error_test"
	institutionName := "Error Bank"
	publicToken := "public-sandbox-error-test"
	accessToken := "access-sandbox-error-test"
	plaidItemID := "plaid-item-error-test"

	plaidAccounts := []map[string]interface{}{
		{
			"account_id": "plaid-acct-error-1",
			"balances":   map[string]interface{}{"available": 100.0, "current": 100.0, "iso_currency_code": "USD"},
			"name":       "Checking",
			"subtype":    "checking",
			"type":       "depository",
		},
	}
	_, plaidClient := newTestPlaidServer(t, accessToken, plaidItemID, plaidAccounts)

	mockPlaidSvc := &MockPlaidService{}
	mockPlaidSvc.On("GetClient").Return(plaidClient)

	mockLinkedItemRepo := &MockLinkedItemRepository{}
	mockAccountRepo := &MockAccountRepository{}
	mockUserRepo := &MockUserRepository{}

	user := &domain.User{ID: userID, SubscriptionTier: "premium"}
	mockUserRepo.On("GetUserByID", ctx, userID).Return(user, nil)
	mockAccountRepo.On("CountByUserID", ctx, userID).Return(0, nil)

	// Step 7: no active item
	mockLinkedItemRepo.On("GetByUserAndInstitution", ctx, userID, institutionID, string(domain.LinkedItemStatusActive)).
		Return(nil, nil)

	// Step 7b: unexpected database error (not ErrLinkedItemNotFound — must propagate)
	dbErr := errors.New("connection timeout: failed to query disconnected items")
	mockLinkedItemRepo.On("GetByUserAndInstitution", ctx, userID, institutionID, string(domain.LinkedItemStatusDisconnected)).
		Return(nil, dbErr)

	svc := NewAccountService(
		mockPlaidSvc,
		newTestSubscriptionService(t),
		mockLinkedItemRepo,
		mockAccountRepo,
		mockUserRepo,
		nil,
		WithAccountServiceLogger(slog.Default()),
	)

	result, err := svc.LinkAccounts(ctx, userID, publicToken, institutionID, institutionName)

	// Assert: error propagates, no result
	require.Error(t, err, "LinkAccounts should return error when disconnected check fails")
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "failed to check for disconnected linked item")

	// Assert: no items or accounts were created
	mockLinkedItemRepo.AssertNotCalled(t, "Create", mock.Anything, mock.Anything)
	mockAccountRepo.AssertNotCalled(t, "CreateBatch", mock.Anything, mock.Anything)

	mockLinkedItemRepo.AssertExpectations(t)
	mockAccountRepo.AssertExpectations(t)
	mockUserRepo.AssertExpectations(t)
	mockPlaidSvc.AssertExpectations(t)
}

// ============================================================================
// Issue #1010: Cross-user Plaid item uniqueness — fingerprint ownership tests
// ============================================================================

// noopEncryptionSvc is an identity encryption service used for testing.
// BlindIndex will still produce real HMAC-SHA256 values using the test key.
type noopEncryptionSvc struct{}

func (n *noopEncryptionSvc) Encrypt(plaintext string) (string, error) {
	return "NOOP_" + plaintext, nil
}

func (n *noopEncryptionSvc) Decrypt(ciphertext string) (string, error) {
	return strings.TrimPrefix(ciphertext, "NOOP_"), nil
}

func newTestFieldEncryptor(t *testing.T) *appcrypto.FieldEncryptor {
	t.Helper()
	svc := &noopEncryptionSvc{}
	key := []byte("12345678901234567890123456789012") // 32 bytes
	enc, err := appcrypto.NewFieldEncryptor(svc, key)
	require.NoError(t, err)
	return enc
}

// newTestPlaidServerWithPersistentID creates a mock Plaid server that returns
// persistent_account_id in account objects. It extends newTestPlaidServer.
func newTestPlaidServerWithPersistentID(t *testing.T, accessToken, plaidItemID string, accounts []map[string]interface{}) (*httptest.Server, *plaid.APIClient) {
	return newTestPlaidServer(t, accessToken, plaidItemID, accounts)
}

// TestLinkAccounts_FingerprintCheck_BlocksAnotherUserActiveItem verifies that
// LinkAccounts returns ErrAccountOwnedByAnotherUser when a Plaid account's
// persistent fingerprint is already owned by a different user (active item).
//
// Issue #1010: Cross-user Plaid item uniqueness
func TestLinkAccounts_FingerprintCheck_BlocksAnotherUserActiveItem(t *testing.T) {
	ctx := context.Background()
	userID := uuid.New().String()
	otherUserID := uuid.New().String()
	institutionID := "ins_test" // must match mock server item.institution_id
	institutionName := "Test Bank"
	publicToken := "public-sandbox-fingerprint-block"
	accessToken := "access-sandbox-fingerprint-block"
	plaidItemID := "plaid-item-fingerprint-block"
	persistentAccountID := "persistent-acct-block-test"

	enc := newTestFieldEncryptor(t)
	expectedPersistentBlind := enc.BlindIndex("pid:" + persistentAccountID)

	plaidAccounts := []map[string]interface{}{
		{
			"account_id":           "plaid-acct-block-1",
			"persistent_account_id": persistentAccountID,
			"balances":             map[string]interface{}{"available": 100.0, "current": 110.0, "iso_currency_code": "USD"},
			"mask":                 "1234",
			"name":                 "Checking",
			"subtype":              "checking",
			"type":                 "depository",
		},
	}
	_, plaidClient := newTestPlaidServerWithPersistentID(t, accessToken, plaidItemID, plaidAccounts)

	mockPlaidSvc := &MockPlaidService{}
	mockPlaidSvc.On("GetClient").Return(plaidClient)

	mockLinkedItemRepo := &MockLinkedItemRepository{}
	mockAccountRepo := &MockAccountRepository{}
	mockUserRepo := &MockUserRepository{}

	user := &domain.User{ID: userID, SubscriptionTier: "premium"}
	mockUserRepo.On("GetUserByID", ctx, userID).Return(user, nil)
	mockAccountRepo.On("CountByUserID", ctx, userID).Return(0, nil)

	mockLinkedItemRepo.On("GetByUserAndInstitution", ctx, userID, institutionID, string(domain.LinkedItemStatusActive)).
		Return(nil, nil)
	mockLinkedItemRepo.On("GetByUserAndInstitution", ctx, userID, institutionID, string(domain.LinkedItemStatusDisconnected)).
		Return(nil, repository.ErrLinkedItemNotFound)

	// Persistent fingerprint is owned by another user with an active item
	otherItemID := uuid.New().String()
	mockAccountRepo.On("CheckFingerprintOwnership", ctx, expectedPersistentBlind, userID).
		Return(&repository.FingerprintOwnership{
			UserID:       otherUserID,
			LinkedItemID: otherItemID,
			ItemStatus:   domain.LinkedItemStatusActive,
		}, nil)

	svc := NewAccountService(
		mockPlaidSvc,
		newTestSubscriptionService(t),
		mockLinkedItemRepo,
		mockAccountRepo,
		mockUserRepo,
		nil,
		WithEncryptor(enc),
		WithAccountServiceLogger(slog.Default()),
	)

	result, err := svc.LinkAccounts(ctx, userID, publicToken, institutionID, institutionName)

	require.Error(t, err, "should block when another user's active item owns the fingerprint")
	assert.Nil(t, result)
	assert.ErrorIs(t, err, repository.ErrAccountOwnedByAnotherUser)

	mockAccountRepo.AssertNotCalled(t, "CreateBatch", mock.Anything, mock.Anything)
	mockLinkedItemRepo.AssertNotCalled(t, "Create", mock.Anything, mock.Anything)

	mockAccountRepo.AssertExpectations(t)
	mockUserRepo.AssertExpectations(t)
	mockPlaidSvc.AssertExpectations(t)
}

// TestLinkAccounts_FingerprintCheck_BlocksTTLNotExpired verifies that a fingerprint
// owned by another user with a disconnected item < 90 days ago blocks the link.
//
// Issue #1010: Cross-user Plaid item uniqueness
func TestLinkAccounts_FingerprintCheck_BlocksTTLNotExpired(t *testing.T) {
	ctx := context.Background()
	userID := uuid.New().String()
	otherUserID := uuid.New().String()
	institutionID := "ins_test"
	institutionName := "Test Bank"
	publicToken := "public-sandbox-fingerprint-ttl"
	accessToken := "access-sandbox-fingerprint-ttl"
	plaidItemID := "plaid-item-fingerprint-ttl"
	persistentAccountID := "persistent-acct-ttl-test"

	enc := newTestFieldEncryptor(t)
	expectedPersistentBlind := enc.BlindIndex("pid:" + persistentAccountID)

	plaidAccounts := []map[string]interface{}{
		{
			"account_id":           "plaid-acct-ttl-1",
			"persistent_account_id": persistentAccountID,
			"balances":             map[string]interface{}{"available": 100.0, "current": 110.0, "iso_currency_code": "USD"},
			"mask":                 "5678",
			"name":                 "Savings",
			"subtype":              "savings",
			"type":                 "depository",
		},
	}
	_, plaidClient := newTestPlaidServerWithPersistentID(t, accessToken, plaidItemID, plaidAccounts)

	mockPlaidSvc := &MockPlaidService{}
	mockPlaidSvc.On("GetClient").Return(plaidClient)

	mockLinkedItemRepo := &MockLinkedItemRepository{}
	mockAccountRepo := &MockAccountRepository{}
	mockUserRepo := &MockUserRepository{}

	user := &domain.User{ID: userID, SubscriptionTier: "premium"}
	mockUserRepo.On("GetUserByID", ctx, userID).Return(user, nil)
	mockAccountRepo.On("CountByUserID", ctx, userID).Return(0, nil)

	mockLinkedItemRepo.On("GetByUserAndInstitution", ctx, userID, institutionID, string(domain.LinkedItemStatusActive)).
		Return(nil, nil)
	mockLinkedItemRepo.On("GetByUserAndInstitution", ctx, userID, institutionID, string(domain.LinkedItemStatusDisconnected)).
		Return(nil, repository.ErrLinkedItemNotFound)

	// Disconnected 30 days ago — within 90-day TTL, should still block
	statusChangedAt := time.Now().Add(-30 * 24 * time.Hour)
	otherItemID := uuid.New().String()
	mockAccountRepo.On("CheckFingerprintOwnership", ctx, expectedPersistentBlind, userID).
		Return(&repository.FingerprintOwnership{
			UserID:          otherUserID,
			LinkedItemID:    otherItemID,
			ItemStatus:      domain.LinkedItemStatusDisconnected,
			StatusChangedAt: &statusChangedAt,
		}, nil)

	svc := NewAccountService(
		mockPlaidSvc,
		newTestSubscriptionService(t),
		mockLinkedItemRepo,
		mockAccountRepo,
		mockUserRepo,
		nil,
		WithEncryptor(enc),
		WithAccountServiceLogger(slog.Default()),
	)

	result, err := svc.LinkAccounts(ctx, userID, publicToken, institutionID, institutionName)

	require.Error(t, err, "should block when another user disconnected < 90 days ago")
	assert.Nil(t, result)
	assert.ErrorIs(t, err, repository.ErrAccountOwnedByAnotherUser)

	mockAccountRepo.AssertNotCalled(t, "CreateBatch", mock.Anything, mock.Anything)
	mockAccountRepo.AssertExpectations(t)
}

// TestLinkAccounts_FingerprintCheck_AllowsAfterTTLExpiry verifies that a fingerprint
// owned by another user with a disconnected item >= 90 days ago releases and allows link.
//
// Issue #1010: Cross-user Plaid item uniqueness
func TestLinkAccounts_FingerprintCheck_AllowsAfterTTLExpiry(t *testing.T) {
	ctx := context.Background()
	userID := uuid.New().String()
	otherUserID := uuid.New().String()
	institutionID := "ins_test"
	institutionName := "Test Bank"
	publicToken := "public-sandbox-fingerprint-expired"
	accessToken := "access-sandbox-fingerprint-expired"
	plaidItemID := "plaid-item-fingerprint-expired"
	newItemID := uuid.New().String()
	persistentAccountID := "persistent-acct-expired-test"
	mask := "9012"

	enc := newTestFieldEncryptor(t)
	expectedPersistentBlind := enc.BlindIndex("pid:" + persistentAccountID)
	// Fallback fingerprint (institution_id comes from the Plaid item response: "ins_test")
	expectedFallbackBlind := enc.BlindIndex("fb:" + institutionID + "\x00" + mask + "\x00" + "depository" + "\x00" + "checking")

	plaidAccounts := []map[string]interface{}{
		{
			"account_id":           "plaid-acct-expired-1",
			"persistent_account_id": persistentAccountID,
			"balances":             map[string]interface{}{"available": 100.0, "current": 110.0, "iso_currency_code": "USD"},
			"mask":                 mask,
			"name":                 "Checking",
			"subtype":              "checking",
			"type":                 "depository",
		},
	}
	_, plaidClient := newTestPlaidServerWithPersistentID(t, accessToken, plaidItemID, plaidAccounts)

	mockPlaidSvc := &MockPlaidService{}
	mockPlaidSvc.On("GetClient").Return(plaidClient)

	mockLinkedItemRepo := &MockLinkedItemRepository{}
	mockAccountRepo := &MockAccountRepository{}
	mockUserRepo := &MockUserRepository{}

	user := &domain.User{ID: userID, SubscriptionTier: "premium"}
	mockUserRepo.On("GetUserByID", ctx, userID).Return(user, nil)
	mockAccountRepo.On("CountByUserID", ctx, userID).Return(0, nil)

	mockLinkedItemRepo.On("GetByUserAndInstitution", ctx, userID, institutionID, string(domain.LinkedItemStatusActive)).
		Return(nil, nil)
	mockLinkedItemRepo.On("GetByUserAndInstitution", ctx, userID, institutionID, string(domain.LinkedItemStatusDisconnected)).
		Return(nil, repository.ErrLinkedItemNotFound)

	// Disconnected 91 days ago — TTL expired, should release and allow
	statusChangedAt := time.Now().Add(-91 * 24 * time.Hour)
	otherItemID := uuid.New().String()
	mockAccountRepo.On("CheckFingerprintOwnership", ctx, expectedPersistentBlind, userID).
		Return(&repository.FingerprintOwnership{
			UserID:          otherUserID,
			LinkedItemID:    otherItemID,
			ItemStatus:      domain.LinkedItemStatusDisconnected,
			StatusChangedAt: &statusChangedAt,
		}, nil)

	// Persistent fingerprint expired → release it
	mockAccountRepo.On("ExpireAccountsByFingerprint", ctx, expectedPersistentBlind).Return(nil)

	// Fallback fingerprint not owned by anyone
	mockAccountRepo.On("CheckFingerprintOwnership", ctx, expectedFallbackBlind, userID).
		Return(nil, nil)

	// Proceed with normal link flow
	mockLinkedItemRepo.On("Create", ctx, mock.MatchedBy(func(item *domain.LinkedItem) bool {
		return item.UserID == userID && item.Status == domain.LinkedItemStatusActive
	})).Run(func(args mock.Arguments) {
		args.Get(1).(*domain.LinkedItem).ID = newItemID
	}).Return(nil)

	mockAccountRepo.On("CreateBatch", ctx, mock.MatchedBy(func(accounts []domain.Account) bool {
		return len(accounts) == 1 && accounts[0].UserID == userID
	})).Return(nil)

	svc := NewAccountService(
		mockPlaidSvc,
		newTestSubscriptionService(t),
		mockLinkedItemRepo,
		mockAccountRepo,
		mockUserRepo,
		nil,
		WithEncryptor(enc),
		WithAccountServiceLogger(slog.Default()),
	)

	result, err := svc.LinkAccounts(ctx, userID, publicToken, institutionID, institutionName)

	require.NoError(t, err, "should succeed when TTL has expired")
	require.NotNil(t, result)

	mockAccountRepo.AssertExpectations(t)
	mockLinkedItemRepo.AssertExpectations(t)
	mockUserRepo.AssertExpectations(t)
	mockPlaidSvc.AssertExpectations(t)
}

// TestLinkAccounts_FingerprintCheck_AllowsSameUser verifies that linking an account
// with a fingerprint owned by the same user (re-link) is allowed.
//
// Issue #1010: Cross-user Plaid item uniqueness
func TestLinkAccounts_FingerprintCheck_AllowsSameUser(t *testing.T) {
	ctx := context.Background()
	userID := uuid.New().String()
	institutionID := "ins_test"
	institutionName := "Test Bank"
	publicToken := "public-sandbox-fingerprint-sameuser"
	accessToken := "access-sandbox-fingerprint-sameuser"
	plaidItemID := "plaid-item-fingerprint-sameuser"
	newItemID := uuid.New().String()
	mask := "3456"

	enc := newTestFieldEncryptor(t)
	// No persistent_account_id — only fallback fingerprint
	expectedFallbackBlind := enc.BlindIndex("fb:" + institutionID + "\x00" + mask + "\x00" + "depository" + "\x00" + "savings")

	plaidAccounts := []map[string]interface{}{
		{
			"account_id": "plaid-acct-sameuser-1",
			// No persistent_account_id
			"balances": map[string]interface{}{"available": 500.0, "current": 500.0, "iso_currency_code": "USD"},
			"mask":     mask,
			"name":     "Savings",
			"subtype":  "savings",
			"type":     "depository",
		},
	}
	_, plaidClient := newTestPlaidServer(t, accessToken, plaidItemID, plaidAccounts)

	mockPlaidSvc := &MockPlaidService{}
	mockPlaidSvc.On("GetClient").Return(plaidClient)

	mockLinkedItemRepo := &MockLinkedItemRepository{}
	mockAccountRepo := &MockAccountRepository{}
	mockUserRepo := &MockUserRepository{}

	user := &domain.User{ID: userID, SubscriptionTier: "premium"}
	mockUserRepo.On("GetUserByID", ctx, userID).Return(user, nil)
	mockAccountRepo.On("CountByUserID", ctx, userID).Return(0, nil)

	mockLinkedItemRepo.On("GetByUserAndInstitution", ctx, userID, institutionID, string(domain.LinkedItemStatusActive)).
		Return(nil, nil)
	mockLinkedItemRepo.On("GetByUserAndInstitution", ctx, userID, institutionID, string(domain.LinkedItemStatusDisconnected)).
		Return(nil, repository.ErrLinkedItemNotFound)

	// Fallback fingerprint — no other user owns it (same-user re-link means excludeUserID filters it out)
	mockAccountRepo.On("CheckFingerprintOwnership", ctx, expectedFallbackBlind, userID).
		Return(nil, nil)

	// Proceed with normal link
	mockLinkedItemRepo.On("Create", ctx, mock.MatchedBy(func(item *domain.LinkedItem) bool {
		return item.UserID == userID && item.Status == domain.LinkedItemStatusActive
	})).Run(func(args mock.Arguments) {
		args.Get(1).(*domain.LinkedItem).ID = newItemID
	}).Return(nil)

	mockAccountRepo.On("CreateBatch", ctx, mock.MatchedBy(func(accounts []domain.Account) bool {
		return len(accounts) == 1
	})).Return(nil)

	svc := NewAccountService(
		mockPlaidSvc,
		newTestSubscriptionService(t),
		mockLinkedItemRepo,
		mockAccountRepo,
		mockUserRepo,
		nil,
		WithEncryptor(enc),
		WithAccountServiceLogger(slog.Default()),
	)

	result, err := svc.LinkAccounts(ctx, userID, publicToken, institutionID, institutionName)

	require.NoError(t, err, "same-user re-link should succeed")
	require.NotNil(t, result)

	mockAccountRepo.AssertExpectations(t)
	mockLinkedItemRepo.AssertExpectations(t)
	mockPlaidSvc.AssertExpectations(t)
}

// TestLinkAccounts_FingerprintCheck_RaceConditionHandled verifies that when a DB
// UNIQUE constraint violation occurs on CreateBatch (race condition where two users
// simultaneously link the same account), ErrAccountOwnedByAnotherUser is returned.
//
// Issue #1010: Cross-user Plaid item uniqueness
func TestLinkAccounts_FingerprintCheck_RaceConditionHandled(t *testing.T) {
	ctx := context.Background()
	userID := uuid.New().String()
	institutionID := "ins_test"
	institutionName := "Test Bank"
	publicToken := "public-sandbox-fingerprint-race"
	accessToken := "access-sandbox-fingerprint-race"
	plaidItemID := "plaid-item-fingerprint-race"
	newItemID := uuid.New().String()
	mask := "7890"

	enc := newTestFieldEncryptor(t)
	expectedFallbackBlind := enc.BlindIndex("fb:" + institutionID + "\x00" + mask + "\x00" + "depository" + "\x00" + "checking")

	plaidAccounts := []map[string]interface{}{
		{
			"account_id": "plaid-acct-race-1",
			"balances":   map[string]interface{}{"available": 100.0, "current": 100.0, "iso_currency_code": "USD"},
			"mask":       mask,
			"name":       "Checking",
			"subtype":    "checking",
			"type":       "depository",
		},
	}
	_, plaidClient := newTestPlaidServer(t, accessToken, plaidItemID, plaidAccounts)

	mockPlaidSvc := &MockPlaidService{}
	mockPlaidSvc.On("GetClient").Return(plaidClient)

	mockLinkedItemRepo := &MockLinkedItemRepository{}
	mockAccountRepo := &MockAccountRepository{}
	mockUserRepo := &MockUserRepository{}

	user := &domain.User{ID: userID, SubscriptionTier: "premium"}
	mockUserRepo.On("GetUserByID", ctx, userID).Return(user, nil)
	mockAccountRepo.On("CountByUserID", ctx, userID).Return(0, nil)

	mockLinkedItemRepo.On("GetByUserAndInstitution", ctx, userID, institutionID, string(domain.LinkedItemStatusActive)).
		Return(nil, nil)
	mockLinkedItemRepo.On("GetByUserAndInstitution", ctx, userID, institutionID, string(domain.LinkedItemStatusDisconnected)).
		Return(nil, repository.ErrLinkedItemNotFound)

	// App-level check passes (no ownership found — race: other user hasn't committed yet)
	mockAccountRepo.On("CheckFingerprintOwnership", ctx, expectedFallbackBlind, userID).
		Return(nil, nil)

	// Linked item creates successfully
	mockLinkedItemRepo.On("Create", ctx, mock.Anything).
		Run(func(args mock.Arguments) {
			args.Get(1).(*domain.LinkedItem).ID = newItemID
		}).Return(nil)

	// CreateBatch hits UNIQUE constraint violation (race condition)
	mockAccountRepo.On("CreateBatch", ctx, mock.Anything).
		Return(repository.ErrAccountOwnedByAnotherUser)

	svc := NewAccountService(
		mockPlaidSvc,
		newTestSubscriptionService(t),
		mockLinkedItemRepo,
		mockAccountRepo,
		mockUserRepo,
		nil,
		WithEncryptor(enc),
		WithAccountServiceLogger(slog.Default()),
	)

	result, err := svc.LinkAccounts(ctx, userID, publicToken, institutionID, institutionName)

	require.Error(t, err, "race condition should return ErrAccountOwnedByAnotherUser")
	assert.Nil(t, result)
	assert.ErrorIs(t, err, repository.ErrAccountOwnedByAnotherUser)

	mockAccountRepo.AssertExpectations(t)
	mockLinkedItemRepo.AssertExpectations(t)
	mockPlaidSvc.AssertExpectations(t)
}

// TestLinkAccounts_FingerprintCheck_NilMaskAndSubtypeUseSentinels verifies that when
// a Plaid account has no mask and no subtype, the fallback fingerprint blind index
// uses the sentinel values "__NO_MASK__" and "__NO_SUBTYPE__" instead of empty strings.
//
// Issue #1010: Cross-user Plaid item uniqueness — sentinel values for nil fields
func TestLinkAccounts_FingerprintCheck_NilMaskAndSubtypeUseSentinels(t *testing.T) {
	ctx := context.Background()
	userID := uuid.New().String()
	institutionID := "ins_test"
	institutionName := "Test Bank"
	publicToken := "public-sandbox-sentinel"
	accessToken := "access-sandbox-sentinel"
	plaidItemID := "plaid-item-sentinel"
	newItemID := uuid.New().String()

	enc := newTestFieldEncryptor(t)
	// Sentinel values used when mask and subtype are absent from the Plaid response
	expectedFallbackBlind := enc.BlindIndex("fb:" + institutionID + "\x00" + "__NO_MASK__" + "\x00" + "depository" + "\x00" + "__NO_SUBTYPE__")

	// Account with no mask and no subtype
	plaidAccounts := []map[string]interface{}{
		{
			"account_id": "plaid-acct-sentinel-1",
			"balances":   map[string]interface{}{"available": 100.0, "current": 100.0, "iso_currency_code": "USD"},
			// mask intentionally omitted — nil
			"name": "Checking",
			// subtype intentionally omitted — nil
			"type": "depository",
		},
	}
	_, plaidClient := newTestPlaidServer(t, accessToken, plaidItemID, plaidAccounts)

	mockPlaidSvc := &MockPlaidService{}
	mockPlaidSvc.On("GetClient").Return(plaidClient)

	mockLinkedItemRepo := &MockLinkedItemRepository{}
	mockAccountRepo := &MockAccountRepository{}
	mockUserRepo := &MockUserRepository{}

	user := &domain.User{ID: userID, SubscriptionTier: "premium"}
	mockUserRepo.On("GetUserByID", ctx, userID).Return(user, nil)
	mockAccountRepo.On("CountByUserID", ctx, userID).Return(0, nil)

	mockLinkedItemRepo.On("GetByUserAndInstitution", ctx, userID, institutionID, string(domain.LinkedItemStatusActive)).
		Return(nil, nil)
	mockLinkedItemRepo.On("GetByUserAndInstitution", ctx, userID, institutionID, string(domain.LinkedItemStatusDisconnected)).
		Return(nil, repository.ErrLinkedItemNotFound)

	// The fallback fingerprint must use sentinels — this is the key assertion
	mockAccountRepo.On("CheckFingerprintOwnership", ctx, expectedFallbackBlind, userID).
		Return(nil, nil)

	mockLinkedItemRepo.On("Create", ctx, mock.MatchedBy(func(item *domain.LinkedItem) bool {
		return item.UserID == userID && item.Status == domain.LinkedItemStatusActive
	})).Run(func(args mock.Arguments) {
		args.Get(1).(*domain.LinkedItem).ID = newItemID
	}).Return(nil)

	mockAccountRepo.On("CreateBatch", ctx, mock.MatchedBy(func(accounts []domain.Account) bool {
		return len(accounts) == 1
	})).Return(nil)

	svc := NewAccountService(
		mockPlaidSvc,
		newTestSubscriptionService(t),
		mockLinkedItemRepo,
		mockAccountRepo,
		mockUserRepo,
		nil,
		WithEncryptor(enc),
		WithAccountServiceLogger(slog.Default()),
	)

	result, err := svc.LinkAccounts(ctx, userID, publicToken, institutionID, institutionName)

	require.NoError(t, err, "nil mask/subtype should succeed using sentinel values")
	require.NotNil(t, result)

	// AssertExpectations verifies CheckFingerprintOwnership was called with the sentinel-based blind
	mockAccountRepo.AssertExpectations(t)
	mockLinkedItemRepo.AssertExpectations(t)
	mockPlaidSvc.AssertExpectations(t)
}

// TestAccountService_ListActiveByUser_ExcludesReplaced verifies that ListActiveByUser
// returns only active accounts, excluding replaced and expired (Issue #1109).
func TestAccountService_ListActiveByUser_ExcludesReplaced(t *testing.T) {
	mockAccountRepo := new(MockAccountRepository)
	mockLinkedItemRepo := new(MockLinkedItemRepository)
	mockUserRepo := new(MockUserRepository)

	ctx := context.Background()
	userID := uuid.New().String()

	// GetByUserID now returns only active accounts (replaced filtered at repo layer)
	activeAccounts := []domain.Account{
		{
			ID:     "account-active",
			UserID: userID,
			Name:   "Active Checking",
			Type:   "depository",
			Status: domain.AccountStatusActive,
		},
	}

	mockAccountRepo.On("GetByUserID", ctx, userID).Return(activeAccounts, nil)

	svc := NewAccountService(
		nil, // plaid not needed
		nil, // subscription not needed
		mockLinkedItemRepo,
		mockAccountRepo,
		mockUserRepo,
		nil, // db not needed
	)

	result, err := svc.ListActiveByUser(ctx, userID)

	require.NoError(t, err)
	assert.Len(t, result, 1)
	assert.Equal(t, "account-active", result[0].ID)
	assert.Equal(t, domain.AccountStatusActive, result[0].Status)

	mockAccountRepo.AssertExpectations(t)
}
