package domain

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/maxnussbaum/BoostedBudget/internal/pfc"
)

// CustomCategory represents a user-defined category for transaction classification.
// Custom categories are FREE for all subscription tiers (Issue #345).
type CustomCategory struct {
	ID                      string  `json:"id" db:"id"`
	UserID                  string  `json:"user_id" db:"user_id"`
	Name                    string  `json:"name" db:"name"`
	ParentPFCPrimary        *string `json:"parent_pfc_primary,omitempty" db:"parent_pfc_primary" enum:"BANK_FEES,ENTERTAINMENT,FOOD_AND_DRINK,GENERAL_MERCHANDISE,GENERAL_SERVICES,GOVERNMENT_AND_NON_PROFIT,HOME_IMPROVEMENT,INCOME,LOAN_DISBURSEMENTS,LOAN_PAYMENTS,MEDICAL,OTHER,PERSONAL_CARE,RENT_AND_UTILITIES,TRANSFER_IN,TRANSFER_OUT,TRANSPORTATION,TRAVEL" doc:"The PFCv2 primary this custom category nests under. This is NOT a cosmetic label: budget roll-up counts the category's transactions under this primary, and analytics classifies them through it -- a category nested under TRANSFER_IN or TRANSFER_OUT makes its transactions TRANSFERS, so they are excluded from cash flow when include_transfers is false (Issue #1609). Leave unset for a category with no PFC parent."`
	ParentPFCPrimaryDisplay *string `json:"parent_pfc_primary_display,omitempty" db:"-"` // populated by PopulatePFCDisplay
	// ParentID nests this custom category under another CUSTOM primary (Issue #1203).
	// Mutually exclusive with ParentPFCPrimary. The parent_id chain itself is capped at
	// one hop (a custom-to-custom-to-custom chain is blocked by
	// enforce_custom_category_two_levels), but a parent nested via ParentPFCPrimary may
	// still have a custom child nested under it via ParentID, so a legal three-level chain
	// exists overall: pfc root -> custom child (ParentPFCPrimary) -> custom grandchild
	// (ParentID). See analytics_category_path.go (Issue #1630 / #1626).
	ParentID   *string   `json:"parent_id,omitempty" db:"parent_id"`
	ParentName *string   `json:"parent_name,omitempty" db:"-"` // display name of the custom parent, populated by the service
	Icon       *string   `json:"icon,omitempty" db:"icon"`
	Color      *string   `json:"color,omitempty" db:"color"` // Hex color like #FF5733
	UsageCount int       `json:"usage_count" db:"usage_count"`
	CreatedAt  time.Time `json:"created_at" db:"created_at"`
	UpdatedAt  time.Time `json:"updated_at" db:"updated_at"`
}

// Validate checks that the CustomCategory has all required fields and valid values.
func (c *CustomCategory) Validate() error {
	if c.UserID == "" {
		return errors.New("user_id is required")
	}

	if c.Name == "" {
		return errors.New("name is required")
	}

	if len(c.Name) > 100 {
		return errors.New("name must be 100 characters or less")
	}

	// Validate color format if provided
	if c.Color != nil && *c.Color != "" {
		if !isValidHexColor(*c.Color) {
			return errors.New("color must be a valid hex color (e.g., #FF5733)")
		}
	}

	// parent_pfc_primary is not decoration. Budget roll-up already resolves through it
	// (transaction_repository.categoryMatchClause) and, since Issue #1609 Item 11, so does the
	// analytics transfer filter (analytics_repository.effectiveTransferExpr): a custom category
	// nested under TRANSFER_IN now makes its transactions transfers. Reject anything the DB CHECK
	// valid_custom_category_parent_pfc (migration 000065) would reject, so the caller gets a
	// message naming the field instead of an opaque constraint violation. "" means "unset" and is
	// normalised to nil by the service before it reaches the repository.
	if c.ParentPFCPrimary != nil && *c.ParentPFCPrimary != "" &&
		!pfc.IsValidPrimary(*c.ParentPFCPrimary) {
		return fmt.Errorf("parent_pfc_primary must be one of %s", strings.Join(pfc.PrimaryEnums(), ", "))
	}

	// Hierarchy: a custom category is at most one of top-level, nested under a
	// PFC primary, or nested under a custom primary (Issue #1203).
	if c.ParentID != nil && *c.ParentID != "" {
		if c.ParentPFCPrimary != nil && *c.ParentPFCPrimary != "" {
			return errors.New("category cannot have both a custom parent and a PFC parent")
		}
		if c.ID != "" && *c.ParentID == c.ID {
			return errors.New("category cannot be its own parent")
		}
	}

	return nil
}

