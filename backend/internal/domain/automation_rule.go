// Package domain contains domain models for the automation rules engine.
// Issue #35: Rules Engine Core - supports transfer suggestions, budget alerts, and category rollovers.
// Subject to subscription tier limits (free: 1 rule, premium_lite: 5, premium: 15, premium+: unlimited).
// This is SEPARATE from categorization rules (Issue #346) which handle transaction auto-categorization.
package domain

import (
	"encoding/json"
	"errors"
	"time"
)

// =============================================================================
// Rule Types
// =============================================================================

// RuleType represents the type of automation rule.
type RuleType string

// Supported automation rule types (matches database CHECK constraint).
const (
	RuleTypeTransferSuggestion RuleType = "transfer_suggestion"
	RuleTypeBudgetAlert        RuleType = "budget_alert"
	RuleTypeCategoryRollover   RuleType = "category_rollover"
	RuleTypeSavingsGoal        RuleType = "savings_goal" // Issue #36: Savings goal contributions
)

// validRuleTypes is a set of all valid rule types.
var validRuleTypes = map[RuleType]bool{
	RuleTypeTransferSuggestion: true,
	RuleTypeBudgetAlert:        true,
	RuleTypeCategoryRollover:   true,
	RuleTypeSavingsGoal:        true,
}

// IsValid checks if the rule type is valid.
func (rt RuleType) IsValid() bool {
	return validRuleTypes[rt]
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

// validTriggerTypes is a set of all valid trigger types.
var validTriggerTypes = map[TriggerType]bool{
	TriggerTypeEvent:    true,
	TriggerTypeSchedule: true,
}

// IsValid checks if the trigger type is valid.
func (tt TriggerType) IsValid() bool {
	return validTriggerTypes[tt]
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
	SuggestionStatusPending  SuggestionStatus = "pending"
	SuggestionStatusAccepted SuggestionStatus = "accepted"
	SuggestionStatusRejected SuggestionStatus = "rejected"
	SuggestionStatusExpired  SuggestionStatus = "expired"
)

// validSuggestionStatuses is a set of all valid suggestion statuses.
var validSuggestionStatuses = map[SuggestionStatus]bool{
	SuggestionStatusPending:  true,
	SuggestionStatusAccepted: true,
	SuggestionStatusRejected: true,
	SuggestionStatusExpired:  true,
}

// IsValid checks if the suggestion status is valid.
func (ss SuggestionStatus) IsValid() bool {
	return validSuggestionStatuses[ss]
}

// SuggestionSource represents the source of a transfer suggestion.
type SuggestionSource string

// Supported suggestion sources (matches database CHECK constraint).
const (
	SuggestionSourceRule   SuggestionSource = "rule"
	SuggestionSourceManual SuggestionSource = "manual"
	SuggestionSourceAI     SuggestionSource = "ai"
)

// validSuggestionSources is a set of all valid suggestion sources.
var validSuggestionSources = map[SuggestionSource]bool{
	SuggestionSourceRule:   true,
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
	case TriggerTypeSchedule:
		if tc.Cron == "" {
			return errors.New("cron expression is required for schedule trigger type")
		}
	}

	return nil
}

// =============================================================================
// Rule Condition
// =============================================================================

// AutomationRuleCondition represents a condition that must be met for a rule to fire.
type AutomationRuleCondition struct {
	Field    string            `json:"field"`    // Field to evaluate: account.balance, budget.percent_used, budget_category.remaining, etc.
	Operator ConditionOperator `json:"operator"` // Comparison operator
	Value    interface{}       `json:"value"`    // Value to compare against
}

// =============================================================================
// Action Types (JSONB parsed structures)
// =============================================================================

// TransferAction defines the action for transfer_suggestion rules.
type TransferAction struct {
	FromAccountID string  `json:"from_account_id"`
	ToAccountID   string  `json:"to_account_id"`
	AmountType    string  `json:"amount_type,omitempty"` // fixed, percentage, excess
	Amount        float64 `json:"amount"`
	Threshold     float64 `json:"threshold,omitempty"` // For excess type: minimum before transfer
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
	SavingsGoalID string   `json:"savings_goal_id"`         // Target savings goal
	FromAccountID string   `json:"from_account_id"`         // Source account for transfer
	AmountType    string   `json:"amount_type"`             // fixed, percentage, remaining
	Amount        float64  `json:"amount,omitempty"`        // For fixed amount type
	Percentage    *float64 `json:"percentage,omitempty"`    // For percentage amount type
	Threshold     float64  `json:"threshold,omitempty"`     // Minimum balance threshold for remaining type
}

// =============================================================================
// AutomationRule
// =============================================================================

