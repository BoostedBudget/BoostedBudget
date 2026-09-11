// Package domain contains domain models for the boosts engine.
// Issue #35: Rules Engine Core - supports transfer suggestions, budget alerts, and category rollovers.
// This is SEPARATE from categorization rules (Issue #346) which handle transaction auto-categorization.
package domain

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/robfig/cron/v3"
)

// =============================================================================
// Boost Types
// =============================================================================

// BoostType represents the type of boost.
type BoostType string

// Supported boost types (matches database CHECK constraint).
const (
	BoostTypeTransferSuggestion BoostType = "transfer_suggestion"
	BoostTypeBudgetAlert        BoostType = "budget_alert"
	BoostTypeCategoryRollover   BoostType = "category_rollover"
	BoostTypeSavingsGoal        BoostType = "savings_goal" // Issue #36: Savings goal contributions
)

// validBoostTypes is a set of all valid boost types.
var validBoostTypes = map[BoostType]bool{
	BoostTypeTransferSuggestion: true,
	BoostTypeBudgetAlert:        true,
	BoostTypeCategoryRollover:   true,
	BoostTypeSavingsGoal:        true,
}

// IsValid checks if the boost type is valid.
func (bt BoostType) IsValid() bool {
	return validBoostTypes[bt]
}

// =============================================================================
// Trigger Types
// =============================================================================

// TriggerType represents how a rule is triggered.
type TriggerType string

// Supported trigger types.
const (
	TriggerTypeEvent    TriggerType = "event"    // Triggered by events (budget_updated, transaction_created, etc.)
	TriggerTypeSchedule TriggerType = "schedule" // Triggered by cron schedule (Issue #37)
)

// =============================================================================
// Event Type Constants
// =============================================================================

// Event type constants for event-triggered automation rules.
// These are the valid values for TriggerConfig.Event when TriggerType is "event".
// Issue #35: Rules Engine Core - budget_updated event integration.
// Issue #972: Savings Transfer Automation - budget_period_end event.
const (
	EventBudgetUpdated   = "budget_updated"    // Fired when budget income/spent amounts are recalculated
	EventBudgetPeriodEnd = "budget_period_end" // Fired when a budget period ends (for rollover/savings rules)
)

// EventData key constants for rule trigger data maps.
// These keys MUST match between the emitter (budget_period_close_check.go) and the
// readers (calculateTransferAmount / calculateSavingsGoalAmount). Using constants
// eliminates the class of bug where a reader accesses a different string than the
// emitter publishes (e.g. "balance" vs "remaining").
//
// Issue #972: Savings Transfer Automation — key mismatch bug fix (GAP G).
const (
	EventDataKeyRemaining    = "remaining"     // Budget remaining amount after spending; emitted by period-close check
	EventDataKeyAmount       = "amount"        // Budget total amount (income) for the period
	EventDataKeyIncomeAmount = "income_amount" // Explicit income amount; emitted by period-close check
	EventDataKeyTotalSpent   = "total_spent"   // Total spent in the period; emitted by period-close check
	EventDataKeyPercentUsed  = "percent_used"  // Budget percent used (0-100+); emitted by period-close check via Budget.PercentUsed()
	EventDataKeyBudgetID     = "budget_id"     // Budget UUID; emitted by period-close check
	EventDataKeyBudgetName   = "budget_name"   // Human-readable budget name; emitted by period-close check
	EventDataKeyPeriodStart  = "period_start"  // Period start date (YYYY-MM-DD); emitted by period-close check
	EventDataKeyPeriodEnd    = "period_end"    // Period end date (YYYY-MM-DD); emitted by period-close check
	EventDataKeyCategories   = "categories"    // Per-category metrics map (keyed by category ID); emitted by both emitters (#1270)

	// EventDataCategoryKeyIsSubAllocation is a per-category metric key (nested under
	// EventDataKeyCategories) marking whether that row is a sub-allocation of a budgeted
	// parent (Issue #1630 Task 3.6). 1.0 if the row's dollars are already reflected in
	// its parent's own spent/remaining (BudgetCategory.IsSubAllocationWithin), 0.0
	// otherwise. Consumers that sum or roll per-category dollars across rows (e.g. the
	// category_rollover boost executor) MUST consult this before treating a row as an
	// independent source of money — otherwise a sub-allocation's dollars, already
	// counted via its parent, get counted a second time. Kept as a float64 (not bool)
	// so the value fits the same map[string]map[string]float64 shape BuildCategoryEventData
	// already returns for every other per-category metric.
	EventDataCategoryKeyIsSubAllocation = "is_sub_allocation"
)

// validTriggerTypes is a set of all valid trigger types.
var validTriggerTypes = map[TriggerType]bool{
	TriggerTypeEvent:    true,
	TriggerTypeSchedule: true,
}

// IsValid checks if the trigger type is valid.
func (tt TriggerType) IsValid() bool {
	return validTriggerTypes[tt]
}

// validEventTypes is the complete set of event names accepted for event-triggered rules.
// This is checked by TriggerConfig.Validate to reject unknown event names at API ingestion.
// See also validConditionFieldsByEvent for per-event condition-field restrictions.
var validEventTypes = map[string]bool{
	EventBudgetUpdated:   true,
	EventBudgetPeriodEnd: true,
	"transaction_created": true,
	"balance_changed":     true,
}

