package services

import (
	"context"
	"testing"
	"time"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// MockSavingsGoalRepository is a mock implementation of repository.SavingsGoalRepository
type MockSavingsGoalRepository struct {
	mock.Mock
}

func (m *MockSavingsGoalRepository) Create(ctx context.Context, goal *domain.SavingsGoal) error {
	args := m.Called(ctx, goal)
	return args.Error(0)
}

func (m *MockSavingsGoalRepository) GetByID(ctx context.Context, id string) (*domain.SavingsGoal, error) {
	args := m.Called(ctx, id)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*domain.SavingsGoal), args.Error(1)
}

func (m *MockSavingsGoalRepository) GetByUserID(ctx context.Context, userID string) ([]*domain.SavingsGoal, error) {
	args := m.Called(ctx, userID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]*domain.SavingsGoal), args.Error(1)
}

func (m *MockSavingsGoalRepository) GetActiveByUserID(ctx context.Context, userID string) ([]*domain.SavingsGoal, error) {
	args := m.Called(ctx, userID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]*domain.SavingsGoal), args.Error(1)
}

func (m *MockSavingsGoalRepository) GetByName(ctx context.Context, userID, name string) (*domain.SavingsGoal, error) {
	args := m.Called(ctx, userID, name)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*domain.SavingsGoal), args.Error(1)
}

func (m *MockSavingsGoalRepository) Update(ctx context.Context, goal *domain.SavingsGoal) error {
	args := m.Called(ctx, goal)
	return args.Error(0)
}

func (m *MockSavingsGoalRepository) Delete(ctx context.Context, id, userID string) error {
	args := m.Called(ctx, id, userID)
	return args.Error(0)
}

func (m *MockSavingsGoalRepository) CountByUserID(ctx context.Context, userID string) (int, error) {
	args := m.Called(ctx, userID)
	return args.Int(0), args.Error(1)
}

func (m *MockSavingsGoalRepository) GetByAccountID(ctx context.Context, accountID string) ([]*domain.SavingsGoal, error) {
	args := m.Called(ctx, accountID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]*domain.SavingsGoal), args.Error(1)
}

func (m *MockSavingsGoalRepository) UpdateCurrentAmount(ctx context.Context, goalID string, currentAmount float64) error {
	args := m.Called(ctx, goalID, currentAmount)
	return args.Error(0)
}

func (m *MockSavingsGoalRepository) MarkCompleted(ctx context.Context, goalID string) error {
	args := m.Called(ctx, goalID)
	return args.Error(0)
}

// MockAccountRepository is defined in account_service_test.go

// Helper to create test savings goal
func createTestSavingsGoal(userID string) *domain.SavingsGoal {
	return &domain.SavingsGoal{
		ID:            "goal-123",
		UserID:        userID,
		Name:          "Vacation Fund",
		TargetAmount:  5000.00,
		CurrentAmount: 1000.00,
		CurrencyCode:  "USD",
		AccountID:     "account-123",
		IsCompleted:   false,
		CreatedAt:     time.Now(),
		UpdatedAt:     time.Now(),
	}
}

// Helper to create test create savings goal request
func createTestCreateSavingsGoalRequest() *domain.CreateSavingsGoalRequest {
	return &domain.CreateSavingsGoalRequest{
		Name:          "Vacation Fund",
		TargetAmount:  5000.00,
		CurrentAmount: 0,
		CurrencyCode:  "USD",
		AccountID:     "account-123",
	}
}

// Helper to create test account
func createTestAccountForGoal(userID string) domain.Account {
	return domain.Account{
		ID:     "account-123",
		UserID: userID,
		Name:   "Savings Account",
		Type:   "depository",
	}
}

// ============================================================================
// Savings Goal CRUD Tests
// ============================================================================

