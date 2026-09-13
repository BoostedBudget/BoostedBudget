package domain

import (
	"fmt"
	"github.com/maxnussbaum/BoostedBudget/internal/pfc"
)

// BudgetGroupMember is a category selector, never a separate allocation. Custom
// hierarchy fields are resolved by the repository, not accepted from clients.
type BudgetGroupMember struct {
	CategoryKind                string  `json:"category_kind" enum:"pfc,custom,uncategorized"`
	PFCPrimary                  *string `json:"pfc_primary,omitempty"`
	PFCDetailed                 *string `json:"pfc_detailed,omitempty"`
	CustomCategoryID            *string `json:"custom_category_id,omitempty"`
	CustomParentID              *string `json:"-"`
	CustomParentPFCPrimary      *string `json:"-"`
	CustomGrandparentPFCPrimary *string `json:"-"`
}

// ValidateForCreate distinguishes a new group from a retained group whose final
// custom member was deleted. The latter remains editable with its stable ID.
func (bc *BudgetCategory) ValidateForCreate() error {
	if bc.CategoryKind == "group" && len(bc.Members) == 0 {
		return fmt.Errorf("%w: a new group requires at least one member", ErrInvalidBudget)
	}
	return bc.Validate()
}

func (m BudgetGroupMember) Validate() error {
	if m.CategoryKind == "uncategorized" {
		if m.PFCPrimary == nil && m.PFCDetailed == nil && m.CustomCategoryID == nil {
			return nil
		}
		return fmt.Errorf("%w: Uncategorized member must not carry selectors", ErrInvalidBudget)
	}
	if m.CategoryKind != "pfc" && m.CategoryKind != "custom" {
		return fmt.Errorf("%w: group member must be pfc, custom, or uncategorized", ErrInvalidBudget)
	}
	if m.CategoryKind == "pfc" {
		if m.PFCPrimary == nil || !pfc.IsValidPrimary(*m.PFCPrimary) {
			return fmt.Errorf("%w: group member PFC primary is invalid", ErrInvalidBudget)
		}
		if m.PFCDetailed != nil {
			valid := false
			for _, d := range pfc.AllDetailed() {
				if d.PrimaryEnum == *m.PFCPrimary && d.Enum == *m.PFCDetailed {
					valid = true
					break
				}
			}
			if !valid {
				return fmt.Errorf("%w: group member PFC detailed does not belong to its primary", ErrInvalidBudget)
			}
		}
	}
	return (&BudgetCategory{BudgetID: "member", CategoryKind: m.CategoryKind, PFCPrimary: m.PFCPrimary, PFCDetailed: m.PFCDetailed, CustomCategoryID: m.CustomCategoryID}).Validate()
}

// Covers reports semantic containment using repository-resolved custom ancestry.
func (m BudgetGroupMember) Covers(other BudgetGroupMember) bool {
	equal := func(a, b *string) bool { return a != nil && b != nil && *a == *b }
	switch m.CategoryKind {
	case "uncategorized":
		return other.CategoryKind == "uncategorized"
	case "pfc":
		if other.CategoryKind == "pfc" {
			return equal(m.PFCPrimary, other.PFCPrimary) && (!hasPFCDetailed(m.PFCDetailed) || equal(m.PFCDetailed, other.PFCDetailed))
		}
		return !hasPFCDetailed(m.PFCDetailed) && other.CategoryKind == "custom" && (equal(m.PFCPrimary, other.CustomParentPFCPrimary) || equal(m.PFCPrimary, other.CustomGrandparentPFCPrimary))
	case "custom":
		return other.CategoryKind == "custom" && (equal(m.CustomCategoryID, other.CustomCategoryID) || equal(m.CustomCategoryID, other.CustomParentID))
	}
	return false
}

// NormalizeBudgetGroupMembers removes duplicates and selectors already covered
// by another member; input order is preserved for surviving selectors.
func NormalizeBudgetGroupMembers(members []BudgetGroupMember) []BudgetGroupMember {
	result := make([]BudgetGroupMember, 0, len(members))
	for i, m := range members {
		covered := false
		for j, other := range members {
			if i != j && other.Covers(m) && (!m.Covers(other) || j < i) {
				covered = true
				break
			}
		}
		if !covered {
			result = append(result, m)
		}
	}
	return result
}

// ValidateBudgetGroupCoverage checks only shared buckets. Individual lines have
// precedence over groups and may therefore intentionally overlap them.
func ValidateBudgetGroupCoverage(categories []BudgetCategory) error {
	remainders := 0
	for i, c := range categories {
		if c.CategoryKind == "remainder" {
			remainders++
			if remainders > 1 {
				return fmt.Errorf("%w: only one Other bucket is allowed", ErrInvalidBudget)
			}
		}
		if c.CategoryKind != "group" {
			continue
		}
		for _, other := range categories[:i] {
			if other.CategoryKind != "group" {
				continue
			}
			for _, a := range c.Members {
				for _, b := range other.Members {
					if a.Covers(b) || b.Covers(a) {
						return fmt.Errorf("%w: groups must not cover overlapping categories", ErrInvalidBudget)
					}
				}
			}
		}
	}
	return nil
}