// validConditionFieldsByEvent maps budget event types to their allowed condition fields.
// For events present as keys, validateConditions enforces that every condition field
// must be in the inner set. Events absent from this map (e.g. balance_changed,
// transaction_created) do not have a field allowlist — their conditions are not
// field-validated (the emitter owns the shape of that event's data).
var validConditionFieldsByEvent = map[string]map[string]bool{
	EventBudgetUpdated: {
		"budget.percent_used":              true,
		"budget.remaining_amount":          true,
		"budget.total_spent":               true,
		"budget_category.percent_used":     true,
		"budget_category.spent_amount":     true,
		"budget_category.remaining_amount": true,
		"budget_category.allocated":        true, // Issue #1271 Item 5
	},
	EventBudgetPeriodEnd: {
		"budget.percent_used":              true,
		"budget.remaining_amount":          true,
		"budget.total_spent":               true,
		"budget_category.percent_used":     true,
		"budget_category.spent_amount":     true,
		"budget_category.remaining_amount": true,
		"budget_category.allocated":        true, // Issue #1271 Item 5
	},
}

// allowedConditionOperators is the set of operators supported by
// validateConditions / evaluateCondition. ConditionOperatorBetween is
// intentionally excluded because it is not handled in evaluateCondition.
var allowedConditionOperators = map[ConditionOperator]bool{
	ConditionOperatorEq:       true,
	ConditionOperatorNeq:      true,
	ConditionOperatorGt:       true,
	ConditionOperatorGte:      true,
	ConditionOperatorLt:       true,
	ConditionOperatorLte:      true,
	ConditionOperatorContains: true,
}

// AllowedConditionOperators returns the set of condition operators that are both
// allowed at validation time and evaluable at execution time.
func AllowedConditionOperators() map[ConditionOperator]bool {
	return allowedConditionOperators
}

// =============================================================================
// Condition Operators
// =============================================================================

// ConditionOperator represents comparison operators for conditions.
type ConditionOperator string

// Supported condition operators.
const (
	ConditionOperatorEq       ConditionOperator = "eq"       // Equal
	ConditionOperatorNeq      ConditionOperator = "neq"      // Not equal
	ConditionOperatorGt       ConditionOperator = "gt"       // Greater than
	ConditionOperatorGte      ConditionOperator = "gte"      // Greater than or equal
	ConditionOperatorLt       ConditionOperator = "lt"       // Less than
	ConditionOperatorLte      ConditionOperator = "lte"      // Less than or equal
	ConditionOperatorBetween  ConditionOperator = "between"  // Between two values
	ConditionOperatorContains ConditionOperator = "contains" // Contains substring/element
)

// validConditionOperators is a set of all valid condition operators.
var validConditionOperators = map[ConditionOperator]bool{
	ConditionOperatorEq:       true,
	ConditionOperatorNeq:      true,
	ConditionOperatorGt:       true,
	ConditionOperatorGte:      true,
	ConditionOperatorLt:       true,
	ConditionOperatorLte:      true,
	ConditionOperatorBetween:  true,
	ConditionOperatorContains: true,
}

// IsValid checks if the condition operator is valid.
func (co ConditionOperator) IsValid() bool {
	return validConditionOperators[co]
}

// =============================================================================
// Execution Status
// =============================================================================

// ExecutionStatus represents the status of a rule execution.
type ExecutionStatus string

// Supported execution statuses (matches database CHECK constraint).
const (
	ExecutionStatusPending   ExecutionStatus = "pending"
	ExecutionStatusRunning   ExecutionStatus = "running"
	ExecutionStatusCompleted ExecutionStatus = "completed"
	ExecutionStatusFailed    ExecutionStatus = "failed"
)

// validExecutionStatuses is a set of all valid execution statuses.
var validExecutionStatuses = map[ExecutionStatus]bool{
	ExecutionStatusPending:   true,
	ExecutionStatusRunning:   true,
	ExecutionStatusCompleted: true,
	ExecutionStatusFailed:    true,
}

// IsValid checks if the execution status is valid.
func (es ExecutionStatus) IsValid() bool {
	return validExecutionStatuses[es]
}

// =============================================================================
// Suggestion Status and Source
// =============================================================================

// SuggestionStatus represents the status of a transfer suggestion.
type SuggestionStatus string

// Supported suggestion statuses (matches database CHECK constraint).
const (
	SuggestionStatusPending   SuggestionStatus = "pending"
	SuggestionStatusAccepted  SuggestionStatus = "accepted"
	SuggestionStatusRejected  SuggestionStatus = "rejected"
	SuggestionStatusExpired   SuggestionStatus = "expired"
	SuggestionStatusCompleted SuggestionStatus = "completed"
)

// validSuggestionStatuses is a set of all valid suggestion statuses.
var validSuggestionStatuses = map[SuggestionStatus]bool{
	SuggestionStatusPending:   true,
	SuggestionStatusAccepted:  true,
	SuggestionStatusRejected:  true,
	SuggestionStatusExpired:   true,
	SuggestionStatusCompleted: true,
}

// IsValid checks if the suggestion status is valid.
func (ss SuggestionStatus) IsValid() bool {
	return validSuggestionStatuses[ss]
}

// SuggestionSource represents the source of a transfer suggestion.
type SuggestionSource string

