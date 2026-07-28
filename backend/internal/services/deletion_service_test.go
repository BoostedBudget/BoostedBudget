package services

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/jackc/pgx/v5"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	phclient "github.com/maxnussbaum/BoostedBudget/internal/integration/posthog"
	"github.com/maxnussbaum/BoostedBudget/internal/repository"
	"github.com/pashagolub/pgxmock/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// MockProfileRepositoryForDeletion for testing deletion service
type MockProfileRepositoryForDeletion struct {
	mock.Mock
}

func (m *MockProfileRepositoryForDeletion) GetUserByID(ctx context.Context, userID string) (*domain.User, error) {
	args := m.Called(ctx, userID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*domain.User), args.Error(1)
}

func (m *MockProfileRepositoryForDeletion) GetUserByClerkID(ctx context.Context, clerkUserID string) (*domain.User, error) {
	args := m.Called(ctx, clerkUserID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*domain.User), args.Error(1)
}

func (m *MockProfileRepositoryForDeletion) GetUserByPaddleSubscriptionID(ctx context.Context, subscriptionID string) (*domain.User, error) {
	args := m.Called(ctx, subscriptionID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*domain.User), args.Error(1)
}

func (m *MockProfileRepositoryForDeletion) CreateUser(ctx context.Context, user *domain.User) error {
	args := m.Called(ctx, user)
	return args.Error(0)
}

func (m *MockProfileRepositoryForDeletion) CreateUserTx(ctx context.Context, tx pgx.Tx, user *domain.User) error {
	args := m.Called(ctx, tx, user)
	return args.Error(0)
}

func (m *MockProfileRepositoryForDeletion) UpdateUser(ctx context.Context, user *domain.User) error {
	args := m.Called(ctx, user)
	return args.Error(0)
}

func (m *MockProfileRepositoryForDeletion) UpdateLastLogin(ctx context.Context, clerkUserID string) error {
	args := m.Called(ctx, clerkUserID)
	return args.Error(0)
}

func (m *MockProfileRepositoryForDeletion) MarkForDeletion(ctx context.Context, userID string, scheduledAt time.Time) error {
	args := m.Called(ctx, userID, scheduledAt)
	return args.Error(0)
}

func (m *MockProfileRepositoryForDeletion) CancelDeletion(ctx context.Context, userID string) error {
	args := m.Called(ctx, userID)
	return args.Error(0)
}

func (m *MockProfileRepositoryForDeletion) GetUsersScheduledForDeletion(ctx context.Context) ([]*domain.User, error) {
	args := m.Called(ctx)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]*domain.User), args.Error(1)
}

func (m *MockProfileRepositoryForDeletion) DeleteUserCascade(ctx context.Context, userID string) error {
	args := m.Called(ctx, userID)
	return args.Error(0)
}

// Issue #679, #680: Trial expiration methods
func (m *MockProfileRepositoryForDeletion) GetUsersWithExpiredTrials(ctx context.Context) ([]*domain.User, error) {
	args := m.Called(ctx)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]*domain.User), args.Error(1)
}

func (m *MockProfileRepositoryForDeletion) GetUsersWithExpiringTrials(ctx context.Context, withinDays int) ([]*domain.User, error) {
	args := m.Called(ctx, withinDays)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]*domain.User), args.Error(1)
}

func (m *MockProfileRepositoryForDeletion) UpdateSubscriptionStatus(ctx context.Context, userID string, status string) error {
	args := m.Called(ctx, userID, status)
	return args.Error(0)
}

func (m *MockProfileRepositoryForDeletion) UpdateTrialStatus(ctx context.Context, userID string, status string, trialStartedAt, trialEndsAt *time.Time, hadTrial bool) error {
	args := m.Called(ctx, userID, status, trialStartedAt, trialEndsAt, hadTrial)
	return args.Error(0)
}

func (m *MockProfileRepositoryForDeletion) UpdatePaddleCustomerID(ctx context.Context, userID, paddleCustomerID string) error {
	args := m.Called(ctx, userID, paddleCustomerID)
	return args.Error(0)
}

func (m *MockProfileRepositoryForDeletion) UpdateSubscriptionFromPaddle(ctx context.Context, userID, tier, status string, startedAt, endsAt *time.Time, subscriptionID *string) error {
	args := m.Called(ctx, userID, tier, status, startedAt, endsAt, subscriptionID)
	return args.Error(0)
}

func (m *MockProfileRepositoryForDeletion) GetUsersWithActivePaddleSubscriptions(ctx context.Context) ([]*domain.User, error) {
	args := m.Called(ctx)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]*domain.User), args.Error(1)
}

// Retention tracking stubs (#930)
func (m *MockProfileRepositoryForDeletion) GetEffectiveSubscriptionStatusByUserID(ctx context.Context, userID string) (string, error) {
	args := m.Called(ctx, userID)
	return args.String(0), args.Error(1)
}
func (m *MockProfileRepositoryForDeletion) SetRetentionDates(ctx context.Context, userID string, startedAt, expiresAt time.Time) error {
	return nil
}
func (m *MockProfileRepositoryForDeletion) ClearRetentionDates(ctx context.Context, userID string) error {
	return nil
}
func (m *MockProfileRepositoryForDeletion) SetRetentionNotice30dSent(ctx context.Context, userID string) error {
	return nil
}
func (m *MockProfileRepositoryForDeletion) SetRetentionNotice7dSent(ctx context.Context, userID string) error {
	return nil
}
func (m *MockProfileRepositoryForDeletion) GetUsersNeedingRetention30dNotice(ctx context.Context) ([]*domain.User, error) {
	return nil, nil
}
func (m *MockProfileRepositoryForDeletion) GetUsersNeedingRetention7dNotice(ctx context.Context) ([]*domain.User, error) {
	return nil, nil
}
func (m *MockProfileRepositoryForDeletion) GetUsersWithExpiredRetention(ctx context.Context) ([]*domain.User, error) {
	return nil, nil
}
func (m *MockProfileRepositoryForDeletion) GetUsersWithExpiredManualTierOverride(ctx context.Context) ([]*domain.User, error) {
	return nil, nil
}

// Dunning methods (#898)
func (m *MockProfileRepositoryForDeletion) SetPastDueStartedAt(ctx context.Context, userID string, t time.Time) error {
	return nil
}
func (m *MockProfileRepositoryForDeletion) ClearPastDueStartedAt(ctx context.Context, userID string) error {
	return nil
}
func (m *MockProfileRepositoryForDeletion) GetUsersInPastDue(ctx context.Context) ([]domain.User, error) {
	return []domain.User{}, nil
}
func (m *MockProfileRepositoryForDeletion) CancelIfPastDue(ctx context.Context, userID string) (bool, error) {
	return false, nil
}

func (m *MockProfileRepositoryForDeletion) ClaimPlaidDisconnectIfPastDue(ctx context.Context, userID string) (bool, error) {
	return false, nil
}

func (m *MockProfileRepositoryForDeletion) ExpireTrialIfNoOverride(ctx context.Context, userID string) (bool, error) {
	return false, nil
}

// ClaimForDeletion (Issue #1413 Critical finding C2, Codex review round 2) is stubbed via
// m.Called so individual tests can assert the claimed/not-claimed branch explicitly — unlike
// the other narrow-interface CAS stubs above, ExecuteDeletion's own tests need to control this
// return value directly.
func (m *MockProfileRepositoryForDeletion) ClaimForDeletion(ctx context.Context, userID string) (bool, error) {
	args := m.Called(ctx, userID)
	return args.Bool(0), args.Error(1)
}

func (m *MockProfileRepositoryForDeletion) IsRetentionNotice30dEligible(ctx context.Context, userID string) (bool, error) {
	return false, nil
}

func (m *MockProfileRepositoryForDeletion) IsRetentionNotice7dEligible(ctx context.Context, userID string) (bool, error) {
	return false, nil
}

func (m *MockProfileRepositoryForDeletion) GetUserByEmail(ctx context.Context, email string) (*domain.User, error) {
	return nil, nil
}

func (m *MockProfileRepositoryForDeletion) UpdateUserEmailTx(ctx context.Context, tx pgx.Tx, userID, plainEmail string) error {
	args := m.Called(ctx, tx, userID, plainEmail)
	return args.Error(0)
}

// MockEmailService for testing
type MockEmailService struct {
	mock.Mock
}

func (m *MockEmailService) SendDeletionConfirmation(ctx context.Context, email, cancellationToken string, scheduledDate time.Time) error {
	args := m.Called(ctx, email, cancellationToken, scheduledDate)
	return args.Error(0)
}

func (m *MockEmailService) SendNotificationEmail(ctx context.Context, toEmail string, notification *domain.Notification) error {
	args := m.Called(ctx, toEmail, notification)
	return args.Error(0)
}

// SendHouseholdInvite (#1367 — Phase 3) is not exercised by these deletion tests;
// stubbed to satisfy the expanded EmailService interface.
func (m *MockEmailService) SendHouseholdInvite(ctx context.Context, toEmail, inviterName, householdName, inviteURL string, expiresAt time.Time) error {
	args := m.Called(ctx, toEmail, inviterName, householdName, inviteURL, expiresAt)
	return args.Error(0)
}

// SendHouseholdMemberRemoved (#1371 — Phase 7, Task 3) is not exercised by these
// deletion tests; stubbed to satisfy the expanded EmailService interface.
func (m *MockEmailService) SendHouseholdMemberRemoved(ctx context.Context, toEmail, householdName string, purgeDate time.Time, disbanded bool) error {
	args := m.Called(ctx, toEmail, householdName, purgeDate, disbanded)
	return args.Error(0)
}

// SendHouseholdOverCapacityNotice/Warning (#1371 — Phase 7, Task 6) are not exercised
// by these deletion tests; stubbed to satisfy the expanded EmailService interface.
func (m *MockEmailService) SendHouseholdOverCapacityNotice(ctx context.Context, toEmail, householdName string, activeCount, seatLimit int, deadline time.Time) error {
	args := m.Called(ctx, toEmail, householdName, activeCount, seatLimit, deadline)
	return args.Error(0)
}

func (m *MockEmailService) SendHouseholdOverCapacityWarning(ctx context.Context, toEmail, householdName string, activeCount, seatLimit int, deadline time.Time) error {
	args := m.Called(ctx, toEmail, householdName, activeCount, seatLimit, deadline)
	return args.Error(0)
}

// MockClerkService for testing
type MockClerkService struct {
	mock.Mock
}

func (m *MockClerkService) DeleteUser(ctx context.Context, clerkUserID string) error {
	args := m.Called(ctx, clerkUserID)
	return args.Error(0)
}

func (m *MockClerkService) UpdateUserMetadata(ctx context.Context, clerkUserID string, metadata map[string]interface{}) error {
	args := m.Called(ctx, clerkUserID, metadata)
	return args.Error(0)
}

func (m *MockClerkService) UpdateUserEmail(ctx context.Context, clerkUserID, newEmail string) error {
	args := m.Called(ctx, clerkUserID, newEmail)
	return args.Error(0)
}

// MockAuditLogger for testing
type MockAuditLogger struct {
	mock.Mock
}

func (m *MockAuditLogger) Log(ctx context.Context, action, resourceType, resourceID string) error {
	args := m.Called(ctx, action, resourceType, resourceID)
	return args.Error(0)
}

// MockWorkClaimsRepository mocks repository.WorkClaimsRepository (Issue #1413 Critical finding
// C2) — the durable, fencing-token-guarded claim mechanism ExecuteDeletion uses for exclusivity
// on ActionAccountDeletion, wired in via SetWorkClaimsRepo.
type MockWorkClaimsRepository struct {
	mock.Mock
}

func (m *MockWorkClaimsRepository) Claim(ctx context.Context, resourceType, resourceID, action string, lease time.Duration) (string, bool, error) {
	args := m.Called(ctx, resourceType, resourceID, action, lease)
	return args.String(0), args.Bool(1), args.Error(2)
}

func (m *MockWorkClaimsRepository) Complete(ctx context.Context, resourceType, resourceID, action, token string) (bool, error) {
	args := m.Called(ctx, resourceType, resourceID, action, token)
	return args.Bool(0), args.Error(1)
}

func (m *MockWorkClaimsRepository) Release(ctx context.Context, resourceType, resourceID, action, token, reason string) (bool, error) {
	args := m.Called(ctx, resourceType, resourceID, action, token, reason)
	return args.Bool(0), args.Error(1)
}

// testDeletionClaimToken is the fixed fencing token newAllowingClaimsRepo's Claim stub returns.
// Tests that only care about ExecuteDeletion's OTHER behavior (Paddle/Plaid/cascade ordering,
// etc.) can assert Complete was called with this exact value without having to plumb a real
// per-test token through.
const testDeletionClaimToken = "test-claim-token"

