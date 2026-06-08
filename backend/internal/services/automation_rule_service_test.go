// Package services provides tests for automation rule service ownership validation.
// Issue #972: GAP AE — savings_goal/transfer rules must verify referenced IDs belong to the rule owner.
package services

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/repository"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// ============================================================================
// Helpers
// ============================================================================

// savingsGoalRuleActions returns a marshalled savings_goal action JSON for the given IDs.
func savingsGoalRuleActions(savingsGoalID, fromAccountID string) json.RawMessage {
	b, _ := json.Marshal(map[string]interface{}{
		"savings_goal_id":  savingsGoalID,
		"from_account_id":  fromAccountID,
		"amount_type":      "fixed",
		"amount":           50.0,
	})
	return b
}

// transferRuleActions returns a marshalled transfer_suggestion action JSON.
func transferRuleActions(fromAccountID, toAccountID string) json.RawMessage {
	b, _ := json.Marshal(map[string]interface{}{
		"from_account_id": fromAccountID,
		"to_account_id":   toAccountID,
		"amount_type":     "fixed",
		"amount":          50.0,
	})
	return b
}

// transferRuleActionsWithUserAccountIDs returns a marshalled transfer_suggestion action JSON
// that includes stable from_user_account_id/to_user_account_id fields (Issue #1109 re-link refs).
func transferRuleActionsWithUserAccountIDs(fromAccountID, toAccountID, fromUserAccountID, toUserAccountID string) json.RawMessage {
	b, _ := json.Marshal(map[string]interface{}{
		"from_account_id":      fromAccountID,
		"to_account_id":        toAccountID,
		"from_user_account_id": fromUserAccountID,
		"to_user_account_id":   toUserAccountID,
		"amount_type":          "fixed",
		"amount":               50.0,
	})
	return b
}

// newSavingsGoalRuleRequest builds a CreateAutomationRuleRequest for a savings_goal rule.
func newSavingsGoalRuleRequest(savingsGoalID, fromAccountID string) *domain.CreateAutomationRuleRequest {
	return &domain.CreateAutomationRuleRequest{
		Name:     "my savings rule",
		RuleType: domain.RuleTypeSavingsGoal,
		TriggerConfig: domain.TriggerConfig{
			Type:  domain.TriggerTypeEvent,
			Event: domain.EventBudgetPeriodEnd,
		},
		Actions: savingsGoalRuleActions(savingsGoalID, fromAccountID),
	}
}

// newTransferRuleRequest builds a CreateAutomationRuleRequest for a transfer_suggestion rule.
func newTransferRuleRequest(fromAccountID, toAccountID string) *domain.CreateAutomationRuleRequest {
	return &domain.CreateAutomationRuleRequest{
		Name:     "my transfer rule",
		RuleType: domain.RuleTypeTransferSuggestion,
		TriggerConfig: domain.TriggerConfig{
			Type:  domain.TriggerTypeEvent,
			Event: domain.EventBudgetPeriodEnd,
		},
		Actions: transferRuleActions(fromAccountID, toAccountID),
	}
}

// ============================================================================
// Tests — CreateRule ownership validation (GAP AE)
// ============================================================================

// TestCreateRule_SavingsGoal_SavingsGoalIDOwnership verifies that CreateRule rejects
// a savings_goal rule whose savings_goal_id belongs to a different user (IDOR prevention).
func TestCreateRule_SavingsGoal_SavingsGoalIDOwnership(t *testing.T) {
	ctx := context.Background()
	ownerUserID := "user-owner"
	attackerUserID := "user-attacker"
	goalID := "goal-owned-by-other"
	fromAccountID := "from-acct-attacker"

	ruleRepo := &mockAutomationRulesRepository{}
	goalRepo := &mockSavingsGoalRepository{
		goals: []*domain.SavingsGoal{
			{ID: goalID, UserID: ownerUserID}, // owned by DIFFERENT user
		},
	}
	acctRepo := &MockAccountRepository{}
	// from_account belongs to attacker — only savings_goal_id mismatch matters here
	acctRepo.On("GetByIDAndUser", mock.Anything, fromAccountID, attackerUserID).
		Return(&domain.Account{ID: fromAccountID}, nil)

	svc := NewAutomationRuleService(ruleRepo)
	svc.SetSavingsGoalRepo(goalRepo)
	svc.SetAccountRepo(acctRepo)

	_, err := svc.CreateRule(ctx, attackerUserID, newSavingsGoalRuleRequest(goalID, fromAccountID))
	require.Error(t, err, "CreateRule must reject a savings_goal rule referencing another user's goal")
	assert.Contains(t, err.Error(), "savings_goal_id", "error must identify which ID failed ownership")
}

