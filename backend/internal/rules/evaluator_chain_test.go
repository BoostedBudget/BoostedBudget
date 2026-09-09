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

// recordingEvaluator captures the condition it was handed, so a test can assert on the VALUE the
// dispatch delivered rather than on a match result that could be right for the wrong reason.
type recordingEvaluator struct{ got domain.RuleCondition }

func (r *recordingEvaluator) Evaluate(_ *domain.Transaction, c domain.RuleCondition) (bool, error) {
	r.got = c
	return true, nil
}

// TestRegisteredEvaluatorReceivesTheCoercedValue is #1625's final-review I5.
//
// repairedForEvaluation used to be applied ONLY on the evaluateField fallback, i.e. AFTER the
// registered-evaluator lookup. So a registered evaluator saw the RAW stored value while the
// built-in path saw the repaired one, and the same rule meant two different things depending on
// whether anyone had called Register(). evaluator.go's own doc comment documents that exact trap
// for the fourteen deleted structs, one paragraph above the line that had it.
func TestRegisteredEvaluatorReceivesTheCoercedValue(t *testing.T) {
	registry := NewConditionRegistry()
	rec := &recordingEvaluator{}
	registry.Register(domain.ConditionAmountGreaterThan, rec)

	// A value ValidateShape REJECTS and CoercedValue repairs: a number sent as a string.
	raw := domain.RuleCondition{Type: domain.ConditionAmountGreaterThan, Value: "1000"}
	if err := raw.Normalize(); err != nil {
		t.Fatalf("fixture failed to normalize: %v", err)
	}
	if err := raw.ValidateShape(); err == nil {
		t.Fatal("fixture is not falsifiable: ValidateShape now ACCEPTS this value, so coercion " +
			"never runs on it and this test proves nothing")
	}

	if _, err := registry.Evaluate(&domain.Transaction{Amount: -2000}, raw); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if got, ok := rec.got.Value.(float64); !ok || got != 1000 {
		t.Errorf("registered evaluator received %#v (%T); it must receive the SAME repaired value "+
			"the built-in path gets, or one rule has two meanings", rec.got.Value, rec.got.Value)
	}
}

// The other half: a value the validator ACCEPTS must reach a registered evaluator byte-identical.
// Coercion is gated on rejection, and hoisting it before dispatch must not have widened that.
func TestRegisteredEvaluatorReceivesAValidValueUntouched(t *testing.T) {
	registry := NewConditionRegistry()
	rec := &recordingEvaluator{}
	registry.Register(domain.ConditionMerchantContains, rec)

	// ⚠ The value that motivated the gate: textValues drops the 42 and still reports ok, so this
	// PASSES ValidateShape and the live write path accepts it today. Coercing it to ["uber","42"]
	// would make the rule match transactions it did not match yesterday.
	raw := domain.RuleCondition{
		Type:  domain.ConditionMerchantContains,
		Value: []interface{}{"uber", 42},
	}
	if err := raw.Normalize(); err != nil {
		t.Fatalf("fixture failed to normalize: %v", err)
	}
	if err := raw.ValidateShape(); err != nil {
		t.Fatalf("fixture is not falsifiable: ValidateShape now REJECTS this value (%v), so it "+
			"would be coerced legitimately", err)
	}

	if _, err := registry.Evaluate(&domain.Transaction{}, raw); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	arr, ok := rec.got.Value.([]interface{})
	if !ok || len(arr) != 2 || arr[0] != "uber" || arr[1] != 42 {
		t.Errorf("registered evaluator received %#v; a value the validator accepts must arrive "+
			"unchanged", rec.got.Value)
	}
}