// newAllowingClaimsRepo returns a MockWorkClaimsRepository stubbed to always grant the
// ActionAccountDeletion claim and always complete it successfully — for tests that exercise
// ExecuteDeletion's OTHER behavior and aren't themselves testing the claim mechanism. userID is
// wildcarded (mock.Anything) so one instance works across this file's whole matrix of fixture
// users. Both expectations are .Maybe() because some callers (e.g. the already-deleted /
// belt-and-braces-skip tests) never reach the claim at all.
func newAllowingClaimsRepo() *MockWorkClaimsRepository {
	m := new(MockWorkClaimsRepository)
	m.On("Claim", mock.Anything, repository.ResourceTypeUser, mock.Anything, repository.ActionAccountDeletion, accountDeletionClaimLease).
		Return(testDeletionClaimToken, true, nil).Maybe()
	m.On("Complete", mock.Anything, repository.ResourceTypeUser, mock.Anything, repository.ActionAccountDeletion, testDeletionClaimToken).
		Return(true, nil).Maybe()
	m.On("Release", mock.Anything, repository.ResourceTypeUser, mock.Anything, repository.ActionAccountDeletion, testDeletionClaimToken, mock.AnythingOfType("string")).
		Return(true, nil).Maybe()
	return m
}

// TestRequestDeletion_Success tests successful deletion request
func TestRequestDeletion_Success(t *testing.T) {
	ctx := context.Background()
	mockRepo := new(MockProfileRepositoryForDeletion)
	mockEmail := new(MockEmailService)
	mockClerk := new(MockClerkService)
	mockAudit := new(MockAuditLogger)

	service := NewDeletionService(mockRepo, mockEmail, mockClerk, mockAudit, nil, 30, "test-secret-key")

	userID := "user-123"
	email := "test@example.com"

	mockRepo.On("MarkForDeletion", ctx, userID, mock.AnythingOfType("time.Time")).Return(nil)
	mockEmail.On("SendDeletionConfirmation", ctx, email, mock.AnythingOfType("string"), mock.AnythingOfType("time.Time")).Return(nil)
	mockAudit.On("Log", ctx, "user.deletion_requested", "user", userID).Return(nil)

	token, scheduledTime, err := service.RequestDeletion(ctx, userID, email)

	assert.NoError(t, err)
	assert.NotEmpty(t, token)
	assert.True(t, scheduledTime.After(time.Now().Add(29*24*time.Hour)))
	assert.True(t, scheduledTime.Before(time.Now().Add(31*24*time.Hour)))
	mockRepo.AssertExpectations(t)
	mockEmail.AssertExpectations(t)
	mockAudit.AssertExpectations(t)
}

// TestRequestDeletion_SchedulesFor30Days verifies 30-day scheduling
func TestRequestDeletion_SchedulesFor30Days(t *testing.T) {
	ctx := context.Background()
	mockRepo := new(MockProfileRepositoryForDeletion)
	mockEmail := new(MockEmailService)
	mockClerk := new(MockClerkService)
	mockAudit := new(MockAuditLogger)

	gracePeriodDays := 30
	service := NewDeletionService(mockRepo, mockEmail, mockClerk, mockAudit, nil, gracePeriodDays, "test-secret-key")

	userID := "user-123"
	email := "test@example.com"

	mockRepo.On("MarkForDeletion", ctx, userID, mock.AnythingOfType("time.Time")).Return(nil)
	mockEmail.On("SendDeletionConfirmation", ctx, email, mock.AnythingOfType("string"), mock.AnythingOfType("time.Time")).Return(nil)
	mockAudit.On("Log", ctx, "user.deletion_requested", "user", userID).Return(nil)

	_, scheduledTime, err := service.RequestDeletion(ctx, userID, email)

	assert.NoError(t, err)
	expectedTime := time.Now().Add(time.Duration(gracePeriodDays) * 24 * time.Hour)
	diff := scheduledTime.Sub(expectedTime)
	assert.True(t, diff < time.Minute && diff > -time.Minute, "Scheduled time should be approximately 30 days from now")
}

// TestRequestDeletion_GeneratesToken verifies token generation
func TestRequestDeletion_GeneratesToken(t *testing.T) {
	ctx := context.Background()
	mockRepo := new(MockProfileRepositoryForDeletion)
	mockEmail := new(MockEmailService)
	mockClerk := new(MockClerkService)
	mockAudit := new(MockAuditLogger)

	service := NewDeletionService(mockRepo, mockEmail, mockClerk, mockAudit, nil, 30, "test-secret-key")

	userID := "user-123"
	email := "test@example.com"

	mockRepo.On("MarkForDeletion", ctx, userID, mock.AnythingOfType("time.Time")).Return(nil)
	mockEmail.On("SendDeletionConfirmation", ctx, email, mock.AnythingOfType("string"), mock.AnythingOfType("time.Time")).Return(nil)
	mockAudit.On("Log", ctx, "user.deletion_requested", "user", userID).Return(nil)

	token, _, err := service.RequestDeletion(ctx, userID, email)

	assert.NoError(t, err)
	assert.NotEmpty(t, token)
	assert.True(t, len(token) > 20, "Token should be a reasonable length")
}

// TestRequestDeletion_CreatesAuditLog verifies audit logging
func TestRequestDeletion_CreatesAuditLog(t *testing.T) {
	ctx := context.Background()
	mockRepo := new(MockProfileRepositoryForDeletion)
	mockEmail := new(MockEmailService)
	mockClerk := new(MockClerkService)
	mockAudit := new(MockAuditLogger)

	service := NewDeletionService(mockRepo, mockEmail, mockClerk, mockAudit, nil, 30, "test-secret-key")

	userID := "user-123"
	email := "test@example.com"

	mockRepo.On("MarkForDeletion", ctx, userID, mock.AnythingOfType("time.Time")).Return(nil)
	mockEmail.On("SendDeletionConfirmation", ctx, email, mock.AnythingOfType("string"), mock.AnythingOfType("time.Time")).Return(nil)
	mockAudit.On("Log", ctx, "user.deletion_requested", "user", userID).Return(nil)

	_, _, err := service.RequestDeletion(ctx, userID, email)

	assert.NoError(t, err)
	mockAudit.AssertExpectations(t)
	mockAudit.AssertCalled(t, "Log", ctx, "user.deletion_requested", "user", userID)
}

// TestRequestDeletion_EmailServiceFailure tests email failure handling
func TestRequestDeletion_EmailServiceFailure(t *testing.T) {
	ctx := context.Background()
	mockRepo := new(MockProfileRepositoryForDeletion)
	mockEmail := new(MockEmailService)
	mockClerk := new(MockClerkService)
	mockAudit := new(MockAuditLogger)

	service := NewDeletionService(mockRepo, mockEmail, mockClerk, mockAudit, nil, 30, "test-secret-key")

	userID := "user-123"
	email := "test@example.com"

	mockRepo.On("MarkForDeletion", ctx, userID, mock.AnythingOfType("time.Time")).Return(nil)
	mockEmail.On("SendDeletionConfirmation", ctx, email, mock.AnythingOfType("string"), mock.AnythingOfType("time.Time")).Return(errors.New("email service error"))
	mockAudit.On("Log", ctx, "user.deletion_requested", "user", userID).Return(nil)

	_, _, err := service.RequestDeletion(ctx, userID, email)

	// Should still succeed even if email fails (deletion is scheduled)
	assert.NoError(t, err)
}

// TestCancelDeletion_ValidToken tests successful cancellation
func TestCancelDeletion_ValidToken(t *testing.T) {
	ctx := context.Background()
	mockRepo := new(MockProfileRepositoryForDeletion)
	mockEmail := new(MockEmailService)
	mockClerk := new(MockClerkService)
	mockAudit := new(MockAuditLogger)

	service := NewDeletionService(mockRepo, mockEmail, mockClerk, mockAudit, nil, 30, "test-secret-key")

	// First request deletion to get a token
	userID := "user-123"
	email := "test@example.com"

	mockRepo.On("MarkForDeletion", ctx, userID, mock.AnythingOfType("time.Time")).Return(nil)
	mockEmail.On("SendDeletionConfirmation", ctx, email, mock.AnythingOfType("string"), mock.AnythingOfType("time.Time")).Return(nil)
	mockAudit.On("Log", ctx, "user.deletion_requested", "user", userID).Return(nil)

	token, _, _ := service.RequestDeletion(ctx, userID, email)

	// Now cancel with the token
	mockRepo.On("CancelDeletion", ctx, userID).Return(nil)
	mockAudit.On("Log", ctx, "user.deletion_cancelled", "user", userID).Return(nil)

	err := service.CancelDeletion(ctx, token)

	assert.NoError(t, err)
	mockRepo.AssertCalled(t, "CancelDeletion", ctx, userID)
	mockAudit.AssertCalled(t, "Log", ctx, "user.deletion_cancelled", "user", userID)
}

// TestCancelDeletion_InvalidToken tests invalid token handling
func TestCancelDeletion_InvalidToken(t *testing.T) {
	ctx := context.Background()
	mockRepo := new(MockProfileRepositoryForDeletion)
	mockEmail := new(MockEmailService)
	mockClerk := new(MockClerkService)
	mockAudit := new(MockAuditLogger)

	service := NewDeletionService(mockRepo, mockEmail, mockClerk, mockAudit, nil, 30, "test-secret-key")

	invalidToken := "invalid-token-string"

	err := service.CancelDeletion(ctx, invalidToken)

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid token")
	mockRepo.AssertNotCalled(t, "CancelDeletion", mock.Anything, mock.Anything)
}

// TestCancelDeletion_ExpiredToken tests expired token handling
func TestCancelDeletion_ExpiredToken(t *testing.T) {
	ctx := context.Background()
	mockRepo := new(MockProfileRepositoryForDeletion)
	mockEmail := new(MockEmailService)
	mockClerk := new(MockClerkService)
	mockAudit := new(MockAuditLogger)

	tokenSecret := "test-secret-key"
	service := NewDeletionService(mockRepo, mockEmail, mockClerk, mockAudit, nil, 30, tokenSecret)

	// Create a real expired JWT token (expired 1 hour ago)
	expiredTime := time.Now().Add(-1 * time.Hour)
	claims := &tokenClaims{
		UserID: "user-123",
		Email:  "test@example.com",
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(expiredTime),
			IssuedAt:  jwt.NewNumericDate(time.Now().Add(-2 * time.Hour)),
			NotBefore: jwt.NewNumericDate(time.Now().Add(-2 * time.Hour)),
			Issuer:    "boostedbudget-deletion-service",
			Subject:   "user-123",
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	expiredToken, _ := token.SignedString([]byte(tokenSecret))

	err := service.CancelDeletion(ctx, expiredToken)

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "expired")
}

// TestExecuteDeletion_CascadesAllData tests complete data deletion
func TestExecuteDeletion_CascadesAllData(t *testing.T) {
	ctx := context.Background()
	mockRepo := new(MockProfileRepositoryForDeletion)
	mockEmail := new(MockEmailService)
	mockClerk := new(MockClerkService)
	mockAudit := new(MockAuditLogger)

	service := NewDeletionService(mockRepo, mockEmail, mockClerk, mockAudit, nil, 30, "test-secret-key")

	user := &domain.User{
		ID:          "user-123",
		ClerkUserID: "user_123",
		Email:       "test@example.com",
		IsDeleted:   false,
	}

	mockRepo.On("GetUserByID", ctx, user.ID).Return(user, nil)
	mockRepo.On("ClaimForDeletion", ctx, user.ID).Return(true, nil)
	service.SetWorkClaimsRepo(newAllowingClaimsRepo())
	mockRepo.On("DeleteUserCascade", ctx, user.ID).Return(nil)
	mockClerk.On("DeleteUser", ctx, user.ClerkUserID).Return(nil)
	mockAudit.On("Log", ctx, "user.deleted", "user", user.ID).Return(nil)

	err := service.ExecuteDeletion(ctx, user.ID)

	assert.NoError(t, err)
	mockRepo.AssertCalled(t, "DeleteUserCascade", ctx, user.ID)
	mockClerk.AssertCalled(t, "DeleteUser", ctx, user.ClerkUserID)
	mockAudit.AssertCalled(t, "Log", ctx, "user.deleted", "user", user.ID)
}

