// Categorization rule types matching backend domain models and API schemas
// Issue #346: Custom Transaction Categorization Rules

/**
 * Condition types supported by the rule engine
 */
export type ConditionType =
  | 'merchant_contains'
  | 'merchant_equals'
  | 'merchant_starts_with'
  | 'merchant_ends_with'
  | 'amount_greater_than'
  | 'amount_less_than'
  | 'amount_equals'
  | 'amount_range'
  | 'account_id'
  | 'account_type'
  | 'payment_channel'
  | 'existing_category'
  | 'date_range'

/**
 * Human-readable labels for condition types
 */
export const CONDITION_TYPE_LABELS: Record<ConditionType, string> = {
  merchant_contains: 'Merchant contains',
  merchant_equals: 'Merchant equals',
  merchant_starts_with: 'Merchant starts with',
  merchant_ends_with: 'Merchant ends with',
  amount_greater_than: 'Amount greater than',
  amount_less_than: 'Amount less than',
  amount_equals: 'Amount equals',
  amount_range: 'Amount between',
  account_id: 'Account is',
  account_type: 'Account type is',
  payment_channel: 'Payment channel is',
  existing_category: 'Current category is',
  date_range: 'Date between',
}

/**
 * A single condition within a categorization rule
 */
export interface RuleCondition {
  type: ConditionType
  value: string | number | string[] | AmountRangeValue | DateRangeValue
  operator?: string // for amount comparisons
}

/**
 * Value type for amount_range condition
 */
export interface AmountRangeValue {
  min: number
  max: number
}

/**
 * Value type for date_range condition
 */
export interface DateRangeValue {
  start?: string // ISO date string
  end?: string // ISO date string
}

/**
 * Condition logic options
 */
export type ConditionLogic = 'AND' | 'OR'

/**
 * Tag behavior options
 */
export type TagBehavior = 'add' | 'replace'

/**
 * Rule source options
 */
export type RuleSource = 'manual' | 'ai' | 'template'

/**
 * User-defined categorization rule
 * Matches backend domain.CategorizationRule schema
 */
export interface CategorizationRule {
  id: string
  user_id: string
  name: string
  description?: string | null
  enabled: boolean
  priority: number
  conditions: RuleCondition[]
  condition_logic: ConditionLogic
  target_category?: string[] | null
  target_tags?: string[] | null
  tag_behavior: TagBehavior
  source: RuleSource
  match_count: number
  override_count: number
  last_matched_at?: string | null // ISO 8601 date-time
  created_at: string // ISO 8601 date-time
  updated_at: string // ISO 8601 date-time
}

/**
 * Request to create a new categorization rule
 */
export interface CreateRuleRequest {
  name: string
  description?: string
  priority?: number
  conditions: RuleCondition[]
  condition_logic?: ConditionLogic
  target_category?: string[]
  target_tags?: string[]
  tag_behavior?: TagBehavior
}

/**
 * Request to update an existing categorization rule
 */
export interface UpdateRuleRequest {
  name?: string
  description?: string
  enabled?: boolean
  priority?: number
  conditions?: RuleCondition[]
  condition_logic?: ConditionLogic
  target_category?: string[]
  target_tags?: string[]
  tag_behavior?: TagBehavior
}

/**
 * Request to toggle rule enabled status
 */
export interface ToggleRuleRequest {
  enabled: boolean
}

/**
 * Hook result type for useAutoCategorizationRules
 */
export interface UseAutoCategorizationRulesResult {
  rules: CategorizationRule[]
  loading: boolean
  error: string | null
  createRule: (request: CreateRuleRequest) => Promise<CategorizationRule>
  updateRule: (ruleId: string, request: UpdateRuleRequest) => Promise<CategorizationRule>
  deleteRule: (ruleId: string) => Promise<void>
  toggleRule: (ruleId: string, enabled: boolean) => Promise<CategorizationRule>
  refresh: () => Promise<void>
}
