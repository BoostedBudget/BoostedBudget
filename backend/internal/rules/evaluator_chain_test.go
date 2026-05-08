package rules

// Issue #1109: Chain-transparency tests for AccountIDEvaluator.
// After a re-link, tx.UserAccountID is the stable reference that should match
// the rule condition value (which was backfilled by migration 000070).

import (
	"testing"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
)

// TestAccountIDEvaluator_ChainAware_PreferUserAccountID verifies that when the
// transaction has a UserAccountID, the evaluator matches against it rather than
// the raw AccountID. This is the post-relink scenario: AccountID rotated but
// UserAccountID (the stable reference) still matches the rule condition.
func TestAccountIDEvaluator_ChainAware_PreferUserAccountID(t *testing.T) {
	evaluator := &AccountIDEvaluator{}

	stableUserAccountID := "user-acct-stable-001"
	oldAccountID := "old-account-uuid"
	ruleConditionValue := stableUserAccountID // rule was backfilled with stable ID

	tx := &domain.Transaction{
		AccountID:     oldAccountID,                // rotated raw ID (post-relink)
		UserAccountID: &stableUserAccountID,        // stable ID
	}

	condition := domain.RuleCondition{
		Type:  domain.ConditionAccountID,
		Value: ruleConditionValue,
	}

	got, err := evaluator.Evaluate(tx, condition)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !got {
		t.Errorf("expected match via UserAccountID, got false (AccountID=%q UserAccountID=%q conditionValue=%q)",
			oldAccountID, stableUserAccountID, ruleConditionValue)
	}
}

// TestAccountIDEvaluator_ChainAware_NoMatchWhenUserAccountIDDiffers verifies that
// when both tx.UserAccountID and rule condition value are set but differ, the
// evaluator returns false even if tx.AccountID accidentally matches.
func TestAccountIDEvaluator_ChainAware_NoMatchWhenUserAccountIDDiffers(t *testing.T) {
	evaluator := &AccountIDEvaluator{}

	txUserAccountID := "user-acct-A"
	ruleValue := "user-acct-B"

	tx := &domain.Transaction{
		AccountID:     ruleValue,          // raw ID happens to equal rule value
		UserAccountID: &txUserAccountID,   // but stable ID differs
	}

	condition := domain.RuleCondition{
		Type:  domain.ConditionAccountID,
		Value: ruleValue,
	}

	got, err := evaluator.Evaluate(tx, condition)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got {
		t.Errorf("expected no match when UserAccountID differs from condition value, got true")
	}
}

// TestAccountIDEvaluator_ChainAware_FallbackToAccountIDWhenNoUserAccountID verifies
// that legacy transactions without UserAccountID still match on raw AccountID.
func TestAccountIDEvaluator_ChainAware_FallbackToAccountIDWhenNoUserAccountID(t *testing.T) {
	evaluator := &AccountIDEvaluator{}

	accountID := "account-legacy-uuid"

	tx := &domain.Transaction{
		AccountID:     accountID,
		UserAccountID: nil, // no stable ID
	}

	condition := domain.RuleCondition{
		Type:  domain.ConditionAccountID,
		Value: accountID,
	}

	got, err := evaluator.Evaluate(tx, condition)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !got {
		t.Errorf("expected match via AccountID fallback, got false")
	}
}