// TestExecuteDeletion_DeletesClerkUser verifies Clerk deletion
func TestExecuteDeletion_DeletesClerkUser(t *testing.T) {
	ctx := context.Background()
	mockRepo := new(MockProfileRepositoryForDeletion)
	mockEmail := new(MockEmailService)
	mockClerk := new(MockClerkService)
	mockAudit := new(MockAuditLogger)

	service := NewDeletionService(mockRepo, mockEmail, mockClerk, mockAudit, nil, 30, "test-secret-key")

	user := &domain.User{
		ID:          "user-123",
		ClerkUserID: "user_123",
		Email:       "test@example.com",
		IsDeleted:   false,
	}

	mockRepo.On("GetUserByID", ctx, user.ID).Return(user, nil)
	mockRepo.On("ClaimForDeletion", ctx, user.ID).Return(true, nil)
	service.SetWorkClaimsRepo(newAllowingClaimsRepo())
	mockRepo.On("DeleteUserCascade", ctx, user.ID).Return(nil)
	mockClerk.On("DeleteUser", ctx, user.ClerkUserID).Return(nil)
	mockAudit.On("Log", ctx, "user.deleted", "user", user.ID).Return(nil)

	err := service.ExecuteDeletion(ctx, user.ID)

	assert.NoError(t, err)
	mockClerk.AssertExpectations(t)
	mockClerk.AssertCalled(t, "DeleteUser", ctx, user.ClerkUserID)
}

// TestExecuteDeletion_HandlesAlreadyDeleted tests idempotency
func TestExecuteDeletion_HandlesAlreadyDeleted(t *testing.T) {
	ctx := context.Background()
	mockRepo := new(MockProfileRepositoryForDeletion)
	mockEmail := new(MockEmailService)
	mockClerk := new(MockClerkService)
	mockAudit := new(MockAuditLogger)

	service := NewDeletionService(mockRepo, mockEmail, mockClerk, mockAudit, nil, 30, "test-secret-key")

	user := &domain.User{
		ID:          "user-123",
		ClerkUserID: "user_123",
		Email:       "test@example.com",
		IsDeleted:   true, // Already deleted
	}

	mockRepo.On("GetUserByID", ctx, user.ID).Return(user, nil)

	err := service.ExecuteDeletion(ctx, user.ID)

	assert.NoError(t, err, "Should not error on already deleted user")
	mockRepo.AssertNotCalled(t, "DeleteUserCascade", mock.Anything, mock.Anything)
	mockClerk.AssertNotCalled(t, "DeleteUser", mock.Anything, mock.Anything)
}

// TestExecuteDeletion_HandlesClerkError tests graceful Clerk error handling
func TestExecuteDeletion_HandlesClerkError(t *testing.T) {
	ctx := context.Background()
	mockRepo := new(MockProfileRepositoryForDeletion)
	mockEmail := new(MockEmailService)
	mockClerk := new(MockClerkService)
	mockAudit := new(MockAuditLogger)

	service := NewDeletionService(mockRepo, mockEmail, mockClerk, mockAudit, nil, 30, "test-secret-key")

	user := &domain.User{
		ID:          "user-123",
		ClerkUserID: "user_123",
		Email:       "test@example.com",
		IsDeleted:   false,
	}

	mockRepo.On("GetUserByID", ctx, user.ID).Return(user, nil)
	mockRepo.On("ClaimForDeletion", ctx, user.ID).Return(true, nil)
	service.SetWorkClaimsRepo(newAllowingClaimsRepo())
	mockRepo.On("DeleteUserCascade", ctx, user.ID).Return(nil)
	mockClerk.On("DeleteUser", ctx, user.ClerkUserID).Return(errors.New("Clerk API error"))
	mockAudit.On("Log", ctx, "user.deleted", "user", user.ID).Return(nil)

	err := service.ExecuteDeletion(ctx, user.ID)

	// Should still succeed even if Clerk fails (our data is deleted)
	assert.NoError(t, err)
	mockRepo.AssertCalled(t, "DeleteUserCascade", ctx, user.ID)
	mockAudit.AssertCalled(t, "Log", ctx, "user.deleted", "user", user.ID)
}

// MockPlaidItemCleanupService is a mock for PlaidItemCleanupService
type MockPlaidItemCleanupService struct {
	mock.Mock
}

func (m *MockPlaidItemCleanupService) RemoveAllUserItems(ctx context.Context, userID string) error {
	args := m.Called(ctx, userID)
	return args.Error(0)
}

func (m *MockPlaidItemCleanupService) RemoveItem(ctx context.Context, linkedItemID string) error {
	args := m.Called(ctx, linkedItemID)
	return args.Error(0)
}

// Issue #679, #680: DisconnectAllUserItems for trial expiration
func (m *MockPlaidItemCleanupService) DisconnectAllUserItems(ctx context.Context, userID string) error {
	args := m.Called(ctx, userID)
	return args.Error(0)
}

// TestExecuteDeletion_CallsPlaidCleanupBeforeDBDelete verifies that
// ExecuteDeletion calls PlaidCleanupService.RemoveAllUserItems before DeleteUserCascade
// Issue #668: Plaid Item Management for Cost Optimization
func TestExecuteDeletion_CallsPlaidCleanupBeforeDBDelete(t *testing.T) {
	ctx := context.Background()
	mockRepo := new(MockProfileRepositoryForDeletion)
	mockEmail := new(MockEmailService)
	mockClerk := new(MockClerkService)
	mockAudit := new(MockAuditLogger)
	mockPlaidCleanup := new(MockPlaidItemCleanupService)

	service := NewDeletionService(mockRepo, mockEmail, mockClerk, mockAudit, mockPlaidCleanup, 30, "test-secret-key")

	user := &domain.User{
		ID:          "user-123",
		ClerkUserID: "user_123",
		Email:       "test@example.com",
		IsDeleted:   false,
	}

	// Set up call order tracking
	callOrder := []string{}

	mockRepo.On("GetUserByID", ctx, user.ID).Return(user, nil)
	mockRepo.On("ClaimForDeletion", ctx, user.ID).Return(true, nil)
	service.SetWorkClaimsRepo(newAllowingClaimsRepo())
	mockPlaidCleanup.On("RemoveAllUserItems", ctx, user.ID).Return(nil).Run(func(args mock.Arguments) {
		callOrder = append(callOrder, "RemoveAllUserItems")
	})
	mockRepo.On("DeleteUserCascade", ctx, user.ID).Return(nil).Run(func(args mock.Arguments) {
		callOrder = append(callOrder, "DeleteUserCascade")
	})
	mockClerk.On("DeleteUser", ctx, user.ClerkUserID).Return(nil)
	mockAudit.On("Log", ctx, "user.deleted", "user", user.ID).Return(nil)

	err := service.ExecuteDeletion(ctx, user.ID)

	assert.NoError(t, err)
	mockPlaidCleanup.AssertCalled(t, "RemoveAllUserItems", ctx, user.ID)
	mockRepo.AssertCalled(t, "DeleteUserCascade", ctx, user.ID)

	// Verify Plaid cleanup happens BEFORE database deletion
	assert.Equal(t, []string{"RemoveAllUserItems", "DeleteUserCascade"}, callOrder,
		"PlaidCleanup should be called before DeleteUserCascade")
}

// TestExecuteDeletion_PlaidCleanupFails_AbortsDeletion is a corrected test (Issue #1413 Critical
// finding C2, round 3 remediation).
//
// This test previously asserted the OPPOSITE — named "...SucceedsEvenIfPlaidCleanupFails" — that
// ExecuteDeletion continues to cascade-delete even when Plaid cleanup fails, framed as "graceful
// degradation." That was the C2 bug, not a documented rule: DeleteUserCascade clears the user's
// Plaid references, so proceeding after a failed cleanup destroys the only local record of the
// Plaid item that still needs removing (and is still billing) — nothing would ever retry it.
// Aborting and releasing the claim is the deliberate fix: the next run retries the whole
// sequence while the references are still present to retry against.
func TestExecuteDeletion_PlaidCleanupFails_AbortsDeletion(t *testing.T) {
	ctx := context.Background()
	mockRepo := new(MockProfileRepositoryForDeletion)
	mockEmail := new(MockEmailService)
	mockClerk := new(MockClerkService)
	mockAudit := new(MockAuditLogger)
	mockPlaidCleanup := new(MockPlaidItemCleanupService)

	service := NewDeletionService(mockRepo, mockEmail, mockClerk, mockAudit, mockPlaidCleanup, 30, "test-secret-key")

	user := &domain.User{
		ID:          "user-123",
		ClerkUserID: "user_123",
		Email:       "test@example.com",
		IsDeleted:   false,
	}

	mockClaims := new(MockWorkClaimsRepository)
	mockClaims.On("Claim", ctx, repository.ResourceTypeUser, user.ID, repository.ActionAccountDeletion, accountDeletionClaimLease).
		Return(testDeletionClaimToken, true, nil)
	mockClaims.On("Release", ctx, repository.ResourceTypeUser, user.ID, repository.ActionAccountDeletion, testDeletionClaimToken, mock.AnythingOfType("string")).
		Return(true, nil)
	service.SetWorkClaimsRepo(mockClaims)

	mockRepo.On("GetUserByID", ctx, user.ID).Return(user, nil)
	mockRepo.On("ClaimForDeletion", ctx, user.ID).Return(true, nil)
	plaidErr := errors.New("plaid API unavailable")
	mockPlaidCleanup.On("RemoveAllUserItems", ctx, user.ID).Return(plaidErr)
	// DeleteUserCascade, Clerk deletion, and the audit log are deliberately left unstubbed —
	// an unstubbed testify mock call panics, a stronger proof than AssertNotCalled alone that
	// the abort actually happens before any of them.

	err := service.ExecuteDeletion(ctx, user.ID)

	require.Error(t, err, "a failed Plaid cleanup must abort the deletion, not be swallowed")
	assert.ErrorIs(t, err, plaidErr)
	mockRepo.AssertNotCalled(t, "DeleteUserCascade", mock.Anything, mock.Anything)
	mockClerk.AssertNotCalled(t, "DeleteUser", mock.Anything, mock.Anything)
	mockClaims.AssertCalled(t, "Release", ctx, repository.ResourceTypeUser, user.ID, repository.ActionAccountDeletion, testDeletionClaimToken, mock.AnythingOfType("string"))
}

// TestSetPlaidCleanupService verifies that SetPlaidCleanupService wires up the service
// Issue #668: Setter for late binding of Plaid cleanup service
func TestSetPlaidCleanupService(t *testing.T) {
	ctx := context.Background()
	mockRepo := new(MockProfileRepositoryForDeletion)
	mockEmail := new(MockEmailService)
	mockClerk := new(MockClerkService)
	mockAudit := new(MockAuditLogger)
	mockPlaidCleanup := new(MockPlaidItemCleanupService)

	// Create service with nil plaidCleanupSvc
	service := NewDeletionService(mockRepo, mockEmail, mockClerk, mockAudit, nil, 30, "test-secret-key")

	// Set the cleanup service via setter
	service.SetPlaidCleanupService(mockPlaidCleanup)

	user := &domain.User{
		ID:          "user-123",
		ClerkUserID: "user_123",
		Email:       "test@example.com",
		IsDeleted:   false,
	}

	mockRepo.On("GetUserByID", ctx, user.ID).Return(user, nil)
	mockRepo.On("ClaimForDeletion", ctx, user.ID).Return(true, nil)
	service.SetWorkClaimsRepo(newAllowingClaimsRepo())
	mockPlaidCleanup.On("RemoveAllUserItems", ctx, user.ID).Return(nil)
	mockRepo.On("DeleteUserCascade", ctx, user.ID).Return(nil)
	mockClerk.On("DeleteUser", ctx, user.ClerkUserID).Return(nil)
	mockAudit.On("Log", ctx, "user.deleted", "user", user.ID).Return(nil)

	err := service.ExecuteDeletion(ctx, user.ID)

	assert.NoError(t, err)
	// Verify the cleanup service was called (proving the setter worked)
	mockPlaidCleanup.AssertCalled(t, "RemoveAllUserItems", ctx, user.ID)
}

// MockPaddleDeletionCleaner is a mock for PaddleDeletionCleaner.
// Only 2 methods needed for DeletionService — narrowed interface. Issue #1006.
type MockPaddleDeletionCleaner struct {
	mock.Mock
}

func (m *MockPaddleDeletionCleaner) CancelSubscriptionImmediately(ctx context.Context, subscriptionID string) error {
	return m.Called(ctx, subscriptionID).Error(0)
}

func (m *MockPaddleDeletionCleaner) ArchiveAndAnonymizeCustomer(ctx context.Context, customerID, anonymizedEmail string) error {
	return m.Called(ctx, customerID, anonymizedEmail).Error(0)
}

