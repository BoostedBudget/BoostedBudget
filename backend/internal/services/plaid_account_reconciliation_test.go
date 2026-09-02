package services

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"

	"github.com/plaid/plaid-go/v23/plaid"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/observability"
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
// (MarkRemovedByProvider — the same statement as UpdateStatus, stamping removed_at and letting the
// history trigger close the interval, plus removed_reason='provider_absent'), and
// the removal is audit-logged so "why did this disappear?" has an answer.
func TestReconcileRemovedAccounts_AbsentFromCompleteResponse_MarksRemoved(t *testing.T) {
	accountRepo := &MockAccountRepository{}
	audit := &MockAuditLogger{}
	svc := reconcileTestService(accountRepo, audit)

	accountRepo.On("MarkRemovedByProvider", mock.Anything, "acct-gone", "user-1").
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
	accountRepo.AssertNotCalled(t, "MarkRemovedByProvider", mock.Anything, "acct-live", mock.Anything)
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
	accountRepo.AssertNotCalled(t, "MarkRemovedByProvider", mock.Anything, mock.Anything, mock.Anything)
	audit.AssertNotCalled(t, "Log", mock.Anything, mock.Anything, mock.Anything, mock.Anything)
}

// TestReconcileRemovedAccounts_ZeroAccountsResponse_MarksNothing is THE catastrophic case.
//
// An errored, throttled, or degraded /accounts/get can come back with an empty `accounts` array.
// Read as "Plaid enumerated everything and none of your accounts exist any more," that sweeps a
// user's ENTIRE portfolio to 'removed' in one run and zeroes their net worth. It must mark
// NOTHING. Note the mock carries no MarkRemovedByProvider expectation at all: any call is a hard
// failure.
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
			accountRepo.AssertNotCalled(t, "MarkRemovedByProvider", mock.Anything, mock.Anything, mock.Anything)
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
	accountRepo.AssertNotCalled(t, "MarkRemovedByProvider", mock.Anything, mock.Anything, mock.Anything)
	audit.AssertNotCalled(t, "Log", mock.Anything, mock.Anything, mock.Anything, mock.Anything)
}

