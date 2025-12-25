package services

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// =============================================================================
// Mock Repository for TransactionMonitorService Tests
// =============================================================================

// mockSuggestionRepoForMonitor is a mock for transfer suggestion operations.
type mockSuggestionRepoForMonitor struct {
	mock.Mock
}

func (m *mockSuggestionRepoForMonitor) GetPendingByUserID(ctx context.Context, userID string) ([]*domain.TransferSuggestion, error) {
	args := m.Called(ctx, userID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]*domain.TransferSuggestion), args.Error(1)
}

func (m *mockSuggestionRepoForMonitor) UpdateStatus(ctx context.Context, id string, status domain.SuggestionStatus, userNotes *string) error {
	args := m.Called(ctx, id, status, userNotes)
	return args.Error(0)
}

// =============================================================================
// Test Helpers
// =============================================================================

func createTestSuggestionForMonitor(userID string, toAccountID string, amount float64, createdAt time.Time) *domain.TransferSuggestion {
	fromAcct := "from-account"
	reason := "Savings goal"
	return &domain.TransferSuggestion{
		ID:            "suggestion-" + toAccountID,
		UserID:        userID,
		FromAccountID: &fromAcct,
		ToAccountID:   &toAccountID,
		Amount:        amount,
		CurrencyCode:  "USD",
		Reason:        &reason,
		Source:        domain.SuggestionSourceRule,
		Status:        domain.SuggestionStatusPending,
		CreatedAt:     createdAt,
		UpdatedAt:     createdAt,
	}
}

func createTestTransactionForMonitor(userID string, accountID string, amount float64, date time.Time) *domain.Transaction {
	return &domain.Transaction{
		ID:        "txn-" + accountID,
		UserID:    userID,
		AccountID: accountID,
		Amount:    amount,
		Date:      date,
		Name:      "Transfer",
	}
}

// =============================================================================
// MonitorForMatches Tests
// =============================================================================

func TestTransactionMonitorService_MonitorForMatches_MatchFound(t *testing.T) {
	ctx := context.Background()
	suggestionRepo := new(mockSuggestionRepoForMonitor)

	svc := NewTransactionMonitorService(suggestionRepo)

	userID := "user-123"
	accountID := "savings-account"
	amount := 100.00
	now := time.Now()

	// Create suggestion that was created recently
	suggestion := createTestSuggestionForMonitor(userID, accountID, amount, now.Add(-1*time.Hour))

	// Create matching transaction (same account, same amount, positive)
	transaction := createTestTransactionForMonitor(userID, accountID, amount, now)

	// Mock: Get pending suggestions
	suggestionRepo.On("GetPendingByUserID", ctx, userID).Return([]*domain.TransferSuggestion{suggestion}, nil)

	// Mock: Auto-accept the matched suggestion
	expectedNote := "Auto-matched with transaction txn-savings-account"
	suggestionRepo.On("UpdateStatus", ctx, suggestion.ID, domain.SuggestionStatusAccepted, &expectedNote).Return(nil)

	err := svc.MonitorForMatches(ctx, userID, []*domain.Transaction{transaction})

	require.NoError(t, err)
	suggestionRepo.AssertExpectations(t)
}

func TestTransactionMonitorService_MonitorForMatches_NoMatch_DifferentAmount(t *testing.T) {
	ctx := context.Background()
	suggestionRepo := new(mockSuggestionRepoForMonitor)

	svc := NewTransactionMonitorService(suggestionRepo)

	userID := "user-123"
	accountID := "savings-account"
	now := time.Now()

	// Suggestion for $100
	suggestion := createTestSuggestionForMonitor(userID, accountID, 100.00, now.Add(-1*time.Hour))

	// Transaction for $50 (different amount)
	transaction := createTestTransactionForMonitor(userID, accountID, 50.00, now)

	suggestionRepo.On("GetPendingByUserID", ctx, userID).Return([]*domain.TransferSuggestion{suggestion}, nil)

	// No UpdateStatus call expected - no match

	err := svc.MonitorForMatches(ctx, userID, []*domain.Transaction{transaction})

	require.NoError(t, err)
	suggestionRepo.AssertExpectations(t)
	suggestionRepo.AssertNotCalled(t, "UpdateStatus", mock.Anything, mock.Anything, mock.Anything, mock.Anything)
}

