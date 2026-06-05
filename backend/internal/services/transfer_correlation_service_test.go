// Package services provides transfer correlation tests.
// Issue #972: Savings transfer automation — B3 transfer correlation service.
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

// =============================================================================
// Mock repositories for TransferCorrelationService tests
// =============================================================================

// mockActiveSuggestionRepo mocks the ActiveSuggestionRepository interface.
type mockActiveSuggestionRepo struct {
	mock.Mock
}

func (m *mockActiveSuggestionRepo) GetActiveByUserID(ctx context.Context, userID string) ([]*domain.TransferSuggestion, error) {
	args := m.Called(ctx, userID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]*domain.TransferSuggestion), args.Error(1)
}

func (m *mockActiveSuggestionRepo) Complete(ctx context.Context, id string, matchedTransactionID string, actualAmount float64, completionSource string, completedAt time.Time) error {
	args := m.Called(ctx, id, matchedTransactionID, actualAmount, completionSource, completedAt)
	return args.Error(0)
}

// mockRecentTransactionRepo mocks the RecentTransactionRepository interface.
type mockRecentTransactionRepo struct {
	mock.Mock
}

func (m *mockRecentTransactionRepo) GetRecentInflowsByUserID(ctx context.Context, userID string, since time.Time) ([]*domain.Transaction, error) {
	args := m.Called(ctx, userID, since)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]*domain.Transaction), args.Error(1)
}

// mockCorrelationNotificationRepo mocks the CorrelationNotificationRepository interface.
type mockCorrelationNotificationRepo struct {
	mock.Mock
}

func (m *mockCorrelationNotificationRepo) Create(ctx context.Context, notification *domain.Notification) error {
	args := m.Called(ctx, notification)
	return args.Error(0)
}

func (m *mockCorrelationNotificationRepo) GetOpenByTransferSuggestionID(ctx context.Context, transferSuggestionID string) (*domain.Notification, error) {
	args := m.Called(ctx, transferSuggestionID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*domain.Notification), args.Error(1)
}

// =============================================================================
// Test Helpers
// =============================================================================

func newTestSuggestion(id, userID string, amount float64, toAccountID string, status domain.SuggestionStatus, createdAt time.Time) *domain.TransferSuggestion {
	s := &domain.TransferSuggestion{
		ID:           id,
		UserID:       userID,
		ToAccountID:  &toAccountID,
		Amount:       amount,
		CurrencyCode: "USD",
		Source:       domain.SuggestionSourceRule,
		Status:       status,
		CreatedAt:    createdAt,
		UpdatedAt:    createdAt,
	}
	return s
}

func newTestTransaction(id, userID, accountID string, amount float64, date time.Time, pending bool) *domain.Transaction {
	return &domain.Transaction{
		ID:        id,
		UserID:    userID,
		AccountID: accountID,
		Amount:    amount, // negative = inflow under PFCv2
		Date:      date,
		Name:      "Transfer",
		Pending:   pending,
	}
}

// =============================================================================
// Tests
// =============================================================================

func TestCorrelateForUser_ExactMatch(t *testing.T) {
	ctx := context.Background()
	suggestionRepo := new(mockActiveSuggestionRepo)
	txnRepo := new(mockRecentTransactionRepo)
	notifRepo := new(mockCorrelationNotificationRepo)

	svc := NewTransferCorrelationService(suggestionRepo, txnRepo, notifRepo)

	userID := "user-1"
	now := time.Now().Truncate(24 * time.Hour)
	toAcct := "savings-acct"

	suggestion := newTestSuggestion("sug-1", userID, 200.00, toAcct, domain.SuggestionStatusPending, now.Add(-1*time.Hour))
	// Exact inflow: -200.00
	txn := newTestTransaction("txn-1", userID, toAcct, -200.00, now, false)

	suggestionRepo.On("GetActiveByUserID", ctx, userID).Return([]*domain.TransferSuggestion{suggestion}, nil)
	txnRepo.On("GetRecentInflowsByUserID", ctx, userID, mock.AnythingOfType("time.Time")).Return([]*domain.Transaction{txn}, nil)
	suggestionRepo.On("Complete", ctx, "sug-1", "txn-1", 200.00, "exact_match", mock.AnythingOfType("time.Time")).Return(nil)
	notifRepo.On("Create", ctx, mock.MatchedBy(func(n *domain.Notification) bool {
		return n.Type == "transfer_suggestion" && n.InteractionPattern == "acknowledgeable"
	})).Return(nil)

	err := svc.CorrelateForUser(ctx, userID)

	require.NoError(t, err)
	suggestionRepo.AssertExpectations(t)
	txnRepo.AssertExpectations(t)
	notifRepo.AssertExpectations(t)
}