// TestCreateRule_SavingsGoal_FromAccountOwnership verifies that CreateRule rejects
// a savings_goal rule whose from_account_id belongs to a different user.
func TestCreateRule_SavingsGoal_FromAccountOwnership(t *testing.T) {
	ctx := context.Background()
	userID := "user-a"
	goalID := "goal-owned-by-user-a"
	fromAccountID := "from-acct-owned-by-other"

	ruleRepo := &mockAutomationRulesRepository{}
	goalRepo := &mockSavingsGoalRepository{
		goals: []*domain.SavingsGoal{
			{ID: goalID, UserID: userID}, // goal belongs to the same user
		},
	}
	acctRepo := &MockAccountRepository{}
	// from_account belongs to a different user → GetByIDAndUser returns not found
	acctRepo.On("GetByIDAndUser", mock.Anything, fromAccountID, userID).
		Return((*domain.Account)(nil), repository.ErrAccountNotFound)

	svc := NewAutomationRuleService(ruleRepo)
	svc.SetSavingsGoalRepo(goalRepo)
	svc.SetAccountRepo(acctRepo)

	_, err := svc.CreateRule(ctx, userID, newSavingsGoalRuleRequest(goalID, fromAccountID))
	require.Error(t, err, "CreateRule must reject a savings_goal rule referencing another user's account")
	assert.Contains(t, err.Error(), "from_account_id", "error must identify which ID failed ownership")
}

// TestCreateRule_SavingsGoal_HappyPath verifies that CreateRule accepts a valid
// savings_goal rule where all referenced IDs belong to the rule owner.
func TestCreateRule_SavingsGoal_HappyPath(t *testing.T) {
	ctx := context.Background()
	userID := "user-a"
	goalID := "goal-a"
	fromAccountID := "from-acct-a"

	ruleRepo := &mockAutomationRulesRepository{}
	goalRepo := &mockSavingsGoalRepository{
		goals: []*domain.SavingsGoal{
			{ID: goalID, UserID: userID},
		},
	}
	acctRepo := &MockAccountRepository{}
	acctRepo.On("GetByIDAndUser", mock.Anything, fromAccountID, userID).
		Return(&domain.Account{ID: fromAccountID}, nil)

	svc := NewAutomationRuleService(ruleRepo)
	svc.SetSavingsGoalRepo(goalRepo)
	svc.SetAccountRepo(acctRepo)

	rule, err := svc.CreateRule(ctx, userID, newSavingsGoalRuleRequest(goalID, fromAccountID))
	require.NoError(t, err, "CreateRule must succeed when all IDs belong to the rule owner")
	assert.Equal(t, userID, rule.UserID)
}

// TestCreateRule_Transfer_FromAccountOwnership verifies that CreateRule rejects
// a transfer rule whose from_account_id belongs to a different user.
func TestCreateRule_Transfer_FromAccountOwnership(t *testing.T) {
	ctx := context.Background()
	userID := "user-a"
	fromAccountID := "from-acct-other"
	toAccountID := "to-acct-user-a"

	ruleRepo := &mockAutomationRulesRepository{}
	acctRepo := &MockAccountRepository{}
	acctRepo.On("GetByIDAndUser", mock.Anything, fromAccountID, userID).
		Return((*domain.Account)(nil), repository.ErrAccountNotFound)
	// to_account_id check is not reached if from_account_id fails

	svc := NewAutomationRuleService(ruleRepo)
	svc.SetAccountRepo(acctRepo)

	_, err := svc.CreateRule(ctx, userID, newTransferRuleRequest(fromAccountID, toAccountID))
	require.Error(t, err, "CreateRule must reject a transfer rule referencing another user's from_account_id")
	assert.Contains(t, err.Error(), "from_account_id")
}