// Supported suggestion sources (matches database CHECK constraint).
const (
	SuggestionSourceBoost  SuggestionSource = "boost"
	SuggestionSourceManual SuggestionSource = "manual"
	SuggestionSourceAI     SuggestionSource = "ai"
)

// validSuggestionSources is a set of all valid suggestion sources.
var validSuggestionSources = map[SuggestionSource]bool{
	SuggestionSourceBoost:  true,
	SuggestionSourceManual: true,
	SuggestionSourceAI:     true,
}

// IsValid checks if the suggestion source is valid.
func (ss SuggestionSource) IsValid() bool {
	return validSuggestionSources[ss]
}

// =============================================================================
// Trigger Configuration
// =============================================================================

// TriggerConfig defines when a rule should fire.
type TriggerConfig struct {
	Type     TriggerType            `json:"type"`
	Event    string                 `json:"event,omitempty"`    // For event triggers: budget_updated, transaction_created, balance_changed, budget_period_end
	Filters  map[string]interface{} `json:"filters,omitempty"`  // Event filtering (e.g., min_amount, account_id)
	Cron     string                 `json:"cron,omitempty"`     // For schedule triggers: cron expression
	Timezone string                 `json:"timezone,omitempty"` // For schedule triggers: e.g., "America/New_York"
}

// Validate checks that the trigger configuration is valid.
func (tc *TriggerConfig) Validate() error {
	if !tc.Type.IsValid() {
		return errors.New("invalid trigger type: " + string(tc.Type))
	}

	switch tc.Type {
	case TriggerTypeEvent:
		if tc.Event == "" {
			return errors.New("event is required for event trigger type")
		}
		if !validEventTypes[tc.Event] {
			return fmt.Errorf("unknown event type %q", tc.Event)
		}
	case TriggerTypeSchedule:
		if tc.Cron == "" {
			return errors.New("cron expression is required for schedule trigger type")
		}
		// Validate cron expression (Issue #37)
		if err := ValidateCronExpression(tc.Cron); err != nil {
			return err
		}
		// Validate timezone if provided
		if tc.Timezone != "" {
			if _, err := time.LoadLocation(tc.Timezone); err != nil {
				return fmt.Errorf("invalid timezone '%s': %w", tc.Timezone, err)
			}
		}
	}

	return nil
}

// ValidateCronExpression validates a cron expression string.
// Issue #37: Rule Scheduler - used to validate cron expressions before scheduling.
// Supports standard 5-field cron syntax: minute, hour, day-of-month, month, day-of-week.
func ValidateCronExpression(expr string) error {
	parser := cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow)
	_, err := parser.Parse(expr)
	if err != nil {
		return fmt.Errorf("invalid cron expression '%s': %w", expr, err)
	}
	return nil
}

// =============================================================================
// Rule Condition
// =============================================================================

// BoostCondition represents a condition that must be met for a boost to fire.
type BoostCondition struct {
	Field      string            `json:"field"`                // Field to evaluate: account.balance, budget.percent_used, budget_category.remaining, etc.
	Operator   ConditionOperator `json:"operator"`             // Comparison operator
	Value      interface{}       `json:"value"`                // Value to compare against
	CategoryID string            `json:"category_id,omitempty"` // Optional category ID for category-scoped conditions
	BudgetID   string            `json:"budget_id,omitempty"`   // Optional budget ID for budget-scoped conditions
}

// =============================================================================
// Action Types (JSONB parsed structures)
// =============================================================================

// TransferAction defines the action for transfer_suggestion rules.
type TransferAction struct {
	FromAccountID     string  `json:"from_account_id"`
	ToAccountID       string  `json:"to_account_id"`
	FromUserAccountID string  `json:"from_user_account_id,omitempty"` // Stable reference surviving re-links (Issue #1109)
	ToUserAccountID   string  `json:"to_user_account_id,omitempty"`   // Stable reference surviving re-links (Issue #1109)
	AmountType        string  `json:"amount_type,omitempty"`          // fixed, percentage, excess, remaining, threshold (legacy)
	Amount            float64 `json:"amount"`
	Threshold         float64 `json:"threshold,omitempty"` // For excess/threshold types: minimum before transfer (must be ≥0)
}

// AlertAction defines the action for budget_alert rules.
type AlertAction struct {
	NotificationChannels []string `json:"notification_channels"` // email, push, in_app
	ThresholdType        string   `json:"threshold_type"`        // percentage, amount
	ThresholdValue       float64  `json:"threshold_value"`
	BudgetID             string   `json:"budget_id,omitempty"`
	CategoryID           string   `json:"category_id,omitempty"`
	TitleTemplate        string   `json:"title_template,omitempty"`
	MessageTemplate      string   `json:"message_template,omitempty"`
}

// RolloverAction defines the action for category_rollover rules.
// Issue #36: Rule Executors - Enhanced for envelope budgeting with optional target category.
type RolloverAction struct {
	SourceBudgetCategoryID string   `json:"source_budget_category_id"`
	TargetBudgetCategoryID *string  `json:"target_budget_category_id,omitempty"` // nil = to unallocated funds
	RolloverType           string   `json:"rollover_type"`                       // full, capped, percentage
	CapAmount              *float64 `json:"cap_amount,omitempty"`
	Percentage             *float64 `json:"percentage,omitempty"`
}

