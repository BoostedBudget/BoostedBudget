package services

// Issue #1569 Task 6: checkFingerprintOwnership's ExpireAccountsByFingerprint call writes into
// account_status_history (via the accounts trigger) with no audit trail and, until this task, no
// timestamp of its own — a cross-user side effect landing unaudited in the exact liveness record
// this branch exists to make trustworthy. See account_repository.go's ExpireAccountsByFingerprint
// and migration 000155 for the expired_at half of the fix; these tests cover the audit half.
//
// THE ACTOR PROBLEM: the expired account belongs to ownership.UserID (the dispossessed owner), but
// the write is triggered by userID (the acting LinkAccounts caller). Neither is the "actor" of this
// audit entry in any honest sense — userID never touched ownership.UserID's account, and
// ownership.UserID did nothing at all. AuditLogger.Log carries no actor parameter (see
// audit_log_repository.go's writeAuditLogEntry doc comment: user_id is always written NULL), which
// is exactly right here — these tests confirm the call site does not try to smuggle either user in
// some other way (e.g. into resourceID) as a stand-in for an actor the interface has no room for.

import (
	"context"
	"log/slog"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/repository"
)

// TestCheckFingerprintOwnership_ExpiryAuditsEachExpiredAccount proves the audit gap plaid_account_
// reconciliation.go's own comment calls out ("the expiry path does not [audit-log]") is closed: one
// account.expired_by_fingerprint_release entry per account ExpireAccountsByFingerprint actually
// flipped, naming the SUBJECT account — never the acting user's own account, and never any account
// this fingerprint release did not touch.
func TestCheckFingerprintOwnership_ExpiryAuditsEachExpiredAccount(t *testing.T) {
	mockAccountRepo := &MockAccountRepository{}
	mockAuditLogger := &MockAuditLogger{}

	svc := &accountService{
		accountRepo: mockAccountRepo,
		auditLogger: mockAuditLogger,
		logger:      slog.Default(),
	}

	ctx := context.Background()
	actingUserID := uuid.New().String()
	dispossessedOwnerID := uuid.New().String()
	blindIdx := "test-fingerprint-blind-idx"
	otherItemID := uuid.New().String()
	expiredAccountID1 := uuid.New().String()
	expiredAccountID2 := uuid.New().String()

	statusChangedAt := time.Now().Add(-91 * 24 * time.Hour) // past the 90-day TTL
	mockAccountRepo.On("CheckFingerprintOwnership", ctx, blindIdx, actingUserID).
		Return(&repository.FingerprintOwnership{
			UserID:          dispossessedOwnerID,
			LinkedItemID:    otherItemID,
			ItemStatus:      domain.LinkedItemStatusDisconnected,
			StatusChangedAt: &statusChangedAt,
		}, nil)
	mockAccountRepo.On("ExpireAccountsByFingerprint", ctx, blindIdx).
		Return([]string{expiredAccountID1, expiredAccountID2}, nil)

	mockAuditLogger.On("Log", ctx, "account.expired_by_fingerprint_release", "account", expiredAccountID1).Return(nil)
	mockAuditLogger.On("Log", ctx, "account.expired_by_fingerprint_release", "account", expiredAccountID2).Return(nil)

	err := svc.checkFingerprintOwnership(ctx, blindIdx, actingUserID)

	require.NoError(t, err)
	mockAuditLogger.AssertExpectations(t)
	mockAuditLogger.AssertNotCalled(t, "Log", ctx, "account.expired_by_fingerprint_release", "account", actingUserID)
}

// TestCheckFingerprintOwnership_ExpiryAudit_NoExpiredAccounts_NoAuditCall proves the audit call is
// driven entirely by what ExpireAccountsByFingerprint actually returned — if nothing was flipped
// (already non-active, or a race with a concurrent release), nothing is logged. A hardcoded log
// call using the caller's own blind-index lookup (rather than the repo's return value) would fail
// this test by logging regardless.
func TestCheckFingerprintOwnership_ExpiryAudit_NoExpiredAccounts_NoAuditCall(t *testing.T) {
	mockAccountRepo := &MockAccountRepository{}
	mockAuditLogger := &MockAuditLogger{}

	svc := &accountService{
		accountRepo: mockAccountRepo,
		auditLogger: mockAuditLogger,
		logger:      slog.Default(),
	}

	ctx := context.Background()
	actingUserID := uuid.New().String()
	dispossessedOwnerID := uuid.New().String()
	blindIdx := "test-fingerprint-blind-idx-none"
	otherItemID := uuid.New().String()

	statusChangedAt := time.Now().Add(-91 * 24 * time.Hour)
	mockAccountRepo.On("CheckFingerprintOwnership", ctx, blindIdx, actingUserID).
		Return(&repository.FingerprintOwnership{
			UserID:          dispossessedOwnerID,
			LinkedItemID:    otherItemID,
			ItemStatus:      domain.LinkedItemStatusDisconnected,
			StatusChangedAt: &statusChangedAt,
		}, nil)
	mockAccountRepo.On("ExpireAccountsByFingerprint", ctx, blindIdx).Return(nil, nil)

	err := svc.checkFingerprintOwnership(ctx, blindIdx, actingUserID)

	require.NoError(t, err)
	mockAuditLogger.AssertNotCalled(t, "Log", mock.Anything, mock.Anything, mock.Anything, mock.Anything)
}

