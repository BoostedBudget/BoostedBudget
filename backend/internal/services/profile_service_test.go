package services

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/queue"
	"github.com/maxnussbaum/BoostedBudget/internal/repository"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

// TestMain initializes the domain region registry before running tests.
func TestMain(m *testing.M) {
	domain.InitRegionRegistry(
		map[string]domain.RegionInfo{
			"iad": {Code: "iad", DisplayName: "USA (Virginia)", Country: "USA", City: "Virginia", Flag: "🇺🇸", FlyRegion: "iad"},
		},
		"iad",
		"iad",
	)
	os.Exit(m.Run())
}

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

func (m *MockProfileRepository) GetUserByPaddleSubscriptionID(ctx context.Context, subscriptionID string) (*domain.User, error) {
	args := m.Called(ctx, subscriptionID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*domain.User), args.Error(1)
}

func (m *MockProfileRepository) CreateUser(ctx context.Context, user *domain.User) error {
	args := m.Called(ctx, user)
	return args.Error(0)
}

func (m *MockProfileRepository) CreateUserTx(ctx context.Context, tx pgx.Tx, user *domain.User) error {
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

// Issue #679, #680: Trial expiration - methods for trial management
func (m *MockProfileRepository) GetUsersWithExpiredTrials(ctx context.Context) ([]*domain.User, error) {
	args := m.Called(ctx)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]*domain.User), args.Error(1)
}

func (m *MockProfileRepository) GetUsersWithExpiringTrials(ctx context.Context, withinDays int) ([]*domain.User, error) {
	args := m.Called(ctx, withinDays)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]*domain.User), args.Error(1)
}

func (m *MockProfileRepository) UpdateSubscriptionStatus(ctx context.Context, userID string, status string) error {
	args := m.Called(ctx, userID, status)
	return args.Error(0)
}

func (m *MockProfileRepository) UpdateTrialStatus(ctx context.Context, userID string, status string, trialStartedAt, trialEndsAt *time.Time, hadTrial bool) error {
	args := m.Called(ctx, userID, status, trialStartedAt, trialEndsAt, hadTrial)
	return args.Error(0)
}

func (m *MockProfileRepository) UpdatePaddleCustomerID(ctx context.Context, userID, paddleCustomerID string) error {
	args := m.Called(ctx, userID, paddleCustomerID)
	return args.Error(0)
}

func (m *MockProfileRepository) UpdateSubscriptionFromPaddle(ctx context.Context, userID, tier, status string, startedAt, endsAt *time.Time, subscriptionID *string) error {
	args := m.Called(ctx, userID, tier, status, startedAt, endsAt, subscriptionID)
	return args.Error(0)
}

func (m *MockProfileRepository) GetUsersWithActivePaddleSubscriptions(ctx context.Context) ([]*domain.User, error) {
	args := m.Called(ctx)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]*domain.User), args.Error(1)
}

// Retention tracking stubs (#930)
func (m *MockProfileRepository) GetSubscriptionStatusByUserID(ctx context.Context, userID string) (string, error) {
	return "", nil
}
func (m *MockProfileRepository) SetRetentionDates(ctx context.Context, userID string, startedAt, expiresAt time.Time) error {
	return nil
}
func (m *MockProfileRepository) ClearRetentionDates(ctx context.Context, userID string) error {
	return nil
}
func (m *MockProfileRepository) SetRetentionNotice30dSent(ctx context.Context, userID string) error {
	return nil
}
func (m *MockProfileRepository) SetRetentionNotice7dSent(ctx context.Context, userID string) error {
	return nil
}
func (m *MockProfileRepository) GetUsersNeedingRetention30dNotice(ctx context.Context) ([]*domain.User, error) {
	return nil, nil
}
func (m *MockProfileRepository) GetUsersNeedingRetention7dNotice(ctx context.Context) ([]*domain.User, error) {
	return nil, nil
}
func (m *MockProfileRepository) GetUsersWithExpiredRetention(ctx context.Context) ([]*domain.User, error) {
	return nil, nil
}

