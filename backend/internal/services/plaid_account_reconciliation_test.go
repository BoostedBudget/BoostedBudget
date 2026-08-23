package services

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"

	"github.com/plaid/plaid-go/v23/plaid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/repository"
)

// Issue #1569, Task 5 — Plaid-initiated account death.
//
// When a bank closes an account, Plaid stops returning it from /accounts/get. Nothing used to
// notice: the row stayed 'active' with a frozen balance forever, inflating CURRENT net worth.
// reconcileRemovedAccounts walks the reverse direction (internal accounts → Plaid's roster) and
// soft-removes what Plaid no longer enumerates — but ONLY when the response is provably complete.

func reconcileTestService(accountRepo repository.AccountRepository, audit AuditLogger) *transactionSyncService {
	return &transactionSyncService{
		logger:      slog.New(slog.NewTextHandler(io.Discard, nil)),
		accountRepo: accountRepo,
		auditLogger: audit,
	}
}

func reconcileTestItem() *domain.LinkedItem {
	return &domain.LinkedItem{ID: "item-1", UserID: "user-1"}
}

func reconcileTestAccount(id, plaidID string) domain.Account {
	return domain.Account{
		ID:             id,
		LinkedItemID:   "item-1",
		UserID:         "user-1",
		PlaidAccountID: plaidID,
		Status:         domain.AccountStatusActive,
	}
}

// TestReconcileRemovedAccounts_AbsentFromCompleteResponse_MarksRemoved is the core behaviour:
// Plaid enumerated the item's accounts successfully and this one was not among them, so the bank
// closed it. It transitions to 'removed' through the EXISTING soft-remove write path
// (UpdateStatus, which stamps removed_at and lets the history trigger close the interval), and
// the removal is audit-logged so "why did this disappear?" has an answer.
func TestReconcileRemovedAccounts_AbsentFromCompleteResponse_MarksRemoved(t *testing.T) {
	accountRepo := &MockAccountRepository{}
	audit := &MockAuditLogger{}
	svc := reconcileTestService(accountRepo, audit)

	accountRepo.On("UpdateStatus", mock.Anything, "acct-gone", "user-1", domain.AccountStatusRemoved, (*string)(nil)).
		Return(nil).Once()
	audit.On("Log", mock.Anything, "account.removed_by_provider", "account", "acct-gone").Return(nil).Once()

	roster := PlaidAccountRosterFrom([]plaid.AccountBase{{AccountId: "plaid-live"}}, nil)
	removed := svc.reconcileRemovedAccounts(context.Background(), reconcileTestItem(), roster, []domain.Account{
		reconcileTestAccount("acct-live", "plaid-live"),
		reconcileTestAccount("acct-gone", "plaid-gone"),
	})

	assert.Equal(t, 1, removed)
	accountRepo.AssertExpectations(t)
	audit.AssertExpectations(t)
	// The still-present account is never touched.
	accountRepo.AssertNotCalled(t, "UpdateStatus", mock.Anything, "acct-live", mock.Anything, mock.Anything, mock.Anything)
}

// TestReconcileRemovedAccounts_StillPresent_StaysActive — brief test 4.
func TestReconcileRemovedAccounts_StillPresent_StaysActive(t *testing.T) {
	accountRepo := &MockAccountRepository{}
	audit := &MockAuditLogger{}
	svc := reconcileTestService(accountRepo, audit)

	roster := PlaidAccountRosterFrom([]plaid.AccountBase{
		{AccountId: "plaid-a"},
		{AccountId: "plaid-b"},
	}, nil)
	removed := svc.reconcileRemovedAccounts(context.Background(), reconcileTestItem(), roster, []domain.Account{
		reconcileTestAccount("acct-a", "plaid-a"),
		reconcileTestAccount("acct-b", "plaid-b"),
	})

	assert.Equal(t, 0, removed)
	accountRepo.AssertNotCalled(t, "UpdateStatus", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything)
	audit.AssertNotCalled(t, "Log", mock.Anything, mock.Anything, mock.Anything, mock.Anything)
}

// TestReconcileRemovedAccounts_ZeroAccountsResponse_MarksNothing is THE catastrophic case.
//
// An errored, throttled, or degraded /accounts/get can come back with an empty `accounts` array.
// Read as "Plaid enumerated everything and none of your accounts exist any more," that sweeps a
// user's ENTIRE portfolio to 'removed' in one run and zeroes their net worth. It must mark
// NOTHING. Note the mock carries no UpdateStatus expectation at all: any call is a hard failure.
func TestReconcileRemovedAccounts_ZeroAccountsResponse_MarksNothing(t *testing.T) {
	for name, roster := range map[string]PlaidAccountRoster{
		"nil accounts":   PlaidAccountRosterFrom(nil, nil),
		"empty accounts": PlaidAccountRosterFrom([]plaid.AccountBase{}, nil),
		"blank ids only": PlaidAccountRosterFrom([]plaid.AccountBase{{AccountId: ""}}, nil),
	} {
		t.Run(name, func(t *testing.T) {
			accountRepo := &MockAccountRepository{}
			audit := &MockAuditLogger{}
			svc := reconcileTestService(accountRepo, audit)

			removed := svc.reconcileRemovedAccounts(context.Background(), reconcileTestItem(), roster, []domain.Account{
				reconcileTestAccount("acct-a", "plaid-a"),
				reconcileTestAccount("acct-b", "plaid-b"),
				reconcileTestAccount("acct-c", "plaid-c"),
			})

			assert.Equal(t, 0, removed, "a zero-account response must never sweep a portfolio to removed")
			accountRepo.AssertNotCalled(t, "UpdateStatus", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything)
			audit.AssertNotCalled(t, "Log", mock.Anything, mock.Anything, mock.Anything, mock.Anything)
		})
	}
}