func TestSavingsGoalService_CreateGoal_Success(t *testing.T) {
	mockGoalRepo := new(MockSavingsGoalRepository)
	mockAccountRepo := new(MockAccountRepository)

	service := NewSavingsGoalService(mockGoalRepo, mockAccountRepo, testLogger())

	ctx := context.Background()
	userID := "user-123"
	req := createTestCreateSavingsGoalRequest()

	// User has the account
	accounts := []domain.Account{createTestAccountForGoal(userID)}

	// Setup mocks
	mockAccountRepo.On("GetByUserID", ctx, userID).Return(accounts, nil)
	mockGoalRepo.On("Create", ctx, mock.AnythingOfType("*domain.SavingsGoal")).Return(nil)

	// Execute
	goal, err := service.CreateGoal(ctx, userID, req)

	// Assert
	assert.NoError(t, err)
	assert.NotNil(t, goal)
	assert.Equal(t, userID, goal.UserID)
	assert.Equal(t, req.Name, goal.Name)
	assert.Equal(t, req.TargetAmount, goal.TargetAmount)
	assert.Equal(t, req.AccountID, goal.AccountID)
	mockGoalRepo.AssertExpectations(t)
	mockAccountRepo.AssertExpectations(t)
}

func TestSavingsGoalService_CreateGoal_ValidationError(t *testing.T) {
	mockGoalRepo := new(MockSavingsGoalRepository)
	mockAccountRepo := new(MockAccountRepository)

	service := NewSavingsGoalService(mockGoalRepo, mockAccountRepo, testLogger())

	ctx := context.Background()
	userID := "user-123"

	// Request with missing required fields
	req := &domain.CreateSavingsGoalRequest{
		Name: "", // Empty name - validation error
	}

	// Execute
	goal, err := service.CreateGoal(ctx, userID, req)

	// Assert
	assert.Error(t, err)
	assert.Nil(t, goal)
	assert.Contains(t, err.Error(), "name is required")
}

func TestSavingsGoalService_CreateGoal_InvalidAccount(t *testing.T) {
	mockGoalRepo := new(MockSavingsGoalRepository)
	mockAccountRepo := new(MockAccountRepository)

	service := NewSavingsGoalService(mockGoalRepo, mockAccountRepo, testLogger())

	ctx := context.Background()
	userID := "user-123"
	req := createTestCreateSavingsGoalRequest()
	req.AccountID = "nonexistent-account"

	// User has different accounts
	accounts := []domain.Account{
		{ID: "other-account", UserID: userID, Name: "Other"},
	}

	// Setup mocks
	mockAccountRepo.On("GetByUserID", ctx, userID).Return(accounts, nil)

	// Execute
	goal, err := service.CreateGoal(ctx, userID, req)

	// Assert
	assert.Error(t, err)
	assert.Nil(t, goal)
	assert.Contains(t, err.Error(), "account not found")
	mockAccountRepo.AssertExpectations(t)
}

func TestSavingsGoalService_CreateGoal_DuplicateName(t *testing.T) {
	mockGoalRepo := new(MockSavingsGoalRepository)
	mockAccountRepo := new(MockAccountRepository)

	service := NewSavingsGoalService(mockGoalRepo, mockAccountRepo, testLogger())

	ctx := context.Background()
	userID := "user-123"
	req := createTestCreateSavingsGoalRequest()

	accounts := []domain.Account{createTestAccountForGoal(userID)}

	// Setup mocks - simulate duplicate name error
	mockAccountRepo.On("GetByUserID", ctx, userID).Return(accounts, nil)
	mockGoalRepo.On("Create", ctx, mock.AnythingOfType("*domain.SavingsGoal")).Return(domain.ErrSavingsGoalAlreadyExists)

	// Execute
	goal, err := service.CreateGoal(ctx, userID, req)

	// Assert
	assert.ErrorIs(t, err, domain.ErrSavingsGoalAlreadyExists)
	assert.Nil(t, goal)
	mockGoalRepo.AssertExpectations(t)
	mockAccountRepo.AssertExpectations(t)
}

