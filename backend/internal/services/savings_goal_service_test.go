package services

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/repository"
	"github.com/maxnussbaum/BoostedBudget/internal/testsupport/mockassert"
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

// GetByIDForUser retrieves a savings goal by ID with ownership validation (GAP AE).
func (m *MockSavingsGoalRepository) GetByIDForUser(ctx context.Context, id, userID string) (*domain.SavingsGoal, error) {
	args := m.Called(ctx, id, userID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*domain.SavingsGoal), args.Error(1)
}

func (m *MockSavingsGoalRepository) GetDeletedByUserID(ctx context.Context, userID string) ([]*domain.SavingsGoal, error) {
	args := m.Called(ctx, userID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]*domain.SavingsGoal), args.Error(1)
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

// fakeTx is a repository.Transaction double for the unit tests. It records commit/rollback and
// nothing else: the SQL those methods would run is exercised against real Postgres in
// savings_basis_switch_atomicity_integration_test.go, and what the unit tests need to observe is
// the SEQUENCE — that a seed failure ends in Rollback and no Commit.
type fakeTx struct {
	committed  bool
	rolledBack bool
	commitErr  error
}

func (f *fakeTx) Exec(context.Context, string, ...interface{}) (pgconn.CommandTag, error) {
	return pgconn.CommandTag{}, nil
}
func (f *fakeTx) Query(context.Context, string, ...interface{}) (pgx.Rows, error) { return nil, nil }
func (f *fakeTx) QueryRow(context.Context, string, ...interface{}) pgx.Row        { return nil }
func (f *fakeTx) Commit(context.Context) error {
	f.committed = true
	return f.commitErr
}
func (f *fakeTx) Rollback(context.Context) error {
	f.rolledBack = true
	return nil
}

func (m *MockSavingsGoalRepository) BeginTx(ctx context.Context) (repository.Transaction, error) {
	args := m.Called(ctx)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(repository.Transaction), args.Error(1)
}

func (m *MockSavingsGoalRepository) UpdateWithBasisGuardInTx(ctx context.Context, tx repository.Transaction, goal *domain.SavingsGoal, priorBasis string) error {
	args := m.Called(ctx, tx, goal, priorBasis)
	return args.Error(0)
}

func (m *MockSavingsGoalRepository) Update(ctx context.Context, goal *domain.SavingsGoal) error {
	args := m.Called(ctx, goal)
	return args.Error(0)
}

func (m *MockSavingsGoalRepository) Delete(ctx context.Context, id, userID string, auditLog repository.AuditLogRepository) error {
	args := m.Called(ctx, id, userID)
	return args.Error(0)
}

func (m *MockSavingsGoalRepository) PurgeGoal(ctx context.Context, id, userID string, auditLog repository.AuditLogRepository) error {
	args := m.Called(ctx, id, userID)
	return args.Error(0)
}

func (m *MockSavingsGoalRepository) CountByUserID(ctx context.Context, userID string) (int, error) {
	args := m.Called(ctx, userID)
	return args.Int(0), args.Error(1)
}

func (m *MockSavingsGoalRepository) CountCompletedByUserID(ctx context.Context, userID string) (int, error) {
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

func (m *MockSavingsGoalRepository) UpdateCurrentAmount(ctx context.Context, goalID, basis string, currentAmount float64, lastCalculatedAt time.Time, balanceAsOf *time.Time) error {
	args := m.Called(ctx, goalID, basis, currentAmount, lastCalculatedAt, balanceAsOf)
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

// =============================================================================
// M11 Tests — progress_basis/account_percentage settable through the API (Issue #1300 gap G1)
// =============================================================================

// TestSavingsGoalService_CreateGoal_SetsProgressBasis verifies that CreateGoal maps
// ProgressBasis and AccountPercentage from the request onto the saved goal (gap G1).
func TestSavingsGoalService_CreateGoal_SetsProgressBasis(t *testing.T) {
	pct := 0.4

	tests := []struct {
		name              string
		basis             string
		accountPercentage *float64
		wantBasis         string
		wantPct           *float64
	}{
		{
			name:      "contributions basis persisted",
			basis:     "contributions",
			wantBasis: "contributions",
			wantPct:   nil,
		},
		{
			name:      "balance basis persisted",
			basis:     "balance",
			wantBasis: "balance",
			wantPct:   nil,
		},
		{
			name:              "account_percentage basis + pct persisted",
			basis:             "account_percentage",
			accountPercentage: &pct,
			wantBasis:         "account_percentage",
			wantPct:           &pct,
		},
		{
			// #1625: the domain doc (savings_goal.go) has always documented "contributions"
			// as the default for NEW goals, while the code defaulted to "manual". The
			// consequence was that POST /savings-goals/{id}/contributions returned a bare
			// 422 "basis mismatch" on every brand-new goal, with no way to discover why.
			// The doc describes the intent; the code was the bug.
			name:      "empty basis defaults to contributions for a new goal",
			basis:     "",
			wantBasis: "contributions",
			wantPct:   nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockGoalRepo := new(MockSavingsGoalRepository)
			mockAccountRepo := new(MockAccountRepository)
			service := NewSavingsGoalService(mockGoalRepo, mockAccountRepo, testLogger())

			ctx := context.Background()
			userID := "user-123"

			req := createTestCreateSavingsGoalRequest()
			req.ProgressBasis = tt.basis
			req.AccountPercentage = tt.accountPercentage

			mockAccountRepo.On("GetByUserID", ctx, userID).Return(
				[]domain.Account{createTestAccountForGoal(userID)}, nil)
			mockGoalRepo.On("Create", ctx, mock.AnythingOfType("*domain.SavingsGoal")).
				Run(func(args mock.Arguments) {
					saved := args.Get(1).(*domain.SavingsGoal)
					assert.Equal(t, tt.wantBasis, saved.ProgressBasis,
						"ProgressBasis should be mapped from request")
					if tt.wantPct == nil {
						assert.Nil(t, saved.AccountPercentage, "AccountPercentage should be nil")
					} else {
						assert.NotNil(t, saved.AccountPercentage)
						assert.InDelta(t, *tt.wantPct, *saved.AccountPercentage, 1e-9)
					}
				}).Return(nil)

			goal, err := service.CreateGoal(ctx, userID, req)

			require.NoError(t, err)
			assert.Equal(t, tt.wantBasis, goal.ProgressBasis)
			mockGoalRepo.AssertExpectations(t)
		})
	}
}

// TestSavingsGoalService_UpdateGoal_SetsProgressBasis verifies that UpdateGoal maps
// ProgressBasis and AccountPercentage changes onto the goal (gap G1).
func TestSavingsGoalService_UpdateGoal_SetsProgressBasis(t *testing.T) {
	pct := 0.6
	newBasis := "contributions"
	pctBasis := "account_percentage"

	tests := []struct {
		name      string
		req       domain.UpdateSavingsGoalRequest
		wantBasis string
		wantPct   *float64
	}{
		{
			name:      "switch to contributions clears account_percentage",
			req:       domain.UpdateSavingsGoalRequest{ProgressBasis: &newBasis},
			wantBasis: "contributions",
			wantPct:   nil,
		},
		{
			name: "switch to account_percentage sets pct",
			req: domain.UpdateSavingsGoalRequest{
				ProgressBasis:     &pctBasis,
				AccountPercentage: &pct,
			},
			wantBasis: "account_percentage",
			wantPct:   &pct,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockGoalRepo := new(MockSavingsGoalRepository)
			mockAccountRepo := new(MockAccountRepository)
			mockContribRepo := new(MockGoalBasisSwitchContribRepo)
			service := NewSavingsGoalService(mockGoalRepo, mockAccountRepo, testLogger())
			// #1625 Task 17: a basis change goes through the guarded transaction, and a service
			// with no ledger repo wired REFUSES one outright rather than taking the old unlocked
			// path. This test is about which FIELDS the switch writes, so it wires the collaborator
			// a real caller has.
			service.SetBasisSwitchContribRepo(mockContribRepo)

			ctx := context.Background()
			userID := "user-123"
			goalID := "goal-123"

			existingGoal := createTestSavingsGoal(userID)
			existingGoal.ProgressBasis = "manual"

			tx := &fakeTx{}
			mockGoalRepo.On("GetByID", ctx, goalID).Return(existingGoal, nil)
			mockGoalRepo.On("BeginTx", ctx).Return(tx, nil)
			mockContribRepo.On("ArchiveByGoalInTx", ctx, repository.Transaction(tx), goalID).Return(nil).Maybe()
			mockContribRepo.On("InsertInTx", ctx, repository.Transaction(tx), userID,
				mock.AnythingOfType("*domain.SavingsGoalContribution")).Return(nil).Maybe()
			mockGoalRepo.On("UpdateWithBasisGuardInTx", ctx, repository.Transaction(tx),
				mock.AnythingOfType("*domain.SavingsGoal"), domain.ProgressBasisManual).
				Run(func(args mock.Arguments) {
					saved := args.Get(2).(*domain.SavingsGoal)
					assert.Equal(t, tt.wantBasis, saved.ProgressBasis)
					if tt.wantPct == nil {
						assert.Nil(t, saved.AccountPercentage)
					} else {
						require.NotNil(t, saved.AccountPercentage)
						assert.InDelta(t, *tt.wantPct, *saved.AccountPercentage, 1e-9)
					}
				}).Return(nil)

			goal, err := service.UpdateGoal(ctx, userID, goalID, &tt.req)

			require.NoError(t, err)
			assert.Equal(t, tt.wantBasis, goal.ProgressBasis)
			mockGoalRepo.AssertExpectations(t)
		})
	}
}