// TestReconcileRemovedAccounts_ErroredResponse_MarksNothing — brief test 2. A call that returned
// an error proves nothing about which accounts still exist, even when it also handed back a
// partially-decoded accounts array. Every internal account must be left completely untouched.
func TestReconcileRemovedAccounts_ErroredResponse_MarksNothing(t *testing.T) {
	accountRepo := &MockAccountRepository{}
	audit := &MockAuditLogger{}
	svc := reconcileTestService(accountRepo, audit)

	roster := PlaidAccountRosterFrom([]plaid.AccountBase{{AccountId: "plaid-a"}}, errors.New("plaid: PRODUCT_NOT_READY"))
	removed := svc.reconcileRemovedAccounts(context.Background(), reconcileTestItem(), roster, []domain.Account{
		reconcileTestAccount("acct-a", "plaid-a"),
		reconcileTestAccount("acct-b", "plaid-b"),
	})

	assert.Equal(t, 0, removed)
	accountRepo.AssertNotCalled(t, "UpdateStatus", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything)
	audit.AssertNotCalled(t, "Log", mock.Anything, mock.Anything, mock.Anything, mock.Anything)
}

// TestReconcileRemovedAccounts_AlreadyDisconnected_NotReTransitioned — brief test 5. Task 2 gave
// UpdateStatus a status guard: it refuses a non-active row with ErrAccountAlreadyRemoved (the
// idempotency case) or ErrAccountAlreadyDisconnected ('replaced'/'expired' — a more specific
// terminal reason this sweep must not overwrite). Reconciliation cooperates: it swallows both,
// audit-logs NEITHER (nothing happened to log), and keeps going for the remaining accounts.
func TestReconcileRemovedAccounts_AlreadyDisconnected_NotReTransitioned(t *testing.T) {
	accountRepo := &MockAccountRepository{}
	audit := &MockAuditLogger{}
	svc := reconcileTestService(accountRepo, audit)

	accountRepo.On("UpdateStatus", mock.Anything, "acct-replaced", "user-1", domain.AccountStatusRemoved, (*string)(nil)).
		Return(repository.ErrAccountAlreadyDisconnected).Once()
	accountRepo.On("UpdateStatus", mock.Anything, "acct-already", "user-1", domain.AccountStatusRemoved, (*string)(nil)).
		Return(repository.ErrAccountAlreadyRemoved).Once()
	accountRepo.On("UpdateStatus", mock.Anything, "acct-gone", "user-1", domain.AccountStatusRemoved, (*string)(nil)).
		Return(nil).Once()
	audit.On("Log", mock.Anything, "account.removed_by_provider", "account", "acct-gone").Return(nil).Once()

	roster := PlaidAccountRosterFrom([]plaid.AccountBase{{AccountId: "plaid-live"}}, nil)
	removed := svc.reconcileRemovedAccounts(context.Background(), reconcileTestItem(), roster, []domain.Account{
		reconcileTestAccount("acct-replaced", "plaid-replaced"),
		reconcileTestAccount("acct-already", "plaid-already"),
		reconcileTestAccount("acct-gone", "plaid-gone"),
	})

	assert.Equal(t, 1, removed, "only the genuine transition counts")
	accountRepo.AssertExpectations(t)
	audit.AssertExpectations(t)
	audit.AssertNumberOfCalls(t, "Log", 1)
}

// TestReconcileRemovedAccounts_ForeignAccountSkipped mirrors the ownership guard the liability and
// holdings syncs already apply: GetByLinkedItemID is keyed on linked_item_id and is NOT
// user-scoped, so a divergence between accounts.user_id and linked_items.user_id must never be
// laundered into a write against another user's row.
func TestReconcileRemovedAccounts_ForeignAccountSkipped(t *testing.T) {
	accountRepo := &MockAccountRepository{}
	audit := &MockAuditLogger{}
	svc := reconcileTestService(accountRepo, audit)

	foreign := reconcileTestAccount("acct-foreign", "plaid-gone")
	foreign.UserID = "user-2"

	roster := PlaidAccountRosterFrom([]plaid.AccountBase{{AccountId: "plaid-live"}}, nil)
	removed := svc.reconcileRemovedAccounts(context.Background(), reconcileTestItem(), roster, []domain.Account{foreign})

	assert.Equal(t, 0, removed)
	accountRepo.AssertNotCalled(t, "UpdateStatus", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything)
}