func TestCorrelateForUser_FuzzyMatch(t *testing.T) {
	ctx := context.Background()
	suggestionRepo := new(mockActiveSuggestionRepo)
	txnRepo := new(mockRecentTransactionRepo)
	notifRepo := new(mockCorrelationNotificationRepo)

	svc := NewTransferCorrelationService(suggestionRepo, txnRepo, notifRepo)

	userID := "user-2"
	now := time.Now().Truncate(24 * time.Hour)
	toAcct := "savings-acct"

	suggestion := newTestSuggestion("sug-2", userID, 200.00, toAcct, domain.SuggestionStatusPending, now.Add(-1*time.Hour))
	// Fuzzy: amount differs by $10 (5%) — within 50% threshold
	txn := newTestTransaction("txn-2", userID, toAcct, -190.00, now, false)

	suggestionRepo.On("GetActiveByUserID", ctx, userID).Return([]*domain.TransferSuggestion{suggestion}, nil)
	txnRepo.On("GetRecentInflowsByUserID", ctx, userID, mock.AnythingOfType("time.Time")).Return([]*domain.Transaction{txn}, nil)
	// No open validation notification yet
	notifRepo.On("GetOpenByTransferSuggestionID", ctx, "sug-2").Return(nil, nil)
	notifRepo.On("Create", ctx, mock.MatchedBy(func(n *domain.Notification) bool {
		return n.Type == "transfer_suggestion" &&
			n.InteractionPattern == "accept_reject" &&
			n.TransferSuggestionID != nil &&
			*n.TransferSuggestionID == "sug-2"
	})).Return(nil)

	err := svc.CorrelateForUser(ctx, userID)

	require.NoError(t, err)
	suggestionRepo.AssertExpectations(t)
	txnRepo.AssertExpectations(t)
	notifRepo.AssertExpectations(t)
	// Complete should NOT be called for fuzzy match
	suggestionRepo.AssertNotCalled(t, "Complete", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything)
}

func TestCorrelateForUser_NoMatch(t *testing.T) {
	ctx := context.Background()
	suggestionRepo := new(mockActiveSuggestionRepo)
	txnRepo := new(mockRecentTransactionRepo)
	notifRepo := new(mockCorrelationNotificationRepo)

	svc := NewTransferCorrelationService(suggestionRepo, txnRepo, notifRepo)

	userID := "user-3"
	now := time.Now().Truncate(24 * time.Hour)
	toAcct := "savings-acct"

	suggestion := newTestSuggestion("sug-3", userID, 200.00, toAcct, domain.SuggestionStatusPending, now.Add(-1*time.Hour))
	// Wrong account
	txn := newTestTransaction("txn-3", userID, "other-acct", -200.00, now, false)

	suggestionRepo.On("GetActiveByUserID", ctx, userID).Return([]*domain.TransferSuggestion{suggestion}, nil)
	txnRepo.On("GetRecentInflowsByUserID", ctx, userID, mock.AnythingOfType("time.Time")).Return([]*domain.Transaction{txn}, nil)

	err := svc.CorrelateForUser(ctx, userID)

	require.NoError(t, err)
	suggestionRepo.AssertExpectations(t)
	txnRepo.AssertExpectations(t)
	suggestionRepo.AssertNotCalled(t, "Complete", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything)
	notifRepo.AssertNotCalled(t, "Create", mock.Anything, mock.Anything)
}

func TestCorrelateForUser_SkipsPendingTxns(t *testing.T) {
	ctx := context.Background()
	suggestionRepo := new(mockActiveSuggestionRepo)
	txnRepo := new(mockRecentTransactionRepo)
	notifRepo := new(mockCorrelationNotificationRepo)

	svc := NewTransferCorrelationService(suggestionRepo, txnRepo, notifRepo)

	userID := "user-4"
	now := time.Now().Truncate(24 * time.Hour)
	toAcct := "savings-acct"

	suggestion := newTestSuggestion("sug-4", userID, 200.00, toAcct, domain.SuggestionStatusPending, now.Add(-1*time.Hour))
	// Transaction is pending (not yet posted)
	txn := newTestTransaction("txn-4", userID, toAcct, -200.00, now, true)

	suggestionRepo.On("GetActiveByUserID", ctx, userID).Return([]*domain.TransferSuggestion{suggestion}, nil)
	txnRepo.On("GetRecentInflowsByUserID", ctx, userID, mock.AnythingOfType("time.Time")).Return([]*domain.Transaction{txn}, nil)

	err := svc.CorrelateForUser(ctx, userID)

	require.NoError(t, err)
	suggestionRepo.AssertExpectations(t)
	txnRepo.AssertExpectations(t)
	suggestionRepo.AssertNotCalled(t, "Complete", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything)
	notifRepo.AssertNotCalled(t, "Create", mock.Anything, mock.Anything)
}

