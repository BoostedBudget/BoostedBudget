package domain

import (
	"errors"
	"fmt"
	"time"
)

// BudgetPeriod represents the budget time period type.
type BudgetPeriod string

// Supported budget periods.
const (
	BudgetPeriodWeekly    BudgetPeriod = "weekly"
	BudgetPeriodMonthly   BudgetPeriod = "monthly"
	BudgetPeriodQuarterly BudgetPeriod = "quarterly"
	BudgetPeriodYearly    BudgetPeriod = "yearly"
	BudgetPeriodCustom    BudgetPeriod = "custom"
)

// validBudgetPeriods is a set of all valid budget periods.
var validBudgetPeriods = map[BudgetPeriod]bool{
	BudgetPeriodWeekly:    true,
	BudgetPeriodMonthly:   true,
	BudgetPeriodQuarterly: true,
	BudgetPeriodYearly:    true,
	BudgetPeriodCustom:    true,
}

// IsValid checks if the budget period is valid.
func (bp BudgetPeriod) IsValid() bool {
	return validBudgetPeriods[bp]
}

// =============================================================================
// Issue #491: Envelope Budgeting Types
// =============================================================================

// IncomeSourceType represents the type of income source for envelope budgeting.
type IncomeSourceType string

// Supported income source types.
const (
	IncomeSourceTypePlaidCategory IncomeSourceType = "plaid_category"
	IncomeSourceTypeTag           IncomeSourceType = "tag"
	IncomeSourceTypeManual        IncomeSourceType = "manual"
)

// validIncomeSourceTypes is a set of all valid income source types.
var validIncomeSourceTypes = map[IncomeSourceType]bool{
	IncomeSourceTypePlaidCategory: true,
	IncomeSourceTypeTag:           true,
	IncomeSourceTypeManual:        true,
}

// IsValid checks if the income source type is valid.
func (ist IncomeSourceType) IsValid() bool {
	return validIncomeSourceTypes[ist]
}

// IncomeSource represents a tracked source of income for envelope budgeting.
type IncomeSource struct {
	Type        IncomeSourceType `json:"type"`
	Value       string           `json:"value"`                 // Category name, tag name, or description
	Description string           `json:"description,omitempty"` // Optional user-friendly label
}

// Validate checks that the IncomeSource has valid fields.
func (is *IncomeSource) Validate() error {
	if !is.Type.IsValid() {
		return errors.New("invalid income source type")
	}
	if is.Value == "" {
		return errors.New("income source value is required")
	}
	return nil
}

// MatchLogic represents how a budget category matches transactions.
type MatchLogic string

// Supported match logic values.
const (
	MatchLogicCategoryOnly MatchLogic = "category_only"
	MatchLogicTagOnly      MatchLogic = "tag_only"
	MatchLogicAnd          MatchLogic = "and"
	MatchLogicOr           MatchLogic = "or"
)

// validMatchLogicValues is a set of all valid match logic values.
var validMatchLogicValues = map[MatchLogic]bool{
	MatchLogicCategoryOnly: true,
	MatchLogicTagOnly:      true,
	MatchLogicAnd:          true,
	MatchLogicOr:           true,
}

// IsValid checks if the match logic value is valid.
func (ml MatchLogic) IsValid() bool {
	return validMatchLogicValues[ml]
}

// Budget represents a user-defined budget for expense tracking.
// Budgets are FREE for all subscription tiers (Issue #27).
type Budget struct {
	ID           string       `json:"id" db:"id"`
	UserID       string       `json:"user_id" db:"user_id"`
	Name         string       `json:"name" db:"name"`
	Description  *string      `json:"description,omitempty" db:"description"`
	Period       BudgetPeriod `json:"period" db:"period"`
	StartDate    time.Time    `json:"start_date" db:"start_date"`
	EndDate      *time.Time   `json:"end_date,omitempty" db:"end_date"`
	Amount       float64      `json:"amount" db:"amount"`
	CurrencyCode string       `json:"currency_code" db:"currency_code"`
	IsActive     bool         `json:"is_active" db:"is_active"`
	CreatedAt    time.Time    `json:"created_at" db:"created_at"`
	UpdatedAt    time.Time    `json:"updated_at" db:"updated_at"`

	// Envelope budgeting fields (Issue #491)
	IncomeAmount  float64        `json:"income_amount" db:"income_amount"`
	IncomeSources []IncomeSource `json:"income_sources" db:"-"` // JSONB, populated from DB

	// Associated data (populated on demand, not stored in DB)
	Categories []BudgetCategory `json:"categories,omitempty" db:"-"`
}