// TestCreateRule_Transfer_ToAccountOwnership verifies that CreateRule rejects
// a transfer rule whose to_account_id belongs to a different user.
func TestCreateRule_Transfer_ToAccountOwnership(t *testing.T) {
	ctx := context.Background()
	userID := "user-a"
	fromAccountID := "from-acct-user-a"
	toAccountID := "to-acct-other"

	ruleRepo := &mockAutomationRulesRepository{}
	acctRepo := &MockAccountRepository{}
	acctRepo.On("GetByIDAndUser", mock.Anything, fromAccountID, userID).
		Return(&domain.Account{ID: fromAccountID}, nil)
	acctRepo.On("GetByIDAndUser", mock.Anything, toAccountID, userID).
		Return((*domain.Account)(nil), repository.ErrAccountNotFound)

	svc := NewAutomationRuleService(ruleRepo)
	svc.SetAccountRepo(acctRepo)

	_, err := svc.CreateRule(ctx, userID, newTransferRuleRequest(fromAccountID, toAccountID))
	require.Error(t, err, "CreateRule must reject a transfer rule referencing another user's to_account_id")
	assert.Contains(t, err.Error(), "to_account_id")
}

// TestCreateRule_Transfer_HappyPath verifies that CreateRule accepts a transfer rule
// where both account IDs belong to the rule owner.
func TestCreateRule_Transfer_HappyPath(t *testing.T) {
	ctx := context.Background()
	userID := "user-a"
	fromAccountID := "from-a"
	toAccountID := "to-a"

	ruleRepo := &mockAutomationRulesRepository{}
	acctRepo := &MockAccountRepository{}
	acctRepo.On("GetByIDAndUser", mock.Anything, fromAccountID, userID).
		Return(&domain.Account{ID: fromAccountID}, nil)
	acctRepo.On("GetByIDAndUser", mock.Anything, toAccountID, userID).
		Return(&domain.Account{ID: toAccountID}, nil)

	svc := NewAutomationRuleService(ruleRepo)
	svc.SetAccountRepo(acctRepo)

	rule, err := svc.CreateRule(ctx, userID, newTransferRuleRequest(fromAccountID, toAccountID))
	require.NoError(t, err)
	assert.Equal(t, userID, rule.UserID)
}

// TestUpdateRule_OwnershipEnforced verifies that UpdateRule also enforces ID ownership.
func TestUpdateRule_OwnershipEnforced(t *testing.T) {
	ctx := context.Background()
	userID := "user-a"
	ruleID := "rule-1"
	goalID := "goal-other-user"
	fromAccountID := "from-a"

	existingRule := &domain.AutomationRule{
		ID:       ruleID,
		UserID:   userID,
		Name:     "existing",
		Enabled:  true,
		RuleType: domain.RuleTypeSavingsGoal,
		TriggerConfig: domain.TriggerConfig{
			Type:  domain.TriggerTypeEvent,
			Event: domain.EventBudgetPeriodEnd,
		},
		Actions:        savingsGoalRuleActions("old-goal-id", fromAccountID),
		ConditionLogic: "AND",
	}

	ruleRepo := &mockAutomationRulesRepository{rules: []*domain.AutomationRule{existingRule}}
	goalRepo := &mockSavingsGoalRepository{
		goals: []*domain.SavingsGoal{
			// goalID belongs to a different user, not userID
		},
	}
	acctRepo := &MockAccountRepository{}
	acctRepo.On("GetByIDAndUser", mock.Anything, fromAccountID, userID).
		Return(&domain.Account{ID: fromAccountID}, nil)

	svc := NewAutomationRuleService(ruleRepo)
	svc.SetSavingsGoalRepo(goalRepo)
	svc.SetAccountRepo(acctRepo)

	newActions := savingsGoalRuleActions(goalID, fromAccountID)
	_, err := svc.UpdateRule(ctx, userID, ruleID, &domain.UpdateAutomationRuleRequest{
		Actions: &newActions,
	})
	require.Error(t, err, "UpdateRule must reject updating to reference another user's goal")
	assert.Contains(t, err.Error(), "savings_goal_id")
}

