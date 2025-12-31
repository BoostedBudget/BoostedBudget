package services

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/repository"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

// MockProfileRepository is a mock implementation of ProfileRepository
type MockProfileRepository struct {
	mock.Mock
}

func (m *MockProfileRepository) GetUserByClerkID(ctx context.Context, clerkUserID string) (*domain.User, error) {
	args := m.Called(ctx, clerkUserID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*domain.User), args.Error(1)
}

func (m *MockProfileRepository) GetUserByID(ctx context.Context, id string) (*domain.User, error) {
	args := m.Called(ctx, id)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*domain.User), args.Error(1)
}

func (m *MockProfileRepository) CreateUser(ctx context.Context, user *domain.User) error {
	args := m.Called(ctx, user)
	return args.Error(0)
}

func (m *MockProfileRepository) CreateUserTx(ctx context.Context, tx *sql.Tx, user *domain.User) error {
	args := m.Called(ctx, tx, user)
	return args.Error(0)
}

func (m *MockProfileRepository) UpdateUser(ctx context.Context, user *domain.User) error {
	args := m.Called(ctx, user)
	return args.Error(0)
}

func (m *MockProfileRepository) UpdateLastLogin(ctx context.Context, clerkUserID string) error {
	args := m.Called(ctx, clerkUserID)
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

// TestGetProfile_ExistingUser tests retrieving an existing user
// NOTE: After PR #197, GetProfile now receives internal user UUID, not Clerk ID
func TestGetProfile_ExistingUser(t *testing.T) {
	// Setup
	mockRepo := new(MockProfileRepository)
	service := NewProfileService(mockRepo)
	ctx := context.Background()

	userID := uuid.New().String()
	clerkUserID := "user_507f1f77bcf86cd799439011"
	email := "test@example.com"
	existingUser := &domain.User{
		ID:                 userID,
		ClerkUserID:        clerkUserID,
		Email:              email,
		SubscriptionTier:   "free",
		SubscriptionStatus: "active",
		PrimaryRegion:      "iad",
		CreatedAt:          time.Now(),
		UpdatedAt:          time.Now(),
	}

	// Mock expectations - now using GetUserByID with internal UUID
	mockRepo.On("GetUserByID", ctx, userID).Return(existingUser, nil)
	mockRepo.On("UpdateLastLogin", ctx, clerkUserID).Return(nil)

	// Execute - pass internal UUID, not Clerk ID
	user, err := service.GetProfile(ctx, userID, email)

	// Assert
	assert.NoError(t, err)
	assert.NotNil(t, user)
	assert.Equal(t, clerkUserID, user.ClerkUserID)
	assert.Equal(t, email, user.Email)
	mockRepo.AssertExpectations(t)
}

// TestGetProfile_UserNotFound tests handling when user doesn't exist
// NOTE: After PR #197, GetProfile no longer creates users (handled by Clerk middleware)
// It now returns ErrUserNotFound if user doesn't exist
func TestGetProfile_UserNotFound(t *testing.T) {
	// Setup
	mockRepo := new(MockProfileRepository)
	service := NewProfileService(mockRepo)
	ctx := context.Background()

	userID := uuid.New().String()
	email := "newuser@example.com"

	// Mock expectations - user not found
	mockRepo.On("GetUserByID", ctx, userID).Return(nil, repository.ErrUserNotFound)

	// Execute
	user, err := service.GetProfile(ctx, userID, email)

	// Assert - should return error, not create user
	assert.Error(t, err)
	assert.Equal(t, repository.ErrUserNotFound, err)
	assert.Nil(t, user)
	mockRepo.AssertExpectations(t)
}

// TestGetProfile_RepositoryError tests handling of repository errors
func TestGetProfile_RepositoryError(t *testing.T) {
	// Setup
	mockRepo := new(MockProfileRepository)
	service := NewProfileService(mockRepo)
	ctx := context.Background()

	userID := uuid.New().String()
	email := "test@example.com"

	// Mock expectations - database error
	mockRepo.On("GetUserByID", ctx, userID).Return(nil, errors.New("database error"))

	// Execute
	user, err := service.GetProfile(ctx, userID, email)

	// Assert
	assert.Error(t, err)
	assert.Nil(t, user)
	mockRepo.AssertExpectations(t)
}

// TestGetProfile_UpdateLastLoginError tests that UpdateLastLogin errors don't fail the request
func TestGetProfile_UpdateLastLoginError(t *testing.T) {
	// Setup
	mockRepo := new(MockProfileRepository)
	service := NewProfileService(mockRepo)
	ctx := context.Background()

	userID := uuid.New().String()
	clerkUserID := "user_507f1f77bcf86cd799439011"
	email := "test@example.com"
	existingUser := &domain.User{
		ID:                 userID,
		ClerkUserID:        clerkUserID,
		Email:              email,
		SubscriptionTier:   "free",
		SubscriptionStatus: "active",
		PrimaryRegion:      "iad",
	}

	// Mock expectations - UpdateLastLogin fails but shouldn't affect response
	mockRepo.On("GetUserByID", ctx, userID).Return(existingUser, nil)
	mockRepo.On("UpdateLastLogin", ctx, clerkUserID).Return(errors.New("update failed"))

	// Execute
	user, err := service.GetProfile(ctx, userID, email)

	// Assert - should still succeed despite UpdateLastLogin error
	assert.NoError(t, err)
	assert.NotNil(t, user)
	mockRepo.AssertExpectations(t)
}

// TestUpdateProfile_Success tests successful profile update
// NOTE: After PR #197, UpdateProfile now receives internal user UUID, not Clerk ID
func TestUpdateProfile_Success(t *testing.T) {
	// Setup
	mockRepo := new(MockProfileRepository)
	service := NewProfileService(mockRepo)
	ctx := context.Background()

	userID := uuid.New().String()
	clerkUserID := "user_507f1f77bcf86cd799439011"
	existingUser := &domain.User{
		ID:                 userID,
		ClerkUserID:        clerkUserID,
		Email:              "old@example.com",
		SubscriptionTier:   "free",
		SubscriptionStatus: "active",
		PrimaryRegion:      "iad",
	}

	newEmail := "new@example.com"
	req := &domain.UpdateProfileRequest{
		Email: &newEmail,
	}

	// Mock expectations - now using GetUserByID with internal UUID
	mockRepo.On("GetUserByID", ctx, userID).Return(existingUser, nil)
	mockRepo.On("UpdateUser", ctx, mock.MatchedBy(func(u *domain.User) bool {
		return u.Email == newEmail && u.PrimaryRegion == "iad"
	})).Return(nil)

	// Execute - pass internal UUID, not Clerk ID
	user, err := service.UpdateProfile(ctx, userID, req)

	// Assert
	assert.NoError(t, err)
	assert.NotNil(t, user)
	assert.Equal(t, newEmail, user.Email)
	mockRepo.AssertExpectations(t)
}

// TestUpdateProfile_InvalidEmail tests email validation
// NOTE: After PR #197, UpdateProfile now receives internal user UUID, not Clerk ID
func TestUpdateProfile_InvalidEmail(t *testing.T) {
	// Setup
	mockRepo := new(MockProfileRepository)
	service := NewProfileService(mockRepo)
	ctx := context.Background()

	userID := uuid.New().String()
	clerkUserID := "user_507f1f77bcf86cd799439011"
	existingUser := &domain.User{
		ID:            userID,
		ClerkUserID:   clerkUserID,
		Email:         "valid@example.com",
		PrimaryRegion: "iad",
	}

	invalidEmail := "not-an-email"
	req := &domain.UpdateProfileRequest{
		Email: &invalidEmail,
	}

	// Mock expectations - now using GetUserByID with internal UUID
	mockRepo.On("GetUserByID", ctx, userID).Return(existingUser, nil)

	// Execute - pass internal UUID, not Clerk ID
	user, err := service.UpdateProfile(ctx, userID, req)

	// Assert
	assert.Error(t, err)
	assert.Equal(t, ErrInvalidEmail, err)
	assert.Nil(t, user)
	mockRepo.AssertExpectations(t)
}

// TestUpdateProfile_InvalidRegion tests region validation
// NOTE: After PR #197, UpdateProfile now receives internal user UUID, not Clerk ID
func TestUpdateProfile_InvalidRegion(t *testing.T) {
	// Setup
	mockRepo := new(MockProfileRepository)
	service := NewProfileService(mockRepo)
	ctx := context.Background()

	userID := uuid.New().String()
	clerkUserID := "user_507f1f77bcf86cd799439011"
	existingUser := &domain.User{
		ID:            userID,
		ClerkUserID:   clerkUserID,
		Email:         "test@example.com",
		PrimaryRegion: "iad",
	}

	invalidRegion := "invalid-region"
	req := &domain.UpdateProfileRequest{
		PrimaryRegion: &invalidRegion,
	}

	// Mock expectations - now using GetUserByID with internal UUID
	mockRepo.On("GetUserByID", ctx, userID).Return(existingUser, nil)

	// Execute - pass internal UUID, not Clerk ID
	user, err := service.UpdateProfile(ctx, userID, req)

	// Assert
	assert.Error(t, err)
	assert.Equal(t, ErrInvalidRegion, err)
	assert.Nil(t, user)
	mockRepo.AssertExpectations(t)
}

// TestUpdateProfile_ValidRegion tests updating to a valid region
// NOTE: After PR #197, UpdateProfile now receives internal user UUID, not Clerk ID
func TestUpdateProfile_ValidRegion(t *testing.T) {
	// Setup
	mockRepo := new(MockProfileRepository)
	service := NewProfileService(mockRepo)
	ctx := context.Background()

	userID := uuid.New().String()
	clerkUserID := "user_507f1f77bcf86cd799439011"
	existingUser := &domain.User{
		ID:            userID,
		ClerkUserID:   clerkUserID,
		Email:         "test@example.com",
		PrimaryRegion: "iad",
	}

	newRegion := "lhr"
	req := &domain.UpdateProfileRequest{
		PrimaryRegion: &newRegion,
	}

	// Mock expectations - now using GetUserByID with internal UUID
	mockRepo.On("GetUserByID", ctx, userID).Return(existingUser, nil)
	mockRepo.On("UpdateUser", ctx, mock.MatchedBy(func(u *domain.User) bool {
		return u.PrimaryRegion == newRegion
	})).Return(nil)

	// Execute - pass internal UUID, not Clerk ID
	user, err := service.UpdateProfile(ctx, userID, req)

	// Assert
	assert.NoError(t, err)
	assert.NotNil(t, user)
	assert.Equal(t, newRegion, user.PrimaryRegion)
	mockRepo.AssertExpectations(t)
}

// TestUpdateProfile_UserNotFound tests updating non-existent user
// NOTE: After PR #197, UpdateProfile now receives internal user UUID, not Clerk ID
func TestUpdateProfile_UserNotFound(t *testing.T) {
	// Setup
	mockRepo := new(MockProfileRepository)
	service := NewProfileService(mockRepo)
	ctx := context.Background()

	userID := uuid.New().String()
	newEmail := "new@example.com"
	req := &domain.UpdateProfileRequest{
		Email: &newEmail,
	}

	// Mock expectations - now using GetUserByID with internal UUID
	mockRepo.On("GetUserByID", ctx, userID).Return(nil, repository.ErrUserNotFound)

	// Execute - pass internal UUID, not Clerk ID
	user, err := service.UpdateProfile(ctx, userID, req)

	// Assert
	assert.Error(t, err)
	assert.Equal(t, repository.ErrUserNotFound, err)
	assert.Nil(t, user)
	mockRepo.AssertExpectations(t)
}

// TestUpdateProfile_UpdateError tests handling of update errors
// NOTE: After PR #197, UpdateProfile now receives internal user UUID, not Clerk ID
func TestUpdateProfile_UpdateError(t *testing.T) {
	// Setup
	mockRepo := new(MockProfileRepository)
	service := NewProfileService(mockRepo)
	ctx := context.Background()

	userID := uuid.New().String()
	clerkUserID := "user_507f1f77bcf86cd799439011"
	existingUser := &domain.User{
		ID:            userID,
		ClerkUserID:   clerkUserID,
		Email:         "test@example.com",
		PrimaryRegion: "iad",
	}

	newEmail := "new@example.com"
	req := &domain.UpdateProfileRequest{
		Email: &newEmail,
	}

	// Mock expectations - now using GetUserByID with internal UUID
	mockRepo.On("GetUserByID", ctx, userID).Return(existingUser, nil)
	mockRepo.On("UpdateUser", ctx, mock.Anything).Return(errors.New("update failed"))

	// Execute - pass internal UUID, not Clerk ID
	user, err := service.UpdateProfile(ctx, userID, req)

	// Assert
	assert.Error(t, err)
	assert.Nil(t, user)
	mockRepo.AssertExpectations(t)
}

// TestValidateEmail tests email validation function
func TestValidateEmail(t *testing.T) {
	tests := []struct {
		name    string
		email   string
		wantErr bool
	}{
		{"valid email", "test@example.com", false},
		{"valid email with subdomain", "user@mail.example.com", false},
		{"valid email with plus", "test+tag@example.com", false},
		{"invalid - no @", "notanemail", true},
		{"invalid - no domain", "test@", true},
		{"invalid - no local part", "@example.com", true},
		{"invalid - empty", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateEmail(tt.email)
			if tt.wantErr {
				assert.Error(t, err)
				assert.Equal(t, ErrInvalidEmail, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

// TestValidateRegion tests region validation function
func TestValidateRegion(t *testing.T) {
	tests := []struct {
		name    string
		region  string
		wantErr bool
	}{
		{"valid iad", "iad", false},
		{"valid lhr", "lhr", false},
		{"valid syd", "syd", false},
		{"invalid region", "invalid-region", true},
		{"invalid empty", "", true},
		{"invalid us-west-1", "us-west-1", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateRegion(tt.region)
			if tt.wantErr {
				assert.Error(t, err)
				assert.Equal(t, ErrInvalidRegion, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}
