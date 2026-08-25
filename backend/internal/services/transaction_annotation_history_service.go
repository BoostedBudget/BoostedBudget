package services

import (
	"context"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
)

// Issue #1569 Task 6 (#1579) — the read half of the annotation trail.
//
// A writer with no reader ships nothing observable, which is the lesson task 5 paid for on
// rule_execution_history: a table, two migrations, and no way for anyone to see a row. The capture
// lands with a repository, this service and an endpoint in the same commit.

// TransactionAnnotationHistoryRepository is the services-package view of the read repository —
// the narrow interface this service actually needs, declared at the consumer as the rest of this
// package does (see RuleExecutionHistoryRepository in rule_application_service.go).
type TransactionAnnotationHistoryRepository interface {
	GetByUserID(ctx context.Context, userID string, limit, offset int) ([]*domain.TransactionAnnotationChange, int, error)
}

// TransactionAnnotationHistoryService reads a user's annotation-overwrite history.
type TransactionAnnotationHistoryService interface {
	// GetUserAnnotationHistory returns the user's annotation changes, newest first, with the total.
	GetUserAnnotationHistory(ctx context.Context, userID string, limit, offset int) ([]*domain.TransactionAnnotationChange, int, error)
}

type transactionAnnotationHistoryService struct {
	repo TransactionAnnotationHistoryRepository
}

// NewTransactionAnnotationHistoryService creates a TransactionAnnotationHistoryService.
func NewTransactionAnnotationHistoryService(repo TransactionAnnotationHistoryRepository) TransactionAnnotationHistoryService {
	return &transactionAnnotationHistoryService{repo: repo}
}

// GetUserAnnotationHistory retrieves the user's whole annotation history, including changes made to
// transactions that have since been tombstoned or purged.
//
// TENANCY. Ownership is the history row's OWN denormalised user_id, enforced in the repository's
// `WHERE user_id = $1`. Never a join to `transactions`: that table is tombstoned rather than
// deleted, and the three purges hard-delete rows outright, so a transaction-keyed read would hide
// exactly the changes whose prior value is least recoverable. The userID reaches here from
// inputs.AuthenticatedInput; nothing client-supplied touches the filter.
func (s *transactionAnnotationHistoryService) GetUserAnnotationHistory(
	ctx context.Context,
	userID string,
	limit, offset int,
) ([]*domain.TransactionAnnotationChange, int, error) {
	return s.repo.GetByUserID(ctx, userID, limit, offset)
}