func TestSavingsGoalService_GetGoal_Success(t *testing.T) {
	mockGoalRepo := new(MockSavingsGoalRepository)
	mockAccountRepo := new(MockAccountRepository)

	service := NewSavingsGoalService(mockGoalRepo, mockAccountRepo, testLogger())

	ctx := context.Background()
	userID := "user-123"
	goalID := "goal-123"

	expectedGoal := createTestSavingsGoal(userID)

	// Setup mocks
	mockGoalRepo.On("GetByID", ctx, goalID).Return(expectedGoal, nil)

	// Execute
	goal, err := service.GetGoal(ctx, userID, goalID)

	// Assert
	assert.NoError(t, err)
	assert.NotNil(t, goal)
	assert.Equal(t, expectedGoal.ID, goal.ID)
	mockGoalRepo.AssertExpectations(t)
}

func TestSavingsGoalService_GetGoal_NotFound(t *testing.T) {
	mockGoalRepo := new(MockSavingsGoalRepository)
	mockAccountRepo := new(MockAccountRepository)

	service := NewSavingsGoalService(mockGoalRepo, mockAccountRepo, testLogger())

	ctx := context.Background()
	userID := "user-123"
	goalID := "nonexistent-goal"

	// Setup mocks
	mockGoalRepo.On("GetByID", ctx, goalID).Return(nil, domain.ErrSavingsGoalNotFound)

	// Execute
	goal, err := service.GetGoal(ctx, userID, goalID)

	// Assert
	assert.ErrorIs(t, err, domain.ErrSavingsGoalNotFound)
	assert.Nil(t, goal)
	mockGoalRepo.AssertExpectations(t)
}

func TestSavingsGoalService_GetGoal_WrongUser(t *testing.T) {
	mockGoalRepo := new(MockSavingsGoalRepository)
	mockAccountRepo := new(MockAccountRepository)

	service := NewSavingsGoalService(mockGoalRepo, mockAccountRepo, testLogger())

	ctx := context.Background()
	wrongUserID := "wrong-user"
	goalID := "goal-123"

	// Goal belongs to a different user
	goalOwnedByOther := createTestSavingsGoal("actual-owner")

	// Setup mocks
	mockGoalRepo.On("GetByID", ctx, goalID).Return(goalOwnedByOther, nil)

	// Execute
	goal, err := service.GetGoal(ctx, wrongUserID, goalID)

	// Assert
	assert.ErrorIs(t, err, domain.ErrSavingsGoalNotFound)
	assert.Nil(t, goal)
	mockGoalRepo.AssertExpectations(t)
}

func TestSavingsGoalService_GetGoals_Success(t *testing.T) {
	mockGoalRepo := new(MockSavingsGoalRepository)
	mockAccountRepo := new(MockAccountRepository)

	service := NewSavingsGoalService(mockGoalRepo, mockAccountRepo, testLogger())

	ctx := context.Background()
	userID := "user-123"

	expectedGoals := []*domain.SavingsGoal{
		createTestSavingsGoal(userID),
		{
			ID:            "goal-456",
			UserID:        userID,
			Name:          "Emergency Fund",
			TargetAmount:  10000.00,
			CurrentAmount: 5000.00,
			CurrencyCode:  "USD",
			AccountID:     "account-456",
		},
	}

	// Setup mocks
	mockGoalRepo.On("GetByUserID", ctx, userID).Return(expectedGoals, nil)

	// Execute
	goals, err := service.GetGoals(ctx, userID)

	// Assert
	assert.NoError(t, err)
	assert.Len(t, goals, 2)
	mockGoalRepo.AssertExpectations(t)
}

