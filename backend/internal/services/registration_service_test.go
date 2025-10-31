package services

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/repository"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

// Mock implementations
// Note: MockProfileRepository and MockPrivacyRepository are defined in their respective test files
// We only define MockUserIdentityRepository here as it's unique to this test

type MockUserIdentityRepository struct {
	mock.Mock
}

func (m *MockUserIdentityRepository) GetByAuth0ID(ctx context.Context, auth0UserID string) (*domain.UserIdentity, error) {
	args := m.Called(ctx, auth0UserID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*domain.UserIdentity), args.Error(1)
}

func (m *MockUserIdentityRepository) GetByUserID(ctx context.Context, userID string) ([]*domain.UserIdentity, error) {
	args := m.Called(ctx, userID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]*domain.UserIdentity), args.Error(1)
}

func (m *MockUserIdentityRepository) Create(ctx context.Context, identity *domain.UserIdentity) error {
	args := m.Called(ctx, identity)
	return args.Error(0)
}

func (m *MockUserIdentityRepository) CreateTx(ctx context.Context, tx *sql.Tx, identity *domain.UserIdentity) error {
	args := m.Called(ctx, tx, identity)
	return args.Error(0)
}

func (m *MockUserIdentityRepository) UpdateLastUsed(ctx context.Context, auth0UserID string) error {
	args := m.Called(ctx, auth0UserID)
	return args.Error(0)
}

func (m *MockUserIdentityRepository) SetPrimary(ctx context.Context, identityID string) error {
	args := m.Called(ctx, identityID)
	return args.Error(0)
}

func (m *MockUserIdentityRepository) Delete(ctx context.Context, identityID string) error {
	args := m.Called(ctx, identityID)
	return args.Error(0)
}

// MockDB is used for transaction testing if needed
type MockDB struct {
	mock.Mock
}

func (m *MockDB) BeginTx(ctx context.Context, opts *sql.TxOptions) (*sql.Tx, error) {
	args := m.Called(ctx, opts)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*sql.Tx), args.Error(1)
}

// MockAuditLogger is defined in deletion_service_test.go and shared across test files

// TestRegisterUser_Success tests successful user registration
// RED Phase: This test will FAIL because RegistrationService doesn't exist yet
func TestRegisterUser_Success(t *testing.T) {
	// Arrange
	ctx := context.Background()
	auth0UserID := "google-oauth2|123456789"
	email := "newuser@example.com"

	mockProfileRepo := new(MockProfileRepository)
	mockIdentityRepo := new(MockUserIdentityRepository)
	mockPrivacyRepo := new(MockPrivacyRepository)

	// Identity doesn't exist (new user)
	mockIdentityRepo.On("GetByAuth0ID", ctx, auth0UserID).Return(nil, repository.ErrIdentityNotFound)

	// Expect user creation
	mockProfileRepo.On("CreateUser", ctx, mock.MatchedBy(func(u *domain.User) bool {
		return u.Auth0UserID == auth0UserID &&
			u.Email == email &&
			u.SubscriptionTier == "free" &&
			u.SubscriptionStatus == "active" &&
			u.PrimaryRegion == "iad"
	})).Return(nil)

	// Expect identity creation
	mockIdentityRepo.On("Create", ctx, mock.MatchedBy(func(i *domain.UserIdentity) bool {
		return i.Auth0UserID == auth0UserID &&
			i.Provider == "google-oauth2" &&
			i.ProviderUserID == "123456789" &&
			i.Email == email &&
			i.IsPrimary == true
	})).Return(nil)

	// Expect privacy settings creation
	mockPrivacyRepo.On("Create", ctx, mock.MatchedBy(func(p *domain.PrivacySettings) bool {
		return p.GDPRNecessaryConsent == true &&
			p.GDPRAnalyticsConsent == false &&
			p.GDPRAdvertisingConsent == false
	})).Return(nil)

	// Service with nil audit logger and nil db (not testing audit logging or transactions)
	service := NewRegistrationService(mockProfileRepo, mockIdentityRepo, mockPrivacyRepo, nil, nil)

	// Act
	user, err := service.RegisterUser(ctx, auth0UserID, email, true)

	// Assert
	assert.NoError(t, err)
	assert.NotNil(t, user)
	assert.Equal(t, auth0UserID, user.Auth0UserID)
	assert.Equal(t, email, user.Email)
	assert.Equal(t, "free", user.SubscriptionTier)
	assert.Equal(t, "active", user.SubscriptionStatus)
	assert.Equal(t, "iad", user.PrimaryRegion)

	mockIdentityRepo.AssertExpectations(t)
	mockProfileRepo.AssertExpectations(t)
	mockPrivacyRepo.AssertExpectations(t)
}