// TestReconcileRemovedAccounts_BlankPlaidAccountID_Skipped: a row with no plaid_account_id can
// never be "found" in any roster, so absence says nothing about it. It must not be swept.
func TestReconcileRemovedAccounts_BlankPlaidAccountID_Skipped(t *testing.T) {
	accountRepo := &MockAccountRepository{}
	svc := reconcileTestService(accountRepo, nil)

	roster := PlaidAccountRosterFrom([]plaid.AccountBase{{AccountId: "plaid-live"}}, nil)
	removed := svc.reconcileRemovedAccounts(context.Background(), reconcileTestItem(), roster, []domain.Account{
		reconcileTestAccount("acct-blank", ""),
	})

	assert.Equal(t, 0, removed)
	accountRepo.AssertNotCalled(t, "UpdateStatus", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything)
}

// TestReconcileRemovedAccounts_AuditFailureIsNonFatal: the account is already removed by the time
// the audit write runs, so an audit failure must not abort the pass or the remaining accounts.
// Same "already committed, don't abort" reasoning as DisconnectAccount's audit call.
func TestReconcileRemovedAccounts_AuditFailureIsNonFatal(t *testing.T) {
	accountRepo := &MockAccountRepository{}
	audit := &MockAuditLogger{}
	svc := reconcileTestService(accountRepo, audit)

	accountRepo.On("UpdateStatus", mock.Anything, mock.Anything, "user-1", domain.AccountStatusRemoved, (*string)(nil)).
		Return(nil).Twice()
	audit.On("Log", mock.Anything, "account.removed_by_provider", "account", mock.Anything).
		Return(errors.New("audit db down")).Twice()

	roster := PlaidAccountRosterFrom([]plaid.AccountBase{{AccountId: "plaid-live"}}, nil)
	removed := svc.reconcileRemovedAccounts(context.Background(), reconcileTestItem(), roster, []domain.Account{
		reconcileTestAccount("acct-gone-1", "plaid-gone-1"),
		reconcileTestAccount("acct-gone-2", "plaid-gone-2"),
	})

	assert.Equal(t, 2, removed)
	accountRepo.AssertExpectations(t)
	audit.AssertExpectations(t)
}

// TestReconcileRemovedAccounts_NilAuditLogger_NoPanic: the audit logger is setter-injected and
// optional, exactly like every other optional wiring on this service.
func TestReconcileRemovedAccounts_NilAuditLogger_NoPanic(t *testing.T) {
	accountRepo := &MockAccountRepository{}
	svc := reconcileTestService(accountRepo, nil)

	accountRepo.On("UpdateStatus", mock.Anything, "acct-gone", "user-1", domain.AccountStatusRemoved, (*string)(nil)).
		Return(nil).Once()

	roster := PlaidAccountRosterFrom([]plaid.AccountBase{{AccountId: "plaid-live"}}, nil)
	require.NotPanics(t, func() {
		svc.reconcileRemovedAccounts(context.Background(), reconcileTestItem(), roster, []domain.Account{
			reconcileTestAccount("acct-gone", "plaid-gone"),
		})
	})
	accountRepo.AssertExpectations(t)
}

// TestReconcileRemovedAccounts_UpdateFailureDoesNotStopThePass: one row failing (deadlock,
// transient) must not leave the rest of the item unreconciled.
func TestReconcileRemovedAccounts_UpdateFailureDoesNotStopThePass(t *testing.T) {
	accountRepo := &MockAccountRepository{}
	audit := &MockAuditLogger{}
	svc := reconcileTestService(accountRepo, audit)

	accountRepo.On("UpdateStatus", mock.Anything, "acct-fail", "user-1", domain.AccountStatusRemoved, (*string)(nil)).
		Return(errors.New("deadlock detected")).Once()
	accountRepo.On("UpdateStatus", mock.Anything, "acct-ok", "user-1", domain.AccountStatusRemoved, (*string)(nil)).
		Return(nil).Once()
	audit.On("Log", mock.Anything, "account.removed_by_provider", "account", "acct-ok").Return(nil).Once()

	roster := PlaidAccountRosterFrom([]plaid.AccountBase{{AccountId: "plaid-live"}}, nil)
	removed := svc.reconcileRemovedAccounts(context.Background(), reconcileTestItem(), roster, []domain.Account{
		reconcileTestAccount("acct-fail", "plaid-fail"),
		reconcileTestAccount("acct-ok", "plaid-ok"),
	})

	assert.Equal(t, 1, removed)
	accountRepo.AssertExpectations(t)
	audit.AssertNumberOfCalls(t, "Log", 1)
}