func TestSavingsGoalService_GetActiveGoals_Success(t *testing.T) {
	mockGoalRepo := new(MockSavingsGoalRepository)
	mockAccountRepo := new(MockAccountRepository)

	service := NewSavingsGoalService(mockGoalRepo, mockAccountRepo, testLogger())

	ctx := context.Background()
	userID := "user-123"

	expectedGoals := []*domain.SavingsGoal{
		createTestSavingsGoal(userID),
	}

	// Setup mocks
	mockGoalRepo.On("GetActiveByUserID", ctx, userID).Return(expectedGoals, nil)

	// Execute
	goals, err := service.GetActiveGoals(ctx, userID)

	// Assert
	assert.NoError(t, err)
	assert.Len(t, goals, 1)
	assert.False(t, goals[0].IsCompleted)
	mockGoalRepo.AssertExpectations(t)
}

func TestSavingsGoalService_UpdateGoal_Success(t *testing.T) {
	mockGoalRepo := new(MockSavingsGoalRepository)
	mockAccountRepo := new(MockAccountRepository)

	service := NewSavingsGoalService(mockGoalRepo, mockAccountRepo, testLogger())

	ctx := context.Background()
	userID := "user-123"
	goalID := "goal-123"
	newName := "Updated Vacation Fund"
	newTarget := 7500.00

	existingGoal := createTestSavingsGoal(userID)

	req := &domain.UpdateSavingsGoalRequest{
		Name:         &newName,
		TargetAmount: &newTarget,
	}

	// Setup mocks
	mockGoalRepo.On("GetByID", ctx, goalID).Return(existingGoal, nil)
	mockGoalRepo.On("Update", ctx, mock.AnythingOfType("*domain.SavingsGoal")).Return(nil)

	// Execute
	goal, err := service.UpdateGoal(ctx, userID, goalID, req)

	// Assert
	assert.NoError(t, err)
	assert.NotNil(t, goal)
	assert.Equal(t, newName, goal.Name)
	assert.Equal(t, newTarget, goal.TargetAmount)
	mockGoalRepo.AssertExpectations(t)
}

func TestSavingsGoalService_UpdateGoal_NotFound(t *testing.T) {
	mockGoalRepo := new(MockSavingsGoalRepository)
	mockAccountRepo := new(MockAccountRepository)

	service := NewSavingsGoalService(mockGoalRepo, mockAccountRepo, testLogger())

	ctx := context.Background()
	userID := "user-123"
	goalID := "nonexistent"
	newName := "Updated"

	req := &domain.UpdateSavingsGoalRequest{
		Name: &newName,
	}

	// Setup mocks
	mockGoalRepo.On("GetByID", ctx, goalID).Return(nil, domain.ErrSavingsGoalNotFound)

	// Execute
	goal, err := service.UpdateGoal(ctx, userID, goalID, req)

	// Assert
	assert.ErrorIs(t, err, domain.ErrSavingsGoalNotFound)
	assert.Nil(t, goal)
	mockGoalRepo.AssertExpectations(t)
}

func TestSavingsGoalService_UpdateGoal_ChangeAccount(t *testing.T) {
	mockGoalRepo := new(MockSavingsGoalRepository)
	mockAccountRepo := new(MockAccountRepository)

	service := NewSavingsGoalService(mockGoalRepo, mockAccountRepo, testLogger())

	ctx := context.Background()
	userID := "user-123"
	goalID := "goal-123"
	newAccountID := "new-account-123"

	existingGoal := createTestSavingsGoal(userID)

	req := &domain.UpdateSavingsGoalRequest{
		AccountID: &newAccountID,
	}

	// User has both accounts
	accounts := []domain.Account{
		createTestAccountForGoal(userID),
		{ID: newAccountID, UserID: userID, Name: "New Account"},
	}

	// Setup mocks
	mockGoalRepo.On("GetByID", ctx, goalID).Return(existingGoal, nil)
	mockAccountRepo.On("GetByUserID", ctx, userID).Return(accounts, nil)
	mockGoalRepo.On("Update", ctx, mock.AnythingOfType("*domain.SavingsGoal")).Return(nil)

	// Execute
	goal, err := service.UpdateGoal(ctx, userID, goalID, req)

	// Assert
	assert.NoError(t, err)
	assert.NotNil(t, goal)
	assert.Equal(t, newAccountID, goal.AccountID)
	mockGoalRepo.AssertExpectations(t)
	mockAccountRepo.AssertExpectations(t)
}

