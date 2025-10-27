package services

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

// MockProfileRepository for testing
type MockProfileRepository struct {
	mock.Mock
}

func (m *MockProfileRepository) GetUserByID(ctx context.Context, userID string) (*domain.User, error) {
	args := m.Called(ctx, userID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*domain.User), args.Error(1)
}

func (m *MockProfileRepository) GetUserByAuth0ID(ctx context.Context, auth0UserID string) (*domain.User, error) {
	args := m.Called(ctx, auth0UserID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*domain.User), args.Error(1)
}

func (m *MockProfileRepository) CreateUser(ctx context.Context, user *domain.User) error {
	args := m.Called(ctx, user)
	return args.Error(0)
}

func (m *MockProfileRepository) UpdateUser(ctx context.Context, user *domain.User) error {
	args := m.Called(ctx, user)
	return args.Error(0)
}

func (m *MockProfileRepository) UpdateLastLogin(ctx context.Context, auth0UserID string) error {
	args := m.Called(ctx, auth0UserID)
	return args.Error(0)
}

func (m *MockProfileRepository) MarkForDeletion(ctx context.Context, userID string, scheduledAt time.Time) error {
	args := m.Called(ctx, userID, scheduledAt)
	return args.Error(0)
}

func (m *MockProfileRepository) CancelDeletion(ctx context.Context, userID string) error {
	args := m.Called(ctx, userID)
	return args.Error(0)
}

func (m *MockProfileRepository) GetUsersScheduledForDeletion(ctx context.Context) ([]*domain.User, error) {
	args := m.Called(ctx)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]*domain.User), args.Error(1)
}