// TestCreateRule_NoRepos_SkipsOwnershipCheck verifies that when repos are not injected
// (e.g. in tests that don't set them), CreateRule silently skips the ownership check.
// This preserves backward compatibility for tests and nil-guarded production paths.
func TestCreateRule_NoRepos_SkipsOwnershipCheck(t *testing.T) {
	ctx := context.Background()
	userID := "user-a"
	ruleRepo := &mockAutomationRulesRepository{}

	svc := NewAutomationRuleService(ruleRepo)
	// No SetSavingsGoalRepo or SetAccountRepo calls — repos are nil

	_, err := svc.CreateRule(ctx, userID, newSavingsGoalRuleRequest("any-goal", "any-account"))
	require.NoError(t, err, "CreateRule must succeed when ownership repos are not configured (nil guard)")
}

// ============================================================================
// Tests — chain-resolver IDOR (GAP AE, Issue #972)
// ============================================================================

// TestCreateRule_Transfer_FromUserAccountIDOwnership verifies that CreateRule rejects
// a transfer rule whose from_user_account_id belongs to a different user.
// This closes the chain-resolver IDOR attack: an attacker could supply their own
// from_account_id (passes GetByIDAndUser check) but a victim's from_user_account_id —
// the unscoped ResolveActiveID would then overwrite the suggestion with the victim's account.
func TestCreateRule_Transfer_FromUserAccountIDOwnership(t *testing.T) {
	ctx := context.Background()
	attackerUserID := "user-attacker"
	fromAccountID := "from-attacker"   // attacker's own account — passes standard check
	toAccountID := "to-attacker"       // attacker's own account
	fromUserAccountID := "ua-victim-1" // victim's stable re-link ref — must be rejected

	ruleRepo := &mockAutomationRulesRepository{}
	acctRepo := &MockAccountRepository{}
	// Both account IDs belong to the attacker (standard checks pass)
	acctRepo.On("GetByIDAndUser", mock.Anything, fromAccountID, attackerUserID).
		Return(&domain.Account{ID: fromAccountID}, nil)
	acctRepo.On("GetByIDAndUser", mock.Anything, toAccountID, attackerUserID).
		Return(&domain.Account{ID: toAccountID}, nil)
	// from_user_account_id belongs to a different user — chain-resolver check must catch it
	acctRepo.On("GetByUserAccountIDAndUser", mock.Anything, fromUserAccountID, attackerUserID).
		Return((*domain.Account)(nil), repository.ErrAccountNotFound)

	svc := NewAutomationRuleService(ruleRepo)
	svc.SetAccountRepo(acctRepo)

	actions := transferRuleActionsWithUserAccountIDs(fromAccountID, toAccountID, fromUserAccountID, "")
	req := &domain.CreateAutomationRuleRequest{
		Name:     "chain resolver idor test",
		RuleType: domain.RuleTypeTransferSuggestion,
		TriggerConfig: domain.TriggerConfig{
			Type:  domain.TriggerTypeEvent,
			Event: domain.EventBudgetPeriodEnd,
		},
		Actions: actions,
	}
	_, err := svc.CreateRule(ctx, attackerUserID, req)
	require.Error(t, err, "CreateRule must reject a transfer rule with another user's from_user_account_id")
	assert.Contains(t, err.Error(), "from_user_account_id", "error must identify the chain-resolver ref that failed ownership")
}

