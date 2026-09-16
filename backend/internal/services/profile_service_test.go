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
	"github.com/maxnussbaum/BoostedBudget/internal/database"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/queue"
	"github.com/maxnussbaum/BoostedBudget/internal/repository"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

// TestMain initializes the domain region registry before running tests, then tears down the
// PostHog erasure obligations this package's DeleteUserCascade fixtures leave behind.
func TestMain(m *testing.M) {
	domain.InitRegionRegistry(
		map[string]domain.RegionInfo{
			"iad": {Code: "iad", DisplayName: "USA (Virginia)", Country: "USA", City: "Virginia", Flag: "🇺🇸", FlyRegion: "iad"},
			"ewr": {Code: "ewr", DisplayName: "USA (New Jersey)", Country: "USA", City: "New Jersey", Flag: "🇺🇸", FlyRegion: "ewr"},
		},
		"iad",
		"iad",
	)
	code := m.Run()
	cleanupPostHogErasureOutbox()
	os.Exit(code)
}

// cleanupPostHogErasureOutbox removes the erasure obligations left behind by this package's
// DeleteUserCascade fixtures. posthog_erasure_outbox deliberately has no users FK, so nothing
// cascades these rows; without this teardown the NEXT `go test ./internal/...` run fails, because
// internal/database runs BEFORE internal/services and its 000211 rollback guard refuses to drop
// the table while obligations exist. The repository package's teardown cannot protect it for the
// same ordering reason. Best-effort: a missing or unreachable DATABASE_URL must not fail the
// package.
func cleanupPostHogErasureOutbox() {
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		return
	}
	ctx := context.Background()
	pool, err := database.NewPoolFromURL(ctx, dsn)
	if err != nil {
		return
	}
	defer pool.Close()
	_, _ = pool.Exec(ctx, "DELETE FROM posthog_erasure_operator_audit")
	_, _ = pool.Exec(ctx, "DELETE FROM posthog_erasure_outbox")
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

// RecordDeletionResidue (Issue #1504) records that a deletion performed an irreversible external
// effect and then did not complete. See repository.ProfileRepository for the contract.
func (m *MockProfileRepository) RecordDeletionResidue(ctx context.Context, userID string, effect repository.DeletionResidueEffect) error {
	args := m.Called(ctx, userID, effect)
	return args.Error(0)
}

func (m *MockProfileRepository) DeleteUserCascade(ctx context.Context, userID string, expectedDeletionRequestedAt *time.Time) (bool, []string, error) {
	args := m.Called(ctx, userID, expectedDeletionRequestedAt)
	return args.Bool(0), nil, args.Error(1)
}

// CountUncleanedLinkedItems — Issue #1494. Only DeletionService's Plaid-less branch calls this; these
// tests never reach it, so this is a plain stub rather than a testify expectation (which would panic
// unmatched).
func (m *MockProfileRepository) CountUncleanedLinkedItems(ctx context.Context, userID string) (int, error) {
	return 0, nil
}

