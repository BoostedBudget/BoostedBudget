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

// savingsGoalBoostActions returns a marshalled savings_goal action JSON for the given IDs.
func savingsGoalBoostActions(savingsGoalID, fromAccountID string) json.RawMessage {
	b, _ := json.Marshal(map[string]interface{}{
		"savings_goal_id": savingsGoalID,
		"from_account_id": fromAccountID,
		"amount_type":     "fixed",
		"amount":          50.0,
	})
	return b
}

// transferBoostActions returns a marshalled transfer_suggestion action JSON.
func transferBoostActions(fromAccountID, toAccountID string) json.RawMessage {
	b, _ := json.Marshal(map[string]interface{}{
		"from_account_id": fromAccountID,
		"to_account_id":   toAccountID,
		"amount_type":     "fixed",
		"amount":          50.0,
	})
	return b
}

// transferBoostActionsWithUserAccountIDs returns a marshalled transfer_suggestion action JSON
// that includes stable from_user_account_id/to_user_account_id fields (Issue #1109 re-link refs).
func transferBoostActionsWithUserAccountIDs(fromAccountID, toAccountID, fromUserAccountID, toUserAccountID string) json.RawMessage {
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

// newSavingsGoalBoostRequest builds a CreateBoostRequest for a savings_goal rule.
func newSavingsGoalBoostRequest(savingsGoalID, fromAccountID string) *domain.CreateBoostRequest {
	return &domain.CreateBoostRequest{
		Name:      "my savings rule",
		BoostType: domain.BoostTypeSavingsGoal,
		TriggerConfig: domain.TriggerConfig{
			Type:  domain.TriggerTypeEvent,
			Event: domain.EventBudgetPeriodEnd,
		},
		Actions: savingsGoalBoostActions(savingsGoalID, fromAccountID),
	}
}

// newTransferBoostRequest builds a CreateBoostRequest for a transfer_suggestion rule.
func newTransferBoostRequest(fromAccountID, toAccountID string) *domain.CreateBoostRequest {
	return &domain.CreateBoostRequest{
		Name:      "my transfer rule",
		BoostType: domain.BoostTypeTransferSuggestion,
		TriggerConfig: domain.TriggerConfig{
			Type:  domain.TriggerTypeEvent,
			Event: domain.EventBudgetPeriodEnd,
		},
		Actions: transferBoostActions(fromAccountID, toAccountID),
	}
}

// categoryRolloverBoostActions returns a marshalled category_rollover action JSON.
func categoryRolloverBoostActions(sourceBudgetCategoryID string) json.RawMessage {
	b, _ := json.Marshal(map[string]interface{}{
		"source_budget_category_id": sourceBudgetCategoryID,
		"rollover_type":             "full",
	})
	return b
}

// newCategoryRolloverBoostRequest builds a CreateBoostRequest for a category_rollover rule.
func newCategoryRolloverBoostRequest(sourceBudgetCategoryID, budgetID string) *domain.CreateBoostRequest {
	return &domain.CreateBoostRequest{
		Name:      "my rollover rule",
		BoostType: domain.BoostTypeCategoryRollover,
		TriggerConfig: domain.TriggerConfig{
			Type:  domain.TriggerTypeEvent,
			Event: domain.EventBudgetPeriodEnd,
		},
		Conditions: []domain.BoostCondition{
			{Field: "budget_category.remaining_amount", Operator: domain.ConditionOperatorGt, Value: "0", CategoryID: sourceBudgetCategoryID, BudgetID: budgetID},
		},
		ConditionLogic: "AND",
		Actions:        categoryRolloverBoostActions(sourceBudgetCategoryID),
	}
}

// ============================================================================
// Tests — CreateBoost ownership validation (GAP AE)
// ============================================================================

// TestCreateBoost_SavingsGoal_SavingsGoalIDOwnership verifies that CreateBoost rejects
// a savings_goal rule whose savings_goal_id belongs to a different user (IDOR prevention).
func TestCreateBoost_SavingsGoal_SavingsGoalIDOwnership(t *testing.T) {
	ctx := context.Background()
	ownerUserID := "user-owner"
	attackerUserID := "user-attacker"
	goalID := "goal-owned-by-other"
	fromAccountID := "from-acct-attacker"

	boostRepo := &mockBoostsRepository{}
	goalRepo := &mockSavingsGoalRepository{
		goals: []*domain.SavingsGoal{
			{ID: goalID, UserID: ownerUserID}, // owned by DIFFERENT user
		},
	}
	acctRepo := &MockAccountRepository{}
	// from_account belongs to attacker — only savings_goal_id mismatch matters here
	acctRepo.On("GetByIDAndUser", mock.Anything, fromAccountID, attackerUserID).
		Return(&domain.Account{ID: fromAccountID}, nil)

	svc := NewBoostService(boostRepo)
	svc.SetSavingsGoalRepo(goalRepo)
	svc.SetAccountRepo(acctRepo)

	_, err := svc.CreateBoost(ctx, attackerUserID, newSavingsGoalBoostRequest(goalID, fromAccountID))
	require.Error(t, err, "CreateBoost must reject a savings_goal rule referencing another user's goal")
	assert.Contains(t, err.Error(), "savings_goal_id", "error must identify which ID failed ownership")
}

// TestCreateBoost_SavingsGoal_FromAccountOwnership verifies that CreateBoost rejects
// a savings_goal rule whose from_account_id belongs to a different user.
func TestCreateBoost_SavingsGoal_FromAccountOwnership(t *testing.T) {
	ctx := context.Background()
	userID := "user-a"
	goalID := "goal-owned-by-user-a"
	fromAccountID := "from-acct-owned-by-other"

	boostRepo := &mockBoostsRepository{}
	goalRepo := &mockSavingsGoalRepository{
		goals: []*domain.SavingsGoal{
			{ID: goalID, UserID: userID}, // goal belongs to the same user
		},
	}
	acctRepo := &MockAccountRepository{}
	// from_account belongs to a different user → GetByIDAndUser returns not found
	acctRepo.On("GetByIDAndUser", mock.Anything, fromAccountID, userID).
		Return((*domain.Account)(nil), repository.ErrAccountNotFound)

	svc := NewBoostService(boostRepo)
	svc.SetSavingsGoalRepo(goalRepo)
	svc.SetAccountRepo(acctRepo)

	_, err := svc.CreateBoost(ctx, userID, newSavingsGoalBoostRequest(goalID, fromAccountID))
	require.Error(t, err, "CreateBoost must reject a savings_goal rule referencing another user's account")
	assert.Contains(t, err.Error(), "from_account_id", "error must identify which ID failed ownership")
}

// TestCreateBoost_SavingsGoal_HappyPath verifies that CreateBoost accepts a valid
// savings_goal rule where all referenced IDs belong to the rule owner.
func TestCreateBoost_SavingsGoal_HappyPath(t *testing.T) {
	ctx := context.Background()
	userID := "user-a"
	goalID := "goal-a"
	fromAccountID := "from-acct-a"

	boostRepo := &mockBoostsRepository{}
	goalRepo := &mockSavingsGoalRepository{
		goals: []*domain.SavingsGoal{
			{ID: goalID, UserID: userID},
		},
	}
	acctRepo := &MockAccountRepository{}
	acctRepo.On("GetByIDAndUser", mock.Anything, fromAccountID, userID).
		Return(&domain.Account{ID: fromAccountID}, nil)

	svc := NewBoostService(boostRepo)
	svc.SetSavingsGoalRepo(goalRepo)
	svc.SetAccountRepo(acctRepo)

	boost, err := svc.CreateBoost(ctx, userID, newSavingsGoalBoostRequest(goalID, fromAccountID))
	require.NoError(t, err, "CreateBoost must succeed when all IDs belong to the boost owner")
	assert.Equal(t, userID, boost.UserID)
}

// TestCreateBoost_Transfer_FromAccountOwnership verifies that CreateBoost rejects
// a transfer rule whose from_account_id belongs to a different user.
func TestCreateBoost_Transfer_FromAccountOwnership(t *testing.T) {
	ctx := context.Background()
	userID := "user-a"
	fromAccountID := "from-acct-other"
	toAccountID := "to-acct-user-a"

	boostRepo := &mockBoostsRepository{}
	acctRepo := &MockAccountRepository{}
	acctRepo.On("GetByIDAndUser", mock.Anything, fromAccountID, userID).
		Return((*domain.Account)(nil), repository.ErrAccountNotFound)
	// to_account_id check is not reached if from_account_id fails

	svc := NewBoostService(boostRepo)
	svc.SetAccountRepo(acctRepo)

	_, err := svc.CreateBoost(ctx, userID, newTransferBoostRequest(fromAccountID, toAccountID))
	require.Error(t, err, "CreateBoost must reject a transfer rule referencing another user's from_account_id")
	assert.Contains(t, err.Error(), "from_account_id")
}

// TestCreateBoost_Transfer_ToAccountOwnership verifies that CreateBoost rejects
// a transfer rule whose to_account_id belongs to a different user.
func TestCreateBoost_Transfer_ToAccountOwnership(t *testing.T) {
	ctx := context.Background()
	userID := "user-a"
	fromAccountID := "from-acct-user-a"
	toAccountID := "to-acct-other"

	boostRepo := &mockBoostsRepository{}
	acctRepo := &MockAccountRepository{}
	acctRepo.On("GetByIDAndUser", mock.Anything, fromAccountID, userID).
		Return(&domain.Account{ID: fromAccountID}, nil)
	acctRepo.On("GetByIDAndUser", mock.Anything, toAccountID, userID).
		Return((*domain.Account)(nil), repository.ErrAccountNotFound)

	svc := NewBoostService(boostRepo)
	svc.SetAccountRepo(acctRepo)

	_, err := svc.CreateBoost(ctx, userID, newTransferBoostRequest(fromAccountID, toAccountID))
	require.Error(t, err, "CreateBoost must reject a transfer rule referencing another user's to_account_id")
	assert.Contains(t, err.Error(), "to_account_id")
}

// TestCreateBoost_Transfer_HappyPath verifies that CreateBoost accepts a transfer rule
// where both account IDs belong to the rule owner.
func TestCreateBoost_Transfer_HappyPath(t *testing.T) {
	ctx := context.Background()
	userID := "user-a"
	fromAccountID := "from-a"
	toAccountID := "to-a"

	boostRepo := &mockBoostsRepository{}
	acctRepo := &MockAccountRepository{}
	acctRepo.On("GetByIDAndUser", mock.Anything, fromAccountID, userID).
		Return(&domain.Account{ID: fromAccountID}, nil)
	acctRepo.On("GetByIDAndUser", mock.Anything, toAccountID, userID).
		Return(&domain.Account{ID: toAccountID}, nil)

	svc := NewBoostService(boostRepo)
	svc.SetAccountRepo(acctRepo)

	boost, err := svc.CreateBoost(ctx, userID, newTransferBoostRequest(fromAccountID, toAccountID))
	require.NoError(t, err)
	assert.Equal(t, userID, boost.UserID)
}

// TestUpdateBoost_OwnershipEnforced verifies that UpdateBoost also enforces ID ownership.
func TestUpdateBoost_OwnershipEnforced(t *testing.T) {
	ctx := context.Background()
	userID := "user-a"
	boostID := "rule-1"
	goalID := "goal-other-user"
	fromAccountID := "from-a"

	existingBoost := &domain.Boost{
		ID:        boostID,
		UserID:    userID,
		Name:      "existing",
		Enabled:   true,
		BoostType: domain.BoostTypeSavingsGoal,
		TriggerConfig: domain.TriggerConfig{
			Type:  domain.TriggerTypeEvent,
			Event: domain.EventBudgetPeriodEnd,
		},
		Actions:        savingsGoalBoostActions("old-goal-id", fromAccountID),
		ConditionLogic: "AND",
	}

	boostRepo := &mockBoostsRepository{boosts: []*domain.Boost{existingBoost}}
	goalRepo := &mockSavingsGoalRepository{
		goals: []*domain.SavingsGoal{
			// goalID belongs to a different user, not userID
		},
	}
	acctRepo := &MockAccountRepository{}
	acctRepo.On("GetByIDAndUser", mock.Anything, fromAccountID, userID).
		Return(&domain.Account{ID: fromAccountID}, nil)

	svc := NewBoostService(boostRepo)
	svc.SetSavingsGoalRepo(goalRepo)
	svc.SetAccountRepo(acctRepo)

	newActions := savingsGoalBoostActions(goalID, fromAccountID)
	_, err := svc.UpdateBoost(ctx, userID, boostID, &domain.UpdateBoostRequest{
		Actions: &newActions,
	})
	require.Error(t, err, "UpdateBoost must reject updating to reference another user's goal")
	assert.Contains(t, err.Error(), "savings_goal_id")
}

// TestCreateBoost_NoRepos_SkipsOwnershipCheck verifies that when repos are not injected
// (e.g. in tests that don't set them), CreateBoost silently skips the ownership check.
// This preserves backward compatibility for tests and nil-guarded production paths.
func TestCreateBoost_NoRepos_SkipsOwnershipCheck(t *testing.T) {
	ctx := context.Background()
	userID := "user-a"
	boostRepo := &mockBoostsRepository{}

	svc := NewBoostService(boostRepo)
	// No SetSavingsGoalRepo or SetAccountRepo calls — repos are nil

	_, err := svc.CreateBoost(ctx, userID, newSavingsGoalBoostRequest("any-goal", "any-account"))
	require.NoError(t, err, "CreateBoost must succeed when ownership repos are not configured (nil guard)")
}

// ============================================================================
// Tests — chain-resolver IDOR (GAP AE, Issue #972)
// ============================================================================

// TestCreateBoost_Transfer_FromUserAccountIDOwnership verifies that CreateBoost rejects
// a transfer rule whose from_user_account_id belongs to a different user.
// This closes the chain-resolver IDOR attack: an attacker could supply their own
// from_account_id (passes GetByIDAndUser check) but a victim's from_user_account_id —
// the unscoped ResolveActiveID would then overwrite the suggestion with the victim's account.
func TestCreateBoost_Transfer_FromUserAccountIDOwnership(t *testing.T) {
	ctx := context.Background()
	attackerUserID := "user-attacker"
	fromAccountID := "from-attacker"   // attacker's own account — passes standard check
	toAccountID := "to-attacker"       // attacker's own account
	fromUserAccountID := "ua-victim-1" // victim's stable re-link ref — must be rejected

	boostRepo := &mockBoostsRepository{}
	acctRepo := &MockAccountRepository{}
	// Both account IDs belong to the attacker (standard checks pass)
	acctRepo.On("GetByIDAndUser", mock.Anything, fromAccountID, attackerUserID).
		Return(&domain.Account{ID: fromAccountID}, nil)
	acctRepo.On("GetByIDAndUser", mock.Anything, toAccountID, attackerUserID).
		Return(&domain.Account{ID: toAccountID}, nil)
	// from_user_account_id belongs to a different user — chain-resolver check must catch it
	acctRepo.On("GetByUserAccountIDAndUser", mock.Anything, fromUserAccountID, attackerUserID).
		Return((*domain.Account)(nil), repository.ErrAccountNotFound)

	svc := NewBoostService(boostRepo)
	svc.SetAccountRepo(acctRepo)

	actions := transferBoostActionsWithUserAccountIDs(fromAccountID, toAccountID, fromUserAccountID, "")
	req := &domain.CreateBoostRequest{
		Name:      "chain resolver idor test",
		BoostType: domain.BoostTypeTransferSuggestion,
		TriggerConfig: domain.TriggerConfig{
			Type:  domain.TriggerTypeEvent,
			Event: domain.EventBudgetPeriodEnd,
		},
		Actions: actions,
	}
	_, err := svc.CreateBoost(ctx, attackerUserID, req)
	require.Error(t, err, "CreateBoost must reject a transfer rule with another user's from_user_account_id")
	assert.Contains(t, err.Error(), "from_user_account_id", "error must identify the chain-resolver ref that failed ownership")
}

// TestCreateBoost_Transfer_ToUserAccountIDOwnership verifies that CreateBoost rejects
// a transfer rule whose to_user_account_id belongs to a different user.
func TestCreateBoost_Transfer_ToUserAccountIDOwnership(t *testing.T) {
	ctx := context.Background()
	attackerUserID := "user-attacker"
	fromAccountID := "from-attacker"
	toAccountID := "to-attacker"
	toUserAccountID := "ua-victim-2" // victim's stable re-link ref

	boostRepo := &mockBoostsRepository{}
	acctRepo := &MockAccountRepository{}
	acctRepo.On("GetByIDAndUser", mock.Anything, fromAccountID, attackerUserID).
		Return(&domain.Account{ID: fromAccountID}, nil)
	acctRepo.On("GetByIDAndUser", mock.Anything, toAccountID, attackerUserID).
		Return(&domain.Account{ID: toAccountID}, nil)
	acctRepo.On("GetByUserAccountIDAndUser", mock.Anything, toUserAccountID, attackerUserID).
		Return((*domain.Account)(nil), repository.ErrAccountNotFound)

	svc := NewBoostService(boostRepo)
	svc.SetAccountRepo(acctRepo)

	actions := transferBoostActionsWithUserAccountIDs(fromAccountID, toAccountID, "", toUserAccountID)
	req := &domain.CreateBoostRequest{
		Name:      "chain resolver idor test to",
		BoostType: domain.BoostTypeTransferSuggestion,
		TriggerConfig: domain.TriggerConfig{
			Type:  domain.TriggerTypeEvent,
			Event: domain.EventBudgetPeriodEnd,
		},
		Actions: actions,
	}
	_, err := svc.CreateBoost(ctx, attackerUserID, req)
	require.Error(t, err, "CreateBoost must reject a transfer rule with another user's to_user_account_id")
	assert.Contains(t, err.Error(), "to_user_account_id", "error must identify the chain-resolver ref that failed ownership")
}

// ============================================================================
// Tests — CreateBoost category ownership validation (Issue #1270)
// ============================================================================

// budgetCategoryConditionBoost builds a CreateBoostRequest with a single
// budget_category.* condition for the given categoryID.
// Issue #1271 Item 2: BudgetID is now required by ValidateForPersistence() in addition
// to CategoryID; use a fixed sentinel budget ID so the rule passes persist validation.
func budgetCategoryConditionBoost(categoryID string) *domain.CreateBoostRequest {
	b, _ := json.Marshal(map[string]interface{}{
		"savings_goal_id": "goal-1",
		"from_account_id": "acct-1",
		"amount_type":     "fixed",
		"amount":          10.0,
	})
	return &domain.CreateBoostRequest{
		Name:      "category condition rule",
		BoostType: domain.BoostTypeSavingsGoal,
		TriggerConfig: domain.TriggerConfig{
			Type:  domain.TriggerTypeEvent,
			Event: domain.EventBudgetPeriodEnd,
		},
		Conditions: []domain.BoostCondition{
			{
				Field:      "budget_category.percent_used",
				CategoryID: categoryID,
				BudgetID:   "budget-1",
				Operator:   domain.ConditionOperatorGt,
				Value:      "80",
			},
		},
		Actions: b,
	}
}

// TestCreateBoost_CategoryCondition_AcceptsOwnedCategory verifies that CreateBoost accepts
// a rule whose budget_category.* condition references a category owned by the rule's user.
// Issue #1270: SetBudgetRepo + GetCategoryByIDAndUser happy path.
func TestCreateBoost_CategoryCondition_AcceptsOwnedCategory(t *testing.T) {
	ctx := context.Background()
	userID := "user-alice"
	categoryID := "cat-alice-1"

	boostRepo := &mockBoostsRepository{}
	boostRepo.boosts = []*domain.Boost{} // ensure Create stores the rule
	budgetRepo := &MockBudgetRepository{}
	budgetRepo.On("GetCategoryByIDAndUser", mock.Anything, categoryID, userID).
		Return(&domain.BudgetCategory{ID: categoryID, BudgetID: "budget-1"}, nil)
	goalRepo := &MockSavingsGoalRepository{}
	goalRepo.On("GetByIDForUser", mock.Anything, "goal-1", userID).
		Return(&domain.SavingsGoal{ID: "goal-1"}, nil)
	acctRepo := &MockAccountRepository{}
	acctRepo.On("GetByIDAndUser", mock.Anything, "acct-1", userID).
		Return(&domain.Account{ID: "acct-1"}, nil)

	svc := NewBoostService(boostRepo)
	svc.SetBudgetRepo(budgetRepo)
	svc.SetSavingsGoalRepo(goalRepo)
	svc.SetAccountRepo(acctRepo)

	req := budgetCategoryConditionBoost(categoryID)
	boost, err := svc.CreateBoost(ctx, userID, req)
	require.NoError(t, err, "CreateBoost must accept a boost whose category_id belongs to the user")
	assert.NotNil(t, boost)
}

// TestCreateBoost_CategoryCondition_RejectsForeignCategory verifies that CreateBoost rejects
// a rule whose budget_category.* condition references a category owned by a different user
// (IDOR prevention). Issue #1270.
func TestCreateBoost_CategoryCondition_RejectsForeignCategory(t *testing.T) {
	ctx := context.Background()
	attackerUserID := "user-attacker"
	victimCategoryID := "cat-victim-99"

	boostRepo := &mockBoostsRepository{}
	budgetRepo := &MockBudgetRepository{}
	budgetRepo.On("GetCategoryByIDAndUser", mock.Anything, victimCategoryID, attackerUserID).
		Return((*domain.BudgetCategory)(nil), domain.ErrBudgetCategoryNotFound)

	svc := NewBoostService(boostRepo)
	svc.SetBudgetRepo(budgetRepo)

	req := budgetCategoryConditionBoost(victimCategoryID)
	_, err := svc.CreateBoost(ctx, attackerUserID, req)
	require.Error(t, err, "CreateBoost must reject a rule whose category_id belongs to a different user")
	assert.Contains(t, err.Error(), "category_id", "error must identify the category_id ownership failure")
}

// TestCreateBoost_CategoryCondition_DBError_Propagates verifies that an infrastructure error
// from GetCategoryByIDAndUser is NOT silently converted to 403 — it must propagate as-is
// so the HTTP layer returns 500. Issue #1270 (Codex LOW finding).
func TestCreateBoost_CategoryCondition_DBError_Propagates(t *testing.T) {
	ctx := context.Background()
	userID := "user-alice"
	categoryID := "cat-alice-1"
	dbErr := errors.New("connection timeout")

	boostRepo := &mockBoostsRepository{}
	budgetRepo := &MockBudgetRepository{}
	budgetRepo.On("GetCategoryByIDAndUser", mock.Anything, categoryID, userID).
		Return((*domain.BudgetCategory)(nil), dbErr)

	svc := NewBoostService(boostRepo)
	svc.SetBudgetRepo(budgetRepo)

	req := budgetCategoryConditionBoost(categoryID)
	_, err := svc.CreateBoost(ctx, userID, req)
	require.Error(t, err, "CreateBoost must fail on DB error")
	assert.False(t, errors.Is(err, domain.ErrBoostConditionForbidden),
		"a DB infrastructure error must not be wrapped as ErrBoostConditionForbidden (would cause 403 instead of 500)")
}

// TestCreateBoost_CategoryCondition_RejectsForeignCategory_SentinelWrapped strengthens the
// foreign-category rejection test to assert the error is wrapped as ErrBoostConditionForbidden,
// not just contain the text "category_id". Issue #1270 (Codex LOW finding).
func TestCreateBoost_CategoryCondition_RejectsForeignCategory_SentinelWrapped(t *testing.T) {
	ctx := context.Background()
	attackerUserID := "user-attacker-2"
	victimCategoryID := "cat-victim-sentinel"

	boostRepo := &mockBoostsRepository{}
	budgetRepo := &MockBudgetRepository{}
	budgetRepo.On("GetCategoryByIDAndUser", mock.Anything, victimCategoryID, attackerUserID).
		Return((*domain.BudgetCategory)(nil), domain.ErrBudgetCategoryNotFound)

	svc := NewBoostService(boostRepo)
	svc.SetBudgetRepo(budgetRepo)

	req := budgetCategoryConditionBoost(victimCategoryID)
	_, err := svc.CreateBoost(ctx, attackerUserID, req)
	require.Error(t, err)
	assert.True(t, errors.Is(err, domain.ErrBoostConditionForbidden),
		"foreign category rejection must be wrapped as ErrBoostConditionForbidden for correct 403 mapping")
}

// TestUpdateBoost_CategoryCondition_RejectsForeignCategory verifies that UpdateBoost rejects
// an update that introduces a budget_category.* condition referencing a category owned by a
// different user. UpdateBoost routes through the same validateBoostReferenceOwnership as CreateBoost,
// so this pins the UpdateBoost entry point. Issue #1270 (Codex LOW finding — only CreateBoost tested).
func TestUpdateBoost_CategoryCondition_RejectsForeignCategory(t *testing.T) {
	ctx := context.Background()
	userID := "user-bob"
	boostID := "rule-update-cat"
	victimCategoryID := "cat-victim-update"

	existingBoost := &domain.Boost{
		ID:        boostID,
		UserID:    userID,
		Name:      "existing rule",
		Enabled:   true,
		BoostType: domain.BoostTypeSavingsGoal,
		TriggerConfig: domain.TriggerConfig{
			Type:  domain.TriggerTypeEvent,
			Event: domain.EventBudgetPeriodEnd,
		},
		Actions:        savingsGoalBoostActions("goal-ok", "acct-ok"),
		ConditionLogic: "AND",
	}

	boostRepo := &mockBoostsRepository{boosts: []*domain.Boost{existingBoost}}
	budgetRepo := &MockBudgetRepository{}
	// GetCategoryByIDAndUser returns not-found → the category belongs to another user.
	budgetRepo.On("GetCategoryByIDAndUser", mock.Anything, victimCategoryID, userID).
		Return((*domain.BudgetCategory)(nil), domain.ErrBudgetCategoryNotFound)
	goalRepo := &MockSavingsGoalRepository{}
	goalRepo.On("GetByIDForUser", mock.Anything, "goal-ok", userID).
		Return(&domain.SavingsGoal{ID: "goal-ok"}, nil)
	acctRepo := &MockAccountRepository{}
	acctRepo.On("GetByIDAndUser", mock.Anything, "acct-ok", userID).
		Return(&domain.Account{ID: "acct-ok"}, nil)

	svc := NewBoostService(boostRepo)
	svc.SetBudgetRepo(budgetRepo)
	svc.SetSavingsGoalRepo(goalRepo)
	svc.SetAccountRepo(acctRepo)

	newConditions := []domain.BoostCondition{
		{
			Field:      "budget_category.percent_used",
			CategoryID: victimCategoryID,
			BudgetID:   "budget-1", // required by ValidateForPersistence() (Issue #1271 Item 2)
			Operator:   domain.ConditionOperatorGt,
			Value:      "80",
		},
	}
	_, err := svc.UpdateBoost(ctx, userID, boostID, &domain.UpdateBoostRequest{
		Conditions: &newConditions,
	})
	require.Error(t, err, "UpdateBoost must reject an update introducing a foreign category condition")
	assert.True(t, errors.Is(err, domain.ErrBoostConditionForbidden),
		"foreign category rejection must be wrapped as ErrBoostConditionForbidden")
	assert.Contains(t, err.Error(), "category_id", "error must identify the category_id ownership failure")
}

// TestCreateBoost_CategoryRollover_RejectsSubAllocationSource verifies that CreateBoost refuses a
// category_rollover boost whose source_budget_category_id is a sub-allocation of another
// budgeted category in the same budget (Issue #1630 B6) — rolling it independently would
// double-count dollars already reflected in its parent's own rollover.
func TestCreateBoost_CategoryRollover_RejectsSubAllocationSource(t *testing.T) {
	ctx := context.Background()
	userID := "user-rollover"
	parentPFC := "FOOD_AND_DRINK"
	childPFC := "FOOD_AND_DRINK"
	childDetailed := "FOOD_AND_DRINK_FAST_FOOD"
	childCategoryID := "cat-child-sub-allocation"
	budgetID := "budget-1"

	boostRepo := &mockBoostsRepository{}
	budgetRepo := &MockBudgetRepository{}
	budgetRepo.On("GetCategoryByIDAndUser", mock.Anything, childCategoryID, userID).
		Return(&domain.BudgetCategory{
			ID: childCategoryID, BudgetID: budgetID, CategoryKind: "pfc",
			PFCPrimary: &childPFC, PFCDetailed: &childDetailed, MatchLogic: domain.MatchLogicCategoryOnly,
		}, nil)
	// GetCategoriesByBudgetID returns the budget's FULL category set: the budgeted PFC-primary
	// parent (making the child's detailed row a sub-allocation) plus the child itself.
	budgetRepo.On("GetCategoriesByBudgetID", mock.Anything, budgetID).
		Return([]domain.BudgetCategory{
			{ID: "cat-parent", BudgetID: budgetID, CategoryKind: "pfc", PFCPrimary: &parentPFC, MatchLogic: domain.MatchLogicCategoryOnly},
			{ID: childCategoryID, BudgetID: budgetID, CategoryKind: "pfc", PFCPrimary: &childPFC, PFCDetailed: &childDetailed, MatchLogic: domain.MatchLogicCategoryOnly},
		}, nil)

	svc := NewBoostService(boostRepo)
	svc.SetBudgetRepo(budgetRepo)

	req := newCategoryRolloverBoostRequest(childCategoryID, budgetID)
	_, err := svc.CreateBoost(ctx, userID, req)
	require.Error(t, err, "CreateBoost must refuse a rollover whose source is a sub-allocation")
	assert.True(t, errors.Is(err, domain.ErrInvalidBoost),
		"sub-allocation refusal must be wrapped as ErrInvalidBoost for a 400, not a 403 or 500")
	assert.Contains(t, err.Error(), "sub-allocation")
}

// TestCreateBoost_CategoryRollover_AcceptsTopLevelSource verifies that CreateBoost accepts a
// category_rollover boost whose source is NOT a sub-allocation (the common, legitimate case) —
// a control proving the new guard does not over-refuse.
func TestCreateBoost_CategoryRollover_AcceptsTopLevelSource(t *testing.T) {
	ctx := context.Background()
	userID := "user-rollover-ok"
	sourcePFC := "FOOD_AND_DRINK"
	sourceCategoryID := "cat-top-level"
	budgetID := "budget-2"

	boostRepo := &mockBoostsRepository{}
	boostRepo.boosts = []*domain.Boost{}
	budgetRepo := &MockBudgetRepository{}
	budgetRepo.On("GetCategoryByIDAndUser", mock.Anything, sourceCategoryID, userID).
		Return(&domain.BudgetCategory{
			ID: sourceCategoryID, BudgetID: budgetID, CategoryKind: "pfc",
			PFCPrimary: &sourcePFC, MatchLogic: domain.MatchLogicCategoryOnly,
		}, nil)
	// The budget's ONLY category is the source itself — no PFC-detailed child, no parent —
	// so it is a top-level budgeted row, not a sub-allocation.
	budgetRepo.On("GetCategoriesByBudgetID", mock.Anything, budgetID).
		Return([]domain.BudgetCategory{
			{ID: sourceCategoryID, BudgetID: budgetID, CategoryKind: "pfc", PFCPrimary: &sourcePFC, MatchLogic: domain.MatchLogicCategoryOnly},
		}, nil)

	svc := NewBoostService(boostRepo)
	svc.SetBudgetRepo(budgetRepo)

	req := newCategoryRolloverBoostRequest(sourceCategoryID, budgetID)
	boost, err := svc.CreateBoost(ctx, userID, req)
	require.NoError(t, err, "CreateBoost must accept a rollover whose source is a top-level budgeted category")
	assert.NotNil(t, boost)
}

// TestCreateBoost_CategoryRollover_SkipsGuardWithoutBudgetRepo verifies the guard is silently
// skipped (not a hard failure) when the optional budget repository is unwired — mirroring every
// other optional ownership check in validateBoostReferenceOwnership.
func TestCreateBoost_CategoryRollover_SkipsGuardWithoutBudgetRepo(t *testing.T) {
	ctx := context.Background()
	userID := "user-no-budget-repo"

	boostRepo := &mockBoostsRepository{}
	boostRepo.boosts = []*domain.Boost{}
	svc := NewBoostService(boostRepo)
	// Deliberately no SetBudgetRepo call.

	req := newCategoryRolloverBoostRequest("cat-whatever", "budget-whatever")
	boost, err := svc.CreateBoost(ctx, userID, req)
	require.NoError(t, err)
	assert.NotNil(t, boost)
}

// TestCreateBoost_InvalidConditions_WrapsErrInvalidBoost verifies that CreateBoost wraps
// field-allowlist violations (from boost.Validate()) as domain.ErrInvalidBoost so the HTTP
// layer returns 400, not 500. Issue #1270 (Codex LOW finding — mapper tested, not service).
func TestCreateBoost_InvalidConditions_WrapsErrInvalidBoost(t *testing.T) {
	ctx := context.Background()
	userID := "user-invalid-cond"

	boostRepo := &mockBoostsRepository{}
	svc := NewBoostService(boostRepo)

	// budget_category.invalid_field is not in validConditionFieldsByEvent for budget_period_end.
	b, _ := json.Marshal(map[string]interface{}{
		"savings_goal_id": "goal-x",
		"from_account_id": "acct-x",
		"amount_type":     "fixed",
		"amount":          10.0,
	})
	req := &domain.CreateBoostRequest{
		Name:      "bad field rule",
		BoostType: domain.BoostTypeSavingsGoal,
		TriggerConfig: domain.TriggerConfig{
			Type:  domain.TriggerTypeEvent,
			Event: domain.EventBudgetPeriodEnd,
		},
		Conditions: []domain.BoostCondition{
			{
				Field:    "budget_category.invalid_field",
				Operator: domain.ConditionOperatorGt,
				Value:    "80",
			},
		},
		Actions: b,
	}

	_, err := svc.CreateBoost(ctx, userID, req)
	require.Error(t, err, "CreateBoost must reject conditions with an unknown field")
	assert.True(t, errors.Is(err, domain.ErrInvalidBoost),
		"field-allowlist violation must be wrapped as domain.ErrInvalidBoost for 400 mapping")
}

// TestUpdateBoost_InvalidConditions_WrapsErrInvalidBoost verifies that UpdateBoost wraps
// field-allowlist violations as domain.ErrInvalidBoost (same contract as CreateBoost).
// Issue #1270 (Codex LOW finding — only CreateBoost sentinel wrapping was covered).
func TestUpdateBoost_InvalidConditions_WrapsErrInvalidBoost(t *testing.T) {
	ctx := context.Background()
	userID := "user-invalid-update"
	boostID := "rule-bad-cond"

	existingBoost := &domain.Boost{
		ID:        boostID,
		UserID:    userID,
		Name:      "existing valid rule",
		Enabled:   true,
		BoostType: domain.BoostTypeSavingsGoal,
		TriggerConfig: domain.TriggerConfig{
			Type:  domain.TriggerTypeEvent,
			Event: domain.EventBudgetPeriodEnd,
		},
		Actions:        savingsGoalBoostActions("goal-y", "acct-y"),
		ConditionLogic: "AND",
	}

	boostRepo := &mockBoostsRepository{boosts: []*domain.Boost{existingBoost}}
	svc := NewBoostService(boostRepo)

	// Try to update with an invalid condition field.
	invalidConditions := []domain.BoostCondition{
		{
			Field:    "budget_category.invalid_field",
			Operator: domain.ConditionOperatorGt,
			Value:    "80",
		},
	}
	_, err := svc.UpdateBoost(ctx, userID, boostID, &domain.UpdateBoostRequest{
		Conditions: &invalidConditions,
	})
	require.Error(t, err, "UpdateBoost must reject conditions with an unknown field")
	assert.True(t, errors.Is(err, domain.ErrInvalidBoost),
		"field-allowlist violation in UpdateBoost must be wrapped as domain.ErrInvalidBoost for 400 mapping")
}

// TestCreateBoost_CategoryCondition_NoBudgetRepo_SkipsCheck verifies that when SetBudgetRepo
// is not called the category ownership check is skipped (backwards-compat for test setups that
// don't inject budgetRepo). Issue #1270.
func TestCreateBoost_CategoryCondition_NoBudgetRepo_SkipsCheck(t *testing.T) {
	ctx := context.Background()
	userID := "user-bob"
	categoryID := "cat-any"

	boostRepo := &mockBoostsRepository{}
	goalRepo := &MockSavingsGoalRepository{}
	goalRepo.On("GetByIDForUser", mock.Anything, "goal-1", userID).
		Return(&domain.SavingsGoal{ID: "goal-1"}, nil)
	acctRepo := &MockAccountRepository{}
	acctRepo.On("GetByIDAndUser", mock.Anything, "acct-1", userID).
		Return(&domain.Account{ID: "acct-1"}, nil)

	svc := NewBoostService(boostRepo)
	svc.SetSavingsGoalRepo(goalRepo)
	svc.SetAccountRepo(acctRepo)
	// intentionally NOT calling svc.SetBudgetRepo

	req := budgetCategoryConditionBoost(categoryID)
	boost, err := svc.CreateBoost(ctx, userID, req)
	require.NoError(t, err, "CreateBoost must skip the category check when budgetRepo is not injected")
	assert.NotNil(t, boost)
}

// ============================================================================
// Tests — category-not-in-budget ownership chain (Issue #1271 Item 3, A-CR4)
// ============================================================================

// TestCreateBoost_CategoryNotInSelectedBudget_Returns400 verifies that CreateBoost rejects
// a rule whose condition references a category that belongs to the user but lives in a
// different budget than the one specified in condition.BudgetID. The category IS owned by
// the user (GetCategoryByIDAndUser succeeds) but cat.BudgetID != condition.BudgetID → 400.
func TestCreateBoost_CategoryNotInSelectedBudget_Returns400(t *testing.T) {
	ctx := context.Background()
	userID := "user-alice"
	categoryID := "cat-1"

	boostRepo := &mockBoostsRepository{}
	budgetRepo := &MockBudgetRepository{}
	// Category is owned by the user but lives in "budget-OTHER", not "budget-MINE".
	budgetRepo.On("GetCategoryByIDAndUser", mock.Anything, categoryID, userID).
		Return(&domain.BudgetCategory{ID: categoryID, BudgetID: "budget-OTHER"}, nil)
	goalRepo := &MockSavingsGoalRepository{}
	goalRepo.On("GetByIDForUser", mock.Anything, "goal-1", userID).
		Return(&domain.SavingsGoal{ID: "goal-1"}, nil)
	acctRepo := &MockAccountRepository{}
	acctRepo.On("GetByIDAndUser", mock.Anything, "acct-1", userID).
		Return(&domain.Account{ID: "acct-1"}, nil)

	svc := NewBoostService(boostRepo)
	svc.SetBudgetRepo(budgetRepo)
	svc.SetSavingsGoalRepo(goalRepo)
	svc.SetAccountRepo(acctRepo)

	// Build a request with condition.BudgetID = "budget-MINE" (different from cat's budget).
	b, _ := json.Marshal(map[string]interface{}{
		"savings_goal_id": "goal-1",
		"from_account_id": "acct-1",
		"amount_type":     "fixed",
		"amount":          10.0,
	})
	req := &domain.CreateBoostRequest{
		Name:      "category-not-in-budget rule",
		BoostType: domain.BoostTypeSavingsGoal,
		TriggerConfig: domain.TriggerConfig{
			Type:  domain.TriggerTypeEvent,
			Event: domain.EventBudgetPeriodEnd,
		},
		Conditions: []domain.BoostCondition{
			{
				Field:      "budget_category.percent_used",
				CategoryID: categoryID,
				BudgetID:   "budget-MINE",
				Operator:   domain.ConditionOperatorGt,
				Value:      "80",
			},
		},
		Actions: b,
	}
	_, err := svc.CreateBoost(ctx, userID, req)
	require.Error(t, err, "CreateBoost must reject a condition whose category is not in the selected budget")
	assert.True(t, errors.Is(err, domain.ErrInvalidBoost),
		"category-not-in-budget must be wrapped as domain.ErrInvalidBoost (400), got: %v", err)
}

// TestUpdateBoost_CategoryNotInSelectedBudget_Returns400 verifies that UpdateBoost applies
// the same category-not-in-budget check as CreateBoost. Issue #1271 Item 3 (A-CR4).
func TestUpdateBoost_CategoryNotInSelectedBudget_Returns400(t *testing.T) {
	ctx := context.Background()
	userID := "user-bob"
	boostID := "rule-update-budget-check"
	categoryID := "cat-1"

	existingBoost := &domain.Boost{
		ID:        boostID,
		UserID:    userID,
		Name:      "existing rule",
		Enabled:   true,
		BoostType: domain.BoostTypeSavingsGoal,
		TriggerConfig: domain.TriggerConfig{
			Type:  domain.TriggerTypeEvent,
			Event: domain.EventBudgetPeriodEnd,
		},
		Actions:        savingsGoalBoostActions("goal-ok", "acct-ok"),
		ConditionLogic: "AND",
	}

	boostRepo := &mockBoostsRepository{boosts: []*domain.Boost{existingBoost}}
	budgetRepo := &MockBudgetRepository{}
	// Category is owned by the user but lives in "budget-OTHER", not "budget-MINE".
	budgetRepo.On("GetCategoryByIDAndUser", mock.Anything, categoryID, userID).
		Return(&domain.BudgetCategory{ID: categoryID, BudgetID: "budget-OTHER"}, nil)
	goalRepo := &MockSavingsGoalRepository{}
	goalRepo.On("GetByIDForUser", mock.Anything, "goal-ok", userID).
		Return(&domain.SavingsGoal{ID: "goal-ok"}, nil)
	acctRepo := &MockAccountRepository{}
	acctRepo.On("GetByIDAndUser", mock.Anything, "acct-ok", userID).
		Return(&domain.Account{ID: "acct-ok"}, nil)

	svc := NewBoostService(boostRepo)
	svc.SetBudgetRepo(budgetRepo)
	svc.SetSavingsGoalRepo(goalRepo)
	svc.SetAccountRepo(acctRepo)

	newConditions := []domain.BoostCondition{
		{
			Field:      "budget_category.percent_used",
			CategoryID: categoryID,
			BudgetID:   "budget-MINE",
			Operator:   domain.ConditionOperatorGt,
			Value:      "80",
		},
	}
	_, err := svc.UpdateBoost(ctx, userID, boostID, &domain.UpdateBoostRequest{
		Conditions: &newConditions,
	})
	require.Error(t, err, "UpdateBoost must reject a condition whose category is not in the selected budget")
	assert.True(t, errors.Is(err, domain.ErrInvalidBoost),
		"category-not-in-budget must be wrapped as domain.ErrInvalidBoost (400), got: %v", err)
}

// ===========================================================================
// Priority 2: Boost service read/write methods — zero coverage
// ===========================================================================

// TestBoostService_GetUserBoosts_ReturnsUserBoosts verifies GetUserBoosts delegates to repo.
func TestBoostService_GetUserBoosts_ReturnsUserBoosts(t *testing.T) {
	ctx := context.Background()
	userID := "user-abc"

	boost1 := &domain.Boost{ID: "b1", UserID: userID, Name: "Rule 1", BoostType: domain.BoostTypeBudgetAlert}
	boost2 := &domain.Boost{ID: "b2", UserID: userID, Name: "Rule 2", BoostType: domain.BoostTypeBudgetAlert}
	boostRepo := &mockBoostsRepository{boosts: []*domain.Boost{boost1, boost2}}

	svc := NewBoostService(boostRepo)
	boosts, err := svc.GetUserBoosts(ctx, userID)

	require.NoError(t, err)
	assert.Len(t, boosts, 2)
	assert.Equal(t, "b1", boosts[0].ID)
	assert.Equal(t, "b2", boosts[1].ID)
}

// TestBoostService_GetUserBoosts_EmptyWhenNoBoosts verifies empty slice returned for unknown user.
func TestBoostService_GetUserBoosts_EmptyWhenNoBoosts(t *testing.T) {
	ctx := context.Background()
	boostRepo := &mockBoostsRepository{boosts: []*domain.Boost{}}

	svc := NewBoostService(boostRepo)
	boosts, err := svc.GetUserBoosts(ctx, "nobody")

	require.NoError(t, err)
	assert.Empty(t, boosts)
}

// TestBoostService_GetUserBoostsFiltered_ByType verifies type filtering is applied.
func TestBoostService_GetUserBoostsFiltered_ByType(t *testing.T) {
	ctx := context.Background()
	userID := "user-abc"

	b1 := &domain.Boost{ID: "b1", UserID: userID, BoostType: domain.BoostTypeBudgetAlert}
	b2 := &domain.Boost{ID: "b2", UserID: userID, BoostType: domain.BoostTypeTransferSuggestion}
	boostRepo := &mockBoostsRepository{boosts: []*domain.Boost{b1, b2}}

	svc := NewBoostService(boostRepo)
	boosts, err := svc.GetUserBoostsFiltered(ctx, userID, []domain.BoostType{domain.BoostTypeBudgetAlert})

	require.NoError(t, err)
	assert.Len(t, boosts, 1)
	assert.Equal(t, "b1", boosts[0].ID)
}

// TestBoostService_GetUserBoostsFiltered_EmptyFilter_ReturnsAll verifies no filter returns all boosts.
func TestBoostService_GetUserBoostsFiltered_EmptyFilter_ReturnsAll(t *testing.T) {
	ctx := context.Background()
	userID := "user-abc"

	b1 := &domain.Boost{ID: "b1", UserID: userID, BoostType: domain.BoostTypeBudgetAlert}
	b2 := &domain.Boost{ID: "b2", UserID: userID, BoostType: domain.BoostTypeTransferSuggestion}
	boostRepo := &mockBoostsRepository{boosts: []*domain.Boost{b1, b2}}

	svc := NewBoostService(boostRepo)
	boosts, err := svc.GetUserBoostsFiltered(ctx, userID, nil)

	require.NoError(t, err)
	assert.Len(t, boosts, 2)
}

// TestBoostService_GetBoostByID_ReturnsBoost verifies GetBoostByID returns the correct boost.
func TestBoostService_GetBoostByID_ReturnsBoost(t *testing.T) {
	ctx := context.Background()
	userID := "user-abc"
	boostID := "b1"

	boost := &domain.Boost{ID: boostID, UserID: userID, Name: "My Rule", BoostType: domain.BoostTypeBudgetAlert}
	boostRepo := &mockBoostsRepository{boosts: []*domain.Boost{boost}}

	svc := NewBoostService(boostRepo)
	result, err := svc.GetBoostByID(ctx, userID, boostID)

	require.NoError(t, err)
	assert.Equal(t, boostID, result.ID)
	assert.Equal(t, userID, result.UserID)
}

// TestBoostService_GetBoostByID_RejectsWrongOwner verifies ownership check prevents IDOR.
func TestBoostService_GetBoostByID_RejectsWrongOwner(t *testing.T) {
	ctx := context.Background()
	ownerID := "user-owner"
	attackerID := "user-attacker"
	boostID := "b1"

	boost := &domain.Boost{ID: boostID, UserID: ownerID, Name: "My Rule", BoostType: domain.BoostTypeBudgetAlert}
	boostRepo := &mockBoostsRepository{boosts: []*domain.Boost{boost}}

	svc := NewBoostService(boostRepo)
	_, err := svc.GetBoostByID(ctx, attackerID, boostID)

	require.ErrorIs(t, err, domain.ErrBoostNotFound)
}

// TestBoostService_GetBoostByID_NotFound verifies not-found error propagated.
func TestBoostService_GetBoostByID_NotFound(t *testing.T) {
	ctx := context.Background()
	boostRepo := &mockBoostsRepository{boosts: []*domain.Boost{}}

	svc := NewBoostService(boostRepo)
	_, err := svc.GetBoostByID(ctx, "user-abc", "nonexistent")

	require.Error(t, err)
}

// TestBoostService_DeleteBoost_RemovesBoost verifies DeleteBoost removes the boost.
func TestBoostService_DeleteBoost_RemovesBoost(t *testing.T) {
	ctx := context.Background()
	userID := "user-abc"
	boostID := "b1"

	boost := &domain.Boost{ID: boostID, UserID: userID, Name: "My Rule", BoostType: domain.BoostTypeBudgetAlert}
	boostRepo := &mockBoostsRepository{boosts: []*domain.Boost{boost}}

	svc := NewBoostService(boostRepo)
	err := svc.DeleteBoost(ctx, userID, boostID)

	require.NoError(t, err)
	// Verify boost was actually removed from the repo
	remaining, _ := boostRepo.GetByUserID(ctx, userID)
	assert.Empty(t, remaining)
}

// TestBoostService_DeleteBoost_RejectsWrongOwner verifies DeleteBoost prevents cross-user delete.
func TestBoostService_DeleteBoost_RejectsWrongOwner(t *testing.T) {
	ctx := context.Background()
	ownerID := "user-owner"
	attackerID := "user-attacker"
	boostID := "b1"

	boost := &domain.Boost{ID: boostID, UserID: ownerID, Name: "My Rule", BoostType: domain.BoostTypeBudgetAlert}
	boostRepo := &mockBoostsRepository{boosts: []*domain.Boost{boost}}

	svc := NewBoostService(boostRepo)
	err := svc.DeleteBoost(ctx, attackerID, boostID)

	require.ErrorIs(t, err, domain.ErrBoostNotFound)
	// Verify the boost was NOT deleted
	remaining, _ := boostRepo.GetByUserID(ctx, ownerID)
	assert.Len(t, remaining, 1)
}

// TestBoostService_ToggleBoost_EnablesBoost verifies ToggleBoost enables the boost.
func TestBoostService_ToggleBoost_EnablesBoost(t *testing.T) {
	ctx := context.Background()
	userID := "user-abc"
	boostID := "b1"

	boost := &domain.Boost{
		ID: boostID, UserID: userID, Name: "My Rule",
		Enabled: false, BoostType: domain.BoostTypeBudgetAlert,
		TriggerConfig:  domain.TriggerConfig{Type: domain.TriggerTypeSchedule, Cron: "0 9 * * *"},
		Actions:        json.RawMessage(`{}`),
		ConditionLogic: "AND",
	}
	boostRepo := &mockBoostsRepository{boosts: []*domain.Boost{boost}}

	svc := NewBoostService(boostRepo)
	result, err := svc.ToggleBoost(ctx, userID, boostID, true)

	require.NoError(t, err)
	assert.True(t, result.Enabled)
}

// TestBoostService_ToggleBoost_DisablesBoost verifies ToggleBoost disables the boost.
func TestBoostService_ToggleBoost_DisablesBoost(t *testing.T) {
	ctx := context.Background()
	userID := "user-abc"
	boostID := "b1"

	boost := &domain.Boost{
		ID: boostID, UserID: userID, Name: "My Rule",
		Enabled: true, BoostType: domain.BoostTypeBudgetAlert,
		TriggerConfig:  domain.TriggerConfig{Type: domain.TriggerTypeSchedule, Cron: "0 9 * * *"},
		Actions:        json.RawMessage(`{}`),
		ConditionLogic: "AND",
	}
	boostRepo := &mockBoostsRepository{boosts: []*domain.Boost{boost}}

	svc := NewBoostService(boostRepo)
	result, err := svc.ToggleBoost(ctx, userID, boostID, false)

	require.NoError(t, err)
	assert.False(t, result.Enabled)
}

// TestBoostService_ToggleBoost_RejectsWrongOwner verifies ownership check on toggle.
func TestBoostService_ToggleBoost_RejectsWrongOwner(t *testing.T) {
	ctx := context.Background()
	ownerID := "user-owner"
	boostID := "b1"

	boost := &domain.Boost{ID: boostID, UserID: ownerID, Name: "Rule", BoostType: domain.BoostTypeBudgetAlert}
	boostRepo := &mockBoostsRepository{boosts: []*domain.Boost{boost}}

	svc := NewBoostService(boostRepo)
	_, err := svc.ToggleBoost(ctx, "attacker", boostID, true)

	require.ErrorIs(t, err, domain.ErrBoostNotFound)
}

// TestBoostService_GetEnabledBoosts_ReturnsOnlyEnabled verifies only enabled boosts are returned.
func TestBoostService_GetEnabledBoosts_ReturnsOnlyEnabled(t *testing.T) {
	ctx := context.Background()
	userID := "user-abc"

	enabled := &domain.Boost{ID: "b1", UserID: userID, Name: "Enabled", Enabled: true, BoostType: domain.BoostTypeBudgetAlert}
	disabled := &domain.Boost{ID: "b2", UserID: userID, Name: "Disabled", Enabled: false, BoostType: domain.BoostTypeBudgetAlert}
	boostRepo := &mockBoostsRepository{boosts: []*domain.Boost{enabled, disabled}}

	svc := NewBoostService(boostRepo)
	boosts, err := svc.GetEnabledBoosts(ctx, userID)

	require.NoError(t, err)
	assert.Len(t, boosts, 1)
	assert.Equal(t, "b1", boosts[0].ID)
}

// TestBoostService_GetBoostsByType_ReturnsMatchingType verifies type filter applied.
func TestBoostService_GetBoostsByType_ReturnsMatchingType(t *testing.T) {
	ctx := context.Background()
	userID := "user-abc"

	b1 := &domain.Boost{ID: "b1", UserID: userID, Enabled: true, BoostType: domain.BoostTypeBudgetAlert}
	b2 := &domain.Boost{ID: "b2", UserID: userID, Enabled: true, BoostType: domain.BoostTypeTransferSuggestion}
	boostRepo := &mockBoostsRepository{boosts: []*domain.Boost{b1, b2}}

	svc := NewBoostService(boostRepo)
	boosts, err := svc.GetBoostsByType(ctx, userID, domain.BoostTypeBudgetAlert)

	require.NoError(t, err)
	assert.Len(t, boosts, 1)
	assert.Equal(t, "b1", boosts[0].ID)
}