// SavingsGoalAction defines the action for savings_goal rules.
// Issue #36: Rule Executors - Creates transfer suggestions to contribute to savings goals.
type SavingsGoalAction struct {
	SavingsGoalID string   `json:"savings_goal_id"`      // Target savings goal
	FromAccountID string   `json:"from_account_id"`      // Source account for transfer
	AmountType    string   `json:"amount_type"`          // fixed, percentage, remaining
	Amount        float64  `json:"amount,omitempty"`     // For fixed amount type
	Percentage    *float64 `json:"percentage,omitempty"` // For percentage amount type
	Threshold     float64  `json:"threshold,omitempty"`  // Minimum balance threshold for remaining type
}

// =============================================================================
// Boost
// =============================================================================

// Boost represents a user-defined boost.
type Boost struct {
	ID               string                    `json:"id" db:"id"`
	UserID           string                    `json:"user_id" db:"user_id"`
	Name             string                    `json:"name" db:"name"`
	Description      *string                   `json:"description,omitempty" db:"description"`
	Enabled          bool                      `json:"enabled" db:"enabled"`
	Priority         int                       `json:"priority" db:"priority"` // Higher = evaluated first
	BoostType         BoostType                  `json:"boost_type" db:"boost_type"`
	TriggerConfig    TriggerConfig             `json:"trigger_config" db:"-"` // Stored as JSONB
	Conditions       []BoostCondition `json:"conditions" db:"-"`     // Stored as JSONB
	ConditionLogic   string                    `json:"condition_logic" db:"condition_logic"`
	Actions          json.RawMessage           `json:"actions" db:"-"` // Stored as JSONB, structure depends on boost_type
	ExecutionCount   int64                     `json:"execution_count" db:"execution_count"`
	LastExecutedAt   *time.Time                `json:"last_executed_at,omitempty" db:"last_executed_at"`
	LastSuccessfulAt *time.Time                `json:"last_successful_at,omitempty" db:"last_successful_at"`
	CreatedAt        time.Time                 `json:"created_at" db:"created_at"`
	UpdatedAt        time.Time                 `json:"updated_at" db:"updated_at"`
}

// Validate checks that the boost has all required fields and valid values.
func (b *Boost) Validate() error {
	if b.UserID == "" {
		return errors.New("user_id is required")
	}

	if b.Name == "" {
		return errors.New("name is required")
	}

	if len(b.Name) > 100 {
		return errors.New("name must be 100 characters or less")
	}

	if !b.BoostType.IsValid() {
		return errors.New("invalid boost_type: " + string(b.BoostType))
	}

	// Validate trigger config
	if err := b.TriggerConfig.Validate(); err != nil {
		return err
	}

	// Validate conditions — operator and CategoryID checks apply to all trigger types;
	// field allowlists are only enforced for events that have a defined set.
	if err := b.validateConditions(); err != nil {
		return err
	}

	// Validate condition logic (default to AND if empty)
	if b.ConditionLogic != "" && b.ConditionLogic != "AND" && b.ConditionLogic != "OR" {
		return errors.New("condition_logic must be AND or OR")
	}

	// Validate actions based on rule type
	return b.validateActions()
}

// validateConditions checks each condition's operator and field against the
// allowlists for the boost's event type. It must only be called for event-type
// triggers (caller responsibility).
func (b *Boost) validateConditions() error {
	for _, condition := range b.Conditions {
		if !allowedConditionOperators[condition.Operator] {
			return fmt.Errorf("condition field %q has unsupported operator %q", condition.Field, condition.Operator)
		}
		// Only enforce field allowlist for events that have a defined field set.
		// Events without an entry (e.g. balance_changed, transaction_created) do not
		// have a field allowlist — their conditions are not field-validated here.
		if allowedFields, hasAllowlist := validConditionFieldsByEvent[b.TriggerConfig.Event]; hasAllowlist {
			if !allowedFields[condition.Field] {
				return fmt.Errorf("condition field %q is not valid for event type %q", condition.Field, b.TriggerConfig.Event)
			}
		}
	}
	return nil
}

// BoundBudgetID returns the budget this boost is scoped to, derived from its
// category-scoped conditions. Returns ("", false) when no condition binds a budget.
// The result is AND/OR-agnostic: any category-scoped condition binds the whole rule (A-CR2).
func (b *Boost) BoundBudgetID() (string, bool) {
	for _, c := range b.Conditions {
		if strings.HasPrefix(c.Field, "budget_category.") && c.BudgetID != "" {
			return c.BudgetID, true
		}
	}
	return "", false
}

// ValidateForPersistence validates the boost for create/update. It calls Validate()
// for static checks (wrapping any error as ErrInvalidBoost), then additionally requires
// scope IDs for category-scoped conditions and validates multi-budget constraint.
func (b *Boost) ValidateForPersistence() error {
	if err := b.Validate(); err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidBoost, err)
	}

	// Validate category-scoped conditions and collect distinct BudgetIDs (single-pass).
	budgetIDs := make(map[string]struct{})
	for _, condition := range b.Conditions {
		if !strings.HasPrefix(condition.Field, "budget_category.") {
			continue
		}
		if condition.CategoryID == "" {
			return fmt.Errorf("%w: condition field %q requires a category_id", ErrInvalidBoost, condition.Field)
		}
		if condition.BudgetID == "" {
			return fmt.Errorf("%w: condition field %q requires a budget_id", ErrInvalidBoost, condition.Field)
		}
		budgetIDs[condition.BudgetID] = struct{}{}
	}

	// Multi-budget check: rule may bind at most one budget.
	if len(budgetIDs) > 1 {
		return fmt.Errorf("%w: a boost may bind at most one budget", ErrInvalidBoost)
	}
	return nil
}