func (m *MockProfileRepository) GetUserByEmail(ctx context.Context, email string) (*domain.User, error) {
	// If no expectation registered, return safe default (no pre-encryption user found).
	// This avoids needing to add GetUserByEmail mocks to all tests that don't care about
	// the encryption fallback path (#1001).
	for _, call := range m.ExpectedCalls {
		if call.Method == "GetUserByEmail" {
			args := m.Called(ctx, email)
			if args.Get(0) == nil {
				return nil, args.Error(1)
			}
			return args.Get(0).(*domain.User), args.Error(1)
		}
	}
	return nil, repository.ErrUserNotFound
}

func (m *MockProfileRepository) UpdateUserEmailTx(ctx context.Context, tx pgx.Tx, userID, plainEmail string) error {
	args := m.Called(ctx, tx, userID, plainEmail)
	return args.Error(0)
}

// TestGetProfile_ExistingUser tests retrieving an existing user
// NOTE: After PR #197, GetProfile now receives internal user UUID, not Clerk ID
func TestGetProfile_ExistingUser(t *testing.T) {
	// Setup
	mockRepo := new(MockProfileRepository)
	service := NewProfileService(NewProfileServiceDeps{Repo: mockRepo})
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
	service := NewProfileService(NewProfileServiceDeps{Repo: mockRepo})
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
	service := NewProfileService(NewProfileServiceDeps{Repo: mockRepo})
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
	service := NewProfileService(NewProfileServiceDeps{Repo: mockRepo})
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

// TestUpdateProfile_Success tests successful profile update with email change
// (transactional path). After the refactor, email changes go through Begin/Commit.
// NOTE: After PR #197, UpdateProfile now receives internal user UUID, not Clerk ID
func TestUpdateProfile_Success(t *testing.T) {
	// Setup
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
	updatedUser := &domain.User{
		ID:                 userID,
		ClerkUserID:        clerkUserID,
		Email:              newEmail,
		EmailVerified:      false,
		SubscriptionTier:   "free",
		SubscriptionStatus: "active",
		PrimaryRegion:      "iad",
	}

	mockRepo := new(MockProfileRepository)
	mockIdentityRepo := new(MockUserIdentityRepository)
	mockDB := new(mockTxBeginner)
	tx := new(mockTx)
	mockJQ := new(mockProfileJobQueue)

	mockRepo.On("GetUserByID", ctx, userID).Return(existingUser, nil).Once()
	mockDB.On("Begin", ctx).Return(tx, nil)
	mockRepo.On("UpdateUserEmailTx", ctx, tx, userID, newEmail).Return(nil)
	mockIdentityRepo.On("UpdatePrimaryIdentityEmailTx", ctx, tx, userID, newEmail).Return(nil)
	tx.On("Commit", ctx).Return(nil)
	mockJQ.On("Enqueue", ctx, mock.Anything).Return(&queue.JobResult{ID: 1, Kind: "email_change_propagation"}, nil)
	mockRepo.On("GetUserByID", ctx, userID).Return(updatedUser, nil).Once()

	service := NewProfileService(NewProfileServiceDeps{
		Repo:         mockRepo,
		IdentityRepo: mockIdentityRepo,
		DB:           mockDB,
		JobQueue:     mockJQ,
	})

	req := &domain.UpdateProfileRequest{Email: &newEmail}

	// Execute
	user, err := service.UpdateProfile(ctx, userID, req)

	// Assert
	assert.NoError(t, err)
	assert.NotNil(t, user)
	assert.Equal(t, newEmail, user.Email)
	mockRepo.AssertExpectations(t)
	mockDB.AssertExpectations(t)
	tx.AssertExpectations(t)
}

// TestUpdateProfile_InvalidEmail tests email validation
// NOTE: After PR #197, UpdateProfile now receives internal user UUID, not Clerk ID
func TestUpdateProfile_InvalidEmail(t *testing.T) {
	// Setup
	mockRepo := new(MockProfileRepository)
	service := NewProfileService(NewProfileServiceDeps{Repo: mockRepo})
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
	service := NewProfileService(NewProfileServiceDeps{Repo: mockRepo})
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
	service := NewProfileService(NewProfileServiceDeps{Repo: mockRepo})
	ctx := context.Background()

	userID := uuid.New().String()
	clerkUserID := "user_507f1f77bcf86cd799439011"
	existingUser := &domain.User{
		ID:            userID,
		ClerkUserID:   clerkUserID,
		Email:         "test@example.com",
		PrimaryRegion: "iad",
	}

	newRegion := "iad"
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
	service := NewProfileService(NewProfileServiceDeps{Repo: mockRepo})
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

// TestUpdateProfile_UpdateError tests handling of update errors during email change
// (transactional path: UpdateUserEmailTx fails).
// NOTE: After PR #197, UpdateProfile now receives internal user UUID, not Clerk ID
func TestUpdateProfile_UpdateError(t *testing.T) {
	// Setup
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

	mockRepo := new(MockProfileRepository)
	mockIdentityRepo := new(MockUserIdentityRepository)
	mockDB := new(mockTxBeginner)
	tx := new(mockTx)
	mockJQ := new(mockProfileJobQueue)

	mockRepo.On("GetUserByID", ctx, userID).Return(existingUser, nil)
	mockDB.On("Begin", ctx).Return(tx, nil)
	mockRepo.On("UpdateUserEmailTx", ctx, tx, userID, newEmail).Return(errors.New("update failed"))

	service := NewProfileService(NewProfileServiceDeps{
		Repo:         mockRepo,
		IdentityRepo: mockIdentityRepo,
		DB:           mockDB,
		JobQueue:     mockJQ,
	})

	req := &domain.UpdateProfileRequest{Email: &newEmail}

	// Execute
	user, err := service.UpdateProfile(ctx, userID, req)

	// Assert
	assert.Error(t, err)
	assert.Nil(t, user)
	mockRepo.AssertExpectations(t)
	mockDB.AssertExpectations(t)
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
		{"lhr now invalid", "lhr", true},
		{"syd now invalid", "syd", true},
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

// ---------------------------------------------------------------------------
// Mocks for transactional email-change tests
// ---------------------------------------------------------------------------

// mockTxBeginner implements TxBeginner for testing.
type mockTxBeginner struct {
	mock.Mock
}

func (m *mockTxBeginner) Begin(ctx context.Context) (pgx.Tx, error) {
	args := m.Called(ctx)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(pgx.Tx), args.Error(1)
}

// mockTx implements pgx.Tx for testing. Only Commit and Rollback carry
// test-relevant behaviour; everything else panics (not exercised).
type mockTx struct {
	mock.Mock
}

func (m *mockTx) Begin(ctx context.Context) (pgx.Tx, error) { panic("not implemented") }
func (m *mockTx) Commit(ctx context.Context) error {
	args := m.Called(ctx)
	return args.Error(0)
}
func (m *mockTx) Rollback(ctx context.Context) error                     { return nil }
func (m *mockTx) CopyFrom(ctx context.Context, tableName pgx.Identifier, columnNames []string, rowSrc pgx.CopyFromSource) (int64, error) {
	panic("not implemented")
}
func (m *mockTx) SendBatch(ctx context.Context, b *pgx.Batch) pgx.BatchResults {
	panic("not implemented")
}
func (m *mockTx) LargeObjects() pgx.LargeObjects     { panic("not implemented") }
func (m *mockTx) Prepare(ctx context.Context, name, sql string) (*pgconn.StatementDescription, error) {
	panic("not implemented")
}
func (m *mockTx) Exec(ctx context.Context, sql string, arguments ...any) (pgconn.CommandTag, error) {
	panic("not implemented")
}
func (m *mockTx) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	panic("not implemented")
}
func (m *mockTx) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	panic("not implemented")
}
func (m *mockTx) Conn() *pgx.Conn { panic("not implemented") }

// mockProfileJobQueue implements queue.JobQueue for profile tests.
// We use a separate name to avoid conflicts with MockJobQueue in account_service_test.go.
type mockProfileJobQueue struct {
	mock.Mock
}

func (m *mockProfileJobQueue) Enqueue(ctx context.Context, job queue.Job, opts ...queue.EnqueueOption) (*queue.JobResult, error) {
	args := m.Called(ctx, job)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*queue.JobResult), args.Error(1)
}

