package services

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/jackc/pgx/v5"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
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
