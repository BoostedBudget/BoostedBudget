package services

import (
	"context"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
)

// Issue #1569 Task 5 (#1572) — the read half of the automation audit trail.
//
// A writer with no reader ships nothing observable, which is why this exists alongside the INSERT
// rather than after it. boost_executions is the template: it has a writer, a per-user read
// (GetByUserID) and an endpoint (GET /api/v1/boost-executions), and this mirrors all three.

// RuleExecutionHistoryService reads a user's automation audit trail.
type RuleExecutionHistoryService interface {
	// GetUserExecutionHistory returns the user's rule executions, newest first, with the total.
	GetUserExecutionHistory(ctx context.Context, userID string, limit, offset int) ([]*domain.RuleExecution, int, error)
}

type ruleExecutionHistoryService struct {
	repo RuleExecutionHistoryRepository
}

// NewRuleExecutionHistoryService creates a RuleExecutionHistoryService.
func NewRuleExecutionHistoryService(repo RuleExecutionHistoryRepository) RuleExecutionHistoryService {
	return &ruleExecutionHistoryService{repo: repo}
}

// GetUserExecutionHistory retrieves the user's whole automation history, orphans included.
//
// TENANCY. Ownership is enforced by the repository's `WHERE user_id = $1`, on the execution row's
// OWN denormalised user_id — never through a join to categorization_rules. That is not a shortcut:
// fk_rule_execution_history_rule_id is ON DELETE SET NULL, and CategorizationRuleRepository.Delete
// still HARD deletes, so deleting a rule in the UI leaves its executions with rule_id NULL. A check
// that went through the rule would either reject every such row or crash on the NULL — and those
// rows are exactly the ones a user needs to see, because the rule itself is gone.
//
// The userID reaches here from inputs.AuthenticatedInput. Nothing client-supplied touches the
// filter.
func (s *ruleExecutionHistoryService) GetUserExecutionHistory(
	ctx context.Context,
	userID string,
	limit, offset int,
) ([]*domain.RuleExecution, int, error) {
	return s.repo.GetByUserID(ctx, userID, limit, offset)
}
