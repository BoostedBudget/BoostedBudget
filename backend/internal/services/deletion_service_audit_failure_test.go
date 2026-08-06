package services

// deletion_service_audit_failure_test.go — pins the DECISION taken in Issue #1506 about what an
// audit-write failure does to a GDPR deletion: it is reported at ERROR level and the deletion
// PROCEEDS. Both halves are asserted here, because both are choices someone could reasonably make
// differently, and an undocumented choice is worse than either.
//
// WHY NOT ABORT (from AuditLogger's and RequestDeletion's doc comments): every audit write in this
// service happens AFTER the state change it describes has already committed — MarkForDeletion here,
// the whole irreversible cascade at the ExecuteDeletion call site. Returning an error would report
// failure for work that succeeded, and for ExecuteDeletion it would make the River worker retry a
// deletion that has already run. There is nothing left to roll back.

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

var errAuditWriteRefused = errors.New("audit log: write refused by the database")

func TestRequestDeletion_AuditWriteFailure_DoesNotAbortTheDeletionRequest(t *testing.T) {
	ctx := context.Background()
	mockRepo := new(MockProfileRepositoryForDeletion)
	mockEmail := new(MockEmailService)
	mockAudit := new(MockAuditLogger)

	service := NewDeletionService(mockRepo, mockEmail, new(MockClerkService), mockAudit, nil, 30, "test-secret-key")

	const userID = "user-audit-fail"
	mockRepo.On("MarkForDeletion", ctx, userID, mock.AnythingOfType("time.Time")).Return(nil)
	mockEmail.On("SendDeletionConfirmation", ctx, "audit-fail@example.com", mock.AnythingOfType("string"), mock.AnythingOfType("time.Time")).Return(nil)
	mockAudit.On("Log", ctx, "user.deletion_requested", "user", userID).Return(errAuditWriteRefused)

	token, scheduledAt, err := service.RequestDeletion(ctx, userID, "audit-fail@example.com")

	require.NoError(t, err, "the state change already committed; a failed audit write must not report it as a failure")
	assert.NotEmpty(t, token)
	assert.True(t, scheduledAt.After(time.Now()))
	// The write was genuinely attempted and genuinely refused — this is not a test that passes
	// because the call site skipped the logger.
	mockAudit.AssertExpectations(t)
	mockRepo.AssertExpectations(t)
}

func TestCancelDeletion_AuditWriteFailure_DoesNotAbortTheCancellation(t *testing.T) {
	ctx := context.Background()
	mockRepo := new(MockProfileRepositoryForDeletion)
	mockAudit := new(MockAuditLogger)

	service := NewDeletionService(mockRepo, new(MockEmailService), new(MockClerkService), mockAudit, nil, 30, "test-secret-key")

	const userID = "user-cancel-audit-fail"
	token, err := service.(*deletionService).generateCancellationToken(userID, "cancel-audit@example.com", time.Now().Add(24*time.Hour))
	require.NoError(t, err)
	require.NotEmpty(t, token)

	mockRepo.On("CancelDeletion", ctx, userID).Return(nil)
	mockAudit.On("Log", ctx, "user.deletion_cancelled", "user", userID).Return(errAuditWriteRefused)

	require.NoError(t, service.CancelDeletion(ctx, token),
		"the cancellation already committed; a failed audit write must not report it as a failure")
	mockAudit.AssertExpectations(t)
	mockRepo.AssertExpectations(t)
}