// Validate checks that the Budget has all required fields and valid values.
func (b *Budget) Validate() error {
	if b.UserID == "" {
		return errors.New("user_id is required")
	}

	if b.Name == "" {
		return errors.New("name is required")
	}

	if len(b.Name) > 100 {
		return errors.New("name must be 100 characters or less")
	}

	if !b.Period.IsValid() {
		return errors.New("invalid budget period: must be weekly, monthly, quarterly, yearly, or custom")
	}

	if b.StartDate.IsZero() {
		return errors.New("start_date is required")
	}

	if b.Amount <= 0 {
		return errors.New("amount must be positive")
	}

	if b.CurrencyCode == "" {
		return errors.New("currency_code is required")
	}

	// Custom periods require an end_date
	if b.Period == BudgetPeriodCustom && b.EndDate == nil {
		return errors.New("end_date is required for custom period budgets")
	}

	// End date must be after start date if set
	if b.EndDate != nil && !b.EndDate.After(b.StartDate) {
		return errors.New("end_date must be after start_date")
	}

	// Envelope budgeting validation (Issue #491)
	// Income amount can be 0 for traditional budgets, but not negative
	if b.IncomeAmount < 0 {
		return errors.New("income_amount cannot be negative")
	}

	// Validate each income source
	for i, source := range b.IncomeSources {
		if err := source.Validate(); err != nil {
			return fmt.Errorf("income_sources[%d]: %w", i, err)
		}
	}

	return nil
}

// TotalAllocated returns the sum of all category allocations.
func (b *Budget) TotalAllocated() float64 {
	var total float64
	for _, cat := range b.Categories {
		total += cat.AllocatedAmount
	}
	return total
}

// TotalSpent returns the sum of all category spent amounts.
func (b *Budget) TotalSpent() float64 {
	var total float64
	for _, cat := range b.Categories {
		total += cat.SpentAmount
	}
	return total
}

// RemainingAmount returns the budget amount minus total spent.
func (b *Budget) RemainingAmount() float64 {
	return b.Amount - b.TotalSpent()
}

// IsOverBudget returns true if total spent exceeds the budget amount.
func (b *Budget) IsOverBudget() bool {
	return b.TotalSpent() > b.Amount
}

// PercentUsed returns the percentage of budget used (0-100+).
func (b *Budget) PercentUsed() float64 {
	if b.Amount == 0 {
		if b.TotalSpent() > 0 {
			return 100 // Over budget with no amount
		}
		return 0
	}
	return (b.TotalSpent() / b.Amount) * 100
}

// UnallocatedAmount returns the income amount minus total allocated to categories.
// This is used for envelope budgeting to show how much income remains to be allocated.
// Issue #491
func (b *Budget) UnallocatedAmount() float64 {
	return b.IncomeAmount - b.TotalAllocated()
}

// IsFullyAllocated returns true if all income has been allocated to categories.
// Returns true if IncomeAmount is 0 (no income to allocate) or if allocations >= income.
// Issue #491
func (b *Budget) IsFullyAllocated() bool {
	return b.TotalAllocated() >= b.IncomeAmount
}

// BudgetCategory represents a category allocation within a budget.
type BudgetCategory struct {
	ID              string    `json:"id" db:"id"`
	BudgetID        string    `json:"budget_id" db:"budget_id"`
	Category        []string  `json:"category" db:"category"` // JSONB array matching transaction category format
	AllocatedAmount float64   `json:"allocated_amount" db:"allocated_amount"`
	SpentAmount     float64   `json:"spent_amount" db:"spent_amount"` // Denormalized, updated via Issue #451
	CreatedAt       time.Time `json:"created_at" db:"created_at"`
	UpdatedAt       time.Time `json:"updated_at" db:"updated_at"`

	// Flexible matching fields (Issue #491)
	Tags       []string   `json:"tags" db:"-"`             // JSONB array of tags to match
	MatchLogic MatchLogic `json:"match_logic" db:"match_logic"` // How to combine category and tags
}

// Validate checks that the BudgetCategory has all required fields.
func (bc *BudgetCategory) Validate() error {
	if bc.BudgetID == "" {
		return errors.New("budget_id is required")
	}

	// Validate match_logic if set (Issue #491)
	// Empty match_logic is valid and treated as category_only
	if bc.MatchLogic != "" && !bc.MatchLogic.IsValid() {
		return errors.New("invalid match_logic value")
	}

	// If match_logic is tag_only or and, tags are required
	if bc.MatchLogic == MatchLogicTagOnly || bc.MatchLogic == MatchLogicAnd {
		if len(bc.Tags) == 0 {
			return errors.New("tags are required when match_logic is tag_only or and")
		}
	}

	// Category is required unless match_logic is tag_only
	if len(bc.Category) == 0 && bc.MatchLogic != MatchLogicTagOnly {
		return errors.New("category is required")
	}

	if bc.AllocatedAmount < 0 {
		return errors.New("allocated_amount cannot be negative")
	}

	if bc.SpentAmount < 0 {
		return errors.New("spent_amount cannot be negative")
	}

	return nil
}