func (m *MockProfileRepository) DeleteUserCascade(ctx context.Context, userID string) error {
	args := m.Called(ctx, userID)
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

// MockAuth0Service for testing
type MockAuth0Service struct {
	mock.Mock
}

func (m *MockAuth0Service) DeleteUser(ctx context.Context, auth0UserID string) error {
	args := m.Called(ctx, auth0UserID)
	return args.Error(0)
}

func (m *MockAuth0Service) GetManagementToken(ctx context.Context) (string, error) {
	args := m.Called(ctx)
	return args.String(0), args.Error(1)
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
	mockRepo := new(MockProfileRepository)
	mockEmail := new(MockEmailService)
	mockAuth0 := new(MockAuth0Service)
	mockAudit := new(MockAuditLogger)

	service := NewDeletionService(mockRepo, mockEmail, mockAuth0, mockAudit, 30, "test-secret-key")

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
	mockRepo := new(MockProfileRepository)
	mockEmail := new(MockEmailService)
	mockAuth0 := new(MockAuth0Service)
	mockAudit := new(MockAuditLogger)

	gracePeriodDays := 30
	service := NewDeletionService(mockRepo, mockEmail, mockAuth0, mockAudit, gracePeriodDays, "test-secret-key")

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
	mockRepo := new(MockProfileRepository)
	mockEmail := new(MockEmailService)
	mockAuth0 := new(MockAuth0Service)
	mockAudit := new(MockAuditLogger)

	service := NewDeletionService(mockRepo, mockEmail, mockAuth0, mockAudit, 30, "test-secret-key")

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
	mockRepo := new(MockProfileRepository)
	mockEmail := new(MockEmailService)
	mockAuth0 := new(MockAuth0Service)
	mockAudit := new(MockAuditLogger)

	service := NewDeletionService(mockRepo, mockEmail, mockAuth0, mockAudit, 30, "test-secret-key")

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
	mockRepo := new(MockProfileRepository)
	mockEmail := new(MockEmailService)
	mockAuth0 := new(MockAuth0Service)
	mockAudit := new(MockAuditLogger)

	service := NewDeletionService(mockRepo, mockEmail, mockAuth0, mockAudit, 30, "test-secret-key")

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
	mockRepo := new(MockProfileRepository)
	mockEmail := new(MockEmailService)
	mockAuth0 := new(MockAuth0Service)
	mockAudit := new(MockAuditLogger)

	service := NewDeletionService(mockRepo, mockEmail, mockAuth0, mockAudit, 30, "test-secret-key")

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
	mockRepo := new(MockProfileRepository)
	mockEmail := new(MockEmailService)
	mockAuth0 := new(MockAuth0Service)
	mockAudit := new(MockAuditLogger)

	service := NewDeletionService(mockRepo, mockEmail, mockAuth0, mockAudit, 30, "test-secret-key")

	invalidToken := "invalid-token-string"

	err := service.CancelDeletion(ctx, invalidToken)

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid token")
	mockRepo.AssertNotCalled(t, "CancelDeletion", mock.Anything, mock.Anything)
}

// TestCancelDeletion_ExpiredToken tests expired token handling
func TestCancelDeletion_ExpiredToken(t *testing.T) {
	ctx := context.Background()
	mockRepo := new(MockProfileRepository)
	mockEmail := new(MockEmailService)
	mockAuth0 := new(MockAuth0Service)
	mockAudit := new(MockAuditLogger)

	tokenSecret := "test-secret-key"
	service := NewDeletionService(mockRepo, mockEmail, mockAuth0, mockAudit, 30, tokenSecret)

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
	mockRepo := new(MockProfileRepository)
	mockEmail := new(MockEmailService)
	mockAuth0 := new(MockAuth0Service)
	mockAudit := new(MockAuditLogger)

	service := NewDeletionService(mockRepo, mockEmail, mockAuth0, mockAudit, 30, "test-secret-key")

	user := &domain.User{
		ID:          "user-123",
		Auth0UserID: "auth0|123",
		Email:       "test@example.com",
		IsDeleted:   false,
	}

	mockRepo.On("GetUserByID", ctx, user.ID).Return(user, nil)
	mockRepo.On("DeleteUserCascade", ctx, user.ID).Return(nil)
	mockAuth0.On("DeleteUser", ctx, user.Auth0UserID).Return(nil)
	mockAudit.On("Log", ctx, "user.deleted", "user", user.ID).Return(nil)

	err := service.ExecuteDeletion(ctx, user.ID)

	assert.NoError(t, err)
	mockRepo.AssertCalled(t, "DeleteUserCascade", ctx, user.ID)
	mockAuth0.AssertCalled(t, "DeleteUser", ctx, user.Auth0UserID)
	mockAudit.AssertCalled(t, "Log", ctx, "user.deleted", "user", user.ID)
}

// TestExecuteDeletion_DeletesAuth0User verifies Auth0 deletion
func TestExecuteDeletion_DeletesAuth0User(t *testing.T) {
	ctx := context.Background()
	mockRepo := new(MockProfileRepository)
	mockEmail := new(MockEmailService)
	mockAuth0 := new(MockAuth0Service)
	mockAudit := new(MockAuditLogger)

	service := NewDeletionService(mockRepo, mockEmail, mockAuth0, mockAudit, 30, "test-secret-key")

	user := &domain.User{
		ID:          "user-123",
		Auth0UserID: "auth0|123",
		Email:       "test@example.com",
		IsDeleted:   false,
	}

	mockRepo.On("GetUserByID", ctx, user.ID).Return(user, nil)
	mockRepo.On("DeleteUserCascade", ctx, user.ID).Return(nil)
	mockAuth0.On("DeleteUser", ctx, user.Auth0UserID).Return(nil)
	mockAudit.On("Log", ctx, "user.deleted", "user", user.ID).Return(nil)

	err := service.ExecuteDeletion(ctx, user.ID)

	assert.NoError(t, err)
	mockAuth0.AssertExpectations(t)
	mockAuth0.AssertCalled(t, "DeleteUser", ctx, user.Auth0UserID)
}

// TestExecuteDeletion_HandlesAlreadyDeleted tests idempotency
func TestExecuteDeletion_HandlesAlreadyDeleted(t *testing.T) {
	ctx := context.Background()
	mockRepo := new(MockProfileRepository)
	mockEmail := new(MockEmailService)
	mockAuth0 := new(MockAuth0Service)
	mockAudit := new(MockAuditLogger)

	service := NewDeletionService(mockRepo, mockEmail, mockAuth0, mockAudit, 30, "test-secret-key")

	user := &domain.User{
		ID:          "user-123",
		Auth0UserID: "auth0|123",
		Email:       "test@example.com",
		IsDeleted:   true, // Already deleted
	}

	mockRepo.On("GetUserByID", ctx, user.ID).Return(user, nil)

	err := service.ExecuteDeletion(ctx, user.ID)

	assert.NoError(t, err, "Should not error on already deleted user")
	mockRepo.AssertNotCalled(t, "DeleteUserCascade", mock.Anything, mock.Anything)
	mockAuth0.AssertNotCalled(t, "DeleteUser", mock.Anything, mock.Anything)
}

// TestExecuteDeletion_HandlesAuth0Error tests graceful Auth0 error handling
func TestExecuteDeletion_HandlesAuth0Error(t *testing.T) {
	ctx := context.Background()
	mockRepo := new(MockProfileRepository)
	mockEmail := new(MockEmailService)
	mockAuth0 := new(MockAuth0Service)
	mockAudit := new(MockAuditLogger)

	service := NewDeletionService(mockRepo, mockEmail, mockAuth0, mockAudit, 30, "test-secret-key")

	user := &domain.User{
		ID:          "user-123",
		Auth0UserID: "auth0|123",
		Email:       "test@example.com",
		IsDeleted:   false,
	}

	mockRepo.On("GetUserByID", ctx, user.ID).Return(user, nil)
	mockRepo.On("DeleteUserCascade", ctx, user.ID).Return(nil)
	mockAuth0.On("DeleteUser", ctx, user.Auth0UserID).Return(errors.New("Auth0 API error"))
	mockAudit.On("Log", ctx, "user.deleted", "user", user.ID).Return(nil)

	err := service.ExecuteDeletion(ctx, user.ID)

	// Should still succeed even if Auth0 fails (our data is deleted)
	assert.NoError(t, err)
	mockRepo.AssertCalled(t, "DeleteUserCascade", ctx, user.ID)
	mockAudit.AssertCalled(t, "Log", ctx, "user.deleted", "user", user.ID)
}