// =============================================================================
// MockGoalAutoCreateBudgetRepo — narrow interface mock for auto-create fan-out
// =============================================================================

type MockGoalAutoCreateBudgetRepo struct {
	mock.Mock
}

func (m *MockGoalAutoCreateBudgetRepo) GetActiveByUserID(ctx context.Context, userID string) ([]*domain.Budget, error) {
	args := m.Called(ctx, userID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]*domain.Budget), args.Error(1)
}

func (m *MockGoalAutoCreateBudgetRepo) CreateCategory(ctx context.Context, category *domain.BudgetCategory) error {
	args := m.Called(ctx, category)
	return args.Error(0)
}

func (m *MockGoalAutoCreateBudgetRepo) IsGoalDetachedForSeries(ctx context.Context, seriesID, goalID string) (bool, error) {
	args := m.Called(ctx, seriesID, goalID)
	return args.Bool(0), args.Error(1)
}

// =============================================================================
// CreateGoal auto-create tests (Issue #1300)
// =============================================================================

func TestSavingsGoalService_CreateGoal_AutoCreatesSavingsCategories(t *testing.T) {
	t.Run("creates savings category in each active non-tombstoned budget", func(t *testing.T) {
		ctx := context.Background()
		userID := "user-123"

		mockGoalRepo := new(MockSavingsGoalRepository)
		mockAccountRepo := new(MockAccountRepository)
		mockBudgetRepo := new(MockGoalAutoCreateBudgetRepo)

		service := NewSavingsGoalService(mockGoalRepo, mockAccountRepo, testLogger())
		service.SetBudgetRepo(mockBudgetRepo)

		req := createTestCreateSavingsGoalRequest()
		account := createTestAccountForGoal(userID)

		budgetID := "budget-1"
		activeBudgets := []*domain.Budget{
			// SeriesID == ID for a standalone budget (its own series).
			{ID: budgetID, SeriesID: budgetID, UserID: userID, IsActive: true},
		}

		mockAccountRepo.On("GetByUserID", ctx, userID).Return([]domain.Account{account}, nil)
		mockGoalRepo.On("Create", ctx, mock.AnythingOfType("*domain.SavingsGoal")).Return(nil)
		mockBudgetRepo.On("GetActiveByUserID", ctx, userID).Return(activeBudgets, nil)
		// No tombstone for the new goal (keyed on series_id = budgetID for standalone budgets).
		mockBudgetRepo.On("IsGoalDetachedForSeries", ctx, budgetID, mock.AnythingOfType("string")).Return(false, nil)
		// CreateCategory called with savings kind + auto_created
		mockBudgetRepo.On("CreateCategory", ctx, mock.MatchedBy(func(c *domain.BudgetCategory) bool {
			return c.CategoryKind == "savings" && c.AutoCreated && c.BudgetID == budgetID
		})).Return(nil)

		goal, err := service.CreateGoal(ctx, userID, req)

		require.NoError(t, err)
		assert.NotNil(t, goal)
		mockGoalRepo.AssertExpectations(t)
		mockBudgetRepo.AssertExpectations(t)
	})

	t.Run("no auto-create when budgetRepo not set (backward compatible)", func(t *testing.T) {
		ctx := context.Background()
		userID := "user-123"

		mockGoalRepo := new(MockSavingsGoalRepository)
		mockAccountRepo := new(MockAccountRepository)

		service := NewSavingsGoalService(mockGoalRepo, mockAccountRepo, testLogger())
		// No SetBudgetRepo call

		req := createTestCreateSavingsGoalRequest()
		account := createTestAccountForGoal(userID)

		mockAccountRepo.On("GetByUserID", ctx, userID).Return([]domain.Account{account}, nil)
		mockGoalRepo.On("Create", ctx, mock.AnythingOfType("*domain.SavingsGoal")).Return(nil)

		goal, err := service.CreateGoal(ctx, userID, req)

		require.NoError(t, err)
		assert.NotNil(t, goal)
		mockGoalRepo.AssertExpectations(t)
	})

	t.Run("goal create succeeds even if auto-create category fails (best-effort)", func(t *testing.T) {
		ctx := context.Background()
		userID := "user-123"

		mockGoalRepo := new(MockSavingsGoalRepository)
		mockAccountRepo := new(MockAccountRepository)
		mockBudgetRepo := new(MockGoalAutoCreateBudgetRepo)

		service := NewSavingsGoalService(mockGoalRepo, mockAccountRepo, testLogger())
		service.SetBudgetRepo(mockBudgetRepo)

		req := createTestCreateSavingsGoalRequest()
		account := createTestAccountForGoal(userID)

		budgetID := "budget-fail"
		activeBudgets := []*domain.Budget{
			{ID: budgetID, SeriesID: budgetID, UserID: userID, IsActive: true},
		}

		mockAccountRepo.On("GetByUserID", ctx, userID).Return([]domain.Account{account}, nil)
		mockGoalRepo.On("Create", ctx, mock.AnythingOfType("*domain.SavingsGoal")).Return(nil)
		mockBudgetRepo.On("GetActiveByUserID", ctx, userID).Return(activeBudgets, nil)
		mockBudgetRepo.On("IsGoalDetachedForSeries", ctx, budgetID, mock.AnythingOfType("string")).Return(false, nil)
		// CreateCategory fails — goal creation must still succeed
		mockBudgetRepo.On("CreateCategory", ctx, mock.AnythingOfType("*domain.BudgetCategory")).Return(
			errors.New("db: unique constraint violation"),
		)

		goal, err := service.CreateGoal(ctx, userID, req)

		require.NoError(t, err, "goal creation must succeed even if category fan-out fails")
		assert.NotNil(t, goal)
		mockGoalRepo.AssertExpectations(t)
	})
}