func TestTransactionMonitorService_MonitorForMatches_NoMatch_DifferentAccount(t *testing.T) {
	ctx := context.Background()
	suggestionRepo := new(mockSuggestionRepoForMonitor)

	svc := NewTransactionMonitorService(suggestionRepo)

	userID := "user-123"
	now := time.Now()

	// Suggestion to transfer to "savings-account"
	suggestion := createTestSuggestionForMonitor(userID, "savings-account", 100.00, now.Add(-1*time.Hour))

	// Transaction in "checking-account" (different account)
	transaction := createTestTransactionForMonitor(userID, "checking-account", 100.00, now)

	suggestionRepo.On("GetPendingByUserID", ctx, userID).Return([]*domain.TransferSuggestion{suggestion}, nil)

	err := svc.MonitorForMatches(ctx, userID, []*domain.Transaction{transaction})

	require.NoError(t, err)
	suggestionRepo.AssertExpectations(t)
	suggestionRepo.AssertNotCalled(t, "UpdateStatus", mock.Anything, mock.Anything, mock.Anything, mock.Anything)
}

func TestTransactionMonitorService_MonitorForMatches_NoMatch_NegativeAmount(t *testing.T) {
	ctx := context.Background()
	suggestionRepo := new(mockSuggestionRepoForMonitor)

	svc := NewTransactionMonitorService(suggestionRepo)

	userID := "user-123"
	accountID := "savings-account"
	now := time.Now()

	// Suggestion for $100 deposit
	suggestion := createTestSuggestionForMonitor(userID, accountID, 100.00, now.Add(-1*time.Hour))

	// Transaction is negative (debit/withdrawal) - should not match
	transaction := createTestTransactionForMonitor(userID, accountID, -100.00, now)

	suggestionRepo.On("GetPendingByUserID", ctx, userID).Return([]*domain.TransferSuggestion{suggestion}, nil)

	err := svc.MonitorForMatches(ctx, userID, []*domain.Transaction{transaction})

	require.NoError(t, err)
	suggestionRepo.AssertExpectations(t)
	suggestionRepo.AssertNotCalled(t, "UpdateStatus", mock.Anything, mock.Anything, mock.Anything, mock.Anything)
}

func TestTransactionMonitorService_MonitorForMatches_NoMatch_OutsideDateRange(t *testing.T) {
	ctx := context.Background()
	suggestionRepo := new(mockSuggestionRepoForMonitor)

	svc := NewTransactionMonitorService(suggestionRepo)

	userID := "user-123"
	accountID := "savings-account"
	now := time.Now()

	// Suggestion created 10 days ago (outside 7-day window)
	suggestion := createTestSuggestionForMonitor(userID, accountID, 100.00, now.Add(-10*24*time.Hour))

	// Transaction today (too far from suggestion creation)
	transaction := createTestTransactionForMonitor(userID, accountID, 100.00, now)

	suggestionRepo.On("GetPendingByUserID", ctx, userID).Return([]*domain.TransferSuggestion{suggestion}, nil)

	err := svc.MonitorForMatches(ctx, userID, []*domain.Transaction{transaction})

	require.NoError(t, err)
	suggestionRepo.AssertExpectations(t)
	suggestionRepo.AssertNotCalled(t, "UpdateStatus", mock.Anything, mock.Anything, mock.Anything, mock.Anything)
}

func TestTransactionMonitorService_MonitorForMatches_AmountWithinTolerance(t *testing.T) {
	ctx := context.Background()
	suggestionRepo := new(mockSuggestionRepoForMonitor)

	svc := NewTransactionMonitorService(suggestionRepo)

	userID := "user-123"
	accountID := "savings-account"
	now := time.Now()

	// Suggestion for $100.00
	suggestion := createTestSuggestionForMonitor(userID, accountID, 100.00, now.Add(-1*time.Hour))

	// Transaction for $100.005 (within 1 cent tolerance)
	transaction := createTestTransactionForMonitor(userID, accountID, 100.005, now)

	suggestionRepo.On("GetPendingByUserID", ctx, userID).Return([]*domain.TransferSuggestion{suggestion}, nil)

	expectedNote := "Auto-matched with transaction txn-savings-account"
	suggestionRepo.On("UpdateStatus", ctx, suggestion.ID, domain.SuggestionStatusAccepted, &expectedNote).Return(nil)

	err := svc.MonitorForMatches(ctx, userID, []*domain.Transaction{transaction})

	require.NoError(t, err)
	suggestionRepo.AssertExpectations(t)
}