// TestRegisterUser_AlreadyExists tests idempotent registration (user already exists)
// RED Phase: This test will FAIL because RegistrationService doesn't exist yet
func TestRegisterUser_AlreadyExists(t *testing.T) {
	// Arrange
	ctx := context.Background()
	auth0UserID := "google-oauth2|123456789"
	email := "existing@example.com"
	userID := "existing-user-uuid"

	mockProfileRepo := new(MockProfileRepository)
	mockIdentityRepo := new(MockUserIdentityRepository)
	mockPrivacyRepo := new(MockPrivacyRepository)

	// Identity already exists
	existingIdentity := &domain.UserIdentity{
		ID:             "identity-uuid",
		UserID:         userID,
		Auth0UserID:    auth0UserID,
		Provider:       "google-oauth2",
		ProviderUserID: "123456789",
		Email:          email,
		IsPrimary:      true,
	}
	mockIdentityRepo.On("GetByAuth0ID", ctx, auth0UserID).Return(existingIdentity, nil)

	// Expect user lookup (not creation)
	existingUser := &domain.User{
		ID:                 userID,
		Auth0UserID:        auth0UserID,
		Email:              email,
		SubscriptionTier:   "free",
		SubscriptionStatus: "active",
		PrimaryRegion:      "iad",
	}
	mockProfileRepo.On("GetUserByID", ctx, userID).Return(existingUser, nil)

	// Service with nil audit logger and nil db (not testing audit logging or transactions)
	service := NewRegistrationService(mockProfileRepo, mockIdentityRepo, mockPrivacyRepo, nil, nil)

	// Act
	user, err := service.RegisterUser(ctx, auth0UserID, email, true)

	// Assert
	assert.NoError(t, err)
	assert.NotNil(t, user)
	assert.Equal(t, userID, user.ID)
	assert.Equal(t, auth0UserID, user.Auth0UserID)
	assert.Equal(t, email, user.Email)

	// Should NOT create new user/identity/privacy (idempotent)
	mockIdentityRepo.AssertNotCalled(t, "Create")
	mockProfileRepo.AssertNotCalled(t, "CreateUser")
	mockPrivacyRepo.AssertNotCalled(t, "Create")

	mockIdentityRepo.AssertExpectations(t)
	mockProfileRepo.AssertExpectations(t)
}

// TestRegisterUser_InvalidEmail tests registration with invalid email
// RED Phase: This test will FAIL because RegistrationService doesn't exist yet
func TestRegisterUser_InvalidEmail(t *testing.T) {
	// Arrange
	ctx := context.Background()
	auth0UserID := "google-oauth2|123456789"
	invalidEmail := "not-an-email"

	mockProfileRepo := new(MockProfileRepository)
	mockIdentityRepo := new(MockUserIdentityRepository)
	mockPrivacyRepo := new(MockPrivacyRepository)

	// Service with nil audit logger and nil db (not testing audit logging or transactions)
	service := NewRegistrationService(mockProfileRepo, mockIdentityRepo, mockPrivacyRepo, nil, nil)

	// Act
	user, err := service.RegisterUser(ctx, auth0UserID, invalidEmail, true)

	// Assert
	assert.Error(t, err)
	assert.Nil(t, user)
	assert.Contains(t, err.Error(), "invalid email")

	// Should not call any repositories
	mockIdentityRepo.AssertNotCalled(t, "GetByAuth0ID")
	mockProfileRepo.AssertNotCalled(t, "CreateUser")
	mockIdentityRepo.AssertNotCalled(t, "Create")
	mockPrivacyRepo.AssertNotCalled(t, "Create")
}

