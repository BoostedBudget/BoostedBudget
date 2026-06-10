/**
 * Types for Boosts Feature
 *
 * Issue #1283: Rename rules → boosts (Phase 5)
 * Aligned with backend domain/boost.go
 */

// Re-export generated types for convenience
export type {
  Boost,
  BoostCondition,
  TriggerConfig,
  CreateBoostRequest,
  UpdateBoostRequest,
  BoostExecution,
  BoostTestResult,
} from '@boostedbudget/api-client/models'

// =============================================================================
// Boost Types (matches backend BoostType enum)
// =============================================================================

export type BoostType =
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
  | 'contains'

export type ConditionLogic = 'AND' | 'OR'

export interface BoostConditionLocal {
  field: string
  operator: ConditionOperator
  value: unknown
  category_id?: string
  budget_id?: string
}

// =============================================================================
// Action Types (matches backend action structs)
// =============================================================================

export type AmountType = 'fixed' | 'percentage' | 'excess' | 'remaining'
export type ThresholdType = 'percentage' | 'amount'
export type RolloverType = 'full' | 'capped' | 'percentage'
export type NotificationChannel = 'email' | 'push' | 'in_app'

/**
 * TransferAction - for transfer_suggestion boosts
 * Creates transfer suggestions between accounts
 */
export interface TransferAction {
  from_account_id: string
  to_account_id: string
  /** Stable identifier that survives Plaid re-link (Issue #1109) */
  from_user_account_id?: string
  /** Stable identifier that survives Plaid re-link (Issue #1109) */
  to_user_account_id?: string
  amount_type: 'fixed' | 'percentage' | 'excess'
  amount: number
  threshold?: number // For excess type: minimum before transfer
}

/**
 * AlertAction - for budget_alert boosts
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
 * RolloverAction - for category_rollover boosts
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
 * SavingsGoalAction - for savings_goal boosts
 * Creates transfer suggestions to contribute to savings goals
 */
export interface SavingsGoalAction {
  savings_goal_id: string
  from_account_id: string
  /** Stable identifier that survives Plaid re-link (Issue #1109) */
  from_user_account_id?: string
  amount_type: 'fixed' | 'percentage' | 'remaining'
  amount?: number // For fixed type
  percentage?: number // For percentage type
  threshold?: number // Minimum balance for remaining type
}

/**
 * Union type for all action types
 */
export type BoostAction =
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

export interface BoostFormState {
  // Basic fields
  name: string
  description: string
  priority: number
  enabled: boolean

  // Boost configuration
  boostType: BoostType | null
  triggerConfig: TriggerFormState
  conditions: BoostConditionLocal[]
  conditionLogic: ConditionLogic
  actions: BoostAction | null

  // Form metadata
  isDirty: boolean
  isValid: boolean
}

export interface BoostFormErrors {
  name?: string
  description?: string
  priority?: string
  boostType?: string
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

export interface BoostListItem {
  id: string
  name: string
  boostType: BoostType
  enabled: boolean
  executionCount: number
  lastExecutedAt?: string
  lastSuccessfulAt?: string
  nextScheduledRun?: string // Computed from cron
  triggerType: TriggerType
}
