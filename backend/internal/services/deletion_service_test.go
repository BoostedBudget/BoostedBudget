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
func (m *MockProfileRepositoryForDeletion) GetSubscriptionStatusByUserID(ctx context.Context, userID string) (string, error) {
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

// TestExecuteDeletion_SucceedsEvenIfPlaidCleanupFails verifies that
// ExecuteDeletion continues with deletion even if Plaid cleanup fails
// Issue #668: Graceful degradation - don't block deletion if Plaid unavailable
func TestExecuteDeletion_SucceedsEvenIfPlaidCleanupFails(t *testing.T) {
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

	mockRepo.On("GetUserByID", ctx, user.ID).Return(user, nil)
	// Plaid cleanup fails
	mockPlaidCleanup.On("RemoveAllUserItems", ctx, user.ID).Return(errors.New("plaid API unavailable"))
	// But deletion should still proceed
	mockRepo.On("DeleteUserCascade", ctx, user.ID).Return(nil)
	mockClerk.On("DeleteUser", ctx, user.ClerkUserID).Return(nil)
	mockAudit.On("Log", ctx, "user.deleted", "user", user.ID).Return(nil)

	err := service.ExecuteDeletion(ctx, user.ID)

	// Should succeed even if Plaid cleanup fails
	assert.NoError(t, err)
	mockRepo.AssertCalled(t, "DeleteUserCascade", ctx, user.ID)
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
	mockRepo.On("DeleteUserCascade", ctx, user.ID).Return(nil)
	mockClerk.On("DeleteUser", ctx, user.ClerkUserID).Return(nil)
	mockAudit.On("Log", ctx, "user.deleted", "user", user.ID).Return(nil)

	err := service.ExecuteDeletion(ctx, user.ID)
	assert.NoError(t, err)
	mockPaddle.AssertNotCalled(t, "ArchiveAndAnonymizeCustomer", mock.Anything, mock.Anything, mock.Anything)
}

// TestExecuteDeletion_PaddleCancelFails_DeletionContinues verifies that a Paddle cancel
// failure is non-critical — deletion still completes. Issue #1006.
func TestExecuteDeletion_PaddleCancelFails_DeletionContinues(t *testing.T) {
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
	mockPaddle.On("CancelSubscriptionImmediately", ctx, paddleSubID).Return(errors.New("Paddle API timeout"))
	mockPaddle.On("ArchiveAndAnonymizeCustomer", ctx, paddleCustID, expectedAnonymizedEmail).Return(nil)
	mockRepo.On("DeleteUserCascade", ctx, user.ID).Return(nil)
	mockClerk.On("DeleteUser", ctx, user.ClerkUserID).Return(nil)
	mockAudit.On("Log", ctx, "user.deleted", "user", user.ID).Return(nil)

	err := service.ExecuteDeletion(ctx, user.ID)
	// Must succeed despite Paddle cancel failure
	assert.NoError(t, err)
	mockRepo.AssertCalled(t, "DeleteUserCascade", ctx, user.ID)
	// Archive must still run even when cancel fails
	mockPaddle.AssertCalled(t, "ArchiveAndAnonymizeCustomer", ctx, paddleCustID, expectedAnonymizedEmail)
}

// TestExecuteDeletion_PaddleArchiveFails_DeletionContinues verifies that a Paddle archive
// failure is non-critical — deletion still completes. Issue #1006.
func TestExecuteDeletion_PaddleArchiveFails_DeletionContinues(t *testing.T) {
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
	mockPaddle.On("CancelSubscriptionImmediately", ctx, paddleSubID).Return(nil)
	mockPaddle.On("ArchiveAndAnonymizeCustomer", ctx, paddleCustID, expectedAnonymizedEmail).Return(errors.New("Paddle API timeout"))
	mockRepo.On("DeleteUserCascade", ctx, user.ID).Return(nil)
	mockClerk.On("DeleteUser", ctx, user.ClerkUserID).Return(nil)
	mockAudit.On("Log", ctx, "user.deleted", "user", user.ID).Return(nil)

	err := service.ExecuteDeletion(ctx, user.ID)
	// Must succeed despite Paddle archive failure
	assert.NoError(t, err)
	mockRepo.AssertCalled(t, "DeleteUserCascade", ctx, user.ID)
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