// RemainingAmount returns the allocated amount minus spent amount.
func (bc *BudgetCategory) RemainingAmount() float64 {
	return bc.AllocatedAmount - bc.SpentAmount
}

// IsOverBudget returns true if spent exceeds allocated.
func (bc *BudgetCategory) IsOverBudget() bool {
	return bc.SpentAmount > bc.AllocatedAmount
}

// PercentUsed returns the percentage of allocation used (0-100+).
func (bc *BudgetCategory) PercentUsed() float64 {
	if bc.AllocatedAmount == 0 {
		if bc.SpentAmount > 0 {
			return 100 // Over budget with no allocation
		}
		return 0
	}
	return (bc.SpentAmount / bc.AllocatedAmount) * 100
}

// CreateBudgetRequest represents the request to create a new budget.
type CreateBudgetRequest struct {
	Name         string                        `json:"name" validate:"required,max=100"`
	Description  *string                       `json:"description,omitempty"`
	Period       BudgetPeriod                  `json:"period" validate:"required"`
	StartDate    time.Time                     `json:"start_date" validate:"required"`
	EndDate      *time.Time                    `json:"end_date,omitempty"`
	Amount       float64                       `json:"amount" validate:"required,gt=0"`
	CurrencyCode string                        `json:"currency_code,omitempty"`
	Categories   []CreateBudgetCategoryRequest `json:"categories,omitempty"`

	// Envelope budgeting fields (Issue #491)
	IncomeAmount  float64        `json:"income_amount,omitempty"`
	IncomeSources []IncomeSource `json:"income_sources,omitempty"`
}

// CreateBudgetCategoryRequest represents a category allocation in a budget creation request.
type CreateBudgetCategoryRequest struct {
	Category        []string `json:"category" validate:"required,min=1"`
	AllocatedAmount float64  `json:"allocated_amount" validate:"required,gte=0"`

	// Flexible matching fields (Issue #491)
	Tags       []string   `json:"tags,omitempty"`
	MatchLogic MatchLogic `json:"match_logic,omitempty"` // defaults to category_only
}

// UpdateBudgetRequest represents the request to update a budget.
type UpdateBudgetRequest struct {
	Name        *string       `json:"name,omitempty" validate:"omitempty,max=100"`
	Description *string       `json:"description,omitempty"`
	Period      *BudgetPeriod `json:"period,omitempty"`
	StartDate   *time.Time    `json:"start_date,omitempty"`
	EndDate     *time.Time    `json:"end_date,omitempty"`
	Amount      *float64      `json:"amount,omitempty" validate:"omitempty,gt=0"`
	IsActive    *bool         `json:"is_active,omitempty"`

	// Envelope budgeting fields (Issue #491)
	IncomeAmount  *float64        `json:"income_amount,omitempty" validate:"omitempty,gte=0"`
	IncomeSources *[]IncomeSource `json:"income_sources,omitempty"`
}

// UpdateBudgetCategoryRequest represents the request to update a budget category.
type UpdateBudgetCategoryRequest struct {
	AllocatedAmount *float64 `json:"allocated_amount,omitempty" validate:"omitempty,gte=0"`

	// Flexible matching fields (Issue #491)
	Tags       *[]string   `json:"tags,omitempty"`
	MatchLogic *MatchLogic `json:"match_logic,omitempty"`
}

// CategoryPreviewRequest represents the request to preview matching transactions for a category config.
// Issue #497: Transaction preview feature
type CategoryPreviewRequest struct {
	Categories [][]string `json:"categories" validate:"required,min=1"`
	Tags       []string   `json:"tags,omitempty"`
	MatchLogic MatchLogic `json:"match_logic" validate:"required,oneof=category_only tag_only and or"`
	StartDate  string     `json:"start_date" validate:"required"`
	EndDate    string     `json:"end_date" validate:"required"`
}

// CategoryPreviewResponse represents the response with matching transaction stats.
// Issue #497: Transaction preview feature
type CategoryPreviewResponse struct {
	TransactionCount int     `json:"transaction_count"`
	TotalAmount      float64 `json:"total_amount"`
}

// CategoryTransactionsResponse represents the paginated response with transactions matching a budget category.
// Issue #499: Transaction-Budget Linking UI
type CategoryTransactionsResponse struct {
	Transactions []*Transaction `json:"transactions"`
	TotalCount   int            `json:"total_count"`
	TotalAmount  float64        `json:"total_amount"`
	HasMore      bool           `json:"has_more"`
}

// Budget-related errors.
var (
	ErrBudgetNotFound         = errors.New("budget not found")
	ErrBudgetAlreadyExists    = errors.New("budget with this name already exists")
	ErrBudgetCategoryNotFound = errors.New("budget category not found")
)