// validateActions validates the actions JSON based on rule type.
func (b *Boost) validateActions() error {
	if len(b.Actions) == 0 {
		return errors.New("actions is required")
	}

	switch b.BoostType {
	case BoostTypeTransferSuggestion:
		return b.validateTransferAction()
	case BoostTypeBudgetAlert:
		return b.validateAlertAction()
	case BoostTypeCategoryRollover:
		return b.validateRolloverAction()
	case BoostTypeSavingsGoal:
		return b.validateSavingsGoalAction()
	}

	return nil
}

func (b *Boost) validateTransferAction() error {
	action, err := b.ParseTransferAction()
	if err != nil {
		return errors.New("invalid transfer action: " + err.Error())
	}

	if action.FromAccountID == "" {
		return errors.New("from_account_id is required for transfer_suggestion")
	}
	if action.ToAccountID == "" {
		return errors.New("to_account_id is required for transfer_suggestion")
	}

	// 'excess' and 'remaining' derive the transfer amount from triggerData at runtime, so amount=0 is valid.
	// 'threshold' is a legacy income-based type (transfer income minus threshold); kept for backwards compatibility.
	validAmountTypes := map[string]bool{"fixed": true, "percentage": true, "excess": true, "remaining": true, "threshold": true}
	amountType := action.AmountType
	if amountType == "" {
		amountType = "fixed"
	}
	if !validAmountTypes[amountType] {
		return errors.New("amount_type must be 'fixed', 'percentage', 'excess', 'remaining', or 'threshold' for transfer_suggestion")
	}
	// Only require a positive amount for types that use it as the direct transfer value.
	if (amountType == "fixed" || amountType == "percentage") && action.Amount <= 0 {
		return errors.New("amount must be positive for transfer_suggestion")
	}
	// GAP AG: percentage cannot exceed 100 (over-suggestion guard).
	if amountType == "percentage" && action.Amount > 100 {
		return errors.New("percentage must be between 0 and 100 for transfer_suggestion")
	}
	// Negative threshold produces nonsensical over-transfer amounts; reject at validation time.
	if (amountType == "excess" || amountType == "threshold") && action.Threshold < 0 {
		return errors.New("threshold must be non-negative for transfer_suggestion")
	}

	return nil
}

func (b *Boost) validateAlertAction() error {
	action, err := b.ParseAlertAction()
	if err != nil {
		return errors.New("invalid alert action: " + err.Error())
	}

	if len(action.NotificationChannels) == 0 {
		return errors.New("notification_channels is required for budget_alert")
	}

	validThresholdTypes := map[string]bool{"percentage": true, "amount": true}
	if !validThresholdTypes[action.ThresholdType] {
		return errors.New("threshold_type must be 'percentage' or 'amount' for budget_alert")
	}

	return nil
}

func (b *Boost) validateRolloverAction() error {
	action, err := b.ParseRolloverAction()
	if err != nil {
		return errors.New("invalid rollover action: " + err.Error())
	}

	if action.SourceBudgetCategoryID == "" {
		return errors.New("source_budget_category_id is required for category_rollover")
	}

	// "all" is accepted as a legacy alias for "full" (executor treats them identically).
	validRolloverTypes := map[string]bool{"all": true, "full": true, "capped": true, "percentage": true}
	if !validRolloverTypes[action.RolloverType] {
		return errors.New("rollover_type must be 'full', 'capped', or 'percentage' for category_rollover")
	}

	if action.RolloverType == "capped" && action.CapAmount == nil {
		return errors.New("cap_amount is required when rollover_type is 'capped'")
	}

	if action.RolloverType == "percentage" && action.Percentage == nil {
		return errors.New("percentage is required when rollover_type is 'percentage'")
	}

	// GAP AG: rollover percentage must be in (0, 100] to prevent over-rollover.
	if action.RolloverType == "percentage" && action.Percentage != nil &&
		(*action.Percentage <= 0 || *action.Percentage > 100) {
		return errors.New("percentage must be between 0 (exclusive) and 100 (inclusive) for category_rollover")
	}

	return nil
}

func (b *Boost) validateSavingsGoalAction() error {
	action, err := b.ParseSavingsGoalAction()
	if err != nil {
		return errors.New("invalid savings goal action: " + err.Error())
	}

	if action.SavingsGoalID == "" {
		return errors.New("savings_goal_id is required for savings_goal")
	}

	if action.FromAccountID == "" {
		return errors.New("from_account_id is required for savings_goal")
	}

	validAmountTypes := map[string]bool{"fixed": true, "percentage": true, "remaining": true}
	if !validAmountTypes[action.AmountType] {
		return errors.New("amount_type must be 'fixed', 'percentage', or 'remaining' for savings_goal")
	}

	if action.AmountType == "fixed" && action.Amount <= 0 {
		return errors.New("amount must be positive when amount_type is 'fixed'")
	}

	if action.AmountType == "percentage" && (action.Percentage == nil || *action.Percentage <= 0 || *action.Percentage > 100) {
		return errors.New("percentage must be between 0 and 100 when amount_type is 'percentage'")
	}

	// GAP AG: a negative threshold for the 'remaining' type would cause calculateSavingsGoalAmount
	// to over-suggest (remaining + |threshold| instead of remaining - threshold).
	if action.AmountType == "remaining" && action.Threshold < 0 {
		return errors.New("threshold must be non-negative when amount_type is 'remaining' for savings_goal")
	}

	return nil
}

