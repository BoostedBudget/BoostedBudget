package services

import (
	"context"
	"testing"
	"time"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
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
// MonitorForMatches is now a no-op (Issue #972: auto-accept removed).
// Transfer correlation is handled by TransferCorrelationService.CorrelateForUser.
// =============================================================================

func TestTransactionMonitorService_MonitorForMatches_NoOp_WithTransactions(t *testing.T) {
	ctx := context.Background()
	suggestionRepo := new(mockSuggestionRepoForMonitor)

	svc := NewTransactionMonitorService(suggestionRepo)

	userID := "user-123"
	now := time.Now()

	transaction := createTestTransactionForMonitor(userID, "savings-account", -100.00, now)

	// No repo calls expected — MonitorForMatches is a no-op
	err := svc.MonitorForMatches(ctx, userID, []*domain.Transaction{transaction})

	require.NoError(t, err)
	suggestionRepo.AssertNotCalled(t, "GetPendingByUserID", mock.Anything, mock.Anything)
	suggestionRepo.AssertNotCalled(t, "UpdateStatus", mock.Anything, mock.Anything, mock.Anything, mock.Anything)
}

func TestTransactionMonitorService_MonitorForMatches_NoOp_EmptyTransactions(t *testing.T) {
	ctx := context.Background()
	suggestionRepo := new(mockSuggestionRepoForMonitor)

	svc := NewTransactionMonitorService(suggestionRepo)

	userID := "user-123"

	err := svc.MonitorForMatches(ctx, userID, []*domain.Transaction{})

	require.NoError(t, err)
	suggestionRepo.AssertNotCalled(t, "GetPendingByUserID", mock.Anything, mock.Anything)
}

func TestTransactionMonitorService_MonitorForMatches_NoOp_NilTransactions(t *testing.T) {
	ctx := context.Background()
	suggestionRepo := new(mockSuggestionRepoForMonitor)

	svc := NewTransactionMonitorService(suggestionRepo)

	userID := "user-123"

	err := svc.MonitorForMatches(ctx, userID, nil)

	require.NoError(t, err)
	suggestionRepo.AssertNotCalled(t, "GetPendingByUserID", mock.Anything, mock.Anything)
}
