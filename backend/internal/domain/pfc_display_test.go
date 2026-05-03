package domain

import (
	"testing"

	"github.com/maxnussbaum/BoostedBudget/internal/pfc"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ─────────────────────────────────────────────────────────────────────────────
// Transaction – primary display
// ─────────────────────────────────────────────────────────────────────────────

func TestTransaction_PopulatePFCDisplay_AllPrimaries(t *testing.T) {
	for _, p := range pfc.AllPrimaries() {
		p := p // capture range var
		t.Run(p.Enum, func(t *testing.T) {
			tx := &Transaction{PFCPrimary: &p.Enum}
			tx.PopulatePFCDisplay()

			require.NotNil(t, tx.PFCPrimaryDisplay)
			expected := pfc.PrimaryDisplayName(p.Enum)
			assert.Equal(t, expected, *tx.PFCPrimaryDisplay)
			assert.NotEmpty(t, *tx.PFCPrimaryDisplay)
		})
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Transaction – detailed display
// ─────────────────────────────────────────────────────────────────────────────

func TestTransaction_PopulatePFCDisplay_AllDetailed(t *testing.T) {
	for _, d := range pfc.AllDetailed() {
		d := d
		t.Run(d.Enum, func(t *testing.T) {
			tx := &Transaction{PFCPrimary: &d.PrimaryEnum, PFCDetailed: &d.Enum}
			tx.PopulatePFCDisplay()

			require.NotNil(t, tx.PFCDetailedDisplay)
			expected := pfc.DetailedDisplayName(d.PrimaryEnum, d.Enum)
			assert.Equal(t, expected, *tx.PFCDetailedDisplay)
			assert.NotEmpty(t, *tx.PFCDetailedDisplay)
		})
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Transaction – override display fields
// ─────────────────────────────────────────────────────────────────────────────

func TestTransaction_PopulatePFCDisplay_OverridePrimary(t *testing.T) {
	for _, p := range pfc.AllPrimaries() {
		p := p
		t.Run(p.Enum, func(t *testing.T) {
			tx := &Transaction{PFCPrimaryOverride: &p.Enum}
			tx.PopulatePFCDisplay()

			require.NotNil(t, tx.PFCPrimaryOverrideDisplay)
			expected := pfc.PrimaryDisplayName(p.Enum)
			assert.Equal(t, expected, *tx.PFCPrimaryOverrideDisplay)
			assert.NotEmpty(t, *tx.PFCPrimaryOverrideDisplay)
		})
	}
}

func TestTransaction_PopulatePFCDisplay_OverrideDetailed(t *testing.T) {
	for _, d := range pfc.AllDetailed() {
		d := d
		t.Run(d.Enum, func(t *testing.T) {
			tx := &Transaction{PFCPrimaryOverride: &d.PrimaryEnum, PFCDetailedOverride: &d.Enum}
			tx.PopulatePFCDisplay()

			require.NotNil(t, tx.PFCDetailedOverrideDisplay)
			expected := pfc.DetailedDisplayName(d.PrimaryEnum, d.Enum)
			assert.Equal(t, expected, *tx.PFCDetailedOverrideDisplay)
			assert.NotEmpty(t, *tx.PFCDetailedOverrideDisplay)
		})
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// BudgetCategory – primary + detailed display
// ─────────────────────────────────────────────────────────────────────────────

func TestBudgetCategory_PopulatePFCDisplay_AllPrimaries(t *testing.T) {
	for _, p := range pfc.AllPrimaries() {
		p := p
		t.Run(p.Enum, func(t *testing.T) {
			bc := &BudgetCategory{PFCPrimary: &p.Enum}
			bc.PopulatePFCDisplay()

			require.NotNil(t, bc.PFCPrimaryDisplay)
			expected := pfc.PrimaryDisplayName(p.Enum)
			assert.Equal(t, expected, *bc.PFCPrimaryDisplay)
			assert.NotEmpty(t, *bc.PFCPrimaryDisplay)
		})
	}
}

func TestBudgetCategory_PopulatePFCDisplay_AllDetailed(t *testing.T) {
	for _, d := range pfc.AllDetailed() {
		d := d
		t.Run(d.Enum, func(t *testing.T) {
			bc := &BudgetCategory{PFCPrimary: &d.PrimaryEnum, PFCDetailed: &d.Enum}
			bc.PopulatePFCDisplay()

			require.NotNil(t, bc.PFCDetailedDisplay)
			expected := pfc.DetailedDisplayName(d.PrimaryEnum, d.Enum)
			assert.Equal(t, expected, *bc.PFCDetailedDisplay)
			assert.NotEmpty(t, *bc.PFCDetailedDisplay)
		})
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// CategorizationRule – TargetPFCPrimary + TargetPFCDetailed display
// ─────────────────────────────────────────────────────────────────────────────

func TestCategorizationRule_PopulatePFCDisplay_AllPrimaries(t *testing.T) {
	for _, p := range pfc.AllPrimaries() {
		p := p
		t.Run(p.Enum, func(t *testing.T) {
			r := &CategorizationRule{TargetPFCPrimary: &p.Enum}
			r.PopulatePFCDisplay()

			require.NotNil(t, r.TargetPFCPrimaryDisplay)
			expected := pfc.PrimaryDisplayName(p.Enum)
			assert.Equal(t, expected, *r.TargetPFCPrimaryDisplay)
			assert.NotEmpty(t, *r.TargetPFCPrimaryDisplay)
		})
	}
}

func TestCategorizationRule_PopulatePFCDisplay_AllDetailed(t *testing.T) {
	for _, d := range pfc.AllDetailed() {
		d := d
		t.Run(d.Enum, func(t *testing.T) {
			r := &CategorizationRule{TargetPFCPrimary: &d.PrimaryEnum, TargetPFCDetailed: &d.Enum}
			r.PopulatePFCDisplay()

			require.NotNil(t, r.TargetPFCDetailedDisplay)
			expected := pfc.DetailedDisplayName(d.PrimaryEnum, d.Enum)
			assert.Equal(t, expected, *r.TargetPFCDetailedDisplay)
			assert.NotEmpty(t, *r.TargetPFCDetailedDisplay)
		})
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// CustomCategory – ParentPFCPrimary display
// ─────────────────────────────────────────────────────────────────────────────

func TestCustomCategory_PopulatePFCDisplay_AllPrimaries(t *testing.T) {
	for _, p := range pfc.AllPrimaries() {
		p := p
		t.Run(p.Enum, func(t *testing.T) {
			c := &CustomCategory{ParentPFCPrimary: &p.Enum}
			c.PopulatePFCDisplay()

			require.NotNil(t, c.ParentPFCPrimaryDisplay)
			expected := pfc.PrimaryDisplayName(p.Enum)
			assert.Equal(t, expected, *c.ParentPFCPrimaryDisplay)
			assert.NotEmpty(t, *c.ParentPFCPrimaryDisplay)
		})
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// IncomeSource – ValueDisplay for pfc_primary type
// ─────────────────────────────────────────────────────────────────────────────

func TestIncomeSource_PopulatePFCDisplay_AllPrimaries(t *testing.T) {
	for _, p := range pfc.AllPrimaries() {
		p := p
		t.Run(p.Enum, func(t *testing.T) {
			is := &IncomeSource{Type: IncomeSourceTypePFCPrimary, Value: p.Enum}
			is.PopulatePFCDisplay()

			expected := pfc.PrimaryDisplayName(p.Enum)
			assert.Equal(t, expected, is.ValueDisplay)
			assert.NotEmpty(t, is.ValueDisplay)
		})
	}
}

func TestIncomeSource_PopulatePFCDisplay_NonPFCTypeNoOp(t *testing.T) {
	is := &IncomeSource{Type: IncomeSourceTypeTag, Value: "groceries"}
	is.PopulatePFCDisplay()
	assert.Empty(t, is.ValueDisplay, "non-pfc_primary type should not set ValueDisplay")
}

// ─────────────────────────────────────────────────────────────────────────────
// Idempotency
// ─────────────────────────────────────────────────────────────────────────────

func TestTransaction_PopulatePFCDisplay_Idempotent(t *testing.T) {
	primary := "FOOD_AND_DRINK"
	detailed := "FOOD_AND_DRINK_FAST_FOOD"
	tx := &Transaction{PFCPrimary: &primary, PFCDetailed: &detailed}

	tx.PopulatePFCDisplay()
	firstPrimary := *tx.PFCPrimaryDisplay
	firstDetailed := *tx.PFCDetailedDisplay

	tx.PopulatePFCDisplay()
	assert.Equal(t, firstPrimary, *tx.PFCPrimaryDisplay, "second call should yield same primary display")
	assert.Equal(t, firstDetailed, *tx.PFCDetailedDisplay, "second call should yield same detailed display")
}

// ─────────────────────────────────────────────────────────────────────────────
// No-op on nil/empty PFC fields
// ─────────────────────────────────────────────────────────────────────────────

func TestTransaction_PopulatePFCDisplay_NilFieldsNoOp(t *testing.T) {
	tx := &Transaction{}
	tx.PopulatePFCDisplay()
	assert.Nil(t, tx.PFCPrimaryDisplay, "nil PFCPrimary should leave PFCPrimaryDisplay nil")
	assert.Nil(t, tx.PFCDetailedDisplay, "nil PFCDetailed should leave PFCDetailedDisplay nil")
	assert.Nil(t, tx.PFCPrimaryOverrideDisplay)
	assert.Nil(t, tx.PFCDetailedOverrideDisplay)
}

func TestBudgetCategory_PopulatePFCDisplay_NilFieldsNoOp(t *testing.T) {
	bc := &BudgetCategory{}
	bc.PopulatePFCDisplay()
	assert.Nil(t, bc.PFCPrimaryDisplay)
	assert.Nil(t, bc.PFCDetailedDisplay)
}

func TestCategorizationRule_PopulatePFCDisplay_NilFieldsNoOp(t *testing.T) {
	r := &CategorizationRule{}
	r.PopulatePFCDisplay()
	assert.Nil(t, r.TargetPFCPrimaryDisplay)
	assert.Nil(t, r.TargetPFCDetailedDisplay)
}

// ───────────────────────────────────────────────────────────────────────────────
// CategorizationRule – Condition.ValueDisplay for PFC-typed conditions
// ───────────────────────────────────────────────────────────────────────────────

func TestCategorizationRule_PopulatePFCDisplay_ConditionPrimaryValueDisplay(t *testing.T) {
	for _, p := range pfc.AllPrimaries() {
		p := p
		t.Run(p.Enum, func(t *testing.T) {
			rule := &CategorizationRule{
				Conditions: []RuleCondition{
					{Type: ConditionExistingPFCPrimary, Value: p.Enum},
				},
			}
			rule.PopulatePFCDisplay()
			expected := pfc.PrimaryDisplayName(p.Enum)
			require.Equal(t, expected, rule.Conditions[0].ValueDisplay)
			assert.NotEmpty(t, rule.Conditions[0].ValueDisplay)
		})
	}
}

func TestCategorizationRule_PopulatePFCDisplay_ConditionDetailedValueDisplay(t *testing.T) {
	for _, d := range pfc.AllDetailed() {
		d := d
		t.Run(d.Enum, func(t *testing.T) {
			rule := &CategorizationRule{
				Conditions: []RuleCondition{
					{Type: ConditionExistingPFCDetailed, Value: d.Enum},
				},
			}
			rule.PopulatePFCDisplay()
			expected := pfc.DetailedDisplayName(d.PrimaryEnum, d.Enum)
			require.Equal(t, expected, rule.Conditions[0].ValueDisplay)
			assert.NotEmpty(t, rule.Conditions[0].ValueDisplay)
		})
	}
}

func TestCategorizationRule_PopulatePFCDisplay_NonPFCConditionsNoValueDisplay(t *testing.T) {
	rule := &CategorizationRule{
		Conditions: []RuleCondition{
			{Type: ConditionMerchantContains, Value: "starbucks"},
			{Type: ConditionAmountGreaterThan, Value: 5.0},
		},
	}
	rule.PopulatePFCDisplay()
	assert.Empty(t, rule.Conditions[0].ValueDisplay, "merchant condition should have no ValueDisplay")
	assert.Empty(t, rule.Conditions[1].ValueDisplay, "amount condition should have no ValueDisplay")
}

func TestCategorizationRule_PopulatePFCDisplay_ConditionValueDisplayIdempotent(t *testing.T) {
	v := "FOOD_AND_DRINK"
	rule := &CategorizationRule{
		Conditions: []RuleCondition{
			{Type: ConditionExistingPFCPrimary, Value: v},
		},
	}
	rule.PopulatePFCDisplay()
	first := rule.Conditions[0].ValueDisplay
	rule.PopulatePFCDisplay()
	assert.Equal(t, first, rule.Conditions[0].ValueDisplay, "second call should yield same condition ValueDisplay")
}

func TestCustomCategory_PopulatePFCDisplay_NilFieldsNoOp(t *testing.T) {
	c := &CustomCategory{}
	c.PopulatePFCDisplay()
	assert.Nil(t, c.ParentPFCPrimaryDisplay)
}

// ─────────────────────────────────────────────────────────────────────────────
// EffectiveCategory display fields via GetEffectiveCategory
// ─────────────────────────────────────────────────────────────────────────────

// ─────────────────────────────────────────────────────────────────────────────
// Stale-clearing: display field cleared when source enum is set to nil
// ─────────────────────────────────────────────────────────────────────────────

func TestTransaction_PopulatePFCDisplay_ClearsStaleDisplayWhenPrimaryCleared(t *testing.T) {
	primary := "FOOD_AND_DRINK"
	tx := &Transaction{PFCPrimary: &primary}
	tx.PopulatePFCDisplay()
	require.NotNil(t, tx.PFCPrimaryDisplay)

	tx.PFCPrimary = nil
	tx.PopulatePFCDisplay()
	assert.Nil(t, tx.PFCPrimaryDisplay, "stale PFCPrimaryDisplay must be cleared when PFCPrimary becomes nil")
}

func TestBudgetCategory_PopulatePFCDisplay_ClearsStaleDisplayWhenPrimaryCleared(t *testing.T) {
	primary := "FOOD_AND_DRINK"
	bc := &BudgetCategory{PFCPrimary: &primary}
	bc.PopulatePFCDisplay()
	require.NotNil(t, bc.PFCPrimaryDisplay)

	bc.PFCPrimary = nil
	bc.PopulatePFCDisplay()
	assert.Nil(t, bc.PFCPrimaryDisplay, "stale PFCPrimaryDisplay must be cleared when PFCPrimary becomes nil")
}

func TestCategorizationRule_PopulatePFCDisplay_ClearsStaleDisplayWhenTargetCleared(t *testing.T) {
	primary := "FOOD_AND_DRINK"
	r := &CategorizationRule{TargetPFCPrimary: &primary}
	r.PopulatePFCDisplay()
	require.NotNil(t, r.TargetPFCPrimaryDisplay)

	r.TargetPFCPrimary = nil
	r.PopulatePFCDisplay()
	assert.Nil(t, r.TargetPFCPrimaryDisplay, "stale TargetPFCPrimaryDisplay must be cleared when TargetPFCPrimary becomes nil")
}

func TestIncomeSource_PopulatePFCDisplay_ClearsStaleDisplayWhenValueCleared(t *testing.T) {
	is := &IncomeSource{Type: IncomeSourceTypePFCPrimary, Value: "INCOME"}
	is.PopulatePFCDisplay()
	require.NotEmpty(t, is.ValueDisplay)

	is.Value = ""
	is.PopulatePFCDisplay()
	assert.Empty(t, is.ValueDisplay, "stale ValueDisplay must be cleared when Value becomes empty")
}

func TestGetEffectiveCategory_PopulatesPFCDisplayFields(t *testing.T) {
	primary := "FOOD_AND_DRINK"
	detailed := "FOOD_AND_DRINK_FAST_FOOD"
	tx := &Transaction{PFCPrimary: &primary, PFCDetailed: &detailed}

	ec := tx.GetEffectiveCategory()
	require.NotNil(t, ec)
	assert.Equal(t, EffectiveCategoryKindPFC, ec.Kind)

	require.NotNil(t, ec.PFCPrimaryDisplay)
	assert.Equal(t, pfc.PrimaryDisplayName(primary), *ec.PFCPrimaryDisplay)
	assert.NotEmpty(t, *ec.PFCPrimaryDisplay)

	require.NotNil(t, ec.PFCDetailedDisplay)
	assert.Equal(t, pfc.DetailedDisplayName(primary, detailed), *ec.PFCDetailedDisplay)
	assert.NotEmpty(t, *ec.PFCDetailedDisplay)
}

func TestGetEffectiveCategory_OverridePFC_PopulatesDisplayFields(t *testing.T) {
	overrideKind := "pfc"
	primary := "INCOME"
	detailed := "INCOME_WAGES"
	tx := &Transaction{
		CategoryOverrideKind: &overrideKind,
		PFCPrimaryOverride:   &primary,
		PFCDetailedOverride:  &detailed,
	}

	ec := tx.GetEffectiveCategory()
	require.NotNil(t, ec)
	assert.Equal(t, EffectiveCategoryKindPFC, ec.Kind)

	require.NotNil(t, ec.PFCPrimaryDisplay)
	assert.Equal(t, pfc.PrimaryDisplayName(primary), *ec.PFCPrimaryDisplay)

	require.NotNil(t, ec.PFCDetailedDisplay)
	assert.Equal(t, pfc.DetailedDisplayName(primary, detailed), *ec.PFCDetailedDisplay)
}

func TestGetEffectiveCategory_CustomOverride_NoDisplayFields(t *testing.T) {
	overrideKind := "custom"
	catID := "some-uuid"
	tx := &Transaction{
		CategoryOverrideKind:     &overrideKind,
		CustomCategoryOverrideID: &catID,
	}

	ec := tx.GetEffectiveCategory()
	require.NotNil(t, ec)
	assert.Equal(t, EffectiveCategoryKindCustom, ec.Kind)
	// PFC display fields should be nil for custom kind
	assert.Nil(t, ec.PFCPrimaryDisplay)
	assert.Nil(t, ec.PFCDetailedDisplay)
}

func TestGetEffectiveCategory_NilWhenNoPFC(t *testing.T) {
	tx := &Transaction{}
	ec := tx.GetEffectiveCategory()
	assert.Nil(t, ec)
}
