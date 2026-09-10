package domain

import (
	"errors"
	"fmt"
	"sort"
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
	IncomeSourceTypePFCPrimary IncomeSourceType = "pfc_primary"
	IncomeSourceTypeTag        IncomeSourceType = "tag"
	IncomeSourceTypeManual     IncomeSourceType = "manual"
)

// validIncomeSourceTypes is a set of all valid income source types.
var validIncomeSourceTypes = map[IncomeSourceType]bool{
	IncomeSourceTypePFCPrimary: true,
	IncomeSourceTypeTag:        true,
	IncomeSourceTypeManual:     true,
}

// IsValid checks if the income source type is valid.
func (ist IncomeSourceType) IsValid() bool {
	return validIncomeSourceTypes[ist]
}

// IncomeSource represents a tracked source of income for envelope budgeting.
type IncomeSource struct {
	Type         IncomeSourceType `json:"type" enum:"pfc_primary,tag,manual" doc:"How this income source is matched. pfc_primary matches transactions by their PFCv2 primary category (e.g. INCOME); tag matches by transaction tag; manual is a fixed, user-entered amount with no automatic matching."`
	Value        string           `json:"value"`                   // Category name, tag name, or description
	Description  string           `json:"description,omitempty"`   // Optional user-friendly label
	ValueDisplay string           `json:"value_display,omitempty"` // Human-readable display for Value when Type is pfc_primary
}