// buildUserWithPaddle creates a test user with Paddle IDs set.
// Returns user, subscriptionID, customerID for convenience.
func buildUserWithPaddle(subscriptionStatus string) (*domain.User, string, string) {
	paddleSubID := "sub_01abc123"
	paddleCustID := "ctm_01abc123"
	return &domain.User{
		ID:                   "user-123",
		ClerkUserID:          "user_123",
		Email:                "test@example.com",
		IsDeleted:            false,
		SubscriptionStatus:   subscriptionStatus,
		PaddleSubscriptionID: &paddleSubID,
		PaddleCustomerID:     &paddleCustID,
	}, paddleSubID, paddleCustID
}

const expectedAnonymizedEmail = "deleted_user-123@deleted.local"

// TestExecuteDeletion_ActiveSubscription_CancelsImmediately verifies that an active
// subscription is cancelled immediately and customer is archived. Issue #1006.
func TestExecuteDeletion_ActiveSubscription_CancelsImmediately(t *testing.T) {
	ctx := context.Background()
	mockRepo := new(MockProfileRepositoryForDeletion)
	mockEmail := new(MockEmailService)
	mockClerk := new(MockClerkService)
	mockAudit := new(MockAuditLogger)
	mockPaddle := new(MockPaddleDeletionCleaner)

	service := NewDeletionService(mockRepo, mockEmail, mockClerk, mockAudit, nil, 30, "test-secret-key")
	service.SetPaddleCleanupService(mockPaddle)

	user, paddleSubID, paddleCustID := buildUserWithPaddle("active")
	mockRepo.On("GetUserByID", ctx, user.ID).Return(user, nil)
	mockRepo.On("ClaimForDeletion", ctx, user.ID).Return(true, nil)
	service.SetWorkClaimsRepo(newAllowingClaimsRepo())
	mockPaddle.On("CancelSubscriptionImmediately", ctx, paddleSubID).Return(nil)
	mockPaddle.On("ArchiveAndAnonymizeCustomer", ctx, paddleCustID, expectedAnonymizedEmail).Return(nil)
	mockRepo.On("DeleteUserCascade", ctx, user.ID).Return(nil)
	mockClerk.On("DeleteUser", ctx, user.ClerkUserID).Return(nil)
	mockAudit.On("Log", ctx, "user.deleted", "user", user.ID).Return(nil)

	err := service.ExecuteDeletion(ctx, user.ID)
	assert.NoError(t, err)
	mockPaddle.AssertCalled(t, "CancelSubscriptionImmediately", ctx, paddleSubID)
	mockPaddle.AssertCalled(t, "ArchiveAndAnonymizeCustomer", ctx, paddleCustID, expectedAnonymizedEmail)
}

// TestExecuteDeletion_TrialSubscription_CancelsImmediately verifies trial subscriptions
// are cancelled immediately. Issue #1006.
func TestExecuteDeletion_TrialSubscription_CancelsImmediately(t *testing.T) {
	ctx := context.Background()
	mockRepo := new(MockProfileRepositoryForDeletion)
	mockEmail := new(MockEmailService)
	mockClerk := new(MockClerkService)
	mockAudit := new(MockAuditLogger)
	mockPaddle := new(MockPaddleDeletionCleaner)

	service := NewDeletionService(mockRepo, mockEmail, mockClerk, mockAudit, nil, 30, "test-secret-key")
	service.SetPaddleCleanupService(mockPaddle)

	user, paddleSubID, paddleCustID := buildUserWithPaddle("trial")
	mockRepo.On("GetUserByID", ctx, user.ID).Return(user, nil)
	mockRepo.On("ClaimForDeletion", ctx, user.ID).Return(true, nil)
	service.SetWorkClaimsRepo(newAllowingClaimsRepo())
	mockPaddle.On("CancelSubscriptionImmediately", ctx, paddleSubID).Return(nil)
	mockPaddle.On("ArchiveAndAnonymizeCustomer", ctx, paddleCustID, expectedAnonymizedEmail).Return(nil)
	mockRepo.On("DeleteUserCascade", ctx, user.ID).Return(nil)
	mockClerk.On("DeleteUser", ctx, user.ClerkUserID).Return(nil)
	mockAudit.On("Log", ctx, "user.deleted", "user", user.ID).Return(nil)

	err := service.ExecuteDeletion(ctx, user.ID)
	assert.NoError(t, err)
	mockPaddle.AssertCalled(t, "CancelSubscriptionImmediately", ctx, paddleSubID)
}

// TestExecuteDeletion_TrialingSubscription_CancelsImmediately verifies Paddle-trialing
// subscriptions are cancelled immediately. Issue #1006.
func TestExecuteDeletion_TrialingSubscription_CancelsImmediately(t *testing.T) {
	ctx := context.Background()
	mockRepo := new(MockProfileRepositoryForDeletion)
	mockEmail := new(MockEmailService)
	mockClerk := new(MockClerkService)
	mockAudit := new(MockAuditLogger)
	mockPaddle := new(MockPaddleDeletionCleaner)

	service := NewDeletionService(mockRepo, mockEmail, mockClerk, mockAudit, nil, 30, "test-secret-key")
	service.SetPaddleCleanupService(mockPaddle)

	user, paddleSubID, paddleCustID := buildUserWithPaddle("trialing")
	mockRepo.On("GetUserByID", ctx, user.ID).Return(user, nil)
	mockRepo.On("ClaimForDeletion", ctx, user.ID).Return(true, nil)
	service.SetWorkClaimsRepo(newAllowingClaimsRepo())
	mockPaddle.On("CancelSubscriptionImmediately", ctx, paddleSubID).Return(nil)
	mockPaddle.On("ArchiveAndAnonymizeCustomer", ctx, paddleCustID, expectedAnonymizedEmail).Return(nil)
	mockRepo.On("DeleteUserCascade", ctx, user.ID).Return(nil)
	mockClerk.On("DeleteUser", ctx, user.ClerkUserID).Return(nil)
	mockAudit.On("Log", ctx, "user.deleted", "user", user.ID).Return(nil)

	err := service.ExecuteDeletion(ctx, user.ID)
	assert.NoError(t, err)
	mockPaddle.AssertCalled(t, "CancelSubscriptionImmediately", ctx, paddleSubID)
}

// TestExecuteDeletion_PastDueSubscription_CancelsImmediately verifies past_due subscriptions
// are cancelled immediately (billing is still active). Issue #1006.
func TestExecuteDeletion_PastDueSubscription_CancelsImmediately(t *testing.T) {
	ctx := context.Background()
	mockRepo := new(MockProfileRepositoryForDeletion)
	mockEmail := new(MockEmailService)
	mockClerk := new(MockClerkService)
	mockAudit := new(MockAuditLogger)
	mockPaddle := new(MockPaddleDeletionCleaner)

	service := NewDeletionService(mockRepo, mockEmail, mockClerk, mockAudit, nil, 30, "test-secret-key")
	service.SetPaddleCleanupService(mockPaddle)

	user, paddleSubID, paddleCustID := buildUserWithPaddle("past_due")
	mockRepo.On("GetUserByID", ctx, user.ID).Return(user, nil)
	mockRepo.On("ClaimForDeletion", ctx, user.ID).Return(true, nil)
	service.SetWorkClaimsRepo(newAllowingClaimsRepo())
	mockPaddle.On("CancelSubscriptionImmediately", ctx, paddleSubID).Return(nil)
	mockPaddle.On("ArchiveAndAnonymizeCustomer", ctx, paddleCustID, expectedAnonymizedEmail).Return(nil)
	mockRepo.On("DeleteUserCascade", ctx, user.ID).Return(nil)
	mockClerk.On("DeleteUser", ctx, user.ClerkUserID).Return(nil)
	mockAudit.On("Log", ctx, "user.deleted", "user", user.ID).Return(nil)

	err := service.ExecuteDeletion(ctx, user.ID)
	assert.NoError(t, err)
	mockPaddle.AssertCalled(t, "CancelSubscriptionImmediately", ctx, paddleSubID)
}

// TestExecuteDeletion_CancelledSubscription_DoesNotCancelAgain verifies that a cancelled
// subscription does NOT trigger another cancel call, but customer is still archived. Issue #1006.
func TestExecuteDeletion_CancelledSubscription_DoesNotCancelAgain(t *testing.T) {
	ctx := context.Background()
	mockRepo := new(MockProfileRepositoryForDeletion)
	mockEmail := new(MockEmailService)
	mockClerk := new(MockClerkService)
	mockAudit := new(MockAuditLogger)
	mockPaddle := new(MockPaddleDeletionCleaner)

	service := NewDeletionService(mockRepo, mockEmail, mockClerk, mockAudit, nil, 30, "test-secret-key")
	service.SetPaddleCleanupService(mockPaddle)

	user, _, paddleCustID := buildUserWithPaddle("cancelled")
	mockRepo.On("GetUserByID", ctx, user.ID).Return(user, nil)
	mockRepo.On("ClaimForDeletion", ctx, user.ID).Return(true, nil)
	service.SetWorkClaimsRepo(newAllowingClaimsRepo())
	// CancelSubscriptionImmediately must NOT be called
	mockPaddle.On("ArchiveAndAnonymizeCustomer", ctx, paddleCustID, expectedAnonymizedEmail).Return(nil)
	mockRepo.On("DeleteUserCascade", ctx, user.ID).Return(nil)
	mockClerk.On("DeleteUser", ctx, user.ClerkUserID).Return(nil)
	mockAudit.On("Log", ctx, "user.deleted", "user", user.ID).Return(nil)

	err := service.ExecuteDeletion(ctx, user.ID)
	assert.NoError(t, err)
	mockPaddle.AssertNotCalled(t, "CancelSubscriptionImmediately", mock.Anything, mock.Anything)
	mockPaddle.AssertCalled(t, "ArchiveAndAnonymizeCustomer", ctx, paddleCustID, expectedAnonymizedEmail)
}

// TestExecuteDeletion_InactiveSubscription_DoesNotCancel verifies that an inactive
// subscription (no subscription ID) does not trigger a cancel call. Issue #1006.
func TestExecuteDeletion_InactiveSubscription_DoesNotCancel(t *testing.T) {
	ctx := context.Background()
	mockRepo := new(MockProfileRepositoryForDeletion)
	mockEmail := new(MockEmailService)
	mockClerk := new(MockClerkService)
	mockAudit := new(MockAuditLogger)
	mockPaddle := new(MockPaddleDeletionCleaner)

	service := NewDeletionService(mockRepo, mockEmail, mockClerk, mockAudit, nil, 30, "test-secret-key")
	service.SetPaddleCleanupService(mockPaddle)

	// inactive: no subscription ID (only customer ID)
	paddleCustID := "ctm_01abc123"
	user := &domain.User{
		ID:                   "user-123",
		ClerkUserID:          "user_123",
		Email:                "test@example.com",
		IsDeleted:            false,
		SubscriptionStatus:   "inactive",
		PaddleSubscriptionID: nil,
		PaddleCustomerID:     &paddleCustID,
	}
	mockRepo.On("GetUserByID", ctx, user.ID).Return(user, nil)
	mockRepo.On("ClaimForDeletion", ctx, user.ID).Return(true, nil)
	service.SetWorkClaimsRepo(newAllowingClaimsRepo())
	mockPaddle.On("ArchiveAndAnonymizeCustomer", ctx, paddleCustID, expectedAnonymizedEmail).Return(nil)
	mockRepo.On("DeleteUserCascade", ctx, user.ID).Return(nil)
	mockClerk.On("DeleteUser", ctx, user.ClerkUserID).Return(nil)
	mockAudit.On("Log", ctx, "user.deleted", "user", user.ID).Return(nil)

	err := service.ExecuteDeletion(ctx, user.ID)
	assert.NoError(t, err)
	mockPaddle.AssertNotCalled(t, "CancelSubscriptionImmediately", mock.Anything, mock.Anything)
}

// TestExecuteDeletion_NoPaddleCustomerID_SkipsArchive verifies that when no customer ID
// is set, ArchiveAndAnonymizeCustomer is not called. Issue #1006.
func TestExecuteDeletion_NoPaddleCustomerID_SkipsArchive(t *testing.T) {
	ctx := context.Background()
	mockRepo := new(MockProfileRepositoryForDeletion)
	mockEmail := new(MockEmailService)
	mockClerk := new(MockClerkService)
	mockAudit := new(MockAuditLogger)
	mockPaddle := new(MockPaddleDeletionCleaner)

	service := NewDeletionService(mockRepo, mockEmail, mockClerk, mockAudit, nil, 30, "test-secret-key")
	service.SetPaddleCleanupService(mockPaddle)

	// No customer ID set — user never completed checkout
	user := &domain.User{
		ID:                   "user-123",
		ClerkUserID:          "user_123",
		Email:                "test@example.com",
		IsDeleted:            false,
		SubscriptionStatus:   "trial",
		PaddleSubscriptionID: nil,
		PaddleCustomerID:     nil,
	}
	mockRepo.On("GetUserByID", ctx, user.ID).Return(user, nil)
	mockRepo.On("ClaimForDeletion", ctx, user.ID).Return(true, nil)
	service.SetWorkClaimsRepo(newAllowingClaimsRepo())
	mockRepo.On("DeleteUserCascade", ctx, user.ID).Return(nil)
	mockClerk.On("DeleteUser", ctx, user.ClerkUserID).Return(nil)
	mockAudit.On("Log", ctx, "user.deleted", "user", user.ID).Return(nil)

	err := service.ExecuteDeletion(ctx, user.ID)
	assert.NoError(t, err)
	mockPaddle.AssertNotCalled(t, "ArchiveAndAnonymizeCustomer", mock.Anything, mock.Anything, mock.Anything)
}