// ParseTransferAction parses the Actions JSON as a TransferAction.
func (b *Boost) ParseTransferAction() (*TransferAction, error) {
	var action TransferAction
	if err := json.Unmarshal(b.Actions, &action); err != nil {
		return nil, err
	}
	return &action, nil
}

// ParseAlertAction parses the Actions JSON as an AlertAction.
func (b *Boost) ParseAlertAction() (*AlertAction, error) {
	var action AlertAction
	if err := json.Unmarshal(b.Actions, &action); err != nil {
		return nil, err
	}
	return &action, nil
}

// ParseRolloverAction parses the Actions JSON as a RolloverAction.
func (b *Boost) ParseRolloverAction() (*RolloverAction, error) {
	var action RolloverAction
	if err := json.Unmarshal(b.Actions, &action); err != nil {
		return nil, err
	}
	return &action, nil
}

// ParseSavingsGoalAction parses the Actions JSON as a SavingsGoalAction.
func (b *Boost) ParseSavingsGoalAction() (*SavingsGoalAction, error) {
	var action SavingsGoalAction
	if err := json.Unmarshal(b.Actions, &action); err != nil {
		return nil, err
	}
	return &action, nil
}

// =============================================================================
// BoostExecution
// =============================================================================

// BoostExecution tracks a single execution of a boost.
type BoostExecution struct {
	ID string `json:"id" db:"id"`
	// BoostID is a POINTER because fk_boost_executions_boost_id is ON DELETE SET NULL since Issue
	// #1569 Task 14 (migration 000164). NULL means "the boost that ran has since been purged",
	// never "no boost" -- every row was written for a real one. BoostName and DefinitionSnapshot
	// are what a reader shows in its place.
	//
	// ⚠ NON-NULL DOES NOT MEAN THE BOOST IS STILL THERE. boostsRepository.Delete TOMBSTONES
	// (deleted_at), so a normally-deleted boost's executions keep this pointer set and the row
	// still looks parented. A reader deciding "came from a deleted boost" must compare against the
	// LIVE boost listing, not against nil -- see BoostAutomationHistoryPanel.tsx, which does.
	BoostID *string `json:"boost_id,omitempty" db:"boost_id"`
	// UserID is denormalised onto the execution row. It is the row's own ON DELETE CASCADE path to
	// users (so GDPR erasure still reaches it once boost_id can be NULL) AND the key GetByUserID
	// reads it by (so an orphan is not invisible to every list in the app).
	UserID string `json:"user_id" db:"user_id"`
	// BoostName and DefinitionSnapshot are written once, at execution time, and never updated.
	// They record the definition AS IT WAS THEN -- editing or deleting the boost afterwards does
	// not rewrite history.
	BoostName           *string         `json:"boost_name,omitempty" db:"boost_name"`
	DefinitionSnapshot  json.RawMessage `json:"definition_snapshot,omitempty" db:"-"`
	Status              ExecutionStatus `json:"status" db:"status"`
	StartedAt           time.Time       `json:"started_at" db:"started_at"`
	CompletedAt         *time.Time      `json:"completed_at,omitempty" db:"completed_at"`
	ExecutionDurationMs *int            `json:"execution_duration_ms,omitempty" db:"execution_duration_ms"`
	TriggerContext      json.RawMessage `json:"trigger_context" db:"-"` // What triggered this execution
	InputData           json.RawMessage `json:"input_data,omitempty" db:"-"`
	OutputData          json.RawMessage `json:"output_data,omitempty" db:"-"`
	ErrorCode           *string         `json:"error_code,omitempty" db:"error_code"`
	ErrorMessage        *string         `json:"error_message,omitempty" db:"error_message"`
	RetryCount          int             `json:"retry_count" db:"retry_count"`
	MaxRetries          int             `json:"max_retries" db:"max_retries"`
}

// MarkComplete marks the execution as completed and calculates duration.
func (e *BoostExecution) MarkComplete() {
	now := time.Now()
	e.Status = ExecutionStatusCompleted
	e.CompletedAt = &now
	durationMs := int(now.Sub(e.StartedAt).Milliseconds())
	e.ExecutionDurationMs = &durationMs
}

// MarkFailed marks the execution as failed with error details.
func (e *BoostExecution) MarkFailed(errorCode, errorMessage string) {
	now := time.Now()
	e.Status = ExecutionStatusFailed
	e.CompletedAt = &now
	e.ErrorCode = &errorCode
	e.ErrorMessage = &errorMessage
	durationMs := int(now.Sub(e.StartedAt).Milliseconds())
	e.ExecutionDurationMs = &durationMs
}

// =============================================================================
// TransferSuggestion
// =============================================================================