// TestReconcileRemovedAccounts_AlreadyDisconnected_NotReTransitioned — brief test 5. Task 2 gave
// the removal statement a status guard: it refuses a non-active row with ErrAccountAlreadyRemoved (the
// idempotency case) or ErrAccountAlreadyDisconnected ('replaced'/'expired' — a more specific
// terminal reason this sweep must not overwrite). Reconciliation cooperates: it swallows both,
// audit-logs NEITHER (nothing happened to log), and keeps going for the remaining accounts.
func TestReconcileRemovedAccounts_AlreadyDisconnected_NotReTransitioned(t *testing.T) {
	accountRepo := &MockAccountRepository{}
	audit := &MockAuditLogger{}
	svc := reconcileTestService(accountRepo, audit)

	accountRepo.On("MarkRemovedByProvider", mock.Anything, "acct-replaced", "user-1").
		Return(repository.ErrAccountAlreadyDisconnected).Once()
	accountRepo.On("MarkRemovedByProvider", mock.Anything, "acct-already", "user-1").
		Return(repository.ErrAccountAlreadyRemoved).Once()
	accountRepo.On("MarkRemovedByProvider", mock.Anything, "acct-gone", "user-1").
		Return(nil).Once()
	audit.On("Log", mock.Anything, "account.removed_by_provider", "account", "acct-gone").Return(nil).Once()

	roster := PlaidAccountRosterFrom([]plaid.AccountBase{{AccountId: "plaid-live"}}, nil)
	removed := svc.reconcileRemovedAccounts(context.Background(), reconcileTestItem(), roster, []domain.Account{
		reconcileTestAccount("acct-replaced", "plaid-replaced"),
		reconcileTestAccount("acct-already", "plaid-already"),
		reconcileTestAccount("acct-gone", "plaid-gone"),
		// A survivor, so the all-or-nothing bound (which is exercised on its own below) is not
		// what this test is measuring.
		reconcileTestAccount("acct-live", "plaid-live"),
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
	accountRepo.AssertNotCalled(t, "MarkRemovedByProvider", mock.Anything, mock.Anything, mock.Anything)
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
	accountRepo.AssertNotCalled(t, "MarkRemovedByProvider", mock.Anything, mock.Anything, mock.Anything)
}

// TestReconcileRemovedAccounts_AuditFailureIsNonFatal: the account is already removed by the time
// the audit write runs, so an audit failure must not abort the pass or the remaining accounts.
// Same "already committed, don't abort" reasoning as DisconnectAccount's audit call.
func TestReconcileRemovedAccounts_AuditFailureIsNonFatal(t *testing.T) {
	accountRepo := &MockAccountRepository{}
	audit := &MockAuditLogger{}
	svc := reconcileTestService(accountRepo, audit)

	accountRepo.On("MarkRemovedByProvider", mock.Anything, mock.Anything, "user-1").
		Return(nil).Twice()
	audit.On("Log", mock.Anything, "account.removed_by_provider", "account", mock.Anything).
		Return(errors.New("audit db down")).Twice()

	roster := PlaidAccountRosterFrom([]plaid.AccountBase{{AccountId: "plaid-live"}}, nil)
	removed := svc.reconcileRemovedAccounts(context.Background(), reconcileTestItem(), roster, []domain.Account{
		reconcileTestAccount("acct-gone-1", "plaid-gone-1"),
		reconcileTestAccount("acct-gone-2", "plaid-gone-2"),
		reconcileTestAccount("acct-live", "plaid-live"), // survivor: not an all-item sweep
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

	accountRepo.On("MarkRemovedByProvider", mock.Anything, "acct-gone", "user-1").
		Return(nil).Once()

	roster := PlaidAccountRosterFrom([]plaid.AccountBase{{AccountId: "plaid-live"}}, nil)
	require.NotPanics(t, func() {
		svc.reconcileRemovedAccounts(context.Background(), reconcileTestItem(), roster, []domain.Account{
			reconcileTestAccount("acct-gone", "plaid-gone"),
			reconcileTestAccount("acct-live", "plaid-live"), // survivor: not an all-item sweep
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

	accountRepo.On("MarkRemovedByProvider", mock.Anything, "acct-fail", "user-1").
		Return(errors.New("deadlock detected")).Once()
	accountRepo.On("MarkRemovedByProvider", mock.Anything, "acct-ok", "user-1").
		Return(nil).Once()
	audit.On("Log", mock.Anything, "account.removed_by_provider", "account", "acct-ok").Return(nil).Once()

	roster := PlaidAccountRosterFrom([]plaid.AccountBase{{AccountId: "plaid-live"}}, nil)
	removed := svc.reconcileRemovedAccounts(context.Background(), reconcileTestItem(), roster, []domain.Account{
		reconcileTestAccount("acct-fail", "plaid-fail"),
		reconcileTestAccount("acct-ok", "plaid-ok"),
		reconcileTestAccount("acct-live", "plaid-live"), // survivor: not an all-item sweep
	})

	assert.Equal(t, 1, removed)
	accountRepo.AssertExpectations(t)
	audit.AssertNumberOfCalls(t, "Log", 1)
}

// ---------------------------------------------------------------------------
// Fix round 1: the all-or-nothing bound.
//
// A removal is PERMANENT — nothing in this codebase writes an account back to 'active', and there
// is no un-remove path or UI — so the degenerate shape is refused outright: if every active
// account under an item would go at once, none of them does.
// ---------------------------------------------------------------------------

// TestReconcileRemovedAccounts_RefusesToRemoveEveryAccountOfAnItem: a complete, non-empty Plaid
// response that matches NONE of the item's accounts is an anomaly (rotated ids, or a response for
// a different item), not a mass closure — and this sync cannot create replacement rows to
// compensate. Nothing is written.
func TestReconcileRemovedAccounts_RefusesToRemoveEveryAccountOfAnItem(t *testing.T) {
	accountRepo := &MockAccountRepository{}
	audit := &MockAuditLogger{}
	svc := reconcileTestService(accountRepo, audit)

	// Plaid returned real accounts — the roster IS complete — but none of them are ours.
	roster := PlaidAccountRosterFrom([]plaid.AccountBase{
		{AccountId: "plaid-rotated-1"},
		{AccountId: "plaid-rotated-2"},
	}, nil)
	removed := svc.reconcileRemovedAccounts(context.Background(), reconcileTestItem(), roster, []domain.Account{
		reconcileTestAccount("acct-a", "plaid-a"),
		reconcileTestAccount("acct-b", "plaid-b"),
		reconcileTestAccount("acct-c", "plaid-c"),
	})

	assert.Equal(t, 0, removed, "an all-item sweep must be refused outright")
	accountRepo.AssertNotCalled(t, "MarkRemovedByProvider", mock.Anything, mock.Anything, mock.Anything)
	audit.AssertNotCalled(t, "Log", mock.Anything, mock.Anything, mock.Anything, mock.Anything)
}

// TestReconcileRemovedAccounts_SingleAccountItemIsNotExempt pins the edge case CONSCIOUSLY, so a
// future reader does not "fix" it. For a one-account item, "complete roster, zero matches" is the
// id-rotation shape rather than a closure (a genuine whole-institution closure arrives as an EMPTY
// accounts array, which the completeness guard already refuses) — so the bound applies here too.
// The cost is a real single-account closure staying stale until the user disconnects it, which is
// recoverable; removing it wrongly is not.
func TestReconcileRemovedAccounts_SingleAccountItemIsNotExempt(t *testing.T) {
	accountRepo := &MockAccountRepository{}
	svc := reconcileTestService(accountRepo, nil)

	roster := PlaidAccountRosterFrom([]plaid.AccountBase{{AccountId: "plaid-someone-elses"}}, nil)
	removed := svc.reconcileRemovedAccounts(context.Background(), reconcileTestItem(), roster, []domain.Account{
		reconcileTestAccount("acct-only", "plaid-only"),
	})

	assert.Equal(t, 0, removed)
	accountRepo.AssertNotCalled(t, "MarkRemovedByProvider", mock.Anything, mock.Anything, mock.Anything)
}

// TestReconcileRemovedAccounts_BoundIsAllOrNothingNotAMajority proves the bound is exactly the
// degenerate case and no wider: with one survivor out of three, the other two DO get removed.
// Without this, "refuse everything" and "refuse when most would go" both pass the test above.
func TestReconcileRemovedAccounts_BoundIsAllOrNothingNotAMajority(t *testing.T) {
	accountRepo := &MockAccountRepository{}
	audit := &MockAuditLogger{}
	svc := reconcileTestService(accountRepo, audit)

	accountRepo.On("MarkRemovedByProvider", mock.Anything, "acct-a", "user-1").Return(nil).Once()
	accountRepo.On("MarkRemovedByProvider", mock.Anything, "acct-b", "user-1").Return(nil).Once()
	audit.On("Log", mock.Anything, "account.removed_by_provider", "account", mock.Anything).Return(nil).Twice()

	roster := PlaidAccountRosterFrom([]plaid.AccountBase{{AccountId: "plaid-c"}}, nil)
	removed := svc.reconcileRemovedAccounts(context.Background(), reconcileTestItem(), roster, []domain.Account{
		reconcileTestAccount("acct-a", "plaid-a"),
		reconcileTestAccount("acct-b", "plaid-b"),
		reconcileTestAccount("acct-c", "plaid-c"),
	})

	assert.Equal(t, 2, removed, "two of three is not an all-item sweep and must proceed")
	accountRepo.AssertExpectations(t)
}

// TestReconcileRemovedAccounts_ForeignAccountDoesNotUnlockTheBound: the ownership filter runs
// BEFORE the bound is measured, so an account this pass could never write to cannot be counted as
// "the survivor" that makes a sweep look partial. Item has two rows; one belongs to another user;
// the only writable one is absent from Plaid — that IS an all-item sweep of what we may touch.
func TestReconcileRemovedAccounts_ForeignAccountDoesNotUnlockTheBound(t *testing.T) {
	accountRepo := &MockAccountRepository{}
	svc := reconcileTestService(accountRepo, nil)

	foreign := reconcileTestAccount("acct-foreign", "plaid-foreign")
	foreign.UserID = "user-2"

	roster := PlaidAccountRosterFrom([]plaid.AccountBase{{AccountId: "plaid-unrelated"}}, nil)
	removed := svc.reconcileRemovedAccounts(context.Background(), reconcileTestItem(), roster, []domain.Account{
		foreign,
		reconcileTestAccount("acct-mine", "plaid-mine"),
	})

	assert.Equal(t, 0, removed)
	accountRepo.AssertNotCalled(t, "MarkRemovedByProvider", mock.Anything, mock.Anything, mock.Anything)
}

// TestReconcileRemovedAccounts_MetricRecordsOutcomes: a mass sweep must be visible in metrics, not
// only in logs. Reads deltas rather than absolute values — the collector is a package-level
// registry shared with every other test in this package.
func TestReconcileRemovedAccounts_MetricRecordsOutcomes(t *testing.T) {
	read := func(outcome string) float64 {
		return testutil.ToFloat64(observability.PlaidProviderAccountRemoval.WithLabelValues(outcome))
	}
	beforeRemoved := read("removed")
	beforeRefused := read("refused_full_item_sweep")
	beforeSkipped := read("skipped_incomplete_response")

	// A genuine removal, with a survivor so the bound does not trip.
	accountRepo := &MockAccountRepository{}
	svc := reconcileTestService(accountRepo, nil)
	accountRepo.On("MarkRemovedByProvider", mock.Anything, "acct-gone", "user-1").Return(nil).Once()
	svc.reconcileRemovedAccounts(context.Background(), reconcileTestItem(),
		PlaidAccountRosterFrom([]plaid.AccountBase{{AccountId: "plaid-live"}}, nil),
		[]domain.Account{
			reconcileTestAccount("acct-gone", "plaid-gone"),
			reconcileTestAccount("acct-live", "plaid-live"),
		})

	// A refused all-item sweep of three accounts.
	svc2 := reconcileTestService(&MockAccountRepository{}, nil)
	svc2.reconcileRemovedAccounts(context.Background(), reconcileTestItem(),
		PlaidAccountRosterFrom([]plaid.AccountBase{{AccountId: "plaid-rotated"}}, nil),
		[]domain.Account{
			reconcileTestAccount("acct-a", "plaid-a"),
			reconcileTestAccount("acct-b", "plaid-b"),
			reconcileTestAccount("acct-c", "plaid-c"),
		})

	// A pass declined for an untrustworthy response.
	svc3 := reconcileTestService(&MockAccountRepository{}, nil)
	svc3.reconcileRemovedAccounts(context.Background(), reconcileTestItem(),
		PlaidAccountRosterFrom(nil, nil),
		[]domain.Account{reconcileTestAccount("acct-a", "plaid-a")})

	assert.Equal(t, float64(1), read("removed")-beforeRemoved)
	assert.Equal(t, float64(3), read("refused_full_item_sweep")-beforeRefused,
		"the refusal counter must record the SIZE of the sweep it prevented")
	assert.Equal(t, float64(1), read("skipped_incomplete_response")-beforeSkipped)
}

// ---------------------------------------------------------------------------
// Fix round 2: auto-restore of provider-removed accounts.
//
// A removal used to be permanent — nothing else in this codebase writes an account back to
// 'active'. If Plaid starts returning an account we marked removed, the removal was wrong (or the
// closure was reversed) and the honest current state is 'active'. The one thing this must never do
// is resurrect an account the USER disconnected.
// ---------------------------------------------------------------------------

// removedTestAccount is a PROVIDER-removed account: status 'removed' with the cause recorded, which
// is the only shape eligible for restore. Use userRemovedTestAccount or legacyRemovedTestAccount for
// the two ineligible shapes.
func removedTestAccount(id, plaidID string) domain.Account {
	a := reconcileTestAccount(id, plaidID)
	a.Status = domain.AccountStatusRemoved
	reason := string(domain.RemovalReasonProviderAbsent)
	a.RemovedReason = &reason
	return a
}

// userRemovedTestAccount is an account the USER disconnected. It must never be restored.
func userRemovedTestAccount(id, plaidID string) domain.Account {
	a := removedTestAccount(id, plaidID)
	reason := string(domain.RemovalReasonUserDisconnect)
	a.RemovedReason = &reason
	return a
}

// legacyRemovedTestAccount is a 'removed' row written before migration 000154: no recorded cause.
// NULL is read as "not provider-removed", the safe direction.
func legacyRemovedTestAccount(id, plaidID string) domain.Account {
	a := removedTestAccount(id, plaidID)
	a.RemovedReason = nil
	return a
}

// TestRestoreProviderRemovedAccounts_ReappearedAccountIsRestored: the happy path — restored via the
// repository's guarded write, and audit-logged under an action distinct from both removal actions.
func TestRestoreProviderRemovedAccounts_ReappearedAccountIsRestored(t *testing.T) {
	accountRepo := &MockAccountRepository{}
	audit := &MockAuditLogger{}
	svc := reconcileTestService(accountRepo, audit)

	accountRepo.On("RestoreProviderRemovedAccount", mock.Anything, "acct-back", "user-1").Return(nil).Once()
	audit.On("Log", mock.Anything, "account.restored_by_provider", "account", "acct-back").Return(nil).Once()

	roster := PlaidAccountRosterFrom([]plaid.AccountBase{{AccountId: "plaid-back"}}, nil)
	restored := svc.restoreProviderRemovedAccounts(context.Background(), reconcileTestItem(), roster, []domain.Account{
		removedTestAccount("acct-back", "plaid-back"),
	})

	assert.Equal(t, 1, restored)
	accountRepo.AssertExpectations(t)
	audit.AssertExpectations(t)
}

// TestRestoreProviderRemovedAccounts_UserDisconnectedIsNotRestored is THE dangerous direction.
//
// The user asked for this account to be gone; the bank still reporting it is not a counter-argument.
// The service now recognises the cause itself and never even asks the repository — which is both the
// second layer of the guarantee and the reason a user who disconnected an account they still hold at
// the bank does not pay a SELECT ... FOR UPDATE round trip on every sync forever.
//
// ⚠ This test pins the OPTIMISATION AND SECOND LAYER, not the guarantee. The guarantee is the SQL
// predicate, and it is pinned by TestRestore_Integration_UserDisconnectedAccountIsNeverRestored plus
// TestRestoreProviderRemovedAccounts_RepositoryStillRefusesWhenTheServiceFilterIsBypassed below —
// deleting the service filter must not be enough to resurrect anything.
func TestRestoreProviderRemovedAccounts_UserDisconnectedIsNotRestored(t *testing.T) {
	accountRepo := &MockAccountRepository{}
	audit := &MockAuditLogger{}
	svc := reconcileTestService(accountRepo, audit)

	roster := PlaidAccountRosterFrom([]plaid.AccountBase{{AccountId: "plaid-user-removed"}}, nil)
	restored := svc.restoreProviderRemovedAccounts(context.Background(), reconcileTestItem(), roster, []domain.Account{
		userRemovedTestAccount("acct-user-removed", "plaid-user-removed"),
	})

	assert.Equal(t, 0, restored)
	accountRepo.AssertNotCalled(t, "RestoreProviderRemovedAccount", mock.Anything, mock.Anything, mock.Anything)
	// No second attempt through any other door either.
	accountRepo.AssertNotCalled(t, "UpdateStatus", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything)
	audit.AssertNotCalled(t, "Log", mock.Anything, mock.Anything, mock.Anything, mock.Anything)
}

// TestRestoreProviderRemovedAccounts_LegacyNullReasonIsNotRestored: a 'removed' row from before
// migration 000154 has no recorded cause. NULL means "not provider-removed" — not eligible, and not
// worth a round trip to be told so.
func TestRestoreProviderRemovedAccounts_LegacyNullReasonIsNotRestored(t *testing.T) {
	accountRepo := &MockAccountRepository{}
	svc := reconcileTestService(accountRepo, nil)

	roster := PlaidAccountRosterFrom([]plaid.AccountBase{{AccountId: "plaid-legacy"}}, nil)
	restored := svc.restoreProviderRemovedAccounts(context.Background(), reconcileTestItem(), roster, []domain.Account{
		legacyRemovedTestAccount("acct-legacy", "plaid-legacy"),
	})

	assert.Equal(t, 0, restored)
	accountRepo.AssertNotCalled(t, "RestoreProviderRemovedAccount", mock.Anything, mock.Anything, mock.Anything)
}

// TestRestoreProviderRemovedAccounts_RepositoryStillRefusesWhenTheServiceFilterIsBypassed is the
// test that would survive the service filter being deleted, mis-implemented, or fed accounts from a
// query that does not populate RemovedReason.
//
// It simulates precisely that: an account whose in-memory RemovedReason SAYS 'provider_absent' while
// the database disagrees, so the service filter waves it through and the repository is the only
// thing left. The repository answers ErrAccountNotRestorable and the service must honour that —
// no retry, no other write path, and above all no audit entry claiming a restore that never
// happened. The real SQL behind that answer is pinned in the integration suite.
func TestRestoreProviderRemovedAccounts_RepositoryStillRefusesWhenTheServiceFilterIsBypassed(t *testing.T) {
	accountRepo := &MockAccountRepository{}
	audit := &MockAuditLogger{}
	svc := reconcileTestService(accountRepo, audit)

	accountRepo.On("RestoreProviderRemovedAccount", mock.Anything, "acct-stale", "user-1").
		Return(repository.ErrAccountNotRestorable).Once()

	roster := PlaidAccountRosterFrom([]plaid.AccountBase{{AccountId: "plaid-stale"}}, nil)
	restored := svc.restoreProviderRemovedAccounts(context.Background(), reconcileTestItem(), roster, []domain.Account{
		removedTestAccount("acct-stale", "plaid-stale"), // in-memory cause says provider; the DB will not
	})

	assert.Equal(t, 0, restored)
	accountRepo.AssertExpectations(t)
	accountRepo.AssertNumberOfCalls(t, "RestoreProviderRemovedAccount", 1)
	audit.AssertNotCalled(t, "Log", mock.Anything, mock.Anything, mock.Anything, mock.Anything)
}

// TestRestoreProviderRemovedAccounts_PartialRosterRestoresNothing: an account "reappearing" in a
// partial or errored response proves nothing, exactly as its absence proves nothing on the removal
// side. Note the account IS in the payload — only the roster's completeness differs.
func TestRestoreProviderRemovedAccounts_PartialRosterRestoresNothing(t *testing.T) {
	for name, roster := range map[string]PlaidAccountRoster{
		"errored call":   PlaidAccountRosterFrom([]plaid.AccountBase{{AccountId: "plaid-back"}}, errors.New("plaid: 500")),
		"empty accounts": PlaidAccountRosterFrom(nil, nil),
		"blank ids only": PlaidAccountRosterFrom([]plaid.AccountBase{{AccountId: ""}}, nil),
	} {
		t.Run(name, func(t *testing.T) {
			accountRepo := &MockAccountRepository{}
			svc := reconcileTestService(accountRepo, nil)

			restored := svc.restoreProviderRemovedAccounts(context.Background(), reconcileTestItem(), roster, []domain.Account{
				removedTestAccount("acct-back", "plaid-back"),
			})

			assert.Equal(t, 0, restored)
			accountRepo.AssertNotCalled(t, "RestoreProviderRemovedAccount", mock.Anything, mock.Anything, mock.Anything)
		})
	}
}

// TestRestoreProviderRemovedAccounts_StillAbsentIsLeftAlone: a removed account Plaid still does not
// return stays removed, and is not even asked about.
func TestRestoreProviderRemovedAccounts_StillAbsentIsLeftAlone(t *testing.T) {
	accountRepo := &MockAccountRepository{}
	svc := reconcileTestService(accountRepo, nil)

	roster := PlaidAccountRosterFrom([]plaid.AccountBase{{AccountId: "plaid-other"}}, nil)
	restored := svc.restoreProviderRemovedAccounts(context.Background(), reconcileTestItem(), roster, []domain.Account{
		removedTestAccount("acct-gone", "plaid-gone"),
	})

	assert.Equal(t, 0, restored)
	accountRepo.AssertNotCalled(t, "RestoreProviderRemovedAccount", mock.Anything, mock.Anything, mock.Anything)
}

// TestRestoreProviderRemovedAccounts_ActiveAccountsAreNotTouched: only 'removed' rows are
// candidates. An active account Plaid returns is simply normal.
func TestRestoreProviderRemovedAccounts_ActiveAccountsAreNotTouched(t *testing.T) {
	accountRepo := &MockAccountRepository{}
	svc := reconcileTestService(accountRepo, nil)

	roster := PlaidAccountRosterFrom([]plaid.AccountBase{{AccountId: "plaid-live"}}, nil)
	restored := svc.restoreProviderRemovedAccounts(context.Background(), reconcileTestItem(), roster, []domain.Account{
		reconcileTestAccount("acct-live", "plaid-live"),
	})

	assert.Equal(t, 0, restored)
	accountRepo.AssertNotCalled(t, "RestoreProviderRemovedAccount", mock.Anything, mock.Anything, mock.Anything)
}

// TestRestoreProviderRemovedAccounts_ForeignAccountIsNotRestored: the ownership guard, mirroring
// the removal side — GetByLinkedItemIDIncludingRetained is not user-scoped.
func TestRestoreProviderRemovedAccounts_ForeignAccountIsNotRestored(t *testing.T) {
	accountRepo := &MockAccountRepository{}
	svc := reconcileTestService(accountRepo, nil)

	foreign := removedTestAccount("acct-foreign", "plaid-back")
	foreign.UserID = "user-2"

	roster := PlaidAccountRosterFrom([]plaid.AccountBase{{AccountId: "plaid-back"}}, nil)
	restored := svc.restoreProviderRemovedAccounts(context.Background(), reconcileTestItem(), roster, []domain.Account{foreign})

	assert.Equal(t, 0, restored)
	accountRepo.AssertNotCalled(t, "RestoreProviderRemovedAccount", mock.Anything, mock.Anything, mock.Anything)
}

// TestRestoreProviderRemovedAccounts_WriteFailureIsNonFatal: a transient failure must not abort the
// pass, and must not be audit-logged as a restore.
func TestRestoreProviderRemovedAccounts_WriteFailureIsNonFatal(t *testing.T) {
	accountRepo := &MockAccountRepository{}
	audit := &MockAuditLogger{}
	svc := reconcileTestService(accountRepo, audit)

	accountRepo.On("RestoreProviderRemovedAccount", mock.Anything, "acct-fail", "user-1").
		Return(errors.New("deadlock detected")).Once()
	accountRepo.On("RestoreProviderRemovedAccount", mock.Anything, "acct-ok", "user-1").Return(nil).Once()
	audit.On("Log", mock.Anything, "account.restored_by_provider", "account", "acct-ok").Return(nil).Once()

	roster := PlaidAccountRosterFrom([]plaid.AccountBase{
		{AccountId: "plaid-fail"},
		{AccountId: "plaid-ok"},
	}, nil)
	restored := svc.restoreProviderRemovedAccounts(context.Background(), reconcileTestItem(), roster, []domain.Account{
		removedTestAccount("acct-fail", "plaid-fail"),
		removedTestAccount("acct-ok", "plaid-ok"),
	})

	assert.Equal(t, 1, restored)
	accountRepo.AssertExpectations(t)
	audit.AssertNumberOfCalls(t, "Log", 1)
}