// HouseholdOwnerBlocksDeletion — round 15, finding D2. Only DeletionService's pre-cleanup
// precondition calls this; these tests never reach it, so it is a plain stub for the same reason as
// the one above. It returns FALSE (nothing blocks) so a test that ever DID reach it would exercise
// the proceed path rather than an unexplained abort.
func (m *MockProfileRepository) HouseholdOwnerBlocksDeletion(ctx context.Context, userID string) (bool, error) {
	return false, nil
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
func (m *MockProfileRepository) GetEffectiveSubscriptionStatusByUserID(ctx context.Context, userID string) (string, error) {
	return "", nil
}
func (m *MockProfileRepository) SetRetentionDates(ctx context.Context, userID string, guard repository.RetentionClockGuard, retentionMonths int) error {
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
func (m *MockProfileRepository) GetUsersWithExpiredManualTierOverride(ctx context.Context) ([]*domain.User, error) {
	return nil, nil
}

// Dunning methods (#898)
func (m *MockProfileRepository) SetPastDueStartedAt(ctx context.Context, userID string, t time.Time) error {
	return nil
}
func (m *MockProfileRepository) ClearPastDueStartedAt(ctx context.Context, userID string) error {
	return nil
}
func (m *MockProfileRepository) GetUsersInPastDue(ctx context.Context) ([]domain.User, error) {
	return []domain.User{}, nil
}

// GetUsersNeedingRetentionClockRepair, RepairRetentionClock (Issue #1413 H5 residual) are not
// exercised by these profile-service tests; stubbed to satisfy the widened ProfileRepository
// interface.
func (m *MockProfileRepository) GetUsersNeedingRetentionClockRepair(ctx context.Context) ([]*domain.User, error) {
	return []*domain.User{}, nil
}

func (m *MockProfileRepository) RepairRetentionClock(ctx context.Context, userID string, startedAt time.Time, retentionMonths int) (bool, error) {
	return false, nil
}

// GetUsersNeedingPlaidDisconnectRepair (Issue #1413 round-7 grace-window repair path) is not
// exercised by these profile-service tests; stubbed to satisfy the widened ProfileRepository
// interface.
func (m *MockProfileRepository) GetUsersNeedingPlaidDisconnectRepair(ctx context.Context, gracePeriod time.Duration) ([]*domain.User, error) {
	return []*domain.User{}, nil
}

func (m *MockProfileRepository) ClaimPlaidDisconnectIfPastDue(ctx context.Context, userID string) (bool, error) {
	return false, nil
}

func (m *MockProfileRepository) ExpireTrialIfNoOverride(ctx context.Context, userID string) (bool, error) {
	return false, nil
}

// CountRetentionDeletionsWithheld is Issue #1569 T1's withholding-count read. Never exercised by
// this suite: it exists so MockProfileRepository keeps satisfying repository.ProfileRepository.
func (m *MockProfileRepository) CountRetentionDeletionsWithheld(ctx context.Context) (repository.RetentionWithholdingCounts, error) {
	return repository.RetentionWithholdingCounts{}, nil
}

// ClaimForDeletion (Issue #1413 Codex review round 3) is not exercised by these profile-service
// tests; stubbed to satisfy the widened ProfileRepository interface.
func (m *MockProfileRepository) ClaimForDeletion(ctx context.Context, userID string, kind repository.DeletionClaimKind) (bool, error) {
	return false, nil
}

// DeletionStillAuthorised (ExecuteDeletion's use-time authorisation recheck) is likewise not
// exercised by these profile-service tests. It returns TRUE — matching ClaimForDeletion's stub
// returning false, both are "whatever keeps this unreachable path uninteresting" — so a future test
// that did reach it would take the proceed path rather than be silently short-circuited.
func (m *MockProfileRepository) DeletionStillAuthorised(ctx context.Context, userID string, kind repository.DeletionClaimKind, expectedDeletionRequestedAt *time.Time) (bool, error) {
	return true, nil
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

// TestUpdateProfile_EmailAndRegionChange verifies that when both email and region change
// simultaneously, the region UpdateUser call uses the re-fetched (new) email, not the
// stale pre-transaction email. Regression test for the stale-email overwrite bug.
func TestUpdateProfile_EmailAndRegionChange(t *testing.T) {
	ctx := context.Background()
	userID := uuid.New().String()
	clerkUserID := "user_abc"

	oldEmail := "old@example.com"
	newEmail := "new@example.com"
	newRegion := "ewr"

	existingUser := &domain.User{
		ID:            userID,
		ClerkUserID:   clerkUserID,
		Email:         oldEmail,
		PrimaryRegion: "iad",
	}
	// After the email transaction commits, the DB reflects new email + email_verified=false.
	afterEmailCommit := &domain.User{
		ID:            userID,
		ClerkUserID:   clerkUserID,
		Email:         newEmail,
		EmailVerified: false,
		PrimaryRegion: "iad",
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
	// Re-fetch after email commit — must happen BEFORE region UpdateUser.
	mockRepo.On("GetUserByID", ctx, userID).Return(afterEmailCommit, nil).Once()
	// UpdateUser for region change must carry the NEW email, not the old one.
	mockRepo.On("UpdateUser", ctx, mock.MatchedBy(func(u *domain.User) bool {
		return u.Email == newEmail && u.PrimaryRegion == newRegion
	})).Return(nil)
	mockJQ.On("Enqueue", ctx, mock.Anything).Return(&queue.JobResult{ID: 1, Kind: "email_change_propagation"}, nil)

	service := NewProfileService(NewProfileServiceDeps{
		Repo:         mockRepo,
		IdentityRepo: mockIdentityRepo,
		DB:           mockDB,
		JobQueue:     mockJQ,
	})

	req := &domain.UpdateProfileRequest{Email: &newEmail, PrimaryRegion: &newRegion}

	user, err := service.UpdateProfile(ctx, userID, req)

	assert.NoError(t, err)
	assert.Equal(t, newEmail, user.Email)
	assert.Equal(t, newRegion, user.PrimaryRegion)
	mockRepo.AssertExpectations(t)
	mockDB.AssertExpectations(t)
	tx.AssertExpectations(t)
	mockJQ.AssertExpectations(t)
}

// TestUpdateProfile_NilJobQueue verifies that email change succeeds gracefully
// when jobQueue is nil (e.g., River not configured because Plaid is disabled).
func TestUpdateProfile_NilJobQueue(t *testing.T) {
	ctx := context.Background()
	userID := uuid.New().String()
	clerkUserID := "user_abc"
	newEmail := "new@example.com"

	existingUser := &domain.User{
		ID:            userID,
		ClerkUserID:   clerkUserID,
		Email:         "old@example.com",
		PrimaryRegion: "iad",
	}
	updatedUser := &domain.User{
		ID:            userID,
		ClerkUserID:   clerkUserID,
		Email:         newEmail,
		EmailVerified: false,
		PrimaryRegion: "iad",
	}

	mockRepo := new(MockProfileRepository)
	mockIdentityRepo := new(MockUserIdentityRepository)
	mockDB := new(mockTxBeginner)
	tx := new(mockTx)

	mockRepo.On("GetUserByID", ctx, userID).Return(existingUser, nil).Once()
	mockDB.On("Begin", ctx).Return(tx, nil)
	mockRepo.On("UpdateUserEmailTx", ctx, tx, userID, newEmail).Return(nil)
	mockIdentityRepo.On("UpdatePrimaryIdentityEmailTx", ctx, tx, userID, newEmail).Return(nil)
	tx.On("Commit", ctx).Return(nil)
	mockRepo.On("GetUserByID", ctx, userID).Return(updatedUser, nil).Once()

	service := NewProfileService(NewProfileServiceDeps{
		Repo:         mockRepo,
		IdentityRepo: mockIdentityRepo,
		DB:           mockDB,
		JobQueue:     nil, // explicitly nil — no panic expected
	})

	req := &domain.UpdateProfileRequest{Email: &newEmail}

	user, err := service.UpdateProfile(ctx, userID, req)

	assert.NoError(t, err)
	assert.Equal(t, newEmail, user.Email)
	mockRepo.AssertExpectations(t)
	mockDB.AssertExpectations(t)
	tx.AssertExpectations(t)
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
func (m *mockTx) Rollback(ctx context.Context) error { return nil }
func (m *mockTx) CopyFrom(ctx context.Context, tableName pgx.Identifier, columnNames []string, rowSrc pgx.CopyFromSource) (int64, error) {
	panic("not implemented")
}
func (m *mockTx) SendBatch(ctx context.Context, b *pgx.Batch) pgx.BatchResults {
	panic("not implemented")
}
func (m *mockTx) LargeObjects() pgx.LargeObjects { panic("not implemented") }
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

// TestUpdateProfile_ValidTimezone tests that when only Timezone is provided (no source),
// TimezoneSource defaults to "user" (manual selection assumed).
func TestUpdateProfile_ValidTimezone(t *testing.T) {
	mockRepo := new(MockProfileRepository)
	service := NewProfileService(NewProfileServiceDeps{Repo: mockRepo})
	ctx := context.Background()

	userID := uuid.New().String()
	existingUser := &domain.User{
		ID:             userID,
		ClerkUserID:    "user_tz1",
		Email:          "tz@example.com",
		PrimaryRegion:  "iad",
		Timezone:       "UTC",
		TimezoneSource: "default",
	}

	tz := "America/Los_Angeles"
	req := &domain.UpdateProfileRequest{Timezone: &tz}

	mockRepo.On("GetUserByID", ctx, userID).Return(existingUser, nil)
	mockRepo.On("UpdateUser", ctx, mock.MatchedBy(func(u *domain.User) bool {
		return u.Timezone == "America/Los_Angeles" && u.TimezoneSource == "user"
	})).Return(nil)

	user, err := service.UpdateProfile(ctx, userID, req)

	assert.NoError(t, err)
	assert.NotNil(t, user)
	assert.Equal(t, "America/Los_Angeles", user.Timezone)
	assert.Equal(t, "user", user.TimezoneSource)
	mockRepo.AssertExpectations(t)
}

// TestUpdateProfile_InvalidTimezone tests that an invalid IANA timezone is rejected.
func TestUpdateProfile_InvalidTimezone(t *testing.T) {
	mockRepo := new(MockProfileRepository)
	service := NewProfileService(NewProfileServiceDeps{Repo: mockRepo})
	ctx := context.Background()

	userID := uuid.New().String()
	existingUser := &domain.User{
		ID:            userID,
		ClerkUserID:   "user_tz2",
		Email:         "tz@example.com",
		PrimaryRegion: "iad",
	}

	bad := "not/a/timezone"
	req := &domain.UpdateProfileRequest{Timezone: &bad}

	mockRepo.On("GetUserByID", ctx, userID).Return(existingUser, nil)

	user, err := service.UpdateProfile(ctx, userID, req)

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid timezone")
	assert.Nil(t, user)
	mockRepo.AssertExpectations(t)
}

// TestUpdateProfile_AutoCaptureCombined tests the browser auto-capture path where
// both Timezone and TimezoneSource:"auto" are sent together. The backend must NOT
// coerce the source to "user" — the "auto" value must be preserved.
func TestUpdateProfile_AutoCaptureCombined(t *testing.T) {
	mockRepo := new(MockProfileRepository)
	service := NewProfileService(NewProfileServiceDeps{Repo: mockRepo})
	ctx := context.Background()

	userID := uuid.New().String()
	existingUser := &domain.User{
		ID:             userID,
		ClerkUserID:    "user_tz_auto",
		Email:          "tz@example.com",
		PrimaryRegion:  "iad",
		Timezone:       "UTC",
		TimezoneSource: "default",
	}

	tz := "America/Chicago"
	src := "auto"
	req := &domain.UpdateProfileRequest{Timezone: &tz, TimezoneSource: &src}

	mockRepo.On("GetUserByID", ctx, userID).Return(existingUser, nil)
	mockRepo.On("UpdateUser", ctx, mock.MatchedBy(func(u *domain.User) bool {
		return u.Timezone == "America/Chicago" && u.TimezoneSource == "auto"
	})).Return(nil)

	user, err := service.UpdateProfile(ctx, userID, req)

	assert.NoError(t, err)
	assert.NotNil(t, user)
	assert.Equal(t, "America/Chicago", user.Timezone)
	// "auto" source must survive — it is the correct value for browser auto-detect.
	assert.Equal(t, "auto", user.TimezoneSource)
	mockRepo.AssertExpectations(t)
}

// TestUpdateProfile_AutoCapture tests that TimezoneSource "auto" is accepted when
// no explicit Timezone is provided (browser auto-capture path).
func TestUpdateProfile_AutoCapture(t *testing.T) {
	mockRepo := new(MockProfileRepository)
	service := NewProfileService(NewProfileServiceDeps{Repo: mockRepo})
	ctx := context.Background()

	userID := uuid.New().String()
	existingUser := &domain.User{
		ID:             userID,
		ClerkUserID:    "user_tz3",
		Email:          "tz@example.com",
		PrimaryRegion:  "iad",
		Timezone:       "UTC",
		TimezoneSource: "default",
	}

	src := "auto"
	req := &domain.UpdateProfileRequest{TimezoneSource: &src}

	mockRepo.On("GetUserByID", ctx, userID).Return(existingUser, nil)
	mockRepo.On("UpdateUser", ctx, mock.MatchedBy(func(u *domain.User) bool {
		return u.TimezoneSource == "auto"
	})).Return(nil)

	user, err := service.UpdateProfile(ctx, userID, req)

	assert.NoError(t, err)
	assert.NotNil(t, user)
	assert.Equal(t, "auto", user.TimezoneSource)
	mockRepo.AssertExpectations(t)
}

// TestUpdateProfile_InvalidTimezoneSource tests that an unrecognised TimezoneSource value
// is rejected.
func TestUpdateProfile_InvalidTimezoneSource(t *testing.T) {
	mockRepo := new(MockProfileRepository)
	service := NewProfileService(NewProfileServiceDeps{Repo: mockRepo})
	ctx := context.Background()

	userID := uuid.New().String()
	existingUser := &domain.User{
		ID:            userID,
		ClerkUserID:   "user_tz4",
		Email:         "tz@example.com",
		PrimaryRegion: "iad",
	}

	bad := "admin_override"
	req := &domain.UpdateProfileRequest{TimezoneSource: &bad}

	mockRepo.On("GetUserByID", ctx, userID).Return(existingUser, nil)

	user, err := service.UpdateProfile(ctx, userID, req)

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid timezone_source")
	assert.Nil(t, user)
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