// TestExecuteDeletion_PaddleCancelFails_AbortsDeletion is a corrected test (Issue #1413 Critical
// finding C2, round 3 remediation).
//
// This test previously asserted the OPPOSITE — named "...DeletionContinues" — that a Paddle
// cancel failure is "non-critical" and the archive step and cascade still run. That was the C2
// bug: DeleteUserCascade clears paddle_subscription_id/paddle_customer_id, so proceeding after a
// failed cancel destroys the only local record of the subscription that still needs cancelling —
// nothing would ever retry it, and the user keeps getting billed. Aborting on the FIRST cleanup
// failure (before ever attempting the archive step) and releasing the claim is the fix.
func TestExecuteDeletion_PaddleCancelFails_AbortsDeletion(t *testing.T) {
	ctx := context.Background()
	mockRepo := new(MockProfileRepositoryForDeletion)
	mockEmail := new(MockEmailService)
	mockClerk := new(MockClerkService)
	mockAudit := new(MockAuditLogger)
	mockPaddle := new(MockPaddleDeletionCleaner)

	service := NewDeletionService(mockRepo, mockEmail, mockClerk, mockAudit, nil, 30, "test-secret-key")
	service.SetPaddleCleanupService(mockPaddle)

	user, paddleSubID, _ := buildUserWithPaddle("active")

	mockClaims := new(MockWorkClaimsRepository)
	mockClaims.On("Claim", ctx, repository.ResourceTypeUser, user.ID, repository.ActionAccountDeletion, accountDeletionClaimLease).
		Return(testDeletionClaimToken, true, nil)
	mockClaims.On("Release", ctx, repository.ResourceTypeUser, user.ID, repository.ActionAccountDeletion, testDeletionClaimToken, mock.AnythingOfType("string")).
		Return(true, nil)
	service.SetWorkClaimsRepo(mockClaims)

	mockRepo.On("GetUserByID", ctx, user.ID).Return(user, nil)
	mockRepo.On("ClaimForDeletion", ctx, user.ID).Return(true, nil)
	paddleErr := errors.New("Paddle API timeout")
	mockPaddle.On("CancelSubscriptionImmediately", ctx, paddleSubID).Return(paddleErr)
	// ArchiveAndAnonymizeCustomer, DeleteUserCascade, and Clerk deletion are deliberately left
	// unstubbed — an unstubbed testify mock call panics, proving the abort happens before any
	// of them, not just before the cascade.

	err := service.ExecuteDeletion(ctx, user.ID)

	require.Error(t, err, "a failed Paddle cancel must abort the deletion, not be swallowed")
	assert.ErrorIs(t, err, paddleErr)
	mockPaddle.AssertNotCalled(t, "ArchiveAndAnonymizeCustomer", mock.Anything, mock.Anything, mock.Anything)
	mockRepo.AssertNotCalled(t, "DeleteUserCascade", mock.Anything, mock.Anything)
	mockClaims.AssertCalled(t, "Release", ctx, repository.ResourceTypeUser, user.ID, repository.ActionAccountDeletion, testDeletionClaimToken, mock.AnythingOfType("string"))
}

// TestExecuteDeletion_PaddleArchiveFails_AbortsDeletion is the archive-step sibling of the
// cancel-failure test above — same C2 correction, same rationale.
func TestExecuteDeletion_PaddleArchiveFails_AbortsDeletion(t *testing.T) {
	ctx := context.Background()
	mockRepo := new(MockProfileRepositoryForDeletion)
	mockEmail := new(MockEmailService)
	mockClerk := new(MockClerkService)
	mockAudit := new(MockAuditLogger)
	mockPaddle := new(MockPaddleDeletionCleaner)

	service := NewDeletionService(mockRepo, mockEmail, mockClerk, mockAudit, nil, 30, "test-secret-key")
	service.SetPaddleCleanupService(mockPaddle)

	user, paddleSubID, paddleCustID := buildUserWithPaddle("active")

	mockClaims := new(MockWorkClaimsRepository)
	mockClaims.On("Claim", ctx, repository.ResourceTypeUser, user.ID, repository.ActionAccountDeletion, accountDeletionClaimLease).
		Return(testDeletionClaimToken, true, nil)
	mockClaims.On("Release", ctx, repository.ResourceTypeUser, user.ID, repository.ActionAccountDeletion, testDeletionClaimToken, mock.AnythingOfType("string")).
		Return(true, nil)
	service.SetWorkClaimsRepo(mockClaims)

	mockRepo.On("GetUserByID", ctx, user.ID).Return(user, nil)
	mockRepo.On("ClaimForDeletion", ctx, user.ID).Return(true, nil)
	mockPaddle.On("CancelSubscriptionImmediately", ctx, paddleSubID).Return(nil)
	archiveErr := errors.New("Paddle API timeout")
	mockPaddle.On("ArchiveAndAnonymizeCustomer", ctx, paddleCustID, expectedAnonymizedEmail).Return(archiveErr)
	// DeleteUserCascade and Clerk deletion are deliberately left unstubbed.

	err := service.ExecuteDeletion(ctx, user.ID)

	require.Error(t, err, "a failed Paddle archive must abort the deletion, not be swallowed")
	assert.ErrorIs(t, err, archiveErr)
	mockRepo.AssertNotCalled(t, "DeleteUserCascade", mock.Anything, mock.Anything)
	mockClaims.AssertCalled(t, "Release", ctx, repository.ResourceTypeUser, user.ID, repository.ActionAccountDeletion, testDeletionClaimToken, mock.AnythingOfType("string"))
}

// TestSetPaddleCleanupService_WiresCorrectly verifies that SetPaddleCleanupService wires
// the mock so it's invoked during ExecuteDeletion. Issue #1006.
func TestSetPaddleCleanupService_WiresCorrectly(t *testing.T) {
	ctx := context.Background()
	mockRepo := new(MockProfileRepositoryForDeletion)
	mockEmail := new(MockEmailService)
	mockClerk := new(MockClerkService)
	mockAudit := new(MockAuditLogger)
	mockPaddle := new(MockPaddleDeletionCleaner)

	// Create with nil paddleCleanupSvc, wire via setter
	service := NewDeletionService(mockRepo, mockEmail, mockClerk, mockAudit, nil, 30, "test-secret-key")
	service.SetPaddleCleanupService(mockPaddle)

	user, paddleSubID, paddleCustID := buildUserWithPaddle("active")
	mockRepo.On("GetUserByID", ctx, user.ID).Return(user, nil)
	mockRepo.On("ClaimForDeletion", ctx, user.ID).Return(true, nil)
	service.SetWorkClaimsRepo(newAllowingClaimsRepo())
	mockPaddle.On("CancelSubscriptionImmediately", ctx, paddleSubID).Return(nil)
	mockPaddle.On("ArchiveAndAnonymizeCustomer", ctx, paddleCustID, expectedAnonymizedEmail).Return(nil)
	mockRepo.On("DeleteUserCascade", ctx, user.ID).Return(nil)
	mockClerk.On("DeleteUser", ctx, user.ClerkUserID).Return(nil)
	mockAudit.On("Log", ctx, "user.deleted", "user", user.ID).Return(nil)

	err := service.ExecuteDeletion(ctx, user.ID)
	assert.NoError(t, err)
	// Both mock methods called proves setter worked
	mockPaddle.AssertExpectations(t)
}

// TestExecuteDeletion_PaddleRunsBeforePlaidAndDB verifies the execution order:
// Paddle cleanup → Plaid cleanup → DB cascade. Issue #1006.
func TestExecuteDeletion_PaddleRunsBeforePlaidAndDB(t *testing.T) {
	ctx := context.Background()
	mockRepo := new(MockProfileRepositoryForDeletion)
	mockEmail := new(MockEmailService)
	mockClerk := new(MockClerkService)
	mockAudit := new(MockAuditLogger)
	mockPaddle := new(MockPaddleDeletionCleaner)
	mockPlaid := new(MockPlaidItemCleanupService)

	service := NewDeletionService(mockRepo, mockEmail, mockClerk, mockAudit, mockPlaid, 30, "test-secret-key")
	service.SetPaddleCleanupService(mockPaddle)

	user, paddleSubID, paddleCustID := buildUserWithPaddle("active")
	callOrder := []string{}

	mockRepo.On("GetUserByID", ctx, user.ID).Return(user, nil)
	mockRepo.On("ClaimForDeletion", ctx, user.ID).Return(true, nil)
	service.SetWorkClaimsRepo(newAllowingClaimsRepo())
	mockPaddle.On("CancelSubscriptionImmediately", ctx, paddleSubID).Return(nil).Run(func(args mock.Arguments) {
		callOrder = append(callOrder, "CancelSubscription")
	})
	mockPaddle.On("ArchiveAndAnonymizeCustomer", ctx, paddleCustID, expectedAnonymizedEmail).Return(nil).Run(func(args mock.Arguments) {
		callOrder = append(callOrder, "ArchiveCustomer")
	})
	mockPlaid.On("RemoveAllUserItems", ctx, user.ID).Return(nil).Run(func(args mock.Arguments) {
		callOrder = append(callOrder, "RemoveAllUserItems")
	})
	mockRepo.On("DeleteUserCascade", ctx, user.ID).Return(nil).Run(func(args mock.Arguments) {
		callOrder = append(callOrder, "DeleteUserCascade")
	})
	mockClerk.On("DeleteUser", ctx, user.ClerkUserID).Return(nil)
	mockAudit.On("Log", ctx, "user.deleted", "user", user.ID).Return(nil)

	err := service.ExecuteDeletion(ctx, user.ID)
	assert.NoError(t, err)
	assert.Equal(t,
		[]string{"CancelSubscription", "ArchiveCustomer", "RemoveAllUserItems", "DeleteUserCascade"},
		callOrder,
		"Execution order must be: Paddle cancel, Paddle archive, Plaid remove, DB cascade",
	)
}

// mockAnalyticsForDeletion is a minimal phclient.Analytics stub for setter tests.
type mockAnalyticsForDeletion struct{}

func (m *mockAnalyticsForDeletion) Capture(distinctID, event string, properties map[string]interface{}) {
}
func (m *mockAnalyticsForDeletion) Identify(distinctID string, properties map[string]interface{}) {}
func (m *mockAnalyticsForDeletion) Close()                                                        {}

var _ phclient.Analytics = (*mockAnalyticsForDeletion)(nil)

// ─── Household owner-deletion guard (#1367 — Phase 3) ────────────────────────

type mockHouseholdReaderForDeletion struct{ mock.Mock }

func (m *mockHouseholdReaderForDeletion) GetHouseholdForUser(ctx context.Context, userID string) (*domain.Household, bool, bool, error) {
	args := m.Called(ctx, userID)
	var h *domain.Household
	if args.Get(0) != nil {
		h = args.Get(0).(*domain.Household)
	}
	return h, args.Bool(1), args.Bool(2), args.Error(3)
}
func (m *mockHouseholdReaderForDeletion) CountActiveMembers(ctx context.Context, householdID string) (int, error) {
	args := m.Called(ctx, householdID)
	return args.Int(0), args.Error(1)
}

// LockHouseholdForUpdate is not exercised by the non-transactional-path tests below
// (only the SetDB-wired transactional path uses a real, tx-scoped HouseholdRepository
// for locking); stubbed to satisfy the expanded HouseholdReader interface.
func (m *mockHouseholdReaderForDeletion) LockHouseholdForUpdate(ctx context.Context, householdID string) (string, error) {
	args := m.Called(ctx, householdID)
	return args.String(0), args.Error(1)
}

type mockHouseholdInviteRevokerForDeletion struct{ mock.Mock }

func (m *mockHouseholdInviteRevokerForDeletion) RevokeAllPendingForHousehold(ctx context.Context, householdID string) error {
	args := m.Called(ctx, householdID)
	return args.Error(0)
}