// ============================================================================
// M13 (Issue #1300): RecomputeGoal after CreateGoal (G3)
// ============================================================================

// MockGoalProgressRecomputer is a mock for the GoalProgressRecomputer interface.
type MockGoalProgressRecomputer struct {
	mock.Mock
}

func (m *MockGoalProgressRecomputer) RecomputeGoal(ctx context.Context, goal *domain.SavingsGoal) (float64, error) {
	args := m.Called(ctx, goal)
	return args.Get(0).(float64), args.Error(1)
}

func TestSavingsGoalService_CreateGoal_TriggersRecomputeGoal(t *testing.T) {
	ctx := context.Background()
	userID := "user-recompute"

	mockGoalRepo := new(MockSavingsGoalRepository)
	mockAccountRepo := new(MockAccountRepository)
	mockProgress := new(MockGoalProgressRecomputer)

	service := NewSavingsGoalService(mockGoalRepo, mockAccountRepo, testLogger())
	service.SetProgressService(mockProgress)

	req := createTestCreateSavingsGoalRequest()
	account := createTestAccountForGoal(userID)

	mockAccountRepo.On("GetByUserID", ctx, userID).Return([]domain.Account{account}, nil)
	mockGoalRepo.On("Create", ctx, mock.AnythingOfType("*domain.SavingsGoal")).Return(nil)
	// RecomputeGoal must be called with the created goal.
	mockProgress.On("RecomputeGoal", ctx, mock.AnythingOfType("*domain.SavingsGoal")).Return(0.0, nil)
	// #1625: a SUCCESSFUL recompute is followed by a re-read, because it writes six columns
	// across two statements on two different clocks and mirroring them field-by-field was wrong
	// three times running.
	mockGoalRepo.On("GetByID", ctx, mock.AnythingOfType("string")).Return(nil, nil).Once()

	goal, err := service.CreateGoal(ctx, userID, req)

	require.NoError(t, err)
	assert.NotNil(t, goal)
	mockGoalRepo.AssertExpectations(t)
	mockProgress.AssertExpectations(t) // verifies RecomputeGoal was called
}

func TestSavingsGoalService_CreateGoal_RecomputeGoalErrorIsNonFatal(t *testing.T) {
	ctx := context.Background()
	userID := "user-recompute-fail"

	mockGoalRepo := new(MockSavingsGoalRepository)
	mockAccountRepo := new(MockAccountRepository)
	mockProgress := new(MockGoalProgressRecomputer)

	service := NewSavingsGoalService(mockGoalRepo, mockAccountRepo, testLogger())
	service.SetProgressService(mockProgress)

	req := createTestCreateSavingsGoalRequest()
	account := createTestAccountForGoal(userID)

	mockAccountRepo.On("GetByUserID", ctx, userID).Return([]domain.Account{account}, nil)
	mockGoalRepo.On("Create", ctx, mock.AnythingOfType("*domain.SavingsGoal")).Return(nil)
	// RecomputeGoal fails — CreateGoal must still succeed (non-fatal).
	mockProgress.On("RecomputeGoal", ctx, mock.AnythingOfType("*domain.SavingsGoal")).
		Return(0.0, errors.New("recompute: db connection lost"))

	goal, err := service.CreateGoal(ctx, userID, req)

	require.NoError(t, err, "CreateGoal must succeed even when RecomputeGoal errors")
	assert.NotNil(t, goal)
	mockProgress.AssertExpectations(t)
}

func TestSavingsGoalService_CreateGoal_NoProgressService_NoOp(t *testing.T) {
	// Verify that when no progress service is wired, CreateGoal still succeeds.
	ctx := context.Background()
	userID := "user-no-progress"

	mockGoalRepo := new(MockSavingsGoalRepository)
	mockAccountRepo := new(MockAccountRepository)

	service := NewSavingsGoalService(mockGoalRepo, mockAccountRepo, testLogger())
	// Intentionally do NOT call SetProgressService.

	req := createTestCreateSavingsGoalRequest()
	account := createTestAccountForGoal(userID)

	mockAccountRepo.On("GetByUserID", ctx, userID).Return([]domain.Account{account}, nil)
	mockGoalRepo.On("Create", ctx, mock.AnythingOfType("*domain.SavingsGoal")).Return(nil)

	goal, err := service.CreateGoal(ctx, userID, req)

	require.NoError(t, err)
	assert.NotNil(t, goal)
}

// ============================================================================
// M14 (Issue #1300): Basis-switch semantics in UpdateGoal (G4 / Codex C10)
// ============================================================================

// MockGoalBasisSwitchContribRepo mocks the narrow interface for basis-switch
// contribution operations (seed + archive).
type MockGoalBasisSwitchContribRepo struct {
	mock.Mock
}

// #1625: the InTx forms. `tx` is recorded so a test can assert the seed and the archive were
// handed THE SAME transaction the goal update ran in — the whole point of the boundary.
func (m *MockGoalBasisSwitchContribRepo) InsertInTx(ctx context.Context, tx repository.Transaction, userID string, contrib *domain.SavingsGoalContribution) error {
	args := m.Called(ctx, tx, userID, contrib)
	return args.Error(0)
}

func (m *MockGoalBasisSwitchContribRepo) ArchiveByGoalInTx(ctx context.Context, tx repository.Transaction, goalID string) error {
	args := m.Called(ctx, tx, goalID)
	return args.Error(0)
}

// MockAnalytics mocks the phclient.Analytics interface for test assertions.
type MockAnalytics struct {
	mock.Mock
}

func (m *MockAnalytics) Capture(distinctID, event string, properties map[string]interface{}) {
	m.Called(distinctID, event, properties)
}

func (m *MockAnalytics) Identify(distinctID string, properties map[string]interface{}) {
	m.Called(distinctID, properties)
}

func (m *MockAnalytics) Close() {
	m.Called()
}