// Validate checks that the IncomeSource has valid fields.
func (is *IncomeSource) Validate() error {
	if !is.Type.IsValid() {
		return fmt.Errorf("%w: invalid income source type %q: must be one of pfc_primary, tag, manual", ErrInvalidBudget, is.Type)
	}
	if is.Value == "" {
		return fmt.Errorf("%w: income source value is required", ErrInvalidBudget)
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

	// ExpectedIncome is the planned income for the budget period (Issue #1048).
	// This replaces IncomeAmount as the basis for allocation calculations.
	// IncomeAmount remains for backward compatibility and tracks actual income received.
	ExpectedIncome float64 `json:"expected_income" db:"expected_income"`

	// SeriesID groups recurring budget periods together (Issue #1300 G5 / migration 000098).
	// When a budget is standalone it is its own series (SeriesID == ID). When a budget is
	// created as a continuation of a prior period, SeriesID is inherited from the ancestor
	// so savings-goal detach tombstones persist across the whole series rather than per-period.
	SeriesID string `json:"series_id" db:"series_id"`

	// Associated data (populated on demand, not stored in DB)
	Categories []BudgetCategory `json:"categories,omitempty" db:"-"`
}

// Validate checks that the Budget has all required fields and valid values.
func (b *Budget) Validate() error {
	if b.UserID == "" {
		return fmt.Errorf("%w: user_id is required", ErrInvalidBudget)
	}

	if b.Name == "" {
		return fmt.Errorf("%w: name is required", ErrInvalidBudget)
	}

	if len(b.Name) > 100 {
		return fmt.Errorf("%w: name must be 100 characters or less", ErrInvalidBudget)
	}

	if !b.Period.IsValid() {
		return fmt.Errorf("%w: invalid budget period %q: must be one of weekly, monthly, quarterly, yearly, custom", ErrInvalidBudget, b.Period)
	}

	if b.StartDate.IsZero() {
		return fmt.Errorf("%w: start_date is required", ErrInvalidBudget)
	}

	if b.Amount <= 0 {
		return fmt.Errorf("%w: amount must be positive", ErrInvalidBudget)
	}

	if b.ExpectedIncome <= 0 {
		return fmt.Errorf("%w: expected_income must be positive", ErrInvalidBudget)
	}

	if b.CurrencyCode == "" {
		return fmt.Errorf("%w: currency_code is required", ErrInvalidBudget)
	}

	// Custom periods require an end_date
	if b.Period == BudgetPeriodCustom && b.EndDate == nil {
		return fmt.Errorf("%w: end_date is required for custom period budgets", ErrInvalidBudget)
	}

	// End date must be after start date if set
	if b.EndDate != nil && !b.EndDate.After(b.StartDate) {
		return fmt.Errorf("%w: end_date must be after start_date", ErrInvalidBudget)
	}

	// Envelope budgeting validation (Issue #491)
	// Income amount can be 0 for traditional budgets, but not negative
	if b.IncomeAmount < 0 {
		return fmt.Errorf("%w: income_amount cannot be negative", ErrInvalidBudget)
	}

	// Validate each income source
	for i, source := range b.IncomeSources {
		if err := source.Validate(); err != nil {
			return fmt.Errorf("income_sources[%d]: %w", i, err)
		}
	}

	return nil
}

// isContainingMatchLogic reports whether a PARENT's match_logic still contains the plain
// category match — i.e. its matched-transaction set is a superset of "category only"
// (Issue #1630). '' is treated as 'category_only' (the domain/column default).
func isContainingMatchLogic(ml MatchLogic) bool {
	return ml == "" || ml == MatchLogicCategoryOnly || ml == MatchLogicOr
}

// isContainedMatchLogic reports whether a CHILD's match_logic keeps it inside the plain
// category match — i.e. it cannot escape the category clause via tags (Issue #1630). ''
// is treated as 'category_only' (the domain/column default).
func isContainedMatchLogic(ml MatchLogic) bool {
	return ml == "" || ml == MatchLogicCategoryOnly || ml == MatchLogicAnd
}

// budgetedParentSets returns lookup sets of the PFC primaries and custom category
// IDs that are budgeted as top-level rows AND eligible to adopt children — i.e. the
// row's own match_logic still contains the plain category match (Issue #1630). A
// 'tag_only' or 'and' parent has no envelope a plain-category child could fall inside,
// so it is deliberately excluded from these sets.
func (b *Budget) budgetedParentSets() (pfcPrimaries, customIDs map[string]bool) {
	return budgetedParentSetsFor(b.Categories)
}

// budgetedParentSetsFor is the slice-based core of budgetedParentSets, extracted so
// callers holding only a []BudgetCategory (not a full *Budget) — e.g.
// BuildCategoryEventData — can compute the same sets without re-deriving the rule
// (Issue #1630 Task 3.6).
func budgetedParentSetsFor(categories []BudgetCategory) (pfcPrimaries, customIDs map[string]bool) {
	pfcPrimaries = make(map[string]bool)
	customIDs = make(map[string]bool)
	for _, cat := range categories {
		if !isContainingMatchLogic(cat.MatchLogic) {
			continue
		}
		if cat.CategoryKind == "pfc" && cat.PFCPrimary != nil && cat.PFCDetailed == nil {
			pfcPrimaries[*cat.PFCPrimary] = true
		}
		if cat.CategoryKind == "custom" && cat.CustomCategoryID != nil {
			customIDs[*cat.CustomCategoryID] = true
		}
	}
	return pfcPrimaries, customIDs
}

// TotalAllocated returns the sum of TOP-LEVEL category allocations. Sub-allocations
// (a child whose parent is also budgeted) are excluded because their amount is
// already accounted for within the parent's allocation (Issue #1203).
func (b *Budget) TotalAllocated() float64 {
	pfcPrimaries, customIDs := b.budgetedParentSets()
	var total float64
	for i := range b.Categories {
		if b.Categories[i].IsSubAllocationWithin(pfcPrimaries, customIDs) {
			continue
		}
		total += b.Categories[i].AllocatedAmount
	}
	return total
}

// TotalSpent returns the sum of TOP-LEVEL category spent amounts. A parent's
// SpentAmount already rolls up its children's matched spend, so summing only
// top-level rows avoids double-counting (Issue #1203). This also fixes the
// pre-existing PFC primary + detailed double-count.
func (b *Budget) TotalSpent() float64 {
	pfcPrimaries, customIDs := b.budgetedParentSets()
	var total float64
	for i := range b.Categories {
		if b.Categories[i].IsSubAllocationWithin(pfcPrimaries, customIDs) {
			continue
		}
		total += b.Categories[i].SpentAmount
	}
	return total
}

// ChildrenAllocatedByParent maps each budgeted parent category's ID to the sum of
// its budgeted children's allocations (sub-allocations), Issue #1203.
func (b *Budget) ChildrenAllocatedByParent() map[string]float64 {
	pfcPrimaryToID := make(map[string]string)
	customIDToCatID := make(map[string]string)
	for i := range b.Categories {
		c := &b.Categories[i]
		if c.CategoryKind == "pfc" && c.PFCPrimary != nil && c.PFCDetailed == nil {
			pfcPrimaryToID[*c.PFCPrimary] = c.ID
		}
		if c.CategoryKind == "custom" && c.CustomCategoryID != nil {
			customIDToCatID[*c.CustomCategoryID] = c.ID
		}
	}

	sums := make(map[string]float64)
	for i := range b.Categories {
		c := &b.Categories[i]
		var parentID string
		switch c.CategoryKind {
		case "pfc":
			if c.PFCDetailed != nil && c.PFCPrimary != nil {
				parentID = pfcPrimaryToID[*c.PFCPrimary]
			}
		case "custom":
			if c.CustomParentID != nil {
				parentID = customIDToCatID[*c.CustomParentID]
			} else if c.CustomParentPFCPrimary != nil {
				parentID = pfcPrimaryToID[*c.CustomParentPFCPrimary]
			}
		}
		if parentID != "" {
			sums[parentID] += c.AllocatedAmount
		}
	}
	return sums
}

// OverEarmarkedParentIDs returns the IDs of budgeted parent categories whose child
// sub-allocations exceed the parent's own allocation. Informational only — the app
// permits over-earmarking (mirrors ExceedsExpectedIncome), Issue #1203.
func (b *Budget) OverEarmarkedParentIDs() []string {
	childSums := b.ChildrenAllocatedByParent()
	byID := make(map[string]float64, len(b.Categories))
	for i := range b.Categories {
		byID[b.Categories[i].ID] = b.Categories[i].AllocatedAmount
	}
	var over []string
	for parentID, childSum := range childSums {
		if allocated, ok := byID[parentID]; ok && childSum > allocated {
			over = append(over, parentID)
		}
	}
	sort.Strings(over)
	return over
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

// UnallocatedAmount returns the expected income minus total allocated to categories.
// This is used for envelope budgeting to show how much income remains to be allocated.
// Uses ExpectedIncome as the basis (Issue #1048; previously used IncomeAmount).
func (b *Budget) UnallocatedAmount() float64 {
	return b.ExpectedIncome - b.TotalAllocated()
}

// IsFullyAllocated returns true if all expected income has been allocated to categories.
// Returns true if ExpectedIncome is 0 (no income to allocate) or if allocations >= expected income.
// Uses ExpectedIncome as the basis (Issue #1048; previously used IncomeAmount).
func (b *Budget) IsFullyAllocated() bool {
	return b.TotalAllocated() >= b.ExpectedIncome
}

// ExceedsExpectedIncome returns true if the total allocated to categories exceeds
// the expected income for the budget period. Issue #1048.
func (b *Budget) ExceedsExpectedIncome() bool {
	return b.TotalAllocated() > b.ExpectedIncome
}

// BudgetCategory represents a category allocation within a budget.
// Uses a discriminated union: category_kind='pfc' (pfc_primary required),
// category_kind='custom' (custom_category_id required), or
// category_kind='savings' (savings_goal_id required — Issue #1300).
type BudgetCategory struct {
	ID               string  `json:"id" db:"id"`
	BudgetID         string  `json:"budget_id" db:"budget_id"`
	CategoryKind     string  `json:"category_kind" db:"category_kind"` // "pfc", "custom", or "savings"
	PFCPrimary       *string `json:"pfc_primary,omitempty" db:"pfc_primary"`
	PFCDetailed      *string `json:"pfc_detailed,omitempty" db:"pfc_detailed"`
	CustomCategoryID *string `json:"custom_category_id,omitempty" db:"custom_category_id"`
	// SavingsGoalID links this category to a SavingsGoal (required when kind=savings — Issue #1300).
	SavingsGoalID *string `json:"savings_goal_id,omitempty" db:"savings_goal_id"`
	// AutoCreated marks categories automatically created when a savings goal was created.
	AutoCreated bool `json:"auto_created" db:"auto_created"`
	// ManagedByGoalID is the goal ID that auto-created this category (populated when AutoCreated=true).
	ManagedByGoalID *string   `json:"managed_by_goal_id,omitempty" db:"managed_by_goal_id"`
	AllocatedAmount float64   `json:"allocated_amount" db:"allocated_amount"`
	SpentAmount     float64   `json:"spent_amount" db:"spent_amount"`
	CreatedAt       time.Time `json:"created_at" db:"created_at"`
	UpdatedAt       time.Time `json:"updated_at" db:"updated_at"`

	// PFC display names (populated by PopulatePFCDisplay, not stored in DB)
	PFCPrimaryDisplay  *string `json:"pfc_primary_display,omitempty" db:"-"`
	PFCDetailedDisplay *string `json:"pfc_detailed_display,omitempty" db:"-"`

	// Parent linkage of the referenced custom category (Issue #1203), populated
	// from a join when the budget is loaded. Used to detect sub-allocations
	// (a child whose parent is also budgeted) so totals aren't double-counted.
	CustomParentID         *string `json:"custom_parent_id,omitempty" db:"-"`
	CustomParentPFCPrimary *string `json:"custom_parent_pfc_primary,omitempty" db:"-"`

	// SavingsGoalName is the display name of the linked savings goal, populated
	// via a JOIN when categories are loaded (Issue #1300). Not stored in the
	// budget_categories table — sourced from savings_goals.name on read.
	SavingsGoalName *string `json:"savings_goal_name,omitempty" db:"-"`

	// Flexible matching fields (Issue #491)
	Tags       []string   `json:"tags" db:"-"`
	MatchLogic MatchLogic `json:"match_logic" db:"match_logic" enum:"category_only,tag_only,and,or"`
}

// IsSubAllocationWithin reports whether this category's hierarchical parent is also
// present among the given budget categories — i.e. this row is a sub-allocation
// "within" a budgeted parent and must be excluded from budget totals (Issue #1203).
//
// Hierarchy alone is not sufficient (Issue #1630): exclusion is sound only when this
// CHILD's own matched-transaction set is contained within the PARENT's. budgetedPFCPrimaries
// and budgetedCustomIDs already carry the parent-side containment test (only a parent whose
// own match_logic is 'category_only'/'or' is present in them — see budgetedParentSets), so
// this method only needs to additionally gate on the CHILD's own match_logic: a 'tag_only' or
// 'or' child can match transactions outside the parent's category clause entirely, so it must
// never be treated as a sub-allocation even when it is hierarchically nested.
func (bc *BudgetCategory) IsSubAllocationWithin(budgetedPFCPrimaries, budgetedCustomIDs map[string]bool) bool {
	if !isContainedMatchLogic(bc.MatchLogic) {
		return false
	}
	switch bc.CategoryKind {
	case "pfc":
		// A PFC detailed is a child of its primary when that primary is also budgeted.
		if bc.PFCDetailed != nil && bc.PFCPrimary != nil && budgetedPFCPrimaries[*bc.PFCPrimary] {
			return true
		}
	case "custom":
		if bc.CustomParentID != nil && budgetedCustomIDs[*bc.CustomParentID] {
			return true
		}
		if bc.CustomParentPFCPrimary != nil && budgetedPFCPrimaries[*bc.CustomParentPFCPrimary] {
			return true
		}
	}
	return false
}

// Validate checks that the BudgetCategory has all required fields.
func (bc *BudgetCategory) Validate() error {
	if bc.BudgetID == "" {
		return fmt.Errorf("%w: budget_id is required", ErrInvalidBudget)
	}

	if bc.MatchLogic != "" && !bc.MatchLogic.IsValid() {
		return fmt.Errorf("%w: invalid match_logic value %q: must be one of category_only, tag_only, and, or", ErrInvalidBudget, bc.MatchLogic)
	}

	if bc.MatchLogic == MatchLogicTagOnly || bc.MatchLogic == MatchLogicAnd {
		if len(bc.Tags) == 0 {
			return fmt.Errorf("%w: tags are required when match_logic is tag_only or and", ErrInvalidBudget)
		}
	}

	switch bc.CategoryKind {
	case "pfc":
		if bc.PFCPrimary == nil || *bc.PFCPrimary == "" {
			return fmt.Errorf("%w: pfc_primary is required when category_kind is pfc", ErrInvalidBudget)
		}
		if bc.CustomCategoryID != nil {
			return fmt.Errorf("%w: custom_category_id must not be set when category_kind is pfc", ErrInvalidBudget)
		}
		if bc.SavingsGoalID != nil {
			return fmt.Errorf("%w: savings_goal_id must not be set when category_kind is pfc", ErrInvalidBudget)
		}
	case "custom":
		if bc.CustomCategoryID == nil || *bc.CustomCategoryID == "" {
			return fmt.Errorf("%w: custom_category_id is required when category_kind is custom", ErrInvalidBudget)
		}
		if bc.PFCPrimary != nil || bc.PFCDetailed != nil {
			return fmt.Errorf("%w: pfc_primary and pfc_detailed must not be set when category_kind is custom", ErrInvalidBudget)
		}
		if bc.SavingsGoalID != nil {
			return fmt.Errorf("%w: savings_goal_id must not be set when category_kind is custom", ErrInvalidBudget)
		}
	case "savings":
		// Savings categories link to a savings goal; PFC/custom fields must be absent.
		if bc.SavingsGoalID == nil || *bc.SavingsGoalID == "" {
			return fmt.Errorf("%w: savings_goal_id is required when category_kind is savings", ErrInvalidBudget)
		}
		if bc.PFCPrimary != nil || bc.PFCDetailed != nil {
			return fmt.Errorf("%w: pfc_primary and pfc_detailed must not be set when category_kind is savings", ErrInvalidBudget)
		}
		if bc.CustomCategoryID != nil {
			return fmt.Errorf("%w: custom_category_id must not be set when category_kind is savings", ErrInvalidBudget)
		}
	default:
		if bc.MatchLogic != MatchLogicTagOnly {
			return fmt.Errorf("%w: category_kind must be pfc, custom, or savings", ErrInvalidBudget)
		}
	}

	if bc.AllocatedAmount < 0 {
		return fmt.Errorf("%w: allocated_amount cannot be negative", ErrInvalidBudget)
	}

	if bc.SpentAmount < 0 {
		return fmt.Errorf("%w: spent_amount cannot be negative", ErrInvalidBudget)
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

// BuildCategoryEventData returns a map keyed by budget category ID suitable for
// inclusion as the "categories" key in rules engine EventData.
// Each value is a map with "percent_used", "remaining", "spent", "allocated", and
// "is_sub_allocation" keys. The zero-allocation guard in BudgetCategory.PercentUsed
// ensures no NaN values.
// Issue #1271 Items 5+6: renamed "total_spent" → "spent", added "allocated".
//
// Issue #1630 Task 3.6: every row is still emitted — a rules-engine condition may
// legitimately reference a sub-allocation child by category ID, so rows are never
// dropped here. Instead each row is MARKED via "is_sub_allocation" so a consumer that
// sums or rolls per-category dollars (unlike a single-row condition read) can exclude
// sub-allocations and avoid double-counting dollars already reflected in the parent.
func BuildCategoryEventData(categories []BudgetCategory) map[string]map[string]float64 {
	pfcPrimaries, customIDs := budgetedParentSetsFor(categories)
	result := make(map[string]map[string]float64, len(categories))
	for _, bc := range categories {
		isSubAllocation := 0.0
		if bc.IsSubAllocationWithin(pfcPrimaries, customIDs) {
			isSubAllocation = 1.0
		}
		result[bc.ID] = map[string]float64{
			"percent_used":                      bc.PercentUsed(),
			"remaining":                         bc.RemainingAmount(),
			"spent":                             bc.SpentAmount,
			"allocated":                         bc.AllocatedAmount,
			EventDataCategoryKeyIsSubAllocation: isSubAllocation,
		}
	}
	return result
}

// CreateBudgetRequest represents the request to create a new budget.
type CreateBudgetRequest struct {
	Name        string        `json:"name" validate:"required,max=100"`
	Description *string       `json:"description,omitempty"`
	Period      BudgetPeriod  `json:"period" validate:"required"`
	StartDate   FlexibleDate  `json:"start_date" validate:"required" doc:"Budget start date. Accepts YYYY-MM-DD (matching every read endpoint's date format) or a full RFC3339 timestamp."`
	EndDate     *FlexibleDate `json:"end_date,omitempty" doc:"Required only when period is 'custom'. When set, must be strictly after start_date. Optional for every other period (weekly/monthly/quarterly/yearly) — omitting it there is valid, not an error. Accepts YYYY-MM-DD or a full RFC3339 timestamp."`
	// Amount is overwritten to mirror expected_income when expected_income is set and > 0
	// (Issue #1048) — declined the alternative of making this read-only, since amount is
	// still the authoritative field when expected_income is absent.
	Amount       float64                       `json:"amount" validate:"required,gt=0" doc:"Budget amount. If expected_income is also set and > 0, expected_income wins and this field is overwritten to mirror it — set expected_income instead if you want it to be authoritative."`
	CurrencyCode string                        `json:"currency_code,omitempty"`
	Categories   []CreateBudgetCategoryRequest `json:"categories,omitempty"`

	// Envelope budgeting fields (Issue #491)
	IncomeAmount  float64        `json:"income_amount,omitempty"`
	IncomeSources []IncomeSource `json:"income_sources,omitempty"`

	// ExpectedIncome is optional; when nil the service falls back to Amount (Issue #1048).
	ExpectedIncome *float64 `json:"expected_income,omitempty" validate:"omitempty,gt=0" doc:"Planned income for the period. When set and > 0, this wins over amount — amount is overwritten to mirror this value. Leave unset to have amount remain authoritative."`

	// FromBudgetID, when set, causes the new budget to inherit the ancestor's SeriesID
	// so savings-goal detach tombstones (keyed by series_id) persist across budget periods
	// (Issue #1300 M22 / RG3). Must be a budget owned by the same user.
	FromBudgetID *string `json:"from_budget_id,omitempty" validate:"omitempty,uuid"`
}

// CreateBudgetCategoryRequest represents a category allocation in a budget creation request.
type CreateBudgetCategoryRequest struct {
	CategoryKind     string  `json:"category_kind" validate:"required,oneof=pfc custom savings"`
	PFCPrimary       *string `json:"pfc_primary,omitempty"`
	PFCDetailed      *string `json:"pfc_detailed,omitempty"`
	CustomCategoryID *string `json:"custom_category_id,omitempty" validate:"omitempty,uuid"`
	// SavingsGoalID is required when CategoryKind = "savings" (Issue #1300).
	SavingsGoalID   *string `json:"savings_goal_id,omitempty" validate:"omitempty,uuid"`
	AllocatedAmount float64 `json:"allocated_amount" validate:"required,gte=0"`

	// Flexible matching fields (Issue #491)
	Tags       []string   `json:"tags,omitempty"`
	MatchLogic MatchLogic `json:"match_logic,omitempty" enum:"category_only,tag_only,and,or" doc:"How tags and category matching combine. tag_only and and both REQUIRE a non-empty tags array — omitting tags with either of these is rejected (400)."`
}

// UpdateBudgetRequest represents the request to update a budget.
type UpdateBudgetRequest struct {
	Name        *string       `json:"name,omitempty" validate:"omitempty,max=100"`
	Description *string       `json:"description,omitempty"`
	Period      *BudgetPeriod `json:"period,omitempty"`
	StartDate   *time.Time    `json:"start_date,omitempty"`
	EndDate     *time.Time    `json:"end_date,omitempty" doc:"Required only when period is 'custom'. When set, must be strictly after start_date. Optional for every other period (weekly/monthly/quarterly/yearly) — omitting it there is valid, not an error."`
	// Amount is overwritten to mirror expected_income when expected_income is set and > 0
	// (Issue #1048) — declined the alternative of making this read-only, since amount is
	// still the authoritative field when expected_income is absent.
	Amount   *float64 `json:"amount,omitempty" validate:"omitempty,gt=0" doc:"Budget amount. If expected_income is also set and > 0 in this same request, expected_income wins and this field is overwritten to mirror it — set expected_income instead if you want it to be authoritative."`
	IsActive *bool    `json:"is_active,omitempty"`

	// Envelope budgeting fields (Issue #491)
	IncomeAmount  *float64        `json:"income_amount,omitempty" validate:"omitempty,gte=0"`
	IncomeSources *[]IncomeSource `json:"income_sources,omitempty"`

	// ExpectedIncome is optional; when nil the service leaves the existing value unchanged (Issue #1048).
	ExpectedIncome *float64 `json:"expected_income,omitempty" validate:"omitempty,gt=0" doc:"Planned income for the period. When set and > 0, this wins over amount in this same request — amount is overwritten to mirror this value. Leave unset to have amount remain authoritative."`
}

// UpdateBudgetCategoryRequest represents the request to update a budget category.
type UpdateBudgetCategoryRequest struct {
	AllocatedAmount *float64 `json:"allocated_amount,omitempty" validate:"omitempty,gte=0"`

	// Flexible matching fields (Issue #491)
	Tags       *[]string   `json:"tags,omitempty"`
	MatchLogic *MatchLogic `json:"match_logic,omitempty" enum:"category_only,tag_only,and,or" doc:"How tags and category matching combine. tag_only and and both REQUIRE a non-empty tags array — omitting tags with either of these is rejected (400)."`
}

// CategoryPreviewRequest represents the request to preview matching transactions for a category config.
// Issue #497: Transaction preview feature
type CategoryPreviewRequest struct {
	CategoryKind     string     `json:"category_kind"`
	PFCPrimary       *string    `json:"pfc_primary,omitempty"`
	PFCDetailed      *string    `json:"pfc_detailed,omitempty"`
	CustomCategoryID *string    `json:"custom_category_id,omitempty"`
	Tags             []string   `json:"tags,omitempty"`
	MatchLogic       MatchLogic `json:"match_logic" validate:"required,oneof=category_only tag_only and or" enum:"category_only,tag_only,and,or" doc:"How tags and category matching combine. tag_only and and both REQUIRE a non-empty tags array."`
	StartDate        string     `json:"start_date" validate:"required"`
	EndDate          string     `json:"end_date" validate:"required"`
}

// CategoryPreviewResponse represents the response with matching transaction stats.
// Issue #497: Transaction preview feature
//
// Issue #1630 Task 5: a `pfc_primary` (or parent `custom`) category rolls up its nested
// children (Issue #1203) — a correct total that nonetheless surprised a reporter who had
// no way to see it. DirectTransactionCount/DirectAmount and Children disclose the split:
// Direct* covers transactions filed straight under the targeted category itself, and each
// Children entry is one nested custom category (a PFC-primary's nested custom categories,
// or a parent custom category's custom children) with its own share of the total. Both are
// populated ONLY when the category kind can roll up AND at least one child actually
// matched — a leaf category (pfc_detailed set, or a custom category with no children)
// leaves them zero-valued/nil because there is nothing to disclose. TransactionCount and
// TotalAmount remain the single rolled-up figure every existing caller already relies on;
// this is additive.
type CategoryPreviewResponse struct {
	TransactionCount int     `json:"transaction_count"`
	TotalAmount      float64 `json:"total_amount"`

	// DirectTransactionCount/DirectAmount cover only transactions filed directly under the
	// targeted category (not a nested child). Populated only when Children is non-empty.
	DirectTransactionCount int     `json:"direct_transaction_count,omitempty"`
	DirectAmount           float64 `json:"direct_amount,omitempty"`

	// Children breaks the rolled-up total down by nested custom category. Empty/nil when
	// the category kind cannot roll up, or no nested child actually matched.
	Children []CategoryPreviewBreakdownItem `json:"children,omitempty"`
}

// CategoryPreviewBreakdownItem is one nested child's contribution to a rolled-up
// CategoryPreviewResponse total. Issue #1630 Task 5.
type CategoryPreviewBreakdownItem struct {
	CategoryID       string  `json:"category_id"`
	CategoryName     string  `json:"category_name"`
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
	// ErrInvalidBudget is the field-validation sentinel for Budget / BudgetCategory /
	// IncomeSource (Issue #1630, task 7 — the same headline defect #1625 fixed for savings
	// goals, in a third domain). Budget.Validate(), BudgetCategory.Validate() and
	// IncomeSource.Validate() previously returned bare errors.New with no sentinel, so every
	// field failure (an invalid income_sources[].type, an invalid match_logic, a negative
	// amount, an invalid category_kind) fell through internal/huma/errors/mapper.go's switch
	// to the default case and became a 500 + Sentry event instead of a 4xx naming the field.
	// Mirrors ErrInvalidSavingsGoal's shape exactly; see the mapper case comment for the
	// ordering rationale.
	ErrInvalidBudget = errors.New("invalid budget")
	// ErrDuplicateParentCategory: Issue #1630 Task 4. budgetedParentSets (and the frontend's
	// buildBudgetedParentSets) key a PARENT category by its pfc_primary/custom_category_id VALUE,
	// not its row ID, and nothing previously stopped two live rows sharing that value from
	// coexisting — the maps would silently keep whichever row a range/map iteration visited last,
	// leaving which row "owns" a given child's rendering undefined. Enforced at write time instead
	// (AddCategory, CreateBudget's batch path): the second row sharing a parent identity is
	// rejected rather than allowed to collide. No backfill needed — Issue #1630 was authored
	// pre-launch with zero production budgets, so no duplicate rows exist to reconcile.
	ErrDuplicateParentCategory = errors.New("a budget category with this parent identity already exists")
	// ErrEscapedNestedChild: adversarial review of Issue #1630's own fix found a HIGH-severity
	// double-count it introduced. buildCategoryClause (transaction_repository.go) rolls up a
	// parent's SpentAmount hierarchically — every transaction filed under a nested custom
	// category or PFC detailed, regardless of that child's own match_logic. The Go/SQL/TS
	// containment rule (isContainingMatchLogic(parent) && isContainedMatchLogic(child)) only
	// decides whether the CHILD is excluded from top-level totals; it does nothing to the
	// PARENT's roll-up query, which has no sibling/match_logic context at all. So a nested child
	// whose own match_logic escapes containment (e.g. a custom category nested under a budgeted
	// 'category_only'/'or' parent, but itself 'or' or 'tag_only') is counted BOTH as its own
	// top-level row AND inside the parent's roll-up — every dollar doubled. Wraps ErrInvalidBudget
	// so the mapper's existing 400 case picks it up with no separate switch entry.
	//
	// Fixed by refusing to PERSIST the escaping shape at all (EscapedNestedChild, wired into
	// CreateBudget's batch loop and AddCategory — the same two call sites as
	// DuplicateParentIdentity). This is write-time only and does NOT retroactively fix the
	// parent's roll-up query; any row that predates this guard (there are none — Issue #1630 was
	// authored pre-launch with zero production budgets) would keep the pre-existing double-count
	// forever, exactly as ErrDuplicateParentCategory's rows are grandfathered.
	ErrEscapedNestedChild = fmt.Errorf("%w: nested budget category's match_logic escapes its parent's containment envelope", ErrInvalidBudget)
)

// DuplicateParentIdentity reports whether `candidate` would collide with an existing LIVE
// category's PARENT IDENTITY if added to `existing` (Issue #1630 Task 4).
//
// Parent identity is:
//   - PFC: the pfc_primary VALUE, when the row is primary-only (PFCDetailed == nil) — that shape
//     is exactly what budgetedParentSetsFor treats as a candidate parent.
//   - Custom: the custom_category_id VALUE — a custom category has no primary/detailed split, so
//     the referenced ID *is* its identity; two rows referencing the same custom_category_id in one
//     budget are ambiguous the same way two same-primary PFC parent rows are.
//
// A detailed PFC row (PFCDetailed != nil) is a CHILD shape and is deliberately never compared
// here — two children of the same primary are a normal, legal multi-child budget, not a
// duplicate parent.
func DuplicateParentIdentity(existing []BudgetCategory, candidate *BudgetCategory) bool {
	if candidate == nil {
		return false
	}
	switch candidate.CategoryKind {
	case "pfc":
		if candidate.PFCDetailed != nil || candidate.PFCPrimary == nil {
			return false
		}
		for i := range existing {
			e := &existing[i]
			if e.CategoryKind == "pfc" && e.PFCDetailed == nil &&
				e.PFCPrimary != nil && *e.PFCPrimary == *candidate.PFCPrimary {
				return true
			}
		}
	case "custom":
		if candidate.CustomCategoryID == nil {
			return false
		}
		for i := range existing {
			e := &existing[i]
			if e.CategoryKind == "custom" && e.CustomCategoryID != nil &&
				*e.CustomCategoryID == *candidate.CustomCategoryID {
				return true
			}
		}
	}
	return false
}

// hierarchyHasNestedChild reports whether `child` is hierarchically nested under `parent`'s
// identity — the same hierarchy test IsSubAllocationWithin and DuplicateParentIdentity use,
// factored out so EscapedNestedChild can run it in both directions. It looks only at identity
// (pfc_primary / custom_category_id / custom parent linkage), never at match_logic.
//
// The custom case relies on child.CustomParentID / child.CustomParentPFCPrimary — the parent
// linkage of the CHILD's own referenced custom_categories row. At read time these are populated
// by GetCategoriesByBudgetID's join; at write time (before either row is persisted) a caller
// constructing a *BudgetCategory in memory must populate them itself via a custom-category
// lookup (mirrors budget_service.go's existing IDOR-check calls to customCategoryRepo), or this
// always reports "not nested" for that candidate's custom-parent-of-existing direction.
func hierarchyHasNestedChild(parent, child *BudgetCategory) bool {
	switch child.CategoryKind {
	case "pfc":
		return child.PFCDetailed != nil && child.PFCPrimary != nil &&
			parent.CategoryKind == "pfc" && parent.PFCDetailed == nil && parent.PFCPrimary != nil &&
			*parent.PFCPrimary == *child.PFCPrimary
	case "custom":
		if child.CustomParentID != nil && parent.CategoryKind == "custom" && parent.CustomCategoryID != nil &&
			*parent.CustomCategoryID == *child.CustomParentID {
			return true
		}
		if child.CustomParentPFCPrimary != nil && parent.CategoryKind == "pfc" && parent.PFCDetailed == nil &&
			parent.PFCPrimary != nil && *parent.PFCPrimary == *child.CustomParentPFCPrimary {
			return true
		}
	}
	return false
}

// EscapedNestedChild reports whether `candidate` would form a hierarchically-nested pair with
// some row in `existing` whose match_logic combination escapes containment (Issue #1630 HIGH
// review finding — see ErrEscapedNestedChild). A pair escapes containment when the would-be
// PARENT's match_logic still contains the plain category match (isContainingMatchLogic) but the
// would-be CHILD's does not stay inside it (isContainedMatchLogic) — the exact condition under
// which buildCategoryClause's hierarchical roll-up still counts the child's transactions inside
// the parent's own SpentAmount, while the child ALSO surfaces as its own top-level row (because
// IsSubAllocationWithin/isContainedMatchLogic excludes it from sub-allocation treatment).
//
// Checked in BOTH directions so call-site ordering within one batch never matters:
//   - candidate as the CHILD of an existing (already-persisted-or-already-built) containing
//     parent — the common case: adding a nested category under a parent that exists already.
//   - candidate as the containing PARENT of an existing escaping child — covers a batch where
//     the child request happens to be processed before its parent (CreateBudget's request order
//     is caller-controlled, so either order is legal input).
//
// Only a hierarchically-nested pair is considered at all; two unrelated top-level rows never
// trip this regardless of their match_logic values.
func EscapedNestedChild(existing []BudgetCategory, candidate *BudgetCategory) bool {
	if candidate == nil {
		return false
	}
	for i := range existing {
		other := &existing[i]
		// candidate as child, other as parent.
		if !isContainedMatchLogic(candidate.MatchLogic) && isContainingMatchLogic(other.MatchLogic) &&
			hierarchyHasNestedChild(other, candidate) {
			return true
		}
		// candidate as parent, other as child.
		if isContainingMatchLogic(candidate.MatchLogic) && !isContainedMatchLogic(other.MatchLogic) &&
			hierarchyHasNestedChild(candidate, other) {
			return true
		}
	}
	return false
}
