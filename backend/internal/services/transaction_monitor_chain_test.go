package services

// Issue #1109: Chain-transparency tests for transaction monitor service.
// TDD RED phase — these tests drive the accountsMatch helper.

import (
	"context"
	"testing"
	"time"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/stretchr/testify/require"
)

// TestMonitorForMatches_ChainAware_UserAccountIDMatch verifies that when a
// suggestion has ToUserAccountID set and the incoming transaction's UserAccountID
// matches, the suggestion is accepted even when the raw account IDs differ
// (the post-relink scenario).
func TestMonitorForMatches_ChainAware_UserAccountIDMatch(t *testing.T) {
	ctx := context.Background()
	suggestionRepo := new(mockSuggestionRepoForMonitor)
	svc := NewTransactionMonitorService(suggestionRepo)

	userID := "user-123"
	now := time.Now()

	oldAccountID := "old-account-uuid"
	newAccountID := "new-account-uuid"   // post-relink: different raw ID
	stableUserAccountID := "user-acct-stable-uuid"

	suggestion := &domain.TransferSuggestion{
		ID:                "suggestion-relink",
		UserID:            userID,
		FromAccountID:     ptrStr("from-account"),
		ToAccountID:       ptrStr(oldAccountID),    // points at old, replaced account
		ToUserAccountID:   ptrStr(stableUserAccountID), // stable ID backfilled by migration
		Amount:            200.00,
		CurrencyCode:      "USD",
		Source:            domain.SuggestionSourceRule,
		Status:            domain.SuggestionStatusPending,
		CreatedAt:         now.Add(-1 * time.Hour),
	}

	// Transaction arrives on the NEW account but carries the same stable user_account_id
	txn := &domain.Transaction{
		ID:            "txn-new-account",
		UserID:        userID,
		AccountID:     newAccountID,
		UserAccountID: ptrStr(stableUserAccountID),
		Amount:        -200.00, // inflow into savings
		Date:          now,
		Name:          "Transfer",
	}

	suggestionRepo.On("GetPendingByUserID", ctx, userID).
		Return([]*domain.TransferSuggestion{suggestion}, nil)

	expectedNote := "Auto-matched with transaction txn-new-account"
	suggestionRepo.On("UpdateStatus", ctx, suggestion.ID,
		domain.SuggestionStatusAccepted, &expectedNote).Return(nil)

	err := svc.MonitorForMatches(ctx, userID, []*domain.Transaction{txn})
	require.NoError(t, err)
	suggestionRepo.AssertExpectations(t)
}

// TestMonitorForMatches_ChainAware_NoMatchWhenUserAccountIDDiffers verifies that
// when both suggestion and transaction have UserAccountID set but they differ,
// the suggestion is NOT accepted.
func TestMonitorForMatches_ChainAware_NoMatchWhenUserAccountIDDiffers(t *testing.T) {
	ctx := context.Background()
	suggestionRepo := new(mockSuggestionRepoForMonitor)
	svc := NewTransactionMonitorService(suggestionRepo)

	userID := "user-123"
	now := time.Now()

	suggestion := &domain.TransferSuggestion{
		ID:              "suggestion-mismatch",
		UserID:          userID,
		FromAccountID:   ptrStr("from-account"),
		ToAccountID:     ptrStr("old-account-uuid"),
		ToUserAccountID: ptrStr("user-acct-A"),
		Amount:          100.00,
		CurrencyCode:    "USD",
		Source:          domain.SuggestionSourceRule,
		Status:          domain.SuggestionStatusPending,
		CreatedAt:       now.Add(-1 * time.Hour),
	}

	txn := &domain.Transaction{
		ID:            "txn-other-account",
		UserID:        userID,
		AccountID:     "some-new-account",
		UserAccountID: ptrStr("user-acct-B"), // different stable ID
		Amount:        -100.00,
		Date:          now,
		Name:          "Transfer",
	}

	suggestionRepo.On("GetPendingByUserID", ctx, userID).
		Return([]*domain.TransferSuggestion{suggestion}, nil)

	err := svc.MonitorForMatches(ctx, userID, []*domain.Transaction{txn})
	require.NoError(t, err)
	suggestionRepo.AssertNotCalled(t, "UpdateStatus")
}