func TestUpdateGoal_ManualToContributions_SeedsContribution(t *testing.T) {
	ctx := context.Background()
	userID := "user-basis-switch"
	goalID := "goal-basis"

	mockGoalRepo := new(MockSavingsGoalRepository)
	mockAccountRepo := new(MockAccountRepository)
	mockProgress := new(MockGoalProgressRecomputer)
	mockContribRepo := new(MockGoalBasisSwitchContribRepo)
	mockAnalytics := new(MockAnalytics)

	service := NewSavingsGoalService(mockGoalRepo, mockAccountRepo, testLogger())
	service.SetProgressService(mockProgress)
	service.SetBasisSwitchContribRepo(mockContribRepo)
	service.SetAnalytics(mockAnalytics)

	priorAmount := 750.0
	existing := &domain.SavingsGoal{
		ID:            goalID,
		UserID:        userID,
		Name:          "Vacation",
		TargetAmount:  5000.0,
		CurrentAmount: priorAmount,
		CurrencyCode:  "USD",
		AccountID:     "account-savings",
		ProgressBasis: domain.ProgressBasisManual,
	}
	newBasis := domain.ProgressBasisContributions
	req := &domain.UpdateSavingsGoalRequest{ProgressBasis: &newBasis}

	tx := &fakeTx{}
	mockGoalRepo.On("GetByID", ctx, goalID).Return(existing, nil)
	// #1625: the switch runs in ONE transaction, behind a lock on the goal row, and the plain
	// Update is NOT the path it takes.
	mockGoalRepo.On("BeginTx", ctx).Return(tx, nil)
	mockGoalRepo.On("UpdateWithBasisGuardInTx", ctx, repository.Transaction(tx),
		mock.AnythingOfType("*domain.SavingsGoal"), domain.ProgressBasisManual).Return(nil)
	// #1625 Task 12: any PRIOR contributions period is closed out BEFORE the seed goes in, so a
	// goal that round-trips does not end up counting the old rows and the synthetic seed together.
	mockContribRepo.On("ArchiveByGoalInTx", ctx, repository.Transaction(tx), goalID).Return(nil)
	// manual → contributions: a seed contribution must be inserted with Amount = priorAmount.
	mockContribRepo.On("InsertInTx", ctx, repository.Transaction(tx), userID, mock.MatchedBy(func(c *domain.SavingsGoalContribution) bool {
		return c.SavingsGoalID == goalID &&
			c.Amount == priorAmount &&
			c.Source == "manual" &&
			c.Status == "confirmed" &&
			!c.ManagedByReconciliation &&
			c.ConfirmedByUser
	})).Return(nil)
	// Analytics event fired for basis change.
	mockAnalytics.On("Capture", userID, "savings_goal_progress_basis_changed", mock.Anything).Return()
	// RecomputeGoal triggered after basis switch.
	mockProgress.On("RecomputeGoal", ctx, mock.AnythingOfType("*domain.SavingsGoal")).Return(0.0, nil)

	goal, err := service.UpdateGoal(ctx, userID, goalID, req)

	require.NoError(t, err)
	assert.NotNil(t, goal)
	assert.Equal(t, domain.ProgressBasisContributions, goal.ProgressBasis)
	assert.True(t, tx.committed, "the basis, the archive and the seed must commit together")
	mockGoalRepo.AssertNotCalled(t, "Update", mock.Anything, mock.Anything)
	mockContribRepo.AssertExpectations(t)
	mockAnalytics.AssertExpectations(t)
	mockProgress.AssertExpectations(t)
}

func TestUpdateGoal_ContributionsToBalance_ArchivesContributions(t *testing.T) {
	ctx := context.Background()
	userID := "user-basis-archive"
	goalID := "goal-archive"
	accountID := "account-savings"

	mockGoalRepo := new(MockSavingsGoalRepository)
	mockAccountRepo := new(MockAccountRepository)
	mockProgress := new(MockGoalProgressRecomputer)
	mockContribRepo := new(MockGoalBasisSwitchContribRepo)
	mockAnalytics := new(MockAnalytics)

	service := NewSavingsGoalService(mockGoalRepo, mockAccountRepo, testLogger())
	service.SetProgressService(mockProgress)
	service.SetBasisSwitchContribRepo(mockContribRepo)
	service.SetAnalytics(mockAnalytics)

	existing := &domain.SavingsGoal{
		ID:            goalID,
		UserID:        userID,
		Name:          "House",
		TargetAmount:  50000.0,
		CurrentAmount: 12000.0,
		CurrencyCode:  "USD",
		AccountID:     accountID,
		ProgressBasis: domain.ProgressBasisContributions,
	}
	newBasis := domain.ProgressBasisBalance
	req := &domain.UpdateSavingsGoalRequest{ProgressBasis: &newBasis}

	tx := &fakeTx{}
	mockGoalRepo.On("GetByID", ctx, goalID).Return(existing, nil)
	mockGoalRepo.On("BeginTx", ctx).Return(tx, nil)
	mockGoalRepo.On("UpdateWithBasisGuardInTx", ctx, repository.Transaction(tx),
		mock.AnythingOfType("*domain.SavingsGoal"), domain.ProgressBasisContributions).Return(nil)
	// contributions → balance: the closing period is archived in the same transaction.
	mockContribRepo.On("ArchiveByGoalInTx", ctx, repository.Transaction(tx), goalID).Return(nil)
	// Analytics event fired for basis change.
	mockAnalytics.On("Capture", userID, "savings_goal_progress_basis_changed", mock.Anything).Return()
	// RecomputeGoal triggered.
	mockProgress.On("RecomputeGoal", ctx, mock.AnythingOfType("*domain.SavingsGoal")).Return(0.0, nil)

	goal, err := service.UpdateGoal(ctx, userID, goalID, req)

	require.NoError(t, err)
	assert.NotNil(t, goal)
	assert.Equal(t, domain.ProgressBasisBalance, goal.ProgressBasis)
	assert.True(t, tx.committed)
	mockContribRepo.AssertExpectations(t)
	mockAnalytics.AssertExpectations(t)
	mockProgress.AssertExpectations(t)
}

func TestUpdateGoal_NoBasisChange_NoSeedNoArchive(t *testing.T) {
	ctx := context.Background()
	userID := "user-no-basis-change"
	goalID := "goal-stable"

	mockGoalRepo := new(MockSavingsGoalRepository)
	mockAccountRepo := new(MockAccountRepository)
	mockContribRepo := new(MockGoalBasisSwitchContribRepo)

	service := NewSavingsGoalService(mockGoalRepo, mockAccountRepo, testLogger())
	service.SetBasisSwitchContribRepo(mockContribRepo)

	existing := &domain.SavingsGoal{
		ID:            goalID,
		UserID:        userID,
		Name:          "Emergency Fund",
		TargetAmount:  10000.0,
		CurrentAmount: 3000.0,
		CurrencyCode:  "USD",
		AccountID:     "account-efund",
		ProgressBasis: domain.ProgressBasisManual,
	}
	newName := "Emergency Fund 2"
	req := &domain.UpdateSavingsGoalRequest{Name: &newName}

	mockGoalRepo.On("GetByID", ctx, goalID).Return(existing, nil)
	mockGoalRepo.On("Update", ctx, mock.AnythingOfType("*domain.SavingsGoal")).Return(nil)
	// No basis change → Insert and ArchiveByGoal must NOT be called.

	goal, err := service.UpdateGoal(ctx, userID, goalID, req)

	require.NoError(t, err)
	assert.NotNil(t, goal)
	mockassert.NoCallTo(t, &mockContribRepo.Mock, "InsertInTx")
	mockassert.NoCallTo(t, &mockContribRepo.Mock, "ArchiveByGoalInTx")
	// No basis change means no transaction at all: the ordinary single-statement update.
	mockassert.NoCallTo(t, &mockGoalRepo.Mock, "BeginTx")
}

