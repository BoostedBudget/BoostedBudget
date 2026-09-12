package domain

import (
	"errors"
	"fmt"
	"sort"
	"strings"
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

// hasPFCDetailed reports whether pfcDetailed carries a real detailed-category value —
// treating a present-but-empty string ("") identically to nil/absent (Issue #1630).
// BudgetCategory.Validate now rejects pfc_detailed: "" at write time going forward, but every
// predicate that decides "is this row a primary-only parent or a detailed child" reads this
// helper instead of testing the pointer directly, so a row that reaches these predicates any
// other way (a pre-existing row, a value built in memory before validation) cannot make the
// Go/SQL and TypeScript totals disagree about the same data. TS's `!cat.pfc_detailed` already
// treats "" as absent for the same reason; this makes the Go/SQL side agree with it.
func hasPFCDetailed(pfcDetailed *string) bool {
	return pfcDetailed != nil && *pfcDetailed != ""
}

// isContainingMatchLogic reports whether a PARENT's match_logic still contains the plain
// category match — i.e. its matched-transaction set is a superset of "category only"
// (Issue #1630). An empty value is treated as 'category_only' (the domain/column default).
func isContainingMatchLogic(ml MatchLogic) bool {
	return ml == "" || ml == MatchLogicCategoryOnly || ml == MatchLogicOr
}

// isContainedMatchLogic reports whether a CHILD's match_logic keeps it inside the plain
// category match — i.e. it cannot escape the category clause via tags (Issue #1630). An empty value
// is treated as 'category_only' (the domain/column default).
func isContainedMatchLogic(ml MatchLogic) bool {
	return ml == "" || ml == MatchLogicCategoryOnly || ml == MatchLogicAnd
}

// budgetedParentSets returns lookup sets of the PFC primaries and custom category
// IDs that are budgeted as top-level rows AND eligible to adopt children — i.e. the
// row's own match_logic still contains the plain category match (Issue #1630).
//
// A 'tag_only' or 'and' parent IS still excluded from these sets, but not because it "has no
// envelope" — that framing is backwards and was corrected after an adversarial-review finding
// (Issue #1630 fixwave 4). Summing top-level rows is only safe when those rows are pairwise
// DISJOINT; containment (parent's matched set ⊇ child's) is one way to get disjointness (the
// child's dollars live entirely inside the parent's roll-up, so excluding the child is correct),
// but it is not the only overlap that matters. An 'and' or 'tag_only' parent's matched-transaction
// set is NOT a superset of "category only" — but buildCategoryClause (transaction_repository.go)
// rolls up a nested child's category HIERARCHICALLY regardless of the parent's own match_logic,
// so the parent's SpentAmount can still OVERLAP a nested child's SpentAmount even though the
// parent doesn't contain it. Excluding only "containing" parents from these sets is what makes
// EscapedNestedChild's write-time refusal (below) necessary for every other combination: this
// function narrows which pairs are silently treated as safe sub-allocations, EscapedNestedChild
// widens which pairs are refused at write time so the two invariants agree.
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
		if cat.CategoryKind == "pfc" && cat.PFCPrimary != nil && !hasPFCDetailed(cat.PFCDetailed) {
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
		if c.CategoryKind == "pfc" && c.PFCPrimary != nil && !hasPFCDetailed(c.PFCDetailed) {
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
			if hasPFCDetailed(c.PFCDetailed) && c.PFCPrimary != nil {
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
	GroupName        *string             `json:"group_name,omitempty" db:"group_name"`
	Members          []BudgetGroupMember `json:"members,omitempty" db:"-"`
	ID               string              `json:"id" db:"id"`
	BudgetID         string              `json:"budget_id" db:"budget_id"`
	CategoryKind     string              `json:"category_kind" db:"category_kind"` // "pfc", "custom", or "savings"
	PFCPrimary       *string             `json:"pfc_primary,omitempty" db:"pfc_primary"`
	PFCDetailed      *string             `json:"pfc_detailed,omitempty" db:"pfc_detailed"`
	CustomCategoryID *string             `json:"custom_category_id,omitempty" db:"custom_category_id"`
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

	// CustomGrandparentPFCPrimary is the PFC primary this category's PARENT (CustomParentID)
	// is itself nested under via parent_pfc_primary — i.e. this row is a GRANDCHILD of a PFC
	// root two hops down: pfc root -> custom parent (CustomParentPFCPrimary on the PARENT,
	// not on this row) -> this custom category (CustomParentID). Populated from a second join
	// hop alongside CustomParentID/CustomParentPFCPrimary (Issue #1630 Task B2), only ever set
	// when CustomParentID is also set (a row nested via CustomParentPFCPrimary directly has no
	// further custom grandparent to report — see analytics_category_path.go's ragged 3-level
	// tree). Needed because buildCategoryClause's widened "pfc" branch now rolls this row's
	// transactions into that root's total two hops away, so IsSubAllocationWithin and
	// EscapedNestedChild must be able to recognize the same transitive pair or they double
	// count: this row is uncounted as its own top-level total AND rolled into the root's.
	CustomGrandparentPFCPrimary *string `json:"custom_grandparent_pfc_primary,omitempty" db:"-"`

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
//
// ⚠ UPDATE (Issue #1630 Task B2): the paragraph below described a real gap that is now CLOSED.
// custom_category_service.go's validateParent rejects a parent only when parent.ParentID != nil
// — it does NOT reject a parent whose own ParentPFCPrimary is set. So a three-level chain is
// constructible: pfc X ← custom C (ParentPFCPrimary = X) ← custom G (ParentID = C).
// buildCategoryClause's "pfc" branch now rolls X's total up through BOTH hops (the direct child
// C via parent_pfc_primary, and the grandchild G via a second join through C), so a transaction
// filed under G IS counted in X's SpentAmount. That widening, by itself, would create a NEW
// double-count whenever G is budgeted directly and C is not: G's own top-level row would count
// its transactions AND X's roll-up would count them again, because this method previously only
// ever inspected bc's DIRECT parent linkage (CustomParentID/CustomParentPFCPrimary) and had no
// way to recognize that G nests under X two hops away. CustomGrandparentPFCPrimary closes that:
// it carries the PFC primary of bc's parent's own parent_pfc_primary, so a grandchild is now
// recognized as a sub-allocation of a budgeted PFC root exactly like a direct child is.
// 'or' child can match transactions outside the parent's category clause entirely, so it must
// never be treated as a sub-allocation even when it is hierarchically nested.
func (bc *BudgetCategory) IsSubAllocationWithin(budgetedPFCPrimaries, budgetedCustomIDs map[string]bool) bool {
	if !isContainedMatchLogic(bc.MatchLogic) {
		return false
	}
	switch bc.CategoryKind {
	case "pfc":
		// A PFC detailed is a child of its primary when that primary is also budgeted.
		if hasPFCDetailed(bc.PFCDetailed) && bc.PFCPrimary != nil && budgetedPFCPrimaries[*bc.PFCPrimary] {
			return true
		}
	case "custom":
		if bc.CustomParentID != nil && budgetedCustomIDs[*bc.CustomParentID] {
			return true
		}
		if bc.CustomParentPFCPrimary != nil && budgetedPFCPrimaries[*bc.CustomParentPFCPrimary] {
			return true
		}
		// Transitive case (Issue #1630 Task B2): bc is a GRANDCHILD of a PFC root — its own
		// parent (CustomParentID) is itself nested under CustomGrandparentPFCPrimary via
		// parent_pfc_primary. buildCategoryClause's widened roll-up reaches bc from that root
		// two hops away regardless of whether the intermediate parent is itself budgeted, so
		// this must agree even when the intermediate parent has no budget row at all.
		if bc.CustomGrandparentPFCPrimary != nil && budgetedPFCPrimaries[*bc.CustomGrandparentPFCPrimary] {
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

	if bc.CategoryKind != "group" && (bc.GroupName != nil || len(bc.Members) > 0) {
		return fmt.Errorf("%w: group_name and members require category_kind group", ErrInvalidBudget)
	}
	switch bc.CategoryKind {
	case "group", "remainder":
		if bc.PFCPrimary != nil || bc.PFCDetailed != nil || bc.CustomCategoryID != nil || bc.SavingsGoalID != nil {
			return fmt.Errorf("%w: group and remainder must not carry individual category selectors", ErrInvalidBudget)
		}
		if len(bc.Tags) > 0 || (bc.MatchLogic != "" && bc.MatchLogic != MatchLogicCategoryOnly) {
			return fmt.Errorf("%w: group and remainder require category_only without tags", ErrInvalidBudget)
		}
		if bc.CategoryKind == "group" {
			if bc.GroupName == nil || strings.TrimSpace(*bc.GroupName) == "" || len(*bc.GroupName) > 100 || (len(bc.Members) == 0 && bc.ID == "") {
				return fmt.Errorf("%w: group requires a name of at most 100 characters and at least one member", ErrInvalidBudget)
			}
			for _, member := range bc.Members {
				if err := member.Validate(); err != nil {
					return err
				}
			}
		}
	case "pfc":
		if bc.PFCPrimary == nil || *bc.PFCPrimary == "" {
			return fmt.Errorf("%w: pfc_primary is required when category_kind is pfc", ErrInvalidBudget)
		}
		// Issue #1630: pfc_detailed: "" is ambiguous between "no sub-category" (nil) and a
		// bogus non-nil-but-empty value. Go and SQL now BOTH treat "" identically to nil/absent
		// (hasPFCDetailed, and the SQL side's explicit `pfc_detailed IS NOT NULL AND pfc_detailed
		// <> ''` predicates) — matching the frontend's `!cat.pfc_detailed`, which already reads ""
		// as falsy. So a persisted "" no longer makes Go/SQL and TypeScript disagree about the
		// same row. This rejection is still worth keeping: reject at write time instead of relying
		// on every reader to keep applying that agreement correctly, because a present-but-empty
		// pfc_detailed is never a value any caller intends to set, whereas nil (omitted)
		// unambiguously means "no sub-category".
		if bc.PFCDetailed != nil && *bc.PFCDetailed == "" {
			return fmt.Errorf("%w: pfc_detailed must not be an empty string; omit it entirely for a primary-only category", ErrInvalidBudget)
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

// IsCategorySubAllocation reports whether the budget category identified by categoryID within
// categories (a full slice for ONE budget, e.g. from BudgetRepository.GetCategoriesByBudgetID) is
// currently a sub-allocation of another budgeted row in the same slice. It computes the same
// budgetedPFCPrimaries/budgetedCustomIDs sets BuildCategoryEventData does and calls
// BudgetCategory.IsSubAllocationWithin, but is callable directly against a freshly-loaded,
// LIVE slice rather than only through a stale triggerData snapshot (Issue #1630 review round 2:
// the execution-time sub-allocation check previously trusted triggerData's categories map, built
// when the event FIRED, which can be stale by the time an asynchronously-queued job actually
// runs).
//
// found is false when categoryID does not appear in categories at all (already deleted, wrong
// budget, etc.) — callers MUST NOT treat "not found" as "not a sub-allocation"; the prior bug was
// exactly that fail-open shortcut, just taken one layer up (missing triggerData data, not a
// missing category). Fail closed on found == false.
func IsCategorySubAllocation(categories []BudgetCategory, categoryID string) (isSubAllocation bool, found bool) {
	pfcPrimaries, customIDs := budgetedParentSetsFor(categories)
	for i := range categories {
		if categories[i].ID == categoryID {
			return categories[i].IsSubAllocationWithin(pfcPrimaries, customIDs), true
		}
	}
	return false, false
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
	CategoryKind string              `json:"category_kind" validate:"required,oneof=pfc custom savings group remainder"`
	GroupName    *string             `json:"group_name,omitempty"`
	Members      []BudgetGroupMember `json:"members,omitempty"`
	// PFCPrimary set with PFCDetailed omitted makes this a pfc_primary category. A pfc_primary
	// category's own spend total ROLLS UP every transaction filed under a custom category
	// nested under it (parent_pfc_primary), in addition to its own directly-matched
	// transactions — the same roll-up previewCategoryMatches discloses via
	// direct_transaction_count/direct_amount/children. See EscapedNestedChild /
	// ErrEscapedNestedChild for the match_logic constraint a nested child must satisfy to stay
	// correctly excluded from double-counting against this roll-up.
	PFCPrimary       *string `json:"pfc_primary,omitempty" doc:"PFC primary category. When set with pfc_detailed omitted, this row is a pfc_primary category whose own spend total rolls up transactions filed under any custom category nested under it, in addition to its own direct matches."`
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
	// StartDate/EndDate are domain.FlexibleDate, matching CreateBudgetRequest (Issue #1630
	// Task 9): accepts YYYY-MM-DD (matching every read endpoint's date format) or a full
	// RFC3339 timestamp. Left as *time.Time until now, which 422'd "2026-09-01" here while
	// CreateBudgetRequest already accepted it — issue #1630's own P3 complaint, one verb over.
	StartDate *FlexibleDate `json:"start_date,omitempty" doc:"Budget start date. Accepts YYYY-MM-DD (matching every read endpoint's date format) or a full RFC3339 timestamp."`
	EndDate   *FlexibleDate `json:"end_date,omitempty" doc:"Required only when period is 'custom'. When set, must be strictly after start_date. Optional for every other period (weekly/monthly/quarterly/yearly) — omitting it there is valid, not an error. Accepts YYYY-MM-DD or a full RFC3339 timestamp."`
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
	GroupName       *string              `json:"group_name,omitempty"`
	Members         *[]BudgetGroupMember `json:"members,omitempty"`
	AllocatedAmount *float64             `json:"allocated_amount,omitempty" validate:"omitempty,gte=0"`

	// Flexible matching fields (Issue #491)
	Tags       *[]string   `json:"tags,omitempty"`
	MatchLogic *MatchLogic `json:"match_logic,omitempty" enum:"category_only,tag_only,and,or" doc:"How tags and category matching combine. tag_only and and both REQUIRE a non-empty tags array — omitting tags with either of these is rejected (400)."`
}

// CategoryPreviewRequest represents the request to preview matching transactions for a category config.
// Issue #497: Transaction preview feature
//
// StartDate/EndDate are domain.FlexibleDate (Issue #1630 Task B5, gap #21) — accepting both
// YYYY-MM-DD and RFC3339, matching createBudget/updateBudget (Issue #1630 Task 9). This was the
// last write-shaped operation still carrying P3's original YYYY-MM-DD-only inconsistency.
type CategoryPreviewRequest struct {
	BudgetID          string              `json:"budget_id,omitempty" doc:"Existing budget whose complete sibling configuration participates in matching."`
	ReplaceCategoryID string              `json:"replace_category_id,omitempty" doc:"Existing allocation being edited, omitted when adding a new line."`
	DraftCategories   []BudgetCategory    `json:"draft_categories,omitempty" doc:"Complete unsaved sibling configuration for a new budget preview."`
	GroupName         *string             `json:"group_name,omitempty"`
	Members           []BudgetGroupMember `json:"members,omitempty"`
	CategoryKind      string              `json:"category_kind"`
	PFCPrimary        *string             `json:"pfc_primary,omitempty"`
	PFCDetailed       *string             `json:"pfc_detailed,omitempty"`
	CustomCategoryID  *string             `json:"custom_category_id,omitempty"`
	Tags              []string            `json:"tags,omitempty"`
	MatchLogic        MatchLogic          `json:"match_logic" validate:"required,oneof=category_only tag_only and or" enum:"category_only,tag_only,and,or" doc:"How tags and category matching combine. tag_only and and both REQUIRE a non-empty tags array."`
	StartDate         FlexibleDate        `json:"start_date" validate:"required" doc:"Start of the date range to preview. Accepts YYYY-MM-DD (matching every read endpoint's date format) or a full RFC3339 timestamp."`
	EndDate           FlexibleDate        `json:"end_date" validate:"required" doc:"End of the date range to preview (inclusive; the service extends it to end-of-day). Must not be before start_date. Accepts YYYY-MM-DD or a full RFC3339 timestamp."`
}

// CategoryPreviewResponse represents the response with matching transaction stats.
// Issue #497: Transaction preview feature
//
// Issue #1630 Task B5: a `pfc_primary` (or parent `custom`) category rolls up its nested
// children (Issue #1203) — a correct total that nonetheless surprised a reporter who had
// no way to see it. direct_amount, children, and tag_only_amount disclose a REAL PARTITION
// of total_amount — the three always sum to it exactly:
//  1. category-direct (DirectTransactionCount/DirectAmount): filed straight under the
//     targeted category itself.
//  2. nested-descendant (Children): one nested custom category (a pfc_primary's nested
//     custom categories, or a parent custom category's custom children, both hops) with
//     its own share of the total.
//  3. tag-only-outside-category (TagOnlyTransactionCount/TagOnlyAmount): pulled into the
//     total by the tag clause (match_logic or/tag_only) but not matched by the category
//     clause at all — not the target, not any nested descendant.
//
// All three are zero-valued/nil together when there is nothing to disclose (a leaf
// category with match_logic category_only and no tags — the total is already
// self-explanatory). TransactionCount and TotalAmount remain the single rolled-up figure
// every existing caller already relies on; this is additive.
type CategoryPreviewResponse struct {
	CurrencyCode     string  `json:"currency_code,omitempty" doc:"Currency of matching activity; no conversion or mixed-currency summation is performed."`
	TransactionCount int     `json:"transaction_count" doc:"Total matching transaction count. When the targeted category is a pfc_primary or parent custom category, this INCLUDES transactions filed under its nested custom categories — see direct_transaction_count, children, and tag_only_transaction_count to break the total down."`
	TotalAmount      float64 `json:"total_amount" doc:"Total matching transaction amount. direct_amount + every children[].total_amount + tag_only_amount always sum to exactly this value when any of them is populated — see those fields to break the total down."`

	// DirectTransactionCount/DirectAmount cover only transactions filed directly under the
	// targeted category (not a nested child, and not pulled in by a tag alone).
	DirectTransactionCount int     `json:"direct_transaction_count,omitempty" doc:"Count of matching transactions filed directly under the targeted category itself, excluding any nested custom category and excluding tag-only-outside-category matches. Zero-valued when there is nothing to disclose."`
	DirectAmount           float64 `json:"direct_amount,omitempty" doc:"Amount of matching transactions filed directly under the targeted category itself, excluding any nested custom category and excluding tag-only-outside-category matches. Zero-valued when there is nothing to disclose."`

	// Children breaks the rolled-up total down by nested custom category. Empty/nil when
	// the category kind cannot roll up (a leaf category), or no nested child actually matched.
	Children []CategoryPreviewBreakdownItem `json:"children,omitempty" doc:"Breaks the rolled-up transaction_count/total_amount down by each nested custom category that actually matched. Empty/nil when the targeted category cannot roll up (a leaf category), or no nested child matched anything."`

	// TagOnlyTransactionCount/TagOnlyAmount are the third partition bucket: transactions
	// the tag clause pulled into the total (match_logic or/tag_only) that the category
	// clause never matched at all.
	TagOnlyTransactionCount int     `json:"tag_only_transaction_count,omitempty" doc:"Count of matching transactions pulled into the total by the tag clause (match_logic or/tag_only) that the category clause did not match at all — not the targeted category itself, and not any nested custom category. Always zero for match_logic category_only or and, since every included row there is required to satisfy the category clause."`
	TagOnlyAmount           float64 `json:"tag_only_amount,omitempty" doc:"Amount of matching transactions pulled into the total by the tag clause (match_logic or/tag_only) that the category clause did not match at all — not the targeted category itself, and not any nested custom category. Always zero for match_logic category_only or and, since every included row there is required to satisfy the category clause."`
}

// CategoryPreviewBreakdownItem is one nested child's contribution to a rolled-up
// CategoryPreviewResponse total. Issue #1630 Task 5.
type CategoryPreviewBreakdownItem struct {
	CategoryID       string  `json:"category_id" doc:"UUID of the nested custom category this breakdown entry covers."`
	CategoryName     string  `json:"category_name" doc:"Display name of the nested custom category this breakdown entry covers."`
	TransactionCount int     `json:"transaction_count" doc:"Count of matching transactions filed under this nested custom category, already included in the parent CategoryPreviewResponse's transaction_count."`
	TotalAmount      float64 `json:"total_amount" doc:"Amount of matching transactions filed under this nested custom category, already included in the parent CategoryPreviewResponse's total_amount."`
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
	// leaving which row "owns" a given child's rendering undefined. Enforced at write time only,
	// from CreateBudget's batch path and AddCategory — NOT from UpdateBudget (which updates the
	// Budget row's own fields and never touches Categories at all; UpdateCategory, which DOES
	// mutate a Category row's match_logic, is guarded separately by EscapedNestedChild instead,
	// since parent-identity fields are immutable via that request and duplication cannot be
	// introduced there). No backfill: this guard is a
	// write-time gate, so a pre-existing colliding
	// row (issue #1630 was reproduced against a real production budget,
	// bf6676b1-7bbd-4f06-b765-62756ce0094e, five categories) is grandfathered — it stays readable
	// and updatable, and only a NEW row that would collide with it is refused going forward.
	ErrDuplicateParentCategory = errors.New("a budget category with this parent identity already exists")
	// ErrEscapedNestedChild: adversarial review of Issue #1630's own fix found a HIGH-severity
	// double-count it introduced, and a SECOND independent adversarial review found this guard's
	// first fix was itself incomplete (fixwave 4) — see EscapedNestedChild's doc comment for the
	// full "containment is one way to get disjointness, not the only one" reasoning.
	// buildCategoryClause (transaction_repository.go) rolls up a parent's SpentAmount
	// hierarchically — every transaction filed under a nested custom category or PFC detailed,
	// regardless of that child's own match_logic, AND regardless of the PARENT's own match_logic
	// (an 'and' parent's clause still runs the category roll-up before applying its tag filter).
	// The Go/SQL/TS containment rule (isContainingMatchLogic(parent) && isContainedMatchLogic
	// (child)) only decides whether the CHILD is excluded from top-level totals; it does nothing
	// to the PARENT's roll-up query, which has no sibling/match_logic context at all. So ANY
	// nested pair that isn't cleanly contained — including a custom category nested under a
	// budgeted 'category_only'/'or' parent but itself 'or'/'tag_only' (fixwave 3), and an 'and' or
	// 'tag_only' parent with any nested child at all (fixwave 4) — is counted BOTH as its own
	// top-level row AND inside the parent's roll-up (or the parent's own overlapping tag-match
	// sum) — every dollar doubled. Wraps ErrInvalidBudget so the mapper's existing 400 case picks
	// it up with no separate switch entry.
	//
	// Fixed by refusing to PERSIST the escaping shape at all (EscapedNestedChild, wired into
	// CreateBudget's batch loop and AddCategory — the same two call sites as
	// DuplicateParentIdentity). UpdateCategory ALSO mutates match_logic and can recreate this
	// exact shape as new data (PATCHing a conforming nested child's match_logic to escape, or
	// PATCHing a parent's match_logic away from clean containment while an existing nested child
	// still relies on it) — it is guarded by EscapedNestedChild too, which is now the ONLY guard:
	// see EscapedNestedChild's doc comment for why the widened single check subsumes the
	// now-removed WouldOrphanContainedChildren. UpdateBudget (the Budget row's own fields) is the
	// one path that genuinely never touches Categories. This is write-time only and does NOT
	// retroactively fix the parent's roll-up query: any row that predates this guard (or predates
	// fixwave 4's widening) keeps the pre-existing double-count forever, exactly as
	// ErrDuplicateParentCategory's rows are grandfathered — it stays readable and updatable, just
	// never re-created. This is not hypothetical: issue #1630 was reproduced against a real
	// production budget (bf6676b1-7bbd-4f06-b765-62756ce0094e, five categories), so say plainly
	// that the double count for any pre-existing escaping row persists — it is not fixed.
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
		if hasPFCDetailed(candidate.PFCDetailed) || candidate.PFCPrimary == nil {
			return false
		}
		for i := range existing {
			e := &existing[i]
			if e.CategoryKind == "pfc" && !hasPFCDetailed(e.PFCDetailed) &&
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
//
// Also recognizes the TRANSITIVE pair (Issue #1630 Task B2): child.CustomGrandparentPFCPrimary
// set and equal to a PFC-root parent's own PFCPrimary — child is a grandchild of that root two
// hops away (root -> child's own parent via parent_pfc_primary -> child via parent_id), the same
// pair buildCategoryClause's widened roll-up now sums together. Without this, EscapedNestedChild
// would let an 'or'/'tag_only' grandchild coexist with a budgeted root that its own roll-up
// double-counts, exactly the shape this function exists to refuse for the one-hop case.
func hierarchyHasNestedChild(parent, child *BudgetCategory) bool {
	switch child.CategoryKind {
	case "pfc":
		return hasPFCDetailed(child.PFCDetailed) && child.PFCPrimary != nil &&
			parent.CategoryKind == "pfc" && !hasPFCDetailed(parent.PFCDetailed) && parent.PFCPrimary != nil &&
			*parent.PFCPrimary == *child.PFCPrimary
	case "custom":
		if child.CustomParentID != nil && parent.CategoryKind == "custom" && parent.CustomCategoryID != nil &&
			*parent.CustomCategoryID == *child.CustomParentID {
			return true
		}
		if child.CustomParentPFCPrimary != nil && parent.CategoryKind == "pfc" && !hasPFCDetailed(parent.PFCDetailed) &&
			parent.PFCPrimary != nil && *parent.PFCPrimary == *child.CustomParentPFCPrimary {
			return true
		}
		if child.CustomGrandparentPFCPrimary != nil && parent.CategoryKind == "pfc" && !hasPFCDetailed(parent.PFCDetailed) &&
			parent.PFCPrimary != nil && *parent.PFCPrimary == *child.CustomGrandparentPFCPrimary {
			return true
		}
	}
	return false
}

// EscapedNestedChild reports whether `candidate` would form a hierarchically-nested pair with
// some row in `existing` that is not CLEANLY CONTAINED (Issue #1630 HIGH review finding, WIDENED
// by an independent adversarial review — see ErrEscapedNestedChild).
//
// A pair is cleanly contained only when the would-be PARENT's match_logic still contains the
// plain category match (isContainingMatchLogic: category_only/or) AND the would-be CHILD's stays
// inside it (isContainedMatchLogic: category_only/and). Every OTHER combination is refused,
// including an 'and' or 'tag_only' PARENT with a nested child — the case the first version of
// this guard missed, because it only ever checked pairs where isContainingMatchLogic(parent) was
// already true. That check silently assumed a non-containing parent's own SpentAmount could never
// overlap a nested child's: true for the specific "buildCategoryClause rolls up the child's
// category into the parent's category clause" mechanism the first fix targeted, but false in
// general — an 'and' parent's clause is `parent_category ∧ parent_tags`, and buildCategoryClause
// still rolls the nested child's category into that `parent_category` term before the tag filter
// is applied, so any transaction carrying the parent's tag AND filed under the child's (contained)
// category is swept into the parent's sum; a 'tag_only' parent skips buildCategoryClause
// entirely, but its pure tag-match sum still catches any transaction that happens to carry the
// parent's tag and sits in the child's category, which the child then ALSO counts as its own
// top-level row. Neither is a superset of the other's matched set, but their matched sets are not
// disjoint either — and summing top-level rows requires disjointness, not containment
// specifically. Containment is the one combination this codebase can PROVE disjoint without
// reasoning about the transactions themselves; refusing every other nested combination is the
// only sound response until/unless a specific non-containing combination is proven disjoint too.
//
// Checked in BOTH directions so call-site ordering within one batch never matters, and so an edit
// to either the CHILD's or the PARENT's own match_logic (UpdateCategory can PATCH either row) is
// caught by the same single check:
//   - candidate as the CHILD of an existing (already-persisted-or-already-built) row — the common
//     case: adding a nested category under a parent that exists already, or PATCHing a nested
//     child's own match_logic.
//   - candidate as the PARENT of an existing nested row — covers a batch where the child request
//     happens to be processed before its parent (CreateBudget's request order is caller-controlled,
//     so either order is legal input), and covers PATCHing a PARENT's match_logic away from
//     containing while an existing, still-conforming child relies on it (the transition a prior
//     narrower version of this guard needed a second function, WouldOrphanContainedChildren, to
//     catch — this widened single check subsumes it: NOT(isContainingMatchLogic(candidate) &&
//     isContainedMatchLogic(other)) is true whenever candidate stops containing at all, for any
//     nested other, exactly what WouldOrphanContainedChildren tested. WouldOrphanContainedChildren
//     was removed rather than also widened).
//
// Only a hierarchically-nested pair is considered at all; two unrelated top-level rows never
// trip this regardless of their match_logic values.
func EscapedNestedChild(existing []BudgetCategory, candidate *BudgetCategory) bool {
	if candidate == nil {
		return false
	}
	for i := range existing {
		other := &existing[i]
		// candidate as child, other as parent: refuse unless other cleanly contains candidate.
		if hierarchyHasNestedChild(other, candidate) &&
			(!isContainingMatchLogic(other.MatchLogic) || !isContainedMatchLogic(candidate.MatchLogic)) {
			return true
		}
		// candidate as parent, other as child: refuse unless candidate cleanly contains other.
		if hierarchyHasNestedChild(candidate, other) &&
			(!isContainingMatchLogic(candidate.MatchLogic) || !isContainedMatchLogic(other.MatchLogic)) {
			return true
		}
	}
	return false
}