// TestCreateRule_Transfer_ToUserAccountIDOwnership verifies that CreateRule rejects
// a transfer rule whose to_user_account_id belongs to a different user.
func TestCreateRule_Transfer_ToUserAccountIDOwnership(t *testing.T) {
	ctx := context.Background()
	attackerUserID := "user-attacker"
	fromAccountID := "from-attacker"
	toAccountID := "to-attacker"
	toUserAccountID := "ua-victim-2" // victim's stable re-link ref

	ruleRepo := &mockAutomationRulesRepository{}
	acctRepo := &MockAccountRepository{}
	acctRepo.On("GetByIDAndUser", mock.Anything, fromAccountID, attackerUserID).
		Return(&domain.Account{ID: fromAccountID}, nil)
	acctRepo.On("GetByIDAndUser", mock.Anything, toAccountID, attackerUserID).
		Return(&domain.Account{ID: toAccountID}, nil)
	acctRepo.On("GetByUserAccountIDAndUser", mock.Anything, toUserAccountID, attackerUserID).
		Return((*domain.Account)(nil), repository.ErrAccountNotFound)

	svc := NewAutomationRuleService(ruleRepo)
	svc.SetAccountRepo(acctRepo)

	actions := transferRuleActionsWithUserAccountIDs(fromAccountID, toAccountID, "", toUserAccountID)
	req := &domain.CreateAutomationRuleRequest{
		Name:     "chain resolver idor test to",
		RuleType: domain.RuleTypeTransferSuggestion,
		TriggerConfig: domain.TriggerConfig{
			Type:  domain.TriggerTypeEvent,
			Event: domain.EventBudgetPeriodEnd,
		},
		Actions: actions,
	}
	_, err := svc.CreateRule(ctx, attackerUserID, req)
	require.Error(t, err, "CreateRule must reject a transfer rule with another user's to_user_account_id")
	assert.Contains(t, err.Error(), "to_user_account_id", "error must identify the chain-resolver ref that failed ownership")
}

// ============================================================================
// Tests — CreateRule category ownership validation (Issue #1270)
// ============================================================================

// budgetCategoryConditionRule builds a CreateAutomationRuleRequest with a single
// budget_category.* condition for the given categoryID.
func budgetCategoryConditionRule(categoryID string) *domain.CreateAutomationRuleRequest {
	b, _ := json.Marshal(map[string]interface{}{
		"savings_goal_id": "goal-1",
		"from_account_id": "acct-1",
		"amount_type":     "fixed",
		"amount":          10.0,
	})
	return &domain.CreateAutomationRuleRequest{
		Name:     "category condition rule",
		RuleType: domain.RuleTypeSavingsGoal,
		TriggerConfig: domain.TriggerConfig{
			Type:  domain.TriggerTypeEvent,
			Event: domain.EventBudgetPeriodEnd,
		},
		Conditions: []domain.AutomationRuleCondition{
			{
				Field:      "budget_category.percent_used",
				CategoryID: categoryID,
				Operator:   domain.ConditionOperatorGt,
				Value:      "80",
			},
		},
		Actions: b,
	}
}

// TestCreateRule_CategoryCondition_AcceptsOwnedCategory verifies that CreateRule accepts
// a rule whose budget_category.* condition references a category owned by the rule's user.
// Issue #1270: SetBudgetRepo + GetCategoryByIDAndUser happy path.
func TestCreateRule_CategoryCondition_AcceptsOwnedCategory(t *testing.T) {
	ctx := context.Background()
	userID := "user-alice"
	categoryID := "cat-alice-1"

	ruleRepo := &mockAutomationRulesRepository{}
	ruleRepo.rules = []*domain.AutomationRule{} // ensure Create stores the rule
	budgetRepo := &MockBudgetRepository{}
	budgetRepo.On("GetCategoryByIDAndUser", mock.Anything, categoryID, userID).
		Return(&domain.BudgetCategory{ID: categoryID}, nil)
	goalRepo := &MockSavingsGoalRepository{}
	goalRepo.On("GetByIDForUser", mock.Anything, "goal-1", userID).
		Return(&domain.SavingsGoal{ID: "goal-1"}, nil)
	acctRepo := &MockAccountRepository{}
	acctRepo.On("GetByIDAndUser", mock.Anything, "acct-1", userID).
		Return(&domain.Account{ID: "acct-1"}, nil)

	svc := NewAutomationRuleService(ruleRepo)
	svc.SetBudgetRepo(budgetRepo)
	svc.SetSavingsGoalRepo(goalRepo)
	svc.SetAccountRepo(acctRepo)

	req := budgetCategoryConditionRule(categoryID)
	rule, err := svc.CreateRule(ctx, userID, req)
	require.NoError(t, err, "CreateRule must accept a rule whose category_id belongs to the user")
	assert.NotNil(t, rule)
}