// TestUpdateGoal_BasisSwitch_SeedFailureRollsBackTheSwitch is the #1625 DATA-LOSS regression.
//
// This test previously asserted the OPPOSITE — that a failed seed was non-fatal and UpdateGoal
// still returned 200. That was the defect, not the contract. The sequence it blessed:
//
//	manual goal holding $500 → new basis COMMITS → prior period archived → seed insert fails and
//	is LOGGED → UpdateGoal returns success → RecomputeGoal sums a now-empty live ledger → $0
//	persisted, permanently, with no restore path anywhere on this branch.
//
// The seed IS the money on a manual→contributions switch. There is no such thing as a switch that
// half worked, so the whole thing rolls back and the caller is told.
func TestUpdateGoal_BasisSwitch_SeedFailureRollsBackTheSwitch(t *testing.T) {
	ctx := context.Background()
	userID := "user-seed-fail"
	goalID := "goal-seed-fail"

	mockGoalRepo := new(MockSavingsGoalRepository)
	mockAccountRepo := new(MockAccountRepository)
	mockProgress := new(MockGoalProgressRecomputer)
	mockContribRepo := new(MockGoalBasisSwitchContribRepo)
	mockAnalytics := new(MockAnalytics)

	service := NewSavingsGoalService(mockGoalRepo, mockAccountRepo, testLogger())
	service.SetProgressService(mockProgress)
	service.SetBasisSwitchContribRepo(mockContribRepo)
	service.SetAnalytics(mockAnalytics)

	existing := &domain.SavingsGoal{
		ID:            goalID,
		UserID:        userID,
		Name:          "Travel",
		TargetAmount:  2000.0,
		CurrentAmount: 500.0,
		CurrencyCode:  "USD",
		AccountID:     "account-travel",
		ProgressBasis: domain.ProgressBasisManual,
	}
	newBasis := domain.ProgressBasisContributions
	req := &domain.UpdateSavingsGoalRequest{ProgressBasis: &newBasis}

	tx := &fakeTx{}
	seedErr := errors.New("db: foreign key violation")
	mockGoalRepo.On("GetByID", ctx, goalID).Return(existing, nil)
	mockGoalRepo.On("BeginTx", ctx).Return(tx, nil)
	mockGoalRepo.On("UpdateWithBasisGuardInTx", ctx, repository.Transaction(tx),
		mock.AnythingOfType("*domain.SavingsGoal"), domain.ProgressBasisManual).Return(nil)
	mockContribRepo.On("ArchiveByGoalInTx", ctx, repository.Transaction(tx), goalID).Return(nil)
	mockContribRepo.On("InsertInTx", ctx, repository.Transaction(tx), userID,
		mock.AnythingOfType("*domain.SavingsGoalContribution")).Return(seedErr)

	goal, err := service.UpdateGoal(ctx, userID, goalID, req)

	require.Error(t, err, "a failed seed must fail the switch, not be logged and swallowed")
	assert.ErrorIs(t, err, seedErr)
	assert.Nil(t, goal)
	assert.False(t, tx.committed, "nothing about the switch may be committed")
	assert.True(t, tx.rolledBack)

	// The two calls that turned a failed seed into a persisted zero. Neither may run: the
	// recompute would sum an empty ledger, and the analytics event would report a basis change
	// that did not happen.
	mockassert.NoCallTo(t, &mockProgress.Mock, "RecomputeGoal")
	mockassert.NoCallTo(t, &mockAnalytics.Mock, "Capture")
	mockassert.NoCallTo(t, &mockGoalRepo.Mock, "UpdateCurrentAmount")
}

// TestUpdateGoal_BasisSwitch_ConcurrentSwitchIsRefused pins the SERVICE half of the concurrency
// fix: when the repository reports that the row already moved off the basis this switch was
// planned from, UpdateGoal surfaces that instead of archiving and seeding a second time. The
// database half — that the guard genuinely serialises two overlapping requests — is proved against
// real Postgres in TestSavingsBasisSwitch_Integration_ConcurrentSwitchesSeedExactlyOnce.
func TestUpdateGoal_BasisSwitch_ConcurrentSwitchIsRefused(t *testing.T) {
	ctx := context.Background()
	userID := "user-basis-race"
	goalID := "goal-basis-race"

	mockGoalRepo := new(MockSavingsGoalRepository)
	mockAccountRepo := new(MockAccountRepository)
	mockProgress := new(MockGoalProgressRecomputer)
	mockContribRepo := new(MockGoalBasisSwitchContribRepo)

	service := NewSavingsGoalService(mockGoalRepo, mockAccountRepo, testLogger())
	service.SetProgressService(mockProgress)
	service.SetBasisSwitchContribRepo(mockContribRepo)

	existing := &domain.SavingsGoal{
		ID:            goalID,
		UserID:        userID,
		Name:          "Car",
		TargetAmount:  8000.0,
		CurrentAmount: 100.0,
		CurrencyCode:  "USD",
		AccountID:     "account-car",
		ProgressBasis: domain.ProgressBasisManual,
	}
	newBasis := domain.ProgressBasisContributions
	req := &domain.UpdateSavingsGoalRequest{ProgressBasis: &newBasis}

	tx := &fakeTx{}
	mockGoalRepo.On("GetByID", ctx, goalID).Return(existing, nil)
	mockGoalRepo.On("BeginTx", ctx).Return(tx, nil)
	mockGoalRepo.On("UpdateWithBasisGuardInTx", ctx, repository.Transaction(tx),
		mock.AnythingOfType("*domain.SavingsGoal"), domain.ProgressBasisManual).
		Return(domain.ErrSavingsGoalBasisConflict)

	_, err := service.UpdateGoal(ctx, userID, goalID, req)

	require.Error(t, err)
	assert.ErrorIs(t, err, domain.ErrSavingsGoalBasisConflict,
		"the loser of a basis-switch race must be refused, not allowed to seed a second time")
	assert.False(t, tx.committed)
	mockassert.NoCallTo(t, &mockContribRepo.Mock, "ArchiveByGoalInTx")
	mockassert.NoCallTo(t, &mockContribRepo.Mock, "InsertInTx")
}

// ── #1569 Task 12 fix round 1 (I5) ────────────────────────────────────────────────────────────
//
// Before these, replacing GetDeletedGoals's body with `return nil, nil` left
// `go test ./internal/services/` entirely green: the repository stub existed and was never called,
// so the service layer of gap 2 was unexercised.

func TestSavingsGoalService_GetDeletedGoals_ReturnsWhatTheRepositoryFound(t *testing.T) {
	mockGoalRepo := new(MockSavingsGoalRepository)
	mockAccountRepo := new(MockAccountRepository)
	service := NewSavingsGoalService(mockGoalRepo, mockAccountRepo, testLogger())

	ctx := context.Background()
	userID := "user-123"
	deletedAt := time.Now().Add(-72 * time.Hour)
	deleted := []*domain.SavingsGoal{{
		ID:        "goal-deleted",
		UserID:    userID,
		Name:      "Abandoned Trip",
		DeletedAt: &deletedAt,
	}}

	// The DELETED listing, not GetByUserID. A service that called the live listing instead would
	// return goals the purge screen must never offer to destroy.
	mockGoalRepo.On("GetDeletedByUserID", ctx, userID).Return(deleted, nil)

	got, err := service.GetDeletedGoals(ctx, userID)

	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, "goal-deleted", got[0].ID)
	require.NotNil(t, got[0].DeletedAt, "deleted_at must survive the service layer")
	mockGoalRepo.AssertExpectations(t)
	// Nothing else may be consulted: recomputing a balance-based current_amount for a goal the
	// user stopped tracking would state a figure that has kept moving since they walked away.
	mockGoalRepo.AssertNotCalled(t, "GetByUserID", ctx, userID)
	mockAccountRepo.AssertExpectations(t)
}

func TestSavingsGoalService_GetDeletedGoals_PropagatesRepositoryError(t *testing.T) {
	mockGoalRepo := new(MockSavingsGoalRepository)
	mockAccountRepo := new(MockAccountRepository)
	service := NewSavingsGoalService(mockGoalRepo, mockAccountRepo, testLogger())

	ctx := context.Background()
	mockGoalRepo.On("GetDeletedByUserID", ctx, "user-123").
		Return(nil, errors.New("database error"))

	got, err := service.GetDeletedGoals(ctx, "user-123")

	require.Error(t, err)
	assert.Nil(t, got)
	mockGoalRepo.AssertExpectations(t)
}

