/**
 * Rules Feature Index
 * Issue #39: Rule Builder UI
 *
 * Exports all public API for the rules feature.
 */

// Components
export {
  RulesPage,
  RuleBuilder,
  RuleCard,
  RuleTable,
  CronBuilder,
  TriggerSelector,
  ConditionBuilder,
  BudgetAlertActionForm,
  TransferActionForm,
  RolloverActionForm,
  SavingsGoalActionForm,
} from './components'

// Types
export type {
  RuleCardProps,
  RuleTableProps,
  SortColumn,
  SortDirection,
  CronBuilderProps,
  TriggerSelectorProps,
  ConditionBuilderProps,
  BudgetAlertActionFormProps,
  TransferActionFormProps,
  RolloverActionFormProps,
  SavingsGoalActionFormProps,
} from './components'

// Hooks
export { useRuleForm } from './hooks/useRuleForm'

// Types
export type {
  RuleType,
  TriggerType,
  TriggerFormState,
  EventType,
  RuleCondition,
  ConditionOperator,
  ConditionLogic,
  AlertAction,
  TransferAction,
  RolloverAction,
  SavingsGoalAction,
  RuleAction,
  RuleFormState,
} from './types'

// Constants
export {
  RULE_TYPE_LABELS,
  RULE_TYPE_DESCRIPTIONS,
  TRIGGER_TYPE_LABELS,
  TRIGGER_TYPE_DESCRIPTIONS,
  CONDITION_FIELD_OPTIONS,
  CONDITION_OPERATOR_LABELS,
  EVENT_TYPE_OPTIONS,
  CRON_PRESETS,
  NOTIFICATION_CHANNEL_OPTIONS,
  DEFAULT_PRIORITY,
  DEFAULT_TRIGGER_CONFIG,
  DEFAULT_CRON,
  DEFAULT_TIMEZONE,
} from './constants'