func TestCorrelateForUser_SkipsAlreadyUsedTxns(t *testing.T) {
	ctx := context.Background()
	suggestionRepo := new(mockActiveSuggestionRepo)
	txnRepo := new(mockRecentTransactionRepo)
	notifRepo := new(mockCorrelationNotificationRepo)

	svc := NewTransferCorrelationService(suggestionRepo, txnRepo, notifRepo)

	userID := "user-5"
	now := time.Now().Truncate(24 * time.Hour)
	toAcct := "savings-acct"

	// Two suggestions pointing to the same account
	suggestion1 := newTestSuggestion("sug-5a", userID, 200.00, toAcct, domain.SuggestionStatusPending, now.Add(-2*time.Hour))
	suggestion2 := newTestSuggestion("sug-5b", userID, 200.00, toAcct, domain.SuggestionStatusPending, now.Add(-1*time.Hour))

	txn := newTestTransaction("txn-5", userID, toAcct, -200.00, now, false)

	suggestionRepo.On("GetActiveByUserID", ctx, userID).Return([]*domain.TransferSuggestion{suggestion1, suggestion2}, nil)
	txnRepo.On("GetRecentInflowsByUserID", ctx, userID, mock.AnythingOfType("time.Time")).Return([]*domain.Transaction{txn}, nil)

	// Only the first suggestion (oldest) should be completed
	suggestionRepo.On("Complete", ctx, "sug-5a", "txn-5", 200.00, "exact_match", mock.AnythingOfType("time.Time")).Return(nil)
	notifRepo.On("Create", ctx, mock.MatchedBy(func(n *domain.Notification) bool {
		return n.InteractionPattern == "acknowledgeable"
	})).Return(nil)

	err := svc.CorrelateForUser(ctx, userID)

	require.NoError(t, err)
	// sug-5b should NOT get Complete called because txn-5 is already used
	suggestionRepo.AssertNotCalled(t, "Complete", ctx, "sug-5b", mock.Anything, mock.Anything, mock.Anything, mock.Anything)
	suggestionRepo.AssertExpectations(t)
}

func TestCorrelateForUser_NoRepeatFuzzyPrompt(t *testing.T) {
	ctx := context.Background()
	suggestionRepo := new(mockActiveSuggestionRepo)
	txnRepo := new(mockRecentTransactionRepo)
	notifRepo := new(mockCorrelationNotificationRepo)

	svc := NewTransferCorrelationService(suggestionRepo, txnRepo, notifRepo)

	userID := "user-6"
	now := time.Now().Truncate(24 * time.Hour)
	toAcct := "savings-acct"

	suggestion := newTestSuggestion("sug-6", userID, 200.00, toAcct, domain.SuggestionStatusPending, now.Add(-1*time.Hour))
	txn := newTestTransaction("txn-6", userID, toAcct, -190.00, now, false) // fuzzy

	existingNotification := &domain.Notification{
		ID:                   "notif-existing",
		UserID:               userID,
		Type:                 "transfer_suggestion",
		InteractionPattern:   "accept_reject",
		TransferSuggestionID: &suggestion.ID,
		Title:                "Did you transfer a different amount?",
		Message:              "existing prompt",
	}

	suggestionRepo.On("GetActiveByUserID", ctx, userID).Return([]*domain.TransferSuggestion{suggestion}, nil)
	txnRepo.On("GetRecentInflowsByUserID", ctx, userID, mock.AnythingOfType("time.Time")).Return([]*domain.Transaction{txn}, nil)
	// Open validation notification exists — should NOT create another
	notifRepo.On("GetOpenByTransferSuggestionID", ctx, "sug-6").Return(existingNotification, nil)

	err := svc.CorrelateForUser(ctx, userID)

	require.NoError(t, err)
	notifRepo.AssertNotCalled(t, "Create", mock.Anything, mock.Anything)
	suggestionRepo.AssertExpectations(t)
}