func (m *mockProfileJobQueue) EnqueueTx(ctx context.Context, tx pgx.Tx, job queue.Job, opts ...queue.EnqueueOption) (*queue.JobResult, error) {
	args := m.Called(ctx, tx, job)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*queue.JobResult), args.Error(1)
}

func (m *mockProfileJobQueue) EnqueueMany(ctx context.Context, jobList []queue.Job, opts ...queue.EnqueueOption) ([]*queue.JobResult, error) {
	args := m.Called(ctx, jobList)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]*queue.JobResult), args.Error(1)
}

func (m *mockProfileJobQueue) EnqueueManyTx(ctx context.Context, tx pgx.Tx, jobList []queue.Job, opts ...queue.EnqueueOption) ([]*queue.JobResult, error) {
	args := m.Called(ctx, tx, jobList)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]*queue.JobResult), args.Error(1)
}

// ---------------------------------------------------------------------------
// Email-change transaction tests
// ---------------------------------------------------------------------------

// TestUpdateProfile_EmailChange_PropagatesAndResetsVerified verifies that when
// the email changes, the service updates both users and user_identities inside
// a transaction and enqueues an email-change propagation job.
func TestUpdateProfile_EmailChange_PropagatesAndResetsVerified(t *testing.T) {
	ctx := context.Background()
	userID := uuid.New().String()
	clerkUserID := "user_abc123"
	oldEmail := "old@example.com"
	newEmail := "new@example.com"

	existingUser := &domain.User{
		ID:                 userID,
		ClerkUserID:        clerkUserID,
		Email:              oldEmail,
		EmailVerified:      true,
		SubscriptionTier:   "premium",
		SubscriptionStatus: "active",
		PrimaryRegion:      "iad",
	}
	updatedUser := &domain.User{
		ID:                 userID,
		ClerkUserID:        clerkUserID,
		Email:              newEmail,
		EmailVerified:      false, // reset by UpdateUserEmailTx
		SubscriptionTier:   "premium",
		SubscriptionStatus: "active",
		PrimaryRegion:      "iad",
	}

	mockRepo := new(MockProfileRepository)
	mockIdentityRepo := new(MockUserIdentityRepository)
	mockDB := new(mockTxBeginner)
	tx := new(mockTx)
	mockJQ := new(mockProfileJobQueue)

	// First GetUserByID returns the existing user (before update)
	mockRepo.On("GetUserByID", ctx, userID).Return(existingUser, nil).Once()
	// Begin returns our mock transaction
	mockDB.On("Begin", ctx).Return(tx, nil)
	// Transactional updates
	mockRepo.On("UpdateUserEmailTx", ctx, tx, userID, newEmail).Return(nil)
	mockIdentityRepo.On("UpdatePrimaryIdentityEmailTx", ctx, tx, userID, newEmail).Return(nil)
	tx.On("Commit", ctx).Return(nil)
	// Enqueue propagation job
	mockJQ.On("Enqueue", ctx, mock.MatchedBy(func(job interface{}) bool {
		args, ok := job.(emailChangePropagationArgs)
		return ok && args.UserID == userID && args.ClerkUserID == clerkUserID
	})).Return(&queue.JobResult{ID: 1, Kind: "email_change_propagation"}, nil)
	// Second GetUserByID returns the committed state
	mockRepo.On("GetUserByID", ctx, userID).Return(updatedUser, nil).Once()

	service := NewProfileService(NewProfileServiceDeps{
		Repo:         mockRepo,
		IdentityRepo: mockIdentityRepo,
		DB:           mockDB,
		JobQueue:     mockJQ,
	})

	req := &domain.UpdateProfileRequest{Email: &newEmail}
	result, err := service.UpdateProfile(ctx, userID, req)

	assert.NoError(t, err)
	assert.NotNil(t, result)
	assert.Equal(t, newEmail, result.Email)
	assert.False(t, result.EmailVerified)

	mockRepo.AssertExpectations(t)
	mockIdentityRepo.AssertExpectations(t)
	mockDB.AssertExpectations(t)
	tx.AssertExpectations(t)
	mockJQ.AssertExpectations(t)
}

