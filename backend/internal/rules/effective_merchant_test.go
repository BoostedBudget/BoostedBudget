package rules

import (
	"testing"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
)

// #1625 Task 11: match on the merchant name the USER sees.
//
// `merchant` matches tx.MerchantName -- the raw provider string. A user who renamed or merged a
// merchant sees their own name everywhere in the product, then writes a rule against it and it
// never fires, because the evaluator is comparing against a name only Plaid uses.
//
// ⚠ `merchant` is deliberately NOT repointed at the effective name. Doing so would silently change
// what every EXISTING persisted rule matches, every time a normalization, merge or override
// changed a name -- a rule the user never edited would start or stop matching on its own.
// effective_merchant is a NEW field, so old rows keep their exact meaning and no data migration
// is needed. TestMerchantField_StillMatchesTheRawProviderName pins that.
func TestEffectiveMerchantField_MatchesTheNameTheUserSees(t *testing.T) {
	registry := NewConditionRegistry()

	raw := "SQ *BLUE BOTTLE 1234"
	effective := "Blue Bottle Coffee"
	tx := &domain.Transaction{
		MerchantName:          &raw,
		EffectiveMerchantName: &effective,
	}

	cond := domain.RuleCondition{
		Field:    domain.FieldEffectiveMerchant,
		Operator: string(domain.RuleOpContains),
		Value:    "blue bottle coffee",
	}

	matched, err := registry.Evaluate(tx, cond)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !matched {
		t.Error("effective_merchant did not match the user's own name for the merchant")
	}
}

// When nothing enriched the transaction, effective_merchant falls back to the raw name rather
// than matching nothing. The sync path enriches best-effort, so an un-enriched transaction must
// still behave sensibly instead of silently disabling the rule.
func TestEffectiveMerchantField_FallsBackToTheRawNameWhenUnenriched(t *testing.T) {
	registry := NewConditionRegistry()

	raw := "STARBUCKS"
	tx := &domain.Transaction{MerchantName: &raw}

	cond := domain.RuleCondition{
		Field:    domain.FieldEffectiveMerchant,
		Operator: string(domain.RuleOpContains),
		Value:    "starbucks",
	}

	matched, err := registry.Evaluate(tx, cond)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !matched {
		t.Error("an un-enriched transaction must fall back to the raw merchant name, not match nothing")
	}
}

// The other half of the all-or-nothing decision: `merchant` keeps its exact pre-#1625 meaning.
func TestMerchantField_StillMatchesTheRawProviderName(t *testing.T) {
	registry := NewConditionRegistry()

	raw := "SQ *BLUE BOTTLE 1234"
	effective := "Blue Bottle Coffee"
	tx := &domain.Transaction{MerchantName: &raw, EffectiveMerchantName: &effective}

	// The user's own name must NOT match the raw field, or every existing rule silently changes
	// meaning the moment someone renames a merchant.
	matched, err := registry.Evaluate(tx, domain.RuleCondition{
		Field: domain.FieldMerchant, Operator: string(domain.RuleOpContains), Value: "blue bottle coffee",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if matched {
		t.Error("`merchant` matched the EFFECTIVE name — existing rules would change meaning under the user")
	}
}