func TestCorrelateForUser_SkipsSuggestionWithMatchedTransactionID(t *testing.T) {
	ctx := context.Background()
	suggestionRepo := new(mockActiveSuggestionRepo)
	txnRepo := new(mockRecentTransactionRepo)
	notifRepo := new(mockCorrelationNotificationRepo)

	svc := NewTransferCorrelationService(suggestionRepo, txnRepo, notifRepo)

	userID := "user-7"
	now := time.Now().Truncate(24 * time.Hour)
	toAcct := "savings-acct"
	alreadyMatchedTxn := "txn-already-matched"

	suggestion := newTestSuggestion("sug-7", userID, 200.00, toAcct, domain.SuggestionStatusAccepted, now.Add(-1*time.Hour))
	suggestion.MatchedTransactionID = &alreadyMatchedTxn // already queued for processing

	suggestionRepo.On("GetActiveByUserID", ctx, userID).Return([]*domain.TransferSuggestion{suggestion}, nil)
	txnRepo.On("GetRecentInflowsByUserID", ctx, userID, mock.AnythingOfType("time.Time")).Return([]*domain.Transaction{}, nil)

	err := svc.CorrelateForUser(ctx, userID)

	require.NoError(t, err)
	suggestionRepo.AssertNotCalled(t, "Complete", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything)
	notifRepo.AssertNotCalled(t, "Create", mock.Anything, mock.Anything)
}

func TestCorrelateForUser_FuzzyMatchTooLarge(t *testing.T) {
	ctx := context.Background()
	suggestionRepo := new(mockActiveSuggestionRepo)
	txnRepo := new(mockRecentTransactionRepo)
	notifRepo := new(mockCorrelationNotificationRepo)

	svc := NewTransferCorrelationService(suggestionRepo, txnRepo, notifRepo)

	userID := "user-8"
	now := time.Now().Truncate(24 * time.Hour)
	toAcct := "savings-acct"

	suggestion := newTestSuggestion("sug-8", userID, 200.00, toAcct, domain.SuggestionStatusPending, now.Add(-1*time.Hour))
	// Amount differs by 60% — over 50% threshold, should not match at all
	txn := newTestTransaction("txn-8", userID, toAcct, -80.00, now, false)

	suggestionRepo.On("GetActiveByUserID", ctx, userID).Return([]*domain.TransferSuggestion{suggestion}, nil)
	txnRepo.On("GetRecentInflowsByUserID", ctx, userID, mock.AnythingOfType("time.Time")).Return([]*domain.Transaction{txn}, nil)

	err := svc.CorrelateForUser(ctx, userID)

	require.NoError(t, err)
	suggestionRepo.AssertNotCalled(t, "Complete", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything)
	notifRepo.AssertNotCalled(t, "Create", mock.Anything, mock.Anything)
}

func TestCorrelateForUser_OutsideDateWindow(t *testing.T) {
	ctx := context.Background()
	suggestionRepo := new(mockActiveSuggestionRepo)
	txnRepo := new(mockRecentTransactionRepo)
	notifRepo := new(mockCorrelationNotificationRepo)

	svc := NewTransferCorrelationService(suggestionRepo, txnRepo, notifRepo)

	userID := "user-9"
	now := time.Now().Truncate(24 * time.Hour)
	toAcct := "savings-acct"

	// Suggestion created 10 days ago
	createdAt := now.Add(-10 * 24 * time.Hour)
	suggestion := newTestSuggestion("sug-9", userID, 200.00, toAcct, domain.SuggestionStatusPending, createdAt)
	// Transaction today — 10 days after creation, outside 7-day window
	txn := newTestTransaction("txn-9", userID, toAcct, -200.00, now, false)

	suggestionRepo.On("GetActiveByUserID", ctx, userID).Return([]*domain.TransferSuggestion{suggestion}, nil)
	txnRepo.On("GetRecentInflowsByUserID", ctx, userID, mock.AnythingOfType("time.Time")).Return([]*domain.Transaction{txn}, nil)

	err := svc.CorrelateForUser(ctx, userID)

	require.NoError(t, err)
	suggestionRepo.AssertNotCalled(t, "Complete", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything)
	notifRepo.AssertNotCalled(t, "Create", mock.Anything, mock.Anything)
}

