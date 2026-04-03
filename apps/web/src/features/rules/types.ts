/**
 * Types for Automation Rules Feature
 *
 * Issue #39: Rule Builder UI
 * Aligned with backend domain/automation_rule.go
 */

// Re-export generated types for convenience
export type {
  AutomationRule,
  AutomationRuleCondition,
  TriggerConfig,
  CreateAutomationRuleRequest,
  UpdateAutomationRuleRequest,
  RuleExecution,
  RuleTestResult,
} from '@boostedbudget/api-client/models'

// =============================================================================
// Rule Types (matches backend RuleType enum)
// =============================================================================

export type RuleType =
  | 'transfer_suggestion'
  | 'budget_alert'
  | 'category_rollover'
  | 'savings_goal'

// =============================================================================
// Trigger Types
// =============================================================================

export type TriggerType = 'schedule' | 'event'

export type EventType =
  | 'budget_updated'
  | 'transaction_created'
  | 'balance_changed'
  | 'budget_period_end'

// =============================================================================
// Condition Types
// =============================================================================

export type ConditionOperator =
  | 'eq'
  | 'neq'
  | 'gt'
  | 'gte'
  | 'lt'
  | 'lte'
  | 'between'
  | 'contains'

export type ConditionLogic = 'AND' | 'OR'

export interface RuleCondition {
  field: string
  operator: ConditionOperator
  value: unknown
}

// =============================================================================
// Action Types (matches backend action structs)
// =============================================================================

export type AmountType = 'fixed' | 'percentage' | 'excess' | 'remaining'
export type ThresholdType = 'percentage' | 'amount'
export type RolloverType = 'full' | 'capped' | 'percentage'
export type NotificationChannel = 'email' | 'push' | 'in_app'

/**
 * TransferAction - for transfer_suggestion rules
 * Creates transfer suggestions between accounts
 */
export interface TransferAction {
  from_account_id: string
  to_account_id: string
  amount_type: 'fixed' | 'percentage' | 'excess'
  amount: number
  threshold?: number // For excess type: minimum before transfer
}

/**
 * AlertAction - for budget_alert rules
 * Sends notifications when budget thresholds are exceeded
 */
export interface AlertAction {
  notification_channels: NotificationChannel[]
  threshold_type: ThresholdType
  threshold_value: number
  budget_id?: string
  category_id?: string
  title_template?: string
  message_template?: string
}

/**
 * RolloverAction - for category_rollover rules
 * Moves unspent envelope funds to another category
 */
export interface RolloverAction {
  source_budget_category_id: string
  target_budget_category_id?: string | null // null = unallocated funds
  rollover_type: RolloverType
  cap_amount?: number // For capped type
  percentage?: number // For percentage type
}

/**
 * SavingsGoalAction - for savings_goal rules
 * Creates transfer suggestions to contribute to savings goals
 */
export interface SavingsGoalAction {
  savings_goal_id: string
  from_account_id: string
  amount_type: 'fixed' | 'percentage' | 'remaining'
  amount?: number // For fixed type
  percentage?: number // For percentage type
  threshold?: number // Minimum balance for remaining type
}

/**
 * Union type for all action types
 */
export type RuleAction =
  | TransferAction
  | AlertAction
  | RolloverAction
  | SavingsGoalAction

// =============================================================================
// Form State Types
// =============================================================================

export interface TriggerFormState {
  type: TriggerType
  // Schedule trigger fields
  cron?: string
  timezone?: string
  // Event trigger fields
  event?: EventType
  filters?: Record<string, unknown>
}

export interface RuleFormState {
  // Basic fields
  name: string
  description: string
  priority: number
  enabled: boolean

  // Rule configuration
  ruleType: RuleType | null
  triggerConfig: TriggerFormState
  conditions: RuleCondition[]
  conditionLogic: ConditionLogic
  actions: RuleAction | null

  // Form metadata
  isDirty: boolean
  isValid: boolean
}

export interface RuleFormErrors {
  name?: string
  description?: string
  priority?: string
  ruleType?: string
  trigger?: {
    cron?: string
    timezone?: string
    event?: string
  }
  conditions?: string
  conditionErrors?: Record<number, string>
  actions?: Record<string, string>
}

// =============================================================================
// API Helper Types
// =============================================================================

// =============================================================================
// List View Types
// =============================================================================

export type ListView = 'cards' | 'table'

export interface RuleListItem {
  id: string
  name: string
  ruleType: RuleType
  enabled: boolean
  executionCount: number
  lastExecutedAt?: string
  lastSuccessfulAt?: string
  nextScheduledRun?: string // Computed from cron
  triggerType: TriggerType
}
