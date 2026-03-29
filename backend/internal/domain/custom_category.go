package domain

import (
	"errors"
	"time"
)

// CustomCategory represents a user-defined category for transaction classification.
// Custom categories are FREE for all subscription tiers (Issue #345).
type CustomCategory struct {
	ID             string    `json:"id" db:"id"`
	UserID         string    `json:"user_id" db:"user_id"`
	Name           string    `json:"name" db:"name"`
	ParentCategory *string   `json:"parent_category,omitempty" db:"parent_category"` // Maps to Plaid parent
	Icon           *string   `json:"icon,omitempty" db:"icon"`
	Color          *string   `json:"color,omitempty" db:"color"` // Hex color like #FF5733
	UsageCount     int       `json:"usage_count" db:"usage_count"`
	CreatedAt      time.Time `json:"created_at" db:"created_at"`
	UpdatedAt      time.Time `json:"updated_at" db:"updated_at"`
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

	return nil
}

// CreateCustomCategoryRequest represents the request to create a new custom category.
type CreateCustomCategoryRequest struct {
	Name           string  `json:"name" validate:"required,max=100"`
	ParentCategory *string `json:"parent_category,omitempty"`
	Icon           *string `json:"icon,omitempty" validate:"omitempty,max=50"`
	Color          *string `json:"color,omitempty" validate:"omitempty,hexcolor"`
}

// UpdateCustomCategoryRequest represents the request to update a custom category.
type UpdateCustomCategoryRequest struct {
	Name           *string `json:"name,omitempty" validate:"omitempty,max=100"`
	ParentCategory *string `json:"parent_category,omitempty"`
	Icon           *string `json:"icon,omitempty" validate:"omitempty,max=50"`
	Color          *string `json:"color,omitempty" validate:"omitempty,hexcolor"`
}

// MergeCategoriesRequest represents the request to merge two categories.
type MergeCategoriesRequest struct {
	FromCategoryID string `json:"from_category_id" validate:"required,uuid"`
	ToCategoryID   string `json:"to_category_id" validate:"required,uuid"`
}

// UpdateTransactionCategoryRequest represents the request to update a transaction's category.
type UpdateTransactionCategoryRequest struct {
	Category []string `json:"category" validate:"required,min=1"`
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
)

// StandardPlaidCategories returns Plaid's standard category list.
// These are the top-level categories that transactions are typically assigned to.
func StandardPlaidCategories() []string {
	return []string{
		"Bank Fees",
		"Cash Advance",
		"Community",
		"Food and Drink",
		"Healthcare",
		"Interest",
		"Payment",
		"Recreation",
		"Service",
		"Shops",
		"Tax",
		"Transfer",
		"Travel",
		"Income",
	}
}