// TestUpdateProfile_EmailUnchanged_NoJobEnqueued verifies that when the
// requested email matches the current email, no transaction is started and
// no propagation job is enqueued.
func TestUpdateProfile_EmailUnchanged_NoJobEnqueued(t *testing.T) {
	ctx := context.Background()
	userID := uuid.New().String()
	sameEmail := "same@example.com"

	existingUser := &domain.User{
		ID:                 userID,
		ClerkUserID:        "user_xyz",
		Email:              sameEmail,
		EmailVerified:      true,
		SubscriptionTier:   "premium",
		SubscriptionStatus: "active",
		PrimaryRegion:      "iad",
	}

	mockRepo := new(MockProfileRepository)
	mockDB := new(mockTxBeginner)
	mockJQ := new(mockProfileJobQueue)

	mockRepo.On("GetUserByID", ctx, userID).Return(existingUser, nil)
	// Email unchanged → simple UpdateUser path
	mockRepo.On("UpdateUser", ctx, mock.MatchedBy(func(u *domain.User) bool {
		return u.Email == sameEmail
	})).Return(nil)

	service := NewProfileService(NewProfileServiceDeps{
		Repo:     mockRepo,
		DB:       mockDB,
		JobQueue: mockJQ,
	})

	req := &domain.UpdateProfileRequest{Email: &sameEmail}
	result, err := service.UpdateProfile(ctx, userID, req)

	assert.NoError(t, err)
	assert.NotNil(t, result)
	// Begin should never have been called
	mockDB.AssertNotCalled(t, "Begin", mock.Anything)
	// Enqueue should never have been called
	mockJQ.AssertNotCalled(t, "Enqueue", mock.Anything, mock.Anything)
	mockRepo.AssertExpectations(t)
}