func TestSavingsGoalService_DeleteGoal_Success(t *testing.T) {
	mockGoalRepo := new(MockSavingsGoalRepository)
	mockAccountRepo := new(MockAccountRepository)

	service := NewSavingsGoalService(mockGoalRepo, mockAccountRepo, testLogger())

	ctx := context.Background()
	userID := "user-123"
	goalID := "goal-123"

	existingGoal := createTestSavingsGoal(userID)

	// Setup mocks
	mockGoalRepo.On("GetByID", ctx, goalID).Return(existingGoal, nil)
	mockGoalRepo.On("Delete", ctx, goalID, userID).Return(nil)

	// Execute
	err := service.DeleteGoal(ctx, userID, goalID)

	// Assert
	assert.NoError(t, err)
	mockGoalRepo.AssertExpectations(t)
}

func TestSavingsGoalService_DeleteGoal_NotFound(t *testing.T) {
	mockGoalRepo := new(MockSavingsGoalRepository)
	mockAccountRepo := new(MockAccountRepository)

	service := NewSavingsGoalService(mockGoalRepo, mockAccountRepo, testLogger())

	ctx := context.Background()
	userID := "user-123"
	goalID := "nonexistent"

	// Setup mocks
	mockGoalRepo.On("GetByID", ctx, goalID).Return(nil, domain.ErrSavingsGoalNotFound)

	// Execute
	err := service.DeleteGoal(ctx, userID, goalID)

	// Assert
	assert.ErrorIs(t, err, domain.ErrSavingsGoalNotFound)
	mockGoalRepo.AssertExpectations(t)
}

// TestSavingsGoalService_CreateGoal_ReplacedAccountRejected verifies that a replaced
// account cannot be used for a savings goal (Issue #1109: re-link chain transparency).
// GetByUserID now returns only active accounts, so replaced account IDs are not found.
func TestSavingsGoalService_CreateGoal_ReplacedAccountRejected(t *testing.T) {
	mockGoalRepo := new(MockSavingsGoalRepository)
	mockAccountRepo := new(MockAccountRepository)

	service := NewSavingsGoalService(mockGoalRepo, mockAccountRepo, testLogger())

	ctx := context.Background()
	userID := "user-123"
	req := &domain.CreateSavingsGoalRequest{
		Name:         "Test Goal",
		TargetAmount: 1000.00,
		CurrencyCode: "USD",
		AccountID:    "account-replaced", // this is a replaced account ID
	}

	// GetByUserID returns only active accounts (replaced filtered at repo layer)
	activeOnlyAccounts := []domain.Account{
		{
			ID:     "account-active",
			UserID: userID,
			Name:   "Active Checking",
			Type:   "depository",
			Status: domain.AccountStatusActive,
		},
	}

	mockAccountRepo.On("GetByUserID", ctx, userID).Return(activeOnlyAccounts, nil)

	_, err := service.CreateGoal(ctx, userID, req)

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "account not found")
	mockAccountRepo.AssertExpectations(t)
}


