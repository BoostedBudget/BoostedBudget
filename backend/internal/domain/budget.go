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
	Type         IncomeSourceType `json:"type"`
	Value        string           `json:"value"`                   // Category name, tag name, or description
	Description  string           `json:"description,omitempty"`   // Optional user-friendly label
	ValueDisplay string           `json:"value_display,omitempty"` // Human-readable display for Value when Type is pfc_primary
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
	ID          string       `json:"id" db:"id"`
	UserID      string       `json:"user_id" db:"user_id"`
	Name        string       `json:"name" db:"name"`
	Description *string      `json:"description,omitempty" db:"description"`
	Period      BudgetPeriod `json:"period" db:"period"`
	// StartDate/EndDate stay time.Time — NOT serialized — since start_date/end_date are Postgres
	// DATE columns (migration 000004) and both fields are still SQL bind params/scan targets;
	// StartDateDisplay/EndDateDisplay are what actually reach clients (#1631 Batch F2, same
	// sibling-display pattern as domain/activity_item.go and the analytics/dashboard fix in
	// 49f08d5a). EndDateDisplay is a *string, not string: an empty string is a PRESENT key, and
	// here it is worse than that — an empty end_date renders as a literal " - Invalid Date" range
	// suffix at BudgetCard.tsx:102.
	StartDate        time.Time  `json:"-" db:"start_date"`
	StartDateDisplay string     `json:"start_date" format:"date" doc:"UTC calendar day (YYYY-MM-DD) this budget period starts on"`
	EndDate          *time.Time `json:"-" db:"end_date"`
	EndDateDisplay   *string    `json:"end_date,omitempty" format:"date" doc:"UTC calendar day (YYYY-MM-DD) this budget period ends on; omitted for open-ended budgets"`
	Amount           float64    `json:"amount" db:"amount"`
	CurrencyCode     string     `json:"currency_code" db:"currency_code"`
	IsActive         bool       `json:"is_active" db:"is_active"`
	CreatedAt        time.Time  `json:"created_at" db:"created_at"`
	UpdatedAt        time.Time  `json:"updated_at" db:"updated_at"`

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

