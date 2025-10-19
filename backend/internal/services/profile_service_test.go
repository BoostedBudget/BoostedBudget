package services

import (
	"context"
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

func (m *MockProfileRepository) GetUserByAuth0ID(ctx context.Context, auth0UserID string) (*domain.User, error) {
	args := m.Called(ctx, auth0UserID)
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

func (m *MockProfileRepository) UpdateUser(ctx context.Context, user *domain.User) error {
	args := m.Called(ctx, user)
	return args.Error(0)
}

func (m *MockProfileRepository) UpdateLastLogin(ctx context.Context, auth0UserID string) error {
	args := m.Called(ctx, auth0UserID)
	return args.Error(0)
}

// TestGetProfile_ExistingUser tests retrieving an existing user
func TestGetProfile_ExistingUser(t *testing.T) {
	// Setup
	mockRepo := new(MockProfileRepository)
	service := NewProfileService(mockRepo)
	ctx := context.Background()

	auth0UserID := "auth0|507f1f77bcf86cd799439011"
	email := "test@example.com"
	existingUser := &domain.User{
		ID:                 uuid.New().String(),
		Auth0UserID:        auth0UserID,
		Email:              email,
		SubscriptionTier:   "free",
		SubscriptionStatus: "active",
		PrimaryRegion:      "us-east-1",
		CreatedAt:          time.Now(),
		UpdatedAt:          time.Now(),
	}

	// Mock expectations
	mockRepo.On("GetUserByAuth0ID", ctx, auth0UserID).Return(existingUser, nil)
	mockRepo.On("UpdateLastLogin", ctx, auth0UserID).Return(nil)

	// Execute
	user, err := service.GetProfile(ctx, auth0UserID, email)

	// Assert
	assert.NoError(t, err)
	assert.NotNil(t, user)
	assert.Equal(t, auth0UserID, user.Auth0UserID)
	assert.Equal(t, email, user.Email)
	mockRepo.AssertExpectations(t)
}

// TestGetProfile_FirstLogin tests creating a new user on first login
func TestGetProfile_FirstLogin(t *testing.T) {
	// Setup
	mockRepo := new(MockProfileRepository)
	service := NewProfileService(mockRepo)
	ctx := context.Background()

	auth0UserID := "auth0|newuser123"
	email := "newuser@example.com"

	// Mock expectations
	mockRepo.On("GetUserByAuth0ID", ctx, auth0UserID).Return(nil, repository.ErrUserNotFound)
	mockRepo.On("CreateUser", ctx, mock.MatchedBy(func(u *domain.User) bool {
		return u.Auth0UserID == auth0UserID &&
			u.Email == email &&
			u.SubscriptionTier == "free" &&
			u.PrimaryRegion == "us-east-1"
	})).Return(nil)

	// Execute
	user, err := service.GetProfile(ctx, auth0UserID, email)

	// Assert
	assert.NoError(t, err)
	assert.NotNil(t, user)
	assert.Equal(t, auth0UserID, user.Auth0UserID)
	assert.Equal(t, email, user.Email)
	assert.Equal(t, "free", user.SubscriptionTier)
	assert.Equal(t, "us-east-1", user.PrimaryRegion)
	mockRepo.AssertExpectations(t)
}

// TestGetProfile_RepositoryError tests handling of repository errors
func TestGetProfile_RepositoryError(t *testing.T) {
	// Setup
	mockRepo := new(MockProfileRepository)
	service := NewProfileService(mockRepo)
	ctx := context.Background()

	auth0UserID := "auth0|507f1f77bcf86cd799439011"
	email := "test@example.com"

	// Mock expectations
	mockRepo.On("GetUserByAuth0ID", ctx, auth0UserID).Return(nil, errors.New("database error"))

	// Execute
	user, err := service.GetProfile(ctx, auth0UserID, email)

	// Assert
	assert.Error(t, err)
	assert.Nil(t, user)
	mockRepo.AssertExpectations(t)
}

// TestGetProfile_CreateUserError tests handling of create user errors
func TestGetProfile_CreateUserError(t *testing.T) {
	// Setup
	mockRepo := new(MockProfileRepository)
	service := NewProfileService(mockRepo)
	ctx := context.Background()

	auth0UserID := "auth0|newuser123"
	email := "newuser@example.com"

	// Mock expectations
	mockRepo.On("GetUserByAuth0ID", ctx, auth0UserID).Return(nil, repository.ErrUserNotFound)
	mockRepo.On("CreateUser", ctx, mock.Anything).Return(errors.New("create failed"))

	// Execute
	user, err := service.GetProfile(ctx, auth0UserID, email)

	// Assert
	assert.Error(t, err)
	assert.Nil(t, user)
	mockRepo.AssertExpectations(t)
}

// TestUpdateProfile_Success tests successful profile update
func TestUpdateProfile_Success(t *testing.T) {
	// Setup
	mockRepo := new(MockProfileRepository)
	service := NewProfileService(mockRepo)
	ctx := context.Background()

	auth0UserID := "auth0|507f1f77bcf86cd799439011"
	userID := uuid.New().String()
	existingUser := &domain.User{
		ID:                 userID,
		Auth0UserID:        auth0UserID,
		Email:              "old@example.com",
		SubscriptionTier:   "free",
		SubscriptionStatus: "active",
		PrimaryRegion:      "us-east-1",
	}

	newEmail := "new@example.com"
	req := &domain.UpdateProfileRequest{
		Email: &newEmail,
	}

	// Mock expectations
	mockRepo.On("GetUserByAuth0ID", ctx, auth0UserID).Return(existingUser, nil)
	mockRepo.On("UpdateUser", ctx, mock.MatchedBy(func(u *domain.User) bool {
		return u.Email == newEmail && u.PrimaryRegion == "us-east-1"
	})).Return(nil)

	// Execute
	user, err := service.UpdateProfile(ctx, auth0UserID, req)

	// Assert
	assert.NoError(t, err)
	assert.NotNil(t, user)
	assert.Equal(t, newEmail, user.Email)
	mockRepo.AssertExpectations(t)
}

// TestUpdateProfile_InvalidEmail tests email validation
func TestUpdateProfile_InvalidEmail(t *testing.T) {
	// Setup
	mockRepo := new(MockProfileRepository)
	service := NewProfileService(mockRepo)
	ctx := context.Background()

	auth0UserID := "auth0|507f1f77bcf86cd799439011"
	existingUser := &domain.User{
		ID:            uuid.New().String(),
		Auth0UserID:   auth0UserID,
		Email:         "valid@example.com",
		PrimaryRegion: "us-east-1",
	}

	invalidEmail := "not-an-email"
	req := &domain.UpdateProfileRequest{
		Email: &invalidEmail,
	}

	// Mock expectations
	mockRepo.On("GetUserByAuth0ID", ctx, auth0UserID).Return(existingUser, nil)

	// Execute
	user, err := service.UpdateProfile(ctx, auth0UserID, req)

	// Assert
	assert.Error(t, err)
	assert.Equal(t, ErrInvalidEmail, err)
	assert.Nil(t, user)
	mockRepo.AssertExpectations(t)
}

// TestUpdateProfile_InvalidRegion tests region validation
func TestUpdateProfile_InvalidRegion(t *testing.T) {
	// Setup
	mockRepo := new(MockProfileRepository)
	service := NewProfileService(mockRepo)
	ctx := context.Background()

	auth0UserID := "auth0|507f1f77bcf86cd799439011"
	existingUser := &domain.User{
		ID:            uuid.New().String(),
		Auth0UserID:   auth0UserID,
		Email:         "test@example.com",
		PrimaryRegion: "us-east-1",
	}

	invalidRegion := "invalid-region"
	req := &domain.UpdateProfileRequest{
		PrimaryRegion: &invalidRegion,
	}

	// Mock expectations
	mockRepo.On("GetUserByAuth0ID", ctx, auth0UserID).Return(existingUser, nil)

	// Execute
	user, err := service.UpdateProfile(ctx, auth0UserID, req)

	// Assert
	assert.Error(t, err)
	assert.Equal(t, ErrInvalidRegion, err)
	assert.Nil(t, user)
	mockRepo.AssertExpectations(t)
}

// TestUpdateProfile_ValidRegion tests updating to a valid region
func TestUpdateProfile_ValidRegion(t *testing.T) {
	// Setup
	mockRepo := new(MockProfileRepository)
	service := NewProfileService(mockRepo)
	ctx := context.Background()

	auth0UserID := "auth0|507f1f77bcf86cd799439011"
	existingUser := &domain.User{
		ID:            uuid.New().String(),
		Auth0UserID:   auth0UserID,
		Email:         "test@example.com",
		PrimaryRegion: "us-east-1",
	}

	newRegion := "eu-west-1"
	req := &domain.UpdateProfileRequest{
		PrimaryRegion: &newRegion,
	}

	// Mock expectations
	mockRepo.On("GetUserByAuth0ID", ctx, auth0UserID).Return(existingUser, nil)
	mockRepo.On("UpdateUser", ctx, mock.MatchedBy(func(u *domain.User) bool {
		return u.PrimaryRegion == newRegion
	})).Return(nil)

	// Execute
	user, err := service.UpdateProfile(ctx, auth0UserID, req)

	// Assert
	assert.NoError(t, err)
	assert.NotNil(t, user)
	assert.Equal(t, newRegion, user.PrimaryRegion)
	mockRepo.AssertExpectations(t)
}

// TestUpdateProfile_UserNotFound tests updating non-existent user
func TestUpdateProfile_UserNotFound(t *testing.T) {
	// Setup
	mockRepo := new(MockProfileRepository)
	service := NewProfileService(mockRepo)
	ctx := context.Background()

	auth0UserID := "auth0|nonexistent"
	newEmail := "new@example.com"
	req := &domain.UpdateProfileRequest{
		Email: &newEmail,
	}

	// Mock expectations
	mockRepo.On("GetUserByAuth0ID", ctx, auth0UserID).Return(nil, repository.ErrUserNotFound)

	// Execute
	user, err := service.UpdateProfile(ctx, auth0UserID, req)

	// Assert
	assert.Error(t, err)
	assert.Equal(t, repository.ErrUserNotFound, err)
	assert.Nil(t, user)
	mockRepo.AssertExpectations(t)
}

// TestUpdateProfile_UpdateError tests handling of update errors
func TestUpdateProfile_UpdateError(t *testing.T) {
	// Setup
	mockRepo := new(MockProfileRepository)
	service := NewProfileService(mockRepo)
	ctx := context.Background()

	auth0UserID := "auth0|507f1f77bcf86cd799439011"
	existingUser := &domain.User{
		ID:            uuid.New().String(),
		Auth0UserID:   auth0UserID,
		Email:         "test@example.com",
		PrimaryRegion: "us-east-1",
	}

	newEmail := "new@example.com"
	req := &domain.UpdateProfileRequest{
		Email: &newEmail,
	}

	// Mock expectations
	mockRepo.On("GetUserByAuth0ID", ctx, auth0UserID).Return(existingUser, nil)
	mockRepo.On("UpdateUser", ctx, mock.Anything).Return(errors.New("update failed"))

	// Execute
	user, err := service.UpdateProfile(ctx, auth0UserID, req)

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
		{"valid us-east-1", "us-east-1", false},
		{"valid eu-west-1", "eu-west-1", false},
		{"valid ap-southeast-1", "ap-southeast-1", false},
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