// =============================================================================
// #1625 — new goals default to the contributions basis, and CreateGoal reports the
// amount that was actually persisted.
// =============================================================================

// TestCreateGoal_ReturnsRecomputedAmount pins the response-consistency half of the fix.
// RecomputeGoal persists via UpdateCurrentAmount, which does NOT mutate the in-memory goal, so
// CreateGoal used to return the caller-supplied current_amount while the database held the
// recomputed one. Under a contributions basis with no contributions yet, that is a goal that
// claims progress it does not have.
func TestCreateGoal_ReturnsRecomputedAmount(t *testing.T) {
	mockGoalRepo := new(MockSavingsGoalRepository)
	mockAccountRepo := new(MockAccountRepository)
	recomputer := new(MockGoalProgressRecomputer)
	service := NewSavingsGoalService(mockGoalRepo, mockAccountRepo, testLogger())
	service.SetProgressService(recomputer)

	ctx := context.Background()
	userID := "user-123"

	req := createTestCreateSavingsGoalRequest()
	req.ProgressBasis = domain.ProgressBasisContributions

	mockAccountRepo.On("GetByUserID", ctx, userID).Return(
		[]domain.Account{createTestAccountForGoal(userID)}, nil)
	mockGoalRepo.On("Create", ctx, mock.AnythingOfType("*domain.SavingsGoal")).Return(nil)
	recomputer.On("RecomputeGoal", ctx, mock.AnythingOfType("*domain.SavingsGoal")).Return(0.0, nil)
	// #1625: a successful recompute is followed by a re-read. Returning nil here keeps the test
	// focused on the amount — reloadAfterRecompute falls back to the in-memory goal.
	mockGoalRepo.On("GetByID", ctx, mock.AnythingOfType("string")).Return(nil, nil).Once()

	goal, err := service.CreateGoal(ctx, userID, req)

	require.NoError(t, err)
	assert.Equal(t, 0.0, goal.CurrentAmount,
		"CreateGoal must report the recomputed amount, not the caller-supplied one")
	recomputer.AssertExpectations(t)
}

// TestCreateGoal_RecomputeFailureStaysNonFatal keeps the documented best-effort contract:
// RecomputeGoal errors are logged, never surfaced. Widening its signature must not change that.
func TestCreateGoal_RecomputeFailureStaysNonFatal(t *testing.T) {
	mockGoalRepo := new(MockSavingsGoalRepository)
	mockAccountRepo := new(MockAccountRepository)
	recomputer := new(MockGoalProgressRecomputer)
	service := NewSavingsGoalService(mockGoalRepo, mockAccountRepo, testLogger())
	service.SetProgressService(recomputer)

	ctx := context.Background()
	userID := "user-123"

	mockAccountRepo.On("GetByUserID", ctx, userID).Return(
		[]domain.Account{createTestAccountForGoal(userID)}, nil)
	mockGoalRepo.On("Create", ctx, mock.AnythingOfType("*domain.SavingsGoal")).Return(nil)
	recomputer.On("RecomputeGoal", ctx, mock.AnythingOfType("*domain.SavingsGoal")).
		Return(0.0, assert.AnError)

	goal, err := service.CreateGoal(ctx, userID, req0(t))

	require.NoError(t, err, "a recompute failure must not fail goal creation")
	assert.NotNil(t, goal)
}

func req0(t *testing.T) *domain.CreateSavingsGoalRequest {
	t.Helper()
	return createTestCreateSavingsGoalRequest()
}

// TestCreateGoal_ContributionsBasisIsZeroWithoutARecomputer covers the MCP create path, which
// builds a transaction-local goal service with NO progress service wired (Codex gap analysis
// C4/C5). Before this, such a goal kept the caller-supplied current_amount while its basis said
// the contribution ledger owns that number — a brand-new goal claiming progress it does not have.
func TestCreateGoal_ContributionsBasisIsZeroWithoutARecomputer(t *testing.T) {
	mockGoalRepo := new(MockSavingsGoalRepository)
	mockAccountRepo := new(MockAccountRepository)
	service := NewSavingsGoalService(mockGoalRepo, mockAccountRepo, testLogger())
	// NOTE: SetProgressService is deliberately NOT called — that is the MCP path.

	ctx := context.Background()
	userID := "user-123"

	req := createTestCreateSavingsGoalRequest()
	req.CurrentAmount = 5000 // a caller-supplied figure the ledger does not back

	mockAccountRepo.On("GetByUserID", ctx, userID).Return(
		[]domain.Account{createTestAccountForGoal(userID)}, nil)

	// ⚠ Assert the PERSISTED argument, not just the returned struct. The first version of this
	// test used a bare mock.AnythingOfType and therefore could not see that the fix zeroed the
	// in-memory copy AFTER the insert — the row held 5000 while the response said 0, and the
	// test passed. Codex review caught what the test could not.
	var persisted float64
	mockGoalRepo.On("Create", ctx, mock.AnythingOfType("*domain.SavingsGoal")).
		Run(func(args mock.Arguments) {
			persisted = args.Get(1).(*domain.SavingsGoal).CurrentAmount
		}).Return(nil)

	goal, err := service.CreateGoal(ctx, userID, req)

	require.NoError(t, err)
	assert.Equal(t, domain.ProgressBasisContributions, goal.ProgressBasis)
	assert.Equal(t, 0.0, persisted,
		"the row must be INSERTED with zero — a goal created this instant has no contributions")
	assert.Equal(t, 0.0, goal.CurrentAmount, "and the response must agree with the row")
}

// ...but a basis whose value is DERIVED FROM AN ACCOUNT must not be zeroed: zero would be a
// fabricated answer rather than a computed one. Those are corrected by the next sync.
func TestCreateGoal_BalanceBasisIsNotZeroedWithoutARecomputer(t *testing.T) {
	mockGoalRepo := new(MockSavingsGoalRepository)
	mockAccountRepo := new(MockAccountRepository)
	service := NewSavingsGoalService(mockGoalRepo, mockAccountRepo, testLogger())

	ctx := context.Background()
	userID := "user-123"

	req := createTestCreateSavingsGoalRequest()
	req.ProgressBasis = domain.ProgressBasisBalance
	req.CurrentAmount = 5000

	mockAccountRepo.On("GetByUserID", ctx, userID).Return(
		[]domain.Account{createTestAccountForGoal(userID)}, nil)

	var persisted float64
	mockGoalRepo.On("Create", ctx, mock.AnythingOfType("*domain.SavingsGoal")).
		Run(func(args mock.Arguments) {
			persisted = args.Get(1).(*domain.SavingsGoal).CurrentAmount
		}).Return(nil)

	goal, err := service.CreateGoal(ctx, userID, req)

	require.NoError(t, err)
	assert.Equal(t, 5000.0, persisted, "a derived-from-account basis keeps the caller's seed on the row")
	assert.Equal(t, 5000.0, goal.CurrentAmount, "and the response agrees with the row")
}