func TestRequestDeletion_OwnerWithActiveMembers_Blocked(t *testing.T) {
	ctx := context.Background()
	mockRepo := new(MockProfileRepositoryForDeletion)
	mockEmail := new(MockEmailService)
	mockClerk := new(MockClerkService)
	mockAudit := new(MockAuditLogger)
	hhReader := &mockHouseholdReaderForDeletion{}

	service := NewDeletionService(mockRepo, mockEmail, mockClerk, mockAudit, nil, 30, "test-secret-key")
	service.SetHouseholdReader(hhReader)

	hhReader.On("GetHouseholdForUser", ctx, "owner-1").Return(&domain.Household{ID: "hh-1"}, true, true, nil)
	hhReader.On("CountActiveMembers", ctx, "hh-1").Return(2, nil)

	_, _, err := service.RequestDeletion(ctx, "owner-1", "owner@example.com")
	require.ErrorIs(t, err, domain.ErrHouseholdHasActiveMembers)
	mockRepo.AssertNotCalled(t, "MarkForDeletion", mock.Anything, mock.Anything, mock.Anything)
}

func TestRequestDeletion_OwnerWithNoOtherMembers_ProceedsAndRevokesInvites(t *testing.T) {
	ctx := context.Background()
	mockRepo := new(MockProfileRepositoryForDeletion)
	mockEmail := new(MockEmailService)
	mockClerk := new(MockClerkService)
	mockAudit := new(MockAuditLogger)
	hhReader := &mockHouseholdReaderForDeletion{}
	inviteRevoker := &mockHouseholdInviteRevokerForDeletion{}

	service := NewDeletionService(mockRepo, mockEmail, mockClerk, mockAudit, nil, 30, "test-secret-key")
	service.SetHouseholdReader(hhReader)
	service.SetHouseholdInviteRevoker(inviteRevoker)

	hhReader.On("GetHouseholdForUser", ctx, "owner-1").Return(&domain.Household{ID: "hh-1"}, true, true, nil)
	hhReader.On("CountActiveMembers", ctx, "hh-1").Return(1, nil) // owner only
	inviteRevoker.On("RevokeAllPendingForHousehold", ctx, "hh-1").Return(nil)
	mockRepo.On("MarkForDeletion", ctx, "owner-1", mock.AnythingOfType("time.Time")).Return(nil)
	mockEmail.On("SendDeletionConfirmation", ctx, "owner@example.com", mock.AnythingOfType("string"), mock.AnythingOfType("time.Time")).Return(nil)
	mockAudit.On("Log", ctx, "user.deletion_requested", "user", "owner-1").Return(nil)

	_, _, err := service.RequestDeletion(ctx, "owner-1", "owner@example.com")
	require.NoError(t, err)
	inviteRevoker.AssertExpectations(t)
	mockRepo.AssertExpectations(t)
}

// ─── Transactional owner-guard (#1367 — Codex review, High finding 4) ────────
//
// When SetDB is wired, the household lock/check/revoke-invites/mark-for-deletion
// sequence runs inside ONE transaction (via fresh tx-scoped repositories, not the
// pool-bound HouseholdReader/HouseholdInviteRevoker), so an in-flight AcceptInvite
// cannot commit a new member between this guard's check and MarkForDeletion.

func TestRequestDeletion_Transactional_OwnerWithActiveMembers_RollsBack(t *testing.T) {
	ctx := context.Background()
	mockRepo := new(MockProfileRepositoryForDeletion)
	mockEmail := new(MockEmailService)
	mockClerk := new(MockClerkService)
	mockAudit := new(MockAuditLogger)
	pool, err := pgxmock.NewPool()
	require.NoError(t, err)
	defer pool.Close()

	service := NewDeletionService(mockRepo, mockEmail, mockClerk, mockAudit, nil, 30, "test-secret-key")
	service.SetHouseholdReader(&mockHouseholdReaderForDeletion{}) // gating flag only; unused once SetDB is wired
	service.SetDB(pool)

	pool.ExpectBegin()
	pool.ExpectQuery("household_members hm").WithArgs("owner-1").
		WillReturnRows(pgxmock.NewRows([]string{"id", "owner_user_id", "name", "created_at", "updated_at", "over_capacity_since", "over_capacity_start_notice_sent_at", "over_capacity_warn3d_sent_at", "role"}).
			AddRow("hh-1", "owner-1", "The Smiths", time.Now(), time.Now(), nil, nil, nil, "owner"))
	pool.ExpectQuery("FOR UPDATE").WithArgs("hh-1").
		WillReturnRows(pgxmock.NewRows([]string{"owner_user_id"}).AddRow("owner-1"))
	pool.ExpectQuery("SELECT COUNT").WithArgs("hh-1").
		WillReturnRows(pgxmock.NewRows([]string{"count"}).AddRow(2))
	pool.ExpectRollback()

	_, _, err = service.RequestDeletion(ctx, "owner-1", "owner@example.com")
	require.ErrorIs(t, err, domain.ErrHouseholdHasActiveMembers)
	require.NoError(t, pool.ExpectationsWereMet())
	mockRepo.AssertNotCalled(t, "MarkForDeletion", mock.Anything, mock.Anything, mock.Anything)
}

func TestRequestDeletion_Transactional_OwnerWithNoOtherMembers_LocksRevokesAndMarks(t *testing.T) {
	ctx := context.Background()
	mockRepo := new(MockProfileRepositoryForDeletion)
	mockEmail := new(MockEmailService)
	mockClerk := new(MockClerkService)
	mockAudit := new(MockAuditLogger)
	pool, err := pgxmock.NewPool()
	require.NoError(t, err)
	defer pool.Close()

	service := NewDeletionService(mockRepo, mockEmail, mockClerk, mockAudit, nil, 30, "test-secret-key")
	service.SetHouseholdReader(&mockHouseholdReaderForDeletion{})
	service.SetDB(pool)

	mockEmail.On("SendDeletionConfirmation", ctx, "owner@example.com", mock.AnythingOfType("string"), mock.AnythingOfType("time.Time")).Return(nil)
	mockAudit.On("Log", ctx, "user.deletion_requested", "user", "owner-1").Return(nil)

	pool.ExpectBegin()
	pool.ExpectQuery("household_members hm").WithArgs("owner-1").
		WillReturnRows(pgxmock.NewRows([]string{"id", "owner_user_id", "name", "created_at", "updated_at", "over_capacity_since", "over_capacity_start_notice_sent_at", "over_capacity_warn3d_sent_at", "role"}).
			AddRow("hh-1", "owner-1", "The Smiths", time.Now(), time.Now(), nil, nil, nil, "owner"))
	pool.ExpectQuery("FOR UPDATE").WithArgs("hh-1").
		WillReturnRows(pgxmock.NewRows([]string{"owner_user_id"}).AddRow("owner-1"))
	pool.ExpectQuery("SELECT COUNT").WithArgs("hh-1").
		WillReturnRows(pgxmock.NewRows([]string{"count"}).AddRow(1))
	pool.ExpectExec("UPDATE household_invites SET status = 'revoked'").WithArgs("hh-1").
		WillReturnResult(pgxmock.NewResult("UPDATE", 1))
	pool.ExpectExec("UPDATE users SET deletion_requested_at").WithArgs(pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), "owner-1").
		WillReturnResult(pgxmock.NewResult("UPDATE", 1))
	pool.ExpectCommit()

	_, _, err = service.RequestDeletion(ctx, "owner-1", "owner@example.com")
	require.NoError(t, err)
	require.NoError(t, pool.ExpectationsWereMet())
}

func TestRequestDeletion_Transactional_NonOwner_JustMarksForDeletion(t *testing.T) {
	ctx := context.Background()
	mockRepo := new(MockProfileRepositoryForDeletion)
	mockEmail := new(MockEmailService)
	mockClerk := new(MockClerkService)
	mockAudit := new(MockAuditLogger)
	pool, err := pgxmock.NewPool()
	require.NoError(t, err)
	defer pool.Close()

	service := NewDeletionService(mockRepo, mockEmail, mockClerk, mockAudit, nil, 30, "test-secret-key")
	service.SetHouseholdReader(&mockHouseholdReaderForDeletion{})
	service.SetDB(pool)

	mockEmail.On("SendDeletionConfirmation", ctx, "member@example.com", mock.AnythingOfType("string"), mock.AnythingOfType("time.Time")).Return(nil)
	mockAudit.On("Log", ctx, "user.deletion_requested", "user", "member-1").Return(nil)

	pool.ExpectBegin()
	pool.ExpectQuery("household_members hm").WithArgs("member-1").
		WillReturnRows(pgxmock.NewRows([]string{"id", "owner_user_id", "name", "created_at", "updated_at", "over_capacity_since", "over_capacity_start_notice_sent_at", "over_capacity_warn3d_sent_at", "role"}).
			AddRow("hh-1", "owner-1", "The Smiths", time.Now(), time.Now(), nil, nil, nil, "member"))
	pool.ExpectExec("UPDATE users SET deletion_requested_at").WithArgs(pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), "member-1").
		WillReturnResult(pgxmock.NewResult("UPDATE", 1))
	pool.ExpectCommit()

	_, _, err = service.RequestDeletion(ctx, "member-1", "member@example.com")
	require.NoError(t, err)
	require.NoError(t, pool.ExpectationsWereMet())
}

func TestRequestDeletion_NonOwner_ProceedsNormally(t *testing.T) {
	ctx := context.Background()
	mockRepo := new(MockProfileRepositoryForDeletion)
	mockEmail := new(MockEmailService)
	mockClerk := new(MockClerkService)
	mockAudit := new(MockAuditLogger)
	hhReader := &mockHouseholdReaderForDeletion{}

	service := NewDeletionService(mockRepo, mockEmail, mockClerk, mockAudit, nil, 30, "test-secret-key")
	service.SetHouseholdReader(hhReader)

	hhReader.On("GetHouseholdForUser", ctx, "member-1").Return(&domain.Household{ID: "hh-1"}, false, true, nil)
	mockRepo.On("MarkForDeletion", ctx, "member-1", mock.AnythingOfType("time.Time")).Return(nil)
	mockEmail.On("SendDeletionConfirmation", ctx, "member@example.com", mock.AnythingOfType("string"), mock.AnythingOfType("time.Time")).Return(nil)
	mockAudit.On("Log", ctx, "user.deletion_requested", "user", "member-1").Return(nil)

	_, _, err := service.RequestDeletion(ctx, "member-1", "member@example.com")
	require.NoError(t, err)
	hhReader.AssertNotCalled(t, "CountActiveMembers", mock.Anything, mock.Anything)
}

func TestDeletionService_SetAnalytics_DoesNotPanic(t *testing.T) {
	repo := new(MockProfileRepositoryForDeletion)
	email := new(MockEmailService)
	clerkSvc := new(MockClerkService)
	auditLog := new(MockAuditLogger)
	plaid := new(MockPlaidItemCleanupService)
	svc := NewDeletionService(repo, email, clerkSvc, auditLog, plaid, 30, "test-secret-key")
	ds := svc.(*deletionService)
	require.NotPanics(t, func() {
		svc.SetAnalytics(&mockAnalyticsForDeletion{})
	}, "SetAnalytics with a valid analytics must not panic")
	assert.NotNil(t, ds.analytics, "analytics field should be set after SetAnalytics(non-nil)")
	require.NotPanics(t, func() {
		svc.SetAnalytics(nil)
	}, "SetAnalytics with nil must not panic")
	assert.Nil(t, ds.analytics, "analytics field should be nil after SetAnalytics(nil)")
}

// --- Issue #1413 Part C: the grant-vs-delete race guard ---
//
// ExecuteDeletion is shared by two callers: the retention cleanup worker (auto-deletion of
// long-inactive accounts — DeletionRequestedAt is always nil there; all three
// GetUsersNeedingRetention30dNotice/GetUsersNeedingRetention7dNotice/GetUsersWithExpiredRetention
// queries filter on deletion_requested_at IS NULL) and the scheduled deletion worker (a
// user-requested GDPR deletion — DeletionRequestedAt is always set there, via
// GetUsersScheduledForDeletion). A ratified product decision: a user's own GDPR deletion
// request beats an admin comp. The guard below is therefore conditional on
// DeletionRequestedAt == nil, never a blanket comp check — a blanket guard would illegally
// strand a comped user who has asked to be deleted.

// compedUser builds a domain.User with an admin manual tier override, for Part C guard tests.
func compedUser(endsAt *time.Time, deletionRequestedAt *time.Time) *domain.User {
	tier := "premium"
	return &domain.User{
		ID:                  "user-123",
		ClerkUserID:         "user_123",
		Email:               "test@example.com",
		IsDeleted:           false,
		ManualTierOverride:  &tier,
		ManualTierEndsAt:    endsAt,
		DeletionRequestedAt: deletionRequestedAt,
	}
}

