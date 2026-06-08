package services

// Issue #1109: Chain-transparency tests for accountsMatch helper.
// Originally tested via MonitorForMatches; updated in Issue #972 to reflect
// that MonitorForMatches is now a no-op. The accountsMatch helper is now used
// by TransferCorrelationService.CorrelateForUser instead.
// Chain-aware matching coverage continues via TestCorrelateForUser_UserAccountIDPreferredOverAccountID.

import (
	"context"
	"testing"
	"time"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/stretchr/testify/require"
)

// TestMonitorForMatches_ChainAware_NoOp verifies that MonitorForMatches no longer
// performs any auto-accept logic (Issue #972: moved to TransferCorrelationService).
func TestMonitorForMatches_ChainAware_NoOp(t *testing.T) {
	ctx := context.Background()
	suggestionRepo := new(mockSuggestionRepoForMonitor)
	svc := NewTransactionMonitorService(suggestionRepo)

	userID := "user-123"
	now := time.Now()

	stableUserAccountID := "user-acct-stable-uuid"

	txn := &domain.Transaction{
		ID:            "txn-new-account",
		UserID:        userID,
		AccountID:     "new-account-uuid",
		UserAccountID: ptrStr(stableUserAccountID),
		Amount:        -200.00,
		Date:          now,
		Name:          "Transfer",
	}

	// MonitorForMatches is a no-op — no repo calls expected
	err := svc.MonitorForMatches(ctx, userID, []*domain.Transaction{txn})
	require.NoError(t, err)
	suggestionRepo.AssertNotCalled(t, "GetPendingByUserID")
	suggestionRepo.AssertNotCalled(t, "UpdateStatus")
}

// TestMonitorForMatches_ChainAware_NoMatchWhenUserAccountIDDiffers verifies that
// MonitorForMatches (no-op) does not call UpdateStatus regardless of account matching.
func TestMonitorForMatches_ChainAware_NoMatchWhenUserAccountIDDiffers(t *testing.T) {
	ctx := context.Background()
	suggestionRepo := new(mockSuggestionRepoForMonitor)
	svc := NewTransactionMonitorService(suggestionRepo)

	userID := "user-123"
	now := time.Now()

	txn := &domain.Transaction{
		ID:            "txn-other-account",
		UserID:        userID,
		AccountID:     "some-new-account",
		UserAccountID: ptrStr("user-acct-B"),
		Amount:        -100.00,
		Date:          now,
		Name:          "Transfer",
	}

	err := svc.MonitorForMatches(ctx, userID, []*domain.Transaction{txn})
	require.NoError(t, err)
	suggestionRepo.AssertNotCalled(t, "UpdateStatus")
}

// TestAccountsMatch_UserAccountIDPreferred verifies that accountsMatch prefers
// UserAccountID over raw AccountID when both are present — covering the chain-aware
// matching logic independently of MonitorForMatches (Issue #1109, Issue #972).
func TestAccountsMatch_UserAccountIDPreferred(t *testing.T) {
	stableID := "user-acct-stable"
	oldAcctID := "acct-old"
	newAcctID := "acct-new"

	// UserAccountID match even when raw IDs differ
	if !accountsMatch(newAcctID, &stableID, &oldAcctID, &stableID) {
		t.Fatal("expected match via UserAccountID but got false")
	}

	// UserAccountID mismatch overrides raw account match
	differentID := "user-acct-other"
	if accountsMatch(oldAcctID, &differentID, &oldAcctID, &stableID) {
		t.Fatal("expected no match when UserAccountIDs differ")
	}
}

// TestAccountsMatch_FallbackToAccountID verifies that accountsMatch falls back
// to raw AccountID when UserAccountIDs are nil (legacy data).
func TestAccountsMatch_FallbackToAccountID(t *testing.T) {
	accountID := "savings-account"

	if !accountsMatch(accountID, nil, &accountID, nil) {
		t.Fatal("expected match via raw AccountID fallback but got false")
	}

	otherID := "other-account"
	if accountsMatch(otherID, nil, &accountID, nil) {
		t.Fatal("expected no match when raw AccountIDs differ")
	}
}

// Ensure the test file still compiles with domain and time imports.
var _ = domain.SuggestionStatusPending
var _ = time.Now