// TransferSuggestion represents a suggested money transfer.
// IMPORTANT: No actual money movement - suggestions only (per CLAUDE.md).
type TransferSuggestion struct {
	ID                   string           `json:"id" db:"id"`
	UserID               string           `json:"user_id" db:"user_id"`
	BoostID               *string          `json:"boost_id,omitempty" db:"boost_id"`
	BoostExecutionID      *string          `json:"boost_execution_id,omitempty" db:"boost_execution_id"`
	FromAccountID        *string          `json:"from_account_id,omitempty" db:"from_account_id"`
	ToAccountID          *string          `json:"to_account_id,omitempty" db:"to_account_id"`
	FromUserAccountID    *string          `json:"from_user_account_id,omitempty" db:"from_user_account_id"` // Stable reference surviving re-links (Issue #1109)
	ToUserAccountID      *string          `json:"to_user_account_id,omitempty" db:"to_user_account_id"`     // Stable reference surviving re-links (Issue #1109)
	Amount               float64          `json:"amount" db:"amount"`
	CurrencyCode         string           `json:"currency_code" db:"currency_code"`
	Reason               *string          `json:"reason,omitempty" db:"reason"`
	Source               SuggestionSource `json:"source" db:"source"`
	Status               SuggestionStatus `json:"status" db:"status"`
	ExpiresAt            *time.Time       `json:"expires_at,omitempty"             db:"expires_at"`
	UserActionAt         *time.Time       `json:"user_action_at,omitempty"         db:"user_action_at"`
	UserNotes            *string          `json:"user_notes,omitempty"             db:"user_notes"`
	CompletedAt          *time.Time       `json:"completed_at,omitempty"           db:"completed_at"`
	MatchedTransactionID *string          `json:"matched_transaction_id,omitempty" db:"matched_transaction_id"`
	ActualAmount         *float64         `json:"actual_amount,omitempty"          db:"actual_amount"`
	CompletionSource     *string          `json:"completion_source,omitempty"      db:"completion_source"`
	CreatedAt            time.Time        `json:"created_at" db:"created_at"`
	UpdatedAt            time.Time        `json:"updated_at" db:"updated_at"`

	// Enriched fields — populated by GetByUserIDStatuses (JOIN-enriched list only).
	// Nil when the suggestion was fetched via a single-row lookup (GetByID, etc.)
	// or when the related row does not exist.
	ToAccountName        *string    `json:"to_account_name,omitempty"        db:"-"`
	FromAccountName      *string    `json:"from_account_name,omitempty"      db:"-"`
	BoostName            *string    `json:"boost_name,omitempty"             db:"-"`
	MatchedTransactionAt *time.Time `json:"matched_transaction_at,omitempty" db:"-"`

	// MatchState is a server-derived enum describing the match lifecycle for the UI.
	// Set by the list handler BEFORE maskReservation clears MatchedTransactionID.
	// Values: "completed", "confirmable", "unmatched".
	// Omitted (empty/nil) for pending, rejected, and expired suggestions.
	MatchState *string `json:"match_state,omitempty" db:"-" enum:"completed,confirmable,unmatched"`
}

// Validate checks that the transfer suggestion has all required fields and valid values.
func (s *TransferSuggestion) Validate() error {
	if s.UserID == "" {
		return errors.New("user_id is required")
	}

	if s.Amount <= 0 {
		return errors.New("amount must be positive")
	}

	if !s.Source.IsValid() {
		return errors.New("invalid source: " + string(s.Source))
	}

	if !s.Status.IsValid() {
		return errors.New("invalid status: " + string(s.Status))
	}

	return nil
}

// Accept marks the suggestion as accepted by the user.
func (s *TransferSuggestion) Accept(notes *string) {
	now := time.Now()
	s.Status = SuggestionStatusAccepted
	s.UserActionAt = &now
	s.UserNotes = notes
}

// Reject marks the suggestion as rejected by the user.
func (s *TransferSuggestion) Reject(notes *string) {
	now := time.Now()
	s.Status = SuggestionStatusRejected
	s.UserActionAt = &now
	s.UserNotes = notes
}

// Complete marks the suggestion as completed after a transfer was detected or confirmed.
// completionSource must be "exact_match" or "user_confirmed".
func (s *TransferSuggestion) Complete(matchedTransactionID string, actualAmount float64, completionSource string) error {
	if s.Status == SuggestionStatusCompleted {
		return ErrTransferSuggestionAlreadyActioned
	}
	now := time.Now().UTC()
	s.Status = SuggestionStatusCompleted
	s.CompletedAt = &now
	s.MatchedTransactionID = &matchedTransactionID
	s.ActualAmount = &actualAmount
	s.CompletionSource = &completionSource
	return nil
}

// ComputeMatchState returns the server-derived match lifecycle state string for this
// suggestion. The handler MUST call this BEFORE maskReservation clears MatchedTransactionID.
//
// Returned values:
//   - "completed"   — Status is completed
//   - "confirmable" — Status is accepted and a fuzzy-match reservation is held
//   - "unmatched"   — Status is accepted with no reservation yet
//   - ""            — All other statuses (pending/rejected/expired) → field omitted
func (s *TransferSuggestion) ComputeMatchState() string {
	switch s.Status {
	case SuggestionStatusCompleted:
		return "completed"
	case SuggestionStatusAccepted:
		if s.MatchedTransactionID != nil {
			return "confirmable"
		}
		return "unmatched"
	default:
		return "" // pending/rejected/expired → omitted
	}
}