// TestCheckFingerprintOwnership_ExpiryAudit_NilLoggerStillSucceeds mirrors the nil-safety guarantee
// every other audit call site in this file already has: fingerprint release must not start
// requiring an audit logger to function.
func TestCheckFingerprintOwnership_ExpiryAudit_NilLoggerStillSucceeds(t *testing.T) {
	mockAccountRepo := &MockAccountRepository{}

	svc := &accountService{
		accountRepo: mockAccountRepo,
		auditLogger: nil,
		logger:      slog.Default(),
	}

	ctx := context.Background()
	actingUserID := uuid.New().String()
	dispossessedOwnerID := uuid.New().String()
	blindIdx := "test-fingerprint-blind-idx-nil-logger"
	otherItemID := uuid.New().String()
	expiredAccountID := uuid.New().String()

	statusChangedAt := time.Now().Add(-91 * 24 * time.Hour)
	mockAccountRepo.On("CheckFingerprintOwnership", ctx, blindIdx, actingUserID).
		Return(&repository.FingerprintOwnership{
			UserID:          dispossessedOwnerID,
			LinkedItemID:    otherItemID,
			ItemStatus:      domain.LinkedItemStatusDisconnected,
			StatusChangedAt: &statusChangedAt,
		}, nil)
	mockAccountRepo.On("ExpireAccountsByFingerprint", ctx, blindIdx).Return([]string{expiredAccountID}, nil)

	err := svc.checkFingerprintOwnership(ctx, blindIdx, actingUserID)

	require.NoError(t, err, "a nil auditLogger must not fail fingerprint release")
}

// TestCheckFingerprintOwnership_ExpiryAudit_LogFailureNonFatal proves the same "already committed,
// don't abort" discipline every other audit call in this file follows: the expiry already
// committed by the time the audit call runs, so a logging failure must not be surfaced as a
// LinkAccounts/checkFingerprintOwnership error.
func TestCheckFingerprintOwnership_ExpiryAudit_LogFailureNonFatal(t *testing.T) {
	mockAccountRepo := &MockAccountRepository{}
	mockAuditLogger := &MockAuditLogger{}

	svc := &accountService{
		accountRepo: mockAccountRepo,
		auditLogger: mockAuditLogger,
		logger:      slog.Default(),
	}

	ctx := context.Background()
	actingUserID := uuid.New().String()
	dispossessedOwnerID := uuid.New().String()
	blindIdx := "test-fingerprint-blind-idx-log-fail"
	otherItemID := uuid.New().String()
	expiredAccountID := uuid.New().String()

	statusChangedAt := time.Now().Add(-91 * 24 * time.Hour)
	mockAccountRepo.On("CheckFingerprintOwnership", ctx, blindIdx, actingUserID).
		Return(&repository.FingerprintOwnership{
			UserID:          dispossessedOwnerID,
			LinkedItemID:    otherItemID,
			ItemStatus:      domain.LinkedItemStatusDisconnected,
			StatusChangedAt: &statusChangedAt,
		}, nil)
	mockAccountRepo.On("ExpireAccountsByFingerprint", ctx, blindIdx).Return([]string{expiredAccountID}, nil)
	mockAuditLogger.On("Log", ctx, "account.expired_by_fingerprint_release", "account", expiredAccountID).
		Return(assert.AnError)

	err := svc.checkFingerprintOwnership(ctx, blindIdx, actingUserID)

	require.NoError(t, err, "an audit-write failure must not fail an already-committed expiry")
	// The failing Log call must actually have been attempted, not skipped.
	mockAuditLogger.AssertExpectations(t)
}
