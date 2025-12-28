/**
 * Constants for Automation Rules Feature
 *
 * Issue #39: Rule Builder UI
 */

import type {
  RuleType,
  TriggerType,
  EventType,
  ConditionOperator,
  NotificationChannel,
  RolloverType,
  AmountType,
} from './types'

// =============================================================================
// Rule Type Labels
// =============================================================================

export const RULE_TYPE_LABELS: Record<RuleType, string> = {
  transfer_suggestion: 'Transfer Suggestion',
  budget_alert: 'Budget Alert',
  category_rollover: 'Category Rollover',
  savings_goal: 'Savings Goal',
}

export const RULE_TYPE_DESCRIPTIONS: Record<RuleType, string> = {
  transfer_suggestion:
    'Suggest transfers between accounts based on balance or spending patterns',
  budget_alert:
    'Get notified when budget spending reaches a threshold',
  category_rollover:
    'Automatically roll over unspent envelope funds at period end',
  savings_goal:
    'Automate contributions to your savings goals',
}

export const RULE_TYPE_ICONS: Record<RuleType, string> = {
  transfer_suggestion: 'arrow-right-left',
  budget_alert: 'bell',
  category_rollover: 'refresh-cw',
  savings_goal: 'piggy-bank',
}

// =============================================================================
// Trigger Type Labels
// =============================================================================

export const TRIGGER_TYPE_LABELS: Record<TriggerType, string> = {
  schedule: 'Scheduled',
  event: 'Event-based',
}

export const TRIGGER_TYPE_DESCRIPTIONS: Record<TriggerType, string> = {
  schedule: 'Run on a fixed schedule (daily, weekly, monthly)',
  event: 'Run when something happens (transaction created, budget updated)',
}

// =============================================================================
// Event Type Labels
// =============================================================================

export const EVENT_TYPE_OPTIONS: Array<{ value: EventType; label: string; description: string }> = [
  {
    value: 'budget_updated',
    label: 'Budget Updated',
    description: 'Fires after budget recalculation',
  },
  {
    value: 'transaction_created',
    label: 'Transaction Created',
    description: 'Fires when new transactions are synced',
  },
  {
    value: 'balance_changed',
    label: 'Balance Changed',
    description: 'Fires when account balance changes',
  },
  {
    value: 'budget_period_end',
    label: 'Budget Period End',
    description: 'Fires when a budget period ends',
  },
]

// =============================================================================
// Cron Presets
// =============================================================================

export interface CronPreset {
  label: string
  description: string
  cron: string
}

export const CRON_PRESETS: CronPreset[] = [
  {
    label: 'Daily at 9am',
    description: 'Every day at 9:00 AM',
    cron: '0 9 * * *',
  },
  {
    label: 'Weekly (Monday)',
    description: 'Every Monday at 9:00 AM',
    cron: '0 9 * * 1',
  },
  {
    label: 'Monthly (1st)',
    description: 'First day of each month at 9:00 AM',
    cron: '0 9 1 * *',
  },
  {
    label: 'End of Month',
    description: 'Last day of each month at 11:59 PM',
    cron: '59 23 L * *',
  },
  {
    label: 'Bi-weekly',
    description: 'Every other Monday at 9:00 AM',
    cron: '0 9 1,15 * *',
  },
]

// Common timezones for the timezone selector
export const COMMON_TIMEZONES = [
  { value: 'America/New_York', label: 'Eastern Time (ET)' },
  { value: 'America/Chicago', label: 'Central Time (CT)' },
  { value: 'America/Denver', label: 'Mountain Time (MT)' },
  { value: 'America/Los_Angeles', label: 'Pacific Time (PT)' },
  { value: 'America/Anchorage', label: 'Alaska Time (AKT)' },
  { value: 'Pacific/Honolulu', label: 'Hawaii Time (HT)' },
  { value: 'UTC', label: 'UTC' },
  { value: 'Europe/London', label: 'London (GMT/BST)' },
  { value: 'Europe/Paris', label: 'Paris (CET/CEST)' },
  { value: 'Europe/Berlin', label: 'Berlin (CET/CEST)' },
  { value: 'Asia/Tokyo', label: 'Tokyo (JST)' },
  { value: 'Asia/Shanghai', label: 'Shanghai (CST)' },
  { value: 'Australia/Sydney', label: 'Sydney (AEST/AEDT)' },
]

// =============================================================================
// Condition Field Options
// =============================================================================

export interface ConditionFieldOption {
  value: string
  label: string
  category: string
  operators: ConditionOperator[]
  valueType: 'number' | 'string' | 'boolean' | 'select'
}