func TestCorrelateForUser_AcceptedSuggestion_UsesUserActionAtForWindow(t *testing.T) {
	ctx := context.Background()
	suggestionRepo := new(mockActiveSuggestionRepo)
	txnRepo := new(mockRecentTransactionRepo)
	notifRepo := new(mockCorrelationNotificationRepo)

	svc := NewTransferCorrelationService(suggestionRepo, txnRepo, notifRepo)

	userID := "user-10"
	now := time.Now().Truncate(24 * time.Hour)
	toAcct := "savings-acct"

	// Suggestion created 20 days ago but accepted 3 days ago
	createdAt := now.Add(-20 * 24 * time.Hour)
	acceptedAt := now.Add(-3 * 24 * time.Hour)
	suggestion := newTestSuggestion("sug-10", userID, 200.00, toAcct, domain.SuggestionStatusAccepted, createdAt)
	suggestion.UserActionAt = &acceptedAt // accepted 3 days ago

	// Transaction today — within 7 days of acceptedAt, should match
	txn := newTestTransaction("txn-10", userID, toAcct, -200.00, now, false)

	suggestionRepo.On("GetActiveByUserID", ctx, userID).Return([]*domain.TransferSuggestion{suggestion}, nil)
	txnRepo.On("GetRecentInflowsByUserID", ctx, userID, mock.AnythingOfType("time.Time")).Return([]*domain.Transaction{txn}, nil)
	suggestionRepo.On("Complete", ctx, "sug-10", "txn-10", 200.00, "exact_match", mock.AnythingOfType("time.Time")).Return(nil)
	notifRepo.On("Create", ctx, mock.MatchedBy(func(n *domain.Notification) bool {
		return n.InteractionPattern == "acknowledgeable"
	})).Return(nil)

	err := svc.CorrelateForUser(ctx, userID)

	require.NoError(t, err)
	suggestionRepo.AssertExpectations(t)
	notifRepo.AssertExpectations(t)
}

func TestCorrelateForUser_EmptySuggestions(t *testing.T) {
	ctx := context.Background()
	suggestionRepo := new(mockActiveSuggestionRepo)
	txnRepo := new(mockRecentTransactionRepo)
	notifRepo := new(mockCorrelationNotificationRepo)

	svc := NewTransferCorrelationService(suggestionRepo, txnRepo, notifRepo)

	userID := "user-11"

	suggestionRepo.On("GetActiveByUserID", ctx, userID).Return([]*domain.TransferSuggestion{}, nil)

	err := svc.CorrelateForUser(ctx, userID)

	require.NoError(t, err)
	txnRepo.AssertNotCalled(t, "GetRecentInflowsByUserID", mock.Anything, mock.Anything, mock.Anything)
}

func TestCorrelateForUser_UserAccountIDPreferredOverAccountID(t *testing.T) {
	ctx := context.Background()
	suggestionRepo := new(mockActiveSuggestionRepo)
	txnRepo := new(mockRecentTransactionRepo)
	notifRepo := new(mockCorrelationNotificationRepo)

	svc := NewTransferCorrelationService(suggestionRepo, txnRepo, notifRepo)

	userID := "user-12"
	now := time.Now().Truncate(24 * time.Hour)
	toUserAcct := "ua-savings"
	toAcct := "old-acct-id"
	newAcctID := "new-acct-id" // different raw account_id after re-link

	// Suggestion has ToUserAccountID (stable)
	suggestion := newTestSuggestion("sug-12", userID, 100.00, toAcct, domain.SuggestionStatusPending, now.Add(-1*time.Hour))
	suggestion.ToUserAccountID = &toUserAcct

	// Transaction has new raw account_id but same UserAccountID — should match via UserAccountID
	txn := newTestTransaction("txn-12", userID, newAcctID, -100.00, now, false)
	txn.UserAccountID = &toUserAcct

	suggestionRepo.On("GetActiveByUserID", ctx, userID).Return([]*domain.TransferSuggestion{suggestion}, nil)
	txnRepo.On("GetRecentInflowsByUserID", ctx, userID, mock.AnythingOfType("time.Time")).Return([]*domain.Transaction{txn}, nil)
	suggestionRepo.On("Complete", ctx, "sug-12", "txn-12", 100.00, "exact_match", mock.AnythingOfType("time.Time")).Return(nil)
	notifRepo.On("Create", ctx, mock.MatchedBy(func(n *domain.Notification) bool {
		return n.InteractionPattern == "acknowledgeable"
	})).Return(nil)

	err := svc.CorrelateForUser(ctx, userID)

	require.NoError(t, err)
	suggestionRepo.AssertExpectations(t)

	assert.True(t, true) // explicit pass
}