// TestUpdateGoal_RefusesCurrentAmountOnADerivedBasis closes the route by which the ORIGINAL
// #1625 bug came back: the create path was fixed and the update path was not, so
// PUT {"current_amount": 5000} on a contributions goal wrote 5000 against an empty ledger.
// Found by adversarial review, not by the author.
func TestUpdateGoal_RefusesCurrentAmountOnADerivedBasis(t *testing.T) {
	amount := 5000.0
	contributions := domain.ProgressBasisContributions
	manual := domain.ProgressBasisManual

	tests := []struct {
		name        string
		goalBasis   string
		reqBasis    *string
		wantRefused bool
	}{
		{"contributions goal refuses a hand-set amount", domain.ProgressBasisContributions, nil, true},
		{"balance goal refuses a hand-set amount", domain.ProgressBasisBalance, nil, true},
		{"switching TO contributions while setting the amount is refused", domain.ProgressBasisManual, &contributions, true},
		{"a manual goal still accepts one", domain.ProgressBasisManual, nil, false},
		// The escape hatch the 400 advertises MUST work, or the advice is a dead end. The check
		// is on the TARGET basis, so switching INTO manual while setting the amount is allowed.
		{"switching TO manual while setting the amount is allowed", domain.ProgressBasisContributions, &manual, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockGoalRepo := new(MockSavingsGoalRepository)
			mockAccountRepo := new(MockAccountRepository)
			mockContribRepo := new(MockGoalBasisSwitchContribRepo)
			service := NewSavingsGoalService(mockGoalRepo, mockAccountRepo, testLogger())
			// #1625 Task 17: one row here DOES change the basis (the manual escape hatch), and an
			// unwired service now refuses that outright. Wire the ledger so this table keeps
			// testing what it is about — which current_amount writes are refused — rather than
			// accidentally testing the wiring guard.
			service.SetBasisSwitchContribRepo(mockContribRepo)

			ctx := context.Background()
			userID := "user-123"
			existing := createTestSavingsGoal(userID)
			existing.ProgressBasis = tt.goalBasis

			tx := &fakeTx{}
			mockGoalRepo.On("GetByID", ctx, existing.ID).Return(existing, nil)
			mockGoalRepo.On("Update", ctx, mock.AnythingOfType("*domain.SavingsGoal")).Return(nil).Maybe()
			mockGoalRepo.On("BeginTx", ctx).Return(tx, nil).Maybe()
			mockGoalRepo.On("UpdateWithBasisGuardInTx", ctx, repository.Transaction(tx),
				mock.AnythingOfType("*domain.SavingsGoal"), mock.AnythingOfType("string")).Return(nil).Maybe()
			mockContribRepo.On("ArchiveByGoalInTx", ctx, repository.Transaction(tx), existing.ID).Return(nil).Maybe()
			mockContribRepo.On("InsertInTx", ctx, repository.Transaction(tx), userID,
				mock.AnythingOfType("*domain.SavingsGoalContribution")).Return(nil).Maybe()
			mockAccountRepo.On("GetByUserID", ctx, userID).Return(
				[]domain.Account{createTestAccountForGoal(userID)}, nil).Maybe()

			_, err := service.UpdateGoal(ctx, userID, existing.ID, &domain.UpdateSavingsGoalRequest{
				CurrentAmount: &amount,
				ProgressBasis: tt.reqBasis,
			})

			if tt.wantRefused {
				require.Error(t, err, "a derived basis owns current_amount and must refuse a hand-set one")
				assert.True(t, errors.Is(err, domain.ErrInvalidGoalUpdate),
					"must wrap ErrInvalidGoalUpdate so the mapper returns 400, not 500: %v", err)
				mockGoalRepo.AssertNotCalled(t, "Update", ctx, mock.AnythingOfType("*domain.SavingsGoal"))
			} else {
				require.NoError(t, err)
			}
		})
	}
}

// TestCreateGoal_ReReadsTheGoalAfterASuccessfulRecompute pins the fix for the divergence class
// that three earlier attempts each got wrong a different way.
//
// RecomputeGoal writes six columns across two statements — current_amount, last_calculated_at,
// balance_as_of and updated_at via UpdateCurrentAmount, then is_completed, completed_at and
// updated_at again via MarkCompleted — on two different clocks. Mirroring them back onto the
// in-memory struct field-by-field failed repeatedly because the set kept growing and the
// timestamps are not knowable from the service. Re-reading is correct for every column,
// including ones added later.
func TestCreateGoal_ReReadsTheGoalAfterASuccessfulRecompute(t *testing.T) {
	mockGoalRepo := new(MockSavingsGoalRepository)
	mockAccountRepo := new(MockAccountRepository)
	recomputer := new(MockGoalProgressRecomputer)
	service := NewSavingsGoalService(mockGoalRepo, mockAccountRepo, testLogger())
	service.SetProgressService(recomputer)

	ctx := context.Background()
	userID := "user-123"
	completedAt := time.Now().Add(-3 * time.Minute)
	calculatedAt := time.Now().Add(-2 * time.Minute)

	// What the ROW looks like after the recompute's two writes.
	rowAfterRecompute := &domain.SavingsGoal{
		ID:               "goal-1",
		UserID:           userID,
		ProgressBasis:    domain.ProgressBasisContributions,
		CurrentAmount:    250,
		IsCompleted:      true,
		CompletedAt:      &completedAt,
		LastCalculatedAt: &calculatedAt,
	}

	mockAccountRepo.On("GetByUserID", ctx, userID).Return(
		[]domain.Account{createTestAccountForGoal(userID)}, nil)
	mockGoalRepo.On("Create", ctx, mock.AnythingOfType("*domain.SavingsGoal")).Return(nil)
	recomputer.On("RecomputeGoal", ctx, mock.AnythingOfType("*domain.SavingsGoal")).Return(250.0, nil)
	mockGoalRepo.On("GetByID", ctx, mock.AnythingOfType("string")).Return(rowAfterRecompute, nil).Once()

	goal, err := service.CreateGoal(ctx, userID, createTestCreateSavingsGoalRequest())

	require.NoError(t, err)
	// Not just the amount: the completion flag and BOTH timestamps have to come back too, and
	// none of them was reachable by the field-mirroring approach this replaced.
	assert.Equal(t, 250.0, goal.CurrentAmount)
	assert.True(t, goal.IsCompleted, "a goal already complete on creation must not report otherwise")
	require.NotNil(t, goal.CompletedAt)
	assert.Equal(t, completedAt, *goal.CompletedAt,
		"completed_at must be the ROW's value — MarkCompleted stamps its own clock, so any value invented here is wrong by construction")
	require.NotNil(t, goal.LastCalculatedAt)
	assert.Equal(t, calculatedAt, *goal.LastCalculatedAt)
	mockGoalRepo.AssertExpectations(t)
}

// ...and a failed re-read must not fail the create. It is best-effort, like the recompute.
func TestCreateGoal_SurvivesAFailedReRead(t *testing.T) {
	mockGoalRepo := new(MockSavingsGoalRepository)
	mockAccountRepo := new(MockAccountRepository)
	recomputer := new(MockGoalProgressRecomputer)
	service := NewSavingsGoalService(mockGoalRepo, mockAccountRepo, testLogger())
	service.SetProgressService(recomputer)

	ctx := context.Background()
	userID := "user-123"

	mockAccountRepo.On("GetByUserID", ctx, userID).Return(
		[]domain.Account{createTestAccountForGoal(userID)}, nil)
	mockGoalRepo.On("Create", ctx, mock.AnythingOfType("*domain.SavingsGoal")).Return(nil)
	recomputer.On("RecomputeGoal", ctx, mock.AnythingOfType("*domain.SavingsGoal")).Return(250.0, nil)
	mockGoalRepo.On("GetByID", ctx, mock.AnythingOfType("string")).
		Return(nil, errors.New("connection reset")).Once()

	goal, err := service.CreateGoal(ctx, userID, createTestCreateSavingsGoalRequest())

	require.NoError(t, err, "a failed re-read must not fail goal creation")
	assert.Equal(t, 250.0, goal.CurrentAmount, "the amount RecomputeGoal reported still stands")
}