// TestExecuteDeletion_ActiveComp_NoDeletionRequest_SkipsDeletion verifies the retention-worker
// path: a comped user with no user-initiated deletion request must not be auto-deleted, and
// the skip must be returned as a nil error (a legitimate outcome, not a failure).
func TestExecuteDeletion_ActiveComp_NoDeletionRequest_SkipsDeletion(t *testing.T) {
	ctx := context.Background()
	mockRepo := new(MockProfileRepositoryForDeletion)
	mockEmail := new(MockEmailService)
	mockClerk := new(MockClerkService)
	mockAudit := new(MockAuditLogger)
	mockPlaidCleanup := new(MockPlaidItemCleanupService)

	service := NewDeletionService(mockRepo, mockEmail, mockClerk, mockAudit, mockPlaidCleanup, 30, "test-secret-key")

	user := compedUser(nil, nil) // no expiry, never requested deletion

	mockRepo.On("GetUserByID", ctx, user.ID).Return(user, nil)

	err := service.ExecuteDeletion(ctx, user.ID)

	assert.NoError(t, err, "a skipped auto-deletion is a legitimate outcome, not an error")
	mockPlaidCleanup.AssertNotCalled(t, "RemoveAllUserItems", mock.Anything, mock.Anything)
	mockRepo.AssertNotCalled(t, "DeleteUserCascade", mock.Anything, mock.Anything)
	mockClerk.AssertNotCalled(t, "DeleteUser", mock.Anything, mock.Anything)
}

// TestExecuteDeletion_ActiveComp_WithDeletionRequest_ProceedsAnyway verifies the GDPR case:
// a user's own deletion request must proceed even while an admin comp is active. A blanket
// comp guard would illegally strand this user (Part C requirement).
func TestExecuteDeletion_ActiveComp_WithDeletionRequest_ProceedsAnyway(t *testing.T) {
	ctx := context.Background()
	mockRepo := new(MockProfileRepositoryForDeletion)
	mockEmail := new(MockEmailService)
	mockClerk := new(MockClerkService)
	mockAudit := new(MockAuditLogger)
	mockPlaidCleanup := new(MockPlaidItemCleanupService)

	service := NewDeletionService(mockRepo, mockEmail, mockClerk, mockAudit, mockPlaidCleanup, 30, "test-secret-key")

	requestedAt := time.Now().Add(-24 * time.Hour)
	user := compedUser(nil, &requestedAt) // active comp, but user asked to be deleted

	mockRepo.On("GetUserByID", ctx, user.ID).Return(user, nil)
	mockRepo.On("ClaimForDeletion", ctx, user.ID).Return(true, nil)
	service.SetWorkClaimsRepo(newAllowingClaimsRepo())
	mockPlaidCleanup.On("RemoveAllUserItems", ctx, user.ID).Return(nil)
	mockRepo.On("DeleteUserCascade", ctx, user.ID).Return(nil)
	mockClerk.On("DeleteUser", ctx, user.ClerkUserID).Return(nil)
	mockAudit.On("Log", ctx, "user.deleted", "user", user.ID).Return(nil)

	err := service.ExecuteDeletion(ctx, user.ID)

	assert.NoError(t, err)
	mockRepo.AssertCalled(t, "DeleteUserCascade", ctx, user.ID)
	mockPlaidCleanup.AssertCalled(t, "RemoveAllUserItems", ctx, user.ID)
}

// TestExecuteDeletion_ExpiredComp_NoDeletionRequest_BeltAndBracesDoesNotShortCircuit is a
// corrected test (Issue #1413 Critical finding C1, round 3 remediation).
//
// Previously named "...ProceedsAnyway" and asserting that deletion completes for an expired comp
// with no deletion request. That was accurate under the OLD claimForDeletionPredicate
// (noActiveManualOverridePredicate — "still live right now"), which was itself the C1 bug: a
// lapsed-but-uncleared comp must delay deletion until the daily expiry job clears the column,
// exactly like the retention queries already required (see
// profile_repository.go's noManualOverrideAtAllPredicate). This test's actual, narrower purpose
// was to prove the cheap belt-and-braces short-circuit
// (HasActiveManualOverride() && DeletionRequestedAt == nil) does NOT itself fire for an EXPIRED
// override — i.e. execution reaches the real claim/predicate machinery rather than being wrongly
// (short-circuit-)skipped there too, since HasActiveManualOverride is false for an expired
// override. It does not, and should not, assert what the real predicate then decides — that is
// proven against a live database in
// TestClaimForDeletion_Integration_ExpiredOverride_NoDeletionRequest_NotClaimed
// (profile_repository_claim_for_deletion_integration_test.go). ClaimForDeletion is mocked here to
// return the real, corrected answer (false) rather than the stale `true` this test used to
// assert.
func TestExecuteDeletion_ExpiredComp_NoDeletionRequest_BeltAndBracesDoesNotShortCircuit(t *testing.T) {
	ctx := context.Background()
	mockRepo := new(MockProfileRepositoryForDeletion)
	mockEmail := new(MockEmailService)
	mockClerk := new(MockClerkService)
	mockAudit := new(MockAuditLogger)
	mockPlaidCleanup := new(MockPlaidItemCleanupService)

	service := NewDeletionService(mockRepo, mockEmail, mockClerk, mockAudit, mockPlaidCleanup, 30, "test-secret-key")

	expired := time.Now().Add(-1 * time.Hour)
	user := compedUser(&expired, nil)

	mockClaims := new(MockWorkClaimsRepository)
	mockClaims.On("Claim", ctx, repository.ResourceTypeUser, user.ID, repository.ActionAccountDeletion, accountDeletionClaimLease).
		Return(testDeletionClaimToken, true, nil)
	mockClaims.On("Release", ctx, repository.ResourceTypeUser, user.ID, repository.ActionAccountDeletion, testDeletionClaimToken, mock.AnythingOfType("string")).
		Return(true, nil)
	service.SetWorkClaimsRepo(mockClaims)

	mockRepo.On("GetUserByID", ctx, user.ID).Return(user, nil)
	// Reflects the corrected predicate's real answer for a lapsed-but-uncleared override with
	// no deletion request (see the doc comment above for the live-DB proof this mirrors).
	mockRepo.On("ClaimForDeletion", ctx, user.ID).Return(false, nil)

	err := service.ExecuteDeletion(ctx, user.ID)

	assert.NoError(t, err, "a denied claim is a legitimate skip, not an error")
	mockRepo.AssertCalled(t, "ClaimForDeletion", ctx, user.ID) // proves the belt-and-braces short-circuit did NOT fire here
	mockRepo.AssertNotCalled(t, "DeleteUserCascade", mock.Anything, mock.Anything)
	mockPlaidCleanup.AssertNotCalled(t, "RemoveAllUserItems", mock.Anything, mock.Anything)
	mockClaims.AssertCalled(t, "Release", ctx, repository.ResourceTypeUser, user.ID, repository.ActionAccountDeletion, testDeletionClaimToken, mock.AnythingOfType("string"))
}

// TestExecuteDeletion_NonComped_ProceedsNormally verifies the new guard does not affect
// ordinary (never-comped) deletions.
func TestExecuteDeletion_NonComped_ProceedsNormally(t *testing.T) {
	ctx := context.Background()
	mockRepo := new(MockProfileRepositoryForDeletion)
	mockEmail := new(MockEmailService)
	mockClerk := new(MockClerkService)
	mockAudit := new(MockAuditLogger)

	service := NewDeletionService(mockRepo, mockEmail, mockClerk, mockAudit, nil, 30, "test-secret-key")

	user := &domain.User{
		ID:          "user-123",
		ClerkUserID: "user_123",
		Email:       "test@example.com",
		IsDeleted:   false,
	}

	mockRepo.On("GetUserByID", ctx, user.ID).Return(user, nil)
	mockRepo.On("ClaimForDeletion", ctx, user.ID).Return(true, nil)
	service.SetWorkClaimsRepo(newAllowingClaimsRepo())
	mockRepo.On("DeleteUserCascade", ctx, user.ID).Return(nil)
	mockClerk.On("DeleteUser", ctx, user.ClerkUserID).Return(nil)
	mockAudit.On("Log", ctx, "user.deleted", "user", user.ID).Return(nil)

	err := service.ExecuteDeletion(ctx, user.ID)

	assert.NoError(t, err)
	mockRepo.AssertCalled(t, "DeleteUserCascade", ctx, user.ID)
}

// --- Issue #1413 Critical finding C2 (closed for real, round 3): TWO claim gates now guard
// ExecuteDeletion's destructive work ---
//
// The Part C guard above (HasActiveManualOverride() && DeletionRequestedAt == nil) is a
// read-only check against the GetUserByID snapshot. It cannot, by itself, protect the
// irreversible external cleanup (Paddle cancel, Plaid disconnect) and cascade delete from a comp
// granted in the window between that snapshot and this call. It is kept as a cheap
// belt-and-braces short-circuit (unchanged, see above). The tests below use a user object that
// looks eligible from the snapshot (no comp visible) so that short-circuit does NOT fire, forcing
// execution through BOTH real gates:
//
//  1. claimsRepo.Claim (work_claims, EXCLUSIVITY) — replaces the old self-assigning
//     ClaimForDeletion-as-a-guard. A lost claim here means another worker holds the lease, or the
//     action was already completed; the predicate is never even re-checked.
//  2. profileRepo.ClaimForDeletion (CORRECTNESS re-check) — still a conditional UPDATE evaluated
//     against committed state at the moment of the claim, now gated behind gate 1 rather than
//     being the only gate.
//
// Both gates release the work claim on denial/error so a later run doesn't wait out the full
// lease, per the #1413 design doc's required call shape.

// TestExecuteDeletion_WorkClaimNotGranted_SkipsWithoutPredicateOrCleanup proves gate 1 alone
// blocks all destructive work AND the predicate re-check: even though the snapshot looked
// eligible, a lost work claim (simulating another worker holding the lease, or this action
// already completed) must prevent ClaimForDeletion, Paddle cancel, Paddle archive, Plaid
// disconnect, cascade delete, and Clerk deletion. Every one of those collaborators is
// deliberately left unstubbed: an unstubbed testify mock call panics, a stronger proof than
// AssertNotCalled alone.
func TestExecuteDeletion_WorkClaimNotGranted_SkipsWithoutPredicateOrCleanup(t *testing.T) {
	ctx := context.Background()
	mockRepo := new(MockProfileRepositoryForDeletion)
	mockEmail := new(MockEmailService)
	mockClerk := new(MockClerkService)
	mockAudit := new(MockAuditLogger)
	mockPlaidCleanup := new(MockPlaidItemCleanupService)
	mockPaddle := new(MockPaddleDeletionCleaner)

	service := NewDeletionService(mockRepo, mockEmail, mockClerk, mockAudit, mockPlaidCleanup, 30, "test-secret-key")
	service.SetPaddleCleanupService(mockPaddle)

	user := &domain.User{
		ID:          "user-123",
		ClerkUserID: "user_123",
		Email:       "test@example.com",
		IsDeleted:   false,
	}

	mockClaims := new(MockWorkClaimsRepository)
	mockClaims.On("Claim", ctx, repository.ResourceTypeUser, user.ID, repository.ActionAccountDeletion, accountDeletionClaimLease).
		Return("", false, nil)
	service.SetWorkClaimsRepo(mockClaims)

	mockRepo.On("GetUserByID", ctx, user.ID).Return(user, nil)
	// ClaimForDeletion and everything below is deliberately left unstubbed on purpose.

	err := service.ExecuteDeletion(ctx, user.ID)

	assert.NoError(t, err, "a claim not won is a legitimate skip, not an error")
	mockRepo.AssertNotCalled(t, "ClaimForDeletion", mock.Anything, mock.Anything)
	mockPaddle.AssertNotCalled(t, "CancelSubscriptionImmediately", mock.Anything, mock.Anything)
	mockPaddle.AssertNotCalled(t, "ArchiveAndAnonymizeCustomer", mock.Anything, mock.Anything, mock.Anything)
	mockPlaidCleanup.AssertNotCalled(t, "RemoveAllUserItems", mock.Anything, mock.Anything)
	mockRepo.AssertNotCalled(t, "DeleteUserCascade", mock.Anything, mock.Anything)
	mockClerk.AssertNotCalled(t, "DeleteUser", mock.Anything, mock.Anything)
	mockAudit.AssertNotCalled(t, "Log", ctx, "user.deleted", mock.Anything, mock.Anything)
}