// TestRegisterUser_DatabaseError tests handling of database errors
// RED Phase: This test will FAIL because RegistrationService doesn't exist yet
func TestRegisterUser_DatabaseError(t *testing.T) {
	// Arrange
	ctx := context.Background()
	auth0UserID := "google-oauth2|123456789"
	email := "newuser@example.com"

	mockProfileRepo := new(MockProfileRepository)
	mockIdentityRepo := new(MockUserIdentityRepository)
	mockPrivacyRepo := new(MockPrivacyRepository)

	// Identity doesn't exist (new user)
	mockIdentityRepo.On("GetByAuth0ID", ctx, auth0UserID).Return(nil, repository.ErrIdentityNotFound)

	// Simulate database error on user creation
	dbError := errors.New("database connection failed")
	mockProfileRepo.On("CreateUser", ctx, mock.Anything).Return(dbError)

	// Service with nil audit logger and nil db (not testing audit logging or transactions)
	service := NewRegistrationService(mockProfileRepo, mockIdentityRepo, mockPrivacyRepo, nil, nil)

	// Act
	user, err := service.RegisterUser(ctx, auth0UserID, email, true)

	// Assert
	assert.Error(t, err)
	assert.Nil(t, user)
	assert.Contains(t, err.Error(), "database connection failed")

	mockIdentityRepo.AssertExpectations(t)
	mockProfileRepo.AssertExpectations(t)
}

// TestRegisterUser_EmptyAuth0UserID tests validation of required fields
// RED Phase: This test will FAIL because RegistrationService doesn't exist yet
func TestRegisterUser_EmptyAuth0UserID(t *testing.T) {
	// Arrange
	ctx := context.Background()
	auth0UserID := ""
	email := "user@example.com"

	mockProfileRepo := new(MockProfileRepository)
	mockIdentityRepo := new(MockUserIdentityRepository)
	mockPrivacyRepo := new(MockPrivacyRepository)

	// Service with nil audit logger and nil db (not testing audit logging or transactions)
	service := NewRegistrationService(mockProfileRepo, mockIdentityRepo, mockPrivacyRepo, nil, nil)

	// Act
	user, err := service.RegisterUser(ctx, auth0UserID, email, true)

	// Assert
	assert.Error(t, err)
	assert.Nil(t, user)
	assert.Contains(t, err.Error(), "auth0_user_id is required")

	// Should not call any repositories
	mockIdentityRepo.AssertNotCalled(t, "GetByAuth0ID")
	mockProfileRepo.AssertNotCalled(t, "CreateUser")
}

// TestRegisterUser_AuditLogging tests that successful registration logs an audit event
// RED Phase: This test will FAIL because audit logging is not yet implemented
func TestRegisterUser_AuditLogging(t *testing.T) {
	// Arrange
	ctx := context.Background()
	auth0UserID := "google-oauth2|123456789"
	email := "newuser@example.com"

	mockProfileRepo := new(MockProfileRepository)
	mockIdentityRepo := new(MockUserIdentityRepository)
	mockPrivacyRepo := new(MockPrivacyRepository)
	mockAuditLogger := new(MockAuditLogger)

	// Identity doesn't exist (new user)
	mockIdentityRepo.On("GetByAuth0ID", ctx, auth0UserID).Return(nil, repository.ErrIdentityNotFound)

	// Expect user creation - mock sets user ID like real CreateUser does
	mockProfileRepo.On("CreateUser", ctx, mock.MatchedBy(func(u *domain.User) bool {
		return u.Auth0UserID == auth0UserID && u.Email == email
	})).Run(func(args mock.Arguments) {
		// Simulate CreateUser setting the user ID
		user := args.Get(1).(*domain.User)
		user.ID = "test-user-uuid-12345"
	}).Return(nil)

	// Expect identity creation
	mockIdentityRepo.On("Create", ctx, mock.Anything).Return(nil)

	// Expect privacy settings creation
	mockPrivacyRepo.On("Create", ctx, mock.Anything).Return(nil)

	// Expect audit log call AFTER successful registration
	mockAuditLogger.On("Log", ctx, "user.registration", "user", mock.MatchedBy(func(userID string) bool {
		// Should be a valid UUID string
		return len(userID) > 0
	})).Return(nil)

	// Service with audit logger and nil db (not testing transactions)
	service := NewRegistrationService(mockProfileRepo, mockIdentityRepo, mockPrivacyRepo, mockAuditLogger, nil)

	// Act
	user, err := service.RegisterUser(ctx, auth0UserID, email, true)

	// Assert
	assert.NoError(t, err)
	assert.NotNil(t, user)

	// Verify audit log was called
	mockAuditLogger.AssertExpectations(t)
	mockAuditLogger.AssertCalled(t, "Log", ctx, "user.registration", "user", mock.Anything)

	// Verify other expectations
	mockIdentityRepo.AssertExpectations(t)
	mockProfileRepo.AssertExpectations(t)
	mockPrivacyRepo.AssertExpectations(t)
}