// TestCreateRule_CategoryCondition_RejectsForeignCategory verifies that CreateRule rejects
// a rule whose budget_category.* condition references a category owned by a different user
// (IDOR prevention). Issue #1270.
func TestCreateRule_CategoryCondition_RejectsForeignCategory(t *testing.T) {
	ctx := context.Background()
	attackerUserID := "user-attacker"
	victimCategoryID := "cat-victim-99"

	ruleRepo := &mockAutomationRulesRepository{}
	budgetRepo := &MockBudgetRepository{}
	budgetRepo.On("GetCategoryByIDAndUser", mock.Anything, victimCategoryID, attackerUserID).
		Return((*domain.BudgetCategory)(nil), domain.ErrBudgetCategoryNotFound)

	svc := NewAutomationRuleService(ruleRepo)
	svc.SetBudgetRepo(budgetRepo)

	req := budgetCategoryConditionRule(victimCategoryID)
	_, err := svc.CreateRule(ctx, attackerUserID, req)
	require.Error(t, err, "CreateRule must reject a rule whose category_id belongs to a different user")
	assert.Contains(t, err.Error(), "category_id", "error must identify the category_id ownership failure")
}

// TestCreateRule_CategoryCondition_DBError_Propagates verifies that an infrastructure error
// from GetCategoryByIDAndUser is NOT silently converted to 403 — it must propagate as-is
// so the HTTP layer returns 500. Issue #1270 (Codex LOW finding).
func TestCreateRule_CategoryCondition_DBError_Propagates(t *testing.T) {
	ctx := context.Background()
	userID := "user-alice"
	categoryID := "cat-alice-1"
	dbErr := errors.New("connection timeout")

	ruleRepo := &mockAutomationRulesRepository{}
	budgetRepo := &MockBudgetRepository{}
	budgetRepo.On("GetCategoryByIDAndUser", mock.Anything, categoryID, userID).
		Return((*domain.BudgetCategory)(nil), dbErr)

	svc := NewAutomationRuleService(ruleRepo)
	svc.SetBudgetRepo(budgetRepo)

	req := budgetCategoryConditionRule(categoryID)
	_, err := svc.CreateRule(ctx, userID, req)
	require.Error(t, err, "CreateRule must fail on DB error")
	assert.False(t, errors.Is(err, domain.ErrRuleConditionForbidden),
		"a DB infrastructure error must not be wrapped as ErrRuleConditionForbidden (would cause 403 instead of 500)")
}

// TestCreateRule_CategoryCondition_NoBudgetRepo_SkipsCheck verifies that when SetBudgetRepo
// is not called the category ownership check is skipped (backwards-compat for test setups that
// don't inject budgetRepo). Issue #1270.
func TestCreateRule_CategoryCondition_NoBudgetRepo_SkipsCheck(t *testing.T) {
	ctx := context.Background()
	userID := "user-bob"
	categoryID := "cat-any"

	ruleRepo := &mockAutomationRulesRepository{}
	goalRepo := &MockSavingsGoalRepository{}
	goalRepo.On("GetByIDForUser", mock.Anything, "goal-1", userID).
		Return(&domain.SavingsGoal{ID: "goal-1"}, nil)
	acctRepo := &MockAccountRepository{}
	acctRepo.On("GetByIDAndUser", mock.Anything, "acct-1", userID).
		Return(&domain.Account{ID: "acct-1"}, nil)

	svc := NewAutomationRuleService(ruleRepo)
	svc.SetSavingsGoalRepo(goalRepo)
	svc.SetAccountRepo(acctRepo)
	// intentionally NOT calling svc.SetBudgetRepo

	req := budgetCategoryConditionRule(categoryID)
	rule, err := svc.CreateRule(ctx, userID, req)
	require.NoError(t, err, "CreateRule must skip the category check when budgetRepo is not injected")
	assert.NotNil(t, rule)
}