// =============================================================================
// Request/Response Types
// =============================================================================

// CreateBoostRequest represents the request to create a new boost.
type CreateBoostRequest struct {
	Name           string                    `json:"name" validate:"required,max=100"`
	Description    *string                   `json:"description,omitempty"`
	Priority       int                       `json:"priority,omitempty"`
	BoostType       BoostType                  `json:"boost_type" validate:"required"`
	TriggerConfig  TriggerConfig             `json:"trigger_config" validate:"required"`
	Conditions     []BoostCondition `json:"conditions,omitempty"`
	ConditionLogic string                    `json:"condition_logic,omitempty" validate:"omitempty,oneof=AND OR"`
	Actions        json.RawMessage           `json:"actions" validate:"required"`
}

// UpdateBoostRequest represents the request to update a boost.
type UpdateBoostRequest struct {
	Name           *string                    `json:"name,omitempty" validate:"omitempty,max=100"`
	Description    *string                    `json:"description,omitempty"`
	Enabled        *bool                      `json:"enabled,omitempty"`
	Priority       *int                       `json:"priority,omitempty"`
	TriggerConfig  *TriggerConfig             `json:"trigger_config,omitempty"`
	Conditions     *[]BoostCondition `json:"conditions,omitempty"`
	ConditionLogic *string                    `json:"condition_logic,omitempty" validate:"omitempty,oneof=AND OR"`
	Actions        *json.RawMessage           `json:"actions,omitempty"`
}

// ToggleBoostRequest represents the request to enable/disable a boost.
type ToggleBoostRequest struct {
	Enabled bool `json:"enabled"`
}

// =============================================================================
// BoostTestResult (Issue #38: Test Boost Execution)
// =============================================================================

// BoostTestResult represents the result of a dry-run boost execution.
// Used by the POST /api/v1/rules/:id/test endpoint to preview what would happen
// without persisting any changes.
type BoostTestResult struct {
	BoostID           string                 `json:"boost_id"`
	BoostName         string                 `json:"boost_name"`
	BoostType         BoostType               `json:"boost_type"`
	ConditionsMet    bool                   `json:"conditions_met"`
	ConditionResults []ConditionEvalResult  `json:"condition_results"`
	SimulatedAction  map[string]interface{} `json:"simulated_action,omitempty"`
	TriggerDataUsed  map[string]interface{} `json:"trigger_data_used"`
	Error            string                 `json:"error,omitempty"`
}

// ConditionEvalResult represents the evaluation result of a single condition.
type ConditionEvalResult struct {
	Field         string      `json:"field"`
	Operator      string      `json:"operator"`
	Value         interface{} `json:"value"`
	ValueDisplay  string      `json:"value_display,omitempty"`
	Actual        interface{} `json:"actual"`
	ActualDisplay string      `json:"actual_display,omitempty"`
	Met           bool        `json:"met"`
}

// =============================================================================
// BoostExecutionsResponse (Issue #38: Execution History)
// =============================================================================

// BoostExecutionsResponse represents a paginated list of boost executions.
type BoostExecutionsResponse struct {
	Executions []*BoostExecution `json:"executions"`
	Total      int              `json:"total"`
	Limit      int              `json:"limit"`
	Offset     int              `json:"offset"`
}

// =============================================================================
// Error Constants
// =============================================================================

// Boost-related errors.
var (
	ErrBoostNotFound                     = errors.New("boost not found")
	ErrBoostAlreadyExists                = errors.New("boost with this name already exists")
	ErrTransferSuggestionNotFound        = errors.New("transfer suggestion not found")
	ErrTransferSuggestionAlreadyActioned = errors.New("transfer suggestion has already been accepted or rejected")
	ErrBoostExecutionNotFound            = errors.New("boost execution not found")

	// Issue #1270: GAP-L — typed sentinels so the error mapper can return the correct HTTP status.
	// ErrInvalidBoost is returned (wrapped) when boost.Validate() fails due to bad client input
	// (unknown event, unsupported operator, disallowed condition field). Maps to HTTP 400.
	ErrInvalidBoost = errors.New("invalid boost")
	// ErrBoostConditionForbidden is returned when a condition's CategoryID does not belong to the
	// requesting user (IDOR prevention). Maps to HTTP 403.
	ErrBoostConditionForbidden = errors.New("boost condition access denied")

	// ErrRolloverSourceBecameSubAllocation is returned by executeCategoryRollover (Issue #1630
	// B6) when a category_rollover boost's source category IS a sub-allocation of another
	// budgeted category at EXECUTION time. boost_service.go's CreateBoost/UpdateBoost already
	// refuse this shape up front, but a source that was a valid top-level category when the
	// boost was created can become a sub-allocation LATER if the hierarchy changes underneath
	// it (a reparent, or a new parent category budgeted after the fact) — this is the
	// execution-time half of the same fix, for exactly that case. ExecuteBoost
	// (boost_execution_service.go) treats this sentinel as a SKIP, not a failure: it must not
	// increment the boost's execution_count or touch last_successful, the same way "boost
	// disabled" and "conditions not met" are already skipped rather than failed.
	ErrRolloverSourceBecameSubAllocation = errors.New("category_rollover source category is a sub-allocation of another budgeted category")
)