// #1625, found by adversarial review: a failed archive must NOT be followed by the seed.
//
// handleBasisSwitch logged the archive error and seeded anyway. That reinstates precisely the
// defect Task 12 exists to remove — the prior period's rows are still live, the synthetic seed
// lands beside them, and the goal reports both. Skipping the seed leaves current_amount
// understated until the next recompute, which is recoverable and visible; double-counting is not.
func TestUpdateGoal_BasisSwitch_FailedArchiveSkipsTheSeed(t *testing.T) {
	ctx := context.Background()
	userID := "user-archive-fail"
	goalID := "goal-archive-fail"

	mockGoalRepo := new(MockSavingsGoalRepository)
	mockAccountRepo := new(MockAccountRepository)
	mockProgress := new(MockGoalProgressRecomputer)
	mockContribRepo := new(MockGoalBasisSwitchContribRepo)
	mockAnalytics := new(MockAnalytics)

	service := NewSavingsGoalService(mockGoalRepo, mockAccountRepo, testLogger())
	service.SetProgressService(mockProgress)
	service.SetBasisSwitchContribRepo(mockContribRepo)
	service.SetAnalytics(mockAnalytics)

	existing := &domain.SavingsGoal{
		ID: goalID, UserID: userID, Name: "Vacation", TargetAmount: 5000.0,
		CurrentAmount: 750.0, CurrencyCode: "USD", AccountID: "account-savings",
		ProgressBasis: domain.ProgressBasisManual,
	}
	newBasis := domain.ProgressBasisContributions

	tx := &fakeTx{}
	archiveErr := errors.New("archive blew up")
	mockGoalRepo.On("GetByID", ctx, goalID).Return(existing, nil)
	mockGoalRepo.On("BeginTx", ctx).Return(tx, nil)
	mockGoalRepo.On("UpdateWithBasisGuardInTx", ctx, repository.Transaction(tx),
		mock.AnythingOfType("*domain.SavingsGoal"), domain.ProgressBasisManual).Return(nil)
	mockContribRepo.On("ArchiveByGoalInTx", ctx, repository.Transaction(tx), goalID).Return(archiveErr)

	_, err := service.UpdateGoal(ctx, userID, goalID, &domain.UpdateSavingsGoalRequest{ProgressBasis: &newBasis})

	// THE ASSERTION THIS TEST EXISTS FOR, unchanged: no seed after a failed archive.
	mockassert.NoCallTo(t, &mockContribRepo.Mock, "InsertInTx")

	// ⚠ WHAT CHANGED, and why it is a strengthening rather than a contradiction. This used to
	// assert require.NoError — "the basis change itself is still non-fatal" — because the basis
	// UPDATE had already committed on its own and there was nothing left to undo. #1625 put the
	// basis, the archive and the seed in ONE transaction, so a failed archive no longer leaves a
	// half-applied switch to be lenient about: the goal stays on its prior basis, the prior
	// period stays live, and the caller is told. The invariant the old comment defended (never a
	// live prior period beside a synthetic seed) now holds by construction rather than by the
	// service remembering to return early.
	require.Error(t, err, "a failed archive fails the whole switch — see the comment above")
	assert.ErrorIs(t, err, archiveErr)
	assert.False(t, tx.committed, "no part of the switch may be committed")
	assert.True(t, tx.rolledBack)
	mockassert.NoCallTo(t, &mockProgress.Mock, "RecomputeGoal")
	mockassert.NoCallTo(t, &mockAnalytics.Mock, "Capture")
}

// TestUpdateGoal_BasisChangeWithoutTheLedgerRepoIsRefused is the service half of closing the
// unguarded-path bypass (#1625 Task 17, fix round 1).
//
// basisSwitchRepo is wired by a SETTER, so it is optional by construction. UpdateGoal used to fall
// through to the plain goalRepo.Update when it was nil — meaning a basis change on a
// half-constructed service took no lock, checked no predicate, archived nothing and seeded
// nothing, exactly as before this ticket. Every test stayed green because every test that cared
// wired the repo.
//
// It is now refused. The refusal is belt to the repository's braces: Update itself would also
// refuse (TestSavingsGoalRepository_Update_RefusesABasisChange), but catching it here means the
// error names the service-level cause instead of surfacing as a mystery from two layers down.
func TestUpdateGoal_BasisChangeWithoutTheLedgerRepoIsRefused(t *testing.T) {
	ctx := context.Background()
	userID := "user-unwired"
	goalID := "goal-unwired"

	mockGoalRepo := new(MockSavingsGoalRepository)
	mockAccountRepo := new(MockAccountRepository)

	// Deliberately NOT calling SetBasisSwitchContribRepo — that is the whole scenario.
	service := NewSavingsGoalService(mockGoalRepo, mockAccountRepo, testLogger())

	existing := &domain.SavingsGoal{
		ID: goalID, UserID: userID, Name: "Boat", TargetAmount: 4000.0,
		CurrentAmount: 900.0, CurrencyCode: "USD", AccountID: "account-boat",
		ProgressBasis: domain.ProgressBasisManual,
	}
	newBasis := domain.ProgressBasisContributions
	mockGoalRepo.On("GetByID", ctx, goalID).Return(existing, nil)

	goal, err := service.UpdateGoal(ctx, userID, goalID,
		&domain.UpdateSavingsGoalRequest{ProgressBasis: &newBasis})

	require.Error(t, err, "an unwired service must refuse a basis change, not perform an unguarded one")
	assert.ErrorIs(t, err, repository.ErrUnguardedBasisChange)
	assert.Nil(t, goal)
	mockassert.NoCallTo(t, &mockGoalRepo.Mock, "Update")
	mockassert.NoCallTo(t, &mockGoalRepo.Mock, "BeginTx")
}

// TestUpdateGoal_NonBasisChangeStillTakesThePlainUpdate keeps the refusal above narrow. Renaming a
// goal on a service with no ledger repo wired must still work — the guard is about the BASIS, and
// a guard that refused every update on an optional dependency would be a regression of its own.
func TestUpdateGoal_NonBasisChangeStillTakesThePlainUpdate(t *testing.T) {
	ctx := context.Background()
	userID := "user-unwired-rename"
	goalID := "goal-unwired-rename"

	mockGoalRepo := new(MockSavingsGoalRepository)
	mockAccountRepo := new(MockAccountRepository)
	service := NewSavingsGoalService(mockGoalRepo, mockAccountRepo, testLogger())

	existing := &domain.SavingsGoal{
		ID: goalID, UserID: userID, Name: "Boat", TargetAmount: 4000.0,
		CurrentAmount: 900.0, CurrencyCode: "USD", AccountID: "account-boat",
		ProgressBasis: domain.ProgressBasisManual,
	}
	newName := "Bigger Boat"
	mockGoalRepo.On("GetByID", ctx, goalID).Return(existing, nil)
	mockGoalRepo.On("Update", ctx, mock.AnythingOfType("*domain.SavingsGoal")).Return(nil)

	goal, err := service.UpdateGoal(ctx, userID, goalID,
		&domain.UpdateSavingsGoalRequest{Name: &newName})

	require.NoError(t, err)
	assert.Equal(t, "Bigger Boat", goal.Name)
	mockGoalRepo.AssertExpectations(t)
}