// AutomationRule represents a user-defined automation rule.
// Subject to subscription tier limits (Issue #35).
type AutomationRule struct {
	ID               string                    `json:"id" db:"id"`
	UserID           string                    `json:"user_id" db:"user_id"`
	Name             string                    `json:"name" db:"name"`
	Description      *string                   `json:"description,omitempty" db:"description"`
	Enabled          bool                      `json:"enabled" db:"enabled"`
	Priority         int                       `json:"priority" db:"priority"` // Higher = evaluated first
	RuleType         RuleType                  `json:"rule_type" db:"rule_type"`
	TriggerConfig    TriggerConfig             `json:"trigger_config" db:"-"` // Stored as JSONB
	Conditions       []AutomationRuleCondition `json:"conditions" db:"-"`     // Stored as JSONB
	ConditionLogic   string                    `json:"condition_logic" db:"condition_logic"`
	Actions          json.RawMessage           `json:"actions" db:"-"` // Stored as JSONB, structure depends on rule_type
	ExecutionCount   int64                     `json:"execution_count" db:"execution_count"`
	LastExecutedAt   *time.Time                `json:"last_executed_at,omitempty" db:"last_executed_at"`
	LastSuccessfulAt *time.Time                `json:"last_successful_at,omitempty" db:"last_successful_at"`
	CreatedAt        time.Time                 `json:"created_at" db:"created_at"`
	UpdatedAt        time.Time                 `json:"updated_at" db:"updated_at"`
}

// Validate checks that the automation rule has all required fields and valid values.
func (r *AutomationRule) Validate() error {
	if r.UserID == "" {
		return errors.New("user_id is required")
	}

	if r.Name == "" {
		return errors.New("name is required")
	}

	if len(r.Name) > 100 {
		return errors.New("name must be 100 characters or less")
	}

	if !r.RuleType.IsValid() {
		return errors.New("invalid rule_type: " + string(r.RuleType))
	}

	// Validate trigger config
	if err := r.TriggerConfig.Validate(); err != nil {
		return err
	}

	// Validate condition logic (default to AND if empty)
	if r.ConditionLogic != "" && r.ConditionLogic != "AND" && r.ConditionLogic != "OR" {
		return errors.New("condition_logic must be AND or OR")
	}

	// Validate actions based on rule type
	return r.validateActions()
}

// validateActions validates the actions JSON based on rule type.
func (r *AutomationRule) validateActions() error {
	if len(r.Actions) == 0 {
		return errors.New("actions is required")
	}

	switch r.RuleType {
	case RuleTypeTransferSuggestion:
		return r.validateTransferAction()
	case RuleTypeBudgetAlert:
		return r.validateAlertAction()
	case RuleTypeCategoryRollover:
		return r.validateRolloverAction()
	case RuleTypeSavingsGoal:
		return r.validateSavingsGoalAction()
	}

	return nil
}

func (r *AutomationRule) validateTransferAction() error {
	action, err := r.ParseTransferAction()
	if err != nil {
		return errors.New("invalid transfer action: " + err.Error())
	}

	if action.FromAccountID == "" {
		return errors.New("from_account_id is required for transfer_suggestion")
	}
	if action.ToAccountID == "" {
		return errors.New("to_account_id is required for transfer_suggestion")
	}
	if action.Amount <= 0 {
		return errors.New("amount must be positive for transfer_suggestion")
	}

	return nil
}

