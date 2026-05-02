// Package domain — PFC display name population helpers.
// PopulatePFCDisplay() enriches each domain type that carries PFC enum fields
// with sibling human-readable display-name fields sourced from the pfc registry.
// The display fields are computed on demand and are never stored in the database.
package domain

import "github.com/maxnussbaum/BoostedBudget/internal/pfc"

// PopulatePFCDisplay fills in PFCPrimaryDisplay, PFCDetailedDisplay,
// PFCPrimaryOverrideDisplay, and PFCDetailedOverrideDisplay on the transaction
// from the pfc registry. Safe to call multiple times (idempotent).
func (t *Transaction) PopulatePFCDisplay() {
	if t.PFCPrimary != nil && *t.PFCPrimary != "" {
		d := pfc.PrimaryDisplayName(*t.PFCPrimary)
		t.PFCPrimaryDisplay = &d
	}
	if t.PFCPrimary != nil && *t.PFCPrimary != "" && t.PFCDetailed != nil && *t.PFCDetailed != "" {
		d := pfc.DetailedDisplayName(*t.PFCPrimary, *t.PFCDetailed)
		t.PFCDetailedDisplay = &d
	}
	if t.PFCPrimaryOverride != nil && *t.PFCPrimaryOverride != "" {
		d := pfc.PrimaryDisplayName(*t.PFCPrimaryOverride)
		t.PFCPrimaryOverrideDisplay = &d
	}
	if t.PFCPrimaryOverride != nil && *t.PFCPrimaryOverride != "" && t.PFCDetailedOverride != nil && *t.PFCDetailedOverride != "" {
		d := pfc.DetailedDisplayName(*t.PFCPrimaryOverride, *t.PFCDetailedOverride)
		t.PFCDetailedOverrideDisplay = &d
	}
}

// PopulatePFCDisplay fills in PFCPrimaryDisplay and PFCDetailedDisplay on the
// BudgetCategory from the pfc registry. Safe to call multiple times (idempotent).
func (bc *BudgetCategory) PopulatePFCDisplay() {
	if bc.PFCPrimary != nil && *bc.PFCPrimary != "" {
		d := pfc.PrimaryDisplayName(*bc.PFCPrimary)
		bc.PFCPrimaryDisplay = &d
	}
	if bc.PFCPrimary != nil && *bc.PFCPrimary != "" && bc.PFCDetailed != nil && *bc.PFCDetailed != "" {
		d := pfc.DetailedDisplayName(*bc.PFCPrimary, *bc.PFCDetailed)
		bc.PFCDetailedDisplay = &d
	}
}

// PopulatePFCDisplay fills in TargetPFCPrimaryDisplay and TargetPFCDetailedDisplay
// on the CategorizationRule from the pfc registry. Safe to call multiple times (idempotent).
func (r *CategorizationRule) PopulatePFCDisplay() {
	if r.TargetPFCPrimary != nil && *r.TargetPFCPrimary != "" {
		d := pfc.PrimaryDisplayName(*r.TargetPFCPrimary)
		r.TargetPFCPrimaryDisplay = &d
	}
	if r.TargetPFCPrimary != nil && *r.TargetPFCPrimary != "" && r.TargetPFCDetailed != nil && *r.TargetPFCDetailed != "" {
		d := pfc.DetailedDisplayName(*r.TargetPFCPrimary, *r.TargetPFCDetailed)
		r.TargetPFCDetailedDisplay = &d
	}
}

// PopulatePFCDisplay fills in ParentPFCPrimaryDisplay on the CustomCategory
// from the pfc registry. Safe to call multiple times (idempotent).
func (c *CustomCategory) PopulatePFCDisplay() {
	if c.ParentPFCPrimary != nil && *c.ParentPFCPrimary != "" {
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