func TestTransactionMonitorService_MonitorForMatches_NoPendingSuggestions(t *testing.T) {
	ctx := context.Background()
	suggestionRepo := new(mockSuggestionRepoForMonitor)

	svc := NewTransactionMonitorService(suggestionRepo)

	userID := "user-123"
	now := time.Now()

	transaction := createTestTransactionForMonitor(userID, "any-account", 100.00, now)

	// No pending suggestions
	suggestionRepo.On("GetPendingByUserID", ctx, userID).Return([]*domain.TransferSuggestion{}, nil)

	err := svc.MonitorForMatches(ctx, userID, []*domain.Transaction{transaction})

	require.NoError(t, err)
	suggestionRepo.AssertExpectations(t)
}

func TestTransactionMonitorService_MonitorForMatches_NoTransactions(t *testing.T) {
	ctx := context.Background()
	suggestionRepo := new(mockSuggestionRepoForMonitor)

	svc := NewTransactionMonitorService(suggestionRepo)

	userID := "user-123"

	// No transactions to check - should return early without querying suggestions
	err := svc.MonitorForMatches(ctx, userID, []*domain.Transaction{})

	require.NoError(t, err)
	suggestionRepo.AssertNotCalled(t, "GetPendingByUserID", mock.Anything, mock.Anything)
}

func TestTransactionMonitorService_MonitorForMatches_RepoError(t *testing.T) {
	ctx := context.Background()
	suggestionRepo := new(mockSuggestionRepoForMonitor)

	svc := NewTransactionMonitorService(suggestionRepo)

	userID := "user-123"
	now := time.Now()

	transaction := createTestTransactionForMonitor(userID, "any-account", 100.00, now)

	suggestionRepo.On("GetPendingByUserID", ctx, userID).Return(nil, errors.New("database error"))

	err := svc.MonitorForMatches(ctx, userID, []*domain.Transaction{transaction})

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "database error")
	suggestionRepo.AssertExpectations(t)
}

func TestTransactionMonitorService_MonitorForMatches_NilToAccountID(t *testing.T) {
	ctx := context.Background()
	suggestionRepo := new(mockSuggestionRepoForMonitor)

	svc := NewTransactionMonitorService(suggestionRepo)

	userID := "user-123"
	now := time.Now()

	// Suggestion with nil ToAccountID (shouldn't match anything)
	suggestion := &domain.TransferSuggestion{
		ID:            "suggestion-nil-to",
		UserID:        userID,
		FromAccountID: nil,
		ToAccountID:   nil, // No target account
		Amount:        100.00,
		CurrencyCode:  "USD",
		Status:        domain.SuggestionStatusPending,
		CreatedAt:     now.Add(-1 * time.Hour),
	}

	transaction := createTestTransactionForMonitor(userID, "any-account", 100.00, now)

	suggestionRepo.On("GetPendingByUserID", ctx, userID).Return([]*domain.TransferSuggestion{suggestion}, nil)

	err := svc.MonitorForMatches(ctx, userID, []*domain.Transaction{transaction})

	require.NoError(t, err)
	suggestionRepo.AssertExpectations(t)
	suggestionRepo.AssertNotCalled(t, "UpdateStatus", mock.Anything, mock.Anything, mock.Anything, mock.Anything)
}

func TestTransactionMonitorService_MonitorForMatches_MultipleTransactionsOneMatch(t *testing.T) {
	ctx := context.Background()
	suggestionRepo := new(mockSuggestionRepoForMonitor)

	svc := NewTransactionMonitorService(suggestionRepo)

	userID := "user-123"
	now := time.Now()

	// Suggestion for savings account
	suggestion := createTestSuggestionForMonitor(userID, "savings-account", 100.00, now.Add(-1*time.Hour))

	// Multiple transactions - only one matches
	transactions := []*domain.Transaction{
		createTestTransactionForMonitor(userID, "checking-account", 50.00, now),   // wrong account
		createTestTransactionForMonitor(userID, "savings-account", -100.00, now),  // negative
		createTestTransactionForMonitor(userID, "savings-account", 100.00, now),   // MATCH
	}

	suggestionRepo.On("GetPendingByUserID", ctx, userID).Return([]*domain.TransferSuggestion{suggestion}, nil)

	expectedNote := "Auto-matched with transaction txn-savings-account"
	suggestionRepo.On("UpdateStatus", ctx, suggestion.ID, domain.SuggestionStatusAccepted, &expectedNote).Return(nil)

	err := svc.MonitorForMatches(ctx, userID, transactions)

	require.NoError(t, err)
	suggestionRepo.AssertExpectations(t)
}
