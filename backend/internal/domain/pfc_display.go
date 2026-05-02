// Package domain — PFC display name population helpers.
// PopulatePFCDisplay() enriches each domain type that carries PFC enum fields
// with sibling human-readable display-name fields sourced from the pfc registry.
// The display fields are computed on demand and are never stored in the database.
package domain

import "github.com/maxnussbaum/BoostedBudget/internal/pfc"

// notEmpty reports whether a string pointer is non-nil and non-empty.
func notEmpty(s *string) bool {
	return s != nil && *s != ""
}

// PopulatePFCDisplay fills in PFCPrimaryDisplay, PFCDetailedDisplay,
// PFCPrimaryOverrideDisplay, and PFCDetailedOverrideDisplay on the transaction
// from the pfc registry. Safe to call multiple times (idempotent).
func (t *Transaction) PopulatePFCDisplay() {
	if notEmpty(t.PFCPrimary) {
		d := pfc.PrimaryDisplayName(*t.PFCPrimary)
		t.PFCPrimaryDisplay = &d
	}
	if notEmpty(t.PFCPrimary) && notEmpty(t.PFCDetailed) {
		d := pfc.DetailedDisplayName(*t.PFCPrimary, *t.PFCDetailed)
		t.PFCDetailedDisplay = &d
	}
	if notEmpty(t.PFCPrimaryOverride) {
		d := pfc.PrimaryDisplayName(*t.PFCPrimaryOverride)
		t.PFCPrimaryOverrideDisplay = &d
	}
	if notEmpty(t.PFCPrimaryOverride) && notEmpty(t.PFCDetailedOverride) {
		d := pfc.DetailedDisplayName(*t.PFCPrimaryOverride, *t.PFCDetailedOverride)
		t.PFCDetailedOverrideDisplay = &d
	}
}

// PopulatePFCDisplay fills in PFCPrimaryDisplay and PFCDetailedDisplay on the
// BudgetCategory from the pfc registry. Safe to call multiple times (idempotent).
func (bc *BudgetCategory) PopulatePFCDisplay() {
	if notEmpty(bc.PFCPrimary) {
		d := pfc.PrimaryDisplayName(*bc.PFCPrimary)
		bc.PFCPrimaryDisplay = &d
	}
	if notEmpty(bc.PFCPrimary) && notEmpty(bc.PFCDetailed) {
		d := pfc.DetailedDisplayName(*bc.PFCPrimary, *bc.PFCDetailed)
		bc.PFCDetailedDisplay = &d
	}
}

// PopulatePFCDisplay fills in TargetPFCPrimaryDisplay, TargetPFCDetailedDisplay, and
// ValueDisplay on PFC-typed conditions for the CategorizationRule from the pfc registry.
// Safe to call multiple times (idempotent).
func (r *CategorizationRule) PopulatePFCDisplay() {
	if notEmpty(r.TargetPFCPrimary) {
		d := pfc.PrimaryDisplayName(*r.TargetPFCPrimary)
		r.TargetPFCPrimaryDisplay = &d
	}
	if notEmpty(r.TargetPFCPrimary) && notEmpty(r.TargetPFCDetailed) {
		d := pfc.DetailedDisplayName(*r.TargetPFCPrimary, *r.TargetPFCDetailed)
		r.TargetPFCDetailedDisplay = &d
	}
	for i := range r.Conditions {
		v, ok := r.Conditions[i].Value.(string)
		if !ok || v == "" {
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

// PopulatePFCDisplay fills in ParentPFCPrimaryDisplay on the CustomCategory
// from the pfc registry. Safe to call multiple times (idempotent).
func (c *CustomCategory) PopulatePFCDisplay() {
	if notEmpty(c.ParentPFCPrimary) {
		d := pfc.PrimaryDisplayName(*c.ParentPFCPrimary)
		c.ParentPFCPrimaryDisplay = &d
	}
}

// PopulatePFCDisplay fills in ValueDisplay on the IncomeSource when its Type is
// IncomeSourceTypePFCPrimary. Safe to call multiple times (idempotent).
func (is *IncomeSource) PopulatePFCDisplay() {
	if is.Type == IncomeSourceTypePFCPrimary && is.Value != "" {
		is.ValueDisplay = pfc.PrimaryDisplayName(is.Value)
	}
}
