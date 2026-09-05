package services

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/repository"
	"github.com/maxnussbaum/BoostedBudget/internal/testsupport/mockassert"
)

// Issue #1569 round 17 task 11 — control 2 ("an audit row written in the SAME transaction") was
// only as strong as one SetAuditLogRepository line in cmd/server/main.go that nothing checked.
// The repository purge methods skip the audit write when the sink is nil, so dropping that line
// left the destruction working and the trail silently absent, with every test still green.
//
// These tests pin the fail-loud half of the fix: the service refuses the purge outright rather
// than destroying financial history un-audited. The repository must never be reached.
// The wiring half is cmd/server's TestExplicitDestructionAuditSinkIsWiredInMain.

// TestPurgeAccount_RefusesWithoutAuditSink proves accountService.PurgeAccount does not destroy an
// account when its audit sink is unwired.
func TestPurgeAccount_RefusesWithoutAuditSink(t *testing.T) {
	mockAccountRepo := &MockAccountRepository{}
	service := &accountService{accountRepo: mockAccountRepo}

	err := service.PurgeAccount(context.Background(), uuid.New().String(), uuid.New().String())

	require.ErrorIs(t, err, repository.ErrAuditLogRepositoryNotWired)
	mockassert.NoCallTo(t, &mockAccountRepo.Mock, "PurgeAccount")
}

// TestPurgeInstitution_RefusesWithoutAuditSink is the institution-level equal of
// TestPurgeAccount_RefusesWithoutAuditSink.
func TestPurgeInstitution_RefusesWithoutAuditSink(t *testing.T) {
	mockLinkedItemRepo := &MockLinkedItemRepository{}
	service := &accountService{linkedItemRepo: mockLinkedItemRepo}

	err := service.PurgeInstitution(context.Background(), uuid.New().String(), uuid.New().String())

	require.ErrorIs(t, err, repository.ErrAuditLogRepositoryNotWired)
	mockassert.NoCallTo(t, &mockLinkedItemRepo.Mock, "PurgeLinkedItem")
}

// TestPurgeGoal_RefusesWithoutAuditSink is the savings-goal equal of
// TestPurgeAccount_RefusesWithoutAuditSink.
func TestPurgeGoal_RefusesWithoutAuditSink(t *testing.T) {
	mockGoalRepo := &MockSavingsGoalRepository{}
	service := NewSavingsGoalService(mockGoalRepo, nil, testLogger())

	err := service.PurgeGoal(context.Background(), uuid.New().String(), uuid.New().String())

	require.ErrorIs(t, err, repository.ErrAuditLogRepositoryNotWired)
	mockassert.NoCallTo(t, &mockGoalRepo.Mock, "PurgeGoal")
}

// TestPurgeAccount_StillWorksWhenTheAuditSinkIsWired is the control case: the refusals above must
// not have turned the endpoint into a no-op for a correctly wired build.
func TestPurgeAccount_StillWorksWhenTheAuditSinkIsWired(t *testing.T) {
	mockAccountRepo := &MockAccountRepository{}
	mockAuditLogRepo := &MockAuditLogRepository{}
	service := &accountService{accountRepo: mockAccountRepo, auditLogRepo: mockAuditLogRepo}

	ctx := context.Background()
	accountID := uuid.New().String()
	userID := uuid.New().String()

	mockAccountRepo.On("GetByIDAndUser", ctx, accountID, userID).
		Return(&domain.Account{ID: accountID, UserID: userID, LinkedItemID: uuid.New().String()}, nil)
	mockAccountRepo.On("PurgeAccount", ctx, accountID, userID, repository.AuditLogRepository(mockAuditLogRepo)).
		Return(nil)

	assert.NoError(t, service.PurgeAccount(ctx, accountID, userID))
	mockAccountRepo.AssertExpectations(t)
}