// TestSavingsGoalService_UpdateGoal_RefreshesUserAccountID verifies that when a user
// changes a savings goal's linked account, UpdateGoal writes the new account's
// user_account_id to the goal. Issue #1109: B.5 fix.
func TestSavingsGoalService_UpdateGoal_RefreshesUserAccountID(t *testing.T) {
	mockGoalRepo := new(MockSavingsGoalRepository)
	mockAccountRepo := new(MockAccountRepository)
	service := NewSavingsGoalService(mockGoalRepo, mockAccountRepo, testLogger())

	ctx := context.Background()
	userID := "user-update-001"
	goalID := "goal-update-001"

	uaidA := "user-acct-stable-A"
	uaidB := "user-acct-stable-B"
	accountA := "account-A-uuid"
	accountB := "account-B-uuid"

	existingGoal := &domain.SavingsGoal{
		ID:            goalID,
		UserID:        userID,
		Name:          "My Fund",
		TargetAmount:  10000.0,
		CurrentAmount: 0,
		CurrencyCode:  "USD",
		AccountID:     accountA,
		UserAccountID: &uaidA,
		IsCompleted:   false,
	}

	accounts := []domain.Account{
		{
			ID:            accountB,
			UserID:        userID,
			Name:          "New Savings",
			Type:          "depository",
			UserAccountID: &uaidB,
		},
	}

	mockGoalRepo.On("GetByID", ctx, goalID).Return(existingGoal, nil)
	mockAccountRepo.On("GetByUserID", ctx, userID).Return(accounts, nil)
	mockGoalRepo.On("Update", ctx, mock.AnythingOfType("*domain.SavingsGoal")).Return(nil)

	req := &domain.UpdateSavingsGoalRequest{
		AccountID: &accountB,
	}

	updatedGoal, err := service.UpdateGoal(ctx, userID, goalID, req)
	require.NoError(t, err)
	require.NotNil(t, updatedGoal)

	assert.Equal(t, accountB, updatedGoal.AccountID)
	require.NotNil(t, updatedGoal.UserAccountID, "UserAccountID must be updated to new account's stable ID")
	assert.Equal(t, uaidB, *updatedGoal.UserAccountID,
		"UserAccountID must reflect the new account's stable user_account_id")

	mockGoalRepo.AssertExpectations(t)
	mockAccountRepo.AssertExpectations(t)
}

// TestSavingsGoalService_UpdateGoal_ClearsUserAccountIDWhenNewAccountHasNone verifies
// that when the new account has no user_account_id (legacy account), UserAccountID
// is cleared on the goal. Issue #1109: B.5 fix.
func TestSavingsGoalService_UpdateGoal_ClearsUserAccountIDWhenNewAccountHasNone(t *testing.T) {
	mockGoalRepo := new(MockSavingsGoalRepository)
	mockAccountRepo := new(MockAccountRepository)
	service := NewSavingsGoalService(mockGoalRepo, mockAccountRepo, testLogger())

	ctx := context.Background()
	userID := "user-update-002"
	goalID := "goal-update-002"

	uaidA := "user-acct-stable-A"
	accountA := "account-A-uuid"
	accountLegacy := "account-legacy-uuid"

	existingGoal := &domain.SavingsGoal{
		ID:            goalID,
		UserID:        userID,
		Name:          "My Fund",
		TargetAmount:  5000.0,
		CurrentAmount: 0,
		CurrencyCode:  "USD",
		AccountID:     accountA,
		UserAccountID: &uaidA,
		IsCompleted:   false,
	}

	legacyAccounts := []domain.Account{
		{
			ID:            accountLegacy,
			UserID:        userID,
			Name:          "Legacy Savings",
			Type:          "depository",
			UserAccountID: nil, // no stable ID
		},
	}

	mockGoalRepo.On("GetByID", ctx, goalID).Return(existingGoal, nil)
	mockAccountRepo.On("GetByUserID", ctx, userID).Return(legacyAccounts, nil)
	mockGoalRepo.On("Update", ctx, mock.AnythingOfType("*domain.SavingsGoal")).Return(nil)

	req := &domain.UpdateSavingsGoalRequest{
		AccountID: &accountLegacy,
	}

	updatedGoal, err := service.UpdateGoal(ctx, userID, goalID, req)
	require.NoError(t, err)
	require.NotNil(t, updatedGoal)

	assert.Equal(t, accountLegacy, updatedGoal.AccountID)
	assert.Nil(t, updatedGoal.UserAccountID, "UserAccountID must be cleared when new account has no stable ID")

	mockGoalRepo.AssertExpectations(t)
	mockAccountRepo.AssertExpectations(t)
}