func (r *AutomationRule) validateAlertAction() error {
	action, err := r.ParseAlertAction()
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

func (r *AutomationRule) validateRolloverAction() error {
	action, err := r.ParseRolloverAction()
	if err != nil {
		return errors.New("invalid rollover action: " + err.Error())
	}

	if action.SourceBudgetCategoryID == "" {
		return errors.New("source_budget_category_id is required for category_rollover")
	}

	validRolloverTypes := map[string]bool{"full": true, "capped": true, "percentage": true}
	if !validRolloverTypes[action.RolloverType] {
		return errors.New("rollover_type must be 'full', 'capped', or 'percentage' for category_rollover")
	}

	if action.RolloverType == "capped" && action.CapAmount == nil {
		return errors.New("cap_amount is required when rollover_type is 'capped'")
	}

	if action.RolloverType == "percentage" && action.Percentage == nil {
		return errors.New("percentage is required when rollover_type is 'percentage'")
	}

	return nil
}

func (r *AutomationRule) validateSavingsGoalAction() error {
	action, err := r.ParseSavingsGoalAction()
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

	return nil
}

// ParseTransferAction parses the Actions JSON as a TransferAction.
func (r *AutomationRule) ParseTransferAction() (*TransferAction, error) {
	var action TransferAction
	if err := json.Unmarshal(r.Actions, &action); err != nil {
		return nil, err
	}
	return &action, nil
}

// ParseAlertAction parses the Actions JSON as an AlertAction.
func (r *AutomationRule) ParseAlertAction() (*AlertAction, error) {
	var action AlertAction
	if err := json.Unmarshal(r.Actions, &action); err != nil {
		return nil, err
	}
	return &action, nil
}

// ParseRolloverAction parses the Actions JSON as a RolloverAction.
func (r *AutomationRule) ParseRolloverAction() (*RolloverAction, error) {
	var action RolloverAction
	if err := json.Unmarshal(r.Actions, &action); err != nil {
		return nil, err
	}
	return &action, nil
}

// ParseSavingsGoalAction parses the Actions JSON as a SavingsGoalAction.
func (r *AutomationRule) ParseSavingsGoalAction() (*SavingsGoalAction, error) {
	var action SavingsGoalAction
	if err := json.Unmarshal(r.Actions, &action); err != nil {
		return nil, err
	}
	return &action, nil
}

// =============================================================================
// RuleExecution
// =============================================================================

// RuleExecution tracks a single execution of an automation rule.
type RuleExecution struct {
	ID                  string          `json:"id" db:"id"`
	RuleID              string          `json:"rule_id" db:"rule_id"`
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
func (e *RuleExecution) MarkComplete() {
	now := time.Now()
	e.Status = ExecutionStatusCompleted
	e.CompletedAt = &now
	durationMs := int(now.Sub(e.StartedAt).Milliseconds())
	e.ExecutionDurationMs = &durationMs
}

// MarkFailed marks the execution as failed with error details.
func (e *RuleExecution) MarkFailed(errorCode, errorMessage string) {
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
	ID              string           `json:"id" db:"id"`
	UserID          string           `json:"user_id" db:"user_id"`
	RuleID          *string          `json:"rule_id,omitempty" db:"rule_id"`
	RuleExecutionID *string          `json:"rule_execution_id,omitempty" db:"rule_execution_id"`
	FromAccountID   *string          `json:"from_account_id,omitempty" db:"from_account_id"`
	ToAccountID     *string          `json:"to_account_id,omitempty" db:"to_account_id"`
	Amount          float64          `json:"amount" db:"amount"`
	CurrencyCode    string           `json:"currency_code" db:"currency_code"`
	Reason          *string          `json:"reason,omitempty" db:"reason"`
	Source          SuggestionSource `json:"source" db:"source"`
	Status          SuggestionStatus `json:"status" db:"status"`
	ExpiresAt       *time.Time       `json:"expires_at,omitempty" db:"expires_at"`
	UserActionAt    *time.Time       `json:"user_action_at,omitempty" db:"user_action_at"`
	UserNotes       *string          `json:"user_notes,omitempty" db:"user_notes"`
	CreatedAt       time.Time        `json:"created_at" db:"created_at"`
	UpdatedAt       time.Time        `json:"updated_at" db:"updated_at"`
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

// =============================================================================
// Request/Response Types
// =============================================================================

// CreateAutomationRuleRequest represents the request to create a new automation rule.
type CreateAutomationRuleRequest struct {
	Name           string                    `json:"name" validate:"required,max=100"`
	Description    *string                   `json:"description,omitempty"`
	Priority       int                       `json:"priority,omitempty"`
	RuleType       RuleType                  `json:"rule_type" validate:"required"`
	TriggerConfig  TriggerConfig             `json:"trigger_config" validate:"required"`
	Conditions     []AutomationRuleCondition `json:"conditions,omitempty"`
	ConditionLogic string                    `json:"condition_logic,omitempty" validate:"omitempty,oneof=AND OR"`
	Actions        json.RawMessage           `json:"actions" validate:"required"`
}

// UpdateAutomationRuleRequest represents the request to update an automation rule.
type UpdateAutomationRuleRequest struct {
	Name           *string                    `json:"name,omitempty" validate:"omitempty,max=100"`
	Description    *string                    `json:"description,omitempty"`
	Enabled        *bool                      `json:"enabled,omitempty"`
	Priority       *int                       `json:"priority,omitempty"`
	TriggerConfig  *TriggerConfig             `json:"trigger_config,omitempty"`
	Conditions     *[]AutomationRuleCondition `json:"conditions,omitempty"`
	ConditionLogic *string                    `json:"condition_logic,omitempty" validate:"omitempty,oneof=AND OR"`
	Actions        *json.RawMessage           `json:"actions,omitempty"`
}

// ToggleAutomationRuleRequest represents the request to enable/disable a rule.
type ToggleAutomationRuleRequest struct {
	Enabled bool `json:"enabled"`
}

// =============================================================================
// Error Constants
// =============================================================================

// Automation rule-related errors.
var (
	ErrAutomationRuleNotFound      = errors.New("automation rule not found")
	ErrAutomationRuleAlreadyExists = errors.New("automation rule with this name already exists")
	ErrAutomationRuleLimitExceeded = errors.New("automation rule limit exceeded for subscription tier")
	ErrExecutionLimitExceeded      = errors.New("daily execution limit exceeded for subscription tier")
	ErrExecutionIntervalNotMet     = errors.New("minimum execution interval not met for subscription tier")
	ErrEventTriggersNotAllowed     = errors.New("event-based triggers not allowed for subscription tier (upgrade to premium)")
	ErrTransferSuggestionNotFound  = errors.New("transfer suggestion not found")
)