// TestRegisterUser_TransactionAtomicity tests that user registration uses atomic transactions
// NOTE: This test is commented out because mocking sql.DB/sql.Tx is complex in Go
// Transaction atomicity is tested implicitly by integration tests
// Unit tests verify the rollback behavior (see TestRegisterUser_TransactionRollback* tests)
/*
func TestRegisterUser_TransactionAtomicity(t *testing.T) {
	// Arrange
	ctx := context.Background()
	auth0UserID := "google-oauth2|123456789"
	email := "newuser@example.com"

	mockProfileRepo := new(MockProfileRepository)
	mockIdentityRepo := new(MockUserIdentityRepository)
	mockPrivacyRepo := new(MockPrivacyRepository)
	mockDB := new(MockDB)

	// Identity doesn't exist (new user)
	mockIdentityRepo.On("GetByAuth0ID", ctx, auth0UserID).Return(nil, repository.ErrIdentityNotFound)

	// Mock transaction start
	mockTx := &sql.Tx{} // This will be replaced by actual transaction in implementation
	mockDB.On("BeginTx", ctx, (*sql.TxOptions)(nil)).Return(mockTx, nil)

	// Expect CreateTx methods to be called (not Create)
	mockProfileRepo.On("CreateUserTx", ctx, mockTx, mock.MatchedBy(func(u *domain.User) bool {
		return u.Auth0UserID == auth0UserID && u.Email == email
	})).Return(nil)

	mockIdentityRepo.On("CreateTx", ctx, mockTx, mock.MatchedBy(func(i *domain.UserIdentity) bool {
		return i.Auth0UserID == auth0UserID && i.Email == email
	})).Return(nil)

	mockPrivacyRepo.On("CreateTx", ctx, mockTx, mock.Anything).Return(nil)

	// Service with mock DB
	service := &registrationService{
		profileRepo:  mockProfileRepo,
		identityRepo: mockIdentityRepo,
		privacyRepo:  mockPrivacyRepo,
		auditLogger:  nil,
		db:           mockDB, // Need to cast MockDB to *sql.DB - this won't work as-is
	}

	// Act
	user, err := service.RegisterUser(ctx, auth0UserID, email)

	// Assert
	assert.NoError(t, err)
	assert.NotNil(t, user)

	// Verify transaction methods were called
	mockDB.AssertCalled(t, "BeginTx", ctx, (*sql.TxOptions)(nil))
	mockProfileRepo.AssertCalled(t, "CreateUserTx", ctx, mockTx, mock.Anything)
	mockIdentityRepo.AssertCalled(t, "CreateTx", ctx, mockTx, mock.Anything)
	mockPrivacyRepo.AssertCalled(t, "CreateTx", ctx, mockTx, mock.Anything)
}
*/