// TestExecuteDeletion_WorkClaimErrors_PropagatesAndSkipsPredicateAndCleanup proves a DB error
// during the work-claims Claim call itself (as opposed to a lost claim) propagates as an error
// and still must not reach the predicate re-check or any destructive work.
func TestExecuteDeletion_WorkClaimErrors_PropagatesAndSkipsPredicateAndCleanup(t *testing.T) {
	ctx := context.Background()
	mockRepo := new(MockProfileRepositoryForDeletion)
	mockEmail := new(MockEmailService)
	mockClerk := new(MockClerkService)
	mockAudit := new(MockAuditLogger)
	mockPlaidCleanup := new(MockPlaidItemCleanupService)

	service := NewDeletionService(mockRepo, mockEmail, mockClerk, mockAudit, mockPlaidCleanup, 30, "test-secret-key")

	user := &domain.User{
		ID:          "user-123",
		ClerkUserID: "user_123",
		Email:       "test@example.com",
		IsDeleted:   false,
	}

	mockClaims := new(MockWorkClaimsRepository)
	mockClaims.On("Claim", ctx, repository.ResourceTypeUser, user.ID, repository.ActionAccountDeletion, accountDeletionClaimLease).
		Return("", false, errors.New("db unavailable"))
	service.SetWorkClaimsRepo(mockClaims)

	mockRepo.On("GetUserByID", ctx, user.ID).Return(user, nil)
	// Nothing below is stubbed on purpose.

	err := service.ExecuteDeletion(ctx, user.ID)

	assert.Error(t, err)
	mockRepo.AssertNotCalled(t, "ClaimForDeletion", mock.Anything, mock.Anything)
	mockPlaidCleanup.AssertNotCalled(t, "RemoveAllUserItems", mock.Anything, mock.Anything)
	mockRepo.AssertNotCalled(t, "DeleteUserCascade", mock.Anything, mock.Anything)
}

// TestExecuteDeletion_PredicateDenied_ReleasesClaimAndSkipsCleanup proves gate 2 (the
// ClaimForDeletion re-check) still gates correctness even after gate 1 is won — e.g. a comp
// granted between the work claim and the predicate re-check — and that the work claim is
// released so a later run doesn't wait out the full lease.
func TestExecuteDeletion_PredicateDenied_ReleasesClaimAndSkipsCleanup(t *testing.T) {
	ctx := context.Background()
	mockRepo := new(MockProfileRepositoryForDeletion)
	mockEmail := new(MockEmailService)
	mockClerk := new(MockClerkService)
	mockAudit := new(MockAuditLogger)
	mockPlaidCleanup := new(MockPlaidItemCleanupService)
	mockPaddle := new(MockPaddleDeletionCleaner)

	service := NewDeletionService(mockRepo, mockEmail, mockClerk, mockAudit, mockPlaidCleanup, 30, "test-secret-key")
	service.SetPaddleCleanupService(mockPaddle)

	user := &domain.User{
		ID:          "user-123",
		ClerkUserID: "user_123",
		Email:       "test@example.com",
		IsDeleted:   false,
	}

	mockClaims := new(MockWorkClaimsRepository)
	mockClaims.On("Claim", ctx, repository.ResourceTypeUser, user.ID, repository.ActionAccountDeletion, accountDeletionClaimLease).
		Return(testDeletionClaimToken, true, nil)
	mockClaims.On("Release", ctx, repository.ResourceTypeUser, user.ID, repository.ActionAccountDeletion, testDeletionClaimToken, mock.AnythingOfType("string")).
		Return(true, nil)
	service.SetWorkClaimsRepo(mockClaims)

	mockRepo.On("GetUserByID", ctx, user.ID).Return(user, nil)
	mockRepo.On("ClaimForDeletion", ctx, user.ID).Return(false, nil)
	// Everything below is deliberately left unstubbed on purpose.

	err := service.ExecuteDeletion(ctx, user.ID)

	assert.NoError(t, err, "a denied predicate re-check is a legitimate skip, not an error")
	mockPaddle.AssertNotCalled(t, "CancelSubscriptionImmediately", mock.Anything, mock.Anything)
	mockPlaidCleanup.AssertNotCalled(t, "RemoveAllUserItems", mock.Anything, mock.Anything)
	mockRepo.AssertNotCalled(t, "DeleteUserCascade", mock.Anything, mock.Anything)
	mockClerk.AssertNotCalled(t, "DeleteUser", mock.Anything, mock.Anything)
	mockAudit.AssertNotCalled(t, "Log", ctx, "user.deleted", mock.Anything, mock.Anything)
	mockClaims.AssertCalled(t, "Release", ctx, repository.ResourceTypeUser, user.ID, repository.ActionAccountDeletion, testDeletionClaimToken, mock.AnythingOfType("string"))
}

// TestExecuteDeletion_PredicateErrors_ReleasesClaimAndPropagates proves a DB error during the
// predicate re-check itself (as opposed to a denial) propagates as an error, releases the work
// claim, and still must not run any destructive work.
func TestExecuteDeletion_PredicateErrors_ReleasesClaimAndPropagates(t *testing.T) {
	ctx := context.Background()
	mockRepo := new(MockProfileRepositoryForDeletion)
	mockEmail := new(MockEmailService)
	mockClerk := new(MockClerkService)
	mockAudit := new(MockAuditLogger)
	mockPlaidCleanup := new(MockPlaidItemCleanupService)

	service := NewDeletionService(mockRepo, mockEmail, mockClerk, mockAudit, mockPlaidCleanup, 30, "test-secret-key")

	user := &domain.User{
		ID:          "user-123",
		ClerkUserID: "user_123",
		Email:       "test@example.com",
		IsDeleted:   false,
	}

	mockClaims := new(MockWorkClaimsRepository)
	mockClaims.On("Claim", ctx, repository.ResourceTypeUser, user.ID, repository.ActionAccountDeletion, accountDeletionClaimLease).
		Return(testDeletionClaimToken, true, nil)
	mockClaims.On("Release", ctx, repository.ResourceTypeUser, user.ID, repository.ActionAccountDeletion, testDeletionClaimToken, mock.AnythingOfType("string")).
		Return(true, nil)
	service.SetWorkClaimsRepo(mockClaims)

	mockRepo.On("GetUserByID", ctx, user.ID).Return(user, nil)
	mockRepo.On("ClaimForDeletion", ctx, user.ID).Return(false, errors.New("db unavailable"))
	// Nothing below is stubbed on purpose.

	err := service.ExecuteDeletion(ctx, user.ID)

	assert.Error(t, err)
	mockPlaidCleanup.AssertNotCalled(t, "RemoveAllUserItems", mock.Anything, mock.Anything)
	mockRepo.AssertNotCalled(t, "DeleteUserCascade", mock.Anything, mock.Anything)
	mockClaims.AssertCalled(t, "Release", ctx, repository.ResourceTypeUser, user.ID, repository.ActionAccountDeletion, testDeletionClaimToken, mock.AnythingOfType("string"))
}

// TestExecuteDeletion_BothClaimsGranted_ProceedsWithFullCleanupAndCompletes is the
// negative-direction proof: an over-tight claim/predicate that silently stopped ALL deletions
// would pass every "nothing bad happened" test above, so this asserts the destructive path
// genuinely still runs once BOTH gates are won, and that Complete is called with the exact
// token Claim returned — closing the loop the fencing token exists to protect.
func TestExecuteDeletion_BothClaimsGranted_ProceedsWithFullCleanupAndCompletes(t *testing.T) {
	ctx := context.Background()
	mockRepo := new(MockProfileRepositoryForDeletion)
	mockEmail := new(MockEmailService)
	mockClerk := new(MockClerkService)
	mockAudit := new(MockAuditLogger)
	mockPlaidCleanup := new(MockPlaidItemCleanupService)

	service := NewDeletionService(mockRepo, mockEmail, mockClerk, mockAudit, mockPlaidCleanup, 30, "test-secret-key")

	user := &domain.User{
		ID:          "user-123",
		ClerkUserID: "user_123",
		Email:       "test@example.com",
		IsDeleted:   false,
	}

	mockClaims := new(MockWorkClaimsRepository)
	mockClaims.On("Claim", ctx, repository.ResourceTypeUser, user.ID, repository.ActionAccountDeletion, accountDeletionClaimLease).
		Return(testDeletionClaimToken, true, nil)
	mockClaims.On("Complete", ctx, repository.ResourceTypeUser, user.ID, repository.ActionAccountDeletion, testDeletionClaimToken).
		Return(true, nil)
	service.SetWorkClaimsRepo(mockClaims)

	mockRepo.On("GetUserByID", ctx, user.ID).Return(user, nil)
	mockRepo.On("ClaimForDeletion", ctx, user.ID).Return(true, nil)
	mockPlaidCleanup.On("RemoveAllUserItems", ctx, user.ID).Return(nil)
	mockRepo.On("DeleteUserCascade", ctx, user.ID).Return(nil)
	mockClerk.On("DeleteUser", ctx, user.ClerkUserID).Return(nil)
	mockAudit.On("Log", ctx, "user.deleted", "user", user.ID).Return(nil)

	err := service.ExecuteDeletion(ctx, user.ID)

	assert.NoError(t, err)
	mockPlaidCleanup.AssertCalled(t, "RemoveAllUserItems", ctx, user.ID)
	mockRepo.AssertCalled(t, "DeleteUserCascade", ctx, user.ID)
	mockClerk.AssertCalled(t, "DeleteUser", ctx, user.ClerkUserID)
	mockClaims.AssertCalled(t, "Complete", ctx, repository.ResourceTypeUser, user.ID, repository.ActionAccountDeletion, testDeletionClaimToken)
}

// TestExecuteDeletion_CompleteReturnsFalse_LoggedNotSwallowed proves that a lost lease at
// Complete time (another worker reclaimed this tuple while we were mid-flight — the exact
// scenario the fencing token exists to detect) does not silently pass as success: ExecuteDeletion
// must still return nil (the deletion genuinely happened; there's nothing left to retry), but the
// caller-visible contract is that this case is surfaced loudly rather than swallowed. Since the
// production code logs via slog.Default() rather than an injectable logger, this test's only
// direct assertion is that ExecuteDeletion does not error or panic when Complete reports ok=false
// — the loud-logging behavior itself is inspected in the source (see ExecuteDeletion's Complete
// handling) and is not independently mechanically asserted here for lack of an injectable sink.
func TestExecuteDeletion_CompleteReturnsFalse_LoggedNotSwallowed(t *testing.T) {
	ctx := context.Background()
	mockRepo := new(MockProfileRepositoryForDeletion)
	mockEmail := new(MockEmailService)
	mockClerk := new(MockClerkService)
	mockAudit := new(MockAuditLogger)
	mockPlaidCleanup := new(MockPlaidItemCleanupService)

	service := NewDeletionService(mockRepo, mockEmail, mockClerk, mockAudit, mockPlaidCleanup, 30, "test-secret-key")

	user := &domain.User{
		ID:          "user-123",
		ClerkUserID: "user_123",
		Email:       "test@example.com",
		IsDeleted:   false,
	}

	mockClaims := new(MockWorkClaimsRepository)
	mockClaims.On("Claim", ctx, repository.ResourceTypeUser, user.ID, repository.ActionAccountDeletion, accountDeletionClaimLease).
		Return(testDeletionClaimToken, true, nil)
	// The lease was reclaimed by another worker before we could Complete it.
	mockClaims.On("Complete", ctx, repository.ResourceTypeUser, user.ID, repository.ActionAccountDeletion, testDeletionClaimToken).
		Return(false, nil)
	service.SetWorkClaimsRepo(mockClaims)

	mockRepo.On("GetUserByID", ctx, user.ID).Return(user, nil)
	mockRepo.On("ClaimForDeletion", ctx, user.ID).Return(true, nil)
	mockPlaidCleanup.On("RemoveAllUserItems", ctx, user.ID).Return(nil)
	mockRepo.On("DeleteUserCascade", ctx, user.ID).Return(nil)
	mockClerk.On("DeleteUser", ctx, user.ClerkUserID).Return(nil)
	mockAudit.On("Log", ctx, "user.deleted", "user", user.ID).Return(nil)

	err := service.ExecuteDeletion(ctx, user.ID)

	assert.NoError(t, err, "the cascade already ran; a lost lease at Complete time must not be reported as a deletion failure")
	mockRepo.AssertCalled(t, "DeleteUserCascade", ctx, user.ID)
	mockClaims.AssertCalled(t, "Complete", ctx, repository.ResourceTypeUser, user.ID, repository.ActionAccountDeletion, testDeletionClaimToken)
}