// CreateCustomCategoryRequest represents the request to create a new custom category.
type CreateCustomCategoryRequest struct {
	Name             string  `json:"name" validate:"required,max=100"`
	ParentPFCPrimary *string `json:"parent_pfc_primary,omitempty" enum:"BANK_FEES,ENTERTAINMENT,FOOD_AND_DRINK,GENERAL_MERCHANDISE,GENERAL_SERVICES,GOVERNMENT_AND_NON_PROFIT,HOME_IMPROVEMENT,INCOME,LOAN_DISBURSEMENTS,LOAN_PAYMENTS,MEDICAL,OTHER,PERSONAL_CARE,RENT_AND_UTILITIES,TRANSFER_IN,TRANSFER_OUT,TRANSPORTATION,TRAVEL," doc:"The PFCv2 primary this custom category nests under. This is NOT a cosmetic label: budget roll-up counts the category's transactions under this primary, and analytics classifies them through it -- a category nested under TRANSFER_IN or TRANSFER_OUT makes its transactions TRANSFERS, so they are excluded from cash flow when include_transfers is false (Issue #1609). Leave unset for a category with no PFC parent. Send an empty string to clear it."`
	ParentID         *string `json:"parent_id,omitempty" validate:"omitempty,uuid"`
	Icon             *string `json:"icon,omitempty" validate:"omitempty,max=50"`
	Color            *string `json:"color,omitempty" validate:"omitempty,hexcolor"`
}

// UpdateCustomCategoryRequest represents the request to update a custom category.
// ParentID and ParentPFCPrimary are pointers so the caller can distinguish
// "leave unchanged" (nil) from "set to a value". To clear a parent, send an
// explicit empty string.
type UpdateCustomCategoryRequest struct {
	Name             *string `json:"name,omitempty" validate:"omitempty,max=100"`
	ParentPFCPrimary *string `json:"parent_pfc_primary,omitempty" enum:"BANK_FEES,ENTERTAINMENT,FOOD_AND_DRINK,GENERAL_MERCHANDISE,GENERAL_SERVICES,GOVERNMENT_AND_NON_PROFIT,HOME_IMPROVEMENT,INCOME,LOAN_DISBURSEMENTS,LOAN_PAYMENTS,MEDICAL,OTHER,PERSONAL_CARE,RENT_AND_UTILITIES,TRANSFER_IN,TRANSFER_OUT,TRANSPORTATION,TRAVEL," doc:"The PFCv2 primary this custom category nests under. This is NOT a cosmetic label: budget roll-up counts the category's transactions under this primary, and analytics classifies them through it -- a category nested under TRANSFER_IN or TRANSFER_OUT makes its transactions TRANSFERS, so they are excluded from cash flow when include_transfers is false (Issue #1609). Leave unset for a category with no PFC parent. Send an empty string to clear it."`
	ParentID         *string `json:"parent_id,omitempty"`
	Icon             *string `json:"icon,omitempty" validate:"omitempty,max=50"`
	Color            *string `json:"color,omitempty" validate:"omitempty,hexcolor"`
}

// CategoryUsage reports how many records reference a custom category, computed live
// (not from the cached usage_count). Used to warn the user before deletion (Issue #1203).
type CategoryUsage struct {
	TransactionCount int `json:"transaction_count" db:"-"`
	BudgetCount      int `json:"budget_count" db:"-"`
	RuleCount        int `json:"rule_count" db:"-"`
	ChildCount       int `json:"child_count" db:"-"`
}

// MergeCategoriesRequest represents the request to merge two categories.
type MergeCategoriesRequest struct {
	FromCategoryID string `json:"from_category_id" validate:"required,uuid"`
	ToCategoryID   string `json:"to_category_id" validate:"required,uuid"`
}

// UpdateTransactionCategoryRequest represents the request to update a transaction's category.
type UpdateTransactionCategoryRequest struct {
	Kind             string  `json:"kind"`
	PFCPrimary       *string `json:"pfc_primary,omitempty"`
	PFCDetailed      *string `json:"pfc_detailed,omitempty"`
	CustomCategoryID *string `json:"custom_category_id,omitempty"`
}

// AddTransactionTagRequest represents the request to add a tag to a transaction.
type AddTransactionTagRequest struct {
	Tag string `json:"tag" validate:"required,max=100"`
}

// Category-related errors
var (
	ErrCategoryNotFound      = errors.New("category not found")
	ErrCategoryAlreadyExists = errors.New("category with this name already exists")
	ErrCategoryLimitExceeded = errors.New("category limit exceeded for subscription tier")
	// Hierarchy errors (Issue #1203)
	ErrCategoryParentHasParent = errors.New("parent category must be top-level (no grandchildren allowed)")
	ErrCategoryHasChildren     = errors.New("category has children and cannot be nested under another category")
	ErrCategoryInvalidParent   = errors.New("invalid parent category")
)