export const CONDITION_FIELD_OPTIONS: ConditionFieldOption[] = [
  // Account fields
  {
    value: 'account.balance',
    label: 'Account Balance',
    category: 'Account',
    operators: ['eq', 'neq', 'gt', 'gte', 'lt', 'lte', 'between'],
    valueType: 'number',
  },
  {
    value: 'account.type',
    label: 'Account Type',
    category: 'Account',
    operators: ['eq', 'neq'],
    valueType: 'select',
  },
  // Budget fields
  {
    value: 'budget.percent_used',
    label: 'Budget % Used',
    category: 'Budget',
    operators: ['eq', 'neq', 'gt', 'gte', 'lt', 'lte', 'between'],
    valueType: 'number',
  },
  {
    value: 'budget.total_spent',
    label: 'Budget Total Spent',
    category: 'Budget',
    operators: ['eq', 'neq', 'gt', 'gte', 'lt', 'lte', 'between'],
    valueType: 'number',
  },
  {
    value: 'budget.remaining_amount',
    label: 'Budget Remaining',
    category: 'Budget',
    operators: ['eq', 'neq', 'gt', 'gte', 'lt', 'lte', 'between'],
    valueType: 'number',
  },
  // Budget category fields
  {
    value: 'budget_category.percent_used',
    label: 'Category % Used',
    category: 'Budget Category',
    operators: ['eq', 'neq', 'gt', 'gte', 'lt', 'lte', 'between'],
    valueType: 'number',
  },
  {
    value: 'budget_category.spent_amount',
    label: 'Category Spent',
    category: 'Budget Category',
    operators: ['eq', 'neq', 'gt', 'gte', 'lt', 'lte', 'between'],
    valueType: 'number',
  },
  {
    value: 'budget_category.remaining_amount',
    label: 'Category Remaining',
    category: 'Budget Category',
    operators: ['eq', 'neq', 'gt', 'gte', 'lt', 'lte', 'between'],
    valueType: 'number',
  },
  // Transaction fields
  {
    value: 'transaction.amount',
    label: 'Transaction Amount',
    category: 'Transaction',
    operators: ['eq', 'neq', 'gt', 'gte', 'lt', 'lte', 'between'],
    valueType: 'number',
  },
  {
    value: 'transaction.merchant',
    label: 'Merchant Name',
    category: 'Transaction',
    operators: ['eq', 'neq', 'contains'],
    valueType: 'string',
  },
]

// =============================================================================
// Condition Operator Labels
// =============================================================================

export const CONDITION_OPERATOR_LABELS: Record<ConditionOperator, string> = {
  eq: 'equals',
  neq: 'not equals',
  gt: 'greater than',
  gte: 'greater than or equal',
  lt: 'less than',
  lte: 'less than or equal',
  between: 'between',
  contains: 'contains',
}

// =============================================================================
// Notification Channel Options
// =============================================================================

export const NOTIFICATION_CHANNEL_OPTIONS: Array<{
  value: NotificationChannel
  label: string
  description: string
}> = [
  {
    value: 'in_app',
    label: 'In-App',
    description: 'Show notification in the app',
  },
  {
    value: 'email',
    label: 'Email',
    description: 'Send email notification',
  },
  {
    value: 'push',
    label: 'Push',
    description: 'Send push notification to mobile',
  },
]

// =============================================================================
// Rollover Type Options
// =============================================================================

export const ROLLOVER_TYPE_OPTIONS: Array<{
  value: RolloverType
  label: string
  description: string
}> = [
  {
    value: 'full',
    label: 'Full Amount',
    description: 'Roll over the entire remaining amount',
  },
  {
    value: 'capped',
    label: 'Capped Amount',
    description: 'Roll over up to a maximum amount',
  },
  {
    value: 'percentage',
    label: 'Percentage',
    description: 'Roll over a percentage of the remaining amount',
  },
]

// =============================================================================
// Amount Type Options
// =============================================================================

export const AMOUNT_TYPE_OPTIONS: Array<{
  value: AmountType
  label: string
  description: string
}> = [
  {
    value: 'fixed',
    label: 'Fixed Amount',
    description: 'Transfer a specific dollar amount',
  },
  {
    value: 'percentage',
    label: 'Percentage',
    description: 'Transfer a percentage of the balance',
  },
  {
    value: 'excess',
    label: 'Excess Over Threshold',
    description: 'Transfer any amount above a threshold',
  },
  {
    value: 'remaining',
    label: 'Remaining Balance',
    description: 'Transfer the remaining balance above threshold',
  },
]

// =============================================================================
// Template Placeholders
// =============================================================================

export const ALERT_TEMPLATE_PLACEHOLDERS = [
  { placeholder: '{{budget_name}}', description: 'Name of the budget' },
  { placeholder: '{{category_name}}', description: 'Name of the category' },
  { placeholder: '{{percent_used}}', description: 'Percentage of budget used' },
  { placeholder: '{{total_spent}}', description: 'Total amount spent' },
  { placeholder: '{{remaining}}', description: 'Remaining budget amount' },
  { placeholder: '{{threshold}}', description: 'Alert threshold value' },
]

// =============================================================================
// Default Values
// =============================================================================

export const DEFAULT_PRIORITY = 10
export const DEFAULT_TIMEZONE = 'America/New_York'
export const DEFAULT_CRON = '0 9 * * *' // Daily at 9am

export const DEFAULT_TRIGGER_CONFIG = {
  type: 'schedule' as TriggerType,
  cron: DEFAULT_CRON,
  timezone: DEFAULT_TIMEZONE,
}

// =============================================================================
// Local Storage Keys
// =============================================================================

export const STORAGE_KEY_LIST_VIEW = 'rules-list-view'