// TestUpdateProfile_DuplicateEmail_ReturnsError verifies that when
// UpdateUserEmailTx returns ErrDuplicateEmail, the error propagates and
// no propagation job is enqueued.
func TestUpdateProfile_DuplicateEmail_ReturnsError(t *testing.T) {
	ctx := context.Background()
	userID := uuid.New().String()
	clerkUserID := "user_dup"
	newEmail := "taken@example.com"

	existingUser := &domain.User{
		ID:                 userID,
		ClerkUserID:        clerkUserID,
		Email:              "original@example.com",
		EmailVerified:      true,
		SubscriptionTier:   "premium",
		SubscriptionStatus: "active",
		PrimaryRegion:      "iad",
	}

	mockRepo := new(MockProfileRepository)
	mockIdentityRepo := new(MockUserIdentityRepository)
	mockDB := new(mockTxBeginner)
	tx := new(mockTx)
	mockJQ := new(mockProfileJobQueue)

	mockRepo.On("GetUserByID", ctx, userID).Return(existingUser, nil)
	mockDB.On("Begin", ctx).Return(tx, nil)
	mockRepo.On("UpdateUserEmailTx", ctx, tx, userID, newEmail).Return(repository.ErrDuplicateEmail)

	service := NewProfileService(NewProfileServiceDeps{
		Repo:         mockRepo,
		IdentityRepo: mockIdentityRepo,
		DB:           mockDB,
		JobQueue:     mockJQ,
	})

	req := &domain.UpdateProfileRequest{Email: &newEmail}
	result, err := service.UpdateProfile(ctx, userID, req)

	assert.Nil(t, result)
	assert.Error(t, err)
	assert.True(t, errors.Is(err, repository.ErrDuplicateEmail))
	// Enqueue should never have been called
	mockJQ.AssertNotCalled(t, "Enqueue", mock.Anything, mock.Anything)
	mockRepo.AssertExpectations(t)
	mockDB.AssertExpectations(t)
}