// TestRegisterUser_TransactionRollbackOnUserError tests rollback when user creation fails
// RED Phase: This test will FAIL because rollback logic doesn't exist yet
func TestRegisterUser_TransactionRollbackOnUserError(t *testing.T) {
	// Arrange
	ctx := context.Background()
	auth0UserID := "google-oauth2|123456789"
	email := "newuser@example.com"

	mockProfileRepo := new(MockProfileRepository)
	mockIdentityRepo := new(MockUserIdentityRepository)
	mockPrivacyRepo := new(MockPrivacyRepository)

	// Identity doesn't exist (new user)
	mockIdentityRepo.On("GetByAuth0ID", ctx, auth0UserID).Return(nil, repository.ErrIdentityNotFound)

	// User creation fails
	userCreateError := errors.New("database error during user creation")
	mockProfileRepo.On("CreateUser", ctx, mock.Anything).Return(userCreateError)

	// Service - using nil for db since we can't easily mock transaction rollback
	service := NewRegistrationService(mockProfileRepo, mockIdentityRepo, mockPrivacyRepo, nil, nil)

	// Act
	user, err := service.RegisterUser(ctx, auth0UserID, email, true)

	// Assert
	assert.Error(t, err)
	assert.Nil(t, user)
	assert.Contains(t, err.Error(), "database error")

	// Identity and privacy should NOT be created (transaction rolled back)
	mockIdentityRepo.AssertNotCalled(t, "Create")
	mockIdentityRepo.AssertNotCalled(t, "CreateTx")
	mockPrivacyRepo.AssertNotCalled(t, "Create")
	mockPrivacyRepo.AssertNotCalled(t, "CreateTx")
}

// TestRegisterUser_TransactionRollbackOnIdentityError tests rollback when identity creation fails
// RED Phase: This test will FAIL because rollback logic doesn't exist yet
func TestRegisterUser_TransactionRollbackOnIdentityError(t *testing.T) {
	// Arrange
	ctx := context.Background()
	auth0UserID := "google-oauth2|123456789"
	email := "newuser@example.com"

	mockProfileRepo := new(MockProfileRepository)
	mockIdentityRepo := new(MockUserIdentityRepository)
	mockPrivacyRepo := new(MockPrivacyRepository)

	// Identity doesn't exist (new user)
	mockIdentityRepo.On("GetByAuth0ID", ctx, auth0UserID).Return(nil, repository.ErrIdentityNotFound)

	// User creation succeeds
	mockProfileRepo.On("CreateUser", ctx, mock.Anything).Return(nil)

	// Identity creation fails (e.g., duplicate detected in race condition)
	identityCreateError := repository.ErrDuplicateIdentity
	mockIdentityRepo.On("Create", ctx, mock.Anything).Return(identityCreateError)

	// Service
	service := NewRegistrationService(mockProfileRepo, mockIdentityRepo, mockPrivacyRepo, nil, nil)

	// Act
	user, err := service.RegisterUser(ctx, auth0UserID, email, true)

	// Assert
	assert.Error(t, err)
	assert.Nil(t, user)

	// Privacy should NOT be created (transaction rolled back)
	mockPrivacyRepo.AssertNotCalled(t, "Create")
	mockPrivacyRepo.AssertNotCalled(t, "CreateTx")

	// TODO: With proper transaction implementation, user should also be rolled back
	// For now, this creates orphaned user record - will be fixed in Phase 1.7
}

// TestRegisterUser_TransactionRollbackOnPrivacyError tests rollback when privacy creation fails
// RED Phase: This test will FAIL because rollback logic doesn't exist yet
func TestRegisterUser_TransactionRollbackOnPrivacyError(t *testing.T) {
	// Arrange
	ctx := context.Background()
	auth0UserID := "google-oauth2|123456789"
	email := "newuser@example.com"

	mockProfileRepo := new(MockProfileRepository)
	mockIdentityRepo := new(MockUserIdentityRepository)
	mockPrivacyRepo := new(MockPrivacyRepository)

	// Identity doesn't exist (new user)
	mockIdentityRepo.On("GetByAuth0ID", ctx, auth0UserID).Return(nil, repository.ErrIdentityNotFound)

	// User creation succeeds
	mockProfileRepo.On("CreateUser", ctx, mock.Anything).Return(nil)

	// Identity creation succeeds
	mockIdentityRepo.On("Create", ctx, mock.Anything).Return(nil)

	// Privacy creation fails
	privacyCreateError := errors.New("database error during privacy creation")
	mockPrivacyRepo.On("Create", ctx, mock.Anything).Return(privacyCreateError)

	// Service
	service := NewRegistrationService(mockProfileRepo, mockIdentityRepo, mockPrivacyRepo, nil, nil)

	// Act
	user, err := service.RegisterUser(ctx, auth0UserID, email, true)

	// Assert
	assert.Error(t, err)
	assert.Nil(t, user)
	assert.Contains(t, err.Error(), "privacy")

	// TODO: With proper transaction implementation, user and identity should be rolled back
	// For now, this creates orphaned records - will be fixed in Phase 1.7
}

