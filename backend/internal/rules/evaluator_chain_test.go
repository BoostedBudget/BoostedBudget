package rules

// Issue #1109: chain-transparency tests for account_id matching.
// After a re-link, tx.UserAccountID is the stable reference that should match
// the rule condition value (which was backfilled by migration 000070).
//
// #1625: these ran against AccountIDEvaluator, one of fourteen per-type evaluator structs that
// duplicated the unified field path and that nothing in production registered. The structs are
// gone; the tests now drive ConditionRegistry.Evaluate, which is what actually runs — so this
// coverage moved onto the live implementation rather than being deleted with the dead one.

import (
	"testing"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
)

// TestAccountIDMatching_ChainAware_PreferUserAccountID verifies that when the
// transaction has a UserAccountID, the evaluator matches against it rather than
// the raw AccountID. This is the post-relink scenario: AccountID rotated but
// UserAccountID (the stable reference) still matches the rule condition.
func TestAccountIDMatching_ChainAware_PreferUserAccountID(t *testing.T) {
	evaluator := NewConditionRegistry()

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

// TestAccountIDMatching_ChainAware_MatchesViaAccountIDWhenUserAccountIDDiffers verifies
// that stale rules (storing raw account_id) still match via fallback when tx.UserAccountID
// is set to a different value (Issue #1306 P2 regression guard). See evaluator.go comment
// for collision risk analysis.
func TestAccountIDMatching_ChainAware_MatchesViaAccountIDWhenUserAccountIDDiffers(t *testing.T) {
	evaluator := NewConditionRegistry()

	txUserAccountID := "user-acct-A"
	rawAccountID := "raw-account-uuid-123"

	tx := &domain.Transaction{
		AccountID:     rawAccountID,      // stale rule stores this raw account_id
		UserAccountID: &txUserAccountID,  // stable ID set by P2 backfill (different UUID)
	}

	// Rule stores raw account_id (migration 000072 skipped this account).
	condition := domain.RuleCondition{
		Type:  domain.ConditionAccountID,
		Value: rawAccountID,
	}

	got, err := evaluator.Evaluate(tx, condition)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !got {
		t.Errorf("expected match via AccountID fallback when UserAccountID differs (stale migration-skipped rule), got false")
	}
}

// TestAccountIDMatching_ChainAware_FallbackToAccountIDWhenNoUserAccountID verifies
// that legacy transactions without UserAccountID still match on raw AccountID.
func TestAccountIDMatching_ChainAware_FallbackToAccountIDWhenNoUserAccountID(t *testing.T) {
	evaluator := NewConditionRegistry()

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

// TestAccountIDMatching_ChainAware_KnownLimitation_UUIDCollision documents the
// theoretical false-positive: if a MIGRATED rule stores account B's user_account_id as
// its condition value, and a DIFFERENT transaction's raw AccountID happens to equal that
// same UUID, the OR-logic fallback would match. This is the known trade-off of Issue #1306
// P2 (see evaluator.go comment). In practice, both accounts.id and user_accounts.id are
// server-generated v4 UUIDs — collision probability is ~1 in 2^122 per pair and not
// exploitable. This test documents the current behavior so any future change is visible.
func TestAccountIDMatching_ChainAware_KnownLimitation_UUIDCollision(t *testing.T) {
	evaluator := NewConditionRegistry()

	txUserAccountID := "user-acct-A"              // tx belongs to account A
	accountBUserAccountID := "user-acct-B-stable" // account B's stable user_account_id

	tx := &domain.Transaction{
		AccountID:     accountBUserAccountID, // impossible UUID collision (documented)
		UserAccountID: &txUserAccountID,
	}

	// Rule was properly migrated for account B — stores B's user_account_id.
	condition := domain.RuleCondition{
		Type:  domain.ConditionAccountID,
		Value: accountBUserAccountID,
	}

	got, err := evaluator.Evaluate(tx, condition)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Under the UUID-collision scenario, the OR-logic fallback matches via AccountID.
	// This is the accepted known limitation; correct behavior requires zero UUID collision.
	if !got {
		t.Errorf("known-limitation test changed behavior unexpectedly: expected true (via AccountID fallback), got false")
	}
}

func TestAccountIDMatching_ChainAware_Depth2(t *testing.T) {
	// Depth-2 chain: acct_old -> acct_mid -> acct_current
	// Rule was authored with the stable user_account_id (not any raw account UUID).
	// After two re-links, the transaction carries the stable user_account_id so it
	// still matches even though the raw account IDs have changed twice.
	evaluator := NewConditionRegistry()

	stableUserAccountID := "user-acct-stable-depth2"

	tx := &domain.Transaction{
		AccountID:     "acct_current",    // third-generation raw ID
		UserAccountID: &stableUserAccountID, // stable across both re-links
	}

	condition := domain.RuleCondition{
		Type:  domain.ConditionAccountID,
		Value: stableUserAccountID, // rule authored with stable ID
	}

	got, err := evaluator.Evaluate(tx, condition)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !got {
		t.Errorf("depth-2 chain: expected match via UserAccountID, got false "+
			"(AccountID=%q UserAccountID=%q conditionValue=%q)",
			tx.AccountID, stableUserAccountID, condition.Value)
	}
}
