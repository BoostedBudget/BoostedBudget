// Package domain — PFC display name population helpers.
// PopulatePFCDisplay() enriches each domain type that carries PFC enum fields
// with sibling human-readable display-name fields sourced from the pfc registry.
// The display fields are computed on demand and are never stored in the database.
package domain

import (
	"strings"

	"github.com/maxnussbaum/BoostedBudget/internal/pfc"
)

// notEmpty reports whether a string pointer is non-nil and non-empty.
func notEmpty(s *string) bool {
	return s != nil && *s != ""
}

// PopulatePFCDisplay fills in PFCPrimaryDisplay, PFCDetailedDisplay,
// PFCPrimaryOverrideDisplay, and PFCDetailedOverrideDisplay from the pfc registry.
func (t *Transaction) PopulatePFCDisplay() {
	if notEmpty(t.PFCPrimary) {
		d := pfc.PrimaryDisplayName(*t.PFCPrimary)
		t.PFCPrimaryDisplay = &d
	} else {
		t.PFCPrimaryDisplay = nil
	}
	if notEmpty(t.PFCPrimary) && notEmpty(t.PFCDetailed) {
		d := pfc.DetailedDisplayName(*t.PFCPrimary, *t.PFCDetailed)
		t.PFCDetailedDisplay = &d
	} else {
		t.PFCDetailedDisplay = nil
	}
	if notEmpty(t.PFCPrimaryOverride) {
		d := pfc.PrimaryDisplayName(*t.PFCPrimaryOverride)
		t.PFCPrimaryOverrideDisplay = &d
	} else {
		t.PFCPrimaryOverrideDisplay = nil
	}
	if notEmpty(t.PFCPrimaryOverride) && notEmpty(t.PFCDetailedOverride) {
		d := pfc.DetailedDisplayName(*t.PFCPrimaryOverride, *t.PFCDetailedOverride)
		t.PFCDetailedOverrideDisplay = &d
	} else {
		t.PFCDetailedOverrideDisplay = nil
	}
}

// PopulatePFCDisplay fills in PFCPrimaryDisplay and PFCDetailedDisplay
// from the pfc registry.
func (bc *BudgetCategory) PopulatePFCDisplay() {
	if notEmpty(bc.PFCPrimary) {
		d := pfc.PrimaryDisplayName(*bc.PFCPrimary)
		bc.PFCPrimaryDisplay = &d
	} else {
		bc.PFCPrimaryDisplay = nil
	}
	if notEmpty(bc.PFCPrimary) && notEmpty(bc.PFCDetailed) {
		d := pfc.DetailedDisplayName(*bc.PFCPrimary, *bc.PFCDetailed)
		bc.PFCDetailedDisplay = &d
	} else {
		bc.PFCDetailedDisplay = nil
	}
}

// PopulatePFCDisplay fills in TargetPFCPrimaryDisplay, TargetPFCDetailedDisplay, and
// ValueDisplay on PFC-typed conditions from the pfc registry.
func (r *CategorizationRule) PopulatePFCDisplay() {
	if notEmpty(r.TargetPFCPrimary) {
		d := pfc.PrimaryDisplayName(*r.TargetPFCPrimary)
		r.TargetPFCPrimaryDisplay = &d
	} else {
		r.TargetPFCPrimaryDisplay = nil
	}
	if notEmpty(r.TargetPFCPrimary) && notEmpty(r.TargetPFCDetailed) {
		d := pfc.DetailedDisplayName(*r.TargetPFCPrimary, *r.TargetPFCDetailed)
		r.TargetPFCDetailedDisplay = &d
	} else {
		r.TargetPFCDetailedDisplay = nil
	}
	for i := range r.Conditions {
		v, ok := r.Conditions[i].Value.(string)
		if !ok || v == "" {
			r.Conditions[i].ValueDisplay = ""
			continue
		}
		switch r.Conditions[i].Type {
		case ConditionExistingPFCPrimary:
			r.Conditions[i].ValueDisplay = pfc.PrimaryDisplayName(v)
		case ConditionExistingPFCDetailed:
			for _, d := range pfc.AllDetailed() {
				if d.Enum == v {
					r.Conditions[i].ValueDisplay = pfc.DetailedDisplayName(d.PrimaryEnum, v)
					break
				}
			}
		}
	}
}

// PopulatePFCDisplay fills in ParentPFCPrimaryDisplay from the pfc registry.
func (c *CustomCategory) PopulatePFCDisplay() {
	if notEmpty(c.ParentPFCPrimary) {
		d := pfc.PrimaryDisplayName(*c.ParentPFCPrimary)
		c.ParentPFCPrimaryDisplay = &d
	} else {
		c.ParentPFCPrimaryDisplay = nil
	}
}

// PopulatePFCDisplay fills in ValueDisplay on the IncomeSource when its Type is
// IncomeSourceTypePFCPrimary.
func (is *IncomeSource) PopulatePFCDisplay() {
	if is.Type == IncomeSourceTypePFCPrimary && is.Value != "" {
		is.ValueDisplay = pfc.PrimaryDisplayName(is.Value)
	} else {
		is.ValueDisplay = ""
	}
}

// PopulatePFCDisplay fills in ValueDisplay/ActualDisplay when Field references a
// PFC-typed attribute. Both bare (`pfc_primary`) and dotted (`*.pfc_primary`) field
// name forms are recognized — the rule engine and UI historically use both.
func (c *ConditionEvalResult) PopulatePFCDisplay() {
	c.ValueDisplay = pfcDisplayForConditionField(c.Field, c.Value)
	c.ActualDisplay = pfcDisplayForConditionField(c.Field, c.Actual)
}

// PopulatePFCDisplay enriches every condition result in the dry-run response,
// mirroring the aggregate-level pattern used by CategorizationRule.
func (r *RuleTestResult) PopulatePFCDisplay() {
	for i := range r.ConditionResults {
		r.ConditionResults[i].PopulatePFCDisplay()
	}
}

// pfcDisplayForConditionField returns the humanized display for a condition
// field/value pair when the field is a PFC-typed attribute. Returns "" otherwise.
func pfcDisplayForConditionField(field string, raw interface{}) string {
	v, ok := raw.(string)
	if !ok || v == "" {
		return ""
	}
	isPrimary := field == "pfc_primary" || strings.HasSuffix(field, ".pfc_primary")
	isDetailed := field == "pfc_detailed" || strings.HasSuffix(field, ".pfc_detailed")
	if isPrimary {
		return pfc.PrimaryDisplayName(v)
	}
	if isDetailed {
		// For detailed, find the primary enum that pairs with it
		for _, d := range pfc.AllDetailed() {
			if d.Enum == v {
				return pfc.DetailedDisplayName(d.PrimaryEnum, v)
			}
		}
	}
	return ""
}