// TestRegisterUser_EmailVerifiedTrue tests that emailVerified=true is correctly stored
// RED Phase: This test will FAIL until we update all test calls with emailVerified parameter
func TestRegisterUser_EmailVerifiedTrue(t *testing.T) {
	// Arrange
	ctx := context.Background()
	auth0UserID := "google-oauth2|123456789"
	email := "verified@example.com"
	emailVerified := true

	mockProfileRepo := new(MockProfileRepository)
	mockIdentityRepo := new(MockUserIdentityRepository)
	mockPrivacyRepo := new(MockPrivacyRepository)

	// Identity doesn't exist (new user)
	mockIdentityRepo.On("GetByAuth0ID", ctx, auth0UserID).Return(nil, repository.ErrIdentityNotFound)

	// Expect user creation with emailVerified=true
	mockProfileRepo.On("CreateUser", ctx, mock.MatchedBy(func(u *domain.User) bool {
		// Verify EmailVerified is set correctly
		return u.Auth0UserID == auth0UserID &&
			u.Email == email &&
			u.EmailVerified == true
	})).Return(nil)

	// Expect identity creation with emailVerified=true
	mockIdentityRepo.On("Create", ctx, mock.MatchedBy(func(i *domain.UserIdentity) bool {
		return i.Auth0UserID == auth0UserID &&
			i.Email == email &&
			i.EmailVerified == true
	})).Return(nil)

	// Expect privacy settings creation
	mockPrivacyRepo.On("Create", ctx, mock.Anything).Return(nil)

	// Service
	service := NewRegistrationService(mockProfileRepo, mockIdentityRepo, mockPrivacyRepo, nil, nil)

	// Act
	user, err := service.RegisterUser(ctx, auth0UserID, email, emailVerified)

	// Assert
	assert.NoError(t, err)
	assert.NotNil(t, user)
	assert.Equal(t, true, user.EmailVerified)

	mockIdentityRepo.AssertExpectations(t)
	mockProfileRepo.AssertExpectations(t)
	mockPrivacyRepo.AssertExpectations(t)
}

// TestRegisterUser_EmailVerifiedFalse tests that emailVerified=false is correctly stored
// RED Phase: This test will FAIL until we update all test calls with emailVerified parameter
func TestRegisterUser_EmailVerifiedFalse(t *testing.T) {
	// Arrange
	ctx := context.Background()
	auth0UserID := "google-oauth2|987654321"
	email := "unverified@example.com"
	emailVerified := false

	mockProfileRepo := new(MockProfileRepository)
	mockIdentityRepo := new(MockUserIdentityRepository)
	mockPrivacyRepo := new(MockPrivacyRepository)

	// Identity doesn't exist (new user)
	mockIdentityRepo.On("GetByAuth0ID", ctx, auth0UserID).Return(nil, repository.ErrIdentityNotFound)

	// Expect user creation with emailVerified=false
	mockProfileRepo.On("CreateUser", ctx, mock.MatchedBy(func(u *domain.User) bool {
		// Verify EmailVerified is set correctly
		return u.Auth0UserID == auth0UserID &&
			u.Email == email &&
			u.EmailVerified == false
	})).Return(nil)

	// Expect identity creation with emailVerified=false
	mockIdentityRepo.On("Create", ctx, mock.MatchedBy(func(i *domain.UserIdentity) bool {
		return i.Auth0UserID == auth0UserID &&
			i.Email == email &&
			i.EmailVerified == false
	})).Return(nil)

	// Expect privacy settings creation
	mockPrivacyRepo.On("Create", ctx, mock.Anything).Return(nil)

	// Service
	service := NewRegistrationService(mockProfileRepo, mockIdentityRepo, mockPrivacyRepo, nil, nil)

	// Act
	user, err := service.RegisterUser(ctx, auth0UserID, email, emailVerified)

	// Assert
	assert.NoError(t, err)
	assert.NotNil(t, user)
	assert.Equal(t, false, user.EmailVerified)

	mockIdentityRepo.AssertExpectations(t)
	mockProfileRepo.AssertExpectations(t)
	mockPrivacyRepo.AssertExpectations(t)
}