// PopulateDateDisplay fills StartDateDisplay/EndDateDisplay from StartDate/EndDate (#1631 Batch
// F2). Idempotent, so calling it again after mutating StartDate/EndDate (e.g. in UpdateBudget) is
// the correct way to keep the two in sync — there is no dependency tracking; the display fields
// are only ever as fresh as the last populate call.
func (b *Budget) PopulateDateDisplay() {
	b.StartDateDisplay = formatDateOnly(b.StartDate)
	b.EndDateDisplay = formatNullableDateOnly(b.EndDate)
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

	if b.ExpectedIncome <= 0 {
		return errors.New("expected_income must be positive")
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

// budgetedParentSets returns lookup sets of the PFC primaries and custom category
// IDs that are budgeted as top-level rows, used to detect sub-allocations.
func (b *Budget) budgetedParentSets() (pfcPrimaries, customIDs map[string]bool) {
	pfcPrimaries = make(map[string]bool)
	customIDs = make(map[string]bool)
	for _, cat := range b.Categories {
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
	MatchLogic MatchLogic `json:"match_logic" db:"match_logic"`
}

// IsSubAllocationWithin reports whether this category's hierarchical parent is also
// present among the given budget categories — i.e. this row is a sub-allocation
// "within" a budgeted parent and must be excluded from budget totals (Issue #1203).
func (bc *BudgetCategory) IsSubAllocationWithin(budgetedPFCPrimaries, budgetedCustomIDs map[string]bool) bool {
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
		return errors.New("budget_id is required")
	}

	if bc.MatchLogic != "" && !bc.MatchLogic.IsValid() {
		return errors.New("invalid match_logic value")
	}

	if bc.MatchLogic == MatchLogicTagOnly || bc.MatchLogic == MatchLogicAnd {
		if len(bc.Tags) == 0 {
			return errors.New("tags are required when match_logic is tag_only or and")
		}
	}

	switch bc.CategoryKind {
	case "pfc":
		if bc.PFCPrimary == nil || *bc.PFCPrimary == "" {
			return errors.New("pfc_primary is required when category_kind is pfc")
		}
		if bc.CustomCategoryID != nil {
			return errors.New("custom_category_id must not be set when category_kind is pfc")
		}
		if bc.SavingsGoalID != nil {
			return errors.New("savings_goal_id must not be set when category_kind is pfc")
		}
	case "custom":
		if bc.CustomCategoryID == nil || *bc.CustomCategoryID == "" {
			return errors.New("custom_category_id is required when category_kind is custom")
		}
		if bc.PFCPrimary != nil || bc.PFCDetailed != nil {
			return errors.New("pfc_primary and pfc_detailed must not be set when category_kind is custom")
		}
		if bc.SavingsGoalID != nil {
			return errors.New("savings_goal_id must not be set when category_kind is custom")
		}
	case "savings":
		// Savings categories link to a savings goal; PFC/custom fields must be absent.
		if bc.SavingsGoalID == nil || *bc.SavingsGoalID == "" {
			return errors.New("savings_goal_id is required when category_kind is savings")
		}
		if bc.PFCPrimary != nil || bc.PFCDetailed != nil {
			return errors.New("pfc_primary and pfc_detailed must not be set when category_kind is savings")
		}
		if bc.CustomCategoryID != nil {
			return errors.New("custom_category_id must not be set when category_kind is savings")
		}
	default:
		if bc.MatchLogic != MatchLogicTagOnly {
			return errors.New("category_kind must be pfc, custom, or savings")
		}
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

// BuildCategoryEventData returns a map keyed by budget category ID suitable for
// inclusion as the "categories" key in rules engine EventData.
// Each value is a map with "percent_used", "remaining", "spent", and "allocated" keys.
// The zero-allocation guard in BudgetCategory.PercentUsed ensures no NaN values.
// Issue #1271 Items 5+6: renamed "total_spent" → "spent", added "allocated".
func BuildCategoryEventData(categories []BudgetCategory) map[string]map[string]float64 {
	result := make(map[string]map[string]float64, len(categories))
	for _, bc := range categories {
		result[bc.ID] = map[string]float64{
			"percent_used": bc.PercentUsed(),
			"remaining":    bc.RemainingAmount(),
			"spent":        bc.SpentAmount,
			"allocated":    bc.AllocatedAmount,
		}
	}
	return result
}

// CreateBudgetRequest represents the request to create a new budget.
type CreateBudgetRequest struct {
	Name        string       `json:"name" validate:"required,max=100"`
	Description *string      `json:"description,omitempty"`
	Period      BudgetPeriod `json:"period" validate:"required"`
	// StartDate/EndDate accept EITHER a bare "YYYY-MM-DD" date-only string OR a full RFC3339
	// timestamp (#1631 Batch F2). Today's frontend sends the RFC3339 form
	// (`new Date(dateOnly).toISOString()` in CreateBudgetDialog.tsx), so that form must keep
	// working; FlexibleDate widens acceptance rather than narrowing it. See
	// domain/flexible_date.go for why this can't just stay time.Time.
	// ⚠ Deliberately NO `format:"..."` tag here. huma applies an explicit `format` struct tag as
	// an override on TOP of the type-driven schema, which would defeat the whole point of
	// FlexibleDate: schemaFromType only skips huma's strict format validator (validateFormat) when
	// the type is detected via the encoding.TextUnmarshaler carve-out AND no format is set. Adding
	// `format:"date-time"` here would put the RFC3339-only check right back and 422 every bare
	// date request FlexibleDate exists to accept. See domain/flexible_date.go.
	StartDate    FlexibleDate                  `json:"start_date" validate:"required" doc:"Budget period start. Accepts a bare YYYY-MM-DD date or a full RFC3339 timestamp."`
	EndDate      *FlexibleDate                 `json:"end_date,omitempty" doc:"Budget period end (open-ended if omitted). Accepts a bare YYYY-MM-DD date or a full RFC3339 timestamp."`
	Amount       float64                       `json:"amount" validate:"required,gt=0"`
	CurrencyCode string                        `json:"currency_code,omitempty"`
	Categories   []CreateBudgetCategoryRequest `json:"categories,omitempty"`

	// Envelope budgeting fields (Issue #491)
	IncomeAmount  float64        `json:"income_amount,omitempty"`
	IncomeSources []IncomeSource `json:"income_sources,omitempty"`

	// ExpectedIncome is optional; when nil the service falls back to Amount (Issue #1048).
	ExpectedIncome *float64 `json:"expected_income,omitempty" validate:"omitempty,gt=0"`

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
	MatchLogic MatchLogic `json:"match_logic,omitempty"`
}

// UpdateBudgetRequest represents the request to update a budget.
type UpdateBudgetRequest struct {
	Name        *string       `json:"name,omitempty" validate:"omitempty,max=100"`
	Description *string       `json:"description,omitempty"`
	Period      *BudgetPeriod `json:"period,omitempty"`
	// StartDate/EndDate: same FlexibleDate acceptance (bare date OR RFC3339) as
	// CreateBudgetRequest above, and the same reason for omitting a `format` tag — see there.
	StartDate *FlexibleDate `json:"start_date,omitempty" doc:"Budget period start. Accepts a bare YYYY-MM-DD date or a full RFC3339 timestamp."`
	EndDate   *FlexibleDate `json:"end_date,omitempty" doc:"Budget period end. Accepts a bare YYYY-MM-DD date or a full RFC3339 timestamp."`
	Amount    *float64      `json:"amount,omitempty" validate:"omitempty,gt=0"`
	IsActive  *bool         `json:"is_active,omitempty"`

	// Envelope budgeting fields (Issue #491)
	IncomeAmount  *float64        `json:"income_amount,omitempty" validate:"omitempty,gte=0"`
	IncomeSources *[]IncomeSource `json:"income_sources,omitempty"`

	// ExpectedIncome is optional; when nil the service leaves the existing value unchanged (Issue #1048).
	ExpectedIncome *float64 `json:"expected_income,omitempty" validate:"omitempty,gt=0"`
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
	CategoryKind     string     `json:"category_kind"`
	PFCPrimary       *string    `json:"pfc_primary,omitempty"`
	PFCDetailed      *string    `json:"pfc_detailed,omitempty"`
	CustomCategoryID *string    `json:"custom_category_id,omitempty"`
	Tags             []string   `json:"tags,omitempty"`
	MatchLogic       MatchLogic `json:"match_logic" validate:"required,oneof=category_only tag_only and or"`
	StartDate        string     `json:"start_date" validate:"required"`
	EndDate          string     `json:"end_date" validate:"required"`
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