// TestMonitorForMatches_ChainAware_FallbackToAccountID verifies that when
// ToUserAccountID is nil (legacy suggestion), the old account_id comparison
// still works correctly.
func TestMonitorForMatches_ChainAware_FallbackToAccountID(t *testing.T) {
	ctx := context.Background()
	suggestionRepo := new(mockSuggestionRepoForMonitor)
	svc := NewTransactionMonitorService(suggestionRepo)

	userID := "user-123"
	now := time.Now()
	accountID := "savings-account"

	suggestion := &domain.TransferSuggestion{
		ID:              "suggestion-legacy",
		UserID:          userID,
		FromAccountID:   ptrStr("from-account"),
		ToAccountID:     ptrStr(accountID),
		ToUserAccountID: nil, // legacy — no stable ID
		Amount:          50.00,
		CurrencyCode:    "USD",
		Source:          domain.SuggestionSourceRule,
		Status:          domain.SuggestionStatusPending,
		CreatedAt:       now.Add(-1 * time.Hour),
	}

	txn := &domain.Transaction{
		ID:            "txn-savings-account",
		UserID:        userID,
		AccountID:     accountID,
		UserAccountID: nil, // also no stable ID
		Amount:        -50.00,
		Date:          now,
		Name:          "Transfer",
	}

	suggestionRepo.On("GetPendingByUserID", ctx, userID).
		Return([]*domain.TransferSuggestion{suggestion}, nil)

	expectedNote := "Auto-matched with transaction txn-savings-account"
	suggestionRepo.On("UpdateStatus", ctx, suggestion.ID,
		domain.SuggestionStatusAccepted, &expectedNote).Return(nil)

	err := svc.MonitorForMatches(ctx, userID, []*domain.Transaction{txn})
	require.NoError(t, err)
	suggestionRepo.AssertExpectations(t)
}

func TestMonitorForMatches_ChainAware_Depth2(t *testing.T) {
	// Depth-2 chain: acct_old -> acct_mid -> acct_current
	// The suggestion points at acct_old (pre-first-relink) via ToAccountID and
	// carries the stable ToUserAccountID. A new transaction arrives on acct_current
	// (post-second-relink) but carries the same stable UserAccountID.
	// The monitor should match based on UserAccountID even at depth-2.
	ctx := context.Background()
	suggestionRepo := new(mockSuggestionRepoForMonitor)
	svc := NewTransactionMonitorService(suggestionRepo)

	userID := "user-depth2"
	now := time.Now()

	acctOld := "acct_old"
	acctCurrent := "acct_current"
	stableUserAccountID := "user-acct-stable-depth2"

	suggestion := &domain.TransferSuggestion{
		ID:              "suggestion-depth2",
		UserID:          userID,
		FromAccountID:   ptrStr("from-account"),
		ToAccountID:     ptrStr(acctOld),           // original pre-relink account ID
		ToUserAccountID: ptrStr(stableUserAccountID), // stable ID — survives both re-links
		Amount:          300.00,
		CurrencyCode:    "USD",
		Source:          domain.SuggestionSourceRule,
		Status:          domain.SuggestionStatusPending,
		CreatedAt:       now.Add(-1 * time.Hour),
	}

	txn := &domain.Transaction{
		ID:            "txn-depth2-current",
		UserID:        userID,
		AccountID:     acctCurrent,                   // third-generation raw ID
		UserAccountID: ptrStr(stableUserAccountID),   // same stable ID
		Amount:        -300.00,                        // inflow
		Date:          now,
		Name:          "Depth-2 Transfer",
	}

	suggestionRepo.On("GetPendingByUserID", ctx, userID).
		Return([]*domain.TransferSuggestion{suggestion}, nil)

	expectedNote := "Auto-matched with transaction txn-depth2-current"
	suggestionRepo.On("UpdateStatus", ctx, suggestion.ID,
		domain.SuggestionStatusAccepted, &expectedNote).Return(nil)

	err := svc.MonitorForMatches(ctx, userID, []*domain.Transaction{txn})
	require.NoError(t, err)
	suggestionRepo.AssertExpectations(t)
}
